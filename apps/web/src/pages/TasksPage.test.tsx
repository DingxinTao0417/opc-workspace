import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BrowserRouter } from "react-router-dom";
import { ApiError } from "../api/client";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useUiStore } from "../store/ui";
import type {
  Task,
  TaskListParams,
  TaskSavedView,
  TaskStatus,
} from "../types/models";
import { TasksPage } from "./TasksPage";

const mocks = vi.hoisted(() => ({
  getTask: vi.fn(),
  getTaskSavedViews: vi.fn(),
  batch: vi.fn(),
  move: vi.fn(),
  resetMove: vi.fn(),
  drag: vi.fn(),
  resetDrag: vi.fn(),
  resetBatch: vi.fn(),
  resetOrder: vi.fn(),
  resetResetOrder: vi.fn(),
  lifecycle: vi.fn(),
  resetLifecycle: vi.fn(),
  taskQueries: [] as TaskListParams[],
  taskQueryEnabled: [] as boolean[],
  taskRefetch: vi.fn(),
  taskItems: null as Task[] | null,
  taskResponsePage: null as number | null,
  taskTotal: 101,
  fetching: false,
  savedViews: [] as TaskSavedView[],
  placeholder: false,
  taskStatus: "todo" as TaskStatus,
}));

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getTask: mocks.getTask,
  getTaskSavedViews: mocks.getTaskSavedViews,
}));

const task: Task = {
  id: "task-1",
  title: "整理交付清单",
  description: "",
  kind: "work",
  status: "todo",
  priority: "P1",
  projectId: "project-1",
  projectName: "品牌官网改版",
  parentTaskId: null,
  completionCriteria: "清单经负责人确认",
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: "2026-08-27",
  estimatedMinutes: 30,
  actualMinutes: 0,
  manualOrder: null,
  version: 7,
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

vi.mock("../components/ClientSelect", () => ({
  ClientSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value="client-1">示例客户</option>
    </select>
  ),
}));

vi.mock("../components/ProjectSelect", () => ({
  ProjectSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value="project-1">品牌官网改版</option>
    </select>
  ),
}));

vi.mock("../api/hooks", () => ({
  useTaskPageQuery: (input: TaskListParams, enabled = true) => {
    mocks.taskQueries.push(input);
    mocks.taskQueryEnabled.push(enabled);
    return {
      data: {
        items: mocks.taskItems ?? [{ ...task, status: mocks.taskStatus }],
        meta: {
          page: mocks.taskResponsePage ?? input.page ?? 1,
          pageSize: 50,
          total: mocks.taskTotal,
        },
      },
      error: null,
      isError: false,
      isFetching: mocks.placeholder || mocks.fetching,
      isPending: false,
      isPlaceholderData: mocks.placeholder,
      isSuccess: true,
      refetch: mocks.taskRefetch,
    };
  },
  useTaskSavedViewsQuery: () => ({
    data: mocks.savedViews,
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useDeleteTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useTagOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useBatchUpdateTasks: () => ({
    error: null,
    isPending: false,
    mutate: mocks.batch,
    reset: mocks.resetBatch,
  }),
  useMoveTaskWithinPlan: () => ({
    error: null,
    isPending: false,
    mutate: mocks.move,
    reset: mocks.resetMove,
    variables: undefined,
  }),
  useReorderTaskWithinPlanStatus: () => ({
    error: null,
    isPending: false,
    mutate: mocks.drag,
    reset: mocks.resetDrag,
  }),
  useResetTaskOrder: () => ({
    error: null,
    isPending: false,
    mutate: mocks.resetOrder,
    reset: mocks.resetResetOrder,
  }),
  useTaskLifecycleCommand: () => ({
    error: null,
    isPending: false,
    mutate: mocks.lifecycle,
    reset: mocks.resetLifecycle,
  }),
  useCreateTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
  useDeleteTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
}));

function lastPageQuery(): TaskListParams | undefined {
  return [...mocks.taskQueries]
    .reverse()
    .find((input) => input.pageSize === 50);
}

function lastPageQueryEnabled(): boolean | undefined {
  for (let index = mocks.taskQueries.length - 1; index >= 0; index -= 1) {
    if (mocks.taskQueries[index]?.pageSize === 50) {
      return mocks.taskQueryEnabled[index];
    }
  }
  return undefined;
}

describe("TasksPage", () => {
  beforeEach(() => {
    mocks.getTask.mockReset();
    mocks.getTaskSavedViews.mockReset();
    mocks.taskQueries.length = 0;
    mocks.taskQueryEnabled.length = 0;
    mocks.taskRefetch.mockClear();
    mocks.batch.mockClear();
    mocks.move.mockClear();
    mocks.resetMove.mockClear();
    mocks.drag.mockClear();
    mocks.resetDrag.mockClear();
    mocks.resetBatch.mockClear();
    mocks.resetOrder.mockClear();
    mocks.resetResetOrder.mockClear();
    mocks.lifecycle.mockClear();
    mocks.resetLifecycle.mockClear();
    mocks.placeholder = false;
    mocks.fetching = false;
    mocks.taskStatus = "todo";
    mocks.taskItems = null;
    mocks.taskResponsePage = null;
    mocks.taskTotal = 101;
    mocks.savedViews = [];
    window.history.replaceState({}, "", "/tasks");
    useAiWorkbenchHandoff.setState({
      pending: null,
      pendingIssue: null,
      taskSelectionReturn: null,
    });
    useUiStore.setState({ taskDetailId: null, agentRunDrawer: null });
  });

  afterEach(cleanup);

  it("opens the exact Agent run from a strict route and preserves the return conversation", async () => {
    const taskId = "018f0000-0000-7000-8000-000000000111";
    const runId = "018f0000-0000-7000-8000-000000000112";
    const sessionId = "018f0000-0000-7000-8000-000000000113";
    window.history.replaceState(
      {},
      "",
      `/tasks/${taskId}?agent_run=${runId}&return_session=${sessionId}`,
    );
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    await waitFor(() =>
      expect(useUiStore.getState().agentRunDrawer).toEqual({
        taskId,
        runId,
        returnSession: sessionId,
      }),
    );
    expect(screen.getByRole("link", { name: "返回原对话" })).toHaveAttribute(
      "href",
      "/ai",
    );
    expect(useUiStore.getState().taskDetailId).toBeNull();
  });

  it("re-reads and applies a confirmed saved-view link before querying tasks", async () => {
    const id = "018f0000-0000-7000-8000-000000000201";
    const session = "018f0000-0000-7000-8000-000000000202";
    window.history.replaceState(
      {},
      "",
      `/tasks?task_view=${id}&return_session=${session}`,
    );
    mocks.getTaskSavedViews.mockResolvedValue([
      {
        id,
        name: "待验收",
        definition: {
          q: "交付",
          status: "waiting_review",
          priority: "P1",
          kind: "review",
          projectId: "",
          clientId: "",
          tagIds: [],
          plannedDate: "",
          plannedFrom: "",
          plannedTo: "",
          dueFrom: "",
          dueTo: "",
          sort: "-updated_at",
        },
        schemaVersion: 1,
        version: 1,
        createdAt: "2026-09-21T00:00:00Z",
        updatedAt: "2026-09-21T00:00:00Z",
      },
    ]);
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    expect(lastPageQueryEnabled()).toBe(false);
    expect(
      screen.queryByRole("button", { name: `查看任务：${task.title}` }),
    ).not.toBeInTheDocument();
    await waitFor(() => expect(lastPageQueryEnabled()).toBe(true));
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({
        q: "交付",
        status: "waiting_review",
        priority: "P1",
        rootOnly: false,
      }),
    );
    expect(screen.getByText(/已从保存视图“待验收”载入/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "返回原对话" })).toHaveAttribute(
      "href",
      "/ai",
    );
  });

  it("does not show an unfiltered task list when the linked view is gone", async () => {
    const id = "018f0000-0000-7000-8000-000000000203";
    window.history.replaceState({}, "", `/tasks?task_view=${id}`);
    mocks.getTaskSavedViews.mockResolvedValue([]);
    render(<TasksPage />);
    await waitFor(() =>
      expect(screen.getByText(/该保存视图已不存在/)).toBeInTheDocument(),
    );
    expect(lastPageQueryEnabled()).toBe(false);
    expect(
      screen.queryByRole("button", { name: `查看任务：${task.title}` }),
    ).not.toBeInTheDocument();
  });

  it("keeps task results hidden after a view read error and allows retry", async () => {
    const id = "018f0000-0000-7000-8000-000000000204";
    window.history.replaceState({}, "", `/tasks?task_view=${id}`);
    mocks.getTaskSavedViews.mockRejectedValueOnce(new Error("offline"));
    mocks.getTaskSavedViews.mockResolvedValueOnce([
      {
        id,
        name: "待办",
        definition: {
          q: "",
          status: "todo",
          priority: "",
          kind: "",
          projectId: "",
          clientId: "",
          tagIds: [],
          plannedDate: "",
          plannedFrom: "",
          plannedTo: "",
          dueFrom: "",
          dueTo: "",
          sort: "",
        },
        schemaVersion: 1,
        version: 1,
        createdAt: "2026-09-21T00:00:00Z",
        updatedAt: "2026-09-21T00:00:00Z",
      },
    ]);
    render(<TasksPage />);
    await waitFor(() =>
      expect(screen.getByText(/保存视图读取失败/)).toBeInTheDocument(),
    );
    expect(lastPageQueryEnabled()).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(lastPageQueryEnabled()).toBe(true));
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ status: "todo" }),
    );
  });

  it("fails closed for a malformed saved-view link", () => {
    const id = "018f0000-0000-7000-8000-000000000203";
    window.history.replaceState(
      {},
      "",
      `/tasks?task_view=${id}&task_view=${id}`,
    );
    render(<TasksPage />);
    expect(screen.getByText(/保存视图链接无效/)).toBeInTheDocument();
    expect(lastPageQueryEnabled()).toBe(false);
    expect(mocks.getTaskSavedViews).not.toHaveBeenCalled();
  });

  it("does not open a task or run for duplicate or unknown location keys", () => {
    const taskId = "018f0000-0000-7000-8000-000000000111";
    const runId = "018f0000-0000-7000-8000-000000000112";
    window.history.replaceState(
      {},
      "",
      `/tasks/${taskId}?agent_run=${runId}&agent_run=${runId}`,
    );
    const view = render(<TasksPage />);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
    expect(useUiStore.getState().taskDetailId).toBeNull();

    window.history.replaceState(
      {},
      "",
      `/tasks/${taskId}?agent_run=${runId}&unknown=1`,
    );
    view.rerender(<TasksPage />);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
    expect(useUiStore.getState().taskDetailId).toBeNull();
  });

  it("uses root pagination by default and switches to flat server filtering", () => {
    render(<TasksPage />);

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ page: 1, pageSize: 50, rootOnly: true }),
    );

    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("状态"), {
      target: { value: "todo" },
    });
    fireEvent.change(screen.getByLabelText("客户"), {
      target: { value: "client-1" },
    });
    fireEvent.change(screen.getByLabelText("项目"), {
      target: { value: "project-1" },
    });

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({
        status: "todo",
        clientId: "client-1",
        projectId: "project-1",
        rootOnly: false,
      }),
    );
    expect(screen.getByText("101 项")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({
        page: 2,
        status: "todo",
        clientId: "client-1",
        projectId: "project-1",
      }),
    );
  });

  it("settles directly on the last valid task page after totals shrink", async () => {
    const view = render(<TasksPage />);

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(lastPageQuery()).toEqual(expect.objectContaining({ page: 3 }));

    mocks.taskTotal = 0;
    view.rerender(<TasksPage />);

    await waitFor(() =>
      expect(lastPageQuery()).toEqual(expect.objectContaining({ page: 1 })),
    );
  });

  it("switches to the real six-state board without changing task lifecycle facts", () => {
    mocks.taskItems = [
      task,
      { ...task, id: "task-2", title: "等待复核", status: "waiting_review" },
    ];
    render(<TasksPage />);

    fireEvent.click(screen.getByRole("button", { name: "看板视图" }));

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ page: 1, pageSize: 50, rootOnly: false }),
    );
    expect(screen.getByLabelText("任务看板")).toBeVisible();
    for (const heading of [
      "待办",
      "进行中",
      "阻塞",
      "待验收",
      "已完成",
      "已取消",
    ]) {
      expect(screen.getByRole("heading", { name: heading })).toBeVisible();
    }

    fireEvent.click(
      screen.getByRole("button", { name: `查看任务：${task.title}` }),
    );
    expect(useUiStore.getState().taskDetailId).toBe(task.id);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    expect(screen.getByText("已选 1 项")).toBeVisible();
  });

  it("hands exact selected Task identities to the agent without copying row content or granting access", () => {
    const selectedId = "018f0000-0000-7000-8000-000000001930";
    mocks.taskItems = [
      { ...task, id: selectedId, description: "PRIVATE BODY" },
    ];
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "交给智能体（已选任务）" }),
    );

    expect(window.location.pathname).toBe("/ai");
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.label).toBe("已选 1 项任务");
    expect(pending?.scopes).toEqual(["work", "actions"]);
    expect(pending?.prompt).toContain(selectedId);
    expect(pending?.prompt).not.toContain(task.title);
    expect(pending?.prompt).not.toContain("PRIVATE BODY");
    expect(mocks.batch).not.toHaveBeenCalled();
    expect(useAiWorkbenchHandoff.getState().taskSelectionReturn?.ids).toEqual([
      selectedId,
    ]);
  });

  it("restores the prior view and re-reads the whole selection before batch writes", async () => {
    const selectedId = "018f0000-0000-7000-8000-000000001930";
    const definition = {
      q: "",
      status: "todo" as const,
      priority: "" as const,
      kind: "" as const,
      projectId: "",
      clientId: "",
      tagIds: [],
      plannedDate: "",
      plannedFrom: "",
      plannedTo: "",
      dueFrom: "",
      dueTo: "",
      sort: "",
    };
    useAiWorkbenchHandoff.getState().rememberTaskSelectionReturn({
      ids: [selectedId],
      definition,
      page: 2,
      view: "board",
    });
    mocks.taskItems = [{ ...task, id: selectedId }];
    mocks.getTask.mockResolvedValue({
      ...task,
      id: selectedId,
      title: "最新标题",
      version: 9,
    });
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    expect(screen.getByText("正在重新读取先前选中的任务…")).toBeVisible();
    await waitFor(() =>
      expect(
        screen.getByText("已重新读取并恢复 1 项选择；批量操作前请再次核对。"),
      ).toBeVisible(),
    );
    expect(screen.getByLabelText("任务看板")).toBeVisible();
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({ page: 2, status: "todo", rootOnly: false }),
    );
    expect(useAiWorkbenchHandoff.getState().taskSelectionReturn).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "set_project",
        items: [{ id: selectedId, expectedVersion: 9 }],
        projectId: null,
      },
      expect.any(Object),
    );
  });

  it("rejects the entire returned selection when any Task cannot be re-read", async () => {
    const ids = [
      "018f0000-0000-7000-8000-000000001930",
      "018f0000-0000-7000-8000-000000001931",
    ];
    useAiWorkbenchHandoff.getState().rememberTaskSelectionReturn({
      ids,
      definition: {
        q: "",
        status: "",
        priority: "",
        kind: "",
        projectId: "",
        clientId: "",
        tagIds: [],
        plannedDate: "",
        plannedFrom: "",
        plannedTo: "",
        dueFrom: "",
        dueTo: "",
        sort: "",
      },
      page: 1,
      view: "list",
    });
    mocks.getTask.mockImplementation((id: string) =>
      id === ids[0]
        ? Promise.resolve({ ...task, id, version: 12 })
        : Promise.reject(new Error("Task deleted")),
    );
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("alert", {
          name: "",
        }),
      ).toHaveTextContent("原选择中的任务无法全部重新读取，未恢复勾选"),
    );
    expect(screen.queryByLabelText("批量操作")).not.toBeInTheDocument();
    expect(useAiWorkbenchHandoff.getState().taskSelectionReturn).toBeNull();
    expect(mocks.batch).not.toHaveBeenCalled();
  });

  it("does not hand off more than 20 selected Tasks", () => {
    mocks.taskItems = Array.from({ length: 21 }, (_, index) => ({
      ...task,
      id: `018f0000-0000-7000-8000-${String(index + 1).padStart(12, "0")}`,
      title: `任务 ${index + 1}`,
    }));
    render(
      <BrowserRouter>
        <TasksPage />
      </BrowserRouter>,
    );
    fireEvent.click(screen.getByRole("checkbox", { name: "选择任务：任务 1" }));
    fireEvent.click(screen.getByLabelText("批量操作").querySelector("input")!);
    expect(
      screen.getByText("交给智能体每次最多 20 项，请减少选择。"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "交给智能体（已选任务）" }),
    ).not.toBeInTheDocument();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("maps a cross-column drop to a confirmed versioned lifecycle command", () => {
    render(<TasksPage />);
    fireEvent.click(screen.getByRole("button", { name: "看板视图" }));

    const transfer = {
      effectAllowed: "none",
      dropEffect: "none",
      setData: vi.fn(),
    };
    fireEvent.dragStart(screen.getByLabelText(`移动任务：${task.title}`), {
      dataTransfer: transfer,
    });
    const target = screen
      .getByRole("heading", { name: "进行中" })
      .closest("section");
    expect(target).not.toBeNull();
    fireEvent.dragEnter(target!, { dataTransfer: transfer });
    fireEvent.drop(target!, { dataTransfer: transfer });

    expect(
      screen.getByRole("dialog", { name: "确认任务状态变更" }),
    ).toBeVisible();
    expect(
      screen.getByText("将执行“开始执行”命令；", { exact: false }),
    ).toBeVisible();
    expect(mocks.lifecycle).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "确认变更" }));
    expect(mocks.lifecycle).toHaveBeenCalledWith(
      {
        id: task.id,
        input: { action: "start", expectedVersion: task.version },
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
    const callbacks = mocks.lifecycle.mock.calls[0][1] as {
      onError: (error: unknown) => void;
    };
    act(() =>
      callbacks.onError(
        new ApiError("任务已变化", {
          code: "VERSION_CONFLICT",
          status: 409,
        }),
      ),
    );
    expect(
      screen.getByText("任务已被其他操作更新，列表已刷新，请重新拖动。"),
    ).toBeVisible();
    expect(mocks.taskRefetch).not.toHaveBeenCalled();
  });

  it("keeps manual review acceptance out of board dragging", () => {
    mocks.taskItems = [
      { ...task, status: "waiting_review", reviewPolicy: "manual" },
    ];
    render(<TasksPage />);
    fireEvent.click(screen.getByRole("button", { name: "看板视图" }));

    const transfer = {
      effectAllowed: "none",
      dropEffect: "none",
      setData: vi.fn(),
    };
    fireEvent.dragStart(screen.getByLabelText(`移动任务：${task.title}`), {
      dataTransfer: transfer,
    });
    const target = screen
      .getByRole("heading", { name: "已完成" })
      .closest("section");
    fireEvent.dragEnter(target!, { dataTransfer: transfer });
    fireEvent.drop(target!, { dataTransfer: transfer });

    expect(
      screen.getByText(
        "待验收任务必须在详情中由人工执行接受或驳回，不能通过拖拽完成。",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("dialog", { name: "确认任务状态变更" }),
    ).not.toBeInTheDocument();
    expect(mocks.lifecycle).not.toHaveBeenCalled();
  });

  it("sends the selected task version with an atomic batch update", () => {
    render(<TasksPage />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "set_project",
        items: [{ id: task.id, expectedVersion: task.version }],
        projectId: null,
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("sets batch priority and converts a local due time to an absolute instant", () => {
    render(<TasksPage />);
    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.change(screen.getByLabelText("批量操作类型"), {
      target: { value: "set_priority" },
    });
    fireEvent.change(screen.getByLabelText("批量目标优先级"), {
      target: { value: "P0" },
    });
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(mocks.batch).toHaveBeenLastCalledWith(
      {
        action: "set_priority",
        items: [{ id: task.id, expectedVersion: task.version }],
        priority: "P0",
      },
      expect.any(Object),
    );
    fireEvent.change(screen.getByLabelText("批量操作类型"), {
      target: { value: "set_due_date" },
    });
    fireEvent.change(
      screen.getByLabelText("批量截止时间，按本机时区填写；留空表示清除"),
      { target: { value: "2026-09-25T16:30" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    expect(mocks.batch).toHaveBeenLastCalledWith(
      {
        action: "set_due_date",
        items: [{ id: task.id, expectedVersion: task.version }],
        dueDate: new Date("2026-09-25T16:30").toISOString(),
      },
      expect.any(Object),
    );
  });

  it("uses the shared project selector for an atomic batch move", () => {
    render(<TasksPage />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.change(screen.getByLabelText("批量目标项目"), {
      target: { value: "project-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "set_project",
        items: [{ id: task.id, expectedVersion: task.version }],
        projectId: "project-1",
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("requires an explicit second confirmation for batch lifecycle commands", () => {
    render(<TasksPage />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.change(screen.getByLabelText("批量操作类型"), {
      target: { value: "complete" },
    });
    fireEvent.click(screen.getByRole("button", { name: "应用" }));

    expect(mocks.batch).not.toHaveBeenCalled();
    expect(screen.getByRole("status")).toHaveTextContent(
      "将对 1 项执行“完成任务”，再次点击确认。",
    );

    fireEvent.click(screen.getByRole("button", { name: "确认完成任务" }));
    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "complete",
        items: [{ id: task.id, expectedVersion: task.version }],
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("includes the normalized reason in a confirmed batch block", () => {
    render(<TasksPage />);

    fireEvent.click(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    );
    fireEvent.change(screen.getByLabelText("批量操作类型"), {
      target: { value: "block" },
    });
    fireEvent.change(screen.getByLabelText("批量操作原因"), {
      target: { value: "  等待外部确认  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    fireEvent.click(screen.getByRole("button", { name: "确认阻塞任务" }));

    expect(mocks.batch).toHaveBeenCalledWith(
      {
        action: "block",
        items: [{ id: task.id, expectedVersion: task.version }],
        reason: "等待外部确认",
      },
      expect.any(Object),
    );
  });

  it("drops a stale selection when a selected task was deleted elsewhere", () => {
    render(<TasksPage />);

    const checkbox = screen.getByRole("checkbox", {
      name: `选择任务：${task.title}`,
    });
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: "应用" }));
    const callbacks = mocks.batch.mock.calls[0][1] as {
      onError: (error: unknown) => void;
    };
    act(() =>
      callbacks.onError(
        new ApiError("任务集合已变化", {
          code: "TASK_BATCH_SET_CHANGED",
        }),
      ),
    );

    expect(checkbox).not.toBeChecked();
    expect(screen.queryByLabelText("批量操作")).not.toBeInTheDocument();
    expect(mocks.taskRefetch).not.toHaveBeenCalled();
  });

  it("only enables persisted reordering for one exact plan date", () => {
    render(<TasksPage />);

    fireEvent.change(screen.getByLabelText("任务排序"), {
      target: { value: "manual_order" },
    });
    expect(
      screen.getByText("选择一个精确计划日期，并清除其他筛选后即可调整顺序。"),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-08-27" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: `上移任务：${task.title}` }),
    );

    expect(mocks.move).toHaveBeenCalledWith({
      taskId: task.id,
      plannedDate: task.plannedDate,
      direction: "up",
    });
  });

  it("sends valid plan and due date ranges and blocks inverted ranges", () => {
    render(<TasksPage />);
    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("计划日期从"), {
      target: { value: "2026-08-20" },
    });
    fireEvent.change(screen.getByLabelText("计划日期到"), {
      target: { value: "2026-08-25" },
    });
    fireEvent.change(screen.getByLabelText("截止日期从"), {
      target: { value: "2026-08-21" },
    });
    fireEvent.change(screen.getByLabelText("截止日期到"), {
      target: { value: "2026-08-30" },
    });

    expect(lastPageQuery()).toEqual(
      expect.objectContaining({
        plannedDate: undefined,
        plannedFrom: "2026-08-20",
        plannedTo: "2026-08-25",
        dueFrom: "2026-08-21",
        dueTo: "2026-08-30",
        rootOnly: false,
      }),
    );
    expect(lastPageQueryEnabled()).toBe(true);

    fireEvent.change(screen.getByLabelText("计划日期从"), {
      target: { value: "2026-08-28" },
    });
    expect(screen.getByRole("alert")).toHaveTextContent(
      "计划日期起点不能晚于终点",
    );
    expect(lastPageQueryEnabled()).toBe(false);
    expect(
      screen.queryByRole("button", { name: `查看任务：${task.title}` }),
    ).not.toBeInTheDocument();
  });

  it("applies a persisted saved view to the complete server query", () => {
    mocks.savedViews = [
      {
        id: "view-1",
        name: "客户验收",
        definition: {
          q: "交付",
          status: "waiting_review",
          priority: "P1",
          kind: "review",
          projectId: "project-1",
          clientId: "client-1",
          tagIds: [],
          plannedDate: "",
          plannedFrom: "2026-08-20",
          plannedTo: "2026-08-31",
          dueFrom: "2026-08-25",
          dueTo: "2026-09-05",
          sort: "-updated_at",
        },
        schemaVersion: 1,
        version: 2,
        createdAt: "2026-08-27T08:00:00Z",
        updatedAt: "2026-08-28T08:00:00Z",
      },
    ];
    render(<TasksPage />);
    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("已保存视图"), {
      target: { value: "view-1" },
    });

    expect(screen.getByLabelText("项目")).toHaveValue("project-1");
    expect(lastPageQuery()).toEqual(
      expect.objectContaining({
        page: 1,
        q: "交付",
        status: "waiting_review",
        priority: "P1",
        kind: "review",
        projectId: "project-1",
        clientId: "client-1",
        plannedFrom: "2026-08-20",
        plannedTo: "2026-08-31",
        dueFrom: "2026-08-25",
        dueTo: "2026-09-05",
        sort: "-updated_at",
        rootOnly: false,
      }),
    );
  });

  it("previews and persists pointer drag within an exact plan status group", () => {
    const target = {
      ...task,
      id: "task-2",
      title: "确认交付范围",
      version: 8,
    };
    mocks.taskItems = [task, target];
    render(<TasksPage />);
    fireEvent.change(screen.getByLabelText("任务排序"), {
      target: { value: "manual_order" },
    });
    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-08-27" },
    });
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      setData: vi.fn(),
    };
    fireEvent.dragStart(screen.getByTitle(`拖动排序：${task.title}`), {
      dataTransfer,
    });
    const targetRow = screen
      .getByRole("button", { name: `查看任务：${target.title}` })
      .closest("article");
    fireEvent.dragOver(targetRow!, { dataTransfer });
    fireEvent.drop(targetRow!, { dataTransfer });

    expect(mocks.drag).toHaveBeenCalledWith(
      { source: task, target, position: "after" },
      expect.objectContaining({ onSettled: expect.any(Function) }),
    );
    expect(
      screen
        .getAllByRole("button", {
          name: /查看任务：(整理交付清单|确认交付范围)/,
        })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["查看任务：确认交付范围", "查看任务：整理交付清单"]);

    const callbacks = mocks.drag.mock.calls[0][1] as {
      onSettled: () => void;
    };
    act(() => callbacks.onSettled());
    expect(
      screen
        .getAllByRole("button", {
          name: /查看任务：(整理交付清单|确认交付范围)/,
        })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["查看任务：整理交付清单", "查看任务：确认交付范围"]);
  });

  it("blocks writes while a previous task page is shown as placeholder data", () => {
    mocks.placeholder = true;
    render(<TasksPage />);

    expect(
      screen.getByRole("checkbox", { name: `选择任务：${task.title}` }),
    ).toBeDisabled();
  });

  it("offers all six status filters and collapses cancelled tasks by default", () => {
    mocks.taskStatus = "cancelled";
    render(<TasksPage />);

    fireEvent.click(screen.getByRole("button", { name: "筛选" }));
    const filter = screen.getByLabelText("状态");
    for (const value of [
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ]) {
      expect(filter.querySelector(`option[value="${value}"]`)).not.toBeNull();
    }
    expect(
      screen.queryByRole("button", { name: `查看任务：${task.title}` }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "已取消" }));
    expect(
      screen.getByRole("button", { name: `查看任务：${task.title}` }),
    ).toBeVisible();
  });
});
