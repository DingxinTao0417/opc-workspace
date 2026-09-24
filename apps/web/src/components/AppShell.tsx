import {
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
} from "react";
import { useEffect } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { useSettingsStore } from "../store/settings";
import {
  BROWSER_PANEL_DEFAULT_WIDTH,
  BROWSER_PANEL_MAX_WIDTH,
  BROWSER_PANEL_MIN_WIDTH,
  RIGHT_OVERVIEW_DEFAULT_WIDTH,
  RIGHT_OVERVIEW_MAX_WIDTH,
  RIGHT_OVERVIEW_MIN_WIDTH,
  useUiStore,
} from "../store/ui";
import { RightOverview } from "./RightOverview";
import { WorkbenchOverview } from "./WorkbenchOverview";
import { RightFloatingCard } from "./RightFloatingCard";
import { AiSessionRail } from "./AiSessionRail";
import { ModeSwitch, isAgentRoute } from "./ModeSwitch";
import { Sidebar } from "./Sidebar";
import { useWorkspacePanels } from "../store/workspacePanels";

/**
 * Pointer capture is unavailable in jsdom and some embedded webviews; the
 * resize must keep working there instead of throwing on pointerdown.
 */
function capturePointer(target: HTMLElement, pointerId: number) {
  try {
    target.setPointerCapture(pointerId);
  } catch {
    /* Explicit capture is optional for this interaction. */
  }
}

function releasePointer(target: HTMLElement, pointerId: number) {
  try {
    if (target.hasPointerCapture(pointerId)) {
      target.releasePointerCapture(pointerId);
    }
  } catch {
    /* Mirror capturePointer: cleanup never breaks the drag. */
  }
}

export function AppShell() {
  useHealthQuery();
  const configuredRightOverview = useSettingsStore(
    (state) =>
      state.preview?.general.showRightOverview ?? state.showRightOverview,
  );
  const sidebarCollapsed = useUiStore((state) => state.sidebarCollapsed);
  const agentRailCollapsed = useUiStore((state) => state.agentRailCollapsed);
  const rightOverviewCollapsed = useUiStore(
    (state) => state.rightOverviewCollapsed,
  );
  const rightOverviewWidth = useUiStore((state) => state.rightOverviewWidth);
  const workspaceMaximized = useWorkspacePanels((state) => state.maximized);
  const browserPanelWidth = useUiStore((state) => state.browserPanelWidth);
  const setBrowserPanelWidth = useUiStore(
    (state) => state.setBrowserPanelWidth,
  );
  const setRightOverviewWidth = useUiStore(
    (state) => state.setRightOverviewWidth,
  );
  const setLastWorkspacePath = useUiStore(
    (state) => state.setLastWorkspacePath,
  );
  const [resizingRightOverview, setResizingRightOverview] = useState(false);
  const frameRef = useRef<HTMLDivElement>(null);
  const [frameWidth, setFrameWidth] = useState(0);
  const resize = useRef<{
    pointerId: number;
    startX: number;
    startWidth: number;
  } | null>(null);

  const location = useLocation();
  const agentMode = isAgentRoute(location.pathname);
  const isAiPage = agentMode;
  const hideAgentRail = agentMode && agentRailCollapsed;
  // Business cards and the agent tool workspace have separate widths. The
  // agent's temporary collapse/maximize state must not hide business content.
  const showRightOverview =
    configuredRightOverview && (!agentMode || !rightOverviewCollapsed);
  const showFloatingCard =
    configuredRightOverview && !showRightOverview && isAiPage;
  const browserOpen = showRightOverview && agentMode;
  const panelMin = browserOpen
    ? BROWSER_PANEL_MIN_WIDTH
    : RIGHT_OVERVIEW_MIN_WIDTH;
  const panelMax = browserOpen
    ? Math.min(
        BROWSER_PANEL_MAX_WIDTH,
        Math.max(BROWSER_PANEL_MIN_WIDTH, (frameWidth || 1040) - 360),
      )
    : RIGHT_OVERVIEW_MAX_WIDTH;
  const panelWidth = browserOpen
    ? Math.min(browserPanelWidth, panelMax)
    : rightOverviewWidth;
  const panelDefault = browserOpen
    ? BROWSER_PANEL_DEFAULT_WIDTH
    : RIGHT_OVERVIEW_DEFAULT_WIDTH;
  const setPanelWidth = (width: number) => {
    if (browserOpen)
      setBrowserPanelWidth(Math.min(panelMax, Math.max(panelMin, width)));
    else setRightOverviewWidth(width);
  };

  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return;
    const measure = () => setFrameWidth(frame.clientWidth);
    measure();
    const observer =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(measure);
    observer?.observe(frame);
    window.addEventListener("resize", measure);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);

  useEffect(() => {
    if (!agentMode)
      setLastWorkspacePath(
        `${location.pathname}${location.search}${location.hash}`,
      );
  }, [
    agentMode,
    location.pathname,
    location.search,
    location.hash,
    setLastWorkspacePath,
  ]);

  const handleOverviewResizeKeyDown = (
    event: KeyboardEvent<HTMLDivElement>,
  ) => {
    const step = event.shiftKey ? 48 : 12;
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      setPanelWidth(panelWidth + step);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      setPanelWidth(panelWidth - step);
      return;
    }
    if (event.key === "Home") {
      event.preventDefault();
      setPanelWidth(Math.min(panelDefault, panelMax));
    }
  };

  const handleOverviewResizePointerDown = (
    event: PointerEvent<HTMLDivElement>,
  ) => {
    // Only the primary button starts a drag; middle/right clicks are ignored.
    if (event.button > 0) return;
    capturePointer(event.currentTarget, event.pointerId);
    resize.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startWidth: panelWidth,
    };
    setResizingRightOverview(true);
  };

  const handleOverviewResizePointerMove = (
    event: PointerEvent<HTMLDivElement>,
  ) => {
    const active = resize.current;
    if (!active || active.pointerId !== event.pointerId) return;
    setPanelWidth(active.startWidth - (event.clientX - active.startX));
  };

  const handleOverviewResizePointerEnd = (
    event: PointerEvent<HTMLDivElement>,
  ) => {
    if (resize.current?.pointerId !== event.pointerId) return;
    resize.current = null;
    setResizingRightOverview(false);
    releasePointer(event.currentTarget, event.pointerId);
  };

  return (
    <div
      className={[
        "app-shell",
        showRightOverview ? "" : "app-shell-no-overview",
        agentMode ? "app-shell-agent" : "",
        hideAgentRail ? "app-shell-nav-hidden" : "",
        !agentMode && sidebarCollapsed ? "app-shell-sidebar-collapsed" : "",
        resizingRightOverview ? "app-shell-resizing" : "",
        browserOpen ? "app-shell-browser-open" : "",
        showRightOverview && agentMode && workspaceMaximized
          ? "app-shell-workspace-maximized"
          : "",
      ]
        .filter(Boolean)
        .join(" ")}
      style={
        {
          "--right-overview-width": `${panelWidth}px`,
        } as CSSProperties
      }
    >
      {hideAgentRail ? null : (
        <div
          className={`app-nav${
            !agentMode && sidebarCollapsed ? " is-collapsed" : ""
          }`}
        >
          <ModeSwitch />
          {agentMode ? <AiSessionRail /> : <Sidebar />}
        </div>
      )}
      <div className="workspace-frame" ref={frameRef}>
        <main className="main-column">
          <div className="page-scroll">
            <Outlet />
          </div>
          {showFloatingCard ? <RightFloatingCard /> : null}
        </main>
        {showRightOverview ? (
          <div
            aria-label="调整右侧概览宽度"
            aria-orientation="vertical"
            aria-valuemax={panelMax}
            aria-valuemin={panelMin}
            aria-valuenow={panelWidth}
            className="ov-resizer"
            data-dragging={resizingRightOverview ? "true" : undefined}
            onKeyDown={handleOverviewResizeKeyDown}
            onPointerCancel={handleOverviewResizePointerEnd}
            onPointerDown={handleOverviewResizePointerDown}
            onPointerMove={handleOverviewResizePointerMove}
            onPointerUp={handleOverviewResizePointerEnd}
            role="separator"
            tabIndex={0}
          />
        ) : null}
        {showRightOverview ? (
          agentMode ? (
            <RightOverview />
          ) : (
            <WorkbenchOverview />
          )
        ) : null}
      </div>
    </div>
  );
}
