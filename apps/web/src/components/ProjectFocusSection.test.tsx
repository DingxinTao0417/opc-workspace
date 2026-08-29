import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusReport, FocusSession } from "../types/models";
import { ProjectFocusSection } from "./ProjectFocusSection";

const hookMocks = vi.hoisted(() => ({
  history: vi.fn(),
  historyRefetch: vi.fn(),
  report: vi.fn(),
  reportRefetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useFocusReportQuery: hookMocks.report,
  useFocusSessionHistoryQuery: hookMocks.history,
}));

const report: FocusReport = {
  dateFrom: "2026-08-22",
  dateTo: "2026-08-28",
  timezone: "UTC",
  totals: { sessions: 3, seconds: 4500, minutes: 75 },
  days: [
    { date: "2026-08-27", sessions: 1, seconds: 1500, minutes: 25 },
    { date: "2026-08-28", sessions: 2, seconds: 3000, minutes: 50 },
  ],
  projects: [],
  hours: [],
  heatmap: [],
  tags: [],
  currentStreakDays: 2,
  longestStreakDays: 4,
};

const session: FocusSession = {
  id: "session-1",
  taskId: "task-1",
  taskTitle: "整理项目交付清单",
  status: "completed",
  plannedSeconds: 1800,
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

function reportResult(overrides: Record<string, unknown> = {}) {
  return {
    data: report,
    error: null,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: hookMocks.reportRefetch,
    ...overrides,
  };
}

function historyResult(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      items: [session],
      meta: { page: 1, pageSize: 6, total: 8 },
    },
    error: null,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: hookMocks.historyRefetch,
    ...overrides,
  };
}

describe("ProjectFocusSection", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-28T12:00:00Z"));
    hookMocks.report.mockReturnValue(reportResult());
    hookMocks.history.mockReturnValue(historyResult());
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("scopes the report and terminal history to the current project", () => {
    render(<ProjectFocusSection projectId="project-1" />);

    expect(hookMocks.report).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-22",
        dateTo: "2026-08-28",
        projectId: "project-1",
        timezone: expect.any(String),
      }),
    );
    expect(hookMocks.history).toHaveBeenLastCalledWith({
      page: 1,
      pageSize: 6,
      status: "terminal",
      projectId: "project-1",
    });
    expect(screen.getByText("1 小时 15 分钟")).toBeVisible();
    expect(screen.getByText("整理项目交付清单")).toBeVisible();
    expect(screen.getByText("记录 25:00")).toBeVisible();
    expect(screen.getByLabelText("项目每日专注趋势")).toBeVisible();
    expect(screen.getByText(/“当前项目归属”统计/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "30 天" }));
    expect(hookMocks.report).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-07-30",
        dateTo: "2026-08-28",
        projectId: "project-1",
      }),
    );

    fireEvent.click(
      screen.getByRole("button", { name: "下一页项目 Session 历史" }),
    );
    expect(hookMocks.history).toHaveBeenLastCalledWith({
      page: 2,
      pageSize: 6,
      status: "terminal",
      projectId: "project-1",
    });
  });

  it("keeps a retryable report failure isolated from session history", () => {
    hookMocks.report.mockReturnValue(
      reportResult({
        data: undefined,
        error: new Error("offline"),
        isError: true,
      }),
    );
    render(<ProjectFocusSection projectId="project-1" />);

    expect(
      screen.getByText("项目专注统计暂时不可用；项目其他内容不受影响。"),
    ).toBeVisible();
    expect(screen.getByText("整理项目交付清单")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(hookMocks.reportRefetch).toHaveBeenCalledOnce();
  });

  it("returns to a valid history page when the project result shrinks", () => {
    let shrunk = false;
    hookMocks.history.mockImplementation((input: { page: number }) =>
      input.page === 2 && shrunk
        ? historyResult({
            data: { items: [], meta: { page: 2, pageSize: 6, total: 2 } },
          })
        : historyResult(),
    );
    const view = render(<ProjectFocusSection projectId="project-1" />);

    fireEvent.click(
      screen.getByRole("button", { name: "下一页项目 Session 历史" }),
    );
    expect(hookMocks.history).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2, projectId: "project-1" }),
    );

    act(() => {
      shrunk = true;
      view.rerender(<ProjectFocusSection projectId="project-1" />);
    });
    expect(hookMocks.history).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, projectId: "project-1" }),
    );
  });

  it("keeps a retryable history failure isolated from the report", () => {
    hookMocks.history.mockReturnValue(
      historyResult({
        data: undefined,
        error: new Error("offline"),
        isError: true,
      }),
    );
    render(<ProjectFocusSection projectId="project-1" />);

    expect(
      screen.getByText("项目 Session 历史暂时不可用；项目其他内容不受影响。"),
    ).toBeVisible();
    expect(screen.getByText("1 小时 15 分钟")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(hookMocks.historyRefetch).toHaveBeenCalledOnce();
  });

  it("renders report and history loading states independently", () => {
    hookMocks.report.mockReturnValue(
      reportResult({ data: undefined, isFetching: true, isPending: true }),
    );
    hookMocks.history.mockReturnValue(
      historyResult({ data: undefined, isFetching: true, isPending: true }),
    );
    render(<ProjectFocusSection projectId="project-1" />);

    expect(screen.getByText("正在统计项目专注记录…")).toBeVisible();
    expect(screen.getByText("正在读取项目 Session 历史…")).toBeVisible();
  });

  it("renders distinct empty states without inventing records", () => {
    hookMocks.report.mockReturnValue(
      reportResult({
        data: {
          ...report,
          totals: { sessions: 0, seconds: 0, minutes: 0 },
          days: report.days.map((day) => ({
            ...day,
            sessions: 0,
            seconds: 0,
            minutes: 0,
          })),
          currentStreakDays: 0,
          longestStreakDays: 0,
        },
      }),
    );
    hookMocks.history.mockReturnValue(
      historyResult({
        data: { items: [], meta: { page: 1, pageSize: 6, total: 0 } },
      }),
    );
    render(<ProjectFocusSection projectId="project-1" />);

    expect(screen.getByText("最近 7 天还没有已完成的专注记录。")).toBeVisible();
    expect(
      screen.getByText("当前项目归属下还没有终态 Session。"),
    ).toBeVisible();
  });
});
