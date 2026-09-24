//! Production review capture for version-2 recovery records.
//! Reviews freeze local facts, never confer model authority. Only the product
//! command can consume one into the bound helper's native/UAC execution chain.
use super::edits::{FileReplacementPreview, FileReplacementReview, ReplacementFileState as Status};
#[cfg(test)]
use super::replacement::{rename_no_replace, source_file};
use super::replacement_journal::{Intent, load};
use super::replacement_metadata::Metadata;
use super::replacement_security::SecurityDescriptor;
use super::*;
use ::windows::Win32::Storage::FileSystem::FILE_FLAG_OPEN_REPARSE_POINT;
use base64::{Engine, engine::general_purpose::STANDARD};
use std::{
    fs::{File, OpenOptions},
    os::windows::fs::OpenOptionsExt,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum RecoveryMode {
    MissingTarget,
    UndoInstalled,
}

#[cfg_attr(not(test), allow(dead_code))]
struct FrozenFile {
    modified: u64,
    metadata: Metadata,
    security: Vec<u8>,
}
impl FrozenFile {
    fn capture(file: &mut File, identity: &FileIdentity, expected: &[u8]) -> Result<Self, String> {
        if windows::recorded_identity(file)? != *identity {
            return Err("恢复对象已被替换，未读取其他文件正文".into());
        }
        let (bytes, actual, modified) = windows::read_parked(file, MAX_BYTES)?;
        if actual != *identity || bytes != expected {
            return Err("恢复对象的正文已变化".into());
        }
        Ok(Self {
            modified,
            metadata: Metadata::capture(file, expected)?,
            security: SecurityDescriptor::capture(file)?.bytes().to_vec(),
        })
    }
    #[cfg_attr(not(test), allow(dead_code))] // Used by the pending internal request bridge.
    fn revalidate(
        &self,
        file: &mut File,
        identity: &FileIdentity,
        expected: &[u8],
    ) -> Result<(), String> {
        let current = Self::capture(file, identity, expected)?;
        if current.modified != self.modified
            || !self.metadata.matches_original(&current.metadata)
            || current.security != self.security
        {
            return Err("审查后文件身份、内容或元数据发生变化，不能沿用旧审查".into());
        }
        Ok(())
    }
}

struct HeldPair {
    original: File,
    candidate: File,
    _root: windows::PinnedDirectory,
    _parent: windows::PinnedDirectory,
}
fn check_absent(path: &Path) -> Result<(), String> {
    match OpenOptions::new()
        .read(true)
        .share_mode(1)
        .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
        .open(path)
    {
        Err(error) if error.raw_os_error() == Some(2) => Ok(()),
        _ => Err("恢复目标位置已存在或无法确认缺失；未覆盖任何文件".into()),
    }
}
impl HeldPair {
    fn open(intent: &Intent, mode: RecoveryMode, moving: bool) -> Result<Self, String> {
        let root = windows::PinnedDirectory::open(&intent.root, false)?;
        let target = intent.target();
        let parent = windows::PinnedDirectory::open(target.parent().ok_or("恢复路径无效")?, false)?;
        if root.identity != intent.root_identity || parent.identity != intent.parent_identity {
            return Err("恢复的根目录或父目录已替换".into());
        }
        let open = |path: &Path| {
            #[cfg(test)]
            if moving {
                return source_file(path);
            }
            #[cfg(not(test))]
            let _ = moving;
            {
                OpenOptions::new()
                    .read(true)
                    .share_mode(0)
                    .custom_flags(FILE_FLAG_OPEN_REPARSE_POINT.0)
                    .open(path)
                    .map_err(|_| "恢复对象不可读或被占用".to_string())
            }
        };
        let original = open(&intent.sibling("original"))?;
        let candidate_path = match mode {
            RecoveryMode::MissingTarget => {
                check_absent(&target)?;
                intent.sibling("candidate")
            }
            RecoveryMode::UndoInstalled => {
                check_absent(&intent.sibling("candidate"))?;
                target
            }
        };
        let candidate = open(&candidate_path)?;
        Ok(Self {
            original,
            candidate,
            _root: root,
            _parent: parent,
        })
    }
}

// Freeze only during explicit local review; not serializable, not cloneable,
// root selection and exact intent are bound, expires in ten minutes.
#[cfg_attr(not(test), allow(dead_code))]
pub(super) struct RecoveryReview {
    intent: Intent,
    storage: PathBuf,
    root_id: String,
    mode: RecoveryMode,
    original: FrozenFile,
    candidate: FrozenFile,
    created: Instant,
}

#[derive(Clone, Default)]
pub struct RecoveryReviewState(Arc<Mutex<HashMap<String, RecoveryReview>>>);
impl RecoveryReviewState {
    pub(super) fn capture(
        &self,
        storage: &Path,
        root_id: &str,
        root: &Path,
        id: &str,
        mode: &str,
    ) -> Result<FileReplacementReview, String> {
        let mode = match mode {
            "missing_target" => RecoveryMode::MissingTarget,
            "undo_installed" => RecoveryMode::UndoInstalled,
            _ => return Err("不支持此恢复审查类型".into()),
        };
        let mut values = self.0.lock().map_err(|_| "恢复审查状态不可用")?;
        values.retain(|_, review| review.created.elapsed() < TTL);
        if values.len() >= MAX_SNAPSHOTS {
            return Err("最多保留八项恢复审查，请先关闭旧审查".into());
        }
        let review = RecoveryReview::capture(storage, root_id, root, id, mode)?;
        let ticket = uuid::Uuid::new_v4().to_string();
        let missing = mode == RecoveryMode::MissingTarget;
        let intent = &review.intent;
        let response = FileReplacementReview {
            preview: FileReplacementPreview {
                id: intent.id.clone(),
                path: intent.path.clone(),
                observation: if missing {
                    "target_missing"
                } else {
                    "candidate_present"
                }
                .into(),
                target: if missing {
                    Status::Missing
                } else {
                    Status::Candidate
                },
                parked: Status::Original,
                staged: if missing {
                    Status::Candidate
                } else {
                    Status::Missing
                },
                current_base64: if missing {
                    None
                } else {
                    Some(STANDARD.encode(&intent.after))
                },
                original_base64: STANDARD.encode(&intent.before),
                candidate_base64: STANDARD.encode(&intent.after),
            },
            review_id: ticket.clone(),
            mode: if missing {
                "missing_target"
            } else {
                "undo_installed"
            }
            .into(),
            expires_in_seconds: TTL.as_secs(),
        };
        values.insert(ticket, review);
        Ok(response)
    }
    pub(super) fn release(&self, root_id: &str, id: &str) -> Result<(), String> {
        let mut values = self.0.lock().map_err(|_| "恢复审查状态不可用")?;
        values.retain(|_, review| review.created.elapsed() < TTL);
        if values
            .get(id)
            .is_some_and(|review| review.root_id != root_id)
        {
            return Err("恢复审查不属于此目录选择".into());
        }
        values.remove(id);
        Ok(())
    }
    #[cfg_attr(not(test), allow(dead_code))]
    fn consume(&self, root_id: &str, id: &str) -> Result<RecoveryReview, String> {
        let mut values = self.0.lock().map_err(|_| "恢复审查状态不可用")?;
        values.retain(|_, review| review.created.elapsed() < TTL);
        let review = values.remove(id).ok_or("恢复审查已过期或已使用")?;
        if review.root_id != root_id {
            return Err("恢复审查的目录选择不一致".into());
        }
        Ok(review)
    }

    // Consumed only by the trusted-main-WebView product command after a human
    // click. CLI/model callers cannot provide or reconstruct this ticket.
    #[cfg_attr(not(test), allow(dead_code))]
    pub(super) fn freeze_request(
        &self,
        root_id: &str,
        root: &Path,
        review_id: &str,
        operation_id: &str,
    ) -> Result<crate::file_operation_contract::CheckedRequest, String> {
        self.consume(root_id, review_id)?
            .into_request(root_id, root, review_id, operation_id)
    }
}
impl RecoveryReview {
    #[cfg_attr(not(test), allow(dead_code))]
    fn into_request(
        self,
        root_id: &str,
        root: &Path,
        review_id: &str,
        operation_id: &str,
    ) -> Result<crate::file_operation_contract::CheckedRequest, String> {
        use super::request_bridge::{identity, observed, root_text, times};
        use crate::file_operation_contract::{CheckedRequest, Operation, Request, sha256};
        if self.root_id != root_id || self.intent.root != root {
            return Err("恢复审查目录选择已变化".into());
        }
        times(self.created)?;
        let _storage = windows::PinnedDirectory::open(&self.storage, false)?;
        let _journal = windows::journal_file(
            &self.storage.join(format!("{}.json", self.intent.id)),
            false,
        )?;
        if load(&self.storage, &self.intent.id)? != self.intent {
            return Err("恢复记录在审查后变化".into());
        }
        let mut pair = HeldPair::open(&self.intent, self.mode, false)?;
        self.original.revalidate(
            &mut pair.original,
            &self.intent.source_identity,
            &self.intent.before,
        )?;
        self.candidate.revalidate(
            &mut pair.candidate,
            &self.intent.candidate_identity,
            &self.intent.after,
        )?;
        let original = observed(
            &self.intent.source_identity,
            self.original.modified,
            &self.intent.before,
            &self.original.metadata,
            &self.original.security,
        )?;
        let candidate = observed(
            &self.intent.candidate_identity,
            self.candidate.modified,
            &self.intent.after,
            &self.candidate.metadata,
            &self.candidate.security,
        )?;
        let record_sha256 =
            sha256(&serde_json::to_vec(&self.intent).map_err(|_| "恢复记录编码失败")?);
        let operation = match self.mode {
            RecoveryMode::MissingTarget => Operation::RestoreMissing {
                record_version: self.intent.version,
                record_id: self.intent.id,
                record_sha256,
                original,
                candidate,
            },
            RecoveryMode::UndoInstalled => Operation::UndoInstalled {
                record_version: self.intent.version,
                record_id: self.intent.id,
                record_sha256,
                original,
                candidate,
            },
        };
        let (issued_at_ms, expires_at_ms) = times(self.created)?;
        CheckedRequest::from_local(
            Request {
                version: 1,
                operation_id: operation_id.into(),
                review_id: review_id.into(),
                root_selection_id: root_id.into(),
                issued_at_ms,
                expires_at_ms,
                root: root_text(root)?,
                root_identity: identity(&self.intent.root_identity),
                parent_identity: identity(&self.intent.parent_identity),
                path: self.intent.path,
                operation,
            },
            issued_at_ms,
        )
        .map_err(str::to_owned)
    }
    pub(super) fn capture(
        storage: &Path,
        root_id: &str,
        root: &Path,
        id: &str,
        mode: RecoveryMode,
    ) -> Result<Self, String> {
        let intent = load(storage, id)?;
        if intent.root != root || root_id.is_empty() {
            return Err("恢复记录与所选目录不一致".into());
        }
        let mut pair = HeldPair::open(&intent, mode, false)?;
        let original =
            FrozenFile::capture(&mut pair.original, &intent.source_identity, &intent.before)?;
        let candidate = FrozenFile::capture(
            &mut pair.candidate,
            &intent.candidate_identity,
            &intent.after,
        )?;
        if !intent.metadata.matches_original(&original.metadata)
            || !intent.metadata.matches_candidate(&candidate.metadata)
            || original.security != intent.security
            || candidate.security != intent.security
        {
            return Err("记录对象的权限或附带元数据已变化，不能恢复".into());
        }
        Ok(Self {
            intent,
            storage: storage.into(),
            root_id: root_id.into(),
            mode,
            original,
            candidate,
            created: Instant::now(),
        })
    }

    // Consumes this review on any attempted operation, including failures.
    // Only a future explicit approval bridge may call this; never the model.
    #[cfg(test)]
    pub(super) fn prepare(self, root_id: &str, root: &Path) -> Result<ReadyRecovery, String> {
        if self.created.elapsed() >= TTL || self.root_id != root_id || self.intent.root != root {
            return Err("恢复审查已过期或目录选择已变化".into());
        }
        let storage = windows::PinnedDirectory::open(&self.storage, false)?;
        let journal = windows::journal_file(
            &self.storage.join(format!("{}.json", self.intent.id)),
            false,
        )?;
        // The held read handle denies writes/deletion while reloading and moving.
        if load(&self.storage, &self.intent.id)? != self.intent {
            return Err("恢复记录在审查后发生变化".into());
        }
        let mut pair = HeldPair::open(&self.intent, self.mode, true)?;
        self.original.revalidate(
            &mut pair.original,
            &self.intent.source_identity,
            &self.intent.before,
        )?;
        self.candidate.revalidate(
            &mut pair.candidate,
            &self.intent.candidate_identity,
            &self.intent.after,
        )?;
        Ok(ReadyRecovery {
            review: self,
            pair,
            _storage: storage,
            _journal: journal,
        })
    }
}

#[cfg(test)]
pub(super) struct ReadyRecovery {
    review: RecoveryReview,
    pair: HeldPair,
    _storage: windows::PinnedDirectory,
    _journal: File,
}
#[cfg(test)]
pub(super) struct VacantRecovery(ReadyRecovery);
#[cfg(test)]
impl ReadyRecovery {
    pub(super) fn park_current(mut self) -> Result<VacantRecovery, String> {
        if self.review.created.elapsed() >= TTL {
            return Err("恢复审查已过期，未移动文件".into());
        }
        let intent = &self.review.intent;
        self.review.original.revalidate(
            &mut self.pair.original,
            &intent.source_identity,
            &intent.before,
        )?;
        self.review.candidate.revalidate(
            &mut self.pair.candidate,
            &intent.candidate_identity,
            &intent.after,
        )?;
        if self.review.mode == RecoveryMode::UndoInstalled {
            // Reuse the now-vacant recorded staging name; both bytes/identities
            // remain recoverable from the existing durable intent after a crash.
            rename_no_replace(&self.pair.candidate, &intent.sibling("candidate"))?;
        }
        Ok(VacantRecovery(self))
    }
}
#[cfg(test)]
impl VacantRecovery {
    pub(super) fn restore(mut self) -> Result<(), String> {
        let ready = &mut self.0;
        let intent = &ready.review.intent;
        ready.review.original.revalidate(
            &mut ready.pair.original,
            &intent.source_identity,
            &intent.before,
        )?;
        ready.review.candidate.revalidate(
            &mut ready.pair.candidate,
            &intent.candidate_identity,
            &intent.after,
        )?;
        // A concurrent user file wins. Do not roll back automatically, delete
        // the winner, or mutate either retained object's bytes.
        rename_no_replace(&ready.pair.original, &intent.target())?;
        let verified = ready
            .review
            .original
            .revalidate(
                &mut ready.pair.original,
                &intent.source_identity,
                &intent.before,
            )
            .and_then(|_| {
                ready.review.candidate.revalidate(
                    &mut ready.pair.candidate,
                    &intent.candidate_identity,
                    &intent.after,
                )
            });
        verified.map_err(|_| {
            "原对象已移回，但完整核验失败；请检查恢复记录，不要自动重试".to_string()
        })?;
        Ok(())
    }
}

#[cfg(test)]
#[path = "replacement/recovery/tests.rs"]
mod tests;
