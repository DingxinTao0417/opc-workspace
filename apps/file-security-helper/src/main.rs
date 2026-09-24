//! Separate process boundary for the reviewed single-file security helper.
//! The operation bootstrap is accepted only from the installed, bound desktop
//! launch chain. No path, request JSON, unlock flag or general IPC is exposed.
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/file_operation_contract.rs"]
mod file_operation_contract;
#[cfg(windows)]
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/file_operation_disk.rs"]
mod file_operation_disk;
#[cfg(windows)]
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/workspace_file_snapshot/replacement_metadata.rs"]
mod file_operation_metadata;
#[cfg(windows)]
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/workspace_file_snapshot/windows.rs"]
mod file_operation_read;
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/file_operation_review.rs"]
mod file_operation_review;
#[cfg(windows)]
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/workspace_file_snapshot/replacement_security.rs"]
mod file_operation_security;
#[cfg(windows)]
#[allow(dead_code)]
#[path = "../../desktop/src-tauri/src/file_recovery_record.rs"]
mod file_recovery_record;
#[path = "../../desktop/src-tauri/src/file_security_helper/mod.rs"]
mod helper;

fn main() {
    // Seven lets the strict six-field parser observe and reject one extra field
    // without allocating an unbounded attacker-controlled argv vector.
    let args: Vec<_> = std::env::args_os().skip(1).take(7).collect();
    if helper::status_only(&args) {
        println!(
            "{{\"protocolVersion\":1,\"status\":\"approval_transport_internal_only\",\"fileOperationsEnabled\":false}}"
        );
    } else if args.len() == 1 && args[0] == "--verify-build" {
        if helper::verify_build().is_ok() {
            println!(
                "{{\"protocolVersion\":1,\"buildPairVerified\":true,\"fileOperationsEnabled\":false}}"
            );
        } else {
            eprintln!("本次构建配对无法核验；未提权或执行文件操作。");
            std::process::exit(2);
        }
    } else if args.first().is_some_and(|value| value == "--review-v1") {
        if helper::review_once(&args).is_err() {
            eprintln!("本次单文件操作未完成；不会自动重试，请通过桌面恢复记录核对当前状态。");
            std::process::exit(3);
        }
    } else {
        eprintln!("单文件辅助程序仅接受受信桌面启动的一次性审批通道；未提权、读取或修改文件。");
        std::process::exit(2);
    }
}
