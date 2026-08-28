import {
  cleanup,
  fireEvent,
  render,
  screen,
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
  moveTask: vi.fn(),
  moveError: null as unknown,
  movePending: false,
  resetMove: vi.fn(),
  resetOrder: vi.fn(),
  resetResetOrder: vi.fn(),
  setNewTaskOpen: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: mocks.inbox,
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
  useTaskPageQuery: () => ({
    data: undefined,
    isFetching: false,
    isPlaceholderData: false,
    isSuccess: false,
  }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({ setNewTaskOpen: mocks.setNewTaskOpen }),
}));

describe("TodayPage Inbox overview", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.moveError = null;
    mocks.movePending = false;
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
});
