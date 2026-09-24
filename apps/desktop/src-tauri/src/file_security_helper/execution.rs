//! Private, started-only single-file executor. It is never directly reachable
//! from argv, Tauri or the model: the bound per-use launch chain must first
//! finish native review, recovery binding and the durable started transition.
use super::{
    execution_journal::ExecutionOutcomeEvidence,
    security_inspection::{MaterializedCandidate, SecurityInspection},
};
use crate::{
    file_operation_contract::{CheckedRequest, Operation},
    file_operation_disk::DiskReview,
};
use std::{
    fs::File,
    mem::{offset_of, size_of},
    os::windows::{ffi::OsStrExt, io::AsRawHandle},
    path::Path,
};
use windows::Win32::{
    Foundation::HANDLE,
    Storage::FileSystem::{FILE_RENAME_INFO, FileRenameInfo, SetFileInformationByHandle},
};

type Result<T> = std::result::Result<T, String>;

/// Rename the object referenced by `file` to one absolute name without ever
/// replacing the occupant. The caller keeps the destination parent pinned.
fn rename_no_replace(file: &File, target: &Path) -> Result<()> {
    if !target.is_absolute() {
        return Err("重命名目标必须为绝对路径".into());
    }
    let mut name: Vec<u16> = target.as_os_str().encode_wide().collect();
    if name.contains(&0) || name.len() > 32766 {
        return Err("重命名目标无效".into());
    }
    let name_bytes = name.len() * 2;
    name.push(0);
    let bytes = (offset_of!(FILE_RENAME_INFO, FileName) + name.len() * 2)
        .max(size_of::<FILE_RENAME_INFO>());
    let mut aligned = vec![0u64; bytes.div_ceil(8)];
    // SAFETY: aligned storage contains the fixed header and bounded UTF-16 tail;
    // the source is an owned live handle opened with DELETE.
    unsafe {
        let info = aligned.as_mut_ptr().cast::<FILE_RENAME_INFO>();
        (*info).Anonymous.ReplaceIfExists = false;
        (*info).RootDirectory = HANDLE::default();
        (*info).FileNameLength = name_bytes as u32;
        std::ptr::copy_nonoverlapping(
            name.as_ptr(),
            std::ptr::addr_of_mut!((*info).FileName).cast::<u16>(),
            name.len(),
        );
        SetFileInformationByHandle(
            HANDLE(file.as_raw_handle()),
            FileRenameInfo,
            info.cast(),
            bytes as u32,
        )
    }
    .map_err(|_| "目标已存在或无法安全重命名；不会覆盖现有对象".into())
}

fn move_replace(source: &File, candidate: &File, parked: &Path, target: &Path) -> Result<()> {
    rename_no_replace(source, parked)?;
    // Do not roll back if a competitor occupies target after the source move.
    // Both reviewed objects remain named by the durable recovery intent.
    rename_no_replace(candidate, target)
}

fn move_recovery(
    original: &File,
    candidate: &File,
    target: &Path,
    staged_candidate: &Path,
    undo_installed: bool,
) -> Result<()> {
    if undo_installed {
        rename_no_replace(candidate, staged_candidate)?;
    }
    // Missing restore moves only original. Undo reaches this point only after
    // the installed candidate is safely retained under its recorded name.
    rename_no_replace(original, target)
}

pub(super) fn replace(
    inspection: &SecurityInspection,
    request: &CheckedRequest,
    materialized: MaterializedCandidate,
) -> Result<ExecutionOutcomeEvidence> {
    if !matches!(request.facts().operation, Operation::Replace { .. }) {
        return Err("新替换执行器收到恢复请求".into());
    }
    let (candidate, guard, candidate_evidence) =
        materialized.into_execution(inspection, request)?;
    let (mut candidate, candidate_path) = candidate.into_retained_file()?;
    let expected_candidate = guard.parent().join(format!(
        ".opc-file-{}.candidate",
        request.facts().operation_id
    ));
    if candidate_path != expected_candidate {
        return Err("执行候选路径与已固定父目录不一致".into());
    }
    let mut source = guard.arm_for_execution(request)?;
    // The DELETE-handle exchange is followed by a complete descriptor readback
    // before the first mutation. Any competitor that won the swap is rejected.
    inspection.verify_original_outcome(request, source.original_mut(), false)?;
    inspection.verify_new_candidate_outcome(request, &candidate_evidence, &mut candidate)?;

    let target = Path::new(&request.facts().root).join(&request.facts().path);
    let parked = source.parent().join(format!(
        ".opc-file-{}.original",
        request.facts().operation_id
    ));
    move_replace(source.original_mut(), &candidate, &parked, &target)?;

    let installed =
        inspection.verify_new_candidate_outcome(request, &candidate_evidence, &mut candidate)?;
    let retained = inspection.verify_original_outcome(request, source.original_mut(), false)?;
    Ok(ExecutionOutcomeEvidence::replace(&installed, &retained))
}

pub(super) fn recover(
    inspection: &SecurityInspection,
    request: &CheckedRequest,
) -> Result<ExecutionOutcomeEvidence> {
    if matches!(request.facts().operation, Operation::Replace { .. }) {
        return Err("恢复执行器收到新替换请求".into());
    }
    let mut pair = DiskReview::inspect_for_audit(request)?.arm_for_execution()?;
    inspection.verify_original_outcome(request, pair.original_mut(), true)?;
    inspection.verify_recovery_candidate_outcome(request, pair.candidate_mut())?;

    let target = pair.target().to_path_buf();
    let staged_candidate = pair.staged_candidate().to_path_buf();
    let undo_installed = pair.undo_installed();
    let (original, candidate) = pair.files_mut();
    move_recovery(
        original,
        candidate,
        &target,
        &staged_candidate,
        undo_installed,
    )?;

    let restored = inspection.verify_original_outcome(request, pair.original_mut(), true)?;
    let retained = inspection.verify_recovery_candidate_outcome(request, pair.candidate_mut())?;
    Ok(if undo_installed {
        ExecutionOutcomeEvidence::undo_installed(&restored, &retained)
    } else {
        ExecutionOutcomeEvidence::restore_missing(&restored, &retained)
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        fs::{self, OpenOptions},
        os::windows::fs::OpenOptionsExt,
        path::PathBuf,
    };
    use windows::Win32::Storage::FileSystem::{
        DELETE, FILE_FLAG_OPEN_REPARSE_POINT, FILE_GENERIC_READ,
    };

    struct Temp(PathBuf);
    impl Temp {
        fn new() -> Self {
            let path = std::env::temp_dir().join(format!(
                "opc-file-helper-execution-{}",
                uuid::Uuid::new_v4()
            ));
            fs::create_dir(&path).unwrap();
            Self(path)
        }
    }
    impl Drop for Temp {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }
    fn mover(path: &Path) -> File {
        OpenOptions::new()
            .access_mode((FILE_GENERIC_READ | DELETE).0)
            .share_mode(0)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
            .open(path)
            .unwrap()
    }

    #[test]
    fn no_replace_rename_moves_the_held_identity() {
        let temp = Temp::new();
        let source = temp.0.join("source.txt");
        let target = temp.0.join("target.txt");
        fs::write(&source, b"source").unwrap();
        let held = mover(&source);
        rename_no_replace(&held, &target).unwrap();
        drop(held);
        assert!(!source.exists());
        assert_eq!(fs::read(&target).unwrap(), b"source");
    }

    #[test]
    fn no_replace_rename_never_overwrites_a_competitor() {
        let temp = Temp::new();
        let source = temp.0.join("source.txt");
        let target = temp.0.join("target.txt");
        fs::write(&source, b"source").unwrap();
        fs::write(&target, b"winner").unwrap();
        let held = mover(&source);
        assert!(rename_no_replace(&held, &target).is_err());
        drop(held);
        assert_eq!(fs::read(&source).unwrap(), b"source");
        assert_eq!(fs::read(&target).unwrap(), b"winner");
    }

    #[test]
    fn replace_sequence_keeps_both_versions_when_target_wins_after_park() {
        let temp = Temp::new();
        let source = temp.0.join("file.txt");
        let candidate = temp.0.join(".candidate");
        let parked = temp.0.join(".original");
        fs::write(&source, b"original").unwrap();
        fs::write(&candidate, b"candidate").unwrap();
        let original_handle = mover(&source);
        let candidate_handle = mover(&candidate);
        // Park explicitly, then let a competing target appear before install.
        rename_no_replace(&original_handle, &parked).unwrap();
        fs::write(&source, b"winner").unwrap();
        assert!(rename_no_replace(&candidate_handle, &source).is_err());
        drop(original_handle);
        drop(candidate_handle);
        assert_eq!(fs::read(&parked).unwrap(), b"original");
        assert_eq!(fs::read(&candidate).unwrap(), b"candidate");
        assert_eq!(fs::read(&source).unwrap(), b"winner");
    }

    #[test]
    fn recovery_sequences_preserve_the_recorded_layout_without_overwrite() {
        for undo_installed in [false, true] {
            let temp = Temp::new();
            let target = temp.0.join("file.txt");
            let original = temp.0.join(".original");
            let staged = temp.0.join(".candidate");
            fs::write(&original, b"original").unwrap();
            if undo_installed {
                fs::write(&target, b"candidate").unwrap();
            } else {
                fs::write(&staged, b"candidate").unwrap();
            }
            let original_handle = mover(&original);
            let candidate_handle = mover(if undo_installed { &target } else { &staged });
            move_recovery(
                &original_handle,
                &candidate_handle,
                &target,
                &staged,
                undo_installed,
            )
            .unwrap();
            drop(original_handle);
            drop(candidate_handle);
            assert_eq!(fs::read(&target).unwrap(), b"original");
            assert_eq!(fs::read(&staged).unwrap(), b"candidate");
            assert!(!original.exists());
        }
    }

    #[test]
    fn undo_staging_collision_prevents_the_first_move() {
        let temp = Temp::new();
        let target = temp.0.join("file.txt");
        let original = temp.0.join(".original");
        let staged = temp.0.join(".candidate");
        fs::write(&target, b"candidate").unwrap();
        fs::write(&original, b"original").unwrap();
        fs::write(&staged, b"winner").unwrap();
        let original_handle = mover(&original);
        let candidate_handle = mover(&target);
        assert!(
            move_recovery(&original_handle, &candidate_handle, &target, &staged, true,).is_err()
        );
        drop(original_handle);
        drop(candidate_handle);
        assert_eq!(fs::read(&target).unwrap(), b"candidate");
        assert_eq!(fs::read(&original).unwrap(), b"original");
        assert_eq!(fs::read(&staged).unwrap(), b"winner");
    }

    #[test]
    fn restore_target_collision_keeps_both_recorded_objects() {
        let temp = Temp::new();
        let target = temp.0.join("file.txt");
        let original = temp.0.join(".original");
        let staged = temp.0.join(".candidate");
        fs::write(&target, b"winner").unwrap();
        fs::write(&original, b"original").unwrap();
        fs::write(&staged, b"candidate").unwrap();
        let original_handle = mover(&original);
        let candidate_handle = mover(&staged);
        assert!(
            move_recovery(&original_handle, &candidate_handle, &target, &staged, false,).is_err()
        );
        drop(original_handle);
        drop(candidate_handle);
        assert_eq!(fs::read(&target).unwrap(), b"winner");
        assert_eq!(fs::read(&original).unwrap(), b"original");
        assert_eq!(fs::read(&staged).unwrap(), b"candidate");
    }
}
