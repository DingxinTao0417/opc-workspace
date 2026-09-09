import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getAiUsageSummary, resetRuntimeConnection } from "./client";

const sessionId = "018f0000-0000-7000-8000-000000006411";

function zeroTrend(day: string) {
  return {
    day,
    total_generations: 0,
    completed_generations: 0,
    failed_generations: 0,
    cancelled_generations: 0,
    active_generations: 0,
    provider_usage_generations: 0,
    unknown_usage_generations: 0,
    input_tokens: 0,
    output_tokens: 0,
    input_bytes: 0,
    output_bytes: 0,
    duration_ms: 0,
  };
}

function usageSummaryResponse(
  providerExtra: Record<string, unknown> = {},
  totalsExtra: Record<string, unknown> = {},
  dataExtra: Record<string, unknown> = {},
) {
  return new Response(
    JSON.stringify({
      data: {
        scope: { session_id: sessionId, provider_id: null },
        trend_days: 7,
        totals: {
          total_generations: 3,
          completed_generations: 2,
          failed_generations: 1,
          cancelled_generations: 0,
          active_generations: 0,
          provider_usage_generations: 2,
          unknown_usage_generations: 1,
          input_tokens: 150,
          output_tokens: 30,
          input_bytes: 3000,
          output_bytes: 600,
          duration_ms: 3000,
          ...totalsExtra,
        },
        providers: [
          {
            provider_id: "018f0000-0000-7000-8000-000000006412",
            provider_name: "Local A",
            provider_kind: "local",
            provider_protocol: "openai_chat",
            model: "model-a",
            total_generations: 2,
            completed_generations: 1,
            failed_generations: 1,
            cancelled_generations: 0,
            active_generations: 0,
            provider_usage_generations: 1,
            unknown_usage_generations: 1,
            input_tokens: 100,
            output_tokens: 20,
            input_bytes: 2000,
            output_bytes: 400,
            duration_ms: 2000,
            ...providerExtra,
          },
          {
            provider_id: "018f0000-0000-7000-8000-000000006413",
            provider_name: "Local B",
            provider_kind: "local",
            provider_protocol: "anthropic_messages",
            model: "model-b",
            total_generations: 1,
            completed_generations: 1,
            failed_generations: 0,
            cancelled_generations: 0,
            active_generations: 0,
            provider_usage_generations: 1,
            unknown_usage_generations: 0,
            input_tokens: 50,
            output_tokens: 10,
            input_bytes: 1000,
            output_bytes: 200,
            duration_ms: 1000,
          },
        ],
        trend: [
          zeroTrend("2026-09-02"),
          zeroTrend("2026-09-03"),
          zeroTrend("2026-09-04"),
          zeroTrend("2026-09-05"),
          zeroTrend("2026-09-06"),
          zeroTrend("2026-09-07"),
          zeroTrend("2026-09-08"),
        ],
        ...dataExtra,
      },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI local usage summary parsing", () => {
  it("parses session and Provider aggregates without inventing costs", async () => {
    let requestedURL = "";
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      requestedURL = String(input);
      return usageSummaryResponse();
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await getAiUsageSummary({ sessionId });

    expect(requestedURL).toContain(
      `/api/v1/ai/usage-summary?session_id=${sessionId}&trend_days=7`,
    );
    expect(result).toMatchObject({
      scope: { sessionId, providerId: null },
      totals: {
        totalGenerations: 3,
        providerUsageGenerations: 2,
        unknownUsageGenerations: 1,
        inputTokens: 150,
        outputTokens: 30,
      },
      providers: [
        {
          providerName: "Local A",
          providerKind: "local",
          providerProtocol: "openai_chat",
          inputTokens: 100,
        },
        {
          providerName: "Local B",
          providerProtocol: "anthropic_messages",
          inputTokens: 50,
        },
      ],
    });
    expect(result.trendDays).toBe(7);
    expect(result.trend).toContainEqual(
      expect.objectContaining({ day: "2026-09-02", totalGenerations: 0 }),
    );
    expect(result).not.toHaveProperty("cost");
  });

  it("rejects inconsistent terminal, coverage, or Provider totals", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        usageSummaryResponse({}, { provider_usage_generations: 3 }),
      ),
    );

    await expect(getAiUsageSummary({ sessionId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("fails closed if aggregate rows contain endpoint or content fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => usageSummaryResponse({ base_url: "secret" })),
    );

    await expect(getAiUsageSummary({ sessionId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects server-computed costs instead of presenting estimates", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => usageSummaryResponse({ estimated_cost: 0.12 })),
    );

    await expect(getAiUsageSummary({ sessionId })).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects non-consecutive trend dates or active trend rows", async () => {
    for (const extra of [
      {
        trend: [
          zeroTrend("2026-09-02"),
          zeroTrend("2026-09-04"),
          zeroTrend("2026-09-05"),
          zeroTrend("2026-09-06"),
          zeroTrend("2026-09-07"),
          zeroTrend("2026-09-08"),
          zeroTrend("2026-09-09"),
        ],
      },
      {
        trend: [
          {
            ...zeroTrend("2026-09-02"),
            total_generations: 1,
            active_generations: 1,
          },
          zeroTrend("2026-09-03"),
          zeroTrend("2026-09-04"),
          zeroTrend("2026-09-05"),
          zeroTrend("2026-09-06"),
          zeroTrend("2026-09-07"),
          zeroTrend("2026-09-08"),
        ],
      },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => usageSummaryResponse({}, {}, extra)),
      );
      await expect(getAiUsageSummary({ sessionId })).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      } satisfies Partial<ApiError>);
      vi.unstubAllGlobals();
      resetRuntimeConnection();
    }
  });
});
