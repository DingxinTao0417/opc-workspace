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
import type { Actor, Task, TaskAssignment } from "../types/models";
import { TaskAssignmentsSection } from "./TaskAssignmentsSection";

const apiMocks = vi.hoisted(() => ({
  getAllActors: vi.fn(),
  getTaskAssignments: vi.fn(),
  createTaskAssignment: vi.fn(),
  reassignTaskAssignment: vi.fn(),
  endTaskAssignment: vi.fn(),
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
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 45,
  actualMinutes: 0,
  manualOrder: null,
  version: 3,
  subtaskTotal: 0,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T09:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  tags: [],
};

const owner: Actor = {
  id: "actor-owner",
  type: "owner",
  displayName: "我",
  status: "active",
  isBuiltin: true,
  notes: "",
  metadata: {},
  version: 1,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const person: Actor = {
  ...owner,
  id: "actor-person",
  type: "person",
  displayName: "陈设计",
  isBuiltin: false,
};

const nextPerson: Actor = {
  ...person,
  id: "actor-person-2",
  displayName: "林顾问",
};

const activeAssignment: TaskAssignment = {
  id: "assignment-active",
  taskId: task.id,
  role: "assignee",
  actorId: person.id,
  actor: person,
  assignedByActorId: owner.id,
  assignedByActor: owner,
  assignedAt: "2026-08-27T09:00:00Z",
  unassignedAt: null,
  reason: null,
  isActive: true,
  inferred: false,
};

const endedAssignment: TaskAssignment = {
  ...activeAssignment,
  id: "assignment-ended",
  unassignedAt: "2026-08-27T10:00:00Z",
  reason: null,
  isActive: false,
  inferred: true,
};

function assignmentPage(
  overrides: Partial<{
    active: {
      assignee: TaskAssignment | null;
      reviewer: TaskAssignment | null;
    };
    history: TaskAssignment[];
    page: number;
    pageSize: number;
    total: number;
    taskVersion: number;
  }> = {},
) {
  return {
    active: overrides.active ?? {
      assignee: activeAssignment,
      reviewer: null,
    },
    history: overrides.history ?? [endedAssignment],
    meta: {
      page: overrides.page ?? 1,
      pageSize: overrides.pageSize ?? 20,
      total: overrides.total ?? 1,
      taskVersion: overrides.taskVersion ?? task.version,
    },
  };
}

function renderSection(value: Task = task) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskAssignmentsSection task={value} />
    </QueryClientProvider>,
  );
}

describe("TaskAssignmentsSection", () => {
  beforeEach(() => {
    apiMocks.getTaskAssignments.mockResolvedValue(assignmentPage());
    apiMocks.getAllActors.mockResolvedValue([owner, person, nextPerson]);
    apiMocks.createTaskAssignment.mockResolvedValue({
      assignment: activeAssignment,
      task: { ...task, version: 4 },
    });
    apiMocks.reassignTaskAssignment.mockResolvedValue({
      previousAssignment: endedAssignment,
      assignment: { ...activeAssignment, actorId: owner.id, actor: owner },
      task: { ...task, version: 4 },
    });
    apiMocks.endTaskAssignment.mockResolvedValue({
      assignment: endedAssignment,
      task: { ...task, version: 4 },
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows current snapshots and paginates inferred assignment history", async () => {
    apiMocks.getTaskAssignments.mockImplementation(
      async (_taskId: string, input: { page: number }) =>
        input.page === 1
          ? assignmentPage({ pageSize: 1, total: 2 })
          : assignmentPage({
              history: [
                {
                  ...endedAssignment,
                  id: "assignment-older",
                  actorId: owner.id,
                  actor: owner,
                },
              ],
              page: 2,
              pageSize: 1,
              total: 2,
            }),
    );
    renderSection();

    expect(await screen.findByText("陈设计")).toBeInTheDocument();
    expect(
      screen.getByText("仅在本机记录，不会通知对方或授予访问权限。"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /历史 2/ }));
    expect(screen.getAllByText("迁移推定").length).toBeGreaterThan(0);
    expect(screen.queryByText(/schema_v7/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "加载更早" }));
    expect((await screen.findAllByText("我")).length).toBeGreaterThan(0);
    await waitFor(() =>
      expect(screen.getAllByText("迁移推定")).toHaveLength(2),
    );
    expect(apiMocks.getTaskAssignments).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({ page: 2 }),
    );
  });

  it.each([
    ["Task completed", "任务完成后自动结束"],
    ["Task cancelled", "任务取消后自动结束"],
  ])("translates the internal assignment reason %s", async (reason, label) => {
    apiMocks.getTaskAssignments.mockResolvedValue(
      assignmentPage({
        history: [{ ...endedAssignment, inferred: false, reason }],
      }),
    );
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: /历史 1/ }));
    expect(screen.getByText(`原因：${label}`)).toBeInTheDocument();
    expect(screen.queryByText(new RegExp(reason))).not.toBeInTheDocument();
  });

  it("offers only active owner/person candidates and explains person semantics", async () => {
    const system: Actor = {
      ...owner,
      id: "actor-system",
      type: "system",
      displayName: "系统",
    };
    const agent: Actor = {
      ...owner,
      id: "actor-agent",
      type: "agent",
      displayName: "Agent 占位",
    };
    const inactive = {
      ...nextPerson,
      id: "inactive",
      status: "inactive" as const,
    };
    apiMocks.getTaskAssignments.mockResolvedValue(
      assignmentPage({
        active: { assignee: null, reviewer: null },
        history: [],
        total: 0,
      }),
    );
    apiMocks.getAllActors.mockResolvedValue([
      owner,
      person,
      system,
      agent,
      inactive,
    ]);
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "分派负责人" }));
    expect(await screen.findByRole("option", { name: /陈设计/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /我/ })).toBeVisible();
    expect(
      screen.queryByRole("option", { name: /系统/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /Agent/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /林顾问/ }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("option", { name: /陈设计/ }));
    expect(
      screen.getByText(/仅在本机记录 陈设计 为负责人；不会通知对方/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认分派" }));

    await waitFor(() =>
      expect(apiMocks.createTaskAssignment).toHaveBeenCalledWith(
        task.id,
        {
          role: "assignee",
          actorId: person.id,
          expectedVersion: task.version,
        },
        expect.any(String),
      ),
    );
  });

  it("offers only the active owner for the reviewer role", async () => {
    apiMocks.getTaskAssignments.mockResolvedValue(
      assignmentPage({
        active: { assignee: null, reviewer: null },
        history: [],
        total: 0,
      }),
    );
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "设置审核人" }));
    expect(await screen.findByRole("option", { name: /我/ })).toBeVisible();
    expect(
      screen.queryByRole("option", { name: /陈设计/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: /林顾问/ }),
    ).not.toBeInTheDocument();
  });

  it("keeps snapshots visible when the candidate query fails", async () => {
    apiMocks.getAllActors.mockRejectedValue(new Error("offline"));
    renderSection();

    expect(await screen.findByText("陈设计")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "改派" }));
    expect(
      await screen.findByText(
        "候选人读取失败；当前责任记录仍可查看。",
        {},
        { timeout: 3_000 },
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("陈设计")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /历史 1/ }));
    expect(screen.getAllByText("迁移推定").length).toBeGreaterThan(0);
  });

  it("requires a reason, pins the observed version, and never auto-replays a conflict", async () => {
    apiMocks.getTaskAssignments
      .mockResolvedValueOnce(assignmentPage({ taskVersion: 3 }))
      .mockResolvedValue(assignmentPage({ taskVersion: 4 }));
    apiMocks.reassignTaskAssignment
      .mockRejectedValueOnce(
        new ApiError("任务已变化", {
          code: "VERSION_CONFLICT",
          status: 409,
          requestId: "req-conflict",
        }),
      )
      .mockResolvedValueOnce({
        previousAssignment: endedAssignment,
        assignment: { ...activeAssignment, actorId: owner.id, actor: owner },
        task: { ...task, version: 5 },
      });
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "改派" }));
    fireEvent.click(await screen.findByRole("option", { name: /我/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认改派" }));
    expect(await screen.findByText("请填写原因。")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("改派原因"), {
      target: { value: "转交所有者" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认改派" }));

    expect(
      await screen.findByText(/已读取最新版 v4，你的选择和原因仍保留/),
    ).toBeInTheDocument();
    expect(apiMocks.reassignTaskAssignment).toHaveBeenCalledTimes(1);
    expect(apiMocks.reassignTaskAssignment).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({ expectedVersion: 3 }),
      expect.any(String),
    );

    fireEvent.click(screen.getByRole("button", { name: "保留选择" }));
    expect(apiMocks.reassignTaskAssignment).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "确认改派" }));

    await waitFor(() =>
      expect(apiMocks.reassignTaskAssignment).toHaveBeenCalledTimes(2),
    );
    expect(apiMocks.reassignTaskAssignment).toHaveBeenLastCalledWith(
      task.id,
      expect.objectContaining({
        reason: "转交所有者",
        expectedVersion: 4,
      }),
      expect.any(String),
    );
  });

  it.each(["done", "cancelled"] as const)(
    "blocks assign/reassign on %s tasks but still allows ending",
    async (status) => {
      renderSection({ ...task, status, version: 7 });

      expect(
        await screen.findByRole("button", { name: "改派" }),
      ).toBeDisabled();
      expect(screen.getByRole("button", { name: "设置审核人" })).toBeDisabled();
      fireEvent.click(screen.getByRole("button", { name: "结束" }));
      fireEvent.change(screen.getByLabelText("结束原因"), {
        target: { value: "任务已经结束" },
      });
      fireEvent.click(screen.getByRole("button", { name: "确认结束" }));

      await waitFor(() =>
        expect(apiMocks.endTaskAssignment).toHaveBeenCalledWith(
          activeAssignment.id,
          { reason: "任务已经结束", expectedVersion: 7 },
          expect.any(String),
        ),
      );
    },
  );
});
