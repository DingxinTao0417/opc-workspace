package api

import (
	"errors"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// Manual HTTP commands and human-approved AI drafts share these transactions.
// The owner is the confirming local user; models cannot choose actor/source IDs.
func createClientActivityInTransaction(tx *gorm.DB, activity models.ClientActivity) (clientActivityResponse, error) {
	var count int64
	if err := tx.Model(&models.Client{}).Where("id=?", activity.ClientID).Count(&count).Error; err != nil {
		return clientActivityResponse{}, err
	}
	if count == 0 {
		return clientActivityResponse{}, newProjectRequestError(404, "CLIENT_NOT_FOUND", "Client not found")
	}
	if err := tx.Create(&activity).Error; err != nil {
		return clientActivityResponse{}, err
	}
	row, err := loadClientActivityRow(tx, activity.ID)
	return clientActivityResponseFromRow(row), err
}

func prepareClientActivityChange(tx *gorm.DB, id string, expected int64) (clientActivityRow, error) {
	row, err := loadClientActivityRow(tx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, newProjectRequestError(404, "CLIENT_ACTIVITY_NOT_FOUND", "Client activity not found")
	}
	if err != nil {
		return row, err
	}
	if row.Version != expected {
		return row, clientActivityVersionConflict()
	}
	if row.DeletedAt != nil {
		return row, newProjectRequestError(409, "CLIENT_ACTIVITY_DELETED", "Deleted client activities cannot be changed")
	}
	if row.Kind == "system_reference" {
		return row, newProjectRequestError(409, "CLIENT_ACTIVITY_READ_ONLY", "System reference activities are read-only")
	}
	return row, nil
}

func changeClientActivityInTransaction(tx *gorm.DB, id string, expected int64, patch map[string]any, now string) (clientActivityResponse, error) {
	if _, err := prepareClientActivityChange(tx, id, expected); err != nil {
		return clientActivityResponse{}, err
	}
	updates := make(map[string]any, len(patch)+2)
	for key, value := range patch {
		updates[key] = value
	}
	updates["updated_at"], updates["version"] = now, gorm.Expr("version + 1")
	result := tx.Model(&models.ClientActivity{}).Where("id=? AND version=? AND deleted_at IS NULL", id, expected).Updates(updates)
	if result.Error != nil {
		return clientActivityResponse{}, result.Error
	}
	if result.RowsAffected != 1 {
		return clientActivityResponse{}, clientActivityVersionConflict()
	}
	row, err := loadClientActivityRow(tx, id)
	return clientActivityResponseFromRow(row), err
}
