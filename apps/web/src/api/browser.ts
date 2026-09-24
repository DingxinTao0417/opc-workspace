import { isDesktopRuntime } from "./desktop";

export interface BrowserTabState {
  id: string;
  url: string;
  title: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
  error: string | null;
}

export interface BrowserSnapshot {
  tabs: BrowserTabState[];
  activeTabId: string | null;
  revision: number;
}

export interface BrowserPageTextCapture {
  tabId: string;
  url: string;
  title: string;
  text: string;
  truncated: boolean;
  links: BrowserPageLink[];
  linksTruncated: boolean;
}

export interface BrowserPageLink {
  label: string;
  url: string;
}

export interface BrowserBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface BrowserNotice {
  tabId: string;
  message: string;
}

export type BrowserAction = "back" | "forward" | "reload" | "stop";
export const BROWSER_TAB_LIMIT = 12;

/** Address-bar convenience only. The native host independently validates URLs. */
export function normalizeBrowserAddress(input: string): string {
  const raw = input.trim();
  if (!raw || raw === "about:blank") return "about:blank";
  if (raw.length > 8192 || /[\u0000-\u001f\u007f]/.test(raw)) {
    throw new Error("请输入有效的 HTTP 或 HTTPS 网址");
  }
  let candidate = raw;
  if (!/^https?:\/\//i.test(raw)) {
    // A hostname:port is not a URI scheme. Other explicit schemes stay rejected.
    const local =
      /^(localhost|127(?:\.\d{1,3}){3}|\[::1\])(?::\d+)?(?:[/?#]|$)/i.test(raw);
    if (
      /^[a-z][a-z\d+.-]*:/i.test(raw) &&
      !local &&
      !/^[^/?#:]+:\d+(?:[/?#]|$)/.test(raw)
    ) {
      throw new Error("仅支持 HTTP 或 HTTPS 网页");
    }
    candidate = `${local ? "http" : "https"}://${raw}`;
  }
  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    throw new Error("请输入有效的 HTTP 或 HTTPS 网址");
  }
  if (
    !["http:", "https:"].includes(url.protocol) ||
    !url.hostname ||
    url.username ||
    url.password
  ) {
    throw new Error("仅支持不含用户名或密码的 HTTP 或 HTTPS 网址");
  }
  return url.href;
}

export function isEmbeddedBrowserRuntime(): boolean {
  return isDesktopRuntime();
}

async function command<T>(
  name: string,
  args?: Record<string, unknown>,
): Promise<T> {
  if (!isEmbeddedBrowserRuntime()) throw new Error("BROWSER_DESKTOP_REQUIRED");
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke<T>(name, args);
}

export const nativeBrowser = {
  snapshot: () => command<BrowserSnapshot>("browser_snapshot"),
  createTab: (url = "about:blank") =>
    command<BrowserSnapshot>("browser_create_tab", { url }),
  activateTab: (tabId: string) =>
    command<BrowserSnapshot>("browser_activate_tab", { tabId }),
  closeTab: (tabId: string) =>
    command<BrowserSnapshot>("browser_close_tab", { tabId }),
  navigate: (tabId: string, url: string) =>
    command<BrowserSnapshot>("browser_navigate", { tabId, url }),
  action: (tabId: string, action: BrowserAction) =>
    command<BrowserSnapshot>("browser_action", { tabId, action }),
  capturePageText: (tabId: string) =>
    command<BrowserPageTextCapture>("browser_capture_page_text", { tabId }),
  async subscribe(
    onSnapshot: (snapshot: BrowserSnapshot) => void,
  ): Promise<() => void> {
    const { listen } = await import("@tauri-apps/api/event");
    return listen<BrowserSnapshot>("browser-state-changed", (event) =>
      onSnapshot(event.payload),
    );
  },
  async subscribeNotices(
    onNotice: (notice: BrowserNotice) => void,
  ): Promise<() => void> {
    const { listen } = await import("@tauri-apps/api/event");
    return listen<BrowserNotice>("browser-notice", (event) =>
      onNotice(event.payload),
    );
  },
};

// Child webviews are native siblings above the DOM. Hiding must bypass a slow
// geometry request; the epoch invalidates queued shows, and the owner lease
// prevents delayed StrictMode/unmount cleanup from hiding a newer pane.
let surfaceOwner: symbol | null = null;
let layoutEpoch = 0;
let layoutQueue: Promise<void> = Promise.resolve();
let latestHide: Promise<void> = Promise.resolve();

async function dispatchLayout(
  bounds: BrowserBounds | null,
  current: () => boolean,
) {
  if (!current()) return;
  if (!isEmbeddedBrowserRuntime()) throw new Error("BROWSER_DESKTOP_REQUIRED");
  const { invoke } = await import("@tauri-apps/api/core");
  // Ownership may change while the native API module is loading.
  if (!current()) return;
  await invoke<void>("browser_set_layout", { bounds });
}

export function acquireBrowserSurface() {
  const owner = Symbol("browser-surface");
  surfaceOwner = owner;
  layoutEpoch++;
  const hide = (releasing: boolean) => {
    const epoch = ++layoutEpoch;
    const task = dispatchLayout(
      null,
      () =>
        layoutEpoch === epoch &&
        (releasing ? surfaceOwner === null : surfaceOwner === owner),
    );
    latestHide = task;
    return task;
  };
  return {
    update(bounds: BrowserBounds | null) {
      if (surfaceOwner !== owner) return Promise.resolve();
      if (bounds === null) return hide(false);
      const epoch = layoutEpoch;
      const hideBarrier = latestHide;
      const task = layoutQueue
        .catch(() => undefined)
        .then(async () => {
          // A later show must follow the preceding immediate hide even when
          // dynamic-import completion or IPC response ordering differs.
          await hideBarrier.catch(() => undefined);
          await dispatchLayout(
            bounds,
            () => surfaceOwner === owner && layoutEpoch === epoch,
          );
        });
      layoutQueue = task;
      return task;
    },
    release() {
      if (surfaceOwner !== owner) return Promise.resolve();
      surfaceOwner = null;
      return hide(true);
    },
  };
}

export function browserErrorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (/BROWSER_CAPTURE_EMPTY/i.test(message))
    return "当前页面没有可分享的可见正文或链接。";
  if (/BROWSER_PAGE_UNAVAILABLE/i.test(message))
    return "请等当前网页加载完成后，再分享页面内容。";
  if (/BROWSER_PAGE_CHANGED/i.test(message))
    return "活动网页已变化，请核对当前页面后重新分享内容。";
  if (/BROWSER_TARGET_CHANGED/i.test(message))
    return "活动标签已变化，请核对当前页面后重新确认。";
  if (/BROWSER_CAPTURE_TIMEOUT/i.test(message))
    return "读取网页内容超时；网页未发送给 AI，可以稍后重试。";
  if (/BROWSER_CAPTURE_FAILED/i.test(message))
    return "无法安全读取网页内容；网页未发送给 AI。";
  if (/BROWSER_ACTION_UNAVAILABLE/i.test(message))
    return "当前标签已不能执行该操作，请核对页面状态。";
  if (/UNSUPPORTED|DESKTOP_REQUIRED/i.test(message))
    return "内嵌浏览器需要 Windows 桌面版和 WebView2。";
  if (/BROWSER_RUNTIME/i.test(message))
    return "请将 WebView2 Runtime 升级到 122.0.2365.46 或更新版本后重试。";
  if (/TAB_LIMIT/i.test(message))
    return `最多打开 ${BROWSER_TAB_LIMIT} 个标签，请先关闭一个。`;
  if (/URL|SCHEME|NAVIGATION_BLOCKED/i.test(message))
    return "此地址无法在内嵌浏览器中打开，请使用不含账号密码的 HTTP 或 HTTPS 网址。";
  if (/TIMEOUT/i.test(message)) return "网页加载超时，可以重新加载或更换地址。";
  if (/LOAD|NAVIGATION_FAILED/i.test(message))
    return "网页加载失败，请检查地址或网络后重试。";
  return "浏览器操作失败，请重试。";
}
