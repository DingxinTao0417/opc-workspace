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

func TestAITagCreateRequiresConfirmationAndReturnsTaskRoute(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	row := proposeTestAction(t, store, tool, `{"action":"tag.create","changes":{"name":" Urgent ","color":"#aa11cc"}}`)
	if !strings.Contains(row.PreviewJSON, `"name":"Urgent"`) || !strings.Contains(row.PreviewJSON, `"color":"#AA11CC"`) {
		t.Fatalf("non-canonical preview: %s", row.PreviewJSON)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE name=?", 0, "Urgent")
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	var output struct {
		Data aiActionResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil || output.Data.ResultID == nil || output.Data.ResultVersion == nil || *output.Data.ResultVersion != 1 || output.Data.Route != "/tasks" {
		t.Fatalf("result=%s err=%v", response.Body.String(), err)
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE id=? AND name=? AND color=? AND version=1", 1, *output.Data.ResultID, "Urgent", "#AA11CC")
	options := *tool.(*aiWorkspaceTool)
	options.name = "workspace_task_options"
	result, err := options.Execute(context.Background(), []byte(`{"type":"tag","query":"Urgent"}`))
	if err != nil || !strings.Contains(result, `"color":"#AA11CC"`) || !strings.Contains(result, `"version":1`) {
		t.Fatalf("tag options=%s err=%v", result, err)
	}
}

func TestAITagUpdateBumpsLinkedTaskVersion(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tag := models.Tag{ID: uuid.NewString(), Name: "Old", Color: "#112233", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	task := models.Task{ID: uuid.NewString(), Title: "Tagged task", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec("INSERT INTO task_tags(task_id,tag_id) VALUES(?,?)", task.ID, tag.ID).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"tag.update","tag_id":%q,"expected_version":1,"changes":{"name":"Renamed","color":"#abcdef"}}`, tag.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", response.Code, response.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE id=? AND name=? AND color=? AND version=2", 1, tag.ID, "Renamed", "#ABCDEF")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=2", 1, task.ID)
}

func TestAITagDeleteRequiresConsentAndRejectsChangedImpact(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tag := models.Tag{ID: uuid.NewString(), Name: "Disposable", Color: "#445566", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"tag.delete","tag_id":%q,"expected_version":1,"changes":{}}`, tag.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	missingConsent := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
	assertAPIError(t, missingConsent, http.StatusUnprocessableEntity, "TAG_DELETE_CONFIRMATION_REQUIRED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE id=?", 1, tag.ID)

	task := models.Task{ID: uuid.NewString(), Title: "Late relation", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec("INSERT INTO task_tags(task_id,tag_id) VALUES(?,?)", task.ID, tag.ID).Error; err != nil {
		t.Fatal(err)
	}
	changed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_tag_delete":true}`, row.Fingerprint)), nil)
	assertAPIError(t, changed, http.StatusConflict, "AI_ACTION_PREVIEW_CHANGED")
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE id=?", 1, tag.ID)

	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE tag_id=?", 1, tag.ID)
}

func TestAITagDeleteDetachesTasksAfterExplicitConsent(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	tag := models.Tag{ID: uuid.NewString(), Name: "Disposable", Color: "#445566", Version: 1, CreatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&tag).Error; err != nil {
		t.Fatal(err)
	}
	task := models.Task{ID: uuid.NewString(), Title: "Tagged task", Description: "", Kind: "work", Status: "todo", ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", Version: 1, CreatedAt: "2026-09-18T12:00:00Z", UpdatedAt: "2026-09-18T12:00:00Z"}
	if err := store.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Exec("INSERT INTO task_tags(task_id,tag_id) VALUES(?,?)", task.ID, tag.ID).Error; err != nil {
		t.Fatal(err)
	}
	row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"tag.delete","tag_id":%q,"expected_version":1,"changes":{}}`, tag.ID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	confirmed := performRequest(router, http.MethodPost, "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm","confirm_tag_delete":true}`, row.Fingerprint)), nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm=%d %s", confirmed.Code, confirmed.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tags WHERE id=?", 0, tag.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM task_tags WHERE tag_id=?", 0, tag.ID)
	assertDatabaseCount(t, store, "SELECT COUNT(*) FROM tasks WHERE id=? AND version=2", 1, task.ID)
}
