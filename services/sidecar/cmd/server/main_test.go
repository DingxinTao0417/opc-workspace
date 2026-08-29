package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/api"
	"github.com/opc-workspace/opc-sidecar/internal/database"
)

func TestWatchStdinForShutdown(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader("ignored\nshutdown\n"), log.New(io.Discard, "", 0))
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown control message was not observed")
	}
}

func TestWatchStdinIgnoresEOF(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader(""), log.New(io.Discard, "", 0))
	select {
	case <-shutdown:
		t.Fatal("EOF must not request shutdown")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestRunJournalsDatabaseStartupFailureForNextHealthyOpen(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write blocked parent fixture: %v", err)
	}
	logDir := filepath.Join(root, "logs")
	code := run([]string{
		"--dev",
		"--db", filepath.Join(blockedParent, "workspace.db"),
		"--artifacts", filepath.Join(root, "artifacts"),
		"--backups", filepath.Join(root, "backups"),
		"--logs", logDir,
	})
	if code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	logContent, err := os.ReadFile(filepath.Join(logDir, "opc-sidecar.log"))
	if err != nil {
		t.Fatalf("read operational log: %v", err)
	}
	if !strings.Contains(string(logContent), "database initialization failed") {
		t.Fatalf("operational log does not contain the safe startup stage: %s", logContent)
	}

	store, err := database.Open(filepath.Join(root, "healthy.db"))
	if err != nil {
		t.Fatalf("open healthy replay database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := api.ReplayStartupIncidents(store.DB, logDir, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("replay startup failure: %v", err)
	}
	var count int64
	if err := store.DB.Table("inbox_items").Where(
		"source_entity_type = 'system_maintenance' AND source_entity_id = 'database:startup'",
	).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("database startup incidents = %d err=%v", count, err)
	}
}
