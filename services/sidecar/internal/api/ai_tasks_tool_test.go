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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIWorkspaceTasksRegisteredForWorkReadOnly(t *testing.T) {
	_, store, service, _, generation := aiActionTestFixture(t)
	var provider models.AIProvider
	if err := store.DB.First(&provider, "id = ?", generation.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	tool, exists := registry.Get("workspace_tasks")
	if !exists {
		t.Fatal("work must expose structured task filtering without actions or a saved session")
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"filters":{"priority":"P0","planned_from":"2026-09-18","planned_to":"2026-09-25"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]string{{"clients"}, {"knowledge_actions"}} {
		other, err := service.aiChatToolRegistry(generation.SessionID, true, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}, generation.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := other.Get("workspace_tasks"); exists {
			t.Fatalf("task discovery exposed without work: %v", scopes)
		}
	}
	denied := *tool.(*aiWorkspaceTool)
	denied.policy = harness.NewCapabilities("clients")
	if _, err := denied.Execute(context.Background(), []byte(`{}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("runtime scope recheck = %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIWorkspaceTasksExactSelectedIDsAreOneBoundedLiveRead(t *testing.T) {
	router, store, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_tasks"
	first := createTaskForTaskFacts(t, router, `{"title":"First task","description":"PRIVATE-FIRST-BODY","priority":"P0","due_date":"2026-09-25T12:30:00Z"}`)
	second := createTaskForTaskFacts(t, router, `{"title":"Second task","description":"PRIVATE-SECOND-BODY","priority":"P3","planned_date":"2026-09-22"}`)
	createTaskForTaskFacts(t, router, `{"title":"UNSELECTED-TASK","priority":"P1"}`)
	args := fmt.Sprintf(`{"task_ids":[%q,%q]}`, second.ID, first.ID)
	result, err := tool.Execute(context.Background(), []byte(args))
	if err != nil {
		t.Fatal(err)
	}
	var page aiTaskPageForTest
	if err := json.Unmarshal([]byte(result), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Total != 2 || page.HasMore || page.Next != nil || page.WindowLimited || page.AsOf == "" {
		t.Fatalf("exact selection page = %s", result)
	}
	if page.Items[0].ID != second.ID || page.Items[1].ID != first.ID || page.Items[0].Priority != "P3" || page.Items[1].Priority != "P0" || page.Items[0].Version != second.Version || page.Items[1].Version != first.Version || page.Items[0].PlannedDate == nil || page.Items[1].DueDate == nil {
		t.Fatalf("selected task order or facts = %s", result)
	}
	for _, forbidden := range []string{"PRIVATE-", "UNSELECTED-TASK", "description", "completion_criteria", "client_id", "project_name"} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("exact selection leaked %s: %s", forbidden, result)
		}
	}
	if err := store.DB.Model(&models.Task{}).Where("id = ?", first.ID).Updates(map[string]any{"priority": "P1", "version": first.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := tool.Execute(context.Background(), []byte(args))
	if err != nil || !strings.Contains(updated, `"priority":"P1"`) || !strings.Contains(updated, fmt.Sprintf(`"version":%d`, first.Version+1)) {
		t.Fatalf("selected facts must be live, got %s, %v", updated, err)
	}
	missing := fmt.Sprintf(`{"task_ids":[%q,%q]}`, second.ID, uuid.NewString())
	if result, err := tool.Execute(context.Background(), []byte(missing)); err == nil {
		t.Fatalf("missing selected Task must fail as a whole: %s", result)
	}
	for _, bad := range []string{
		`{"task_ids":[]}`, `{"task_ids":null}`, `{"task_ids":["bad"]}`,
		fmt.Sprintf(`{"task_ids":[%q,%q]}`, first.ID, first.ID),
		fmt.Sprintf(`{"task_ids":[%q],"filters":{}}`, first.ID),
		fmt.Sprintf(`{"task_ids":[%q],"limit":1}`, first.ID),
		fmt.Sprintf(`{"task_ids":[%q],"offset":0}`, first.ID),
		fmt.Sprintf(`{"task_ids":[%q],"task_ids":[%q]}`, first.ID, second.ID),
		`{"task_ids":[` + strings.Repeat(fmt.Sprintf(`%q,`, first.ID), 20) + fmt.Sprintf(`%q]}`, second.ID),
	} {
		if result, err := tool.Execute(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("invalid exact selection accepted: %s => %s", bad, result)
		}
	}
}

func TestAIWorkspaceTasksExactSelectionKeepsAllTwentyWithinResultBudget(t *testing.T) {
	_, store, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_tasks"
	ids := make([]string, 20)
	rows := make([]models.Task, 20)
	for i := range rows {
		ids[i] = uuid.NewString()
		rows[i] = models.Task{ID: ids[i], Title: strings.Repeat("<", 200), Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	}
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"task_ids": ids})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("20 selected Tasks must remain readable even with JSON-escaped labels: %v", err)
	}
	var page aiTaskPageForTest
	if err := json.Unmarshal([]byte(result), &page); err != nil || len(page.Items) != 20 || page.Total != 20 {
		t.Fatalf("exact read dropped Task rows: %d %s %v", len(page.Items), result, err)
	}
	for _, item := range page.Items {
		if len([]rune(item.Label)) != 101 || !strings.HasSuffix(item.Label, "…") {
			t.Fatalf("long selected label was not visibly bounded: %#v", item)
		}
	}
}

type aiTaskPageForTest struct {
	Items         []aiTaskListItem `json:"items"`
	Total         int64            `json:"total_items"`
	HasMore       bool             `json:"has_more"`
	Next          *int             `json:"next_offset"`
	WindowLimited bool             `json:"window_limited"`
	AsOf          string           `json:"as_of"`
}

func TestAIWorkspaceTasksMatchesNativeFiltersAndFindsTargetsBeyondSearchPage(t *testing.T) {
	router, store, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_tasks"
	// work can filter a known client identity through Project, as native Tasks
	// and saved views already do; no Client data is read into the result.
	tool.policy = harness.NewCapabilities("work")
	stamp := "2026-09-18T12:00:00Z"
	secret := "PRIVATE-CUSTOMER-NOTE"
	client := models.Client{ID: uuid.NewString(), Name: "PRIVATE-CUSTOMER-NAME", Notes: &secret, Status: "active", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{ID: uuid.NewString(), Name: "PRIVATE-PROJECT", Description: "PRIVATE-PROJECT-BODY", ClientID: &client.ID, Status: "planning", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	tag1 := createTagForTaskFacts(t, router, "first", "#112233")
	tag2 := createTagForTaskFacts(t, router, "second", "#223344")
	target := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"needle 100%% target","description":"PRIVATE-TASK-BODY","completion_criteria":"PRIVATE-CRITERIA","kind":"review","priority":"P0","project_id":%q,"tag_ids":[%q,%q],"planned_date":"2026-09-18","due_date":"2026-09-18T11:59:59Z"}`, project.ID, tag1.ID, tag2.ID))
	second := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"second","priority":"P1","project_id":%q,"tag_ids":[%q],"planned_date":"2026-09-19","due_date":"2026-09-19T12:00:00Z"}`, project.ID, tag1.ID))
	child := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"child","parent_task_id":%q,"due_date":"2026-09-19T12:00:00.000000001Z"}`, target.ID))
	done := createTaskForTaskFacts(t, router, `{"title":"done overdue","due_date":"2026-09-17T12:00:00Z"}`)
	if err := store.DB.Model(&done).Updates(map[string]any{"status": "done", "completed_at": stamp}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		noise := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"noise %02d","priority":"P3"}`, i))
		if err := store.DB.Model(&noise).Update("updated_at", "2026-09-19T12:00:00Z").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.DB.Model(&models.Task{}).Where("id IN ?", []string{target.ID, second.ID, child.ID, done.ID}).Update("updated_at", stamp).Error; err != nil {
		t.Fatal(err)
	}
	search := tool
	search.name = "workspace_search"
	firstSearch, err := search.Execute(context.Background(), []byte(`{"type":"task","limit":20}`))
	if err != nil || strings.Contains(firstSearch, target.ID) {
		t.Fatalf("fixture must put desired task beyond initial search page: %s %v", firstSearch, err)
	}
	tests := []struct {
		name    string
		filters map[string]any
		wantIDs []string
	}{
		{"combined", map[string]any{"q": "needle", "status": "active", "priority": "P0", "kind": "review", "project_id": project.ID, "client_id": client.ID, "tag_ids": []string{tag1.ID, tag2.ID}, "planned_from": "2026-09-18", "planned_to": "2026-09-19", "due_from": "2026-09-18", "due_to": "2026-09-19", "sort": "due_date"}, []string{target.ID}},
		{"client", map[string]any{"client_id": client.ID, "sort": "priority"}, []string{target.ID, second.ID}},
		{"all tags", map[string]any{"tag_ids": []string{tag1.ID, tag2.ID}}, []string{target.ID}},
		{"exact planned day", map[string]any{"planned_date": "2026-09-19"}, []string{second.ID}},
		{"overdue excludes terminal", map[string]any{"due_state": "overdue"}, []string{target.ID}},
		{"due soon inclusive boundary", map[string]any{"due_state": "due_soon", "status": "active"}, []string{second.ID}},
		{"parent", map[string]any{"parent_task_id": target.ID}, []string{child.ID}},
		{"parent and root are intersected", map[string]any{"parent_task_id": target.ID, "root_only": true}, []string{}},
		{"planned state", map[string]any{"planned_state": "scheduled", "sort": "-planned_date"}, []string{second.ID, target.ID}},
		{"unscheduled roots", map[string]any{"planned_state": "unscheduled", "root_only": true, "q": "child"}, []string{}},
		{"literal percent", map[string]any{"q": "%"}, []string{target.ID}},
		{"description search stays local", map[string]any{"q": "PRIVATE-TASK-BODY"}, []string{target.ID}},
		{"missing client", map[string]any{"client_id": uuid.NewString()}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"filters": tt.filters, "limit": 20})
			encoded, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatal(err)
			}
			var got aiTaskPageForTest
			if err := json.Unmarshal([]byte(encoded), &got); err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, item := range got.Items {
				ids = append(ids, item.ID)
				if item.Version < 1 || item.Route != "/tasks/"+item.ID || item.Status == "" || item.Priority == "" || item.Kind == "" {
					t.Fatalf("incomplete factual task metadata: %#v", item)
				}
			}
			if !reflect.DeepEqual(ids, tt.wantIDs) || got.Total != int64(len(ids)) || got.HasMore || got.Next != nil || got.WindowLimited || got.AsOf != stamp {
				t.Fatalf("task discovery = %s; want %v", encoded, tt.wantIDs)
			}
			for _, forbidden := range []string{"PRIVATE-", "description", "completion_criteria", "project_name", "client_id", "content_text", "blocked_reason", "tag_names"} {
				if strings.Contains(encoded, forbidden) {
					t.Fatalf("task query leaked %s: %s", forbidden, encoded)
				}
			}
			query := url.Values{"page_size": {"20"}}
			for key, value := range tt.filters {
				if tags, ok := value.([]string); ok {
					query["tag_id"] = tags
				} else {
					query.Set(key, fmt.Sprint(value))
				}
			}
			response := performRequest(router, http.MethodGet, "/api/v1/tasks?"+query.Encode(), nil, nil)
			if response.Code != 200 {
				t.Fatalf("native list %d: %s", response.Code, response.Body.String())
			}
			var native struct {
				Data []models.Task `json:"data"`
				Meta pageMeta      `json:"meta"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &native); err != nil {
				t.Fatal(err)
			}
			if native.Meta.Total != got.Total || len(native.Data) != len(got.Items) {
				t.Fatalf("native and tool counts differ: %#v / %#v", native.Meta, got)
			}
			for i, task := range native.Data {
				if task.ID != got.Items[i].ID || !reflect.DeepEqual(task.PlannedDate, got.Items[i].PlannedDate) || !reflect.DeepEqual(task.DueDate, got.Items[i].DueDate) {
					t.Fatalf("native and tool ordering/dates differ: %#v / %#v", task, got.Items[i])
				}
			}
		})
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 29)
}

func TestAIWorkspaceTasksHarnessLoopAndConsentDoesNotCarry(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	target := createTaskForTaskFacts(t, router, `{"title":"today important","priority":"P0","planned_date":"2026-09-18","description":"PRIVATE-DESCRIPTION","completion_criteria":"PRIVATE-CRITERIA"}`)
	createTaskForTaskFacts(t, router, `{"title":"another task","priority":"P1","planned_date":"2026-09-18"}`)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		foundTool, foundGuide := false, false
		for _, definition := range payload.Tools {
			function, _ := definition["function"].(map[string]any)
			if function["name"] == "workspace_guide" {
				foundGuide = true
			}
			if function["name"] == "workspace_tasks" {
				foundTool = true
				params, _ := function["parameters"].(map[string]any)
				properties, _ := params["properties"].(map[string]any)
				filters, _ := properties["filters"].(map[string]any)
				filterProperties, _ := filters["properties"].(map[string]any)
				if properties["task_ids"] == nil || filterProperties["priority"] == nil || filterProperties["planned_date"] == nil || filterProperties["due_state"] == nil || filters["additionalProperties"] != false {
					t.Error("advertised task schema cannot express the structured request")
				}
			}
		}
		if foundTool != (call == 2 || call == 3) {
			t.Errorf("task capability request=%d registered=%v", call, foundTool)
		}
		if foundGuide != (call < 4) {
			t.Errorf("guide capability request=%d registered=%v", call, foundGuide)
		}
		if call == 1 || call == 2 {
			name, id, arguments := "workspace_guide", "discover-tasks", `{"topic":"tasks_projects"}`
			if call == 2 {
				foundGuideResult := false
				for _, message := range payload.Messages {
					if message["role"] != "tool" {
						continue
					}
					content, _ := message["content"].(string)
					var guide aiWorkspaceGuideResult
					if !strings.HasPrefix(content, "workspace_guide: ") || json.Unmarshal([]byte(strings.TrimPrefix(content, "workspace_guide: ")), &guide) != nil || guide.Topic != "tasks_projects" {
						t.Errorf("unexpected guide result: %q", content)
						continue
					}
					for _, name := range guide.AvailableTools {
						if name == "workspace_tasks" {
							foundGuideResult = true
						}
					}
				}
				if !foundGuideResult {
					t.Error("harness did not return the task discovery guide")
				}
				name, id, arguments = "workspace_tasks", "filter-tasks", `{"filters":{"priority":"P0","planned_date":"2026-09-18"},"limit":10}`
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if call == 3 {
			foundResult := false
			for _, message := range payload.Messages {
				if message["role"] != "tool" {
					continue
				}
				content, _ := message["content"].(string)
				if strings.HasPrefix(content, "workspace_guide: ") {
					continue
				}
				if !strings.HasPrefix(content, "workspace_tasks: ") {
					t.Fatalf("unexpected tool result identity: %q", content)
				}
				var page aiTaskPageForTest
				if err := json.Unmarshal([]byte(strings.TrimPrefix(content, "workspace_tasks: ")), &page); err != nil {
					t.Errorf("task result=%q parse=%v", content, err)
					continue
				}
				foundResult = len(page.Items) == 1 && page.Items[0].ID == target.ID && page.Total == 1
				if strings.Contains(content, "PRIVATE-") || strings.Contains(content, "description") {
					t.Error("structured query sent a task body to the model")
				}
			}
			if !foundResult {
				t.Error("harness did not return the exact filtered task result")
			}
		}
		streamMockAIDelta(w, `查询到 1 项 P0 任务，尚未修改任务。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "structured-task-query", upstream.URL+"/v1", "gpt-test")
	var current models.AIProvider
	if err := store.DB.First(&current, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "找出计划在 2026-09-18 的 P0 任务", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || !strings.Contains(response.Body.String(), `"tool_name":"workspace_tasks"`) {
		t.Fatalf("structured task chat = %d %s", response.Code, response.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	response = chatRequest(t, router, provider.ID, session.ID, "谢谢")
	if response.Code != 200 || calls.Load() != 4 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("ungranted next message = %d %s calls=%d", response.Code, response.Body.String(), calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_guide' AND status='succeeded'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name='workspace_tasks' AND status='succeeded'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=1", 1, target.ID)
}

func TestAIWorkspaceTasksStrictArgumentsAndBounds(t *testing.T) {
	_, _, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_tasks"
	for _, args := range []string{
		`null`, `[]`, `{"filters":null}`, `{"limit":null}`, `{"offset":null}`,
		`{"FILTERS":{"due_state":"invalid"}}`, `{"filters":{"DUE_STATE":"invalid"}}`,
		`{"FILTERS":{"priority":null}}`, `{"filters":{"DUE_STATE":null}}`,
		`{"filters":{"due_state":"overdue","DUE_STATE":"invalid"}}`,
		`{"filters":{"due_state":"overdue"},"FILTERS":{}}`,
		`{"filters":{"due_state":"invalid"},"filters":{}}`,
		`{"filters":{},"filters":{"due_state":"invalid"}}`,
		`{"filters":{"due_state":"overdue","due_state":"due_soon"}}`,
		`{"limit":1,"limit":20}`, `{"limit":1,"LIMIT":20}`, `{"offset":0,"offset":1}`,
		`{"filters":{"q":"old","q":"new"}}`, `{"filters":{"q":null,"q":"new"}}`,
		`{"filters":{"\u0044UE_STATE":"invalid"}}`, `{"filters":{},"\u0066ilters":{}}`,
		`{"type":"task"}`, `{"filters":{"description":"secret"}}`, `{"filters":{"priority":null}}`,
		`{"filters":{"priority":"P9"}}`, `{"filters":{"kind":"invalid"}}`, `{"filters":{"status":"invalid"}}`,
		`{"filters":{"project_id":"invalid"}}`, `{"filters":{"client_id":"invalid"}}`, `{"filters":{"parent_task_id":"invalid"}}`,
		`{"filters":{"tag_ids":["bad"]}}`, `{"filters":{"root_only":"true"}}`, `{"filters":{"planned_state":"bad"}}`,
		`{"filters":{"due_state":""}}`, `{"filters":{"due_state":"yesterday"}}`,
		`{"filters":{"due_state":"overdue","status":"todo"}}`, `{"filters":{"due_state":"due_soon","due_to":""}}`,
		`{"filters":{"planned_state":"unscheduled","planned_from":"2026-09-01"}}`,
		`{"filters":{"planned_date":"2026-09-18","planned_to":"2026-09-21"}}`,
		`{"filters":{"planned_date":"2026-09-31"}}`, `{"filters":{"due_from":"2026-09-18T00:00:00Z"}}`,
		`{"filters":{"planned_from":"2026-09-20","planned_to":"2026-09-01"}}`,
		`{"filters":{"due_from":"2026-09-20","due_to":"2026-09-01"}}`,
		`{"filters":{"sort":"title;drop table tasks"}}`, `{"limit":0}`, `{"limit":21}`, `{"offset":-1}`, `{"offset":1001}`,
		fmt.Sprintf(`{"filters":{"q":%q}}`, strings.Repeat("界", 201)),
		fmt.Sprintf(`{"filters":{"q":%q}}`, strings.Repeat("a", 8192)),
		`{"filters":{"tag_ids":[` + strings.Repeat(`"00000000-0000-4000-8000-000000000001",`, 20) + `"00000000-0000-4000-8000-000000000001"]}}`,
	} {
		if result, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("invalid args %s accepted: %s", args, result)
		}
	}
}

func TestAIWorkspaceTasksPaginationAndWindowCount(t *testing.T) {
	_, store, _, proposal, _ := aiActionTestFixture(t)
	tool := *proposal.(*aiWorkspaceTool)
	tool.name = "workspace_tasks"
	rows := make([]models.Task, 1003)
	for i := range rows {
		rows[i] = models.Task{ID: uuid.NewString(), Title: fmt.Sprintf("task %04d", i), Description: "PRIVATE-BODY", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	}
	if err := store.DB.CreateInBatches(rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		offset, limit, count int
		next                 *int
		windowLimited        bool
	}{{0, 2, 2, intPointer(2), false}, {2, 2, 2, intPointer(4), false}, {1000, 1, 1, nil, true}, {1000, 20, 3, nil, false}} {
		result, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"filters":{"sort":"title"},"limit":%d,"offset":%d}`, test.limit, test.offset)))
		if err != nil {
			t.Fatal(err)
		}
		var page aiTaskPageForTest
		if err := json.Unmarshal([]byte(result), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != test.count || page.Total != 1003 || !reflect.DeepEqual(page.Next, test.next) || page.WindowLimited != test.windowLimited || page.HasMore != (test.offset+test.count < 1003) {
			t.Fatalf("page = %s", result)
		}
		for i, item := range page.Items {
			if item.ID != rows[test.offset+i].ID {
				t.Fatalf("unstable pagination offset=%d index=%d: %#v", test.offset, i, item)
			}
		}
	}
}
