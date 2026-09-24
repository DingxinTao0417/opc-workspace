package database

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestAICompleteAccessRequestMigrationPreservesHistoryAndGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "complete-scopes.db")
	old := openDatabaseAtVersion(t, path, 78)
	t.Cleanup(func() { _ = old.Close() })
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('s','Saved',1,'stamp','stamp')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:1/v1','test','ready','healthy',0,1,1,'stamp','stamp','stamp')`,
	} {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"open", "dismissed", "consumed"} {
		if _, err := old.Exec(`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES (?,'s','p','completed','stamp','stamp')`, status); err != nil {
			t.Fatal(err)
		}
		if _, err := old.Exec(`INSERT INTO ai_workspace_access_requests VALUES (?,'["work","actions"]','open','stamp','stamp')`, status); err != nil {
			t.Fatal(err)
		}
		if status != "open" {
			if _, err := old.Exec(`UPDATE ai_workspace_access_requests SET status=? WHERE generation_id=?`, status, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	old.Close()
	gated, gate, err := OpenBeforeDestructiveMigrations(path)
	if err != nil {
		t.Fatal(err)
	}
	if gate == nil || gate.CurrentVersion != 78 || gate.TargetVersion != 79 || !reflect.DeepEqual(gate.PendingVersions, []int{79}) {
		t.Fatalf("gate=%+v", gate)
	}
	gated.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, expected := range []string{"open", "dismissed", "consumed"} {
		var scopes, status, created, updated string
		if err := store.SQL.QueryRow(`SELECT scopes_json,status,created_at,updated_at FROM ai_workspace_access_requests WHERE generation_id=?`, expected).Scan(&scopes, &status, &created, &updated); err != nil {
			t.Fatal(err)
		}
		if scopes != `["work","actions"]` || status != expected || created != "stamp" || updated != "stamp" {
			t.Fatalf("history changed: %s %s %s %s", scopes, status, created, updated)
		}
	}
	if _, err := store.SQL.Exec(`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('complete','s','p','completed','stamp','stamp')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`INSERT INTO ai_workspace_access_requests VALUES ('complete','["work","actions","outputs","clients"]','open','stamp','stamp')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`UPDATE ai_workspace_access_requests SET scopes_json='["finance"]' WHERE generation_id='complete'`); err == nil {
		t.Fatal("scope history became mutable")
	}
	if _, err := store.SQL.Exec(`DELETE FROM ai_generations WHERE id='complete'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.SQL.QueryRow(`SELECT count(*) FROM ai_workspace_access_requests WHERE generation_id='complete'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade count=%d err=%v", count, err)
	}
}

func TestAIWorkspaceAccessRequestMigrationRejectsGrantsAndInvalidStates(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "access-request.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('saved-session','Saved',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('ephemeral-session','Ephemeral',0,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('provider','Provider','local','openai_chat','http://127.0.0.1:1/v1','test','ready','healthy',0,1,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('saved-complete','saved-session','provider','completed','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('saved-failed','saved-session','provider','failed','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('ephemeral-complete','ephemeral-session','provider','completed','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
	} {
		if err := store.DB.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	insert := func(generation, scopes, status string) error {
		return store.DB.Exec(`INSERT INTO ai_workspace_access_requests(generation_id,scopes_json,status,created_at,updated_at) VALUES (?,?,?,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`, generation, scopes, status).Error
	}
	for _, test := range []struct{ generation, scopes, status string }{
		{"saved-failed", `["work"]`, "open"},
		{"ephemeral-complete", `["work"]`, "open"},
		{"saved-complete", `["work","work"]`, "open"},
		{"saved-complete", `["unknown"]`, "open"},
		{"saved-complete", `["work"]`, "granted"},
		{"saved-complete", `["work"]`, "dismissed"},
	} {
		if err := insert(test.generation, test.scopes, test.status); err == nil {
			t.Fatalf("accepted invalid request: %+v", test)
		}
	}
	if err := insert("saved-complete", `["work","actions"]`, "open"); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`UPDATE ai_workspace_access_requests SET scopes_json='["finance"]' WHERE generation_id='saved-complete'`).Error; err == nil {
		t.Fatal("persisted scope was rewritten")
	}
	if err := store.DB.Exec(`UPDATE ai_workspace_access_requests SET status='dismissed' WHERE generation_id='saved-complete'`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec(`UPDATE ai_workspace_access_requests SET status='open' WHERE generation_id='saved-complete'`).Error; err == nil {
		t.Fatal("dismissed request was reopened")
	}
}
