package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/models"
)

func TestAIAgentInboxMergesUnplannedApprovalsAndCurrentReviewsWithoutBodies(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	planned := proposeTestAction(t, fixture.Store, fixture.Tool,
		`{"action":"task.create","changes":{"title":"PLANNED PRIVATE TITLE","description":"PLANNED PRIVATE BODY"}}`)
	unplanned := proposeTestAction(t, fixture.Store, fixture.Tool,
		`{"action":"task.create","changes":{"title":"UNPLANNED PRIVATE TITLE","description":"UNPLANNED PRIVATE BODY"}}`)
	decodePlanTool(t, planTool(t, fixture), updatePlanArgs(sampleWorkPlan(planned.ID), 0))

	submission := models.TaskSubmission{
		ID: uuid.NewString(), TaskID: fixture.Task.ID, Sequence: 1, Status: "pending_review", Origin: "manual",
		Summary: "PRIVATE REVIEW SUMMARY", SubmittedByActorID: fixture.Actor.ID, SubmittedAt: fixture.Generation.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&submission).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.Store.DB.Model(&models.Task{}).Where("id = ?", fixture.Task.ID).Updates(map[string]any{
		"status": "waiting_review", "review_policy": "manual", "submitted_at": submission.SubmittedAt,
		"current_submission_id": submission.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var automationRule models.AutomationRule
	if err := fixture.Store.DB.First(&automationRule, "preset_key = ?", automationPresetDailyToday).Error; err != nil {
		t.Fatal(err)
	}
	retryAt := "2026-09-20T09:00:00Z"
	scheduledFor := "2026-09-19T09:00:00Z"
	automationFailure := models.AutomationRun{
		ID: uuid.NewString(), RuleID: automationRule.ID, RuleVersion: automationRule.Version,
		TriggerType: "schedule", ScheduledFor: &scheduledFor, LogicalKey: "PRIVATE AUTOMATION LOGICAL KEY", DedupeKey: "PRIVATE AUTOMATION DEDUPE KEY",
		Status: "failed", Attempt: 1, Retryable: true, RetryAt: &retryAt,
		ConfigSnapshotJSON: `{"local_time":"09:00","timezone":"UTC"}`,
		ActionSnapshotJSON: `{"private":"AUTOMATION PRIVATE SNAPSHOT"}`,
		ErrorCode:          stringPointer("ACTION_WRITE_FAILED"), ResultSummary: "AUTOMATION PRIVATE RESULT",
		StartedAt: "2026-09-19T12:02:00Z", EndedAt: "2026-09-19T12:02:00Z",
	}
	if err := fixture.Store.DB.Create(&automationFailure).Error; err != nil {
		t.Fatal(err)
	}
	oldFailure := automationFailure
	oldFailure.ID = uuid.NewString()
	oldFailure.LogicalKey = "old"
	oldFailure.DedupeKey = "old:1"
	oldFailure.RetryAt = nil
	oldScheduledFor := "2026-09-18T09:00:00Z"
	oldFailure.ScheduledFor = &oldScheduledFor
	oldFailure.StartedAt = "2026-09-19T11:00:00Z"
	oldFailure.EndedAt = oldFailure.StartedAt
	if err := fixture.Store.DB.Create(&oldFailure).Error; err != nil {
		t.Fatal(err)
	}
	completedRetry := oldFailure
	completedRetry.ID = uuid.NewString()
	completedRetry.Status = "succeeded"
	completedRetry.Attempt = 2
	completedRetry.Retryable = false
	completedRetry.RetryOfRunID = &oldFailure.ID
	completedRetry.DedupeKey = "old:2"
	completedRetry.ErrorCode = nil
	resultType := "inbox_item"
	resultID := uuid.NewString()
	completedRetry.ResultType = &resultType
	completedRetry.ResultID = &resultID
	completedRetry.StartedAt = "2026-09-19T11:01:00Z"
	completedRetry.EndedAt = completedRetry.StartedAt
	if err := fixture.Store.DB.Create(&completedRetry).Error; err != nil {
		t.Fatal(err)
	}
	knowledgeSource := models.KnowledgeSource{
		ID: uuid.NewString(), Name: "billing-guide.md", Title: "PRIVATE KNOWLEDGE TITLE", SourceType: "markdown",
		ImportMode: "managed_copy", MimeType: "text/markdown", SizeBytes: 29,
		ContentSHA256: strings.Repeat("a", 64), OriginalContent: []byte("PRIVATE KNOWLEDGE SOURCE BODY"),
		Status: "failed", Version: 1, CreatedAt: "2026-09-19T10:00:00Z", UpdatedAt: "2026-09-19T12:03:00Z",
	}
	if err := fixture.Store.DB.Create(&knowledgeSource).Error; err != nil {
		t.Fatal(err)
	}
	knowledgeFailureTime := "2026-09-19T12:03:00Z"
	knowledgeFailure := models.KnowledgeIndexJob{
		ID: uuid.NewString(), SourceID: knowledgeSource.ID, Operation: "import", Status: "failed", Stage: "complete",
		Progress: 40, Attempt: 1, ErrorCode: stringPointer("KNOWLEDGE_INDEX_INTERRUPTED"),
		StartedAt: &knowledgeFailureTime, CompletedAt: &knowledgeFailureTime, CreatedAt: knowledgeFailureTime,
	}
	if err := fixture.Store.DB.Create(&knowledgeFailure).Error; err != nil {
		t.Fatal(err)
	}
	resolvedSource := knowledgeSource
	resolvedSource.ID = uuid.NewString()
	resolvedSource.Name = "resolved.md"
	resolvedSource.Title = "Resolved"
	resolvedSource.Status = "ready"
	resolvedSource.CreatedAt = "2026-09-18T10:00:00Z"
	resolvedSource.UpdatedAt = "2026-09-18T11:01:00Z"
	if err := fixture.Store.DB.Create(&resolvedSource).Error; err != nil {
		t.Fatal(err)
	}
	resolvedFailureTime := "2026-09-18T11:00:00Z"
	resolvedFailure := knowledgeFailure
	resolvedFailure.ID = uuid.NewString()
	resolvedFailure.SourceID = resolvedSource.ID
	resolvedFailure.ErrorCode = stringPointer("KNOWLEDGE_INDEX_FAILED")
	resolvedFailure.StartedAt = &resolvedFailureTime
	resolvedFailure.CompletedAt = &resolvedFailureTime
	resolvedFailure.CreatedAt = resolvedFailureTime
	if err := fixture.Store.DB.Create(&resolvedFailure).Error; err != nil {
		t.Fatal(err)
	}
	resolvedSuccessTime := "2026-09-18T11:01:00Z"
	resolvedSuccess := resolvedFailure
	resolvedSuccess.ID = uuid.NewString()
	resolvedSuccess.Status = "succeeded"
	resolvedSuccess.Progress = 100
	resolvedSuccess.Attempt = 2
	resolvedSuccess.RetryOfJobID = &resolvedFailure.ID
	resolvedSuccess.ErrorCode = nil
	resolvedSuccess.StartedAt = &resolvedSuccessTime
	resolvedSuccess.CompletedAt = &resolvedSuccessTime
	resolvedSuccess.CreatedAt = resolvedSuccessTime
	if err := fixture.Store.DB.Create(&resolvedSuccess).Error; err != nil {
		t.Fatal(err)
	}
	failedSession := models.AISession{
		ID: uuid.NewString(), Title: "Provider recovery", Persist: true, Version: 1,
		CreatedAt: "2026-09-19T10:00:00Z", UpdatedAt: "2026-09-19T12:04:00Z",
	}
	if err := fixture.Store.DB.Create(&failedSession).Error; err != nil {
		t.Fatal(err)
	}
	privatePartial := "PRIVATE FAILED GENERATION BODY"
	generationFailure := models.AIGeneration{
		ID: uuid.NewString(), SessionID: failedSession.ID, ProviderID: fixture.Provider.ID,
		Status: "failed", ErrorCode: stringPointer("AI_PROVIDER_ERROR"), Content: &privatePartial,
		CreatedAt: "2026-09-19T12:04:00Z", UpdatedAt: "2026-09-19T12:04:00Z",
	}
	if err := fixture.Store.DB.Create(&generationFailure).Error; err != nil {
		t.Fatal(err)
	}
	interruptedSession := failedSession
	interruptedSession.ID = uuid.NewString()
	interruptedSession.Title = "Interrupted private session"
	interruptedSession.CreatedAt = "2026-09-19T12:05:00Z"
	interruptedSession.UpdatedAt = interruptedSession.CreatedAt
	if err := fixture.Store.DB.Create(&interruptedSession).Error; err != nil {
		t.Fatal(err)
	}
	interruptedPartial := "PRIVATE INTERRUPTED GENERATION BODY"
	interruptedGeneration := models.AIGeneration{
		ID: uuid.NewString(), SessionID: interruptedSession.ID, ProviderID: fixture.Provider.ID,
		Status: "cancelled", ErrorCode: stringPointer("AI_GENERATION_INTERRUPTED"), Content: &interruptedPartial,
		CreatedAt: interruptedSession.CreatedAt, UpdatedAt: interruptedSession.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&interruptedGeneration).Error; err != nil {
		t.Fatal(err)
	}
	userCancelledSession := failedSession
	userCancelledSession.ID = uuid.NewString()
	userCancelledSession.Title = "User cancelled session"
	userCancelledSession.CreatedAt = "2026-09-19T12:06:00Z"
	userCancelledSession.UpdatedAt = userCancelledSession.CreatedAt
	if err := fixture.Store.DB.Create(&userCancelledSession).Error; err != nil {
		t.Fatal(err)
	}
	userCancelledGeneration := models.AIGeneration{
		ID: uuid.NewString(), SessionID: userCancelledSession.ID, ProviderID: fixture.Provider.ID,
		Status: "cancelled", ErrorCode: stringPointer("AI_GENERATION_CANCELLED"),
		CreatedAt: userCancelledSession.CreatedAt, UpdatedAt: userCancelledSession.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&userCancelledGeneration).Error; err != nil {
		t.Fatal(err)
	}
	recoveredSession := failedSession
	recoveredSession.ID = uuid.NewString()
	recoveredSession.Title = "Recovered provider session"
	recoveredSession.CreatedAt = "2026-09-19T09:00:00Z"
	recoveredSession.UpdatedAt = "2026-09-19T09:02:00Z"
	if err := fixture.Store.DB.Create(&recoveredSession).Error; err != nil {
		t.Fatal(err)
	}
	recoveredFailure := generationFailure
	recoveredFailure.ID = uuid.NewString()
	recoveredFailure.SessionID = recoveredSession.ID
	recoveredFailure.Status = "cancelled"
	recoveredFailure.ErrorCode = stringPointer("AI_GENERATION_INTERRUPTED")
	recoveredFailure.Content = nil
	recoveredFailure.CreatedAt = "2026-09-19T09:00:00Z"
	recoveredFailure.UpdatedAt = "2026-09-19T09:01:00Z"
	if err := fixture.Store.DB.Create(&recoveredFailure).Error; err != nil {
		t.Fatal(err)
	}
	recoveredGeneration := recoveredFailure
	recoveredGeneration.ID = uuid.NewString()
	recoveredGeneration.Status = "completed"
	recoveredGeneration.ErrorCode = nil
	recoveredGeneration.CreatedAt = "2026-09-19T09:02:00Z"
	recoveredGeneration.UpdatedAt = "2026-09-19T09:02:00Z"
	if err := fixture.Store.DB.Create(&recoveredGeneration).Error; err != nil {
		t.Fatal(err)
	}
	temporarySession := failedSession
	temporarySession.ID = uuid.NewString()
	temporarySession.Title = "Temporary private session"
	temporarySession.Persist = false
	temporarySession.CreatedAt = "2026-09-19T12:05:00Z"
	temporarySession.UpdatedAt = temporarySession.CreatedAt
	if err := fixture.Store.DB.Create(&temporarySession).Error; err != nil {
		t.Fatal(err)
	}
	temporaryPartial := "PRIVATE TEMPORARY GENERATION BODY"
	temporaryFailure := generationFailure
	temporaryFailure.ID = uuid.NewString()
	temporaryFailure.SessionID = temporarySession.ID
	temporaryFailure.Content = &temporaryPartial
	temporaryFailure.CreatedAt = temporarySession.CreatedAt
	temporaryFailure.UpdatedAt = temporarySession.UpdatedAt
	if err := fixture.Store.DB.Create(&temporaryFailure).Error; err != nil {
		t.Fatal(err)
	}
	providerIssue := models.AIProvider{
		ID: uuid.NewString(), Name: "Local recovery model", Kind: "local", Protocol: "openai_chat",
		BaseURL: "http://127.0.0.1:11434/PRIVATE_PROVIDER_ENDPOINT", Model: "recovery-model",
		Status: "unavailable", HealthStatus: "unhealthy", HealthErrorCode: stringPointer("AI_ENDPOINT_UNREACHABLE"),
		LastHealthAt: stringPointer("2026-09-19T12:06:00Z"), Version: 2, ConfigVersion: 1,
		CreatedAt: "2026-09-19T12:05:00Z", UpdatedAt: "2026-09-19T12:06:00Z",
	}
	if err := fixture.Store.DB.Create(&providerIssue).Error; err != nil {
		t.Fatal(err)
	}
	nonActionableProvider := models.AIProvider{
		ID: uuid.NewString(), Name: "Transient provider drift", Kind: "local", Protocol: "openai_chat",
		BaseURL: "http://127.0.0.1:11435/PRIVATE_NON_ACTIONABLE_ENDPOINT", Model: "transient-model",
		Status: "unavailable", HealthStatus: "unknown", HealthErrorCode: nil,
		Version: 2, ConfigVersion: 1,
		CreatedAt: "2026-09-19T12:05:30Z", UpdatedAt: "2026-09-19T12:06:30Z",
	}
	if err := fixture.Store.DB.Create(&nonActionableProvider).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.Service.projectSystemMaintenanceFailureAt(
		storageLowSpaceMaintenanceIncident,
		"agent-inbox-maintenance-test",
		"2026-09-19T12:07:00Z",
		uuid.NewString(),
	); err != nil {
		t.Fatal(err)
	}
	var maintenanceItem models.InboxItem
	if err := fixture.Store.DB.First(
		&maintenanceItem,
		"source_entity_type = ? AND source_entity_id = ?",
		systemMaintenanceInboxSourceType,
		systemMaintenanceSourceID(storageLowSpaceMaintenanceIncident.component, storageLowSpaceMaintenanceIncident.operation),
	).Error; err != nil {
		t.Fatal(err)
	}
	unknownMaintenanceSourceID := "private:unknown"
	unknownMaintenance := models.InboxItem{
		ID: uuid.NewString(), Kind: "event", Title: "PRIVATE UNKNOWN MAINTENANCE TITLE", Summary: "PRIVATE UNKNOWN MAINTENANCE SUMMARY",
		SourceEntityType: systemMaintenanceInboxSourceType,
		SourceEntityID:   &unknownMaintenanceSourceID,
		Priority:         "P1", Status: "open", ResolutionPolicy: "manual",
		PayloadJSON: `{"private":"UNKNOWN MAINTENANCE PAYLOAD"}`,
		Version:     1, CreatedAt: "2026-09-19T12:07:30Z", UpdatedAt: "2026-09-19T12:07:30Z",
	}
	if err := fixture.Store.DB.Create(&unknownMaintenance).Error; err != nil {
		if !strings.Contains(err.Error(), "INVALID_SYSTEM_MAINTENANCE_INBOX_SOURCE") {
			t.Fatalf("unknown maintenance source error = %v", err)
		}
	} else {
		t.Fatal("unknown maintenance source was inserted")
	}

	read := func(path string) (int, []aiAgentInboxItem, aiAgentInboxMeta, string) {
		t.Helper()
		response := performRequest(fixture.Router, http.MethodGet, path, nil, nil)
		var envelope struct {
			Data []aiAgentInboxItem `json:"data"`
			Meta aiAgentInboxMeta   `json:"meta"`
		}
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("decode inbox: %s", response.Body.String())
		}
		return response.Code, envelope.Data, envelope.Meta, response.Body.String()
	}

	code, items, meta, body := read("/api/v1/ai/inbox?limit=50&offset=0")
	if code != http.StatusOK || len(items) != 8 || meta.Total != 8 || meta.ApprovalTotal != 1 || meta.ReviewTotal != 1 || meta.AutomationFailureTotal != 1 || meta.KnowledgeFailureTotal != 1 || meta.GenerationFailureTotal != 2 || meta.ProviderIssueTotal != 1 || meta.MaintenanceFailureTotal != 1 || meta.HasMore {
		t.Fatalf("inbox = %d %+v %+v", code, items, meta)
	}
	var approval, review, automation, knowledge, generation, interrupted, provider, maintenance *aiAgentInboxItem
	for index := range items {
		switch items[index].Kind {
		case "approval":
			approval = &items[index]
		case "review":
			review = &items[index]
		case "automation_failure":
			automation = &items[index]
		case "knowledge_failure":
			knowledge = &items[index]
		case "generation_failure":
			if items[index].ID == generationFailure.ID {
				generation = &items[index]
			} else if items[index].ID == interruptedGeneration.ID {
				interrupted = &items[index]
			}
		case "provider_issue":
			provider = &items[index]
		case "maintenance_failure":
			maintenance = &items[index]
		}
	}
	if approval == nil || approval.ID != unplanned.ID || approval.Action == nil || *approval.Action != "task.create" ||
		approval.SessionID == nil || *approval.SessionID != fixture.Generation.SessionID || approval.GenerationID == nil ||
		approval.TaskID != nil || review == nil || review.ID != submission.ID || review.TaskID == nil ||
		*review.TaskID != fixture.Task.ID || review.SubmissionID == nil || *review.SubmissionID != submission.ID ||
		review.Sequence == nil || *review.Sequence != 1 || review.SessionID != nil ||
		automation == nil || automation.ID != automationFailure.ID || automation.RuleID == nil || *automation.RuleID != automationRule.ID ||
		automation.RuleName == nil || *automation.RuleName != "每日查看今日任务" || automation.Attempt == nil || *automation.Attempt != 1 ||
		automation.Retryable == nil || !*automation.Retryable || automation.RetryAt == nil || *automation.RetryAt != retryAt ||
		automation.ErrorCode == nil || *automation.ErrorCode != "ACTION_WRITE_FAILED" ||
		knowledge == nil || knowledge.ID != knowledgeFailure.ID || knowledge.SourceID == nil || *knowledge.SourceID != knowledgeSource.ID ||
		knowledge.SourceName == nil || *knowledge.SourceName != knowledgeSource.Name || knowledge.Operation == nil || *knowledge.Operation != "import" ||
		knowledge.Attempt == nil || *knowledge.Attempt != 1 || knowledge.ErrorCode == nil || *knowledge.ErrorCode != "KNOWLEDGE_INDEX_INTERRUPTED" ||
		generation == nil || generation.ID != generationFailure.ID || generation.SessionID == nil || *generation.SessionID != failedSession.ID ||
		generation.SessionTitle == nil || *generation.SessionTitle != failedSession.Title || generation.GenerationID == nil || *generation.GenerationID != generationFailure.ID ||
		generation.ErrorCode == nil || *generation.ErrorCode != "AI_PROVIDER_ERROR" || generation.TaskID != nil || generation.SourceID != nil ||
		interrupted == nil || interrupted.ID != interruptedGeneration.ID || interrupted.SessionID == nil || *interrupted.SessionID != interruptedSession.ID ||
		interrupted.SessionTitle == nil || *interrupted.SessionTitle != interruptedSession.Title || interrupted.GenerationID == nil || *interrupted.GenerationID != interruptedGeneration.ID ||
		interrupted.ErrorCode == nil || *interrupted.ErrorCode != "AI_GENERATION_INTERRUPTED" ||
		provider == nil || provider.ID != providerIssue.ID || provider.ProviderID == nil || *provider.ProviderID != providerIssue.ID ||
		provider.ProviderName == nil || *provider.ProviderName != providerIssue.Name || provider.ProviderModel == nil || *provider.ProviderModel != providerIssue.Model ||
		provider.ProviderStatus == nil || *provider.ProviderStatus != "unavailable" || provider.HealthStatus == nil || *provider.HealthStatus != "unhealthy" ||
		provider.ErrorCode == nil || *provider.ErrorCode != "AI_ENDPOINT_UNREACHABLE" || provider.SessionID != nil || provider.TaskID != nil ||
		maintenance == nil || maintenance.ID != maintenanceItem.ID || maintenance.Component == nil || *maintenance.Component != "storage" ||
		maintenance.Operation == nil || *maintenance.Operation != "low_space" || maintenance.ErrorCode == nil || *maintenance.ErrorCode != "storage_low_space" ||
		maintenance.SessionID != nil || maintenance.TaskID != nil || maintenance.SourceID != nil {
		t.Fatalf("unexpected items: %+v", items)
	}
	for _, secret := range []string{
		"PLANNED PRIVATE TITLE", "PLANNED PRIVATE BODY", "UNPLANNED PRIVATE TITLE", "UNPLANNED PRIVATE BODY",
		"PRIVATE REVIEW SUMMARY", "changes", "preview", "fingerprint",
		"AUTOMATION PRIVATE SNAPSHOT", "AUTOMATION PRIVATE RESULT", "PRIVATE AUTOMATION LOGICAL KEY",
		"PRIVATE KNOWLEDGE TITLE", "PRIVATE KNOWLEDGE SOURCE BODY", "PRIVATE FAILED GENERATION BODY", "PRIVATE INTERRUPTED GENERATION BODY", "PRIVATE TEMPORARY GENERATION BODY",
		"PRIVATE_PROVIDER_ENDPOINT", "PRIVATE_NON_ACTIONABLE_ENDPOINT", nonActionableProvider.Name, nonActionableProvider.Model,
		storageLowSpaceMaintenanceIncident.message, unknownMaintenance.Title, unknownMaintenance.Summary, "UNKNOWN MAINTENANCE PAYLOAD",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("Agent Inbox leaked %q: %s", secret, body)
		}
	}

	code, items, meta, _ = read("/api/v1/ai/inbox?kind=approval&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != unplanned.ID || meta.Total != 1 || meta.ApprovalTotal != 1 || meta.ReviewTotal != 1 {
		t.Fatalf("approval filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=review&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != submission.ID || meta.Total != 1 {
		t.Fatalf("review filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=automation_failure&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != automationFailure.ID || meta.Total != 1 || meta.AutomationFailureTotal != 1 {
		t.Fatalf("automation filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=knowledge_failure&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != knowledgeFailure.ID || meta.Total != 1 || meta.KnowledgeFailureTotal != 1 {
		t.Fatalf("knowledge filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=generation_failure&limit=2&offset=0")
	if code != http.StatusOK || len(items) != 2 || items[0].ID != interruptedGeneration.ID || items[1].ID != generationFailure.ID || meta.Total != 2 || meta.GenerationFailureTotal != 2 {
		t.Fatalf("generation filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=provider_issue&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != providerIssue.ID || meta.Total != 1 || meta.ProviderIssueTotal != 1 {
		t.Fatalf("provider filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=maintenance_failure&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || items[0].ID != maintenanceItem.ID || meta.Total != 1 || meta.MaintenanceFailureTotal != 1 {
		t.Fatalf("maintenance filter = %d %+v %+v", code, items, meta)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || !meta.HasMore || meta.NextOffset == nil || *meta.NextOffset != 1 {
		t.Fatalf("pagination = %d %+v %+v", code, items, meta)
	}
	code, _, _, body = read("/api/v1/ai/inbox?kind=unknown")
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "AI_INBOX_KIND_INVALID") {
		t.Fatalf("invalid kind = %d %s", code, body)
	}
}

func TestAIAgentInboxOpenAccessRequestNavigatesWithoutExposingScopes(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	if err := fixture.Store.DB.Model(&models.AIGeneration{}).Where("id = ?", fixture.Generation.ID).
		Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	request := models.AIWorkspaceAccessRequest{
		GenerationID: fixture.Generation.ID, ScopesJSON: `["work","actions"]`, Status: "open",
		CreatedAt: fixture.Generation.CreatedAt, UpdatedAt: fixture.Generation.UpdatedAt,
	}
	if err := fixture.Store.DB.Create(&request).Error; err != nil {
		t.Fatal(err)
	}
	read := func(kind string) (aiAgentInboxItem, aiAgentInboxMeta, string) {
		t.Helper()
		response := performRequest(fixture.Router, http.MethodGet, "/api/v1/ai/inbox?kind="+kind, nil, nil)
		var envelope struct {
			Data []aiAgentInboxItem `json:"data"`
			Meta aiAgentInboxMeta   `json:"meta"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 1 {
			t.Fatalf("inbox %s = %d %s", kind, response.Code, response.Body.String())
		}
		return envelope.Data[0], envelope.Meta, response.Body.String()
	}
	item, meta, body := read("access_request")
	if item.ID != fixture.Generation.ID || item.Kind != "access_request" || item.SessionID == nil ||
		*item.SessionID != fixture.Generation.SessionID || item.GenerationID == nil ||
		*item.GenerationID != fixture.Generation.ID || item.SessionTitle == nil ||
		*item.SessionTitle != "Agent execution" || meta.Total != 1 || meta.AccessRequestTotal != 1 {
		t.Fatalf("access request = %+v %+v", item, meta)
	}
	for _, secret := range []string{"scopes", "actions", "PRIVATE", "provider_id", "grant"} {
		if strings.Contains(body, secret) {
			t.Fatalf("access request inbox leaked %q: %s", secret, body)
		}
	}
	_, allMeta, _ := read("all")
	if allMeta.Total != 1 || allMeta.AccessRequestTotal != 1 {
		t.Fatalf("all inbox = %+v", allMeta)
	}
	newer := models.AIGeneration{
		ID: uuid.NewString(), SessionID: fixture.Generation.SessionID, ProviderID: fixture.Provider.ID,
		Status: "completed", CreatedAt: "2026-09-21T12:01:00Z", UpdatedAt: "2026-09-21T12:01:00Z",
	}
	if err := fixture.Store.DB.Create(&newer).Error; err != nil {
		t.Fatal(err)
	}
	stillOpen := performRequest(fixture.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
	if stillOpen.Code != http.StatusOK || !strings.Contains(stillOpen.Body.String(), fixture.Generation.ID) || !strings.Contains(stillOpen.Body.String(), `"access_request_total":1`) {
		t.Fatalf("open request hidden by unrelated generation: %d %s", stillOpen.Code, stillOpen.Body.String())
	}
	dismiss := performRequest(fixture.Router, http.MethodPost, "/api/v1/ai/generations/"+fixture.Generation.ID+"/access-request/dismiss", nil, nil)
	if dismiss.Code != http.StatusNoContent {
		t.Fatalf("dismiss = %d %s", dismiss.Code, dismiss.Body.String())
	}
	closed := performRequest(fixture.Router, http.MethodGet, "/api/v1/ai/inbox?kind=access_request", nil, nil)
	if closed.Code != http.StatusOK || strings.Contains(closed.Body.String(), fixture.Generation.ID) || !strings.Contains(closed.Body.String(), `"access_request_total":0`) {
		t.Fatalf("closed request remains in inbox: %d %s", closed.Code, closed.Body.String())
	}
}

func TestAIAgentInboxIncludesActionableAgentRunsWithoutOutputOrRetryParents(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	var assignment models.TaskAssignment
	if err := fixture.Store.DB.First(&assignment, "task_id = ?", fixture.Task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fixture.Actor.AgentAdapterID == nil {
		t.Fatal("fixture actor has no adapter")
	}
	newRun := func(id, createdAt, status, delivery string, attempt int, parent *string) models.AgentRun {
		completedAt := createdAt
		startedAt := createdAt
		if status == "queued" {
			startedAt = ""
			completedAt = ""
		}
		var startedAtRef, completedAtRef *string
		if startedAt != "" {
			startedAtRef = &startedAt
		}
		if completedAt != "" {
			completedAtRef = &completedAt
		}
		run := models.AgentRun{
			ID: id, TaskID: fixture.Task.ID, AssignmentID: assignment.ID, ActorID: fixture.Actor.ID,
			AdapterID: *fixture.Actor.AgentAdapterID, CreatedByActorID: models.BuiltinOwnerActorID,
			ParentRunID: parent, Attempt: attempt, Status: status, ProviderID: fixture.Provider.ID,
			Model: fixture.Provider.Model, TaskVersion: fixture.Task.Version, AssignmentAssignedAt: assignment.AssignedAt,
			ActorVersion: fixture.Actor.Version, AdapterVersion: 1, ProviderVersion: fixture.Provider.Version,
			ProviderConfigVersion: fixture.Provider.ConfigVersion, ExecutionContractVersion: agentRunExecutionContractVersion,
			InputSnapshotJSON: `{"private":"RUN INPUT MUST NOT LEAK"}`, OutputDeliveryStatus: delivery,
			CreatedAt: createdAt, StartedAt: startedAtRef, CompletedAt: completedAtRef,
		}
		if status == "failed" {
			run.ErrorCode = stringPointer("AGENT_RUN_FAILED")
		}
		return run
	}
	visibleFailed := newRun("018f0000-0000-7000-8000-000000001001", "2026-09-19T12:10:00Z", "failed", agentRunOutputNotReady, 1, nil)
	visibleRetained := newRun("018f0000-0000-7000-8000-000000001002", "2026-09-19T12:09:00Z", "succeeded", agentRunOutputRetained, 2, nil)
	visibleRetained.ResultText = stringPointer("retained result")
	visibleRetained.ResultBytes = intPointer(len("retained result"))
	visibleRetained.OutputDeliveryErrorCode = stringPointer("AGENT_OUTPUT_DELIVERY_RETAINED")
	notAttention := newRun("018f0000-0000-7000-8000-000000001003", "2026-09-19T12:08:00Z", "cancelled", agentRunOutputNotReady, 3, nil)
	parent := newRun("018f0000-0000-7000-8000-000000001004", "2026-09-19T12:07:00Z", "failed", agentRunOutputNotReady, 4, nil)
	child := newRun("018f0000-0000-7000-8000-000000001005", "2026-09-19T12:06:00Z", "queued", agentRunOutputNotReady, 5, &parent.ID)
	for _, run := range []models.AgentRun{visibleFailed, visibleRetained, notAttention, parent, child} {
		if err := fixture.Store.DB.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) (int, []aiAgentInboxItem, aiAgentInboxMeta, string) {
		t.Helper()
		response := performRequest(fixture.Router, http.MethodGet, path, nil, nil)
		var envelope struct {
			Data []aiAgentInboxItem `json:"data"`
			Meta aiAgentInboxMeta   `json:"meta"`
		}
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
			t.Fatalf("decode inbox: %s", response.Body.String())
		}
		return response.Code, envelope.Data, envelope.Meta, response.Body.String()
	}
	code, items, meta, body := read("/api/v1/ai/inbox?kind=agent_run&limit=50&offset=0")
	if code != http.StatusOK || len(items) != 3 || meta.Total != 3 || meta.AgentRunTotal != 3 || meta.HasMore {
		t.Fatalf("agent run inbox = %d %+v %+v", code, items, meta)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Kind != "agent_run" || item.TaskID == nil || *item.TaskID != fixture.Task.ID || item.TaskTitle == nil ||
			*item.TaskTitle != fixture.Task.Title || item.Attempt == nil || item.RunStatus == nil || item.RunDelivery == nil ||
			item.Action != nil || item.SessionID != nil || item.SourceID != nil || item.ErrorCode != nil {
			t.Fatalf("unexpected agent run item: %+v", item)
		}
		seen[item.ID] = true
	}
	if !seen[visibleFailed.ID] || !seen[visibleRetained.ID] || !seen[child.ID] || seen[notAttention.ID] || seen[parent.ID] {
		t.Fatalf("agent run attention identities = %#v", seen)
	}
	if strings.Contains(body, "RUN INPUT MUST NOT LEAK") || strings.Contains(body, "AGENT_RUN_FAILED") {
		t.Fatalf("agent run inbox leaked execution details: %s", body)
	}
	code, items, meta, _ = read("/api/v1/ai/inbox?kind=agent_run&limit=1&offset=0")
	if code != http.StatusOK || len(items) != 1 || !meta.HasMore || meta.NextOffset == nil || *meta.NextOffset != 1 {
		t.Fatalf("agent run pagination = %d %+v %+v", code, items, meta)
	}
	code, _, _, body = read("/api/v1/ai/inbox?kind=unknown")
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "AI_INBOX_KIND_INVALID") {
		t.Fatalf("invalid kind = %d %s", code, body)
	}
}

func TestAIAgentInboxIncludesActivePlanContinuationsAsMetadataOnly(t *testing.T) {
	f, a, _ := newContinuationTestFixture(t)
	a.aiContinuations.close()
	now := "2026-09-19T12:00:00Z"
	expires := "2026-09-19T13:00:00Z"
	continuation := models.AIContinuation{
		ID: uuid.NewString(), SessionID: f.Generation.SessionID,
		ProviderID: f.Provider.ID, ProviderVersion: f.Provider.Version,
		ProviderConfigVersion: f.Provider.ConfigVersion, ProviderName: f.Provider.Name,
		ProviderKind: f.Provider.Kind, ProviderProtocol: f.Provider.Protocol,
		ProviderModel: f.Provider.Model, WorkspaceJSON: `{"provider_version":1,"scopes":["actions","work"]}`,
		InitialPlanVersion: 1, CurrentPlanVersion: 1, MaxTurns: 4, TurnsStarted: 1,
		Status: "waiting", Reason: "pending_approval", Version: 1,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: expires,
	}
	if err := f.Store.DB.Create(&continuation).Error; err != nil {
		t.Fatal(err)
	}
	response := performRequest(f.Router, http.MethodGet, "/api/v1/ai/inbox?kind=continuation&limit=50&offset=0", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("continuation inbox = %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []aiAgentInboxItem `json:"data"`
		Meta aiAgentInboxMeta   `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].Kind != "continuation" || envelope.Data[0].ID != continuation.ID ||
		envelope.Data[0].SessionID == nil || *envelope.Data[0].SessionID != f.Generation.SessionID ||
		envelope.Data[0].ContinuationStatus == nil || *envelope.Data[0].ContinuationStatus != "waiting" ||
		envelope.Data[0].ContinuationReason == nil || *envelope.Data[0].ContinuationReason != "pending_approval" ||
		envelope.Data[0].ContinuationMaxTurns == nil || *envelope.Data[0].ContinuationMaxTurns != 4 ||
		envelope.Data[0].ContinuationTurnsStarted == nil || *envelope.Data[0].ContinuationTurnsStarted != 1 ||
		envelope.Data[0].ContinuationExpiresAt == nil || *envelope.Data[0].ContinuationExpiresAt != expires ||
		envelope.Meta.Total != 1 || envelope.Meta.ContinuationTotal != 1 {
		t.Fatalf("continuation item = %+v meta=%+v", envelope.Data, envelope.Meta)
	}
	for _, secret := range []string{"workspace_json", "actions", "provider_id", f.Provider.BaseURL, "last_observation_hash"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("continuation inbox leaked %q: %s", secret, response.Body.String())
		}
	}
}

func TestAIAgentInboxProjectsEnabledAdapterIssuesOnly(t *testing.T) {
	fixture := newAIAgentRunFixture(t)
	stamp := "2026-09-19T12:10:00Z"
	attention := models.AgentAdapter{
		ID: uuid.NewString(), AdapterKey: "inbox-adapter-attention-v1", Kind: "builtin",
		DisplayName: "本地文本诊断执行器", ExecutableRef: "builtin:inbox-attention-v1",
		ManifestJSON: `{"private":"ADAPTER MANIFEST"}`, ProtocolVersion: agentAdapterProtocolVersion,
		Status: "disabled", HealthStatus: "blocked", HealthErrorCode: stringPointer("PLATFORM_ISOLATION_UNVERIFIED"),
		IsolationStatus: "unverified", ExecutionReady: false, LastHealthAt: &stamp,
		Version: 1, CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := fixture.Store.DB.Create(&attention).Error; err != nil {
		t.Fatal(err)
	}
	ready := attention
	ready.ID = uuid.NewString()
	ready.AdapterKey = "builtin-local-ready-v1"
	ready.ExecutableRef = "builtin:local-ready-v1"
	ready.DisplayName = "已就绪执行器"
	ready.HealthStatus = "healthy"
	ready.HealthErrorCode = nil
	ready.IsolationStatus = "verified"
	ready.ExecutionReady = true
	if err := fixture.Store.DB.Create(&ready).Error; err != nil {
		t.Fatal(err)
	}
	disabled := attention
	disabled.ID = uuid.NewString()
	disabled.AdapterKey = "inbox-adapter-disabled-v1"
	disabled.ExecutableRef = "builtin:inbox-disabled-v1"
	disabled.DisplayName = "已停用执行器"
	disabled.Status = "disabled"
	disabled.HealthStatus = "healthy"
	disabled.HealthErrorCode = nil
	disabled.IsolationStatus = "verified"
	disabled.ExecutionReady = true
	if err := fixture.Store.DB.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(fixture.Router, http.MethodGet, "/api/v1/ai/inbox?kind=adapter_issue&limit=50&offset=0", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("adapter inbox = %d %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data []aiAgentInboxItem `json:"data"`
		Meta aiAgentInboxMeta   `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Meta.Total != 1 || envelope.Meta.AdapterIssueTotal != 1 {
		t.Fatalf("adapter items = %+v meta=%+v", envelope.Data, envelope.Meta)
	}
	item := envelope.Data[0]
	if item.Kind != "adapter_issue" || item.ID != attention.ID || item.AdapterID == nil || *item.AdapterID != attention.ID ||
		item.AdapterName == nil || *item.AdapterName != attention.DisplayName || item.AdapterKey == nil || *item.AdapterKey != attention.AdapterKey ||
		item.AdapterStatus == nil || *item.AdapterStatus != "disabled" || item.HealthStatus == nil || *item.HealthStatus != "blocked" ||
		item.IsolationStatus == nil || *item.IsolationStatus != "unverified" || item.ExecutionReady == nil || *item.ExecutionReady ||
		item.ErrorCode == nil || *item.ErrorCode != "PLATFORM_ISOLATION_UNVERIFIED" ||
		item.SessionID != nil || item.TaskID != nil || item.ProviderID != nil {
		t.Fatalf("adapter item = %+v", item)
	}
	body := response.Body.String()
	for _, secret := range []string{"builtin:local-text-v1\"", "ADAPTER MANIFEST", ready.DisplayName, disabled.DisplayName} {
		if strings.Contains(body, secret) {
			t.Fatalf("adapter inbox leaked %q: %s", secret, body)
		}
	}
}
