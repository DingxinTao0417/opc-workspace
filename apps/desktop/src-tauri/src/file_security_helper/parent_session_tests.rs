use super::super::{build_binding::tests::sign_pair, transport::native::Listener};
use super::*;
use crate::file_operation_contract::{
    Content, ObjectIdentity, ObservedFile, Operation, Request, sha256,
};
use std::{
    ffi::OsString,
    fs,
    io::{BufRead, BufReader, Read, Write},
    path::PathBuf,
    process::{Child, Command, Stdio},
    time::{Instant, SystemTime, UNIX_EPOCH},
};
const TIMEOUT: Duration = Duration::from_secs(10);

fn payload() -> Vec<u8> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64;
    let id = || uuid::Uuid::new_v4().to_string();
    serde_json::to_vec(&Request {
        version: 1,
        operation_id: id(),
        review_id: id(),
        root_selection_id: id(),
        issued_at_ms: now - 1,
        expires_at_ms: now + 60_000,
        root: std::env::var("OPC_PARENT_FIXTURE_ROOT").unwrap(),
        path: "test.txt".into(),
        root_identity: ObjectIdentity {
            volume: 1,
            index: 1,
        },
        parent_identity: ObjectIdentity {
            volume: 1,
            index: 1,
        },
        operation: Operation::Replace {
            original: ObservedFile {
                identity: ObjectIdentity {
                    volume: 1,
                    index: 2,
                },
                modified: 1,
                content: Content::from_bytes(b"test before").unwrap(),
                metadata_sha256: sha256(b"metadata"),
                readable_security_sha256: sha256(b"partial security"),
            },
            candidate: Content::from_bytes(b"test after").unwrap(),
        },
    })
    .unwrap()
}

// Only invoked inside an OWNED copy of this test executable, never the app or
// an elevated helper. It sends one unapproved request over a real local pipe.
#[test]
#[ignore = "owned parent fixture executed by the ordinary process tests"]
fn parent_process_fixture() {
    let mode = std::env::var("OPC_PARENT_FIXTURE_MODE").unwrap();
    let bytes = if mode == "malformed" {
        br#"{"approved":true}"#.to_vec()
    } else {
        payload()
    };
    let binding = Binding::new(&bytes).unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let me = Peer::pin(std::process::id()).unwrap();
    println!(
        "OPC_PARENT_BOOTSTRAP {}",
        binding
            .bootstrap_parameters(
                listener.id(),
                std::process::id(),
                me.creation_time().unwrap()
            )
            .unwrap()
    );
    std::io::stdout().flush().unwrap();
    if mode != "idle" {
        let peer = Peer::pin(
            std::env::var("OPC_PARENT_FIXTURE_CLIENT")
                .unwrap()
                .parse()
                .unwrap(),
        )
        .unwrap();
        let receipt = listener
            .accept(peer)
            .and_then(|endpoint| endpoint.request(&binding, &bytes));
        if mode == "good" {
            assert_eq!(receipt.unwrap(), b"received, unapproved");
        } else {
            assert!(receipt.is_err());
        }
    }
    // Keep the server process alive through the peer's receipt validation.
    std::io::stdin().read_to_end(&mut Vec::new()).unwrap();
}

struct Fixture {
    path: PathBuf,
    key: String,
}
impl Fixture {
    fn new() -> Self {
        let path =
            std::env::temp_dir().join(format!("opc-parent-session-test-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&path).unwrap();
        let source = fs::read(std::env::current_exe().unwrap()).unwrap();
        fs::write(path.join("opc-workspace-desktop.exe"), &source).unwrap();
        fs::write(path.join("opc-file-security-helper.exe"), b"fixture helper").unwrap();
        let (key, manifest) = sign_pair(b"fixture helper", &source);
        fs::write(path.join("opc-desktop-build-binding.json"), manifest).unwrap();
        Self { path, key }
    }
    fn pair(&self) -> InstalledPair {
        InstalledPair::fixture_open(&self.path, &self.key).unwrap()
    }
    fn child(&self, mode: &str) -> OwnedParent {
        let child = Command::new(self.path.join("opc-workspace-desktop.exe"))
            .args([
                "--exact",
                "helper::parent_session::tests::parent_process_fixture",
                "--ignored",
                "--nocapture",
            ])
            .env("OPC_PARENT_FIXTURE_MODE", mode)
            .env("OPC_PARENT_FIXTURE_CLIENT", std::process::id().to_string())
            .env("OPC_PARENT_FIXTURE_ROOT", self.path.join("selected")) // never exists; no unrelated project touched
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        OwnedParent {
            child,
            reader: None,
        }
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(
            self.path.is_absolute() && self.path.parent() == Some(std::env::temp_dir().as_path())
        );
        assert!(
            self.path
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-parent-session-test-")
        );
        fs::remove_dir_all(&self.path).unwrap();
    }
}
struct OwnedParent {
    child: Child,
    reader: Option<std::thread::JoinHandle<()>>,
}
impl OwnedParent {
    fn arguments(&mut self) -> Vec<OsString> {
        let stdout = self.child.stdout.take().unwrap();
        let (send, recv) = std::sync::mpsc::channel();
        self.reader = Some(std::thread::spawn(move || {
            for line in BufReader::new(stdout).lines().take(16) {
                let line = line.unwrap();
                if let Some(parameters) = line.strip_prefix("OPC_PARENT_BOOTSTRAP ") {
                    assert!(parameters.len() < 512);
                    let _ = send.send(
                        parameters
                            .split(' ')
                            .map(OsString::from)
                            .collect::<Vec<_>>(),
                    );
                    // Continue draining until the owned test harness exits;
                    // closing stdout here makes its final summary fail.
                }
            }
        }));
        let args = recv.recv_timeout(TIMEOUT).expect("owned parent bootstrap");
        args
    }
    fn finish(&mut self) {
        drop(self.child.stdin.take());
        let until = Instant::now() + TIMEOUT;
        loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                assert!(status.success(), "owned parent failed");
                return;
            }
            assert!(Instant::now() < until, "owned parent did not finish");
            std::thread::sleep(Duration::from_millis(5));
        }
    }
    fn stop(&mut self) {
        self.child.kill().unwrap();
        self.child.wait().unwrap();
    }
}
impl Drop for OwnedParent {
    fn drop(&mut self) {
        if !matches!(self.child.try_wait(), Ok(Some(_))) {
            let _ = self.child.kill();
        }
        let _ = self.child.wait();
        if let Some(reader) = self.reader.take() {
            let _ = reader.join();
        }
    }
}

#[test]
fn signed_pair_process_generation_pipe_and_request_are_checked_together() {
    let f = Fixture::new();
    let mut parent = f.child("good");
    let args = parent.arguments();
    let mut session =
        ParentSession::connect(UntrustedBootstrap::parse(&args).unwrap(), f.pair()).unwrap();
    let mut request = session.receive().unwrap();
    assert_eq!(request.unapproved().unwrap().facts().path, "test.txt");
    assert!(request.review_disk().is_err()); // authenticated metadata is not disk proof or approval
    for name in [
        "opc-workspace-desktop.exe",
        "opc-file-security-helper.exe",
        "opc-desktop-build-binding.json",
    ] {
        assert!(fs::write(f.path.join(name), b"must remain pinned").is_err());
    }
    request.reply(b"received, unapproved").unwrap();
    assert!(session.receive().is_err());
    parent.finish();
}

#[test]
fn wrong_parent_generation_or_process_is_rejected_before_connecting() {
    for field in [2, 3] {
        let f = Fixture::new();
        let mut parent = f.child("idle");
        let mut args = parent.arguments();
        args[field] = if field == 2 {
            std::process::id().to_string()
        } else {
            "1".into()
        }
        .into();
        assert!(
            ParentSession::connect(UntrustedBootstrap::parse(&args).unwrap(), f.pair()).is_err()
        );
        parent.finish();
    }
}

#[test]
fn malformed_body_or_binding_consumes_the_only_receive_attempt() {
    for mode in ["malformed", "wrong_binding"] {
        let f = Fixture::new();
        let mut parent = f.child(mode);
        let mut args = parent.arguments();
        if mode == "wrong_binding" {
            args[5] = "00".repeat(32).into();
        }
        let mut session =
            ParentSession::connect(UntrustedBootstrap::parse(&args).unwrap(), f.pair()).unwrap();
        assert!(session.receive().is_err());
        assert!(session.receive().is_err());
        parent.finish();
    }
}

#[test]
fn late_manifest_alias_prevents_receiving_even_buffered_content() {
    let f = Fixture::new();
    let mut parent = f.child("reject");
    let args = parent.arguments();
    let mut session =
        ParentSession::connect(UntrustedBootstrap::parse(&args).unwrap(), f.pair()).unwrap();
    fs::hard_link(
        f.path.join("opc-desktop-build-binding.json"),
        f.path.join("alias"),
    )
    .unwrap();
    assert!(session.receive().is_err());
    assert!(session.receive().is_err());
    parent.finish();
}

#[test]
fn exited_parent_invalidates_a_received_request_before_use_or_reply() {
    let f = Fixture::new();
    let mut parent = f.child("reject");
    let args = parent.arguments();
    let mut session =
        ParentSession::connect(UntrustedBootstrap::parse(&args).unwrap(), f.pair()).unwrap();
    let mut request = session.receive().unwrap();
    parent.stop();
    assert!(request.unapproved().is_err());
    assert!(request.reply(b"must not reply").is_err());
}
