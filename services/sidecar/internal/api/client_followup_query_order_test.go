package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestClientFollowupQueryOrderPreservesNativeTimePrecisionAcrossPages(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 1, 0, time.UTC)
	router, store, _, clientID := followupDueTimeFixture(t, &now)
	var owned []models.ClientFollowup
	for _, at := range []string{
		"2026-08-29T12:00:00.1Z", "2026-08-29T12:00:00.000000001Z",
		"2026-08-29T12:00:00Z", "2026-08-29T12:00:00.01Z", "2026-08-29T12:00:00.1Z",
	} {
		owned = append(owned, createFollowupDueTime(t, router, store, clientID, at))
	}
	otherClient := createClientForTest(t, router, `{"name":"Other query-order client"}`, nil)
	other := createFollowupDueTime(t, router, store, otherClient.ID, "2026-08-29T12:00:00.005Z")
	all := append(slices.Clone(owned), other)
	for _, tc := range []struct {
		name, route string
		rows        []models.ClientFollowup
	}{
		{"global", "/api/v1/client-followups", all},
		{"client_detail", "/api/v1/clients/" + clientID + "/followups", owned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := slices.Clone(tc.rows)
			sort.Slice(want, func(i, j int) bool {
				left, _ := time.Parse(time.RFC3339Nano, want[i].ScheduledAt)
				right, _ := time.Parse(time.RFC3339Nano, want[j].ScheduledAt)
				if left.Equal(right) {
					return want[i].ID < want[j].ID
				}
				return left.Before(right)
			})
			seen := []string{}
			for page := 1; page <= (len(want)+1)/2; page++ {
				response := performRequest(router, http.MethodGet, fmt.Sprintf("%s?page=%d&page_size=2", tc.route, page), nil, nil)
				var envelope struct {
					Data []clientFollowupResponse `json:"data"`
					Meta clientFollowupPageMeta   `json:"meta"`
				}
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
					t.Fatalf("list page %d: %d %s", page, response.Code, response.Body.String())
				}
				if envelope.Meta.Total != int64(len(want)) || envelope.Meta.Page != page || envelope.Meta.PageSize != 2 || envelope.Meta.ServerNow != now.Format(time.RFC3339Nano) {
					t.Fatalf("metadata changed: %#v", envelope.Meta)
				}
				for _, row := range envelope.Data {
					index := len(seen)
					if index >= len(want) {
						t.Fatalf("page %d returned an unexpected extra row", page)
					}
					if row.ID != want[index].ID || row.ScheduledAt != want[index].ScheduledAt || row.Version != want[index].Version {
						t.Errorf("page %d index %d is %s at %s; want %s at %s", page, index, row.ID, row.ScheduledAt, want[index].ID, want[index].ScheduledAt)
					}
					seen = append(seen, row.ID)
				}
			}
			if len(seen) != len(want) {
				t.Fatalf("pagination lost items: %d want %d", len(seen), len(want))
			}
		})
	}
}
