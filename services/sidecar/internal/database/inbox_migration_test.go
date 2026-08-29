package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInboxMigrationUpgradesV11WithoutChangingExistingFacts(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "v11-inbox-upgrade.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() current database error = %v", err)
	}
	const clientID = "018f0000-0000-7000-8000-000000001211"
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, contact_name, email, status, version, created_at, updated_at)
		VALUES (?, '迁移前客户', '联系人', 'before@example.com', 'active', 3, '2026-08-27T08:00:00Z', '2026-08-27T09:00:00Z')
	`, clientID).Error; err != nil {
		t.Fatalf("seed v11 client fact: %v", err)
	}
	// Build an exact schema-history v11 boundary from the verified current store.
	if err := store.DB.Exec("DROP TABLE reminders").Error; err != nil {
		t.Fatalf("remove v14 Reminder table from fixture: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 14").Error; err != nil {
		t.Fatalf("rewind fixture migration 14 history: %v", err)
	}
	// Migration 013 depends on the migration 012 Inbox table, so remove its
	// independent Task trigger and relation table before rewinding migration 012.
	if err := store.DB.Exec("DROP TRIGGER trg_tasks_prevent_active_inbox_relation_delete").Error; err != nil {
		t.Fatalf("remove v13 Task delete trigger from fixture: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE inbox_item_tasks").Error; err != nil {
		t.Fatalf("remove v13 relation table from fixture: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 13").Error; err != nil {
		t.Fatalf("rewind fixture migration 13 history: %v", err)
	}
	if err := store.DB.Exec("DROP TABLE inbox_items").Error; err != nil {
		t.Fatalf("remove v12 table from fixture: %v", err)
	}
	if err := store.DB.Exec("DELETE FROM schema_migrations WHERE version = 12").Error; err != nil {
		t.Fatalf("rewind fixture migration 12 history: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close v11 fixture: %v", err)
	}

	store, err = Open(databasePath)
	if err != nil {
		t.Fatalf("upgrade v11 database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 30 {
		t.Fatalf("SchemaVersion = %d, want 30", store.SchemaVersion)
	}
	var client struct {
		Name        string
		ContactName *string
		Email       *string
		Status      string
		Version     int64
		CreatedAt   string
		UpdatedAt   string
	}
	if err := store.DB.Table("clients").Where("id = ?", clientID).Take(&client).Error; err != nil {
		t.Fatalf("load preserved client: %v", err)
	}
	if client.Name != "迁移前客户" || client.ContactName == nil || *client.ContactName != "联系人" ||
		client.Email == nil || *client.Email != "before@example.com" || client.Status != "active" ||
		client.Version != 3 || client.CreatedAt != "2026-08-27T08:00:00Z" || client.UpdatedAt != "2026-08-27T09:00:00Z" {
		t.Fatalf("preserved client = %#v", client)
	}
	var inboxCount int64
	if err := store.DB.Table("inbox_items").Count(&inboxCount).Error; err != nil || inboxCount != 0 {
		t.Fatalf("new Inbox table count=%d err=%v", inboxCount, err)
	}
}

func TestInboxMigrationCreatesConstrainedManualIntakeFacts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 30 {
		t.Fatalf("SchemaVersion = %d, want 30", store.SchemaVersion)
	}

	const itemID = "018f0000-0000-7000-8000-000000001201"
	item := models.InboxItem{
		ID: itemID, Kind: "manual", Title: "手工受理项", Summary: "",
		SourceEntityType: "manual", Priority: "P2", Status: "open",
		ResolutionPolicy: "manual", PayloadJSON: `{}`, Version: 1,
		CreatedAt: "2026-08-28T08:00:00Z", UpdatedAt: "2026-08-28T08:00:00Z",
	}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatalf("insert valid Inbox Item: %v", err)
	}

	invalid := []struct {
		name  string
		query string
	}{
		{"trimmed title", `UPDATE inbox_items SET title = ' invalid ' WHERE id = '` + itemID + `'`},
		{"payload array", `UPDATE inbox_items SET payload_json = '[]' WHERE id = '` + itemID + `'`},
		{"manual source id", `UPDATE inbox_items SET source_entity_id = '018f0000-0000-7000-8000-000000001299' WHERE id = '` + itemID + `'`},
		{"terminal without triage", `UPDATE inbox_items SET status = 'resolved', resolved_by_actor_id = '` + models.BuiltinOwnerActorID + `', resolved_at = '2026-08-28T09:00:00Z', resolution_reason = 'done', resolution_mode = 'manual' WHERE id = '` + itemID + `'`},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := store.DB.Exec(test.query).Error; err == nil {
				t.Fatalf("constraint accepted invalid mutation: %s", test.query)
			}
		})
	}

	firstKey := "manual-source-event"
	secondKey := firstKey
	first := models.InboxItem{
		ID: "018f0000-0000-7000-8000-000000001202", Kind: "event", Title: "来源事件一", Summary: "",
		SourceEntityType: "system", SourceEventKey: &firstKey, Priority: "P1", Status: "open",
		ResolutionPolicy: "manual", PayloadJSON: `{}`, Version: 1,
		CreatedAt: "2026-08-28T08:00:01Z", UpdatedAt: "2026-08-28T08:00:01Z",
	}
	if err := store.DB.Create(&first).Error; err != nil {
		t.Fatalf("insert source event Inbox Item: %v", err)
	}
	second := first
	second.ID = "018f0000-0000-7000-8000-000000001203"
	second.SourceEventKey = &secondKey
	if err := store.DB.Create(&second).Error; err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate source_event_key error = %v, want unique constraint", err)
	}
	second.SourceEventKey = nil
	if err := store.DB.Create(&second).Error; err != nil {
		t.Fatalf("multiple NULL source_event_key values must be allowed: %v", err)
	}
}
