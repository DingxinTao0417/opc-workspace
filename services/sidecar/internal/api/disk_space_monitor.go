package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const bytesPerGiB uint64 = 1 << 30

const (
	storageCapacitySampleInterval = 15 * time.Minute
	storageCapacityRetention      = 30 * 24 * time.Hour
)

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

type storageCapacityHistoryPoint struct {
	Scope          string `json:"scope"`
	CheckedAt      string `json:"checked_at"`
	AvailableBytes int64  `json:"available_bytes"`
	TotalBytes     int64  `json:"total_bytes"`
	ThresholdBytes int64  `json:"threshold_bytes"`
	Status         string `json:"status"`
}

type storageCapacityHistoryResponse struct {
	From   string                        `json:"from"`
	To     string                        `json:"to"`
	Points []storageCapacityHistoryPoint `json:"points"`
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

func (a *API) storageCapacitySnapshot(record bool) storageCapacityResponse {
	thresholdBytes := a.currentLowDiskThresholdBytes()
	targets, results, counts := probeStorageTargets(a.options)
	checkedAt := a.options.Now().UTC()
	if record {
		a.recordStorageCapacitySamples(targets, results, thresholdBytes, checkedAt)
	}
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
	return storageCapacityResponse{
		CheckedAt: checkedAt.Format(time.RFC3339Nano), ThresholdGiB: thresholdBytes / bytesPerGiB,
		Locations: locations,
	}
}

func (a *API) getStorageCapacity(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": a.storageCapacitySnapshot(false)})
}

func (a *API) checkStorageCapacity(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": a.storageCapacitySnapshot(true)})
}

func (a *API) getStorageCapacityHistory(c *gin.Context) {
	days := 7
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 30 {
			writeError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "days must be an integer between 1 and 30")
			return
		}
		days = parsed
	}
	to := a.options.Now().UTC()
	from := to.Add(-time.Duration(days) * 24 * time.Hour)
	points := make([]storageCapacityHistoryPoint, 0)
	if err := a.db.Raw(`
		SELECT scope, checked_at, available_bytes, total_bytes, threshold_bytes, status
		FROM storage_capacity_samples
		WHERE sample_bucket BETWEEN ? AND ?
		ORDER BY sample_bucket ASC, scope ASC
	`, from.Unix()/int64(storageCapacitySampleInterval/time.Second), to.Unix()/int64(storageCapacitySampleInterval/time.Second)).Scan(&points).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": storageCapacityHistoryResponse{
		From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano), Points: points,
	}})
}

func (a *API) scanDiskSpace() error {
	thresholdBytes := a.currentLowDiskThresholdBytes()
	targets, results, _ := probeStorageTargets(a.options)
	a.recordStorageCapacitySamples(targets, results, thresholdBytes, a.options.Now().UTC())
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

func (a *API) recordStorageCapacitySamples(targets []storageProbeTarget, results map[string]storageProbeResult, thresholdBytes uint64, checkedAt time.Time) {
	if thresholdBytes == 0 || thresholdBytes > math.MaxInt64 {
		return
	}
	scopes := make(map[string][]string)
	for _, target := range targets {
		if target.groupKey != "" {
			scopes[target.groupKey] = append(scopes[target.groupKey], target.kind)
		}
	}
	bucket := checkedAt.Unix() / int64(storageCapacitySampleInterval/time.Second)
	cutoffBucket := checkedAt.Add(-storageCapacityRetention).Unix() / int64(storageCapacitySampleInterval/time.Second)
	err := a.db.Transaction(func(tx *gorm.DB) error {
		for groupKey, kinds := range scopes {
			result, ok := results[groupKey]
			if !ok || !result.valid() || result.available > math.MaxInt64 || result.total > math.MaxInt64 {
				continue
			}
			sort.Strings(kinds)
			scope := canonicalStorageCapacityScope(kinds)
			if scope == "" {
				continue
			}
			status := "healthy"
			if result.available < thresholdBytes {
				status = "low"
			}
			if err := tx.Exec(`
				INSERT INTO storage_capacity_samples (
					scope, sample_bucket, available_bytes, total_bytes, threshold_bytes, status, checked_at
				) VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(scope, sample_bucket) DO UPDATE SET
					available_bytes = excluded.available_bytes,
					total_bytes = excluded.total_bytes,
					threshold_bytes = excluded.threshold_bytes,
					status = excluded.status,
					checked_at = excluded.checked_at
			`, scope, bucket, int64(result.available), int64(result.total), int64(thresholdBytes), status, checkedAt.Format(time.RFC3339Nano)).Error; err != nil {
				return err
			}
		}
		return tx.Exec("DELETE FROM storage_capacity_samples WHERE sample_bucket < ?", cutoffBucket).Error
	})
	if err != nil && a.options.Logger != nil {
		a.options.Logger.Print("storage capacity history could not be recorded safely")
	}
}

func canonicalStorageCapacityScope(kinds []string) string {
	present := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		present[kind] = true
	}
	ordered := make([]string, 0, 3)
	for _, kind := range []string{"database", "artifacts", "backups"} {
		if present[kind] {
			ordered = append(ordered, kind)
		}
	}
	return strings.Join(ordered, "+")
}
