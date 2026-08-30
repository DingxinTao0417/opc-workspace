package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxScheduledBackupRetention = 365

type scheduledBackupPolicy struct {
	Singleton         int     `gorm:"column:singleton" json:"-"`
	Enabled           bool    `gorm:"column:enabled" json:"enabled"`
	LocalTime         string  `gorm:"column:local_time" json:"local_time"`
	Timezone          string  `gorm:"column:timezone" json:"timezone"`
	RetentionCount    int     `gorm:"column:retention_count" json:"retention_count"`
	LastAttemptedDate *string `gorm:"column:last_attempted_date" json:"last_attempted_date"`
	LastAttemptAt     *string `gorm:"column:last_attempt_at" json:"last_attempt_at"`
	LastSuccessAt     *string `gorm:"column:last_success_at" json:"last_success_at"`
	LastBackupID      *string `gorm:"column:last_backup_id" json:"last_backup_id"`
	LastStatus        string  `gorm:"column:last_status" json:"last_status"`
	LastErrorCode     *string `gorm:"column:last_error_code" json:"last_error_code"`
	Version           int64   `gorm:"column:version" json:"version"`
	UpdatedAt         string  `gorm:"column:updated_at" json:"updated_at"`
	NextRunAt         *string `gorm:"-" json:"next_run_at"`
}

type updateScheduledBackupPolicyRequest struct {
	Enabled        *bool   `json:"enabled"`
	LocalTime      *string `json:"local_time"`
	Timezone       *string `json:"timezone"`
	RetentionCount *int    `json:"retention_count"`
}

func (a *API) getScheduledBackupPolicy(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	policy, err := loadScheduledBackupPolicy(a.db.WithContext(c.Request.Context()))
	if err != nil {
		a.logBackupError("read scheduled policy", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_POLICY_READ_FAILED", "The scheduled backup policy could not be read")
		return
	}
	decorateScheduledBackupPolicy(&policy, a.options.Now())
	setProjectETag(c, policy.Version)
	c.JSON(http.StatusOK, gin.H{"data": policy})
}

func (a *API) updateScheduledBackupPolicy(c *gin.Context) {
	if a.backupStore == nil {
		writeError(c, http.StatusServiceUnavailable, "BACKUP_UNAVAILABLE", "Verified local backups are unavailable")
		return
	}
	expectedVersion, ok := projectIfMatch(c)
	if !ok {
		return
	}
	var input updateScheduledBackupPolicyRequest
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, http.StatusBadRequest, "INVALID_JSON", "The request body is not valid JSON")
		return
	}
	if input.Enabled == nil || input.LocalTime == nil || input.Timezone == nil || input.RetentionCount == nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "enabled, local_time, timezone and retention_count are required")
		return
	}
	localTime := strings.TrimSpace(*input.LocalTime)
	timezone := strings.TrimSpace(*input.Timezone)
	if _, _, err := parseScheduledLocalTime(localTime); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "local_time must use HH:MM in 24-hour time")
		return
	}
	if len(timezone) > 100 {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "timezone is too long")
		return
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "timezone must be a supported IANA timezone")
		return
	}
	if *input.RetentionCount < 1 || *input.RetentionCount > maxScheduledBackupRetention {
		writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "retention_count must be between 1 and 365")
		return
	}
	nowText := a.options.Now().UTC().Format(time.RFC3339Nano)
	result := a.db.WithContext(c.Request.Context()).Table("scheduled_backup_policy").
		Where("singleton = 1 AND version = ?", expectedVersion).
		Updates(map[string]any{
			"enabled":         *input.Enabled,
			"local_time":      localTime,
			"timezone":        timezone,
			"retention_count": *input.RetentionCount,
			"version":         gorm.Expr("version + 1"),
			"updated_at":      nowText,
		})
	if result.Error != nil {
		a.logBackupError("update scheduled policy", result.Error)
		writeError(c, http.StatusInternalServerError, "BACKUP_POLICY_UPDATE_FAILED", "The scheduled backup policy could not be saved")
		return
	}
	if result.RowsAffected != 1 {
		writeError(c, http.StatusConflict, "VERSION_CONFLICT", "Scheduled backup settings changed in another window; reload and retry")
		return
	}
	policy, err := loadScheduledBackupPolicy(a.db.WithContext(c.Request.Context()))
	if err != nil {
		a.logBackupError("reload scheduled policy", err)
		writeError(c, http.StatusInternalServerError, "BACKUP_POLICY_READ_FAILED", "The saved scheduled backup policy could not be reloaded")
		return
	}
	decorateScheduledBackupPolicy(&policy, a.options.Now())
	setProjectETag(c, policy.Version)
	c.JSON(http.StatusOK, gin.H{"data": policy})
}

func loadScheduledBackupPolicy(db *gorm.DB) (scheduledBackupPolicy, error) {
	var policy scheduledBackupPolicy
	err := db.Table("scheduled_backup_policy").Where("singleton = 1").Take(&policy).Error
	return policy, err
}

func parseScheduledLocalTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, 0, errors.New("invalid scheduled local time")
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func decorateScheduledBackupPolicy(policy *scheduledBackupPolicy, now time.Time) {
	if policy == nil || !policy.Enabled {
		policy.NextRunAt = nil
		return
	}
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		policy.NextRunAt = nil
		return
	}
	hour, minute, err := parseScheduledLocalTime(policy.LocalTime)
	if err != nil {
		policy.NextRunAt = nil
		return
	}
	localNow := now.In(location)
	localDate := localNow.Format("2006-01-02")
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !candidate.After(localNow) && policy.LastAttemptedDate != nil && *policy.LastAttemptedDate == localDate {
		candidate = candidate.AddDate(0, 0, 1)
	}
	formatted := candidate.UTC().Format(time.RFC3339Nano)
	policy.NextRunAt = &formatted
}

func (a *API) runDueScheduledBackup(ctx context.Context) error {
	if a.backupStore == nil || a.restorePending.Load() {
		return nil
	}
	a.maintenance.Lock()
	defer a.maintenance.Unlock()
	a.backupStore.mu.Lock()
	defer a.backupStore.mu.Unlock()

	policy, err := loadScheduledBackupPolicy(a.db.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	if !policy.Enabled {
		return nil
	}
	location, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return a.finishScheduledBackupAttempt(ctx, policy, nil, "BACKUP_POLICY_TIMEZONE_INVALID")
	}
	hour, minute, err := parseScheduledLocalTime(policy.LocalTime)
	if err != nil {
		return a.finishScheduledBackupAttempt(ctx, policy, nil, "BACKUP_POLICY_TIME_INVALID")
	}
	now := a.options.Now()
	localNow := now.In(location)
	localDate := localNow.Format("2006-01-02")
	dueAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if localNow.Before(dueAt) || (policy.LastAttemptedDate != nil && *policy.LastAttemptedDate == localDate) {
		return nil
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	claimed := a.db.WithContext(ctx).Table("scheduled_backup_policy").
		Where("singleton = 1 AND version = ?", policy.Version).
		Updates(map[string]any{
			"last_attempted_date": localDate,
			"last_attempt_at":     nowText,
			"last_status":         "failed",
			"last_error_code":     "SCHEDULED_BACKUP_INTERRUPTED",
			"version":             gorm.Expr("version + 1"),
			"updated_at":          nowText,
		})
	if claimed.Error != nil {
		return fmt.Errorf("claim scheduled backup: %w", claimed.Error)
	}
	if claimed.RowsAffected != 1 {
		return nil
	}
	policy.Version++
	policy.LastAttemptedDate = &localDate
	if err := a.backupStore.requireCreateCapacity(a.db.WithContext(ctx), a.options, 0); err != nil {
		code := "BACKUP_CAPACITY_UNAVAILABLE"
		if errors.Is(err, errBackupSpaceInsufficient) {
			code = "BACKUP_SPACE_INSUFFICIENT"
		}
		return a.finishScheduledBackupAttempt(ctx, policy, nil, code)
	}
	note := "计划备份 · " + localDate
	summary, err := a.backupStore.createWithKind(a.db.WithContext(ctx), a.options, note, "", sha256Hex([]byte("scheduled:"+localDate)), "scheduled")
	if err != nil {
		return a.finishScheduledBackupAttempt(ctx, policy, nil, "BACKUP_CREATE_FAILED")
	}
	if err := a.pruneScheduledBackupsLocked(policy.RetentionCount); err != nil {
		return a.finishScheduledBackupAttempt(ctx, policy, &summary, "BACKUP_RETENTION_FAILED")
	}
	return a.finishScheduledBackupAttempt(ctx, policy, &summary, "")
}

func (a *API) finishScheduledBackupAttempt(ctx context.Context, policy scheduledBackupPolicy, backup *backupSummary, errorCode string) error {
	nowText := a.options.Now().UTC().Format(time.RFC3339Nano)
	updates := map[string]any{
		"last_status":     "failed",
		"last_error_code": errorCode,
		"version":         gorm.Expr("version + 1"),
		"updated_at":      nowText,
	}
	if backup != nil {
		updates["last_backup_id"] = backup.ID
		updates["last_success_at"] = backup.CreatedAt
	}
	if errorCode == "" {
		updates["last_status"] = "succeeded"
		updates["last_error_code"] = nil
	}
	result := a.db.WithContext(ctx).Table("scheduled_backup_policy").
		Where("singleton = 1 AND version = ?", policy.Version).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("scheduled backup policy changed while recording result")
	}
	if errorCode != "" {
		return fmt.Errorf("scheduled backup failed: %s", errorCode)
	}
	return nil
}

func (a *API) pruneScheduledBackupsLocked(retention int) error {
	entries, err := os.ReadDir(a.backupStore.root)
	if err != nil {
		return err
	}
	type candidate struct {
		id        string
		createdAt string
	}
	candidates := make([]candidate, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, err := readBackupManifest(filepath.Join(a.backupStore.root, entry.Name()))
		if err != nil || manifest.Kind != "scheduled" || validateBackupManifest(manifest, entry.Name(), a.options.SchemaVersion) != nil {
			continue
		}
		candidates = append(candidates, candidate{id: manifest.ID, createdAt: manifest.CreatedAt})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].createdAt == candidates[j].createdAt {
			return candidates[i].id > candidates[j].id
		}
		return candidates[i].createdAt > candidates[j].createdAt
	})
	protected := map[string]struct{}{}
	if plan, found, err := loadPendingRestore(a.backupStore.root); err != nil {
		return err
	} else if found {
		protected[plan.BackupID] = struct{}{}
		protected[plan.RollbackBackupID] = struct{}{}
	}
	for index := retention; index < len(candidates); index++ {
		if _, ok := protected[candidates[index].id]; ok {
			continue
		}
		if err := removeBackupPackageSafely(a.backupStore.root, candidates[index].id); err != nil {
			return err
		}
	}
	return nil
}

func removeBackupPackageSafely(root, id string) error {
	packagePath := filepath.Join(root, id)
	deletingPath := filepath.Join(root, deletingBackupPrefix+id)
	packageExists, err := safeBackupDeletionPath(packagePath)
	if err != nil {
		return fmt.Errorf("%w: %v", errBackupDeleteUnsafe, err)
	}
	deletingExists, err := safeBackupDeletionPath(deletingPath)
	if err != nil {
		return fmt.Errorf("%w: %v", errBackupDeleteUnsafe, err)
	}
	if !packageExists && !deletingExists {
		return os.ErrNotExist
	}
	if packageExists {
		if deletingExists {
			return errors.New("pending deletion already exists")
		}
		if err := os.Rename(packagePath, deletingPath); err != nil {
			return err
		}
		if err := syncArtifactDirectory(root); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(deletingPath); err != nil {
		return err
	}
	return syncArtifactDirectory(root)
}
