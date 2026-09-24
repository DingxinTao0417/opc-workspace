use super::*;

#[test]
fn request_fingerprint_matches_stable_metadata_not_access_time() {
    let mut meta = Metadata {
        created: 17,
        accessed: 29,
        attributes: 0x20,
        extra_streams: Vec::new(),
    };
    let initial = meta.review_fingerprint().unwrap();
    meta.accessed += 10;
    assert_eq!(meta.review_fingerprint().unwrap(), initial);
    meta.created += 1;
    assert_ne!(meta.review_fingerprint().unwrap(), initial);
    meta.created -= 1;
    meta.attributes |= 2;
    assert_ne!(meta.review_fingerprint().unwrap(), initial);
    meta.attributes = 0x80; // NORMAL source becomes an ARCHIVE candidate.
    let candidate_digest = meta.candidate_review_fingerprint().unwrap();
    let mut candidate = meta.clone();
    candidate.attributes = 0x20;
    candidate.accessed += 50;
    assert!(meta.matches_candidate(&candidate));
    assert_eq!(candidate_digest, candidate.review_fingerprint().unwrap());
    assert_eq!(meta.attributes, 0x80); // deriving a fingerprint never mutates source facts
}

fn stream(kind: u32, name: &str, body: &[u8]) -> Vec<u8> {
    let mut value = Vec::new();
    value.extend(kind.to_le_bytes());
    value.extend(0u32.to_le_bytes());
    value.extend((body.len() as i64).to_le_bytes());
    value.extend(((name.encode_utf16().count() * 2) as u32).to_le_bytes());
    for unit in name.encode_utf16() {
        value.extend(unit.to_le_bytes());
    }
    value.extend(body);
    value
}

#[test]
fn strictly_preserves_only_bounded_ea_and_named_data_streams() {
    let data = stream(1, "", b"original");
    let ads = stream(
        4,
        ":Zone.Identifier:$DATA",
        b"[ZoneTransfer]\r\nZoneId=3\r\n",
    );
    let ea = stream(2, "", b"opaque EA bytes");
    let mut expected = vec![ads.clone(), ea.clone()];
    expected.sort();
    assert_eq!(
        parse_streams(&[data, ads, ea].concat(), b"original").unwrap(),
        expected
    );
    assert!(parse_streams(&stream(1, "", b"drift"), b"original").is_err());
    assert!(parse_streams(&[], b"nonempty").is_err());
    assert!(parse_streams(&[], b"").unwrap().is_empty());
}

#[test]
fn unsupported_truncated_and_redirecting_streams_fail_closed() {
    for kind in [0, 3, 6, 7, 8, 9, 10, 999] {
        assert!(parse_streams(&stream(kind, "", b"payload"), b"").is_err());
    }
    for name in [
        "::\u{0}$DATA",
        "::$DATA",
        ":../other:$DATA",
        ":a\\b:$DATA",
        ":name:$INDEX_ALLOCATION",
    ] {
        assert!(
            parse_streams(&stream(4, name, b"x"), b"").is_err(),
            "{name:?}"
        );
    }
    let valid = stream(4, ":data:$DATA", b"xyz");
    for end in 1..valid.len() {
        assert!(parse_streams(&valid[..end], b"").is_err());
    }
    let mut oversized = stream(4, ":data:$DATA", &vec![0; MAX_EXTRA_BYTES]);
    assert!(parse_streams(&oversized, b"").is_err());
    oversized[8..16].copy_from_slice(&(-1i64).to_le_bytes());
    assert!(parse_streams(&oversized, b"").is_err());
    assert!(parse_streams(&valid.repeat(MAX_STREAMS + 1), b"").is_err());
}

#[test]
fn link_metadata_is_not_replayed_into_the_replacement() {
    assert!(
        parse_streams(&stream(5, "", b"unrelated path"), b"")
            .unwrap()
            .is_empty()
    );
    let metadata = Metadata {
        created: 1,
        accessed: 1,
        attributes: 0x20,
        extra_streams: vec![stream(5, "", b"unrelated path")],
    };
    assert!(metadata.validate().is_err());
}

#[test]
fn unsupported_file_attributes_are_not_silently_lost() {
    for attribute in [0x1, 0x10, 0x200, 0x400, 0x800, 0x1000, 0x4000, 0x40000] {
        assert!(
            Metadata {
                created: 1,
                accessed: 1,
                attributes: attribute,
                extra_streams: Vec::new()
            }
            .validate()
            .is_err()
        );
    }
}

#[test]
fn frozen_metadata_roundtrips_canonically_and_rejects_smuggled_streams() {
    let mut extras = vec![
        stream(4, ":Zone.Identifier:$DATA", b"zone"),
        stream(2, "", b"ea"),
    ];
    extras.sort();
    let metadata = Metadata {
        created: 17,
        accessed: 29,
        attributes: 0x20,
        extra_streams: extras,
    };
    let bytes = metadata.freeze().unwrap();
    assert_eq!(Metadata::thaw(&bytes).unwrap(), metadata);
    let mut extended = bytes[..bytes.len() - 1].to_vec();
    extended.extend_from_slice(b",\"approved\":true}");
    assert!(Metadata::thaw(&extended).is_err());
    let mut duplicate = metadata.clone();
    duplicate
        .extra_streams
        .push(duplicate.extra_streams[0].clone());
    assert!(duplicate.freeze().is_err());
    let mut link = metadata;
    link.extra_streams = vec![stream(5, "", b"outside")];
    assert!(link.freeze().is_err());
}
