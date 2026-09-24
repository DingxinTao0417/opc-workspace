//! Bounded, handle-only preservation of ordinary file metadata. Unknown stream
//! types and special file attributes are rejected, never silently discarded.
use crate::file_operation_contract::MAX_FILE_BYTES as MAX_BYTES;
use ::windows::Win32::Foundation::HANDLE;
use ::windows::Win32::Storage::FileSystem::{
    BackupRead, BackupWrite, FILE_BASIC_INFO, FileBasicInfo, GetFileInformationByHandleEx,
    SetFileInformationByHandle,
};
use serde::{Deserialize, Serialize};
use std::{fs::File, mem::size_of, os::windows::io::AsRawHandle};

const MAX_EXTRA_BYTES: usize = 64 * 1024;
const MAX_STREAMS: usize = 32;
const ALLOWED_ATTRIBUTES: u32 = 0x2 | 0x4 | 0x20 | 0x80 | 0x2000;
const HEADER: usize = 20; // offsetof(WIN32_STREAM_ID, cStreamName), not sizeof.

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct Metadata {
    created: i64,
    accessed: i64,
    attributes: u32,
    extra_streams: Vec<Vec<u8>>,
}

fn basic(file: &File) -> Result<FILE_BASIC_INFO, String> {
    let mut info = FILE_BASIC_INFO::default();
    // SAFETY: file owns a live handle and info is an aligned output structure.
    unsafe {
        GetFileInformationByHandleEx(
            HANDLE(file.as_raw_handle()),
            FileBasicInfo,
            (&mut info as *mut FILE_BASIC_INFO).cast(),
            size_of::<FILE_BASIC_INFO>() as u32,
        )
    }
    .map_err(|_| "无法完整读取文件属性")?;
    Ok(info)
}

struct Backup<'a> {
    file: &'a File,
    context: *mut std::ffi::c_void,
    writing: bool,
}
impl Drop for Backup<'_> {
    fn drop(&mut self) {
        let mut transferred = 0;
        // SAFETY: context belongs to the matching synchronous Backup API and
        // must be aborted on every success/error path to release its memory.
        unsafe {
            if self.writing {
                let _ = BackupWrite(
                    HANDLE(self.file.as_raw_handle()),
                    &[],
                    &mut transferred,
                    true,
                    false,
                    &mut self.context,
                );
                return;
            }
            {
                let _ = BackupRead(
                    HANDLE(self.file.as_raw_handle()),
                    &mut [],
                    &mut transferred,
                    true,
                    false,
                    &mut self.context,
                );
            }
        }
    }
}

fn backup_bytes(file: &File) -> Result<Vec<u8>, String> {
    let mut reader = Backup {
        file,
        context: std::ptr::null_mut(),
        writing: false,
    };
    let mut bytes = Vec::new();
    let mut buffer = [0u8; 32768];
    loop {
        let mut count = 0;
        // Security is captured separately; no backup/restore privilege is enabled.
        // SAFETY: initialized writable buffer, live synchronous handle/context.
        unsafe {
            BackupRead(
                HANDLE(file.as_raw_handle()),
                &mut buffer,
                &mut count,
                false,
                false,
                &mut reader.context,
            )
        }
        .map_err(|_| "无法完整检查文件附带数据")?;
        if count == 0 {
            return Ok(bytes);
        }
        let count = count as usize;
        if count > buffer.len() || bytes.len() + count > MAX_BYTES + MAX_EXTRA_BYTES + 8192 {
            return Err("文件附带数据超过保留上限，未替换".into());
        }
        bytes.extend_from_slice(&buffer[..count]);
    }
}

// Never restore BACKUP_LINK, security, reparse or unknown streams. Their payload
// may refer to unrelated paths or require semantics this writer cannot preserve.
fn parse_streams(raw: &[u8], expected: &[u8]) -> Result<Vec<Vec<u8>>, String> {
    let mut offset = 0;
    let mut extras = Vec::new();
    let mut default_seen = false;
    let mut count = 0;
    let mut extra_bytes = 0;
    while offset < raw.len() {
        count += 1;
        if count > MAX_STREAMS || raw.len() - offset < HEADER {
            return Err("文件流头不完整或数量超限".into());
        }
        let header = &raw[offset..offset + HEADER];
        let kind = u32::from_le_bytes(header[0..4].try_into().unwrap());
        let flags = u32::from_le_bytes(header[4..8].try_into().unwrap());
        let size = i64::from_le_bytes(header[8..16].try_into().unwrap());
        let name_size = u32::from_le_bytes(header[16..20].try_into().unwrap()) as usize;
        if size < 0
            || size as u64 > (MAX_BYTES + MAX_EXTRA_BYTES) as u64
            || name_size > 1024
            || name_size % 2 != 0
            || flags != 0
        {
            return Err("不支持此文件流结构，未替换".into());
        }
        let end = offset
            .checked_add(HEADER + name_size)
            .and_then(|n| n.checked_add(size as usize))
            .filter(|&n| n <= raw.len())
            .ok_or("文件流正文不完整")?;
        let payload = &raw[offset + HEADER + name_size..end];
        match kind {
            1 if name_size == 0 && !default_seen => {
                default_seen = true;
                if payload != expected {
                    return Err("检查附带数据时原文发生变化".into());
                }
            }
            2 if name_size == 0 => {
                extras.push(raw[offset..end].to_vec());
            }
            4 if name_size > 0 => {
                let units: Vec<u16> = raw[offset + HEADER..offset + HEADER + name_size]
                    .chunks_exact(2)
                    .map(|v| u16::from_le_bytes([v[0], v[1]]))
                    .collect();
                let name = String::from_utf16(&units).map_err(|_| "文件流名称编码无效")?;
                let label = name
                    .strip_prefix(':')
                    .and_then(|s| s.strip_suffix(":$DATA"))
                    .filter(|s| {
                        !s.is_empty() && !s.chars().any(|c| c.is_control() || "\\/:".contains(c))
                    })
                    .ok_or("不支持此附带文件流名称")?;
                let _ = label;
                extras.push(raw[offset..end].to_vec());
            }
            // Links belong to the retained source object, never the candidate.
            5 => (),
            _ => return Err("文件含尚未支持的元数据流，未替换".into()),
        }
        if matches!(kind, 2 | 4) {
            extra_bytes += end - offset;
            if extra_bytes > MAX_EXTRA_BYTES {
                return Err("附带数据超过 64 KiB，未替换".into());
            }
        }
        offset = end;
    }
    if !default_seen && !expected.is_empty() {
        return Err("缺少完整的文件主数据流".into());
    }
    // BackupRead stream enumeration order is not an authorization identity.
    extras.sort();
    if extras.windows(2).any(|v| v[0] == v[1]) {
        return Err("重复元数据流".into());
    }
    Ok(extras)
}

impl Metadata {
    #[allow(dead_code)] // Used by the separately compiled helper stage journal.
    pub(super) fn freeze(&self) -> Result<Vec<u8>, String> {
        self.validate_frozen()?;
        serde_json::to_vec(self).map_err(|_| "无法冻结完整文件元数据".into())
    }

    #[allow(dead_code)] // Used by the separately compiled helper stage journal.
    pub(super) fn thaw(bytes: &[u8]) -> Result<Self, String> {
        if bytes.is_empty() || bytes.len() > MAX_EXTRA_BYTES + 8192 {
            return Err("持久文件元数据为空或超限".into());
        }
        let value: Self = serde_json::from_slice(bytes).map_err(|_| "持久文件元数据结构无效")?;
        value.validate_frozen()?;
        if value.freeze()? != bytes {
            return Err("持久文件元数据不是规范编码".into());
        }
        Ok(value)
    }

    #[allow(dead_code)] // Reachable through helper-only freeze/thaw methods.
    fn validate_frozen(&self) -> Result<(), String> {
        if self.attributes & !ALLOWED_ATTRIBUTES != 0
            || self.extra_streams.len() > MAX_STREAMS
            || self.extra_streams.iter().map(Vec::len).sum::<usize>() > MAX_EXTRA_BYTES
            || self.extra_streams.windows(2).any(|v| v[0] >= v[1])
        {
            return Err("持久文件元数据属性、数量或顺序无效".into());
        }
        for stream in &self.extra_streams {
            if stream.len() < HEADER {
                return Err("持久文件流头不完整".into());
            }
            let kind = u32::from_le_bytes(stream[0..4].try_into().unwrap());
            let flags = u32::from_le_bytes(stream[4..8].try_into().unwrap());
            let size = i64::from_le_bytes(stream[8..16].try_into().unwrap());
            let name_size = u32::from_le_bytes(stream[16..20].try_into().unwrap()) as usize;
            if !matches!(kind, 2 | 4)
                || flags != 0
                || size < 0
                || name_size > 1024
                || name_size % 2 != 0
                || HEADER
                    .checked_add(name_size)
                    .and_then(|n| n.checked_add(size as usize))
                    != Some(stream.len())
                || (kind == 2 && name_size != 0)
                || (kind == 4 && name_size == 0)
            {
                return Err("持久文件流结构无效".into());
            }
            if kind == 4 {
                let units: Vec<u16> = stream[HEADER..HEADER + name_size]
                    .chunks_exact(2)
                    .map(|v| u16::from_le_bytes([v[0], v[1]]))
                    .collect();
                let name = String::from_utf16(&units).map_err(|_| "持久文件流名称编码无效")?;
                name.strip_prefix(':')
                    .and_then(|s| s.strip_suffix(":$DATA"))
                    .filter(|s| {
                        !s.is_empty() && !s.chars().any(|c| c.is_control() || "\\/:".contains(c))
                    })
                    .ok_or("持久文件流名称无效")?;
            }
        }
        Ok(())
    }

    pub(super) fn candidate_review_fingerprint(&self) -> Result<String, String> {
        let mut candidate = self.clone();
        candidate.attributes = (self.attributes & !0x80) | 0x20;
        candidate.review_fingerprint()
    }
    pub(super) fn review_fingerprint(&self) -> Result<String, String> {
        // LastAccessTime can change because of the review itself. Like
        // matches_original, bind creation/attributes/streams, not access time.
        #[derive(Serialize)]
        struct Stable<'a> {
            created: i64,
            attributes: u32,
            extra_streams: &'a [Vec<u8>],
        }
        let bytes = serde_json::to_vec(&Stable {
            created: self.created,
            attributes: self.attributes,
            extra_streams: &self.extra_streams,
        })
        .map_err(|_| "无法冻结文件元数据")?;
        Ok(crate::file_operation_contract::sha256(&bytes))
    }
    pub(super) fn capture(file: &File, expected: &[u8]) -> Result<Self, String> {
        let info = basic(file)?;
        if info.FileAttributes & !ALLOWED_ATTRIBUTES != 0 {
            return Err("只支持普通非只读文件；不替换压缩、加密、稀疏、离线或重解析文件".into());
        }
        let extra_streams = parse_streams(&backup_bytes(file)?, expected)?;
        let after = basic(file)?;
        if info.CreationTime != after.CreationTime || info.FileAttributes != after.FileAttributes {
            return Err("读取期间文件属性变化，未替换".into());
        }
        Ok(Self {
            created: info.CreationTime,
            accessed: info.LastAccessTime,
            attributes: info.FileAttributes,
            extra_streams,
        })
    }
    pub(super) fn matches_original(&self, other: &Self) -> bool {
        // A read may update access time. It is preserved, not used as a lock.
        self.created == other.created
            && self.attributes == other.attributes
            && self.extra_streams == other.extra_streams
    }
    pub(super) fn matches_candidate(&self, other: &Self) -> bool {
        self.created == other.created
            && ((self.attributes & !0x80) | 0x20) == other.attributes
            && self.extra_streams == other.extra_streams
    }
    pub(super) fn validate(&self) -> Result<(), String> {
        if self.attributes & !ALLOWED_ATTRIBUTES != 0
            || self.extra_streams.len() > MAX_STREAMS
            || self.extra_streams.iter().map(Vec::len).sum::<usize>() > MAX_EXTRA_BYTES
        {
            return Err("恢复元数据超限或无效".into());
        }
        // Every saved stream must parse strictly as a supported non-default stream.
        let joined = self.extra_streams.concat();
        if parse_streams(&joined, &[])? != self.extra_streams {
            return Err("恢复元数据结构无效".into());
        }
        Ok(())
    }
    #[allow(dead_code)] // Production use is isolated to the separately compiled helper.
    pub(super) fn apply(&self, candidate: &File, expected: &[u8]) -> Result<(), String> {
        self.validate()?;
        let mut writer = Backup {
            file: candidate,
            context: std::ptr::null_mut(),
            writing: true,
        };
        for stream in &self.extra_streams {
            let mut count = 0;
            // SAFETY: buffer was strictly parsed; writes only EA/named DATA on
            // this newly created, held file. No link/reparse/security restore.
            unsafe {
                BackupWrite(
                    HANDLE(candidate.as_raw_handle()),
                    stream,
                    &mut count,
                    false,
                    false,
                    &mut writer.context,
                )
            }
            .map_err(|_| "无法完整保留文件附带数据")?;
            if count as usize != stream.len() {
                return Err("附带数据未完整写入".into());
            }
        }
        drop(writer);
        let info = FILE_BASIC_INFO {
            CreationTime: self.created,
            LastAccessTime: self.accessed,
            LastWriteTime: 0,
            ChangeTime: 0,
            FileAttributes: (self.attributes & !0x80) | 0x20, // edited => archive bit
        };
        // SAFETY: aligned initialized input and a live candidate handle with
        // WRITE_ATTRIBUTES; the original file metadata is never changed.
        unsafe {
            SetFileInformationByHandle(
                HANDLE(candidate.as_raw_handle()),
                FileBasicInfo,
                (&info as *const FILE_BASIC_INFO).cast(),
                size_of::<FILE_BASIC_INFO>() as u32,
            )
        }
        .map_err(|_| "无法保留创建时间及普通文件属性")?;
        candidate.sync_all().map_err(|_| "候选元数据未能刷盘")?;
        let current = Self::capture(candidate, expected)?;
        if current.created != self.created
            || current.extra_streams != self.extra_streams
            || current.attributes != info.FileAttributes
        {
            return Err("候选文件附带数据或属性核验失败".into());
        }
        Ok(())
    }
}

#[cfg(test)]
#[path = "replacement/metadata/tests.rs"]
mod tests;
