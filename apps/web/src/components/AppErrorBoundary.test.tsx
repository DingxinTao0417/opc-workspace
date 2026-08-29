import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import { RoutedAppErrorBoundary } from "./AppErrorBoundary";

let shouldThrow = true;

function BrokenPage() {
  if (shouldThrow) throw new Error("secret customer detail");
  return <p>页面已恢复</p>;
}

function TestRoutes() {
  return (
    <RoutedAppErrorBoundary>
      <Routes>
        <Route element={<BrokenPage />} path="/broken" />
        <Route element={<p>今日页面可用</p>} path="/today" />
      </Routes>
    </RoutedAppErrorBoundary>
  );
}

describe("RoutedAppErrorBoundary", () => {
  afterEach(() => {
    cleanup();
    shouldThrow = true;
    useUiStore.setState({ settingsOpen: false, settingsModule: "general" });
    vi.restoreAllMocks();
  });

  it("shows a safe fallback, opens diagnostics and retries without raw errors", () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    render(
      <MemoryRouter initialEntries={["/broken"]}>
        <TestRoutes />
      </MemoryRouter>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("当前页面未能正常显示");
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "secret customer detail",
    );

    fireEvent.click(screen.getByRole("button", { name: "打开运行诊断" }));
    expect(useUiStore.getState()).toMatchObject({
      settingsOpen: true,
      settingsModule: "diagnostics",
    });

    shouldThrow = false;
    fireEvent.click(screen.getByRole("button", { name: "重新渲染" }));
    expect(screen.getByText("页面已恢复")).toBeVisible();
  });

  it("resets the failure when navigating to a different route", () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    render(
      <MemoryRouter initialEntries={["/broken"]}>
        <TestRoutes />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "返回今日" }));
    expect(screen.getByText("今日页面可用")).toBeVisible();
  });
});
