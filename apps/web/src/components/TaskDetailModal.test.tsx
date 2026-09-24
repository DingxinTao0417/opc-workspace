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
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
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

vi.mock("./TaskSelect", () => ({
  TaskSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    selectedTitle,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    selectedTitle?: string | null;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      {value && value !== "task-parent" ? (
        <option value={value}>{selectedTitle ?? "当前父任务"}</option>
      ) : null}
      <option value="task-parent">父任务候选</option>
    </select>
  ),
}));

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
  it("passes explicit conversation to the execution drawer only after the task draft is saved or cleared", async () => {
    const id = "33333333-3333-4333-8333-333333333333";
    const session = "33333333-3333-4333-8333-333333333334";
    apiMocks.getTask.mockResolvedValue({ ...task, id });
    useUiStore.setState({ taskDetailId: id, agentRunDrawer: null });
    renderModal(`/tasks/${id}?return_session=${session}`);
    const input = await screen.findByLabelText("任务名称");
    fireEvent.change(input, { target: { value: "未保存任务草稿" } });
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    expect(
      screen.queryByRole("link", { name: "返回原对话" }),
    ).not.toBeInTheDocument();
    const open = screen.getByRole("button", { name: "查看执行过程" });
    expect(open).toBeDisabled();
    fireEvent.click(open);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
    fireEvent.change(input, { target: { value: task.title } });
    fireEvent.click(open);
    expect(useUiStore.getState().agentRunDrawer).toMatchObject({
      taskId: id,
      returnSession: session,
    });
    expect(apiMocks.updateTask).not.toHaveBeenCalled();
  });
  it.each(["", "?return_session=invalid"])(
    "does not infer a return conversation on an ordinary task %s",
    async (query) => {
      const id = "33333333-3333-4333-8333-333333333333";
      apiMocks.getTask.mockResolvedValue({ ...task, id });
      useUiStore.setState({ taskDetailId: id });
      renderModal(`/tasks/${id}${query}`);
      await screen.findByLabelText("任务名称");
      expect(
        screen.queryByRole("link", { name: "返回原对话" }),
      ).not.toBeInTheDocument();
    },
  );
  it("returns from the ordinary Task modal without sending or changing the Task", async () => {
    const id = "33333333-3333-4333-8333-333333333333";
    const session = "33333333-3333-4333-8333-333333333334";
    apiMocks.getTask.mockResolvedValue({ ...task, id });
    useUiStore.setState({ taskDetailId: id });
    useAiChatStore.setState({ activeSessionId: "before-task-return" });
    renderModal(`/tasks/${id}?return_session=${session}`);
    await screen.findByLabelText("任务名称");
    expect(useAiChatStore.getState().activeSessionId).toBe(
      "before-task-return",
    );
    const returnLink = screen.getByRole("link", { name: "返回原对话" });
    for (const modifiers of [
      { ctrlKey: true },
      { metaKey: true },
      { shiftKey: true },
      { altKey: true },
      { button: 1 },
    ]) {
      document.addEventListener("click", (event) => event.preventDefault(), {
        once: true,
      });
      fireEvent.click(returnLink, modifiers);
      expect(useUiStore.getState().taskDetailId).toBe(id);
      expect(useAiChatStore.getState().activeSessionId).toBe(
        "before-task-return",
      );
      expect(screen.getByTestId("current-location")).not.toHaveTextContent(
        "/ai",
      );
    }
    fireEvent(
      returnLink,
      new MouseEvent("auxclick", { bubbles: true, button: 1 }),
    );
    expect(useUiStore.getState().taskDetailId).toBe(id);
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(screen.getByTestId("current-location")).toHaveTextContent("/ai");
    expect(useUiStore.getState().taskDetailId).toBeNull();
    expect(useAiChatStore.getState().activeSessionId).toBe(session);
    expect(apiMocks.updateTask).not.toHaveBeenCalled();
    expect(apiMocks.executeTaskLifecycleCommand).not.toHaveBeenCalled();
  });
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
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    vi.clearAllMocks();
    useUiStore.setState({ taskDetailId: null });
  });

  it("hands off the saved task identity only after its unsaved draft is cleared", async () => {
    const id = "33333333-3333-4333-8333-333333333333";
    apiMocks.getTask.mockResolvedValue({ ...task, id });
    useUiStore.setState({ taskDetailId: id });
    renderModal(`/tasks/${id}`);
    const input = await screen.findByLabelText("任务名称");
    const handoff = screen.getByRole("button", { name: "交给智能体" });
    expect(handoff).toBeEnabled();
    fireEvent.change(input, { target: { value: "尚未保存的新标题" } });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    fireEvent.change(input, { target: { value: task.title } });
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "任务",
      route: `/tasks/${id}`,
      scopes: ["work", "outputs", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=task");
    expect(pending?.prompt).toContain(`id=${id}`);
    expect(pending?.prompt).toContain("workspace_task_assignments");
    expect(pending?.prompt).toContain("workspace_task_submissions");
    expect(pending?.prompt).toContain("不要启动 Agent");
    expect(useUiStore.getState().taskDetailId).toBeNull();
    expect(screen.getByTestId("current-location")).toHaveTextContent("/ai");
    expect(apiMocks.updateTask).not.toHaveBeenCalled();
    expect(apiMocks.executeTaskLifecycleCommand).not.toHaveBeenCalled();
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
    fireEvent.change(screen.getByLabelText("父任务"), {
      target: { value: "task-parent" },
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
          parentTaskId: "task-parent",
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
    expect(
      screen.getByText("删除后无法恢复；活动 Agent 作业会阻止删除。确定继续？"),
    ).toBeTruthy();

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

  it("explains how to resolve an active Agent run before deletion", async () => {
    apiMocks.deleteTask.mockRejectedValueOnce(
      new ApiError("actor run is active", {
        status: 409,
        code: "TASK_HAS_ACTIVE_AGENT_RUN",
      }),
    );
    renderModal();

    await screen.findByLabelText("任务名称");
    fireEvent.click(screen.getByRole("button", { name: "删除任务" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(
      await screen.findByText(
        "该任务仍有执行中的 Agent 作业。请先等待或取消执行；若产出登记待恢复，请先在执行过程中重试登记。",
      ),
    ).toBeVisible();
    expect(useUiStore.getState().taskDetailId).toBe(task.id);
  });

  it("keeps the detail open and explains how to unlink Content Calendar items before deletion", async () => {
    apiMocks.deleteTask.mockRejectedValueOnce(
      new ApiError("Task is referenced by Content Calendar items", {
        status: 409,
        code: "TASK_CONTENT_ITEMS_EXIST",
      }),
    );
    renderModal();

    await screen.findByLabelText("任务名称");
    fireEvent.click(screen.getByRole("button", { name: "删除任务" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(
      await screen.findByText(
        "该任务仍被内容日历条目引用。请先到内容日历解除任务关联，再删除任务。",
      ),
    ).toBeVisible();
    expect(screen.getByLabelText("任务名称")).toBeVisible();
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
