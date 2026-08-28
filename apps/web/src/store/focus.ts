import { useEffect, useState } from "react";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { FocusSessionSnapshot } from "../types/models";
import type { FocusSettings } from "./settings";

export interface FocusClock {
  elapsedSeconds: number;
  remainingSeconds: number;
  progress: number;
  uncertainSeconds: number;
}

export type FocusCyclePhase = "idle" | "work" | "break" | "ready" | "complete";

interface FocusCycleState {
  phase: FocusCyclePhase;
  taskId: string | null;
  taskTitle: string | null;
  completedCycles: number;
  targetCycles: number;
  breakDurationSeconds: number;
  breakRemainingSeconds: number;
  breakEndsAtMs: number | null;
  beginWork: (
    taskId: string | null,
    targetCycles: number,
    taskTitle?: string | null,
  ) => void;
  completeWork: (
    taskId: string | null,
    settings: FocusSettings,
    taskTitle?: string | null,
    nowMs?: number,
  ) => void;
  pauseBreak: (nowMs?: number) => void;
  resumeBreak: (nowMs?: number) => void;
  finishBreak: () => void;
  resetCycle: () => void;
}

const cycleMemoryStorage = new Map<string, string>();
const cycleFallbackStorage: Storage = {
  get length() {
    return cycleMemoryStorage.size;
  },
  clear: () => cycleMemoryStorage.clear(),
  getItem: (key) => cycleMemoryStorage.get(key) ?? null,
  key: (index) => [...cycleMemoryStorage.keys()][index] ?? null,
  removeItem: (key) => cycleMemoryStorage.delete(key),
  setItem: (key, value) => cycleMemoryStorage.set(key, value),
};

function focusCycleStorage(): Storage {
  try {
    return window.localStorage ?? cycleFallbackStorage;
  } catch {
    return cycleFallbackStorage;
  }
}

const initialCycleState = {
  phase: "idle" as FocusCyclePhase,
  taskId: null,
  taskTitle: null,
  completedCycles: 0,
  targetCycles: 1,
  breakDurationSeconds: 0,
  breakRemainingSeconds: 0,
  breakEndsAtMs: null,
};

function breakSecondsAt(
  state: Pick<
    FocusCycleState,
    "phase" | "breakEndsAtMs" | "breakRemainingSeconds"
  >,
  nowMs: number,
): number {
  if (state.phase !== "break") return 0;
  if (state.breakEndsAtMs === null) return state.breakRemainingSeconds;
  return Math.max(0, Math.ceil((state.breakEndsAtMs - nowMs) / 1_000));
}

export const useFocusCycleStore = create<FocusCycleState>()(
  persist(
    (set) => ({
      ...initialCycleState,
      beginWork: (taskId, targetCycles, taskTitle) =>
        set((state) => {
          const continueSequence =
            state.phase === "ready" && state.taskId === taskId;
          return {
            phase: "work",
            taskId,
            taskTitle: taskTitle ?? (continueSequence ? state.taskTitle : null),
            completedCycles: continueSequence ? state.completedCycles : 0,
            targetCycles: continueSequence
              ? state.targetCycles
              : Math.max(1, Math.round(targetCycles)),
            breakDurationSeconds: 0,
            breakRemainingSeconds: 0,
            breakEndsAtMs: null,
          };
        }),
      completeWork: (taskId, settings, taskTitle, nowMs = Date.now()) =>
        set((state) => {
          const continuing = state.phase === "work" && state.taskId === taskId;
          const targetCycles = continuing
            ? state.targetCycles
            : Math.max(1, Math.round(settings.cycles));
          const completedCycles = Math.min(
            targetCycles,
            (continuing ? state.completedCycles : 0) + 1,
          );
          if (completedCycles >= targetCycles) {
            return {
              phase: "complete",
              taskId,
              taskTitle: taskTitle ?? state.taskTitle,
              completedCycles,
              targetCycles,
              breakDurationSeconds: 0,
              breakRemainingSeconds: 0,
              breakEndsAtMs: null,
            };
          }
          const breakDurationSeconds = Math.max(
            1,
            Math.round(settings.breakMinutes * 60),
          );
          return {
            phase: "break",
            taskId,
            taskTitle: taskTitle ?? state.taskTitle,
            completedCycles,
            targetCycles,
            breakDurationSeconds,
            breakRemainingSeconds: breakDurationSeconds,
            breakEndsAtMs: settings.autoStartBreak
              ? nowMs + breakDurationSeconds * 1_000
              : null,
          };
        }),
      pauseBreak: (nowMs = Date.now()) =>
        set((state) => ({
          breakRemainingSeconds: breakSecondsAt(state, nowMs),
          breakEndsAtMs: null,
        })),
      resumeBreak: (nowMs = Date.now()) =>
        set((state) => ({
          breakEndsAtMs:
            state.phase === "break" && state.breakRemainingSeconds > 0
              ? nowMs + state.breakRemainingSeconds * 1_000
              : null,
        })),
      finishBreak: () =>
        set((state) => ({
          phase: "ready",
          taskId: state.taskId,
          taskTitle: state.taskTitle,
          completedCycles: state.completedCycles,
          targetCycles: state.targetCycles,
          breakDurationSeconds: 0,
          breakRemainingSeconds: 0,
          breakEndsAtMs: null,
        })),
      resetCycle: () => set(initialCycleState),
    }),
    {
      name: "opc-workspace-focus-cycle-v1",
      storage: createJSONStorage(focusCycleStorage),
      partialize: (state) => ({
        phase: state.phase,
        taskId: state.taskId,
        taskTitle: state.taskTitle,
        completedCycles: state.completedCycles,
        targetCycles: state.targetCycles,
        breakDurationSeconds: state.breakDurationSeconds,
        breakRemainingSeconds: state.breakRemainingSeconds,
        breakEndsAtMs: state.breakEndsAtMs,
      }),
      version: 1,
    },
  ),
);

export function deriveBreakClock(
  state: Pick<
    FocusCycleState,
    "phase" | "breakEndsAtMs" | "breakRemainingSeconds" | "breakDurationSeconds"
  >,
  nowMs = Date.now(),
): FocusClock {
  const remainingSeconds = breakSecondsAt(state, nowMs);
  const elapsedSeconds = Math.max(
    0,
    state.breakDurationSeconds - remainingSeconds,
  );
  return {
    elapsedSeconds,
    remainingSeconds,
    progress:
      state.breakDurationSeconds > 0
        ? Math.min(1, elapsedSeconds / state.breakDurationSeconds)
        : 0,
    uncertainSeconds: 0,
  };
}

export function useBreakClock(): FocusClock {
  const state = useFocusCycleStore();
  const running = state.phase === "break" && state.breakEndsAtMs !== null;
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    setNowMs(Date.now());
    if (!running) return;
    const interval = window.setInterval(() => setNowMs(Date.now()), 1_000);
    return () => window.clearInterval(interval);
  }, [running, state.breakEndsAtMs]);

  return deriveBreakClock(state, nowMs);
}

function parsedTimestamp(value: string | null): number | null {
  if (!value) return null;
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : null;
}

export function deriveFocusClock(
  snapshot: FocusSessionSnapshot | undefined,
  clientNowMs = Date.now(),
): FocusClock {
  const session = snapshot?.session;
  if (!snapshot || !session) {
    return {
      elapsedSeconds: 0,
      remainingSeconds: 0,
      progress: 0,
      uncertainSeconds: 0,
    };
  }

  const serverNowAtSnapshot = parsedTimestamp(snapshot.serverNow);
  const effectiveServerNow =
    serverNowAtSnapshot === null
      ? clientNowMs
      : serverNowAtSnapshot + Math.max(0, clientNowMs - snapshot.receivedAtMs);
  let elapsedSeconds = session.accumulatedSeconds;

  if (session.status === "active") {
    const lastResumedAt = parsedTimestamp(session.lastResumedAt);
    if (lastResumedAt !== null) {
      elapsedSeconds += Math.max(
        0,
        Math.floor((effectiveServerNow - lastResumedAt) / 1_000),
      );
    }
  } else if (session.status === "recovery_pending") {
    const lastResumedAt = parsedTimestamp(session.lastResumedAt);
    const heartbeatAt = parsedTimestamp(session.lastHeartbeatAt);
    if (lastResumedAt !== null && heartbeatAt !== null) {
      elapsedSeconds += Math.max(
        0,
        Math.floor(
          (Math.min(effectiveServerNow, heartbeatAt) - lastResumedAt) / 1_000,
        ),
      );
    }
  }

  elapsedSeconds = Math.min(
    session.plannedSeconds,
    Math.max(0, elapsedSeconds),
  );
  const remainingSeconds = Math.max(0, session.plannedSeconds - elapsedSeconds);
  const heartbeatAt = parsedTimestamp(session.lastHeartbeatAt);
  const uncertainSeconds =
    session.status === "recovery_pending" && heartbeatAt !== null
      ? Math.min(
          session.plannedSeconds - elapsedSeconds,
          Math.max(0, Math.floor((effectiveServerNow - heartbeatAt) / 1_000)),
        )
      : 0;

  return {
    elapsedSeconds,
    remainingSeconds,
    progress:
      session.plannedSeconds > 0
        ? Math.min(1, elapsedSeconds / session.plannedSeconds)
        : 0,
    uncertainSeconds,
  };
}

export function useFocusClock(
  snapshot: FocusSessionSnapshot | undefined,
): FocusClock {
  const active = snapshot?.session?.status === "active";
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    setNowMs(Date.now());
    if (!active) return;
    const interval = window.setInterval(() => setNowMs(Date.now()), 1_000);
    return () => window.clearInterval(interval);
  }, [active, snapshot?.receivedAtMs, snapshot?.session?.id]);

  return deriveFocusClock(snapshot, nowMs);
}

export function formatFocusTime(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds)
    ? Math.max(0, Math.floor(seconds))
    : 0;
  const minutes = Math.floor(safeSeconds / 60);
  const remainingSeconds = safeSeconds % 60;

  return `${String(minutes).padStart(2, "0")}:${String(remainingSeconds).padStart(2, "0")}`;
}
