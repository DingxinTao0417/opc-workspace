package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func aiProjectOutputsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["project_id"],"properties":{"project_id":{"type":"string","format":"uuid","description":"Current Project membership, not a saved Inbox source snapshot."},"artifact_id":{"type":"string","format":"uuid","description":"Optional exact Task Artifact, filtered before paging; another Project returns no items."},"followup_status":{"type":"string","enum":["all","none","active","open","tracking","resolved","dismissed"],"description":"Default all. active=open/tracking; none=no linked source Inbox, not proof that followup is unnecessary or completed."},"include_deleted":{"type":"boolean","description":"Default false. Deleted history is metadata only; no content or files."},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

type aiProjectOutputsQuery struct {
	ProjectID      string `json:"project_id"`
	ArtifactID     string `json:"artifact_id"`
	FollowupStatus string `json:"followup_status"`
	IncludeDeleted bool   `json:"include_deleted"`
	Limit          *int   `json:"limit"`
	Offset         int    `json:"offset"`
}

// The global decoder intentionally retains existing tools' compatibility.
// This new query contract accepts exact, unique, non-null top-level fields.
func parseAIProjectOutputsQuery(args json.RawMessage) (aiProjectOutputsQuery, error) {
	var input aiProjectOutputsQuery
	allowed := map[string]bool{"project_id": true, "artifact_id": true, "followup_status": true, "include_deleted": true, "limit": true, "offset": true}
	seen := make(map[string]bool)
	decoder := json.NewDecoder(bytes.NewReader(args))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return input, errors.New("project output query must be an object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || seen[key] {
			return input, errors.New("project output query requires exact unique schema fields")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return input, errors.New("project output query fields must be valid and non-null")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return input, errors.New("invalid project output query")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return input, errors.New("project output query has trailing data")
	}
	if err := decodeStrictToolArguments(args, &input); err != nil {
		return input, err
	}
	if id, err := uuid.Parse(input.ProjectID); err != nil || id.String() != input.ProjectID {
		return input, errors.New("project_id must be a canonical UUID")
	}
	if seen["artifact_id"] {
		if id, err := uuid.Parse(input.ArtifactID); err != nil || id.String() != input.ArtifactID {
			return input, errors.New("artifact_id must be a canonical UUID")
		}
	}
	if !seen["followup_status"] {
		input.FollowupStatus = "all"
	}
	switch input.FollowupStatus {
	case "all", "none", "active", "open", "tracking", "resolved", "dismissed":
	default:
		return input, errors.New("invalid followup_status")
	}
	return input, nil
}

type aiProjectOutputTask struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Version int64  `json:"version"`
	Route   string `json:"route"`
}

type aiProjectOutputFollowup struct {
	projectArtifactFollowupOutput
	Route string `json:"route"`
}

type aiProjectOutputItem struct {
	ArtifactID         string                   `json:"artifact_id"`
	Label              string                   `json:"label"`
	StorageKind        string                   `json:"storage_kind"`
	SubmissionID       string                   `json:"submission_id"`
	SubmissionStatus   string                   `json:"submission_status"`
	SubmissionSequence int                      `json:"submission_sequence"`
	RequiresFollowup   bool                     `json:"requires_followup"`
	CreatedAt          string                   `json:"created_at"`
	DeletedAt          *string                  `json:"deleted_at"`
	Task               aiProjectOutputTask      `json:"task" gorm:"-"`
	Followup           *aiProjectOutputFollowup `json:"followup" gorm:"-"`
	Route              string                   `json:"route" gorm:"-"`
}

func (t *aiWorkspaceTool) projectOutputs(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	if err := t.policy.Require("outputs"); err != nil {
		return nil, err
	}
	input, err := parseAIProjectOutputsQuery(args)
	if err != nil {
		return nil, err
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project struct {
			ID, Label, Status string
			Version           int64
		}
		if err := tx.Table("projects").Select("id,substr(name,1,200) AS label,status,version").Where("id=?", input.ProjectID).Take(&project).Error; err != nil {
			return err
		}
		asOf := formatInboxTimestamp(t.api.options.Now())
		base := tx.Table("task_artifacts AS a").Joins("JOIN tasks AS t ON t.id=a.task_id").Joins("JOIN task_submissions AS s ON s.id=a.submission_id AND s.task_id=a.task_id").Where("t.project_id=?", input.ProjectID)
		if !input.IncludeDeleted {
			base = base.Where("a.deleted_at IS NULL")
		}
		if input.ArtifactID != "" {
			base = base.Where("a.id=?", input.ArtifactID)
		}
		// EXISTS avoids multiplying Artifact rows. Identity validation below
		// reads every source for each returned Artifact, not only filter matches.
		source := tx.Table("inbox_items AS i").Select("1").Where("i.source_entity_type=? AND i.source_entity_id=a.id", taskArtifactInboxSourceType)
		switch input.FollowupStatus {
		case "none":
			base = base.Where("NOT EXISTS (?)", source)
		case "active":
			base = base.Where("EXISTS (?)", source.Where("i.status IN ?", []string{"open", "tracking"}))
		case "open", "tracking", "resolved", "dismissed":
			base = base.Where("EXISTS (?)", source.Where("i.status=?", input.FollowupStatus))
		}
		var total int64
		if err := base.Count(&total).Error; err != nil {
			return err
		}
		var rows []struct {
			Item        aiProjectOutputItem `gorm:"embedded"`
			TaskID      string
			TaskTitle   string
			TaskStatus  string
			TaskVersion int64
		}
		if err := base.Select(`a.id AS artifact_id,substr(a.name,1,200) AS label,a.storage_kind,a.submission_id,
 s.status AS submission_status,s.sequence AS submission_sequence,a.requires_followup,a.created_at,a.deleted_at,
 t.id AS task_id,substr(t.title,1,200) AS task_title,t.status AS task_status,t.version AS task_version`).
			Order("a.created_at DESC,a.id ASC").Offset(input.Offset).Limit(limit + 1).Scan(&rows).Error; err != nil {
			return err
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.Item.ArtifactID)
		}
		followups, err := loadProjectArtifactFollowups(tx, ids)
		if err != nil {
			return err
		}
		items := make([]aiProjectOutputItem, 0, len(rows))
		for _, row := range rows {
			item := row.Item
			item.Task = aiProjectOutputTask{ID: row.TaskID, Title: row.TaskTitle, Status: row.TaskStatus, Version: row.TaskVersion, Route: searchRoute("task", row.TaskID)}
			item.Route = taskSubmissionRoute(row.TaskID, item.SubmissionID)
			if followup := followups[item.ArtifactID]; followup != nil {
				item.Followup = &aiProjectOutputFollowup{projectArtifactFollowupOutput: *followup, Route: searchRoute("inbox_item", followup.InboxItemID)}
			}
			items = append(items, item)
		}
		var next *int
		if more && input.Offset+limit <= 1000 {
			value := input.Offset + limit
			next = &value
		}
		result = map[string]any{"project_id": project.ID, "project_version": project.Version, "project_label": project.Label, "project_status": project.Status, "route": searchRoute("project", project.ID), "as_of": asOf,
			"total_items": total, "items": items, "next_offset": next, "has_more": more, "window_limited": more && next == nil}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
