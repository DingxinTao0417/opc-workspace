package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestContentItemDueProjectionAdvancesPastTheFirstScanBatch(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "content-inbox-batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	dueAt, timezone := formatInboxTimestamp(now.Add(-time.Minute)), "UTC"
	for index := 1; index <= 101; index++ {
		item := models.ContentItem{
			ID:    fmt.Sprintf("018f0000-0000-7000-8000-%012d", 3800+index),
			Title: fmt.Sprintf("Content batch %03d", index), Platform: "Web", Status: "scheduled",
			ScheduledAt: &dueAt, ScheduledTimezone: &timezone, Version: 1,
			CreatedAt: formatInboxTimestamp(now.Add(-time.Hour)), UpdatedAt: formatInboxTimestamp(now.Add(-time.Hour)),
		}
		if err := store.DB.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'content_item'", 100)
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'content_item'", 101)
}

func TestContentItemDueProjectionIsIdempotentAndResolvesSupersededVersion(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "content-inbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	dueAt := formatInboxTimestamp(now.Add(-time.Minute))
	futureAt := formatInboxTimestamp(now.Add(time.Hour))
	timezone := "UTC"
	seed := func(id, title, status, scheduledAt string) models.ContentItem {
		row := models.ContentItem{
			ID: id, Title: title, Platform: "Newsletter", Status: status,
			ScheduledAt: &scheduledAt, ScheduledTimezone: &timezone, Version: 1,
			CreatedAt: formatInboxTimestamp(now.Add(-time.Hour)), UpdatedAt: formatInboxTimestamp(now.Add(-time.Hour)),
		}
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	review := seed("018f0000-0000-7000-8000-000000003801", "Review article", "in_review", dueAt)
	publish := seed("018f0000-0000-7000-8000-000000003802", "Publish article", "scheduled", dueAt)
	seed("018f0000-0000-7000-8000-000000003803", "Future article", "scheduled", futureAt)
	seed("018f0000-0000-7000-8000-000000003804", "Draft article", "draft", dueAt)

	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'content_item'", 2)

	var reviewInbox models.InboxItem
	if err := store.DB.First(&reviewInbox, "source_event_key = ?", contentItemInboxEventKey(review.ID, "review_due", 1)).Error; err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(reviewInbox.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if reviewInbox.Priority != "P2" || reviewInbox.DueAt == nil || *reviewInbox.DueAt != dueAt ||
		payload["event_type"] != "review_due" || payload["content_version"] != float64(1) {
		t.Fatalf("review projection=%#v payload=%#v", reviewInbox, payload)
	}
	var publishInbox models.InboxItem
	if err := store.DB.First(&publishInbox, "source_event_key = ?", contentItemInboxEventKey(publish.ID, "publish_due", 1)).Error; err != nil {
		t.Fatal(err)
	}
	if publishInbox.Priority != "P1" {
		t.Fatalf("publish priority=%s", publishInbox.Priority)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND action = 'source_projected' AND aggregate_id IN (SELECT id FROM inbox_items WHERE source_entity_type = 'content_item')", 2)

	options := Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	}
	router, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	updated := performRequest(
		router, http.MethodPatch, "/api/v1/content-items/"+publish.ID,
		[]byte(`{"title":"Publish revised article"}`), map[string]string{"If-Match": `"1"`},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("update projected Content Item=%d: %s", updated.Code, updated.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE id = ? AND status = 'resolved'", 1, publishInbox.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_resolved'", 1, publishInbox.ID)
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 2, publish.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ? AND status = 'open'", 1, contentItemInboxEventKey(publish.ID, "publish_due", 2))
}

func TestContentItemDeleteCoordinatesTerminalInboxSource(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "content-inbox-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	dueAt, timezone := formatInboxTimestamp(now.Add(-time.Minute)), "UTC"
	item := models.ContentItem{
		ID: "018f0000-0000-7000-8000-000000003811", Title: "Delete after archive",
		Platform: "Web", Status: "scheduled", ScheduledAt: &dueAt, ScheduledTimezone: &timezone,
		Version: 1, CreatedAt: formatInboxTimestamp(now.Add(-time.Hour)), UpdatedAt: formatInboxTimestamp(now.Add(-time.Hour)),
	}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	options := Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	}
	router, err := NewRouter(store.DB, options)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	archived := performRequest(
		router, http.MethodPatch, "/api/v1/content-items/"+item.ID,
		[]byte(`{"status":"archived"}`), map[string]string{"If-Match": `"1"`},
	)
	if archived.Code != http.StatusOK {
		t.Fatalf("archive Content Item=%d: %s", archived.Code, archived.Body.String())
	}
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/content-items/"+item.ID+"?confirm=true", nil,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, 2)},
	)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete Content Item=%d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'content_item' AND source_entity_id = ? AND source_deleted_at IS NOT NULL", 1, item.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND action = 'source_deleted' AND aggregate_id IN (SELECT id FROM inbox_items WHERE source_entity_type = 'content_item' AND source_entity_id = ?)", 1, item.ID)
}
