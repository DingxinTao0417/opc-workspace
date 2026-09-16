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
  RIGHT_OVERVIEW_DEFAULT_WIDTH,
  RIGHT_OVERVIEW_MAX_WIDTH,
  RIGHT_OVERVIEW_MIN_WIDTH,
  useUiStore,
} from "../store/ui";
import { RightOverview } from "./RightOverview";
import { RightFloatingCard } from "./RightFloatingCard";
import { AiSessionRail } from "./AiSessionRail";
import { ModeSwitch, isAgentRoute } from "./ModeSwitch";
import { Sidebar } from "./Sidebar";

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
  const setRightOverviewWidth = useUiStore(
    (state) => state.setRightOverviewWidth,
  );
  const setLastWorkspacePath = useUiStore(
    (state) => state.setLastWorkspacePath,
  );
  const [resizingRightOverview, setResizingRightOverview] = useState(false);
  const resize = useRef<{
    pointerId: number;
    startX: number;
    startWidth: number;
  } | null>(null);

  const location = useLocation();
  const agentMode = isAgentRoute(location.pathname);
  const isAiPage = agentMode;
  const hideAgentRail = agentMode && agentRailCollapsed;
  // The docked overview belongs to agent mode only: business pages keep the
  // full width, while the conversation can dock the panel or fall back to the
  // floating card when it is collapsed.
  const showRightOverview =
    configuredRightOverview && !rightOverviewCollapsed && agentMode;
  const showFloatingCard =
    configuredRightOverview && !showRightOverview && isAiPage;

  useEffect(() => {
    if (!agentMode) setLastWorkspacePath(location.pathname);
  }, [agentMode, location.pathname, setLastWorkspacePath]);

  const handleOverviewResizeKeyDown = (
    event: KeyboardEvent<HTMLDivElement>,
  ) => {
    const step = event.shiftKey ? 48 : 12;
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      setRightOverviewWidth(rightOverviewWidth + step);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      setRightOverviewWidth(rightOverviewWidth - step);
      return;
    }
    if (event.key === "Home") {
      event.preventDefault();
      setRightOverviewWidth(RIGHT_OVERVIEW_DEFAULT_WIDTH);
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
      startWidth: rightOverviewWidth,
    };
    setResizingRightOverview(true);
  };

  const handleOverviewResizePointerMove = (
    event: PointerEvent<HTMLDivElement>,
  ) => {
    const active = resize.current;
    if (!active || active.pointerId !== event.pointerId) return;
    setRightOverviewWidth(active.startWidth - (event.clientX - active.startX));
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
      ]
        .filter(Boolean)
        .join(" ")}
      style={
        {
          "--right-overview-width": `${rightOverviewWidth}px`,
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
      <div className="workspace-frame">
        <main className="main-column">
          <div className="page-scroll">
            <Outlet />
          </div>
        </main>
        {showRightOverview ? (
          <div
            aria-label="调整右侧概览宽度"
            aria-orientation="vertical"
            aria-valuemax={RIGHT_OVERVIEW_MAX_WIDTH}
            aria-valuemin={RIGHT_OVERVIEW_MIN_WIDTH}
            aria-valuenow={rightOverviewWidth}
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
        {showRightOverview ? <RightOverview /> : null}
      </div>
      {showFloatingCard ? <RightFloatingCard /> : null}
    </div>
  );
}
