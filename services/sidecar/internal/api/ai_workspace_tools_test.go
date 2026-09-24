package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

func TestAIWorkspaceGrantValidationBeforeAcceptance(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	for _, test := range []struct {
		grant  string
		code   string
		status int
	}{
		{`{"provider_version":999,"scopes":["work"]}`, "AI_WORKSPACE_PROVIDER_CHANGED", 409},
		{`{"provider_version":1,"scopes":["shell"]}`, "AI_WORKSPACE_GRANT_INVALID", 422},
		{`{"provider_version":1,"scopes":["work","work"]}`, "AI_WORKSPACE_GRANT_INVALID", 422},
		{`{"provider_version":1,"scopes":[]}`, "AI_WORKSPACE_GRANT_INVALID", 422},
		{`{"provider_version":1,"scopes":["outputs"]}`, "AI_WORKSPACE_GRANT_INVALID", 422},
		{`{"provider_version":1,"scopes":["work"],"shell":true}`, "INVALID_JSON", 400},
	} {
		response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", []byte(fmt.Sprintf(`{"provider_id":%q,"message":"query","workspace":%s}`, provider.ID, test.grant)), nil)
		assertAPIError(t, response, test.status, test.code)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_sessions", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_messages", 0)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_generations", 0)
}

func TestAIWorkspaceGuideIsScopeFilteredAndCoversDetailedHandbook(t *testing.T) {
	bullets := aiWorkspacePromptBullets()
	if len(bullets) != 39 {
		t.Fatalf("detailed handbook has %d bullets; classify every new rule into workspace_guide or the core prompt", len(bullets))
	}
	covered := map[int]string{14: "core", 15: "core", 29: "core", 30: "core"}
	for topic, indexes := range aiWorkspaceGuideTopics {
		for _, index := range indexes {
			if previous, duplicate := covered[index]; duplicate {
				t.Fatalf("handbook bullet %d belongs to both %s and %s", index, previous, topic)
			}
			covered[index] = topic
		}
	}
	for index := range bullets {
		if _, ok := covered[index]; !ok {
			t.Fatalf("handbook bullet %d is not available through the core prompt or workspace_guide", index)
		}
	}

	_, store, _ := newAIProviderTestRouter(t, time.Now().UTC())
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	guide, ok := registry.Get("workspace_guide")
	if !ok {
		t.Fatal("workspace_guide missing for work scope")
	}
	schema := string(guide.(interface{ InputSchema() json.RawMessage }).InputSchema())
	if !strings.Contains(schema, `"tasks_projects"`) || strings.Contains(schema, `"finance"`) || strings.Contains(schema, `"agents"`) {
		t.Fatalf("work guide schema is not scope filtered: %s", schema)
	}
	result, err := guide.Execute(context.Background(), []byte(`{"topic":"tasks_projects"}`))
	if err != nil || !strings.Contains(result, "task.delete") || !strings.Contains(result, "tag_ids") || !strings.Contains(result, "task.move") || !strings.Contains(result, "task.batch_create") || !strings.Contains(result, "workspace_projects") {
		t.Fatalf("task guide=%s err=%v", result, err)
	}
	if _, err = guide.Execute(context.Background(), []byte(`{"topic":"finance"}`)); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatalf("finance guidance escaped work scope: %v", err)
	}
	result, err = guide.Execute(context.Background(), []byte(`{"topic":"workspace_ui"}`))
	if err != nil || !strings.Contains(result, "/ai?workspace=files") || !strings.Contains(result, "/ai?workspace=browser") {
		t.Fatalf("workspace UI guide=%s err=%v", result, err)
	}
}

// The generic search schema (a `type` enum with `type` required) is only ever
// correct for workspace_search/get/outputs. Any other tool that falls through
// to it advertises arguments its handler rejects, which makes the tool
// unusable for a real model even though direct handler tests still pass.
func TestOnlySearchToolsAdvertiseTheGenericTypeSchema(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "schema audit", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes: []string{"work", "clients", "outputs", "actions", "workspace_ui", "knowledge", "knowledge_actions",
			"finance", "finance_actions", "invoice_actions", "finance_exports", "agent_execution", "agent_files"},
		KnowledgeSources: []aiKnowledgeSourceGrant{{SourceID: uuid.NewString(), ExpectedSourceVersion: 1}},
	}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Only the two resource-listing tools legitimately advertise the generic
	// searchable-resource enum; workspace_outputs narrows `type` to its own
	// values inside the same branch.
	allowed := map[string]bool{"workspace_search": true, "workspace_get": true}
	names := registry.Names()
	if len(names) == 0 {
		t.Fatal("no tools registered")
	}
	for _, name := range names {
		tool, _ := registry.Get(name)
		provider, ok := tool.(interface{ InputSchema() json.RawMessage })
		if !ok {
			continue
		}
		var root struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(provider.InputSchema(), &root); err != nil {
			t.Fatalf("%s schema is invalid: %v", name, err)
		}
		typeProperty, _ := root.Properties["type"].(map[string]any)
		enum, hasEnum := typeProperty["enum"].([]any)
		requiresType := false
		for _, field := range root.Required {
			if field == "type" {
				requiresType = true
			}
		}
		// The generic schema is identified by the searchable resource enum, not
		// by merely having a required `type` field: domain tools such as
		// workspace_client_records legitimately use their own `type` enum.
		genericEnum := map[string]bool{}
		for _, value := range enum {
			if text, ok := value.(string); ok {
				genericEnum[text] = true
			}
		}
		generic := hasEnum && requiresType && genericEnum["task"] && genericEnum["project"] && genericEnum["inbox_item"]
		if generic && !allowed[name] {
			t.Fatalf("%s advertises the generic search schema; add an explicit InputSchema case", name)
		}
	}
	for _, name := range []string{"workspace_search", "workspace_get"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s must be registered for work consent", name)
		}
		provider, _ := tool.(interface{ InputSchema() json.RawMessage })
		var root struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(provider.InputSchema(), &root); err != nil {
			t.Fatalf("%s schema is invalid: %v", name, err)
		}
		typeProperty, _ := root.Properties["type"].(map[string]any)
		if _, ok := typeProperty["enum"].([]any); !ok {
			t.Fatalf("%s must keep the searchable-resource enum", name)
		}
	}
}

func TestAIWorkspaceOpenPanelRequiresExplicitUIConsent(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}

	without, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"work"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := without.Get("workspace_open_panel"); ok {
		t.Fatal("workspace panel tool escaped its explicit UI grant")
	}

	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_open_panel")
	if !ok {
		t.Fatal("workspace panel tool missing with explicit UI grant")
	}
	var emitted aiWorkspacePanelRequest
	ctx := withAIWorkspacePanelEmitter(context.Background(), func(request aiWorkspacePanelRequest) bool {
		emitted = request
		return true
	})
	result, err := tool.Execute(ctx, []byte("{\"panel\":\"review\"}"))
	if err != nil || len(emitted.Panels) != 1 || emitted.Panels[0] != aiWorkspacePanelReview || emitted.SplitRatio != nil || !strings.Contains(result, `"panel":"review"`) || !strings.Contains(result, `"reads_content":false`) {
		t.Fatalf("open panel result=%s emitted=%+v err=%v", result, emitted, err)
	}
	if _, err := tool.Execute(ctx, []byte("{\"panel\":\"review\",\"url\":\"https://example.invalid\"}")); err == nil {
		t.Fatal("panel tool accepted an arbitrary navigation field")
	}
	if _, err := tool.Execute(ctx, []byte("{\"panel\":\"shell\"}")); err == nil {
		t.Fatal("panel tool accepted an unlisted panel")
	}
}

func TestAIWorkspaceOpenPanelSchemaKeepsSplitRatioOptionalAndBounded(t *testing.T) {
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(aiWorkspaceOpenPanelSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	var ratio struct {
		Type    string  `json:"type"`
		Minimum float64 `json:"minimum"`
		Maximum float64 `json:"maximum"`
	}
	if err := json.Unmarshal(schema.Properties["split_ratio"], &ratio); err != nil {
		t.Fatal(err)
	}
	if ratio.Type != "number" || ratio.Minimum != 0.25 || ratio.Maximum != 0.75 {
		t.Fatalf("split ratio schema=%+v", ratio)
	}
	for _, required := range schema.Required {
		if required == "split_ratio" {
			t.Fatal("split_ratio became required, breaking older panel requests")
		}
	}
}

func TestAIWorkspaceOpenPanelCanArrangeTwoFixedPanelsWithoutContentAccess(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_open_panel")
	if !ok {
		t.Fatal("workspace panel tool missing with explicit UI grant")
	}
	var emitted aiWorkspacePanelRequest
	ctx := withAIWorkspacePanelEmitter(context.Background(), func(request aiWorkspacePanelRequest) bool {
		emitted = request
		return true
	})
	result, err := tool.Execute(ctx, []byte("{\"panel\":\"files\",\"companion_panel\":\"review\"}"))
	if err != nil || len(emitted.Panels) != 2 || emitted.Panels[0] != aiWorkspacePanelFiles || emitted.Panels[1] != aiWorkspacePanelReview || emitted.SplitRatio != nil {
		t.Fatalf("split request result=%s emitted=%+v err=%v", result, emitted, err)
	}
	for _, expected := range []string{`"layout":"split"`, `"reads_content":false`, `"executes_command":false`} {
		if !strings.Contains(result, expected) {
			t.Fatalf("split request omitted %s: %s", expected, result)
		}
	}
	ratioResult, err := tool.Execute(ctx, []byte(`{"panel":"files","companion_panel":"review","split_ratio":0.65}`))
	if err != nil || emitted.SplitRatio == nil || *emitted.SplitRatio != 0.65 || !strings.Contains(ratioResult, `"split_ratio":0.65`) {
		t.Fatalf("split ratio request result=%s emitted=%+v err=%v", ratioResult, emitted, err)
	}
	for _, invalid := range []string{
		"{\"panel\":\"files\",\"companion_panel\":\"files\"}",
		"{\"panel\":\"browser\",\"companion_panel\":\"browser\"}",
		"{\"panel\":\"files\",\"companion_panel\":null}",
		"{\"panel\":\"files\",\"companion_panel\":\"shell\"}",
		"{\"panel\":\"files\",\"split_ratio\":0.65}",
		"{\"panel\":\"files\",\"companion_panel\":\"review\",\"split_ratio\":0.249}",
		"{\"panel\":\"files\",\"companion_panel\":\"review\",\"split_ratio\":0.751}",
		"{\"panel\":\"files\",\"companion_panel\":\"review\",\"split_ratio\":null}",
		"{\"panel\":\"files\",\"companion_panel\":\"review\",\"path\":\"C:\\\\private\"}",
	} {
		if _, err := tool.Execute(ctx, []byte(invalid)); err == nil {
			t.Fatalf("panel tool accepted invalid split request %s", invalid)
		}
	}
}

func TestAIWorkspaceBrowserNavigationRequiresExplicitConsentAndAConfirmedBridge(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}

	without, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := without.Get("workspace_request_browser_navigation"); ok {
		t.Fatal("browser navigation tool escaped its explicit grant")
	}
	if _, ok := without.Get("workspace_request_browser_action"); ok {
		t.Fatal("browser action tool escaped its explicit grant")
	}

	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"workspace_browser"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_request_browser_navigation")
	if !ok {
		t.Fatal("browser navigation tool missing with explicit grant")
	}
	emitted := ""
	ctx := withAIWorkspaceBrowserNavigationEmitter(context.Background(), func(address string) bool {
		emitted = address
		return true
	})
	result, err := tool.Execute(ctx, []byte(`{"url":"https://example.com/docs?from=agent"}`))
	if err != nil || emitted != "https://example.com/docs?from=agent" || !strings.Contains(result, `"requires_user_approval":true`) {
		t.Fatalf("browser request result=%s emitted=%q err=%v", result, emitted, err)
	}
	for _, arguments := range []string{
		`{"url":"javascript:alert(1)"}`,
		`{"url":"https://user:secret@example.com"}`,
		`{"url":"ftp://example.com"}`,
		`{"url":"https://example.com","new_tab":true}`,
	} {
		if _, err := tool.Execute(ctx, []byte(arguments)); err == nil {
			t.Fatalf("browser navigation accepted unsafe arguments %s", arguments)
		}
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"url":"https://example.com"}`)); err == nil {
		t.Fatal("browser navigation claimed a UI effect without its chat bridge")
	}
	actionTool, ok := registry.Get("workspace_request_browser_action")
	if !ok {
		t.Fatal("browser action tool missing with explicit grant")
	}
	actionEmitted := ""
	actionCtx := withAIWorkspaceBrowserActionEmitter(context.Background(), func(action string) bool {
		actionEmitted = action
		return true
	})
	actionResult, err := actionTool.Execute(actionCtx, []byte(`{"action":"reload"}`))
	if err != nil || actionEmitted != "reload" || !strings.Contains(actionResult, `"requires_user_confirmation":true`) || strings.Contains(actionResult, "example.com") {
		t.Fatalf("browser action result=%s emitted=%q err=%v", actionResult, actionEmitted, err)
	}
	for _, arguments := range []string{`{"action":"click"}`, `{"action":"reload","tab_id":"private"}`, `{"action":"back","url":"https://example.com"}`} {
		if _, err := actionTool.Execute(actionCtx, []byte(arguments)); err == nil {
			t.Fatalf("browser action accepted unsafe arguments %s", arguments)
		}
	}
	if _, err := actionTool.Execute(context.Background(), []byte(`{"action":"back"}`)); err == nil {
		t.Fatal("browser action claimed a UI effect without its chat bridge")
	}
}

func TestAIWorkspaceRecordNavigationRequiresBothUIAndMatchingReadConsent(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	task := models.Task{ID: uuid.NewString(), Title: "定位任务", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}

	for _, grant := range []aiWorkspaceGrant{
		{ProviderVersion: provider.Version, Scopes: []string{"work"}},
		{ProviderVersion: provider.Version, Scopes: []string{"workspace_ui"}},
	} {
		registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &grant)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := registry.Get("workspace_request_record_navigation"); ok {
			t.Fatalf("record navigation escaped required grants: %#v", grant.Scopes)
		}
	}

	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version,
		Scopes:          []string{"work", "workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_request_record_navigation")
	if !ok {
		t.Fatal("record navigation tool missing with UI and work grants")
	}
	var emitted aiWorkspaceRecordNavigationRequest
	ctx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
		emitted = request
		return true
	})
	result, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"task","record_id":%q}`, task.ID)))
	if err != nil || emitted.RecordType != "task" || emitted.RecordID != task.ID || emitted.TaskID != "" || !strings.Contains(result, `"requires_user_confirmation":true`) || strings.Contains(result, task.Title) {
		t.Fatalf("record request result=%s emitted=%#v err=%v", result, emitted, err)
	}
	for _, arguments := range []string{
		fmt.Sprintf(`{"record_type":"task","record_id":%q,"route":"/tasks/other"}`, task.ID),
		fmt.Sprintf(`{"record_type":"person","record_id":%q}`, task.ID),
		fmt.Sprintf(`{"record_type":"task","record_id":%q}`, strings.ToUpper(task.ID)),
	} {
		if _, err := tool.Execute(ctx, []byte(arguments)); err == nil {
			t.Fatalf("record navigation accepted unsafe arguments %s", arguments)
		}
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"task","record_id":%q}`, task.ID))); err == nil {
		t.Fatal("record navigation claimed a UI effect without its chat bridge")
	}
}

func TestAIWorkspaceSavedViewNavigationChecksIdentityAndExistence(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	view := models.TaskSavedView{
		ID: uuid.NewString(), Name: "待验收", DefinitionJSON: `{}`, SchemaVersion: 1,
		Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if err := store.DB.Create(&view).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version, Scopes: []string{"work", "workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("workspace_request_record_navigation")
	if !ok {
		t.Fatal("navigation tool missing")
	}
	var emitted aiWorkspaceRecordNavigationRequest
	ctx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
		emitted = request
		return true
	})
	result, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"task_saved_view","record_id":%q}`, view.ID)))
	if err != nil || emitted != (aiWorkspaceRecordNavigationRequest{RecordType: "task_saved_view", RecordID: view.ID}) || !strings.Contains(result, `"requires_user_confirmation":true`) || strings.Contains(result, view.Name) {
		t.Fatalf("saved view navigation result=%s emitted=%#v err=%v", result, emitted, err)
	}
	for _, id := range []string{uuid.NewString(), strings.ToUpper(view.ID)} {
		if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"task_saved_view","record_id":%q}`, id))); err == nil {
			t.Fatalf("accepted missing or noncanonical view %q", id)
		}
	}
	if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"task_saved_view","record_id":%q,"route":"/tasks"}`, view.ID))); err == nil {
		t.Fatal("accepted model-provided route")
	}
	clientRegistry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{
		ProviderVersion: provider.Version, Scopes: []string{"clients", "workspace_ui"},
	})
	if err != nil {
		t.Fatal(err)
	}
	clientTool, ok := clientRegistry.Get("workspace_request_record_navigation")
	if !ok {
		t.Fatal("client navigation tool missing")
	}
	if _, err := clientTool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"task_saved_view","record_id":%q}`, view.ID))); err == nil {
		t.Fatal("saved view escaped the work read scope")
	}
}

func TestWorkspaceSystemPromptDescribesServerDerivedOutputNavigation(t *testing.T) {
	prompt := workspaceSystemPrompt(&aiWorkspaceGrant{Scopes: []string{"work", "outputs", "workspace_ui"}})
	for _, want := range []string{
		"workspace_request_record_navigation",
		"Run/Submission/Artifact 的 Task 与 Submission 仅服务端推导",
		"模型不得提供",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("workspace prompt is missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "task_id") || strings.Contains(prompt, "/tasks/") {
		t.Fatalf("workspace prompt leaked a server-derived navigation payload: %s", prompt)
	}
}

func TestAIWorkspaceRelatedRecordNavigationDerivesParentAndRequiresMatchingScope(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	stamp := now.Format(time.RFC3339Nano)
	project := models.Project{ID: uuid.NewString(), Name: "Private project", Description: "PRIVATE PROJECT BODY", Status: "planning", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	client := models.Client{ID: uuid.NewString(), Name: "Private client", Status: "active", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	activityBody := "PRIVATE ACTIVITY BODY"
	note := models.ProjectNote{ID: uuid.NewString(), ProjectID: project.ID, Title: "Private note", Body: "PRIVATE NOTE BODY", OccurredAt: stamp, CreatedByActorID: models.BuiltinOwnerActorID, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	activity := models.ClientActivity{ID: uuid.NewString(), ClientID: client.ID, Kind: "note", Title: "Private activity", Body: &activityBody, OccurredAt: stamp, CreatedByActorID: models.BuiltinOwnerActorID, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	followup := models.ClientFollowup{ID: uuid.NewString(), ClientID: client.ID, AssignedActorID: models.BuiltinOwnerActorID, ScheduledAt: stamp, Timezone: "UTC", Channel: "phone", Purpose: "PRIVATE FOLLOWUP PURPOSE", Status: "planned", Priority: "normal", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	entry := models.FinancialEntry{ID: uuid.NewString(), Type: "income", AmountMinor: 1234, Currency: "CNY", OccurredOn: "2026-09-21", Status: "confirmed", Category: "Private category", Notes: "PRIVATE ENTRY BODY", CreatedByActorID: models.BuiltinOwnerActorID, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	invoice := models.Invoice{ID: uuid.NewString(), InvoiceNumber: "PRIVATE-NAV-1", ClientID: client.ID, AmountMinor: 4321, Currency: "CNY", Status: "draft", IssueDate: "2026-09-21", DueDate: "2026-09-30", Notes: "PRIVATE INVOICE BODY", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	for _, record := range []any{&note, &activity, &followup, &entry, &invoice} {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, id, scope, parentID string
	}{
		{"project_note", note.ID, "work", project.ID},
		{"client_activity", activity.ID, "clients", client.ID},
		{"client_followup", followup.ID, "clients", client.ID},
		{"financial_entry", entry.ID, "finance", ""},
		{"invoice", invoice.ID, "finance", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"workspace_ui", test.scope}})
			if err != nil {
				t.Fatal(err)
			}
			tool, ok := registry.Get("workspace_request_record_navigation")
			if !ok {
				t.Fatal("navigation tool missing with matching read scope")
			}
			schema := string(tool.(interface{ InputSchema() json.RawMessage }).InputSchema())
			if !strings.Contains(schema, `"`+test.name+`"`) {
				t.Fatalf("type absent from schema: %s", schema)
			}
			var emitted aiWorkspaceRecordNavigationRequest
			ctx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
				emitted = request
				return true
			})
			result, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":%q,"record_id":%q}`, test.name, test.id)))
			if err != nil || emitted != (aiWorkspaceRecordNavigationRequest{RecordType: test.name, RecordID: test.id, ParentID: test.parentID}) || !strings.Contains(result, `"requires_user_confirmation":true`) {
				t.Fatalf("result=%s emitted=%#v err=%v", result, emitted, err)
			}
			for _, secret := range []string{test.parentID, note.Title, note.Body, activity.Title, activityBody, followup.Purpose, entry.Notes, invoice.Notes, invoice.InvoiceNumber} {
				if secret != "" && strings.Contains(result, secret) {
					t.Fatalf("navigation tool result disclosed %q: %s", secret, result)
				}
			}
			if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":%q,"record_id":%q,"parent_id":%q}`, test.name, test.id, test.parentID))); err == nil {
				t.Fatal("model supplied a parent identity")
			}
			if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":%q,"record_id":%q}`, test.name, uuid.NewString()))); err == nil {
				t.Fatal("missing record was navigable")
			}
			if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":%q,"record_id":%q}`, test.name, test.id))); err == nil {
				t.Fatal("tool claimed a UI effect without a bridge")
			}
		})
	}
	for _, scopes := range [][]string{{"workspace_ui", "work"}, {"workspace_ui", "clients"}, {"workspace_ui", "finance"}} {
		registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		tool, _ := registry.Get("workspace_request_record_navigation")
		schema := string(tool.(interface{ InputSchema() json.RawMessage }).InputSchema())
		for name, required := range map[string]string{"project_note": "work", "client_activity": "clients", "client_followup": "clients", "financial_entry": "finance", "invoice": "finance"} {
			allowed := slices.Contains(scopes, required)
			if strings.Contains(schema, `"`+name+`"`) != allowed {
				t.Fatalf("type %s scope leakage in %v: %s", name, scopes, schema)
			}
		}
	}
	deletedAt := stamp
	if err := store.DB.Model(&note).Updates(map[string]any{"deleted_at": deletedAt, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "removed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Model(&activity).Updates(map[string]any{"deleted_at": deletedAt, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "removed"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ scope, name, id string }{{"work", "project_note", note.ID}, {"clients", "client_activity", activity.ID}} {
		registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"workspace_ui", test.scope}})
		if err != nil {
			t.Fatal(err)
		}
		tool, _ := registry.Get("workspace_request_record_navigation")
		ctx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(aiWorkspaceRecordNavigationRequest) bool { return true })
		if _, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":%q,"record_id":%q}`, test.name, test.id))); err == nil {
			t.Fatalf("deleted %s remained navigable", test.name)
		}
	}
}

func TestAIChatRelatedRecordNavigationStreamsOnlyServerDerivedParent(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	stamp := now.Format(time.RFC3339Nano)
	project := models.Project{ID: uuid.NewString(), Name: "Private project", Status: "planning", Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	note := models.ProjectNote{ID: uuid.NewString(), ProjectID: project.ID, Title: "PRIVATE NOTE TITLE", Body: "PRIVATE NOTE BODY", OccurredAt: stamp, CreatedByActorID: models.BuiltinOwnerActorID, Version: 1, CreatedAt: stamp, UpdatedAt: stamp}
	for _, record := range []any{&project, &note} {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "open-note", "type": "function", "function": map[string]any{"name": "workspace_request_record_navigation", "arguments": fmt.Sprintf(`{"record_type":"project_note","record_id":%q}`, note.ID)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `我已请求显示项目笔记定位确认卡，请你决定是否打开。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	if err := store.DB.Model(&models.AIProvider{}).Where("id = ?", provider.ID).Updates(map[string]any{
		"base_url": upstream.URL + "/v1", "version": gorm.Expr("version + 1"), "config_version": gorm.Expr("config_version + 1"), "updated_at": stamp,
	}).Error; err != nil {
		t.Fatal(err)
	}
	provider.Version++
	provider.ConfigVersion++
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "请打开这条项目笔记", "workspace": aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "workspace_ui"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 {
		t.Fatalf("chat status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
	}
	want := fmt.Sprintf(`"record_type":"project_note","record_id":%q,"parent_id":%q}`, note.ID, project.ID)
	if !strings.Contains(response.Body.String(), "event: workspace_record_navigation") || !strings.Contains(response.Body.String(), want) || strings.Contains(response.Body.String(), `"route"`) || strings.Contains(response.Body.String(), note.Title) || strings.Contains(response.Body.String(), note.Body) {
		t.Fatalf("related navigation event leaked or lost its server-derived identity: %s", response.Body.String())
	}
}

func TestAIWorkspaceAgentRunNavigationRequiresOutputsAndDerivesItsTask(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	var run models.AgentRun
	var submission models.TaskSubmission
	var artifact models.TaskArtifact
	if err := fixture.Store.DB.Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareAgentRun(tx, prepareAgentRunInput{
			TaskID: fixture.Task.ID, ProviderID: fixture.Provider.ID,
			ArtifactStore: fixture.Service.artifactStore,
		})
		if err != nil {
			return err
		}
		run, err = createAgentRunInTransaction(
			tx, prepared, models.BuiltinOwnerActorID, "record-navigation", fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
		submission = models.TaskSubmission{
			ID:                 uuid.NewString(),
			TaskID:             fixture.Task.ID,
			Sequence:           1,
			Status:             "pending_review",
			Origin:             "manual",
			Summary:            "private submission summary",
			SubmittedByActorID: models.BuiltinOwnerActorID,
			SubmittedAt:        fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := tx.Create(&submission).Error; err != nil {
			return err
		}
		privateBody := "PRIVATE ARTIFACT BODY"
		artifact = models.TaskArtifact{
			ID: uuid.NewString(), TaskID: fixture.Task.ID, SubmissionID: submission.ID,
			Position: 1, StorageKind: "text", Name: "PRIVATE ARTIFACT NAME", ContentText: &privateBody,
			ProducedByActorID: models.BuiltinOwnerActorID, RecordedByActorID: models.BuiltinOwnerActorID,
			IntegrityStatus: "unverified", CreatedAt: fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
		}
		return tx.Create(&artifact).Error
	}); err != nil {
		t.Fatal(err)
	}

	withoutOutputs, err := fixture.Service.aiChatToolRegistry(fixture.Generation.SessionID, true, &fixture.Provider, &aiWorkspaceGrant{
		ProviderVersion: fixture.Provider.Version,
		Scopes:          []string{"work", "workspace_ui"},
	}, fixture.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := withoutOutputs.Get("workspace_request_record_navigation")
	if !ok {
		t.Fatal("work record navigation tool missing")
	}
	if schema := string(tool.(interface{ InputSchema() json.RawMessage }).InputSchema()); strings.Contains(schema, `"agent_run"`) || strings.Contains(schema, `"task_submission"`) || strings.Contains(schema, `"task_artifact"`) {
		t.Fatalf("output record escaped outputs consent: %s", schema)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"agent_run","record_id":%q}`, run.ID))); err == nil {
		t.Fatal("agent run navigation escaped outputs consent")
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"task_submission","record_id":%q}`, submission.ID))); err == nil {
		t.Fatal("task submission navigation escaped outputs consent")
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q}`, artifact.ID))); err == nil {
		t.Fatal("task artifact navigation escaped outputs consent")
	}

	registry, err := fixture.Service.aiChatToolRegistry(fixture.Generation.SessionID, true, &fixture.Provider, &aiWorkspaceGrant{
		ProviderVersion: fixture.Provider.Version,
		Scopes:          []string{"work", "outputs", "workspace_ui"},
	}, fixture.Generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok = registry.Get("workspace_request_record_navigation")
	if !ok {
		t.Fatal("agent run navigation tool missing with outputs consent")
	}
	if schema := string(tool.(interface{ InputSchema() json.RawMessage }).InputSchema()); !strings.Contains(schema, `"agent_run"`) || !strings.Contains(schema, `"task_submission"`) || !strings.Contains(schema, `"task_artifact"`) {
		t.Fatalf("output records missing from outputs-consented schema: %s", schema)
	}
	var emitted aiWorkspaceRecordNavigationRequest
	ctx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
		emitted = request
		return true
	})
	result, err := tool.Execute(ctx, []byte(fmt.Sprintf(`{"record_type":"agent_run","record_id":%q}`, run.ID)))
	if err != nil || emitted != (aiWorkspaceRecordNavigationRequest{RecordType: "agent_run", RecordID: run.ID, TaskID: fixture.Task.ID}) || !strings.Contains(result, `"requires_user_confirmation":true`) || strings.Contains(result, fixture.Task.ID) || strings.Contains(result, fixture.Task.Title) {
		t.Fatalf("agent run navigation result=%s emitted=%#v err=%v", result, emitted, err)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"agent_run","record_id":%q}`, run.ID))); err == nil {
		t.Fatal("agent run navigation claimed a UI effect without its chat bridge")
	}
	var emittedSubmission aiWorkspaceRecordNavigationRequest
	submissionCtx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
		emittedSubmission = request
		return true
	})
	result, err = tool.Execute(submissionCtx, []byte(fmt.Sprintf(`{"record_type":"task_submission","record_id":%q}`, submission.ID)))
	if err != nil || emittedSubmission != (aiWorkspaceRecordNavigationRequest{RecordType: "task_submission", RecordID: submission.ID, TaskID: fixture.Task.ID}) || !strings.Contains(result, `"requires_user_confirmation":true`) || strings.Contains(result, fixture.Task.ID) || strings.Contains(result, submission.Summary) {
		t.Fatalf("task submission navigation result=%s emitted=%#v err=%v", result, emittedSubmission, err)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"task_submission","record_id":%q}`, submission.ID))); err == nil {
		t.Fatal("task submission navigation claimed a UI effect without its chat bridge")
	}
	var emittedArtifact aiWorkspaceRecordNavigationRequest
	artifactCtx := withAIWorkspaceRecordNavigationEmitter(context.Background(), func(request aiWorkspaceRecordNavigationRequest) bool {
		emittedArtifact = request
		return true
	})
	result, err = tool.Execute(artifactCtx, []byte(fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q}`, artifact.ID)))
	if err != nil || emittedArtifact != (aiWorkspaceRecordNavigationRequest{RecordType: "task_artifact", RecordID: artifact.ID, TaskID: fixture.Task.ID, SubmissionID: submission.ID}) || !strings.Contains(result, `"requires_user_confirmation":true`) {
		t.Fatalf("task artifact navigation result=%s emitted=%#v err=%v", result, emittedArtifact, err)
	}
	for _, secret := range []string{fixture.Task.ID, submission.ID, artifact.Name, *artifact.ContentText, fixture.Task.Title} {
		if strings.Contains(result, secret) {
			t.Fatalf("task artifact navigation disclosed %q: %s", secret, result)
		}
	}
	for _, args := range []string{
		fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q,"submission_id":%q}`, artifact.ID, submission.ID),
		fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q}`, uuid.NewString()),
	} {
		if _, err := tool.Execute(artifactCtx, []byte(args)); err == nil {
			t.Fatalf("task artifact navigation accepted %s", args)
		}
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q}`, artifact.ID))); err == nil {
		t.Fatal("task artifact navigation claimed a UI effect without its chat bridge")
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 || calls == 3 || calls == 5 {
			recordType, recordID, callID := "agent_run", run.ID, "open-run"
			if calls == 3 {
				recordType, recordID, callID = "task_submission", submission.ID, "open-submission"
			} else if calls == 5 {
				recordType, recordID, callID = "task_artifact", artifact.ID, "open-artifact"
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": callID, "type": "function", "function": map[string]any{"name": "workspace_request_record_navigation", "arguments": fmt.Sprintf(`{"record_type":%q,"record_id":%q}`, recordType, recordID)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `我已请求显示执行过程定位确认卡，请你决定是否打开。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	if err := fixture.Store.DB.Model(&models.AIProvider{}).Where("id = ?", fixture.Provider.ID).Updates(map[string]any{
		"base_url":       upstream.URL + "/v1",
		"version":        gorm.Expr("version + 1"),
		"config_version": gorm.Expr("config_version + 1"),
		"updated_at":     fixture.Service.options.Now().UTC().Format(time.RFC3339Nano),
	}).Error; err != nil {
		t.Fatal(err)
	}
	fixture.Provider.Version++
	fixture.Provider.ConfigVersion++
	body, _ := json.Marshal(map[string]any{
		"provider_id": fixture.Provider.ID,
		"message":     "请打开刚才的 Agent 执行过程",
		"workspace": aiWorkspaceGrant{
			ProviderVersion: fixture.Provider.Version,
			Scopes:          []string{"work", "outputs", "workspace_ui"},
		},
	})
	response := performRequest(fixture.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: workspace_record_navigation") {
		t.Fatalf("agent run chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	want := fmt.Sprintf(`"record_type":"agent_run","record_id":%q,"task_id":%q}`, run.ID, fixture.Task.ID)
	if !strings.Contains(response.Body.String(), want) || strings.Contains(response.Body.String(), `"route"`) || strings.Contains(response.Body.String(), fixture.Task.Title) {
		t.Fatalf("agent run event was not a strict server-derived payload: %s", response.Body.String())
	}
	response = performRequest(fixture.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 4 || !strings.Contains(response.Body.String(), "event: workspace_record_navigation") {
		t.Fatalf("task submission chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	want = fmt.Sprintf(`"record_type":"task_submission","record_id":%q,"task_id":%q}`, submission.ID, fixture.Task.ID)
	if !strings.Contains(response.Body.String(), want) || strings.Contains(response.Body.String(), `"route"`) || strings.Contains(response.Body.String(), fixture.Task.Title) || strings.Contains(response.Body.String(), submission.Summary) {
		t.Fatalf("task submission event was not a strict server-derived payload: %s", response.Body.String())
	}
	response = performRequest(fixture.Router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 6 || !strings.Contains(response.Body.String(), "event: workspace_record_navigation") {
		t.Fatalf("task artifact chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	want = fmt.Sprintf(`"record_type":"task_artifact","record_id":%q,"task_id":%q,"submission_id":%q}`, artifact.ID, fixture.Task.ID, submission.ID)
	if !strings.Contains(response.Body.String(), want) || strings.Contains(response.Body.String(), `"route"`) || strings.Contains(response.Body.String(), artifact.Name) || strings.Contains(response.Body.String(), *artifact.ContentText) {
		t.Fatalf("task artifact event was not a strict server-derived payload: %s", response.Body.String())
	}
	deletedAt := fixture.Service.options.Now().UTC().Format(time.RFC3339Nano)
	if err := fixture.Store.DB.Model(&artifact).Updates(map[string]any{"deleted_at": deletedAt, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "test"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(artifactCtx, []byte(fmt.Sprintf(`{"record_type":"task_artifact","record_id":%q}`, artifact.ID))); err == nil {
		t.Fatal("deleted task artifact remained navigable")
	}
}

func TestAIChatWorkspacePanelBridgeStreamsOnlyAnExplicitAllowedPanel(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "open-panel", "type": "function", "function": map[string]any{"name": "workspace_open_panel", "arguments": `{"panel":"agents","companion_panel":"review","split_ratio":0.62}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `已打开子智能体工作区。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "workspace-ui", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "打开子智能体面板",
		"workspace": aiWorkspaceGrant{
			ProviderVersion: current.Version,
			Scopes:          []string{"workspace_ui"},
		},
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: workspace_panels") {
		t.Fatalf("chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	if !strings.Contains(response.Body.String(), `"panels":["agents","review"],"split_ratio":0.62}`) || strings.Contains(response.Body.String(), "example.invalid") {
		t.Fatalf("workspace event was not the fixed panel payload: %s", response.Body.String())
	}
}

func TestAIChatBrowserNavigationBridgeStreamsOnlyAnExplicitValidatedRequest(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "request-browser", "type": "function", "function": map[string]any{"name": "workspace_request_browser_navigation", "arguments": `{"url":"https://example.com/docs"}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `我已请求在右侧显示确认卡，请你核对地址后决定是否打开。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "workspace-browser", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "请打开我给出的文档地址",
		"workspace": aiWorkspaceGrant{
			ProviderVersion: current.Version,
			Scopes:          []string{"workspace_browser"},
		},
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: workspace_browser_navigation") {
		t.Fatalf("chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	if !strings.Contains(response.Body.String(), `"url":"https://example.com/docs"}`) || strings.Contains(response.Body.String(), `"new_tab"`) {
		t.Fatalf("browser event was not the strict URL payload: %s", response.Body.String())
	}
}

func TestAIChatBrowserActionBridgeStreamsOnlyAUserConfirmableAction(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "request-browser-action", "type": "function", "function": map[string]any{"name": "workspace_request_browser_action", "arguments": `{"action":"reload"}`}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `我已请求你确认刷新当前浏览器标签。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "workspace-browser-action", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "请刷新当前浏览器标签",
		"workspace":   aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"workspace_browser"}},
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: workspace_browser_action") {
		t.Fatalf("chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	if !strings.Contains(response.Body.String(), `"action":"reload"}`) || strings.Contains(response.Body.String(), `"tab_id"`) {
		t.Fatalf("browser action event leaked unexpected fields: %s", response.Body.String())
	}
}

func TestAIChatRecordNavigationBridgeStreamsOnlyAnAuthorizedIdentity(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	task := models.Task{ID: uuid.NewString(), Title: "打开我", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "open-record", "type": "function", "function": map[string]any{"name": "workspace_request_record_navigation", "arguments": fmt.Sprintf(`{"record_type":"task","record_id":%q}`, task.ID)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		streamMockAIDelta(w, `我已请求显示定位确认卡，请你决定是否打开该任务。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "workspace-record", upstream.URL+"/v1", "test-model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"provider_id": provider.ID,
		"message":     "请定位这条任务",
		"workspace": aiWorkspaceGrant{
			ProviderVersion: current.Version,
			Scopes:          []string{"work", "workspace_ui"},
		},
	})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, nil)
	if response.Code != http.StatusOK || calls != 2 || !strings.Contains(response.Body.String(), "event: workspace_record_navigation") {
		t.Fatalf("chat=%d body=%s calls=%d", response.Code, response.Body.String(), calls)
	}
	want := fmt.Sprintf(`"record_type":"task","record_id":%q}`, task.ID)
	if !strings.Contains(response.Body.String(), want) || strings.Contains(response.Body.String(), `"route"`) || strings.Contains(response.Body.String(), task.Title) {
		t.Fatalf("record event was not the strict identity payload: %s", response.Body.String())
	}
}

// Every model-facing tool must advertise exactly the arguments its handler
// accepts. Metadata tools previously fell through to the generic search schema
// (type/status/project_id) while their handlers expected `view`, so a real
// model calling them as advertised always failed.
func TestAIMetadataToolSchemasMatchTheirHandlers(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "metadata tools", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "actions", "knowledge", "knowledge_actions"},
			KnowledgeSources: []aiKnowledgeSourceGrant{{SourceID: uuid.NewString(), ExpectedSourceVersion: 1}}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		properties []string
		probes     []string
	}{
		{
			name:       "knowledge_library",
			properties: []string{"view", "id", "query", "status", "limit", "offset"},
			probes:     []string{`{"view":"list","limit":5}`, `{"view":"list","query":"x","status":"failed"}`},
		},
		{
			name:       "workspace_task_views",
			properties: []string{"view", "id", "query", "limit", "offset"},
			probes:     []string{`{"view":"list","limit":5}`, `{"view":"list","query":"x"}`},
		},
	} {
		tool, ok := registry.Get(test.name)
		if !ok {
			t.Fatalf("%s is not registered for the metadata scopes", test.name)
		}
		schema, ok := tool.(interface{ InputSchema() json.RawMessage })
		if !ok {
			t.Fatalf("%s has no input schema", test.name)
		}
		var root struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(schema.InputSchema(), &root); err != nil {
			t.Fatalf("%s schema is invalid: %v", test.name, err)
		}
		declared := make([]string, 0, len(root.Properties))
		for key := range root.Properties {
			declared = append(declared, key)
		}
		sort.Strings(declared)
		expected := append([]string(nil), test.properties...)
		sort.Strings(expected)
		if strings.Join(declared, ",") != strings.Join(expected, ",") {
			t.Fatalf("%s advertises %v; the handler accepts %v", test.name, declared, expected)
		}
		if len(root.Required) != 1 || root.Required[0] != "view" {
			t.Fatalf("%s must require only view: %v", test.name, root.Required)
		}
		for _, probe := range test.probes {
			if _, err := tool.Execute(context.Background(), []byte(probe)); err != nil {
				t.Fatalf("%s rejected its own advertised arguments %s: %v", test.name, probe, err)
			}
		}
		// The generic search schema must never be the advertised contract.
		if _, err := tool.Execute(context.Background(), []byte(`{"type":"task"}`)); err == nil {
			t.Fatalf("%s accepted a generic search argument", test.name)
		}
	}
}

func TestAIWorkspaceToolSchemasAreExplicitForRegisteredTools(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	session := models.AISession{ID: uuid.NewString(), Title: "all tools", Persist: true, Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	generation := models.AIGeneration{ID: uuid.NewString(), SessionID: session.ID, ProviderID: provider.ID,
		Status: "streaming", CreatedAt: session.CreatedAt, UpdatedAt: session.CreatedAt}
	if err := store.DB.Create(&generation).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry(session.ID, true, &provider,
		&aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{
			"work", "actions", "clients", "outputs", "output_files", "finance", "finance_actions", "invoice_actions", "finance_exports",
			"agent_execution", "agent_files", "knowledge", "knowledge_actions",
		}, KnowledgeSources: []aiKnowledgeSourceGrant{{SourceID: uuid.NewString(), ExpectedSourceVersion: 1}}}, generation.ID)
	if err != nil {
		t.Fatal(err)
	}
	genericContracts := map[string]map[string]bool{
		"workspace_search":  {"type": true, "limit": true, "offset": true, "query": true, "status": true, "project_id": true},
		"workspace_get":     {"type": true, "id": true, "content_offset": true, "content_limit": true},
		"workspace_outputs": {"type": true, "task_id": true, "limit": true, "offset": true},
	}
	for _, name := range registry.Names() {
		if aiProgressToolName(name) != name {
			t.Fatalf("%s is registered but would be hidden as unknown_tool in run progress", name)
		}
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("registry name %s disappeared", name)
		}
		workspaceTool, ok := tool.(*aiWorkspaceTool)
		if !ok {
			continue
		}
		if strings.Contains(workspaceTool.Summary(), "Unsupported workspace tool") {
			t.Fatalf("%s is registered without an explicit summary", name)
		}
		schema := workspaceTool.InputSchema()
		var root struct {
			Required   []string       `json:"required"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(schema, &root); err != nil {
			t.Fatalf("%s schema is invalid: %v", name, err)
		}
		if _, ok := root.Properties["unsupported_workspace_tool"]; ok {
			t.Fatalf("%s is registered without an explicit input schema", name)
		}
		if expected, generic := genericContracts[name]; generic {
			assertPropertySet(t, name, root.Properties, expected)
			continue
		}
		for genericName, genericProperties := range genericContracts {
			if samePropertySet(root.Properties, genericProperties) {
				t.Fatalf("%s advertises the generic %s schema; add an explicit schema matching its handler", name, genericName)
			}
		}
	}
}

func assertPropertySet(t *testing.T, name string, properties map[string]any, expected map[string]bool) {
	t.Helper()
	if !samePropertySet(properties, expected) {
		declared := make([]string, 0, len(properties))
		for key := range properties {
			declared = append(declared, key)
		}
		sort.Strings(declared)
		wanted := make([]string, 0, len(expected))
		for key := range expected {
			wanted = append(wanted, key)
		}
		sort.Strings(wanted)
		t.Fatalf("%s properties = %v, want %v", name, declared, wanted)
	}
}

func samePropertySet(properties map[string]any, expected map[string]bool) bool {
	if len(properties) != len(expected) {
		return false
	}
	for key := range properties {
		if !expected[key] {
			return false
		}
	}
	return true
}

func TestAIWorkspaceToolsScopePagingPrivacyAndProviderRevocation(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	fixture := seedAIBusinessContextFixture(t, store, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	var extra models.Task
	if err := store.DB.First(&extra, "id = ?", fixture.taskID).Error; err != nil {
		t.Fatal(err)
	}
	extra.ID = uuid.NewString()
	if err := store.DB.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, nil)
	if err != nil || len(registry.Names()) != 4 {
		t.Fatalf("default registry: %v %v", registry, err)
	}
	if _, exists := registry.Get("workspace_request_access"); !exists {
		t.Fatal("ungranted registry must offer only a request, not workspace access")
	}
	registry, err = service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Get("workspace_outputs"); exists {
		t.Fatal("outputs exposed without consent")
	}
	search, _ := registry.Get("workspace_search")
	get, _ := registry.Get("workspace_get")
	page, err := search.Execute(context.Background(), []byte(`{"type":"task","limit":1}`))
	if err != nil || !strings.Contains(page, `"next_offset":1`) || !strings.Contains(page, `"has_more":true`) {
		t.Fatalf("page=%s %v", page, err)
	}
	page, err = search.Execute(context.Background(), []byte(`{"type":"task","limit":1,"offset":1}`))
	if err != nil || !strings.Contains(page, `"next_offset":null`) || !strings.Contains(page, `"has_more":false`) {
		t.Fatalf("last page=%s %v", page, err)
	}
	for _, args := range []string{`{"type":"client"}`, `{"type":"invoice"}`, `{"type":"artifact"}`} {
		if _, err := search.Execute(context.Background(), []byte(args)); !errors.Is(err, harness.ErrPermissionDenied) {
			t.Fatalf("denial for %s: %v", args, err)
		}
	}
	for _, args := range []string{`{"type":"task","limit":0}`, `{"type":"task","offset":1001}`, `{"type":"task","sql":"SELECT *"}`, `{"type":"task"} {}`} {
		if _, err := search.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatalf("accepted invalid %s", args)
		}
	}
	result, err := search.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"task","query":"release","status":"blocked","project_id":%q,"limit":1}`, fixture.projectID)))
	if err != nil || !strings.Contains(result, "Prepare release") || !strings.Contains(result, "/tasks/") {
		t.Fatalf("search=%s %v", result, err)
	}
	for _, literal := range []string{"%", "_", "' OR 1=1 --"} {
		args, _ := json.Marshal(map[string]any{"type": "task", "query": literal})
		result, err = search.Execute(context.Background(), args)
		if err != nil || !strings.Contains(result, `"items":[]`) {
			t.Fatalf("literal query=%s %v", result, err)
		}
	}
	for _, item := range []struct{ kind, id string }{{"task", fixture.taskID}, {"project", fixture.projectID}} {
		result, err = get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":%q,"id":%q}`, item.kind, item.id)))
		if err != nil || !strings.Contains(result, item.id) {
			t.Fatalf("get=%s %v", result, err)
		}
		for _, secret := range []string{"private@example.com", "amount_minor", "999900", "Secret Contact", "contact_name"} {
			if strings.Contains(result, secret) {
				t.Fatalf("leaked %s", secret)
			}
		}
	}
	clients, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"clients"}})
	if err != nil {
		t.Fatal(err)
	}
	clientSearch, ok := clients.Get("workspace_search")
	if !ok {
		t.Fatal("client workspace_search missing")
	}
	result, err = clientSearch.Execute(context.Background(), []byte(`{"type":"client","query":"客"}`))
	if err != nil || !strings.Contains(result, `"items":[]`) {
		t.Fatalf("client notes were searchable: %s %v", result, err)
	}
	result, err = clientSearch.Execute(context.Background(), []byte(`{"type":"client","query":"Acme"}`))
	if err != nil || !strings.Contains(result, fixture.clientID) {
		t.Fatalf("client name was not searchable: %s %v", result, err)
	}
	clientGet, _ := clients.Get("workspace_get")
	result, err = clientGet.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"client","id":%q}`, fixture.clientID)))
	if err != nil || !strings.Contains(result, "Acme") || strings.Contains(result, "private@example.com") {
		t.Fatalf("client=%s %v", result, err)
	}
	if _, err = clientGet.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"task","id":%q}`, fixture.taskID))); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal("client consent granted tasks")
	}
	if err := store.DB.Model(&models.AIProvider{}).Where("id = ?", provider.ID).Updates(map[string]any{"config_version": provider.ConfigVersion + 1, "version": provider.Version + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = search.Execute(context.Background(), []byte(`{"type":"task"}`)); err == nil || !strings.Contains(err.Error(), "permission expired") {
		t.Fatalf("changed provider allowed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = search.Execute(ctx, []byte(`{"type":"task"}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestAIWorkspaceProjectDetailUsesNativeLifecycleOptions(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	_, store, _ := newAIProviderTestRouter(t, now)
	fixture := seedAIBusinessContextFixture(t, store, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	get, ok := registry.Get("workspace_get")
	if !ok {
		t.Fatal("workspace_get missing with work scope")
	}
	for _, test := range []struct {
		status  string
		actions []string
		pending int64
	}{
		{"planning", []string{"start", "archive"}, 1},
		{"in_progress", []string{"pause", "complete", "archive"}, 1},
		{"paused", []string{"resume", "complete", "archive"}, 1},
		{"completed", []string{"reopen", "archive"}, 1},
		{"archived", []string{"restore"}, 1},
	} {
		if err := store.DB.Model(&models.Project{}).Where("id = ?", fixture.projectID).Update("status", test.status).Error; err != nil {
			t.Fatal(err)
		}
		result, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"project","id":%q}`, fixture.projectID)))
		if err != nil {
			t.Fatal(err)
		}
		var output struct {
			Record struct {
				Fields struct {
					AvailableActions    []string `json:"available_actions"`
					IncompleteTaskCount int64    `json:"incomplete_task_count"`
				} `json:"fields"`
			} `json:"record"`
		}
		if err := json.Unmarshal([]byte(result), &output); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(output.Record.Fields.AvailableActions, test.actions) || output.Record.Fields.IncompleteTaskCount != test.pending {
			t.Fatalf("status=%s: options=%v incomplete=%d, want %v/%d", test.status, output.Record.Fields.AvailableActions, output.Record.Fields.IncompleteTaskCount, test.actions, test.pending)
		}
		for _, secret := range []string{fixture.clientID, "999900", "Acme", "private@example.com"} {
			if strings.Contains(result, secret) {
				t.Fatalf("status=%s: leaked %s", test.status, secret)
			}
		}
	}
	emptyProjectID := uuid.NewString()
	if err := store.DB.Create(&models.Project{ID: emptyProjectID, Name: "Empty project", Status: "planning", Version: 1,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}).Error; err != nil {
		t.Fatal(err)
	}
	result, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"project","id":%q}`, emptyProjectID)))
	if err != nil || !strings.Contains(result, `"incomplete_task_count":0`) {
		t.Fatalf("project without tasks should have no completion warning: %s %v", result, err)
	}
}

func TestAIChatWorkspaceToolLoopAndGrantDoesNotCarryToNextMessage(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	fixture := seedAIBusinessContextFixture(t, store, now)
	var calls atomic.Int32
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var payload struct {
			Messages []map[string]any `json:"messages"`
			Tools    []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if call <= 2 {
			// Generic queries need no discovery turn. Domain helper schemas are
			// loaded by workspace_guide, not copied into every request.
			expected := map[string]bool{"memory_search": true, "memory_write": true, "memory_propose": true, "workspace_request_access": true, "workspace_guide": true, "workspace_search": true, "workspace_get": true}
			for _, definition := range payload.Tools {
				function, ok := definition["function"].(map[string]any)
				if !ok {
					t.Error("missing function definition")
					continue
				}
				name, _ := function["name"].(string)
				if !expected[name] {
					t.Errorf("unexpected or duplicate granted tool %q", name)
				}
				delete(expected, name)
			}
			if len(expected) != 0 {
				t.Errorf("missing granted tools: %v", expected)
			}
		}
		if call == 1 {
			if !strings.Contains(fmt.Sprint(payload.Messages[0]), "本次允许的范围：work") {
				t.Error("permission prompt not propagated")
			}
			if !strings.Contains(fmt.Sprint(payload.Messages[0]), "workspace_guide") {
				t.Error("on-demand workspace guide not required by core prompt")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "read-work", "type": "function", "function": map[string]any{"name": "workspace_get", "arguments": fmt.Sprintf(`{"type":"task","id":%q}`, fixture.taskID)}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if call == 2 {
			found := false
			for _, message := range payload.Messages {
				if message["role"] == "tool" && strings.Contains(fmt.Sprint(message["content"]), "Prepare release") && strings.Contains(fmt.Sprint(message["content"]), "blocked") {
					found = true
				}
			}
			if !found {
				t.Error("real task result missing")
			}
		}
		if call == 3 && (len(payload.Tools) != 4 || strings.Contains(fmt.Sprint(payload.Messages[0]), "本次允许的范围")) {
			t.Error("grant carried to next message")
		}
		streamMockAIDelta(w, `任务处于阻塞状态。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "work-query", upstream.URL+"/v1", "gpt-test")
	var currentProvider models.AIProvider
	if err := store.DB.First(&currentProvider, "id = ?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "query task", "workspace": aiWorkspaceGrant{ProviderVersion: currentProvider.Version, Scopes: []string{"work"}}})
	response := performRequest(router, http.MethodPost, "/api/v1/ai/chat", body, map[string]string{"Idempotency-Key": "work-query-once"})
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") {
		t.Fatalf("chat=%d %s", response.Code, response.Body.String())
	}
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	response = chatRequest(t, router, provider.ID, session.ID, "next without consent")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "event: done") || calls.Load() != 3 {
		t.Fatalf("next=%d %s calls=%d", response.Code, response.Body.String(), calls.Load())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM ai_run_steps WHERE tool_name = 'workspace_get' AND status = 'succeeded'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE action = 'ai_workspace_access_granted'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM workflow_events WHERE current_json LIKE ?", 0, "%Prepare release%")
	// Same idempotency identity with a changed grant cannot become another run.
	changed, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "query task"})
	response = performRequest(router, http.MethodPost, "/api/v1/ai/chat", changed, map[string]string{"Idempotency-Key": "work-query-once"})
	if response.Code != 409 || calls.Load() != 3 {
		t.Fatalf("changed retry=%d %s", response.Code, response.Body.String())
	}
}

func TestAIWorkspaceArtifactResultsPaginationAndDeletedBoundary(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	content := strings.Repeat("中文🙂", 1600)
	body, _ := json.Marshal(map[string]any{"summary": "Delivery", "artifacts": []any{map[string]any{"client_ref": "text", "storage_kind": "text", "name": "Result", "content_text": content}}})
	response := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", body, map[string]string{"If-Match": `"3"`})
	if response.Code != 201 {
		t.Fatalf("submission=%d %s", response.Code, response.Body.String())
	}
	artifactID := decodeSubmitOutputResponse(t, response.Body.Bytes()).Artifacts[0].ID
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs"}})
	if err != nil {
		t.Fatal(err)
	}
	get, _ := registry.Get("workspace_get")
	list, _ := registry.Get("workspace_outputs")
	metadata, err := list.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","task_id":%q}`, task.ID)))
	if err != nil || !strings.Contains(metadata, artifactID) || strings.Contains(metadata, "中文") {
		t.Fatalf("metadata=%s %v", metadata, err)
	}
	var result struct {
		Content string `json:"content"`
		Next    *int   `json:"next_content_offset"`
		Status  string `json:"status"`
	}
	text, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q}`, artifactID)))
	if err != nil || json.Unmarshal([]byte(text), &result) != nil {
		t.Fatalf("detail=%s %v", text, err)
	}
	if len([]rune(result.Content)) != 4000 || result.Next == nil || *result.Next != 4000 || result.Status != "pending_review" {
		t.Fatalf("first page=%+v", result)
	}
	first := result.Content
	text, err = get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q,"content_offset":4000}`, artifactID)))
	if err != nil || json.Unmarshal([]byte(text), &result) != nil || result.Next != nil || first+result.Content != content {
		t.Fatalf("pagination failed: %v", err)
	}
	// Use the production deletion command, not bypassing artifact audit triggers.
	response = performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": `"4"`})
	if response.Code != 200 {
		t.Fatalf("review=%d %s", response.Code, response.Body.String())
	}
	response = performRequest(router, http.MethodDelete, "/api/v1/artifacts/"+artifactID+"?confirm=true", []byte(`{"reason":"remove"}`), map[string]string{"If-Match": `"5"`})
	if response.Code != 200 && response.Code != 204 {
		t.Fatalf("delete=%d %s", response.Code, response.Body.String())
	}
	if _, err = get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q}`, artifactID))); err == nil {
		t.Fatal("deleted artifact still readable")
	}
	metadata, err = list.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","task_id":%q}`, task.ID)))
	if err != nil || strings.Contains(metadata, artifactID) {
		t.Fatalf("deleted listing=%s %v", metadata, err)
	}
}

func TestAIWorkspaceOutputPageCanRecoverFromEncodedBudget(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	task, _ := setupManualReviewTask(t, router)
	content := strings.Repeat("<", 4100) + "中文🙂"
	body, _ := json.Marshal(map[string]any{"summary": "Review", "artifacts": []any{map[string]any{"client_ref": "text", "storage_kind": "text", "name": strings.Repeat("&", 200), "content_text": content}}})
	response := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+"/submit-output", body, map[string]string{"If-Match": `"3"`})
	if response.Code != 201 {
		t.Fatalf("submission=%d %s", response.Code, response.Body.String())
	}
	id := decodeSubmitOutputResponse(t, response.Body.Bytes()).Artifacts[0].ID
	provider := createCompactionTestProvider(t, store, time.Now().UTC(), uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work", "outputs"}})
	if err != nil {
		t.Fatal(err)
	}
	get, _ := registry.Get("workspace_get")
	if _, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q}`, id))); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected encoded budget failure, got %v", err)
	}
	var recovered strings.Builder
	for offset := 0; ; {
		text, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q,"content_offset":%d,"content_limit":1000}`, id, offset)))
		if err != nil {
			t.Fatal(err)
		}
		var page struct {
			Content string `json:"content"`
			Next    *int   `json:"next_content_offset"`
		}
		if err := json.Unmarshal([]byte(text), &page); err != nil {
			t.Fatal(err)
		}
		recovered.WriteString(page.Content)
		if page.Next == nil {
			break
		}
		if *page.Next <= offset {
			t.Fatal("pagination did not advance")
		}
		offset = *page.Next
	}
	if recovered.String() != content {
		t.Fatal("complete Unicode output was not recovered")
	}
	for _, limit := range []string{"0", "-1", "4001", "1.5", `"100"`} {
		if _, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"artifact","id":%q,"content_limit":%s}`, id, limit))); err == nil {
			t.Fatalf("accepted invalid limit %s", limit)
		}
	}
	if _, err := get.Execute(context.Background(), []byte(fmt.Sprintf(`{"type":"task","id":%q,"content_limit":10}`, task.ID))); err == nil {
		t.Fatal("accepted text paging on task")
	}
}

func TestAIWorkspaceInboxSearchReturnsServerSnapshotAndGlobalUnreadTotal(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 987, time.UTC)
	router, store, _ := newAIProviderTestRouter(t, now)
	provider := createCompactionTestProvider(t, store, now, uuid.NewString())
	service := &API{db: store.DB, maintenance: &sync.RWMutex{}, options: Options{Now: func() time.Time { return now }}}
	registry, err := service.aiChatToolRegistry("ephemeral", false, &provider, &aiWorkspaceGrant{ProviderVersion: provider.Version, Scopes: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	search, ok := registry.Get("workspace_search")
	if !ok {
		t.Fatal("workspace_search missing")
	}

	filtered := createInboxItemForTest(t, router, `{"title":"Needle filtered item","priority":"P0"}`, "")
	_ = createInboxItemForTest(t, router, `{"title":"Other globally unread item","priority":"P2"}`, "")
	alreadyRead := createInboxItemForTest(t, router, `{"title":"Already read item"}`, "")
	read := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+alreadyRead.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"1"`})
	if read.Code != http.StatusOK {
		t.Fatal(read.Body.String())
	}
	snoozed := createInboxItemForTest(t, router, `{"title":"Future snoozed item"}`, "")
	snooze := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+snoozed.ID+"/snooze",
		[]byte(fmt.Sprintf(`{"snoozed_until":%q}`, formatInboxTimestamp(now.Add(time.Hour)))),
		map[string]string{"If-Match": `"1"`},
	)
	if snooze.Code != http.StatusOK {
		t.Fatal(snooze.Body.String())
	}

	result, err := search.Execute(context.Background(), []byte(`{"type":"inbox_item","query":"Needle","status":"open","limit":1}`))
	var snapshot struct {
		Items []struct {
			ID    string `json:"id"`
			Route string `json:"route"`
		} `json:"items"`
		SnapshotAt          string `json:"snapshot_at"`
		ServerNow           string `json:"server_now"`
		SnapshotUnreadTotal int64  `json:"snapshot_unread_total"`
		ReadAllMaxItems     int    `json:"read_all_max_items"`
	}
	if err != nil || json.Unmarshal([]byte(result), &snapshot) != nil {
		t.Fatalf("inbox search=%s err=%v", result, err)
	}
	wantSnapshot := formatInboxTimestamp(now)
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != filtered.ID || snapshot.Items[0].Route != "/inbox/"+filtered.ID ||
		snapshot.SnapshotAt != wantSnapshot || snapshot.ServerNow != wantSnapshot || snapshot.SnapshotUnreadTotal != 2 ||
		snapshot.ReadAllMaxItems != maxAIInboxReadAllItems {
		t.Fatalf("inbox snapshot=%s", result)
	}
}
