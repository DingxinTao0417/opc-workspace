package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAIRunStepsMigrationBackfillsAndConstrainsContentFreeTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-run-steps.db")
	v60 := openDatabaseAtVersion(t, path, 60)
	const (
		sessionID    = "018f0000-0000-7000-8000-000000006301"
		providerID   = "018f0000-0000-7000-8000-000000006302"
		generationID = "018f0000-0000-7000-8000-000000006303"
		messageID    = "018f0000-0000-7000-8000-000000006304"
	)
	if _, err := v60.Exec(`
		INSERT INTO ai_providers(
			id, name, kind, protocol, base_url, model, status, health_status,
			has_key, last_health_at, version, created_at, updated_at
		) VALUES (?, 'Migration provider', 'local', 'openai_chat', 'http://127.0.0.1:11434/v1',
			'model', 'ready', 'healthy', 0, '2026-09-08T20:00:00Z', 1, '2026-09-08T20:00:00Z', '2026-09-08T20:00:00Z')
	`, providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := v60.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'run migration', 1, 1, '2026-09-08T20:00:00Z', '2026-09-08T20:01:00Z')
	`, sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := v60.Exec(`
		INSERT INTO ai_generations(id, session_id, provider_id, status, content, created_at, updated_at)
		VALUES (?, ?, ?, 'completed', 'private answer body', '2026-09-08T20:00:10Z', '2026-09-08T20:00:20Z')
	`, generationID, sessionID, providerID); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	if _, err := v60.Exec(`
		INSERT INTO ai_messages(id, session_id, role, status, content, created_at, updated_at)
		VALUES (?, ?, 'assistant', 'completed', 'private answer body', '2026-09-08T20:00:20Z', '2026-09-08T20:00:20Z')
	`, messageID, sessionID); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := v60.Close(); err != nil {
		t.Fatalf("close v60 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 70 {
		t.Fatalf("SchemaVersion=%d, want 70", store.SchemaVersion)
	}
	var step struct {
		Status      string
		OutputBytes int
		ErrorCode   *string
	}
	if err := store.DB.Table("ai_run_steps").Select("status, output_bytes, error_code").
		Where("generation_id = ? AND sequence = 1", generationID).Take(&step).Error; err != nil {
		t.Fatalf("read backfilled step: %v", err)
	}
	if step.Status != "succeeded" || step.OutputBytes != len("private answer body") || step.ErrorCode != nil {
		t.Fatalf("backfilled step=%#v", step)
	}
	if err := store.DB.Model(&struct{ GenerationID *string }{}).Table("ai_messages").
		Where("id = ?", messageID).Update("generation_id", generationID).Error; err != nil {
		t.Fatalf("link generation: %v", err)
	}
	if err := store.DB.Table("ai_run_steps").Where("generation_id = ?", generationID).
		Update("output_bytes", 999).Error; err == nil || !strings.Contains(err.Error(), "AI_RUN_STEP_IMMUTABLE") {
		t.Fatalf("terminal step mutation error=%v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO ai_run_steps(
			id, generation_id, sequence, kind, status, turn_index, tool_name,
			started_at, completed_at, duration_ms, input_bytes, output_bytes, created_at
		) VALUES (?, ?, 2, 'tool_call', 'succeeded', NULL, NULL, ?, ?, 1, 0, 0, ?)
	`, "018f0000-0000-7000-8000-000000006305", generationID,
		"2026-09-08T20:00:11Z", "2026-09-08T20:00:12Z", "2026-09-08T20:00:11Z").Error; err == nil {
		t.Fatal("tool step without tool_name was accepted")
	}
}
