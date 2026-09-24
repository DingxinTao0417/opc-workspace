use super::wire::{Binding, MAX_FRAME};
use std::{
    mem::size_of,
    os::windows::{
        ffi::OsStringExt,
        io::{AsRawHandle, FromRawHandle, OwnedHandle},
    },
    path::PathBuf,
    sync::Arc,
    time::{Duration, Instant},
};
use windows::{
    Win32::{
        Foundation::{
            DUPLICATE_HANDLE_OPTIONS, DuplicateHandle, ERROR_INSUFFICIENT_BUFFER, ERROR_IO_PENDING,
            ERROR_PIPE_CONNECTED, FILETIME, HANDLE, HLOCAL, LocalFree, WAIT_OBJECT_0, WAIT_TIMEOUT,
        },
        Security::{
            Authorization::{
                ConvertSidToStringSidW, ConvertStringSecurityDescriptorToSecurityDescriptorW,
            },
            GetTokenInformation, PSECURITY_DESCRIPTOR, SECURITY_ATTRIBUTES, TOKEN_QUERY,
            TOKEN_USER, TokenUser,
        },
        Storage::FileSystem::{
            CreateFileW, FILE_FLAG_FIRST_PIPE_INSTANCE, FILE_FLAG_OVERLAPPED, FILE_SHARE_MODE,
            OPEN_EXISTING, PIPE_ACCESS_DUPLEX, ReadFile, SECURITY_ANONYMOUS, SECURITY_SQOS_PRESENT,
            WriteFile,
        },
        System::{
            IO::{CancelIoEx, GetOverlappedResult, OVERLAPPED},
            Pipes::{
                ConnectNamedPipe, CreateNamedPipeW, GetNamedPipeClientProcessId,
                GetNamedPipeServerProcessId, PIPE_READMODE_MESSAGE, PIPE_REJECT_REMOTE_CLIENTS,
                PIPE_TYPE_MESSAGE, PeekNamedPipe, SetNamedPipeHandleState,
            },
            Threading::{
                CreateEventW, GetCurrentProcess, GetProcessId, GetProcessTimes, OpenProcess,
                OpenProcessToken, PROCESS_NAME_FORMAT, PROCESS_QUERY_LIMITED_INFORMATION,
                PROCESS_SYNCHRONIZE, QueryFullProcessImageNameW, SetEvent, WaitForMultipleObjects,
                WaitForSingleObject,
            },
        },
    },
    core::{HRESULT, PCWSTR, PWSTR},
};

type Result<T> = std::result::Result<T, &'static str>;
fn raw(handle: &OwnedHandle) -> HANDLE {
    HANDLE(handle.as_raw_handle())
}
unsafe fn own(handle: HANDLE) -> OwnedHandle {
    // SAFETY: callers pass a successfully created, uniquely owned kernel handle.
    unsafe { OwnedHandle::from_raw_handle(handle.0) }
}
fn is_error(result: &windows::core::Result<()>, code: u32) -> bool {
    result
        .as_ref()
        .err()
        .is_some_and(|e| e.code() == HRESULT::from_win32(code))
}
fn wide(value: &str) -> Vec<u16> {
    value.encode_utf16().chain(Some(0)).collect()
}

/// OS identity, not executable-signature or human-approval verification. The
/// Parent launch adapters must consume the returned handle via from_handle;
/// PID-only pinning is not image authentication or a substitute for that handle.
pub(crate) struct Peer {
    handle: OwnedHandle,
    pid: u32,
    created: u64,
}
impl Peer {
    /// Compare kernel token users, never environment/user-name strings. This
    /// does not impersonate either process or grant access to its profile.
    pub(crate) fn require_same_user(&self) -> Result<()> {
        self.alive()?;
        require_same_sid(
            &token_user_sid(raw(&self.handle))?,
            &token_user_sid(unsafe { GetCurrentProcess() })?,
        )?;
        self.alive()
    }
    pub(crate) fn creation_time(&self) -> Result<u64> {
        self.alive()?;
        Ok(self.created)
    }
    pub(crate) fn require_creation_time(&self, expected: u64) -> Result<()> {
        if expected == 0 || self.creation_time()? != expected {
            return Err("辅助调用进程世代与本次启动不一致");
        }
        Ok(())
    }
    pub(crate) fn pin(pid: u32) -> Result<Self> {
        if pid == 0 {
            return Err("辅助通信进程身份无效");
        }
        // SAFETY: read/query/synchronize only, never terminate or inject.
        let handle = unsafe {
            OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE,
                false,
                pid,
            )
        }
        .map_err(|_| "无法固定辅助通信进程")?;
        let handle = unsafe { own(handle) };
        Self::from_handle(handle)
    }
    /// Consume the launch result itself; never reopen by a reusable PID. Keep
    /// only query/synchronize rights and a non-inheritable duplicate of the SAME
    /// kernel object. This still says nothing about image trust or approval.
    pub(crate) fn from_handle(launch: OwnedHandle) -> Result<Self> {
        let mut limited = HANDLE::default();
        unsafe {
            DuplicateHandle(
                GetCurrentProcess(),
                raw(&launch),
                GetCurrentProcess(),
                &mut limited,
                (PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_SYNCHRONIZE).0,
                false,
                DUPLICATE_HANDLE_OPTIONS(0),
            )
        }
        .map_err(|_| "无法保留启动返回的进程身份")?;
        let handle = unsafe { own(limited) };
        let pid = unsafe { GetProcessId(raw(&handle)) };
        if pid == 0 {
            return Err("启动返回的句柄不是可核验进程");
        }
        let mut times = [FILETIME::default(); 4];
        let [created, exited, kernel, user] = &mut times;
        unsafe { GetProcessTimes(raw(&handle), created, exited, kernel, user) }
            .map_err(|_| "无法核验辅助进程世代")?;
        let created = ((created.dwHighDateTime as u64) << 32) | created.dwLowDateTime as u64;
        let result = Self {
            handle,
            pid,
            created,
        };
        result.alive()?;
        Ok(result)
    }
    pub(crate) fn image_path(&self) -> Result<PathBuf> {
        self.alive()?;
        let mut buffer = vec![0u16; 32768];
        let mut size = buffer.len() as u32;
        unsafe {
            QueryFullProcessImageNameW(
                raw(&self.handle),
                PROCESS_NAME_FORMAT(0),
                PWSTR(buffer.as_mut_ptr()),
                &mut size,
            )
        }
        .map_err(|_| "无法核验辅助进程映像")?;
        let size = size as usize;
        if size == 0 || size >= buffer.len() || buffer[..size].contains(&0) {
            return Err("辅助进程映像路径无效");
        }
        self.alive()?;
        Ok(PathBuf::from(std::ffi::OsString::from_wide(
            &buffer[..size],
        )))
    }
    fn alive(&self) -> Result<()> {
        // A retained process handle and creation time bind a generation, not a
        // bare reusable PID. Exited peers invalidate even already-buffered data.
        if unsafe { WaitForSingleObject(raw(&self.handle), 0) } != WAIT_TIMEOUT
            || unsafe { GetProcessId(raw(&self.handle)) } != self.pid
            || self.created == 0
        {
            Err("辅助通信进程已退出或身份失效")
        } else {
            Ok(())
        }
    }
    fn verify(&self, pipe: HANDLE, server: bool) -> Result<()> {
        self.alive()?;
        let mut actual = 0;
        // SAFETY: live named-pipe handle; identity comes from kernel, not payload.
        unsafe {
            if server {
                GetNamedPipeClientProcessId(pipe, &mut actual)
            } else {
                GetNamedPipeServerProcessId(pipe, &mut actual)
            }
        }
        .map_err(|_| "无法核验辅助通信对端")?;
        if actual != self.pid {
            return Err("辅助通信连接到了非预期进程");
        }
        self.alive()
    }
}

fn require_same_sid(parent: &[u8], current: &[u8]) -> Result<()> {
    if parent.is_empty() || parent != current {
        return Err("辅助程序与父进程不是同一用户，拒绝定位恢复目录");
    }
    Ok(())
}
fn token_user_sid(process: HANDLE) -> Result<Vec<u8>> {
    use windows::Win32::Security::{GetLengthSid, IsValidSid};
    let mut token = HANDLE::default();
    unsafe { OpenProcessToken(process, TOKEN_QUERY, &mut token) }
        .map_err(|_| "无法核验恢复目录所属用户")?;
    let token = unsafe { own(token) };
    let mut length = 0;
    let measured = unsafe { GetTokenInformation(raw(&token), TokenUser, None, 0, &mut length) };
    if !is_error(&measured, ERROR_INSUFFICIENT_BUFFER.0)
        || !(size_of::<TOKEN_USER>()..=64 * 1024).contains(&(length as usize))
    {
        return Err("用户身份描述无效");
    }
    let capacity = length;
    let mut aligned = vec![0u64; (length as usize).div_ceil(8)];
    unsafe {
        GetTokenInformation(
            raw(&token),
            TokenUser,
            Some(aligned.as_mut_ptr().cast()),
            capacity,
            &mut length,
        )
    }
    .map_err(|_| "用户身份读取失败")?;
    if length > capacity || (length as usize) < size_of::<TOKEN_USER>() {
        return Err("用户身份长度变化");
    }
    let user = unsafe { &*aligned.as_ptr().cast::<TOKEN_USER>() };
    if !unsafe { IsValidSid(user.User.Sid) }.as_bool() {
        return Err("用户 SID 无效");
    }
    let sid_length = unsafe { GetLengthSid(user.User.Sid) } as usize;
    if !(8..=68).contains(&sid_length) {
        return Err("用户 SID 长度无效");
    }
    // Pointer and length come exclusively from the successful kernel query.
    Ok(unsafe { std::slice::from_raw_parts(user.User.Sid.0.cast::<u8>(), sid_length) }.to_vec())
}

#[derive(Clone)]
pub(crate) struct Cancel(Arc<OwnedHandle>);
impl Cancel {
    pub(crate) fn new() -> Result<Self> {
        let event = unsafe { CreateEventW(None, true, false, PCWSTR::null()) }
            .map_err(|_| "无法建立辅助通信取消信号")?;
        Ok(Self(Arc::new(unsafe { own(event) })))
    }
    pub(crate) fn cancel(&self) -> Result<()> {
        unsafe { SetEvent(raw(&self.0)) }.map_err(|_| "无法取消辅助通信")
    }
}
#[derive(Clone)]
struct Budget {
    until: Instant,
    cancel: Cancel,
}
impl Budget {
    fn for_request(
        request: &crate::file_operation_contract::CheckedRequest,
        now_ms: u64,
        cancel: Cancel,
    ) -> Result<Self> {
        let result = Self {
            until: request.wait_deadline(now_ms)?,
            cancel,
        };
        result.check()?;
        Ok(result)
    }
    fn new(timeout: Duration, cancel: Cancel) -> Result<Self> {
        if timeout.is_zero() || timeout > Duration::from_secs(30) {
            return Err("辅助通信期限必须介于零至三十秒之间");
        }
        Ok(Self {
            until: Instant::now() + timeout,
            cancel,
        })
    }
    fn check(&self) -> Result<u32> {
        if unsafe { WaitForSingleObject(raw(&self.cancel.0), 0) } != WAIT_TIMEOUT {
            return Err("辅助通信已取消");
        }
        let left = self
            .until
            .checked_duration_since(Instant::now())
            .ok_or("辅助通信已超时")?;
        Ok(left.as_millis().max(1).min(u32::MAX as u128) as u32)
    }
}

// LocalAlloc memory must never be handed to Rust's allocator.
struct Local(HLOCAL);
impl Drop for Local {
    fn drop(&mut self) {
        unsafe {
            let _ = LocalFree(Some(self.0));
        }
    }
}

fn pipe_acl() -> Result<Local> {
    let mut token = HANDLE::default();
    unsafe { OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &mut token) }
        .map_err(|_| "无法读取本机通信所有者")?;
    let token = unsafe { own(token) };
    let mut size = 0;
    let measured = unsafe { GetTokenInformation(raw(&token), TokenUser, None, 0, &mut size) };
    if !is_error(&measured, ERROR_INSUFFICIENT_BUFFER.0)
        || !(size_of::<TOKEN_USER>()..=64 * 1024).contains(&(size as usize))
    {
        return Err("通信所有者描述无效");
    }
    let capacity = size;
    let mut aligned = vec![0u64; (size as usize).div_ceil(8)];
    unsafe {
        GetTokenInformation(
            raw(&token),
            TokenUser,
            Some(aligned.as_mut_ptr().cast()),
            capacity,
            &mut size,
        )
    }
    .map_err(|_| "通信所有者读取失败")?;
    if size > capacity {
        return Err("通信所有者描述变化");
    }
    // SAFETY: aligned successful kernel TOKEN_USER response.
    let user = unsafe { &*aligned.as_ptr().cast::<TOKEN_USER>() };
    let mut sid = PWSTR::null();
    unsafe { ConvertSidToStringSidW(user.User.Sid, &mut sid) }.map_err(|_| "通信所有者身份无效")?;
    let _sid_memory = Local(HLOCAL(sid.0.cast()));
    let sid = unsafe { sid.to_string() }.map_err(|_| "通信所有者身份编码无效")?;
    // Explicit local user/SYSTEM/admin access, no Everyone/anonymous/default ACL.
    // Client-specific rights deliberately exclude FILE_CREATE_PIPE_INSTANCE.
    let sddl = wide(&format!(
        "D:P(A;;0x12019b;;;SY)(A;;0x12019b;;;BA)(A;;0x12019b;;;{sid})"
    ));
    let mut sd = PSECURITY_DESCRIPTOR::default();
    unsafe {
        ConvertStringSecurityDescriptorToSecurityDescriptorW(
            PCWSTR(sddl.as_ptr()),
            1,
            &mut sd,
            None,
        )
    }
    .map_err(|_| "无法构造专用通信权限")?;
    Ok(Local(HLOCAL(sd.0)))
}

fn pipe_name(id: &str) -> Result<Vec<u16>> {
    if !uuid::Uuid::parse_str(id).is_ok_and(|v| v.to_string() == id) {
        return Err("辅助通信身份不是规范 UUID");
    }
    Ok(wide(&format!(r"\\.\pipe\opc-file-helper-{id}")))
}

pub(crate) struct Listener {
    pipe: OwnedHandle,
    id: String,
    budget: Budget,
}
impl Listener {
    pub(crate) fn for_request(
        request: &crate::file_operation_contract::CheckedRequest,
        now_ms: u64,
        cancel: Cancel,
    ) -> Result<Self> {
        Self::with_budget(
            uuid::Uuid::new_v4().to_string(),
            Budget::for_request(request, now_ms, cancel)?,
        )
    }
    pub(crate) fn new(timeout: Duration, cancel: Cancel) -> Result<Self> {
        Self::create(uuid::Uuid::new_v4().to_string(), timeout, cancel)
    }
    fn create(id: String, timeout: Duration, cancel: Cancel) -> Result<Self> {
        let budget = Budget::new(timeout, cancel)?;
        Self::with_budget(id, budget)
    }
    fn with_budget(id: String, budget: Budget) -> Result<Self> {
        budget.check()?;
        let name = pipe_name(&id)?;
        let sd = pipe_acl()?;
        let attributes = SECURITY_ATTRIBUTES {
            nLength: size_of::<SECURITY_ATTRIBUTES>() as u32,
            lpSecurityDescriptor: sd.0.0,
            bInheritHandle: false.into(),
        };
        // SAFETY: local-only, first instance, one client, explicit owned ACL;
        // overlapped I/O always has an owned event and bounded completion wait.
        let pipe = unsafe {
            CreateNamedPipeW(
                PCWSTR(name.as_ptr()),
                PIPE_ACCESS_DUPLEX | FILE_FLAG_FIRST_PIPE_INSTANCE | FILE_FLAG_OVERLAPPED,
                PIPE_TYPE_MESSAGE | PIPE_READMODE_MESSAGE | PIPE_REJECT_REMOTE_CLIENTS,
                1,
                MAX_FRAME as u32,
                MAX_FRAME as u32,
                0,
                Some(&attributes),
            )
        };
        if pipe.is_invalid() {
            return Err("无法建立独占本机辅助通道");
        }
        Ok(Self {
            pipe: unsafe { own(pipe) },
            id,
            budget,
        })
    }
    pub(crate) fn id(&self) -> &str {
        &self.id
    }
    pub(crate) fn accept(self, peer: Peer) -> Result<Endpoint> {
        let endpoint = Endpoint {
            pipe: self.pipe,
            peer,
            budget: self.budget,
        };
        endpoint.check()?;
        let mut io = Io::new()?;
        let started = unsafe { ConnectNamedPipe(raw(&endpoint.pipe), Some(&mut io.overlapped)) };
        if !is_error(&started, ERROR_PIPE_CONNECTED.0) {
            endpoint.finish(&mut io, started)?;
        }
        endpoint.peer.verify(raw(&endpoint.pipe), true)?;
        endpoint.check()?;
        Ok(endpoint)
    }
}

pub(crate) fn connect(id: &str, peer: Peer, timeout: Duration, cancel: Cancel) -> Result<Endpoint> {
    let budget = Budget::new(timeout, cancel)?;
    budget.check()?;
    peer.alive()?;
    let name = pipe_name(id)?;
    // Anonymous SQOS prevents a server from impersonating an elevated client.
    // The server must already exist; no automatic reconnection or name fallback.
    let handle = unsafe {
        CreateFileW(
            PCWSTR(name.as_ptr()),
            0x12019b,
            FILE_SHARE_MODE(0),
            None,
            OPEN_EXISTING,
            FILE_FLAG_OVERLAPPED | SECURITY_SQOS_PRESENT | SECURITY_ANONYMOUS,
            None,
        )
    }
    .map_err(|_| "无法连接本次专用辅助通道")?;
    let pipe = unsafe { own(handle) };
    peer.verify(raw(&pipe), false)?;
    let mode = PIPE_READMODE_MESSAGE;
    unsafe { SetNamedPipeHandleState(raw(&pipe), Some(&mode), None, None) }
        .map_err(|_| "无法设置辅助通信消息边界")?;
    Ok(Endpoint { pipe, peer, budget })
}

// Io does not outlive finish(), which drains a canceled operation before its
// OVERLAPPED, event or data buffer can be freed. No global CancelIo/pipe reuse.
struct Io {
    overlapped: OVERLAPPED,
    _event: OwnedHandle,
}
impl Io {
    fn new() -> Result<Self> {
        let event = unsafe { CreateEventW(None, true, false, PCWSTR::null()) }
            .map_err(|_| "无法建立辅助通信等待信号")?;
        Ok(Self {
            overlapped: OVERLAPPED {
                hEvent: event,
                ..Default::default()
            },
            _event: unsafe { own(event) },
        })
    }
}
pub(crate) struct Endpoint {
    pipe: OwnedHandle,
    peer: Peer,
    budget: Budget,
}
impl Endpoint {
    #[cfg(test)]
    pub(super) fn send_extra_for_test(&self) -> Result<()> {
        self.send(b"unexpected extra message")
    }
    #[cfg(test)]
    pub(super) fn send_request_for_test(&self, binding: &Binding, body: &[u8]) -> Result<()> {
        binding.begin_request()?;
        self.send(&binding.frame(true, body)?)
    }
    #[cfg(test)]
    pub(super) fn oversized_peer_message(&self) -> Result<()> {
        // Emulate an uncooperative peer bypassing the encoder's limit. Never
        // compiled into the helper's production binary.
        let bytes = vec![0; MAX_FRAME + 1];
        let mut io = Io::new()?;
        let started = unsafe {
            WriteFile(
                raw(&self.pipe),
                Some(&bytes),
                None,
                Some(&mut io.overlapped),
            )
        };
        self.finish(&mut io, started).map(|_| ())
    }
    fn check(&self) -> Result<u32> {
        self.peer.alive()?;
        self.budget.check()
    }
    fn finish(&self, io: &mut Io, started: windows::core::Result<()>) -> Result<u32> {
        if is_error(&started, ERROR_IO_PENDING.0) {
            let outcome = self.check().and_then(|ms| {
                let handles = [
                    io.overlapped.hEvent,
                    raw(&self.budget.cancel.0),
                    raw(&self.peer.handle),
                ];
                let waited = unsafe { WaitForMultipleObjects(&handles, false, ms) };
                if waited == WAIT_OBJECT_0 {
                    Ok(())
                } else if waited == WAIT_TIMEOUT {
                    Err("辅助通信已超时")
                } else {
                    Err("辅助通信已取消或对端退出")
                }
            });
            if let Err(error) = outcome {
                // SAFETY: cancel only THIS pending operation; drain completion
                // before releasing stack/data memory even if completion raced.
                unsafe {
                    let _ = CancelIoEx(raw(&self.pipe), Some(&io.overlapped));
                    let mut ignored = 0;
                    let _ =
                        GetOverlappedResult(raw(&self.pipe), &io.overlapped, &mut ignored, true);
                }
                return Err(error);
            }
        } else {
            started.map_err(|_| "辅助通信失败或消息超限")?;
        }
        let mut count = 0;
        unsafe { GetOverlappedResult(raw(&self.pipe), &io.overlapped, &mut count, false) }
            .map_err(|_| "辅助通信未完整完成")?;
        self.check()?;
        Ok(count)
    }
    fn send(&self, bytes: &[u8]) -> Result<()> {
        self.check()?;
        if bytes.len() > MAX_FRAME {
            return Err("辅助通信消息超限");
        }
        let mut io = Io::new()?;
        let started =
            unsafe { WriteFile(raw(&self.pipe), Some(bytes), None, Some(&mut io.overlapped)) };
        if self.finish(&mut io, started)? as usize != bytes.len() {
            return Err("辅助通信只发送了部分消息");
        }
        Ok(())
    }
    fn receive(&self) -> Result<Vec<u8>> {
        self.check()?;
        let mut bytes = vec![0; MAX_FRAME];
        let mut io = Io::new()?;
        let started = unsafe {
            ReadFile(
                raw(&self.pipe),
                Some(&mut bytes),
                None,
                Some(&mut io.overlapped),
            )
        };
        let count = self.finish(&mut io, started)? as usize;
        if count == 0 || count > bytes.len() {
            return Err("辅助通信消息为空或无效");
        }
        bytes.truncate(count);
        Ok(bytes)
    }
    /// Consumes the connection even on timeout/failure: no retry or reuse.
    pub(crate) fn request(self, binding: &Binding, body: &[u8]) -> Result<Vec<u8>> {
        binding.begin_request()?;
        self.send(&binding.frame(true, body)?)?;
        binding.decode(false, self.receive()?)
    }
    pub(crate) fn incoming(self, binding: &Binding) -> Result<Incoming<'_>> {
        binding.begin_receive()?;
        let body = binding.decode(true, self.receive()?)?;
        Ok(Incoming {
            endpoint: self,
            binding,
            body,
        })
    }
}
pub(crate) struct Incoming<'a> {
    endpoint: Endpoint,
    binding: &'a Binding,
    body: Vec<u8>,
}
/// Owned, read-only liveness observer for the native window thread. No body,
/// binding, receive or reply API; duplicated handles retain the SAME objects.
pub(crate) struct ConnectionWatch {
    pipe: OwnedHandle,
    peer: Peer,
    budget: Budget,
}
impl ConnectionWatch {
    pub(crate) fn check(&self) -> Result<()> {
        self.peer.alive()?;
        self.budget.check()?;
        check_incoming_pipe(&self.pipe)?;
        self.peer.alive()?;
        self.budget.check().map(|_| ())
    }
}
fn check_incoming_pipe(pipe: &OwnedHandle) -> Result<()> {
    let mut available = 0;
    unsafe { PeekNamedPipe(raw(pipe), None, 0, None, Some(&mut available), None) }
        .map_err(|_| "本次辅助连接已关闭或不可用")?;
    if available != 0 {
        return Err("单次辅助连接出现额外请求数据");
    }
    Ok(())
}
impl Incoming<'_> {
    /// Call only after this bound body has passed the strict shared grammar.
    /// Untrusted bootstrap/ordinary connection timeouts cannot use this path.
    pub(crate) fn for_review(
        mut self,
        request: &crate::file_operation_contract::CheckedRequest,
        now_ms: u64,
    ) -> Result<(Self, ConnectionWatch)> {
        self.check()?;
        self.endpoint.budget =
            Budget::for_request(request, now_ms, self.endpoint.budget.cancel.clone())?;
        let watch = ConnectionWatch {
            pipe: self
                .endpoint
                .pipe
                .try_clone()
                .map_err(|_| "无法保留本次审查连接")?,
            peer: Peer::from_handle(
                self.endpoint
                    .peer
                    .handle
                    .try_clone()
                    .map_err(|_| "无法保留本次审查进程")?,
            )?,
            budget: self.endpoint.budget.clone(),
        };
        watch.check()?;
        Ok((self, watch))
    }
    pub(crate) fn check(&self) -> Result<()> {
        self.endpoint.check()?;
        // The peer can remain alive after canceling THIS session. Inspect our
        // overlapped pipe without consuming bytes; a closed channel or extra
        // request data invalidates this one-shot incoming request.
        check_incoming_pipe(&self.endpoint.pipe)?;
        self.endpoint.check().map(|_| ())
    }
    pub(crate) fn body(&self) -> &[u8] {
        &self.body
    }
    pub(crate) fn reply(self, body: &[u8]) -> Result<()> {
        self.check()?;
        self.endpoint.send(&self.binding.frame(false, body)?)
    }
}

#[cfg(test)]
pub(super) fn duplicate_listener_for_test(id: &str) -> Result<Listener> {
    Listener::create(id.to_owned(), Duration::from_secs(1), Cancel::new()?)
}

#[cfg(test)]
#[path = "request_budget_tests.rs"]
mod request_budget_tests;

#[cfg(test)]
mod user_tests {
    use super::*;
    #[test]
    fn same_user_requires_kernel_sid_equality_not_a_name_or_empty_identity() {
        let sid = token_user_sid(unsafe { GetCurrentProcess() }).unwrap();
        require_same_sid(&sid, &sid).unwrap();
        let mut different = sid.clone();
        *different.last_mut().unwrap() ^= 1;
        assert!(require_same_sid(&different, &sid).is_err());
        assert!(require_same_sid(&[], &[]).is_err());
        Peer::pin(std::process::id())
            .unwrap()
            .require_same_user()
            .unwrap();
    }
}
