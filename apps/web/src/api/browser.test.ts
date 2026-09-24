import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  acquireBrowserSurface,
  nativeBrowser,
  normalizeBrowserAddress,
} from "./browser";

const mocks = vi.hoisted(() => ({
  invoke: vi.fn(),
  desktop: vi.fn(() => true),
  listen: vi.fn(),
}));
vi.mock("./desktop", () => ({ isDesktopRuntime: mocks.desktop }));
vi.mock("@tauri-apps/api/core", () => ({ invoke: mocks.invoke }));
vi.mock("@tauri-apps/api/event", () => ({ listen: mocks.listen }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.desktop.mockReturnValue(true);
  mocks.invoke.mockResolvedValue(undefined);
});
afterEach(() => vi.restoreAllMocks());

describe("browser addresses", () => {
  it.each([
    ["example.com/docs", "https://example.com/docs"],
    ["localhost:5173", "http://localhost:5173/"],
    ["127.0.0.1:5173/path", "http://127.0.0.1:5173/path"],
    ["[::1]:5173", "http://[::1]:5173/"],
    ["example.com:8080", "https://example.com:8080/"],
    [
      " https://example.com/path?q=test#heading ",
      "https://example.com/path?q=test#heading",
    ],
    ["", "about:blank"],
  ])("normalizes %s", (input, expected) =>
    expect(normalizeBrowserAddress(input)).toBe(expected),
  );

  it.each([
    "javascript:alert(1)",
    "data:text/html,test",
    "file:///C:/secret.txt",
    "tauri://localhost",
    "https://user:pass@example.com",
    "https://",
    "hello world",
    "https://exam\nple.com",
  ])("rejects %s before opening anything", (input) =>
    expect(() => normalizeBrowserAddress(input)).toThrow(),
  );
});

describe("native browser commands", () => {
  it("subscribes to non-fatal notices separately from page snapshots", async () => {
    const onNotice = vi.fn();
    const unlisten = vi.fn();
    mocks.listen.mockResolvedValueOnce(unlisten);
    const cleanup = await nativeBrowser.subscribeNotices(onNotice);
    const [name, callback] = mocks.listen.mock.calls[0];
    expect(name).toBe("browser-notice");
    const payload = { tabId: "one", message: "BROWSER_TAB_LIMIT" };
    callback({ payload });
    expect(onNotice).toHaveBeenCalledWith(payload);
    cleanup();
    expect(unlisten).toHaveBeenCalledOnce();
    expect(mocks.invoke).not.toHaveBeenCalled();
  });

  it("routes each action through the dedicated typed command", async () => {
    await nativeBrowser.createTab();
    await nativeBrowser.navigate("tab-1", "https://example.com/");
    await nativeBrowser.activateTab("tab-1");
    await nativeBrowser.action("tab-1", "back");
    await nativeBrowser.capturePageText("tab-1");
    await nativeBrowser.closeTab("tab-1");
    expect(mocks.invoke.mock.calls).toEqual([
      ["browser_create_tab", { url: "about:blank" }],
      ["browser_navigate", { tabId: "tab-1", url: "https://example.com/" }],
      ["browser_activate_tab", { tabId: "tab-1" }],
      ["browser_action", { tabId: "tab-1", action: "back" }],
      ["browser_capture_page_text", { tabId: "tab-1" }],
      ["browser_close_tab", { tabId: "tab-1" }],
    ]);
  });

  it("does not invoke native APIs in web development", async () => {
    mocks.desktop.mockReturnValue(false);
    await expect(nativeBrowser.snapshot()).rejects.toThrow(
      "BROWSER_DESKTOP_REQUIRED",
    );
    expect(mocks.invoke).not.toHaveBeenCalled();
  });

  it("discards stale geometry and cleanup from a replaced surface owner", async () => {
    const old = acquireBrowserSurface();
    const stale = old.update({ x: 1, y: 2, width: 3, height: 4 });
    const cleanup = old.release();
    const current = acquireBrowserSurface();
    const bounds = { x: 100, y: 90, width: 680, height: 600 };
    await current.update(bounds);
    await Promise.all([stale, cleanup]);
    expect(mocks.invoke.mock.calls).toEqual([
      ["browser_set_layout", { bounds }],
    ]);
    await current.release();
    expect(mocks.invoke).toHaveBeenLastCalledWith("browser_set_layout", {
      bounds: null,
    });
  });

  it("serializes geometry and still releases after a failed update", async () => {
    const surface = acquireBrowserSurface();
    mocks.invoke.mockRejectedValueOnce(new Error("native unavailable"));
    await expect(
      surface.update({ x: 0, y: 0, width: 300, height: 500 }),
    ).rejects.toThrow();
    await surface.release();
    expect(mocks.invoke).toHaveBeenLastCalledWith("browser_set_layout", {
      bounds: null,
    });
  });

  it("hides immediately while a previous layout is pending and drops queued stale shows", async () => {
    const surface = acquireBrowserSurface();
    const first = { x: 0, y: 0, width: 300, height: 500 };
    const stale = { ...first, width: 350 };
    const latest = { ...first, width: 400 };
    let completeLayout!: () => void;
    let signalInvoked!: () => void;
    const invoked = new Promise<void>((resolve) => {
      signalInvoked = resolve;
    });
    mocks.invoke.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          completeLayout = resolve;
          signalInvoked();
        }),
    );
    const pending = surface.update(first);
    await invoked;
    const queued = surface.update(stale);
    await surface.update(null);
    expect(mocks.invoke.mock.calls).toEqual([
      ["browser_set_layout", { bounds: first }],
      ["browser_set_layout", { bounds: null }],
    ]);
    const restored = surface.update(latest);
    completeLayout();
    await Promise.all([pending, queued, restored]);
    expect(mocks.invoke.mock.calls).toEqual([
      ["browser_set_layout", { bounds: first }],
      ["browser_set_layout", { bounds: null }],
      ["browser_set_layout", { bounds: latest }],
    ]);
    await surface.release();
  });

  it("releases immediately without waiting for an outstanding native layout", async () => {
    const surface = acquireBrowserSurface();
    let completeLayout!: () => void;
    let signalInvoked!: () => void;
    const invoked = new Promise<void>((resolve) => {
      signalInvoked = resolve;
    });
    mocks.invoke.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          completeLayout = resolve;
          signalInvoked();
        }),
    );
    const pending = surface.update({ x: 0, y: 0, width: 300, height: 500 });
    await invoked;
    await surface.release();
    expect(mocks.invoke).toHaveBeenLastCalledWith("browser_set_layout", {
      bounds: null,
    });
    completeLayout();
    await pending;
  });
});
