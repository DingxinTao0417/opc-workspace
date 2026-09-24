package api

import (
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The caller owns the transaction: native idempotency or AI approval commits
// atomically with the note and the existing Project aggregate-version trigger.
func createProjectNoteInTransaction(tx *gorm.DB, note models.ProjectNote) (projectNoteResponse, error) {
	if err := requireMutableProject(tx, note.ProjectID); err != nil {
		return projectNoteResponse{}, err
	}
	if err := tx.Create(&note).Error; err != nil {
		return projectNoteResponse{}, err
	}
	row, err := loadProjectNoteRow(tx, note.ID)
	return projectNoteResponseFromRow(row), err
}

func prepareProjectNoteChange(tx *gorm.DB, id string, version int64) (models.ProjectNote, error) {
	var note models.ProjectNote
	if err := tx.First(&note, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return note, newProjectRequestError(404, "PROJECT_NOTE_NOT_FOUND", "Project note not found")
		}
		return note, err
	}
	if note.Version != version {
		return note, projectNoteVersionConflict()
	}
	if note.DeletedAt != nil {
		return note, newProjectRequestError(409, "PROJECT_NOTE_DELETED", "Deleted project notes cannot be changed")
	}
	return note, requireMutableProject(tx, note.ProjectID)
}

func changeProjectNoteInTransaction(tx *gorm.DB, id string, version int64, patch map[string]any, now string) (projectNoteResponse, error) {
	if _, err := prepareProjectNoteChange(tx, id, version); err != nil {
		return projectNoteResponse{}, err
	}
	updates := make(map[string]any, len(patch)+2)
	for key, value := range patch {
		updates[key] = value
	}
	updates["updated_at"], updates["version"] = now, gorm.Expr("version + 1")
	result := tx.Model(&models.ProjectNote{}).Where("id = ? AND version = ? AND deleted_at IS NULL", id, version).Updates(updates)
	if result.Error != nil {
		return projectNoteResponse{}, result.Error
	}
	if result.RowsAffected != 1 {
		return projectNoteResponse{}, projectNoteVersionConflict()
	}
	row, err := loadProjectNoteRow(tx, id)
	return projectNoteResponseFromRow(row), err
}
