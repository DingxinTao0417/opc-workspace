import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  Project,
  ProjectInput,
  Tag,
  TagInput,
  Task,
} from "../types/models";
import {
  taskDetailQueryKey,
  useCreateProject,
  useCreateTag,
  useTaskPageQuery,
  useTasksQuery,
  useUpdateTaskStatus,
} from "./hooks";

const createProjectMock = vi.hoisted(() => vi.fn());
const createTagMock = vi.hoisted(() => vi.fn());
const getTaskPageMock = vi.hoisted(() => vi.fn());
const getTasksMock = vi.hoisted(() => vi.fn());
const updateTaskStatusMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createProject: createProjectMock,
    createTag: createTagMock,
    getTaskPage: getTaskPageMock,
    getTasks: getTasksMock,
    updateTaskStatus: updateTaskStatusMock,
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
  tags: [],
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
});

describe("versioned task mutations", () => {
  it("uses the observed cached version for legacy status callers", async () => {
    updateTaskStatusMock.mockResolvedValue({
      ...task,
      status: "done",
      version: 5,
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(taskDetailQueryKey(task.id), task);
    const { result } = renderHook(() => useUpdateTaskStatus(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate({ id: task.id, status: "done" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(updateTaskStatusMock).toHaveBeenCalledWith(
      task.id,
      "done",
      task.version,
    );
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
