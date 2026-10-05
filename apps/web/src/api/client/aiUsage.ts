import type {
  AiProviderKind,
  AiProviderProtocol,
  AiRunStep,
  AiRunStepListResult,
  AiUsageSummary,
  AiUsageTotals,
  AiUsageTrendPoint,
} from "../../types/models";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableNonNegativeInteger,
  nullableString,
  positiveInteger,
  stringField,
} from "./core";

function aiRunStepFromRecord(row: unknown): AiRunStep {
  if (!isRecord(row)) return invalidResponse("AI 运行步骤响应格式无效");
  if (
    [
      "prompt",
      "content",
      "reasoning",
      "arguments",
      "result",
      "api_key",
      "base_url",
    ].some((field) => field in row)
  ) {
    return invalidResponse("AI 运行步骤响应包含禁止的正文或凭据字段");
  }
  const kind = stringField(row, "kind");
  const status = stringField(row, "status");
  if (
    kind !== "generation" &&
    kind !== "model_turn" &&
    kind !== "tool_call" &&
    kind !== "self_check" &&
    kind !== "citation_validation" &&
    kind !== "persistence"
  ) {
    return invalidResponse("AI 运行步骤类型无效");
  }
  if (
    status !== "running" &&
    status !== "succeeded" &&
    status !== "failed" &&
    status !== "cancelled"
  ) {
    return invalidResponse("AI 运行步骤状态无效");
  }
  const turnIndexRaw = fieldValue(row, "turn_index", "turnIndex");
  const turnIndex =
    turnIndexRaw === null || turnIndexRaw === undefined
      ? null
      : positiveInteger(turnIndexRaw, "AI 运行轮次");
  const toolName = nullableString(fieldValue(row, "tool_name", "toolName"));
  const completedAt = nullableString(
    fieldValue(row, "completed_at", "completedAt"),
  );
  const durationRaw = fieldValue(row, "duration_ms", "durationMs");
  const durationMs =
    durationRaw === null || durationRaw === undefined
      ? null
      : nonNegativeInteger(durationRaw, "AI 运行步骤耗时");
  const errorCode = nullableString(fieldValue(row, "error_code", "errorCode"));
  const inputTokensRaw = fieldValue(row, "input_tokens", "inputTokens");
  const outputTokensRaw = fieldValue(row, "output_tokens", "outputTokens");
  const inputTokens =
    inputTokensRaw === null || inputTokensRaw === undefined
      ? null
      : nonNegativeInteger(inputTokensRaw, "AI Provider 输入 token");
  const outputTokens =
    outputTokensRaw === null || outputTokensRaw === undefined
      ? null
      : nonNegativeInteger(outputTokensRaw, "AI Provider 输出 token");
  const rawTokenSource = nullableString(
    fieldValue(row, "token_source", "tokenSource"),
  );
  const usageComplete =
    rawTokenSource === "provider" &&
    inputTokens !== null &&
    outputTokens !== null;
  const usageEmpty =
    rawTokenSource === null && inputTokens === null && outputTokens === null;
  if (
    (kind === "model_turn" || kind === "self_check") !== (turnIndex !== null) ||
    (kind === "tool_call") !== (toolName !== null) ||
    (status === "running" &&
      (completedAt !== null || durationMs !== null || errorCode !== null)) ||
    (status !== "running" && (completedAt === null || durationMs === null)) ||
    (status === "failed") !== (errorCode !== null) ||
    (!usageComplete && !usageEmpty)
  ) {
    return invalidResponse("AI 运行步骤状态组合无效");
  }
  return {
    id: stringField(row, "id") ?? "",
    generationId: stringField(row, "generation_id", "generationId") ?? "",
    sequence: positiveInteger(row.sequence, "AI 运行步骤序号"),
    kind,
    status,
    turnIndex,
    toolName,
    startedAt: stringField(row, "started_at", "startedAt") ?? "",
    completedAt,
    durationMs,
    inputBytes: nonNegativeInteger(
      fieldValue(row, "input_bytes", "inputBytes"),
      "AI 运行输入字节",
    ),
    outputBytes: nonNegativeInteger(
      fieldValue(row, "output_bytes", "outputBytes"),
      "AI 运行输出字节",
    ),
    inputTokens,
    outputTokens,
    tokenSource: rawTokenSource === "provider" ? "provider" : null,
    errorCode,
  };
}

export async function getAiRunSteps(
  generationId: string,
): Promise<AiRunStepListResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/generations/${encodeURIComponent(generationId)}/steps`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("AI 运行步骤列表响应格式无效");
  }
  const items = payload.data.map(aiRunStepFromRecord);
  const metaGenerationId =
    stringField(payload.meta, "generation_id", "generationId") ?? "";
  const status = stringField(payload.meta, "status");
  if (
    metaGenerationId !== generationId ||
    (status !== "queued" &&
      status !== "streaming" &&
      status !== "completed" &&
      status !== "failed" &&
      status !== "cancelled") ||
    items.some(
      (item, index) =>
        item.generationId !== generationId || item.sequence !== index + 1,
    )
  ) {
    return invalidResponse("AI 运行步骤列表响应不一致");
  }
  const total = nonNegativeInteger(payload.meta.total, "AI 运行步骤总数");
  const metaInputTokens = nullableNonNegativeInteger(
    fieldValue(payload.meta, "input_tokens", "inputTokens"),
    "AI Provider 输入总 token",
  );
  const metaOutputTokens = nullableNonNegativeInteger(
    fieldValue(payload.meta, "output_tokens", "outputTokens"),
    "AI Provider 输出总 token",
  );
  const metaTokenSource = nullableString(
    fieldValue(payload.meta, "token_source", "tokenSource"),
  );
  const metaUsageComplete =
    metaTokenSource === "provider" &&
    metaInputTokens !== null &&
    metaOutputTokens !== null;
  const metaUsageEmpty =
    metaTokenSource === null &&
    metaInputTokens === null &&
    metaOutputTokens === null;
  if (total !== items.length || (!metaUsageComplete && !metaUsageEmpty)) {
    return invalidResponse("AI 运行步骤总数不一致");
  }
  return {
    items,
    meta: {
      generationId,
      status,
      total,
      inputBytes: nonNegativeInteger(
        fieldValue(payload.meta, "input_bytes", "inputBytes"),
        "AI 运行输入总字节",
      ),
      outputBytes: nonNegativeInteger(
        fieldValue(payload.meta, "output_bytes", "outputBytes"),
        "AI 运行输出总字节",
      ),
      durationMs: nonNegativeInteger(
        fieldValue(payload.meta, "duration_ms", "durationMs"),
        "AI 运行总耗时",
      ),
      inputTokens: metaInputTokens,
      outputTokens: metaOutputTokens,
      tokenSource: metaTokenSource === "provider" ? "provider" : null,
    },
  };
}

function aiUsageTotalsFromRecord(row: unknown, label: string): AiUsageTotals {
  if (!isRecord(row)) return invalidResponse(`${label}响应格式无效`);
  const totals: AiUsageTotals = {
    totalGenerations: nonNegativeInteger(
      fieldValue(row, "total_generations", "totalGenerations"),
      `${label}生成总数`,
    ),
    completedGenerations: nonNegativeInteger(
      fieldValue(row, "completed_generations", "completedGenerations"),
      `${label}已完成数`,
    ),
    failedGenerations: nonNegativeInteger(
      fieldValue(row, "failed_generations", "failedGenerations"),
      `${label}失败数`,
    ),
    cancelledGenerations: nonNegativeInteger(
      fieldValue(row, "cancelled_generations", "cancelledGenerations"),
      `${label}取消数`,
    ),
    activeGenerations: nonNegativeInteger(
      fieldValue(row, "active_generations", "activeGenerations"),
      `${label}活动数`,
    ),
    providerUsageGenerations: nonNegativeInteger(
      fieldValue(row, "provider_usage_generations", "providerUsageGenerations"),
      `${label}Provider usage 数`,
    ),
    unknownUsageGenerations: nonNegativeInteger(
      fieldValue(row, "unknown_usage_generations", "unknownUsageGenerations"),
      `${label}未知 usage 数`,
    ),
    inputTokens: nonNegativeInteger(
      fieldValue(row, "input_tokens", "inputTokens"),
      `${label}输入 token`,
    ),
    outputTokens: nonNegativeInteger(
      fieldValue(row, "output_tokens", "outputTokens"),
      `${label}输出 token`,
    ),
    inputBytes: nonNegativeInteger(
      fieldValue(row, "input_bytes", "inputBytes"),
      `${label}输入字节`,
    ),
    outputBytes: nonNegativeInteger(
      fieldValue(row, "output_bytes", "outputBytes"),
      `${label}输出字节`,
    ),
    durationMs: nonNegativeInteger(
      fieldValue(row, "duration_ms", "durationMs"),
      `${label}总耗时`,
    ),
  };
  const terminalGenerations =
    totals.completedGenerations +
    totals.failedGenerations +
    totals.cancelledGenerations;
  if (
    terminalGenerations + totals.activeGenerations !==
      totals.totalGenerations ||
    totals.providerUsageGenerations + totals.unknownUsageGenerations !==
      terminalGenerations
  ) {
    return invalidResponse(`${label}状态计数不一致`);
  }
  return totals;
}

const aiUsageForbiddenFields = [
  "prompt",
  "content",
  "reasoning",
  "arguments",
  "result",
  "api_key",
  "base_url",
  "estimated_tokens",
  "estimated_cost",
  "cost",
  "cost_minor",
  "currency",
];

function aiUsageTrendPointFromRecord(row: unknown): AiUsageTrendPoint {
  if (!isRecord(row) || aiUsageForbiddenFields.some((field) => field in row)) {
    return invalidResponse("AI 本地用量趋势响应格式无效");
  }
  const day = stringField(row, "day") ?? "";
  const parsed = new Date(`${day}T00:00:00Z`);
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(day) ||
    Number.isNaN(parsed.getTime()) ||
    parsed.toISOString().slice(0, 10) !== day
  ) {
    return invalidResponse("AI 本地用量趋势日期无效");
  }
  const totals = aiUsageTotalsFromRecord(row, "AI 本地用量趋势");
  if (totals.activeGenerations !== 0) {
    return invalidResponse("AI 本地用量趋势不能包含活动生成");
  }
  return { day, ...totals };
}

function aiUsageSummaryFromEnvelope(
  payload: unknown,
  expectedSessionId: string | null,
  expectedProviderId: string | null,
  expectedTrendDays: number,
): AiUsageSummary {
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 本地用量响应格式无效");
  }
  const data = payload.data;
  if (
    !isRecord(data.scope) ||
    !isRecord(data.totals) ||
    !Array.isArray(data.providers) ||
    !Array.isArray(data.trend)
  ) {
    return invalidResponse("AI 本地用量响应格式无效");
  }
  if (
    [data, data.scope, data.totals, ...data.providers, ...data.trend].some(
      (row) =>
        isRecord(row) && aiUsageForbiddenFields.some((field) => field in row),
    )
  ) {
    return invalidResponse("AI 本地用量响应包含禁止的正文或凭据字段");
  }
  const sessionId = nullableString(
    fieldValue(data.scope, "session_id", "sessionId"),
  );
  const providerId = nullableString(
    fieldValue(data.scope, "provider_id", "providerId"),
  );
  if (sessionId !== expectedSessionId || providerId !== expectedProviderId) {
    return invalidResponse("AI 本地用量范围不一致");
  }
  const totals = aiUsageTotalsFromRecord(data.totals, "AI 本地用量");
  const trendDays = positiveInteger(
    fieldValue(data, "trend_days", "trendDays"),
    "AI 本地用量趋势天数",
  );
  if (trendDays !== expectedTrendDays) {
    return invalidResponse("AI 本地用量趋势范围不一致");
  }
  const trend = data.trend.map(aiUsageTrendPointFromRecord);
  if (
    trend.length !== trendDays ||
    trend.some((point, index) => {
      if (index === 0) return false;
      const previous = new Date(`${trend[index - 1]?.day}T00:00:00Z`);
      const current = new Date(`${point.day}T00:00:00Z`);
      return current.getTime() - previous.getTime() !== 86_400_000;
    }) ||
    trend.some((point) =>
      (Object.keys(totals) as Array<keyof AiUsageTotals>).some(
        (key) => point[key] > totals[key],
      ),
    )
  ) {
    return invalidResponse("AI 本地用量趋势分组或范围不一致");
  }
  const providers = data.providers.map((row) => {
    if (!isRecord(row)) return invalidResponse("AI Provider 用量响应格式无效");
    const kind = stringField(row, "provider_kind", "providerKind");
    const protocol = stringField(row, "provider_protocol", "providerProtocol");
    if (
      (kind !== "local" && kind !== "remote") ||
      (protocol !== "openai_chat" && protocol !== "anthropic_messages")
    ) {
      return invalidResponse("AI Provider 用量身份无效");
    }
    const providerTotals = aiUsageTotalsFromRecord(row, "AI Provider 用量");
    const providerRecord = {
      providerId: stringField(row, "provider_id", "providerId") ?? "",
      providerName: stringField(row, "provider_name", "providerName") ?? "",
      model: stringField(row, "model") ?? "",
    };
    if (
      !providerRecord.providerId ||
      !providerRecord.providerName ||
      !providerRecord.model
    ) {
      return invalidResponse("AI Provider 用量身份无效");
    }
    return {
      ...providerRecord,
      providerKind: kind as AiProviderKind,
      providerProtocol: protocol as AiProviderProtocol,
      ...providerTotals,
    };
  });
  if (
    new Set(providers.map((provider) => provider.providerId)).size !==
    providers.length
  ) {
    return invalidResponse("AI Provider 用量分组重复");
  }
  const summed = providers.reduce<AiUsageTotals>(
    (result, provider) => ({
      totalGenerations: result.totalGenerations + provider.totalGenerations,
      completedGenerations:
        result.completedGenerations + provider.completedGenerations,
      failedGenerations: result.failedGenerations + provider.failedGenerations,
      cancelledGenerations:
        result.cancelledGenerations + provider.cancelledGenerations,
      activeGenerations: result.activeGenerations + provider.activeGenerations,
      providerUsageGenerations:
        result.providerUsageGenerations + provider.providerUsageGenerations,
      unknownUsageGenerations:
        result.unknownUsageGenerations + provider.unknownUsageGenerations,
      inputTokens: result.inputTokens + provider.inputTokens,
      outputTokens: result.outputTokens + provider.outputTokens,
      inputBytes: result.inputBytes + provider.inputBytes,
      outputBytes: result.outputBytes + provider.outputBytes,
      durationMs: result.durationMs + provider.durationMs,
    }),
    {
      totalGenerations: 0,
      completedGenerations: 0,
      failedGenerations: 0,
      cancelledGenerations: 0,
      activeGenerations: 0,
      providerUsageGenerations: 0,
      unknownUsageGenerations: 0,
      inputTokens: 0,
      outputTokens: 0,
      inputBytes: 0,
      outputBytes: 0,
      durationMs: 0,
    },
  );
  if (
    (Object.keys(totals) as Array<keyof AiUsageTotals>).some(
      (key) => totals[key] !== summed[key],
    )
  ) {
    return invalidResponse("AI 本地用量 Provider 汇总不一致");
  }
  return {
    scope: { sessionId, providerId },
    totals,
    providers,
    trendDays,
    trend,
  };
}

export async function getAiUsageSummary(
  input: {
    sessionId?: string;
    providerId?: string;
    trendDays?: number;
  } = {},
): Promise<AiUsageSummary> {
  const params = new URLSearchParams();
  if (input.sessionId) params.set("session_id", input.sessionId);
  if (input.providerId) params.set("provider_id", input.providerId);
  const trendDays = input.trendDays ?? 7;
  params.set("trend_days", String(trendDays));
  const query = params.size > 0 ? `?${params}` : "";
  const payload = await apiRequest<unknown>(`/api/v1/ai/usage-summary${query}`);
  return aiUsageSummaryFromEnvelope(
    payload,
    input.sessionId ?? null,
    input.providerId ?? null,
    trendDays,
  );
}
