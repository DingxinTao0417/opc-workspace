package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrationsAndPragmas(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if store.SchemaVersion != 2 {
		t.Fatalf("SchemaVersion = %d, want 2", store.SchemaVersion)
	}

	checks := map[string]int{
		"PRAGMA foreign_keys": 1,
		"PRAGMA busy_timeout": busyTimeoutMilliseconds,
	}
	for query, want := range checks {
		var got int
		if err := store.SQL.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if got != want {
			t.Fatalf("%s = %d, want %d", query, got, want)
		}
	}
	var journalMode string
	if err := store.SQL.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
}

func TestMigrationRemovesOnlyDefaultDemoSeed(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "legacy-seed.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := SeedDevelopmentData(store.DB); err != nil {
		t.Fatalf("seed legacy data: %v", err)
	}

	const (
		customClientID  = "018f0000-0000-7000-8000-000000000201"
		customProjectID = "018f0000-0000-7000-8000-000000000202"
		customTaskID    = "018f0000-0000-7000-8000-000000000203"
	)
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, '用户自己的客户', 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, customClientID).Error; err != nil {
		t.Fatalf("insert custom client: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, '用户自己的项目', ?, 'in_progress', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, customProjectID, customClientID).Error; err != nil {
		t.Fatalf("insert custom project: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO tasks(id, title, status, priority, project_id, created_at, updated_at)
		VALUES (?, '用户自己的任务', 'todo', 'P2', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, customTaskID, customProjectID).Error; err != nil {
		t.Fatalf("insert custom task: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 2").Error; err != nil {
		t.Fatalf("rewind schema version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("reopen legacy database: %v", err)
	}
	defer store.Close()

	for table, ids := range map[string][]string{
		"tasks":    {seedTaskOneID, seedTaskTwoID},
		"projects": {seedProjectID},
		"clients":  {seedClientID},
	} {
		var count int64
		if err := store.DB.Table(table).Where("id IN ?", ids).Count(&count).Error; err != nil {
			t.Fatalf("count %s demo rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s demo row count = %d, want 0", table, count)
		}
	}

	for table, id := range map[string]string{
		"tasks":    customTaskID,
		"projects": customProjectID,
		"clients":  customClientID,
	} {
		var count int64
		if err := store.DB.Table(table).Where("id = ?", id).Count(&count).Error; err != nil {
			t.Fatalf("count custom %s row: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("custom %s row count = %d, want 1", table, count)
		}
	}
}

func TestSeedDevelopmentDataIsIdempotent(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "seed.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if err := SeedDevelopmentData(store.DB); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := SeedDevelopmentData(store.DB); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var count int64
	if err := store.DB.Table("tasks").Count(&count).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 2 {
		t.Fatalf("task count = %d, want 2", count)
	}
}
