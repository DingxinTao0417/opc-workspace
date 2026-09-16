import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppShell } from "./AppShell";

const state = vi.hoisted(() => ({
  sidebarCollapsed: false,
  rightOverviewCollapsed: false,
  showRightOverview: true,
  rightOverviewWidth: 280,
  setRightOverviewWidth: vi.fn(),
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
      setRightOverviewWidth: state.setRightOverviewWidth,
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
    state.rightOverviewWidth = 280;
    state.setRightOverviewWidth.mockReset();
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

  it("drives the overview width from the shell variable and the resize handle", () => {
    state.rightOverviewWidth = 320;
    renderShell();

    expect(screen.getByText("页面").closest(".app-shell")).toHaveStyle({
      "--right-overview-width": "320px",
    });

    const handle = screen.getByRole("separator", {
      name: "调整右侧概览宽度",
    });
    expect(handle).toHaveAttribute("aria-valuenow", "320");
    expect(handle).toHaveAttribute("aria-valuemin", "240");
    expect(handle).toHaveAttribute("aria-valuemax", "480");

    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(332);

    fireEvent.keyDown(handle, { key: "ArrowRight", shiftKey: true });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(272);

    fireEvent.keyDown(handle, { key: "Home" });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(280);
  });

  it("resizes the overview by dragging the separator", () => {
    state.rightOverviewWidth = 300;
    renderShell();

    const handle = screen.getByRole("separator", {
      name: "调整右侧概览宽度",
    });
    // jsdom has no PointerEvent, so its fallback Event drops coordinates.
    // Assign them so the real handler path runs.
    const pointer = (type: string, init: Record<string, number>) => {
      const event = new Event(type, { bubbles: true });
      Object.assign(event, init);
      fireEvent(handle, event);
    };

    pointer("pointerdown", { button: 0, clientX: 500, pointerId: 1 });
    pointer("pointermove", { clientX: 460, pointerId: 1 });
    expect(state.setRightOverviewWidth).toHaveBeenLastCalledWith(340);

    pointer("pointerup", { pointerId: 1 });
    state.setRightOverviewWidth.mockClear();
    pointer("pointermove", { clientX: 300, pointerId: 1 });
    expect(state.setRightOverviewWidth).not.toHaveBeenCalled();
  });

  it("omits the resize handle when the overview is hidden", () => {
    state.showRightOverview = false;
    renderShell();

    expect(
      screen.queryByRole("separator", { name: "调整右侧概览宽度" }),
    ).toBeNull();
  });
});
