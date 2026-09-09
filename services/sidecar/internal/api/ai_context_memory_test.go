package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type queuedAICompactionClient struct {
	mu        sync.Mutex
	responses []string
	requests  []harness.Request
}

func (c *queuedAICompactionClient) Stream(_ context.Context, request harness.Request, _ func(string), _ func(string)) (harness.Turn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return harness.Turn{}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return harness.Turn{Text: response}, nil
}

func TestDecodeAIContextSnapshotEnforcesShapeAndBudgets(t *testing.T) {
	valid := `{"summary":"用户准备发布本地版本","facts":[{"kind":"decision","content":"先完成离线流程"}]}`
	snapshot, err := decodeAIContextSnapshot(valid)
	if err != nil {
		t.Fatalf("decode valid snapshot: %v", err)
	}
	if snapshot.Summary != "用户准备发布本地版本" || len(snapshot.Facts) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for name, input := range map[string]string{
		"unknown field":  `{"summary":"x","facts":[],"extra":true}`,
		"unknown kind":   `{"summary":"x","facts":[{"kind":"guess","content":"y"}]}`,
		"empty":          `{"summary":"","facts":[]}`,
		"trailing":       `{"summary":"x","facts":[]} trailing`,
		"summary budget": `{"summary":"` + strings.Repeat("x", aiContextSummaryBudget+1) + `","facts":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAIContextSnapshot(input); err == nil {
				t.Fatalf("invalid snapshot accepted: %s", input)
			}
		})
	}
}

func TestAICompactionCreatesAndSupersedesSnapshotsWithSanitizedEvents(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-compaction.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	const sessionID = "018f0000-0000-7000-8000-000000005711"
	if err := store.DB.Create(&models.AISession{
		ID: sessionID, Title: "long conversation", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for index := 0; index < 100; index++ {
		for _, role := range []string{"user", "assistant"} {
			now = now.Add(time.Second)
			content := role + "-" + strings.Repeat(string(rune('a'+index%20)), 1024)
			if role == "assistant" && index == 0 {
				content += ` [opc:task]{"title":"must not enter compaction"}[/opc:task]`
			}
			if err := store.DB.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: sessionID, Role: role, Status: "completed",
				Content: content, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
			}).Error; err != nil {
				t.Fatalf("create %s message %d: %v", role, index, err)
			}
		}
	}

	client := &queuedAICompactionClient{responses: []string{
		`{"summary":"用户在规划离线发布","facts":[{"kind":"decision","content":"先完成本地能力"}]}`,
		`{"summary":"用户继续规划离线发布","facts":[{"kind":"decision","content":"先完成本地能力"},{"kind":"progress","content":"上下文压缩已开始"}]}`,
	}}
	service := &API{
		db:       store.DB,
		options:  Options{Now: func() time.Time { return now }, Logger: log.New(io.Discard, "", 0)},
		keyStore: keystore.NewMemoryStore(), harnessClient: client, maintenance: &sync.RWMutex{},
	}
	provider := createCompactionTestProvider(t, store, now, "018f0000-0000-7000-8000-000000005712")
	if err := service.compactAISession(context.Background(), sessionID, provider); err != nil {
		t.Fatalf("first compactAISession: %v", err)
	}
	now = now.Add(time.Second)
	if err := service.compactAISession(context.Background(), sessionID, provider); err != nil {
		t.Fatalf("second compactAISession: %v", err)
	}

	var entries []models.AIMemoryEntry
	if err := store.DB.Where("session_id = ?", sessionID).Order("created_at, id").Find(&entries).Error; err != nil {
		t.Fatalf("read snapshots: %v", err)
	}
	if len(entries) != 2 || entries[0].Status != "superseded" || entries[1].Status != "active" {
		t.Fatalf("snapshot lifecycle = %#v", entries)
	}
	if entries[0].Content == entries[1].Content || entries[0].SourceMessageID == nil || entries[1].SourceMessageID == nil ||
		*entries[0].SourceMessageID == *entries[1].SourceMessageID {
		t.Fatalf("replacement snapshot = %#v", entries[1])
	}
	var eventCount int64
	if err := store.DB.Table("workflow_events").
		Where("aggregate_type = 'ai_session' AND aggregate_id = ? AND action = 'ai_session_compacted'", sessionID).
		Count(&eventCount).Error; err != nil || eventCount != 2 {
		t.Fatalf("compaction events=%d err=%v", eventCount, err)
	}
	var leaked int64
	if err := store.DB.Table("workflow_events").
		Where("aggregate_id = ? AND current_json LIKE ?", sessionID, "%离线发布%").Count(&leaked).Error; err != nil || leaked != 0 {
		t.Fatalf("snapshot content leaked into events=%d err=%v", leaked, err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("compaction requests=%d, want 2", len(client.requests))
	}
	for _, request := range client.requests {
		if request.SystemPrompt != aiCompactionSystemPrompt || len(request.History) != 1 {
			t.Fatalf("compaction request = %#v", request)
		}
		if request.Protocol != provider.Protocol || request.BaseURL != provider.BaseURL || request.Model != provider.Model {
			t.Fatalf("compaction changed provider identity: %#v", request)
		}
		if strings.Contains(request.History[0].Content, "opc:task") {
			t.Fatalf("control block leaked into compaction input: %s", request.History[0].Content)
		}
	}
	if !strings.Contains(client.requests[1].History[0].Content, "用户在规划离线发布") {
		t.Fatal("second compaction did not carry the previous snapshot")
	}

	active, snapshot, err := service.activeAIContextSnapshot(sessionID)
	if err != nil || active == nil {
		t.Fatalf("load active context: active=%#v err=%v", active, err)
	}
	promptContext := promptContextFromSnapshot([]string{"回答简洁"}, snapshot)
	if promptContext.Summary != "用户继续规划离线发布" || len(promptContext.Facts) != 2 || len(promptContext.Memories) != 1 {
		t.Fatalf("prompt context = %#v", promptContext)
	}
	counts, err := service.aiSessionCompactedMessageCounts(context.Background(), []models.AISession{{ID: sessionID}})
	if err != nil || counts[sessionID] <= 0 {
		t.Fatalf("compacted message count=%d err=%v", counts[sessionID], err)
	}
}

func TestAICompactionInvalidOutputLeavesWatermarkAndEventsUntouched(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-compaction-invalid.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	const sessionID = "018f0000-0000-7000-8000-000000005721"
	if err := store.DB.Create(&models.AISession{ID: sessionID, Title: "invalid output", Persist: true, Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for index := 0; index < 80; index++ {
		for _, role := range []string{"user", "assistant"} {
			now = now.Add(time.Second)
			if err := store.DB.Create(&models.AIMessage{
				ID: uuid.NewString(), SessionID: sessionID, Role: role, Status: "completed",
				Content: strings.Repeat("z", 1024), CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
			}).Error; err != nil {
				t.Fatalf("create message: %v", err)
			}
		}
	}
	client := &queuedAICompactionClient{responses: []string{"```json\n{}\n```"}}
	service := &API{
		db:       store.DB,
		options:  Options{Now: func() time.Time { return now }, Logger: log.New(io.Discard, "", 0)},
		keyStore: keystore.NewMemoryStore(), harnessClient: client, maintenance: &sync.RWMutex{},
	}
	provider := createCompactionTestProvider(t, store, now, "018f0000-0000-7000-8000-000000005722")
	err = service.compactAISession(context.Background(), sessionID, provider)
	if err == nil {
		t.Fatal("invalid compaction output was accepted")
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_memory_entries", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'ai_session_compacted'", 0)
}

func TestAIChatInjectsLatestActiveSummaryAndFacts(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-context-injection.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC)
	client := &queuedAICompactionClient{responses: []string{"继续处理"}}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Now: func() time.Time { return now }, Logger: log.New(io.Discard, "", 0),
		KeyStore: keystore.NewMemoryStore(), HarnessClient: client,
		FocusHeartbeatInterval: -1, ReminderScanInterval: -1,
		AutomationDeliveryScanInterval: -1, DiskSpaceScanInterval: -1,
		ScheduledBackupScanInterval: -1,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	const (
		providerID = "018f0000-0000-7000-8000-000000005731"
		sessionID  = "018f0000-0000-7000-8000-000000005732"
		messageID  = "018f0000-0000-7000-8000-000000005733"
	)
	if err := store.DB.Create(&models.AIProvider{
		ID: providerID, Name: "local", Kind: aiProviderKindLocal, Protocol: "openai_chat",
		BaseURL: "http://127.0.0.1:11434/v1", Model: "local-test", Status: "ready",
		HealthStatus: "healthy", HasKey: false, LastHealthAt: aiStringPtr(now.Format(time.RFC3339Nano)), Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := store.DB.Create(&models.AISession{
		ID: sessionID, Title: "context session", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DB.Create(&models.AIMessage{
		ID: messageID, SessionID: sessionID, Role: "assistant", Status: "completed", Content: "older answer",
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create source message: %v", err)
	}
	snapshot := aiContextSnapshot{
		Summary: "用户正在准备本地发布",
		Facts:   []aiContextFact{{Kind: "decision", Content: "先完成离线闭环"}},
	}
	content, _ := json.Marshal(snapshot)
	if err := store.DB.Create(&models.AIMemoryEntry{
		ID: uuid.NewString(), SessionID: sessionID, Kind: "context_snapshot", Content: string(content),
		Tags: `["decision"]`, Origin: "model_compaction", Status: "active", SourceMessageID: aiStringPtr(messageID),
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create context snapshot: %v", err)
	}
	if err := store.DB.Create(&models.AIMemory{
		ID: uuid.NewString(), Content: "回答保持简洁",
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create confirmed memory: %v", err)
	}

	response := chatRequest(t, router, providerID, sessionID, "继续")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat response = %d: %s", response.Code, response.Body.String())
	}
	if err := router.Close(); err != nil {
		t.Fatalf("router.Close: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 1 {
		t.Fatalf("model requests=%d, want 1", len(client.requests))
	}
	request := client.requests[0]
	if request.Summary != snapshot.Summary || len(request.Facts) != 1 || len(request.Memories) != 1 || len(request.Tools) != 3 {
		t.Fatalf("injected request = %#v", request)
	}
}

func TestAICompactionRegistrySerializesSessionAndCancelsOnClose(t *testing.T) {
	registry := newAICompactionRegistry()
	started := make(chan struct{})
	finished := make(chan struct{})
	if !registry.start("session", func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("first compaction was rejected")
	}
	<-started
	if registry.start("session", func(context.Context) {}) {
		t.Fatal("concurrent compaction for one session was accepted")
	}
	registry.close()
	<-finished
}

func createCompactionTestProvider(t *testing.T, store *database.Store, now time.Time, id string) models.AIProvider {
	t.Helper()
	healthAt := now.Format(time.RFC3339Nano)
	provider := models.AIProvider{
		ID: id, Name: "compaction-" + id[len(id)-4:], Kind: aiProviderKindLocal,
		Protocol: "openai_chat", BaseURL: "http://127.0.0.1:11434/v1", Model: "local-test",
		Status: "ready", HealthStatus: "healthy", LastHealthAt: &healthAt, Version: 1,
		CreatedAt: healthAt, UpdatedAt: healthAt,
	}
	if err := store.DB.Create(&provider).Error; err != nil {
		t.Fatalf("create compaction provider: %v", err)
	}
	return provider
}
