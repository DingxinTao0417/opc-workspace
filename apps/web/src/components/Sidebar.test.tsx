import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { Sidebar } from "./Sidebar";

vi.mock("./FocusMiniCard", () => ({
  FocusMiniCard: () => <div data-testid="focus-mini-card" />,
}));

const hooks = vi.hoisted(() => ({
  inbox: {
    data: {
      serverNow: "2026-09-13T10:00:00Z",
      pending: 12,
      unread: 7,
      tracking: 4,
      blocked: 1,
      waitingReview: 2,
    },
    isError: false,
    isPending: false,
  },
  income: vi.fn(),
  recent: vi.fn(),
  roadmap: vi.fn(),
}));

const ui = vi.hoisted(() => ({
  sidebarCollapsed: false,
  toggleSidebarCollapsed: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useInboxStatsQuery: () => hooks.inbox,
  useIncomeStatsQuery: hooks.income,
  useRecentClientActivitiesQuery: hooks.recent,
  useRoadmapMilestonesQuery: hooks.roadmap,
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({ avatarDataUrl: null, displayName: "TAO", preview: null }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      setCommandPaletteOpen: vi.fn(),
      setSettingsOpen: vi.fn(),
      get sidebarCollapsed() {
        return ui.sidebarCollapsed;
      },
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

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 8, 13, 10, 0, 0));

  ui.sidebarCollapsed = false;
  ui.toggleSidebarCollapsed.mockReset();

  hooks.income.mockReturnValue({
    data: { confirmedIncomeCount: 2, confirmedIncomeMinor: 246912 },
    isError: false,
    isPending: false,
  });
  hooks.recent.mockReturnValue({
    data: {
      items: [
        { id: "a1", occurredAt: "2026-09-12T00:00:00Z" },
        { id: "a2", occurredAt: "2026-09-10T00:00:00Z" },
        { id: "a3", occurredAt: "2026-08-01T00:00:00Z" },
      ],
    },
    isError: false,
    isPending: false,
  });
  hooks.roadmap.mockImplementation((input: { status: string }) => ({
    data: {
      items:
        input.status === "planned"
          ? [
              { id: "m1", targetDate: "2026-09-15" },
              { id: "m2", targetDate: "2026-12-01" },
            ]
          : [{ id: "m3", targetDate: "2026-09-18" }],
    },
    isError: false,
    isPending: false,
  }));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("Sidebar navigation", () => {
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

    const roadmapLink = screen.getByRole("link", { name: /路线图/ });
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

  it("replaces the weekly execution card with the compact focus widget", () => {
    renderSidebar();

    expect(screen.getByTestId("focus-mini-card")).toBeInTheDocument();
    expect(screen.queryByText("本周执行")).not.toBeInTheDocument();
  });
});

describe("Sidebar notification badges", () => {
  it("shows due-soon roadmap milestones as a nav badge", () => {
    renderSidebar();

    expect(screen.getByTitle("2 个临期或逾期节点")).toHaveTextContent("2");
  });

  it("shows recent client activities as a nav badge", () => {
    renderSidebar();

    expect(screen.getByTitle("2 条近 7 天客户动态")).toHaveTextContent("2");
  });

  it("shows the current month confirmed income as a nav badge", () => {
    renderSidebar();

    expect(screen.getByTitle("本月已确认收入 ¥2,469.12")).toHaveTextContent(
      "¥2.5k",
    );
  });

  it("hides badges that have nothing to report", () => {
    hooks.roadmap.mockReturnValue({ data: { items: [] }, isError: false });
    hooks.recent.mockReturnValue({ data: { items: [] }, isError: false });
    hooks.income.mockReturnValue({
      data: { confirmedIncomeCount: 0, confirmedIncomeMinor: 0 },
      isError: false,
    });

    renderSidebar();

    expect(screen.queryByTitle(/临期或逾期节点/)).not.toBeInTheDocument();
    expect(screen.queryByTitle(/客户动态/)).not.toBeInTheDocument();
    expect(screen.queryByTitle(/本月已确认收入/)).not.toBeInTheDocument();
  });
});
