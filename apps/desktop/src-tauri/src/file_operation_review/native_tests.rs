use super::*;
use crate::file_operation_contract::{
    CheckedRequest, Content, MAX_FILE_BYTES, ObjectIdentity, ObservedFile, Operation, Request,
    sha256,
};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

fn pending(before: &[u8], after: &[u8]) -> PendingReview {
    let now = now().unwrap();
    let id = || uuid::Uuid::new_v4().to_string();
    let request = CheckedRequest::from_local(
        Request {
            version: 1,
            operation_id: id(),
            review_id: id(),
            root_selection_id: id(),
            issued_at_ms: now,
            expires_at_ms: now + 30_000,
            root: r"D:\selected-project".into(),
            path: "文件.txt".into(),
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
                    content: Content::from_bytes(before).unwrap(),
                    metadata_sha256: sha256(b"metadata"),
                    readable_security_sha256: sha256(b"partial"),
                },
                candidate: Content::from_bytes(after).unwrap(),
            },
        },
        now,
    )
    .unwrap();
    PendingReview::new(request, now).unwrap()
}
fn window() -> Window {
    Window::new(pending(b"before\r\n", b"after\n"), Box::new(|| Ok(()))).unwrap()
}
fn click(window: &Window, index: usize, id: usize) {
    unsafe {
        SendMessageW(
            window.hwnd,
            WM_COMMAND,
            Some(WPARAM(id)),
            Some(LPARAM(window.context.controls.get()[index].0 as isize)),
        );
    }
}

#[test]
fn hidden_native_controls_are_readonly_complete_and_destroyed() {
    let window = window();
    assert!(!unsafe { IsWindowVisible(window.hwnd) }.as_bool());
    assert!(window.context.exact_controls());
    let controls = window.context.controls.get();
    for control in &controls[..3] {
        assert_ne!(
            unsafe { GetWindowLongPtrW(*control, GWL_STYLE) } & ES_READONLY as isize,
            0
        );
    }
    let hwnd = window.hwnd;
    drop(window);
    assert!(!unsafe { IsWindow(Some(hwnd)) }.as_bool());
    for control in controls {
        assert!(!unsafe { IsWindow(Some(control)) }.as_bool());
    }
}
#[test]
fn real_controls_hold_maximum_files_without_32k_truncation() {
    let window = Window::new(
        pending(&vec![b'a'; MAX_FILE_BYTES], &vec![b'b'; MAX_FILE_BYTES]),
        Box::new(|| Ok(())),
    )
    .unwrap();
    assert!(window.context.exact_controls());
    assert!(
        unsafe { GetWindowTextLengthW(window.context.controls.get()[1]) } as usize
            > 2 * MAX_FILE_BYTES
    );
    assert!(!unsafe { IsWindowVisible(window.hwnd) }.as_bool());
}
#[test]
fn only_owned_continue_control_can_produce_single_reviewed_intent() {
    let mut window = window();
    unsafe {
        SendMessageW(
            window.hwnd,
            WM_COMMAND,
            Some(WPARAM(CONTINUE)),
            Some(LPARAM(0)),
        );
    }
    assert_eq!(window.context.outcome.get(), PENDING);
    click(&window, 4, CONTINUE);
    assert_eq!(window.context.outcome.get(), PENDING);
    click(&window, 3, CONTINUE);
    assert_eq!(window.context.outcome.get(), ACCEPTED);
    // Programmatic test notification is not human approval or execution proof.
    assert!(window.finish().unwrap().is_some());
    assert!(window.finish().is_err());
}
#[test]
fn cancel_and_close_do_not_create_a_reviewed_intent() {
    for close in [false, true] {
        let mut window = window();
        if close {
            unsafe {
                SendMessageW(window.hwnd, WM_CLOSE, None, None);
            }
        } else {
            click(&window, 4, CANCEL);
        }
        assert_eq!(window.context.outcome.get(), CANCELED);
        assert!(window.finish().unwrap().is_none());
    }
}
#[test]
fn changed_control_text_or_live_scope_fails_closed() {
    for text_changed in [false, true] {
        let live = Arc::new(AtomicBool::new(true));
        let check = live.clone();
        let mut window = Window::new(
            pending(b"before", b"after"),
            Box::new(move || {
                if check.load(Ordering::SeqCst) {
                    Ok(())
                } else {
                    Err("scope changed".into())
                }
            }),
        )
        .unwrap();
        if text_changed {
            unsafe {
                SetWindowTextW(
                    window.context.controls.get()[1],
                    w!("different displayed bytes"),
                )
                .unwrap();
            }
        } else {
            live.store(false, Ordering::SeqCst);
        }
        click(&window, 3, CONTINUE);
        assert_eq!(window.context.outcome.get(), FAILED);
        assert!(window.finish().is_err());
        live.store(true, Ordering::SeqCst);
        assert!(window.finish().is_err());
    }
}

#[test]
fn construction_cleanup_destroys_owned_class_even_without_userdata_binding() {
    let window = window();
    let hwnd = window.hwnd;
    unsafe {
        SetWindowLongPtrW(hwnd, GWLP_USERDATA, 0);
    }
    drop(window);
    assert!(!unsafe { IsWindow(Some(hwnd)) }.as_bool());
}

#[test]
fn close_after_click_cancels_before_handoff() {
    let mut window = window();
    click(&window, 3, CONTINUE);
    unsafe {
        SendMessageW(window.hwnd, WM_CLOSE, None, None);
    }
    assert!(window.finish().unwrap().is_none());
}
#[test]
fn timer_and_final_handoff_recheck_scope_and_expiration() {
    for expired in [false, true] {
        let live = Arc::new(AtomicBool::new(true));
        let check = live.clone();
        let mut window = Window::new(
            pending(b"before", b"after"),
            Box::new(move || {
                if check.load(Ordering::SeqCst) {
                    Ok(())
                } else {
                    Err("scope changed".into())
                }
            }),
        )
        .unwrap();
        if expired {
            // Manufacture a validated-but-now-expired request without sleeping.
            let current = window.context.pending.take().unwrap().request;
            let mut facts: serde_json::Value =
                serde_json::from_slice(&current.encode(now().unwrap()).unwrap()).unwrap();
            let stamp = now().unwrap();
            facts["issued_at_ms"] = serde_json::json!(stamp - 100);
            facts["expires_at_ms"] = serde_json::json!(stamp - 10);
            let request =
                CheckedRequest::decode(&serde_json::to_vec(&facts).unwrap(), stamp - 50).unwrap();
            window.context.pending = Some(PendingReview::new(request, stamp - 50).unwrap());
            unsafe {
                SendMessageW(window.hwnd, WM_TIMER, Some(WPARAM(TIMER)), None);
            }
            assert_eq!(window.context.outcome.get(), FAILED);
        } else {
            click(&window, 3, CONTINUE);
            live.store(false, Ordering::SeqCst);
        }
        assert!(window.finish().is_err());
        live.store(true, Ordering::SeqCst);
        assert!(window.finish().is_err());
    }
}
