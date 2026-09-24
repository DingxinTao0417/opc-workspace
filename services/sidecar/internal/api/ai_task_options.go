package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Only identities needed to choose an assignee/tag, never Actor notes, metadata,
// contacts or adapter configuration. Assignment does not start an Agent Run.
type aiTaskOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Type    string `json:"type"`
	Version int64  `json:"version"`
	Color   string `json:"color,omitempty"`
}

func aiTaskOptionsSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["type"],"properties":{"type":{"type":"string","enum":["actor","tag"]},"id":{"type":"string","format":"uuid","description":"Exact tag; excludes query/limit/offset/role. Missing fails."},"role":{"type":"string","enum":["assignee","reviewer"],"description":"Actors: default assignee; reviewer=owner only."},"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":20},"offset":{"type":"integer","minimum":0,"maximum":1000}}}`)
}

func (t *aiWorkspaceTool) taskOptions(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("work"); err != nil {
		return nil, err
	}
	var input struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Role   string `json:"role"`
		Query  string `json:"query"`
		Limit  *int   `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, err
	}
	if input.Type != "actor" && input.Type != "tag" {
		return nil, errors.New("type must be actor or tag")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &fields); err != nil {
		return nil, err
	}
	for field := range fields {
		switch field {
		case "type", "id", "role", "query", "limit", "offset":
		default:
			return nil, errors.New("task option field is invalid")
		}
	}
	_, exact := fields["id"]
	if exact {
		id, err := uuid.Parse(input.ID)
		if err != nil || id.String() != input.ID || input.Type != "tag" {
			return nil, errors.New("id must be a canonical tag UUID")
		}
		for _, field := range []string{"query", "limit", "offset", "role"} {
			if _, present := fields[field]; present {
				return nil, errors.New("exact tag id cannot be combined with search or paging")
			}
		}
	}
	if input.Role != "" && (input.Type != "actor" || (input.Role != "assignee" && input.Role != "reviewer")) {
		return nil, errors.New("role applies only to actor candidates and must be assignee or reviewer")
	}
	if utf8.RuneCountInString(input.Query) > 200 {
		return nil, errors.New("query exceeds 200 characters")
	}
	limit, err := workspacePaging(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	query := t.api.db.WithContext(ctx).Table("tags").Select("id, name AS label, 'tag' AS type, version, color")
	label := "name"
	if input.Type == "actor" {
		query = t.api.db.WithContext(ctx).Table("actors").Select("id, display_name AS label, type, version").
			Where("status='active' AND (type IN ('owner','person') OR (type='agent' AND EXISTS (SELECT 1 FROM agent_adapters a WHERE a.id=actors.agent_adapter_id AND a.status='enabled' AND a.execution_ready=1)))")
		label = "display_name"
		if input.Role == "reviewer" {
			query = query.Where("type='owner'")
		}
	}
	if q := strings.TrimSpace(input.Query); q != "" {
		query = query.Where("instr(lower("+label+"),lower(?))>0", q)
	}
	rows := []aiTaskOption{}
	if exact {
		query = query.Where("id = ?", input.ID)
		limit = 1
	}
	if err := query.Order(label).Order("id").Limit(limit + 1).Offset(input.Offset).Scan(&rows).Error; err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	if exact && len(rows) != 1 {
		return nil, errors.New("tag not found; do not substitute a name match")
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	var next *int
	if more && input.Offset+limit <= 1000 {
		v := input.Offset + limit
		next = &v
	}
	return map[string]any{"type": input.Type, "items": rows, "has_more": more, "next_offset": next, "window_limited": more && next == nil}, nil
}
