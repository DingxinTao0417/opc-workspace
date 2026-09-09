package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const (
	aiBusinessContextMaxSources   = 3
	aiBusinessContextMaxBytes     = 16 << 10
	aiBusinessContextTextMaxRunes = 2000
)

type aiBusinessContextSourceInput struct {
	Type            string `json:"type"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version,omitempty"`
}

type aiBusinessContextSource struct {
	Type            string         `json:"type"`
	ID              string         `json:"id"`
	Version         int64          `json:"version"`
	Label           string         `json:"label"`
	Fields          map[string]any `json:"fields"`
	TruncatedFields []string       `json:"truncated_fields"`
}

type aiBusinessContextEnvelope struct {
	Version   int                                `json:"version"`
	Provider  *aiBusinessContextProviderSnapshot `json:"provider,omitempty"`
	Sources   []aiBusinessContextSource          `json:"sources"`
	Knowledge []aiKnowledgeContextSource         `json:"knowledge,omitempty"`
}

type aiBusinessContextProviderSnapshot struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version int64  `json:"version"`
}

type aiBusinessContextPreviewRequest struct {
	ProviderID string                          `json:"provider_id"`
	Sources    []aiBusinessContextSourceInput  `json:"sources"`
	Knowledge  []aiKnowledgeContextSourceInput `json:"knowledge"`
}

type aiBusinessContextPreviewResponse struct {
	ProviderID      string                     `json:"provider_id"`
	ProviderName    string                     `json:"provider_name"`
	ProviderKind    string                     `json:"provider_kind"`
	ProviderVersion int64                      `json:"provider_version"`
	LeavesDevice    bool                       `json:"leaves_device"`
	SerializedBytes int                        `json:"serialized_bytes"`
	Sources         []aiBusinessContextSource  `json:"sources"`
	Knowledge       []aiKnowledgeContextSource `json:"knowledge"`
}

type aiBusinessContextRequestError struct {
	status  int
	code    string
	message string
}

func (e *aiBusinessContextRequestError) Error() string { return e.code }

func (a *API) previewAIBusinessContext(c *gin.Context) {
	var input aiBusinessContextPreviewRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	a.aiProviderMu.RLock()
	defer a.aiProviderMu.RUnlock()
	provider, ok := a.loadChatProvider(c, input.ProviderID)
	if !ok {
		return
	}
	envelope, serializedBytes, err := buildAIMessageContext(c.Request.Context(), a.db, input.Sources, input.Knowledge, false)
	if err != nil {
		if !writeAIBusinessContextError(c, err) {
			writeDatabaseError(c)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": aiBusinessContextPreviewResponse{
		ProviderID: provider.ID, ProviderName: provider.Name, ProviderKind: provider.Kind,
		ProviderVersion: provider.Version, LeavesDevice: provider.Kind != aiProviderKindLocal,
		SerializedBytes: serializedBytes, Sources: envelope.Sources, Knowledge: envelope.Knowledge,
	}})
}

func buildAIBusinessContext(ctx context.Context, db *gorm.DB, inputs []aiBusinessContextSourceInput, requireVersions bool) (aiBusinessContextEnvelope, int, error) {
	if len(inputs) == 0 || len(inputs) > aiBusinessContextMaxSources {
		return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{
			status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCES_INVALID",
			message: "Select between one and three explicit AI context sources",
		}
	}
	seenTypes := make(map[string]struct{}, len(inputs))
	normalized := make([]aiBusinessContextSourceInput, 0, len(inputs))
	for _, input := range inputs {
		input.Type = strings.ToLower(strings.TrimSpace(input.Type))
		input.ID = strings.TrimSpace(input.ID)
		if input.Type != "task" && input.Type != "project" && input.Type != "client" {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCE_TYPE_INVALID", message: "AI context source type is invalid"}
		}
		if _, duplicate := seenTypes[input.Type]; duplicate {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCE_DUPLICATE", message: "Only one AI context source of each type is allowed"}
		}
		if _, err := uuid.Parse(input.ID); err != nil {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCE_ID_INVALID", message: "AI context source id must be a UUID"}
		}
		if requireVersions && input.ExpectedVersion < 1 {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCE_VERSION_INVALID", message: "AI context source version must be positive"}
		}
		seenTypes[input.Type] = struct{}{}
		normalized = append(normalized, input)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return aiContextTypeOrder(normalized[i].Type) < aiContextTypeOrder(normalized[j].Type)
	})

	envelope := aiBusinessContextEnvelope{Version: 1, Sources: make([]aiBusinessContextSource, 0, len(normalized))}
	for _, input := range normalized {
		var source aiBusinessContextSource
		var err error
		switch input.Type {
		case "task":
			source, err = loadAITaskContext(ctx, db, input.ID)
		case "project":
			source, err = loadAIProjectContext(ctx, db, input.ID)
		case "client":
			source, err = loadAIClientContext(ctx, db, input.ID)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusNotFound, code: "AI_CONTEXT_SOURCE_NOT_FOUND", message: "An AI context source no longer exists"}
		}
		if err != nil {
			return aiBusinessContextEnvelope{}, 0, err
		}
		if requireVersions && source.Version != input.ExpectedVersion {
			return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusConflict, code: "AI_CONTEXT_CHANGED", message: "Selected workspace context changed; preview it again before sending"}
		}
		envelope.Sources = append(envelope.Sources, source)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return aiBusinessContextEnvelope{}, 0, err
	}
	if len(encoded) > aiBusinessContextMaxBytes {
		return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_TOO_LARGE", message: "Selected workspace context exceeds the 16 KiB limit"}
	}
	return envelope, len(encoded), nil
}

func aiContextTypeOrder(value string) int {
	switch value {
	case "task":
		return 0
	case "project":
		return 1
	default:
		return 2
	}
}

func loadAITaskContext(ctx context.Context, db *gorm.DB, id string) (aiBusinessContextSource, error) {
	var row struct {
		models.Task
		ProjectName *string `gorm:"column:project_name"`
	}
	err := db.WithContext(ctx).Table("tasks").
		Select("tasks.*, projects.name AS project_name").
		Joins("LEFT JOIN projects ON projects.id = tasks.project_id").
		Where("tasks.id = ?", id).Take(&row).Error
	if err != nil {
		return aiBusinessContextSource{}, err
	}
	description, descriptionTruncated := boundedAIBusinessContextText(row.Description)
	criteria, criteriaTruncated := boundedAIBusinessContextText(row.CompletionCriteria)
	truncated := make([]string, 0, 2)
	if descriptionTruncated {
		truncated = append(truncated, "description")
	}
	if criteriaTruncated {
		truncated = append(truncated, "completion_criteria")
	}
	return aiBusinessContextSource{
		Type: "task", ID: row.ID, Version: row.Version, Label: row.Title,
		Fields: map[string]any{
			"title": row.Title, "description": description, "kind": row.Kind,
			"status": row.Status, "priority": row.Priority, "completion_criteria": criteria,
			"planned_date": row.PlannedDate, "due_date": row.DueDate,
			"project_id": row.ProjectID, "project_name": row.ProjectName,
		},
		TruncatedFields: truncated,
	}, nil
}

func loadAIProjectContext(ctx context.Context, db *gorm.DB, id string) (aiBusinessContextSource, error) {
	var row struct {
		models.Project
		TaskTotal         int64 `gorm:"column:task_total"`
		TaskDone          int64 `gorm:"column:task_done"`
		TaskBlocked       int64 `gorm:"column:task_blocked"`
		TaskWaitingReview int64 `gorm:"column:task_waiting_review"`
	}
	err := db.WithContext(ctx).Raw(`
		SELECT projects.*,
		       COUNT(tasks.id) AS task_total,
		       COALESCE(SUM(CASE WHEN tasks.status = 'done' THEN 1 ELSE 0 END), 0) AS task_done,
		       COALESCE(SUM(CASE WHEN tasks.status = 'blocked' THEN 1 ELSE 0 END), 0) AS task_blocked,
		       COALESCE(SUM(CASE WHEN tasks.status = 'waiting_review' THEN 1 ELSE 0 END), 0) AS task_waiting_review
		FROM projects
		LEFT JOIN tasks ON tasks.project_id = projects.id
		WHERE projects.id = ?
		GROUP BY projects.id
	`, id).Take(&row).Error
	if err != nil {
		return aiBusinessContextSource{}, err
	}
	description, truncated := boundedAIBusinessContextText(row.Description)
	truncatedFields := []string{}
	if truncated {
		truncatedFields = append(truncatedFields, "description")
	}
	return aiBusinessContextSource{
		Type: "project", ID: row.ID, Version: row.Version, Label: row.Name,
		Fields: map[string]any{
			"name": row.Name, "description": description, "status": row.Status,
			"start_date": row.StartDate, "due_date": row.DueDate,
			"task_total": row.TaskTotal, "task_done": row.TaskDone,
			"task_blocked": row.TaskBlocked, "task_waiting_review": row.TaskWaitingReview,
		},
		TruncatedFields: truncatedFields,
	}, nil
}

func loadAIClientContext(ctx context.Context, db *gorm.DB, id string) (aiBusinessContextSource, error) {
	var row models.Client
	if err := db.WithContext(ctx).Where("id = ?", id).Take(&row).Error; err != nil {
		return aiBusinessContextSource{}, err
	}
	notes := ""
	if row.Notes != nil {
		notes = *row.Notes
	}
	notes, truncated := boundedAIBusinessContextText(notes)
	truncatedFields := []string{}
	if truncated {
		truncatedFields = append(truncatedFields, "notes")
	}
	return aiBusinessContextSource{
		Type: "client", ID: row.ID, Version: row.Version, Label: row.Name,
		Fields:          map[string]any{"name": row.Name, "status": row.Status, "notes": notes},
		TruncatedFields: truncatedFields,
	}, nil
}

func boundedAIBusinessContextText(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= aiBusinessContextTextMaxRunes {
		return value, false
	}
	runes := []rune(value)
	return string(runes[:aiBusinessContextTextMaxRunes]), true
}

func aiBusinessContextPromptLines(envelope aiBusinessContextEnvelope) []string {
	lines := make([]string, 0, len(envelope.Sources))
	for _, source := range envelope.Sources {
		encoded, _ := json.Marshal(source)
		lines = append(lines, string(encoded))
	}
	return lines
}

func encodeAIBusinessContextSnapshot(envelope aiBusinessContextEnvelope) (*string, error) {
	if len(envelope.Sources) == 0 && len(envelope.Knowledge) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func decodeAIBusinessContextSnapshot(value *string) (*aiBusinessContextProviderSnapshot, []aiBusinessContextSource, []aiKnowledgeContextSource, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, []aiBusinessContextSource{}, []aiKnowledgeContextSource{}, nil
	}
	var envelope aiBusinessContextEnvelope
	if err := json.Unmarshal([]byte(*value), &envelope); err != nil || (envelope.Version != 1 && envelope.Version != 2) || envelope.Provider == nil || envelope.Sources == nil {
		return nil, nil, nil, errors.New("invalid persisted AI business context")
	}
	if _, err := uuid.Parse(envelope.Provider.ID); err != nil || strings.TrimSpace(envelope.Provider.Name) == "" ||
		(envelope.Provider.Kind != aiProviderKindLocal && envelope.Provider.Kind != aiProviderKindRemote) || envelope.Provider.Version < 1 {
		return nil, nil, nil, errors.New("invalid persisted AI business context provider")
	}
	if len(envelope.Sources) > aiBusinessContextMaxSources || (envelope.Version == 1 && len(envelope.Sources) < 1) {
		return nil, nil, nil, errors.New("invalid persisted AI business context source count")
	}
	seen := make(map[string]struct{}, len(envelope.Sources))
	for _, source := range envelope.Sources {
		if source.Type != "task" && source.Type != "project" && source.Type != "client" {
			return nil, nil, nil, errors.New("invalid persisted AI business context source type")
		}
		if _, duplicate := seen[source.Type]; duplicate {
			return nil, nil, nil, errors.New("duplicate persisted AI business context source type")
		}
		if _, err := uuid.Parse(source.ID); err != nil || source.Version < 1 || strings.TrimSpace(source.Label) == "" || source.Fields == nil || source.TruncatedFields == nil {
			return nil, nil, nil, errors.New("invalid persisted AI business context source")
		}
		seen[source.Type] = struct{}{}
	}
	if envelope.Version == 1 && len(envelope.Knowledge) != 0 {
		return nil, nil, nil, errors.New("invalid persisted AI knowledge context version")
	}
	if envelope.Version == 2 && (len(envelope.Knowledge) > aiKnowledgeContextMaxChunks || len(envelope.Sources)+len(envelope.Knowledge) < 1) {
		return nil, nil, nil, errors.New("invalid persisted AI knowledge context count")
	}
	seenChunks := make(map[string]struct{}, len(envelope.Knowledge))
	for _, source := range envelope.Knowledge {
		for _, id := range []string{source.SourceID, source.DocumentID, source.ChunkID} {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return nil, nil, nil, errors.New("invalid persisted AI knowledge context identity")
			}
		}
		if _, duplicate := seenChunks[source.ChunkID]; duplicate || source.SourceVersion < 1 || source.DocumentVersion < 1 ||
			(source.SourceType != "text" && source.SourceType != "markdown") || strings.TrimSpace(source.SourceName) == "" ||
			strings.TrimSpace(source.DocumentTitle) == "" || strings.TrimSpace(source.Content) == "" ||
			source.ChunkIndex < 0 || source.StartChar < 0 || source.EndChar <= source.StartChar || source.StartLine < 1 || source.EndLine < source.StartLine {
			return nil, nil, nil, errors.New("invalid persisted AI knowledge context source")
		}
		seenChunks[source.ChunkID] = struct{}{}
	}
	return envelope.Provider, envelope.Sources, envelope.Knowledge, nil
}

func writeAIBusinessContextError(c *gin.Context, err error) bool {
	var requestError *aiBusinessContextRequestError
	if errors.As(err, &requestError) {
		writeError(c, requestError.status, requestError.code, requestError.message)
		return true
	}
	return false
}

func promptContextWithBusinessContext(promptContext modelclient.PromptContext, envelope aiBusinessContextEnvelope) modelclient.PromptContext {
	promptContext.BusinessContext = aiBusinessContextPromptLines(envelope)
	promptContext.KnowledgeContext = aiKnowledgeContextPromptLines(envelope.Knowledge)
	return promptContext
}
