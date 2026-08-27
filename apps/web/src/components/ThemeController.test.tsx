import { act, cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_THEME,
  useSettingsStore,
} from "../store/settings";
import { ThemeController } from "./ThemeController";

describe("ThemeController", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      theme: DEFAULT_THEME,
    });
  });

  afterEach(() => {
    cleanup();
    delete document.documentElement.dataset.theme;
    document.documentElement.style.removeProperty("color-scheme");
  });

  it("applies theme changes to the document root", () => {
    render(<ThemeController />);
    expect(document.documentElement.dataset.theme).toBe("dark");

    act(() => useSettingsStore.getState().setTheme("light"));

    expect(document.documentElement.dataset.theme).toBe("light");
    expect(document.documentElement.style.colorScheme).toBe("light");
  });
});
