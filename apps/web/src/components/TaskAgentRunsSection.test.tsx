import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TaskAgentRunsSection } from "./TaskAgentRunsSection";
import { useUiStore } from "../store/ui";
import type {
  Actor,
  AgentAdapter,
  AgentRun,
  Task,
  TaskAssignment,
  TaskAssignmentListResult,
} from "../types/models";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getAiProviders: vi.fn(),
  getTaskAgentRuns: vi.fn(),
  createAgentRun: vi.fn(),
  cancelAgentRun: vi.fn(),
  retryAgentRun: vi.fn(),
  getAgentAdapters: vi.fn(),
  getAllActors: vi.fn(),
  getTaskAssignments: vi.fn(),
  createTaskAssignment: vi.fn(),
}));

import {
  createAgentRun,
  getAiProviders,
  getTaskAgentRuns,
  getAgentAdapters,
  getAllActors,
  getTaskAssignments,
  createTaskAssignment,
  retryAgentRun,
  cancelAgentRun,
  ApiError,
} from "../api/client";

const adapter = {
  id: "adapter-real",
  adapterKey: "builtin-local-text-v1",
  status: "enabled",
  executionReady: true,
  healthStatus: "healthy",
} as AgentAdapter;
const actor = {
  id: "actor-real",
  type: "agent",
  status: "active",
  displayName: "内置执行代理",
  agentAdapterId: adapter.id,
} as Actor;
const assignment = {
  id: "assignment-1",
  actorId: actor.id,
  isActive: true,
} as TaskAssignment;
function assignmentState(
  assignee: TaskAssignment | null = assignment,
): TaskAssignmentListResult {
  return {
    active: { assignee, reviewer: null },
    history: [],
    meta: { page: 1, pageSize: 20, total: 0, taskVersion: 7 },
  };
}

const task = {
  id: "task-1",
  version: 1,
  status: "todo",
} as unknown as Task;

const run: AgentRun = {
  id: "run-1",
  taskId: "task-1",
  assignmentId: "assignment-1",
  actorId: "actor-real",
  adapterId: "adapter-real",
  createdByActorId: "owner-1",
  parentRunId: null,
  attempt: 1,
  status: "succeeded",
  providerId: "provider-1",
  model: "qwen2.5:1.5b-instruct",
  resultText: "任务已完成：要点与建议。",
  resultBytes: 40,
  errorCode: null,
  startedAt: "2026-09-12T12:00:00Z",
  completedAt: "2026-09-12T12:01:00Z",
  createdAt: "2026-09-12T12:00:00Z",
};

function renderSection(currentTask = task) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TaskAgentRunsSection task={currentTask} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.resetAllMocks();
  useUiStore.setState({ agentRunDrawer: null });
  vi.mocked(getAgentAdapters).mockResolvedValue([adapter]);
  vi.mocked(getAllActors).mockResolvedValue([actor]);
  vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState());
  vi.mocked(createTaskAssignment).mockResolvedValue({
    assignment,
    task: { ...task, version: 8 },
  });
  vi.mocked(getAiProviders).mockResolvedValue([
    {
      id: "provider-1",
      name: "Ollama 本地模型",
      kind: "local",
      protocol: "openai_chat",
      base_url: "http://127.0.0.1:11434/v1",
      model: "qwen2.5:1.5b-instruct",
      status: "ready",
      health_status: "healthy",
      health_error_code: null,
      has_key: false,
      last_health_at: "2026-09-12T12:00:00Z",
      version: 1,
      created_at: "2026-09-12T12:00:00Z",
      updated_at: "2026-09-12T12:00:00Z",
    },
  ]);
  vi.mocked(getTaskAgentRuns).mockResolvedValue([run]);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useUiStore.setState({ agentRunDrawer: null });
});

describe("TaskAgentRunsSection", () => {
  it("lists runs with result preview and start controls", async () => {
    renderSection();

    expect(await screen.findByText("已成功")).toBeVisible();
    expect(screen.getByText("查看产出")).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    expect(
      screen.getByRole("combobox", { name: "选择执行模型" }),
    ).toHaveTextContent(/Ollama 本地模型/);
  });

  it("starts a run with the selected provider", async () => {
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() =>
      expect(createAgentRun).toHaveBeenCalledWith("task-1", "provider-1"),
    );
    expect(createTaskAssignment).not.toHaveBeenCalled();
    expect(useUiStore.getState().agentRunDrawer).toEqual({
      taskId: task.id,
      runId: run.id,
    });
  });

  it("opens the selected run in the persistent execution panel", async () => {
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "查看过程" }));

    expect(useUiStore.getState().agentRunDrawer).toEqual({
      taskId: task.id,
      runId: run.id,
    });
  });

  it("blocks an unregistered environment without assigning or starting", async () => {
    vi.mocked(getAgentAdapters).mockResolvedValue([]);
    renderSection();
    expect(await screen.findByText(/尚未登记内置 Agent/)).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    expect(createTaskAssignment).not.toHaveBeenCalled();
    expect(createAgentRun).not.toHaveBeenCalled();
  });

  it.each([
    [{ ...adapter, status: "disabled" }, /尚未启用/],
    [{ ...adapter, executionReady: false }, /尚未通过当前平台/],
    [{ ...adapter, healthStatus: "unhealthy" }, /尚未通过当前平台/],
  ] as const)(
    "blocks an unavailable adapter %#",
    async (currentAdapter, message) => {
      vi.mocked(getAgentAdapters).mockResolvedValue([currentAdapter]);
      renderSection();
      expect(await screen.findByText(message)).toBeVisible();
      expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    },
  );

  it.each([
    [],
    [{ ...actor, agentAdapterId: "another-adapter" }],
    [{ ...actor, status: "inactive" }],
    [actor, { ...actor, id: "duplicate" }],
  ])(
    "blocks missing, mismatched, inactive or ambiguous actors %#",
    async (...actors) => {
      vi.mocked(getAllActors).mockResolvedValue(actors as Actor[]);
      renderSection();
      expect(
        await screen.findByText(/未找到唯一可执行的 Agent 身份/),
      ).toBeVisible();
      expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
      expect(createTaskAssignment).not.toHaveBeenCalled();
    },
  );

  it("assigns only an unassigned task using the real actor and current task version", async () => {
    vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState(null));
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection();
    expect(
      await screen.findByText(/点击「启动执行」会先将此任务分派给/),
    ).toHaveTextContent(actor.displayName);
    changeProvider();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() => expect(createAgentRun).toHaveBeenCalledOnce());
    expect(createTaskAssignment).toHaveBeenCalledWith(
      task.id,
      { role: "assignee", actorId: actor.id, expectedVersion: 7 },
      expect.any(String),
    );
    expect(
      vi.mocked(createTaskAssignment).mock.invocationCallOrder[0],
    ).toBeLessThan(vi.mocked(createAgentRun).mock.invocationCallOrder[0]);
  });

  it("never replaces an existing owner/person assignment", async () => {
    vi.mocked(getTaskAssignments).mockResolvedValue(
      assignmentState({ ...assignment, actorId: "owner-1" }),
    );
    renderSection();
    expect(await screen.findByText(/已有其他负责人/)).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    expect(createTaskAssignment).not.toHaveBeenCalled();
    expect(createAgentRun).not.toHaveBeenCalled();
  });

  it("rechecks a newly disabled adapter before any write", async () => {
    renderSection();
    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
    vi.mocked(getAgentAdapters).mockResolvedValue([
      { ...adapter, status: "disabled" },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/尚未启用/),
    );
    expect(createTaskAssignment).not.toHaveBeenCalled();
    expect(createAgentRun).not.toHaveBeenCalled();
  });

  it("does not start after assignment fails and renders a Chinese error", async () => {
    vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState(null));
    vi.mocked(createTaskAssignment).mockRejectedValue(
      new ApiError("actor_id does not reference an existing Actor", {
        code: "ACTOR_NOT_FOUND",
        status: 422,
      }),
    );
    renderSection();
    await screen.findByText(/点击「启动执行」会先将此任务分派给/);
    changeProvider();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Agent 身份或启用状态已变化/,
    );
    expect(createAgentRun).not.toHaveBeenCalled();
  });

  it("does not blindly reassign and retry an unsuccessful run request", async () => {
    vi.mocked(createAgentRun).mockRejectedValue(
      new ApiError("unavailable", {
        code: "AGENT_RUN_NOT_EXECUTABLE",
        status: 409,
      }),
    );
    renderSection();
    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Agent 身份或启用状态已变化/,
    );
    expect(createAgentRun).toHaveBeenCalledOnce();
    expect(createTaskAssignment).not.toHaveBeenCalled();
  });

  it("blocks unsupported model protocols", async () => {
    vi.mocked(getAiProviders).mockResolvedValue([
      {
        id: "anthropic",
        protocol: "anthropic_messages",
        status: "ready",
        health_status: "healthy",
      } as never,
    ]);
    renderSection();
    expect(
      await screen.findByText(/暂无已就绪的 OpenAI 兼容模型/),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
  });

  it("refreshes a failed actor lookup and then enables execution", async () => {
    vi.mocked(getAllActors).mockRejectedValue(new Error("offline"));
    renderSection();
    expect(
      await screen.findByText(/无法读取 Agent 或任务分派状态/),
    ).toBeVisible();
    vi.mocked(getAllActors).mockResolvedValue([actor]);
    fireEvent.click(screen.getByRole("button", { name: "刷新执行状态" }));
    await waitFor(() =>
      expect(
        screen.queryByText(/无法读取 Agent 或任务分派状态/),
      ).not.toBeInTheDocument(),
    );
    changeProvider();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled();
  });

  it("prevents terminal tasks from executing but preserves past output", async () => {
    renderSection({ ...task, status: "done" });
    expect(await screen.findByText(/当前任务状态不可启动执行/)).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "查看产出" })).toBeEnabled();
  });

  it("guards retry with the same setup checks", async () => {
    renderSection();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "重试" })).toBeEnabled(),
    );
    vi.mocked(getAllActors).mockResolvedValue([]);
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /未找到唯一可执行的 Agent 身份/,
    );
    expect(retryAgentRun).not.toHaveBeenCalled();
  });

  it("keeps cancellation available even when setup is unavailable", async () => {
    vi.mocked(getAgentAdapters).mockResolvedValue([]);
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      { ...run, status: "running" },
    ]);
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "取消" }));
    await waitFor(() => expect(cancelAgentRun).toHaveBeenCalledWith(run.id));
  });
});

function changeProvider() {
  fireEvent.change(screen.getByRole("combobox", { name: "选择执行模型" }), {
    target: { value: "provider-1" },
  });
}
