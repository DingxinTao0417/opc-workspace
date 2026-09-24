//! Compiled only by the separate helper binary, never by the desktop library.
//! The executable exposes status, read-only installed-build verification and a
//! closed internal bootstrap accepted only from the bound desktop launch chain.
//! No Tauri/WebView/model caller exists. The helper independently reviews one
//! request, prepares recovery evidence, performs started-only no-overwrite moves
//! and returns only a request-bound terminal receipt.
use crate::file_operation_contract as contract;
use std::ffi::OsString;

#[cfg(windows)]
#[allow(dead_code)] // Reachable only through the reviewed one-shot entry.
mod attempt_journal;
#[cfg(windows)]
#[allow(dead_code)] // Started-only core; never directly reachable from argv/Tauri.
mod execution;
#[cfg(windows)]
#[allow(dead_code)] // Durable stages are internal to the reviewed one-shot chain.
mod execution_journal;

#[cfg(windows)]
#[allow(dead_code)] // Pair integrity is one gate, never an operation permit by itself.
mod build_binding;
#[cfg(windows)]
#[allow(dead_code)] // Bound receive core; no direct Tauri/WebView/model caller.
mod parent_session;

pub(super) fn verify_build() -> Result<(), String> {
    #[cfg(windows)]
    {
        build_binding::InstalledPair::installed()?.revalidate()
    }
    #[cfg(not(windows))]
    {
        Err("构建配对验证仅支持 Windows".into())
    }
}

pub(super) fn review_once(args: &[OsString]) -> Result<(), String> {
    #[cfg(windows)]
    {
        let bootstrap = transport::bootstrap::UntrustedBootstrap::parse(args)?;
        parent_session::ParentSession::prepare(bootstrap)?.run_once()
    }
    #[cfg(not(windows))]
    {
        let _ = args;
        Err("单文件审批执行仅支持 Windows".into())
    }
}

#[cfg(windows)]
#[allow(dead_code)] // Entered only inside the reviewed helper chain.
mod privilege;
#[cfg(windows)]
#[allow(dead_code)] // Fixed same-user store inspection behind reviewed execution only.
mod recovery_record;
#[cfg(windows)]
#[allow(dead_code)] // Receives a request, never an approval or a file operation.
mod review_request;
#[cfg(windows)]
#[allow(dead_code)]
mod security;
#[cfg(windows)]
#[allow(dead_code)] // Only composed behind the helper's independent native review.
mod security_inspection;
#[cfg(windows)]
#[allow(dead_code)] // Trusted one-shot transport; never a general argv capability.
#[path = "../file_helper_transport/mod.rs"]
mod transport;

pub(super) fn status_only(args: &[OsString]) -> bool {
    args.len() == 1 && args[0] == "--status"
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_dispatch_accepts_only_the_exact_status_option() {
        assert!(status_only(&["--status".into()]));
        for args in [
            vec![],
            vec!["--replace".into()],
            vec!["--status".into(), "--elevate".into()],
            vec!["--status".into(), "C:\\secret.txt".into()],
            vec!["--pipe".into(), "spoofed".into()],
            vec!["--restore".into(), "request.json".into()],
        ] {
            assert!(!status_only(&args));
        }
    }
}
