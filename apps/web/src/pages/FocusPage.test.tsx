import {
  act,
  cleanup,
  fireEvent,
  render as renderTesting,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
import { Link, MemoryRouter, useLocation, useNavigate } from "react-router-dom";
import { focusReportLocationHref } from "../lib/focusReportLocation";
import { useAiChatStore } from "../store/aiChat";
import { ApiError } from "../api/client";
import type {
  FocusReport,
  FocusSessionListResult,
  FocusSessionSnapshot,
} from "../types/models";
import { useFocusCycleStore } from "../store/focus";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  DEFAULT_THEME,
  useSettingsStore,
} from "../store/settings";
import { useUiStore } from "../store/ui";
import { FocusPage } from "./FocusPage";

const mocks = vi.hoisted(() => ({
  focusQuery: {
    data: undefined as FocusSessionSnapshot | undefined,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  taskQuery: {
    data: [] as Array<{ id: string; title: string; status: string }>,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  historyQuery: {
    data: {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0 },
    } as FocusSessionListResult,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  reportQuery: {
    error: null as unknown,
    data: {
      dateFrom: "2026-08-22",
      dateTo: "2026-08-28",
      timezone: "UTC",
      totals: { sessions: 0, seconds: 0, minutes: 0 },
      days: Array.from({ length: 7 }, (_, index) => ({
        date: `2026-08-${String(22 + index).padStart(2, "0")}`,
        sessions: 0,
        seconds: 0,
        minutes: 0,
      })),
      projects: [],
      hours: Array.from({ length: 24 }, (_, hour) => ({
        hour,
        sessions: 0,
        seconds: 0,
        minutes: 0,
      })),
      heatmap: Array.from({ length: 7 * 24 }, (_, index) => ({
        weekday: Math.floor(index / 24) + 1,
        hour: index % 24,
        sessions: 0,
        seconds: 0,
        minutes: 0,
      })),
      tags: [],
      currentStreakDays: 0,
      longestStreakDays: 0,
    } as FocusReport,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  reportHook: vi.fn(),
  todayStatsHook: vi.fn(),
  create: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  stop: vi.fn(),
  cancel: vi.fn(),
}));

const sourceSession = "018f0000-0000-7000-8000-000000005834";
const sourceProject = "018f0000-0000-7000-8000-000000005833";
const linkedReportHref = focusReportLocationHref({
  dateFrom: "2026-11-01",
  dateTo: "2026-11-02",
  timezone: "America/Los_Angeles",
  projectId: sourceProject,
  view: "heatmap",
});
const initialReport = structuredClone(mocks.reportQuery.data);
function LocationProbe() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output aria-label="当前位置">
        {location.pathname}
        {location.search}
      </output>
      <button onClick={() => navigate(-1)}>后退</button>
      <Link to={linkedReportHref.replace("2026-11-02", "2026-11-03")}>
        另一报告
      </Link>
    </>
  );
}
function render(node: ReactElement, route = "/focus") {
  return renderTesting(
    <MemoryRouter initialEntries={[route]}>
      {node}
      <LocationProbe />
    </MemoryRouter>,
  );
}

function mutation(mutate: ReturnType<typeof vi.fn>) {
  return { mutate, isPending: false, isError: false, error: null };
}

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => mocks.focusQuery,
  useCreateFocusSession: () => mutation(mocks.create),
  usePauseFocusSession: () => mutation(mocks.pause),
  useResumeFocusSession: () => mutation(mocks.resume),
  useStopFocusSession: () => mutation(mocks.stop),
  useCancelFocusSession: () => mutation(mocks.cancel),
  useTodayStatsQuery: (dateKey: string) => {
    mocks.todayStatsHook(dateKey);
    return { data: { focus: { sessions: 1, minutes: 25 } } };
  },
  useFocusSessionHistoryQuery: () => mocks.historyQuery,
  useFocusReportQuery: (...args: unknown[]) => {
    mocks.reportHook(...args);
    return mocks.reportQuery;
  },
}));

vi.mock("../components/TaskSelect", () => ({
  TaskSelect: ({
    ariaLabel,
    disabled,
    emptyLabel,
    inputId,
    onChange,
    value,
  }: {
    ariaLabel: string;
    disabled?: boolean;
    emptyLabel: string;
    inputId?: string;
    onChange: (
      value: string,
      task: { id: string; title: string; status: string } | null,
    ) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      disabled={disabled}
      id={inputId}
      onChange={(event) => {
        const selected = mocks.taskQuery.data.find(
          (task) => task.id === event.target.value,
        );
        onChange(event.target.value, selected ?? null);
      }}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      {mocks.taskQuery.data
        .filter((task) => task.status !== "cancelled")
        .map((task) => (
          <option key={task.id} value={task.id}>
            {task.title}
          </option>
        ))}
    </select>
  ),
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.focusQuery.data = undefined;
  mocks.focusQuery.isPending = false;
  mocks.focusQuery.isError = false;
  mocks.taskQuery.data = [];
  mocks.taskQuery.isPending = false;
  mocks.taskQuery.isError = false;
  mocks.reportQuery.data = structuredClone(initialReport);
  mocks.reportQuery.isPending = false;
  mocks.reportQuery.isError = false;
  mocks.reportQuery.error = null;
  useAiChatStore.setState({ activeSessionId: "", streaming: null });
  useFocusCycleStore.getState().resetCycle();
  useSettingsStore.getState().resetSettings();
  useUiStore.setState({ settingsOpen: false, settingsModule: "general" });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("FocusPage", () => {
  it("opens the exact report and returns to its source conversation without focus commands", () => {
    const view = render(
      <FocusPage />,
      `${linkedReportHref}&return_session=${sourceSession}`,
    );
    expect(mocks.reportHook).toHaveBeenCalledWith(
      expect.objectContaining({
        dateFrom: "2026-11-01",
        dateTo: "2026-11-02",
        timezone: "America/Los_Angeles",
        projectId: sourceProject,
      }),
      true,
    );
    expect(
      mocks.reportHook.mock.calls.every(
        ([input]) => input.dateFrom === "2026-11-01",
      ),
    ).toBe(true);
    expect(screen.getByText(/America\/Los_Angeles/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "指定项目" })).toHaveAttribute(
      "href",
      `/projects/${sourceProject}`,
    );
    expect(screen.getByText(/不是对话时的冻结快照/)).toBeInTheDocument();
    expect(screen.getByLabelText("专注报告")).toHaveFocus(); // Empty data locates the report, not a nonexistent heatmap.
    useAiChatStore.setState({ activeSessionId: "another-session" });
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sourceSession);
    expect(screen.getByLabelText("当前位置")).toHaveTextContent("/ai");
    for (const command of [
      mocks.create,
      mocks.pause,
      mocks.resume,
      mocks.stop,
      mocks.cancel,
    ])
      expect(command).not.toHaveBeenCalled();
    expect(useFocusCycleStore.getState().phase).toBe("idle");
    view.unmount();
  });

  it("locates the requested dimension after data loads and does not steal date-input focus", () => {
    mocks.reportQuery.isPending = true;
    const view = render(<FocusPage />, linkedReportHref);
    mocks.reportQuery.isPending = false;
    mocks.reportQuery.data.totals = { sessions: 1, seconds: 60, minutes: 1 };
    // Same mounted route, as when the asynchronous query resolves.
    view.rerender(
      <MemoryRouter initialEntries={[linkedReportHref]}>
        <FocusPage />
        <LocationProbe />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("周几与小时专注热力图")).toHaveFocus();
    const end = screen.getByLabelText("专注回顾结束日期");
    end.focus();
    fireEvent.change(end, { target: { value: "2026-11-03" } });
    expect(end).toHaveFocus();
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateTo: "2026-11-03",
        projectId: sourceProject,
        timezone: "America/Los_Angeles",
      }),
      true,
    );
  });

  it("follows another report and browser back without showing a default-window query", () => {
    render(<FocusPage />, linkedReportHref);
    fireEvent.click(screen.getByRole("link", { name: "另一报告" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({ dateTo: "2026-11-03" }),
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "后退" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({ dateTo: "2026-11-02" }),
      true,
    );
  });

  it("retains linked zone/project for presets even when that zone is on a different day", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-19T01:00:00Z"));
    render(<FocusPage />, linkedReportHref);
    fireEvent.click(screen.getByRole("button", { name: "7 天" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-09-12",
        dateTo: "2026-09-18",
        timezone: "America/Los_Angeles",
        projectId: sourceProject,
      }),
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "清除链接筛选" }));
    expect(mocks.reportHook.mock.lastCall![0].projectId).toBeUndefined();
    expect(screen.queryByText(/按链接条件查看/)).not.toBeInTheDocument();
  });

  it.each([
    "/focus?report=days",
    `${linkedReportHref}&timezone=UTC`,
    linkedReportHref.replace("2026-11-02", "2026-02-30"),
  ])("rejects invalid report navigation %s", (route) => {
    render(<FocusPage />, route);
    expect(screen.getByText("报告链接无效")).toBeInTheDocument();
    expect(
      mocks.reportHook.mock.calls.every(([, enabled]) => enabled === false),
    ).toBe(true);
    expect(
      screen.queryByText(/还没有已完成的专注记录/),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "清除链接筛选" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(expect.any(Object), true);
  });

  it("keeps the report independent of active-session errors and hides unavailable project data", () => {
    mocks.focusQuery.isError = true;
    mocks.reportQuery.isError = true;
    mocks.reportQuery.error = new ApiError("missing", {
      code: "PROJECT_NOT_FOUND",
      status: 404,
    });
    render(
      <FocusPage />,
      `${linkedReportHref}&return_session=https://evil.invalid`,
    );
    expect(screen.getByText(/链接中的项目已不存在/)).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "返回原对话" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/还没有已完成的专注记录/),
    ).not.toBeInTheDocument();
  });

  it("requires a second confirmation before starting without a task", () => {
    render(<FocusPage />);

    fireEvent.click(screen.getByRole("button", { name: "开始专注" }));
    expect(mocks.create).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "确认不绑定任务并开始" }),
    );
    expect(mocks.create).toHaveBeenCalledWith(
      { taskId: null, plannedSeconds: 3000 },
      expect.any(Object),
    );
  });

  it("uses committed duration for creation while rendering draft preview", () => {
    useSettingsStore.getState().beginPreview();
    useSettingsStore.getState().setPreview({
      focus: { ...DEFAULT_FOCUS_SETTINGS, focusMinutes: 55 },
      general: DEFAULT_GENERAL_SETTINGS,
      profile: DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
    });
    mocks.taskQuery.data = [
      { id: "task-1", title: "整理交付", status: "todo" },
    ];
    render(<FocusPage />);

    fireEvent.change(screen.getByLabelText("关联任务"), {
      target: { value: "task-1" },
    });
    expect(screen.getByText(/当前预览 55 分钟/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "开始专注" }));
    expect(mocks.create).toHaveBeenCalledWith(
      { taskId: "task-1", plannedSeconds: 3000 },
      expect.any(Object),
    );
  });

  it("opens the settings modal directly on the focus module", () => {
    render(<FocusPage />);
    fireEvent.click(screen.getByRole("button", { name: "专注设置" }));
    expect(useUiStore.getState()).toMatchObject({
      settingsOpen: true,
      settingsModule: "focus",
    });
  });

  it("renders a local break without treating it as task work", () => {
    useFocusCycleStore.getState().beginWork("task-1", 2, "整理交付");
    useFocusCycleStore
      .getState()
      .completeWork(
        "task-1",
        { ...DEFAULT_FOCUS_SETTINGS, autoStartBreak: false },
        "整理交付",
        1_000,
      );
    render(<FocusPage />);

    expect(screen.getByText("休息阶段 · 不计入工时")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "开始休息" })).toBeEnabled();
  });

  it("renders weekly facts and terminal session history", () => {
    mocks.reportQuery.data = {
      ...mocks.reportQuery.data,
      totals: { sessions: 3, seconds: 4500, minutes: 75 },
      projects: [
        {
          projectId: "project-1",
          projectName: "客户门户",
          sessions: 2,
          seconds: 3000,
          minutes: 50,
        },
        {
          projectId: null,
          projectName: null,
          sessions: 1,
          seconds: 1500,
          minutes: 25,
        },
      ],
      hours: Array.from({ length: 24 }, (_, hour) => ({
        hour,
        sessions: hour === 9 ? 2 : 0,
        seconds: hour === 9 ? 3000 : 0,
        minutes: hour === 9 ? 50 : 0,
      })),
      heatmap: Array.from({ length: 7 * 24 }, (_, index) => ({
        weekday: Math.floor(index / 24) + 1,
        hour: index % 24,
        sessions: index === 8 ? 2 : 0,
        seconds: index === 8 ? 3000 : 0,
        minutes: index === 8 ? 50 : 0,
      })),
      tags: [
        {
          tagId: "tag-1",
          tagName: "深度工作",
          tagColor: "#6C5CE7",
          sessions: 2,
          seconds: 2700,
          minutes: 45,
        },
        {
          tagId: null,
          tagName: null,
          tagColor: null,
          sessions: 1,
          seconds: 1500,
          minutes: 25,
        },
      ],
      currentStreakDays: 2,
      longestStreakDays: 4,
    };
    mocks.historyQuery.data = {
      items: [
        {
          id: "focus-1",
          taskId: null,
          taskTitle: "整理交付",
          status: "completed" as const,
          plannedSeconds: 3000,
          accumulatedSeconds: 1500,
          startedAt: "2026-08-28T10:00:00Z",
          endedAt: "2026-08-28T10:25:00Z",
          lastResumedAt: null,
          lastHeartbeatAt: null,
          endReason: "completed" as const,
          version: 2,
          createdAt: "2026-08-28T10:00:00Z",
          updatedAt: "2026-08-28T10:25:00Z",
        },
      ],
      meta: { page: 1, pageSize: 6, total: 1 },
    };
    render(<FocusPage />);

    expect(screen.getByText("75 分钟")).toBeInTheDocument();
    expect(screen.getByText("整理交付")).toBeInTheDocument();
    expect(screen.getByText("25:00")).toBeInTheDocument();
    expect(screen.getByText("4 天")).toBeInTheDocument();
    expect(screen.getByText("客户门户")).toBeInTheDocument();
    expect(screen.getByText("未归项目")).toBeInTheDocument();
    expect(screen.getByText("2 个专注块 · 50 分钟")).toBeInTheDocument();
    expect(screen.getByText("深度工作")).toBeInTheDocument();
    expect(screen.getByText("未加标签")).toBeInTheDocument();
    expect(screen.getByText("多标签任务会分别计入各标签")).toBeInTheDocument();
    expect(screen.getByText("最佳 09:00–10:00")).toBeInTheDocument();
    expect(
      screen.getByLabelText("09:00–10:00，50 分钟，2 个专注块"),
    ).toBeInTheDocument();
    expect(
      screen.getByLabelText("周一 08:00–09:00，50 分钟，2 个专注块"),
    ).toBeInTheDocument();
  });

  it("switches local report ranges without changing active focus state", () => {
    render(<FocusPage />);

    fireEvent.click(screen.getByRole("button", { name: "30 天" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
        dateTo: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      }),
      true,
    );
    expect(screen.getByText("最近 30 天")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "本月" }));
    expect(screen.getByRole("button", { name: "本月" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(useFocusCycleStore.getState().phase).toBe("idle");
  });

  it("keeps an invalid custom date range local until it is valid", () => {
    render(<FocusPage />);
    fireEvent.click(screen.getByRole("button", { name: "自定义" }));
    fireEvent.change(screen.getByLabelText("专注回顾结束日期"), {
      target: { value: "2026-01-01" },
    });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "结束日期不能早于开始日期。",
    );
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.any(Object),
      false,
    );
  });

  it("refreshes today and preset ranges at midnight without changing a custom range", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 31, 23, 59, 59));
    render(<FocusPage />);

    expect(mocks.todayStatsHook).toHaveBeenLastCalledWith("2026-08-31");
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-25",
        dateTo: "2026-08-31",
      }),
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: "自定义" }));
    fireEvent.change(screen.getByLabelText("专注回顾开始日期"), {
      target: { value: "2026-08-20" },
    });
    fireEvent.change(screen.getByLabelText("专注回顾结束日期"), {
      target: { value: "2026-08-25" },
    });
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-20",
        dateTo: "2026-08-25",
      }),
      true,
    );

    act(() => vi.advanceTimersByTime(1_002));

    expect(mocks.todayStatsHook).toHaveBeenLastCalledWith("2026-09-01");
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-20",
        dateTo: "2026-08-25",
      }),
      true,
    );

    fireEvent.click(screen.getByRole("button", { name: "7 天" }));
    expect(mocks.reportHook).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-26",
        dateTo: "2026-09-01",
      }),
      true,
    );
  });
});
