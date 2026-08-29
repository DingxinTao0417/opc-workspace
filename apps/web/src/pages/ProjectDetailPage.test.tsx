import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { Project, Task } from "../types/models";
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
const taskPageInput = vi.hoisted(() => vi.fn());

const rootTask: Task = {
  id: "task-1",
  title: "完成首页设计",
  description: "",
  kind: "work",
  status: "in_progress",
  priority: "P1",
  projectId: project.id,
  projectName: project.name,
  parentTaskId: null,
  completionCriteria: "",
  reviewPolicy: "none",
  blockedReason: null,
  blockedAt: null,
  blockedFromStatus: null,
  dueDate: null,
  plannedDate: null,
  estimatedMinutes: 120,
  actualMinutes: 30,
  manualOrder: 1,
  version: 1,
  subtaskTotal: 1,
  subtaskCompleted: 0,
  createdAt: "2026-08-27T08:00:00Z",
  updatedAt: "2026-08-27T08:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

const childTask: Task = {
  ...rootTask,
  id: "task-2",
  title: "校对移动端间距",
  parentTaskId: rootTask.id,
  parentTaskTitle: rootTask.title,
  subtaskTotal: 0,
};

let projectTasks: Task[] = [];

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
  useProjectAttachmentsQuery: () => ({
    data: {
      items: [],
      meta: { page: 1, pageSize: 10, total: 0, projectVersion: 2 },
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateProjectAttachment: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useDeleteProjectAttachment: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useDownloadProjectAttachment: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
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
  useTaskPageQuery: (input: {
    page?: number;
    pageSize?: number;
    parentTaskId?: string;
    q?: string;
    rootOnly?: boolean;
    status?: string;
  }) => {
    taskPageInput(input);
    const page = input.page ?? 1;
    const pageSize = input.pageSize ?? 20;
    const matchingTasks = projectTasks.filter((task) => {
      if (input.parentTaskId) return task.parentTaskId === input.parentTaskId;
      if (input.rootOnly && task.parentTaskId !== null) return false;
      if (input.status && task.status !== input.status) return false;
      if (input.q && !`${task.title} ${task.description}`.includes(input.q)) {
        return false;
      }
      return true;
    });
    const start = (page - 1) * pageSize;
    return {
      data: {
        items: matchingTasks.slice(start, start + pageSize),
        meta: { page, pageSize, total: matchingTasks.length },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
  },
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
    taskPageInput.mockReset();
    project.taskSummary.remaining = 2;
    project.taskSummary.total = 3;
    projectTasks = [];
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

  it("shows project tasks as a hierarchy and can switch to a flat list", () => {
    projectTasks = [rootTask, childTask];
    renderPage();

    expect(screen.getByRole("tree")).toBeTruthy();
    expect(screen.getByText("完成首页设计")).toBeTruthy();
    expect(screen.queryByText("校对移动端间距")).toBeNull();
    expect(screen.getByText("3 项 · 1 个根任务")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("button", { name: "展开子任务：完成首页设计" }),
    );
    expect(screen.getByText("校对移动端间距")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "平铺列表视图" }));
    expect(screen.queryByRole("tree")).toBeNull();
    expect(screen.getByText("2 项")).toBeTruthy();
    expect(screen.getByText("校对移动端间距")).toBeTruthy();
  });

  it("searches, filters and paginates project tasks through the server query", () => {
    project.taskSummary.total = 21;
    projectTasks = Array.from({ length: 21 }, (_, index) => ({
      ...rootTask,
      id: `task-${index + 1}`,
      title: index === 20 ? "最后一项交付" : `项目任务 ${index + 1}`,
      status: index === 20 ? "done" : "todo",
      subtaskTotal: 0,
    }));
    renderPage();

    expect(screen.getByText("1–20 / 21")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("最后一项交付")).toBeTruthy();

    fireEvent.change(screen.getByRole("combobox", { name: "项目任务状态" }), {
      target: { value: "done" },
    });
    expect(screen.getByText("1 / 21 项")).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "任务树视图" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "平铺列表视图" })
        .getAttribute("aria-pressed"),
    ).toBe("true");

    vi.useFakeTimers();
    fireEvent.change(
      screen.getByRole("textbox", { name: "搜索项目任务标题或描述" }),
      { target: { value: "不存在" } },
    );
    act(() => vi.advanceTimersByTime(300));
    vi.useRealTimers();

    expect(screen.getByText("没有匹配任务")).toBeTruthy();
    expect(taskPageInput).toHaveBeenCalledWith(
      expect.objectContaining({
        page: 1,
        projectId: project.id,
        q: "不存在",
        rootOnly: false,
        status: "done",
      }),
    );
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
