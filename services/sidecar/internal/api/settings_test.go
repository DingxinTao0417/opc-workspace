package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func newSettingsTestAPI(t *testing.T) (*gin.Engine, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "settings-api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0),
		Now: func() time.Time {
			return time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router.Engine, store
}

func decodeSettingsResponseForTest(t *testing.T, body []byte) settingsResponse {
	t.Helper()
	var response struct {
		Data settingsResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode settings response: %v\n%s", err, body)
	}
	return response.Data
}

func settingItemForTest(t *testing.T, response settingsResponse, key string) settingResponse {
	t.Helper()
	for _, item := range response.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("settings response has no %q item: %#v", key, response.Items)
	return settingResponse{}
}

func TestSettingsGetReturnsUnstoredServiceDefaults(t *testing.T) {
	router, store := newSettingsTestAPI(t)
	recorder := performRequest(router, http.MethodGet, "/api/v1/settings", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d: %s", recorder.Code, recorder.Body.String())
	}
	response := decodeSettingsResponseForTest(t, recorder.Body.Bytes())
	if response.SchemaVersion != 1 || len(response.Items) != 5 {
		t.Fatalf("settings response = %#v", response)
	}
	wantKeys := []string{"workspace", "general", "appearance", "focus", "storage"}
	for index, item := range response.Items {
		if item.Key != wantKeys[index] || item.SchemaVersion != 1 || item.Stored || item.Version != 0 || item.UpdatedByActorID != nil || item.UpdatedAt != nil {
			t.Fatalf("default item[%d] = %#v", index, item)
		}
	}
	if string(settingItemForTest(t, response, "workspace").Value) != `{"display_name":"opc-workspace","avatar_ref":null}` {
		t.Fatalf("workspace default = %s", settingItemForTest(t, response, "workspace").Value)
	}
	if string(settingItemForTest(t, response, "appearance").Value) != `{"theme":"dark"}` {
		t.Fatalf("appearance default = %s", settingItemForTest(t, response, "appearance").Value)
	}
	if string(settingItemForTest(t, response, "storage").Value) != `{"low_space_threshold_gib":1}` {
		t.Fatalf("storage default = %s", settingItemForTest(t, response, "storage").Value)
	}
	var count int64
	if err := store.DB.Table("app_settings").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("stored setting count = %d, err = %v", count, err)
	}
}

func TestSettingsPatchCreatesNormalizesAndAuditsWithoutValues(t *testing.T) {
	router, store := newSettingsTestAPI(t)
	body := []byte(`{"updates":[` +
		`{"key":"workspace","expected_version":0,"value":{"display_name":"  My   Workspace  ","avatar_ref":null}},` +
		`{"key":"general","expected_version":0,"value":{"default_route":"projects","show_right_overview":false,"reduce_motion":true}},` +
		`{"key":"storage","expected_version":0,"value":{"low_space_threshold_gib":5}}` +
		`]}`)
	recorder := performRequest(router, http.MethodPatch, "/api/v1/settings", body, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PATCH settings status = %d: %s", recorder.Code, recorder.Body.String())
	}
	response := decodeSettingsResponseForTest(t, recorder.Body.Bytes())
	workspace := settingItemForTest(t, response, "workspace")
	if !workspace.Stored || workspace.Version != 1 || workspace.UpdatedByActorID == nil || *workspace.UpdatedByActorID != models.BuiltinOwnerActorID {
		t.Fatalf("workspace response = %#v", workspace)
	}
	if string(workspace.Value) != `{"display_name":"My Workspace","avatar_ref":null}` {
		t.Fatalf("normalized workspace = %s", workspace.Value)
	}
	general := settingItemForTest(t, response, "general")
	if !general.Stored || general.Version != 1 || string(general.Value) != `{"default_route":"projects","show_right_overview":false,"reduce_motion":true}` {
		t.Fatalf("general response = %#v", general)
	}
	storage := settingItemForTest(t, response, "storage")
	if !storage.Stored || storage.Version != 1 || string(storage.Value) != `{"low_space_threshold_gib":5}` {
		t.Fatalf("storage response = %#v", storage)
	}
	var settingCount int64
	if err := store.DB.Table("app_settings").Count(&settingCount).Error; err != nil || settingCount != 3 {
		t.Fatalf("stored setting count = %d, err = %v", settingCount, err)
	}
	var events []models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = ?", "setting").Order("aggregate_id ASC").Find(&events).Error; err != nil {
		t.Fatalf("load setting events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("setting event count = %d, want 3", len(events))
	}
	for _, event := range events {
		if event.Action != "settings_updated" || event.ActorID == nil || *event.ActorID != models.BuiltinOwnerActorID || event.PreviousJSON == nil || event.CurrentJSON == nil {
			t.Fatalf("setting event = %#v", event)
		}
		joined := *event.PreviousJSON + *event.CurrentJSON
		if strings.Contains(joined, "My Workspace") || strings.Contains(joined, "projects") || strings.Contains(joined, "avatar") {
			t.Fatalf("setting event leaked values: %s", joined)
		}
	}

	get := performRequest(router, http.MethodGet, "/api/v1/settings", nil, nil)
	if get.Code != http.StatusOK || string(settingItemForTest(t, decodeSettingsResponseForTest(t, get.Body.Bytes()), "workspace").Value) != string(workspace.Value) {
		t.Fatalf("persisted GET status=%d body=%s", get.Code, get.Body.String())
	}
}

func TestSettingsPatchUpdatesWithOptimisticVersion(t *testing.T) {
	router, _ := newSettingsTestAPI(t)
	create := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[{"key":"appearance","expected_version":0,"value":{"theme":"dark"}}]}`), nil)
	if create.Code != http.StatusOK {
		t.Fatalf("create appearance = %d: %s", create.Code, create.Body.String())
	}
	update := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[{"key":"appearance","expected_version":1,"value":{"theme":"light"}}]}`), nil)
	if update.Code != http.StatusOK {
		t.Fatalf("update appearance = %d: %s", update.Code, update.Body.String())
	}
	item := settingItemForTest(t, decodeSettingsResponseForTest(t, update.Body.Bytes()), "appearance")
	if item.Version != 2 || string(item.Value) != `{"theme":"light"}` {
		t.Fatalf("updated appearance = %#v", item)
	}
	conflict := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[{"key":"appearance","expected_version":1,"value":{"theme":"dark"}}]}`), nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale PATCH status = %d: %s", conflict.Code, conflict.Body.String())
	}
	var apiError errorResponse
	if err := json.Unmarshal(conflict.Body.Bytes(), &apiError); err != nil || apiError.Code != "SETTINGS_VERSION_CONFLICT" || apiError.RequestID == "" {
		t.Fatalf("conflict error = %#v, decode err = %v", apiError, err)
	}
}

func TestSettingsPatchRollsBackWholeBatchOnConflict(t *testing.T) {
	router, store := newSettingsTestAPI(t)
	create := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[{"key":"focus","expected_version":0,"value":{"focus_minutes":50,"break_minutes":5,"cycles":4,"auto_start_break":true,"auto_start_focus":false,"sound_enabled":true}}]}`), nil)
	if create.Code != http.StatusOK {
		t.Fatalf("create focus = %d: %s", create.Code, create.Body.String())
	}
	batch := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[`+
		`{"key":"focus","expected_version":1,"value":{"focus_minutes":25,"break_minutes":5,"cycles":4,"auto_start_break":true,"auto_start_focus":false,"sound_enabled":true}},`+
		`{"key":"general","expected_version":99,"value":{"default_route":"today","show_right_overview":true,"reduce_motion":false}}`+
		`]}`), nil)
	if batch.Code != http.StatusConflict {
		t.Fatalf("batch conflict status = %d: %s", batch.Code, batch.Body.String())
	}
	var focus models.AppSetting
	if err := store.DB.First(&focus, "key = ?", "focus").Error; err != nil {
		t.Fatalf("load focus after rollback: %v", err)
	}
	if focus.Version != 1 || !strings.Contains(focus.ValueJSON, `"focus_minutes":50`) {
		t.Fatalf("focus changed despite rollback = %#v", focus)
	}
	var generalCount, eventCount int64
	if err := store.DB.Table("app_settings").Where("key = ?", "general").Count(&generalCount).Error; err != nil || generalCount != 0 {
		t.Fatalf("general count = %d, err = %v", generalCount, err)
	}
	if err := store.DB.Table("workflow_events").Where("aggregate_type = ?", "setting").Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("setting event count = %d, err = %v", eventCount, err)
	}
}

func TestSettingsPatchRejectsUnknownSensitiveAndInvalidValues(t *testing.T) {
	router, _ := newSettingsTestAPI(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "empty batch", body: `{"updates":[]}`},
		{name: "unknown key", body: `{"updates":[{"key":"tokens","expected_version":0,"value":{}}]}`},
		{name: "duplicate key", body: `{"updates":[{"key":"appearance","expected_version":0,"value":{"theme":"dark"}},{"key":"appearance","expected_version":0,"value":{"theme":"light"}}]}`},
		{name: "missing expected version", body: `{"updates":[{"key":"appearance","value":{"theme":"dark"}}]}`},
		{name: "missing required boolean", body: `{"updates":[{"key":"general","expected_version":0,"value":{"default_route":"today","show_right_overview":true}}]}`},
		{name: "null required boolean", body: `{"updates":[{"key":"general","expected_version":0,"value":{"default_route":"today","show_right_overview":null,"reduce_motion":false}}]}`},
		{name: "unknown sensitive field", body: `{"updates":[{"key":"appearance","expected_version":0,"value":{"theme":"dark","token":"secret"}}]}`},
		{name: "data url avatar", body: `{"updates":[{"key":"workspace","expected_version":0,"value":{"display_name":"opc","avatar_ref":"data:image/png;base64,secret"}}]}`},
		{name: "invalid focus bound", body: `{"updates":[{"key":"focus","expected_version":0,"value":{"focus_minutes":3,"break_minutes":5,"cycles":4,"auto_start_break":true,"auto_start_focus":false,"sound_enabled":true}}]}`},
		{name: "invalid storage threshold", body: `{"updates":[{"key":"storage","expected_version":0,"value":{"low_space_threshold_gib":0}}]}`},
		{name: "unknown top field", body: `{"updates":[{"key":"appearance","expected_version":0,"value":{"theme":"dark"}}],"token":"secret"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(test.body), nil)
			if recorder.Code != http.StatusUnprocessableEntity && recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
