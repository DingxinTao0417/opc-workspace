import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
vi.mock("./AgentRunRestartModal", () => ({
  AgentRunRestartModal: ({
    taskId,
    runId,
    onClose,
  }: {
    taskId: string;
    runId: string;
    onClose: () => void;
  }) => (
    <section aria-label="新执行预览入口">
      {taskId} / {runId}
      <button type="button" onClick={onClose}>
        关闭新执行预览
      </button>
    </section>
  ),
}));
vi.mock("./AgentReworkRetryModal", () => ({
  AgentReworkRetryModal: ({
    taskId,
    runId,
  }: {
    taskId: string;
    runId: string;
  }) => (
    <section aria-label="精确执行重试确认">
      {taskId} / {runId}
    </section>
  ),
}));
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { TaskAgentRunsSection } from "./TaskAgentRunsSection";
import { projectFile } from "../test/agentProjectFileFixture";
const projectMocks = vi.hoisted(() => ({ list: vi.fn(), recheck: vi.fn() }));
vi.mock("../api/agentProjectFiles", () => ({
  getAgentProjectFiles: projectMocks.list,
  recheckAgentProjectFiles: projectMocks.recheck,
}));
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
import { useUiStore } from "../store/ui";
import type {
  Actor,
  AgentAdapter,
  AgentRun,
  AgentRunFileCandidate,
  AgentRunFileCandidatesResult,
  AiProvider,
  Task,
  TaskAssignment,
  TaskAssignmentListResult,
} from "../types/models";

const hookMocks = vi.hoisted(() => ({
  invalidateAgentRunDeliveryFacts: vi.fn(),
}));
const reworkMocks = vi.hoisted(() => ({ source: vi.fn(), preview: vi.fn() }));
vi.mock("../api/taskSubmissionLocation", () => ({
  getTaskSubmissionLocation: reworkMocks.source,
}));
vi.mock("../api/agentRunRework", () => ({
  getAgentRunReworkPreview: reworkMocks.preview,
}));

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getAiProviders: vi.fn(),
  getTaskAgentRuns: vi.fn(),
  getTaskAgentRunFiles: vi.fn(),
  createAgentRun: vi.fn(),
  cancelAgentRun: vi.fn(),
  retryAgentRun: vi.fn(),
  getAgentAdapters: vi.fn(),
  getAllActors: vi.fn(),
  getTaskAssignments: vi.fn(),
  createTaskAssignment: vi.fn(),
}));

vi.mock("../api/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/hooks")>()),
  invalidateAgentRunDeliveryFacts: hookMocks.invalidateAgentRunDeliveryFacts,
}));

import {
  createAgentRun,
  getAiProviders,
  getTaskAgentRuns,
  getTaskAgentRunFiles,
  getAgentAdapters,
  getAllActors,
  getTaskAssignments,
  createTaskAssignment,
  retryAgentRun,
  cancelAgentRun,
  ApiError,
} from "../api/client";
import { invalidateAgentRunDeliveryFacts } from "../api/hooks";

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
  id: "018f0000-0000-7000-8000-000000000901",
  title: "制作发布页",
  version: 1,
  status: "todo",
} as unknown as Task;

const localProvider: AiProvider = {
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
  configVersion: 1,
  created_at: "2026-09-12T12:00:00Z",
  updated_at: "2026-09-12T12:00:00Z",
};

const run: AgentRun = {
  id: "018f0000-0000-7000-8000-000000000902",
  taskId: task.id,
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
  outputDeliveryStatus: "retained",
  outputDeliveryErrorCode: "TASK_SUBMISSION_NOT_ALLOWED",
  submissionId: null,
  artifactId: null,
  startedAt: "2026-09-12T12:00:00Z",
  completedAt: "2026-09-12T12:01:00Z",
  createdAt: "2026-09-12T12:00:00Z",
};

const fileCandidate: AgentRunFileCandidate = {
  sourceKind: "task_artifact",
  id: "018f0000-0000-7000-8000-000000000701",
  name: "brief.md",
  mime: "text/markdown",
  sizeBytes: 24_000,
  sha256: "a".repeat(64),
  eligible: true,
  errorCode: null,
  createdAt: "2026-09-18T10:00:00Z",
};

function fileCandidates(
  items: AgentRunFileCandidate[] = [fileCandidate],
  limits: AgentRunFileCandidatesResult["limits"] = {
    maxFiles: 4,
    maxFileBytes: 65_536,
    maxTotalBytes: 131_072,
    maxResultBytes: 65_536,
  },
): AgentRunFileCandidatesResult {
  return { items, limits };
}

function renderSection(
  currentTask = task,
  returnSession?: string,
  disabled = false,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const ui = (nextTask = currentTask) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <TaskAgentRunsSection
          task={nextTask}
          returnSession={returnSession}
          disabled={disabled}
        />
      </MemoryRouter>
    </QueryClientProvider>
  );
  return { ...render(ui()), ui };
}

beforeEach(() => {
  vi.resetAllMocks();
  projectMocks.list.mockResolvedValue({
    items: [projectFile],
    total: 1,
    offset: 0,
    nextOffset: null,
  });
  projectMocks.recheck.mockResolvedValue(undefined);
  useUiStore.setState({ agentRunDrawer: null });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  useAgentRunFileApply.getState().clear();
  vi.mocked(getAgentAdapters).mockResolvedValue([adapter]);
  vi.mocked(getAllActors).mockResolvedValue([actor]);
  vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState());
  vi.mocked(createTaskAssignment).mockResolvedValue({
    assignment,
    task: { ...task, version: 8 },
  });
  vi.mocked(getAiProviders).mockResolvedValue([localProvider]);
  vi.mocked(getTaskAgentRuns).mockResolvedValue([run]);
  vi.mocked(getTaskAgentRunFiles).mockResolvedValue(fileCandidates());
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useUiStore.setState({ agentRunDrawer: null });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  useAgentRunFileApply.getState().clear();
});

describe("TaskAgentRunsSection", () => {
  it("shows the current safe Agent Run stage in task history", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      {
        ...run,
        status: "running",
        resultText: null,
        resultBytes: null,
        errorCode: null,
        outputDeliveryStatus: "not_ready",
        outputDeliveryErrorCode: null,
        submissionId: null,
        artifactId: null,
        completedAt: null,
        progress: { phase: "calling_model", elapsedMs: 72_000 },
      },
    ]);
    renderSection();
    expect(await screen.findByText("正在调用模型")).toBeVisible();
    expect(screen.getByText(/已运行 1 分 12 秒/)).toBeVisible();
  });

  async function selectProjectFile() {
    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.click(
      screen.getByRole("button", { name: "选择跨任务已验收文件" }),
    );
    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: "选择跨任务文件 accepted.md",
      }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "同意发送所选文件正文" }),
    );
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行确认上述跨任务文件/ }),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
  }
  it("starts with separately confirmed project file without requiring an unused ordinary file listing", async () => {
    vi.mocked(getTaskAgentRunFiles).mockRejectedValue(
      new Error("ordinary listing unavailable"),
    );
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection({ ...task, projectId: projectFile.sourceTask.project_id });
    await selectProjectFile();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() => expect(createAgentRun).toHaveBeenCalledOnce());
    expect(projectMocks.recheck).toHaveBeenCalledWith(task.id, [projectFile]);
    expect(vi.mocked(createAgentRun).mock.calls[0][2]).toMatchObject({
      confirmProjectTaskFiles: true,
      confirmFileAccess: true,
      inputFiles: [
        {
          sourceKind: projectFile.sourceKind,
          id: projectFile.id,
          sourceTask: projectFile.sourceTask,
          sha256: projectFile.sha256,
        },
      ],
    });
  });
  it.each(["unmount", "switch", "drift"])(
    "preflight %s cannot create a stale cross-task execution",
    async (mode) => {
      let release!: () => void;
      projectMocks.recheck.mockImplementation(
        () =>
          new Promise<void>((resolve) => {
            release = resolve;
          }),
      );
      const currentTask = {
        ...task,
        projectId: projectFile.sourceTask.project_id,
      };
      const view = renderSection(currentTask);
      await selectProjectFile();
      fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
      await waitFor(() => expect(projectMocks.recheck).toHaveBeenCalledOnce());
      if (mode === "unmount") view.unmount();
      else if (mode === "switch") {
        view.rerender(view.ui({ ...currentTask, id: projectFile.id }));
        view.rerender(view.ui(currentTask));
      } else
        view.rerender(
          view.ui({ ...currentTask, version: currentTask.version + 1 }),
        );
      await act(async () => {
        release();
      });
      expect(createAgentRun).not.toHaveBeenCalled();
    },
  );
  it("clears both source/file consent after failed source revalidation", async () => {
    projectMocks.recheck.mockRejectedValue(
      new ApiError("source changed", { status: 409 }),
    );
    renderSection({ ...task, projectId: projectFile.sourceTask.project_id });
    await selectProjectFile();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: /我另行确认上述跨任务文件/ }),
      ).not.toBeChecked(),
    );
    expect(
      screen.getByRole("checkbox", { name: "同意发送所选文件正文" }),
    ).not.toBeChecked();
    expect(createAgentRun).not.toHaveBeenCalled();
  });
  it.each(["retained", "failed"])(
    "opens exact-source current-facts preview for %s without running anything",
    async (kind) => {
      const source =
        kind === "retained"
          ? run
          : {
              ...run,
              status: "failed" as const,
              outputDeliveryStatus: "not_ready" as const,
              resultText: null,
              resultBytes: null,
            };
      vi.mocked(getTaskAgentRuns).mockResolvedValue([source]);
      renderSection();
      fireEvent.click(
        await screen.findByRole("button", { name: "按当前事实重新执行" }),
      );
      expect(
        screen.getByRole("region", { name: "新执行预览入口" }),
      ).toHaveTextContent(`${task.id} / ${run.id}`);
      expect(createAgentRun).not.toHaveBeenCalled();
      expect(retryAgentRun).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "关闭新执行预览" }));
      expect(
        screen.queryByRole("region", { name: "新执行预览入口" }),
      ).toBeNull();
    },
  );
  it.each([
    ["AGENT_MODEL_TRUNCATED", "模型输出未完整结束，未登记产出。"],
    ["AGENT_MODEL_FILTERED", "模型拒绝或过滤了输出，未登记产出。"],
    [
      "AGENT_MODEL_RESPONSE_INVALID",
      "模型响应不符合完整文本交付协议，未登记产出。",
    ],
  ])(
    "explains model failure %s without automatically retrying or presenting partial output",
    async (errorCode, message) => {
      vi.mocked(getTaskAgentRuns).mockResolvedValue([
        {
          ...run,
          status: "failed",
          outputDeliveryStatus: "not_ready",
          outputDeliveryErrorCode: null,
          submissionId: null,
          artifactId: null,
          resultText: null,
          resultBytes: null,
          errorCode,
        },
      ]);
      renderSection();
      expect(await screen.findByText(message)).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "查看产出" }),
      ).not.toBeInTheDocument();
      expect(retryAgentRun).not.toHaveBeenCalled();
      expect(createAgentRun).not.toHaveBeenCalled();
    },
  );
  it.each(["openai_chat", "anthropic_messages"] as const)(
    "starts %s rework only after complete preview and independent consent",
    async (protocol) => {
      const currentProvider = {
        ...localProvider,
        protocol,
        kind:
          protocol === "anthropic_messages"
            ? ("remote" as const)
            : ("local" as const),
      };
      vi.mocked(getAiProviders).mockResolvedValue([currentProvider]);
      const submissionId = "018f0000-0000-7000-8000-000000000981";
      const artifactId = "018f0000-0000-7000-8000-000000000982";
      const currentTask = {
        ...task,
        version: 7,
        currentSubmissionId: submissionId,
        reviewPolicy: "manual" as const,
        status: "in_progress" as const,
      };
      const context = {
        submission_id: submissionId,
        sequence: 2,
        review_reason: "必须补齐来源证据",
        reviewed_at: "2026-09-21T12:00:00Z",
        artifacts: [
          {
            id: artifactId,
            storage_kind: "structured",
            name: "旧结构化草稿",
            content: '{"exact":1e100}',
            sha256: "a".repeat(64),
          },
        ],
      };
      reworkMocks.source.mockResolvedValue({
        task: currentTask,
        submission: {
          id: submissionId,
          taskId: task.id,
          status: "changes_requested",
          sequence: 2,
          reviewReason: context.review_reason,
          artifacts: [
            {
              id: artifactId,
              storageKind: "structured",
              name: "旧结构化草稿",
              deletedAt: null,
            },
          ],
        },
      });
      reworkMocks.preview.mockResolvedValue(context);
      vi.mocked(getTaskAgentRuns).mockResolvedValue([]);
      vi.mocked(createAgentRun).mockResolvedValue(run);
      renderSection(currentTask);
      await waitFor(() =>
        expect(
          screen.getByRole("combobox", { name: "选择执行模型" }),
        ).toBeEnabled(),
      );
      fireEvent.change(screen.getByRole("combobox", { name: "选择执行模型" }), {
        target: { value: localProvider.id },
      });
      expect(reworkMocks.source).not.toHaveBeenCalled();
      fireEvent.click(
        screen.getByRole("button", { name: "基于当前退回批次再次执行" }),
      );
      const selection = await screen.findByRole("combobox", {
        name: "选择返工批次",
      });
      fireEvent.change(selection, { target: { value: submissionId } });
      fireEvent.click(
        screen.getByRole("checkbox", { name: "携带旧产出 旧结构化草稿" }),
      );
      const start = screen.getByRole("button", {
        name: "基于退回批次启动新执行",
      });
      expect(start).toBeDisabled();
      fireEvent.click(
        screen.getByRole("button", { name: "预览完整返工上下文" }),
      );
      expect(
        await screen.findByText(context.artifacts[0].content),
      ).toBeVisible();
      expect(createAgentRun).not.toHaveBeenCalled();
      fireEvent.click(
        screen.getByRole("checkbox", { name: "同意发送返工意见与旧产出" }),
      );
      await waitFor(() => expect(start).toBeEnabled());
      fireEvent.click(start);
      await waitFor(() =>
        expect(createAgentRun).toHaveBeenCalledWith(task.id, localProvider.id, {
          rework: {
            submissionId,
            artifactIds: [artifactId],
            expectedTaskVersion: 7,
          },
          confirmReworkContext: true,
          reworkProviderConfirmation: {
            version: localProvider.version,
            configVersion: localProvider.configVersion,
            kind: currentProvider.kind,
          },
          outputContract: { type: "text" },
        }),
      );
      expect(reworkMocks.preview).toHaveBeenCalledTimes(2);
      expect(retryAgentRun).not.toHaveBeenCalled();
      expect(createTaskAssignment).not.toHaveBeenCalled();
    },
  );
  it.each(["unmount", "task-aba"])(
    "does not start after rework precheck returns to a stale owner: %s",
    async (change) => {
      const submissionId = "018f0000-0000-7000-8000-000000000981";
      const currentTask = {
        ...task,
        version: 7,
        currentSubmissionId: submissionId,
        reviewPolicy: "manual" as const,
        status: "in_progress" as const,
      };
      const context = {
        submission_id: submissionId,
        sequence: 2,
        review_reason: "仅意见",
        reviewed_at: "2026-09-21T12:00:00Z",
        artifacts: [],
      };
      reworkMocks.source.mockResolvedValue({
        task: currentTask,
        submission: {
          id: submissionId,
          taskId: task.id,
          status: "changes_requested",
          sequence: 2,
          reviewReason: context.review_reason,
          artifacts: [],
        },
      });
      let resolve!: (value: typeof context) => void;
      reworkMocks.preview.mockResolvedValueOnce(context).mockImplementation(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
      );
      vi.mocked(getTaskAgentRuns).mockResolvedValue([]);
      const view = renderSection(currentTask);
      await waitFor(() =>
        expect(
          screen.getByRole("combobox", { name: "选择执行模型" }),
        ).toBeEnabled(),
      );
      fireEvent.change(screen.getByRole("combobox", { name: "选择执行模型" }), {
        target: { value: localProvider.id },
      });
      fireEvent.click(
        screen.getByRole("button", { name: "基于当前退回批次再次执行" }),
      );
      fireEvent.change(
        await screen.findByRole("combobox", { name: "选择返工批次" }),
        { target: { value: submissionId } },
      );
      fireEvent.click(
        screen.getByRole("button", { name: "预览完整返工上下文" }),
      );
      fireEvent.click(
        await screen.findByRole("checkbox", {
          name: "同意发送返工意见与旧产出",
        }),
      );
      const submit = screen.getByRole("button", {
        name: "基于退回批次启动新执行",
      });
      await waitFor(() => expect(submit).toBeEnabled());
      fireEvent.click(submit);
      await waitFor(() => expect(reworkMocks.preview).toHaveBeenCalledTimes(2));
      if (change === "unmount") view.unmount();
      else {
        view.rerender(view.ui({ ...currentTask, id: submissionId }));
        view.rerender(view.ui(currentTask));
      }
      await act(async () => {
        resolve(context);
      });
      expect(createAgentRun).not.toHaveBeenCalled();
      expect(createTaskAssignment).not.toHaveBeenCalled();
    },
  );
  it.each(["查看过程", "查看执行过程"])(
    "carries a supplied return session from %s without executing",
    async (label) => {
      const session = "018f0000-0000-7000-8000-000000000599";
      renderSection(task, session);
      await screen.findByRole("button", { name: "查看过程" });
      fireEvent.click(screen.getByRole("button", { name: label }));
      expect(useUiStore.getState().agentRunDrawer).toEqual({
        taskId: task.id,
        runId: run.id,
        returnSession: session,
      });
      expect(createAgentRun).not.toHaveBeenCalled();
      expect(retryAgentRun).not.toHaveBeenCalled();
    },
  );
  it("does not offer drawer navigation while parent has an unsaved draft or write", async () => {
    renderSection(task, "018f0000-0000-7000-8000-000000000599", true);
    expect(
      await screen.findByRole("button", { name: "查看过程" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "查看执行过程" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "查看过程" }));
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });
  it("lists runs with result preview and start controls", async () => {
    renderSection();

    expect(await screen.findByText("结果已保留 · 未提交")).toBeVisible();
    expect(screen.queryByText("已成功")).not.toBeInTheDocument();
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
      expect(createAgentRun).toHaveBeenCalledWith(task.id, "provider-1"),
    );
    expect(createTaskAssignment).not.toHaveBeenCalled();
    expect(useUiStore.getState().agentRunDrawer).toEqual({
      taskId: task.id,
      runId: run.id,
    });
    expect(invalidateAgentRunDeliveryFacts).toHaveBeenCalledWith(
      expect.anything(),
      task.id,
    );
  });

  it("freezes an ordered multi-file output contract without granting file input access", async () => {
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.change(
      screen.getByRole("combobox", { name: "选择 Agent 输出格式" }),
      { target: { value: "files" } },
    );
    expect(
      screen.getByRole("checkbox", { name: "选择输出文件 agent-output.md" }),
    ).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "选择输出文件 agent-output.txt" }),
    ).toBeChecked();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));

    await waitFor(() =>
      expect(createAgentRun).toHaveBeenCalledWith(task.id, "provider-1", {
        outputContract: {
          type: "files",
          files: [
            { name: "agent-output.md", mime: "text/markdown" },
            { name: "agent-output.txt", mime: "text/plain" },
          ],
        },
      }),
    );
  });

  it("renders a multi-file result against its frozen file names", async () => {
    const multiFileRun: AgentRun = {
      ...run,
      outputContract: {
        type: "files",
        files: [
          { name: "agent-run-attempt-1.md", mime: "text/markdown" },
          { name: "agent-run-attempt-1.json", mime: "application/json" },
        ],
      },
      resultText: JSON.stringify(["# 交付说明", '{"status":"ready"}']),
      resultBytes: 47,
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([multiFileRun]);
    renderSection();

    const showResult = await screen.findByRole("button", {
      name: "查看 2 份产出",
    });
    fireEvent.click(showResult);

    expect(screen.getByText("agent-run-attempt-1.md")).toBeVisible();
    expect(screen.getByText("agent-run-attempt-1.json")).toBeVisible();
    expect(screen.getByText("# 交付说明")).toBeVisible();
    expect(screen.getByText('{"status":"ready"}')).toBeVisible();
    expect(screen.getAllByRole("button", { name: "下载" })).toHaveLength(2);
  });

  it("keeps each eligible multi-file apply action bound to its original index", async () => {
    const submitted: AgentRun = {
      ...run,
      outputContract: {
        type: "files",
        files: [
          { name: "oversized.md", mime: "text/markdown" },
          { name: "eligible.json", mime: "application/json" },
        ],
      },
      resultText: JSON.stringify(["x".repeat(32 * 1024 + 1), '{"ok":true}']),
      resultBytes: 32 * 1024 + 20,
      outputDeliveryStatus: "submitted",
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000903",
      artifactId: "018f0000-0000-7000-8000-000000000904",
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([submitted]);
    renderSection();

    fireEvent.click(
      await screen.findByRole("button", { name: "查看 2 份产出" }),
    );
    const apply = screen.getByRole("button", { name: "用于项目文件" });
    expect(apply).toBeVisible();
    fireEvent.click(apply);

    expect(useAgentRunFileApply.getState().candidate).toMatchObject({
      index: 1,
      name: "eligible.json",
      content: '{"ok":true}',
    });
  });

  it("requires separate file consent and sends the exact v2 identities", async () => {
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "选择文件 brief.md" }),
    );
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    fireEvent.change(
      screen.getByRole("combobox", { name: "选择 Agent 输出格式" }),
      { target: { value: "file:0" } },
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "同意发送所选文件正文" }),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "启动执行" })).toBeEnabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));

    await waitFor(() =>
      expect(createAgentRun).toHaveBeenCalledWith(task.id, "provider-1", {
        inputFiles: [
          {
            sourceKind: "task_artifact",
            id: fileCandidate.id,
          },
        ],
        outputContract: {
          type: "file",
          name: "agent-output.md",
          mime: "text/markdown",
        },
        confirmFileAccess: true,
        fileAccessProviderConfirmation: {
          version: 1,
          configVersion: 1,
          kind: "local",
        },
      }),
    );
  });

  it.each([
    ["version", { ...localProvider, version: 2 }],
    ["config version", { ...localProvider, configVersion: 2 }],
    [
      "local or remote kind",
      {
        ...localProvider,
        kind: "remote" as const,
        base_url: "https://example.test/v1",
        has_key: true,
      },
    ],
  ])(
    "clears file consent when Provider %s changes",
    async (_label, changed) => {
      vi.mocked(getAiProviders)
        .mockResolvedValueOnce([localProvider])
        .mockResolvedValue([changed]);
      renderSection();

      await screen.findByText(/Ollama 本地模型/);
      changeProvider();
      fireEvent.click(screen.getByText("受控输入文件与输出"));
      fireEvent.click(
        await screen.findByRole("checkbox", { name: "选择文件 brief.md" }),
      );
      const consent = screen.getByRole("checkbox", {
        name: "同意发送所选文件正文",
      });
      fireEvent.click(consent);
      expect(consent).toBeChecked();

      fireEvent.click(screen.getByRole("button", { name: "刷新执行状态" }));

      await waitFor(() => expect(consent).not.toBeChecked());
      expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
      expect(createAgentRun).not.toHaveBeenCalled();
    },
  );

  it("clears file consent when the backend catches a last-moment Provider race", async () => {
    vi.mocked(createAgentRun).mockRejectedValue(
      new ApiError("provider changed", {
        code: "AGENT_RUN_IDENTITY_CHANGED",
        status: 409,
      }),
    );
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "选择文件 brief.md" }),
    );
    const consent = screen.getByRole("checkbox", {
      name: "同意发送所选文件正文",
    });
    fireEvent.click(consent);
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Provider 身份已变化.*本次未启动/,
    );
    expect(consent).not.toBeChecked();
    expect(createAgentRun).toHaveBeenCalledOnce();
  });

  it("explains that a full Agent Run queue leaves the task unchanged", async () => {
    vi.mocked(createAgentRun).mockRejectedValue(
      new ApiError("queue full", {
        code: "AGENT_RUN_QUEUE_FULL",
        status: 429,
      }),
    );
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Agent 执行队列已满，本次没有创建执行，也没有修改任务分派/,
    );
    expect(createAgentRun).toHaveBeenCalledOnce();
    expect(createTaskAssignment).not.toHaveBeenCalled();
  });

  it("warns that selected files leave the device for a remote provider", async () => {
    vi.mocked(getAiProviders).mockResolvedValue([
      {
        id: "provider-1",
        name: "DeepSeek 在线模型",
        kind: "remote",
        protocol: "openai_chat",
        base_url: "https://example.test/v1",
        model: "deepseek-chat",
        status: "ready",
        health_status: "healthy",
        health_error_code: null,
        has_key: true,
        last_health_at: "2026-09-12T12:00:00Z",
        version: 1,
        configVersion: 1,
        created_at: "2026-09-12T12:00:00Z",
        updated_at: "2026-09-12T12:00:00Z",
      },
    ]);
    renderSection();

    await screen.findByText(/DeepSeek 在线模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "选择文件 brief.md" }),
    );

    expect(screen.getByText(/文件的正文将离开本机/)).toHaveTextContent(
      "DeepSeek 在线模型",
    );
  });

  it("uses tightened server limits and does not silently replace a refreshed file", async () => {
    const second = {
      ...fileCandidate,
      id: "018f0000-0000-7000-8000-000000000702",
      name: "notes.md",
      sha256: "b".repeat(64),
      sizeBytes: 24_000,
    };
    vi.mocked(getTaskAgentRunFiles)
      .mockResolvedValueOnce(
        fileCandidates([fileCandidate, second], {
          maxFiles: 2,
          maxFileBytes: 32_768,
          maxTotalBytes: 32_768,
          maxResultBytes: 32_768,
        }),
      )
      .mockResolvedValue(
        fileCandidates([{ ...fileCandidate, sha256: "c".repeat(64) }, second], {
          maxFiles: 2,
          maxFileBytes: 32_768,
          maxTotalBytes: 32_768,
          maxResultBytes: 32_768,
        }),
      );
    renderSection();

    await screen.findByText(/Ollama 本地模型/);
    changeProvider();
    fireEvent.click(screen.getByText("受控输入文件与输出"));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "选择文件 brief.md" }),
    );
    expect(
      screen.getByRole("checkbox", { name: "选择文件 notes.md" }),
    ).toBeDisabled();
    expect(screen.getByText(/已达到文件数量或总量上限/)).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "刷新执行状态" }));
    expect(await screen.findByText(/不会自动换成新版本/)).toBeVisible();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
  });

  it("hands off a failed Agent run without cancelling, retrying, or opening output", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      {
        ...run,
        status: "failed",
        resultText: null,
        resultBytes: null,
        errorCode: "AGENT_MODEL_FAILED",
        outputDeliveryStatus: "not_ready",
        outputDeliveryErrorCode: null,
        completedAt: "2026-09-12T12:01:00Z",
      },
    ]);
    renderSection();

    fireEvent.click(await screen.findByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "Agent 执行需要处理",
      route: `/tasks/${task.id}?agent_run=${run.id}`,
      scopes: ["work", "outputs", "actions", "agent_execution"],
    });
    expect(pending?.prompt).toContain("任务「制作发布页」");
    expect(pending?.prompt).toContain("第 1 次 Agent 执行");
    expect(pending?.prompt).toContain("执行失败");
    expect(pending?.prompt).toContain("workspace_agent_runs");
    expect(pending?.prompt).toContain("workspace_outputs");
    expect(pending?.prompt).toContain("不要把建议说成已经执行");
    expect(cancelAgentRun).not.toHaveBeenCalled();
    expect(retryAgentRun).not.toHaveBeenCalled();
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
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

  it("atomically assigns only an unassigned task with the Run request", async () => {
    vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState(null));
    vi.mocked(createAgentRun).mockResolvedValue(run);
    renderSection();
    expect(
      await screen.findByText(/点击「启动执行」会先将此任务分派给/),
    ).toHaveTextContent(actor.displayName);
    changeProvider();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() =>
      expect(createAgentRun).toHaveBeenCalledWith(task.id, localProvider.id, {
        autoAssign: {
          actorId: actor.id,
          expectedTaskVersion: 7,
        },
      }),
    );
    expect(createTaskAssignment).not.toHaveBeenCalled();
  });

  it("surfaces an atomic auto-assignment conflict without a standalone write", async () => {
    vi.mocked(getTaskAssignments).mockResolvedValue(assignmentState(null));
    vi.mocked(createAgentRun).mockRejectedValue(
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
    expect(createAgentRun).toHaveBeenCalledWith(task.id, localProvider.id, {
      autoAssign: {
        actorId: actor.id,
        expectedTaskVersion: 7,
      },
    });
    expect(createTaskAssignment).not.toHaveBeenCalled();
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

  it.each(["unknown", "local-anthropic"])(
    "blocks unsupported model combination %s",
    async (kind) => {
      vi.mocked(getAiProviders).mockResolvedValue([
        {
          ...localProvider,
          protocol:
            kind === "unknown" ? "unknown_protocol" : "anthropic_messages",
        } as never,
      ]);
      renderSection();
      expect(
        await screen.findByText(/暂无已就绪的 OpenAI 兼容模型/),
      ).toBeVisible();
      expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    },
  );
  it("starts a remote Anthropic text run only after explicit model selection", async () => {
    vi.mocked(getAiProviders).mockResolvedValue([
      {
        ...localProvider,
        kind: "remote",
        protocol: "anthropic_messages",
        name: "Claude 远程模型",
      },
    ]);
    vi.mocked(createAgentRun).mockResolvedValue({
      ...run,
      executionContractVersion: 5,
    });
    renderSection();
    await screen.findByRole("option", { name: /Claude 远程模型/ });
    expect(createAgentRun).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "启动执行" })).toBeDisabled();
    changeProvider();
    expect(
      screen.getByText(/Anthropic Messages · v5 · 最多 8192/),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "启动执行" }));
    await waitFor(() =>
      expect(createAgentRun).toHaveBeenCalledWith(task.id, localProvider.id),
    );
  });
  it("opens a v5 retry confirmation without running from list metadata", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      {
        ...run,
        executionContractVersion: 5,
        status: "failed",
        resultText: null,
        resultBytes: null,
        outputDeliveryStatus: "not_ready",
      },
    ]);
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "重试" }));
    expect(screen.getByLabelText("精确执行重试确认")).toHaveTextContent(
      `${task.id} / ${run.id}`,
    );
    expect(retryAgentRun).not.toHaveBeenCalled();
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

  it("refreshes continuation projections when a retry queues a new Run", async () => {
    vi.mocked(retryAgentRun).mockResolvedValue({
      ...run,
      id: "018f0000-0000-7000-8000-000000000903",
      parentRunId: run.id,
      attempt: 2,
      status: "queued",
      resultText: null,
      resultBytes: null,
      errorCode: null,
      outputDeliveryStatus: "not_ready",
      outputDeliveryErrorCode: null,
      startedAt: null,
      completedAt: null,
    });
    renderSection();

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "重试" })).toBeEnabled(),
    );
    changeProvider();
    fireEvent.click(await screen.findByRole("button", { name: "重试" }));

    await waitFor(() => expect(retryAgentRun).toHaveBeenCalledWith(run.id));
    expect(invalidateAgentRunDeliveryFacts).toHaveBeenCalledWith(
      expect.anything(),
      task.id,
    );
  });

  it("does not offer cancellation after execution entered pending output delivery", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      {
        ...run,
        status: "running",
        resultText: null,
        resultBytes: null,
        errorCode: null,
        outputDeliveryStatus: "pending",
        outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
        submissionId: null,
        artifactId: null,
        startedAt: "2026-09-12T12:00:01Z",
        completedAt: null,
      },
    ]);
    renderSection();

    expect(await screen.findByText(/查看执行过程.*重试登记产出/)).toBeVisible();
    expect(screen.getByText("结果待登记")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "取消" }),
    ).not.toBeInTheDocument();
    const openRun = screen.getByRole("button", { name: "查看过程" });
    expect(openRun).toBeEnabled();
    fireEvent.click(openRun);
    expect(useUiStore.getState().agentRunDrawer).toEqual({
      taskId: task.id,
      runId: run.id,
    });
    expect(cancelAgentRun).not.toHaveBeenCalled();
  });

  it("keeps cancellation available even when setup is unavailable", async () => {
    vi.mocked(getAgentAdapters).mockResolvedValue([]);
    vi.mocked(getTaskAgentRuns).mockResolvedValue([
      {
        ...run,
        status: "running",
        outputDeliveryStatus: "not_ready",
        outputDeliveryErrorCode: null,
      },
    ]);
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: "取消" }));
    await waitFor(() => expect(cancelAgentRun).toHaveBeenCalledWith(run.id));
    expect(invalidateAgentRunDeliveryFacts).toHaveBeenCalledWith(
      expect.anything(),
      task.id,
    );
  });
});

function changeProvider() {
  fireEvent.change(screen.getByRole("combobox", { name: "选择执行模型" }), {
    target: { value: "provider-1" },
  });
}
