import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

const memoryStorage = new Map<string, string>();

const fallbackStorage: Storage = {
  get length() {
    return memoryStorage.size;
  },
  clear: () => memoryStorage.clear(),
  getItem: (key) => memoryStorage.get(key) ?? null,
  key: (index) => [...memoryStorage.keys()][index] ?? null,
  removeItem: (key) => {
    memoryStorage.delete(key);
  },
  setItem: (key, value) => {
    memoryStorage.set(key, value);
  },
};

function getSettingsStorage() {
  try {
    return window.localStorage ?? fallbackStorage;
  } catch {
    return fallbackStorage;
  }
}

export interface FocusSettings {
  focusMinutes: number;
  breakMinutes: number;
  cycles: number;
  autoStartBreak: boolean;
  autoStartFocus: boolean;
  soundEnabled: boolean;
}

export type AppearanceTheme = "light" | "dark";

interface SettingsState extends FocusSettings {
  theme: AppearanceTheme;
  setSettings: (settings: Partial<FocusSettings>) => void;
  setTheme: (theme: AppearanceTheme) => void;
  resetSettings: () => void;
}

export const DEFAULT_FOCUS_SETTINGS: FocusSettings = {
  focusMinutes: 50,
  breakMinutes: 5,
  cycles: 4,
  autoStartBreak: true,
  autoStartFocus: false,
  soundEnabled: true,
};

export const DEFAULT_THEME: AppearanceTheme = "dark";

function normalizeSteppedNumber(
  value: unknown,
  fallback: number,
  min: number,
  max: number,
  step: number,
) {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;

  const clamped = Math.min(max, Math.max(min, value));
  return Math.min(max, Math.max(min, Math.round(clamped / step) * step));
}

function normalizeBoolean(value: unknown, fallback: boolean) {
  return typeof value === "boolean" ? value : fallback;
}

export function sanitizeAppearanceTheme(
  value: unknown,
  fallback: AppearanceTheme = DEFAULT_THEME,
): AppearanceTheme {
  return value === "light" || value === "dark" ? value : fallback;
}

export function sanitizeFocusSettings(
  value: unknown,
  fallback: FocusSettings = DEFAULT_FOCUS_SETTINGS,
): FocusSettings {
  const candidate =
    typeof value === "object" && value !== null
      ? (value as Partial<Record<keyof FocusSettings, unknown>>)
      : {};

  return {
    focusMinutes: normalizeSteppedNumber(
      candidate.focusMinutes,
      fallback.focusMinutes,
      5,
      120,
      5,
    ),
    breakMinutes: normalizeSteppedNumber(
      candidate.breakMinutes,
      fallback.breakMinutes,
      5,
      30,
      5,
    ),
    cycles: normalizeSteppedNumber(candidate.cycles, fallback.cycles, 1, 8, 1),
    autoStartBreak: normalizeBoolean(
      candidate.autoStartBreak,
      fallback.autoStartBreak,
    ),
    autoStartFocus: normalizeBoolean(
      candidate.autoStartFocus,
      fallback.autoStartFocus,
    ),
    soundEnabled: normalizeBoolean(
      candidate.soundEnabled,
      fallback.soundEnabled,
    ),
  };
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set) => ({
      ...DEFAULT_FOCUS_SETTINGS,
      theme: DEFAULT_THEME,
      setSettings: (settings) =>
        set((state) => sanitizeFocusSettings(settings, state)),
      setTheme: (theme) => set({ theme: sanitizeAppearanceTheme(theme) }),
      resetSettings: () =>
        set({ ...DEFAULT_FOCUS_SETTINGS, theme: DEFAULT_THEME }),
    }),
    {
      name: "opc-focus-settings",
      storage: createJSONStorage(getSettingsStorage),
      partialize: (state) => ({
        focusMinutes: state.focusMinutes,
        breakMinutes: state.breakMinutes,
        cycles: state.cycles,
        autoStartBreak: state.autoStartBreak,
        autoStartFocus: state.autoStartFocus,
        soundEnabled: state.soundEnabled,
        theme: state.theme,
      }),
      merge: (persistedState, currentState) => {
        const persisted =
          typeof persistedState === "object" && persistedState !== null
            ? (persistedState as Record<string, unknown>)
            : {};
        return {
          ...currentState,
          ...sanitizeFocusSettings(persisted),
          theme: sanitizeAppearanceTheme(persisted.theme),
        };
      },
    },
  ),
);

export function getFocusSettings(): FocusSettings {
  const state = useSettingsStore.getState();
  return {
    focusMinutes: state.focusMinutes,
    breakMinutes: state.breakMinutes,
    cycles: state.cycles,
    autoStartBreak: state.autoStartBreak,
    autoStartFocus: state.autoStartFocus,
    soundEnabled: state.soundEnabled,
  };
}

export function getAppearanceTheme(): AppearanceTheme {
  return useSettingsStore.getState().theme;
}
