import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AutomationRun, AutomationRunDetail } from "../types/models";
import {
  automationRunDetailQueryKey,
  automationQueryKey,
  inboxQueryKey,
  projectQueryKey,
  reminderQueryKey,
  searchQueryKey,
  taskQueryKey,
  useAutomationRunQuery,
  useAutomationRunsQuery,
  useRetryAutomationRun,
} from "./hooks";

const retryAutomationRunMock = vi.hoisted(() => vi.fn());
const getAutomationRunMock = vi.hoisted(() => vi.fn());
const getAutomationRunsMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getAutomationRun: getAutomationRunMock,
    getAutomationRuns: getAutomationRunsMock,
    retryAutomationRun: retryAutomationRunMock,
  };
});

const taskRun: AutomationRun = {
  id: "018f0000-0000-7000-8000-000000001610",
  ruleId: "00000000-0000-5000-8000-000000000104",
  presetKey: "invoice-overdue-task",
  ruleName: "发票逾期跟进",
  ruleVersion: 3,
  triggerType: "event",
  sourceEventId: "018f0000-0000-7000-8000-000000001611",
  scheduledFor: null,
  status: "succeeded",
  attempt: 2,
  retryOfRunId: "018f0000-0000-7000-8000-000000001609",
  retryable: false,
  retryAt: null,
  causedByRunId: null,
  causalDepth: 0,
  configSnapshot: { priority: "P1" },
  actionSnapshot: { action_type: "task", title: "跟进逾期发票" },
  errorCode: null,
  resultType: "task",
  resultId: "018f0000-0000-7000-8000-000000001612",
  resultSummary: "已创建本地发票跟进任务。",
  startedAt: "2026-08-30T10:00:01Z",
  endedAt: "2026-08-30T10:00:01Z",
};

const taskRunDetail: AutomationRunDetail = {
  ...taskRun,
  source: {
    kind: "event",
    available: true,
    eventId: taskRun.sourceEventId,
    aggregateType: "invoice",
    aggregateId: "018f0000-0000-7000-8000-000000001608",
    action: "invoice_overdue",
    occurredAt: "2026-08-30T10:00:00Z",
    scheduledFor: null,
  },
  retryChain: [
    {
      id: taskRun.id,
      status: taskRun.status,
      attempt: taskRun.attempt,
      retryOfRunId: taskRun.retryOfRunId,
      retryable: taskRun.retryable,
      retryAt: taskRun.retryAt,
      errorCode: taskRun.errorCode,
      resultType: taskRun.resultType,
      resultId: taskRun.resultId,
      resultSummary: taskRun.resultSummary,
      startedAt: taskRun.startedAt,
      endedAt: taskRun.endedAt,
    },
  ],
};

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("automation hooks", () => {
  it("uses the detail query key, forwards cancellation, and disables null ids", async () => {
    getAutomationRunMock.mockResolvedValue(taskRunDetail);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const wrapper = wrapperFor(queryClient);
    const disabled = renderHook(() => useAutomationRunQuery(null), { wrapper });
    expect(disabled.result.current.fetchStatus).toBe("idle");
    expect(getAutomationRunMock).not.toHaveBeenCalled();

    const enabled = renderHook(() => useAutomationRunQuery(taskRun.id), {
      wrapper,
    });
    await waitFor(() => expect(enabled.result.current.isSuccess).toBe(true));

    expect(getAutomationRunMock).toHaveBeenCalledWith(
      taskRun.id,
      expect.any(AbortSignal),
    );
    expect(
      queryClient.getQueryData(automationRunDetailQueryKey(taskRun.id)),
    ).toBe(taskRunDetail);
  });

  it("forwards the list query cancellation signal", async () => {
    const input = { status: "failed" as const, page: 2, pageSize: 5 };
    getAutomationRunsMock.mockResolvedValue({
      items: [],
      meta: { page: 2, pageSize: 5, total: 0 },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(() => useAutomationRunsQuery(input), {
      wrapper: wrapperFor(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getAutomationRunsMock).toHaveBeenCalledWith(
      input,
      expect.any(AbortSignal),
    );
  });

  it("refreshes automation runs and task facts after retry creates a task", async () => {
    retryAutomationRunMock.mockResolvedValue(taskRun);
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(
      automationRunDetailQueryKey(taskRun.id),
      taskRunDetail,
    );
    const searchKey = [
      ...searchQueryKey,
      { q: "跟进逾期发票", types: ["task"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useRetryAutomationRun(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate(taskRun.id));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: automationQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: reminderQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: taskQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["stats", "today"] });
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
    expect(
      queryClient.getQueryState(automationRunDetailQueryKey(taskRun.id))
        ?.isInvalidated,
    ).toBe(true);
  });

  it("does not refresh task facts when retry returns a non-task result", async () => {
    retryAutomationRunMock.mockResolvedValue({
      ...taskRun,
      resultType: "reminder",
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useRetryAutomationRun(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate(taskRun.id));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: automationQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: reminderQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: taskQueryKey });
  });
});
