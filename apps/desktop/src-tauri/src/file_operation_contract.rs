//! Shared single-file REQUEST contract, not an execution capability. Both the
//! ordinary desktop and isolated helper validate the same bounded schema. A
//! successful decode proves structure only, not disk facts or human approval.
use base64::{Engine, engine::general_purpose::STANDARD};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::time::{Duration, Instant};

pub(crate) const MAX_FILE_BYTES: usize = 256 * 1024;
pub(crate) const MAX_REQUEST_BYTES: usize = 1024 * 1024;
pub(crate) const MAX_LIFETIME_MS: u64 = 600_000;
const MAX_RECEIPT_BYTES: usize = 256;

/// A pipe-bound terminal acknowledgement, not a standalone proof or a retry
/// token. The transport already binds it to one frozen request; callers must
/// additionally compare the expected operation id before showing success.
#[derive(Debug, PartialEq, Eq)]
pub(crate) enum OperationReceipt {
    ReviewCancelled,
    Succeeded,
}

#[derive(Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct OperationReceiptWire {
    version: u32,
    status: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    operation_id: Option<String>,
}

impl OperationReceipt {
    pub(crate) fn review_cancelled() -> Vec<u8> {
        // A constant, infallible representation is useful on cancellation:
        // no project details or mutable error strings cross the boundary.
        br#"{"version":1,"status":"review_cancelled"}"#.to_vec()
    }

    pub(crate) fn succeeded(operation_id: &str) -> Result<Vec<u8>, &'static str> {
        if !valid_id(operation_id) {
            return Err("文件操作回执身份无效");
        }
        let bytes = serde_json::to_vec(&OperationReceiptWire {
            version: 1,
            status: "succeeded".into(),
            operation_id: Some(operation_id.into()),
        })
        .map_err(|_| "文件操作回执编码失败")?;
        if bytes.len() > MAX_RECEIPT_BYTES {
            return Err("文件操作回执超限");
        }
        Ok(bytes)
    }

    pub(crate) fn decode(bytes: &[u8], expected_operation_id: &str) -> Result<Self, &'static str> {
        if bytes.is_empty() || bytes.len() > MAX_RECEIPT_BYTES || !valid_id(expected_operation_id) {
            return Err("文件操作回执为空、超限或预期身份无效");
        }
        let wire: OperationReceiptWire =
            serde_json::from_slice(bytes).map_err(|_| "文件操作回执结构无效")?;
        let result = match (
            wire.version,
            wire.status.as_str(),
            wire.operation_id.as_deref(),
        ) {
            (1, "review_cancelled", None) => Self::ReviewCancelled,
            (1, "succeeded", Some(id)) if id == expected_operation_id => Self::Succeeded,
            _ => return Err("文件操作回执状态或身份不匹配"),
        };
        let canonical = match result {
            Self::ReviewCancelled => Self::review_cancelled(),
            Self::Succeeded => Self::succeeded(expected_operation_id)?,
        };
        if canonical != bytes {
            return Err("文件操作回执不是规范编码");
        }
        Ok(result)
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ObjectIdentity {
    pub(crate) volume: u32,
    pub(crate) index: u64,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Content {
    size: usize,
    sha256: String,
    base64: String,
}
impl Content {
    pub(crate) fn digest(&self) -> &str {
        &self.sha256
    }

    pub(crate) fn from_bytes(bytes: &[u8]) -> Result<Self, &'static str> {
        if bytes.len() > MAX_FILE_BYTES {
            return Err("完整文件超过单文件上限");
        }
        Ok(Self {
            size: bytes.len(),
            sha256: sha256(bytes),
            base64: STANDARD.encode(bytes),
        })
    }
    pub(crate) fn decode(&self) -> Result<Vec<u8>, &'static str> {
        if self.size > MAX_FILE_BYTES
            || self.base64.len() > MAX_FILE_BYTES.div_ceil(3) * 4
            || !valid_digest(&self.sha256)
        {
            return Err("文件正文声明无效或超限");
        }
        let bytes = STANDARD
            .decode(&self.base64)
            .map_err(|_| "文件正文不是规范 Base64")?;
        if bytes.len() != self.size
            || sha256(&bytes) != self.sha256
            || STANDARD.encode(&bytes) != self.base64
        {
            return Err("文件正文、长度或摘要不一致");
        }
        Ok(bytes)
    }
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct ObservedFile {
    pub(crate) identity: ObjectIdentity,
    pub(crate) modified: u64,
    pub(crate) content: Content,
    // Explicitly PARTIAL ordinary-user security, never a full SACL proof.
    pub(crate) readable_security_sha256: String,
    pub(crate) metadata_sha256: String,
}
impl ObservedFile {
    pub(crate) fn content_digest(&self) -> &str {
        self.content.digest()
    }

    pub(crate) fn decode_content(&self) -> Result<Vec<u8>, &'static str> {
        self.content.decode()
    }

    fn validate(&self) -> Result<Vec<u8>, &'static str> {
        if !valid_digest(&self.readable_security_sha256) || !valid_digest(&self.metadata_sha256) {
            return Err("可读权限或元数据指纹无效");
        }
        self.content.decode()
    }
}

#[derive(Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case", deny_unknown_fields)]
pub(crate) enum Operation {
    Replace {
        original: ObservedFile,
        candidate: Content,
    },
    RestoreMissing {
        record_version: u32,
        record_id: String,
        record_sha256: String,
        original: ObservedFile,
        candidate: ObservedFile,
    },
    UndoInstalled {
        record_version: u32,
        record_id: String,
        record_sha256: String,
        original: ObservedFile,
        candidate: ObservedFile,
    },
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Request {
    pub(crate) version: u32,
    pub(crate) operation_id: String,
    pub(crate) review_id: String,
    pub(crate) root_selection_id: String,
    pub(crate) issued_at_ms: u64,
    pub(crate) expires_at_ms: u64,
    pub(crate) root: String,
    pub(crate) root_identity: ObjectIdentity,
    pub(crate) parent_identity: ObjectIdentity,
    pub(crate) path: String,
    pub(crate) operation: Operation,
}

/// Private validated state is still UNAPPROVED. It cannot be deserialized or
/// upgraded into an execution permit by setting a boolean on the wire.
pub(crate) struct CheckedRequest {
    request: Request,
    deadline: Instant,
}
impl CheckedRequest {
    #[cfg(test)]
    pub(crate) fn monotonic_deadline_for_test(&self) -> Instant {
        self.deadline
    }
    #[cfg(test)]
    pub(crate) fn expire_for_test(&mut self) {
        self.deadline = Instant::now();
    }
    pub(crate) fn from_local(request: Request, now_ms: u64) -> Result<Self, &'static str> {
        request.validate(now_ms)?;
        let deadline = Instant::now() + Duration::from_millis(request.expires_at_ms - now_ms);
        let result = Self { request, deadline };
        result.encode(now_ms)?;
        Ok(result)
    }
    pub(crate) fn decode(bytes: &[u8], now_ms: u64) -> Result<Self, &'static str> {
        if bytes.is_empty() || bytes.len() > MAX_REQUEST_BYTES {
            return Err("单文件请求为空或超限");
        }
        let request: Request = serde_json::from_slice(bytes).map_err(|_| "单文件请求结构无效")?;
        request.validate(now_ms)?;
        let deadline = Instant::now() + Duration::from_millis(request.expires_at_ms - now_ms);
        Ok(Self { request, deadline })
    }
    pub(crate) fn encode(&self, now_ms: u64) -> Result<Vec<u8>, &'static str> {
        self.check_time(now_ms)?;
        self.request.validate(now_ms)?;
        let bytes = serde_json::to_vec(&self.request).map_err(|_| "单文件请求编码失败")?;
        if bytes.len() > MAX_REQUEST_BYTES {
            return Err("单文件请求编码后超限");
        }
        Ok(bytes)
    }
    pub(crate) fn facts(&self) -> &Request {
        &self.request
    }
    pub(crate) fn check_time(&self, now_ms: u64) -> Result<(), &'static str> {
        if Instant::now() >= self.deadline {
            return Err("单文件请求已超过本机有效期");
        }
        self.request.check_time(now_ms)
    }
    /// Absolute deadline for a request-bound wait. Never mint a fresh lifetime
    /// from the declared TTL; wall-clock rollback cannot extend the original
    /// monotonic deadline retained by this checked instance.
    pub(crate) fn wait_deadline(&self, now_ms: u64) -> Result<Instant, &'static str> {
        let instant = Instant::now();
        self.check_time(now_ms)?;
        Ok(self
            .deadline
            .min(instant + Duration::from_millis(self.request.expires_at_ms - now_ms)))
    }
}
impl Request {
    /// Validate durable intent facts without requiring that its short-lived
    /// approval window is still open. This never recreates CheckedRequest or
    /// authorizes execution after expiry.
    pub(crate) fn validate_persisted(&self) -> Result<(), &'static str> {
        if self.version != 1
            || !valid_id(&self.operation_id)
            || !valid_id(&self.review_id)
            || !valid_id(&self.root_selection_id)
            || self.issued_at_ms == 0
            || self.expires_at_ms <= self.issued_at_ms
            || self.expires_at_ms - self.issued_at_ms > MAX_LIFETIME_MS
        {
            return Err("单文件请求版本、身份或持久期限无效");
        }
        validate_root(&self.root)?;
        validate_relative(&self.path)?;
        if self.root_identity.volume != self.parent_identity.volume {
            return Err("目录卷身份不一致");
        }
        let (original, before, after) = match &self.operation {
            Operation::Replace {
                original,
                candidate,
            } => {
                let before = original.validate()?;
                let after = candidate.decode()?;
                for text in [&before, &after] {
                    if text.contains(&0) || std::str::from_utf8(text).is_err() {
                        return Err("替换建议必须是完整 UTF-8 文本，不含 NUL");
                    }
                }
                (original, before, after)
            }
            Operation::RestoreMissing {
                record_version,
                record_id,
                record_sha256,
                original,
                candidate,
            }
            | Operation::UndoInstalled {
                record_version,
                record_id,
                record_sha256,
                original,
                candidate,
            } => {
                if *record_version != 2
                    || !valid_id(record_id)
                    || !valid_digest(record_sha256)
                    || original.identity == candidate.identity
                    || candidate.identity.volume != self.parent_identity.volume
                {
                    return Err("恢复记录或双方文件身份无效");
                }
                (original, original.validate()?, candidate.validate()?)
            }
        };
        if original.identity.volume != self.parent_identity.volume || before == after {
            return Err("原对象卷身份不符或正文无变化");
        }
        Ok(())
    }
    fn check_time(&self, now_ms: u64) -> Result<(), &'static str> {
        if self.issued_at_ms == 0
            || self.issued_at_ms > now_ms
            || self.expires_at_ms <= now_ms
            || self
                .expires_at_ms
                .checked_sub(self.issued_at_ms)
                .is_none_or(|v| v == 0 || v > MAX_LIFETIME_MS)
        {
            return Err("单文件审查期限无效或已过期");
        }
        Ok(())
    }
    fn validate(&self, now_ms: u64) -> Result<(), &'static str> {
        self.validate_persisted()?;
        self.check_time(now_ms)?;
        Ok(())
    }
}

pub(crate) fn sha256(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}
fn valid_digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}
fn valid_id(value: &str) -> bool {
    uuid::Uuid::parse_str(value).is_ok_and(|v| v.to_string() == value)
}

pub(crate) fn validate_relative(path: &str) -> Result<(), &'static str> {
    if path.is_empty() || path.len() > 4096 || path.contains(['\\', ':']) {
        return Err("请选择目录内的规范相对文件路径");
    }
    for part in path.split('/') {
        validate_name(part)?;
    }
    Ok(())
}
fn validate_name(part: &str) -> Result<(), &'static str> {
    let stem = part
        .split('.')
        .next()
        .unwrap_or_default()
        .to_ascii_uppercase();
    let device = matches!(
        stem.as_str(),
        "CON" | "PRN" | "AUX" | "NUL" | "CONIN$" | "CONOUT$"
    ) || ["COM", "LPT"].iter().any(|prefix| {
        stem.strip_prefix(prefix).is_some_and(|n| {
            matches!(
                n,
                "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" | "¹" | "²" | "³"
            )
        })
    });
    if part.is_empty()
        || matches!(part, "." | "..")
        || part.eq_ignore_ascii_case(".git")
        || part.ends_with(['.', ' '])
        || part
            .chars()
            .any(|c| c.is_control() || "<>\"|?*:".contains(c))
        || device
    {
        return Err("不支持路径跳转、Git 元数据或非规范文件名");
    }
    Ok(())
}
fn validate_root(root: &str) -> Result<(), &'static str> {
    if root.len() > 4096 {
        return Err("所选根目录路径超限");
    }
    let path = root.strip_prefix(r"\\?\").unwrap_or(root);
    let bytes = path.as_bytes();
    if bytes.len() < 3
        || !bytes[0].is_ascii_alphabetic()
        || &bytes[1..3] != b":\\"
        || path.contains('/')
    {
        return Err("所选根目录必须是规范本机磁盘绝对路径");
    }
    if path.len() > 3 {
        for part in path[3..].split('\\') {
            validate_name(part)?;
        }
    }
    Ok(())
}

#[cfg(test)]
#[path = "file_operation_contract/tests.rs"]
mod tests;
