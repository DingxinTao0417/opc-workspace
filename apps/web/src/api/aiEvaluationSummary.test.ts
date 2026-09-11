import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  aiEvaluationQualityGroupKey,
  getAiEvaluationSummary,
  resetRuntimeConnection,
} from "./client";

const providerId = "018f0000-0000-7000-8000-000000006901";

function intervalFields(passed: number, total: number) {
  const z = 1.959963984540054;
  const zSquared = z * z;
  const proportion = passed / total;
  const denominator = 1 + zSquared / total;
  const center = (proportion + zSquared / (2 * total)) / denominator;
  const spread =
    (z *
      Math.sqrt(
        (proportion * (1 - proportion)) / total +
          zSquared / (4 * total * total),
      )) /
    denominator;
  const basisPoints = (value: number) =>
    Math.round(Math.max(0, Math.min(1, value)) * 10_000);
  return {
    pass_rate_bps: basisPoints(proportion),
    wilson_lower_bps: basisPoints(center - spread),
    wilson_upper_bps: basisPoints(center + spread),
  };
}

function evaluationSummaryResponse(
  groupExtra: Record<string, unknown> = {},
  trendExtra: Record<string, unknown> = {},
  categoryExtra: Record<string, unknown> = {},
  failureExtra: Record<string, unknown> = {},
) {
  return new Response(
    JSON.stringify({
      data: {
        scope: { provider_id: providerId },
        uncertainty: {
          method: "wilson_score",
          confidence_level_bps: 9500,
          repeated_run_minimum: 3,
        },
        readiness_policy: {
          mode: "advisory",
          current_dataset_version: 2,
          required_suite: "full",
          minimum_completed_runs: 3,
          minimum_overall_lower_bps: 8000,
          minimum_category_lower_bps: 6000,
          required_categories: [
            "grounded",
            "no_evidence",
            "prompt_injection",
            "conflicting_sources",
          ],
          critical_failure_codes: [
            "CONTROL_BLOCK_LEAKED",
            "CITATION_NOT_ALLOWED",
            "FORBIDDEN_PHRASE_PRESENT",
          ],
        },
        status_counts: {
          total: 5,
          queued: 0,
          running: 1,
          succeeded: 3,
          failed: 1,
          cancelled: 0,
        },
        groups: [
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            provider_version_min: 1,
            provider_version_max: 1,
            run_count: 2,
            fully_passed_runs: 1,
            total_cases: 8,
            passed_cases: 7,
            failed_cases: 1,
            ...intervalFields(7, 8),
            evidence_level: "limited_runs",
            readiness_status: "insufficient_evidence",
            readiness_reasons: ["OUTDATED_DATASET"],
            last_completed_at: "2026-09-09T09:00:02Z",
            ...groupExtra,
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            provider_version_min: 1,
            provider_version_max: 1,
            run_count: 1,
            fully_passed_runs: 0,
            total_cases: 4,
            passed_cases: 2,
            failed_cases: 2,
            ...intervalFields(2, 4),
            evidence_level: "single_run",
            readiness_status: "insufficient_evidence",
            readiness_reasons: ["OUTDATED_DATASET"],
            last_completed_at: "2026-09-09T09:00:03Z",
          },
        ],
        categories: [
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            category: "grounded",
            total_cases: 2,
            passed_cases: 2,
            failed_cases: 0,
            ...intervalFields(2, 2),
            last_completed_at: "2026-09-09T09:00:02Z",
            ...categoryExtra,
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            category: "no_evidence",
            total_cases: 2,
            passed_cases: 2,
            failed_cases: 0,
            ...intervalFields(2, 2),
            last_completed_at: "2026-09-09T09:00:02Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            category: "prompt_injection",
            total_cases: 2,
            passed_cases: 2,
            failed_cases: 0,
            ...intervalFields(2, 2),
            last_completed_at: "2026-09-09T09:00:02Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            category: "conflicting_sources",
            total_cases: 2,
            passed_cases: 1,
            failed_cases: 1,
            ...intervalFields(1, 2),
            last_completed_at: "2026-09-09T09:00:02Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            category: "grounded",
            total_cases: 1,
            passed_cases: 1,
            failed_cases: 0,
            ...intervalFields(1, 1),
            last_completed_at: "2026-09-09T09:00:03Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            category: "no_evidence",
            total_cases: 1,
            passed_cases: 1,
            failed_cases: 0,
            ...intervalFields(1, 1),
            last_completed_at: "2026-09-09T09:00:03Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            category: "prompt_injection",
            total_cases: 1,
            passed_cases: 0,
            failed_cases: 1,
            ...intervalFields(0, 1),
            last_completed_at: "2026-09-09T09:00:03Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            category: "conflicting_sources",
            total_cases: 1,
            passed_cases: 0,
            failed_cases: 1,
            ...intervalFields(0, 1),
            last_completed_at: "2026-09-09T09:00:03Z",
          },
        ],
        failure_codes: [
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            failure_code: "CITATION_SET_MISMATCH",
            affected_cases: 1,
            occurrences: 1,
            last_completed_at: "2026-09-09T09:00:02Z",
            ...failureExtra,
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            failure_code: "REQUIRED_PHRASE_MISSING",
            affected_cases: 1,
            occurrences: 1,
            last_completed_at: "2026-09-09T09:00:02Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            failure_code: "REQUIRED_PHRASE_MISSING",
            affected_cases: 2,
            occurrences: 3,
            last_completed_at: "2026-09-09T09:00:03Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            failure_code: "CITATION_SET_MISMATCH",
            affected_cases: 1,
            occurrences: 1,
            last_completed_at: "2026-09-09T09:00:03Z",
          },
          {
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            failure_code: "FORBIDDEN_PHRASE_PRESENT",
            affected_cases: 1,
            occurrences: 1,
            last_completed_at: "2026-09-09T09:00:03Z",
          },
        ],
        trend: [
          {
            run_id: "018f0000-0000-7000-8000-000000006902",
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "alpha",
            dataset_version: 1,
            suite_key: "full",
            total_cases: 4,
            passed_cases: 3,
            failed_cases: 1,
            completed_at: "2026-09-09T09:00:02Z",
            ...trendExtra,
          },
          {
            run_id: "018f0000-0000-7000-8000-000000006903",
            provider_id: providerId,
            provider_name_snapshot: "Ollama",
            provider_model_snapshot: "beta",
            dataset_version: 1,
            suite_key: "full",
            total_cases: 4,
            passed_cases: 2,
            failed_cases: 2,
            completed_at: "2026-09-09T09:00:03Z",
          },
        ],
      },
      meta: { trend_limit: 12 },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI local evaluation quality summary", () => {
  it("keeps stable configuration identity across health ETags and display renames", async () => {
    const responseBody = await evaluationSummaryResponse().json();
    const group = responseBody.data.groups[0];
    Object.assign(group, {
      provider_config_version: 1,
      dataset_version: 4,
      provider_version_min: 1,
      provider_version_max: 3,
      run_count: 3,
      fully_passed_runs: 3,
      total_cases: 72,
      passed_cases: 72,
      failed_cases: 0,
      ...intervalFields(72, 72),
      evidence_level: "repeated_runs",
      readiness_status: "review_candidate",
      readiness_reasons: [],
    });
    responseBody.data.groups = [group];
    responseBody.data.categories = responseBody.data.categories
      .filter(
        (row: Record<string, unknown>) =>
          row.provider_model_snapshot === "alpha",
      )
      .map((row: Record<string, unknown>) => ({
        ...row,
        provider_config_version: 1,
        dataset_version: 4,
        provider_name_snapshot: "Earlier display name",
        total_cases: 18,
        passed_cases: 18,
        failed_cases: 0,
        ...intervalFields(18, 18),
      }));
    responseBody.data.failure_codes = [];
    responseBody.data.trend = [
      {
        ...responseBody.data.trend[0],
        provider_config_version: 1,
        dataset_version: 4,
        total_cases: 24,
        passed_cases: 24,
        failed_cases: 0,
      },
    ];
    responseBody.data.status_counts = {
      total: 3,
      queued: 0,
      running: 0,
      succeeded: 3,
      failed: 0,
      cancelled: 0,
    };
    responseBody.data.readiness_policy.current_dataset_version = 4;
    responseBody.data.readiness_policy.critical_failure_codes.push(
      "FACT_CONTRADICTED",
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify(responseBody), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );
    const summary = await getAiEvaluationSummary({ providerId });
    expect(summary.groups[0]).toMatchObject({
      providerConfigVersion: 1,
      providerVersionMin: 1,
      providerVersionMax: 3,
      readinessStatus: "review_candidate",
    });
    const key = aiEvaluationQualityGroupKey(summary.groups[0]);
    expect(
      aiEvaluationQualityGroupKey({
        ...summary.groups[0],
        providerNameSnapshot: "Another name",
      }),
    ).toBe(key);
    expect(
      aiEvaluationQualityGroupKey({
        ...summary.groups[0],
        providerConfigVersion: 2,
      }),
    ).not.toBe(key);
    expect(
      aiEvaluationQualityGroupKey({
        ...summary.groups[0],
        providerConfigVersion: null,
      }),
    ).not.toBe(key);
  });

  it("rejects a non-positive configuration identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        evaluationSummaryResponse({ provider_config_version: 0 }),
      ),
    );
    await expect(getAiEvaluationSummary({ providerId })).rejects.toBeInstanceOf(
      ApiError,
    );
  });
  it("parses model/dataset groups and chronological points", async () => {
    let requestedURL = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        requestedURL = String(input);
        return evaluationSummaryResponse();
      }),
    );

    const summary = await getAiEvaluationSummary({ providerId });

    expect(requestedURL).toContain(`provider_id=${providerId}`);
    expect(summary).toMatchObject({
      scope: { providerId },
      statusCounts: { total: 5, running: 1, succeeded: 3, failed: 1 },
      uncertainty: {
        method: "wilson_score",
        confidenceLevelBps: 9500,
        repeatedRunMinimum: 3,
      },
      readinessPolicy: {
        mode: "advisory",
        currentDatasetVersion: 2,
        requiredSuite: "full",
        minimumCompletedRuns: 3,
        minimumOverallLowerBps: 8000,
        minimumCategoryLowerBps: 6000,
      },
      groups: [
        {
          providerModelSnapshot: "alpha",
          runCount: 2,
          fullyPassedRuns: 1,
          passedCases: 7,
          totalCases: 8,
          passRateBps: 8750,
          wilsonLowerBps: 5291,
          wilsonUpperBps: 9776,
          evidenceLevel: "limited_runs",
          readinessStatus: "insufficient_evidence",
          readinessReasons: ["OUTDATED_DATASET"],
        },
        {
          providerModelSnapshot: "beta",
          runCount: 1,
          passedCases: 2,
          totalCases: 4,
        },
      ],
      trend: [
        { providerModelSnapshot: "alpha", passedCases: 3 },
        { providerModelSnapshot: "beta", passedCases: 2 },
      ],
      categories: expect.arrayContaining([
        expect.objectContaining({
          providerModelSnapshot: "alpha",
          category: "conflicting_sources",
          passedCases: 1,
          failedCases: 1,
        }),
        expect.objectContaining({
          providerModelSnapshot: "beta",
          category: "prompt_injection",
          passedCases: 0,
          failedCases: 1,
        }),
      ]),
      failureCodes: expect.arrayContaining([
        expect.objectContaining({
          providerModelSnapshot: "alpha",
          failureCode: "CITATION_SET_MISMATCH",
          affectedCases: 1,
          occurrences: 1,
        }),
        expect.objectContaining({
          providerModelSnapshot: "beta",
          failureCode: "REQUIRED_PHRASE_MISSING",
          affectedCases: 2,
          occurrences: 3,
        }),
      ]),
      trendLimit: 12,
    });
  });

  it("accepts a positive dataset v2 summary", async () => {
    const responseBody = await evaluationSummaryResponse().json();
    for (const key of [
      "groups",
      "categories",
      "failure_codes",
      "trend",
    ] as const) {
      for (const row of responseBody.data[key]) row.dataset_version = 2;
    }
    for (const group of responseBody.data.groups) {
      group.readiness_reasons = ["RUN_COUNT_LOW"];
    }
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify(responseBody), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );

    const summary = await getAiEvaluationSummary({ providerId });

    expect(summary.groups.every((group) => group.datasetVersion === 2)).toBe(
      true,
    );
    expect(
      summary.categories.every((group) => group.datasetVersion === 2),
    ).toBe(true);
  });

  it("accepts a topic suite with only its owned category", async () => {
    const responseBody = await evaluationSummaryResponse().json();
    const group = responseBody.data.groups[0];
    group.dataset_version = 3;
    group.suite_key = "grounded";
    group.readiness_status = "insufficient_evidence";
    group.readiness_reasons = ["SUITE_NOT_ELIGIBLE"];
    responseBody.data.readiness_policy.current_dataset_version = 3;
    responseBody.data.groups = [group];
    const category = responseBody.data.categories[0];
    category.dataset_version = 3;
    category.suite_key = "grounded";
    category.total_cases = 8;
    category.passed_cases = 7;
    category.failed_cases = 1;
    Object.assign(category, intervalFields(7, 8));
    responseBody.data.categories = [category];
    responseBody.data.failure_codes = responseBody.data.failure_codes
      .filter(
        (failure: { provider_model_snapshot: string }) =>
          failure.provider_model_snapshot === "alpha",
      )
      .map((failure: Record<string, unknown>) => ({
        ...failure,
        dataset_version: 3,
        suite_key: "grounded",
      }));
    const trend = responseBody.data.trend[0];
    trend.dataset_version = 3;
    trend.suite_key = "grounded";
    responseBody.data.trend = [trend];
    responseBody.data.status_counts.total = 4;
    responseBody.data.status_counts.succeeded = 2;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify(responseBody), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );

    const summary = await getAiEvaluationSummary({ providerId });

    expect(summary.groups[0]).toMatchObject({
      suiteKey: "grounded",
      readinessReasons: ["SUITE_NOT_ELIGIBLE"],
    });
    expect(summary.categories).toHaveLength(1);
    expect(summary.categories[0]?.category).toBe("grounded");
  });

  it("rejects status or group totals that hide incomplete runs", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => evaluationSummaryResponse({ run_count: 1 })),
    );

    await expect(getAiEvaluationSummary({ providerId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects forged intervals, evidence labels, or confidence metadata", async () => {
    for (const response of [
      () => evaluationSummaryResponse({ wilson_lower_bps: 9999 }),
      () => evaluationSummaryResponse({ pass_rate_bps: undefined }),
      () => evaluationSummaryResponse({ wilson_upper_bps: 10001 }),
      () => evaluationSummaryResponse({ evidence_level: "repeated_runs" }),
      () => evaluationSummaryResponse({ readiness_reasons: ["RUN_COUNT_LOW"] }),
      () => evaluationSummaryResponse({ provider_version_min: 2 }),
      () => evaluationSummaryResponse({ suite_key: "quick" }),
      async () => {
        const body = await evaluationSummaryResponse().json();
        body.data.uncertainty.confidence_level_bps = 9000;
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      },
      async () => {
        const body = await evaluationSummaryResponse().json();
        body.data.readiness_policy.minimum_overall_lower_bps = 7500;
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response()),
      );
      await expect(
        getAiEvaluationSummary({ providerId }),
      ).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      } satisfies Partial<ApiError>);
      vi.unstubAllGlobals();
      resetRuntimeConnection();
    }
  });

  it("rejects non-chronological or cross-group trend points", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        evaluationSummaryResponse(
          { last_completed_at: "2026-09-09T09:00:04Z" },
          { provider_model_snapshot: "unknown" },
        ),
      ),
    );

    await expect(getAiEvaluationSummary({ providerId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("fails closed if a summary includes model output fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => evaluationSummaryResponse({ answer: "private" })),
    );

    await expect(getAiEvaluationSummary({ providerId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects category totals that do not reconcile to their model group", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        evaluationSummaryResponse({}, {}, { passed_cases: 1, failed_cases: 1 }),
      ),
    );

    await expect(getAiEvaluationSummary({ providerId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects failure-code rows that claim more affected cases than the group", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        evaluationSummaryResponse(
          {},
          {},
          {},
          { affected_cases: 3, occurrences: 3 },
        ),
      ),
    );

    await expect(getAiEvaluationSummary({ providerId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });
});
