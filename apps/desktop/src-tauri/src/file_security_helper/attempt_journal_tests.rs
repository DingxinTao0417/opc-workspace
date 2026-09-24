use super::*;
use crate::{
    file_operation_contract::{Content, ObservedFile, Operation, Request},
    file_operation_metadata::Metadata,
};
use std::{
    io::{BufRead, BufReader},
    path::PathBuf,
    process::{Child, Command, Stdio},
};

struct Fixture(PathBuf);
impl Fixture {
    fn new() -> Self {
        let path =
            std::env::temp_dir().join(format!("opc-helper-attempt-test-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&path).unwrap();
        Self(path)
    }
    fn request(&self) -> CheckedRequest {
        let id = || uuid::Uuid::new_v4().to_string();
        let identity = ObjectIdentity {
            volume: 1,
            index: 1,
        };
        let at = now().unwrap();
        // Grammar fixtures only: not disk identity evidence or native approval.
        CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: id(),
                review_id: id(),
                root_selection_id: id(),
                issued_at_ms: at,
                expires_at_ms: at + 60_000,
                root: self.0.to_str().unwrap().into(),
                path: "private-project-file.txt".into(),
                root_identity: identity.clone(),
                parent_identity: identity.clone(),
                operation: Operation::Replace {
                    original: ObservedFile {
                        identity,
                        modified: 1,
                        content: Content::from_bytes(b"private original text").unwrap(),
                        metadata_sha256: sha256(b"metadata"),
                        readable_security_sha256: sha256(b"partial"),
                    },
                    candidate: Content::from_bytes(b"private candidate text").unwrap(),
                },
            },
            at,
        )
        .unwrap()
    }
    fn storage(&self) -> PathBuf {
        self.0.join(DIRECTORY)
    }
    fn path(&self, request: &CheckedRequest) -> PathBuf {
        self.storage()
            .join(format!("{}.json", request.facts().operation_id))
    }
    fn seed(&self, request: &CheckedRequest) {
        fs::create_dir_all(self.storage()).unwrap();
        fs::write(
            self.path(request),
            serde_json::to_vec(&Record::for_request(request).unwrap()).unwrap(),
        )
        .unwrap();
    }
    fn stage_request(
        &self,
    ) -> (
        CheckedRequest,
        super::super::execution_journal::AttemptIdentity,
        super::super::security_inspection::FrozenSecurityInspection,
    ) {
        let path = self.0.join("stage-source.txt");
        fs::write(&path, b"private original text").unwrap();
        let file = File::open(path).unwrap();
        let metadata = Metadata::capture(&file, b"private original text").unwrap();
        let id = || uuid::Uuid::new_v4().to_string();
        let identity = ObjectIdentity {
            volume: 1,
            index: 2,
        };
        let at = now().unwrap();
        let request = CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: id(),
                review_id: id(),
                root_selection_id: id(),
                issued_at_ms: at,
                expires_at_ms: at + 60_000,
                root: self.0.to_str().unwrap().into(),
                root_identity: ObjectIdentity {
                    volume: 1,
                    index: 1,
                },
                parent_identity: ObjectIdentity {
                    volume: 1,
                    index: 1,
                },
                path: "stage-source.txt".into(),
                operation: Operation::Replace {
                    original: ObservedFile {
                        identity,
                        modified: 1,
                        content: Content::from_bytes(b"private original text").unwrap(),
                        metadata_sha256: metadata.review_fingerprint().unwrap(),
                        readable_security_sha256: sha256(b"partial"),
                    },
                    candidate: Content::from_bytes(b"private candidate text").unwrap(),
                },
            },
            at,
        )
        .unwrap();
        let request_sha256 = sha256(&request.encode(at).unwrap());
        let attempt = super::super::execution_journal::AttemptIdentity {
            operation_id: request.facts().operation_id.clone(),
            review_id: request.facts().review_id.clone(),
            request_sha256: request_sha256.clone(),
        };
        let inspection = super::super::security_inspection::FrozenSecurityInspection {
            request_sha256,
            original: super::super::security_inspection::FrozenFileObservation {
                security: vec![
                    1, 0, 4, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 20, 0, 0, 0, 2, 0, 8, 0, 0,
                    0, 0, 0,
                ],
                metadata: metadata.freeze().unwrap(),
            },
            candidate: None,
        };
        (request, attempt, inspection)
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(self.0.is_absolute() && self.0.parent() == Some(std::env::temp_dir().as_path()));
        assert!(
            self.0
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-helper-attempt-test-")
        );
        fs::remove_dir_all(&self.0).unwrap();
    }
}
fn changed(request: &CheckedRequest, field: &str, value: serde_json::Value) -> CheckedRequest {
    let mut wire: serde_json::Value =
        serde_json::from_slice(&request.encode(now().unwrap()).unwrap()).unwrap();
    wire[field] = value;
    CheckedRequest::decode(&serde_json::to_vec(&wire).unwrap(), now().unwrap()).unwrap()
}

#[test]
fn reservation_is_metadata_only_and_survives_handle_release() {
    let f = Fixture::new();
    let request = f.request();
    let mut held = register(&f.0, &request).unwrap();
    held.revalidate(&request).unwrap();
    assert!(fs::write(f.path(&request), b"overwrite").is_err());
    assert!(fs::rename(f.storage(), f.0.join("moved")).is_err());
    let changed = changed(&request, "path", "different.txt".into());
    assert!(held.revalidate(&changed).is_err());
    drop(held);
    let bytes = fs::read(f.path(&request)).unwrap();
    let text = std::str::from_utf8(&bytes).unwrap();
    assert!(!text.contains("private") && !text.contains(f.0.to_str().unwrap()));
    let record = Record::decode(&bytes, &request.facts().operation_id).unwrap();
    assert_eq!(
        record.request_sha256,
        sha256(&request.encode(now().unwrap()).unwrap())
    );
    assert!(
        register(&f.0, &request)
            .unwrap_err_message()
            .contains("已登记")
    );
    assert_eq!(fs::read(f.path(&request)).unwrap(), bytes);
}

// Avoid Debug implementations that could expose internal file handles/records.
trait ErrorMessage {
    fn unwrap_err_message(self) -> String;
}
impl ErrorMessage for Result<RegisteredAttempt> {
    fn unwrap_err_message(self) -> String {
        match self {
            Ok(_) => panic!("unexpected acceptance"),
            Err(e) => e,
        }
    }
}

#[test]
fn operation_and_review_are_independently_consumed_even_with_changed_payload() {
    let f = Fixture::new();
    let request = f.request();
    drop(register(&f.0, &request).unwrap());
    for (field, value) in [
        ("operation_id", uuid::Uuid::new_v4().to_string()),
        ("review_id", uuid::Uuid::new_v4().to_string()),
        ("path", "changed.txt".to_string()),
    ] {
        assert!(register(&f.0, &changed(&request, field, value.into())).is_err());
    }
    assert_eq!(fs::read_dir(f.storage()).unwrap().count(), 2);
}

#[test]
fn held_store_serializes_other_attempts_without_retrying_or_overwriting() {
    let f = Fixture::new();
    let first = f.request();
    let second = f.request();
    let held = register(&f.0, &first).unwrap();
    let root = f.0.clone();
    std::thread::scope(|scope| {
        scope
            .spawn(|| assert!(register(&root, &second).is_err()))
            .join()
            .unwrap();
    });
    assert!(!f.path(&second).exists());
    drop(held);
    drop(register(&f.0, &second).unwrap());
    assert!(f.path(&first).exists() && f.path(&second).exists());
}

#[test]
fn torn_unknown_and_extended_records_fail_closed_without_cleanup() {
    for mode in [
        "empty",
        "partial",
        "unknown",
        "oversized",
        "extended",
        "duplicate",
    ] {
        let f = Fixture::new();
        let old = f.request();
        let next = f.request();
        f.seed(&old);
        let original = fs::read(f.path(&old)).unwrap();
        match mode {
            "empty" => fs::write(f.path(&old), b"").unwrap(),
            "partial" => fs::write(f.path(&old), b"{\"version\":1").unwrap(),
            "unknown" => fs::write(f.storage().join("unexpected"), b"preserve").unwrap(),
            "oversized" => fs::write(f.path(&old), vec![b' '; MAX_BYTES + 1]).unwrap(),
            "extended" | "duplicate" => {
                let suffix = if mode == "extended" {
                    ",\"approved\":true}"
                } else {
                    ",\"version\":1}"
                };
                let mut bytes = original[..original.len() - 1].to_vec();
                bytes.extend_from_slice(suffix.as_bytes());
                fs::write(f.path(&old), bytes).unwrap();
            }
            _ => unreachable!(),
        }
        let before = fs::read(f.path(&old)).unwrap();
        assert!(register(&f.0, &next).is_err(), "{mode}");
        assert!(!f.path(&next).exists());
        assert_eq!(fs::read(f.path(&old)).unwrap(), before);
    }
}

#[test]
fn capacity_is_bounded_and_never_prunes_old_attempts() {
    let f = Fixture::new();
    for _ in 0..MAX_RECORDS {
        f.seed(&f.request());
    }
    let request = f.request();
    assert!(
        register(&f.0, &request)
            .unwrap_err_message()
            .contains("容量")
    );
    assert!(!f.path(&request).exists());
    assert_eq!(fs::read_dir(f.storage()).unwrap().count(), MAX_RECORDS + 1);
}

#[test]
fn strict_record_identity_and_version_validation() {
    let f = Fixture::new();
    let request = f.request();
    let record = Record::for_request(&request).unwrap();
    let encoded = serde_json::to_value(&record).unwrap();
    for (field, value) in [
        ("version", serde_json::json!(2)),
        (
            "operation_id",
            serde_json::json!(uuid::Uuid::new_v4().to_string()),
        ),
        ("review_id", serde_json::json!("../escape")),
        ("request_sha256", serde_json::json!("a".repeat(63))),
        ("request_sha256", serde_json::json!("A".repeat(64))),
        ("registered_at_ms", serde_json::json!(0)),
    ] {
        let mut bad = encoded.clone();
        bad[field] = value;
        assert!(
            Record::decode(&serde_json::to_vec(&bad).unwrap(), &record.operation_id).is_err(),
            "{field}"
        );
    }
    let bytes = serde_json::to_vec(&record).unwrap();
    assert_eq!(
        Record::decode(&bytes, &record.operation_id).unwrap(),
        record
    );
}

#[test]
fn duplicated_historical_review_and_nonempty_lock_do_not_get_repaired() {
    let f = Fixture::new();
    let first = f.request();
    let second = changed(
        &first,
        "operation_id",
        uuid::Uuid::new_v4().to_string().into(),
    );
    f.seed(&first);
    f.seed(&second);
    assert!(
        register(&f.0, &f.request())
            .unwrap_err_message()
            .contains("身份重复")
    );
    let g = Fixture::new();
    fs::create_dir(g.storage()).unwrap();
    fs::write(g.storage().join(LOCK), b"unexpected lock content").unwrap();
    assert!(register(&g.0, &g.request()).is_err());
    assert_eq!(
        fs::read(g.storage().join(LOCK)).unwrap(),
        b"unexpected lock content"
    );
}

#[test]
fn expired_requests_and_missing_app_directory_create_nothing() {
    let f = Fixture::new();
    let request = f.request();
    assert!(register(&f.0.join("missing-app"), &request).is_err());
    assert!(!f.0.join("missing-app").exists());
    let mut wire: Request =
        serde_json::from_slice(&request.encode(now().unwrap()).unwrap()).unwrap();
    wire.issued_at_ms = 1;
    wire.expires_at_ms = 60_001;
    let expired = CheckedRequest::from_local(wire, 1).unwrap();
    assert!(register(&f.0, &expired).is_err());
    assert!(!f.storage().exists());
}

#[test]
fn record_and_lock_aliases_are_rejected_and_never_written() {
    let f = Fixture::new();
    let request = f.request();
    let mut held = register(&f.0, &request).unwrap();
    fs::hard_link(f.path(&request), f.0.join("record-alias")).unwrap();
    assert!(held.revalidate(&request).is_err());
    drop(held);
    assert!(register(&f.0, &f.request()).is_err());
    let g = Fixture::new();
    fs::create_dir(g.storage()).unwrap();
    fs::write(g.0.join("outside-lock"), b"").unwrap();
    fs::hard_link(g.0.join("outside-lock"), g.storage().join(LOCK)).unwrap();
    assert!(register(&g.0, &g.request()).is_err());
    assert!(fs::read(g.0.join("outside-lock")).unwrap().is_empty());
}

#[test]
fn duplicate_registration_reports_prepared_and_unknown_started_states() {
    for started in [false, true] {
        let f = Fixture::new();
        let (request, _, inspection) = f.stage_request();
        let held = register(&f.0, &request).unwrap();
        let mut stage =
            super::super::execution_journal::prepare(&f.0, &request, &inspection).unwrap();
        if started {
            let metadata = Metadata::thaw(&inspection.original.metadata).unwrap();
            let evidence = super::super::security_inspection::CandidateEvidence::fixture(
                &request,
                sha256(&inspection.original.security),
                metadata.candidate_review_fingerprint().unwrap(),
            )
            .unwrap();
            stage.bind_candidate(&request, &evidence).unwrap();
            stage
                .bind_recovery(
                    &request,
                    &super::super::recovery_record::RecoveryRecordEvidence::fixture(&request),
                )
                .unwrap();
            stage.mark_started(&request).unwrap();
        }
        drop(stage);
        drop(held);
        let error = register(&f.0, &request).unwrap_err_message();
        if started {
            assert!(error.contains("结果未知"));
        } else {
            assert!(error.contains("完整持久准备"));
        }
    }
}

struct OwnedChild(Child);
impl Drop for OwnedChild {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
#[test]
#[ignore = "owned ordinary child executed by interrupted_process_keeps_attempt_consumed"]
fn attempt_process_fixture() {
    let root = PathBuf::from(std::env::var("OPC_ATTEMPT_TEST_ROOT").unwrap());
    let bytes = fs::read(root.join("fixture-request.json")).unwrap();
    let request = CheckedRequest::decode(&bytes, now().unwrap()).unwrap();
    let _held = register(&root, &request).unwrap();
    println!("ATTEMPT_SYNCED");
    std::io::stdout().flush().unwrap();
    // Parent terminates this exact child after observing the flushed marker.
    let mut line = String::new();
    std::io::stdin().read_line(&mut line).unwrap();
}
#[test]
fn interrupted_process_keeps_attempt_consumed() {
    let f = Fixture::new();
    let request = f.request();
    fs::write(
        f.0.join("fixture-request.json"),
        request.encode(now().unwrap()).unwrap(),
    )
    .unwrap();
    let mut child = OwnedChild(
        Command::new(std::env::current_exe().unwrap())
            .args([
                "--exact",
                "helper::attempt_journal::tests::attempt_process_fixture",
                "--ignored",
                "--nocapture",
            ])
            .env("OPC_ATTEMPT_TEST_ROOT", &f.0)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()
            .unwrap(),
    );
    let stdout = child.0.stdout.take().unwrap();
    let (tx, rx) = std::sync::mpsc::channel();
    let reader = std::thread::spawn(move || {
        for line in BufReader::new(stdout).lines() {
            if line.unwrap() == "ATTEMPT_SYNCED" {
                let _ = tx.send(());
                break;
            }
        }
    });
    let ready = rx.recv_timeout(std::time::Duration::from_secs(15));
    child.0.kill().unwrap();
    child.0.wait().unwrap();
    reader.join().unwrap();
    ready.unwrap();
    assert!(
        register(&f.0, &request)
            .unwrap_err_message()
            .contains("已登记")
    );
    // A fresh request can proceed after the OS has released the dead lock owner.
    drop(register(&f.0, &f.request()).unwrap());
}
