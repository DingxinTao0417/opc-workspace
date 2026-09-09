import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getAiRunSteps, resetRuntimeConnection } from "./client";

const generationId = "018f0000-0000-7000-8000-000000006401";

function runStepResponse(extra: Record<string, unknown> = {}) {
  return new Response(
    JSON.stringify({
      data: [
        {
          id: "018f0000-0000-7000-8000-000000006402",
          generation_id: generationId,
          sequence: 1,
          kind: "generation",
          status: "succeeded",
          turn_index: null,
          tool_name: null,
          started_at: "2026-09-08T12:00:00Z",
          completed_at: "2026-09-08T12:00:01Z",
          duration_ms: 1000,
          input_bytes: 2048,
          output_bytes: 256,
          input_tokens: 100,
          output_tokens: 20,
          token_source: "provider",
          error_code: null,
          created_at: "2026-09-08T12:00:00Z",
          ...extra,
        },
        {
          id: "018f0000-0000-7000-8000-000000006403",
          generation_id: generationId,
          sequence: 2,
          kind: "model_turn",
          status: "succeeded",
          turn_index: 1,
          tool_name: null,
          started_at: "2026-09-08T12:00:00Z",
          completed_at: "2026-09-08T12:00:01Z",
          duration_ms: 900,
          input_bytes: 2048,
          output_bytes: 256,
          input_tokens: 100,
          output_tokens: 20,
          token_source: "provider",
          error_code: null,
          created_at: "2026-09-08T12:00:01Z",
        },
      ],
      meta: {
        generation_id: generationId,
        status: "completed",
        total: 2,
        input_bytes: 2048,
        output_bytes: 256,
        duration_ms: 1000,
        input_tokens: 100,
        output_tokens: 20,
        token_source: "provider",
      },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI run-step parsing", () => {
  it("parses the content-free timeline and byte totals", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => runStepResponse()),
    );

    const result = await getAiRunSteps(generationId);

    expect(result).toMatchObject({
      meta: {
        generationId,
        status: "completed",
        total: 2,
        inputBytes: 2048,
        outputBytes: 256,
        durationMs: 1000,
        inputTokens: 100,
        outputTokens: 20,
        tokenSource: "provider",
      },
      items: [
        { sequence: 1, kind: "generation", turnIndex: null },
        { sequence: 2, kind: "model_turn", turnIndex: 1 },
      ],
    });
  });

  it("fails closed if a step response contains prompt or content", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => runStepResponse({ content: "private answer" })),
    );

    await expect(getAiRunSteps(generationId)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });

  it("rejects partial or estimated token usage", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        runStepResponse({
          input_tokens: 100,
          output_tokens: null,
          token_source: "provider",
        }),
      ),
    );

    await expect(getAiRunSteps(generationId)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });
});
