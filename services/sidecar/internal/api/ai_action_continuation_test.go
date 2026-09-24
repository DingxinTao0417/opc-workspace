package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIActionReceiptSourceRejectedBeforeAcceptance(t *testing.T) {
	router, store, _, _, generation := aiActionTestFixture(t)
	body, _ := json.Marshal(map[string]any{
		"provider_id": generation.ProviderID, "session_id": generation.SessionID,
		"message": "继续处理", "action_receipt_generation_id": "not-a-uuid",
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	assertAPIError(t, response, 422, "AI_ACTION_RECEIPT_SOURCE_INVALID")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 1)
}

func TestAIActionReceiptSourceUnavailableHasNoModelCallsOrWrites(t *testing.T) {
	for _, kind := range []string{"noncanonical", "missing_session", "unknown_session", "unknown_generation", "foreign_session", "ephemeral", "queued", "streaming", "no_proposals", "corrupt_proposal"} {
		t.Run(kind, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			if kind != "no_proposals" {
				proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Private source","description":"Private proposal body"}}`)
			}
			finishAIGeneration(t, store, generation)
			var calls atomic.Int32
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				streamMockAIDelta(w, "must not run")
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "invalid-source", upstream.URL+"/v1", "test-model")
			sessionID, generationID := generation.SessionID, generation.ID
			status, code := 409, "AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE"
			sessionCount := int64(1)
			switch kind {
			case "noncanonical":
				generationID = "{" + generationID + "}"
				status, code = 422, "AI_ACTION_RECEIPT_SOURCE_INVALID"
			case "missing_session":
				sessionID = ""
				status, code = 422, "AI_ACTION_RECEIPT_SOURCE_INVALID"
			case "unknown_session":
				sessionID = uuid.NewString()
			case "unknown_generation":
				generationID = uuid.NewString()
			case "foreign_session":
				var session models.AISession
				if err := store.DB.First(&session, "id=?", sessionID).Error; err != nil {
					t.Fatal(err)
				}
				session.ID = uuid.NewString()
				if err := store.DB.Create(&session).Error; err != nil {
					t.Fatal(err)
				}
				sessionID, sessionCount = session.ID, 2
			case "ephemeral":
				if err := store.DB.Model(&models.AISession{}).Where("id=?", sessionID).Updates(map[string]any{"persist": false, "version": gorm.Expr("version + 1")}).Error; err != nil {
					t.Fatal(err)
				}
			case "queued", "streaming":
				if err := store.DB.Model(&generation).Update("status", kind).Error; err != nil {
					t.Fatal(err)
				}
			case "corrupt_proposal":
				// Valid JSON but not an action object: projection failures must not
				// leak the stored approval contents or fall back to recent receipts.
				if err := store.DB.Exec("DROP TRIGGER trg_ai_action_proposals_immutable").Error; err != nil {
					t.Fatal(err)
				}
				if err := store.DB.Exec(`UPDATE ai_action_proposals SET action_json='"private-invalid-action"'`).Error; err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": sessionID, "message": "继续处理", "action_receipt_generation_id": generationID})
			response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
			assertAPIError(t, response, status, code)
			for _, private := range []string{"Private", "private-invalid-action", generation.ID, generation.SessionID} {
				if strings.Contains(response.Body.String(), private) {
					t.Fatalf("source error disclosed %q: %s", private, response.Body.String())
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("rejected source called model %d times", calls.Load())
			}
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 1)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", sessionCount)
			assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
		})
	}
}

func TestAIActionReceiptSourceSelectsOldGenerationAndBoundsItsOwnItems(t *testing.T) {
	_, store, service, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Private historical task","description":"Private historical description"}}`)
	finishAIGeneration(t, store, generation)
	for i := 0; i < 18; i++ {
		newGeneration := generation
		newGeneration.ID, newGeneration.Status = uuid.NewString(), "completed"
		if err := store.DB.Create(&newGeneration).Error; err != nil {
			t.Fatal(err)
		}
		newRow := row
		newRow.ID, newRow.GenerationID = uuid.NewString(), newGeneration.ID
		newRow.CreatedAt = service.options.Now().Add(time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano)
		if err := store.DB.Create(&newRow).Error; err != nil {
			t.Fatal(err)
		}
	}
	ordinary, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || strings.Contains(ordinary, row.ID) || strings.Contains(ordinary, "source_generation_id") {
		t.Fatalf("ordinary receipts changed or include old source: %s %v", ordinary, err)
	}
	var envelope struct {
		SourceGenerationID string `json:"source_generation_id"`
		AsOf               string `json:"as_of"`
		Limited            bool   `json:"limited"`
		Items              []struct {
			ProposalID   string `json:"proposal_id"`
			GenerationID string `json:"generation_id"`
			Status       string `json:"status"`
		} `json:"items"`
	}
	for _, status := range []string{"completed", "failed", "cancelled"} {
		if err := store.DB.Model(&generation).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		exact, err := service.aiActionReceiptsForGeneration(context.Background(), generation.SessionID, generation.ID)
		if err != nil || json.Unmarshal([]byte(exact), &envelope) != nil || len(envelope.Items) != 1 || envelope.Items[0].ProposalID != row.ID || envelope.SourceGenerationID != generation.ID || envelope.Limited || envelope.AsOf == "" {
			t.Fatalf("exact source (%s)=%s err=%v", status, exact, err)
		}
		if status != "completed" && envelope.Items[0].Status != "unavailable" {
			t.Fatalf("terminal source misrepresents incomplete action: %s", exact)
		}
		for _, private := range []string{"Private", "changes", "preview", "fingerprint", "description"} {
			if strings.Contains(exact, private) {
				t.Fatalf("exact receipt leaked %q", private)
			}
		}
	}
	for i := 0; i < 20; i++ {
		copy := row
		copy.ID, copy.Fingerprint = uuid.NewString(), fmt.Sprintf("%064x", i+1)
		if err := store.DB.Create(&copy).Error; err != nil {
			t.Fatal(err)
		}
	}
	exact, err := service.aiActionReceiptsForGeneration(context.Background(), generation.SessionID, generation.ID)
	if err != nil || json.Unmarshal([]byte(exact), &envelope) != nil || len(envelope.Items) != 16 || !envelope.Limited || len(exact) > 8<<10 {
		t.Fatalf("bounded exact=%s err=%v", exact, err)
	}
	for _, item := range envelope.Items {
		if item.GenerationID != generation.ID {
			t.Fatalf("mixed generation receipt=%s", exact)
		}
	}
}

func TestAIActionReceiptSourceRecheckedBeforeAcceptanceWrites(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Source removed during acceptance"}}`)
	finishAIGeneration(t, store, generation)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		streamMockAIDelta(w, "must not run")
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "removed-source", upstream.URL+"/v1", "test-model")
	var deleted atomic.Bool
	callback := "test:delete-action-receipt-source-after-read"
	if err := store.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "ai_sessions" && deleted.CompareAndSwap(false, true) {
			if err := store.DB.Delete(&generation).Error; err != nil {
				t.Error(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer store.DB.Callback().Query().Remove(callback)
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": generation.SessionID, "message": "继续处理", "action_receipt_generation_id": generation.ID})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	assertAPIError(t, response, 409, "AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE")
	if !deleted.Load() || calls.Load() != 0 {
		t.Fatalf("race deletion=%v model calls=%d", deleted.Load(), calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 1)
}

func TestAIChatActionReceiptContinuationReadsRealProjectWithFreshGrantBeforeTaskProposal(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	projectProposal := proposeTestAction(t, store, tool, `{"action":"project.create","changes":{"name":"Confirmed project","description":"Project body requires fresh work scope"}}`)
	finishAIGeneration(t, store, generation)
	confirm := func(proposal models.AIActionProposal) {
		t.Helper()
		response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
	}
	confirm(projectProposal)
	var project models.Project
	if err := store.DB.First(&project).Error; err != nil {
		t.Fatal(err)
	}
	// More than one ordinary receipt window has elapsed since this approval.
	for i := 0; i < 18; i++ {
		copyGeneration := generation
		copyGeneration.ID, copyGeneration.Status = uuid.NewString(), "completed"
		if err := store.DB.Create(&copyGeneration).Error; err != nil {
			t.Fatal(err)
		}
		copyProposal := projectProposal
		copyProposal.ID, copyProposal.GenerationID = uuid.NewString(), copyGeneration.ID
		copyProposal.CreatedAt = service.options.Now().Add(time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano)
		if err := store.DB.Create(&copyProposal).Error; err != nil {
			t.Fatal(err)
		}
	}
	ordinary, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || strings.Contains(ordinary, projectProposal.ID) {
		t.Fatalf("fixture did not age source out of recent window: %s %v", ordinary, err)
	}
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
			Tools    json.RawMessage                  `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			t.Errorf("decode continuation request: %v", err)
			return
		}
		system := request.Messages[0].Content
		if !strings.Contains(system, `"source_generation_id":"`+generation.ID+`"`) || !strings.Contains(system, projectProposal.ID) || !strings.Contains(system, `"status":"confirmed"`) || !strings.Contains(system, project.ID) {
			t.Errorf("precise confirmed receipt missing: %s", system)
		}
		if strings.Contains(system, project.Name) || strings.Contains(system, "Project body requires fresh work scope") {
			t.Error("receipt automatically disclosed project content")
		}
		sendTool := func(name, args string) {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("continuation-%d", call), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
		hasToolContent := func(fragment string) bool {
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, fragment) {
					return true
				}
			}
			return false
		}
		switch call {
		case 1:
			if !strings.Contains(string(request.Tools), "workspace_get") || !strings.Contains(string(request.Tools), "workspace_guide") || strings.Contains(string(request.Tools), "workspace_propose") {
				t.Error("fresh work/actions grant exposed the wrong initial model catalog")
			}
			sendTool("workspace_get", fmt.Sprintf(`{"type":"project","id":%q}`, project.ID))
		case 2:
			if !hasToolContent("Project body requires fresh work scope") || !hasToolContent(project.ID) {
				t.Error("dependent proposal did not receive a real authorized Project read")
			}
			sendTool("workspace_propose", fmt.Sprintf(`{"action":"task.create","changes":{"title":"Dependent task","project_id":%q}}`, project.ID))
		case 3:
			if !hasToolContent("NOT executed") {
				t.Error("dependent task should remain a proposal awaiting human confirmation")
			}
			streamMockAIDelta(w, `项目已创建，关联任务等待你确认。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		case 4:
			if aiHasProtectedWorkspaceTool(request.Tools) {
				t.Error("exact receipt inherited prior workspace permissions")
			}
			streamMockAIDelta(w, `已读取指定操作的状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		default:
			t.Errorf("unexpected model call %d", call)
			streamMockAIDelta(w, `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
		}
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "receipt-continuation", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"provider_id": provider.ID, "session_id": generation.SessionID, "message": "根据这个已确认的项目继续准备关联任务",
		"action_receipt_generation_id": generation.ID,
		"workspace":                    aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}},
	}
	body, _ := json.Marshal(input)
	headers := map[string]string{"Idempotency-Key": "exact-receipt-continuation"}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, headers)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls.Load() != 3 {
		t.Fatalf("continuation=%d %s model calls=%d", response.Code, response.Body.String(), calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
	var taskProposal models.AIActionProposal
	if err := store.DB.Where("json_extract(action_json,'$.action')='task.create'").First(&taskProposal).Error; err != nil {
		t.Fatal(err)
	}
	confirm(taskProposal)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE project_id=? AND title='Dependent task'", 1, project.ID)
	// A source is part of request identity; neither replay nor changing just its
	// ID may silently create another model run or a second proposal.
	assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, headers), 409, "AI_CHAT_ALREADY_ACCEPTED")
	input["action_receipt_generation_id"] = taskProposal.GenerationID
	changedBody, _ := json.Marshal(input)
	assertAPIError(t, performRequest(router, http.MethodPost, "/api/v1/ai/chat", changedBody, headers), 409, "IDEMPOTENCY_KEY_REUSED")
	if calls.Load() != 3 {
		t.Fatal("idempotency replay executed the model")
	}
	delete(input, "workspace")
	input["action_receipt_generation_id"], input["message"] = generation.ID, "只复核这条历史项目操作"
	body, _ = json.Marshal(input)
	response = performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls.Load() != 4 {
		t.Fatalf("ungranted continuation=%d %s model calls=%d", response.Code, response.Body.String(), calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM projects", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_action_proposals", 20)
}
