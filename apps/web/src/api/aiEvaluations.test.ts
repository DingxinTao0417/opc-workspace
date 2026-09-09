import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  cancelAiEvaluation,
  createAiEvaluation,
  deleteAiEvaluation,
  getAiEvaluation,
  getAiEvaluations,
  resetRuntimeConnection,
} from "./client";

const runId = "018f0000-0000-7000-8000-000000006801";
const providerId = "018f0000-0000-7000-8000-000000006802";

function runRecord(extra: Record<string, unknown> = {}) {
  return {
    id: runId,
    provider_id: providerId,
    provider_name_snapshot: "Ollama",
    provider_model_snapshot: "qwen3",
    provider_protocol_snapshot: "openai_chat",
    provider_version: 3,
    dataset_version: 1,
    suite_key: "full",
    status: "succeeded",
    total_cases: 2,
    completed_cases: 2,
    passed_cases: 1,
    failed_cases: 1,
    error_cases: 0,
    current_case_id: null,
    cancel_requested: false,
    error_code: null,
    started_at: "2026-09-09T08:00:00Z",
    completed_at: "2026-09-09T08:00:05Z",
    created_at: "2026-09-09T08:00:00Z",
    updated_at: "2026-09-09T08:00:05Z",
    results: [],
    ...extra,
  };
}

function resultRecords(extra: Record<string, unknown> = {}) {
  return [
    {
      id: "018f0000-0000-7000-8000-000000006803",
      run_id: runId,
      sequence: 1,
      case_id: "zh_invoice_grounded",
      language: "zh-CN",
      category: "grounded",
      status: "passed",
      failure_codes: [],
      citation_status: "validated",
      citation_count: 1,
      duration_ms: 1200,
      input_bytes: 512,
      output_bytes: 128,
      input_tokens: 64,
      output_tokens: 16,
      token_source: "provider",
      error_code: null,
      created_at: "2026-09-09T08:00:02Z",
      ...extra,
    },
    {
      id: "018f0000-0000-7000-8000-000000006804",
      run_id: runId,
      sequence: 2,
      case_id: "zh_no_evidence_refund",
      language: "zh-CN",
      category: "no_evidence",
      status: "failed",
      failure_codes: ["REQUIRED_PHRASE_MISSING"],
      citation_status: "no_evidence",
      citation_count: 0,
      duration_ms: 1500,
      input_bytes: 480,
      output_bytes: 96,
      input_tokens: null,
      output_tokens: null,
      token_source: null,
      error_code: null,
      created_at: "2026-09-09T08:00:04Z",
    },
  ];
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI local evaluation API parsing", () => {
  it("parses paged content-free runs", async () => {
    let requestedURL = "";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        requestedURL = String(input);
        return new Response(
          JSON.stringify({
            data: [runRecord()],
            meta: { page: 1, page_size: 20, total: 1 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }),
    );

    const page = await getAiEvaluations({ providerId });

    expect(requestedURL).toContain(`provider_id=${providerId}`);
    expect(page).toMatchObject({
      items: [
        {
          id: runId,
          status: "succeeded",
          passedCases: 1,
          failedCases: 1,
          results: [],
        },
      ],
      meta: { page: 1, pageSize: 20, total: 1 },
    });
  });

  it("accepts positive dataset versions while preserving legacy v1", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [
                runRecord(),
                runRecord({
                  id: "018f0000-0000-7000-8000-000000006805",
                  dataset_version: 2,
                  total_cases: 12,
                  completed_cases: 12,
                  passed_cases: 12,
                  failed_cases: 0,
                }),
              ],
              meta: { page: 1, page_size: 20, total: 2 },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    const page = await getAiEvaluations();

    expect(page.items.map((item) => item.datasetVersion)).toEqual([1, 2]);
    expect(page.items[1]).toMatchObject({ totalCases: 12, passedCases: 12 });
  });

  it("parses result failure codes and exact Provider token availability", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: { ...runRecord(), results: resultRecords() },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    const run = await getAiEvaluation(runId);

    expect(run.results).toMatchObject([
      {
        status: "passed",
        inputTokens: 64,
        outputTokens: 16,
        tokenSource: "provider",
      },
      {
        status: "failed",
        failureCodes: ["REQUIRED_PHRASE_MISSING"],
        tokenSource: null,
      },
    ]);
  });

  it("sends an idempotent local run request and a separate cancel command", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            data: runRecord({
              status: "queued",
              completed_cases: 0,
              passed_cases: 0,
              failed_cases: 0,
              total_cases: 4,
              started_at: null,
              completed_at: null,
            }),
          }),
          { status: 202, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            data: runRecord({
              status: "running",
              completed_cases: 1,
              passed_cases: 1,
              failed_cases: 0,
              total_cases: 4,
              current_case_id: "zh_no_evidence_refund",
              cancel_requested: true,
              completed_at: null,
            }),
          }),
          { status: 202, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await createAiEvaluation(providerId, 3, "smoke", "evaluation-idempotency");
    await cancelAiEvaluation(runId);
    await deleteAiEvaluation(runId);

    const createInit = fetchMock.mock.calls[0]?.[1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "evaluation-idempotency",
    );
    expect(JSON.parse(String(createInit?.body))).toEqual({
      provider_id: providerId,
      provider_version: 3,
      suite_key: "smoke",
    });
    expect(String(fetchMock.mock.calls[1]?.[0])).toContain(
      `/api/v1/ai/evaluations/${runId}/cancel`,
    );
    expect(String(fetchMock.mock.calls[2]?.[0])).toContain(
      `/api/v1/ai/evaluations/${runId}?confirm=true`,
    );
    expect(fetchMock.mock.calls[2]?.[1]?.method).toBe("DELETE");
  });

  it("rejects leaked answer fields and inconsistent result/token state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: {
                ...runRecord(),
                results: resultRecords({ answer: "private model answer" }),
              },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    await expect(getAiEvaluation(runId)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects a non-positive dataset version", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [runRecord({ dataset_version: 0 })],
              meta: { page: 1, page_size: 20, total: 1 },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    await expect(getAiEvaluations()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("accepts every code-owned topic suite identity", async () => {
    const suites = [
      "grounded",
      "no_evidence",
      "prompt_injection",
      "conflicting_sources",
    ] as const;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: suites.map((suiteKey, index) =>
                runRecord({
                  id: `topic-run-${index}`,
                  dataset_version: 3,
                  suite_key: suiteKey,
                  total_cases: 6,
                  completed_cases: 6,
                  passed_cases: 6,
                  failed_cases: 0,
                }),
              ),
              meta: { page: 1, page_size: 20, total: suites.length },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    const page = await getAiEvaluations();

    expect(page.items.map((item) => item.suiteKey)).toEqual(suites);
    expect(page.items.every((item) => item.totalCases === 6)).toBe(true);
  });

  it("rejects an unknown or missing persisted suite", async () => {
    for (const suiteKey of ["quick", undefined]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(
          async () =>
            new Response(
              JSON.stringify({
                data: [runRecord({ suite_key: suiteKey })],
                meta: { page: 1, page_size: 20, total: 1 },
              }),
              { status: 200, headers: { "Content-Type": "application/json" } },
            ),
        ),
      );
      await expect(getAiEvaluations()).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      } satisfies Partial<ApiError>);
      vi.unstubAllGlobals();
      resetRuntimeConnection();
    }
  });
});
