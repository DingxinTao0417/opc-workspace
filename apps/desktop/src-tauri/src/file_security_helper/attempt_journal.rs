//! Cooperative cross-process attempt consumption, NOT authenticated history,
//! a backup, successful execution, or permission to resume an operation.
use crate::{
    file_operation_contract::{CheckedRequest, ObjectIdentity, sha256},
    file_operation_read::{PinnedDirectory, journal_file, read_regular},
};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashSet,
    fs::{self, File, OpenOptions},
    io::{ErrorKind, Write},
    os::windows::fs::OpenOptionsExt,
    path::Path,
    time::{SystemTime, UNIX_EPOCH},
};
use windows::Win32::Storage::FileSystem::FILE_FLAG_OPEN_REPARSE_POINT;

const DIRECTORY: &str = "workspace-file-attempts-v1";
const LOCK: &str = "store.lock";
const MAX_RECORDS: usize = 64;
const MAX_BYTES: usize = 1024;
type Result<T> = std::result::Result<T, String>;

#[derive(Serialize, Deserialize, PartialEq, Eq, Debug)]
#[serde(deny_unknown_fields)]
struct Record {
    version: u32,
    operation_id: String,
    review_id: String,
    request_sha256: String,
    registered_at_ms: u64,
}
fn now() -> Result<u64> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}
fn valid_id(id: &str) -> bool {
    uuid::Uuid::parse_str(id).is_ok_and(|parsed| parsed.to_string() == id)
}
impl Record {
    fn for_request(request: &CheckedRequest) -> Result<Self> {
        let at = now()?;
        Ok(Self {
            version: 1,
            operation_id: request.facts().operation_id.clone(),
            review_id: request.facts().review_id.clone(),
            request_sha256: sha256(&request.encode(at)?),
            registered_at_ms: at,
        })
    }
    fn decode(bytes: &[u8], id: &str) -> Result<Self> {
        if bytes.is_empty() || bytes.len() > MAX_BYTES {
            return Err("操作尝试记录为空或超限；需人工检查".into());
        }
        let record: Self =
            serde_json::from_slice(bytes).map_err(|_| "操作尝试记录损坏；不会自动清理或重试")?;
        if record.version != 1
            || !valid_id(&record.operation_id)
            || record.operation_id != id
            || !valid_id(&record.review_id)
            || record.registered_at_ms == 0
            || record.request_sha256.len() != 64
            || !record
                .request_sha256
                .bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        {
            return Err("操作尝试记录不符合约定；需人工检查".into());
        }
        Ok(record)
    }
}

/// All handles are private. Drop closes them but NEVER deletes the reservation.
/// The store lock deliberately serializes helpers until this observation ends.
pub(super) struct RegisteredAttempt {
    file: File,
    identity: ObjectIdentity,
    digest: String,
    request_sha256: String,
    app_dir: std::path::PathBuf,
    stages: super::execution_journal::ValidatedStages,
    prepared: Option<super::execution_journal::PreparedStage>,
    _prior: Vec<File>,
    _lock: File,
    _directory: PinnedDirectory,
}
impl RegisteredAttempt {
    pub(super) fn revalidate(&mut self, request: &CheckedRequest) -> Result<()> {
        self.revalidate_inner(request, true)
    }

    fn revalidate_inner(&mut self, request: &CheckedRequest, require_live: bool) -> Result<()> {
        let request_bytes = if require_live {
            request.encode(now()?)?
        } else {
            request.facts().validate_persisted()?;
            serde_json::to_vec(request.facts()).map_err(|_| "操作尝试请求编码失败")?
        };
        if sha256(&request_bytes) != self.request_sha256 {
            return Err("操作尝试不属于本次冻结请求".into());
        }
        let (bytes, identity, _) = read_regular(&mut self.file, MAX_BYTES)?;
        if identity != self.identity || sha256(&bytes) != self.digest {
            return Err("操作尝试记录发生变化；不能继续".into());
        }
        let (lock, _, _) = read_regular(&mut self._lock, 0)?;
        if !lock.is_empty() {
            return Err("操作尝试锁无效".into());
        }
        self.stages.revalidate()?;
        if let Some(prepared) = &mut self.prepared {
            if require_live {
                prepared.revalidate(request)?;
            } else {
                prepared.revalidate_after_start(request)?;
            }
        }
        if require_live {
            request.check_time(now()?)?;
        }
        Ok(())
    }

    pub(super) fn prepare(
        &mut self,
        request: &CheckedRequest,
        inspection: &super::security_inspection::SecurityInspection,
    ) -> Result<()> {
        if self.prepared.is_some() {
            return Err("本次操作已持久准备；不会重新冻结".into());
        }
        self.revalidate(request)?;
        let frozen = inspection.freeze(request)?;
        let prepared = super::execution_journal::prepare(&self.app_dir, request, &frozen)?;
        self.prepared = Some(prepared);
        self.revalidate(request)
    }

    pub(super) fn mark_started(&mut self, request: &CheckedRequest) -> Result<()> {
        self.revalidate(request)?;
        self.prepared
            .as_mut()
            .ok_or("操作尚未形成完整持久准备")?
            .mark_started(request)
    }

    pub(super) fn bind_candidate(
        &mut self,
        request: &CheckedRequest,
        evidence: &super::security_inspection::CandidateEvidence,
    ) -> Result<()> {
        self.revalidate(request)?;
        self.prepared
            .as_mut()
            .ok_or("操作尚未形成完整持久准备")?
            .bind_candidate(request, evidence)?;
        self.revalidate(request)
    }

    pub(super) fn bind_recovery(
        &mut self,
        request: &CheckedRequest,
        evidence: &super::recovery_record::RecoveryRecordEvidence,
    ) -> Result<()> {
        self.revalidate(request)?;
        self.prepared
            .as_mut()
            .ok_or("操作尚未形成完整持久准备")?
            .bind_recovery(request, evidence)?;
        self.revalidate(request)
    }

    pub(super) fn mark_succeeded(
        &mut self,
        request: &CheckedRequest,
        evidence: &super::execution_journal::ExecutionOutcomeEvidence,
    ) -> Result<()> {
        self.revalidate_inner(request, false)?;
        self.prepared
            .as_mut()
            .ok_or("操作尚未形成完整持久准备")?
            .mark_succeeded(request, evidence)
    }
}

fn lock_store(path: &Path) -> Result<File> {
    let mut file = match OpenOptions::new()
        .read(true)
        .write(true)
        .create_new(true)
        .share_mode(0)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
    {
        Ok(file) => {
            file.sync_all().map_err(|_| "操作尝试锁刷盘失败")?;
            file
        }
        Err(e) if e.kind() == ErrorKind::AlreadyExists => OpenOptions::new()
            .read(true)
            .share_mode(0)
            .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
            .open(path)
            .map_err(|_| "另一个辅助操作正在使用记录目录，或目录不可用")?,
        Err(_) => return Err("无法建立操作尝试锁".into()),
    };
    read_regular(&mut file, 0)?; // Reject links, reparse points and nonempty locks.
    Ok(file)
}

/// Only called with the fixed same-user app directory derived by RecoveryLocation,
/// after independent native review. No argv/JSON/env directory is accepted.
pub(super) fn register(app_dir: &Path, request: &CheckedRequest) -> Result<RegisteredAttempt> {
    super::privilege::require_unimpersonated_thread()?;
    let record = Record::for_request(request)?; // Expired requests create nothing.
    // Never recursively create profile/ancestors or touch recovery-v1/v2.
    let _app = PinnedDirectory::open(app_dir, false)?;
    let storage = app_dir.join(DIRECTORY);
    match fs::create_dir(&storage) {
        Ok(()) => (),
        Err(e) if e.kind() == ErrorKind::AlreadyExists => (),
        Err(_) => return Err("无法建立操作尝试目录".into()),
    }
    let directory = PinnedDirectory::open(&storage, false)?;
    let lock = lock_store(&storage.join(LOCK))?;
    let mut prior = Vec::new();
    let mut reviews = HashSet::new();
    let mut operations = HashSet::new();
    let mut identities = Vec::new();
    let entries = fs::read_dir(&storage).map_err(|_| "无法完整检查操作尝试目录")?;
    for entry in entries {
        let entry = entry.map_err(|_| "无法完整检查操作尝试目录")?;
        let name = entry.file_name();
        if name == LOCK {
            continue;
        }
        if prior.len() >= MAX_RECORDS {
            return Err("操作尝试记录达到容量，需人工管理".into());
        }
        let id = name
            .to_str()
            .and_then(|n| n.strip_suffix(".json"))
            .filter(|id| valid_id(id))
            .ok_or("操作尝试目录含未知条目，需人工检查")?;
        let mut file = journal_file(&entry.path(), false)?;
        let (bytes, _, _) = read_regular(&mut file, MAX_BYTES)?;
        let old = Record::decode(&bytes, id)?;
        if !operations.insert(old.operation_id.clone()) || !reviews.insert(old.review_id.clone()) {
            return Err("操作尝试记录身份重复，需人工检查".into());
        }
        identities.push(super::execution_journal::AttemptIdentity {
            operation_id: old.operation_id.clone(),
            review_id: old.review_id.clone(),
            request_sha256: old.request_sha256.clone(),
        });
        prior.push(file); // Keep checked records pinned until this attempt ends.
    }
    if prior.len() >= MAX_RECORDS {
        return Err("操作尝试记录达到容量，需人工管理".into());
    }
    let mut stages = super::execution_journal::validate_store(app_dir, &identities)?;
    if operations.contains(&record.operation_id) || reviews.contains(&record.review_id) {
        return Err(match stages.states.get(&record.operation_id) {
            Some(super::execution_journal::StageState::UnknownAfterStart) => {
                "本次执行已登记开始但没有终态；结果未知，必须人工检查".into()
            }
            Some(super::execution_journal::StageState::Succeeded) => {
                "本次执行已有完整成功回执；不会重复执行".into()
            }
            Some(super::execution_journal::StageState::Prepared) => {
                "本次操作已有完整持久准备；需重新检查，不会重试".into()
            }
            Some(super::execution_journal::StageState::Materialized) => {
                "本次操作已有实体化候选；需重新检查，不会重试".into()
            }
            Some(super::execution_journal::StageState::RecoveryIntentBound) => {
                "本次操作已有持久恢复意图；需重新检查，不会重试".into()
            }
            None => "本次操作或审查已登记尝试；结果需重新检查，不会重试".into(),
        });
    }
    stages.revalidate()?;
    request.check_time(now()?)?;
    let bytes = serde_json::to_vec(&record).map_err(|_| "操作尝试记录编码失败")?;
    if bytes.len() > MAX_BYTES {
        return Err("操作尝试记录超限".into());
    }
    let mut file = journal_file(&storage.join(format!("{}.json", record.operation_id)), true)?;
    // From this point EVERY failure leaves the entry (including empty/torn data).
    // Existing entries are never opened with data-write access or truncated.
    file.write_all(&bytes)
        .map_err(|_| "操作尝试登记失败；已保留记录，不能自动重试")?;
    file.sync_all()
        .map_err(|_| "操作尝试刷盘失败；已保留记录，不能自动重试")?;
    let (stored, identity, _) = read_regular(&mut file, MAX_BYTES)?;
    if stored != bytes {
        return Err("操作尝试回读失败；不能自动重试".into());
    }
    let mut result = RegisteredAttempt {
        file,
        identity,
        digest: sha256(&bytes),
        request_sha256: record.request_sha256,
        app_dir: app_dir.to_path_buf(),
        stages,
        prepared: None,
        _prior: prior,
        _lock: lock,
        _directory: directory,
    };
    result.revalidate(request)?;
    Ok(result)
}

#[cfg(test)]
#[path = "attempt_journal_tests.rs"]
mod tests;
