import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type { Project, ProjectListParams } from "../types/models";
import { ProjectsPage } from "./ProjectsPage";

const hooks = vi.hoisted(() => ({
  projects: vi.fn(),
  responsePage: null as number | null,
  total: 1,
}));
const clientFilterId = "018f0000-0000-7000-8000-000000001910";

vi.mock("../components/ClientSelect", () => ({
  ClientSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value={clientFilterId}>旧客户（已停用）</option>
    </select>
  ),
}));

const project: Project = {
  id: "project-1",
  name: "品牌官网改版",
  description: "完成设计与开发交付",
  clientId: null,
  clientName: null,
  status: "in_progress",
  startDate: "2026-08-01",
  dueDate: "2026-09-01",
  amountMinor: 680000,
  color: "#6E7BF2",
  version: 2,
  archivedFromStatus: null,
  createdAt: "2026-08-01T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  taskSummary: {
    total: 4,
    completed: 2,
    inProgress: 1,
    remaining: 2,
    progressPercent: 50,
    actualMinutes: 120,
  },
  invoiceCount: 0,
  availableActions: ["pause", "complete", "archive"],
};

vi.mock("../api/hooks", () => ({
  useProjectsQuery: hooks.projects,
  useCreateProject: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateProject: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
}));

describe("ProjectsPage", () => {
  afterEach(cleanup);

  beforeEach(() => {
    useAiWorkbenchHandoff.setState({ pendingIssue: null, pending: null });
    hooks.responsePage = null;
    hooks.total = 1;
    hooks.projects.mockImplementation((input: ProjectListParams) => ({
      data: {
        items: [project],
        meta: {
          page: hooks.responsePage ?? input.page ?? 1,
          pageSize: 12,
          total: hooks.total,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    }));
  });

  it("renders real project progress and links to the detail route", () => {
    render(
      <MemoryRouter>
        <ProjectsPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("品牌官网改版")).toBeTruthy();
    expect(screen.getByText("2/4 项任务")).toBeTruthy();
    expect(screen.getByText("50%")).toBeTruthy();
    expect(screen.getByRole("link", { name: /品牌官网改版/ })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
  });

  it("filters projects by any client status", () => {
    render(
      <MemoryRouter>
        <ProjectsPage />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("option", { name: "旧客户（已停用）" }),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("关联客户"), {
      target: { value: clientFilterId },
    });
    expect(hooks.projects).toHaveBeenLastCalledWith(
      expect.objectContaining({ clientId: clientFilterId }),
    );
  });

  it("restores a filtered page and hands only its filter identity to the agent", () => {
    hooks.total = 25;
    render(
      <MemoryRouter
        initialEntries={[
          `/projects?q=needle&status=paused&client_id=${clientFilterId}&page=2`,
        ]}
      >
        <ProjectsPage />
      </MemoryRouter>,
    );

    expect(hooks.projects).toHaveBeenLastCalledWith(
      expect.objectContaining({
        page: 2,
        query: "needle",
        status: "paused",
        clientId: clientFilterId,
        sort: "-updated_at",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "梳理项目组合" }));
    const handoff = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(handoff?.route).toBe(
      `/projects?q=needle&status=paused&client_id=${clientFilterId}&page=2`,
    );
    expect(handoff?.scopes).toEqual(["work", "clients", "actions"]);
    expect(handoff?.prompt).toContain("workspace_projects");
    expect(handoff?.prompt).not.toContain(project.description);
    expect(handoff?.prompt).not.toContain(project.amountMinor);
    expect(handoff?.prompt).not.toContain(project.name);
  });

  it("can prepare a fresh agent query even when the project list failed", () => {
    hooks.projects.mockImplementation(() => ({
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: false,
      refetch: vi.fn(),
    }));
    render(
      <MemoryRouter initialEntries={["/projects?status=paused"]}>
        <ProjectsPage />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("无法读取项目数据，请确认本地服务已连接。"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "梳理项目组合" }));
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.route).toBe(
      "/projects?status=paused",
    );
  });

  it("settles directly on the last valid project page after totals shrink", async () => {
    hooks.total = 25;
    const view = render(
      <MemoryRouter>
        <ProjectsPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.projects).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 3 }),
    );

    hooks.total = 0;
    view.rerender(
      <MemoryRouter>
        <ProjectsPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.projects).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1 }),
      ),
    );
  });
});
