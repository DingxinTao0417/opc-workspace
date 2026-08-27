import { Outlet } from "react-router-dom";
import { useHealthQuery } from "../api/hooks";
import { RightOverview } from "./RightOverview";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  const health = useHealthQuery();

  return (
    <div className="app-shell">
      <Sidebar />
      <main className="main-column">
        <div
          className="runtime-strip"
          data-state={
            health.isSuccess ? "ready" : health.isError ? "error" : "loading"
          }
        >
          <span className="runtime-dot" />
          <span>
            {health.isSuccess
              ? `本地服务就绪${health.data.app?.version ? ` · v${health.data.app.version}` : ""}`
              : health.isError
                ? "本地服务未就绪 · 数据暂不可用"
                : "正在连接本地 Sidecar…"}
          </span>
        </div>
        <div className="page-scroll">
          <Outlet />
        </div>
      </main>
      <RightOverview />
    </div>
  );
}
