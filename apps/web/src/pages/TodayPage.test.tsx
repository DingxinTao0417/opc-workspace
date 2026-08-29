import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { ApiError } from "../api/client";
import { TodayPage } from "./TodayPage";

const mocks = vi.hoisted(() => ({
  inbox: vi.fn(),
  stats: vi.fn(),
  taskGroups: vi.fn(),
  riskTasks: vi.fn(),
  moveTask: vi.fn(),
  moveError: null as unknown,
  movePending: false,
  resetMove: vi.fn(),
  dragTask: vi.fn(),
  dragError: null as unknown,
  dragPending: false,
  dragData: undefined as
    { orderWarnings: string[]; plannedDateChanged: boolean } | undefined,
  resetDrag: vi.fn(),
  resetOrder: vi.fn(),
  resetResetOrder: vi.fn(),
  setNewTaskOpen: vi.fn(),
  planTask: vi.fn(),
  resetPlanTask: vi.fn(),
  lifecycleTask: vi.fn(),
  resetLifecycleTask: vi.fn(),
  lifecycleError: null as unknown,
  lifecyclePending: false,
  createFocus: vi.fn(),
  resetCreateFocus: vi.fn(),
  createFocusError: null as unknown,
  createFocusPending: false,
  beginFocus: vi.fn(),
  deleteTask: vi.fn(),
  resetDeleteTask: vi.fn(),
  deleteTaskError: null as unknown,
  deleteTaskPending: false,
  setTaskDetailId: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: mocks.inbox,
  useActiveFocusSessionQuery: () => ({
    data: { session: null },
    isFetching: false,
    isSuccess: true,
  }),
  useCreateFocusSession: () => ({
    error: mocks.createFocusError,
    isPending: mocks.createFocusPending,
    mutate: mocks.createFocus,
    reset: mocks.resetCreateFocus,
    variables: undefined,
  }),
  useTodayStatsQuery: mocks.stats,
  useTodayTaskGroupsQuery: mocks.taskGroups,
  useMoveTaskWithinPlan: () => ({
    error: mocks.moveError,
    isPending: mocks.movePending,
    mutate: mocks.moveTask,
    reset: mocks.resetMove,
    variables: undefined,
  }),
  useResetTaskOrder: () => ({
    error: null,
    isPending: false,
    mutate: mocks.resetOrder,
    reset: mocks.resetResetOrder,
  }),
  useMoveTaskAcrossPlans: () => ({
    data: mocks.dragData,
    error: mocks.dragError,
    isPending: mocks.dragPending,
    mutate: mocks.dragTask,
    reset: mocks.resetDrag,
  }),
  useTaskPageQuery: (
    input: { dueState?: "overdue" | "due_soon" },
    enabled = true,
  ) =>
    input.dueState
      ? mocks.riskTasks(input, enabled)
      : {
          data: undefined,
          isError: false,
          isFetching: false,
          isPending: false,
          isPlaceholderData: false,
          isSuccess: false,
          refetch: vi.fn(),
        },
  useSetTaskPlannedDate: () => ({
    error: null,
    isError: false,
    isPending: false,
    mutateAsync: mocks.planTask,
    reset: mocks.resetPlanTask,
  }),
  useTaskLifecycleCommand: () => ({
    error: mocks.lifecycleError,
    isPending: mocks.lifecyclePending,
    mutate: mocks.lifecycleTask,
    reset: mocks.resetLifecycleTask,
    variables: undefined,
  }),
  useDeleteTask: () => ({
    error: mocks.deleteTaskError,
    isPending: mocks.deleteTaskPending,
    mutate: mocks.deleteTask,
    reset: mocks.resetDeleteTask,
  }),
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: unknown) => unknown) =>
    selector({ focusMinutes: 25, cycles: 4 }),
}));

vi.mock("../store/focus", () => ({
  useFocusCycleStore: (selector: (state: unknown) => unknown) =>
    selector({ beginWork: mocks.beginFocus, phase: "idle" }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({
      setNewTaskOpen: mocks.setNewTaskOpen,
      setTaskDetailId: mocks.setTaskDetailId,
    }),
}));

function makeRiskTask(id: string, title: string) {
  return {
    id,
    title,
    description: "",
    kind: "work" as const,
    status: "todo" as const,
    reviewPolicy: "none" as const,
    priority: "P2" as const,
    projectId: null,
    projectName: null,
    parentTaskId: null,
    parentTaskTitle: null,
    completionCriteria: "",
    tags: [],
    dueDate: "2026-08-29T09:00:00Z",
    plannedDate: null,
    estimatedMinutes: 30,
    actualMinutes: 0,
    manualOrder: null,
    version: 1,
    subtaskTotal: 0,
    subtaskCompleted: 0,
    createdAt: "2026-08-28T00:00:00Z",
    updatedAt: "2026-08-28T00:00:00Z",
  };
}

function mockTodayShell(overdue = 2, dueSoon = 3) {
  mocks.taskGroups.mockReturnValue({
    data: { overdue: [], today: [], thisWeek: [], unscheduled: [] },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  });
  mocks.stats.mockReturnValue({
    data: {
      date: "2026-08-28",
      tasks: {
        total: overdue + dueSoon,
        completed: 0,
        remaining: overdue + dueSoon,
        overdue,
        dueSoon,
        estimatedMinutes: 60,
        actualMinutes: 0,
      },
      focus: { sessions: 0, seconds: 0, minutes: 0 },
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  });
  mocks.inbox.mockReturnValue({
    data: undefined,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  });
}

function riskQueryResult(
  items: ReturnType<typeof makeRiskTask>[],
  total = items.length,
) {
  return {
    data: { items, meta: { page: 1, pageSize: 20, total } },
    isError: false,
    isFetching: false,
    isPending: false,
    isPlaceholderData: false,
    isSuccess: true,
    refetch: vi.fn(),
  };
}

describe("TodayPage Inbox overview", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.moveError = null;
    mocks.movePending = false;
    mocks.dragError = null;
    mocks.dragPending = false;
    mocks.dragData = undefined;
    mocks.lifecycleError = null;
    mocks.lifecyclePending = false;
    mocks.createFocusError = null;
    mocks.createFocusPending = false;
    mocks.deleteTaskError = null;
    mocks.deleteTaskPending = false;
    mocks.riskTasks.mockReset();
    vi.useRealTimers();
  });

  it("shows derived Inbox counts with risk deep links", () => {
    mocks.taskGroups.mockReturnValue({
      data: { overdue: [], today: [], thisWeek: [], unscheduled: [] },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: {
        date: "2026-08-28",
        tasks: {
          total: 0,
          completed: 0,
          remaining: 0,
          overdue: 0,
          dueSoon: 0,
          estimatedMinutes: 0,
          actualMinutes: 0,
        },
        focus: { sessions: 0, seconds: 0, minutes: 0 },
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: {
        serverNow: "2026-08-28T10:00:00Z",
        pending: 8,
        unread: 5,
        tracking: 4,
        waitingReview: 2,
        blocked: 1,
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("5 未读")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /待处理 8/ })).toHaveAttribute(
      "href",
      "/inbox",
    );
    expect(screen.getByRole("link", { name: /跟进中 4/ })).toHaveAttribute(
      "href",
      "/inbox?risk=tracking",
    );
    expect(screen.getByRole("link", { name: /待验收 2/ })).toHaveAttribute(
      "href",
      "/inbox?risk=waiting_review",
    );
    expect(screen.getByRole("link", { name: /有阻塞 1/ })).toHaveAttribute(
      "href",
      "/inbox?risk=blocked",
    );
  });

  it("toggles the overdue and due-soon cards as exclusive server filters", () => {
    mockTodayShell(2, 3);
    mocks.riskTasks.mockImplementation(
      (input: { dueState: "overdue" | "due_soon" }) =>
        riskQueryResult([
          makeRiskTask(
            input.dueState,
            input.dueState === "overdue" ? "服务端逾期任务" : "服务端临期任务",
          ),
        ]),
    );

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    const overdue = screen.getByRole("button", { name: /项已逾期/ });
    const dueSoon = screen.getByRole("button", { name: /项临期/ });
    expect(overdue).toHaveAttribute("aria-pressed", "false");
    expect(dueSoon).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(overdue);
    expect(overdue).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("heading", { name: "逾期任务" })).toBeVisible();
    expect(screen.getByText("服务端逾期任务")).toBeVisible();
    expect(screen.queryByRole("heading", { name: "今天" })).toBeNull();
    expect(mocks.riskTasks).toHaveBeenLastCalledWith(
      {
        page: 1,
        pageSize: 20,
        status: "active",
        dueState: "overdue",
        sort: "due_date",
      },
      true,
    );

    fireEvent.click(dueSoon);
    expect(overdue).toHaveAttribute("aria-pressed", "false");
    expect(dueSoon).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("服务端临期任务")).toBeVisible();

    fireEvent.click(dueSoon);
    expect(dueSoon).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("heading", { name: "今天" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "临期任务" })).toBeNull();
  });

  it("keeps risk navigation and row writes locked while task order is saving", () => {
    mockTodayShell();
    mocks.riskTasks.mockReturnValue(
      riskQueryResult([makeRiskTask("locked-risk", "风险锁定任务")]),
    );
    const view = render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: /项已逾期/ }));
    expect(screen.getByRole("heading", { name: "逾期任务" })).toBeVisible();

    mocks.movePending = true;
    view.rerender(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("正在保存任务顺序…")).toBeVisible();
    const overdue = screen.getByRole("button", { name: /项已逾期/ });
    const dueSoon = screen.getByRole("button", { name: /项临期/ });
    expect(overdue).toBeDisabled();
    expect(dueSoon).toBeDisabled();
    expect(screen.getByRole("button", { name: "清除筛选" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "安排任务日期：风险锁定任务" }),
    ).toBeDisabled();

    fireEvent.click(dueSoon);
    expect(screen.getByRole("heading", { name: "逾期任务" })).toBeVisible();
  });

  it("shows a dedicated loading state for the selected risk", () => {
    mockTodayShell();
    mocks.riskTasks.mockReturnValue({
      data: undefined,
      isError: false,
      isFetching: true,
      isPending: true,
      isPlaceholderData: false,
      isSuccess: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: /项临期/ }));

    expect(screen.getByRole("status", { name: "正在加载" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "今天" })).toBeNull();
  });

  it("isolates a risk query error and retries only that query", () => {
    mockTodayShell();
    const refetch = vi.fn();
    mocks.riskTasks.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: false,
      refetch,
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: /项已逾期/ }));

    expect(screen.getByText("无法读取逾期任务。")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(refetch).toHaveBeenCalledTimes(1);
    expect(mocks.taskGroups).toHaveBeenCalledTimes(2);
  });

  it("shows a truthful empty risk result", () => {
    mockTodayShell();
    mocks.riskTasks.mockReturnValue(riskQueryResult([], 0));

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: /项临期/ }));

    expect(screen.getByRole("heading", { name: "没有临期任务" })).toBeVisible();
    expect(screen.getByText("当前没有活动的临期任务。")).toBeVisible();
    expect(screen.getByText("共 0 项")).toBeVisible();
  });

  it("paginates the complete server risk result without enabling reorder", () => {
    mockTodayShell();
    mocks.riskTasks.mockImplementation((input: { page: number }) => {
      const items =
        input.page === 1
          ? Array.from({ length: 20 }, (_, index) =>
              makeRiskTask(`risk-${index + 1}`, `风险任务 ${index + 1}`),
            )
          : Array.from({ length: 5 }, (_, index) =>
              makeRiskTask(`risk-${index + 21}`, `风险任务 ${index + 21}`),
            );
      return {
        ...riskQueryResult(items, 25),
        data: { items, meta: { page: input.page, pageSize: 20, total: 25 } },
      };
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: /项已逾期/ }));

    expect(screen.getByText("共 25 项")).toBeVisible();
    expect(screen.getByText("1–20 / 25")).toBeVisible();
    expect(screen.queryByTitle(/拖动排序/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(mocks.riskTasks).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2, dueState: "overdue" }),
      true,
    );
    expect(screen.getByText("21–25 / 25")).toBeVisible();
    expect(screen.getByText("风险任务 25")).toBeVisible();

    fireEvent.click(
      screen.getByRole("button", { name: "编辑任务：风险任务 21" }),
    );
    expect(mocks.setTaskDetailId).toHaveBeenCalledWith("risk-21");
  });

  it("clamps a stale risk page after the server total shrinks", async () => {
    mockTodayShell();
    let shrunk = false;
    mocks.riskTasks.mockImplementation((input: { page: number }) => {
      if (shrunk) {
        const items =
          input.page === 1 ? [makeRiskTask("remaining", "剩余风险任务")] : [];
        return {
          ...riskQueryResult(items, 1),
          data: { items, meta: { page: input.page, pageSize: 20, total: 1 } },
        };
      }
      const items =
        input.page === 1
          ? Array.from({ length: 20 }, (_, index) =>
              makeRiskTask(`before-${index}`, `收缩前任务 ${index + 1}`),
            )
          : [makeRiskTask("last-page", "末页风险任务")];
      return {
        ...riskQueryResult(items, 21),
        data: { items, meta: { page: input.page, pageSize: 20, total: 21 } },
      };
    });
    const view = render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: /项已逾期/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("末页风险任务")).toBeVisible();

    shrunk = true;
    view.rerender(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(mocks.riskTasks).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1, dueState: "overdue" }),
        true,
      );
    });
    expect(screen.getByText("剩余风险任务")).toBeVisible();
    expect(screen.getByText("共 1 项")).toBeVisible();
  });

  it("renders complete planned-date groups instead of arbitrary slices", () => {
    const makeTask = (id: string, title: string) => ({
      id,
      title,
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: null,
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 1,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    });
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [makeTask("overdue", "逾期任务")],
        today: [makeTask("today", "今日任务")],
        thisWeek: [makeTask("week", "本周任务")],
        unscheduled: [makeTask("unscheduled", "未排期任务")],
      },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "逾期计划" })).toBeVisible();
    expect(screen.getByText("逾期任务")).toBeVisible();
    expect(screen.getByText("今日任务")).toBeVisible();
    expect(screen.getByText("本周任务")).toBeVisible();
    expect(screen.getByRole("heading", { name: "未排期" })).toBeVisible();
    expect(screen.getByText("未排期任务")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "安排任务日期：逾期任务" }),
    ).toBeVisible();
  });

  it("runs versioned lifecycle shortcuts and starts a bound focus session", () => {
    const makeTask = (
      id: string,
      title: string,
      status: "todo" | "in_progress",
      reviewPolicy: "none" | "manual" = "none",
    ) => ({
      id,
      title,
      description: "",
      kind: "work",
      status,
      reviewPolicy,
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: "2026-08-28",
      estimatedMinutes: 25,
      actualMinutes: 0,
      manualOrder: null,
      version: id === "todo" ? 3 : 7,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    });
    const todo = makeTask("todo", "准备交付", "todo");
    const progress = makeTask("progress", "完成交付", "in_progress");
    const manual = makeTask(
      "manual",
      "等待验收的工作",
      "in_progress",
      "manual",
    );
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [],
        today: [todo, progress, manual],
        thisWeek: [],
        unscheduled: [],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "开始执行任务：准备交付" }),
    );
    expect(mocks.lifecycleTask).toHaveBeenCalledWith({
      id: "todo",
      input: { action: "start", expectedVersion: 3 },
    });
    fireEvent.click(screen.getByRole("button", { name: "完成任务：完成交付" }));
    expect(mocks.lifecycleTask).toHaveBeenCalledWith({
      id: "progress",
      input: { action: "complete", expectedVersion: 7 },
    });
    expect(
      screen.queryByRole("button", { name: "完成任务：等待验收的工作" }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "开始专注：准备交付" }));
    expect(mocks.createFocus).toHaveBeenCalledWith(
      { taskId: "todo", plannedSeconds: 1_500 },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    const focusCallbacks = mocks.createFocus.mock.calls.at(-1)?.[1] as {
      onSuccess: () => void;
    };
    focusCallbacks.onSuccess();
    expect(mocks.beginFocus).toHaveBeenCalledWith("todo", 4, "准备交付");
  });

  it("switches the queried date and can return to today", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 10, 0, 0));
    mocks.taskGroups.mockReturnValue({
      data: { overdue: [], today: [], thisWeek: [], unscheduled: [] },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(mocks.taskGroups).toHaveBeenLastCalledWith("2026-08-28");
    fireEvent.click(screen.getByRole("button", { name: "前一天" }));
    expect(mocks.taskGroups).toHaveBeenLastCalledWith("2026-08-27");
    expect(screen.getByRole("button", { name: "回到今天" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "回到今天" }));
    expect(mocks.taskGroups).toHaveBeenLastCalledWith("2026-08-28");
    expect(
      screen.queryByRole("button", { name: "回到今天" }),
    ).not.toBeInTheDocument();
  });

  it("persists active ordering for the exact selected date and unscheduled group", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 10, 0, 0));
    const makeTask = (
      id: string,
      title: string,
      plannedDate: string | null,
    ) => ({
      id,
      title,
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate,
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 1,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    });
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [],
        today: [
          makeTask("today-1", "今日第一项", "2026-08-28"),
          makeTask("today-2", "今日第二项", "2026-08-28"),
        ],
        thisWeek: [],
        unscheduled: [makeTask("unscheduled", "未排期任务", null)],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "下移任务：今日第一项" }),
    );
    expect(mocks.moveTask).toHaveBeenCalledWith({
      taskId: "today-1",
      plannedDate: "2026-08-28",
      direction: "down",
      scope: "active",
    });

    const todaySection = screen
      .getByRole("heading", { name: "今天" })
      .closest("section");
    const unscheduledSection = screen
      .getByRole("heading", { name: "未排期" })
      .closest("section");
    expect(todaySection).not.toBeNull();
    expect(unscheduledSection).not.toBeNull();
    fireEvent.click(
      within(todaySection!).getByRole("button", { name: "恢复默认顺序" }),
    );
    fireEvent.click(
      within(unscheduledSection!).getByRole("button", {
        name: "恢复默认顺序",
      }),
    );
    expect(mocks.resetOrder).toHaveBeenNthCalledWith(1, "2026-08-28");
    expect(mocks.resetOrder).toHaveBeenNthCalledWith(2, null);
  });

  it("keeps the current order visible and exposes a reorder failure", () => {
    mocks.moveError = new ApiError("计划组已变化，请重新操作", {
      code: "TASK_REORDER_SET_CHANGED",
    });
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [],
        today: [],
        thisWeek: [],
        unscheduled: [],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "计划组已变化，请重新操作",
    );
  });

  it("optimistically reorders a dragged exact-date group and clears it after settling", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 10, 0, 0));
    const first = {
      id: "drag-1",
      title: "拖拽第一项",
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: "2026-08-28",
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 1,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    };
    const second = { ...first, id: "drag-2", title: "拖拽第二项", version: 2 };
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [],
        today: [first, second],
        thisWeek: [],
        unscheduled: [],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      setData: vi.fn(),
    };
    const source = screen.getByTitle("拖动排序：拖拽第一项");
    const target = screen
      .getByRole("button", { name: "查看任务：拖拽第二项" })
      .closest("article");

    fireEvent.dragStart(source, { dataTransfer });
    fireEvent.dragOver(target!, { dataTransfer });
    fireEvent.drop(target!, { dataTransfer });

    expect(mocks.dragTask).toHaveBeenCalledWith(
      {
        source: first,
        target: second,
        targetPlannedDate: second.plannedDate,
        position: "after",
      },
      expect.objectContaining({ onSettled: expect.any(Function) }),
    );
    expect(
      screen
        .getAllByRole("button", { name: /查看任务：拖拽/ })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["查看任务：拖拽第二项", "查看任务：拖拽第一项"]);

    const callbacks = mocks.dragTask.mock.calls[0][1] as {
      onSettled: () => void;
    };
    act(() => callbacks.onSettled());
    expect(
      screen
        .getAllByRole("button", { name: /查看任务：拖拽/ })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["查看任务：拖拽第一项", "查看任务：拖拽第二项"]);
  });

  it("shares drag state across visible date groups and previews the move", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 10, 0, 0));
    const overdue = {
      id: "cross-overdue",
      title: "跨组来源",
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: "2026-08-27",
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 3,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-27T00:00:00Z",
      updatedAt: "2026-08-27T00:00:00Z",
    };
    const today = {
      ...overdue,
      id: "cross-today",
      title: "跨组目标",
      plannedDate: "2026-08-28",
      version: 5,
    };
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [overdue],
        today: [today],
        thisWeek: [],
        unscheduled: [],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      setData: vi.fn(),
    };
    const target = screen
      .getByRole("button", { name: "查看任务：跨组目标" })
      .closest("article");

    fireEvent.dragStart(screen.getByTitle("拖动排序：跨组来源"), {
      dataTransfer,
    });
    fireEvent.dragOver(target!, { dataTransfer });
    fireEvent.drop(target!, { dataTransfer });

    expect(mocks.dragTask).toHaveBeenCalledWith(
      {
        source: overdue,
        target: today,
        targetPlannedDate: today.plannedDate,
        position: "before",
      },
      expect.objectContaining({ onSettled: expect.any(Function) }),
    );
    const todaySection = screen
      .getByRole("heading", { name: "今天" })
      .closest("section");
    expect(todaySection).not.toBeNull();
    expect(
      within(todaySection!)
        .getAllByRole("button", { name: /查看任务：跨组/ })
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["查看任务：跨组来源", "查看任务：跨组目标"]);
    expect(
      screen.queryByRole("heading", { name: "逾期计划" }),
    ).not.toBeInTheDocument();
  });

  it("reports confirmed replanning separately from order-save warnings", () => {
    mocks.dragData = {
      plannedDateChanged: true,
      orderWarnings: ["目标日期顺序未能保存"],
    };
    mocks.taskGroups.mockReturnValue({
      data: { overdue: [], today: [], thisWeek: [], unscheduled: [] },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status")).toHaveTextContent(
      "任务已改期；目标日期顺序未能保存",
    );
  });

  it("accepts a dragged task on an empty explicit Today group", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 28, 10, 0, 0));
    const overdue = {
      id: "empty-target-source",
      title: "移入空日期",
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: "2026-08-27",
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 2,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-27T00:00:00Z",
      updatedAt: "2026-08-27T00:00:00Z",
    };
    mocks.taskGroups.mockReturnValue({
      data: {
        overdue: [overdue],
        today: [],
        thisWeek: [],
        unscheduled: [],
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      setData: vi.fn(),
    };
    fireEvent.dragStart(screen.getByTitle("拖动排序：移入空日期"), {
      dataTransfer,
    });
    const todaySection = screen
      .getByRole("heading", { name: "今天" })
      .closest("section");

    fireEvent.dragOver(todaySection!, { dataTransfer });
    fireEvent.drop(todaySection!, { dataTransfer });

    expect(mocks.dragTask).toHaveBeenCalledWith(
      {
        source: overdue,
        target: null,
        targetPlannedDate: "2026-08-28",
        position: "end",
      },
      expect.objectContaining({ onSettled: expect.any(Function) }),
    );
  });

  it("opens editing directly and requires confirmation before row deletion", () => {
    const task = {
      id: "today-row-actions",
      title: "核对今日交付",
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: "2026-08-28",
      estimatedMinutes: 30,
      actualMinutes: 0,
      manualOrder: null,
      version: 7,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    };
    mocks.taskGroups.mockReturnValue({
      data: { overdue: [], today: [task], thisWeek: [], unscheduled: [] },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    mocks.inbox.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "编辑任务：核对今日交付" }),
    );
    expect(mocks.setTaskDetailId).toHaveBeenCalledWith(task.id);

    fireEvent.click(
      screen.getByRole("button", { name: "删除任务：核对今日交付" }),
    );
    expect(screen.getByRole("dialog", { name: "删除任务" })).toBeVisible();
    expect(mocks.deleteTask).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    expect(mocks.deleteTask).toHaveBeenCalledWith(
      { id: task.id, expectedVersion: task.version },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("explains active Inbox relation protection during direct deletion", () => {
    const task = {
      id: "protected-task",
      title: "仍有关联的任务",
      description: "",
      kind: "work",
      status: "todo",
      reviewPolicy: "none",
      priority: "P2",
      projectId: null,
      projectName: null,
      parentTaskId: null,
      parentTaskTitle: null,
      completionCriteria: "",
      tags: [],
      dueDate: null,
      plannedDate: null,
      estimatedMinutes: null,
      actualMinutes: 0,
      manualOrder: null,
      version: 3,
      subtaskTotal: 0,
      subtaskCompleted: 0,
      createdAt: "2026-08-28T00:00:00Z",
      updatedAt: "2026-08-28T00:00:00Z",
    };
    mocks.deleteTaskError = new ApiError("Task has active relations", {
      code: "TASK_HAS_ACTIVE_INBOX_RELATIONS",
      status: 409,
    });
    mocks.taskGroups.mockReturnValue({
      data: { overdue: [], today: [], thisWeek: [], unscheduled: [task] },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    mocks.stats.mockReturnValue({ data: undefined, isPending: false });
    mocks.inbox.mockReturnValue({ data: undefined });

    render(
      <MemoryRouter>
        <TodayPage />
      </MemoryRouter>,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "删除任务：仍有关联的任务" }),
    );

    expect(screen.getByRole("alert")).toHaveTextContent("请先在收件箱解除关联");
  });
});
