package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A bounded, exact chunk read. Never substitute a new document/chunk for an
// expired citation, and never load the full source or its original bytes.
type knowledgeChunk struct {
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

func readKnowledgeChunk(ctx context.Context, db *gorm.DB, sourceID, documentID, chunkID string) (knowledgeChunk, error) {
	var row knowledgeChunk
	err := db.WithContext(ctx).Table("knowledge_chunks AS chunk").Select(`
		chunk.id AS chunk_id, chunk.source_id, chunk.document_id, chunk.chunk_index,
		chunk.start_char, chunk.end_char, chunk.start_line, chunk.end_line,
		chunk.start_page, chunk.end_page, chunk.content,
		source.name AS source_name, source.version AS source_version,
		source.source_type, document.title AS document_title, document.version AS document_version
	`).Joins("JOIN knowledge_documents document ON document.id = chunk.document_id AND document.source_id = chunk.source_id AND document.status = 'ready'").
		Joins("JOIN knowledge_sources source ON source.id = chunk.source_id AND source.status IN ('ready', 'indexing') AND source.deleted_at IS NULL").
		Where("chunk.id = ? AND chunk.document_id = ? AND chunk.source_id = ?", chunkID, documentID, sourceID).
		Take(&row).Error
	return row, err
}

func (a *API) getKnowledgeChunk(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	chunkID, ok := knowledgeID(c, "id", "INVALID_KNOWLEDGE_CHUNK_ID")
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	// All identities and versions are required; duplicate or extra parameters
	// must not silently change which evidence a saved location addresses.
	if len(query) != 4 {
		writeError(c, http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION", "An exact source, document and version location is required")
		return
	}
	for _, name := range []string{"source_id", "document_id", "source_version", "document_version"} {
		if len(query[name]) != 1 {
			writeError(c, http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION", "Location parameters must appear exactly once")
			return
		}
	}
	for _, name := range []string{"source_id", "document_id"} {
		id, err := uuid.Parse(query.Get(name))
		if err != nil || id.String() != query.Get(name) {
			writeError(c, http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION", "Location identities must be canonical UUIDs")
			return
		}
	}
	sourceVersion, errSource := strconv.ParseInt(query.Get("source_version"), 10, 64)
	documentVersion, errDocument := strconv.ParseInt(query.Get("document_version"), 10, 64)
	if errSource != nil || errDocument != nil || sourceVersion < 1 || documentVersion < 1 ||
		strconv.FormatInt(sourceVersion, 10) != query.Get("source_version") || strconv.FormatInt(documentVersion, 10) != query.Get("document_version") {
		writeError(c, http.StatusUnprocessableEntity, "INVALID_KNOWLEDGE_LOCATION", "Location versions must be positive integers")
		return
	}
	row, err := readKnowledgeChunk(c.Request.Context(), a.db, query.Get("source_id"), query.Get("document_id"), chunkID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusNotFound, "KNOWLEDGE_CHUNK_NOT_FOUND", "The referenced knowledge chunk is no longer available")
		return
	}
	if err != nil {
		writeDatabaseError(c)
		return
	}
	if row.SourceVersion != sourceVersion || row.DocumentVersion != documentVersion {
		writeError(c, http.StatusConflict, "KNOWLEDGE_LOCATION_CHANGED", "The knowledge location has changed; the original evidence cannot be verified")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": row})
}
