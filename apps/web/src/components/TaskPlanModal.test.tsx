import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Task } from "../types/models";
import { TaskPlanModal } from "./TaskPlanModal";

const mocks = vi.hoisted(() => ({
  mutateAsync: vi.fn(),
  reset: vi.fn(),
  isPending: false,
  isError: false,
  error: null as unknown,
}));

vi.mock("../api/hooks", () => ({
  useSetTaskPlannedDate: () => mocks,
}));

const task: Task = {
  id: "task-plan",
  title: "安排发布检查",
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
  plannedDate: "2026-08-28",
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: 1,
  version: 4,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-28T00:00:00Z",
  updatedAt: "2026-08-28T00:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

describe("TaskPlanModal", () => {
  beforeEach(() => {
    mocks.mutateAsync.mockReset();
    mocks.reset.mockReset();
    mocks.isPending = false;
    mocks.isError = false;
    mocks.error = null;
  });

  afterEach(cleanup);

  it("saves an arbitrary date with the current task version", async () => {
    const onClose = vi.fn();
    mocks.mutateAsync.mockResolvedValue({});
    render(
      <TaskPlanModal
        onClose={onClose}
        open
        selectedDate="2026-08-28"
        task={task}
      />,
    );
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-09-03" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存计划" }));

    expect(mocks.mutateAsync).toHaveBeenCalledWith({
      taskId: task.id,
      expectedVersion: task.version,
      plannedDate: "2026-09-03",
    });
    await vi.waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("supports clearing the plan", async () => {
    const onClose = vi.fn();
    mocks.mutateAsync.mockResolvedValue({});
    render(
      <TaskPlanModal
        onClose={onClose}
        open
        selectedDate="2026-08-28"
        task={task}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "未排期" }));
    fireEvent.click(screen.getByRole("button", { name: "保存计划" }));
    await vi.waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(mocks.mutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({ plannedDate: null }),
    );
  });

  it("closes without a write when the plan did not change", () => {
    const onClose = vi.fn();
    render(
      <TaskPlanModal
        onClose={onClose}
        open
        selectedDate="2026-08-28"
        task={task}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "保存计划" }));
    expect(mocks.mutateAsync).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("keeps the form open and explains an unconfirmed response", () => {
    mocks.isError = true;
    mocks.error = new ApiError("response lost", { code: "NETWORK_ERROR" });
    const onClose = vi.fn();
    render(
      <TaskPlanModal
        onClose={onClose}
        open
        selectedDate="2026-08-28"
        task={task}
      />,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "无法确认改期结果，任务列表已刷新",
    );
    expect(screen.getByLabelText("计划日期")).toHaveValue("2026-08-28");
    expect(onClose).not.toHaveBeenCalled();
  });
});
