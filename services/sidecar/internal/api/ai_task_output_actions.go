package api

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func isAITaskOutputAction(action string) bool {
	return action == "task.submit_output" || action == "task.review"
}

type aiOutputArtifactInput struct {
	ClientRef        string          `json:"client_ref"`
	StorageKind      string          `json:"storage_kind"`
	Name             string          `json:"name"`
	ContentText      *string         `json:"content_text,omitempty"`
	ReferenceURL     *string         `json:"reference_url,omitempty"`
	StructuredJSON   json.RawMessage `json:"structured_json,omitempty"`
	RequiresFollowup bool            `json:"requires_followup"`
}
type aiSubmitOutputChanges struct {
	Summary   string                  `json:"summary"`
	Artifacts []aiOutputArtifactInput `json:"artifacts"`
}
type aiReviewOutputChanges struct {
	SubmissionID string `json:"submission_id"`
	Decision     string `json:"decision"`
	Reason       string `json:"reason"`
}

// Local human preview only. Never return these bodies in the model tool result
// or in a later conversation's approval receipts. File paths are never read.
type aiOutputArtifactPreview struct {
	ID               *string `json:"id"`
	StorageKind      string  `json:"storage_kind"`
	Name             string  `json:"name"`
	Content          *string `json:"content"`
	SizeBytes        *int64  `json:"size_bytes"`
	SHA256           *string `json:"sha256"`
	RequiresFollowup bool    `json:"requires_followup"`
	Deleted          bool    `json:"deleted"`
}
type aiTaskOutputPreview struct {
	Summary     string                    `json:"summary"`
	Artifacts   []aiOutputArtifactPreview `json:"artifacts"`
	Assignments []aiAssignmentRow         `json:"assignments"`
}

func addAITaskOutputSchema(p map[string]any) {
	a := p["action"].(map[string]any)
	a["enum"] = append(a["enum"].([]any), "task.submit_output", "task.review")
	c := p["changes"].(map[string]any)
	c["description"] = c["description"].(string) + " Task submit_output/review require work+outputs+actions and task_id/expected_version. submit_output requires summary and artifacts (0-20); at least one must be nonempty. Artifacts: unique client_ref, storage_kind text/link/structured, name, requires_followup boolean, exactly matching content_text/reference_url/structured_json. No files/paths or producer override. Human confirms content and current assignee attribution; pending_review is not completion. review requires actual current submission_id, decision accept/request_changes, reason (empty allowed only for accept, max1000). Read workspace_task_submissions first. Owner must personally review evidence and confirm; metadata is not file inspection. Acceptance ends assignments, completes Task and reconciles Inbox/parents; request_changes retains assignments. Never invent consent or claim execution before receipt."
	f := c["properties"].(map[string]any)
	f["submission_id"] = map[string]any{"type": "string", "format": "uuid"}
	f["decision"] = map[string]any{"type": "string", "enum": []string{"accept", "request_changes"}}
	var artifacts any
	_ = json.Unmarshal([]byte(`{"type":"array","maxItems":20,"items":{"type":"object","additionalProperties":false,"required":["client_ref","storage_kind","name","requires_followup"],"properties":{"client_ref":{"type":"string","minLength":1,"maxLength":100},"storage_kind":{"type":"string","enum":["text","link","structured"]},"name":{"type":"string","minLength":1,"maxLength":255},"requires_followup":{"type":"boolean"},"content_text":{"type":"string"},"reference_url":{"type":"string"},"structured_json":{"type":"object"}}}}`), &artifacts)
	f["artifacts"] = artifacts
}

func parseAITaskOutputAction(in aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	for k := range raw {
		if k != "action" && k != "task_id" && k != "expected_version" && k != "changes" {
			return in, errors.New("unsupported task output action field: " + k)
		}
	}
	if id, e := uuid.Parse(in.TaskID); e != nil || id.String() != in.TaskID || in.ExpectedVersion < 1 {
		return in, errors.New("use the exact task_id and current expected_version")
	}
	keys := []string{"summary", "artifacts"}
	if in.Action == "task.review" {
		keys = []string{"submission_id", "decision", "reason"}
	}
	if len(fields) != len(keys) {
		return in, errors.New("task output changes require all documented fields")
	}
	for _, k := range keys {
		if len(fields[k]) == 0 || string(fields[k]) == "null" {
			return in, errors.New("missing or null task output field: " + k)
		}
	}
	if in.Action == "task.review" {
		var c aiReviewOutputChanges
		if e := decodeStrictToolArguments(in.Changes, &c); e != nil {
			return in, e
		}
		if id, e := uuid.Parse(c.SubmissionID); e != nil || id.String() != c.SubmissionID {
			return in, errors.New("use an actual current submission_id")
		}
		c.Reason = strings.TrimSpace(c.Reason)
		if (c.Decision != "accept" && c.Decision != "request_changes") || (c.Decision == "request_changes" && c.Reason == "") || utf8.RuneCountInString(c.Reason) > 1000 {
			return in, errors.New("review requires accept/request_changes and a reason up to 1000 characters; changes require a reason")
		}
		in.Changes, _ = json.Marshal(c)
		return in, nil
	}
	var c aiSubmitOutputChanges
	if e := decodeStrictToolArguments(in.Changes, &c); e != nil {
		return in, e
	}
	var items []map[string]json.RawMessage
	if e := json.Unmarshal(fields["artifacts"], &items); e != nil {
		return in, e
	}
	for _, item := range items {
		var kind string
		_ = json.Unmarshal(item["storage_kind"], &kind)
		payload := map[string]string{"text": "content_text", "link": "reference_url", "structured": "structured_json"}[kind]
		if payload == "" || len(item) != 5 {
			return in, errors.New("only non-file artifacts with one exact payload are supported")
		}
		for _, k := range []string{"client_ref", "storage_kind", "name", "requires_followup", payload} {
			if len(item[k]) == 0 || string(item[k]) == "null" {
				return in, errors.New("missing/null Artifact field: " + k)
			}
		}
	}
	input, prepared, e := prepareTaskOutputInput(aiSubmitOutputRequest(c), false)
	if e != nil {
		return in, e
	}
	c.Summary = input.Summary
	for i, a := range prepared {
		c.Artifacts[i].ClientRef, c.Artifacts[i].Name = a.ClientRef, a.Name
		c.Artifacts[i].ReferenceURL = a.ReferenceURL
		if a.StructuredJSON != nil {
			c.Artifacts[i].StructuredJSON = json.RawMessage(*a.StructuredJSON)
		}
	}
	in.Changes, _ = json.Marshal(c)
	return in, nil
}
func aiSubmitOutputRequest(c aiSubmitOutputChanges) submitOutputRequest {
	in := submitOutputRequest{Summary: c.Summary, Artifacts: make([]submitArtifactInput, len(c.Artifacts))}
	for i, a := range c.Artifacts {
		in.Artifacts[i] = submitArtifactInput{ClientRef: a.ClientRef, StorageKind: a.StorageKind, Name: a.Name, ContentText: a.ContentText, ReferenceURL: a.ReferenceURL, StructuredJSON: a.StructuredJSON, RequiresFollowup: a.RequiresFollowup}
	}
	return in
}

func previewAITaskOutputAction(tx *gorm.DB, in aiWorkspaceAction) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	task, e := loadTask(tx, in.TaskID)
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return p, newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
	}
	if e != nil {
		return p, e
	}
	if task.Version != in.ExpectedVersion {
		return p, taskVersionConflict()
	}
	if task.ReviewPolicy != "manual" {
		return p, newProjectRequestError(409, "TASK_MANUAL_REVIEW_REQUIRED", "Only manual-review tasks accept outputs or reviews")
	}
	if _, e = requireActiveOwnerReviewer(tx, task.ID); e != nil {
		return p, e
	}
	p.Label = task.Title
	p.Before = map[string]any{"status": task.Status, "review_policy": task.ReviewPolicy, "completion_criteria": task.CompletionCriteria, "current_submission_id": task.CurrentSubmissionID, "subtask_total": task.SubtaskTotal, "subtask_completed": task.SubtaskCompleted, "subtask_cancelled": task.SubtaskCancelled}
	for k, v := range p.Before {
		p.After[k] = v
	}
	p.TaskOutput = &aiTaskOutputPreview{Artifacts: []aiOutputArtifactPreview{}, Assignments: []aiAssignmentRow{}}
	e = tx.Table("task_assignments r").Joins("JOIN actors a ON a.id=r.actor_id").Select("r.id,r.role,r.actor_id,a.display_name AS actor_name,a.type AS actor_type,a.status AS actor_status,a.version AS actor_version,r.assigned_at,r.unassigned_at").Where("r.task_id=? AND r.unassigned_at IS NULL", task.ID).Order("r.role,r.id").Scan(&p.TaskOutput.Assignments).Error
	if e != nil {
		return p, e
	}
	if in.Action == "task.submit_output" {
		if task.Status != "todo" && task.Status != "in_progress" {
			return p, newProjectRequestError(409, "TASK_SUBMISSION_NOT_ALLOWED", "Output can only be submitted from todo or in-progress status")
		}
		if _, e = requireTaskOutputActors(tx, task.ID); e != nil {
			return p, e
		}
		var c aiSubmitOutputChanges
		_ = json.Unmarshal(in.Changes, &c)
		p.TaskOutput.Summary = c.Summary
		for _, a := range c.Artifacts {
			content := a.ContentText
			if a.StorageKind == "link" {
				content = a.ReferenceURL
			}
			if a.StorageKind == "structured" {
				s := string(a.StructuredJSON)
				content = &s
			}
			p.TaskOutput.Artifacts = append(p.TaskOutput.Artifacts, aiOutputArtifactPreview{StorageKind: a.StorageKind, Name: a.Name, Content: content, RequiresFollowup: a.RequiresFollowup})
		}
		p.After["status"] = "waiting_review"
		p.After["submission_status"] = "pending_review"
		p.After["submission_origin"] = "manual"
	} else {
		var c aiReviewOutputChanges
		_ = json.Unmarshal(in.Changes, &c)
		if task.Status != "waiting_review" || task.CurrentSubmissionID == nil || *task.CurrentSubmissionID != c.SubmissionID {
			return p, newProjectRequestError(409, "TASK_REVIEW_NOT_ALLOWED", "Read the current pending submission before proposing a review")
		}
		var s models.TaskSubmission
		if e = tx.Where("id=? AND task_id=? AND status='pending_review'", c.SubmissionID, task.ID).Take(&s).Error; e != nil {
			return p, newProjectRequestError(409, "TASK_SUBMISSION_INVALID", "Current pending submission is unavailable")
		}
		// Bound SQL allocation before loading full human evidence. Never truncate a
		// confirmation into a seemingly complete review; native Task UI remains available.
		var budget struct {
			Total int
			Bytes int64
		}
		e = tx.Model(&models.TaskArtifact{}).Where("task_id=? AND submission_id=?", task.ID, s.ID).Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN deleted_at IS NULL THEN length(CAST(COALESCE(content_text,reference_url,structured_json,'') AS BLOB)) ELSE 0 END),0) AS bytes").Scan(&budget).Error
		if e != nil {
			return p, e
		}
		if budget.Total > 20 || budget.Bytes > 96<<10 {
			return p, newProjectRequestError(422, "AI_OUTPUT_PREVIEW_TOO_LARGE", "Review this large submission in Task details; evidence will not be truncated")
		}
		var rows []struct {
			ID               string
			StorageKind      string
			Name             string
			Content          *string
			SizeBytes        *int64
			SHA256           *string
			RequiresFollowup bool
			Deleted          bool
		}
		e = tx.Model(&models.TaskArtifact{}).Select("id,storage_kind,name,CASE WHEN deleted_at IS NULL AND storage_kind <> 'file' THEN COALESCE(content_text,reference_url,structured_json) ELSE NULL END AS content,size_bytes,sha256,requires_followup,deleted_at IS NOT NULL AS deleted").Where("task_id=? AND submission_id=?", task.ID, s.ID).Order("position,id").Scan(&rows).Error
		if e != nil {
			return p, e
		}
		p.TaskOutput.Summary = s.Summary
		for _, a := range rows {
			id := a.ID
			p.TaskOutput.Artifacts = append(p.TaskOutput.Artifacts, aiOutputArtifactPreview{ID: &id, StorageKind: a.StorageKind, Name: a.Name, Content: a.Content, SizeBytes: a.SizeBytes, SHA256: a.SHA256, RequiresFollowup: a.RequiresFollowup, Deleted: a.Deleted})
		}
		p.After["submission_status"] = "changes_requested"
		p.After["status"] = "in_progress"
		if c.Decision == "accept" {
			p.After["submission_status"] = "accepted"
			p.After["status"] = "done"
		}
		p.After["submission_origin"], p.After["reason"] = s.Origin, c.Reason
	}
	return p, nil
}

func executeAITaskOutputAction(tx *gorm.DB, in aiWorkspaceAction, requestID, now string) (aiActionResult, error) {
	if in.Action == "task.submit_output" {
		var c aiSubmitOutputChanges
		_ = json.Unmarshal(in.Changes, &c)
		input, artifacts, e := prepareTaskOutputInput(aiSubmitOutputRequest(c), false)
		if e != nil {
			return aiActionResult{}, e
		}
		r, e := submitTaskOutputInTransaction(tx, in.TaskID, in.ExpectedVersion, input, artifacts, nil, requestID, now)
		return aiActionResult{ID: r.Submission.ID, Version: r.Task.Version}, e
	}
	var c aiReviewOutputChanges
	_ = json.Unmarshal(in.Changes, &c)
	// Preview is repeated in the caller's transaction; still bind the exact batch
	// at the execution boundary rather than treating 'current' as a wildcard.
	var task models.Task
	if e := tx.Select("current_submission_id").First(&task, "id=?", in.TaskID).Error; e != nil {
		return aiActionResult{}, e
	}
	if task.CurrentSubmissionID == nil || *task.CurrentSubmissionID != c.SubmissionID {
		return aiActionResult{}, newProjectRequestError(409, "TASK_SUBMISSION_INVALID", "Submission changed")
	}
	r, e := reviewTaskOutputInTransaction(tx, in.TaskID, in.ExpectedVersion, reviewTaskOutputRequest{Decision: c.Decision, Reason: &c.Reason}, requestID, now)
	return aiActionResult{ID: r.Submission.ID, Version: r.Task.Version}, e
}
