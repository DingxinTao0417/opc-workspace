import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  Actor,
  Project,
  ProjectInput,
  Tag,
  TagInput,
  Task,
  TaskAssignment,
  TaskWorkflowEvent,
} from "../types/models";
import { ApiError } from "./client";
import {
  projectQueryKey,
  taskAssignmentQueryKey,
  taskDetailQueryKey,
  taskEventQueryKey,
  useCreateProject,
  useCreateTag,
  useCreateTaskAssignment,
  useTaskAssignmentsQuery,
  useTaskEventsQuery,
  useTaskLifecycleCommand,
  useTaskPageQuery,
  useTasksQuery,
} from "./hooks";

const createProjectMock = vi.hoisted(() => vi.fn());
const createTagMock = vi.hoisted(() => vi.fn());
const createTaskAssignmentMock = vi.hoisted(() => vi.fn());
const executeTaskLifecycleCommandMock = vi.hoisted(() => vi.fn());
const getAllActorsMock = vi.hoisted(() => vi.fn());
const getTaskAssignmentsMock = vi.hoisted(() => vi.fn());
const getTaskEventsMock = vi.hoisted(() => vi.fn());
const getTaskPageMock = vi.hoisted(() => vi.fn());
const getTasksMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createProject: createProjectMock,
    createTag: createTagMock,
    createTaskAssignment: createTaskAssignmentMock,
    executeTaskLifecycleCommand: executeTaskLifecycleCommandMock,
    getAllActors: getAllActorsMock,
    getTaskAssignments: getTaskAssignmentsMock,
    getTaskEvents: getTaskEventsMock,
    getTaskPage: getTaskPageMock,
    getTasks: getTasksMock,
  };
});

const input: ProjectInput = {
  name: "稳定重试项目",
  description: "验证响应丢失后的人工重试",
  clientId: null,
  startDate: null,
  dueDate: null,
  amountMinor: null,
  color: "#6E7BF2",
};

const project: Project = {
  id: "project-1",
  ...input,
  clientName: null,
  status: "planning",
  version: 1,
  archivedFromStatus: null,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  taskSummary: {
    total: 0,
    completed: 0,
    inProgress: 0,
    remaining: 0,
    progressPercent: 0,
    actualMinutes: 0,
  },
  invoiceCount: 0,
  availableActions: ["start", "archive"],
};

const task: Task = {
  id: "task-1",
  title: "整理交付清单",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P2",
  projectId: null,
  parentTaskId: null,
  completionCriteria: "",
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: "2026-08-27",
  estimatedMinutes: 0,
  actualMinutes: 0,
  manualOrder: null,
  version: 4,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  tags: [],
};

const owner: Actor = {
  id: "actor-owner",
  type: "owner",
  displayName: "我",
  status: "active",
  isBuiltin: true,
  notes: "",
  metadata: {},
  version: 1,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const assignment: TaskAssignment = {
  id: "assignment-1",
  taskId: task.id,
  role: "assignee",
  actorId: owner.id,
  actor: owner,
  assignedByActorId: owner.id,
  assignedByActor: owner,
  assignedAt: "2026-08-27T01:00:00Z",
  unassignedAt: null,
  reason: null,
  isActive: true,
  inferred: false,
};

const taskEvent: TaskWorkflowEvent = {
  id: "event-1",
  action: "task_completed",
  actor: owner,
  assignmentId: null,
  requestId: "request-1",
  commandSeq: 1,
  previous: { status: "in_progress", version: 4 },
  current: { status: "done", version: 5 },
  reason: null,
  createdAt: "2026-08-27T02:00:00Z",
};

const tagInput: TagInput = { name: "交付", color: "#6E7BF2" };
const tag: Tag = {
  id: "tag-1",
  ...tagInput,
  version: 1,
  createdAt: "2026-08-27T00:00:00Z",
};

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return wrapperFor(queryClient);
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => vi.clearAllMocks());

describe("useCreateProject", () => {
  it("reuses the same idempotency key when the same request is retried", async () => {
    createProjectMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(project);
    const { result } = renderHook(() => useCreateProject(), {
      wrapper: createWrapper(),
    });

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const firstKey = createProjectMock.mock.calls[0][1];
    const retryKey = createProjectMock.mock.calls[1][1];
    expect(firstKey).toBeTruthy();
    expect(retryKey).toBe(firstKey);
  });
});

describe("task queries", () => {
  it("returns the complete page envelope from useTaskPageQuery", async () => {
    getTaskPageMock.mockResolvedValue({
      items: [task],
      meta: { page: 2, pageSize: 25, total: 51 },
    });
    const params = {
      page: 2,
      pageSize: 25,
      q: "交付",
      tagIds: ["tag-1"],
      rootOnly: true,
      sort: "manual_order",
    };
    const { result } = renderHook(() => useTaskPageQuery(params), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getTaskPageMock).toHaveBeenCalledWith(params);
    expect(result.current.data).toEqual({
      items: [task],
      meta: { page: 2, pageSize: 25, total: 51 },
    });
  });

  it("keeps the legacy useTasksQuery result as Task[]", async () => {
    getTasksMock.mockResolvedValue([task]);
    const { result } = renderHook(() => useTasksQuery(), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([task]);
    expect(Array.isArray(result.current.data)).toBe(true);
  });

  it("loads assignment history pages under a task-scoped query key", async () => {
    getTaskAssignmentsMock.mockImplementation(
      async (_taskId: string, input: { page: number }) => ({
        active: { assignee: assignment, reviewer: null },
        history:
          input.page === 1
            ? [
                {
                  ...assignment,
                  id: "history-1",
                  isActive: false,
                  unassignedAt: "2026-08-27T02:00:00Z",
                  reason: "转交",
                },
              ]
            : [
                {
                  ...assignment,
                  id: "history-2",
                  isActive: false,
                  unassignedAt: "2026-08-27T01:30:00Z",
                  reason: "结束",
                },
              ],
        meta: { page: input.page, pageSize: 1, total: 2, taskVersion: 4 },
      }),
    );
    const { result } = renderHook(
      () => useTaskAssignmentsQuery(task.id, { pageSize: 1 }),
      { wrapper: createWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    let fetchedPageCount = 0;
    await act(async () => {
      const next = await result.current.fetchNextPage();
      fetchedPageCount = next.data?.pages.length ?? 0;
    });
    expect(getTaskAssignmentsMock).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({ page: 2, pageSize: 1 }),
    );
    expect(fetchedPageCount).toBe(2);
  });

  it("loads task event pages under a separate task-scoped key", async () => {
    getTaskEventsMock.mockImplementation(
      async (_taskId: string, input: { page: number }) => ({
        items: [{ ...taskEvent, id: `event-${input.page}` }],
        meta: { page: input.page, pageSize: 1, total: 2, taskVersion: 5 },
      }),
    );
    const { result } = renderHook(
      () => useTaskEventsQuery(task.id, { pageSize: 1 }),
      { wrapper: createWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await act(async () => {
      await result.current.fetchNextPage();
    });
    expect(getTaskEventsMock).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({ page: 2, pageSize: 1 }),
    );
  });
});

describe("controlled task lifecycle mutations", () => {
  it("requires the observed version and invalidates every affected view", async () => {
    executeTaskLifecycleCommandMock.mockResolvedValue({
      task: { ...task, status: "done", version: 5 },
      event: taskEvent,
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(taskDetailQueryKey(task.id), task);
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTaskLifecycleCommand(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: task.id,
        input: { action: "complete", expectedVersion: task.version },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(executeTaskLifecycleCommandMock).toHaveBeenCalledWith(
      task.id,
      { action: "complete", expectedVersion: task.version },
      expect.any(String),
    );
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskAssignmentQueryKey(task.id),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskEventQueryKey(task.id),
    });
  });

  it("reuses one idempotency key for the same failed command retry", async () => {
    executeTaskLifecycleCommandMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce({
        task: { ...task, status: "blocked", version: 5 },
        event: { ...taskEvent, action: "task_blocked" },
      });
    const { result } = renderHook(() => useTaskLifecycleCommand(), {
      wrapper: createWrapper(),
    });
    const variables = {
      id: task.id,
      input: {
        action: "block" as const,
        reason: "等待客户",
        expectedVersion: task.version,
      },
    };

    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(executeTaskLifecycleCommandMock.mock.calls[0][2]).toBeTruthy();
    expect(executeTaskLifecycleCommandMock.mock.calls[1][2]).toBe(
      executeTaskLifecycleCommandMock.mock.calls[0][2],
    );
  });

  it("refreshes project and today aggregates after a version conflict", async () => {
    executeTaskLifecycleCommandMock.mockRejectedValue(
      new ApiError("任务版本冲突", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTaskLifecycleCommand(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: task.id,
        input: { action: "complete", expectedVersion: task.version },
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: projectQueryKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["stats", "today"],
    });
  });
});

describe("assignment mutations", () => {
  it("reuses the same idempotency key when a failed assignment is retried", async () => {
    createTaskAssignmentMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce({
        assignment,
        task: { ...task, version: task.version + 1 },
      });
    const { result } = renderHook(() => useCreateTaskAssignment(), {
      wrapper: createWrapper(),
    });
    const variables = {
      taskId: task.id,
      input: {
        role: "assignee" as const,
        actorId: owner.id,
        expectedVersion: task.version,
      },
    };

    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(createTaskAssignmentMock.mock.calls[0][2]).toBeTruthy();
    expect(createTaskAssignmentMock.mock.calls[1][2]).toBe(
      createTaskAssignmentMock.mock.calls[0][2],
    );
  });

  it("does not replace a newer cached task with an idempotent replay snapshot", async () => {
    createTaskAssignmentMock.mockResolvedValue({
      assignment,
      task: { ...task, version: 5 },
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(taskDetailQueryKey(task.id), {
      ...task,
      title: "更新后的缓存",
      version: 7,
    });
    const { result } = renderHook(() => useCreateTaskAssignment(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        taskId: task.id,
        input: {
          role: "assignee",
          actorId: owner.id,
          expectedVersion: 4,
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(
      queryClient.getQueryData<Task>(taskDetailQueryKey(task.id)),
    ).toMatchObject({ title: "更新后的缓存", version: 7 });
  });
});

describe("useCreateTag", () => {
  it("reuses its idempotency key for the same retried tag", async () => {
    createTagMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(tag);
    const { result } = renderHook(() => useCreateTag(), {
      wrapper: createWrapper(),
    });

    act(() => result.current.mutate(tagInput));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(tagInput));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const firstKey = createTagMock.mock.calls[0][1];
    const retryKey = createTagMock.mock.calls[1][1];
    expect(firstKey).toBeTruthy();
    expect(retryKey).toBe(firstKey);
  });
});
