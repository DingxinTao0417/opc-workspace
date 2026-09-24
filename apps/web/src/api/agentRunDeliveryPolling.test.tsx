import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentRun, AgentRunSummary } from "../types/models";
import { getAgentRun, getAgentRuns, getTaskAgentRuns } from "./client";
import {
  agentRunListQueryKey,
  agentRunQueryRootKey,
  aiAgentInboxQueryKey,
  aiWorkPlanInboxQueryKey,
  inboxStatsQueryKey,
  invalidateAgentRunDeliveryFacts,
  projectArtifactQueryKey,
  searchQueryKey,
  taskAgentRunsQueryKey,
  taskArtifactQueryKey,
  taskAssignmentQueryKey,
  taskDetailQueryKey,
  taskEventQueryKey,
  taskSubmissionQueryKey,
  useAgentRunQuery,
  useAgentRunsQuery,
  useTaskAgentRunsQuery,
} from "./hooks";

vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  getAgentRun: vi.fn(),
  getAgentRuns: vi.fn(),
  getTaskAgentRuns: vi.fn(),
}));

const taskId = "018f0000-0000-7000-8000-000000000601";
const runId = "018f0000-0000-7000-8000-000000000602";
const aiProjectionKeys = [
  aiAgentInboxQueryKey,
  aiWorkPlanInboxQueryKey,
  ["ai", "work-plan", "018f0000-0000-7000-8000-000000000610"],
  ["ai", "actions", "018f0000-0000-7000-8000-000000000611"],
] as const;

function run(overrides: Partial<AgentRun> = {}): AgentRun {
  return {
    id: runId,
    taskId,
    assignmentId: "018f0000-0000-7000-8000-000000000603",
    actorId: "018f0000-0000-7000-8000-000000000604",
    adapterId: "018f0000-0000-7000-8000-000000000605",
    createdByActorId: "018f0000-0000-7000-8000-000000000606",
    parentRunId: null,
    attempt: 1,
    status: "queued",
    providerId: "018f0000-0000-7000-8000-000000000607",
    model: "test-model",
    resultText: null,
    resultBytes: null,
    errorCode: null,
    outputDeliveryStatus: "not_ready",
    outputDeliveryErrorCode: null,
    submissionId: null,
    artifactId: null,
    startedAt: null,
    completedAt: null,
    createdAt: "2026-09-18T10:00:00Z",
    ...overrides,
  };
}

function summary(value: AgentRun, taskTitle = "任务甲"): AgentRunSummary {
  const { resultText: _resultText, ...metadata } = value;
  return { ...metadata, taskTitle };
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetAllMocks();
});

describe("Agent Run delivery polling", () => {
  it("refreshes every matching approval generation without invalidating trusted unrelated groups", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const action = (
      generationId: string,
      id: string,
      name = "agent_run.start",
    ) => ({
      generation_id: generationId,
      action: { action: name, task_id: id },
      status: "confirmed",
      can_confirm: false,
    });
    const relatedKeys = [
      ["ai", "actions", "start-generation"],
      ["ai", "actions", "cancel-generation"],
      ["ai", "actions", "mixed-generation"],
    ];
    client.setQueryData(relatedKeys[0], [action("start-generation", taskId)]);
    client.setQueryData(relatedKeys[1], [
      action("cancel-generation", taskId, "agent_run.cancel"),
    ]);
    client.setQueryData(relatedKeys[2], [
      action("mixed-generation", "other-task", "task.update"),
      action("mixed-generation", taskId, "agent_run.recover_output"),
    ]);
    const unrelatedKeys = [
      ["ai", "actions", "other-generation"],
      ["ai", "actions", "ordinary-generation"],
    ];
    client.setQueryData(unrelatedKeys[0], [
      action("other-generation", "other-task"),
    ]);
    client.setQueryData(unrelatedKeys[1], [
      action("ordinary-generation", taskId, "task.update"),
    ]);
    const uncertainKeys = [
      ["ai", "actions", "empty-generation"],
      ["ai", "actions", "partial-generation"],
      ["ai", "actions", "mismatched-cache-generation"],
    ];
    client.setQueryData(uncertainKeys[0], []);
    client.setQueryData(uncertainKeys[1], [
      {
        ...action("partial-generation", "other-task", "task.update"),
        status: "pending",
      },
    ]);
    client.setQueryData(uncertainKeys[2], [
      action("wrong-generation", "other-task"),
    ]);

    await invalidateAgentRunDeliveryFacts(client, taskId);

    for (const key of [...relatedKeys, ...uncertainKeys])
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    for (const key of unrelatedKeys)
      expect(client.getQueryState(key)?.isInvalidated).toBe(false);
    expect(getAgentRun).not.toHaveBeenCalled();
    client.clear();
  });

  it.each([true, false])(
    "cancels a late approval read before refreshing terminal receipts (cached=%s)",
    async (cached) => {
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false } },
      });
      const key = ["ai", "actions", "receipt-generation"];
      const old = [
        {
          generation_id: "receipt-generation",
          action: { action: "agent_run.start", task_id: taskId },
          status: "confirmed",
          can_confirm: false,
          agent_run_result: { id: runId, status: "running" },
        },
      ];
      const fresh = [
        { ...old[0], agent_run_result: { id: runId, status: "succeeded" } },
      ];
      if (cached) client.setQueryData(key, old);
      let resolveOld!: (data: typeof old) => void;
      const read = vi
        .fn()
        .mockImplementationOnce(
          () =>
            new Promise<typeof old>((resolve) => {
              resolveOld = resolve;
            }),
        )
        .mockResolvedValue(fresh);
      const wrapper = ({ children }: PropsWithChildren) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      );
      const { result } = renderHook(
        () => useQuery({ queryKey: key, queryFn: read }),
        { wrapper },
      );
      await vi.waitFor(() => expect(resolveOld).toBeDefined());

      await act(async () => invalidateAgentRunDeliveryFacts(client, taskId));
      await vi.waitFor(() => expect(result.current.data).toEqual(fresh));
      expect(read).toHaveBeenCalledTimes(2);
      await act(async () => {
        resolveOld(old);
      });
      expect(client.getQueryData(key)).toEqual(fresh);
      expect(result.current.data).toEqual(fresh);
      expect(read).toHaveBeenCalledTimes(2);
      client.clear();
    },
  );
  it("polls the exact Task+Run through pending, stops at submitted, and refreshes dependent facts", async () => {
    vi.useFakeTimers();
    const queued = run();
    const pending = run({
      status: "running",
      startedAt: "2026-09-18T10:00:01Z",
      resultText: null,
      resultBytes: null,
      outputDeliveryStatus: "pending",
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
    });
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "deliverable",
      resultBytes: 11,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getAgentRun)
      .mockResolvedValueOnce(queued)
      .mockResolvedValueOnce(pending)
      .mockResolvedValue(submitted);

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const dependentKeys = [
      taskAgentRunsQueryKey(taskId),
      agentRunListQueryKey(),
      taskDetailQueryKey(taskId),
      taskSubmissionQueryKey(taskId),
      taskArtifactQueryKey(taskId),
      taskAssignmentQueryKey(taskId),
      taskEventQueryKey(taskId),
      projectArtifactQueryKey("project"),
      inboxStatsQueryKey,
      [...searchQueryKey, { query: "deliverable" }],
      ["stats", "today"],
      ...aiProjectionKeys,
    ];
    dependentKeys.forEach((key) => client.setQueryData(key, "stale"));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useAgentRunQuery(runId, taskId), {
      wrapper,
    });

    await vi.waitFor(() => expect(result.current.data).toEqual(queued));
    expect(getAgentRun).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    await vi.waitFor(() => expect(result.current.data).toEqual(pending));
    expect(getAgentRun).toHaveBeenCalledTimes(2);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    await vi.waitFor(() => expect(result.current.data).toEqual(submitted));
    await vi.waitFor(() =>
      expect(client.getQueryState(dependentKeys[0])?.isInvalidated).toBe(true),
    );
    expect(getAgentRun).toHaveBeenCalledTimes(3);
    for (const key of dependentKeys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    }

    await act(async () => vi.advanceTimersByTimeAsync(10_000));
    expect(getAgentRun).toHaveBeenCalledTimes(3);
    client.clear();
  });

  it("refreshes delivery facts when only the task Run list observes pending becoming submitted", async () => {
    vi.useFakeTimers();
    const pending = run({
      status: "running",
      startedAt: "2026-09-18T10:00:01Z",
      resultText: null,
      resultBytes: null,
      outputDeliveryStatus: "pending",
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
    });
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "deliverable",
      resultBytes: 11,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getTaskAgentRuns)
      .mockResolvedValueOnce([pending])
      .mockResolvedValue([submitted]);

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const dependentKeys = [
      agentRunListQueryKey(),
      taskDetailQueryKey(taskId),
      taskSubmissionQueryKey(taskId),
      taskArtifactQueryKey(taskId),
      taskAssignmentQueryKey(taskId),
      taskEventQueryKey(taskId),
      projectArtifactQueryKey("project"),
      inboxStatsQueryKey,
      [...searchQueryKey, { query: "deliverable" }],
      ["stats", "today"],
      ...aiProjectionKeys,
    ];
    dependentKeys.forEach((key) => client.setQueryData(key, "stale"));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useTaskAgentRunsQuery(taskId), {
      wrapper,
    });

    await vi.waitFor(() => expect(result.current.data).toEqual([pending]));
    expect(getTaskAgentRuns).toHaveBeenCalledTimes(1);

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    await vi.waitFor(() => expect(result.current.data).toEqual([submitted]));
    await vi.waitFor(() => expect(getTaskAgentRuns).toHaveBeenCalledTimes(3));
    for (const key of dependentKeys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    }

    await act(async () => vi.advanceTimersByTimeAsync(10_000));
    expect(getTaskAgentRuns).toHaveBeenCalledTimes(3);
    expect(getAgentRun).not.toHaveBeenCalled();
    client.clear();
  });

  it("invalidates and cancels stale Task facts when the first list response is already terminal", async () => {
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "deliverable",
      resultBytes: 11,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getTaskAgentRuns).mockResolvedValue([submitted]);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const taskKey = taskDetailQueryKey(taskId);
    client.setQueryData(taskKey, { status: "in_progress" });
    let staleTaskRequestStarted = false;
    let staleTaskRequestAborted = false;
    const staleTaskRequest = client
      .fetchQuery({
        queryKey: taskKey,
        queryFn: ({ signal }) =>
          new Promise((_resolve, reject) => {
            staleTaskRequestStarted = true;
            signal.addEventListener("abort", () => {
              staleTaskRequestAborted = true;
              reject(new DOMException("aborted", "AbortError"));
            });
          }),
      })
      .catch(() => undefined);
    await vi.waitFor(() => expect(staleTaskRequestStarted).toBe(true));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useTaskAgentRunsQuery(taskId), {
      wrapper,
    });

    await vi.waitFor(() => expect(result.current.data).toEqual([submitted]));
    await vi.waitFor(() => expect(staleTaskRequestAborted).toBe(true));
    await staleTaskRequest;
    await vi.waitFor(() => expect(getTaskAgentRuns).toHaveBeenCalledTimes(2));
    expect(client.getQueryState(taskKey)?.isInvalidated).toBe(true);

    await new Promise((resolve) => window.setTimeout(resolve, 20));
    expect(getTaskAgentRuns).toHaveBeenCalledTimes(2);
    client.clear();
  });

  it("refreshes Task output facts when the global Run list observes active delivery become terminal", async () => {
    vi.useFakeTimers();
    const pending = run({
      status: "running",
      startedAt: "2026-09-18T10:00:01Z",
      outputDeliveryStatus: "pending",
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
    });
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "deliverable",
      resultBytes: 11,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getAgentRuns)
      .mockResolvedValueOnce({
        items: [summary(pending)],
        meta: {
          page: 1,
          pageSize: 50,
          total: 1,
          activeTotal: 0,
          pendingDeliveryTotal: 1,
          succeededTotal: 0,
        },
      })
      .mockResolvedValue({
        items: [summary(submitted)],
        meta: {
          page: 1,
          pageSize: 50,
          total: 1,
          activeTotal: 0,
          pendingDeliveryTotal: 0,
          succeededTotal: 1,
        },
      });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const dependentKeys = [
      taskDetailQueryKey(taskId),
      taskSubmissionQueryKey(taskId),
      taskArtifactQueryKey(taskId),
      taskAssignmentQueryKey(taskId),
      taskEventQueryKey(taskId),
      ["stats", "today"],
      ...aiProjectionKeys,
    ];
    dependentKeys.forEach((key) => client.setQueryData(key, "stale"));
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useAgentRunsQuery({ pageSize: 50 }), {
      wrapper,
    });

    await vi.waitFor(() =>
      expect(result.current.data?.items[0].outputDeliveryStatus).toBe(
        "pending",
      ),
    );
    for (const key of dependentKeys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(false);
    }

    await act(async () => vi.advanceTimersByTimeAsync(5_000));
    await vi.waitFor(() =>
      expect(result.current.data?.items[0].outputDeliveryStatus).toBe(
        "submitted",
      ),
    );
    await vi.waitFor(() =>
      expect(client.getQueryState(dependentKeys[0])?.isInvalidated).toBe(true),
    );
    for (const key of dependentKeys) {
      expect(client.getQueryState(key)?.isInvalidated).toBe(true);
    }
    client.clear();
  });

  it("does not treat initial terminal history in the global list as a new transition", async () => {
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "historical output",
      resultBytes: 17,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getAgentRuns).mockResolvedValue({
      items: [summary(submitted, "历史任务")],
      meta: {
        page: 1,
        pageSize: 50,
        total: 1,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 1,
      },
    });
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const taskKey = taskDetailQueryKey(taskId);
    client.setQueryData(taskKey, "fresh");
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useAgentRunsQuery({ pageSize: 50 }), {
      wrapper,
    });

    await vi.waitFor(() => expect(result.current.data?.items).toHaveLength(1));
    await new Promise((resolve) => window.setTimeout(resolve, 20));

    expect(client.getQueryState(taskKey)?.isInvalidated).toBe(false);
    expect(getAgentRuns).toHaveBeenCalledTimes(1);
    client.clear();
  });

  it("deduplicates the terminal refresh when list and exact Run hooks observe it together", async () => {
    vi.useFakeTimers();
    const queued = run();
    const submitted = run({
      status: "succeeded",
      startedAt: "2026-09-18T10:00:01Z",
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "deliverable",
      resultBytes: 11,
      outputDeliveryStatus: "submitted",
      submissionId: "018f0000-0000-7000-8000-000000000608",
      artifactId: "018f0000-0000-7000-8000-000000000609",
    });
    vi.mocked(getAgentRun)
      .mockResolvedValueOnce(queued)
      .mockResolvedValue(submitted);
    vi.mocked(getTaskAgentRuns)
      .mockResolvedValueOnce([queued])
      .mockResolvedValue([submitted]);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(
      () => ({
        exact: useAgentRunQuery(runId, taskId),
        list: useTaskAgentRunsQuery(taskId),
      }),
      { wrapper },
    );

    await vi.waitFor(() => expect(result.current.exact.data).toEqual(queued));
    await vi.waitFor(() => expect(result.current.list.data).toEqual([queued]));

    await act(async () => vi.advanceTimersByTimeAsync(2_000));
    await vi.waitFor(() =>
      expect(result.current.exact.data).toEqual(submitted),
    );
    await vi.waitFor(() =>
      expect(result.current.list.data).toEqual([submitted]),
    );
    await vi.waitFor(() =>
      expect(
        invalidate.mock.calls.filter(
          ([filters]) => filters?.queryKey === agentRunQueryRootKey,
        ),
      ).toHaveLength(1),
    );

    await act(async () => vi.advanceTimersByTimeAsync(10_000));
    expect(
      invalidate.mock.calls.filter(
        ([filters]) => filters?.queryKey === agentRunQueryRootKey,
      ),
    ).toHaveLength(1);
    client.clear();
  });
});
