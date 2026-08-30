package database

import (
	"path/filepath"
	"testing"
)

func TestStorageCapacityHistoryMigrationCreatesEmptyConstrainedTable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "storage-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.SchemaVersion != 47 {
		t.Fatalf("schema version=%d, want 47", store.SchemaVersion)
	}
	var count int64
	if err := store.DB.Table("storage_capacity_samples").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("initial sample count=%d err=%v", count, err)
	}
	valid := `INSERT INTO storage_capacity_samples
		(scope, sample_bucket, available_bytes, total_bytes, threshold_bytes, status, checked_at)
		VALUES ('database+artifacts', 1, 10, 20, 5, 'healthy', '2026-08-29T00:00:00Z')`
	if err := store.DB.Exec(valid).Error; err != nil {
		t.Fatalf("insert valid sample: %v", err)
	}
	invalid := []string{
		`INSERT INTO storage_capacity_samples (scope, sample_bucket, available_bytes, total_bytes, threshold_bytes, status, checked_at) VALUES ('private-volume', 2, 10, 20, 5, 'healthy', 'x')`,
		`INSERT INTO storage_capacity_samples (scope, sample_bucket, available_bytes, total_bytes, threshold_bytes, status, checked_at) VALUES ('database', 2, 21, 20, 5, 'healthy', 'x')`,
		`INSERT INTO storage_capacity_samples (scope, sample_bucket, available_bytes, total_bytes, threshold_bytes, status, checked_at) VALUES ('database', 2, 10, 20, 5, 'unknown', 'x')`,
	}
	for _, statement := range invalid {
		if err := store.DB.Exec(statement).Error; err == nil {
			t.Fatalf("invalid sample unexpectedly accepted: %s", statement)
		}
	}
}
