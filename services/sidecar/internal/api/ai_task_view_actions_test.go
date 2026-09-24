package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func createNativeTaskView(t *testing.T, router http.Handler, name string, definition map[string]any) taskSavedViewResponse {
	t.Helper()
	body, err := json.Marshal(map[string]any{"name": name, "definition": definition})
	if err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/task-saved-views", body, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create task view = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data taskSavedViewResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestAITaskViewCreateRequiresConfirmationAndIsReadableByTheModel(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tag := models.Tag{ID: uuid.NewString(), Name: "Focus", Color: "#112233", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	projectColor := "#223344"
	project := models.Project{ID: uuid.NewString(), Name: "Launch", Color: &projectColor, Status: "planning", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	args := fmt.Sprintf(`{"action":"task_view.create","changes":{"name":" 本周 P2 ","definition":{"status":"active","priority":"P2","project_id":%q,"tag_ids":[%q],"planned_from":"2026-09-21","planned_to":"2026-09-27","sort":"due_date"}}}`, project.ID, tag.ID)
	row := proposeTestAction(t, store, tool, args)
	if !strings.Contains(row.PreviewJSON, `"name":"本周 P2"`) ||
		!strings.Contains(row.PreviewJSON, `"project_name":"Launch"`) ||
		!strings.Contains(row.PreviewJSON, `"tag_names":["Focus"]`) ||
		!strings.Contains(row.PreviewJSON, `"planned_from":"2026-09-21"`) ||
		!strings.Contains(row.PreviewJSON, `"next_version":1`) {
		t.Fatalf("task view preview=%s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views", 0)

	finishAIGeneration(t, store, generation)
	receipt := decodeTestActionResponse(t, decideTestAction(t, router, row, ""))
	if receipt.ResultID == nil || receipt.ResultVersion == nil || *receipt.ResultVersion != 1 || receipt.Route != "/tasks" {
		t.Fatalf("task view receipt=%#v", receipt)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE id=? AND name='本周 P2' AND version=1 AND schema_version=1", 1, *receipt.ResultID)

	options := *tool.(*aiWorkspaceTool)
	options.name = "workspace_task_views"
	list, err := options.Execute(context.Background(), []byte(`{"view":"list","limit":10}`))
	if err != nil || !strings.Contains(list, "本周 P2") || !strings.Contains(list, `"route":"/tasks"`) {
		t.Fatalf("task view list=%s err=%v", list, err)
	}
	detail, err := options.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"view","id":%q}`, *receipt.ResultID)))
	if err != nil || !strings.Contains(detail, `"priority":"P2"`) {
		t.Fatalf("task view detail=%s err=%v", detail, err)
	}
	if _, err := options.Execute(context.Background(), []byte(`{"view":"view","id":"not-a-uuid"}`)); err == nil {
		t.Fatal("task view detail must require a canonical UUID")
	}
}

func TestAITaskViewUpdateUsesVersionAndPreviewsPatch(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	view := createNativeTaskView(t, router, "Old name", map[string]any{"status": "active"})
	other := createNativeTaskView(t, router, "Other", map[string]any{"priority": "P3"})

	update := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task_view.update","task_saved_view_id":%q,"expected_version":%d,"changes":{"name":"新名称","definition":{"priority":"P0","sort":"-due_date"}}}`, view.ID, view.Version))
	if !strings.Contains(update.PreviewJSON, `"name":"新名称"`) || !strings.Contains(update.PreviewJSON, `"next_version":2`) {
		t.Fatalf("update preview=%s", update.PreviewJSON)
	}
	// Version drift is refused while proposing, before any write.
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"action":"task_view.update","task_saved_view_id":%q,"expected_version":9,"changes":{"name":"过期"}}`, other.ID))); err == nil || !strings.Contains(err.Error(), "VERSION_CONFLICT") {
		t.Fatalf("stale update must be refused: %v", err)
	}
	finishAIGeneration(t, store, generation)
	updated := decodeTestActionResponse(t, decideTestAction(t, router, update, ""))
	if updated.ResultID == nil || *updated.ResultID != view.ID || updated.ResultVersion == nil || *updated.ResultVersion != 2 {
		t.Fatalf("update receipt=%#v", updated)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE id=? AND name='新名称' AND version=2", 1, view.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE definition_json LIKE '%P0%' AND definition_json LIKE '%-due_date%'", 1)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE id=? AND name='Other' AND version=1", 1, other.ID)
}

func TestAITaskViewDeleteNeedsSeparateConsentAndIsIdempotent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	view := createNativeTaskView(t, router, "Disposable", map[string]any{"status": "active"})
	deleteRow := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"task_view.delete","task_saved_view_id":%q,"expected_version":%d,"changes":{}}`, view.ID, view.Version))
	finishAIGeneration(t, store, generation)
	missing := decideTestAction(t, router, deleteRow, "")
	assertAPIError(t, missing, http.StatusUnprocessableEntity, "TASK_VIEW_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE id=?", 1, view.ID)

	confirmed := decodeTestActionResponse(t, decideTestAction(t, router, deleteRow, `,"confirm_task_view_delete":true`))
	if confirmed.ResultID == nil || *confirmed.ResultID != view.ID {
		t.Fatalf("delete receipt=%#v", confirmed)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views WHERE id=?", 0, view.ID)

	replay := decideTestAction(t, router, deleteRow, `,"confirm_task_view_delete":true`)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
}

func TestAITaskViewRejectsInvalidDefinitionsTargetsAndLimits(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	view := createNativeTaskView(t, router, "Existing", map[string]any{"status": "active"})
	for _, test := range []struct {
		label   string
		payload string
	}{
		{"missing name", `{"action":"task_view.create","changes":{"definition":{"status":"active"}}}`},
		{"blank name", `{"action":"task_view.create","changes":{"name":"  ","definition":{"status":"active"}}}`},
		{"unknown definition field", `{"action":"task_view.create","changes":{"name":"x","definition":{"unsupported":"y"}}}`},
		{"date range conflict", `{"action":"task_view.create","changes":{"name":"x","definition":{"planned_date":"2026-09-21","planned_from":"2026-09-21"}}}`},
		{"bad priority", `{"action":"task_view.create","changes":{"name":"x","definition":{"priority":"P9"}}}`},
		{"bad project id", `{"action":"task_view.create","changes":{"name":"x","definition":{"project_id":"nope"}}}`},
		{"bad sort", `{"action":"task_view.create","changes":{"name":"x","definition":{"sort":"secret"}}}`},
		{"create with target", fmt.Sprintf(`{"action":"task_view.create","task_saved_view_id":%q,"expected_version":1,"changes":{"name":"x","definition":{}}}`, view.ID)},
		{"create change null name", `{"action":"task_view.create","changes":{"name":null,"definition":{}}}`},
		{"update without changes", fmt.Sprintf(`{"action":"task_view.update","task_saved_view_id":%q,"expected_version":1,"changes":{}}`, view.ID)},
		{"update target only", fmt.Sprintf(`{"action":"task_view.update","expected_version":1,"changes":{"name":"x"}}`)},
		{"delete with changes", fmt.Sprintf(`{"action":"task_view.delete","task_saved_view_id":%q,"expected_version":1,"changes":{"name":"x"}}`, view.ID)},
		{"duplicate name", `{"action":"task_view.create","changes":{"name":"Existing","definition":{"status":"active"}}}`},
	} {
		if _, err := tool.Execute(context.Background(), []byte(test.payload)); err == nil {
			t.Fatalf("%s must be rejected", test.label)
		}
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views", 1)

	// The native limit of 20 presets is enforced before a proposal is stored.
	for index := 0; index < maxTaskSavedViews-1; index++ {
		createNativeTaskView(t, router, fmt.Sprintf("view-%02d", index), map[string]any{"status": "active"})
	}
	if _, err := tool.Execute(context.Background(), []byte(`{"action":"task_view.create","changes":{"name":"one too many","definition":{"status":"active"}}}`)); err == nil || !strings.Contains(err.Error(), "TASK_SAVED_VIEW_LIMIT_REACHED") {
		t.Fatalf("limit must be enforced: %v", err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_saved_views", int64(maxTaskSavedViews))
}
