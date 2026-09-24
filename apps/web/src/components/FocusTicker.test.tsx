import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
import { useFocusCycleStore } from "../store/focus";
import { DEFAULT_FOCUS_SETTINGS, useSettingsStore } from "../store/settings";
import { FocusTicker } from "./FocusTicker";

const mocks = vi.hoisted(() => ({
  queryData: undefined as FocusSessionSnapshot | undefined,
  stop: vi.fn(),
  create: vi.fn(),
  completeCycle: false,
  isFetching: false,
  isError: false,
}));

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => ({
    data: mocks.queryData,
    isSuccess: !!mocks.queryData && !mocks.isError,
    isFetching: mocks.isFetching,
  }),
  useStopFocusSession: (completeCycle: boolean) => {
    mocks.completeCycle = completeCycle;
    return { mutateAsync: mocks.stop };
  },
  useCreateFocusSession: () => ({ mutateAsync: mocks.create }),
}));

function activeAtLimit(): FocusSessionSnapshot {
  return {
    serverNow: "2026-08-28T10:05:00Z",
    receivedAtMs: Date.now(),
    session: {
      id: "018f0000-0000-7000-8000-000000000901",
      taskId: "task-1",
      taskTitle: "整理交付",
      status: "active",
      plannedSeconds: 300,
      accumulatedSeconds: 300,
      startedAt: "2026-08-28T10:00:00Z",
      endedAt: null,
      lastResumedAt: "2026-08-28T10:00:00Z",
      lastHeartbeatAt: "2026-08-28T10:05:00Z",
      endReason: null,
      version: 1,
      createdAt: "2026-08-28T10:00:00Z",
      updatedAt: "2026-08-28T10:05:00Z",
    },
  };
}

beforeEach(() => {
  vi.resetAllMocks();
  mocks.queryData = undefined;
  mocks.isFetching = false;
  mocks.isError = false;
  mocks.stop.mockResolvedValue({ ...activeAtLimit(), session: null });
  mocks.create.mockResolvedValue(activeAtLimit());
  useSettingsStore.getState().resetSettings();
  useFocusCycleStore.getState().resetCycle();
});

afterEach(cleanup);

describe("FocusTicker", () => {
  it("does not apply an old auto-stop to a newer session on the same task", async () => {
    let finish!: (value: FocusSessionSnapshot) => void;
    mocks.stop.mockImplementationOnce(
      () =>
        new Promise<FocusSessionSnapshot>((resolve) => {
          finish = resolve;
        }),
    );
    mocks.queryData = activeAtLimit();
    const view = render(<FocusTicker />);
    await waitFor(() => expect(finish).toBeTypeOf("function"));
    const newer = activeAtLimit();
    newer.session!.id = "018f0000-0000-7000-8000-000000000902";
    newer.session!.plannedSeconds = 3000;
    mocks.queryData = newer;
    view.rerender(<FocusTicker />);
    const terminal = activeAtLimit();
    terminal.session!.status = "completed";
    terminal.session!.version = 2;
    await act(async () => finish(terminal));
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      completedCycles: 0,
    });
  });

  it("does not auto-start a local break while another session is paused", async () => {
    useSettingsStore.setState({ autoStartFocus: true });
    useFocusCycleStore.getState().beginWork("old-task", 2);
    useFocusCycleStore
      .getState()
      .completeWork(
        "old-task",
        DEFAULT_FOCUS_SETTINGS,
        null,
        Date.now() - DEFAULT_FOCUS_SETTINGS.breakMinutes * 60_000 - 1_000,
      );
    mocks.queryData = activeAtLimit();
    mocks.queryData.session!.status = "paused";
    render(<FocusTicker />);
    await act(async () => {});
    expect(mocks.create).not.toHaveBeenCalled();
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      taskId: "task-1",
    });
  });
  it("requests auto-stop once with cycle completion owned by the shared hook", async () => {
    mocks.queryData = activeAtLimit();
    render(<FocusTicker />);

    await waitFor(() => expect(mocks.stop).toHaveBeenCalledTimes(1));
    expect(mocks.completeCycle).toBe(true);
    expect(mocks.stop).toHaveBeenCalledWith({
      id: activeAtLimit().session!.id,
      expectedVersion: 1,
    });
  });

  it("creates the next work session after an absolute break when enabled", async () => {
    useSettingsStore.setState({ autoStartFocus: true });
    mocks.queryData = { ...activeAtLimit(), session: null };
    useFocusCycleStore.getState().beginWork("task-1", 2, "整理交付");
    useFocusCycleStore
      .getState()
      .completeWork(
        "task-1",
        { ...DEFAULT_FOCUS_SETTINGS, autoStartFocus: true },
        "整理交付",
        Date.now() - DEFAULT_FOCUS_SETTINGS.breakMinutes * 60_000 - 1_000,
      );

    render(<FocusTicker />);
    await waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith({
        taskId: "task-1",
        plannedSeconds: DEFAULT_FOCUS_SETTINGS.focusMinutes * 60,
      }),
    );
    expect(useFocusCycleStore.getState().phase).toBe("ready");
  });

  it.each(["loading", "fetching", "error"])(
    "waits for authoritative emptiness during %s before auto-starting",
    async (mode) => {
      useSettingsStore.setState({ autoStartFocus: true });
      useFocusCycleStore.getState().beginWork("task-1", 2);
      useFocusCycleStore
        .getState()
        .completeWork("task-1", DEFAULT_FOCUS_SETTINGS, null, 1);
      if (mode !== "loading")
        mocks.queryData = { ...activeAtLimit(), session: null };
      mocks.isFetching = mode === "fetching";
      mocks.isError = mode === "error";
      render(<FocusTicker />);
      await act(async () => {});
      expect(mocks.create).not.toHaveBeenCalled();
      expect(useFocusCycleStore.getState().phase).toBe("break");
    },
  );
});
