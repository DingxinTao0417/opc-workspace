import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { TaskDetailModal } from "./TaskDetailModal";

const apiMocks = vi.hoisted(() => ({
  getTask: vi.fn(),
  updateTask: vi.fn(),
  deleteTask: vi.fn(),
  getTaskAssignments: vi.fn(),
  getAllActors: vi.fn(),
  createTaskAssignment: vi.fn(),
  reassignTaskAssignment: vi.fn(),
  endTaskAssignment: vi.fn(),
  executeTaskLifecycleCommand: vi.fn(),
  getTaskEvents: vi.fn(),
  getTaskSubmissions: vi.fn(),
  getTaskArtifacts: vi.fn(),
  getTaskArtifact: vi.fn(),
  submitTaskOutput: vi.fn(),
  reviewTaskSubmission: vi.fn(),
  deleteTaskArtifact: vi.fn(),
  downloadTaskArtifact: vi.fn(),
  getProject: vi.fn(),
  getProjects: vi.fn(),
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
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
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
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

function CurrentLocation() {
  return (
    <output data-testid="current-location">{useLocation().pathname}</output>
  );
}

function renderModal(initialEntry = "/tasks") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={queryClient}>
        <TaskDetailModal />
        <CurrentLocation />
      </QueryClientProvider>
    </MemoryRouter>,
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
    apiMocks.getTaskAssignments.mockResolvedValue({
      active: { assignee: null, reviewer: null },
      history: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
    apiMocks.getAllActors.mockResolvedValue([
      {
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
      },
    ]);
    apiMocks.getProjects.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    apiMocks.getAllTags.mockResolvedValue([]);
    apiMocks.getAllTasks.mockResolvedValue([]);
    apiMocks.getTaskEvents.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
    });
    apiMocks.getTaskSubmissions.mockResolvedValue({
      items: [],
      meta: { page: 1, pageSize: 10, total: 0, taskVersion: task.version },
    });
    useUiStore.setState({ taskDetailId: task.id });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    useUiStore.setState({ taskDetailId: null });
  });

  it("loads and saves editable task fields", async () => {
    const selectableProject = {
      id: "project-1",
      name: "品牌官网改版",
      status: "planning",
      clientName: null,
    };
    apiMocks.getProjects.mockResolvedValue({
      items: [selectableProject],
      meta: { page: 1, pageSize: 20, total: 1 },
    });
    apiMocks.getProject.mockResolvedValue(selectableProject);
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    fireEvent.focus(screen.getByLabelText("项目"));
    fireEvent.click(
      await screen.findByRole("option", {
        name: "品牌官网改版，规划中，未关联客户",
      }),
    );
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
    await waitFor(() =>
      expect(screen.getByLabelText("验收策略")).toBeEnabled(),
    );
    fireEvent.change(screen.getByLabelText("验收策略"), {
      target: { value: "manual" },
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
          projectId: "project-1",
          reviewPolicy: "manual",
          plannedDate: "2026-08-30",
          estimatedMinutes: 90,
        }),
      ),
    );
    await waitFor(() => expect(useUiStore.getState().taskDetailId).toBeNull());
  });

  it("keeps an archived project fallback visible and unchanged when options fail", async () => {
    const archivedProjectId = "project-archived";
    apiMocks.getTask.mockResolvedValue({
      ...task,
      projectId: archivedProjectId,
      projectName: "已归档网站项目",
    });
    apiMocks.getProject.mockRejectedValue(new Error("project unavailable"));

    renderModal();

    expect(await screen.findByLabelText("项目")).toHaveValue("已归档网站项目");
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() =>
      expect(apiMocks.updateTask).toHaveBeenCalledWith(
        task.id,
        expect.objectContaining({ projectId: archivedProjectId }),
      ),
    );
  });

  it("hydrates and closes a refreshable task detail route", async () => {
    useUiStore.setState({ taskDetailId: null });
    renderModal(`/tasks/${task.id}`);

    expect(await screen.findByLabelText("任务名称")).toHaveValue(task.title);
    expect(useUiStore.getState().taskDetailId).toBe(task.id);
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.getByTestId("current-location")).toHaveTextContent("/tasks");
    expect(useUiStore.getState().taskDetailId).toBeNull();
  });

  it("requires saving fact changes before a lifecycle command", async () => {
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    const startButton = screen.getByRole("button", { name: "开始执行" });

    expect(startButton).toBeEnabled();
    fireEvent.change(title, { target: { value: "尚未保存的任务名称" } });

    expect(
      screen.getByText("当前有未保存的任务信息；请先保存，再执行状态操作。"),
    ).toBeVisible();
    expect(startButton).toBeDisabled();
    expect(apiMocks.executeTaskLifecycleCommand).not.toHaveBeenCalled();
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

  it("explains how to resolve an active Inbox relation before deletion", async () => {
    apiMocks.deleteTask.mockRejectedValueOnce(
      new ApiError(
        "Unlink the Task from active Inbox Items before deleting it",
        {
          status: 409,
          code: "TASK_HAS_ACTIVE_INBOX_RELATIONS",
        },
      ),
    );
    renderModal();

    await screen.findByLabelText("任务名称");
    fireEvent.click(screen.getByRole("button", { name: "删除任务" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(
      await screen.findByText(
        "该任务仍被收件箱条目关联。请先到收件箱解除活动关联，再删除任务。",
      ),
    ).toBeInTheDocument();
    expect(useUiStore.getState().taskDetailId).toBe(task.id);
  });

  it("keeps an unsaved task draft while an assignment write updates the task version", async () => {
    const updatedTask = {
      ...task,
      version: task.version + 1,
      updatedAt: "2026-08-27T09:30:00Z",
    };
    apiMocks.getTask.mockResolvedValueOnce(task).mockResolvedValue(updatedTask);
    apiMocks.getTaskAssignments
      .mockResolvedValueOnce({
        active: { assignee: null, reviewer: null },
        history: [],
        meta: { page: 1, pageSize: 20, total: 0, taskVersion: task.version },
      })
      .mockResolvedValue({
        active: { assignee: null, reviewer: null },
        history: [],
        meta: {
          page: 1,
          pageSize: 20,
          total: 0,
          taskVersion: updatedTask.version,
        },
      });
    let finishAssignment!: (value: unknown) => void;
    apiMocks.createTaskAssignment.mockReturnValue(
      new Promise((resolve) => {
        finishAssignment = resolve;
      }),
    );
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    fireEvent.change(title, { target: { value: "尚未保存的本地草稿" } });
    fireEvent.click(screen.getByRole("button", { name: "分派负责人" }));
    fireEvent.click(await screen.findByRole("option", { name: /我/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认分派" }));

    await waitFor(() =>
      expect(apiMocks.createTaskAssignment).toHaveBeenCalled(),
    );
    expect(screen.getByRole("button", { name: "保存修改" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "删除任务" })).toBeDisabled();

    await act(async () => {
      finishAssignment({
        assignment: {
          id: "assignment-1",
          taskId: task.id,
          role: "assignee",
          actorId: "actor-owner",
          actor: {
            id: "actor-owner",
            type: "owner",
            displayName: "我",
            status: "active",
            isBuiltin: true,
            version: 1,
          },
          assignedByActorId: "actor-owner",
          assignedByActor: {
            id: "actor-owner",
            type: "owner",
            displayName: "我",
            status: "active",
            isBuiltin: true,
            version: 1,
          },
          assignedAt: "2026-08-27T09:30:00Z",
          unassignedAt: null,
          reason: null,
          isActive: true,
          inferred: false,
        },
        task: updatedTask,
      });
    });

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "保存修改" })).toBeEnabled(),
    );
    expect(title).toHaveValue("尚未保存的本地草稿");
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() =>
      expect(apiMocks.updateTask).toHaveBeenCalledWith(
        task.id,
        expect.objectContaining({
          title: "尚未保存的本地草稿",
          expectedVersion: updatedTask.version,
        }),
      ),
    );
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

  it("does not claim the latest version when the conflict refresh fails", async () => {
    apiMocks.getTask
      .mockResolvedValueOnce(task)
      .mockRejectedValue(new Error("offline"));
    apiMocks.updateTask.mockRejectedValueOnce(
      new ApiError("任务已发生变化", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    renderModal();

    const title = await screen.findByLabelText("任务名称");
    fireEvent.change(title, { target: { value: "仍需保留的草稿" } });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(
      await screen.findByText(
        "尚未确认最新版，你的草稿仍保留；当前不会重试写入。",
        {},
        { timeout: 4_000 },
      ),
    ).toBeVisible();
    expect(title).toHaveValue("仍需保留的草稿");
    expect(screen.getByRole("button", { name: "载入最新" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "保留草稿重试" })).toBeDisabled();
  });
});
