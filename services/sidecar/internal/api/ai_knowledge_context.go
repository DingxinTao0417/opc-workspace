package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	aiKnowledgeContextMaxChunks = 3
	aiKnowledgeContextMaxBytes  = 12 << 10
	aiCombinedContextMaxBytes   = 30 << 10
)

type aiKnowledgeContextSourceInput struct {
	SourceID                string `json:"source_id"`
	DocumentID              string `json:"document_id"`
	ChunkID                 string `json:"chunk_id"`
	ExpectedSourceVersion   int64  `json:"expected_source_version,omitempty"`
	ExpectedDocumentVersion int64  `json:"expected_document_version,omitempty"`
}

type aiKnowledgeContextSource = knowledgeChunk

func buildAIMessageContext(
	ctx context.Context,
	db *gorm.DB,
	businessInputs []aiBusinessContextSourceInput,
	knowledgeInputs []aiKnowledgeContextSourceInput,
	requireVersions bool,
) (aiBusinessContextEnvelope, int, error) {
	if len(businessInputs) == 0 && len(knowledgeInputs) == 0 {
		return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{
			status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_SOURCES_INVALID",
			message: "Select at least one explicit workspace or knowledge context source",
		}
	}
	envelope := aiBusinessContextEnvelope{
		Version: 1, Sources: []aiBusinessContextSource{}, Knowledge: []aiKnowledgeContextSource{},
	}
	if len(businessInputs) > 0 {
		business, _, err := buildAIBusinessContext(ctx, db, businessInputs, requireVersions)
		if err != nil {
			return aiBusinessContextEnvelope{}, 0, err
		}
		envelope.Sources = business.Sources
	}
	if len(knowledgeInputs) > 0 {
		knowledge, err := buildAIKnowledgeContext(ctx, db, knowledgeInputs, requireVersions)
		if err != nil {
			return aiBusinessContextEnvelope{}, 0, err
		}
		envelope.Version = 2
		envelope.Knowledge = knowledge
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return aiBusinessContextEnvelope{}, 0, err
	}
	if len(encoded) > aiCombinedContextMaxBytes {
		return aiBusinessContextEnvelope{}, 0, &aiBusinessContextRequestError{
			status: http.StatusUnprocessableEntity, code: "AI_CONTEXT_TOO_LARGE",
			message: "Selected workspace and knowledge context exceeds the 30 KiB limit",
		}
	}
	return envelope, len(encoded), nil
}

func buildAIKnowledgeContext(ctx context.Context, db *gorm.DB, inputs []aiKnowledgeContextSourceInput, requireVersions bool) ([]aiKnowledgeContextSource, error) {
	if len(inputs) == 0 || len(inputs) > aiKnowledgeContextMaxChunks {
		return nil, &aiBusinessContextRequestError{
			status: http.StatusUnprocessableEntity, code: "AI_KNOWLEDGE_CONTEXT_INVALID",
			message: "Select between one and three knowledge chunks",
		}
	}
	seen := make(map[string]struct{}, len(inputs))
	result := make([]aiKnowledgeContextSource, 0, len(inputs))
	for _, input := range inputs {
		input.SourceID = strings.TrimSpace(input.SourceID)
		input.DocumentID = strings.TrimSpace(input.DocumentID)
		input.ChunkID = strings.TrimSpace(input.ChunkID)
		for _, value := range []string{input.SourceID, input.DocumentID, input.ChunkID} {
			parsed, err := uuid.Parse(value)
			if err != nil || parsed.String() != value {
				return nil, &aiBusinessContextRequestError{
					status: http.StatusUnprocessableEntity, code: "AI_KNOWLEDGE_CONTEXT_INVALID",
					message: "Knowledge context identities must be canonical UUIDs",
				}
			}
		}
		if _, duplicate := seen[input.ChunkID]; duplicate {
			return nil, &aiBusinessContextRequestError{
				status: http.StatusUnprocessableEntity, code: "AI_KNOWLEDGE_CONTEXT_DUPLICATE",
				message: "The same knowledge chunk cannot be selected twice",
			}
		}
		if requireVersions && (input.ExpectedSourceVersion < 1 || input.ExpectedDocumentVersion < 1) {
			return nil, &aiBusinessContextRequestError{
				status: http.StatusUnprocessableEntity, code: "AI_KNOWLEDGE_CONTEXT_INVALID",
				message: "Knowledge source and document versions must be positive",
			}
		}
		seen[input.ChunkID] = struct{}{}

		row, err := readKnowledgeChunk(ctx, db, input.SourceID, input.DocumentID, input.ChunkID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &aiBusinessContextRequestError{
				status: http.StatusNotFound, code: "AI_KNOWLEDGE_CONTEXT_NOT_FOUND",
				message: "A selected knowledge chunk no longer exists",
			}
		}
		if err != nil {
			return nil, err
		}
		if requireVersions && (row.SourceVersion != input.ExpectedSourceVersion || row.DocumentVersion != input.ExpectedDocumentVersion) {
			return nil, &aiBusinessContextRequestError{
				status: http.StatusConflict, code: "AI_KNOWLEDGE_CONTEXT_CHANGED",
				message: "Selected knowledge context changed; preview it again before sending",
			}
		}
		result = append(result, row)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) > aiKnowledgeContextMaxBytes {
		return nil, &aiBusinessContextRequestError{
			status: http.StatusUnprocessableEntity, code: "AI_KNOWLEDGE_CONTEXT_TOO_LARGE",
			message: "Selected knowledge chunks exceed the 12 KiB limit",
		}
	}
	return result, nil
}

func aiKnowledgeContextPromptLines(sources []aiKnowledgeContextSource) []string {
	lines := make([]string, 0, len(sources))
	for _, source := range sources {
		encoded, _ := json.Marshal(source)
		lines = append(lines, string(encoded))
	}
	return lines
}
