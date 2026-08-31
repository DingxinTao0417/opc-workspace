import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../types/models";
import { ApiError } from "./client";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  inboxDetailQueryKey,
  inboxQueryKey,
  inboxTaskRelationQueryKey,
  searchQueryKey,
  useCreateInboxItem,
  useInboxItemCommand,
  useInboxItemsQuery,
  useLinkInboxItemTask,
  useProjectArtifactsQuery,
  useSplitInboxItem,
} from "./hooks";

const calls = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  command: vi.fn(),
  linkTask: vi.fn(),
  projectArtifacts: vi.fn(),
  split: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getInboxItems: calls.list,
    createInboxItem: calls.create,
    executeInboxItemCommand: calls.command,
    getProjectArtifacts: calls.projectArtifacts,
    linkInboxItemTask: calls.linkTask,
    splitInboxItem: calls.split,
  };
});

function inboxItem(version = 1): InboxItem {
  return {
    id: "018f0000-0000-7000-8000-000000000801",
    kind: "manual",
    title: "确认本周交付范围",
    summary: "",
    sourceEntityType: "manual",
    sourceEntityId: null,
    sourceEventKey: null,
    sourceDeletedAt: null,
    priority: "P2",
    status: "open",
    resolutionPolicy: "manual",
    dueAt: null,
    readAt: version > 1 ? "2026-08-28T10:01:00Z" : null,
    triagedAt: "2026-08-28T10:00:00Z",
    snoozedUntil: null,
    resolvedByActorId: null,
    resolvedAt: null,
    resolutionReason: null,
    resolutionMode: null,
    dismissedByActorId: null,
    dismissedAt: null,
    dismissReason: null,
    payloadJson: {},
    version,
    createdAt: "2026-08-28T10:00:00Z",
    updatedAt: "2026-08-28T10:00:00Z",
    availableActions:
      version > 1
        ? ["edit", "snooze", "resolve", "dismiss"]
        : ["edit", "read", "snooze", "resolve", "dismiss"],
  };
}

const sourceProjectId = "018f0000-0000-7000-8000-000000000901";

function projectFollowupItem(version = 1, includeProject = true): InboxItem {
  const base = inboxItem(version);
  const artifactId = "018f0000-0000-7000-8000-000000000902";
  return {
    ...base,
    kind: "event",
    sourceEntityType: "task_artifact",
    sourceEntityId: artifactId,
    sourceEventKey: `task-artifact:${artifactId}:followup`,
    payloadJson: includeProject ? { project_id: sourceProjectId } : {},
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

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("inbox hooks", () => {
  it("refreshes the list so snoozed items return when the server clock reaches them", async () => {
    vi.useFakeTimers();
    calls.list.mockResolvedValue({
      items: [],
      meta: {
        page: 1,
        pageSize: 30,
        total: 0,
        unreadTotal: 0,
        snapshotAt: "2026-08-28T10:00:00.000000000Z",
        serverNow: "2026-08-28T10:00:00.000000000Z",
      },
    });
    const queryClient = createQueryClient();

    renderHook(() => useInboxItemsQuery(), {
      wrapper: wrapperFor(queryClient),
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(calls.list).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(INBOX_LIST_REFRESH_INTERVAL_MS);
    });
    expect(calls.list).toHaveBeenCalledTimes(2);
  });

  it("reuses one create idempotency key when the user retries the same draft", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(inboxItem());
    const input = {
      title: "确认本周交付范围",
      summary: "",
      priority: "P2" as const,
      dueAt: null,
    };
    const queryClient = createQueryClient();
    const searchKey = [
      ...searchQueryKey,
      { q: input.title, types: ["inbox_item"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const { result } = renderHook(() => useCreateInboxItem(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.create.mock.calls[0][1]).toBeTruthy();
    expect(calls.create.mock.calls[1][1]).toBe(calls.create.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(inboxDetailQueryKey(inboxItem().id)),
    ).toEqual(inboxItem());
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
  });

  it("reuses the command key after a lost response and caches the returned version", async () => {
    calls.command
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(inboxItem(2));
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useInboxItemCommand(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      action: "read" as const,
      id: inboxItem().id,
      expectedVersion: 1,
    };

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.command.mock.calls[1][1]).toBe(calls.command.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(inboxDetailQueryKey(inboxItem().id)),
    ).toEqual(inboxItem(2));
  });

  it("leaves one conflict refresh to the active Inbox detail editor", async () => {
    calls.command.mockRejectedValue(
      new ApiError("条目版本冲突", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useInboxItemCommand(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        action: "read",
        id: inboxItem().id,
        expectedVersion: 1,
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxQueryKey,
      predicate: expect.any(Function),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxDetailQueryKey(inboxItem().id),
      exact: true,
      refetchType: "none",
    });
  });

  it("reuses the relation key and invalidates Projects even when the source snapshot predates Project assignment", async () => {
    const mutationResult = {
      inboxItem: projectFollowupItem(2, false),
      relation: {
        id: "relation-1",
        inboxItemId: inboxItem().id,
        taskRefId: "task-1",
      },
      progress: {
        activeTotal: 1,
        requiredTotal: 1,
        requiredDone: 0,
        requiredRemaining: 1,
        requiredBlocked: 0,
        requiredWaitingReview: 0,
        requiredCancelled: 0,
        percent: 0,
        allRequiredDone: false,
      },
    };
    calls.linkTask
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(mutationResult);
    const queryClient = createQueryClient();
    const projectCacheKey = [
      "projects",
      "detail",
      sourceProjectId,
      "artifacts",
      "list",
      {},
    ] as const;
    queryClient.setQueryData(projectCacheKey, { cached: true });
    const { result } = renderHook(() => useLinkInboxItemTask(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      inboxItemId: inboxItem().id,
      taskId: "task-1",
      isRequired: true,
      expectedVersion: 1,
    };

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.linkTask.mock.calls[1][1]).toBe(
      calls.linkTask.mock.calls[0][1],
    );
    expect(
      queryClient.getQueryData(inboxDetailQueryKey(inboxItem().id)),
    ).toEqual(projectFollowupItem(2, false));
    expect(queryClient.getQueryState(projectCacheKey)?.isInvalidated).toBe(
      true,
    );
  });

  it("leaves one conflict refresh to the active Inbox task relation editor", async () => {
    calls.linkTask.mockRejectedValue(
      new ApiError("条目版本冲突", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useLinkInboxItemTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        inboxItemId: inboxItem().id,
        taskId: "task-1",
        isRequired: true,
        expectedVersion: 1,
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxQueryKey,
      predicate: expect.any(Function),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxDetailQueryKey(inboxItem().id),
      exact: true,
      refetchType: "none",
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxTaskRelationQueryKey(inboxItem().id),
      refetchType: "none",
    });
  });

  it("still refreshes the whole Inbox tree after a missing relation target", async () => {
    calls.linkTask.mockRejectedValue(
      new ApiError("条目不存在", { code: "NOT_FOUND", status: 404 }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useLinkInboxItemTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        inboxItemId: inboxItem().id,
        taskId: "task-1",
        isRequired: true,
        expectedVersion: 1,
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: inboxDetailQueryKey(inboxItem().id),
      exact: true,
      refetchType: "none",
    });
  });

  it("invalidates Task, Today, and Project read models after an atomic split", async () => {
    const sourceItem = projectFollowupItem(2);
    calls.split.mockResolvedValue({
      inboxItem: sourceItem,
      created: [],
      progress: {
        activeTotal: 1,
        requiredTotal: 1,
        requiredDone: 0,
        requiredRemaining: 1,
        requiredBlocked: 0,
        requiredWaitingReview: 0,
        requiredCancelled: 0,
        percent: 0,
        allRequiredDone: false,
      },
    });
    const queryClient = createQueryClient();
    const taskCacheKey = ["tasks", "list", { page: 1 }] as const;
    const todayCacheKey = ["stats", "today", "2026-08-29"] as const;
    const projectCacheKey = [
      "projects",
      "detail",
      sourceProjectId,
      "artifacts",
      "list",
      {},
    ] as const;
    queryClient.setQueryData(taskCacheKey, { cached: true });
    queryClient.setQueryData(todayCacheKey, { cached: true });
    queryClient.setQueryData(projectCacheKey, { cached: true });
    const { result } = renderHook(() => useSplitInboxItem(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        inboxItemId: sourceItem.id,
        expectedVersion: 1,
        resolutionPolicy: "all_required_tasks_done",
        tasks: [
          {
            key: "task-1",
            parentKey: null,
            title: "完成交付确认",
            description: "",
            kind: "work",
            priority: "P2",
            projectId: sourceProjectId,
            completionCriteria: "客户已确认",
            tagIds: [],
            dueDate: null,
            plannedDate: null,
            estimatedMinutes: null,
            reviewPolicy: "manual",
            isRequired: true,
            assigneeActorId: "owner-1",
          },
        ],
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(queryClient.getQueryState(taskCacheKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(todayCacheKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(projectCacheKey)?.isInvalidated).toBe(
      true,
    );
  });

  it("cancels a stale pending Project Artifact read before refetching follow-up facts", async () => {
    let resolveStale!: (value: {
      items: [];
      meta: {
        page: number;
        pageSize: number;
        total: number;
        projectVersion: number;
      };
    }) => void;
    const stale = new Promise<{
      items: [];
      meta: {
        page: number;
        pageSize: number;
        total: number;
        projectVersion: number;
      };
    }>((resolve) => {
      resolveStale = resolve;
    });
    calls.projectArtifacts.mockReturnValueOnce(stale).mockResolvedValueOnce({
      items: [],
      meta: { page: 1, pageSize: 20, total: 1, projectVersion: 2 },
    });
    calls.linkTask.mockResolvedValue({
      inboxItem: projectFollowupItem(2, false),
      relation: { id: "relation-1" },
      progress: {
        activeTotal: 1,
        requiredTotal: 1,
        requiredDone: 0,
        requiredRemaining: 1,
        requiredBlocked: 0,
        requiredWaitingReview: 0,
        requiredCancelled: 0,
        percent: 0,
        allRequiredDone: false,
      },
    });
    const queryClient = createQueryClient();
    const { result } = renderHook(
      () => ({
        artifacts: useProjectArtifactsQuery(sourceProjectId),
        link: useLinkInboxItemTask(),
      }),
      { wrapper: wrapperFor(queryClient) },
    );
    await waitFor(() =>
      expect(calls.projectArtifacts).toHaveBeenCalledTimes(1),
    );
    const firstSignal = calls.projectArtifacts.mock.calls[0][2] as AbortSignal;

    act(() =>
      result.current.link.mutate({
        inboxItemId: projectFollowupItem().id,
        taskId: "task-1",
        isRequired: true,
        expectedVersion: 1,
      }),
    );
    await waitFor(() =>
      expect(calls.projectArtifacts).toHaveBeenCalledTimes(2),
    );
    await waitFor(() =>
      expect(result.current.artifacts.data?.meta.total).toBe(1),
    );
    expect(firstSignal.aborted).toBe(true);

    resolveStale({
      items: [],
      meta: { page: 1, pageSize: 20, total: 0, projectVersion: 1 },
    });
    await act(async () => Promise.resolve());
    expect(result.current.artifacts.data?.meta.total).toBe(1);
  });
});
