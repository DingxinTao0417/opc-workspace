package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestClientFollowupMigrationCreatesAuditableLocalPlanningFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v34-to-v35.db")
	v34 := openDatabaseAtVersion(t, databasePath, 34)
	if err := v34.Close(); err != nil {
		t.Fatalf("close v34 fixture: %v", err)
	}

	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v34 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 44 {
		t.Fatalf("SchemaVersion = %d, want 44", store.SchemaVersion)
	}

	const clientID = "018f0000-0000-7000-8000-000000003501"
	const followupID = "018f0000-0000-7000-8000-000000003502"
	if _, err := store.SQL.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, 'Followup Client', 'active', '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, clientID); err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_followups(
			id, client_id, assigned_actor_id, scheduled_at, timezone, channel, purpose, status, priority, created_at, updated_at
		) VALUES (?, ?, ?, '2026-09-01T09:00:00Z', 'Asia/Shanghai', 'phone', 'Confirm next milestone', 'planned', 'high', '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, followupID, clientID, models.BuiltinOwnerActorID); err != nil {
		t.Fatalf("insert planned followup: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM clients WHERE id = ?", clientID); got != 2 {
		t.Fatalf("client version after followup insert = %d, want 2", got)
	}

	if _, err := store.SQL.Exec(`
		UPDATE client_followups
		SET status = 'completed', completed_at = '2026-09-01T09:30:00Z', result = 'Confirmed', next_step = 'Send summary', version = version + 1, updated_at = '2026-09-01T09:30:00Z'
		WHERE id = ?
	`, followupID); err != nil {
		t.Fatalf("complete followup: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT version FROM clients WHERE id = ?", clientID); got != 3 {
		t.Fatalf("client version after completion = %d, want 3", got)
	}

	if _, err := store.SQL.Exec(`UPDATE client_followups SET status = 'planned', version = version + 1 WHERE id = ?`, followupID); err == nil {
		t.Fatal("terminal followup status unexpectedly reopened")
	}
	if _, err := store.SQL.Exec(`DELETE FROM clients WHERE id = ?`, clientID); err == nil {
		t.Fatal("client with followup history unexpectedly deleted")
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO client_followups(
			id, client_id, assigned_actor_id, scheduled_at, timezone, channel, purpose, status, priority, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000003503', ?, '00000000-0000-5000-8000-000000000002', '2026-09-02T09:00:00Z', 'Asia/Shanghai', 'phone', 'Invalid assignee', 'planned', 'normal', '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
	`, clientID); err == nil {
		t.Fatal("system actor unexpectedly accepted as followup assignee")
	}

	assertForeignKey(t, store.SQL, "client_followups", "client_id", "clients", "RESTRICT")
	assertForeignKey(t, store.SQL, "client_followups", "assigned_actor_id", "actors", "RESTRICT")
	assertNoForeignKeyViolations(t, store.SQL)

	var status string
	if err := store.SQL.QueryRow("SELECT status FROM client_followups WHERE id = ?", followupID).Scan(&status); err != nil && err != sql.ErrNoRows {
		t.Fatalf("read followup status: %v", err)
	}
}
