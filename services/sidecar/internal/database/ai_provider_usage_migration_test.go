package database

import (
	"path/filepath"
	"testing"
)

func TestAIProviderUsageMigrationPreservesUnknownAndConstrainsExactCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-provider-usage.db")
	v61 := openDatabaseAtVersion(t, path, 61)
	const (
		sessionID    = "018f0000-0000-7000-8000-000000006601"
		providerID   = "018f0000-0000-7000-8000-000000006602"
		generationID = "018f0000-0000-7000-8000-000000006603"
		stepID       = "018f0000-0000-7000-8000-000000006604"
	)
	if _, err := v61.Exec(`
		INSERT INTO ai_providers(
			id, name, kind, protocol, base_url, model, status, health_status,
			has_key, last_health_at, version, created_at, updated_at
		) VALUES (?, 'Usage provider', 'local', 'openai_chat', 'http://127.0.0.1:11434/v1',
			'model', 'ready', 'healthy', 0, '2026-09-08T20:00:00Z', 1,
			'2026-09-08T20:00:00Z', '2026-09-08T20:00:00Z')
	`, providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if _, err := v61.Exec(`
		INSERT INTO ai_sessions(id, title, persist, version, created_at, updated_at)
		VALUES (?, 'usage migration', 1, 1, '2026-09-08T20:00:00Z', '2026-09-08T20:01:00Z')
	`, sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := v61.Exec(`
		INSERT INTO ai_generations(id, session_id, provider_id, status, created_at, updated_at)
		VALUES (?, ?, ?, 'completed', '2026-09-08T20:00:10Z', '2026-09-08T20:00:20Z')
	`, generationID, sessionID, providerID); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	if _, err := v61.Exec(`
		INSERT INTO ai_run_steps(
			id, generation_id, sequence, kind, status, turn_index, started_at,
			completed_at, duration_ms, input_bytes, output_bytes, created_at
		) VALUES (?, ?, 1, 'model_turn', 'succeeded', 1, '2026-09-08T20:00:10Z',
			'2026-09-08T20:00:20Z', 10, 100, 20, '2026-09-08T20:00:20Z')
	`, stepID, generationID); err != nil {
		t.Fatalf("seed step: %v", err)
	}
	if err := v61.Close(); err != nil {
		t.Fatalf("close v61 database: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 71 {
		t.Fatalf("SchemaVersion=%d, want 71", store.SchemaVersion)
	}
	var inputTokens, outputTokens *int
	var tokenSource *string
	if err := store.SQL.QueryRow(`
		SELECT input_tokens, output_tokens, token_source FROM ai_run_steps WHERE id = ?
	`, stepID).Scan(&inputTokens, &outputTokens, &tokenSource); err != nil {
		t.Fatalf("read migrated usage: %v", err)
	}
	if inputTokens != nil || outputTokens != nil || tokenSource != nil {
		t.Fatalf("migration invented usage input=%v output=%v source=%v", inputTokens, outputTokens, tokenSource)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_run_steps(
			id, generation_id, sequence, kind, status, turn_index, started_at,
			completed_at, duration_ms, input_bytes, output_bytes,
			input_tokens, output_tokens, token_source, created_at
		) VALUES (?, ?, 2, 'model_turn', 'succeeded', 2, '2026-09-08T20:00:21Z',
			'2026-09-08T20:00:22Z', 1, 100, 20, 12, 4, 'provider', '2026-09-08T20:00:22Z')
	`, "018f0000-0000-7000-8000-000000006605", generationID); err != nil {
		t.Fatalf("store exact provider usage: %v", err)
	}
	if _, err := store.SQL.Exec(`
		INSERT INTO ai_run_steps(
			id, generation_id, sequence, kind, status, turn_index, started_at,
			completed_at, duration_ms, input_bytes, output_bytes,
			input_tokens, output_tokens, token_source, created_at
		) VALUES (?, ?, 3, 'model_turn', 'succeeded', 3, '2026-09-08T20:00:23Z',
			'2026-09-08T20:00:24Z', 1, 100, 20, 12, NULL, 'provider', '2026-09-08T20:00:24Z')
	`, "018f0000-0000-7000-8000-000000006606", generationID); err == nil {
		t.Fatal("partial provider token usage was accepted")
	}
}
