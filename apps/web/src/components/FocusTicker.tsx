import { useEffect, useRef } from "react";
import {
  useActiveFocusSessionQuery,
  useCreateFocusSession,
  useStopFocusSession,
} from "../api/hooks";
import {
  useBreakClock,
  useFocusClock,
  useFocusCycleStore,
} from "../store/focus";
import { useSettingsStore } from "../store/settings";

function playPhaseCompleteSound() {
  try {
    const AudioContextConstructor =
      window.AudioContext ??
      (
        window as typeof window & {
          webkitAudioContext?: typeof AudioContext;
        }
      ).webkitAudioContext;

    if (!AudioContextConstructor) return;
    const context = new AudioContextConstructor();
    const oscillator = context.createOscillator();
    const gain = context.createGain();

    oscillator.frequency.value = 660;
    gain.gain.setValueAtTime(0.08, context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.14);
    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start();
    oscillator.stop(context.currentTime + 0.14);
    oscillator.addEventListener("ended", () => {
      void context.close().catch(() => undefined);
    });
  } catch {
    // Audio may be unavailable or blocked until the user interacts with the app.
  }
}

export function FocusTicker() {
  const focusQuery = useActiveFocusSessionQuery();
  const createFocus = useCreateFocusSession();
  const stopFocus = useStopFocusSession(true);
  const clock = useFocusClock(focusQuery.data);
  const breakClock = useBreakClock();
  const focusMinutes = useSettingsStore((state) => state.focusMinutes);
  const cycles = useSettingsStore((state) => state.cycles);
  const autoStartFocus = useSettingsStore((state) => state.autoStartFocus);
  const soundEnabled = useSettingsStore((state) => state.soundEnabled);
  const cyclePhase = useFocusCycleStore((state) => state.phase);
  const cycleTaskId = useFocusCycleStore((state) => state.taskId);
  const completedCycles = useFocusCycleStore((state) => state.completedCycles);
  const targetCycles = useFocusCycleStore((state) => state.targetCycles);
  const beginWork = useFocusCycleStore((state) => state.beginWork);
  const resetCycle = useFocusCycleStore((state) => state.resetCycle);
  const finishBreak = useFocusCycleStore((state) => state.finishBreak);
  const autoStopAttempt = useRef<string | null>(null);
  const breakCompletionAttempt = useRef<number | null>(null);
  const session = focusQuery.data?.session;

  useEffect(() => {
    if (
      session &&
      ["active", "paused", "recovery_pending"].includes(session.status)
    ) {
      // The command hook handles local next-round intent. An independently
      // observed Session is a new sequence, even when it targets the same Task.
      beginWork(session.taskId, cycles, session.taskTitle, session.id, false);
    } else if (
      focusQuery.isSuccess &&
      !focusQuery.isFetching &&
      !stopFocus.isPending &&
      cyclePhase === "work"
    ) {
      resetCycle(useFocusCycleStore.getState().sessionId ?? undefined);
    }
  }, [
    beginWork,
    cyclePhase,
    cycles,
    focusQuery.isFetching,
    focusQuery.isSuccess,
    resetCycle,
    session,
    stopFocus.isPending,
  ]);

  useEffect(() => {
    if (!session || session.status !== "active" || clock.remainingSeconds > 0) {
      autoStopAttempt.current = null;
      return;
    }

    const attempt = `${session.id}:${session.version}`;
    if (autoStopAttempt.current === attempt) return;
    const cycle = useFocusCycleStore.getState();
    if (cycle.phase !== "work" || cycle.sessionId !== session.id) return;
    autoStopAttempt.current = attempt;

    void stopFocus
      .mutateAsync({ id: session.id, expectedVersion: session.version })
      .then(() => {
        const current = useFocusCycleStore.getState();
        if (
          soundEnabled &&
          current.sessionId === session.id &&
          (current.phase === "break" || current.phase === "complete")
        )
          playPhaseCompleteSound();
      })
      .catch(() => {
        if (autoStopAttempt.current === attempt) {
          autoStopAttempt.current = null;
        }
      });
  }, [clock.remainingSeconds, session, soundEnabled, stopFocus.mutateAsync]);

  useEffect(() => {
    if (
      cyclePhase !== "break" ||
      useFocusCycleStore.getState().phase !== "break" ||
      !focusQuery.isSuccess ||
      focusQuery.isFetching ||
      session ||
      breakClock.remainingSeconds > 0 ||
      breakCompletionAttempt.current === completedCycles
    ) {
      if (cyclePhase !== "break") breakCompletionAttempt.current = null;
      return;
    }

    breakCompletionAttempt.current = completedCycles;
    finishBreak();
    if (soundEnabled) playPhaseCompleteSound();

    if (!autoStartFocus || completedCycles >= targetCycles) return;
    void createFocus
      .mutateAsync({
        taskId: cycleTaskId,
        plannedSeconds: focusMinutes * 60,
      })
      .catch(() => undefined);
  }, [
    autoStartFocus,
    breakClock.remainingSeconds,
    completedCycles,
    createFocus.mutateAsync,
    cyclePhase,
    cycleTaskId,
    finishBreak,
    focusMinutes,
    focusQuery.isSuccess,
    focusQuery.isFetching,
    session,
    soundEnabled,
    targetCycles,
  ]);

  return null;
}
