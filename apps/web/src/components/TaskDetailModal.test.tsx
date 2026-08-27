import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { TaskDetailModal } from "./TaskDetailModal";

const apiMocks = vi.hoisted(() => ({
  getTask: vi.fn(),
  updateTask: vi.fn(),
  deleteTask: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const task: Task = {
  id: "task-1",
  title: "整理项目简报",
  description: "核对范围",
  status: "todo",
  priority: "P2",
  projectId: null,
  dueDate: "2026-08-29T10:00:00Z",
  plannedDate: "2026-08-28",
  estimatedMinutes: 45,
  actualMinutes: 10,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T09:00:00Z",
  completedAt: null,
};

function renderModal() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskDetailModal />
    </QueryClientProvider>,
  );
}

describe("TaskDetailModal", () => {
  beforeEach(() => {
    apiMocks.getTask.mockResolvedValue(task);
    apiMocks.updateTask.mockResolvedValue({
      ...task,
      title: "整理最终项目简报",
      priority: "P1",
    });
    apiMocks.deleteTask.mockResolvedValue(undefined);
    useUiStore.setState({ taskDetailId: task.id });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    useUiStore.setState({ taskDetailId: null });
  });

  it("loads and saves editable task fields", async () => {
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    fireEvent.change(title, { target: { value: "整理最终项目简报" } });
    fireEvent.change(screen.getByLabelText("描述"), {
      target: { value: "核对范围与交付时间" },
    });
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-08-30" },
    });
    fireEvent.change(screen.getByLabelText("预计时长"), {
      target: { value: "90" },
    });
    fireEvent.click(screen.getByRole("button", { name: "高" }));
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() =>
      expect(apiMocks.updateTask).toHaveBeenCalledWith(
        task.id,
        expect.objectContaining({
          title: "整理最终项目简报",
          description: "核对范围与交付时间",
          priority: "P1",
          plannedDate: "2026-08-30",
          estimatedMinutes: 90,
        }),
      ),
    );
    await waitFor(() => expect(useUiStore.getState().taskDetailId).toBeNull());
  });

  it("requires an explicit confirmation before deleting", async () => {
    renderModal();

    await screen.findByLabelText("任务名称");
    fireEvent.click(screen.getByRole("button", { name: "删除任务" }));

    expect(apiMocks.deleteTask).not.toHaveBeenCalled();
    expect(screen.getByText("删除后无法恢复，确定继续？")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    await waitFor(() =>
      expect(apiMocks.deleteTask).toHaveBeenCalledWith(task.id),
    );
    await waitFor(() => expect(useUiStore.getState().taskDetailId).toBeNull());
  });
});
