package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const aiCitationControlMaxBytes = 4096

var (
	aiCitationBlockPattern = regexp.MustCompile(`(?is)\[opc:citations\](.*?)(?:\[/opc:citations\]|\[opc:citations\])`)
	aiOpenCitationTail     = regexp.MustCompile(`(?is)\[opc:citations\].*$`)
)

type aiCitationControl struct {
	ChunkIDs []string `json:"chunk_ids"`
}

type aiCitationItem struct {
	ChunkID         string `json:"chunk_id"`
	SourceID        string `json:"source_id"`
	SourceName      string `json:"source_name"`
	SourceType      string `json:"source_type"`
	SourceVersion   int64  `json:"source_version"`
	DocumentID      string `json:"document_id"`
	DocumentTitle   string `json:"document_title"`
	DocumentVersion int64  `json:"document_version"`
	ChunkIndex      int    `json:"chunk_index"`
	StartChar       int    `json:"start_char"`
	EndChar         int    `json:"end_char"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
}

type aiCitationSnapshot struct {
	Version int              `json:"version"`
	Status  string           `json:"status"`
	Items   []aiCitationItem `json:"items"`
}

func validateAIResponseCitations(content string, allowed []aiKnowledgeContextSource) (string, *string, aiCitationSnapshot, error) {
	cleaned := stripAICitationBlocks(content)
	if len(allowed) == 0 {
		return cleaned, nil, aiCitationSnapshot{Version: 1, Status: "not_requested", Items: []aiCitationItem{}}, nil
	}
	snapshot := aiCitationSnapshot{Version: 1, Status: "missing", Items: []aiCitationItem{}}
	matches := aiCitationBlockPattern.FindAllStringSubmatch(content, -1)
	if len(matches) != 1 {
		if len(matches) > 1 || strings.Contains(strings.ToLower(content), "[opc:citations]") {
			snapshot.Status = "invalid"
		}
		encoded, err := encodeAICitationSnapshot(snapshot)
		return cleaned, encoded, snapshot, err
	}
	raw := strings.TrimSpace(matches[0][1])
	if len(raw) == 0 || len(raw) > aiCitationControlMaxBytes {
		snapshot.Status = "invalid"
		encoded, err := encodeAICitationSnapshot(snapshot)
		return cleaned, encoded, snapshot, err
	}
	var control aiCitationControl
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&control); err != nil {
		snapshot.Status = "invalid"
		encoded, encodeErr := encodeAICitationSnapshot(snapshot)
		return cleaned, encoded, snapshot, encodeErr
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || control.ChunkIDs == nil || len(control.ChunkIDs) > aiKnowledgeContextMaxChunks {
		snapshot.Status = "invalid"
		encoded, encodeErr := encodeAICitationSnapshot(snapshot)
		return cleaned, encoded, snapshot, encodeErr
	}
	allowedByID := make(map[string]aiKnowledgeContextSource, len(allowed))
	for _, source := range allowed {
		allowedByID[source.ChunkID] = source
	}
	seen := make(map[string]struct{}, len(control.ChunkIDs))
	for _, chunkID := range control.ChunkIDs {
		chunkID = strings.TrimSpace(chunkID)
		parsed, err := uuid.Parse(chunkID)
		source, exists := allowedByID[chunkID]
		if err != nil || parsed.String() != chunkID || !exists {
			snapshot.Status = "invalid"
			snapshot.Items = []aiCitationItem{}
			encoded, encodeErr := encodeAICitationSnapshot(snapshot)
			return cleaned, encoded, snapshot, encodeErr
		}
		if _, duplicate := seen[chunkID]; duplicate {
			snapshot.Status = "invalid"
			snapshot.Items = []aiCitationItem{}
			encoded, encodeErr := encodeAICitationSnapshot(snapshot)
			return cleaned, encoded, snapshot, encodeErr
		}
		seen[chunkID] = struct{}{}
		snapshot.Items = append(snapshot.Items, aiCitationItemFromKnowledge(source))
	}
	if len(snapshot.Items) == 0 {
		snapshot.Status = "no_evidence"
	} else {
		snapshot.Status = "validated"
	}
	encoded, err := encodeAICitationSnapshot(snapshot)
	return cleaned, encoded, snapshot, err
}

func stripAICitationBlocks(content string) string {
	content = aiCitationBlockPattern.ReplaceAllString(content, "")
	return strings.TrimSpace(aiOpenCitationTail.ReplaceAllString(content, ""))
}

func aiCitationItemFromKnowledge(source aiKnowledgeContextSource) aiCitationItem {
	return aiCitationItem{
		ChunkID: source.ChunkID, SourceID: source.SourceID, SourceName: source.SourceName,
		SourceType: source.SourceType, SourceVersion: source.SourceVersion,
		DocumentID: source.DocumentID, DocumentTitle: source.DocumentTitle, DocumentVersion: source.DocumentVersion,
		ChunkIndex: source.ChunkIndex, StartChar: source.StartChar, EndChar: source.EndChar,
		StartLine: source.StartLine, EndLine: source.EndLine,
	}
}

func encodeAICitationSnapshot(snapshot aiCitationSnapshot) (*string, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func decodeAICitationSnapshot(value *string) (string, []aiCitationItem, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "not_requested", []aiCitationItem{}, nil
	}
	var snapshot aiCitationSnapshot
	decoder := json.NewDecoder(strings.NewReader(*value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.Version != 1 || snapshot.Items == nil {
		return "", nil, errors.New("invalid persisted AI citation snapshot")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", nil, errors.New("invalid persisted AI citation snapshot")
	}
	if snapshot.Status != "validated" && snapshot.Status != "no_evidence" && snapshot.Status != "missing" && snapshot.Status != "invalid" {
		return "", nil, errors.New("invalid persisted AI citation status")
	}
	if len(snapshot.Items) > aiKnowledgeContextMaxChunks || (snapshot.Status == "validated") != (len(snapshot.Items) > 0) {
		return "", nil, errors.New("invalid persisted AI citation items")
	}
	seen := make(map[string]struct{}, len(snapshot.Items))
	for _, item := range snapshot.Items {
		for _, id := range []string{item.ChunkID, item.SourceID, item.DocumentID} {
			parsed, err := uuid.Parse(id)
			if err != nil || parsed.String() != id {
				return "", nil, errors.New("invalid persisted AI citation identity")
			}
		}
		if _, duplicate := seen[item.ChunkID]; duplicate || strings.TrimSpace(item.SourceName) == "" ||
			(item.SourceType != "text" && item.SourceType != "markdown") || item.SourceVersion < 1 ||
			strings.TrimSpace(item.DocumentTitle) == "" || item.DocumentVersion < 1 || item.ChunkIndex < 0 ||
			item.StartChar < 0 || item.EndChar <= item.StartChar || item.StartLine < 1 || item.EndLine < item.StartLine {
			return "", nil, errors.New("invalid persisted AI citation item")
		}
		seen[item.ChunkID] = struct{}{}
	}
	return snapshot.Status, snapshot.Items, nil
}
