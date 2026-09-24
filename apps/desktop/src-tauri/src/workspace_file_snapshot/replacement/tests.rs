use super::super::replacement_journal::preview as inspect;
use super::*;

#[test]
fn readonly_inspection_distinguishes_prepared_missing_and_installed_without_receipts() {
    for phase in 0..3 {
        let f = Fixture::new();
        let recorded = f.prepare(b"").record(&f.storage).unwrap();
        let intent = recorded.prepared.intent.clone();
        if phase == 0 {
            drop(recorded);
        } else if phase == 1 {
            drop(recorded.park().unwrap());
        } else {
            recorded.park().unwrap().install().unwrap();
        }
        let before = fs::read(f.storage.join(format!("{}.json", intent.id))).unwrap();
        let view = inspect(&f.storage, &intent.id, &f.root).unwrap();
        assert_eq!(
            view.observation,
            ["source_present", "target_missing", "candidate_present"][phase]
        );
        assert_eq!(view.current_base64.is_none(), phase == 1);
        if phase == 2 {
            assert_eq!(view.current_base64.as_deref(), Some(""));
        }
        assert_eq!(
            fs::read(f.storage.join(format!("{}.json", intent.id))).unwrap(),
            before
        );
        assert_eq!(intent.target().exists(), phase != 1);
    }
}

#[test]
fn readonly_conflict_does_not_disclose_foreign_content_and_detects_changed_known_objects() {
    let f = Fixture::new();
    let recorded = f.prepare(b"candidate").record(&f.storage).unwrap();
    let intent = recorded.prepared.intent.clone();
    drop(recorded.park().unwrap());
    fs::write(f.target(), b"private concurrent user file").unwrap();
    let view = inspect(&f.storage, &intent.id, &f.root).unwrap();
    assert_eq!(view.observation, "conflict");
    assert_eq!(view.target, ReplacementFileState::Foreign);
    assert!(view.current_base64.is_none());
    assert_eq!(
        fs::read(f.target()).unwrap(),
        b"private concurrent user file"
    );
    fs::write(intent.sibling("original"), b"changed original").unwrap();
    let view = inspect(&f.storage, &intent.id, &f.root).unwrap();
    assert_eq!(view.parked, ReplacementFileState::Changed);
    assert_eq!(view.staged, ReplacementFileState::Candidate);
}

#[test]
fn readonly_inspection_rejects_wrong_root_parent_and_corrupt_intent() {
    let f = Fixture::new();
    let recorded = f.prepare(b"candidate").record(&f.storage).unwrap();
    let intent = recorded.prepared.intent.clone();
    drop(recorded);
    assert!(inspect(&f.storage, &intent.id, &f.dir).is_err());
    fs::rename(f.root.join("嵌套目录"), f.root.join("old-parent")).unwrap();
    fs::create_dir(f.root.join("嵌套目录")).unwrap();
    assert!(inspect(&f.storage, &intent.id, &f.root).is_err());
    fs::write(f.storage.join(format!("{}.json", intent.id)), b"not json").unwrap();
    assert!(load(&f.storage, &intent.id).is_err());
}

#[test]
fn readonly_inspection_keeps_late_aliases_and_reports_busy_instead_of_missing() {
    let f = Fixture::new();
    let recorded = f.prepare(b"candidate").record(&f.storage).unwrap();
    let intent = recorded.prepared.intent.clone();
    fs::hard_link(f.target(), f.dir.join("outside.txt")).unwrap();
    drop(recorded);
    assert_eq!(
        inspect(&f.storage, &intent.id, &f.root)
            .unwrap()
            .observation,
        "source_present"
    );
    let busy = source_file(&f.target()).unwrap();
    let view = inspect(&f.storage, &intent.id, &f.root).unwrap();
    assert_eq!(view.target, ReplacementFileState::Unavailable);
    assert_eq!(view.observation, "conflict");
    assert!(view.current_base64.is_none());
    drop(busy);
    assert_eq!(
        fs::read(f.dir.join("outside.txt")).unwrap(),
        b"original\r\n"
    );
}

#[test]
fn combined_listing_is_versioned_bounded_and_creates_no_directories() {
    let f = Fixture::new();
    let legacy = f.dir.join("legacy");
    let initial = super::super::edits::list_all(&legacy, &f.storage, &f.root).unwrap();
    assert_eq!(
        serde_json::to_value(initial).unwrap()["records"],
        serde_json::json!([])
    );
    assert!(!legacy.exists() && !f.storage.exists());
    drop(f.prepare(b"candidate").record(&f.storage).unwrap());
    let list = super::super::edits::list_all(&legacy, &f.storage, &f.root).unwrap();
    let list = serde_json::to_value(list).unwrap();
    assert_eq!(list["records"][0]["version"], 2);
    assert!(list["records"][0]["createdAt"].is_null());
    assert!(!legacy.exists());
    for n in 0..64 {
        fs::write(f.storage.join(format!("unknown-{n}")), b"unrecognized").unwrap();
    }
    assert!(super::super::edits::list_all(&legacy, &f.storage, &f.root).is_err());
}

struct Fixture {
    dir: PathBuf,
    root: PathBuf,
    storage: PathBuf,
}
impl Fixture {
    fn new() -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-file-replace-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        let storage = dir.join("recovery");
        fs::create_dir_all(root.join("嵌套目录")).unwrap();
        fs::write(root.join("嵌套目录/文件.txt"), b"original\r\n").unwrap();
        Self { dir, root, storage }
    }
    fn target(&self) -> PathBuf {
        self.root.join("嵌套目录/文件.txt")
    }
    fn prepare(&self, bytes: &[u8]) -> Prepared {
        let baseline = read_exact(&self.root, "嵌套目录/文件.txt").unwrap();
        Prepared::new(&self.root, "嵌套目录/文件.txt", &baseline, bytes).unwrap()
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
                .starts_with("opc-file-replace-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn late_hardlink_outside_selected_root_keeps_original_bytes() {
    let f = Fixture::new();
    let candidate = "\u{feff}新的内容\r\n".as_bytes();
    let prepared = f.prepare(candidate);
    let source_id = prepared.intent.source_identity.clone();
    let outside = f.dir.join("outside-user-selection.txt");
    // After all baseline comparisons, the exact attack that defeats in-place
    // writing. The replacement engine never obtains source FILE_WRITE_DATA.
    fs::hard_link(f.target(), &outside).unwrap();
    let recorded = prepared.record(&f.storage).unwrap();
    let intent = recorded.park().unwrap().install().unwrap();
    assert_eq!(fs::read(f.target()).unwrap(), candidate);
    assert_eq!(fs::read(&outside).unwrap(), b"original\r\n");
    assert_eq!(
        fs::read(intent.sibling("original")).unwrap(),
        b"original\r\n"
    );
    assert_ne!(
        read_exact(&f.root, &intent.path).unwrap().file_identity,
        source_id
    );
    assert!(!intent.sibling("candidate").exists());
    assert_eq!(load(&f.storage, &intent.id).unwrap().after, candidate);
}

#[test]
fn source_handle_has_no_data_write_rights() {
    let f = Fixture::new();
    let mut prepared = f.prepare(b"candidate");
    assert!(prepared.source.write_all(b"must not write").is_err());
    assert!(prepared.source.set_len(0).is_err());
    let stage = prepared.intent.sibling("candidate");
    drop(prepared);
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
    assert!(!stage.exists()); // Unrecorded, owned candidate is cleaned by handle.
}

#[test]
fn candidate_retains_owner_group_acl_label_and_protected_inheritance() {
    use ::windows::Win32::Security::SetSecurityDescriptorControl;
    let f = Fixture::new();
    let source = OpenOptions::new()
        .access_mode((FILE_GENERIC_READ | WRITE_DAC).0)
        .share_mode(0)
        .open(f.target())
        .unwrap();
    let mut descriptor = SecurityDescriptor::capture(&source).unwrap();
    // SAFETY: owned kernel-generated descriptor, modifies only its control bits.
    unsafe {
        SetSecurityDescriptorControl(
            PSECURITY_DESCRIPTOR(descriptor.aligned.as_mut_ptr().cast()),
            SE_DACL_PROTECTED,
            SE_DACL_PROTECTED,
        )
    }
    .unwrap();
    descriptor.apply_dacl(&source).unwrap();
    let expected = SecurityDescriptor::capture(&source)
        .unwrap()
        .bytes()
        .to_vec();
    drop(source);
    let intent = f
        .prepare(b"new content")
        .record(&f.storage)
        .unwrap()
        .park()
        .unwrap()
        .install()
        .unwrap();
    for path in [f.target(), intent.sibling("original")] {
        let file = File::open(path).unwrap();
        assert_eq!(
            SecurityDescriptor::capture(&file).unwrap().bytes(),
            expected
        );
    }
}

#[test]
fn interrupted_install_with_late_external_alias_restores_without_mutating_it() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let intent = prepared.intent.clone();
    let outside = f.dir.join("outside-alias.txt");
    fs::hard_link(f.target(), &outside).unwrap();
    drop(prepared.record(&f.storage).unwrap().park().unwrap());
    recover_missing(&f.storage, &intent.id, &f.root).unwrap();
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
    assert_eq!(fs::read(outside).unwrap(), b"original\r\n");
}

#[test]
fn concurrent_creation_wins_and_both_reviewed_versions_are_preserved() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let intent = prepared.intent.clone();
    let parked = prepared.record(&f.storage).unwrap().park().unwrap();
    fs::write(f.target(), b"new user file").unwrap();
    assert!(parked.install().is_err());
    assert_eq!(fs::read(f.target()).unwrap(), b"new user file");
    assert_eq!(
        fs::read(intent.sibling("original")).unwrap(),
        b"original\r\n"
    );
    assert_eq!(fs::read(intent.sibling("candidate")).unwrap(), b"candidate");
    assert!(recover_missing(&f.storage, &intent.id, &f.root).is_err());
    assert_eq!(fs::read(f.target()).unwrap(), b"new user file");
}

#[test]
fn interrupted_install_recovers_original_object_without_running_model_again() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let id = prepared.intent.id.clone();
    let identity = prepared.intent.source_identity.clone();
    let parked = prepared.record(&f.storage).unwrap().park().unwrap();
    assert!(!f.target().exists());
    drop(parked); // Simulate the process losing all handles before install.
    let intent = load(&f.storage, &id).unwrap();
    recover_missing(&f.storage, &intent.id, &f.root).unwrap();
    let current = read_exact(&f.root, &intent.path).unwrap();
    assert_eq!(current.content, b"original\r\n");
    assert_eq!(current.file_identity, identity); // Original object, not a copy.
    assert_eq!(fs::read(intent.sibling("candidate")).unwrap(), b"candidate");
    assert!(!intent.sibling("original").exists());
}

#[test]
fn backup_name_collision_and_journal_failure_do_not_move_original() {
    for fail_journal in [true, false] {
        let f = Fixture::new();
        let prepared = f.prepare(b"candidate");
        let intent = prepared.intent.clone();
        if fail_journal {
            fs::write(&f.storage, b"not a directory").unwrap();
            assert!(prepared.record(&f.storage).is_err());
        } else {
            fs::write(intent.sibling("original"), b"preexisting user file").unwrap();
            assert!(prepared.record(&f.storage).unwrap().park().is_err());
            assert_eq!(
                fs::read(intent.sibling("original")).unwrap(),
                b"preexisting user file"
            );
        }
        assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
    }
}

#[test]
fn changed_baseline_fails_before_creating_staged_files() {
    let f = Fixture::new();
    let baseline = read_exact(&f.root, "嵌套目录/文件.txt").unwrap();
    fs::write(f.target(), b"manual edit").unwrap();
    assert!(Prepared::new(&f.root, "嵌套目录/文件.txt", &baseline, b"candidate").is_err());
    assert_eq!(
        fs::read_dir(f.target().parent().unwrap()).unwrap().count(),
        1
    );
    assert_eq!(fs::read(f.target()).unwrap(), b"manual edit");
}

#[test]
fn recovery_refuses_replaced_parked_object_or_parent_directory() {
    for mode in ["source", "parent", "bytes", "root"] {
        let f = Fixture::new();
        let prepared = f.prepare(b"candidate");
        let intent = prepared.intent.clone();
        drop(prepared.record(&f.storage).unwrap().park().unwrap());
        match mode {
            "source" => {
                fs::rename(
                    intent.sibling("original"),
                    f.root.join("untouched-original"),
                )
                .unwrap();
                fs::write(intent.sibling("original"), b"original\r\n").unwrap();
            }
            "parent" => {
                fs::rename(f.root.join("嵌套目录"), f.root.join("moved-parent")).unwrap();
                fs::create_dir(f.root.join("嵌套目录")).unwrap();
                fs::rename(
                    f.root
                        .join("moved-parent")
                        .join(intent.sibling("original").file_name().unwrap()),
                    intent.sibling("original"),
                )
                .unwrap();
            }
            "bytes" => fs::write(intent.sibling("original"), b"user-edited parked source").unwrap(),
            "root" => (),
            _ => unreachable!(),
        }
        let selected = if mode == "root" { &f.dir } else { &f.root };
        assert!(
            recover_missing(&f.storage, &intent.id, selected).is_err(),
            "{mode}"
        );
        assert!(!f.target().exists());
    }
}

#[test]
fn existing_handles_pin_ancestors_and_completed_install_is_not_replayed() {
    let f = Fixture::new();
    let prepared = f.prepare(b"");
    assert!(fs::rename(&f.root, f.dir.join("moved")).is_err());
    assert!(fs::rename(f.target().parent().unwrap(), f.root.join("other")).is_err());
    let intent = prepared
        .record(&f.storage)
        .unwrap()
        .park()
        .unwrap()
        .install()
        .unwrap();
    assert!(fs::read(f.target()).unwrap().is_empty());
    assert!(recover_missing(&f.storage, &intent.id, &f.root).is_err());
    fs::write(f.target(), b"later user change").unwrap();
    assert!(recover_missing(&f.storage, &intent.id, &f.root).is_err());
    assert_eq!(fs::read(f.target()).unwrap(), b"later user change");
}

#[test]
fn replacement_preserves_zone_identifier_and_other_ads_locally() {
    let f = Fixture::new();
    let zone = format!("{}:Zone.Identifier", f.target().display());
    let extra = format!("{}:notes", f.target().display());
    fs::write(&zone, b"[ZoneTransfer]\r\nZoneId=3\r\n").unwrap();
    fs::write(&extra, b"private local metadata\0binary").unwrap();
    let intent = f
        .prepare(b"new default stream")
        .record(&f.storage)
        .unwrap()
        .park()
        .unwrap()
        .install()
        .unwrap();
    assert_eq!(fs::read(f.target()).unwrap(), b"new default stream");
    assert_eq!(fs::read(&zone).unwrap(), b"[ZoneTransfer]\r\nZoneId=3\r\n");
    assert_eq!(fs::read(&extra).unwrap(), b"private local metadata\0binary");
    assert_eq!(
        fs::read(format!("{}:notes", intent.sibling("original").display())).unwrap(),
        b"private local metadata\0binary"
    );
}

#[test]
fn replacement_preserves_creation_time_hidden_and_indexing_attributes() {
    use ::windows::Win32::Storage::FileSystem::{
        FILE_BASIC_INFO, FileBasicInfo, GetFileInformationByHandleEx,
    };
    let f = Fixture::new();
    let file = OpenOptions::new().write(true).open(f.target()).unwrap();
    let expected = FILE_BASIC_INFO {
        CreationTime: 132537600000000000,
        FileAttributes: 0x2 | 0x20 | 0x2000,
        ..Default::default()
    };
    // SAFETY: live isolated fixture handle and initialized metadata input.
    unsafe {
        SetFileInformationByHandle(
            HANDLE(file.as_raw_handle()),
            FileBasicInfo,
            (&expected as *const FILE_BASIC_INFO).cast(),
            size_of::<FILE_BASIC_INFO>() as u32,
        )
    }
    .unwrap();
    drop(file);
    f.prepare(b"changed")
        .record(&f.storage)
        .unwrap()
        .park()
        .unwrap()
        .install()
        .unwrap();
    let file = File::open(f.target()).unwrap();
    let mut actual = FILE_BASIC_INFO::default();
    // SAFETY: live fixture handle and aligned writable output buffer.
    unsafe {
        GetFileInformationByHandleEx(
            HANDLE(file.as_raw_handle()),
            FileBasicInfo,
            (&mut actual as *mut FILE_BASIC_INFO).cast(),
            size_of::<FILE_BASIC_INFO>() as u32,
        )
    }
    .unwrap();
    assert_eq!(actual.CreationTime, expected.CreationTime);
    assert_eq!(actual.FileAttributes, expected.FileAttributes);
}

#[test]
fn oversized_attached_data_is_rejected_before_candidate_creation() {
    let f = Fixture::new();
    fs::write(format!("{}:large", f.target().display()), vec![b'x'; 65537]).unwrap();
    let baseline = read_exact(&f.root, "嵌套目录/文件.txt").unwrap();
    assert!(Prepared::new(&f.root, "嵌套目录/文件.txt", &baseline, b"candidate").is_err());
    assert_eq!(
        fs::read_dir(f.target().parent().unwrap()).unwrap().count(),
        1
    );
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
}

#[test]
fn changed_parked_ads_are_not_restored_as_the_reviewed_original() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let intent = prepared.intent.clone();
    drop(prepared.record(&f.storage).unwrap().park().unwrap());
    fs::write(
        format!("{}:new-metadata", intent.sibling("original").display()),
        b"later edit",
    )
    .unwrap();
    assert!(recover_missing(&f.storage, &intent.id, &f.root).is_err());
    assert!(!f.target().exists());
}

#[test]
fn unrecorded_candidates_are_cleaned_but_durable_conflicts_are_retained() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let stage = prepared.intent.sibling("candidate");
    fs::write(&f.storage, b"blocked journal directory").unwrap();
    assert!(prepared.record(&f.storage).is_err());
    assert!(!stage.exists());
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
}

#[test]
fn unrecorded_cleanup_removes_only_the_owned_candidate_link() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let stage = prepared.intent.sibling("candidate");
    let outside = f.dir.join("externally-created-alias");
    fs::hard_link(&stage, &outside).unwrap();
    drop(prepared);
    assert!(!stage.exists());
    assert_eq!(fs::read(outside).unwrap(), b"candidate");
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
}

#[test]
fn late_named_stream_change_is_either_locked_out_or_rejects_the_install() {
    let f = Fixture::new();
    let prepared = f.prepare(b"candidate");
    let zone = format!("{}:Zone.Identifier", f.target().display());
    let late = fs::write(&zone, b"user supplied metadata");
    let recorded = prepared.record(&f.storage).unwrap();
    if late.is_ok() {
        assert!(recorded.park().is_err());
        assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
        assert_eq!(fs::read(zone).unwrap(), b"user supplied metadata");
    } else {
        assert_eq!(late.unwrap_err().raw_os_error(), Some(32));
        recorded.park().unwrap().install().unwrap();
        assert_eq!(fs::read(f.target()).unwrap(), b"candidate");
    }
}

#[test]
fn full_journal_directory_refuses_before_source_movement_and_cleans_candidate() {
    let f = Fixture::new();
    fs::create_dir(&f.storage).unwrap();
    for index in 0..64 {
        fs::write(f.storage.join(format!("existing-{index}")), b"preserve").unwrap();
    }
    let prepared = f.prepare(b"candidate");
    let stage = prepared.intent.sibling("candidate");
    assert!(prepared.record(&f.storage).is_err());
    assert!(!stage.exists());
    assert_eq!(fs::read_dir(&f.storage).unwrap().count(), 64);
    assert_eq!(fs::read(f.target()).unwrap(), b"original\r\n");
}

#[test]
fn replacement_preserves_real_extended_attributes() {
    use ::windows::Win32::Storage::FileSystem::BackupWrite;
    let f = Fixture::new();
    let file = OpenOptions::new()
        .read(true)
        .write(true)
        .open(f.target())
        .unwrap();
    // FILE_FULL_EA_INFORMATION: one "opc" = "abc" entry, no next entry.
    let ea = [
        0, 0, 0, 0, 0, 3, 3, 0, b'o', b'p', b'c', 0, b'a', b'b', b'c',
    ];
    let mut stream = Vec::new();
    stream.extend(2u32.to_le_bytes());
    stream.extend(0u32.to_le_bytes());
    stream.extend((ea.len() as i64).to_le_bytes());
    stream.extend(0u32.to_le_bytes());
    stream.extend(ea);
    let mut context = std::ptr::null_mut();
    let mut count = 0;
    // SAFETY: valid stream bytes and isolated fixture handle; context released
    // before assertions even if the fixture operation fails.
    let result = unsafe {
        BackupWrite(
            HANDLE(file.as_raw_handle()),
            &stream,
            &mut count,
            false,
            false,
            &mut context,
        )
    };
    unsafe {
        BackupWrite(
            HANDLE(file.as_raw_handle()),
            &[],
            &mut 0,
            true,
            false,
            &mut context,
        )
    }
    .unwrap();
    result.unwrap();
    assert_eq!(count as usize, stream.len());
    drop(file);
    let intent = f
        .prepare(b"new content")
        .record(&f.storage)
        .unwrap()
        .park()
        .unwrap()
        .install()
        .unwrap();
    let file = File::open(f.target()).unwrap();
    let new_meta = Metadata::capture(&file, b"new content").unwrap();
    assert!(intent.metadata.matches_original(&new_meta));
}
