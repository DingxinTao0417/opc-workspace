import { LayoutGrid, Sparkles } from "lucide-react";
import { useLocation, useNavigate } from "react-router-dom";
import { useUiStore } from "../store/ui";

export function isAgentRoute(pathname: string) {
  return pathname === "/ai" || pathname.startsWith("/ai/");
}

/**
 * Two-mode switch: the workspace pages keep the business navigation, while the
 * agent mode swaps that rail for the conversation list. The mode is the route,
 * so it stays deep-linkable and needs no extra persisted preference.
 */
export function ModeSwitch() {
  const location = useLocation();
  const navigate = useNavigate();
  const lastWorkspacePath = useUiStore((state) => state.lastWorkspacePath);
  const agentMode = isAgentRoute(location.pathname);

  return (
    <div aria-label="模式切换" className="mode-switch" role="group">
      <button
        aria-pressed={!agentMode}
        className={`mode-switch-item${agentMode ? "" : " is-active"}`}
        onClick={() => navigate(lastWorkspacePath || "/today")}
        type="button"
      >
        <LayoutGrid aria-hidden="true" size={15} />
        <span className="mode-switch-label">工作台</span>
      </button>
      <button
        aria-pressed={agentMode}
        className={`mode-switch-item${agentMode ? " is-active" : ""}`}
        onClick={() => navigate("/ai")}
        type="button"
      >
        <Sparkles aria-hidden="true" size={15} />
        <span className="mode-switch-label">智能体</span>
      </button>
    </div>
  );
}
