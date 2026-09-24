import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, deleteTask } from "../api/client";
import type { Task } from "../types/models";
import { TaskDeleteConfirmModal } from "./TaskDeleteConfirmModal";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  deleteTask: vi.fn(),
}));

const task = {
  id: "task-1",
  title: "整理季度报告",
  version: 3,
} as Task;

function mount() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskDeleteConfirmModal task={task} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

describe("TaskDeleteConfirmModal", () => {
  beforeEach(() => vi.resetAllMocks());
  afterEach(() => cleanup());

  it("warns about active Agent work and translates its deletion guard", async () => {
    vi.mocked(deleteTask).mockRejectedValueOnce(
      new ApiError("active run", {
        status: 409,
        code: "TASK_HAS_ACTIVE_AGENT_RUN",
      }),
    );
    mount();

    expect(screen.getByText(/存在活动 Agent 作业/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(
      await screen.findByText(
        "该任务仍有执行中的 Agent 作业。请先等待或取消执行；若产出登记待恢复，请先打开执行过程并重试登记。",
      ),
    ).toBeVisible();
    expect(deleteTask).toHaveBeenCalledWith(task.id, task.version);
  });
});
