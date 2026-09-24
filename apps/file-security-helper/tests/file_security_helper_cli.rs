//! Ordinary, non-elevated process tests in the isolated helper package. Never
//! enable a token privilege or request UAC. Internal bootstrap probes lack an
//! installed signed pair and fail before project inspection.
use std::process::{Command, Stdio};

#[test]
fn helper_reports_disabled_without_starting_desktop() {
    let output = Command::new(env!("CARGO_BIN_EXE_opc-file-security-helper"))
        .arg("--status")
        .stdin(Stdio::null())
        .output()
        .expect("start owned status-only helper");
    assert!(output.status.success());
    assert!(output.stderr.is_empty());
    let status: serde_json::Value = serde_json::from_slice(&output.stdout).unwrap();
    assert_eq!(
        status,
        serde_json::json!({
            "protocolVersion": 1,
            "status": "approval_transport_internal_only",
            "fileOperationsEnabled": false
        })
    );
}

#[test]
fn helper_rejects_paths_requests_and_unlock_flags_without_echoing_them() {
    for args in [
        vec![],
        vec!["--replace", "C:\\private-project\\secret.txt"],
        vec!["--request", "untrusted-request.json"],
        vec!["--pipe", "spoofed-pipe"],
        vec!["--status", "--enable"],
        vec!["--verify-build", "C:\\untrusted.json"],
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_opc-file-security-helper"))
            .args(args)
            .stdin(Stdio::null())
            .output()
            .expect("start owned refusing helper");
        assert_eq!(output.status.code(), Some(2));
        assert!(output.stdout.is_empty());
        assert_eq!(
            String::from_utf8(output.stderr).unwrap().trim(),
            "单文件辅助程序仅接受受信桌面启动的一次性审批通道；未提权、读取或修改文件。"
        );
    }
}

#[test]
fn review_bootstrap_is_closed_without_the_bound_installed_pair() {
    for args in [
        vec!["--review-v1", "untrusted-bootstrap"],
        vec![
            "--review-v1",
            "018f0000-0000-7000-8000-000000000001",
            "1",
            "1",
            "00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "00bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        ],
        vec![
            "--review-v1",
            "018f0000-0000-7000-8000-000000000001",
            "1",
            "1",
            "00aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "00bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
            "extra",
        ],
    ] {
        let output = Command::new(env!("CARGO_BIN_EXE_opc-file-security-helper"))
            .args(args)
            .stdin(Stdio::null())
            .output()
            .expect("start owned internal-bootstrap helper");
        assert_eq!(output.status.code(), Some(3));
        assert!(output.stdout.is_empty());
        assert_eq!(
            String::from_utf8(output.stderr).unwrap().trim(),
            "本次单文件操作未完成；不会自动重试，请通过桌面恢复记录核对当前状态。"
        );
    }
}

#[test]
fn runtime_key_override_does_not_enable_unbound_build_verification() {
    let output = Command::new(env!("CARGO_BIN_EXE_opc-file-security-helper"))
        .arg("--verify-build")
        .env("OPC_DESKTOP_BINDING_PUBLIC_KEY", "ab".repeat(64))
        .stdin(Stdio::null())
        .output()
        .unwrap();
    assert_eq!(output.status.code(), Some(2));
    assert!(output.stdout.is_empty());
    assert_eq!(
        String::from_utf8(output.stderr).unwrap().trim(),
        "本次构建配对无法核验；未提权或执行文件操作。"
    );
}
