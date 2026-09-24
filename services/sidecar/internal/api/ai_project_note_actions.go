package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func addAIProjectNoteSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "project_note.create", "project_note.update", "project_note.delete")
	properties["project_note_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Existing note ID; expected_version is NOTE version, not Project version."}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Project note create: changes.project_id/title/body/occurred_at required, no target/version. Update: title/body/occurred_at only; read entire original before replacement. Delete: reason only (1-1000), soft deletion, no undo, separate HUMAN consent. Title 1-200, body 1-10000; actual RFC3339 timestamp, not future plan. Work+actions consent; never files, source/actor overrides or invented events. Full preview <=128 KiB, never truncate."
	changes["properties"].(map[string]any)["title"].(map[string]any)["minLength"] = 1
}

func parseAIProjectNoteAction(in aiWorkspaceAction, fields, raw map[string]json.RawMessage) (aiWorkspaceAction, error) {
	creating := in.Action == "project_note.create"
	if !creating && in.Action != "project_note.update" && in.Action != "project_note.delete" {
		return in, errors.New("unsupported project note action")
	}
	for key := range raw {
		if key != "action" && key != "changes" && (creating || (key != "project_note_id" && key != "expected_version")) {
			return in, errors.New("unexpected project note target/field: " + key)
		}
	}
	if !creating {
		if id, err := uuid.Parse(in.ProjectNoteID); err != nil || id.String() != in.ProjectNoteID || in.ExpectedVersion < 1 {
			return in, errors.New("read actual note ID/version first")
		}
	}
	if len(fields) == 0 {
		return in, errors.New("note changes required")
	}
	canonical := map[string]any{}
	for key, value := range fields {
		var text string
		if string(value) == "null" || json.Unmarshal(value, &text) != nil {
			return in, errors.New("note fields must be non-null strings")
		}
		var cleaned string
		var err error
		if in.Action == "project_note.delete" {
			if key != "reason" {
				return in, errors.New("note delete only accepts reason")
			}
			cleaned, err = cleanClientActivityText(text, key, 1000, true)
		} else {
			switch key {
			case "project_id":
				id, e := uuid.Parse(text)
				if !creating || e != nil || id.String() != text {
					return in, errors.New("create requires canonical project ID; note cannot move projects")
				}
				cleaned = text
			case "title":
				cleaned, err = cleanClientActivityText(text, key, 200, false)
			case "body":
				cleaned, err = cleanClientActivityText(text, key, 10000, true)
			case "occurred_at":
				var occurred time.Time
				occurred, err = time.Parse(time.RFC3339, strings.TrimSpace(text))
				cleaned = occurred.UTC().Format(time.RFC3339Nano)
			default:
				return in, errors.New("unsupported note field: " + key)
			}
		}
		if err != nil {
			return in, err
		}
		canonical[key] = cleaned
	}
	if creating {
		for _, key := range []string{"project_id", "title", "body", "occurred_at"} {
			if _, ok := canonical[key]; !ok {
				return in, errors.New("note create missing " + key)
			}
		}
	}
	in.Changes, _ = json.Marshal(canonical)
	return in, nil
}

func previewAIProjectNoteAction(tx *gorm.DB, in aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	p := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	patch := map[string]any{}
	_ = json.Unmarshal(in.Changes, &patch)
	if in.Action == "project_note.create" {
		for k, v := range patch {
			p.After[k] = v
		}
		p.After["note_state"] = "visible"
	} else {
		row, err := prepareProjectNoteChange(tx, in.ProjectNoteID, in.ExpectedVersion)
		if err != nil {
			return p, err
		}
		for _, fields := range []map[string]any{p.Before, p.After} {
			fields["project_id"], fields["title"], fields["body"], fields["occurred_at"], fields["note_state"] = row.ProjectID, row.Title, row.Body, normalizeTimestamp(row.OccurredAt), "visible"
		}
		for k, v := range patch {
			p.After[k] = v
		}
		if in.Action == "project_note.delete" {
			p.After["note_state"] = "deleted"
		}
	}
	if stamp, ok := patch["occurred_at"]; ok {
		if _, err := cleanClientActivityOccurredAt(stamp.(string), now); err != nil {
			return p, newProjectRequestError(422, "VALIDATION_ERROR", err.Error())
		}
	}
	if err := requireMutableProject(tx, p.After["project_id"].(string)); err != nil {
		return p, err
	}
	var project models.Project
	if err := tx.Select("id", "name", "status", "version").First(&project, "id=?", p.After["project_id"]).Error; err != nil {
		return p, err
	}
	for _, fields := range []map[string]any{p.Before, p.After} {
		if len(fields) == 0 {
			continue
		}
		fields["project_name"], fields["project_status"], fields["project_version"] = project.Name, project.Status, project.Version
	}
	p.Label = p.After["title"].(string)
	return p, nil
}

func executeAIProjectNoteAction(tx *gorm.DB, in aiWorkspaceAction, now string) (aiActionResult, error) {
	clock, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return aiActionResult{}, err
	}
	if in.Action == "project_note.create" {
		var request struct {
			ProjectID string `json:"project_id"`
			createProjectNoteRequest
		}
		_ = json.Unmarshal(in.Changes, &request)
		note, err := projectNoteFromCreateRequestAt(request.ProjectID, request.createProjectNoteRequest, clock)
		if err != nil {
			return aiActionResult{}, err
		}
		out, err := createProjectNoteInTransaction(tx, note)
		return aiActionResult{ID: out.ID, Version: out.Version}, err
	}
	var patch map[string]any
	if in.Action == "project_note.delete" {
		var request deleteProjectNoteRequest
		_ = json.Unmarshal(in.Changes, &request)
		patch = map[string]any{"deleted_at": now, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": request.Reason}
	} else {
		var request updateProjectNoteRequest
		_ = json.Unmarshal(in.Changes, &request)
		patch, err = projectNoteUpdatesAt(request, clock)
		if err != nil {
			return aiActionResult{}, err
		}
	}
	out, err := changeProjectNoteInTransaction(tx, in.ProjectNoteID, in.ExpectedVersion, patch, now)
	return aiActionResult{ID: out.ID, Version: out.Version}, err
}
