export const agentRunFailureRuleId = "00000000-0000-5000-8000-000000000105";

const safeErrorCodes = new Set([
  "AGENT_EXECUTOR_UNAVAILABLE",
  "AGENT_EXECUTOR_FAILED",
  "AGENT_PROTOCOL_INVALID",
  "AGENT_RESULT_TOO_LARGE",
  "AGENT_RUN_TIMED_OUT",
  "AGENT_EXECUTOR_INVALID_INPUT",
  "AGENT_MODEL_ENDPOINT_INVALID",
  "AGENT_MODEL_UNAVAILABLE",
  "AGENT_MODEL_FAILED",
  "AGENT_MODEL_TRUNCATED",
  "AGENT_MODEL_FILTERED",
  "AGENT_MODEL_RESPONSE_INVALID",
  "AGENT_RESULT_EMPTY",
  "AGENT_RESULT_INVALID",
  "AGENT_RUN_IDENTITY_CHANGED",
  "AGENT_MODEL_KEY_UNAVAILABLE",
  "AGENT_RUN_FAILED",
]);

/** Fixed explanations of failure codes, never Provider messages or content. */
export function agentRunFailureReason(errorCode: string): string | null {
  switch (errorCode) {
    case "AGENT_MODEL_TRUNCATED":
      return "模型输出未完整结束，未登记产出。";
    case "AGENT_MODEL_FILTERED":
      return "模型拒绝或过滤了输出，未登记产出。";
    case "AGENT_MODEL_RESPONSE_INVALID":
      return "模型响应不符合完整文本交付协议，未登记产出。";
    case "AGENT_EXECUTOR_UNAVAILABLE":
      return "执行器当前不可用，未登记产出。";
    case "AGENT_EXECUTOR_FAILED":
      return "执行器未正常完成，未登记产出。";
    case "AGENT_PROTOCOL_INVALID":
      return "执行器通信协议无效，未登记产出。";
    case "AGENT_RESULT_TOO_LARGE":
      return "输出超过允许大小，未登记产出。";
    case "AGENT_RUN_TIMED_OUT":
      return "执行超时，未登记产出。";
    case "AGENT_EXECUTOR_INVALID_INPUT":
      return "执行输入不符合冻结契约，未登记产出。";
    case "AGENT_MODEL_ENDPOINT_INVALID":
      return "模型服务地址不符合执行要求，未登记产出。";
    case "AGENT_MODEL_UNAVAILABLE":
      return "模型服务当前不可用，未登记产出。";
    case "AGENT_MODEL_FAILED":
      return "模型请求未成功，未登记产出。";
    case "AGENT_RESULT_EMPTY":
      return "模型未返回可用正文，未登记产出。";
    case "AGENT_RESULT_INVALID":
      return "输出不符合约定格式，未登记产出。";
    case "AGENT_RUN_IDENTITY_CHANGED":
      return "任务、责任或执行配置已变化，未登记产出；需重新核对当前事实。";
    case "AGENT_MODEL_KEY_UNAVAILABLE":
      return "模型所需凭据不可用，未登记产出。";
    case "AGENT_RUN_FAILED":
      return "执行未成功，未登记产出；请查看执行记录并人工判断下一步。";
    default:
      return null;
  }
}
const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const uuid = (value: unknown): value is string =>
  typeof value === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
    value,
  );
const exact = (value: Record<string, unknown>, fields: string[]) =>
  Object.keys(value).length === fields.length &&
  fields.every((key) => Object.hasOwn(value, key));

function validFailedAt(value: unknown): value is string {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$/.test(value)
  )
    return false;
  // Date drops sub-millisecond precision; compare the retained part to reject
  // normalization of impossible dates/times without changing the source value.
  const milliseconds = `${value.slice(0, 23)}Z`;
  if (Number(value.slice(0, 4)) < 1) return false;
  const parsed = new Date(milliseconds);
  return (
    Number.isFinite(parsed.getTime()) && parsed.toISOString() === milliseconds
  );
}

function validFailureFacts(value: Record<string, unknown>): boolean {
  return (
    uuid(value.agent_run_id) &&
    uuid(value.task_id) &&
    typeof value.attempt === "number" &&
    Number.isSafeInteger(value.attempt) &&
    value.attempt >= 1 &&
    typeof value.error_code === "string" &&
    safeErrorCodes.has(value.error_code) &&
    validFailedAt(value.failed_at)
  );
}

export type AgentRunFailurePayload = {
  agent_run_id: string;
  task_id: string;
  attempt: number;
  error_code: string;
  failed_at: string;
  automation_rule_id: string;
  automation_run_id: string;
  source_event_id: string;
};

// This proves snapshot self-consistency only. The destination must read the Run
// and validate its current Task ownership; no output is fetched by this parser.
export function parseAgentRunFailurePayload(
  value: unknown,
): AgentRunFailurePayload | null {
  if (
    !record(value) ||
    !exact(value, [
      "agent_run_id",
      "task_id",
      "attempt",
      "error_code",
      "failed_at",
      "automation_rule_id",
      "automation_run_id",
      "source_event_id",
    ]) ||
    !validFailureFacts(value) ||
    value.automation_rule_id !== agentRunFailureRuleId ||
    !uuid(value.automation_run_id) ||
    !uuid(value.source_event_id)
  )
    return null;
  return value as AgentRunFailurePayload;
}

export function validAgentRunFailureAction(value: unknown): boolean {
  return (
    record(value) &&
    exact(value, [
      "action_type",
      "agent_run_id",
      "task_id",
      "attempt",
      "error_code",
      "failed_at",
      "priority",
    ]) &&
    value.action_type === "inbox_item" &&
    validFailureFacts(value) &&
    typeof value.priority === "string" &&
    ["P0", "P1", "P2", "P3"].includes(value.priority)
  );
}
