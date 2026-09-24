import {
  cleanup,
  createEvent,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppShell } from "./AppShell";

const state = vi.hoisted(() => ({
  sidebarCollapsed: false,
  rightOverviewCollapsed: false,
  showRightOverview: true,
  rightOverviewWidth: 280,
  browserPanelWidth: 680,
  rightPanelTab: "summary",
  setBrowserPanelWidth: vi.fn(),
  lastWorkspacePath: "/today",
  agentRailCollapsed: false,
  workspaceMaximized: false,
  setRightOverviewWidth: vi.fn(),
  setLastWorkspacePath: vi.fn(),
  toggleRightOverviewCollapsed: vi.fn(),
  collapseAgentWorkspace: vi.fn(() => {
    state.rightOverviewCollapsed = true;
  }),
}));

vi.mock("../api/hooks", () => ({ useHealthQuery: vi.fn() }));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (value: unknown) => unknown) =>
    selector({
      preview: null,
      showRightOverview: state.showRightOverview,
    }),
}));

vi.mock("../store/ui", () => ({
  RIGHT_OVERVIEW_DEFAULT_WIDTH: 280,
  RIGHT_OVERVIEW_MAX_WIDTH: 480,
  RIGHT_OVERVIEW_MIN_WIDTH: 240,
  BROWSER_PANEL_DEFAULT_WIDTH: 680,
  BROWSER_PANEL_MIN_WIDTH: 360,
  BROWSER_PANEL_MAX_WIDTH: 1200,
  useUiStore: (selector: (value: unknown) => unknown) =>
    selector({
      sidebarCollapsed: state.sidebarCollapsed,
      rightOverviewCollapsed: state.rightOverviewCollapsed,
      rightOverviewWidth: state.rightOverviewWidth,
      browserPanelWidth: state.browserPanelWidth,
      rightPanelTab: state.rightPanelTab,
      setBrowserPanelWidth: state.setBrowserPanelWidth,
      lastWorkspacePath: state.lastWorkspacePath,
      agentRailCollapsed: state.agentRailCollapsed,
      setRightOverviewWidth: state.setRightOverviewWidth,
      setLastWorkspacePath: state.setLastWorkspacePath,
      toggleRightOverviewCollapsed: state.toggleRightOverviewCollapsed,
      collapseAgentWorkspace: state.collapseAgentWorkspace,
    }),
}));

vi.mock("./Sidebar", () => ({
  Sidebar: () => <aside>导航</aside>,
}));

vi.mock("./RightOverview", () => ({
  RightOverview: () => <aside>概览</aside>,
}));

vi.mock("./WorkbenchOverview", () => ({
  WorkbenchOverview: () => <aside>工作台概览</aside>,
}));

vi.mock("../store/workspacePanels", () => ({
  useWorkspacePanels: (selector: (value: unknown) => unknown) =>
    selector({ maximized: state.workspaceMaximized }),
}));

vi.mock("./RightFloatingCard", () => ({
  RightFloatingCard: () => <aside>悬浮卡</aside>,
}));

vi.mock("./AiSessionRail", () => ({
  AiSessionRail: () => <aside>会话轨</aside>,
}));

function renderShell() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<div>页面</div>} />
          <Route path="/ai" element={<div>AI 页</div>} />
          <Route path="/today" element={<div>今日页</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

function renderShellAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<div>页面</div>} />
          <Route path="/ai" element={<div>AI 页</div>} />
          <Route path="/today" element={<div>今日页</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("AppShell", () => {
  afterEach(() => {
    cleanup();
    state.sidebarCollapsed = false;
    state.rightOverviewCollapsed = true;
    state.showRightOverview = true;
    state.rightOverviewWidth = 280;
    state.browserPanelWidth = 680;
    state.rightPanelTab = "summary";
    state.setBrowserPanelWidth.mockReset();
    state.agentRailCollapsed = false;
    state.workspaceMaximized = false;
    state.setRightOverviewWidth.mockReset();
    state.setLastWorkspacePath.mockReset();
    state.toggleRightOverviewCollapsed.mockReset();
    state.collapseAgentWorkspace.mockClear();
  });

  it("remembers record identity and return context when switching modes", () => {
    const path = "/today?reminder=record&return_session=chat";
    renderShellAt(path);
    expect(state.setLastWorkspacePath).toHaveBeenLastCalledWith(path);
  });

  it("marks the shell when the primary sidebar is collapsed", () => {
    state.sidebarCollapsed = true;
    renderShell();

    expect(screen.getByText("页面").closest(".app-shell")).toHaveClass(
      "app-shell-sidebar-collapsed",
    );
  });

  it("combines collapsed navigation with a hidden overview", () => {
    state.sidebarCollapsed = true;
    state.showRightOverview = false;
    renderShell();

    expect(screen.getByText("页面").closest(".app-shell")).toHaveClass(
      "app-shell-sidebar-collapsed",
      "app-shell-no-overview",
    );
    expect(screen.queryByText("概览")).toBeNull();
    expect(screen.queryByText("工作台概览")).toBeNull();
    expect(screen.queryByRole("button", { name: "展开右侧概览" })).toBeNull();
    expect(screen.queryByText("悬浮卡")).toBeNull();
  });

  it("restores resizable business cards with their own width", () => {
    state.rightOverviewWidth = 320;
    renderShell();

    expect(screen.getByText("页面").closest(".app-shell")).toHaveStyle({
      "--right-overview-width": "320px",
    });
    expect(screen.queryByText("概览")).toBeNull();
    expect(screen.getByText("工作台概览")).toBeInTheDocument();
    const handle = screen.getByRole("separator", { name: "调整右侧概览宽度" });
    expect(handle).toHaveAttribute("aria-valuemin", "240");
    expect(handle).toHaveAttribute("aria-valuemax", "480");
    expect(handle).toHaveAttribute("aria-valuenow", "320");
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(332);
    fireEvent.keyDown(handle, { key: "Home" });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(280);
    expect(state.setBrowserPanelWidth).not.toHaveBeenCalled();
  });

  it("drags the business sidebar without changing agent workspace width", () => {
    renderShellAt("/today");
    const handle = screen.getByRole("separator");
    const start = createEvent.pointerDown(handle);
    Object.assign(start, { pointerId: 1, clientX: 900, button: 0 });
    fireEvent(handle, start);
    const move = createEvent.pointerMove(handle);
    Object.assign(move, { pointerId: 1, clientX: 840 });
    fireEvent(handle, move);
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(340);
    expect(state.setBrowserPanelWidth).not.toHaveBeenCalled();
    const end = createEvent.pointerUp(handle);
    Object.assign(end, { pointerId: 1 });
    fireEvent(handle, end);
    expect(screen.getByRole("main").closest(".app-shell")).not.toHaveClass(
      "app-shell-resizing",
    );
  });

  it("does not apply agent collapse or maximize state to business pages", () => {
    state.rightOverviewCollapsed = true;
    state.workspaceMaximized = true;
    renderShellAt("/today");
    expect(screen.getByText("工作台概览")).toBeInTheDocument();
    expect(screen.getByRole("main").closest(".app-shell")).not.toHaveClass(
      "app-shell-no-overview",
      "app-shell-workspace-maximized",
      "app-shell-browser-open",
    );
  });

  it("shows the floating status card only on the AI page when collapsed", () => {
    state.rightOverviewCollapsed = true;
    const view = renderShellAt("/ai");
    expect(screen.getByText("悬浮卡")).toBeInTheDocument();
    expect(screen.getByRole("main")).toContainElement(
      screen.getByText("悬浮卡"),
    );

    view.unmount();
    cleanup();
    state.rightOverviewCollapsed = true;
    renderShellAt("/");
    expect(screen.queryByText("悬浮卡")).toBeNull();
    expect(screen.getByText("工作台概览")).toBeInTheDocument();
  });

  it("omits the resize handle when the overview is hidden", () => {
    state.showRightOverview = false;
    renderShell();

    expect(
      screen.queryByRole("separator", { name: "调整右侧概览宽度" }),
    ).toBeNull();
  });

  it("swaps the navigation rail for the conversation list in agent mode", () => {
    renderShellAt("/ai");

    const shell = screen.getByText("AI 页").closest(".app-shell");
    expect(shell).toHaveClass("app-shell-agent");
    expect(screen.getByText("会话轨")).toBeInTheDocument();
    expect(screen.queryByText("导航")).toBeNull();
  });

  it("keeps the business navigation outside agent mode", () => {
    renderShell();

    const shell = screen.getByText("页面").closest(".app-shell");
    expect(shell).not.toHaveClass("app-shell-agent");
    expect(screen.getByText("导航")).toBeInTheDocument();
    expect(screen.queryByText("会话轨")).toBeNull();
  });

  it("hides the conversation rail column when the chat header collapses it", () => {
    state.agentRailCollapsed = true;
    renderShellAt("/ai");

    const shell = screen.getByText("AI 页").closest(".app-shell");
    expect(shell).toHaveClass("app-shell-agent", "app-shell-nav-hidden");
    expect(screen.queryByText("会话轨")).toBeNull();
  });

  it("docks a resizable work panel beside the agent conversation", () => {
    state.rightOverviewCollapsed = false;
    renderShellAt("/ai");

    expect(screen.getByText("概览")).toBeInTheDocument();
    expect(screen.queryByText("工作台概览")).toBeNull();
    const handle = screen.getByRole("separator", {
      name: "调整右侧概览宽度",
    });
    expect(handle).toHaveAttribute("aria-valuenow", "680");
    expect(handle).toHaveAttribute("aria-valuemin", "360");
    expect(handle).toHaveAttribute("aria-valuemax", "680");

    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(state.setBrowserPanelWidth).toHaveBeenLastCalledWith(680);
  });

  it("uses the wider tool workspace for all panel types without overwriting legacy preferences", () => {
    state.rightOverviewCollapsed = false;
    state.rightPanelTab = "browser";
    const view = renderShellAt("/ai");
    const handle = screen.getByRole("separator", { name: "调整右侧概览宽度" });
    expect(handle).toHaveAttribute("aria-valuenow", "680");
    fireEvent.keyDown(handle, { key: "ArrowRight", shiftKey: true });
    expect(state.setBrowserPanelWidth).toHaveBeenLastCalledWith(632);
    expect(state.setRightOverviewWidth).not.toHaveBeenCalled();
    view.unmount();
    state.rightPanelTab = "summary";
    renderShellAt("/ai");
    expect(screen.getByRole("separator")).toHaveAttribute(
      "aria-valuenow",
      "680",
    );
  });

  it("keeps at least 360px for chat when the browser wants more space", () => {
    state.rightOverviewCollapsed = false;
    const width = vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(900);
    state.rightPanelTab = "browser";
    state.browserPanelWidth = 1000;
    try {
      renderShellAt("/ai");
      expect(screen.getByRole("separator")).toHaveAttribute(
        "aria-valuenow",
        "540",
      );
      expect(screen.getByRole("separator")).toHaveAttribute(
        "aria-valuemax",
        "540",
      );
    } finally {
      width.mockRestore();
    }
  });

  it("falls back to the floating card when the work panel is collapsed", () => {
    state.rightOverviewCollapsed = true;
    renderShellAt("/ai");

    expect(screen.queryByText("概览")).toBeNull();
    expect(screen.getByText("悬浮卡")).toBeInTheDocument();
    expect(
      screen.queryByRole("separator", { name: "调整右侧概览宽度" }),
    ).toBeNull();
  });

  it("switches between workspace and agent modes", () => {
    renderShellAt("/ai");

    expect(screen.getByText("AI 页")).toBeInTheDocument();
    for (const label of ["工作台", "智能体"]) {
      const button = screen.getByRole("button", { name: label });
      expect(button).toHaveAttribute("aria-label", label);
      expect(button).toHaveAttribute("title", `切换到${label}`);
    }
    expect(screen.getByRole("button", { name: /智能体/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    fireEvent.click(screen.getByRole("button", { name: /工作台/ }));

    expect(state.lastWorkspacePath).toBe("/today");
    expect(screen.getByText("今日页")).toBeInTheDocument();
    expect(screen.queryByText("AI 页")).toBeNull();
    expect(screen.queryByText("会话轨")).toBeNull();
    expect(screen.getByText("导航")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /工作台/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText("工作台概览")).toBeInTheDocument();
    expect(screen.queryByText("概览")).toBeNull();
  });

  it("collapses the agent workspace when switching in from the workbench", () => {
    state.rightOverviewCollapsed = false;
    renderShellAt("/today");

    fireEvent.click(screen.getByRole("button", { name: /智能体/ }));

    expect(state.collapseAgentWorkspace).toHaveBeenCalledOnce();
    expect(screen.getByText("AI 页")).toBeInTheDocument();
    expect(screen.queryByText("概览")).toBeNull();
    expect(screen.getByText("悬浮卡")).toBeInTheDocument();
  });
});
