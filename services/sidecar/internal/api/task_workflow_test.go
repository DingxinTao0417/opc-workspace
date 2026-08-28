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
	"gorm.io/gorm"
)

func decodeTaskLifecycleResponse(t *testing.T, body []byte) taskLifecycleResponse {
	t.Helper()
	var envelope struct {
		Data taskLifecycleResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode task lifecycle response: %v", err)
	}
	return envelope.Data
}

func decodeTaskWorkflowEvents(t *testing.T, body []byte) ([]taskWorkflowEventOutput, taskWorkflowEventMeta) {
	t.Helper()
	var envelope struct {
		Data []taskWorkflowEventOutput `json:"data"`
		Meta taskWorkflowEventMeta     `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode task workflow events: %v", err)
	}
	return envelope.Data, envelope.Meta
}

func TestTaskLifecycleCommandsIdempotencyAndTimeline(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Lifecycle command task"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Lifecycle assignee"}`, nil)
	assignment := createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 1, "")

	missingAssigneeTask := createTaskForTaskFacts(t, router, `{"title":"Cannot start unassigned"}`)
	missingAssignee := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+missingAssigneeTask.ID+"/start",
		[]byte(`{}`),
		map[string]string{"If-Match": `"1"`},
	)
	if missingAssignee.Code != http.StatusConflict || responseErrorCode(t, missingAssignee.Body.Bytes()) != "TASK_ASSIGNEE_REQUIRED" {
		t.Fatalf("start without assignee = %d: %s", missingAssignee.Code, missingAssignee.Body.String())
	}

	startRequestID := uuid.NewString()
	startHeaders := map[string]string{
		"If-Match":        `"2"`,
		"Idempotency-Key": "task-lifecycle-start",
		"X-Request-ID":    startRequestID,
	}
	staleStart := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/start",
		[]byte(`{}`),
		map[string]string{"If-Match": `"1"`},
	)
	if staleStart.Code != http.StatusConflict || responseErrorCode(t, staleStart.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale start = %d: %s", staleStart.Code, staleStart.Body.String())
	}
	startedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+strings.ToUpper(task.ID)+"/start",
		[]byte(`{}`),
		startHeaders,
	)
	if startedRecorder.Code != http.StatusOK || startedRecorder.Header().Get("ETag") != `"3"` {
		t.Fatalf("start task = %d headers=%v: %s", startedRecorder.Code, startedRecorder.Header(), startedRecorder.Body.String())
	}
	started := decodeTaskLifecycleResponse(t, startedRecorder.Body.Bytes())
	if started.Task.Status != "in_progress" || started.Task.Version != 3 ||
		started.Event.Action != "task_started" || started.Event.CommandSeq == nil || *started.Event.CommandSeq != 1 ||
		started.Event.RequestID == nil || *started.Event.RequestID != startRequestID ||
		started.Event.Actor == nil || started.Event.Actor.ID != models.BuiltinOwnerActorID {
		t.Fatalf("started response = %#v", started)
	}

	replayedStart := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/start",
		[]byte(`{}`),
		map[string]string{"If-Match": `"2"`, "Idempotency-Key": "task-lifecycle-start"},
	)
	if replayedStart.Code != http.StatusOK || replayedStart.Header().Get("ETag") != `"3"` ||
		replayedStart.Header().Get("Idempotency-Replayed") != "true" || replayedStart.Body.String() != startedRecorder.Body.String() {
		t.Fatalf("start replay = %d headers=%v body=%s original=%s", replayedStart.Code, replayedStart.Header(), replayedStart.Body.String(), startedRecorder.Body.String())
	}
	conflictingReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/start",
		[]byte(`{}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "task-lifecycle-start"},
	)
	if conflictingReplay.Code != http.StatusConflict || responseErrorCode(t, conflictingReplay.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflicting lifecycle replay = %d: %s", conflictingReplay.Code, conflictingReplay.Body.String())
	}

	blockRequestID := uuid.NewString()
	blockedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/block",
		[]byte(`{"reason":"  External dependency  "}`),
		map[string]string{
			"If-Match": `"3"`, "Idempotency-Key": "task-lifecycle-block", "X-Request-ID": blockRequestID,
		},
	)
	if blockedRecorder.Code != http.StatusOK || blockedRecorder.Header().Get("ETag") != `"4"` {
		t.Fatalf("block task = %d: %s", blockedRecorder.Code, blockedRecorder.Body.String())
	}
	blocked := decodeTaskLifecycleResponse(t, blockedRecorder.Body.Bytes())
	if blocked.Task.Status != "blocked" || blocked.Task.BlockedReason == nil || *blocked.Task.BlockedReason != "External dependency" ||
		blocked.Task.BlockedFromStatus == nil || *blocked.Task.BlockedFromStatus != "in_progress" ||
		blocked.Event.Reason == nil || *blocked.Event.Reason != "External dependency" || blocked.Event.Current["reason"] != "External dependency" {
		t.Fatalf("blocked response = %#v", blocked)
	}
	var blockedSources []models.InboxItem
	if err := store.DB.Where("source_entity_type = ? AND source_entity_id = ?", taskBlockedInboxSourceType, task.ID).
		Order("id ASC").Find(&blockedSources).Error; err != nil {
		t.Fatalf("load projected blocked source: %v", err)
	}
	if len(blockedSources) != 1 || blockedSources[0].Kind != "event" || blockedSources[0].Status != "open" ||
		blockedSources[0].SourceEventKey == nil || *blockedSources[0].SourceEventKey != taskBlockedEventKey(task.ID, 4) ||
		blockedSources[0].Priority != "P2" || blockedSources[0].Summary != "阻塞原因：External dependency" {
		t.Fatalf("projected blocked source = %#v", blockedSources)
	}
	var blockedPayload map[string]any
	if err := json.Unmarshal([]byte(blockedSources[0].PayloadJSON), &blockedPayload); err != nil {
		t.Fatalf("decode blocked source payload: %v", err)
	}
	if blockedPayload["task_id"] != task.ID || blockedPayload["task_title"] != task.Title ||
		blockedPayload["blocked_reason"] != "External dependency" || blockedPayload["blocked_from_status"] != "in_progress" ||
		blockedPayload["block_version"] != float64(4) {
		t.Fatalf("blocked source payload = %#v", blockedPayload)
	}
	blockReplay := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/block",
		[]byte(`{"reason":"External dependency"}`),
		map[string]string{"If-Match": `"3"`, "Idempotency-Key": "task-lifecycle-block"},
	)
	if blockReplay.Code != http.StatusOK || blockReplay.Header().Get("Idempotency-Replayed") != "true" || blockReplay.Body.String() != blockedRecorder.Body.String() {
		t.Fatalf("normalized reason replay = %d headers=%v: %s", blockReplay.Code, blockReplay.Header(), blockReplay.Body.String())
	}
	var blockedSourceCount int64
	if err := store.DB.Model(&models.InboxItem{}).
		Where("source_event_key = ?", taskBlockedEventKey(task.ID, 4)).Count(&blockedSourceCount).Error; err != nil {
		t.Fatalf("count replayed blocked sources: %v", err)
	}
	if blockedSourceCount != 1 {
		t.Fatalf("block replay projected %d Inbox sources, want 1", blockedSourceCount)
	}

	blockedComplete := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"4"`},
	)
	if blockedComplete.Code != http.StatusConflict || responseErrorCode(t, blockedComplete.Body.Bytes()) != "TASK_TRANSITION_NOT_ALLOWED" {
		t.Fatalf("complete blocked task = %d: %s", blockedComplete.Code, blockedComplete.Body.String())
	}
	unknownField := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/unblock",
		[]byte(`{"status":"in_progress"}`),
		map[string]string{"If-Match": `"4"`},
	)
	if unknownField.Code != http.StatusBadRequest || responseErrorCode(t, unknownField.Body.Bytes()) != "INVALID_JSON" {
		t.Fatalf("unblock client target = %d: %s", unknownField.Code, unknownField.Body.String())
	}
	unblockedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/unblock",
		[]byte(`{}`),
		map[string]string{"If-Match": `"4"`},
	)
	unblocked := decodeTaskLifecycleResponse(t, unblockedRecorder.Body.Bytes())
	if unblockedRecorder.Code != http.StatusOK || unblocked.Task.Status != "in_progress" || unblocked.Task.Version != 5 ||
		unblocked.Task.BlockedReason != nil || unblocked.Task.BlockedAt != nil || unblocked.Task.BlockedFromStatus != nil {
		t.Fatalf("unblocked response = %d: %#v", unblockedRecorder.Code, unblocked)
	}

	completeRequestID := uuid.NewString()
	completedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"5"`, "X-Request-ID": completeRequestID},
	)
	completed := decodeTaskLifecycleResponse(t, completedRecorder.Body.Bytes())
	if completedRecorder.Code != http.StatusOK || completedRecorder.Header().Get("ETag") != `"6"` ||
		completed.Task.Status != "done" || completed.Task.Version != 6 || completed.Task.CompletedAt == nil ||
		completed.Event.Action != "task_completed" || completed.Event.CommandSeq == nil || *completed.Event.CommandSeq != 2 {
		t.Fatalf("completed response = %d: %#v", completedRecorder.Code, completed)
	}

	assignmentList := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	assignmentData, assignmentMeta := decodeAssignmentList(t, assignmentList.Body.Bytes())
	if assignmentData.Active.Assignee != nil || assignmentMeta.TaskVersion != 6 || len(assignmentData.History) != 1 ||
		assignmentData.History[0].ID != assignment.Assignment.ID || assignmentData.History[0].Reason == nil ||
		*assignmentData.History[0].Reason != taskCompletedReason {
		t.Fatalf("assignments after completion = %s", assignmentList.Body.String())
	}

	eventsRecorder := performRequest(
		router,
		http.MethodGet,
		"/api/v1/tasks/"+strings.ToUpper(task.ID)+"/events?page=1&page_size=100",
		nil,
		nil,
	)
	events, meta := decodeTaskWorkflowEvents(t, eventsRecorder.Body.Bytes())
	if eventsRecorder.Code != http.StatusOK || eventsRecorder.Header().Get("ETag") != `"6"` || meta.TaskVersion != 6 || meta.Total != int64(len(events)) {
		t.Fatalf("events response = %d headers=%v meta=%#v body=%s", eventsRecorder.Code, eventsRecorder.Header(), meta, eventsRecorder.Body.String())
	}
	completionEvents := make([]taskWorkflowEventOutput, 0, 2)
	for _, event := range events {
		if event.RequestID != nil && *event.RequestID == completeRequestID {
			completionEvents = append(completionEvents, event)
		}
	}
	if len(completionEvents) != 2 || completionEvents[0].Action != "task_completed" || completionEvents[0].CommandSeq == nil || *completionEvents[0].CommandSeq != 2 ||
		completionEvents[1].Action != "assignment_ended" || completionEvents[1].CommandSeq == nil || *completionEvents[1].CommandSeq != 1 {
		t.Fatalf("completion event ordering = %#v", completionEvents)
	}

	reopenedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reopen",
		[]byte(`{}`),
		map[string]string{"If-Match": `"6"`},
	)
	reopened := decodeTaskLifecycleResponse(t, reopenedRecorder.Body.Bytes())
	if reopenedRecorder.Code != http.StatusOK || reopened.Task.Status != "todo" || reopened.Task.Version != 7 || reopened.Task.CompletedAt != nil {
		t.Fatalf("reopened response = %d: %#v", reopenedRecorder.Code, reopened)
	}
	cancelledRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/cancel",
		[]byte(`{"reason":"No longer needed"}`),
		map[string]string{"If-Match": `"7"`},
	)
	cancelled := decodeTaskLifecycleResponse(t, cancelledRecorder.Body.Bytes())
	if cancelledRecorder.Code != http.StatusOK || cancelled.Task.Status != "cancelled" || cancelled.Task.Version != 8 ||
		cancelled.Event.Action != "task_cancelled" || cancelled.Event.Reason == nil || *cancelled.Event.Reason != "No longer needed" {
		t.Fatalf("cancelled response = %d: %#v", cancelledRecorder.Code, cancelled)
	}
	assignCancelled := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/assignments",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q}`, person.ID)),
		map[string]string{"If-Match": `"8"`},
	)
	if assignCancelled.Code != http.StatusConflict || responseErrorCode(t, assignCancelled.Body.Bytes()) != "TASK_NOT_ASSIGNABLE" {
		t.Fatalf("assign cancelled task = %d: %s", assignCancelled.Code, assignCancelled.Body.String())
	}
	finalReopen := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reopen",
		[]byte(`{}`),
		map[string]string{"If-Match": `"8"`},
	)
	if finalReopen.Code != http.StatusOK || decodeTaskLifecycleResponse(t, finalReopen.Body.Bytes()).Task.Version != 9 {
		t.Fatalf("reopen cancelled task = %d: %s", finalReopen.Code, finalReopen.Body.String())
	}
	firstPageRecorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/events?page=1&page_size=2", nil, nil)
	secondPageRecorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/events?page=2&page_size=2", nil, nil)
	firstPage, firstMeta := decodeTaskWorkflowEvents(t, firstPageRecorder.Body.Bytes())
	secondPage, secondMeta := decodeTaskWorkflowEvents(t, secondPageRecorder.Body.Bytes())
	pageIDs := make(map[string]struct{}, 4)
	for _, event := range append(append([]taskWorkflowEventOutput{}, firstPage...), secondPage...) {
		pageIDs[event.ID] = struct{}{}
	}
	if firstPageRecorder.Code != http.StatusOK || secondPageRecorder.Code != http.StatusOK ||
		firstPageRecorder.Header().Get("ETag") != `"9"` || len(firstPage) != 2 || len(secondPage) != 2 ||
		firstMeta.Total != secondMeta.Total || firstMeta.TaskVersion != 9 || secondMeta.TaskVersion != 9 ||
		len(pageIDs) != 4 {
		t.Fatalf("event pagination first=%#v/%#v second=%#v/%#v", firstPage, firstMeta, secondPage, secondMeta)
	}
	finalAssignments := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	finalData, finalMeta := decodeAssignmentList(t, finalAssignments.Body.Bytes())
	if finalData.Active.Assignee != nil || finalData.Active.Reviewer != nil || finalMeta.TaskVersion != 9 {
		t.Fatalf("reopen restored assignments: %s", finalAssignments.Body.String())
	}

	var taskVersion int64
	if err := store.SQL.QueryRow("SELECT version FROM tasks WHERE id = ?", task.ID).Scan(&taskVersion); err != nil || taskVersion != 9 {
		t.Fatalf("final task version = %d err=%v", taskVersion, err)
	}
}

func TestTaskBlockedSourceProtectsDeleteAndKeepsSnapshot(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Blocked source task","priority":"P1"}`)

	blockedRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/block",
		[]byte(`{"reason":"Waiting for a local approval"}`),
		map[string]string{"If-Match": `"1"`, "Idempotency-Key": "blocked-source-delete"},
	)
	if blockedRecorder.Code != http.StatusOK {
		t.Fatalf("block source Task = %d: %s", blockedRecorder.Code, blockedRecorder.Body.String())
	}
	blocked := decodeTaskLifecycleResponse(t, blockedRecorder.Body.Bytes())
	if blocked.Task.Status != "blocked" || blocked.Task.Version != 2 {
		t.Fatalf("blocked source Task = %#v", blocked.Task)
	}

	var source models.InboxItem
	if err := store.DB.Where("source_event_key = ?", taskBlockedEventKey(task.ID, 2)).Take(&source).Error; err != nil {
		t.Fatalf("load blocked source Inbox Item: %v", err)
	}
	deleteBlocked := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+task.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": `"2"`},
	)
	if deleteBlocked.Code != http.StatusConflict || responseErrorCode(t, deleteBlocked.Body.Bytes()) != "TASK_HAS_ACTIVE_INBOX_SOURCES" {
		t.Fatalf("delete active blocked source Task = %d: %s", deleteBlocked.Code, deleteBlocked.Body.String())
	}

	resolved := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+source.ID+"/resolve",
		[]byte(`{"reason":"Approval obtained"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if resolved.Code != http.StatusOK || decodeInboxItemData(t, resolved.Body.Bytes()).Status != "resolved" {
		t.Fatalf("resolve blocked source = %d: %s", resolved.Code, resolved.Body.String())
	}
	unblocked := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/unblock",
		[]byte(`{}`),
		map[string]string{"If-Match": `"2"`},
	)
	if unblocked.Code != http.StatusOK || decodeTaskLifecycleResponse(t, unblocked.Body.Bytes()).Task.Version != 3 {
		t.Fatalf("unblock source Task = %d: %s", unblocked.Code, unblocked.Body.String())
	}
	reblocked := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/block",
		[]byte(`{"reason":"Approval expired"}`),
		map[string]string{"If-Match": `"3"`},
	)
	if reblocked.Code != http.StatusOK || decodeTaskLifecycleResponse(t, reblocked.Body.Bytes()).Task.Version != 4 {
		t.Fatalf("reblock source Task = %d: %s", reblocked.Code, reblocked.Body.String())
	}
	var sources []models.InboxItem
	if err := store.DB.Where("source_entity_type = ? AND source_entity_id = ?", taskBlockedInboxSourceType, task.ID).
		Order("source_event_key ASC").Find(&sources).Error; err != nil {
		t.Fatalf("load repeated blocked sources: %v", err)
	}
	if len(sources) != 2 || sources[0].SourceEventKey == nil || sources[1].SourceEventKey == nil ||
		*sources[0].SourceEventKey != taskBlockedEventKey(task.ID, 2) ||
		*sources[1].SourceEventKey != taskBlockedEventKey(task.ID, 4) {
		t.Fatalf("repeated blocked sources = %#v", sources)
	}
	resolveSecond := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+sources[1].ID+"/resolve",
		[]byte(`{"reason":"Second block acknowledged"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if resolveSecond.Code != http.StatusOK {
		t.Fatalf("resolve second blocked source = %d: %s", resolveSecond.Code, resolveSecond.Body.String())
	}

	deleted := performRequest(
		router,
		http.MethodDelete,
		"/api/v1/tasks/"+task.ID+"?confirm=true",
		nil,
		map[string]string{"If-Match": `"4"`},
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete coordinated blocked source Task = %d: %s", deleted.Code, deleted.Body.String())
	}
	var retained models.InboxItem
	if err := store.DB.First(&retained, "id = ?", source.ID).Error; err != nil {
		t.Fatalf("load retained blocked source: %v", err)
	}
	if retained.SourceDeletedAt == nil || retained.Status != "resolved" || retained.Version != 3 ||
		!strings.Contains(retained.PayloadJSON, `"task_title":"Blocked source task"`) ||
		!strings.Contains(retained.PayloadJSON, `"blocked_reason":"Waiting for a local approval"`) {
		t.Fatalf("retained blocked source = %#v", retained)
	}
	var sourceDeletedEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).
		Where("aggregate_type = 'inbox_item' AND aggregate_id = ? AND action = 'source_deleted'", source.ID).
		Count(&sourceDeletedEvents).Error; err != nil {
		t.Fatalf("count source_deleted events: %v", err)
	}
	if sourceDeletedEvents != 1 {
		t.Fatalf("source_deleted events = %d, want 1", sourceDeletedEvents)
	}
}

func TestTaskLifecycleCancellationEndsBothRolesWithOneTaskVersion(t *testing.T) {
	router, store := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Cancel assigned task"}`)
	person := createActorForTest(t, router, `{"type":"person","display_name":"Cancelled assignee"}`, nil)
	createAssignmentForTest(t, router, task.ID, "assignee", person.ID, 1, "")
	createAssignmentForTest(t, router, task.ID, "reviewer", models.BuiltinOwnerActorID, 2, "")

	requestID := uuid.NewString()
	cancelledRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/cancel",
		[]byte(`{"reason":"Scope removed"}`),
		map[string]string{"If-Match": `"3"`, "X-Request-ID": requestID},
	)
	cancelled := decodeTaskLifecycleResponse(t, cancelledRecorder.Body.Bytes())
	if cancelledRecorder.Code != http.StatusOK || cancelled.Task.Version != 4 || cancelled.Task.Status != "cancelled" ||
		cancelled.Event.CommandSeq == nil || *cancelled.Event.CommandSeq != 3 {
		t.Fatalf("cancel assigned task = %d: %#v", cancelledRecorder.Code, cancelled)
	}

	dataRecorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/assignments", nil, nil)
	data, meta := decodeAssignmentList(t, dataRecorder.Body.Bytes())
	if data.Active.Assignee != nil || data.Active.Reviewer != nil || len(data.History) != 2 || meta.TaskVersion != 4 {
		t.Fatalf("cancel assignment list = %s", dataRecorder.Body.String())
	}
	for _, assignment := range data.History {
		if assignment.Reason == nil || *assignment.Reason != taskCancelledReason || assignment.IsActive {
			t.Fatalf("cancel-ended assignment = %#v", assignment)
		}
	}

	var rows []models.WorkflowEvent
	if err := store.DB.Where("request_id = ?", requestID).Order("command_seq ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load cancellation workflow events: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("cancellation event count = %d, want 3", len(rows))
	}
	for index, event := range rows {
		wantSequence := index + 1
		if event.CommandSeq == nil || *event.CommandSeq != wantSequence {
			t.Fatalf("cancellation event sequence[%d] = %#v", index, event.CommandSeq)
		}
		if index < 2 && event.Action != "assignment_ended" {
			t.Fatalf("cancellation assignment event[%d] = %s", index, event.Action)
		}
	}
	if rows[2].Action != "task_cancelled" {
		t.Fatalf("last cancellation event action = %s", rows[2].Action)
	}

	reopened := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reopen",
		[]byte(`{}`),
		map[string]string{"If-Match": `"4"`},
	)
	if reopened.Code != http.StatusOK || decodeTaskLifecycleResponse(t, reopened.Body.Bytes()).Task.Version != 5 {
		t.Fatalf("reopen cancellation = %d: %s", reopened.Code, reopened.Body.String())
	}
}

func TestTaskWorkflowEventsExposeReassignmentReason(t *testing.T) {
	router, _ := newActorTestAPI(t)
	task := createTaskForTaskFacts(t, router, `{"title":"Reassignment timeline reason"}`)
	first := createActorForTest(t, router, `{"type":"person","display_name":"First assignee"}`, nil)
	second := createActorForTest(t, router, `{"type":"person","display_name":"Second assignee"}`, nil)
	createAssignmentForTest(t, router, task.ID, "assignee", first.ID, 1, "")

	reassigned := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/reassign",
		[]byte(fmt.Sprintf(`{"role":"assignee","actor_id":%q,"reason":"Capacity changed"}`, second.ID)),
		map[string]string{"If-Match": `"2"`},
	)
	if reassigned.Code != http.StatusOK {
		t.Fatalf("reassign task = %d: %s", reassigned.Code, reassigned.Body.String())
	}

	recorder := performRequest(router, http.MethodGet, "/api/v1/tasks/"+task.ID+"/events?page=1&page_size=100", nil, nil)
	events, _ := decodeTaskWorkflowEvents(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK {
		t.Fatalf("list task workflow events = %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, event := range events {
		if event.Action == "assignment_reassigned" {
			if event.Reason == nil || *event.Reason != "Capacity changed" {
				t.Fatalf("reassignment reason = %#v", event.Reason)
			}
			return
		}
	}
	t.Fatalf("assignment_reassigned event missing: %#v", events)
}

func TestTaskLifecycleCommandTransitionMatrix(t *testing.T) {
	router, store := newActorTestAPI(t)
	statuses := []string{"todo", "in_progress", "blocked", "waiting_review", "done", "cancelled"}
	allowedSources := map[string]map[string]bool{
		taskLifecycleStart:    {"todo": true},
		taskLifecycleBlock:    {"todo": true, "in_progress": true, "waiting_review": true},
		taskLifecycleUnblock:  {"blocked": true},
		taskLifecycleComplete: {"todo": true, "in_progress": true},
		taskLifecycleCancel:   {"todo": true, "in_progress": true, "blocked": true, "waiting_review": true},
		taskLifecycleReopen:   {"done": true, "cancelled": true},
	}
	targets := map[string]string{
		taskLifecycleStart:    "in_progress",
		taskLifecycleBlock:    "blocked",
		taskLifecycleUnblock:  "in_progress",
		taskLifecycleComplete: "done",
		taskLifecycleCancel:   "cancelled",
		taskLifecycleReopen:   "todo",
	}
	for command, sources := range allowedSources {
		for _, status := range statuses {
			t.Run(command+"_from_"+status, func(t *testing.T) {
				task := seedTaskForLifecycleMatrix(t, store.DB, command+" from "+status, status)
				if command == taskLifecycleStart && status == "todo" {
					assignment := models.TaskAssignment{
						ID: uuid.NewString(), TaskID: task.ID, ActorID: models.BuiltinOwnerActorID,
						Role: "assignee", AssignedByActorID: models.BuiltinOwnerActorID,
						AssignedAt: task.CreatedAt,
					}
					if err := store.DB.Create(&assignment).Error; err != nil {
						t.Fatalf("seed matrix assignee: %v", err)
					}
				}
				body := `{}`
				if command == taskLifecycleBlock || command == taskLifecycleCancel {
					body = `{"reason":"matrix reason"}`
				}
				recorder := performRequest(
					router,
					http.MethodPost,
					"/api/v1/tasks/"+task.ID+"/"+command,
					[]byte(body),
					map[string]string{"If-Match": `"1"`},
				)
				if sources[status] {
					if recorder.Code != http.StatusOK {
						t.Fatalf("allowed transition = %d: %s", recorder.Code, recorder.Body.String())
					}
					response := decodeTaskLifecycleResponse(t, recorder.Body.Bytes())
					if response.Task.Status != targets[command] || response.Task.Version != 2 {
						t.Fatalf("allowed transition response = %#v", response.Task)
					}
					return
				}
				if recorder.Code != http.StatusConflict || responseErrorCode(t, recorder.Body.Bytes()) != "TASK_TRANSITION_NOT_ALLOWED" {
					t.Fatalf("forbidden transition = %d: %s", recorder.Code, recorder.Body.String())
				}
				var version int64
				if err := store.SQL.QueryRow("SELECT version FROM tasks WHERE id = ?", task.ID).Scan(&version); err != nil || version != 1 {
					t.Fatalf("forbidden transition changed version=%d err=%v", version, err)
				}
			})
		}
	}
}

func seedTaskForLifecycleMatrix(t *testing.T, db *gorm.DB, title, status string) models.Task {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := models.Task{
		ID: uuid.NewString(), Title: title, Description: "", Kind: "work", Status: status,
		ReviewPolicy: "none", Priority: "P2", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	switch status {
	case "blocked":
		reason := "matrix block"
		from := "in_progress"
		task.BlockedReason = &reason
		task.BlockedAt = &now
		task.BlockedFromStatus = &from
	case "waiting_review":
		task.ReviewPolicy = "manual"
		task.SubmittedAt = &now
	case "done":
		task.CompletedAt = &now
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("seed %s lifecycle Task: %v", status, err)
	}
	if status == "waiting_review" {
		submission := models.TaskSubmission{
			ID: uuid.NewString(), TaskID: task.ID, Sequence: 1, Status: "pending_review",
			Summary: "matrix submission", SubmittedByActorID: models.BuiltinOwnerActorID, SubmittedAt: now,
		}
		if err := db.Create(&submission).Error; err != nil {
			t.Fatalf("seed waiting-review submission: %v", err)
		}
		if err := db.Model(&models.Task{}).Where("id = ?", task.ID).UpdateColumn("current_submission_id", submission.ID).Error; err != nil {
			t.Fatalf("link waiting-review submission: %v", err)
		}
		task.CurrentSubmissionID = &submission.ID
	}
	return task
}

func TestTaskLifecycleValidationDeprecatedStatusAndManualReview(t *testing.T) {
	router, store := newActorTestAPI(t)

	for _, status := range []string{"in_progress", "blocked", "waiting_review", "done", "cancelled"} {
		recorder := performRequest(
			router,
			http.MethodPost,
			"/api/v1/tasks",
			[]byte(fmt.Sprintf(`{"title":"Invalid initial status","status":%q}`, status)),
			nil,
		)
		if recorder.Code != http.StatusUnprocessableEntity || responseErrorCode(t, recorder.Body.Bytes()) != "LIFECYCLE_COMMAND_REQUIRED" {
			t.Fatalf("create status %s = %d: %s", status, recorder.Code, recorder.Body.String())
		}
	}
	task := createTaskForTaskFacts(t, router, `{"title":"Deprecated status endpoint","status":"todo"}`)
	if task.ReviewPolicy != "none" || task.BlockedReason != nil || task.SubmittedAt != nil || task.ReviewedAt != nil {
		t.Fatalf("new Task workflow defaults = %#v", task)
	}
	deprecated := performRequest(
		router,
		http.MethodPatch,
		"/api/v1/tasks/"+task.ID+"/status",
		[]byte(`{"status":"done"}`),
		nil,
	)
	if deprecated.Code != http.StatusGone || responseErrorCode(t, deprecated.Body.Bytes()) != "TASK_STATUS_ENDPOINT_DEPRECATED" {
		t.Fatalf("deprecated status endpoint = %d: %s", deprecated.Code, deprecated.Body.String())
	}
	if loaded := getTaskForTaskFacts(t, router, task.ID); loaded.Status != "todo" || loaded.Version != 1 {
		t.Fatalf("deprecated status endpoint mutated task = %#v", loaded)
	}

	validationCases := []struct {
		name    string
		path    string
		body    string
		headers map[string]string
		status  int
		code    string
	}{
		{name: "missing If-Match", path: "/start", body: `{}`, status: http.StatusPreconditionRequired, code: "VERSION_REQUIRED"},
		{name: "invalid If-Match", path: "/start", body: `{}`, headers: map[string]string{"If-Match": `"bad"`}, status: http.StatusBadRequest, code: "INVALID_VERSION"},
		{name: "missing reason", path: "/block", body: `{}`, headers: map[string]string{"If-Match": `"1"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "blank reason", path: "/cancel", body: `{"reason":"  "}`, headers: map[string]string{"If-Match": `"1"`}, status: http.StatusUnprocessableEntity, code: "VALIDATION_ERROR"},
		{name: "unknown field", path: "/reopen", body: `{"force":true}`, headers: map[string]string{"If-Match": `"1"`}, status: http.StatusBadRequest, code: "INVALID_JSON"},
		{name: "invalid idempotency", path: "/complete", body: `{}`, headers: map[string]string{"If-Match": `"1"`, "Idempotency-Key": "contains space"}, status: http.StatusBadRequest, code: "INVALID_IDEMPOTENCY_KEY"},
	}
	for _, test := range validationCases {
		t.Run(test.name, func(t *testing.T) {
			recorder := performRequest(router, http.MethodPost, "/api/v1/tasks/"+task.ID+test.path, []byte(test.body), test.headers)
			if recorder.Code != test.status || responseErrorCode(t, recorder.Body.Bytes()) != test.code {
				t.Fatalf("validation = %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}

	if err := store.DB.Model(&models.Task{}).Where("id = ?", task.ID).Update("review_policy", "manual").Error; err != nil {
		t.Fatalf("seed manual review policy: %v", err)
	}
	manualComplete := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+task.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"1"`},
	)
	if manualComplete.Code != http.StatusConflict || responseErrorCode(t, manualComplete.Body.Bytes()) != "TASK_REVIEW_REQUIRED" {
		t.Fatalf("manual task complete = %d: %s", manualComplete.Code, manualComplete.Body.String())
	}

	invalidTransitions := []struct {
		path string
		body string
	}{
		{path: "/unblock", body: `{}`},
		{path: "/reopen", body: `{}`},
	}
	for _, test := range invalidTransitions {
		recorder := performRequest(
			router,
			http.MethodPost,
			"/api/v1/tasks/"+task.ID+test.path,
			[]byte(test.body),
			map[string]string{"If-Match": `"1"`},
		)
		if recorder.Code != http.StatusConflict || responseErrorCode(t, recorder.Body.Bytes()) != "TASK_TRANSITION_NOT_ALLOWED" {
			t.Fatalf("invalid transition %s = %d: %s", test.path, recorder.Code, recorder.Body.String())
		}
	}

	invalidTaskID := performRequest(router, http.MethodPost, "/api/v1/tasks/not-a-uuid/start", []byte(`{}`), map[string]string{"If-Match": `"1"`})
	if invalidTaskID.Code != http.StatusBadRequest || responseErrorCode(t, invalidTaskID.Body.Bytes()) != "INVALID_TASK_ID" {
		t.Fatalf("invalid lifecycle task id = %d: %s", invalidTaskID.Code, invalidTaskID.Body.String())
	}
	missingEvents := performRequest(router, http.MethodGet, "/api/v1/tasks/"+uuid.NewString()+"/events", nil, nil)
	if missingEvents.Code != http.StatusNotFound || responseErrorCode(t, missingEvents.Body.Bytes()) != "TASK_NOT_FOUND" {
		t.Fatalf("missing task events = %d: %s", missingEvents.Code, missingEvents.Body.String())
	}
}

func TestTodayStatsExcludeCancelledTaskWorkButKeepActualMinutes(t *testing.T) {
	router, store := newActorTestAPI(t)
	today := time.Now().UTC().Format("2006-01-02")
	pastDue := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	create := func(title string, estimated, actual int) models.Task {
		t.Helper()
		task := createTaskForTaskFacts(t, router, fmt.Sprintf(
			`{"title":%q,"planned_date":%q,"due_date":%q,"estimated_minutes":%d}`,
			title, today, pastDue, estimated,
		))
		if err := store.DB.Model(&models.Task{}).Where("id = ?", task.ID).Update("actual_minutes", actual).Error; err != nil {
			t.Fatalf("set actual minutes: %v", err)
		}
		return task
	}
	active := create("Active planned task", 30, 5)
	completed := create("Completed planned task", 20, 7)
	cancelled := create("Cancelled planned task", 40, 11)

	completeRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+completed.ID+"/complete",
		[]byte(`{}`),
		map[string]string{"If-Match": `"1"`},
	)
	if completeRecorder.Code != http.StatusOK {
		t.Fatalf("complete stats task = %d: %s", completeRecorder.Code, completeRecorder.Body.String())
	}
	cancelRecorder := performRequest(
		router,
		http.MethodPost,
		"/api/v1/tasks/"+cancelled.ID+"/cancel",
		[]byte(`{"reason":"Not doing this"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if cancelRecorder.Code != http.StatusOK {
		t.Fatalf("cancel stats task = %d: %s", cancelRecorder.Code, cancelRecorder.Body.String())
	}

	statsRecorder := performRequest(router, http.MethodGet, "/api/v1/stats/today?date="+today, nil, nil)
	if statsRecorder.Code != http.StatusOK {
		t.Fatalf("today stats = %d: %s", statsRecorder.Code, statsRecorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Tasks taskStats `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statsRecorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode today stats: %v", err)
	}
	if envelope.Data.Tasks.Total != 2 || envelope.Data.Tasks.Completed != 1 || envelope.Data.Tasks.Remaining != 1 ||
		envelope.Data.Tasks.Overdue != 1 || envelope.Data.Tasks.DueSoon != 0 || envelope.Data.Tasks.EstimatedMinutes != 50 ||
		envelope.Data.Tasks.ActualMinutes != 23 {
		t.Fatalf("cancel-aware today stats = %#v (active=%s)", envelope.Data.Tasks, active.ID)
	}
}
