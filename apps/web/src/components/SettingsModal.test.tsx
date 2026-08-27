import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { DEFAULT_FOCUS_SETTINGS, useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import { SettingsModal } from "./SettingsModal";

describe("SettingsModal", () => {
  beforeEach(() => {
    useSettingsStore.setState(DEFAULT_FOCUS_SETTINGS);
    useUiStore.setState({ settingsOpen: true });
  });

  afterEach(cleanup);

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
});
