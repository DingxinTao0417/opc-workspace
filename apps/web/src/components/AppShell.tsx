import { Outlet } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { RightOverview } from "./RightOverview";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  useHealthQuery();

  return (
    <div className="app-shell">
      <Sidebar />
      <main className="main-column">
        <div className="page-scroll">
          <Outlet />
        </div>
      </main>
      <RightOverview />
    </div>
  );
}
