package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

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
