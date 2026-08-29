package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

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
