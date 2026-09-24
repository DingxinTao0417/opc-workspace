//! Build-file/OS-bound incoming request. The helper-only bootstrap can now close
//! one reviewed execution, but there is still no Tauri/WebView/model caller.
use super::{
    build_binding::InstalledPair,
    review_request::{ReceivedDiskReview, ReceivedRequest},
    transport::{
        bootstrap::UntrustedBootstrap,
        native::{Cancel, Endpoint, Peer, connect},
        wire::Binding,
    },
};
use crate::{
    file_operation_contract::{CheckedRequest, Operation},
    file_operation_disk::DiskReview,
};
use std::time::Duration;
type Result<T> = std::result::Result<T, String>;

pub(super) struct ParentSession {
    pair: InstalledPair,
    recovery: super::recovery_record::RecoveryLocation,
    binding: Binding,
    channel: Option<Endpoint>,
}
impl ParentSession {
    pub(super) fn prepare(bootstrap: UntrustedBootstrap) -> Result<Self> {
        Self::connect(bootstrap, InstalledPair::installed()?)
    }
    fn connect(bootstrap: UntrustedBootstrap, mut pair: InstalledPair) -> Result<Self> {
        let peer = Peer::pin(bootstrap.parent())?;
        peer.require_creation_time(bootstrap.created())?;
        pair.verify_parent(&peer)?;
        let recovery = super::recovery_record::RecoveryLocation::for_parent(&peer)?;
        let channel = connect(
            bootstrap.pipe(),
            peer,
            Duration::from_secs(30),
            Cancel::new()?,
        )?;
        pair.revalidate()?;
        Ok(Self {
            pair,
            recovery,
            binding: bootstrap.into_binding(),
            channel: Some(channel),
        })
    }
    pub(super) fn receive(&mut self) -> Result<ParentRequest<'_>> {
        // Consume even on file revalidation/decoding failure. No reconnect path.
        let channel = self.channel.take().ok_or("本次辅助请求已消费")?;
        self.pair.revalidate()?;
        let received = ReceivedRequest::receive(channel, &self.binding)?;
        self.pair.revalidate()?;
        Ok(ParentRequest {
            pair: &mut self.pair,
            recovery: &self.recovery,
            received,
        })
    }

    pub(super) fn run_once(mut self) -> Result<()> {
        let Some(reviewed) = self.receive()?.native_review()? else {
            return Ok(());
        };
        reviewed.execute_once()
    }
}
pub(super) struct ParentRequest<'a> {
    pair: &'a mut InstalledPair,
    recovery: &'a super::recovery_record::RecoveryLocation,
    received: ReceivedRequest<'a>,
}
/// A native presentation was accepted, NOT by itself a permission to perform
/// file I/O. Recovery binding and the durable started transition remain gated.
pub(super) struct ReviewedParentRequest<'a>(ParentRequest<'a>);
/// Retains attempt consumption through the observation's lifetime, not a permit.
pub(super) struct RegisteredSecurityInspection {
    security: super::security_inspection::SecurityInspection,
    attempt: super::attempt_journal::RegisteredAttempt,
    recovery: super::recovery_record::RecoveryLocation,
    record: Option<super::recovery_record::HeldRecoveryRecord>,
    candidate: Option<super::security_inspection::MaterializedCandidate>,
}
impl RegisteredSecurityInspection {
    pub(super) fn check_request(&mut self, request: &CheckedRequest) -> Result<()> {
        self.attempt.revalidate(request)?;
        self.security.check_request(request)?;
        if let Some(candidate) = &mut self.candidate {
            candidate.revalidate(&self.security, request)?;
        }
        if let Some(record) = &mut self.record {
            record.revalidate(request)?;
        }
        Ok(())
    }
    /// Internal only: CREATE_NEW on the deterministic sibling, complete
    /// metadata/security readback, then durable materialized binding. Any
    /// failure drops only the owned candidate handle; there is no install.
    pub(super) fn materialize_candidate(&mut self, request: &CheckedRequest) -> Result<()> {
        if self.candidate.is_some() {
            return Err("本次操作已有实体化候选；不会重建".into());
        }
        self.check_request(request)?;
        let mut candidate = super::privilege::with_audit_privilege(|| {
            self.security.materialize_candidate(request)
        })?;
        candidate.revalidate(&self.security, request)?;
        self.attempt.bind_candidate(request, candidate.evidence())?;
        let intent = self
            .security
            .replacement_intent(request, candidate.evidence())?;
        let mut record = self.recovery.create_replace(request, &intent)?;
        record.revalidate(request)?;
        self.attempt.bind_recovery(request, record.evidence())?;
        candidate.revalidate(&self.security, request)?;
        record.revalidate(request)?;
        self.record = Some(record);
        self.candidate = Some(candidate);
        self.check_request(request)
    }
    /// Internal transition only. Once this succeeds, an absent outcome must be
    /// treated as unknown and the candidate remains recoverable on every error.
    pub(super) fn mark_started(&mut self, request: &CheckedRequest) -> Result<()> {
        self.check_request(request)?;
        if self.record.is_none() {
            return Err("恢复意图尚未固定并持有；不能开始执行".into());
        }
        match &request.facts().operation {
            Operation::Replace { .. } if self.candidate.is_none() => {
                return Err("新替换候选尚未实体化；不能开始执行".into());
            }
            Operation::RestoreMissing { .. } | Operation::UndoInstalled { .. } => {
                let mut disk = DiskReview::inspect(request)?;
                disk.revalidate()?;
            }
            _ => {}
        }
        super::privilege::with_audit_privilege(|| self.security.revalidate_live(request))?;
        if let Some(candidate) = &mut self.candidate {
            // Recovery and stage binding are already durable. From here onward
            // a normal error or unwind must never discard one side of the pair.
            candidate.retain_for_recovery();
        }
        self.attempt.mark_started(request)?;
        self.check_request(request)
            .map_err(|_| "执行已经登记开始但会话复核失败；结果必须视为未知且不得重试".into())
    }

    /// Complete one already-reviewed attempt. It remains unreachable from
    /// arbitrary argv/Tauri input: the bound per-use elevation launcher owns
    /// the trusted transport and call chain before this can affect a user file.
    pub(super) fn execute(mut self, request: &CheckedRequest) -> Result<()> {
        self.mark_started(request)?;
        let outcome = match &request.facts().operation {
            Operation::Replace { .. } => {
                let candidate = self
                    .candidate
                    .take()
                    .ok_or("执行已经登记开始但候选所有权缺失；结果必须视为未知且不得重试")?;
                super::privilege::with_audit_privilege(|| {
                    super::execution::replace(&self.security, request, candidate)
                })
            }
            Operation::RestoreMissing { .. } | Operation::UndoInstalled { .. } => {
                super::privilege::with_audit_privilege(|| {
                    super::execution::recover(&self.security, request)
                })
            }
        }
        .map_err(|error| {
            format!("执行已经登记开始但未形成成功回执；结果必须视为未知且不得重试：{error}")
        })?;
        self.attempt
            .mark_succeeded(request, &outcome)
            .map_err(|error| {
                format!("文件终态已核验但成功回执不可用；结果必须视为未知且不得重试：{error}")
            })
    }
}
impl<'a> ReviewedParentRequest<'a> {
    /// Close the helper-owned chain after the independent native review. No
    /// caller can supply an `approved` bit: every disk/security stage is rebuilt
    /// from the retained request and success follows the durable outcome only.
    fn execute_once(mut self) -> Result<()> {
        let operation_id = self.unapproved()?.facts().operation_id.clone();
        let replace = matches!(
            self.unapproved()?.facts().operation,
            Operation::Replace { .. }
        );
        let mut inspection = self.capture_security()?;
        if replace {
            inspection.materialize_candidate(self.unapproved()?)?;
        }
        inspection.execute(self.unapproved()?)?;
        let receipt = crate::file_operation_contract::OperationReceipt::succeeded(&operation_id)?;
        self.reply(&receipt)
    }

    pub(super) fn capture_security(&mut self) -> Result<RegisteredSecurityInspection> {
        self.0.pair.revalidate()?;
        self.0.received.unapproved()?;
        let mut record = self.0.recovery.inspect(self.0.received.unapproved()?)?;
        self.0.pair.revalidate()?;
        let mut attempt = self
            .0
            .recovery
            .register_attempt(self.0.received.unapproved()?)?;
        // Record I/O and hashing may take time. Recheck all live prerequisites
        // before entering the audit scope, not only after privileges are entered.
        self.0.pair.revalidate()?;
        if let Some(record) = record.as_mut() {
            record.revalidate(self.0.received.unapproved()?)?;
        }
        self.0.received.unapproved()?;
        attempt.revalidate(self.0.received.unapproved()?)?;
        let captured = super::privilege::with_audit_privilege(|| {
            let request = self.0.received.unapproved()?;
            let captured = super::security_inspection::SecurityInspection::capture(request)?;
            self.0.received.unapproved()?;
            Ok(captured)
        })?;
        if let Some(record) = record.as_mut() {
            record.revalidate(self.0.received.unapproved()?)?;
        }
        // The thread token and every audit-access file handle have already
        // been released here. A stale observation is never an execution permit.
        self.0.pair.revalidate()?;
        attempt.prepare(self.0.received.unapproved()?, &captured)?;
        if let Some(record) = record.as_ref() {
            attempt.bind_recovery(self.0.received.unapproved()?, record.evidence())?;
        }
        self.0.pair.revalidate()?;
        let mut result = RegisteredSecurityInspection {
            security: captured,
            attempt,
            recovery: self.0.recovery.clone(),
            record,
            candidate: None,
        };
        result.check_request(self.0.received.unapproved()?)?;
        Ok(result)
    }
    pub(super) fn unapproved(&mut self) -> Result<&CheckedRequest> {
        self.0.unapproved()
    }
    pub(super) fn review_disk(&mut self) -> Result<ParentDiskReview<'_, 'a>> {
        self.0.review_disk()
    }
    pub(super) fn reply(self, bytes: &[u8]) -> Result<()> {
        self.0.reply(bytes)
    }
}
pub(super) struct ParentDiskReview<'r, 's> {
    pair: &'r mut InstalledPair,
    disk: ReceivedDiskReview<'r, 's>,
}
impl ParentDiskReview<'_, '_> {
    pub(super) fn revalidate(&mut self) -> Result<()> {
        self.pair.revalidate()?;
        self.disk.revalidate()?;
        self.pair.revalidate()
    }
}
impl<'a> ParentRequest<'a> {
    pub(super) fn native_review(self) -> Result<Option<ReviewedParentRequest<'a>>> {
        // Retain all installation pins while the window owns only the request
        // and its bounded, read-only connection observer. Hash before/after,
        // never scan images in the native window's short timer callback.
        self.pair.revalidate()?;
        let received = self.received.native_review()?;
        self.pair.revalidate()?;
        Ok(received.map(|received| {
            ReviewedParentRequest(Self {
                pair: self.pair,
                recovery: self.recovery,
                received,
            })
        }))
    }
    pub(super) fn unapproved(&mut self) -> Result<&CheckedRequest> {
        self.pair.revalidate()?;
        Ok(self.received.unapproved()?)
    }
    pub(super) fn review_disk(&mut self) -> Result<ParentDiskReview<'_, 'a>> {
        self.pair.revalidate()?;
        let disk = self.received.review_disk()?;
        self.pair.revalidate()?;
        Ok(ParentDiskReview {
            pair: &mut *self.pair,
            disk,
        })
    }
    pub(super) fn reply(self, bytes: &[u8]) -> Result<()> {
        self.pair.revalidate()?;
        Ok(self.received.reply(bytes)?)
    }
}

#[cfg(test)]
#[path = "parent_session_tests.rs"]
mod tests;
