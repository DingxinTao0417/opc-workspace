//! Complete, handle-bound security capture for the isolated helper. The kernel
//! supplies live descriptors. Strict thaw is only for the fixed local stage
//! journal and is never accepted from a model, WebView, recovery JSON, or argv;
//! it is schema validation, not authorization to apply an ACL.
use std::{
    fs::File,
    mem::size_of,
    os::windows::{
        ffi::OsStrExt,
        io::{AsRawHandle, FromRawHandle},
    },
    path::{Path, PathBuf},
};
use windows::Win32::{
    Foundation::{ERROR_INSUFFICIENT_BUFFER, GetLastError, HANDLE},
    Security::{
        Authorization::{SE_FILE_OBJECT, SetSecurityInfo},
        BACKUP_SECURITY_INFORMATION, GetKernelObjectSecurity, GetSecurityDescriptorControl,
        GetSecurityDescriptorDacl, GetSecurityDescriptorGroup, GetSecurityDescriptorLength,
        GetSecurityDescriptorOwner, GetSecurityDescriptorSacl, IsValidSecurityDescriptor,
        OBJECT_SECURITY_INFORMATION, PROTECTED_DACL_SECURITY_INFORMATION,
        PROTECTED_SACL_SECURITY_INFORMATION, PSECURITY_DESCRIPTOR, PSID, SE_DACL_PROTECTED,
        SE_SACL_PROTECTED, SECURITY_ATTRIBUTES, UNPROTECTED_DACL_SECURITY_INFORMATION,
        UNPROTECTED_SACL_SECURITY_INFORMATION,
    },
    Storage::FileSystem::{
        CREATE_NEW, CreateFileW, DELETE, FILE_DISPOSITION_INFO, FILE_FLAG_OPEN_REPARSE_POINT,
        FILE_FLAG_WRITE_THROUGH, FILE_GENERIC_READ, FILE_GENERIC_WRITE, FILE_SHARE_MODE,
        FileDispositionInfo, SetFileInformationByHandle, WRITE_DAC, WRITE_OWNER,
    },
};
use windows::core::PCWSTR;

const MAX_DESCRIPTOR: usize = 64 * 1024;
const MIN_DESCRIPTOR: usize = 20; // SECURITY_DESCRIPTOR_RELATIVE header.
const ACCESS_SYSTEM_SECURITY: u32 = 0x01000000;

/// Only this module can create this type, always with CREATE_NEW. There is no
/// API to wrap an existing target/source handle or apply an incoming descriptor.
/// Before a durable recovery intent exists, Drop requests deletion by this
/// owned handle, never by the candidate pathname. The controlled executor can
/// make the object durable exactly once after recovery binding; from then on
/// every error retains it for inspection/recovery.
pub(super) struct NewCandidate {
    file: Option<File>,
    path: PathBuf,
    retain: bool,
}
impl NewCandidate {
    pub(super) fn file(&self) -> &File {
        self.file.as_ref().expect("候选文件句柄始终存在")
    }

    pub(super) fn file_mut(&mut self) -> &mut File {
        self.file.as_mut().expect("候选文件句柄始终存在")
    }

    pub(super) fn path(&self) -> &Path {
        &self.path
    }

    /// Called only after the recovery record and its execution-stage binding
    /// are both durable. It cannot make an existing path disposable again.
    pub(super) fn retain_for_recovery(&mut self) {
        self.retain = true;
    }

    /// Transfers the retained handle to the started executor. The path is kept
    /// only as frozen evidence; no later operation reopens or deletes by it.
    pub(super) fn into_retained_file(mut self) -> Result<(File, PathBuf), &'static str> {
        if !self.retain {
            return Err("候选尚未由持久恢复意图接管");
        }
        let file = self.file.take().ok_or("候选文件句柄缺失")?;
        Ok((file, self.path.clone()))
    }
}
impl Drop for NewCandidate {
    fn drop(&mut self) {
        if self.retain {
            return;
        }
        let Some(file) = self.file.as_ref() else {
            return;
        };
        let disposition = FILE_DISPOSITION_INFO { DeleteFile: true };
        // SAFETY: own newly-created handle with DELETE, no pathname lookup.
        let _ = unsafe {
            SetFileInformationByHandle(
                HANDLE(file.as_raw_handle()),
                FileDispositionInfo,
                (&disposition as *const FILE_DISPOSITION_INFO).cast(),
                size_of::<FILE_DISPOSITION_INFO>() as u32,
            )
        };
    }
}

// BACKUP requests every component, including the privileged SACL. Do not fall
// back to DACL/LABEL-only capture on older systems or access denial.
const QUERY: OBJECT_SECURITY_INFORMATION = BACKUP_SECURITY_INFORMATION;

fn candidate_name(operation_id: &str) -> Result<String, &'static str> {
    if !uuid::Uuid::parse_str(operation_id).is_ok_and(|id| id.to_string() == operation_id) {
        return Err("候选操作身份无效");
    }
    Ok(format!(".opc-file-{operation_id}.candidate"))
}

/// A full descriptor from a successful complete kernel query. Intentionally no
/// Clone/Debug/Serialize/Deserialize: it never leaves this helper as a payload.
pub(super) struct FullDescriptor {
    aligned: Vec<u64>,
    length: usize,
    control: u16,
}

fn bounded(length: usize) -> Result<(), &'static str> {
    if !(MIN_DESCRIPTOR..=MAX_DESCRIPTOR).contains(&length) {
        Err("完整文件权限描述不可用或超限")
    } else {
        Ok(())
    }
}

impl FullDescriptor {
    pub(super) fn freeze(&self) -> Vec<u8> {
        self.bytes().to_vec()
    }

    pub(super) fn thaw(bytes: &[u8]) -> Result<Self, &'static str> {
        bounded(bytes.len())?;
        let mut result = Self {
            aligned: vec![0; bytes.len().div_ceil(8)],
            length: bytes.len(),
            control: 0,
        };
        // SAFETY: aligned storage is initialized and at least bytes.len() long.
        unsafe {
            std::ptr::copy_nonoverlapping(
                bytes.as_ptr(),
                result.aligned.as_mut_ptr().cast(),
                bytes.len(),
            );
        }
        let descriptor = PSECURITY_DESCRIPTOR(result.aligned.as_mut_ptr().cast());
        if !unsafe { IsValidSecurityDescriptor(descriptor) }.as_bool()
            || unsafe { GetSecurityDescriptorLength(descriptor) } as usize != result.length
        {
            return Err("持久完整权限描述无效");
        }
        let mut revision = 0;
        unsafe { GetSecurityDescriptorControl(descriptor, &mut result.control, &mut revision) }
            .map_err(|_| "持久完整权限继承状态无效")?;
        Ok(result)
    }

    /// Caller must keep the single-file identity pinned and have opened this
    /// handle with READ_CONTROL | ACCESS_SYSTEM_SECURITY inside AuditScope.
    pub(super) fn capture(file: &File) -> Result<Self, &'static str> {
        let handle = HANDLE(file.as_raw_handle());
        let mut length = 0;
        // SAFETY: live file handle, null buffer only queries required capacity.
        let measured = unsafe { GetKernelObjectSecurity(handle, QUERY.0, None, 0, &mut length) };
        let error = unsafe { GetLastError() };
        if measured.is_ok() || error != ERROR_INSUFFICIENT_BUFFER {
            return Err("无法读取完整审计权限；不会按无 SACL 继续");
        }
        bounded(length as usize)?;
        let mut result = Self {
            aligned: vec![0; (length as usize).div_ceil(8)],
            length: length as usize,
            control: 0,
        };
        let descriptor = PSECURITY_DESCRIPTOR(result.aligned.as_mut_ptr().cast());
        // SAFETY: owned initialized aligned allocation covers requested length.
        unsafe { GetKernelObjectSecurity(handle, QUERY.0, Some(descriptor), length, &mut length) }
            .map_err(|_| "完整审计权限读取失败或读取期间发生变化")?;
        bounded(length as usize)?;
        if length as usize > result.length {
            return Err("文件权限在捕获期间增长，拒绝截断");
        }
        result.length = length as usize;
        let mut revision = 0;
        // SAFETY: buffer came only from a successful kernel security query.
        if !unsafe { IsValidSecurityDescriptor(descriptor) }.as_bool()
            || unsafe { GetSecurityDescriptorLength(descriptor) } as usize != result.length
        {
            return Err("系统返回的完整权限描述无效");
        }
        unsafe { GetSecurityDescriptorControl(descriptor, &mut result.control, &mut revision) }
            .map_err(|_| "无法核验完整权限继承状态")?;
        Ok(result)
    }

    /// Bind a candidate write to this complete capture, not a legacy v2 partial
    /// security blob. The operation layer must supply ONLY its newly created,
    /// still-empty candidate, never an existing target or original file handle.
    /// The internal materializer is the only caller; the binary exposes no
    /// path- or descriptor-driven dispatch to it.
    pub(super) fn preservation_flags(&self) -> OBJECT_SECURITY_INFORMATION {
        preservation_flags(self.control)
    }

    /// Primitive for one independently reviewed operation. Its caller MUST pin and
    /// independently validate the selected root and every parent component for
    /// the lifetime of this object. No general CLI/IPC caller exists.
    /// Creates an empty candidate already bearing the original descriptor;
    /// source data and source ACLs are never modified by this module.
    pub(super) fn create_empty_candidate(
        &mut self,
        pinned_parent: &Path,
        operation_id: &str,
    ) -> Result<NewCandidate, &'static str> {
        if !pinned_parent.is_absolute() {
            return Err("候选目录必须是已核验的绝对目录");
        }
        let path = pinned_parent.join(candidate_name(operation_id)?);
        let mut name: Vec<u16> = path.as_os_str().encode_wide().collect();
        if name.contains(&0) || name.len() > 32766 {
            return Err("候选目录无效");
        }
        name.push(0);
        self.require_dacl()?;
        let attributes = SECURITY_ATTRIBUTES {
            nLength: size_of::<SECURITY_ATTRIBUTES>() as u32,
            lpSecurityDescriptor: self.aligned.as_mut_ptr().cast(),
            bInheritHandle: false.into(),
        };
        // SAFETY: trusted kernel descriptor, pinned parent is a caller contract,
        // CREATE_NEW cannot overwrite; no body is written before readback.
        let handle = unsafe {
            CreateFileW(
                PCWSTR(name.as_ptr()),
                (FILE_GENERIC_READ | FILE_GENERIC_WRITE | DELETE | WRITE_DAC | WRITE_OWNER).0
                    | ACCESS_SYSTEM_SECURITY,
                FILE_SHARE_MODE(0),
                Some(&attributes),
                CREATE_NEW,
                FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_WRITE_THROUGH,
                None,
            )
        }
        .map_err(|_| "无法按完整权限新建候选；原文件未改变")?;
        let candidate = NewCandidate {
            // SAFETY: transfer new owned handle exactly once.
            file: Some(unsafe { File::from_raw_handle(handle.0) }),
            path,
            retain: false,
        };
        self.preserve_on_new(&candidate)?;
        self.verify_file(candidate.file())?;
        Ok(candidate)
    }

    fn require_dacl(&mut self) -> Result<(), &'static str> {
        let mut present = Default::default();
        let mut defaulted = Default::default();
        let mut dacl = std::ptr::null_mut();
        // SAFETY: descriptor came from a successful full kernel query.
        unsafe {
            GetSecurityDescriptorDacl(
                PSECURITY_DESCRIPTOR(self.aligned.as_mut_ptr().cast()),
                &mut present,
                &mut dacl,
                &mut defaulted,
            )
        }
        .map_err(|_| "无法核验候选访问控制")?;
        if !present.as_bool() || dacl.is_null() {
            return Err("不接受缺失或 NULL 的访问控制表");
        }
        Ok(())
    }

    fn preserve_on_new(&mut self, candidate: &NewCandidate) -> Result<(), &'static str> {
        let descriptor = PSECURITY_DESCRIPTOR(self.aligned.as_mut_ptr().cast());
        let mut owner = PSID::default();
        let mut group = PSID::default();
        let mut dacl = std::ptr::null_mut();
        let mut sacl = std::ptr::null_mut();
        let mut present = Default::default();
        let mut defaulted = Default::default();
        // SAFETY: each returned pointer is into the owned kernel descriptor and
        // stays live through SetSecurityInfo; only our fresh empty file changes.
        unsafe {
            GetSecurityDescriptorOwner(descriptor, &mut owner, &mut defaulted)
                .map_err(|_| "文件所有者描述无效")?;
            GetSecurityDescriptorGroup(descriptor, &mut group, &mut defaulted)
                .map_err(|_| "文件组描述无效")?;
            GetSecurityDescriptorDacl(descriptor, &mut present, &mut dacl, &mut defaulted)
                .map_err(|_| "文件访问控制描述无效")?;
            GetSecurityDescriptorSacl(descriptor, &mut present, &mut sacl, &mut defaulted)
                .map_err(|_| "文件审计控制描述无效")?;
            SetSecurityInfo(
                HANDLE(candidate.file().as_raw_handle()),
                SE_FILE_OBJECT,
                self.preservation_flags(),
                Some(owner),
                Some(group),
                Some(dacl),
                Some(sacl),
            )
            .ok()
        }
        .map_err(|_| "无法完整保留权限与继承；不得写入正文或安装候选")
    }

    pub(super) fn verify_file(&self, file: &File) -> Result<(), &'static str> {
        let actual = Self::capture(file)?;
        exact_match(self.bytes(), actual.bytes())
    }

    fn bytes(&self) -> &[u8] {
        // SAFETY: bounded length never exceeds initialized aligned storage.
        unsafe { std::slice::from_raw_parts(self.aligned.as_ptr().cast(), self.length) }
    }
}

fn preservation_flags(control: u16) -> OBJECT_SECURITY_INFORMATION {
    QUERY
        | if control & SE_DACL_PROTECTED.0 != 0 {
            PROTECTED_DACL_SECURITY_INFORMATION
        } else {
            UNPROTECTED_DACL_SECURITY_INFORMATION
        }
        | if control & SE_SACL_PROTECTED.0 != 0 {
            PROTECTED_SACL_SECURITY_INFORMATION
        } else {
            UNPROTECTED_SACL_SECURITY_INFORMATION
        }
}

fn exact_match(expected: &[u8], actual: &[u8]) -> Result<(), &'static str> {
    if expected != actual {
        Err("候选的完整权限与原文件不一致，不得安装或写入正文")
    } else {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{fs, os::windows::fs::OpenOptionsExt};

    struct Temp(PathBuf);
    impl Temp {
        fn new() -> Self {
            let path = std::env::temp_dir().join(format!(
                "opc-file-helper-candidate-{}",
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
    fn candidate(path: &Path) -> NewCandidate {
        fs::write(path, b"candidate").unwrap();
        let file = std::fs::OpenOptions::new()
            .access_mode((FILE_GENERIC_READ | DELETE).0)
            .share_mode(0)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
            .open(path)
            .unwrap();
        NewCandidate {
            file: Some(file),
            path: path.to_path_buf(),
            retain: false,
        }
    }

    #[test]
    fn capture_is_complete_without_partial_fallback() {
        assert_eq!(QUERY, BACKUP_SECURITY_INFORMATION);
    }

    #[test]
    fn descriptor_bounds_reject_missing_truncated_and_oversized() {
        for length in [0, 1, 19, MAX_DESCRIPTOR + 1, usize::MAX] {
            assert!(bounded(length).is_err());
        }
        for length in [20, 64, MAX_DESCRIPTOR] {
            assert!(bounded(length).is_ok());
        }
    }

    #[test]
    fn access_and_audit_inheritance_are_independent() {
        for dacl in [false, true] {
            for sacl in [false, true] {
                let control = if dacl { SE_DACL_PROTECTED.0 } else { 0 }
                    | if sacl { SE_SACL_PROTECTED.0 } else { 0 };
                let flags = preservation_flags(control).0;
                assert_ne!(flags & BACKUP_SECURITY_INFORMATION.0, 0);
                assert_eq!(flags & PROTECTED_DACL_SECURITY_INFORMATION.0 != 0, dacl);
                assert_eq!(flags & UNPROTECTED_DACL_SECURITY_INFORMATION.0 != 0, !dacl);
                assert_eq!(flags & PROTECTED_SACL_SECURITY_INFORMATION.0 != 0, sacl);
                assert_eq!(flags & UNPROTECTED_SACL_SECURITY_INFORMATION.0 != 0, !sacl);
            }
        }
    }

    #[test]
    fn readback_does_not_ignore_audit_or_inheritance_differences() {
        let expected = [7u8; 64];
        assert!(exact_match(&expected, &expected).is_ok());
        assert!(exact_match(&expected, &expected[..32]).is_err());
        for i in 0..expected.len() {
            let mut changed = expected;
            changed[i] ^= 1;
            assert!(exact_match(&expected, &changed).is_err());
        }
    }

    #[test]
    fn frozen_descriptor_requires_a_complete_valid_relative_descriptor() {
        let valid = [
            1, 0, 4, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 20, 0, 0, 0, 2, 0, 8, 0, 0, 0, 0, 0,
        ];
        let decoded = FullDescriptor::thaw(&valid).unwrap();
        assert_eq!(decoded.freeze(), valid);
        for invalid in [&valid[..19], &valid[..27]] {
            assert!(FullDescriptor::thaw(invalid).is_err());
        }
        let mut changed = valid;
        changed[0] = 0;
        assert!(FullDescriptor::thaw(&changed).is_err());
    }

    #[test]
    fn candidate_name_is_canonical_and_operation_bound() {
        let id = uuid::Uuid::new_v4().to_string();
        assert_eq!(
            candidate_name(&id).unwrap(),
            format!(".opc-file-{id}.candidate")
        );
        for invalid in ["", "NOT-A-UUID", &id.to_uppercase()] {
            assert!(candidate_name(invalid).is_err());
        }
    }

    #[test]
    fn candidate_is_disposable_only_before_durable_recovery_binding() {
        let temp = Temp::new();
        let disposable = temp.0.join("disposable.txt");
        drop(candidate(&disposable));
        assert!(!disposable.exists());

        let retained = temp.0.join("retained.txt");
        let mut owner = candidate(&retained);
        owner.retain_for_recovery();
        drop(owner);
        assert_eq!(fs::read(&retained).unwrap(), b"candidate");
    }

    #[test]
    fn retained_candidate_handle_can_transfer_to_the_started_executor() {
        let temp = Temp::new();
        let path = temp.0.join("retained.txt");
        let owner = candidate(&path);
        assert!(owner.into_retained_file().is_err());

        let mut owner = candidate(&path.with_extension("second"));
        owner.retain_for_recovery();
        let (file, frozen_path) = owner.into_retained_file().unwrap();
        drop(file);
        assert_eq!(frozen_path, path.with_extension("second"));
        assert_eq!(fs::read(frozen_path).unwrap(), b"candidate");
    }
    // No tests call privilege adjustment or read/write an actual file's SACL.
}
