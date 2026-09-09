package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIMessageCitationsMigrationPreservesV59AndConstrainsSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-message-citations.db")
	v59 := openDatabaseAtVersion(t, path, 59)
	const (
		sessionID   = "018f0000-0000-7000-8000-000000006001"
		assistantID = "018f0000-0000-7000-8000-000000006002"
		userID      = "018f0000-0000-7000-8000-000000006003"
	)
	if _, err := v59.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'citation migration', 1, 1, '2026-09-08T20:00:00Z', '2026-09-08T20:00:00Z')
	`, sessionID); err != nil {
		t.Fatalf("seed v59 AI session: %v", err)
	}
	if _, err := v59.Exec(`
		INSERT INTO ai_messages(id, session_id, role, status, content, created_at, updated_at)
		VALUES
			(?, ?, 'assistant', 'completed', 'existing answer', '2026-09-08T20:01:00Z', '2026-09-08T20:01:00Z'),
			(?, ?, 'user', 'completed', 'existing prompt', '2026-09-08T20:00:30Z', '2026-09-08T20:00:30Z')
	`, assistantID, sessionID, userID, sessionID); err != nil {
		t.Fatalf("seed v59 AI messages: %v", err)
	}
	if err := v59.Close(); err != nil {
		t.Fatalf("close v59 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 67 {
		t.Fatalf("SchemaVersion=%d, want 67", store.SchemaVersion)
	}
	var existing *string
	if err := store.SQL.QueryRow("SELECT citations_snapshot FROM ai_messages WHERE id = ?", assistantID).Scan(&existing); err != nil {
		t.Fatalf("read preserved citation snapshot: %v", err)
	}
	if existing != nil {
		t.Fatalf("migration invented citation snapshot: %q", *existing)
	}
	valid := `{"version":1,"status":"validated","items":[{"chunk_id":"018f0000-0000-7000-8000-000000006010","source_id":"018f0000-0000-7000-8000-000000006011","source_name":"guide.md","document_id":"018f0000-0000-7000-8000-000000006012","document_version":1,"start_line":2,"end_line":3}]}`
	if _, err := store.SQL.Exec("UPDATE ai_messages SET citations_snapshot = ? WHERE id = ?", valid, assistantID); err != nil {
		t.Fatalf("store valid citations: %v", err)
	}
	for name, payload := range map[string]string{
		"user message":   `{"version":1,"status":"validated","items":[]}`,
		"invalid json":   `not-json`,
		"invalid status": `{"version":1,"status":"trusted","items":[]}`,
		"too many":       `{"version":1,"status":"validated","items":[{},{},{},{}]}`,
		"oversized":      `{"version":1,"status":"missing","items":[],"padding":"` + strings.Repeat("x", 16384) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			messageID := assistantID
			if name == "user message" {
				messageID = userID
			}
			if _, err := store.SQL.Exec("UPDATE ai_messages SET citations_snapshot = ? WHERE id = ?", payload, messageID); err == nil {
				t.Fatalf("invalid citation snapshot was accepted: %s", payload)
			}
		})
	}
}
