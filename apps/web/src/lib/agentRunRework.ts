import type {
  AgentRunReworkContext,
  AgentRunReworkRequest,
} from "../types/models";
import { isWorkspaceIdentity } from "./focusReportLocation";

const record = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === "object" && !Array.isArray(value);
const keys = (value: Record<string, unknown>, expected: string[]) =>
  Object.keys(value).length === expected.length &&
  expected.every((key) => Object.hasOwn(value, key));
const text = (value: unknown): value is string =>
  typeof value === "string" && !/[\u0000\uD800-\uDFFF]/u.test(value);

export function agentReworkErrorMessage(error: unknown): string | null {
  if (!record(error)) return null;
  switch (error.code) {
    case "AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED":
      return "请独立核对同项目其他任务的已验收文件来源，并另行同意跨任务使用；原文件正文发送确认不能代替这项确认。";
    case "AGENT_RUN_REWORK_INVALID":
      return "返工来源已变化、类型不受支持，或完整意见与所选旧稿超过 64 KiB；请刷新当前退回批次并减少旧稿选择，不会截断发送。";
    case "AGENT_REWORK_CONFIRMATION_REQUIRED":
      return "请重新完整预览返工意见与旧稿，并独立确认发送给当前 Provider。";
    case "CONFIRMATION_REQUIRED":
      return "当前 Provider 或发送范围需要重新确认；返工上下文与受控文件必须分别同意。";
    case "AGENT_RUN_NOT_RETRYABLE":
      return "当前执行不能重试；已成功或待登记的执行不会再次调用模型，请查看执行记录或恢复登记。";
    default:
      return null;
  }
}

export function reworkEncodedBytes(value: unknown): number {
  // Go's JSON encoder escapes these characters; count the actual wire bound.
  return new TextEncoder().encode(
    JSON.stringify(value).replace(
      /[<>&\u2028\u2029]/g,
      (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, "0")}`,
    ),
  ).length;
}

export function validAgentRunReworkRequest(
  value: unknown,
): value is AgentRunReworkRequest {
  return (
    record(value) &&
    keys(value, ["submissionId", "artifactIds", "expectedTaskVersion"]) &&
    typeof value.submissionId === "string" &&
    isWorkspaceIdentity(value.submissionId) &&
    Number.isSafeInteger(value.expectedTaskVersion) &&
    Number(value.expectedTaskVersion) > 0 &&
    Array.isArray(value.artifactIds) &&
    value.artifactIds.length <= 4 &&
    value.artifactIds.every(
      (id) => typeof id === "string" && isWorkspaceIdentity(id),
    ) &&
    new Set(value.artifactIds).size === value.artifactIds.length
  );
}

export function validAgentRunReworkContext(
  value: unknown,
): value is AgentRunReworkContext {
  if (
    !record(value) ||
    !keys(value, [
      "submission_id",
      "sequence",
      "review_reason",
      "reviewed_at",
      "artifacts",
    ]) ||
    typeof value.submission_id !== "string" ||
    !isWorkspaceIdentity(value.submission_id) ||
    !Number.isSafeInteger(value.sequence) ||
    Number(value.sequence) < 1 ||
    !text(value.review_reason) ||
    !value.review_reason.trim() ||
    typeof value.reviewed_at !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(
      value.reviewed_at,
    ) ||
    !Number.isFinite(Date.parse(value.reviewed_at)) ||
    !Array.isArray(value.artifacts) ||
    value.artifacts.length > 4
  )
    return false;
  if (
    !value.artifacts.every(
      (item) =>
        record(item) &&
        keys(item, ["id", "storage_kind", "name", "content", "sha256"]) &&
        typeof item.id === "string" &&
        isWorkspaceIdentity(item.id) &&
        ["text", "link", "structured"].includes(String(item.storage_kind)) &&
        text(item.name) &&
        !!item.name.trim() &&
        text(item.content) &&
        typeof item.sha256 === "string" &&
        /^[0-9a-f]{64}$/.test(item.sha256),
    )
  )
    return false;
  return (
    new Set(value.artifacts.map((item) => item.id)).size ===
      value.artifacts.length && reworkEncodedBytes(value) <= 65_536
  );
}
