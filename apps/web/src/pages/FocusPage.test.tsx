import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
      currentStreakDays: 0,
      longestStreakDays: 0,
    } as FocusReport,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  reportHook: vi.fn(),
  create: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  stop: vi.fn(),
  cancel: vi.fn(),
}));

function mutation(mutate: ReturnType<typeof vi.fn>) {
  return { mutate, isPending: false, isError: false, error: null };
}

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => mocks.focusQuery,
  useTaskOptionsQuery: () => mocks.taskQuery,
  useCreateFocusSession: () => mutation(mocks.create),
  usePauseFocusSession: () => mutation(mocks.pause),
  useResumeFocusSession: () => mutation(mocks.resume),
  useStopFocusSession: () => mutation(mocks.stop),
  useCancelFocusSession: () => mutation(mocks.cancel),
  useTodayStatsQuery: () => ({
    data: { focus: { sessions: 1, minutes: 25 } },
  }),
  useFocusSessionHistoryQuery: () => mocks.historyQuery,
  useFocusReportQuery: (...args: unknown[]) => {
    mocks.reportHook(...args);
    return mocks.reportQuery;
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.focusQuery.data = undefined;
  mocks.focusQuery.isPending = false;
  mocks.focusQuery.isError = false;
  mocks.taskQuery.data = [];
  mocks.taskQuery.isPending = false;
  mocks.taskQuery.isError = false;
  useFocusCycleStore.getState().resetCycle();
  useSettingsStore.getState().resetSettings();
  useUiStore.setState({ settingsOpen: false, settingsModule: "general" });
});

afterEach(cleanup);

describe("FocusPage", () => {
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
    expect(screen.getByText("最佳 09:00–10:00")).toBeInTheDocument();
    expect(
      screen.getByLabelText("09:00–10:00，50 分钟，2 个专注块"),
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
});
