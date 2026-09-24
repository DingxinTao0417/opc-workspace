package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func aiClientRecordsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["type","view"],"properties":{
"type":{"type":"string","enum":["activity","followup"]},"view":{"type":"string","enum":["list","detail"]},
"id":{"type":"string","format":"uuid","description":"Required for detail; forbidden for list"},
"client_id":{"type":"string","format":"uuid","description":"Optional list filter; find real Client IDs using workspace_search"},
"status":{"type":"string","enum":["planned","completed","skipped","cancelled"],"description":"Followup list only"},
"assigned_actor_id":{"type":"string","format":"uuid","description":"Followup list only"},
"due_state":{"type":"string","enum":["overdue"],"description":"Followup list only; planned and scheduled strictly before server_now"},
"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000},
"field":{"type":"string","enum":["body","notes","result","next_step","skip_reason","cancel_reason"],"description":"Detail only: activity body (default); followup notes (default), result, next_step, skip_reason or cancel_reason. One text field per request."},
"content_offset":{"type":"integer","minimum":0,"maximum":1000000,"description":"Detail only, Unicode character offset"},
"content_limit":{"type":"integer","minimum":1,"maximum":4000,"description":"Detail only, max Unicode characters per page (default 4000). Reduce if JSON escaping exceeds the 24 KiB result budget."}}}`)
}

func (t *aiWorkspaceTool) clientRecords(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("clients"); err != nil {
		return nil, err
	}
	var input struct {
		Type            string `json:"type"`
		View            string `json:"view"`
		ID              string `json:"id"`
		ClientID        string `json:"client_id"`
		Status          string `json:"status"`
		AssignedActorID string `json:"assigned_actor_id"`
		DueState        string `json:"due_state"`
		Limit           *int   `json:"limit"`
		Offset          int    `json:"offset"`
		Field           string `json:"field"`
		ContentOffset   int    `json:"content_offset"`
		ContentLimit    *int   `json:"content_limit"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(arguments, &raw)
	for key, value := range raw {
		if string(value) == "null" {
			return nil, errors.New("null client record argument: " + key)
		}
		if key == "id" || key == "client_id" || key == "assigned_actor_id" {
			var text string
			_ = json.Unmarshal(value, &text)
			if id, err := uuid.Parse(text); err != nil || id.String() != text {
				return nil, errors.New("use canonical actual UUIDs")
			}
		}
	}
	if input.Type != "activity" && input.Type != "followup" {
		return nil, errors.New("type must be activity or followup")
	}
	if input.View == "detail" {
		for _, key := range []string{"client_id", "status", "assigned_actor_id", "due_state", "limit", "offset"} {
			if _, present := raw[key]; present {
				return nil, errors.New("list filters are not allowed for detail")
			}
		}
		if input.ID == "" || input.ContentOffset < 0 || input.ContentOffset > 1000000 {
			return nil, errors.New("detail requires id and content_offset 0-1000000")
		}
		contentLimit := 4000
		if input.ContentLimit != nil {
			contentLimit = *input.ContentLimit
		}
		if contentLimit < 1 || contentLimit > 4000 {
			return nil, errors.New("content_limit must be 1-4000")
		}
		if _, present := raw["field"]; present && input.Field == "" {
			return nil, errors.New("field cannot be empty")
		}
		if input.Field == "" {
			if input.Type == "activity" {
				input.Field = "body"
			} else {
				input.Field = "notes"
			}
		}
		allowed := input.Field == "body" && input.Type == "activity"
		if input.Type == "followup" {
			allowed = input.Field == "notes" || input.Field == "result" || input.Field == "next_step" || input.Field == "skip_reason" || input.Field == "cancel_reason"
		}
		if !allowed {
			return nil, errors.New("unsupported record text field")
		}
		return t.clientRecordDetail(ctx, input.Type, input.ID, input.Field, input.ContentOffset, contentLimit)
	}
	if input.View != "list" {
		return nil, errors.New("view must be list or detail")
	}
	for _, key := range []string{"id", "field", "content_offset", "content_limit"} {
		if _, present := raw[key]; present {
			return nil, errors.New("detail arguments are not allowed for list")
		}
	}
	if input.Type == "activity" {
		for _, key := range []string{"status", "assigned_actor_id", "due_state"} {
			if _, present := raw[key]; present {
				return nil, errors.New("followup filters are not allowed for activity")
			}
		}
	}
	if _, present := raw["status"]; present {
		if _, valid := validClientFollowupStatuses[input.Status]; !valid {
			return nil, errors.New("invalid followup status")
		}
	}
	if _, present := raw["due_state"]; present {
		if input.DueState != "overdue" || (input.Status != "" && input.Status != "planned") {
			return nil, errors.New("overdue requires planned status")
		}
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	now := t.api.options.Now().UTC()
	type record struct {
		ID             string `json:"id"`
		ClientID       string `json:"client_id"`
		Label          string `json:"label"`
		LabelTruncated bool   `json:"label_truncated"`
		Status         string `json:"status,omitempty"`
		Kind           string `json:"kind,omitempty"`
		At             string `json:"at"`
		Version        int64  `json:"version"`
		Route          string `json:"route"`
	}
	items := []record{}
	var total int64
	order := "scheduled_at ascending, id ascending (nanosecond UTC key)"
	if input.Type == "activity" {
		order = "occurred_at descending, id descending (nanosecond UTC key)"
	}
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if input.ClientID != "" {
			var client models.Client
			if err := tx.Select("id").First(&client, "id=?", input.ClientID).Error; err != nil {
				return err
			}
		}
		query := tx.Table("client_followups")
		columns := "id, client_id, substr(purpose,1,160) AS label, length(purpose)>160 AS label_truncated, status, scheduled_at AS at, version"
		if input.Type == "activity" {
			query = tx.Table("client_activities").Where("deleted_at IS NULL")
			columns = "id, client_id, substr(title,1,160) AS label, length(title)>160 AS label_truncated, kind, occurred_at AS at, version"
		}
		if input.ClientID != "" {
			query = query.Where("client_id=?", input.ClientID)
		}
		if input.Status != "" {
			query = query.Where("status=?", input.Status)
		}
		if input.AssignedActorID != "" {
			query = query.Where("assigned_actor_id=?", input.AssignedActorID)
		}
		if input.DueState == "overdue" {
			query = query.Where(clientFollowupOverduePredicate, now.Format(clientFollowupTimeKeyLayout))
		}
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		if input.Type == "activity" {
			query = query.Order(clientActivityOccurredAtUTCKeyExpression + " DESC").Order("id DESC")
		} else {
			query = query.Order(clientFollowupScheduledTimeKey + " ASC").Order("id ASC")
		}
		return query.Select(columns).Limit(limit).Offset(input.Offset).Scan(&items).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	for i := range items {
		items[i].Route = aiClientRecordRoute(items[i].ClientID, input.Type, items[i].ID)
	}
	more := int64(input.Offset+len(items)) < total
	var next *int
	if n := input.Offset + limit; more && n <= 1000 {
		next = &n
	}
	return map[string]any{"type": input.Type, "items": items, "total_items": total, "limit": limit, "offset": input.Offset, "has_more": more, "next_offset": next, "window_limited": more && next == nil, "server_now": now.Format(time.RFC3339Nano), "order": order, "snapshot": "live per request; changes between pages can move records"}, nil
}

func (t *aiWorkspaceTool) clientRecordDetail(ctx context.Context, kind, id, field string, offset, limit int) (any, error) {
	fields := map[string]any{}
	var text struct {
		Content       *string
		ContentLength int
	}
	var clientID string
	var version int64
	err := t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		table := "client_followups"
		query := tx.Table(table).Where("id=?", id)
		if kind == "activity" {
			table = "client_activities"
			query = tx.Table(table).Where("id=? AND deleted_at IS NULL", id)
			var item models.ClientActivity
			if err := query.Select("id", "client_id", "kind", "title", "occurred_at", "version").Take(&item).Error; err != nil {
				return err
			}
			clientID, version = item.ClientID, item.Version
			fields["title"], fields["kind"], fields["occurred_at"] = item.Title, item.Kind, item.OccurredAt
			fields["read_only"] = item.Kind == "system_reference"
		} else {
			var item models.ClientFollowup
			if err := query.Select("id", "client_id", "assigned_actor_id", "scheduled_at", "timezone", "channel", "purpose", "status", "priority", "completed_at", "skipped_at", "cancelled_at", "rescheduled_from_id", "version").Take(&item).Error; err != nil {
				return err
			}
			clientID, version = item.ClientID, item.Version
			fields = aiClientFollowupPlan(item)
			delete(fields, "notes")
			fields["completed_at"], fields["skipped_at"], fields["cancelled_at"], fields["rescheduled_from_id"] = item.CompletedAt, item.SkippedAt, item.CancelledAt, item.RescheduledFromID
			if err := addAIClientFollowupIdentities(tx, fields); err != nil {
				return err
			}
		}
		// field is from the closed enum above; pagination happens in SQL, before allocation.
		return query.Select("substr("+field+",?,?) AS content, COALESCE(length("+field+"),0) AS content_length", offset+1, limit).Take(&text).Error
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	count := 0
	if text.Content != nil {
		count = utf8.RuneCountInString(*text.Content)
	}
	var next *int
	if n := offset + count; n < text.ContentLength && n <= 1000000 {
		next = &n
	}
	return map[string]any{"type": kind, "id": id, "client_id": clientID, "version": version, "fields": fields, "field": field,
		"content": text.Content, "content_offset": offset, "content_limit": limit, "content_length": text.ContentLength, "next_content_offset": next,
		"route": aiClientRecordRoute(clientID, kind, id), "server_now": t.api.options.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func aiClientRecordRoute(clientID, kind, id string) string {
	return searchRoute("client", clientID) + "?" + kind + "=" + id
}
