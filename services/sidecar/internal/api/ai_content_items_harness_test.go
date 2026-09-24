package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIContentItemsHarnessReadOnlyDiscovery(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			created := performRequest(router, http.MethodPost, "/api/v1/content-items", []byte(`{"title":"Metadata-only content","platform":"website","notes":"PRIVATE CONTENT NOTES","external_link":"https://private.invalid/content-token"}`), nil)
			if created.Code != http.StatusCreated {
				t.Fatal(created.Body.String())
			}
			item := decodeContentItemResponse(t, created.Body.Bytes())
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if strings.Contains(string(raw), "PRIVATE CONTENT NOTES") || strings.Contains(string(raw), "private.invalid/content-token") {
					t.Error("list metadata leaked notes or link")
				}
				if aiBudgetHasTool(payload, "workspace_propose") {
					t.Error("work-only grant exposed writes")
				}
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_content_items") {
						t.Error("content helper must initially be deferred")
					}
					writeAIBudgetToolTurn(w, protocol, "content-read-1", "workspace_guide", `{"topic":"roadmap_content"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_content_items") {
						t.Error("roadmap_content guide did not expose content query")
					}
					writeAIBudgetToolTurn(w, protocol, "content-read-2", "workspace_content_items", `{"view":"list","filters":{"platform":"website"}}`)
				case 3:
					var result struct {
						Total int `json:"total_items"`
						Items []struct {
							ID      string `json:"id"`
							Version int64  `json:"version"`
						} `json:"items"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "content-read-2", "workspace_content_items")
					if json.Unmarshal([]byte(content), &result) != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != item.ID || result.Items[0].Version != item.Version {
						t.Errorf("content metadata not discovered: %s", content)
					}
					writeAIBudgetTextTurn(w, protocol, `已读取条目元数据，没有修改或发布。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected call %d", call)
					writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "只读查看网站内容排期列表。", "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 3 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("read-only chat=%d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=? AND status='draft' AND published_at IS NULL", 1, item.ID, item.Version)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
		})
	}
}

type aiContentHarnessItem struct {
	ID                string  `json:"id"`
	Version           int64   `json:"version"`
	Status            string  `json:"status"`
	Platform          string  `json:"platform"`
	ScheduledAt       *string `json:"scheduled_at"`
	ScheduledTimezone *string `json:"scheduled_timezone"`
	ProjectID         *string `json:"project_id"`
	TaskTotal         int64   `json:"task_total"`
	TaskDone          int64   `json:"task_done"`
	RequiredTotal     int64   `json:"required_task_total"`
	RequiredDone      int64   `json:"required_task_done"`
}

// This uses 51 native Task/relationship creations and native completion for
// fixture setup. Only the real paged tool results supply model targets. The
// final incomplete Task is outside workspace_get's old 50-item detail window.
func TestAIContentItemsHarnessPagedPreparationAndApprovedSchedule(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			project := createProjectForTest(t, router, `{"name":"Editorial harness project"}`, nil)
			createContent := func(title, platform, at string) contentItemResponse {
				t.Helper()
				body, _ := json.Marshal(map[string]any{"title": title, "platform": platform, "project_id": project.ID, "status": "scheduled", "scheduled_at": at, "scheduled_timezone": "UTC", "notes": "PRIVATE CONTENT NOTES", "external_link": "https://private.invalid/content-token"})
				response := performRequest(router, http.MethodPost, "/api/v1/content-items", body, nil)
				if response.Code != http.StatusCreated {
					t.Fatalf("native content creation=%d %s", response.Code, response.Body.String())
				}
				return decodeContentItemResponse(t, response.Body.Bytes())
			}
			item := createContent("Main scheduled delivery", "website", "2026-09-22T12:00:00Z")
			linkTask := func(item contentItemResponse, taskID string) contentItemResponse {
				t.Helper()
				response := performRequest(router, http.MethodPost, "/api/v1/content-items/"+item.ID+"/tasks", []byte(fmt.Sprintf(`{"task_id":%q,"is_required":true}`, taskID)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, item.Version)})
				if response.Code != http.StatusCreated {
					t.Fatalf("native link=%d %s", response.Code, response.Body.String())
				}
				return decodeContentItemResponse(t, response.Body.Bytes())
			}
			for i := 1; i <= 51; i++ {
				task := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":"Preparation %02d","project_id":%q,"description":"PRIVATE CONTENT TASK DESCRIPTION","review_policy":"none"}`, i, project.ID))
				item = linkTask(item, task.ID)
			}
			var sorted []models.Task
			if err := store.DB.Table("content_item_tasks r").Select("t.*").Joins("JOIN tasks t ON t.id=r.task_id").Where("r.content_item_id=?", item.ID).Order("r.linked_at ASC,t.id ASC").Scan(&sorted).Error; err != nil || len(sorted) != 51 {
				t.Fatalf("native relation fixture len=%d err=%v", len(sorted), err)
			}
			for _, task := range sorted[:50] {
				response := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)})
				if response.Code != http.StatusOK {
					t.Fatalf("fixture complete=%d %s", response.Code, response.Body.String())
				}
			}
			targetOracle := sorted[50]
			// Both negative controls have a genuine incomplete required Task, so
			// platform/time filtering, not empty preparation, excludes them.
			linkTask(createContent("Other platform delivery", "newsletter", "2026-09-22T12:00:00Z"), targetOracle.ID)
			linkTask(createContent("Outside requested dates", "website", "2026-10-22T12:00:00Z"), targetOracle.ID)
			baseItemVersion := item.Version
			var observedItem aiContentHarnessItem
			var observedTaskID, taskProposalID, scheduleProposalID string
			var observedTaskVersion int64
			var nextOffset int
			seen := map[string]bool{}
			listArgs := func(taskState string) string {
				return fmt.Sprintf(`{"view":"list","filters":{"platform":"website","status":"scheduled","project_id":%q,"scheduled_from":"2026-09-22T00:00:00Z","scheduled_to":"2026-09-23T00:00:00Z","schedule_state":"scheduled","task_state":%q},"limit":20}`, project.ID, taskState)
			}
			readItem := func(got aiContentHarnessItem, wantDone int64, wantAt string) {
				if got.ID != item.ID || got.Status != "scheduled" || got.Platform != "website" || got.ProjectID == nil || *got.ProjectID != project.ID || got.ScheduledAt == nil || *got.ScheduledAt != wantAt || got.ScheduledTimezone == nil || *got.ScheduledTimezone != "UTC" || got.TaskTotal != 51 || got.TaskDone != wantDone || got.RequiredTotal != 51 || got.RequiredDone != wantDone {
					t.Errorf("content progress/identity mismatch: %+v", got)
				}
				observedItem = got
			}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				for _, secret := range []string{"PRIVATE CONTENT NOTES", "private.invalid/content-token", "PRIVATE CONTENT TASK DESCRIPTION"} {
					if strings.Contains(string(raw), secret) {
						t.Errorf("content query leaked %q", secret)
					}
				}
				emit := func(name, args string) {
					writeAIBudgetToolTurn(w, protocol, fmt.Sprintf("content-page-%d", call), name, args)
				}
				finish := func(text string) {
					writeAIBudgetTextTurn(w, protocol, text+`[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
				result := func(id int, name string) string {
					return aiPlanRetryHarnessResult(t, payload, protocol, fmt.Sprintf("content-page-%d", id), name)
				}
				readList := func(id, wantTotal int) {
					var output struct {
						Total int                    `json:"total_items"`
						Items []aiContentHarnessItem `json:"items"`
					}
					content := result(id, "workspace_content_items")
					if json.Unmarshal([]byte(content), &output) != nil || output.Total != wantTotal || len(output.Items) != wantTotal {
						t.Errorf("filtered content list=%s want=%d", content, wantTotal)
						return
					}
					if wantTotal == 1 {
						readItem(output.Items[0], 50, "2026-09-22T12:00:00Z")
					}
				}
				readTaskPage := func(id, wantCount int, wantDone int64, wantAt string, collect bool) {
					var output struct {
						Item  aiContentHarnessItem `json:"content_item"`
						Total int                  `json:"total_items"`
						Next  *int                 `json:"next_offset"`
						More  bool                 `json:"has_more"`
						Items []struct {
							ID       string `json:"id"`
							Version  int64  `json:"version"`
							Status   string `json:"status"`
							Required bool   `json:"is_required"`
						} `json:"items"`
					}
					content := result(id, "workspace_content_items")
					if json.Unmarshal([]byte(content), &output) != nil || output.Total != 51 || len(output.Items) != wantCount {
						t.Errorf("content task page=%s want=%d", content, wantCount)
						return
					}
					readItem(output.Item, wantDone, wantAt)
					if !collect {
						return
					}
					for _, task := range output.Items {
						if task.ID == "" || task.Version < 1 || !task.Required || seen[task.ID] {
							t.Errorf("invalid/duplicate paged Task: %+v", task)
						}
						seen[task.ID] = true
						if task.Status == "todo" {
							if len(seen) != 51 || task.ID != targetOracle.ID {
								t.Errorf("unfinished Task was not the actual 51st relation: %+v index=%d", task, len(seen))
							}
							observedTaskID, observedTaskVersion = task.ID, task.Version
						} else if task.Status != "done" {
							t.Errorf("unexpected fixture task status %q", task.Status)
						}
					}
					if len(seen) < 51 {
						if !output.More || output.Next == nil || *output.Next != len(seen) {
							t.Errorf("lost actual next page: %s", content)
						} else {
							nextOffset = *output.Next
						}
					} else if output.More || output.Next != nil {
						t.Errorf("last page falsely has more: %s", content)
					}
				}
				readProposal := func(id int) string {
					var output struct {
						ID string `json:"proposal_id"`
					}
					content := result(id, "workspace_propose")
					if json.Unmarshal([]byte(content), &output) != nil || output.ID == "" || !strings.Contains(content, "NOT executed") {
						t.Errorf("missing pending proposal: %s", content)
					}
					return output.ID
				}
				switch call {
				case 1, 10, 15:
					if aiBudgetHasTool(payload, "workspace_content_items") {
						t.Error("new generation inherited deferred catalog")
					}
					emit("workspace_guide", `{"topic":"roadmap_content"}`)
				case 2, 11, 16:
					if !aiBudgetHasTool(payload, "workspace_content_items") {
						t.Error("content guide did not expose query")
					}
					state := "required_incomplete"
					if call == 16 {
						state = "all"
					}
					emit("workspace_content_items", listArgs(state))
				case 3:
					readList(2, 1)
					emit("workspace_content_items", fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"all","required_only":true,"limit":20,"offset":0}`, observedItem.ID))
				case 4, 5:
					readTaskPage(call-1, 20, 50, "2026-09-22T12:00:00Z", true)
					emit("workspace_content_items", fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"all","required_only":true,"limit":20,"offset":%d}`, observedItem.ID, nextOffset))
				case 6:
					readTaskPage(5, 11, 50, "2026-09-22T12:00:00Z", true)
					if observedTaskID == "" {
						t.Error("paged query never reached incomplete Task")
						finish("无法核实任务。")
						return
					}
					emit("workspace_guide", `{"topic":"tasks_projects"}`)
				case 7:
					guide := result(6, "workspace_guide")
					if !strings.Contains(guide, `"topic":"tasks_projects"`) || !aiBudgetHasTool(payload, "workspace_tasks") || !aiBudgetHasTool(payload, "workspace_propose") {
						t.Errorf("task operation rules were not loaded before proposing completion: %s", guide)
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"task.complete","task_id":%q,"expected_version":%d,"changes":{}}`, observedTaskID, observedTaskVersion))
				case 8:
					if names := aiBudgetToolNames(payload); len(names) != 0 {
						t.Errorf("last-round finish must not regain tools: %v", names)
					}
					if !strings.Contains(aiBudgetSystemPrompt(payload), harness.BudgetHandoffPrompt) {
						t.Error("eighth turn did not receive the actual bounded-handoff instruction")
					}
					taskProposalID = readProposal(7)
					finish("已核对第51项准备任务及任务操作规则，完成建议等待你确认，不会发布内容。")
				case 9:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("ungranted message inherited %s", name)
						}
					}
					finish("本条没有工作台授权，未继续查询或修改。")
				case 12, 17:
					readList(call-1, 0)
					emit("workspace_content_items", fmt.Sprintf(`{"view":"tasks","content_item_id":%q,"task_state":"all","required_only":true,"limit":1}`, observedItem.ID))
				case 13:
					readTaskPage(12, 1, 51, "2026-09-22T12:00:00Z", false)
					if observedItem.Version != baseItemVersion {
						t.Error("Task progress must be fresh even when Content Item version did not change")
					}
					emit("workspace_propose", fmt.Sprintf(`{"action":"content_item.schedule","content_item_id":%q,"expected_version":%d,"changes":{"scheduled_at":"2026-10-01T12:00:00Z","scheduled_timezone":"UTC"}}`, observedItem.ID, observedItem.Version))
				case 14:
					scheduleProposalID = readProposal(13)
					finish("必需准备任务已全部完成。改到10月1日的本地排期建议待确认，未发布。")
				case 18:
					readTaskPage(17, 1, 51, "2026-10-01T12:00:00Z", false)
					if observedItem.Version != baseItemVersion+1 || aiBudgetHasTool(payload, "workspace_propose") {
						t.Error("fresh schedule read has wrong version or inherited write scope")
					}
					finish("原日期范围已不包含该条目，最新本地排期为10月1日，准备任务51/51完成；这不是发布事实。")
				default:
					t.Errorf("unexpected Provider call %d", call)
					finish("停止。")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			var sessionID string
			send := func(message string, scopes []string, wantCalls int) {
				t.Helper()
				request := map[string]any{"provider_id": provider.ID, "message": message}
				if sessionID != "" {
					request["session_id"] = sessionID
				}
				if scopes != nil {
					request["workspace"] = aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes}
				}
				body, _ := json.Marshal(request)
				response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || upstream.calls.Load() != int32(wantCalls) || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") || t.Failed() {
					t.Fatalf("chat calls=%d want=%d response=%d %s", upstream.calls.Load(), wantCalls, response.Code, response.Body.String())
				}
				if wantCalls == 8 {
					meta := decodeAIBudgetMeta(t, response.Body.String())
					replacement := decodeAIBudgetReplace(t, response.Body.String())
					if replacement.GenerationID != meta.GenerationID || !strings.HasPrefix(replacement.Text, aiBudgetHandoffNotice+"\n\n") || !strings.Contains(replacement.Text, "完成建议等待你确认") {
						t.Fatalf("bounded finish lost the fixed handoff/approval boundary: %+v", replacement)
					}
					generationRead := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+meta.GenerationID, nil, nil)
					var saved struct {
						Data aiBudgetGenerationForTest `json:"data"`
					}
					if generationRead.Code != http.StatusOK || json.Unmarshal(generationRead.Body.Bytes(), &saved) != nil || saved.Data.Status != "completed" || saved.Data.Content != replacement.Text {
						t.Fatalf("handoff did not persist the same successful pending-approval reply: %d %s", generationRead.Code, generationRead.Body.String())
					}
				}
				if sessionID == "" {
					var session models.AISession
					if err := store.DB.First(&session).Error; err != nil {
						t.Fatal(err)
					}
					sessionID = session.ID
				}
			}
			confirm := func(id string) {
				t.Helper()
				var proposal models.AIActionProposal
				if err := store.DB.First(&proposal, "id=?", id).Error; err != nil {
					t.Fatal(err)
				}
				for range 2 {
					response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+id+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
					if response.Code != http.StatusOK {
						t.Fatalf("human approval=%d %s", response.Code, response.Body.String())
					}
				}
			}
			send(fmt.Sprintf("查看项目 %s 在9月22日的网站排期和未完成必需准备任务；最后一项已经完成工作，先给我完成建议，不要发布。", project.ID), []string{"work", "actions"}, 8)
			if len(seen) != 51 || observedTaskID != targetOracle.ID || observedTaskVersion != targetOracle.Version {
				t.Fatal("proposal target was not read through all three actual pages")
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='done'", 50)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='todo' AND version=?", 1, observedTaskID, observedTaskVersion)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
			confirm(taskProposalID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE status='done'", 51)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND version=?", 1, item.ID, baseItemVersion)
			send("谢谢。", nil, 9)
			send("重新读取未完准备筛选和真实准备进度，然后建议改到10月1日中午UTC。", []string{"work", "actions"}, 14)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND scheduled_at='2026-09-22T12:00:00Z' AND version=?", 1, item.ID, baseItemVersion)
			confirm(scheduleProposalID)
			send("只读重新检查原日期范围及该条目最新准备进度。", []string{"work"}, 18)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE id=? AND scheduled_at='2026-10-01T12:00:00Z' AND version=? AND status='scheduled' AND published_at IS NULL", 1, item.ID, baseItemVersion+1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_items WHERE published_at IS NOT NULL OR status='published'", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='confirmed'", 2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 2)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM content_item_tasks WHERE content_item_id=?", 51, item.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM agent_runs", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE content LIKE '%PRIVATE CONTENT%' OR content LIKE '%private.invalid/content-token%'", 0)
		})
	}
}
