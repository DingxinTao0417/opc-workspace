package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

var errKnowledgeJobNoLongerRunnable = errors.New("knowledge index job is no longer runnable")

// knowledgeIndexer is a single-mailbox local actor. It serializes CPU and
// SQLite-heavy indexing work while HTTP handlers only validate, persist a
// queued command, and enqueue its immutable job identity.
type knowledgeIndexer struct {
	api       *API
	mailbox   chan string
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	stageHook func(jobID, stage string)
}

func newKnowledgeIndexer(api *API) *knowledgeIndexer {
	indexer := &knowledgeIndexer{
		api: api, mailbox: make(chan string, 64), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go indexer.run()
	return indexer
}

func (i *knowledgeIndexer) enqueue(ctx context.Context, jobID string) bool {
	if i == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-i.stop:
		return false
	default:
	}
	select {
	case <-ctx.Done():
		return false
	case <-i.stop:
		return false
	case i.mailbox <- jobID:
		return true
	default:
		return false
	}
}

func (i *knowledgeIndexer) close() {
	if i == nil {
		return
	}
	i.closeOnce.Do(func() {
		close(i.stop)
		<-i.done
	})
}

func (i *knowledgeIndexer) run() {
	defer close(i.done)
	for {
		select {
		case <-i.stop:
			return
		case jobID := <-i.mailbox:
			i.api.runKnowledgeIndexJob(jobID)
		}
	}
}

func recoverKnowledgeIndexJobsOnStartup(db *gorm.DB, now time.Time) error {
	completedAt := now.UTC().Format(time.RFC3339Nano)
	return db.Transaction(func(tx *gorm.DB) error {
		var sourceIDs []string
		if err := tx.Model(&models.KnowledgeIndexJob{}).
			Where("status IN ?", []string{"queued", "running"}).
			Distinct("source_id").Pluck("source_id", &sourceIDs).Error; err != nil {
			return err
		}
		if len(sourceIDs) == 0 {
			return nil
		}
		if err := tx.Model(&models.KnowledgeIndexJob{}).
			Where("status IN ?", []string{"queued", "running"}).Updates(map[string]any{
			"status": "failed", "stage": "complete", "error_code": "KNOWLEDGE_INDEX_INTERRUPTED",
			"completed_at": completedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE knowledge_sources
			SET status = CASE
					WHEN EXISTS (SELECT 1 FROM knowledge_documents d WHERE d.source_id = knowledge_sources.id)
					THEN 'ready' ELSE 'failed' END,
				version = version + 1,
				updated_at = ?
			WHERE id IN ? AND status = 'indexing' AND deleted_at IS NULL
		`, completedAt, sourceIDs).Error
	})
}

func (a *API) runKnowledgeIndexJob(jobID string) {
	a.maintenance.RLock()
	defer a.maintenance.RUnlock()
	now := a.options.Now().UTC().Format(time.RFC3339Nano)
	claimed := a.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.KnowledgeIndexJob{}).
			Where("id = ? AND status = 'queued' AND cancel_requested = 0", jobID).
			Updates(map[string]any{"status": "running", "stage": "extracting", "progress": 10, "started_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errKnowledgeJobNoLongerRunnable
		}
		return nil
	})
	if claimed != nil {
		return
	}
	if a.knowledgeIndexer != nil && a.knowledgeIndexer.stageHook != nil {
		a.knowledgeIndexer.stageHook(jobID, "extracting")
	}

	var job models.KnowledgeIndexJob
	var source models.KnowledgeSource
	if err := a.db.First(&job, "id = ?", jobID).Error; err != nil {
		return
	}
	if err := a.db.First(&source, "id = ? AND deleted_at IS NULL", job.SourceID).Error; err != nil {
		a.failKnowledgeIndexJob(jobID, "KNOWLEDGE_SOURCE_UNAVAILABLE")
		return
	}
	text, pages, err := extractKnowledgeSource(source)
	if err != nil {
		a.failKnowledgeIndexJob(jobID, knowledgeIndexFailureCode(err))
		return
	}
	if !a.advanceKnowledgeIndexJob(jobID, "chunking", 40) {
		return
	}
	if a.knowledgeIndexer != nil && a.knowledgeIndexer.stageHook != nil {
		a.knowledgeIndexer.stageHook(jobID, "chunking")
	}
	chunks := splitKnowledgeText(text)
	for index := range chunks {
		chunks[index].StartPage, chunks[index].EndPage = knowledgePagesForLines(pages, chunks[index].StartLine, chunks[index].EndLine)
	}
	if len(chunks) == 0 {
		a.failKnowledgeIndexJob(jobID, "KNOWLEDGE_EMPTY_SOURCE")
		return
	}
	extractorVersion := knowledgeExtractorVersion
	if source.SourceType == "pdf" {
		extractorVersion = knowledgePDFExtractorVersion
	}
	if !a.advanceKnowledgeIndexJob(jobID, "indexing", 75) {
		return
	}
	if a.knowledgeIndexer != nil && a.knowledgeIndexer.stageHook != nil {
		a.knowledgeIndexer.stageHook(jobID, "indexing")
	}
	completedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var currentJob models.KnowledgeIndexJob
		if err := tx.First(&currentJob, "id = ?", jobID).Error; err != nil {
			return err
		}
		if currentJob.Status != "running" || currentJob.CancelRequested {
			return errKnowledgeJobNoLongerRunnable
		}
		var currentSource models.KnowledgeSource
		if err := tx.First(&currentSource, "id = ? AND deleted_at IS NULL", job.SourceID).Error; err != nil {
			return err
		}
		if currentSource.Status != "indexing" || currentSource.ContentSHA256 != source.ContentSHA256 {
			return errKnowledgeJobNoLongerRunnable
		}
		var document models.KnowledgeDocument
		documentErr := tx.First(&document, "source_id = ?", source.ID).Error
		if documentErr != nil && !errors.Is(documentErr, gorm.ErrRecordNotFound) {
			return documentErr
		}
		if errors.Is(documentErr, gorm.ErrRecordNotFound) {
			document = models.KnowledgeDocument{
				ID: uuid.NewString(), SourceID: source.ID, Title: source.Title, Language: detectKnowledgeLanguage(text),
				ExtractorVersion: extractorVersion, ContentText: text,
				ContentSHA256: knowledgeSHA256([]byte(text)), Status: "ready", Version: 1,
				CreatedAt: completedAt, UpdatedAt: completedAt,
			}
			if err := tx.Create(&document).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("document_id = ?", document.ID).Delete(&models.KnowledgeChunk{}).Error; err != nil {
				return err
			}
			document.Version++
			if err := tx.Model(&models.KnowledgeDocument{}).Where("id = ?", document.ID).Updates(map[string]any{
				"language": detectKnowledgeLanguage(text), "extractor_version": extractorVersion,
				"content_text": text, "content_sha256": knowledgeSHA256([]byte(text)),
				"status": "ready", "version": document.Version, "updated_at": completedAt,
			}).Error; err != nil {
				return err
			}
		}
		if err := insertKnowledgeChunks(tx, source.ID, document.ID, document.Version, chunks, completedAt); err != nil {
			return err
		}
		if result := tx.Model(&models.KnowledgeSource{}).
			Where("id = ? AND version = ? AND status = 'indexing'", source.ID, currentSource.Version).
			Updates(map[string]any{
				"status": "ready", "last_indexed_at": completedAt,
				"version": currentSource.Version + 1, "updated_at": completedAt,
			}); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return errKnowledgeJobNoLongerRunnable
		}
		result := tx.Model(&models.KnowledgeIndexJob{}).
			Where("id = ? AND status = 'running' AND cancel_requested = 0", jobID).
			Updates(map[string]any{
				"status": "succeeded", "stage": "complete", "progress": 100,
				"completed_at": completedAt, "error_code": nil,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errKnowledgeJobNoLongerRunnable
		}
		return nil
	})
	if err != nil && !errors.Is(err, errKnowledgeJobNoLongerRunnable) {
		a.failKnowledgeIndexJob(jobID, "KNOWLEDGE_INDEX_FAILED")
	}
}

func (a *API) advanceKnowledgeIndexJob(jobID, stage string, progress int) bool {
	result := a.db.Model(&models.KnowledgeIndexJob{}).
		Where("id = ? AND status = 'running' AND cancel_requested = 0", jobID).
		Updates(map[string]any{"stage": stage, "progress": progress})
	return result.Error == nil && result.RowsAffected == 1
}

func (a *API) failKnowledgeIndexJob(jobID, code string) {
	completedAt := a.options.Now().UTC().Format(time.RFC3339Nano)
	_ = a.db.Transaction(func(tx *gorm.DB) error {
		var job models.KnowledgeIndexJob
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != "queued" && job.Status != "running" {
			return nil
		}
		if err := tx.Model(&models.KnowledgeIndexJob{}).Where("id = ?", jobID).Updates(map[string]any{
			"status": "failed", "stage": "complete", "error_code": code, "completed_at": completedAt,
		}).Error; err != nil {
			return err
		}
		var documentCount int64
		if err := tx.Model(&models.KnowledgeDocument{}).Where("source_id = ?", job.SourceID).Count(&documentCount).Error; err != nil {
			return err
		}
		status := "failed"
		if documentCount > 0 {
			status = "ready"
		}
		return tx.Model(&models.KnowledgeSource{}).
			Where("id = ? AND status = 'indexing' AND deleted_at IS NULL", job.SourceID).
			Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1"), "updated_at": completedAt}).Error
	})
}

func knowledgeIndexFailureCode(err error) string {
	var requestErr *projectRequestError
	if errors.As(err, &requestErr) && requestErr.code != "" {
		return requestErr.code
	}
	return "KNOWLEDGE_INDEX_FAILED"
}
