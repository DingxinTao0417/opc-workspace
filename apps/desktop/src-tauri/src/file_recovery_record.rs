//! Shared v2 recovery observation grammar. Digests detect changes, not an
//! authenticated history. Never applies security bytes from a stored record.
use crate::file_operation_contract::{
    CheckedRequest, MAX_FILE_BYTES, ObjectIdentity, Operation, sha256, validate_relative,
};
use crate::file_operation_metadata::Metadata;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

pub(crate) const MAX_RECORD_BYTES: usize = 4 * 1024 * 1024;
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub(crate) struct Intent {
    pub(crate) version: u32,
    pub(crate) id: String,
    pub(crate) root: PathBuf,
    pub(crate) path: String,
    pub(crate) root_identity: ObjectIdentity,
    pub(crate) parent_identity: ObjectIdentity,
    pub(crate) source_identity: ObjectIdentity,
    pub(crate) candidate_identity: ObjectIdentity,
    pub(crate) before: Vec<u8>,
    pub(crate) after: Vec<u8>,
    pub(crate) metadata: Metadata,
    pub(crate) security: Vec<u8>,
}
impl Intent {
    pub(crate) fn validate(&self) -> Result<(), String> {
        validate_relative(&self.path)?;
        self.metadata.validate()?;
        if self.version != 2
            || !valid_id(&self.id)
            || !self.root.is_absolute()
            || self.before.len() > MAX_FILE_BYTES
            || self.after.len() > MAX_FILE_BYTES
            || self.before == self.after
            || self.source_identity == self.candidate_identity
            || self.security.is_empty()
            || self.security.len() > 64 * 1024
        {
            return Err("替换恢复意图无效".into());
        }
        Ok(())
    }
    pub(crate) fn target(&self) -> PathBuf {
        self.root.join(&self.path)
    }
    pub(crate) fn sibling(&self, suffix: &str) -> PathBuf {
        self.target()
            .parent()
            .unwrap()
            .join(format!(".opc-file-{}.{suffix}", self.id))
    }
    pub(crate) fn digest(&self) -> Result<String, String> {
        Ok(sha256(
            &serde_json::to_vec(self).map_err(|_| "恢复记录编码失败")?,
        ))
    }
    pub(crate) fn matches_request(
        &self,
        request: &CheckedRequest,
        now_ms: u64,
    ) -> Result<(), String> {
        request.check_time(now_ms)?;
        self.validate()?;
        let facts = request.facts();
        let (version, id, digest, original, candidate) = match &facts.operation {
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
            } => (
                *record_version,
                record_id,
                record_sha256,
                original,
                candidate,
            ),
            Operation::Replace { .. } => return Err("新替换请求不能冒充恢复请求".into()),
        };
        if self.version != version
            || &self.id != id
            || &self.digest()? != digest
            || self.root != Path::new(&facts.root)
            || self.path != facts.path
            || self.root_identity != facts.root_identity
            || self.parent_identity != facts.parent_identity
            || self.source_identity != original.identity
            || self.candidate_identity != candidate.identity
            || self.before != original.content.decode()?
            || self.after != candidate.content.decode()?
            || self.metadata.review_fingerprint()? != original.metadata_sha256
            || self.metadata.candidate_review_fingerprint()? != candidate.metadata_sha256
            || sha256(&self.security) != original.readable_security_sha256
            || sha256(&self.security) != candidate.readable_security_sha256
        {
            return Err("恢复记录与本次审查请求不一致".into());
        }
        Ok(())
    }
}
pub(crate) fn valid_id(id: &str) -> bool {
    uuid::Uuid::parse_str(id).is_ok_and(|v| v.to_string() == id)
}
pub(crate) fn decode(bytes: &[u8], id: &str) -> Result<Intent, String> {
    if !valid_id(id) || bytes.is_empty() || bytes.len() > MAX_RECORD_BYTES {
        return Err("恢复记录身份或长度无效".into());
    }
    #[derive(Deserialize)]
    #[serde(deny_unknown_fields)]
    struct Stored {
        intent: Intent,
        digest: String,
    }
    let stored: Stored = serde_json::from_slice(bytes).map_err(|_| "替换意图损坏")?;
    stored.intent.validate()?;
    if stored.intent.id != id || stored.intent.digest()? != stored.digest {
        return Err("替换意图完整性检查失败".into());
    }
    Ok(stored.intent)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn legacy_v2_field_order_and_canonical_digest_remain_unchanged() {
        // Frozen legacy format, including field order used by existing digests.
        // The partial security bytes are grammar fixtures, never applied to ACLs.
        let encoded = r#"{"version":2,"id":"12345678-1234-4234-8234-123456789abc","root":"C:\\fixture","path":"file.txt","root_identity":{"volume":1,"index":1},"parent_identity":{"volume":1,"index":1},"source_identity":{"volume":1,"index":2},"candidate_identity":{"volume":1,"index":3},"before":[1],"after":[2],"metadata":{"created":1,"accessed":2,"attributes":32,"extra_streams":[]},"security":[7]}"#;
        let digest = sha256(encoded.as_bytes());
        let stored = format!(r#"{{"intent":{encoded},"digest":"{digest}"}}"#);
        let intent = decode(stored.as_bytes(), "12345678-1234-4234-8234-123456789abc").unwrap();
        assert_eq!(serde_json::to_string(&intent).unwrap(), encoded);
        assert_eq!(intent.digest().unwrap(), digest);
    }
}
