import {
  agentRunFailureRuleId,
  validAgentRunFailureAction,
} from "../lib/agentRunFailureSource";

export interface AiAutomationRetryPreview {
  run_id: string;
  rule_id: string;
  rule_version: number;
  current_rule_version: number;
  rule_enabled: boolean;
  trigger_type: "event" | "schedule";
  action_type: "inbox_item" | "task" | "reminder";
  permissions: string[];
  attempt: number;
  next_attempt: number;
  scheduled_for: string | null;
  error_code: string | null;
  config: Record<string, string>;
  action: Record<string, string | number | null>;
}
export interface AiAutomationRetryResult {
  id: string;
  rule_id: string;
  rule_version: number;
  status: "succeeded" | "failed";
  attempt: number;
  retryable: boolean;
  retry_at: string | null;
  error_code: string | null;
  result_type: "inbox_item" | "task" | "reminder" | null;
  result_id: string | null;
}
const object = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);
const id = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v);
const positive = (v: unknown): v is number =>
  typeof v === "number" && Number.isSafeInteger(v) && v > 0;
const text = (v: unknown): v is string => typeof v === "string" && v.length > 0;
const date = (v: unknown) =>
  typeof v === "string" && /T.*Z$/.test(v) && Number.isFinite(Date.parse(v));
const exact = (v: Record<string, unknown>, keys: string[]) =>
  Object.keys(v).length === keys.length &&
  keys.every((key) => Object.hasOwn(v, key));
const errorCode = (v: unknown, allowMissingAgentSource = false) =>
  v === null ||
  (allowMissingAgentSource && v === "SOURCE_UNAVAILABLE") ||
  [
    "SCHEDULE_WINDOW_EXPIRED",
    "ACTION_WRITE_FAILED",
    "SOURCE_EVENT_CONFLICT",
    "ACTION_SNAPSHOT_INVALID",
    "ATTEMPT_CONTRACT_INVALID",
    "SOURCE_EVENT_INVALID",
    "UNKNOWN_ERROR",
  ].includes(String(v));

export function validAutomationRetryProposal(
  value: Record<string, unknown>,
): boolean {
  if (!object(value.action) || !object(value.preview)) return false;
  const a = value.action;
  const p = value.preview.automation_retry;
  if (
    !object(p) ||
    !exact(p, [
      "run_id",
      "rule_id",
      "rule_version",
      "current_rule_version",
      "rule_enabled",
      "trigger_type",
      "action_type",
      "permissions",
      "attempt",
      "next_attempt",
      "scheduled_for",
      "error_code",
      "config",
      "action",
    ]) ||
    !object(p.config) ||
    !object(p.action) ||
    !object(a.changes) ||
    Object.keys(a.changes).length ||
    !object(value.preview.before) ||
    Object.keys(value.preview.before).length ||
    !object(value.preview.after) ||
    Object.keys(value.preview.after).length ||
    !id(p.run_id) ||
    p.run_id !== a.automation_run_id ||
    !id(p.rule_id) ||
    !positive(p.rule_version) ||
    p.rule_version !== a.expected_version ||
    !positive(p.current_rule_version) ||
    p.current_rule_version < p.rule_version ||
    typeof p.rule_enabled !== "boolean" ||
    !positive(p.attempt) ||
    p.attempt > 2 ||
    p.next_attempt !== p.attempt + 1 ||
    !Array.isArray(p.permissions) ||
    p.permissions.length === 0 ||
    !p.permissions.every(text) ||
    !errorCode(p.error_code) ||
    p.error_code === null
  )
    return false;
  const kinds: Record<string, [string, string]> = {
    "00000000-0000-5000-8000-000000000101": ["event", "inbox_item"],
    "00000000-0000-5000-8000-000000000102": ["schedule", "reminder"],
    "00000000-0000-5000-8000-000000000103": ["schedule", "reminder"],
    "00000000-0000-5000-8000-000000000104": ["event", "task"],
    [agentRunFailureRuleId]: ["event", "inbox_item"],
  };
  const kind = kinds[p.rule_id];
  if (
    !kind ||
    p.trigger_type !== kind[0] ||
    p.action_type !== kind[1] ||
    p.action.action_type !== kind[1] ||
    (p.rule_id !== agentRunFailureRuleId &&
      (!text(p.action.title) ||
        !Object.values(p.action).every(
          (v) => v === null || typeof v === "string",
        )))
  )
    return false;
  if (p.trigger_type === "schedule") {
    if (
      !p.rule_enabled ||
      !date(p.scheduled_for) ||
      !exact(p.config, ["local_time", "timezone"]) ||
      typeof p.config.local_time !== "string" ||
      !/^([01]\d|2[0-3]):[0-5]\d$/.test(p.config.local_time) ||
      !text(p.config.timezone) ||
      p.config.timezone === "Local" ||
      !exact(p.action, ["action_type", "title", "summary", "priority"]) ||
      typeof p.action.summary !== "string" ||
      p.action.priority !== "P2"
    )
      return false;
    try {
      new Intl.DateTimeFormat("en", { timeZone: p.config.timezone });
    } catch {
      return false;
    }
  } else {
    if (
      p.scheduled_for !== null ||
      !exact(p.config, ["priority"]) ||
      !["P0", "P1", "P2", "P3"].includes(String(p.config.priority)) ||
      p.action.priority !== p.config.priority
    )
      return false;
    if (p.rule_id === agentRunFailureRuleId) {
      if (!validAgentRunFailureAction(p.action)) return false;
    } else if (p.action_type === "inbox_item") {
      if (
        !exact(p.action, [
          "action_type",
          "title",
          "project_id",
          "project_name",
          "priority",
        ]) ||
        !id(p.action.project_id) ||
        !text(p.action.project_name)
      )
        return false;
    } else if (
      !exact(p.action, [
        "action_type",
        "title",
        "invoice_id",
        "invoice_number",
        "project_id",
        "description",
        "kind",
        "status",
        "review_policy",
        "priority",
        "due_date",
        "planned_date",
      ]) ||
      !id(p.action.invoice_id) ||
      !text(p.action.invoice_number) ||
      (p.action.project_id !== null && !id(p.action.project_id)) ||
      typeof p.action.description !== "string" ||
      p.action.kind !== "followup" ||
      p.action.status !== "todo" ||
      p.action.review_policy !== "none" ||
      p.action.due_date !== null ||
      p.action.planned_date !== null
    )
      return false;
  }
  if (value.status !== "confirmed")
    return (
      value.automation_run_result === undefined &&
      value.result_id === null &&
      value.result_version === null
    );
  const r = value.automation_run_result;
  if (
    !object(r) ||
    !exact(r, [
      "id",
      "rule_id",
      "rule_version",
      "status",
      "attempt",
      "retryable",
      "retry_at",
      "error_code",
      "result_type",
      "result_id",
    ]) ||
    !id(r.id) ||
    r.id === p.run_id ||
    r.id !== value.result_id ||
    r.rule_id !== p.rule_id ||
    r.rule_version !== p.rule_version ||
    r.rule_version !== value.result_version ||
    r.attempt !== p.next_attempt ||
    typeof r.retryable !== "boolean" ||
    (r.error_code === "SOURCE_UNAVAILABLE" && r.retryable) ||
    !errorCode(r.error_code, p.rule_id === agentRunFailureRuleId)
  )
    return false;
  if (r.status === "succeeded")
    return (
      !r.retryable &&
      r.retry_at === null &&
      r.error_code === null &&
      r.result_type === p.action_type &&
      id(r.result_id)
    );
  return (
    r.status === "failed" &&
    r.error_code !== null &&
    r.result_id === null &&
    r.result_type === null &&
    (r.retryable ? r.attempt === 2 && date(r.retry_at) : r.retry_at === null)
  );
}

export function automationRetryResultRoute(
  result?: AiAutomationRetryResult,
): string {
  if (result?.status !== "succeeded" || !id(result.result_id)) return "";
  if (result.result_type === "reminder")
    return `/inbox?reminder=${result.result_id}`;
  if (result.result_type === "task") return `/tasks/${result.result_id}`;
  if (result.result_type === "inbox_item") return `/inbox/${result.result_id}`;
  return "";
}
