package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvoiceInboxProjectionMigrationGuardsSourcesAndPaidEntries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "invoice-inbox.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if store.SchemaVersion != 49 {
		t.Fatalf("schema version = %d, want 49", store.SchemaVersion)
	}
	const (
		clientID  = "018f0000-0000-7000-8000-000000004801"
		projectID = "018f0000-0000-7000-8000-000000004802"
		invoiceID = "018f0000-0000-7000-8000-000000004803"
		entryID   = "018f0000-0000-7000-8000-000000004804"
	)
	if err := store.DB.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, '迁移客户', 'active', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, clientID).Error; err != nil {
		t.Fatalf("seed Invoice projection client: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO projects(id, name, client_id, status, created_at, updated_at)
		VALUES (?, '迁移项目', ?, 'completed', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, projectID, clientID).Error; err != nil {
		t.Fatalf("seed Invoice projection project: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO invoices(id, invoice_number, client_id, project_id, amount_minor, currency, status,
		                     issue_date, due_date, notes, version, created_at, updated_at)
		VALUES (?, 'INV-2026-480', ?, ?, 128045, 'CNY', 'sent',
		        '2026-08-01', '2026-09-01', '', 2, '2026-08-01T00:00:00Z', '2026-08-02T00:00:00Z')
	`, invoiceID, clientID, projectID).Error; err != nil {
		t.Fatalf("seed Invoice projection Invoice: %v", err)
	}

	payload := fmt.Sprintf(`{"invoice_id":%q,"invoice_number":"INV-2026-480","client_id":%q,"client_name":"迁移客户","project_id":%q,"project_name":"迁移项目","amount_minor":128045,"currency":"CNY","due_date":"2026-09-01","due_state":"due_soon","occurrence_date":"2026-08-29","invoice_version":2,"projected_at":"2026-08-29T08:00:00Z","lead_days":3}`, invoiceID, clientID, projectID)
	insertSourceAt := func(id, key, body, dueAt string) error {
		return store.DB.Exec(`
			INSERT INTO inbox_items(
				id, kind, title, source_entity_type, source_entity_id, source_event_key,
				priority, status, resolution_policy, due_at, payload_json, version, created_at, updated_at
			) VALUES (?, 'event', '发票即将到期', 'invoice_due', ?, ?,
			          'P2', 'open', 'manual', ?, ?, 1,
			          '2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z')
		`, id, invoiceID, key, dueAt, body).Error
	}
	insertSource := func(id, key, body string) error {
		return insertSourceAt(id, key, body, "2026-09-01T23:59:59Z")
	}
	const sourceID = "018f0000-0000-7000-8000-000000004805"
	if err := insertSource(sourceID, "invoice:"+invoiceID+":due_soon:2026-09-01", payload); err != nil {
		t.Fatalf("insert valid Invoice due source: %v", err)
	}
	const draftInvoiceID = "018f0000-0000-7000-8000-000000004893"
	if err := store.DB.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, amount_minor, currency, status,
			issue_date, due_date, notes, version, created_at, updated_at
		) VALUES (
			?, 'INV-2026-DRAFT', ?, 128045, 'CNY', 'draft',
			'2026-08-01', '2026-09-01', '', 1,
			'2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z'
		)
	`, draftInvoiceID, clientID).Error; err != nil {
		t.Fatalf("seed draft Invoice: %v", err)
	}
	draftPayload := strings.Replace(payload, invoiceID, draftInvoiceID, 1)
	draftPayload = strings.Replace(draftPayload, `"invoice_number":"INV-2026-480"`, `"invoice_number":"INV-2026-DRAFT"`, 1)
	draftPayload = strings.Replace(draftPayload, `"project_id":"`+projectID+`","project_name":"迁移项目"`, `"project_id":null,"project_name":null`, 1)
	draftPayload = strings.Replace(draftPayload, `"invoice_version":2`, `"invoice_version":1`, 1)
	if err := store.DB.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, source_entity_type, source_entity_id, source_event_key,
			priority, status, resolution_policy, due_at, payload_json,
			version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000004890', 'event', '草稿伪造来源',
			'invoice_due', ?, ?, 'P2', 'open', 'manual',
			'2026-09-01T23:59:59Z', ?, 1,
			'2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z'
		)
	`, draftInvoiceID, "invoice:"+draftInvoiceID+":due_soon:2026-09-01", draftPayload).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_DUE_INBOX_SOURCE_NOT_FOUND") {
		t.Fatalf("draft Invoice due source error = %v", err)
	}
	for name, body := range map[string]string{
		"missing exact field":           strings.Replace(payload, `,"lead_days":3`, "", 1),
		"missing with unexpected field": strings.Replace(payload, `,"lead_days":3`, `,"unexpected":3`, 1),
		"extra exact field":             strings.Replace(payload, `,"lead_days":3`, `,"lead_days":3,"extra":true`, 1),
		"invalid occurrence":            strings.Replace(payload, `"occurrence_date":"2026-08-29"`, `"occurrence_date":"2026-08-20"`, 1),
		"blank client name":             strings.Replace(payload, `"client_name":"迁移客户"`, `"client_name":" "`, 1),
		"blank project name":            strings.Replace(payload, `"project_name":"迁移项目"`, `"project_name":" "`, 1),
		"invalid currency":              strings.Replace(payload, `"currency":"CNY"`, `"currency":"cny"`, 1),
		"invalid projected at":          strings.Replace(payload, `"projected_at":"2026-08-29T08:00:00Z"`, `"projected_at":"2026-08-29 08:00:00"`, 1),
		"projected at missing seconds":  strings.Replace(payload, `"projected_at":"2026-08-29T08:00:00Z"`, `"projected_at":"2026-08-29T08:00Z"`, 1),
		"projected at long fraction":    strings.Replace(payload, `"projected_at":"2026-08-29T08:00:00Z"`, `"projected_at":"2026-08-29T08:00:00.1234567890Z"`, 1),
		"projected at hour 24":          strings.Replace(payload, `"projected_at":"2026-08-29T08:00:00Z"`, `"projected_at":"2026-08-29T24:00:00Z"`, 1),
		"projected at invalid leap day": strings.Replace(payload, `"projected_at":"2026-08-29T08:00:00Z"`, `"projected_at":"2026-02-29T08:00:00Z"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			err := insertSource("118f0000-0000-7000-8000-"+fmt.Sprintf("%012d", len(name)), "invoice:"+invoiceID+":due_soon:2026-09-01", body)
			if err == nil || !strings.Contains(err.Error(), "INVALID_INVOICE_DUE_INBOX_SOURCE") {
				t.Fatalf("invalid Invoice due source error = %v", err)
			}
		})
	}
	for index, dueAt := range []string{
		"2026-09-01 23:59:59",
		"2026-09-01T23:59Z",
		"2026-09-01T23:59:59.1234567890Z",
		"2026-09-01T24:00:00Z",
		"2026-02-29T23:59:59Z",
	} {
		if err := insertSourceAt(
			"118f0000-0000-7000-8000-"+fmt.Sprintf("%012d", 4899+index),
			"invoice:"+invoiceID+":due_soon:2026-09-01",
			payload,
			dueAt,
		); err == nil || !strings.Contains(err.Error(), "INVALID_INVOICE_DUE_INBOX_SOURCE") {
			t.Fatalf("invalid Invoice due_at %q error = %v", dueAt, err)
		}
	}
	const deletedInvoiceID = "018f0000-0000-7000-8000-000000004897"
	deletedPayload := strings.Replace(payload, invoiceID, deletedInvoiceID, 1)
	if err := store.DB.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, source_entity_type, source_entity_id, source_event_key,
			priority, status, resolution_policy, due_at, payload_json,
			source_deleted_at, triaged_at, resolved_by_actor_id, resolved_at,
			resolution_reason, resolution_mode, version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000004898', 'event', '已删除发票快照',
			'invoice_due', ?, ?, 'P2', 'resolved', 'manual',
			'2026-09-01T23:59:59Z', ?, '2026-09-02T08:00:00Z',
			'2026-09-02T08:00:00Z', '00000000-0000-5000-8000-000000000001',
			'2026-09-02T08:00:00Z', '来源已删除', 'manual', 1,
			'2026-08-29T08:00:00Z', '2026-09-02T00:00:00Z'
		)
	`, deletedInvoiceID, "invoice:"+deletedInvoiceID+":due_soon:2026-09-01", deletedPayload).Error; err != nil {
		t.Fatalf("insert terminal deleted Invoice source snapshot: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, source_entity_type, source_entity_id, source_event_key,
			priority, status, resolution_policy, due_at, payload_json,
			source_deleted_at, triaged_at, resolved_by_actor_id, resolved_at,
			resolution_reason, resolution_mode, version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000004896', 'event', '碰撞快照',
			'invoice_due', ?, ?, 'P2', 'resolved', 'manual',
			'2026-09-01T23:59:59Z', ?, '2026-09-02T08:00:00Z',
			'2026-09-02T08:00:00Z', '00000000-0000-5000-8000-000000000001',
			'2026-09-02T08:00:00Z', '来源已删除', 'manual', 1,
			'2026-08-29T08:00:00Z', '2026-09-02T00:00:00Z'
		)
	`, invoiceID, "invoice:"+invoiceID+":due:2026-09-01", strings.Replace(payload, `"due_state":"due_soon","occurrence_date":"2026-08-29"`, `"due_state":"due","occurrence_date":"2026-09-01"`, 1)).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_DUE_DELETED_SOURCE_COLLISION") {
		t.Fatalf("deleted Invoice source collision error = %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO inbox_items(
			id, kind, title, source_entity_type, source_event_key, priority, status,
			resolution_policy, payload_json, version, created_at, updated_at
		) VALUES (
			'018f0000-0000-7000-8000-000000004895', 'event', '普通来源',
			'generic', 'generic:source', 'P3', 'open', 'manual', '{}', 1,
			'2026-08-29T08:00:00Z', '2026-08-29T08:00:00Z'
		)
	`).Error; err != nil {
		t.Fatalf("insert generic Inbox source: %v", err)
	}
	if err := store.DB.Exec(`
		UPDATE inbox_items
		SET source_entity_type='invoice_due', source_entity_id=?, source_event_key=?,
		    due_at='2026-09-01T23:59:59Z', payload_json=?
		WHERE id='018f0000-0000-7000-8000-000000004895'
	`, invoiceID, "invoice:"+invoiceID+":due:2026-09-01", payload).Error; err == nil || !strings.Contains(err.Error(), "INVALID_INVOICE_DUE_INBOX_SOURCE") {
		t.Fatalf("update into Invoice due source error = %v", err)
	}
	if err := store.DB.Exec("UPDATE inbox_items SET payload_json = '{}' WHERE id = ?", sourceID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_DUE_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("Invoice due source identity update error = %v", err)
	}
	if err := store.DB.Exec("UPDATE inbox_items SET due_at = '2026-09-02T23:59:59Z' WHERE id = ?", sourceID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_DUE_INBOX_SOURCE_IMMUTABLE") {
		t.Fatalf("Invoice due_at identity update error = %v", err)
	}
	if err := store.DB.Exec("UPDATE inbox_items SET source_deleted_at = '2026-08-30T00:00:00Z' WHERE id = ?", sourceID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_DUE_INBOX_SOURCE_ACTIVE") {
		t.Fatalf("active Invoice due source deletion error = %v", err)
	}
	for _, deletedAt := range []string{
		"2026-08-30 00:00:00",
		"2026-08-30T00:00Z",
		"2026-08-30T00:00:00.1234567890Z",
		"2026-08-30T24:00:00Z",
		"2026-02-29T00:00:00Z",
	} {
		if err := store.DB.Exec(`
			UPDATE inbox_items
			SET status='resolved', triaged_at='2026-08-30T00:00:00Z',
			    resolved_by_actor_id='00000000-0000-5000-8000-000000000001',
			    resolved_at='2026-08-30T00:00:00Z', resolution_reason='完成',
			    resolution_mode='manual', source_deleted_at=?
			WHERE id=?
		`, deletedAt, sourceID).Error; err == nil || !strings.Contains(err.Error(), "INVALID_INVOICE_DUE_SOURCE_DELETED_AT") {
			t.Fatalf("invalid Invoice source_deleted_at %q update error = %v", deletedAt, err)
		}
	}

	if err := store.DB.Exec("UPDATE invoices SET status='viewed', version=3, updated_at='2026-08-03T00:00:00Z' WHERE id=?", invoiceID).Error; err != nil {
		t.Fatalf("advance Invoice to viewed: %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO financial_entries(
			id, type, amount_minor, currency, occurred_on, status, category, client_id, project_id,
			invoice_id, notes, created_by_actor_id, version, created_at, updated_at
		) VALUES ('018f0000-0000-7000-8000-000000004894', 'income', 1, 'CNY', '2026-08-30',
		          'confirmed', '坏发票回款', ?, ?, ?, '',
		          '00000000-0000-5000-8000-000000000001', 1,
		          '2026-08-30T00:00:00Z', '2026-08-30T00:00:00Z')
	`, clientID, projectID, invoiceID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_LINKED_FINANCIAL_ENTRY_MISMATCH") {
		t.Fatalf("mismatched linked entry insert error = %v", err)
	}
	if err := store.DB.Exec("UPDATE invoices SET status='paid', paid_date='2026-08-30', version=4, updated_at='2026-08-30T00:00:00Z' WHERE id=?", invoiceID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_PAID_ENTRY_MISMATCH") {
		t.Fatalf("paid Invoice without matching entry error = %v", err)
	}
	if err := store.DB.Exec(`
		INSERT INTO financial_entries(
			id, type, amount_minor, currency, occurred_on, status, category, client_id, project_id,
			invoice_id, notes, created_by_actor_id, version, created_at, updated_at
		) VALUES (?, 'income', 128045, 'CNY', '2026-08-30', 'confirmed', '发票回款', ?, ?, ?, '',
		          '00000000-0000-5000-8000-000000000001', 1, '2026-08-30T00:00:00Z', '2026-08-30T00:00:00Z')
	`, entryID, clientID, projectID, invoiceID).Error; err != nil {
		t.Fatalf("insert matching Invoice entry: %v", err)
	}
	if err := store.DB.Exec("UPDATE invoices SET status='paid', paid_date='2026-08-30', version=4, updated_at='2026-08-30T00:00:00Z' WHERE id=?", invoiceID).Error; err != nil {
		t.Fatalf("pay Invoice with matching entry: %v", err)
	}
	if err := store.DB.Exec("UPDATE financial_entries SET amount_minor=1 WHERE id=?", entryID).Error; err == nil || !strings.Contains(err.Error(), "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE") {
		t.Fatalf("linked entry mutation error = %v", err)
	}
	if err := store.DB.Exec("UPDATE clients SET name='已重命名客户' WHERE id=?", clientID).Error; err != nil {
		t.Fatalf("rename historical source client: %v", err)
	}
	if err := store.DB.Exec("UPDATE projects SET name='已重命名项目' WHERE id=?", projectID).Error; err != nil {
		t.Fatalf("rename historical source project: %v", err)
	}

	historicalPayload := strings.Replace(payload, `"due_state":"due_soon","occurrence_date":"2026-08-29"`, `"due_state":"due","occurrence_date":"2026-09-01"`, 1)
	if err := insertSource("018f0000-0000-7000-8000-000000004806", "invoice:"+invoiceID+":due:2026-09-01", historicalPayload); err != nil {
		t.Fatalf("restore historical source after Invoice became paid: %v", err)
	}
	assertNoForeignKeyViolations(t, store.SQL)
}

func TestInvoiceInboxProjectionMigrationRejectsExistingPaymentInconsistency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invoice-inbox-invalid-v47.db")
	v47 := openDatabaseAtVersion(t, path, 47)
	const (
		clientID  = "018f0000-0000-7000-8000-000000004891"
		invoiceID = "018f0000-0000-7000-8000-000000004892"
	)
	if _, err := v47.Exec(`
		INSERT INTO clients(id, name, status, created_at, updated_at)
		VALUES (?, '不一致付款客户', 'active', '2026-08-01T00:00:00Z', '2026-08-01T00:00:00Z')
	`, clientID); err != nil {
		t.Fatalf("seed v47 client: %v", err)
	}
	if _, err := v47.Exec(`
		INSERT INTO invoices(
			id, invoice_number, client_id, amount_minor, currency, status,
			issue_date, due_date, paid_date, notes, version, created_at, updated_at
		) VALUES (
			?, 'INV-2026-489', ?, 8800, 'CNY', 'paid',
			'2026-08-01', '2026-08-20', '2026-08-18', '', 2,
			'2026-08-01T00:00:00Z', '2026-08-18T00:00:00Z'
		)
	`, invoiceID, clientID); err != nil {
		t.Fatalf("seed inconsistent v47 paid Invoice: %v", err)
	}
	if err := v47.Close(); err != nil {
		t.Fatalf("close inconsistent v47 database: %v", err)
	}
	if store, err := Open(path); err == nil {
		_ = store.Close()
		t.Fatal("v48 migration accepted paid Invoice without matching entry")
	} else if !strings.Contains(err.Error(), "invoice_payment_consistency_v48_guard") {
		t.Fatalf("v48 payment consistency migration error = %v", err)
	}
}
