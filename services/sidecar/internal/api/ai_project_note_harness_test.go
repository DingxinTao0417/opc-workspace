package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/opc-workspace/opc-sidecar/internal/harness"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAIProjectNoteHarnessAndReceipt(t *testing.T) {
	router, store, _ := newAIProviderTestRouter(t, time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	project := createProjectForTest(t, router, `{"name":"实际项目"}`, nil)
	r := performRequest(router, "POST", "/api/v1/projects/"+project.ID+"/notes", []byte(`{"title":"已有笔记","body":"用户原始事实","occurred_at":"2026-09-17T12:00:00Z"}`), nil)
	if r.Code != 201 {
		t.Fatal(r.Body.String())
	}
	note := decodeProjectNoteResponse(t, r.Body.Bytes())
	calls := 0
	upstream := newMockAIUpstream(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal(body)
		if calls <= 2 {
			name, args := "workspace_project_notes", fmt.Sprintf(`{"view":"detail","project_id":%q,"note_id":%q,"expected_version":1}`, project.ID, note.ID)
			if calls == 2 {
				if !strings.Contains(string(encoded), "用户原始事实") {
					t.Error("real note text missing")
				}
				name = "workspace_propose"
				args = fmt.Sprintf(`{"action":"project_note.update","project_note_id":%q,"expected_version":1,"changes":{"body":"用户修订的完整事实"}}`, note.ID)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprint("note-", calls), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\n", frame)
			return
		}
		if calls == 3 && !strings.Contains(string(encoded), "NOT executed") {
			t.Error("missing pending receipt")
		}
		if calls == 4 {
			if !strings.Contains(string(encoded), `\"result_id\":\"`+note.ID+`\"`) || !strings.Contains(string(encoded), `\"confirmed\"`) || !strings.Contains(string(encoded), aiProjectNoteRoute(project.ID, note.ID)) {
				t.Error("missing actual identity/receipt/route")
			}
			registered, _ := json.Marshal(body["tools"])
			if aiHasProtectedWorkspaceTool(registered) {
				t.Error("grant inherited")
			}
			if strings.Contains(string(encoded), "用户修订的完整事实") {
				t.Error("private preview body in receipt")
			}
		}
		streamMockAIDelta(w, `请核对笔记确认卡。[opc:selfcheck]{"sufficient":true}[/opc:selfcheck]`)
	})
	defer upstream.Close()
	provider := createReadyAIProvider(t, router, "note-harness", upstream.URL+"/v1", "model")
	var current models.AIProvider
	if err := store.DB.First(&current, "id=?", provider.ID).Error; err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"provider_id": provider.ID, "message": "修订项目笔记", "workspace": aiWorkspaceGrant{ProviderVersion: current.Version, Scopes: []string{"work", "actions"}}})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 3 || !strings.Contains(r.Body.String(), "event: done") {
		t.Fatal(r.Code, r.Body.String(), calls)
	}
	assertDatabaseCount(t, store, "SELECT count(*) FROM project_notes WHERE body='用户原始事实' AND version=1", 1)
	var proposal models.AIActionProposal
	if err := store.DB.First(&proposal).Error; err != nil {
		t.Fatal(err)
	}
	r = performRequest(router, "POST", "/api/v1/ai/actions/"+proposal.ID+"/decision", []byte(fmt.Sprintf(`{"fingerprint":%q,"decision":"confirm"}`, proposal.Fingerprint)), nil)
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	assertDatabaseCount(t, store, "SELECT count(*) FROM project_notes WHERE body='用户修订的完整事实' AND version=2", 1)
	var session models.AISession
	if err := store.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"provider_id": provider.ID, "session_id": session.ID, "message": "刚才修改了吗"})
	r = performRequest(router, "POST", "/api/v1/ai/chat", body, nil)
	if r.Code != 200 || calls != 4 {
		t.Fatal(r.Body.String(), calls)
	}
}

func TestAIProjectNoteScopeTimestampAndBodyCapacity(t *testing.T) {
	router, store, _, tool, _ := aiActionTestFixture(t)
	project := createProjectForTest(t, router, `{"name":"边界项目"}`, nil)
	args := fmt.Sprintf(`{"action":"project_note.create","changes":{"project_id":%q,"title":"笔记","body":"正文","occurred_at":"2026-09-18T12:06:00Z"}}`, project.ID)
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
		t.Fatal("future actual event accepted")
	}
	args = strings.ReplaceAll(args, "12:06:00Z", "12:00:00Z")
	concrete := tool.(*aiWorkspaceTool)
	for _, scopes := range [][]string{{"work"}, {"actions"}, {"finance", "finance_actions"}, {"clients", "actions"}} {
		concrete.policy = harness.NewCapabilities(scopes...)
		if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
			t.Fatal("insufficient scopes accepted", scopes)
		}
	}
	concrete.policy = harness.NewCapabilities("work", "actions")
	longArgs := strings.Replace(args, "正文", strings.Repeat("🙂", 10000), 1)
	p := proposeTestAction(t, store, tool, longArgs)
	var preview aiActionPreview
	if err := json.Unmarshal([]byte(p.PreviewJSON), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.After["body"] != strings.Repeat("🙂", 10000) {
		t.Fatal("full text truncated")
	}
	assertDatabaseCount(t, store, "SELECT count(*) FROM project_notes", 0)
	// No immutable permission grant for future requests or ephemeral sessions.
	if err := store.DB.Model(&models.AISession{}).Where("id=(SELECT session_id FROM ai_generations WHERE id=?)", concrete.generationID).Updates(map[string]any{"persist": false, "version": 2}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(context.Background(), []byte(args)); err == nil {
		t.Fatal("ephemeral proposal accepted")
	}
}
