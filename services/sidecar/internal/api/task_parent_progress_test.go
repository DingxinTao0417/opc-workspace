package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func createRollupParentForTest(t *testing.T, router http.Handler, title string) (models.Task, actorResponse) {
	t.Helper()
	parent := createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":%q,"review_policy":"manual"}`, title))
	person := createActorForTest(t, router, fmt.Sprintf(`{"type":"person","display_name":%q}`, title+" 负责人"), nil)
	createAssignmentForTest(t, router, parent.ID, "assignee", person.ID, parent.Version, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	createAssignmentForTest(t, router, parent.ID, "reviewer", models.BuiltinOwnerActorID, parent.Version, "")
	return getTaskForTaskFacts(t, router, parent.ID), person
}

func createChildTaskForTest(t *testing.T, router http.Handler, parentID, title string) models.Task {
	t.Helper()
	return createTaskForTaskFacts(t, router, fmt.Sprintf(`{"title":%q,"parent_task_id":%q}`, title, parentID))
}

func runTaskLifecycleForParentTest(
	t *testing.T,
	router http.Handler,
	task models.Task,
	action string,
	reason string,
) taskLifecycleResponse {
	t.Helper()
	body := []byte(`{}`)
	if reason != "" {
		body = []byte(fmt.Sprintf(`{"reason":%q}`, reason))
	}
	recorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/"+action,
		body,
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, task.Version)},
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s Task %s = %d: %s", action, task.ID, recorder.Code, recorder.Body.String())
	}
	return decodeTaskLifecycleResponse(t, recorder.Body.Bytes())
}

func loadTaskSubmissionsForParentTest(t *testing.T, router http.Handler, taskID string) []taskSubmissionOutput {
	t.Helper()
	recorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+taskID+"/submissions?page_size=100", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list Task submissions = %d: %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data []taskSubmissionOutput `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode Task submissions: %v", err)
	}
	return envelope.Data
}

func TestTaskParentProgressRequestsSystemReviewAndAccepts(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "父任务自动验收")
	completedChild := createChildTaskForTest(t, router, parent.ID, "完成交付子任务")
	cancelledChild := createChildTaskForTest(t, router, parent.ID, "取消的子任务")

	cancelled := runTaskLifecycleForParentTest(t, router, cancelledChild, taskLifecycleCancel, "不再需要")
	if cancelled.Task.Status != "cancelled" {
		t.Fatalf("cancelled child = %#v", cancelled.Task)
	}
	completed := runTaskLifecycleForParentTest(t, router, completedChild, taskLifecycleComplete, "")
	if completed.Task.Status != "done" {
		t.Fatalf("completed child = %#v", completed.Task)
	}

	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil ||
		parent.SubtaskTotal != 2 || parent.SubtaskCompleted != 1 || parent.SubtaskCancelled != 1 {
		t.Fatalf("rolled-up parent = %#v", parent)
	}
	submissions := loadTaskSubmissionsForParentTest(t, router, parent.ID)
	if len(submissions) != 1 || submissions[0].ID != *parent.CurrentSubmissionID ||
		submissions[0].Origin != taskSubmissionOriginChildRollup ||
		submissions[0].SubmittedByActorID != models.BuiltinSystemActorID ||
		submissions[0].IsInferred || submissions[0].ArtifactCount != 0 ||
		submissions[0].Summary != childRollupSummary {
		t.Fatalf("child-rollup submission = %#v", submissions)
	}
	var activeAssignments int64
	if err := store.DB.Model(&models.TaskAssignment{}).
		Where("task_id = ? AND unassigned_at IS NULL", parent.ID).
		Count(&activeAssignments).Error; err != nil || activeAssignments != 2 {
		t.Fatalf("active parent assignments = %d err=%v", activeAssignments, err)
	}
	var automaticEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_id = ? AND action = ? AND actor_id = ?", parent.ID, "task_parent_review_requested", models.BuiltinSystemActorID).
		Count(&automaticEvents).Error; err != nil || automaticEvents != 1 {
		t.Fatalf("automatic review events = %d err=%v", automaticEvents, err)
	}

	reviewed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+parent.ID+"/review",
		[]byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if reviewed.Code != http.StatusOK {
		t.Fatalf("accept child-rollup = %d: %s", reviewed.Code, reviewed.Body.String())
	}
	accepted := decodeReviewOutputResponse(t, reviewed.Body.Bytes())
	if accepted.Task.Status != "done" || accepted.Submission.Origin != taskSubmissionOriginChildRollup ||
		accepted.Submission.Status != "accepted" {
		t.Fatalf("accepted child-rollup = %#v", accepted)
	}
	if err := store.DB.Model(&models.TaskAssignment{}).
		Where("task_id = ? AND unassigned_at IS NULL", parent.ID).
		Count(&activeAssignments).Error; err != nil || activeAssignments != 0 {
		t.Fatalf("terminal parent active assignments = %d err=%v", activeAssignments, err)
	}
}

func TestTaskParentProgressRequiresANonCancelledChild(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "全部取消父任务")
	first := createChildTaskForTest(t, router, parent.ID, "取消子任务一")
	second := createChildTaskForTest(t, router, parent.ID, "取消子任务二")
	runTaskLifecycleForParentTest(t, router, first, taskLifecycleCancel, "不再需要")
	runTaskLifecycleForParentTest(t, router, second, taskLifecycleCancel, "不再需要")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "todo" || parent.CurrentSubmissionID != nil ||
		parent.SubtaskTotal != 2 || parent.SubtaskCompleted != 0 || parent.SubtaskCancelled != 2 {
		t.Fatalf("all-cancelled parent = %#v", parent)
	}
	var rollupCount int64
	if err := store.DB.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND origin = ?", parent.ID, taskSubmissionOriginChildRollup).
		Count(&rollupCount).Error; err != nil || rollupCount != 0 {
		t.Fatalf("all-cancelled rollup count = %d err=%v", rollupCount, err)
	}
}

func TestTaskParentProgressLifecycleReplayDoesNotDuplicateRollup(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "幂等父任务")
	child := createChildTaskForTest(t, router, parent.ID, "幂等子任务")
	path := "/api/v1/tasks/" + child.ID + "/complete"
	headers := map[string]string{
		"If-Match":        fmt.Sprintf(`"%d"`, child.Version),
		"Idempotency-Key": "parent-rollup-lifecycle-replay",
	}
	first := performRequest(router, http.MethodPost, path, []byte(`{}`), headers)
	if first.Code != http.StatusOK {
		t.Fatalf("first complete = %d: %s", first.Code, first.Body.String())
	}
	replayed := performRequest(router, http.MethodPost, path, []byte(`{}`), headers)
	if replayed.Code != http.StatusOK || replayed.Header().Get("Idempotency-Replayed") != "true" ||
		replayed.Body.String() != first.Body.String() {
		t.Fatalf("replayed complete = %d headers=%v: %s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	var rollupCount int64
	if err := store.DB.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND origin = ?", parent.ID, taskSubmissionOriginChildRollup).
		Count(&rollupCount).Error; err != nil || rollupCount != 1 {
		t.Fatalf("replayed rollup count = %d err=%v", rollupCount, err)
	}
}

func TestTaskParentProgressGatesWithdrawalAndRejectedRollup(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent := createTaskForTaskFacts(t, router, `{"title":"门禁父任务","review_policy":"manual"}`)
	child := createChildTaskForTest(t, router, parent.ID, "门禁子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "todo" {
		t.Fatalf("parent advanced without assignments = %#v", parent)
	}

	person := createActorForTest(t, router, `{"type":"person","display_name":"门禁负责人"}`, nil)
	createdAssignee := createAssignmentForTest(t, router, parent.ID, "assignee", person.ID, parent.Version, "")
	if createdAssignee.Assignment.Role != "assignee" {
		t.Fatalf("created assignee = %#v", createdAssignee)
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "todo" {
		t.Fatalf("parent advanced without reviewer = %#v", parent)
	}
	reviewer := createAssignmentForTest(t, router, parent.ID, "reviewer", models.BuiltinOwnerActorID, parent.Version, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil {
		t.Fatalf("parent did not advance after final gate = %#v", parent)
	}
	gateWithdrawnSubmissionID := *parent.CurrentSubmissionID

	endedReviewer := performRequest(
		router, http.MethodPost, "/api/v1/assignments/"+reviewer.Assignment.ID+"/end",
		[]byte(`{"reason":"暂时移除验收人"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if endedReviewer.Code != http.StatusOK {
		t.Fatalf("end reviewer gate = %d: %s", endedReviewer.Code, endedReviewer.Body.String())
	}
	ended := decodeAssignmentMutation(t, endedReviewer.Body.Bytes())
	if ended.Task.Status != "in_progress" || ended.Task.CurrentSubmissionID != nil || ended.Assignment.IsActive {
		t.Fatalf("ended reviewer gate response = %#v", ended)
	}
	var gateWithdrawn models.TaskSubmission
	if err := store.DB.First(&gateWithdrawn, "id = ?", gateWithdrawnSubmissionID).Error; err != nil || gateWithdrawn.Status != "withdrawn" {
		t.Fatalf("gate-withdrawn child-rollup = %#v err=%v", gateWithdrawn, err)
	}
	reviewer = createAssignmentForTest(
		t, router, parent.ID, "reviewer", models.BuiltinOwnerActorID, ended.Task.Version, "",
	)
	if reviewer.Task.Status != "waiting_review" || reviewer.Task.CurrentSubmissionID == nil {
		t.Fatalf("restored reviewer gate response = %#v", reviewer)
	}
	parent = reviewer.Task
	firstSubmissionID := *parent.CurrentSubmissionID

	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleReopen, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "in_progress" || parent.CurrentSubmissionID != nil || parent.SubmittedAt != nil {
		t.Fatalf("invalidated parent = %#v", parent)
	}
	var withdrawn models.TaskSubmission
	if err := store.DB.First(&withdrawn, "id = ?", firstSubmissionID).Error; err != nil ||
		withdrawn.Status != "withdrawn" || withdrawn.WithdrawnByActorID == nil ||
		*withdrawn.WithdrawnByActorID != models.BuiltinSystemActorID {
		t.Fatalf("withdrawn child-rollup = %#v err=%v", withdrawn, err)
	}

	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil || *parent.CurrentSubmissionID == firstSubmissionID {
		t.Fatalf("second child-rollup = %#v", parent)
	}
	reviewed := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+parent.ID+"/review",
		[]byte(`{"decision":"request_changes","reason":"父任务还需人工补充"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if reviewed.Code != http.StatusOK {
		t.Fatalf("request changes = %d: %s", reviewed.Code, reviewed.Body.String())
	}
	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleReopen, "")
	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "in_progress" || parent.CurrentSubmissionID == nil {
		// request_changes deliberately keeps the rejected current pointer for history.
		t.Fatalf("rejected rollup was resubmitted = %#v", parent)
	}
	submissions := loadTaskSubmissionsForParentTest(t, router, parent.ID)
	if len(submissions) != 3 || submissions[0].Status != "changes_requested" ||
		submissions[1].Status != "withdrawn" || submissions[2].Status != "withdrawn" {
		t.Fatalf("rejected rollup history = %#v", submissions)
	}
}

func TestTaskParentProgressStartsWhenReviewPolicyBecomesManual(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	parent := createTaskForTaskFacts(t, router, `{"title":"策略切换父任务","review_policy":"none"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"策略切换负责人"}`, nil)
	createAssignmentForTest(t, router, parent.ID, "assignee", person.ID, parent.Version, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	createAssignmentForTest(t, router, parent.ID, "reviewer", models.BuiltinOwnerActorID, parent.Version, "")
	child := createChildTaskForTest(t, router, parent.ID, "策略切换子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "todo" || parent.CurrentSubmissionID != nil {
		t.Fatalf("review-none parent advanced = %#v", parent)
	}

	updated := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/"+parent.ID,
		[]byte(`{"review_policy":"manual"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if updated.Code != http.StatusOK {
		t.Fatalf("restore review policy = %d: %s", updated.Code, updated.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.ReviewPolicy != "manual" || parent.CurrentSubmissionID == nil {
		t.Fatalf("restored review policy parent = %#v", parent)
	}
}

func TestTaskParentProgressReconcilesHistoricalReadyParentOnStart(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "历史就绪父任务")
	child := createChildTaskForTest(t, router, parent.ID, "历史已完成子任务")
	completedAt := "2026-08-29T08:30:00Z"
	if err := store.DB.Exec(
		"UPDATE tasks SET status = 'done', completed_at = ?, updated_at = ?, version = version + 1 WHERE id = ?",
		completedAt, completedAt, child.ID,
	).Error; err != nil {
		t.Fatalf("prepare historical completed child: %v", err)
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "todo" || parent.CurrentSubmissionID != nil || parent.SubtaskCompleted != 1 {
		t.Fatalf("historical ready fixture = %#v", parent)
	}

	started := runTaskLifecycleForParentTest(t, router, parent, taskLifecycleStart, "")
	if started.Task.Status != "waiting_review" || started.Task.CurrentSubmissionID == nil ||
		started.Event.Action != "task_started" || started.Event.Current["version"] == float64(started.Task.Version) {
		t.Fatalf("historical parent start response = %#v", started)
	}
	submissions := loadTaskSubmissionsForParentTest(t, router, parent.ID)
	if len(submissions) != 1 || submissions[0].Origin != taskSubmissionOriginChildRollup {
		t.Fatalf("historical parent start submissions = %#v", submissions)
	}
}

func TestTaskParentProgressReconcilesHistoricalReadyParentOnBatchStart(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "批量历史就绪父任务")
	child := createChildTaskForTest(t, router, parent.ID, "批量历史已完成子任务")
	completedAt := "2026-08-29T08:31:00Z"
	if err := store.DB.Exec(
		"UPDATE tasks SET status = 'done', completed_at = ?, updated_at = ?, version = version + 1 WHERE id = ?",
		completedAt, completedAt, child.ID,
	).Error; err != nil {
		t.Fatalf("prepare batch historical completed child: %v", err)
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	batch := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/batch",
		[]byte(fmt.Sprintf(`{"action":"start","items":[{"id":%q,"expected_version":%d}]}`, parent.ID, parent.Version)), nil,
	)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch-start historical parent = %d: %s", batch.Code, batch.Body.String())
	}
	var envelope struct {
		Data batchUpdatedTasksResponse `json:"data"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode historical batch start: %v", err)
	}
	if len(envelope.Data.Tasks) != 1 || envelope.Data.Tasks[0].Status != "waiting_review" ||
		envelope.Data.Tasks[0].CurrentSubmissionID == nil {
		t.Fatalf("historical batch start response = %#v", envelope.Data)
	}
}

func TestTaskParentProgressPreservesBlockAndRechecksOnUnblock(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "阻塞父任务")
	child := createChildTaskForTest(t, router, parent.ID, "阻塞场景子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	blocked := runTaskLifecycleForParentTest(t, router, parent, taskLifecycleBlock, "等待外部确认")
	if blocked.Task.Status != "blocked" || blocked.Task.BlockedFromStatus == nil || *blocked.Task.BlockedFromStatus != "waiting_review" {
		t.Fatalf("blocked rollup parent = %#v", blocked.Task)
	}

	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleReopen, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "blocked" || parent.BlockedReason == nil || *parent.BlockedReason != "等待外部确认" ||
		parent.BlockedFromStatus == nil || *parent.BlockedFromStatus != "in_progress" || parent.CurrentSubmissionID != nil {
		t.Fatalf("invalidated blocked parent = %#v", parent)
	}
	child = getTaskForTaskFacts(t, router, child.ID)
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "blocked" || parent.CurrentSubmissionID != nil {
		t.Fatalf("blocked parent advanced early = %#v", parent)
	}

	unblocked := runTaskLifecycleForParentTest(t, router, parent, taskLifecycleUnblock, "")
	if unblocked.Task.Status != "waiting_review" || unblocked.Task.CurrentSubmissionID == nil ||
		unblocked.Event.Action != "task_unblocked" || unblocked.Event.Current["version"] == float64(unblocked.Task.Version) {
		t.Fatalf("unblock final response = %#v", unblocked)
	}
}

func TestTaskParentProgressReopensAcceptedAncestors(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	grandparent, _ := createRollupParentForTest(t, router, "最上层父任务")
	parent, _ := createRollupParentForTest(t, router, "中间父任务")
	parentUpdate := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/"+parent.ID,
		[]byte(fmt.Sprintf(`{"parent_task_id":%q}`, grandparent.ID)),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if parentUpdate.Code != http.StatusOK {
		t.Fatalf("attach middle parent = %d: %s", parentUpdate.Code, parentUpdate.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	leaf := createChildTaskForTest(t, router, parent.ID, "最底层子任务")
	runTaskLifecycleForParentTest(t, router, leaf, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" {
		t.Fatalf("middle parent did not roll up = %#v", parent)
	}
	grandparent = getTaskForTaskFacts(t, router, grandparent.ID)
	if grandparent.Status != "todo" || grandparent.CurrentSubmissionID != nil {
		t.Fatalf("grandparent advanced from a non-done direct child = %#v", grandparent)
	}
	reviewMiddle := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+parent.ID+"/review", []byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if reviewMiddle.Code != http.StatusOK {
		t.Fatalf("accept middle parent = %d: %s", reviewMiddle.Code, reviewMiddle.Body.String())
	}
	grandparent = getTaskForTaskFacts(t, router, grandparent.ID)
	if grandparent.Status != "waiting_review" {
		t.Fatalf("grandparent did not roll up = %#v", grandparent)
	}
	reviewGrandparent := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+grandparent.ID+"/review", []byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, grandparent.Version)},
	)
	if reviewGrandparent.Code != http.StatusOK {
		t.Fatalf("accept grandparent = %d: %s", reviewGrandparent.Code, reviewGrandparent.Body.String())
	}

	leaf = getTaskForTaskFacts(t, router, leaf.ID)
	runTaskLifecycleForParentTest(t, router, leaf, taskLifecycleReopen, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	grandparent = getTaskForTaskFacts(t, router, grandparent.ID)
	if parent.Status != "todo" || parent.CurrentSubmissionID != nil ||
		grandparent.Status != "todo" || grandparent.CurrentSubmissionID != nil {
		t.Fatalf("accepted ancestor rollback: parent=%#v grandparent=%#v", parent, grandparent)
	}
	var reopenEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_id IN ? AND action = ? AND actor_id = ?", []string{parent.ID, grandparent.ID}, "task_parent_reopened", models.BuiltinSystemActorID).
		Count(&reopenEvents).Error; err != nil || reopenEvents != 2 {
		t.Fatalf("ancestor reopen events = %d err=%v", reopenEvents, err)
	}
	var restoredAssignments int64
	if err := store.DB.Model(&models.TaskAssignment{}).
		Where("task_id IN ? AND unassigned_at IS NULL", []string{parent.ID, grandparent.ID}).
		Count(&restoredAssignments).Error; err != nil || restoredAssignments != 0 {
		t.Fatalf("system reopen restored assignments = %d err=%v", restoredAssignments, err)
	}
}

func TestTaskParentProgressDoesNotOverrideManualSubmission(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "人工提交父任务")
	child := createChildTaskForTest(t, router, parent.ID, "人工提交场景子任务")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	submitted := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+parent.ID+"/submit-output",
		[]byte(`{"summary":"人工父任务验收资料","artifacts":[]}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if submitted.Code != http.StatusCreated {
		t.Fatalf("manual parent submission = %d: %s", submitted.Code, submitted.Body.String())
	}
	manual := decodeSubmitOutputResponse(t, submitted.Body.Bytes())
	if manual.Submission.Origin != taskSubmissionOriginManual {
		t.Fatalf("manual submission origin = %#v", manual.Submission)
	}
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" || parent.CurrentSubmissionID == nil || *parent.CurrentSubmissionID != manual.Submission.ID {
		t.Fatalf("manual submission was replaced = %#v", parent)
	}
	var automaticEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_id = ? AND action LIKE 'task_parent_%'", parent.ID).
		Count(&automaticEvents).Error; err != nil || automaticEvents != 0 {
		t.Fatalf("manual parent automatic events = %d err=%v", automaticEvents, err)
	}
}

func TestTaskParentProgressReconcilesCreateReparentAndDelete(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	firstParent, _ := createRollupParentForTest(t, router, "结构变化父任务一")
	doneChild := createChildTaskForTest(t, router, firstParent.ID, "将被改挂的完成子任务")
	runTaskLifecycleForParentTest(t, router, doneChild, taskLifecycleComplete, "")
	firstParent = getTaskForTaskFacts(t, router, firstParent.ID)
	if firstParent.Status != "waiting_review" {
		t.Fatalf("first parent initial rollup = %#v", firstParent)
	}

	createChildTaskForTest(t, router, firstParent.ID, "新建后使汇总失效")
	firstParent = getTaskForTaskFacts(t, router, firstParent.ID)
	if firstParent.Status != "in_progress" || firstParent.CurrentSubmissionID != nil {
		t.Fatalf("child create did not withdraw rollup = %#v", firstParent)
	}

	secondParent, _ := createRollupParentForTest(t, router, "结构变化父任务二")
	doneChild = getTaskForTaskFacts(t, router, doneChild.ID)
	reparented := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/"+doneChild.ID,
		[]byte(fmt.Sprintf(`{"parent_task_id":%q}`, secondParent.ID)),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, doneChild.Version)},
	)
	if reparented.Code != http.StatusOK {
		t.Fatalf("reparent done child = %d: %s", reparented.Code, reparented.Body.String())
	}
	secondParent = getTaskForTaskFacts(t, router, secondParent.ID)
	if secondParent.Status != "waiting_review" || secondParent.CurrentSubmissionID == nil {
		t.Fatalf("new parent did not roll up = %#v", secondParent)
	}

	doneChild = getTaskForTaskFacts(t, router, doneChild.ID)
	deleted := performRequest(
		router, http.MethodDelete, "/api/v1/tasks/"+doneChild.ID,
		nil, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, doneChild.Version)},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete rolled-up child = %d: %s", deleted.Code, deleted.Body.String())
	}
	secondParent = getTaskForTaskFacts(t, router, secondParent.ID)
	if secondParent.Status != "in_progress" || secondParent.CurrentSubmissionID != nil || secondParent.SubtaskTotal != 0 {
		t.Fatalf("child delete did not withdraw rollup = %#v", secondParent)
	}
}

func TestTaskParentProgressBatchSiblingAndAncestorVersions(t *testing.T) {
	router, store, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "批量兄弟父任务")
	first := createChildTaskForTest(t, router, parent.ID, "批量子任务一")
	second := createChildTaskForTest(t, router, parent.ID, "批量子任务二")
	batch := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/batch",
		[]byte(fmt.Sprintf(`{"action":"complete","items":[{"id":%q,"expected_version":%d},{"id":%q,"expected_version":%d}]}`,
			first.ID, first.Version, second.ID, second.Version)), nil,
	)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch-complete siblings = %d: %s", batch.Code, batch.Body.String())
	}
	parent = getTaskForTaskFacts(t, router, parent.ID)
	if parent.Status != "waiting_review" {
		t.Fatalf("batch parent = %#v", parent)
	}
	var rollupCount int64
	if err := store.DB.Model(&models.TaskSubmission{}).
		Where("task_id = ? AND origin = ?", parent.ID, taskSubmissionOriginChildRollup).
		Count(&rollupCount).Error; err != nil || rollupCount != 1 {
		t.Fatalf("batch rollup count = %d err=%v", rollupCount, err)
	}

	root := createTaskForTaskFacts(t, router, `{"title":"同批祖先任务"}`)
	child := createChildTaskForTest(t, router, root.ID, "同批后代任务")
	root = getTaskForTaskFacts(t, router, root.ID)
	ancestorBatch := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/batch",
		[]byte(fmt.Sprintf(`{"action":"cancel","reason":"整组取消","items":[{"id":%q,"expected_version":%d},{"id":%q,"expected_version":%d}]}`,
			child.ID, child.Version, root.ID, root.Version)), nil,
	)
	if ancestorBatch.Code != http.StatusOK {
		t.Fatalf("batch-cancel ancestor and child = %d: %s", ancestorBatch.Code, ancestorBatch.Body.String())
	}
	var envelope struct {
		Data batchUpdatedTasksResponse `json:"data"`
	}
	if err := json.Unmarshal(ancestorBatch.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode ancestor batch: %v", err)
	}
	if len(envelope.Data.Tasks) != 2 || envelope.Data.Tasks[0].Status != "cancelled" ||
		envelope.Data.Tasks[1].Status != "cancelled" || envelope.Data.Tasks[1].Version <= root.Version {
		t.Fatalf("ancestor batch response = %#v", envelope.Data)
	}
}

func TestTaskParentProgressBatchReopensAcceptedParentAndChildInInputOrder(t *testing.T) {
	router, _, _ := newTaskOutputTestAPI(t)
	parent, _ := createRollupParentForTest(t, router, "批量重开已验收父任务")
	child := createChildTaskForTest(t, router, parent.ID, "批量重开已完成子任务")
	runTaskLifecycleForParentTest(t, router, child, taskLifecycleComplete, "")
	parent = getTaskForTaskFacts(t, router, parent.ID)
	accepted := performRequest(
		router, http.MethodPost, "/api/v1/tasks/"+parent.ID+"/review", []byte(`{"decision":"accept"}`),
		map[string]string{"If-Match": fmt.Sprintf(`"%d"`, parent.Version)},
	)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept parent before batch reopen = %d: %s", accepted.Code, accepted.Body.String())
	}
	child = getTaskForTaskFacts(t, router, child.ID)
	parent = getTaskForTaskFacts(t, router, parent.ID)

	batch := performRequest(
		router, http.MethodPatch, "/api/v1/tasks/batch",
		[]byte(fmt.Sprintf(`{"action":"reopen","items":[{"id":%q,"expected_version":%d},{"id":%q,"expected_version":%d}]}`,
			child.ID, child.Version, parent.ID, parent.Version)), nil,
	)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch-reopen child then accepted parent = %d: %s", batch.Code, batch.Body.String())
	}
	var envelope struct {
		Data batchUpdatedTasksResponse `json:"data"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode parent/child reopen batch: %v", err)
	}
	if len(envelope.Data.Tasks) != 2 || envelope.Data.Tasks[0].Status != "todo" || envelope.Data.Tasks[1].Status != "todo" {
		t.Fatalf("parent/child reopen batch = %#v", envelope.Data)
	}
}
