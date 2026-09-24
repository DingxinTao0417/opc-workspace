use super::*;
use crate::{
    file_operation_contract::{Content, ObservedFile},
    file_operation_read::{PinnedDirectory, read_regular},
    file_operation_security::SecurityDescriptor,
};
use std::{fs, fs::OpenOptions, path::PathBuf};

fn minimal_descriptor() -> Vec<u8> {
    // Valid self-relative descriptor with an empty, present DACL. It is only a
    // serialization fixture; no test applies it to a file or enters privilege.
    vec![
        1, 0, 4, 128, // revision, control: SELF_RELATIVE | DACL_PRESENT
        0, 0, 0, 0, // owner
        0, 0, 0, 0, // group
        0, 0, 0, 0, // SACL
        20, 0, 0, 0, // DACL offset
        2, 0, 8, 0, 0, 0, 0, 0, // empty ACL
    ]
}

struct Fixture {
    dir: PathBuf,
    request: CheckedRequest,
    inspection: FrozenSecurityInspection,
    attempt: AttemptIdentity,
}
impl Fixture {
    fn new(lifetime_ms: u64) -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-helper-stage-test-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&dir).unwrap();
        let path = dir.join("private-project-file.txt");
        fs::write(&path, b"private original text").unwrap();
        let mut file = File::open(&path).unwrap();
        let (content, identity, modified) = read_regular(&mut file, 1024).unwrap();
        let metadata = Metadata::capture(&file, &content).unwrap();
        let root_identity = PinnedDirectory::open(&dir, false).unwrap().identity;
        let partial_security = SecurityDescriptor::capture(&file).unwrap();
        let at = now().unwrap();
        let request = CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: uuid::Uuid::new_v4().to_string(),
                review_id: uuid::Uuid::new_v4().to_string(),
                root_selection_id: uuid::Uuid::new_v4().to_string(),
                issued_at_ms: at,
                expires_at_ms: at + lifetime_ms,
                root: dir.to_str().unwrap().into(),
                root_identity: root_identity.clone(),
                parent_identity: root_identity,
                path: "private-project-file.txt".into(),
                operation: Operation::Replace {
                    original: ObservedFile {
                        identity,
                        modified,
                        content: Content::from_bytes(&content).unwrap(),
                        readable_security_sha256: sha256(partial_security.bytes()),
                        metadata_sha256: metadata.review_fingerprint().unwrap(),
                    },
                    candidate: Content::from_bytes(b"private candidate text").unwrap(),
                },
            },
            at,
        )
        .unwrap();
        let request_sha256 = sha256(&request.encode(at).unwrap());
        let attempt = AttemptIdentity {
            operation_id: request.facts().operation_id.clone(),
            review_id: request.facts().review_id.clone(),
            request_sha256: request_sha256.clone(),
        };
        let inspection = FrozenSecurityInspection {
            request_sha256,
            original: FrozenFileObservation {
                security: minimal_descriptor(),
                metadata: metadata.freeze().unwrap(),
            },
            candidate: None,
        };
        Self {
            dir,
            request,
            inspection,
            attempt,
        }
    }
    fn storage(&self) -> PathBuf {
        self.dir.join(DIRECTORY)
    }
    fn prepared_path(&self) -> PathBuf {
        self.storage()
            .join(format!("{}.prepared.json", self.attempt.operation_id))
    }
    fn started_path(&self) -> PathBuf {
        self.storage()
            .join(format!("{}.started.json", self.attempt.operation_id))
    }
    fn materialized_path(&self) -> PathBuf {
        self.storage()
            .join(format!("{}.materialized.json", self.attempt.operation_id))
    }
    fn recovery_path(&self) -> PathBuf {
        self.storage()
            .join(format!("{}.recovery.json", self.attempt.operation_id))
    }
    fn outcome_path(&self) -> PathBuf {
        self.storage()
            .join(format!("{}.outcome.json", self.attempt.operation_id))
    }
    fn evidence(&self) -> CandidateEvidence {
        let metadata = Metadata::thaw(&self.inspection.original.metadata).unwrap();
        CandidateEvidence::fixture(
            &self.request,
            sha256(&self.inspection.original.security),
            metadata.candidate_review_fingerprint().unwrap(),
        )
        .unwrap()
    }
    fn recovery_evidence(&self) -> RecoveryRecordEvidence {
        RecoveryRecordEvidence::fixture(&self.request)
    }
    fn outcome_evidence(&self) -> ExecutionOutcomeEvidence {
        let original_observed = match &self.request.facts().operation {
            Operation::Replace { original, .. }
            | Operation::RestoreMissing { original, .. }
            | Operation::UndoInstalled { original, .. } => original,
        };
        let original = OutcomeFile {
            identity: original_observed.identity.clone(),
            content_sha256: original_observed.content_digest().into(),
            metadata_sha256: original_observed.metadata_sha256.clone(),
            readable_security_sha256: original_observed.readable_security_sha256.clone(),
            full_security_sha256: sha256(&self.inspection.original.security),
        };
        let candidate = match &self.request.facts().operation {
            Operation::Replace { .. } => {
                let evidence = self.evidence();
                OutcomeFile {
                    identity: evidence.identity().clone(),
                    content_sha256: evidence.content_sha256().into(),
                    metadata_sha256: evidence.metadata_sha256().into(),
                    readable_security_sha256: evidence.readable_security_sha256().into(),
                    full_security_sha256: evidence.full_security_sha256().into(),
                }
            }
            Operation::RestoreMissing { candidate, .. }
            | Operation::UndoInstalled { candidate, .. } => OutcomeFile {
                identity: candidate.identity.clone(),
                content_sha256: candidate.content_digest().into(),
                metadata_sha256: candidate.metadata_sha256.clone(),
                readable_security_sha256: candidate.readable_security_sha256.clone(),
                full_security_sha256: sha256(&self.inspection.candidate.as_ref().unwrap().security),
            },
        };
        match &self.request.facts().operation {
            Operation::Replace { .. } => ExecutionOutcomeEvidence {
                mode: "replace",
                target: candidate,
                retained: original,
            },
            Operation::RestoreMissing { .. } => ExecutionOutcomeEvidence {
                mode: "restore_missing",
                target: original,
                retained: candidate,
            },
            Operation::UndoInstalled { .. } => ExecutionOutcomeEvidence {
                mode: "undo_installed",
                target: original,
                retained: candidate,
            },
        }
    }
    fn recovery() -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-helper-stage-test-{}", uuid::Uuid::new_v4()));
        fs::create_dir(&dir).unwrap();
        let original_path = dir.join("original.txt");
        let candidate_path = dir.join("candidate.txt");
        fs::write(&original_path, b"private original bytes").unwrap();
        fs::write(&candidate_path, b"private candidate bytes").unwrap();
        let mut original = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&original_path)
            .unwrap();
        let mut candidate = OpenOptions::new()
            .read(true)
            .write(true)
            .open(&candidate_path)
            .unwrap();
        let (original_bytes, original_identity, original_modified) =
            read_regular(&mut original, 1024).unwrap();
        let metadata = Metadata::capture(&original, &original_bytes).unwrap();
        metadata
            .apply(&candidate, b"private candidate bytes")
            .unwrap();
        let (candidate_bytes, candidate_identity, candidate_modified) =
            read_regular(&mut candidate, 1024).unwrap();
        let candidate_metadata = Metadata::capture(&candidate, &candidate_bytes).unwrap();
        let root_identity = PinnedDirectory::open(&dir, false).unwrap().identity;
        let at = now().unwrap();
        let request = CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: uuid::Uuid::new_v4().to_string(),
                review_id: uuid::Uuid::new_v4().to_string(),
                root_selection_id: uuid::Uuid::new_v4().to_string(),
                issued_at_ms: at,
                expires_at_ms: at + 60_000,
                root: dir.to_str().unwrap().into(),
                root_identity: root_identity.clone(),
                parent_identity: root_identity,
                path: "target.txt".into(),
                operation: Operation::RestoreMissing {
                    record_version: 2,
                    record_id: uuid::Uuid::new_v4().to_string(),
                    record_sha256: "0".repeat(64),
                    original: ObservedFile {
                        identity: original_identity,
                        modified: original_modified,
                        content: Content::from_bytes(&original_bytes).unwrap(),
                        readable_security_sha256: sha256(
                            SecurityDescriptor::capture(&original).unwrap().bytes(),
                        ),
                        metadata_sha256: metadata.review_fingerprint().unwrap(),
                    },
                    candidate: ObservedFile {
                        identity: candidate_identity,
                        modified: candidate_modified,
                        content: Content::from_bytes(&candidate_bytes).unwrap(),
                        readable_security_sha256: sha256(
                            SecurityDescriptor::capture(&candidate).unwrap().bytes(),
                        ),
                        metadata_sha256: candidate_metadata.review_fingerprint().unwrap(),
                    },
                },
            },
            at,
        )
        .unwrap();
        let request_sha256 = sha256(&request.encode(at).unwrap());
        let attempt = AttemptIdentity {
            operation_id: request.facts().operation_id.clone(),
            review_id: request.facts().review_id.clone(),
            request_sha256: request_sha256.clone(),
        };
        let descriptor = minimal_descriptor();
        let inspection = FrozenSecurityInspection {
            request_sha256,
            original: FrozenFileObservation {
                security: descriptor.clone(),
                metadata: metadata.freeze().unwrap(),
            },
            candidate: Some(FrozenFileObservation {
                security: descriptor,
                metadata: candidate_metadata.freeze().unwrap(),
            }),
        };
        Self {
            dir,
            request,
            inspection,
            attempt,
        }
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(
            self.dir.is_absolute() && self.dir.parent() == Some(std::env::temp_dir().as_path())
        );
        assert!(
            self.dir
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-helper-stage-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn prepared_record_freezes_complete_request_metadata_and_security() {
    let f = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    stage.revalidate(&f.request).unwrap();
    assert!(fs::write(f.prepared_path(), b"overwrite").is_err());
    assert!(fs::rename(f.storage(), f.dir.join("moved")).is_err());
    drop(stage);
    let bytes = fs::read(f.prepared_path()).unwrap();
    let stored = StoredPrepared::decode(&bytes, &f.attempt).unwrap();
    let request = stored.prepared.validate(Some(&f.attempt)).unwrap();
    assert_eq!(request.path, "private-project-file.txt");
    assert_eq!(
        decode_base64(
            &stored.prepared.original.security_base64,
            64 * 1024,
            "fixture"
        )
        .unwrap(),
        minimal_descriptor()
    );
    assert!(
        !bytes
            .windows(b"private original text".len())
            .any(|v| v == b"private original text")
    );
}

#[test]
fn reopen_distinguishes_prepared_from_started_unknown() {
    let f = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    assert!(
        stage
            .mark_started(&f.request)
            .unwrap_err()
            .contains("尚未实体化")
    );
    drop(stage);
    let mut reopened = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&f.attempt.operation_id),
        Some(&StageState::Prepared)
    );
    reopened.revalidate().unwrap();
    drop(reopened);

    let stage = prepare(&f.dir, &f.request, &f.inspection);
    assert!(stage.is_err()); // CREATE_NEW never overwrites the durable preparation.
    // Reopen the existing record through the validator, then simulate the exact
    // start transition using a newly prepared fixture instead.
    let g = Fixture::new(60_000);
    let mut started = prepare(&g.dir, &g.request, &g.inspection).unwrap();
    started.bind_candidate(&g.request, &g.evidence()).unwrap();
    drop(started);
    let reopened = validate_store(&g.dir, std::slice::from_ref(&g.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&g.attempt.operation_id),
        Some(&StageState::Materialized)
    );
    drop(reopened);
    // Reopen is read-only; the live test object performs the start transition
    // on a fresh fixture to keep CREATE_NEW semantics explicit.
    let g = Fixture::new(60_000);
    let mut started = prepare(&g.dir, &g.request, &g.inspection).unwrap();
    started.bind_candidate(&g.request, &g.evidence()).unwrap();
    started
        .bind_recovery(&g.request, &g.recovery_evidence())
        .unwrap();
    drop(started);
    let reopened = validate_store(&g.dir, std::slice::from_ref(&g.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&g.attempt.operation_id),
        Some(&StageState::RecoveryIntentBound)
    );
    drop(reopened);
    // Reopen is read-only; the live test object performs the start transition
    // on a fresh fixture to keep CREATE_NEW semantics explicit.
    let g = Fixture::new(60_000);
    let mut started = prepare(&g.dir, &g.request, &g.inspection).unwrap();
    started.bind_candidate(&g.request, &g.evidence()).unwrap();
    started
        .bind_recovery(&g.request, &g.recovery_evidence())
        .unwrap();
    started.mark_started(&g.request).unwrap();
    assert!(fs::write(g.started_path(), b"overwrite").is_err());
    drop(started);
    let mut reopened = validate_store(&g.dir, std::slice::from_ref(&g.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&g.attempt.operation_id),
        Some(&StageState::UnknownAfterStart)
    );
    reopened.revalidate().unwrap();
}

#[test]
fn verified_success_is_terminal_and_binds_the_complete_expected_layout() {
    let f = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    stage.bind_candidate(&f.request, &f.evidence()).unwrap();
    stage
        .bind_recovery(&f.request, &f.recovery_evidence())
        .unwrap();
    stage.mark_started(&f.request).unwrap();
    let mut wrong = f.outcome_evidence();
    wrong.target.identity.index = wrong.target.identity.index.saturating_add(1);
    assert!(stage.mark_succeeded(&f.request, &wrong).is_err());
    assert!(!f.outcome_path().exists());
    stage
        .mark_succeeded(&f.request, &f.outcome_evidence())
        .unwrap();
    assert!(fs::write(f.outcome_path(), b"overwrite").is_err());
    assert!(
        stage
            .mark_succeeded(&f.request, &f.outcome_evidence())
            .is_err()
    );
    drop(stage);
    let mut reopened = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&f.attempt.operation_id),
        Some(&StageState::Succeeded)
    );
    reopened.revalidate().unwrap();
}

#[test]
fn outcome_can_close_an_already_started_operation_after_approval_expiry() {
    let mut f = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    stage.bind_candidate(&f.request, &f.evidence()).unwrap();
    stage
        .bind_recovery(&f.request, &f.recovery_evidence())
        .unwrap();
    stage.mark_started(&f.request).unwrap();
    f.request.expire_for_test();
    assert!(f.request.check_time(now().unwrap()).is_err());
    stage
        .mark_succeeded(&f.request, &f.outcome_evidence())
        .unwrap();
}

#[test]
fn persisted_preparation_remains_inspectable_after_approval_expires() {
    let mut f = Fixture::new(60_000);
    let stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    drop(stage);
    f.request.expire_for_test();
    assert!(f.request.check_time(now().unwrap()).is_err());
    let reopened = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&f.attempt.operation_id),
        Some(&StageState::Prepared)
    );
    assert!(!f.started_path().exists());
}

#[test]
fn start_requires_live_request_and_never_reuses_marker() {
    let prerequisite = Fixture::new(60_000);
    let mut incomplete = prepare(
        &prerequisite.dir,
        &prerequisite.request,
        &prerequisite.inspection,
    )
    .unwrap();
    incomplete
        .bind_candidate(&prerequisite.request, &prerequisite.evidence())
        .unwrap();
    assert!(
        incomplete
            .mark_started(&prerequisite.request)
            .unwrap_err()
            .contains("恢复意图")
    );
    assert!(!prerequisite.started_path().exists());

    let mut f = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    stage.bind_candidate(&f.request, &f.evidence()).unwrap();
    stage
        .bind_recovery(&f.request, &f.recovery_evidence())
        .unwrap();
    f.request.expire_for_test();
    assert!(f.request.check_time(now().unwrap()).is_err());
    assert!(stage.mark_started(&f.request).is_err());
    assert!(!f.started_path().exists());

    let g = Fixture::new(60_000);
    let mut stage = prepare(&g.dir, &g.request, &g.inspection).unwrap();
    stage.bind_candidate(&g.request, &g.evidence()).unwrap();
    stage
        .bind_recovery(&g.request, &g.recovery_evidence())
        .unwrap();
    stage.mark_started(&g.request).unwrap();
    assert!(
        stage
            .mark_started(&g.request)
            .unwrap_err()
            .contains("结果未知")
    );
    drop(stage);
    let before = fs::read(g.started_path()).unwrap();
    assert_eq!(fs::read(g.started_path()).unwrap(), before);
}

#[test]
fn orphan_unknown_and_tampered_stages_fail_closed_without_repair() {
    for mode in [
        "orphan",
        "unknown",
        "prepared_tamper",
        "materialized_tamper",
        "recovery_tamper",
        "started_tamper",
        "outcome_tamper",
    ] {
        let f = Fixture::new(60_000);
        let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
        if matches!(
            mode,
            "materialized_tamper" | "recovery_tamper" | "started_tamper" | "outcome_tamper"
        ) {
            stage.bind_candidate(&f.request, &f.evidence()).unwrap();
        }
        if matches!(
            mode,
            "recovery_tamper" | "started_tamper" | "outcome_tamper"
        ) {
            stage
                .bind_recovery(&f.request, &f.recovery_evidence())
                .unwrap();
        }
        if matches!(mode, "started_tamper" | "outcome_tamper") {
            stage.mark_started(&f.request).unwrap();
        }
        if mode == "outcome_tamper" {
            stage
                .mark_succeeded(&f.request, &f.outcome_evidence())
                .unwrap();
        }
        drop(stage);
        let path = match mode {
            "unknown" => {
                let path = f.storage().join("unexpected");
                fs::write(&path, b"preserve").unwrap();
                path
            }
            "prepared_tamper" => {
                let path = f.prepared_path();
                let mut value: serde_json::Value =
                    serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
                value["digest"] = serde_json::Value::String("0".repeat(64));
                fs::write(&path, serde_json::to_vec(&value).unwrap()).unwrap();
                path
            }
            "started_tamper" => {
                let path = f.started_path();
                fs::write(&path, b"{\"version\":1}").unwrap();
                path
            }
            "materialized_tamper" => {
                let path = f.materialized_path();
                fs::write(&path, b"{\"version\":1}").unwrap();
                path
            }
            "recovery_tamper" => {
                let path = f.recovery_path();
                fs::write(&path, b"{\"version\":1}").unwrap();
                path
            }
            "outcome_tamper" => {
                let path = f.outcome_path();
                fs::write(&path, b"{\"version\":1}").unwrap();
                path
            }
            "orphan" => f.prepared_path(),
            _ => unreachable!(),
        };
        let before = fs::read(&path).unwrap();
        let attempts = if mode == "orphan" {
            &[][..]
        } else {
            std::slice::from_ref(&f.attempt)
        };
        assert!(validate_store(&f.dir, attempts).is_err(), "{mode}");
        assert_eq!(fs::read(&path).unwrap(), before);
    }
}

#[test]
fn prepared_candidate_shape_and_attempt_binding_are_strict() {
    let f = Fixture::new(60_000);
    let stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    drop(stage);
    let original = fs::read(f.prepared_path()).unwrap();
    for field in ["operation_id", "review_id", "request_sha256"] {
        let mut value: serde_json::Value = serde_json::from_slice(&original).unwrap();
        value["prepared"][field] = serde_json::Value::String(if field == "request_sha256" {
            "0".repeat(64)
        } else {
            uuid::Uuid::new_v4().to_string()
        });
        fs::write(f.prepared_path(), serde_json::to_vec(&value).unwrap()).unwrap();
        assert!(
            validate_store(&f.dir, std::slice::from_ref(&f.attempt)).is_err(),
            "{field}"
        );
        fs::write(f.prepared_path(), &original).unwrap();
    }
    let mut value: serde_json::Value = serde_json::from_slice(&original).unwrap();
    value["prepared"]["candidate"] = value["prepared"]["original"].clone();
    fs::write(f.prepared_path(), serde_json::to_vec(&value).unwrap()).unwrap();
    assert!(validate_store(&f.dir, std::slice::from_ref(&f.attempt)).is_err());
}

#[test]
fn new_candidate_binding_is_exact_single_use_and_required_before_start() {
    let f = Fixture::new(60_000);
    let other = Fixture::new(60_000);
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    assert!(stage.bind_candidate(&f.request, &other.evidence()).is_err());
    assert!(!f.materialized_path().exists());
    stage.bind_candidate(&f.request, &f.evidence()).unwrap();
    let before = {
        drop(stage);
        fs::read(f.materialized_path()).unwrap()
    };
    let mut reopened = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&f.attempt.operation_id),
        Some(&StageState::Materialized)
    );
    reopened.revalidate().unwrap();
    assert_eq!(fs::read(f.materialized_path()).unwrap(), before);
}

#[test]
fn recovery_pair_is_materialized_from_the_same_complete_observation() {
    let f = Fixture::recovery();
    let mut stage = prepare(&f.dir, &f.request, &f.inspection).unwrap();
    stage.revalidate(&f.request).unwrap();
    assert!(f.materialized_path().exists());
    stage
        .bind_recovery(&f.request, &f.recovery_evidence())
        .unwrap();
    stage.mark_started(&f.request).unwrap();
    drop(stage);
    let reopened = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert_eq!(
        reopened.states.get(&f.attempt.operation_id),
        Some(&StageState::UnknownAfterStart)
    );
}

#[test]
fn missing_stage_store_is_read_only_and_capacity_is_bounded() {
    let f = Fixture::new(60_000);
    let empty = validate_store(&f.dir, std::slice::from_ref(&f.attempt)).unwrap();
    assert!(empty.states.is_empty() && !f.storage().exists());
    drop(empty);
    fs::create_dir(f.storage()).unwrap();
    for _ in 0..=MAX_OPERATIONS * 5 {
        fs::write(
            f.storage()
                .join(format!("{}.prepared.json", uuid::Uuid::new_v4())),
            b"invalid",
        )
        .unwrap();
    }
    assert!(validate_store(&f.dir, std::slice::from_ref(&f.attempt)).is_err());
    assert_eq!(
        fs::read_dir(f.storage()).unwrap().count(),
        MAX_OPERATIONS * 5 + 1
    );
}
