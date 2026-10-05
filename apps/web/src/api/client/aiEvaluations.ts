import type {
  AiEvaluationListResult,
  AiEvaluationResult,
  AiEvaluationRun,
  AiEvaluationSuiteKey,
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
import type { JsonRecord } from "./core";

export const aiEvaluationFailureCodes = new Set([
  "FACT_CONTRADICTED",
  "FACT_MISSING",
  "CASE_ID_MISMATCH",
  "ANSWER_EMPTY",
  "CONTROL_BLOCK_LEAKED",
  "CITATION_STATUS_MISMATCH",
  "REQUIRED_PHRASE_MISSING",
  "FORBIDDEN_PHRASE_PRESENT",
  "CITATION_NOT_ALLOWED",
  "CITATION_DUPLICATE",
  "CITATION_COUNT_LOW",
  "CITATION_SET_MISMATCH",
  "OBSERVATION_MISSING",
]);

export function aiEvaluationForbiddenField(row: Record<string, unknown>) {
  return [
    "prompt",
    "question",
    "chunks",
    "content",
    "answer",
    "reasoning",
    "arguments",
    "result",
    "base_url",
    "api_key",
  ].some((field) => field in row);
}

function aiEvaluationResultFromRecord(
  row: unknown,
  expectedRunId: string,
): AiEvaluationResult {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测结果包含无效或禁止字段");
  }
  const status = stringField(row, "status");
  const language = stringField(row, "language");
  const category = stringField(row, "category");
  const citationStatus = nullableString(
    fieldValue(row, "citation_status", "citationStatus"),
  );
  const errorCode = nullableString(fieldValue(row, "error_code", "errorCode"));
  const rawFailureCodes = fieldValue(row, "failure_codes", "failureCodes");
  if (
    (status !== "passed" && status !== "failed" && status !== "error") ||
    (language !== "zh-CN" && language !== "en") ||
    (category !== "grounded" &&
      category !== "no_evidence" &&
      category !== "prompt_injection" &&
      category !== "conflicting_sources") ||
    !Array.isArray(rawFailureCodes) ||
    rawFailureCodes.some(
      (code) => typeof code !== "string" || !aiEvaluationFailureCodes.has(code),
    ) ||
    (citationStatus !== null &&
      citationStatus !== "validated" &&
      citationStatus !== "no_evidence" &&
      citationStatus !== "missing" &&
      citationStatus !== "invalid")
  ) {
    return invalidResponse("AI 本地评测结果格式无效");
  }
  const inputTokens = nullableNonNegativeInteger(
    fieldValue(row, "input_tokens", "inputTokens"),
    "AI 本地评测输入 token",
  );
  const outputTokens = nullableNonNegativeInteger(
    fieldValue(row, "output_tokens", "outputTokens"),
    "AI 本地评测输出 token",
  );
  const tokenSource = nullableString(
    fieldValue(row, "token_source", "tokenSource"),
  );
  const usageComplete =
    tokenSource === "provider" && inputTokens !== null && outputTokens !== null;
  const usageEmpty =
    tokenSource === null && inputTokens === null && outputTokens === null;
  const failureCodes = rawFailureCodes as string[];
  if (
    (!usageComplete && !usageEmpty) ||
    (status === "passed" &&
      (failureCodes.length !== 0 ||
        errorCode !== null ||
        citationStatus === null)) ||
    (status === "failed" &&
      (failureCodes.length === 0 ||
        errorCode !== null ||
        citationStatus === null)) ||
    (status === "error" &&
      (failureCodes.length !== 0 ||
        errorCode === null ||
        citationStatus !== null))
  ) {
    return invalidResponse("AI 本地评测结果状态组合无效");
  }
  const runId = stringField(row, "run_id", "runId") ?? "";
  const id = stringField(row, "id") ?? "";
  const caseId = stringField(row, "case_id", "caseId") ?? "";
  const citationCount = nonNegativeInteger(
    fieldValue(row, "citation_count", "citationCount"),
    "AI 本地评测引用数",
    3,
  );
  if (
    !id ||
    !caseId ||
    runId !== expectedRunId ||
    (status !== "error" &&
      ((citationStatus === "validated" && citationCount === 0) ||
        (citationStatus !== "validated" && citationCount !== 0)))
  ) {
    return invalidResponse("AI 本地评测结果归属不一致");
  }
  return {
    id,
    runId,
    sequence: positiveInteger(row.sequence, "AI 本地评测结果序号"),
    caseId,
    language,
    category,
    status,
    failureCodes,
    citationStatus,
    citationCount,
    durationMs: nonNegativeInteger(
      fieldValue(row, "duration_ms", "durationMs"),
      "AI 本地评测耗时",
    ),
    inputBytes: nonNegativeInteger(
      fieldValue(row, "input_bytes", "inputBytes"),
      "AI 本地评测输入字节",
    ),
    outputBytes: nonNegativeInteger(
      fieldValue(row, "output_bytes", "outputBytes"),
      "AI 本地评测输出字节",
    ),
    inputTokens,
    outputTokens,
    tokenSource: tokenSource === "provider" ? "provider" : null,
    errorCode,
    createdAt: stringField(row, "created_at", "createdAt") ?? "",
  };
}

export function aiEvaluationSuiteKeyFromRecord(
  row: JsonRecord,
): AiEvaluationSuiteKey {
  const suiteKey = stringField(row, "suite_key", "suiteKey");
  if (
    suiteKey !== "smoke" &&
    suiteKey !== "full" &&
    suiteKey !== "grounded" &&
    suiteKey !== "no_evidence" &&
    suiteKey !== "prompt_injection" &&
    suiteKey !== "conflicting_sources"
  ) {
    return invalidResponse("AI 本地评测套件无效");
  }
  return suiteKey;
}

export function aiEvaluationConfigVersion(row: JsonRecord): number | null {
  const value = fieldValue(
    row,
    "provider_config_version",
    "providerConfigVersion",
  );
  return value == null ? null : positiveInteger(value, "AI 模型配置版本");
}

function aiEvaluationRunFromRecord(
  row: unknown,
  requireResults: boolean,
): AiEvaluationRun {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测响应包含无效或禁止字段");
  }
  const status = stringField(row, "status");
  const protocol = stringField(
    row,
    "provider_protocol_snapshot",
    "providerProtocolSnapshot",
  );
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  if (
    (status !== "queued" &&
      status !== "running" &&
      status !== "succeeded" &&
      status !== "failed" &&
      status !== "cancelled") ||
    protocol !== "openai_chat"
  ) {
    return invalidResponse("AI 本地评测身份或状态无效");
  }
  const totalCases = positiveInteger(
    fieldValue(row, "total_cases", "totalCases"),
    "AI 本地评测 case 总数",
    32,
  );
  const completedCases = nonNegativeInteger(
    fieldValue(row, "completed_cases", "completedCases"),
    "AI 本地评测完成数",
    totalCases,
  );
  const passedCases = nonNegativeInteger(
    fieldValue(row, "passed_cases", "passedCases"),
    "AI 本地评测通过数",
    completedCases,
  );
  const failedCases = nonNegativeInteger(
    fieldValue(row, "failed_cases", "failedCases"),
    "AI 本地评测失败数",
    completedCases,
  );
  const errorCases = nonNegativeInteger(
    fieldValue(row, "error_cases", "errorCases"),
    "AI 本地评测错误数",
    completedCases,
  );
  const currentCaseId = nullableString(
    fieldValue(row, "current_case_id", "currentCaseId"),
  );
  const errorCode = nullableString(fieldValue(row, "error_code", "errorCode"));
  const startedAt = nullableString(fieldValue(row, "started_at", "startedAt"));
  const completedAt = nullableString(
    fieldValue(row, "completed_at", "completedAt"),
  );
  const cancelRequested = fieldValue(
    row,
    "cancel_requested",
    "cancelRequested",
  );
  if (typeof cancelRequested !== "boolean") {
    return invalidResponse("AI 本地评测取消状态无效");
  }
  if (
    passedCases + failedCases + errorCases !== completedCases ||
    (status === "queued" &&
      (startedAt !== null ||
        completedAt !== null ||
        currentCaseId !== null ||
        errorCode !== null)) ||
    (status === "running" &&
      (startedAt === null || completedAt !== null || errorCode !== null)) ||
    (status === "succeeded" &&
      (startedAt === null ||
        completedAt === null ||
        currentCaseId !== null ||
        errorCode !== null ||
        completedCases !== totalCases ||
        errorCases !== 0)) ||
    (status === "failed" &&
      (startedAt === null ||
        completedAt === null ||
        currentCaseId !== null ||
        errorCode === null)) ||
    (status === "cancelled" &&
      (completedAt === null || currentCaseId !== null || errorCode !== null))
  ) {
    return invalidResponse("AI 本地评测状态组合无效");
  }
  const id = stringField(row, "id") ?? "";
  const providerId = stringField(row, "provider_id", "providerId") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const createdAt = stringField(row, "created_at", "createdAt") ?? "";
  const updatedAt = stringField(row, "updated_at", "updatedAt") ?? "";
  const rawResults = fieldValue(row, "results");
  if (
    !Array.isArray(rawResults) ||
    (!requireResults && rawResults.length > 0)
  ) {
    return invalidResponse("AI 本地评测结果列表格式无效");
  }
  const results = rawResults.map((result) =>
    aiEvaluationResultFromRecord(result, id),
  );
  if (
    !id ||
    !providerId ||
    !providerNameSnapshot ||
    !providerModelSnapshot ||
    !createdAt ||
    !updatedAt ||
    (requireResults && results.length !== completedCases) ||
    results.some((result, index) => result.sequence !== index + 1) ||
    new Set(results.map((result) => result.caseId)).size !== results.length
  ) {
    return invalidResponse("AI 本地评测结果序列不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    id,
    providerId,
    providerNameSnapshot,
    providerModelSnapshot,
    providerProtocolSnapshot: "openai_chat",
    providerVersion: positiveInteger(
      fieldValue(row, "provider_version", "providerVersion"),
      "AI 本地评测 Provider 版本",
    ),
    datasetVersion,
    suiteKey,
    status,
    totalCases,
    completedCases,
    passedCases,
    failedCases,
    errorCases,
    currentCaseId,
    cancelRequested,
    errorCode,
    startedAt,
    completedAt,
    createdAt,
    updatedAt,
    results,
  };
}

export async function getAiEvaluations(
  input: {
    providerId?: string;
    status?: AiEvaluationRun["status"];
    page?: number;
    pageSize?: number;
  } = {},
): Promise<AiEvaluationListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.providerId) params.set("provider_id", input.providerId);
  if (input.status) params.set("status", input.status);
  const payload = await apiRequest<unknown>(`/api/v1/ai/evaluations?${params}`);
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("AI 本地评测列表响应格式无效");
  }
  const items = payload.data.map((row) =>
    aiEvaluationRunFromRecord(row, false),
  );
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "AI 本地评测页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "AI 本地评测每页数量",
        100,
      ),
      total: nonNegativeInteger(payload.meta.total, "AI 本地评测总数"),
    },
  };
}

export async function getAiEvaluation(id: string): Promise<AiEvaluationRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/evaluations/${encodeURIComponent(id)}`,
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 本地评测详情响应格式无效");
  }
  return aiEvaluationRunFromRecord(payload.data, true);
}

export async function createAiEvaluation(
  providerId: string,
  providerVersion: number,
  suiteKey: AiEvaluationSuiteKey,
  idempotencyKey: string,
): Promise<AiEvaluationRun> {
  const payload = await apiRequest<unknown>("/api/v1/ai/evaluations", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      provider_id: providerId,
      provider_version: providerVersion,
      suite_key: suiteKey,
    }),
  });
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 本地评测创建响应格式无效");
  }
  return aiEvaluationRunFromRecord(payload.data, false);
}

export async function cancelAiEvaluation(id: string): Promise<AiEvaluationRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/evaluations/${encodeURIComponent(id)}/cancel`,
    { method: "POST" },
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 本地评测取消响应格式无效");
  }
  return aiEvaluationRunFromRecord(payload.data, false);
}

export async function deleteAiEvaluation(id: string): Promise<void> {
  await apiRequest<unknown>(
    `/api/v1/ai/evaluations/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE" },
  );
}
