import { beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_THEME,
  sanitizeAppearanceTheme,
  sanitizeFocusSettings,
  useSettingsStore,
} from "./settings";

describe("focus settings", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      theme: DEFAULT_THEME,
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
});
