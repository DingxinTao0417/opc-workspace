import {
  cleanup,
  render,
  screen,
  fireEvent,
  waitFor,
  within,
  act,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  closeAiWorkPlan,
  getAiWorkPlan,
  getAiWorkPlanContinuation,
  type AiWorkPlanContinuation,
  type AiWorkPlan as Plan,
} from "../api/aiWorkPlan";
import { ApiError } from "../api/client";
import { AiWorkPlan } from "./AiWorkPlan";
import { useAiChatStore } from "../store/aiChat";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
vi.mock("../api/aiPlanContinuation", async () => ({
  ...(await vi.importActual("../api/aiPlanContinuation")),
  getAiPlanContinuation: vi.fn(async () => null),
}));
vi.mock("../api/aiWorkPlan", async () => ({
  ...(await vi.importActual("../api/aiWorkPlan")),
  closeAiWorkPlan: vi.fn(),
  getAiWorkPlan: vi.fn(),
  getAiWorkPlanContinuation: vi.fn(),
}));
vi.mock("./AiWorkspaceActions", () => ({
  AiWorkspaceActions: ({
    generationId,
    onContinue,
  }: {
    generationId: string;
    onContinue?: (request: AiActionContinuation) => void;
  }) => (
    <div>
      确认卡 {generationId}
      {onContinue ? (
        <button
          type="button"
          onClick={() =>
            onContinue({
              generationId,
              prompt: "固定继续提示",
              scopes: ["work", "actions"],
            })
          }
        >
          模拟审批接续
        </button>
      ) : null}
    </div>
  ),
}));
const session = "018f0000-0000-7000-8000-000000000901";
const generation = "018f0000-0000-7000-8000-000000000902";
const task = "018f0000-0000-7000-8000-000000000904";
const originalRun = "018f0000-0000-7000-8000-000000000907";
const retryRun = "018f0000-0000-7000-8000-000000000908";
const retryGeneration = "018f0000-0000-7000-8000-000000000909";
function fixture(): Plan {
  return {
    session_id: session,
    generation_id: generation,
    generation_status: "completed",
    version: 2,
    title: "完成报告",
    created_at: "2026-09-19T12:00:00Z",
    as_of: "2026-09-19T12:01:00Z",
    steps: [
      {
        id: "check",
        title: "检查要求",
        kind: "analysis",
        depends_on: [],
        report: "reported_done",
        state: "reported_done",
        ready: true,
        satisfied: true,
      },
      {
        id: "run",
        title: "生成报告",
        kind: "action",
        depends_on: ["check"],
        proposal_id: task,
        state: "output_pending",
        ready: true,
        satisfied: false,
        evidence: {
          proposal_id: task,
          generation_id: generation,
          action: "agent_run.start",
          status: "confirmed",
          result_id: task,
          route: `/tasks/${task}`,
        },
      },
      {
        id: "review",
        title: "复查产出",
        kind: "analysis",
        depends_on: ["run"],
        report: "pending",
        state: "pending",
        ready: false,
        satisfied: false,
      },
    ],
  };
}
function replacedPlan(): Plan {
  const plan = fixture();
  const old = plan.steps[1];
  old.state = "rejected";
  old.satisfied = false;
  old.superseded_by = "replacement";
  old.evidence!.status = "rejected";
  delete old.evidence!.result_id;
  const proposal = "018f0000-0000-7000-8000-000000000905";
  plan.steps.splice(2, 0, {
    id: "replacement",
    title: "登记替代任务",
    kind: "action",
    depends_on: ["check"],
    proposal_id: proposal,
    replaces: "run",
    state: "recorded",
    ready: true,
    satisfied: true,
    evidence: {
      proposal_id: proposal,
      generation_id: "018f0000-0000-7000-8000-000000000906",
      action: "task.create",
      status: "confirmed",
      result_id: task,
    },
  });
  Object.assign(plan.steps[3], {
    depends_on: ["replacement"],
    report: "reported_done",
    state: "reported_done",
    ready: true,
    satisfied: true,
  });
  return plan;
}
function retryPlan(oldState = "failed", retryState = "pending"): Plan {
  const plan = fixture();
  const old = plan.steps[1];
  Object.assign(old, {
    state: oldState,
    satisfied: false,
    superseded_by: "retry",
  });
  old.evidence!.result_id = originalRun;
  const submitted = retryState === "submitted";
  const pending = retryState === "pending";
  const proposal = "018f0000-0000-7000-8000-000000000910";
  plan.steps.splice(2, 0, {
    id: "retry",
    title: "重试生成报告",
    kind: "action",
    depends_on: ["check"],
    proposal_id: proposal,
    replaces: "run",
    state: retryState,
    ready: true,
    satisfied: submitted,
    evidence: {
      proposal_id: proposal,
      generation_id: retryGeneration,
      action: "agent_run.retry",
      status: pending ? "pending" : "confirmed",
      retry_of_run_id: originalRun,
      ...(pending ? {} : { result_id: retryRun }),
      route: `/tasks/${task}`,
      ...(submitted ? { submission_status: "pending_review" as const } : {}),
    },
  });
  Object.assign(plan.steps[3], {
    depends_on: ["retry"],
    ready: submitted,
  });
  return plan;
}
function setup(
  id = session,
  onContinue?: (prompt: string, scopes: string[]) => void,
  focusRequest = 0,
  onContinueActions?: (request: AiActionContinuation) => void,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AiWorkPlan
          sessionId={id}
          onContinue={onContinue}
          focusRequest={focusRequest}
          onContinueActions={onContinueActions}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getAiWorkPlanContinuation).mockImplementation(
    async (id, _version, signal) => ({
      plan: (await getAiWorkPlan(id, signal))!,
      ready: true,
      reason: "ready",
    }),
  );
  useAiChatStore.setState({ workPlanVersions: {} });
});
afterEach(cleanup);
function stuckPlan(): Plan {
  const plan = fixture();
  plan.steps[1].state = "rejected";
  plan.steps[1].evidence!.status = "rejected";
  delete plan.steps[1].evidence!.result_id;
  return plan;
}
describe("AiWorkPlan closure", () => {
  it("closes a stuck plan only after explicit acknowledgement", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(stuckPlan());
    vi.mocked(closeAiWorkPlan).mockResolvedValue({
      ...stuckPlan(),
      closed_at: "2026-09-19T12:05:00Z",
    });
    setup(session, vi.fn(), 1);
    fireEvent.click(await screen.findByRole("button", { name: "关闭计划" }));
    const confirm = screen.getByRole("group", { name: "确认关闭计划" });
    expect(confirm).toHaveTextContent(/不会拒绝待确认操作、取消执行、撤销结果/);
    const submit = within(confirm).getByRole("button", {
      name: "确认关闭计划",
    });
    expect(submit).toBeDisabled();
    fireEvent.click(within(confirm).getByRole("checkbox"));
    fireEvent.click(submit);
    await waitFor(() =>
      expect(closeAiWorkPlan).toHaveBeenCalledExactlyOnceWith(session, 2),
    );
    expect(
      await screen.findByText(/关闭此计划。它不再出现在续办队列/),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭计划" })).toBeNull();
  });

  it("keeps the plan open and explains a server refusal", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(stuckPlan());
    vi.mocked(closeAiWorkPlan).mockRejectedValue(
      new ApiError("blocked", { code: "AI_PLAN_CLOSE_BLOCKED" }),
    );
    setup(session, vi.fn(), 1);
    fireEvent.click(await screen.findByRole("button", { name: "关闭计划" }));
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "确认关闭计划" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/暂不能关闭/);
    expect(screen.getByRole("button", { name: "继续此计划" })).toBeVisible();
    expect(screen.queryByText(/已由你关闭/)).toBeNull();
  });

  it.each(["output_pending", "retained", "pending", "running", "streaming"])(
    "does not offer closure while a step is %s",
    async (state) => {
      const plan = stuckPlan();
      plan.steps[1].state = state;
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      setup(session, vi.fn(), 1);
      await screen.findByText(/执行计划 · 完成报告/);
      expect(screen.queryByRole("button", { name: "关闭计划" })).toBeNull();
      expect(closeAiWorkPlan).not.toHaveBeenCalled();
    },
  );

  it("renders a closed plan without continuation controls", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue({
      ...stuckPlan(),
      closed_at: "2026-09-19T12:05:00Z",
    });
    setup(session, vi.fn(), 1);
    expect(await screen.findByText(/· 已由你关闭/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭计划" })).toBeNull();
    expect(screen.getByText("已拒绝 · 未执行")).toBeVisible();
  });
});
describe("AiWorkPlan", () => {
  it.each(["pending", "submitted"])(
    "labels current-facts restart separately and retains the original receipt (%s)",
    async (state) => {
      const plan = retryPlan("retained", state);
      const step = plan.steps[2];
      step.title = "按当前事实生成报告";
      step.evidence!.action = "agent_run.start";
      step.evidence!.restart_of_run_id = originalRun;
      delete step.evidence!.retry_of_run_id;
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      const continuation = vi.fn();
      setup(session, continuation, 1);
      expect(
        await screen.findByText("按当前事实重新执行接续“生成报告”"),
      ).toBeVisible();
      expect(
        screen.getByText(
          "已由“按当前事实生成报告”按当前事实重新执行接续 · 保留原状态与回执",
        ),
      ).toBeVisible();
      expect(screen.getByText("结果已保留 · 未提交")).toBeVisible();
      expect(continuation).not.toHaveBeenCalled();
      expect(getAiWorkPlanContinuation).not.toHaveBeenCalled();
    },
  );
  it.each([
    ["failed", "执行失败"],
    ["cancelled", "执行已取消"],
    ["interrupted", "执行已中断"],
  ])(
    "preserves the original %s receipt and both approval entries for a retry continuation",
    async (oldState, label) => {
      const plan = retryPlan(oldState);
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      const next = vi.fn();
      setup(session, next, 1);
      await screen.findByText(/执行计划 · 完成报告/);
      const old = screen.getByText("生成报告").closest("li")!;
      const retry = screen.getByText("重试生成报告").closest("li")!;
      expect(within(old).getByText(label)).toBeVisible();
      expect(
        within(old).getByText("已由“重试生成报告”重试接续 · 保留原状态与回执"),
      ).toBeVisible();
      expect(within(retry).getByText("重试接续“生成报告”")).toBeVisible();
      expect(within(retry).getByText("等待确认 · 未执行")).toBeVisible();
      fireEvent.click(
        within(old).getByRole("button", { name: "查看该轮操作建议" }),
      );
      expect(screen.getByText(`确认卡 ${generation}`)).toBeVisible();
      fireEvent.click(
        within(retry).getByRole("button", { name: "查看该轮操作建议" }),
      );
      expect(screen.getByText(`确认卡 ${retryGeneration}`)).toBeVisible();
      expect(screen.queryByText(`确认卡 ${generation}`)).toBeNull();
      expect(within(old).getByText(label)).toBeVisible();
      expect(plan.steps[1].evidence).toMatchObject({
        action: "agent_run.start",
        status: "confirmed",
        result_id: originalRun,
      });
      expect(next).not.toHaveBeenCalled();
      expect(getAiWorkPlanContinuation).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["pending", "等待确认 · 未执行"],
    ["queued", "执行排队中"],
    ["running", "执行中"],
    ["output_pending", "结果待登记 · 勿重跑模型"],
  ])(
    "does not count or continue an unfinished %s retry",
    async (state, label) => {
      vi.mocked(getAiWorkPlan).mockResolvedValue(retryPlan("failed", state));
      const next = vi.fn();
      setup(session, next, 1);
      await screen.findByText(/执行计划 · 完成报告/);
      expect(screen.getByText("重试接续“生成报告”")).toBeVisible();
      expect(screen.getByText(label)).toBeVisible();
      expect(screen.getByText("执行失败")).toBeVisible();
      expect(
        screen.getByText("已满足 1/3 · 历史替代 1 步（保留原回执）"),
      ).toBeVisible();
      expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
      expect(next).not.toHaveBeenCalled();
      expect(getAiWorkPlanContinuation).not.toHaveBeenCalled();
    },
  );

  it("counts a submitted retry once and prepares only the existing explicit continuation scopes", async () => {
    const plan = retryPlan("failed", "submitted");
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    const next = vi.fn();
    setup(session, next, 1);
    const button = await screen.findByRole("button", { name: "继续此计划" });
    expect(screen.getByText("重试接续“生成报告”")).toBeVisible();
    expect(screen.getByText("执行失败")).toBeVisible();
    expect(screen.getByText("产出已提交 · 非任务完成")).toBeVisible();
    expect(screen.getByText("验收状态：待人工验收")).toBeVisible();
    expect(
      screen.getByText("已满足 2/3 · 历史替代 1 步（保留原回执）"),
    ).toBeVisible();
    expect(next).not.toHaveBeenCalled();
    fireEvent.click(button);
    await waitFor(() =>
      expect(next).toHaveBeenCalledExactlyOnceWith(expect.any(String), [
        "work",
        "outputs",
        "actions",
        "agent_execution",
      ]),
    );
    expect(getAiWorkPlanContinuation).toHaveBeenCalledExactlyOnceWith(
      session,
      plan.version,
      expect.any(AbortSignal),
    );
    expect(next.mock.calls[0][1]).not.toContain("agent_files");
    expect(screen.getByText(/不会自动发送或执行操作/)).toBeVisible();
  });

  it("does not offer completed retry work again or erase the failed original", async () => {
    const plan = retryPlan("failed", "submitted");
    Object.assign(plan.steps[3], {
      report: "reported_done",
      state: "reported_done",
      satisfied: true,
    });
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    setup(session, vi.fn(), 1);
    await screen.findByText("重试接续“生成报告”");
    expect(
      screen.getByText("已满足 3/3 · 历史替代 1 步（保留原回执）"),
    ).toBeVisible();
    expect(screen.getByText("执行失败")).toBeVisible();
    expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
  });

  it.each(["rejected", "expired"])(
    "keeps the generic replacement wording for an original %s proposal",
    async (state) => {
      const plan = replacedPlan();
      plan.steps[1].state = state;
      plan.steps[1].evidence!.status = state;
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      setup(session, vi.fn(), 1);
      await screen.findByText("替代“生成报告”");
      expect(
        screen.getByText("已由“登记替代任务”替代 · 保留原状态与回执"),
      ).toBeVisible();
      expect(screen.queryByText(/重试接续/)).toBeNull();
    },
  );

  it.each(["missing_lineage", "not_retry"])(
    "does not infer a retry continuation from %s evidence",
    async (caseName) => {
      const plan = replacedPlan();
      plan.steps[2].evidence!.action = "agent_run.retry";
      if (caseName === "not_retry") {
        plan.steps[2].evidence!.action = "task.create";
        plan.steps[2].evidence!.retry_of_run_id = originalRun;
      }
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      setup(session, undefined, 1);
      await screen.findByText("替代“生成报告”");
      expect(
        screen.getByText("已由“登记替代任务”替代 · 保留原状态与回执"),
      ).toBeVisible();
      expect(screen.queryByText(/重试接续/)).toBeNull();
    },
  );

  it("does not prepare continuation directly from a cached plan", async () => {
    const plan = fixture();
    plan.steps[1].state = "submitted";
    plan.steps[1].satisfied = true;
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    let resolveCheck!: (value: AiWorkPlanContinuation) => void;
    vi.mocked(getAiWorkPlanContinuation).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCheck = resolve;
        }),
    );
    const next = vi.fn();
    setup(session, next, 1);
    fireEvent.click(await screen.findByRole("button", { name: "继续此计划" }));
    expect(next).not.toHaveBeenCalled();
    expect(getAiWorkPlanContinuation).toHaveBeenCalledExactlyOnceWith(
      session,
      plan.version,
      expect.any(AbortSignal),
    );
    expect(
      screen.getByRole("button", { name: "正在核对接续条件…" }),
    ).toBeDisabled();
    await act(async () =>
      resolveCheck({
        plan,
        ready: false,
        reason: "pending_approval",
        blocking_generation_id: generation,
        blockers: [
          {
            reason: "pending_approval",
            total: 2,
            generation_ids: [generation],
          },
          {
            reason: "run_active",
            total: 1,
            generation_ids: [generation],
          },
        ],
      }),
    );
    expect(screen.getByText(/整轮建议中仍有待确认操作/)).toBeVisible();
    expect(
      screen.getByText("阻塞摘要：待确认 2 · Agent 执行中 1"),
    ).toBeVisible();
    expect(next).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "查看阻塞这一轮的操作建议" }),
    );
    expect(screen.getByText(`确认卡 ${generation}`)).toBeVisible();
  });

  it("opens every blocking receipt group from one continuation check", async () => {
    const plan = retryPlan("failed", "submitted");
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    vi.mocked(getAiWorkPlanContinuation).mockResolvedValue({
      plan,
      ready: false,
      reason: "pending_approval",
      blocking_generation_id: generation,
      blockers: [
        {
          reason: "pending_approval",
          total: 2,
          generation_ids: [generation, retryGeneration],
        },
        {
          reason: "output_pending",
          total: 1,
          generation_ids: [retryGeneration],
        },
      ],
    });
    setup(session, vi.fn(), 1);
    fireEvent.click(await screen.findByRole("button", { name: "继续此计划" }));
    expect(
      await screen.findByText("阻塞摘要：待确认 2 · 结果待登记 1"),
    ).toBeVisible();
    const groups = screen.getAllByRole("button", {
      name: /查看阻塞回执组/,
    });
    expect(groups).toHaveLength(2);
    fireEvent.click(groups[0]);
    expect(screen.getByText(`确认卡 ${generation}`)).toBeVisible();
    fireEvent.click(groups[1]);
    expect(screen.getByText(`确认卡 ${retryGeneration}`)).toBeVisible();
    expect(screen.queryByText(`确认卡 ${generation}`)).toBeNull();
  });

  it("shows both replacement titles and original refusal evidence without offering completed work again", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(replacedPlan());
    const onContinue = vi.fn();
    setup(session, onContinue, 1);
    await screen.findByText(/执行计划 · 完成报告/);
    expect(
      screen.getByText("已由“登记替代任务”替代 · 保留原状态与回执"),
    ).toBeVisible();
    expect(screen.getByText("替代“生成报告”")).toBeVisible();
    expect(screen.getByText("已拒绝 · 未执行")).toBeVisible();
    expect(
      screen.getByText("已满足 3/3 · 历史替代 1 步（保留原回执）"),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
    const old = screen.getByText("生成报告").closest("li")!;
    fireEvent.click(
      within(old).getByRole("button", { name: "查看该轮操作建议" }),
    );
    expect(screen.getByText(`确认卡 ${generation}`)).toBeVisible();
    expect(onContinue).not.toHaveBeenCalled();
  });

  it.each(["agent_run.start", "financial_entry.create"])(
    "continues an unfinished replacement without recommending obsolete %s permissions",
    async (oldAction) => {
      const plan = replacedPlan();
      plan.steps[1].evidence!.action = oldAction;
      Object.assign(plan.steps[2], { state: "not_proposed", satisfied: false });
      delete plan.steps[2].evidence;
      delete plan.steps[2].proposal_id;
      Object.assign(plan.steps[3], {
        report: "pending",
        state: "pending",
        ready: false,
        satisfied: false,
      });
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      const next = vi.fn();
      setup(session, next, 1);
      fireEvent.click(
        await screen.findByRole("button", { name: "继续此计划" }),
      );
      await waitFor(() =>
        expect(next).toHaveBeenCalledExactlyOnceWith(expect.any(String), [
          "work",
          "actions",
        ]),
      );
      expect(
        screen.getByText("已满足 1/3 · 历史替代 1 步（保留原回执）"),
      ).toBeVisible();
    },
  );

  it("does not apply a later replacement relationship to a historical revision", async () => {
    vi.mocked(getAiWorkPlan).mockImplementation(
      async (_id, _signal, version) => {
        if (version === undefined) return replacedPlan();
        const old = fixture();
        old.version = 1;
        old.steps[1].state = "rejected";
        old.steps[1].evidence!.status = "rejected";
        delete old.steps[1].evidence!.result_id;
        return old;
      },
    );
    setup(session, vi.fn(), 1);
    await screen.findByText("替代“生成报告”");
    fireEvent.click(screen.getByRole("button", { name: "上一版" }));
    await screen.findByText("版本 1");
    expect(screen.queryByText(/历史替代/)).toBeNull();
    expect(screen.queryByText("登记替代任务")).toBeNull();
    expect(screen.getByText("已拒绝 · 未执行")).toBeVisible();
    expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
  });
  it("routes the original approval continuation to the owning chat input", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(fixture());
    const next = vi.fn();
    setup(session, undefined, 0, next);
    fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));
    fireEvent.click(screen.getByRole("button", { name: "查看该轮操作建议" }));
    fireEvent.click(screen.getByRole("button", { name: "模拟审批接续" }));
    expect(next).toHaveBeenCalledExactlyOnceWith({
      generationId: generation,
      prompt: "固定继续提示",
      scopes: ["work", "actions"],
    });
  });
  it("expands the saved plan when entered from the continuation queue", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(fixture());
    setup(session, undefined, 1);
    expect(
      (await screen.findByText(/执行计划 · 完成报告/)).closest("details"),
    ).toHaveAttribute("open");
    expect(
      screen.getByRole("button", { name: "查看该轮操作建议" }),
    ).toBeVisible();
  });
  it("returns from a historical revision to the latest plan on a new continuation request", async () => {
    vi.mocked(getAiWorkPlan).mockImplementation(
      async (_id, _signal, version) => ({
        ...fixture(),
        version: version ?? 2,
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    const element = (focusRequest: number) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <AiWorkPlan sessionId={session} focusRequest={focusRequest} />
        </MemoryRouter>
      </QueryClientProvider>
    );
    const view = render(element(1));
    await screen.findByText("版本 2");
    fireEvent.click(screen.getByRole("button", { name: "上一版" }));
    await screen.findByText("版本 1");
    view.rerender(element(2));
    await screen.findByText("版本 2");
    expect(screen.queryByRole("button", { name: "返回最新计划" })).toBeNull();
  });
  it("shows truthful receipt states and opens existing approvals, not automatic execution", async () => {
    vi.mocked(getAiWorkPlan).mockResolvedValue(fixture());
    setup();
    fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));
    expect(screen.getByText("分析完成（模型自报）")).toBeInTheDocument();
    expect(screen.getByText("结果待登记 · 勿重跑模型")).toBeInTheDocument();
    expect(screen.getByText(/前置步骤尚未满足/)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "查看工作台记录" }),
    ).toHaveAttribute("href", `/tasks/${task}?return_session=${session}`);
    fireEvent.click(screen.getByRole("button", { name: "查看该轮操作建议" }));
    expect(screen.getByText(`确认卡 ${generation}`)).toBeInTheDocument();
  });
  it.each([
    [
      "running",
      `/tasks/${task}?agent_run=${originalRun}`,
      "查看 Agent 执行",
      `/tasks/${task}?agent_run=${originalRun}&return_session=${session}`,
    ],
    [
      "submitted",
      `/tasks/${task}/submissions/${originalRun}`,
      "查看待验收提交",
      `/tasks/${task}/submissions/${originalRun}?return_session=${session}`,
    ],
  ])(
    "labels an Agent %s receipt by its exact destination",
    async (state, route, label, href) => {
      const plan = fixture();
      plan.steps[1].state = state;
      plan.steps[1].evidence!.result_id = originalRun;
      plan.steps[1].evidence!.route = route;
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      setup();

      fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));

      expect(screen.getByRole("link", { name: label })).toHaveAttribute(
        "href",
        href,
      );
      expect(
        screen.queryByRole("link", { name: "查看工作台记录" }),
      ).not.toBeInTheDocument();
    },
  );
  it("loads prior revision and can return to latest", async () => {
    vi.mocked(getAiWorkPlan).mockImplementation(async (_id, _signal, v) => ({
      ...fixture(),
      version: v ?? 2,
    }));
    setup();
    fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));
    fireEvent.click(screen.getByRole("button", { name: "上一版" }));
    await waitFor(() => expect(screen.getByText("版本 1")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "上一版" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "返回最新计划" }));
    await waitFor(() => expect(screen.getByText("版本 2")).toBeInTheDocument());
  });
  it("re-reads the latest plan immediately after the stream metadata receipt", async () => {
    vi.mocked(getAiWorkPlan).mockImplementation(async () => ({
      ...fixture(),
      version: useAiChatStore.getState().workPlanVersions[session] || 1,
    }));
    setup();
    expect(await screen.findByText("版本 1")).toBeInTheDocument();
    useAiChatStore.setState({ workPlanVersions: { [session]: 2 } });
    await waitFor(() => expect(screen.getByText("版本 2")).toBeInTheDocument());
  });
  it("keeps failures explicit and allows read-only recovery", async () => {
    vi.mocked(getAiWorkPlan).mockRejectedValue(new Error("offline"));
    setup();
    expect(await screen.findByText(/计划暂不可读取/)).toBeInTheDocument();
    vi.mocked(getAiWorkPlan).mockResolvedValue(null);
    fireEvent.click(screen.getByRole("button", { name: "重试读取计划" }));
    await waitFor(() =>
      expect(screen.queryByText(/计划暂不可读取/)).toBeNull(),
    );
  });
  it("does not request a plan for an unsaved chat", () => {
    setup("");
    expect(getAiWorkPlan).not.toHaveBeenCalled();
  });
  it("prepares an explicit continuation without granting or executing work", async () => {
    const plan = fixture();
    plan.steps[1].state = "failed";
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    const onContinue = vi.fn();
    setup(session, onContinue);
    fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));
    fireEvent.click(screen.getByRole("button", { name: "继续此计划" }));

    await waitFor(() => expect(onContinue).toHaveBeenCalledTimes(1));
    expect(onContinue.mock.calls[0][0]).toContain("读取最新计划");
    expect(onContinue.mock.calls[0][0]).toContain("创建新的确认建议");
    expect(onContinue.mock.calls[0][1]).toEqual([
      "work",
      "outputs",
      "actions",
      "agent_execution",
    ]);
    expect(screen.getByText(/打开单次权限确认/)).toBeInTheDocument();
  });
  it("does not offer continuation while approval or output recovery is pending", async () => {
    for (const state of [
      "pending",
      "child_pending_approval",
      "output_pending",
    ] as const) {
      const plan = fixture();
      plan.steps[1].state = state;
      if (state === "child_pending_approval") {
        plan.steps[1].evidence!.action = "agent_delegate.spawn";
        plan.steps[1].evidence!.route = `/ai?session=${task}`;
      }
      vi.mocked(getAiWorkPlan).mockResolvedValueOnce(plan);
      const onContinue = vi.fn();
      setup(session, onContinue);
      fireEvent.click(await screen.findByText(/执行计划 · 完成报告/));
      expect(screen.queryByRole("button", { name: "继续此计划" })).toBeNull();
      if (state === "child_pending_approval") {
        expect(screen.getByText("子会话待人工核对")).toBeInTheDocument();
      }
      cleanup();
    }
  });

  it.each([
    "generation_active",
    "run_active",
    "output_pending",
    "evidence_unavailable",
    "generation_unavailable",
  ] as const)(
    "does not prepare when the full-group check reports %s",
    async (reason) => {
      const plan = fixture();
      plan.steps[1].state = "failed";
      vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
      vi.mocked(getAiWorkPlanContinuation).mockResolvedValue({
        plan,
        ready: false,
        reason,
      });
      const next = vi.fn();
      setup(session, next, 1);
      fireEvent.click(
        await screen.findByRole("button", { name: "继续此计划" }),
      );
      await waitFor(() =>
        expect(
          screen.getByRole("button", { name: "继续此计划" }),
        ).toBeEnabled(),
      );
      expect(next).not.toHaveBeenCalled();
    },
  );

  it("requires another click after refreshing a changed plan", async () => {
    const plan = fixture();
    plan.steps[1].state = "failed";
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    const next = vi.fn();
    setup(session, next, 1);
    const button = await screen.findByRole("button", { name: "继续此计划" });
    const latest = { ...plan, version: 3 };
    vi.mocked(getAiWorkPlan).mockResolvedValue(latest);
    vi.mocked(getAiWorkPlanContinuation).mockRejectedValueOnce(
      new ApiError("changed", { code: "AI_PLAN_CONTINUATION_CHANGED" }),
    );
    fireEvent.click(button);
    await screen.findByText("版本 3");
    expect(
      screen.getByText(/计划已更新，请核对最新计划后再次继续/),
    ).toBeVisible();
    expect(next).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "继续此计划" }));
    await waitFor(() => expect(next).toHaveBeenCalledTimes(1));
    expect(
      vi.mocked(getAiWorkPlanContinuation).mock.calls.map((call) => call[1]),
    ).toEqual([2, 3]);
  });

  it.each([
    "NETWORK_ERROR",
    "AI_PLAN_CONTINUATION_UNAVAILABLE",
    "INVALID_RESPONSE",
  ])("fails closed on %s without using cached readiness", async (code) => {
    const plan = fixture();
    plan.steps[1].state = "failed";
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    vi.mocked(getAiWorkPlanContinuation).mockRejectedValue(
      new ApiError("failed", { code }),
    );
    const next = vi.fn();
    setup(session, next, 1);
    fireEvent.click(await screen.findByRole("button", { name: "继续此计划" }));
    await screen.findByText(/未准备续办|暂不能准备续办/);
    expect(next).not.toHaveBeenCalled();
  });

  it("aborts and discards a check after unmount", async () => {
    const plan = fixture();
    plan.steps[1].state = "failed";
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    let resolveCheck!: (value: AiWorkPlanContinuation) => void;
    vi.mocked(getAiWorkPlanContinuation).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveCheck = resolve;
        }),
    );
    const next = vi.fn();
    const view = setup(session, next, 1);
    fireEvent.click(await screen.findByRole("button", { name: "继续此计划" }));
    const signal = vi.mocked(getAiWorkPlanContinuation).mock.calls[0][2]!;
    view.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => resolveCheck({ plan, ready: true, reason: "ready" }));
    expect(next).not.toHaveBeenCalled();
  });

  it("discards a late check when its owner changes away and back", async () => {
    const plan = fixture();
    plan.steps[1].state = "failed";
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    let resolveCheck!: (value: AiWorkPlanContinuation) => void;
    vi.mocked(getAiWorkPlanContinuation).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveCheck = resolve;
        }),
    );
    const next = vi.fn();
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    const element = (enabled: boolean) => (
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <AiWorkPlan
            sessionId={session}
            focusRequest={1}
            onContinue={enabled ? next : undefined}
          />
        </MemoryRouter>
      </QueryClientProvider>
    );
    const view = render(element(true));
    fireEvent.click(await screen.findByRole("button", { name: "继续此计划" }));
    const signal = vi.mocked(getAiWorkPlanContinuation).mock.calls[0][2]!;
    view.rerender(element(false));
    view.rerender(element(true));
    expect(signal.aborted).toBe(true);
    await act(async () => resolveCheck({ plan, ready: true, reason: "ready" }));
    expect(next).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "继续此计划" }));
    await waitFor(() => expect(next).toHaveBeenCalledTimes(1));
  });
});
