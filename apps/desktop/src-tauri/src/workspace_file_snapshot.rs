//! Exact, local-only baselines for the reviewed project-file bridge.
//!
//! Reading never authorizes model disclosure. A snapshot identity is an opaque
//! in-process capability, never a model argument; consuming one into a write
//! still requires native review, a bound helper and per-use system consent.
use crate::developer_workspace::WorkspaceState;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};
use tauri::State;

const MAX_BYTES: usize = crate::file_operation_contract::MAX_FILE_BYTES;
const MAX_SNAPSHOTS: usize = 8;
const TTL: Duration = Duration::from_secs(10 * 60);

mod edits;
#[cfg(windows)]
mod replacement_journal;
#[cfg(windows)]
use crate::file_operation_metadata as replacement_metadata;
#[cfg(windows)]
mod replacement_recovery;
#[cfg(windows)]
use crate::file_operation_security as replacement_security;
#[cfg(windows)]
mod request_bridge;
#[cfg(windows)]
pub use replacement_recovery::RecoveryReviewState as FileRecoveryReviewState;
#[cfg(not(windows))]
#[derive(Clone, Default)]
pub struct FileRecoveryReviewState;
// The historical replacement engine remains test-only. Product writes use the
// separately verified helper executor, not this ordinary-permission prototype.
#[cfg(all(test, windows))]
mod replacement;
#[cfg(windows)]
use crate::file_operation_read as windows;
pub use edits::*;

use crate::file_operation_contract::ObjectIdentity as FileIdentity;
#[cfg(windows)]
use crate::file_operation_read::ExactFile;
#[cfg(not(windows))]
#[derive(Clone, PartialEq, Eq)]
struct ExactFile {
    root_identity: FileIdentity,
    file_identity: FileIdentity,
    // Metadata changes are conservative conflicts, even if bytes are identical.
    modified: u64,
    content: Vec<u8>,
}

struct Snapshot {
    root_id: String,
    root: PathBuf,
    path: String,
    file: ExactFile,
    created: Instant,
    recovery: Option<(String, Vec<u8>)>,
}

#[derive(Clone, Default)]
pub struct FileSnapshotState(Arc<Mutex<HashMap<String, Snapshot>>>);

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct FileSnapshot {
    id: String,
    path: String,
    content: String,
    size: usize,
    sha256: String,
    expires_in_seconds: u64,
}

// Use one portable, canonical relative-path spelling. Do not accept Windows
// normalization aliases (trailing dots/spaces, devices or ADS) as edit targets.
fn validate_path(path: &str) -> Result<(), String> {
    crate::file_operation_contract::validate_relative(path).map_err(str::to_owned)
}

fn hash(content: &str) -> String {
    format!("{:x}", Sha256::digest(content.as_bytes()))
}

impl FileSnapshotState {
    fn capture(
        &self,
        root_id: String,
        root: PathBuf,
        path: String,
    ) -> Result<FileSnapshot, String> {
        validate_path(&path)?;
        // Serialize state changes and bounded reads. The snapshot is local-only;
        // no OS handles or model-access grants survive this method.
        let mut snapshots = self.0.lock().map_err(|_| "文件快照状态不可用")?;
        snapshots.retain(|_, s| s.created.elapsed() < TTL);
        if snapshots.len() >= MAX_SNAPSHOTS {
            return Err("最多保留 8 个修改基线，请先释放旧基线".into());
        }
        let file = read_exact(&root, &path)?;
        if file.content.contains(&0) {
            return Err("二进制内容不能作为文本修改基线".into());
        }
        let content = String::from_utf8(file.content.clone())
            .map_err(|_| "文件不是有效 UTF-8，不能使用替换字符修改原文件")?;
        let id = uuid::Uuid::new_v4().to_string();
        let result = FileSnapshot {
            id: id.clone(),
            path: path.clone(),
            sha256: hash(&content),
            size: file.content.len(),
            content,
            expires_in_seconds: TTL.as_secs(),
        };
        snapshots.insert(
            id,
            Snapshot {
                root_id,
                root,
                path,
                file,
                created: Instant::now(),
                recovery: None,
            },
        );
        Ok(result)
    }

    fn validate(&self, root_id: &str, root: &Path, id: &str) -> Result<(), String> {
        let mut snapshots = self.0.lock().map_err(|_| "文件快照状态不可用")?;
        snapshots.retain(|_, s| s.created.elapsed() < TTL);
        let snapshot = snapshots
            .get(id)
            .ok_or("文件基线已过期或释放，请重新读取并审查")?;
        if snapshot.root_id != root_id || snapshot.root != root {
            return Err("文件基线不属于当前目录".into());
        }
        let result = read_exact(root, &snapshot.path).and_then(|current| {
            if current.root_identity != snapshot.file.root_identity
                || current.file_identity != snapshot.file.file_identity
                || current.modified != snapshot.file.modified
                || current.content != snapshot.file.content
            {
                Err("文件或目录已变化，请重新读取并审查；未写入文件".into())
            } else {
                Ok(())
            }
        });
        // A detected conflict is terminal for this baseline, even if somebody
        // subsequently restores the old bytes. Never resurrect a reviewed edit.
        if result.is_err() {
            snapshots.remove(id);
        }
        result
    }

    fn release(&self, root_id: &str, id: &str) -> Result<(), String> {
        let mut snapshots = self.0.lock().map_err(|_| "文件快照状态不可用")?;
        if let Some(snapshot) = snapshots.get(id) {
            if snapshot.root_id != root_id {
                return Err("文件基线不属于当前目录".into());
            }
            snapshots.remove(id);
        }
        Ok(())
    }
}

#[cfg(windows)]
fn read_exact(root: &Path, relative: &str) -> Result<ExactFile, String> {
    windows::open(root, relative, false)?.read()
}

#[cfg(not(windows))]
fn read_exact(_: &Path, _: &str) -> Result<ExactFile, String> {
    Err("精确项目文件基线目前只支持 Windows 桌面端".into())
}

#[tauri::command]
pub async fn workspace_file_snapshot(
    state: State<'_, WorkspaceState>,
    snapshots: State<'_, FileSnapshotState>,
    root_id: String,
    path: String,
) -> Result<FileSnapshot, String> {
    let root = state.root(&root_id)?;
    let snapshots = snapshots.inner().clone();
    tauri::async_runtime::spawn_blocking(move || snapshots.capture(root_id, root, path))
        .await
        .map_err(|_| "文件快照读取任务失败")?
}

#[tauri::command]
pub async fn workspace_file_snapshot_validate(
    state: State<'_, WorkspaceState>,
    snapshots: State<'_, FileSnapshotState>,
    root_id: String,
    snapshot_id: String,
) -> Result<(), String> {
    // Read-only freshness check, NOT a transferable write authorization. A
    // future writer must compare under its own pinned handles while writing.
    let root = state.root(&root_id)?;
    let snapshots = snapshots.inner().clone();
    tauri::async_runtime::spawn_blocking(move || snapshots.validate(&root_id, &root, &snapshot_id))
        .await
        .map_err(|_| "文件快照校验任务失败")?
}

#[tauri::command]
pub fn workspace_file_snapshot_release(
    snapshots: State<'_, FileSnapshotState>,
    root_id: String,
    snapshot_id: String,
) -> Result<(), String> {
    snapshots.release(&root_id, &snapshot_id)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn digest_is_sha256_of_exact_utf8_bytes() {
        assert_eq!(
            hash(""),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
        assert_eq!(
            hash("abc"),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
        assert_ne!(hash("a\r\n"), hash("a\n"));
        assert_ne!(hash("\u{feff}a"), hash("a"));
    }

    #[test]
    fn paths_reject_escapes_aliases_and_metadata() {
        for path in [
            "",
            "../a",
            "a/../b",
            "a/./b",
            "/a",
            "a//b",
            "a/",
            "C:/a",
            "\\\\host\\file",
            "a\\b",
            "a:stream",
            ".git/config",
            ".GIT/x",
            "x\0y",
            "a. ",
            "a.",
            "a ",
            "NUL.txt",
            "dir/COM1",
            "LPT²",
            "a?b",
            "x\ny",
        ] {
            assert!(validate_path(path).is_err(), "{path:?}");
        }
        for path in [
            "src/main.rs",
            "空 格/测试.md",
            ".env",
            "COM10.txt",
            "a..b",
            ".gitignore",
        ] {
            assert!(validate_path(path).is_ok(), "{path}");
        }
    }

    #[cfg(windows)]
    mod windows_tests {
        use super::*;
        use std::{fs, fs::OpenOptions, os::windows::fs::OpenOptionsExt};

        // Create a directory junction entirely through Win32. Unlike symbolic
        // link creation this requires neither elevation nor Developer Mode.
        fn junction(link: &Path, target: &Path) {
            use ::windows::Win32::{
                Foundation::HANDLE,
                Storage::FileSystem::{FILE_FLAG_BACKUP_SEMANTICS, FILE_FLAG_OPEN_REPARSE_POINT},
                System::{IO::DeviceIoControl, Ioctl::FSCTL_SET_REPARSE_POINT},
            };
            use std::os::windows::{ffi::OsStrExt, io::AsRawHandle};
            fs::create_dir(link).unwrap();
            let handle = OpenOptions::new()
                .read(true)
                .write(true)
                .share_mode(0)
                .custom_flags(FILE_FLAG_BACKUP_SEMANTICS.0 | FILE_FLAG_OPEN_REPARSE_POINT.0)
                .open(link)
                .unwrap();
            // target is the test fixture's canonical verbatim disk path.
            let target = target.as_os_str().encode_wide().collect::<Vec<_>>();
            assert_eq!(&target[..4], &[92, 92, 63, 92]); // \\?\
            let mut substitute = vec![92, 63, 63, 92]; // NT prefix \??\
            substitute.extend_from_slice(&target[4..]);
            let print = &target[4..];
            let path_bytes = (substitute.len() + 1 + print.len() + 1) * 2;
            let mut buffer = Vec::new();
            buffer.extend_from_slice(&0xA0000003u32.to_le_bytes()); // IO_REPARSE_TAG_MOUNT_POINT
            for value in [
                (8 + path_bytes) as u16,
                0,
                0,
                (substitute.len() * 2) as u16,
                ((substitute.len() + 1) * 2) as u16,
                (print.len() * 2) as u16,
            ] {
                buffer.extend_from_slice(&value.to_le_bytes());
            }
            for word in substitute
                .iter()
                .chain(std::iter::once(&0))
                .chain(print)
                .chain(std::iter::once(&0))
            {
                buffer.extend_from_slice(&word.to_le_bytes());
            }
            let mut returned = 0;
            // SAFETY: handle and the complete MOUNT_POINT_REPARSE_BUFFER remain
            // valid for this synchronous call. Only test-owned paths are used.
            unsafe {
                DeviceIoControl(
                    HANDLE(handle.as_raw_handle()),
                    FSCTL_SET_REPARSE_POINT,
                    Some(buffer.as_ptr().cast()),
                    buffer.len() as u32,
                    None,
                    0,
                    Some(&mut returned),
                    None,
                )
            }
            .unwrap();
        }

        // Exact, unique test-owned root; no recursive deletion or user data.
        struct Fixture(PathBuf);
        impl Fixture {
            fn new() -> Self {
                let path =
                    std::env::temp_dir().join(format!("opc-exact-file-{}", uuid::Uuid::new_v4()));
                fs::create_dir(&path).unwrap();
                Self(fs::canonicalize(path).unwrap())
            }
            fn capture(&self, state: &FileSnapshotState) -> FileSnapshot {
                state
                    .capture("root".into(), self.0.clone(), "file.txt".into())
                    .unwrap()
            }
        }
        impl Drop for Fixture {
            fn drop(&mut self) {
                use std::os::windows::fs::MetadataExt;
                // Every fixture has only top-level test files. Never traverse links.
                for entry in fs::read_dir(&self.0).unwrap() {
                    let path = entry.unwrap().path();
                    // is_dir() is false for a junction's symlink metadata; use
                    // its directory attribute to remove the link, never target.
                    if fs::symlink_metadata(&path).unwrap().file_attributes() & 0x10 != 0 {
                        fs::remove_dir(path).unwrap();
                    } else {
                        fs::remove_file(path).unwrap();
                    }
                }
                fs::remove_dir(&self.0).unwrap();
            }
        }

        #[test]
        fn exact_utf8_bom_crlf_and_empty_round_trip_without_writes() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            for content in ["\u{feff}你好\r\n<svg>\n末行", ""] {
                fs::write(dir.0.join("file.txt"), content).unwrap();
                let before = fs::metadata(dir.0.join("file.txt"))
                    .unwrap()
                    .modified()
                    .unwrap();
                let snapshot = dir.capture(&state);
                assert_eq!(snapshot.content, content);
                assert_eq!(snapshot.size, content.len());
                assert_eq!(snapshot.sha256, hash(content));
                assert_eq!(snapshot.expires_in_seconds, 600);
                assert!(state.validate("root", &dir.0, &snapshot.id).is_ok());
                assert_eq!(
                    fs::read(dir.0.join("file.txt")).unwrap(),
                    content.as_bytes()
                );
                assert_eq!(
                    fs::metadata(dir.0.join("file.txt"))
                        .unwrap()
                        .modified()
                        .unwrap(),
                    before
                );
                let wire = serde_json::to_string(&snapshot).unwrap();
                assert!(!wire.contains(&dir.0.to_string_lossy().to_string()));
                assert!(!wire.contains("rootIdentity"));
                state.release("root", &snapshot.id).unwrap();
                assert!(state.validate("root", &dir.0, &snapshot.id).is_err());
            }
        }

        #[test]
        fn never_uses_lossy_or_truncated_preview_as_baseline() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            fs::write(dir.0.join("file.txt"), [0xff, b'a']).unwrap();
            assert!(
                crate::developer_workspace::preview(&dir.0, "file.txt")
                    .unwrap()
                    .content
                    .contains('\u{fffd}')
            );
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "file.txt".into())
                    .unwrap_err()
                    .contains("UTF-8")
            );
            for bytes in [vec![b'x'; MAX_BYTES + 1], vec![0, 1, 2]] {
                fs::write(dir.0.join("file.txt"), &bytes).unwrap();
                assert!(
                    state
                        .capture("root".into(), dir.0.clone(), "file.txt".into())
                        .is_err()
                );
            }
            assert!(state.0.lock().unwrap().is_empty());
            fs::write(dir.0.join("file.txt"), vec![b'x'; MAX_BYTES]).unwrap();
            assert_eq!(dir.capture(&state).size, MAX_BYTES);
        }

        #[test]
        fn concurrent_writer_directory_and_hardlink_are_rejected() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            let path = dir.0.join("file.txt");
            fs::write(&path, "before").unwrap();
            let writer = OpenOptions::new()
                .write(true)
                .share_mode(7)
                .open(&path)
                .unwrap();
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "file.txt".into())
                    .is_err()
            );
            drop(writer);
            fs::hard_link(&path, dir.0.join("alias.txt")).unwrap();
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "file.txt".into())
                    .unwrap_err()
                    .contains("硬链接")
            );
            fs::create_dir(dir.0.join("folder")).unwrap();
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "folder".into())
                    .is_err()
            );
        }

        #[test]
        fn changed_bytes_or_replaced_identity_invalidate_baseline() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            let path = dir.0.join("file.txt");
            fs::write(&path, "before").unwrap();
            let first = dir.capture(&state);
            fs::write(&path, "after").unwrap();
            assert!(
                state
                    .validate("root", &dir.0, &first.id)
                    .unwrap_err()
                    .contains("已变化")
            );
            fs::write(&path, "before").unwrap();
            assert!(
                state
                    .validate("root", &dir.0, &first.id)
                    .unwrap_err()
                    .contains("释放")
            );
            let second = dir.capture(&state);
            fs::rename(&path, dir.0.join("old.txt")).unwrap();
            fs::write(&path, "before").unwrap();
            assert!(
                state
                    .validate("root", &dir.0, &second.id)
                    .unwrap_err()
                    .contains("已变化")
            );
        }

        #[test]
        fn baseline_is_bound_to_root_and_cannot_be_used_after_expiry() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            fs::write(dir.0.join("file.txt"), "value").unwrap();
            let snapshot = dir.capture(&state);
            assert!(state.validate("other", &dir.0, &snapshot.id).is_err());
            assert!(state.release("other", &snapshot.id).is_err());
            assert!(
                state
                    .validate("root", &dir.0.join("other"), &snapshot.id)
                    .is_err()
            );
            assert!(state.validate("root", &dir.0, &snapshot.id).is_ok());
            state
                .0
                .lock()
                .unwrap()
                .get_mut(&snapshot.id)
                .unwrap()
                .created = Instant::now() - TTL;
            assert!(state.validate("root", &dir.0, &snapshot.id).is_err());
            assert!(state.0.lock().unwrap().is_empty());
        }

        #[test]
        fn missing_file_invalidates_baseline_without_restoring_it() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            let path = dir.0.join("file.txt");
            fs::write(&path, "before").unwrap();
            let snapshot = dir.capture(&state);
            fs::remove_file(&path).unwrap();
            assert!(state.validate("root", &dir.0, &snapshot.id).is_err());
            assert!(!path.exists());
            assert!(state.0.lock().unwrap().is_empty());
        }

        #[test]
        fn junction_is_rejected_at_selected_root_and_nested_path() {
            let dir = Fixture::new();
            let outside = Fixture::new();
            fs::write(outside.0.join("file.txt"), "outside selected directory").unwrap();
            junction(&dir.0.join("link"), &outside.0);
            let state = FileSnapshotState::default();
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "link/file.txt".into())
                    .unwrap_err()
                    .contains("目录联接")
            );
            assert!(
                state
                    .capture("root".into(), dir.0.join("link"), "file.txt".into())
                    .unwrap_err()
                    .contains("目录联接")
            );
            assert!(state.0.lock().unwrap().is_empty());
            assert_eq!(
                fs::read_to_string(outside.0.join("file.txt")).unwrap(),
                "outside selected directory"
            );
        }

        #[test]
        fn remote_and_device_roots_are_rejected_before_open() {
            for root in [
                r"\\server\share\folder",
                r"\\?\UNC\server\share\folder",
                r"\\.\C:\folder",
            ] {
                assert!(
                    read_exact(Path::new(root), "file.txt")
                        .err()
                        .unwrap()
                        .contains("网络或设备")
                );
            }
        }

        #[test]
        fn root_replacement_does_not_rebind_snapshot_even_with_same_file() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            let selected = dir.0.join("project");
            let old = dir.0.join("old-project");
            fs::create_dir(&selected).unwrap();
            fs::write(selected.join("file.txt"), "unchanged").unwrap();
            let snapshot = state
                .capture("root".into(), selected.clone(), "file.txt".into())
                .unwrap();
            fs::rename(&selected, &old).unwrap();
            fs::create_dir(&selected).unwrap();
            fs::rename(old.join("file.txt"), selected.join("file.txt")).unwrap();
            assert!(
                state
                    .validate("root", &selected, &snapshot.id)
                    .unwrap_err()
                    .contains("已变化")
            );
            fs::remove_file(selected.join("file.txt")).unwrap();
        }

        #[test]
        fn capacity_and_release_are_bounded_and_idempotent() {
            let dir = Fixture::new();
            let state = FileSnapshotState::default();
            fs::write(dir.0.join("file.txt"), "value").unwrap();
            let mut ids = Vec::new();
            for _ in 0..MAX_SNAPSHOTS {
                ids.push(dir.capture(&state).id);
            }
            assert!(
                state
                    .capture("root".into(), dir.0.clone(), "file.txt".into())
                    .is_err()
            );
            state.release("root", &ids[0]).unwrap();
            state.release("root", &ids[0]).unwrap();
            dir.capture(&state);
            assert_eq!(state.0.lock().unwrap().len(), MAX_SNAPSHOTS);
        }
    }
}
