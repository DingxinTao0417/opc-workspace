//! A private impersonation token on one synchronous helper thread. Never adjust
//! the app/process token or enable Backup/Restore/TakeOwnership privileges.
use std::{marker::PhantomData, mem::size_of, rc::Rc};
use windows::{
    Win32::{
        Foundation::{CloseHandle, ERROR_NO_TOKEN, ERROR_SUCCESS, GetLastError, HANDLE, LUID},
        Security::{
            AdjustTokenPrivileges, DuplicateTokenEx, GetTokenInformation, LUID_AND_ATTRIBUTES,
            LookupPrivilegeValueW, RevertToSelf, SE_PRIVILEGE_ENABLED, SE_SECURITY_NAME,
            SecurityImpersonation, TOKEN_ADJUST_PRIVILEGES, TOKEN_DUPLICATE, TOKEN_ELEVATION,
            TOKEN_IMPERSONATE, TOKEN_PRIVILEGES, TOKEN_QUERY, TokenElevation, TokenImpersonation,
        },
        System::Threading::{
            GetCurrentProcess, GetCurrentThread, OpenProcessToken, OpenThreadToken, SetThreadToken,
        },
    },
    core::PCWSTR,
};

struct Token(HANDLE);
impl Drop for Token {
    fn drop(&mut self) {
        // SAFETY: this wrapper exclusively owns a successfully opened token.
        unsafe {
            let _ = CloseHandle(self.0);
        }
    }
}

// BOOL success is insufficient: Windows may return ERROR_NOT_ALL_ASSIGNED.
fn fully_adjusted(success: bool, last_error: u32) -> Result<(), &'static str> {
    if !success || last_error != ERROR_SUCCESS.0 {
        Err("无法启用本次所需审计权限")
    } else {
        Ok(())
    }
}

/// !Send + !Sync: must never cross threads or an async suspension point.
struct AuditScope {
    _token: Token,
    _thread: PhantomData<Rc<()>>,
}
impl AuditScope {
    fn enter() -> Result<Self, &'static str> {
        require_unimpersonated_thread()?;
        let mut process = HANDLE::default();
        // SAFETY: owned output, no mutation of the process token.
        unsafe {
            OpenProcessToken(
                GetCurrentProcess(),
                TOKEN_QUERY | TOKEN_DUPLICATE,
                &mut process,
            )
        }
        .map_err(|_| "无法读取辅助进程身份")?;
        let process = Token(process);
        let mut elevation = TOKEN_ELEVATION::default();
        let mut length = 0;
        // SAFETY: exact sized, aligned initialized output.
        unsafe {
            GetTokenInformation(
                process.0,
                TokenElevation,
                Some((&mut elevation as *mut TOKEN_ELEVATION).cast()),
                size_of::<TOKEN_ELEVATION>() as u32,
                &mut length,
            )
        }
        .map_err(|_| "无法核验辅助进程提权状态")?;
        if length as usize != size_of::<TOKEN_ELEVATION>() || elevation.TokenIsElevated == 0 {
            return Err("必须由本次人工批准启动独立提权辅助程序");
        }
        let mut duplicate = HANDLE::default();
        // SAFETY: a NEW non-inheritable token; process token is never adjusted.
        unsafe {
            DuplicateTokenEx(
                process.0,
                TOKEN_QUERY | TOKEN_ADJUST_PRIVILEGES | TOKEN_IMPERSONATE,
                None,
                SecurityImpersonation,
                TokenImpersonation,
                &mut duplicate,
            )
        }
        .map_err(|_| "无法创建本次独立权限上下文")?;
        let token = Token(duplicate);
        // Disable inherited privileges on the duplicate before enabling exactly one.
        let disabled = unsafe { AdjustTokenPrivileges(token.0, true, None, 0, None, None) };
        let disabled_error = unsafe { GetLastError() }.0;
        fully_adjusted(disabled.is_ok(), disabled_error)?;
        let mut luid = LUID::default();
        unsafe { LookupPrivilegeValueW(PCWSTR::null(), SE_SECURITY_NAME, &mut luid) }
            .map_err(|_| "系统不支持所需审计权限")?;
        let requested = TOKEN_PRIVILEGES {
            PrivilegeCount: 1,
            Privileges: [LUID_AND_ATTRIBUTES {
                Luid: luid,
                Attributes: SE_PRIVILEGE_ENABLED,
            }],
        };
        let adjusted =
            unsafe { AdjustTokenPrivileges(token.0, false, Some(&requested), 0, None, None) };
        let adjusted_error = unsafe { GetLastError() }.0;
        fully_adjusted(adjusted.is_ok(), adjusted_error)?;
        // SAFETY: applies only to this synchronous thread, reverted by Drop.
        unsafe { SetThreadToken(None, Some(token.0)) }.map_err(|_| "无法进入本次权限上下文")?;
        Ok(Self {
            _token: token,
            _thread: PhantomData,
        })
    }
}
pub(super) fn require_unimpersonated_thread() -> Result<(), &'static str> {
    let mut existing = HANDLE::default();
    // Read-only check. Never replace, revert, or impersonate an existing token.
    let result = unsafe { OpenThreadToken(GetCurrentThread(), TOKEN_QUERY, true, &mut existing) };
    if result.is_ok() {
        drop(Token(existing));
        return Err("辅助线程已有身份上下文，拒绝嵌套权限操作");
    }
    if result.unwrap_err().code().0 as u32 != (0x80070000 | ERROR_NO_TOKEN.0) {
        return Err("无法核验辅助线程身份");
    }
    Ok(())
}
impl Drop for AuditScope {
    fn drop(&mut self) {
        // SAFETY: same thread; no earlier impersonation existed. If reverting
        // fails, do not continue running with unexpected rights in this helper.
        if unsafe { RevertToSelf() }.is_err() {
            std::process::abort();
        }
    }
}

pub(super) fn with_audit_privilege<T>(
    work: impl FnOnce() -> Result<T, String>,
) -> Result<T, String> {
    scoped(AuditScope::enter, work)
}

fn scoped<G, T, E: From<&'static str>>(
    enter: impl FnOnce() -> Result<G, &'static str>,
    work: impl FnOnce() -> Result<T, E>,
) -> Result<T, E> {
    let _scope = enter().map_err(E::from)?;
    work()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::Cell;

    #[test]
    fn partial_adjustment_is_failure_even_with_success_bool() {
        assert!(fully_adjusted(true, 0).is_ok());
        for (ok, code) in [(true, 1300), (false, 0), (false, 5), (true, 5)] {
            assert!(fully_adjusted(ok, code).is_err());
        }
    }
    struct FakeScope<'a>(&'a Cell<usize>);
    impl Drop for FakeScope<'_> {
        fn drop(&mut self) {
            self.0.set(self.0.get() + 1);
        }
    }

    #[test]
    fn scope_released_after_success_failure_and_unwind() {
        let drops = Cell::new(0);
        assert_eq!(
            scoped(|| Ok(FakeScope(&drops)), || Ok::<_, String>(7)),
            Ok(7)
        );
        assert_eq!(
            scoped(|| Ok(FakeScope(&drops)), || Err::<(), _>("failed")),
            Err("failed")
        );
        assert_eq!(
            scoped(
                || Ok(FakeScope(&drops)),
                || Err::<(), String>("disk validation failed".into())
            ),
            Err("disk validation failed".into())
        );
        let panic = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            let _ = scoped(
                || Ok(FakeScope(&drops)),
                || -> Result<(), &'static str> { panic!("fixture") },
            );
        }));
        assert!(panic.is_err());
        assert_eq!(drops.get(), 4);
    }

    #[test]
    fn failed_scope_never_performs_operation() {
        let result =
            scoped::<(), (), &'static str>(|| Err("unavailable"), || panic!("must not run"));
        assert_eq!(result, Err("unavailable"));
        assert_eq!(
            scoped::<(), (), String>(|| Err("unavailable"), || panic!("must not capture")),
            Err("unavailable".into())
        );
    }
    // No test calls AuditScope::enter: default checks never adjust OS tokens.
}
