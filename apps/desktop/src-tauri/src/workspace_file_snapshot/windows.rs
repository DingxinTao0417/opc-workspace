use crate::file_operation_contract::{
    MAX_FILE_BYTES as MAX_BYTES, ObjectIdentity as FileIdentity, validate_relative,
};

#[derive(Clone, PartialEq, Eq)]
pub(crate) struct ExactFile {
    pub(crate) root_identity: FileIdentity,
    pub(crate) file_identity: FileIdentity,
    pub(crate) modified: u64,
    pub(crate) content: Vec<u8>,
}
fn validate_path(path: &str) -> Result<(), String> {
    validate_relative(path).map_err(str::to_owned)
}
use std::{
    fs::{File, OpenOptions},
    io::{Read, Seek, SeekFrom},
    os::windows::{ffi::OsStrExt, fs::OpenOptionsExt, io::AsRawHandle},
    path::{Component, Path, PathBuf, Prefix},
};
use windows::{
    Win32::{
        Foundation::HANDLE,
        Storage::FileSystem::{
            BY_HANDLE_FILE_INFORMATION, FILE_FLAG_BACKUP_SEMANTICS, FILE_FLAG_OPEN_REPARSE_POINT,
            GetDriveTypeW, GetFileInformationByHandle,
        },
    },
    core::PCWSTR,
};

fn info(file: &File) -> Result<BY_HANDLE_FILE_INFORMATION, String> {
    let mut value = BY_HANDLE_FILE_INFORMATION::default();
    // SAFETY: File owns a live handle; value is a writable output buffer.
    unsafe { GetFileInformationByHandle(HANDLE(file.as_raw_handle()), &mut value) }
        .map_err(|_| "文件身份不可用")?;
    if value.dwFileAttributes & 0x400 != 0 {
        return Err("不读取符号链接或目录联接".into());
    }
    Ok(value)
}
fn identity(value: &BY_HANDLE_FILE_INFORMATION) -> FileIdentity {
    FileIdentity {
        volume: value.dwVolumeSerialNumber,
        index: (u64::from(value.nFileIndexHigh) << 32) | u64::from(value.nFileIndexLow),
    }
}
fn modified(value: &BY_HANDLE_FILE_INFORMATION) -> u64 {
    (u64::from(value.ftLastWriteTime.dwHighDateTime) << 32)
        | u64::from(value.ftLastWriteTime.dwLowDateTime)
}

pub(super) struct PinnedDirectory {
    _handles: Vec<File>,
    pub(super) identity: FileIdentity,
}
impl PinnedDirectory {
    pub(super) fn open(root: &Path, create: bool) -> Result<Self, String> {
        if !root.is_absolute()
            || !matches!(root.components().next(), Some(Component::Prefix(p)) if matches!(p.kind(), Prefix::Disk(_) | Prefix::VerbatimDisk(_)))
        {
            return Err("精确文件操作只支持本机固定磁盘，不支持网络或设备路径".into());
        }
        let drive: PathBuf = root.components().take(2).collect();
        let wide: Vec<u16> = drive.as_os_str().encode_wide().chain(Some(0)).collect();
        // SAFETY: wide is a live NUL-terminated drive root.
        if unsafe { GetDriveTypeW(PCWSTR(wide.as_ptr())) } != 3 {
            return Err("精确文件操作只支持本机固定磁盘".into());
        }
        let mut handles = Vec::new();
        let mut directory = PathBuf::new();
        for component in root.components() {
            if matches!(component, Component::CurDir | Component::ParentDir) {
                return Err("工作目录必须为规范绝对路径".into());
            }
            directory.push(component.as_os_str());
            if !matches!(component, Component::Normal(_) | Component::RootDir) {
                continue;
            }
            if create && matches!(component, Component::Normal(_)) {
                match std::fs::create_dir(&directory) {
                    Ok(()) => (),
                    Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => (),
                    Err(_) => return Err("无法建立本地恢复目录".into()),
                }
            }
            let handle = OpenOptions::new()
                .read(true)
                .share_mode(3)
                .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0 | FILE_FLAG_BACKUP_SEMANTICS.0)
                .open(&directory)
                .map_err(|_| "目录不可用或正在被替换")?;
            if info(&handle)?.dwFileAttributes & 0x10 == 0 {
                return Err("路径包含非目录项".into());
            }
            handles.push(handle);
        }
        let id = identity(&info(handles.last().ok_or("工作目录无效")?)?);
        Ok(Self {
            _handles: handles,
            identity: id,
        })
    }
}

// OPEN_REPARSE_POINT + create_new never follows or truncates an existing entry.
// Caller must hold the parent path pinned for the whole handle lifetime.
pub(super) fn journal_file(path: &Path, create: bool) -> Result<File, String> {
    let file = OpenOptions::new()
        .read(true)
        .write(create)
        .create_new(create)
        .share_mode(if create { 0 } else { 1 })
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0 | if create { 0x80000000 } else { 0 })
        .open(path)
        .map_err(|_| "恢复记录不可用；不会覆盖已有记录")?;
    let meta = info(&file)?;
    if meta.nNumberOfLinks != 1 || meta.dwFileAttributes & 0x10 != 0 {
        return Err("恢复记录必须为无硬链接的普通文件".into());
    }
    Ok(file)
}

pub(super) fn read_regular(
    file: &mut File,
    max: usize,
) -> Result<(Vec<u8>, FileIdentity, u64), String> {
    read_regular_impl(file, max, false)
}

// A parked-object recovery only renames the original object. It never writes
// its bytes, so late aliases must not prevent restoring an absent target name.
pub(super) fn read_parked(
    file: &mut File,
    max: usize,
) -> Result<(Vec<u8>, FileIdentity, u64), String> {
    read_regular_impl(file, max, true)
}

fn read_regular_impl(
    file: &mut File,
    max: usize,
    allow_aliases: bool,
) -> Result<(Vec<u8>, FileIdentity, u64), String> {
    let before = info(file)?;
    if before.dwFileAttributes & 0x10 != 0 || (!allow_aliases && before.nNumberOfLinks != 1) {
        return Err("只支持无硬链接的普通文件".into());
    }
    if before.nFileSizeHigh != 0 || before.nFileSizeLow as usize > max {
        return Err("文件超过完整读取上限，不使用截断内容".into());
    }
    file.seek(SeekFrom::Start(0)).map_err(|_| "文件定位失败")?;
    let mut bytes = Vec::new();
    file.take(max as u64 + 1)
        .read_to_end(&mut bytes)
        .map_err(|_| "文件读取失败")?;
    let after = info(file)?;
    if bytes.len() > max
        || bytes.len() != before.nFileSizeLow as usize
        || identity(&before) != identity(&after)
        || modified(&before) != modified(&after)
        || (!allow_aliases && after.nNumberOfLinks != 1)
        || before.nFileSizeLow != after.nFileSizeLow
        || after.nFileSizeHigh != 0
    {
        return Err("读取期间文件变化，请重新读取".into());
    }
    Ok((bytes, identity(&before), modified(&before)))
}

// Recovery inspection may read a known object with late aliases, but must not
// disclose bytes from an unrelated object placed at a recorded path.
pub(super) fn recorded_identity(file: &File) -> Result<FileIdentity, String> {
    let value = info(file)?;
    if value.dwFileAttributes & 0x10 != 0 {
        return Err("记录位置不是普通文件".into());
    }
    Ok(identity(&value))
}
pub(super) fn read_recorded(file: &mut File, expected: &FileIdentity) -> Result<Vec<u8>, String> {
    if recorded_identity(file)? != *expected {
        return Err("记录位置已被其他文件占用".into());
    }
    let (bytes, id, _) = read_regular_impl(file, MAX_BYTES, true)?;
    if id != *expected {
        return Err("记录对象变化".into());
    }
    Ok(bytes)
}

pub(super) struct ProtectedFile {
    pub(super) file: File,
    _parents: PinnedDirectory,
    root_identity: FileIdentity,
}
impl ProtectedFile {
    pub(super) fn read(&mut self) -> Result<ExactFile, String> {
        let (content, file_identity, modified) = read_regular(&mut self.file, MAX_BYTES)?;
        Ok(ExactFile {
            root_identity: self.root_identity.clone(),
            file_identity,
            modified,
            content,
        })
    }
}
pub(super) fn open(root: &Path, relative: &str, write: bool) -> Result<ProtectedFile, String> {
    validate_path(relative)?;
    let root_pin = PinnedDirectory::open(root, false)?;
    let target = root.join(relative);
    let parents = PinnedDirectory::open(target.parent().ok_or("文件路径无效")?, false)?;
    // The root is already included in parents. Overlapping no-delete handles
    // close the handover gap. Exclusive RW keeps the final compare and write
    // on one handle; no canonicalize-then-reopen write race.
    let file = OpenOptions::new()
        .read(true)
        .write(write)
        .share_mode(if write { 0 } else { 1 })
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(target)
        .map_err(|_| "文件不可用、只读或正在被使用，请重新读取")?;
    Ok(ProtectedFile {
        file,
        _parents: parents,
        root_identity: root_pin.identity.clone(),
    })
}
