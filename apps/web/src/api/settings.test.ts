import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  commitAppSettingsWithAvatar,
  getAppSetting,
  getAppSettings,
  getStorageCapacity,
  getStorageCapacityHistory,
  getWorkspaceAvatarBlob,
  normalizeAppSettingsResponse,
  updateAppSettings,
} from "./client";

const validPayload = (): any => ({
  data: {
    schema_version: 2,
    items: [
      {
        key: "workspace",
        value: { display_name: "opc-workspace", avatar_ref: null },
        schema_version: 2,
        version: 0,
        stored: false,
        updated_by_actor_id: null,
        updated_at: null,
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
        version: 2,
        stored: true,
        updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
        updated_at: "2026-08-28T12:00:00Z",
      },
      {
        key: "appearance",
        value: { theme: "dark" },
        schema_version: 2,
        version: 0,
        stored: false,
        updated_by_actor_id: null,
        updated_at: null,
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
        version: 0,
        stored: false,
        updated_by_actor_id: null,
        updated_at: null,
      },
    ],
  },
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("settings API", () => {
  it("strictly reads pathless storage capacity diagnostics", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: {
                checked_at: "2026-08-28T12:00:00Z",
                threshold_gib: 5,
                locations: [
                  {
                    kind: "database",
                    status: "healthy",
                    available_bytes: 10,
                    total_bytes: 20,
                    shared_volume: true,
                  },
                  {
                    kind: "artifacts",
                    status: "low",
                    available_bytes: 2,
                    total_bytes: 20,
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
          ),
      ),
    );
    await expect(getStorageCapacity()).resolves.toMatchObject({
      thresholdGiB: 5,
      locations: [
        {
          kind: "database",
          status: "healthy",
          availableBytes: 10,
          sharedVolume: true,
        },
        {
          kind: "artifacts",
          status: "low",
          availableBytes: 2,
          sharedVolume: true,
        },
        {
          kind: "backups",
          status: "unavailable",
          availableBytes: null,
          sharedVolume: false,
        },
      ],
    });
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/api/v1/diagnostics/storage/check"),
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("strictly reads chronological pathless storage history", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: {
                from: "2026-08-22T12:00:00Z",
                to: "2026-08-29T12:00:00Z",
                points: [
                  {
                    scope: "database+artifacts",
                    checked_at: "2026-08-28T12:00:00Z",
                    available_bytes: 10,
                    total_bytes: 20,
                    threshold_bytes: 5,
                    status: "healthy",
                  },
                  {
                    scope: "backups",
                    checked_at: "2026-08-29T12:00:00Z",
                    available_bytes: 2,
                    total_bytes: 20,
                    threshold_bytes: 5,
                    status: "low",
                  },
                ],
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    await expect(getStorageCapacityHistory()).resolves.toMatchObject({
      points: [
        { scope: "database+artifacts", availableBytes: 10 },
        { scope: "backups", status: "low" },
      ],
    });
  });

  it("rejects storage history with private scope or reversed time", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: {
                from: "2026-08-22T12:00:00Z",
                to: "2026-08-29T12:00:00Z",
                points: [
                  {
                    scope: "private-volume-id",
                    checked_at: "2026-08-29T12:00:00Z",
                    available_bytes: 2,
                    total_bytes: 20,
                    threshold_bytes: 5,
                    status: "low",
                  },
                ],
              },
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    await expect(getStorageCapacityHistory()).rejects.toThrow(
      "存储容量趋势点响应无效",
    );
  });

  it("strictly normalizes all five settings modules", () => {
    const settings = normalizeAppSettingsResponse(validPayload());

    expect(settings.schemaVersion).toBe(2);
    expect(settings.items.map((item) => item.key)).toEqual([
      "workspace",
      "general",
      "appearance",
      "focus",
      "storage",
    ]);
    expect(getAppSetting(settings, "general")).toMatchObject({
      stored: true,
      version: 2,
      value: {
        defaultRoute: "today",
        showRightOverview: true,
        reduceMotion: false,
        closeToTray: true,
      },
    });
    expect(getAppSetting(settings, "workspace")).toMatchObject({
      stored: false,
      version: 0,
      value: { displayName: "opc-workspace", avatarRef: null },
    });
  });

  it.each([
    [
      "wrong module order",
      (payload: ReturnType<typeof validPayload>) => {
        [payload.data.items[0], payload.data.items[1]] = [
          payload.data.items[1],
          payload.data.items[0],
        ];
      },
    ],
    [
      "unknown value field",
      (payload: ReturnType<typeof validPayload>) => {
        Object.assign(payload.data.items[2].value, { token: "secret" });
      },
    ],
    [
      "unstored version",
      (payload: ReturnType<typeof validPayload>) => {
        payload.data.items[0].version = 1;
      },
    ],
    [
      "stored metadata",
      (payload: ReturnType<typeof validPayload>) => {
        payload.data.items[1].updated_at = null;
      },
    ],
    [
      "data URL avatar",
      (payload: ReturnType<typeof validPayload>) => {
        payload.data.items[0].value = {
          display_name: "opc-workspace",
          avatar_ref: "data:image/png;base64,secret",
        };
      },
    ],
    [
      "focus bound",
      (payload: ReturnType<typeof validPayload>) => {
        payload.data.items[3].value = {
          ...payload.data.items[3].value,
          focus_minutes: 3,
        };
      },
    ],
  ])("rejects %s", (_name, mutate) => {
    const payload = validPayload();
    mutate(payload);
    expect(() => normalizeAppSettingsResponse(payload)).toThrowError(ApiError);
  });

  it("reads and serializes an atomic settings update", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PATCH") {
        expect(JSON.parse(String(init.body))).toEqual({
          updates: [
            {
              key: "appearance",
              expected_version: 0,
              value: { theme: "light" },
            },
            {
              key: "focus",
              expected_version: 1,
              value: {
                focus_minutes: 25,
                break_minutes: 5,
                cycles: 3,
                auto_start_break: true,
                auto_start_focus: false,
                sound_enabled: true,
              },
            },
            {
              key: "storage",
              expected_version: 0,
              value: { low_space_threshold_gib: 5 },
            },
          ],
        });
      }
      return new Response(JSON.stringify(validPayload()), {
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(getAppSettings()).resolves.toMatchObject({ schemaVersion: 2 });
    await expect(
      updateAppSettings([
        { key: "appearance", expectedVersion: 0, value: { theme: "light" } },
        {
          key: "focus",
          expectedVersion: 1,
          value: {
            focusMinutes: 25,
            breakMinutes: 5,
            cycles: 3,
            autoStartBreak: true,
            autoStartFocus: false,
            soundEnabled: true,
          },
        },
        {
          key: "storage",
          expectedVersion: 0,
          value: { lowSpaceThresholdGiB: 5 },
        },
      ]),
    ).resolves.toMatchObject({ schemaVersion: 2 });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("commits and reads a controlled workspace avatar", async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/content")) {
        return new Response(new Uint8Array([137, 80, 78, 71]), {
          headers: { "Content-Type": "image/png" },
        });
      }
      expect(init?.method).toBe("POST");
      expect(init?.body).toBeInstanceOf(FormData);
      const form = init?.body as FormData;
      const manifest = JSON.parse(String(form.get("manifest")));
      expect(manifest).toEqual({
        operation: "replace",
        updates: [
          {
            key: "workspace",
            expected_version: 0,
            value: { display_name: "Workspace", avatar_ref: null },
          },
        ],
      });
      expect(form.get("file")).toBeInstanceOf(File);
      return new Response(JSON.stringify(validPayload()), {
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);
    const file = new File([new Uint8Array([137, 80, 78, 71])], "avatar.png", {
      type: "image/png",
    });
    await expect(
      commitAppSettingsWithAvatar(
        "replace",
        [
          {
            key: "workspace",
            expectedVersion: 0,
            value: { displayName: "Workspace", avatarRef: null },
          },
        ],
        file,
      ),
    ).resolves.toMatchObject({ schemaVersion: 2 });
    await expect(getWorkspaceAvatarBlob()).resolves.toMatchObject({
      type: "image/png",
      size: 4,
    });
  });
});
