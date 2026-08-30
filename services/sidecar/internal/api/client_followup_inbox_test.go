package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInboxListFiltersDueClientFollowups(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	followupID := "018f0000-0000-7000-8000-000000001510"
	clientID := "018f0000-0000-7000-8000-000000001511"
	now := formatInboxTimestamp(clock.now)
	dueAt := "2026-08-29T11:59:00.000000000Z"
	key := "followup:" + followupID + ":due:1"
	if err := store.DB.Create(&models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: "确认回访结果", Summary: "客户：Inbox Filter Client · 渠道：phone",
		SourceEntityType: clientFollowupInboxSourceType, SourceEntityID: &followupID, SourceEventKey: &key,
		Priority: "P2", Status: "open", ResolutionPolicy: "manual", DueAt: &dueAt,
		PayloadJSON: `{"client_followup_id":"` + followupID + `","client_id":"` + clientID + `","scheduled_at":"` + dueAt + `","timezone":"UTC","channel":"phone"}`,
		Version:     1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createInboxItemForTest(t, router, `{"title":"普通事项"}`, "")

	response := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=client_followup", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("filtered Inbox list = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []inboxItemOutput `json:"data"`
		Meta inboxListMeta     `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || len(envelope.Data) != 1 || envelope.Data[0].SourceEntityID == nil || *envelope.Data[0].SourceEntityID != followupID || envelope.Meta.Total != 1 {
		t.Fatalf("filtered Inbox data=%s err=%v", response.Body.String(), err)
	}
	invalid := performRequest(router, http.MethodGet, "/api/v1/inbox-items?source_entity_type=task_due", nil, nil)
	if invalid.Code != http.StatusBadRequest || responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid source filter = %d: %s", invalid.Code, invalid.Body.String())
	}
}

func TestClientFollowupDueProjectionIsIdempotentAndSkipsTerminalPlans(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "followup-projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	client := models.Client{ID: "018f0000-0000-7000-8000-000000001501", Name: "Followup Projection Client", Status: "active", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	seed := func(id, status string) models.ClientFollowup {
		row := models.ClientFollowup{ID: id, ClientID: client.ID, AssignedActorID: models.BuiltinOwnerActorID, ScheduledAt: "2026-08-29T11:59:00.000000000Z", Timezone: "UTC", Channel: "phone", Purpose: "Follow up", Status: status, Priority: "normal", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
		if status == "cancelled" {
			at, reason := now.Format(time.RFC3339Nano), "rescheduled"
			row.CancelledAt, row.CancelReason, row.Version = &at, &reason, 2
		}
		if err := store.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	due := seed("018f0000-0000-7000-8000-000000001502", "planned")
	terminal := seed("018f0000-0000-7000-8000-000000001503", "cancelled")
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueClientFollowups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.projectDueClientFollowups(context.Background()); err != nil {
		t.Fatal(err)
	}
	key := "followup:" + due.ID + ":due:1"
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key = ?", 1, key)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 0, terminal.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND aggregate_id = ? AND action = 'client_followup_due'", 1, due.ID)
}

func TestClientFollowupDueProjectionDrainsBoundedBacklogWithoutRepeats(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "followup-projection-backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	nowText := formatInboxTimestamp(now)
	client := models.Client{
		ID:        uuid.NewString(),
		Name:      "Followup Projection Backlog Client",
		Status:    "active",
		Version:   1,
		CreatedAt: nowText,
		UpdatedAt: nowText,
	}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	dueAt := formatInboxTimestamp(now.Add(-time.Minute))
	rows := make([]models.ClientFollowup, 0, 103)
	for index := 0; index < 101; index++ {
		rows = append(rows, models.ClientFollowup{
			ID:              uuid.NewString(),
			ClientID:        client.ID,
			AssignedActorID: models.BuiltinOwnerActorID,
			ScheduledAt:     dueAt,
			Timezone:        "UTC",
			Channel:         "phone",
			Purpose:         fmt.Sprintf("Due followup %03d", index),
			Status:          "planned",
			Priority:        "normal",
			Version:         1,
			CreatedAt:       nowText,
			UpdatedAt:       nowText,
		})
	}
	future := models.ClientFollowup{
		ID:              uuid.NewString(),
		ClientID:        client.ID,
		AssignedActorID: models.BuiltinOwnerActorID,
		ScheduledAt:     formatInboxTimestamp(now.Add(time.Minute)),
		Timezone:        "UTC",
		Channel:         "phone",
		Purpose:         "Future followup",
		Status:          "planned",
		Priority:        "normal",
		Version:         1,
		CreatedAt:       nowText,
		UpdatedAt:       nowText,
	}
	completedAt, result := nowText, "done"
	terminal := models.ClientFollowup{
		ID:              uuid.NewString(),
		ClientID:        client.ID,
		AssignedActorID: models.BuiltinOwnerActorID,
		ScheduledAt:     dueAt,
		Timezone:        "UTC",
		Channel:         "phone",
		Purpose:         "Completed followup",
		Status:          "completed",
		Priority:        "normal",
		CompletedAt:     &completedAt,
		Result:          &result,
		Version:         2,
		CreatedAt:       nowText,
		UpdatedAt:       nowText,
	}
	rows = append(rows, future, terminal)
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	project := func(want int64) {
		t.Helper()
		if err := service.projectDueClientFollowups(context.Background()); err != nil {
			t.Fatal(err)
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type = ?", want, clientFollowupInboxSourceType)
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND action = 'client_followup_due'", want)
	}

	project(100)
	project(101)
	project(101)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 0, future.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id = ?", 0, terminal.ID)
}

func TestClientFollowupDueProjectionUsesCurrentVersionKey(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "followup-projection-version.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	nowText := formatInboxTimestamp(now)
	client := models.Client{ID: uuid.NewString(), Name: "Versioned Followup Client", Status: "active", Version: 1, CreatedAt: nowText, UpdatedAt: nowText}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	followup := models.ClientFollowup{
		ID: uuid.NewString(), ClientID: client.ID, AssignedActorID: models.BuiltinOwnerActorID,
		ScheduledAt: formatInboxTimestamp(now.Add(-time.Minute)), Timezone: "UTC", Channel: "phone",
		Purpose: "Versioned followup", Status: "planned", Priority: "normal", Version: 2,
		CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&followup).Error; err != nil {
		t.Fatal(err)
	}
	oldKey := fmt.Sprintf("followup:%s:due:1", followup.ID)
	ownerID, resolutionReason, resolutionMode := models.BuiltinOwnerActorID, "superseded", "manual"
	if err := store.DB.Create(&models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: "Old followup projection", Summary: "old",
		SourceEntityType: clientFollowupInboxSourceType, SourceEntityID: &followup.ID, SourceEventKey: &oldKey,
		Priority: "P2", Status: "resolved", ResolutionPolicy: "manual", DueAt: &followup.ScheduledAt,
		TriagedAt: &nowText, ResolvedByActorID: &ownerID, ResolvedAt: &nowText,
		ResolutionReason: &resolutionReason, ResolutionMode: &resolutionMode, PayloadJSON: `{}`,
		Version: 2, CreatedAt: nowText, UpdatedAt: nowText,
	}).Error; err != nil {
		t.Fatal(err)
	}

	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	if err := service.projectDueClientFollowups(context.Background()); err != nil {
		t.Fatal(err)
	}
	currentKey := fmt.Sprintf("followup:%s:due:2", followup.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key IN (?, ?)", 2, oldKey, currentKey)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND aggregate_id = ? AND action = 'client_followup_due'", 1, followup.ID)
}

func TestClientFollowupDueProjectionRejectsIncompatibleCurrentKeyOwner(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "followup-projection-invalid-key.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	nowText := formatInboxTimestamp(now)
	client := models.Client{ID: uuid.NewString(), Name: "Invalid Projection Client", Status: "active", Version: 1, CreatedAt: nowText, UpdatedAt: nowText}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	followup := models.ClientFollowup{
		ID: uuid.NewString(), ClientID: client.ID, AssignedActorID: models.BuiltinOwnerActorID,
		ScheduledAt: formatInboxTimestamp(now.Add(-time.Minute)), Timezone: "UTC", Channel: "phone",
		Purpose: "Invalid key owner", Status: "planned", Priority: "normal", Version: 1,
		CreatedAt: nowText, UpdatedAt: nowText,
	}
	if err := store.DB.Create(&followup).Error; err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("followup:%s:due:1", followup.ID)
	if err := store.DB.Create(&models.InboxItem{
		ID: uuid.NewString(), Kind: "reminder", Title: "Wrong projection owner", Summary: "invalid",
		SourceEntityType: clientFollowupInboxSourceType, SourceEntityID: &followup.ID, SourceEventKey: &key,
		Priority: "P2", Status: "open", ResolutionPolicy: "manual", DueAt: &followup.ScheduledAt,
		PayloadJSON: `{}`, Version: 1, CreatedAt: nowText, UpdatedAt: nowText,
	}).Error; err != nil {
		t.Fatal(err)
	}

	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	err = service.projectDueClientFollowups(context.Background())
	if err == nil || !strings.Contains(err.Error(), "source_event_key belongs to an incompatible Inbox Item") {
		t.Fatalf("projection error = %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type = 'client_followup' AND aggregate_id = ? AND action = 'client_followup_due'", 0, followup.ID)
}
