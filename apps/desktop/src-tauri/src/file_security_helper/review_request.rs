//! Composes OS-bound transport with the shared single-file grammar. A received
//! request remains UNAPPROVED and unverified against disk. No privilege use here.
use super::{
    contract::CheckedRequest,
    transport::{
        native::{ConnectionWatch, Endpoint, Incoming},
        wire::Binding,
    },
};
use crate::{
    file_operation_contract::{OperationReceipt, sha256},
    file_operation_review::{
        PendingReview,
        native::{self, NativeReviewedRequest},
    },
};
use std::time::{SystemTime, UNIX_EPOCH};

pub(super) struct ReceivedRequest<'a> {
    incoming: Incoming<'a>,
    request: CheckedRequest,
}
pub(super) struct ReceivedDiskReview<'r, 'a> {
    received: &'r ReceivedRequest<'a>,
    disk: crate::file_operation_disk::DiskReview<'r>,
}
impl ReceivedDiskReview<'_, '_> {
    pub(super) fn revalidate(&mut self) -> Result<(), String> {
        self.received.unapproved()?;
        self.disk.revalidate()?;
        self.received.unapproved()?;
        Ok(())
    }
}
fn now() -> Result<u64, &'static str> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围")
}
impl<'a> ReceivedRequest<'a> {
    pub(super) fn receive(channel: Endpoint, binding: &'a Binding) -> Result<Self, &'static str> {
        let incoming = channel.incoming(binding)?;
        let request = CheckedRequest::decode(incoming.body(), now()?)?;
        // Parsing can take time. Recheck peer/cancel/deadline after it, too.
        incoming.check()?;
        request.check_time(now()?)?;
        Ok(Self { incoming, request })
    }
    pub(super) fn unapproved(&self) -> Result<&CheckedRequest, &'static str> {
        self.incoming.check()?;
        self.request.check_time(now()?)?;
        Ok(&self.request)
    }
    pub(super) fn native_review(self) -> Result<Option<Self>, String> {
        self.review_with(|pending, watch| {
            native::show(pending, move || watch.check().map_err(str::to_owned))
        })
    }
    // Private injection point, not a configurable renderer or WebView approval
    // adapter. Production always uses the complete independent native window.
    fn review_with(
        self,
        render: impl FnOnce(
            PendingReview,
            ConnectionWatch,
        ) -> Result<Option<NativeReviewedRequest>, String>,
    ) -> Result<Option<Self>, String> {
        let digest = sha256(&self.unapproved()?.encode(now()?)?);
        let Self { incoming, request } = self;
        let valid_wall_time = request.facts().issued_at_ms..request.facts().expires_at_ms;
        let (incoming, watch) = incoming.for_review(&request, now()?)?;
        let pending = PendingReview::new(request, now()?)?;
        let Some(reviewed) = render(pending, watch)? else {
            // This confirms ONLY canceled review. Never reports a rollback or
            // file outcome. If the channel/deadline failed, the caller gets Err.
            if !valid_wall_time.contains(&now()?) {
                return Err("取消审查时原请求期限已失效".into());
            }
            incoming.reply(&OperationReceipt::review_cancelled())?;
            return Ok(None);
        };
        incoming.check()?;
        let request = reviewed.into_unapproved_request(now()?)?;
        if sha256(&request.encode(now()?)?) != digest {
            return Err("原生审查交接与本次接收请求不一致".into());
        }
        let result = Self { incoming, request };
        result.unapproved()?;
        Ok(Some(result))
    }
    // Ordinary read-only disk facts, not SACL or user authorization. The borrow
    // prevents replying/dropping the incoming request while this review lives.
    pub(super) fn review_disk(&self) -> Result<ReceivedDiskReview<'_, 'a>, String> {
        let review = crate::file_operation_disk::DiskReview::inspect(self.unapproved()?)?;
        self.unapproved()?;
        Ok(ReceivedDiskReview {
            received: self,
            disk: review,
        })
    }
    pub(super) fn reply(self, body: &[u8]) -> Result<(), &'static str> {
        self.unapproved()?;
        self.incoming.reply(body)
    }
}

#[cfg(test)]
mod tests {
    use super::super::{
        contract::{Content, ObjectIdentity, ObservedFile, Operation, Request, sha256},
        transport::native::{Cancel, Listener, Peer, connect},
    };
    use super::*;
    use std::{thread, time::Duration};
    const TIMEOUT: Duration = Duration::from_secs(5);
    fn payload(expires: bool) -> Vec<u8> {
        let now = now().unwrap();
        let id = || uuid::Uuid::new_v4().to_string();
        serde_json::to_vec(&Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: now - 1,
            expires_at_ms: if expires { now - 1 } else { now + 30_000 },
            root: r"C:\explicit-project".into(),
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
        })
        .unwrap()
    }
    fn peer() -> Peer {
        Peer::pin(std::process::id()).unwrap()
    }
    fn simulate_continue(pending: PendingReview) -> NativeReviewedRequest {
        let (document, digest) = pending.document(now().unwrap()).unwrap();
        let root = serde_json::to_value(document).unwrap()["rootSelectionId"]
            .as_str()
            .unwrap()
            .to_owned();
        let digest = digest.to_owned();
        NativeReviewedRequest::simulated(
            pending
                .decide(
                    crate::file_operation_review::Decision::Continue,
                    &digest,
                    &root,
                    now().unwrap(),
                )
                .unwrap()
                .unwrap(),
        )
    }
    #[test]
    fn independent_review_consumes_cancel_and_binds_the_original_request() {
        for mode in [
            "accept",
            "cancel",
            "wrong",
            "failure",
            "cancel_invalid",
            "accept_invalid",
        ] {
            let bytes = payload(false);
            let binding = Binding::new(&bytes).unwrap();
            let cancel = Cancel::new().unwrap();
            let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
            let id = listener.id().to_owned();
            thread::scope(|scope| {
                let client = scope.spawn(|| {
                    let received = ReceivedRequest::receive(
                        connect(&id, peer(), TIMEOUT, cancel.clone()).unwrap(),
                        &binding,
                    )
                    .unwrap();
                    let original_deadline =
                        received.unapproved().unwrap().monotonic_deadline_for_test();
                    let original = received
                        .unapproved()
                        .unwrap()
                        .encode(now().unwrap())
                        .unwrap();
                    let reviewed = received.review_with(|pending, watch| {
                        watch.check().unwrap();
                        match mode {
                            "cancel" => Ok(None),
                            "cancel_invalid" => {
                                cancel.cancel().unwrap();
                                assert!(watch.check().is_err());
                                Ok(None)
                            }
                            "accept_invalid" => {
                                cancel.cancel().unwrap();
                                Ok(Some(simulate_continue(pending)))
                            }
                            "failure" => Err("simulated renderer failure".into()),
                            "wrong" => Ok(Some(simulate_continue(
                                PendingReview::new(
                                    CheckedRequest::decode(&payload(false), now().unwrap())
                                        .unwrap(),
                                    now().unwrap(),
                                )
                                .unwrap(),
                            ))),
                            _ => Ok(Some(simulate_continue(pending))),
                        }
                    });
                    match mode {
                        "accept" => {
                            let received = reviewed.unwrap().unwrap();
                            let request = received.unapproved().unwrap();
                            assert_eq!(request.encode(now().unwrap()).unwrap(), original);
                            // Compare the retained Instant itself, not two
                            // estimates from millisecond wall-clock samples.
                            // Exact equality also detects request re-decoding.
                            assert_eq!(request.monotonic_deadline_for_test(), original_deadline);
                            assert!(received.review_disk().is_err());
                            received.reply(b"reviewed, not executed").unwrap();
                        }
                        "cancel" => assert!(reviewed.unwrap().is_none()),
                        _ => assert!(reviewed.is_err()),
                    }
                });
                let response = listener.accept(peer()).unwrap().request(&binding, &bytes);
                match mode {
                    "accept" => assert_eq!(response.unwrap(), b"reviewed, not executed"),
                    "cancel" => assert_eq!(
                        response.unwrap(),
                        br#"{"version":1,"status":"review_cancelled"}"#
                    ),
                    _ => assert!(response.is_err()),
                }
                client.join().unwrap();
            });
        }
    }
    #[test]
    fn parent_disconnect_between_review_and_handoff_rejects_acceptance() {
        let bytes = payload(false);
        let binding = Binding::new(&bytes).unwrap();
        let parent_cancel = Cancel::new().unwrap();
        let listener = Listener::new(TIMEOUT, parent_cancel.clone()).unwrap();
        let id = listener.id().to_owned();
        let (closed, disconnected) = std::sync::mpsc::channel();
        thread::scope(|scope| {
            let binding = &binding;
            let client = scope.spawn(move || {
                let received = ReceivedRequest::receive(
                    connect(&id, peer(), TIMEOUT, Cancel::new().unwrap()).unwrap(),
                    binding,
                )
                .unwrap();
                assert!(
                    received
                        .review_with(|pending, watch| {
                            watch.check().unwrap();
                            parent_cancel.cancel().unwrap();
                            disconnected.recv_timeout(TIMEOUT).unwrap();
                            assert!(watch.check().is_err());
                            // Even a stale accepted native result cannot pass the
                            // final incoming check after THIS parent channel closed.
                            Ok(Some(simulate_continue(pending)))
                        })
                        .is_err()
                );
            });
            assert!(
                listener
                    .accept(peer())
                    .unwrap()
                    .request(binding, &bytes)
                    .is_err()
            );
            closed.send(()).unwrap();
            client.join().unwrap();
        });
    }
    #[test]
    fn bound_transport_is_parsed_as_one_unapproved_request() {
        let bytes = payload(false);
        let binding = Binding::new(&bytes).unwrap();
        let cancel = Cancel::new().unwrap();
        let listener = Listener::new(TIMEOUT, cancel.clone()).unwrap();
        let id = listener.id().to_owned();
        thread::scope(|scope| {
            let client = scope.spawn(|| {
                let received = ReceivedRequest::receive(
                    connect(&id, peer(), TIMEOUT, cancel.clone()).unwrap(),
                    &binding,
                )
                .unwrap();
                assert_eq!(received.unapproved().unwrap().facts().path, "file.txt");
                // Structurally valid wire facts cannot substitute for disk facts.
                assert!(received.review_disk().is_err());
                // Fixed fixture response says nothing about real approval/disk.
                received.reply(b"parsed, unapproved").unwrap();
            });
            assert_eq!(
                listener
                    .accept(peer())
                    .unwrap()
                    .request(&binding, &bytes)
                    .unwrap(),
                b"parsed, unapproved"
            );
            client.join().unwrap();
        });
    }
    #[test]
    fn valid_transport_cannot_bypass_bad_request_or_expiration() {
        for bytes in [br#"{"approved":true,"files":[]}"#.to_vec(), payload(true)] {
            let binding = Binding::new(&bytes).unwrap();
            let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
            let id = listener.id().to_owned();
            thread::scope(|scope| {
                let client = scope.spawn(|| {
                    let channel = connect(&id, peer(), TIMEOUT, Cancel::new().unwrap()).unwrap();
                    assert!(ReceivedRequest::receive(channel, &binding).is_err());
                });
                assert!(
                    listener
                        .accept(peer())
                        .unwrap()
                        .request(&binding, &bytes)
                        .is_err()
                );
                client.join().unwrap();
            });
        }
    }
}
