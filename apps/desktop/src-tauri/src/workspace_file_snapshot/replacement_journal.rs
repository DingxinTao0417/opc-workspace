//! Read-only version 2 intent decoding; replacement writers remain test-only.
use super::edits::{FileReplacementPreview, ReplacementFileState as Status};
use super::*;
pub(super) use crate::file_recovery_record::Intent;
use ::windows::Win32::Storage::FileSystem::FILE_FLAG_OPEN_REPARSE_POINT;
use base64::{Engine, engine::general_purpose::STANDARD};
use std::{
    fs::{File, OpenOptions},
    os::windows::fs::OpenOptionsExt,
};

// All observed handles stay open until the complete response is constructed.
// This is a point-in-time observation, never an approval token or recovery lock.
struct Observed {
    status: Status,
    content: Option<Vec<u8>>,
    _file: Option<File>,
}
fn observe(path: &Path, intent: &Intent, source: bool, candidate: bool) -> Observed {
    let mut file = match OpenOptions::new()
        .read(true)
        .share_mode(1)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
    {
        Ok(file) => file,
        Err(error) => {
            return Observed {
                status: if error.raw_os_error() == Some(2) {
                    Status::Missing
                } else {
                    Status::Unavailable
                },
                content: None,
                _file: None,
            };
        }
    };
    let mut result = Observed {
        status: Status::Unavailable,
        content: None,
        _file: None,
    };
    if let Ok(identity) = windows::recorded_identity(&file) {
        let expected = if source && identity == intent.source_identity {
            Some((&intent.before, Status::Original))
        } else if candidate && identity == intent.candidate_identity {
            Some((&intent.after, Status::Candidate))
        } else {
            None
        };
        if let Some((bytes, matching)) = expected {
            if let Ok(content) = windows::read_recorded(&mut file, &identity) {
                result.status = if content == *bytes {
                    matching
                } else {
                    Status::Changed
                };
                result.content = Some(content);
            }
        } else {
            // No read of a concurrently created user's unrelated object.
            result.status = Status::Foreign;
        }
    }
    result._file = Some(file);
    result
}

pub(super) fn preview(
    storage: &Path,
    id: &str,
    root: &Path,
) -> Result<FileReplacementPreview, String> {
    let intent = load(storage, id)?;
    if intent.root != root {
        return Err("替换记录不属于所选目录".into());
    }
    let root_pin = windows::PinnedDirectory::open(root, false)?;
    let target = intent.target();
    let parent = windows::PinnedDirectory::open(target.parent().ok_or("记录路径无效")?, false)?;
    if root_pin.identity != intent.root_identity || parent.identity != intent.parent_identity {
        return Err("记录的根目录或父目录已被替换，不能核对".into());
    }
    let target = observe(&target, &intent, true, true);
    let parked = observe(&intent.sibling("original"), &intent, true, false);
    let staged = observe(&intent.sibling("candidate"), &intent, false, true);
    let observation = match (&target.status, &parked.status, &staged.status) {
        (Status::Original, Status::Missing, Status::Candidate) => "source_present",
        (Status::Missing, Status::Original, Status::Candidate) => "target_missing",
        (Status::Candidate, Status::Original, Status::Missing) => "candidate_present",
        _ => "conflict",
    };
    Ok(FileReplacementPreview {
        id: intent.id,
        path: intent.path,
        observation: observation.into(),
        target: target.status,
        parked: parked.status,
        staged: staged.status,
        current_base64: target.content.as_ref().map(|v| STANDARD.encode(v)),
        original_base64: STANDARD.encode(intent.before),
        candidate_base64: STANDARD.encode(intent.after),
    })
}

pub(super) fn load(storage: &Path, id: &str) -> Result<Intent, String> {
    if !uuid::Uuid::parse_str(id).is_ok_and(|v| v.to_string() == id) {
        return Err("恢复身份无效".into());
    }
    let _pin = windows::PinnedDirectory::open(storage, false)?;
    let mut file = windows::journal_file(&storage.join(format!("{id}.json")), false)?;
    let (bytes, _, _) = windows::read_regular(&mut file, 4 * 1024 * 1024)?;
    crate::file_recovery_record::decode(&bytes, id)
}
