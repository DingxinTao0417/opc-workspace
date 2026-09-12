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
import type { AgentRun, Task } from "../types/models";

vi.mock("../api/client", () => ({
  getAiProviders: vi.fn(),
  getTaskAgentRuns: vi.fn(),
  createAgentRun: vi.fn(),
  cancelAgentRun: vi.fn(),
  retryAgentRun: vi.fn(),
  BUILTIN_AGENT_ACTOR_ID: "018f0000-0000-5000-8000-000000003411",
  apiRequest: vi.fn(),
}));

import {
  createAgentRun,
  getAiProviders,
  getTaskAgentRuns,
} from "../api/client";

const task = {
  id: "task-1",
  version: 1,
  status: "todo",
} as unknown as Task;

const run: AgentRun = {
  id: "run-1",
  taskId: "task-1",
  assignmentId: "assignment-1",
  actorId: "actor-1",
  adapterId: "adapter-1",
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

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <TaskAgentRunsSection task={task} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
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
  });
});

function changeProvider() {
  fireEvent.change(screen.getByRole("combobox", { name: "选择执行模型" }), {
    target: { value: "provider-1" },
  });
}
