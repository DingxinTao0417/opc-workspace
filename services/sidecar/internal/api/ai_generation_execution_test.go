package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func newGenerationExecutionFixture(t *testing.T) (*API, models.AIProvider, models.AISession, models.AIGeneration) {
	t.Helper()
	_, a := newAIRuntimeLockTest(t)
	now := nowStamp(a)
	provider := models.AIProvider{ID: uuid.NewString(), Name: "execution", Kind: "local", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:1", Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}
	session := models.AISession{ID: uuid.NewString(), Title: "execution", Persist: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID, Status: "streaming", CreatedAt: now, UpdatedAt: now}
	for _, row := range []any{&provider, &session, &generation} {
		if err := a.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := createAIRunRootStep(a.db, generation); err != nil {
		t.Fatal(err)
	}
	return a, provider, session, generation
}

type generationExecutionClient struct {
	requests []harness.Request
	stream   func(context.Context) (harness.Turn, error)
}

func (m *generationExecutionClient) Stream(ctx context.Context, request harness.Request, delta, _ func(string)) (harness.Turn, error) {
	m.requests = append(m.requests, request)
	if m.stream != nil {
		return m.stream(ctx)
	}
	text := `continued answer[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`
	if delta != nil {
		delta(text)
	}
	return harness.Turn{Text: text}, nil
}

func generationExecutionLease(t *testing.T, a *API, provider models.AIProvider, session *models.AISession, generation models.AIGeneration) models.AIContinuation {
	t.Helper()
	now := nowStamp(a)
	if err := a.db.Create(&aiWorkPlanRevision{SessionID: session.ID, Version: 1, GenerationID: generation.ID,
		PlanJSON: `{"title":"check result","steps":[{"id":"next","title":"read","kind":"read","depends_on":[]}]}`, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.finalizeCompletedGeneration(generation, session, "prior", "", provider, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := a.db.First(session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	lease := models.AIContinuation{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		ProviderVersion: provider.Version, ProviderConfigVersion: provider.ConfigVersion, ProviderName: provider.Name,
		ProviderKind: provider.Kind, ProviderProtocol: provider.Protocol, ProviderModel: provider.Model,
		WorkspaceJSON: `{"provider_version":1,"scopes":["work","actions"]}`, InitialPlanVersion: 1, CurrentPlanVersion: 1,
		MaxTurns: 8, Status: "waiting", Reason: "ready", Version: 1, CreatedAt: now, UpdatedAt: now,
		ExpiresAt: a.options.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	if err := a.db.Create(&lease).Error; err != nil {
		t.Fatal(err)
	}
	return lease
}

func generationExecutionAcceptHook(t *testing.T, session models.AISession, lease models.AIContinuation, called *int, fail error) func(*gorm.DB, models.AIGeneration) error {
	t.Helper()
	return func(tx *gorm.DB, generation models.AIGeneration) error {
		*called++
		var sessionBefore models.AISession
		if err := tx.First(&sessionBefore, "id = ?", session.ID).Error; err != nil {
			return err
		}
		if sessionBefore.Version != session.Version {
			t.Errorf("session advanced before claim: %d != %d", sessionBefore.Version, session.Version)
		}
		var generations, messages int64
		tx.Model(&models.AIGeneration{}).Where("id = ?", generation.ID).Count(&generations)
		tx.Model(&models.AIMessage{}).Where("generation_id = ?", generation.ID).Count(&messages)
		if generations != 1 || messages != 0 {
			t.Errorf("hook ordering: generations=%d messages=%d", generations, messages)
		}
		if err := tx.Model(&models.AIContinuation{}).Where("id = ? AND version = ?", lease.ID, lease.Version).Updates(map[string]any{
			"version": lease.Version + 1, "turns_started": 1, "status": "running", "reason": "generating", "current_generation_id": generation.ID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.AIContinuationTurn{ContinuationID: lease.ID, TurnIndex: 1, GenerationID: generation.ID,
			PlanVersion: 1, ObservationHash: strings.Repeat("a", 64), CreatedAt: generation.CreatedAt}).Error; err != nil {
			return err
		}
		return fail
	}
}

func TestGenerationHeadlessAcceptanceIsAtomicAndReplayDoesNotExecute(t *testing.T) {
	for _, failHook := range []bool{true, false} {
		t.Run(map[bool]string{true: "rollback", false: "success"}[failHook], func(t *testing.T) {
			a, provider, session, prior := newGenerationExecutionFixture(t)
			lease := generationExecutionLease(t, a, provider, &session, prior)
			client := &generationExecutionClient{}
			a.harnessClient = client
			a.aiCompactions = newAICompactionRegistry()
			t.Cleanup(a.aiCompactions.close)
			called := 0
			var hookErr error
			if failHook {
				hookErr = errors.New("claim rejected")
			}
			options := aiGenerationRunOptions{GenerationID: uuid.NewString(), ContinuationID: lease.ID, Headless: true,
				OnAccept: generationExecutionAcceptHook(t, session, lease, &called, hookErr)}
			input := chatAIRequest{ProviderID: provider.ID, SessionID: session.ID, Message: "continue after result", Workspace: &aiWorkspaceGrant{ProviderVersion: 1, Scopes: []string{"work", "actions"}}}
			result, err := a.runAIGeneration(context.Background(), input, options, nil)
			var stored models.AIContinuation
			a.db.First(&stored, "id = ?", lease.ID)
			var generations, messages, turns int64
			a.db.Model(&models.AIGeneration{}).Where("id = ?", options.GenerationID).Count(&generations)
			a.db.Model(&models.AIMessage{}).Where("generation_id = ?", options.GenerationID).Count(&messages)
			a.db.Model(&models.AIContinuationTurn{}).Where("continuation_id = ?", lease.ID).Count(&turns)
			if failHook {
				if !errors.Is(err, hookErr) || result.Accepted || stored.TurnsStarted != 0 || generations != 0 || messages != 0 || turns != 0 || len(client.requests) != 0 {
					t.Fatalf("rollback result=%+v err=%v lease=%+v counts=%d/%d/%d calls=%d", result, err, stored, generations, messages, turns, len(client.requests))
				}
				return
			}
			if err != nil || !result.Accepted || result.Status != "completed" || stored.TurnsStarted != 1 || messages != 2 || generations != 1 || turns != 1 || called != 1 || len(client.requests) != 1 || !client.requests[0].DisableTransientRetries {
				t.Fatalf("result=%+v err=%v messages=%d calls=%d hook=%d", result, err, messages, len(client.requests), called)
			}
			a.aiCompactions.mu.Lock()
			compactions := len(a.aiCompactions.active) + len(a.aiCompactions.states)
			a.aiCompactions.mu.Unlock()
			if compactions != 0 {
				t.Fatal("headless generation started implicit compaction")
			}
			result, err = a.runAIGeneration(context.Background(), input, options, nil)
			if err != nil || result.Status != "completed" || len(client.requests) != 1 || called != 1 {
				t.Fatalf("replayed: %+v %v calls=%d hook=%d", result, err, len(client.requests), called)
			}
			input.Message = "different input"
			_, err = a.runAIGeneration(context.Background(), input, options, nil)
			var conflict *aiGenerationRunError
			if !errors.As(err, &conflict) || conflict.Code != "IDEMPOTENCY_KEY_REUSED" || len(client.requests) != 1 {
				t.Fatalf("identity reused: %v", err)
			}
		})
	}
}

func TestGenerationActiveContinuationBlocksManualMessageAndUIGrants(t *testing.T) {
	a, provider, session, prior := newGenerationExecutionFixture(t)
	lease := generationExecutionLease(t, a, provider, &session, prior)
	client := &generationExecutionClient{}
	a.harnessClient = client
	r := gin.New()
	r.POST("/chat", a.chatAI)
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "manual"})
	response := performRequest(r, http.MethodPost, "/chat", body, nil)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "AI_CONTINUATION_ACTIVE") {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, scope := range []string{"workspace_ui", "workspace_browser"} {
		_, err := a.runAIGeneration(context.Background(), chatAIRequest{ProviderID: provider.ID, SessionID: session.ID, Message: "continue", Workspace: &aiWorkspaceGrant{ProviderVersion: 1, Scopes: []string{"work", scope}}},
			aiGenerationRunOptions{GenerationID: uuid.NewString(), ContinuationID: lease.ID, Headless: true, OnAccept: func(*gorm.DB, models.AIGeneration) error { return nil }}, nil)
		var invalid *aiGenerationRunError
		if !errors.As(err, &invalid) || invalid.Code != "AI_CONTINUATION_SCOPE_INVALID" {
			t.Fatalf("scope=%s err=%v", scope, err)
		}
	}
	var messages int64
	a.db.Model(&models.AIMessage{}).Where("session_id=? AND role='user'", session.ID).Count(&messages)
	if messages != 0 || len(client.requests) != 0 {
		t.Fatalf("unauthorized message/model calls %d/%d", messages, len(client.requests))
	}
}

func TestGenerationHeadlessCancellationPersistsActualTerminal(t *testing.T) {
	a, provider, session, prior := newGenerationExecutionFixture(t)
	lease := generationExecutionLease(t, a, provider, &session, prior)
	started := make(chan struct{})
	client := &generationExecutionClient{stream: func(ctx context.Context) (harness.Turn, error) {
		close(started)
		<-ctx.Done()
		return harness.Turn{Text: "partial"}, ctx.Err()
	}}
	a.harnessClient = client
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := 0
	options := aiGenerationRunOptions{GenerationID: uuid.NewString(), ContinuationID: lease.ID, Headless: true, OnAccept: generationExecutionAcceptHook(t, session, lease, &called, nil)}
	done := make(chan aiGenerationRunResult, 1)
	go func() {
		result, err := a.runAIGeneration(ctx, chatAIRequest{ProviderID: provider.ID, SessionID: session.ID, Message: "continue"}, options, nil)
		if err != nil {
			t.Errorf("cancel finalize: %v", err)
		}
		done <- result
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("generation never started")
	}
	cancel()
	select {
	case result := <-done:
		if !result.Accepted || result.Status != "cancelled" {
			t.Fatalf("actual terminal %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not finish")
	}
	var rows []models.AIMessage
	a.db.Where("generation_id = ?", options.GenerationID).Find(&rows)
	if len(rows) != 2 {
		t.Fatalf("user+cancelled assistant: %+v", rows)
	}
	encoded, _ := json.Marshal(rows)
	if !strings.Contains(string(encoded), `"status":"cancelled"`) {
		t.Fatal(string(encoded))
	}
}

func TestGenerationFinalizationIsIdempotent(t *testing.T) {
	a, provider, session, generation := newGenerationExecutionFixture(t)
	for i := 0; i < 2; i++ {
		if err := a.finalizeCompletedGeneration(generation, &session, "original", "", provider, nil, nil, nowStamp(a)); err != nil {
			t.Fatalf("finalize %d: %v", i, err)
		}
	}
	a.finalizeCancelledGeneration(generation, &session, "late cancelled", "", nil)
	a.finalizeFailedGeneration(generation, "AI_STREAM_ERROR", nil)
	var saved models.AIGeneration
	var messages, events int64
	a.db.First(&saved, "id = ?", generation.ID)
	a.db.Model(&models.AIMessage{}).Where("generation_id = ?", generation.ID).Count(&messages)
	a.db.Table("workflow_events").Where("aggregate_id = ? AND action LIKE 'ai_generation_%'", generation.ID).Count(&events)
	a.db.First(&session, "id = ?", session.ID)
	if saved.Status != "completed" || saved.Content == nil || *saved.Content != "original" || messages != 1 || events != 1 || session.Version != 2 {
		t.Fatalf("terminal changed: generation=%+v messages=%d events=%d sessionVersion=%d", saved, messages, events, session.Version)
	}
}

func TestGenerationDoesNotPublishSuccessAfterAnotherTerminalWins(t *testing.T) {
	a, provider, session, _ := newGenerationExecutionFixture(t)
	a.harnessClient = &generationExecutionClient{}
	id := uuid.NewString()
	won := false
	var events []string
	result, err := a.runAIGeneration(context.Background(), chatAIRequest{ProviderID: provider.ID, SessionID: session.ID, Message: "finish"}, aiGenerationRunOptions{GenerationID: id}, func(event, payload string) bool {
		events = append(events, event)
		if !won && event == "progress" && strings.Contains(payload, `"kind":"citation_validation"`) {
			won = true
			var generation models.AIGeneration
			a.db.First(&generation, "id=?", id)
			if cancelErr := a.finalizeCancelledGeneration(generation, &session, "authoritative stop", "", nil); cancelErr != nil {
				t.Fatal(cancelErr)
			}
		}
		return true
	})
	if err != nil || result.Status != "cancelled" || !won {
		t.Fatalf("result=%+v err=%v won=%v", result, err, won)
	}
	for _, event := range events {
		if event == "done" {
			t.Fatal("published completed for a cancelled durable generation")
		}
	}
}
