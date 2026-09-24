import { ApiError } from "./client";
import type { AiRunProgress } from "../types/models";

const keys = new Set([
  "sequence",
  "kind",
  "status",
  "turn_index",
  "tool_name",
  "started_at",
  "completed_at",
  "duration_ms",
  "retry_count",
  "retry_reason",
  "trimmed_history_turns",
  "compacted_tool_results",
]);
const kinds = new Set([
  "model_turn",
  "tool_call",
  "self_check",
  "citation_validation",
  "persistence",
]);
const tools = new Set([
  "memory_search",
  "memory_write",
  "memory_propose",
  "workspace_guide",
  "workspace_plan",
  "workspace_search",
  "workspace_get",
  "workspace_outputs",
  "workspace_agent_runs",
  "workspace_agent_execution",
  "workspace_agent_project_files",
  "workspace_inbox_tasks",
  "workspace_inbox_source",
  "workspace_task_options",
  "workspace_task_assignments",
  "workspace_task_submissions",
  "workspace_read_artifact_file",
  "workspace_task_views",
  "workspace_today",
  "workspace_tasks",
  "workspace_projects",
  "workspace_focus",
  "workspace_focus_report",
  "workspace_project_notes",
  "workspace_project_outputs",
  "workspace_content_items",
  "workspace_roadmap_milestones",
  "workspace_client_records",
  "workspace_automations",
  "workspace_finance",
  "workspace_propose",
  "knowledge_search",
  "knowledge_read",
  "knowledge_library",
  "unknown_tool",
]);

function invalid(): never {
  throw new ApiError("运行进度响应无效", { code: "INVALID_RESPONSE" });
}

function timestamp(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.test(
      value,
    ) &&
    Number.isFinite(Date.parse(value))
  );
}

export function parseAiRunProgress(value: unknown): AiRunProgress {
  if (!value || typeof value !== "object" || Array.isArray(value))
    return invalid();
  const item = value as Record<string, unknown>;
  if (
    Object.keys(item).some((key) => !keys.has(key)) ||
    !Number.isSafeInteger(item.sequence) ||
    (item.sequence as number) < 2 ||
    (item.sequence as number) > 64 ||
    typeof item.kind !== "string" ||
    !kinds.has(item.kind) ||
    typeof item.status !== "string" ||
    !["running", "succeeded", "failed", "cancelled"].includes(item.status) ||
    !timestamp(item.started_at) ||
    !Number.isSafeInteger(item.duration_ms) ||
    (item.duration_ms as number) < 0
  )
    return invalid();
  const isModel = item.kind === "model_turn" || item.kind === "self_check";
  if (
    item.trimmed_history_turns !== undefined &&
    (!isModel ||
      !Number.isSafeInteger(item.trimmed_history_turns) ||
      (item.trimmed_history_turns as number) < 1 ||
      (item.trimmed_history_turns as number) > 200)
  )
    return invalid();
  if (
    item.compacted_tool_results !== undefined &&
    (!isModel ||
      !Number.isSafeInteger(item.compacted_tool_results) ||
      (item.compacted_tool_results as number) < 1 ||
      (item.compacted_tool_results as number) > 32)
  )
    return invalid();
  if (
    isModel
      ? !Number.isSafeInteger(item.turn_index) ||
        (item.turn_index as number) < 1 ||
        (item.turn_index as number) > 8
      : item.turn_index !== undefined
  )
    return invalid();
  if (
    item.kind === "tool_call"
      ? typeof item.tool_name !== "string" || !tools.has(item.tool_name)
      : item.tool_name !== undefined
  )
    return invalid();
  if (isModel) {
    if (
      item.retry_count !== undefined &&
      (!Number.isSafeInteger(item.retry_count) ||
        (item.retry_count as number) < 0 ||
        (item.retry_count as number) > 8)
    )
      return invalid();
    if (
      item.retry_reason !== undefined &&
      (typeof item.retry_reason !== "string" ||
        item.retry_reason.length > 80 ||
        ((item.retry_count ?? 0) as number) < 1)
    )
      return invalid();
  } else if (
    item.retry_count !== undefined ||
    item.retry_reason !== undefined
  ) {
    return invalid();
  }
  if (item.status === "running") {
    if (
      item.completed_at !== undefined ||
      item.duration_ms !== 0 ||
      item.kind === "persistence"
    )
      return invalid();
  } else if (
    !timestamp(item.completed_at) ||
    Date.parse(item.completed_at) < Date.parse(item.started_at)
  )
    return invalid();
  // Return only a new, fixed-shape value; no untrusted metadata reaches state.
  return {
    sequence: item.sequence as number,
    kind: item.kind as AiRunProgress["kind"],
    status: item.status as AiRunProgress["status"],
    started_at: item.started_at,
    duration_ms: item.duration_ms as number,
    ...(isModel ? { turn_index: item.turn_index as number } : {}),
    ...(item.kind === "tool_call"
      ? { tool_name: item.tool_name as string }
      : {}),
    ...(isModel && item.retry_count !== undefined
      ? { retry_count: item.retry_count as number }
      : {}),
    ...(isModel && item.retry_reason !== undefined
      ? { retry_reason: item.retry_reason as string }
      : {}),
    ...(isModel && item.trimmed_history_turns !== undefined
      ? { trimmed_history_turns: item.trimmed_history_turns as number }
      : {}),
    ...(isModel && item.compacted_tool_results !== undefined
      ? { compacted_tool_results: item.compacted_tool_results as number }
      : {}),
    ...(item.completed_at !== undefined
      ? { completed_at: item.completed_at as string }
      : {}),
  };
}

export function parseAiRunProgressList(
  value: unknown,
): AiRunProgress[] | undefined {
  if (value === undefined) return undefined; // Older Sidecars do not report progress.
  if (!Array.isArray(value) || value.length > 63) return invalid();
  const result = value.map(parseAiRunProgress);
  result.forEach((step, index) => {
    if (
      step.sequence !== index + 2 ||
      (index < result.length - 1 &&
        (step.status === "running" || step.kind === "persistence"))
    )
      invalid();
  });
  return result;
}

export function appendAiRunProgress(
  steps: AiRunProgress[] | undefined,
  step: AiRunProgress,
): AiRunProgress[] {
  const next = [...(steps ?? [])];
  const index = step.sequence - 2;
  if (index > next.length || index < next.length - 1) return invalid();
  const previous = next[index];
  if (previous) {
    if (
      previous.status !== "running" ||
      step.status === "running" ||
      previous.kind !== step.kind ||
      previous.started_at !== step.started_at ||
      previous.turn_index !== step.turn_index ||
      previous.tool_name !== step.tool_name
    )
      return invalid();
    next[index] = step;
  } else {
    if (
      next.at(-1)?.status === "running" ||
      next.at(-1)?.kind === "persistence"
    )
      return invalid();
    next.push(step);
  }
  return next;
}
