import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  clearCommandRecentsForTests,
  loadCommandRecents,
  recordCommandRecent,
} from "../store/commandRecents";
import { useUiStore } from "../store/ui";
import { CommandPalette } from "./CommandPalette";
import { Modal } from "./Modal";

const mocks = vi.hoisted(() => ({
  healthQuery: vi.fn(),
  healthRefetch: vi.fn(),
  searchQuery: vi.fn(),
  refetch: vi.fn(),
  getTask: vi.fn(),
  getProject: vi.fn(),
  getClient: vi.fn(),
  getInboxItem: vi.fn(),
  getInvoice: vi.fn(),
  getRoadmapMilestone: vi.fn(),
  getContentItem: vi.fn(),
}));

vi.mock("../api/client", async (importActual) => {
  const actual = await importActual<typeof import("../api/client")>();
  return {
    ...actual,
    getTask: mocks.getTask,
    getProject: mocks.getProject,
    getClient: mocks.getClient,
    getInboxItem: mocks.getInboxItem,
    getInvoice: mocks.getInvoice,
    getRoadmapMilestone: mocks.getRoadmapMilestone,
    getContentItem: mocks.getContentItem,
  };
});

vi.mock("../api/hooks", () => ({
  useHealthQuery: mocks.healthQuery,
  useSearchQuery: mocks.searchQuery,
}));

const healthyRuntime = {
  status: "ok",
  app: { name: "opc-workspace", version: "0.1.7", commit: "abc123" },
  api: { version: "v1" },
  schema: { version: 43 },
};

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
  const location = useLocation();
  return (
    <output data-testid="current-location">
      {location.pathname}
      {location.search}
    </output>
  );
}

function PaletteOverModalHarness() {
  const [modalOpen, setModalOpen] = useState(false);
  return (
    <>
      <button onClick={() => setModalOpen(true)} type="button">
        打开底层弹窗
      </button>
      <Modal
        onClose={() => setModalOpen(false)}
        open={modalOpen}
        title="底层弹窗"
      >
        <button
          onClick={() => useUiStore.getState().setCommandPaletteOpen(true)}
          type="button"
        >
          打开命令面板
        </button>
      </Modal>
      <CommandPalette />
    </>
  );
}

describe("CommandPalette", () => {
  beforeEach(() => {
    mocks.healthQuery.mockReturnValue({
      data: healthyRuntime,
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.clearAllMocks();
    clearCommandRecentsForTests();
    useUiStore.setState({
      commandPaletteOpen: false,
      settingsOpen: false,
      settingsModule: "general",
      taskDetailId: null,
    });
    document.body.style.overflow = "";
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
    expect(screen.getByText("正在准备本地搜索…")).toBeVisible();
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

  it("includes commands for all searchable delivered pages", () => {
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
    expect(screen.getByRole("option", { name: /发票/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /路线图/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /内容日历/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /本地提醒/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /打开设置/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /自动化设置/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /数据与备份/ })).toBeVisible();
  });

  it("shows the actual local runtime versions while health is current", () => {
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

    expect(screen.getByRole("status")).toHaveTextContent("本地服务在线");
    expect(screen.getByRole("status")).toHaveTextContent(
      "v0.1.7 · API v1 · Schema v43",
    );
    expect(mocks.healthQuery).toHaveBeenLastCalledWith(true);
  });

  it("waits for initial health before searching and hides cached business results", () => {
    vi.useFakeTimers();
    mocks.healthQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isFetching: true,
      isPending: true,
      refetch: mocks.healthRefetch,
    });
    mocks.searchQuery.mockReturnValue({
      data: {
        items: [taskResult],
        meta: { page: 1, pageSize: 12, total: 1 },
      },
      isError: false,
      isPending: true,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "正在检查本地服务状态",
    );
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "跨页" },
    });
    act(() => vi.advanceTimersByTime(200));

    expect(mocks.searchQuery).toHaveBeenLastCalledWith(
      { q: "跨页", page: 1, pageSize: 12 },
      false,
    );
    expect(screen.getByText("等待本地服务状态…")).toBeVisible();
    expect(screen.queryByText(taskResult.title)).toBeNull();
  });

  it("keeps fixed recent commands and local keyboard navigation when initial health fails", () => {
    recordCommandRecent([], { kind: "command", commandId: "today" });
    recordCommandRecent(loadCommandRecents(), {
      kind: "resource",
      resourceType: "task",
      resourceId: "cached-task",
    });
    mocks.healthQuery.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    });
    mocks.searchQuery.mockReturnValue({
      data: {
        items: [taskResult],
        meta: { page: 1, pageSize: 12, total: 1 },
      },
      isError: false,
      isPending: true,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter initialEntries={["/projects"]}>
        <CommandPalette />
        <CurrentLocation />
      </MemoryRouter>,
    );

    expect(screen.getByText("最近使用")).toBeVisible();
    expect(mocks.getTask).not.toHaveBeenCalled();
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "今日" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByTestId("current-location")).toHaveTextContent("/today");
  });

  it("offers health recheck and opens runtime diagnostics after initial failure", () => {
    vi.useFakeTimers();
    mocks.healthQuery.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    });
    mocks.searchQuery.mockReturnValue({
      data: {
        items: [taskResult],
        meta: { page: 1, pageSize: 12, total: 1 },
      },
      isError: false,
      isPending: true,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status")).toHaveTextContent(
      "本地服务不可用，业务搜索已暂停",
    );
    expect(screen.getByRole("option", { name: /今日/ })).toBeVisible();
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "跨页" },
    });
    act(() => vi.advanceTimersByTime(200));
    expect(
      screen.getByText("本地业务搜索不可用；页面与设置命令仍可使用。"),
    ).toBeVisible();
    expect(screen.queryByText(taskResult.title)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重新检查" }));
    expect(mocks.healthRefetch).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: "打开运行诊断" }));
    expect(useUiStore.getState()).toMatchObject({
      commandPaletteOpen: false,
      settingsModule: "diagnostics",
      settingsOpen: true,
    });
  });

  it("lets a focused health action handle Enter without running the active command", () => {
    mocks.healthQuery.mockReturnValue({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    });
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      isFetching: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter initialEntries={["/projects"]}>
        <CommandPalette />
        <CurrentLocation />
      </MemoryRouter>,
    );

    const recheck = screen.getByRole("button", { name: "重新检查" });
    recheck.focus();
    expect(fireEvent.keyDown(recheck, { key: "Enter" })).toBe(true);
    expect(screen.getByTestId("current-location")).toHaveTextContent(
      "/projects",
    );
    expect(useUiStore.getState().commandPaletteOpen).toBe(true);

    fireEvent.click(recheck);
    expect(mocks.healthRefetch).toHaveBeenCalledOnce();
  });

  it("keeps the recheck action mounted while pending and restores focus before removing it", async () => {
    let finishRecheck: (() => void) | undefined;
    const recheckResult = new Promise<void>((resolve) => {
      finishRecheck = resolve;
    });
    let healthState = {
      data: undefined as typeof healthyRuntime | undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthRefetch.mockReturnValue(recheckResult);
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      isFetching: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const recheck = screen.getByRole("button", { name: "重新检查" });
    recheck.focus();
    fireEvent.click(recheck);

    healthState = {
      ...healthState,
      isError: false,
      isFetching: true,
      isPending: true,
    };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "检查中…" })).toBeVisible();
    expect(screen.getByRole("combobox")).toHaveFocus();

    healthState = {
      ...healthState,
      data: healthyRuntime,
      isFetching: false,
      isPending: false,
    };
    await act(async () => finishRecheck?.());

    expect(screen.queryByRole("button", { name: "检查中…" })).toBeNull();
    expect(screen.getByRole("combobox")).toHaveFocus();
  });

  it("keeps externally refreshed health actions mounted until focus leaves safely", async () => {
    let healthState = {
      data: healthyRuntime,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      isFetching: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const recheck = screen.getByRole("button", { name: "重新检查" });
    recheck.focus();

    healthState = { ...healthState, isError: false, isFetching: true };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "检查中…" })).toHaveFocus();
    expect(screen.getByRole("button", { name: "检查中…" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );

    healthState = { ...healthState, isFetching: false };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(screen.getByRole("button", { name: "重新检查" })).toHaveFocus();

    const diagnostics = screen.getByRole("button", {
      name: "打开运行诊断",
    });
    diagnostics.focus();
    fireEvent.keyDown(diagnostics, { key: "Tab" });
    expect(screen.getByRole("combobox")).toHaveFocus();
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "打开运行诊断" })).toBeNull(),
    );
  });

  it("retains cached health facts with a stale warning and refreshes the current search after recovery", () => {
    vi.useFakeTimers();
    let healthState = {
      data: healthyRuntime,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockImplementation(
      (input: { q?: string }, enabled: boolean) => ({
        data:
          enabled && input.q
            ? {
                items: [taskResult],
                meta: { page: 1, pageSize: 12, total: 1 },
              }
            : undefined,
        isError: false,
        isPending: false,
        refetch: mocks.refetch,
      }),
    );
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "本地服务状态可能已过期",
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "上次确认 v0.1.7 · API v1 · Schema v43",
    );
    fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "跨页" },
    });
    act(() => vi.advanceTimersByTime(200));
    expect(screen.getByRole("option", { name: /跨页交付任务/ })).toBeVisible();
    expect(mocks.searchQuery).toHaveBeenLastCalledWith(
      { q: "跨页", page: 1, pageSize: 12 },
      true,
    );

    healthState = { ...healthState, isError: false };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("本地服务在线");
    expect(mocks.refetch).toHaveBeenCalledOnce();
  });

  it("does not duplicate or replay an obsolete search when stale health recovers", () => {
    vi.useFakeTimers();
    let searchFetching = false;
    let healthState = {
      data: healthyRuntime,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockImplementation(() => ({
      data: undefined,
      isError: false,
      isPending: false,
      isFetching: searchFetching,
      refetch: mocks.refetch,
    }));
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "旧查询" } });
    act(() => vi.advanceTimersByTime(200));
    fireEvent.change(input, { target: { value: "当前查询" } });

    healthState = { ...healthState, isError: false };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(mocks.refetch).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(200));
    expect(mocks.searchQuery).toHaveBeenLastCalledWith(
      { q: "当前查询", page: 1, pageSize: 12 },
      true,
    );

    healthState = { ...healthState, isError: true };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    searchFetching = true;
    healthState = { ...healthState, isError: false };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("waits for cached health to recover before resolving recent resources", async () => {
    recordCommandRecent([], {
      kind: "resource",
      resourceType: "task",
      resourceId: "recovered-task",
    });
    mocks.getTask.mockResolvedValue({
      id: "recovered-task",
      title: "恢复后的最近任务",
      status: "in_progress",
    });
    let healthState = {
      data: healthyRuntime,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      isFetching: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    expect(mocks.getTask).not.toHaveBeenCalled();

    healthState = { ...healthState, isError: false };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );

    expect(
      await screen.findByRole("option", { name: /恢复后的最近任务/ }),
    ).toBeVisible();
    expect(mocks.getTask).toHaveBeenCalledOnce();
  });

  it("enables only the current search when initial health recovers", () => {
    vi.useFakeTimers();
    let healthState = {
      data: undefined as typeof healthyRuntime | undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: mocks.healthRefetch,
    };
    mocks.healthQuery.mockImplementation(() => healthState);
    mocks.searchQuery.mockImplementation(
      (_input: { q?: string }, _enabled: boolean) => ({
        data: undefined,
        isError: false,
        isPending: false,
        refetch: mocks.refetch,
      }),
    );
    useUiStore.setState({ commandPaletteOpen: true });

    const view = render(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );
    const input = screen.getByRole("combobox");
    fireEvent.change(input, { target: { value: "旧查询" } });
    act(() => vi.advanceTimersByTime(200));
    fireEvent.change(input, { target: { value: "当前查询" } });
    act(() => vi.advanceTimersByTime(200));

    healthState = {
      ...healthState,
      data: healthyRuntime,
      isError: false,
    };
    view.rerender(
      <MemoryRouter>
        <CommandPalette />
      </MemoryRouter>,
    );

    const enabledCalls = mocks.searchQuery.mock.calls.filter(
      ([, enabled]) => enabled,
    );
    expect(enabledCalls).toEqual([
      [{ q: "当前查询", page: 1, pageSize: 12 }, true],
    ]);
    expect(mocks.refetch).not.toHaveBeenCalled();
  });

  it("opens the scheduled local Reminder list through its fixed command", () => {
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    useUiStore.setState({ commandPaletteOpen: true });

    render(
      <MemoryRouter initialEntries={["/today"]}>
        <CommandPalette />
        <CurrentLocation />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("option", { name: /本地提醒/ }));

    expect(screen.getByTestId("current-location")).toHaveTextContent(
      "/inbox?reminders=scheduled",
    );
    expect(useUiStore.getState().commandPaletteOpen).toBe(false);
  });

  it.each([
    {
      result: {
        resourceType: "invoice" as const,
        resourceId: "invoice-search-result",
        title: "INV-SEARCH-001",
        subtitle: "检索客户",
        matchedFields: ["invoice_number"],
        route: "/invoices/invoice-search-result",
        status: "sent",
        updatedAt: "2026-08-28T01:00:00Z",
      },
      expected: "/invoices/invoice-search-result",
    },
    {
      result: {
        resourceType: "roadmap_milestone" as const,
        resourceId: "milestone-search-result",
        title: "唯一检索里程碑",
        subtitle: "检索项目",
        matchedFields: ["title"],
        route: "/roadmap?milestone=milestone-search-result",
        status: "active",
        updatedAt: "2026-08-28T01:00:00Z",
      },
      expected: "/roadmap?milestone=milestone-search-result",
    },
    {
      result: {
        resourceType: "content_item" as const,
        resourceId: "content-search-result",
        title: "唯一检索内容项",
        subtitle: "微信公众号",
        matchedFields: ["title"],
        route: "/content-calendar?item=content-search-result",
        status: "scheduled",
        updatedAt: "2026-08-28T01:00:00Z",
      },
      expected: "/content-calendar?item=content-search-result",
    },
  ])(
    "opens $result.resourceType search results on their stable route",
    ({ result, expected }) => {
      vi.useFakeTimers();
      mocks.searchQuery.mockImplementation((input: { q?: string }) => ({
        data: input.q
          ? { items: [result], meta: { page: 1, pageSize: 12, total: 1 } }
          : undefined,
        isError: false,
        isPending: false,
        refetch: mocks.refetch,
      }));
      useUiStore.setState({ commandPaletteOpen: true });

      render(
        <MemoryRouter>
          <CommandPalette />
          <CurrentLocation />
        </MemoryRouter>,
      );
      fireEvent.change(screen.getByRole("combobox"), {
        target: { value: "唯一检索" },
      });
      act(() => vi.advanceTimersByTime(200));
      fireEvent.click(
        screen.getByRole("option", { name: new RegExp(result.title) }),
      );

      expect(screen.getByTestId("current-location")).toHaveTextContent(
        expected,
      );
    },
  );

  it("shows and reruns bounded local recent commands before fixed commands", () => {
    recordCommandRecent([], { kind: "command", commandId: "today" });
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
        <CurrentLocation />
      </MemoryRouter>,
    );

    expect(screen.getByText("最近使用")).toBeVisible();
    fireEvent.click(screen.getAllByRole("option", { name: /今日/ })[0]);
    expect(screen.getByTestId("current-location")).toHaveTextContent("/today");
  });

  it("removes a stale recent resource after the local API confirms 404", async () => {
    recordCommandRecent([], {
      kind: "resource",
      resourceType: "task",
      resourceId: "deleted-task",
    });
    mocks.getTask.mockRejectedValue(
      new ApiError("任务不存在", { status: 404, code: "TASK_NOT_FOUND" }),
    );
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

    await waitFor(() =>
      expect(mocks.getTask).toHaveBeenCalledWith("deleted-task"),
    );
    await waitFor(() => expect(loadCommandRecents()).toEqual([]));
  });

  it("reloads new recent resource types from their local detail APIs", async () => {
    const now = Date.now();
    let recents = recordCommandRecent(
      [],
      {
        kind: "resource",
        resourceType: "invoice",
        resourceId: "invoice-recent",
      },
      now - 2,
    );
    recents = recordCommandRecent(
      recents,
      {
        kind: "resource",
        resourceType: "roadmap_milestone",
        resourceId: "milestone-recent",
      },
      now - 1,
    );
    recordCommandRecent(
      recents,
      {
        kind: "resource",
        resourceType: "content_item",
        resourceId: "content-recent",
      },
      now,
    );
    mocks.getInvoice.mockResolvedValue({
      id: "invoice-recent",
      invoiceNumber: "INV-RECENT",
      clientName: "最近客户",
      status: "sent",
    });
    mocks.getRoadmapMilestone.mockResolvedValue({
      id: "milestone-recent",
      title: "最近里程碑",
      status: "active",
    });
    mocks.getContentItem.mockResolvedValue({
      id: "content-recent",
      title: "最近内容项",
      platform: "微信公众号",
      status: "scheduled",
    });
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
        <CurrentLocation />
      </MemoryRouter>,
    );

    expect(
      await screen.findByRole("option", { name: /最近内容项/ }),
    ).toBeVisible();
    expect(mocks.getInvoice).toHaveBeenCalledWith("invoice-recent");
    expect(mocks.getRoadmapMilestone).toHaveBeenCalledWith("milestone-recent");
    expect(mocks.getContentItem).toHaveBeenCalledWith("content-recent");
    fireEvent.click(screen.getByRole("option", { name: /最近里程碑/ }));
    expect(screen.getByTestId("current-location")).toHaveTextContent(
      "/roadmap?milestone=milestone-recent",
    );
  });

  it("removes a stale recent content item after its detail API confirms 404", async () => {
    recordCommandRecent([], {
      kind: "resource",
      resourceType: "content_item",
      resourceId: "deleted-content",
    });
    mocks.getContentItem.mockRejectedValue(
      new ApiError("内容项不存在", {
        status: 404,
        code: "CONTENT_ITEM_NOT_FOUND",
      }),
    );
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

    await waitFor(() =>
      expect(mocks.getContentItem).toHaveBeenCalledWith("deleted-content"),
    );
    await waitFor(() => expect(loadCommandRecents()).toEqual([]));
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

  it("closes only the top command palette and preserves the shared modal lock", async () => {
    document.body.style.overflow = "scroll";
    mocks.searchQuery.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    render(
      <MemoryRouter>
        <PaletteOverModalHarness />
      </MemoryRouter>,
    );
    const pageOpener = screen.getByRole("button", {
      name: "打开底层弹窗",
    });
    pageOpener.focus();
    fireEvent.click(pageOpener);
    const paletteOpener = await screen.findByRole("button", {
      name: "打开命令面板",
    });
    paletteOpener.focus();
    fireEvent.click(paletteOpener);

    const palette = await screen.findByRole("dialog", { name: "命令面板" });
    expect(palette.closest(".command-root")?.parentElement).toBe(document.body);
    const modal = screen.getByRole("dialog", {
      name: "底层弹窗",
      hidden: true,
    });
    expect(modal.closest(".modal-root")).toHaveAttribute("inert");
    expect(modal).not.toHaveAttribute("aria-modal");
    expect(palette).toHaveAttribute("aria-modal", "true");
    expect(document.body.style.overflow).toBe("hidden");
    const input = screen.getByRole("combobox", {
      name: "搜索页面、业务或操作",
    });
    await waitFor(() => expect(input).toHaveFocus());

    fireEvent.keyDown(input, { key: "Escape" });

    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "命令面板" })).toBeNull(),
    );
    expect(screen.getByRole("dialog", { name: "底层弹窗" })).toHaveAttribute(
      "aria-modal",
      "true",
    );
    await waitFor(() => expect(paletteOpener).toHaveFocus());
    expect(document.body.style.overflow).toBe("hidden");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(pageOpener).toHaveFocus());
    expect(document.body.style.overflow).toBe("scroll");
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
