use super::*;
use crate::{
    file_operation_contract::{Content, ObservedFile, Request},
    file_operation_metadata::Metadata,
    file_operation_security::SecurityDescriptor,
    file_recovery_record::Intent,
};
use std::{fs, path::Path};

struct Fixture {
    dir: PathBuf,
    location: RecoveryLocation,
    intent: Intent,
}
impl Fixture {
    fn new() -> Self {
        let dir =
            std::env::temp_dir().join(format!("opc-helper-record-test-{}", uuid::Uuid::new_v4()));
        let root = dir.join("project");
        let storage = dir.join("records");
        fs::create_dir_all(&root).unwrap();
        fs::create_dir(&storage).unwrap();
        fs::write(root.join("file.txt"), b"before").unwrap();
        fs::write(root.join("candidate.txt"), b"after").unwrap();
        let mut source = File::open(root.join("file.txt")).unwrap();
        let mut candidate = File::open(root.join("candidate.txt")).unwrap();
        let (_, source_identity, _) = read_regular(&mut source, 1024).unwrap();
        let (_, candidate_identity, _) = read_regular(&mut candidate, 1024).unwrap();
        let root_identity = PinnedDirectory::open(&root, false).unwrap().identity;
        let intent = Intent {
            version: 2,
            id: uuid::Uuid::new_v4().to_string(),
            root,
            path: "file.txt".into(),
            root_identity: root_identity.clone(),
            parent_identity: root_identity,
            source_identity,
            candidate_identity,
            before: b"before".to_vec(),
            after: b"after".to_vec(),
            metadata: Metadata::capture(&source, b"before").unwrap(),
            security: SecurityDescriptor::capture(&source)
                .unwrap()
                .bytes()
                .to_vec(),
        };
        let result = Self {
            dir,
            location: RecoveryLocation(storage),
            intent,
        };
        result.write();
        result
    }
    fn path(&self) -> PathBuf {
        self.location.0.join(format!("{}.json", self.intent.id))
    }
    fn stored(&self) -> serde_json::Value {
        serde_json::json!({"intent": self.intent, "digest": self.intent.digest().unwrap()})
    }
    fn write(&self) {
        fs::write(self.path(), serde_json::to_vec(&self.stored()).unwrap()).unwrap();
    }
    fn request(&self, undo: bool) -> CheckedRequest {
        // These are request/record consistency fixtures, NOT a claim that the
        // target files currently occupy valid recovery positions or match all
        // snapshot metadata. Independent DiskReview must prove that separately.
        let original = ObservedFile {
            identity: self.intent.source_identity.clone(),
            modified: 1,
            content: Content::from_bytes(&self.intent.before).unwrap(),
            metadata_sha256: self.intent.metadata.review_fingerprint().unwrap(),
            readable_security_sha256: sha256(&self.intent.security),
        };
        let candidate = ObservedFile {
            identity: self.intent.candidate_identity.clone(),
            modified: 2,
            content: Content::from_bytes(&self.intent.after).unwrap(),
            metadata_sha256: self.intent.metadata.candidate_review_fingerprint().unwrap(),
            readable_security_sha256: sha256(&self.intent.security),
        };
        let operation = if undo {
            Operation::UndoInstalled {
                record_version: 2,
                record_id: self.intent.id.clone(),
                record_sha256: self.intent.digest().unwrap(),
                original,
                candidate,
            }
        } else {
            Operation::RestoreMissing {
                record_version: 2,
                record_id: self.intent.id.clone(),
                record_sha256: self.intent.digest().unwrap(),
                original,
                candidate,
            }
        };
        let at = now().unwrap();
        CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: uuid::Uuid::new_v4().to_string(),
                review_id: uuid::Uuid::new_v4().to_string(),
                root_selection_id: uuid::Uuid::new_v4().to_string(),
                issued_at_ms: at,
                expires_at_ms: at + 60_000,
                root: self.intent.root.to_str().unwrap().into(),
                path: self.intent.path.clone(),
                root_identity: self.intent.root_identity.clone(),
                parent_identity: self.intent.parent_identity.clone(),
                operation,
            },
            at,
        )
        .unwrap()
    }
    fn replace_request(&self) -> CheckedRequest {
        let at = now().unwrap();
        CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: self.intent.id.clone(),
                review_id: uuid::Uuid::new_v4().to_string(),
                root_selection_id: uuid::Uuid::new_v4().to_string(),
                issued_at_ms: at,
                expires_at_ms: at + 60_000,
                root: self.intent.root.to_str().unwrap().into(),
                path: self.intent.path.clone(),
                root_identity: self.intent.root_identity.clone(),
                parent_identity: self.intent.parent_identity.clone(),
                operation: Operation::Replace {
                    original: ObservedFile {
                        identity: self.intent.source_identity.clone(),
                        modified: 1,
                        content: Content::from_bytes(&self.intent.before).unwrap(),
                        metadata_sha256: self.intent.metadata.review_fingerprint().unwrap(),
                        readable_security_sha256: sha256(&self.intent.security),
                    },
                    candidate: Content::from_bytes(&self.intent.after).unwrap(),
                },
            },
            at,
        )
        .unwrap()
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
                .starts_with("opc-helper-record-test-")
        );
        fs::remove_dir_all(&self.dir).unwrap();
    }
}

#[test]
fn known_folder_location_is_same_user_fixed_and_matches_desktop_identifier() {
    let parent = Peer::pin(std::process::id()).unwrap();
    parent.require_same_user().unwrap();
    let location = RecoveryLocation::for_parent(&parent).unwrap();
    assert_eq!(location.0, current_store().unwrap());
    assert!(location.0.ends_with(Path::new(APP_ID).join(DIRECTORY)));
    let config: serde_json::Value =
        serde_json::from_str(include_str!("../../tauri.conf.json")).unwrap();
    assert_eq!(config["identifier"], APP_ID);
    // No files in the real profile are read, listed, created or changed here.
}
#[test]
fn matching_v2_records_are_held_readonly_for_both_recovery_directions() {
    for undo in [false, true] {
        let f = Fixture::new();
        let request = f.request(undo);
        let mut held = f.location.inspect(&request).unwrap().unwrap();
        assert!(fs::write(f.path(), b"cannot rewrite").is_err());
        assert!(fs::rename(&f.location.0, f.dir.join("moved")).is_err());
        held.revalidate(&request).unwrap();
        drop(held);
        fs::rename(&f.location.0, f.dir.join("released")).unwrap();
        assert_eq!(fs::read(f.intent.root.join("file.txt")).unwrap(), b"before");
    }
}
#[test]
fn independent_record_check_rejects_every_frozen_request_mismatch() {
    let f = Fixture::new();
    for field in [
        "digest",
        "root",
        "path",
        "root_id",
        "parent_id",
        "original_id",
        "candidate_id",
        "original_body",
        "candidate_body",
        "metadata",
        "candidate_metadata",
        "security",
        "candidate_security",
    ] {
        let original = f.request(false);
        let mut value = serde_json::to_value(original.facts()).unwrap();
        match field {
            "digest" => value["operation"]["record_sha256"] = sha256(b"wrong").into(),
            "root" => value["root"] = f.dir.to_str().unwrap().into(),
            "path" => value["path"] = "elsewhere.txt".into(),
            "root_id" => value["root_identity"]["index"] = 1.into(),
            "parent_id" => value["parent_identity"]["index"] = 1.into(),
            "original_id" => value["operation"]["original"]["identity"]["index"] = 1.into(),
            "candidate_id" => value["operation"]["candidate"]["identity"]["index"] = 1.into(),
            "original_body" => {
                value["operation"]["original"]["content"] =
                    serde_json::to_value(Content::from_bytes(b"changed original").unwrap()).unwrap()
            }
            "candidate_body" => {
                value["operation"]["candidate"]["content"] =
                    serde_json::to_value(Content::from_bytes(b"changed candidate").unwrap())
                        .unwrap()
            }
            "metadata" => {
                value["operation"]["original"]["metadata_sha256"] = sha256(b"wrong").into()
            }
            "candidate_metadata" => {
                value["operation"]["candidate"]["metadata_sha256"] = sha256(b"wrong").into()
            }
            "security" => {
                value["operation"]["original"]["readable_security_sha256"] = sha256(b"wrong").into()
            }
            _ => {
                value["operation"]["candidate"]["readable_security_sha256"] =
                    sha256(b"wrong").into()
            }
        }
        let request =
            CheckedRequest::decode(&serde_json::to_vec(&value).unwrap(), now().unwrap()).unwrap();
        assert!(f.location.inspect(&request).is_err(), "{field}");
    }
}
#[test]
fn corrupt_oversized_duplicate_or_coherently_rewritten_record_is_not_accepted() {
    let f = Fixture::new();
    let request = f.request(false);
    let encoded = serde_json::to_string(&f.stored()).unwrap();
    let mut extended = f.stored();
    extended["extra"] = true.into();
    let mut rewritten = f.stored();
    rewritten["intent"]["after"] = serde_json::json!([99]);
    let changed: Intent = serde_json::from_value(rewritten["intent"].clone()).unwrap();
    rewritten["digest"] = changed.digest().unwrap().into();
    for bytes in [
        vec![],
        vec![b'x'; MAX_RECORD_BYTES + 1],
        br#"{"approved":true}"#.to_vec(),
        encoded
            .replacen("\"version\":2", "\"version\":2,\"version\":2", 1)
            .into_bytes(),
        serde_json::to_vec(&extended).unwrap(),
        serde_json::to_vec(&rewritten).unwrap(),
    ] {
        fs::write(f.path(), bytes).unwrap();
        assert!(f.location.inspect(&request).is_err());
    }
}
#[test]
fn missing_store_is_not_created_and_new_replace_does_not_read_records() {
    let f = Fixture::new();
    let request = f.request(false);
    let absent = RecoveryLocation(f.dir.join("missing-store"));
    assert!(absent.inspect(&request).is_err());
    assert!(!absent.0.exists());
    let mut value = serde_json::to_value(request.facts()).unwrap();
    value["operation"] = serde_json::json!({"kind":"replace", "original":value["operation"]["original"], "candidate":Content::from_bytes(b"new text").unwrap()});
    let replace =
        CheckedRequest::decode(&serde_json::to_vec(&value).unwrap(), now().unwrap()).unwrap();
    assert!(absent.inspect(&replace).unwrap().is_none());
    assert!(!absent.0.exists());
}
#[test]
fn new_replace_intent_is_created_once_and_read_back_before_start() {
    let f = Fixture::new();
    let location = RecoveryLocation(f.dir.join("new-records"));
    let request = f.replace_request();
    let mut held = location.create_replace(&request, &f.intent).unwrap();
    let path = location.0.join(format!("{}.json", f.intent.id));
    assert!(fs::write(&path, b"cannot rewrite").is_err());
    assert!(fs::rename(&location.0, f.dir.join("moved-records")).is_err());
    held.revalidate(&request).unwrap();
    drop(held);
    assert_eq!(
        crate::file_recovery_record::decode(&fs::read(&path).unwrap(), &f.intent.id).unwrap(),
        f.intent
    );
    assert!(location.create_replace(&request, &f.intent).is_err());
    assert_eq!(fs::read(f.intent.root.join("file.txt")).unwrap(), b"before");
    assert_eq!(
        fs::read(f.intent.root.join("candidate.txt")).unwrap(),
        b"after"
    );
}
#[test]
fn mismatched_replace_intent_creates_no_store_or_project_write() {
    let f = Fixture::new();
    let location = RecoveryLocation(f.dir.join("rejected-records"));
    let request = f.replace_request();
    let mut changed = f.intent.clone();
    changed.after = b"unreviewed".to_vec();
    assert!(location.create_replace(&request, &changed).is_err());
    assert!(!location.0.exists());
    assert_eq!(fs::read(f.intent.root.join("file.txt")).unwrap(), b"before");
    assert_eq!(
        fs::read(f.intent.root.join("candidate.txt")).unwrap(),
        b"after"
    );
}
#[test]
fn late_record_alias_invalidates_the_held_observation() {
    let f = Fixture::new();
    let request = f.request(false);
    let mut held = f.location.inspect(&request).unwrap().unwrap();
    fs::hard_link(f.path(), f.dir.join("late-alias.json")).unwrap();
    assert!(held.revalidate(&request).is_err());
}
