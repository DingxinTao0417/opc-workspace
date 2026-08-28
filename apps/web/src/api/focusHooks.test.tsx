import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
import { ApiError } from "./client";
import {
  focusSessionQueryKey,
  projectQueryKey,
  taskQueryKey,
  useCreateFocusSession,
  useStopFocusSession,
} from "./hooks";

const calls = vi.hoisted(() => ({
  create: vi.fn(),
  stop: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createFocusSession: calls.create,
    stopFocusSession: calls.stop,
  };
});

function snapshot(
  status: "active" | "completed" = "active",
): FocusSessionSnapshot {
  return {
    serverNow: "2026-08-28T10:00:00Z",
    receivedAtMs: 1,
    session: {
      id: "018f0000-0000-7000-8000-000000000901",
      taskId: null,
      taskTitle: null,
      status,
      plannedSeconds: 300,
      accumulatedSeconds: status === "completed" ? 300 : 0,
      startedAt: "2026-08-28T10:00:00Z",
      endedAt: status === "completed" ? "2026-08-28T10:05:00Z" : null,
      lastResumedAt: status === "active" ? "2026-08-28T10:00:00Z" : null,
      lastHeartbeatAt: "2026-08-28T10:00:00Z",
      endReason: status === "completed" ? "completed" : null,
      version: status === "completed" ? 2 : 1,
      createdAt: "2026-08-28T10:00:00Z",
      updatedAt: "2026-08-28T10:00:00Z",
    },
  };
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("focus hooks", () => {
  it("reuses one create idempotency key after a lost response", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(snapshot());
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCreateFocusSession(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = { taskId: null, plannedSeconds: 300 };

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.create.mock.calls[0][1]).toBeTruthy();
    expect(calls.create.mock.calls[1][1]).toBe(calls.create.mock.calls[0][1]);
    expect(queryClient.getQueryData(focusSessionQueryKey)).toEqual(snapshot());
  });

  it("retries stop with one key and clears the active cache only after success", async () => {
    calls.stop
      .mockRejectedValueOnce(
        new ApiError("response lost", { code: "NETWORK_ERROR" }),
      )
      .mockResolvedValueOnce(snapshot("completed"));
    const queryClient = createQueryClient();
    queryClient.setQueryData(focusSessionQueryKey, snapshot());
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useStopFocusSession(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: "018f0000-0000-7000-8000-000000000901",
        expectedVersion: 1,
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.stop).toHaveBeenCalledTimes(2);
    expect(calls.stop.mock.calls[1][2]).toBe(calls.stop.mock.calls[0][2]);
    expect(
      queryClient.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey)
        ?.session,
    ).toBeNull();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusSessionQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: taskQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["stats", "today"],
    });
  });
});
