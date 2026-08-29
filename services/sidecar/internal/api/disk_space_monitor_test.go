package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestDiskSpaceMonitorProjectsOncePerLowSpaceEpisode(t *testing.T) {
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	available := uint64(512 << 20)
	checks := 0
	service := &API{db: store.DB, options: Options{
		ArtifactDir: root, BackupDir: root, DatabasePath: filepath.Join(root, "workspace.db"),
		LogDir: filepath.Join(root, "logs"), Logger: log.New(io.Discard, "", 0),
		Now: func() time.Time { return time.Date(2026, 8, 28, 21, 30, 0, 0, time.UTC) },
		DiskSpaceCheck: func(string) (uint64, uint64, error) {
			checks++
			return available, 100 << 30, nil
		},
	}}

	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("deduplicated probe checks=%d, want 1", checks)
	}
	assertStorageIncidentCount(t, store, 1)
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	assertStorageIncidentCount(t, store, 1)

	if err := store.DB.Model(&models.InboxItem{}).
		Where("source_entity_id = ?", "storage:low_space").
		Updates(map[string]any{
			"status": "resolved", "triaged_at": "2026-08-28T21:31:00Z",
			"resolved_by_actor_id": models.BuiltinOwnerActorID,
			"resolved_at":          "2026-08-28T21:31:00Z",
			"resolution_reason":    "space cleanup started", "resolution_mode": "manual",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	assertStorageIncidentCount(t, store, 1)

	available = 2 << 30
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	available = 512 << 20
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	assertStorageIncidentCount(t, store, 2)
}

func TestDiskSpaceMonitorProbesDifferentPathsOnOneVolumeOnce(t *testing.T) {
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	checks := 0
	service := &API{db: store.DB, options: Options{
		DatabasePath: filepath.Join(root, "workspace.db"),
		ArtifactDir:  filepath.Join(root, "artifacts"),
		BackupDir:    filepath.Join(root, "backups"),
		LogDir:       filepath.Join(root, "logs"), Logger: log.New(io.Discard, "", 0), Now: time.Now,
		VolumeIdentityCheck: func(string) (string, error) { return "same-private-volume", nil },
		DiskSpaceCheck: func(string) (uint64, uint64, error) {
			checks++
			return 2 << 30, 100 << 30, nil
		},
	}}
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("physical volume probe checks=%d, want 1", checks)
	}
	assertStorageIncidentCount(t, store, 0)
}

func TestStorageVolumeGroupRetriesAnotherLogicalPathAfterProbeFailure(t *testing.T) {
	root := t.TempDir()
	checks := 0
	targets, results, counts := probeStorageTargets(Options{
		DatabasePath: filepath.Join(root, "workspace.db"),
		ArtifactDir:  filepath.Join(root, "artifacts"),
		BackupDir:    filepath.Join(root, "backups"),
		VolumeIdentityCheck: func(string) (string, error) {
			return "same-private-volume", nil
		},
		DiskSpaceCheck: func(string) (uint64, uint64, error) {
			checks++
			if checks == 1 {
				return 0, 0, errors.New("private mount failure")
			}
			return 2 << 30, 100 << 30, nil
		},
	})
	if len(targets) != 3 || len(results) != 1 || checks != 2 {
		t.Fatalf("group retry targets=%d results=%d checks=%d", len(targets), len(results), checks)
	}
	for key, result := range results {
		if !result.valid() || counts[key] != 3 {
			t.Fatalf("group retry result=%#v count=%d", result, counts[key])
		}
	}
}

func TestDiskSpaceMonitorFallsBackToSafeJournal(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(root, "logs")
	service := &API{db: store.DB, options: Options{
		DatabasePath: databasePath, LogDir: logDir, Logger: log.New(io.Discard, "", 0),
		Now:            func() time.Time { return time.Date(2026, 8, 28, 21, 35, 0, 0, time.UTC) },
		DiskSpaceCheck: func(string) (uint64, uint64, error) { return 512 << 20, 100 << 30, nil },
	}}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	journalPath, err := startupIncidentJournalPath(logDir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := readStartupIncidentJournal(journalPath)
	if err != nil || len(journal.Incidents) != 1 || journal.Incidents[0].Kind != StartupIncidentStorageLowSpace {
		t.Fatalf("low-space fallback journal=%#v err=%v", journal, err)
	}
}

func TestDiskSpaceMonitorKeepsKnownLowSpaceWhenAnotherProbeFails(t *testing.T) {
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	checks := 0
	service := &API{db: store.DB, options: Options{
		DatabasePath: filepath.Join(root, "workspace.db"), ArtifactDir: filepath.Join(root, "artifacts"),
		LogDir: filepath.Join(root, "logs"), Logger: log.New(io.Discard, "", 0), Now: time.Now,
		DiskSpaceCheck: func(string) (uint64, uint64, error) {
			checks++
			if checks == 1 {
				return 512 << 20, 100 << 30, nil
			}
			return 0, 0, errors.New("path canary must stay private")
		},
	}}
	if err := service.scanDiskSpace(); err != nil {
		t.Fatalf("known low-space fact was discarded: %v", err)
	}
	assertStorageIncidentCount(t, store, 1)
}

func TestDiskSpaceMonitorUsesStoredThresholdOnTheNextScan(t *testing.T) {
	root := t.TempDir()
	store, err := database.Open(filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setting := models.AppSetting{
		Key: "storage", ValueJSON: `{"low_space_threshold_gib":5}`, SchemaVersion: 1, Version: 1,
		UpdatedByActorID: models.BuiltinOwnerActorID, UpdatedAt: "2026-08-28T21:40:00Z",
	}
	if err := store.DB.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, options: Options{
		DatabasePath: filepath.Join(root, "workspace.db"), LogDir: filepath.Join(root, "logs"),
		Logger: log.New(io.Discard, "", 0), Now: time.Now,
		DiskSpaceCheck: func(string) (uint64, uint64, error) { return 2 << 30, 100 << 30, nil },
	}}
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	assertStorageIncidentCount(t, store, 1)

	if err := store.DB.Model(&models.AppSetting{}).Where("key = ?", "storage").Updates(map[string]any{
		"value_json": `{"low_space_threshold_gib":1}`, "version": 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	service.lowDiskActive.Store(false)
	if err := service.scanDiskSpace(); err != nil {
		t.Fatal(err)
	}
	assertStorageIncidentCount(t, store, 1)
}

func TestStorageCapacityEndpointReturnsSafeLogicalLocationStatus(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	setting := models.AppSetting{
		Key: "storage", ValueJSON: `{"low_space_threshold_gib":5}`, SchemaVersion: 1, Version: 1,
		UpdatedByActorID: models.BuiltinOwnerActorID, UpdatedAt: "2026-08-28T21:45:00Z",
	}
	if err := store.DB.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		DatabasePath: databasePath, ArtifactDir: filepath.Join(root, "artifacts"), BackupDir: filepath.Join(root, "backups"),
		Logger: log.New(io.Discard, "", 0), Now: func() time.Time { return time.Date(2026, 8, 28, 21, 45, 0, 0, time.UTC) },
		VolumeIdentityCheck: func(path string) (string, error) { return "volume:" + path, nil },
		DiskSpaceCheck: func(path string) (uint64, uint64, error) {
			if strings.HasSuffix(path, "backups") {
				return 0, 0, errors.New("private probe failure")
			}
			if strings.HasSuffix(path, "artifacts") {
				return 2 << 30, 100 << 30, nil
			}
			return 10 << 30, 100 << 30, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	recorder := performRequest(router.Engine, http.MethodGet, "/api/v1/diagnostics/storage", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("storage diagnostics status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), root) || strings.Contains(recorder.Body.String(), "private probe failure") {
		t.Fatalf("storage diagnostics leaked private details: %s", recorder.Body.String())
	}
	var envelope struct {
		Data storageCapacityResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ThresholdGiB != 5 || envelope.Data.CheckedAt != "2026-08-28T21:45:00Z" || len(envelope.Data.Locations) != 3 {
		t.Fatalf("storage capacity response=%#v", envelope.Data)
	}
	if envelope.Data.Locations[0].Kind != "database" || envelope.Data.Locations[0].Status != "healthy" ||
		envelope.Data.Locations[0].SharedVolume ||
		envelope.Data.Locations[1].Kind != "artifacts" || envelope.Data.Locations[1].Status != "low" ||
		envelope.Data.Locations[1].SharedVolume ||
		envelope.Data.Locations[2].Kind != "backups" || envelope.Data.Locations[2].Status != "unavailable" ||
		envelope.Data.Locations[2].SharedVolume ||
		envelope.Data.Locations[2].AvailableBytes != nil || envelope.Data.Locations[2].TotalBytes != nil {
		t.Fatalf("storage locations=%#v", envelope.Data.Locations)
	}
}

func TestStorageCapacityEndpointMarksSharedVolumeWithoutReturningIdentity(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	checks := 0
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		DatabasePath: databasePath, ArtifactDir: filepath.Join(root, "artifacts"), BackupDir: filepath.Join(root, "backups"),
		Logger: log.New(io.Discard, "", 0), Now: time.Now,
		VolumeIdentityCheck: func(path string) (string, error) {
			if strings.HasSuffix(path, "backups") {
				return "private-volume-b", nil
			}
			return "private-volume-a", nil
		},
		DiskSpaceCheck: func(string) (uint64, uint64, error) {
			checks++
			return 10 << 30, 100 << 30, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	recorder := performRequest(router.Engine, http.MethodGet, "/api/v1/diagnostics/storage", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("storage diagnostics status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if checks != 2 || strings.Contains(recorder.Body.String(), "private-volume") || strings.Contains(recorder.Body.String(), root) {
		t.Fatalf("shared-volume response checks=%d body=%s", checks, recorder.Body.String())
	}
	var envelope struct {
		Data storageCapacityResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.Locations[0].SharedVolume || !envelope.Data.Locations[1].SharedVolume || envelope.Data.Locations[2].SharedVolume {
		t.Fatalf("shared volume flags=%#v", envelope.Data.Locations)
	}
}

func assertStorageIncidentCount(t *testing.T, store *database.Store, want int64) {
	t.Helper()
	var count int64
	if err := store.DB.Model(&models.InboxItem{}).Where("source_entity_id = ?", "storage:low_space").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("storage incident count=%d, want %d", count, want)
	}
}
