package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	databaseStartupMaintenanceIncident = systemMaintenanceIncident{
		component:   "database",
		operation:   "startup",
		failureCode: "database_startup_failed",
		title:       "本地数据库启动需要处理",
		message:     "上次启动未能安全打开本地数据库。工作区没有进入就绪状态；请检查本地存储和应用日志。",
	}
	databaseMigrationMaintenanceIncident = systemMaintenanceIncident{
		component:   "database",
		operation:   "migration",
		failureCode: "database_migration_failed",
		title:       "本地数据库迁移需要处理",
		message:     "上次启动未能完成受保护的数据库迁移。已有数据未被新版本继续使用；请检查回滚备份和应用日志。",
	}
	sidecarStartupMaintenanceIncident = systemMaintenanceIncident{
		component:   "sidecar",
		operation:   "startup",
		failureCode: "sidecar_startup_failed",
		title:       "本地服务启动需要处理",
		message:     "上次本地服务启动未能进入就绪状态。请检查应用日志后重新启动。",
	}
	runtimeDatabaseMaintenanceIncident = systemMaintenanceIncident{
		component:   "database",
		operation:   "runtime",
		failureCode: "database_runtime_failed",
		title:       "本地数据库运行需要处理",
		message:     "运行中的本地数据库操作失败。请检查可用磁盘空间和应用日志，并在继续重要写入前创建或校验备份。",
	}
	storageLowSpaceMaintenanceIncident = systemMaintenanceIncident{
		component:   "storage",
		operation:   "low_space",
		failureCode: "storage_low_space",
		title:       "本地存储空间不足",
		message:     "本地数据或备份所在磁盘的可用空间已低于设置阈值。请释放空间，并在继续重要写入前创建或校验备份。",
	}
)

func allowedSystemMaintenanceIncident(incident systemMaintenanceIncident) bool {
	switch incident {
	case backupCreateMaintenanceIncident, backupVerifyMaintenanceIncident,
		backupDrillMaintenanceIncident, backupRestoreMaintenanceIncident,
		databaseStartupMaintenanceIncident, databaseMigrationMaintenanceIncident,
		sidecarStartupMaintenanceIncident, runtimeDatabaseMaintenanceIncident,
		storageLowSpaceMaintenanceIncident:
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
	return a.projectSystemMaintenanceFailureAt(incident, requestID, formatInboxTimestamp(a.options.Now()), uuid.NewString())
}

func (a *API) projectSystemMaintenanceFailureAt(
	incident systemMaintenanceIncident,
	requestID, occurredAt, incidentID string,
) error {
	if !allowedSystemMaintenanceIncident(incident) {
		return errors.New("unsupported system maintenance incident")
	}
	sourceID := systemMaintenanceSourceID(incident.component, incident.operation)
	eventKey := systemMaintenanceEventKey(sourceID, incidentID)
	return a.db.Transaction(func(tx *gorm.DB) error {
		var replayed int64
		if err := tx.Model(&models.InboxItem{}).Where("source_event_key = ?", eventKey).Count(&replayed).Error; err != nil {
			return err
		}
		if replayed > 0 {
			return nil
		}
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
			"failure_code": incident.failureCode, "occurred_at": occurredAt, "message": incident.message,
		})
		if err != nil {
			return err
		}
		item := models.InboxItem{
			ID: uuid.NewString(), Kind: "event", Title: incident.title, Summary: incident.message,
			SourceEntityType: systemMaintenanceInboxSourceType,
			SourceEntityID:   &sourceID,
			Priority:         "P1", Status: "open", ResolutionPolicy: "manual",
			PayloadJSON: string(payloadJSON), Version: 1, CreatedAt: occurredAt, UpdatedAt: occurredAt,
		}
		item.SourceEventKey = &eventKey
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("create system maintenance Inbox Item: %w", err)
		}
		return recordInboxWorkflowEventAs(
			tx, item.ID, "source_projected", models.BuiltinSystemActorID,
			nil, inboxItemEventState(item, ""), requestID, occurredAt,
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

func (a *API) recordRuntimeDatabaseFailure(requestID string) {
	if strings.TrimSpace(a.options.LogDir) == "" {
		return
	}
	if err := a.projectSystemMaintenanceFailure(runtimeDatabaseMaintenanceIncident, requestID); err == nil {
		return
	}
	if err := RecordStartupIncident(a.options.LogDir, StartupIncidentDatabaseRuntime, a.options.Now()); err != nil && a.options.Logger != nil {
		a.options.Logger.Print("runtime database incident could not be persisted safely")
	}
}

func (a *API) recordStorageLowSpace() error {
	if err := a.projectSystemMaintenanceFailure(storageLowSpaceMaintenanceIncident, "disk-space-monitor"); err == nil {
		return nil
	}
	if strings.TrimSpace(a.options.LogDir) == "" {
		return errors.New("storage incident journal is unavailable")
	}
	return RecordStartupIncident(a.options.LogDir, StartupIncidentStorageLowSpace, a.options.Now())
}
