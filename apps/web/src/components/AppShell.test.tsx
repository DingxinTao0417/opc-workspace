import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppShell } from "./AppShell";

const state = vi.hoisted(() => ({
  sidebarCollapsed: false,
  rightOverviewCollapsed: false,
  showRightOverview: true,
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
  useUiStore: (selector: (value: unknown) => unknown) =>
    selector({
      sidebarCollapsed: state.sidebarCollapsed,
      rightOverviewCollapsed: state.rightOverviewCollapsed,
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

function renderShell() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<div>页面</div>} />
          <Route path="/ai" element={<div>AI 页</div>} />
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

  it("lets the user collapse and restore the right overview for this session", () => {
    const view = renderShell();

    const collapseButton = screen.getByRole("button", {
      name: "收起右侧概览",
    });
    expect(collapseButton).toHaveAttribute("aria-controls", "right-overview");
    expect(collapseButton).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(collapseButton);
    expect(state.toggleRightOverviewCollapsed).toHaveBeenCalledOnce();

    state.rightOverviewCollapsed = true;
    view.rerender(
      <MemoryRouter initialEntries={["/"]}>
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<div>页面</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText("页面").closest(".app-shell")).toHaveClass(
      "app-shell-no-overview",
    );
    expect(screen.queryByText("概览")).toBeNull();
    expect(
      screen.getByRole("button", { name: "展开右侧概览" }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("悬浮卡")).toBeNull();
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
});
