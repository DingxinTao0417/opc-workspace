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

export type DefaultRoute = "today" | "tasks" | "projects" | "clients" | "focus";

export interface GeneralSettings {
  defaultRoute: DefaultRoute;
  showRightOverview: boolean;
  reduceMotion: boolean;
}

export interface ProfileSettings {
  displayName: string;
  avatarDataUrl: string | null;
}

export interface SettingsPreview {
  focus: FocusSettings;
  general: GeneralSettings;
  profile: ProfileSettings;
  theme: AppearanceTheme;
}

interface SettingsState
  extends FocusSettings, GeneralSettings, ProfileSettings {
  theme: AppearanceTheme;
  preview: SettingsPreview | null;
  setSettings: (settings: Partial<FocusSettings>) => void;
  setGeneralSettings: (settings: Partial<GeneralSettings>) => void;
  setProfileSettings: (settings: Partial<ProfileSettings>) => void;
  setTheme: (theme: AppearanceTheme) => void;
  beginPreview: () => void;
  setPreview: (settings: SettingsPreview) => void;
  commitPreview: () => void;
  cancelPreview: () => void;
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

export const DEFAULT_GENERAL_SETTINGS: GeneralSettings = {
  defaultRoute: "today",
  showRightOverview: true,
  reduceMotion: false,
};

export const DEFAULT_PROFILE_SETTINGS: ProfileSettings = {
  displayName: "opc-workspace",
  avatarDataUrl: null,
};

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

function sanitizeDefaultRoute(
  value: unknown,
  fallback: DefaultRoute,
): DefaultRoute {
  return value === "today" ||
    value === "tasks" ||
    value === "projects" ||
    value === "clients" ||
    value === "focus"
    ? value
    : fallback;
}

export function sanitizeGeneralSettings(
  value: unknown,
  fallback: GeneralSettings = DEFAULT_GENERAL_SETTINGS,
): GeneralSettings {
  const candidate =
    typeof value === "object" && value !== null
      ? (value as Partial<Record<keyof GeneralSettings, unknown>>)
      : {};

  return {
    defaultRoute: sanitizeDefaultRoute(
      candidate.defaultRoute,
      fallback.defaultRoute,
    ),
    showRightOverview: normalizeBoolean(
      candidate.showRightOverview,
      fallback.showRightOverview,
    ),
    reduceMotion: normalizeBoolean(
      candidate.reduceMotion,
      fallback.reduceMotion,
    ),
  };
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

export function sanitizeProfileSettings(
  value: unknown,
  fallback: ProfileSettings = DEFAULT_PROFILE_SETTINGS,
): ProfileSettings {
  const candidate =
    typeof value === "object" && value !== null
      ? (value as Partial<Record<keyof ProfileSettings, unknown>>)
      : {};
  const normalizedName =
    typeof candidate.displayName === "string"
      ? candidate.displayName.trim().replace(/\s+/g, " ").slice(0, 32)
      : "";
  const avatarDataUrl = candidate.avatarDataUrl;
  const normalizedAvatar =
    avatarDataUrl === null
      ? null
      : typeof avatarDataUrl === "string" &&
          avatarDataUrl.length <= 2_900_000 &&
          /^data:image\/(?:png|jpeg|webp);base64,/i.test(avatarDataUrl)
        ? avatarDataUrl
        : fallback.avatarDataUrl;

  return {
    displayName: normalizedName || fallback.displayName,
    avatarDataUrl: normalizedAvatar,
  };
}

export const useSettingsStore = create<SettingsState>()(
  persist(
    (set) => ({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
      preview: null,
      setSettings: (settings) =>
        set((state) => sanitizeFocusSettings(settings, state)),
      setGeneralSettings: (settings) =>
        set((state) => sanitizeGeneralSettings(settings, state)),
      setProfileSettings: (settings) =>
        set((state) => sanitizeProfileSettings(settings, state)),
      setTheme: (theme) => set({ theme: sanitizeAppearanceTheme(theme) }),
      beginPreview: () =>
        set((state) => ({
          preview: {
            focus: pickFocusSettings(state),
            general: pickGeneralSettings(state),
            profile: pickProfileSettings(state),
            theme: state.theme,
          },
        })),
      setPreview: (settings) =>
        set((state) => ({
          preview: {
            focus: sanitizeFocusSettings(
              settings.focus,
              state.preview?.focus ?? pickFocusSettings(state),
            ),
            general: sanitizeGeneralSettings(
              settings.general,
              state.preview?.general ?? pickGeneralSettings(state),
            ),
            profile: sanitizeProfileSettings(
              settings.profile,
              state.preview?.profile ?? pickProfileSettings(state),
            ),
            theme: sanitizeAppearanceTheme(
              settings.theme,
              state.preview?.theme ?? state.theme,
            ),
          },
        })),
      commitPreview: () =>
        set((state) =>
          state.preview
            ? {
                ...state.preview.focus,
                ...state.preview.general,
                ...state.preview.profile,
                theme: state.preview.theme,
                preview: null,
              }
            : state,
        ),
      cancelPreview: () => set({ preview: null }),
      resetSettings: () =>
        set({
          ...DEFAULT_FOCUS_SETTINGS,
          ...DEFAULT_GENERAL_SETTINGS,
          ...DEFAULT_PROFILE_SETTINGS,
          theme: DEFAULT_THEME,
          preview: null,
        }),
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
        defaultRoute: state.defaultRoute,
        showRightOverview: state.showRightOverview,
        reduceMotion: state.reduceMotion,
        displayName: state.displayName,
        avatarDataUrl: state.avatarDataUrl,
      }),
      merge: (persistedState, currentState) => {
        const persisted =
          typeof persistedState === "object" && persistedState !== null
            ? (persistedState as Record<string, unknown>)
            : {};
        return {
          ...currentState,
          ...sanitizeFocusSettings(persisted),
          ...sanitizeGeneralSettings(persisted),
          ...sanitizeProfileSettings(persisted),
          theme: sanitizeAppearanceTheme(persisted.theme),
        };
      },
    },
  ),
);

export function getFocusSettings(): FocusSettings {
  return pickFocusSettings(useSettingsStore.getState());
}

function pickFocusSettings(state: FocusSettings): FocusSettings {
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

export function getGeneralSettings(): GeneralSettings {
  return pickGeneralSettings(useSettingsStore.getState());
}

export function getProfileSettings(): ProfileSettings {
  return pickProfileSettings(useSettingsStore.getState());
}

function pickGeneralSettings(state: GeneralSettings): GeneralSettings {
  return {
    defaultRoute: state.defaultRoute,
    showRightOverview: state.showRightOverview,
    reduceMotion: state.reduceMotion,
  };
}

function pickProfileSettings(state: ProfileSettings): ProfileSettings {
  return {
    displayName: state.displayName,
    avatarDataUrl: state.avatarDataUrl,
  };
}
