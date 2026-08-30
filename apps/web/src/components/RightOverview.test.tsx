import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { RightOverview } from "./RightOverview";

const mocks = vi.hoisted(() => ({
  recent: vi.fn(),
  refetch: vi.fn(),
  income: vi.fn(),
  incomeRefetch: vi.fn(),
  roadmap: vi.fn(),
  roadmapRefetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useActiveFocusSessionQuery: () => ({
    data: { session: null },
    isError: false,
    isPending: false,
  }),
  usePauseFocusSession: () => ({ isPending: false, mutate: vi.fn() }),
  useResumeFocusSession: () => ({ isPending: false, mutate: vi.fn() }),
  useIncomeStatsQuery: mocks.income,
  useRecentClientActivitiesQuery: mocks.recent,
  useRoadmapMilestonesQuery: mocks.roadmap,
}));

vi.mock("../store/focus", () => ({
  formatFocusTime: (seconds: number) => `${seconds}s`,
  useBreakClock: () => ({ progress: 0, remainingSeconds: 0 }),
  useFocusClock: () => ({ progress: 0, remainingSeconds: 1500 }),
  useFocusCycleStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      breakDurationSeconds: 300,
      breakEndsAtMs: null,
      pauseBreak: vi.fn(),
      phase: "idle",
      resumeBreak: vi.fn(),
      taskTitle: null,
    }),
}));

vi.mock("../store/settings", () => ({
  useSettingsStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({ focusMinutes: 25, preview: null }),
}));

afterEach(() => cleanup());

beforeEach(() => {
  mocks.refetch.mockReset();
  mocks.income.mockReset();
  mocks.incomeRefetch.mockReset();
  mocks.income.mockReturnValue({
    data: {
      averageIncomeMinor: 0,
      confirmedExpenseCount: 0,
      confirmedExpenseMinor: 0,
      confirmedIncomeCount: 0,
      confirmedIncomeMinor: 0,
      currency: "CNY",
      entryCount: 0,
      netCashFlowMinor: 0,
      pendingExpenseMinor: 0,
      pendingIncomeMinor: 0,
    },
    isError: false,
    isPending: false,
    refetch: mocks.incomeRefetch,
  });
  mocks.recent.mockReset();
  mocks.recent.mockReturnValue({
    data: { items: [], meta: { page: 1, pageSize: 3, total: 0 } },
    isError: false,
    isPending: false,
    refetch: mocks.refetch,
  });
  mocks.roadmap.mockReset();
  mocks.roadmapRefetch.mockReset();
  mocks.roadmap.mockReturnValue({
    data: { items: [], meta: { page: 1, pageSize: 3, total: 0 } },
    isError: false,
    isPending: false,
    refetch: mocks.roadmapRefetch,
  });
});

function milestone(
  id: string,
  title: string,
  targetDate: string,
  status: "planned" | "active",
) {
  return {
    id,
    title,
    description: null,
    year: Number(targetDate.slice(0, 4)),
    quarter: 4,
    targetDate,
    status,
    manualOrder: 1024,
    archivedFromStatus: null,
    version: 1,
    createdAt: "2026-08-29T09:00:00Z",
    updatedAt: "2026-08-29T09:00:00Z",
    projects: [{ id: "project-1", name: "桌面交付", status: "active" }],
    taskSummary: {
      total: 4,
      completed: 2,
      inProgress: 1,
      progressPercent: 50,
    },
  };
}

describe("RightOverview monthly income", () => {
  it("shows confirmed CNY income from the local ledger", () => {
    mocks.income.mockReturnValue({
      data: {
        averageIncomeMinor: 123456,
        confirmedExpenseCount: 0,
        confirmedExpenseMinor: 0,
        confirmedIncomeCount: 2,
        confirmedIncomeMinor: 246912,
        currency: "CNY",
        entryCount: 3,
        netCashFlowMinor: 246912,
        pendingExpenseMinor: 0,
        pendingIncomeMinor: 8000,
      },
      isError: false,
      isPending: false,
      refetch: mocks.incomeRefetch,
    });

    render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );

    expect(screen.getByText("¥2,469.12")).toBeInTheDocument();
    expect(screen.getByText("2 笔已确认")).toBeInTheDocument();
  });

  it("keeps explicit loading and retry states", () => {
    mocks.income.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch: mocks.incomeRefetch,
    });
    const { rerender } = render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "收入读取失败，重试" }));
    expect(mocks.incomeRefetch).toHaveBeenCalledTimes(1);

    mocks.income.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: true,
      refetch: mocks.incomeRefetch,
    });
    rerender(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    expect(screen.getByText("正在读取本月收入…")).toBeInTheDocument();
  });
});

describe("RightOverview recent client activities", () => {
  it("shows real local activity facts and links to the owning client", () => {
    mocks.recent.mockReturnValue({
      data: {
        items: [
          {
            id: "activity-1",
            clientId: "client-1",
            clientName: "示例客户",
            clientStatus: "active",
            kind: "meeting",
            title: "复盘会议",
            body: "下一步",
            occurredAt: "2026-08-29T09:00:00Z",
            createdBy: {
              id: "owner-1",
              type: "owner",
              displayName: "Owner",
            },
            sourceType: null,
            sourceId: null,
            version: 1,
            deletedAt: null,
            deletedByActorId: null,
            deleteReason: null,
            createdAt: "2026-08-29T09:00:00Z",
            updatedAt: "2026-08-29T09:00:00Z",
            clientVersion: 2,
          },
        ],
        meta: { page: 1, pageSize: 3, total: 1 },
      },
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });

    render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );

    const link = screen.getByRole("link", {
      name: "查看客户 示例客户：复盘会议",
    });
    expect(link).toHaveAttribute("href", "/clients/client-1");
    expect(screen.getByText("示例客户")).toBeInTheDocument();
    expect(screen.queryByText("暂无客户动态")).not.toBeInTheDocument();
  });

  it("keeps explicit empty and retry states", () => {
    mocks.recent.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch: mocks.refetch,
    });
    const { rerender } = render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "读取失败，重试" }));
    expect(mocks.refetch).toHaveBeenCalledTimes(1);

    mocks.recent.mockReturnValue({
      data: { items: [], meta: { page: 1, pageSize: 3, total: 0 } },
      isError: false,
      isPending: false,
      refetch: mocks.refetch,
    });
    rerender(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    expect(screen.getByText("暂无客户动态")).toBeInTheDocument();
  });
});

describe("RightOverview upcoming roadmap milestones", () => {
  it("merges planned and active facts by target date and shows ownership", () => {
    mocks.roadmap.mockImplementation(
      (input: { status: "planned" | "active" }) => ({
        data: {
          items:
            input.status === "planned"
              ? [milestone("later", "后续节点", "2099-12-20", "planned")]
              : [milestone("near", "最近节点", "2099-12-10", "active")],
          meta: { page: 1, pageSize: 3, total: 1 },
        },
        isError: false,
        isPending: false,
        refetch: mocks.roadmapRefetch,
      }),
    );

    render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );

    const links = screen.getAllByRole("link", { name: /查看路线图节点/ });
    expect(links.map((link) => link.textContent)).toEqual([
      expect.stringContaining("最近节点"),
      expect.stringContaining("后续节点"),
    ]);
    expect(links[0]).toHaveAttribute("href", "/roadmap?milestone=near");
    expect(screen.getAllByText("桌面交付 · 2/4 任务")).toHaveLength(2);
    expect(mocks.roadmap).toHaveBeenCalledWith({
      page: 1,
      pageSize: 3,
      sort: "target_date",
      status: "planned",
    });
  });

  it("keeps explicit loading, empty, and aggregate retry states", () => {
    mocks.roadmap.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch: mocks.roadmapRefetch,
    });
    const { rerender } = render(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "节点读取失败，重试" }));
    expect(mocks.roadmapRefetch).toHaveBeenCalledTimes(2);

    mocks.roadmap.mockReturnValue({
      data: { items: [], meta: { page: 1, pageSize: 3, total: 0 } },
      isError: false,
      isPending: false,
      refetch: mocks.roadmapRefetch,
    });
    rerender(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    expect(screen.getByText("暂无未完成的路线图节点")).toBeInTheDocument();

    mocks.roadmap.mockReturnValue({
      data: undefined,
      isError: false,
      isPending: true,
      refetch: mocks.roadmapRefetch,
    });
    rerender(
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>,
    );
    expect(screen.getByText("正在读取路线图节点…")).toBeInTheDocument();
  });
});
