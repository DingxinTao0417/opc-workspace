import type {
  AutomationActionType,
  AutomationConfig,
  AutomationPreview,
  AutomationRule,
  AutomationRuleStatus,
  AutomationRun,
  AutomationRunAttemptSummary,
  AutomationRunDetail,
  AutomationRunListParams,
  AutomationRunListResult,
  AutomationRunSource,
  AutomationRunStatus,
  AutomationTriggerType,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  positiveInteger,
  stringArray,
  stringField,
  validRFC3339Timestamp,
} from "./core";
import { asTaskPriority } from "./tasks";

function asAutomationRuleStatus(value: unknown): AutomationRuleStatus {
  if (value === "enabled" || value === "disabled" || value === "unavailable") {
    return value;
  }
  return invalidResponse("自动化规则状态响应无效");
}

function asAutomationTriggerType(value: unknown): AutomationTriggerType {
  if (value === "event" || value === "schedule") return value;
  return invalidResponse("自动化触发类型响应无效");
}

function asAutomationActionType(value: unknown): AutomationActionType {
  if (value === "inbox_item" || value === "task" || value === "reminder") {
    return value;
  }
  return invalidResponse("自动化动作类型响应无效");
}

function asAutomationRunStatus(value: unknown): AutomationRunStatus {
  if (
    value === "succeeded" ||
    value === "failed" ||
    value === "skipped" ||
    value === "cancelled"
  ) {
    return value;
  }
  return invalidResponse("自动化运行状态响应无效");
}

function normalizeAutomationConfig(value: unknown): AutomationConfig {
  if (!isRecord(value)) return invalidResponse("自动化配置响应格式无效");
  const priority = fieldValue(value, "priority");
  const localTime = fieldValue(value, "local_time", "localTime");
  const timezone = fieldValue(value, "timezone");
  if (priority !== undefined) {
    if (localTime !== undefined || timezone !== undefined) {
      return invalidResponse("自动化配置响应不一致");
    }
    return { priority: asTaskPriority(priority) };
  }
  if (
    typeof localTime !== "string" ||
    !/^\d{2}:\d{2}$/.test(localTime) ||
    typeof timezone !== "string" ||
    !timezone
  ) {
    return invalidResponse("自动化计划配置响应无效");
  }
  return { localTime, timezone };
}

export function normalizeAutomationRule(value: unknown): AutomationRule {
  if (!isRecord(value)) return invalidResponse("自动化规则响应格式无效");
  const id = stringField(value, "id");
  const presetKey = stringField(value, "preset_key", "presetKey");
  const name = stringField(value, "name");
  const description = stringField(value, "description");
  const triggerLabel = stringField(value, "trigger_label", "triggerLabel");
  const actionLabel = stringField(value, "action_label", "actionLabel");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const available = value.available;
  const unavailableReason = fieldValue(
    value,
    "unavailable_reason",
    "unavailableReason",
  );
  const nextRunAt = nullableString(
    fieldValue(value, "next_run_at", "nextRunAt"),
  );
  const status = asAutomationRuleStatus(value.status);
  const triggerType = asAutomationTriggerType(
    fieldValue(value, "trigger_type", "triggerType"),
  );
  if (
    !id ||
    !presetKey ||
    !name ||
    !description ||
    !triggerLabel ||
    !actionLabel ||
    !createdAt ||
    !updatedAt ||
    typeof available !== "boolean" ||
    (unavailableReason !== undefined &&
      typeof unavailableReason !== "string") ||
    (status === "unavailable") !== !available ||
    (status === "enabled" && !available) ||
    (triggerType === "event" && nextRunAt !== null)
  ) {
    return invalidResponse("自动化规则响应不一致");
  }
  return {
    id,
    presetKey,
    name,
    description,
    status,
    available,
    unavailableReason:
      typeof unavailableReason === "string" ? unavailableReason : "",
    triggerType,
    triggerLabel,
    actionType: asAutomationActionType(
      fieldValue(value, "action_type", "actionType"),
    ),
    actionLabel,
    config: normalizeAutomationConfig(value.config),
    nextRunAt,
    permissions: stringArray(value.permissions, "自动化权限"),
    version: positiveInteger(value.version, "自动化规则版本"),
    createdAt,
    updatedAt,
  };
}

export function normalizeAutomationPreview(value: unknown): AutomationPreview {
  if (!isRecord(value)) return invalidResponse("自动化预览响应格式无效");
  const triggerSummary = stringField(
    value,
    "trigger_summary",
    "triggerSummary",
  );
  const actionSummary = stringField(value, "action_summary", "actionSummary");
  const unavailableReason = fieldValue(
    value,
    "unavailable_reason",
    "unavailableReason",
  );
  if (
    typeof value.can_enable !== "boolean" ||
    !triggerSummary ||
    !actionSummary ||
    (unavailableReason !== undefined && typeof unavailableReason !== "string")
  ) {
    return invalidResponse("自动化预览响应不一致");
  }
  return {
    canEnable: value.can_enable,
    unavailableReason:
      typeof unavailableReason === "string" ? unavailableReason : "",
    triggerSummary,
    actionSummary,
    config: normalizeAutomationConfig(value.config),
    nextRunAt: nullableString(fieldValue(value, "next_run_at", "nextRunAt")),
    permissions: stringArray(value.permissions, "自动化权限"),
  };
}

export function normalizeAutomationRun(value: unknown): AutomationRun {
  if (!isRecord(value)) return invalidResponse("自动化运行响应格式无效");
  const resultSummary = fieldValue(value, "result_summary", "resultSummary");
  const requiredStrings = {
    id: stringField(value, "id"),
    ruleId: stringField(value, "rule_id", "ruleId"),
    presetKey: stringField(value, "preset_key", "presetKey"),
    ruleName: stringField(value, "rule_name", "ruleName"),
    startedAt: stringField(value, "started_at", "startedAt"),
    endedAt: stringField(value, "ended_at", "endedAt"),
  };
  if (
    Object.values(requiredStrings).some((item) => !item) ||
    typeof resultSummary !== "string"
  ) {
    return invalidResponse("自动化运行响应缺少必要字段");
  }
  const triggerType = asAutomationTriggerType(
    fieldValue(value, "trigger_type", "triggerType"),
  );
  const sourceEventId = nullableString(
    fieldValue(value, "source_event_id", "sourceEventId"),
  );
  const scheduledFor = nullableString(
    fieldValue(value, "scheduled_for", "scheduledFor"),
  );
  const retryable = value.retryable;
  const configSnapshot = fieldValue(value, "config_snapshot", "configSnapshot");
  const actionSnapshot = fieldValue(value, "action_snapshot", "actionSnapshot");
  const attempt = positiveInteger(value.attempt, "自动化运行次数");
  const causalDepth = nonNegativeInteger(
    fieldValue(value, "causal_depth", "causalDepth"),
    "自动化因果深度",
  );
  if (
    typeof retryable !== "boolean" ||
    !isRecord(configSnapshot) ||
    !isRecord(actionSnapshot) ||
    attempt > 3 ||
    causalDepth > 4 ||
    (triggerType === "event" && (!sourceEventId || scheduledFor !== null)) ||
    (triggerType === "schedule" && (sourceEventId !== null || !scheduledFor))
  ) {
    return invalidResponse("自动化运行响应不一致");
  }
  return {
    id: requiredStrings.id!,
    ruleId: requiredStrings.ruleId!,
    presetKey: requiredStrings.presetKey!,
    ruleName: requiredStrings.ruleName!,
    ruleVersion: positiveInteger(
      fieldValue(value, "rule_version", "ruleVersion"),
      "自动化规则版本",
    ),
    triggerType,
    sourceEventId,
    scheduledFor,
    status: asAutomationRunStatus(value.status),
    attempt,
    retryOfRunId: nullableString(
      fieldValue(value, "retry_of_run_id", "retryOfRunId"),
    ),
    retryable,
    retryAt: nullableString(fieldValue(value, "retry_at", "retryAt")),
    causedByRunId: nullableString(
      fieldValue(value, "caused_by_run_id", "causedByRunId"),
    ),
    causalDepth,
    configSnapshot: { ...configSnapshot },
    actionSnapshot: { ...actionSnapshot },
    errorCode: nullableString(fieldValue(value, "error_code", "errorCode")),
    resultType: nullableString(fieldValue(value, "result_type", "resultType")),
    resultId: nullableString(fieldValue(value, "result_id", "resultId")),
    resultSummary,
    startedAt: requiredStrings.startedAt!,
    endedAt: requiredStrings.endedAt!,
  };
}

const automationRunSourceKeys = [
  "kind",
  "available",
  "event_id",
  "aggregate_type",
  "aggregate_id",
  "action",
  "occurred_at",
  "scheduled_for",
] as const;

const automationRunAttemptSummaryKeys = [
  "id",
  "status",
  "attempt",
  "retry_of_run_id",
  "retryable",
  "retry_at",
  "error_code",
  "result_type",
  "result_id",
  "result_summary",
  "started_at",
  "ended_at",
] as const;

function normalizeAutomationRunSource(
  value: unknown,
  run: AutomationRun,
): AutomationRunSource {
  if (!isRecord(value) || !hasExactKeys(value, automationRunSourceKeys)) {
    return invalidResponse("自动化运行来源响应格式无效");
  }
  const source: AutomationRunSource = {
    kind: asAutomationTriggerType(value.kind),
    available: value.available as boolean,
    eventId: nullableString(value.event_id),
    aggregateType: nullableString(value.aggregate_type),
    aggregateId: nullableString(value.aggregate_id),
    action: nullableString(value.action),
    occurredAt: nullableString(value.occurred_at),
    scheduledFor: nullableString(value.scheduled_for),
  };
  if (typeof value.available !== "boolean" || source.kind !== run.triggerType) {
    return invalidResponse("自动化运行来源响应不一致");
  }
  if (source.kind === "schedule") {
    if (
      !source.available ||
      source.eventId !== null ||
      source.aggregateType !== null ||
      source.aggregateId !== null ||
      source.action !== null ||
      source.occurredAt !== null ||
      source.scheduledFor !== run.scheduledFor ||
      !validRFC3339Timestamp(source.scheduledFor)
    ) {
      return invalidResponse("自动化计划运行来源响应不一致");
    }
    return source;
  }
  if (
    source.eventId !== run.sourceEventId ||
    source.scheduledFor !== null ||
    (source.available
      ? !source.aggregateType ||
        !source.aggregateId ||
        !source.action ||
        !validRFC3339Timestamp(source.occurredAt)
      : source.aggregateType !== null ||
        source.aggregateId !== null ||
        source.action !== null ||
        source.occurredAt !== null)
  ) {
    return invalidResponse("自动化事件运行来源响应不一致");
  }
  return source;
}

function normalizeAutomationRunAttemptSummary(
  value: unknown,
): AutomationRunAttemptSummary {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, automationRunAttemptSummaryKeys)
  ) {
    return invalidResponse("自动化重试摘要响应格式无效");
  }
  const id = stringField(value, "id");
  const resultSummary = value.result_summary;
  const startedAt = stringField(value, "started_at");
  const endedAt = stringField(value, "ended_at");
  const attempt = positiveInteger(value.attempt, "自动化重试次数");
  const resultType = nullableString(value.result_type);
  if (
    !id ||
    typeof resultSummary !== "string" ||
    !startedAt ||
    !endedAt ||
    attempt > 3 ||
    typeof value.retryable !== "boolean"
  ) {
    return invalidResponse("自动化重试摘要响应不一致");
  }
  return {
    id,
    status: asAutomationRunStatus(value.status),
    attempt,
    retryOfRunId: nullableString(value.retry_of_run_id),
    retryable: value.retryable,
    retryAt: nullableString(value.retry_at),
    errorCode: nullableString(value.error_code),
    resultType,
    resultId: nullableString(value.result_id),
    resultSummary,
    startedAt,
    endedAt,
  };
}

function automationRunMatchesAttemptSummary(
  run: AutomationRun,
  summary: AutomationRunAttemptSummary,
): boolean {
  return (
    summary.id === run.id &&
    summary.status === run.status &&
    summary.attempt === run.attempt &&
    summary.retryOfRunId === run.retryOfRunId &&
    summary.retryable === run.retryable &&
    summary.retryAt === run.retryAt &&
    summary.errorCode === run.errorCode &&
    summary.resultType === run.resultType &&
    summary.resultId === run.resultId &&
    summary.resultSummary === run.resultSummary &&
    summary.startedAt === run.startedAt &&
    summary.endedAt === run.endedAt
  );
}

export function normalizeAutomationRunDetail(
  value: unknown,
): AutomationRunDetail {
  if (
    !isRecord(value) ||
    !Array.isArray(fieldValue(value, "retry_chain", "retryChain"))
  ) {
    return invalidResponse("自动化运行详情响应格式无效");
  }
  const run = normalizeAutomationRun(value);
  const source = normalizeAutomationRunSource(value.source, run);
  const retryChain = (
    fieldValue(value, "retry_chain", "retryChain") as unknown[]
  ).map(normalizeAutomationRunAttemptSummary);
  if (
    retryChain.length < 1 ||
    retryChain.length > 3 ||
    retryChain.some(
      (attempt, index) =>
        index > 0 && attempt.attempt <= retryChain[index - 1].attempt,
    ) ||
    new Set(retryChain.map((attempt) => attempt.id)).size !==
      retryChain.length ||
    !retryChain.some((attempt) =>
      automationRunMatchesAttemptSummary(run, attempt),
    )
  ) {
    return invalidResponse("自动化运行重试链响应不一致");
  }
  return { ...run, source, retryChain };
}

export function normalizeAutomationRunListResult(
  value: unknown,
): AutomationRunListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("自动化运行列表响应格式无效");
  }
  const items = value.data.map(normalizeAutomationRun);
  const result = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "自动化运行页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "自动化运行分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "自动化运行总数"),
    },
  };
  if (items.length > result.meta.pageSize || result.meta.total < items.length) {
    return invalidResponse("自动化运行分页响应不一致");
  }
  return result;
}

function automationConfigPayload(
  config: AutomationConfig,
): Record<string, string> {
  if (config.priority) return { priority: config.priority };
  return {
    local_time: config.localTime ?? "",
    timezone: config.timezone ?? "",
  };
}

export async function getAutomationRules(
  signal?: AbortSignal,
): Promise<AutomationRule[]> {
  const payload = await apiRequest<unknown>("/api/v1/automations/rules", {
    signal,
  });
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("自动化规则列表响应格式无效");
  }
  return payload.data.map(normalizeAutomationRule);
}

export async function getAutomationRule(id: string): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

export async function previewAutomationRule(
  id: string,
  config: AutomationConfig,
): Promise<AutomationPreview> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}/preview`,
    {
      method: "POST",
      body: JSON.stringify({ config: automationConfigPayload(config) }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationPreview(body);
}

export async function updateAutomationRule(
  id: string,
  config: AutomationConfig,
  expectedVersion: number,
): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ config: automationConfigPayload(config) }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

async function changeAutomationRuleEnabled(
  id: string,
  action: "enable" | "disable",
  expectedVersion: number,
): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}/${action}`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

export function enableAutomationRule(
  id: string,
  expectedVersion: number,
): Promise<AutomationRule> {
  return changeAutomationRuleEnabled(id, "enable", expectedVersion);
}

export function disableAutomationRule(
  id: string,
  expectedVersion: number,
): Promise<AutomationRule> {
  return changeAutomationRuleEnabled(id, "disable", expectedVersion);
}

export async function getAutomationRuns(
  input: AutomationRunListParams = {},
  signal?: AbortSignal,
): Promise<AutomationRunListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.ruleId) params.set("rule_id", input.ruleId);
  if (input.status) params.set("status", input.status);
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs?${params}`,
    { signal },
  );
  return normalizeAutomationRunListResult(payload);
}

export async function getAutomationRun(
  id: string,
  signal?: AbortSignal,
): Promise<AutomationRunDetail> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const detail = normalizeAutomationRunDetail(body);
  if (detail.id !== id) {
    return invalidResponse("自动化运行详情标识响应不一致");
  }
  return detail;
}

export async function retryAutomationRun(id: string): Promise<AutomationRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs/${encodeURIComponent(id)}/retry`,
    { method: "POST" },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRun(body);
}
