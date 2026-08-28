import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
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
});
