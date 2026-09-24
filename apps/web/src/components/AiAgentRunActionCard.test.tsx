import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AiActionProposal } from "../api/aiWorkspaceActions";
import type { AgentRun } from "../types/models";
import { AiWorkspaceActions } from "./AiWorkspaceActions";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import { projectFileProposal } from "../test/agentProjectFileFixture";

const state = vi.hoisted(() => ({
  load: vi.fn(),
  decide: vi.fn(),
  getRun: vi.fn(),
  retryDelivery: vi.fn(),
}));
vi.mock("../api/aiWorkspaceActions", async () => ({
  ...(await vi.importActual("../api/aiWorkspaceActions")),
  getAiWorkspaceActions: state.load,
  decideAiAgentRun: state.decide,
  decideAiAgentRunCancel: state.decide,
  decideAiAgentRunRecovery: state.decide,
}));
vi.mock("../api/client", async () => ({
  ...(await vi.importActual("../api/client")),
  getAgentRun: state.getRun,
  retryAgentRunOutputDelivery: state.retryDelivery,
}));

const taskId = "018f0000-0000-7000-8000-000000000301";
const providerId = "018f0000-0000-7000-8000-000000000302";
const runId = "018f0000-0000-7000-8000-000000000303";
const submissionId = "018f0000-0000-7000-8000-000000000310";
const artifactId = "018f0000-0000-7000-8000-000000000311";
const proposal = {
  id: "018f0000-0000-7000-8000-000000000304",
  generation_id: "018f0000-0000-7000-8000-000000000305",
  fingerprint: "a".repeat(64),
  action: {
    action: "agent_run.start",
    task_id: taskId,
    expected_version: 7,
    changes: {
      provider_id: providerId,
      expected_provider_version: 4,
      expected_provider_config_version: 5,
    },
  },
  preview: {
    label: "整理季度报告",
    before: {},
    after: {},
    agent_run_start: {
      task: {
        id: taskId,
        title: "整理季度报告",
        description: "核对财务数据并形成摘要",
        completion_criteria: "数字准确且经负责人检查",
        status: "in_progress",
        kind: "work",
        review_policy: "manual",
        priority: "P1",
        project_id: null,
        parent_task_id: null,
        due_date: null,
        planned_date: "2026-09-19",
        estimated_minutes: 90,
        actual_minutes: 15,
        manual_order: 2,
        version: 7,
        created_at: "2026-09-18T08:00:00Z",
        updated_at: "2026-09-18T09:00:00Z",
      },
      assignment: {
        id: "018f0000-0000-7000-8000-000000000306",
        role: "assignee",
        assigned_at: "2026-09-18T08:30:00Z",
        actor_id: "018f0000-0000-7000-8000-000000000307",
      },
      agent: {
        id: "018f0000-0000-7000-8000-000000000307",
        display_name: "报告 Agent",
        type: "agent",
        status: "active",
        version: 2,
      },
      adapter: {
        id: "018f0000-0000-7000-8000-000000000308",
        display_name: "内置执行器",
        kind: "builtin",
        protocol_version: "opc-agent-pipe-v1",
        status: "enabled",
        health_status: "healthy",
        isolation_status: "verified",
        execution_ready: true,
        version: 3,
      },
      provider: {
        id: providerId,
        name: "远程模型",
        kind: "remote",
        protocol: "openai_chat",
        model: "test-model",
        status: "ready",
        health_status: "healthy",
        version: 4,
        config_version: 5,
        leaves_device: true,
      },
      attempt: 1,
      execution_contract_version: 1,
      runtime_limits: { timeout_seconds: 600, max_result_bytes: 65_536 },
      success_does_not_complete_task: true,
      result_requires_manual_review: true,
    },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: `/tasks/${taskId}`,
  created_at: "2026-09-18T10:00:00Z",
  decided_at: null,
} as unknown as AiActionProposal;

function run(overrides: Partial<AgentRun> = {}): AgentRun {
  return {
    id: runId,
    taskId,
    assignmentId: proposal.preview.agent_run_start!.assignment.id,
    actorId: proposal.preview.agent_run_start!.agent.id,
    adapterId: proposal.preview.agent_run_start!.adapter.id,
    createdByActorId: "018f0000-0000-7000-8000-000000000312",
    parentRunId: null,
    attempt: 1,
    status: "queued",
    providerId,
    model: "test-model",
    resultText: null,
    resultBytes: null,
    errorCode: null,
    outputDeliveryStatus: "not_ready",
    outputDeliveryErrorCode: null,
    submissionId: null,
    artifactId: null,
    startedAt: null,
    completedAt: null,
    createdAt: "2026-09-18T10:01:00Z",
    ...overrides,
  };
}

function mount(onContinue?: (request: AiActionContinuation) => void) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions
          generationId={proposal.generation_id}
          sessionId="018f0000-0000-7000-8000-000000000309"
          onContinue={onContinue}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return { ...view, client };
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Agent execution start gates", () => {
  const predecessor = "018f0000-0000-7000-8000-0000000003a1";
  const gatedProposal = {
    ...proposal,
    action: {
      ...proposal.action,
      changes: {
        ...proposal.action.changes,
        start_after_run_id: predecessor,
        start_after: "accepted",
        start_within_hours: 6,
      },
    },
    preview: {
      ...proposal.preview,
      agent_run_start: {
        ...proposal.preview.agent_run_start!,
        start_gate: {
          predecessor_run_id: predecessor,
          predecessor_task_id: "018f0000-0000-7000-8000-0000000003a2",
          predecessor_task_title: "撰写季度初稿",
          predecessor_attempt: 2,
          require: "accepted",
          within_hours: 6,
        },
      },
    },
  } as unknown as AiActionProposal;
  const waitingGate = {
    predecessor_run_id: predecessor,
    require: "accepted" as const,
    expires_at: "2026-09-18T16:01:00Z",
    status: "waiting" as const,
  };

  it("discloses the deferred start and asks for deferred consent", async () => {
    state.load.mockReset().mockResolvedValue([gatedProposal]);
    mount();
    expect(
      await screen.findByText("满足条件后由系统启动 · 不是现在启动"),
    ).toBeVisible();
    expect(
      screen.getByText(/等待任务「撰写季度初稿」第 2 次执行/),
    ).toHaveTextContent("被人工验收接受");
    expect(
      screen.getByRole("checkbox", {
        name: /同意在条件满足后由系统启动这一次执行/,
      }),
    ).not.toBeChecked();
    expect(
      screen.queryByRole("checkbox", { name: /同意现在启动一次执行/ }),
    ).toBeNull();
  });

  it("shows a confirmed gated Run as waiting, not started", async () => {
    state.load.mockReset().mockResolvedValue([
      {
        ...gatedProposal,
        status: "confirmed",
        can_confirm: false,
        result_id: runId,
        result_version: 1,
        route: `/tasks/${taskId}?agent_run=${runId}`,
        decided_at: "2026-09-18T10:01:00Z",
        agent_run_result: {
          id: runId,
          task_id: taskId,
          status: "queued",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
          output_delivery_status: "not_ready",
          output_delivery_error_code: null,
          submission_id: null,
          artifact_id: null,
          start_gate: waitingGate,
        },
      },
    ]);
    state.getRun.mockReset().mockResolvedValue(
      run({
        startedAt: "2026-09-18T10:01:00Z",
        startGate: {
          predecessorRunId: predecessor,
          require: "accepted",
          expiresAt: waitingGate.expires_at,
          status: "waiting",
        },
      }),
    );
    mount();
    expect(await screen.findByText("已确认 · 等待前置执行")).toBeVisible();
    expect(screen.queryByText(/已启动 · 排队中/)).toBeNull();
  });
});

describe("Agent execution confirmation card", () => {
  it("requires independent cross-task consent and resets it with source metadata", async () => {
    const p = projectFileProposal();
    p.generation_id = proposal.generation_id;
    state.load.mockReset().mockResolvedValue([p]);
    const view = mount();
    const submit = await screen.findByRole("button", {
      name: "确认按当前事实重新执行",
    });
    await screen.findByText(/来源任务「已验收的来源任务」/);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已核对任务完整快照、Agent、Adapter、Provider/,
      }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我确认关联此原运行/ }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我已核对上方每个受控输入文件/ }),
    );
    expect(submit).toBeDisabled();
    const extra = screen.getByRole("checkbox", {
      name: /我另行确认所列跨任务文件/,
    });
    fireEvent.click(extra);
    expect(submit).toBeEnabled();
    const changed = structuredClone(p);
    changed.preview.agent_run_start!.input_files![0].source_task!
      .task_version++;
    state.load.mockResolvedValue([changed]);
    await act(async () => {
      await view.client.invalidateQueries({ queryKey: ["ai"] });
    });
    await waitFor(() => expect(extra).not.toBeChecked());
    expect(submit).toBeDisabled();
    expect(state.decide).not.toHaveBeenCalled();
    for (const name of [
      /我已核对任务完整快照、Agent、Adapter、Provider/,
      /我确认关联此原运行/,
      /我已核对上方每个受控输入文件/,
      /我另行确认所列跨任务文件/,
    ])
      fireEvent.click(screen.getByRole("checkbox", { name }));
    state.decide.mockResolvedValue({
      ...changed,
      status: "rejected",
      can_confirm: false,
    });
    fireEvent.click(submit);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        changed,
        "confirm",
        true,
        true,
        undefined,
        true,
        true,
      ),
    );
  });
  it("requires restart consent separately and resets it when current facts change", async () => {
    const restart = structuredClone(proposal);
    restart.action.changes.restart_of_run_id = runId;
    restart.preview.agent_run_start!.restart = {
      run_id: runId,
      task_id: taskId,
      status: "succeeded",
      output_delivery_status: "retained",
      attempt: 8,
      task_version: 5,
      actor_id: restart.preview.agent_run_start!.agent.id,
      provider_id: providerId,
      model: "original-model",
      completed_at: "2026-09-21T12:00:00Z",
    };
    state.load.mockReset().mockResolvedValue([restart]);
    const view = mount();
    const submit = await screen.findByRole("button", {
      name: "确认按当前事实重新执行",
    });
    expect(screen.getByText(/原任务版本 5/)).toBeVisible();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已核对任务完整快照、Agent、Adapter、Provider/,
      }),
    );
    expect(submit).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我确认关联此原运行/ }),
    );
    expect(submit).toBeEnabled();
    expect(state.decide).not.toHaveBeenCalled();
    const changed = structuredClone(restart);
    changed.preview.agent_run_start!.task.description = "新的当前说明";
    state.load.mockResolvedValue([changed]);
    await act(async () => {
      await view.client.invalidateQueries({ queryKey: ["ai"] });
    });
    await screen.findByText("新的当前说明");
    expect(
      screen.getByRole("checkbox", { name: /我确认关联此原运行/ }),
    ).not.toBeChecked();
    expect(submit).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已核对任务完整快照、Agent、Adapter、Provider/,
      }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我确认关联此原运行/ }),
    );
    state.decide.mockResolvedValue({
      ...changed,
      status: "rejected",
      can_confirm: false,
    });
    fireEvent.click(submit);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        changed,
        "confirm",
        true,
        undefined,
        undefined,
        true,
      ),
    );
  });
  it.each([false, true])(
    "refreshes the full approval receipt after real Run polling without manual refresh or automatic commands (pending sibling=%s)",
    async (pendingSibling) => {
      vi.useFakeTimers();
      const network = vi.fn();
      vi.stubGlobal("fetch", network);
      const onContinue = vi.fn();
      let current = run();
      state.getRun.mockReset().mockImplementation(async () => current);
      const sibling: AiActionProposal = {
        ...proposal,
        id: "018f0000-0000-7000-8000-000000000399",
        action: { action: "task.update", task_id: taskId, changes: {} },
        preview: { label: "同轮另一个待确认操作", before: {}, after: {} },
      };
      state.load.mockReset().mockImplementation(async () => [
        {
          ...proposal,
          status: "confirmed",
          can_confirm: false,
          result_id: runId,
          result_version: 1,
          agent_run_result: {
            id: runId,
            task_id: taskId,
            status: current.status,
            attempt: 1,
            provider_id: providerId,
            model: "test-model",
            output_delivery_status: current.outputDeliveryStatus,
            output_delivery_error_code: current.outputDeliveryErrorCode,
            submission_id: current.submissionId,
            artifact_id: current.artifactId,
          },
        },
        ...(pendingSibling ? [sibling] : []),
      ]);
      const { client } = mount(onContinue);
      await act(async () => vi.advanceTimersByTimeAsync(0));
      await vi.waitFor(() => expect(state.getRun).toHaveBeenCalledOnce());
      expect(
        screen.queryByRole("button", { name: "继续处理这一轮" }),
      ).toBeNull();

      current = run({ status: "running" });
      await act(async () => vi.advanceTimersByTimeAsync(2_000));
      expect(screen.getByText("已启动 · 执行中")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "继续处理这一轮" }),
      ).toBeNull();

      current = run({ status: "running", outputDeliveryStatus: "pending" });
      await act(async () => vi.advanceTimersByTimeAsync(2_000));
      expect(screen.getByText("结果待登记")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "继续处理这一轮" }),
      ).toBeNull();

      current = run({
        status: "succeeded",
        outputDeliveryStatus: "submitted",
        submissionId,
        artifactId,
      });
      await act(async () => vi.advanceTimersByTimeAsync(2_000));
      await vi.waitFor(() =>
        expect(screen.getByText("已提交为任务产出")).toBeVisible(),
      );
      await vi.waitFor(() => expect(state.load).toHaveBeenCalledTimes(2));
      if (pendingSibling) {
        expect(
          screen.queryByRole("button", { name: "继续处理这一轮" }),
        ).toBeNull();
        expect(
          screen.getByText("这一轮仍有待确认操作，请先处理全部建议。"),
        ).toBeVisible();
      } else {
        await vi.waitFor(() =>
          expect(
            screen.getByRole("button", { name: "继续处理这一轮" }),
          ).toBeEnabled(),
        );
      }
      expect(state.load).toHaveBeenCalledTimes(2);
      expect(state.getRun).toHaveBeenCalledTimes(4);
      await act(async () => vi.advanceTimersByTimeAsync(10_000));
      expect(state.load).toHaveBeenCalledTimes(2);
      expect(state.getRun).toHaveBeenCalledTimes(4);
      expect(state.decide).not.toHaveBeenCalled();
      expect(state.retryDelivery).not.toHaveBeenCalled();
      expect(onContinue).not.toHaveBeenCalled();
      expect(network).not.toHaveBeenCalled();
      client.clear();
    },
  );
  it("recovers stored output only after its separate consent", async () => {
    const recovery: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_run.recover_output",
        task_id: taskId,
        agent_run_id: runId,
        expected_version: 7,
        changes: {},
      },
      preview: {
        label: "整理季度报告",
        before: {},
        after: {},
        agent_run_recovery: {
          run_id: runId,
          task_id: taskId,
          task_version: 7,
          status: "running",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
          output_kind: "text",
          output_bytes: 20,
          output_sha256: "b".repeat(64),
        },
      },
      route: `/tasks/${taskId}?agent_run=${runId}`,
    };
    state.load.mockReset().mockResolvedValue([recovery]);
    state.decide.mockResolvedValue(recovery);
    mount();
    const button = await screen.findByRole("button", { name: "确认恢复登记" });
    expect(button).toBeDisabled();
    expect(screen.getByText(/不重新调用模型/)).toBeInTheDocument();
    expect(
      screen.queryByRole("checkbox", { name: /同意现在启动一次执行/ }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我确认恢复这一次执行的已有产出登记/,
      }),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(recovery, "confirm", true),
    );
  });
  it("requires fresh consent for retry and shows the original attempt", async () => {
    const retry: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_run.retry",
        task_id: taskId,
        agent_run_id: runId,
        expected_version: 7,
        changes: {},
      },
      preview: {
        ...proposal.preview,
        agent_run_start: { ...proposal.preview.agent_run_start!, attempt: 2 },
        agent_run_retry: { run_id: runId, status: "failed", attempt: 1 },
      },
      route: `/tasks/${taskId}?agent_run=${runId}`,
    };
    state.load.mockReset().mockResolvedValue([retry]);
    state.decide.mockResolvedValue(retry);
    mount();
    const button = await screen.findByRole("button", {
      name: "确认重试 Agent",
    });
    expect(button).toBeDisabled();
    expect(screen.getByText(/不覆盖原记录/)).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我已核对任务完整快照/ }),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(retry, "confirm", true),
    );
  });
  it("requires cancellation consent and distinguishes accepted intent from stopped execution", async () => {
    const cancelProposal: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_run.cancel",
        task_id: taskId,
        agent_run_id: runId,
        expected_version: 7,
        changes: {},
      },
      preview: {
        label: "整理季度报告",
        before: {},
        after: {},
        agent_run_cancel: {
          run_id: runId,
          task_id: taskId,
          task_version: 7,
          status: "running",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
        },
      },
      route: `/tasks/${taskId}?agent_run=${runId}`,
    };
    const accepted: AiActionProposal = {
      ...cancelProposal,
      status: "confirmed",
      can_confirm: false,
      result_id: runId,
      result_version: 1,
      decided_at: "2026-09-19T00:00:00Z",
      agent_run_result: {
        id: runId,
        task_id: taskId,
        status: "running",
        attempt: 1,
        provider_id: providerId,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    state.load
      .mockReset()
      .mockResolvedValueOnce([cancelProposal])
      .mockResolvedValue([accepted]);
    state.decide.mockResolvedValue(accepted);
    state.getRun.mockResolvedValue(run({ status: "running" }));
    const { client } = mount();
    const button = await screen.findByRole("button", { name: "确认停止执行" });
    expect(button).toBeDisabled();
    expect(
      screen.getByText(/仅停止这一次执行，不取消任务/),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我已核对执行 ID 与任务/ }),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        cancelProposal,
        "confirm",
        true,
      ),
    );
    expect(
      await screen.findByText("停止请求已接受 · 等待执行器收尾"),
    ).toBeInTheDocument();
    expect(screen.queryByText("执行已停止")).not.toBeInTheDocument();
    state.getRun.mockResolvedValue(run({ status: "cancelled" }));
    await client.invalidateQueries({ queryKey: ["agent-runs"] });
    // Exact run detail uses a separate key; invalidate all reads to emulate
    // the next status poll without issuing any second cancellation command.
    await client.invalidateQueries();
    expect(await screen.findByText("执行已停止")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledTimes(1);
  });
  it("shows every batch interruption target and requires one explicit consent", async () => {
    const secondTask = "018f0000-0000-7000-8000-000000000251";
    const secondRun = "018f0000-0000-7000-8000-000000000252";
    const batch: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_run.cancel_many",
        changes: {
          items: [
            {
              agent_run_id: runId,
              task_id: taskId,
              expected_task_version: 7,
            },
            {
              agent_run_id: secondRun,
              task_id: secondTask,
              expected_task_version: 3,
            },
          ],
        },
      },
      preview: {
        label: "停止 2 个 Agent 执行",
        before: { active_runs: 2 },
        after: { cancel_requested_runs: 2 },
        agent_run_cancel_many: {
          count: 2,
          items: [
            {
              run_id: runId,
              task_id: taskId,
              task_title: "整理季度报告",
              task_version: 7,
              status: "running",
              attempt: 1,
              provider_id: providerId,
              model: "test-model",
            },
            {
              run_id: secondRun,
              task_id: secondTask,
              task_title: "复核季度报告",
              task_version: 3,
              status: "queued",
              attempt: 1,
              provider_id: providerId,
              model: "test-model",
            },
          ],
        },
      },
      route: "/ai?workspace=agents",
    };
    const accepted: AiActionProposal = {
      ...batch,
      status: "confirmed",
      can_confirm: false,
      result_id: batch.id,
      result_version: 1,
      decided_at: "2026-09-19T00:00:00Z",
      agent_run_cancel_many_result: {
        count: 2,
        items: [
          { run_id: runId, task_id: taskId, status: "running" },
          { run_id: secondRun, task_id: secondTask, status: "cancelled" },
        ],
      },
    };
    state.load
      .mockReset()
      .mockResolvedValueOnce([batch])
      .mockResolvedValue([accepted]);
    state.decide.mockResolvedValue(accepted);
    mount();
    expect(await screen.findByText("整理季度报告")).toBeInTheDocument();
    expect(screen.getByText("复核季度报告")).toBeInTheDocument();
    const button = screen.getByRole("button", { name: "确认停止 2 个执行" });
    expect(button).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /逐项核对这 2 个执行/ }),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(batch, "confirm", true),
    );
    expect(await screen.findByText("批量停止请求已接受")).toBeInTheDocument();
    expect(state.getRun).not.toHaveBeenCalled();
  });
  beforeEach(() => {
    const confirmed = {
      ...proposal,
      status: "confirmed",
      can_confirm: false,
      result_id: runId,
      result_version: 1,
      route: `/tasks/${taskId}?agent_run=${runId}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: runId,
        task_id: taskId,
        status: "queued",
        attempt: 1,
        provider_id: providerId,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    state.load
      .mockReset()
      .mockResolvedValueOnce([proposal])
      .mockResolvedValue([confirmed]);
    state.decide.mockReset().mockResolvedValue(confirmed);
    state.getRun.mockReset().mockResolvedValue(run());
    state.retryDelivery.mockReset();
  });

  it("shows the frozen execution boundary and requires a separate human checkbox", async () => {
    mount();
    expect(await screen.findByText("启动 Agent 执行")).toBeVisible();
    expect(screen.getByText("数字准确且经负责人检查")).toBeVisible();
    expect(screen.getByText(/远程服务/)).toBeVisible();
    expect(screen.getByText(/10 分钟/)).toBeVisible();
    expect(screen.getByText(/64 KiB/)).toBeVisible();
    expect(screen.getAllByText(/不会自动完成任务/).length).toBeGreaterThan(0);

    const confirm = screen.getByRole("button", { name: "确认启动 Agent" });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已核对任务完整快照、Agent、Adapter、Provider/,
      }),
    );
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(proposal, "confirm", true),
    );
    expect(await screen.findByText("已启动 · 排队中")).toBeVisible();
    expect(screen.getByRole("link", { name: "查看执行过程" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}?agent_run=${runId}&return_session=018f0000-0000-7000-8000-000000000309`,
    );
  });

  it("derives the remote boundary for a legacy v1 card without leaves_device", async () => {
    const legacy = structuredClone(proposal);
    delete (legacy.preview.agent_run_start!.provider as Record<string, unknown>)
      .leaves_device;
    state.load.mockReset().mockResolvedValue([legacy]);
    mount();
    expect(await screen.findByText(/数据离开本机/)).toBeVisible();
  });

  it.each([
    [4, false],
    [4, true],
    [5, false],
    [5, true],
  ] as const)(
    "requires v%s independent rework consent with files=%s and revokes it on a new snapshot",
    async (version, withFiles) => {
      const rework = structuredClone(proposal);
      const preview = rework.preview.agent_run_start!;
      preview.execution_contract_version = version;
      if (version === 5) {
        preview.provider.protocol = "anthropic_messages";
        preview.runtime_limits.max_output_tokens = 8192;
      }
      preview.rework_context = {
        submission_id: submissionId,
        sequence: 2,
        review_reason: "补齐精确数据",
        reviewed_at: "2026-09-21T12:00:00Z",
        artifacts: [
          {
            id: artifactId,
            storage_kind: "structured",
            name: "完整旧稿",
            content: '{"exact":1e100}',
            sha256: "b".repeat(64),
          },
        ],
      };
      preview.output_contract = { type: "text" };
      preview.file_limits = {
        max_files: 4,
        max_file_bytes: 65536,
        max_total_bytes: 131072,
        max_result_bytes: 65536,
      };
      preview.input_files_leave_device = withFiles;
      if (version === 5 && !withFiles) {
        delete preview.file_limits;
        delete preview.input_files_leave_device;
      }
      if (withFiles) {
        preview.input_files = [
          {
            source_kind: "task_artifact",
            id: runId,
            name: "input.md",
            mime: "text/markdown",
            size_bytes: 12,
            sha256: "a".repeat(64),
          },
        ];
        preview.input_file_total_bytes = 12;
      }
      rework.action.changes = {
        ...rework.action.changes,
        rework_submission_id: submissionId,
        rework_artifact_ids: [artifactId],
      };
      state.load.mockReset().mockResolvedValue([rework]);
      const view = mount();
      expect(await screen.findByText('{"exact":1e100}')).toBeVisible();
      const submit = screen.getByRole("button", { name: "确认启动 Agent" });
      fireEvent.click(
        screen.getByRole("checkbox", {
          name: /我已核对任务完整快照、Agent、Adapter、Provider/,
        }),
      );
      if (withFiles)
        fireEvent.click(
          screen.getByRole("checkbox", {
            name: /我已核对上方每个受控输入文件/,
          }),
        );
      else
        expect(
          screen.queryByRole("checkbox", {
            name: /我已核对上方每个受控输入文件/,
          }),
        ).not.toBeInTheDocument();
      expect(submit).toBeDisabled();
      fireEvent.click(
        screen.getByRole("checkbox", {
          name: /我已核对完整返工意见与所选旧产出/,
        }),
      );
      expect(submit).toBeEnabled();
      expect(state.decide).not.toHaveBeenCalled();
      const changed = structuredClone(rework);
      changed.preview.agent_run_start!.rework_context!.review_reason =
        "新意见必须重新核对";
      state.load.mockResolvedValue([changed]);
      await act(async () => {
        await view.client.invalidateQueries({ queryKey: ["ai"] });
      });
      await screen.findByText("新意见必须重新核对");
      expect(
        screen.getByRole("checkbox", {
          name: /我已核对完整返工意见与所选旧产出/,
        }),
      ).not.toBeChecked();
      expect(submit).toBeDisabled();
      fireEvent.click(
        screen.getByRole("checkbox", {
          name: /我已核对任务完整快照、Agent、Adapter、Provider/,
        }),
      );
      if (withFiles)
        fireEvent.click(
          screen.getByRole("checkbox", {
            name: /我已核对上方每个受控输入文件/,
          }),
        );
      fireEvent.click(
        screen.getByRole("checkbox", {
          name: /我已核对完整返工意见与所选旧产出/,
        }),
      );
      fireEvent.click(submit);
      await waitFor(() =>
        expect(state.decide).toHaveBeenCalledWith(
          changed,
          "confirm",
          true,
          withFiles ? true : undefined,
          true,
        ),
      );
    },
  );
  it("shows v5 protocol/token limits and needs only execution consent for plain text", async () => {
    const value = structuredClone(proposal);
    const current = value.preview.agent_run_start!;
    current.execution_contract_version = 5;
    current.provider.protocol = "anthropic_messages";
    current.runtime_limits.max_output_tokens = 8192;
    current.output_contract = { type: "text" };
    state.load.mockReset().mockResolvedValue([value]);
    const view = mount();
    expect(await screen.findByText(/anthropic_messages/)).toBeVisible();
    expect(screen.getByText(/最多 8192 输出 tokens/)).toBeVisible();
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    const button = screen.getByRole("button", { name: "确认启动 Agent" });
    expect(button).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox"));
    expect(button).toBeEnabled();
    const changed = structuredClone(value);
    changed.preview.agent_run_start!.provider.config_version += 1;
    state.load.mockResolvedValue([changed]);
    await act(async () => {
      await view.client.invalidateQueries({ queryKey: ["ai"] });
    });
    await waitFor(() => expect(screen.getByRole("checkbox")).not.toBeChecked());
    expect(button).toBeDisabled();
    expect(state.decide).not.toHaveBeenCalled();
  });

  it("shows only frozen file metadata and requires independent remote file-body consent", async () => {
    const controlled = structuredClone(proposal);
    controlled.action.changes = {
      ...controlled.action.changes,
      input_file_candidate_ids: ["018f0000-0000-7000-8000-000000000313"],
      output_kind: "file",
    };
    const preview = controlled.preview.agent_run_start!;
    preview.execution_contract_version = 2;
    preview.input_files = [
      {
        source_kind: "task_artifact",
        id: "018f0000-0000-7000-8000-000000000314",
        name: "quarter-source.md",
        mime: "text/markdown",
        size_bytes: 26,
        sha256: "b".repeat(64),
      },
    ];
    preview.input_file_total_bytes = 26;
    preview.input_files_leave_device = false;
    preview.output_contract = {
      type: "file",
      name: "agent-run-attempt-1.md",
      mime: "text/markdown",
    };
    preview.file_limits = {
      max_files: 4,
      max_file_bytes: 65_536,
      max_total_bytes: 131_072,
      max_result_bytes: 65_536,
    };
    const confirmed = {
      ...controlled,
      status: "confirmed",
      can_confirm: false,
      result_id: runId,
      result_version: 1,
      route: `/tasks/${taskId}?agent_run=${runId}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: runId,
        task_id: taskId,
        status: "queued",
        attempt: 1,
        provider_id: providerId,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    state.load
      .mockReset()
      .mockResolvedValueOnce([controlled])
      .mockResolvedValue([confirmed]);
    state.decide.mockReset().mockResolvedValue(confirmed);

    mount();
    expect(await screen.findByText("quarter-source.md")).toBeVisible();
    expect(
      screen.getByText("agent-run-attempt-1.md", { exact: false }),
    ).toBeVisible();
    expect(screen.getAllByText(/文件将离开本机/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/C:\\/)).not.toBeInTheDocument();

    const confirm = screen.getByRole("button", { name: "确认启动 Agent" });
    const executionConsent = screen.getByRole("checkbox", {
      name: /我已核对任务完整快照、Agent、Adapter、Provider/,
    });
    const fileConsent = screen.getByRole("checkbox", {
      name: /我已核对上方每个受控输入文件/,
    });
    fireEvent.click(executionConsent);
    expect(confirm).toBeDisabled();
    fireEvent.click(fileConsent);
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        controlled,
        "confirm",
        true,
        true,
      ),
    );
  });

  it("does not request file-body consent for a server-named file output without inputs", async () => {
    const fileOutput = structuredClone(proposal);
    fileOutput.action.changes = {
      ...fileOutput.action.changes,
      output_kind: "file",
    };
    const preview = fileOutput.preview.agent_run_start!;
    preview.execution_contract_version = 2;
    preview.input_files_leave_device = true;
    preview.output_contract = {
      type: "file",
      name: "agent-run-attempt-1.md",
      mime: "text/markdown",
    };
    preview.file_limits = {
      max_files: 4,
      max_file_bytes: 65_536,
      max_total_bytes: 131_072,
      max_result_bytes: 65_536,
    };
    state.load.mockReset().mockResolvedValue([fileOutput]);

    mount();
    expect(await screen.findByText("本次没有选择受控输入文件。")).toBeVisible();
    expect(
      screen.queryByRole("checkbox", {
        name: /我已核对上方每个受控输入文件/,
      }),
    ).not.toBeInTheDocument();
    const confirm = screen.getByRole("button", { name: "确认启动 Agent" });
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已核对任务完整快照、Agent、Adapter、Provider/,
      }),
    );
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(fileOutput, "confirm", true),
    );
  });

  it("links a submitted result to the exact submission without claiming acceptance", async () => {
    const submitted = run({
      status: "succeeded",
      resultText: "季度报告",
      resultBytes: 12,
      completedAt: "2026-09-18T10:02:00Z",
      outputDeliveryStatus: "submitted",
      submissionId,
      artifactId,
    });
    state.load.mockReset().mockResolvedValue([
      {
        ...proposal,
        status: "confirmed",
        can_confirm: false,
        result_id: runId,
        result_version: 1,
        route: `/tasks/${taskId}?agent_run=${runId}`,
        decided_at: "2026-09-18T10:01:00Z",
        agent_run_result: {
          id: runId,
          task_id: taskId,
          status: "succeeded",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
          output_delivery_status: "submitted",
          output_delivery_error_code: null,
          submission_id: submissionId,
          artifact_id: artifactId,
        },
      },
    ]);
    state.getRun.mockReset().mockResolvedValue(submitted);
    mount();

    expect(await screen.findByText("已提交为任务产出")).toBeVisible();
    expect(screen.queryByText(/已验收/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看提交批次" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}/submissions/${submissionId}?return_session=018f0000-0000-7000-8000-000000000309`,
    );
  });

  it.each([
    ["the same Submission", true],
    ["a different Submission", false],
  ])(
    "reports human acceptance only when the receipt names %s",
    async (_name, sameSubmission) => {
      const submitted = run({
        status: "succeeded",
        resultText: "季度报告",
        resultBytes: 12,
        completedAt: "2026-09-18T10:02:00Z",
        outputDeliveryStatus: "submitted",
        submissionId,
        artifactId,
      });
      state.load.mockReset().mockResolvedValue([
        {
          ...proposal,
          status: "confirmed",
          can_confirm: false,
          result_id: runId,
          result_version: 1,
          route: `/tasks/${taskId}?agent_run=${runId}`,
          decided_at: "2026-09-18T10:01:00Z",
          agent_run_result: {
            id: runId,
            task_id: taskId,
            status: "succeeded",
            attempt: 1,
            provider_id: providerId,
            model: "test-model",
            output_delivery_status: "submitted",
            output_delivery_error_code: null,
            submission_id: sameSubmission
              ? submissionId
              : "018f0000-0000-7000-8000-0000000003ff",
            submission_status: "accepted",
            artifact_id: artifactId,
          },
        },
      ]);
      state.getRun.mockReset().mockResolvedValue(submitted);
      mount();

      if (sameSubmission) {
        expect(await screen.findByText("产出已验收")).toBeVisible();
      } else {
        expect(await screen.findByText("已提交为任务产出")).toBeVisible();
        expect(screen.queryByText("产出已验收")).not.toBeInTheDocument();
      }
    },
  );

  it("shows a safe retained reason and keeps the exact Run route", async () => {
    const retained = run({
      status: "succeeded",
      resultText: "季度报告",
      resultBytes: 12,
      completedAt: "2026-09-18T10:02:00Z",
      outputDeliveryStatus: "retained",
      outputDeliveryErrorCode: "TASK_SUBMISSION_NOT_ALLOWED",
    });
    state.load.mockReset().mockResolvedValue([
      {
        ...proposal,
        status: "confirmed",
        can_confirm: false,
        result_id: runId,
        result_version: 1,
        route: `/tasks/${taskId}?agent_run=${runId}`,
        decided_at: "2026-09-18T10:01:00Z",
        agent_run_result: {
          id: runId,
          task_id: taskId,
          status: "succeeded",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
          output_delivery_status: "retained",
          output_delivery_error_code: "TASK_SUBMISSION_NOT_ALLOWED",
          submission_id: null,
          artifact_id: null,
        },
      },
    ]);
    state.getRun.mockReset().mockResolvedValue(retained);
    mount();

    expect(await screen.findByText("结果已保留 · 未提交")).toBeVisible();
    expect(screen.getByText(/任务当前状态不允许登记新产出/)).toBeVisible();
    expect(screen.getByRole("link", { name: "查看执行过程" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}?agent_run=${runId}&return_session=018f0000-0000-7000-8000-000000000309`,
    );
  });

  it("renders a deleted Run as historical confirmation without polling or a dead link", async () => {
    state.load.mockReset().mockResolvedValue([
      {
        ...proposal,
        status: "confirmed",
        can_confirm: false,
        result_id: runId,
        result_version: 1,
        route: "",
        decided_at: "2026-09-18T10:01:00Z",
      },
    ]);
    state.getRun.mockReset();
    mount();

    expect(await screen.findByText("已确认 · 执行记录已删除")).toBeVisible();
    expect(
      screen.getByText(/本次确认历史仍会保留，但无法再读取运行状态/),
    ).toBeVisible();
    expect(
      screen.queryByRole("link", { name: /查看执行过程|查看任务/ }),
    ).not.toBeInTheDocument();
    expect(state.getRun).not.toHaveBeenCalled();
  });

  it("requires a human click before retrying a pending delivery", async () => {
    const pendingRun = run({
      status: "running",
      startedAt: "2026-09-18T10:01:01Z",
      resultText: null,
      resultBytes: null,
      outputDeliveryStatus: "pending",
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
    });
    const submitted = {
      ...pendingRun,
      status: "succeeded" as const,
      completedAt: "2026-09-18T10:02:00Z",
      resultText: "季度报告",
      resultBytes: 12,
      outputDeliveryStatus: "submitted" as const,
      outputDeliveryErrorCode: null,
      submissionId,
      artifactId,
    };
    state.load.mockReset().mockResolvedValue([
      {
        ...proposal,
        status: "confirmed",
        can_confirm: false,
        result_id: runId,
        result_version: 1,
        route: `/tasks/${taskId}?agent_run=${runId}`,
        decided_at: "2026-09-18T10:01:00Z",
        agent_run_result: {
          id: runId,
          task_id: taskId,
          status: "running",
          attempt: 1,
          provider_id: providerId,
          model: "test-model",
          output_delivery_status: "pending",
          output_delivery_error_code: "AGENT_OUTPUT_DELIVERY_PENDING",
          submission_id: null,
          artifact_id: null,
        },
      },
    ]);
    state.getRun.mockReset().mockResolvedValue(pendingRun);
    state.retryDelivery.mockImplementation(async () => {
      state.getRun.mockResolvedValue(submitted);
      return submitted;
    });
    mount();

    const retry = await screen.findByRole("button", {
      name: "重试登记产出",
    });
    expect(state.retryDelivery).not.toHaveBeenCalled();
    fireEvent.click(retry);
    await waitFor(() =>
      expect(state.retryDelivery).toHaveBeenCalledWith(runId),
    );
    expect(await screen.findByText("已提交为任务产出")).toBeVisible();
  });
});
