import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAiEvaluationReview,
  getAiEvaluationReviews,
  resetRuntimeConnection,
} from "./client";
import type {
  AiEvaluationQualityGroup,
  CreateAiEvaluationReviewInput,
} from "../types/models";

const providerId = "018f0000-0000-7000-8000-000000006901";

const group: AiEvaluationQualityGroup = {
  providerId,
  providerNameSnapshot: "Ollama",
  providerModelSnapshot: "qwen3",
  datasetVersion: 3,
  suiteKey: "full",
  providerVersionMin: 3,
  providerVersionMax: 3,
  runCount: 3,
  fullyPassedRuns: 3,
  totalCases: 72,
  passedCases: 72,
  failedCases: 0,
  passRateBps: 10_000,
  wilsonLowerBps: 9493,
  wilsonUpperBps: 10_000,
  evidenceLevel: "repeated_runs",
  readinessStatus: "review_candidate",
  readinessReasons: [],
  lastCompletedAt: "2026-09-09T10:02:01Z",
};

const reviewInput: CreateAiEvaluationReviewInput = {
  group,
  decision: "accepted_for_local_use",
  reason: "三次完整评测稳定通过，仅批准本机试用。",
};

function reviewRecord(extra: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000006902",
    provider_id_snapshot: providerId,
    provider_name_snapshot: "Ollama",
    provider_model_snapshot: "qwen3",
    dataset_version: 3,
    suite_key: "full",
    provider_version_min: 3,
    provider_version_max: 3,
    group_last_completed_at: "2026-09-09T10:02:01Z",
    run_count: 3,
    total_cases: 72,
    passed_cases: 72,
    failed_cases: 0,
    overall_wilson_lower_bps: 9493,
    minimum_category: "conflicting_sources",
    minimum_category_wilson_lower_bps: 8242,
    readiness_status: "review_candidate",
    readiness_reasons: [],
    critical_failure_codes: [],
    decision: "accepted_for_local_use",
    reason: reviewInput.reason,
    reviewed_by_actor_id: "00000000-0000-5000-8000-000000000001",
    reviewed_by_actor_name_snapshot: "我",
    created_at: "2026-09-09T10:05:00Z",
    ...extra,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI evaluation review API", () => {
  it("creates an append-only decision against the exact visible snapshot", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ data: reviewRecord() }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const review = await createAiEvaluationReview(
      reviewInput,
      "evaluation-review-key",
    );

    expect(review).toMatchObject({
      providerIdSnapshot: providerId,
      overallWilsonLowerBps: 9493,
      decision: "accepted_for_local_use",
      reason: reviewInput.reason,
      reviewedByActorNameSnapshot: "我",
    });
    const request = fetchMock.mock.calls[0]?.[1];
    expect(new Headers(request?.headers).get("Idempotency-Key")).toBe(
      "evaluation-review-key",
    );
    expect(JSON.parse(String(request?.body))).toEqual({
      provider_id: providerId,
      provider_name_snapshot: "Ollama",
      provider_model_snapshot: "qwen3",
      dataset_version: 3,
      suite_key: "full",
      expected_provider_version_min: 3,
      expected_provider_version_max: 3,
      expected_last_completed_at: "2026-09-09T10:02:01Z",
      expected_run_count: 3,
      expected_total_cases: 72,
      expected_passed_cases: 72,
      expected_failed_cases: 0,
      expected_readiness_status: "review_candidate",
      expected_readiness_reasons: [],
      decision: "accepted_for_local_use",
      reason: reviewInput.reason,
    });
  });

  it("parses filtered paged audit history", async () => {
    let requestedUrl = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        requestedUrl = String(input);
        return new Response(
          JSON.stringify({
            data: [reviewRecord()],
            meta: { page: 1, page_size: 10, total: 1 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }),
    );

    const result = await getAiEvaluationReviews({
      providerId,
      decision: "accepted_for_local_use",
      page: 1,
      pageSize: 10,
    });

    expect(requestedUrl).toContain(`provider_id=${providerId}`);
    expect(requestedUrl).toContain("decision=accepted_for_local_use");
    expect(result.meta).toEqual({ page: 1, pageSize: 10, total: 1 });
    expect(result.items[0]?.minimumCategory).toBe("conflicting_sources");
  });

  it("accepts an audited topic-suite evidence snapshot", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [
                reviewRecord({
                  suite_key: "grounded",
                  run_count: 1,
                  total_cases: 6,
                  passed_cases: 6,
                  overall_wilson_lower_bps: 6097,
                  minimum_category: "grounded",
                  minimum_category_wilson_lower_bps: 6097,
                  readiness_status: "insufficient_evidence",
                  readiness_reasons: ["SUITE_NOT_ELIGIBLE"],
                  decision: "needs_more_evidence",
                }),
              ],
              meta: { page: 1, page_size: 20, total: 1 },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    const result = await getAiEvaluationReviews();

    expect(result.items[0]).toMatchObject({
      suiteKey: "grounded",
      totalCases: 6,
      minimumCategory: "grounded",
      decision: "needs_more_evidence",
    });
  });

  it.each([
    ["forbidden answer", { answer: "private model answer" }],
    ["wrong Wilson lower bound", { overall_wilson_lower_bps: 9000 }],
    ["unexpected reviewer", { reviewed_by_actor_id: "another-actor" }],
    ["invalid candidate reasons", { readiness_reasons: ["RUN_COUNT_LOW"] }],
  ])("rejects %s", async (_name, extra) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [reviewRecord(extra)],
              meta: { page: 1, page_size: 20, total: 1 },
            }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          ),
      ),
    );

    await expect(
      getAiEvaluationReviews({ page: 1, pageSize: 20 }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
});
