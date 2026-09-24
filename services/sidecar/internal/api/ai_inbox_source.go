package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func aiInboxSourceSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["inbox_item_id"],"properties":{"inbox_item_id":{"type":"string","format":"uuid","description":"Actual Inbox ID. Resolves current source identities only; never infer them from titles or historical payloads."}}}`)
}

func parseAIInboxSourceID(raw json.RawMessage) (string, error) {
	invalid := errors.New("inbox source query requires exactly one canonical inbox_item_id")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", invalid
	}
	key, err := decoder.Token()
	if err != nil || key != "inbox_item_id" {
		return "", invalid
	}
	var id string
	if err := decoder.Decode(&id); err != nil || decoder.More() {
		return "", invalid
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return "", invalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", invalid
	}
	if parsed, err := uuid.Parse(id); err != nil || parsed.String() != id {
		return "", invalid
	}
	return id, nil
}

type aiInboxSourceEntity struct {
	Type    string  `json:"type" gorm:"-"`
	ID      string  `json:"id"`
	Version *int64  `json:"version"`
	Status  *string `json:"status"`
	Route   string  `json:"route" gorm:"-"`
}

type aiInboxSourceResult struct {
	InboxItemID            string                `json:"inbox_item_id"`
	InboxItemVersion       int64                 `json:"inbox_item_version"`
	InboxItemStatus        string                `json:"inbox_item_status"`
	AsOf                   string                `json:"as_of"`
	SourceType             string                `json:"source_type"`
	LookupStatus           string                `json:"lookup_status"`
	RequiredScopes         []string              `json:"required_scopes"`
	Subject                *aiInboxSourceEntity  `json:"subject"`
	Related                []aiInboxSourceEntity `json:"related"`
	SourceVersion          *int64                `json:"source_version"`
	SnapshotMatchesCurrent *bool                 `json:"snapshot_matches_current"`
}

type aiInboxSourceReference struct {
	ID, Kind, Status, SourceEntityType              string
	Version                                         int64
	SourceEntityID, SourceEventKey, SourceDeletedAt *string
}

type aiInboxSourceDefinition struct {
	Scope, Table, Type, PayloadID string
	Prefix, Event, PayloadVersion string
}

// All identifiers below are code-owned SQL names. Unknown/custom source types
// are not capabilities, even when an imported Inbox happens to name a table.
var aiInboxSourceDefinitions = map[string]aiInboxSourceDefinition{
	"task":               {Table: "tasks", Type: "task", PayloadID: "task_id", Prefix: "task", Event: "blocked", PayloadVersion: "block_version"},
	"task_due":           {Table: "tasks", Type: "task", PayloadID: "task_id"},
	"project_completion": {Table: "projects", Type: "project", PayloadID: "project_id", Prefix: "project", Event: "completed", PayloadVersion: "completion_version"},
	"content_item":       {Table: "content_items", Type: "content_item", PayloadID: "content_item_id", Prefix: "content", PayloadVersion: "content_version"},
	"roadmap_milestone":  {Table: "roadmap_milestones", Type: "roadmap_milestone", PayloadID: "roadmap_milestone_id", Prefix: "roadmap", PayloadVersion: "milestone_version"},
	"reminder":           {Table: "reminders", Type: "reminder", PayloadID: "reminder_id"},
	"client_followup":    {Scope: "clients", Table: "client_followups", Type: "client_followup", PayloadID: "client_followup_id", Prefix: "followup", Event: "due"},
	"invoice_due":        {Scope: "finance", Table: "invoices", Type: "invoice", PayloadID: "invoice_id", PayloadVersion: "invoice_version"},
	"task_artifact":      {Scope: "outputs", Table: "task_artifacts", Type: "artifact", PayloadID: "artifact_id"},
	"automation":         {Table: "automation_runs", Type: "automation_run", PayloadID: "automation_run_id"},
	"agent_run_failed":   {Scope: "outputs", Table: "agent_runs", Type: "agent_run", PayloadID: "agent_run_id"},
}

var errAIInboxSourceIdentity = errors.New("Inbox source identity is inconsistent")

type aiInboxSourceProof struct {
	ID, RelatedID, OtherID, EventType, At, OtherAt, Preset string
	Version                                                int64
}

func loadAIInboxSourceProof(tx *gorm.DB, ref aiInboxSourceReference, def aiInboxSourceDefinition) (aiInboxSourceProof, error) {
	// Only individually enumerated identity/proof fields are read. No raw JSON,
	// labels, reasons, amount, body, file references or configuration snapshots.
	columns := []string{"json_extract(payload_json,'$." + def.PayloadID + "') AS id"}
	if def.PayloadVersion != "" {
		columns = append(columns, "json_extract(payload_json,'$."+def.PayloadVersion+"') AS version")
	}
	switch ref.SourceEntityType {
	case "content_item", "roadmap_milestone":
		columns = append(columns, "json_extract(payload_json,'$.event_type') AS event_type")
	case "task_due":
		columns = append(columns, "json_extract(payload_json,'$.due_at') AS at")
	case "reminder":
		columns = append(columns, "json_extract(payload_json,'$.trigger_at') AS at")
	case "client_followup":
		columns = append(columns, "json_extract(payload_json,'$.client_id') AS related_id")
	case "invoice_due":
		columns = append(columns, "json_extract(payload_json,'$.due_state') AS event_type", "json_extract(payload_json,'$.due_date') AS at", "json_extract(payload_json,'$.occurrence_date') AS other_at")
	case "task_artifact":
		columns = append(columns, "json_extract(payload_json,'$.task_id') AS related_id", "json_extract(payload_json,'$.submission_id') AS other_id")
	case "automation":
		columns = append(columns, "json_extract(payload_json,'$.automation_rule_id') AS related_id", "json_extract(payload_json,'$.preset_key') AS preset")
	}
	var proof aiInboxSourceProof
	err := tx.Table("inbox_items").Select(strings.Join(columns, ",")).Where("id=?", ref.ID).Take(&proof).Error
	return proof, err
}

func validateAIInboxSourceKey(ref aiInboxSourceReference, def aiInboxSourceDefinition, proof *aiInboxSourceProof) error {
	if ref.SourceEntityID == nil || ref.SourceEventKey == nil || proof.ID != *ref.SourceEntityID || !validCanonicalAutomationUUID(proof.ID) {
		return errAIInboxSourceIdentity
	}
	wantKind := "event"
	if ref.SourceEntityType == "reminder" {
		wantKind = "reminder"
	}
	if ref.Kind != wantKind {
		return errAIInboxSourceIdentity
	}
	key := *ref.SourceEventKey
	event := def.Event
	if ref.SourceEntityType == "content_item" {
		if proof.EventType != "review_due" && proof.EventType != "publish_due" {
			return errAIInboxSourceIdentity
		}
		event = proof.EventType
	}
	if ref.SourceEntityType == "roadmap_milestone" {
		if proof.EventType != "due" && proof.EventType != "achieved" {
			return errAIInboxSourceIdentity
		}
		event = proof.EventType
	}
	if def.Prefix != "" {
		prefix := def.Prefix + ":" + proof.ID + ":" + event + ":"
		version, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
		if err != nil || version < 1 || key != prefix+strconv.FormatInt(version, 10) || (def.PayloadVersion != "" && proof.Version != version) {
			return errAIInboxSourceIdentity
		}
		proof.Version = version
		return nil
	}
	switch ref.SourceEntityType {
	case "task_due":
		if _, err := time.Parse(time.RFC3339Nano, proof.At); err != nil || key != taskDueEventKey(proof.ID, proof.At) {
			return errAIInboxSourceIdentity
		}
	case "reminder":
		if key != "reminder:"+proof.ID+":due" {
			return errAIInboxSourceIdentity
		}
	case "invoice_due":
		if state, ok := invoiceDueState(proof.At, proof.OtherAt); !ok || state != proof.EventType || proof.Version < 1 {
			return errAIInboxSourceIdentity
		}
		switch proof.EventType {
		case "due_soon", "due", "overdue":
		default:
			return errAIInboxSourceIdentity
		}
		if key != invoiceDueEventKey(proof.ID, proof.EventType, proof.At, proof.OtherAt) {
			return errAIInboxSourceIdentity
		}
	case "task_artifact":
		if key != taskArtifactFollowupEventKey(proof.ID) {
			return errAIInboxSourceIdentity
		}
	case "automation": // The actual Run's logical key/result are checked below.
		if proof.Preset != automationPresetProjectCompleted {
			return errAIInboxSourceIdentity
		}
	default:
		return errAIInboxSourceIdentity
	}
	return nil
}

func loadAIInboxSourceEntity(tx *gorm.DB, table, kind, id string) (aiInboxSourceEntity, error) {
	var entity aiInboxSourceEntity
	if !validCanonicalAutomationUUID(id) {
		return entity, errAIInboxSourceIdentity
	}
	columns := "id,version,status"
	if kind == "task_submission" || kind == "automation_run" {
		columns = "id,status"
	}
	if kind == "artifact" {
		columns = "id"
	}
	if kind == "automation_rule" {
		columns = "id,version"
	}
	if err := tx.Table(table).Select(columns).Where("id=?", id).Take(&entity).Error; err != nil {
		return entity, err
	}
	entity.Type = kind
	entity.Route = searchRoute(kind, entity.ID)
	return entity, nil
}

func (t *aiWorkspaceTool) inboxSource(ctx context.Context, args json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	id, err := parseAIInboxSourceID(args)
	if err != nil {
		return nil, err
	}
	result := aiInboxSourceResult{RequiredScopes: []string{}, Related: []aiInboxSourceEntity{}}
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ref aiInboxSourceReference
		if err := tx.Table("inbox_items").Select("id,kind,status,version,source_entity_type,source_entity_id,source_event_key,source_deleted_at").Where("id=?", id).Take(&ref).Error; err != nil {
			return err
		}
		result.InboxItemID, result.InboxItemVersion, result.InboxItemStatus = ref.ID, ref.Version, ref.Status
		result.AsOf = formatInboxTimestamp(t.api.options.Now())
		result.SourceType, result.LookupStatus = "unsupported", "unsupported"
		if ref.SourceEntityType == "manual" {
			result.SourceType = "manual"
			if ref.Kind != "manual" || ref.SourceEntityID != nil || ref.SourceEventKey != nil {
				return errAIInboxSourceIdentity
			}
			result.SourceType, result.LookupStatus = "manual", "none"
			return nil
		}
		def, supported := aiInboxSourceDefinitions[ref.SourceEntityType]
		if !supported {
			return nil
		}
		result.SourceType = ref.SourceEntityType
		if def.Scope != "" && !t.policy.Allows(def.Scope) {
			result.LookupStatus, result.RequiredScopes = "permission_required", []string{def.Scope}
			return nil // Do not query even the existence of a protected target.
		}
		if ref.SourceDeletedAt != nil {
			result.LookupStatus = "deleted"
			return nil
		}
		if ref.SourceEntityType == "agent_run_failed" {
			if err := completeAIInboxAgentFailureSource(tx, ref, &result); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errAIInboxSourceIdentity
				}
				return err
			}
			return nil
		}
		proof, err := loadAIInboxSourceProof(tx, ref, def)
		if err != nil {
			return err
		}
		if err := validateAIInboxSourceKey(ref, def, &proof); err != nil {
			return err
		}
		entity, err := loadAIInboxSourceEntity(tx, def.Table, def.Type, proof.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result.LookupStatus = "unavailable"
			return nil
		}
		if err != nil {
			return err
		}
		if proof.Version > 0 && entity.Version != nil {
			if proof.Version > *entity.Version {
				return errAIInboxSourceIdentity
			}
			result.SourceVersion = &proof.Version
			matches := proof.Version == *entity.Version
			result.SnapshotMatchesCurrent = &matches
		}
		if err := completeAIInboxSource(tx, ref, proof, &entity, &result); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				result.LookupStatus = "unavailable"
				result.Related = []aiInboxSourceEntity{}
				result.SourceVersion, result.SnapshotMatchesCurrent = nil, nil
				return nil
			}
			return err
		}
		if result.LookupStatus == "deleted" {
			return nil
		}
		result.Subject, result.LookupStatus = &entity, "available"
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if errors.Is(err, errAIInboxSourceIdentity) {
		result.LookupStatus, result.Subject = "unavailable", nil
		result.Related = []aiInboxSourceEntity{}
		result.SourceVersion, result.SnapshotMatchesCurrent = nil, nil
		return result, nil
	}
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	return result, nil
}

func completeAIInboxSource(tx *gorm.DB, ref aiInboxSourceReference, proof aiInboxSourceProof, entity *aiInboxSourceEntity, result *aiInboxSourceResult) error {
	switch ref.SourceEntityType {
	case "task_due":
		var row struct{ DueDate *string }
		if err := tx.Table("tasks").Select("due_date").Where("id=?", entity.ID).Take(&row).Error; err != nil {
			return err
		}
		matches := false
		if row.DueDate != nil {
			current, err := time.Parse(time.RFC3339Nano, *row.DueDate)
			if err != nil {
				return errAIInboxSourceIdentity
			}
			old, _ := time.Parse(time.RFC3339Nano, proof.At)
			matches = current.Equal(old)
		}
		result.SnapshotMatchesCurrent = &matches
	case "reminder":
		var row struct {
			TriggerAt   string
			InboxItemID *string
		}
		if err := tx.Table("reminders").Select("trigger_at,inbox_item_id").Where("id=?", entity.ID).Take(&row).Error; err != nil {
			return err
		}
		if entity.Status == nil || *entity.Status != "fired" || row.InboxItemID == nil || *row.InboxItemID != ref.ID || row.TriggerAt != proof.At {
			return errAIInboxSourceIdentity
		}
	case "client_followup":
		var row struct{ ClientID string }
		if err := tx.Table("client_followups").Select("client_id").Where("id=?", entity.ID).Take(&row).Error; err != nil {
			return err
		}
		if row.ClientID != proof.RelatedID {
			return errAIInboxSourceIdentity
		}
		client, err := loadAIInboxSourceEntity(tx, "clients", "client", row.ClientID)
		if err != nil {
			return err
		}
		entity.Route = aiClientRecordRoute(client.ID, "followup", entity.ID)
		result.Related = append(result.Related, client)
	case "task_artifact":
		var row struct {
			TaskID, SubmissionID string
			RequiresFollowup     bool
			DeletedAt            *string
		}
		if err := tx.Table("task_artifacts").Select("task_id,submission_id,requires_followup,deleted_at").Where("id=?", entity.ID).Take(&row).Error; err != nil {
			return err
		}
		if row.DeletedAt != nil {
			result.LookupStatus = "deleted"
			return nil
		}
		if !row.RequiresFollowup || row.TaskID != proof.RelatedID || row.SubmissionID != proof.OtherID {
			return errAIInboxSourceIdentity
		}
		task, err := loadAIInboxSourceEntity(tx, "tasks", "task", row.TaskID)
		if err != nil {
			return err
		}
		submission, err := loadAIInboxSourceEntity(tx, "task_submissions", "task_submission", row.SubmissionID)
		if err != nil {
			return err
		}
		var owner struct{ TaskID string }
		if err := tx.Table("task_submissions").Select("task_id").Where("id=?", submission.ID).Take(&owner).Error; err != nil {
			return err
		}
		if owner.TaskID != task.ID {
			return errAIInboxSourceIdentity
		}
		submission.Route = taskSubmissionRoute(task.ID, submission.ID)
		entity.Route = submission.Route
		result.Related = append(result.Related, task, submission)
	case "automation":
		var row struct {
			RuleID, LogicalKey   string
			ResultType, ResultID *string
		}
		if err := tx.Table("automation_runs").Select("rule_id,logical_key,result_type,result_id").Where("id=?", entity.ID).Take(&row).Error; err != nil {
			return err
		}
		if entity.Status == nil || *entity.Status != "succeeded" || row.RuleID != proof.RelatedID || row.ResultType == nil || *row.ResultType != "inbox_item" || row.ResultID == nil || *row.ResultID != ref.ID || *ref.SourceEventKey != "automation:"+row.LogicalKey {
			return errAIInboxSourceIdentity
		}
		rule, err := loadAIInboxSourceEntity(tx, "automation_rules", "automation_rule", row.RuleID)
		if err != nil {
			return err
		}
		var current struct{ PresetKey string }
		if err := tx.Table("automation_rules").Select("preset_key").Where("id=?", row.RuleID).Take(&current).Error; err != nil {
			return err
		}
		if current.PresetKey != proof.Preset {
			return errAIInboxSourceIdentity
		}
		entity.Route, rule.Route = aiAutomationRoute("run", entity.ID), aiAutomationRoute("rule", rule.ID)
		result.Related = append(result.Related, rule)
	}
	return nil
}
