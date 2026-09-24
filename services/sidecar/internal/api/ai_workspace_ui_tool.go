package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
)

// aiWorkspacePanel is deliberately a closed, code-owned list. It mirrors the
// existing Agent Workspace tabs but excludes URLs, local paths, shell input and
// browser navigation. A model may only request one of these views after the
// user grants the separate workspace_ui capability for this message.
type aiWorkspacePanel string

const (
	aiWorkspacePanelOverview aiWorkspacePanel = "overview"
	aiWorkspacePanelAgents   aiWorkspacePanel = "agents"
	aiWorkspacePanelFiles    aiWorkspacePanel = "files"
	aiWorkspacePanelReview   aiWorkspacePanel = "review"
	aiWorkspacePanelTerminal aiWorkspacePanel = "terminal"
	aiWorkspacePanelBrowser  aiWorkspacePanel = "browser"
	aiWorkspacePanelManaged  aiWorkspacePanel = "managed"
)

var aiWorkspacePanels = map[aiWorkspacePanel]struct{}{
	aiWorkspacePanelOverview: {},
	aiWorkspacePanelAgents:   {},
	aiWorkspacePanelFiles:    {},
	aiWorkspacePanelReview:   {},
	aiWorkspacePanelTerminal: {},
	aiWorkspacePanelBrowser:  {},
	aiWorkspacePanelManaged:  {},
}

type aiWorkspaceOpenPanelInput struct {
	Panel          aiWorkspacePanel `json:"panel"`
	CompanionPanel json.RawMessage  `json:"companion_panel"`
	SplitRatio     json.RawMessage  `json:"split_ratio"`
}

type aiWorkspacePanelRequest struct {
	Panels     []aiWorkspacePanel
	SplitRatio *float64
}

type aiWorkspacePanelEmitter func(aiWorkspacePanelRequest) bool

type aiWorkspacePanelEmitterContextKey struct{}

func withAIWorkspacePanelEmitter(ctx context.Context, emit aiWorkspacePanelEmitter) context.Context {
	return context.WithValue(ctx, aiWorkspacePanelEmitterContextKey{}, emit)
}

// Browser navigation is a distinct, explicitly consented capability. The tool
// can only request one normalized HTTP(S) URL; the Web client still displays
// a local confirmation before it opens a new browser tab. It has no readback
// channel for browser content, history, cookies, or page state.
type aiWorkspaceBrowserNavigationInput struct {
	URL string `json:"url"`
}

type aiWorkspaceBrowserNavigationEmitter func(string) bool

type aiWorkspaceBrowserNavigationEmitterContextKey struct{}

func withAIWorkspaceBrowserNavigationEmitter(ctx context.Context, emit aiWorkspaceBrowserNavigationEmitter) context.Context {
	return context.WithValue(ctx, aiWorkspaceBrowserNavigationEmitterContextKey{}, emit)
}

// A browser action is a request for the human-operated active tab, never a
// browser command issued by the Sidecar. The local UI binds the tab and asks
// the user before invoking the native browser; no tab metadata returns here.
type aiWorkspaceBrowserActionInput struct {
	Action string `json:"action"`
}

type aiWorkspaceBrowserActionEmitter func(string) bool
type aiWorkspaceBrowserActionEmitterContextKey struct{}

func withAIWorkspaceBrowserActionEmitter(ctx context.Context, emit aiWorkspaceBrowserActionEmitter) context.Context {
	return context.WithValue(ctx, aiWorkspaceBrowserActionEmitterContextKey{}, emit)
}

var aiWorkspaceBrowserActions = map[string]struct{}{
	"back": {}, "forward": {}, "reload": {}, "stop": {},
}

// Record navigation remains local and user-confirmed. The model supplies only
// a closed resource kind plus a canonical ID; the server rechecks that the
// record exists within the already granted read scope, while the client derives
// the route from that pair rather than trusting a model-provided URL.
type aiWorkspaceRecordNavigationInput struct {
	RecordType string `json:"record_type"`
	RecordID   string `json:"record_id"`
}

// A navigation request contains only server-authorized identifiers. Run,
// Submission and Artifact owners are resolved here; the model never supplies
// those relationships or an arbitrary route.
type aiWorkspaceRecordNavigationRequest struct {
	RecordType   string
	RecordID     string
	TaskID       string
	SubmissionID string
	ParentID     string
}

type aiWorkspaceRecordNavigationEmitter func(aiWorkspaceRecordNavigationRequest) bool

type aiWorkspaceRecordNavigationEmitterContextKey struct{}

func withAIWorkspaceRecordNavigationEmitter(ctx context.Context, emit aiWorkspaceRecordNavigationEmitter) context.Context {
	return context.WithValue(ctx, aiWorkspaceRecordNavigationEmitterContextKey{}, emit)
}

var aiWorkspaceRecordNavigationTypes = map[string]struct{}{
	"task":              {},
	"task_saved_view":   {},
	"project":           {},
	"client":            {},
	"client_activity":   {},
	"client_followup":   {},
	"project_note":      {},
	"financial_entry":   {},
	"invoice":           {},
	"inbox_item":        {},
	"reminder":          {},
	"roadmap_milestone": {},
	"content_item":      {},
	"agent_run":         {},
	"task_submission":   {},
	"task_artifact":     {},
}

func aiWorkspaceBrowserNavigationSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"url"},
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "maxLength": 4096},
		},
	})
	return encoded
}

func aiWorkspaceBrowserActionSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"action"},
		"properties": map[string]any{"action": map[string]any{
			"type": "string", "enum": []string{"back", "forward", "reload", "stop"},
		}},
	})
	return encoded
}

func aiWorkspaceRecordNavigationSchema(recordTypes []string) json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"record_type", "record_id"},
		"properties": map[string]any{
			"record_type": map[string]any{"type": "string", "enum": recordTypes},
			"record_id":   map[string]any{"type": "string", "format": "uuid"},
		},
	})
	return encoded
}

func normalizeAIWorkspaceBrowserURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 || strings.IndexFunc(value, func(r rune) bool {
		return r <= 0x1f || r == 0x7f
	}) >= 0 {
		return "", errors.New("browser navigation URL is invalid")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("browser navigation URL must be an HTTP or HTTPS URL without credentials")
	}
	return parsed.String(), nil
}

func aiWorkspaceOpenPanelSchema() json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"panel"},
		"properties": map[string]any{
			"panel": map[string]any{
				"type": "string",
				"enum": []string{
					string(aiWorkspacePanelOverview), string(aiWorkspacePanelAgents),
					string(aiWorkspacePanelFiles), string(aiWorkspacePanelReview),
					string(aiWorkspacePanelTerminal), string(aiWorkspacePanelBrowser),
					string(aiWorkspacePanelManaged),
				},
			},
			"companion_panel": map[string]any{
				"type": "string",
				"enum": []string{
					string(aiWorkspacePanelOverview), string(aiWorkspacePanelAgents),
					string(aiWorkspacePanelFiles), string(aiWorkspacePanelReview),
					string(aiWorkspacePanelTerminal), string(aiWorkspacePanelBrowser),
					string(aiWorkspacePanelManaged),
				},
				"description": "Optional second fixed panel; when present, both panels are opened side by side.",
			},
			"split_ratio": map[string]any{
				"type": "number", "minimum": 0.25, "maximum": 0.75,
				"description": "Optional initial width share for the primary panel when companion_panel is present. Defaults to the current layout or an even split for a new pair.",
			},
		},
	})
	return encoded
}

func (t *aiWorkspaceTool) openPanel(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("workspace_ui"); err != nil {
		return nil, err
	}
	var input aiWorkspaceOpenPanelInput
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, errors.New("workspace panel request is invalid")
	}
	if _, ok := aiWorkspacePanels[input.Panel]; !ok {
		return nil, errors.New("workspace panel is not allowed")
	}
	requestedPanels := []aiWorkspacePanel{input.Panel}
	if len(input.CompanionPanel) > 0 {
		var companion aiWorkspacePanel
		if err := json.Unmarshal(input.CompanionPanel, &companion); err != nil {
			return nil, errors.New("workspace companion panel is invalid")
		}
		if _, ok := aiWorkspacePanels[companion]; !ok {
			return nil, errors.New("workspace companion panel is not allowed")
		}
		if companion == input.Panel {
			return nil, errors.New("workspace split requires two different panels")
		}
		if companion == aiWorkspacePanelBrowser && input.Panel == aiWorkspacePanelBrowser {
			return nil, errors.New("workspace split allows at most one Chromium surface")
		}
		requestedPanels = append(requestedPanels, companion)
	}
	var splitRatio *float64
	if len(input.SplitRatio) > 0 {
		if len(requestedPanels) != 2 {
			return nil, errors.New("workspace split ratio requires a companion panel")
		}
		var value float64
		if err := json.Unmarshal(input.SplitRatio, &value); err != nil || value < 0.25 || value > 0.75 {
			return nil, errors.New("workspace split ratio is outside the allowed range")
		}
		splitRatio = &value
	}
	emit, ok := ctx.Value(aiWorkspacePanelEmitterContextKey{}).(aiWorkspacePanelEmitter)
	if !ok || emit == nil {
		// A registry may be inspected or exercised outside an accepted chat
		// stream. Do not pretend that a local UI effect happened in that case.
		return nil, errors.New("workspace panel bridge is unavailable")
	}
	if !emit(aiWorkspacePanelRequest{Panels: requestedPanels, SplitRatio: splitRatio}) {
		return nil, context.Canceled
	}
	result := map[string]any{
		// Keep the original singleton result field stable for clients/models
		// that only understand the pre-split tool contract.
		"panel":            input.Panel,
		"panels":           requestedPanels,
		"layout":           "single",
		"requested":        true,
		"reads_content":    false,
		"executes_command": false,
	}
	if len(requestedPanels) == 2 {
		result["layout"] = "split"
	}
	if splitRatio != nil {
		result["split_ratio"] = *splitRatio
	}
	return result, nil
}

func (t *aiWorkspaceTool) requestBrowserNavigation(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("workspace_browser"); err != nil {
		return nil, err
	}
	var input aiWorkspaceBrowserNavigationInput
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, errors.New("browser navigation request is invalid")
	}
	address, err := normalizeAIWorkspaceBrowserURL(input.URL)
	if err != nil {
		return nil, err
	}
	emit, ok := ctx.Value(aiWorkspaceBrowserNavigationEmitterContextKey{}).(aiWorkspaceBrowserNavigationEmitter)
	if !ok || emit == nil {
		return nil, errors.New("browser navigation bridge is unavailable")
	}
	if !emit(address) {
		return nil, context.Canceled
	}
	return map[string]any{
		"url":                    address,
		"requested":              true,
		"requires_user_approval": true,
		"reads_browser_content":  false,
	}, nil
}

func (t *aiWorkspaceTool) requestBrowserAction(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("workspace_browser"); err != nil {
		return nil, err
	}
	var input aiWorkspaceBrowserActionInput
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, errors.New("browser action request is invalid")
	}
	if _, ok := aiWorkspaceBrowserActions[input.Action]; !ok {
		return nil, errors.New("browser action is not allowed")
	}
	emit, ok := ctx.Value(aiWorkspaceBrowserActionEmitterContextKey{}).(aiWorkspaceBrowserActionEmitter)
	if !ok || emit == nil {
		return nil, errors.New("browser action bridge is unavailable")
	}
	if !emit(input.Action) {
		return nil, context.Canceled
	}
	return map[string]any{
		"action": input.Action, "requested": true,
		"requires_user_confirmation": true, "reads_browser_content": false,
	}, nil
}

func (t *aiWorkspaceTool) requestRecordNavigation(ctx context.Context, arguments json.RawMessage) (any, error) {
	if err := t.policy.Require("workspace_ui"); err != nil {
		return nil, err
	}
	var input aiWorkspaceRecordNavigationInput
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return nil, errors.New("workspace record navigation request is invalid")
	}
	if _, ok := aiWorkspaceRecordNavigationTypes[input.RecordType]; !ok || !t.allowsRecordNavigationType(input.RecordType) {
		return nil, errors.New("workspace record type is not allowed")
	}
	parsedID, err := uuid.Parse(input.RecordID)
	if err != nil || parsedID.String() != input.RecordID {
		return nil, errors.New("workspace record id must be a canonical UUID")
	}
	if err := t.authorize(input.RecordType); err != nil {
		return nil, err
	}
	request := aiWorkspaceRecordNavigationRequest{RecordType: input.RecordType, RecordID: input.RecordID}
	if input.RecordType == "task_artifact" {
		var row struct {
			ID           string
			TaskID       string
			SubmissionID string
		}
		if err := t.api.db.WithContext(ctx).Table("task_artifacts AS artifact").
			Select("artifact.id, artifact.task_id, artifact.submission_id").
			Joins("JOIN task_submissions AS submission ON submission.id = artifact.submission_id AND submission.task_id = artifact.task_id").
			Joins("JOIN tasks ON tasks.id = artifact.task_id").
			Where("artifact.id = ? AND artifact.deleted_at IS NULL", input.RecordID).
			Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		for _, identity := range []string{row.TaskID, row.SubmissionID} {
			parsed, err := uuid.Parse(identity)
			if err != nil || parsed.String() != identity {
				return nil, errors.New("workspace record owner identity is invalid")
			}
		}
		request.TaskID = row.TaskID
		request.SubmissionID = row.SubmissionID
	} else if input.RecordType == "agent_run" || input.RecordType == "task_submission" {
		var row struct {
			ID     string
			TaskID string
		}
		query := t.api.db.WithContext(ctx)
		if input.RecordType == "agent_run" {
			query = query.Table("agent_runs").
				Select("agent_runs.id, agent_runs.task_id").
				Joins("JOIN tasks ON tasks.id = agent_runs.task_id").
				Where("agent_runs.id = ?", input.RecordID)
		} else {
			query = query.Table("task_submissions").
				Select("task_submissions.id, task_submissions.task_id").
				Joins("JOIN tasks ON tasks.id = task_submissions.task_id").
				Where("task_submissions.id = ?", input.RecordID)
		}
		if err := query.Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		parsedTaskID, err := uuid.Parse(row.TaskID)
		if err != nil || parsedTaskID.String() != row.TaskID {
			return nil, errors.New("workspace record task identity is invalid")
		}
		request.TaskID = row.TaskID
	} else if input.RecordType == "project_note" || input.RecordType == "client_activity" || input.RecordType == "client_followup" {
		var row struct {
			ID       string
			ParentID string
		}
		query := t.api.db.WithContext(ctx)
		switch input.RecordType {
		case "project_note":
			query = query.Table("project_notes AS note").
				Select("note.id, note.project_id AS parent_id").
				Joins("JOIN projects ON projects.id = note.project_id").
				Where("note.id = ? AND note.deleted_at IS NULL", input.RecordID)
		case "client_activity":
			query = query.Table("client_activities AS activity").
				Select("activity.id, activity.client_id AS parent_id").
				Joins("JOIN clients ON clients.id = activity.client_id").
				Where("activity.id = ? AND activity.deleted_at IS NULL", input.RecordID)
		case "client_followup":
			query = query.Table("client_followups AS followup").
				Select("followup.id, followup.client_id AS parent_id").
				Joins("JOIN clients ON clients.id = followup.client_id").
				Where("followup.id = ?", input.RecordID)
		}
		if err := query.Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
		parentID, err := uuid.Parse(row.ParentID)
		if err != nil || parentID.String() != row.ParentID {
			return nil, errors.New("workspace record parent identity is invalid")
		}
		request.ParentID = row.ParentID
	} else if input.RecordType == "task_saved_view" {
		var row struct{ ID string }
		if err := t.api.db.WithContext(ctx).Table("task_saved_views").Select("id").Where("id = ?", input.RecordID).Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
	} else if input.RecordType == "financial_entry" || input.RecordType == "invoice" {
		table := "financial_entries"
		if input.RecordType == "invoice" {
			table = "invoices"
		}
		var row struct{ ID string }
		if err := t.api.db.WithContext(ctx).Table(table).Select("id").Where("id = ?", input.RecordID).Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
	} else {
		resource, ok := aiWorkspaceResources[input.RecordType]
		if !ok {
			return nil, harness.ErrPermissionDenied
		}
		var row struct{ ID string }
		if err := t.api.db.WithContext(ctx).Table(resource.table).Select("id").Where("id = ?", input.RecordID).Take(&row).Error; err != nil {
			return nil, safeAIWorkspaceError(ctx, err)
		}
	}
	emit, ok := ctx.Value(aiWorkspaceRecordNavigationEmitterContextKey{}).(aiWorkspaceRecordNavigationEmitter)
	if !ok || emit == nil {
		return nil, errors.New("workspace record navigation bridge is unavailable")
	}
	if !emit(request) {
		return nil, context.Canceled
	}
	return map[string]any{
		"record_type":                input.RecordType,
		"record_id":                  input.RecordID,
		"requested":                  true,
		"requires_user_confirmation": true,
		"reads_record":               false,
	}, nil
}

var _ harness.Tool = (*aiWorkspaceTool)(nil)
