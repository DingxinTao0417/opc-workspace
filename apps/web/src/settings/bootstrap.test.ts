import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { normalizeAppSettingsResponse } from "../api/client";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  LEGACY_SETTINGS_STORAGE_KEY,
  LOCAL_SETTINGS_STORAGE_KEY,
  useSettingsStore,
  type LegacySettingsSnapshot,
} from "../store/settings";
import {
  bootstrapAppSettings,
  committedSettingsFromServer,
  legacySettingUpdates,
} from "./bootstrap";

function testStorage(): Storage {
  const values = new Map<string, string>();
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

function payload(storedKeys: string[] = []): any {
  const stored = (key: string) => storedKeys.includes(key);
  const meta = (key: string) =>
    stored(key)
      ? {
          version: 1,
          stored: true,
          updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
          updated_at: "2026-08-28T12:00:00Z",
        }
      : {
          version: 0,
          stored: false,
          updated_by_actor_id: null,
          updated_at: null,
        };
  return {
    data: {
      schema_version: 1,
      items: [
        {
          key: "workspace",
          value: { display_name: "Server Workspace", avatar_ref: null },
          schema_version: 1,
          ...meta("workspace"),
        },
        {
          key: "general",
          value: {
            default_route: "today",
            show_right_overview: true,
            reduce_motion: false,
          },
          schema_version: 1,
          ...meta("general"),
        },
        {
          key: "appearance",
          value: { theme: "dark" },
          schema_version: 1,
          ...meta("appearance"),
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
          ...meta("focus"),
        },
        {
          key: "storage",
          value: { low_space_threshold_gib: 1 },
          schema_version: 1,
          ...meta("storage"),
        },
      ],
    },
  };
}

const legacy: LegacySettingsSnapshot = {
  exists: true,
  focus: { ...DEFAULT_FOCUS_SETTINGS, focusMinutes: 25 },
  general: { ...DEFAULT_GENERAL_SETTINGS, defaultRoute: "projects" },
  profile: {
    displayName: "Legacy Workspace",
    avatarDataUrl: "data:image/png;base64,AA==",
  },
  theme: "light",
};

describe("settings bootstrap", () => {
  beforeEach(() => {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: testStorage(),
    });
    useSettingsStore.setState({
      ...DEFAULT_FOCUS_SETTINGS,
      ...DEFAULT_GENERAL_SETTINGS,
      ...DEFAULT_PROFILE_SETTINGS,
      theme: "dark",
      preview: null,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("migrates only modules that have no server fact", () => {
    const settings = normalizeAppSettingsResponse(payload(["general"]));
    expect(legacySettingUpdates(settings, legacy)).toEqual([
      {
        key: "workspace",
        expectedVersion: 0,
        value: { displayName: "Legacy Workspace", avatarRef: null },
      },
      {
        key: "appearance",
        expectedVersion: 0,
        value: { theme: "light" },
      },
      {
        key: "focus",
        expectedVersion: 0,
        value: { ...DEFAULT_FOCUS_SETTINGS, focusMinutes: 25 },
      },
    ]);
  });

  it("maps server facts while keeping only the local avatar compatibility value", () => {
    const settings = normalizeAppSettingsResponse(
      payload(["workspace", "general", "appearance", "focus"]),
    );
    expect(
      committedSettingsFromServer(settings, "data:image/png;base64,LOCAL=="),
    ).toEqual({
      profile: {
        displayName: "Server Workspace",
        avatarDataUrl: "data:image/png;base64,LOCAL==",
      },
      general: DEFAULT_GENERAL_SETTINGS,
      theme: "dark",
      focus: DEFAULT_FOCUS_SETTINGS,
    });
  });

  it("atomically migrates the historical localStorage envelope", async () => {
    window.localStorage.setItem(
      LEGACY_SETTINGS_STORAGE_KEY,
      JSON.stringify({
        state: {
          focusMinutes: 25,
          breakMinutes: 5,
          cycles: 4,
          autoStartBreak: true,
          autoStartFocus: false,
          soundEnabled: true,
          defaultRoute: "projects",
          showRightOverview: true,
          reduceMotion: false,
          displayName: "Legacy Workspace",
          avatarDataUrl: "data:image/png;base64,AA==",
          theme: "light",
        },
      }),
    );
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify(payload()), {
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockImplementationOnce(async (_url: string, init: RequestInit) => {
        const manifest = JSON.parse(
          String((init.body as FormData).get("manifest")),
        );
        expect(manifest.operation).toBe("replace");
        expect(
          manifest.updates.map((item: { key: string }) => item.key),
        ).toEqual(["workspace", "general", "appearance", "focus"]);
        expect(manifest.updates[0].value).toEqual({
          display_name: "Legacy Workspace",
          avatar_ref: null,
        });
        const migrated = payload([
          "workspace",
          "general",
          "appearance",
          "focus",
        ]);
        migrated.data.items[0].value = {
          display_name: "Legacy Workspace",
          avatar_ref: "avatars/018f0000-0000-4000-8000-000000000222.png",
        };
        migrated.data.items[1].value = {
          default_route: "projects",
          show_right_overview: true,
          reduce_motion: false,
        };
        migrated.data.items[2].value = { theme: "light" };
        migrated.data.items[3].value = {
          focus_minutes: 25,
          break_minutes: 5,
          cycles: 4,
          auto_start_break: true,
          auto_start_focus: false,
          sound_enabled: true,
        };
        return new Response(JSON.stringify(migrated), {
          headers: { "Content-Type": "application/json" },
        });
      })
      .mockResolvedValueOnce(
        new Response(new Uint8Array([137, 80, 78, 71]), {
          headers: { "Content-Type": "image/png" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const result = await bootstrapAppSettings();

    expect(result.legacyExists).toBe(true);
    expect(result.migratedKeys).toEqual([
      "workspace",
      "general",
      "appearance",
      "focus",
    ]);
    expect(result.committed.profile).toEqual({
      displayName: "Legacy Workspace",
      avatarDataUrl: expect.stringMatching(
        /^(?:blob:|data:image\/png;base64,)/,
      ),
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("does not resurrect a legacy avatar after the new local snapshot cleared it", async () => {
    window.localStorage.setItem(
      LEGACY_SETTINGS_STORAGE_KEY,
      JSON.stringify({
        state: { avatarDataUrl: "data:image/png;base64,OLD==" },
      }),
    );
    window.localStorage.setItem(
      LOCAL_SETTINGS_STORAGE_KEY,
      JSON.stringify({ state: { avatarDataUrl: null }, version: 0 }),
    );
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify(
              payload(["workspace", "general", "appearance", "focus"]),
            ),
            { headers: { "Content-Type": "application/json" } },
          ),
        ),
    );

    const result = await bootstrapAppSettings();

    expect(result.committed.profile.avatarDataUrl).toBeNull();
    expect(result.migratedKeys).toEqual([]);
  });
});
