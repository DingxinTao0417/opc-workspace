package database

import (
	"path/filepath"
	"testing"
)

func TestAIWorkPlanMigrationPreserves75AndImmutableRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	old := openDatabaseAtVersion(t, path, 75)
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,version,created_at,updated_at) VALUES ('s','Keep',1,1,'2026-09-19T12:00:00Z','2026-09-19T12:00:00Z')`,
		`INSERT INTO ai_sessions(id,title,persist,version,created_at,updated_at) VALUES ('other','Other',0,1,'2026-09-19T12:00:00Z','2026-09-19T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:1/v1','test','ready','healthy',0,1,1,'2026-09-19T12:00:00Z','2026-09-19T12:00:00Z','2026-09-19T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('g','s','p','streaming','2026-09-19T12:00:00Z','2026-09-19T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('other-g','other','p','streaming','2026-09-19T12:00:00Z','2026-09-19T12:00:00Z')`,
	} {
		if _, err := old.Exec(statement); err != nil {
			_ = old.Close()
			t.Fatal(err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	store, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if gate == nil || gate.CurrentVersion != 78 || gate.TargetVersion != 79 || store.SchemaVersion != 78 {
		t.Fatalf("additive migration gated: %d %+v", store.SchemaVersion, gate)
	}
	const insert = `INSERT INTO ai_work_plan_revisions(session_id,version,generation_id,plan_json,created_at) VALUES (?,?,?,?, '2026-09-19T12:00:00Z')`
	if _, err := store.SQL.Exec(insert, "s", 1, "g", `{"title":"Plan","steps":[]}`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE ai_work_plan_revisions SET plan_json='{}' WHERE session_id='s'`,
		`UPDATE ai_work_plan_revisions SET generation_id='other-g' WHERE session_id='s'`,
		`UPDATE ai_work_plan_revisions SET version=2 WHERE session_id='s'`,
	} {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("mutable plan: %s", statement)
		}
	}
	for _, args := range [][]any{{"s", 1, "g", "{}"}, {"s", 3, "g", "{}"}, {"s", 2, "other-g", "{}"}, {"other", 1, "other-g", "{}"}, {"s", 2, "g", "invalid JSON"}} {
		if _, err := store.SQL.Exec(insert, args...); err == nil {
			t.Fatalf("invalid revision accepted: %v", args)
		}
	}
	if _, err := store.SQL.Exec(`UPDATE ai_generations SET status='completed' WHERE id='g'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(insert, "s", 2, "g", "{}"); err == nil {
		t.Fatal("terminal generation wrote plan")
	}
	var title string
	if err := store.SQL.QueryRow(`SELECT title FROM ai_sessions WHERE id='s'`).Scan(&title); err != nil || title != "Keep" {
		t.Fatalf("existing conversation damaged: %q %v", title, err)
	}
	// Session API deletes generations/messages first (their historical FKs
	// intentionally restrict direct session deletion), then the session.
	if _, err := store.SQL.Exec(`DELETE FROM ai_generations WHERE session_id='s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`DELETE FROM ai_sessions WHERE id='s'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.SQL.QueryRow(`SELECT COUNT(*) FROM ai_work_plan_revisions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade=%d %v", count, err)
	}
}
