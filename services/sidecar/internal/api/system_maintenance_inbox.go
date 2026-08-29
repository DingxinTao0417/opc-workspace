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

type systemMaintenanceIncident struct {
	component   string
	operation   string
	failureCode string
	title       string
	message     string
}

const systemMaintenanceBackupComponent = "backup"

var (
	backupCreateMaintenanceIncident = systemMaintenanceIncident{
		component:   systemMaintenanceBackupComponent,
		operation:   "create",
		failureCode: "backup_create_failed",
		title:       "本地备份需要处理",
		message:     "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
	}
	backupVerifyMaintenanceIncident = systemMaintenanceIncident{
		component:   systemMaintenanceBackupComponent,
		operation:   "verify",
		failureCode: "backup_verify_failed",
		title:       "本地备份校验需要处理",
		message:     "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
	}
	backupDrillMaintenanceIncident = systemMaintenanceIncident{
		component:   systemMaintenanceBackupComponent,
		operation:   "drill",
		failureCode: "backup_drill_failed",
		title:       "本地备份恢复演练需要处理",
		message:     "无法在隔离环境中完成本地备份恢复演练。现有工作区数据没有被修改。请检查备份状态后重试。",
	}
	backupRestoreMaintenanceIncident = systemMaintenanceIncident{
		component:   systemMaintenanceBackupComponent,
		operation:   "restore",
		failureCode: "backup_restore_failed",
		title:       "本地备份恢复需要处理",
		message:     "无法安全安排本地备份恢复。现有工作区数据没有被修改。请检查本地存储后重试。",
	}
)

func allowedSystemMaintenanceIncident(incident systemMaintenanceIncident) bool {
	switch incident {
	case backupCreateMaintenanceIncident, backupVerifyMaintenanceIncident,
		backupDrillMaintenanceIncident, backupRestoreMaintenanceIncident:
		return true
	default:
		return false
	}
}

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
func (a *API) projectSystemMaintenanceFailure(incident systemMaintenanceIncident, requestID string) error {
	if !allowedSystemMaintenanceIncident(incident) {
		return errors.New("unsupported system maintenance incident")
	}
	sourceID := systemMaintenanceSourceID(incident.component, incident.operation)
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
			"component": incident.component, "operation": incident.operation,
			"failure_code": incident.failureCode, "occurred_at": now, "message": incident.message,
		})
		if err != nil {
			return err
		}
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: incident.title, Summary: incident.message,
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
	return a.projectSystemMaintenanceFailure(backupCreateMaintenanceIncident, requestID)
}

func (a *API) projectBackupVerifyFailure(requestID string) error {
	return a.projectSystemMaintenanceFailure(backupVerifyMaintenanceIncident, requestID)
}

func (a *API) projectBackupDrillFailure(requestID string) error {
	return a.projectSystemMaintenanceFailure(backupDrillMaintenanceIncident, requestID)
}

func (a *API) projectBackupRestoreFailure(requestID string) error {
	return a.projectSystemMaintenanceFailure(backupRestoreMaintenanceIncident, requestID)
}
