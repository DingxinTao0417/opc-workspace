import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
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
    expect(screen.getByRole("option", { name: /打开设置/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /自动化设置/ })).toBeVisible();
    expect(screen.getByRole("option", { name: /数据与备份/ })).toBeVisible();
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
