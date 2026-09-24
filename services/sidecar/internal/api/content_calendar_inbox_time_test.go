package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func contentDueTimeFixture(t *testing.T, now time.Time) (*database.Store, *API) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "content-due-time.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
}

func seedContentDueTime(t *testing.T, store *database.Store, scheduledAt string) models.ContentItem {
	t.Helper()
	zone := "UTC"
	item := models.ContentItem{
		ID: uuid.NewString(), Title: "private-content-title", Platform: "Web", Status: "scheduled",
		ScheduledAt: &scheduledAt, ScheduledTimezone: &zone, Version: 1,
		CreatedAt: "2026-08-29T00:00:00Z", UpdatedAt: "2026-08-29T00:00:00Z",
	}
	if err := store.DB.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	return item
}

func TestContentItemDueTimeBoundaryUsesRealInstant(t *testing.T) {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		now  time.Time
		at   string
		want int64
	}{
		{"whole_second_due_during_same_second", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00Z", 1},
		{"whole_second_exactly_due", base, "2026-08-29T12:00:00Z", 1},
		{"short_fraction_exactly_due", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.5Z", 1},
		{"fixed_fraction_exactly_due", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.500000000Z", 1},
		{"fraction_future_in_same_second", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.500000001Z", 0},
		{"one_nanosecond_is_future", base, "2026-08-29T12:00:00.000000001Z", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, service := contentDueTimeFixture(t, tc.now)
			item := seedContentDueTime(t, store, tc.at)
			for scan := 0; scan < 2; scan++ {
				if err := service.projectDueContentItems(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", tc.want, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_projected'", tc.want)
			if tc.want == 1 {
				var inbox models.InboxItem
				if err := store.DB.First(&inbox, "source_entity_id=?", item.ID).Error; err != nil {
					t.Fatal(err)
				}
				if inbox.SourceEventKey == nil || *inbox.SourceEventKey != contentItemInboxEventKey(item.ID, "publish_due", 1) || inbox.DueAt == nil || *inbox.DueAt != tc.at {
					t.Fatalf("projection identity/time changed: %#v", inbox)
				}
			}
		})
	}
}

func TestContentItemDueTimeBatchOrdersRealInstants(t *testing.T) {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store, service := contentDueTimeFixture(t, base.Add(time.Second))
	whole := seedContentDueTime(t, store, base.Format(time.RFC3339Nano))
	var last models.ContentItem
	for index := 1; index <= 100; index++ {
		last = seedContentDueTime(t, store, base.Add(time.Duration(index)*time.Nanosecond).Format(time.RFC3339Nano))
	}
	if err := service.projectDueContentItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item'", 100)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", 1, whole.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", 0, last.ID)
	for scan := 0; scan < 2; scan++ {
		if err := service.projectDueContentItems(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='content_item'", 101)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='source_projected'", 101)
}

func TestContentItemDueTimeTransactionRechecksReschedule(t *testing.T) {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store, service := contentDueTimeFixture(t, base)
	item := seedContentDueTime(t, store, base.Add(-time.Second).Format(time.RFC3339Nano))
	// Simulate a candidate selected before the user moves it into the future.
	future := base.Add(time.Nanosecond).Format(time.RFC3339Nano)
	if err := store.DB.Model(&models.ContentItem{}).Where("id=?", item.ID).
		Updates(map[string]any{"scheduled_at": future, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.projectContentItemDue(context.Background(), item.ID, formatInboxTimestamp(base)); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	if err := service.projectContentItemDue(context.Background(), item.ID, future); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key=?", 1, contentItemInboxEventKey(item.ID, "publish_due", 2))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key=?", 0, contentItemInboxEventKey(item.ID, "publish_due", 1))
}

func TestContentItemDueTimeMalformedValueFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	for _, at := range []string{
		"2026-08-29T00:00:00.private-schedule-secretZ", "2026-02-30T00:00:00Z",
		"2026-08-29T00:00:00.1234567891Z", "2026-08-29T00:00:00,5Z",
		"2026-08-29T00:00:00+00:00", "2026-08-29T0:00:00Z",
	} {
		t.Run(at, func(t *testing.T) {
			store, service := contentDueTimeFixture(t, now)
			item := seedContentDueTime(t, store, at)
			err := service.projectContentItemDue(context.Background(), item.ID, formatInboxTimestamp(now))
			if err == nil {
				t.Fatal("invalid stored schedule was accepted")
			}
			for _, secret := range []string{at, item.Title, "private-schedule-secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error exposed private value: %s", err)
				}
			}
			if err := service.projectDueContentItems(context.Background()); err == nil || strings.Contains(err.Error(), at) {
				t.Fatalf("scan must reject invalid schedule without echo: %v", err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item'", 0)
		})
	}
	store, service := contentDueTimeFixture(t, now)
	item := seedContentDueTime(t, store, "2026-08-29T00:00:00Z")
	if err := service.projectContentItemDue(context.Background(), item.ID, "private-invalid-clock"); err == nil || strings.Contains(err.Error(), "private-invalid-clock") {
		t.Fatalf("clock must fail closed without echo: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.projectDueContentItems(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
}

func TestContentItemDueTimeTransactionKeepsSourceIdentityGuard(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 500000000, time.UTC)
	store, service := contentDueTimeFixture(t, now)
	item := seedContentDueTime(t, store, "2026-08-29T12:00:00Z")
	key := contentItemInboxEventKey(item.ID, "publish_due", 1)
	// A legacy incompatible record must not be accepted as an idempotent receipt.
	row := models.InboxItem{ID: uuid.NewString(), Kind: "event", Title: "Existing unrelated source", SourceEntityType: "legacy_test_source", SourceEventKey: &key, Priority: "P2", Status: "open", ResolutionPolicy: "manual", PayloadJSON: "{}", Version: 1, CreatedAt: formatInboxTimestamp(now), UpdatedAt: formatInboxTimestamp(now)}
	if err := store.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.projectContentItemDue(context.Background(), item.ID, formatInboxTimestamp(now)); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("incompatible source accepted: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item'", 0)
}
