package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeInboxSplitResponse(t *testing.T, body []byte) splitInboxItemResponse {
	t.Helper()
	var envelope struct {
		Data splitInboxItemResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Inbox split response: %v: %s", err, body)
	}
	return envelope.Data
}

func TestInboxSplitCreatesHierarchyAssignmentsRelationsAndReplays(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"拆分发布工作"}`, "")
	body := `{
		"resolution_policy":"all_required_tasks_done",
		"tasks":[
			{"key":"prepare","title":"准备发布资料","priority":"P1","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"},
			{"key":"publish","parent_key":"prepare","title":"完成正式发布","kind":"review","review_policy":"manual","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"},
			{"key":"note","title":"记录可选备注","is_required":false,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"}
		]
	}`
	response := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(body),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "split-release-1"},
	)
	if response.Code != http.StatusCreated || response.Header().Get("ETag") != `"2"` {
		t.Fatalf("split = %d headers=%v: %s", response.Code, response.Header(), response.Body.String())
	}
	created := decodeInboxSplitResponse(t, response.Body.Bytes())
	if created.InboxItem.Status != "tracking" || created.InboxItem.ResolutionPolicy != "all_required_tasks_done" ||
		created.InboxItem.Version != 2 || len(created.Created) != 3 || created.Progress.ActiveTotal != 3 ||
		created.Progress.RequiredTotal != 2 || created.Progress.RequiredDone != 0 {
		t.Fatalf("split response = %#v", created)
	}
	prepare := created.Created[0]
	publish := created.Created[1]
	if prepare.Key != "prepare" || prepare.Relation.RelationType != "created" || prepare.Relation.Position != 1 ||
		len(prepare.Assignments) != 1 || prepare.Assignments[0].Role != "assignee" || prepare.Task.Version != 2 {
		t.Fatalf("prepare output = %#v", prepare)
	}
	if publish.Task.ParentTaskID == nil || *publish.Task.ParentTaskID != prepare.Task.ID ||
		len(publish.Assignments) != 2 || publish.Assignments[1].Role != "reviewer" ||
		publish.Assignments[1].ActorID != models.BuiltinOwnerActorID || publish.Task.Version != 3 {
		t.Fatalf("publish output = %#v", publish)
	}
	var taskEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'task' AND aggregate_id IN ?", []string{prepare.Task.ID, publish.Task.ID, created.Created[2].Task.ID}).
		Count(&taskEvents).Error; err != nil || taskEvents != 7 {
		t.Fatalf("Task event count=%d err=%v", taskEvents, err)
	}

	replay := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(body),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "split-release-1"},
	)
	if replay.Code != http.StatusCreated || replay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeInboxSplitResponse(t, replay.Body.Bytes()).Created[0].Task.ID != prepare.Task.ID {
		t.Fatalf("split replay = %d headers=%v: %s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(`{"tasks":[{"key":"other","title":"另一任务","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "split-release-1"},
	)
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("split conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
}

func TestInboxAutomaticResolutionAndReopenFollowRequiredTaskLifecycle(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 16, 0, 0, 0, time.UTC)}
	router, _ := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"自动解决事项"}`, "")
	body := `{"tasks":[
		{"key":"one","title":"第一项必需工作","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"},
		{"key":"two","title":"第二项必需工作","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"}
	]}`
	splitResponse := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(body), map[string]string{"If-Match": `"1"`})
	if splitResponse.Code != http.StatusCreated {
		t.Fatalf("split = %d: %s", splitResponse.Code, splitResponse.Body.String())
	}
	split := decodeInboxSplitResponse(t, splitResponse.Body.Bytes())
	first := split.Created[0].Task
	second := split.Created[1].Task

	clock.Set(clock.now.Add(time.Second))
	completeFirst := performRequest(router, http.MethodPost, "/api/v1/tasks/"+first.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": `"2"`})
	if completeFirst.Code != http.StatusOK {
		t.Fatalf("complete first = %d: %s", completeFirst.Code, completeFirst.Body.String())
	}
	stillTracking := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID, nil, nil)
	if got := decodeInboxItemData(t, stillTracking.Body.Bytes()); got.Status != "tracking" || got.Version != 2 {
		t.Fatalf("Inbox after first completion = %#v", got)
	}

	clock.Set(clock.now.Add(time.Second))
	completeSecond := performRequest(router, http.MethodPost, "/api/v1/tasks/"+second.ID+"/complete", []byte(`{}`), map[string]string{"If-Match": `"2"`})
	if completeSecond.Code != http.StatusOK {
		t.Fatalf("complete second = %d: %s", completeSecond.Code, completeSecond.Body.String())
	}
	resolvedResponse := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID, nil, nil)
	resolved := decodeInboxItemData(t, resolvedResponse.Body.Bytes())
	if resolved.Status != "resolved" || resolved.ResolutionMode == nil || *resolved.ResolutionMode != "automatic" ||
		resolved.ResolvedByActorID == nil || *resolved.ResolvedByActorID != models.BuiltinSystemActorID || resolved.Version != 3 {
		t.Fatalf("automatically resolved Inbox = %#v", resolved)
	}

	clock.Set(clock.now.Add(time.Second))
	reopenTask := performRequest(router, http.MethodPost, "/api/v1/tasks/"+second.ID+"/reopen", []byte(`{}`), map[string]string{"If-Match": `"3"`})
	if reopenTask.Code != http.StatusOK {
		t.Fatalf("reopen Task = %d: %s", reopenTask.Code, reopenTask.Body.String())
	}
	reopenedResponse := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID, nil, nil)
	reopened := decodeInboxItemData(t, reopenedResponse.Body.Bytes())
	if reopened.Status != "tracking" || reopened.ResolutionMode != nil || reopened.ResolvedAt != nil || reopened.Version != 4 {
		t.Fatalf("automatically reopened Inbox = %#v", reopened)
	}
}

func TestInboxAutoPolicyBlocksManualResolveAndForceResolveIsAudited(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 17, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"需要例外结束"}`, "")
	body := `{"tasks":[{"key":"blocked","title":"尚未完成任务","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"}]}`
	splitResponse := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(body), map[string]string{"If-Match": `"1"`})
	if splitResponse.Code != http.StatusCreated {
		t.Fatalf("split = %d: %s", splitResponse.Code, splitResponse.Body.String())
	}
	manual := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/resolve", []byte(`{"reason":"直接结束"}`), map[string]string{"If-Match": `"2"`})
	if manual.Code != http.StatusConflict || responseErrorCode(t, manual.Body.Bytes()) != "INBOX_REQUIRED_TASKS_INCOMPLETE" {
		t.Fatalf("manual resolve = %d: %s", manual.Code, manual.Body.String())
	}
	missingConfirm := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/force-resolve", []byte(`{"confirm":false,"reason":"业务例外"}`), map[string]string{"If-Match": `"2"`})
	if missingConfirm.Code != http.StatusBadRequest || responseErrorCode(t, missingConfirm.Body.Bytes()) != "CONFIRMATION_REQUIRED" {
		t.Fatalf("force resolve confirmation = %d: %s", missingConfirm.Code, missingConfirm.Body.String())
	}
	forcedResponse := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/force-resolve", []byte(`{"confirm":true,"reason":"客户已线下取消，无需继续"}`),
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "force-resolve-1"},
	)
	if forcedResponse.Code != http.StatusOK || forcedResponse.Header().Get("ETag") != `"3"` {
		t.Fatalf("force resolve = %d: %s", forcedResponse.Code, forcedResponse.Body.String())
	}
	forced := decodeInboxItemData(t, forcedResponse.Body.Bytes())
	if forced.ResolutionMode == nil || *forced.ResolutionMode != "forced" || forced.ResolutionReason == nil ||
		*forced.ResolutionReason != "客户已线下取消，无需继续" || forced.ResolvedByActorID == nil || *forced.ResolvedByActorID != models.BuiltinOwnerActorID {
		t.Fatalf("forced Inbox = %#v", forced)
	}
	var forceEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'force_resolved'", item.ID).
		Count(&forceEvents).Error; err != nil || forceEvents != 1 {
		t.Fatalf("force event count=%d err=%v", forceEvents, err)
	}
	replay := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/force-resolve", []byte(`{"confirm":true,"reason":"客户已线下取消，无需继续"}`),
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "force-resolve-1"},
	)
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("force replay = %d: %s", replay.Code, replay.Body.String())
	}
}

func TestInboxSplitValidationAndTransactionRollback(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"拆分失败回滚"}`, "")
	unknownActorID := "018f0000-0000-7000-8000-000000001599"
	body := `{"tasks":[
		{"key":"valid","title":"不应保留的任务","is_required":true,"assignee_actor_id":"` + models.BuiltinOwnerActorID + `"},
		{"key":"invalid","parent_key":"valid","title":"非法负责人任务","is_required":true,"assignee_actor_id":"` + unknownActorID + `"}
	]}`
	response := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split", []byte(body), map[string]string{"If-Match": `"1"`})
	if response.Code != http.StatusUnprocessableEntity || responseErrorCode(t, response.Body.Bytes()) != "ACTOR_NOT_FOUND" {
		t.Fatalf("split invalid actor = %d: %s", response.Code, response.Body.String())
	}
	var taskCount int64
	if err := store.DB.Model(&models.Task{}).Where("title IN ?", []string{"不应保留的任务", "非法负责人任务"}).Count(&taskCount).Error; err != nil || taskCount != 0 {
		t.Fatalf("rolled back Task count=%d err=%v", taskCount, err)
	}
	current := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID, nil, nil)
	if got := decodeInboxItemData(t, current.Body.Bytes()); got.Version != 1 || got.Status != "open" || got.ResolutionPolicy != "manual" {
		t.Fatalf("Inbox after rollback = %#v", got)
	}
	forwardParent := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split",
		[]byte(`{"tasks":[{"key":"child","parent_key":"parent","title":"子任务","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"},{"key":"parent","title":"父任务","is_required":true,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`),
		map[string]string{"If-Match": `"1"`},
	)
	if forwardParent.Code != http.StatusUnprocessableEntity || responseErrorCode(t, forwardParent.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("forward parent = %d: %s", forwardParent.Code, forwardParent.Body.String())
	}
	allOptional := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/split",
		[]byte(`{"resolution_policy":"all_required_tasks_done","tasks":[{"key":"optional","title":"仅作参考任务","is_required":false,"assignee_actor_id":"`+models.BuiltinOwnerActorID+`"}]}`),
		map[string]string{"If-Match": `"1"`},
	)
	if allOptional.Code != http.StatusUnprocessableEntity || responseErrorCode(t, allOptional.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("automatic policy without required Tasks = %d: %s", allOptional.Code, allOptional.Body.String())
	}
}
