import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AppShell } from "./AppShell";

const state = vi.hoisted(() => ({
  sidebarCollapsed: false,
  showRightOverview: true,
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
    selector({ sidebarCollapsed: state.sidebarCollapsed }),
}));

vi.mock("./Sidebar", () => ({
  Sidebar: () => <aside>导航</aside>,
}));

vi.mock("./RightOverview", () => ({
  RightOverview: () => <aside>概览</aside>,
}));

function renderShell() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<div>页面</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("AppShell", () => {
  afterEach(() => {
    cleanup();
    state.sidebarCollapsed = false;
    state.showRightOverview = true;
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
  });
});
