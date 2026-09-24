//! Read-only verification of a checked request against current disk objects.
//! Not an approval, journal authentication, full SACL proof or write capability.
use crate::file_operation_contract::{
    CheckedRequest, MAX_FILE_BYTES, ObservedFile, Operation, sha256,
};
use crate::file_operation_metadata::Metadata;
use crate::file_operation_read::{self as read, PinnedDirectory};
use crate::file_operation_security::SecurityDescriptor;
use std::{
    fs::{File, OpenOptions},
    os::windows::fs::OpenOptionsExt,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
use windows::Win32::Storage::FileSystem::{
    DELETE, FILE_FLAG_OPEN_REPARSE_POINT, FILE_GENERIC_READ,
};

const ACCESS_SYSTEM_SECURITY: u32 = 0x01000000;

fn check_time(request: &CheckedRequest) -> Result<(), String> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围")?;
    request.check_time(now).map_err(str::to_owned)
}
fn open_read(path: &Path) -> std::io::Result<File> {
    open_for_inspection(path, false)
}
fn inspection_access(audit: bool) -> u32 {
    FILE_GENERIC_READ.0 | if audit { ACCESS_SYSTEM_SECURITY } else { 0 }
}
fn open_for_inspection(path: &Path, audit: bool) -> std::io::Result<File> {
    OpenOptions::new()
        .access_mode(inspection_access(audit))
        .share_mode(0)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
}
fn move_access(audit: bool) -> u32 {
    (FILE_GENERIC_READ | DELETE).0 | if audit { ACCESS_SYSTEM_SECURITY } else { 0 }
}
fn open_for_move(path: &Path, audit: bool) -> std::io::Result<File> {
    OpenOptions::new()
        .access_mode(move_access(audit))
        .share_mode(0)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
}
fn absent(path: &Path) -> Result<(), String> {
    // Only FILE_NOT_FOUND counts. Access denial, occupied directories, dangling
    // reparses and sharing conflicts are not an empty destination slot.
    match open_read(path) {
        Err(e) if e.raw_os_error() == Some(2) => Ok(()),
        _ => Err("本次操作要求的位置并非已确认缺失".into()),
    }
}
fn verify(file: &mut File, expected: &ObservedFile, aliases: bool) -> Result<(), String> {
    // Do not read an unrelated object's data, even when it has the same bytes.
    if read::recorded_identity(file)? != expected.identity {
        return Err("文件身份与审查请求不符".into());
    }
    let read_bytes = if aliases {
        read::read_parked
    } else {
        read::read_regular
    };
    let before = read_bytes(file, MAX_FILE_BYTES)?;
    if before.1 != expected.identity
        || before.2 != expected.modified
        || before.0 != expected.content.decode()?
    {
        return Err("文件正文或修改时间与审查请求不符".into());
    }
    let metadata = Metadata::capture(file, &before.0)?;
    if metadata.review_fingerprint()? != expected.metadata_sha256 {
        return Err("文件元数据与审查请求不符".into());
    }
    if sha256(SecurityDescriptor::capture(file)?.bytes()) != expected.readable_security_sha256 {
        return Err("文件可读权限与审查请求不符".into());
    }
    if read_bytes(file, MAX_FILE_BYTES)? != before {
        return Err("磁盘复核期间文件已变化".into());
    }
    Ok(())
}

/// Holds inspection handles and pinned ancestors (ordinary reads, or internal
/// SACL access without a setter/file accessor). An absent name is an
/// observation, not a reservation: the executor still needs no-overwrite
/// operations and fresh verification. There is deliberately no file accessor.
pub(crate) struct DiskReview<'a> {
    request: &'a CheckedRequest,
    original: File,
    candidate: Option<File>,
    vacant: Option<PathBuf>,
    _parent: PinnedDirectory,
    _root: PinnedDirectory,
}

/// Replace-only ownership guard for the helper candidate. It retains the
/// exact reviewed source plus every selected ancestor, but grants no access to
/// move, delete, or mutate that source. The candidate itself remains separate.
pub(crate) struct ReplaceGuard {
    original: File,
    parent_path: PathBuf,
    parent: PinnedDirectory,
    root: PinnedDirectory,
}
/// Delete-capable source handle armed only after the durable started marker.
/// The held root/parent handles keep the already-reviewed namespace stable;
/// moving still uses no-overwrite kernel operations in the helper executor.
pub(crate) struct ReplaceExecutionGuard {
    original: File,
    parent_path: PathBuf,
    _parent: PinnedDirectory,
    _root: PinnedDirectory,
}
impl ReplaceGuard {
    pub(crate) fn parent(&self) -> &Path {
        &self.parent_path
    }

    /// Read-only source handle opened with audit-query access by the isolated
    /// helper. It has no data-write, DELETE, WRITE_DAC, or WRITE_OWNER rights.
    pub(crate) fn original(&self) -> &File {
        &self.original
    }

    pub(crate) fn revalidate(&mut self, request: &CheckedRequest) -> Result<(), String> {
        check_time(request)?;
        let facts = request.facts();
        let Operation::Replace { original, .. } = &facts.operation else {
            return Err("只有新替换可以持有候选保护对象".into());
        };
        let target = Path::new(&facts.root).join(&facts.path);
        if self.root.identity != facts.root_identity
            || self.parent.identity != facts.parent_identity
            || target.parent() != Some(self.parent_path.as_path())
        {
            return Err("候选保护对象与所选目录不一致".into());
        }
        verify(&mut self.original, original, false)?;
        check_time(request)
    }

    /// Consumes the read-only observation guard and reopens the exact source
    /// with DELETE while ancestor handles remain pinned. The brief handle swap
    /// grants no race winner authority: identity/body/metadata/security and the
    /// request lifetime are checked again before this method returns.
    pub(crate) fn arm_for_execution(
        mut self,
        request: &CheckedRequest,
    ) -> Result<ReplaceExecutionGuard, String> {
        self.revalidate(request)?;
        let facts = request.facts();
        let Operation::Replace { original, .. } = &facts.operation else {
            return Err("只有新替换可以切换到移动句柄".into());
        };
        let target = Path::new(&facts.root).join(&facts.path);
        if target.parent() != Some(self.parent_path.as_path()) {
            return Err("执行源与已固定父目录不一致".into());
        }
        let ReplaceGuard {
            original: read_only,
            parent_path,
            parent,
            root,
        } = self;
        drop(read_only);
        let mut moving = open_for_move(&target, true)
            .map_err(|_| "原对象无法切换到独占移动句柄；未修改文件".to_string())?;
        verify(&mut moving, original, false)?;
        check_time(request)?;
        Ok(ReplaceExecutionGuard {
            original: moving,
            parent_path,
            _parent: parent,
            _root: root,
        })
    }
}
impl ReplaceExecutionGuard {
    pub(crate) fn original_mut(&mut self) -> &mut File {
        &mut self.original
    }

    pub(crate) fn parent(&self) -> &Path {
        &self.parent_path
    }
}

/// Exact delete-capable pair for one reviewed recovery mode. It is created
/// only after rechecking the required empty destination and both recorded
/// identities; it does not itself move, overwrite or delete anything.
pub(crate) struct RecoveryExecutionGuard {
    original: File,
    candidate: File,
    target: PathBuf,
    parked_original: PathBuf,
    staged_candidate: PathBuf,
    undo_installed: bool,
    _parent: PinnedDirectory,
    _root: PinnedDirectory,
}
impl RecoveryExecutionGuard {
    pub(crate) fn files_mut(&mut self) -> (&mut File, &mut File) {
        (&mut self.original, &mut self.candidate)
    }
    pub(crate) fn original_mut(&mut self) -> &mut File {
        &mut self.original
    }
    pub(crate) fn candidate_mut(&mut self) -> &mut File {
        &mut self.candidate
    }
    pub(crate) fn target(&self) -> &Path {
        &self.target
    }
    pub(crate) fn parked_original(&self) -> &Path {
        &self.parked_original
    }
    pub(crate) fn staged_candidate(&self) -> &Path {
        &self.staged_candidate
    }
    pub(crate) fn undo_installed(&self) -> bool {
        self.undo_installed
    }
}

impl<'a> DiskReview<'a> {
    pub(crate) fn inspect(request: &'a CheckedRequest) -> Result<Self, String> {
        Self::inspect_access(request, false)
    }
    /// Helper-only inspection used inside AuditScope so the resulting replace
    /// guard can keep querying the exact source SACL without reopening a path.
    pub(crate) fn inspect_for_audit(request: &'a CheckedRequest) -> Result<Self, String> {
        Self::inspect_access(request, true)
    }
    fn inspect_access(request: &'a CheckedRequest, audit: bool) -> Result<Self, String> {
        check_time(request)?;
        let facts = request.facts();
        let root = Path::new(&facts.root);
        let root_pin = PinnedDirectory::open(root, false)?;
        if root_pin.identity != facts.root_identity {
            return Err("所选根目录身份与审查请求不符".into());
        }
        let target = root.join(&facts.path);
        let parent = target.parent().ok_or("文件父目录无效")?;
        let parent_pin = PinnedDirectory::open(parent, false)?;
        if parent_pin.identity != facts.parent_identity {
            return Err("文件父目录身份与审查请求不符".into());
        }
        let (original_path, candidate_path, vacant) = match &facts.operation {
            Operation::Replace { .. } => (target, None, None),
            Operation::RestoreMissing { record_id, .. } => (
                parent.join(format!(".opc-file-{record_id}.original")),
                Some(parent.join(format!(".opc-file-{record_id}.candidate"))),
                Some(target),
            ),
            Operation::UndoInstalled { record_id, .. } => (
                parent.join(format!(".opc-file-{record_id}.original")),
                Some(target.clone()),
                Some(parent.join(format!(".opc-file-{record_id}.candidate"))),
            ),
        };
        if let Some(path) = &vacant {
            absent(path)?;
        }
        let original = open_for_inspection(&original_path, audit)
            .map_err(|_| "原对象不可用或缺少本次检查权限")?;
        // Identify the original before opening/reading any candidate data.
        let expected = match &facts.operation {
            Operation::Replace { original, .. }
            | Operation::RestoreMissing { original, .. }
            | Operation::UndoInstalled { original, .. } => original,
        };
        if read::recorded_identity(&original)? != expected.identity {
            return Err("文件身份与审查请求不符".into());
        }
        let candidate = candidate_path
            .as_deref()
            .map(|path| open_for_inspection(path, audit))
            .transpose()
            .map_err(|_| "候选对象不可用或正在使用")?;
        let mut review = Self {
            request,
            original,
            candidate,
            vacant,
            _parent: parent_pin,
            _root: root_pin,
        };
        review.revalidate()?;
        Ok(review)
    }

    /// Helper-only caller must enter its per-use audit scope first. The mask
    /// grants SACL access (Windows has no separate SACL-read bit), NOT data
    /// writes/DELETE/WRITE_DAC/WRITE_OWNER. No file handle accessor is exposed.
    /// Capture and verify run on the SAME identity-checked held objects; all
    /// handles are dropped before this returns to the privilege-scope owner.
    pub(crate) fn capture_security<T>(
        request: &'a CheckedRequest,
        capture: impl Fn(&File) -> Result<T, String>,
        verify: impl Fn(&File, &T) -> Result<(), String>,
    ) -> Result<(T, Option<T>), String> {
        Self::capture_with_access(request, true, capture, verify)
    }
    fn capture_with_access<T>(
        request: &'a CheckedRequest,
        audit: bool,
        capture: impl Fn(&File) -> Result<T, String>,
        verify: impl Fn(&File, &T) -> Result<(), String>,
    ) -> Result<(T, Option<T>), String> {
        let mut held = Self::inspect_access(request, audit)?;
        let original = capture(&held.original)?;
        let candidate = held.candidate.as_ref().map(&capture).transpose()?;
        held.revalidate()?;
        verify(&held.original, &original)?;
        if let (Some(file), Some(expected)) = (&held.candidate, &candidate) {
            verify(file, expected)?;
        }
        held.revalidate()?;
        Ok((original, candidate))
    }

    /// Convert the already verified replace review into an owned no-mutation
    /// guard. This does not create a file and is unavailable to recovery modes.
    pub(crate) fn into_replace_guard(mut self) -> Result<ReplaceGuard, String> {
        self.revalidate()?;
        if !matches!(self.request.facts().operation, Operation::Replace { .. })
            || self.candidate.is_some()
            || self.vacant.is_some()
        {
            return Err("磁盘审查不是新替换候选的来源".into());
        }
        let request = self.request;
        let parent_path = Path::new(&request.facts().root)
            .join(&request.facts().path)
            .parent()
            .ok_or("文件父目录无效")?
            .to_path_buf();
        let mut result = ReplaceGuard {
            original: self.original,
            parent_path,
            parent: self._parent,
            root: self._root,
        };
        result.revalidate(request)?;
        Ok(result)
    }

    pub(crate) fn revalidate(&mut self) -> Result<(), String> {
        check_time(self.request)?;
        if let Some(path) = &self.vacant {
            absent(path)?;
        }
        match &self.request.facts().operation {
            Operation::Replace { original, .. } => verify(&mut self.original, original, false)?,
            Operation::RestoreMissing {
                original,
                candidate,
                ..
            }
            | Operation::UndoInstalled {
                original,
                candidate,
                ..
            } => {
                let held_candidate = self.candidate.as_mut().ok_or("候选句柄缺失")?;
                if read::recorded_identity(held_candidate)? != candidate.identity {
                    return Err("候选身份与审查请求不符".into());
                }
                verify(&mut self.original, original, true)?;
                verify(held_candidate, candidate, true)?;
            }
        }
        if let Some(path) = &self.vacant {
            absent(path)?;
        }
        check_time(self.request)
    }

    /// After the durable started marker, exchange the read-only review handles
    /// for DELETE-capable handles while the selected ancestors remain pinned.
    /// Every object and required absence is rechecked after the exchange.
    pub(crate) fn arm_for_execution(mut self) -> Result<RecoveryExecutionGuard, String> {
        self.revalidate()?;
        let facts = self.request.facts();
        let target = Path::new(&facts.root).join(&facts.path);
        let parent = target.parent().ok_or("文件父目录无效")?;
        let (record_id, original_expected, candidate_expected, undo_installed) =
            match &facts.operation {
                Operation::RestoreMissing {
                    record_id,
                    original,
                    candidate,
                    ..
                } => (record_id, original, candidate, false),
                Operation::UndoInstalled {
                    record_id,
                    original,
                    candidate,
                    ..
                } => (record_id, original, candidate, true),
                Operation::Replace { .. } => {
                    return Err("新替换必须使用候选保护对象切换移动句柄".into());
                }
            };
        let parked_original = parent.join(format!(".opc-file-{record_id}.original"));
        let staged_candidate = parent.join(format!(".opc-file-{record_id}.candidate"));
        let candidate_path = if undo_installed {
            target.clone()
        } else {
            staged_candidate.clone()
        };
        let vacant = if undo_installed {
            staged_candidate.clone()
        } else {
            target.clone()
        };
        if self.vacant.as_deref() != Some(vacant.as_path()) {
            return Err("恢复执行的空目标与审查不一致".into());
        }
        let DiskReview {
            original: read_original,
            candidate: read_candidate,
            _parent: parent_pin,
            _root: root_pin,
            ..
        } = self;
        drop(read_original);
        drop(read_candidate);
        absent(&vacant)?;
        let mut original = open_for_move(&parked_original, true)
            .map_err(|_| "原对象无法切换到独占移动句柄；未修改文件".to_string())?;
        verify(&mut original, original_expected, true)?;
        let mut candidate = open_for_move(&candidate_path, true)
            .map_err(|_| "候选对象无法切换到独占移动句柄；未修改文件".to_string())?;
        verify(&mut candidate, candidate_expected, true)?;
        absent(&vacant)?;
        check_time(self.request)?;
        Ok(RecoveryExecutionGuard {
            original,
            candidate,
            target,
            parked_original,
            staged_candidate,
            undo_installed,
            _parent: parent_pin,
            _root: root_pin,
        })
    }
}

#[cfg(test)]
#[path = "file_operation_disk/tests.rs"]
mod tests;
