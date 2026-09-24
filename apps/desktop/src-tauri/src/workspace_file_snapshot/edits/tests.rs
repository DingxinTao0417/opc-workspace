use super::*;
use std::os::windows::fs::OpenOptionsExt;

struct Fixture {
    dir: PathBuf,
    root: PathBuf,
    storage: PathBuf,
    state: FileSnapshotState,
}
impl Fixture {
    fn new(content: &[u8]) -> Self {
        let dir = std::env::temp_dir().join(format!("opc-file-edit-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        let storage = dir.join("recovery");
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join("file.txt"), content).unwrap();
        Self {
            dir,
            root,
            storage,
            state: FileSnapshotState::default(),
        }
    }
    fn capture(&self) -> FileSnapshot {
        self.state
            .capture("root".into(), self.root.clone(), "file.txt".into())
            .unwrap()
    }
    fn apply(
        &self,
        snapshot: &str,
        id: &str,
        content: &[u8],
        fault: Option<usize>,
    ) -> Result<FileEditResult, String> {
        apply(
            &self.state,
            &self.storage,
            "root",
            &self.root,
            snapshot,
            id,
            content,
            None,
            fault,
        )
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        assert!(self.dir.starts_with(std::env::temp_dir()));
        assert!(
            self.dir
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("opc-file-edit-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn product_operation_lease_is_single_scope_cancellable_and_not_replayable() {
    let state = FileEditState::default();
    let first_id = uuid::Uuid::new_v4().to_string();
    let lease = state.begin("root-a", &first_id).unwrap();
    assert!(
        state
            .begin("root-a", &uuid::Uuid::new_v4().to_string())
            .is_err()
    );
    assert!(state.cancel("root-b", &first_id).is_err());
    state.cancel("root-a", &first_id).unwrap();
    assert!(lease.operation.cancelled.load(Ordering::Acquire));
    drop(lease);
    // A new operation may begin only after the old lease has been consumed;
    // the old id itself is not reused by product request construction.
    let next = state
        .begin("root-a", &uuid::Uuid::new_v4().to_string())
        .unwrap();
    drop(next);
    state.cancel("root-a", &first_id).unwrap(); // late cancellation is idempotent
}

#[test]
fn writes_exact_bytes_after_backup_and_replays_without_mutation() {
    let before = "\u{feff}原文\r\n".as_bytes();
    let f = Fixture::new(before);
    let snap = f.capture();
    assert!(!f.storage.exists());
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), before);
    let id = uuid::Uuid::new_v4().to_string();
    assert_eq!(f.apply(&snap.id, &id, b"", None).unwrap().status, "applied");
    let record = load_record(&f.storage, &id).unwrap();
    assert_eq!(record.before, before);
    assert!(record.after.is_empty());
    assert!(fs::read(f.root.join("file.txt")).unwrap().is_empty());
    fs::write(f.root.join("file.txt"), b"user changes later").unwrap();
    assert_eq!(
        f.apply(&snap.id, &id, b"", None).unwrap().status,
        "already_recorded"
    );
    assert_eq!(
        fs::read(f.root.join("file.txt")).unwrap(),
        b"user changes later"
    );
    assert!(f.apply(&snap.id, &id, b"different", None).is_err());
}

#[test]
fn drift_expiry_wrong_root_and_hardlinks_reject_without_writes() {
    for mode in [
        "bytes", "identity", "expired", "root", "hardlink", "busy", "readonly",
    ] {
        let f = Fixture::new(b"before");
        let snap = f.capture();
        let path = f.root.join("file.txt");
        let mut held = None;
        match mode {
            "bytes" => fs::write(&path, b"user change").unwrap(),
            "identity" => {
                fs::rename(&path, f.root.join("old.txt")).unwrap();
                fs::write(&path, b"before").unwrap();
            }
            "expired" => {
                f.state.0.lock().unwrap().get_mut(&snap.id).unwrap().created = Instant::now() - TTL
            }
            "root" => f.state.0.lock().unwrap().get_mut(&snap.id).unwrap().root_id = "other".into(),
            "hardlink" => fs::hard_link(&path, f.root.join("linked.txt")).unwrap(),
            "busy" => {
                held = Some(
                    fs::OpenOptions::new()
                        .read(true)
                        .share_mode(1)
                        .open(&path)
                        .unwrap(),
                )
            }
            "readonly" => {
                let mut p = fs::metadata(&path).unwrap().permissions();
                p.set_readonly(true);
                fs::set_permissions(&path, p).unwrap();
            }
            _ => unreachable!(),
        }
        assert!(
            f.apply(
                &snap.id,
                &uuid::Uuid::new_v4().to_string(),
                b"candidate",
                None
            )
            .is_err(),
            "{mode}"
        );
        drop(held);
        assert!(!f.storage.exists(), "{mode}");
        assert_ne!(fs::read(&path).unwrap(), b"candidate");
        if mode == "readonly" {
            let mut p = fs::metadata(&path).unwrap().permissions();
            p.set_readonly(false);
            fs::set_permissions(&path, p).unwrap();
        }
    }
}

#[test]
fn exclusive_handle_blocks_writers_but_not_new_links_so_legacy_writer_stays_closed() {
    let f = Fixture::new(b"before");
    let mut held = windows::open(&f.root, "file.txt", true).unwrap();
    assert_eq!(held.read().unwrap().content, b"before");
    assert!(fs::write(f.root.join("file.txt"), b"attack").is_err());
    // Verified Windows behavior: do not turn the failing assumption into a
    // claimed sandbox. The old in-place writer remains closed; the product
    // path uses the separate helper-owned no-overwrite replacement protocol.
    fs::hard_link(f.root.join("file.txt"), f.root.join("late-link.txt")).unwrap();
    assert!(held.read().is_err());
    assert!(ensure_legacy_edits_disabled().is_err());
    assert_eq!(
        fs::read(f.root.join("late-link.txt"))
            .unwrap_err()
            .raw_os_error(),
        Some(::windows::Win32::Foundation::ERROR_SHARING_VIOLATION.0 as i32)
    );
    assert!(fs::rename(&f.root, f.dir.join("moved")).is_err());
    assert!(fs::rename(f.root.join("file.txt"), f.root.join("moved.txt")).is_err());
    drop(held);
    assert_eq!(fs::read(f.root.join("late-link.txt")).unwrap(), b"before");
}

#[test]
fn torn_utf8_write_can_be_reviewed_and_restored_after_restart() {
    let f = Fixture::new(b"original\r\n");
    let snap = f.capture();
    let id = uuid::Uuid::new_v4().to_string();
    let outcome = f
        .apply(&snap.id, &id, "新的全文\n".as_bytes(), Some(1))
        .unwrap();
    assert_eq!(outcome.status, "recovery_required");
    assert!(String::from_utf8(fs::read(f.root.join("file.txt")).unwrap()).is_err());
    let fresh = FileSnapshotState::default();
    let pending = preview(&fresh, &f.storage, "new-root", &f.root, &id).unwrap();
    assert_eq!(
        STANDARD.decode(&pending.original_base64).unwrap(),
        b"original\r\n"
    );
    let current = STANDARD.decode(&pending.current_base64).unwrap();
    assert_eq!(current, fs::read(f.root.join("file.txt")).unwrap());
    let restore_id = uuid::Uuid::new_v4().to_string();
    assert_eq!(
        apply(
            &fresh,
            &f.storage,
            "new-root",
            &f.root,
            &pending.snapshot_id,
            &restore_id,
            b"original\r\n",
            Some(id.clone()),
            None
        )
        .unwrap()
        .status,
        "applied"
    );
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"original\r\n");
    let undo_restore = load_record(&f.storage, &restore_id).unwrap();
    assert_eq!(undo_restore.before, current);
    assert_eq!(undo_restore.restores, Some(id));
    assert_eq!(list(&f.storage, &f.root).unwrap().records.len(), 2);
}

#[test]
fn recovery_rejects_later_changes_replaced_files_and_wrong_source() {
    let f = Fixture::new(b"original");
    let snap = f.capture();
    let id = uuid::Uuid::new_v4().to_string();
    f.apply(&snap.id, &id, b"candidate", None).unwrap();
    let p = preview(&f.state, &f.storage, "root", &f.root, &id).unwrap();
    fs::write(f.root.join("file.txt"), b"manual edit").unwrap();
    assert!(
        apply(
            &f.state,
            &f.storage,
            "root",
            &f.root,
            &p.snapshot_id,
            &uuid::Uuid::new_v4().to_string(),
            b"original",
            Some(id.clone()),
            None
        )
        .is_err()
    );
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"manual edit");
    fs::rename(f.root.join("file.txt"), f.root.join("old.txt")).unwrap();
    fs::write(f.root.join("file.txt"), b"candidate").unwrap();
    assert!(preview(&f.state, &f.storage, "root", &f.root, &id).is_err());
    assert!(preview(&f.state, &f.storage, "root", &f.dir, &id).is_err());
}

#[test]
fn backup_gate_and_corrupt_records_fail_closed() {
    let f = Fixture::new(b"before");
    fs::write(&f.storage, b"not a directory").unwrap();
    assert!(
        f.apply(
            &f.capture().id,
            &uuid::Uuid::new_v4().to_string(),
            b"candidate",
            None
        )
        .is_err()
    );
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"before");
    fs::remove_file(&f.storage).unwrap(); // Exact isolated test fixture, not user data.
    let id = uuid::Uuid::new_v4().to_string();
    f.apply(&f.capture().id, &id, b"candidate", None).unwrap();
    fs::write(record_path(&f.storage, &id).unwrap(), b"{incomplete").unwrap();
    assert!(preview(&f.state, &f.storage, "root", &f.root, &id).is_err());
    assert_eq!(list(&f.storage, &f.root).unwrap().damaged, 1);
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"candidate");
}

#[test]
fn capacity_bounds_and_invalid_candidates_do_not_modify_the_file() {
    let f = Fixture::new(b"before");
    for value in [vec![0], vec![255], vec![b'a'; 32 * 1024 + 1]] {
        assert!(
            f.apply(
                &f.capture().id,
                &uuid::Uuid::new_v4().to_string(),
                &value,
                None
            )
            .is_err()
        );
    }
    fs::create_dir(&f.storage).unwrap();
    for _ in 0..MAX_RECORDS {
        fs::write(
            f.storage.join(format!("{}.json", uuid::Uuid::new_v4())),
            b"incomplete",
        )
        .unwrap();
    }
    assert!(
        f.apply(
            &f.capture().id,
            &uuid::Uuid::new_v4().to_string(),
            b"candidate",
            None
        )
        .is_err()
    );
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"before");
}

#[test]
fn recovery_listing_rejects_overflow_instead_of_hiding_records() {
    let f = Fixture::new(b"before");
    assert!(list(&f.storage, &f.root).unwrap().records.is_empty());
    assert!(!f.storage.exists()); // Read-only inspection does not create storage.
    fs::create_dir(&f.storage).unwrap();
    for index in 0..MAX_RECORDS {
        fs::write(f.storage.join(format!("invalid-{index}")), b"invalid").unwrap();
    }
    assert_eq!(list(&f.storage, &f.root).unwrap().damaged, MAX_RECORDS);
    fs::write(f.storage.join("overflow"), b"invalid").unwrap();
    assert!(list(&f.storage, &f.root).err().unwrap().contains("64"));
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"before");
}

#[test]
fn recovery_freezes_reviewed_original_even_if_record_is_replaced() {
    let f = Fixture::new(b"original");
    let id = uuid::Uuid::new_v4().to_string();
    f.apply(&f.capture().id, &id, b"candidate", None).unwrap();
    let pending = preview(&f.state, &f.storage, "root", &f.root, &id).unwrap();
    let mut record = load_record(&f.storage, &id).unwrap();
    record.before = b"unreviewed replacement".to_vec();
    let digest = hash(&serde_json::to_string(&record).unwrap());
    fs::write(
        record_path(&f.storage, &id).unwrap(),
        serde_json::to_vec(&StoredRecord { record, digest }).unwrap(),
    )
    .unwrap();
    assert!(
        apply(
            &f.state,
            &f.storage,
            "root",
            &f.root,
            &pending.snapshot_id,
            &uuid::Uuid::new_v4().to_string(),
            b"unreviewed replacement",
            Some(id),
            None
        )
        .is_err()
    );
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), b"candidate");
}
