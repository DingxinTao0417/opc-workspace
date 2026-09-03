import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { Sidebar } from "./Sidebar";

const hooks = vi.hoisted(() => ({
  inbox: {
    data: {
      serverNow: "2026-08-28T10:00:00Z",
      pending: 12,
      unread: 7,
      tracking: 4,
      blocked: 1,
      waitingReview: 2,
    },
    isError: false,
    isPending: false,
  },
  weekInput: vi.fn(),
  week: {
    data: undefined as
      | {
          plannedFrom: string;
          plannedTo: string;
          taskCount: number;
          completedCount: number;
          completedPercent: number;
        }
      | undefined,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  },
}));

const ui = vi.hoisted(() => ({
  sidebarCollapsed: false,
  toggleSidebarCollapsed: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: () => hooks.inbox,
  useSidebarWeekTasksQuery: (input: unknown) => {
    hooks.weekInput(input);
    return hooks.week;
  },
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: unknown) => unknown) =>
    selector({ displayName: "OPC", avatarDataUrl: "", preview: null }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({
      setCommandPaletteOpen: vi.fn(),
      setSettingsOpen: vi.fn(),
      sidebarCollapsed: ui.sidebarCollapsed,
      toggleSidebarCollapsed: ui.toggleSidebarCollapsed,
    }),
}));

function renderSidebar() {
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  );
}

describe("Sidebar", () => {
  beforeEach(() => {
    hooks.week.data = {
      plannedFrom: "2026-08-24",
      plannedTo: "2026-08-30",
      taskCount: 0,
      completedCount: 0,
      completedPercent: 0,
    };
    hooks.week.isError = false;
    hooks.week.isPending = false;
    hooks.week.refetch.mockReset();
    hooks.weekInput.mockReset();
    ui.sidebarCollapsed = false;
    ui.toggleSidebarCollapsed.mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("shows the current actionable Inbox count", () => {
    renderSidebar();

    expect(screen.getByLabelText("12 项待处理")).toHaveTextContent("12");
  });

  it("collapses from the brand control and keeps an accessible restore entry", () => {
    const view = renderSidebar();

    fireEvent.click(screen.getByRole("button", { name: "收起侧边栏" }));
    expect(ui.toggleSidebarCollapsed).toHaveBeenCalledOnce();

    ui.sidebarCollapsed = true;
    view.rerender(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );

    expect(screen.getByLabelText("主导航")).toHaveClass("is-collapsed");
    expect(screen.getByRole("button", { name: "展开侧边栏" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.getByRole("link", { name: "AI 助手" })).toHaveAttribute(
      "title",
      "AI 助手",
    );
  });

  it("shows delivered Roadmap and Content Calendar navigation without future badges", () => {
    renderSidebar();

    const roadmapLink = screen.getByRole("link", { name: "路线图" });
    const contentLink = screen.getByRole("link", { name: "内容日历" });
    expect(screen.getByText("规划与内容")).toBeVisible();
    expect(roadmapLink).toHaveAttribute("href", "/roadmap");
    expect(contentLink).toHaveAttribute("href", "/content-calendar");
    expect(
      roadmapLink.compareDocumentPosition(contentLink) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(screen.queryByText("后续")).toBeNull();
  });

  it("requests the browser-local Monday through Sunday across a month boundary", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 1, 12, 0, 0));

    renderSidebar();

    expect(hooks.weekInput).toHaveBeenCalledWith({
      plannedFrom: "2026-02-23",
      plannedTo: "2026-03-01",
    });
  });

  it("moves the weekly query range when local Monday begins", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 1, 23, 59, 59));

    renderSidebar();

    expect(hooks.weekInput).toHaveBeenLastCalledWith({
      plannedFrom: "2026-02-23",
      plannedTo: "2026-03-01",
    });

    act(() => vi.advanceTimersByTime(1_002));

    expect(hooks.weekInput).toHaveBeenLastCalledWith({
      plannedFrom: "2026-03-02",
      plannedTo: "2026-03-08",
    });
  });

  it("renders weekly progress from the dedicated read model", () => {
    hooks.week.data = {
      plannedFrom: "2026-08-24",
      plannedTo: "2026-08-30",
      taskCount: 4,
      completedCount: 3,
      completedPercent: 75,
    };

    renderSidebar();

    expect(screen.getByLabelText("本周完成度 75%")).toBeVisible();
    expect(screen.getByText("3 / 4 项")).toBeVisible();
  });

  it("distinguishes loading, retryable errors, and an empty week", () => {
    hooks.week.data = undefined;
    hooks.week.isPending = true;
    const view = renderSidebar();

    expect(screen.getByRole("status")).toHaveTextContent("正在加载本周任务");
    expect(screen.queryByText("暂无任务")).toBeNull();

    hooks.week.isPending = false;
    hooks.week.isError = true;
    view.rerender(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("无法读取本周任务");
    expect(screen.queryByText("暂无任务")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(hooks.week.refetch).toHaveBeenCalledOnce();

    hooks.week.isError = false;
    hooks.week.data = {
      plannedFrom: "2026-08-24",
      plannedTo: "2026-08-30",
      taskCount: 0,
      completedCount: 0,
      completedPercent: 0,
    };
    view.rerender(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );
    expect(screen.getByRole("status")).toHaveTextContent("本周暂无已排期任务");
    expect(screen.queryByText("暂无任务")).toBeNull();
  });
});
