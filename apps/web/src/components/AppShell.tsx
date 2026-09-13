import { PanelRightClose, PanelRightOpen } from "lucide-react";
import { Outlet, useLocation } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import { RightOverview } from "./RightOverview";
import { RightFloatingCard } from "./RightFloatingCard";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  useHealthQuery();
  const configuredRightOverview = useSettingsStore(
    (state) =>
      state.preview?.general.showRightOverview ?? state.showRightOverview,
  );
  const sidebarCollapsed = useUiStore((state) => state.sidebarCollapsed);
  const rightOverviewCollapsed = useUiStore(
    (state) => state.rightOverviewCollapsed,
  );
  const toggleRightOverviewCollapsed = useUiStore(
    (state) => state.toggleRightOverviewCollapsed,
  );
  const showRightOverview = configuredRightOverview && !rightOverviewCollapsed;
  const location = useLocation();
  const isAiPage =
    location.pathname === "/ai" || location.pathname.startsWith("/ai/");
  const showFloatingCard =
    configuredRightOverview && !showRightOverview && isAiPage;

  return (
    <div
      className={[
        "app-shell",
        showRightOverview ? "" : "app-shell-no-overview",
        sidebarCollapsed ? "app-shell-sidebar-collapsed" : "",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <Sidebar />
      <main className="main-column">
        {configuredRightOverview ? (
          <div className="workspace-frame-chrome">
            <button
              aria-controls={showRightOverview ? "right-overview" : undefined}
              aria-expanded={showRightOverview}
              aria-label={showRightOverview ? "收起右侧概览" : "展开右侧概览"}
              className="icon-button workspace-overview-toggle"
              onClick={toggleRightOverviewCollapsed}
              title={showRightOverview ? "收起右侧概览" : "展开右侧概览"}
              type="button"
            >
              {showRightOverview ? (
                <PanelRightClose aria-hidden="true" size={16} />
              ) : (
                <PanelRightOpen aria-hidden="true" size={16} />
              )}
            </button>
          </div>
        ) : null}
        <div className="page-scroll">
          <Outlet />
        </div>
      </main>
      {showRightOverview ? <RightOverview /> : null}
      {showFloatingCard ? <RightFloatingCard /> : null}
    </div>
  );
}
