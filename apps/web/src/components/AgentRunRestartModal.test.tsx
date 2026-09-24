import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AgentRunRestartModal } from "./AgentRunRestartModal";
import { restartProposal } from "../test/agentRunRestartFixture";
import {
  projectFile,
  projectFileProposal,
} from "../test/agentProjectFileFixture";
const projectApi = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock("../api/agentProjectFiles", () => ({
  getAgentProjectFiles: projectApi.list,
}));
const mocks = vi.hoisted(() => ({
  source: vi.fn(),
  providers: vi.fn(),
  files: vi.fn(),
  create: vi.fn(),
  preview: vi.fn(),
  reworkSource: vi.fn(),
  reworkPreview: vi.fn(),
  task: {} as Record<string, unknown>,
  assignment: true,
}));
vi.mock("../api/taskSubmissionLocation", () => ({
  getTaskSubmissionLocation: mocks.reworkSource,
}));
vi.mock("../api/agentRunRework", () => ({
  getAgentRunReworkPreview: mocks.reworkPreview,
}));
vi.mock("../api/client", async () => ({
  ...(await vi.importActual("../api/client")),
  getAgentRun: mocks.source,
  getAiProviders: mocks.providers,
  getTaskAgentRunFiles: mocks.files,
  createAgentRun: mocks.create,
}));
vi.mock("../api/agentRunRestart", () => ({
  getAgentRunRestartPreview: mocks.preview,
}));
vi.mock("../api/hooks", () => ({
  useTaskQuery: () => ({ data: mocks.task, isFetching: false, error: null }),
  useTaskAssignmentsQuery: () => ({
    data: {
      pages: [
        {
          active: {
            assignee: mocks.assignment ? { actorId: "current" } : null,
          },
        },
      ],
    },
    isFetching: false,
    error: null,
  }),
}));
const p = restartProposal();
const snapshot = p.preview.agent_run_start;
const taskId = p.action.task_id,
  runId = snapshot.restart.run_id,
  providerId = snapshot.provider.id;
const provider = {
  ...snapshot.provider,
  configVersion: snapshot.provider.config_version,
};
const preview = {
  task_id: taskId,
  task_version: 9,
  source_run: snapshot.restart,
  current_start: snapshot,
  fingerprint: "a".repeat(64),
};
const source = {
  id: runId,
  taskId,
  status: "succeeded",
  outputDeliveryStatus: "retained",
};
beforeEach(() => {
  vi.resetAllMocks();
  projectApi.list.mockResolvedValue({
    items: [projectFile],
    total: 1,
    offset: 0,
    nextOffset: null,
  });
  mocks.assignment = true;
  mocks.task = {
    id: taskId,
    version: 9,
    status: "in_progress",
    title: "当前任务",
    reviewPolicy: "manual",
    currentSubmissionId: null,
  };
  mocks.source.mockResolvedValue(source);
  mocks.providers.mockResolvedValue([provider]);
  mocks.files.mockResolvedValue({ items: [] });
  mocks.preview.mockResolvedValue(preview);
  mocks.create.mockResolvedValue({
    id: "new-run",
    taskId,
    restartOfRunId: runId,
  });
});
afterEach(cleanup);
function mount() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  const onClose = vi.fn(),
    onSuccess = vi.fn();
  const ui = (id = runId) => (
    <QueryClientProvider client={client}>
      <AgentRunRestartModal
        taskId={taskId}
        runId={id}
        onClose={onClose}
        onSuccess={onSuccess}
      />
    </QueryClientProvider>
  );
  return { ...render(ui()), ui, client, onClose, onSuccess };
}
async function prepare() {
  await waitFor(() =>
    expect(screen.getByLabelText("新执行模型")).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("新执行模型"), {
    target: { value: providerId },
  });
  const button = screen.getByRole("button", { name: "预览完整当前事实" });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await screen.findByRole("region", { name: "Agent 执行完整快照" });
}
function consent() {
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对完整当前任务/ }),
  );
  fireEvent.click(screen.getByRole("checkbox", { name: /我确认关联此原运行/ }));
}
it("previews and creates a v6 restart only with independent cross-task consent", async () => {
  mocks.task.projectId = projectFile.sourceTask.project_id;
  mocks.preview.mockResolvedValue({
    ...preview,
    current_start: projectFileProposal().preview.agent_run_start,
  });
  mount();
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  );
  await prepare();
  consent();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我单独同意读取所列精确文件正文/ }),
  );
  const submit = screen.getByRole("button", { name: "确认按当前事实重新执行" });
  expect(submit).toBeDisabled();
  expect(mocks.create).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我另行确认跨任务文件/ }),
  );
  fireEvent.click(submit);
  await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
  expect(mocks.create.mock.calls[0][2]).toMatchObject({
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
it("removing a selected cross-task source clears the full current-facts preview", async () => {
  mocks.task.projectId = projectFile.sourceTask.project_id;
  mocks.preview.mockResolvedValue({
    ...preview,
    current_start: projectFileProposal().preview.agent_run_start,
  });
  mount();
  fireEvent.click(screen.getByRole("button", { name: "选择跨任务已验收文件" }));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "选择跨任务文件 accepted.md" }),
  );
  await prepare();
  consent();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我另行确认跨任务文件/ }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "移除跨任务文件 accepted.md" }),
  );
  expect(
    screen.queryByRole("region", { name: "Agent 执行完整快照" }),
  ).toBeNull();
  expect(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  ).toBeDisabled();
  expect(mocks.create).not.toHaveBeenCalled();
});
it("requires explicit current Provider, full preview and separate restart/execution consent", async () => {
  const result = mount();
  expect(mocks.preview).not.toHaveBeenCalled();
  expect(mocks.create).not.toHaveBeenCalled();
  await prepare();
  expect(screen.getByText(/原任务版本 7/)).toBeVisible();
  const submit = screen.getByRole("button", { name: "确认按当前事实重新执行" });
  expect(submit).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对完整当前任务/ }),
  );
  expect(submit).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: /我确认关联此原运行/ }));
  fireEvent.click(submit);
  await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
  expect(mocks.create).toHaveBeenCalledWith(taskId, providerId, {
    restart: { runId, expectedTaskVersion: 9 },
    outputContract: { type: "text" },
    confirmRestart: true,
    restartPreviewHash: "a".repeat(64),
  });
  expect(mocks.preview).toHaveBeenCalledTimes(2);
  await waitFor(() => expect(result.onSuccess).toHaveBeenCalledOnce());
});
it("restarts with an explicitly chosen remote Anthropic v5 Provider without inheriting files or rework", async () => {
  const current = structuredClone(preview);
  current.current_start.provider.protocol = "anthropic_messages";
  current.current_start.execution_contract_version = 5;
  Object.assign(current.current_start, { output_contract: { type: "text" } });
  Object.assign(current.current_start.runtime_limits, {
    max_output_tokens: 8192,
  });
  mocks.providers.mockResolvedValue([
    { ...provider, protocol: "anthropic_messages" },
  ]);
  mocks.preview.mockResolvedValue(current);
  mount();
  await prepare();
  expect(screen.getByText(/文本结果最多.*8192 输出 tokens/)).toBeVisible();
  expect(
    screen.queryByRole("checkbox", { name: /我另行同意/ }),
  ).not.toBeInTheDocument();
  consent();
  fireEvent.click(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  );
  await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
  expect(mocks.create.mock.calls[0][2]).not.toHaveProperty("confirmFileAccess");
  expect(mocks.create.mock.calls[0][2]).not.toHaveProperty(
    "confirmReworkContext",
  );
});
it("keeps file-list errors visible without blocking an unselected plaintext execution", async () => {
  mocks.files.mockRejectedValue(new Error("file endpoint unavailable"));
  mount();
  await screen.findByText(/受控文件列表读取失败/);
  await prepare();
  consent();
  fireEvent.click(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  );
  await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
  expect(mocks.create.mock.calls[0][2].inputFiles).toBeUndefined();
});
it("does not inherit files and requires separate selected-file consent", async () => {
  mocks.files.mockResolvedValue({
    items: [
      {
        id: taskId,
        sourceKind: "task_artifact",
        name: "input.md",
        sizeBytes: 12,
        eligible: true,
      },
    ],
  });
  const result = mount();
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: /input.md/ })).toBeEnabled(),
  );
  expect(screen.getByRole("checkbox", { name: /input.md/ })).not.toBeChecked();
  fireEvent.click(screen.getByRole("checkbox", { name: /input.md/ }));
  await prepare();
  consent();
  const submit = screen.getByRole("button", { name: "确认按当前事实重新执行" });
  expect(submit).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我单独同意读取所列精确文件/ }),
  );
  fireEvent.click(submit);
  await waitFor(() => expect(result.onSuccess).toHaveBeenCalledOnce());
  expect(mocks.create.mock.calls[0][2]).toMatchObject({
    inputFiles: [{ id: taskId, sourceKind: "task_artifact" }],
    confirmFileAccess: true,
    fileAccessProviderConfirmation: {
      version: 2,
      configVersion: 3,
      kind: "remote",
    },
  });
});
it.each(["queued", "running", "submitted", "pending"])(
  "does not prepare invalid source %s",
  async (state) => {
    mocks.source.mockResolvedValue({
      ...source,
      ...(state === "queued" || state === "running"
        ? { status: state, outputDeliveryStatus: "not_ready" }
        : { outputDeliveryStatus: state }),
    });
    mount();
    await screen.findByText(/原运行不再符合新执行条件/);
    expect(
      screen.getByRole("button", { name: "预览完整当前事实" }),
    ).toBeDisabled();
    expect(mocks.preview).not.toHaveBeenCalled();
  },
);
it("clears preview and all consent when the current Provider version changes", async () => {
  const result = mount();
  await prepare();
  consent();
  act(() =>
    result.client.setQueryData(
      ["ai-providers-for-agent"],
      [{ ...provider, version: 3 }],
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("region", { name: "Agent 执行完整快照" }),
    ).toBeNull(),
  );
  expect(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  ).toBeDisabled();
  expect(mocks.create).not.toHaveBeenCalled();
});
it.each(["unmount", "source", "task"])(
  "cannot post after a deferred preflight and %s",
  async (mode) => {
    const result = mount();
    await prepare();
    consent();
    let resolve!: (value: unknown) => void;
    mocks.preview.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "确认按当前事实重新执行" }),
    );
    await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
    if (mode === "unmount") result.unmount();
    else if (mode === "source") result.rerender(result.ui(taskId));
    else {
      mocks.task = { ...mocks.task, version: 10 };
      result.rerender(result.ui());
    }
    await act(async () => resolve(preview));
    expect(mocks.create).not.toHaveBeenCalled();
    expect(result.onSuccess).not.toHaveBeenCalled();
  },
);
it("does not post on changed authoritative preview and requires another explicit preview", async () => {
  mount();
  await prepare();
  consent();
  mocks.preview.mockResolvedValue({ ...preview, fingerprint: "b".repeat(64) });
  fireEvent.click(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  );
  await screen.findByText(/当前事实已变化，请重新完整预览/);
  expect(mocks.create).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("region", { name: "Agent 执行完整快照" }),
  ).toBeNull();
});
it("requires explicit feedback-only rework selection and fresh independent rework consent", async () => {
  const submissionId = "018f0000-0000-7000-8000-000000000991";
  mocks.task = { ...mocks.task, currentSubmissionId: submissionId };
  const context = {
    submission_id: submissionId,
    sequence: 2,
    review_reason: "补齐可核验证据",
    reviewed_at: "2026-09-21T12:00:00Z",
    artifacts: [],
  };
  mocks.reworkSource.mockResolvedValue({
    task: mocks.task,
    submission: {
      id: submissionId,
      taskId,
      status: "changes_requested",
      sequence: 2,
      reviewReason: context.review_reason,
      artifacts: [],
    },
  });
  mocks.reworkPreview.mockResolvedValue(context);
  mocks.preview.mockResolvedValue({
    ...preview,
    current_start: {
      ...snapshot,
      execution_contract_version: 4,
      rework_context: context,
      output_contract: { type: "text" },
      input_files_leave_device: false,
      file_limits: {
        max_files: 4,
        max_file_bytes: 65536,
        max_total_bytes: 131072,
        max_result_bytes: 65536,
      },
    },
  });
  mount();
  await waitFor(() =>
    expect(screen.getByLabelText("新执行模型")).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("新执行模型"), {
    target: { value: providerId },
  });
  expect(mocks.reworkSource).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /本次明确选择当前退回批次资料/ }),
  );
  const select = await screen.findByLabelText("选择返工批次");
  fireEvent.change(select, { target: { value: submissionId } });
  fireEvent.click(screen.getByRole("button", { name: "预览完整返工上下文" }));
  fireEvent.click(await screen.findByLabelText("同意发送返工意见与旧产出"));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "预览完整当前事实" }),
    ).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "预览完整当前事实" }));
  await screen.findByRole("region", { name: "Agent 执行完整快照" });
  consent();
  const submit = screen.getByRole("button", { name: "确认按当前事实重新执行" });
  expect(submit).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我单独同意发送以上完整返工意见/ }),
  );
  fireEvent.click(submit);
  await waitFor(() => expect(mocks.create).toHaveBeenCalledOnce());
  expect(mocks.create.mock.calls[0][2]).toMatchObject({
    rework: { submissionId, artifactIds: [], expectedTaskVersion: 9 },
    confirmReworkContext: true,
    reworkProviderConfirmation: {
      version: 2,
      configVersion: 3,
      kind: "remote",
    },
  });
  expect(mocks.create.mock.calls[0][2].confirmFileAccess).toBeUndefined();
});
it("does not resume a stale preflight after Provider changes away and back", async () => {
  const result = mount();
  await prepare();
  consent();
  let resolve!: (value: unknown) => void;
  mocks.preview.mockImplementationOnce(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "确认按当前事实重新执行" }),
  );
  await waitFor(() => expect(mocks.preview).toHaveBeenCalledTimes(2));
  act(() =>
    result.client.setQueryData(
      ["ai-providers-for-agent"],
      [{ ...provider, version: 3 }],
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("region", { name: "Agent 执行完整快照" }),
    ).toBeNull(),
  );
  act(() => result.client.setQueryData(["ai-providers-for-agent"], [provider]));
  await act(async () => resolve(preview));
  expect(mocks.create).not.toHaveBeenCalled();
});
it("allows explicit multi-file output but never preselects old output files", async () => {
  mount();
  await waitFor(() =>
    expect(screen.getByLabelText("新执行模型")).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("新执行模型"), {
    target: { value: providerId },
  });
  fireEvent.change(screen.getByLabelText("新执行输出契约"), {
    target: { value: "files" },
  });
  expect(
    screen.getByRole("button", { name: "预览完整当前事实" }),
  ).toBeDisabled();
  const choices = screen
    .getAllByRole("checkbox")
    .filter((c) =>
      c.closest("fieldset")?.textContent?.includes("明确选择本次输出文件"),
    );
  expect(choices.every((c) => !(c as HTMLInputElement).checked)).toBe(true);
  fireEvent.click(choices[0]);
  expect(
    screen.getByRole("button", { name: "预览完整当前事实" }),
  ).toBeDisabled();
  fireEvent.click(choices[1]);
  fireEvent.click(screen.getByRole("button", { name: "预览完整当前事实" }));
  await waitFor(() => expect(mocks.preview).toHaveBeenCalledOnce());
  expect(mocks.preview.mock.calls[0][2].outputContract).toMatchObject({
    type: "files",
    files: expect.any(Array),
  });
  expect(mocks.preview.mock.calls[0][2].outputContract.files).toHaveLength(2);
  expect(mocks.create).not.toHaveBeenCalled();
});
it("can explicitly clear an unavailable file selection instead of inheriting stale files", async () => {
  mocks.files.mockResolvedValue({
    items: [
      {
        id: taskId,
        sourceKind: "task_artifact",
        name: "input.md",
        sizeBytes: 12,
        eligible: true,
      },
    ],
  });
  const result = mount();
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: /input.md/ })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("checkbox", { name: /input.md/ }));
  mocks.files.mockRejectedValue(new Error("unavailable"));
  await act(async () => {
    await result.client.invalidateQueries({
      queryKey: ["task-agent-run-files", taskId],
    });
  });
  expect(
    screen.getByRole("button", { name: "预览完整当前事实" }),
  ).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "清空本次文件选择" }));
  await prepare();
  expect(mocks.preview.mock.calls[0][2].inputFiles).toBeUndefined();
  expect(mocks.create).not.toHaveBeenCalled();
});
