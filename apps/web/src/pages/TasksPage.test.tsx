import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Task, TaskListParams } from "../types/models";
import { TasksPage } from "./TasksPage";

const mocks = vi.hoisted(() => ({
  batch: vi.fn(),
  move: vi.fn(),
  resetBatch: vi.fn(),
  resetOrder: vi.fn(),
  taskQueries: [] as TaskListParams[],
  placeholder: false,
}));

const task: Task = {
  id: "task-1",
  title: "整理交付清单",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P1",
  projectId: "project-1",
  projectName: "品牌官网改版",
  parentTaskId: null,
  completionCriteria: "清单经负责人确认",
  dueDate: null,
  plannedDate: "2026-08-27",
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: null,
  version: 7,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T08:00:00Z",
  completedAt: null,
  tags: [],
};

vi.mock("../api/hooks", () => ({
  useTaskPageQuery: (input: TaskListParams) => {
    mocks.taskQueries.push(input);
    return {
      data: {
        items: [task],
        meta: { page: input.page ?? 1, pageSize: 50, total: 101 },
      },
      error: null,
      isError: false,
      isFetching: mocks.placeholder,
      isPending: false,
      isPlaceholderData: mocks.placeholder,
      isSuccess: true,
      refetch: vi.fn(),
    };
  },
  useProjectOptionsQuery: () => ({
    data: [{ id: "project-1", name: "品牌官网改版" }],
    isError: false,
    isPending: false,
  }),
  useTagOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useBatchUpdateTasks: () => ({
    error: null,
    isPending: false,
    mutate: mocks.batch,
    reset: mocks.resetBatch,
  }),
  useMoveTaskWithinPlan: () => ({
    error: null,
    isPending: false,
    mutate: mocks.move,
    variables: undefined,
  }),
  useResetTaskOrder: () => ({
    error: null,
    isPending: false,
    mutate: mocks.resetOrder,
  }),
  useUpdateTaskStatus: () => ({
    error: null,
    isError: false,
    isPending: false,
    mutate: vi.fn(),
    variables: undefined,
  }),
  useCreateTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
  useDeleteTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
}));

function lastPageQuery(): TaskListParams | undefined {
  return [...mocks.taskQueries]
    .reverse()
    .find((input) => input.pageSize === 50);
}

describe("TasksPage", () => {
  beforeEach(() => {
    mocks.taskQueries.length = 0;
    mocks.batch.mockClear();
    mocks.move.mockClear();
    mocks.resetBatch.mockClear();
    mocks.resetOrder.mockClear();
    mocks.placeholder = false;
  });

  afterEach(cleanup);

  it("uses root pagination by default and switches to flat server filtering", () => {
    render(<TasksPage />);

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ page: 1, pageSize: 50, rootOnly: true }),
    );

    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("状态"), {
      target: { value: "todo" },
    });

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ status: "todo", rootOnly: false }),
    );
    expect(screen.getByText("101 项")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ page: 2, status: "todo" }),
    );
  });

  it("sends the selected task version with an atomic batch update", () => {
    render(<TasksPage />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "set_project",
        items: [{ id: task.id, expectedVersion: task.version }],
        projectId: null,
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("drops a stale selection when a selected task was deleted elsewhere", () => {
    render(<TasksPage />);

    const checkbox = screen.getByRole("checkbox", {
      name: `选择任务：${task.title}`,
    });
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    const callbacks = mocks.batch.mock.calls[0][1] as {
      onError: (error: unknown) => void;
    };
    act(() =>
      callbacks.onError(
        new ApiError("任务集合已变化", {
          code: "TASK_BATCH_SET_CHANGED",
        }),
      ),
    );

    expect(checkbox).not.toBeChecked();
    expect(screen.queryByLabelText("批量操作")).not.toBeInTheDocument();
  });

  it("only enables persisted reordering for one exact plan date", () => {
    render(<TasksPage />);

    fireEvent.change(screen.getByLabelText("任务排序"), {
      target: { value: "manual_order" },
    });
    expect(
      screen.getByText("选择一个精确计划日期，并清除其他筛选后即可调整顺序。"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-08-27" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: `上移任务：${task.title}` }),
    );

    expect(mocks.move).toHaveBeenCalledWith({
      taskId: task.id,
      plannedDate: task.plannedDate,
      direction: "up",
    });
  });

  it("blocks writes while a previous task page is shown as placeholder data", () => {
    mocks.placeholder = true;
    render(<TasksPage />);

    expect(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    ).toBeDisabled();
  });
});
