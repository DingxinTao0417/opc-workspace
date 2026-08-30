package main

import (
	"bytes"
	"encoding/json"
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

func TestWriteStartupStageUsesBoundedProtocol(t *testing.T) {
	var output bytes.Buffer
	if err := writeStartupStage(&output, "checking_pending_restore"); err != nil {
		t.Fatalf("write startup stage: %v", err)
	}
	var payload startupEvent
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode startup stage: %v", err)
	}
	if payload != (startupEvent{Event: "startup", Stage: "checking_pending_restore"}) {
		t.Fatalf("startup event = %#v", payload)
	}
}

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

func TestRunRejectsBlockedInvoicePDFDirectoryBeforeDatabaseOpen(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "workspace.db")
	blockedInvoicePath := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedInvoicePath, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write blocked invoice fixture: %v", err)
	}

	code := run([]string{
		"--dev",
		"--db", databasePath,
		"--artifacts", filepath.Join(root, "artifacts"),
		"--invoices", blockedInvoicePath,
		"--backups", filepath.Join(root, "backups"),
		"--logs", filepath.Join(root, "logs"),
	})
	if code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
	if _, err := os.Stat(databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database was touched before invoice PDF directory failure returned: Stat error = %v", err)
	}
}

func TestPrepareInvoicePDFDirectoryRejectsSymbolicLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create invoice target fixture: %v", err)
	}
	link := filepath.Join(root, "invoices-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable in this environment: %v", err)
	}

	if err := prepareInvoicePDFDirectory(link); err == nil {
		t.Fatal("expected symbolic invoice PDF directory to be rejected")
	}
}

func TestPrepareInvoicePDFDirectoryRejectsSymbolicParentBeforeCreation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create invoice target fixture: %v", err)
	}
	link := filepath.Join(root, "linked-parent")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable in this environment: %v", err)
	}

	if err := prepareInvoicePDFDirectory(filepath.Join(link, "invoices")); err == nil {
		t.Fatal("expected symbolic invoice PDF parent to be rejected")
	}
	if _, err := os.Stat(filepath.Join(target, "invoices")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected path traversal created a directory in the symlink target: %v", err)
	}
}

func TestPrepareInvoicePDFDirectoryDurablyCreatesDirectoryChain(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "level-one", "level-two", "invoices")
	originalSync := syncInvoicePDFDirectory
	t.Cleanup(func() { syncInvoicePDFDirectory = originalSync })

	var synced []string
	syncInvoicePDFDirectory = func(path string) error {
		synced = append(synced, filepath.Clean(path))
		return nil
	}
	if err := prepareInvoicePDFDirectory(target); err != nil {
		t.Fatalf("prepare invoice PDF directory: %v", err)
	}
	want := []string{
		filepath.Clean(root),
		filepath.Join(root, "level-one"),
		filepath.Join(root, "level-one", "level-two"),
	}
	if fmt.Sprint(synced) != fmt.Sprint(want) {
		t.Fatalf("synced parents = %v, want %v", synced, want)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("prepared invoice PDF path is not a directory: info=%v err=%v", info, err)
	}
}

func TestPrepareInvoicePDFDirectoryCompensatesSyncFailure(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "level-one", "invoices")
	originalSync := syncInvoicePDFDirectory
	t.Cleanup(func() { syncInvoicePDFDirectory = originalSync })

	syncCalls := 0
	syncInvoicePDFDirectory = func(string) error {
		syncCalls++
		if syncCalls == 2 {
			return errors.New("forced directory sync failure")
		}
		return nil
	}
	if err := prepareInvoicePDFDirectory(target); err == nil || !strings.Contains(err.Error(), "forced directory sync failure") {
		t.Fatalf("prepare error = %v, want forced sync failure", err)
	}
	if _, err := os.Stat(filepath.Join(root, "level-one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed durable creation was not compensated: %v", err)
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
		"--invoices", filepath.Join(root, "invoices"),
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
