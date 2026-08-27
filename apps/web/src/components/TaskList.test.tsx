import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { TaskList } from "./TaskList";

vi.mock("../api/hooks", () => ({
  useUpdateTaskStatus: () => ({ isPending: false, mutate: vi.fn() }),
}));

const task: Task = {
  id: "task-1",
  title: "整理项目简报",
  description: "",
  status: "todo",
  priority: "P2",
  projectId: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 30,
  actualMinutes: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T08:00:00Z",
  completedAt: null,
};

describe("TaskList", () => {
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
});
