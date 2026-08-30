import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AutomationRun } from "../types/models";
import {
  automationQueryKey,
  taskQueryKey,
  useRetryAutomationRun,
} from "./hooks";

const retryAutomationRunMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
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

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("automation hooks", () => {
  it("refreshes automation runs and task facts after retry creates a task", async () => {
    retryAutomationRunMock.mockResolvedValue(taskRun);
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
    expect(invalidate).toHaveBeenCalledWith({ queryKey: taskQueryKey });
  });
});
