import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RoadmapMilestone } from "../types/models";
import { RoadmapPage } from "./RoadmapPage";

const hooks = vi.hoisted(() => ({
  milestones: vi.fn(),
  projects: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useRoadmapMilestonesQuery: hooks.milestones,
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
  }),
  useDeleteRoadmapMilestone: () => ({
    isPending: false,
    isError: false,
    mutate: hooks.remove,
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

describe("RoadmapPage", () => {
  afterEach(cleanup);

  beforeEach(() => {
    hooks.update.mockReset();
    hooks.remove.mockReset();
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
});
