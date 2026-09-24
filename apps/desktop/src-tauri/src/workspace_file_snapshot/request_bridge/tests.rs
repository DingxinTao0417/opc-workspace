use super::*;
use std::fs;

struct Fixture {
    dir: PathBuf,
    root: PathBuf,
    choice: String,
    state: FileSnapshotState,
}
impl Fixture {
    fn new() -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-file-request-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        fs::create_dir_all(&root).unwrap();
        fs::write(root.join("file.txt"), b"\xef\xbb\xbforiginal\r\n").unwrap();
        Self {
            dir,
            root,
            choice: uuid::Uuid::new_v4().to_string(),
            state: FileSnapshotState::default(),
        }
    }
    fn capture(&self) -> FileSnapshot {
        self.state
            .capture(self.choice.clone(), self.root.clone(), "file.txt".into())
            .unwrap()
    }
    fn freeze(&self, snapshot: &FileSnapshot) -> Result<CheckedRequest, String> {
        self.state.freeze_edit_request(
            &self.choice,
            &self.root,
            &snapshot.id,
            &uuid::Uuid::new_v4().to_string(),
            b"candidate\n",
        )
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
                .starts_with("opc-file-request-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn native_snapshot_becomes_exact_unapproved_request_once_without_writing() {
    let f = Fixture::new();
    let snapshot = f.capture();
    let baseline = f
        .state
        .0
        .lock()
        .unwrap()
        .get(&snapshot.id)
        .unwrap()
        .file
        .clone();
    let request = f.freeze(&snapshot).unwrap();
    let facts = request.facts();
    assert_eq!(facts.root_selection_id, f.choice);
    assert_eq!(facts.review_id, snapshot.id);
    assert_eq!(facts.root, f.root.to_str().unwrap());
    let Operation::Replace { original, .. } = &facts.operation else {
        panic!()
    };
    assert!(original.identity == identity(&baseline.file_identity));
    assert_eq!(original.modified, baseline.modified);
    let bytes = request.encode(now_ms().unwrap()).unwrap();
    let decoded = CheckedRequest::decode(&bytes, now_ms().unwrap()).unwrap();
    assert_eq!(decoded.encode(now_ms().unwrap()).unwrap(), bytes);
    let pending =
        crate::file_operation_review::PendingReview::new(decoded, now_ms().unwrap()).unwrap();
    let digest = pending.document(now_ms().unwrap()).unwrap().1.to_owned();
    // A simulated UI decision only tests immutable binding, not native consent.
    let decoded = pending
        .decide(
            crate::file_operation_review::Decision::Continue,
            &digest,
            &f.choice,
            now_ms().unwrap(),
        )
        .unwrap()
        .unwrap()
        .into_unapproved_request(now_ms().unwrap())
        .unwrap();
    assert_eq!(decoded.encode(now_ms().unwrap()).unwrap(), bytes);
    let mut disk = crate::file_operation_disk::DiskReview::inspect(&decoded).unwrap();
    disk.revalidate().unwrap();
    drop(disk);
    assert!(f.freeze(&snapshot).is_err());
    assert_eq!(fs::read(f.root.join("file.txt")).unwrap(), baseline.content);
    assert_eq!(fs::read_dir(&f.root).unwrap().count(), 1);
}

#[test]
fn native_request_does_not_extend_review_and_expired_reviews_are_consumed() {
    let f = Fixture::new();
    let snapshot = f.capture();
    f.state
        .0
        .lock()
        .unwrap()
        .get_mut(&snapshot.id)
        .unwrap()
        .created = Instant::now() - (TTL - Duration::from_secs(10));
    let request = f.freeze(&snapshot).unwrap();
    assert!(request.facts().expires_at_ms - request.facts().issued_at_ms <= 10_000);
    assert!(request.encode(request.facts().expires_at_ms).is_err());
    let snapshot = f.capture();
    f.state
        .0
        .lock()
        .unwrap()
        .get_mut(&snapshot.id)
        .unwrap()
        .created = Instant::now() - TTL;
    assert!(f.freeze(&snapshot).is_err());
    assert!(!f.state.0.lock().unwrap().contains_key(&snapshot.id));
}

#[test]
fn drift_wrong_selection_and_invalid_candidate_consume_without_requests() {
    let f = Fixture::new();
    let snapshot = f.capture();
    assert!(
        f.state
            .freeze_edit_request(
                &uuid::Uuid::new_v4().to_string(),
                &f.root,
                &snapshot.id,
                &uuid::Uuid::new_v4().to_string(),
                b"candidate"
            )
            .is_err()
    );
    assert!(f.freeze(&snapshot).is_err());
    let snapshot = f.capture();
    assert!(
        f.state
            .freeze_edit_request(
                &f.choice,
                &f.root,
                &snapshot.id,
                &uuid::Uuid::new_v4().to_string(),
                &[255]
            )
            .is_err()
    );
    assert!(f.freeze(&snapshot).is_err());
    let snapshot = f.capture();
    fs::write(f.root.join("file.txt"), b"newer user content").unwrap();
    assert!(f.freeze(&snapshot).is_err());
    assert_eq!(
        fs::read(f.root.join("file.txt")).unwrap(),
        b"newer user content"
    );
    assert_eq!(fs::read_dir(&f.root).unwrap().count(), 1);
}

#[test]
fn late_alias_and_recovery_baseline_cannot_be_repackaged_as_a_text_edit() {
    let f = Fixture::new();
    let snapshot = f.capture();
    f.state
        .0
        .lock()
        .unwrap()
        .get_mut(&snapshot.id)
        .unwrap()
        .recovery = Some((uuid::Uuid::new_v4().to_string(), b"recovery".to_vec()));
    assert!(f.freeze(&snapshot).is_err());
    let snapshot = f.capture();
    fs::hard_link(f.root.join("file.txt"), f.dir.join("outside-selection.txt")).unwrap();
    assert!(f.freeze(&snapshot).is_err());
    assert_eq!(
        fs::read(f.dir.join("outside-selection.txt")).unwrap(),
        b"\xef\xbb\xbforiginal\r\n"
    );
}
