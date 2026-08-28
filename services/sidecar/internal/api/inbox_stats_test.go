package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestInboxStatsDeriveVisibleRiskCounts(t *testing.T) {
	clock := &inboxTestClock{now: time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)}
	router, store := newInboxTestAPI(t, clock)
	open := createInboxItemForTest(t, router, `{"title":"普通待处理"}`, "")
	blockedInbox := createInboxItemForTest(t, router, `{"title":"阻塞跟进"}`, "")
	reviewInbox := createInboxItemForTest(t, router, `{"title":"等待验收"}`, "")
	snoozed := createInboxItemForTest(t, router, `{"title":"稍后处理"}`, "")
	resolved := createInboxItemForTest(t, router, `{"title":"已经归档"}`, "")

	now := formatInboxTimestamp(clock.now)
	blockedReason := "等待本地资料"
	blockedFrom := "todo"
	blockedTask := models.Task{
		ID: uuid.NewString(), Title: "阻塞任务", Description: "", Kind: "work", Status: "blocked",
		ReviewPolicy: "none", Priority: "P2", CompletionCriteria: "", ActualMinutes: 0, Version: 1,
		BlockedReason: &blockedReason, BlockedAt: &now, BlockedFromStatus: &blockedFrom,
		CreatedAt: now, UpdatedAt: now,
	}
	waitingTask := models.Task{
		ID: uuid.NewString(), Title: "待验收任务", Description: "", Kind: "review", Status: "waiting_review",
		ReviewPolicy: "manual", Priority: "P1", CompletionCriteria: "", ActualMinutes: 0, Version: 1,
		SubmittedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.DB.Create(&blockedTask).Error; err != nil {
		t.Fatalf("create blocked Task: %v", err)
	}
	if err := store.DB.Create(&waitingTask).Error; err != nil {
		t.Fatalf("create waiting-review Task: %v", err)
	}

	for _, relation := range []struct {
		inboxID string
		taskID  string
	}{
		{inboxID: blockedInbox.ID, taskID: blockedTask.ID},
		{inboxID: reviewInbox.ID, taskID: waitingTask.ID},
	} {
		response := performRequest(
			router, http.MethodPost, "/api/v1/inbox-items/"+relation.inboxID+"/tasks/"+relation.taskID,
			[]byte(`{"is_required":true}`), map[string]string{"If-Match": `"1"`},
		)
		if response.Code != http.StatusCreated {
			t.Fatalf("link Task = %d: %s", response.Code, response.Body.String())
		}
	}
	read := performRequest(router, http.MethodPost, "/api/v1/inbox-items/"+reviewInbox.ID+"/read", []byte(`{}`), map[string]string{"If-Match": `"2"`})
	if read.Code != http.StatusOK {
		t.Fatalf("read tracking Inbox = %d: %s", read.Code, read.Body.String())
	}
	snooze := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+snoozed.ID+"/snooze",
		[]byte(`{"snoozed_until":"2026-08-29T18:00:00Z"}`), map[string]string{"If-Match": `"1"`},
	)
	if snooze.Code != http.StatusOK {
		t.Fatalf("snooze Inbox = %d: %s", snooze.Code, snooze.Body.String())
	}
	resolve := performRequest(
		router, http.MethodPost, "/api/v1/inbox-items/"+resolved.ID+"/resolve",
		[]byte(`{"reason":"处理完成"}`), map[string]string{"If-Match": `"1"`},
	)
	if resolve.Code != http.StatusOK {
		t.Fatalf("resolve Inbox = %d: %s", resolve.Code, resolve.Body.String())
	}

	response := performRequest(router, http.MethodGet, "/api/v1/stats/inbox", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("Inbox stats = %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			ServerNow     string `json:"server_now"`
			Pending       int    `json:"pending"`
			Unread        int    `json:"unread"`
			Tracking      int    `json:"tracking"`
			Blocked       int    `json:"blocked"`
			WaitingReview int    `json:"waiting_review"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode Inbox stats: %v", err)
	}
	if envelope.Data.ServerNow != now || envelope.Data.Pending != 3 || envelope.Data.Unread != 2 ||
		envelope.Data.Tracking != 2 || envelope.Data.Blocked != 1 || envelope.Data.WaitingReview != 1 {
		t.Fatalf("Inbox stats = %#v (open=%s)", envelope.Data, open.ID)
	}
	for _, filter := range []struct {
		risk string
		want string
	}{
		{risk: "blocked", want: blockedInbox.ID},
		{risk: "waiting_review", want: reviewInbox.ID},
	} {
		filtered := performRequest(router, http.MethodGet, "/api/v1/inbox-items?view=inbox&risk="+filter.risk, nil, nil)
		if filtered.Code != http.StatusOK {
			t.Fatalf("filter %s = %d: %s", filter.risk, filtered.Code, filtered.Body.String())
		}
		var list struct {
			Data []models.InboxItem `json:"data"`
		}
		if err := json.Unmarshal(filtered.Body.Bytes(), &list); err != nil || len(list.Data) != 1 || list.Data[0].ID != filter.want {
			t.Fatalf("filter %s = %s err=%v", filter.risk, filtered.Body.String(), err)
		}
	}
	invalid := performRequest(router, http.MethodGet, "/api/v1/inbox-items?risk=unknown", nil, nil)
	if invalid.Code != http.StatusBadRequest || responseErrorCode(t, invalid.Body.Bytes()) != "INVALID_FILTER" {
		t.Fatalf("invalid risk = %d: %s", invalid.Code, invalid.Body.String())
	}
}
