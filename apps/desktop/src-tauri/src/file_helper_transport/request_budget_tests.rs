//! Deterministic local pipes/deadline inspection; no windows, UAC or project I/O.
use super::*;
use crate::file_operation_contract::{
    CheckedRequest, Content, ObjectIdentity, ObservedFile, Operation, Request, sha256,
};
use std::{sync::mpsc, thread};
const NOW: u64 = 1_000_000;
const TIMEOUT: Duration = Duration::from_secs(10);
fn request() -> CheckedRequest {
    let id = || uuid::Uuid::new_v4().to_string();
    CheckedRequest::from_local(
        Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: NOW,
            expires_at_ms: NOW + 120_000,
            root: r"C:\fixture-never-opened".into(),
            path: "file.txt".into(),
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
                    content: Content::from_bytes(b"before").unwrap(),
                    readable_security_sha256: sha256(b"partial"),
                    metadata_sha256: sha256(b"metadata"),
                },
                candidate: Content::from_bytes(b"after").unwrap(),
            },
        },
        NOW,
    )
    .unwrap()
}
fn me() -> Peer {
    Peer::pin(std::process::id()).unwrap()
}

#[test]
fn review_budget_is_absolute_and_ordinary_connections_stay_bounded() {
    assert!(Listener::new(Duration::from_secs(31), Cancel::new().unwrap()).is_err());
    assert!(
        connect(
            &uuid::Uuid::new_v4().to_string(),
            me(),
            Duration::from_secs(31),
            Cancel::new().unwrap()
        )
        .is_err()
    );
    let request = request();
    let cancel = Cancel::new().unwrap();
    let original = request.wait_deadline(NOW).unwrap();
    let listener = Listener::for_request(&request, NOW, cancel.clone()).unwrap();
    assert!(listener.budget.until <= original);
    assert!(listener.budget.check().unwrap() > 30_000);
    let shortened = Budget::for_request(&request, NOW + 119_000, cancel.clone()).unwrap();
    assert!(shortened.check().unwrap() <= 1000);
    assert!(Budget::for_request(&request, NOW + 120_000, cancel.clone()).is_err());
    cancel.cancel().unwrap();
    assert!(listener.budget.check().is_err());
    assert!(Budget::for_request(&request, NOW, cancel).is_err());
}

#[test]
fn owned_review_observer_retains_deadline_and_detects_session_failure() {
    for mode in [
        "good",
        "closed",
        "extra",
        "cancel",
        "expired",
        "already_expired",
    ] {
        let request = request();
        let bytes = request.encode(NOW).unwrap();
        let binding = Binding::new(&bytes).unwrap();
        let listener = Listener::for_request(&request, NOW, Cancel::new().unwrap()).unwrap();
        let id = listener.id().to_owned();
        let cancel = Cancel::new().unwrap();
        let (ready, received) = mpsc::channel();
        let (changed, inspect) = mpsc::channel();
        thread::scope(|scope| {
            let bytes = &bytes;
            let binding = &binding;
            let client_cancel = cancel.clone();
            let client = scope.spawn(move || {
                let mut incoming = connect(&id, me(), TIMEOUT, client_cancel)
                    .unwrap()
                    .incoming(binding)
                    .unwrap();
                let checked = CheckedRequest::decode(incoming.body(), NOW).unwrap();
                if mode == "already_expired" {
                    incoming.endpoint.budget.until = Instant::now() - Duration::from_millis(1);
                    assert!(incoming.for_review(&checked, NOW).is_err());
                    ready.send(()).unwrap();
                    inspect.recv_timeout(TIMEOUT).unwrap();
                    return;
                }
                let deadline = checked.wait_deadline(NOW).unwrap();
                let (incoming, mut watch) = incoming.for_review(&checked, NOW).unwrap();
                assert_eq!(watch.budget.until, incoming.endpoint.budget.until);
                assert!(watch.budget.until <= deadline);
                assert!(watch.budget.check().unwrap() > 30_000);
                watch.check().unwrap();
                ready.send(()).unwrap();
                inspect.recv_timeout(TIMEOUT).unwrap();
                if mode == "expired" {
                    watch.budget.until = Instant::now() - Duration::from_millis(1);
                }
                if mode == "good" {
                    watch.check().unwrap();
                    assert_eq!(incoming.body(), bytes);
                    drop(watch); // observing did not consume body or reply role
                    incoming.reply(b"one receipt").unwrap();
                } else {
                    assert!(watch.check().is_err(), "{mode}");
                }
            });
            let endpoint = listener.accept(me()).unwrap();
            endpoint.send_request_for_test(binding, bytes).unwrap();
            received.recv_timeout(TIMEOUT).unwrap();
            let mut held = Some(endpoint);
            match mode {
                "closed" => drop(held.take()),
                "extra" => held.as_ref().unwrap().send_extra_for_test().unwrap(),
                "cancel" => cancel.cancel().unwrap(),
                _ => (),
            }
            changed.send(()).unwrap();
            if mode == "good" {
                assert_eq!(
                    binding
                        .decode(false, held.as_ref().unwrap().receive().unwrap())
                        .unwrap(),
                    b"one receipt"
                );
            }
            client.join().unwrap();
        });
    }
}
