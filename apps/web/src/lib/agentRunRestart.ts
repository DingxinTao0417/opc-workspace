import type {
  AgentRun,
  AgentRunRestartRequest,
  AgentRunRestartSource,
} from "../types/models";
import { isWorkspaceIdentity } from "./focusReportLocation";
const record = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);
const exact = (value: Record<string, unknown>, keys: string[]) =>
  Object.keys(value).length === keys.length &&
  keys.every((key) => Object.hasOwn(value, key));
const id = (value: unknown): value is string =>
  typeof value === "string" && isWorkspaceIdentity(value);
const positive = (value: unknown) =>
  Number.isSafeInteger(value) && Number(value) > 0;
export function canRestartAgentRun(
  run: Pick<AgentRun, "status" | "outputDeliveryStatus">,
) {
  return (
    (run.status === "succeeded" && run.outputDeliveryStatus === "retained") ||
    (["failed", "cancelled", "interrupted"].includes(run.status) &&
      run.outputDeliveryStatus === "not_ready")
  );
}
export function validAgentRunRestartRequest(
  value: unknown,
): value is AgentRunRestartRequest {
  return (
    record(value) &&
    exact(value, ["runId", "expectedTaskVersion"]) &&
    id(value.runId) &&
    positive(value.expectedTaskVersion)
  );
}
export function validAgentRunRestartSource(
  value: unknown,
): value is AgentRunRestartSource {
  return (
    record(value) &&
    exact(value, [
      "run_id",
      "task_id",
      "status",
      "output_delivery_status",
      "attempt",
      "task_version",
      "actor_id",
      "provider_id",
      "model",
      "completed_at",
    ]) &&
    [value.run_id, value.task_id, value.actor_id, value.provider_id].every(
      id,
    ) &&
    positive(value.attempt) &&
    positive(value.task_version) &&
    ((value.status === "succeeded" &&
      value.output_delivery_status === "retained") ||
      (["failed", "cancelled", "interrupted"].includes(String(value.status)) &&
        value.output_delivery_status === "not_ready")) &&
    typeof value.model === "string" &&
    !!value.model.trim() &&
    new TextEncoder().encode(value.model).length <= 255 &&
    !/[\u0000\uD800-\uDFFF]/u.test(value.model) &&
    typeof value.completed_at === "string" &&
    /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(
      value.completed_at,
    ) &&
    Number.isFinite(Date.parse(value.completed_at)) &&
    new Date(value.completed_at).toISOString().slice(0, 19) ===
      value.completed_at.slice(0, 19)
  );
}
export function sameAgentRestartFacts(left: unknown, right: unknown): boolean {
  const canonical = (value: unknown): unknown =>
    Array.isArray(value)
      ? value.map(canonical)
      : record(value)
        ? Object.fromEntries(
            Object.keys(value)
              .sort()
              .map((key) => [key, canonical(value[key])]),
          )
        : value;
  return JSON.stringify(canonical(left)) === JSON.stringify(canonical(right));
}
export function agentRestartErrorMessage(error: unknown): string | null {
  if (!record(error)) return null;
  switch (error.code) {
    case "AGENT_RUN_RESTART_INVALID":
      return "原执行不可作为新执行来源，或来源已变化；请重新读取原运行。活动、待登记和已提交的运行不能使用此入口。";
    case "AGENT_RUN_IDENTITY_CHANGED":
    case "VERSION_CONFLICT":
      return "任务、分派、模型或资料已变化；本次未启动，请重新预览全部当前事实并确认。";
    case "CONFIRMATION_REQUIRED":
      return "请重新预览当前执行快照，并单独确认按当前事实重新执行。";
    default:
      return null;
  }
}
