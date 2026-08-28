import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { TaskList } from "./TaskList";

const mocks = vi.hoisted(() => ({ childTotal: 1 }));

vi.mock("../api/hooks", () => ({
  useTaskPageQuery: (input: { page?: number; parentTaskId?: string }) => ({
    data:
      input.parentTaskId === task.id
        ? {
            items: [input.page === 2 ? secondPageChild : childTask],
            meta: {
              page: input.page ?? 1,
              pageSize: 100,
              total: mocks.childTotal,
            },
          }
        : undefined,
    isError: false,
    isFetching: false,
    isPending: false,
    isPlaceholderData: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
}));

const task: Task = {
  id: "task-1",
  title: "整理项目简报",
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
  plannedDate: null,
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: null,
  version: 1,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T08:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

const childTask: Task = {
  ...task,
  id: "task-2",
  title: "确认交付清单",
  parentTaskId: task.id,
  parentTaskTitle: task.title,
};

const secondPageChild: Task = {
  ...childTask,
  id: "task-102",
  title: "第 101 项子任务",
};

describe("TaskList", () => {
  beforeEach(() => {
    mocks.childTotal = 1;
  });

  afterEach(() => {
    cleanup();
    useUiStore.setState({ taskDetailId: null });
  });

  it("opens the selected task in the shared detail modal", () => {
    render(<TaskList live tasks={[task]} />);

    fireEvent.click(
      screen.getByRole("button", { name: `查看任务：${task.title}` }),
    );

    expect(useUiStore.getState().taskDetailId).toBe(task.id);
  });

  it("exposes an accessible plan action without opening task details", () => {
    const onPlanTask = vi.fn();
    render(<TaskList live onPlanTask={onPlanTask} tasks={[task]} />);

    fireEvent.click(
      screen.getByRole("button", { name: `安排任务日期：${task.title}` }),
    );

    expect(onPlanTask).toHaveBeenCalledWith(task);
    expect(useUiStore.getState().taskDetailId).toBeNull();
  });

  it("exposes only policy-safe lifecycle shortcuts plus focus", () => {
    const onStartTask = vi.fn();
    const onCompleteTask = vi.fn();
    const onStartFocus = vi.fn();
    const inProgress = {
      ...task,
      id: "task-progress",
      title: "进行中的任务",
      status: "in_progress" as const,
    };
    const manualReview = {
      ...inProgress,
      id: "task-manual",
      title: "人工验收任务",
      reviewPolicy: "manual" as const,
    };
    render(
      <TaskList
        live
        onCompleteTask={onCompleteTask}
        onStartFocus={onStartFocus}
        onStartTask={onStartTask}
        tasks={[task, inProgress, manualReview]}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: `开始执行任务：${task.title}` }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: `完成任务：${inProgress.title}` }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: `开始专注：${manualReview.title}` }),
    );

    expect(onStartTask).toHaveBeenCalledWith(task);
    expect(onCompleteTask).toHaveBeenCalledWith(inProgress);
    expect(onStartFocus).toHaveBeenCalledWith(manualReview);
    expect(
      screen.queryByRole("button", {
        name: `完成任务：${manualReview.title}`,
      }),
    ).not.toBeInTheDocument();
  });

  it("disables shortcuts while facts are changing or focus is unavailable", () => {
    render(
      <TaskList
        focusActionDisabled
        live
        onStartFocus={vi.fn()}
        onStartTask={vi.fn()}
        quickActionsDisabled
        quickActionPendingId={task.id}
        tasks={[task]}
      />,
    );

    expect(
      screen.getByRole("button", { name: `开始执行任务：${task.title}` }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: `开始专注：${task.title}` }),
    ).toBeDisabled();
  });

  it("uses the status control only to open details for every lifecycle state", () => {
    const statuses = [
      ["todo", "待办"],
      ["in_progress", "进行中"],
      ["blocked", "阻塞"],
      ["waiting_review", "待验收"],
      ["done", "已完成"],
      ["cancelled", "已取消"],
    ] as const;
    render(
      <TaskList
        live
        tasks={statuses.map(([status], index) => ({
          ...task,
          id: `task-${index + 10}`,
          title: `状态任务 ${index + 1}`,
          status,
        }))}
      />,
    );

    for (const [, label] of statuses) {
      expect(
        screen.getByRole("button", { name: new RegExp(`（${label}）$`) }),
      ).toBeVisible();
    }
    fireEvent.click(
      screen.getByRole("button", {
        name: /查看任务状态：状态任务 6（已取消）/,
      }),
    );
    expect(useUiStore.getState().taskDetailId).toBe("task-15");
  });

  it("loads and renders children only after expanding a parent", () => {
    render(
      <TaskList hierarchical live tasks={[{ ...task, subtaskTotal: 1 }]} />,
    );

    expect(screen.queryByText(childTask.title)).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: `展开子任务：${task.title}` }),
    );

    expect(screen.getByText(childTask.title)).toBeInTheDocument();
  });

  it("keeps children beyond the first 100 accessible", () => {
    mocks.childTotal = 101;
    render(
      <TaskList hierarchical live tasks={[{ ...task, subtaskTotal: 101 }]} />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: `展开子任务：${task.title}` }),
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: "下一页",
      }),
    );

    expect(screen.getByText(secondPageChild.title)).toBeInTheDocument();
    expect(screen.getByText("2 / 2 · 共 101 项")).toBeInTheDocument();
  });

  it("offers pointer drag sorting alongside the keyboard-friendly move buttons", () => {
    const onDropTask = vi.fn();
    const second = { ...task, id: "task-drag-2", title: "第二项任务" };
    render(
      <TaskList
        allowDrag
        allowReorder
        live
        onDropTask={onDropTask}
        onMove={vi.fn()}
        tasks={[task, second]}
      />,
    );
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      setData: vi.fn(),
    };
    const source = screen.getByTitle(`拖动排序：${task.title}`);
    const target = screen
      .getByRole("button", { name: `查看任务：${second.title}` })
      .closest("article");
    expect(target).not.toBeNull();

    fireEvent.dragStart(source, { dataTransfer });
    fireEvent.dragOver(target!, { dataTransfer });
    fireEvent.drop(target!, { dataTransfer });

    expect(dataTransfer.setData).toHaveBeenCalledWith("text/plain", task.id);
    expect(onDropTask).toHaveBeenCalledWith(task, second);
    expect(
      screen.getByRole("button", { name: `下移任务：${task.title}` }),
    ).toBeVisible();
  });
});
