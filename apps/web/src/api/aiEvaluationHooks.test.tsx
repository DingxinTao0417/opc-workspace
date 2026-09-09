import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useCreateAiEvaluation, useCreateAiEvaluationReview } from "./hooks";

const createAiEvaluationMock = vi.hoisted(() => vi.fn());
const createAiEvaluationReviewMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createAiEvaluation: createAiEvaluationMock,
    createAiEvaluationReview: createAiEvaluationReviewMock,
  };
});

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("AI evaluation hooks", () => {
  it("reuses one idempotency key for the same failed suite request and rotates it when the suite changes", async () => {
    createAiEvaluationMock.mockRejectedValue(new Error("offline"));
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const { result } = renderHook(() => useCreateAiEvaluation(), {
      wrapper: wrapperFor(queryClient),
    });
    const smoke = {
      providerId: "provider-1",
      providerVersion: 3,
      suiteKey: "smoke" as const,
    };

    await act(async () => {
      await expect(result.current.mutateAsync(smoke)).rejects.toThrow(
        "offline",
      );
      await expect(result.current.mutateAsync(smoke)).rejects.toThrow(
        "offline",
      );
      await expect(
        result.current.mutateAsync({
          ...smoke,
          suiteKey: "prompt_injection",
        }),
      ).rejects.toThrow("offline");
    });

    expect(createAiEvaluationMock).toHaveBeenCalledTimes(3);
    const firstKey = createAiEvaluationMock.mock.calls[0]?.[3];
    const retryKey = createAiEvaluationMock.mock.calls[1]?.[3];
    const changedSuiteKey = createAiEvaluationMock.mock.calls[2]?.[3];
    expect(typeof firstKey).toBe("string");
    expect(retryKey).toBe(firstKey);
    expect(changedSuiteKey).not.toBe(firstKey);
    expect(createAiEvaluationMock.mock.calls[0]?.slice(0, 3)).toEqual([
      "provider-1",
      3,
      "smoke",
    ]);
    expect(createAiEvaluationMock.mock.calls[2]?.slice(0, 3)).toEqual([
      "provider-1",
      3,
      "prompt_injection",
    ]);
  });

  it("scopes review retries to the exact evidence, decision, and reason", async () => {
    createAiEvaluationReviewMock.mockRejectedValue(new Error("offline"));
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const { result } = renderHook(() => useCreateAiEvaluationReview(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      group: {
        providerId: "provider-1",
        providerNameSnapshot: "Ollama",
        providerModelSnapshot: "qwen3",
        datasetVersion: 3,
        suiteKey: "full" as const,
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
        evidenceLevel: "repeated_runs" as const,
        readinessStatus: "review_candidate" as const,
        readinessReasons: [],
        lastCompletedAt: "2026-09-09T10:02:01Z",
      },
      decision: "accepted_for_local_use" as const,
      reason: "仅批准本机试用。",
    };

    await act(async () => {
      await expect(result.current.mutateAsync(input)).rejects.toThrow(
        "offline",
      );
      await expect(result.current.mutateAsync(input)).rejects.toThrow(
        "offline",
      );
      await expect(
        result.current.mutateAsync({ ...input, reason: "继续补充人工复核。" }),
      ).rejects.toThrow("offline");
    });

    expect(createAiEvaluationReviewMock).toHaveBeenCalledTimes(3);
    const firstKey = createAiEvaluationReviewMock.mock.calls[0]?.[1];
    const retryKey = createAiEvaluationReviewMock.mock.calls[1]?.[1];
    const changedKey = createAiEvaluationReviewMock.mock.calls[2]?.[1];
    expect(typeof firstKey).toBe("string");
    expect(retryKey).toBe(firstKey);
    expect(changedKey).not.toBe(firstKey);
    expect(createAiEvaluationReviewMock.mock.calls[0]?.[0]).toEqual(input);
  });
});
