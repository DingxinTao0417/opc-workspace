import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { TodayPage } from "./TodayPage";

const mocks = vi.hoisted(() => ({
  inbox: vi.fn(),
  stats: vi.fn(),
  taskGroups: vi.fn(),
  setNewTaskOpen: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: mocks.inbox,
  useTodayStatsQuery: mocks.stats,
  useTodayTaskGroupsQuery: mocks.taskGroups,
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
});
