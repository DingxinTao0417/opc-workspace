package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const systemMaintenanceInboxSourceType = "system_maintenance"

const (
	systemMaintenanceBackupComponent     = "backup"
	systemMaintenanceCreateOperation     = "create"
	systemMaintenanceBackupCreateCode    = "backup_create_failed"
	systemMaintenanceBackupCreateTitle   = "本地备份需要处理"
	systemMaintenanceBackupCreateMessage = "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。"
)

func systemMaintenanceSourceID(component, operation string) string {
	return component + ":" + operation
}

func systemMaintenanceEventKey(sourceID, incidentID string) string {
	return fmt.Sprintf("system:%s:%s", sourceID, incidentID)
}

// projectSystemMaintenanceFailure records a single active, safe-to-display
// maintenance incident per component/operation. It deliberately does not retain
// the underlying error because that can contain local paths or other sensitive
// implementation details. Resolving or dismissing the Inbox Item closes the
// incident; a later failure then opens a new one.
func (a *API) projectSystemMaintenanceFailure(
	component string,
	operation string,
	failureCode string,
	title string,
	message string,
	requestID string,
) error {
	if component != systemMaintenanceBackupComponent || operation != systemMaintenanceCreateOperation ||
		failureCode != systemMaintenanceBackupCreateCode || title != systemMaintenanceBackupCreateTitle ||
		message != systemMaintenanceBackupCreateMessage {
		return errors.New("unsupported system maintenance incident")
	}
	sourceID := systemMaintenanceSourceID(component, operation)
	now := formatInboxTimestamp(a.options.Now())
	return a.db.Transaction(func(tx *gorm.DB) error {
		var existing models.InboxItem
		err := tx.Where(
			"source_entity_type = ? AND source_entity_id = ? AND status IN ('open', 'tracking') AND source_deleted_at IS NULL",
			systemMaintenanceInboxSourceType,
			sourceID,
		).Order("created_at ASC").First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		payloadJSON, err := json.Marshal(map[string]any{
			"component": component, "operation": operation,
			"failure_code": failureCode, "occurred_at": now, "message": message,
		})
		if err != nil {
			return err
		}
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: title, Summary: message,
			SourceEntityType: systemMaintenanceInboxSourceType,
			SourceEntityID:   &sourceID,
			Priority:         "P1", Status: "open", ResolutionPolicy: "manual",
			PayloadJSON: string(payloadJSON), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		incidentID := uuid.NewString()
		eventKey := systemMaintenanceEventKey(sourceID, incidentID)
		item.SourceEventKey = &eventKey
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("create system maintenance Inbox Item: %w", err)
		}
		return recordInboxWorkflowEventAs(
			tx, item.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(item, ""), requestID, now,
		)
	})
}

func (a *API) projectBackupCreateFailure(requestID string) error {
	return a.projectSystemMaintenanceFailure(
		systemMaintenanceBackupComponent,
		systemMaintenanceCreateOperation,
		systemMaintenanceBackupCreateCode,
		systemMaintenanceBackupCreateTitle,
		systemMaintenanceBackupCreateMessage,
		requestID,
	)
}
