import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Task } from "../types/models";
import {
  resolveBoardTransition,
  TaskBoardTransitionModal,
} from "./TaskBoardTransitionModal";

const mocks = vi.hoisted(() => ({ mutate: vi.fn(), reset: vi.fn() }));

vi.mock("../api/hooks", () => ({
  useTaskLifecycleCommand: () => ({
    error: null,
    isPending: false,
    mutate: mocks.mutate,
    reset: mocks.reset,
  }),
}));

const task: Task = {
  id: "task-1",
  title: "整理交付清单",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P1",
  projectId: null,
  parentTaskId: null,
  completionCriteria: "清单已确认",
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: null,
  version: 7,
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

describe("TaskBoardTransitionModal", () => {
  afterEach(() => {
    cleanup();
    mocks.mutate.mockClear();
    mocks.reset.mockClear();
  });

  it("allows only transitions backed by an explicit lifecycle command", () => {
    expect(resolveBoardTransition(task, "in_progress")?.action).toBe("start");
    expect(resolveBoardTransition(task, "blocked")?.action).toBe("block");
    expect(resolveBoardTransition(task, "done")?.action).toBe("complete");
    expect(
      resolveBoardTransition({ ...task, reviewPolicy: "manual" }, "done"),
    ).toBeNull();
    expect(
      resolveBoardTransition(
        {
          ...task,
          status: "blocked",
          blockedFromStatus: "in_progress",
        },
        "in_progress",
      )?.action,
    ).toBe("unblock");
    expect(
      resolveBoardTransition(
        { ...task, status: "blocked", blockedFromStatus: "in_progress" },
        "todo",
      ),
    ).toBeNull();
  });

  it("requires and submits a reason for a block command", () => {
    const transition = resolveBoardTransition(task, "blocked");
    render(
      <TaskBoardTransitionModal
        onClose={vi.fn()}
        onConflict={vi.fn()}
        transition={transition}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "确认变更" }));
    expect(
      screen.getByText("请填写原因。该原因会写入任务活动记录。"),
    ).toBeVisible();
    expect(mocks.mutate).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("原因"), {
      target: { value: "等待客户提供素材" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认变更" }));
    expect(mocks.mutate).toHaveBeenCalledWith(
      {
        id: task.id,
        input: {
          action: "block",
          reason: "等待客户提供素材",
          expectedVersion: task.version,
        },
      },
      expect.any(Object),
    );
  });
});
