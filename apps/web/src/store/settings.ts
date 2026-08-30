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

export const LEGACY_SETTINGS_STORAGE_KEY = "opc-focus-settings";
export const LOCAL_SETTINGS_STORAGE_KEY = "opc-settings-local-v1";

export function getSettingsStorage() {
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
  closeToTray: boolean;
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
  replaceCommitted: (settings: SettingsPreview) => void;
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
  closeToTray: true,
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
    closeToTray: normalizeBoolean(candidate.closeToTray, fallback.closeToTray),
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
          (/^data:image\/(?:png|jpeg|webp);base64,/i.test(avatarDataUrl) ||
            avatarDataUrl.startsWith("blob:"))
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
      replaceCommitted: (settings) =>
        set((state) => {
          const nextProfile = sanitizeProfileSettings(settings.profile, state);
          if (
            state.avatarDataUrl?.startsWith("blob:") &&
            state.avatarDataUrl !== nextProfile.avatarDataUrl &&
            typeof URL.revokeObjectURL === "function"
          ) {
            URL.revokeObjectURL(state.avatarDataUrl);
          }
          return {
            ...sanitizeFocusSettings(settings.focus, state),
            ...sanitizeGeneralSettings(settings.general, state),
            ...nextProfile,
            theme: sanitizeAppearanceTheme(settings.theme, state.theme),
            preview: null,
          };
        }),
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
      name: LOCAL_SETTINGS_STORAGE_KEY,
      storage: createJSONStorage(getSettingsStorage),
      // Runtime avatar display URLs are backed by authenticated Blob responses
      // and must never be persisted. The old key remains readable only for the
      // one-time controlled-file migration below.
      partialize: () => ({}),
      merge: (persistedState, currentState) => {
        const persisted =
          typeof persistedState === "object" && persistedState !== null
            ? (persistedState as Record<string, unknown>)
            : {};
        return {
          ...currentState,
          avatarDataUrl: sanitizeProfileSettings(
            {
              displayName: currentState.displayName,
              avatarDataUrl: persisted.avatarDataUrl,
            },
            currentState,
          ).avatarDataUrl,
        };
      },
    },
  ),
);

export interface LegacySettingsSnapshot extends SettingsPreview {
  exists: boolean;
}

export interface LocalAvatarSnapshot {
  exists: boolean;
  avatarDataUrl: string | null;
}

export function readLocalAvatarSnapshot(
  storage: Storage = getSettingsStorage(),
): LocalAvatarSnapshot {
  const fallback: LocalAvatarSnapshot = {
    exists: false,
    avatarDataUrl: null,
  };
  try {
    const raw = storage.getItem(LOCAL_SETTINGS_STORAGE_KEY);
    if (!raw) return fallback;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return fallback;
    const envelope = parsed as Record<string, unknown>;
    if (typeof envelope.state !== "object" || envelope.state === null) {
      return fallback;
    }
    const state = envelope.state as Record<string, unknown>;
    if (!("avatarDataUrl" in state)) return fallback;
    return {
      exists: true,
      avatarDataUrl: sanitizeProfileSettings({
        displayName: DEFAULT_PROFILE_SETTINGS.displayName,
        avatarDataUrl: state.avatarDataUrl,
      }).avatarDataUrl,
    };
  } catch {
    return fallback;
  }
}

export function clearLocalAvatarSnapshot(
  storage: Storage = getSettingsStorage(),
): void {
  try {
    storage.removeItem(LOCAL_SETTINGS_STORAGE_KEY);
  } catch {
    // A verified server avatar remains authoritative even when stale browser
    // compatibility storage cannot be removed.
  }
}

export function readLegacySettingsSnapshot(
  storage: Storage = getSettingsStorage(),
): LegacySettingsSnapshot {
  const fallback: LegacySettingsSnapshot = {
    exists: false,
    focus: DEFAULT_FOCUS_SETTINGS,
    general: DEFAULT_GENERAL_SETTINGS,
    profile: DEFAULT_PROFILE_SETTINGS,
    theme: DEFAULT_THEME,
  };
  try {
    const raw = storage.getItem(LEGACY_SETTINGS_STORAGE_KEY);
    if (!raw) return fallback;
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return fallback;
    const envelope = parsed as Record<string, unknown>;
    const state =
      typeof envelope.state === "object" && envelope.state !== null
        ? envelope.state
        : envelope;
    return {
      exists: true,
      focus: sanitizeFocusSettings(state),
      general: sanitizeGeneralSettings(state),
      profile: sanitizeProfileSettings(state),
      theme: sanitizeAppearanceTheme((state as Record<string, unknown>).theme),
    };
  } catch {
    return fallback;
  }
}

export function clearLegacySettings(
  storage: Storage = getSettingsStorage(),
): void {
  try {
    storage.removeItem(LEGACY_SETTINGS_STORAGE_KEY);
  } catch {
    // Server facts are already durable; an inaccessible legacy cache must not
    // block application startup or be reported as a failed migration.
  }
}

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
    closeToTray: state.closeToTray,
  };
}

function pickProfileSettings(state: ProfileSettings): ProfileSettings {
  return {
    displayName: state.displayName,
    avatarDataUrl: state.avatarDataUrl,
  };
}
