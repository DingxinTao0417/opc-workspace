package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIMessageContextMigrationPreservesV57AndConstrainsSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-message-context.db")
	v57 := openDatabaseAtVersion(t, path, 57)
	const (
		sessionID = "018f0000-0000-7000-8000-000000005801"
		messageID = "018f0000-0000-7000-8000-000000005802"
	)
	if _, err := v57.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'context migration', 1, 1, '2026-09-08T20:00:00Z', '2026-09-08T20:00:00Z')
	`, sessionID); err != nil {
		t.Fatalf("seed v57 AI session: %v", err)
	}
	if _, err := v57.Exec(`
		INSERT INTO ai_messages(id, session_id, role, status, content, created_at, updated_at)
		VALUES (?, ?, 'user', 'completed', 'existing prompt', '2026-09-08T20:01:00Z', '2026-09-08T20:01:00Z')
	`, messageID, sessionID); err != nil {
		t.Fatalf("seed v57 AI message: %v", err)
	}
	if err := v57.Close(); err != nil {
		t.Fatalf("close v57 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion=%d, want 71", store.SchemaVersion)
	}
	var existing *string
	if err := store.SQL.QueryRow("SELECT context_snapshot FROM ai_messages WHERE id = ?", messageID).Scan(&existing); err != nil {
		t.Fatalf("read preserved message context: %v", err)
	}
	if existing != nil {
		t.Fatalf("existing message invented context: %q", *existing)
	}
	valid := `{"version":1,"provider":{"id":"018f0000-0000-7000-8000-000000005804","name":"Local","kind":"local","version":1},"sources":[{"type":"task","id":"018f0000-0000-7000-8000-000000005803","version":1,"label":"Task","fields":{"title":"Task"},"truncated_fields":[]}]}`
	if _, err := store.SQL.Exec("UPDATE ai_messages SET context_snapshot = ? WHERE id = ?", valid, messageID); err != nil {
		t.Fatalf("store valid context: %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE ai_messages SET context_snapshot = 'not-json' WHERE id = ?", messageID); err == nil {
		t.Fatal("invalid context JSON was accepted")
	}
	oversized := `{"content":"` + strings.Repeat("x", 32768) + `"}`
	if _, err := store.SQL.Exec("UPDATE ai_messages SET context_snapshot = ? WHERE id = ?", oversized, messageID); err == nil {
		t.Fatal("oversized context JSON was accepted")
	}
}
