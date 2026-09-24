package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiProjectNotesSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["project_id","view"],"properties":{"project_id":{"type":"string","format":"uuid"},"view":{"type":"string","enum":["list","detail"]},"note_id":{"type":"string","format":"uuid","description":"Detail only; exact note belonging to project_id."},"expected_version":{"type":"integer","minimum":1,"description":"Detail only, required. Note version from list; protects body pages against concurrent edits."},"content_offset":{"type":"integer","minimum":0,"maximum":1000000},"content_limit":{"type":"integer","minimum":1,"maximum":4000,"description":"Detail only; reduce if JSON escaping exceeds result budget."},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

func aiProjectNoteRoute(projectID, noteID string) string {
	route := searchRoute("project", projectID)
	if noteID == "" {
		return route
	}
	return route + "?note=" + noteID
}

type aiProjectNoteRow struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Version    int64  `json:"version"`
	OccurredAt string `json:"occurred_at"`
	BodyLength int    `json:"body_length"`
	Route      string `json:"route" gorm:"-"`
}

func (t *aiWorkspaceTool) projectNotes(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var in struct {
		ProjectID       string `json:"project_id"`
		View            string `json:"view"`
		NoteID          string `json:"note_id"`
		ExpectedVersion int64  `json:"expected_version"`
		ContentOffset   int    `json:"content_offset"`
		ContentLimit    *int   `json:"content_limit"`
		Limit           *int   `json:"limit"`
		Offset          int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(args, &in); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(args, &raw)
	if id, err := uuid.Parse(in.ProjectID); err != nil || id.String() != in.ProjectID {
		return nil, errors.New("canonical project_id required")
	}
	allowed := map[string]bool{"project_id": true, "view": true}
	switch in.View {
	case "list":
		allowed["limit"], allowed["offset"] = true, true
	case "detail":
		allowed["note_id"], allowed["content_offset"], allowed["content_limit"], allowed["expected_version"] = true, true, true, true
		if in.ExpectedVersion < 1 {
			return nil, errors.New("detail requires current note expected_version")
		}
		if id, err := uuid.Parse(in.NoteID); err != nil || id.String() != in.NoteID {
			return nil, errors.New("canonical note_id required")
		}
	default:
		return nil, errors.New("view must be list or detail")
	}
	for key, value := range raw {
		if !allowed[key] || string(value) == "null" {
			return nil, errors.New("unexpected or null note query field: " + key)
		}
	}
	limit, err := workspacePaging(in.Limit, in.Offset)
	if err != nil {
		return nil, err
	}
	contentLimit := 4000
	if in.ContentLimit != nil {
		contentLimit = *in.ContentLimit
	}
	if contentLimit < 1 || contentLimit > 4000 || in.ContentOffset < 0 || in.ContentOffset > 1000000 {
		return nil, errors.New("invalid note content window")
	}
	var result map[string]any
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project models.Project
		if err := tx.Select("id", "name", "version", "status").First(&project, "id=?", in.ProjectID).Error; err != nil {
			return err
		}
		result = map[string]any{"project_id": project.ID, "project_name": project.Name, "project_version": project.Version, "project_status": project.Status, "view": in.View, "route": searchRoute("project", project.ID), "server_now": t.api.options.Now().UTC()}
		query := func() *gorm.DB {
			return tx.Table("project_notes").Where("project_id=? AND deleted_at IS NULL", project.ID)
		}
		columns := "id,title,version,occurred_at,length(body) AS body_length"
		if in.View == "list" {
			rows := []aiProjectNoteRow{}
			var total int64
			if err := query().Count(&total).Error; err != nil {
				return err
			}
			if err := query().Select(columns).Order("occurred_at DESC,id ASC").Offset(in.Offset).Limit(limit).Scan(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				rows[i].Route = aiProjectNoteRoute(project.ID, rows[i].ID)
				rows[i].OccurredAt = normalizeTimestamp(rows[i].OccurredAt)
			}
			result["items"], result["total"] = rows, total
			setAISubmissionPage(result, int64(in.Offset+len(rows)) < total, limit, in.Offset)
			return nil
		}
		var row struct {
			Note aiProjectNoteRow `gorm:"embedded"`
			Body string
		}
		if err := query().Select(columns+",substr(body,?,?) AS body", in.ContentOffset+1, contentLimit).Where("id=? AND version=?", in.NoteID, in.ExpectedVersion).Take(&row).Error; err != nil {
			return err
		}
		row.Note.OccurredAt = normalizeTimestamp(row.Note.OccurredAt)
		row.Note.Route = aiProjectNoteRoute(project.ID, row.Note.ID)
		result["route"] = row.Note.Route
		next := in.ContentOffset + utf8.RuneCountInString(row.Body)
		result["note"], result["body"], result["content_offset"], result["has_more"] = row.Note, row.Body, in.ContentOffset, next < row.Note.BodyLength
		result["next_content_offset"] = nil
		if next < row.Note.BodyLength {
			result["next_content_offset"] = next
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}
