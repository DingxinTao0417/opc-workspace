import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  DEFAULT_THEME,
  clearLegacySettings,
  LEGACY_SETTINGS_STORAGE_KEY,
  LOCAL_SETTINGS_STORAGE_KEY,
  readLocalAvatarSnapshot,
  readLegacySettingsSnapshot,
  sanitizeAppearanceTheme,
  sanitizeFocusSettings,
  sanitizeGeneralSettings,
  sanitizeProfileSettings,
  useSettingsStore,
} from "./settings";

function testStorage(entries: Record<string, string> = {}): Storage {
  const values = new Map(Object.entries(entries));
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => values.delete(key),
    setItem: (key, value) => values.set(key, value),
  };
}

describe("focus settings", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
      preview: null,
    });
  });

  it("normalizes invalid and out-of-range values", () => {
    expect(
      sanitizeFocusSettings({
        focusMinutes: 123,
        breakMinutes: 3,
        cycles: 3.7,
        autoStartBreak: "yes",
        autoStartFocus: true,
        soundEnabled: null,
      }),
    ).toEqual({
      focusMinutes: 120,
      breakMinutes: 5,
      cycles: 4,
      autoStartBreak: true,
      autoStartFocus: true,
      soundEnabled: true,
    });
  });

  it("normalizes partial updates against the current settings", () => {
    const { setSettings } = useSettingsStore.getState();

    setSettings({ focusMinutes: 52, breakMinutes: 28, cycles: 9 });

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 50,
      breakMinutes: 30,
      cycles: 8,
      autoStartBreak: true,
      autoStartFocus: false,
      soundEnabled: true,
    });
  });

  it("sanitizes the shape read from persisted storage", () => {
    expect(
      sanitizeFocusSettings({
        focusMinutes: -10,
        breakMinutes: 17,
        cycles: "four",
        autoStartBreak: false,
        autoStartFocus: "false",
        soundEnabled: false,
      }),
    ).toEqual({
      focusMinutes: 5,
      breakMinutes: 15,
      cycles: 4,
      autoStartBreak: false,
      autoStartFocus: false,
      soundEnabled: false,
    });
  });

  it("accepts only supported appearance themes", () => {
    expect(sanitizeAppearanceTheme("light")).toBe("light");
    expect(sanitizeAppearanceTheme("dark")).toBe("dark");
    expect(sanitizeAppearanceTheme("system")).toBe(DEFAULT_THEME);
  });

  it("sanitizes general workspace preferences", () => {
    expect(
      sanitizeGeneralSettings({
        defaultRoute: "unknown",
        showRightOverview: false,
        reduceMotion: "yes",
      }),
    ).toEqual({
      defaultRoute: "today",
      showRightOverview: false,
      reduceMotion: false,
      closeToTray: true,
    });
  });

  it("sanitizes local profile values", () => {
    expect(
      sanitizeProfileSettings({
        displayName: "  Dingxin   Tao  ",
        avatarDataUrl: "data:text/html;base64,PHNjcmlwdD4=",
      }),
    ).toEqual({
      displayName: "Dingxin Tao",
      avatarDataUrl: null,
    });
  });

  it("keeps previews separate until they are committed", () => {
    const { beginPreview, setPreview, commitPreview } =
      useSettingsStore.getState();

    beginPreview();
    setPreview({
      focus: { ...DEFAULT_FOCUS_SETTINGS, focusMinutes: 65 },
      general: { ...DEFAULT_GENERAL_SETTINGS, showRightOverview: false },
      profile: { ...DEFAULT_PROFILE_SETTINGS, displayName: "Dingxin Tao" },
      theme: "light",
    });

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 50,
      showRightOverview: true,
      theme: "dark",
      preview: {
        focus: { focusMinutes: 65 },
        general: { showRightOverview: false },
        profile: { displayName: "Dingxin Tao" },
        theme: "light",
      },
    });

    commitPreview();

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 65,
      showRightOverview: false,
      displayName: "Dingxin Tao",
      theme: "light",
      preview: null,
    });
  });

  it("reads and sanitizes the historical persisted envelope", () => {
    const storage = testStorage({
      [LEGACY_SETTINGS_STORAGE_KEY]: JSON.stringify({
        state: {
          focusMinutes: 65,
          breakMinutes: 10,
          cycles: 3,
          autoStartBreak: false,
          autoStartFocus: true,
          soundEnabled: false,
          defaultRoute: "projects",
          showRightOverview: false,
          reduceMotion: true,
          displayName: "  Legacy   Workspace ",
          avatarDataUrl: "data:image/png;base64,AA==",
          theme: "light",
        },
        version: 0,
      }),
    });

    expect(readLegacySettingsSnapshot(storage)).toEqual({
      exists: true,
      focus: {
        focusMinutes: 65,
        breakMinutes: 10,
        cycles: 3,
        autoStartBreak: false,
        autoStartFocus: true,
        soundEnabled: false,
      },
      general: {
        defaultRoute: "projects",
        showRightOverview: false,
        reduceMotion: true,
        closeToTray: true,
      },
      profile: {
        displayName: "Legacy Workspace",
        avatarDataUrl: "data:image/png;base64,AA==",
      },
      theme: "light",
    });
    clearLegacySettings(storage);
    expect(storage.getItem(LEGACY_SETTINGS_STORAGE_KEY)).toBeNull();
  });

  it("does not claim malformed legacy storage exists", () => {
    const storage = testStorage({ [LEGACY_SETTINGS_STORAGE_KEY]: "{" });
    expect(readLegacySettingsSnapshot(storage)).toMatchObject({
      exists: false,
      focus: DEFAULT_FOCUS_SETTINGS,
      general: DEFAULT_GENERAL_SETTINGS,
      profile: DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
    });
  });

  it("distinguishes an explicitly cleared local avatar from no local snapshot", () => {
    expect(readLocalAvatarSnapshot(testStorage())).toEqual({
      exists: false,
      avatarDataUrl: null,
    });
    expect(
      readLocalAvatarSnapshot(
        testStorage({
          [LOCAL_SETTINGS_STORAGE_KEY]: JSON.stringify({
            state: { avatarDataUrl: null },
            version: 0,
          }),
        }),
      ),
    ).toEqual({ exists: true, avatarDataUrl: null });
  });

  it("replaces committed values with a normalized server snapshot", () => {
    useSettingsStore.getState().replaceCommitted({
      focus: { ...DEFAULT_FOCUS_SETTINGS, focusMinutes: 25 },
      general: { ...DEFAULT_GENERAL_SETTINGS, defaultRoute: "projects" },
      profile: {
        displayName: "  Server   Workspace ",
        avatarDataUrl: null,
      },
      theme: "light",
    });

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 25,
      defaultRoute: "projects",
      displayName: "Server Workspace",
      theme: "light",
      preview: null,
    });
  });
});
