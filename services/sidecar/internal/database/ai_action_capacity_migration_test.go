package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIActionCapacityUpgradePreservesDecisionsAndRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capacity.db")
	old := openDatabaseAtVersion(t, path, 72)
	t.Cleanup(func() { _ = old.Close() })
	for _, sql := range []string{
		`INSERT INTO ai_sessions(id,title,persist,version,created_at,updated_at) VALUES ('s','Keep',1,1,'2026-09-18T00:00:00Z','2026-09-18T00:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:11434/v1','test','ready','healthy',0,1,1,'2026-09-18T00:00:00Z','2026-09-18T00:00:00Z','2026-09-18T00:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('g','s','p','completed','2026-09-18T00:00:00Z','2026-09-18T00:00:00Z')`,
	} {
		if _, err := old.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"pending", "confirmed", "rejected"} {
		var result, version, decided any
		if status != "pending" {
			decided = "2026-09-18T00:01:00Z"
		}
		if status == "confirmed" {
			result, version = "actual-task", 7
		}
		_, err := old.Exec(`INSERT INTO ai_action_proposals(id,generation_id,fingerprint,action_json,preview_json,status,result_id,result_version,created_at,decided_at) VALUES (?,'g',?,'{"action":"task.create"}','{"label":"Keep exact bytes"}',?,?,?,'2026-09-18T00:00:00Z',?)`,
			status, strings.Repeat(status[:1], 64), status, result, version, decided)
		if err != nil {
			t.Fatal(err)
		}
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var item migration
	for _, candidate := range migrations {
		if candidate.version == 73 {
			item = candidate
			break
		}
	}
	if item.version != 73 {
		t.Fatal("schema 73 migration not found")
	}
	// Failure after copying and replacing must restore the old table AND trigger.
	broken := item
	broken.sql += "; SELECT * FROM deliberately_missing_capacity_table;"
	conn, err := old.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(context.Background(), conn, broken); err == nil {
		t.Fatal("expected rollback")
	}
	_ = conn.Close()
	var count int
	if err := old.QueryRow("SELECT COUNT(*) FROM ai_action_proposals").Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if _, err := old.Exec("UPDATE ai_action_proposals SET preview_json='{}' WHERE id='pending'"); err == nil {
		t.Fatal("rollback lost immutable trigger")
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
	if err := gated.SQL.QueryRow("SELECT COUNT(*) FROM ai_action_proposals").Scan(&count); err != nil || count != 3 {
		_ = gated.Close()
		t.Fatalf("gated proposal count=%d err=%v", count, err)
	}
	if err := gated.Close(); err != nil {
		t.Fatal(err)
	}
	// Open is the explicit destructive-migration authorization after the gate
	// has preserved and exposed the schema-74 database.
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SchemaVersion != 79 {
		t.Fatalf("authorized schema=%d, want 79", store.SchemaVersion)
	}
	for _, status := range []string{"pending", "confirmed", "rejected"} {
		var got, action, preview string
		if err := store.SQL.QueryRow("SELECT status,action_json,preview_json FROM ai_action_proposals WHERE id=?", status).Scan(&got, &action, &preview); err != nil {
			t.Fatal(err)
		}
		if got != status || action != `{"action":"task.create"}` || preview != `{"label":"Keep exact bytes"}` {
			t.Fatal(got, action, preview)
		}
	}
	var result string
	var version int
	if err := store.SQL.QueryRow("SELECT result_id,result_version FROM ai_action_proposals WHERE id='confirmed'").Scan(&result, &version); err != nil || result != "actual-task" || version != 7 {
		t.Fatal(result, version, err)
	}
	action := `{"body":"` + strings.Repeat(`\u003c`, 10000) + `"}`
	preview := `{"before":` + action + `,"after":` + action + `}`
	insert := `INSERT INTO ai_action_proposals(id,generation_id,fingerprint,action_json,preview_json,status,created_at) VALUES (?,'g',?,?,?,'pending','2026-09-18T00:00:00Z')`
	if _, err := store.SQL.Exec(insert, "long", strings.Repeat("l", 64), action, preview); err != nil {
		t.Fatal(err)
	}
	for _, values := range [][2]string{{`{"body":"` + strings.Repeat("a", 65536) + `"}`, "{}"}, {"{}", `{"body":"` + strings.Repeat("b", 131072) + `"}`}, {"not-json", "{}"}} {
		if _, err := store.SQL.Exec(insert, "invalid", strings.Repeat("i", 64), values[0], values[1]); err == nil {
			t.Fatal("accepted invalid or oversized JSON")
		}
	}
	if _, err := store.SQL.Exec("UPDATE ai_action_proposals SET action_json='{}' WHERE id='long'"); err == nil {
		t.Fatal("identity mutable after upgrade")
	}
	if _, err := store.SQL.Exec("UPDATE ai_action_proposals SET status='pending',decided_at=NULL,result_id=NULL,result_version=NULL WHERE id='confirmed'"); err == nil {
		t.Fatal("terminal replay permitted")
	}
	if _, err := store.SQL.Exec("DELETE FROM ai_generations WHERE id='g'"); err != nil {
		t.Fatal(err)
	}
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_action_proposals").Scan(&count); err != nil || count != 0 {
		t.Fatal("cascade", count, err)
	}
}
