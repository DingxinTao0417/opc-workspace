package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"strings"
	"testing"
)

func TestAIProjectNoteLifecycleAndNativeFacts(t *testing.T) {
	router, store, _, tool, generation := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"真实项目"}`, nil)
	originalVersion := project.Version
	proposal := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project_note.create","changes":{"project_id":%q,"title":"记","body":"第一行\n完整正文","occurred_at":"2026-09-18T08:00:00Z"}}`, project.ID))
	assertDatabaseCount(t, store, "SELECT count(*) FROM project_notes", 0)
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decision := func(p models.AIActionProposal, extra string) aiActionResponse {
		t.Helper()
		r := performRequest(router, "POST", "/api/v1/ai/actions/"+p.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"%s}`, p.Fingerprint, extra)), nil)
		if r.Code != 200 {
			t.Fatal(r.Code, r.Body.String())
		}
		var out struct {
			Data aiActionResponse `json:"data"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data
	}
	result := decision(proposal, "")
	if result.ResultID == nil || result.ResultVersion == nil || *result.ResultVersion != 1 || result.Route != aiProjectNoteRoute(project.ID, *result.ResultID) {
		t.Fatal(result)
	}
	noteID := *result.ResultID
	decision(proposal, "")
	assertDatabaseCount(t, store, "SELECT count(*) FROM project_notes", 1)
	native := decodeProjectNoteResponse(t, performRequest(router, "GET", "/api/v1/project-notes/"+noteID, nil, nil).Body.Bytes())
	if native.Body == nil || *native.Body != "第一行\n完整正文" || native.CreatedBy.ID != models.BuiltinOwnerActorID || native.ProjectVersion != originalVersion+1 {
		t.Fatal(native)
	}
	// A separate generation is necessary: proposing requires a live saved run.
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	update := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"expected_version":1,"changes":{"title":"新标题"}}`, noteID))
	if !strings.Contains(update.PreviewJSON, "完整正文") {
		t.Fatal("must show full unchanged body even for title edits")
	}
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	decision(update, "")
	native = decodeProjectNoteResponse(t, performRequest(router, "GET", "/api/v1/project-notes/"+noteID, nil, nil).Body.Bytes())
	if native.Title != "新标题" || native.Version != 2 || native.ProjectVersion != originalVersion+2 {
		t.Fatal(native)
	}
	if err := store.DB.Model(&generation).Update("status", "streaming").Error; err != nil {
		t.Fatal(err)
	}
	deletion := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project_note.delete","project_note_id":%q,"expected_version":2,"changes":{"reason":"重复记录"}}`, noteID))
	if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	missing := performRequest(router, "POST", "/api/v1/ai/actions/"+deletion.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, deletion.Fingerprint)), nil)
	assertAPIError(t, missing, 422, "PROJECT_NOTE_DELETE_CONFIRMATION_REQUIRED")
	decision(deletion, `,"confirm_project_note_delete":true`)
	decision(deletion, `,"confirm_project_note_delete":true`)
	native = decodeProjectNoteResponse(t, performRequest(router, "GET", "/api/v1/project-notes/"+noteID, nil, nil).Body.Bytes())
	if native.Body != nil || native.DeletedAt == nil || native.Version != 3 || native.ProjectVersion != originalVersion+3 {
		t.Fatal(native)
	}
	assertDatabaseCount(t, store, "SELECT count(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 3)
	assertDatabaseCount(t, store, "SELECT count(*) FROM workflow_events WHERE aggregate_type='project'", 1) // Creation only; notes are not lifecycle events.
}

func TestAIProjectNotesReadPrivacyPagingAndRevocation(t *testing.T) {
	router, store, _, proposalTool, _ := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"笔记项目","description":"Not note metadata"}`, nil)
	long := strings.Repeat("中🙂", 2501)
	create := func(title, body string) projectNoteResponse {
		raw, _ := json.Marshal(createProjectNoteRequest{Title: title, Body: body, OccurredAt: "2026-09-17T12:00:00Z"})
		r := performRequest(router, "POST", "/api/v1/projects/"+project.ID+"/notes", raw, nil)
		if r.Code != 201 {
			t.Fatal(r.Body.String())
		}
		return decodeProjectNoteResponse(t, r.Body.Bytes())
	}
	note := create("长正文", long)
	deleted := create("已删", "不应泄露的删除正文")
	r := performRequest(router, "DELETE", "/api/v1/project-notes/"+deleted.ID+"?confirm=true", []byte(`{"reason":"重复"}`), map[string]string{"If-Match": `"1"`})
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	tool := *(proposalTool.(*aiWorkspaceTool))
	tool.name = "workspace_project_notes"
	read := func(args string) map[string]any {
		t.Helper()
		out, err := tool.Execute(context.Background(), []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if json.Unmarshal([]byte(out), &data) != nil {
			t.Fatal(out)
		}
		if strings.Contains(out, "Not note metadata") || strings.Contains(out, "不应泄露") || strings.Contains(out, "created_by") {
			t.Fatal("private fields leaked", out)
		}
		return data
	}
	list := read(fmt.Sprintf(`{"view":"list","project_id":%q,"limit":1}`, project.ID))
	items := list["items"].([]any)
	if len(items) != 1 || list["total"] != float64(1) || strings.Contains(fmt.Sprint(list), "中🙂") || items[0].(map[string]any)["route"] != aiProjectNoteRoute(project.ID, note.ID) {
		t.Fatal(list)
	}
	first := read(fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q}`, project.ID, note.ID))
	if first["body"] != string([]rune(long)[:4000]) || first["next_content_offset"] != float64(4000) || first["route"] != aiProjectNoteRoute(project.ID, note.ID) || first["note"].(map[string]any)["route"] != aiProjectNoteRoute(project.ID, note.ID) {
		t.Fatal(first)
	}
	last := read(fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q,"content_offset":4000}`, project.ID, note.ID))
	if last["body"] != string([]rune(long)[4000:]) || last["has_more"] != false {
		t.Fatal(last)
	}
	for _, args := range []string{
		fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q}`, project.ID, deleted.ID),
		fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q}`, uuid.NewString(), note.ID),
		fmt.Sprintf(`{"view":"list","project_id":%q,"note_id":%q}`, project.ID, note.ID),
		fmt.Sprintf(`{"view":"list","project_id":%q,"include_deleted":true}`, project.ID),
		fmt.Sprintf(`{"view":"list","project_id":%q,"offset":null}`, project.ID),
		fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q,"content_limit":4001}`, project.ID, note.ID),
	} {
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("accepted", args)
		}
	}
	otherProject := createProjectForTest(t, router, `{"name":"另一个真实项目"}`, nil)
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q}`, otherProject.ID, note.ID))); err == nil {
		t.Fatal("cross-project note accepted")
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"detail","project_id":%q,"note_id":%q}`, project.ID, note.ID))); err == nil {
		t.Fatal("unversioned body accepted")
	}
	edited := performRequest(router, "PATCH", "/api/v1/project-notes/"+note.ID, []byte(`{"body":"new revision"}`), map[string]string{"If-Match": `"1"`})
	if edited.Code != 200 {
		t.Fatal(edited.Body.String())
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q,"content_offset":4000}`, project.ID, note.ID))); err == nil {
		t.Fatal("old body page silently read a different revision")
	}
	updated := read(fmt.Sprintf(`{"view":"detail","expected_version":2,"project_id":%q,"note_id":%q}`, project.ID, note.ID))
	if updated["body"] != "new revision" {
		t.Fatal(updated)
	}
	escaped := create("转义正文", strings.Repeat("&", 10000))
	small := read(fmt.Sprintf(`{"view":"detail","expected_version":1,"project_id":%q,"note_id":%q,"content_limit":1000}`, project.ID, escaped.ID))
	if small["body"] != strings.Repeat("&", 1000) || small["next_content_offset"] != float64(1000) {
		t.Fatal("bounded escaped body incorrect")
	}
	tool.policy = harness.NewCapabilities("clients", "outputs")
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"list","project_id":%q}`, project.ID))); !errors.Is(err, harness.ErrPermissionDenied) {
		t.Fatal(err)
	}
	tool.policy = harness.NewCapabilities("work")
	tool.api.restorePending.Store(true)
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"list","project_id":%q}`, project.ID))); err == nil {
		t.Fatal("restore accepted")
	}
	tool.api.restorePending.Store(false)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Execute(cancelled, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.DB.Model(&models.AIProvider{}).Where("id=?", tool.providerID).Updates(map[string]any{"status": "disabled", "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(fmt.Sprintf(`{"view":"list","project_id":%q}`, project.ID))); err == nil {
		t.Fatal("provider revocation ignored")
	}
}

func TestAIProjectNoteStalePreviewRollbackAndStrictInput(t *testing.T) {
	for _, mode := range []string{"note_changed", "project_changed", "archived", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			router, store, _, tool, generation := aiActionTestFixture(t)
			project := createProjectForTest(t, router, `{"name":"审批项目"}`, nil)
			r := performRequest(router, "POST", "/api/v1/projects/"+project.ID+"/notes", []byte(`{"title":"原始","body":"完整旧正文","occurred_at":"2026-09-17T12:00:00Z"}`), nil)
			if r.Code != 201 {
				t.Fatal(r.Body.String())
			}
			note := decodeProjectNoteResponse(t, r.Body.Bytes())
			row := proposeTestAction(t, store, tool, fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"expected_version":1,"changes":{"body":"完整新正文"}}`, note.ID))
			switch mode {
			case "note_changed":
				r = performRequest(router, "PATCH", "/api/v1/project-notes/"+note.ID, []byte(`{"title":"人工已改"}`), map[string]string{"If-Match": `"1"`})
				if r.Code != 200 {
					t.Fatal(r.Body.String())
				}
			case "project_changed":
				if err := store.DB.Model(&models.Project{}).Where("id=?", project.ID).Updates(map[string]any{"name": "项目已改", "version": note.ProjectVersion + 1}).Error; err != nil {
					t.Fatal(err)
				}
			case "archived":
				if err := store.DB.Model(&models.Project{}).Where("id=?", project.ID).Updates(map[string]any{"status": "archived", "archived_from_status": "planning", "version": note.ProjectVersion + 1}).Error; err != nil {
					t.Fatal(err)
				}
			case "rollback":
				if err := store.DB.Exec("CREATE TRIGGER fail_note_approval BEFORE UPDATE ON ai_action_proposals BEGIN SELECT RAISE(ABORT,'forced approval failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := store.DB.Model(&generation).Update("status", "completed").Error; err != nil {
				t.Fatal(err)
			}
			r = performRequest(router, "POST", "/api/v1/ai/actions/"+row.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, row.Fingerprint)), nil)
			code := map[string]string{"note_changed": "VERSION_CONFLICT", "project_changed": "AI_ACTION_PREVIEW_CHANGED", "archived": "PROJECT_ARCHIVED"}
			if mode != "rollback" {
				assertAPIError(t, r, 409, code[mode])
			} else if r.Code != 500 {
				t.Fatal(r.Code, r.Body.String())
			}
			fresh := decodeProjectNoteResponse(t, performRequest(router, "GET", "/api/v1/project-notes/"+note.ID, nil, nil).Body.Bytes())
			if fresh.Body == nil || *fresh.Body != "完整旧正文" {
				t.Fatal("mutation escaped", fresh)
			}
			if mode == "rollback" && (fresh.Version != 1 || fresh.ProjectVersion != note.ProjectVersion) {
				t.Fatal("trigger escaped rollback", fresh)
			}
			assertDatabaseCount(t, store, "SELECT count(*) FROM ai_action_proposals WHERE status='pending'", 1)
			assertDatabaseCount(t, store, "SELECT count(*) FROM workflow_events WHERE action='ai_workspace_action_confirmed'", 0)
		})
	}
	id := uuid.NewString()
	for _, args := range []string{
		fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"expected_version":1,"changes":{"project_id":%q}}`, id, id),
		fmt.Sprintf(`{"action":"project_note.delete","project_note_id":%q,"expected_version":1,"changes":{"reason":"x","confirm":true}}`, id),
		fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"task_id":%q,"expected_version":1,"changes":{"title":"x"}}`, id, id),
		fmt.Sprintf(`{"action":"task.submit_output","project_note_id":%q,"changes":{}}`, id),
		fmt.Sprintf(`{"action":"project_note.create","project_note_id":%q,"changes":{"title":"x"}}`, id),
		fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"expected_version":1,"changes":{"body":null}}`, id),
	} {
		if _, err := parseAIWorkspaceAction([]byte(args)); err == nil {
			t.Fatal("accepted", args)
		}
	}
}
