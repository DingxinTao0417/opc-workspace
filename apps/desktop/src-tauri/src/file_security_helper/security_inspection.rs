//! Complete security observation and the sole source for durable preparation,
//! still not an execution permit. No project handle or model attachment.
use super::security::{FullDescriptor, NewCandidate};
use crate::{
    file_operation_contract::{
        CheckedRequest, MAX_FILE_BYTES, ObjectIdentity, ObservedFile, Operation, sha256,
    },
    file_operation_disk::{DiskReview, ReplaceGuard},
    file_operation_metadata::Metadata,
    file_operation_read::{read_parked, read_regular, recorded_identity},
    file_operation_security::SecurityDescriptor,
    file_recovery_record::Intent,
};
use std::time::{SystemTime, UNIX_EPOCH};
use std::{fs::File, io::Write, path::Path};

struct CapturedFile {
    security: FullDescriptor,
    readable_security: Vec<u8>,
    metadata: Metadata,
}
pub(super) struct FrozenFileObservation {
    pub(super) security: Vec<u8>,
    pub(super) metadata: Vec<u8>,
}
pub(super) struct FrozenSecurityInspection {
    pub(super) request_sha256: String,
    pub(super) original: FrozenFileObservation,
    pub(super) candidate: Option<FrozenFileObservation>,
}
/// Only SecurityInspection can construct production evidence. It represents a
/// fully materialized sibling candidate, not approval to install it.
#[derive(PartialEq, Eq)]
pub(super) struct CandidateEvidence {
    identity: ObjectIdentity,
    modified: u64,
    content_sha256: String,
    metadata_sha256: String,
    readable_security_sha256: String,
    full_security_sha256: String,
    name: String,
}

/// Owns the exact new candidate plus the reviewed source/ancestor pins. Before
/// recovery binding, Drop deletes only the candidate by its handle. After the
/// explicit durable-retention transition, every failure keeps both versions.
pub(super) struct MaterializedCandidate {
    candidate: NewCandidate,
    guard: ReplaceGuard,
    evidence: CandidateEvidence,
}
impl MaterializedCandidate {
    pub(super) fn evidence(&self) -> &CandidateEvidence {
        &self.evidence
    }

    pub(super) fn revalidate(
        &mut self,
        inspection: &SecurityInspection,
        request: &CheckedRequest,
    ) -> Result<(), String> {
        self.guard.revalidate(request)?;
        inspection
            .original
            .security
            .verify_file(self.guard.original())?;
        let path = self.candidate.path().to_path_buf();
        let actual =
            inspection.verify_materialized_candidate(request, &path, self.candidate.file_mut())?;
        if actual != self.evidence {
            return Err("实体化候选与已登记证据不一致".into());
        }
        self.guard.revalidate(request)?;
        inspection
            .original
            .security
            .verify_file(self.guard.original())
            .map_err(str::to_owned)
    }

    pub(super) fn retain_for_recovery(&mut self) {
        self.candidate.retain_for_recovery();
    }

    pub(super) fn into_execution(
        mut self,
        inspection: &SecurityInspection,
        request: &CheckedRequest,
    ) -> Result<(NewCandidate, ReplaceGuard, CandidateEvidence), String> {
        self.revalidate(inspection, request)?;
        Ok((self.candidate, self.guard, self.evidence))
    }
}
impl CandidateEvidence {
    pub(super) fn identity(&self) -> &ObjectIdentity {
        &self.identity
    }
    pub(super) fn modified(&self) -> u64 {
        self.modified
    }
    pub(super) fn content_sha256(&self) -> &str {
        &self.content_sha256
    }
    pub(super) fn metadata_sha256(&self) -> &str {
        &self.metadata_sha256
    }
    pub(super) fn readable_security_sha256(&self) -> &str {
        &self.readable_security_sha256
    }
    pub(super) fn full_security_sha256(&self) -> &str {
        &self.full_security_sha256
    }
    pub(super) fn name(&self) -> &str {
        &self.name
    }
    #[cfg(test)]
    pub(super) fn fixture(
        request: &CheckedRequest,
        full_security_sha256: String,
        metadata_sha256: String,
    ) -> Result<Self, String> {
        let Operation::Replace {
            original,
            candidate,
        } = &request.facts().operation
        else {
            return Err("测试候选只支持新替换".into());
        };
        Ok(Self {
            identity: ObjectIdentity {
                volume: original.identity.volume,
                index: original.identity.index + 100,
            },
            modified: original.modified + 100,
            content_sha256: candidate.digest().into(),
            metadata_sha256,
            readable_security_sha256: original.readable_security_sha256.clone(),
            full_security_sha256,
            name: format!(".opc-file-{}.candidate", request.facts().operation_id),
        })
    }
}
pub(super) struct SecurityInspection {
    request_sha256: String,
    original: CapturedFile,
    candidate: Option<CapturedFile>,
}

/// Evidence derived only from an identity-bound, post-operation handle while
/// the audit privilege scope is active. It is deliberately not serializable or
/// constructible outside this module.
pub(super) struct VerifiedFileEvidence {
    identity: ObjectIdentity,
    content_sha256: String,
    metadata_sha256: String,
    readable_security_sha256: String,
    full_security_sha256: String,
}
impl VerifiedFileEvidence {
    pub(super) fn identity(&self) -> &ObjectIdentity {
        &self.identity
    }
    pub(super) fn content_sha256(&self) -> &str {
        &self.content_sha256
    }
    pub(super) fn metadata_sha256(&self) -> &str {
        &self.metadata_sha256
    }
    pub(super) fn readable_security_sha256(&self) -> &str {
        &self.readable_security_sha256
    }
    pub(super) fn full_security_sha256(&self) -> &str {
        &self.full_security_sha256
    }
}
fn now() -> Result<u64, String> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| "本机时钟不可用")?
        .as_millis()
        .try_into()
        .map_err(|_| "本机时钟超出范围".into())
}
impl SecurityInspection {
    /// Called inside the helper's synchronous AuditScope AFTER native review.
    /// Kernel handles never outlive the scope, even on failure or unwinding.
    pub(super) fn capture(request: &CheckedRequest) -> Result<Self, String> {
        let request_sha256 = sha256(&request.encode(now()?)?);
        let (original, candidate) = DiskReview::capture_security(
            request,
            |file| Self::capture_file(request, file),
            |file, expected| Self::verify_file(request, file, expected),
        )?;
        let result = Self {
            request_sha256,
            original,
            candidate,
        };
        result.check_request(request)?;
        Ok(result)
    }

    fn expected<'a>(
        request: &'a CheckedRequest,
        identity: &ObjectIdentity,
    ) -> Result<Vec<u8>, String> {
        let operation = &request.facts().operation;
        let (original, candidate) = match operation {
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
        if &original.identity == identity {
            original.decode_content().map_err(str::to_owned)
        } else if let Some(candidate) = candidate.filter(|value| &value.identity == identity) {
            candidate.decode_content().map_err(str::to_owned)
        } else {
            Err("完整权限观察遇到未绑定的文件身份".into())
        }
    }

    fn capture_file(request: &CheckedRequest, file: &File) -> Result<CapturedFile, String> {
        let content = Self::expected(request, &recorded_identity(file)?)?;
        let metadata = Metadata::capture(file, &content)?;
        let readable_security = SecurityDescriptor::capture(file)?.bytes().to_vec();
        let security = FullDescriptor::capture(file).map_err(str::to_owned)?;
        Ok(CapturedFile {
            security,
            readable_security,
            metadata,
        })
    }

    fn verify_file(
        request: &CheckedRequest,
        file: &File,
        expected: &CapturedFile,
    ) -> Result<(), String> {
        expected.security.verify_file(file).map_err(str::to_owned)?;
        let content = Self::expected(request, &recorded_identity(file)?)?;
        let metadata = Metadata::capture(file, &content)?;
        let readable_security = SecurityDescriptor::capture(file)?.bytes().to_vec();
        if metadata.review_fingerprint()? != expected.metadata.review_fingerprint()?
            || readable_security != expected.readable_security
        {
            return Err("完整权限观察期间文件元数据发生变化".into());
        }
        Ok(())
    }

    fn captured_for_identity(
        &self,
        request: &CheckedRequest,
        identity: &ObjectIdentity,
    ) -> Result<&CapturedFile, String> {
        match &request.facts().operation {
            Operation::Replace { original, .. } if &original.identity == identity => {
                Ok(&self.original)
            }
            Operation::RestoreMissing { original, .. }
            | Operation::UndoInstalled { original, .. }
                if &original.identity == identity =>
            {
                Ok(&self.original)
            }
            Operation::RestoreMissing { candidate, .. }
            | Operation::UndoInstalled { candidate, .. }
                if &candidate.identity == identity =>
            {
                self.candidate
                    .as_ref()
                    .ok_or("完整权限观察缺少候选对象".into())
            }
            _ => Err("完整权限复核遇到未绑定的文件身份".into()),
        }
    }

    /// Re-open only the frozen request's exact live objects inside a fresh
    /// AuditScope and compare complete security, readable security and ordinary
    /// metadata again. This remains observation only and must precede started.
    pub(super) fn revalidate_live(&self, request: &CheckedRequest) -> Result<(), String> {
        self.check_request(request)?;
        let (original, candidate) = DiskReview::capture_security(
            request,
            |file| {
                let identity = recorded_identity(file)?;
                Self::verify_file(
                    request,
                    file,
                    self.captured_for_identity(request, &identity)?,
                )?;
                Ok(identity)
            },
            |file, identity| {
                if recorded_identity(file)? != *identity {
                    return Err("完整权限复核期间文件身份变化".into());
                }
                Self::verify_file(
                    request,
                    file,
                    self.captured_for_identity(request, identity)?,
                )
            },
        )?;
        let expected_candidate = matches!(
            &request.facts().operation,
            Operation::RestoreMissing { .. } | Operation::UndoInstalled { .. }
        );
        if self.captured_for_identity(request, &original).is_err()
            || candidate.is_some() != expected_candidate
        {
            return Err("完整权限复核对象集合不一致".into());
        }
        self.check_request(request)
    }

    pub(super) fn freeze(
        &self,
        request: &CheckedRequest,
    ) -> Result<FrozenSecurityInspection, String> {
        self.check_request(request)?;
        let freeze = |file: &CapturedFile| -> Result<FrozenFileObservation, String> {
            Ok(FrozenFileObservation {
                security: file.security.freeze(),
                metadata: file.metadata.freeze()?,
            })
        };
        Ok(FrozenSecurityInspection {
            request_sha256: self.request_sha256.clone(),
            original: freeze(&self.original)?,
            candidate: self.candidate.as_ref().map(freeze).transpose()?,
        })
    }

    /// Called only inside a fresh AuditScope. It creates one deterministic
    /// sibling with CREATE_NEW, writes the complete candidate body, restores
    /// bounded ordinary metadata and verifies the complete captured descriptor.
    /// The returned owner keeps the reviewed source and ancestors pinned.
    pub(super) fn materialize_candidate(
        &mut self,
        request: &CheckedRequest,
    ) -> Result<MaterializedCandidate, String> {
        self.check_request(request)?;
        let Operation::Replace { candidate, .. } = &request.facts().operation else {
            return Err("恢复操作不创建新候选".into());
        };
        let bytes = candidate.decode()?;
        let mut guard = DiskReview::inspect_for_audit(request)?.into_replace_guard()?;
        guard.revalidate(request)?;
        self.original.security.verify_file(guard.original())?;
        let mut created = self
            .original
            .security
            .create_empty_candidate(guard.parent(), &request.facts().operation_id)?;
        created
            .file_mut()
            .write_all(&bytes)
            .map_err(|_| "候选正文写入失败；原文件未改变")?;
        created
            .file()
            .sync_all()
            .map_err(|_| "候选正文未能刷盘；原文件未改变")?;
        self.original.metadata.apply(created.file(), &bytes)?;
        self.original.security.verify_file(created.file())?;
        let path = created.path().to_path_buf();
        let evidence = self.verify_materialized_candidate(request, &path, created.file_mut())?;
        guard.revalidate(request)?;
        self.original.security.verify_file(guard.original())?;
        Ok(MaterializedCandidate {
            candidate: created,
            guard,
            evidence,
        })
    }

    /// Build the legacy-compatible v2 recovery intent from the exact reviewed
    /// source and a verified new candidate. Persistence remains the fixed-store
    /// recovery module's responsibility and must precede started.
    pub(super) fn replacement_intent(
        &self,
        request: &CheckedRequest,
        candidate_evidence: &CandidateEvidence,
    ) -> Result<Intent, String> {
        self.check_request(request)?;
        let Operation::Replace {
            original,
            candidate,
        } = &request.facts().operation
        else {
            return Err("恢复操作不创建新恢复意图".into());
        };
        if candidate_evidence.content_sha256() != candidate.digest()
            || candidate_evidence.metadata_sha256()
                != self.original.metadata.candidate_review_fingerprint()?
            || candidate_evidence.readable_security_sha256()
                != sha256(&self.original.readable_security)
        {
            return Err("候选证据不能形成恢复意图".into());
        }
        let intent = Intent {
            version: 2,
            id: request.facts().operation_id.clone(),
            root: request.facts().root.clone().into(),
            path: request.facts().path.clone(),
            root_identity: request.facts().root_identity.clone(),
            parent_identity: request.facts().parent_identity.clone(),
            source_identity: original.identity.clone(),
            candidate_identity: candidate_evidence.identity().clone(),
            before: original.decode_content()?,
            after: candidate.decode()?,
            metadata: self.original.metadata.clone(),
            security: self.original.readable_security.clone(),
        };
        intent.validate()?;
        Ok(intent)
    }

    /// Called only inside the helper's AuditScope after a new sibling candidate has
    /// been fully written and flushed. The target/source are not changed here.
    pub(super) fn verify_materialized_candidate(
        &self,
        request: &CheckedRequest,
        path: &Path,
        file: &mut File,
    ) -> Result<CandidateEvidence, String> {
        self.check_request(request)?;
        let Operation::Replace {
            original,
            candidate,
        } = &request.facts().operation
        else {
            return Err("只有新替换需要实体化候选".into());
        };
        let expected_name = format!(".opc-file-{}.candidate", request.facts().operation_id);
        if path.file_name().and_then(|v| v.to_str()) != Some(expected_name.as_str()) {
            return Err("候选文件名未绑定本次操作".into());
        }
        let expected = candidate.decode()?;
        let (bytes, identity, modified) = read_regular(file, MAX_FILE_BYTES)?;
        if bytes != expected || identity == original.identity {
            return Err("实体化候选正文或身份无效".into());
        }
        let metadata = Metadata::capture(file, &bytes)?;
        if !self.original.metadata.matches_candidate(&metadata) {
            return Err("实体化候选普通元数据未完整保留".into());
        }
        let readable = sha256(SecurityDescriptor::capture(file)?.bytes());
        if readable != original.readable_security_sha256 {
            return Err("实体化候选可读权限未完整保留".into());
        }
        self.original
            .security
            .verify_file(file)
            .map_err(str::to_owned)?;
        let second = read_regular(file, MAX_FILE_BYTES)?;
        if second.0 != bytes || second.1 != identity || second.2 != modified {
            return Err("实体化候选核验期间发生变化".into());
        }
        Ok(CandidateEvidence {
            identity,
            modified,
            content_sha256: candidate.digest().into(),
            metadata_sha256: metadata.review_fingerprint()?,
            readable_security_sha256: readable,
            full_security_sha256: sha256(&self.original.security.freeze()),
            name: expected_name,
        })
    }

    fn verify_outcome_file(
        &self,
        file: &mut File,
        expected: &ObservedFile,
        expected_full_security_sha256: &str,
        allow_aliases: bool,
    ) -> Result<VerifiedFileEvidence, String> {
        let read = if allow_aliases {
            read_parked
        } else {
            read_regular
        };
        if recorded_identity(file)? != expected.identity {
            return Err("执行后文件身份与冻结事实不一致".into());
        }
        let (bytes, identity, modified) = read(file, MAX_FILE_BYTES)?;
        if identity != expected.identity
            || modified != expected.modified
            || bytes != expected.decode_content()?
        {
            return Err("执行后文件正文或修改时间与冻结事实不一致".into());
        }
        let metadata_sha256 = Metadata::capture(file, &bytes)?.review_fingerprint()?;
        let readable_security_sha256 = sha256(SecurityDescriptor::capture(file)?.bytes());
        let full_security_sha256 = sha256(&FullDescriptor::capture(file)?.freeze());
        if metadata_sha256 != expected.metadata_sha256
            || readable_security_sha256 != expected.readable_security_sha256
            || full_security_sha256 != expected_full_security_sha256
        {
            return Err("执行后文件元数据或完整权限与冻结事实不一致".into());
        }
        let second = read(file, MAX_FILE_BYTES)?;
        if second.0 != bytes || second.1 != identity || second.2 != modified {
            return Err("执行后核验期间文件发生变化".into());
        }
        Ok(VerifiedFileEvidence {
            identity,
            content_sha256: sha256(&bytes),
            metadata_sha256,
            readable_security_sha256,
            full_security_sha256,
        })
    }

    pub(super) fn verify_original_outcome(
        &self,
        request: &CheckedRequest,
        file: &mut File,
        allow_aliases: bool,
    ) -> Result<VerifiedFileEvidence, String> {
        let original = match &request.facts().operation {
            Operation::Replace { original, .. }
            | Operation::RestoreMissing { original, .. }
            | Operation::UndoInstalled { original, .. } => original,
        };
        self.verify_outcome_file(
            file,
            original,
            &sha256(&self.original.security.freeze()),
            allow_aliases,
        )
    }

    pub(super) fn verify_recovery_candidate_outcome(
        &self,
        request: &CheckedRequest,
        file: &mut File,
    ) -> Result<VerifiedFileEvidence, String> {
        let candidate = match &request.facts().operation {
            Operation::RestoreMissing { candidate, .. }
            | Operation::UndoInstalled { candidate, .. } => candidate,
            Operation::Replace { .. } => return Err("新替换没有既有恢复候选".into()),
        };
        let captured = self.candidate.as_ref().ok_or("完整权限观察缺少恢复候选")?;
        self.verify_outcome_file(file, candidate, &sha256(&captured.security.freeze()), true)
    }

    pub(super) fn verify_new_candidate_outcome(
        &self,
        request: &CheckedRequest,
        evidence: &CandidateEvidence,
        file: &mut File,
    ) -> Result<VerifiedFileEvidence, String> {
        let Operation::Replace { candidate, .. } = &request.facts().operation else {
            return Err("恢复操作没有新建候选".into());
        };
        let (bytes, identity, modified) = read_regular(file, MAX_FILE_BYTES)?;
        if identity != *evidence.identity()
            || modified != evidence.modified()
            || bytes != candidate.decode()?
        {
            return Err("安装后的新候选身份、正文或修改时间不一致".into());
        }
        let metadata_sha256 = Metadata::capture(file, &bytes)?.review_fingerprint()?;
        let readable_security_sha256 = sha256(SecurityDescriptor::capture(file)?.bytes());
        let full_security_sha256 = sha256(&FullDescriptor::capture(file)?.freeze());
        if metadata_sha256 != evidence.metadata_sha256()
            || readable_security_sha256 != evidence.readable_security_sha256()
            || full_security_sha256 != evidence.full_security_sha256()
        {
            return Err("安装后的新候选元数据或完整权限不一致".into());
        }
        let second = read_regular(file, MAX_FILE_BYTES)?;
        if second.0 != bytes || second.1 != identity || second.2 != modified {
            return Err("安装后候选核验期间发生变化".into());
        }
        Ok(VerifiedFileEvidence {
            identity,
            content_sha256: sha256(&bytes),
            metadata_sha256,
            readable_security_sha256,
            full_security_sha256,
        })
    }

    pub(super) fn check_request(&self, request: &CheckedRequest) -> Result<(), String> {
        if sha256(&request.encode(now()?)?) != self.request_sha256 {
            return Err("完整权限观察与本次请求不一致".into());
        }
        Ok(())
    }
}
