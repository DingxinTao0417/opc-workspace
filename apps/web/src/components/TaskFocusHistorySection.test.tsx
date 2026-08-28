import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusSession } from "../types/models";
import { TaskFocusHistorySection } from "./TaskFocusHistorySection";

const apiMocks = vi.hoisted(() => ({ getFocusSessions: vi.fn() }));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

const session: FocusSession = {
  id: "focus-1",
  taskId: "task-1",
  taskTitle: "整理交付",
  status: "completed",
  plannedSeconds: 3000,
  accumulatedSeconds: 1500,
  startedAt: "2026-08-28T10:00:00Z",
  endedAt: "2026-08-28T10:25:00Z",
  lastResumedAt: null,
  lastHeartbeatAt: "2026-08-28T10:25:00Z",
  endReason: "completed",
  version: 2,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:25:00Z",
};

function renderSection() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskFocusHistorySection taskId="task-1" />
    </QueryClientProvider>,
  );
}

describe("TaskFocusHistorySection", () => {
  beforeEach(() => {
    apiMocks.getFocusSessions.mockResolvedValue({
      items: [session],
      meta: { page: 1, pageSize: 5, total: 1 },
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("loads task-filtered terminal Focus sessions only when expanded", async () => {
    renderSection();
    expect(apiMocks.getFocusSessions).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(await screen.findByText("已计入工时")).toBeVisible();
    expect(screen.getByText("25:00")).toBeVisible();
    expect(apiMocks.getFocusSessions).toHaveBeenCalledWith({
      page: 1,
      pageSize: 5,
      status: "terminal",
      taskId: "task-1",
    });
  });

  it("paginates without replacing Task facts", async () => {
    apiMocks.getFocusSessions.mockImplementation(
      async (input: { page: number }) => ({
        items: [{ ...session, id: `focus-${input.page}` }],
        meta: { page: input.page, pageSize: 5, total: 6 },
      }),
    );
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "下一页任务专注记录" }),
    );

    await waitFor(() =>
      expect(apiMocks.getFocusSessions).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 2, taskId: "task-1" }),
      ),
    );
    expect(await screen.findByText("2 / 2 · 共 6 条")).toBeVisible();
  });

  it("shows an independent retryable error", async () => {
    apiMocks.getFocusSessions.mockRejectedValue(new Error("offline"));
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "查看记录" }));

    expect(
      await screen.findByText(
        "专注记录读取失败；任务其他内容不受影响。",
        {},
        { timeout: 3_000 },
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "重试" })).toBeVisible();
  });
});
