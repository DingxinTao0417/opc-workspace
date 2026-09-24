package database

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIContinuationMigrationPreserves76AndAddsBoundedLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuation.db")
	old := openDatabaseAtVersion(t, path, 76)
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,version,created_at,updated_at) VALUES ('s','Keep',1,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z'),('other','Other',1,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z'),('temporary','Temporary',0,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,status,health_status,has_key,version,config_version,last_health_at,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:1/v1','test','ready','healthy',0,1,1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,content,created_at,updated_at) VALUES ('g','s','p','streaming','original answer','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z'),('other-g','other','p','streaming',NULL,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_work_plan_revisions(session_id,version,generation_id,plan_json,created_at) VALUES ('s',1,'g','{"title":"Keep","steps":[]}','2026-09-21T12:00:00Z'),('other',1,'other-g','{"title":"Other","steps":[]}','2026-09-21T12:00:00Z')`,
		`UPDATE ai_generations SET status='completed' WHERE id='g'`,
		`INSERT INTO ai_messages(id,session_id,role,status,content,generation_id,created_at,updated_at) VALUES ('old-answer','s','assistant','completed','original answer','g','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
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
	if gate == nil || gate.CurrentVersion != 78 || gate.TargetVersion != 79 || store.SchemaVersion != 78 {
		t.Fatalf("additive migration: schema=%d gate=%+v", store.SchemaVersion, gate)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var content string
	if err := store.SQL.QueryRow(`SELECT content FROM ai_generations WHERE id='g'`).Scan(&content); err != nil || content != "original answer" {
		t.Fatalf("existing generation changed: %q %v", content, err)
	}
	if _, err := store.SQL.Exec(`INSERT INTO ai_messages(id,session_id,role,status,content,generation_id,created_at,updated_at) VALUES ('continuation-trigger','s','user','completed','Explicit automatic continuation','g','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`); err != nil {
		t.Fatalf("continuation trigger and original answer cannot share generation: %v", err)
	}
	for _, role := range []string{"user", "assistant"} {
		if _, err := store.SQL.Exec(`INSERT INTO ai_messages(id,session_id,role,status,content,generation_id,created_at,updated_at) VALUES (?,'s',?,'completed','duplicate','g','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`, "duplicate-"+role, role); err == nil {
			t.Fatalf("duplicate %s for same generation accepted", role)
		}
	}
	const insert = `INSERT INTO ai_continuations(id,session_id,provider_id,provider_version,provider_config_version,provider_name,provider_kind,provider_protocol,provider_model,workspace_json,initial_plan_version,current_plan_version,last_observation_hash,max_turns,turns_started,status,reason,version,created_at,updated_at,expires_at) VALUES (?,?,'p',1,1,'Local','local','openai_chat','test','{"scopes":["work","outputs","actions"]}',1,1,'',2,0,'waiting','ready',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z','2026-09-21T12:30:00Z')`
	if _, err := store.SQL.Exec(insert, "lease", "s"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]any{{"second", "s"}, {"temporary-lease", "temporary"}, {"missing-lease", "missing"}} {
		if _, err := store.SQL.Exec(insert, args...); err == nil {
			t.Fatalf("invalid lease accepted: %v", args)
		}
	}
	for _, update := range []string{
		`provider_model='different'`, `provider_version=2`, `provider_config_version=2`, `provider_name='Different'`,
		`provider_kind='remote'`, `provider_protocol='anthropic_messages'`, `workspace_json='{}'`,
		`max_turns=3`, `initial_plan_version=2`, `expires_at='2026-09-21T13:00:00Z'`,
		`turns_started=3`, `current_plan_version=2`, `status='unknown'`, `reason='private error body'`,
		`last_observation_hash='INVALID'`, `last_observation_hash='` + strings.Repeat("A", 64) + `'`,
		`current_generation_id='other-g'`, `last_generation_id='other-g'`,
	} {
		if _, err := store.SQL.Exec(`UPDATE ai_continuations SET version=version+1,` + update + ` WHERE id='lease'`); err == nil {
			t.Fatalf("invalid update accepted: %s", update)
		}
	}
	if _, err := store.SQL.Exec(`UPDATE ai_continuations SET reason='run_active' WHERE id='lease'`); err == nil {
		t.Fatal("versionless update accepted")
	}
	hash := strings.Repeat("a", 64)
	tx, err := store.SQL.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('auto-g','s','p','streaming','2026-09-21T12:01:00Z','2026-09-21T12:01:00Z')`,
		`UPDATE ai_continuations SET turns_started=1,status='running',reason='generating',current_generation_id='auto-g',last_generation_id='auto-g',last_observation_hash='` + hash + `',version=version+1 WHERE id='lease'`,
		`INSERT INTO ai_continuation_turns(continuation_id,turn_index,generation_id,plan_version,observation_hash,created_at) VALUES ('lease',1,'auto-g',1,'` + hash + `','2026-09-21T12:01:00Z')`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE ai_continuation_turns SET observation_hash='` + strings.Repeat("b", 64) + `' WHERE continuation_id='lease'`,
		`UPDATE ai_continuations SET turns_started=0,version=version+1 WHERE id='lease'`,
		`INSERT INTO ai_continuation_turns(continuation_id,turn_index,generation_id,plan_version,observation_hash,created_at) VALUES ('lease',2,'other-g',1,'` + hash + `','2026-09-21T12:02:00Z')`,
		`INSERT INTO ai_continuation_turns(continuation_id,turn_index,generation_id,plan_version,observation_hash,created_at) VALUES ('lease',2,'auto-g',1,'` + hash + `','2026-09-21T12:02:00Z')`,
	} {
		if _, err := store.SQL.Exec(statement); err == nil {
			t.Fatalf("invalid turn accepted: %s", statement)
		}
	}
	if _, err := store.SQL.Exec(`UPDATE ai_continuations SET status='interrupted',reason='process_restart',current_generation_id=NULL,version=version+1 WHERE id='lease'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`UPDATE ai_continuations SET status='waiting',reason='ready',version=version+1 WHERE id='lease'`); err == nil {
		t.Fatal("interrupted authorization was revived")
	}
	if _, err := store.SQL.Exec(insert, "new-lease", "s"); err != nil {
		t.Fatalf("fresh explicit authorization blocked: %v", err)
	}
	// Existing session deletion removes its generations before the session.
	for _, statement := range []string{`DELETE FROM ai_messages WHERE session_id='s'`, `DELETE FROM ai_generations WHERE session_id='s'`, `DELETE FROM ai_sessions WHERE id='s'`} {
		if _, err := store.SQL.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"ai_continuations", "ai_continuation_turns"} {
		var count int
		if err := store.SQL.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade %s count=%d: %v", table, count, err)
		}
	}
}

func TestAIContinuationMigrationBoundariesAndAtomicAcceptance(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "bounds.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, statement := range []string{
		`INSERT INTO ai_sessions(id,title,persist,created_at,updated_at) VALUES ('s','Saved',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z'),('other','Other',1,'2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,created_at,updated_at) VALUES ('p','Local','local','openai_chat','http://127.0.0.1:1','model','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('g','s','p','streaming','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z'),('other-g','other','p','streaming','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_work_plan_revisions(session_id,version,generation_id,plan_json,created_at) VALUES ('s',1,'g','{}','2026-09-21T12:00:00Z')`,
	} {
		if _, err := store.SQL.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	base := models.AIContinuation{
		ID: "lease", SessionID: "s", ProviderID: "p", ProviderVersion: 1, ProviderConfigVersion: 1,
		ProviderName: "Local", ProviderKind: "local", ProviderProtocol: "openai_chat", ProviderModel: "model",
		WorkspaceJSON:      `{"provider_version":1,"scopes":["work","outputs","actions"]}`,
		InitialPlanVersion: 1, CurrentPlanVersion: 1, MaxTurns: 8, Status: "waiting", Reason: "ready", Version: 1,
		CreatedAt: "2026-09-21T12:00:00Z", UpdatedAt: "2026-09-21T12:00:00Z", ExpiresAt: "2026-09-21T14:00:00Z",
	}
	for _, test := range []struct {
		name   string
		mutate func(*models.AIContinuation)
	}{
		{"zero turns", func(v *models.AIContinuation) { v.MaxTurns = 0 }},
		{"nine turns", func(v *models.AIContinuation) { v.MaxTurns = 9 }},
		{"negative consumed", func(v *models.AIContinuation) { v.TurnsStarted = -1 }},
		{"over consumed", func(v *models.AIContinuation) { v.TurnsStarted = 9 }},
		{"zero provider version", func(v *models.AIContinuation) { v.ProviderVersion = 0 }},
		{"missing provider", func(v *models.AIContinuation) { v.ProviderID = "missing" }},
		{"zero provider config", func(v *models.AIContinuation) { v.ProviderConfigVersion = 0 }},
		{"unknown kind", func(v *models.AIContinuation) { v.ProviderKind = "unknown" }},
		{"unknown protocol", func(v *models.AIContinuation) { v.ProviderProtocol = "unknown" }},
		{"zero plan", func(v *models.AIContinuation) { v.InitialPlanVersion = 0 }},
		{"plan beyond ledger", func(v *models.AIContinuation) { v.CurrentPlanVersion = 129 }},
		{"unavailable plan", func(v *models.AIContinuation) { v.CurrentPlanVersion = 2 }},
		{"array grant", func(v *models.AIContinuation) { v.WorkspaceJSON = "[]" }},
		{"null grant", func(v *models.AIContinuation) { v.WorkspaceJSON = "null" }},
		{"malformed grant", func(v *models.AIContinuation) { v.WorkspaceJSON = "{" }},
		{"oversized grant", func(v *models.AIContinuation) { v.WorkspaceJSON = `{"value":"` + strings.Repeat("x", 65536) + `"}` }},
		{"bad observation", func(v *models.AIContinuation) { v.LastObservationHash = strings.Repeat("g", 64) }},
		{"uppercase observation", func(v *models.AIContinuation) { v.LastObservationHash = strings.Repeat("A", 64) }},
		{"unknown state", func(v *models.AIContinuation) { v.Status = "retrying" }},
		{"unknown reason", func(v *models.AIContinuation) { v.Reason = "provider private response" }},
		{"zero ttl", func(v *models.AIContinuation) { v.ExpiresAt = v.CreatedAt }},
		{"over two hours", func(v *models.AIContinuation) { v.ExpiresAt = "2026-09-21T14:01:00Z" }},
		{"invalid expiry", func(v *models.AIContinuation) { v.ExpiresAt = "not a timestamp" }},
		{"foreign generation", func(v *models.AIContinuation) { id := "other-g"; v.CurrentGenerationID = &id }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := base
			test.mutate(&value)
			if err := store.DB.Create(&value).Error; err == nil {
				t.Fatal("invalid authorization accepted")
			}
		})
	}
	if err := store.DB.Create(&base).Error; err != nil {
		t.Fatalf("valid 8 turns / 120 minutes boundary rejected: %v", err)
	}
	encoded, err := json.Marshal(base)
	if err != nil || strings.Contains(string(encoded), "workspace_json") || strings.Contains(string(encoded), "scopes") {
		t.Fatalf("model serializes raw authorization: %s %v", encoded, err)
	}
	// A rejected ledger insert must roll back BOTH the debit and generation.
	tx, err := store.SQL.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO ai_generations(id,session_id,provider_id,status,created_at,updated_at) VALUES ('rollback-g','s','p','streaming','2026-09-21T12:01:00Z','2026-09-21T12:01:00Z')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE ai_continuations SET turns_started=1,status='running',current_generation_id='rollback-g',reason='generating',version=2 WHERE id='lease'`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO ai_continuation_turns(continuation_id,turn_index,generation_id,plan_version,observation_hash,created_at) VALUES ('lease',1,'other-g',1,?,'2026-09-21T12:01:00Z')`, strings.Repeat("a", 64)); err == nil {
		_ = tx.Rollback()
		t.Fatal("cross-session generation entered accepted turn")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var lease models.AIContinuation
	if err := store.DB.Take(&lease, "id='lease'").Error; err != nil || lease.TurnsStarted != 0 || lease.Version != 1 || lease.CurrentGenerationID != nil {
		t.Fatalf("failed claim consumed authorization: %+v %v", lease, err)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM ai_generations WHERE id='rollback-g'`, `SELECT COUNT(*) FROM ai_continuation_turns`} {
		var count int
		if err := store.SQL.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial claim survived: %d %v", count, err)
		}
	}
	// An idle lease must not prevent deletion of a provider with no generation
	// history. Retain its frozen identity so the coordinator can stop it safely.
	for _, statement := range []string{
		`INSERT INTO ai_providers(id,name,kind,protocol,base_url,model,created_at,updated_at) VALUES ('idle-provider','Idle local','local','openai_chat','http://127.0.0.1:2','model','2026-09-21T12:00:00Z','2026-09-21T12:00:00Z')`,
		`INSERT INTO ai_work_plan_revisions(session_id,version,generation_id,plan_json,created_at) VALUES ('other',1,'other-g','{}','2026-09-21T12:00:00Z')`,
	} {
		if _, err := store.SQL.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	idle := base
	idle.ID, idle.SessionID, idle.ProviderID = "idle-lease", "other", "idle-provider"
	idle.ProviderName = "Idle local"
	if err := store.DB.Create(&idle).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.SQL.Exec(`DELETE FROM ai_providers WHERE id='idle-provider'`); err != nil {
		t.Fatalf("idle authorization blocks ordinary provider deletion: %v", err)
	}
	if _, err := store.SQL.Exec(`UPDATE ai_continuations SET status='failed',reason='provider_changed',version=2 WHERE id='idle-lease'`); err != nil {
		t.Fatalf("missing provider cannot stop frozen lease: %v", err)
	}
}

func TestAIContinuationMigrationFailureRestoresOldMessageUniqueness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	old := openDatabaseAtVersion(t, path, 76)
	t.Cleanup(func() { _ = old.Close() })
	var indexBefore string
	if err := old.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='idx_ai_messages_generation'`).Scan(&indexBefore); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var selected migration
	for _, migration := range migrations {
		if migration.version == 77 {
			selected = migration
		}
	}
	if selected.version != 77 || selected.destructive {
		t.Fatalf("missing additive migration: %+v", selected)
	}
	broken := selected
	broken.sql += "; SELECT * FROM deliberately_missing_continuation_table;"
	conn, err := old.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(context.Background(), conn, broken); err == nil {
		_ = conn.Close()
		t.Fatal("broken migration succeeded")
	}
	_ = conn.Close()
	var indexAfter string
	if err := old.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='idx_ai_messages_generation'`).Scan(&indexAfter); err != nil || indexAfter != indexBefore {
		t.Fatalf("rollback lost original unique index: %q %v", indexAfter, err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('ai_continuations','ai_continuation_turns','idx_ai_messages_user_generation')`,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=77`,
	} {
		var count int
		if err := old.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial migration survived: %d %v", count, err)
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
		t.Fatalf("retry after rollback failed: schema=%d gate=%+v", store.SchemaVersion, gate)
	}
}
