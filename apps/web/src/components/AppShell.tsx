import { Outlet } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { useSettingsStore } from "../store/settings";
import { RightOverview } from "./RightOverview";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  useHealthQuery();
  const showRightOverview = useSettingsStore(
    (state) =>
      state.preview?.general.showRightOverview ?? state.showRightOverview,
  );

  return (
    <div
      className={
        showRightOverview ? "app-shell" : "app-shell app-shell-no-overview"
      }
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
