//! Replacement engine under test; not registered as a production capability.
//! Never opens the reviewed source with FILE_WRITE_DATA. Only a newly created
//! sibling receives bytes. The source is parked by its held handle, then the
//! candidate is installed with ReplaceIfExists=false. No pathname overwrite.
use super::replacement_journal::{Intent, load};
use super::replacement_metadata::Metadata;
use super::replacement_recovery::{RecoveryMode, RecoveryReview};
use super::replacement_security::SecurityDescriptor;
use super::*;
use ::windows::Win32::{
    Foundation::HANDLE,
    Security::{
        Authorization::{SE_FILE_OBJECT, SetSecurityInfo},
        DACL_SECURITY_INFORMATION, GetSecurityDescriptorControl, GetSecurityDescriptorDacl,
        PROTECTED_DACL_SECURITY_INFORMATION, PSECURITY_DESCRIPTOR, SE_DACL_PROTECTED,
        SECURITY_ATTRIBUTES, UNPROTECTED_DACL_SECURITY_INFORMATION,
    },
    Storage::FileSystem::{
        CREATE_NEW, CreateFileW, DELETE, FILE_DISPOSITION_INFO, FILE_FLAG_OPEN_REPARSE_POINT,
        FILE_FLAG_WRITE_THROUGH, FILE_GENERIC_READ, FILE_GENERIC_WRITE, FILE_RENAME_INFO,
        FILE_SHARE_MODE, FileDispositionInfo, FileRenameInfo, SetFileInformationByHandle,
        WRITE_DAC,
    },
};
use ::windows::core::PCWSTR;
use std::{
    fs::{self, File, OpenOptions},
    io::Write,
    mem::{offset_of, size_of},
    os::windows::{
        ffi::OsStrExt,
        fs::OpenOptionsExt,
        io::{AsRawHandle, FromRawHandle},
    },
};

// Until a complete journal is durable, this newly created object is disposable.
// Delete by its owned handle, never by a pathname that could now name user data.
struct Candidate {
    file: File,
    retain: bool,
}
impl std::ops::Deref for Candidate {
    type Target = File;
    fn deref(&self) -> &File {
        &self.file
    }
}
impl std::ops::DerefMut for Candidate {
    fn deref_mut(&mut self) -> &mut File {
        &mut self.file
    }
}
impl Drop for Candidate {
    fn drop(&mut self) {
        if !self.retain {
            let disposition = FILE_DISPOSITION_INFO { DeleteFile: true };
            // SAFETY: live owned handle has DELETE; this can remove only our
            // newly-created link. Source objects never use this wrapper.
            let _ = unsafe {
                SetFileInformationByHandle(
                    HANDLE(self.file.as_raw_handle()),
                    FileDispositionInfo,
                    (&disposition as *const FILE_DISPOSITION_INFO).cast(),
                    size_of::<FILE_DISPOSITION_INFO>() as u32,
                )
            };
        }
    }
}

impl SecurityDescriptor {
    fn create(&mut self, path: &Path) -> Result<Candidate, String> {
        let name: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
        let attributes = SECURITY_ATTRIBUTES {
            nLength: size_of::<SECURITY_ATTRIBUTES>() as u32,
            lpSecurityDescriptor: self.aligned.as_mut_ptr().cast(),
            bInheritHandle: false.into(),
        };
        // SAFETY: pointers live through the call, descriptor comes from the
        // kernel, CREATE_NEW cannot overwrite a pathname, parent is pinned.
        let handle = unsafe {
            CreateFileW(
                PCWSTR(name.as_ptr()),
                (FILE_GENERIC_READ | FILE_GENERIC_WRITE | DELETE | WRITE_DAC).0,
                FILE_SHARE_MODE(0),
                Some(&attributes),
                CREATE_NEW,
                FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_WRITE_THROUGH,
                None,
            )
        }
        .map_err(|_| "无法按原权限创建候选文件，原文件未改变")?;
        // SAFETY: ownership of the newly returned handle transfers exactly once.
        let file = Candidate {
            file: unsafe { File::from_raw_handle(handle.0) },
            retain: false,
        };
        self.apply_dacl(&file)?;
        let actual = Self::capture(&file)?;
        if actual.bytes() != self.bytes() {
            return Err("候选权限与原文件不一致，未写入正文或替换原文件".into());
        }
        Ok(file)
    }
    fn apply_dacl(&mut self, file: &File) -> Result<(), String> {
        let sd = PSECURITY_DESCRIPTOR(self.aligned.as_mut_ptr().cast());
        let mut control = 0;
        let mut revision = 0;
        let mut present = Default::default();
        let mut defaulted = Default::default();
        let mut dacl = std::ptr::null_mut();
        // SAFETY: descriptor remains owned and the returned DACL points into it.
        unsafe {
            GetSecurityDescriptorControl(sd, &mut control, &mut revision)
                .map_err(|_| "权限继承标记不可用")?;
            GetSecurityDescriptorDacl(sd, &mut present, &mut dacl, &mut defaulted)
                .map_err(|_| "文件访问权限不可用")?;
        }
        if !present.as_bool() || dacl.is_null() {
            return Err("不接受缺失或空的访问控制表".into());
        }
        let inheritance = if control & SE_DACL_PROTECTED.0 != 0 {
            PROTECTED_DACL_SECURITY_INFORMATION
        } else {
            UNPROTECTED_DACL_SECURITY_INFORMATION
        };
        // SAFETY: the DACL lives through the call; only the new empty file is
        // changed. Use the filesystem API to preserve inheritance bookkeeping.
        unsafe {
            SetSecurityInfo(
                HANDLE(file.as_raw_handle()),
                SE_FILE_OBJECT,
                DACL_SECURITY_INFORMATION | inheritance,
                None,
                None,
                Some(dacl),
                None,
            )
            .ok()
        }
        .map_err(|_| "不能保留原访问权限与继承方式".into())
    }
}

pub(super) struct Prepared {
    pub(super) intent: Intent,
    source: File,
    candidate: Candidate,
    _root: windows::PinnedDirectory,
    _parent: windows::PinnedDirectory,
}

pub(super) fn source_file(path: &Path) -> Result<File, String> {
    OpenOptions::new()
        .access_mode((FILE_GENERIC_READ | DELETE).0)
        .share_mode(0)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
        .map_err(|_| "原文件不可用或正在被使用".into())
}

// The caller owns both the source handle and a pinned destination parent.
// The aligned storage includes the flexible UTF-16 tail, including its NUL.
pub(super) fn rename_no_replace(file: &File, target: &Path) -> Result<(), String> {
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
    // SAFETY: u64 storage has FILE_RENAME_INFO alignment, at least the header
    // size, and sufficient initialized tail space. File owns a live handle.
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
    .map_err(|_| "目标已存在或无法安全重命名；未覆盖任何目标".into())
}

impl Prepared {
    pub(super) fn new(
        root: &Path,
        path: &str,
        baseline: &ExactFile,
        after: &[u8],
    ) -> Result<Self, String> {
        validate_path(path)?;
        if after.len() > MAX_BYTES || after == baseline.content {
            return Err("替换内容无变化或超限".into());
        }
        let root_pin = windows::PinnedDirectory::open(root, false)?;
        let target = root.join(path);
        let parent = windows::PinnedDirectory::open(target.parent().ok_or("文件路径无效")?, false)?;
        let mut source = source_file(&target)?;
        let (before, identity, modified) = windows::read_regular(&mut source, MAX_BYTES)?;
        if root_pin.identity != baseline.root_identity
            || identity != baseline.file_identity
            || modified != baseline.modified
            || before != baseline.content
        {
            return Err("原文件或目录已变化，未替换".into());
        }
        let metadata = Metadata::capture(&source, &before)?;
        let id = uuid::Uuid::new_v4().to_string();
        let staged = target
            .parent()
            .unwrap()
            .join(format!(".opc-file-{id}.candidate"));
        // Apply access control at creation, before any sensitive byte is written.
        let mut security = SecurityDescriptor::capture(&source)?;
        let mut candidate = security.create(&staged)?;
        candidate
            .write_all(after)
            .and_then(|_| candidate.sync_all())
            .map_err(|_| "候选文件未能刷盘，原文件未改变")?;
        metadata.apply(&candidate, after)?;
        let (readback, candidate_identity, _) = windows::read_regular(&mut candidate, MAX_BYTES)?;
        if readback != after {
            return Err("候选全文核验失败，原文件未改变".into());
        }
        let intent = Intent {
            version: 2,
            id,
            root: root.into(),
            path: path.into(),
            root_identity: root_pin.identity.clone(),
            parent_identity: parent.identity.clone(),
            source_identity: identity,
            candidate_identity,
            before,
            after: after.into(),
            metadata,
            security: security.bytes().to_vec(),
        };
        intent.validate()?;
        Ok(Self {
            intent,
            source,
            candidate,
            _root: root_pin,
            _parent: parent,
        })
    }

    pub(super) fn record(mut self, storage: &Path) -> Result<Recorded, String> {
        let pin = windows::PinnedDirectory::open(storage, true)?;
        let mut count = 0;
        for entry in fs::read_dir(storage)
            .map_err(|_| "无法检查恢复记录容量")?
            .take(64)
        {
            entry.map_err(|_| "不能完整检查恢复记录容量")?;
            count += 1;
        }
        if count >= 64 {
            return Err("恢复记录已达 64 项，请先人工备份整理；原文件未改变".into());
        }
        let encoded = serde_json::to_string(&self.intent).map_err(|_| "替换意图编码失败")?;
        let bytes = serde_json::to_vec(&serde_json::json!({
            "intent": self.intent, "digest": hash(&encoded),
        }))
        .map_err(|_| "替换意图编码失败")?;
        if bytes.len() > 4 * 1024 * 1024 {
            return Err("替换意图超过完整保存上限".into());
        }
        let mut journal =
            windows::journal_file(&storage.join(format!("{}.json", self.intent.id)), true)?;
        journal
            .write_all(&bytes)
            .and_then(|_| journal.sync_all())
            .map_err(|_| "替换意图未刷盘，原文件未改变")?;
        // After this point every error retains both versions for inspection.
        self.candidate.retain = true;
        Ok(Recorded {
            prepared: self,
            _journal: journal,
            _storage: pin,
        })
    }
}

// No API lets the caller park a source without first retaining a synced journal.
pub(super) struct Recorded {
    prepared: Prepared,
    _journal: File,
    _storage: windows::PinnedDirectory,
}
pub(super) struct Parked {
    recorded: Recorded,
}
impl Recorded {
    pub(super) fn park(self) -> Result<Parked, String> {
        let p = &self.prepared;
        if !p
            .intent
            .metadata
            .matches_original(&Metadata::capture(&p.source, &p.intent.before)?)
            || SecurityDescriptor::capture(&p.source)?.bytes() != p.intent.security
        {
            return Err("准备期间原文件元数据或权限已变化，未替换".into());
        }
        rename_no_replace(
            &self.prepared.source,
            &self.prepared.intent.sibling("original"),
        )?;
        Ok(Parked { recorded: self })
    }
}
impl Parked {
    pub(super) fn install(mut self) -> Result<Intent, String> {
        let p = &mut self.recorded.prepared;
        // A competing new user file wins. Both source and candidate are retained
        // with recorded identities; never fall back to overwrite or path delete.
        rename_no_replace(&p.candidate, &p.intent.target())?;
        let (bytes, identity, _) = windows::read_regular(&mut p.candidate, MAX_BYTES)?;
        if bytes != p.intent.after || identity != p.intent.candidate_identity {
            return Err("安装后核验失败；保留原文件和恢复意图，不自动重试".into());
        }
        Ok(p.intent.clone())
    }
}

fn recover_missing(storage: &Path, id: &str, selected_root: &Path) -> Result<(), String> {
    RecoveryReview::capture(
        storage,
        "test-root",
        selected_root,
        id,
        RecoveryMode::MissingTarget,
    )?
    .prepare("test-root", selected_root)?
    .park_current()?
    .restore()
}

#[cfg(test)]
mod tests;
