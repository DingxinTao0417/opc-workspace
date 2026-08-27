import { beforeEach, describe, expect, it } from "vitest";
import { formatFocusTime, type FocusConfig, useFocusStore } from "./focus";

const config: FocusConfig = {
  focusMinutes: 25,
  breakMinutes: 5,
  cycles: 2,
  autoStartBreak: false,
  autoStartFocus: false,
  soundEnabled: true,
};

describe("focus store", () => {
  beforeEach(() => {
    useFocusStore.getState().setDurationsOrSync(config);
  });

  it("starts, pauses, toggles and resets the current session", () => {
    useFocusStore.getState().start();
    expect(useFocusStore.getState().running).toBe(true);

    useFocusStore.getState().tick();
    expect(useFocusStore.getState().remainingSeconds).toBe(1499);

    useFocusStore.getState().pause();
    expect(useFocusStore.getState().running).toBe(false);

    useFocusStore.getState().toggle();
    expect(useFocusStore.getState().running).toBe(true);

    useFocusStore.getState().reset();
    expect(useFocusStore.getState()).toMatchObject({
      phase: "focus",
      remainingSeconds: 1500,
      running: false,
      completedCycles: 0,
      completed: false,
    });
  });

  it("moves from focus to a paused break and counts a completed cycle", () => {
    useFocusStore.setState({ remainingSeconds: 1, running: true });
    useFocusStore.getState().tick();

    expect(useFocusStore.getState()).toMatchObject({
      phase: "break",
      durationMinutes: 5,
      remainingSeconds: 300,
      running: false,
      completedCycles: 1,
      completed: false,
    });
  });

  it("honors automatic starts between focus and break phases", () => {
    useFocusStore.getState().setDurationsOrSync({
      ...config,
      autoStartBreak: true,
      autoStartFocus: true,
    });
    useFocusStore.setState({ remainingSeconds: 1, running: true });

    useFocusStore.getState().tick();
    expect(useFocusStore.getState()).toMatchObject({
      phase: "break",
      running: true,
      completedCycles: 1,
    });

    useFocusStore.setState({ remainingSeconds: 1 });
    useFocusStore.getState().tick();
    expect(useFocusStore.getState()).toMatchObject({
      phase: "focus",
      remainingSeconds: 1500,
      running: true,
      completedCycles: 1,
    });
  });

  it("stops and marks the sequence complete after the configured cycles", () => {
    useFocusStore.setState({
      remainingSeconds: 1,
      running: true,
      completedCycles: 1,
    });
    useFocusStore.getState().tick();

    expect(useFocusStore.getState()).toMatchObject({
      phase: "focus",
      durationMinutes: 25,
      remainingSeconds: 1500,
      running: false,
      completedCycles: 2,
      completed: true,
    });
  });

  it("pauses and resets when settings are synchronized", () => {
    useFocusStore.setState({
      phase: "break",
      remainingSeconds: 9,
      running: true,
      completedCycles: 1,
    });
    useFocusStore.getState().setDurationsOrSync({
      ...config,
      focusMinutes: 45,
      breakMinutes: 10,
      cycles: 3,
      soundEnabled: false,
    });

    expect(useFocusStore.getState()).toMatchObject({
      phase: "focus",
      durationMinutes: 45,
      remainingSeconds: 2700,
      running: false,
      completedCycles: 0,
      completed: false,
      breakMinutes: 10,
      cycles: 3,
      soundEnabled: false,
    });
  });

  it("normalizes direct configuration updates to supported bounds", () => {
    useFocusStore.getState().setDurationsOrSync({
      ...config,
      focusMinutes: 2,
      breakMinutes: 99,
      cycles: 20,
    });

    expect(useFocusStore.getState()).toMatchObject({
      focusMinutes: 5,
      breakMinutes: 30,
      cycles: 8,
      durationMinutes: 5,
      remainingSeconds: 300,
    });
  });

  it("does not tick while paused", () => {
    useFocusStore.getState().tick();
    expect(useFocusStore.getState().remainingSeconds).toBe(1500);
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
