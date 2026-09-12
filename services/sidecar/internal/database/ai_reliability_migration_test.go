package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIReliabilityMigrationPreservesHistoryAndGuardsIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-reliability.db")
	old := openDatabaseAtVersion(t, path, 68)
	for _, query := range []string{
		`INSERT INTO ai_providers(id,name,protocol,base_url,model,created_at,updated_at) VALUES ('provider','preserved','openai_chat','http://127.0.0.1:11434','model','2026-09-10T00:00:00Z','2026-09-10T00:00:00Z')`,
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('session','preserved',1,'2026-09-10T00:00:00Z','2026-09-10T00:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,content,created_at,updated_at) VALUES ('old-run','session','provider','completed','original answer','2026-09-10T00:00:00Z','2026-09-10T00:00:00Z')`,
		`INSERT INTO ai_messages(id,session_id,role,content,task_id,task_title_snapshot,created_at,updated_at) VALUES ('message','session','assistant','original answer','deleted-task','original task','2026-09-10T00:00:00Z','2026-09-10T00:00:00Z')`,
		`INSERT INTO ai_memory_entries(id,session_id,kind,content,tags,origin,status,created_at,updated_at) VALUES ('proposal','session','memory_proposal','original preference','[]','model_proposal','superseded','2026-09-10T00:00:00Z','2026-09-10T00:01:00Z')`,
		`INSERT INTO workflow_events(id,aggregate_type,aggregate_id,action,current_json,created_at) VALUES ('event','ai_session','session','ai_memory_proposal_confirmed','{"proposal_id":"proposal","memory_id":"00000000-0000-4000-8000-000000000001"}','2026-09-10T00:01:00Z')`,
	} {
		if _, err := old.Exec(query); err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("schema=%d", store.SchemaVersion)
	}
	var body string
	var requestKey, requestHash *string
	if err := store.SQL.QueryRow("SELECT content,request_key,request_hash FROM ai_generations WHERE id='old-run'").Scan(&body, &requestKey, &requestHash); err != nil || body != "original answer" || requestKey != nil || requestHash != nil {
		t.Fatalf("history changed: %q %v", body, err)
	}
	var decision, memoryID string
	if err := store.SQL.QueryRow("SELECT decision,memory_id FROM ai_memory_entries WHERE id='proposal'").Scan(&decision, &memoryID); err != nil || decision != "confirmed" || memoryID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("historic decision not recovered: %q %q %v", decision, memoryID, err)
	}
	if _, err := store.SQL.Exec("UPDATE ai_messages SET task_id='another-task' WHERE id='message'"); err == nil {
		t.Fatal("static binding was changed")
	}
	if _, err := store.SQL.Exec("UPDATE ai_memory_entries SET decision='rejected',memory_id=NULL WHERE id='proposal'"); err == nil {
		t.Fatal("confirmed decision was changed")
	}
	if _, err := store.SQL.Exec(`INSERT INTO ai_memory_entries(id,session_id,kind,content,tags,origin,status,memory_id,created_at,updated_at) VALUES ('invalid-decision','session','memory_proposal','not confirmed','[]','model_proposal','active','00000000-0000-4000-8000-000000000001','2026-09-10T00:02:00Z','2026-09-10T00:02:00Z')`); err == nil {
		t.Fatal("NULL decision accepted a memory id")
	}
	hash := strings.Repeat("a", 64)
	insert := `INSERT INTO ai_generations(id,session_id,provider_id,status,request_key,request_hash,created_at,updated_at) VALUES (?,'session','provider','queued',?,?,'2026-09-10T00:02:00Z','2026-09-10T00:02:00Z')`
	if _, err := store.SQL.Exec(insert, "new-run", "stable-request", hash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(insert, "duplicate", "stable-request", hash); err == nil {
		t.Fatal("duplicate request identity accepted")
	}
	if _, err := store.SQL.Exec(insert, "missing-hash", "other-request", nil); err == nil {
		t.Fatal("unpaired request identity accepted")
	}
	if _, err := store.SQL.Exec("UPDATE ai_generations SET request_hash=? WHERE id='new-run'", strings.Repeat("b", 64)); err == nil {
		t.Fatal("request hash was changed")
	}
	if _, err := store.SQL.Exec(`INSERT INTO ai_memory_entries(id,session_id,kind,content,tags,origin,source_message_id,source_message_offset,created_at,updated_at) VALUES ('snapshot','session','context_snapshot','{"summary":"partial","facts":[]}','[]','model_compaction','message',4,'2026-09-10T00:02:00Z','2026-09-10T00:02:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec("UPDATE ai_memory_entries SET status='superseded',source_message_offset=5,updated_at='2026-09-10T00:03:00Z' WHERE id='snapshot'"); err == nil {
		t.Fatal("partial watermark mutated")
	}
	if _, err := store.SQL.Exec("UPDATE ai_memory_entries SET status='superseded',updated_at='2026-09-10T00:03:00Z' WHERE id='snapshot'"); err != nil {
		t.Fatal(err)
	}
	var fk int
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&fk); err != nil || fk != 0 {
		t.Fatalf("foreign keys: %d %v", fk, err)
	}
}
