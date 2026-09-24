package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type progressModel struct {
	calls   int
	release chan struct{}
}

func (m *progressModel) Stream(ctx context.Context, _ harness.Request, delta, _ func(string)) (harness.Turn, error) {
	m.calls++
	if m.calls == 1 {
		return harness.Turn{ToolCalls: []harness.ToolCall{{ID: "private-call-id", Name: "memory_search", Arguments: json.RawMessage(`{"query":"private-query"}`)}}}, nil
	}
	select {
	case <-ctx.Done():
		return harness.Turn{}, ctx.Err()
	case <-m.release:
	}
	answer := `private-answer[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`
	if delta != nil {
		delta(answer)
	}
	return harness.Turn{Text: answer}, nil
}

func TestAIRunProgressReportsModelTurnRetries(t *testing.T) {
	started := time.Now().UTC()
	step := harness.RunStep{
		Kind: "model_turn", Status: "succeeded", TurnIndex: 1,
		StartedAt: started, CompletedAt: started.Add(2 * time.Second), DurationMS: 2000,
		RetryCount: 2, RetryReason: "upstream_503",
	}
	progress := aiProgressFromStep(2, step)
	if progress.RetryCount != 2 || progress.RetryReason != "upstream_503" || progress.TurnIndex != 1 {
		t.Fatalf("progress=%#v", progress)
	}
	// Retry facts belong to the model/self-check turn only: tool and
	// persistence steps never claim them.
	tool := aiProgressFromStep(3, harness.RunStep{
		Kind: "tool_call", Status: "succeeded", ToolName: "memory_search", StartedAt: started,
		RetryCount: 2, RetryReason: "upstream_503",
	})
	if tool.RetryCount != 0 || tool.RetryReason != "" {
		t.Fatalf("tool progress=%#v", tool)
	}
}

func TestAIRunProgressStreamsBeforeModelFinishesAndRecovers(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRun), func(t *testing.T) {
			r, a := newAIRuntimeLockTest(t)
			model := &progressModel{release: make(chan struct{})}
			a.harnessClient = model
			now := nowStamp(a)
			provider := models.AIProvider{ID: uuid.NewString(), Name: "progress", Kind: "local", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:1", Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &now, Version: 1, CreatedAt: now, UpdatedAt: now}
			session := models.AISession{ID: uuid.NewString(), Title: "ephemeral", Persist: false, Version: 1, CreatedAt: now, UpdatedAt: now}
			for _, row := range []any{&provider, &session} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(r)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/ai/chat", strings.NewReader(fmt.Sprintf(`{"provider_id":%q,"session_id":%q,"message":"private-prompt"}`, provider.ID, session.ID)))
			req.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal(response.StatusCode)
			}
			scanner := bufio.NewScanner(response.Body)
			generationID, event := "", ""
			var events []aiRunProgress
			readEvent := func() string {
				t.Helper()
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "event: ") {
						event = strings.TrimPrefix(line, "event: ")
					}
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					data := strings.TrimPrefix(line, "data: ")
					var payload struct {
						GenerationID string        `json:"generation_id"`
						Step         aiRunProgress `json:"step"`
					}
					if err := json.Unmarshal([]byte(data), &payload); err != nil {
						t.Fatal(err)
					}
					if generationID == "" {
						generationID = payload.GenerationID
					}
					if generationID != payload.GenerationID {
						t.Fatal("wrong generation")
					}
					if event == "progress" {
						if strings.Contains(data, "private") || strings.Contains(data, "arguments") {
							t.Fatal("progress leaked payload", data)
						}
						events = append(events, payload.Step)
					}
					return event
				}
				t.Fatalf("stream ended early: %v", scanner.Err())
				return ""
			}
			for len(events) < 5 {
				readEvent()
			}
			if events[0].Kind != "model_turn" || events[0].Status != "running" || events[2].ToolName != "memory_search" || events[3].Status != "succeeded" || events[4].TurnIndex != 2 || events[4].Status != "running" {
				t.Fatalf("bad order: %+v", events)
			}
			get := func() aiGenerationResponse {
				t.Helper()
				res := performRequest(r, http.MethodGet, "/api/v1/ai/generations/"+generationID, nil, nil)
				var body struct {
					Data aiGenerationResponse `json:"data"`
				}
				body.Data.aiGenerationCitations = &aiGenerationCitations{}
				if res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(res.Body.Bytes(), &body) != nil {
					t.Fatal(res.Code, res.Body.String())
				}
				return body.Data
			}
			active := get()
			if len(active.Progress) != 3 || active.Progress[2] != events[4] {
				t.Fatalf("active recovery: %+v", active)
			}
			var count int64
			a.db.Model(&models.AIRunStep{}).Where("generation_id = ?", generationID).Count(&count)
			if count != 1 {
				t.Fatal("live details written prematurely", count)
			}
			if cancelRun {
				res := performRequest(r, http.MethodPost, "/api/v1/ai/generations/"+generationID+"/cancel", nil, nil)
				if res.Code != 200 && res.Code != 202 {
					t.Fatal(res.Code, res.Body.String())
				}
			} else {
				close(model.release)
			}
			for {
				kind := readEvent()
				if kind == "done" || kind == "cancelled" || kind == "error" {
					break
				}
			}
			terminal := get()
			if terminal.Content != "" || terminal.Reasoning != "" || len(terminal.Progress) < 4 {
				t.Fatalf("terminal recovery: %+v", terminal)
			}
			for _, step := range terminal.Progress {
				if step.Status == "running" {
					t.Fatal("stale running", step)
				}
			}
			if cancelRun {
				if terminal.Status != "cancelled" || terminal.Progress[2].Status != "cancelled" {
					t.Fatal(terminal)
				}
			} else {
				var collapsed []aiRunProgress
				for _, step := range events {
					if step.Status != "running" {
						collapsed = append(collapsed, step)
					}
				}
				if terminal.Status != "completed" || !reflect.DeepEqual(collapsed, terminal.Progress) {
					t.Fatalf("SSE and durable recovery differ: %+v / %+v", collapsed, terminal.Progress)
				}
			}
			a.db.Model(&models.AIMessage{}).Count(&count)
			if count != 0 {
				t.Fatal("ephemeral body persisted")
			}
		})
	}
}

func TestAIRunProgressSnapshotIsolationAndBounds(t *testing.T) {
	r := newAIGenerationRegistry()
	r.setSnapshot(aiGenerationResponse{ID: "g"})
	start := harness.RunStep{Kind: "tool_call", ToolName: "private-old-model-name", Status: "running", StartedAt: time.Now()}
	step := aiProgressFromStep(2, start)
	if step.ToolName != "unknown_tool" || !r.setProgress("g", step) {
		t.Fatal(step)
	}
	snapshot, _ := r.snapshot("g")
	snapshot.Progress[0].ToolName = "private-mutated"
	if copy, _ := r.snapshot("g"); copy.Progress[0].ToolName != "unknown_tool" {
		t.Fatal("aliased snapshot")
	}
	if r.setProgress("g", step) {
		t.Fatal("duplicate start accepted")
	}
	step.Status, step.CompletedAt = "succeeded", step.StartedAt
	if !r.setProgress("g", step) {
		t.Fatal("terminal rejected")
	}
	for seq := 3; seq <= 64; seq++ {
		step.Sequence = seq
		if !r.setProgress("g", step) {
			t.Fatal(seq)
		}
	}
	step.Sequence = 65
	if r.setProgress("g", step) {
		t.Fatal("unbounded progress")
	}
	r.release("g")
	if _, ok := r.snapshot("g"); ok {
		t.Fatal("released snapshot retained")
	}
}
