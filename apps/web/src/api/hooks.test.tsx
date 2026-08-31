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
  TaskArtifactSummary,
  TaskAssignment,
  TaskSubmission,
  TaskWorkflowEvent,
} from "../types/models";
import { ApiError } from "./client";
import {
  projectQueryKey,
  roadmapMilestoneQueryKey,
  searchQueryKey,
  taskQueryKey,
  taskAssignmentQueryKey,
  taskAssignmentQueryRootKey,
  taskArtifactDetailQueryKey,
  taskArtifactQueryKey,
  taskArtifactQueryRootKey,
  taskDetailQueryKey,
  taskEventQueryKey,
  taskEventQueryRootKey,
  taskSubmissionQueryKey,
  taskSubmissionQueryRootKey,
  useCreateProject,
  useCreateTag,
  useCreateTaskAssignment,
  useDownloadTaskArtifact,
  useTaskAssignmentsQuery,
  useTaskEventsQuery,
  useTaskArtifactsQuery,
  useTaskSubmissionsQuery,
  useTaskLifecycleCommand,
  useSubmitTaskOutput,
  useReviewTaskSubmission,
  useSearchQuery,
  useMoveTaskAcrossPlans,
  useMoveTaskWithinPlan,
  useReorderActiveTasksWithinPlan,
  useReorderTaskWithinPlanStatus,
  useSetTaskPlannedDate,
  useSidebarWeekTasksQuery,
  useDeleteTaskArtifact,
  useDeleteTask,
  useTaskPageQuery,
  useTodayTaskGroupsQuery,
  useTodayStatsQuery,
  useTasksQuery,
  useUpdateTask,
} from "./hooks";

const createProjectMock = vi.hoisted(() => vi.fn());
const createTagMock = vi.hoisted(() => vi.fn());
const createTaskAssignmentMock = vi.hoisted(() => vi.fn());
const downloadTaskArtifactMock = vi.hoisted(() => vi.fn());
const executeTaskLifecycleCommandMock = vi.hoisted(() => vi.fn());
const getAllActorsMock = vi.hoisted(() => vi.fn());
const getTaskAssignmentsMock = vi.hoisted(() => vi.fn());
const getTaskEventsMock = vi.hoisted(() => vi.fn());
const getTaskArtifactsMock = vi.hoisted(() => vi.fn());
const getTaskSubmissionsMock = vi.hoisted(() => vi.fn());
const submitTaskOutputMock = vi.hoisted(() => vi.fn());
const reviewTaskSubmissionMock = vi.hoisted(() => vi.fn());
const deleteTaskArtifactMock = vi.hoisted(() => vi.fn());
const deleteTaskMock = vi.hoisted(() => vi.fn());
const getTaskPageMock = vi.hoisted(() => vi.fn());
const getSearchResultsMock = vi.hoisted(() => vi.fn());
const getTodayStatsMock = vi.hoisted(() => vi.fn());
const getTasksMock = vi.hoisted(() => vi.fn());
const getAllTasksMock = vi.hoisted(() => vi.fn());
const reorderTasksMock = vi.hoisted(() => vi.fn());
const batchUpdateTasksMock = vi.hoisted(() => vi.fn());
const getTaskMock = vi.hoisted(() => vi.fn());
const updateTaskMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    batchUpdateTasks: batchUpdateTasksMock,
    createProject: createProjectMock,
    createTag: createTagMock,
    createTaskAssignment: createTaskAssignmentMock,
    downloadTaskArtifact: downloadTaskArtifactMock,
    executeTaskLifecycleCommand: executeTaskLifecycleCommandMock,
    getAllActors: getAllActorsMock,
    getTaskAssignments: getTaskAssignmentsMock,
    getTaskEvents: getTaskEventsMock,
    getTaskArtifacts: getTaskArtifactsMock,
    getTaskSubmissions: getTaskSubmissionsMock,
    submitTaskOutput: submitTaskOutputMock,
    reviewTaskSubmission: reviewTaskSubmissionMock,
    deleteTaskArtifact: deleteTaskArtifactMock,
    deleteTask: deleteTaskMock,
    getTaskPage: getTaskPageMock,
    getSearchResults: getSearchResultsMock,
    getTodayStats: getTodayStatsMock,
    getTask: getTaskMock,
    getTasks: getTasksMock,
    getAllTasks: getAllTasksMock,
    reorderTasks: reorderTasksMock,
    updateTask: updateTaskMock,
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
  currentSubmissionId: null,
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
  submissionId: null,
  artifactId: null,
  requestId: "request-1",
  commandSeq: 1,
  previous: { status: "in_progress", version: 4 },
  current: { status: "done", version: 5 },
  reason: null,
  createdAt: "2026-08-27T02:00:00Z",
};

const artifact: TaskArtifactSummary = {
  id: "artifact-1",
  taskId: task.id,
  submissionId: "submission-1",
  submissionStatus: "pending_review",
  position: 1,
  storageKind: "text",
  name: "交付说明",
  mimeType: null,
  sizeBytes: null,
  sha256: null,
  requiresFollowup: false,
  producedByActorId: owner.id,
  producedByActor: owner,
  recordedByActorId: owner.id,
  recordedByActor: owner,
  integrityStatus: "unverified",
  integrityCheckedAt: null,
  deletedAt: null,
  deletedByActorId: null,
  deletedByActor: null,
  deleteReason: null,
  createdAt: "2026-08-27T02:00:00Z",
};

const submission: TaskSubmission = {
  id: "submission-1",
  taskId: task.id,
  sequence: 1,
  status: "pending_review",
  origin: "manual",
  summary: "请验收",
  submittedByActorId: owner.id,
  submittedByActor: owner,
  submittedAt: "2026-08-27T02:00:00Z",
  reviewedByActorId: null,
  reviewedByActor: null,
  reviewedAt: null,
  reviewReason: null,
  withdrawnByActorId: null,
  withdrawnByActor: null,
  withdrawnAt: null,
  isInferred: false,
  artifacts: [artifact],
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

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe("search query", () => {
  it("aborts the obsolete request and keeps the new query result isolated", async () => {
    let firstSignal: AbortSignal | undefined;
    let markFirstStarted: (() => void) | undefined;
    const firstStarted = new Promise<void>((resolve) => {
      markFirstStarted = resolve;
    });
    getSearchResultsMock.mockImplementation(
      (input: { q: string }, signal?: AbortSignal) => {
        if (input.q === "旧查询") {
          firstSignal = signal;
          markFirstStarted?.();
          return new Promise((_resolve, reject) => {
            signal?.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            );
          });
        }
        return Promise.resolve({
          items: [],
          meta: { page: 1, pageSize: 12, total: 0 },
        });
      },
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(
      ({ q }) => useSearchQuery({ q, page: 1, pageSize: 12 }),
      {
        initialProps: { q: "旧查询" },
        wrapper: wrapperFor(queryClient),
      },
    );
    await firstStarted;

    hook.rerender({ q: "当前查询" });
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));

    expect(firstSignal).toBeDefined();
    expect(firstSignal?.aborted).toBe(true);
    expect(getSearchResultsMock).toHaveBeenLastCalledWith(
      { q: "当前查询", page: 1, pageSize: 12 },
      expect.any(AbortSignal),
    );
    expect(hook.result.current.data).toEqual({
      items: [],
      meta: { page: 1, pageSize: 12, total: 0 },
    });

    hook.unmount();
    queryClient.clear();
  });
});

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
    expect(getTaskPageMock).toHaveBeenCalledWith(
      params,
      expect.any(AbortSignal),
    );
    expect(result.current.data).toEqual({
      items: [task],
      meta: { page: 2, pageSize: 25, total: 51 },
    });
  });

  it("aborts an in-flight task page when its observer unmounts", async () => {
    let requestSignal: AbortSignal | undefined;
    let markStarted: (() => void) | undefined;
    const started = new Promise<void>((resolve) => {
      markStarted = resolve;
    });
    getTaskPageMock.mockImplementation(
      (_input: unknown, signal?: AbortSignal) =>
        new Promise((_resolve, reject) => {
          requestSignal = signal;
          markStarted?.();
          signal?.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          );
        }),
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(() => useTaskPageQuery({ page: 1, pageSize: 20 }), {
      wrapper: wrapperFor(queryClient),
    });
    await started;

    expect(requestSignal).toBeDefined();
    expect(requestSignal?.aborted).toBe(false);
    hook.unmount();
    expect(requestSignal?.aborted).toBe(true);
    queryClient.clear();
  });

  it("polls only due-state task pages to keep deadline risk current", async () => {
    vi.useFakeTimers();
    getTaskPageMock.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const dueParams = {
      page: 1,
      pageSize: 20,
      status: "active" as const,
      dueState: "due_soon" as const,
      sort: "due_date",
    };
    const dueHook = renderHook(() => useTaskPageQuery(dueParams), {
      wrapper: wrapperFor(queryClient),
    });
    await act(async () => Promise.resolve());
    expect(getTaskPageMock).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(getTaskPageMock).toHaveBeenCalledTimes(2);
    dueHook.unmount();
    queryClient.clear();
    getTaskPageMock.mockClear();

    const normalParams = { page: 1, pageSize: 20, status: "active" as const };
    const normalHook = renderHook(() => useTaskPageQuery(normalParams), {
      wrapper: wrapperFor(queryClient),
    });
    await act(async () => Promise.resolve());
    expect(getTaskPageMock).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(getTaskPageMock).toHaveBeenCalledTimes(1);
    normalHook.unmount();
    queryClient.clear();
  });

  it("polls Today statistics every minute to prevent clock drift", async () => {
    vi.useFakeTimers();
    getTodayStatsMock.mockResolvedValue({
      date: "2026-08-28",
      tasks: {
        total: 0,
        completed: 0,
        remaining: 0,
        overdue: 0,
        dueSoon: 0,
        estimatedMinutes: 0,
        actualMinutes: 0,
      },
      focus: { sessions: 0, seconds: 0, minutes: 0 },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(() => useTodayStatsQuery("2026-08-28"), {
      wrapper: wrapperFor(queryClient),
    });
    await act(async () => Promise.resolve());
    expect(getTodayStatsMock).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(60_000));
    expect(getTodayStatsMock).toHaveBeenCalledTimes(2);
    hook.unmount();
    queryClient.clear();
  });

  it("keeps the legacy useTasksQuery result as Task[]", async () => {
    getTasksMock.mockResolvedValue([task]);
    const { result } = renderHook(() => useTasksQuery(), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([task]);
    expect(Array.isArray(result.current.data)).toBe(true);
    expect(getTasksMock).toHaveBeenCalledWith(
      { projectId: undefined },
      expect.any(AbortSignal),
    );
  });

  it("passes cancellation through the legacy complete task query", async () => {
    getAllTasksMock.mockResolvedValue([task]);
    const { result } = renderHook(
      () => useTasksQuery({ projectId: "project-1", loadAll: true }),
      { wrapper: createWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getAllTasksMock).toHaveBeenCalledWith(
      { projectId: "project-1" },
      expect.any(AbortSignal),
    );
    expect(result.current.data).toEqual([task]);
  });

  it("loads complete active Today groups with explicit date boundaries", async () => {
    getAllTasksMock.mockImplementation(async (input) => [
      { ...task, id: JSON.stringify(input) },
    ]);
    const { result } = renderHook(() => useTodayTaskGroupsQuery("2026-08-28"), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getAllTasksMock).toHaveBeenCalledTimes(4);
    expect(getAllTasksMock).toHaveBeenNthCalledWith(
      1,
      {
        status: "active",
        plannedTo: "2026-08-27",
      },
      expect.any(AbortSignal),
    );
    expect(getAllTasksMock).toHaveBeenNthCalledWith(
      2,
      {
        status: "active",
        plannedDate: "2026-08-28",
      },
      expect.any(AbortSignal),
    );
    expect(getAllTasksMock).toHaveBeenNthCalledWith(
      3,
      {
        status: "active",
        plannedFrom: "2026-08-29",
        plannedTo: "2026-08-30",
      },
      expect.any(AbortSignal),
    );
    expect(getAllTasksMock).toHaveBeenNthCalledWith(
      4,
      {
        status: "active",
        plannedState: "unscheduled",
      },
      expect.any(AbortSignal),
    );
    expect(
      new Set(getAllTasksMock.mock.calls.map((call) => call[1])).size,
    ).toBe(1);
    expect(result.current.data?.overdue).toHaveLength(1);
    expect(result.current.data?.today).toHaveLength(1);
    expect(result.current.data?.thisWeek).toHaveLength(1);
    expect(result.current.data?.unscheduled).toHaveLength(1);
  });

  it("rejects a task duplicated across Today groups", async () => {
    getAllTasksMock.mockResolvedValue([task]);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(() => useTodayTaskGroupsQuery("2026-08-28"), {
      wrapper: wrapperFor(queryClient),
    });

    await waitFor(() => expect(hook.result.current.isError).toBe(true), {
      timeout: 2_500,
    });
    expect(hook.result.current.error).toMatchObject({
      code: "INVALID_RESPONSE",
    });
    hook.unmount();
    queryClient.clear();
  });

  it("aborts every obsolete Today page when the selected date changes", async () => {
    const obsoleteSignals: AbortSignal[] = [];
    const callStart = getAllTasksMock.mock.calls.length;
    let markObsoleteStarted: (() => void) | undefined;
    const obsoleteStarted = new Promise<void>((resolve) => {
      markObsoleteStarted = resolve;
    });
    getAllTasksMock.mockImplementation(
      (_input: unknown, signal?: AbortSignal) => {
        if (getAllTasksMock.mock.calls.length <= callStart + 4) {
          if (!signal) throw new Error("Today query did not pass a signal");
          obsoleteSignals.push(signal);
          if (obsoleteSignals.length === 4) markObsoleteStarted?.();
          return new Promise((_resolve, reject) => {
            signal.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            );
          });
        }
        return Promise.resolve([]);
      },
    );
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const hook = renderHook(({ dateKey }) => useTodayTaskGroupsQuery(dateKey), {
      initialProps: { dateKey: "2026-08-28" },
      wrapper: wrapperFor(queryClient),
    });
    await obsoleteStarted;

    hook.rerender({ dateKey: "2026-08-29" });
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(new Set(obsoleteSignals).size).toBe(1);
    expect(obsoleteSignals.every((signal) => signal.aborted)).toBe(true);
    const currentSignals = getAllTasksMock.mock.calls
      .slice(callStart + 4)
      .map((call) => call[1] as AbortSignal);
    expect(currentSignals).toHaveLength(4);
    expect(new Set(currentSignals).size).toBe(1);
    expect(currentSignals[0]).not.toBe(obsoleteSignals[0]);
    expect(currentSignals[0].aborted).toBe(false);

    hook.unmount();
    queryClient.clear();
  });

  it("loads the complete Sidebar week and excludes cancelled Tasks from progress", async () => {
    const doneTask = { ...task, id: "done", status: "done" as const };
    const reviewTask = {
      ...task,
      id: "review",
      status: "waiting_review" as const,
    };
    const cancelledTask = {
      ...task,
      id: "cancelled",
      status: "cancelled" as const,
    };
    getAllTasksMock.mockResolvedValue([
      task,
      doneTask,
      reviewTask,
      cancelledTask,
    ]);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(
      () =>
        useSidebarWeekTasksQuery({
          plannedFrom: "2026-02-23",
          plannedTo: "2026-03-01",
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getAllTasksMock).toHaveBeenCalledWith(
      {
        plannedFrom: "2026-02-23",
        plannedTo: "2026-03-01",
      },
      expect.any(AbortSignal),
    );
    expect(result.current.data).toEqual({
      plannedFrom: "2026-02-23",
      plannedTo: "2026-03-01",
      taskCount: 3,
      completedCount: 1,
      completedPercent: 33,
    });
    expect(
      queryClient.getQueryData([
        ...taskQueryKey,
        "sidebar-week",
        "2026-02-23",
        "2026-03-01",
      ]),
    ).toEqual(result.current.data);
  });

  it("moves across active statuses while preserving terminal task slots", async () => {
    const done = { ...task, id: "done", status: "done" as const, version: 5 };
    const blocked = {
      ...task,
      id: "blocked",
      status: "blocked" as const,
      version: 6,
    };
    getAllTasksMock.mockResolvedValue([task, done, blocked]);
    reorderTasksMock.mockImplementation(async (input) => ({
      plannedDate: input.plannedDate,
      mode: input.mode,
      changed: 2,
      tasks: [],
    }));

    const { result } = renderHook(() => useMoveTaskWithinPlan(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await result.current.mutateAsync({
        taskId: task.id,
        plannedDate: task.plannedDate,
        direction: "down",
        scope: "active",
      });
    });

    expect(reorderTasksMock).toHaveBeenCalledWith({
      plannedDate: task.plannedDate,
      mode: "manual",
      items: [
        { id: blocked.id, expectedVersion: blocked.version },
        { id: done.id, expectedVersion: done.version },
        { id: task.id, expectedVersion: task.version },
      ],
    });
  });

  it("persists an explicit active drag order without moving terminal slots", async () => {
    const done = { ...task, id: "done", status: "done" as const, version: 5 };
    const blocked = {
      ...task,
      id: "blocked",
      status: "blocked" as const,
      version: 6,
    };
    getAllTasksMock.mockResolvedValue([task, done, blocked]);
    reorderTasksMock.mockResolvedValue({
      plannedDate: task.plannedDate,
      mode: "manual",
      changed: 2,
      tasks: [],
    });
    const { result } = renderHook(() => useReorderActiveTasksWithinPlan(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await result.current.mutateAsync({
        plannedDate: task.plannedDate,
        orderedTaskIds: [blocked.id, task.id],
      });
    });

    expect(reorderTasksMock).toHaveBeenCalledWith({
      plannedDate: task.plannedDate,
      mode: "manual",
      items: [
        { id: blocked.id, expectedVersion: blocked.version },
        { id: done.id, expectedVersion: done.version },
        { id: task.id, expectedVersion: task.version },
      ],
    });
  });

  it("moves a task relative to another task without changing status slots", async () => {
    const target = { ...task, id: "todo-target", version: 5 };
    const tail = { ...task, id: "todo-tail", version: 6 };
    const blocked = {
      ...task,
      id: "blocked",
      status: "blocked" as const,
      version: 7,
    };
    const done = {
      ...task,
      id: "done",
      status: "done" as const,
      version: 8,
    };
    getAllTasksMock.mockResolvedValue([task, blocked, target, done, tail]);
    reorderTasksMock.mockResolvedValue({
      plannedDate: task.plannedDate,
      mode: "manual",
      changed: 2,
      tasks: [],
    });
    const { result } = renderHook(() => useReorderTaskWithinPlanStatus(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await result.current.mutateAsync({
        source: task,
        target,
        position: "after",
      });
    });

    expect(reorderTasksMock).toHaveBeenCalledWith({
      plannedDate: task.plannedDate,
      mode: "manual",
      items: [
        { id: target.id, expectedVersion: target.version },
        { id: blocked.id, expectedVersion: blocked.version },
        { id: task.id, expectedVersion: task.version },
        { id: done.id, expectedVersion: done.version },
        { id: tail.id, expectedVersion: tail.version },
      ],
    });
  });

  it("rejects a status-group drag when a visible task version is stale", async () => {
    const target = { ...task, id: "todo-target", version: 5 };
    getAllTasksMock.mockResolvedValue([
      { ...task, version: task.version + 1 },
      target,
    ]);
    const { result } = renderHook(() => useReorderTaskWithinPlanStatus(), {
      wrapper: createWrapper(),
    });

    let caught: unknown;
    await act(async () => {
      try {
        await result.current.mutateAsync({
          source: task,
          target,
          position: "after",
        });
      } catch (error) {
        caught = error;
      }
    });

    expect(caught).toMatchObject({ code: "TASK_REORDER_SET_CHANGED" });
    expect(reorderTasksMock).not.toHaveBeenCalled();
  });

  it("rejects a drag snapshot when the active plan set changed", async () => {
    const blocked = {
      ...task,
      id: "blocked",
      status: "blocked" as const,
      version: 6,
    };
    getAllTasksMock.mockResolvedValue([task, blocked]);
    const { result } = renderHook(() => useReorderActiveTasksWithinPlan(), {
      wrapper: createWrapper(),
    });

    let caught: unknown;
    await act(async () => {
      try {
        await result.current.mutateAsync({
          plannedDate: task.plannedDate,
          orderedTaskIds: [task.id],
        });
      } catch (error) {
        caught = error;
      }
    });
    expect(caught).toMatchObject({ code: "TASK_REORDER_SET_CHANGED" });
    expect(reorderTasksMock).not.toHaveBeenCalled();
  });

  it("moves a task to another plan and reports target-order partial success", async () => {
    const target = {
      ...task,
      id: "target",
      title: "目标日期任务",
      plannedDate: "2026-08-28",
      version: 8,
    };
    const sourceTail = {
      ...task,
      id: "source-tail",
      title: "源日期剩余任务",
      version: 6,
    };
    const targetDone = {
      ...target,
      id: "target-done",
      status: "done" as const,
      version: 9,
    };
    getAllTasksMock.mockImplementation(
      async (input: { plannedDate?: string }) =>
        input.plannedDate === "2026-08-28"
          ? [target, targetDone]
          : [task, sourceTail],
    );
    const moved = {
      ...task,
      plannedDate: "2026-08-28",
      version: task.version + 1,
    };
    batchUpdateTasksMock.mockResolvedValue({
      action: "set_planned_date",
      changed: 1,
      tasks: [moved],
    });
    reorderTasksMock
      .mockResolvedValueOnce({
        plannedDate: "2026-08-27",
        mode: "manual",
        changed: 1,
        tasks: [sourceTail],
      })
      .mockRejectedValueOnce(
        new ApiError("目标组已变化", { code: "TASK_REORDER_SET_CHANGED" }),
      );
    const { result } = renderHook(() => useMoveTaskAcrossPlans(), {
      wrapper: createWrapper(),
    });

    let response: Awaited<ReturnType<typeof result.current.mutateAsync>>;
    await act(async () => {
      response = await result.current.mutateAsync({
        source: task,
        target,
        targetPlannedDate: target.plannedDate,
        position: "before",
      });
    });

    expect(batchUpdateTasksMock).toHaveBeenCalledWith({
      action: "set_planned_date",
      items: [{ id: task.id, expectedVersion: task.version }],
      plannedDate: "2026-08-28",
    });
    expect(reorderTasksMock).toHaveBeenNthCalledWith(1, {
      plannedDate: "2026-08-27",
      mode: "manual",
      items: [{ id: sourceTail.id, expectedVersion: sourceTail.version }],
    });
    expect(reorderTasksMock).toHaveBeenNthCalledWith(2, {
      plannedDate: "2026-08-28",
      mode: "manual",
      items: [
        { id: moved.id, expectedVersion: moved.version },
        { id: targetDone.id, expectedVersion: targetDone.version },
        { id: target.id, expectedVersion: target.version },
      ],
    });
    expect(response!).toMatchObject({
      plannedDateChanged: true,
      task: moved,
      orderWarnings: ["目标日期顺序未能保存"],
    });
  });

  it("reorders tasks from an aggregate view within their exact plan date", async () => {
    const target = {
      ...task,
      id: "same-date-target",
      title: "同日目标",
      version: 6,
    };
    const done = {
      ...task,
      id: "same-date-done",
      status: "done" as const,
      version: 7,
    };
    getAllTasksMock.mockResolvedValue([target, done, task]);
    reorderTasksMock.mockImplementation(async (input) => ({
      plannedDate: input.plannedDate,
      mode: input.mode,
      changed: 2,
      tasks: [task, done, target],
    }));
    const { result } = renderHook(() => useMoveTaskAcrossPlans(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await result.current.mutateAsync({
        source: task,
        target,
        targetPlannedDate: target.plannedDate,
        position: "before",
      });
    });

    expect(batchUpdateTasksMock).not.toHaveBeenCalled();
    expect(reorderTasksMock).toHaveBeenCalledWith({
      plannedDate: task.plannedDate,
      mode: "manual",
      items: [
        { id: task.id, expectedVersion: task.version },
        { id: done.id, expectedVersion: done.version },
        { id: target.id, expectedVersion: target.version },
      ],
    });
  });

  it("moves a task into an empty explicit plan group", async () => {
    getAllTasksMock.mockImplementation(
      async (input: { plannedDate?: string }) =>
        input.plannedDate === task.plannedDate ? [task] : [],
    );
    const moved = { ...task, plannedDate: null, version: task.version + 1 };
    batchUpdateTasksMock.mockResolvedValue({
      action: "set_planned_date",
      changed: 1,
      tasks: [moved],
    });
    reorderTasksMock.mockImplementation(async (input) => ({
      plannedDate: input.plannedDate,
      mode: input.mode,
      changed: input.items.length,
      tasks: input.items.length ? [moved] : [],
    }));
    const { result } = renderHook(() => useMoveTaskAcrossPlans(), {
      wrapper: createWrapper(),
    });

    await act(async () => {
      await result.current.mutateAsync({
        source: task,
        target: null,
        targetPlannedDate: null,
        position: "end",
      });
    });

    expect(reorderTasksMock).toHaveBeenNthCalledWith(1, {
      plannedDate: task.plannedDate,
      mode: "manual",
      items: [],
    });
    expect(reorderTasksMock).toHaveBeenNthCalledWith(2, {
      plannedDate: null,
      mode: "manual",
      items: [{ id: moved.id, expectedVersion: moved.version }],
    });
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

  it("pages task submissions and deleted artifacts under separate keys", async () => {
    getTaskSubmissionsMock.mockImplementation(
      async (_taskId: string, input: { page: number }) => ({
        items: [
          {
            ...submission,
            id: `submission-${input.page}`,
            sequence: 3 - input.page,
          },
        ],
        meta: { page: input.page, pageSize: 1, total: 2, taskVersion: 5 },
      }),
    );
    getTaskArtifactsMock.mockImplementation(
      async (_taskId: string, input: { page: number }) => ({
        items: [{ ...artifact, id: `artifact-${input.page}` }],
        meta: { page: input.page, pageSize: 1, total: 2, taskVersion: 5 },
      }),
    );
    const wrapper = createWrapper();
    const submissionsHook = renderHook(
      () => useTaskSubmissionsQuery(task.id, { pageSize: 1 }),
      { wrapper },
    );
    const artifactsHook = renderHook(
      () =>
        useTaskArtifactsQuery(task.id, {
          pageSize: 1,
          includeDeleted: true,
        }),
      { wrapper },
    );

    await waitFor(() =>
      expect(submissionsHook.result.current.isSuccess).toBe(true),
    );
    await waitFor(() =>
      expect(artifactsHook.result.current.isSuccess).toBe(true),
    );
    await act(async () => {
      await submissionsHook.result.current.fetchNextPage();
      await artifactsHook.result.current.fetchNextPage();
    });
    expect(getTaskSubmissionsMock).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({ page: 2, pageSize: 1 }),
    );
    expect(getTaskArtifactsMock).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({
        page: 2,
        pageSize: 1,
        includeDeleted: true,
      }),
    );
  });
});

describe("task planned-date mutation", () => {
  afterEach(() => {
    batchUpdateTasksMock.mockReset();
    getTaskMock.mockReset();
  });

  it("uses the task version and accepts a verified ambiguous response", async () => {
    batchUpdateTasksMock.mockRejectedValue(
      new ApiError("response lost", { code: "NETWORK_ERROR" }),
    );
    getTaskMock.mockResolvedValue({
      ...task,
      plannedDate: "2026-08-28",
      version: task.version + 1,
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const { result } = renderHook(() => useSetTaskPlannedDate(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        taskId: task.id,
        expectedVersion: task.version,
        plannedDate: "2026-08-28",
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(batchUpdateTasksMock).toHaveBeenCalledWith({
      action: "set_planned_date",
      items: [{ id: task.id, expectedVersion: task.version }],
      plannedDate: "2026-08-28",
    });
    expect(getTaskMock).toHaveBeenCalledWith(task.id);
    expect(queryClient.getQueryData(taskDetailQueryKey(task.id))).toMatchObject(
      {
        plannedDate: "2026-08-28",
        version: task.version + 1,
      },
    );
  });

  it("keeps an ambiguous write failed when the latest task does not prove it", async () => {
    const ambiguous = new ApiError("response lost", { code: "TIMEOUT" });
    batchUpdateTasksMock.mockRejectedValue(ambiguous);
    getTaskMock.mockResolvedValue({ ...task, plannedDate: task.plannedDate });
    const { result } = renderHook(() => useSetTaskPlannedDate(), {
      wrapper: createWrapper(),
    });

    act(() =>
      result.current.mutate({
        taskId: task.id,
        expectedVersion: task.version,
        plannedDate: null,
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(result.current.error).toBe(ambiguous);
  });
});

describe("task deletion mutation", () => {
  it("uses the visible version and refreshes task facts after a conflict", async () => {
    deleteTaskMock.mockRejectedValue(
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
    const { result } = renderHook(() => useDeleteTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({ id: task.id, expectedVersion: task.version }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(deleteTaskMock).toHaveBeenCalledWith(task.id, task.version);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: taskQueryKey });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: projectQueryKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["stats", "today"],
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: roadmapMilestoneQueryKey,
    });
  });
});

describe("controlled task lifecycle mutations", () => {
  it("requires the observed version and invalidates parent aggregates globally", async () => {
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
    const parentTaskId = "task-parent";
    const parentAggregateKeys = [
      [...taskAssignmentQueryKey(parentTaskId), "history"] as const,
      [...taskEventQueryKey(parentTaskId), "timeline"] as const,
      [...taskSubmissionQueryKey(parentTaskId), "history"] as const,
      [...taskArtifactQueryKey(parentTaskId), "history"] as const,
    ];
    parentAggregateKeys.forEach((queryKey) => {
      queryClient.setQueryData(queryKey, { cached: true });
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
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(executeTaskLifecycleCommandMock).toHaveBeenCalledWith(
      task.id,
      { action: "complete", expectedVersion: task.version },
      expect.any(String),
    );
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskAssignmentQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskEventQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskSubmissionQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskArtifactQueryRootKey,
    });
    parentAggregateKeys.forEach((queryKey) => {
      expect(queryClient.getQueryState(queryKey)?.isInvalidated).toBe(true);
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

describe("task update mutation", () => {
  it("does not replace a newer detail cache and invalidates unified search", async () => {
    updateTaskMock.mockResolvedValue({
      ...task,
      title: "较早的重放响应",
      version: task.version + 1,
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(taskDetailQueryKey(task.id), {
      ...task,
      title: "缓存中的更新版本",
      version: task.version + 3,
    });
    const searchKey = [
      ...searchQueryKey,
      { q: task.title, types: ["task"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: task.id,
        input: {
          title: "本次编辑",
          description: task.description,
          priority: task.priority,
          dueDate: task.dueDate,
          plannedDate: task.plannedDate,
          estimatedMinutes: task.estimatedMinutes,
          expectedVersion: task.version,
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(updateTaskMock).toHaveBeenCalledWith(
      task.id,
      expect.objectContaining({ expectedVersion: task.version }),
    );
    expect(
      queryClient.getQueryData<Task>(taskDetailQueryKey(task.id)),
    ).toMatchObject({
      title: "缓存中的更新版本",
      version: task.version + 3,
    });
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
  });
});

describe("task output mutations", () => {
  it.each([
    { outcome: "成功", error: null },
    {
      outcome: "文件缺失",
      error: new ApiError("文件缺失", {
        code: "ARTIFACT_FILE_MISSING",
        status: 410,
      }),
    },
    {
      outcome: "完整性不匹配",
      error: new ApiError("文件校验不一致", {
        code: "ARTIFACT_INTEGRITY_MISMATCH",
        status: 409,
      }),
    },
    {
      outcome: "一般错误",
      error: new ApiError("下载失败", {
        code: "REQUEST_ERROR",
        status: 500,
      }),
    },
  ])("下载$outcome后刷新所有 Artifact 表示", async ({ error }) => {
    if (error) {
      downloadTaskArtifactMock.mockRejectedValue(error);
    } else {
      downloadTaskArtifactMock.mockResolvedValue({
        blob: new Blob(["artifact"]),
        fileName: artifact.name,
        mimeType: "text/plain",
      });
    }
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useDownloadTaskArtifact(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        taskId: task.id,
        id: artifact.id,
        name: artifact.name,
      }),
    );
    await waitFor(() =>
      expect(error ? result.current.isError : result.current.isSuccess).toBe(
        true,
      ),
    );

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskArtifactDetailQueryKey(artifact.id),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskArtifactQueryKey(task.id),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskSubmissionQueryKey(task.id),
    });
  });

  it("keeps one idempotency key for a retried File draft and invalidates the aggregate", async () => {
    const waitingTask: Task = {
      ...task,
      reviewPolicy: "manual",
      status: "waiting_review",
      currentSubmissionId: submission.id,
      version: 5,
    };
    submitTaskOutputMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce({
        task: waitingTask,
        submission,
        artifacts: [artifact],
        event: { ...taskEvent, action: "task_output_submitted" },
      });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useSubmitTaskOutput(), {
      wrapper: wrapperFor(queryClient),
    });
    const file = new File(["hello"], "result.txt", {
      type: "text/plain",
      lastModified: 123,
    });
    const variables = {
      taskId: task.id,
      input: {
        summary: "请验收",
        artifacts: [
          {
            clientRef: "file-draft",
            storageKind: "file" as const,
            name: "result.txt",
            file,
            requiresFollowup: false,
          },
        ],
        expectedVersion: task.version,
      },
    };

    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(variables));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(submitTaskOutputMock.mock.calls[0][2]).toBeTruthy();
    expect(submitTaskOutputMock.mock.calls[1][2]).toBe(
      submitTaskOutputMock.mock.calls[0][2],
    );
    expect(submitTaskOutputMock.mock.calls[1][1].artifacts[0].file).toBe(file);
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskSubmissionQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskArtifactQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskAssignmentQueryRootKey,
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: taskEventQueryRootKey,
    });
  });

  it("uses stable review and delete keys and refreshes output caches", async () => {
    const accepted = {
      ...submission,
      status: "accepted" as const,
      reviewedByActorId: owner.id,
      reviewedByActor: owner,
      reviewedAt: "2026-08-27T03:00:00Z",
      artifacts: submission.artifacts.map((item) => ({
        ...item,
        submissionStatus: "accepted" as const,
      })),
    };
    reviewTaskSubmissionMock.mockResolvedValue({
      task: {
        ...task,
        status: "done",
        reviewPolicy: "manual",
        currentSubmissionId: submission.id,
        version: 5,
      },
      submission: accepted,
      event: { ...taskEvent, action: "task_review_accepted" },
    });
    deleteTaskArtifactMock.mockResolvedValue({
      task: { ...task, version: 6 },
      artifact: {
        ...artifact,
        submissionStatus: "accepted",
        deletedAt: "2026-08-27T04:00:00Z",
        deletedByActorId: owner.id,
        deletedByActor: owner,
        deleteReason: "重复产出",
      },
      event: { ...taskEvent, action: "task_artifact_deleted" },
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const wrapper = wrapperFor(queryClient);
    const reviewHook = renderHook(() => useReviewTaskSubmission(), { wrapper });
    const deleteHook = renderHook(() => useDeleteTaskArtifact(), { wrapper });

    act(() =>
      reviewHook.result.current.mutate({
        taskId: task.id,
        input: { decision: "accept", expectedVersion: task.version },
      }),
    );
    await waitFor(() => expect(reviewHook.result.current.isSuccess).toBe(true));
    act(() =>
      deleteHook.result.current.mutate({
        taskId: task.id,
        artifactId: artifact.id,
        input: { reason: "重复产出", expectedVersion: 5 },
      }),
    );
    await waitFor(() => expect(deleteHook.result.current.isSuccess).toBe(true));

    expect(reviewTaskSubmissionMock.mock.calls[0][2]).toEqual(
      expect.any(String),
    );
    expect(deleteTaskArtifactMock.mock.calls[0][3]).toEqual(expect.any(String));
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
