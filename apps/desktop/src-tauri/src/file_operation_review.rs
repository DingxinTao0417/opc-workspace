//! Exact presentation and single-use handoff for the native consent UI.
//! An acknowledged presentation is NOT a trusted-launch or execution permit.
use crate::file_operation_contract::{CheckedRequest, Content, Operation, sha256};
use serde::Serialize;

#[cfg(windows)]
#[path = "file_operation_review/native.rs"]
pub(crate) mod native;
#[path = "file_operation_review/text.rs"]
mod text;

#[derive(Serialize)]
#[serde(rename_all = "snake_case")]
enum ObjectSlot {
    Missing,
    Original,
    Candidate,
}

/// Only produced from a checked request. UI adapters render these complete
/// objects and direction; they must not supply their own before/after text.
/// Original and candidate appear once each, including for maximum-sized files.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub(crate) struct ReviewDocument {
    version: u32,
    operation_id: String,
    review_id: String,
    root_selection_id: String,
    root: String,
    path: String,
    expires_at_ms: u64,
    request_sha256: String,
    operation: &'static str,
    from: ObjectSlot,
    to: ObjectSlot,
    original: Content,
    candidate: Content,
    retained: ObjectSlot,
}

/// Ownership prevents reusing an individual presentation after an attempted
/// decision. No Deserialize/Clone; request facts and document are immutable.
pub(crate) struct PendingReview {
    request: CheckedRequest,
    document: ReviewDocument,
    document_sha256: String,
}

pub(crate) enum Decision {
    Cancel,
    Continue,
}

/// Still unapproved for execution: the launch layer must establish trusted
/// source, real native user consent, disk/SACL/journal facts and IPC binding.
pub(crate) struct ReviewedIntent {
    request: CheckedRequest,
    document_sha256: String,
}
impl PendingReview {
    pub(crate) fn new(request: CheckedRequest, now_ms: u64) -> Result<Self, &'static str> {
        let request_sha256 = sha256(&request.encode(now_ms)?);
        let facts = request.facts();
        let (operation, from, to, original, candidate, retained) = match &facts.operation {
            Operation::Replace {
                original,
                candidate,
            } => (
                "replace",
                ObjectSlot::Original,
                ObjectSlot::Candidate,
                &original.content,
                candidate,
                ObjectSlot::Original,
            ),
            Operation::RestoreMissing {
                original,
                candidate,
                ..
            } => (
                "restore_missing",
                ObjectSlot::Missing,
                ObjectSlot::Original,
                &original.content,
                &candidate.content,
                ObjectSlot::Candidate,
            ),
            Operation::UndoInstalled {
                original,
                candidate,
                ..
            } => (
                "undo_installed",
                ObjectSlot::Candidate,
                ObjectSlot::Original,
                &original.content,
                &candidate.content,
                ObjectSlot::Candidate,
            ),
        };
        let document = ReviewDocument {
            version: 1,
            operation_id: facts.operation_id.clone(),
            review_id: facts.review_id.clone(),
            root_selection_id: facts.root_selection_id.clone(),
            root: facts.root.clone(),
            path: facts.path.clone(),
            expires_at_ms: facts.expires_at_ms,
            request_sha256,
            operation,
            from,
            to,
            original: Content::from_bytes(&original.decode()?)?,
            candidate: Content::from_bytes(&candidate.decode()?)?,
            retained,
        };
        let encoded = serde_json::to_vec(&document).map_err(|_| "完整审查文档编码失败")?;
        let document_sha256 = sha256(&encoded);
        request.check_time(now_ms)?;
        Ok(Self {
            request,
            document,
            document_sha256,
        })
    }
    pub(crate) fn document(&self, now_ms: u64) -> Result<(&ReviewDocument, &str), &'static str> {
        self.request.check_time(now_ms)?;
        Ok((&self.document, &self.document_sha256))
    }
    /// Called only by the trusted native UI adapter, never directly from model
    /// JSON/CLI. Consumes on cancel, mismatch and expiry as well as success.
    /// A matching digest proves binding, NOT that a human saw or approved it.
    pub(crate) fn decide(
        self,
        decision: Decision,
        displayed_sha256: &str,
        selected_root_id: &str,
        now_ms: u64,
    ) -> Result<Option<ReviewedIntent>, &'static str> {
        self.request.check_time(now_ms)?;
        if !matches!(decision, Decision::Continue) {
            return Ok(None);
        }
        if displayed_sha256 != self.document_sha256
            || selected_root_id != self.request.facts().root_selection_id
            || sha256(&self.request.encode(now_ms)?) != self.document.request_sha256
        {
            return Err("审查内容或目录选择与本次请求不一致");
        }
        Ok(Some(ReviewedIntent {
            request: self.request,
            document_sha256: self.document_sha256,
        }))
    }
}
impl ReviewedIntent {
    /// Moves the original checked request onward without re-decoding it or
    /// extending its monotonic deadline. It remains subject to every gate.
    pub(crate) fn into_unapproved_request(
        self,
        now_ms: u64,
    ) -> Result<CheckedRequest, &'static str> {
        self.request.check_time(now_ms)?;
        Ok(self.request)
    }
    pub(crate) fn document_sha256(&self) -> &str {
        &self.document_sha256
    }
}

#[cfg(test)]
#[path = "file_operation_review/tests.rs"]
mod tests;
