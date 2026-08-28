import { beforeEach, describe, expect, it } from "vitest";
import type { FocusSessionSnapshot } from "../types/models";
import type { FocusSettings } from "./settings";
import {
  deriveBreakClock,
  deriveFocusClock,
  formatFocusTime,
  useFocusCycleStore,
} from "./focus";

const settings: FocusSettings = {
  focusMinutes: 25,
  breakMinutes: 5,
  cycles: 2,
  autoStartBreak: true,
  autoStartFocus: false,
  soundEnabled: true,
};

function snapshot(
  status: "active" | "paused" | "recovery_pending",
): FocusSessionSnapshot {
  return {
    serverNow: "2026-08-28T10:00:00Z",
    receivedAtMs: 10_000,
    session: {
      id: "018f0000-0000-7000-8000-000000000901",
      taskId: null,
      taskTitle: null,
      status,
      plannedSeconds: 300,
      accumulatedSeconds: 30,
      startedAt: "2026-08-28T09:59:00Z",
      endedAt: null,
      lastResumedAt: status === "paused" ? null : "2026-08-28T09:59:50Z",
      lastHeartbeatAt: "2026-08-28T09:59:58Z",
      endReason: null,
      version: 2,
      createdAt: "2026-08-28T09:59:00Z",
      updatedAt: "2026-08-28T10:00:00Z",
    },
  };
}

describe("focus clock", () => {
  it("derives active time from the server clock instead of local ticks", () => {
    expect(deriveFocusClock(snapshot("active"), 15_000)).toMatchObject({
      elapsedSeconds: 45,
      remainingSeconds: 255,
      progress: 0.15,
    });
  });

  it("keeps paused facts fixed and separates confirmed recovery time from the gap", () => {
    expect(deriveFocusClock(snapshot("paused"), 99_000).elapsedSeconds).toBe(
      30,
    );
    expect(
      deriveFocusClock(snapshot("recovery_pending"), 15_000),
    ).toMatchObject({
      elapsedSeconds: 38,
      remainingSeconds: 262,
      uncertainSeconds: 7,
    });
  });

  it("caps display time at the planned duration", () => {
    const value = snapshot("active");
    value.session!.lastResumedAt = "2026-08-28T08:00:00Z";
    expect(deriveFocusClock(value, 15_000)).toMatchObject({
      elapsedSeconds: 300,
      remainingSeconds: 0,
      progress: 1,
    });
  });
});

describe("focus cycle coordinator", () => {
  beforeEach(() => {
    useFocusCycleStore.getState().resetCycle();
  });

  it("keeps work sessions separate from an absolute-time break", () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("task-1", 2, "第一项任务");
    useFocusCycleStore
      .getState()
      .completeWork("task-1", settings, "第一项任务", 1_000);

    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "break",
      taskId: "task-1",
      taskTitle: "第一项任务",
      completedCycles: 1,
      targetCycles: 2,
      breakRemainingSeconds: 300,
      breakEndsAtMs: 301_000,
    });
    expect(
      deriveBreakClock(useFocusCycleStore.getState(), 61_000),
    ).toMatchObject({ remainingSeconds: 240, elapsedSeconds: 60 });
  });

  it("pauses, resumes and skips a break without creating work time", () => {
    useFocusCycleStore.getState().beginWork(null, 2);
    useFocusCycleStore.getState().completeWork(null, settings, null, 1_000);
    useFocusCycleStore.getState().pauseBreak(61_000);
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "break",
      breakRemainingSeconds: 240,
      breakEndsAtMs: null,
    });

    useFocusCycleStore.getState().resumeBreak(70_000);
    expect(useFocusCycleStore.getState().breakEndsAtMs).toBe(310_000);
    useFocusCycleStore.getState().finishBreak();
    expect(useFocusCycleStore.getState().phase).toBe("ready");
  });

  it("continues the same sequence and marks its final cycle complete", () => {
    useFocusCycleStore.getState().beginWork("task-1", 2, "任务");
    useFocusCycleStore
      .getState()
      .completeWork("task-1", settings, "任务", 1_000);
    useFocusCycleStore.getState().finishBreak();
    useFocusCycleStore.getState().beginWork("task-1", 9, "任务");
    useFocusCycleStore
      .getState()
      .completeWork("task-1", settings, "任务", 2_000);

    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "complete",
      completedCycles: 2,
      targetCycles: 2,
    });
  });

  it("waits for an explicit break start when auto-start is disabled", () => {
    useFocusCycleStore.getState().beginWork(null, 2);
    useFocusCycleStore
      .getState()
      .completeWork(null, { ...settings, autoStartBreak: false }, null, 1_000);
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "break",
      breakRemainingSeconds: 300,
      breakEndsAtMs: null,
    });
  });
});

describe("formatFocusTime", () => {
  it("formats whole, fractional, negative and invalid seconds", () => {
    expect(formatFocusTime(2052)).toBe("34:12");
    expect(formatFocusTime(61.9)).toBe("01:01");
    expect(formatFocusTime(-1)).toBe("00:00");
    expect(formatFocusTime(Number.NaN)).toBe("00:00");
  });
});
