import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Task, TaskWorkflowEvent } from "../types/models";
import { TaskLifecycleSection } from "./TaskLifecycleSection";

const apiMocks = vi.hoisted(() => ({
  executeTaskLifecycleCommand: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, ...apiMocks };
});

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
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: null,
  version: 4,
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

const event: TaskWorkflowEvent = {
  id: "event-1",
  action: "task_blocked",
  actor: null,
  assignmentId: null,
  submissionId: null,
  artifactId: null,
  requestId: "request-1",
  commandSeq: 1,
  previous: { status: "todo", version: 4 },
  current: { status: "blocked", version: 5, reason: "等待客户" },
  reason: "等待客户",
  createdAt: "2026-08-27T09:00:00Z",
};

function renderSection(
  value: Task = task,
  options: {
    hasUnsavedFacts?: boolean;
    onRefreshTask?: () => Promise<Task | null>;
    onTaskUpdated?: (task: Task) => void;
  } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return render(
    <TaskLifecycleSection
      hasUnsavedFacts={options.hasUnsavedFacts}
      onRefreshTask={options.onRefreshTask ?? (async () => value)}
      onTaskUpdated={options.onTaskUpdated}
      task={value}
    />,
    { wrapper: Wrapper },
  );
}

describe("TaskLifecycleSection", () => {
  beforeEach(() => {
    apiMocks.executeTaskLifecycleCommand.mockResolvedValue({
      task: { ...task, status: "blocked", version: 5 },
      event,
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows only commands allowed by each controlled status", () => {
    const { rerender } = renderSection();
    expect(screen.getByRole("button", { name: "开始执行" })).toBeVisible();
    expect(screen.getByRole("button", { name: "标记阻塞" })).toBeVisible();
    expect(screen.getByRole("button", { name: "完成任务" })).toBeVisible();
    expect(screen.getByRole("button", { name: "取消任务" })).toBeVisible();

    rerender(
      <TaskLifecycleSection
        onRefreshTask={async () => task}
        task={{
          ...task,
          status: "blocked",
          blockedReason: "等待客户",
          blockedFromStatus: "in_progress",
        }}
      />,
    );
    expect(screen.getByRole("button", { name: "解除阻塞" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "完成任务" })).toBeNull();
    expect(screen.getByText("阻塞原因：等待客户")).toBeVisible();
  });

  it("requires a reason and sends an explicit block command", async () => {
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "标记阻塞" }));
    fireEvent.click(screen.getByRole("button", { name: "确认标记阻塞" }));
    expect(screen.getByText("请填写原因。")).toBeVisible();
    fireEvent.change(screen.getByLabelText("阻塞原因"), {
      target: { value: "等待客户确认" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认标记阻塞" }));

    await waitFor(() =>
      expect(apiMocks.executeTaskLifecycleCommand).toHaveBeenCalledWith(
        task.id,
        {
          action: "block",
          reason: "等待客户确认",
          expectedVersion: task.version,
        },
        expect.any(String),
      ),
    );
  });

  it("keeps a conflict draft pinned until an explicit second submit", async () => {
    const latest = { ...task, version: 5 };
    apiMocks.executeTaskLifecycleCommand
      .mockRejectedValueOnce(
        new ApiError("任务已变化", { code: "VERSION_CONFLICT", status: 409 }),
      )
      .mockResolvedValueOnce({
        task: { ...latest, status: "blocked", version: 6 },
        event: { ...event, current: { status: "blocked", version: 6 } },
      });
    const refresh = vi.fn(async () => latest);
    renderSection(task, { onRefreshTask: refresh });

    fireEvent.click(screen.getByRole("button", { name: "标记阻塞" }));
    fireEvent.change(screen.getByLabelText("阻塞原因"), {
      target: { value: "等待反馈" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认标记阻塞" }));

    expect(
      await screen.findByText(/已读取最新版 v5；请确认后再次提交/),
    ).toBeVisible();
    expect(apiMocks.executeTaskLifecycleCommand).toHaveBeenCalledTimes(1);
    expect(apiMocks.executeTaskLifecycleCommand.mock.calls[0][1]).toMatchObject(
      {
        expectedVersion: 4,
        reason: "等待反馈",
      },
    );

    fireEvent.click(screen.getByRole("button", { name: "保留操作" }));
    expect(apiMocks.executeTaskLifecycleCommand).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("阻塞原因")).toHaveValue("等待反馈");
    fireEvent.click(screen.getByRole("button", { name: "确认标记阻塞" }));

    await waitFor(() =>
      expect(apiMocks.executeTaskLifecycleCommand).toHaveBeenCalledTimes(2),
    );
    expect(apiMocks.executeTaskLifecycleCommand.mock.calls[1][1]).toMatchObject(
      {
        expectedVersion: 5,
        reason: "等待反馈",
      },
    );
  });

  it("blocks lifecycle commands while task facts are unsaved", () => {
    renderSection(task, { hasUnsavedFacts: true });

    expect(
      screen.getByText("当前有未保存的任务信息；请先保存，再执行状态操作。"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "开始执行" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "完成任务" })).toBeDisabled();
  });

  it("explains that cancelling ends current assignments", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    expect(
      screen.getByText(/当前责任分派会结束，历史记录会保留/),
    ).toBeVisible();
  });
});
