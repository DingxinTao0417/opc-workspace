package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// CreatePreMigrationBackup creates the same fully verified SQLite and
// controlled-file package as the manual backup flow, but before the migration
// gate is released. Startup is single-threaded here, so no HTTP maintenance
// lock is required.
func CreatePreMigrationBackup(db *gorm.DB, options Options, targetSchema int) (string, error) {
	if db == nil {
		return "", errors.New("pre-migration backup requires an open database")
	}
	if options.SchemaVersion < 9 || targetSchema <= options.SchemaVersion {
		return "", fmt.Errorf("invalid pre-migration schema boundary %d -> %d", options.SchemaVersion, targetSchema)
	}
	if strings.TrimSpace(options.ArtifactDir) == "" || strings.TrimSpace(options.BackupDir) == "" || strings.TrimSpace(options.DatabasePath) == "" {
		return "", errors.New("pre-migration backup requires database, Artifact, and backup directories")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	artifacts, err := openWorkspaceArtifactStore(db, options.ArtifactDir, false)
	if err != nil {
		return "", fmt.Errorf("open controlled files for pre-migration backup: %w", err)
	}
	backups, err := newBackupStore(options.BackupDir, options.DatabasePath, artifacts)
	if err != nil {
		return "", errors.Join(err, artifacts.close())
	}
	note := fmt.Sprintf("自动迁移前备份：schema v%d → v%d", options.SchemaVersion, targetSchema)
	if err := backups.requireCreateCapacity(db, options, 0); err != nil {
		return "", errors.Join(fmt.Errorf("verify pre-migration backup capacity: %w", err), artifacts.close())
	}
	summary, err := backups.create(db, options, note, "", sha256Hex([]byte(note)))
	if err != nil {
		return "", errors.Join(fmt.Errorf("create verified pre-migration backup: %w", err), artifacts.close())
	}
	if err := artifacts.close(); err != nil {
		return "", fmt.Errorf("release controlled files after pre-migration backup: %w", err)
	}
	return summary.ID, nil
}
