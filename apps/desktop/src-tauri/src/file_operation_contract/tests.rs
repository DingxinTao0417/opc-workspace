use super::*;
use serde_json::{Value, json};
const NOW: u64 = 1_000_000;

#[test]
fn monotonic_expiration_is_not_revived_by_a_valid_wall_clock_value() {
    let mut checked = CheckedRequest::from_local(request(), NOW).unwrap();
    checked.deadline = Instant::now() - Duration::from_millis(1);
    assert!(checked.check_time(NOW + 1).is_err());
    assert!(checked.encode(NOW + 1).is_err());
    assert!(checked.wait_deadline(NOW + 1).is_err());
}
#[test]
fn request_bound_wait_never_renews_the_original_monotonic_deadline() {
    let mut facts = request();
    facts.expires_at_ms = NOW + MAX_LIFETIME_MS;
    let mut checked = CheckedRequest::from_local(facts, NOW).unwrap();
    assert!(
        checked
            .wait_deadline(NOW)
            .unwrap()
            .duration_since(Instant::now())
            > Duration::from_secs(30)
    );
    // Model elapsed monotonic time with a still-valid, rolled-back wall clock.
    checked.deadline = Instant::now() + Duration::from_secs(2);
    let original = checked.deadline;
    assert!(checked.wait_deadline(NOW).unwrap() <= original);
    assert!(checked.wait_deadline(NOW + MAX_LIFETIME_MS - 1).unwrap() < original);
    assert!(checked.wait_deadline(NOW + MAX_LIFETIME_MS).is_err());
    assert!(checked.wait_deadline(NOW - 1).is_err());
}
fn id() -> String {
    uuid::Uuid::new_v4().to_string()
}

#[test]
fn terminal_receipts_are_canonical_bounded_and_operation_bound() {
    let operation_id = id();
    let success = OperationReceipt::succeeded(&operation_id).unwrap();
    assert_eq!(
        OperationReceipt::decode(&success, &operation_id).unwrap(),
        OperationReceipt::Succeeded
    );
    let cancelled = OperationReceipt::review_cancelled();
    assert_eq!(
        OperationReceipt::decode(&cancelled, &operation_id).unwrap(),
        OperationReceipt::ReviewCancelled
    );
    assert!(OperationReceipt::decode(&success, &id()).is_err());
    assert!(OperationReceipt::succeeded("not-an-id").is_err());

    let success_text = String::from_utf8(success).unwrap();
    for invalid in [
        success_text.replacen("\"succeeded\"", "\"failed\"", 1),
        success_text.replacen("\"version\":1", "\"version\":2", 1),
        success_text.replacen("}", ",\"approved\":true}", 1),
        format!(" {success_text}"),
    ] {
        assert!(OperationReceipt::decode(invalid.as_bytes(), &operation_id).is_err());
    }
    assert!(OperationReceipt::decode(&vec![b'x'; MAX_RECEIPT_BYTES + 1], &operation_id).is_err());
}

fn file(index: u64, bytes: &[u8]) -> ObservedFile {
    ObservedFile {
        identity: ObjectIdentity { volume: 7, index },
        modified: 13,
        content: Content::from_bytes(bytes).unwrap(),
        readable_security_sha256: sha256(b"partial"),
        metadata_sha256: sha256(b"metadata"),
    }
}
fn request() -> Request {
    Request {
        version: 1,
        operation_id: id(),
        review_id: id(),
        root_selection_id: id(),
        issued_at_ms: NOW,
        expires_at_ms: NOW + 30_000,
        root: r"\\?\D:\workspace\project".into(),
        root_identity: ObjectIdentity {
            volume: 7,
            index: 1,
        },
        parent_identity: ObjectIdentity {
            volume: 7,
            index: 2,
        },
        path: "src/文件.rs".into(),
        operation: Operation::Replace {
            original: file(3, b"\xef\xbb\xbfbefore\r\n"),
            candidate: Content::from_bytes(b"after\n").unwrap(),
        },
    }
}
fn value() -> Value {
    serde_json::to_value(request()).unwrap()
}
fn checked(value: Value) -> Result<CheckedRequest, &'static str> {
    CheckedRequest::decode(&serde_json::to_vec(&value).unwrap(), NOW)
}

#[test]
fn roundtrip_preserves_full_bytes_ids_and_partial_security_label() {
    let initial = request();
    let original = serde_json::to_vec(&initial).unwrap();
    let decoded = CheckedRequest::decode(&original, NOW).unwrap();
    assert_eq!(decoded.encode(NOW).unwrap(), original);
    assert!(decoded.check_time(NOW + 29_999).is_ok());
    assert!(decoded.encode(NOW + 30_000).is_err());
    assert!(decoded.check_time(NOW - 1).is_err());
    assert_eq!(decoded.facts().path, "src/文件.rs");
    let Operation::Replace {
        original,
        candidate,
    } = &decoded.facts().operation
    else {
        panic!()
    };
    assert_eq!(
        original.content.decode().unwrap(),
        b"\xef\xbb\xbfbefore\r\n"
    );
    assert_eq!(candidate.decode().unwrap(), b"after\n");
}

#[test]
fn persisted_validation_preserves_structure_without_reviving_approval() {
    let facts = request();
    assert!(facts.validate_persisted().is_ok());
    let checked = CheckedRequest::from_local(facts, NOW).unwrap();
    assert!(checked.check_time(NOW + 30_000).is_err());
    assert!(checked.facts().validate_persisted().is_ok());
    let mut invalid = request();
    invalid.expires_at_ms = invalid.issued_at_ms + MAX_LIFETIME_MS + 1;
    assert!(invalid.validate_persisted().is_err());
    invalid = request();
    invalid.expires_at_ms = invalid.issued_at_ms;
    assert!(invalid.validate_persisted().is_err());
}

#[test]
fn no_batch_shell_approval_or_unknown_field_can_be_smuggled() {
    for (key, payload) in [
        ("approved", json!(true)),
        ("files", json!([])),
        ("command", json!("powershell")),
        ("full_sacl_verified", json!(true)),
    ] {
        let mut v = value();
        v[key] = payload;
        assert!(checked(v).is_err());
    }
    for kind in [
        "shell",
        "delete",
        "batch",
        "restore",
        "replace_all",
        "Replace",
    ] {
        let mut v = value();
        v["operation"]["kind"] = json!(kind);
        assert!(checked(v).is_err());
    }
    let mut v = value();
    v["operation"]["approved"] = json!(true);
    assert!(checked(v).is_err());
    let mut v = value();
    v["operation"]["original"]["identity"]["path"] = json!("outside");
    assert!(checked(v).is_err());
    let mut v = value();
    v["operation"] = json!([v["operation"].clone()]);
    assert!(checked(v).is_err());
}

#[test]
fn duplicate_keys_and_mixed_operation_shapes_are_rejected() {
    let bytes = serde_json::to_string(&value()).unwrap();
    for replaced in [
        bytes.replacen("\"version\":1", "\"version\":1,\"version\":1", 1),
        bytes.replacen(
            "\"kind\":\"replace\"",
            "\"kind\":\"replace\",\"kind\":\"replace\"",
            1,
        ),
        bytes.replacen("\"modified\":13", "\"modified\":13,\"modified\":13", 1),
        bytes.replacen("\"index\":3", "\"index\":3,\"index\":3", 1),
    ] {
        assert_ne!(replaced, bytes);
        assert!(CheckedRequest::decode(replaced.as_bytes(), NOW).is_err());
    }
    let mut v = value();
    v["operation"]["record_id"] = json!(id());
    assert!(checked(v).is_err());
}

#[test]
fn paths_are_local_canonical_single_file_and_not_git_or_streams() {
    for path in [
        "",
        "../escape",
        "a/./b",
        "a//b",
        "C:/outside",
        "a\\b",
        "file:ads",
        "a/.GIT/config",
        "AUX.txt",
        "x/LPT¹",
        "x/last.",
        "x/last ",
        "x/\0",
        "/absolute",
    ] {
        let mut v = value();
        v["path"] = json!(path);
        assert!(checked(v).is_err(), "{path}");
    }
    for root in [
        r"D:relative",
        r"\\host\share",
        r"\\?\UNC\host\share",
        r"\\.\C:\folder",
        r"D:\root\..\other",
        r"D:\root\",
        "D:/root",
        r"D:\root\.git",
        r"D:\root\NUL",
        "D:\\root\0",
    ] {
        let mut v = value();
        v["root"] = json!(root);
        assert!(checked(v).is_err(), "{root}");
    }
    for root in [r"D:\", r"D:\项目", r"\\?\D:\项目"] {
        let mut v = value();
        v["root"] = json!(root);
        assert!(checked(v).is_ok(), "{root}");
    }
}

#[test]
fn content_size_hash_encoding_and_text_are_not_declarations_of_truth() {
    for (field, data) in [
        ("size", json!(0)),
        ("sha256", json!("0".repeat(64))),
        ("base64", json!("@@@")),
    ] {
        let mut v = value();
        v["operation"]["candidate"][field] = data;
        assert!(checked(v).is_err());
    }
    for bytes in [vec![0], vec![255], b"\xef\xbb\xbfbefore\r\n".to_vec()] {
        let mut v = value();
        v["operation"]["candidate"] =
            serde_json::to_value(Content::from_bytes(&bytes).unwrap()).unwrap();
        assert!(checked(v).is_err());
    }
    let mut v = value();
    v["operation"]["candidate"] = serde_json::to_value(Content::from_bytes(&[]).unwrap()).unwrap();
    assert!(checked(v).is_ok());
    assert!(Content::from_bytes(&vec![0; MAX_FILE_BYTES + 1]).is_err());
    assert!(CheckedRequest::decode(&vec![b' '; MAX_REQUEST_BYTES + 1], NOW).is_err());
}

#[test]
fn full_maximum_pair_fits_transport_without_json_expansion_truncation() {
    let mut r = request();
    r.operation = Operation::Replace {
        original: file(3, &vec![b'a'; MAX_FILE_BYTES]),
        candidate: Content::from_bytes(&vec![b'b'; MAX_FILE_BYTES]).unwrap(),
    };
    let r = CheckedRequest::from_local(r, NOW).unwrap();
    let bytes = r.encode(NOW).unwrap();
    assert!(bytes.len() < MAX_REQUEST_BYTES);
    assert!(CheckedRequest::decode(&bytes, NOW).is_ok());
}

#[test]
fn expiration_invalid_ids_and_cross_volume_facts_fail() {
    for field in ["operation_id", "review_id", "root_selection_id"] {
        let mut v = value();
        v[field] = json!("not-an-id");
        assert!(checked(v).is_err());
    }
    for (issued, expires) in [
        (0, NOW + 1),
        (NOW + 1, NOW + 2),
        (NOW, NOW),
        (NOW, u64::MAX),
        (NOW - 1, NOW),
    ] {
        let mut v = value();
        v["issued_at_ms"] = json!(issued);
        v["expires_at_ms"] = json!(expires);
        assert!(checked(v).is_err());
    }
    let mut v = value();
    v["version"] = json!(2);
    assert!(checked(v).is_err());
    let mut v = value();
    v["parent_identity"]["volume"] = json!(8);
    assert!(checked(v).is_err());
    let mut v = value();
    v["operation"]["original"]["identity"]["volume"] = json!(8);
    assert!(checked(v).is_err());
}

#[test]
fn recovery_modes_preserve_raw_bytes_and_require_distinct_known_objects() {
    for missing in [true, false] {
        let mut r = request();
        let fields = (
            id(),
            sha256(b"frozen record"),
            file(3, b"original"),
            file(4, &[255, 0]),
        );
        r.operation = if missing {
            Operation::RestoreMissing {
                record_version: 2,
                record_id: fields.0,
                record_sha256: fields.1,
                original: fields.2,
                candidate: fields.3,
            }
        } else {
            Operation::UndoInstalled {
                record_version: 2,
                record_id: fields.0,
                record_sha256: fields.1,
                original: fields.2,
                candidate: fields.3,
            }
        };
        let v = serde_json::to_value(r).unwrap();
        assert!(checked(v.clone()).is_ok());
        let mut legacy = v.clone();
        legacy["operation"]["record_version"] = json!(1);
        assert!(checked(legacy).is_err());
        for field in ["record_id", "record_sha256"] {
            let mut bad = v.clone();
            bad["operation"][field] = json!("");
            assert!(checked(bad).is_err());
        }
        let mut bad = v.clone();
        bad["operation"]["candidate"]["identity"] =
            bad["operation"]["original"]["identity"].clone();
        assert!(checked(bad).is_err());
        let mut bad = v.clone();
        bad["operation"]["candidate"]["identity"]["volume"] = json!(8);
        assert!(checked(bad).is_err());
    }
}
