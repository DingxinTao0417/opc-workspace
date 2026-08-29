package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestUnexpectedRuntimeDatabaseErrorProjectsSanitizedMaintenanceInboxItem(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	logDir := filepath.Join(root, "logs")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router, err := NewRouter(store.DB, Options{
		AppVersion: "0.1.0-test", Commit: "runtime-db-test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), ArtifactDir: filepath.Join(root, "artifacts"),
		DatabasePath: databasePath, BackupDir: filepath.Join(root, "backups"), LogDir: logDir,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	if err := store.DB.Exec("DROP TABLE clients").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router.Engine, http.MethodGet, "/api/v1/clients", nil, nil)
	if response.Code != http.StatusInternalServerError || responseErrorCode(t, response.Body.Bytes()) != "INTERNAL_ERROR" {
		t.Fatalf("runtime database response = %d: %s", response.Code, response.Body.String())
	}
	var item models.InboxItem
	if err := store.DB.Where("source_entity_type = ? AND source_entity_id = ?", systemMaintenanceInboxSourceType, "database:runtime").Take(&item).Error; err != nil {
		t.Fatalf("find runtime incident: %v", err)
	}
	joined := item.Title + item.Summary + item.PayloadJSON
	if strings.Contains(joined, databasePath) || strings.Contains(joined, "no such table") || strings.Contains(joined, testToken) {
		t.Fatalf("runtime incident leaked raw database details: %s", joined)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil || payload["failure_code"] != "database_runtime_failed" {
		t.Fatalf("runtime incident payload=%v err=%v", payload, err)
	}
	journalPath, _ := startupIncidentJournalPath(logDir)
	if _, err := os.Lstat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("healthy projection left fallback journal: %v", err)
	}
}

func TestRuntimeDatabaseFailureFallsBackToDurableSafeJournalAndReplays(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	logDir := filepath.Join(root, "logs")
	store, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{db: store.DB, options: Options{
		LogDir: logDir, Now: func() time.Time { return time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC) },
		Logger: log.New(io.Discard, "", 0),
	}}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	api.recordRuntimeDatabaseFailure("runtime-request")
	journalPath, err := startupIncidentJournalPath(logDir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := readStartupIncidentJournal(journalPath)
	if err != nil || len(journal.Incidents) != 1 || journal.Incidents[0].Kind != StartupIncidentDatabaseRuntime {
		t.Fatalf("runtime fallback journal=%#v err=%v", journal, err)
	}

	reopened, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := ReplayStartupIncidents(reopened.DB, logDir, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("ReplayStartupIncidents() error = %v", err)
	}
	var count int64
	if err := reopened.DB.Model(&models.InboxItem{}).Where("source_entity_id = ?", "database:runtime").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("replayed runtime incident count=%d err=%v", count, err)
	}
	if _, err := os.Lstat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("replayed journal was not removed: %v", err)
	}
}
