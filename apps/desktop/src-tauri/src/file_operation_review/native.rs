//! Standalone Win32 review window. No WebView, shell, elevation or file writes.
//! The production entry is not wired to CLI/Tauri yet. Tests create HIDDEN
//! windows and exercise controls; they do not constitute human consent testing.
use super::{Decision, PendingReview, ReviewedIntent};
use std::{
    cell::Cell,
    marker::PhantomData,
    rc::Rc,
    time::{SystemTime, UNIX_EPOCH},
};
use windows::{
    Win32::{
        Foundation::{HINSTANCE, HWND, LPARAM, LRESULT, RECT, WPARAM},
        Graphics::Gdi::{COLOR_WINDOW, DEFAULT_GUI_FONT, GetStockObject, GetSysColorBrush},
        System::LibraryLoader::GetModuleHandleW,
        UI::{
            Input::KeyboardAndMouse::{EnableWindow, SetFocus},
            WindowsAndMessaging::*,
        },
    },
    core::{PCWSTR, w},
};

type Result<T> = std::result::Result<T, String>;
type Check = Box<dyn Fn() -> Result<()> + Send>;
/// Only the native window adapter produces this handoff in production. It is
/// not serializable/cloneable and cannot be created from a WebView approval
/// boolean or by calling the presentation core's decide method alone.
pub(crate) struct NativeReviewedRequest(ReviewedIntent);
impl NativeReviewedRequest {
    pub(crate) fn into_unapproved_request(
        self,
        now_ms: u64,
    ) -> Result<crate::file_operation_contract::CheckedRequest> {
        self.0
            .into_unapproved_request(now_ms)
            .map_err(str::to_owned)
    }
    #[cfg(test)]
    pub(crate) fn simulated(intent: ReviewedIntent) -> Self {
        Self(intent)
    }
}
const CONTINUE: usize = 1001;
const CANCEL: usize = 1002;
const TIMER: usize = 1;
const PENDING: u8 = 0;
const ACCEPTED: u8 = 1;
const CANCELED: u8 = 2;
const FAILED: u8 = 3;
const EM_SETLIMITTEXT_MESSAGE: u32 = 0x00c5;
fn wide(value: &str) -> Vec<u16> {
    value.encode_utf16().chain(Some(0)).collect()
}
fn now() -> Result<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}

/// `is_current` must check the caller's live selection/session/peer/cancellation
/// scope. It runs before display, on a timer and again before handing off.
/// Keep it short and nonblocking: no model, disk scan, prompt or network calls.
/// The return still lacks trusted launch, SACL/journal proof or write permission.
pub(crate) fn show(
    pending: PendingReview,
    is_current: impl Fn() -> Result<()> + Send + 'static,
) -> Result<Option<NativeReviewedRequest>> {
    std::thread::spawn(move || Window::new(pending, Box::new(is_current))?.run())
        .join()
        .map_err(|_| "原生审查窗口异常退出；未执行文件操作".to_owned())?
        .map(|intent| intent.map(NativeReviewedRequest))
}

struct Context {
    pending: Option<PendingReview>,
    check: Check,
    expected: [String; 3],
    controls: Cell<[HWND; 5]>, // summary, difference, retained, continue, cancel
    outcome: Cell<u8>,
}
impl Context {
    fn valid(&self) -> Result<()> {
        (self.check)()?;
        self.pending
            .as_ref()
            .ok_or("审查已消费")?
            .document(now()?)?;
        Ok(())
    }
    fn exact_controls(&self) -> bool {
        self.controls.get()[..3]
            .iter()
            .zip(&self.expected)
            .all(|(&hwnd, expected)| {
                let expected: Vec<u16> = expected.encode_utf16().collect();
                // Owned same-thread controls; bounded by the already built document.
                let length = unsafe { GetWindowTextLengthW(hwnd) };
                if length < 0 || length as usize != expected.len() {
                    return false;
                }
                let mut actual = vec![0; expected.len() + 1];
                let copied = unsafe { GetWindowTextW(hwnd, &mut actual) };
                copied as usize == expected.len() && actual[..expected.len()] == expected
            })
    }
    fn command(&self, wparam: WPARAM, lparam: LPARAM) {
        if self.outcome.get() != PENDING {
            return;
        }
        let id = wparam.0 & 0xffff;
        let notification = wparam.0 >> 16;
        let controls = self.controls.get();
        if id == CANCEL || id == IDCANCEL.0 as usize {
            self.outcome.set(CANCELED);
        } else if id == CONTINUE
            && notification == BN_CLICKED as usize
            && lparam.0 == controls[3].0 as isize
            && !controls[3].0.is_null()
        {
            self.outcome
                .set(if self.valid().is_ok() && self.exact_controls() {
                    ACCEPTED
                } else {
                    FAILED
                });
        }
    }
}

struct Class {
    name: Vec<u16>,
    module: HINSTANCE,
}
impl Class {
    fn new() -> Result<Self> {
        let module = HINSTANCE(
            unsafe { GetModuleHandleW(None) }
                .map_err(|_| "原生模块不可用")?
                .0,
        );
        let name = wide(&format!("OpcSingleFileReview-{}", uuid::Uuid::new_v4()));
        let class = WNDCLASSW {
            lpfnWndProc: Some(procedure),
            hInstance: module,
            lpszClassName: PCWSTR(name.as_ptr()),
            hCursor: unsafe { LoadCursorW(None, IDC_ARROW) }.map_err(|_| "光标不可用")?,
            hbrBackground: unsafe { GetSysColorBrush(COLOR_WINDOW) },
            ..Default::default()
        };
        // Unique class name and this module's callback; never reuse another class.
        if unsafe { RegisterClassW(&class) } == 0 {
            return Err("无法注册原生审查窗口".into());
        }
        Ok(Self { name, module })
    }
}
impl Drop for Class {
    fn drop(&mut self) {
        let _ = unsafe { UnregisterClassW(PCWSTR(self.name.as_ptr()), Some(self.module)) };
    }
}
struct Window {
    hwnd: HWND,
    context: Box<Context>,
    _class: Class,
    _thread_bound: PhantomData<Rc<()>>,
}
impl Window {
    // Always create hidden. show() is the only ordinary path that reveals it;
    // fixture tests inspect this exact constructor without displaying a window.
    fn new(pending: PendingReview, check: Check) -> Result<Self> {
        check()?;
        let (document, _) = pending.document(now()?)?;
        let text = document.native_text()?;
        let context = Box::new(Context {
            pending: Some(pending),
            check,
            expected: [text.summary, text.difference, text.retained],
            controls: Cell::new([HWND::default(); 5]),
            outcome: Cell::new(PENDING),
        });
        let class = Class::new()?;
        let hwnd = unsafe {
            CreateWindowExW(
                WS_EX_CONTROLPARENT,
                PCWSTR(class.name.as_ptr()),
                w!("opc-workspace · 单文件完整审查（不会执行）"),
                WS_OVERLAPPEDWINDOW,
                CW_USEDEFAULT,
                CW_USEDEFAULT,
                1000,
                780,
                None,
                None,
                Some(class.module),
                None,
            )
        }
        .map_err(|_| "无法创建原生审查窗口")?;
        let window = Self {
            hwnd,
            context,
            _class: class,
            _thread_bound: PhantomData,
        };
        // Box address is stable until Window::drop destroys the HWND. The
        // callback only borrows it and never frees it or stores an escaping ref.
        unsafe {
            SetWindowLongPtrW(
                hwnd,
                GWLP_USERDATA,
                (&*window.context as *const Context) as isize,
            );
        }
        if unsafe { GetWindowLongPtrW(hwnd, GWLP_USERDATA) }
            != (&*window.context as *const Context) as isize
        {
            return Err("无法绑定原生窗口与本次审查".into());
        }
        let edit = WS_CHILD
            | WS_VISIBLE
            | WS_TABSTOP
            | WS_VSCROLL
            | WS_HSCROLL
            | WINDOW_STYLE((ES_MULTILINE | ES_READONLY | ES_AUTOVSCROLL | ES_AUTOHSCROLL) as u32);
        let mut controls = [HWND::default(); 5];
        for (index, control) in controls.iter_mut().enumerate() {
            let (class, caption, style, id) = if index < 3 {
                (w!("EDIT"), "", edit, 1100 + index)
            } else if index == 3 {
                (
                    w!("BUTTON"),
                    "确认以上内容（仅审查）",
                    WS_CHILD | WS_VISIBLE | WS_TABSTOP,
                    CONTINUE,
                )
            } else {
                (
                    w!("BUTTON"),
                    "取消",
                    WS_CHILD | WS_VISIBLE | WS_TABSTOP,
                    CANCEL,
                )
            };
            let caption = wide(caption);
            *control = unsafe {
                CreateWindowExW(
                    if index < 3 {
                        WS_EX_CLIENTEDGE
                    } else {
                        WINDOW_EX_STYLE(0)
                    },
                    class,
                    PCWSTR(caption.as_ptr()),
                    style,
                    0,
                    0,
                    10,
                    10,
                    Some(hwnd),
                    Some(HMENU(id as *mut _)),
                    Some(window._class.module),
                    None,
                )
            }
            .map_err(|_| "无法创建完整审查控件")?;
            unsafe {
                SendMessageW(
                    *control,
                    WM_SETFONT,
                    Some(WPARAM(GetStockObject(DEFAULT_GUI_FONT).0 as usize)),
                    Some(LPARAM(1)),
                );
            }
            if index < 3 {
                let value = wide(&window.context.expected[index]);
                unsafe {
                    SendMessageW(
                        *control,
                        EM_SETLIMITTEXT_MESSAGE,
                        Some(WPARAM(value.len())),
                        None,
                    );
                    SetWindowTextW(*control, PCWSTR(value.as_ptr()))
                        .map_err(|_| "无法显示完整审查内容")?;
                }
            }
        }
        window.context.controls.set(controls);
        if !window.context.exact_controls() {
            return Err("原生控件未完整保留审查内容；拒绝继续".into());
        }
        window.context.valid()?;
        layout(hwnd, &window.context);
        if unsafe { SetTimer(Some(hwnd), TIMER, 250, None) } == 0 {
            return Err("无法建立审查过期检查".into());
        }
        Ok(window)
    }
    fn run(mut self) -> Result<Option<ReviewedIntent>> {
        self.context.valid()?;
        unsafe {
            let _ = ShowWindow(self.hwnd, SW_SHOW);
            let _ = SetFocus(Some(self.context.controls.get()[4])); // Cancel is the safe initial focus.
        }
        let mut message = MSG::default();
        while self.context.outcome.get() == PENDING {
            // Dedicated thread created by show(); never drain the app's UI queue.
            let result = unsafe { GetMessageW(&mut message, None, 0, 0) }.0;
            if result <= 0 {
                self.context.outcome.set(FAILED);
                break;
            }
            unsafe {
                if !IsDialogMessageW(self.hwnd, &message).as_bool() {
                    let _ = TranslateMessage(&message);
                    DispatchMessageW(&message);
                }
            }
        }
        self.finish()
    }
    fn finish(&mut self) -> Result<Option<ReviewedIntent>> {
        let result = self.finish_once();
        // Even a guard failure after the click consumes the instance; restoring
        // scope or text later cannot resurrect a previously failed decision.
        self.context.pending.take();
        result
    }
    fn finish_once(&mut self) -> Result<Option<ReviewedIntent>> {
        let outcome = self.context.outcome.get();
        if outcome == CANCELED {
            self.context.pending.take();
            return Ok(None);
        }
        if outcome != ACCEPTED {
            self.context.pending.take();
            return Err("审查已失效或窗口异常；未执行文件操作".into());
        }
        self.context.valid()?;
        if !self.context.exact_controls() {
            self.context.pending.take();
            return Err("显示内容变化；未执行文件操作".into());
        }
        let pending = self.context.pending.take().ok_or("审查已消费")?;
        let (_, digest) = pending.document(now()?)?;
        let digest = digest.to_owned();
        let root = pending.request.facts().root_selection_id.clone();
        pending
            .decide(Decision::Continue, &digest, &root, now()?)
            .map_err(str::to_owned)
    }
}
impl Drop for Window {
    fn drop(&mut self) {
        unsafe {
            let mut class_name = vec![0; self._class.name.len()];
            let length = GetClassNameW(self.hwnd, &mut class_name);
            let owns_class = length > 0
                && class_name[..length as usize] == self._class.name[..self._class.name.len() - 1];
            if IsWindow(Some(self.hwnd)).as_bool()
                && (owns_class
                    || GetWindowLongPtrW(self.hwnd, GWLP_USERDATA)
                        == (&*self.context as *const Context) as isize)
            {
                let _ = KillTimer(Some(self.hwnd), TIMER);
                // Detach before freeing the Box even if destruction fails.
                SetWindowLongPtrW(self.hwnd, GWLP_USERDATA, 0);
                if GetWindowLongPtrW(self.hwnd, GWLP_USERDATA) != 0 {
                    std::process::abort(); // Never leave a callback to freed state.
                }
                let _ = DestroyWindow(self.hwnd);
            }
        }
        // Owned children are destroyed (or the callback is detached) before
        // context/class are dropped; a recycled foreign HWND is never touched.
    }
}
fn layout(hwnd: HWND, context: &Context) {
    let mut r = RECT::default();
    if unsafe { GetClientRect(hwnd, &mut r) }.is_err() {
        context.outcome.set(FAILED);
        return;
    }
    let width = (r.right - r.left - 24).max(40);
    let height = (r.bottom - r.top - 222).max(40);
    let left = (width - 12) / 2;
    let [summary, difference, retained, accept, cancel] = context.controls.get();
    if summary.0.is_null() {
        return;
    }
    for (control, x, y, w, h) in [
        (summary, 12, 12, width, 144),
        (difference, 12, 168, left, height),
        (retained, 24 + left, 168, width - left - 12, height),
        (accept, (width - 324).max(12), 180 + height, 220, 30),
        (cancel, (width - 90).max(12), 180 + height, 100, 30),
    ] {
        if unsafe { MoveWindow(control, x, y, w, h, true) }.is_err() {
            context.outcome.set(FAILED);
        }
    }
}
unsafe extern "system" fn procedure(
    hwnd: HWND,
    msg: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    // No Rust unwind may cross Win32. Context uses shared borrows + Cell because
    // child control messages can re-enter this callback synchronously.
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| unsafe {
        dispatch(hwnd, msg, wparam, lparam)
    }))
    .unwrap_or_else(|_| {
        let ptr = unsafe { GetWindowLongPtrW(hwnd, GWLP_USERDATA) } as *const Context;
        if let Some(context) = unsafe { ptr.as_ref() } {
            context.outcome.set(FAILED);
        }
        LRESULT(0)
    })
}
unsafe fn dispatch(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if msg == WM_GETMINMAXINFO && lparam.0 != 0 {
        let info = unsafe { &mut *(lparam.0 as *mut MINMAXINFO) };
        info.ptMinTrackSize.x = 720;
        info.ptMinTrackSize.y = 520;
        return LRESULT(0);
    }
    let ptr = unsafe { GetWindowLongPtrW(hwnd, GWLP_USERDATA) } as *const Context;
    if let Some(context) = unsafe { ptr.as_ref() } {
        match msg {
            WM_COMMAND => {
                context.command(wparam, lparam);
                return LRESULT(0);
            }
            WM_CLOSE | WM_DESTROY => {
                context.outcome.set(CANCELED);
                return LRESULT(0);
            }
            WM_TIMER if wparam.0 == TIMER => {
                if context.valid().is_err() {
                    context.outcome.set(FAILED);
                    unsafe {
                        let _ = EnableWindow(context.controls.get()[3], false);
                    }
                }
                return LRESULT(0);
            }
            WM_SIZE => {
                layout(hwnd, context);
                return LRESULT(0);
            }
            WM_NCDESTROY => unsafe {
                SetWindowLongPtrW(hwnd, GWLP_USERDATA, 0);
            },
            _ => (),
        }
    }
    unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) }
}

#[cfg(test)]
#[path = "native_tests.rs"]
mod tests;
