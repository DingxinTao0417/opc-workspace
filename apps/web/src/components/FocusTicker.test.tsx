import { render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
import { useFocusCycleStore } from "../store/focus";
import { DEFAULT_FOCUS_SETTINGS, useSettingsStore } from "../store/settings";
import { FocusTicker } from "./FocusTicker";

const mocks = vi.hoisted(() => ({
  queryData: undefined as FocusSessionSnapshot | undefined,
  stop: vi.fn(),
  create: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => ({ data: mocks.queryData }),
  useStopFocusSession: () => ({ mutateAsync: mocks.stop }),
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
  vi.clearAllMocks();
  mocks.queryData = undefined;
  mocks.stop.mockResolvedValue({ ...activeAtLimit(), session: null });
  mocks.create.mockResolvedValue(activeAtLimit());
  useSettingsStore.getState().resetSettings();
  useFocusCycleStore.getState().resetCycle();
});

describe("FocusTicker", () => {
  it("stops a completed server session once and enters a non-work break", async () => {
    mocks.queryData = activeAtLimit();
    render(<FocusTicker />);

    await waitFor(() => expect(mocks.stop).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(useFocusCycleStore.getState()).toMatchObject({
        phase: "break",
        taskId: "task-1",
        taskTitle: "整理交付",
        completedCycles: 1,
        targetCycles: DEFAULT_FOCUS_SETTINGS.cycles,
      }),
    );
  });

  it("creates the next work session after an absolute break when enabled", async () => {
    useSettingsStore.setState({ autoStartFocus: true });
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
    await waitFor(() =>
      expect(useFocusCycleStore.getState().phase).toBe("work"),
    );
  });
});
