package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func followupDueTimeFixture(t *testing.T, now *time.Time) (http.Handler, *database.Store, *API, string) {
	t.Helper()
	router, store := newTaskDueFilterAPIWithClock(t, func() time.Time { return *now })
	client := createClientForTest(t, router, `{"name":"Private due-time client"}`, nil)
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return *now }}}
	return router, store, service, client.ID
}

// Use the real create API so the regression proves its normal RFC3339Nano
// storage format reaches the scanner without manually manufacturing a row.
func createFollowupDueTime(t *testing.T, router http.Handler, store *database.Store, clientID, at string) models.ClientFollowup {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"client_id": clientID, "assigned_actor_id": models.BuiltinOwnerActorID,
		"scheduled_at": at, "timezone": "UTC", "channel": "phone",
		"purpose": "Private followup purpose", "notes": "Private followup notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/client-followups", body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create followup=%d %s", response.Code, response.Body.String())
	}
	id := decodeClientFollowupResponse(t, response.Body.Bytes()).ID
	var row models.ClientFollowup
	if err := store.DB.First(&row, "id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestClientFollowupDueTimeNativeBoundary(t *testing.T) {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		now   time.Time
		at    string
		fixed bool
		want  int64
	}{
		{"whole_second_same_second", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00Z", false, 1},
		{"whole_second_exact_due", base, "2026-08-29T12:00:00Z", false, 1},
		{"short_fraction_exact_due", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.5Z", false, 1},
		{"fixed_nine_fraction_compatible", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.500000000Z", true, 1},
		{"same_second_one_nanosecond_future", base.Add(500 * time.Millisecond), "2026-08-29T12:00:00.500000001Z", false, 0},
		{"whole_second_one_nanosecond_future", base, "2026-08-29T12:00:00.000000001Z", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := tc.now
			router, store, service, clientID := followupDueTimeFixture(t, &now)
			item := createFollowupDueTime(t, router, store, clientID, tc.at)
			if tc.fixed {
				// Historical fixed-nine timestamps remain valid, even though the
				// current native API normalizes away trailing fractional zeroes.
				if err := store.DB.Model(&models.ClientFollowup{}).Where("id=?", item.ID).
					Updates(map[string]any{"scheduled_at": tc.at, "version": item.Version + 1}).Error; err != nil {
					t.Fatal(err)
				}
				item.ScheduledAt, item.Version = tc.at, item.Version+1
			} else if item.ScheduledAt != tc.at {
				t.Fatalf("unexpected native stored timestamp: %s", item.ScheduledAt)
			}
			for scan := 0; scan < 2; scan++ {
				if err := service.projectDueClientFollowups(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", tc.want, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='client_followup' AND aggregate_id=? AND action='client_followup_due'", tc.want, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='created'", tc.want)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM client_followups WHERE id=? AND version=? AND status='planned' AND scheduled_at=?", 1, item.ID, item.Version, item.ScheduledAt)
			if tc.want == 1 {
				var inbox models.InboxItem
				if err := store.DB.First(&inbox, "source_entity_id=?", item.ID).Error; err != nil {
					t.Fatal(err)
				}
				if inbox.SourceEventKey == nil || *inbox.SourceEventKey != fmt.Sprintf("followup:%s:due:%d", item.ID, item.Version) || inbox.DueAt == nil || *inbox.DueAt != item.ScheduledAt {
					t.Fatalf("projection changed original source identity/time: %#v", inbox)
				}
			}
		})
	}
}

func TestClientFollowupDueTimeNativeBatchOrderAndProgress(t *testing.T) {
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	now := base.Add(time.Second)
	router, store, service, clientID := followupDueTimeFixture(t, &now)
	whole := createFollowupDueTime(t, router, store, clientID, base.Format(time.RFC3339Nano))
	var last models.ClientFollowup
	for index := 1; index <= 100; index++ {
		last = createFollowupDueTime(t, router, store, clientID, base.Add(time.Duration(index)*time.Nanosecond).Format(time.RFC3339Nano))
	}
	if err := service.projectDueClientFollowups(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='client_followup'", 100)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", 1, whole.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_id=?", 0, last.ID)
	for scan := 0; scan < 2; scan++ {
		if err := service.projectDueClientFollowups(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_entity_type='client_followup'", 101)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='client_followup' AND action='client_followup_due'", 101)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item' AND action='created'", 101)
}

func TestClientFollowupDueTimeTransactionRechecksNativeReschedule(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 500000000, time.UTC)
	router, store, service, clientID := followupDueTimeFixture(t, &now)
	item := createFollowupDueTime(t, router, store, clientID, now.Add(-time.Second).Format(time.RFC3339Nano))
	scanTime := formatInboxTimestamp(now)
	future := now.Add(time.Nanosecond)
	response := performRequest(router, http.MethodPatch, "/api/v1/client-followups/"+item.ID, []byte(fmt.Sprintf(`{"scheduled_at":%q}`, future.Format(time.RFC3339Nano))), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)})
	if response.Code != http.StatusOK {
		t.Fatalf("native edit=%d %s", response.Code, response.Body.String())
	}
	// The candidate ID was selected before the edit. A later clock must not
	// substitute for the scan's captured time when re-checking the current row.
	now = future.Add(time.Hour)
	if err := service.projectClientFollowup(context.Background(), item.ID, scanTime); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	if err := service.projectClientFollowup(context.Background(), item.ID, formatInboxTimestamp(future)); err != nil {
		t.Fatal(err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key=?", 1, fmt.Sprintf("followup:%s:due:%d", item.ID, item.Version+1))
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items WHERE source_event_key=?", 0, fmt.Sprintf("followup:%s:due:%d", item.ID, item.Version))
}

func TestClientFollowupDueTimeMalformedStorageFailsClosed(t *testing.T) {
	for _, at := range []string{
		"2026-08-29T00:00:00.secretZ", "2026-02-30T00:00:00Z",
		"2026-08-29T00:00:00.1234567891Z", "2026-08-29T00:00:00,5Z", "2026-08-29T00:00:00+00:00",
	} {
		t.Run(at, func(t *testing.T) {
			now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
			router, store, service, clientID := followupDueTimeFixture(t, &now)
			item := createFollowupDueTime(t, router, store, clientID, "2026-08-29T00:00:00Z")
			if err := store.DB.Model(&models.ClientFollowup{}).Where("id=?", item.ID).Updates(map[string]any{"scheduled_at": at, "version": item.Version + 1}).Error; err != nil {
				t.Fatal(err)
			}
			for _, run := range []func() error{
				func() error {
					return service.projectClientFollowup(context.Background(), item.ID, formatInboxTimestamp(now))
				},
				func() error { return service.projectDueClientFollowups(context.Background()) },
			} {
				err := run()
				if err == nil {
					t.Fatal("invalid stored schedule accepted")
				}
				for _, secret := range []string{at, "secret", item.Purpose, "Private due-time client"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaked private fact: %v", err)
					}
				}
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='client_followup_due'", 0)
		})
	}
}

func TestClientFollowupDueTimeCancellationAndAtomicAudit(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 500000000, time.UTC)
	router, store, service, clientID := followupDueTimeFixture(t, &now)
	item := createFollowupDueTime(t, router, store, clientID, "2026-08-29T12:00:00Z")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.projectDueClientFollowups(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation=%v", err)
	}
	if err := service.projectClientFollowup(ctx, item.ID, formatInboxTimestamp(now)); !errors.Is(err, context.Canceled) {
		t.Fatalf("projection cancellation=%v", err)
	}
	if err := service.projectClientFollowup(context.Background(), item.ID, "private-invalid-clock"); err == nil || strings.Contains(err.Error(), "private-invalid-clock") {
		t.Fatalf("clock must fail closed without echo: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	if err := store.DB.Exec(`CREATE TRIGGER fail_followup_due_time_audit BEFORE INSERT ON workflow_events WHEN NEW.action='client_followup_due' BEGIN SELECT RAISE(ABORT,'TEST_FOLLOWUP_DUE_TIME_FAILURE'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.projectDueClientFollowups(context.Background()); err == nil {
		t.Fatal("audit failure did not reject projection")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE aggregate_type='inbox_item'", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='client_followup_due'", 0)
	if err := store.DB.Exec("DROP TRIGGER fail_followup_due_time_audit").Error; err != nil {
		t.Fatal(err)
	}
	for scan := 0; scan < 2; scan++ {
		if err := service.projectDueClientFollowups(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM inbox_items", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='client_followup_due'", 1)
}
