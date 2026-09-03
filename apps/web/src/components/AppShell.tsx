import { Outlet } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import { RightOverview } from "./RightOverview";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  useHealthQuery();
  const showRightOverview = useSettingsStore(
    (state) =>
      state.preview?.general.showRightOverview ?? state.showRightOverview,
  );
  const sidebarCollapsed = useUiStore((state) => state.sidebarCollapsed);

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
        <div className="page-scroll">
          <Outlet />
        </div>
      </main>
      {showRightOverview ? <RightOverview /> : null}
    </div>
  );
}
