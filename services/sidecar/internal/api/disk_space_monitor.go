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
	SharedVolume   bool    `json:"shared_volume"`
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

type storageProbeTarget struct {
	kind      string
	path      string
	groupKey  string
	shareable bool
}

type storageProbeResult struct {
	available uint64
	total     uint64
	err       error
}

func (result storageProbeResult) valid() bool {
	return result.err == nil && result.total > 0 && result.available <= result.total
}

func storageProbeTargets(options Options) []storageProbeTarget {
	databasePath := ""
	if strings.TrimSpace(options.DatabasePath) != "" {
		databasePath = filepath.Dir(options.DatabasePath)
	}
	targets := []storageProbeTarget{
		{kind: "database", path: databasePath},
		{kind: "artifacts", path: options.ArtifactDir},
		{kind: "backups", path: options.BackupDir},
	}
	identityCheck := options.VolumeIdentityCheck
	if identityCheck == nil {
		identityCheck = storageVolumeIdentity
	}
	for index := range targets {
		canonical, ok := canonicalStorageProbePath(targets[index].path)
		if !ok {
			targets[index].path = ""
			continue
		}
		targets[index].path = canonical
		identity, err := identityCheck(canonical)
		identity = strings.TrimSpace(identity)
		if err == nil && identity != "" {
			if runtime.GOOS == "windows" {
				identity = strings.ToLower(identity)
			}
			targets[index].groupKey = "volume:" + identity
			targets[index].shareable = true
			continue
		}
		pathKey := canonical
		if runtime.GOOS == "windows" {
			pathKey = strings.ToLower(pathKey)
		}
		targets[index].groupKey = "path:" + pathKey
	}
	return targets
}

func probeStorageTargets(options Options) ([]storageProbeTarget, map[string]storageProbeResult, map[string]int) {
	targets := storageProbeTargets(options)
	checker := options.DiskSpaceCheck
	if checker == nil {
		checker = diskFreeBytes
	}
	results := make(map[string]storageProbeResult, len(targets))
	counts := make(map[string]int, len(targets))
	for _, target := range targets {
		if target.groupKey == "" {
			continue
		}
		counts[target.groupKey]++
		if existing, exists := results[target.groupKey]; exists && existing.valid() {
			continue
		}
		available, total, err := checker(target.path)
		result := storageProbeResult{available: available, total: total, err: err}
		if existing, exists := results[target.groupKey]; !exists || result.valid() || !existing.valid() {
			results[target.groupKey] = result
		}
	}
	return targets, results, counts
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
	targets, results, counts := probeStorageTargets(a.options)
	locations := make([]storageCapacityLocation, 0, len(targets))
	for _, target := range targets {
		location := storageCapacityLocation{
			Kind: target.kind, Status: "unavailable",
			SharedVolume: target.shareable && counts[target.groupKey] > 1,
		}
		if result, ok := results[target.groupKey]; ok {
			if result.valid() {
				available, total := result.available, result.total
				location.AvailableBytes = &available
				location.TotalBytes = &total
				location.Status = "healthy"
				if result.available < thresholdBytes {
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
	_, results, _ := probeStorageTargets(a.options)
	low := false
	probeFailed := false
	for _, result := range results {
		if !result.valid() {
			probeFailed = true
			continue
		}
		if result.available < thresholdBytes {
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
