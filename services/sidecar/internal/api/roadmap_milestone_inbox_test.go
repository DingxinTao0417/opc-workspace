package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestRoadmapMilestoneDueProjectionUsesLocalCalendarDateAndStableScheduleDeduplication(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "roadmap-inbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("America/Tijuana-test", -7*60*60)
	now := time.Date(2026, 8, 29, 0, 15, 0, 0, location)
	createdAt := formatInboxTimestamp(now.Add(-time.Hour).UTC())
	for _, milestone := range []models.RoadmapMilestone{
		{ID: "018f0000-0000-7000-8000-000000003901", Title: "Due locally", Year: 2026, Quarter: 3, TargetDate: "2026-08-29", Status: "active", Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "018f0000-0000-7000-8000-000000003902", Title: "Tomorrow locally", Year: 2026, Quarter: 3, TargetDate: "2026-08-30", Status: "planned", Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt},
		{ID: "018f0000-0000-7000-8000-000000003903", Title: "Already achieved", Year: 2026, Quarter: 3, TargetDate: "2026-08-29", Status: "achieved", Version: 1, CreatedAt: createdAt, UpdatedAt: createdAt},
	} {
		if err := store.DB.Create(&milestone).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueRoadmapMilestones(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.projectDueRoadmapMilestones(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'roadmap_milestone'", 1)
	var projected models.InboxItem
	if err := store.DB.First(&projected, "source_entity_id = ?", "018f0000-0000-7000-8000-000000003901").Error; err != nil {
		t.Fatal(err)
	}
	if projected.DueAt == nil || *projected.DueAt != "2026-08-30T06:59:59Z" || projected.Priority != "P1" {
		t.Fatalf("projected Roadmap Inbox Item = %#v", projected)
	}
	if err := store.DB.Model(&models.RoadmapMilestone{}).Where("id = ?", "018f0000-0000-7000-8000-000000003901").Updates(map[string]any{"manual_order": 1024, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.projectDueRoadmapMilestones(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = 'roadmap_milestone'", 1)
}

func TestRoadmapMilestoneAPIProjectsTransitionsAndCoordinatesDeletion(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "roadmap-inbox-lifecycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("America/Tijuana-test", -7*60*60)
	now := time.Date(2026, 8, 29, 9, 0, 0, 0, location)
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()

	createdRecorder := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(`{
		"title":"Ship local event","year":2026,"quarter":3,"target_date":"2026-08-29","status":"active"
	}`), nil)
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create milestone = %d: %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeRoadmapMilestoneResponse(t, createdRecorder.Body.Bytes())
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ? AND status = 'open'", 1, created.ID)
	filtered := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=roadmap_milestone", nil, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filter Roadmap Inbox Items = %d: %s", filtered.Code, filtered.Body.String())
	}

	updated := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(`{"title":"Ship renamed local event"}`), map[string]string{"If-Match": `"1"`})
	if updated.Code != http.StatusOK {
		t.Fatalf("rename milestone = %d: %s", updated.Code, updated.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 1, created.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ? AND status = 'open'", 1, created.ID)

	achieved := performRequest(router, http.MethodPatch, "/api/v1/roadmap/milestones/"+created.ID, []byte(`{"status":"achieved"}`), map[string]string{"If-Match": `"2"`})
	if achieved.Code != http.StatusOK {
		t.Fatalf("achieve milestone = %d: %s", achieved.Code, achieved.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 2, created.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ? AND status = 'resolved' AND json_extract(payload_json, '$.event_type') = 'due'", 1, created.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ? AND status = 'open'", 1, roadmapMilestoneInboxEventKey(created.ID, "achieved", 3))

	archived := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones/"+created.ID+"/archive", nil, map[string]string{"If-Match": `"3"`})
	if archived.Code != http.StatusOK {
		t.Fatalf("archive milestone = %d: %s", archived.Code, archived.Body.String())
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/roadmap/milestones/"+created.ID+"?confirm=true", nil, map[string]string{"If-Match": `"4"`})
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete milestone = %d: %s", deleted.Code, deleted.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ? AND source_deleted_at IS NOT NULL", 2, created.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'inbox_item' AND action = 'source_deleted' AND aggregate_id IN (SELECT id FROM inbox_items WHERE source_entity_id = ?)", 2, created.ID)
}
