import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  DEFAULT_THEME,
  useSettingsStore,
} from "../store/settings";
import { useUiStore } from "../store/ui";
import { SettingsModal } from "./SettingsModal";
import { applyTheme, ThemeController } from "./ThemeController";

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="current-location">{location.pathname}</output>;
}

function renderSettings() {
  return render(
    <MemoryRouter initialEntries={["/today"]}>
      <ThemeController />
      <LocationProbe />
      <SettingsModal />
    </MemoryRouter>,
  );
}

describe("SettingsModal", () => {
  beforeEach(() => {
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
      preview: null,
    });
    useUiStore.setState({ settingsOpen: true });
    applyTheme(DEFAULT_THEME);
  });

  afterEach(() => {
    cleanup();
    applyTheme(DEFAULT_THEME);
  });

  it("saves focus settings and closes", () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "专注" }));
    fireEvent.click(screen.getByRole("button", { name: "增加专注时长" }));
    fireEvent.click(screen.getByRole("switch", { name: "自动开始专注" }));

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 50,
      autoStartFocus: false,
      preview: {
        focus: { focusMinutes: 55, autoStartFocus: true },
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 55,
      autoStartFocus: true,
      preview: null,
    });
    expect(useUiStore.getState().settingsOpen).toBe(false);
  });

  it("discards draft changes when cancelled", () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "专注" }));
    fireEvent.click(screen.getByRole("button", { name: "增加休息时长" }));
    expect(useSettingsStore.getState().preview?.focus.breakMinutes).toBe(10);
    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(useSettingsStore.getState().breakMinutes).toBe(5);
    expect(useSettingsStore.getState().preview).toBeNull();
    expect(useUiStore.getState().settingsOpen).toBe(false);
  });

  it("saves the selected appearance theme", () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "外观" }));
    fireEvent.click(screen.getByRole("radio", { name: /亮色/ }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(useSettingsStore.getState().theme).toBe("dark");

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState().theme).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("restores the persisted theme when preview is cancelled", () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "外观" }));
    fireEvent.click(screen.getByRole("radio", { name: /亮色/ }));
    expect(document.documentElement.dataset.theme).toBe("light");

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(useSettingsStore.getState().theme).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("saves general workspace preferences", () => {
    renderSettings();

    fireEvent.change(screen.getByLabelText("默认首页"), {
      target: { value: "tasks" },
    });
    fireEvent.click(screen.getByRole("switch", { name: "显示右侧概览" }));
    fireEvent.click(screen.getByRole("switch", { name: "减少动效" }));

    expect(screen.getByTestId("current-location").textContent).toBe("/tasks");
    expect(document.documentElement.dataset.reduceMotion).toBe("true");
    expect(useSettingsStore.getState()).toMatchObject({
      defaultRoute: "today",
      showRightOverview: true,
      reduceMotion: false,
      preview: {
        general: {
          defaultRoute: "tasks",
          showRightOverview: false,
          reduceMotion: true,
        },
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState()).toMatchObject({
      defaultRoute: "tasks",
      showRightOverview: false,
      reduceMotion: true,
      preview: null,
    });
  });

  it("previews and saves the profile name", () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "个人资料" }));
    fireEvent.change(screen.getByLabelText("名称"), {
      target: { value: "Dingxin Tao" },
    });

    expect(useSettingsStore.getState()).toMatchObject({
      displayName: "opc-workspace",
      preview: { profile: { displayName: "Dingxin Tao" } },
    });

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(useSettingsStore.getState()).toMatchObject({
      displayName: "Dingxin Tao",
      preview: null,
    });
  });

  it("previews a local avatar upload without saving it immediately", async () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "个人资料" }));
    const avatar = new File([new Uint8Array([137, 80, 78, 71])], "avatar.png", {
      type: "image/png",
    });
    fireEvent.change(screen.getByLabelText("上传头像"), {
      target: { files: [avatar] },
    });

    await waitFor(() =>
      expect(
        useSettingsStore.getState().preview?.profile.avatarDataUrl,
      ).toMatch(/^data:image\/png;base64,/),
    );
    expect(useSettingsStore.getState().avatarDataUrl).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(useSettingsStore.getState().avatarDataUrl).toBeNull();
    expect(useSettingsStore.getState().preview).toBeNull();
  });

  it("restores the original route and preferences when preview is cancelled", () => {
    renderSettings();

    fireEvent.change(screen.getByLabelText("默认首页"), {
      target: { value: "projects" },
    });
    fireEvent.click(screen.getByRole("switch", { name: "显示右侧概览" }));

    expect(screen.getByTestId("current-location").textContent).toBe(
      "/projects",
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(screen.getByTestId("current-location").textContent).toBe("/today");
    expect(useSettingsStore.getState()).toMatchObject({
      defaultRoute: "today",
      showRightOverview: true,
      preview: null,
    });
  });

  it("switches between settings modules", () => {
    renderSettings();

    expect(screen.getByRole("heading", { name: "通用" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关于" }));

    expect(screen.getByText("v0.1.0")).toBeTruthy();
    expect(screen.getByText("本地 SQLite")).toBeTruthy();
  });
});
