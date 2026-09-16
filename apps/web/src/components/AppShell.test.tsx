import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppShell } from "./AppShell";

const state = vi.hoisted(() => ({
  sidebarCollapsed: false,
  rightOverviewCollapsed: false,
  showRightOverview: true,
  rightOverviewWidth: 280,
  lastWorkspacePath: "/today",
  agentRailCollapsed: false,
  setRightOverviewWidth: vi.fn(),
  setLastWorkspacePath: vi.fn(),
  toggleRightOverviewCollapsed: vi.fn(),
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
  useUiStore: (selector: (value: unknown) => unknown) =>
    selector({
      sidebarCollapsed: state.sidebarCollapsed,
      rightOverviewCollapsed: state.rightOverviewCollapsed,
      rightOverviewWidth: state.rightOverviewWidth,
      lastWorkspacePath: state.lastWorkspacePath,
      agentRailCollapsed: state.agentRailCollapsed,
      setRightOverviewWidth: state.setRightOverviewWidth,
      setLastWorkspacePath: state.setLastWorkspacePath,
      toggleRightOverviewCollapsed: state.toggleRightOverviewCollapsed,
    }),
}));

vi.mock("./Sidebar", () => ({
  Sidebar: () => <aside>导航</aside>,
}));

vi.mock("./RightOverview", () => ({
  RightOverview: () => <aside>概览</aside>,
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
    state.rightOverviewCollapsed = false;
    state.showRightOverview = true;
    state.rightOverviewWidth = 280;
    state.agentRailCollapsed = false;
    state.setRightOverviewWidth.mockReset();
    state.setLastWorkspacePath.mockReset();
    state.toggleRightOverviewCollapsed.mockReset();
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
    expect(screen.queryByRole("button", { name: "展开右侧概览" })).toBeNull();
    expect(screen.queryByText("悬浮卡")).toBeNull();
  });

  it("keeps the docked overview out of the workspace layout", () => {
    state.rightOverviewWidth = 320;
    renderShell();

    expect(screen.getByText("页面").closest(".app-shell")).toHaveStyle({
      "--right-overview-width": "320px",
    });
    expect(screen.queryByText("概览")).toBeNull();
    expect(
      screen.queryByRole("separator", { name: "调整右侧概览宽度" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "收起右侧概览" })).toBeNull();
  });

  it("shows the floating status card only on the AI page when collapsed", () => {
    state.rightOverviewCollapsed = true;
    const view = renderShellAt("/ai");
    expect(screen.getByText("悬浮卡")).toBeInTheDocument();

    view.unmount();
    cleanup();
    state.rightOverviewCollapsed = true;
    renderShellAt("/");
    expect(screen.queryByText("悬浮卡")).toBeNull();
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
    renderShellAt("/ai");

    expect(screen.getByText("概览")).toBeInTheDocument();
    const handle = screen.getByRole("separator", {
      name: "调整右侧概览宽度",
    });
    expect(handle).toHaveAttribute("aria-valuenow", "280");
    expect(handle).toHaveAttribute("aria-valuemin", "240");
    expect(handle).toHaveAttribute("aria-valuemax", "480");

    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(292);
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
  });
});
