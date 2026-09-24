package api

import (
	"errors"
	"unicode/utf8"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func validateFinancialVoidReason(reason string) error {
	if n := utf8.RuneCountInString(reason); n < 1 || n > 1000 {
		return newFinancialEntryRequestError(422, "VALIDATION_ERROR", "reason must contain 1 to 1000 characters")
	}
	return nil
}

// Shared by the human API and AI approval inside the caller's transaction.
func createFinancialEntryInTransaction(tx *gorm.DB, entry models.FinancialEntry, requestID string) (financialEntryResponse, error) {
	if err := normalizeFinancialEntryAssociations(tx, &entry); err != nil {
		return financialEntryResponse{}, err
	}
	if err := tx.Create(&entry).Error; err != nil {
		return financialEntryResponse{}, err
	}
	if err := recordFinancialEntryWorkflowEvent(tx, entry, "financial_entry_created", nil, requestID); err != nil {
		return financialEntryResponse{}, err
	}
	row, err := loadFinancialEntryRow(tx, entry.ID)
	return financialEntryResponseFromRow(row), err
}

func prepareFinancialEntryChange(tx *gorm.DB, id string, version int64) (models.FinancialEntry, error) {
	var entry models.FinancialEntry
	if err := tx.Where("id = ?", id).Take(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return entry, newFinancialEntryRequestError(404, "FINANCIAL_ENTRY_NOT_FOUND", "Financial entry not found")
		}
		return entry, err
	}
	if entry.Version != version {
		return entry, financialEntryVersionConflict()
	}
	if entry.InvoiceID != nil {
		return entry, newFinancialEntryRequestError(409, "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE", "Invoice-linked financial entries can only be changed through the invoice workflow")
	}
	if entry.Status == "voided" {
		return entry, newFinancialEntryRequestError(409, "FINANCIAL_ENTRY_VOIDED", "A voided financial entry cannot be changed")
	}
	return entry, nil
}

func updateFinancialEntryInTransaction(tx *gorm.DB, id string, version int64, input updateFinancialEntryRequest, requestID, now string) (financialEntryResponse, error) {
	entry, err := prepareFinancialEntryChange(tx, id, version)
	if err != nil {
		return financialEntryResponse{}, err
	}
	updates, err := financialEntryUpdates(tx, entry, input)
	if err != nil {
		return financialEntryResponse{}, err
	}
	return changeFinancialEntryInTransaction(tx, entry, updates, "financial_entry_updated", requestID, now)
}

func voidFinancialEntryInTransaction(tx *gorm.DB, id string, version int64, reason, requestID, now string) (financialEntryResponse, error) {
	entry, err := prepareFinancialEntryChange(tx, id, version)
	if err != nil {
		return financialEntryResponse{}, err
	}
	if err := validateFinancialVoidReason(reason); err != nil {
		return financialEntryResponse{}, err
	}
	return changeFinancialEntryInTransaction(tx, entry, map[string]any{
		"status": "voided", "voided_at": now, "voided_by_actor_id": models.BuiltinOwnerActorID, "void_reason": reason,
	}, "financial_entry_voided", requestID, now)
}

func changeFinancialEntryInTransaction(tx *gorm.DB, entry models.FinancialEntry, updates map[string]any, action, requestID, now string) (financialEntryResponse, error) {
	previous := financialEntryEventState(entry)
	updates["version"], updates["updated_at"] = gorm.Expr("version + 1"), now
	result := tx.Model(&models.FinancialEntry{}).Where("id = ? AND version = ?", entry.ID, entry.Version).Updates(updates)
	if result.Error != nil {
		return financialEntryResponse{}, result.Error
	}
	if result.RowsAffected != 1 {
		return financialEntryResponse{}, financialEntryVersionConflict()
	}
	if err := tx.Where("id = ?", entry.ID).Take(&entry).Error; err != nil {
		return financialEntryResponse{}, err
	}
	if err := recordFinancialEntryWorkflowEvent(tx, entry, action, previous, requestID); err != nil {
		return financialEntryResponse{}, err
	}
	row, err := loadFinancialEntryRow(tx, entry.ID)
	return financialEntryResponseFromRow(row), err
}
