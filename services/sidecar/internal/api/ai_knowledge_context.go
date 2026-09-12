package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
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

type aiKnowledgeContextSource struct {
	SourceID        string `json:"source_id"`
	SourceName      string `json:"source_name"`
	SourceVersion   int64  `json:"source_version"`
	SourceType      string `json:"source_type"`
	DocumentID      string `json:"document_id"`
	DocumentTitle   string `json:"document_title"`
	DocumentVersion int64  `json:"document_version"`
	ChunkID         string `json:"chunk_id"`
	ChunkIndex      int    `json:"chunk_index"`
	StartChar       int    `json:"start_char"`
	EndChar         int    `json:"end_char"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	StartPage       int    `json:"start_page"`
	EndPage         int    `json:"end_page"`
	Content         string `json:"content"`
}

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

		var row struct {
			models.KnowledgeChunk
			SourceName      string `gorm:"column:source_name"`
			SourceVersion   int64  `gorm:"column:source_version"`
			SourceType      string `gorm:"column:source_type"`
			DocumentTitle   string `gorm:"column:document_title"`
			DocumentVersion int64  `gorm:"column:document_version"`
		}
		err := db.WithContext(ctx).Table("knowledge_chunks AS chunk").Select(`
			chunk.*, source.name AS source_name, source.version AS source_version,
			source.source_type AS source_type, document.title AS document_title,
			document.version AS document_version
		`).Joins("JOIN knowledge_documents document ON document.id = chunk.document_id AND document.status = 'ready'").
			Joins("JOIN knowledge_sources source ON source.id = chunk.source_id AND source.status IN ('ready', 'indexing') AND source.deleted_at IS NULL").
			Where("chunk.id = ? AND chunk.document_id = ? AND chunk.source_id = ?", input.ChunkID, input.DocumentID, input.SourceID).
			Take(&row).Error
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
		result = append(result, aiKnowledgeContextSource{
			SourceID: row.SourceID, SourceName: row.SourceName, SourceVersion: row.SourceVersion,
			SourceType: row.SourceType, DocumentID: row.DocumentID, DocumentTitle: row.DocumentTitle,
			DocumentVersion: row.DocumentVersion, ChunkID: row.ID, ChunkIndex: row.ChunkIndex,
			StartChar: row.StartChar, EndChar: row.EndChar, StartLine: row.StartLine, EndLine: row.EndLine,
			StartPage: row.StartPage, EndPage: row.EndPage,
			Content: row.Content,
		})
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
