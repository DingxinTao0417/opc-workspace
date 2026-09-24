package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opc-workspace/opc-sidecar/internal/agentexec"
	"github.com/opc-workspace/opc-sidecar/internal/models"
	"gorm.io/gorm"
)

// The model can only propose. There is deliberately no confirm/execute tool.
// The authenticated human endpoint binds consent to an immutable fingerprint.
type aiWorkspaceAction struct {
	AgentRunID          string          `json:"agent_run_id,omitempty"`
	ProjectNoteID       string          `json:"project_note_id,omitempty"`
	InvoiceID           string          `json:"invoice_id,omitempty"`
	FinancialEntryID    string          `json:"financial_entry_id,omitempty"`
	Action              string          `json:"action"`
	ProjectID           string          `json:"project_id,omitempty"`
	TaskID              string          `json:"task_id,omitempty"`
	TagID               string          `json:"tag_id,omitempty"`
	InboxItemID         string          `json:"inbox_item_id,omitempty"`
	ReminderID          string          `json:"reminder_id,omitempty"`
	FocusSessionID      string          `json:"focus_session_id,omitempty"`
	ClientFollowupID    string          `json:"client_followup_id,omitempty"`
	ClientActivityID    string          `json:"client_activity_id,omitempty"`
	AutomationRuleID    string          `json:"automation_rule_id,omitempty"`
	AutomationRunID     string          `json:"automation_run_id,omitempty"`
	RoadmapMilestoneID  string          `json:"roadmap_milestone_id,omitempty"`
	ContentItemID       string          `json:"content_item_id,omitempty"`
	KnowledgeSourceID   string          `json:"knowledge_source_id,omitempty"`
	KnowledgeIndexJobID string          `json:"knowledge_index_job_id,omitempty"`
	TaskSavedViewID     string          `json:"task_saved_view_id,omitempty"`
	ClientID            string          `json:"client_id,omitempty"`
	ClientActorLinkID   string          `json:"client_actor_link_id,omitempty"`
	ActorID             string          `json:"actor_id,omitempty"`
	ExpectedVersion     int64           `json:"expected_version,omitempty"`
	Changes             json.RawMessage `json:"changes"`
}

type aiActionPreview struct {
	AgentDelegation    *aiAgentDelegationPreview    `json:"agent_delegation,omitempty"`
	AgentFollowup      *aiAgentFollowupPreview      `json:"agent_followup,omitempty"`
	AgentRunRecovery   *aiAgentRunRecoveryPreview   `json:"agent_run_recovery,omitempty"`
	AgentRunRetry      *aiAgentRunRetryPreview      `json:"agent_run_retry,omitempty"`
	AgentRunCancel     *aiAgentRunCancelPreview     `json:"agent_run_cancel,omitempty"`
	AgentRunCancelMany *aiAgentRunCancelManyPreview `json:"agent_run_cancel_many,omitempty"`
	TaskOutput         *aiTaskOutputPreview         `json:"task_output,omitempty"`
	InboxReadAll       *aiInboxReadAllPreview       `json:"inbox_read_all,omitempty"`
	AgentRunStart      *aiAgentRunStartPreview      `json:"agent_run_start,omitempty"`
	Label              string                       `json:"label"`
	Before             map[string]any               `json:"before"`
	After              map[string]any               `json:"after"`
	Tasks              []aiInboxSplitDraft          `json:"tasks,omitempty"`
	NextFollowup       map[string]any               `json:"next_followup,omitempty"`
	AutomationRetry    *aiAutomationRetryPreview    `json:"automation_retry,omitempty"`
	Knowledge          *aiKnowledgeSourcePreview    `json:"knowledge,omitempty"`
	TaskView           *aiTaskViewPreview           `json:"task_view,omitempty"`
	TaskBatch          *aiTaskBatchPreview          `json:"task_batch,omitempty"`
	TaskBatchCreate    *aiTaskBatchCreatePreview    `json:"task_batch_create,omitempty"`
	TaskOrder          *aiTaskOrderPreview          `json:"task_order,omitempty"`
	RoadmapOrder       *aiRoadmapOrderPreview       `json:"roadmap_order,omitempty"`
}

type aiActionResponse struct {
	CreatedTaskIDs           []string                    `json:"created_task_ids,omitempty"`
	ID                       string                      `json:"id"`
	GenerationID             string                      `json:"generation_id"`
	Fingerprint              string                      `json:"fingerprint"`
	Action                   aiWorkspaceAction           `json:"action"`
	Preview                  aiActionPreview             `json:"preview"`
	Status                   string                      `json:"status"`
	CanConfirm               bool                        `json:"can_confirm"`
	ResultID                 *string                     `json:"result_id"`
	ResultVersion            *int64                      `json:"result_version"`
	Route                    string                      `json:"route"`
	CreatedAt                string                      `json:"created_at"`
	DecidedAt                *string                     `json:"decided_at"`
	AutomationRunResult      *aiAutomationRetryResult    `json:"automation_run_result,omitempty"`
	InvoicePDFResult         *aiInvoicePDFResult         `json:"invoice_pdf_result,omitempty"`
	InboxReadAllResult       *readAllInboxItemsOutput    `json:"inbox_read_all_result,omitempty"`
	AgentRunResult           *aiAgentRunResult           `json:"agent_run_result,omitempty"`
	AgentRunCancelManyResult *aiAgentRunCancelManyResult `json:"agent_run_cancel_many_result,omitempty"`
	AgentDelegationResult    *aiAgentDelegationResult    `json:"agent_delegation_result,omitempty"`
	AgentFollowupResult      *aiAgentDelegationResult    `json:"agent_followup_result,omitempty"`
	TaskBatchCreateResult    *aiTaskBatchCreateResult    `json:"task_batch_create_result,omitempty"`
}

var aiTaskChangeFields = map[string]bool{
	"review_policy": true,
	"title":         true, "description": true, "priority": true, "kind": true,
	"planned_date": true, "due_date": true, "estimated_minutes": true,
	"project_id": true, "parent_task_id": true, "completion_criteria": true, "tag_ids": true,
}

func aiWorkspaceActionSchema() json.RawMessage {
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action","changes"],"properties":{
"action":{"type":"string","enum":["task.create","task.update","task.start","task.block","task.unblock","task.complete","task.cancel","task.reopen","task.delete","project.create","project.update","project.start","project.pause","project.resume","project.complete","project.reopen","project.archive","project.restore","project.delete","inbox.create","inbox.update","inbox.read","inbox.read_all","inbox.snooze","inbox.unsnooze","inbox.resolve","inbox.dismiss","inbox.reopen","inbox.link_task","inbox.set_required","inbox.unlink_task"]},
"task_id":{"type":"string","format":"uuid","description":"Required for existing task operations; read current task first"},
"project_id":{"type":"string","format":"uuid","description":"Required for existing project operations; never for task actions"},
"inbox_item_id":{"type":"string","format":"uuid","description":"Required for existing Inbox operations, from workspace_get type inbox_item; never for other actions"},
"expected_version":{"type":"integer","minimum":1,"description":"Required except create actions and focus.start; exact version from workspace_get or workspace_focus"},
"changes":{"type":"object","additionalProperties":false,"description":"Task create: title required, review_policy optional. Task update review_policy: read current review_policy and review_policy_change_allowed first; none/manual only, actual change requires todo with no Submission history. Manual to none removes the future review gate; none to manual may request parent review for completed children, never acceptance or execution. Human confirmation rechecks facts. tag_ids is the complete set; parent_task_id is a real Task ID and null removes it on update. Task delete uses empty changes; consent is human-only. Project create: name required. Update: changed fields only. Project fields: name, description, start_date, due_date, color, client_id (requires clients scope). Inbox create/update: title, summary, priority, due_at; create requires title and only creates manual items. Inbox read_all requires the exact through_created_at snapshot_at returned by workspace_search for inbox_item; never invent a time. Inbox snooze requires future snoozed_until; resolve/dismiss require reason, reopen optionally accepts reason. All other lifecycle changes empty except task block/cancel require reason. Incomplete-task consent is human-only. Inbox link_task/set_required require changes.task_id, expected_task_version, is_required; unlink_task requires changes.task_id, expected_task_version and reason (2-1000 chars). Use workspace_inbox_tasks for real current relations. These do not complete/delete tasks; automatic Inbox resolution can occur. Inbox force_resolve requires reason (1-2000 chars) and all_required_tasks_done policy; proposes an exception only, with separate human consent. Never supply consent flags. No source/payload edits.","properties":{
"task_id":{"type":"string","format":"uuid","description":"Only Inbox relation changes: real Task ID"}, "expected_task_version":{"type":"integer","minimum":1,"description":"Only Inbox relation changes: current Task version"}, "is_required":{"type":"boolean","description":"Only Inbox link_task/set_required; explicit true or false"},
"summary":{"type":["string","null"],"maxLength":10000},"due_at":{"type":["string","null"],"description":"Inbox RFC3339 timestamp with explicit zone; null clears"},"through_created_at":{"type":"string","description":"Inbox read_all only: exact server snapshot_at from workspace_search type inbox_item"},"snoozed_until":{"type":"string","description":"Inbox future RFC3339 timestamp, later than server_now; changes visibility only, not a scheduled Reminder"},
"name":{"type":"string","minLength":1,"maxLength":100,"description":"Project names require 2-100 characters; Tag names require 1-50 characters. The selected action is validated before a proposal is stored."},"start_date":{"type":["string","null"],"description":"Project YYYY-MM-DD"},"color":{"type":["string","null"],"description":"Project or Tag #RRGGBB; Tag create/update does not accept null"},"client_id":{"type":["string","null"],"description":"Project customer UUID; requires clients scope"},
"title":{"type":"string","minLength":2,"maxLength":200},"description":{"type":"string","maxLength":10000},
"kind":{"type":"string","enum":["work","review","followup","reminder"]},"priority":{"type":"string","enum":["P0","P1","P2","P3"]},
"project_id":{"type":["string","null"]},"parent_task_id":{"type":["string","null"],"description":"Task create/update only. Real canonical Task UUID from workspace_search/get; null removes the parent on update."},"planned_date":{"type":["string","null"],"description":"YYYY-MM-DD, or null to unschedule"},
"due_date":{"type":["string","null"],"description":"Task: RFC3339 timestamp with explicit time zone. Project: YYYY-MM-DD. Null clears."},
"estimated_minutes":{"type":["integer","null"],"minimum":0},"completion_criteria":{"type":"string","maxLength":10000},
"tag_ids":{"type":"array","maxItems":20,"uniqueItems":true,"items":{"type":"string","format":"uuid"},"description":"Task create/update only. Complete replacement set; [] removes all tags. Resolve IDs with workspace_task_options type tag."},
"review_policy":{"type":"string","enum":["none","manual"]},"reason":{"type":"string","minLength":1,"maxLength":2000,"description":"Task block/cancel: 2-1000 chars. Inbox resolve/dismiss: 1-2000 chars; reopen optional."}
}}}}`)
	var root map[string]any
	_ = json.Unmarshal(schema, &root)
	properties := root["properties"].(map[string]any)
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), "focus.start", "focus.pause", "focus.resume", "focus.stop", "focus.cancel", "focus.recover")
	properties["focus_session_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Required for Focus commands except start: actual Session ID and expected_version from workspace_focus. Resume is NOT crash recovery."}
	action["enum"] = append(action["enum"].([]any), "inbox.split", "inbox.force_resolve")
	action["enum"] = append(action["enum"].([]any), "reminder.create", "reminder.update", "reminder.cancel")
	properties["reminder_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Reminder update/cancel target from workspace_get; expected_version required"}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Focus start: changes.task_id (UUID or explicit null), planned_seconds (300-7200) and expected_task_version when bound. No top-level Session ID/version. Check workspace_focus active first; cannot replace an open session. Starts a new local cycle only after separate human consent. Focus pause/resume/stop/cancel require empty changes. Stop credits actual time at confirmation, never completes a Task; cancel keeps audit but credits no work time. Focus recover requires recovery_action: include_gap_resume, exclude_gap_resume, or interrupt. Ask the user how to handle the unknown gap; never choose inclusion by assumption. Inclusion requires separate human consent. Never supply consent flags."
	focusProperties := changes["properties"].(map[string]any)
	focusProperties["task_id"] = map[string]any{"type": []string{"string", "null"}, "description": "Focus start: actual Task ID or explicit null. Inbox relations require an actual UUID, never null."}
	focusProperties["expected_task_version"] = map[string]any{"type": "integer", "minimum": 1, "description": "Required for bound Focus start and Inbox task relations; read current Task version."}
	focusProperties["planned_seconds"] = map[string]any{"type": "integer", "minimum": 300, "maximum": 7200}
	focusProperties["recovery_action"] = map[string]any{"type": "string", "enum": []string{"include_gap_resume", "exclude_gap_resume", "interrupt"}}
	changes["description"] = changes["description"].(string) + " Inbox split: tasks (1-20 drafts) and optional resolution_policy only; creates tasks, hierarchy, assignments and Inbox relations atomically after human confirmation. No Agent execution or external messages."
	for key, raw := range aiInboxSplitSchemaFields() {
		var value any
		_ = json.Unmarshal(raw, &value)
		changes["properties"].(map[string]any)[key] = value
	}
	changes["description"] = changes["description"].(string) + " Reminder create requires title/trigger_at, update requires at least one editable field, cancel requires reason (1-1000 chars). Reminder fields are title/summary/priority/trigger_at/recurrence_type/recurrence_interval/recurrence_timezone; no nulls, no source/actor/series/anchor overrides. Only scheduled occurrences may change. Cancel stops future recurrence, never deletes history."
	reminderProperties := changes["properties"].(map[string]any)
	reminderProperties["trigger_at"] = map[string]any{"type": "string", "description": "Future RFC3339 with explicit zone; later than server_now. Ask user if timezone/time is ambiguous."}
	reminderProperties["recurrence_type"] = map[string]any{"type": "string", "enum": []string{"none", "daily", "weekly", "weekdays", "monthly"}}
	reminderProperties["recurrence_interval"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 365}
	reminderProperties["recurrence_timezone"] = map[string]any{"type": "string", "description": "IANA timezone for recurring reminders; none requires interval=1/timezone=UTC. Weekdays means Mon-Fri, not holidays."}
	addAIClientFollowupSchema(properties)
	addAIClientActivitySchema(properties)
	addAIProjectNoteSchema(properties)
	addAIAutomationSchema(properties)
	addAIFinancialActionSchema(properties)
	addAIRoadmapMilestoneSchema(properties)
	addAIContentItemSchema(properties)
	addAIClientSchema(properties)
	addAIClientContactSchema(properties)
	addAIPersonSchema(properties)
	addAIInvoiceActionSchema(properties)
	addAIFinanceExportSchema(properties)
	addAIAssignmentSchema(properties)
	addAITaskOutputSchema(properties)
	addAIAgentRunSchema(properties)
	addAITagSchema(properties)
	addAIKnowledgeSourceSchema(properties)
	addAITaskViewSchema(properties)
	addAITaskBatchSchema(properties)
	addAITaskBatchCreateSchema(properties)
	addAITaskOrderSchema(properties)
	encoded, _ := json.Marshal(root)
	return encoded
}

// A batch update must stay byte-cheap: it reuses the per-task change fields
// (project_id/planned_date/tag_ids/reason) and adds only the action plus the
// aligned task set, because the model-facing schema shares one 64 KiB budget
// with user text and approval receipts.
func addAITaskBatchSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), aiTaskBatchUpdateAction)
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Task batch (work+actions): task.batch_update applies one change to 1-20 distinct real Tasks in one card; changes.items and changes.expected_versions align in order. batch_action supports set_priority | set_due_date | set_project | set_planned_date | add_tags | remove_tags | start | block | unblock | complete | cancel | reopen | set_assignee | clear_assignee | set_reviewer | clear_reviewer. Priority is P0-P3; due_date is an RFC3339 instant with offset or explicit null to clear. Project/date/tag/lifecycle fields follow their normal rules. Setting responsibility needs a real eligible actor_id and reason; clearing needs reason and an active Assignment on every Task. First read current versions/responsibility with workspace_task_assignments(task_ids) and target Actor with workspace_task_options. Same-Actor or already-empty targets are rejected. Approval freezes each Actor identity and uses native Assignment commands in one transaction; nothing is partially applied. Assignment neither starts Agent execution nor contacts a person."
	changeProperties := changes["properties"].(map[string]any)
	changeProperties["batch_action"] = map[string]any{
		"type": "string",
		"enum": []string{"set_priority", "set_due_date", "set_project", "set_planned_date", "add_tags", "remove_tags", "start", "block", "unblock", "complete", "cancel", "reopen", "set_assignee", "clear_assignee", "set_reviewer", "clear_reviewer"},
	}
	changeProperties["items"] = map[string]any{
		"type": "array", "maxItems": 20, "uniqueItems": true,
		"items": map[string]any{"type": "string", "format": "uuid"},
	}
	changeProperties["expected_versions"] = map[string]any{
		"type": "array", "maxItems": 20, "items": map[string]any{"type": "integer", "minimum": 1},
	}
	changeProperties["actor_id"] = map[string]any{"type": "string", "format": "uuid", "description": "Required only when setting batch responsibility; real eligible Actor from workspace_task_options"}
}

// Task saved views are local filter presets; the model may propose creating,
// renaming, retargeting or deleting them, but never apply one silently.
func addAITaskViewSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any), aiTaskViewCreateAction, aiTaskViewUpdateAction, aiTaskViewDeleteAction)
	properties["task_saved_view_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Existing view for task_view.update/delete, from workspace_task_views.",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Task views (work+actions): create needs name+definition; update needs id, expected_version and name or definition; delete needs id, expected_version, empty changes and separate human consent. Max 20 views, unique names. Views are local Tasks-page presets that never change or apply to tasks; read the current version first."
	changeProperties := changes["properties"].(map[string]any)
	changeProperties["definition"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"description": "Complete task filter preset; the server validates every key. Allowed keys: q, status (active/todo/in_progress/blocked/waiting_review/done/cancelled), priority (P0-P3), kind (work/review/followup/reminder), project_id (uuid), client_id (uuid), tag_ids (<=20 uuid), planned_date, planned_from, planned_to, due_from, due_to (YYYY-MM-DD) and sort (comma-separated manual_order/priority/due_date/planned_date/created_at/updated_at/title/status/kind, '-' prefix = descending). Empty means no filter; planned_date excludes planned_from/planned_to.",
	}
}

// Knowledge library management is proposed and confirmed like every other
// domain command; the model never receives document text for these actions.
func addAIKnowledgeSourceSchema(properties map[string]any) {
	action := properties["action"].(map[string]any)
	action["enum"] = append(action["enum"].([]any),
		aiKnowledgeCreateAction, aiKnowledgeReindexAction, aiKnowledgeDeleteAction, aiKnowledgeJobRetryAction, aiKnowledgeJobCancelAct)
	properties["knowledge_source_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Knowledge source target for knowledge_source.reindex/delete, read from knowledge_library. Never for index job actions.",
	}
	properties["knowledge_index_job_id"] = map[string]any{
		"type": "string", "format": "uuid",
		"description": "Knowledge index job target for knowledge_index_job.retry/cancel, read from knowledge_library. Never for source actions.",
	}
	changes := properties["changes"].(map[string]any)
	changes["description"] = changes["description"].(string) + " Knowledge (knowledge_actions, saved conversation only): create stores user-supplied text as a managed source (changes.name ends in .txt/.md/.markdown, changes.content is complete UTF-8 text <=24000 bytes, changes.title optional; never file bytes or paths); reindex rebuilds the index; delete removes source+index+managed copy after separate human consent; job retry re-attempts the newest failed/cancelled job; job cancel stops a queued/running job. Existing-source actions need expected_version (source version, or the job's current source version) and take no content. Only deletion accepts changes.reason (<=500 chars). Metadata only: library text is never returned, and reading it needs the separate knowledge scope."
	changeProperties := changes["properties"].(map[string]any)
	changeProperties["content"] = map[string]any{
		"type": "string", "minLength": 1, "maxLength": 24000,
		"description": "knowledge_source.create only: the complete UTF-8 text the user supplied here. Never invent or rewrite it.",
	}
}

func parseAIWorkspaceAction(arguments json.RawMessage) (aiWorkspaceAction, error) {
	var input aiWorkspaceAction
	if len(arguments) > 64<<10 {
		return input, errors.New("action is too large")
	}
	if err := decodeStrictToolArguments(arguments, &input); err != nil {
		return input, err
	}
	// A manual activity may contain 10,000 Unicode characters. This exception
	// does not loosen other domains or the 24 KiB model-facing result budget.
	if !strings.HasPrefix(input.Action, "project_note.") && !isAITaskOutputAction(input.Action) && input.Action != aiTaskBatchCreateAction && !strings.HasPrefix(input.Action, "client_activity.") && !strings.HasPrefix(input.Action, "financial_entry.") && !strings.HasPrefix(input.Action, "invoice.") && input.Action != aiKnowledgeCreateAction && len(arguments) > 24<<10 {
		return input, errors.New("action is too large")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(input.Changes, &fields) != nil || fields == nil {
		return input, errors.New("changes must be an object")
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(arguments, &raw)
	if input.Action == aiAgentDelegateSpawnAction {
		return parseAIAgentDelegationAction(input, fields, raw)
	}
	if input.Action == aiAgentDelegateFollowupAction {
		return parseAIAgentFollowupAction(input, fields, raw)
	}
	if _, present := raw["client_id"]; present && !strings.HasPrefix(input.Action, "client.") && !strings.HasPrefix(input.Action, "client_contact.") {
		return input, errors.New("only Client actions accept client_id as a top-level target")
	}
	if input.Action == aiAgentRunCancelManyAction {
		return parseAIAgentRunCancelMany(input, fields, raw)
	}
	if input.Action == "agent_run.cancel" || input.Action == "agent_run.retry" || input.Action == aiAgentRunRecoverOutput {
		return parseAIAgentRunControl(input, fields, raw)
	}
	if _, present := raw["agent_run_id"]; present {
		return input, errors.New("only Agent execution control accepts agent_run_id")
	}
	if strings.HasPrefix(input.Action, "project_note.") {
		return parseAIProjectNoteAction(input, fields, raw)
	}
	if _, present := raw["project_note_id"]; present {
		return input, errors.New("only note actions accept project_note_id")
	}
	if isAITaskOutputAction(input.Action) {
		return parseAITaskOutputAction(input, fields, raw)
	}
	if isAIAgentRunAction(input.Action) {
		return parseAIAgentRunAction(input, fields, raw)
	}
	if isAIAssignmentAction(input.Action) {
		return parseAIAssignmentAction(input, fields, raw)
	}
	if input.Action == "finance.export_csv" {
		return parseAIFinanceExport(input, fields, raw)
	}
	if strings.HasPrefix(input.Action, "invoice.") {
		return parseAIInvoiceAction(input, fields, raw)
	}
	if _, present := raw["invoice_id"]; present {
		return input, errors.New("only invoice actions accept invoice_id")
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		return parseAIFinancialAction(input, fields, raw)
	}
	if _, present := raw["financial_entry_id"]; present {
		return input, errors.New("only financial entry actions accept financial_entry_id")
	}
	if strings.HasPrefix(input.Action, "client.") {
		return parseAIClientAction(input, fields, raw)
	}
	if strings.HasPrefix(input.Action, "client_contact.") {
		return parseAIClientContactAction(input, fields, raw)
	}
	if _, present := raw["client_actor_link_id"]; present {
		return input, errors.New("only Client contact actions accept client_actor_link_id")
	}
	if isAIPersonAction(input.Action) {
		return parseAIPersonAction(input, fields, raw)
	}
	if _, present := raw["actor_id"]; present {
		return input, errors.New("only person actions accept actor_id as a top-level target")
	}
	if strings.HasPrefix(input.Action, "tag.") {
		return parseAITagAction(input, fields, raw)
	}
	if input.Action == aiTaskBatchUpdateAction {
		return parseAITaskBatchAction(input, fields, raw)
	}
	if input.Action == aiTaskBatchCreateAction {
		return parseAITaskBatchCreateAction(input, arguments)
	}
	if input.Action == aiTaskMoveAction {
		return parseAITaskMoveAction(input, fields, raw)
	}
	if _, present := raw["tag_id"]; present {
		return input, errors.New("only Tag actions accept tag_id")
	}
	if strings.HasPrefix(input.Action, "automation.") {
		return parseAIAutomationAction(input, fields, raw)
	}
	if isAIKnowledgeAction(input.Action) {
		return parseAIKnowledgeSourceAction(input, fields, raw)
	}
	if isAITaskViewAction(input.Action) {
		return parseAITaskViewAction(input, fields, raw)
	}
	if _, present := raw["task_saved_view_id"]; present {
		return input, errors.New("only task view actions accept task_saved_view_id")
	}
	if _, present := raw["knowledge_source_id"]; present {
		return input, errors.New("only knowledge source actions accept knowledge_source_id")
	}
	if _, present := raw["knowledge_index_job_id"]; present {
		return input, errors.New("only knowledge index job actions accept knowledge_index_job_id")
	}
	if _, present := raw["automation_rule_id"]; present {
		return input, errors.New("only automation actions accept automation_rule_id")
	}
	if _, present := raw["automation_run_id"]; present {
		return input, errors.New("only automation retry accepts automation_run_id")
	}
	if strings.HasPrefix(input.Action, "client_activity.") {
		return parseAIClientActivityAction(input, fields, raw)
	}
	if _, present := raw["client_activity_id"]; present {
		return input, errors.New("only activity actions accept client_activity_id")
	}
	if strings.HasPrefix(input.Action, "client_followup.") {
		return parseAIClientFollowupAction(input, fields, raw)
	}
	if _, present := raw["client_followup_id"]; present {
		return input, errors.New("only followup actions accept client_followup_id")
	}
	if strings.HasPrefix(input.Action, "focus.") {
		if input.Action == "focus.start" {
			for _, key := range []string{"focus_session_id", "expected_version"} {
				if _, present := raw[key]; present {
					return input, errors.New("Focus start does not accept a Session target/version")
				}
			}
		}
		for _, key := range []string{"task_id", "project_id", "inbox_item_id", "reminder_id"} {
			if _, present := raw[key]; present {
				return input, errors.New("Focus actions only accept focus_session_id as target")
			}
		}
		return parseAIFocusAction(input, fields)
	}
	if _, present := raw["focus_session_id"]; present {
		return input, errors.New("only Focus actions accept focus_session_id")
	}
	if strings.HasPrefix(input.Action, "reminder.") {
		return parseAIReminderAction(input, fields)
	}
	if input.ReminderID != "" {
		return input, errors.New("only Reminder actions accept reminder_id")
	}
	if strings.HasPrefix(input.Action, "inbox.") {
		return parseAIInboxAction(input, fields)
	}
	if input.InboxItemID != "" {
		return input, errors.New("only Inbox actions accept inbox_item_id")
	}
	if strings.HasPrefix(input.Action, "roadmap_milestone.") {
		return parseAIRoadmapMilestoneAction(input, fields, raw)
	}
	if _, present := raw["roadmap_milestone_id"]; present {
		return input, errors.New("only roadmap milestone actions accept roadmap_milestone_id")
	}
	if strings.HasPrefix(input.Action, "content_item.") {
		return parseAIContentItemAction(input, fields, raw)
	}
	if _, present := raw["content_item_id"]; present {
		return input, errors.New("only content item actions accept content_item_id")
	}
	if strings.HasPrefix(input.Action, "project.") {
		return parseAIProjectAction(input, fields)
	}
	if input.ProjectID != "" {
		return input, errors.New("task actions do not accept a top-level project_id; use changes.project_id")
	}
	if input.Action == "task.create" {
		if input.TaskID != "" || input.ExpectedVersion != 0 {
			return input, errors.New("create does not accept a task ID/version")
		}
	} else if _, err := uuid.Parse(input.TaskID); err != nil || input.ExpectedVersion < 1 {
		return input, errors.New("use a real task ID and expected_version from workspace_get")
	}
	for key := range fields {
		if input.Action == "task.create" || input.Action == "task.update" {
			if !aiTaskChangeFields[key] {
				return input, errors.New("unsupported task field: " + key)
			}
		} else if key != "reason" {
			return input, errors.New("lifecycle actions accept only reason when blocking/cancelling")
		}
	}
	switch input.Action {
	case "task.create":
		var create createTaskRequest
		if err := decodeStrictToolArguments(input.Changes, &create); err != nil {
			return input, err
		}
		if _, err := taskFromCreateRequest(create); err != nil {
			return input, err
		}
		tagIDs, err := validateAITaskTagIDs(create.TagIDs)
		if err != nil {
			return input, err
		}
		if _, present := fields["tag_ids"]; present {
			fields["tag_ids"], _ = json.Marshal(tagIDs)
		}
		if _, present := fields["parent_task_id"]; present {
			parentID, err := validateAITaskParentID(create.ParentTaskID)
			if err != nil {
				return input, err
			}
			fields["parent_task_id"], _ = json.Marshal(parentID)
		}
	case "task.update":
		var update updateTaskRequest
		if err := decodeStrictToolArguments(input.Changes, &update); err != nil {
			return input, err
		}
		if _, present := fields["review_policy"]; present && update.ReviewPolicy == nil {
			return input, errors.New("review_policy must be none or manual, not null")
		}
		if update.ReviewPolicy != nil {
			for _, field := range []string{"title", "description", "kind", "priority", "completion_criteria"} {
				if raw, present := fields[field]; present && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return input, errors.New("task policy changes require explicit non-null " + field)
				}
			}
		}
		_, tagIDs, err := validateTaskUpdate(update)
		if err != nil {
			return input, err
		}
		if update.ReviewPolicy != nil {
			fields["review_policy"], _ = json.Marshal(strings.TrimSpace(*update.ReviewPolicy))
		}
		if update.TagIDs.Set {
			tagIDs, err = validateAITaskTagIDs(tagIDs)
			if err != nil {
				return input, err
			}
			fields["tag_ids"], _ = json.Marshal(tagIDs)
		}
		if update.ParentTaskID.Set {
			parentID, err := validateAITaskParentID(update.ParentTaskID.Value)
			if err != nil {
				return input, err
			}
			fields["parent_task_id"], _ = json.Marshal(parentID)
		}
	case "task.start", "task.block", "task.unblock", "task.complete", "task.cancel", "task.reopen":
		_, err := aiActionReason(input)
		if err != nil {
			return input, err
		}
	case "task.delete":
		if len(fields) != 0 {
			return input, errors.New("task deletion requires empty changes; permanent-deletion consent belongs to the human decision")
		}
	default:
		return input, errors.New("unsupported workspace action")
	}
	// Canonical map encoding makes retries independent of key order/whitespace.
	input.Changes, _ = json.Marshal(fields)
	return input, nil
}

func aiActionReason(input aiWorkspaceAction) (string, error) {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(input.Changes, &fields)
	if input.Action != "task.block" && input.Action != "task.cancel" {
		if len(fields) != 0 {
			return "", errors.New("this lifecycle action requires empty changes")
		}
		return "", nil
	}
	var reason string
	if json.Unmarshal(fields["reason"], &reason) != nil {
		return "", errors.New("reason is required")
	}
	return validateAssignmentReason(reason)
}

func aiTaskActionFields(task models.Task) map[string]any {
	return map[string]any{
		"title": task.Title, "description": task.Description, "priority": task.Priority, "kind": task.Kind,
		"planned_date": task.PlannedDate, "due_date": task.DueDate, "estimated_minutes": task.EstimatedMinutes,
		"project_id": task.ProjectID, "completion_criteria": task.CompletionCriteria,
		"review_policy": task.ReviewPolicy, "status": task.Status,
	}
}

func aiTaskTagValues(tags []models.Tag) ([]string, []string) {
	ids := make([]string, 0, len(tags))
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		ids = append(ids, tag.ID)
		names = append(names, tag.Name)
	}
	return ids, names
}

func validateAITaskTagIDs(values []string) ([]string, error) {
	ids, err := validateTaskTagIDs(values)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return nil, errors.New("tag_ids must use canonical UUIDs from workspace_task_options")
		}
	}
	return ids, nil
}

func validateAITaskParentID(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	id := strings.TrimSpace(*value)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return nil, errors.New("parent_task_id must use a canonical UUID from workspace_search/get")
	}
	return &id, nil
}

func aiTaskParentTitle(tx *gorm.DB, taskID, parentID string) (*string, error) {
	if err := requireValidTaskParent(tx, taskID, parentID); err != nil {
		return nil, err
	}
	var parent models.Task
	if err := tx.Select("id", "title").First(&parent, "id = ?", parentID).Error; err != nil {
		return nil, err
	}
	title := parent.Title
	return &title, nil
}

func loadAITaskTags(tx *gorm.DB, tagIDs []string) ([]models.Tag, error) {
	if len(tagIDs) == 0 {
		return []models.Tag{}, nil
	}
	var tags []models.Tag
	if err := tx.Where("id IN ?", tagIDs).Order("LOWER(name) ASC").Order("id ASC").Find(&tags).Error; err != nil {
		return nil, err
	}
	if len(tags) != len(tagIDs) {
		return nil, newProjectRequestError(422, "TAG_NOT_FOUND", "tag_ids contains a tag that does not exist")
	}
	for index := range tags {
		normalizeTag(&tags[index])
	}
	return tags, nil
}

// Preview validates the same domain rules without executing changes. Confirmation
// repeats all validation inside the atomic domain-command/decision transaction.
func previewAIWorkspaceAction(tx *gorm.DB, input aiWorkspaceAction, now time.Time) (aiActionPreview, error) {
	if input.Action == aiAgentDelegateSpawnAction {
		return aiActionPreview{}, errors.New("Agent delegation preview requires its frozen parent grant")
	}
	if input.Action == aiAgentDelegateFollowupAction {
		return aiActionPreview{}, errors.New("Agent follow-up preview requires its frozen parent grant")
	}
	if input.Action == aiAgentRunRecoverOutput {
		return previewAIAgentRunRecovery(tx, input)
	}
	if input.Action == "agent_run.retry" {
		return previewAIAgentRunRetry(tx, input, nil)
	}
	if input.Action == aiAgentRunCancelManyAction {
		return previewAIAgentRunCancelMany(tx, input)
	}
	if input.Action == "agent_run.cancel" {
		return previewAIAgentRunCancel(tx, input)
	}
	if isAIAgentRunAction(input.Action) {
		return previewAIAgentRunAction(tx, input)
	}
	if isAITaskOutputAction(input.Action) {
		return previewAITaskOutputAction(tx, input)
	}
	if isAIAssignmentAction(input.Action) {
		return previewAIAssignmentAction(tx, input)
	}
	if input.Action == "finance.export_csv" {
		p, _, err := previewAIFinanceExport(tx, input)
		return p, err
	}
	if strings.HasPrefix(input.Action, "invoice.") {
		return previewAIInvoiceAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "financial_entry.") {
		return previewAIFinancialAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "automation.") {
		return previewAIAutomationAction(tx, input)
	}
	if isAIKnowledgeAction(input.Action) {
		return previewAIKnowledgeSourceAction(tx, input, now)
	}
	if isAITaskViewAction(input.Action) {
		return previewAITaskViewAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "project_note.") {
		return previewAIProjectNoteAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_activity.") {
		return previewAIClientActivityAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "client_followup.") {
		return previewAIClientFollowupAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "focus.") {
		return previewAIFocusAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "reminder.") {
		return previewAIReminderAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "inbox.") {
		return previewAIInboxAction(tx, input, now)
	}
	if strings.HasPrefix(input.Action, "roadmap_milestone.") {
		if input.Action == aiRoadmapMilestoneMoveAction {
			return previewAIRoadmapMilestoneMove(tx, input)
		}
		return previewAIRoadmapMilestoneAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "content_item.") {
		return previewAIContentItemAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "client.") {
		return previewAIClientAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "client_contact.") {
		return previewAIClientContactAction(tx, input)
	}
	if isAIPersonAction(input.Action) {
		return previewAIPersonAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "tag.") {
		return previewAITagAction(tx, input)
	}
	if strings.HasPrefix(input.Action, "project.") {
		return previewAIProjectAction(tx, input)
	}
	if input.Action == aiTaskBatchUpdateAction {
		return previewAITaskBatchAction(tx, input)
	}
	if input.Action == aiTaskBatchCreateAction {
		return previewAITaskBatchCreateAction(tx, input)
	}
	if input.Action == aiTaskMoveAction {
		return previewAITaskMoveAction(tx, input)
	}
	preview := aiActionPreview{Before: map[string]any{}, After: map[string]any{}}
	if input.Action == "task.create" {
		var create createTaskRequest
		_ = json.Unmarshal(input.Changes, &create)
		task, err := taskFromCreateRequest(create)
		if err != nil {
			return preview, err
		}
		if task.ProjectID != nil {
			if err := requireAssignableProject(tx, *task.ProjectID); err != nil {
				return preview, err
			}
		}
		preview.Label, preview.After = task.Title, aiTaskActionFields(task)
		var changeFields map[string]json.RawMessage
		_ = json.Unmarshal(input.Changes, &changeFields)
		if _, present := changeFields["tag_ids"]; present {
			tagIDs, err := validateAITaskTagIDs(create.TagIDs)
			if err != nil {
				return preview, err
			}
			tags, err := loadAITaskTags(tx, tagIDs)
			if err != nil {
				return preview, err
			}
			_, tagNames := aiTaskTagValues(tags)
			preview.After["tag_names"] = tagNames
		}
		if _, present := changeFields["parent_task_id"]; present {
			parentID, err := validateAITaskParentID(task.ParentTaskID)
			if err != nil {
				return preview, err
			}
			if parentID == nil {
				preview.After["parent_task_title"] = nil
			} else {
				parentTitle, err := aiTaskParentTitle(tx, task.ID, *parentID)
				if err != nil {
					return preview, err
				}
				preview.After["parent_task_title"] = parentTitle
			}
		}
		return preview, nil
	}
	task, err := loadTask(tx, input.TaskID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return preview, newProjectRequestError(404, "TASK_NOT_FOUND", "Task not found")
	}
	if err != nil {
		return preview, err
	}
	if task.Version != input.ExpectedVersion {
		return preview, taskVersionConflict()
	}
	preview.Label = task.Title
	before := aiTaskActionFields(task)
	if input.Action == "task.delete" {
		_, impact, err := loadTaskDeletionImpact(tx, task.ID, task.Version)
		if err != nil {
			return preview, err
		}
		preview.After = map[string]any{
			"task_deleted":                   true,
			"child_tasks_moved_to_top_level": impact.ChildTasks,
			"submissions_deleted":            impact.Submissions,
			"artifacts_deleted":              impact.Artifacts,
			"file_artifacts_deleted":         impact.FileArtifacts,
			"assignments_deleted":            impact.Assignments,
			"agent_runs_deleted":             impact.AgentRuns,
			"focus_sessions_detached":        impact.FocusSessions,
			"inbox_relations_detached":       impact.InboxRelations,
			"inbox_sources_coordinated":      impact.InboxSources,
			"tag_links_deleted":              impact.TagLinks,
		}
	} else if input.Action == "task.update" {
		var update updateTaskRequest
		_ = json.Unmarshal(input.Changes, &update)
		changes, tagIDs, err := validateTaskUpdate(update)
		if err != nil {
			return preview, err
		}
		if update.ReviewPolicy != nil {
			targetPolicy := changes["review_policy"].(string)
			if err := validateTaskReviewPolicyChange(tx, task, targetPolicy); err != nil {
				return preview, err
			}
			progress, err := loadTaskChildProgress(tx, task.ID)
			if err != nil {
				return preview, err
			}
			gatesReady, err := taskParentRollupGatesReady(tx, task.ID)
			if err != nil {
				return preview, err
			}
			// A changed policy has already passed the todo/no-history gate.
			// The native command reconciles this parent only on an actual change.
			willRequestReview := targetPolicy != task.ReviewPolicy && targetPolicy == "manual" && progress.readyForReview() && gatesReady
			preview.Before["status"], preview.After["status"] = task.Status, task.Status
			if willRequestReview {
				preview.After["status"] = "waiting_review"
			}
			preview.After["subtask_total"] = progress.Total
			preview.After["subtask_completed"] = progress.Completed
			preview.After["subtask_cancelled"] = progress.Cancelled
			preview.After["parent_rollup_gates_ready"] = gatesReady
			preview.After["will_request_parent_review"] = willRequestReview
		}
		if update.TagIDs.Set {
			tagIDs, err = validateAITaskTagIDs(tagIDs)
			if err != nil {
				return preview, err
			}
		}
		if project, ok := changes["project_id"].(string); ok {
			if err := requireAssignableProject(tx, project); err != nil {
				return preview, err
			}
		}
		for field, value := range changes {
			if field == "parent_task_id" {
				continue
			}
			preview.Before[field], preview.After[field] = before[field], value
		}
		if update.TagIDs.Set {
			tags, err := loadAITaskTags(tx, tagIDs)
			if err != nil {
				return preview, err
			}
			_, beforeNames := aiTaskTagValues(task.Tags)
			_, afterNames := aiTaskTagValues(tags)
			preview.Before["tag_names"] = beforeNames
			preview.After["tag_names"] = afterNames
		}
		if update.ParentTaskID.Set {
			preview.Before["parent_task_title"] = task.ParentTaskTitle
			parentID, err := validateAITaskParentID(update.ParentTaskID.Value)
			if err != nil {
				return preview, err
			}
			if parentID == nil {
				preview.After["parent_task_title"] = nil
			} else {
				parentTitle, err := aiTaskParentTitle(tx, task.ID, *parentID)
				if err != nil {
					return preview, err
				}
				preview.After["parent_task_title"] = parentTitle
			}
		}
	} else {
		command := strings.TrimPrefix(input.Action, "task.")
		if err := validateTaskLifecycleTransition(tx, task, command); err != nil {
			return preview, err
		}
		reason, _ := aiActionReason(input)
		changes, _, err := taskLifecycleUpdates(task, command, reason, now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return preview, err
		}
		preview.Before["status"], preview.After["status"] = task.Status, changes["status"]
		if reason != "" {
			preview.After["reason"] = reason
		}
	}
	return preview, nil
}

func executeAITaskAction(tx *gorm.DB, input aiWorkspaceAction, requestID, now string) (models.Task, error) {
	if isAIAssignmentAction(input.Action) {
		return executeAIAssignmentAction(tx, input, requestID, now)
	}
	if input.Action == "task.create" {
		var create createTaskRequest
		_ = json.Unmarshal(input.Changes, &create)
		task, err := taskFromCreateRequest(create)
		if err != nil {
			return models.Task{}, err
		}
		tagIDs, err := validateAITaskTagIDs(create.TagIDs)
		if err != nil {
			return models.Task{}, err
		}
		return createTaskInTransaction(tx, task, tagIDs, requestID)
	}
	if input.Action == "task.update" {
		var update updateTaskRequest
		_ = json.Unmarshal(input.Changes, &update)
		return updateTaskInTransaction(tx, input.TaskID, input.ExpectedVersion, update, requestID)
	}
	reason, err := aiActionReason(input)
	if err != nil {
		return models.Task{}, err
	}
	result, err := transitionTaskInTransaction(tx, input.TaskID, input.ExpectedVersion, strings.TrimPrefix(input.Action, "task."), reason, requestID, now)
	return result.Task, err
}

func (t *aiWorkspaceTool) propose(ctx context.Context, arguments json.RawMessage) (any, error) {
	agentFileFields := agentRunFileFieldsInArguments(arguments)
	if agentFileFields {
		if err := t.policy.Require("agent_files"); err != nil {
			return nil, err
		}
		if t.agentExecutionRun == nil {
			return nil, errors.New("read controlled file candidates in this saved generation before proposing Agent execution")
		}
	}
	input, err := parseAIWorkspaceAction(arguments)
	if err != nil {
		return nil, err
	}
	if input.Action == aiAgentDelegateFollowupAction && t.name != "workspace_agent_followup" {
		return nil, errors.New("Agent follow-up is not available through generic proposals")
	}
	if err := requireAIWorkspaceActionScopes(t.policy, input); err != nil {
		return nil, err
	}
	if agentFileFields {
		var changes aiAgentRunStartChanges
		if err := json.Unmarshal(input.Changes, &changes); err != nil {
			return nil, err
		}
		bindings, err := t.agentExecutionRun.resolveFileCandidates(input.TaskID, changes.InputFileCandidateIDs)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			if binding.File.SourceKind == agentexec.FileSourceProjectTaskArtifact {
				if err := t.policy.Require("agent_project_files"); err != nil {
					return nil, err
				}
			}
		}
	}
	encoded, _ := json.Marshal(input)
	fingerprint := sha256Hex(encoded)
	var proposal models.AIActionProposal
	err = t.api.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("ai_generations AS g").Joins("JOIN ai_sessions AS s ON s.id=g.session_id").
			Where("g.id=? AND g.status='streaming' AND s.persist=1", t.generationID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return newProjectRequestError(409, "AI_ACTION_CONVERSATION_UNAVAILABLE", "Proposals require an active saved conversation. Do not retry in this run; ask the user to continue in a saved conversation with a newly authorized message")
		}
		err := tx.Where("generation_id=? AND fingerprint=?", t.generationID, fingerprint).Take(&proposal).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Model(&models.AIActionProposal{}).Where("generation_id=?", t.generationID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 8 {
			return newProjectRequestError(409, "AI_ACTION_PROPOSAL_LIMIT", "This reply already contains eight proposals; existing proposals are retained, not executed. Stop proposing in this run, ask the user to review them, then continue in a new message with fresh authorization and current facts")
		}
		var preview aiActionPreview
		if input.Action == aiAgentDelegateSpawnAction {
			preview, err = previewAIAgentDelegation(tx, input, t.generationID, t.providerID, t.configVersion, t.delegationGrant, "", false)
		} else if input.Action == aiAgentDelegateFollowupAction {
			preview, err = previewAIAgentFollowup(tx, input, t.generationID, t.providerID, t.configVersion, t.delegationGrant, nil, t.api.options.Now())
		} else if input.Action == "agent_run.retry" {
			if err = requireAIAgentRetryFileScope(tx, t.policy, input); err != nil {
				return err
			}
			preview, err = previewAIAgentRunRetry(tx, input, t.api.artifactStore)
		} else if isAIAgentRunAction(input.Action) && agentFileFields {
			preview, err = previewAIAgentRunActionWithCandidates(tx, input, t.agentExecutionRun, t.api.artifactStore)
		} else {
			preview, err = previewAIWorkspaceAction(tx, input, t.api.options.Now())
		}
		if err != nil {
			return err
		}
		if t.api.invoicePDFStore == nil && (input.Action == "invoice.generate_pdf" || (input.Action == "invoice.delete" && preview.Before["pdf_asset_id"] != nil)) {
			return newInvoiceRequestError(503, "INVOICE_PDF_STORAGE_UNAVAILABLE", "Invoice PDF storage is unavailable")
		}
		previewJSON, _ := json.Marshal(preview)
		maxPreviewBytes := 24 << 10
		if input.Action == aiAgentDelegateSpawnAction || input.Action == aiAgentDelegateFollowupAction || strings.HasPrefix(input.Action, "project_note.") || isAITaskOutputAction(input.Action) || isAIAgentRunAction(input.Action) || input.Action == aiTaskBatchCreateAction || strings.HasPrefix(input.Action, "client_activity.") || strings.HasPrefix(input.Action, "financial_entry.") || strings.HasPrefix(input.Action, "invoice.") {
			maxPreviewBytes = 128 << 10
		}
		if len(previewJSON) > maxPreviewBytes {
			if isAITaskOutputAction(input.Action) {
				return newProjectRequestError(422, "AI_OUTPUT_PREVIEW_TOO_LARGE", "Complete evidence exceeds preview capacity; use Task details rather than truncating")
			}
			return newProjectRequestError(422, "AI_ACTION_PREVIEW_TOO_LARGE", "The complete approval preview exceeds capacity. No proposal was saved. Reduce the scope or use the workbench; do not truncate required evidence or retry unchanged")
		}
		proposal = models.AIActionProposal{ID: uuid.NewString(), GenerationID: t.generationID, Fingerprint: fingerprint,
			ActionJSON: string(encoded), PreviewJSON: string(previewJSON), Status: "pending", CreatedAt: nowStamp(t.api)}
		if err := tx.Create(&proposal).Error; err != nil {
			return err
		}
		return recordAIActionEvent(tx, proposal, "ai_workspace_action_proposed", "", nowStamp(t.api))
	})
	if err != nil {
		var invoice *invoiceRequestError
		if errors.As(err, &invoice) {
			return nil, errors.New(invoice.code + ": " + invoice.message)
		}
		var financial *financialEntryRequestError
		if errors.As(err, &financial) {
			return nil, errors.New(financial.code + ": " + financial.message)
		}
		var domain *projectRequestError
		if errors.As(err, &domain) {
			return nil, errors.New(domain.code + ": " + domain.message)
		}
		// No raw SQL/schema errors leave the tool boundary.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("proposal unavailable: verify target/version, saved conversation and eight-proposal limit")
	}
	return map[string]any{"proposal_id": proposal.ID, "status": proposal.Status, "action": input.Action,
		"instruction": "Proposal saved, NOT executed. Tell the user to review the separate confirmation card. Do not emit opc:task blocks or claim success."}, nil
}

func recordAIActionEvent(tx *gorm.DB, row models.AIActionProposal, action, requestID, now string, createdTaskIDs ...string) error {
	fields := map[string]any{"generation_id": row.GenerationID, "proposal_id": row.ID, "fingerprint": row.Fingerprint, "result_id": row.ResultID, "result_version": row.ResultVersion}
	if len(createdTaskIDs) > 0 {
		fields["created_task_ids"] = createdTaskIDs
	}
	payload, _ := json.Marshal(fields)
	return tx.Table("workflow_events").Create(map[string]any{
		"id": uuid.NewString(), "aggregate_type": "ai_action_proposal", "aggregate_id": row.ID, "action": action,
		"actor_id": models.BuiltinOwnerActorID, "request_id": aiNullableString(requestID), "current_json": string(payload), "created_at": now,
	}).Error
}

func aiActionOutput(row models.AIActionProposal, generationStatus string, now time.Time) (aiActionResponse, error) {
	var out aiActionResponse
	if json.Unmarshal([]byte(row.ActionJSON), &out.Action) != nil || json.Unmarshal([]byte(row.PreviewJSON), &out.Preview) != nil {
		return out, errors.New("invalid stored proposal")
	}
	out.ID, out.GenerationID, out.Fingerprint, out.Status = row.ID, row.GenerationID, row.Fingerprint, row.Status
	out.ResultID, out.ResultVersion, out.CreatedAt, out.DecidedAt = row.ResultID, row.ResultVersion, row.CreatedAt, row.DecidedAt
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return out, err
	}
	if row.Status == "pending" {
		if generationStatus == "failed" || generationStatus == "cancelled" {
			out.Status = "unavailable"
		} else if now.Sub(created) > 24*time.Hour {
			out.Status = "expired"
		}
	}
	out.CanConfirm = out.Status == "pending" && generationStatus == "completed"
	if out.Action.Action == aiAgentDelegateSpawnAction {
		if out.Preview.AgentDelegation == nil || out.Preview.AgentDelegation.ParentGenerationID != out.GenerationID {
			return out, errors.New("invalid delegated Agent proposal")
		}
		if out.Status == "confirmed" {
			if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || *out.ResultID != out.Preview.AgentDelegation.ChildSessionID {
				return out, errors.New("invalid delegated Agent receipt")
			}
			out.Route = "/ai?session=" + *out.ResultID
		}
		return out, nil
	}
	if out.Action.Action == aiAgentDelegateFollowupAction {
		if out.Preview.AgentFollowup == nil || out.Preview.AgentFollowup.Target.ParentSessionID == "" || out.Preview.AgentFollowup.Target.ChildSessionID == "" {
			return out, errors.New("invalid Agent follow-up proposal")
		}
		if out.Status == "confirmed" {
			if out.ResultID == nil || out.ResultVersion == nil || *out.ResultVersion != 1 || *out.ResultID != out.Preview.AgentFollowup.Target.ChildSessionID {
				return out, errors.New("invalid Agent follow-up receipt")
			}
			out.Route = "/ai?session=" + *out.ResultID
		}
		return out, nil
	}
	if out.Action.Action == "inbox.read_all" && out.Status == "confirmed" && out.Preview.InboxReadAll != nil {
		out.InboxReadAllResult = &readAllInboxItemsOutput{
			ThroughCreatedAt: out.Preview.InboxReadAll.ThroughCreatedAt,
			MarkedCount:      out.Preview.InboxReadAll.CandidateCount,
		}
	}
	if out.Action.Action == aiTaskBatchCreateAction && out.Status == "confirmed" {
		if out.ResultID == nil || *out.ResultID != out.ID || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.TaskBatchCreate == nil {
			return out, errors.New("invalid task batch create receipt")
		}
		out.TaskBatchCreateResult = aiTaskBatchCreateReceipt(out.ID, out.Preview.TaskBatchCreate)
	}
	if out.Action.Action == aiAgentRunCancelManyAction && out.Status == "confirmed" {
		if out.ResultID == nil || *out.ResultID != out.ID || out.ResultVersion == nil || *out.ResultVersion != 1 || out.Preview.AgentRunCancelMany == nil {
			return out, errors.New("invalid batch Agent cancellation receipt")
		}
	}
	kind, targetID := aiActionTarget(out.Action)
	if out.Action.Action == aiAgentRunCancelManyAction {
		out.Route = "/ai?workspace=agents"
	} else if out.Action.Action == "agent_run.cancel" || out.Action.Action == aiAgentRunRecoverOutput {
		out.Route = agentRunRoute(out.Action.TaskID, out.Action.AgentRunID)
	} else if isAIAgentRunAction(out.Action.Action) {
		out.Route = agentRunRoute(out.Action.TaskID, out.Action.AgentRunID)
		if sourceID, err := aiActionRestartOfRunID(out.Action); err == nil && sourceID != "" {
			out.Route = agentRunRoute(out.Action.TaskID, sourceID)
		}
		if out.Status == "confirmed" && out.ResultID != nil {
			out.Route = agentRunRoute(out.Action.TaskID, *out.ResultID)
		}
	} else if isAITaskOutputAction(out.Action.Action) {
		out.Route = searchRoute("task", out.Action.TaskID)
		submissionID := ""
		if out.ResultID != nil {
			submissionID = *out.ResultID
		} else if out.Action.Action == "task.review" {
			var changes aiReviewOutputChanges
			if err := json.Unmarshal(out.Action.Changes, &changes); err != nil {
				return out, err
			}
			submissionID = changes.SubmissionID
		}
		if id, err := uuid.Parse(submissionID); err == nil && id.String() == submissionID {
			out.Route = taskSubmissionRoute(out.Action.TaskID, submissionID)
		}
	} else if out.Action.Action == "finance.export_csv" {
		out.Route = ""
	} else if out.Action.Action == "inbox.read_all" {
		out.Route = "/inbox"
	} else if kind == "project_note" {
		projectID, _ := out.Preview.After["project_id"].(string)
		out.Route = searchRoute("project", projectID)
		noteID := targetID
		if out.ResultID != nil {
			noteID = *out.ResultID
		}
		if project, err := uuid.Parse(projectID); err == nil && project.String() == projectID {
			if note, err := uuid.Parse(noteID); err == nil && note.String() == noteID {
				out.Route = aiProjectNoteRoute(projectID, noteID)
			}
		}
	} else if kind == "client_contact" {
		clientID, _ := out.Preview.After["client_id"].(string)
		if id, err := uuid.Parse(clientID); err == nil && id.String() == clientID {
			out.Route = searchRoute("client", clientID)
		}
	} else if kind == "person" {
		// Person records live in the Settings modal, not at a routable detail URL.
		out.Route = ""
	} else if kind == "tag" {
		// Tags are managed from the Task workspace rather than a standalone route.
		out.Route = "/tasks"
	} else if kind == "client_followup" || kind == "client_activity" {
		clientID, _ := out.Preview.After["client_id"].(string)
		if id, err := uuid.Parse(clientID); err == nil && id.String() == clientID {
			out.Route = searchRoute("client", clientID)
			recordID := targetID
			if out.ResultID != nil {
				recordID = *out.ResultID
			}
			if id, err := uuid.Parse(recordID); err == nil && id.String() == recordID {
				out.Route = aiClientRecordRoute(clientID, strings.TrimPrefix(kind, "client_"), recordID)
			}
		}
	} else if kind == "automation_rule" || kind == "automation_run" {
		id := targetID
		if out.ResultID != nil {
			id = *out.ResultID
		}
		out.Route = aiAutomationRoute(strings.TrimPrefix(kind, "automation_"), id)
	} else if isAIKnowledgeAction(out.Action.Action) {
		// The native page only highlights a source whose latest job is the
		// exact failed job in the link. Management receipts describe queued,
		// running, cancelled or deleted states, so they open the library itself
		// instead of claiming a precise location that does not exist.
		out.Route = "/knowledge"
	} else if isAITaskViewAction(out.Action.Action) {
		// Saved views are applied inside the Tasks page filter bar; there is no
		// per-view route, so the receipt only opens the task list.
		out.Route = aiTaskViewRoute()
	} else if out.Action.Action == aiTaskBatchUpdateAction {
		// A batch has no single target route: the card and receipt link to the
		// task list where every affected task is visible.
		out.Route = "/tasks"
	} else if out.Action.Action == aiTaskBatchCreateAction {
		out.Route = searchRoute("project", out.Action.ProjectID)
	} else if kind == "financial_entry" {
		id := targetID
		if out.ResultID != nil {
			id = *out.ResultID
		}
		if id != "" {
			out.Route = "/income/" + id
		}
	} else if out.Action.Action == "project.delete" && out.Status == "confirmed" {
		out.Route = "/projects"
	} else if out.Action.Action == "client.delete" && out.Status == "confirmed" {
		out.Route = "/clients"
	} else if out.Action.Action == "content_item.delete" && out.Status == "confirmed" {
		out.Route = "/content-calendar"
	} else if out.Action.Action == "roadmap_milestone.delete" && out.Status == "confirmed" {
		out.Route = "/roadmap"
	} else if out.Action.Action == "task.delete" && out.Status == "confirmed" {
		out.Route = "/tasks"
	} else if out.Action.Action == "invoice.delete" && out.Status == "confirmed" {
		out.Route = "/invoices"
	} else if out.Action.Action == "invoice.generate_pdf" {
		out.Route = searchRoute("invoice", out.Action.InvoiceID)
	} else if out.ResultID != nil {
		out.Route = searchRoute(kind, *out.ResultID)
	} else if targetID != "" {
		out.Route = searchRoute(kind, targetID)
	}
	return out, nil
}

func (a *API) listAIWorkspaceActions(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "INVALID_AI_GENERATION_ID", "Invalid generation ID")
		return
	}
	var generation models.AIGeneration
	if err := a.db.WithContext(c.Request.Context()).First(&generation, "id=?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, 404, "AI_GENERATION_NOT_FOUND", "Generation not found")
		} else {
			writeDatabaseError(c)
		}
		return
	}
	rows := []models.AIActionProposal{}
	if err := a.db.WithContext(c.Request.Context()).Where("generation_id=?", id).Order("created_at,id").Find(&rows).Error; err != nil {
		writeDatabaseError(c)
		return
	}
	items := []aiActionResponse{}
	for _, row := range rows {
		out, err := aiActionOutputWithFacts(a.db.WithContext(c.Request.Context()), row, generation.Status, a.options.Now())
		if err != nil {
			writeDatabaseError(c)
			return
		}
		items = append(items, out)
	}
	c.JSON(200, gin.H{"data": items})
}

func (a *API) decideAIWorkspaceAction(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(c, 400, "AI_ACTION_ID_INVALID", "Invalid proposal ID")
		return
	}
	var input struct {
		ConfirmTaskOutput        *bool  `json:"confirm_task_output"`
		ConfirmTaskDelete        *bool  `json:"confirm_task_delete"`
		ConfirmProjectDelete     *bool  `json:"confirm_project_delete"`
		ConfirmClientDelete      *bool  `json:"confirm_client_delete"`
		ConfirmContentItemDelete *bool  `json:"confirm_content_item_delete"`
		ConfirmContentPublished  *bool  `json:"confirm_content_published"`
		ConfirmRoadmapDelete     *bool  `json:"confirm_roadmap_milestone_delete"`
		ConfirmTagDelete         *bool  `json:"confirm_tag_delete"`
		ConfirmInvoicePDF        *bool  `json:"confirm_invoice_pdf"`
		ConfirmFinanceExport     *bool  `json:"confirm_finance_export"`
		ConfirmInvoiceDelete     *bool  `json:"confirm_invoice_delete"`
		ConfirmInvoiceEffects    *bool  `json:"confirm_invoice_effects"`
		ConfirmFinancialEffects  *bool  `json:"confirm_financial_effects"`
		Fingerprint              string `json:"fingerprint"`
		Decision                 string `json:"decision"`
		ConfirmIncompleteTasks   *bool  `json:"confirm_incomplete_tasks"`
		ConfirmForceResolve      *bool  `json:"confirm_force_resolve"`
		ConfirmFocusStart        *bool  `json:"confirm_focus_start"`
		ConfirmFocusGap          *bool  `json:"confirm_focus_gap"`
		ConfirmFollowupCompleted *bool  `json:"confirm_followup_completed"`
		ConfirmProjectNoteDelete *bool  `json:"confirm_project_note_delete"`
		ConfirmActivityDelete    *bool  `json:"confirm_activity_delete"`
		ConfirmAutomationEffects *bool  `json:"confirm_automation_effects"`
		ConfirmAutomationRetry   *bool  `json:"confirm_automation_retry"`
		ConfirmAgentExecution    *bool  `json:"confirm_agent_execution"`
		ConfirmAgentDelegation   *bool  `json:"confirm_agent_delegation"`
		ConfirmAgentCancel       *bool  `json:"confirm_agent_cancel"`
		ConfirmAgentFiles        *bool  `json:"confirm_agent_files"`
		ConfirmAgentProjectFiles *bool  `json:"confirm_agent_project_files"`
		ConfirmAgentRework       *bool  `json:"confirm_agent_rework"`
		ConfirmAgentRestart      *bool  `json:"confirm_agent_restart"`
		ConfirmAgentRecovery     *bool  `json:"confirm_agent_output_recovery"`
		ConfirmKnowledgeDelete   *bool  `json:"confirm_knowledge_source_delete"`
		ConfirmTaskViewDelete    *bool  `json:"confirm_task_view_delete"`
	}
	if err := decodeJSON(c, &input); err != nil {
		writeError(c, 400, "INVALID_JSON", "Invalid decision request")
		return
	}
	if len(input.Fingerprint) != 64 || (input.Decision != "confirm" && input.Decision != "reject") {
		writeError(c, 422, "AI_ACTION_DECISION_INVALID", "Confirm or reject the exact displayed proposal")
		return
	}
	var out aiActionResponse
	completedProject := false
	overdueInvoice := false
	var movedInvoicePDF *trashedInvoicePDF
	var movedTaskArtifactFiles []trashedArtifactFile
	var movedProjectAttachmentFiles []trashedArtifactFile
	var movedClientAttachmentFiles []trashedArtifactFile
	deletedProjectID := ""
	deletedClientID := ""
	var generatedInvoicePDF *invoicePDFChange
	launchAgentRunID := ""
	var launchAgentDelegation *aiAgentDelegationLaunch
	var delegationReservation *aiDelegationReservation
	cancelAgentRunIDs := []string{}
	queuedKnowledgeJobID := ""
	decisionChanged := false
	recoveryFiles := agentRunOutputFileDelivery{api: a, store: a.artifactStore}
	defer func() {
		if delegationReservation != nil && a.aiDelegations != nil {
			a.aiDelegations.releaseReservation(delegationReservation)
		}
	}()
	decisionTransaction := func(operation func(*gorm.DB) error) error {
		return a.db.WithContext(c.Request.Context()).Transaction(operation)
	}
	// Action JSON is immutable. Read it before the transaction to preserve the
	// same store -> DB lock order used by native PDF writes/deletes and backups.
	// Output recovery also needs the pinned-connection commit boundary before
	// its file compensation can safely inspect durable Artifact references.
	if input.Decision == "confirm" {
		var candidate models.AIActionProposal
		if err := a.db.WithContext(c.Request.Context()).Select("action_json").Where("id=?", id).Take(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeError(c, 404, "AI_ACTION_NOT_FOUND", "Proposal not found")
			} else {
				writeDatabaseError(c)
			}
			return
		}
		var action aiWorkspaceAction
		if json.Unmarshal([]byte(candidate.ActionJSON), &action) == nil && a.invoicePDFStore != nil && (action.Action == "invoice.delete" || action.Action == "invoice.generate_pdf") {
			a.invoicePDFStore.mu.Lock()
			defer a.invoicePDFStore.mu.Unlock()
		}
		if action.Action == aiAgentRunRecoverOutput {
			decisionTransaction = func(operation func(*gorm.DB) error) error {
				return withAgentRunOutputTransaction(a.db.WithContext(c.Request.Context()), operation)
			}
		}
	}
	err := decisionTransaction(func(tx *gorm.DB) error {
		var row models.AIActionProposal
		if err := tx.First(&row, "id=?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return newProjectRequestError(404, "AI_ACTION_NOT_FOUND", "Proposal not found")
			}
			return err
		}
		if row.Fingerprint != input.Fingerprint {
			return newProjectRequestError(409, "AI_ACTION_CHANGED", "Reload the proposal before confirming")
		}
		var generation models.AIGeneration
		if err := tx.First(&generation, "id=?", row.GenerationID).Error; err != nil {
			return err
		}
		var err error
		clock := a.options.Now()
		out, err = aiActionOutputWithFacts(tx, row, generation.Status, clock)
		if err != nil {
			return err
		}
		if input.ConfirmIncompleteTasks != nil && (input.Decision != "confirm" || out.Action.Action != "project.complete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Incomplete-task consent is only valid for confirming project completion")
		}
		if input.ConfirmForceResolve != nil && (input.Decision != "confirm" || out.Action.Action != "inbox.force_resolve") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Force-resolve consent is only valid for confirming an Inbox force-resolve proposal")
		}
		if input.ConfirmFocusStart != nil && (input.Decision != "confirm" || out.Action.Action != "focus.start") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Start consent is only valid for confirming Focus start")
		}
		includesGap := out.Action.Action == "focus.recover" && aiFocusRecoveryAction(out.Action) == "include_gap_resume"
		if input.ConfirmFocusGap != nil && (input.Decision != "confirm" || !includesGap) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Gap consent is only valid for including the recovery gap")
		}
		if input.ConfirmTaskOutput != nil && (input.Decision != "confirm" || !isAITaskOutputAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task output consent only applies to confirming submit/review")
		}
		if input.ConfirmTaskDelete != nil && (input.Decision != "confirm" || out.Action.Action != "task.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task deletion consent only applies to confirming task deletion")
		}
		if input.ConfirmProjectDelete != nil && (input.Decision != "confirm" || out.Action.Action != "project.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Project deletion consent only applies to confirming project deletion")
		}
		if input.ConfirmClientDelete != nil && (input.Decision != "confirm" || out.Action.Action != "client.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Client deletion consent only applies to confirming client deletion")
		}
		if input.ConfirmContentItemDelete != nil && (input.Decision != "confirm" || out.Action.Action != "content_item.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Content Item deletion consent only applies to confirming Content Item deletion")
		}
		if input.ConfirmContentPublished != nil && (input.Decision != "confirm" || out.Action.Action != "content_item.publish") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Publication consent only applies to confirming a content publication fact")
		}
		if input.ConfirmRoadmapDelete != nil && (input.Decision != "confirm" || out.Action.Action != "roadmap_milestone.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Roadmap milestone deletion consent only applies to confirming roadmap milestone deletion")
		}
		if input.ConfirmTagDelete != nil && (input.Decision != "confirm" || out.Action.Action != "tag.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Tag deletion consent only applies to confirming tag deletion")
		}
		if input.ConfirmAgentExecution != nil && (input.Decision != "confirm" || !isAIAgentRunAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent execution consent only applies to confirming an Agent run start or retry")
		}
		if input.ConfirmAgentDelegation != nil && (input.Decision != "confirm" || (out.Action.Action != aiAgentDelegateSpawnAction && out.Action.Action != aiAgentDelegateFollowupAction)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent delegation consent only applies to confirming a child instruction")
		}
		if input.ConfirmAgentCancel != nil && (input.Decision != "confirm" || !isAIAgentRunCancelAction(out.Action.Action)) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent cancellation consent only applies to confirming cancellation")
		}
		if input.ConfirmAgentRecovery != nil && (input.Decision != "confirm" || out.Action.Action != aiAgentRunRecoverOutput) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Output recovery consent only applies to confirming output recovery")
		}
		if input.ConfirmKnowledgeDelete != nil && (input.Decision != "confirm" || out.Action.Action != aiKnowledgeDeleteAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Knowledge deletion consent only applies to confirming knowledge source deletion")
		}
		if input.ConfirmTaskViewDelete != nil && (input.Decision != "confirm" || out.Action.Action != aiTaskViewDeleteAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Task view deletion consent only applies to confirming task view deletion")
		}
		agentHasInputFiles := isAIAgentRunAction(out.Action.Action) && out.Preview.AgentRunStart != nil && len(out.Preview.AgentRunStart.InputFiles) > 0
		agentHasProjectFiles := agentHasInputFiles && agentRunHasProjectTaskFiles(out.Preview.AgentRunStart.InputFiles)
		agentHasRework := isAIAgentRunAction(out.Action.Action) && out.Preview.AgentRunStart != nil && out.Preview.AgentRunStart.ReworkContext != nil
		agentHasRestart := out.Action.Action == "agent_run.start" && out.Preview.AgentRunStart != nil && out.Preview.AgentRunStart.Restart != nil
		if input.ConfirmAgentRestart != nil && (input.Decision != "confirm" || !agentHasRestart) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Restart consent only applies to confirming a current-facts start with an explicit source Run")
		}
		if input.ConfirmAgentRework != nil && (input.Decision != "confirm" || !agentHasRework) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Rework consent only applies to confirming an Agent run with frozen rework context")
		}
		if input.ConfirmAgentFiles != nil && (input.Decision != "confirm" || !agentHasInputFiles) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Agent file consent only applies to confirming an Agent run with frozen controlled inputs")
		}
		if input.ConfirmAgentProjectFiles != nil && (input.Decision != "confirm" || !agentHasProjectFiles) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Project Task file consent only applies to confirming selected accepted predecessor files")
		}
		wanted := "confirmed"
		if input.ConfirmFinanceExport != nil && (input.Decision != "confirm" || out.Action.Action != "finance.export_csv") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Export consent only applies to CSV export")
		}
		financialAction := strings.HasPrefix(out.Action.Action, "financial_entry.")
		invoiceAction := strings.HasPrefix(out.Action.Action, "invoice.")
		if input.ConfirmInvoicePDF != nil && (input.Decision != "confirm" || out.Action.Action != "invoice.generate_pdf") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "PDF consent only applies to confirming invoice PDF generation")
		}
		if input.ConfirmInvoiceDelete != nil && (input.Decision != "confirm" || out.Action.Action != "invoice.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Invoice deletion consent only applies to confirming invoice deletion")
		}
		if input.ConfirmInvoiceEffects != nil && (input.Decision != "confirm" || !invoiceAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Invoice consent only applies to confirming an invoice command")
		}
		if input.ConfirmFinancialEffects != nil && (input.Decision != "confirm" || !financialAction) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Financial consent only applies to confirming a ledger change")
		}
		if input.ConfirmAutomationRetry != nil && (input.Decision != "confirm" || out.Action.Action != "automation.retry") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Retry consent only applies to retrying a captured automation run")
		}
		automationConsent := aiAutomationNeedsConsent(out.Action, out.Preview)
		if input.ConfirmAutomationEffects != nil && (input.Decision != "confirm" || !automationConsent) {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Automation consent only applies to enabling or updating an enabled rule")
		}
		if input.ConfirmProjectNoteDelete != nil && (input.Decision != "confirm" || out.Action.Action != "project_note.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Note deletion consent only applies to confirming note deletion")
		}
		if input.ConfirmActivityDelete != nil && (input.Decision != "confirm" || out.Action.Action != "client_activity.delete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Activity deletion consent only applies to deleting an activity")
		}
		if input.ConfirmFollowupCompleted != nil && (input.Decision != "confirm" || out.Action.Action != "client_followup.complete") {
			return newProjectRequestError(422, "AI_ACTION_DECISION_INVALID", "Completion consent only applies to completing a client followup")
		}
		if input.Decision == "reject" {
			wanted = "rejected"
		}
		if row.Status == wanted {
			if wanted == "confirmed" && out.Action.Action == "agent_run.cancel" && out.AgentRunResult != nil && out.AgentRunResult.Status == "running" {
				cancelAgentRunIDs = append(cancelAgentRunIDs, out.Action.AgentRunID)
			}
			if wanted == "confirmed" && out.Action.Action == aiAgentRunCancelManyAction && out.AgentRunCancelManyResult != nil {
				for _, item := range out.AgentRunCancelManyResult.Items {
					if item.Status == "running" {
						cancelAgentRunIDs = append(cancelAgentRunIDs, item.RunID)
					}
				}
			}
			return nil
		} // Durable identity, independent of HTTP retry keys/TTL.
		if row.Status != "pending" {
			return newProjectRequestError(409, "AI_ACTION_ALREADY_DECIDED", "Proposal already decided")
		}
		if input.Decision == "confirm" && !out.CanConfirm {
			return newProjectRequestError(409, "AI_ACTION_NOT_CONFIRMABLE", "Proposal is unfinished, expired or unavailable")
		}
		now := clock.UTC().Format(time.RFC3339Nano)
		updates := map[string]any{"status": wanted, "decided_at": now}
		if input.Decision == "confirm" {
			if (out.Action.Action == aiAgentDelegateSpawnAction || out.Action.Action == aiAgentDelegateFollowupAction) && (input.ConfirmAgentDelegation == nil || !*input.ConfirmAgentDelegation) {
				return newProjectRequestError(422, "AI_DELEGATION_CONFIRMATION_REQUIRED", "Review the complete child instruction, Provider and delegated scope subset before confirming")
			}
			if isAIAgentRunCancelAction(out.Action.Action) && (input.ConfirmAgentCancel == nil || !*input.ConfirmAgentCancel) {
				return newProjectRequestError(422, "AGENT_CANCEL_CONFIRMATION_REQUIRED", "Explicitly confirm cancelling the displayed Agent run set")
			}
			if out.Action.Action == aiAgentRunRecoverOutput && (input.ConfirmAgentRecovery == nil || !*input.ConfirmAgentRecovery) {
				return newProjectRequestError(422, "AGENT_OUTPUT_RECOVERY_CONFIRMATION_REQUIRED", "Confirm recovery of the stored output without running the model")
			}
			if isAIAgentRunAction(out.Action.Action) && (input.ConfirmAgentExecution == nil || !*input.ConfirmAgentExecution) {
				return newProjectRequestError(422, "AGENT_EXECUTION_CONFIRMATION_REQUIRED", "Explicitly confirm starting this exact frozen Agent run")
			}
			if agentHasInputFiles && (input.ConfirmAgentFiles == nil || !*input.ConfirmAgentFiles) {
				return newProjectRequestError(422, "AGENT_FILE_CONFIRMATION_REQUIRED", "Explicitly confirm sending the frozen controlled files to this exact Provider")
			}
			if agentHasProjectFiles && (input.ConfirmAgentProjectFiles == nil || !*input.ConfirmAgentProjectFiles) {
				return newProjectRequestError(422, "AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED", "Independently confirm sending the selected accepted predecessor Task files to this exact Provider")
			}
			if agentHasRework && (input.ConfirmAgentRework == nil || !*input.ConfirmAgentRework) {
				return newProjectRequestError(422, "AGENT_REWORK_CONFIRMATION_REQUIRED", "Review the full feedback and selected outputs, and explicitly confirm sending them to this exact Provider")
			}
			if agentHasRestart && (input.ConfirmAgentRestart == nil || !*input.ConfirmAgentRestart) {
				return newProjectRequestError(422, "AGENT_RESTART_CONFIRMATION_REQUIRED", "Review the source Run and complete current execution facts, then explicitly confirm a new execution")
			}
			if isAITaskOutputAction(out.Action.Action) && (input.ConfirmTaskOutput == nil || !*input.ConfirmTaskOutput) {
				return newProjectRequestError(422, "AI_TASK_OUTPUT_CONFIRMATION_REQUIRED", "Review the full evidence, attribution and effects before confirming")
			}
			if out.Action.Action == "task.delete" && (input.ConfirmTaskDelete == nil || !*input.ConfirmTaskDelete) {
				return newProjectRequestError(422, "TASK_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this Task and the displayed local history and files")
			}
			if out.Action.Action == "project.delete" && (input.ConfirmProjectDelete == nil || !*input.ConfirmProjectDelete) {
				return newProjectRequestError(422, "PROJECT_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived Project and the displayed local records and files")
			}
			if out.Action.Action == "client.delete" && (input.ConfirmClientDelete == nil || !*input.ConfirmClientDelete) {
				return newProjectRequestError(422, "CLIENT_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this inactive Client and the displayed local history and files")
			}
			if out.Action.Action == "content_item.delete" && (input.ConfirmContentItemDelete == nil || !*input.ConfirmContentItemDelete) {
				return newProjectRequestError(422, "CONTENT_ITEM_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived Content Item and the displayed local relations")
			}
			if out.Action.Action == "content_item.publish" && (input.ConfirmContentPublished == nil || !*input.ConfirmContentPublished) {
				return newProjectRequestError(422, "CONTENT_PUBLISHED_CONFIRMATION_REQUIRED", "Personally verify external publication before recording it as a local fact")
			}
			if out.Action.Action == "roadmap_milestone.delete" && (input.ConfirmRoadmapDelete == nil || !*input.ConfirmRoadmapDelete) {
				return newProjectRequestError(422, "ROADMAP_MILESTONE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this archived roadmap milestone and the displayed local relations")
			}
			if out.Action.Action == "tag.delete" && (input.ConfirmTagDelete == nil || !*input.ConfirmTagDelete) {
				return newProjectRequestError(422, "TAG_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this Tag and detaching it from the displayed Tasks")
			}
			if out.Action.Action == "finance.export_csv" && (input.ConfirmFinanceExport == nil || !*input.ConfirmFinanceExport) {
				return newProjectRequestError(422, "FINANCE_EXPORT_CONFIRMATION_REQUIRED", "Verify the export scope and included private fields before approving")
			}
			if out.Action.Action == "invoice.generate_pdf" && (input.ConfirmInvoicePDF == nil || !*input.ConfirmInvoicePDF) {
				return newProjectRequestError(422, "INVOICE_PDF_CONFIRMATION_REQUIRED", "Explicitly confirm generating or replacing the controlled local PDF; no external sending or download")
			}
			if invoiceAction && (input.ConfirmInvoiceEffects == nil || !*input.ConfirmInvoiceEffects) {
				return newProjectRequestError(422, "INVOICE_ACTION_CONFIRMATION_REQUIRED", "Explicitly verify invoice facts and any actual payment and local automation effects")
			}
			if out.Action.Action == "invoice.delete" && (input.ConfirmInvoiceDelete == nil || !*input.ConfirmInvoiceDelete) {
				return newProjectRequestError(422, "INVOICE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm permanently deleting this draft and its stored PDF; approval and audit history remain")
			}
			if financialAction && (input.ConfirmFinancialEffects == nil || !*input.ConfirmFinancialEffects) {
				return newProjectRequestError(422, "FINANCIAL_ACTION_CONFIRMATION_REQUIRED", "Explicitly verify the financial facts and their effect on local reports")
			}
			if out.Action.Action == "automation.retry" && (input.ConfirmAutomationRetry == nil || !*input.ConfirmAutomationRetry) {
				return newProjectRequestError(422, "AUTOMATION_RETRY_CONFIRMATION_REQUIRED", "Explicitly confirm retrying the original captured local action")
			}
			if automationConsent && (input.ConfirmAutomationEffects == nil || !*input.ConfirmAutomationEffects) {
				return newProjectRequestError(422, "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED", "Explicitly confirm continuing local automation effects")
			}
			if out.Action.Action == "project_note.delete" && (input.ConfirmProjectNoteDelete == nil || !*input.ConfirmProjectNoteDelete) {
				return newProjectRequestError(422, "PROJECT_NOTE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm soft deletion and retained history")
			}
			if out.Action.Action == "client_activity.delete" && (input.ConfirmActivityDelete == nil || !*input.ConfirmActivityDelete) {
				return newProjectRequestError(422, "CLIENT_ACTIVITY_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm soft deletion and retained history")
			}
			if out.Action.Action == "client_followup.complete" && (input.ConfirmFollowupCompleted == nil || !*input.ConfirmFollowupCompleted) {
				return newProjectRequestError(422, "CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED", "Confirm the actual completion time and reported result")
			}
			if out.Action.Action == aiKnowledgeDeleteAction && (input.ConfirmKnowledgeDelete == nil || !*input.ConfirmKnowledgeDelete) {
				return newProjectRequestError(422, "KNOWLEDGE_SOURCE_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm deleting this knowledge source, its index and its managed copy")
			}
			if out.Action.Action == aiTaskViewDeleteAction && (input.ConfirmTaskViewDelete == nil || !*input.ConfirmTaskViewDelete) {
				return newProjectRequestError(422, "TASK_VIEW_DELETE_CONFIRMATION_REQUIRED", "Explicitly confirm deleting this saved task view")
			}
			if out.Action.Action == "focus.start" && (input.ConfirmFocusStart == nil || !*input.ConfirmFocusStart) {
				return newProjectRequestError(422, "FOCUS_START_CONFIRMATION_REQUIRED", "Explicitly confirm starting a new local cycle")
			}
			if includesGap && (input.ConfirmFocusGap == nil || !*input.ConfirmFocusGap) {
				return newProjectRequestError(422, "FOCUS_GAP_CONFIRMATION_REQUIRED", "Explicitly confirm including the unknown gap")
			}
			action, err := parseAIWorkspaceAction([]byte(row.ActionJSON))
			if err != nil {
				return err
			}
			var preview aiActionPreview
			if action.Action == aiAgentDelegateSpawnAction {
				preview, err = previewAIAgentDelegationFromStored(tx, action, out.Preview.AgentDelegation)
			} else if action.Action == aiAgentDelegateFollowupAction {
				if out.Preview.AgentFollowup == nil {
					return errors.New("missing follow-up approval preview")
				}
				frozen := out.Preview.AgentFollowup
				grant := &aiWorkspaceGrant{ProviderVersion: frozen.Target.ProviderVersion, Scopes: append([]string(nil), frozen.Scopes...), KnowledgeSources: append([]aiKnowledgeSourceGrant(nil), frozen.Knowledge...)}
				preview, err = previewAIAgentFollowup(tx, action, row.GenerationID, frozen.Target.ProviderID, frozen.Target.ConfigVersion, grant, frozen, clock)
			} else if isAIAgentRunAction(action.Action) {
				preview, err = previewAIAgentRunActionFromStored(tx, action, out.Preview.AgentRunStart, a.artifactStore)
			} else {
				preview, err = previewAIWorkspaceAction(tx, action, clock)
			}
			if err != nil {
				return err
			}
			// Related Tasks can change progress without bumping the Inbox version.
			if isAIAgentRunCancelAction(action.Action) || action.Action == aiAgentRunRecoverOutput {
				encoded, marshalErr := json.Marshal(preview)
				if marshalErr != nil {
					return marshalErr
				}
				if string(encoded) != row.PreviewJSON {
					return newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Agent execution changed; request a fresh control proposal")
				}
			}
			// Never approve a newly computed effect instead of the displayed snapshot.
			if action.Action == aiAgentDelegateSpawnAction || action.Action == aiAgentDelegateFollowupAction || action.Action == "task.create" || action.Action == "task.update" || action.Action == "task.delete" || action.Action == aiTaskBatchUpdateAction || action.Action == aiTaskBatchCreateAction || action.Action == aiTaskMoveAction || action.Action == "project.delete" || strings.HasPrefix(action.Action, "tag.") || strings.HasPrefix(action.Action, "project_note.") || strings.HasPrefix(action.Action, "roadmap_milestone.") || strings.HasPrefix(action.Action, "content_item.") || strings.HasPrefix(action.Action, "client.") || strings.HasPrefix(action.Action, "client_contact.") || isAIPersonAction(action.Action) || isAITaskOutputAction(action.Action) || isAIAgentRunAction(action.Action) || isAIAssignmentAction(action.Action) || action.Action == "finance.export_csv" || invoiceAction || financialAction || strings.HasPrefix(action.Action, "automation.") || isAIKnowledgeAction(action.Action) || isAITaskViewAction(action.Action) || strings.HasPrefix(action.Action, "client_activity.") || strings.HasPrefix(action.Action, "client_followup.") || strings.HasPrefix(action.Action, "focus.") || isAIInboxTaskAction(action.Action) || action.Action == "inbox.split" || action.Action == "inbox.force_resolve" || action.Action == "inbox.read_all" {
				matches := false
				if isAIAgentRunAction(action.Action) {
					matches = aiAgentRunPreviewMatchesStored(preview, row.PreviewJSON)
				} else {
					encoded, marshalErr := json.Marshal(preview)
					if marshalErr != nil {
						return marshalErr
					}
					matches = string(encoded) == row.PreviewJSON
				}
				if !matches {
					return newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Related records or Task progress changed; request a fresh proposal")
				}
			}
			var result aiActionResult
			if action.Action == aiAgentDelegateSpawnAction {
				result, launchAgentDelegation, err = executeAIAgentDelegation(tx, row, action, preview.AgentDelegation, clock)
			} else if action.Action == aiAgentDelegateFollowupAction {
				result, launchAgentDelegation, err = executeAIAgentFollowup(tx, row, action, preview.AgentFollowup, clock)
			} else if action.Action == aiAgentRunRecoverOutput {
				_, err = a.recoverAgentRunOutputInTransaction(tx, action.AgentRunID, clock, &recoveryFiles)
				result = aiActionResult{ID: action.AgentRunID, Version: 1}
			} else if action.Action == aiAgentRunCancelManyAction {
				var ids []string
				ids, err = executeAIAgentRunCancelMany(tx, action, requestIDFromContext(c), now)
				if err == nil {
					result = aiActionResult{ID: row.ID, Version: 1}
					cancelAgentRunIDs = append(cancelAgentRunIDs, ids...)
				}
			} else if action.Action == "agent_run.cancel" {
				err = requestAgentRunCancellation(tx, action.AgentRunID, requestIDFromContext(c), now)
				if err == nil {
					result = aiActionResult{ID: action.AgentRunID, Version: 1}
					cancelAgentRunIDs = append(cancelAgentRunIDs, action.AgentRunID)
				}
			} else if isAIAgentRunAction(action.Action) {
				run, executeErr := executeAIAgentRunAction(tx, action, preview.AgentRunStart, a.artifactStore, requestIDFromContext(c), now)
				err = executeErr
				if err == nil {
					result = aiActionResult{ID: run.ID, Version: 1}
					launchAgentRunID = run.ID
				}
			} else if action.Action == "finance.export_csv" {
				// Approval identity/version, not a ledger record or saved file.
				result = aiActionResult{ID: row.ID, Version: 1}
			} else if isAIKnowledgeAction(action.Action) {
				// Indexing is enqueued only after this transaction commits; the
				// durable job row must already exist for the indexer to claim it.
				var knowledgeErr error
				result, queuedKnowledgeJobID, knowledgeErr = executeAIKnowledgeSourceAction(tx, action, now)
				err = knowledgeErr
			} else if isAITaskViewAction(action.Action) {
				result, err = executeAITaskViewAction(tx, action, now)
			} else if action.Action == aiTaskBatchUpdateAction {
				if batchErr := executeAITaskBatchAction(tx, action, requestIDFromContext(c), now); batchErr != nil {
					err = batchErr
				} else {
					// A batch has no single target; the proposal is the durable
					// receipt identity, exactly like the snapshot read-all card.
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == aiTaskBatchCreateAction {
				if batchErr := executeAITaskBatchCreateAction(tx, action, row.ID, requestIDFromContext(c), now); batchErr != nil {
					err = batchErr
				} else {
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == aiTaskMoveAction {
				result, err = executeAITaskMoveAction(tx, action, clock)
			} else if action.Action == aiRoadmapMilestoneMoveAction {
				result, err = executeAIRoadmapMilestoneMove(tx, action, clock)
			} else if action.Action == "inbox.read_all" {
				bulk, executeErr := executeAIInboxReadAllAction(tx, action, requestIDFromContext(c), clock)
				if executeErr != nil {
					err = executeErr
				} else if expected, ok := preview.After["marked_count"].(int64); !ok || expected != bulk.MarkedCount {
					err = newProjectRequestError(409, "AI_ACTION_PREVIEW_CHANGED", "Inbox snapshot changed; request a fresh proposal")
				} else {
					// Batch actions have no single Inbox entity. The proposal is the
					// durable receipt identity; version 1 is the receipt version.
					result = aiActionResult{ID: row.ID, Version: 1}
				}
			} else if action.Action == "invoice.generate_pdf" {
				generatedInvoicePDF, err = a.generateAIInvoicePDF(tx, row, action, clock)
				if err == nil {
					result = aiActionResult{ID: generatedInvoicePDF.asset.ID, Version: action.ExpectedVersion}
				}
			} else if action.Action == "invoice.delete" {
				movedInvoicePDF, err = deleteInvoiceInTransaction(tx, a.invoicePDFStore, action.InvoiceID, action.ExpectedVersion, requestIDFromContext(c), now)
				// Hard deletion has no new version; retain the last deleted version
				// as a historical receipt, never an extant entity/version claim.
				result = aiActionResult{ID: action.InvoiceID, Version: action.ExpectedVersion}
			} else if action.Action == "task.delete" {
				_, _, movedTaskArtifactFiles, err = a.deleteTaskInTransaction(tx, action.TaskID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The receipt identifies the deleted Task and its last version. It
				// must never be interpreted as an extant Task/version pair.
				result = aiActionResult{ID: action.TaskID, Version: action.ExpectedVersion}
			} else if action.Action == "project.delete" {
				deletedProjectID = action.ProjectID
				_, _, _, movedProjectAttachmentFiles, err = a.deleteProjectInTransaction(tx, action.ProjectID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ProjectID, Version: action.ExpectedVersion}
			} else if action.Action == "client.delete" {
				deletedClientID = action.ClientID
				_, _, _, movedClientAttachmentFiles, err = a.deleteClientInTransaction(tx, action.ClientID, action.ExpectedVersion, now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ClientID, Version: action.ExpectedVersion}
			} else if action.Action == "content_item.delete" {
				_, _, err = deleteContentItemInTransaction(tx, action.ContentItemID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.ContentItemID, Version: action.ExpectedVersion}
			} else if action.Action == "roadmap_milestone.delete" {
				_, _, err = deleteRoadmapMilestoneInTransaction(tx, action.RoadmapMilestoneID, action.ExpectedVersion, requestIDFromContext(c), now)
				// The deleted aggregate keeps its last version only as receipt data.
				result = aiActionResult{ID: action.RoadmapMilestoneID, Version: action.ExpectedVersion}
			} else if action.Action == "tag.delete" {
				_, err = deleteTagInTransaction(tx, action.TagID, action.ExpectedVersion, now)
				result = aiActionResult{ID: action.TagID, Version: action.ExpectedVersion}
			} else {
				result, err = a.executeAIWorkspaceAction(tx, action, requestIDFromContext(c), clock, input.ConfirmIncompleteTasks != nil && *input.ConfirmIncompleteTasks, input.ConfirmForceResolve != nil && *input.ConfirmForceResolve)
			}
			if err != nil {
				return err
			}
			completedProject = action.Action == "project.complete"
			overdueInvoice = action.Action == "invoice.mark_overdue"
			row.ResultID, row.ResultVersion = &result.ID, &result.Version
			out.CreatedTaskIDs = result.CreatedTaskIDs
			updates["result_id"], updates["result_version"] = result.ID, result.Version
		}
		updated := tx.Model(&models.AIActionProposal{}).Where("id=? AND status='pending'", id).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return newProjectRequestError(409, "AI_ACTION_ALREADY_DECIDED", "Proposal already decided")
		}
		row.Status, row.DecidedAt = wanted, &now
		if err := recordAIActionEvent(tx, row, "ai_workspace_action_"+wanted, requestIDFromContext(c), now, out.CreatedTaskIDs...); err != nil {
			return err
		}
		if launchAgentDelegation != nil && launchAgentDelegation.Followup == nil {
			if err := recordAIAgentDelegationLifecycleEvent(tx, launchAgentDelegation.ProposalID, launchAgentDelegation.SessionID, aiDelegationQueuedEvent, requestIDFromContext(c), now); err != nil {
				return err
			}
		}
		out, err = aiActionOutputWithFacts(tx, row, generation.Status, a.options.Now())
		if err == nil {
			if launchAgentDelegation != nil {
				delegationReservation, err = a.reserveAIAgentDelegations([]aiAgentDelegationLaunch{*launchAgentDelegation})
			}
			if err == nil {
				decisionChanged = true
			}
		}
		return err
	})
	a.finishInvoiceDeletion(movedInvoicePDF, err)
	a.finishTaskDeletion(movedTaskArtifactFiles, err)
	a.finishProjectDeletion(deletedProjectID, movedProjectAttachmentFiles, err)
	a.finishClientDeletion(deletedClientID, movedClientAttachmentFiles, err)
	a.finishInvoicePDFChange(generatedInvoicePDF, err)
	err = recoveryFiles.finish(err)
	if err != nil {
		if writeFinancialEntryRequestError(c, err) || writeInvoiceRequestError(c, err) {
			return
		}
		if writeProjectRequestError(c, mapReminderConstraintError(mapInboxTaskConstraintError(mapInboxItemConstraintError(mapTaskWorkflowConstraintError(err))))) {
			return
		}
		writeDatabaseError(c)
		return
	}
	if decisionChanged {
		a.wakeAIContinuationsAfterFactCommit()
	}
	if launchAgentRunID != "" {
		a.launchAgentRun(launchAgentRunID)
	}
	if launchAgentDelegation != nil {
		if code := a.aiDelegations.launchReservedBatch(delegationReservation, []aiAgentDelegationLaunch{*launchAgentDelegation}); code != "" {
			_ = recordAIDelegationLaunchFailure(a.db, *launchAgentDelegation, code, a.options.Now())
		}
	}
	if queuedKnowledgeJobID != "" && !a.knowledgeIndexer.enqueue(context.Background(), queuedKnowledgeJobID) {
		a.failKnowledgeIndexJob(queuedKnowledgeJobID, "KNOWLEDGE_INDEX_QUEUE_UNAVAILABLE")
	}
	for _, runID := range cancelAgentRunIDs {
		if cancel, ok := a.agentRunCancel(runID); ok {
			cancel()
		}
	}
	if completedProject {
		a.consumeAutomationEventDeliveriesBestEffort("project-completed")
	}
	if overdueInvoice {
		a.consumeAutomationEventDeliveriesBestEffort("invoice-overdue")
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}
