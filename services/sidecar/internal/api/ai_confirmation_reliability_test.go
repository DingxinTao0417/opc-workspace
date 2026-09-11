package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIMemoryNonPersistentToolDoesNotStoreBody(t *testing.T) {
	now := time.Now().UTC()
	_, store, _ := newAIProviderTestRouter(t, now)
	session := models.AISession{ID: uuid.NewString(), Title: "temporary", Persist: false, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiMemoryToolRegistry(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"memory_write", "memory_propose"} {
		tool, _ := registry.Get(name)
		result, err := tool.Execute(context.Background(), json.RawMessage(`{"content":"private-body-marker"}`))
		if err != nil {
			t.Fatal(err)
		}
		if name == "memory_propose" && strings.Contains(result, `"proposal_id"`) {
			t.Errorf("ephemeral proposal has durable identity: %s", result)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE session_id = ?", 0, session.ID)
	search, _ := registry.Get("memory_search")
	result, err := search.Execute(context.Background(), json.RawMessage(`{"query":"private-body-marker"}`))
	if err != nil || !strings.Contains(result, "private-body-marker") {
		t.Fatalf("running memory unavailable: %s %v", result, err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE current_json LIKE ?", 0, "%private-body-marker%")
}

func TestAITaskConfirmationConcurrentAndRollback(t *testing.T) {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	router, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	session := models.AISession{ID: uuid.NewString(), Title: "atomic", Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	message := models.AIMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed", Content: `[opc:task]{"title":"atomic task"}[/opc:task]`, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/messages/" + message.ID + "/task-confirmation"
	if err := store.DB.Exec(`CREATE TRIGGER test_confirm_failure BEFORE UPDATE ON ai_messages WHEN NEW.task_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'test storage failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := performRequest(router, http.MethodPost, path, []byte(`{"title":"atomic task"}`), nil)
	if failed.Code != 500 {
		t.Fatalf("expected injected failure: %d %s", failed.Code, failed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title = ?", 0, "atomic task")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE id = ? AND task_id IS NOT NULL", 0, message.ID)
	if err := store.DB.Exec("DROP TRIGGER test_confirm_failure").Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- performRequest(router, http.MethodPost, path, []byte(`{"title":"atomic task"}`), nil).Code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != http.StatusCreated && code != http.StatusOK {
			t.Errorf("concurrent confirmation status=%d", code)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title = ?", 1, "atomic task")
	// Plain task creation has no task_created event. Confirmation must preserve
	// the domain's existing event contract instead of inventing a second stream.
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'task_created' AND aggregate_id IN (SELECT id FROM tasks WHERE title = ?)", 0, "atomic task")
}

func TestAIMemoryLegacyDecisionSurvivesReloadAndResponseLoss(t *testing.T) {
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	router, store, _ := newAIProviderTestRouter(t, now)
	session := models.AISession{ID: uuid.NewString(), Title: "legacy", Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	for _, decision := range []string{"confirmed", "rejected"} {
		message := models.AIMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed", Content: `可以记住偏好 [opc:memory]{"content":"回答简洁"}[/opc:memory]`, CreatedAt: stamp, UpdatedAt: stamp}
		if err := store.DB.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/ai/messages/" + message.ID + "/memory-decision"
		initial := performRequest(router, http.MethodGet, path, nil, nil)
		if initial.Code != 200 || !strings.Contains(initial.Body.String(), `"status":"pending"`) {
			t.Fatalf("read legacy pending: %d %s", initial.Code, initial.Body.String())
		}
		assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE id = ?", 0, message.ID)
		body := []byte(`{"content":"回答简洁","source_message_id":"` + message.ID + `"}`)
		var memoryID string
		for attempt := 0; attempt < 2; attempt++ {
			if decision == "rejected" {
				result := performRequest(router, http.MethodDelete, path, nil, nil)
				if result.Code != 200 {
					t.Fatalf("reject legacy: %d %s", result.Code, result.Body.String())
				}
			} else {
				result := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, nil)
				if result.Code != 201 && result.Code != 200 {
					t.Fatalf("confirm legacy: %d %s", result.Code, result.Body.String())
				}
				var parsed struct {
					Data aiMemoryResponse `json:"data"`
				}
				if err := json.Unmarshal(result.Body.Bytes(), &parsed); err != nil {
					t.Fatal(err)
				}
				if memoryID != "" && memoryID != parsed.Data.ID {
					t.Fatal("legacy replay created another memory")
				}
				memoryID = parsed.Data.ID
			}
		}
		readback := performRequest(router, http.MethodGet, path, nil, nil)
		if readback.Code != 200 || !strings.Contains(readback.Body.String(), `"status":"`+decision+`"`) {
			t.Fatalf("resolved readback: %d %s", readback.Code, readback.Body.String())
		}
		if decision == "rejected" {
			result := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, nil)
			if result.Code != 409 {
				t.Fatalf("rejected proposal recreated: %d %s", result.Code, result.Body.String())
			}
		} else {
			result := performRequest(router, http.MethodDelete, path, nil, nil)
			if result.Code != 409 {
				t.Fatalf("confirmed proposal rejected: %d %s", result.Code, result.Body.String())
			}
			deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/memories/"+memoryID, nil, nil)
			if deleted.Code != 200 {
				t.Fatal(deleted.Body.String())
			}
			retry := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, nil)
			if retry.Code != 410 {
				t.Fatalf("deleted confirmed memory recreated: %d %s", retry.Code, retry.Body.String())
			}
		}
	}
}

func TestAIMemoryPreMigrationLegacyDeletionKeepsDecision(t *testing.T) {
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	router, store, _ := newAIProviderTestRouter(t, now)
	session := models.AISession{ID: uuid.NewString(), Title: "old confirmation", Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	message := models.AIMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed", Content: `[opc:memory]{"content":"历史偏好"}[/opc:memory]`, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	memory := models.AIMemory{ID: uuid.NewString(), Content: "历史偏好", SourceMessageID: &message.ID, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&memory).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/messages/" + message.ID + "/memory-decision"
	initial := performRequest(router, http.MethodGet, path, nil, nil)
	if initial.Code != 200 || !strings.Contains(initial.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("legacy confirmed read: %d %s", initial.Code, initial.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries WHERE id = ?", 0, message.ID)
	deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/memories/"+memory.ID, nil, nil)
	if deleted.Code != 200 {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	readback := performRequest(router, http.MethodGet, path, nil, nil)
	if readback.Code != 200 || !strings.Contains(readback.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("legacy decision lost: %d %s", readback.Code, readback.Body.String())
	}
	retry := performRequest(router, http.MethodPost, "/api/v1/ai/memories", []byte(`{"content":"历史偏好","source_message_id":"`+message.ID+`"}`), nil)
	if retry.Code != 410 {
		t.Fatalf("old memory recreated: %d %s", retry.Code, retry.Body.String())
	}
}

func TestAIMemoryCachedConfirmationCannotReportDeletedMemoryAsSaved(t *testing.T) {
	router, _, _ := newAIProviderTestRouter(t, time.Now().UTC())
	body := []byte(`{"content":"明确保存后删除的偏好"}`)
	headers := map[string]string{"Idempotency-Key": "memory-lost-response"}
	created := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, headers)
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var parsed struct {
		Data aiMemoryResponse `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/ai/memories/"+parsed.Data.ID, nil, nil)
	if deleted.Code != 200 {
		t.Fatal(deleted.Body.String())
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/ai/memories", body, headers)
	if replay.Code != 410 {
		t.Fatalf("stale cache reported saved: %d %s", replay.Code, replay.Body.String())
	}
}

func TestAITaskConfirmationAtomicReplayAndStaticBinding(t *testing.T) {
	now := time.Now().UTC()
	router, store, _ := newAIProviderTestRouter(t, now)
	session := models.AISession{ID: uuid.NewString(), Title: "confirm", Persist: true, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	message := models.AIMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Status: "completed", Content: `尚未创建 [opc:task]{"title":"写作业"}[/opc:task]`, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt}
	if err := store.DB.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/ai/messages/" + message.ID + "/task-confirmation"
	for i := 0; i < 3; i++ {
		result := performRequest(router, http.MethodPost, path, []byte(`{"title":"写作业"}`), nil)
		if result.Code != http.StatusCreated && result.Code != http.StatusOK {
			t.Fatalf("confirm %d: %d %s", i, result.Code, result.Body.String())
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title = ?", 1, "写作业")
	conflict := performRequest(router, http.MethodPost, path, []byte(`{"title":"另一项作业"}`), nil)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("changed confirmation accepted: %d %s", conflict.Code, conflict.Body.String())
	}
	if err := store.DB.First(&message, "id = ?", message.ID).Error; err != nil {
		t.Fatal(err)
	}
	if message.TaskID == nil {
		t.Fatal("task not linked atomically")
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+*message.TaskID, nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusOK && deleted.Code != http.StatusNoContent {
		t.Fatalf("delete task: %d %s", deleted.Code, deleted.Body.String())
	}
	retry := performRequest(router, http.MethodPost, path, []byte(`{"title":"写作业"}`), nil)
	if retry.Code != http.StatusGone {
		t.Fatalf("deleted confirmed task recreated: %d %s", retry.Code, retry.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE title = ?", 0, "写作业")
}
