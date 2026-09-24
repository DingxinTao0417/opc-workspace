package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAITodayScopeArgumentsAndRuntimeGuards(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id=?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{nil, {"clients"}, {"work"}} {
		var grant *aiWorkspaceGrant
		if scopes != nil {
			grant = &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
		}
		registry, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, grant)
		if err != nil {
			t.Fatal(err)
		}
		_, exists := registry.Get("workspace_today")
		if exists != (len(scopes) == 1 && scopes[0] == "work") {
			t.Fatalf("today availability scopes=%v exists=%v", scopes, exists)
		}
	}
	tool := &aiWorkspaceTool{api: service, name: "workspace_today", policy: harness.NewCapabilities("work"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	for _, args := range []string{
		`{}`, `{"timezone":null}`, `{"timezone":""}`, `{"timezone":"Local"}`,
		`{"timezone":"Nope/Nowhere"}`, `{"timezone":" UTC"}`, `{"timezone":"+08:00"}`,
		`{"timezone":"UTC","date":null}`, `{"timezone":"UTC","date":""}`,
		`{"timezone":"UTC","date":"2026-02-30"}`, `{"timezone":"UTC","date":"2026-9-18"}`,
		`{"timezone":"UTC","task_id":"private"}`, `{"timezone":"UTC","date":123}`,
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Errorf("accepted invalid args %s", args)
		}
	}
	tool.policy = harness.NewCapabilities("clients")
	if _, err := tool.Execute(context.Background(), []byte(`{"timezone":"UTC"}`)); err == nil {
		t.Fatal("client-only grant read Today")
	}
	tool.policy = harness.NewCapabilities("work")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, []byte(`{"timezone":"UTC"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	service.restorePending.Store(true)
	if _, err := tool.Execute(context.Background(), []byte(`{"timezone":"UTC"}`)); err == nil || !strings.Contains(err.Error(), "restore") {
		t.Fatalf("restore=%v", err)
	}
	service.restorePending.Store(false)
	if err := store.DB.Model(&models.AIProvider{}).Where("id=?", provider.ID).Updates(map[string]any{"config_version": provider.ConfigVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"timezone":"UTC"}`)); err == nil || !strings.Contains(err.Error(), "permission expired") {
		t.Fatalf("provider change=%v", err)
	}
}

func TestAITodayMatchesNativeAggregateWithoutPrivateRows(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	router, store, service, _, generation := aiActionTestFixture(t, now)
	tool := &aiWorkspaceTool{api: service, name: "workspace_today", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	date, due := "2026-03-08", "2026-03-07T01:00:00Z"
	for _, row := range []struct {
		status   string
		planned  *string
		due      *string
		estimate int
		actual   int
	}{
		{"todo", &date, nil, 30, 0},
		{"done", &date, nil, 20, 15},
		{"cancelled", &date, nil, 40, 10},
		{"todo", nil, &due, 0, 0},
	} {
		id := uuid.NewString()
		task := models.Task{ID: id, Title: "private-task-name", Description: "private-task-body", Kind: "work", Status: row.status, ReviewPolicy: "none", Priority: "P2", PlannedDate: row.planned, DueDate: row.due, EstimatedMinutes: &row.estimate, ActualMinutes: row.actual, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
		if row.status == "done" {
			completed := now.Format(time.RFC3339Nano)
			task.CompletedAt = &completed
		}
		if err := store.DB.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-08T09:30:00Z", 3600}}, "completed")
	seedCompletedFocusIntervals(t, store, uuid.NewString(), []focusIntervalFixture{{"2026-03-08T10:00:00Z", 600}}, "cancelled")
	before := focusInt64(t, store, "SELECT COUNT(*) FROM tasks")
	args := []byte(`{"date":"2026-03-08","timezone":"America/Los_Angeles"}`)
	output, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Date      string          `json:"date"`
		Timezone  string          `json:"timezone"`
		Tasks     taskStats       `json:"tasks"`
		Focus     focusStats      `json:"focus"`
		Semantics map[string]bool `json:"semantics"`
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodGet, "/api/v1/stats/today?date=2026-03-08&timezone=America%2FLos_Angeles", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("native Today %d: %s", response.Code, response.Body.String())
	}
	var native struct {
		Data todayStatsSnapshot `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &native); err != nil {
		t.Fatal(err)
	}
	if got.Date != native.Data.Date || got.Timezone != "America/Los_Angeles" || !reflect.DeepEqual(got.Tasks, native.Data.Tasks) || !reflect.DeepEqual(got.Focus, native.Data.Focus) {
		t.Fatalf("AI %s differs from native %s", output, response.Body.String())
	}
	if got.Tasks.Total != 2 || got.Tasks.Completed != 1 || got.Tasks.Remaining != 1 || got.Tasks.Overdue != 1 || got.Tasks.EstimatedMinutes != 50 || got.Tasks.ActualMinutes != 25 || got.Focus.Seconds != 3600 {
		t.Fatalf("wrong aggregate: %s", output)
	}
	if !got.Semantics["overdue_and_due_soon_across_all_active_tasks"] || got.Semantics["task_details_included"] || got.Semantics["local_cycle_included"] {
		t.Fatalf("unsafe semantics: %s", output)
	}
	if strings.Contains(output, "private-task") || focusInt64(t, store, "SELECT COUNT(*) FROM tasks") != before {
		t.Fatalf("leaked rows or changed Task facts: %s", output)
	}
}

func TestAITodayDefaultsToConfirmedLocalDate(t *testing.T) {
	_, _, service, _, generation := aiActionTestFixture(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	tool := &aiWorkspaceTool{api: service, name: "workspace_today", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	for _, test := range []struct{ zone, date string }{{"UTC", "2026-09-18"}, {"Pacific/Kiritimati", "2026-09-19"}, {"America/Los_Angeles", "2026-09-18"}} {
		args, _ := json.Marshal(map[string]string{"timezone": test.zone})
		output, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Date string `json:"date"`
		}
		if err := json.Unmarshal([]byte(output), &got); err != nil || got.Date != test.date {
			t.Fatalf("%s default=%s err=%v", test.zone, output, err)
		}
	}
}
