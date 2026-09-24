use super::{native::*, wire::Binding};
use std::{
    thread,
    time::{Duration, Instant},
};

const TIMEOUT: Duration = Duration::from_secs(10);
fn me() -> Peer {
    Peer::pin(std::process::id()).unwrap()
}

#[test]
fn closed_channel_invalidates_received_request_even_while_peer_process_lives() {
    for close in [true, false] {
        let binding = Binding::new(b"request").unwrap();
        let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
        let id = listener.id().to_owned();
        let (ready, received) = std::sync::mpsc::channel();
        let (closed, check) = std::sync::mpsc::channel();
        thread::scope(|scope| {
            let current_binding = &binding;
            let client = scope.spawn(move || {
                let incoming = connect(&id, me(), TIMEOUT, Cancel::new().unwrap())
                    .unwrap()
                    .incoming(current_binding)
                    .unwrap();
                incoming.check().unwrap();
                ready.send(()).unwrap();
                check.recv_timeout(TIMEOUT).unwrap();
                // Same OS peer is still alive, but the parent abandoned this use.
                assert!(incoming.check().is_err());
            });
            let endpoint = listener.accept(me()).unwrap();
            endpoint
                .send_request_for_test(&binding, b"request")
                .unwrap();
            received.recv_timeout(TIMEOUT).unwrap();
            let mut held = Some(endpoint);
            if close {
                drop(held.take());
            } else {
                held.as_ref().unwrap().send_extra_for_test().unwrap();
            }
            closed.send(()).unwrap();
            client.join().unwrap();
        });
    }
}

#[test]
fn real_pipe_preserves_exact_bound_request_and_receipt() {
    let request = b"reviewed\0binary\r\nrequest";
    let binding = Binding::new(request).unwrap();
    let cancel = Cancel::new().unwrap();
    let listener = Listener::new(TIMEOUT, cancel.clone()).unwrap();
    let id = listener.id().to_owned();
    thread::scope(|scope| {
        let child = scope.spawn(|| {
            let incoming = connect(&id, me(), TIMEOUT, cancel.clone())
                .unwrap()
                .incoming(&binding)
                .unwrap();
            assert_eq!(incoming.body(), request);
            incoming.reply(b"receipt").unwrap();
        });
        let response = listener
            .accept(me())
            .unwrap()
            .request(&binding, request)
            .unwrap();
        assert_eq!(response, b"receipt");
        child.join().unwrap();
    });
}

#[test]
fn real_pipe_accepts_full_frame_and_rejects_oversized_peer_message() {
    let request = vec![13; super::wire::MAX_BODY];
    let binding = Binding::new(&request).unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let id = listener.id().to_owned();
    thread::scope(|scope| {
        let client = scope.spawn(|| {
            let incoming = connect(&id, me(), TIMEOUT, Cancel::new().unwrap())
                .unwrap()
                .incoming(&binding)
                .unwrap();
            assert_eq!(incoming.body(), request);
            incoming.reply(&request).unwrap();
        });
        assert_eq!(
            listener
                .accept(me())
                .unwrap()
                .request(&binding, &request)
                .unwrap(),
            request
        );
        client.join().unwrap();
    });
    let binding = Binding::new(b"request").unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let id = listener.id().to_owned();
    thread::scope(|scope| {
        let client = scope.spawn(|| {
            let channel = connect(&id, me(), TIMEOUT, Cancel::new().unwrap()).unwrap();
            // Either fully buffered or interrupted by receiver's rejection;
            // neither outcome allows receiver to return a truncated request.
            let _ = channel.oversized_peer_message();
        });
        assert!(listener.accept(me()).unwrap().incoming(&binding).is_err());
        client.join().unwrap();
    });
}

#[test]
fn first_instance_and_canonical_local_names_only() {
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    assert!(duplicate_listener_for_test(listener.id()).is_err());
    for id in [
        "",
        "../escape",
        r"\\host\pipe\share",
        "123",
        "00000000-0000-0000-0000-00000000000X",
    ] {
        assert!(connect(id, me(), TIMEOUT, Cancel::new().unwrap()).is_err());
    }
}

#[test]
fn canceled_and_timed_out_connects_drain_without_a_client() {
    let cancel = Cancel::new().unwrap();
    let listener = Listener::new(TIMEOUT, cancel.clone()).unwrap();
    cancel.cancel().unwrap();
    assert!(listener.accept(me()).is_err());
    let start = Instant::now();
    let listener = Listener::new(Duration::from_millis(30), Cancel::new().unwrap()).unwrap();
    assert!(listener.accept(me()).is_err());
    assert!(start.elapsed() < Duration::from_secs(2));
}

#[test]
fn blocked_read_is_canceled_and_disconnected_peer_fails() {
    let cancel = Cancel::new().unwrap();
    let binding = Binding::new(b"request").unwrap();
    let listener = Listener::new(TIMEOUT, cancel.clone()).unwrap();
    let id = listener.id().to_owned();
    thread::scope(|scope| {
        let client = scope.spawn(|| {
            let channel = connect(&id, me(), TIMEOUT, Cancel::new().unwrap()).unwrap();
            // No request yet; wait until the parent signals it accepted.
            let start = Instant::now();
            let result = channel.incoming(&binding);
            assert!(result.is_err());
            assert!(start.elapsed() < TIMEOUT + Duration::from_secs(1));
        });
        let endpoint = listener.accept(me()).unwrap();
        drop(endpoint);
        client.join().unwrap();
    });

    let binding = Binding::new(b"second request").unwrap();
    let listener = Listener::new(TIMEOUT, cancel.clone()).unwrap();
    let id = listener.id().to_owned();
    let (accepted, wait) = std::sync::mpsc::channel();
    thread::scope(|scope| {
        let client = scope.spawn(|| {
            let channel = connect(&id, me(), TIMEOUT, cancel.clone()).unwrap();
            accepted.send(()).unwrap();
            assert!(channel.incoming(&binding).is_err());
        });
        let endpoint = listener.accept(me()).unwrap();
        wait.recv_timeout(TIMEOUT).unwrap();
        cancel.cancel().unwrap();
        client.join().unwrap();
        drop(endpoint);
    });
}

#[test]
fn wrong_binding_is_not_delivered_as_an_approved_request() {
    let binding = Binding::new(b"request").unwrap();
    let other = Binding::new(b"request").unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let id = listener.id().to_owned();
    thread::scope(|scope| {
        let client = scope.spawn(|| {
            let channel = connect(&id, me(), TIMEOUT, Cancel::new().unwrap()).unwrap();
            assert!(channel.incoming(&other).is_err());
        });
        assert!(
            listener
                .accept(me())
                .unwrap()
                .request(&binding, b"request")
                .is_err()
        );
        client.join().unwrap();
    });
}

// The parent tests launch this exact ignored test in an OWNED child test binary.
// It never launches an app/UAC, touches a project file or enables any privilege.
#[test]
#[ignore = "fixture entrypoint executed by ordinary parent process tests"]
fn pipe_process_fixture() {
    use std::io::Read;
    let mode = std::env::var("OPC_PIPE_TEST_MODE").unwrap();
    match mode.as_str() {
        "idle" => {}
        "exchange" | "review_exchange" => {
            let id = std::env::var("OPC_PIPE_TEST_ID").unwrap();
            let peer = Peer::pin(
                std::env::var("OPC_PIPE_TEST_PARENT")
                    .unwrap()
                    .parse()
                    .unwrap(),
            )
            .unwrap();
            let nonce = std::env::var("OPC_PIPE_TEST_NONCE").unwrap();
            let binding = if mode == "review_exchange" {
                Binding::request_fixture(&nonce, &std::env::var("OPC_PIPE_TEST_DIGEST").unwrap())
            } else {
                Binding::process_fixture(&nonce)
            };
            let incoming = connect(&id, peer, TIMEOUT, Cancel::new().unwrap())
                .unwrap()
                .incoming(&binding)
                .unwrap();
            if mode == "review_exchange" {
                let received = incoming.body().to_vec();
                let now = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap()
                    .as_millis() as u64;
                crate::file_operation_contract::CheckedRequest::decode(&received, now).unwrap();
                // Echo only test bytes through the real pipe. No disk/approval
                // or privilege API is invoked by this ordinary fixture.
                incoming.reply(&received).unwrap();
            } else {
                assert_eq!(incoming.body(), b"process fixture");
                incoming.reply(b"child receipt").unwrap();
            }
        }
        _ => panic!("unknown fixture mode"),
    }
    // Keep OS peer alive until the parent consumes the buffered receipt.
    let mut done = String::new();
    std::io::stdin().read_to_string(&mut done).unwrap();
}

struct ChildFixture(std::process::Child);
impl ChildFixture {
    fn start(mode: &str, listener: &Listener, binding: &Binding) -> Self {
        use std::process::{Command, Stdio};
        Self(
            Command::new(std::env::current_exe().unwrap())
                .args([
                    "--exact",
                    &format!(
                        "{}::pipe_process_fixture",
                        module_path!().split_once("::").unwrap().1
                    ),
                    "--ignored",
                    "--nocapture",
                ])
                .env("OPC_PIPE_TEST_MODE", mode)
                .env("OPC_PIPE_TEST_ID", listener.id())
                .env("OPC_PIPE_TEST_PARENT", std::process::id().to_string())
                .env("OPC_PIPE_TEST_NONCE", binding.fixture_nonce())
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .spawn()
                .unwrap(),
        )
    }
    fn peer(&self) -> Peer {
        Peer::pin(self.0.id()).unwrap()
    }
    fn finish(&mut self) {
        drop(self.0.stdin.take());
        let until = Instant::now() + TIMEOUT;
        loop {
            if let Some(status) = self.0.try_wait().unwrap() {
                if !status.success() {
                    use std::io::Read;
                    let mut output = String::new();
                    self.0
                        .stdout
                        .take()
                        .unwrap()
                        .read_to_string(&mut output)
                        .unwrap();
                    self.0
                        .stderr
                        .take()
                        .unwrap()
                        .read_to_string(&mut output)
                        .unwrap();
                    panic!("owned pipe fixture failed: {output}");
                }
                return;
            }
            assert!(Instant::now() < until, "owned pipe fixture did not finish");
            thread::sleep(Duration::from_millis(5));
        }
    }
}
impl Drop for ChildFixture {
    fn drop(&mut self) {
        // RAII only ever stops/waits this exact owned child, even on assertion.
        if !matches!(self.0.try_wait(), Ok(Some(_))) {
            let _ = self.0.kill();
        }
        let _ = self.0.wait();
    }
}

#[test]
fn real_child_exchanges_bound_request_without_elevation() {
    let binding = Binding::new(b"process fixture").unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let mut child = ChildFixture::start("exchange", &listener, &binding);
    let response = listener
        .accept(child.peer())
        .unwrap()
        .request(&binding, b"process fixture")
        .unwrap();
    assert_eq!(response, b"child receipt");
    child.finish();
}

#[test]
fn kernel_peer_checks_reject_another_live_process_in_both_directions() {
    let binding = Binding::new(b"process fixture").unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let mut child = ChildFixture::start("idle", &listener, &binding);
    // Real server is this parent, not the live pinned child. No payload is sent.
    assert!(connect(listener.id(), child.peer(), TIMEOUT, Cancel::new().unwrap()).is_err());
    drop(listener);

    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let endpoint = connect(listener.id(), me(), TIMEOUT, Cancel::new().unwrap()).unwrap();
    // Real client is this parent, but the server expects the pinned child.
    assert!(listener.accept(child.peer()).is_err());
    drop(endpoint);
    child.finish();
}

#[test]
fn exited_peer_invalidates_even_an_existing_pin() {
    let binding = Binding::new(b"process fixture").unwrap();
    let listener = Listener::new(TIMEOUT, Cancel::new().unwrap()).unwrap();
    let mut child = ChildFixture::start("idle", &listener, &binding);
    let pinned = child.peer();
    child.finish();
    assert!(listener.accept(pinned).is_err());
}
