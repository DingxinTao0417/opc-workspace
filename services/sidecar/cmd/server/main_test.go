package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/api"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/runlease"
)

func TestWatchStdinForShutdown(t *testing.T) {
	for _, exitOnEOF := range []bool{false, true} {
		t.Run(fmt.Sprintf("exit_on_eof_%t", exitOnEOF), func(t *testing.T) {
			shutdown := watchStdinForShutdown(strings.NewReader("ignored\nshutdown\n"), log.New(io.Discard, "", 0), exitOnEOF)
			select {
			case <-shutdown:
			case <-time.After(time.Second):
				t.Fatal("shutdown control message was not observed")
			}
		})
	}
}

func TestWatchStdinIgnoresEOF(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader(""), log.New(io.Discard, "", 0), false)
	select {
	case <-shutdown:
		t.Fatal("EOF must not request shutdown")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestWatchStdinRequestsShutdownOnEOFWhenEnabled(t *testing.T) {
	shutdown := watchStdinForShutdown(strings.NewReader(""), log.New(io.Discard, "", 0), true)
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("EOF did not request shutdown when enabled")
	}
}

func TestWatchStdinDoesNotTreatScannerErrorAsEOF(t *testing.T) {
	logWrites := make(chan string, 1)
	shutdown := watchStdinForShutdown(
		errorReader{err: errors.New("read failed")},
		log.New(channelWriter{writes: logWrites}, "", 0),
		true,
	)

	select {
	case entry := <-logWrites:
		if !strings.Contains(entry, "stdin control channel failed: read failed") {
			t.Fatalf("log entry = %q, want scanner error", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("scanner error was not logged")
	}
	select {
	case <-shutdown:
		t.Fatal("scanner error must not request shutdown")
	default:
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type channelWriter struct {
	writes chan<- string
}

func (writer channelWriter) Write(value []byte) (int, error) {
	writer.writes <- string(value)
	return len(value), nil
}

func TestRunRejectsHeldDatabaseLeaseBeforeDatabaseOpen(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	lease, err := runlease.Acquire(databasePath)
	if err != nil {
		t.Fatalf("acquire fixture run lease: %v", err)
	}
	t.Cleanup(func() { _ = lease.Close() })

	code := run([]string{
		"--dev",
		"--db", databasePath,
		"--artifacts", filepath.Join(root, "artifacts"),
		"--backups", filepath.Join(root, "backups"),
		"--logs", filepath.Join(root, "logs"),
	})
	if code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was touched before lease conflict returned: Stat error = %v", err)
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
