package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/opc-workspace/opc-sidecar/internal/database"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

type inboxTestClock struct {
	now time.Time
}

func (clock *inboxTestClock) Now() time.Time { return clock.now }
func (clock *inboxTestClock) Set(value time.Time) {
	clock.now = value
}

func newInboxTestAPI(t *testing.T, clock *inboxTestClock) (*Router, *database.Store) {
	t.Helper()
	store, err := database.Open(filepath.Join(t.TempDir(), "inbox-api.db"))
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	router, err := NewRouter(store.DB, Options{
		AppVersion: "test", Commit: "test", SchemaVersion: store.SchemaVersion,
		SessionToken: testToken, AllowedOrigins: []string{"tauri://localhost"},
		Logger: log.New(io.Discard, "", 0), Now: clock.Now, FocusHeartbeatInterval: -1,
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("NewRouter() error = %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close()
		_ = store.Close()
	})
	return router, store
}

func createInboxItemForTest(t *testing.T, router http.Handler, body, key string) inboxItemOutput {
	t.Helper()
	headers := map[string]string{}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	response := performRequest(router, http.MethodPost, "/api/v1/inbox-items", []byte(body), headers)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Inbox Item = %d: %s", response.Code, response.Body.String())
	}
	return decodeInboxItemData(t, response.Body.Bytes())
}

func decodeInboxItemData(t *testing.T, body []byte) inboxItemOutput {
	t.Helper()
	var response struct {
		Data inboxItemOutput `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Inbox Item response: %v: %s", err, body)
	}
	return response.Data
}

func TestInboxCreateListDetailAndManualBoundary(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 9, 0, 0, 101, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	body := `{
		"kind":"manual",
		"title":"  跟进本地事项  ",
		"summary":"  核对范围  ",
		"source_entity_type":"manual",
		"priority":"P2",
		"resolution_policy":"manual",
		"due_at":"2026-08-29T18:30:00+08:00",
		"payload_json":{"origin":"owner","sequence":1}
	}`
	createdResponse := performRequest(router, http.MethodPost, "/api/v1/inbox-items", []byte(body), map[string]string{"Idempotency-Key": "inbox-create-1"})
	if createdResponse.Code != http.StatusCreated || createdResponse.Header().Get("ETag") != `"1"` {
		t.Fatalf("create = %d headers=%v: %s", createdResponse.Code, createdResponse.Header(), createdResponse.Body.String())
	}
	created := decodeInboxItemData(t, createdResponse.Body.Bytes())
	if created.Title != "跟进本地事项" || created.Summary != "核对范围" || created.Kind != "manual" ||
		created.SourceEntityType != "manual" || created.SourceEntityID != nil || created.SourceEventKey != nil ||
		created.ResolutionPolicy != "manual" || created.DueAt == nil || *created.DueAt != "2026-08-29T10:30:00Z" ||
		created.PayloadJSON["origin"] != "owner" || created.Version != 1 || created.Status != "open" {
		t.Fatalf("created Inbox Item = %#v", created)
	}
	if len(created.AvailableActions) != 5 || created.AvailableActions[0] != "edit" || created.AvailableActions[1] != "read" {
		t.Fatalf("created available_actions = %#v", created.AvailableActions)
	}

	replayed := performRequest(router, http.MethodPost, "/api/v1/inbox-items", []byte(body), map[string]string{"Idempotency-Key": "inbox-create-1"})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || decodeInboxItemData(t, replayed.Body.Bytes()).ID != created.ID {
		t.Fatalf("create replay = %d headers=%v: %s", replayed.Code, replayed.Header(), replayed.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/inbox-items", []byte(`{"title":"另一条事项"}`), map[string]string{"Idempotency-Key": "inbox-create-1"})
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("create idempotency conflict = %d: %s", conflict.Code, conflict.Body.String())
	}

	for name, invalidBody := range map[string]string{
		"event kind":         `{"kind":"event","title":"非法来源"}`,
		"task source":        `{"title":"非法来源","source_entity_type":"task"}`,
		"source key":         `{"title":"非法来源","source_event_key":"event:1"}`,
		"derived policy":     `{"title":"非法策略","resolution_policy":"all_required_tasks_done"}`,
		"non-object payload": `{"title":"非法载荷","payload_json":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			invalid := performRequest(router, http.MethodPost, "/api/v1/inbox-items", []byte(invalidBody), nil)
			if invalid.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalid.Body.Bytes()) != "VALIDATION_ERROR" {
				t.Fatalf("invalid manual boundary = %d: %s", invalid.Code, invalid.Body.String())
			}
		})
	}

	clock.Set(clock.now.Add(time.Nanosecond))
	high := createInboxItemForTest(t, router, `{"title":"紧急范围确认","summary":"匹配 Needle","priority":"P0"}`, "")
	list := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=inbox&page=1&page_size=1", nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", list.Code, list.Body.String())
	}
	var listed struct {
		Data []inboxItemOutput `json:"data"`
		Meta inboxListMeta     `json:"meta"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != high.ID || listed.Meta.Total != 2 || listed.Meta.UnreadTotal != 2 || listed.Meta.SnapshotAt == "" || listed.Meta.ServerNow != listed.Meta.SnapshotAt {
		t.Fatalf("list response = %s", list.Body.String())
	}
	filtered := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=inbox&q=needle&priority=P0", nil, nil)
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered list = %d: %s", filtered.Code, filtered.Body.String())
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode filtered list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != high.ID || listed.Meta.Total != 1 || listed.Meta.UnreadTotal != 2 {
		t.Fatalf("filtered list/global unread = %s", filtered.Body.String())
	}
	detail := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+created.ID, nil, nil)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") != `"1"` || decodeInboxItemData(t, detail.Body.Bytes()).ID != created.ID {
		t.Fatalf("detail = %d headers=%v: %s", detail.Code, detail.Header(), detail.Body.String())
	}
	var createdEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).Where("aggregate_type = 'inbox_item' AND action = 'created'").Count(&createdEvents).Error; err != nil || createdEvents != 2 {
		t.Fatalf("created event count=%d err=%v", createdEvents, err)
	}
}

func TestInboxPatchUsesOptimisticLockAndRollsBackWithoutAudit(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"待编辑事项","summary":"old"}`, "")
	clock.Set(clock.now.Add(time.Second))
	updatedResponse := performRequest(
		router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID,
		[]byte(`{"title":"编辑后的事项","summary":null,"priority":"P1","due_at":"2026-08-29T09:00:00+08:00"}`),
		map[string]string{"If-Match": `"1"`},
	)
	if updatedResponse.Code != http.StatusOK || updatedResponse.Header().Get("ETag") != `"2"` {
		t.Fatalf("patch = %d headers=%v: %s", updatedResponse.Code, updatedResponse.Header(), updatedResponse.Body.String())
	}
	updated := decodeInboxItemData(t, updatedResponse.Body.Bytes())
	if updated.Title != "编辑后的事项" || updated.Summary != "" || updated.Priority != "P1" ||
		updated.DueAt == nil || *updated.DueAt != "2026-08-29T01:00:00Z" || updated.TriagedAt == nil || updated.Version != 2 {
		t.Fatalf("updated Inbox Item = %#v", updated)
	}
	stale := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID, []byte(`{"title":"旧写入"}`), map[string]string{"If-Match": `"1"`})
	if stale.Code != http.StatusConflict || responseErrorCode(t, stale.Body.Bytes()) != "VERSION_CONFLICT" {
		t.Fatalf("stale patch = %d: %s", stale.Code, stale.Body.String())
	}
	noOp := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID, []byte(`{"title":"编辑后的事项"}`), map[string]string{"If-Match": `"2"`})
	if noOp.Code != http.StatusOK || noOp.Header().Get("ETag") != `"2"` {
		t.Fatalf("no-op patch = %d headers=%v: %s", noOp.Code, noOp.Header(), noOp.Body.String())
	}

	if err := store.DB.Exec(`
		CREATE TRIGGER fail_inbox_updated_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'inbox_item' AND NEW.action = 'updated'
		BEGIN
			SELECT RAISE(ABORT, 'FORCED_INBOX_EVENT_FAILURE');
		END
	`).Error; err != nil {
		t.Fatalf("create rollback trigger: %v", err)
	}
	failed := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID, []byte(`{"title":"不得保留"}`), map[string]string{"If-Match": `"2"`})
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed audited patch = %d: %s", failed.Code, failed.Body.String())
	}
	if err := store.DB.Exec("DROP TRIGGER fail_inbox_updated_event").Error; err != nil {
		t.Fatalf("drop rollback trigger: %v", err)
	}
	current := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID, nil, nil)
	currentItem := decodeInboxItemData(t, current.Body.Bytes())
	if currentItem.Title != updated.Title || currentItem.Version != 2 {
		t.Fatalf("failed transaction leaked mutation: %#v", currentItem)
	}

	clock.Set(clock.now.Add(time.Second))
	success := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID, []byte(`{"title":"最终事项"}`), map[string]string{"If-Match": `"2"`})
	if success.Code != http.StatusOK || success.Header().Get("ETag") != `"3"` {
		t.Fatalf("final patch = %d: %s", success.Code, success.Body.String())
	}
	events := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/events?page=1&page_size=1", nil, nil)
	if events.Code != http.StatusOK || events.Header().Get("ETag") != `"3"` {
		t.Fatalf("event list = %d headers=%v: %s", events.Code, events.Header(), events.Body.String())
	}
	var eventList struct {
		Data []inboxWorkflowEventOutput `json:"data"`
		Meta inboxWorkflowEventMeta     `json:"meta"`
	}
	if err := json.Unmarshal(events.Body.Bytes(), &eventList); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(eventList.Data) != 1 || eventList.Data[0].Action != "updated" || eventList.Data[0].ActorID == nil ||
		*eventList.Data[0].ActorID != models.BuiltinOwnerActorID || eventList.Data[0].Actor == nil ||
		eventList.Meta.Total != 3 || eventList.Meta.InboxItemVersion != 3 {
		t.Fatalf("event list = %s", events.Body.String())
	}
}

func TestInboxCommandsPreserveIndependentFactsAndReplayBeforeVersion(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC)}
	router, _ := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"命令流转事项"}`, "")

	clock.Set(clock.now.Add(time.Second))
	read := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"1"`, "Idempotency-Key": "inbox-read-1"})
	if read.Code != http.StatusOK || read.Header().Get("ETag") != `"2"` {
		t.Fatalf("read = %d headers=%v: %s", read.Code, read.Header(), read.Body.String())
	}
	readItem := decodeInboxItemData(t, read.Body.Bytes())
	if readItem.ReadAt == nil || readItem.TriagedAt != nil {
		t.Fatalf("read changed triage facts: %#v", readItem)
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"1"`, "Idempotency-Key": "inbox-read-1"})
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" || replay.Header().Get("ETag") != `"2"` {
		t.Fatalf("read replay = %d headers=%v: %s", replay.Code, replay.Header(), replay.Body.String())
	}
	conflict := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"2"`, "Idempotency-Key": "inbox-read-1"})
	if conflict.Code != http.StatusConflict || responseErrorCode(t, conflict.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("read replay different version = %d: %s", conflict.Code, conflict.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	snoozeUntil := clock.now.Add(time.Hour).Format(time.RFC3339Nano)
	snoozed := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/snooze", []byte(fmt.Sprintf(`{"snoozed_until":%q}`, snoozeUntil)), map[string]string{"If-Match": `"2"`})
	if snoozed.Code != http.StatusOK || snoozed.Header().Get("ETag") != `"3"` {
		t.Fatalf("snooze = %d: %s", snoozed.Code, snoozed.Body.String())
	}
	snoozedItem := decodeInboxItemData(t, snoozed.Body.Bytes())
	if snoozedItem.ReadAt == nil || snoozedItem.TriagedAt == nil || snoozedItem.SnoozedUntil == nil {
		t.Fatalf("snoozed independent facts = %#v", snoozedItem)
	}
	inboxView := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=inbox", nil, nil)
	snoozedView := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=snoozed", nil, nil)
	if !responseHasInboxCount(t, inboxView.Body.Bytes(), 0) || !responseHasInboxCount(t, snoozedView.Body.Bytes(), 1) {
		t.Fatalf("view split inbox=%s snoozed=%s", inboxView.Body.String(), snoozedView.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	unsnoozed := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/unsnooze", []byte(`{}`), map[string]string{"If-Match": `"3"`})
	if unsnoozed.Code != http.StatusOK || decodeInboxItemData(t, unsnoozed.Body.Bytes()).SnoozedUntil != nil {
		t.Fatalf("unsnooze = %d: %s", unsnoozed.Code, unsnoozed.Body.String())
	}
	clock.Set(clock.now.Add(time.Second))
	resolved := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/resolve", []byte(`{"reason":"已人工处理"}`), map[string]string{"If-Match": `"4"`})
	resolvedItem := decodeInboxItemData(t, resolved.Body.Bytes())
	if resolved.Code != http.StatusOK || resolvedItem.Status != "resolved" || resolvedItem.ResolutionMode == nil ||
		*resolvedItem.ResolutionMode != "manual" || resolvedItem.ResolutionReason == nil || resolvedItem.ReadAt == nil ||
		resolvedItem.TriagedAt == nil || len(resolvedItem.AvailableActions) != 1 || resolvedItem.AvailableActions[0] != "reopen" {
		t.Fatalf("resolve = %d: %s", resolved.Code, resolved.Body.String())
	}
	terminalPatch := performRequest(router, http.MethodPatch, "/api/v1/inbox-items/"+item.ID, []byte(`{"title":"不能编辑"}`), map[string]string{"If-Match": `"5"`})
	if terminalPatch.Code != http.StatusConflict || responseErrorCode(t, terminalPatch.Body.Bytes()) != "INBOX_ITEM_TERMINAL" {
		t.Fatalf("terminal patch = %d: %s", terminalPatch.Code, terminalPatch.Body.String())
	}

	clock.Set(clock.now.Add(time.Second))
	reopened := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/reopen", []byte(`{"reason":"需要补充"}`), map[string]string{"If-Match": `"5"`})
	reopenedItem := decodeInboxItemData(t, reopened.Body.Bytes())
	if reopened.Code != http.StatusOK || reopenedItem.Status != "open" || reopenedItem.ResolvedAt != nil ||
		reopenedItem.ResolutionReason != nil || reopenedItem.ReadAt == nil || reopenedItem.TriagedAt == nil || reopenedItem.SnoozedUntil != nil {
		t.Fatalf("reopen = %d: %s", reopened.Code, reopened.Body.String())
	}
	clock.Set(clock.now.Add(time.Second))
	dismissed := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/dismiss", []byte(`{"reason":"无需处理"}`), map[string]string{"If-Match": `"6"`})
	dismissedItem := decodeInboxItemData(t, dismissed.Body.Bytes())
	if dismissed.Code != http.StatusOK || dismissedItem.Status != "dismissed" || dismissedItem.DismissReason == nil || dismissedItem.ResolutionReason != nil {
		t.Fatalf("dismiss = %d: %s", dismissed.Code, dismissed.Body.String())
	}
	badReason := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+item.ID+"/resolve", []byte(`{"reason":"  "}`), map[string]string{"If-Match": `"7"`})
	if badReason.Code != http.StatusUnprocessableEntity || responseErrorCode(t, badReason.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("blank resolve reason = %d: %s", badReason.Code, badReason.Body.String())
	}

	events := performRequest(router, http.MethodGet, "/api/v1/inbox-items/"+item.ID+"/events?page_size=20", nil, nil)
	var eventList struct {
		Data []inboxWorkflowEventOutput `json:"data"`
		Meta inboxWorkflowEventMeta     `json:"meta"`
	}
	if err := json.Unmarshal(events.Body.Bytes(), &eventList); err != nil {
		t.Fatalf("decode command events: %v", err)
	}
	wantActions := map[string]bool{"created": true, "read": true, "snoozed": true, "unsnoozed": true, "resolved": true, "reopened": true, "dismissed": true}
	if eventList.Meta.Total != 7 || len(eventList.Data) != 7 {
		t.Fatalf("command event count = %s", events.Body.String())
	}
	for _, event := range eventList.Data {
		delete(wantActions, event.Action)
	}
	if len(wantActions) != 0 {
		t.Fatalf("missing command events: %#v", wantActions)
	}
}

func TestInboxUnreadTerminalItemsRemainReadable(t *testing.T) {
	for _, command := range []string{"resolve", "dismiss"} {
		t.Run(command, func(t *testing.T) {
			clock := &inboxTestClock{now: time.Date(2026, 8, 28, 11, 30, 0, 0, time.UTC)}
			router, _ := newInboxTestAPI(t, clock)
			item := createInboxItemForTest(t, router, fmt.Sprintf(`{"title":%q}`, command+" unread item"), "")

			clock.Set(clock.now.Add(time.Second))
			terminal := performRequest(
				router,
				http.MethodPost,
				"/api/v1/inbox-items/"+item.ID+"/"+command,
				[]byte(`{"reason":"人工终结"}`),
				map[string]string{"If-Match": `"1"`},
			)
			if terminal.Code != http.StatusOK || terminal.Header().Get("ETag") != `"2"` {
				t.Fatalf("%s unread item = %d headers=%v: %s", command, terminal.Code, terminal.Header(), terminal.Body.String())
			}
			terminalItem := decodeInboxItemData(t, terminal.Body.Bytes())
			if terminalItem.ReadAt != nil || len(terminalItem.AvailableActions) != 2 ||
				terminalItem.AvailableActions[0] != "read" || terminalItem.AvailableActions[1] != "reopen" {
				t.Fatalf("%s unread terminal actions = %#v item=%#v", command, terminalItem.AvailableActions, terminalItem)
			}

			clock.Set(clock.now.Add(time.Second))
			read := performRequest(
				router,
				http.MethodPost,
				"/api/v1/inbox-items/"+item.ID+"/read",
				[]byte(`{}`),
				map[string]string{"If-Match": `"2"`},
			)
			if read.Code != http.StatusOK || read.Header().Get("ETag") != `"3"` {
				t.Fatalf("read %s item = %d headers=%v: %s", command, read.Code, read.Header(), read.Body.String())
			}
			readItem := decodeInboxItemData(t, read.Body.Bytes())
			if readItem.Status != terminalItem.Status || readItem.ReadAt == nil ||
				len(readItem.AvailableActions) != 1 || readItem.AvailableActions[0] != "reopen" {
				t.Fatalf("read %s terminal item = %#v", command, readItem)
			}
			if command == "resolve" && (readItem.ResolutionReason == nil || readItem.ResolvedAt == nil || readItem.DismissedAt != nil) {
				t.Fatalf("read changed resolved facts: %#v", readItem)
			}
			if command == "dismiss" && (readItem.DismissReason == nil || readItem.DismissedAt == nil || readItem.ResolvedAt != nil) {
				t.Fatalf("read changed dismissed facts: %#v", readItem)
			}
		})
	}
}

func TestInboxReadAllUsesSnapshotViewAndIsIdempotent(t *testing.T) {
	base := time.Date(2026, 8, 28, 12, 0, 0, 100, time.UTC)
	clock := &inboxTestClock{now: base}
	router, store := newInboxTestAPI(t, clock)
	visible := createInboxItemForTest(t, router, `{"title":"快照内可见"}`, "")

	clock.Set(base.Add(time.Nanosecond))
	snoozed := createInboxItemForTest(t, router, `{"title":"快照内稍后"}`, "")
	clock.Set(base.Add(2 * time.Nanosecond))
	snoozeUntil := base.Add(time.Second)
	snooze := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+snoozed.ID+"/snooze", []byte(fmt.Sprintf(`{"snoozed_until":%q}`, snoozeUntil.Format(time.RFC3339Nano))), map[string]string{"If-Match": `"1"`})
	if snooze.Code != http.StatusOK {
		t.Fatalf("prepare snoozed item = %d: %s", snooze.Code, snooze.Body.String())
	}

	clock.Set(base.Add(3 * time.Nanosecond))
	archived := createInboxItemForTest(t, router, `{"title":"快照内归档"}`, "")
	clock.Set(base.Add(4 * time.Nanosecond))
	resolved := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+archived.ID+"/resolve", []byte(`{"reason":"完成"}`), map[string]string{"If-Match": `"1"`})
	if resolved.Code != http.StatusOK {
		t.Fatalf("prepare archived item = %d: %s", resolved.Code, resolved.Body.String())
	}

	clock.Set(base.Add(5 * time.Nanosecond))
	visibleSecond := createInboxItemForTest(t, router, `{"title":"快照内第二条"}`, "")
	clock.Set(base.Add(6 * time.Nanosecond))
	list := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=archive&q=快照内归档", nil, nil)
	var listResponse struct {
		Data []inboxItemOutput `json:"data"`
		Meta inboxListMeta     `json:"meta"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("decode snapshot list: %v", err)
	}
	if listResponse.Meta.UnreadTotal != 2 {
		t.Fatalf("global inbox unread at snapshot = %d, want 2: %s", listResponse.Meta.UnreadTotal, list.Body.String())
	}
	cutoff := listResponse.Meta.SnapshotAt

	clock.Set(base.Add(7 * time.Nanosecond))
	afterSnapshot := createInboxItemForTest(t, router, `{"title":"快照后创建"}`, "")
	clock.Set(base.Add(2 * time.Second))
	unsnoozeAfterSnapshot := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+snoozed.ID+"/unsnooze",
		[]byte(`{}`),
		map[string]string{"If-Match": `"2"`},
	)
	if unsnoozeAfterSnapshot.Code != http.StatusOK || decodeInboxItemData(t, unsnoozeAfterSnapshot.Body.Bytes()).SnoozedUntil != nil {
		t.Fatalf("unsnooze after snapshot = %d: %s", unsnoozeAfterSnapshot.Code, unsnoozeAfterSnapshot.Body.String())
	}
	if err := store.DB.Exec(fmt.Sprintf(`
		CREATE TRIGGER fail_read_all_event
		BEFORE INSERT ON workflow_events
		WHEN NEW.aggregate_type = 'inbox_item' AND NEW.action = 'read' AND NEW.aggregate_id = '%s'
		BEGIN
			SELECT RAISE(ABORT, 'FORCED_READ_ALL_FAILURE');
		END
	`, visibleSecond.ID)).Error; err != nil {
		t.Fatalf("create read-all rollback trigger: %v", err)
	}
	failedReadAll := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/read-all",
		[]byte(fmt.Sprintf(`{"through_created_at":%q}`, cutoff)),
		map[string]string{"Idempotency-Key": "inbox-read-all-1"},
	)
	if failedReadAll.Code != http.StatusInternalServerError || readInboxItemReadAt(t, store, visible.ID) != nil || readInboxItemReadAt(t, store, visibleSecond.ID) != nil {
		t.Fatalf("read-all rollback failed response=%d visible=%v second=%v: %s", failedReadAll.Code, readInboxItemReadAt(t, store, visible.ID), readInboxItemReadAt(t, store, visibleSecond.ID), failedReadAll.Body.String())
	}
	if err := store.DB.Exec("DROP TRIGGER fail_read_all_event").Error; err != nil {
		t.Fatalf("drop read-all rollback trigger: %v", err)
	}
	readAll := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/read-all",
		[]byte(fmt.Sprintf(`{"through_created_at":%q}`, cutoff)),
		map[string]string{"Idempotency-Key": "inbox-read-all-1"},
	)
	if readAll.Code != http.StatusOK {
		t.Fatalf("read-all = %d: %s", readAll.Code, readAll.Body.String())
	}
	var marked struct {
		Data readAllInboxItemsOutput `json:"data"`
	}
	if err := json.Unmarshal(readAll.Body.Bytes(), &marked); err != nil {
		t.Fatalf("decode read-all: %v", err)
	}
	if marked.Data.MarkedCount != 2 || marked.Data.ThroughCreatedAt != cutoff {
		t.Fatalf("read-all response = %s", readAll.Body.String())
	}

	if readInboxItemReadAt(t, store, visible.ID) == nil || readInboxItemReadAt(t, store, visibleSecond.ID) == nil ||
		readInboxItemReadAt(t, store, snoozed.ID) != nil || readInboxItemReadAt(t, store, archived.ID) != nil || readInboxItemReadAt(t, store, afterSnapshot.ID) != nil {
		t.Fatalf("read-all scope visible=%v second=%v snoozed=%v archived=%v after=%v",
			readInboxItemReadAt(t, store, visible.ID), readInboxItemReadAt(t, store, visibleSecond.ID),
			readInboxItemReadAt(t, store, snoozed.ID), readInboxItemReadAt(t, store, archived.ID), readInboxItemReadAt(t, store, afterSnapshot.ID))
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/inbox-items/read-all", []byte(fmt.Sprintf(`{"through_created_at":%q}`, cutoff)), map[string]string{"Idempotency-Key": "inbox-read-all-1"})
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("read-all replay = %d headers=%v: %s", replay.Code, replay.Header(), replay.Body.String())
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &marked); err != nil || marked.Data.MarkedCount != 2 {
		t.Fatalf("read-all replay snapshot = %s err=%v", replay.Body.String(), err)
	}
	different := performRequest(router, http.MethodPost, "/api/v1/inbox-items/read-all", []byte(fmt.Sprintf(`{"through_created_at":%q}`, formatInboxTimestamp(base))), map[string]string{"Idempotency-Key": "inbox-read-all-1"})
	if different.Code != http.StatusConflict || responseErrorCode(t, different.Body.Bytes()) != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("read-all different replay = %d: %s", different.Code, different.Body.String())
	}
	future := performRequest(router, http.MethodPost, "/api/v1/inbox-items/read-all", []byte(fmt.Sprintf(`{"through_created_at":%q}`, formatInboxTimestamp(clock.now.Add(time.Second)))), nil)
	if future.Code != http.StatusUnprocessableEntity {
		t.Fatalf("future read-all cutoff = %d: %s", future.Code, future.Body.String())
	}

	listNow := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=archive&priority=P0", nil, nil)
	if err := json.Unmarshal(listNow.Body.Bytes(), &listResponse); err != nil {
		t.Fatalf("decode post read-all list: %v", err)
	}
	if listResponse.Meta.Total != 0 || listResponse.Meta.UnreadTotal != 2 {
		t.Fatalf("filtered total/global unread after read-all = %s", listNow.Body.String())
	}
	var readEvents int64
	if err := store.DB.Model(&models.WorkflowEvent{}).Where("aggregate_type = 'inbox_item' AND action = 'read'").Count(&readEvents).Error; err != nil || readEvents != 2 {
		t.Fatalf("read-all event count=%d err=%v", readEvents, err)
	}
}

func TestInboxReadAllSkipsItemsUnsnoozedAfterSnapshot(t *testing.T) {
	base := time.Date(2026, 8, 28, 12, 30, 0, 100, time.UTC)
	clock := &inboxTestClock{now: base}
	router, store := newInboxTestAPI(t, clock)
	item := createInboxItemForTest(t, router, `{"title":"稍后恢复事项"}`, "")

	clock.Set(base.Add(time.Nanosecond))
	snoozeUntil := base.Add(time.Hour)
	snoozed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+item.ID+"/snooze",
		[]byte(fmt.Sprintf(`{"snoozed_until":%q}`, snoozeUntil.Format(time.RFC3339Nano))),
		map[string]string{"If-Match": `"1"`},
	)
	if snoozed.Code != http.StatusOK {
		t.Fatalf("snooze before snapshot = %d: %s", snoozed.Code, snoozed.Body.String())
	}

	clock.Set(base.Add(2 * time.Nanosecond))
	list := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=inbox", nil, nil)
	var listed struct {
		Data []inboxItemOutput `json:"data"`
		Meta inboxListMeta     `json:"meta"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode pre-unsnooze snapshot: %v", err)
	}
	if len(listed.Data) != 0 || listed.Meta.UnreadTotal != 0 {
		t.Fatalf("future-snoozed item leaked into snapshot: %s", list.Body.String())
	}

	clock.Set(base.Add(time.Second))
	unsnoozed := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/"+item.ID+"/unsnooze",
		[]byte(`{}`),
		map[string]string{"If-Match": `"2"`},
	)
	if unsnoozed.Code != http.StatusOK || decodeInboxItemData(t, unsnoozed.Body.Bytes()).SnoozedUntil != nil {
		t.Fatalf("unsnooze after snapshot = %d: %s", unsnoozed.Code, unsnoozed.Body.String())
	}

	readAll := performRequest(
		router,
		http.MethodPost,
		"/api/v1/inbox-items/read-all",
		[]byte(fmt.Sprintf(`{"through_created_at":%q}`, listed.Meta.SnapshotAt)),
		nil,
	)
	if readAll.Code != http.StatusOK {
		t.Fatalf("old-snapshot read-all = %d: %s", readAll.Code, readAll.Body.String())
	}
	var marked struct {
		Data readAllInboxItemsOutput `json:"data"`
	}
	if err := json.Unmarshal(readAll.Body.Bytes(), &marked); err != nil {
		t.Fatalf("decode old-snapshot read-all: %v", err)
	}
	if marked.Data.MarkedCount != 0 || readInboxItemReadAt(t, store, item.ID) != nil {
		t.Fatalf("old snapshot consumed newly unsnoozed item: response=%s read_at=%v", readAll.Body.String(), readInboxItemReadAt(t, store, item.ID))
	}
}

func readInboxItemReadAt(t *testing.T, store *database.Store, id string) *string {
	t.Helper()
	var value *string
	if err := store.SQL.QueryRow("SELECT read_at FROM inbox_items WHERE id = ?", id).Scan(&value); err != nil {
		t.Fatalf("read Inbox Item read_at: %v", err)
	}
	return value
}

func responseHasInboxCount(t *testing.T, body []byte, want int) bool {
	t.Helper()
	var response struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Inbox list: %v", err)
	}
	return len(response.Data) == want
}
