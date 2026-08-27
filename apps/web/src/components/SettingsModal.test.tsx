import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_THEME,
  useSettingsStore,
} from "../store/settings";
import { useUiStore } from "../store/ui";
import { SettingsModal } from "./SettingsModal";
import { applyTheme } from "./ThemeController";

describe("SettingsModal", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      theme: DEFAULT_THEME,
    });
    useUiStore.setState({ settingsOpen: true });
    applyTheme(DEFAULT_THEME);
  });

  afterEach(() => {
    cleanup();
    applyTheme(DEFAULT_THEME);
  });

  it("saves focus settings and closes", () => {
    render(<SettingsModal />);

    fireEvent.click(screen.getByRole("button", { name: "增加专注时长" }));
    fireEvent.click(screen.getByRole("switch", { name: "自动开始专注" }));
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 55,
      autoStartFocus: true,
    });
    expect(useUiStore.getState().settingsOpen).toBe(false);
  });

  it("discards draft changes when cancelled", () => {
    render(<SettingsModal />);

    fireEvent.click(screen.getByRole("button", { name: "增加休息时长" }));
    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(useSettingsStore.getState().breakMinutes).toBe(5);
    expect(useUiStore.getState().settingsOpen).toBe(false);
  });

  it("saves the selected appearance theme", () => {
    render(<SettingsModal />);

    fireEvent.click(screen.getByRole("radio", { name: "亮色" }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(useSettingsStore.getState().theme).toBe("dark");

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState().theme).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("restores the persisted theme when preview is cancelled", () => {
    render(<SettingsModal />);

    fireEvent.click(screen.getByRole("radio", { name: "亮色" }));
    expect(document.documentElement.dataset.theme).toBe("light");

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(useSettingsStore.getState().theme).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });
});
