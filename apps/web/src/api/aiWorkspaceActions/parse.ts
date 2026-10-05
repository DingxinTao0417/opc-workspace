import { automationLocationHref } from "../../lib/automationLocation";
import { clientRecordHref } from "../../lib/clientRecordLocation";
import { projectNoteHref } from "../../lib/projectNoteLocation";
import { taskSubmissionHref } from "../../lib/taskSubmissionLocation";
import {
  isAssignmentAction,
  validAssignmentProposal,
} from "../aiAssignmentActions";
import { validAutomationRetryProposal } from "../aiAutomationRetry";
import { validFinanceExport } from "../aiFinanceExport";
import { validFinancialProposal } from "../aiFinancialActions";
import {
  validInvoicePdfResult,
  validInvoiceProposal,
} from "../aiInvoiceActions";
import { validProjectNoteProposal } from "../aiProjectNoteActions";
import {
  isTaskOutputAction,
  validTaskOutputProposal,
} from "../aiTaskOutputActions";
import { apiRequest } from "../client";
import {
  validAgentDelegationProposal,
  validAgentFollowupProposal,
} from "./agentDelegationProposals";
import {
  validAgentRunCancelManyProposal,
  validAgentRunCancelProposal,
  validAgentRunStartProposal,
} from "./agentRunProposals";
import { aiActionLabels } from "./catalog";
import {
  validClientContactProposal,
  validClientProposal,
  validPersonProposal,
} from "./clientProposals";
import { validContentItemProposal } from "./contentProposals";
import { validKnowledgeProposal } from "./knowledgeProposals";
import { canonicalUuid, fields, invalid, object, uuid } from "./primitives";
import {
  validProjectDeleteProposal,
  validRoadmapOrderProposal,
  validRoadmapProposal,
} from "./projectProposals";
import {
  splitDrafts,
  validTagProposal,
  validTaskBatchCreateProposal,
  validTaskBatchProposal,
  validTaskDeleteProposal,
  validTaskOrderProposal,
  validTaskParentProposal,
  validTaskReviewPolicyProposal,
  validTaskTagProposal,
  validTaskViewProposal,
} from "./taskProposals";
import type { AiActionProposal } from "./types";

export function parseAiActionProposal(value: unknown): AiActionProposal {
  if (
    !object(value) ||
    typeof value.id !== "string" ||
    !uuid.test(value.id) ||
    typeof value.generation_id !== "string" ||
    !uuid.test(value.generation_id) ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    !object(value.action) ||
    typeof value.action.action !== "string" ||
    !Object.hasOwn(aiActionLabels, value.action.action) ||
    !object(value.action.changes) ||
    !object(value.preview) ||
    typeof value.preview.label !== "string" ||
    !fields(value.preview.before) ||
    !fields(value.preview.after) ||
    typeof value.can_confirm !== "boolean" ||
    typeof value.status !== "string" ||
    !["pending", "confirmed", "rejected", "expired", "unavailable"].includes(
      value.status,
    ) ||
    (value.can_confirm && value.status !== "pending") ||
    typeof value.created_at !== "string" ||
    (value.decided_at !== null && typeof value.decided_at !== "string")
  )
    return invalid();
  if (value.created_task_ids !== undefined) {
    const ids = value.created_task_ids;
    if (
      value.action.action !== "inbox.split" ||
      value.status !== "confirmed" ||
      !Array.isArray(ids) ||
      ids.length < 1 ||
      ids.length > 20 ||
      !Array.isArray(value.preview.tasks) ||
      ids.length !== value.preview.tasks.length ||
      ids.some((id) => typeof id !== "string" || !canonicalUuid.test(id)) ||
      new Set(ids).size !== ids.length
    )
      return invalid();
  }
  if (value.action.action === "agent_delegate.spawn") {
    if (!validAgentDelegationProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "agent_delegate.followup") {
    if (!validAgentFollowupProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "agent_run.cancel_many") {
    if (!validAgentRunCancelManyProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (
    value.action.action === "agent_run.cancel" ||
    value.action.action === "agent_run.recover_output"
  ) {
    if (!validAgentRunCancelProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (
    value.action.action.startsWith("knowledge_source.") ||
    value.action.action.startsWith("knowledge_index_job.")
  ) {
    if (!validKnowledgeProposal(value)) return invalid();
    // Management receipts describe queued/running/cancelled/deleted states the
    // native page cannot highlight, so they open the library root instead of
    // inventing a precise location.
    return { ...(value as unknown as AiActionProposal), route: "/knowledge" };
  }
  if (value.action.action.startsWith("task_view.")) {
    if (!validTaskViewProposal(value)) return invalid();
    // Saved views are applied inside the Tasks page filter bar; there is no
    // per-view route, so the card only links to the task list.
    return { ...(value as unknown as AiActionProposal), route: "/tasks" };
  }
  if (value.action.action === "task.batch_update") {
    if (!validTaskBatchProposal(value)) return invalid();
    return { ...(value as unknown as AiActionProposal), route: "/tasks" };
  }
  if (value.action.action === "task.batch_create") {
    if (!validTaskBatchCreateProposal(value)) return invalid();
    return value as unknown as AiActionProposal;
  }
  if (value.action.action === "task.move") {
    if (!validTaskOrderProposal(value)) return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: `/tasks/${value.action.task_id}`,
    };
  }
  if (value.action.action === "roadmap_milestone.move") {
    if (!validRoadmapOrderProposal(value)) return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: `/roadmap?milestone=${value.action.roadmap_milestone_id}`,
    };
  }
  if (
    (value.action.agent_run_id !== undefined &&
      value.action.action !== "agent_run.retry") ||
    (value.preview.agent_run_retry !== undefined &&
      value.action.action !== "agent_run.retry") ||
    value.preview.agent_run_cancel !== undefined ||
    value.preview.agent_run_recovery !== undefined
  )
    return invalid();
  if (value.action.action.startsWith("project_note.")) {
    if (!validProjectNoteProposal(value as unknown as AiActionProposal))
      return invalid();
    const projectId = String(value.preview.after.project_id);
    const noteId =
      typeof value.result_id === "string"
        ? value.result_id
        : typeof value.action.project_note_id === "string"
          ? value.action.project_note_id
          : null;
    return {
      ...(value as unknown as AiActionProposal),
      route: noteId
        ? projectNoteHref(projectId, noteId)
        : `/projects/${projectId}`,
    };
  }
  if (
    value.action.action === "agent_run.start" ||
    value.action.action === "agent_run.retry"
  ) {
    if (!validAgentRunStartProposal(value)) return invalid();
    const result = value.agent_run_result as
      Record<string, unknown> | undefined;
    if (result === undefined) {
      return {
        ...(value as unknown as AiActionProposal),
        route: value.status === "confirmed" ? "" : String(value.route),
      };
    }
    const route =
      result?.output_delivery_status === "submitted" &&
      typeof result.submission_id === "string"
        ? taskSubmissionHref(String(value.action.task_id), result.submission_id)
        : String(value.route);
    return { ...(value as unknown as AiActionProposal), route };
  }
  if (
    value.preview.agent_run_start !== undefined ||
    value.agent_run_result !== undefined
  )
    return invalid();
  if (Object.hasOwn(value.action, "project_note_id")) return invalid();
  if (
    Object.hasOwn(value.action, "knowledge_source_id") ||
    Object.hasOwn(value.action, "knowledge_index_job_id") ||
    value.preview.knowledge !== undefined
  )
    return invalid();
  if (
    Object.hasOwn(value.action, "task_saved_view_id") ||
    value.preview.task_view !== undefined
  )
    return invalid();
  if (value.preview.task_batch !== undefined) return invalid();
  if (value.preview.task_order !== undefined) return invalid();
  if (value.preview.roadmap_order !== undefined) return invalid();
  if (isTaskOutputAction(value.action.action)) {
    if (!validTaskOutputProposal(value as unknown as AiActionProposal))
      return invalid();
    return {
      ...(value as unknown as AiActionProposal),
      route: value.result_id
        ? `/tasks/${value.action.task_id}/submissions/${value.result_id}`
        : value.action.action === "task.review"
          ? `/tasks/${value.action.task_id}/submissions/${value.action.changes.submission_id}`
          : `/tasks/${value.action.task_id}`,
    };
  }
  if (value.preview.task_output !== undefined) return invalid();
  if (value.action.action === "inbox.read_all") {
    const readAllAction = value.action as Record<string, unknown>;
    const changes = value.action.changes;
    const before = value.preview.before;
    const after = value.preview.after;
    const snapshot = value.preview.inbox_read_all;
    const cutoff = changes.through_created_at;
    const count = after.marked_count;
    const targetKeys = [
      "invoice_id",
      "financial_entry_id",
      "task_id",
      "project_id",
      "inbox_item_id",
      "reminder_id",
      "focus_session_id",
      "client_followup_id",
      "client_activity_id",
      "client_id",
      "automation_rule_id",
      "automation_run_id",
      "project_note_id",
      "expected_version",
    ];
    const validTimestamp =
      typeof cutoff === "string" &&
      /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(cutoff) &&
      Number.isFinite(Date.parse(cutoff));
    const validCount =
      typeof count === "number" &&
      Number.isSafeInteger(count) &&
      count > 0 &&
      count <= 1_000;
    if (
      Object.keys(readAllAction).length !== 2 ||
      Object.keys(readAllAction).some(
        (key) => key !== "action" && key !== "changes",
      ) ||
      Object.keys(value.preview).length !== 4 ||
      Object.keys(value.preview).some(
        (key) => !["label", "before", "after", "inbox_read_all"].includes(key),
      ) ||
      Object.keys(changes).length !== 1 ||
      !validTimestamp ||
      targetKeys.some((key) => readAllAction[key] !== undefined) ||
      !object(snapshot) ||
      Object.keys(snapshot).length !== 3 ||
      snapshot.through_created_at !== cutoff ||
      snapshot.candidate_count !== count ||
      typeof snapshot.selection_fingerprint !== "string" ||
      !/^[a-f0-9]{64}$/.test(snapshot.selection_fingerprint) ||
      Object.keys(before).length !== 2 ||
      before.snapshot_at !== cutoff ||
      before.snapshot_unread_count !== count ||
      Object.keys(after).length !== 3 ||
      after.through_created_at !== cutoff ||
      after.snapshot_unread_count !== 0 ||
      !validCount ||
      value.preview.tasks !== undefined ||
      value.preview.next_followup !== undefined ||
      value.preview.automation_retry !== undefined ||
      value.automation_run_result !== undefined ||
      value.invoice_pdf_result !== undefined ||
      (value.result_id !== null &&
        (typeof value.result_id !== "string" || !uuid.test(value.result_id))) ||
      (value.result_version !== null && value.result_version !== 1)
    )
      return invalid();
    const confirmed = value.status === "confirmed";
    const readAllResult = object(value.inbox_read_all_result)
      ? value.inbox_read_all_result
      : null;
    if (
      confirmed !== Boolean(readAllResult) ||
      (confirmed &&
        (!readAllResult ||
          Object.keys(readAllResult).length !== 2 ||
          value.result_id !== value.id ||
          value.result_version !== 1 ||
          readAllResult.through_created_at !== cutoff ||
          readAllResult.marked_count !== count)) ||
      (!confirmed &&
        (value.result_id !== null || value.result_version !== null))
    )
      return invalid();
    return { ...(value as unknown as AiActionProposal), route: "/inbox" };
  }
  if (
    value.preview.inbox_read_all !== undefined ||
    value.inbox_read_all_result !== undefined
  )
    return invalid();
  if (value.action.action === "finance.export_csv") {
    if (!validFinanceExport(value as unknown as AiActionProposal))
      return invalid();
    return { ...(value as unknown as AiActionProposal), route: "" };
  }
  if (
    isAssignmentAction(value.action.action) &&
    !validAssignmentProposal(value as unknown as AiActionProposal)
  )
    return invalid();
  const isProject = value.action.action.startsWith("project.");
  const isTag = value.action.action.startsWith("tag.");
  const isRoadmap = value.action.action.startsWith("roadmap_milestone.");
  const isContentItem = value.action.action.startsWith("content_item.");
  const isFinancial = value.action.action.startsWith("financial_entry.");
  const isInvoice = value.action.action.startsWith("invoice.");
  const isInbox = value.action.action.startsWith("inbox.");
  const isReminder = value.action.action.startsWith("reminder.");
  const isFocus = value.action.action.startsWith("focus.");
  const isClient = value.action.action.startsWith("client.");
  const isClientContact = value.action.action.startsWith("client_contact.");
  const isPerson = value.action.action.startsWith("person.");
  const isFollowup = value.action.action.startsWith("client_followup.");
  const isActivity = value.action.action.startsWith("client_activity.");
  const isAutomation = value.action.action.startsWith("automation.");
  const isAutomationRetry = value.action.action === "automation.retry";
  const action = value.action;
  if (
    !validTaskReviewPolicyProposal(value) ||
    !validTaskTagProposal(value) ||
    !validTaskParentProposal(value) ||
    !validTaskDeleteProposal(value) ||
    !validProjectDeleteProposal(value) ||
    !validTagProposal(value)
  )
    return invalid();
  const targetKey = isInvoice
    ? "invoice_id"
    : isFinancial
      ? "financial_entry_id"
      : isAutomationRetry
        ? "automation_run_id"
        : isAutomation
          ? "automation_rule_id"
          : isClient || isClientContact
            ? "client_id"
            : isPerson
              ? "actor_id"
              : isTag
                ? "tag_id"
                : isActivity
                  ? "client_activity_id"
                  : isFollowup
                    ? "client_followup_id"
                    : isFocus
                      ? "focus_session_id"
                      : isReminder
                        ? "reminder_id"
                        : isInbox
                          ? "inbox_item_id"
                          : isRoadmap
                            ? "roadmap_milestone_id"
                            : isContentItem
                              ? "content_item_id"
                              : isProject
                                ? "project_id"
                                : "task_id";
  const targetId = value.action[targetKey];
  const unexpectedTarget = [
    "invoice_id",
    "financial_entry_id",
    "task_id",
    "tag_id",
    "project_id",
    "roadmap_milestone_id",
    "content_item_id",
    "inbox_item_id",
    "reminder_id",
    "focus_session_id",
    "client_followup_id",
    "client_activity_id",
    "client_id",
    "client_actor_link_id",
    "actor_id",
    "automation_rule_id",
    "automation_run_id",
    "knowledge_source_id",
    "knowledge_index_job_id",
  ].some(
    (key) =>
      key !== targetKey &&
      !(isClientContact && key === "client_actor_link_id") &&
      action[key] !== undefined,
  );
  const creating =
    value.action.action.endsWith(".create") || action.action === "focus.start";
  if (isInvoice && !validInvoiceProposal(value as unknown as AiActionProposal))
    return invalid();
  if (
    isFinancial &&
    !validFinancialProposal(value as unknown as AiActionProposal)
  )
    return invalid();
  if (isRoadmap && !validRoadmapProposal(value)) return invalid();
  if (isContentItem && !validContentItemProposal(value)) return invalid();
  if (isClient && !validClientProposal(value)) return invalid();
  if (isClientContact && !validClientContactProposal(value)) return invalid();
  if (isPerson && !validPersonProposal(value)) return invalid();
  if (!isFollowup && value.preview.next_followup !== undefined)
    return invalid();
  if (isAutomationRetry) {
    if (!validAutomationRetryProposal(value)) return invalid();
  } else if (
    value.preview.automation_retry !== undefined ||
    value.automation_run_result !== undefined
  )
    return invalid();
  if (isAutomation && !isAutomationRetry) {
    const before = value.preview.before;
    const after = value.preview.after;
    const changes = value.action.changes;
    const updating = action.action === "automation.update";
    const presetIds: Record<string, [string, string, string]> = {
      "00000000-0000-5000-8000-000000000101": [
        "project-completed-inbox",
        "event",
        "inbox_item",
      ],
      "00000000-0000-5000-8000-000000000102": [
        "daily-today-reminder",
        "schedule",
        "reminder",
      ],
      "00000000-0000-5000-8000-000000000103": [
        "weekly-review-reminder",
        "schedule",
        "reminder",
      ],
      "00000000-0000-5000-8000-000000000104": [
        "invoice-overdue-task",
        "event",
        "task",
      ],
      "00000000-0000-5000-8000-000000000105": [
        "agent-run-failed-inbox",
        "event",
        "inbox_item",
      ],
    };
    const preset = presetIds[String(targetId)];
    if (!preset) return invalid();
    const configKeys =
      preset[1] === "schedule" ? ["local_time", "timezone"] : ["priority"];
    const metadata = [
      "preset_key",
      "name",
      "available",
      "unavailable_reason",
      "trigger_type",
      "trigger_label",
      "action_type",
      "action_label",
      "permission_summary",
    ];
    const keys = [...metadata, "rule_enabled", ...configKeys];
    const validRecord = (f: Record<string, unknown>) => {
      if (
        Object.keys(f).length !== keys.length ||
        Object.keys(f).some((key) => !keys.includes(key)) ||
        metadata.some((key) =>
          key === "available"
            ? typeof f[key] !== "boolean"
            : typeof f[key] !== "string",
        ) ||
        typeof f.rule_enabled !== "boolean" ||
        !f.name ||
        !f.permission_summary ||
        !f.trigger_label ||
        !f.action_label ||
        f.preset_key !== preset[0] ||
        f.trigger_type !== preset[1] ||
        f.action_type !== preset[2]
      )
        return false;
      if (preset[1] === "event")
        return ["P0", "P1", "P2", "P3"].includes(String(f.priority));
      if (
        typeof f.local_time !== "string" ||
        !/^([01]\d|2[0-3]):[0-5]\d$/.test(f.local_time) ||
        typeof f.timezone !== "string" ||
        f.timezone === "Local" ||
        !f.timezone ||
        f.timezone.length > 100
      )
        return false;
      try {
        new Intl.DateTimeFormat("en", { timeZone: f.timezone });
        return true;
      } catch {
        return false;
      }
    };
    if (
      !validRecord(before) ||
      !validRecord(after) ||
      value.preview.label !== after.name ||
      metadata.some((key) => before[key] !== after[key]) ||
      Object.keys(changes).length !== (updating ? configKeys.length : 0) ||
      Object.keys(changes).some(
        (key) => !configKeys.includes(key) || changes[key] !== after[key],
      ) ||
      (updating
        ? before.rule_enabled !== after.rule_enabled
        : configKeys.some((key) => before[key] !== after[key])) ||
      (!updating &&
        after.rule_enabled !== (action.action === "automation.enable")) ||
      (action.action === "automation.enable" && !after.available) ||
      (value.result_id !== null && value.result_id !== targetId)
    )
      return invalid();
  }
  if (isActivity) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const deleting = action.action === "client_activity.delete";
    const text = (v: unknown, max: number) =>
      typeof v === "string" &&
      v.trim().length > 0 &&
      Array.from(v).length <= max;
    const metadata = [
      "client_id",
      "client_name",
      "client_status",
      "kind",
      "title",
      "occurred_at",
      "activity_state",
    ];
    const bodyChanged = Object.hasOwn(changes, "body");
    const previewKeys = [...metadata, ...(bodyChanged ? ["body"] : [])];
    const record = (f: Record<string, unknown>, state: string) =>
      typeof f.client_id === "string" &&
      uuid.test(f.client_id) &&
      text(f.client_name, 200) &&
      ["active", "inactive", "lead"].includes(String(f.client_status)) &&
      ["note", "meeting"].includes(String(f.kind)) &&
      text(f.title, 200) &&
      typeof f.occurred_at === "string" &&
      /Z$/.test(f.occurred_at) &&
      Number.isFinite(Date.parse(f.occurred_at)) &&
      f.activity_state === state &&
      (!bodyChanged || text(f.body, 10000));
    const allowedChanges = deleting
      ? ["reason"]
      : [
          "kind",
          "title",
          "body",
          "occurred_at",
          ...(creating ? ["client_id"] : []),
        ];
    if (
      !record(after, deleting ? "deleted" : "visible") ||
      value.preview.label !== after.title ||
      Object.keys(after).some(
        (key) =>
          ![...previewKeys, ...(deleting ? ["reason"] : [])].includes(key),
      ) ||
      Object.keys(changes).length === 0 ||
      Object.keys(changes).some(
        (key) => !allowedChanges.includes(key) || changes[key] !== after[key],
      ) ||
      (deleting && !text(changes.reason, 1000)) ||
      (creating &&
        (Object.keys(before).length !== 0 ||
          allowedChanges.some((key) => !Object.hasOwn(changes, key)))) ||
      (!creating &&
        (!record(before, "visible") ||
          Object.keys(before).some((key) => !previewKeys.includes(key)) ||
          metadata.some(
            (key) =>
              key !== "activity_state" &&
              !Object.hasOwn(changes, key) &&
              before[key] !== after[key],
          )))
    )
      return invalid();
  }
  if (isFollowup) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const next = value.preview.next_followup;
    const completing = action.action === "client_followup.complete";
    const rescheduling = action.action === "client_followup.reschedule";
    const reasonOnly = [
      "client_followup.cancel",
      "client_followup.skip",
    ].includes(String(action.action));
    const planFields = [
      "assigned_actor_id",
      "scheduled_at",
      "timezone",
      "channel",
      "purpose",
      "notes",
      "priority",
    ];
    const identities = [
      "client_id",
      "client_name",
      "client_status",
      "assigned_actor_name",
      "assigned_actor_type",
      "assigned_actor_status",
    ];
    const previewFields = [...planFields, ...identities, "status"];
    const instant = (v: unknown) =>
      typeof v === "string" && /Z$/.test(v) && Number.isFinite(Date.parse(v));
    const nonempty = (v: unknown, max: number) =>
      typeof v === "string" &&
      v.trim().length > 0 &&
      Array.from(v).length <= max;
    const plan = (f: Record<string, unknown>) =>
      ["client_id", "assigned_actor_id"].every(
        (key) => typeof f[key] === "string" && uuid.test(f[key] as string),
      ) &&
      [
        "client_name",
        "assigned_actor_name",
        "purpose",
        "channel",
        "timezone",
      ].every(
        (key) =>
          typeof f[key] === "string" && (f[key] as string).trim().length > 0,
      ) &&
      ["active", "lead", "inactive"].includes(String(f.client_status)) &&
      ["owner", "person"].includes(String(f.assigned_actor_type)) &&
      ["active", "inactive"].includes(String(f.assigned_actor_status)) &&
      ["low", "normal", "high"].includes(String(f.priority)) &&
      instant(f.scheduled_at) &&
      (f.notes === undefined ||
        f.notes === null ||
        typeof f.notes === "string");
    const expectedStatus = completing
      ? "completed"
      : rescheduling || action.action === "client_followup.cancel"
        ? "cancelled"
        : action.action === "client_followup.skip"
          ? "skipped"
          : "planned";
    const lifecycleFields = completing
      ? ["result", "completed_at", "next_step"]
      : reasonOnly || rescheduling
        ? ["reason"]
        : [];
    if (
      !plan(after) ||
      after.status !== expectedStatus ||
      ((creating || action.action === "client_followup.update") &&
        after.client_status === "inactive") ||
      ((creating ||
        (action.action === "client_followup.update" &&
          Object.hasOwn(changes, "assigned_actor_id"))) &&
        after.assigned_actor_status !== "active") ||
      (creating
        ? Object.keys(before).length !== 0
        : !plan(before) ||
          before.status !== "planned" ||
          before.client_id !== after.client_id) ||
      Object.keys(before).some((key) => !previewFields.includes(key)) ||
      Object.keys(after).some(
        (key) => !previewFields.includes(key) && !lifecycleFields.includes(key),
      ) ||
      Object.keys(changes).length === 0
    )
      return invalid();
    // Existing records retain every unedited plan field. Nested next plans must
    // be shown independently, never silently substituted for the original.
    if (
      !creating &&
      Object.entries(before).some(
        ([key, v]) =>
          key !== "status" &&
          !(
            action.action === "client_followup.update" &&
            (Object.hasOwn(changes, key) ||
              (Object.hasOwn(changes, "assigned_actor_id") &&
                key.startsWith("assigned_actor_")))
          ) &&
          after[key] !== v,
      )
    )
      return invalid();
    if (completing) {
      if (
        Object.keys(changes).some(
          (key) => ![...lifecycleFields, "next_followup"].includes(key),
        ) ||
        !nonempty(after.result, 4000) ||
        !instant(after.completed_at) ||
        !(after.next_step === null || typeof after.next_step === "string") ||
        lifecycleFields.some((key) => after[key] !== changes[key]) ||
        Object.hasOwn(changes, "next_followup") !== (next !== undefined)
      )
        return invalid();
    } else if (reasonOnly) {
      if (
        Object.keys(changes).length !== 1 ||
        !nonempty(changes.reason, 1000) ||
        changes.reason !== after.reason
      )
        return invalid();
    } else if (rescheduling) {
      if (
        !nonempty(changes.reason, 1000) ||
        changes.reason !== after.reason ||
        next === undefined
      )
        return invalid();
    } else {
      if (
        Object.entries(changes).some(
          ([key, v]) =>
            !(planFields.includes(key) || (creating && key === "client_id")) ||
            after[key] !== v,
        ) ||
        (creating &&
          ["client_id", ...planFields].some(
            (key) => !Object.hasOwn(changes, key),
          )) ||
        (!creating &&
          Object.hasOwn(changes, "notes") &&
          !Object.hasOwn(before, "notes"))
      )
        return invalid();
    }
    if (next !== undefined) {
      const rawNext = rescheduling ? changes : changes.next_followup;
      if (
        (!completing && !rescheduling) ||
        !fields(next) ||
        !plan(next) ||
        next.status !== "planned" ||
        next.client_id !== after.client_id ||
        next.client_name !== after.client_name ||
        next.client_status !== after.client_status ||
        next.client_status === "inactive" ||
        next.assigned_actor_status !== "active" ||
        Object.keys(next).some((key) => !previewFields.includes(key)) ||
        !object(rawNext) ||
        planFields.some(
          (key) => !Object.hasOwn(rawNext, key) || rawNext[key] !== next[key],
        ) ||
        Object.keys(rawNext).some(
          (key) =>
            !planFields.includes(key) && !(rescheduling && key === "reason"),
        )
      )
        return invalid();
    }
  }
  if (isFocus) {
    const after = value.preview.after;
    const before = value.preview.before;
    const changes = value.action.changes;
    const starting = action.action === "focus.start";
    const recovering = action.action === "focus.recover";
    const expectedStatus: Record<string, string> = {
      "focus.start": "active",
      "focus.pause": "paused",
      "focus.resume": "active",
      "focus.stop": "completed",
      "focus.cancel": "cancelled",
      "focus.recover":
        changes.recovery_action === "interrupt" ? "interrupted" : "active",
    };
    const beforeStatuses: Record<string, string[]> = {
      "focus.start": ["idle"],
      "focus.pause": ["active"],
      "focus.resume": ["paused"],
      "focus.stop": ["active", "paused"],
      "focus.cancel": ["active", "paused"],
      "focus.recover": ["recovery_pending"],
    };
    if (
      Object.keys(before).length !== 1 ||
      Object.keys(after).length !== (starting ? 5 : recovering ? 7 : 4) ||
      !beforeStatuses[String(action.action)]?.includes(String(before.status)) ||
      after.status !== expectedStatus[String(action.action)] ||
      !(
        after.task_id === null ||
        (typeof after.task_id === "string" && uuid.test(after.task_id))
      ) ||
      !(after.task_title === null || typeof after.task_title === "string") ||
      (after.task_id === null) !== (after.task_title === null) ||
      typeof after.planned_seconds !== "number" ||
      !Number.isSafeInteger(after.planned_seconds) ||
      after.planned_seconds < 300 ||
      after.planned_seconds > 7200
    )
      return invalid();
    if (starting) {
      if (
        Object.keys(changes).some(
          (key) =>
            !["task_id", "expected_task_version", "planned_seconds"].includes(
              key,
            ),
        ) ||
        changes.task_id !== after.task_id ||
        changes.planned_seconds !== after.planned_seconds ||
        (after.task_id === null
          ? after.task_version !== null ||
            Object.hasOwn(changes, "expected_task_version")
          : typeof changes.expected_task_version !== "number" ||
            !Number.isSafeInteger(changes.expected_task_version) ||
            changes.expected_task_version < 1 ||
            after.task_version !== changes.expected_task_version)
      )
        return invalid();
    } else if (recovering) {
      if (
        Object.keys(changes).length !== 1 ||
        !["include_gap_resume", "exclude_gap_resume", "interrupt"].includes(
          String(changes.recovery_action),
        ) ||
        after.recovery_action !== changes.recovery_action ||
        !(
          after.last_heartbeat_at === null ||
          (typeof after.last_heartbeat_at === "string" &&
            /Z$/.test(after.last_heartbeat_at) &&
            Number.isFinite(Date.parse(after.last_heartbeat_at)))
        ) ||
        typeof after.accumulated_seconds !== "number" ||
        !Number.isSafeInteger(after.accumulated_seconds) ||
        after.accumulated_seconds < 0 ||
        after.accumulated_seconds > after.planned_seconds
      )
        return invalid();
    } else if (Object.keys(changes).length !== 0) return invalid();
  }
  if (value.action.action === "inbox.force_resolve") {
    const before = value.preview.before;
    const after = value.preview.after;
    const changes = value.action.changes;
    const counts = [
      "active_task_count",
      "required_task_count",
      "required_done_count",
      "required_remaining_count",
      "required_blocked_count",
      "required_waiting_review_count",
      "required_cancelled_count",
    ];
    if (
      Object.keys(changes).length !== 1 ||
      typeof changes.reason !== "string" ||
      !changes.reason.trim() ||
      after.reason !== changes.reason.trim() ||
      !["open", "tracking"].includes(String(before.status)) ||
      after.status !== "resolved" ||
      before.resolution_policy !== "all_required_tasks_done" ||
      after.resolution_policy !== before.resolution_policy ||
      after.resolution_mode !== "forced" ||
      counts.some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0 ||
          before[key] !== after[key],
      ) ||
      Number(after.required_task_count) > Number(after.active_task_count) ||
      Number(after.required_done_count) +
        Number(after.required_remaining_count) !==
        Number(after.required_task_count) ||
      Number(after.required_blocked_count) +
        Number(after.required_waiting_review_count) +
        Number(after.required_cancelled_count) >
        Number(after.required_remaining_count)
    )
      return invalid();
  }
  if (isReminder) {
    const schedule = (f: Record<string, unknown>) =>
      typeof f.title === "string" &&
      (f.summary === undefined || typeof f.summary === "string") &&
      ["P0", "P1", "P2", "P3"].includes(String(f.priority)) &&
      typeof f.trigger_at === "string" &&
      /Z$/.test(f.trigger_at) &&
      Number.isFinite(Date.parse(f.trigger_at)) &&
      ["none", "daily", "weekly", "weekdays", "monthly"].includes(
        String(f.recurrence_type),
      ) &&
      typeof f.recurrence_timezone === "string" &&
      f.recurrence_timezone.length > 0 &&
      [
        "recurrence_interval",
        "recurrence_anchor_day",
        "occurrence_number",
      ].every(
        (key) =>
          typeof f[key] === "number" &&
          Number.isSafeInteger(f[key]) &&
          Number(f[key]) > 0,
      ) &&
      Number(f.recurrence_interval) <= 365 &&
      Number(f.recurrence_anchor_day) <= 31 &&
      (f.recurrence_type === "monthly" || f.recurrence_anchor_day === 1) &&
      (f.recurrence_type !== "none" ||
        (f.recurrence_interval === 1 && f.recurrence_timezone === "UTC"));
    const after = value.preview.after;
    if (
      !schedule(after) ||
      ((creating || Object.hasOwn(value.action.changes, "summary")) &&
        typeof after.summary !== "string") ||
      (!creating &&
        Object.hasOwn(value.action.changes, "summary") &&
        typeof value.preview.before.summary !== "string") ||
      (!creating &&
        (!schedule(value.preview.before) ||
          value.preview.before.status !== "scheduled")) ||
      after.status !==
        (action.action === "reminder.cancel" ? "cancelled" : "scheduled") ||
      (action.action === "reminder.cancel" &&
        (typeof after.reason !== "string" || !after.reason.trim()))
    )
      return invalid();
  }
  if (value.action.action === "inbox.split") {
    const after = value.preview.after;
    if (
      !splitDrafts(value.preview.tasks, value.action.changes) ||
      after.created_task_count !== value.preview.tasks.length ||
      after.status !== "tracking" ||
      !["manual", "all_required_tasks_done"].includes(
        String(after.resolution_policy),
      ) ||
      ["active_task_count", "required_task_count", "required_done_count"].some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0,
      )
    )
      return invalid();
  } else if (value.preview.tasks !== undefined) return invalid();
  if (
    ["inbox.link_task", "inbox.set_required", "inbox.unlink_task"].includes(
      value.action.action,
    )
  ) {
    const changes = value.action.changes;
    const after = value.preview.after;
    if (
      typeof changes.task_id !== "string" ||
      !uuid.test(changes.task_id) ||
      typeof changes.expected_task_version !== "number" ||
      !Number.isSafeInteger(changes.expected_task_version) ||
      changes.expected_task_version < 1 ||
      after.task_id !== changes.task_id ||
      after.task_version !== changes.expected_task_version ||
      typeof after.task_title !== "string" ||
      typeof after.task_status !== "string" ||
      typeof after.is_required !== "boolean" ||
      !["linked", "unlinked"].includes(String(after.relation_state)) ||
      !["manual", "all_required_tasks_done"].includes(
        String(after.resolution_policy),
      ) ||
      !["open", "tracking", "resolved"].includes(String(after.status)) ||
      [
        "active_task_count",
        "required_task_count",
        "required_done_count",
        "required_remaining_count",
      ].some(
        (key) =>
          typeof after[key] !== "number" ||
          !Number.isSafeInteger(after[key]) ||
          Number(after[key]) < 0,
      )
    )
      return invalid();
  }
  if (
    unexpectedTarget ||
    (creating &&
      (targetId !== undefined || value.action.expected_version !== undefined))
  )
    return invalid();
  if (
    !creating &&
    (typeof targetId !== "string" ||
      !uuid.test(targetId) ||
      typeof value.action.expected_version !== "number" ||
      !Number.isSafeInteger(value.action.expected_version) ||
      value.action.expected_version < 1)
  )
    return invalid();
  if (
    value.action.action === "project.complete" &&
    (typeof value.preview.after.incomplete_task_count !== "number" ||
      !Number.isSafeInteger(value.preview.after.incomplete_task_count) ||
      value.preview.after.incomplete_task_count < 0)
  )
    return invalid();
  if (
    value.result_id !== null &&
    (typeof value.result_id !== "string" || !uuid.test(value.result_id))
  )
    return invalid();
  if (
    value.result_version !== null &&
    (typeof value.result_version !== "number" ||
      !Number.isInteger(value.result_version) ||
      value.result_version < 1)
  )
    return invalid();
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return invalid();
  const result = value as unknown as AiActionProposal;
  if (!validInvoicePdfResult(result)) return invalid();
  const routeId =
    action.action === "invoice.generate_pdf"
      ? targetId
      : isClientContact
        ? action.client_id
        : (result.result_id ?? targetId);
  return {
    ...result,
    route:
      action.action === "project.delete" && result.status === "confirmed"
        ? "/projects"
        : action.action === "client.delete" && result.status === "confirmed"
          ? "/clients"
          : action.action === "content_item.delete" &&
              result.status === "confirmed"
            ? "/content-calendar"
            : action.action === "roadmap_milestone.delete" &&
                result.status === "confirmed"
              ? "/roadmap"
              : action.action === "task.delete" && result.status === "confirmed"
                ? "/tasks"
                : isTag
                  ? "/tasks"
                  : action.action === "invoice.delete" &&
                      result.status === "confirmed"
                    ? "/invoices"
                    : isAutomation
                      ? automationLocationHref(
                          isAutomationRetry ? "run" : "rule",
                          String(routeId),
                        )
                      : isPerson
                        ? ""
                        : isFollowup || isActivity
                          ? routeId
                            ? clientRecordHref(
                                String(result.preview.after.client_id),
                                isFollowup ? "followup" : "activity",
                                String(routeId),
                              )
                            : `/clients/${result.preview.after.client_id}`
                          : routeId
                            ? isFocus
                              ? "/focus"
                              : isReminder
                                ? `/inbox?reminder=${routeId}`
                                : isRoadmap
                                  ? `/roadmap?milestone=${routeId}`
                                  : isContentItem
                                    ? `/content-calendar?item=${routeId}`
                                    : isClient || isClientContact
                                      ? `/clients/${routeId}`
                                      : `/${isInvoice ? "invoices" : isFinancial ? "income" : isInbox ? "inbox" : isProject ? "projects" : "tasks"}/${routeId}`
                            : "",
  };
}

export async function getAiWorkspaceActions(
  generationId: string,
): Promise<AiActionProposal[]> {
  const result = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/generations/${encodeURIComponent(generationId)}/actions`,
  );
  if (!Array.isArray(result.data) || result.data.length > 8) return invalid();
  const items = result.data.map(parseAiActionProposal);
  if (
    items.some((item) => item.generation_id !== generationId) ||
    new Set(items.map((item) => item.id)).size !== items.length
  )
    return invalid();
  return items;
}
