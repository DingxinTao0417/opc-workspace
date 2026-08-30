package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const (
	automationDeliveryRuleID  = "018f0000-0000-7000-8000-000000004901"
	automationDeliveryEventID = "018f0000-0000-7000-8000-000000004902"
	automationDeliveryID      = "018f0000-0000-7000-8000-000000004903"
)

func TestAutomationEventDeliveryMigrationUpgradesV48WithEmptyPendingQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v48-automation-delivery.db")
	v48 := openDatabaseAtVersion(t, path, 48)
	seedAutomationDeliveryReferences(t, v48)
	if err := v48.Close(); err != nil {
		t.Fatalf("close v48 fixture: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade v48 automation delivery database: %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 49 {
		t.Fatalf("SchemaVersion = %d, want 49", store.SchemaVersion)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM automation_event_deliveries"); got != 0 {
		t.Fatalf("automation_event_deliveries rows = %d, want no historical backfill", got)
	}
	if got := readInt64(t, store.SQL, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index'
		  AND name IN ('idx_automation_event_deliveries_due', 'idx_automation_event_deliveries_source')
	`); got != 2 {
		t.Fatalf("automation delivery lookup indexes = %d, want 2", got)
	}
	for indexName, wantColumns := range map[string]string{
		"idx_automation_event_deliveries_due":    "available_at,captured_at,id",
		"idx_automation_event_deliveries_source": "source_event_id,id",
	} {
		var columns string
		if err := store.SQL.QueryRow(`
			SELECT group_concat(name, ',')
			FROM (
				SELECT name
				FROM pragma_index_info(?)
				ORDER BY seqno
			)
		`, indexName).Scan(&columns); err != nil {
			t.Fatalf("read %s columns: %v", indexName, err)
		}
		if columns != wantColumns {
			t.Fatalf("%s columns = %q, want %q", indexName, columns, wantColumns)
		}
	}
}

func TestAutomationEventDeliveryMigrationConstrainsPendingDeliveryIdentity(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "automation-delivery-constraints.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	seedAutomationDeliveryReferences(t, store.SQL)

	insertAutomationDelivery(t, store.SQL, automationDeliveryID, automationDeliveryRuleID, automationDeliveryEventID, automationDeliveryLogicalKey(automationDeliveryRuleID, automationDeliveryEventID))

	t.Run("null id", func(t *testing.T) {
		const nullIDEvent = "018f0000-0000-7000-8000-000000004915"
		insertWorkflowEvent(t, store.SQL, nullIDEvent)
		if _, err := store.SQL.Exec(`
			INSERT INTO automation_event_deliveries(
				id, rule_id, preset_key, rule_version, source_event_id, logical_key,
				config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
			) VALUES (NULL, ?, 'project-completed-inbox', 1, ?, ?, '{}', '{}', ?, ?, ?)
		`,
			automationDeliveryRuleID, nullIDEvent,
			automationDeliveryLogicalKey(automationDeliveryRuleID, nullIDEvent),
			"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
		); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not null") {
			t.Fatalf("null delivery id error = %v", err)
		}
	})

	t.Run("foreign keys", func(t *testing.T) {
		assertAutomationDeliveryInsertFails(t, store.SQL,
			"018f0000-0000-7000-8000-000000004904",
			"018f0000-0000-7000-8000-000000004999",
			automationDeliveryEventID,
			automationDeliveryLogicalKey("018f0000-0000-7000-8000-000000004999", automationDeliveryEventID),
			"AUTOMATION_EVENT_DELIVERY_RULE_SNAPSHOT_INVALID",
		)
		assertAutomationDeliveryInsertFails(t, store.SQL,
			"018f0000-0000-7000-8000-000000004905",
			automationDeliveryRuleID,
			"018f0000-0000-7000-8000-000000004999",
			automationDeliveryLogicalKey(automationDeliveryRuleID, "018f0000-0000-7000-8000-000000004999"),
			"FOREIGN KEY",
		)
	})

	t.Run("rule snapshot", func(t *testing.T) {
		const wrongSnapshotEventID = "018f0000-0000-7000-8000-000000004906"
		insertWorkflowEvent(t, store.SQL, wrongSnapshotEventID)
		if _, err := store.SQL.Exec(`
			INSERT INTO automation_event_deliveries(
				id, rule_id, preset_key, rule_version, source_event_id, logical_key,
				config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
			) VALUES (?, ?, 'wrong-preset', 1, ?, ?, '{}', '{}', ?, ?, ?)
		`,
			"018f0000-0000-7000-8000-000000004906", automationDeliveryRuleID,
			wrongSnapshotEventID, automationDeliveryLogicalKey(automationDeliveryRuleID, wrongSnapshotEventID),
			"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
		); err == nil || !strings.Contains(err.Error(), "AUTOMATION_EVENT_DELIVERY_RULE_SNAPSHOT_INVALID") {
			t.Fatalf("invalid rule snapshot error = %v", err)
		}

		const (
			disabledRuleID  = "018f0000-0000-7000-8000-000000004912"
			disabledEventID = "018f0000-0000-7000-8000-000000004913"
		)
		if _, err := store.SQL.Exec(`
			INSERT INTO automation_rules(
				id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at
			) VALUES (?, 'invoice-due-inbox', 0, '{}', NULL, 1, ?, ?)
		`, disabledRuleID, "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z"); err != nil {
			t.Fatalf("insert disabled automation rule: %v", err)
		}
		insertWorkflowEvent(t, store.SQL, disabledEventID)
		if _, err := store.SQL.Exec(`
			INSERT INTO automation_event_deliveries(
				id, rule_id, preset_key, rule_version, source_event_id, logical_key,
				config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
			) VALUES (?, ?, 'invoice-due-inbox', 1, ?, ?, '{}', '{}', ?, ?, ?)
		`,
			"018f0000-0000-7000-8000-000000004914", disabledRuleID, disabledEventID,
			automationDeliveryLogicalKey(disabledRuleID, disabledEventID),
			"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
		); err == nil || !strings.Contains(err.Error(), "AUTOMATION_EVENT_DELIVERY_RULE_SNAPSHOT_INVALID") {
			t.Fatalf("disabled rule snapshot error = %v", err)
		}
	})

	t.Run("unique rule event", func(t *testing.T) {
		assertAutomationDeliveryInsertFails(t, store.SQL,
			"018f0000-0000-7000-8000-000000004907",
			automationDeliveryRuleID,
			automationDeliveryEventID,
			automationDeliveryLogicalKey(automationDeliveryRuleID, automationDeliveryEventID),
			"UNIQUE",
		)
	})

	t.Run("logical key formula", func(t *testing.T) {
		const secondEventID = "018f0000-0000-7000-8000-000000004908"
		insertWorkflowEvent(t, store.SQL, secondEventID)
		assertAutomationDeliveryInsertFails(t, store.SQL,
			"018f0000-0000-7000-8000-000000004909",
			automationDeliveryRuleID,
			secondEventID,
			"event:"+secondEventID,
			"CHECK CONSTRAINT",
		)
	})

	t.Run("JSON object snapshots", func(t *testing.T) {
		const secondEventID = "018f0000-0000-7000-8000-000000004910"
		insertWorkflowEvent(t, store.SQL, secondEventID)
		if _, err := store.SQL.Exec(`
			INSERT INTO automation_event_deliveries(
				id, rule_id, preset_key, rule_version, source_event_id, logical_key,
				config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
			) VALUES (?, ?, 'project-completed-inbox', 1, ?, ?, '[]', '{}', ?, ?, ?)
		`,
			"018f0000-0000-7000-8000-000000004911", automationDeliveryRuleID,
			secondEventID, automationDeliveryLogicalKey(automationDeliveryRuleID, secondEventID),
			"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
		); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
			t.Fatalf("non-object snapshot error = %v", err)
		}
	})
}

func TestAutomationEventDeliveryMigrationOnlyAllowsBackoffMetadataUpdatesAndDelete(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "automation-delivery-immutability.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	seedAutomationDeliveryReferences(t, store.SQL)
	insertAutomationDelivery(t, store.SQL, automationDeliveryID, automationDeliveryRuleID, automationDeliveryEventID, automationDeliveryLogicalKey(automationDeliveryRuleID, automationDeliveryEventID))

	if _, err := store.SQL.Exec(`
		UPDATE automation_event_deliveries
		SET delivery_attempts = delivery_attempts + 1,
		    available_at = '2026-08-30T12:05:00Z',
		    updated_at = '2026-08-30T12:00:30Z'
		WHERE id = ?
	`, automationDeliveryID); err != nil {
		t.Fatalf("update delivery backoff metadata: %v", err)
	}
	var attempts int
	var availableAt string
	var lastErrorCode sql.NullString
	if err := store.SQL.QueryRow(`
		SELECT delivery_attempts, available_at, last_error_code
		FROM automation_event_deliveries
		WHERE id = ?
	`, automationDeliveryID).Scan(&attempts, &availableAt, &lastErrorCode); err != nil {
		t.Fatalf("read delivery backoff metadata: %v", err)
	}
	if attempts != 1 || availableAt != "2026-08-30T12:05:00Z" || lastErrorCode.Valid {
		t.Fatalf("delivery claim attempts=%d available_at=%q last_error=%#v", attempts, availableAt, lastErrorCode)
	}

	if _, err := store.SQL.Exec(`
		UPDATE automation_event_deliveries
		SET last_error_code = ' RETRYABLE ',
		    last_error_at = '2026-08-30T12:00:40Z',
		    updated_at = '2026-08-30T12:00:40Z'
		WHERE id = ?
	`, automationDeliveryID); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
		t.Fatalf("untrimmed delivery error code = %v", err)
	}

	if _, err := store.SQL.Exec(`
		UPDATE automation_event_deliveries
		SET last_error_code = 'AUTOMATION_DELIVERY_RETRYABLE',
		    last_error_at = '2026-08-30T12:00:45Z',
		    updated_at = '2026-08-30T12:00:45Z'
		WHERE id = ?
	`, automationDeliveryID); err != nil {
		t.Fatalf("record delivery failure metadata: %v", err)
	}
	if err := store.SQL.QueryRow(`
		SELECT last_error_code
		FROM automation_event_deliveries
		WHERE id = ?
	`, automationDeliveryID).Scan(&lastErrorCode); err != nil {
		t.Fatalf("read delivery failure metadata: %v", err)
	}
	if !lastErrorCode.Valid || lastErrorCode.String != "AUTOMATION_DELIVERY_RETRYABLE" {
		t.Fatalf("delivery last error = %#v", lastErrorCode)
	}

	immutableUpdates := []struct {
		name  string
		query string
	}{
		{name: "logical key", query: "UPDATE automation_event_deliveries SET logical_key = 'changed' WHERE id = ?"},
		{name: "config snapshot", query: "UPDATE automation_event_deliveries SET config_snapshot_json = '{\"changed\":true}' WHERE id = ?"},
		{name: "action snapshot", query: "UPDATE automation_event_deliveries SET action_snapshot_json = '{\"changed\":true}' WHERE id = ?"},
		{name: "captured at", query: "UPDATE automation_event_deliveries SET captured_at = '2026-08-30T13:00:00Z' WHERE id = ?"},
	}
	for _, test := range immutableUpdates {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.SQL.Exec(test.query, automationDeliveryID); err == nil || !strings.Contains(err.Error(), "AUTOMATION_EVENT_DELIVERY_SNAPSHOT_IMMUTABLE") {
				t.Fatalf("immutable update error = %v", err)
			}
		})
	}

	if _, err := store.SQL.Exec(`
		UPDATE automation_event_deliveries
		SET updated_at = '2026-08-30T12:01:00Z'
		WHERE id = ?
	`, automationDeliveryID); err == nil || !strings.Contains(err.Error(), "AUTOMATION_EVENT_DELIVERY_BACKOFF_INVALID") {
		t.Fatalf("non-backoff metadata update error = %v", err)
	}

	if _, err := store.SQL.Exec("DELETE FROM automation_event_deliveries WHERE id = ?", automationDeliveryID); err != nil {
		t.Fatalf("delete completed pending delivery: %v", err)
	}
	if got := readInt64(t, store.SQL, "SELECT COUNT(*) FROM automation_event_deliveries"); got != 0 {
		t.Fatalf("pending deliveries after delete = %d, want 0", got)
	}
}

func seedAutomationDeliveryReferences(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO automation_rules(
			id, preset_key, enabled, config_json, next_run_at, version, created_at, updated_at
		) VALUES (?, 'project-completed-inbox', 1, '{}', NULL, 1, ?, ?)
	`, automationDeliveryRuleID, "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z"); err != nil {
		t.Fatalf("insert automation delivery rule: %v", err)
	}
	insertWorkflowEvent(t, db, automationDeliveryEventID)
}

func insertWorkflowEvent(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO workflow_events(
			id, aggregate_type, aggregate_id, action, actor_id,
			previous_json, current_json, created_at
		) VALUES (?, 'project', 'automation-delivery-project', 'project_completed', ?,
		          '{"status":"in_progress"}', '{"status":"completed"}', ?)
	`, id, builtinSystemActorID, "2026-08-30T12:00:00Z"); err != nil {
		t.Fatalf("insert workflow event %s: %v", id, err)
	}
}

func insertAutomationDelivery(t *testing.T, db *sql.DB, id, ruleID, eventID, logicalKey string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO automation_event_deliveries(
			id, rule_id, preset_key, rule_version, source_event_id, logical_key,
			config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
		) VALUES (?, ?, 'project-completed-inbox', 1, ?, ?, '{"priority":"P1"}',
		          '{"type":"create_inbox_item"}', ?, ?, ?)
	`,
		id, ruleID, eventID, logicalKey,
		"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
	); err != nil {
		t.Fatalf("insert automation delivery: %v", err)
	}
}

func assertAutomationDeliveryInsertFails(t *testing.T, db *sql.DB, id, ruleID, eventID, logicalKey, wantError string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO automation_event_deliveries(
			id, rule_id, preset_key, rule_version, source_event_id, logical_key,
			config_snapshot_json, action_snapshot_json, available_at, captured_at, updated_at
		) VALUES (?, ?, 'project-completed-inbox', 1, ?, ?, '{}', '{}', ?, ?, ?)
	`,
		id, ruleID, eventID, logicalKey,
		"2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z", "2026-08-30T12:00:00Z",
	)
	if err == nil || !strings.Contains(strings.ToUpper(err.Error()), strings.ToUpper(wantError)) {
		t.Fatalf("insert error = %v, want %q", err, wantError)
	}
}

func automationDeliveryLogicalKey(ruleID, eventID string) string {
	return "event:" + ruleID + ":" + eventID
}
