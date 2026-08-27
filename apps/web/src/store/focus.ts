import { create } from "zustand";
import { DEFAULT_FOCUS_SETTINGS, type FocusSettings } from "./settings";

const MIN_FOCUS_MINUTES = 5;
const MAX_FOCUS_MINUTES = 120;
const MIN_BREAK_MINUTES = 5;
const MAX_BREAK_MINUTES = 30;
const MIN_CYCLES = 1;
const MAX_CYCLES = 8;

export type FocusPhase = "focus" | "break";

export type FocusConfig = FocusSettings;

function clampInteger(value: number, minimum: number, maximum: number): number {
  if (!Number.isFinite(value)) {
    return minimum;
  }

  return Math.min(maximum, Math.max(minimum, Math.round(value)));
}

function normalizeConfig(config: FocusConfig): FocusConfig {
  return {
    focusMinutes: clampInteger(
      config.focusMinutes,
      MIN_FOCUS_MINUTES,
      MAX_FOCUS_MINUTES,
    ),
    breakMinutes: clampInteger(
      config.breakMinutes,
      MIN_BREAK_MINUTES,
      MAX_BREAK_MINUTES,
    ),
    cycles: clampInteger(config.cycles, MIN_CYCLES, MAX_CYCLES),
    autoStartBreak: config.autoStartBreak,
    autoStartFocus: config.autoStartFocus,
    soundEnabled: config.soundEnabled,
  };
}

export function formatFocusTime(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds)
    ? Math.max(0, Math.floor(seconds))
    : 0;
  const minutes = Math.floor(safeSeconds / 60);
  const remainingSeconds = safeSeconds % 60;

  return `${String(minutes).padStart(2, "0")}:${String(remainingSeconds).padStart(2, "0")}`;
}

interface FocusState extends FocusConfig {
  phase: FocusPhase;
  durationMinutes: number;
  remainingSeconds: number;
  running: boolean;
  completedCycles: number;
  completed: boolean;
  start: () => void;
  pause: () => void;
  toggle: () => void;
  reset: () => void;
  setDurationsOrSync: (config: FocusConfig) => void;
  tick: () => void;
}

export const useFocusStore = create<FocusState>((set) => ({
  ...DEFAULT_FOCUS_SETTINGS,
  phase: "focus",
  durationMinutes: DEFAULT_FOCUS_SETTINGS.focusMinutes,
  remainingSeconds: DEFAULT_FOCUS_SETTINGS.focusMinutes * 60,
  running: false,
  completedCycles: 0,
  completed: false,
  start: () =>
    set((state) => ({
      running: state.remainingSeconds > 0 && !state.completed,
    })),
  pause: () => set({ running: false }),
  toggle: () =>
    set((state) => ({
      running: state.running
        ? false
        : state.remainingSeconds > 0 && !state.completed,
    })),
  reset: () =>
    set((state) => ({
      phase: "focus",
      durationMinutes: state.focusMinutes,
      remainingSeconds: state.focusMinutes * 60,
      running: false,
      completedCycles: 0,
      completed: false,
    })),
  setDurationsOrSync: (config) => {
    const normalized = normalizeConfig(config);

    set({
      ...normalized,
      phase: "focus",
      durationMinutes: normalized.focusMinutes,
      remainingSeconds: normalized.focusMinutes * 60,
      running: false,
      completedCycles: 0,
      completed: false,
    });
  },
  tick: () =>
    set((state) => {
      if (!state.running) {
        return state;
      }

      if (state.remainingSeconds > 1) {
        return { remainingSeconds: state.remainingSeconds - 1 };
      }

      if (state.phase === "focus") {
        const completedCycles = state.completedCycles + 1;

        if (completedCycles >= state.cycles) {
          return {
            phase: "focus",
            durationMinutes: state.focusMinutes,
            remainingSeconds: state.focusMinutes * 60,
            running: false,
            completedCycles,
            completed: true,
          };
        }

        return {
          phase: "break",
          durationMinutes: state.breakMinutes,
          remainingSeconds: state.breakMinutes * 60,
          running: state.autoStartBreak,
          completedCycles,
          completed: false,
        };
      }

      return {
        phase: "focus",
        durationMinutes: state.focusMinutes,
        remainingSeconds: state.focusMinutes * 60,
        running: state.autoStartFocus,
        completed: false,
      };
    }),
}));
