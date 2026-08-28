import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getAppSetting,
  getAppSettings,
  normalizeAppSettingsResponse,
  updateAppSettings,
} from "./client";

const validPayload = (): any => ({
  data: {
    schema_version: 1,
    items: [
      {
        key: "workspace",
        value: { display_name: "opc-workspace", avatar_ref: null },
        schema_version: 1,
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
        },
        schema_version: 1,
        version: 2,
        stored: true,
        updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
        updated_at: "2026-08-28T12:00:00Z",
      },
      {
        key: "appearance",
        value: { theme: "dark" },
        schema_version: 1,
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
        schema_version: 1,
        version: 1,
        stored: true,
        updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
        updated_at: "2026-08-28T12:00:00Z",
      },
    ],
  },
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("settings API", () => {
  it("strictly normalizes all four settings modules", () => {
    const settings = normalizeAppSettingsResponse(validPayload());

    expect(settings.schemaVersion).toBe(1);
    expect(settings.items.map((item) => item.key)).toEqual([
      "workspace",
      "general",
      "appearance",
      "focus",
    ]);
    expect(getAppSetting(settings, "general")).toMatchObject({
      stored: true,
      version: 2,
      value: {
        defaultRoute: "today",
        showRightOverview: true,
        reduceMotion: false,
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
          ],
        });
      }
      return new Response(JSON.stringify(validPayload()), {
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(getAppSettings()).resolves.toMatchObject({ schemaVersion: 1 });
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
      ]),
    ).resolves.toMatchObject({ schemaVersion: 1 });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
