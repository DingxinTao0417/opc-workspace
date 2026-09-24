package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func workspaceAgentProjectFilesSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"task_id"},
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "format": "uuid", "description": "Target Task. Lists only a different Task's current accepted file outputs in the same Project; no file bodies."},
			"offset":  map[string]any{"type": "integer", "minimum": 0, "default": 0},
			"query":   map[string]any{"type": "string", "maxLength": 200, "description": "Optional literal source Task title or file name filter. At most 20 records per page, fewer when encoded metadata is large; always follow next_offset even if some records are not eligible."},
		},
	}
}

type aiAgentProjectFilesQuery struct {
	TaskID string `json:"task_id"`
	Offset int    `json:"offset"`
	Query  string `json:"query"`
}

func parseAIAgentProjectFilesQuery(raw json.RawMessage) (aiAgentProjectFilesQuery, error) {
	var input aiAgentProjectFilesQuery
	if !utf8.Valid(raw) {
		return input, errors.New("project task file query must be UTF-8")
	}
	fields, err := strictAgentRestartObject(raw, "task_id", "offset", "query")
	if err != nil || fields["task_id"] == nil {
		return input, errors.New("project task file query requires exact unique non-null schema fields")
	}
	if err := decodeStrictToolArguments(raw, &input); err != nil {
		return input, err
	}
	if id, err := uuid.Parse(input.TaskID); err != nil || id.String() != input.TaskID {
		return input, errors.New("task_id must be a canonical UUID returned by a workspace tool")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Offset < 0 || utf8.RuneCountInString(input.Query) > 200 || strings.ContainsRune(input.Query, '\x00') {
		return input, errors.New("project task file query offset or filter is invalid")
	}
	return input, nil
}

type aiAgentProjectFilesResult struct {
	TaskID              string                          `json:"task_id"`
	TaskVersion         int64                           `json:"task_version"`
	InputFileCandidates []aiAgentExecutionFileCandidate `json:"input_file_candidates"`
	FileLimits          map[string]int                  `json:"file_limits"`
	Offset              int                             `json:"offset"`
	Limit               int                             `json:"limit"`
	Total               int64                           `json:"total"`
	NextOffset          *int                            `json:"next_offset"`
}

func (t *aiWorkspaceTool) agentProjectFiles(ctx context.Context, arguments json.RawMessage) (any, error) {
	for _, scope := range []string{"work", "outputs", "actions", "agent_execution", "agent_files", "agent_project_files"} {
		if err := t.policy.Require(scope); err != nil {
			return nil, err
		}
	}
	// Registry construction establishes the saved session. Opaque candidates
	// additionally require this exact generation's private shared map.
	if t.agentExecutionRun == nil || t.generationID == "" || t.agentExecutionRun.generationID != t.generationID {
		return nil, errors.New("project task file candidates require the current saved-session generation")
	}
	if id, err := uuid.Parse(t.generationID); err != nil || id.String() != t.generationID {
		return nil, errors.New("project task file candidates require a valid generation")
	}
	input, err := parseAIAgentProjectFilesQuery(arguments)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := aiAgentProjectFilesResult{
		TaskID: input.TaskID, Offset: input.Offset, Limit: agentRunProjectTaskFilesPageSize,
		InputFileCandidates: []aiAgentExecutionFileCandidate{},
		FileLimits: map[string]int{"max_files": agentexec.MaxFileInputs, "max_file_bytes": agentexec.MaxFileInputBytes,
			"max_total_bytes": agentexec.MaxFileInputsTotalBytes, "max_result_bytes": agentexec.MaxResultBytes},
	}
	var files []agentRunFileCandidate
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.Task
		if err := tx.Select("id", "project_id", "version").First(&task, "id = ?", input.TaskID).Error; err != nil {
			return err
		}
		result.TaskVersion = task.Version
		var err error
		files, result.Total, err = t.api.loadAgentRunProjectTaskFileCandidates(tx, task, input.Offset, input.Query)
		return err
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, safeAIWorkspaceError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Bound the actual JSON, including escaped user-owned titles/names. A
	// full metadata page must not make later evidence unreachable. The cursor
	// advances over the consumed source rows, not only eligible candidates.
	for {
		result.InputFileCandidates, _ = t.agentExecutionRun.publishFileCandidates(input.TaskID, files)
		result.NextOffset = nil
		if len(files) > 0 && int64(input.Offset) < result.Total-int64(len(files)) {
			next := input.Offset + len(files)
			result.NextOffset = &next
		}
		encoded, err := json.Marshal(result)
		if err == nil && len(encoded) <= 24<<10 {
			return result, nil
		}
		if err != nil || len(files) <= 1 {
			return nil, errors.New("project task file metadata exceeds the tool result budget")
		}
		files = files[:len(files)-1]
	}
}
