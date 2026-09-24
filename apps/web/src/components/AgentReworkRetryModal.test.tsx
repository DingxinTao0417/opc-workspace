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
import { AgentReworkRetryModal } from "./AgentReworkRetryModal";
import type { AgentRun, AiProvider } from "../types/models";
import { projectWireFile } from "../test/agentProjectFileFixture";

const mocks = vi.hoisted(() => ({
  detail: vi.fn(),
  providers: vi.fn(),
  retry: vi.fn(),
}));
vi.mock("../api/client", () => ({
  getAgentRun: mocks.detail,
  getAiProviders: mocks.providers,
  retryAgentRun: mocks.retry,
}));
const taskId = "018f0000-0000-7000-8000-000000000950";
const runId = "018f0000-0000-7000-8000-000000000951";
const provider = {
  id: "provider",
  name: "原远程模型",
  kind: "remote",
  version: 2,
  configVersion: 3,
  status: "ready",
  health_status: "healthy",
  protocol: "openai_chat",
} as AiProvider;
const run = {
  id: runId,
  taskId,
  providerId: provider.id,
  model: "frozen-model",
  executionContractVersion: 4,
  status: "failed",
  outputDeliveryStatus: "not_ready",
  attempt: 1,
  reworkProviderConfirmation: { version: 2, configVersion: 3, kind: "remote" },
  reworkContext: {
    submission_id: taskId,
    sequence: 2,
    review_reason: "保留精确数据",
    reviewed_at: "2026-09-21T12:00:00Z",
    artifacts: [
      {
        id: runId,
        storage_kind: "structured",
        name: "旧草稿",
        content: '{"exact":1e100}',
        sha256: "a".repeat(64),
      },
    ],
  },
  reworkInputFiles: [
    {
      id: taskId,
      source_kind: "task_artifact",
      name: "input.md",
      mime: "text/markdown",
      size_bytes: 12,
      sha256: "b".repeat(64),
    },
  ],
} as AgentRun;
function anthropicRun(withFiles = false, withRework = false): AgentRun {
  return {
    ...run,
    executionContractVersion: 5,
    modelProtocol: "anthropic_messages",
    maxOutputTokens: 8192,
    outputContract: { type: "text" },
    executionProviderConfirmation: run.reworkProviderConfirmation,
    executionInputFiles: withFiles ? run.reworkInputFiles : [],
    reworkProviderConfirmation: undefined,
    reworkInputFiles: undefined,
    reworkContext: withRework ? run.reworkContext : undefined,
  };
}
beforeEach(() => {
  vi.resetAllMocks();
  mocks.detail.mockResolvedValue(run);
  mocks.providers.mockResolvedValue([provider]);
  mocks.retry.mockResolvedValue({
    ...run,
    id: taskId,
    attempt: 2,
    status: "queued",
  });
});
afterEach(cleanup);
it.each([false, true])(
  "requires a third independent source consent for v6 anthropic=%s",
  async (anthropic) => {
    const current: AgentRun = {
      ...anthropicRun(true),
      executionContractVersion: 6,
      modelProtocol: anthropic ? "anthropic_messages" : "openai_chat",
      maxOutputTokens: anthropic ? 8192 : 0,
      executionInputFiles: [projectWireFile],
    };
    mocks.detail.mockResolvedValue(current);
    mocks.providers.mockResolvedValue([
      { ...provider, protocol: current.modelProtocol },
    ]);
    mount();
    const button = await screen.findByRole("button", {
      name: "确认重试原执行",
    });
    await screen.findByText(/来源任务「已验收的来源任务」/);
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我同意重新调用模型执行/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行同意读取并发送/ }),
    );
    expect(button).toBeDisabled();
    expect(mocks.retry).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行确认原跨任务文件/ }),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(mocks.retry).toHaveBeenCalledWith(runId, {
        confirmFileAccess: true,
        fileAccessProviderConfirmation: current.executionProviderConfirmation,
        confirmProjectTaskFiles: true,
      }),
    );
  },
);
it.each([
  [false, false],
  [true, false],
  [false, true],
  [true, true],
])(
  "requires v5 confirmations only for real files=%s and rework=%s",
  async (withFiles, withRework) => {
    const current = anthropicRun(withFiles, withRework);
    mocks.detail.mockResolvedValue(current);
    mocks.providers.mockResolvedValue([
      { ...provider, protocol: "anthropic_messages" },
    ]);
    mount();
    await screen.findByText(/冻结协议 Anthropic Messages/);
    expect(screen.getByText(/最多 8192 输出 tokens/)).toBeVisible();
    const button = screen.getByRole("button", { name: "确认重试原执行" });
    expect(button).toBeDisabled();
    expect(mocks.retry).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我同意重新调用模型执行/ }),
    );
    const fileCheck = screen.queryByRole("checkbox", {
      name: /我另行同意读取并发送/,
    });
    const reworkCheck = screen.queryByRole("checkbox", {
      name: /我已核对完整返工意见和旧稿/,
    });
    expect(!!fileCheck).toBe(withFiles);
    expect(!!reworkCheck).toBe(withRework);
    if (fileCheck) {
      expect(button).toBeDisabled();
      fireEvent.click(fileCheck);
    }
    if (reworkCheck) {
      expect(button).toBeDisabled();
      fireEvent.click(reworkCheck);
    }
    fireEvent.click(button);
    await waitFor(() => expect(mocks.retry).toHaveBeenCalledOnce());
    const options = {
      ...(withFiles
        ? {
            confirmFileAccess: true,
            fileAccessProviderConfirmation:
              current.executionProviderConfirmation,
          }
        : {}),
      ...(withRework
        ? {
            confirmReworkContext: true,
            reworkProviderConfirmation: current.executionProviderConfirmation,
          }
        : {}),
    };
    expect(mocks.retry.mock.calls[0]).toEqual(
      withFiles || withRework ? [runId, options] : [runId],
    );
  },
);
it.each(["protocol", "tokens", "kind", "providerVersion"])(
  "blocks v5 frozen identity drift: %s",
  async (kind) => {
    const current = anthropicRun();
    const currentProvider = { ...provider, protocol: "anthropic_messages" };
    if (kind === "protocol") currentProvider.protocol = "openai_chat";
    if (kind === "tokens") Object.assign(current, { maxOutputTokens: 8193 });
    if (kind === "kind") currentProvider.kind = "local";
    if (kind === "providerVersion") currentProvider.version += 1;
    mocks.detail.mockResolvedValue(current);
    mocks.providers.mockResolvedValue([currentProvider]);
    mount();
    await screen.findByRole("alert");
    expect(
      screen.getByRole("button", { name: "确认重试原执行" }),
    ).toBeDisabled();
    expect(mocks.retry).not.toHaveBeenCalled();
  },
);
it("does not send a v5 retry after its detail precheck returns to an unmounted source", async () => {
  const current = anthropicRun();
  let finish!: (value: AgentRun) => void;
  mocks.detail.mockResolvedValueOnce(current).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  mocks.providers.mockResolvedValue([
    { ...provider, protocol: "anthropic_messages" },
  ]);
  const view = mount();
  await screen.findByText(/冻结协议 Anthropic Messages/);
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我同意重新调用模型执行/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认重试原执行" }));
  await waitFor(() => expect(mocks.detail).toHaveBeenCalledTimes(2));
  view.unmount();
  await act(async () => finish(current));
  expect(mocks.retry).not.toHaveBeenCalled();
});
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const onClose = vi.fn();
  const onSuccess = vi.fn();
  const ui = (id = runId) => (
    <QueryClientProvider client={client}>
      <AgentReworkRetryModal
        taskId={taskId}
        runId={id}
        onClose={onClose}
        onSuccess={onSuccess}
      />
    </QueryClientProvider>
  );
  return { ...render(ui()), ui, client, onClose, onSuccess };
}
async function consent() {
  await screen.findByText('{"exact":1e100}');
  await waitFor(() =>
    expect(screen.queryByRole("status")).not.toBeInTheDocument(),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对完整返工意见和旧稿/ }),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我同意重新调用模型执行/ }),
  );
}
it("shows complete frozen text and requires file, rework and execution consent independently", async () => {
  const view = mount();
  await consent();
  const submit = screen.getByRole("button", { name: "确认重试原返工执行" });
  expect(submit).toBeDisabled();
  expect(screen.getByText(/内容会离开本机/)).toBeVisible();
  expect(mocks.retry).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我另行同意读取并发送/ }),
  );
  expect(submit).toBeEnabled();
  fireEvent.click(submit);
  await waitFor(() =>
    expect(mocks.retry).toHaveBeenCalledWith(runId, {
      confirmReworkContext: true,
      reworkProviderConfirmation: run.reworkProviderConfirmation,
      confirmFileAccess: true,
      fileAccessProviderConfirmation: run.reworkProviderConfirmation,
    }),
  );
  expect(mocks.detail).toHaveBeenCalledTimes(2);
  await waitFor(() => expect(view.onSuccess).toHaveBeenCalledOnce());
});
it("does not invent file consent for a reason-only retry", async () => {
  mocks.detail.mockResolvedValue({
    ...run,
    reworkInputFiles: [],
    reworkContext: { ...run.reworkContext!, artifacts: [] },
  });
  mount();
  await screen.findByText(/本次仅携带返工意见/);
  expect(
    screen.queryByRole("checkbox", { name: /我另行同意读取并发送/ }),
  ).not.toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对完整返工意见和旧稿/ }),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我同意重新调用模型执行/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认重试原返工执行" }));
  await waitFor(() =>
    expect(mocks.retry).toHaveBeenCalledWith(runId, {
      confirmReworkContext: true,
      reworkProviderConfirmation: run.reworkProviderConfirmation,
    }),
  );
});
it.each(["unmount", "switch", "aba"])(
  "does not POST after a late detail precheck and %s",
  async (change) => {
    let resolve!: (value: AgentRun) => void;
    mocks.detail.mockResolvedValueOnce(run).mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const view = mount();
    await consent();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我另行同意读取并发送/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认重试原返工执行" }));
    await waitFor(() => expect(mocks.detail).toHaveBeenCalledTimes(2));
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(view.onClose).not.toHaveBeenCalled();
    const done = resolve;
    if (change === "unmount") view.unmount();
    else {
      view.rerender(view.ui(taskId));
      if (change === "aba") view.rerender(view.ui());
    }
    await act(async () => {
      done(run);
    });
    expect(mocks.retry).not.toHaveBeenCalled();
    expect(view.onSuccess).not.toHaveBeenCalled();
  },
);
it("revokes all consent while exact source is being refetched, even if unchanged", async () => {
  const view = mount();
  await consent();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我另行同意读取并发送/ }),
  );
  let resolve!: (value: AgentRun) => void;
  mocks.detail.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  void view.client.invalidateQueries({
    queryKey: ["agent-rework-retry-detail"],
  });
  await waitFor(() =>
    expect(
      screen.getByRole("checkbox", { name: /我已核对完整返工意见和旧稿/ }),
    ).not.toBeChecked(),
  );
  await act(async () => {
    resolve(run);
  });
  expect(
    screen.getByRole("button", { name: "确认重试原返工执行" }),
  ).toBeDisabled();
});
it.each(["provider", "succeeded", "pending", "missing-context"])(
  "rejects unavailable frozen facts: %s",
  async (kind) => {
    if (kind === "provider")
      mocks.providers.mockResolvedValue([{ ...provider, configVersion: 4 }]);
    else
      mocks.detail.mockResolvedValue({
        ...run,
        ...(kind === "succeeded"
          ? { status: "succeeded" }
          : kind === "pending"
            ? { outputDeliveryStatus: "pending" }
            : { reworkContext: undefined }),
      });
    mount();
    await screen.findByRole("alert");
    expect(
      screen.getByRole("button", { name: "确认重试原返工执行" }),
    ).toBeDisabled();
    expect(mocks.retry).not.toHaveBeenCalled();
  },
);
