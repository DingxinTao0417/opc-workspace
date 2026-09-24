//! Ordinary-permission, read-only bridge from native reviews to the shared
//! single-file request. Consuming a review is NOT permission to execute it.
use super::replacement_metadata::Metadata;
use super::replacement_security::SecurityDescriptor;
use super::*;
use crate::file_operation_contract::{
    CheckedRequest, Content, ObjectIdentity, ObservedFile, Operation, Request, sha256,
};
use std::time::{SystemTime, UNIX_EPOCH};

pub(super) fn identity(id: &FileIdentity) -> ObjectIdentity {
    ObjectIdentity {
        volume: id.volume,
        index: id.index,
    }
}
pub(super) fn observed(
    id: &FileIdentity,
    modified: u64,
    content: &[u8],
    metadata: &Metadata,
    readable_security: &[u8],
) -> Result<ObservedFile, String> {
    Ok(ObservedFile {
        identity: identity(id),
        modified,
        content: Content::from_bytes(content)?,
        metadata_sha256: metadata.review_fingerprint()?,
        readable_security_sha256: sha256(readable_security),
    })
}
pub(super) fn now_ms() -> Result<u64, String> {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis();
    millis.try_into().map_err(|_| "本机时钟超出范围".into())
}
pub(super) fn times(created: Instant) -> Result<(u64, u64), String> {
    let remaining = TTL
        .checked_sub(created.elapsed())
        .ok_or("原审查已过期")?
        .as_millis() as u64;
    if remaining == 0 {
        return Err("原审查已过期".into());
    }
    let now = now_ms()?;
    let expires = now.checked_add(remaining).ok_or("审查期限超限")?;
    Ok((now, expires))
}
pub(super) fn root_text(root: &Path) -> Result<String, String> {
    root.to_str()
        .map(str::to_owned)
        .ok_or("所选目录包含无法精确传递的字符".into())
}

impl FileSnapshotState {
    pub(super) fn freeze_edit_request(
        &self,
        root_id: &str,
        root: &Path,
        snapshot_id: &str,
        operation_id: &str,
        candidate: &[u8],
    ) -> Result<CheckedRequest, String> {
        // Consume before every attempted conversion, including wrong selection,
        // invalid candidate, expired review or disk conflict. No resurrect/retry.
        let snapshot = self
            .0
            .lock()
            .map_err(|_| "文件审查不可用")?
            .remove(snapshot_id)
            .ok_or("文件审查已使用或释放")?;
        if snapshot.root_id != root_id || snapshot.root != root || snapshot.recovery.is_some() {
            return Err("文件审查不属于本次替换操作或目录选择".into());
        }
        times(snapshot.created)?;
        let candidate = Content::from_bytes(candidate)?;
        let mut held = windows::open(root, &snapshot.path, false)?;
        let current = held.read()?;
        if current != snapshot.file {
            return Err("原文件或目录已变化，请重新审查".into());
        }
        let target = root.join(&snapshot.path);
        let parent = windows::PinnedDirectory::open(target.parent().ok_or("文件路径无效")?, false)?;
        let metadata = Metadata::capture(&held.file, &current.content)?;
        let security = SecurityDescriptor::capture(&held.file)?;
        if held.read()? != snapshot.file {
            return Err("冻结请求期间原文件已变化".into());
        }
        let (issued_at_ms, expires_at_ms) = times(snapshot.created)?;
        let original = observed(
            &current.file_identity,
            current.modified,
            &current.content,
            &metadata,
            security.bytes(),
        )?;
        CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: operation_id.into(),
                review_id: snapshot_id.into(),
                root_selection_id: root_id.into(),
                issued_at_ms,
                expires_at_ms,
                root: root_text(root)?,
                root_identity: identity(&current.root_identity),
                parent_identity: identity(&parent.identity),
                path: snapshot.path,
                operation: Operation::Replace {
                    original,
                    candidate,
                },
            },
            issued_at_ms,
        )
        .map_err(str::to_owned)
    }
}

#[cfg(test)]
#[path = "request_bridge/tests.rs"]
mod tests;
