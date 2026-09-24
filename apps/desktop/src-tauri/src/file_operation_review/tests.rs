use super::*;
use crate::file_operation_contract::{
    MAX_FILE_BYTES, MAX_REQUEST_BYTES, ObjectIdentity, ObservedFile, Request,
};
use serde_json::{Value, json};
const NOW: u64 = 1_000_000;

#[test]
fn native_plain_text_escapes_invisible_characters_without_executing_markup() {
    let pending = PendingReview::new(
        request(
            "replace",
            "\u{feff}中\r\n\u{202e}<script>\t".as_bytes(),
            b"after\n",
        ),
        NOW,
    )
    .unwrap();
    let text = pending.document.native_text().unwrap();
    assert!(text.difference.contains("\\ufeff中\\r\\n"));
    assert!(text.difference.contains("\\u202e<script>\\t"));
    assert!(!text.difference.contains('\u{202e}'));
    assert!(text.retained.contains("保留的原对象"));
}
#[test]
fn native_recovery_text_shows_missing_empty_and_binary_without_loss() {
    let pending =
        PendingReview::new(request("restore_missing", b"", &[0, 255, 13, 10]), NOW).unwrap();
    let text = pending.document.native_text().unwrap();
    assert!(text.difference.contains("目标路径不存在，不是空文件"));
    assert!(text.difference.contains("+ [空文件，0 字节]"));
    assert!(text.retained.contains("00 ff 0d 0a"));
    assert!(text.summary.contains("不安装候选"));
}
fn id() -> String {
    uuid::Uuid::new_v4().to_string()
}
fn observed(index: u64, bytes: &[u8]) -> ObservedFile {
    ObservedFile {
        identity: ObjectIdentity { volume: 7, index },
        modified: 3,
        content: Content::from_bytes(bytes).unwrap(),
        metadata_sha256: sha256(b"metadata"),
        readable_security_sha256: sha256(b"partial"),
    }
}
fn request(mode: &str, before: &[u8], after: &[u8]) -> CheckedRequest {
    let original = observed(3, before);
    let candidate = observed(4, after);
    let operation = match mode {
        "replace" => Operation::Replace {
            original,
            candidate: candidate.content,
        },
        "restore_missing" => Operation::RestoreMissing {
            record_version: 2,
            record_id: id(),
            record_sha256: sha256(b"record"),
            original,
            candidate,
        },
        "undo_installed" => Operation::UndoInstalled {
            record_version: 2,
            record_id: id(),
            record_sha256: sha256(b"record"),
            original,
            candidate,
        },
        _ => panic!(),
    };
    CheckedRequest::from_local(
        Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: NOW,
            expires_at_ms: NOW + 30_000,
            root: r"D:\project".into(),
            path: "file.txt".into(),
            root_identity: ObjectIdentity {
                volume: 7,
                index: 1,
            },
            parent_identity: ObjectIdentity {
                volume: 7,
                index: 2,
            },
            operation,
        },
        NOW,
    )
    .unwrap()
}
fn document(pending: &PendingReview) -> Value {
    serde_json::to_value(pending.document(NOW).unwrap().0).unwrap()
}

#[test]
fn presentation_has_exact_bytes_and_operation_specific_direction() {
    for (mode, from, to) in [
        ("replace", "original", "candidate"),
        ("restore_missing", "missing", "original"),
        ("undo_installed", "candidate", "original"),
    ] {
        let request = request(mode, b"\xef\xbb\xbfbefore\r\n", b"after\n");
        let hash = sha256(&request.encode(NOW).unwrap());
        let pending = PendingReview::new(request, NOW).unwrap();
        let doc = document(&pending);
        assert_eq!(doc["operation"], mode);
        assert_eq!(doc["from"], from);
        assert_eq!(doc["to"], to);
        assert_eq!(doc["requestSha256"], hash);
        assert_eq!(
            doc["retained"],
            if mode == "replace" {
                "original"
            } else {
                "candidate"
            }
        );
        assert_eq!(
            pending.document.original.decode().unwrap(),
            b"\xef\xbb\xbfbefore\r\n"
        );
        assert_eq!(pending.document.candidate.decode().unwrap(), b"after\n");
    }
}
#[test]
fn missing_and_empty_are_distinct_and_binary_recovery_is_lossless() {
    let pending =
        PendingReview::new(request("restore_missing", b"", &[0, 255, 13, 10]), NOW).unwrap();
    let doc = document(&pending);
    assert_eq!(doc["from"], "missing");
    assert_eq!(doc["original"]["size"], 0);
    assert_eq!(
        pending.document.candidate.decode().unwrap(),
        &[0, 255, 13, 10]
    );
}
#[test]
fn maximum_pair_is_neither_duplicated_nor_truncated() {
    for mode in ["replace", "restore_missing", "undo_installed"] {
        let pending = PendingReview::new(
            request(
                mode,
                &vec![b'a'; MAX_FILE_BYTES],
                &vec![b'b'; MAX_FILE_BYTES],
            ),
            NOW,
        )
        .unwrap();
        let bytes = serde_json::to_vec(pending.document(NOW).unwrap().0).unwrap();
        assert!(bytes.len() <= MAX_REQUEST_BYTES);
        assert_eq!(
            pending.document.original.decode().unwrap().len(),
            MAX_FILE_BYTES
        );
        assert_eq!(
            pending.document.candidate.decode().unwrap().len(),
            MAX_FILE_BYTES
        );
    }
}
#[test]
fn acknowledged_document_moves_exact_request_without_new_expiration() {
    let request = request("replace", b"before", b"after");
    let expected = request.encode(NOW).unwrap();
    let root = request.facts().root_selection_id.clone();
    let pending = PendingReview::new(request, NOW).unwrap();
    let digest = pending.document_sha256.clone();
    let intent = pending
        .decide(Decision::Continue, &digest, &root, NOW + 1)
        .unwrap()
        .unwrap();
    assert_eq!(intent.document_sha256(), digest);
    let request = intent.into_unapproved_request(NOW + 2).unwrap();
    assert_eq!(request.encode(NOW + 2).unwrap(), expected);
    assert!(request.check_time(NOW + 30_000).is_err());
}
#[test]
fn cancel_wrong_selection_wrong_document_and_expiry_produce_no_handoff() {
    for failure in ["cancel", "root", "digest", "expired"] {
        let request = request("replace", b"before", b"after");
        let root = request.facts().root_selection_id.clone();
        let pending = PendingReview::new(request, NOW).unwrap();
        let digest = pending.document_sha256.clone();
        let result = pending.decide(
            if failure == "cancel" {
                Decision::Cancel
            } else {
                Decision::Continue
            },
            if failure == "digest" {
                "other"
            } else {
                &digest
            },
            if failure == "root" { "other" } else { &root },
            if failure == "expired" {
                NOW + 30_000
            } else {
                NOW
            },
        );
        if failure == "cancel" {
            assert!(result.unwrap().is_none());
        } else {
            assert!(result.is_err());
        }
    }
}
#[test]
fn all_hidden_request_facts_are_in_the_presentation_binding() {
    let bytes = request("undo_installed", b"before", b"after")
        .encode(NOW)
        .unwrap();
    let pending = PendingReview::new(CheckedRequest::decode(&bytes, NOW).unwrap(), NOW).unwrap();
    let old_digest = pending.document_sha256.clone();
    for field in [
        "modified",
        "metadata_sha256",
        "readable_security_sha256",
        "record_sha256",
    ] {
        let mut value: Value = serde_json::from_slice(&bytes).unwrap();
        if field == "record_sha256" {
            value["operation"][field] = json!(sha256(b"changed"));
        } else {
            value["operation"]["original"][field] = if field == "modified" {
                json!(4)
            } else {
                json!(sha256(b"changed"))
            };
        }
        let changed = CheckedRequest::decode(&serde_json::to_vec(&value).unwrap(), NOW).unwrap();
        let root = changed.facts().root_selection_id.clone();
        let changed = PendingReview::new(changed, NOW).unwrap();
        assert!(
            changed
                .decide(Decision::Continue, &old_digest, &root, NOW)
                .is_err(),
            "{field}"
        );
    }
}
