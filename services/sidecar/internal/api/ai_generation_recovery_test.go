package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestChatAcceptedRequestRetriesRecoverOneGeneration(t *testing.T) {
	r, a := newAIRuntimeLockTest(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.db.Create(&models.AIProvider{ID: id, Name: "recovery", Kind: "local", Protocol: "openai_chat", BaseURL: server.URL, Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	body := []byte(fmt.Sprintf(`{"provider_id":%q,"message":"hello"}`, id))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat", strings.NewReader(string(body))).WithContext(ctx)
	request.Header.Set("Idempotency-Key", key)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); r.ServeHTTP(w, request); done <- w }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream not started")
	}
	replay := performRequest(r, http.MethodPost, "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": key})
	if replay.Code != http.StatusConflict || responseErrorCode(t, replay.Body.Bytes()) != "AI_CHAT_ALREADY_ACCEPTED" {
		cancel()
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	var replayData struct {
		GenerationID string `json:"generation_id"`
		SessionID    string `json:"session_id"`
	}
	json.Unmarshal(replay.Body.Bytes(), &replayData)
	active := performRequest(r, http.MethodGet, "/api/v1/ai/active-generations", nil, nil)
	if !strings.Contains(active.Body.String(), key) || !strings.Contains(active.Body.String(), replayData.GenerationID) {
		cancel()
		t.Fatalf("missing recovery identity=%s", active.Body.String())
	}
	changed := performRequest(r, http.MethodPost, "/api/v1/ai/chat", []byte(fmt.Sprintf(`{"provider_id":%q,"message":"different"}`, id)), map[string]string{"Idempotency-Key": key})
	if changed.Code != http.StatusConflict || responseErrorCode(t, changed.Body.Bytes()) != "IDEMPOTENCY_KEY_REUSED" {
		cancel()
		t.Fatalf("changed body=%d %s", changed.Code, changed.Body.String())
	}
	close(release)
	select {
	case completed := <-done:
		if !strings.Contains(completed.Body.String(), "event: done") {
			t.Fatal(completed.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("chat stuck")
	}
	byKey := performRequest(r, http.MethodGet, "/api/v1/ai/generations/by-request/"+key, nil, nil)
	if byKey.Code != http.StatusOK || !strings.Contains(byKey.Body.String(), `"status":"completed"`) || !strings.Contains(byKey.Body.String(), `"content":"partial"`) {
		t.Fatalf("terminal recovery=%s", byKey.Body.String())
	}
	replay = performRequest(r, http.MethodPost, "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": key})
	if responseErrorCode(t, replay.Body.Bytes()) != "AI_CHAT_ALREADY_ACCEPTED" || calls.Load() != 1 {
		t.Fatalf("accepted retried upstream calls=%d response=%s", calls.Load(), replay.Body.String())
	}
	var count int64
	a.db.Model(&models.AIGeneration{}).Count(&count)
	if count != 1 {
		t.Fatalf("generation count=%d", count)
	}
}

func TestIncompleteChatPersistsRecoverableFailedPartialOnlyWhenPersistent(t *testing.T) {
	for _, persist := range []bool{true, false} {
		t.Run(fmt.Sprint(persist), func(t *testing.T) {
			router, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
			upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"private-partial\"}}]}\n\n")
			})
			defer upstream.Close()
			provider := createReadyAIProvider(t, router, "partial", upstream.URL+"/v1", "test")
			now := time.Now().UTC().Format(time.RFC3339Nano)
			session := models.AISession{ID: uuid.NewString(), Title: "partial", Persist: persist, Version: 1, CreatedAt: now, UpdatedAt: now}
			if err := store.DB.Create(&session).Error; err != nil {
				t.Fatal(err)
			}
			response := chatRequest(t, router, provider.ID, session.ID, "hello")
			if strings.Contains(response.Body.String(), "event: done") || !strings.Contains(response.Body.String(), "AI_STREAM_INCOMPLETE") {
				t.Fatal(response.Body.String())
			}
			var row models.AIGeneration
			if err := store.DB.Where("session_id = ?", session.ID).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != "failed" {
				t.Fatalf("status=%s", row.Status)
			}
			var messages []models.AIMessage
			store.DB.Where("session_id = ? AND role = 'assistant'", session.ID).Find(&messages)
			if persist {
				if row.Content == nil || *row.Content != "private-partial" || len(messages) != 1 || messages[0].Status != "failed" {
					t.Fatalf("partial not recoverable generation=%+v messages=%+v", row, messages)
				}
			} else if row.Content != nil || len(messages) > 0 {
				t.Fatalf("nonpersistent reply retained generation=%+v messages=%+v", row, messages)
			}
		})
	}
}
