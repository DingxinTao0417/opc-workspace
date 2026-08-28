import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { useUiStore } from "../store/ui";
import { CommandPalette } from "./CommandPalette";

const mocks = vi.hoisted(() => ({
  searchQuery: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useSearchQuery: mocks.searchQuery,
}));

const taskResult = {
  resourceType: "task" as const,
  resourceId: "task-search-result",
  title: "跨页交付任务",
  subtitle: "交付项目",
  matchedFields: ["title"],
  route: "/tasks/task-search-result",
  status: "in_progress",
  updatedAt: "2026-08-28T01:00:00Z",
};

function CurrentLocation() {
  return (
    <output data-testid="current-location">{useLocation().pathname}</output>
  );
}

describe("CommandPalette", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
    useUiStore.setState({
      commandPaletteOpen: false,
      settingsOpen: false,
      settingsModule: "general",
      taskDetailId: null,
    });
  });

  it("debounces unified search and opens the exact stable resource route", () => {
    vi.useFakeTimers();
    mocks.searchQuery.mockImplementation((input: { q?: string }) => ({
      data: input.q
        ? { items: [taskResult], meta: { page: 1, pageSize: 12, total: 1 } }
        : undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    }));
    useUiStore.setState({ commandPaletteOpen: true, taskDetailId: null });

    render(
      <MemoryRouter>
        <CommandPalette />
        <CurrentLocation />
      </MemoryRouter>,
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "搜索页面、业务或操作" }),
      {
        target: { value: "跨页" },
      },
    );
    expect(screen.getByText("正在搜索本地业务…")).toBeVisible();
    act(() => vi.advanceTimersByTime(200));

    expect(mocks.searchQuery).toHaveBeenLastCalledWith(
      { q: "跨页", page: 1, pageSize: 12 },
      true,
    );
    fireEvent.click(screen.getByRole("option", { name: /跨页交付任务/ }));
    expect(screen.getByTestId("current-location")).toHaveTextContent(
      "/tasks/task-search-result",
    );
    expect(useUiStore.getState().commandPaletteOpen).toBe(false);
  });

  it("keeps page navigation but removes unimplemented finance commands", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );

    expect(screen.getByRole("option", { name: /今日/ })).toBeVisible();
    expect(screen.queryByRole("option", { name: /收入/ })).toBeNull();
    expect(screen.queryByRole("option", { name: /发票/ })).toBeNull();
    expect(screen.getByRole("option", { name: /打开设置/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /数据与备份/ })).toBeVisible();
  });

  it("opens the requested settings module directly", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("option", { name: /专注设置/ }));

    expect(useUiStore.getState()).toMatchObject({
      commandPaletteOpen: false,
      settingsOpen: true,
      settingsModule: "focus",
    });
  });

  it("opens the data and backup settings module directly", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("option", { name: /数据与备份/ }));

    expect(useUiStore.getState()).toMatchObject({
      commandPaletteOpen: false,
      settingsOpen: true,
      settingsModule: "data",
    });
  });

  it("keeps input focus while arrows select an option and Enter executes it", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const input = screen.getByRole("combobox", {
      name: "搜索页面、业务或操作",
    });
    fireEvent.change(input, { target: { value: "设置" } });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(input).toHaveAttribute(
      "aria-activedescendant",
      "command-result-settings-focus",
    );

    fireEvent.keyDown(input, { key: "Enter" });
    expect(useUiStore.getState()).toMatchObject({
      commandPaletteOpen: false,
      settingsOpen: true,
      settingsModule: "focus",
    });
  });

  it("traps focus and restores it to the opener after closing", async () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    const opener = document.createElement("button");
    opener.textContent = "打开命令";
    document.body.appendChild(opener);
    opener.focus();
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const input = screen.getByRole("combobox", {
      name: "搜索页面、业务或操作",
    });
    await waitFor(() => expect(input).toHaveFocus());

    fireEvent.keyDown(input, { key: "Tab", shiftKey: true });
    expect(input).toHaveFocus();
    fireEvent.keyDown(input, { key: "Tab" });
    expect(input).toHaveFocus();

    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(opener).toHaveFocus());
    expect(useUiStore.getState().commandPaletteOpen).toBe(false);
    opener.remove();
  });

  it("does not execute the active command while an IME composition is active", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    fireEvent.keyDown(screen.getByRole("combobox"), {
      isComposing: true,
      key: "Enter",
    });

    expect(useUiStore.getState().commandPaletteOpen).toBe(true);
    expect(useUiStore.getState().taskDetailId).toBeNull();
  });

  it("shows a retryable unified-search error without hiding local commands", () => {
    vi.useFakeTimers();
    mocks.searchQuery.mockImplementation((input: { q?: string }) => ({
      data: undefined,
      isError: Boolean(input.q),
      isPending: false,
      refetch: mocks.refetch,
    }));
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    fireEvent.change(
      screen.getByRole("combobox", { name: "搜索页面、业务或操作" }),
      {
        target: { value: "不存在的任务" },
      },
    );
    act(() => vi.advanceTimersByTime(200));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "本地业务搜索暂时不可用",
    );
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });
});
