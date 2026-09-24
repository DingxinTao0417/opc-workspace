package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAIRoadmapMilestonesHarnessDiscoversAndReadsNativePlan(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))
			project := createProjectForTest(t, router, `{"name":"Harness roadmap project"}`, nil)
			created := performRequest(router, http.MethodPost, "/api/v1/roadmap/milestones", []byte(fmt.Sprintf(`{
				"title":"Quarter goal","description":"PRIVATE ROADMAP DESCRIPTION",
				"year":2026,"quarter":3,"target_date":"2026-09-28","status":"planned","project_ids":[%q]
			}`, project.ID)), nil)
			if created.Code != http.StatusCreated {
				t.Fatal(created.Body.String())
			}
			milestone := decodeRoadmapMilestoneResponse(t, created.Body.Bytes())
			upstream := newAIBudgetMockUpstream(t, protocol, func(call int, payload map[string]any, raw []byte, w http.ResponseWriter) {
				if strings.Contains(string(raw), "PRIVATE ROADMAP DESCRIPTION") {
					t.Error("roadmap description leaked into model payload")
				}
				if aiBudgetHasTool(payload, "workspace_propose") {
					t.Error("work-only grant exposed a write tool")
				}
				switch call {
				case 1:
					if aiBudgetHasTool(payload, "workspace_roadmap_milestones") {
						t.Error("roadmap helper was not deferred")
					}
					writeAIBudgetToolTurn(w, protocol, "roadmap-read-1", "workspace_guide", `{"topic":"roadmap_content"}`)
				case 2:
					if !aiBudgetHasTool(payload, "workspace_roadmap_milestones") {
						t.Error("roadmap guide did not expose the list helper")
					}
					writeAIBudgetToolTurn(w, protocol, "roadmap-read-2", "workspace_roadmap_milestones", fmt.Sprintf(`{"filters":{"year":2026,"quarter":3,"project_id":%q},"limit":20}`, project.ID))
				case 3:
					var page struct {
						Total int `json:"total_items"`
						Items []struct {
							ID           string `json:"id"`
							Version      int64  `json:"version"`
							ProjectCount int    `json:"project_count"`
							Route        string `json:"route"`
						} `json:"items"`
					}
					content := aiPlanRetryHarnessResult(t, payload, protocol, "roadmap-read-2", "workspace_roadmap_milestones")
					if json.Unmarshal([]byte(content), &page) != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != milestone.ID || page.Items[0].Version != milestone.Version || page.Items[0].ProjectCount != 1 || page.Items[0].Route != searchRoute("roadmap_milestone", milestone.ID) {
						t.Errorf("roadmap result mismatch: %s", content)
					}
					writeAIBudgetTextTurn(w, protocol, `已读取路线图计划，没有修改里程碑。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				default:
					t.Errorf("unexpected model call %d", call)
					writeAIBudgetTextTurn(w, protocol, `停止。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
				}
			})
			provider := createAIBudgetProvider(t, router, store, upstream, protocol)
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "只读查看今年第三季度与此项目有关的里程碑。", "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}}})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			if response.Code != http.StatusOK || upstream.calls.Load() != 3 || strings.Contains(response.Body.String(), "event: error") || !strings.Contains(response.Body.String(), "event: done") {
				t.Fatalf("read-only chat=%d calls=%d %s", response.Code, upstream.calls.Load(), response.Body.String())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM roadmap_milestones WHERE id=? AND version=1 AND status='planned'", 1, milestone.ID)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 0)
		})
	}
}
