package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// clientDeletionImpact is safe for confirmation previews: it contains counts
// only, never contact fields, activity text, attachment names or paths.
type clientDeletionImpact struct {
	Projects        int64
	Activities      int64
	ActorLinks      int64
	Attachments     int64
	AttachmentFiles int64
}

func loadClientDeletionImpact(tx *gorm.DB, id string, expectedVersion int64) (models.Client, clientDeletionImpact, error) {
	var client models.Client
	if err := tx.First(&client, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return client, clientDeletionImpact{}, newProjectRequestError(http.StatusNotFound, "CLIENT_NOT_FOUND", "Client not found")
		}
		return client, clientDeletionImpact{}, err
	}
	if client.Version != expectedVersion {
		return client, clientDeletionImpact{}, clientVersionConflict()
	}
	if client.Status != "inactive" {
		return client, clientDeletionImpact{}, newProjectRequestError(http.StatusConflict, "CLIENT_NOT_INACTIVE", "Only inactive clients can be permanently deleted")
	}

	blockers := []struct {
		table   string
		code    string
		message string
	}{
		{"invoices", "CLIENT_HAS_INVOICES", "Client is referenced by %d invoice(s) and cannot be deleted"},
		{"financial_entries", "CLIENT_HAS_FINANCIAL_ENTRIES", "Client is referenced by %d financial entry record(s) and cannot be deleted"},
		{"client_followups", "CLIENT_HAS_FOLLOWUPS", "Client is referenced by %d follow-up(s) and cannot be deleted"},
	}
	for _, blocker := range blockers {
		var count int64
		if err := tx.Table(blocker.table).Where("client_id = ?", id).Count(&count).Error; err != nil {
			return client, clientDeletionImpact{}, err
		}
		if count > 0 {
			return client, clientDeletionImpact{}, newProjectRequestError(http.StatusConflict, blocker.code, fmt.Sprintf(blocker.message, count))
		}
	}

	impact := clientDeletionImpact{}
	counts := []struct {
		query *gorm.DB
		value *int64
	}{
		{tx.Table("projects").Where("client_id = ?", id), &impact.Projects},
		{tx.Table("client_activities").Where("client_id = ?", id), &impact.Activities},
		{tx.Table("client_actor_links").Where("client_id = ?", id), &impact.ActorLinks},
		{tx.Table("client_attachments").Where("client_id = ?", id), &impact.Attachments},
		{tx.Table("client_attachments").Where("client_id = ? AND deleted_at IS NULL", id), &impact.AttachmentFiles},
	}
	for _, count := range counts {
		if err := count.query.Count(count.value).Error; err != nil {
			return client, clientDeletionImpact{}, err
		}
	}
	return client, impact, nil
}

func (a *API) deleteClientInTransaction(tx *gorm.DB, id string, expectedVersion int64, deletedAt string) (models.Client, clientDeletionImpact, deletedClientResponse, []trashedArtifactFile, error) {
	client, impact, err := loadClientDeletionImpact(tx, id, expectedVersion)
	if err != nil {
		return client, impact, deletedClientResponse{}, nil, err
	}
	moved, err := a.trashClientAttachmentFiles(tx, id, deletedAt)
	if err != nil {
		return client, impact, deletedClientResponse{}, moved, err
	}
	result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.Client{})
	if result.Error != nil {
		return client, impact, deletedClientResponse{}, moved, result.Error
	}
	if result.RowsAffected == 0 {
		return client, impact, deletedClientResponse{}, moved, clientVersionConflict()
	}
	return client, impact, deletedClientResponse{DeletedID: id, DetachedProjects: impact.Projects}, moved, nil
}

func (a *API) finishClientDeletion(clientID string, moved []trashedArtifactFile, err error) {
	if err != nil {
		if restoreErr := a.restoreClientAttachmentFiles(moved); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("Client attachment delete compensation failed client_id=%s error=%v", clientID, restoreErr)
		}
		return
	}
	if a.artifactStore != nil {
		for _, file := range moved {
			a.artifactStore.purgeTrashedFile(file)
		}
	}
}
