package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIActionProposalsMigrationPreserves71AndGuardsApprovalIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals.db")
	old := openDatabaseAtVersion(t, path, 71)
	t.Cleanup(func() { _ = old.Close() })
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,version,created_at,updated_at) VALUES ('s','Keep this conversation',1,1,'2026-09-18T12:00:00Z','2026-09-18T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:11434/v1','test','ready','healthy',0,1,1,'2026-09-18T12:00:00Z','2026-09-18T12:00:00Z','2026-09-18T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('g','s','p','completed','2026-09-18T12:00:00Z','2026-09-18T12:00:00Z')`,
	} {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatal(err)
	}
	if gated.SchemaVersion != 74 || gate == nil || gate.CurrentVersion != 74 || gate.TargetVersion != 79 ||
		len(gate.PendingVersions) != 5 || gate.PendingVersions[0] != 75 || gate.PendingVersions[1] != 76 || gate.PendingVersions[2] != 77 || gate.PendingVersions[3] != 78 || gate.PendingVersions[4] != 79 {
		_ = gated.Close()
		t.Fatalf("schema 075 destructive gate: store=%d gate=%#v", gated.SchemaVersion, gate)
	}
	var title string
	if err := gated.SQL.QueryRow("SELECT title FROM ai_sessions WHERE id='s'").Scan(&title); err != nil || title != "Keep this conversation" {
		_ = gated.Close()
		t.Fatalf("gated existing session=%q %v", title, err)
	}
	if err := gated.Close(); err != nil {
		t.Fatal(err)
	}
	// Explicit authorization applies the destructive schema-075 normalization
	// only after the caller has observed the pending gate.
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SchemaVersion != 79 {
		t.Fatalf("authorized schema=%d, want 79", store.SchemaVersion)
	}
	if err := store.SQL.QueryRow("SELECT title FROM ai_sessions WHERE id='s'").Scan(&title); err != nil || title != "Keep this conversation" {
		t.Fatalf("existing session=%q %v", title, err)
	}
	insert := `INSERT INTO ai_action_proposals(id,generation_id,fingerprint,action_json,preview_json,status,created_at) VALUES ('a','g',?,'{"action":"task.create"}','{}','pending','2026-09-18T12:00:00Z')`
	if _, err := store.SQL.Exec(insert, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE ai_action_proposals SET action_json='{}' WHERE id='a'`,
		`UPDATE ai_action_proposals SET status='confirmed',decided_at='2026-09-18T12:01:00Z' WHERE id='a'`,
		`UPDATE ai_action_proposals SET generation_id='missing' WHERE id='a'`,
	} {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("accepted unsafe edit: %s", statement)
		}
	}
	if _, err := store.SQL.Exec(`UPDATE ai_action_proposals SET status='rejected',decided_at='2026-09-18T12:01:00Z' WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`UPDATE ai_action_proposals SET status='confirmed',result_id='task',result_version=1 WHERE id='a'`); err == nil {
		t.Fatal("terminal decision changed")
	}
	if _, err := store.SQL.Exec(`DELETE FROM ai_generations WHERE id='g'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`DELETE FROM ai_sessions WHERE id='s'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_action_proposals").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade count=%d err=%v", count, err)
	}
}
