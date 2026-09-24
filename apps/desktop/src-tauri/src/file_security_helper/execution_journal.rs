//! Durable preparation and conservative "started means unknown" markers.
//! These records do not execute, resume, roll back, or prove an outcome.
use super::{
    recovery_record::RecoveryRecordEvidence,
    security::FullDescriptor,
    security_inspection::{
        CandidateEvidence, FrozenFileObservation, FrozenSecurityInspection, VerifiedFileEvidence,
    },
};
use crate::{
    file_operation_contract::{CheckedRequest, MAX_REQUEST_BYTES, Operation, Request, sha256},
    file_operation_metadata::Metadata,
    file_operation_read::{PinnedDirectory, journal_file, read_regular},
};
use base64::{Engine, engine::general_purpose::STANDARD};
use serde::{Deserialize, Serialize};
use std::{
    collections::{HashMap, HashSet},
    fs::{self, File},
    io::{ErrorKind, Write},
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};

const DIRECTORY: &str = "workspace-file-operation-stages-v1";
const MAX_OPERATIONS: usize = 64;
const MAX_PREPARED_BYTES: usize = 3 * 1024 * 1024;
const MAX_MATERIALIZED_BYTES: usize = 2048;
const MAX_RECOVERY_BYTES: usize = 2048;
const MAX_STARTED_BYTES: usize = 1024;
const MAX_OUTCOME_BYTES: usize = 2048;
type Result<T> = std::result::Result<T, String>;

#[derive(Clone)]
pub(super) struct AttemptIdentity {
    pub(super) operation_id: String,
    pub(super) review_id: String,
    pub(super) request_sha256: String,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum StageState {
    Prepared,
    Materialized,
    RecoveryIntentBound,
    UnknownAfterStart,
    Succeeded,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct FrozenFile {
    metadata_base64: String,
    security_base64: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Prepared {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    prepared_at_ms: u64,
    request_base64: String,
    original: FrozenFile,
    candidate: Option<FrozenFile>,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredPrepared {
    prepared: Prepared,
    digest: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Materialized {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    prepared_sha256: String,
    materialized_at_ms: u64,
    candidate_name: Option<String>,
    candidate_identity: crate::file_operation_contract::ObjectIdentity,
    candidate_modified: u64,
    candidate_content_sha256: String,
    candidate_metadata_sha256: String,
    candidate_readable_security_sha256: String,
    candidate_full_security_sha256: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredMaterialized {
    materialized: Materialized,
    digest: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct RecoveryBound {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    prepared_sha256: String,
    materialized_sha256: String,
    bound_at_ms: u64,
    record_version: u32,
    record_id: String,
    record_sha256: String,
    stored_sha256: String,
    record_identity: crate::file_operation_contract::ObjectIdentity,
    record_modified: u64,
    source_identity: crate::file_operation_contract::ObjectIdentity,
    candidate_identity: crate::file_operation_contract::ObjectIdentity,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredRecoveryBound {
    recovery: RecoveryBound,
    digest: String,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Started {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    prepared_sha256: String,
    materialized_sha256: String,
    recovery_sha256: String,
    started_at_ms: u64,
}
#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredStarted {
    started: Started,
    digest: String,
}

#[derive(Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct OutcomeFile {
    identity: crate::file_operation_contract::ObjectIdentity,
    content_sha256: String,
    metadata_sha256: String,
    readable_security_sha256: String,
    full_security_sha256: String,
}

/// Constructed only from post-mutation handles after complete readback. The
/// durable journal validates every field against the frozen request and stages.
pub(super) struct ExecutionOutcomeEvidence {
    mode: &'static str,
    target: OutcomeFile,
    retained: OutcomeFile,
}
impl ExecutionOutcomeEvidence {
    fn file(value: &VerifiedFileEvidence) -> OutcomeFile {
        OutcomeFile {
            identity: value.identity().clone(),
            content_sha256: value.content_sha256().into(),
            metadata_sha256: value.metadata_sha256().into(),
            readable_security_sha256: value.readable_security_sha256().into(),
            full_security_sha256: value.full_security_sha256().into(),
        }
    }

    pub(super) fn replace(target: &VerifiedFileEvidence, retained: &VerifiedFileEvidence) -> Self {
        Self {
            mode: "replace",
            target: Self::file(target),
            retained: Self::file(retained),
        }
    }

    pub(super) fn restore_missing(
        target: &VerifiedFileEvidence,
        retained: &VerifiedFileEvidence,
    ) -> Self {
        Self {
            mode: "restore_missing",
            target: Self::file(target),
            retained: Self::file(retained),
        }
    }

    pub(super) fn undo_installed(
        target: &VerifiedFileEvidence,
        retained: &VerifiedFileEvidence,
    ) -> Self {
        Self {
            mode: "undo_installed",
            target: Self::file(target),
            retained: Self::file(retained),
        }
    }
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct Outcome {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    prepared_sha256: String,
    materialized_sha256: String,
    recovery_sha256: String,
    started_sha256: String,
    completed_at_ms: u64,
    mode: String,
    target: OutcomeFile,
    retained: OutcomeFile,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct StoredOutcome {
    outcome: Outcome,
    digest: String,
}

fn now() -> Result<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}
fn valid_digest(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn valid_id(value: &str) -> bool {
    uuid::Uuid::parse_str(value).is_ok_and(|id| id.to_string() == value)
}
fn canonical<T: Serialize>(value: &T) -> Result<Vec<u8>> {
    serde_json::to_vec(value).map_err(|_| "操作阶段记录编码失败".into())
}
fn decode_base64(value: &str, max: usize, label: &str) -> Result<Vec<u8>> {
    if value.len() > max.div_ceil(3) * 4 {
        return Err(format!("{label}超限"));
    }
    let bytes = STANDARD
        .decode(value)
        .map_err(|_| format!("{label}编码无效"))?;
    if bytes.len() > max || STANDARD.encode(&bytes) != value {
        return Err(format!("{label}不是规范编码"));
    }
    Ok(bytes)
}
impl FrozenFile {
    fn from_observation(value: &FrozenFileObservation) -> Self {
        Self {
            metadata_base64: STANDARD.encode(&value.metadata),
            security_base64: STANDARD.encode(&value.security),
        }
    }
    fn validate(&self) -> Result<Metadata> {
        let metadata = Metadata::thaw(&decode_base64(
            &self.metadata_base64,
            72 * 1024,
            "完整元数据",
        )?)?;
        // This proves bounded descriptor grammar only. The store is mutable by
        // the same user; a future recovery must compare it to live pinned facts.
        FullDescriptor::thaw(&decode_base64(
            &self.security_base64,
            64 * 1024,
            "完整权限描述",
        )?)
        .map_err(str::to_owned)?;
        Ok(metadata)
    }
}
impl Prepared {
    fn create(request: &CheckedRequest, inspection: &FrozenSecurityInspection) -> Result<Self> {
        let at = now()?;
        request.check_time(at)?;
        let request_bytes = request.encode(at)?;
        if sha256(&request_bytes) != inspection.request_sha256 {
            return Err("持久准备与完整权限观察不一致".into());
        }
        let result = Self {
            version: 1,
            operation_id: request.facts().operation_id.clone(),
            review_id: request.facts().review_id.clone(),
            request_sha256: inspection.request_sha256.clone(),
            prepared_at_ms: at,
            request_base64: STANDARD.encode(request_bytes),
            original: FrozenFile::from_observation(&inspection.original),
            candidate: inspection
                .candidate
                .as_ref()
                .map(FrozenFile::from_observation),
        };
        result.validate(None)?;
        Ok(result)
    }
    fn validate(&self, attempt: Option<&AttemptIdentity>) -> Result<Request> {
        if self.version != 1
            || !valid_id(&self.operation_id)
            || !valid_id(&self.review_id)
            || !valid_digest(&self.request_sha256)
            || self.prepared_at_ms == 0
        {
            return Err("持久准备身份、摘要或时间无效".into());
        }
        if let Some(attempt) = attempt {
            if self.operation_id != attempt.operation_id
                || self.review_id != attempt.review_id
                || self.request_sha256 != attempt.request_sha256
            {
                return Err("持久准备与尝试登记不一致".into());
            }
        }
        let request_bytes = decode_base64(&self.request_base64, MAX_REQUEST_BYTES, "冻结请求")?;
        if sha256(&request_bytes) != self.request_sha256 {
            return Err("冻结请求摘要不一致".into());
        }
        let request: Request =
            serde_json::from_slice(&request_bytes).map_err(|_| "冻结请求结构无效")?;
        request.validate_persisted()?;
        if request.operation_id != self.operation_id || request.review_id != self.review_id {
            return Err("冻结请求与操作阶段身份不一致".into());
        }
        let original_metadata = self.original.validate()?;
        let (expected_original, expected_candidate) = match &request.operation {
            Operation::Replace { original, .. } => (original, None),
            Operation::RestoreMissing {
                original,
                candidate,
                ..
            }
            | Operation::UndoInstalled {
                original,
                candidate,
                ..
            } => (original, Some(candidate)),
        };
        if original_metadata.review_fingerprint()? != expected_original.metadata_sha256 {
            return Err("持久准备的原对象元数据不一致".into());
        }
        match (&self.candidate, expected_candidate) {
            (None, None) => (),
            (Some(frozen), Some(expected))
                if frozen.validate()?.review_fingerprint()? == expected.metadata_sha256 => {}
            _ => return Err("持久准备的候选对象元数据不一致".into()),
        }
        Ok(request)
    }
}
impl StoredPrepared {
    fn create(prepared: Prepared) -> Result<Self> {
        let digest = sha256(&canonical(&prepared)?);
        Ok(Self { prepared, digest })
    }
    fn decode(bytes: &[u8], attempt: &AttemptIdentity) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_PREPARED_BYTES {
            return Err("持久准备记录为空或超限".into());
        }
        let result: Self =
            serde_json::from_slice(bytes).map_err(|_| "持久准备记录损坏；结果需人工检查")?;
        result.prepared.validate(Some(attempt))?;
        if !valid_digest(&result.digest)
            || result.digest != sha256(&canonical(&result.prepared)?)
            || canonical(&result)? != bytes
        {
            return Err("持久准备记录摘要或编码无效".into());
        }
        Ok(result)
    }
}
impl Materialized {
    fn for_existing_recovery(prepared: &StoredPrepared) -> Result<Option<Self>> {
        let request = prepared.prepared.validate(None)?;
        let candidate = match &request.operation {
            Operation::Replace { .. } => return Ok(None),
            Operation::RestoreMissing { candidate, .. }
            | Operation::UndoInstalled { candidate, .. } => candidate,
        };
        let frozen = prepared
            .prepared
            .candidate
            .as_ref()
            .ok_or("恢复准备缺少候选完整描述")?;
        let full = decode_base64(&frozen.security_base64, 64 * 1024, "候选完整权限描述")?;
        let result = Self {
            version: 1,
            operation_id: prepared.prepared.operation_id.clone(),
            review_id: prepared.prepared.review_id.clone(),
            request_sha256: prepared.prepared.request_sha256.clone(),
            prepared_sha256: prepared.digest.clone(),
            materialized_at_ms: now()?,
            candidate_name: None,
            candidate_identity: candidate.identity.clone(),
            candidate_modified: candidate.modified,
            candidate_content_sha256: candidate.decode_content().map(|v| sha256(&v))?,
            candidate_metadata_sha256: candidate.metadata_sha256.clone(),
            candidate_readable_security_sha256: candidate.readable_security_sha256.clone(),
            candidate_full_security_sha256: sha256(&full),
        };
        result.validate(prepared)?;
        Ok(Some(result))
    }

    fn for_new_candidate(prepared: &StoredPrepared, evidence: &CandidateEvidence) -> Result<Self> {
        let result = Self {
            version: 1,
            operation_id: prepared.prepared.operation_id.clone(),
            review_id: prepared.prepared.review_id.clone(),
            request_sha256: prepared.prepared.request_sha256.clone(),
            prepared_sha256: prepared.digest.clone(),
            materialized_at_ms: now()?,
            candidate_name: Some(evidence.name().into()),
            candidate_identity: evidence.identity().clone(),
            candidate_modified: evidence.modified(),
            candidate_content_sha256: evidence.content_sha256().into(),
            candidate_metadata_sha256: evidence.metadata_sha256().into(),
            candidate_readable_security_sha256: evidence.readable_security_sha256().into(),
            candidate_full_security_sha256: evidence.full_security_sha256().into(),
        };
        result.validate(prepared)?;
        Ok(result)
    }

    fn validate(&self, prepared: &StoredPrepared) -> Result<()> {
        if self.version != 1
            || self.operation_id != prepared.prepared.operation_id
            || self.review_id != prepared.prepared.review_id
            || self.request_sha256 != prepared.prepared.request_sha256
            || self.prepared_sha256 != prepared.digest
            || self.materialized_at_ms < prepared.prepared.prepared_at_ms
            || !valid_digest(&self.candidate_content_sha256)
            || !valid_digest(&self.candidate_metadata_sha256)
            || !valid_digest(&self.candidate_readable_security_sha256)
            || !valid_digest(&self.candidate_full_security_sha256)
        {
            return Err("候选实体记录与持久准备不一致".into());
        }
        let request = prepared.prepared.validate(None)?;
        let original = prepared.prepared.original.validate()?;
        let original_full = decode_base64(
            &prepared.prepared.original.security_base64,
            64 * 1024,
            "原对象完整权限描述",
        )?;
        match &request.operation {
            Operation::Replace {
                original: expected_original,
                candidate,
            } => {
                if self.candidate_name.as_deref()
                    != Some(format!(".opc-file-{}.candidate", self.operation_id).as_str())
                    || self.candidate_identity == expected_original.identity
                    || self.candidate_identity.volume != request.parent_identity.volume
                    || self.candidate_content_sha256 != candidate.digest()
                    || self.candidate_metadata_sha256 != original.candidate_review_fingerprint()?
                    || self.candidate_readable_security_sha256
                        != expected_original.readable_security_sha256
                    || self.candidate_full_security_sha256 != sha256(&original_full)
                {
                    return Err("新候选实体与冻结请求或完整元数据不一致".into());
                }
            }
            Operation::RestoreMissing { candidate, .. }
            | Operation::UndoInstalled { candidate, .. } => {
                let frozen = prepared
                    .prepared
                    .candidate
                    .as_ref()
                    .ok_or("恢复候选完整描述缺失")?;
                let full =
                    decode_base64(&frozen.security_base64, 64 * 1024, "恢复候选完整权限描述")?;
                if self.candidate_name.is_some()
                    || self.candidate_identity != candidate.identity
                    || self.candidate_modified != candidate.modified
                    || self.candidate_content_sha256 != candidate.content_digest()
                    || self.candidate_metadata_sha256 != candidate.metadata_sha256
                    || self.candidate_readable_security_sha256 != candidate.readable_security_sha256
                    || self.candidate_full_security_sha256 != sha256(&full)
                {
                    return Err("恢复候选实体与冻结请求不一致".into());
                }
            }
        }
        Ok(())
    }
}
impl StoredMaterialized {
    fn create(materialized: Materialized) -> Result<Self> {
        let digest = sha256(&canonical(&materialized)?);
        Ok(Self {
            materialized,
            digest,
        })
    }
    fn decode(bytes: &[u8], prepared: &StoredPrepared) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_MATERIALIZED_BYTES {
            return Err("候选实体记录为空或超限".into());
        }
        let result: Self =
            serde_json::from_slice(bytes).map_err(|_| "候选实体记录损坏；不能开始执行")?;
        result.materialized.validate(prepared)?;
        if !valid_digest(&result.digest)
            || result.digest != sha256(&canonical(&result.materialized)?)
            || canonical(&result)? != bytes
        {
            return Err("候选实体记录摘要或编码无效".into());
        }
        Ok(result)
    }
}
impl RecoveryBound {
    fn create(
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        evidence: &RecoveryRecordEvidence,
    ) -> Result<Self> {
        let result = Self {
            version: 1,
            operation_id: prepared.prepared.operation_id.clone(),
            review_id: prepared.prepared.review_id.clone(),
            request_sha256: prepared.prepared.request_sha256.clone(),
            prepared_sha256: prepared.digest.clone(),
            materialized_sha256: materialized.digest.clone(),
            bound_at_ms: now()?,
            record_version: evidence.record_version(),
            record_id: evidence.record_id().into(),
            record_sha256: evidence.record_sha256().into(),
            stored_sha256: evidence.stored_sha256().into(),
            record_identity: evidence.identity().clone(),
            record_modified: evidence.modified(),
            source_identity: evidence.source_identity().clone(),
            candidate_identity: evidence.candidate_identity().clone(),
        };
        result.validate(prepared, materialized)?;
        Ok(result)
    }

    fn validate(&self, prepared: &StoredPrepared, materialized: &StoredMaterialized) -> Result<()> {
        if self.version != 1
            || self.operation_id != prepared.prepared.operation_id
            || self.review_id != prepared.prepared.review_id
            || self.request_sha256 != prepared.prepared.request_sha256
            || self.prepared_sha256 != prepared.digest
            || self.materialized_sha256 != materialized.digest
            || self.bound_at_ms < materialized.materialized.materialized_at_ms
            || self.record_version != 2
            || !valid_id(&self.record_id)
            || !valid_digest(&self.record_sha256)
            || !valid_digest(&self.stored_sha256)
        {
            return Err("恢复意图绑定与持久准备不一致".into());
        }
        let request = prepared.prepared.validate(None)?;
        match &request.operation {
            Operation::Replace { original, .. }
                if self.record_id == request.operation_id
                    && self.source_identity == original.identity
                    && self.candidate_identity == materialized.materialized.candidate_identity => {}
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
            } if self.record_version == *record_version
                && self.record_id == *record_id
                && self.record_sha256 == *record_sha256
                && self.source_identity == original.identity
                && self.candidate_identity == candidate.identity
                && self.candidate_identity == materialized.materialized.candidate_identity => {}
            _ => return Err("恢复意图身份与冻结请求不一致".into()),
        }
        Ok(())
    }
}
impl StoredRecoveryBound {
    fn create(recovery: RecoveryBound) -> Result<Self> {
        let digest = sha256(&canonical(&recovery)?);
        Ok(Self { recovery, digest })
    }
    fn decode(
        bytes: &[u8],
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
    ) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_RECOVERY_BYTES {
            return Err("恢复意图绑定记录为空或超限".into());
        }
        let result: Self =
            serde_json::from_slice(bytes).map_err(|_| "恢复意图绑定损坏；不能开始执行")?;
        result.recovery.validate(prepared, materialized)?;
        if !valid_digest(&result.digest)
            || result.digest != sha256(&canonical(&result.recovery)?)
            || canonical(&result)? != bytes
        {
            return Err("恢复意图绑定摘要或编码无效".into());
        }
        Ok(result)
    }
}
impl Started {
    fn create(
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
    ) -> Result<Self> {
        Ok(Self {
            version: 1,
            operation_id: prepared.prepared.operation_id.clone(),
            review_id: prepared.prepared.review_id.clone(),
            request_sha256: prepared.prepared.request_sha256.clone(),
            prepared_sha256: prepared.digest.clone(),
            materialized_sha256: materialized.digest.clone(),
            recovery_sha256: recovery.digest.clone(),
            started_at_ms: now()?,
        })
    }
    fn validate(
        &self,
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
    ) -> Result<()> {
        if self.version != 1
            || self.operation_id != prepared.prepared.operation_id
            || self.review_id != prepared.prepared.review_id
            || self.request_sha256 != prepared.prepared.request_sha256
            || self.prepared_sha256 != prepared.digest
            || self.materialized_sha256 != materialized.digest
            || self.recovery_sha256 != recovery.digest
            || self.started_at_ms < recovery.recovery.bound_at_ms
        {
            return Err("执行开始记录与持久准备不一致".into());
        }
        Ok(())
    }
}
impl StoredStarted {
    fn create(started: Started) -> Result<Self> {
        let digest = sha256(&canonical(&started)?);
        Ok(Self { started, digest })
    }
    fn decode(
        bytes: &[u8],
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
    ) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_STARTED_BYTES {
            return Err("执行开始记录为空或超限；结果必须视为未知".into());
        }
        let result: Self =
            serde_json::from_slice(bytes).map_err(|_| "执行开始记录损坏；结果必须视为未知")?;
        result.started.validate(prepared, materialized, recovery)?;
        if !valid_digest(&result.digest)
            || result.digest != sha256(&canonical(&result.started)?)
            || canonical(&result)? != bytes
        {
            return Err("执行开始记录摘要或编码无效；结果必须视为未知".into());
        }
        Ok(result)
    }
}

fn expected_outcome(
    prepared: &StoredPrepared,
    materialized: &StoredMaterialized,
) -> Result<(&'static str, OutcomeFile, OutcomeFile)> {
    let request = prepared.prepared.validate(None)?;
    let original_observed = match &request.operation {
        Operation::Replace { original, .. }
        | Operation::RestoreMissing { original, .. }
        | Operation::UndoInstalled { original, .. } => original,
    };
    let original = OutcomeFile {
        identity: original_observed.identity.clone(),
        content_sha256: original_observed.content_digest().into(),
        metadata_sha256: original_observed.metadata_sha256.clone(),
        readable_security_sha256: original_observed.readable_security_sha256.clone(),
        full_security_sha256: sha256(&decode_base64(
            &prepared.prepared.original.security_base64,
            64 * 1024,
            "原对象完整权限描述",
        )?),
    };
    let candidate = OutcomeFile {
        identity: materialized.materialized.candidate_identity.clone(),
        content_sha256: materialized.materialized.candidate_content_sha256.clone(),
        metadata_sha256: materialized.materialized.candidate_metadata_sha256.clone(),
        readable_security_sha256: materialized
            .materialized
            .candidate_readable_security_sha256
            .clone(),
        full_security_sha256: materialized
            .materialized
            .candidate_full_security_sha256
            .clone(),
    };
    Ok(match request.operation {
        Operation::Replace { .. } => ("replace", candidate, original),
        Operation::RestoreMissing { .. } => ("restore_missing", original, candidate),
        Operation::UndoInstalled { .. } => ("undo_installed", original, candidate),
    })
}

impl Outcome {
    fn create(
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
        started: &StoredStarted,
        evidence: &ExecutionOutcomeEvidence,
    ) -> Result<Self> {
        let result = Self {
            version: 1,
            operation_id: prepared.prepared.operation_id.clone(),
            review_id: prepared.prepared.review_id.clone(),
            request_sha256: prepared.prepared.request_sha256.clone(),
            prepared_sha256: prepared.digest.clone(),
            materialized_sha256: materialized.digest.clone(),
            recovery_sha256: recovery.digest.clone(),
            started_sha256: started.digest.clone(),
            completed_at_ms: now()?,
            mode: evidence.mode.into(),
            target: evidence.target.clone(),
            retained: evidence.retained.clone(),
        };
        result.validate(prepared, materialized, recovery, started)?;
        Ok(result)
    }

    fn validate(
        &self,
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
        started: &StoredStarted,
    ) -> Result<()> {
        let (mode, target, retained) = expected_outcome(prepared, materialized)?;
        let file_valid = |file: &OutcomeFile| {
            valid_digest(&file.content_sha256)
                && valid_digest(&file.metadata_sha256)
                && valid_digest(&file.readable_security_sha256)
                && valid_digest(&file.full_security_sha256)
        };
        if self.version != 1
            || self.operation_id != prepared.prepared.operation_id
            || self.review_id != prepared.prepared.review_id
            || self.request_sha256 != prepared.prepared.request_sha256
            || self.prepared_sha256 != prepared.digest
            || self.materialized_sha256 != materialized.digest
            || self.recovery_sha256 != recovery.digest
            || self.started_sha256 != started.digest
            || self.completed_at_ms < started.started.started_at_ms
            || self.mode != mode
            || self.target != target
            || self.retained != retained
            || !file_valid(&self.target)
            || !file_valid(&self.retained)
        {
            return Err("成功回执与冻结请求或完整磁盘终态不一致".into());
        }
        Ok(())
    }
}

impl StoredOutcome {
    fn create(outcome: Outcome) -> Result<Self> {
        let digest = sha256(&canonical(&outcome)?);
        Ok(Self { outcome, digest })
    }

    fn decode(
        bytes: &[u8],
        prepared: &StoredPrepared,
        materialized: &StoredMaterialized,
        recovery: &StoredRecoveryBound,
        started: &StoredStarted,
    ) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_OUTCOME_BYTES {
            return Err("成功回执为空或超限；结果必须视为未知".into());
        }
        let result: Self =
            serde_json::from_slice(bytes).map_err(|_| "成功回执损坏；结果必须视为未知")?;
        result
            .outcome
            .validate(prepared, materialized, recovery, started)?;
        if !valid_digest(&result.digest)
            || result.digest != sha256(&canonical(&result.outcome)?)
            || canonical(&result)? != bytes
        {
            return Err("成功回执摘要或编码无效；结果必须视为未知".into());
        }
        Ok(result)
    }
}

struct HeldFile {
    file: File,
    identity: crate::file_operation_contract::ObjectIdentity,
    digest: String,
    max: usize,
}
impl HeldFile {
    fn create(path: &Path, bytes: &[u8], max: usize, failure: &'static str) -> Result<Self> {
        let mut file = journal_file(path, true)?;
        // From CREATE_NEW onward, every failure retains the entry.
        file.write_all(bytes).map_err(|_| failure.to_string())?;
        file.sync_all().map_err(|_| failure.to_string())?;
        let (stored, identity, _) = read_regular(&mut file, max)?;
        if stored != bytes {
            return Err(failure.into());
        }
        Ok(Self {
            file,
            identity,
            digest: sha256(bytes),
            max,
        })
    }
    fn revalidate(&mut self) -> Result<Vec<u8>> {
        let (bytes, identity, _) = read_regular(&mut self.file, self.max)?;
        if identity != self.identity || sha256(&bytes) != self.digest {
            return Err("操作阶段记录在持有期间变化；结果需人工检查".into());
        }
        Ok(bytes)
    }
}

pub(super) struct ValidatedStages {
    pub(super) states: HashMap<String, StageState>,
    held: Vec<HeldFile>,
    _directory: Option<PinnedDirectory>,
}
impl ValidatedStages {
    pub(super) fn revalidate(&mut self) -> Result<()> {
        for file in &mut self.held {
            file.revalidate()?;
        }
        Ok(())
    }
}

enum EntryKind {
    Prepared,
    Materialized,
    Recovery,
    Started,
    Outcome,
}
fn entry_name(name: &str) -> Option<(&str, EntryKind)> {
    name.strip_suffix(".prepared.json")
        .map(|id| (id, EntryKind::Prepared))
        .or_else(|| {
            name.strip_suffix(".materialized.json")
                .map(|id| (id, EntryKind::Materialized))
        })
        .or_else(|| {
            name.strip_suffix(".recovery.json")
                .map(|id| (id, EntryKind::Recovery))
        })
        .or_else(|| {
            name.strip_suffix(".started.json")
                .map(|id| (id, EntryKind::Started))
        })
        .or_else(|| {
            name.strip_suffix(".outcome.json")
                .map(|id| (id, EntryKind::Outcome))
        })
        .filter(|(id, _)| valid_id(id))
}
pub(super) fn validate_store(
    app_dir: &Path,
    attempts: &[AttemptIdentity],
) -> Result<ValidatedStages> {
    let storage = app_dir.join(DIRECTORY);
    match fs::metadata(&storage) {
        Err(error) if error.kind() == ErrorKind::NotFound => {
            return Ok(ValidatedStages {
                states: HashMap::new(),
                held: Vec::new(),
                _directory: None,
            });
        }
        Err(_) => return Err("无法检查操作阶段目录".into()),
        Ok(_) => (),
    }
    let directory = PinnedDirectory::open(&storage, false)?;
    let known: HashMap<_, _> = attempts
        .iter()
        .map(|attempt| (attempt.operation_id.as_str(), attempt))
        .collect();
    let mut prepared = HashMap::<String, (StoredPrepared, HeldFile)>::new();
    let mut materialized = HashMap::<String, (Vec<u8>, HeldFile)>::new();
    let mut recovery = HashMap::<String, (Vec<u8>, HeldFile)>::new();
    let mut started = HashMap::<String, (Vec<u8>, HeldFile)>::new();
    let mut outcome = HashMap::<String, (Vec<u8>, HeldFile)>::new();
    let mut entries = 0;
    for entry in fs::read_dir(&storage).map_err(|_| "无法完整检查操作阶段目录")? {
        let entry = entry.map_err(|_| "无法完整检查操作阶段目录")?;
        entries += 1;
        if entries > MAX_OPERATIONS * 5 {
            return Err("操作阶段记录超过容量，需人工管理".into());
        }
        let name = entry.file_name();
        let name = name.to_str().ok_or("操作阶段目录含非 Unicode 条目")?;
        let (id, kind) = entry_name(name).ok_or("操作阶段目录含未知条目，需人工检查")?;
        let attempt = known
            .get(id)
            .ok_or("操作阶段记录没有对应尝试，需人工检查")?;
        let max = match kind {
            EntryKind::Prepared => MAX_PREPARED_BYTES,
            EntryKind::Materialized => MAX_MATERIALIZED_BYTES,
            EntryKind::Recovery => MAX_RECOVERY_BYTES,
            EntryKind::Started => MAX_STARTED_BYTES,
            EntryKind::Outcome => MAX_OUTCOME_BYTES,
        };
        let mut file = journal_file(&entry.path(), false)?;
        let (bytes, identity, _) = read_regular(&mut file, max)?;
        let held = HeldFile {
            file,
            identity,
            digest: sha256(&bytes),
            max,
        };
        match kind {
            EntryKind::Prepared => {
                let decoded = StoredPrepared::decode(&bytes, attempt)?;
                if prepared.insert(id.into(), (decoded, held)).is_some() {
                    return Err("操作阶段准备身份重复".into());
                }
            }
            EntryKind::Materialized => {
                if materialized.insert(id.into(), (bytes, held)).is_some() {
                    return Err("候选实体身份重复".into());
                }
            }
            EntryKind::Recovery => {
                if recovery.insert(id.into(), (bytes, held)).is_some() {
                    return Err("恢复意图绑定身份重复".into());
                }
            }
            EntryKind::Started => {
                if started.insert(id.into(), (bytes, held)).is_some() {
                    return Err("执行开始身份重复".into());
                }
            }
            EntryKind::Outcome => {
                if outcome.insert(id.into(), (bytes, held)).is_some() {
                    return Err("成功回执身份重复".into());
                }
            }
        }
    }
    if prepared.len() > MAX_OPERATIONS {
        return Err("操作阶段记录达到容量，需人工管理".into());
    }
    let mut states = HashMap::new();
    let mut held = Vec::new();
    let mut seen_reviews = HashSet::new();
    for (id, (prepared, file)) in prepared {
        if !seen_reviews.insert(prepared.prepared.review_id.clone()) {
            return Err("操作阶段含重复审查身份".into());
        }
        held.push(file);
        if let Some((bytes, materialized_file)) = materialized.remove(&id) {
            let materialized = StoredMaterialized::decode(&bytes, &prepared)?;
            held.push(materialized_file);
            if let Some((bytes, recovery_file)) = recovery.remove(&id) {
                let recovery = StoredRecoveryBound::decode(&bytes, &prepared, &materialized)?;
                held.push(recovery_file);
                if let Some((bytes, started_file)) = started.remove(&id) {
                    let started =
                        StoredStarted::decode(&bytes, &prepared, &materialized, &recovery)?;
                    held.push(started_file);
                    if let Some((bytes, outcome_file)) = outcome.remove(&id) {
                        StoredOutcome::decode(
                            &bytes,
                            &prepared,
                            &materialized,
                            &recovery,
                            &started,
                        )?;
                        held.push(outcome_file);
                        states.insert(id, StageState::Succeeded);
                    } else {
                        states.insert(id, StageState::UnknownAfterStart);
                    }
                } else if outcome.contains_key(&id) {
                    return Err("成功回执缺少执行开始记录；结果必须视为未知".into());
                } else {
                    states.insert(id, StageState::RecoveryIntentBound);
                }
            } else if started.contains_key(&id) || outcome.contains_key(&id) {
                return Err("执行开始记录缺少恢复意图绑定；结果必须视为未知".into());
            } else {
                states.insert(id, StageState::Materialized);
            }
        } else if recovery.contains_key(&id)
            || started.contains_key(&id)
            || outcome.contains_key(&id)
        {
            return Err("执行阶段记录缺少候选实体；结果必须视为未知".into());
        } else {
            states.insert(id, StageState::Prepared);
        }
    }
    if !materialized.is_empty()
        || !recovery.is_empty()
        || !started.is_empty()
        || !outcome.is_empty()
    {
        return Err("操作阶段记录缺少对应持久准备；结果需人工检查".into());
    }
    Ok(ValidatedStages {
        states,
        held,
        _directory: Some(directory),
    })
}

pub(super) struct PreparedStage {
    request_sha256: String,
    stored: StoredPrepared,
    prepared: HeldFile,
    materialized: Option<(StoredMaterialized, HeldFile)>,
    recovery: Option<(StoredRecoveryBound, HeldFile)>,
    started: Option<(StoredStarted, HeldFile)>,
    outcome: Option<(StoredOutcome, HeldFile)>,
    storage: PathBuf,
    _directory: PinnedDirectory,
}
impl PreparedStage {
    pub(super) fn revalidate(&mut self, request: &CheckedRequest) -> Result<()> {
        self.revalidate_inner(request, true)
    }

    pub(super) fn revalidate_after_start(&mut self, request: &CheckedRequest) -> Result<()> {
        self.revalidate_inner(request, false)
    }

    fn revalidate_inner(&mut self, request: &CheckedRequest, require_live: bool) -> Result<()> {
        if require_live {
            request.check_time(now()?)?;
        }
        request.facts().validate_persisted()?;
        let request_bytes = canonical(request.facts())?;
        if request_bytes.len() > MAX_REQUEST_BYTES || sha256(&request_bytes) != self.request_sha256
        {
            return Err("操作阶段不属于本次冻结请求".into());
        }
        let bytes = self.prepared.revalidate()?;
        StoredPrepared::decode(
            &bytes,
            &AttemptIdentity {
                operation_id: request.facts().operation_id.clone(),
                review_id: request.facts().review_id.clone(),
                request_sha256: self.request_sha256.clone(),
            },
        )?;
        if let Some((materialized, file)) = &mut self.materialized {
            StoredMaterialized::decode(&file.revalidate()?, &self.stored)?;
            if let Some((recovery, recovery_file)) = &mut self.recovery {
                StoredRecoveryBound::decode(
                    &recovery_file.revalidate()?,
                    &self.stored,
                    materialized,
                )?;
                if let Some((started, started_file)) = &mut self.started {
                    StoredStarted::decode(
                        &started_file.revalidate()?,
                        &self.stored,
                        materialized,
                        recovery,
                    )?;
                    if let Some((_, outcome_file)) = &mut self.outcome {
                        StoredOutcome::decode(
                            &outcome_file.revalidate()?,
                            &self.stored,
                            materialized,
                            recovery,
                            started,
                        )?;
                    }
                } else if self.outcome.is_some() {
                    return Err("成功回执缺少执行开始；结果必须视为未知".into());
                }
            } else if self.started.is_some() || self.outcome.is_some() {
                return Err("执行开始缺少恢复意图绑定；结果必须视为未知".into());
            }
        } else if self.recovery.is_some() || self.started.is_some() || self.outcome.is_some() {
            return Err("恢复意图或执行开始缺少候选实体；结果需人工检查".into());
        }
        if require_live {
            request.check_time(now()?)?;
        }
        Ok(())
    }
    fn write_materialized(&mut self, materialized: Materialized) -> Result<()> {
        if self.materialized.is_some() {
            return Err("候选实体已登记；不会重新绑定".into());
        }
        materialized.validate(&self.stored)?;
        let stored = StoredMaterialized::create(materialized)?;
        let bytes = canonical(&stored)?;
        let file = HeldFile::create(
            &self.storage.join(format!(
                "{}.materialized.json",
                self.stored.prepared.operation_id
            )),
            &bytes,
            MAX_MATERIALIZED_BYTES,
            "候选实体登记失败；不会开始执行",
        )?;
        self.materialized = Some((stored, file));
        Ok(())
    }
    pub(super) fn bind_candidate(
        &mut self,
        request: &CheckedRequest,
        evidence: &CandidateEvidence,
    ) -> Result<()> {
        self.revalidate(request)?;
        self.write_materialized(Materialized::for_new_candidate(&self.stored, evidence)?)?;
        self.revalidate(request)
    }
    pub(super) fn bind_recovery(
        &mut self,
        request: &CheckedRequest,
        evidence: &RecoveryRecordEvidence,
    ) -> Result<()> {
        if self.recovery.is_some() {
            return Err("恢复意图已绑定；不会替换".into());
        }
        self.revalidate(request)?;
        let materialized = self
            .materialized
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("候选尚未实体化；不能绑定恢复意图")?;
        let stored = StoredRecoveryBound::create(RecoveryBound::create(
            &self.stored,
            materialized,
            evidence,
        )?)?;
        let bytes = canonical(&stored)?;
        let file = HeldFile::create(
            &self.storage.join(format!(
                "{}.recovery.json",
                self.stored.prepared.operation_id
            )),
            &bytes,
            MAX_RECOVERY_BYTES,
            "恢复意图绑定失败；不会开始执行",
        )?;
        self.recovery = Some((stored, file));
        self.revalidate(request)
    }
    /// Must be the last durable action immediately before the first mutation.
    /// Once written, absence of an outcome MUST be presented as unknown.
    pub(super) fn mark_started(&mut self, request: &CheckedRequest) -> Result<()> {
        if self.started.is_some() {
            return Err("执行已经标记开始；结果未知，不能重试".into());
        }
        self.revalidate(request)?;
        let materialized = self
            .materialized
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("候选尚未实体化并持久绑定；不能开始执行")?;
        let recovery = self
            .recovery
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("恢复意图尚未刷盘并持久绑定；不能开始执行")?;
        let stored = StoredStarted::create(Started::create(&self.stored, materialized, recovery)?)?;
        let bytes = canonical(&stored)?;
        let file = HeldFile::create(
            &self.storage.join(format!(
                "{}.started.json",
                self.stored.prepared.operation_id
            )),
            &bytes,
            MAX_STARTED_BYTES,
            "执行开始登记失败；结果必须视为未知",
        )
        .map_err(|_| "执行开始登记不可用；结果必须视为未知且不得重试".to_string())?;
        self.started = Some((stored, file));
        self.revalidate(request)
            .map_err(|_| "执行已经登记开始但复核失败；结果必须视为未知且不得重试".into())
    }

    /// Records only a completely verified success. Errors after started never
    /// call this method and therefore remain UnknownAfterStart.
    pub(super) fn mark_succeeded(
        &mut self,
        request: &CheckedRequest,
        evidence: &ExecutionOutcomeEvidence,
    ) -> Result<()> {
        if self.outcome.is_some() {
            return Err("执行成功回执已存在；不会重写".into());
        }
        self.revalidate_inner(request, false)?;
        let materialized = self
            .materialized
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("候选实体缺失；不能登记成功")?;
        let recovery = self
            .recovery
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("恢复意图绑定缺失；不能登记成功")?;
        let started = self
            .started
            .as_ref()
            .map(|(stored, _)| stored)
            .ok_or("执行尚未登记开始；不能登记成功")?;
        let stored = StoredOutcome::create(Outcome::create(
            &self.stored,
            materialized,
            recovery,
            started,
            evidence,
        )?)?;
        let bytes = canonical(&stored)?;
        let file = HeldFile::create(
            &self.storage.join(format!(
                "{}.outcome.json",
                self.stored.prepared.operation_id
            )),
            &bytes,
            MAX_OUTCOME_BYTES,
            "成功回执写入失败；结果必须视为未知",
        )
        .map_err(|_| "成功回执不可用；结果必须视为未知且不得重试".to_string())?;
        self.outcome = Some((stored, file));
        self.revalidate_inner(request, false)
            .map_err(|_| "成功回执已写入但复核失败；结果必须视为未知且不得重试".into())
    }
}

pub(super) fn prepare(
    app_dir: &Path,
    request: &CheckedRequest,
    inspection: &FrozenSecurityInspection,
) -> Result<PreparedStage> {
    request.check_time(now()?)?;
    let _app = PinnedDirectory::open(app_dir, false)?;
    let storage = app_dir.join(DIRECTORY);
    match fs::create_dir(&storage) {
        Ok(()) => (),
        Err(error) if error.kind() == ErrorKind::AlreadyExists => (),
        Err(_) => return Err("无法建立操作阶段目录".into()),
    }
    let directory = PinnedDirectory::open(&storage, false)?;
    let prepared = Prepared::create(request, inspection)?;
    let stored = StoredPrepared::create(prepared)?;
    let bytes = canonical(&stored)?;
    if bytes.len() > MAX_PREPARED_BYTES {
        return Err("持久准备记录超限".into());
    }
    let held = HeldFile::create(
        &storage.join(format!("{}.prepared.json", request.facts().operation_id)),
        &bytes,
        MAX_PREPARED_BYTES,
        "持久准备写入失败；已保留记录，不能自动重试",
    )?;
    let mut result = PreparedStage {
        request_sha256: inspection.request_sha256.clone(),
        stored,
        prepared: held,
        materialized: None,
        recovery: None,
        started: None,
        outcome: None,
        storage,
        _directory: directory,
    };
    if let Some(materialized) = Materialized::for_existing_recovery(&result.stored)? {
        result.write_materialized(materialized)?;
    }
    result.revalidate(request)?;
    Ok(result)
}

#[cfg(test)]
#[path = "execution_journal_tests.rs"]
mod tests;
