import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project, Task } from "../types/models";
import { ApiError } from "./client";
import {
  financialEntryQueryKey,
  focusReportQueryKey,
  focusSessionHistoryQueryKey,
  inboxQueryKey,
  invoiceQueryKey,
  useBatchUpdateTasks,
  useDeleteProject,
  useDeleteTag,
  useDeleteTask,
  useSetTaskPlannedDate,
  useUpdateProject,
  useUpdateTag,
  useUpdateTask,
} from "./hooks";

const clientMocks = vi.hoisted(() => ({
  batchUpdateTasks: vi.fn(),
  deleteProject: vi.fn(),
  deleteTag: vi.fn(),
  deleteTask: vi.fn(),
  updateProject: vi.fn(),
  updateTag: vi.fn(),
  updateTask: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return { ...actual, ...clientMocks };
});

const task: Task = {
  id: "task-1",
  title: "整理交付清单",
  description: "",
  kind: "work",
  status: "in_progress",
  priority: "P1",
  projectId: "project-1",
  projectName: "品牌官网改版",
  parentTaskId: null,
  completionCriteria: "",
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 30,
  actualMinutes: 25,
  manualOrder: 1,
  version: 2,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-28T09:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

const project: Project = {
  id: "project-1",
  name: "品牌官网改版",
  description: "",
  clientId: null,
  clientName: null,
  status: "in_progress",
  startDate: null,
  dueDate: null,
  amountMinor: null,
  color: null,
  version: 2,
  archivedFromStatus: null,
  createdAt: "2026-08-28T09:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  taskSummary: {
    total: 1,
    completed: 0,
    inProgress: 1,
    remaining: 1,
    progressPercent: 0,
    actualMinutes: 25,
  },
  invoiceCount: 0,
  availableActions: ["pause", "complete", "archive"],
};

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

describe("Focus query-time attribution invalidation", () => {
  it("invalidates report and history after editing Task query-time facts", async () => {
    clientMocks.updateTask.mockResolvedValue({
      ...task,
      title: "更新后的交付清单",
      projectId: "project-2",
      projectName: "新项目",
      version: 3,
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: task.id,
        input: {
          title: "更新后的交付清单",
          description: task.description,
          kind: task.kind,
          priority: task.priority,
          projectId: "project-2",
          parentTaskId: null,
          completionCriteria: task.completionCriteria,
          reviewPolicy: task.reviewPolicy,
          tagIds: ["tag-1"],
          dueDate: null,
          plannedDate: null,
          estimatedMinutes: task.estimatedMinutes,
          expectedVersion: task.version,
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("refreshes report and history after a Task edit conflict", async () => {
    clientMocks.updateTask.mockRejectedValue(
      new ApiError("任务版本冲突", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: task.id,
        input: {
          title: "并发编辑",
          description: task.description,
          priority: task.priority,
          dueDate: task.dueDate,
          plannedDate: task.plannedDate,
          estimatedMinutes: task.estimatedMinutes,
          expectedVersion: task.version,
        },
      }),
    );
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("invalidates both project-scoped caches after a batch project move", async () => {
    clientMocks.batchUpdateTasks.mockResolvedValue({
      action: "set_project",
      changed: 1,
      tasks: [{ ...task, projectId: "project-2", version: 3 }],
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        action: "set_project",
        items: [{ id: task.id, expectedVersion: task.version }],
        projectId: "project-2",
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("only invalidates the report for batch tag changes", async () => {
    clientMocks.batchUpdateTasks.mockResolvedValue({
      action: "add_tags",
      changed: 1,
      tasks: [{ ...task, version: 3 }],
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        action: "add_tags",
        items: [{ id: task.id, expectedVersion: task.version }],
        tagIds: ["tag-1"],
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("does not invalidate Focus for a planned-date-only change", async () => {
    clientMocks.batchUpdateTasks.mockResolvedValue({
      action: "set_planned_date",
      changed: 1,
      tasks: [{ ...task, plannedDate: "2026-08-29", version: 3 }],
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useSetTaskPlannedDate(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        taskId: task.id,
        expectedVersion: task.version,
        plannedDate: "2026-08-29",
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusReportQueryKey,
    });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("invalidates both caches after deleting a Task", async () => {
    clientMocks.deleteTask.mockResolvedValue({ deletedId: task.id });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useDeleteTask(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({ id: task.id, expectedVersion: task.version }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("invalidates report labels after Tag rename and deletion", async () => {
    clientMocks.updateTag.mockResolvedValue({
      id: "tag-1",
      name: "深度工作",
      color: "#6E7BF2",
      version: 2,
    });
    clientMocks.deleteTag.mockResolvedValue({
      deletedId: "tag-1",
      detachedTasks: 1,
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = wrapperFor(queryClient);
    const updateHook = renderHook(() => useUpdateTag(), { wrapper });

    act(() =>
      updateHook.result.current.mutate({
        id: "tag-1",
        input: { name: "深度工作", expectedVersion: 1 },
      }),
    );
    await waitFor(() => expect(updateHook.result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });

    invalidate.mockClear();
    const deleteHook = renderHook(() => useDeleteTag(), { wrapper });
    act(() =>
      deleteHook.result.current.mutate({
        id: "tag-1",
        expectedVersion: 2,
      }),
    );
    await waitFor(() => expect(deleteHook.result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });

  it("invalidates project labels after Project rename and deletion", async () => {
    clientMocks.updateProject.mockResolvedValue({
      ...project,
      name: "官网二期",
      version: 3,
    });
    clientMocks.deleteProject.mockResolvedValue({
      deletedId: project.id,
      detachedTasks: 1,
      detachedInvoices: 0,
      detachedFinancialEntries: 0,
    });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const wrapper = wrapperFor(queryClient);
    const updateHook = renderHook(() => useUpdateProject(), { wrapper });

    act(() =>
      updateHook.result.current.mutate({
        id: project.id,
        input: {
          name: "官网二期",
          description: project.description,
          clientId: null,
          startDate: null,
          dueDate: null,
          amountMinor: null,
          color: null,
          expectedVersion: project.version,
        },
      }),
    );
    await waitFor(() => expect(updateHook.result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: focusReportQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: invoiceQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: financialEntryQueryKey,
    });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });

    invalidate.mockClear();
    const deleteHook = renderHook(() => useDeleteProject(), { wrapper });
    act(() =>
      deleteHook.result.current.mutate({
        id: project.id,
        expectedVersion: 3,
      }),
    );
    await waitFor(() => expect(deleteHook.result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: focusReportQueryKey,
      refetchType: "none",
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: invoiceQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: financialEntryQueryKey,
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: focusSessionHistoryQueryKey,
    });
  });
});
