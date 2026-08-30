package database

import (
	"path/filepath"
	"testing"
)

func TestFinancialEntriesMigrationCreatesConstrainedAuditLedger(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "financial-entries.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 49 {
		t.Fatalf("schema version = %d, want 49", store.SchemaVersion)
	}
	var initialCount int64
	if err := store.DB.Table("financial_entries").Count(&initialCount).Error; err != nil || initialCount != 0 {
		t.Fatalf("initial financial entry count = %d, err=%v", initialCount, err)
	}

	valid := `INSERT INTO financial_entries(
		id, type, amount_minor, currency, occurred_on, status, category,
		created_by_actor_id, version, created_at, updated_at
	) VALUES (?, 'income', 12800, 'CNY', '2026-08-29', 'confirmed', '咨询服务',
		'00000000-0000-5000-8000-000000000001', 1, '2026-08-29T10:00:00Z', '2026-08-29T10:00:00Z')`
	if err := store.DB.Exec(valid, "11111111-1111-4111-8111-111111111111").Error; err != nil {
		t.Fatalf("insert valid financial entry: %v", err)
	}
	for name, statement := range map[string]string{
		"zero amount":        `INSERT INTO financial_entries(id,type,amount_minor,currency,occurred_on,status,category,created_by_actor_id,created_at,updated_at) VALUES('21111111-1111-4111-8111-111111111111','income',0,'CNY','2026-08-29','confirmed','x','00000000-0000-5000-8000-000000000001','x','x')`,
		"lower currency":     `INSERT INTO financial_entries(id,type,amount_minor,currency,occurred_on,status,category,created_by_actor_id,created_at,updated_at) VALUES('31111111-1111-4111-8111-111111111111','income',1,'cny','2026-08-29','confirmed','x','00000000-0000-5000-8000-000000000001','x','x')`,
		"invalid date":       `INSERT INTO financial_entries(id,type,amount_minor,currency,occurred_on,status,category,created_by_actor_id,created_at,updated_at) VALUES('41111111-1111-4111-8111-111111111111','income',1,'CNY','2026-02-30','confirmed','x','00000000-0000-5000-8000-000000000001','x','x')`,
		"void without audit": `INSERT INTO financial_entries(id,type,amount_minor,currency,occurred_on,status,category,created_by_actor_id,created_at,updated_at) VALUES('51111111-1111-4111-8111-111111111111','expense',1,'CNY','2026-08-29','voided','x','00000000-0000-5000-8000-000000000001','x','x')`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.DB.Exec(statement).Error; err == nil {
				t.Fatal("invalid financial entry was accepted")
			}
		})
	}

	id := "11111111-1111-4111-8111-111111111111"
	if err := store.DB.Exec(`UPDATE financial_entries SET status='voided', voided_at='2026-08-29T11:00:00Z', voided_by_actor_id='00000000-0000-5000-8000-000000000001', void_reason='duplicate', version=2, updated_at='2026-08-29T11:00:00Z' WHERE id=?`, id).Error; err != nil {
		t.Fatalf("void financial entry: %v", err)
	}
	if err := store.DB.Exec("UPDATE financial_entries SET notes='changed' WHERE id=?", id).Error; err == nil {
		t.Fatal("voided entry was mutable")
	}
	if err := store.DB.Exec("DELETE FROM financial_entries WHERE id=?", id).Error; err == nil {
		t.Fatal("financial entry hard delete was accepted")
	}
	assertForeignKey(t, store.SQL, "financial_entries", "client_id", "clients", "RESTRICT")
	assertForeignKey(t, store.SQL, "financial_entries", "project_id", "projects", "SET NULL")
}
