package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const bytesPerGiB uint64 = 1 << 30

type storageCapacityLocation struct {
	Kind           string  `json:"kind"`
	Status         string  `json:"status"`
	AvailableBytes *uint64 `json:"available_bytes"`
	TotalBytes     *uint64 `json:"total_bytes"`
}

type storageCapacityResponse struct {
	CheckedAt    string                    `json:"checked_at"`
	ThresholdGiB uint64                    `json:"threshold_gib"`
	Locations    []storageCapacityLocation `json:"locations"`
}

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
		canonical, ok := canonicalStorageProbePath(candidate)
		if !ok {
			continue
		}
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

func canonicalStorageProbePath(candidate string) (string, bool) {
	if strings.TrimSpace(candidate) == "" {
		return "", false
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	return filepath.Clean(absolute), true
}

func (a *API) currentLowDiskThresholdBytes() uint64 {
	thresholdBytes := a.lowDiskThresholdBytes.Load()
	if thresholdBytes == 0 {
		thresholdBytes = bytesPerGiB
	}
	if configuredThreshold, err := loadLowDiskThresholdBytes(a.db); err == nil {
		thresholdBytes = configuredThreshold
		a.lowDiskThresholdBytes.Store(configuredThreshold)
	}
	return thresholdBytes
}

func (a *API) getStorageCapacity(c *gin.Context) {
	thresholdBytes := a.currentLowDiskThresholdBytes()
	checker := a.options.DiskSpaceCheck
	if checker == nil {
		checker = diskFreeBytes
	}
	databasePath := ""
	if strings.TrimSpace(a.options.DatabasePath) != "" {
		databasePath = filepath.Dir(a.options.DatabasePath)
	}
	targets := []struct {
		kind string
		path string
	}{
		{kind: "database", path: databasePath},
		{kind: "artifacts", path: a.options.ArtifactDir},
		{kind: "backups", path: a.options.BackupDir},
	}
	locations := make([]storageCapacityLocation, 0, len(targets))
	for _, target := range targets {
		location := storageCapacityLocation{Kind: target.kind, Status: "unavailable"}
		if path, ok := canonicalStorageProbePath(target.path); ok {
			available, total, err := checker(path)
			if err == nil && total > 0 && available <= total {
				location.AvailableBytes = &available
				location.TotalBytes = &total
				location.Status = "healthy"
				if available < thresholdBytes {
					location.Status = "low"
				}
			}
		}
		locations = append(locations, location)
	}
	c.JSON(http.StatusOK, gin.H{"data": storageCapacityResponse{
		CheckedAt: a.options.Now().UTC().Format(time.RFC3339Nano), ThresholdGiB: thresholdBytes / bytesPerGiB,
		Locations: locations,
	}})
}

func (a *API) scanDiskSpace() error {
	thresholdBytes := a.currentLowDiskThresholdBytes()
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
