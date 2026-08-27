import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  DEFAULT_THEME,
  useSettingsStore,
} from "../store/settings";
import { ThemeController } from "./ThemeController";

describe("ThemeController", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
      preview: null,
    });
  });

  afterEach(() => {
    cleanup();
    delete document.documentElement.dataset.theme;
    delete document.documentElement.dataset.reduceMotion;
    document.documentElement.style.removeProperty("color-scheme");
  });

  it("applies theme changes to the document root", () => {
    render(<ThemeController />);
    expect(document.documentElement.dataset.theme).toBe("dark");

    act(() => useSettingsStore.getState().setTheme("light"));

    expect(document.documentElement.dataset.theme).toBe("light");
    expect(document.documentElement.style.colorScheme).toBe("light");
  });

  it("applies the reduced-motion preference", () => {
    render(<ThemeController />);
    expect(document.documentElement.dataset.reduceMotion).toBe("false");

    act(() =>
      useSettingsStore.getState().setGeneralSettings({ reduceMotion: true }),
    );

    expect(document.documentElement.dataset.reduceMotion).toBe("true");
  });

  it("applies preview values without replacing saved preferences", () => {
    render(<ThemeController />);

    act(() => {
      useSettingsStore.getState().beginPreview();
      useSettingsStore.getState().setPreview({
        focus: DEFAULT_FOCUS_SETTINGS,
        general: { ...DEFAULT_GENERAL_SETTINGS, reduceMotion: true },
        profile: DEFAULT_PROFILE_SETTINGS,
        theme: "light",
      });
    });

    expect(document.documentElement.dataset.theme).toBe("light");
    expect(document.documentElement.dataset.reduceMotion).toBe("true");
    expect(useSettingsStore.getState()).toMatchObject({
      theme: "dark",
      reduceMotion: false,
    });

    act(() => useSettingsStore.getState().cancelPreview());

    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.dataset.reduceMotion).toBe("false");
  });
});
