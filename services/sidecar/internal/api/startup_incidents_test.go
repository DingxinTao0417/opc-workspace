package api

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestStartupIncidentJournalReplaysSafeUniqueFacts(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	first := time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)
	for _, input := range []struct {
		kind StartupIncidentKind
		at   time.Time
	}{
		{StartupIncidentDatabaseStartup, first},
		{StartupIncidentDatabaseStartup, first.Add(time.Hour)},
		{StartupIncidentDatabaseMigration, first.Add(2 * time.Hour)},
		{StartupIncidentSidecarStartup, first.Add(3 * time.Hour)},
	} {
		if err := RecordStartupIncident(logDir, input.kind, input.at); err != nil {
			t.Fatalf("RecordStartupIncident(%s): %v", input.kind, err)
		}
	}

	journalPath := filepath.Join(logDir, startupIncidentJournalName)
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read startup incident journal: %v", err)
	}
	for _, forbidden := range []string{"token", "request", "error", logDir} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Fatalf("startup journal leaked %q: %s", forbidden, raw)
		}
	}
	journal, err := readStartupIncidentJournal(journalPath)
	if err != nil {
		t.Fatalf("validate startup incident journal: %v", err)
	}
	if len(journal.Incidents) != 3 || journal.Incidents[0].OccurredAt != first.Format(time.RFC3339Nano) {
		t.Fatalf("startup incident journal = %#v", journal)
	}

	store, err := database.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("open replay database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := ReplayStartupIncidents(store.DB, logDir, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("ReplayStartupIncidents: %v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("replayed startup journal still exists: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance'", 3)
	for _, incident := range []systemMaintenanceIncident{
		databaseStartupMaintenanceIncident,
		databaseMigrationMaintenanceIncident,
		sidecarStartupMaintenanceIncident,
	} {
		var item models.InboxItem
		sourceID := systemMaintenanceSourceID(incident.component, incident.operation)
		if err := store.DB.Where("source_entity_type = ? AND source_entity_id = ?", systemMaintenanceInboxSourceType, sourceID).Take(&item).Error; err != nil {
			t.Fatalf("load replayed %s incident: %v", sourceID, err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
			t.Fatalf("decode %s payload: %v", sourceID, err)
		}
		if len(payload) != 5 || payload["failure_code"] != incident.failureCode || payload["message"] != incident.message {
			t.Fatalf("replayed %s payload = %#v", sourceID, payload)
		}
	}
}

func TestStartupIncidentReplayIsStableAfterCleanupAmbiguity(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	if err := RecordStartupIncident(logDir, StartupIncidentDatabaseMigration, time.Now().UTC()); err != nil {
		t.Fatalf("record startup incident: %v", err)
	}
	journalPath := filepath.Join(logDir, startupIncidentJournalName)
	raw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read replay fixture: %v", err)
	}
	store, err := database.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("open replay database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	logger := log.New(io.Discard, "", 0)
	if err := ReplayStartupIncidents(store.DB, logDir, logger); err != nil {
		t.Fatalf("first replay: %v", err)
	}
	if err := store.DB.Model(&models.InboxItem{}).
		Where("source_entity_type = ? AND source_entity_id = ?", systemMaintenanceInboxSourceType, "database:migration").
		Updates(map[string]any{
			"status": "resolved", "triaged_at": time.Now().UTC().Format(time.RFC3339Nano),
			"resolved_by_actor_id": models.BuiltinOwnerActorID,
			"resolved_at":          time.Now().UTC().Format(time.RFC3339Nano),
			"resolution_reason":    "acknowledged", "resolution_mode": "manual", "version": 2,
		}).Error; err != nil {
		t.Fatalf("resolve projected incident fixture: %v", err)
	}
	if err := os.WriteFile(journalPath, raw, 0o600); err != nil {
		t.Fatalf("restore ambiguous journal fixture: %v", err)
	}
	if err := ReplayStartupIncidents(store.DB, logDir, logger); err != nil {
		t.Fatalf("ambiguous replay: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'system_maintenance' AND source_entity_id = 'database:migration'", 1)
}

func TestRecordStartupIncidentQuarantinesInvalidJournal(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("create log directory: %v", err)
	}
	journalPath := filepath.Join(logDir, startupIncidentJournalName)
	if err := os.WriteFile(journalPath, []byte(`{"unknown":"unsafe"}`), 0o600); err != nil {
		t.Fatalf("write invalid journal: %v", err)
	}
	if err := RecordStartupIncident(logDir, StartupIncidentSidecarStartup, time.Now().UTC()); err != nil {
		t.Fatalf("record after invalid journal: %v", err)
	}
	if _, err := readStartupIncidentJournal(journalPath); err != nil {
		t.Fatalf("replacement journal is invalid: %v", err)
	}
	invalid, err := filepath.Glob(filepath.Join(logDir, ".startup-incidents-invalid-*.json"))
	if err != nil || len(invalid) != 1 {
		t.Fatalf("quarantined journals = %v err=%v", invalid, err)
	}
}
