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
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import {
  getAgentRun,
  getTask,
  getTaskAgentRuns,
  retryAgentRunOutputDelivery,
} from "../api/client";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
import { useUiStore } from "../store/ui";
import { useAiChatStore } from "../store/aiChat";
import type { AgentRun, Task } from "../types/models";
import { AgentRunDrawer } from "./AgentRunDrawer";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getTask: vi.fn(),
  getTaskAgentRuns: vi.fn(),
  getAgentRun: vi.fn(),
  retryAgentRunOutputDelivery: vi.fn(),
}));

const task = {
  id: "018f0000-0000-7000-8000-000000000501",
  title: "整理季度报告",
} as Task;
const completedRun: AgentRun = {
  id: "018f0000-0000-7000-8000-000000000502",
  taskId: task.id,
  assignmentId: "assignment-1",
  actorId: "agent-1",
  adapterId: "adapter-1",
  createdByActorId: "owner-1",
  parentRunId: "run-1",
  attempt: 2,
  status: "succeeded",
  providerId: "provider-1",
  model: "deepseek-test",
  resultText: "<script>alert('unsafe')</script>\n季度报告正文",
  resultBytes: 60,
  errorCode: null,
  outputDeliveryStatus: "retained",
  outputDeliveryErrorCode: "TASK_SUBMISSION_NOT_ALLOWED",
  submissionId: null,
  artifactId: null,
  startedAt: "2026-09-12T12:00:01Z",
  completedAt: "2026-09-12T12:01:00Z",
  createdAt: "2026-09-12T12:00:00Z",
};
const failedRun: AgentRun = {
  ...completedRun,
  id: "018f0000-0000-7000-8000-000000000503",
  parentRunId: null,
  attempt: 1,
  status: "failed",
  resultText: null,
  resultBytes: null,
  errorCode: "MODEL_UNAVAILABLE",
  outputDeliveryStatus: "not_ready",
  outputDeliveryErrorCode: null,
  submissionId: null,
  artifactId: null,
};

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="drawer-location">
      {location.pathname}
      {location.search}
    </output>
  );
}
function renderDrawer(
  runId: string | null = completedRun.id,
  returnSession?: string,
) {
  useUiStore.setState({
    agentRunDrawer: { taskId: task.id, runId, ...{ returnSession } },
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <AgentRunDrawer />
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

beforeEach(() => {
  vi.resetAllMocks();
  useUiStore.setState({ agentRunDrawer: null, taskDetailId: null });
  useAiChatStore.setState({ activeSessionId: "before-drawer-session" });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  useAgentRunFileApply.getState().clear();
  vi.mocked(getTask).mockResolvedValue(task);
  vi.mocked(getTaskAgentRuns).mockResolvedValue([completedRun, failedRun]);
  vi.mocked(getAgentRun).mockImplementation(async (id) => {
    if (id === completedRun.id) return completedRun;
    if (id === failedRun.id) return failedRun;
    throw new Error("not found");
  });
});

afterEach(() => {
  cleanup();
  useUiStore.setState({ agentRunDrawer: null, taskDetailId: null });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  useAgentRunFileApply.getState().clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentRunDrawer", () => {
  it("shows safe execution phase and elapsed time while a Run is active", async () => {
    const activeRun: AgentRun = {
      ...failedRun,
      status: "running",
      resultText: null,
      resultBytes: null,
      errorCode: null,
      outputDeliveryStatus: "not_ready",
      outputDeliveryErrorCode: null,
      completedAt: null,
      progress: { phase: "registering_result", elapsedMs: 125_000 },
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([activeRun]);
    vi.mocked(getAgentRun).mockResolvedValue(activeRun);
    renderDrawer(activeRun.id);
    expect(await screen.findByText("正在校验并登记完整结果")).toBeVisible();
    expect(screen.getByText(/已运行 2 分 5 秒/)).toBeVisible();
  });

  it("opens the exact retry parent from the run lineage", async () => {
    renderDrawer();
    fireEvent.click(
      await screen.findByRole("button", { name: "打开重试来源" }),
    );
    expect(useUiStore.getState().agentRunDrawer).toMatchObject({
      taskId: task.id,
      runId: completedRun.parentRunId,
    });
  });

  it("hands a submitted file output to the local Files review without writing", async () => {
    const submittedFile = {
      ...completedRun,
      outputDeliveryStatus: "submitted" as const,
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000401",
      artifactId: "018f0000-0000-7000-8000-000000000402",
      outputContract: {
        type: "file" as const,
        name: "quarter.md",
        mime: "text/markdown",
      },
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([submittedFile]);
    vi.mocked(getAgentRun).mockResolvedValue(submittedFile);
    renderDrawer(submittedFile.id);

    fireEvent.click(
      await screen.findByRole("button", {
        name: "用于项目文件：quarter.md",
      }),
    );

    expect(useAgentRunFileApply.getState().candidate).toMatchObject({
      runId: submittedFile.id,
      submissionId: submittedFile.submissionId,
      artifactId: submittedFile.artifactId,
      name: "quarter.md",
      content: submittedFile.resultText,
    });
    expect(useAgentRunFileApply.getState().review).toBeNull();
    expect(useUiStore.getState()).toMatchObject({
      agentRunDrawer: null,
      rightOverviewCollapsed: false,
      rightPanelTab: "files",
      rightPanelRequestMode: "activate",
    });
  });

  it("opens an exact-source new execution preview from a retained drawer without recovering or switching conversations", async () => {
    vi.mocked(getAgentRun).mockResolvedValue({
      ...completedRun,
      outputDeliveryStatus: "retained",
      outputDeliveryErrorCode: "AGENT_RUN_IDENTITY_CHANGED",
    });
    renderDrawer();
    fireEvent.click(
      await screen.findByRole("button", { name: "按当前事实重新执行" }),
    );
    expect(
      screen.getByRole("region", { name: "新执行预览入口" }),
    ).toHaveTextContent(`${task.id} / ${completedRun.id}`);
    expect(retryAgentRunOutputDelivery).not.toHaveBeenCalled();
    expect(useAiChatStore.getState().activeSessionId).toBe(
      "before-drawer-session",
    );
  });
  it.each([
    ["AGENT_MODEL_TRUNCATED", "模型输出未完整结束，未登记产出。"],
    ["AGENT_MODEL_FILTERED", "模型拒绝或过滤了输出，未登记产出。"],
    [
      "AGENT_MODEL_RESPONSE_INVALID",
      "模型响应不符合完整文本交付协议，未登记产出。",
    ],
  ])(
    "explains terminal model failure %s without creating delivery actions",
    async (errorCode, message) => {
      const failed = { ...failedRun, errorCode };
      vi.mocked(getTaskAgentRuns).mockResolvedValue([failed]);
      vi.mocked(getAgentRun).mockResolvedValue(failed);
      renderDrawer(failed.id);
      expect(await screen.findByText(message)).toBeVisible();
      expect(screen.getByText(errorCode)).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "重试登记产出" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("link", { name: "打开提交批次" }),
      ).not.toBeInTheDocument();
      expect(retryAgentRunOutputDelivery).not.toHaveBeenCalled();
    },
  );
  it.each(["返回原对话", "打开提交批次"])(
    "keeps the drawer and current session for modifier or middle clicks on %s",
    async (label) => {
      const submitted = {
        ...completedRun,
        outputDeliveryStatus: "submitted" as const,
        submissionId: "018f0000-0000-7000-8000-000000000401",
        artifactId: "018f0000-0000-7000-8000-000000000402",
      };
      vi.mocked(getAgentRun).mockResolvedValue(submitted);
      renderDrawer(completedRun.id, "018f0000-0000-7000-8000-000000000599");
      const link = await screen.findByRole("link", { name: label });
      for (const modifiers of [
        { ctrlKey: true },
        { metaKey: true },
        { shiftKey: true },
        { altKey: true },
        { button: 1 },
      ]) {
        document.addEventListener("click", (event) => event.preventDefault(), {
          once: true,
        });
        fireEvent.click(link, modifiers);
        expect(useUiStore.getState().agentRunDrawer?.runId).toBe(
          completedRun.id,
        );
        expect(useAiChatStore.getState().activeSessionId).toBe(
          "before-drawer-session",
        );
        expect(screen.getByTestId("drawer-location").textContent).toBe("/");
      }
      fireEvent(link, new MouseEvent("auxclick", { bubbles: true, button: 1 }));
      expect(useUiStore.getState().agentRunDrawer?.runId).toBe(completedRun.id);
    },
  );
  it("preserves an explicit conversation across selection and returns only on click", async () => {
    const session = "018f0000-0000-7000-8000-000000000599";
    useAiChatStore.setState({ activeSessionId: "before-drawer-session" });
    renderDrawer(failedRun.id, session);
    await screen.findByText("MODEL_UNAVAILABLE");
    expect(useAiChatStore.getState().activeSessionId).toBe(
      "before-drawer-session",
    );
    fireEvent.change(screen.getByLabelText("执行记录"), {
      target: { value: completedRun.id },
    });
    expect(useUiStore.getState().agentRunDrawer).toMatchObject({
      runId: completedRun.id,
      returnSession: session,
    });
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(screen.getByTestId("drawer-location")).toHaveTextContent("/ai");
    expect(useAiChatStore.getState().activeSessionId).toBe(session);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });
  it.each([undefined, "https://evil.invalid", "invalid&run=other"])(
    "does not infer an absent or invalid return identity %s",
    async (session) => {
      renderDrawer(failedRun.id, session);
      await screen.findByText("MODEL_UNAVAILABLE");
      expect(
        screen.queryByRole("link", { name: "返回原对话" }),
      ).not.toBeInTheDocument();
    },
  );
  it("keeps return on exact missing imported Run without recreating or substituting", async () => {
    const missing = "018f0000-0000-7000-8000-000000000598";
    renderDrawer(missing, "018f0000-0000-7000-8000-000000000599");
    expect(
      await screen.findByText("无法精确读取所选执行记录，未展示其他记录代替。"),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
    expect(
      screen.queryByText(completedRun.resultText!),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /重试执行|启动执行|重新创建/ }),
    ).not.toBeInTheDocument();
  });
  it("shows service facts and treats result HTML as plain text without execution controls", async () => {
    renderDrawer();
    const dialog = await screen.findByRole("dialog", {
      name: "执行过程",
    });
    expect(await within(dialog).findByText(task.title)).toBeVisible();
    expect(
      await within(dialog).findByText(completedRun.model, { selector: "dd" }),
    ).toBeVisible();
    expect(
      within(dialog).getByText("第 2 次", { selector: "dd" }),
    ).toBeVisible();
    expect(within(dialog).getByText("创建时间")).toBeVisible();
    expect(within(dialog).getByText("开始时间")).toBeVisible();
    expect(within(dialog).getByText("结束时间")).toBeVisible();
    expect(dialog.querySelector("pre")?.textContent).toBe(
      completedRun.resultText,
    );
    expect(dialog.querySelector("script, iframe")).toBeNull();
    expect(getAgentRun).toHaveBeenCalledWith(
      completedRun.id,
      expect.any(AbortSignal),
    );
    expect(
      within(dialog).queryByRole("button", {
        name: /^(启动执行|重试执行|取消执行)$/,
      }),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).getByText(/任务当前状态不允许登记新产出/),
    ).toBeVisible();
  });

  it("links a submitted output to its exact batch without claiming review", async () => {
    const submitted = {
      ...completedRun,
      outputDeliveryStatus: "submitted" as const,
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000401",
      artifactId: "018f0000-0000-7000-8000-000000000402",
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([submitted]);
    vi.mocked(getAgentRun).mockResolvedValue(submitted);
    renderDrawer();

    const link = await screen.findByRole("link", { name: "打开提交批次" });
    expect(link).toHaveAttribute(
      "href",
      `/tasks/${task.id}/submissions/${submitted.submissionId}`,
    );
    expect(screen.getByText(/执行成功没有代替人工验收/)).toBeVisible();
    expect(screen.queryByText(/验收通过/)).not.toBeInTheDocument();
  });

  it("carries the explicit return identity to a submitted batch and closes only on click", async () => {
    const session = "018f0000-0000-7000-8000-000000000599";
    const submitted = {
      ...completedRun,
      outputDeliveryStatus: "submitted" as const,
      submissionId: "018f0000-0000-7000-8000-000000000401",
      artifactId: "018f0000-0000-7000-8000-000000000402",
    };
    vi.mocked(getAgentRun).mockResolvedValue(submitted);
    renderDrawer(completedRun.id, session);
    const link = await screen.findByRole("link", { name: "打开提交批次" });
    expect(link).toHaveAttribute(
      "href",
      `/tasks/${task.id}/submissions/${submitted.submissionId}?return_session=${session}`,
    );
    expect(useUiStore.getState().agentRunDrawer).not.toBeNull();
    fireEvent.click(link);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
    expect(useAiChatStore.getState().activeSessionId).toBe(
      "before-drawer-session",
    );
  });

  it("blocks return, close and history while manual delivery recovery is in flight", async () => {
    const pendingRun = {
      ...completedRun,
      status: "running" as const,
      resultText: null,
      outputDeliveryStatus: "pending" as const,
    };
    let finish!: (value: AgentRun) => void;
    vi.mocked(getAgentRun).mockResolvedValue(pendingRun);
    vi.mocked(retryAgentRunOutputDelivery).mockReturnValue(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    const { queryClient } = renderDrawer(
      completedRun.id,
      "018f0000-0000-7000-8000-000000000599",
    );
    expect(retryAgentRunOutputDelivery).not.toHaveBeenCalled();
    fireEvent.click(
      await screen.findByRole("button", { name: "重试登记产出" }),
    );
    await waitFor(() =>
      expect(retryAgentRunOutputDelivery).toHaveBeenCalledOnce(),
    );
    expect(
      screen.queryByRole("link", { name: "返回原对话" }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "打开任务" })).toBeDisabled();
    expect(screen.getByLabelText("执行记录")).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(
      screen.queryByRole("button", { name: "收起执行过程" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "收起" }));
    expect(useUiStore.getState().agentRunDrawer).not.toBeNull();
    await act(async () => finish(completedRun));
    await waitFor(() =>
      expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible(),
    );
    queryClient.clear();
  });

  it("distinguishes completed execution with pending delivery from an active model run", async () => {
    const pendingDelivery = {
      ...completedRun,
      status: "running" as const,
      resultText: null,
      resultBytes: null,
      completedAt: null,
      outputDeliveryStatus: "pending" as const,
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
    };
    vi.mocked(getTaskAgentRuns).mockResolvedValue([pendingDelivery]);
    vi.mocked(getAgentRun).mockResolvedValue(pendingDelivery);
    renderDrawer();

    const dialog = await screen.findByRole("dialog", { name: "执行过程" });
    expect(
      (await within(dialog).findAllByText("结果待登记")).length,
    ).toBeGreaterThan(0);
    expect(
      within(dialog).getByText(/结果正文暂存于受保护的恢复区/),
    ).toBeVisible();
    expect(within(dialog).queryByText(/执行尚未结束/)).not.toBeInTheDocument();
  });

  it.each([
    [
      "waiting",
      {
        status: "queued" as const,
        startedAt: "2026-09-12T12:00:00Z",
        completedAt: null,
        startGate: {
          predecessorRunId: "018f0000-0000-7000-8000-000000000601",
          require: "accepted" as const,
          expiresAt: "2026-09-12T14:00:00Z",
          status: "waiting" as const,
        },
      },
      "等待前置执行",
      /等待前置执行的产出被人工验收接受/,
    ],
    [
      "closed",
      {
        status: "cancelled" as const,
        completedAt: "2026-09-12T12:30:00Z",
        startGate: {
          predecessorRunId: "018f0000-0000-7000-8000-000000000601",
          require: "accepted" as const,
          expiresAt: "2026-09-12T14:00:00Z",
          status: "closed" as const,
          closeReason: "predecessor_failed" as const,
        },
      },
      "未启动 · 前置条件未满足",
      /前置执行失败或中断，这次执行没有启动/,
    ],
  ])(
    "explains a %s start gate without claiming execution",
    async (_name, overrides, badge, fact) => {
      const gated = { ...failedRun, errorCode: null, ...overrides };
      vi.mocked(getTaskAgentRuns).mockResolvedValue([gated]);
      vi.mocked(getAgentRun).mockResolvedValue(gated);
      renderDrawer(gated.id);

      const dialog = await screen.findByRole("dialog", { name: "执行过程" });
      expect(
        (await within(dialog).findAllByText(badge)).length,
      ).toBeGreaterThan(0);
      expect(within(dialog).getByText(fact)).toBeVisible();
      expect(
        dialog
          .querySelector(".task-agent-run-status")
          ?.getAttribute("data-phase"),
      ).toBe(_name === "waiting" ? "pending" : "cancelled");
    },
  );

  it("selects a historical run and shows its error instead of another run's result", async () => {
    renderDrawer();
    fireEvent.change(
      await screen.findByRole("combobox", { name: "执行记录" }),
      { target: { value: failedRun.id } },
    );
    expect(await screen.findByText("MODEL_UNAVAILABLE")).toBeVisible();
    expect(screen.getByText("此次执行没有可用文本产出。")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
    expect(useUiStore.getState().agentRunDrawer?.runId).toBe(failedRun.id);
  });

  it("hands off the exact failed run from the drawer without opening or mutating it", async () => {
    renderDrawer(failedRun.id);

    fireEvent.click(await screen.findByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "Agent 执行需要处理",
      route: `/tasks/${task.id}?agent_run=${failedRun.id}`,
      scopes: ["work", "outputs", "actions", "agent_execution"],
    });
    expect(pending?.prompt).toContain("任务「整理季度报告」");
    expect(pending?.prompt).toContain("第 1 次 Agent 执行状态是执行失败");
    expect(pending?.prompt).toContain("workspace_agent_runs");
    expect(pending?.prompt).toContain("workspace_outputs");
    expect(useUiStore.getState().taskDetailId).toBeNull();
    expect(useUiStore.getState().agentRunDrawer?.runId).toBe(failedRun.id);
  });

  it("hands off a retained result for diagnosis without starting delivery recovery", async () => {
    renderDrawer(completedRun.id);

    fireEvent.click(await screen.findByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "Agent 执行需要处理",
      route: `/tasks/${task.id}?agent_run=${completedRun.id}`,
      scopes: ["work", "outputs", "actions", "agent_execution"],
    });
    expect(pending?.prompt).toContain("结果已保留但尚未作为任务产出提交");
    expect(pending?.prompt).toContain("不能再通过产出登记恢复接口提交");
    expect(useUiStore.getState().taskDetailId).toBeNull();
    expect(useUiStore.getState().agentRunDrawer?.runId).toBe(completedRun.id);
  });

  it("closes the drawer with its accessible close control", async () => {
    renderDrawer();
    fireEvent.click(
      await screen.findByRole("button", { name: "收起执行过程" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });

  it("opens the task and closes only the run drawer", async () => {
    renderDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "打开任务" }));
    expect(useUiStore.getState().taskDetailId).toBe(task.id);
    expect(useUiStore.getState().agentRunDrawer).toBeNull();
  });

  it("shows a loading state until records arrive", async () => {
    let resolveRuns!: (runs: AgentRun[]) => void;
    vi.mocked(getTaskAgentRuns).mockReturnValue(
      new Promise((resolve) => {
        resolveRuns = resolve;
      }),
    );
    renderDrawer();
    expect(screen.getByText("正在读取执行记录…")).toBeVisible();
    await act(async () => resolveRuns([completedRun]));
    expect(
      await screen.findByRole("button", { name: "下载文本" }),
    ).toBeVisible();
  });

  it("shows an empty state and no invented run result", async () => {
    vi.mocked(getTaskAgentRuns).mockResolvedValue([]);
    renderDrawer(null);
    expect(await screen.findByText("此任务尚无执行记录。")).toBeVisible();
    expect(
      screen.queryByRole("combobox", { name: "执行记录" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
  });

  it("can refresh a failed record request", async () => {
    vi.mocked(getTaskAgentRuns).mockRejectedValue(new Error("offline"));
    renderDrawer();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "无法读取最新执行记录",
    );
    vi.mocked(getTaskAgentRuns).mockResolvedValue([completedRun]);
    fireEvent.click(screen.getByRole("button", { name: "刷新执行记录" }));
    expect(
      await screen.findByRole("button", { name: "下载文本" }),
    ).toBeVisible();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("does not silently substitute another run when the requested run is missing", async () => {
    renderDrawer("missing-run");
    expect(
      await screen.findByText("无法精确读取所选执行记录，未展示其他记录代替。"),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
  });

  it("rejects an exact run whose task identity does not match the route", async () => {
    vi.mocked(getAgentRun).mockResolvedValue({
      ...completedRun,
      taskId: "another-task",
    });
    renderDrawer();
    expect(
      await screen.findByText(
        "所选执行记录不属于当前任务，未展示其他记录代替。",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载文本" }),
    ).not.toBeInTheDocument();
  });

  it("downloads HTML-looking output as a plain text file", async () => {
    const createObjectURL = vi.fn((_blob: Blob) => "blob:run-result");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal(
      "URL",
      class extends URL {
        static createObjectURL = createObjectURL;
        static revokeObjectURL = revokeObjectURL;
      },
    );
    let downloadedName = "";
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      downloadedName = this.download;
    });
    renderDrawer();
    fireEvent.click(await screen.findByRole("button", { name: "下载文本" }));
    expect(createObjectURL).toHaveBeenCalledOnce();
    expect(createObjectURL.mock.calls[0]?.[0]).toHaveProperty(
      "type",
      "text/plain;charset=utf-8",
    );
    expect(downloadedName).toBe("整理季度报告-第2次.txt");
    await waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:run-result"),
    );
  });
});
