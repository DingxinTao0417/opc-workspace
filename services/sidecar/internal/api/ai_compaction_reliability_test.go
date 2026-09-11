package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/modelclient"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func seedOversizedCompaction(t *testing.T, a *API) (string, string, string) {
	t.Helper()
	sessionID := uuid.NewString()
	base := time.Now().UTC().Add(-time.Hour)
	if err := a.db.Create(&models.AISession{ID: sessionID, Title: "large", Persist: true, Version: 1, CreatedAt: base.Format(time.RFC3339Nano), UpdatedAt: base.Format(time.RFC3339Nano)}).Error; err != nil {
		t.Fatal(err)
	}
	largeID := uuid.NewString()
	large := strings.Repeat("中文\\\"", 11000)
	for i := 0; i < 150; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id := uuid.NewString()
		content := strings.Repeat("x", 1100)
		if i == 1 {
			id = largeID
			content = large
		}
		stamp := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		if err := a.db.Create(&models.AIMessage{ID: id, SessionID: sessionID, Role: role, Status: "completed", Content: content, CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return sessionID, largeID, large
}

func TestOversizedCompactionMakesBoundedLosslessProgress(t *testing.T) {
	_, a := newAIRuntimeLockTest(t)
	sessionID, largeID, large := seedOversizedCompaction(t, a)
	var previous *models.AIMemoryEntry
	var reconstructed strings.Builder
	parts := 0
	for rounds := 0; rounds < 20; rounds++ {
		candidate, err := a.buildAICompactionCandidate(sessionID, "openai_chat", "test", modelclient.PromptContext{}, previous)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidate.Messages) == 0 {
			t.Fatalf("no progress at round %d", rounds)
		}
		if candidate.InputBytes > aiCompactionBatchBytes {
			t.Fatalf("oversized input=%d", candidate.InputBytes)
		}
		for _, m := range candidate.Messages {
			if m.Role == "assistant" && strings.Contains(m.Content, "中文") {
				reconstructed.WriteString(m.Content)
				parts++
			}
		}
		if err := a.persistAIContextSnapshot(context.Background(), sessionID, previous, aiContextSnapshot{Summary: "compressed", Facts: []aiContextFact{}}, candidate); err != nil {
			t.Fatal(err)
		}
		previous, _, err = a.activeAIContextSnapshot(sessionID)
		if err != nil {
			t.Fatal(err)
		}
		counts, err := a.aiSessionCompactedMessageCounts(context.Background(), []models.AISession{{ID: sessionID}})
		if err != nil {
			t.Fatal(err)
		}
		if previous.SourceMessageID != nil && *previous.SourceMessageID == largeID && previous.SourceMessageOffset > 0 && counts[sessionID] != 1 {
			t.Fatalf("partial message incorrectly counted: %v", counts)
		}
		if reconstructed.Len() >= len(large) {
			break
		}
	}
	if parts < 2 || reconstructed.String() != large {
		t.Fatalf("segments=%d actual=%d expected=%d", parts, reconstructed.Len(), len(large))
	}
}

type compactionBlockClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *compactionBlockClient) Stream(ctx context.Context, request harness.Request, _ func(string), _ func(string)) (harness.Turn, error) {
	close(c.started)
	select {
	case <-ctx.Done():
		return harness.Turn{}, ctx.Err()
	case <-c.release:
	}
	return harness.Turn{Text: `{"summary":"snapshot","facts":[]}`}, nil
}

func TestCompactionReleasesLocksAndRechecksSemanticProviderIdentity(t *testing.T) {
	for _, change := range []string{"health", "configuration", "cancel"} {
		t.Run(change, func(t *testing.T) {
			r, a := newAIRuntimeLockTest(t)
			r.GET("/api/v1/ai/sessions/:id/compaction", a.getAICompactionStatus)
			sessionID, _, _ := seedOversizedCompaction(t, a)
			a.aiCompactions = newAICompactionRegistry()
			defer a.aiCompactions.close()
			stamp := time.Now().UTC().Format(time.RFC3339Nano)
			provider := models.AIProvider{ID: uuid.NewString(), Name: "compact", Kind: "local", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:1", Model: "test", Status: "ready", HealthStatus: "healthy", LastHealthAt: &stamp, Version: 1, ConfigVersion: 1, CreatedAt: stamp, UpdatedAt: stamp}
			if err := a.db.Create(&provider).Error; err != nil {
				t.Fatal(err)
			}
			client := &compactionBlockClient{started: make(chan struct{}), release: make(chan struct{})}
			a.harnessClient = client
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- a.compactAISession(ctx, sessionID, provider) }()
			select {
			case <-client.started:
			case <-time.After(2 * time.Second):
				t.Fatal("model not started")
			}
			unlocked := make(chan struct{})
			go func() {
				a.maintenance.Lock()
				a.maintenance.Unlock()
				a.aiProviderMu.Lock()
				defer a.aiProviderMu.Unlock()
				updates := map[string]any{"version": gorm.Expr("version + 1")}
				if change == "configuration" {
					updates["model"] = "changed"
					updates["config_version"] = 2
				}
				if change != "cancel" {
					if err := a.db.Model(&models.AIProvider{}).Where("id = ?", provider.ID).Updates(updates).Error; err != nil {
						t.Errorf("provider update: %v", err)
					}
				}
				close(unlocked)
			}()
			select {
			case <-unlocked:
			case <-time.After(250 * time.Millisecond):
				cancel()
				<-done
				t.Fatal("compaction held a lock over network wait")
			}
			if change == "cancel" {
				cancel()
			} else {
				close(client.release)
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(time.Second):
				t.Fatal("compaction did not finish")
			}
			var count int64
			a.db.Model(&models.AIMemoryEntry{}).Where("session_id = ?", sessionID).Count(&count)
			if change == "health" && (err != nil || count != 1) {
				t.Fatalf("health invalidated compaction: %v count=%d", err, count)
			}
			if change == "configuration" && (!errors.Is(err, errAICompactionProviderChanged) || count != 0) {
				t.Fatalf("changed config persisted: %v count=%d", err, count)
			}
			if change == "cancel" && (!errors.Is(err, context.Canceled) || count != 0) {
				t.Fatalf("cancel persisted: %v count=%d", err, count)
			}
			status := performRequest(r, http.MethodGet, "/api/v1/ai/sessions/"+sessionID+"/compaction", nil, nil)
			want := `"status":"succeeded"`
			if change == "configuration" {
				want = `"status":"failed"`
			}
			if change == "cancel" {
				want = `"status":"cancelled"`
			}
			if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), want) {
				t.Fatalf("status=%s", status.Body.String())
			}
		})
	}
}

func TestPartialCompactionFailureRetriesExactlyRemainingBytes(t *testing.T) {
	_, a := newAIRuntimeLockTest(t)
	sessionID, _, _ := seedOversizedCompaction(t, a)
	candidate, err := a.buildAICompactionCandidate(sessionID, "openai_chat", "test", modelclient.PromptContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.persistAIContextSnapshot(context.Background(), sessionID, nil, aiContextSnapshot{Summary: "first", Facts: []aiContextFact{}}, candidate); err != nil {
		t.Fatal(err)
	}
	previous, _, err := a.activeAIContextSnapshot(sessionID)
	if err != nil || previous.SourceMessageOffset == 0 {
		t.Fatalf("no partial watermark %v %v", previous, err)
	}
	next, err := a.buildAICompactionCandidate(sessionID, "openai_chat", "test", modelclient.PromptContext{}, previous)
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.persistAIContextSnapshot(cancelCtx, sessionID, previous, aiContextSnapshot{Summary: "must not replace", Facts: []aiContextFact{}}, next); err == nil {
		t.Fatal("cancelled snapshot persisted")
	}
	after, _, err := a.activeAIContextSnapshot(sessionID)
	if err != nil || after.ID != previous.ID {
		t.Fatalf("failed commit changed watermark: %+v %v", after, err)
	}
	retry, err := a.buildAICompactionCandidate(sessionID, "openai_chat", "test", modelclient.PromptContext{}, after)
	if err != nil {
		t.Fatal(err)
	}
	if buildAICompactionInput(previous, next) != buildAICompactionInput(after, retry) {
		t.Fatal("retry skipped or duplicated source bytes")
	}
	if err := a.persistAIContextSnapshot(context.Background(), sessionID, after, aiContextSnapshot{Summary: "second", Facts: []aiContextFact{}}, retry); err != nil {
		t.Fatal(err)
	}
}

func TestPartialCompactionContinuesWhenSourceReentersLiveWindow(t *testing.T) {
	_, a := newAIRuntimeLockTest(t)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	sessionID := uuid.NewString()
	sourceID := uuid.NewString()
	if err := a.db.Create(&models.AISession{ID: sessionID, Title: "window", Persist: true, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&models.AIMessage{ID: sourceID, SessionID: sessionID, Role: "user", Status: "completed", Content: strings.Repeat("x", 40000), CreatedAt: stamp, UpdatedAt: stamp}).Error; err != nil {
		t.Fatal(err)
	}
	active := &models.AIMemoryEntry{SourceMessageID: &sourceID, SourceMessageOffset: 30000, Content: `{"summary":"shorter","facts":[]}`}
	candidate, err := a.buildAICompactionCandidate(sessionID, "openai_chat", "test", modelclient.PromptContext{}, active)
	if err != nil || len(candidate.Messages) != 1 || len(candidate.Messages[0].Content) != 10000 || candidate.SourceMessageOffset != 0 {
		t.Fatalf("stranded partial source candidate=%+v err=%v", candidate, err)
	}
}
