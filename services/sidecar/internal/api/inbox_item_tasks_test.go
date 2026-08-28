package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func decodeInboxTaskMutationResponse(t *testing.T, body []byte) inboxTaskMutationResponse {
	t.Helper()
	var envelope struct {
		Data inboxTaskMutationResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Inbox Task mutation: %v: %s", err, body)
	}
	return envelope.Data
}

func decodeInboxTaskList(t *testing.T, body []byte) (inboxTaskListData, inboxTaskListMeta) {
	t.Helper()
	var envelope struct {
		Data inboxTaskListData `json:"data"`
		Meta inboxTaskListMeta `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode Inbox Task list: %v: %s", err, body)
	}
	return envelope.Data, envelope.Meta
}

func TestInboxTaskRelationLifecycleProgressIdempotencyAndHistory(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 12, 0, 0, 123, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"编排已有任务"}`, "")
	firstTask := createTaskForTaskFacts(t, router, `{"title":"核对现有交付"}`)
	secondTask := createTaskForTaskFacts(t, router, `{"title":"已取消但保留解释"}`)
	cancelled := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+secondTask.ID+"/cancel",
		[]byte(`{"reason":"不再执行"}`), map[string]string{"If-Match": `"1"`},
	)
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel Task = %d: %s", cancelled.Code, cancelled.Body.String())
	}

	linkURL := "/api/v1/inbox-items/" + item.ID + "/tasks/" + firstTask.ID
	linkedResponse := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "inbox-task-link-1"},
	)
	if linkedResponse.Code != http.StatusCreated || linkedResponse.Header().Get("ETag") != `"2"` {
		t.Fatalf("link Task = %d headers=%v: %s", linkedResponse.Code, linkedResponse.Header(), linkedResponse.Body.String())
	}
	linked := decodeInboxTaskMutationResponse(t, linkedResponse.Body.Bytes())
	if linked.InboxItem.Status != "tracking" || linked.InboxItem.Version != 2 || linked.InboxItem.ReadAt != nil ||
		linked.Relation.TaskRefID != firstTask.ID || linked.Relation.TaskID == nil || *linked.Relation.TaskID != firstTask.ID ||
		linked.Relation.RelationType != "linked" || !linked.Relation.IsRequired || linked.Relation.Position != 1 ||
		linked.Relation.LinkedByActor.ID != models.BuiltinOwnerActorID || linked.Relation.Task == nil ||
		linked.Relation.Task.Title != firstTask.Title || linked.Progress.ActiveTotal != 1 ||
		linked.Progress.RequiredTotal != 1 || linked.Progress.RequiredDone != 0 ||
		linked.Progress.RequiredRemaining != 1 || linked.Progress.Percent == nil || *linked.Progress.Percent != 0 ||
		linked.Progress.AllRequiredDone {
		t.Fatalf("linked response = %#v", linked)
	}

	replayed := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "inbox-task-link-1"},
	)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" ||
		decodeInboxTaskMutationResponse(t, replayed.Body.Bytes()).Relation.ID != linked.Relation.ID {
		t.Fatalf("link replay = %d headers=%v: %s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	conflictingReplay := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":false}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "inbox-task-link-1"},
	)
	if conflictingReplay.Code != http.StatusConflict || responseErrorCode(t, conflictingReplay.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("link replay conflict = %d: %s", conflictingReplay.Code, conflictingReplay.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	secondURL := "/api/v1/inbox-items/" + item.ID + "/tasks/" + secondTask.ID
	secondLinkedResponse := performRequest(
		router, http.MethodPost, secondURL, []byte(`{"is_required":false}`),
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "inbox-task-link-2"},
	)
	if secondLinkedResponse.Code != http.StatusCreated || secondLinkedResponse.Header().Get("ETag") != `"3"` {
		t.Fatalf("link second Task = %d: %s", secondLinkedResponse.Code, secondLinkedResponse.Body.String())
	}
	secondLinked := decodeInboxTaskMutationResponse(t, secondLinkedResponse.Body.Bytes())
	if secondLinked.Relation.Position != 2 || secondLinked.Relation.Task == nil || secondLinked.Relation.Task.Status != "cancelled" ||
		secondLinked.Progress.ActiveTotal != 2 || secondLinked.Progress.RequiredTotal != 1 || secondLinked.Progress.RequiredCancelled != 0 {
		t.Fatalf("second linked response = %#v", secondLinked)
	}

	clock.Set(clock.now.Add(time.Second))
	requiredResponse := performRequest(
		router, http.MethodPatch, secondURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "inbox-task-required-1"},
	)
	if requiredResponse.Code != http.StatusOK || requiredResponse.Header().Get("ETag") != `"4"` {
		t.Fatalf("change requirement = %d: %s", requiredResponse.Code, requiredResponse.Body.String())
	}
	required := decodeInboxTaskMutationResponse(t, requiredResponse.Body.Bytes())
	if !required.Relation.IsRequired || required.Progress.RequiredTotal != 2 || required.Progress.RequiredCancelled != 1 ||
		required.Progress.RequiredRemaining != 2 || required.Progress.Percent == nil || *required.Progress.Percent != 0 {
		t.Fatalf("required response = %#v", required)
	}
	noOp := performRequest(
		router, http.MethodPatch, secondURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"4"`, "Idempotency-Key": "inbox-task-required-noop"},
	)
	if noOp.Code != http.StatusOK || noOp.Header().Get("ETag") != `"4"` || decodeInboxTaskMutationResponse(t, noOp.Body.Bytes()).InboxItem.Version != 4 {
		t.Fatalf("requirement no-op = %d: %s", noOp.Code, noOp.Body.String())
	}
	stale := performRequest(
		router, http.MethodPatch, secondURL, []byte(`{"is_required":false}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "inbox-task-required-stale"},
	)
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale requirement = %d: %s", stale.Code, stale.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	unlinkedResponse := performRequest(
		router, http.MethodDelete, linkURL, []byte(`{"reason":"由第二条任务继续跟进"}`),
		map[string]string{"If-Match": `"4"`, "Idempotency-Key": "inbox-task-unlink-1"},
	)
	if unlinkedResponse.Code != http.StatusOK || unlinkedResponse.Header().Get("ETag") != `"5"` {
		t.Fatalf("unlink Task = %d: %s", unlinkedResponse.Code, unlinkedResponse.Body.String())
	}
	unlinked := decodeInboxTaskMutationResponse(t, unlinkedResponse.Body.Bytes())
	if unlinked.Relation.IsActive || unlinked.Relation.UnlinkedAt == nil || unlinked.Relation.UnlinkedByActor == nil ||
		unlinked.Relation.UnlinkedByActor.ID != models.BuiltinOwnerActorID || unlinked.Relation.UnlinkReason == nil ||
		*unlinked.Relation.UnlinkReason != "由第二条任务继续跟进" || unlinked.InboxItem.Status != "tracking" ||
		unlinked.Progress.ActiveTotal != 1 || unlinked.Progress.RequiredCancelled != 1 {
		t.Fatalf("unlinked response = %#v", unlinked)
	}
	unlinkedReplay := performRequest(
		router, http.MethodDelete, linkURL, []byte(`{"reason":"由第二条任务继续跟进"}`),
		map[string]string{"If-Match": `"4"`, "Idempotency-Key": "inbox-task-unlink-1"},
	)
	if unlinkedReplay.Code != http.StatusOK || unlinkedReplay.Header().Get("Idempotency-Replayed") != "true" ||
		decodeInboxTaskMutationResponse(t, unlinkedReplay.Body.Bytes()).Relation.ID != unlinked.Relation.ID {
		t.Fatalf("unlink replay = %d headers=%v: %s", unlinkedReplay.Code, unlinkedReplay.Header(), unlinkedReplay.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	relinkedResponse := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"5"`, "Idempotency-Key": "inbox-task-relink-1"},
	)
	if relinkedResponse.Code != http.StatusCreated || relinkedResponse.Header().Get("ETag") != `"6"` {
		t.Fatalf("relink Task = %d: %s", relinkedResponse.Code, relinkedResponse.Body.String())
	}
	relinked := decodeInboxTaskMutationResponse(t, relinkedResponse.Body.Bytes())
	if relinked.Relation.ID == linked.Relation.ID || relinked.Relation.Position != 3 {
		t.Fatalf("relinked relation = %#v", relinked.Relation)
	}

	listedResponse := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/tasks?page=1&page_size=1", nil, nil)
	if listedResponse.Code != http.StatusOK || listedResponse.Header().Get("ETag") != `"6"` {
		t.Fatalf("list relations = %d: %s", listedResponse.Code, listedResponse.Body.String())
	}
	listed, meta := decodeInboxTaskList(t, listedResponse.Body.Bytes())
	if len(listed.Active) != 2 || listed.Active[0].TaskRefID != secondTask.ID || listed.Active[0].Position != 2 ||
		listed.Active[1].TaskRefID != firstTask.ID || listed.Active[1].Position != 3 ||
		len(listed.History) != 1 || listed.History[0].ID != linked.Relation.ID ||
		meta.Total != 1 || meta.InboxItemVersion != 6 || meta.Progress.ActiveTotal != 2 ||
		meta.Progress.RequiredTotal != 2 || meta.Progress.RequiredCancelled != 1 {
		t.Fatalf("relation list data=%#v meta=%#v", listed, meta)
	}
	var firstTaskVersion int64
	if err := store.DB.Model(&models.Task{}).Select("version").Where("id = ?", firstTask.ID).Scan(&firstTaskVersion).Error; err != nil || firstTaskVersion != 1 {
		t.Fatalf("Inbox relations changed Task version=%d err=%v", firstTaskVersion, err)
	}
	for action, want := range map[string]int64{
		"task_linked": 3, "task_requirement_changed": 1, "task_unlinked": 1,
	} {
		var count int64
		if err := store.DB.Model(&models.WorkflowEvent{}).
			Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = ?", item.ID, action).
			Count(&count).Error; err != nil || count != want {
			t.Fatalf("%s event count=%d want=%d err=%v", action, count, want, err)
		}
	}
}

func TestInboxTaskProgressUsesOnlyActiveRequiredDoneTasks(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"多状态派生进度"}`, "")
	now := formatInboxTimestamp(clock.now)
	blockedReason := "等待输入"
	blockedFrom := "in_progress"
	statuses := []struct {
		status    string
		required  bool
		configure func(*models.Task)
	}{
		{status: "todo", required: true},
		{status: "done", required: true, configure: func(task *models.Task) { task.CompletedAt = &now }},
		{status: "blocked", required: true, configure: func(task *models.Task) {
			task.BlockedReason = &blockedReason
			task.BlockedAt = &now
			task.BlockedFromStatus = &blockedFrom
		}},
		{status: "waiting_review", required: true, configure: func(task *models.Task) {
			task.ReviewPolicy = "manual"
			task.SubmittedAt = &now
		}},
		{status: "cancelled", required: true},
		{status: "done", required: false, configure: func(task *models.Task) { task.CompletedAt = &now }},
	}
	for index, entry := range statuses {
		taskIDValue := uuid.NewString()
		task := models.Task{
			ID: taskIDValue, Title: fmt.Sprintf("状态任务 %d", index+1), Description: "", Kind: "work",
			Status: entry.status, ReviewPolicy: "none", Priority: "P2", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if entry.configure != nil {
			entry.configure(&task)
		}
		if err := store.DB.Create(&task).Error; err != nil {
			t.Fatalf("seed %s Task: %v", entry.status, err)
		}
		relation := models.InboxItemTask{
			ID: uuid.NewString(), InboxItemID: item.ID, TaskRefID: taskIDValue, TaskID: &taskIDValue,
			TaskTitleSnapshot: task.Title, RelationType: "linked", IsRequired: entry.required,
			Position: index + 1, LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: now,
		}
		if err := store.DB.Create(&relation).Error; err != nil {
			t.Fatalf("seed %s relation: %v", entry.status, err)
		}
	}
	response := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/tasks", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list progress = %d: %s", response.Code, response.Body.String())
	}
	data, meta := decodeInboxTaskList(t, response.Body.Bytes())
	if len(data.Active) != 6 || meta.Progress.ActiveTotal != 6 || meta.Progress.RequiredTotal != 5 ||
		meta.Progress.RequiredDone != 1 || meta.Progress.RequiredRemaining != 4 ||
		meta.Progress.RequiredBlocked != 1 || meta.Progress.RequiredWaitingReview != 1 ||
		meta.Progress.RequiredCancelled != 1 || meta.Progress.Percent == nil || *meta.Progress.Percent != 20 ||
		meta.Progress.AllRequiredDone {
		t.Fatalf("derived progress = %#v", meta.Progress)
	}

	emptyRequired := createInboxItemForTest(t, router, `{"title":"只有可选任务"}`, "")
	optionalTaskID := uuid.NewString()
	optionalTask := models.Task{
		ID: optionalTaskID, Title: "已完成可选任务", Description: "", Kind: "work", Status: "done",
		ReviewPolicy: "none", Priority: "P2", Version: 1, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&optionalTask).Error; err != nil {
		t.Fatalf("seed optional Task: %v", err)
	}
	optionalRelation := models.InboxItemTask{
		ID: uuid.NewString(), InboxItemID: emptyRequired.ID, TaskRefID: optionalTaskID, TaskID: &optionalTaskID,
		TaskTitleSnapshot: optionalTask.Title, RelationType: "linked", IsRequired: false, Position: 1,
		LinkedByActorID: models.BuiltinOwnerActorID, LinkedAt: now,
	}
	if err := store.DB.Create(&optionalRelation).Error; err != nil {
		t.Fatalf("seed optional relation: %v", err)
	}
	emptyResponse := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+emptyRequired.ID+"/tasks", nil, nil)
	_, emptyMeta := decodeInboxTaskList(t, emptyResponse.Body.Bytes())
	if emptyMeta.Progress.ActiveTotal != 1 || emptyMeta.Progress.RequiredTotal != 0 ||
		emptyMeta.Progress.RequiredRemaining != 0 || emptyMeta.Progress.Percent != nil || emptyMeta.Progress.AllRequiredDone {
		t.Fatalf("zero-required progress = %#v", emptyMeta.Progress)
	}
}

func TestInboxTaskDeleteProtectionReopenAndDeletedHistory(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"删除保护关系"}`, "")
	task := createTaskForTaskFacts(t, router, `{"title":"不能静默删除的任务"}`)
	linkURL := "/api/v1/inbox-items/" + item.ID + "/tasks/" + task.ID
	linked := performRequest(router, http.MethodPost, linkURL, []byte(`{"is_required":true}`), map[string]string{"If-Match": `"1"`})
	if linked.Code != http.StatusCreated {
		t.Fatalf("link Task = %d: %s", linked.Code, linked.Body.String())
	}
	if err := store.DB.Delete(&models.Task{}, "id = ?", task.ID).Error; err == nil || !strings.Contains(err.Error(), "TASK_HAS_ACTIVE_INBOX_RELATIONS") {
		t.Fatalf("database Task delete protection error = %v", err)
	}
	deleted := performRequest(router, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusConflict || responseErrorCode(t, deleted.Body.Bytes()) != "TASK_HAS_ACTIVE_INBOX_RELATIONS" {
		t.Fatalf("API Task delete protection = %d: %s", deleted.Code, deleted.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	resolved := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/resolve",
		[]byte(`{"reason":"暂时归档"}`), map[string]string{"If-Match": `"2"`},
	)
	if resolved.Code != http.StatusOK || decodeInboxItemData(t, resolved.Body.Bytes()).Status != "resolved" {
		t.Fatalf("resolve Inbox Item = %d: %s", resolved.Code, resolved.Body.String())
	}
	terminalUnlink := performRequest(
		router, http.MethodDelete, linkURL, []byte(`{"reason":"终态不可改关系"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if terminalUnlink.Code != http.StatusConflict || responseErrorCode(t, terminalUnlink.Body.Bytes()) != "INBOX_ITEM_TERMINAL" {
		t.Fatalf("terminal unlink = %d: %s", terminalUnlink.Code, terminalUnlink.Body.String())
	}
	clock.Set(clock.now.Add(time.Second))
	reopened := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/reopen", []byte(`{}`),
		map[string]string{"If-Match": `"3"`},
	)
	reopenedItem := decodeInboxItemData(t, reopened.Body.Bytes())
	if reopened.Code != http.StatusOK || reopenedItem.Status != "tracking" || reopenedItem.Version != 4 {
		t.Fatalf("reopen linked Inbox Item = %d: %s", reopened.Code, reopened.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	unlinked := performRequest(
		router, http.MethodDelete, linkURL, []byte(`{"reason":"允许永久删除任务"}`),
		map[string]string{"If-Match": `"4"`, "Idempotency-Key": "delete-history-unlink"},
	)
	unlinkedData := decodeInboxTaskMutationResponse(t, unlinked.Body.Bytes())
	if unlinked.Code != http.StatusOK || unlinkedData.InboxItem.Status != "open" || unlinkedData.InboxItem.Version != 5 {
		t.Fatalf("unlink before Task delete = %d: %s", unlinked.Code, unlinked.Body.String())
	}
	deleted = performRequest(router, http.MethodDelete, "/api/v1/tasks/"+task.ID, nil, map[string]string{"If-Match": `"1"`})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete unlinked Task = %d: %s", deleted.Code, deleted.Body.String())
	}
	list := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/tasks", nil, nil)
	data, meta := decodeInboxTaskList(t, list.Body.Bytes())
	if list.Code != http.StatusOK || len(data.Active) != 0 || len(data.History) != 1 || meta.Total != 1 ||
		data.History[0].TaskID != nil || data.History[0].Task != nil || !data.History[0].TaskDeleted ||
		data.History[0].TaskRefID != task.ID || data.History[0].TaskTitleSnapshot != task.Title {
		t.Fatalf("deleted Task relation history data=%#v meta=%#v", data, meta)
	}
	var event models.WorkflowEvent
	if err := store.DB.Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'task_unlinked'", item.ID).Take(&event).Error; err != nil {
		t.Fatalf("load unlink event: %v", err)
	}
	if event.PreviousJSON == nil || !strings.Contains(*event.PreviousJSON, `"inbox_status":"tracking"`) ||
		event.CurrentJSON == nil || !strings.Contains(*event.CurrentJSON, `"inbox_status":"open"`) ||
		!strings.Contains(*event.CurrentJSON, `"inbox_version":5`) {
		t.Fatalf("unlink event snapshots previous=%v current=%v", event.PreviousJSON, event.CurrentJSON)
	}
}

func TestInboxTaskRelationEventFailureRollsBackMutationAndIdempotency(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"关系事务回滚"}`, "")
	task := createTaskForTaskFacts(t, router, `{"title":"回滚测试任务"}`)
	if err := store.DB.Exec(`
		CREATE TRIGGER test_fail_inbox_task_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.action = 'task_linked'
		BEGIN
			SELECT RAISE(ABORT, 'TEST_INBOX_TASK_EVENT_FAILURE');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	linkURL := "/api/v1/inbox-items/" + item.ID + "/tasks/" + task.ID
	failed := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "relation-rollback-key"},
	)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed link = %d: %s", failed.Code, failed.Body.String())
	}
	var relationCount int64
	if err := store.DB.Model(&models.InboxItemTask{}).Where("inbox_item_id = ?", item.ID).Count(&relationCount).Error; err != nil || relationCount != 0 {
		t.Fatalf("failed link relation count=%d err=%v", relationCount, err)
	}
	var current models.InboxItem
	if err := store.DB.First(&current, "id = ?", item.ID).Error; err != nil {
		t.Fatalf("load rolled back Inbox Item: %v", err)
	}
	if current.Status != "open" || current.Version != 1 || current.TriagedAt != nil {
		t.Fatalf("failed link changed Inbox Item = %#v", current)
	}
	var snapshotCount int64
	endpoint := fmt.Sprintf("POST /api/v1/inbox-items/%s/tasks/%s", item.ID, task.ID)
	if err := store.DB.Model(&models.IdempotencyKey{}).
		Where("key = ? AND endpoint = ?", "relation-rollback-key", endpoint).
		Count(&snapshotCount).Error; err != nil || snapshotCount != 0 {
		t.Fatalf("failed link idempotency count=%d err=%v", snapshotCount, err)
	}
	if err := store.DB.Exec("DROP TRIGGER test_fail_inbox_task_event").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	retried := performRequest(
		router, http.MethodPost, linkURL, []byte(`{"is_required":true}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "relation-rollback-key"},
	)
	if retried.Code != http.StatusCreated || decodeInboxTaskMutationResponse(t, retried.Body.Bytes()).InboxItem.Version != 2 {
		t.Fatalf("retry rolled back link = %d: %s", retried.Code, retried.Body.String())
	}
}

func TestInboxTaskConcurrentLinksClaimOneInboxVersion(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 16, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"并发关联版本所有权"}`, "")
	tasks := []models.Task{
		createTaskForTaskFacts(t, router, `{"title":"并发任务一"}`),
		createTaskForTaskFacts(t, router, `{"title":"并发任务二"}`),
	}

	type result struct {
		code int
		body []byte
	}
	start := make(chan struct{})
	results := make(chan result, len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			<-start
			response := performRequest(
				router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/tasks/"+task.ID,
				[]byte(`{"is_required":true}`), map[string]string{"If-Match": `"1"`},
			)
			results <- result{code: response.Code, body: response.Body.Bytes()}
		}()
	}
	close(start)
	created := 0
	conflicted := 0
	for range tasks {
		result := <-results
		switch {
		case result.code == http.StatusCreated:
			created++
		case result.code == http.StatusConflict:
			if code := responseErrorCode(t, result.body); code != "VERSION_CONFLICT" {
				t.Fatalf("concurrent link conflict code=%s body=%s", code, result.body)
			}
			conflicted++
		default:
			t.Fatalf("concurrent link result = %#v", result)
		}
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("concurrent link outcomes created=%d conflicted=%d", created, conflicted)
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/tasks", nil, nil)
	data, meta := decodeInboxTaskList(t, listed.Body.Bytes())
	if listed.Code != http.StatusOK || len(data.Active) != 1 || meta.InboxItemVersion != 2 || meta.Progress.ActiveTotal != 1 {
		t.Fatalf("concurrent link aggregate data=%#v meta=%#v response=%s", data, meta, listed.Body.String())
	}
	var eventCount int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'task_linked'", item.ID).
		Count(&eventCount).Error; err != nil || eventCount != 1 {
		t.Fatalf("concurrent link event count=%d err=%v", eventCount, err)
	}
}
