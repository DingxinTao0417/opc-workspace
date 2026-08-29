package api

import (
	"errors"
	"io"
	"log"
	"path/filepath"
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
