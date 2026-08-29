import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RoadmapMilestone } from "../types/models";
import { RoadmapPage } from "./RoadmapPage";

const hooks = vi.hoisted(() => ({
  milestones: vi.fn(),
  projects: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useRoadmapMilestonesQuery: hooks.milestones,
  useProjectsQuery: hooks.projects,
  useCreateRoadmapMilestone: () => ({ isPending: false, isError: false, mutate: vi.fn() }),
  useArchiveRoadmapMilestone: () => ({ isPending: false, error: null, mutate: vi.fn() }),
  useRestoreRoadmapMilestone: () => ({ isPending: false, error: null, mutate: vi.fn() }),
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
    hooks.milestones.mockReturnValue({
      data: { items: [milestone], meta: { page: 1, pageSize: 50, total: 1 } },
      isError: false,
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
    render(<MemoryRouter><RoadmapPage /></MemoryRouter>);

    expect(screen.getByText("路线图 API")).toBeTruthy();
    expect(screen.getByText("2/4 项关联任务")).toBeTruthy();
    expect(screen.getByText("50%")).toBeTruthy();
    expect(screen.getByRole("link", { name: /opc-workspace/ })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
  });
});
