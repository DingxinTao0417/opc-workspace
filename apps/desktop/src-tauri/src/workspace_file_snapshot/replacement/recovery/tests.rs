use super::*;
use crate::workspace_file_snapshot::replacement::Prepared;
use std::{
    fs,
    io::{BufRead, BufReader, Read, Write},
    process::{Child, Command, Stdio},
    sync::mpsc,
    thread,
};

#[test]
fn native_recovery_review_freezes_exact_mode_record_and_objects_without_moves() {
    use crate::file_operation_contract::{CheckedRequest, Operation};
    for installed in [false, true] {
        let f = Fixture::new(installed);
        let state = RecoveryReviewState::default();
        let choice = uuid::Uuid::new_v4().to_string();
        let mode = if installed {
            "undo_installed"
        } else {
            "missing_target"
        };
        let review = state
            .capture(&f.storage, &choice, &f.root, &f.intent.id, mode)
            .unwrap();
        let request = state
            .freeze_request(
                &choice,
                &f.root,
                &review.review_id,
                &uuid::Uuid::new_v4().to_string(),
            )
            .unwrap();
        let bytes = request
            .encode(super::super::request_bridge::now_ms().unwrap())
            .unwrap();
        let decoded =
            CheckedRequest::decode(&bytes, super::super::request_bridge::now_ms().unwrap())
                .unwrap();
        let (record, original, candidate) = match &decoded.facts().operation {
            Operation::RestoreMissing {
                record_id,
                original,
                candidate,
                ..
            } if !installed => (record_id, original, candidate),
            Operation::UndoInstalled {
                record_id,
                original,
                candidate,
                ..
            } if installed => (record_id, original, candidate),
            _ => panic!("operation must not be inferred or changed"),
        };
        assert_eq!(record, &f.intent.id);
        assert_eq!(original.identity.index, f.intent.source_identity.index);
        assert_eq!(candidate.identity.index, f.intent.candidate_identity.index);
        let mut disk = crate::file_operation_disk::DiskReview::inspect(&decoded).unwrap();
        disk.revalidate().unwrap();
        drop(disk);
        assert!(
            state
                .freeze_request(
                    &choice,
                    &f.root,
                    &review.review_id,
                    &uuid::Uuid::new_v4().to_string()
                )
                .is_err()
        );
        assert_eq!(f.intent.target().exists(), installed);
        assert_eq!(
            fs::read(f.intent.sibling("original")).unwrap(),
            f.intent.before
        );
        assert_eq!(fs::read_dir(&f.storage).unwrap().count(), 1);
    }
}

#[test]
fn recovery_bridge_refuses_post_review_drift_without_reusing_ticket() {
    let f = Fixture::new(true);
    let state = RecoveryReviewState::default();
    let choice = uuid::Uuid::new_v4().to_string();
    let review = state
        .capture(&f.storage, &choice, &f.root, &f.intent.id, "undo_installed")
        .unwrap();
    fs::write(f.intent.target(), b"user's newer candidate").unwrap();
    assert!(
        state
            .freeze_request(
                &choice,
                &f.root,
                &review.review_id,
                &uuid::Uuid::new_v4().to_string()
            )
            .is_err()
    );
    assert!(state.consume(&choice, &review.review_id).is_err());
    assert_eq!(
        fs::read(f.intent.target()).unwrap(),
        b"user's newer candidate"
    );
    assert_eq!(
        fs::read(f.intent.sibling("original")).unwrap(),
        f.intent.before
    );
}

#[test]
fn review_ticket_is_bounded_root_bound_and_single_use() {
    let f = Fixture::new(true);
    let state = RecoveryReviewState::default();
    let first = state
        .capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            "undo_installed",
        )
        .unwrap();
    assert_eq!(
        first.preview.current_base64,
        Some(STANDARD.encode(&f.intent.after))
    );
    assert_eq!(first.expires_in_seconds, 600);
    assert!(state.release("other-choice", &first.review_id).is_err());
    for _ in 1..MAX_SNAPSHOTS {
        state
            .capture(
                &f.storage,
                "root-choice",
                &f.root,
                &f.intent.id,
                "undo_installed",
            )
            .unwrap();
    }
    assert!(
        state
            .capture(
                &f.storage,
                "root-choice",
                &f.root,
                &f.intent.id,
                "undo_installed"
            )
            .is_err()
    );
    let held = state.consume("root-choice", &first.review_id).unwrap();
    assert!(state.consume("root-choice", &first.review_id).is_err());
    drop(held);
    state
        .capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            "undo_installed",
        )
        .unwrap();
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
    assert!(!f.intent.sibling("candidate").exists());
}

#[test]
fn tickets_release_expire_and_consume_even_a_wrong_selection_attempt() {
    let f = Fixture::new(false);
    let state = RecoveryReviewState::default();
    let first = state
        .capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            "missing_target",
        )
        .unwrap();
    assert!(first.preview.current_base64.is_none());
    state.release("root-choice", &first.review_id).unwrap();
    state.release("root-choice", &first.review_id).unwrap();
    assert!(state.consume("root-choice", &first.review_id).is_err());
    let next = state
        .capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            "missing_target",
        )
        .unwrap();
    assert!(state.consume("wrong-choice", &next.review_id).is_err());
    assert!(state.consume("root-choice", &next.review_id).is_err());
    let expired = state
        .capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            "missing_target",
        )
        .unwrap();
    state
        .0
        .lock()
        .unwrap()
        .get_mut(&expired.review_id)
        .unwrap()
        .created = Instant::now() - TTL;
    assert!(state.consume("root-choice", &expired.review_id).is_err());
    assert!(
        state
            .capture(
                &f.storage,
                "root-choice",
                &f.root,
                &f.intent.id,
                "automatic"
            )
            .is_err()
    );
    assert!(state.0.lock().unwrap().is_empty());
    assert!(!f.intent.target().exists());
}

// Not standalone acceptance: the parent test below launches this exact helper
// with an isolated fixture and force-terminates its owned process handle.
#[test]
#[ignore = "subprocess fixture, invoked by process_termination_preserves_recoverable_objects"]
fn process_recovery_fixture() {
    let dir = PathBuf::from(std::env::var_os("OPC_TEST_RECOVERY_DIR").expect("fixture directory"));
    assert!(dir.is_absolute() && dir.parent() == Some(std::env::temp_dir().as_path()));
    let suffix = dir
        .file_name()
        .unwrap()
        .to_str()
        .unwrap()
        .strip_prefix("opc-file-recovery-test-")
        .unwrap();
    assert!(uuid::Uuid::parse_str(suffix).is_ok_and(|id| id.to_string() == suffix));
    let id = std::env::var("OPC_TEST_RECOVERY_ID").expect("fixture identity");
    let root = dir.join("project");
    let vacant = RecoveryReview::capture(
        &dir.join("recovery"),
        "process-choice",
        &root,
        &id,
        RecoveryMode::UndoInstalled,
    )
    .unwrap()
    .prepare("process-choice", &root)
    .unwrap()
    .park_current()
    .unwrap();
    println!("OPC_RECOVERY_FIXTURE_READY");
    std::io::stdout().flush().unwrap();
    // Keep all file/journal handles alive; parent kills this specific child.
    let _ = std::io::stdin().read(&mut [0u8; 1]);
    drop(vacant);
}

struct OwnedChild(Child);
impl Drop for OwnedChild {
    fn drop(&mut self) {
        // Only the child returned by Command::spawn, never a searched PID.
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
#[test]
fn process_termination_preserves_recoverable_objects() {
    let f = Fixture::new(true);
    let helper = "workspace_file_snapshot::replacement_recovery::tests::process_recovery_fixture";
    let mut child = OwnedChild(
        Command::new(std::env::current_exe().unwrap())
            .args(["--exact", helper, "--ignored", "--nocapture"])
            .env("OPC_TEST_RECOVERY_DIR", &f.dir)
            .env("OPC_TEST_RECOVERY_ID", &f.intent.id)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap(),
    );
    let stdout = child.0.stdout.take().unwrap();
    let (send, receive) = mpsc::channel();
    let reader = thread::spawn(move || {
        let ready = BufReader::new(stdout)
            .lines()
            .any(|line| line.is_ok_and(|text| text.trim() == "OPC_RECOVERY_FIXTURE_READY"));
        let _ = send.send(ready);
    });
    let ready = receive.recv_timeout(Duration::from_secs(20));
    // Terminate before checking readiness so error paths cannot leak a process.
    let killed = child.0.kill();
    let exit = child.0.wait();
    reader.join().unwrap();
    assert!(
        matches!(ready, Ok(true)),
        "child did not reach the held recovery gap: {ready:?}"
    );
    killed.unwrap();
    assert!(!exit.unwrap().success());
    assert!(!f.intent.target().exists());
    assert_eq!(
        fs::read(f.intent.sibling("original")).unwrap(),
        f.intent.before
    );
    assert_eq!(
        fs::read(f.intent.sibling("candidate")).unwrap(),
        f.intent.after
    );
    assert_eq!(
        inspect(&f.storage, &f.intent.id, &f.root)
            .unwrap()
            .observation,
        "target_missing"
    );
    // New review, newly opened objects, no reuse of the killed process's grant.
    f.finish(f.review(RecoveryMode::MissingTarget)).unwrap();
    assert_eq!(
        read_exact(&f.root, &f.intent.path).unwrap().file_identity,
        f.intent.source_identity
    );
}
use crate::workspace_file_snapshot::replacement_journal::preview as inspect;

struct Fixture {
    dir: PathBuf,
    root: PathBuf,
    storage: PathBuf,
    intent: Intent,
}
impl Fixture {
    fn new(installed: bool) -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-file-recovery-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        let storage = dir.join("recovery");
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join("file.txt"), b"original\r\n").unwrap();
        fs::write(
            format!("{}:Zone.Identifier", root.join("file.txt").display()),
            b"[ZoneTransfer]\r\nZoneId=3\r\n",
        )
        .unwrap();
        let baseline = read_exact(&root, "file.txt").unwrap();
        let prepared = Prepared::new(&root, "file.txt", &baseline, b"candidate\n").unwrap();
        let intent = prepared.intent.clone();
        let parked = prepared.record(&storage).unwrap().park().unwrap();
        if installed {
            parked.install().unwrap();
        } else {
            drop(parked);
        }
        Self {
            dir,
            root,
            storage,
            intent,
        }
    }
    fn review(&self, mode: RecoveryMode) -> RecoveryReview {
        RecoveryReview::capture(
            &self.storage,
            "root-choice",
            &self.root,
            &self.intent.id,
            mode,
        )
        .unwrap()
    }
    fn finish(&self, review: RecoveryReview) -> Result<(), String> {
        review
            .prepare("root-choice", &self.root)?
            .park_current()?
            .restore()
    }
    fn journal(&self) -> PathBuf {
        self.storage.join(format!("{}.json", self.intent.id))
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(self.dir.is_absolute() && self.dir.starts_with(std::env::temp_dir()));
        assert!(
            self.dir
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-file-recovery-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn undo_restores_original_identity_and_retains_candidate_and_streams() {
    let f = Fixture::new(true);
    let journal_before = fs::read(f.journal()).unwrap();
    let review = f.review(RecoveryMode::UndoInstalled);
    // Capturing a review neither moves objects nor leaves cross-click locks.
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
    assert!(!f.intent.sibling("candidate").exists());
    f.finish(review).unwrap();
    let exact = read_exact(&f.root, &f.intent.path).unwrap();
    assert_eq!(exact.file_identity, f.intent.source_identity);
    assert_eq!(exact.content, f.intent.before);
    assert_eq!(
        fs::read(f.intent.sibling("candidate")).unwrap(),
        f.intent.after
    );
    assert!(!f.intent.sibling("original").exists());
    for path in [f.intent.target(), f.intent.sibling("candidate")] {
        assert_eq!(
            fs::read(format!("{}:Zone.Identifier", path.display())).unwrap(),
            b"[ZoneTransfer]\r\nZoneId=3\r\n"
        );
    }
    assert_eq!(fs::read(f.journal()).unwrap(), journal_before);
    assert_eq!(
        inspect(&f.storage, &f.intent.id, &f.root)
            .unwrap()
            .observation,
        "source_present"
    );
    assert!(
        RecoveryReview::capture(
            &f.storage,
            "root-choice",
            &f.root,
            &f.intent.id,
            RecoveryMode::UndoInstalled
        )
        .is_err()
    );
}

#[test]
fn interrupted_undo_can_be_reviewed_as_missing_and_restored_without_overwrite() {
    let f = Fixture::new(true);
    let ready = f
        .review(RecoveryMode::UndoInstalled)
        .prepare("root-choice", &f.root)
        .unwrap();
    drop(ready.park_current().unwrap()); // All process-owned handles released.
    assert!(!f.intent.target().exists());
    assert_eq!(
        inspect(&f.storage, &f.intent.id, &f.root)
            .unwrap()
            .observation,
        "target_missing"
    );
    let review = f.review(RecoveryMode::MissingTarget);
    f.finish(review).unwrap();
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.before);
    assert_eq!(
        fs::read(f.intent.sibling("candidate")).unwrap(),
        f.intent.after
    );
}

#[test]
fn recovery_modes_do_not_silently_change_when_disk_state_changes() {
    for installed in [false, true] {
        let f = Fixture::new(installed);
        let wrong = if installed {
            RecoveryMode::MissingTarget
        } else {
            RecoveryMode::UndoInstalled
        };
        assert!(
            RecoveryReview::capture(&f.storage, "root-choice", &f.root, &f.intent.id, wrong)
                .is_err()
        );
        assert_eq!(f.intent.target().exists(), installed);
    }
}

#[test]
fn expired_or_rebound_reviews_leave_both_versions_untouched() {
    for mode in ["expired", "choice", "path"] {
        let f = Fixture::new(true);
        let mut review = f.review(RecoveryMode::UndoInstalled);
        if mode == "expired" {
            review.created = Instant::now() - TTL;
        }
        let choice = if mode == "choice" {
            "other-selection"
        } else {
            "root-choice"
        };
        let root = if mode == "path" { &f.dir } else { &f.root };
        assert!(review.prepare(choice, root).is_err());
        assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
        assert_eq!(
            fs::read(f.intent.sibling("original")).unwrap(),
            f.intent.before
        );
        assert!(!f.intent.sibling("candidate").exists());
    }
}

#[test]
fn post_review_body_stream_or_file_identity_changes_refuse_before_movement() {
    for change in [
        "target-body",
        "source-body",
        "target-stream",
        "source-stream",
        "target-identity",
    ] {
        let f = Fixture::new(true);
        let review = f.review(RecoveryMode::UndoInstalled);
        match change {
            "target-body" => fs::write(f.intent.target(), b"user edit").unwrap(),
            "source-body" => fs::write(f.intent.sibling("original"), b"source edit").unwrap(),
            "target-stream" => fs::write(
                format!("{}:Zone.Identifier", f.intent.target().display()),
                b"new stream",
            )
            .unwrap(),
            "source-stream" => fs::write(
                format!("{}:Zone.Identifier", f.intent.sibling("original").display()),
                b"new stream",
            )
            .unwrap(),
            "target-identity" => {
                fs::rename(f.intent.target(), f.root.join("retained-candidate")).unwrap();
                fs::write(f.intent.target(), &f.intent.after).unwrap();
            }
            _ => unreachable!(),
        }
        assert!(f.finish(review).is_err(), "{change}");
        assert!(f.intent.target().exists());
        assert!(f.intent.sibling("original").exists());
        assert!(!f.intent.sibling("candidate").exists());
    }
}

#[test]
fn coherently_rewritten_journal_cannot_replace_the_reviewed_origin() {
    let f = Fixture::new(true);
    let review = f.review(RecoveryMode::UndoInstalled);
    let mut changed = f.intent.clone();
    changed.before = b"different original".to_vec();
    let encoded = serde_json::to_string(&changed).unwrap();
    fs::write(
        f.journal(),
        serde_json::to_vec(&serde_json::json!({
            "intent": changed, "digest": hash(&encoded),
        }))
        .unwrap(),
    )
    .unwrap();
    assert!(f.finish(review).is_err());
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
    assert_eq!(
        fs::read(f.intent.sibling("original")).unwrap(),
        f.intent.before
    );
}

#[test]
fn target_created_in_restore_gap_wins_and_both_recorded_objects_remain() {
    for installed in [false, true] {
        let f = Fixture::new(installed);
        let mode = if installed {
            RecoveryMode::UndoInstalled
        } else {
            RecoveryMode::MissingTarget
        };
        let vacant = f
            .review(mode)
            .prepare("root-choice", &f.root)
            .unwrap()
            .park_current()
            .unwrap();
        fs::write(f.intent.target(), b"user wins").unwrap();
        assert!(vacant.restore().is_err());
        assert_eq!(fs::read(f.intent.target()).unwrap(), b"user wins");
        assert_eq!(
            fs::read(f.intent.sibling("original")).unwrap(),
            f.intent.before
        );
        assert_eq!(
            fs::read(f.intent.sibling("candidate")).unwrap(),
            f.intent.after
        );
    }
}

#[test]
fn late_staging_collision_prevents_undo_without_moving_target() {
    let f = Fixture::new(true);
    let ready = f
        .review(RecoveryMode::UndoInstalled)
        .prepare("root-choice", &f.root)
        .unwrap();
    fs::write(f.intent.sibling("candidate"), b"user staging name").unwrap();
    assert!(ready.park_current().is_err());
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
    assert_eq!(
        fs::read(f.intent.sibling("candidate")).unwrap(),
        b"user staging name"
    );
    assert_eq!(
        fs::read(f.intent.sibling("original")).unwrap(),
        f.intent.before
    );
}

#[test]
fn late_hardlinks_and_source_data_remain_unchanged_through_undo() {
    let f = Fixture::new(true);
    let mut ready = f
        .review(RecoveryMode::UndoInstalled)
        .prepare("root-choice", &f.root)
        .unwrap();
    // Undo receives only read/delete, never content-write or truncate rights.
    assert!(ready.pair.original.write_all(b"forbidden").is_err());
    assert!(ready.pair.candidate.set_len(0).is_err());
    fs::hard_link(f.intent.target(), f.dir.join("outside-candidate")).unwrap();
    fs::hard_link(f.intent.sibling("original"), f.dir.join("outside-original")).unwrap();
    ready.park_current().unwrap().restore().unwrap();
    assert_eq!(
        fs::read(f.dir.join("outside-candidate")).unwrap(),
        f.intent.after
    );
    assert_eq!(
        fs::read(f.dir.join("outside-original")).unwrap(),
        f.intent.before
    );
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.before);
}

#[test]
fn ready_recovery_pins_journal_and_ancestors_without_extending_review_expiry() {
    let f = Fixture::new(true);
    let mut ready = f
        .review(RecoveryMode::UndoInstalled)
        .prepare("root-choice", &f.root)
        .unwrap();
    assert!(fs::write(f.journal(), b"replace").is_err());
    assert!(fs::rename(f.journal(), f.storage.join("other")).is_err());
    assert!(fs::rename(&f.root, f.dir.join("other-project")).is_err());
    ready.review.created = Instant::now() - TTL;
    assert!(ready.park_current().is_err());
    assert_eq!(fs::read(f.intent.target()).unwrap(), f.intent.after);
}
