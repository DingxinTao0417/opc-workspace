use super::super::tests::{ChildFixture, Fixture};
use super::*;
use crate::file_operation_contract::{
    Content, ObjectIdentity, ObservedFile, Operation, Request, sha256,
};
use crate::file_operation_review::Decision;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

fn request(time: u64) -> CheckedRequest {
    let id = || uuid::Uuid::new_v4().to_string();
    CheckedRequest::from_local(
        Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: time - 1,
            expires_at_ms: time + 60_000,
            root: r"C:\explicit-test-project".into(),
            path: "example.txt".into(),
            root_identity: ObjectIdentity {
                volume: 1,
                index: 1,
            },
            parent_identity: ObjectIdentity {
                volume: 1,
                index: 1,
            },
            operation: Operation::Replace {
                original: ObservedFile {
                    identity: ObjectIdentity {
                        volume: 1,
                        index: 2,
                    },
                    modified: 1,
                    content: Content::from_bytes(b"original test bytes").unwrap(),
                    readable_security_sha256: sha256(b"partial security"),
                    metadata_sha256: sha256(b"metadata"),
                },
                candidate: Content::from_bytes(b"replacement test bytes").unwrap(),
            },
        },
        time,
    )
    .unwrap()
}
fn simulated(request: CheckedRequest, time: u64) -> NativeReviewedRequest {
    let root = request.facts().root_selection_id.clone();
    let review = PendingReview::new(request, time).unwrap();
    let digest = review.document(time).unwrap().1.to_owned();
    NativeReviewedRequest::simulated(
        review
            .decide(Decision::Continue, &digest, &root, time)
            .unwrap()
            .unwrap(),
    )
}

#[test]
fn invalid_scope_or_expiry_never_reaches_launch() {
    for expired in [false, true] {
        let f = Fixture::new();
        let image = f.executable_pin();
        let time = now().unwrap();
        let reviewed = simulated(request(time), time);
        let called = AtomicBool::new(false);
        let result = exchange_once(
            image,
            reviewed,
            &|_| {
                if expired {
                    Ok(())
                } else {
                    Err("selection revoked".into())
                }
            },
            || Ok(if expired { time + 60_000 } else { time }),
            |_, _, _| {
                called.store(true, Ordering::Release);
                Err("must not launch".into())
            },
        );
        assert!(result.is_err());
        assert!(!called.load(Ordering::Acquire));
    }
}

#[test]
fn launcher_failure_is_consumed_without_retry_or_content_in_arguments() {
    let f = Fixture::new();
    let time = now().unwrap();
    let reviewed = simulated(request(time), time);
    let attempts = AtomicUsize::new(0);
    let result = exchange_once(
        f.executable_pin(),
        reviewed,
        &|_| Ok(()),
        || Ok(time),
        |_, listener, binding| {
            attempts.fetch_add(1, Ordering::AcqRel);
            let args = binding
                .bootstrap_parameters(listener.id(), std::process::id(), 1)
                .unwrap();
            let words: Vec<_> = args.split(' ').collect();
            assert_eq!(words.len(), 6);
            assert_eq!(words[0], "--review-v1");
            assert_eq!(words[1], listener.id());
            assert_eq!(words[3], "1");
            assert_eq!(words[4], binding.fixture_nonce());
            assert_eq!(words[5], binding.fixture_digest());
            assert!(!args.contains("example.txt") && !args.contains("replacement"));
            assert!(!args.contains('\\') && !args.contains('"'));
            Err("simulated UAC cancel".into())
        },
    );
    assert_eq!(result.unwrap_err(), "simulated UAC cancel");
    assert_eq!(attempts.load(Ordering::Acquire), 1);
}

#[test]
fn delayed_launch_rechecks_scope_and_original_deadline_before_sending() {
    for expire in [false, true] {
        let f = Fixture::new();
        let time = now().unwrap();
        let reviewed = simulated(request(time), time);
        let launched = AtomicBool::new(false);
        let mut child = None;
        let result = exchange_once(
            f.executable_pin(),
            reviewed,
            &|_| {
                if !expire && launched.load(Ordering::Acquire) {
                    Err("scope changed".into())
                } else {
                    Ok(())
                }
            },
            || {
                Ok(if expire && launched.load(Ordering::Acquire) {
                    time + 60_000
                } else {
                    time
                })
            },
            |image, listener, binding| {
                let owned = ChildFixture::start(image.path(), "idle", listener, binding);
                let handle = owned.handle();
                child = Some(owned);
                launched.store(true, Ordering::Release);
                Ok(handle)
            },
        );
        assert!(result.is_err());
        child.as_mut().unwrap().finish();
    }
}

#[test]
fn native_handoff_reaches_exact_owned_process_and_preserves_request_bytes() {
    let f = Fixture::new();
    let time = now().unwrap();
    let req = request(time);
    let bytes = req.encode(time).unwrap();
    let expected_root = req.facts().root_selection_id.clone();
    let reviewed = simulated(req, time);
    let mut child = None;
    let result = exchange_once(
        f.executable_pin(),
        reviewed,
        &|root| {
            assert_eq!(root, expected_root);
            Ok(())
        },
        || Ok(time),
        |image, listener, binding| {
            let owned = ChildFixture::start(image.path(), "review_exchange", listener, binding);
            let handle = owned.handle();
            child = Some(owned);
            Ok(handle)
        },
    )
    .unwrap();
    assert_eq!(result, bytes);
    child.as_mut().unwrap().finish();
}

#[test]
fn live_scope_watcher_cancels_a_child_that_never_connects() {
    let f = Fixture::new();
    let time = now().unwrap();
    let reviewed = simulated(request(time), time);
    let checks = AtomicUsize::new(0);
    let mut child = None;
    let start = std::time::Instant::now();
    let result = exchange_once(
        f.executable_pin(),
        reviewed,
        &|_| {
            if checks.fetch_add(1, Ordering::AcqRel) >= 20 {
                Err("revoked while waiting".into())
            } else {
                Ok(())
            }
        },
        || Ok(time),
        |image, listener, binding| {
            let owned = ChildFixture::start(image.path(), "idle", listener, binding);
            let handle = owned.handle();
            child = Some(owned);
            Ok(handle)
        },
    );
    assert!(result.is_err());
    assert!(checks.load(Ordering::Acquire) >= 21);
    assert!(start.elapsed() < Duration::from_secs(10)); // not the 30s pipe timeout
    child.as_mut().unwrap().finish();
}

#[test]
fn bootstrap_rejects_injection_and_has_no_runtime_command_override() {
    let binding = Binding::new(b"test bytes").unwrap();
    for id in [
        "",
        "--option",
        "a b",
        "\"x\"",
        "x;cmd",
        "00000000-0000-0000-0000-00000000000X",
    ] {
        assert!(binding.bootstrap_parameters(id, 1, 1).is_err());
    }
    assert!(
        binding
            .bootstrap_parameters(&uuid::Uuid::new_v4().to_string(), 0, 1)
            .is_err()
    );
    assert!(wide_path(std::path::Path::new("a\0b")).is_err());
}
