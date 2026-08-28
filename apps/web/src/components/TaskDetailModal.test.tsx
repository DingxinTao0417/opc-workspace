import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { TaskDetailModal } from "./TaskDetailModal";

const apiMocks = vi.hoisted(() => ({
  getTask: vi.fn(),
  updateTask: vi.fn(),
  deleteTask: vi.fn(),
  getAllProjects: vi.fn(),
  getAllTags: vi.fn(),
  getAllTasks: vi.fn(),
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
  kind: "work",
  status: "todo",
  priority: "P2",
  projectId: null,
  parentTaskId: null,
  completionCriteria: "",
  dueDate: "2026-08-29T10:00:00Z",
  plannedDate: "2026-08-28",
  estimatedMinutes: 45,
  actualMinutes: 10,
  manualOrder: null,
  version: 3,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T09:00:00Z",
  completedAt: null,
  tags: [],
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
    apiMocks.getAllProjects.mockResolvedValue([]);
    apiMocks.getAllTags.mockResolvedValue([]);
    apiMocks.getAllTasks.mockResolvedValue([]);
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
      expect(apiMocks.deleteTask).toHaveBeenCalledWith(task.id, task.version),
    );
    await waitFor(() => expect(useUiStore.getState().taskDetailId).toBeNull());
  });

  it("keeps the draft and retries against the latest task version", async () => {
    const latestTask = {
      ...task,
      title: "其他窗口保存的名称",
      version: task.version + 1,
      updatedAt: "2026-08-27T10:00:00Z",
    };
    apiMocks.getTask
      .mockResolvedValueOnce(task)
      .mockResolvedValueOnce(latestTask);
    apiMocks.updateTask
      .mockRejectedValueOnce(
        new ApiError("任务已发生变化", { code: "VERSION_CONFLICT" }),
      )
      .mockResolvedValueOnce({
        ...latestTask,
        title: "保留的本地草稿",
        version: latestTask.version + 1,
      });
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    fireEvent.change(title, { target: { value: "保留的本地草稿" } });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(
      await screen.findByText(
        `已读取最新版 v${latestTask.version}，你的草稿仍保留。请决定如何继续。`,
      ),
    ).toBeInTheDocument();
    expect(title).toHaveValue("保留的本地草稿");

    fireEvent.click(screen.getByRole("button", { name: "保留草稿重试" }));
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() => expect(apiMocks.updateTask).toHaveBeenCalledTimes(2));
    expect(apiMocks.updateTask).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({
        title: "保留的本地草稿",
        expectedVersion: latestTask.version,
      }),
    );
  });
});
