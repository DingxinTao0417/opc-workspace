package api

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiTaskSubmissionsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["task_id","view"],"properties":{"task_id":{"type":"string","format":"uuid"},"view":{"type":"string","enum":["list","text","artifacts"]},"submission_id":{"type":"string","format":"uuid","description":"Required for text/artifacts; must belong to task_id. Never guess from an artifact or Run ID."},"status":{"type":"string","enum":["pending_review","accepted","changes_requested","withdrawn"],"description":"List only; omitted includes history."},"field":{"type":"string","enum":["summary","review_reason"],"description":"Text only, required. Each call returns at most 4000 Unicode characters."},"content_limit":{"type":"integer","minimum":1,"maximum":4000,"description":"Text only, default 4000. Reduce if JSON escaping exceeds the result budget."},"content_offset":{"type":"integer","minimum":0,"maximum":1000000,"description":"Text only."},"limit":{"type":"integer","minimum":1,"maximum":20,"description":"List/artifacts only."},"offset":{"type":"integer","minimum":0,"maximum":1000,"description":"List/artifacts only."}}}`)
}

type aiTaskSubmissionRow struct {
	ID                   string  `json:"id"`
	Sequence             int     `json:"sequence"`
	Status               string  `json:"status"`
	Origin               string  `json:"origin"`
	SubmittedAt          string  `json:"submitted_at"`
	ReviewedAt           *string `json:"reviewed_at"`
	WithdrawnAt          *string `json:"withdrawn_at"`
	SummaryLength        int     `json:"summary_length"`
	ReviewReasonLength   int     `json:"review_reason_length"`
	ArtifactCount        int     `json:"artifact_count"`
	DeletedArtifactCount int     `json:"deleted_artifact_count"`
	IsCurrent            bool    `json:"is_current" gorm:"-"`
	Route                string  `json:"route" gorm:"-"`
}

// Do not load summary/review text, Actor details or Artifact payloads when
// listing batches. Summary-only and child-rollup submissions are real batches.
func aiTaskSubmissionQuery(tx *gorm.DB, taskID string) *gorm.DB {
	return tx.Table("task_submissions s").Select(`s.id,s.sequence,s.status,s.origin,s.submitted_at,s.reviewed_at,s.withdrawn_at,
	 length(s.summary) AS summary_length,length(COALESCE(s.review_reason,'')) AS review_reason_length,
	 (SELECT COUNT(*) FROM task_artifacts a WHERE a.submission_id=s.id) AS artifact_count,
	 (SELECT COUNT(*) FROM task_artifacts a WHERE a.submission_id=s.id AND a.deleted_at IS NOT NULL) AS deleted_artifact_count`).
		Where("s.task_id=?", taskID)
}

func (t *aiWorkspaceTool) taskSubmissions(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	if err := t.policy.Require("outputs"); err != nil {
		return nil, err
	}
	var in struct {
		TaskID        string `json:"task_id"`
		View          string `json:"view"`
		SubmissionID  string `json:"submission_id"`
		Status        string `json:"status"`
		Field         string `json:"field"`
		ContentOffset int    `json:"content_offset"`
		ContentLimit  *int   `json:"content_limit"`
		Limit         *int   `json:"limit"`
		Offset        int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &in); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(args, &raw)
	for key, value := range raw {
		if string(value) == "null" {
			return nil, errors.New("null is not allowed: " + key)
		}
	}
	if id, err := uuid.Parse(in.TaskID); err != nil || id.String() != in.TaskID {
		return nil, errors.New("task_id must be a canonical UUID")
	}
	if in.View != "list" && in.View != "text" && in.View != "artifacts" {
		return nil, errors.New("view must be list, text or artifacts")
	}
	allowed := map[string]bool{"task_id": true, "view": true}
	if in.View == "list" {
		allowed["status"] = true
		if _, exists := raw["status"]; exists && in.Status != "pending_review" && in.Status != "accepted" && in.Status != "changes_requested" && in.Status != "withdrawn" {
			return nil, errors.New("invalid submission status")
		}
	} else {
		allowed["submission_id"] = true
		if id, err := uuid.Parse(in.SubmissionID); err != nil || id.String() != in.SubmissionID {
			return nil, errors.New("submission_id must be a canonical UUID")
		}
	}
	contentLimit := 4000
	if in.View == "text" {
		allowed["field"], allowed["content_offset"], allowed["content_limit"] = true, true, true
		if in.Field != "summary" && in.Field != "review_reason" {
			return nil, errors.New("text requires field summary or review_reason")
		}
		if in.ContentOffset < 0 || in.ContentOffset > 1_000_000 {
			return nil, errors.New("invalid content_offset")
		}
		if in.ContentLimit != nil {
			contentLimit = *in.ContentLimit
		}
		if contentLimit < 1 || contentLimit > 4000 {
			return nil, errors.New("content_limit must be 1–4000")
		}
	} else {
		allowed["limit"], allowed["offset"] = true, true
	}
	for key := range raw {
		if !allowed[key] {
			return nil, errors.New("field is not allowed for this view: " + key)
		}
	}
	limit, err := workspacePaging(in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.Select("id", "version", "status", "review_policy", "current_submission_id").First(&task, "id=?", in.TaskID).Error; err != nil {
			return err
		}
		isCurrent := func(id string) bool { return task.CurrentSubmissionID != nil && *task.CurrentSubmissionID == id }
		result = map[string]any{"task_id": task.ID, "task_version": task.Version, "task_status": task.Status, "review_policy": task.ReviewPolicy, "current_submission_id": task.CurrentSubmissionID, "view": in.View, "route": searchRoute("task", task.ID)}
		if in.View == "list" {
			query := aiTaskSubmissionQuery(tx, task.ID)
			if in.Status != "" {
				query = query.Where("s.status=?", in.Status)
			}
			rows := []aiTaskSubmissionRow{}
			if err := query.Order("s.sequence DESC,s.id DESC").Offset(in.Offset).Limit(limit + 1).Scan(&rows).Error; err != nil {
				return err
			}
			more := len(rows) > limit
			if more {
				rows = rows[:limit]
			}
			for index := range rows {
				rows[index].IsCurrent = isCurrent(rows[index].ID)
				rows[index].Route = taskSubmissionRoute(task.ID, rows[index].ID)
			}
			result["items"] = rows
			setAISubmissionPage(result, more, limit, in.Offset)
			return nil
		}
		var submission aiTaskSubmissionRow
		if err := aiTaskSubmissionQuery(tx, task.ID).Where("s.id=?", in.SubmissionID).Take(&submission).Error; err != nil {
			return err
		}
		submission.IsCurrent = isCurrent(submission.ID)
		submission.Route = taskSubmissionRoute(task.ID, submission.ID)
		result["route"] = submission.Route
		result["submission"] = submission
		if in.View == "text" {
			// SQL column is selected from code-owned constants, never input.
			column := "summary"
			if in.Field == "review_reason" {
				column = "review_reason"
			}
			var text struct {
				Content string
				Length  int
			}
			if err := tx.Table("task_submissions").Select("substr(COALESCE("+column+",''),?,?) AS content,length(COALESCE("+column+",'')) AS length", in.ContentOffset+1, contentLimit).
				Where("id=? AND task_id=?", submission.ID, task.ID).Take(&text).Error; err != nil {
				return err
			}
			var next *int
			if n := in.ContentOffset + utf8.RuneCountInString(text.Content); n < text.Length {
				next = &n
			}
			result["field"], result["content"] = in.Field, text.Content
			result["content_limit"] = contentLimit
			result["content_offset"], result["content_length"], result["next_content_offset"] = in.ContentOffset, text.Length, next
			return nil
		}
		// Even deleted rows retain metadata; body reads use workspace_get, which
		// rejects deleted Artifacts. Files are metadata only, not reviewed bytes.
		rows := []struct {
			ID               string  `json:"id"`
			Position         int     `json:"position"`
			Name             string  `json:"name"`
			StorageKind      string  `json:"storage_kind"`
			SizeBytes        *int64  `json:"size_bytes"`
			MimeType         *string `json:"mime_type"`
			SHA256           *string `json:"sha256"`
			RequiresFollowup bool    `json:"requires_followup"`
			DeletedAt        *string `json:"deleted_at"`
		}{}
		if err := tx.Table("task_artifacts").Select("id,position,name,storage_kind,size_bytes,mime_type,sha256,requires_followup,deleted_at").
			Where("task_id=? AND submission_id=?", task.ID, submission.ID).
			Order("position ASC,id ASC").Offset(in.Offset).Limit(limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		result["items"] = rows
		setAISubmissionPage(result, more, limit, in.Offset)
		return nil
	})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

func setAISubmissionPage(result map[string]any, more bool, limit, offset int) {
	var next *int
	if more && offset+limit <= 1000 {
		n := offset + limit
		next = &n
	}
	result["has_more"], result["next_offset"], result["window_limited"] = more, next, more && next == nil
}
