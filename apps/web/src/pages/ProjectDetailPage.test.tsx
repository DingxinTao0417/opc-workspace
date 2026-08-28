import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { Project } from "../types/models";
import { ProjectDetailPage } from "./ProjectDetailPage";

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
    total: 3,
    completed: 1,
    inProgress: 1,
    remaining: 2,
    progressPercent: 33,
    actualMinutes: 120,
  },
  invoiceCount: 0,
  availableActions: ["pause", "complete", "archive"],
};

const transition = vi.hoisted(() => vi.fn());
const projectRefetch = vi.hoisted(() => vi.fn());

vi.mock("../api/hooks", () => ({
  useClientOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useProjectQuery: () => ({
    data: project,
    isError: false,
    isPending: false,
    refetch: projectRefetch,
  }),
  useProjectEventsQuery: () => ({
    data: {
      pages: [
        {
          items: [],
          meta: { page: 1, pageSize: 20, total: 0, projectVersion: 2 },
        },
      ],
    },
    fetchNextPage: vi.fn(),
    hasNextPage: false,
    isError: false,
    isFetchingNextPage: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useProjectNotesQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0, projectVersion: 2 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useProjectArtifactsQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 6, total: 0, projectVersion: 2 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateProjectNote: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateProjectNote: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useDeleteProjectNote: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useTasksQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useTransitionProject: () => ({
    error: null,
    isPending: false,
    mutate: transition,
  }),
  useDeleteProject: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
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

describe("ProjectDetailPage", () => {
  beforeEach(() => {
    transition.mockReset();
    projectRefetch.mockReset();
    project.taskSummary.remaining = 2;
  });

  afterEach(() => {
    cleanup();
    useUiStore.setState({ newTaskOpen: false, newTaskProjectId: null });
  });

  function renderPage() {
    return render(
      <MemoryRouter initialEntries={["/projects/project-1"]}>
        <Routes>
          <Route element={<ProjectDetailPage />} path="/projects/:projectId" />
        </Routes>
      </MemoryRouter>,
    );
  }

  it("requires confirmation before completing a project with open tasks", () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "完成项目" }));
    expect(transition).not.toHaveBeenCalled();
    expect(screen.getByText("仍有 2 项任务未完成")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "确认完成项目" }));
    expect(transition).toHaveBeenCalledWith(
      {
        id: project.id,
        action: "complete",
        expectedVersion: project.version,
        confirmIncompleteTasks: true,
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("opens the task form with the current project preselected", () => {
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "新建任务" }));

    expect(useUiStore.getState()).toMatchObject({
      newTaskOpen: true,
      newTaskProjectId: project.id,
    });
  });

  it("opens confirmation when the server detects newly added open tasks", () => {
    project.taskSummary.remaining = 0;
    transition.mockImplementationOnce((_variables, options) => {
      options.onError(
        new ApiError("Open tasks require confirmation", {
          code: "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED",
        }),
      );
    });
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "完成项目" }));

    expect(projectRefetch).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "确认完成项目" })).toBeTruthy();
    expect(screen.getByText("项目存在尚未完成的任务")).toBeTruthy();
  });
});
