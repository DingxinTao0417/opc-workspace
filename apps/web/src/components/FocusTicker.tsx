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
  const stopFocus = useStopFocusSession();
  const clock = useFocusClock(focusQuery.data);
  const breakClock = useBreakClock();
  const focusMinutes = useSettingsStore((state) => state.focusMinutes);
  const breakMinutes = useSettingsStore((state) => state.breakMinutes);
  const cycles = useSettingsStore((state) => state.cycles);
  const autoStartBreak = useSettingsStore((state) => state.autoStartBreak);
  const autoStartFocus = useSettingsStore((state) => state.autoStartFocus);
  const soundEnabled = useSettingsStore((state) => state.soundEnabled);
  const cyclePhase = useFocusCycleStore((state) => state.phase);
  const cycleTaskId = useFocusCycleStore((state) => state.taskId);
  const cycleTaskTitle = useFocusCycleStore((state) => state.taskTitle);
  const completedCycles = useFocusCycleStore((state) => state.completedCycles);
  const targetCycles = useFocusCycleStore((state) => state.targetCycles);
  const beginWork = useFocusCycleStore((state) => state.beginWork);
  const completeWork = useFocusCycleStore((state) => state.completeWork);
  const finishBreak = useFocusCycleStore((state) => state.finishBreak);
  const autoStopAttempt = useRef<string | null>(null);
  const breakCompletionAttempt = useRef<number | null>(null);
  const session = focusQuery.data?.session;

  useEffect(() => {
    if (
      session?.status === "active" &&
      (cyclePhase === "idle" || cyclePhase === "ready")
    ) {
      beginWork(session.taskId, cycles, session.taskTitle);
    }
  }, [beginWork, cyclePhase, cycles, session]);

  useEffect(() => {
    if (!session || session.status !== "active" || clock.remainingSeconds > 0) {
      autoStopAttempt.current = null;
      return;
    }

    const attempt = `${session.id}:${session.version}`;
    if (autoStopAttempt.current === attempt) return;
    autoStopAttempt.current = attempt;

    void stopFocus
      .mutateAsync({ id: session.id, expectedVersion: session.version })
      .then(() => {
        completeWork(
          session.taskId,
          {
            focusMinutes,
            breakMinutes,
            cycles,
            autoStartBreak,
            autoStartFocus,
            soundEnabled,
          },
          session.taskTitle,
        );
        if (soundEnabled) playPhaseCompleteSound();
      })
      .catch(() => {
        if (autoStopAttempt.current === attempt) {
          autoStopAttempt.current = null;
        }
      });
  }, [
    clock.remainingSeconds,
    autoStartBreak,
    autoStartFocus,
    breakMinutes,
    completeWork,
    cycles,
    focusMinutes,
    session,
    soundEnabled,
    stopFocus.mutateAsync,
  ]);

  useEffect(() => {
    if (
      cyclePhase !== "break" ||
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
      .then(() => beginWork(cycleTaskId, targetCycles, cycleTaskTitle))
      .catch(() => undefined);
  }, [
    autoStartFocus,
    beginWork,
    breakClock.remainingSeconds,
    completedCycles,
    createFocus.mutateAsync,
    cyclePhase,
    cycleTaskId,
    cycleTaskTitle,
    finishBreak,
    focusMinutes,
    soundEnabled,
    targetCycles,
  ]);

  return null;
}
