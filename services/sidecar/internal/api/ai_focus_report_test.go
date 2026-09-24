package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIFocusReportRequiresWorkAndExplicitRange(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
	_, store, service, _, generation := aiFocusClockFixture(t, clock)
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
		_, exists := registry.Get("workspace_focus_report")
		if exists != (len(scopes) == 1 && scopes[0] == "work") {
			t.Fatalf("report availability scopes=%v exists=%v", scopes, exists)
		}
	}
	read := &aiWorkspaceTool{api: service, name: "workspace_focus_report", policy: harness.NewCapabilities("work"), providerID: provider.ID, configVersion: provider.ConfigVersion}
	args := []byte(`{"view":"summary","date_from":"2026-09-12","date_to":"2026-09-18","timezone":"UTC"}`)
	text, err := read.Execute(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Totals   focusStats `json:"totals"`
		Timezone string     `json:"timezone"`
		Route    string     `json:"route"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil || result.Totals.Seconds != 0 || result.Timezone != "UTC" {
		t.Fatalf("empty report=%s err=%v", text, err)
	}
	if result.Route != "/focus?date_from=2026-09-12&date_to=2026-09-18&report=summary&timezone=UTC" {
		t.Fatalf("report lost its location: %s", result.Route)
	}
	read.policy = harness.NewCapabilities("clients")
	if _, err := read.Execute(context.Background(), args); err == nil {
		t.Fatal("client-only grant read Focus report")
	}
}

func TestAIFocusReportStrictArgumentsAndRuntimeGuards(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	tool := &aiWorkspaceTool{api: service, name: "workspace_focus_report", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	base := map[string]any{"view": "summary", "date_from": "2026-09-01", "date_to": "2026-09-18", "timezone": "UTC"}
	for _, key := range []string{"view", "date_from", "date_to", "timezone"} {
		input := make(map[string]any)
		for k, v := range base {
			if k != key {
				input[k] = v
			}
		}
		args, _ := json.Marshal(input)
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("accepted missing %s", key)
		}
	}
	for _, patch := range []map[string]any{
		{"view": "all"}, {"date_from": ""}, {"date_to": "2026-02-30"},
		{"date_from": "2026-09-19"}, {"date_from": "2026-01-01"},
		{"date_from": "0001-01-01", "date_to": "9999-12-31"},
		{"timezone": ""}, {"timezone": "Local"}, {"timezone": "Nope/Nowhere"},
		{"timezone": "../private"}, {"timezone": "+08:00"},
		{"project_id": ""}, {"project_id": "bad"}, {"project_id": "AAAAAAAA-0000-0000-0000-000000000000"},
		{"limit": 1}, {"offset": 0}, {"view": "days", "limit": 0}, {"view": "days", "limit": 21},
		{"view": "days", "offset": -1}, {"view": "days", "offset": 1001},
		{"task_id": uuid.NewString()}, {"status": "active"}, {"confirm": true},
	} {
		input := make(map[string]any)
		for k, v := range base {
			input[k] = v
		}
		for k, v := range patch {
			input[k] = v
		}
		args, _ := json.Marshal(input)
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	for _, key := range []string{"view", "date_from", "date_to", "timezone", "project_id", "limit", "offset"} {
		input := make(map[string]any)
		for k, v := range base {
			input[k] = v
		}
		input[key] = nil
		args, _ := json.Marshal(input)
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("accepted null %s", key)
		}
	}
	args, _ := json.Marshal(base)
	missing := strings.TrimSuffix(string(args), "}") + fmt.Sprintf(`,"project_id":%q}`, uuid.NewString())
	if _, err := tool.Execute(context.Background(), []byte(missing)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing project=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	service.restorePending.Store(true)
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "restore") {
		t.Fatalf("restore=%v", err)
	}
	service.restorePending.Store(false)
	if err := store.DB.Model(&models.AIProvider{}).Where("id=?", generation.ProviderID).Updates(map[string]any{"config_version": 2, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), args); err == nil || !strings.Contains(err.Error(), "permission expired") {
		t.Fatalf("provider change=%v", err)
	}
}

func TestAIFocusReportMatchesHumanAggregatesAndDoesNotWrite(t *testing.T) {
	clock := &focusTestClock{now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	router, store, service, _, generation := aiFocusClockFixture(t, clock)
	tool := &aiWorkspaceTool{api: service, name: "workspace_focus_report", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	projectID, taskID := uuid.NewString(), uuid.NewString()
	seedFocusProject(t, store, projectID, "Report project", "archived")
	seedFocusTask(t, store, taskID, "todo", 0)
	assignFocusTaskProject(t, store, taskID, projectID)
	if err := store.DB.Model(&models.Task{}).Where("id=?", taskID).Updates(map[string]any{"title": "private-task-name", "description": "private-task-body"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		tagID := uuid.NewString()
		if err := store.DB.Exec("INSERT INTO tags(id,name,color,created_at) VALUES (?,?,?,?)", tagID, name, "#123456", clock.Now().Format(time.RFC3339)).Error; err != nil {
			t.Fatal(err)
		}
		if err := store.DB.Exec("INSERT INTO task_tags(task_id,tag_id) VALUES (?,?)", taskID, tagID).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedTerminalFocusSession(t, store, uuid.NewString(), &taskID, "completed", "2026-03-08T07:50:00Z", "2026-03-08T08:10:00Z", 1200)
	// Spring clock skips 02:00; elapsed seconds still include both real halves.
	seedTerminalFocusSession(t, store, uuid.NewString(), nil, "completed", "2026-03-08T09:30:00Z", "2026-03-08T10:30:00Z", 3600)
	// Fall clock repeats 01:00, counted once per session and twice in seconds.
	seedTerminalFocusSession(t, store, uuid.NewString(), &taskID, "completed", "2026-11-01T08:30:00Z", "2026-11-01T09:30:00Z", 3600)
	for _, status := range []string{"cancelled", "interrupted"} {
		seedTerminalFocusSession(t, store, uuid.NewString(), nil, status, "2026-11-01T12:00:00Z", "2026-11-01T12:20:00Z", 1200)
	}
	active := createFocusForTest(t, router, nil, 300, "report-no-heartbeat").Session
	clock.Add(65 * time.Second)
	var before models.FocusSession
	if err := store.DB.First(&before, "id=?", active.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, window := range []struct {
		from, to string
		seconds  int64
	}{{"2026-03-07", "2026-03-10", 4800}, {"2026-11-01", "2026-11-02", 3600}} {
		query := url.Values{"date_from": {window.from}, "date_to": {window.to}, "timezone": {"America/Los_Angeles"}}
		r := performRequest(router, http.MethodGet, "/api/v1/stats/focus?"+query.Encode(), nil, nil)
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		native := decodeFocusPeriodStats(t, r.Body.Bytes())
		if native.Totals.Seconds != window.seconds {
			t.Fatalf("native total=%+v", native.Totals)
		}
		if window.from == "2026-03-07" && (native.Hours[1].Seconds != 1800 || native.Hours[2].Seconds != 0 || native.Hours[3].Seconds != 1800 || native.Days[0].Seconds != 600) {
			t.Fatalf("spring/midnight=%+v", native)
		}
		if window.from == "2026-11-01" && (native.Hours[1].Seconds != 3600 || native.Hours[1].Sessions != 1) {
			t.Fatalf("fall=%+v", native.Hours)
		}
		for view, want := range map[string]any{"days": native.Days, "projects": native.Projects, "tags": native.Tags, "hours": native.Hours, "heatmap": native.Heatmap} {
			all := []json.RawMessage{}
			for offset := 0; ; {
				args, _ := json.Marshal(map[string]any{"view": view, "date_from": window.from, "date_to": window.to, "timezone": "America/Los_Angeles", "limit": 20, "offset": offset})
				text, err := tool.Execute(context.Background(), args)
				if err != nil {
					t.Fatalf("%s: %v", view, err)
				}
				assertAIFocusReportRoute(t, text, view, window.from, window.to, "America/Los_Angeles", "")
				var got struct {
					Items     []json.RawMessage `json:"items"`
					Totals    focusStats        `json:"totals"`
					Next      *int              `json:"next_offset"`
					More      bool              `json:"has_more"`
					Limited   bool              `json:"window_limited"`
					Semantics map[string]any    `json:"semantics"`
				}
				if err := json.Unmarshal([]byte(text), &got); err != nil {
					t.Fatal(err)
				}
				if got.Totals != native.Totals || len(got.Items) > 20 || len(text) > 24<<10 || got.Limited || got.Semantics["completed_only"] != true || got.Semantics["tag_seconds_additive"] != false {
					t.Fatalf("%s: %s", view, text)
				}
				for _, private := range []string{"private-task", "description", "last_heartbeat_at", "client_id", "actor_id", "content"} {
					if strings.Contains(text, private) {
						t.Fatalf("leaked %s in %s", private, text)
					}
				}
				all = append(all, got.Items...)
				if got.Next == nil {
					if got.More {
						t.Fatal("page lost continuation")
					}
					break
				}
				if !got.More || *got.Next != offset+20 {
					t.Fatal("unstable page")
				}
				offset = *got.Next
			}
			actualJSON, _ := json.Marshal(all)
			wantJSON, _ := json.Marshal(want)
			if string(actualJSON) != string(wantJSON) {
				t.Fatalf("%s differs from human report: %s != %s", view, actualJSON, wantJSON)
			}
		}
	}
	var after models.FocusSession
	if err := store.DB.First(&after, "id=?", active.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("report changed active state/heartbeat")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_focus_totals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action LIKE 'focus_%'", 1)
	// Reclassification is intentional: project/tag facts are current, not history.
	args := fmt.Sprintf(`{"view":"tags","date_from":"2026-11-01","date_to":"2026-11-01","timezone":"America/Los_Angeles","project_id":%q}`, projectID)
	result, err := tool.Execute(context.Background(), []byte(args))
	if err != nil || !strings.Contains(result, `"total_items":2`) {
		t.Fatalf("tag result=%s %v", result, err)
	}
	assertAIFocusReportRoute(t, result, "tags", "2026-11-01", "2026-11-01", "America/Los_Angeles", projectID)
	if err := store.DB.Exec("DELETE FROM task_tags WHERE task_id=?", taskID).Error; err != nil {
		t.Fatal(err)
	}
	result, err = tool.Execute(context.Background(), []byte(args))
	if err != nil || !strings.Contains(result, `"tag_id":null`) || !strings.Contains(result, `"total_items":1`) {
		t.Fatalf("current tags=%s %v", result, err)
	}
	if err := store.DB.Exec("UPDATE tasks SET project_id=NULL WHERE id=?", taskID).Error; err != nil {
		t.Fatal(err)
	}
	result, err = tool.Execute(context.Background(), []byte(args))
	if err != nil || !strings.Contains(result, `"seconds":0`) || !strings.Contains(result, `"items":[]`) {
		t.Fatalf("current project=%s %v", result, err)
	}
}

func assertAIFocusReportRoute(t *testing.T, result, view, from, to, timezone, project string) {
	t.Helper()
	var payload struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(payload.Route)
	if err != nil || parsed.Path != "/focus" || parsed.Host != "" || parsed.Fragment != "" {
		t.Fatalf("unsafe route %q: %v", payload.Route, err)
	}
	want := url.Values{"report": {view}, "date_from": {from}, "date_to": {to}, "timezone": {timezone}}
	if project != "" {
		want.Set("project_id", project)
	}
	if !reflect.DeepEqual(parsed.Query(), want) {
		t.Fatalf("report parameters lost: %v != %v", parsed.Query(), want)
	}
}

func TestAIFocusReportPagingEmptyWindowAndSafeStorageError(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	tool := &aiWorkspaceTool{api: service, name: "workspace_focus_report", policy: harness.NewCapabilities("work"), providerID: generation.ProviderID, configVersion: 1}
	for _, view := range []string{"summary", "days", "projects", "tags", "hours", "heatmap"} {
		args := fmt.Sprintf(`{"view":%q,"date_from":"2026-01-01","date_to":"2026-04-03","timezone":"UTC"}`, view)
		result, err := tool.Execute(context.Background(), []byte(args))
		if err != nil || !strings.Contains(result, `"seconds":0`) {
			t.Fatalf("%s=%s %v", view, result, err)
		}
		if view == "days" && (!strings.Contains(result, `"total_items":93`) || !strings.Contains(result, `"next_offset":10`)) {
			t.Fatal(result)
		}
		if view == "projects" && !strings.Contains(result, `"items":[]`) {
			t.Fatal(result)
		}
	}
	items := make([]int, 1021)
	result := map[string]any{}
	aiFocusReportPage(result, items, 20, 980, "test")
	if result["window_limited"] != false || *result["next_offset"].(*int) != 1000 {
		t.Fatal(result)
	}
	aiFocusReportPage(result, items, 20, 1000, "test")
	if result["window_limited"] != true || result["has_more"] != true || result["next_offset"].(*int) != nil || len(result["items"].([]int)) != 20 {
		t.Fatal(result)
	}
	aiFocusReportPage(result, []int{}, 20, 1000, "test")
	if result["window_limited"] != false || result["has_more"] != false {
		t.Fatal(result)
	}
	// Break only this isolated fixture: SQL/table details must not reach the model.
	if err := store.DB.Exec("ALTER TABLE focus_session_intervals RENAME TO report_test_intervals").Error; err != nil {
		t.Fatal(err)
	}
	_, err := tool.Execute(context.Background(), []byte(`{"view":"summary","date_from":"2026-09-18","date_to":"2026-09-18","timezone":"UTC"}`))
	if err == nil || err.Error() != "workspace storage unavailable" {
		t.Fatalf("storage error leaked: %v", err)
	}
}

func TestAIFocusReportHarnessToolResultAndPermissionIsolation(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	seedTerminalFocusSession(t, store, uuid.NewString(), nil, "completed", "2026-09-18T10:00:00Z", "2026-09-18T10:05:00Z", 300)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		definitions, _ := json.Marshal(payload.Tools)
		if calls >= 2 && calls <= 3 && !strings.Contains(string(definitions), `"name":"workspace_focus_report"`) {
			t.Error("report tool not available")
		}
		if strings.Contains(string(definitions), "workspace_propose") {
			t.Error("read grant gained write proposals")
		}
		if calls == 1 {
			if !strings.Contains(string(definitions), `"name":"workspace_guide"`) || strings.Contains(string(definitions), `"name":"workspace_focus_report"`) {
				t.Error("initial focus catalog must expose discovery before the report helper")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "focus-guide", "type": "function", "function": map[string]any{"name": "workspace_guide", "arguments": `{"topic":"focus"}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 2 {
			guideFound := false
			for _, message := range payload.Messages {
				content, _ := message["content"].(string)
				if message["role"] == "tool" && strings.HasPrefix(content, "workspace_guide: ") {
					guideFound = true
				}
			}
			if !guideFound {
				t.Error("accepted focus guide result was not returned to the model")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			args := `{"view":"summary","date_from":"2026-09-18","date_to":"2026-09-18","timezone":"UTC"}`
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "focus-report", "type": "function", "function": map[string]any{"name": "workspace_focus_report", "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 {
			found := false
			for _, message := range payload.Messages {
				if message["role"] == "tool" {
					content, _ := message["content"].(string)
					if strings.Contains(content, `"seconds":300`) && strings.Contains(content, `"completed_only":true`) {
						assertAIFocusReportRoute(t, strings.TrimPrefix(content, "workspace_focus_report: "), "summary", "2026-09-18", "2026-09-18", "UTC", "")
						found = true
					}
				}
			}
			if !found {
				t.Error("authoritative report not returned to model")
			}
		}
		if calls == 4 && aiHasProtectedWorkspaceTool(definitions) {
			t.Error("grant carried over")
		}
		streamMockAIDelta(w, `该日期范围已完成专注 5 分钟，可在[专注报告](/focus?date_from=2026-09-18&date_to=2026-09-18&report=summary&timezone=UTC)查看。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "report-loop", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "统计 2026-09-18 UTC 的专注", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work"}}})
	r := performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatalf("chat=%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "谢谢"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatalf("followup=%d %s calls=%d", r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action LIKE 'focus_%'", 0)
}
