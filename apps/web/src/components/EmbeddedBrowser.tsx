import {
  ArrowLeft,
  ArrowRight,
  ExternalLink,
  FileText,
  Globe,
  LoaderCircle,
  Plus,
  RotateCw,
  Sparkles,
  X,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { KeyboardEvent, RefObject } from "react";
import {
  acquireBrowserSurface,
  BROWSER_TAB_LIMIT,
  browserErrorMessage,
  isEmbeddedBrowserRuntime,
  nativeBrowser,
  normalizeBrowserAddress,
} from "../api/browser";
import type {
  BrowserAction,
  BrowserBounds,
  BrowserNotice,
  BrowserPageTextCapture,
  BrowserSnapshot,
} from "../api/browser";
import {
  browserActionOutcomeHandoff,
  browserAddressHandoff,
  browserPageTextHandoff,
} from "../lib/aiIssueHandoff";
import { revealAgentChat } from "../lib/revealAgentChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useUiStore } from "../store/ui";
import "../browser.css";

const browserActionLabels: Record<BrowserAction, string> = {
  back: "后退",
  forward: "前进",
  reload: "重新加载",
  stop: "停止加载",
};

function elementVisible(element: Element): boolean {
  if (element.closest('[hidden], [aria-hidden="true"]')) return false;
  const style = window.getComputedStyle(element);
  return (
    style.display !== "none" &&
    style.visibility !== "hidden" &&
    style.visibility !== "collapse" &&
    element.getClientRects().length > 0
  );
}

function hasBlockingOverlay(): boolean {
  return Array.from(
    document.querySelectorAll(
      '[data-overlay-root="true"], [role="dialog"], [aria-modal="true"], dialog[open]',
    ),
  ).some(elementVisible);
}

function useNativeSurface(
  ref: RefObject<HTMLDivElement>,
  visible: boolean,
  tabId: string | null,
  onError: (error: unknown) => void,
) {
  useLayoutEffect(() => {
    if (!isEmbeddedBrowserRuntime()) return;
    const lease = acquireBrowserSurface();
    let disposed = false;
    let frame = 0;
    let previous: string | undefined;
    const measure = () => {
      frame = 0;
      if (disposed) return;
      const element = ref.current;
      let bounds: BrowserBounds | null = null;
      if (
        visible &&
        element &&
        document.visibilityState !== "hidden" &&
        elementVisible(element) &&
        !element.closest(".app-shell-resizing") &&
        !hasBlockingOverlay()
      ) {
        const rect = element.getBoundingClientRect();
        const x = Math.max(0, rect.left);
        const y = Math.max(0, rect.top);
        const width = Math.min(window.innerWidth, rect.right) - x;
        const height = Math.min(window.innerHeight, rect.bottom) - y;
        if (width > 0 && height > 0) bounds = { x, y, width, height };
      }
      const next = JSON.stringify(bounds);
      if (previous === next) return;
      previous = next;
      void lease.update(bounds).catch((error) => {
        if (!disposed) {
          previous = undefined;
          onError(error);
        }
      });
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(measure);
    };
    const resize =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(schedule);
    if (ref.current) resize?.observe(ref.current);
    // Native surfaces do not participate in CSS stacking. Hide them for modal
    // portals, sidebar transitions and splitter drags, then restore their rect.
    const mutations = new MutationObserver(schedule);
    mutations.observe(document.body, {
      attributes: true,
      childList: true,
      subtree: true,
      attributeFilter: [
        "class",
        "style",
        "hidden",
        "aria-hidden",
        "aria-modal",
        "open",
      ],
    });
    window.addEventListener("resize", schedule);
    window.addEventListener("scroll", schedule, true);
    document.addEventListener("visibilitychange", schedule);
    measure();
    return () => {
      disposed = true;
      if (frame) window.cancelAnimationFrame(frame);
      resize?.disconnect();
      mutations.disconnect();
      window.removeEventListener("resize", schedule);
      window.removeEventListener("scroll", schedule, true);
      document.removeEventListener("visibilitychange", schedule);
      void lease.release().catch(() => undefined);
    };
  }, [ref, visible, tabId, onError]);
}

export function EmbeddedBrowser({ hideTabs = false }: { hideTabs?: boolean }) {
  const desktop = isEmbeddedBrowserRuntime();
  const [snapshot, setSnapshot] = useState<BrowserSnapshot | null>(null);
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [capturingPageText, setCapturingPageText] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<BrowserNotice | null>(null);
  const [actionOutcome, setActionOutcome] = useState<{
    action: BrowserAction;
    tabId: string;
  } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const browserNavigationRequest = useUiStore(
    (state) => state.browserNavigationRequest,
  );
  const browserActionRequest = useUiStore(
    (state) => state.browserActionRequest,
  );
  const dismissBrowserNavigation = useUiStore(
    (state) => state.dismissBrowserNavigation,
  );
  const dismissBrowserAction = useUiStore(
    (state) => state.dismissBrowserAction,
  );
  const addressRef = useRef<HTMLInputElement>(null);
  // Focus alone is not a draft: native tab creation may focus this input while
  // publishing about:blank before its actual destination arrives.
  const addressDirty = useRef(false);
  const submittedUrl = useRef<string | null>(null);
  const surfaceRef = useRef<HTMLDivElement>(null);
  const mounted = useRef(false);
  const busyRef = useRef(false);
  const activeIdRef = useRef<string | null>(null);
  const active =
    snapshot?.tabs.find((tab) => tab.id === snapshot.activeTabId) ?? null;
  const browserAddress = active?.url ? browserAddressHandoff(active.url) : null;
  const acceptSnapshot = useCallback((next: BrowserSnapshot) => {
    setSnapshot((previous) =>
      previous && previous.revision > next.revision ? previous : next,
    );
  }, []);
  const reportError = useCallback(
    (reason: unknown) => setError(browserErrorMessage(reason)),
    [],
  );

  useEffect(() => {
    mounted.current = true;
    if (!desktop)
      return () => {
        mounted.current = false;
      };
    let disposed = false;
    let unsubscribe: (() => void) | undefined;
    let unsubscribeNotices: (() => void) | undefined;
    setError(null);
    setNotice(null);
    void (async () => {
      // Subscribe first so navigation events between registration and the
      // initial snapshot are retained. Revision rejects late older responses.
      unsubscribe = await nativeBrowser.subscribe((next) => {
        if (!disposed) acceptSnapshot(next);
      });
      if (disposed) {
        unsubscribe();
        return;
      }
      unsubscribeNotices = await nativeBrowser.subscribeNotices((next) => {
        if (!disposed) setNotice(next);
      });
      if (disposed) {
        unsubscribeNotices();
        return;
      }
      const next = await nativeBrowser.snapshot();
      if (!disposed) acceptSnapshot(next);
    })().catch((reason) => {
      if (!disposed) reportError(reason);
    });
    return () => {
      mounted.current = false;
      disposed = true;
      unsubscribe?.();
      unsubscribeNotices?.();
    };
  }, [desktop, attempt, acceptSnapshot, reportError]);

  useEffect(() => {
    const changedTab = activeIdRef.current !== (active?.id ?? null);
    activeIdRef.current = active?.id ?? null;
    if (changedTab) {
      addressDirty.current = false;
      setActionOutcome(null);
    }
    if (!addressDirty.current)
      setAddress(
        active?.url === "about:blank"
          ? (submittedUrl.current ?? "")
          : (active?.url ?? ""),
      );
    if (active?.url && active.url !== "about:blank")
      submittedUrl.current = null;
    if (changedTab && active?.url === "about:blank" && !submittedUrl.current)
      addressRef.current?.focus();
  }, [active?.id, active?.url]);

  useEffect(() => {
    if (browserActionRequest || browserNavigationRequest)
      setActionOutcome(null);
  }, [browserActionRequest?.id, browserNavigationRequest?.id]);

  useEffect(() => {
    const current = document.getElementById(`browser-tab-${active?.id}`);
    current?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [active?.id]);

  useNativeSurface(
    surfaceRef,
    Boolean(active && active.url !== "about:blank" && !active.error),
    active?.id ?? null,
    reportError,
  );

  const perform = async (operation: () => Promise<BrowserSnapshot>) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError(null);
    setNotice(null);
    setActionOutcome(null);
    try {
      const next = await operation();
      if (mounted.current) acceptSnapshot(next);
    } catch (reason) {
      submittedUrl.current = null;
      if (mounted.current) reportError(reason);
    } finally {
      busyRef.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const newTab = () => {
    if (busyRef.current) return;
    submittedUrl.current = null;
    void perform(() => nativeBrowser.createTab());
  };
  const action = (value: BrowserAction) => {
    if (active) void perform(() => nativeBrowser.action(active.id, value));
  };
  const captureCurrentPageText = async () => {
    const target = active;
    if (
      !desktop ||
      !target ||
      target.loading ||
      target.error ||
      target.url === "about:blank" ||
      busyRef.current
    )
      return;
    busyRef.current = true;
    setBusy(true);
    setCapturingPageText(true);
    setError(null);
    try {
      const before = await nativeBrowser.snapshot();
      const current = before.tabs.find((tab) => tab.id === before.activeTabId);
      if (
        !current ||
        current.id !== target.id ||
        current.url !== target.url ||
        current.loading ||
        current.error
      )
        throw new Error("BROWSER_PAGE_CHANGED");
      const capture: BrowserPageTextCapture =
        await nativeBrowser.capturePageText(target.id);
      const after = await nativeBrowser.snapshot();
      const latest = after.tabs.find((tab) => tab.id === after.activeTabId);
      if (
        !latest ||
        latest.id !== target.id ||
        latest.url !== target.url ||
        latest.loading ||
        latest.error ||
        capture.tabId !== target.id ||
        capture.url !== target.url
      )
        throw new Error("BROWSER_PAGE_CHANGED");
      const handoff = browserPageTextHandoff(capture);
      if (!handoff) throw new Error("BROWSER_CAPTURE_FAILED");
      if (!useAiWorkbenchHandoff.getState().stageIssue(handoff))
        throw new Error("BROWSER_CAPTURE_FAILED");
      revealAgentChat();
    } catch (reason) {
      if (mounted.current) reportError(reason);
    } finally {
      busyRef.current = false;
      if (mounted.current) {
        setBusy(false);
        setCapturingPageText(false);
      }
    }
  };
  const navigate = () => {
    if (busyRef.current) return;
    let url: string;
    try {
      url = normalizeBrowserAddress(address);
    } catch (reason) {
      setError((reason as Error).message);
      return;
    }
    if (url === "about:blank") {
      setError("请输入要打开的网址");
      return;
    }
    if (!desktop) {
      window.open(url, "_blank", "noopener,noreferrer");
      setError(null);
      return;
    }
    addressDirty.current = false;
    submittedUrl.current = url;
    setAddress(url === "about:blank" ? "" : url);
    void perform(() =>
      active
        ? nativeBrowser.navigate(active.id, url)
        : nativeBrowser.createTab(url),
    );
  };
  const approveBrowserNavigation = () => {
    const request = browserNavigationRequest;
    if (!request || busyRef.current) return;
    if (desktop && (!snapshot || snapshot.tabs.length >= BROWSER_TAB_LIMIT))
      return;
    if (!desktop) {
      try {
        window.open(request.url, "_blank", "noopener,noreferrer");
        dismissBrowserNavigation(request.id);
        setError(null);
      } catch (reason) {
        reportError(reason);
      }
      return;
    }
    void perform(async () => {
      const next = await nativeBrowser.createTab(request.url);
      // The native snapshot is the first evidence that a tab was created.
      // Keep the pending request and the existing address intact on failure.
      submittedUrl.current = request.url;
      addressDirty.current = false;
      setAddress(request.url);
      dismissBrowserNavigation(request.id);
      return next;
    });
  };
  const approveBrowserAction = () => {
    const request = browserActionRequest;
    const target = active;
    if (!request || !desktop || !target || busyRef.current) return;
    void perform(async () => {
      // Re-read native state immediately before acting: a streamed request
      // must not silently operate on a different tab after a UI change.
      const latest = await nativeBrowser.snapshot();
      const current = latest.tabs.find((tab) => tab.id === latest.activeTabId);
      if (!current || current.id !== target.id || current.url !== target.url)
        throw new Error("BROWSER_TARGET_CHANGED");
      if (
        (request.action === "back" && !current.canGoBack) ||
        (request.action === "forward" && !current.canGoForward) ||
        (request.action === "stop" && !current.loading)
      )
        throw new Error("BROWSER_ACTION_UNAVAILABLE");
      const result = await nativeBrowser.action(current.id, request.action);
      if (
        mounted.current &&
        result.activeTabId === current.id &&
        result.tabs.some((tab) => tab.id === current.id)
      )
        setActionOutcome({ action: request.action, tabId: current.id });
      dismissBrowserAction(request.id);
      return result;
    });
  };
  const actionLabel = browserActionRequest
    ? browserActionLabels[browserActionRequest.action]
    : "";
  const canApproveAction =
    desktop &&
    active &&
    !busy &&
    browserActionRequest &&
    (browserActionRequest.action === "back"
      ? active.canGoBack
      : browserActionRequest.action === "forward"
        ? active.canGoForward
        : browserActionRequest.action === "stop"
          ? active.loading
          : active.url !== "about:blank");
  const keyDown = (event: KeyboardEvent<HTMLElement>) => {
    // Scope to the browser toolbar/tab strip. A webpage has its own native
    // focus and handles its own shortcuts; the chat composer is unaffected.
    if (!desktop || event.defaultPrevented || event.nativeEvent.isComposing)
      return;
    const command =
      (event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey;
    if (command && event.key.toLowerCase() === "l") {
      event.preventDefault();
      addressRef.current?.focus();
      addressRef.current?.select();
    } else if (command && event.key.toLowerCase() === "t") {
      event.preventDefault();
      if (snapshot && snapshot.tabs.length < BROWSER_TAB_LIMIT) newTab();
    } else if (command && event.key.toLowerCase() === "w" && active) {
      event.preventDefault();
      void perform(() => nativeBrowser.closeTab(active.id));
    } else if (
      event.altKey &&
      !event.ctrlKey &&
      !event.metaKey &&
      event.key === "ArrowLeft" &&
      active?.canGoBack
    ) {
      event.preventDefault();
      action("back");
    } else if (
      event.altKey &&
      !event.ctrlKey &&
      !event.metaKey &&
      event.key === "ArrowRight" &&
      active?.canGoForward
    ) {
      event.preventDefault();
      action("forward");
    }
  };

  return (
    <section
      aria-label="内嵌浏览器"
      className="embedded-browser"
      onKeyDown={keyDown}
    >
      {desktop && !hideTabs && (
        <div className="browser-tabs-bar">
          <div
            className="browser-tabs"
            role="tablist"
            aria-label="浏览器标签页"
          >
            {snapshot?.tabs.map((tab) => (
              <div
                className={`browser-tab${tab.id === active?.id ? " browser-tab-active" : ""}`}
                key={tab.id}
              >
                <button
                  type="button"
                  role="tab"
                  id={`browser-tab-${tab.id}`}
                  aria-selected={tab.id === active?.id}
                  aria-controls="browser-page"
                  tabIndex={tab.id === active?.id ? 0 : -1}
                  className="browser-tab-select"
                  title={tab.title || tab.url}
                  disabled={busy}
                  onClick={() =>
                    void perform(() => nativeBrowser.activateTab(tab.id))
                  }
                  onKeyDown={(event) => {
                    if (
                      !["ArrowLeft", "ArrowRight", "Home", "End"].includes(
                        event.key,
                      ) ||
                      event.altKey ||
                      !snapshot
                    )
                      return;
                    event.preventDefault();
                    const index = snapshot.tabs.findIndex(
                      (item) => item.id === tab.id,
                    );
                    const next =
                      event.key === "Home"
                        ? 0
                        : event.key === "End"
                          ? snapshot.tabs.length - 1
                          : (index +
                              (event.key === "ArrowRight" ? 1 : -1) +
                              snapshot.tabs.length) %
                            snapshot.tabs.length;
                    void perform(() =>
                      nativeBrowser.activateTab(snapshot.tabs[next].id),
                    );
                    document
                      .getElementById(`browser-tab-${snapshot.tabs[next].id}`)
                      ?.focus();
                  }}
                >
                  {tab.loading ? (
                    <LoaderCircle
                      className="browser-loading-icon"
                      size={14}
                      aria-label="加载中"
                    />
                  ) : (
                    <Globe size={14} aria-hidden="true" />
                  )}
                  <span>
                    {tab.title ||
                      (tab.url === "about:blank" ? "新标签页" : tab.url)}
                  </span>
                </button>
                <button
                  type="button"
                  className="browser-tab-close"
                  aria-label={`关闭标签：${tab.title || "新标签页"}`}
                  disabled={busy}
                  onClick={() =>
                    void perform(() => nativeBrowser.closeTab(tab.id))
                  }
                >
                  <X size={12} />
                </button>
              </div>
            ))}
          </div>
          <button
            type="button"
            className="browser-icon-button"
            aria-label="新建浏览器标签"
            title="新建标签（Ctrl+T）"
            disabled={
              busy || !snapshot || snapshot.tabs.length >= BROWSER_TAB_LIMIT
            }
            onClick={newTab}
          >
            <Plus size={16} />
          </button>
        </div>
      )}
      <form
        className="browser-toolbar"
        onSubmit={(event) => {
          event.preventDefault();
          navigate();
        }}
      >
        {desktop && (
          <>
            <button
              type="button"
              className="browser-icon-button"
              aria-label="后退"
              title="后退"
              disabled={busy || !active?.canGoBack}
              onClick={() => action("back")}
            >
              <ArrowLeft size={16} />
            </button>
            <button
              type="button"
              className="browser-icon-button"
              aria-label="前进"
              title="前进"
              disabled={busy || !active?.canGoForward}
              onClick={() => action("forward")}
            >
              <ArrowRight size={16} />
            </button>
            <button
              type="button"
              className="browser-icon-button"
              aria-label={active?.loading ? "停止加载" : "重新加载"}
              title={active?.loading ? "停止加载" : "重新加载"}
              disabled={busy || !active || active.url === "about:blank"}
              onClick={() => action(active?.loading ? "stop" : "reload")}
            >
              {active?.loading ? <X size={16} /> : <RotateCw size={15} />}
            </button>
          </>
        )}
        <input
          ref={addressRef}
          className="browser-address"
          aria-label="浏览器地址"
          placeholder="输入网址"
          value={address}
          autoComplete="off"
          spellCheck={false}
          onBlur={() => {
            addressDirty.current = false;
          }}
          onChange={(event) => {
            addressDirty.current = true;
            setAddress(event.target.value);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              addressDirty.current = false;
              setAddress(
                active?.url === "about:blank" ? "" : (active?.url ?? ""),
              );
              event.currentTarget.blur();
            }
          }}
        />
        <button
          type="submit"
          className="browser-icon-button"
          aria-label={desktop ? "打开地址" : "在外部浏览器打开"}
          title={desktop ? "打开地址" : "在外部浏览器打开"}
          disabled={busy || (desktop && !snapshot)}
        >
          {desktop ? <ArrowRight size={15} /> : <ExternalLink size={15} />}
        </button>
        {browserAddress && (
          <button
            type="button"
            className="browser-handoff-button"
            aria-label="交给智能体分析"
            title="带入已脱敏的当前网页地址；不会自动发送、访问或控制网页"
            disabled={busy}
            onClick={() => {
              useAiWorkbenchHandoff.getState().stageIssue(browserAddress);
            }}
          >
            <Sparkles size={14} aria-hidden="true" />
            交给智能体分析
          </button>
        )}
        {desktop && active && active.url !== "about:blank" && (
          <button
            type="button"
            className="browser-icon-button"
            aria-label={
              capturingPageText
                ? "正在读取网页文本和链接"
                : "读取网页文本和链接并预览给 AI"
            }
            title="只读取当前标签的可见正文和有限 HTTP(S) 链接；完整预览后仍需手动带入和发送"
            disabled={
              busy || active.loading || Boolean(active.error) || !browserAddress
            }
            onClick={() => void captureCurrentPageText()}
          >
            {capturingPageText ? (
              <LoaderCircle className="browser-loading-icon" size={16} />
            ) : (
              <FileText size={16} />
            )}
          </button>
        )}
      </form>
      {browserNavigationRequest && (
        <div className="browser-navigation-request" role="alert">
          <div>
            <strong>智能体请求打开一个新网页标签</strong>
            <code>{browserNavigationRequest.url}</code>
            <p>确认后才会访问该地址；网页内容不会回传给智能体。</p>
            {desktop &&
            snapshot &&
            snapshot.tabs.length >= BROWSER_TAB_LIMIT ? (
              <p>
                已达 {BROWSER_TAB_LIMIT} 个标签上限；关闭一个标签后可继续确认。
              </p>
            ) : null}
          </div>
          <div className="browser-navigation-actions">
            <button
              className="button button-secondary"
              type="button"
              disabled={busy}
              onClick={() =>
                dismissBrowserNavigation(browserNavigationRequest.id)
              }
            >
              拒绝
            </button>
            <button
              className="button button-primary"
              type="button"
              disabled={
                busy ||
                (desktop &&
                  (!snapshot || snapshot.tabs.length >= BROWSER_TAB_LIMIT))
              }
              onClick={approveBrowserNavigation}
            >
              在新标签打开
            </button>
          </div>
        </div>
      )}
      {browserActionRequest && (
        <div className="browser-navigation-request" role="alert">
          <div>
            <strong>智能体请求{actionLabel}当前网页标签</strong>
            <code>{active?.url ?? "没有活动标签"}</code>
            <p>
              {desktop
                ? "仅在你确认后操作当前显示的标签；网页内容不会回传，命令结果也不会自动回传给智能体。"
                : "此操作仅支持 Windows 桌面版内嵌 Chromium 浏览器。"}
            </p>
          </div>
          <div className="browser-navigation-actions">
            <button
              className="button button-secondary"
              type="button"
              disabled={busy}
              onClick={() => dismissBrowserAction(browserActionRequest.id)}
            >
              拒绝
            </button>
            <button
              className="button button-primary"
              type="button"
              disabled={!canApproveAction}
              onClick={approveBrowserAction}
            >
              确认{actionLabel}
            </button>
          </div>
        </div>
      )}
      {!browserActionRequest &&
        actionOutcome &&
        actionOutcome.tabId === active?.id && (
          <div className="browser-navigation-request" role="status">
            <div>
              <strong>
                本地浏览器已接受“
                {browserActionLabels[actionOutcome.action]}
                ”命令
              </strong>
              <p>这不证明网页已加载完成，也不会自动把结果传给智能体。</p>
            </div>
            <div className="browser-navigation-actions">
              <button
                className="button button-secondary"
                type="button"
                onClick={() => setActionOutcome(null)}
              >
                关闭
              </button>
              <button
                className="button button-secondary"
                type="button"
                onClick={() => {
                  const handoff = browserActionOutcomeHandoff(
                    actionOutcome.action,
                  );
                  if (
                    handoff &&
                    useAiWorkbenchHandoff.getState().stageIssue(handoff)
                  ) {
                    setActionOutcome(null);
                    revealAgentChat();
                  }
                }}
              >
                把结果带入对话
              </button>
            </div>
          </div>
        )}
      {error && (
        <div className="browser-notice" role="alert">
          {error}
          {desktop && !snapshot && (
            <button
              type="button"
              className="browser-inline-button"
              onClick={() => setAttempt((value) => value + 1)}
            >
              重试
            </button>
          )}
        </div>
      )}
      {notice && notice.tabId === active?.id && (
        <div className="browser-notice browser-popup-notice" role="status">
          <span>未打开新标签：{browserErrorMessage(notice.message)}</span>
          <button
            type="button"
            className="browser-icon-button"
            aria-label="关闭浏览器提示"
            onClick={() => setNotice(null)}
          >
            <X size={14} />
          </button>
        </div>
      )}
      <div
        className="browser-page"
        id="browser-page"
        role="tabpanel"
        aria-label={active?.title || "网页内容"}
        ref={surfaceRef}
      >
        {!desktop ? (
          <div className="browser-empty">
            <Globe size={30} />
            <strong>在桌面版中浏览</strong>
            <p>
              Windows 桌面版使用 Chromium /
              WebView2，可在此打开多个网页标签。当前网页模式可从上方地址栏在外部浏览器打开网址。
            </p>
          </div>
        ) : !snapshot ? (
          <div className="browser-empty">
            <p>{error ? "浏览器暂不可用" : "正在连接内嵌浏览器…"}</p>
          </div>
        ) : active?.error ? (
          <div className="browser-empty">
            <Globe size={30} />
            <strong>无法打开此网页</strong>
            <p>{browserErrorMessage(active.error)}</p>
            <button
              type="button"
              className="button button-secondary"
              disabled={busy}
              onClick={() => action("reload")}
            >
              重新加载网页
            </button>
          </div>
        ) : !active || active.url === "about:blank" ? (
          <div className="browser-empty">
            <Globe size={30} />
            <strong>开始浏览</strong>
            <p>输入网址，网页将在当前工作区打开。</p>
            <button
              type="button"
              className="browser-inline-button"
              onClick={() => addressRef.current?.focus()}
            >
              输入网址
            </button>
          </div>
        ) : (
          <span className="browser-surface-label">
            {active.loading ? "正在加载网页…" : "网页由内嵌浏览器显示"}
          </span>
        )}
      </div>
    </section>
  );
}
