use super::*;
use crate::file_operation_contract::{Content, Request};
use std::{fs, io::Write};

struct Fixture {
    dir: PathBuf,
    root: PathBuf,
    path: PathBuf,
}
impl Fixture {
    fn new() -> Self {
        let dir = std::env::temp_dir().join(format!("opc-file-disk-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        fs::create_dir_all(root.join("src")).unwrap();
        let path = root.join("src/file.txt");
        fs::write(&path, b"\xef\xbb\xbforiginal\r\n").unwrap();
        Self { dir, root, path }
    }
    fn observed(path: &Path) -> ObservedFile {
        let mut file = open_read(path).unwrap();
        let (bytes, identity, modified) = read::read_regular(&mut file, MAX_FILE_BYTES).unwrap();
        ObservedFile {
            identity,
            modified,
            content: Content::from_bytes(&bytes).unwrap(),
            metadata_sha256: Metadata::capture(&file, &bytes)
                .unwrap()
                .review_fingerprint()
                .unwrap(),
            readable_security_sha256: sha256(SecurityDescriptor::capture(&file).unwrap().bytes()),
        }
    }
    fn facts(&self) -> Request {
        let now = Self::now();
        let id = || uuid::Uuid::new_v4().to_string();
        Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: now - 1,
            expires_at_ms: now + 60_000,
            root: self.root.to_str().unwrap().into(),
            path: "src/file.txt".into(),
            root_identity: PinnedDirectory::open(&self.root, false).unwrap().identity,
            parent_identity: PinnedDirectory::open(self.path.parent().unwrap(), false)
                .unwrap()
                .identity,
            operation: Operation::Replace {
                original: Self::observed(&self.path),
                candidate: Content::from_bytes(b"candidate\n").unwrap(),
            },
        }
    }
    fn request(&self) -> CheckedRequest {
        CheckedRequest::from_local(self.facts(), Self::now()).unwrap()
    }
    fn now() -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_millis() as u64
    }
    fn recovery(&self, installed: bool) -> (CheckedRequest, PathBuf, PathBuf) {
        let mut facts = self.facts();
        let Operation::Replace { original, .. } = facts.operation else {
            panic!()
        };
        let id = uuid::Uuid::new_v4().to_string();
        let parked = self.path.with_file_name(format!(".opc-file-{id}.original"));
        let staged = self
            .path
            .with_file_name(format!(".opc-file-{id}.candidate"));
        fs::rename(&self.path, &parked).unwrap();
        fs::write(if installed { &self.path } else { &staged }, b"candidate\n").unwrap();
        let candidate = Self::observed(if installed { &self.path } else { &staged });
        facts.operation = if installed {
            Operation::UndoInstalled {
                record_version: 2,
                record_id: id,
                record_sha256: sha256(b"unverified-journal"),
                original,
                candidate,
            }
        } else {
            Operation::RestoreMissing {
                record_version: 2,
                record_id: id,
                record_sha256: sha256(b"unverified-journal"),
                original,
                candidate,
            }
        };
        (
            CheckedRequest::from_local(facts, Self::now()).unwrap(),
            parked,
            staged,
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
                .starts_with("opc-file-disk-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}
fn error(request: &CheckedRequest) -> String {
    DiskReview::inspect(request).err().expect("must reject")
}

#[test]
fn audit_inspection_never_requests_data_write_or_delete_rights() {
    use windows::Win32::Storage::FileSystem::{
        DELETE, FILE_APPEND_DATA, FILE_WRITE_DATA, WRITE_DAC, WRITE_OWNER,
    };
    assert_eq!(inspection_access(false), FILE_GENERIC_READ.0);
    assert_eq!(
        inspection_access(true),
        FILE_GENERIC_READ.0 | ACCESS_SYSTEM_SECURITY
    );
    assert_eq!(
        inspection_access(true)
            & (DELETE | FILE_APPEND_DATA | FILE_WRITE_DATA | WRITE_DAC | WRITE_OWNER).0,
        0
    );
    // ACCESS_SYSTEM_SECURITY can also SET SACL: it is not an OS read-only ACL
    // permission. The production helper never exposes these handles or setters.
}

#[test]
fn execution_handles_add_only_delete_and_audit_query_to_read_access() {
    use windows::Win32::Storage::FileSystem::{
        FILE_APPEND_DATA, FILE_WRITE_DATA, WRITE_DAC, WRITE_OWNER,
    };
    assert_eq!(move_access(false), (FILE_GENERIC_READ | DELETE).0);
    assert_eq!(
        move_access(true),
        (FILE_GENERIC_READ | DELETE).0 | ACCESS_SYSTEM_SECURITY
    );
    assert_eq!(
        move_access(true) & (FILE_APPEND_DATA | FILE_WRITE_DATA | WRITE_DAC | WRITE_OWNER).0,
        0
    );
}

#[test]
fn replace_guard_pins_the_reviewed_source_and_ancestors_without_write_access() {
    let f = Fixture::new();
    let request = f.request();
    let mut guard = DiskReview::inspect(&request)
        .unwrap()
        .into_replace_guard()
        .unwrap();
    assert_eq!(guard.parent(), f.path.parent().unwrap());
    guard.revalidate(&request).unwrap();
    assert!(fs::write(&f.path, b"forbidden").is_err());
    assert!(fs::rename(&f.root, f.dir.join("moved")).is_err());
    drop(guard);
    fs::rename(&f.root, f.dir.join("released")).unwrap();

    let recovery = Fixture::new();
    let request = recovery.recovery(false).0;
    assert!(
        DiskReview::inspect(&request)
            .unwrap()
            .into_replace_guard()
            .is_err()
    );
}

#[test]
fn security_capture_and_readback_use_same_pinned_objects_then_release_them() {
    use std::cell::Cell;
    for mode in ["replace", "missing", "undo"] {
        let f = Fixture::new();
        let request = match mode {
            "replace" => f.request(),
            _ => f.recovery(mode == "undo").0,
        };
        let captures = Cell::new(0);
        let verifications = Cell::new(0);
        // Ordinary rights and injected capture only. No SACL query or token
        // adjustment; this tests shared pinning/composition, not OS privileges.
        let (original, candidate) = DiskReview::capture_with_access(
            &request,
            false,
            |file| {
                captures.set(captures.get() + 1);
                let mut duplicate = file.try_clone().unwrap();
                assert!(duplicate.write_all(b"forbidden").is_err());
                assert!(duplicate.set_len(0).is_err());
                assert!(fs::rename(&f.root, f.dir.join("moved")).is_err());
                read::recorded_identity(file)
            },
            |file, expected| {
                assert_eq!(&read::recorded_identity(file)?, expected);
                verifications.set(verifications.get() + 1);
                Ok(())
            },
        )
        .unwrap();
        let expected_count = if mode == "replace" { 1 } else { 2 };
        assert_eq!(captures.get(), expected_count);
        assert_eq!(verifications.get(), expected_count);
        assert_eq!(candidate.is_some(), mode != "replace");
        match &request.facts().operation {
            Operation::Replace {
                original: expected, ..
            }
            | Operation::RestoreMissing {
                original: expected, ..
            }
            | Operation::UndoInstalled {
                original: expected, ..
            } => assert_eq!(original, expected.identity),
        }
        fs::rename(&f.root, f.dir.join("released")).unwrap();
    }
}

#[test]
fn invalid_file_facts_prevent_security_capture() {
    for mode in ["identity", "metadata", "partial_acl"] {
        let f = Fixture::new();
        let mut facts = f.facts();
        let Operation::Replace { original, .. } = &mut facts.operation else {
            panic!()
        };
        match mode {
            "identity" => original.identity.index += 1,
            "metadata" => original.metadata_sha256 = sha256(b"wrong metadata"),
            _ => original.readable_security_sha256 = sha256(b"wrong partial ACL"),
        }
        let request = CheckedRequest::from_local(facts, Fixture::now()).unwrap();
        assert!(
            DiskReview::capture_with_access(
                &request,
                false,
                |_| -> Result<(), String> {
                    panic!("capture must not run before file facts match")
                },
                |_, _| panic!("verify must not run"),
            )
            .is_err()
        );
    }
}

#[test]
fn security_capture_or_readback_failure_releases_without_modifying_files() {
    for stage in ["capture", "verify", "unwind"] {
        let f = Fixture::new();
        let request = f.request();
        let outcome = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            DiskReview::capture_with_access(
                &request,
                false,
                |_| {
                    if stage == "capture" {
                        return Err("simulated full capture denial".into());
                    }
                    if stage == "unwind" {
                        panic!("simulated capture panic");
                    }
                    Ok(())
                },
                |_, _| Err("simulated descriptor drift".into()),
            )
        }));
        if stage == "unwind" {
            assert!(outcome.is_err());
        } else {
            assert!(outcome.unwrap().is_err());
        }
        assert_eq!(fs::read(&f.path).unwrap(), b"\xef\xbb\xbforiginal\r\n");
        fs::rename(&f.root, f.dir.join("released")).unwrap();
    }
}

#[test]
fn security_capture_does_not_reserve_missing_targets_or_accept_late_aliases() {
    for missing in [true, false] {
        let f = Fixture::new();
        let request = if missing {
            f.recovery(false).0
        } else {
            f.request()
        };
        let changed = std::cell::Cell::new(false);
        let result = DiskReview::capture_with_access(
            &request,
            false,
            |_| {
                if !changed.replace(true) {
                    if missing {
                        fs::write(&f.path, b"new competing file").unwrap();
                    } else {
                        fs::hard_link(&f.path, f.dir.join("external-alias")).unwrap();
                    }
                }
                Ok(())
            },
            |_, _| Ok(()),
        );
        assert!(result.is_err());
        assert_eq!(
            fs::read(&f.path).unwrap(),
            if missing {
                b"new competing file".as_slice()
            } else {
                b"\xef\xbb\xbforiginal\r\n".as_slice()
            }
        );
    }
}

#[test]
fn exact_review_pins_paths_and_has_no_write_rights() {
    let f = Fixture::new();
    let request = f.request();
    let mut review = DiskReview::inspect(&request).unwrap();
    assert!(review.original.write_all(b"forbidden").is_err());
    assert!(review.original.set_len(0).is_err());
    assert!(fs::write(&f.path, b"conflict").is_err());
    assert!(fs::rename(&f.root, f.dir.join("moved")).is_err());
    assert!(fs::rename(f.root.join("src"), f.root.join("moved-src")).is_err());
    review.revalidate().unwrap();
    drop(review);
    assert_eq!(fs::read(&f.path).unwrap(), b"\xef\xbb\xbforiginal\r\n");
}

#[test]
fn foreign_identity_is_rejected_before_reading_oversized_body() {
    let f = Fixture::new();
    let request = f.request();
    fs::rename(&f.path, f.root.join("retained")).unwrap();
    fs::write(&f.path, vec![42; MAX_FILE_BYTES + 1]).unwrap();
    assert_eq!(error(&request), "文件身份与审查请求不符");
}

#[test]
fn changed_body_timestamp_metadata_and_readable_security_fail_closed() {
    for change in ["body", "time", "stream", "security"] {
        let f = Fixture::new();
        let mut facts = f.facts();
        let modified = fs::metadata(&f.path).unwrap().modified().unwrap();
        if let Operation::Replace { original, .. } = &mut facts.operation {
            if change == "time" {
                original.modified ^= 1;
            }
            if change == "security" {
                original.readable_security_sha256 = sha256(b"not-current-acl");
            }
        }
        let request = CheckedRequest::from_local(facts, Fixture::now()).unwrap();
        match change {
            "body" => fs::write(&f.path, b"user edit").unwrap(),
            "stream" => {
                fs::write(
                    format!("{}:Zone.Identifier", f.path.display()),
                    b"zone data",
                )
                .unwrap();
                // ADS writes also change LastWriteTime. Restore that timestamp
                // so this case proves the separate metadata check rejects drift.
                File::options()
                    .write(true)
                    .open(&f.path)
                    .unwrap()
                    .set_times(fs::FileTimes::new().set_modified(modified))
                    .unwrap();
            }
            _ => (),
        }
        let e = error(&request);
        assert!(
            e.contains(match change {
                "stream" => "元数据",
                "security" => "权限",
                _ => "正文或修改时间",
            }),
            "{change}: {e}"
        );
    }
}

#[test]
fn replaced_root_or_parent_is_rejected_before_file_data() {
    for root in [true, false] {
        let f = Fixture::new();
        let request = f.request();
        let dir = if root {
            f.root.clone()
        } else {
            f.root.join("src")
        };
        fs::rename(&dir, f.dir.join("retained")).unwrap();
        fs::create_dir_all(&dir).unwrap();
        let e = error(&request);
        assert!(
            e.contains(if root {
                "根目录身份"
            } else {
                "父目录身份"
            }),
            "{e}"
        );
    }
}

#[test]
fn late_alias_rejects_replace_including_revalidation() {
    let f = Fixture::new();
    let request = f.request();
    let mut review = DiskReview::inspect(&request).unwrap();
    fs::hard_link(&f.path, f.dir.join("outside")).unwrap();
    assert!(review.revalidate().is_err());
    drop(review);
    assert!(error(&request).contains("硬链接"));
    assert_eq!(
        fs::read(f.dir.join("outside")).unwrap(),
        b"\xef\xbb\xbforiginal\r\n"
    );
}

#[test]
fn recovery_modes_verify_read_only_objects_but_not_journal_or_approval() {
    for installed in [false, true] {
        let f = Fixture::new();
        // This fixture intentionally has no journal: disk review must never be
        // described as independently authenticating the recovery record.
        let (request, parked, staged) = f.recovery(installed);
        let mut review = DiskReview::inspect(&request).unwrap();
        fs::hard_link(&parked, f.dir.join("outside-original")).unwrap();
        fs::hard_link(
            if installed { &f.path } else { &staged },
            f.dir.join("outside-candidate"),
        )
        .unwrap();
        review.revalidate().unwrap();
        assert!(review.original.set_len(0).is_err());
        assert!(
            review
                .candidate
                .as_mut()
                .unwrap()
                .write_all(b"bad")
                .is_err()
        );
        drop(review);
        assert_eq!(f.path.exists(), installed);
        assert_eq!(staged.exists(), !installed);
        assert_eq!(fs::read(&parked).unwrap(), b"\xef\xbb\xbforiginal\r\n");
    }
}

#[test]
fn absent_slot_is_not_reserved_and_new_occupants_win() {
    for installed in [false, true] {
        for directory in [false, true] {
            let f = Fixture::new();
            let (request, _, staged) = f.recovery(installed);
            let mut review = DiskReview::inspect(&request).unwrap();
            let vacant = if installed { &staged } else { &f.path };
            if directory {
                fs::create_dir(vacant).unwrap();
            } else {
                fs::write(vacant, b"user wins").unwrap();
            }
            assert!(review.revalidate().unwrap_err().contains("缺失"));
            drop(review);
            assert!(error(&request).contains("缺失"));
            if !directory {
                assert_eq!(fs::read(vacant).unwrap(), b"user wins");
            }
        }
    }
}

#[test]
fn foreign_or_changed_recovery_candidate_is_not_accepted() {
    for installed in [false, true] {
        for foreign in [false, true] {
            let f = Fixture::new();
            let (request, _, staged) = f.recovery(installed);
            let candidate = if installed { &f.path } else { &staged };
            if foreign {
                fs::rename(candidate, f.root.join("retained")).unwrap();
            }
            fs::write(candidate, b"foreign or edited").unwrap();
            let e = error(&request);
            assert!(
                e.contains(if foreign {
                    "候选身份"
                } else {
                    "正文或修改时间"
                }),
                "{e}"
            );
        }
    }
}

#[test]
fn expired_request_cannot_start_disk_review() {
    let f = Fixture::new();
    let mut facts = f.facts();
    let now = Fixture::now();
    facts.issued_at_ms = now - 100;
    facts.expires_at_ms = now - 10;
    let request = CheckedRequest::from_local(facts, now - 50).unwrap();
    assert!(error(&request).contains("期"));
}
