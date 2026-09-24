package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

const projectTaskSelectedBody = "SELECTED_PREDECESSOR_EVIDENCE: 调研事实已经由负责人验收。"
const projectTaskUnselectedBody = "UNSELECTED_PREDECESSOR_PRIVATE: 这个产出没有获准外发。"

type projectTaskFilesFixture struct {
	*anthropicAgentRunFixture
	SourceTask       models.Task
	SourceRun        models.AgentRun
	SourceSubmission models.TaskSubmission
	SourceArtifact   models.TaskArtifact
}

// A is delivered by a real controlled executor and accepted through the native
// owner-review command. B is a distinct, legally assigned Task in the Project.
func newProjectTaskFilesFixture(t *testing.T, next func(int, map[string]any, http.ResponseWriter)) *projectTaskFilesFixture {
	t.Helper()
	f := newAnthropicAgentRunFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		if call == 1 {
			bodies, _ := json.Marshal([]string{projectTaskSelectedBody, projectTaskUnselectedBody})
			writeAnthropicAgentRunResponse(w, string(bodies), "end_turn")
			return
		}
		next(call, request, w)
	})
	f.addProject(t)
	run := f.wait(t, f.create(t, `,"output_contract":{"type":"files","files":[{"name":"research.md","mime":"text/markdown"},{"name":"unselected.md","mime":"text/markdown"}]}`).ID)
	if run.OutputDeliveryStatus != agentRunOutputSubmitted || run.ArtifactID == nil || run.SubmissionID == nil {
		t.Fatalf("predecessor did not produce real output: %#v", run)
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	review := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/review", []byte(`{"decision":"accept"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if review.Code != http.StatusOK {
		t.Fatalf("accept predecessor=%d: %s", review.Code, review.Body.String())
	}
	f.Task = decodeReviewOutputResponse(t, review.Body.Bytes()).Task
	result := &projectTaskFilesFixture{anthropicAgentRunFixture: f, SourceTask: f.Task, SourceRun: run}
	if err := f.Store.DB.First(&result.SourceSubmission, "id=?", *run.SubmissionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&result.SourceArtifact, "id=?", *run.ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if result.SourceTask.Status != "done" || result.SourceSubmission.Status != "accepted" {
		t.Fatal("predecessor was not actually accepted")
	}
	now := f.Service.options.Now().Format("2006-01-02T15:04:05Z07:00")
	target := models.Task{ID: uuid.NewString(), ProjectID: f.Task.ProjectID, Title: "Write the follow-on proposal", Description: "Use only explicitly selected accepted predecessor evidence.", CompletionCriteria: "A reviewable follow-on proposal.", Kind: "work", Status: "todo", ReviewPolicy: "manual", Priority: "P1", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := f.Store.DB.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	assignment := models.TaskAssignment{ID: uuid.NewString(), TaskID: target.ID, ActorID: f.Actor.ID, Role: "assignee", AssignedByActorID: models.BuiltinOwnerActorID, AssignedAt: now, Reason: "explicit successor assignment"}
	if err := f.Store.DB.Create(&assignment).Error; err != nil {
		t.Fatal(err)
	}
	f.Task = target
	addAgentRunOwnerReviewer(t, f.aiAgentRunFixture)
	// API responses can include derived Task fields; freeze persisted records
	// for identity comparisons instead of comparing a response projection.
	result.SourceTask = models.Task{}
	if err := f.Store.DB.First(&result.SourceTask, "id=?", run.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&f.Task, "id=?", target.ID).Error; err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *projectTaskFilesFixture) sourceIdentity() map[string]any {
	return map[string]any{"project_id": *f.SourceTask.ProjectID, "task_id": f.SourceTask.ID, "task_title": f.SourceTask.Title, "task_version": f.SourceTask.Version, "submission_id": f.SourceSubmission.ID, "submission_sequence": f.SourceSubmission.Sequence}
}

func (f *projectTaskFilesFixture) nativeFiles() []map[string]any {
	return []map[string]any{{"source_kind": "project_task_artifact", "id": f.SourceArtifact.ID, "sha256": *f.SourceArtifact.SHA256, "source_task": f.sourceIdentity()}}
}

func (f *projectTaskFilesFixture) fileConsents() map[string]any {
	return map[string]any{"confirm_file_access": true, "confirm_project_task_files": true, "file_access_provider_confirmation": map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind}}
}

func (f *projectTaskFilesFixture) nativeBody() map[string]any {
	body := f.fileConsents()
	body["provider_id"] = f.Provider.ID
	body["input_files"] = f.nativeFiles()
	return body
}

func (f *projectTaskFilesFixture) createSuccessor(t *testing.T, body map[string]any) models.AgentRun {
	t.Helper()
	encoded, _ := json.Marshal(body)
	response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", encoded, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("accepted predecessor file must be usable by explicit successor: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Data models.AgentRun `json:"data"`
	}
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.ID == "" || result.Data.ExecutionContractVersion != 6 {
		t.Fatalf("new source was not frozen with v6: %s", response.Body.String())
	}
	for _, field := range []string{"source_files", "input_files", "model_protocol", "max_output_tokens", "execution_provider_confirmation", "rework_context"} {
		if strings.Contains(response.Body.String(), `"`+field+`"`) {
			t.Fatalf("create leaked detail-only %s", field)
		}
	}
	return result.Data
}

func (f *projectTaskFilesFixture) assertSourceUnchanged(t *testing.T) {
	t.Helper()
	var task models.Task
	var submission models.TaskSubmission
	var artifact models.TaskArtifact
	var run models.AgentRun
	for _, pair := range []struct {
		value any
		id    string
	}{{&task, f.SourceTask.ID}, {&submission, f.SourceSubmission.ID}, {&artifact, f.SourceArtifact.ID}, {&run, f.SourceRun.ID}} {
		if err := f.Store.DB.First(pair.value, "id=?", pair.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(task, f.SourceTask) || !reflect.DeepEqual(submission, f.SourceSubmission) || !reflect.DeepEqual(artifact, f.SourceArtifact) || !reflect.DeepEqual(run, f.SourceRun) {
		t.Fatal("successor mutated the accepted predecessor or its execution history")
	}
}

func assertProjectTaskSelectedRequest(t *testing.T, request map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(request)
	for _, want := range []string{projectTaskSelectedBody, "research.md"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("successor did not receive exact selected evidence %q", want)
		}
	}
	for _, forbidden := range []string{projectTaskUnselectedBody, "objects/", "relative_path", "PRIVATE ACTOR NOTES"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("successor received unselected or private data %q", forbidden)
		}
	}
}

func (f *projectTaskFilesFixture) useOpenAISuccessor(t *testing.T, kind string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Calls.Add(1)
		if r.URL.Path != "/chat/completions" || r.Header.Get("x-api-key") != "" {
			t.Errorf("OpenAI successor used wrong protocol: %s", r.URL.Path)
		}
		var request map[string]any
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid successor request")
		}
		if _, exists := request["max_tokens"]; exists {
			t.Error("legacy OpenAI request acquired Anthropic output budget")
		}
		assertProjectTaskSelectedRequest(t, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Successor proposal ready for human review."},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	if err := f.Store.DB.Model(&f.Provider).Updates(map[string]any{"protocol": "openai_chat", "kind": kind, "base_url": server.URL, "version": f.Provider.Version + 1, "config_version": f.Provider.ConfigVersion + 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.Store.DB.First(&f.Provider, "id=?", f.Provider.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunProjectTaskFilesAcceptedPredecessorReachesRealSuccessor(t *testing.T) {
	for _, protocol := range []string{"anthropic_messages", "openai_chat_local", "openai_chat_remote"} {
		t.Run(protocol, func(t *testing.T) {
			f := newProjectTaskFilesFixture(t, func(_ int, request map[string]any, w http.ResponseWriter) {
				assertProjectTaskSelectedRequest(t, request)
				writeAnthropicAgentRunResponse(w, "The successor proposal is ready for owner review.", "end_turn")
			})
			wantProtocol, wantTokens := "anthropic_messages", 8192
			if strings.HasPrefix(protocol, "openai_chat_") {
				f.useOpenAISuccessor(t, strings.TrimPrefix(protocol, "openai_chat_"))
				wantProtocol, wantTokens = "openai_chat", 0
			}
			run := f.wait(t, f.createSuccessor(t, f.nativeBody()).ID)
			if run.Status != "succeeded" || run.OutputDeliveryStatus != agentRunOutputSubmitted || run.SubmissionID == nil || run.ArtifactID == nil || f.Calls.Load() != 2 || run.ParentRunID != nil {
				t.Fatalf("successor did not submit exactly once independently: %#v", run)
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='pending_review'", 1, f.Task.ID)
			var frozen map[string]any
			if json.Unmarshal([]byte(run.InputSnapshotJSON), &frozen) != nil || frozen["model_protocol"] != wantProtocol || frozen["max_output_tokens"] != float64(wantTokens) {
				t.Fatalf("v6 did not freeze complete protocol: %s", run.InputSnapshotJSON)
			}
			for _, forbidden := range []string{projectTaskSelectedBody, projectTaskUnselectedBody, "objects/", anthropicRunTestKey} {
				if strings.Contains(run.InputSnapshotJSON, forbidden) {
					t.Fatalf("snapshot leaked %q", forbidden)
				}
			}
			detail := performRequest(f.Native, http.MethodGet, "/api/v1/agent-runs/"+run.ID, nil, nil)
			var response struct {
				Data map[string]any `json:"data"`
			}
			if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &response) != nil || response.Data["model_protocol"] != wantProtocol || response.Data["max_output_tokens"] != float64(wantTokens) || !strings.Contains(detail.Body.String(), f.SourceTask.ID) || !strings.Contains(detail.Body.String(), f.SourceSubmission.ID) || !strings.Contains(detail.Body.String(), *f.SourceArtifact.SHA256) {
				t.Fatalf("v6 detail omitted frozen source/protocol: %d %s", detail.Code, detail.Body.String())
			}
			if response.Data["execution_provider_confirmation"] == nil || response.Data["input_files"] == nil {
				t.Fatal("v6 detail cannot support complete human retry review")
			}
			list := performRequest(f.Router, http.MethodGet, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", nil, nil)
			if list.Code != 200 || strings.Contains(list.Body.String(), `"source_files"`) || strings.Contains(list.Body.String(), `"source_task"`) {
				t.Fatalf("list leaked detail-only predecessor proof: %d %s", list.Code, list.Body.String())
			}
			f.assertSourceUnchanged(t)
		})
	}
}

func (f *projectTaskFilesFixture) prepareSuccessorQueued(t *testing.T) models.AgentRun {
	t.Helper()
	encoded, _ := json.Marshal(f.nativeFiles())
	var files []agentRunInputFileRequest
	if err := json.Unmarshal(encoded, &files); err != nil {
		t.Fatal(err)
	}
	var run models.AgentRun
	if err := f.Store.DB.Transaction(func(tx *gorm.DB) error {
		prepared, err := prepareAgentRun(tx, prepareAgentRunInput{TaskID: f.Task.ID, ProviderID: f.Provider.ID, InputFiles: files, ArtifactStore: f.Artifacts})
		if err != nil {
			return err
		}
		run, err = createAgentRunInTransaction(tx, prepared, models.BuiltinOwnerActorID, "project-task-file-isolated", f.Service.options.Now().Format(time.RFC3339Nano))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run.ExecutionContractVersion != 6 {
		t.Fatal("predecessor evidence did not require v6")
	}
	return run
}

func TestAgentRunProjectTaskFilesRequireExactSourceAndIndependentConsent(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("invalid consent/identity must not invoke successor")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"missing project consent", func(b map[string]any) { delete(b, "confirm_project_task_files") }},
		{"missing ordinary file consent", func(b map[string]any) { delete(b, "confirm_file_access") }},
		{"missing provider binding", func(b map[string]any) { delete(b, "file_access_provider_confirmation") }},
		{"wrong provider binding", func(b map[string]any) {
			b["file_access_provider_confirmation"].(map[string]any)["version"] = f.Provider.Version + 1
		}},
		{"missing owner proof", func(b map[string]any) { delete(b["input_files"].([]map[string]any)[0], "source_task") }},
		{"missing hash", func(b map[string]any) { delete(b["input_files"].([]map[string]any)[0], "sha256") }},
		{"wrong hash", func(b map[string]any) { b["input_files"].([]map[string]any)[0]["sha256"] = strings.Repeat("0", 64) }},
		{"wrong source Task", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["task_id"] = f.Task.ID
		}},
		{"wrong source Project", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["project_id"] = uuid.NewString()
		}},
		{"wrong source batch", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["submission_id"] = uuid.NewString()
		}},
		{"wrong source sequence", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["submission_sequence"] = f.SourceSubmission.Sequence + 1
		}},
		{"old source version", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["task_version"] = f.SourceTask.Version - 1
		}},
		{"changed title", func(b map[string]any) {
			b["input_files"].([]map[string]any)[0]["source_task"].(map[string]any)["task_title"] = "Unverified title"
		}},
		{"old source kind cannot be widened", func(b map[string]any) {
			b["input_files"] = []map[string]any{{"source_kind": "task_artifact", "id": f.SourceArtifact.ID}}
			delete(b, "confirm_project_task_files")
		}},
		{"duplicate selection", func(b map[string]any) { b["input_files"] = append(f.nativeFiles(), f.nativeFiles()...) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := f.nativeBody()
			test.mutate(body)
			encoded, _ := json.Marshal(body)
			response := performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", encoded, nil)
			shapeRejected := (test.name == "missing owner proof" || test.name == "missing hash") && response.Code == http.StatusBadRequest && strings.Contains(response.Body.String(), `"code":"INVALID_JSON"`)
			if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusConflict && !shapeRejected {
				t.Fatalf("unsafe create=%d %s", response.Code, response.Body.String())
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
			if f.Calls.Load() != 1 {
				t.Fatal("unapproved evidence caused model HTTP")
			}
		})
	}
	f.assertSourceUnchanged(t)
}

func TestAgentRunProjectTaskFilesPrelaunchRechecksSourceAndDeletionLocks(t *testing.T) {
	for _, change := range []string{"version", "project", "reopen", "soft_delete", "cancel_unlock"} {
		t.Run(change, func(t *testing.T) {
			f := newProjectTaskFilesFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
				t.Error("changed/ cancelled source must not invoke successor")
				writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
			})
			run := f.prepareSuccessorQueued(t)
			for _, path := range []string{"/api/v1/artifacts/" + f.SourceArtifact.ID + "?confirm=true", "/api/v1/tasks/" + f.SourceTask.ID} {
				response := performRequest(f.Router, http.MethodDelete, path, []byte(`{"reason":"explicit removal"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version), "Idempotency-Key": "project-source-active-" + uuid.NewString()})
				code := "AGENT_RUN_FILE_REFERENCED"
				if strings.Contains(path, "/tasks/") {
					code = "TASK_HAS_ACTIVE_AGENT_RUN"
				}
				if response.Code != 409 || !strings.Contains(response.Body.String(), code) {
					t.Fatalf("active successor failed to protect source deletion: %d %s", response.Code, response.Body.String())
				}
			}
			switch change {
			case "version":
				if err := f.Store.DB.Model(&models.Task{}).Where("id=?", f.SourceTask.ID).Updates(map[string]any{"title": "Updated source heading", "version": f.SourceTask.Version + 1}).Error; err != nil {
					t.Fatal(err)
				}
			case "project":
				if err := f.Store.DB.Model(&models.Task{}).Where("id=?", f.SourceTask.ID).Updates(map[string]any{"project_id": nil, "version": f.SourceTask.Version + 1}).Error; err != nil {
					t.Fatal(err)
				}
			case "reopen":
				response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.SourceTask.ID+"/reopen", []byte(`{}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version)})
				if response.Code != 200 {
					t.Fatalf("reopen fixture=%d %s", response.Code, response.Body.String())
				}
			case "soft_delete":
				// Hostile external history, not a bypass through the public delete API.
				if err := f.Store.DB.Model(&models.TaskArtifact{}).Where("id=?", f.SourceArtifact.ID).Updates(map[string]any{"deleted_at": f.SourceTask.UpdatedAt, "deleted_by_actor_id": models.BuiltinOwnerActorID, "delete_reason": "external deletion fixture"}).Error; err != nil {
					t.Fatal(err)
				}
			case "cancel_unlock":
				cancel := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+run.ID+"/cancel", nil, nil)
				if cancel.Code != 200 {
					t.Fatalf("cancel=%d %s", cancel.Code, cancel.Body.String())
				}
				response := performRequest(f.Router, http.MethodDelete, "/api/v1/artifacts/"+f.SourceArtifact.ID+"?confirm=true", []byte(`{"reason":"explicit removal after cancellation"}`), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.SourceTask.Version), "Idempotency-Key": "project-source-cancelled"})
				if response.Code != 200 && response.Code != 204 {
					t.Fatalf("cancel did not release source: %d %s", response.Code, response.Body.String())
				}
			}
			f.Service.executeAgentRun(context.Background(), run.ID)
			actual := f.wait(t, run.ID)
			if actual.Status != "failed" && !(change == "cancel_unlock" && actual.Status == "cancelled") {
				t.Fatalf("prelaunch accepted stale source: %#v", actual)
			}
			if f.Calls.Load() != 1 || actual.InputSnapshotJSON != run.InputSnapshotJSON {
				t.Fatal("prelaunch source change sent HTTP or rewrote frozen evidence")
			}
			assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
		})
	}
}

func TestAgentRunProjectTaskFilesFailedRetryPreservesSourceAndIndependentConsents(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		assertProjectTaskSelectedRequest(t, request)
		if call == 2 {
			writeAnthropicAgentRunResponse(w, "incomplete successor", "max_tokens")
		} else {
			writeAnthropicAgentRunResponse(w, "Complete successor after exact retry.", "end_turn")
		}
	})
	original := f.wait(t, f.createSuccessor(t, f.nativeBody()).ID)
	if original.Status != "failed" || original.ErrorCode == nil || *original.ErrorCode != "AGENT_MODEL_TRUNCATED" {
		t.Fatalf("not a real failed predecessor-input execution: %#v", original)
	}
	for _, missing := range []string{"confirm_file_access", "confirm_project_task_files", "file_access_provider_confirmation"} {
		body := f.fileConsents()
		delete(body, missing)
		encoded, _ := json.Marshal(body)
		response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+original.ID+"/retry", encoded, nil)
		if response.Code != 422 && response.Code != 409 {
			t.Fatalf("retry without %s=%d %s", missing, response.Code, response.Body.String())
		}
		assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 2)
	}
	encoded, _ := json.Marshal(f.fileConsents())
	response := performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+original.ID+"/retry", encoded, nil)
	var payload struct {
		Data models.AgentRun `json:"data"`
	}
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatalf("retry=%d %s", response.Code, response.Body.String())
	}
	child := f.wait(t, payload.Data.ID)
	if child.OutputDeliveryStatus != agentRunOutputSubmitted || child.ExecutionContractVersion != 6 || child.InputSnapshotJSON != original.InputSnapshotJSON || child.ParentRunID == nil || *child.ParentRunID != original.ID || child.Attempt <= original.Attempt || f.Calls.Load() != 3 {
		t.Fatalf("retry did not preserve exact v6 source: %#v", child)
	}
	var unchanged models.AgentRun
	if err := f.Store.DB.First(&unchanged, "id=?", original.ID).Error; err != nil || !reflect.DeepEqual(original, unchanged) {
		t.Fatal("retry rewrote failed source run")
	}
	response = performRequest(f.Native, http.MethodPost, "/api/v1/agent-runs/"+child.ID+"/retry", encoded, nil)
	assertAPIError(t, response, 409, "AGENT_RUN_NOT_RETRYABLE")
	f.assertSourceUnchanged(t)
}

func TestAgentRunProjectTaskFilesPendingRecoveryNeverRerunsSource(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(_ int, request map[string]any, w http.ResponseWriter) {
		assertProjectTaskSelectedRequest(t, request)
		writeAnthropicAgentRunResponse(w, "Complete persisted successor output.", "end_turn")
	})
	if err := f.Store.DB.Exec(`CREATE TRIGGER fail_project_source_delivery BEFORE INSERT ON workflow_events WHEN NEW.action='task_output_submitted' BEGIN SELECT RAISE(ABORT,'isolated successor delivery failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	created := f.createSuccessor(t, f.nativeBody())
	f.Service.agentRunWorkers.Wait()
	var pending models.AgentRun
	if err := f.Store.DB.First(&pending, "id=?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.OutputDeliveryStatus != agentRunOutputPending || pending.OutputDeliveryPendingText == nil || f.Calls.Load() != 2 {
		t.Fatalf("completed output not safely staged: %#v", pending)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 0, f.Task.ID)
	if err := f.Store.DB.Exec("DROP TRIGGER fail_project_source_delivery").Error; err != nil {
		t.Fatal(err)
	}
	var first models.AgentRun
	for index := range 2 {
		recovered, err := f.Service.recoverAgentRunOutput(pending.ID, f.Service.options.Now().Add(time.Minute))
		if err != nil || recovered.OutputDeliveryStatus != agentRunOutputSubmitted || recovered.InputSnapshotJSON != pending.InputSnapshotJSON {
			t.Fatalf("pending recovery=%#v err=%v", recovered, err)
		}
		if index == 0 {
			first = recovered
		} else if !reflect.DeepEqual(first, recovered) {
			t.Fatal("repeated recovery changed result identity")
		}
	}
	if f.Calls.Load() != 2 {
		t.Fatal("pending delivery recovery reran a model")
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=?", 1, f.Task.ID)
	f.assertSourceUnchanged(t)
}

func TestAgentRunProjectTaskFilesNativeDiscoveryAndPreviewAreMetadataOnly(t *testing.T) {
	f := newProjectTaskFilesFixture(t, func(_ int, _ map[string]any, w http.ResponseWriter) {
		t.Error("read-only source discovery must not execute")
		writeAnthropicAgentRunResponse(w, "unexpected", "end_turn")
	})
	var events int64
	if err := f.Store.DB.Model(&models.WorkflowEvent{}).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/tasks/" + f.Task.ID + "/agent-runs/project-task-files"
	response := performRequest(f.Router, http.MethodGet, base+"?query=research.md", nil, nil)
	var page struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Data) != 1 || page.Data[0]["id"] != f.SourceArtifact.ID || page.Meta["total"] != float64(1) || page.Meta["limit"] != float64(20) || page.Meta["next_offset"] != nil {
		t.Fatalf("exact source metadata=%d %s", response.Code, response.Body.String())
	}
	if source, ok := page.Data[0]["source_task"].(map[string]any); !ok || source["task_id"] != f.SourceTask.ID || source["submission_id"] != f.SourceSubmission.ID || source["task_version"] != float64(f.SourceTask.Version) {
		t.Fatal("discovery lost live source ownership")
	}
	encoded, _ := json.Marshal(map[string]any{"input_files": f.nativeFiles()})
	preview := performRequest(f.Router, http.MethodPost, base+"/preview", encoded, nil)
	var frozen struct {
		Data []map[string]any `json:"data"`
	}
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &frozen) != nil || len(frozen.Data) != 1 || frozen.Data[0]["sha256"] != *f.SourceArtifact.SHA256 || frozen.Data[0]["source_kind"] != "project_task_artifact" {
		t.Fatalf("exact source preview=%d %s", preview.Code, preview.Body.String())
	}
	for _, raw := range []string{response.Body.String(), preview.Body.String()} {
		for _, forbidden := range []string{projectTaskSelectedBody, projectTaskUnselectedBody, "objects/", "relative_path", anthropicRunTestKey} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("source metadata leaked %q", forbidden)
			}
		}
	}
	for _, query := range []string{"?query=research.md&offset=1", "?query=%25", "?query=unmatched"} {
		result := performRequest(f.Router, http.MethodGet, base+query, nil, nil)
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &page) != nil || len(page.Data) != 0 || page.Meta["next_offset"] != nil {
			t.Fatalf("literal/empty page invalid: %d %s", result.Code, result.Body.String())
		}
	}
	for _, query := range []string{"?offset=01", "?offset=-1", "?offset=0&offset=1", "?limit=50", "?query=a&query=b", "?query=%00"} {
		result := performRequest(f.Router, http.MethodGet, base+query, nil, nil)
		if result.Code != 422 {
			t.Fatalf("ambiguous source query accepted: %s %d", query, result.Code)
		}
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM workflow_events", events)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM agent_runs", 1)
	if f.Calls.Load() != 1 {
		t.Fatal("discovery/preview invoked executor")
	}
	f.assertSourceUnchanged(t)
}

func TestAgentRunProjectTaskFilesReworkPreservesIndependentSourceAndReviewEvidence(t *testing.T) {
	const firstBody = "FIRST_SUCCESSOR_DELIVERABLE: 第一稿等待补全验收字段。"
	const reason = "PRECISE_SUCCESSOR_REWORK: 补全第四项方案验收字段。"
	f := newProjectTaskFilesFixture(t, func(call int, request map[string]any, w http.ResponseWriter) {
		assertProjectTaskSelectedRequest(t, request)
		if call == 2 {
			writeAnthropicAgentRunResponse(w, firstBody, "end_turn")
			return
		}
		raw, _ := json.Marshal(request)
		if !strings.Contains(string(raw), firstBody) || !strings.Contains(string(raw), reason) {
			t.Error("v6 rework lost the explicitly selected current returned batch")
		}
		writeAnthropicAgentRunResponse(w, "Reworked successor proposal with corrected acceptance evidence.", "end_turn")
	})
	first := f.wait(t, f.createSuccessor(t, f.nativeBody()).ID)
	if first.OutputDeliveryStatus != agentRunOutputSubmitted || first.SubmissionID == nil || first.ArtifactID == nil {
		t.Fatal("first successor was not actually delivered")
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/review", []byte(fmt.Sprintf(`{"decision":"request_changes","reason":%q}`, reason)), map[string]string{"If-Match": fmt.Sprintf(`"%d"`, f.Task.Version)})
	if response.Code != 200 {
		t.Fatalf("real successor review=%d %s", response.Code, response.Body.String())
	}
	if err := f.Store.DB.First(&f.Task, "id=?", f.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	body := f.nativeBody()
	body["rework"] = map[string]any{"submission_id": *first.SubmissionID, "artifact_ids": []string{*first.ArtifactID}, "expected_task_version": f.Task.Version}
	body["rework_provider_confirmation"] = map[string]any{"version": f.Provider.Version, "config_version": f.Provider.ConfigVersion, "kind": f.Provider.Kind}
	encoded, _ := json.Marshal(body)
	response = performRequest(f.Native, http.MethodPost, "/api/v1/tasks/"+f.Task.ID+"/agent-runs", encoded, nil)
	if response.Code != 422 {
		t.Fatalf("project file consent replaced rework consent: %d %s", response.Code, response.Body.String())
	}
	if f.Calls.Load() != 2 {
		t.Fatal("missing rework consent sent model request")
	}
	body["confirm_rework_context"] = true
	reworked := f.wait(t, f.createSuccessor(t, body).ID)
	if reworked.ExecutionContractVersion != 6 || reworked.OutputDeliveryStatus != agentRunOutputSubmitted || reworked.ParentRunID != nil || reworked.SubmissionID == nil || *reworked.SubmissionID == *first.SubmissionID || f.Calls.Load() != 3 {
		t.Fatalf("v6 rework did not create a new independent review batch: %#v", reworked)
	}
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE id=? AND status='changes_requested' AND review_reason=?", 1, *first.SubmissionID, reason)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM task_submissions WHERE task_id=? AND status='pending_review'", 1, f.Task.ID)
	assertDatabaseCount(t, f.Store, "SELECT COUNT(*) FROM tasks WHERE id=? AND status='waiting_review' AND completed_at IS NULL", 1, f.Task.ID)
	f.assertSourceUnchanged(t)
}
