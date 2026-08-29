package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const bytesPerGiB uint64 = 1 << 30

func loadLowDiskThresholdBytes(db *gorm.DB) (uint64, error) {
	var row models.AppSetting
	err := db.First(&row, "key = ?", "storage").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bytesPerGiB, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load storage setting: %w", err)
	}
	if row.SchemaVersion != settingsSchemaVersion {
		return 0, errors.New("unsupported stored storage setting schema")
	}
	normalized, err := normalizeSettingValue("storage", []byte(row.ValueJSON))
	if err != nil {
		return 0, fmt.Errorf("decode storage setting: %w", err)
	}
	var value storageSettingValue
	if err := json.Unmarshal([]byte(normalized), &value); err != nil {
		return 0, fmt.Errorf("decode normalized storage setting: %w", err)
	}
	return uint64(value.LowSpaceThresholdGiB) * bytesPerGiB, nil
}

func storageProbePaths(options Options) []string {
	candidates := []string{options.ArtifactDir, options.BackupDir}
	if strings.TrimSpace(options.DatabasePath) != "" {
		candidates = append(candidates, filepath.Dir(options.DatabasePath))
	}
	paths := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		canonical := filepath.Clean(absolute)
		key := canonical
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, canonical)
	}
	return paths
}

func (a *API) scanDiskSpace() error {
	thresholdBytes := a.lowDiskThresholdBytes.Load()
	if thresholdBytes == 0 {
		thresholdBytes = bytesPerGiB
	}
	if configuredThreshold, err := loadLowDiskThresholdBytes(a.db); err == nil {
		thresholdBytes = configuredThreshold
		a.lowDiskThresholdBytes.Store(configuredThreshold)
	}
	checker := a.options.DiskSpaceCheck
	if checker == nil {
		checker = diskFreeBytes
	}
	low := false
	probeFailed := false
	for _, path := range storageProbePaths(a.options) {
		available, _, err := checker(path)
		if err != nil {
			probeFailed = true
			continue
		}
		if available < thresholdBytes {
			low = true
		}
	}
	if !low {
		if probeFailed {
			return errors.New("storage capacity probe failed")
		}
		a.lowDiskActive.Store(false)
		return nil
	}
	if !a.lowDiskActive.CompareAndSwap(false, true) {
		return nil
	}
	if err := a.recordStorageLowSpace(); err != nil {
		a.lowDiskActive.Store(false)
		return err
	}
	return nil
}
