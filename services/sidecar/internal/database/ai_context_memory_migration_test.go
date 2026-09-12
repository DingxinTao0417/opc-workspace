package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIContextMemoryMigrationPreservesV56AndConstrainsSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-context-memory.db")
	v56 := openDatabaseAtVersion(t, path, 56)
	const (
		sessionID = "018f0000-0000-7000-8000-000000005701"
		messageID = "018f0000-0000-7000-8000-000000005702"
	)
	if _, err := v56.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'existing session', 1, 1, '2026-09-03T10:00:00Z', '2026-09-03T10:00:00Z')
	`, sessionID); err != nil {
		t.Fatalf("insert v56 session: %v", err)
	}
	if _, err := v56.Exec(`
		INSERT INTO ai_messages(id, session_id, role, status, content, created_at, updated_at)
		VALUES (?, ?, 'assistant', 'completed', 'existing answer', '2026-09-03T10:01:00Z', '2026-09-03T10:01:00Z')
	`, messageID, sessionID); err != nil {
		t.Fatalf("insert v56 message: %v", err)
	}
	if err := v56.Close(); err != nil {
		t.Fatalf("close v56 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion = %d, want 71", store.SchemaVersion)
	}
	var sessionCount, messageCount, entryCount int
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_sessions WHERE id = ?", sessionID).Scan(&sessionCount); err != nil {
		t.Fatalf("count preserved session: %v", err)
	}
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_messages WHERE id = ?", messageID).Scan(&messageCount); err != nil {
		t.Fatalf("count preserved message: %v", err)
	}
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_memory_entries").Scan(&entryCount); err != nil {
		t.Fatalf("count new entries: %v", err)
	}
	if sessionCount != 1 || messageCount != 1 || entryCount != 0 {
		t.Fatalf("preserved session=%d message=%d entries=%d", sessionCount, messageCount, entryCount)
	}

	const firstEntryID = "018f0000-0000-7000-8000-000000005703"
	insertSnapshot := `
		INSERT INTO ai_memory_entries(
			id, session_id, kind, content, tags, origin, status,
			source_message_id, created_at, updated_at
		) VALUES (?, ?, 'context_snapshot', ?, '["decision"]', 'model_compaction', 'active', ?, ?, ?)
	`
	firstContent := `{"summary":"用户在规划发布","facts":[{"kind":"decision","content":"先完成本地版本"}]}`
	if _, err := store.SQL.Exec(insertSnapshot, firstEntryID, sessionID, firstContent, messageID, "2026-09-03T10:02:00Z", "2026-09-03T10:02:00Z"); err != nil {
		t.Fatalf("insert first snapshot: %v", err)
	}
	const (
		otherSessionID = "018f0000-0000-7000-8000-000000005705"
		otherMessageID = "018f0000-0000-7000-8000-000000005706"
	)
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'other session', 1, 1, '2026-09-03T10:00:00Z', '2026-09-03T10:00:00Z');
	`, otherSessionID); err != nil {
		t.Fatalf("insert other session: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_messages(id, session_id, role, status, content, created_at, updated_at)
		VALUES (?, ?, 'assistant', 'completed', 'other answer', '2026-09-03T10:01:00Z', '2026-09-03T10:01:00Z')
	`, otherMessageID, otherSessionID); err != nil {
		t.Fatalf("insert other message: %v", err)
	}
	if _, err := store.SQL.Exec(insertSnapshot, "018f0000-0000-7000-8000-000000005707", sessionID, firstContent, otherMessageID, "2026-09-03T10:03:00Z", "2026-09-03T10:03:00Z"); err == nil {
		t.Fatal("cross-session source message was accepted")
	}
	if _, err := store.SQL.Exec(insertSnapshot, "018f0000-0000-7000-8000-000000005704", sessionID, firstContent, messageID, "2026-09-03T10:03:00Z", "2026-09-03T10:03:00Z"); err == nil {
		t.Fatal("second active snapshot was accepted")
	}
	if _, err := store.SQL.Exec("UPDATE ai_memory_entries SET content = ? WHERE id = ?", `{"summary":"tampered","facts":[]}`, firstEntryID); err == nil || !strings.Contains(err.Error(), "AI_MEMORY_ENTRY_IMMUTABLE") {
		t.Fatalf("mutable snapshot error = %v", err)
	}
	if _, err := store.SQL.Exec("UPDATE ai_memory_entries SET status = 'superseded', updated_at = ? WHERE id = ?", "2026-09-03T10:03:00Z", firstEntryID); err != nil {
		t.Fatalf("supersede snapshot: %v", err)
	}
	if _, err := store.SQL.Exec(insertSnapshot, "018f0000-0000-7000-8000-000000005704", sessionID, firstContent, messageID, "2026-09-03T10:04:00Z", "2026-09-03T10:04:00Z"); err != nil {
		t.Fatalf("insert replacement snapshot: %v", err)
	}
	if _, err := store.SQL.Exec("DELETE FROM ai_messages WHERE id = ?", messageID); err != nil {
		t.Fatalf("delete source message: %v", err)
	}
	if err := store.SQL.QueryRow("SELECT COUNT(*) FROM ai_memory_entries").Scan(&entryCount); err != nil || entryCount != 0 {
		t.Fatalf("source-message cascade entries=%d err=%v", entryCount, err)
	}
}
