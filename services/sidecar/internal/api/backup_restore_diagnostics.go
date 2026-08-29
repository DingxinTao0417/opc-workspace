package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type restoreDiagnostics struct {
	Status               string  `json:"status"`
	RestartRequired      bool    `json:"restart_required"`
	AppliedThisStartup   bool    `json:"applied_this_startup"`
	CleanupRequired      bool    `json:"cleanup_required"`
	AttentionRequired    bool    `json:"attention_required"`
	BackupID             *string `json:"backup_id"`
	RollbackBackupID     *string `json:"rollback_backup_id"`
	RequestedAt          *string `json:"requested_at"`
	ResidualAppliedCount int     `json:"residual_applied_count"`
	FailedAttemptCount   int     `json:"failed_attempt_count"`
	InvalidEntryCount    int     `json:"invalid_entry_count"`
}

func (a *API) getRestoreDiagnostics(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	diagnostics, err := inspectRestoreDiagnostics(a.backupStore.root, a.options.StartupRestore)
	if err != nil {
		a.logBackupError("inspect restore diagnostics", err)
		writeError(c, http.StatusInternalServerError, "RESTORE_DIAGNOSTICS_FAILED", "Restore diagnostics could not be read safely")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": diagnostics})
}

func inspectRestoreDiagnostics(root string, startup StartupRestoreResult) (restoreDiagnostics, error) {
	result := restoreDiagnostics{Status: "idle", AppliedThisStartup: startup.Applied}
	if startup.Applied {
		result.Status = "restored"
		setRestoreDiagnosticPlan(&result, startup.BackupID, startup.RollbackBackupID, startup.RequestedAt)
	}
	if startup.CleanupWarning != "" {
		result.CleanupRequired = true
	}

	pending, found, pendingErr := loadPendingRestore(root)
	if pendingErr != nil {
		result.InvalidEntryCount++
	} else if found {
		result.RestartRequired = true
		setRestoreDiagnosticPlan(&result, pending.BackupID, pending.RollbackBackupID, pending.RequestedAt)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return restoreDiagnostics{}, err
	}
	var latestPlan *pendingRestorePlan
	for _, entry := range entries {
		name := entry.Name()
		kind, operationID, recognized := restoreDiagnosticEntry(name)
		if !recognized {
			continue
		}
		path := filepath.Join(root, name)
		info, infoErr := os.Lstat(path)
		parsed, parseErr := uuid.Parse(operationID)
		if infoErr != nil || parseErr != nil || parsed.String() != operationID || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || requireSafeDirectory(path) != nil {
			result.InvalidEntryCount++
			continue
		}
		var plan pendingRestorePlan
		if readStrictJSONFile(filepath.Join(path, pendingRestorePlanName), maxRestorePlanBytes, &plan) != nil || validatePendingRestorePlan(plan) != nil || plan.OperationID != operationID {
			result.InvalidEntryCount++
			continue
		}
		if kind == "applied" {
			result.ResidualAppliedCount++
			result.CleanupRequired = true
		} else {
			result.FailedAttemptCount++
		}
		if latestPlan == nil || restorePlanTime(plan).After(restorePlanTime(*latestPlan)) {
			copy := plan
			latestPlan = &copy
		}
	}
	if !result.RestartRequired && !startup.Applied && latestPlan != nil {
		setRestoreDiagnosticPlan(&result, latestPlan.BackupID, latestPlan.RollbackBackupID, latestPlan.RequestedAt)
	}
	result.AttentionRequired = result.FailedAttemptCount > 0 || result.InvalidEntryCount > 0
	switch {
	case result.AttentionRequired:
		result.Status = "attention_required"
	case result.RestartRequired:
		result.Status = "restart_required"
	case result.CleanupRequired:
		result.Status = "cleanup_required"
	case startup.Applied:
		result.Status = "restored"
	default:
		result.Status = "idle"
	}
	return result, nil
}

func restoreDiagnosticEntry(name string) (string, string, bool) {
	if strings.HasPrefix(name, appliedRestorePrefix) {
		return "applied", strings.TrimPrefix(name, appliedRestorePrefix), true
	}
	const failedPrefix = ".restore-failed-"
	if strings.HasPrefix(name, failedPrefix) {
		return "failed", strings.TrimPrefix(name, failedPrefix), true
	}
	return "", "", false
}

func restorePlanTime(plan pendingRestorePlan) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, plan.RequestedAt)
	return parsed
}

func setRestoreDiagnosticPlan(result *restoreDiagnostics, backupID, rollbackID, requestedAt string) {
	if backupID != "" {
		result.BackupID = &backupID
	}
	if rollbackID != "" {
		result.RollbackBackupID = &rollbackID
	}
	if requestedAt != "" {
		result.RequestedAt = &requestedAt
	}
}
