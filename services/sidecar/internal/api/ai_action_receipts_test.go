package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIActionReceiptsTrackDecisionsWithoutBusinessContentsOrForeignSessions(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Private title","description":"Private business text"}}`)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	read := func(status string) string {
		t.Helper()
		body, err := service.aiActionReceipts(context.Background(), generation.SessionID)
		if err != nil || !strings.Contains(body, `"status":"`+status+`"`) || !strings.Contains(body, row.ID) {
			t.Fatalf("receipt=%s err=%v", body, err)
		}
		for _, private := range []string{"Private", "changes", "preview", "fingerprint", "description"} {
			if strings.Contains(body, private) {
				t.Fatalf("receipt leaked %q", private)
			}
		}
		return body
	}
	read("pending")
	if other, err := service.aiActionReceipts(context.Background(), uuid.NewString()); err != nil || other != "" {
		t.Fatalf("foreign receipts=%s err=%v", other, err)
	}
	path := "/api/v1/ai/actions/" + row.ID + "/decision"
	response := performRequest(router, "POST", path, []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var task models.Task
	if err := store.DB.First(&task).Error; err != nil {
		t.Fatal(err)
	}
	body := read("confirmed")
	if !strings.Contains(body, task.ID) || !strings.Contains(body, `"result_version":1`) {
		t.Fatalf("missing result identity: %s", body)
	}
	// Receipt describes execution history even after a later manual task change.
	if err := store.DB.Model(&task).Updates(map[string]any{"title": "Changed later", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if read("confirmed") != body {
		t.Fatal("receipt read fresh business data or changed execution version")
	}
}

func TestAIActionReceiptsBoundedAndRefreshUnavailableExpiredRejectedStates(t *testing.T) {
	router, store, service, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Not executed"}}`)
	if err := store.DB.Model(&generation).Update("status", "failed").Error; err != nil {
		t.Fatal(err)
	}
	body, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || !strings.Contains(body, `"status":"unavailable"`) {
		t.Fatalf("failed receipt: %s %v", body, err)
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	now := service.options.Now()
	service.options.Now = func() time.Time { return now.Add(25 * time.Hour) }
	body, err = service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || !strings.Contains(body, `"status":"expired"`) {
		t.Fatalf("expired receipt: %s %v", body, err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"reject"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	body, err = service.aiActionReceipts(context.Background(), generation.SessionID)
	if err != nil || !strings.Contains(body, `"status":"rejected"`) || strings.Contains(body, "result_id") {
		t.Fatalf("rejected receipt: %s %v", body, err)
	}
	for i := 0; i < 18; i++ {
		copyGeneration := generation
		copyGeneration.ID = uuid.NewString()
		if err := store.DB.Create(&copyGeneration).Error; err != nil {
			t.Fatal(err)
		}
		copyProposal := row
		copyProposal.ID, copyProposal.GenerationID = uuid.NewString(), copyGeneration.ID
		copyProposal.CreatedAt = now.Add(time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano)
		if err := store.DB.Create(&copyProposal).Error; err != nil {
			t.Fatal(err)
		}
	}
	body, err = service.aiActionReceipts(context.Background(), generation.SessionID)
	var envelope struct {
		Limited bool              `json:"limited"`
		Items   []json.RawMessage `json:"items"`
	}
	if err != nil || json.Unmarshal([]byte(body), &envelope) != nil || !envelope.Limited || len(envelope.Items) != 16 || len(body) > 8<<10 || strings.Contains(body, row.ID) {
		t.Fatalf("bounded receipt: %s %v", body, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 0)
}

func TestAIChatActionReceiptsSurviveFollowupWithoutInheritingPermission(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"task.create","changes":{"title":"Approved task"}}`)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var captured map[string]json.RawMessage
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		streamMockAIDelta(w, `已确认该操作执行成功。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "receipts", upstream.URL+"/v1", "test-model")
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": generation.SessionID, "message": "刚才执行了吗？"})
	response = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatal(response.Body.String())
	}
	var messages []struct{ Role, Content string }
	if err := json.Unmarshal(captured["messages"], &messages); err != nil || len(messages) < 2 {
		t.Fatalf("messages=%s err=%v", captured["messages"], err)
	}
	if !strings.Contains(messages[0].Content, row.ID) || !strings.Contains(messages[0].Content, `"status":"confirmed"`) || strings.Contains(messages[0].Content, "Approved task") {
		t.Fatalf("missing/private receipt: %s", messages[0].Content)
	}
	if aiHasProtectedWorkspaceTool(captured["tools"]) {
		t.Fatalf("prior permission inherited: %s", captured["tools"])
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks", 1)
	if err := store.DB.Model(&models.AISession{}).Where("id = ?", generation.SessionID).Updates(map[string]any{"persist": false, "version": gorm.Expr("version + 1")}).Error; err != nil {
		t.Fatal(err)
	}
	captured = nil
	response = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(string(captured["messages"]), row.ID) || strings.Contains(string(captured["messages"]), "本会话工作台操作回执") {
		t.Fatal("ephemeral followup injected approval receipts")
	}
}

func TestAIActionReceiptReportsInboxReadAllResultWithoutCandidateContents(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 654, time.UTC)
	router, store, service, tool, generation := aiActionTestFixture(t, now)
	for _, body := range []string{
		`{"title":"Private receipt alpha","summary":"receipt-summary-secret","payload_json":{"secret":"receipt-payload-secret"}}`,
		`{"title":"Private receipt beta"}`,
	} {
		createInboxItemForTest(t, router, body, "")
	}
	cutoff := formatInboxTimestamp(now)
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"inbox.read_all","changes":{"through_created_at":%q}}`, cutoff))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}

	body, err := service.aiActionReceipts(context.Background(), generation.SessionID)
	var envelope struct {
		Items []struct {
			ProposalID         string                   `json:"proposal_id"`
			Action             string                   `json:"action"`
			Status             string                   `json:"status"`
			Route              string                   `json:"route"`
			TargetID           string                   `json:"target_id"`
			ResultID           *string                  `json:"result_id"`
			ResultVersion      *int64                   `json:"result_version"`
			InboxReadAllResult *readAllInboxItemsOutput `json:"inbox_read_all_result"`
		} `json:"items"`
	}
	if err != nil || json.Unmarshal([]byte(body), &envelope) != nil || len(envelope.Items) != 1 {
		t.Fatalf("receipt=%s err=%v", body, err)
	}
	receipt := envelope.Items[0]
	if receipt.ProposalID != row.ID || receipt.Action != "inbox.read_all" || receipt.Status != "confirmed" ||
		receipt.Route != "/inbox" || receipt.TargetID != "" || receipt.ResultID == nil || *receipt.ResultID != row.ID ||
		receipt.ResultVersion == nil || *receipt.ResultVersion != 1 || receipt.InboxReadAllResult == nil ||
		receipt.InboxReadAllResult.ThroughCreatedAt != cutoff || receipt.InboxReadAllResult.MarkedCount != 2 {
		t.Fatalf("read_all receipt=%s", body)
	}
	for _, private := range []string{"Private receipt", "receipt-summary-secret", "receipt-payload-secret", "selection_fingerprint", "preview", "changes"} {
		if strings.Contains(body, private) {
			t.Fatalf("receipt leaked %q: %s", private, body)
		}
	}
}
