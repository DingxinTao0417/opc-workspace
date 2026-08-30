import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { normalizeAppSettingsResponse } from "../api/client";
import { settingsQueryKey } from "../api/hooks";
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

const desktopApi = vi.hoisted(() => ({
  getRuntimeDiagnostics: vi.fn(),
  setCloseToTrayEnabled: vi.fn(),
}));

vi.mock("../api/desktop", async (importActual) => {
  const actual = await importActual<typeof import("../api/desktop")>();
  return {
    ...actual,
    getRuntimeDiagnostics: desktopApi.getRuntimeDiagnostics,
    setCloseToTrayEnabled: desktopApi.setCloseToTrayEnabled,
  };
});

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="current-location">{location.pathname}</output>;
}

const healthPayload = {
  status: "ok",
  app: {
    name: "opc-workspace",
    version: "0.1.0",
    commit: "abc123def456789-dirty",
  },
  api: { version: "v1" },
  schema: { version: 16 },
};

function settingsPayload(): any {
  return {
    data: {
      schema_version: 2,
      items: [
        {
          key: "workspace",
          value: { display_name: "opc-workspace", avatar_ref: null },
          schema_version: 2,
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        },
        {
          key: "general",
          value: {
            default_route: "today",
            show_right_overview: true,
            reduce_motion: false,
            close_to_tray: true,
          },
          schema_version: 2,
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        },
        {
          key: "appearance",
          value: { theme: "dark" },
          schema_version: 2,
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        },
        {
          key: "focus",
          value: {
            focus_minutes: 50,
            break_minutes: 5,
            cycles: 4,
            auto_start_break: true,
            auto_start_focus: false,
            sound_enabled: true,
          },
          schema_version: 2,
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        },
        {
          key: "storage",
          value: { low_space_threshold_gib: 1 },
          schema_version: 2,
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        },
      ],
    },
  };
}

function savedSettingsPayload(init?: RequestInit) {
  const payload = settingsPayload();
  if (!init?.body) return payload;
  const input = JSON.parse(String(init.body)) as {
    updates: { key: string; value: Record<string, unknown> }[];
  };
  for (const update of input.updates) {
    const item = payload.data.items.find(
      (candidate: { key: string }) => candidate.key === update.key,
    )!;
    item.value = update.value as never;
    item.version += 1;
  }
  return payload;
}

function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(
    settingsQueryKey,
    normalizeAppSettingsResponse(settingsPayload()),
  );
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/today"]}>
        <ThemeController />
        <LocationProbe />
        <SettingsModal />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("SettingsModal", () => {
  beforeEach(() => {
    desktopApi.getRuntimeDiagnostics.mockReset();
    desktopApi.setCloseToTrayEnabled.mockReset();
    desktopApi.setCloseToTrayEnabled.mockResolvedValue(true);
    desktopApi.getRuntimeDiagnostics.mockResolvedValue({
      environment: "browser",
      phase: "external",
      generation: null,
      startupStage: null,
      nativeShortcuts: null,
      desktopCapabilities: null,
      appVersion: null,
      apiVersion: null,
      schemaVersion: null,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.endsWith("/api/v1/settings/avatar/content")) {
          return new Response(new Uint8Array([137, 80, 78, 71]), {
            headers: { "Content-Type": "image/png" },
          });
        }
        if (url.endsWith("/api/v1/settings/avatar")) {
          const manifest = JSON.parse(
            String((init?.body as FormData).get("manifest")),
          ) as { updates: { key: string; value: Record<string, unknown> }[] };
          const payload = settingsPayload();
          for (const update of manifest.updates) {
            const item = payload.data.items.find(
              (candidate: { key: string }) => candidate.key === update.key,
            )!;
            item.value = update.value as never;
            item.version += 1;
          }
          payload.data.items[0].value.avatar_ref =
            "avatars/018f0000-0000-4000-8000-000000000111.png";
          return new Response(JSON.stringify(payload), {
            headers: { "Content-Type": "application/json" },
          });
        }
        if (url.endsWith("/api/v1/diagnostics/package")) {
          return new Response(new Uint8Array([80, 75, 3, 4]), {
            headers: {
              "Content-Type": "application/zip",
              "Content-Disposition":
                'attachment; filename="opc-workspace-diagnostics-test.zip"',
              "X-Diagnostic-Format-Version": "1",
            },
          });
        }
        return new Response(
          JSON.stringify(
            url.includes("/api/v1/settings")
              ? savedSettingsPayload(init)
              : healthPayload,
          ),
          { headers: { "Content-Type": "application/json" } },
        );
      }),
    );
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: DEFAULT_THEME,
      preview: null,
    });
    useUiStore.setState({ settingsOpen: true, settingsModule: "general" });
    applyTheme(DEFAULT_THEME);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    applyTheme(DEFAULT_THEME);
  });

  it("saves focus settings and closes", async () => {
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

    await waitFor(() =>
      expect(useSettingsStore.getState()).toMatchObject({
        focusMinutes: 55,
        autoStartFocus: true,
        preview: null,
      }),
    );
    expect(useUiStore.getState().settingsOpen).toBe(false);
    const settingsCall = vi
      .mocked(fetch)
      .mock.calls.find(([, init]) => init?.method === "PATCH");
    expect(JSON.parse(String(settingsCall?.[1]?.body))).toEqual({
      updates: [
        {
          key: "focus",
          expected_version: 1,
          value: {
            focus_minutes: 55,
            break_minutes: 5,
            cycles: 4,
            auto_start_break: true,
            auto_start_focus: true,
            sound_enabled: true,
          },
        },
      ],
    });
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

  it("saves the selected appearance theme", async () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "外观" }));
    fireEvent.click(screen.getByRole("radio", { name: /亮色/ }));
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(useSettingsStore.getState().theme).toBe("dark");

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(useSettingsStore.getState().theme).toBe("light"),
    );
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

  it("saves general workspace preferences", async () => {
    renderSettings();

    fireEvent.change(screen.getByLabelText("默认首页"), {
      target: { value: "tasks" },
    });
    fireEvent.click(screen.getByRole("switch", { name: "显示右侧概览" }));
    fireEvent.click(screen.getByRole("switch", { name: "减少动效" }));
    fireEvent.click(
      screen.getByRole("switch", { name: "关闭窗口时隐藏到托盘" }),
    );

    expect(screen.getByTestId("current-location").textContent).toBe("/tasks");
    expect(document.documentElement.dataset.reduceMotion).toBe("true");
    expect(useSettingsStore.getState()).toMatchObject({
      defaultRoute: "today",
      showRightOverview: true,
      reduceMotion: false,
      closeToTray: true,
      preview: {
        general: {
          defaultRoute: "tasks",
          showRightOverview: false,
          reduceMotion: true,
          closeToTray: false,
        },
      },
    });

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(useSettingsStore.getState()).toMatchObject({
        defaultRoute: "tasks",
        showRightOverview: false,
        reduceMotion: true,
        closeToTray: false,
        preview: null,
      }),
    );
    const settingsCall = vi
      .mocked(fetch)
      .mock.calls.find(([, init]) => init?.method === "PATCH");
    expect(JSON.parse(String(settingsCall?.[1]?.body))).toMatchObject({
      updates: [
        {
          key: "general",
          value: { close_to_tray: false },
        },
      ],
    });
  });

  it("previews and saves the profile name", async () => {
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

    await waitFor(() =>
      expect(useSettingsStore.getState()).toMatchObject({
        displayName: "Dingxin Tao",
        preview: null,
      }),
    );
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

  it("persists an uploaded avatar through the controlled endpoint", async () => {
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

    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(useSettingsStore.getState().avatarDataUrl).toMatch(
        /^(?:blob:|data:image\/png;base64,)/,
      ),
    );
    expect(
      vi.mocked(fetch).mock.calls.some(([, init]) => init?.method === "PATCH"),
    ).toBe(false);
    const avatarCall = vi
      .mocked(fetch)
      .mock.calls.find(([url]) =>
        String(url).endsWith("/api/v1/settings/avatar"),
      );
    expect(avatarCall?.[1]?.method).toBe("POST");
    const manifest = JSON.parse(
      String((avatarCall?.[1]?.body as FormData).get("manifest")),
    );
    expect(manifest.operation).toBe("replace");
    expect(manifest.updates[0]).toMatchObject({
      key: "workspace",
      expected_version: 1,
    });
  });

  it("keeps the preview and committed facts separate after a save failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("network down");
      }),
    );
    renderSettings();
    fireEvent.click(screen.getByRole("button", { name: "专注" }));
    fireEvent.click(screen.getByRole("button", { name: "增加专注时长" }));
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("设置未保存");
    expect(useSettingsStore.getState()).toMatchObject({
      focusMinutes: 50,
      preview: { focus: { focusMinutes: 55 } },
    });
    expect(useUiStore.getState().settingsOpen).toBe(true);
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

  it("previews close-to-tray immediately and restores it when cancelled", async () => {
    renderSettings();

    fireEvent.click(
      screen.getByRole("switch", { name: "关闭窗口时隐藏到托盘" }),
    );
    await waitFor(() =>
      expect(desktopApi.setCloseToTrayEnabled).toHaveBeenCalledWith(false),
    );
    expect(useSettingsStore.getState().preview?.general.closeToTray).toBe(
      false,
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    await waitFor(() =>
      expect(desktopApi.setCloseToTrayEnabled.mock.calls).toEqual([
        [false],
        [true],
      ]),
    );
    expect(useSettingsStore.getState().closeToTray).toBe(true);
    expect(useSettingsStore.getState().preview).toBeNull();
  });

  it("shows real health and version facts in the read-only About module", async () => {
    renderSettings();

    expect(screen.getByRole("heading", { name: "通用" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关于" }));

    expect(await screen.findByText("v0.1.0")).toBeVisible();
    expect(screen.getByText("opc-workspace")).toBeVisible();
    expect(screen.getByText("abc123def456-dirty")).toHaveAttribute(
      "title",
      "abc123def456789-dirty",
    );
    expect(screen.getByText("v1")).toBeVisible();
    expect(screen.getByText("v16")).toBeVisible();
    expect(screen.getByText("本地 SQLite · 可用")).toBeVisible();
    expect(screen.getByText("就绪")).toBeVisible();
    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
    expect(
      screen.getByText("关闭", { selector: ".modal-footer button" }),
    ).toBeVisible();
  });

  it("shows sanitized runtime diagnostics in browser development mode", async () => {
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "运行诊断" }));

    expect(await screen.findByText("浏览器开发模式")).toBeVisible();
    expect(screen.getByText("外部开发进程")).toBeVisible();
    expect(screen.getByText("健康检查通过")).toBeVisible();
    expect(screen.getByText("由 HTTP 健康检查确认")).toBeVisible();
    expect(screen.getByText("v0.1.0 · v1")).toBeVisible();
    expect(
      screen.getByText(
        "摘要与诊断包不会包含会话令牌、监听地址、本地路径、底层错误或业务数据；诊断包 v1 只包含版本、平台、数据库健康、迁移清单和系统维护错误码汇总，不包含原始日志。",
      ),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "生成诊断包" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
  });

  it("keeps the active settings module visible in a scrollable navigation", async () => {
    const original = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "scrollIntoView",
    );
    const scrollIntoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    useUiStore.setState({ settingsOpen: true, settingsModule: "diagnostics" });

    try {
      renderSettings();

      await waitFor(() =>
        expect(scrollIntoView).toHaveBeenCalledWith({
          block: "nearest",
          inline: "nearest",
        }),
      );
    } finally {
      if (original) {
        Object.defineProperty(
          HTMLElement.prototype,
          "scrollIntoView",
          original,
        );
      } else {
        Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
      }
    }
  });

  it("labels a restarting desktop lifecycle in Chinese", async () => {
    desktopApi.getRuntimeDiagnostics.mockResolvedValue({
      environment: "desktop",
      phase: "restarting",
      generation: 2,
      nativeShortcuts: {
        commandPalette: "registered",
        newTask: "unavailable",
      },
      desktopCapabilities: {
        tray: "available",
        nativeNotifications: "not_implemented",
        autostart: "not_implemented",
        nativeFileDialogs: "not_implemented",
        offlineUpdates: "not_implemented",
      },
      appVersion: "0.1.0",
      apiVersion: "v1",
      schemaVersion: "16",
    });
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "运行诊断" }));

    expect(await screen.findByText("重启中")).toBeVisible();
    expect(screen.getByText("Tauri 桌面")).toBeVisible();
    expect(screen.getByText("部分不可用；保留应用内快捷键")).toBeVisible();
    expect(screen.getByText("可用 · 关闭窗口时隐藏")).toBeVisible();
  });

  it("shows a retryable About error when the local service is unavailable", async () => {
    const fetchMock = vi.fn(async () => {
      throw new TypeError("network down");
    });
    vi.stubGlobal("fetch", fetchMock);
    useUiStore.setState({ settingsOpen: true, settingsModule: "about" });
    renderSettings();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "本地服务检查失败",
    );
    expect(screen.getByRole("alert")).toHaveTextContent("无法连接本地 Sidecar");
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("opens directly on a requested settings module", () => {
    useUiStore.setState({ settingsOpen: true, settingsModule: "focus" });
    renderSettings();

    expect(screen.getByRole("heading", { name: "专注" })).toBeVisible();
    expect(screen.getByRole("button", { name: "专注" })).toHaveAttribute(
      "data-active",
      "true",
    );
  });

  it("previews and saves the low-space threshold beside real backup facts", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/api/v1/diagnostics/storage/history")) {
          return new Response(
            JSON.stringify({
              data: {
                from: "2026-08-21T12:00:00Z",
                to: "2026-08-28T12:00:00Z",
                points: [
                  {
                    scope: "database+artifacts",
                    checked_at: "2026-08-27T12:00:00Z",
                    available_bytes: 9 * 1024 ** 3,
                    total_bytes: 100 * 1024 ** 3,
                    threshold_bytes: 1024 ** 3,
                    status: "healthy",
                  },
                  {
                    scope: "database+artifacts",
                    checked_at: "2026-08-28T12:00:00Z",
                    available_bytes: 10 * 1024 ** 3,
                    total_bytes: 100 * 1024 ** 3,
                    threshold_bytes: 1024 ** 3,
                    status: "healthy",
                  },
                ],
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.includes("/api/v1/diagnostics/storage")) {
          return new Response(
            JSON.stringify({
              data: {
                checked_at: "2026-08-28T12:00:00Z",
                threshold_gib: 1,
                locations: [
                  {
                    kind: "database",
                    status: "healthy",
                    available_bytes: 10 * 1024 ** 3,
                    total_bytes: 100 * 1024 ** 3,
                    shared_volume: true,
                  },
                  {
                    kind: "artifacts",
                    status: "low",
                    available_bytes: 512 * 1024 ** 2,
                    total_bytes: 100 * 1024 ** 3,
                    shared_volume: true,
                  },
                  {
                    kind: "backups",
                    status: "unavailable",
                    available_bytes: null,
                    total_bytes: null,
                    shared_volume: false,
                  },
                ],
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.endsWith("/api/v1/backups/policy")) {
          return new Response(
            JSON.stringify({
              data: {
                enabled: false,
                local_time: "02:00",
                timezone: "UTC",
                retention_count: 30,
                last_attempted_date: null,
                last_attempt_at: null,
                last_success_at: null,
                last_backup_id: null,
                last_status: "idle",
                last_error_code: null,
                version: 1,
                updated_at: "2026-08-28T12:00:00Z",
                next_run_at: null,
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          );
        }
        if (url.includes("/api/v1/backups")) {
          return new Response(
            JSON.stringify({
              data: [
                {
                  id: "018f0000-0000-7000-8000-000000001701",
                  created_at: "2026-08-28T12:00:00Z",
                  verified_at: "2026-08-28T12:00:02Z",
                  verification_status: "verified",
                  note: "发布前",
                  app_version: "0.1.0",
                  api_version: "v1",
                  schema_version: 28,
                  artifact_count: 0,
                  artifact_bytes: 0,
                  database_bytes: 65536,
                  total_bytes: 65648,
                  kind: "manual",
                },
              ],
            }),
            { headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response(JSON.stringify(savedSettingsPayload(init)), {
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    useUiStore.setState({ settingsOpen: true, settingsModule: "data" });
    renderSettings();

    expect(
      await screen.findByRole("heading", { name: "数据与备份" }),
    ).toBeVisible();
    expect(await screen.findByText("发布前")).toBeVisible();
    expect(
      await screen.findByText("10 GiB 可用 / 100 GiB · 与其他位置同卷"),
    ).toBeVisible();
    expect(
      screen.getByText("空间不足 · 512 MiB 可用 / 100 GiB · 与其他位置同卷"),
    ).toBeVisible();
    expect(screen.getByText("检查不可用")).toBeVisible();
    expect(await screen.findByText("近 7 天容量趋势")).toBeVisible();
    expect(screen.getByText("本地数据库 + 受控文件")).toBeVisible();
    expect(screen.getByText(/10 GiB 可用 · \+1.0 GiB 变化/)).toBeVisible();
    expect(screen.getByRole("button", { name: "重新检查容量" })).toBeVisible();
    expect(screen.getByRole("button", { name: "立即备份" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "增加低空间提醒阈值" }));
    expect(screen.getByText(/当前预览：可用空间低于 2 GiB/)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(useUiStore.getState().settingsOpen).toBe(false));
    const settingsCall = vi
      .mocked(fetch)
      .mock.calls.find(([, init]) => init?.method === "PATCH");
    expect(JSON.parse(String(settingsCall?.[1]?.body))).toEqual({
      updates: [
        {
          key: "storage",
          expected_version: 1,
          value: { low_space_threshold_gib: 2 },
        },
      ],
    });
  });

  it("opens local actor management without implying an online account", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [
                {
                  id: "00000000-0000-5000-8000-000000000001",
                  type: "owner",
                  display_name: "我",
                  status: "active",
                  is_builtin: true,
                  notes: "",
                  metadata: {},
                  version: 1,
                  created_at: "2026-08-27T00:00:00Z",
                  updated_at: "2026-08-27T00:00:00Z",
                },
              ],
              meta: { page: 1, page_size: 100, total: 1 },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    renderSettings();

    fireEvent.click(screen.getByRole("button", { name: "人员与责任" }));

    expect(await screen.findByText("这里只记录责任归属")).toBeTruthy();
    expect(screen.getByText("人员资料通过模块内按钮单独保存")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "保存" })).toBeNull();
  });
});
