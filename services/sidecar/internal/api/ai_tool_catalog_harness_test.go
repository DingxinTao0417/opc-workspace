package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIToolCatalogProviderDiscoveryAndConfirmedDescription(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			const title = "目录发现后的任务"
			const description = "保留真实业务 description 字段"
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				// Inspect only actual protocol tool definitions, not names mentioned
				// in history or guide results. Tools remain discoverable, not grants.
				if call != 8 {
					for _, core := range []string{"workspace_guide", "workspace_plan", "workspace_search", "workspace_get"} {
						if !aiBudgetHasTool(payload, core) {
							t.Errorf("request %d missing core %s", call, core)
						}
					}
					if aiBudgetHasTool(payload, "workspace_propose") != (call >= 2 && call <= 6) {
						t.Errorf("request %d proposal schema was not scoped to an accepted domain guide", call)
					}
				}
				if aiBudgetHasTool(payload, "workspace_finance") || aiBudgetHasTool(payload, "workspace_agent_execution") {
					t.Errorf("request %d advertised unauthorized tool", call)
				}
				switch call {
				case 1, 7:
					if aiBudgetHasTool(payload, "workspace_tasks") || aiBudgetHasTool(payload, "workspace_focus") {
						t.Errorf("new generation inherited a domain selection: %v", aiBudgetToolNames(payload))
					}
					if call == 7 {
						writeAIBudgetTextTurn(w, protocol, `这是新的一轮。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
						return
					}
					writeAIBudgetToolTurn(w, protocol, "catalog-guide", "workspace_guide", `{"topic":"tasks_projects"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_tasks") || !aiBudgetHasTool(payload, "workspace_task_options") {
						t.Errorf("guide did not load task tools: %v", aiBudgetToolNames(payload))
					}
					writeAIBudgetToolTurn(w, protocol, "catalog-tasks", "workspace_tasks", `{"filters":{"status":"active"},"limit":1}`)
				case 3:
					if !strings.Contains(string(raw), "total_items") {
						t.Error("real task query evidence missing")
					}
					writeAIBudgetToolTurn(w, protocol, "catalog-propose", "workspace_propose", fmt.Sprintf(`{"action":"task.create","changes":{"title":%q,"description":%q}}`, title, description))
				case 4:
					if !strings.Contains(string(raw), "NOT executed") {
						t.Error("proposal did not return pending-confirmation evidence")
					}
					writeAIBudgetToolTurn(w, protocol, "catalog-focus", "workspace_guide", `{"topic":"focus"}`)
				case 5, 6:
					if !aiBudgetHasTool(payload, "workspace_focus") || !aiBudgetHasTool(payload, "workspace_focus_report") || aiBudgetHasTool(payload, "workspace_tasks") {
						t.Errorf("topic replacement/selfcheck retained wrong schemas: %v", aiBudgetToolNames(payload))
					}
					if call == 5 {
						writeAIBudgetTextTurn(w, protocol, `已有待确认建议。[opc:selfcheck]{"sufficient":false,"note":"明确尚未创建任务"}[/opc:selfcheck]`)
					} else {
						writeAIBudgetTextTurn(w, protocol, `请确认创建任务的建议，目前尚未创建。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
					}
				case 8:
					for _, name := range aiBudgetToolNames(payload) {
						if strings.HasPrefix(name, "workspace_") && name != "workspace_request_access" {
							t.Errorf("no-grant generation inherited %s", name)
						}
					}
					writeAIBudgetTextTurn(w, protocol, `没有继承工作台权限。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, "unexpected")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			grant := aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions"}}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查询现有任务后创建一条带描述的任务，再了解专注功能。", "workspace": grant})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 6 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("chat %d (%d calls): %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT count(*) FROM tasks", 0)
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil {
				t.Fatal(err)
			}
			if proposal.Status != "pending" || !strings.Contains(proposal.ActionJSON, description) {
				t.Fatalf("description or approval boundary lost: %+v", proposal)
			}
			response = performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
			if response.Code != http.StatusOK {
				t.Fatalf("confirm %d: %s", response.Code, response.Body.String())
			}
			var task models.Task
			if err := store.DB.Where("title = ?", title).First(&task).Error; err != nil || task.Description != description {
				t.Fatalf("confirmed Task description = %q, err %v", task.Description, err)
			}
			var session models.AISession
			if err := store.DB.First(&session).Error; err != nil {
				t.Fatal(err)
			}
			for _, nextGrant := range []*aiWorkspaceGrant{&grant, nil} {
				body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "继续看看", "workspace": nextGrant})
				response = performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: done") {
					t.Fatalf("next generation %d: %s", response.Code, response.Body.String())
				}
			}
			if upstream.calls.Load() != 8 {
				t.Fatalf("model calls = %d", upstream.calls.Load())
			}
		})
	}
}

func TestAIToolCatalogTwoDomainGuideReadsAcrossWorkbenchModules(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_tasks") || aiBudgetHasTool(payload, "workspace_client_records") {
						t.Error("domain definitions appeared before the guide was accepted")
					}
					writeAIBudgetToolTurn(w, protocol, "joint-guide", "workspace_guide", `{"topic":"tasks_projects","related_topic":"people_clients"}`)
				case 2, 3:
					for _, name := range []string{"workspace_tasks", "workspace_client_records", "workspace_propose"} {
						if !aiBudgetHasTool(payload, name) {
							t.Errorf("turn %d omitted the joined authorized tool %s", call, name)
						}
					}
					if aiBudgetHasTool(payload, "workspace_finance") {
						t.Error("joined guide advertised unrelated finance access")
					}
					if call == 2 {
						writeAIBudgetToolTurn(w, protocol, "task-read", "workspace_tasks", `{"filters":{"status":"active"},"limit":1}`)
					} else {
						writeAIBudgetToolTurn(w, protocol, "client-read", "workspace_client_records", `{"type":"followup","view":"list","limit":1}`)
					}
				case 4:
					if !strings.Contains(string(raw), "total_items") {
						t.Error("joined task and client read results did not reach the model")
					}
					writeAIBudgetToolTurn(w, protocol, "client-proposal", "workspace_propose", `{"action":"client.create","changes":{"name":"联合指南客户","status":"lead"}}`)
				case 5:
					if !strings.Contains(string(raw), "NOT executed") {
						t.Error("joined action proposal did not return the pending-confirmation boundary")
					}
					writeAIBudgetTextTurn(w, protocol, `已分别核对任务与客户回访，并提出新建客户建议；仍需你确认，尚未创建。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, "unexpected")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			grant := aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "clients", "actions"}}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查看任务与客户回访，再建议创建一个名为联合指南客户的客户", "workspace": grant})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 5 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("joined guide chat=%d calls=%d body=%s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			var proposal models.AIActionProposal
			if err := store.DB.First(&proposal).Error; err != nil || !strings.Contains(proposal.ActionJSON, `"client.create"`) {
				t.Fatalf("joined guide did not preserve a pending client proposal: %+v, %v", proposal, err)
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM clients WHERE name='联合指南客户'", 0)
		})
	}
}

func TestAIToolCatalogNarrowTaskOutputGuideReadsThenProposesWithoutExecuting(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
			router, store, _ := newAIProviderTestRouter(t, now)
			project := models.Project{ID: uuid.NewString(), Name: "联合指南项目", Status: "planning", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
			if err := store.DB.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_projects") || aiBudgetHasTool(payload, "workspace_project_outputs") {
						t.Error("joint tools appeared before guide acceptance")
					}
					writeAIBudgetToolTurn(w, protocol, "task-output-guide", "workspace_guide", `{"topic":"tasks_projects","related_topic":"outputs"}`)
				case 2:
					for _, name := range []string{"workspace_projects", "workspace_project_outputs", "workspace_outputs", "workspace_propose"} {
						if !aiBudgetHasTool(payload, name) {
							t.Errorf("joint guide omitted %s", name)
						}
					}
					writeAIBudgetToolTurn(w, protocol, "project-read", "workspace_projects", `{"limit":1}`)
				case 3:
					if !strings.Contains(string(raw), project.ID) {
						t.Error("project identity was not returned to the model")
					}
					writeAIBudgetToolTurn(w, protocol, "output-read", "workspace_project_outputs", fmt.Sprintf(`{"project_id":%q}`, project.ID))
				case 4:
					if !strings.Contains(string(raw), project.ID) || !strings.Contains(string(raw), "workspace_project_outputs") {
						t.Error("project output read did not reach the model")
					}
					writeAIBudgetToolTurn(w, protocol, "project-next-task", "workspace_propose", fmt.Sprintf(`{"action":"task.create","changes":{"title":"交付后续任务","project_id":%q}}`, project.ID))
				case 5:
					if !strings.Contains(string(raw), "NOT executed") {
						t.Error("task proposal bypassed the human approval boundary")
					}
					writeAIBudgetTextTurn(w, protocol, `已查看项目和交付清单，后续任务仍待你确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, "unexpected")
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			grant := aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs", "actions"}}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "查看项目当前交付，再建议一条后续任务", "workspace": grant})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 5 || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("joint guide chat=%d calls=%d body=%s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals WHERE status='pending'", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
		})
	}
}
