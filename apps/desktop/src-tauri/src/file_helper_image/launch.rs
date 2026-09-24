//! Once-only parent orchestration behind trusted native commands. It launches
//! only the installed, bound helper after independent native review; tests use
//! owned ordinary children and never call the actual ShellExecute/UAC adapter.
use super::PinnedHelperImage;
use crate::{
    file_helper_transport::{
        native::{Cancel, Listener, Peer},
        wire::Binding,
    },
    file_operation_contract::{CheckedRequest, OperationReceipt},
    file_operation_review::{
        PendingReview,
        native::{self, NativeReviewedRequest},
    },
};
use std::{
    os::windows::{
        ffi::OsStrExt,
        io::{FromRawHandle, OwnedHandle},
    },
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use windows::{
    Win32::{
        Foundation::ERROR_CANCELLED,
        System::Com::{COINIT_APARTMENTTHREADED, CoInitializeEx, CoUninitialize},
        UI::{
            Shell::{
                SEE_MASK_FLAG_NO_UI, SEE_MASK_NOASYNC, SEE_MASK_NOCLOSEPROCESS, SHELLEXECUTEINFOW,
                ShellExecuteExW,
            },
            WindowsAndMessaging::SW_HIDE,
        },
    },
    core::{HRESULT, PCWSTR, w},
};
type Result<T> = std::result::Result<T, String>;
pub(crate) type CurrentScope = Arc<dyn Fn(&str) -> Result<()> + Send + Sync>;
const UAC_CANCELLED: &str = "本次系统提权已取消；不会自动重试";

fn now() -> Result<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}

/// The trusted native caller supplies a live root/operation lease check,
/// including user cancellation. Never use an always-true WebView flag. Success
/// is accepted only after the bound transport and strict terminal receipt both
/// match this operation.
pub(crate) fn review_and_exchange(
    request: CheckedRequest,
    check: CurrentScope,
) -> Result<Option<()>> {
    std::thread::spawn(move || {
        let root_id = request.facts().root_selection_id.clone();
        let operation_id = request.facts().operation_id.clone();
        check(&root_id)?;
        let image = PinnedHelperImage::installed()?;
        let pending = PendingReview::new(request, now()?)?;
        let during_review = check.clone();
        let Some(reviewed) = native::show(pending, move || during_review(&root_id))? else {
            return Ok(None);
        };
        let receipt = match exchange_once(image, reviewed, check.as_ref(), now, launch_elevated) {
            Err(error) if error == UAC_CANCELLED => return Ok(None),
            result => result?,
        };
        match OperationReceipt::decode(&receipt, &operation_id)? {
            OperationReceipt::ReviewCancelled => Ok(None),
            OperationReceipt::Succeeded => Ok(Some(())),
        }
    })
    .join()
    .map_err(|_| "辅助编排线程异常退出；结果未确认".to_owned())?
}

/// The launcher hook is private: production always uses the fixed adapter;
/// tests can model cancel/failure or spawn ONLY owned ordinary fixture children.
fn exchange_once(
    mut image: PinnedHelperImage,
    reviewed: NativeReviewedRequest,
    check: &(dyn Fn(&str) -> Result<()> + Sync),
    clock: impl Fn() -> Result<u64> + Sync,
    launch: impl FnOnce(&PinnedHelperImage, &Listener, &Binding) -> Result<OwnedHandle>,
) -> Result<Vec<u8>> {
    let request = reviewed.into_unapproved_request(clock()?)?;
    let validate = || -> Result<()> {
        check(&request.facts().root_selection_id)?;
        request.check_time(clock()?)?;
        Ok(())
    };
    validate()?;
    let bytes = request.encode(clock()?)?;
    let binding = Binding::new(&bytes)?;
    let cancel = Cancel::new()?;
    let listener = Listener::for_request(&request, clock()?, cancel.clone())?;
    image.revalidate()?;
    validate()?;
    // Keep cancellation live during OS consent and blocking pipe operations.
    // This is not an atomic revocation of bytes already sent or future writes.
    use std::sync::atomic::{AtomicBool, Ordering};
    let done = AtomicBool::new(false);
    let invalid = AtomicBool::new(false);
    std::thread::scope(|scope| {
        let watcher = scope.spawn(|| {
            while !done.load(Ordering::Acquire) {
                if !matches!(
                    std::panic::catch_unwind(std::panic::AssertUnwindSafe(&validate)),
                    Ok(Ok(()))
                ) {
                    invalid.store(true, Ordering::Release);
                    let _ = cancel.cancel();
                    break;
                }
                std::thread::park_timeout(Duration::from_millis(25));
            }
        });
        struct Stop<'a>(&'a AtomicBool, std::thread::Thread);
        impl Drop for Stop<'_> {
            fn drop(&mut self) {
                self.0.store(true, Ordering::Release);
                self.1.unpark();
            }
        }
        let stop = Stop(&done, watcher.thread().clone());
        let result = (|| {
            validate()?;
            if invalid.load(Ordering::Acquire) {
                return Err("本次审查已失效；不会启动辅助程序".into());
            }
            // Pin exists BEFORE launch; consume the returned handle directly.
            let launched = launch(&image, &listener, &binding)?;
            validate()?;
            let process = image.bind_process(launched)?;
            validate()?;
            let connection = process.accept(listener)?;
            validate()?;
            let receipt = connection.request(&binding, &bytes)?;
            validate()?;
            Ok(receipt)
        })();
        drop(stop);
        watcher.join().map_err(|_| "辅助取消检查异常退出")?;
        if invalid.load(Ordering::Acquire) {
            Err("本次目录选择、期限或取消状态已失效；结果未确认".into())
        } else {
            result
        }
    })
}

fn wide_path(path: &std::path::Path) -> Result<Vec<u16>> {
    let mut wide: Vec<_> = path.as_os_str().encode_wide().collect();
    if wide.is_empty() || wide.contains(&0) {
        return Err("辅助启动路径无效".into());
    }
    wide.push(0);
    Ok(wide)
}
struct Apartment;
impl Drop for Apartment {
    fn drop(&mut self) {
        unsafe {
            CoUninitialize();
        }
    }
}

// Only called on the dedicated thread AFTER native review. No cmd/PowerShell,
// file association, env expansion, search-path fallback, services or retry.
// Windows owns the UAC dialog; NO_UI suppresses error UI, not security prompts.
fn launch_elevated(
    image: &PinnedHelperImage,
    listener: &Listener,
    binding: &Binding,
) -> Result<OwnedHandle> {
    unsafe { CoInitializeEx(None, COINIT_APARTMENTTHREADED) }
        .ok()
        .map_err(|_| "无法初始化辅助启动线程")?;
    let _apartment = Apartment;
    let executable = wide_path(image.path())?;
    let directory = wide_path(image.path().parent().ok_or("辅助启动目录无效")?)?;
    let parameters: Vec<u16> = binding
        .bootstrap_parameters(
            listener.id(),
            std::process::id(),
            Peer::pin(std::process::id())?.creation_time()?,
        )?
        .encode_utf16()
        .chain(Some(0))
        .collect();
    let mut info = SHELLEXECUTEINFOW {
        cbSize: std::mem::size_of::<SHELLEXECUTEINFOW>() as u32,
        fMask: SEE_MASK_NOCLOSEPROCESS | SEE_MASK_NOASYNC | SEE_MASK_FLAG_NO_UI,
        lpVerb: w!("runas"),
        lpFile: PCWSTR(executable.as_ptr()),
        lpParameters: PCWSTR(parameters.as_ptr()),
        lpDirectory: PCWSTR(directory.as_ptr()),
        nShow: SW_HIDE.0,
        ..Default::default()
    };
    // This call can wait on user-controlled OS UI. Request/IPC deadlines are
    // NOT renewed; on return exchange_once rechecks before sending any content.
    let result = unsafe { ShellExecuteExW(&mut info) };
    let returned = if info.hProcess.is_invalid() {
        None
    } else {
        Some(unsafe { OwnedHandle::from_raw_handle(info.hProcess.0) })
    };
    if let Err(error) = result {
        return Err(if error.code() == HRESULT::from_win32(ERROR_CANCELLED.0) {
            UAC_CANCELLED.into()
        } else {
            "辅助启动未确认；不会自动重试".into()
        });
    }
    returned.ok_or_else(|| "启动未返回可核验进程句柄；不会发送请求".into())
}

#[cfg(test)]
#[path = "launch_tests.rs"]
mod tests;
