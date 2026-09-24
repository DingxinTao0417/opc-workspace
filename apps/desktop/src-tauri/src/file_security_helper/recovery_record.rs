//! Read-only, same-user fixed-store recovery inspection. No caller-supplied
//! store path, creation, deletion, ACL application or authenticated history.
use super::transport::native::Peer;
use crate::{
    file_operation_contract::{CheckedRequest, ObjectIdentity, Operation, sha256},
    file_operation_read::{PinnedDirectory, journal_file, read_regular},
    file_recovery_record::{self as record, MAX_RECORD_BYTES},
};
use serde::Serialize;
use std::{
    fs::{self, File},
    io::{ErrorKind, Write},
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
use windows::{
    Win32::{
        Foundation::HANDLE,
        System::Com::CoTaskMemFree,
        UI::Shell::{FOLDERID_LocalAppData, KF_FLAG_DONT_VERIFY},
    },
    core::{GUID, HRESULT, PWSTR},
};
const APP_ID: &str = "com.opcworkspace.desktop";
const DIRECTORY: &str = "workspace-file-recovery-v2";

// Retain the output pointer even on HRESULT failure so the allocator contract
// can be honored. The generated Result<PWSTR> wrapper discards failed outputs.
#[link(name = "shell32")]
unsafe extern "system" {
    fn SHGetKnownFolderPath(
        id: *const GUID,
        flags: u32,
        token: HANDLE,
        path: *mut PWSTR,
    ) -> HRESULT;
}
struct ShellPath(PWSTR);
impl Drop for ShellPath {
    fn drop(&mut self) {
        unsafe { CoTaskMemFree(Some(self.0.0.cast())) }
    }
}
fn current_store() -> Result<PathBuf, String> {
    let mut result = ShellPath(PWSTR::null());
    unsafe {
        SHGetKnownFolderPath(
            &FOLDERID_LocalAppData,
            KF_FLAG_DONT_VERIFY.0 as u32,
            HANDLE::default(),
            &mut result.0,
        )
    }
    .ok()
    .map_err(|_| "无法定位本机应用恢复目录")?;
    if result.0.is_null() {
        return Err("本机应用恢复目录不可用".into());
    }
    let root = unsafe { result.0.to_string() }.map_err(|_| "恢复目录路径编码无效")?;
    if root.len() > 32766 || !PathBuf::from(&root).is_absolute() {
        return Err("本机恢复目录路径无效".into());
    }
    Ok(PathBuf::from(root).join(APP_ID).join(DIRECTORY))
}
fn now() -> Result<u64, String> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}
#[derive(Clone)]
pub(super) struct RecoveryLocation(PathBuf);

pub(super) struct RecoveryRecordEvidence {
    record_version: u32,
    record_id: String,
    record_sha256: String,
    stored_sha256: String,
    identity: ObjectIdentity,
    modified: u64,
    source_identity: ObjectIdentity,
    candidate_identity: ObjectIdentity,
}
impl RecoveryRecordEvidence {
    pub(super) fn record_version(&self) -> u32 {
        self.record_version
    }
    pub(super) fn record_id(&self) -> &str {
        &self.record_id
    }
    pub(super) fn record_sha256(&self) -> &str {
        &self.record_sha256
    }
    pub(super) fn stored_sha256(&self) -> &str {
        &self.stored_sha256
    }
    pub(super) fn identity(&self) -> &ObjectIdentity {
        &self.identity
    }
    pub(super) fn modified(&self) -> u64 {
        self.modified
    }
    pub(super) fn source_identity(&self) -> &ObjectIdentity {
        &self.source_identity
    }
    pub(super) fn candidate_identity(&self) -> &ObjectIdentity {
        &self.candidate_identity
    }
}

impl RecoveryLocation {
    pub(super) fn register_attempt(
        &self,
        request: &CheckedRequest,
    ) -> Result<super::attempt_journal::RegisteredAttempt, String> {
        super::attempt_journal::register(self.0.parent().ok_or("固定应用目录无效")?, request)
    }
    pub(super) fn for_parent(parent: &Peer) -> Result<Self, String> {
        super::privilege::require_unimpersonated_thread()?;
        parent.require_same_user()?;
        let path = current_store()?;
        super::privilege::require_unimpersonated_thread()?;
        parent.require_same_user()?;
        Ok(Self(path))
    }
    pub(super) fn inspect(
        &self,
        request: &CheckedRequest,
    ) -> Result<Option<HeldRecoveryRecord>, String> {
        request.check_time(now()?)?;
        let id = match &request.facts().operation {
            Operation::Replace { .. } => return Ok(None),
            Operation::RestoreMissing { record_id, .. }
            | Operation::UndoInstalled { record_id, .. } => record_id,
        };
        let directory = PinnedDirectory::open(&self.0, false)?;
        let mut file = journal_file(&self.0.join(format!("{id}.json")), false)?;
        let (bytes, identity, modified) = read_regular(&mut file, MAX_RECORD_BYTES)?;
        let intent = record::decode(&bytes, id)?;
        intent.matches_request(request, now()?)?;
        let record_id = intent.id.clone();
        let record_sha256 = intent.digest()?;
        let mut held = HeldRecoveryRecord {
            file,
            request_sha256: sha256(&request.encode(now()?)?),
            evidence: RecoveryRecordEvidence {
                record_version: intent.version,
                record_id,
                record_sha256,
                stored_sha256: sha256(&bytes),
                identity,
                modified,
                source_identity: intent.source_identity,
                candidate_identity: intent.candidate_identity,
            },
            _directory: directory,
        };
        held.revalidate(request)?;
        Ok(Some(held))
    }

    /// Persist the already materialized replacement intent before any started
    /// marker or project-file movement. The cooperative attempt lock is held by
    /// the only internal caller; CREATE_NEW remains the final collision guard.
    pub(super) fn create_replace(
        &self,
        request: &CheckedRequest,
        intent: &record::Intent,
    ) -> Result<HeldRecoveryRecord, String> {
        request.check_time(now()?)?;
        validate_replace_intent(request, intent)?;
        super::privilege::require_unimpersonated_thread()?;
        let app = self.0.parent().ok_or("固定应用目录无效")?;
        let _app = PinnedDirectory::open(app, false)?;
        match fs::create_dir(&self.0) {
            Ok(()) => (),
            Err(error) if error.kind() == ErrorKind::AlreadyExists => (),
            Err(_) => return Err("无法建立固定恢复记录目录".into()),
        }
        let directory = PinnedDirectory::open(&self.0, false)?;
        validate_capacity(&self.0)?;
        #[derive(Serialize)]
        struct Stored<'a> {
            intent: &'a record::Intent,
            digest: String,
        }
        let stored = Stored {
            intent,
            digest: intent.digest()?,
        };
        let bytes = serde_json::to_vec(&stored).map_err(|_| "替换恢复意图编码失败")?;
        if bytes.is_empty() || bytes.len() > MAX_RECORD_BYTES {
            return Err("替换恢复意图超过完整保存上限".into());
        }
        let mut file = journal_file(&self.0.join(format!("{}.json", intent.id)), true)?;
        // From CREATE_NEW onward, failures retain the torn record and consume
        // this attempt. They are never treated as proof that no write occurred.
        file.write_all(&bytes)
            .and_then(|_| file.sync_all())
            .map_err(|_| "恢复意图未能完整刷盘；不会开始执行")?;
        let (readback, identity, modified) = read_regular(&mut file, MAX_RECORD_BYTES)?;
        if readback != bytes {
            return Err("恢复意图回读不一致；不会开始执行".into());
        }
        let decoded = record::decode(&readback, &intent.id)?;
        validate_replace_intent(request, &decoded)?;
        let mut held = HeldRecoveryRecord {
            file,
            request_sha256: sha256(&request.encode(now()?)?),
            evidence: RecoveryRecordEvidence {
                record_version: intent.version,
                record_id: intent.id.clone(),
                record_sha256: intent.digest()?,
                stored_sha256: sha256(&readback),
                identity,
                modified,
                source_identity: intent.source_identity.clone(),
                candidate_identity: intent.candidate_identity.clone(),
            },
            _directory: directory,
        };
        held.revalidate(request)?;
        Ok(held)
    }
}

#[cfg(test)]
impl RecoveryRecordEvidence {
    pub(super) fn fixture(request: &CheckedRequest) -> Self {
        let facts = request.facts();
        let (record_id, record_sha256, source_identity, candidate_identity) = match &facts.operation
        {
            Operation::Replace {
                original,
                candidate: _,
            } => (
                facts.operation_id.clone(),
                sha256(b"fixture recovery intent"),
                original.identity.clone(),
                ObjectIdentity {
                    volume: original.identity.volume,
                    index: original.identity.index.saturating_add(100),
                },
            ),
            Operation::RestoreMissing {
                record_id,
                record_sha256,
                original,
                candidate,
                ..
            }
            | Operation::UndoInstalled {
                record_id,
                record_sha256,
                original,
                candidate,
                ..
            } => (
                record_id.clone(),
                record_sha256.clone(),
                original.identity.clone(),
                candidate.identity.clone(),
            ),
        };
        Self {
            record_version: 2,
            record_id,
            record_sha256,
            stored_sha256: sha256(b"fixture stored recovery intent"),
            identity: ObjectIdentity {
                volume: facts.root_identity.volume,
                index: facts.root_identity.index.saturating_add(10_000),
            },
            modified: 1,
            source_identity,
            candidate_identity,
        }
    }
}

fn validate_capacity(storage: &Path) -> Result<(), String> {
    let mut count = 0;
    for entry in fs::read_dir(storage).map_err(|_| "无法完整检查恢复记录目录")? {
        let entry = entry.map_err(|_| "无法完整检查恢复记录目录")?;
        count += 1;
        if count >= 64 {
            return Err("恢复记录已达 64 项，需先人工备份整理".into());
        }
        let name = entry.file_name();
        let id = name
            .to_str()
            .and_then(|value| value.strip_suffix(".json"))
            .filter(|value| record::valid_id(value))
            .ok_or("恢复记录目录含未知条目，不会新建意图")?;
        let mut file = journal_file(&entry.path(), false)?;
        let (bytes, _, _) = read_regular(&mut file, MAX_RECORD_BYTES)?;
        record::decode(&bytes, id)?;
    }
    Ok(())
}

fn validate_replace_intent(
    request: &CheckedRequest,
    intent: &record::Intent,
) -> Result<(), String> {
    request.check_time(now()?)?;
    intent.validate()?;
    let facts = request.facts();
    let Operation::Replace {
        original,
        candidate,
    } = &facts.operation
    else {
        return Err("只有新替换可创建新恢复意图".into());
    };
    if intent.id != facts.operation_id
        || intent.root != Path::new(&facts.root)
        || intent.path != facts.path
        || intent.root_identity != facts.root_identity
        || intent.parent_identity != facts.parent_identity
        || intent.source_identity != original.identity
        || intent.before != original.decode_content()?
        || intent.after != candidate.decode()?
        || intent.metadata.review_fingerprint()? != original.metadata_sha256
        || sha256(&intent.security) != original.readable_security_sha256
    {
        return Err("新替换恢复意图与冻结请求不一致".into());
    }
    Ok(())
}

pub(super) struct HeldRecoveryRecord {
    file: File,
    request_sha256: String,
    evidence: RecoveryRecordEvidence,
    _directory: PinnedDirectory,
}
impl HeldRecoveryRecord {
    pub(super) fn evidence(&self) -> &RecoveryRecordEvidence {
        &self.evidence
    }
    pub(super) fn revalidate(&mut self, request: &CheckedRequest) -> Result<(), String> {
        request.check_time(now()?)?;
        if sha256(&request.encode(now()?)?) != self.request_sha256 {
            return Err("恢复记录不属于本次冻结请求".into());
        }
        let (bytes, identity, modified) = read_regular(&mut self.file, MAX_RECORD_BYTES)?;
        if identity != self.evidence.identity
            || modified != self.evidence.modified
            || sha256(&bytes) != self.evidence.stored_sha256
        {
            return Err("恢复记录在本次检查期间变化".into());
        }
        let intent = record::decode(&bytes, &self.evidence.record_id)?;
        if intent.version != self.evidence.record_version
            || intent.digest()? != self.evidence.record_sha256
        {
            return Err("恢复记录身份或摘要变化".into());
        }
        match &request.facts().operation {
            Operation::Replace { .. } => validate_replace_intent(request, &intent)?,
            Operation::RestoreMissing { .. } | Operation::UndoInstalled { .. } => {
                intent.matches_request(request, now()?)?
            }
        }
        request.check_time(now()?)?;
        Ok(())
    }
}

#[cfg(test)]
#[path = "recovery_record_tests.rs"]
mod tests;
