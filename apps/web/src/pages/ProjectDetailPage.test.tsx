import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useUiStore } from "../store/ui";
import type { Project, ProjectNote, Task } from "../types/models";
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
const deleteProject = vi.hoisted(() => vi.fn());
const deleteProjectState = vi.hoisted(() => ({
  error: null as unknown,
  isPending: false,
}));
const projectRefetch = vi.hoisted(() => vi.fn());
const projectQueryState = vi.hoisted(() => ({
  isError: false,
  isPending: false,
}));
const taskPageInput = vi.hoisted(() => vi.fn());
const focusReportInput = vi.hoisted(() => vi.fn());
const focusHistoryInput = vi.hoisted(() => vi.fn());
const getProjectNote = vi.hoisted(() => vi.fn());

vi.mock("../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../api/client")>("../api/client");
  return { ...actual, getProjectNote };
});

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
let taskPagePlaceholder = false;

vi.mock("../api/hooks", () => ({
  projectNoteQueryKey: (projectId: string) => [
    "projects",
    "detail",
    projectId,
    "notes",
  ],
  useClientOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useTagOptionsQuery: () => ({
    data: [
      {
        id: "tag-1",
        name: "交付",
        color: "#6E7BF2",
        version: 1,
        createdAt: "2026-08-27T08:00:00Z",
      },
    ],
    isError: false,
    isPending: false,
  }),
  useProjectQuery: () => ({
    data:
      projectQueryState.isError || projectQueryState.isPending ? null : project,
    isError: projectQueryState.isError,
    isPending: projectQueryState.isPending,
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
  useFocusReportQuery: (input: unknown) => {
    focusReportInput(input);
    return {
      data: {
        dateFrom: "2026-08-22",
        dateTo: "2026-08-28",
        timezone: "UTC",
        totals: { sessions: 0, seconds: 0, minutes: 0 },
        days: [],
        projects: [],
        hours: [],
        heatmap: [],
        tags: [],
        currentStreakDays: 0,
        longestStreakDays: 0,
      },
      error: null,
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
  },
  useFocusSessionHistoryQuery: (input: unknown) => {
    focusHistoryInput(input);
    return {
      data: {
        items: [],
        meta: { page: 1, pageSize: 6, total: 0 },
      },
      error: null,
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
  },
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
    projectId?: string;
    q?: string;
    rootOnly?: boolean;
    status?: string;
    priority?: string;
    kind?: string;
    plannedState?: string;
    tagIds?: string[];
  }) => {
    taskPageInput(input);
    const page = input.page ?? 1;
    const pageSize = input.pageSize ?? 20;
    const matchingTasks = projectTasks.filter((task) => {
      if (input.parentTaskId) return task.parentTaskId === input.parentTaskId;
      if (input.rootOnly && task.parentTaskId !== null) return false;
      if (input.status && task.status !== input.status) return false;
      if (input.priority && task.priority !== input.priority) return false;
      if (input.kind && task.kind !== input.kind) return false;
      if (
        input.tagIds?.length &&
        !input.tagIds.every((id) => task.tags.some((tag) => tag.id === id))
      )
        return false;
      if (input.plannedState === "scheduled" && !task.plannedDate) return false;
      if (input.plannedState === "unscheduled" && task.plannedDate)
        return false;
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
      isPlaceholderData: taskPagePlaceholder,
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
    ...deleteProjectState,
    mutate: deleteProject,
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
    deleteProject.mockReset();
    deleteProjectState.error = null;
    deleteProjectState.isPending = false;
    projectRefetch.mockReset();
    projectQueryState.isError = false;
    projectQueryState.isPending = false;
    taskPageInput.mockReset();
    focusReportInput.mockReset();
    focusHistoryInput.mockReset();
    getProjectNote.mockReset();
    project.id = "project-1";
    project.status = "in_progress";
    project.availableActions = ["pause", "complete", "archive"];
    project.taskSummary.remaining = 2;
    project.taskSummary.total = 3;
    project.invoiceCount = 0;
    projectTasks = [];
    taskPagePlaceholder = false;
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    useUiStore.setState({ newTaskOpen: false, newTaskProjectId: null });
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    useAiChatStore.setState({ activeSessionId: "" });
  });

  function renderPage(initialEntry = "/projects/project-1") {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const page = (entry: string) => (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[entry]}>
          <Routes>
            <Route
              element={<ProjectDetailPage />}
              path="/projects/:projectId"
            />
            <Route element={<p>AI 对话</p>} path="/ai" />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
    const view = render(page(initialEntry));
    return {
      ...view,
      rerenderPage: (entry = initialEntry) => view.rerender(page(entry)),
    };
  }

  it("opens an exact note, preserves its source session when closing, and returns", async () => {
    const exactProjectId = "018f0000-0000-7000-8000-000000005861";
    const exactNoteId = "018f0000-0000-7000-8000-000000005862";
    const sessionId = "018f0000-0000-7000-8000-000000005863";
    project.id = exactProjectId;
    const exactNote: ProjectNote = {
      id: exactNoteId,
      projectId: exactProjectId,
      title: "从智能体定位的项目笔记",
      body: "这是直接读取的项目笔记正文",
      occurredAt: "2026-09-18T08:00:00Z",
      createdBy: { id: sessionId, type: "owner", displayName: "我" },
      version: 3,
      deletedAt: null,
      deletedByActorId: null,
      deleteReason: null,
      createdAt: "2026-09-18T08:00:00Z",
      updatedAt: "2026-09-18T08:10:00Z",
      projectVersion: 7,
    };
    getProjectNote.mockResolvedValue(exactNote);
    renderPage(
      `/projects/${exactProjectId}?note=${exactNoteId}&return_session=${sessionId}`,
    );

    expect(await screen.findByText(exactNote.body!)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "关闭定位" }));
    await waitFor(() => expect(screen.queryByText(exactNote.body!)).toBeNull());
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByText("AI 对话")).toBeVisible();
  });

  it("hands off the precise project without running project actions", () => {
    const exactProjectId = "018f0000-0000-7000-8000-000000005891";
    project.id = exactProjectId;
    renderPage(`/projects/${exactProjectId}`);

    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "项目",
      route: `/projects/${exactProjectId}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=project");
    expect(pending?.prompt).toContain(`id=${exactProjectId}`);
    expect(pending?.prompt).toContain("project.*");
    expect(pending?.prompt).toContain("完成项目不会自动完成任务");
    expect(transition).not.toHaveBeenCalled();
    expect(deleteProject).not.toHaveBeenCalled();
  });

  it("rejects malformed note parameters without starting a detail read", () => {
    const exactProjectId = "018f0000-0000-7000-8000-000000005871";
    const exactNoteId = "018f0000-0000-7000-8000-000000005872";
    project.id = exactProjectId;
    renderPage(
      `/projects/${exactProjectId}?note=${exactNoteId}&note=${exactNoteId}`,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "项目笔记链接无效或包含重复参数",
    );
    expect(getProjectNote).not.toHaveBeenCalled();
  });

  it("keeps return-to-chat available when the project itself cannot load", () => {
    const exactProjectId = "018f0000-0000-7000-8000-000000005881";
    const exactNoteId = "018f0000-0000-7000-8000-000000005882";
    const sessionId = "018f0000-0000-7000-8000-000000005883";
    projectQueryState.isError = true;
    renderPage(
      `/projects/${exactProjectId}?note=${exactNoteId}&return_session=${sessionId}`,
    );

    expect(screen.getByText("项目详情不可用")).toBeVisible();
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByText("AI 对话")).toBeVisible();
    expect(getProjectNote).not.toHaveBeenCalled();
  });

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

  it("integrates project-scoped Focus after tasks and keeps it visible when archived", () => {
    project.status = "archived";
    project.availableActions = ["restore"];
    renderPage();

    const taskHeading = screen.getByRole("heading", { name: "项目任务" });
    const focusHeading = screen.getByRole("heading", { name: "项目专注" });
    const notesHeading = screen.getByRole("heading", { name: "项目笔记" });
    expect(
      taskHeading.compareDocumentPosition(focusHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      focusHeading.compareDocumentPosition(notesHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(focusReportInput).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: project.id }),
    );
    expect(focusHistoryInput).toHaveBeenCalledWith(
      expect.objectContaining({ projectId: project.id, status: "terminal" }),
    );
  });

  it("states the current Project financial scope without hiding delivered modules", () => {
    project.invoiceCount = 2;
    renderPage();

    expect(
      screen.getByRole("region", { name: "项目财务范围" }),
    ).toHaveTextContent(
      /当前项目关联 2 张发票；发票明细与本地账本请到对应\s*模块查看，项目内财务汇总尚未接入/,
    );
  });

  it("explains which linked business facts block permanent deletion", () => {
    project.status = "archived";
    project.availableActions = ["restore"];
    renderPage();

    const dangerZone = screen
      .getByRole("heading", { name: "永久删除" })
      .closest("section");
    expect(dangerZone).toHaveTextContent(
      /若仍有关联的内容日历条目、路线图里程碑、\s*非草稿发票或不可变账本记录，删除会被阻止/,
    );
    expect(dangerZone).toHaveTextContent(
      /将解除\s*3 项任务、草稿发票和可变手工账本的项目关联/,
    );
  });

  it("shows an actionable message when Content Calendar links block deletion", () => {
    project.status = "archived";
    project.availableActions = ["restore"];
    deleteProjectState.error = new ApiError("project has content items", {
      code: "PROJECT_CONTENT_ITEMS_EXIST",
    });
    renderPage();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "该项目仍关联内容日历条目，当前不能永久删除。请先到内容日历解除项目关联后重试。",
    );
  });

  it.each([
    [
      "PROJECT_HAS_INVOICES",
      "该项目仍被非草稿发票引用，当前不能永久删除。请保留归档项目以维持历史事实。",
    ],
    [
      "PROJECT_HAS_FINANCIAL_ENTRIES",
      "该项目仍被不可变的本地账本记录引用，当前不能永久删除。请保留归档项目以维持历史事实。",
    ],
  ])("shows the %s deletion guard", (code, message) => {
    project.status = "archived";
    project.availableActions = ["restore"];
    deleteProjectState.error = new ApiError("project delete restricted", {
      code,
    });
    renderPage();

    expect(screen.getByRole("alert")).toHaveTextContent(message);
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
      tags: [
        {
          id: "tag-1",
          name: "交付",
          color: "#6E7BF2",
          version: 1,
          createdAt: "2026-08-27T08:00:00Z",
        },
      ],
    }));
    renderPage();

    expect(screen.getByText("1–20 / 21")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("最后一项交付")).toBeTruthy();

    fireEvent.change(screen.getByRole("combobox", { name: "项目任务状态" }), {
      target: { value: "done" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "项目任务优先级" }), {
      target: { value: "P1" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "项目任务类型" }), {
      target: { value: "work" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "项目任务排期" }), {
      target: { value: "unscheduled" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "项目任务标签" }), {
      target: { value: "tag-1" },
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
        priority: "P1",
        kind: "work",
        plannedState: "unscheduled",
        tagIds: ["tag-1"],
      }),
    );
  });

  it("waits for settled project-task data before clamping a stale page", () => {
    project.taskSummary.total = 41;
    projectTasks = Array.from({ length: 41 }, (_, index) => ({
      ...rootTask,
      id: `task-${index + 1}`,
      title: `项目任务 ${index + 1}`,
      subtaskTotal: 0,
    }));
    vi.useFakeTimers();
    const view = renderPage();
    act(() => vi.advanceTimersByTime(300));
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(taskPageInput).toHaveBeenCalledWith(
      expect.objectContaining({ page: 2, projectId: project.id }),
    );

    taskPageInput.mockClear();
    projectTasks = [rootTask];
    taskPagePlaceholder = true;
    view.rerenderPage();

    const placeholderRootPages = taskPageInput.mock.calls
      .map(
        ([input]) =>
          input as {
            page?: number;
            parentTaskId?: string;
            projectId?: string;
          },
      )
      .filter((input) => !input.parentTaskId && input.projectId === project.id)
      .map((input) => input.page);
    expect(placeholderRootPages.length).toBeGreaterThan(0);
    expect(placeholderRootPages.every((queryPage) => queryPage === 2)).toBe(
      true,
    );

    taskPageInput.mockClear();
    taskPagePlaceholder = false;
    view.rerenderPage();

    expect(taskPageInput).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1, projectId: project.id }),
    );
    vi.useRealTimers();
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
