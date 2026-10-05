import type { AiCitationStatus } from "./ai";

export type AiEvaluationStatus =
  "queued" | "running" | "succeeded" | "failed" | "cancelled";

export type AiEvaluationResultStatus = "passed" | "failed" | "error";

export type AiEvaluationSuiteKey =
  | "smoke"
  | "full"
  | "grounded"
  | "no_evidence"
  | "prompt_injection"
  | "conflicting_sources";

export interface AiEvaluationResult {
  id: string;
  runId: string;
  sequence: number;
  caseId: string;
  language: "zh-CN" | "en";
  category:
    "grounded" | "no_evidence" | "prompt_injection" | "conflicting_sources";
  status: AiEvaluationResultStatus;
  failureCodes: string[];
  citationStatus: Exclude<AiCitationStatus, "not_requested"> | null;
  citationCount: number;
  durationMs: number;
  inputBytes: number;
  outputBytes: number;
  inputTokens: number | null;
  outputTokens: number | null;
  tokenSource: "provider" | null;
  errorCode: string | null;
  createdAt: string;
}

export interface AiEvaluationRun {
  providerConfigVersion?: number | null;
  id: string;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  providerProtocolSnapshot: "openai_chat";
  providerVersion: number;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  status: AiEvaluationStatus;
  totalCases: number;
  completedCases: number;
  passedCases: number;
  failedCases: number;
  errorCases: number;
  currentCaseId: string | null;
  cancelRequested: boolean;
  errorCode: string | null;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
  results: AiEvaluationResult[];
}

export interface AiEvaluationListResult {
  items: AiEvaluationRun[];
  meta: {
    page: number;
    pageSize: number;
    total: number;
  };
}

export interface AiEvaluationStatusCounts {
  total: number;
  queued: number;
  running: number;
  succeeded: number;
  failed: number;
  cancelled: number;
}

export interface AiEvaluationQualityGroup {
  providerConfigVersion?: number | null;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  providerVersionMin: number;
  providerVersionMax: number;
  runCount: number;
  fullyPassedRuns: number;
  totalCases: number;
  passedCases: number;
  failedCases: number;
  passRateBps: number;
  wilsonLowerBps: number;
  wilsonUpperBps: number;
  evidenceLevel: "single_run" | "limited_runs" | "repeated_runs";
  readinessStatus:
    "insufficient_evidence" | "needs_attention" | "review_candidate";
  readinessReasons: Array<
    | "OUTDATED_DATASET"
    | "SUITE_NOT_ELIGIBLE"
    | "RUN_COUNT_LOW"
    | "PROVIDER_VERSION_MIXED"
    | "OVERALL_LOWER_BOUND_LOW"
    | "CATEGORY_LOWER_BOUND_LOW"
    | "CRITICAL_FAILURE_PRESENT"
  >;
  lastCompletedAt: string;
}

export interface AiEvaluationTrendPoint {
  providerConfigVersion?: number | null;
  runId: string;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  totalCases: number;
  passedCases: number;
  failedCases: number;
  completedAt: string;
}

export interface AiEvaluationCategoryGroup {
  providerConfigVersion?: number | null;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  category: AiEvaluationResult["category"];
  totalCases: number;
  passedCases: number;
  failedCases: number;
  passRateBps: number;
  wilsonLowerBps: number;
  wilsonUpperBps: number;
  lastCompletedAt: string;
}

export interface AiEvaluationFailureGroup {
  providerConfigVersion?: number | null;
  providerId: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  failureCode: string;
  affectedCases: number;
  occurrences: number;
  lastCompletedAt: string;
}

export interface AiEvaluationSummary {
  scope: { providerId: string | null };
  uncertainty: {
    method: "wilson_score";
    confidenceLevelBps: 9500;
    repeatedRunMinimum: 3;
  };
  readinessPolicy: {
    mode: "advisory";
    currentDatasetVersion: number;
    requiredSuite: "full";
    minimumCompletedRuns: 3;
    minimumOverallLowerBps: 8000;
    minimumCategoryLowerBps: 6000;
    requiredCategories: AiEvaluationResult["category"][];
    criticalFailureCodes: string[];
  };
  statusCounts: AiEvaluationStatusCounts;
  groups: AiEvaluationQualityGroup[];
  categories: AiEvaluationCategoryGroup[];
  failureCodes: AiEvaluationFailureGroup[];
  trend: AiEvaluationTrendPoint[];
  trendLimit: number;
}

export type AiEvaluationReviewDecision =
  "accepted_for_local_use" | "needs_more_evidence" | "rejected";

export interface AiEvaluationReview {
  providerConfigVersion?: number | null;
  id: string;
  providerIdSnapshot: string;
  providerNameSnapshot: string;
  providerModelSnapshot: string;
  datasetVersion: number;
  suiteKey: AiEvaluationSuiteKey;
  providerVersionMin: number;
  providerVersionMax: number;
  groupLastCompletedAt: string;
  runCount: number;
  totalCases: number;
  passedCases: number;
  failedCases: number;
  overallWilsonLowerBps: number;
  minimumCategory: AiEvaluationResult["category"];
  minimumCategoryWilsonLowerBps: number;
  readinessStatus: AiEvaluationQualityGroup["readinessStatus"];
  readinessReasons: AiEvaluationQualityGroup["readinessReasons"];
  criticalFailureCodes: string[];
  decision: AiEvaluationReviewDecision;
  reason: string;
  reviewedByActorId: string;
  reviewedByActorNameSnapshot: string;
  createdAt: string;
}

export interface CreateAiEvaluationReviewInput {
  group: AiEvaluationQualityGroup;
  decision: AiEvaluationReviewDecision;
  reason: string;
}

export interface AiEvaluationReviewListResult {
  items: AiEvaluationReview[];
  meta: {
    page: number;
    pageSize: number;
    total: number;
  };
}
