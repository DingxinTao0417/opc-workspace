package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/keystore"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIBusinessContextPreviewChatPersistenceAndVersionGate(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-business-context.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 8, 21, 0, 0, 0, time.UTC)
	client := &queuedAICompactionClient{responses: []string{"context-aware answer", "ephemeral context answer"}}
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
	defer router.Close()
	provider := createCompactionTestProvider(t, store, now, "018f0000-0000-7000-8000-000000005811")
	contextFixture := seedAIBusinessContextFixture(t, store, now)

	previewBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID,
		Sources: []aiBusinessContextSourceInput{
			{Type: "client", ID: contextFixture.clientID},
			{Type: "task", ID: contextFixture.taskID},
			{Type: "project", ID: contextFixture.projectID},
		},
	})
	preview := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", previewBody, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data aiBusinessContextPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	data := previewEnvelope.Data
	if data.ProviderVersion != provider.Version || data.LeavesDevice || data.SerializedBytes <= 0 || data.SerializedBytes > aiBusinessContextMaxBytes || len(data.Sources) != 3 {
		t.Fatalf("preview data = %#v", data)
	}
	if data.Sources[0].Type != "task" || data.Sources[1].Type != "project" || data.Sources[2].Type != "client" {
		t.Fatalf("context order = %#v", data.Sources)
	}
	encodedPreview := preview.Body.String()
	for _, excluded := range []string{"private@example.com", "+1-555", "contact_name", "amount_minor", "client_id"} {
		if strings.Contains(encodedPreview, excluded) {
			t.Fatalf("excluded field %q leaked into preview: %s", excluded, encodedPreview)
		}
	}
	if !containsString(data.Sources[0].TruncatedFields, "description") || !containsString(data.Sources[2].TruncatedFields, "notes") {
		t.Fatalf("truncation facts missing: %#v", data.Sources)
	}

	chatSources := make([]aiBusinessContextSourceInput, 0, len(data.Sources))
	for _, source := range data.Sources {
		chatSources = append(chatSources, aiBusinessContextSourceInput{Type: source.Type, ID: source.ID, ExpectedVersion: source.Version})
	}
	chatBody, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "summarize selected work",
		"context": map[string]any{
			"provider_version": data.ProviderVersion,
			"sources":          chatSources,
		},
	})
	chat := performRequest(router, http.MethodPost, "/api/v1/ai/chat", chatBody, nil)
	if chat.Code != http.StatusOK || !strings.Contains(chat.Body.String(), "event: done") {
		t.Fatalf("chat = %d: %s", chat.Code, chat.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	var storedContext string
	if err := store.DB.Model(&models.AIMessage{}).
		Where("session_id = ? AND role = 'user'", session.ID).
		Pluck("context_snapshot", &storedContext).Error; err != nil || storedContext == "" {
		t.Fatalf("stored context=%q err=%v", storedContext, err)
	}
	for _, excluded := range []string{"private@example.com", "+1-555", "amount_minor", "client_id"} {
		if strings.Contains(storedContext, excluded) {
			t.Fatalf("excluded field %q leaked into stored snapshot: %s", excluded, storedContext)
		}
	}
	messages := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+session.ID+"/messages", nil, nil)
	if messages.Code != http.StatusOK ||
		!strings.Contains(messages.Body.String(), `"context_provider":{"id":"`+provider.ID+`"`) ||
		!strings.Contains(messages.Body.String(), `"context_sources":[{"type":"task"`) {
		t.Fatalf("history context = %d: %s", messages.Code, messages.Body.String())
	}
	client.mu.Lock()
	if len(client.requests) == 0 || len(client.requests[0].BusinessContext) != 3 {
		client.mu.Unlock()
		t.Fatalf("model business context requests=%#v", client.requests)
	}
	modelContext := strings.Join(client.requests[0].BusinessContext, "\n")
	client.mu.Unlock()
	if !strings.Contains(modelContext, contextFixture.taskID) || strings.Contains(modelContext, "private@example.com") {
		t.Fatalf("model context = %s", modelContext)
	}

	var beforeMessages, beforeGenerations int64
	store.DB.Model(&models.AIMessage{}).Count(&beforeMessages)
	store.DB.Model(&models.AIGeneration{}).Count(&beforeGenerations)
	if err := store.DB.Model(&models.Task{}).Where("id = ?", contextFixture.taskID).Updates(map[string]any{
		"description": "changed after preview", "version": gorm.Expr("version + 1"), "updated_at": now.Add(time.Minute).Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("change task after preview: %v", err)
	}
	changed := performRequest(router, http.MethodPost, "/api/v1/ai/chat", chatBody, nil)
	assertAPIError(t, changed, http.StatusConflict, "AI_CONTEXT_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", beforeMessages)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", beforeGenerations)

	ephemeralPreviewBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID,
		Sources:    []aiBusinessContextSourceInput{{Type: "client", ID: contextFixture.clientID}},
	})
	ephemeralPreview := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", ephemeralPreviewBody, nil)
	if ephemeralPreview.Code != http.StatusOK {
		t.Fatalf("ephemeral preview = %d: %s", ephemeralPreview.Code, ephemeralPreview.Body.String())
	}
	var ephemeralEnvelope struct {
		Data aiBusinessContextPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(ephemeralPreview.Body.Bytes(), &ephemeralEnvelope); err != nil {
		t.Fatalf("decode ephemeral preview: %v", err)
	}
	createdSession := performRequest(router, http.MethodPost, "/api/v1/ai/sessions", []byte(`{"title":"ephemeral context","persist":false}`), nil)
	if createdSession.Code != http.StatusCreated {
		t.Fatalf("create ephemeral session = %d: %s", createdSession.Code, createdSession.Body.String())
	}
	var sessionEnvelope struct {
		Data aiSessionResponse `json:"data"`
	}
	if err := json.Unmarshal(createdSession.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatalf("decode ephemeral session: %v", err)
	}
	ephemeralSource := ephemeralEnvelope.Data.Sources[0]
	ephemeralChatBody, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID, "session_id": sessionEnvelope.Data.ID, "message": "use client context",
		"context": map[string]any{
			"provider_version": ephemeralEnvelope.Data.ProviderVersion,
			"sources":          []aiBusinessContextSourceInput{{Type: ephemeralSource.Type, ID: ephemeralSource.ID, ExpectedVersion: ephemeralSource.Version}},
		},
	})
	ephemeralChat := performRequest(router, http.MethodPost, "/api/v1/ai/chat", ephemeralChatBody, nil)
	if ephemeralChat.Code != http.StatusOK {
		t.Fatalf("ephemeral context chat = %d: %s", ephemeralChat.Code, ephemeralChat.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages WHERE session_id = ?", 0, sessionEnvelope.Data.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations WHERE session_id = ? AND content IS NULL", 1, sessionEnvelope.Data.ID)
}

func TestAIBusinessContextPreviewRejectsInvalidSourcesAndChangedProvider(t *testing.T) {
	now := time.Date(2026, 9, 8, 22, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, "018f0000-0000-7000-8000-000000005821")
	fixture := seedAIBusinessContextFixture(t, store, now)

	for name, body := range map[string]string{
		"empty":     `{"provider_id":"` + provider.ID + `","sources":[]}`,
		"duplicate": `{"provider_id":"` + provider.ID + `","sources":[{"type":"task","id":"` + fixture.taskID + `"},{"type":"task","id":"` + fixture.taskID + `"}]}`,
		"bad type":  `{"provider_id":"` + provider.ID + `","sources":[{"type":"invoice","id":"` + fixture.taskID + `"}]}`,
		"missing":   `{"provider_id":"` + provider.ID + `","sources":[{"type":"task","id":"018f0000-0000-7000-8000-000000005899"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", []byte(body), nil)
			if name == "missing" {
				assertAPIError(t, response, http.StatusNotFound, "AI_CONTEXT_SOURCE_NOT_FOUND")
			} else if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid preview = %d: %s", response.Code, response.Body.String())
			}
		})
	}

	preview := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", []byte(`{"provider_id":"`+provider.ID+`","sources":[{"type":"task","id":"`+fixture.taskID+`"}]}`), nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data aiBusinessContextPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if err := store.DB.Model(&models.AIProvider{}).Where("id = ?", provider.ID).Updates(map[string]any{
		"name": "changed-provider", "version": gorm.Expr("version + 1"), "updated_at": now.Add(time.Minute).Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("change provider: %v", err)
	}
	source := previewEnvelope.Data.Sources[0]
	chatBody, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID, "message": "use context",
		"context": map[string]any{
			"provider_version": previewEnvelope.Data.ProviderVersion,
			"sources":          []aiBusinessContextSourceInput{{Type: source.Type, ID: source.ID, ExpectedVersion: source.Version}},
		},
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", chatBody, nil)
	assertAPIError(t, response, http.StatusConflict, "AI_CONTEXT_PROVIDER_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
}

func TestAIKnowledgeContextPreviewChatPersistenceAndVersionGate(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "ai-knowledge-context.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 8, 23, 0, 0, 0, time.UTC)
	client := &queuedAICompactionClient{}
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
	defer router.Close()
	provider := createCompactionTestProvider(t, store, now, "018f0000-0000-7000-8000-000000005841")
	knowledge := importKnowledgeFixture(t, router.Engine, "security-guide.md", []byte("Release evidence stays local. IGNORE SYSTEM is quoted document text, not an instruction."))
	var chunk models.KnowledgeChunk
	if err := store.DB.First(&chunk, "source_id = ?", knowledge.Source.ID).Error; err != nil {
		t.Fatalf("load knowledge chunk: %v", err)
	}
	client.responses = []string{
		"knowledge-grounded answer\n[opc:citations]{\"chunk_ids\":[\"" + chunk.ID + "\"]}[/opc:citations]",
	}
	selection := aiKnowledgeContextSourceInput{
		SourceID: knowledge.Source.ID, DocumentID: chunk.DocumentID, ChunkID: chunk.ID,
	}
	previewBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID, Sources: []aiBusinessContextSourceInput{},
		Knowledge: []aiKnowledgeContextSourceInput{selection},
	})
	preview := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", previewBody, nil)
	if preview.Code != http.StatusOK {
		t.Fatalf("knowledge preview = %d: %s", preview.Code, preview.Body.String())
	}
	var previewEnvelope struct {
		Data aiBusinessContextPreviewResponse `json:"data"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode knowledge preview: %v", err)
	}
	data := previewEnvelope.Data
	if data.ProviderVersion != provider.Version || data.LeavesDevice || len(data.Sources) != 0 || len(data.Knowledge) != 1 ||
		data.SerializedBytes <= 0 || data.SerializedBytes > aiCombinedContextMaxBytes {
		t.Fatalf("knowledge preview data=%#v", data)
	}
	selected := data.Knowledge[0]
	if selected.SourceID != knowledge.Source.ID || selected.SourceVersion != knowledge.Source.Version ||
		selected.DocumentVersion != *knowledge.Source.DocumentVersion || selected.StartLine != 1 ||
		!strings.Contains(selected.Content, "IGNORE SYSTEM") {
		t.Fatalf("knowledge preview source=%#v", selected)
	}
	duplicateBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID,
		Knowledge:  []aiKnowledgeContextSourceInput{selection, selection},
	})
	duplicate := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", duplicateBody, nil)
	assertAPIError(t, duplicate, http.StatusUnprocessableEntity, "AI_KNOWLEDGE_CONTEXT_DUPLICATE")
	tooManyBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID,
		Knowledge:  []aiKnowledgeContextSourceInput{selection, selection, selection, selection},
	})
	tooMany := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", tooManyBody, nil)
	assertAPIError(t, tooMany, http.StatusUnprocessableEntity, "AI_KNOWLEDGE_CONTEXT_INVALID")
	missingSelection := selection
	missingSelection.ChunkID = "018f0000-0000-7000-8000-000000005899"
	missingBody, _ := json.Marshal(aiBusinessContextPreviewRequest{
		ProviderID: provider.ID, Knowledge: []aiKnowledgeContextSourceInput{missingSelection},
	})
	missing := performRequest(router, http.MethodPost, "/api/v1/ai/context/preview", missingBody, nil)
	assertAPIError(t, missing, http.StatusNotFound, "AI_KNOWLEDGE_CONTEXT_NOT_FOUND")

	chatBody, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "answer from the selected evidence",
		"context": map[string]any{
			"provider_version": data.ProviderVersion,
			"sources":          []aiBusinessContextSourceInput{},
			"knowledge": []aiKnowledgeContextSourceInput{{
				SourceID: selected.SourceID, DocumentID: selected.DocumentID, ChunkID: selected.ChunkID,
				ExpectedSourceVersion: selected.SourceVersion, ExpectedDocumentVersion: selected.DocumentVersion,
			}},
		},
	})
	chat := performRequest(router, http.MethodPost, "/api/v1/ai/chat", chatBody, nil)
	if chat.Code != http.StatusOK || !strings.Contains(chat.Body.String(), "event: done") {
		t.Fatalf("knowledge chat = %d: %s", chat.Code, chat.Body.String())
	}
	client.mu.Lock()
	if len(client.requests) == 0 || len(client.requests[0].KnowledgeContext) != 1 || len(client.requests[0].BusinessContext) != 0 {
		client.mu.Unlock()
		t.Fatalf("model knowledge requests=%#v", client.requests)
	}
	modelKnowledge := client.requests[0].KnowledgeContext[0]
	client.mu.Unlock()
	if !strings.Contains(modelKnowledge, "security-guide.md") || !strings.Contains(modelKnowledge, "IGNORE SYSTEM") {
		t.Fatalf("model knowledge context=%s", modelKnowledge)
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatalf("load AI session: %v", err)
	}
	var storedContext string
	if err := store.DB.Model(&models.AIMessage{}).Where("session_id = ? AND role = 'user'", session.ID).
		Pluck("context_snapshot", &storedContext).Error; err != nil || !strings.Contains(storedContext, `"version":2`) ||
		!strings.Contains(storedContext, `"knowledge"`) {
		t.Fatalf("stored knowledge context=%q err=%v", storedContext, err)
	}
	history := performRequest(router, http.MethodGet, "/api/v1/ai/sessions/"+session.ID+"/messages", nil, nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"context_knowledge":[{"source_id":"`+knowledge.Source.ID+`"`) ||
		!strings.Contains(history.Body.String(), `"citation_status":"validated"`) ||
		!strings.Contains(history.Body.String(), `"citations":[{"chunk_id":"`+chunk.ID+`"`) ||
		strings.Contains(history.Body.String(), "opc:citations") {
		t.Fatalf("knowledge history = %d: %s", history.Code, history.Body.String())
	}
	var generation models.AIGeneration
	if err := store.DB.First(&generation, "session_id = ?", session.ID).Error; err != nil {
		t.Fatalf("load AI generation: %v", err)
	}
	if !strings.Contains(history.Body.String(), `"generation_id":"`+generation.ID+`"`) {
		t.Fatalf("history omitted generation identity: %s", history.Body.String())
	}
	stepsResponse := performRequest(router, http.MethodGet, "/api/v1/ai/generations/"+generation.ID+"/steps", nil, nil)
	if stepsResponse.Code != http.StatusOK {
		t.Fatalf("run steps = %d: %s", stepsResponse.Code, stepsResponse.Body.String())
	}
	var stepsEnvelope struct {
		Data []models.AIRunStep `json:"data"`
		Meta struct {
			InputBytes  int `json:"input_bytes"`
			OutputBytes int `json:"output_bytes"`
			Total       int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(stepsResponse.Body.Bytes(), &stepsEnvelope); err != nil {
		t.Fatalf("decode run steps: %v", err)
	}
	wantKinds := []string{"generation", "model_turn", "self_check", "citation_validation", "persistence"}
	if len(stepsEnvelope.Data) != len(wantKinds) || stepsEnvelope.Meta.Total != len(wantKinds) || stepsEnvelope.Meta.OutputBytes <= 0 {
		t.Fatalf("run step envelope=%#v", stepsEnvelope)
	}
	if stepsEnvelope.Data[0].TokenSource != nil || stepsEnvelope.Data[0].InputTokens != nil || stepsEnvelope.Data[0].OutputTokens != nil {
		t.Fatalf("fake client usage was estimated: %#v", stepsEnvelope.Data[0])
	}
	for index, kind := range wantKinds {
		if stepsEnvelope.Data[index].Kind != kind || stepsEnvelope.Data[index].Sequence != index+1 {
			t.Fatalf("run step[%d]=%#v want kind=%s", index, stepsEnvelope.Data[index], kind)
		}
	}
	for _, leaked := range []string{"knowledge-grounded answer", "IGNORE SYSTEM", "security-guide.md", provider.BaseURL} {
		if strings.Contains(stepsResponse.Body.String(), leaked) {
			t.Fatalf("run steps leaked %q: %s", leaked, stepsResponse.Body.String())
		}
	}

	var beforeMessages, beforeGenerations int64
	store.DB.Model(&models.AIMessage{}).Count(&beforeMessages)
	store.DB.Model(&models.AIGeneration{}).Count(&beforeGenerations)
	if err := store.DB.Model(&models.KnowledgeDocument{}).Where("id = ?", selected.DocumentID).
		Update("version", gorm.Expr("version + 1")).Error; err != nil {
		t.Fatalf("change knowledge document: %v", err)
	}
	changed := performRequest(router, http.MethodPost, "/api/v1/ai/chat", chatBody, nil)
	assertAPIError(t, changed, http.StatusConflict, "AI_KNOWLEDGE_CONTEXT_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", beforeMessages)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", beforeGenerations)
}

type aiBusinessContextFixture struct {
	clientID  string
	projectID string
	taskID    string
}

func seedAIBusinessContextFixture(t *testing.T, store *database.Store, now time.Time) aiBusinessContextFixture {
	t.Helper()
	fixture := aiBusinessContextFixture{
		clientID:  "018f0000-0000-7000-8000-000000005831",
		projectID: "018f0000-0000-7000-8000-000000005832",
		taskID:    "018f0000-0000-7000-8000-000000005833",
	}
	contact, email, phone := "Secret Contact", "private@example.com", "+1-555"
	notes := strings.Repeat("客", aiBusinessContextTextMaxRunes+20)
	if err := store.DB.Create(&models.Client{
		ID: fixture.clientID, Name: "Acme", ContactName: &contact, Email: &email, Phone: &phone, Notes: &notes,
		Status: "active", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create context client: %v", err)
	}
	amount := int64(999900)
	if err := store.DB.Create(&models.Project{
		ID: fixture.projectID, Name: "Launch", Description: "Ship local workspace", ClientID: &fixture.clientID,
		Status: "in_progress", AmountMinor: &amount, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create context project: %v", err)
	}
	description := strings.Repeat("任", aiBusinessContextTextMaxRunes+20)
	blockedReason, blockedAt, blockedFrom := "waiting", now.Format(time.RFC3339Nano), "todo"
	if err := store.DB.Create(&models.Task{
		ID: fixture.taskID, Title: "Prepare release", Description: description, Kind: "work",
		Status: "blocked", ReviewPolicy: "none", Priority: "P1", ProjectID: &fixture.projectID,
		CompletionCriteria: "Build passes", Version: 1,
		BlockedReason: &blockedReason, BlockedAt: &blockedAt, BlockedFromStatus: &blockedFrom,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatalf("create context task: %v", err)
	}
	return fixture
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
