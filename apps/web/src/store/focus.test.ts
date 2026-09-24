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

  it("binds rounds to a Session rather than a Task, and ignores old completion/reset", () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("task-1", 2, "任务", "a");
    expect(cycle.completeWork("task-1", settings, "任务", 1_000, "a")).toBe(
      true,
    );
    const afterCompletion = useFocusCycleStore.getState();
    cycle.beginWork("task-1", 99, "任务", "a", false);
    expect(cycle.completeWork("task-1", settings, "任务", 2_000, "a")).toBe(
      false,
    );
    expect(useFocusCycleStore.getState()).toBe(afterCompletion);

    cycle.beginWork("task-1", 3, "任务", "b", false);
    const newer = useFocusCycleStore.getState();
    cycle.resetCycle("a");
    expect(cycle.completeWork("task-1", settings, "任务", 3_000, "a")).toBe(
      false,
    );
    expect(useFocusCycleStore.getState()).toBe(newer);
    expect(newer).toMatchObject({
      sessionId: "b",
      phase: "work",
      completedCycles: 0,
      targetCycles: 3,
    });
  });

  it("preserves only explicit local next-round intent, not an independently observed start", () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork(null, 2, null, "a");
    cycle.completeWork(null, settings, null, 1_000, "a");
    cycle.finishBreak();
    cycle.beginWork(null, 9, null, "b");
    expect(useFocusCycleStore.getState()).toMatchObject({
      completedCycles: 1,
      targetCycles: 2,
    });
    cycle.completeWork(null, settings, null, 2_000, "b");
    cycle.beginWork(null, 4, null, "external", false);
    expect(useFocusCycleStore.getState()).toMatchObject({
      completedCycles: 0,
      targetCycles: 4,
    });
  });

  it("migrates v1 persisted rests without losing time or inventing a Session identity", async () => {
    const cycle = useFocusCycleStore.getState();
    cycle.beginWork("task-1", 2, "任务");
    cycle.completeWork("task-1", settings, "任务", 1_000);
    const options = useFocusCycleStore.persist.getOptions();
    const persisted = options.partialize!(useFocusCycleStore.getState());
    cycle.resetCycle();
    await options.storage!.setItem(options.name!, {
      state: persisted,
      version: 1,
    });
    await useFocusCycleStore.persist.rehydrate();
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "break",
      sessionId: null,
      completedCycles: 1,
      targetCycles: 2,
      breakEndsAtMs: 301_000,
    });
    const saved = await options.storage!.getItem(options.name!);
    expect(saved?.version).toBe(2);
    expect(saved?.state).not.toHaveProperty("revision");
    cycle.finishBreak();
    cycle.beginWork("task-1", 5, "任务", "next");
    expect(useFocusCycleStore.getState()).toMatchObject({
      sessionId: "next",
      completedCycles: 1,
      targetCycles: 2,
    });
  });

  it("binds legacy work without dropping its existing completed rounds", () => {
    useFocusCycleStore.setState({
      phase: "work",
      taskId: "task-1",
      sessionId: null,
      completedCycles: 2,
      targetCycles: 4,
    });
    useFocusCycleStore
      .getState()
      .beginWork("task-1", 9, "任务", "restored", false);
    expect(useFocusCycleStore.getState()).toMatchObject({
      phase: "work",
      sessionId: "restored",
      completedCycles: 2,
      targetCycles: 4,
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
