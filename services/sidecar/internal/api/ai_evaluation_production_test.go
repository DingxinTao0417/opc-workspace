package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/aieval"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIEvaluationUsesProductionPromptProtocolAndHarnessRevision(t *testing.T) {
	responses := goodEvaluationResponsesForSuite(t, aieval.SuiteSmoke)
	var mu sync.Mutex
	var prompts []string
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		calls++
		call := calls
		if len(request.Messages) > 0 {
			prompts = append(prompts, request.Messages[0].Content)
		}
		mu.Unlock()
		text := "wrong draft[opc:selfcheck]{\"sufficient\":false,\"note\":\"核对引用资料\"}[/opc:selfcheck]"
		if call > 1 && call <= len(responses)+1 {
			text = responses[call-2] + `[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}, "finish_reason": nil}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", chunk)
	})
	defer upstream.Close()
	router, store, provider := newAIEvaluationTestRouter(t, harness.NewModelClient(upstream.Client()))
	if err := store.DB.Model(&provider).Updates(map[string]any{"base_url": upstream.URL + "/v1", "version": provider.Version + 1, "config_version": provider.ConfigVersion + 1}).Error; err != nil {
		t.Fatal(err)
	}
	created, run := createEvaluationRequestForSuite(t, router, provider, "production-harness", aieval.SuiteSmoke)
	if created.Code != 202 {
		t.Fatalf("create: %#v", created)
	}
	terminal := waitForEvaluationStatus(t, store, run.ID, true)
	if terminal.Status != "succeeded" || terminal.PassedCases != 8 || terminal.ProviderConfigVersion == nil || *terminal.ProviderConfigVersion != provider.ConfigVersion {
		t.Fatalf("run=%#v", terminal)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 9 {
		t.Fatalf("calls=%d, want 8 cases plus real selfcheck revision", calls)
	}
	for _, prompt := range prompts {
		if !strings.Contains(prompt, modelclient.SystemPrompt) || !strings.Contains(prompt, "opc:selfcheck") {
			t.Fatalf("production prompt missing: %.120s", prompt)
		}
	}
	var first models.AIEvaluationResult
	if err := store.DB.First(&first, "run_id=? AND sequence=1", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.InputTokens == nil || *first.InputTokens != 10 || first.OutputTokens == nil || *first.OutputTokens != 6 {
		t.Fatalf("revision/tail usage missing: %#v", first)
	}
}

func TestSlowAIEvaluationReleasesGlobalLocks(t *testing.T) {
	_, a := newAIRuntimeLockTest(t)
	provider := models.AIProvider{ID: uuid.NewString(), Name: "Slow evaluation", Kind: "local", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:11434/v1", Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: stringPointer("2026-09-09T08:00:00Z"), Version: 1, ConfigVersion: 1, CreatedAt: "2026-09-09T08:00:00Z", UpdatedAt: "2026-09-09T08:00:00Z"}
	if err := a.db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	run := models.AIEvaluationRun{ID: uuid.NewString(), ProviderID: provider.ID, ProviderNameSnapshot: provider.Name, ProviderModelSnapshot: provider.Model, ProviderProtocolSnapshot: provider.Protocol, ProviderVersion: 1, ProviderConfigVersion: &provider.ConfigVersion, DatasetVersion: 4, SuiteKey: aieval.SuiteSmoke, Status: "queued", TotalCases: 8, CreatedAt: provider.CreatedAt, UpdatedAt: provider.UpdatedAt}
	if err := a.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	// A health refresh/cosmetic rename after queueing changes the ETag but
	// leaves the captured execution configuration valid.
	if err := a.db.Model(&provider).Updates(map[string]any{"version": 2, "name": "Renamed during queue"}).Error; err != nil {
		t.Fatal(err)
	}
	blocking := &blockingEvaluationClient{started: make(chan struct{})}
	a.harnessClient = blocking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { a.runAIEvaluation(ctx, run.ID); close(done) }()
	select {
	case <-blocking.started:
	case <-time.After(2 * time.Second):
		t.Fatal("evaluation not started")
	}
	locked := make(chan struct{})
	go func() {
		a.maintenance.Lock()
		a.maintenance.Unlock()
		a.aiProviderMu.Lock()
		a.aiProviderMu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("evaluation network holds a global lock")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("evaluation cancellation blocked")
	}
	if err := a.db.First(&run, "id=?", run.ID).Error; err != nil || run.Status != "cancelled" {
		t.Fatalf("cancelled run=%#v %v", run, err)
	}
}
