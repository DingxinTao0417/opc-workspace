package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// projectDeletionImpact is a metadata-only snapshot shared by the native
// Project delete endpoint and AI approval previews. It intentionally excludes
// note bodies, attachment names/paths and financial contents.
type projectDeletionImpact struct {
	Tasks                  int64
	DraftInvoices          int64
	FinancialEntries       int64
	Notes                  int64
	Attachments            int64
	AttachmentFiles        int64
	CompletionInboxSources int64
}

func loadProjectDeletionImpact(tx *gorm.DB, id string, expectedVersion int64) (models.Project, projectDeletionImpact, error) {
	var project models.Project
	if err := tx.First(&project, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return project, projectDeletionImpact{}, newProjectRequestError(http.StatusNotFound, "PROJECT_NOT_FOUND", "Project not found")
		}
		return project, projectDeletionImpact{}, err
	}
	if project.Version != expectedVersion {
		return project, projectDeletionImpact{}, projectVersionConflict()
	}
	if project.Status != "archived" {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, "PROJECT_NOT_ARCHIVED", "Only archived projects can be permanently deleted")
	}

	var roadmapMilestoneCount int64
	if err := tx.Table("roadmap_milestone_projects").Where("project_id = ?", id).Count(&roadmapMilestoneCount).Error; err != nil {
		return project, projectDeletionImpact{}, err
	}
	if roadmapMilestoneCount > 0 {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, "PROJECT_ROADMAP_MILESTONES_EXIST", "Remove the project's roadmap milestone associations before permanently deleting it")
	}
	var contentItemCount int64
	if err := tx.Table("content_items").Where("project_id = ?", id).Count(&contentItemCount).Error; err != nil {
		return project, projectDeletionImpact{}, err
	}
	if contentItemCount > 0 {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, "PROJECT_CONTENT_ITEMS_EXIST", "Remove the project's content item associations before permanently deleting it")
	}
	var protectedInvoiceCount int64
	if err := tx.Table("invoices").Where("project_id = ? AND status <> 'draft'", id).Count(&protectedInvoiceCount).Error; err != nil {
		return project, projectDeletionImpact{}, err
	}
	if protectedInvoiceCount > 0 {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, "PROJECT_HAS_INVOICES", fmt.Sprintf("Project is referenced by %d invoice(s) and cannot be deleted", protectedInvoiceCount))
	}
	var protectedEntryCount int64
	if err := tx.Table("financial_entries").Where("project_id = ? AND (status = 'voided' OR invoice_id IS NOT NULL)", id).Count(&protectedEntryCount).Error; err != nil {
		return project, projectDeletionImpact{}, err
	}
	if protectedEntryCount > 0 {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, "PROJECT_HAS_FINANCIAL_ENTRIES", fmt.Sprintf("Project is referenced by %d financial entry record(s) and cannot be deleted", protectedEntryCount))
	}
	referencesActiveAttachment, err := activeAgentRunReferencesProjectAttachment(tx, id)
	if err != nil {
		return project, projectDeletionImpact{}, err
	}
	if referencesActiveAttachment {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, agentRunFileReferencedByActiveRunCode, "Wait for or cancel the active Agent Run before permanently deleting this Project and its input attachments")
	}

	referencesProjectTask, err := activeAgentRunReferencesProjectTaskSource(tx, "", id)
	if err != nil {
		return project, projectDeletionImpact{}, err
	}
	if referencesProjectTask {
		return project, projectDeletionImpact{}, newProjectRequestError(http.StatusConflict, agentRunFileReferencedByActiveRunCode, "Wait for or cancel the successor Agent Run using this Project's accepted Task files before deleting the Project")
	}
	impact := projectDeletionImpact{}
	counts := []struct {
		query *gorm.DB
		value *int64
	}{
		{tx.Table("tasks").Where("project_id = ?", id), &impact.Tasks},
		{tx.Table("invoices").Where("project_id = ? AND status = 'draft'", id), &impact.DraftInvoices},
		{tx.Table("financial_entries").Where("project_id = ? AND invoice_id IS NULL AND status IN ('pending', 'confirmed')", id), &impact.FinancialEntries},
		{tx.Table("project_notes").Where("project_id = ?", id), &impact.Notes},
		{tx.Table("project_attachments").Where("project_id = ?", id), &impact.Attachments},
		{tx.Table("project_attachments").Where("project_id = ? AND deleted_at IS NULL", id), &impact.AttachmentFiles},
		{tx.Table("inbox_items").Where("source_entity_type = ? AND source_entity_id = ?", projectCompletionInboxSourceType, id), &impact.CompletionInboxSources},
	}
	for _, count := range counts {
		if err := count.query.Count(count.value).Error; err != nil {
			return project, projectDeletionImpact{}, err
		}
	}
	return project, impact, nil
}

func (a *API) deleteProjectInTransaction(tx *gorm.DB, id string, expectedVersion int64, requestID, deletedAt string) (models.Project, projectDeletionImpact, deletedProjectResponse, []trashedArtifactFile, error) {
	project, impact, err := loadProjectDeletionImpact(tx, id, expectedVersion)
	if err != nil {
		return project, impact, deletedProjectResponse{}, nil, err
	}
	if err := coordinateProjectCompletionInboxSourceDeletion(tx, id, requestID, deletedAt); err != nil {
		return project, impact, deletedProjectResponse{}, nil, err
	}
	moved, err := a.trashProjectAttachmentFiles(tx, id, deletedAt)
	if err != nil {
		return project, impact, deletedProjectResponse{}, moved, err
	}
	detachedTasks, err := bumpTasksForProject(tx, id, deletedAt)
	if err != nil {
		return project, impact, deletedProjectResponse{}, moved, err
	}
	if err := recordProjectWorkflowEvent(tx, id, "project_deleted", projectEventState(project), nil, requestID, deletedAt); err != nil {
		return project, impact, deletedProjectResponse{}, moved, err
	}
	result := tx.Where("id = ? AND version = ?", id, expectedVersion).Delete(&models.Project{})
	if result.Error != nil {
		return project, impact, deletedProjectResponse{}, moved, result.Error
	}
	if result.RowsAffected == 0 {
		return project, impact, deletedProjectResponse{}, moved, projectVersionConflict()
	}
	deleted := deletedProjectResponse{
		DeletedID:                id,
		DetachedTasks:            detachedTasks,
		DetachedInvoices:         impact.DraftInvoices,
		DetachedFinancialEntries: impact.FinancialEntries,
	}
	return project, impact, deleted, moved, nil
}

func (a *API) finishProjectDeletion(projectID string, moved []trashedArtifactFile, err error) {
	if err != nil {
		if restoreErr := a.restoreProjectAttachmentFiles(moved); restoreErr != nil && a.options.Logger != nil {
			a.options.Logger.Printf("restore project attachment files after delete rollback failed project_id=%s error=%v", projectID, restoreErr)
		}
		return
	}
	if a.artifactStore != nil {
		for _, file := range moved {
			a.artifactStore.purgeTrashedFile(file)
		}
	}
}
