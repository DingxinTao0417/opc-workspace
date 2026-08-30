import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RoadmapMilestone } from "../types/models";
import { RoadmapPage } from "./RoadmapPage";

const hooks = vi.hoisted(() => ({
  milestones: vi.fn(),
  projects: vi.fn(),
  detail: vi.fn(),
  update: vi.fn(),
  updateReset: vi.fn(),
  remove: vi.fn(),
  reorder: vi.fn(),
  reorderReset: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useRoadmapMilestonesQuery: hooks.milestones,
  useRoadmapMilestoneQuery: hooks.detail,
  useProjectsQuery: hooks.projects,
  useCreateRoadmapMilestone: () => ({
    isPending: false,
    isError: false,
    mutate: vi.fn(),
  }),
  useArchiveRoadmapMilestone: () => ({
    isPending: false,
    error: null,
    mutate: vi.fn(),
  }),
  useRestoreRoadmapMilestone: () => ({
    isPending: false,
    error: null,
    mutate: vi.fn(),
  }),
  useUpdateRoadmapMilestone: () => ({
    isPending: false,
    isError: false,
    mutate: hooks.update,
    reset: hooks.updateReset,
  }),
  useDeleteRoadmapMilestone: () => ({
    isPending: false,
    isError: false,
    mutate: hooks.remove,
  }),
  useReorderRoadmapMilestones: () => ({
    isPending: false,
    isError: false,
    error: null,
    mutate: hooks.reorder,
    reset: hooks.reorderReset,
  }),
}));

vi.mock("../components/ProjectSelect", () => ({
  ProjectSelect: ({
    ariaLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">全部项目</option>
      <option value="project-2">第二个项目</option>
    </select>
  ),
}));

const milestone: RoadmapMilestone = {
  id: "milestone-1",
  title: "路线图 API",
  description: "完成本地优先的里程碑链路",
  year: 2026,
  quarter: 3,
  targetDate: "2026-09-30",
  status: "active",
  manualOrder: 1024,
  archivedFromStatus: null,
  version: 1,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T00:00:00Z",
  projects: [{ id: "project-1", name: "opc-workspace", status: "in_progress" }],
  taskSummary: { total: 4, completed: 2, inProgress: 1, progressPercent: 50 },
};

const secondMilestone: RoadmapMilestone = {
  ...milestone,
  id: "milestone-2",
  title: "桌面壳联调",
  targetDate: "2026-09-20",
  manualOrder: 2048,
  version: 3,
};

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="location-probe">{`${location.pathname}${location.search}`}</output>
  );
}

describe("RoadmapPage", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  beforeEach(() => {
    hooks.update.mockReset();
    hooks.updateReset.mockReset();
    hooks.remove.mockReset();
    hooks.reorder.mockReset();
    hooks.reorderReset.mockReset();
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id ? milestone : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    hooks.milestones.mockReturnValue({
      data: { items: [milestone], meta: { page: 1, pageSize: 50, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    hooks.projects.mockReturnValue({
      data: { items: [], meta: { page: 1, pageSize: 100, total: 0 } },
      isPending: false,
      isSuccess: true,
    });
  });

  it("renders real milestone facts and a project deep link", () => {
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("路线图 API")).toBeTruthy();
    expect(screen.getByText("2/4 项关联任务")).toBeTruthy();
    expect(screen.getByText("50%")).toBeTruthy();
    expect(screen.getByRole("link", { name: /opc-workspace/ })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
  });

  it("follows the next local quarter and resets pagination", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 30, 23, 59, 59));
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 41 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2, year: 2026, quarter: 3 }),
    );

    act(() => vi.advanceTimersByTime(1_002));

    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, year: 2026, quarter: 4 }),
    );
    expect(screen.getByLabelText("季度")).toHaveValue("4");
  });

  it("keeps a manually selected quarter when local midnight crosses a quarter", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 30, 23, 59, 59));
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("季度"), {
      target: { value: "2" },
    });
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ year: 2026, quarter: 2 }),
    );

    act(() => vi.advanceTimersByTime(1_002));

    expect(screen.getByLabelText("季度")).toHaveValue("2");
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ year: 2026, quarter: 2 }),
    );
  });

  it("syncs a closed create date without overwriting an open draft", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 30, 23, 59, 59));
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新建里程碑" }));
    const targetDate = screen.getByLabelText("目标日期");
    expect(targetDate).toHaveValue("2026-09-30");
    fireEvent.change(targetDate, { target: { value: "2026-09-15" } });

    act(() => vi.advanceTimersByTime(1_002));

    expect(
      screen.getByRole("heading", { name: "新建 2026 年 Q4 里程碑" }),
    ).toBeVisible();
    expect(screen.getByLabelText("目标日期")).toHaveValue("2026-09-15");

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "新建里程碑" }));
    expect(screen.getByLabelText("目标日期")).toHaveValue("2026-12-31");
  });

  it("edits milestone facts with the current version", () => {
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("里程碑标题"), {
      target: { value: "路线图交互收口" },
    });
    fireEvent.change(screen.getAllByLabelText("状态")[1], {
      target: { value: "achieved" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: milestone.id,
        input: expect.objectContaining({
          title: "路线图交互收口",
          status: "achieved",
          expectedVersion: milestone.version,
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("loads real detail facts and edits from the latest detail version", () => {
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "查看详情" }));
    expect(hooks.detail).toHaveBeenLastCalledWith(milestone.id);
    expect(screen.getByText("关联任务进度")).toBeTruthy();
    expect(screen.getByText("2 已完成 · 1 进行中 · 4 总计")).toBeTruthy();
    expect(screen.getByText("数据版本")).toBeTruthy();
    expect(screen.getByText("v1")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "编辑里程碑" }));
    expect(screen.getByRole("heading", { name: "编辑里程碑" })).toBeTruthy();
  });

  it("opens a URL-addressed milestone and removes only that query on close", () => {
    render(
      <MemoryRouter
        initialEntries={[
          `/roadmap?milestone=${milestone.id}&source=today-overview`,
        ]}
      >
        <RoadmapPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    expect(hooks.detail).toHaveBeenLastCalledWith(milestone.id);
    expect(screen.getByRole("dialog", { name: "里程碑详情" })).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "关闭" })[0]);

    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/roadmap?source=today-overview",
    );
    expect(hooks.detail).toHaveBeenLastCalledWith(null);
  });

  it("requires a second action before deleting an archived milestone", () => {
    const archived = {
      ...milestone,
      status: "archived" as const,
      archivedFromStatus: "active" as const,
    };
    hooks.milestones.mockReturnValue({
      data: { items: [archived], meta: { page: 1, pageSize: 50, total: 1 } },
      isError: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "永久删除" }));
    expect(
      screen.getByText("此操作不可撤销。关联项目和任务不会被删除。"),
    ).toBeTruthy();
    expect(hooks.remove).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));

    expect(hooks.remove).toHaveBeenCalledWith(
      { id: milestone.id, expectedVersion: milestone.version },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("paginates server results and resets to page one for a project filter", () => {
    hooks.milestones.mockReturnValue({
      data: { items: [milestone], meta: { page: 1, pageSize: 20, total: 41 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("1–1 / 41")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2, pageSize: 20 }),
    );

    fireEvent.change(screen.getByLabelText("关联项目筛选"), {
      target: { value: "project-2" },
    });
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({
        page: 1,
        pageSize: 20,
        projectId: "project-2",
      }),
    );
  });

  it("clamps a stale page after the server total shrinks", async () => {
    let shrunk = false;
    hooks.milestones.mockImplementation((input: { page: number }) => ({
      data: {
        items: shrunk && input.page > 1 ? [] : [milestone],
        meta: {
          page: input.page,
          pageSize: 20,
          total: shrunk ? 1 : 41,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    }));
    const view = render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
    );
    shrunk = true;
    view.rerender(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.milestones).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1 }),
      ),
    );
  });

  it("reorders the complete quarter with a keyboard alternative", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone, secondMilestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 2 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "调整顺序" }));
    const moveDown = await screen.findByRole("button", {
      name: "下移：路线图 API",
    });
    fireEvent.click(moveDown);
    fireEvent.click(screen.getByRole("button", { name: "保存顺序" }));

    expect(hooks.reorder).toHaveBeenCalledWith(
      {
        items: [
          { id: secondMilestone.id, expectedVersion: secondMilestone.version },
          { id: milestone.id, expectedVersion: milestone.version },
        ],
      },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });

  it("supports dragging one milestone onto another before saving", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone, secondMilestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 2 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "调整顺序" }));
    await screen.findByRole("button", { name: "下移：路线图 API" });
    const firstCard = screen.getByText("路线图 API").closest("article");
    const secondCard = screen.getByText("桌面壳联调").closest("article");
    expect(firstCard).toBeTruthy();
    expect(secondCard).toBeTruthy();
    fireEvent.dragStart(firstCard!);
    fireEvent.dragOver(secondCard!);
    fireEvent.drop(secondCard!);
    fireEvent.click(screen.getByRole("button", { name: "保存顺序" }));

    expect(hooks.reorder).toHaveBeenCalledWith(
      {
        items: [
          { id: secondMilestone.id, expectedVersion: secondMilestone.version },
          { id: milestone.id, expectedVersion: milestone.version },
        ],
      },
      expect.any(Object),
    );
  });

  it("moves a milestone to an exact dropped calendar date", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone, secondMilestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 2 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "调整日期" }));
    const dateTarget = await screen.findByRole("button", {
      name: "移动到 2026-09-15",
    });
    const movingCard = screen.getByText("路线图 API").closest("article");
    expect(movingCard).toBeTruthy();
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, pageSize: 100, quarter: 3 }),
    );
    fireEvent.dragStart(movingCard!);
    fireEvent.dragOver(dateTarget);
    fireEvent.drop(dateTarget);

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: milestone.id,
        input: {
          targetDate: "2026-09-15",
          expectedVersion: milestone.version,
        },
      },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });

  it("offers an exact date input as the keyboard alternative", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 1 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "调整日期" }));
    const input = await screen.findByLabelText("目标日期：路线图 API");
    fireEvent.change(input, { target: { value: "2026-09-12" } });
    fireEvent.click(
      screen.getByRole("button", { name: "应用目标日期：路线图 API" }),
    );

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: milestone.id,
        input: {
          targetDate: "2026-09-12",
          expectedVersion: milestone.version,
        },
      },
      expect.any(Object),
    );
  });

  it("does not offer a partial reorder above the API batch limit", () => {
    hooks.milestones.mockReturnValue({
      data: { items: [milestone], meta: { page: 1, pageSize: 20, total: 101 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    const button = screen.getByRole("button", { name: "调整顺序" });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute(
      "title",
      "当前季度超过 100 个里程碑，暂不支持完整批量排序。",
    );
    const dateButton = screen.getByRole("button", { name: "调整日期" });
    expect(dateButton).toBeDisabled();
    expect(dateButton).toHaveAttribute(
      "title",
      "当前季度超过 100 个里程碑，暂不支持完整日期调整。",
    );
    fireEvent.change(screen.getByLabelText("展示范围"), {
      target: { value: "year" },
    });
    const quarterButton = screen.getByRole("button", { name: "调整季度" });
    expect(quarterButton).toBeDisabled();
    expect(quarterButton).toHaveAttribute(
      "title",
      "当前年度超过 100 个里程碑，暂不支持跨季度拖拽。",
    );
  });

  it("loads an annual timeline and moves with the keyboard alternative", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone, { ...secondMilestone, quarter: 4 as const }],
          meta: { page: input.page, pageSize: input.pageSize, total: 2 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("展示范围"), {
      target: { value: "year" },
    });
    expect(hooks.milestones).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageSize: 100, quarter: undefined }),
    );
    expect(screen.getByText("Q4 · 2026")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "调整季度" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "移到下一季度：路线图 API" }),
    );

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: milestone.id,
        input: {
          year: 2026,
          quarter: 4,
          targetDate: "2026-12-31",
          expectedVersion: milestone.version,
        },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("moves a dragged annual milestone to the dropped quarter", async () => {
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [milestone, { ...secondMilestone, quarter: 4 as const }],
          meta: { page: input.page, pageSize: input.pageSize, total: 2 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("展示范围"), {
      target: { value: "year" },
    });
    fireEvent.click(screen.getByRole("button", { name: "调整季度" }));
    const movingCard = screen.getByText("路线图 API").closest("article");
    const destination = screen.getByText("Q4 · 2026").closest("section");
    expect(movingCard).toBeTruthy();
    expect(destination).toBeTruthy();
    fireEvent.dragStart(movingCard!);
    fireEvent.dragOver(destination!);
    fireEvent.drop(destination!);

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: milestone.id,
        input: {
          year: 2026,
          quarter: 4,
          targetDate: "2026-12-31",
          expectedVersion: milestone.version,
        },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("moves a Q4 milestone to next year's Q1 with the keyboard control", async () => {
    const q4Milestone = {
      ...secondMilestone,
      quarter: 4 as const,
      targetDate: "2026-12-20",
    };
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [q4Milestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 1 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("展示范围"), {
      target: { value: "year" },
    });
    fireEvent.click(screen.getByRole("button", { name: "调整季度" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "移到下一季度：桌面壳联调",
      }),
    );

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: q4Milestone.id,
        input: {
          year: 2027,
          quarter: 1,
          targetDate: "2027-03-31",
          expectedVersion: q4Milestone.version,
        },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("moves a dragged Q1 milestone to the previous year's Q4 boundary", () => {
    const q1Milestone = {
      ...milestone,
      quarter: 1 as const,
      targetDate: "2026-03-20",
    };
    hooks.milestones.mockImplementation(
      (input: { page: number; pageSize: number }) => ({
        data: {
          items: [q1Milestone],
          meta: { page: input.page, pageSize: input.pageSize, total: 1 },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    render(
      <MemoryRouter>
        <RoadmapPage />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("展示范围"), {
      target: { value: "year" },
    });
    fireEvent.click(screen.getByRole("button", { name: "调整季度" }));
    const movingCard = screen.getByText("路线图 API").closest("article");
    const destination = screen.getByRole("region", {
      name: "移动到 2025 年 Q4",
    });
    expect(movingCard).toBeTruthy();
    fireEvent.dragStart(movingCard!);
    fireEvent.dragOver(destination);
    fireEvent.drop(destination);

    expect(hooks.update).toHaveBeenCalledWith(
      {
        id: q1Milestone.id,
        input: {
          year: 2025,
          quarter: 4,
          targetDate: "2025-12-31",
          expectedVersion: q1Milestone.version,
        },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });
});
