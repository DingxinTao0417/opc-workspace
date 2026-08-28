import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_FOCUS_SETTINGS,
  DEFAULT_GENERAL_SETTINGS,
  DEFAULT_PROFILE_SETTINGS,
  useSettingsStore,
} from "../store/settings";
import { SettingsBootstrap } from "./SettingsBootstrap";

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

function storedSettingsPayload() {
  const metadata = {
    schema_version: 1,
    version: 1,
    stored: true,
    updated_by_actor_id: "00000000-0000-5000-8000-000000000001",
    updated_at: "2026-08-28T12:00:00Z",
  };
  return {
    data: {
      schema_version: 1,
      items: [
        {
          key: "workspace",
          value: { display_name: "Loaded Workspace", avatar_ref: null },
          ...metadata,
        },
        {
          key: "general",
          value: {
            default_route: "projects",
            show_right_overview: false,
            reduce_motion: true,
          },
          ...metadata,
        },
        {
          key: "appearance",
          value: { theme: "light" },
          ...metadata,
        },
        {
          key: "focus",
          value: {
            focus_minutes: 25,
            break_minutes: 5,
            cycles: 3,
            auto_start_break: true,
            auto_start_focus: false,
            sound_enabled: true,
          },
          ...metadata,
        },
      ],
    },
  };
}

function renderBootstrap() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsBootstrap>
        <div>应用已就绪</div>
      </SettingsBootstrap>
    </QueryClientProvider>,
  );
}

describe("SettingsBootstrap", () => {
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
    cleanup();
    vi.unstubAllGlobals();
  });

  it("gates the app until server settings are applied", async () => {
    let resolveResponse!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            resolveResponse = resolve;
          }),
      ),
    );
    renderBootstrap();
    expect(screen.getByRole("status")).toHaveTextContent("正在读取本地设置");
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    resolveResponse(
      new Response(JSON.stringify(storedSettingsPayload()), {
        headers: { "Content-Type": "application/json" },
      }),
    );

    expect(await screen.findByText("应用已就绪")).toBeVisible();
    expect(useSettingsStore.getState()).toMatchObject({
      displayName: "Loaded Workspace",
      defaultRoute: "projects",
      showRightOverview: false,
      reduceMotion: true,
      focusMinutes: 25,
      cycles: 3,
      theme: "light",
    });
  });

  it("shows a retryable startup error without inventing settings", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(
        new Response(JSON.stringify(storedSettingsPayload()), {
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    renderBootstrap();

    expect(await screen.findByRole("button", { name: "重试" })).toBeVisible();
    expect(screen.queryByText("应用已就绪")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("应用已就绪")).toBeVisible();
  });
});
