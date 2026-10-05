import type {
  AiEvaluationQualityGroup,
  AiEvaluationReview,
  AiEvaluationReviewDecision,
  AiEvaluationReviewListResult,
  CreateAiEvaluationReviewInput,
} from "../../types/models";
import {
  aiEvaluationConfigVersion,
  aiEvaluationForbiddenField,
  aiEvaluationSuiteKeyFromRecord,
} from "./aiEvaluations";
import {
  aiEvaluationCriticalFailureCodes,
  aiEvaluationReadinessReasonCodes,
  aiEvaluationReadinessStatuses,
  aiEvaluationRequiredCategories,
  aiEvaluationWilsonIntervalBps,
} from "./aiEvaluationSummary";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
} from "./core";

const aiEvaluationReviewDecisions = new Set<AiEvaluationReviewDecision>([
  "accepted_for_local_use",
  "needs_more_evidence",
  "rejected",
]);

function aiEvaluationReviewFromRecord(row: unknown): AiEvaluationReview {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测人工决定包含无效或禁止字段");
  }
  const id = stringField(row, "id") ?? "";
  const providerIdSnapshot =
    stringField(row, "provider_id_snapshot", "providerIdSnapshot") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测人工决定数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  const providerVersionMin = positiveInteger(
    fieldValue(row, "provider_version_min", "providerVersionMin"),
    "AI 本地评测人工决定最小 Provider 版本",
  );
  const providerVersionMax = positiveInteger(
    fieldValue(row, "provider_version_max", "providerVersionMax"),
    "AI 本地评测人工决定最大 Provider 版本",
  );
  const groupLastCompletedAt =
    stringField(row, "group_last_completed_at", "groupLastCompletedAt") ?? "";
  const runCount = positiveInteger(
    fieldValue(row, "run_count", "runCount"),
    "AI 本地评测人工决定 Run 数",
  );
  const totalCases = positiveInteger(
    fieldValue(row, "total_cases", "totalCases"),
    "AI 本地评测人工决定 case 总数",
  );
  const passedCases = nonNegativeInteger(
    fieldValue(row, "passed_cases", "passedCases"),
    "AI 本地评测人工决定通过数",
  );
  const failedCases = nonNegativeInteger(
    fieldValue(row, "failed_cases", "failedCases"),
    "AI 本地评测人工决定失败数",
  );
  const overallWilsonLowerBps = nonNegativeInteger(
    fieldValue(row, "overall_wilson_lower_bps", "overallWilsonLowerBps"),
    "AI 本地评测人工决定总体 Wilson 下界",
  );
  const minimumCategory = stringField(
    row,
    "minimum_category",
    "minimumCategory",
  );
  const minimumCategoryWilsonLowerBps = nonNegativeInteger(
    fieldValue(
      row,
      "minimum_category_wilson_lower_bps",
      "minimumCategoryWilsonLowerBps",
    ),
    "AI 本地评测人工决定最低类别 Wilson 下界",
  );
  const readinessStatus = stringField(
    row,
    "readiness_status",
    "readinessStatus",
  );
  const rawReadinessReasons = fieldValue(
    row,
    "readiness_reasons",
    "readinessReasons",
  );
  const rawCriticalFailureCodes = fieldValue(
    row,
    "critical_failure_codes",
    "criticalFailureCodes",
  );
  const decision = stringField(row, "decision");
  const reason = stringField(row, "reason") ?? "";
  const reviewedByActorId =
    stringField(row, "reviewed_by_actor_id", "reviewedByActorId") ?? "";
  const reviewedByActorNameSnapshot =
    stringField(
      row,
      "reviewed_by_actor_name_snapshot",
      "reviewedByActorNameSnapshot",
    ) ?? "";
  const createdAt = stringField(row, "created_at", "createdAt") ?? "";
  const criticalSet = new Set<string>(aiEvaluationCriticalFailureCodes);
  if (
    !Array.isArray(rawReadinessReasons) ||
    rawReadinessReasons.length > aiEvaluationReadinessReasonCodes.size ||
    rawReadinessReasons.some(
      (item) =>
        typeof item !== "string" || !aiEvaluationReadinessReasonCodes.has(item),
    ) ||
    new Set(rawReadinessReasons).size !== rawReadinessReasons.length ||
    !Array.isArray(rawCriticalFailureCodes) ||
    rawCriticalFailureCodes.some(
      (item) => typeof item !== "string" || !criticalSet.has(item),
    ) ||
    new Set(rawCriticalFailureCodes).size !== rawCriticalFailureCodes.length
  ) {
    return invalidResponse("AI 本地评测人工决定原因快照无效");
  }
  const readinessReasons =
    rawReadinessReasons as AiEvaluationQualityGroup["readinessReasons"];
  const criticalFailureCodes = rawCriticalFailureCodes as string[];
  const selectedCriticalCodes = new Set(criticalFailureCodes);
  const expectedCriticalOrder = aiEvaluationCriticalFailureCodes.filter(
    (code) => selectedCriticalCodes.has(code),
  );
  const expectedOverall = aiEvaluationWilsonIntervalBps(
    passedCases,
    totalCases,
  );
  if (
    !id ||
    !providerIdSnapshot ||
    !providerNameSnapshot ||
    providerNameSnapshot !== providerNameSnapshot.trim() ||
    !providerModelSnapshot ||
    providerModelSnapshot !== providerModelSnapshot.trim() ||
    !groupLastCompletedAt ||
    providerVersionMax < providerVersionMin ||
    passedCases + failedCases !== totalCases ||
    overallWilsonLowerBps > 10_000 ||
    overallWilsonLowerBps !== expectedOverall.wilsonLowerBps ||
    minimumCategoryWilsonLowerBps > 10_000 ||
    !aiEvaluationRequiredCategories.includes(
      minimumCategory as (typeof aiEvaluationRequiredCategories)[number],
    ) ||
    !readinessStatus ||
    !aiEvaluationReadinessStatuses.has(readinessStatus) ||
    (readinessStatus === "review_candidate"
      ? readinessReasons.length !== 0
      : readinessReasons.length === 0) ||
    !decision ||
    !aiEvaluationReviewDecisions.has(decision as AiEvaluationReviewDecision) ||
    !reason ||
    reason !== reason.trim() ||
    [...reason].length > 1000 ||
    reviewedByActorId !== "00000000-0000-5000-8000-000000000001" ||
    !reviewedByActorNameSnapshot ||
    reviewedByActorNameSnapshot !== reviewedByActorNameSnapshot.trim() ||
    !createdAt ||
    JSON.stringify(criticalFailureCodes) !==
      JSON.stringify(expectedCriticalOrder)
  ) {
    return invalidResponse("AI 本地评测人工决定证据快照不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    id,
    providerIdSnapshot,
    providerNameSnapshot,
    providerModelSnapshot,
    datasetVersion,
    suiteKey,
    providerVersionMin,
    providerVersionMax,
    groupLastCompletedAt,
    runCount,
    totalCases,
    passedCases,
    failedCases,
    overallWilsonLowerBps,
    minimumCategory: minimumCategory as AiEvaluationReview["minimumCategory"],
    minimumCategoryWilsonLowerBps,
    readinessStatus: readinessStatus as AiEvaluationReview["readinessStatus"],
    readinessReasons,
    criticalFailureCodes,
    decision: decision as AiEvaluationReviewDecision,
    reason,
    reviewedByActorId,
    reviewedByActorNameSnapshot,
    createdAt,
  };
}

export async function getAiEvaluationReviews(
  input: {
    providerId?: string;
    decision?: AiEvaluationReviewDecision;
    page?: number;
    pageSize?: number;
  } = {},
): Promise<AiEvaluationReviewListResult> {
  const page = input.page ?? 1;
  const pageSize = input.pageSize ?? 20;
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  });
  if (input.providerId) params.set("provider_id", input.providerId);
  if (input.decision) params.set("decision", input.decision);
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/evaluation-reviews?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("AI 本地评测人工决定列表响应格式无效");
  }
  const items = payload.data.map(aiEvaluationReviewFromRecord);
  const meta = {
    page: positiveInteger(payload.meta.page, "AI 本地评测人工决定页码"),
    pageSize: positiveInteger(
      fieldValue(payload.meta, "page_size", "pageSize"),
      "AI 本地评测人工决定每页数量",
    ),
    total: nonNegativeInteger(payload.meta.total, "AI 本地评测人工决定总数"),
  };
  if (
    meta.page !== page ||
    meta.pageSize !== pageSize ||
    items.length > meta.pageSize ||
    items.length > meta.total ||
    (input.providerId !== undefined &&
      items.some((item) => item.providerIdSnapshot !== input.providerId)) ||
    (input.decision !== undefined &&
      items.some((item) => item.decision !== input.decision))
  ) {
    return invalidResponse("AI 本地评测人工决定分页不一致");
  }
  return { items, meta };
}

export async function createAiEvaluationReview(
  input: CreateAiEvaluationReviewInput,
  idempotencyKey: string,
): Promise<AiEvaluationReview> {
  const { group } = input;
  const payload = await apiRequest<unknown>("/api/v1/ai/evaluation-reviews", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      provider_id: group.providerId,
      provider_name_snapshot: group.providerNameSnapshot,
      provider_model_snapshot: group.providerModelSnapshot,
      dataset_version: group.datasetVersion,
      suite_key: group.suiteKey,
      ...(group.providerConfigVersion != null
        ? { provider_config_version: group.providerConfigVersion }
        : {}),
      expected_provider_version_min: group.providerVersionMin,
      expected_provider_version_max: group.providerVersionMax,
      expected_last_completed_at: group.lastCompletedAt,
      expected_run_count: group.runCount,
      expected_total_cases: group.totalCases,
      expected_passed_cases: group.passedCases,
      expected_failed_cases: group.failedCases,
      expected_readiness_status: group.readinessStatus,
      expected_readiness_reasons: group.readinessReasons,
      decision: input.decision,
      reason: input.reason,
    }),
  });
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 本地评测人工决定创建响应格式无效");
  }
  return aiEvaluationReviewFromRecord(payload.data);
}
