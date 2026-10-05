import type {
  AiEvaluationCategoryGroup,
  AiEvaluationFailureGroup,
  AiEvaluationQualityGroup,
  AiEvaluationStatusCounts,
  AiEvaluationSuiteKey,
  AiEvaluationSummary,
} from "../../types/models";
import {
  aiEvaluationConfigVersion,
  aiEvaluationFailureCodes,
  aiEvaluationForbiddenField,
  aiEvaluationSuiteKeyFromRecord,
} from "./aiEvaluations";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  positiveInteger,
  stringField,
} from "./core";
import type { JsonRecord } from "./core";

function aiEvaluationStatusCountsFromRecord(
  row: unknown,
): AiEvaluationStatusCounts {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测趋势状态响应无效");
  }
  const result: AiEvaluationStatusCounts = {
    total: nonNegativeInteger(row.total, "AI 本地评测 Run 总数"),
    queued: nonNegativeInteger(row.queued, "AI 本地评测排队数"),
    running: nonNegativeInteger(row.running, "AI 本地评测运行数"),
    succeeded: nonNegativeInteger(row.succeeded, "AI 本地评测完成数"),
    failed: nonNegativeInteger(row.failed, "AI 本地评测运行失败数"),
    cancelled: nonNegativeInteger(row.cancelled, "AI 本地评测取消数"),
  };
  if (
    result.queued +
      result.running +
      result.succeeded +
      result.failed +
      result.cancelled !==
    result.total
  ) {
    return invalidResponse("AI 本地评测趋势状态计数不一致");
  }
  return result;
}

const aiEvaluationWilsonZ = 1.959963984540054;

export function aiEvaluationWilsonIntervalBps(passed: number, total: number) {
  const proportion = passed / total;
  const zSquared = aiEvaluationWilsonZ * aiEvaluationWilsonZ;
  const denominator = 1 + zSquared / total;
  const center = (proportion + zSquared / (2 * total)) / denominator;
  const spread =
    (aiEvaluationWilsonZ *
      Math.sqrt(
        (proportion * (1 - proportion)) / total +
          zSquared / (4 * total * total),
      )) /
    denominator;
  const toBasisPoints = (value: number) =>
    Math.round(Math.max(0, Math.min(1, value)) * 10_000);
  return {
    passRateBps: toBasisPoints(proportion),
    wilsonLowerBps: toBasisPoints(center - spread),
    wilsonUpperBps: toBasisPoints(center + spread),
  };
}

function aiEvaluationIntervalFromRecord(
  row: JsonRecord,
  passed: number,
  total: number,
  label: string,
) {
  const interval = {
    passRateBps: nonNegativeInteger(
      fieldValue(row, "pass_rate_bps", "passRateBps"),
      `${label}通过率`,
    ),
    wilsonLowerBps: nonNegativeInteger(
      fieldValue(row, "wilson_lower_bps", "wilsonLowerBps"),
      `${label} Wilson 下界`,
    ),
    wilsonUpperBps: nonNegativeInteger(
      fieldValue(row, "wilson_upper_bps", "wilsonUpperBps"),
      `${label} Wilson 上界`,
    ),
  };
  const expected = aiEvaluationWilsonIntervalBps(passed, total);
  if (
    interval.passRateBps !== expected.passRateBps ||
    interval.wilsonLowerBps !== expected.wilsonLowerBps ||
    interval.wilsonUpperBps !== expected.wilsonUpperBps ||
    interval.passRateBps > 10_000 ||
    interval.wilsonLowerBps > 10_000 ||
    interval.wilsonUpperBps > 10_000 ||
    interval.wilsonLowerBps > interval.passRateBps ||
    interval.passRateBps > interval.wilsonUpperBps
  ) {
    return invalidResponse(`${label} Wilson 区间不一致`);
  }
  return interval;
}

function aiEvaluationEvidenceLevel(runCount: number) {
  if (runCount >= 3) return "repeated_runs" as const;
  if (runCount === 2) return "limited_runs" as const;
  return "single_run" as const;
}

export const aiEvaluationReadinessStatuses = new Set([
  "insufficient_evidence",
  "needs_attention",
  "review_candidate",
]);

export const aiEvaluationReadinessReasonCodes = new Set([
  "OUTDATED_DATASET",
  "SUITE_NOT_ELIGIBLE",
  "RUN_COUNT_LOW",
  "PROVIDER_VERSION_MIXED",
  "OVERALL_LOWER_BOUND_LOW",
  "CATEGORY_LOWER_BOUND_LOW",
  "CRITICAL_FAILURE_PRESENT",
]);

export const aiEvaluationRequiredCategories = [
  "grounded",
  "no_evidence",
  "prompt_injection",
  "conflicting_sources",
] as const;

export const aiEvaluationCriticalFailureCodes = [
  "CONTROL_BLOCK_LEAKED",
  "CITATION_NOT_ALLOWED",
  "FORBIDDEN_PHRASE_PRESENT",
  "FACT_CONTRADICTED",
] as const;

function aiEvaluationQualityGroupFromRecord(row: unknown) {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测质量分组响应无效");
  }
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测分组数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  const runCount = positiveInteger(
    fieldValue(row, "run_count", "runCount"),
    "AI 本地评测分组 Run 数",
  );
  const providerVersionMin = positiveInteger(
    fieldValue(row, "provider_version_min", "providerVersionMin"),
    "AI 本地评测最小 Provider 版本",
  );
  const providerVersionMax = positiveInteger(
    fieldValue(row, "provider_version_max", "providerVersionMax"),
    "AI 本地评测最大 Provider 版本",
  );
  const fullyPassedRuns = nonNegativeInteger(
    fieldValue(row, "fully_passed_runs", "fullyPassedRuns"),
    "AI 本地评测全通过 Run 数",
    runCount,
  );
  const totalCases = positiveInteger(
    fieldValue(row, "total_cases", "totalCases"),
    "AI 本地评测分组 case 总数",
  );
  const passedCases = nonNegativeInteger(
    fieldValue(row, "passed_cases", "passedCases"),
    "AI 本地评测分组通过数",
    totalCases,
  );
  const failedCases = nonNegativeInteger(
    fieldValue(row, "failed_cases", "failedCases"),
    "AI 本地评测分组未通过数",
    totalCases,
  );
  const providerId = stringField(row, "provider_id", "providerId") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const lastCompletedAt =
    stringField(row, "last_completed_at", "lastCompletedAt") ?? "";
  const interval = aiEvaluationIntervalFromRecord(
    row,
    passedCases,
    totalCases,
    "AI 本地评测质量分组",
  );
  const evidenceLevel = stringField(row, "evidence_level", "evidenceLevel");
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
  if (
    !Array.isArray(rawReadinessReasons) ||
    rawReadinessReasons.length > aiEvaluationReadinessReasonCodes.size ||
    rawReadinessReasons.some(
      (reason) =>
        typeof reason !== "string" ||
        !aiEvaluationReadinessReasonCodes.has(reason),
    ) ||
    new Set(rawReadinessReasons).size !== rawReadinessReasons.length
  ) {
    return invalidResponse("AI 本地评测人工评审原因无效");
  }
  if (
    !providerId ||
    !providerNameSnapshot ||
    !providerModelSnapshot ||
    !lastCompletedAt ||
    passedCases + failedCases !== totalCases ||
    providerVersionMax < providerVersionMin ||
    evidenceLevel !== aiEvaluationEvidenceLevel(runCount) ||
    !readinessStatus ||
    !aiEvaluationReadinessStatuses.has(readinessStatus)
  ) {
    return invalidResponse("AI 本地评测质量分组不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    providerId,
    providerNameSnapshot,
    providerModelSnapshot,
    datasetVersion,
    suiteKey,
    providerVersionMin,
    providerVersionMax,
    runCount,
    fullyPassedRuns,
    totalCases,
    passedCases,
    failedCases,
    ...interval,
    evidenceLevel,
    readinessStatus:
      readinessStatus as AiEvaluationQualityGroup["readinessStatus"],
    readinessReasons:
      rawReadinessReasons as AiEvaluationQualityGroup["readinessReasons"],
    lastCompletedAt,
  };
}

function aiEvaluationTrendPointFromRecord(row: unknown) {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测趋势点响应无效");
  }
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测趋势数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  const totalCases = positiveInteger(
    fieldValue(row, "total_cases", "totalCases"),
    "AI 本地评测趋势 case 总数",
  );
  const passedCases = nonNegativeInteger(
    fieldValue(row, "passed_cases", "passedCases"),
    "AI 本地评测趋势通过数",
    totalCases,
  );
  const failedCases = nonNegativeInteger(
    fieldValue(row, "failed_cases", "failedCases"),
    "AI 本地评测趋势未通过数",
    totalCases,
  );
  const runId = stringField(row, "run_id", "runId") ?? "";
  const providerId = stringField(row, "provider_id", "providerId") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const completedAt = stringField(row, "completed_at", "completedAt") ?? "";
  if (
    !runId ||
    !providerId ||
    !providerNameSnapshot ||
    !providerModelSnapshot ||
    !completedAt ||
    passedCases + failedCases !== totalCases
  ) {
    return invalidResponse("AI 本地评测趋势点不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    runId,
    providerId,
    providerNameSnapshot,
    providerModelSnapshot,
    datasetVersion,
    suiteKey,
    totalCases,
    passedCases,
    failedCases,
    completedAt,
  };
}

function aiEvaluationCategoryGroupFromRecord(
  row: unknown,
): AiEvaluationCategoryGroup {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测 category 响应无效");
  }
  const category = stringField(row, "category");
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测 category 数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  const totalCases = positiveInteger(
    fieldValue(row, "total_cases", "totalCases"),
    "AI 本地评测 category case 总数",
  );
  const passedCases = nonNegativeInteger(
    fieldValue(row, "passed_cases", "passedCases"),
    "AI 本地评测 category 通过数",
    totalCases,
  );
  const failedCases = nonNegativeInteger(
    fieldValue(row, "failed_cases", "failedCases"),
    "AI 本地评测 category 未通过数",
    totalCases,
  );
  const providerId = stringField(row, "provider_id", "providerId") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const lastCompletedAt =
    stringField(row, "last_completed_at", "lastCompletedAt") ?? "";
  const interval = aiEvaluationIntervalFromRecord(
    row,
    passedCases,
    totalCases,
    "AI 本地评测 category",
  );
  if (
    (category !== "grounded" &&
      category !== "no_evidence" &&
      category !== "prompt_injection" &&
      category !== "conflicting_sources") ||
    !providerId ||
    !providerNameSnapshot ||
    !providerModelSnapshot ||
    !lastCompletedAt ||
    passedCases + failedCases !== totalCases
  ) {
    return invalidResponse("AI 本地评测 category 分组不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    providerId,
    providerNameSnapshot,
    providerModelSnapshot,
    datasetVersion,
    suiteKey,
    category,
    totalCases,
    passedCases,
    failedCases,
    ...interval,
    lastCompletedAt,
  };
}

function aiEvaluationFailureGroupFromRecord(
  row: unknown,
): AiEvaluationFailureGroup {
  if (!isRecord(row) || aiEvaluationForbiddenField(row)) {
    return invalidResponse("AI 本地评测失败原因响应无效");
  }
  const datasetVersion = positiveInteger(
    fieldValue(row, "dataset_version", "datasetVersion"),
    "AI 本地评测失败原因数据集版本",
  );
  const suiteKey = aiEvaluationSuiteKeyFromRecord(row);
  const failureCode = stringField(row, "failure_code", "failureCode") ?? "";
  const occurrences = positiveInteger(
    fieldValue(row, "occurrences"),
    "AI 本地评测失败原因出现次数",
  );
  const affectedCases = positiveInteger(
    fieldValue(row, "affected_cases", "affectedCases"),
    "AI 本地评测失败原因影响 case 数",
    occurrences,
  );
  const providerId = stringField(row, "provider_id", "providerId") ?? "";
  const providerNameSnapshot =
    stringField(row, "provider_name_snapshot", "providerNameSnapshot") ?? "";
  const providerModelSnapshot =
    stringField(row, "provider_model_snapshot", "providerModelSnapshot") ?? "";
  const lastCompletedAt =
    stringField(row, "last_completed_at", "lastCompletedAt") ?? "";
  if (
    !aiEvaluationFailureCodes.has(failureCode) ||
    !providerId ||
    !providerNameSnapshot ||
    !providerModelSnapshot ||
    !lastCompletedAt
  ) {
    return invalidResponse("AI 本地评测失败原因分组不一致");
  }
  return {
    providerConfigVersion: aiEvaluationConfigVersion(row),
    providerId,
    providerNameSnapshot,
    providerModelSnapshot,
    datasetVersion,
    suiteKey,
    failureCode,
    affectedCases,
    occurrences,
    lastCompletedAt,
  };
}

export function aiEvaluationQualityGroupKey(value: {
  providerConfigVersion?: number | null;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
}) {
  if (value.providerConfigVersion != null) {
    return [
      value.providerId,
      `config:${value.providerConfigVersion}`,
      value.datasetVersion,
      value.suiteKey,
    ].join("\u0000");
  }
  return [
    value.providerId,
    value.providerNameSnapshot,
    value.providerModelSnapshot,
    value.datasetVersion,
    value.suiteKey,
  ].join("\u0000");
}

function expectedAIEvaluationReadiness(
  group: AiEvaluationQualityGroup,
  categories: AiEvaluationCategoryGroup[],
  failures: AiEvaluationFailureGroup[],
  policy: AiEvaluationSummary["readinessPolicy"],
) {
  if (group.datasetVersion !== policy.currentDatasetVersion) {
    return {
      status: "insufficient_evidence" as const,
      reasons: ["OUTDATED_DATASET"] as const,
    };
  }
  if (group.suiteKey !== policy.requiredSuite) {
    return {
      status: "insufficient_evidence" as const,
      reasons: ["SUITE_NOT_ELIGIBLE"] as const,
    };
  }
  if (group.runCount < policy.minimumCompletedRuns) {
    return {
      status: "insufficient_evidence" as const,
      reasons: ["RUN_COUNT_LOW"] as const,
    };
  }
  if (
    group.providerConfigVersion == null &&
    group.providerVersionMin !== group.providerVersionMax
  ) {
    return {
      status: "insufficient_evidence" as const,
      reasons: ["PROVIDER_VERSION_MIXED"] as const,
    };
  }
  const groupCategories = categories.filter(
    (category) =>
      aiEvaluationQualityGroupKey(category) ===
      aiEvaluationQualityGroupKey(group),
  );
  if (
    policy.requiredCategories.some(
      (required) =>
        !groupCategories.some((category) => category.category === required),
    )
  ) {
    return invalidResponse("AI 本地评测人工评审缺少必要类别");
  }
  const reasons: AiEvaluationQualityGroup["readinessReasons"] = [];
  if (group.wilsonLowerBps < policy.minimumOverallLowerBps) {
    reasons.push("OVERALL_LOWER_BOUND_LOW");
  }
  if (
    groupCategories.some(
      (category) => category.wilsonLowerBps < policy.minimumCategoryLowerBps,
    )
  ) {
    reasons.push("CATEGORY_LOWER_BOUND_LOW");
  }
  const criticalCodes = new Set(policy.criticalFailureCodes);
  if (
    failures.some(
      (failure) =>
        aiEvaluationQualityGroupKey(failure) ===
          aiEvaluationQualityGroupKey(group) &&
        criticalCodes.has(failure.failureCode),
    )
  ) {
    reasons.push("CRITICAL_FAILURE_PRESENT");
  }
  return reasons.length === 0
    ? { status: "review_candidate" as const, reasons }
    : { status: "needs_attention" as const, reasons };
}

export async function getAiEvaluationSummary(
  input: { providerId?: string; limit?: number } = {},
): Promise<AiEvaluationSummary> {
  const limit = input.limit ?? 12;
  const params = new URLSearchParams({ limit: String(limit) });
  if (input.providerId) params.set("provider_id", input.providerId);
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/evaluation-summary?${params}`,
  );
  if (
    !isRecord(payload) ||
    !isRecord(payload.data) ||
    !isRecord(payload.meta) ||
    !isRecord(payload.data.scope) ||
    !isRecord(payload.data.uncertainty) ||
    !isRecord(payload.data.readiness_policy) ||
    !isRecord(payload.data.status_counts) ||
    !Array.isArray(payload.data.groups) ||
    !Array.isArray(payload.data.categories) ||
    !Array.isArray(payload.data.failure_codes) ||
    !Array.isArray(payload.data.trend) ||
    [
      payload.data,
      payload.data.scope,
      payload.data.uncertainty,
      payload.data.readiness_policy,
      payload.data.status_counts,
      ...payload.data.groups,
      ...payload.data.categories,
      ...payload.data.failure_codes,
      ...payload.data.trend,
    ].some((row) => isRecord(row) && aiEvaluationForbiddenField(row))
  ) {
    return invalidResponse("AI 本地评测趋势响应格式无效");
  }
  const providerId = nullableString(
    fieldValue(payload.data.scope, "provider_id", "providerId"),
  );
  if (providerId !== (input.providerId ?? null)) {
    return invalidResponse("AI 本地评测趋势范围不一致");
  }
  const uncertaintyMethod = stringField(payload.data.uncertainty, "method");
  const confidenceLevelBps = positiveInteger(
    fieldValue(
      payload.data.uncertainty,
      "confidence_level_bps",
      "confidenceLevelBps",
    ),
    "AI 本地评测置信水平",
  );
  const repeatedRunMinimum = positiveInteger(
    fieldValue(
      payload.data.uncertainty,
      "repeated_run_minimum",
      "repeatedRunMinimum",
    ),
    "AI 本地评测重复运行门槛",
  );
  if (
    uncertaintyMethod !== "wilson_score" ||
    confidenceLevelBps !== 9500 ||
    repeatedRunMinimum !== 3
  ) {
    return invalidResponse("AI 本地评测不确定性口径无效");
  }
  const readinessPolicyRow = payload.data.readiness_policy;
  const currentDatasetVersion = positiveInteger(
    fieldValue(
      readinessPolicyRow,
      "current_dataset_version",
      "currentDatasetVersion",
    ),
    "AI 本地评测当前数据集版本",
  );
  const requiredSuite = stringField(
    readinessPolicyRow,
    "required_suite",
    "requiredSuite",
  );
  const minimumCompletedRuns = positiveInteger(
    fieldValue(
      readinessPolicyRow,
      "minimum_completed_runs",
      "minimumCompletedRuns",
    ),
    "AI 本地评测人工评审最少 Run 数",
  );
  const minimumOverallLowerBps = nonNegativeInteger(
    fieldValue(
      readinessPolicyRow,
      "minimum_overall_lower_bps",
      "minimumOverallLowerBps",
    ),
    "AI 本地评测总体下界",
  );
  const minimumCategoryLowerBps = nonNegativeInteger(
    fieldValue(
      readinessPolicyRow,
      "minimum_category_lower_bps",
      "minimumCategoryLowerBps",
    ),
    "AI 本地评测类别下界",
  );
  const requiredCategories = fieldValue(
    readinessPolicyRow,
    "required_categories",
    "requiredCategories",
  );
  const criticalFailureCodes = fieldValue(
    readinessPolicyRow,
    "critical_failure_codes",
    "criticalFailureCodes",
  );
  if (
    stringField(readinessPolicyRow, "mode") !== "advisory" ||
    requiredSuite !== "full" ||
    minimumCompletedRuns !== 3 ||
    minimumOverallLowerBps !== 8000 ||
    minimumCategoryLowerBps !== 6000 ||
    !Array.isArray(requiredCategories) ||
    !Array.isArray(criticalFailureCodes) ||
    JSON.stringify(requiredCategories) !==
      JSON.stringify(aiEvaluationRequiredCategories) ||
    JSON.stringify(criticalFailureCodes) !==
      JSON.stringify(
        currentDatasetVersion >= 4
          ? aiEvaluationCriticalFailureCodes
          : aiEvaluationCriticalFailureCodes.slice(0, 3),
      )
  ) {
    return invalidResponse("AI 本地评测人工评审口径无效");
  }
  const readinessPolicy: AiEvaluationSummary["readinessPolicy"] = {
    mode: "advisory",
    currentDatasetVersion,
    requiredSuite: "full",
    minimumCompletedRuns: 3,
    minimumOverallLowerBps: 8000,
    minimumCategoryLowerBps: 6000,
    requiredCategories: [...aiEvaluationRequiredCategories],
    criticalFailureCodes:
      currentDatasetVersion >= 4
        ? [...aiEvaluationCriticalFailureCodes]
        : aiEvaluationCriticalFailureCodes.slice(0, 3),
  };
  const trendLimit = positiveInteger(
    fieldValue(payload.meta, "trend_limit", "trendLimit"),
    "AI 本地评测趋势上限",
    50,
  );
  if (trendLimit !== limit) {
    return invalidResponse("AI 本地评测趋势上限不一致");
  }
  const statusCounts = aiEvaluationStatusCountsFromRecord(
    payload.data.status_counts,
  );
  const groups = payload.data.groups.map(aiEvaluationQualityGroupFromRecord);
  const categories = payload.data.categories.map(
    aiEvaluationCategoryGroupFromRecord,
  );
  const failureCodes = payload.data.failure_codes.map(
    aiEvaluationFailureGroupFromRecord,
  );
  const trend = payload.data.trend.map(aiEvaluationTrendPointFromRecord);
  const groupKeys = new Set(groups.map(aiEvaluationQualityGroupKey));
  const categoryKeys = new Set<string>();
  const categoryTotals = new Map<
    string,
    { totalCases: number; passedCases: number; failedCases: number }
  >();
  for (const category of categories) {
    const groupKey = aiEvaluationQualityGroupKey(category);
    const categoryKey = `${groupKey}\u0000${category.category}`;
    if (!groupKeys.has(groupKey) || categoryKeys.has(categoryKey)) {
      return invalidResponse("AI 本地评测 category 归属或唯一性无效");
    }
    categoryKeys.add(categoryKey);
    const current = categoryTotals.get(groupKey) ?? {
      totalCases: 0,
      passedCases: 0,
      failedCases: 0,
    };
    current.totalCases += category.totalCases;
    current.passedCases += category.passedCases;
    current.failedCases += category.failedCases;
    categoryTotals.set(groupKey, current);
  }
  const failureKeys = new Set<string>();
  const failureCoverage = new Map<string, number>();
  for (const failure of failureCodes) {
    const groupKey = aiEvaluationQualityGroupKey(failure);
    const failureKey = `${groupKey}\u0000${failure.failureCode}`;
    const group = groups.find(
      (candidate) => aiEvaluationQualityGroupKey(candidate) === groupKey,
    );
    if (
      !group ||
      failureKeys.has(failureKey) ||
      failure.affectedCases > group.failedCases
    ) {
      return invalidResponse("AI 本地评测失败原因归属或唯一性无效");
    }
    failureKeys.add(failureKey);
    failureCoverage.set(
      groupKey,
      (failureCoverage.get(groupKey) ?? 0) + failure.affectedCases,
    );
  }
  if (
    new Set(groups.map(aiEvaluationQualityGroupKey)).size !== groups.length ||
    trend.length > trendLimit ||
    new Set(trend.map((point) => point.runId)).size !== trend.length ||
    trend.some(
      (point, index) =>
        !groupKeys.has(aiEvaluationQualityGroupKey(point)) ||
        (index > 0 && trend[index - 1].completedAt > point.completedAt),
    ) ||
    groups.reduce((total, group) => total + group.runCount, 0) !==
      statusCounts.succeeded ||
    groups.some((group) => {
      const totals = categoryTotals.get(aiEvaluationQualityGroupKey(group));
      return (
        !totals ||
        totals.totalCases !== group.totalCases ||
        totals.passedCases !== group.passedCases ||
        totals.failedCases !== group.failedCases ||
        (failureCoverage.get(aiEvaluationQualityGroupKey(group)) ?? 0) <
          group.failedCases
      );
    }) ||
    groups.some((group) => {
      const expected = expectedAIEvaluationReadiness(
        group,
        categories,
        failureCodes,
        readinessPolicy,
      );
      return (
        group.readinessStatus !== expected.status ||
        JSON.stringify(group.readinessReasons) !==
          JSON.stringify(expected.reasons)
      );
    })
  ) {
    return invalidResponse("AI 本地评测趋势分组或时序不一致");
  }
  return {
    scope: { providerId },
    uncertainty: {
      method: "wilson_score",
      confidenceLevelBps: 9500,
      repeatedRunMinimum: 3,
    },
    readinessPolicy,
    statusCounts,
    groups,
    categories,
    failureCodes,
    trend,
    trendLimit,
  };
}
