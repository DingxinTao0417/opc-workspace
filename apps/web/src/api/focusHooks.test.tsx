import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  FocusReport,
  FocusSessionListResult,
  FocusSessionSnapshot,
} from "../types/models";
import { ApiError } from "./client";
import { useFocusCycleStore } from "../store/focus";
import { DEFAULT_FOCUS_SETTINGS, useSettingsStore } from "../store/settings";
import {
  focusSessionQueryKey,
  invalidateFocusActionFacts,
  focusReportQueryKey,
  focusSessionHistoryQueryKey,
  projectQueryKey,
  taskQueryKey,
  useCreateFocusSession,
  useFocusReportQuery,
  useFocusSessionHistoryQuery,
  useStopFocusSession,
  usePauseFocusSession,
  useRecoverFocusSession,
} from "./hooks";

const calls = vi.hoisted(() => ({
  create: vi.fn(),
  history: vi.fn(),
  report: vi.fn(),
  stop: vi.fn(),
  pause: vi.fn(),
  recover: vi.fn(),
  active: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createFocusSession: calls.create,
    getFocusReport: calls.report,
    getFocusSessions: calls.history,
    stopFocusSession: calls.stop,
    pauseFocusSession: calls.pause,
    recoverFocusSession: calls.recover,
    getActiveFocusSession: calls.active,
  };
});

function snapshot(
  status: "active" | "completed" = "active",
): FocusSessionSnapshot {
  return {
    serverNow:
      status === "completed" ? "2026-08-28T10:05:00Z" : "2026-08-28T10:00:00Z",
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

beforeEach(() => {
  vi.resetAllMocks();
  useFocusCycleStore.getState().resetCycle();
  useSettingsStore.getState().resetSettings();
  calls.active.mockResolvedValue(snapshot());
});
afterEach(cleanup);

describe("focus hooks", () => {
  it("does not resurrect an already-ended create when an idempotent retry returns an old same-second snapshot", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(snapshot());
    const client = createQueryClient();
    const empty = { ...snapshot(), session: null };
    client.setQueryData(focusSessionQueryKey, empty);
    calls.active.mockResolvedValue(empty);
    const { result } = renderHook(() => useCreateFocusSession(), {
      wrapper: wrapperFor(client),
    });
    const input = { taskId: null, plannedSeconds: 300 };
    await act(async () => {
      await result.current.mutateAsync(input).catch(() => undefined);
    });
    await act(async () => {
      await result.current.mutateAsync(input);
    });
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(empty);
    expect(useFocusCycleStore.getState().phase).toBe("idle");
    expect(calls.active).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it("cancels a GET started during a command before accepting its success", async () => {
    const client = createQueryClient();
    client.setQueryData(focusSessionQueryKey, snapshot());
    let finishPause!: (value: FocusSessionSnapshot) => void;
    calls.pause.mockImplementation(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finishPause = resolve;
        }),
    );
    const { result } = renderHook(() => usePauseFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() =>
      result.current.mutate({ id: snapshot().session!.id, expectedVersion: 1 }),
    );
    await waitFor(() => expect(finishPause).toBeTypeOf("function"));
    let finishRead!: (value: FocusSessionSnapshot) => void;
    let signal!: AbortSignal;
    const read = client
      .fetchQuery({
        queryKey: focusSessionQueryKey,
        queryFn: (context) => {
          signal = context.signal;
          return new Promise<FocusSessionSnapshot>((resolve) => {
            finishRead = resolve;
          });
        },
      })
      .catch(() => undefined);
    const paused = snapshot();
    paused.session!.status = "paused";
    paused.session!.version = 2;
    act(() => finishPause(paused));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(signal.aborted).toBe(true);
    finishRead(snapshot());
    await read;
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(paused);
    client.clear();
  });
  it("does not let a late stop response clear a different active session", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.stop.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const client = createQueryClient();
    client.setQueryData(focusSessionQueryKey, snapshot());
    const { result } = renderHook(() => useStopFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() =>
      result.current.mutate({ id: snapshot().session!.id, expectedVersion: 1 }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    const newer = snapshot();
    newer.session!.id = "018f0000-0000-7000-8000-000000000902";
    newer.serverNow = "2026-08-28T10:06:00Z";
    client.setQueryData(focusSessionQueryKey, newer);
    useFocusCycleStore.getState().beginWork(null, 4, null, newer.session!.id);
    act(() => finish(snapshot("completed")));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(newer);
    expect(useFocusCycleStore.getState()).toMatchObject({
      sessionId: newer.session!.id,
      phase: "work",
      completedCycles: 0,
    });
    client.clear();
  });

  it("does not rewind the same session after a competing resume", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.pause.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const client = createQueryClient();
    client.setQueryData(focusSessionQueryKey, snapshot());
    const { result } = renderHook(() => usePauseFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() =>
      result.current.mutate({ id: snapshot().session!.id, expectedVersion: 1 }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    const resumed = snapshot();
    resumed.session!.version = 3;
    client.setQueryData(focusSessionQueryKey, resumed);
    const paused = snapshot();
    paused.session!.status = "paused";
    paused.session!.version = 2;
    act(() => finish(paused));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(resumed);
    client.clear();
  });

  it("ignores an old create receipt after an authoritative empty snapshot", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.create.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const client = createQueryClient();
    const { result } = renderHook(() => useCreateFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() => result.current.mutate({ taskId: null, plannedSeconds: 300 }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    // Even within one server-clock second, the refreshed null is newer evidence.
    const empty = { ...snapshot(), session: null };
    client.setQueryData(focusSessionQueryKey, empty);
    act(() => finish(snapshot()));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(empty);
    expect(useFocusCycleStore.getState().phase).toBe("idle");
    client.clear();
  });

  it("binds a successful next round once using committed settings, not a page callback", async () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork(null, 2, null, "previous");
    cycle.completeWork(null, DEFAULT_FOCUS_SETTINGS, null, 1, "previous");
    cycle.finishBreak();
    calls.create.mockResolvedValueOnce(snapshot());
    const client = createQueryClient();
    const { result } = renderHook(() => useCreateFocusSession(), {
      wrapper: wrapperFor(client),
    });
    await act(async () => {
      await result.current.mutateAsync({ taskId: null, plannedSeconds: 300 });
    });
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      sessionId: snapshot().session!.id,
      completedCycles: 1,
      targetCycles: 2,
    });
    client.clear();
  });

  it("does not restore a locally reset sequence from a pending create callback", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.create.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const client = createQueryClient();
    const { result } = renderHook(() => useCreateFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() => result.current.mutate({ taskId: null, plannedSeconds: 300 }));
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    useFocusCycleStore.getState().resetCycle();
    act(() => finish(snapshot()));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(useFocusCycleStore.getState().phase).toBe("idle");
    // The authoritative slot remains available for the ticker to adopt as a
    // new sequence, rather than pretending the server creation was cancelled.
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(snapshot());
    client.clear();
  });

  it.each([true, false])(
    "settles only the matching session with auto-completion=%s",
    async (auto) => {
      const client = createQueryClient();
      client.setQueryData(focusSessionQueryKey, snapshot());
      useFocusCycleStore
        .getState()
        .beginWork(null, 2, null, snapshot().session!.id);
      calls.stop.mockResolvedValue(snapshot("completed"));
      const { result } = renderHook(() => useStopFocusSession(auto), {
        wrapper: wrapperFor(client),
      });
      await act(async () => {
        await result.current.mutateAsync({
          id: snapshot().session!.id,
          expectedVersion: 1,
        });
      });
      expect(useFocusCycleStore.getState()).toMatchObject({
        phase: auto ? "break" : "idle",
        completedCycles: auto ? 1 : 0,
      });
      // An idempotent replay must not count a second round.
      await act(async () => {
        await result.current.mutateAsync({
          id: snapshot().session!.id,
          expectedVersion: 1,
        });
      });
      expect(useFocusCycleStore.getState().completedCycles).toBe(auto ? 1 : 0);
      client.clear();
    },
  );

  it("does not reset a newer cycle after recovery interruption returns late", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    calls.recover.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    const client = createQueryClient();
    client.setQueryData(focusSessionQueryKey, snapshot());
    useFocusCycleStore
      .getState()
      .beginWork(null, 2, null, snapshot().session!.id);
    const { result } = renderHook(() => useRecoverFocusSession(), {
      wrapper: wrapperFor(client),
    });
    act(() =>
      result.current.mutate({
        id: snapshot().session!.id,
        expectedVersion: 1,
        action: "interrupt",
      }),
    );
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    const newer = snapshot();
    newer.session!.id = "newer-session";
    client.setQueryData(focusSessionQueryKey, newer);
    useFocusCycleStore.getState().beginWork(null, 2, null, newer.session!.id);
    const interrupted = snapshot("completed");
    interrupted.session!.status = "interrupted";
    act(() => finish(interrupted));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(client.getQueryData(focusSessionQueryKey)).toEqual(newer);
    expect(useFocusCycleStore.getState()).toMatchObject({
      sessionId: newer.session!.id,
      phase: "work",
    });
    client.clear();
  });
  it("cancels in-flight Focus snapshots before refreshing an AI decision without replaying cycle changes", async () => {
    const client = createQueryClient();
    const cycle = useFocusCycleStore.getState();
    client.setQueryData(focusSessionQueryKey, snapshot());
    client.setQueryData([...focusSessionHistoryQueryKey, {}], []);
    client.setQueryData([...focusReportQueryKey, {}], {});
    let finish!: (value: FocusSessionSnapshot) => void;
    const pending = client
      .fetchQuery({
        queryKey: focusSessionQueryKey,
        queryFn: () =>
          new Promise<FocusSessionSnapshot>((resolve) => {
            finish = resolve;
          }),
      })
      .catch(() => null);
    await invalidateFocusActionFacts(client);
    finish({ ...snapshot(), serverNow: "2099-01-01T00:00:00Z" });
    await pending;
    expect(
      client.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey)
        ?.serverNow,
    ).toBe("2026-08-28T10:00:00Z");
    for (const key of [
      focusSessionQueryKey,
      [...focusSessionHistoryQueryKey, {}],
      [...focusReportQueryKey, {}],
    ]) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    }
    expect(useFocusCycleStore.getState()).toBe(cycle);
    client.clear();
  });
  it("keeps project-scoped report and history queries in distinct caches", async () => {
    const reportInput = {
      dateFrom: "2026-08-22",
      dateTo: "2026-08-28",
      timezone: "UTC",
      projectId: "project-1",
    };
    const historyInput = {
      page: 1,
      pageSize: 6,
      status: "terminal" as const,
      projectId: "project-1",
    };
    const report = {
      dateFrom: reportInput.dateFrom,
      dateTo: reportInput.dateTo,
      timezone: "UTC",
      totals: { sessions: 0, seconds: 0, minutes: 0 },
      days: [],
      projects: [],
      hours: [],
      heatmap: [],
      tags: [],
      currentStreakDays: 0,
      longestStreakDays: 0,
    } satisfies FocusReport;
    const history = {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0 },
    } satisfies FocusSessionListResult;
    calls.report.mockResolvedValue(report);
    calls.history.mockResolvedValue(history);
    const queryClient = createQueryClient();
    const wrapper = wrapperFor(queryClient);
    const reportHook = renderHook(() => useFocusReportQuery(reportInput), {
      wrapper,
    });
    const historyHook = renderHook(
      () => useFocusSessionHistoryQuery(historyInput),
      { wrapper },
    );

    await waitFor(() => expect(reportHook.result.current.isSuccess).toBe(true));
    await waitFor(() =>
      expect(historyHook.result.current.isSuccess).toBe(true),
    );
    expect(calls.report).toHaveBeenCalledWith(reportInput);
    expect(calls.history).toHaveBeenCalledWith(historyInput);
    expect(
      queryClient.getQueryData([...focusReportQueryKey, reportInput]),
    ).toEqual(report);
    expect(
      queryClient.getQueryData([...focusSessionHistoryQueryKey, historyInput]),
    ).toEqual(history);
  });

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
