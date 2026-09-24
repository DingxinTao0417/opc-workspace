import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { AiActionProposal } from "../api/aiWorkspaceActions";
import { AiWorkspaceActions } from "./AiWorkspaceActions";
import { useUiStore } from "../store/ui";
import type { AiActionContinuation } from "../lib/aiActionContinuation";

const state = vi.hoisted(() => ({
  load: vi.fn(),
  decide: vi.fn(),
  batchDecide: vi.fn(),
  invalidate: vi.fn(async () => {}),
  invalidateOutputs: vi.fn(async () => {}),
  invalidateProjects: vi.fn(async () => {}),
  invalidateRoadmap: vi.fn(async () => {}),
  invalidateContent: vi.fn(async () => {}),
  invalidateInbox: vi.fn(async () => {}),
  invalidateInboxReadAll: vi.fn(async () => {}),
  invalidateReminders: vi.fn(async () => {}),
  invalidateFocus: vi.fn(async () => {}),
  invalidateClientFollowups: vi.fn(async () => {}),
  invalidateClientActivities: vi.fn(async () => {}),
  invalidateClients: vi.fn(async () => {}),
  invalidateClientContacts: vi.fn(async () => {}),
  invalidatePeople: vi.fn(async () => {}),
  invalidateAutomations: vi.fn(async () => {}),
  invalidateFinance: vi.fn(async () => {}),
  invalidateInvoices: vi.fn(async () => {}),
  retryAgentRunDelivery: vi.fn(),
  refetchAgentRun: vi.fn(),
  resetAgentRunDeliveryRetry: vi.fn(),
}));
vi.mock("../api/aiWorkspaceActions", async () => ({
  ...(await vi.importActual("../api/aiWorkspaceActions")),
  getAiWorkspaceActions: state.load,
  confirmAiAgentDelegationBatch: state.batchDecide,
  decideAiWorkspaceAction: state.decide,
  decideAiTaskDelete: state.decide,
  decideAiProjectDelete: state.decide,
  decideAiClientDelete: state.decide,
  decideAiContentItemDelete: state.decide,
  decideAiContentPublished: state.decide,
  decideAiRoadmapMilestoneDelete: state.decide,
  decideAiTagDelete: state.decide,
  decideAiKnowledgeAction: state.decide,
  decideAiTaskViewAction: state.decide,
  decideAiFinanceExport: state.decide,
  decideAiTaskOutput: state.decide,
  decideAiProjectNote: state.decide,
  decideAiAgentDelegation: state.decide,
  decideAiAgentFollowup: state.decide,
}));
vi.mock("../api/hooks", () => ({
  invalidateTaskAggregates: state.invalidate,
  invalidateTaskOutputActionFacts: state.invalidateOutputs,
  invalidateProjectActionFacts: state.invalidateProjects,
  invalidateRoadmapActionFacts: state.invalidateRoadmap,
  invalidateContentItemActionFacts: state.invalidateContent,
  invalidateProjectNoteActionFacts: state.invalidateProjects,
  invalidateInboxActionFacts: state.invalidateInbox,
  invalidateInboxReadAllFacts: state.invalidateInboxReadAll,
  invalidateReminderActionFacts: state.invalidateReminders,
  invalidateFocusActionFacts: state.invalidateFocus,
  invalidateClientFollowupActionFacts: state.invalidateClientFollowups,
  invalidateClientActivityActionFacts: state.invalidateClientActivities,
  invalidateClientActionFacts: state.invalidateClients,
  invalidateClientContactActionFacts: state.invalidateClientContacts,
  invalidatePersonActionFacts: state.invalidatePeople,
  invalidateAutomationActionFacts: state.invalidateAutomations,
  invalidateFinancialActionFacts: state.invalidateFinance,
  invalidateInvoiceActionFacts: state.invalidateInvoices,
  useAgentRunQuery: () => ({
    data: undefined,
    isError: false,
    isPending: false,
    isFetching: false,
    refetch: state.refetchAgentRun,
  }),
  useRetryAgentRunOutputDelivery: () => ({
    mutate: state.retryAgentRunDelivery,
    isPending: false,
    isError: false,
    error: null,
    reset: state.resetAgentRunDeliveryRetry,
  }),
}));
const proposal: AiActionProposal = {
  id: "00000000-0000-4000-8000-000000000001",
  generation_id: "00000000-0000-4000-8000-000000000002",
  fingerprint: "a".repeat(64),
  action: {
    action: "task.update",
    task_id: "00000000-0000-4000-8000-000000000003",
    expected_version: 2,
    changes: { planned_date: "2026-09-20" },
  },
  preview: {
    label: "准备发布",
    before: { planned_date: null },
    after: { planned_date: "2026-09-20" },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "/tasks/00000000-0000-4000-8000-000000000003",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: null,
};
beforeEach(() => {
  vi.clearAllMocks();
  state.load.mockResolvedValue([proposal]);
  state.decide.mockReset();
  state.batchDecide.mockReset();
});
afterEach(cleanup);

function policyProposal(before = "none", after = "manual"): AiActionProposal {
  return {
    ...proposal,
    action: { ...proposal.action, changes: { review_policy: after } },
    preview: {
      label: proposal.preview.label,
      before: { review_policy: before, status: "todo" },
      after: {
        review_policy: after,
        status: "todo",
        subtask_total: 0,
        subtask_completed: 0,
        subtask_cancelled: 0,
        parent_rollup_gates_ready: false,
        will_request_parent_review: false,
      },
    },
  };
}

describe("child Agent delegation approval", () => {
  it("shows the complete instruction and requires separate human consent", async () => {
    const childSession = "00000000-0000-4000-8000-000000000031";
    const item: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_delegate.spawn",
        changes: {
          task_name: "fact_check",
          message: "Verify current task facts and report evidence only.",
          scopes: ["work", "outputs", "knowledge"],
        },
      },
      preview: {
        label: "fact_check",
        before: {},
        after: {},
        agent_delegation: {
          task_name: "fact_check",
          message: "Verify current task facts and report evidence only.",
          scopes: ["work", "outputs", "knowledge"],
          knowledge_sources: [
            {
              source_id: "00000000-0000-4000-8000-000000000034",
              expected_source_version: 3,
            },
          ],
          parent_session_id: "00000000-0000-4000-8000-000000000032",
          parent_generation_id: proposal.generation_id,
          child_session_id: childSession,
          provider_id: "00000000-0000-4000-8000-000000000033",
          provider_version: 1,
          provider_config_version: 1,
          provider_name: "Local",
          model: "test-model",
          leaves_device: false,
          depth: 1,
          max_depth: 1,
          sibling_index: 1,
          max_children: 4,
        },
      },
      route: "",
    };
    const confirmed: AiActionProposal = {
      ...item,
      status: "confirmed",
      can_confirm: false,
      result_id: childSession,
      result_version: 1,
      route: `/ai?session=${childSession}`,
      agent_delegation_result: {
        session_id: childSession,
        generation_id: item.id,
        status: "queued",
        error_code: null,
        pending_approvals: 2,
      },
    };
    state.decide
      .mockRejectedValueOnce(
        new ApiError("queue full", {
          code: "AI_DELEGATION_QUEUE_FULL",
          status: 429,
        }),
      )
      .mockResolvedValueOnce(confirmed);
    mountContinuation([item]);
    expect(
      await screen.findByText(
        "Verify current task facts and report evidence only.",
      ),
    ).toBeVisible();
    expect(screen.getByText("委派的知识来源")).toBeVisible();
    expect(
      screen.getByText(/00000000-0000-4000-8000-000000000034 · 版本 3/),
    ).toBeVisible();
    expect(screen.getByText(/子会话不能继续创建子智能体/)).toBeVisible();
    const confirm = screen.getByRole("button", { name: "确认委派子智能体" });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我已核对完整子任务指令/ }),
    );
    expect(confirm).toBeEnabled();
    state.load.mockResolvedValue([item]);
    fireEvent.click(confirm);
    expect(
      await screen.findByText(/智能体任务队列已满，本次确认未生效/),
    ).toBeVisible();
    expect(
      screen.getByRole("checkbox", { name: /我已核对完整子任务指令/ }),
    ).toBeChecked();
    await waitFor(() => expect(confirm).toBeEnabled());
    state.load.mockResolvedValue([confirmed]);
    fireEvent.click(confirm);
    await waitFor(() => expect(state.decide).toHaveBeenCalledTimes(2));
    expect(state.decide).toHaveBeenLastCalledWith(item, "confirm", true);
    expect(await screen.findByText(/子会话还有 2 项待人工核对/)).toBeVisible();
  });

  it("requires an explicit review of an exact selected child batch", async () => {
    const parentSessionId = "00000000-0000-4000-8000-000000000032";
    const providerId = "00000000-0000-4000-8000-000000000033";
    const makeDelegation = (
      index: number,
      taskName: string,
      message: string,
    ): AiActionProposal => {
      const childSessionId = `00000000-0000-4000-8000-00000000003${index}`;
      return {
        ...proposal,
        id: `00000000-0000-4000-8000-00000000001${index}`,
        fingerprint: String.fromCharCode(96 + index).repeat(64),
        action: {
          action: "agent_delegate.spawn",
          changes: { task_name: taskName, message, scopes: ["work"] },
        },
        preview: {
          label: taskName,
          before: {},
          after: {},
          agent_delegation: {
            task_name: taskName,
            message,
            scopes: ["work"],
            parent_session_id: parentSessionId,
            parent_generation_id: proposal.generation_id,
            child_session_id: childSessionId,
            provider_id: providerId,
            provider_version: 1,
            provider_config_version: 1,
            provider_name: "Local",
            model: "test-model",
            leaves_device: false,
            depth: 1,
            max_depth: 1,
            sibling_index: index,
            max_children: 4,
          },
        },
        status: "pending",
        can_confirm: true,
        result_id: null,
        result_version: null,
        route: "",
        decided_at: null,
      };
    };
    const first = makeDelegation(
      1,
      "first_child",
      "Review the first workstream and return verified findings.",
    );
    const second = makeDelegation(
      2,
      "second_child",
      "Review the second workstream and return verified findings.",
    );
    const confirmed = (item: AiActionProposal): AiActionProposal => ({
      ...item,
      status: "confirmed",
      can_confirm: false,
      result_id: item.preview.agent_delegation?.child_session_id ?? null,
      result_version: 1,
      route: `/ai?session=${item.preview.agent_delegation?.child_session_id}`,
      decided_at: "2026-09-18T12:01:00Z",
      agent_delegation_result: {
        session_id: item.preview.agent_delegation!.child_session_id,
        generation_id: item.id,
        status: "queued",
        error_code: null,
        pending_approvals: 0,
      },
    });
    const firstConfirmed = confirmed(first);
    const secondConfirmed = confirmed(second);
    state.batchDecide.mockResolvedValue([firstConfirmed, secondConfirmed]);
    mountContinuation([first, second]);

    const openBatch = await screen.findByRole("button", {
      name: "统一审查子委派（2）",
    });
    fireEvent.click(openBatch);
    const selections = screen.getAllByRole("checkbox", {
      name: /选择此子任务/,
    });
    expect(selections).toHaveLength(2);
    expect(selections[0]).not.toBeChecked();
    expect(selections[1]).not.toBeChecked();
    fireEvent.click(selections[0]);
    fireEvent.click(selections[1]);
    expect(
      (await screen.findAllByText(first.preview.agent_delegation!.message))
        .length,
    ).toBeGreaterThan(1);
    expect(
      (await screen.findAllByText(second.preview.agent_delegation!.message))
        .length,
    ).toBeGreaterThan(1);
    const confirm = screen.getByRole("button", {
      name: "确认委派 2 个子智能体",
    });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /我已逐项核对每个所选子任务/,
      }),
    );
    expect(confirm).toBeEnabled();

    state.batchDecide.mockRejectedValueOnce(
      new ApiError("queue full", {
        code: "AI_DELEGATION_QUEUE_FULL",
        status: 429,
      }),
    );
    state.load.mockResolvedValue([first, second]);
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.batchDecide).toHaveBeenCalledExactlyOnceWith(
        proposal.generation_id,
        [first, second],
        true,
      ),
    );
    expect(
      await screen.findByText(/整批未确认：智能体任务队列已满/),
    ).toBeVisible();
    expect(
      screen.getAllByRole("checkbox", { name: /选择此子任务/ }),
    ).toHaveLength(2);
    expect(
      screen.getAllByRole("checkbox", { name: /选择此子任务/ })[0],
    ).toBeChecked();
    expect(
      screen.getAllByRole("checkbox", { name: /选择此子任务/ })[1],
    ).toBeChecked();
    await waitFor(() => expect(confirm).toBeEnabled());
    state.load.mockResolvedValue([firstConfirmed, secondConfirmed]);
    fireEvent.click(confirm);
    await waitFor(() => expect(state.batchDecide).toHaveBeenCalledTimes(2));
    expect(await screen.findByText(/已原子确认 2 个子会话/)).toBeVisible();
  });
});

describe("existing child Agent follow-up approval", () => {
  it("shows the exact child and new instruction before separate consent", async () => {
    const child = "00000000-0000-4000-8000-000000000031";
    const item: AiActionProposal = {
      ...proposal,
      action: {
        action: "agent_delegate.followup",
        changes: {
          child_session_id: child,
          message: "Recheck the revised evidence.",
          scopes: ["work", "outputs"],
        },
      },
      preview: {
        label: "fact_check",
        before: {},
        after: {},
        agent_followup: {
          target: {
            parent_session_id: "00000000-0000-4000-8000-000000000032",
            child_session_id: child,
            spawn_proposal_id: "00000000-0000-4000-8000-000000000034",
            previous_generation_id: "00000000-0000-4000-8000-000000000035",
            child_version: 2,
            provider_id: "00000000-0000-4000-8000-000000000033",
            provider_version: 1,
            provider_config_version: 2,
          },
          task_name: "fact_check",
          message: "Recheck the revised evidence.",
          scopes: ["work", "outputs"],
          provider_name: "Local",
          model: "test-model",
          leaves_device: false,
        },
      },
      route: "",
    };
    const confirmed: AiActionProposal = {
      ...item,
      status: "confirmed",
      can_confirm: false,
      result_id: child,
      result_version: 1,
      route: `/ai?session=${child}`,
      agent_followup_result: {
        session_id: child,
        generation_id: item.id,
        status: "completed",
        error_code: null,
        pending_approvals: 1,
      },
    };
    state.decide.mockResolvedValue(confirmed);
    mountContinuation([item]);
    expect(
      await screen.findByText("Recheck the revised evidence."),
    ).toBeVisible();
    expect(screen.getByText(/现有子会话：fact_check/)).toBeVisible();
    expect(screen.getByText(/上一轮生成/)).toBeVisible();
    const confirm = screen.getByRole("button", { name: "确认续交办子智能体" });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我已核对现有子会话/ }),
    );
    expect(confirm).toBeEnabled();
    state.load.mockResolvedValue([confirmed]);
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledExactlyOnceWith(
        item,
        "confirm",
        true,
      ),
    );
    expect(await screen.findByText(/子会话还有 1 项待人工核对/)).toBeVisible();
  });
});

describe("Task review policy confirmation", () => {
  it.each([
    ["none", "manual", "人工验收要求提交产出并由验收人审核"],
    [
      "manual",
      "none",
      "关闭人工验收后可按任务规则直接完成，不再要求提交产出验收",
    ],
  ])(
    "discloses the %s to %s policy change without executing it",
    async (before, after, impact) => {
      const item = policyProposal(before, after);
      mountContinuation([item]);
      await screen.findByText("验收方式");
      const row = screen.getByText("验收方式").closest("div")!;
      expect(row).toHaveTextContent(
        before === "none" ? "无需验收 → 人工验收" : "人工验收 → 无需验收",
      );
      expect(screen.getByText(new RegExp(impact))).toBeVisible();
      expect(
        screen.getByText(/不会自动验收通过、完成任务或启动 Agent 执行/),
      ).toBeVisible();
      expect(screen.queryByRole("checkbox")).toBeNull();
      expect(state.decide).not.toHaveBeenCalled();
      state.decide.mockResolvedValue({
        ...item,
        status: "confirmed",
        can_confirm: false,
      });
      fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
      await waitFor(() =>
        expect(state.decide).toHaveBeenCalledExactlyOnceWith(item, "confirm"),
      );
      await waitFor(() => expect(state.invalidate).toHaveBeenCalled());
    },
  );

  it("explains the frozen child rollup and owner review gates before requesting review", async () => {
    const item = policyProposal();
    Object.assign(item.preview.after, {
      subtask_total: 3,
      subtask_completed: 2,
      subtask_cancelled: 1,
      parent_rollup_gates_ready: true,
      will_request_parent_review: true,
      status: "waiting_review",
    });
    mountContinuation([item]);
    await screen.findByText("验收方式");
    expect(
      screen.getByText(/将自动生成子任务汇总提交，并进入待验收；仍须人工审核/),
    ).toBeVisible();
    expect(
      screen.getByText("当前直属子任务数").closest("div"),
    ).toHaveTextContent("3");
    expect(
      screen.getByText("当前已完成子任务").closest("div"),
    ).toHaveTextContent("2");
    expect(
      screen.getByText("当前已取消子任务").closest("div"),
    ).toHaveTextContent("1");
    expect(
      screen.getByText("父任务自动提请验收的责任条件").closest("div"),
    ).toHaveTextContent("已满足");
    expect(
      screen.getByText("本次自动提请父任务验收").closest("div"),
    ).toHaveTextContent("是");
    expect(state.decide).not.toHaveBeenCalled();
  });

  it("explains a locked policy without automatically retrying or forcing the change", async () => {
    const item = policyProposal("manual", "none");
    state.decide.mockRejectedValue(
      new ApiError("review_policy locked", {
        code: "TASK_REVIEW_POLICY_LOCKED",
        status: 409,
      }),
    );
    mountContinuation([item]);
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "任务已不处于待办或已有提交历史，不能再切换验收方式。请查看任务当前状态，不会自动重试或强制修改。",
    );
    await waitFor(() => expect(state.load).toHaveBeenCalledTimes(2));
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(item, "confirm");
    expect(screen.queryByText("已执行")).toBeNull();
  });

  it("does not add policy-specific warnings to existing updates", async () => {
    mountContinuation([proposal]);
    await screen.findByText("准备发布");
    expect(screen.queryByText(/切换验收方式/)).toBeNull();
    expect(screen.queryByText(/关闭人工验收后/)).toBeNull();
    expect(state.decide).not.toHaveBeenCalled();
  });
});

function mountContinuation(
  items: AiActionProposal[],
  onContinue?: (request: AiActionContinuation) => void,
  proposalId?: string,
) {
  state.load.mockResolvedValue(items);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const source = (
    next: typeof onContinue,
    sessionId = "00000000-0000-4000-8000-000000000008",
    generationId = proposal.generation_id,
  ) => (
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions
          generationId={generationId}
          sessionId={sessionId}
          proposalId={proposalId}
          onContinue={next}
        />
      </QueryClientProvider>
    </MemoryRouter>
  );
  const rendered = render(source(onContinue));
  return {
    ...rendered,
    client,
    replaceSource: (
      next: typeof onContinue,
      sessionId?: string,
      generationId?: string,
    ) => rendered.rerender(source(next, sessionId, generationId)),
  };
}

describe("explicit post-approval continuation", () => {
  const settled = {
    ...proposal,
    status: "confirmed" as const,
    can_confirm: false,
  };

  it("only prepares after a human click and a fresh complete generation read", async () => {
    const next = vi.fn();
    mountContinuation([settled], next);
    const button = await screen.findByRole("button", {
      name: "继续处理这一轮",
    });
    expect(next).not.toHaveBeenCalled();
    expect(state.decide).not.toHaveBeenCalled();
    expect(state.load).toHaveBeenCalledTimes(1);
    fireEvent.click(button);
    await waitFor(() => expect(next).toHaveBeenCalledOnce());
    expect(state.load).toHaveBeenCalledTimes(2);
    expect(next.mock.calls[0][0]).toMatchObject({
      generationId: proposal.generation_id,
      scopes: ["work", "actions"],
    });
    expect(next.mock.calls[0][0].prompt).not.toContain(proposal.preview.label);
    expect(state.decide).not.toHaveBeenCalled();
  });

  it("does not show continuation without an input-owner callback", async () => {
    mountContinuation([settled]);
    await screen.findByText(proposal.preview.label);
    expect(screen.queryByRole("button", { name: "继续处理这一轮" })).toBeNull();
  });

  it("cannot skip a pending sibling when showing one exact proposal", async () => {
    const next = vi.fn();
    mountContinuation(
      [
        settled,
        {
          ...proposal,
          id: "00000000-0000-4000-8000-000000000009",
          preview: { ...proposal.preview, label: "隐藏待确认建议" },
        },
      ],
      next,
      settled.id,
    );
    expect(
      await screen.findByText("这一轮仍有待确认操作，请先处理全部建议。"),
    ).toBeVisible();
    expect(screen.queryByText("隐藏待确认建议")).toBeNull();
    expect(screen.queryByRole("button", { name: "继续处理这一轮" })).toBeNull();
    expect(next).not.toHaveBeenCalled();
  });

  it("rechecks newly pending siblings before preparing a continuation", async () => {
    const next = vi.fn();
    mountContinuation([settled], next);
    const button = await screen.findByRole("button", {
      name: "继续处理这一轮",
    });
    state.load.mockResolvedValue([
      settled,
      { ...proposal, id: "00000000-0000-4000-8000-000000000009" },
    ]);
    fireEvent.click(button);
    await waitFor(() =>
      expect(
        screen.getByText("这一轮仍有待确认操作，请先处理全部建议。"),
      ).toBeVisible(),
    );
    expect(next).not.toHaveBeenCalled();
  });

  it("does not trust a mismatched generation returned during refresh", async () => {
    const next = vi.fn();
    mountContinuation([settled], next);
    const button = await screen.findByRole("button", {
      name: "继续处理这一轮",
    });
    state.load.mockResolvedValue([
      { ...settled, generation_id: "00000000-0000-4000-8000-000000000009" },
    ]);
    fireEvent.click(button);
    expect(
      await screen.findByText("无法核对这一轮的完整操作建议，请刷新后重试。"),
    ).toBeVisible();
    expect(next).not.toHaveBeenCalled();
  });

  it("keeps read failures visible and never prepares from cached success", async () => {
    const next = vi.fn();
    mountContinuation([settled], next);
    const button = await screen.findByRole("button", {
      name: "继续处理这一轮",
    });
    state.load.mockRejectedValue(new Error("offline"));
    fireEvent.click(button);
    await waitFor(
      () =>
        expect(screen.getByRole("alert")).toHaveTextContent("操作建议读取失败"),
      { timeout: 3000 },
    );
    expect(next).not.toHaveBeenCalled();
  });

  it("discards a late refresh after the source component is closed", async () => {
    const next = vi.fn();
    const { unmount, client } = mountContinuation([settled], next);
    const button = await screen.findByRole("button", {
      name: "继续处理这一轮",
    });
    let resolve!: (items: AiActionProposal[]) => void;
    state.load.mockImplementation(
      () =>
        new Promise<AiActionProposal[]>((done) => {
          resolve = done;
        }),
    );
    fireEvent.click(button);
    expect(state.load).toHaveBeenCalledTimes(2);
    unmount();
    const refreshed = { ...settled, status: "expired" as const };
    resolve([refreshed]);
    await waitFor(() =>
      expect(
        client.getQueryData(["ai", "actions", proposal.generation_id]),
      ).toEqual([refreshed]),
    );
    expect(next).not.toHaveBeenCalled();
  });
});

describe("explicit conflict recheck", () => {
  const rejected: AiActionProposal = {
    ...proposal,
    status: "rejected",
    can_confirm: false,
  };
  const conflict = () =>
    new ApiError("stale", { code: "VERSION_CONFLICT", status: 409 });

  async function exposeRecheck() {
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    const consent = await screen.findByRole("checkbox", {
      name: /重新发送原操作参数/,
    });
    const button = screen.getByRole("button", {
      name: "撤回旧建议并重新核验",
    });
    expect(button).toBeDisabled();
    fireEvent.click(consent);
    await waitFor(() => expect(button).toBeEnabled());
    return button;
  }

  it.each(["VERSION_CONFLICT", "AI_ACTION_PREVIEW_CHANGED"])(
    "withdraws only the selected %s snapshot before preparing an identity-only source",
    async (code) => {
      const next = vi.fn();
      state.decide.mockImplementation(async (_: unknown, decision: string) => {
        if (decision === "confirm")
          throw new ApiError("stale", { code, status: 409 });
        state.load.mockResolvedValue([rejected]);
        return rejected;
      });
      mountContinuation([proposal], next, proposal.id);
      const button = await exposeRecheck();
      expect(next).not.toHaveBeenCalled();
      expect(state.decide).toHaveBeenCalledTimes(1);
      fireEvent.click(button);
      await waitFor(() => expect(next).toHaveBeenCalledOnce());
      expect(state.decide).toHaveBeenNthCalledWith(2, proposal, "reject");
      expect(next.mock.calls[0][0]).toMatchObject({
        generationId: proposal.generation_id,
        recheckProposalId: proposal.id,
        sourceSessionId: "00000000-0000-4000-8000-000000000008",
        scopes: ["work", "actions"],
      });
      const transfer = JSON.stringify(next.mock.calls[0][0]);
      expect(transfer).not.toContain(proposal.preview.label);
      expect(transfer).not.toContain("2026-09-20");
      expect(state.load.mock.calls.length).toBeGreaterThanOrEqual(4);
    },
  );

  it("keeps a withdrawn recheck available while a hidden sibling is pending", async () => {
    const sibling = {
      ...proposal,
      id: "00000000-0000-4000-8000-000000000009",
      preview: { ...proposal.preview, label: "隐藏待确认项" },
    };
    const next = vi.fn();
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      state.load.mockResolvedValue([rejected, sibling]);
      return rejected;
    });
    mountContinuation([proposal, sibling], next, proposal.id);
    fireEvent.click(await exposeRecheck());
    await screen.findByRole("button", { name: "核对已撤回建议并继续" });
    expect(next).not.toHaveBeenCalled();
    expect(screen.queryByText("隐藏待确认项")).toBeNull();
    state.load.mockResolvedValue([
      rejected,
      { ...sibling, status: "rejected", can_confirm: false },
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "核对已撤回建议并继续" }),
    );
    await waitFor(() => expect(next).toHaveBeenCalledOnce());
    expect(state.decide).toHaveBeenCalledTimes(2);
  });

  it("recovers a lost reject response only from a fresh durable rejected fact", async () => {
    const next = vi.fn();
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      state.load.mockResolvedValue([rejected]);
      throw new ApiError("lost response", { code: "NETWORK_ERROR" });
    });
    mountContinuation([proposal], next);
    fireEvent.click(await exposeRecheck());
    await waitFor(() => expect(next).toHaveBeenCalledOnce());
    expect(state.decide).toHaveBeenCalledTimes(2);
  });

  it("keeps the recheck manual while a sibling Agent is active or awaiting delivery", async () => {
    const sibling: AiActionProposal = {
      ...proposal,
      id: "00000000-0000-4000-8000-000000000009",
      action: {
        action: "agent_run.start",
        task_id: proposal.action.task_id,
        changes: {},
      },
      status: "confirmed",
      can_confirm: false,
      result_id: "00000000-0000-4000-8000-000000000010",
      agent_run_result: {
        id: "00000000-0000-4000-8000-000000000010",
        task_id: proposal.action.task_id!,
        status: "running",
        attempt: 1,
        provider_id: "00000000-0000-4000-8000-000000000011",
        model: "private model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    const next = vi.fn();
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      state.load.mockResolvedValue([rejected, sibling]);
      return rejected;
    });
    mountContinuation([proposal, sibling], next, proposal.id);
    fireEvent.click(await exposeRecheck());
    await screen.findByRole("button", { name: "核对已撤回建议并继续" });
    expect(next).not.toHaveBeenCalled();
    for (const [status, output_delivery_status] of [
      ["running", "pending"],
      ["succeeded", "submitted"],
    ] as const) {
      state.load.mockResolvedValue([
        rejected,
        {
          ...sibling,
          agent_run_result: {
            ...sibling.agent_run_result!,
            status,
            output_delivery_status,
          },
        },
      ]);
      fireEvent.click(
        screen.getByRole("button", { name: "核对已撤回建议并继续" }),
      );
      if (output_delivery_status === "pending") {
        await waitFor(() =>
          expect(
            screen.getAllByText("这一轮仍有产出待登记，请先处理登记结果。")
              .length,
          ).toBeGreaterThan(0),
        );
        expect(next).not.toHaveBeenCalled();
      } else await waitFor(() => expect(next).toHaveBeenCalledOnce());
    }
    expect(state.decide).toHaveBeenCalledTimes(2);
    expect(JSON.stringify(next.mock.calls[0][0])).not.toContain(
      "private model",
    );
  });

  it.each(["NETWORK_ERROR", "AI_ACTION_CHANGED"])(
    "does not interpret %s as an eligible confirmation conflict",
    async (code) => {
      state.decide.mockRejectedValue(new ApiError("failure", { code }));
      const next = vi.fn();
      mountContinuation([proposal], next);
      fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
      await screen.findByRole("alert");
      expect(
        screen.queryByRole("checkbox", { name: /重新发送原操作参数/ }),
      ).toBeNull();
      expect(next).not.toHaveBeenCalled();
    },
  );

  it("does not reinterpret a failed reject as a failed confirmation", async () => {
    state.decide.mockRejectedValue(conflict());
    mountContinuation([proposal], vi.fn());
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    await screen.findByRole("alert");
    expect(
      screen.queryByRole("checkbox", { name: /重新发送原操作参数/ }),
    ).toBeNull();
  });

  it("does not offer recheck without an input-owner callback", async () => {
    state.decide.mockRejectedValue(conflict());
    mountContinuation([proposal]);
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    await screen.findByRole("alert");
    expect(
      screen.queryByRole("checkbox", { name: /重新发送原操作参数/ }),
    ).toBeNull();
  });

  it.each(["confirmed", "missing", "fingerprint", "foreign"])(
    "refuses a %s target in the pre-withdrawal read",
    async (scenario) => {
      const next = vi.fn();
      state.decide.mockRejectedValue(conflict());
      mountContinuation([proposal], next);
      const button = await exposeRecheck();
      state.load.mockResolvedValue(
        scenario === "missing"
          ? []
          : [
              {
                ...proposal,
                ...(scenario === "confirmed"
                  ? { status: "confirmed", can_confirm: false }
                  : {}),
                ...(scenario === "fingerprint"
                  ? { fingerprint: "b".repeat(64) }
                  : {}),
                ...(scenario === "foreign"
                  ? { generation_id: "00000000-0000-4000-8000-000000000099" }
                  : {}),
              },
            ],
      );
      fireEvent.click(button);
      await screen.findByText(
        scenario === "confirmed"
          ? "原建议已经执行，不能视为已撤回或重新提议，请核对真实回执。"
          : "原建议身份已变化或无法读取，请重新定位原审批。",
      );
      expect(state.decide).toHaveBeenCalledTimes(1);
      expect(next).not.toHaveBeenCalled();
    },
  );

  it.each(["pending", "confirmed"] as const)(
    "does not assume a reject succeeded when reread says %s",
    async (status) => {
      const next = vi.fn();
      state.decide.mockImplementation(async (_: unknown, decision: string) => {
        if (decision === "confirm") throw conflict();
        state.load.mockResolvedValue([
          { ...proposal, status, can_confirm: status === "pending" },
        ]);
        throw new ApiError("lost response", { code: "NETWORK_ERROR" });
      });
      mountContinuation([proposal], next);
      fireEvent.click(await exposeRecheck());
      await screen.findByText(
        status === "confirmed"
          ? "原建议已经执行，不能视为已撤回或重新提议，请核对真实回执。"
          : "尚未核实原建议已撤回，不能准备重新核验。",
      );
      expect(state.decide).toHaveBeenCalledTimes(2);
      expect(next).not.toHaveBeenCalled();
    },
  );

  it("withdraws an expired projection to durable rejected before preparing", async () => {
    const next = vi.fn();
    const expired = {
      ...proposal,
      status: "expired" as const,
      can_confirm: false,
    };
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      state.load.mockResolvedValue([rejected]);
      return rejected;
    });
    mountContinuation([proposal], next);
    const button = await exposeRecheck();
    state.load.mockResolvedValue([expired]);
    fireEvent.click(button);
    await waitFor(() => expect(next).toHaveBeenCalledOnce());
    expect(state.decide).toHaveBeenNthCalledWith(2, expired, "reject");
  });

  it("retains a recheck through a failed post-withdrawal read without trusting cache", async () => {
    const next = vi.fn();
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      state.load.mockRejectedValue(new Error("read failed"));
      return rejected;
    });
    mountContinuation([proposal], next);
    fireEvent.click(await exposeRecheck());
    await screen.findByText(
      "无法核对最新操作状态；不会据此重提或执行，请重新读取后再继续。",
      {},
      { timeout: 3000 },
    );
    expect(next).not.toHaveBeenCalled();
    expect(screen.queryByText(proposal.preview.label)).toBeNull();
    state.load.mockResolvedValue([rejected]);
    fireEvent.click(
      screen.getByRole("button", { name: "撤回旧建议并重新核验" }),
    );
    await waitFor(() => expect(next).toHaveBeenCalledOnce());
    expect(state.decide).toHaveBeenCalledTimes(2);
  });

  it.each(["callback", "callback_aba", "streaming", "session", "unmount"])(
    "discards a late pre-withdrawal read after %s changes",
    async (change) => {
      const next = vi.fn();
      const other = vi.fn();
      state.decide.mockRejectedValue(conflict());
      const mounted = mountContinuation([proposal], next);
      const button = await exposeRecheck();
      let resolve!: (rows: AiActionProposal[]) => void;
      state.load.mockImplementation(
        () =>
          new Promise<AiActionProposal[]>((done) => {
            resolve = done;
          }),
      );
      fireEvent.click(button);
      await waitFor(() => expect(resolve).toBeTypeOf("function"));
      if (change === "unmount") mounted.unmount();
      else if (change === "session")
        mounted.replaceSource(other, "00000000-0000-4000-8000-000000000099");
      else if (change === "streaming") mounted.replaceSource(undefined);
      else {
        mounted.replaceSource(other);
        if (change === "callback_aba") mounted.replaceSource(next);
      }
      const before = state.load.mock.calls.length;
      resolve([proposal]);
      await waitFor(() =>
        expect(
          mounted.client.getQueryState([
            "ai",
            "actions",
            proposal.generation_id,
          ])?.fetchStatus,
        ).toBe("idle"),
      );
      expect(state.decide).toHaveBeenCalledTimes(1);
      expect(next).not.toHaveBeenCalled();
      expect(other).not.toHaveBeenCalled();
      expect(state.load).toHaveBeenCalledTimes(before);
      if (change.startsWith("callback"))
        expect(
          screen.getByRole("checkbox", { name: /重新发送原操作参数/ }),
        ).not.toBeChecked();
    },
  );

  it("does not prepare when the callback changes while rejection is in flight", async () => {
    const next = vi.fn();
    const other = vi.fn();
    let resolve!: (row: AiActionProposal) => void;
    state.decide.mockImplementation(async (_: unknown, decision: string) => {
      if (decision === "confirm") throw conflict();
      return new Promise<AiActionProposal>((done) => {
        resolve = done;
      });
    });
    const mounted = mountContinuation([proposal], next);
    fireEvent.click(await exposeRecheck());
    await waitFor(() => expect(resolve).toBeTypeOf("function"));
    mounted.replaceSource(other);
    state.load.mockResolvedValue([rejected]);
    resolve(rejected);
    await screen.findByText("输入来源已变化，请重新勾选后核对。");
    expect(next).not.toHaveBeenCalled();
    expect(other).not.toHaveBeenCalled();
    expect(state.decide).toHaveBeenCalledTimes(2);
  });
});

it("reopens only the requested proposal and uses its original confirmation", async () => {
  state.load.mockResolvedValue([
    proposal,
    {
      ...proposal,
      id: "00000000-0000-4000-8000-000000000009",
      preview: { ...proposal.preview, label: "另一条建议" },
    },
  ]);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions
          generationId={proposal.generation_id}
          proposalId={proposal.id}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("准备发布")).toBeInTheDocument();
  expect(screen.queryByText("另一条建议")).toBeNull();
  expect(state.decide).not.toHaveBeenCalled();
  state.decide.mockResolvedValue({
    ...proposal,
    status: "confirmed",
    can_confirm: false,
  });
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(proposal, "confirm"),
  );
});

it("does not substitute a different proposal when the requested one is gone", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions
          generationId={proposal.generation_id}
          proposalId="00000000-0000-4000-8000-000000000009"
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText(/该操作建议已不存在/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "确认执行" })).toBeNull();
});

it.each(["task.submit_output", "task.review"] as const)(
  "requires personal output consent for %s",
  async (action) => {
    const output: AiActionProposal = {
      ...proposal,
      action: {
        ...proposal.action,
        action,
        changes: action === "task.review" ? { decision: "accept" } : {},
      },
      preview: {
        label: "产出任务",
        before: { status: "todo" },
        after: { status: "waiting_review" },
        task_output: {
          summary: "完整提交说明",
          artifacts: [
            {
              id: null,
              storage_kind: "text",
              name: "最终结果",
              content: "完整产出正文",
              size_bytes: null,
              sha256: null,
              requires_followup: true,
              deleted: false,
            },
          ],
          assignments: [],
        },
      },
    };
    state.load.mockResolvedValue([output]);
    state.decide.mockRejectedValue(
      new ApiError("Approval response lost", { status: 500 }),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <AiWorkspaceActions generationId={output.generation_id} />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    await screen.findByRole("heading", { name: "完整提交说明" });
    expect(screen.getByText("完整产出正文")).toBeVisible();
    const confirm = screen.getByRole("button", { name: "确认执行" });
    expect(confirm).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name:
          action === "task.submit_output" ? /我已核实完整内容/ : /我已人工检查/,
      }),
    );
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(output, "confirm", true),
    );
    await waitFor(() => expect(state.invalidateOutputs).toHaveBeenCalled());
    await waitFor(() =>
      expect(state.load.mock.calls.length).toBeGreaterThan(1),
    );
  },
);
it.each([
  ["task.submit_output", "confirmed"],
  ["task.review", "pending"],
  ["task.review", "confirmed"],
  ["task.review", "rejected"],
] as const)(
  "links %s / %s to the exact batch with UI-owned return context",
  async (action, status) => {
    const batchId = "00000000-0000-4000-8000-000000000005";
    const sourceSession = "00000000-0000-4000-8000-000000000006";
    const output: AiActionProposal = {
      ...proposal,
      action: {
        ...proposal.action,
        action,
        changes: { submission_id: batchId },
      },
      status,
      can_confirm: status === "pending",
      result_id: status === "confirmed" ? batchId : null,
      result_version: status === "confirmed" ? 3 : null,
      route: `${proposal.route}/submissions/${batchId}`,
    };
    state.load.mockResolvedValue([output]);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <MemoryRouter>
        <QueryClientProvider client={client}>
          <AiWorkspaceActions
            generationId={output.generation_id}
            sessionId={sourceSession}
          />
        </QueryClientProvider>
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole("link", { name: "查看提交批次" }),
    ).toHaveAttribute(
      "href",
      `${output.route}?return_session=${sourceSession}`,
    );
    expect(state.decide).not.toHaveBeenCalled();
  },
);

it.each(["task.assign", "task.reassign", "task.unassign"] as const)(
  "renders responsibility %s with explicit human decision",
  async (action) => {
    const assignment: AiActionProposal = {
      ...proposal,
      action: { ...proposal.action, action, changes: { role: "assignee" } },
      preview: {
        label: "分派任务",
        before: { actor_name: "原负责人" },
        after: {
          role: "assignee",
          actor_name: "新负责人",
          actor_type: "person",
          reason: "明确交接",
        },
      },
    };
    state.load.mockResolvedValue([assignment]);
    state.decide.mockImplementation(async () => {
      const result = {
        ...assignment,
        status: "confirmed",
        can_confirm: false,
        result_id: assignment.action.task_id,
        result_version: 3,
      };
      state.load.mockResolvedValue([result]);
      return result;
    });
    mount();
    expect(await screen.findByText("原负责人")).toBeVisible();
    expect(screen.getByText("新负责人")).toBeVisible();
    expect(
      screen.getByText(/不会发送消息、启用适配器或启动 Agent/),
    ).toBeVisible();
    expect(state.decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() => expect(state.decide).toHaveBeenCalled());
    expect(state.decide.mock.calls[0][0]).toEqual(assignment);
    expect(state.decide.mock.calls[0][1]).toBe("confirm");
    await waitFor(() => expect(state.invalidate).toHaveBeenCalled());
    expect(
      await screen.findByRole("link", { name: /查看任务/ }),
    ).toHaveAttribute("href", assignment.route);
  },
);

it("shows task tag replacement semantics with human-readable names", async () => {
  const tagId = "00000000-0000-4000-8000-000000000004";
  const tagged: AiActionProposal = {
    ...proposal,
    action: {
      ...proposal.action,
      changes: {
        tag_ids: [tagId],
      },
    },
    preview: {
      label: "准备发布",
      before: { tag_names: ["旧标签"] },
      after: { tag_names: ["发布"] },
    },
  };
  state.load.mockResolvedValue([tagged]);
  mount();
  expect(await screen.findByText("旧标签")).toBeVisible();
  expect(screen.getByText("发布")).toBeVisible();
  expect(screen.getByText(/标签按上方所示完整替换/)).toBeVisible();
  expect(screen.queryByText(tagId)).toBeNull();
});

it("shows task hierarchy changes by title without exposing parent IDs", async () => {
  const parentId = "00000000-0000-4000-8000-000000000005";
  const reparented: AiActionProposal = {
    ...proposal,
    action: {
      ...proposal.action,
      changes: { parent_task_id: parentId },
    },
    preview: {
      label: "重新归类子任务",
      before: { parent_task_title: "原父任务" },
      after: { parent_task_title: "新父任务" },
    },
  };
  state.load.mockResolvedValue([reparented]);
  mount();
  expect(await screen.findByText("原父任务")).toBeVisible();
  expect(screen.getByText("新父任务")).toBeVisible();
  expect(screen.getByText(/父任务关系按上方所示设置/)).toBeVisible();
  expect(screen.queryByText(parentId)).toBeNull();
});

it.each([
  ["ACTOR_NOT_FOUND", "负责人或标签已不可用"],
  ["ASSIGNMENT_ACTOR_NOT_ACTIVE", "负责人或标签已不可用"],
  ["ASSIGNMENT_ACTOR_NOT_EXECUTABLE", "负责人或标签已不可用"],
  ["ASSIGNMENT_NOT_ACTIVE", "责任分派已变化"],
  ["ASSIGNMENT_ALREADY_ACTIVE", "责任分派已变化"],
  ["ASSIGNMENT_UNCHANGED", "责任分派已变化"],
  ["ASSIGNMENT_REVIEWER_MUST_BE_OWNER", "验收人只能是启用的本人"],
  ["ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED", "所选角色类型不能承担此责任"],
  ["TASK_NOT_ASSIGNABLE", "任务已结束"],
])(
  "explains %s without suggesting a batch creation failed",
  async (code, hint) => {
    const assignment: AiActionProposal = {
      ...proposal,
      action: {
        ...proposal.action,
        action: "task.assign",
        changes: {
          role: "assignee",
          actor_id: "00000000-0000-4000-8000-000000000004",
        },
      },
    };
    state.load.mockResolvedValue([assignment]);
    state.decide.mockRejectedValue(
      new ApiError("assignment unavailable", { code, status: 409 }),
    );
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(hint);
    expect(alert).not.toHaveTextContent("整批任务未创建");
    expect(state.decide).toHaveBeenCalledTimes(1);
    expect(state.decide).toHaveBeenCalledWith(assignment, "confirm");
    await waitFor(() =>
      expect(state.load.mock.calls.length).toBeGreaterThan(1),
    );
    expect(screen.queryByText("已执行")).not.toBeInTheDocument();
  },
);

it("offers confirmed assignment before starting an unassigned task", async () => {
  state.load.mockResolvedValue([
    {
      ...proposal,
      action: { ...proposal.action, action: "task.start", changes: {} },
    },
  ]);
  state.decide.mockRejectedValue(
    new ApiError("assignee required", {
      code: "TASK_ASSIGNEE_REQUIRED",
      status: 409,
    }),
  );
  mount();
  fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "先确认分派负责人",
  );
  expect(state.decide).toHaveBeenCalledTimes(1);
});

it.each(["confirm", "reject", "lost"])(
  "handles export approval %s without automatic download",
  async (mode) => {
    const exportProposal: AiActionProposal = {
      ...proposal,
      action: {
        action: "finance.export_csv",
        changes: {
          export_filters: {
            currency: "CNY",
            date_from: "2026-09-01",
            date_to: "2026-09-30",
            entry_type: "all",
            status: "active",
          },
        },
      },
      preview: {
        label: "九月导出",
        before: {},
        after: {
          currency: "CNY",
          date_from: "2026-09-01",
          date_to: "2026-09-30",
          entry_type: "all",
          export_status: "active",
          category: "",
          client_id: null,
          project_id: null,
          row_count: 121,
          size_bytes: 30000,
          sha256: "a".repeat(64),
          csv_columns: "",
          export_sort: "",
        },
      },
      route: "",
    };
    state.load.mockResolvedValue([exportProposal]);
    state.decide.mockImplementation(async () => {
      const result = {
        ...exportProposal,
        status: mode === "reject" ? "rejected" : "confirmed",
        can_confirm: false,
        result_id: mode === "reject" ? null : proposal.id,
        result_version: mode === "reject" ? null : 1,
      };
      state.load.mockResolvedValue([result]);
      if (mode === "lost")
        throw new ApiError("lost", { code: "NETWORK_ERROR" });
      return result;
    });
    mount();
    const button = await screen.findByRole("button", { name: "确认导出范围" });
    expect(button).toBeDisabled();
    expect(screen.getByText(/121 条/)).toBeVisible();
    expect(screen.getByText(/完整备注/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "下载已批准的 CSV" }),
    ).not.toBeInTheDocument();
    if (mode !== "reject")
      fireEvent.click(
        screen.getByRole("checkbox", { name: /我已核对全部导出范围/ }),
      );
    fireEvent.click(
      mode === "reject" ? screen.getByRole("button", { name: "拒绝" }) : button,
    );
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        exportProposal,
        mode === "reject" ? "reject" : "confirm",
        mode !== "reject",
      ),
    );
    if (mode !== "reject")
      expect(
        await screen.findByRole("button", { name: "下载已批准的 CSV" }),
      ).toBeVisible();
    else expect(await screen.findByText("已拒绝 · 未执行")).toBeVisible();
    expect(state.invalidateFinance).not.toHaveBeenCalled();
    expect(state.invalidate).not.toHaveBeenCalled();
  },
);

it.each(["confirm", "reject", "lost"])(
  "shows full destructive invoice preview and handles %s safely",
  async (mode) => {
    const invoice: AiActionProposal = {
      ...proposal,
      action: {
        action: "invoice.delete",
        invoice_id: proposal.action.task_id,
        expected_version: 1,
        changes: {},
      },
      preview: {
        label: "INV-2026-001",
        before: {
          invoice_number: "INV-2026-001",
          client_id: proposal.action.task_id!,
          client_name: "删除客户",
          project_id: null,
          project_name: null,
          amount_minor: 1250,
          currency: "CNY",
          issue_date: "2026-09-01",
          due_date: "2026-09-20",
          paid_date: null,
          status: "draft",
          notes: "待删除完整备注",
          pdf_asset_id: proposal.action.task_id!,
          pdf_file_name: "invoice.pdf",
          pdf_size_bytes: 1234,
          pdf_sha256: "b".repeat(64),
          pdf_generated_from_version: 1,
          pdf_generated_at: "2026-09-18T12:00:00Z",
        },
        after: { invoice_deleted: true, pdf_removed: true },
      },
      route: `/invoices/${proposal.action.task_id}`,
    };
    state.load.mockResolvedValue([invoice]);
    state.decide.mockImplementation(async () => {
      const result = {
        ...invoice,
        status:
          mode === "reject" ? ("rejected" as const) : ("confirmed" as const),
        can_confirm: false,
        route: mode === "reject" ? invoice.route : "/invoices",
        result_id: mode === "reject" ? null : proposal.action.task_id!,
        result_version: mode === "reject" ? null : 1,
      };
      state.load.mockResolvedValue([result]);
      if (mode === "lost")
        throw new ApiError("lost", { code: "NETWORK_ERROR" });
      return result;
    });
    const client = mount();
    const button = await screen.findByRole("button", { name: "确认执行" });
    expect(button).toBeDisabled();
    expect(screen.getByText("待删除完整备注")).toBeVisible();
    expect(screen.getByText("invoice.pdf")).toBeVisible();
    expect(screen.getByText("CNY 12.50（1250 最小单位）")).toBeVisible();
    expect(screen.getByText(/其他位置的副本和备份不会删除/)).toBeVisible();
    if (mode === "reject")
      fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
    else {
      fireEvent.click(
        screen.getByRole("checkbox", { name: /我已核对待删除草稿/ }),
      );
      expect(button).toBeDisabled();
      fireEvent.click(
        screen.getByRole("checkbox", { name: /我确认永久删除这张草稿/ }),
      );
      expect(button).toBeEnabled();
      fireEvent.click(button);
    }
    expect(
      await screen.findByText(mode === "reject" ? "已拒绝 · 未执行" : "已执行"),
    ).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledTimes(1);
    if (mode === "reject")
      expect(state.decide).toHaveBeenCalledWith(invoice, "reject");
    else {
      expect(state.decide.mock.calls[0].slice(10)).toEqual([true, true]);
      expect(
        screen.getByRole("link", { name: "查看发票列表" }),
      ).toHaveAttribute("href", "/invoices");
    }
    await waitFor(() =>
      expect(state.invalidateInvoices).toHaveBeenCalledWith(client, false),
    );
  },
);

function mount(sessionId?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <AiWorkspaceActions
          generationId={proposal.generation_id}
          sessionId={sessionId}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return client;
}

describe("workspace action result identity", () => {
  it("does not claim execution when a confirmed proposal has no result identity", async () => {
    const confirmedWithoutResult: AiActionProposal = {
      ...proposal,
      action: {
        ...proposal.action,
        action: "client.update",
        client_id: proposal.action.task_id,
        changes: { name: "客户乙" },
      },
      preview: {
        label: "客户甲",
        before: { name: "客户旧" },
        after: { name: "客户乙" },
      },
      status: "confirmed",
      can_confirm: false,
      result_id: null,
      result_version: null,
      route: `/clients/${proposal.action.task_id}`,
    };
    state.load.mockResolvedValue([confirmedWithoutResult]);

    mount(proposal.generation_id);

    expect(await screen.findByText("已确认 · 结果待回读")).toBeInTheDocument();
    expect(screen.queryByText("已执行")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("操作执行结果")).not.toBeInTheDocument();
  });
});
describe("workspace action approvals", () => {
  it("explains that a full Agent Run queue leaves the proposal pending", async () => {
    state.decide.mockRejectedValue(
      new ApiError("queue full", {
        code: "AGENT_RUN_QUEUE_FULL",
        status: 429,
      }),
    );
    mount();

    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /Agent 执行队列已满，本次确认未生效，任务\/审批状态没有改变/,
    );
    expect(state.decide).toHaveBeenCalledWith(proposal, "confirm");
    expect(
      await screen.findByRole("button", { name: "确认执行" }),
    ).toBeEnabled();
  });

  it("immediately invalidates the exact source plan when a decision settles", async () => {
    const sessionId = "00000000-0000-4000-8000-000000000008";
    const decided = {
      ...proposal,
      status: "confirmed" as const,
      can_confirm: false,
      result_id: proposal.action.task_id,
      result_version: 3,
    };
    state.decide.mockResolvedValue(decided);
    const client = mount(sessionId);
    const invalidate = vi.spyOn(client, "invalidateQueries");
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(proposal, "confirm"),
    );
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["ai", "work-plan", sessionId],
      }),
    );
  });

  it("shows a natural-language receipt for a confirmed task creation", async () => {
    const taskId = "00000000-0000-4000-8000-0000000000a1";
    const create: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.create",
        changes: { title: "整理发布清单", priority: "P2" },
      },
      preview: {
        label: "整理发布清单",
        before: {},
        after: {
          title: "整理发布清单",
          priority: "P2",
          status: "todo",
        },
      },
      route: `/tasks/${taskId}`,
    };
    const confirmed: AiActionProposal = {
      ...create,
      status: "confirmed",
      can_confirm: false,
      result_id: taskId,
      result_version: 1,
      decided_at: "2026-09-23T12:00:01Z",
    };
    state.load.mockResolvedValue([create]);
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([confirmed]);
      return confirmed;
    });

    mount();
    expect(screen.queryByLabelText("任务执行结果")).toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));

    expect(await screen.findByLabelText("任务执行结果")).toHaveTextContent(
      "已创建任务「整理发布清单」",
    );
    expect(screen.getByRole("link", { name: "打开任务" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}`,
    );
  });

  it("shows a natural-language receipt for a confirmed non-task action", async () => {
    const clientId = "00000000-0000-4000-8000-0000000000a2";
    const update: AiActionProposal = {
      ...proposal,
      action: {
        action: "client.update",
        client_id: clientId,
        expected_version: 4,
        changes: { status: "active" },
      },
      preview: {
        label: "客户甲",
        before: { status: "inactive" },
        after: { status: "active" },
      },
      route: `/clients/${clientId}`,
    };
    const confirmed: AiActionProposal = {
      ...update,
      status: "confirmed",
      can_confirm: false,
      result_id: clientId,
      result_version: 5,
      decided_at: "2026-09-23T12:00:01Z",
    };
    state.load.mockResolvedValue([update]);
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([confirmed]);
      return confirmed;
    });

    mount();
    expect(screen.queryByLabelText("操作执行结果")).toBeNull();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));

    expect(await screen.findByLabelText("操作执行结果")).toHaveTextContent(
      "已更新客户「客户甲」",
    );
    expect(screen.getByRole("link", { name: "打开客户" })).toHaveAttribute(
      "href",
      `/clients/${clientId}`,
    );
  });

  it("shows the complete project task draft and exact task link after approval", async () => {
    const projectId = "00000000-0000-4000-8000-0000000000b1";
    const taskId = "00000000-0000-4000-8000-0000000000b2";
    const actorId = "00000000-0000-4000-8000-0000000000b3";
    const batch: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.batch_create",
        project_id: projectId,
        expected_version: 1,
        changes: {
          drafts: [
            { key: "first", title: "准备发布", assignee_actor_id: actorId },
          ],
        },
      },
      preview: {
        label: "在项目中创建 1 个任务",
        before: {},
        after: { count: 1 },
        task_batch_create: {
          project_id: projectId,
          project_name: "发布项目",
          project_version: 1,
          count: 1,
          items: [
            {
              key: "first",
              title: "准备发布",
              description: "写文案",
              kind: "work",
              priority: "P2",
              planned_date: null,
              due_date: null,
              estimated_minutes: null,
              completion_criteria: "可审核",
              review_policy: "manual",
              tag_ids: [],
              tag_names: [],
              assignments: [
                {
                  role: "assignee",
                  actor_id: actorId,
                  actor_name: "小李",
                  actor_type: "person",
                  actor_status: "active",
                  actor_version: 1,
                },
                {
                  role: "reviewer",
                  actor_id: "00000000-0000-5000-8000-000000000001",
                  actor_name: "负责人",
                  actor_type: "owner",
                  actor_status: "active",
                  actor_version: 1,
                },
              ],
            },
          ],
        },
      },
      route: `/projects/${projectId}`,
    };
    state.load.mockResolvedValue([batch]);
    const decided: AiActionProposal = {
      ...batch,
      status: "confirmed",
      can_confirm: false,
      result_id: batch.id,
      result_version: 1,
      task_batch_create_result: {
        project_id: projectId,
        count: 1,
        items: [
          {
            key: "first",
            id: taskId,
            title: "准备发布",
            route: `/tasks/${taskId}`,
          },
        ],
      },
    };
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([decided]);
      return decided;
    });
    const client = mount();
    expect(
      await screen.findByRole("heading", { name: /准备发布/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("写文案")).toBeInTheDocument();
    expect(screen.getByText("初始负责人")).toBeInTheDocument();
    expect(screen.getByText("小李")).toBeInTheDocument();
    expect(screen.getByText("初始审核人")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(batch, "confirm"),
    );
    await waitFor(() => expect(state.invalidate).toHaveBeenCalledWith(client));
    expect(
      await screen.findByRole("link", { name: "准备发布" }),
    ).toHaveAttribute("href", `/tasks/${taskId}`);
    expect(screen.getByText(/其中 1 个已设置初始负责人/)).toBeInTheDocument();
  });

  it("renders task batch rows and refreshes task aggregates", async () => {
    const batch: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: "block",
          items: [
            "00000000-0000-4000-8000-000000000101",
            "00000000-0000-4000-8000-000000000102",
          ],
          expected_versions: [1, 2],
          reason: "等待客户材料",
        },
      },
      preview: {
        label: "2 个任务",
        before: {},
        after: { count: 2 },
        task_batch: {
          action: "block",
          count: 2,
          reason: "等待客户材料",
          items: [
            {
              task_id: "00000000-0000-4000-8000-000000000101",
              title: "准备发布",
              version: 1,
              field: "status",
              before: "待办",
              after: "阻塞",
            },
            {
              task_id: "00000000-0000-4000-8000-000000000102",
              title: "整理素材",
              version: 2,
              field: "status",
              before: "进行中",
              after: "阻塞",
            },
          ],
        },
      },
      route: "/tasks",
    };
    state.load.mockResolvedValue([batch]);
    state.decide.mockResolvedValue({
      ...batch,
      status: "confirmed",
      can_confirm: false,
      result_id: batch.id,
      result_version: 1,
    });
    const client = mount();
    expect(
      await screen.findByRole("heading", { name: /准备发布/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("等待客户材料")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: /整理素材/ }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(batch, "confirm"),
    );
    await waitFor(() => expect(state.invalidate).toHaveBeenCalledWith(client));
    expect(
      screen.getByRole("link", { name: /查看.*批量任务/ }),
    ).toHaveAttribute("href", "/tasks");
  });

  it("shows batch assignment identity and reason before confirmation", async () => {
    const actorId = "00000000-0000-4000-8000-000000000111";
    const taskId = "00000000-0000-4000-8000-000000000101";
    const batch: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: "set_assignee",
          items: [taskId],
          expected_versions: [3],
          actor_id: actorId,
          reason: "交由小李处理",
        },
      },
      preview: {
        label: "1 个任务",
        before: {},
        after: { count: 1 },
        task_batch: {
          action: "set_assignee",
          count: 1,
          reason: "交由小李处理",
          items: [
            {
              task_id: taskId,
              title: "准备发布",
              version: 3,
              field: "assignee",
              before: "未分派",
              after: "小李",
              after_actor: {
                role: "assignee",
                actor_id: actorId,
                actor_name: "小李",
                actor_type: "person",
                actor_status: "active",
                actor_version: 2,
              },
            },
          ],
        },
      },
      route: "/tasks",
    };
    state.load.mockResolvedValue([batch]);
    mount();
    expect(await screen.findByText("交由小李处理")).toBeInTheDocument();
    expect(screen.getByText("负责人")).toBeInTheDocument();
    expect(screen.getByText("小李")).toBeInTheDocument();
    expect(
      screen.getByText(new RegExp(`person · ${actorId} · v2`)),
    ).toBeInTheDocument();
    expect(screen.getByText(/责任变更不会自动启动智能体/)).toBeInTheDocument();
  });

  it("shows the exact batch due-time change before confirmation", async () => {
    const taskId = "00000000-0000-4000-8000-000000000101";
    const batch: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.batch_update",
        changes: {
          batch_action: "set_due_date",
          items: [taskId],
          expected_versions: [2],
          due_date: "2026-09-25T08:30:00Z",
        },
      },
      preview: {
        label: "1 个任务",
        before: {},
        after: { count: 1 },
        task_batch: {
          action: "set_due_date",
          count: 1,
          items: [
            {
              task_id: taskId,
              title: "准备发布",
              version: 2,
              field: "due_date",
              before: "未安排",
              after: "2026-09-25T08:30:00Z",
            },
          ],
        },
      },
      route: "/tasks",
    };
    state.load.mockResolvedValue([batch]);
    mount();
    expect(await screen.findByText("截止时间")).toBeInTheDocument();
    expect(screen.getByText("2026-09-25T08:30:00Z")).toBeInTheDocument();
  });

  it("shows the exact task move and refreshes task aggregates after approval", async () => {
    const taskId = "00000000-0000-4000-8000-000000000121";
    const move: AiActionProposal = {
      ...proposal,
      action: {
        action: "task.move",
        task_id: taskId,
        expected_version: 2,
        changes: {
          anchor_task_id: "00000000-0000-4000-8000-000000000122",
          expected_anchor_version: 1,
          placement: "before",
        },
      },
      preview: {
        label: "写发布说明",
        before: {},
        after: {},
        task_order: {
          task_id: taskId,
          title: "写发布说明",
          anchor_task_id: "00000000-0000-4000-8000-000000000122",
          anchor_title: "整理素材",
          placement: "before",
          planned_date: "2026-09-18",
          group_count: 5,
          active_count: 3,
          before_position: 3,
          after_position: 1,
          group_fingerprint: "b".repeat(64),
        },
      },
      route: `/tasks/${taskId}`,
    };
    state.load.mockResolvedValue([move]);
    state.decide.mockResolvedValue({
      ...move,
      status: "confirmed",
      can_confirm: false,
      result_id: taskId,
      result_version: 3,
    });
    const client = mount();
    expect(
      await screen.findByRole("region", { name: "任务顺序调整" }),
    ).toHaveTextContent("活动任务第 3 位");
    expect(
      screen.getByRole("region", { name: "任务顺序调整" }),
    ).toHaveTextContent("整理素材");
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(move, "confirm"),
    );
    await waitFor(() => expect(state.invalidate).toHaveBeenCalledWith(client));
    expect(screen.getByRole("link", { name: /查看.*任务/ })).toHaveAttribute(
      "href",
      `/tasks/${taskId}`,
    );
  });

  it("shows a roadmap move and refreshes the roadmap after approval", async () => {
    const milestoneId = "00000000-0000-4000-8000-000000000131";
    const move: AiActionProposal = {
      ...proposal,
      action: {
        action: "roadmap_milestone.move",
        roadmap_milestone_id: milestoneId,
        expected_version: 2,
        changes: {
          anchor_milestone_id: "00000000-0000-4000-8000-000000000132",
          expected_anchor_version: 1,
          placement: "before",
        },
      },
      preview: {
        label: "发布里程碑",
        before: {},
        after: {},
        roadmap_order: {
          milestone_id: milestoneId,
          title: "发布里程碑",
          anchor_id: "00000000-0000-4000-8000-000000000132",
          anchor_title: "准备里程碑",
          placement: "before",
          year: 2026,
          quarter: 3,
          group_count: 3,
          before_position: 3,
          after_position: 1,
          group_fingerprint: "a".repeat(64),
        },
      },
      route: `/roadmap?milestone=${milestoneId}`,
    };
    state.load.mockResolvedValue([move]);
    state.decide.mockResolvedValue({
      ...move,
      status: "confirmed",
      can_confirm: false,
      result_id: milestoneId,
      result_version: 3,
    });
    const client = mount();
    expect(
      await screen.findByRole("region", { name: "路线图顺序调整" }),
    ).toHaveTextContent("准备里程碑");
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(move, "confirm"),
    );
    await waitFor(() =>
      expect(state.invalidateRoadmap).toHaveBeenCalledWith(client),
    );
    expect(screen.getByRole("link", { name: /查看.*里程碑/ })).toHaveAttribute(
      "href",
      `/roadmap?milestone=${milestoneId}`,
    );
  });

  it.each(["confirm", "reject", "lost"])(
    "requires both PDF consents and recovers the exact result (%s)",
    async (mode) => {
      const invoice: AiActionProposal = {
        ...proposal,
        action: {
          action: "invoice.generate_pdf",
          invoice_id: proposal.action.task_id,
          expected_version: 1,
          changes: {},
        },
        preview: {
          label: "INV-2026-001",
          before: {
            invoice_number: "INV-2026-001",
            client_name: "PDF 客户",
            amount_minor: 1250,
            currency: "CNY",
            notes: "本地完整 PDF 备注",
            pdf_file_name: "previous.pdf",
          },
          after: { pdf_generated: true, pdf_replaced: true },
        },
        route: `/invoices/${proposal.action.task_id}`,
      };
      const result: AiActionProposal = {
        ...invoice,
        status: "confirmed",
        can_confirm: false,
        result_id: proposal.id,
        result_version: 1,
        invoice_pdf_result: {
          asset_id: proposal.id,
          invoice_id: proposal.action.task_id!,
          file_name: "new-invoice.pdf",
          mime_type: "application/pdf",
          size_bytes: 1234,
          sha256: "b".repeat(64),
          generated_from_version: 1,
          generated_at: "2026-09-18T12:00:00Z",
          integrity_status: "verified",
          integrity_checked_at: "2026-09-18T12:00:00Z",
        },
      };
      state.load.mockResolvedValue([invoice]);
      state.decide.mockImplementation(async () => {
        const decided =
          mode === "reject"
            ? { ...invoice, status: "rejected", can_confirm: false }
            : result;
        state.load.mockResolvedValue([decided]);
        if (mode === "lost")
          throw new ApiError("lost", { code: "NETWORK_ERROR" });
        return decided;
      });
      const client = mount();
      const button = await screen.findByRole("button", { name: "确认执行" });
      expect(button).toBeDisabled();
      expect(screen.getByText("本地完整 PDF 备注")).toBeVisible();
      expect(screen.getByText("previous.pdf")).toBeVisible();
      expect(
        screen.queryByRole("button", { name: "下载本次 PDF" }),
      ).not.toBeInTheDocument();
      if (mode === "reject")
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
      else {
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我已核对将写入 PDF/ }),
        );
        expect(button).toBeDisabled();
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我同意生成本地 PDF/ }),
        );
        expect(button).toBeEnabled();
        fireEvent.click(button);
      }
      if (mode === "reject") {
        expect(await screen.findByText("已拒绝 · 未执行")).toBeVisible();
        expect(state.decide).toHaveBeenCalledWith(invoice, "reject");
        expect(
          screen.queryByRole("button", { name: "下载本次 PDF" }),
        ).not.toBeInTheDocument();
      } else {
        expect(
          await screen.findByRole("button", { name: "下载本次 PDF" }),
        ).toBeVisible();
        expect(screen.getByText("new-invoice.pdf")).toBeVisible();
        expect(state.decide.mock.calls[0].slice(10)).toEqual([
          true,
          undefined,
          true,
        ]);
        expect(screen.getByRole("link", { name: "查看发票" })).toHaveAttribute(
          "href",
          invoice.route,
        );
      }
      expect(state.decide).toHaveBeenCalledTimes(1);
      await waitFor(() =>
        expect(state.invalidateInvoices).toHaveBeenCalledWith(client, false),
      );
    },
  );
  it.each(["confirm", "reject", "lost", "overdue"])(
    "requires actual invoice consent and refreshes shared facts (%s)",
    async (mode) => {
      const record = {
        invoice_number: "INV-2026-001",
        client_id: proposal.action.task_id!,
        client_name: "客户",
        project_id: null,
        project_name: null,
        amount_minor: 1250,
        currency: "CNY",
        issue_date: "2026-09-01",
        due_date: "2026-09-10",
        paid_date: null,
        status: "viewed",
        notes: "完整原始备注",
      };
      const overdue = mode === "overdue";
      const invoice: AiActionProposal = {
        ...proposal,
        action: {
          action: overdue ? "invoice.mark_overdue" : "invoice.mark_paid",
          invoice_id: proposal.action.task_id,
          expected_version: 3,
          changes: overdue ? {} : { paid_date: "2026-09-18" },
        },
        preview: {
          label: "INV-2026-001",
          before: record,
          after: {
            ...record,
            status: overdue ? "overdue" : "paid",
            paid_date: overdue ? null : "2026-09-18",
          },
        },
        route: `/invoices/${proposal.action.task_id}`,
      };
      state.load.mockResolvedValue([invoice]);
      if (mode === "lost")
        state.decide.mockRejectedValue(
          new ApiError("连接中断", { code: "NETWORK_ERROR" }),
        );
      else
        state.decide.mockResolvedValue({
          ...invoice,
          status: mode === "reject" ? "rejected" : "confirmed",
          can_confirm: false,
        });
      const client = mount();
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      expect(screen.getAllByText("CNY 12.50（1250 最小单位）")).toHaveLength(2);
      expect(screen.getAllByText("完整原始备注")).toHaveLength(2);
      expect(screen.getByRole("link", { name: "查看发票" })).toHaveAttribute(
        "href",
        invoice.route,
      );
      if (overdue)
        expect(screen.getByText(/逾期事件可能触发当前已启用/)).toBeVisible();
      else expect(screen.getByText(/创建唯一已确认收入/)).toBeVisible();
      if (mode === "reject")
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
      else {
        fireEvent.click(
          screen.getByRole("checkbox", {
            name: overdue ? /我已核实发票/ : /我确认已实际收到/,
          }),
        );
        fireEvent.click(confirm);
      }
      await waitFor(() =>
        expect(state.invalidateInvoices).toHaveBeenCalledWith(client, overdue),
      );
      expect(state.invalidateFinance).not.toHaveBeenCalled();
      expect(state.invalidate).not.toHaveBeenCalled();
      if (mode !== "reject") expect(state.decide.mock.calls[0][10]).toBe(true);
      await waitFor(() =>
        expect(state.load.mock.calls.length).toBeGreaterThan(1),
      );
    },
  );
  it.each(["confirm", "reject", "lost"])(
    "requires financial consent and refreshes facts after %s",
    async (decision) => {
      const record = {
        type: "expense",
        amount_minor: 1250,
        currency: "CNY",
        occurred_on: "2026-09-18",
        status: "confirmed",
        category: "办公",
        client_id: null,
        project_id: null,
        client_name: null,
        project_name: null,
        notes: "核对原始票据",
      };
      const financial: AiActionProposal = {
        ...proposal,
        action: {
          action: "financial_entry.void",
          financial_entry_id: proposal.action.task_id,
          expected_version: 1,
          changes: { reason: "重复记账" },
        },
        preview: {
          label: "办公支出",
          before: record,
          after: { ...record, status: "voided", reason: "重复记账" },
        },
        route: `/income/${proposal.action.task_id}`,
      };
      state.load.mockResolvedValue([financial]);
      if (decision === "lost")
        state.decide.mockRejectedValue(
          new ApiError("连接中断", { code: "NETWORK_ERROR" }),
        );
      else
        state.decide.mockResolvedValue({
          ...financial,
          status: decision === "confirm" ? "confirmed" : "rejected",
          can_confirm: false,
        });
      mount();
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      expect(screen.getAllByText("CNY 12.50（1250 最小单位）")).toHaveLength(2);
      expect(
        screen.getByText(/作废保留历史并排除统计，当前不能撤销/),
      ).toBeVisible();
      expect(
        screen.getByRole("link", { name: "查看收支记录" }),
      ).toHaveAttribute("href", financial.route);
      if (decision === "reject")
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
      else {
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我已核对收支类型/ }),
        );
        fireEvent.click(confirm);
      }
      await waitFor(() => expect(state.invalidateFinance).toHaveBeenCalled());
      expect(state.invalidate).not.toHaveBeenCalled();
      expect(state.decide.mock.calls[0][1]).toBe(
        decision === "reject" ? "reject" : "confirm",
      );
      if (decision !== "reject")
        expect(state.decide.mock.calls[0][9]).toBe(true);
      await waitFor(() =>
        expect(state.load.mock.calls.length).toBeGreaterThan(1),
      );
    },
  );
  it.each(["success", "failed", "lost", "reject"])(
    "shows original retry effects and the actual outcome (%s)",
    async (mode) => {
      const retry: AiActionProposal = {
        ...proposal,
        route: "",
        action: {
          action: "automation.retry",
          automation_run_id: proposal.action.task_id,
          expected_version: 2,
          changes: {},
        },
        preview: {
          label: "项目完成自动化",
          before: {},
          after: {},
          automation_retry: {
            run_id: proposal.action.task_id!,
            rule_id: "00000000-0000-5000-8000-000000000101",
            rule_version: 2,
            current_rule_version: 4,
            rule_enabled: false,
            trigger_type: "event",
            action_type: "inbox_item",
            permissions: ["创建本地收件箱事项"],
            attempt: 1,
            next_attempt: 2,
            scheduled_for: null,
            error_code: "ACTION_WRITE_FAILED",
            config: { priority: "P1" },
            action: {
              action_type: "inbox_item",
              title: "原始标题",
              project_id: proposal.id,
              project_name: "原始项目",
              priority: "P1",
            },
          },
        },
      };
      state.load.mockResolvedValue([retry]);
      state.decide.mockImplementation(async () => {
        const next: AiActionProposal =
          mode === "reject"
            ? { ...retry, status: "rejected", can_confirm: false }
            : {
                ...retry,
                status: "confirmed",
                can_confirm: false,
                result_id: proposal.id,
                result_version: 2,
                automation_run_result: {
                  id: proposal.id,
                  rule_id: retry.preview.automation_retry!.rule_id,
                  rule_version: 2,
                  status: mode === "success" ? "succeeded" : "failed",
                  attempt: 2,
                  retryable: mode !== "success",
                  retry_at: mode === "success" ? null : "2026-09-18T12:05:00Z",
                  error_code: mode === "success" ? null : "ACTION_WRITE_FAILED",
                  result_type: mode === "success" ? "inbox_item" : null,
                  result_id: mode === "success" ? proposal.id : null,
                },
              };
        state.load.mockResolvedValue([next]);
        if (mode === "lost")
          throw new ApiError("lost", { code: "NETWORK_ERROR" });
        return next;
      });
      const client = mount();
      expect(await screen.findByText("原始项目")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "确认执行" })).toBeDisabled();
      if (mode !== "reject")
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我确认按原快照重试/ }),
        );
      fireEvent.click(
        screen.getByRole("button", {
          name: mode === "reject" ? "拒绝" : "确认执行",
        }),
      );
      expect(
        await screen.findByText(
          mode === "reject"
            ? "已拒绝 · 未执行"
            : mode === "success"
              ? "重试成功"
              : "重试失败 · 已保留记录",
        ),
      ).toBeInTheDocument();
      expect(screen.queryByText("已执行")).not.toBeInTheDocument();
      expect(state.decide).toHaveBeenCalledTimes(1);
      if (mode !== "reject")
        expect(state.decide).toHaveBeenCalledWith(
          retry,
          "confirm",
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          true,
        );
      else expect(state.decide).toHaveBeenCalledWith(retry, "reject");
      await waitFor(() =>
        expect(state.invalidateAutomations).toHaveBeenCalledWith(client, true),
      );
      if (mode === "success")
        expect(
          screen.getByRole("link", { name: "查看本次创建的收件箱事项" }),
        ).toHaveAttribute("href", `/inbox/${proposal.id}`);
      if (mode === "failed" || mode === "lost")
        expect(screen.getByRole("alert")).toHaveTextContent(
          "本次未创建目标对象",
        );
    },
  );
  it.each(["confirm", "reject", "lost"])(
    "separates continuing automation consent and opens the real settings entry (%s)",
    async (decision) => {
      const record = {
        name: "每日查看今日任务",
        permission_summary: "创建本地提醒",
        rule_enabled: false,
        local_time: "09:00",
        timezone: "UTC",
      };
      const automation: AiActionProposal = {
        ...proposal,
        action: {
          action: "automation.enable",
          automation_rule_id: proposal.id,
          expected_version: 1,
          changes: {},
        },
        preview: {
          label: record.name,
          before: record,
          after: { ...record, rule_enabled: true },
        },
        route: `/settings/automation?rule=${proposal.id}`,
      };
      state.load.mockResolvedValue([automation]);
      if (decision === "lost")
        state.decide.mockRejectedValue(new Error("lost response"));
      else
        state.decide.mockResolvedValue({
          ...automation,
          status: decision === "confirm" ? "confirmed" : "rejected",
          can_confirm: false,
        });
      useUiStore.getState().setSettingsOpen(false);
      const client = mount(proposal.generation_id);
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      expect(screen.getByText(/停用阻止新触发/)).toBeVisible();
      expect(
        screen.getByRole("link", { name: "查看自动化规则" }),
      ).toHaveAttribute(
        "href",
        `/settings/automation?rule=${proposal.id}&return_session=${proposal.generation_id}`,
      );
      expect(
        screen.queryByRole("button", { name: "打开自动化设置" }),
      ).toBeNull();
      expect(state.decide).not.toHaveBeenCalled();
      useUiStore.getState().setSettingsOpen(false);
      if (decision !== "reject") {
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我同意该规则持续自动创建/ }),
        );
        expect(confirm).toBeEnabled();
        fireEvent.click(confirm);
        await waitFor(() =>
          expect(state.decide).toHaveBeenCalledWith(
            automation,
            "confirm",
            undefined,
            undefined,
            undefined,
            undefined,
            undefined,
            true,
          ),
        );
      } else {
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
        await waitFor(() =>
          expect(state.decide).toHaveBeenCalledWith(automation, "reject"),
        );
      }
      await waitFor(() =>
        expect(state.invalidateAutomations).toHaveBeenCalledWith(client, false),
      );
      if (decision === "lost")
        expect(await screen.findByRole("alert")).toBeVisible();
      expect(state.invalidate).not.toHaveBeenCalled();
      client.clear();
    },
  );
  it.each(["confirm", "reject", "lost"])(
    "requires human activity deletion consent and refreshes real facts (%s)",
    async (decision) => {
      const record = {
        client_id: proposal.id,
        client_name: "客户甲",
        client_status: "active",
        kind: "note",
        title: "重复会议",
        occurred_at: "2026-09-17T00:00:00Z",
        activity_state: "visible",
      };
      const activity: AiActionProposal = {
        ...proposal,
        action: {
          action: "client_activity.delete",
          client_activity_id: proposal.id,
          expected_version: 1,
          changes: { reason: "重复记录" },
        },
        preview: {
          label: record.title,
          before: record,
          after: { ...record, activity_state: "deleted", reason: "重复记录" },
        },
        route: `/clients/${proposal.id}?activity=${proposal.id}`,
      };
      state.load.mockResolvedValue([activity]);
      state.decide.mockImplementation(async () => {
        const result = {
          ...activity,
          status: decision === "reject" ? "rejected" : "confirmed",
          can_confirm: false,
          result_id:
            decision === "reject" ? null : activity.action.client_activity_id,
          result_version: decision === "reject" ? null : 1,
        };
        state.load.mockResolvedValue([result]);
        if (decision === "lost") throw new Error("response lost");
        return result;
      });
      mount(proposal.generation_id);
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      expect(screen.getByText(/当前不支持撤销删除/)).toBeVisible();
      expect(
        screen.getByRole("link", { name: "查看客户活动" }),
      ).toHaveAttribute(
        "href",
        `/clients/${proposal.id}?activity=${proposal.id}&return_session=${proposal.generation_id}`,
      );
      if (decision === "reject") {
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
        await waitFor(() =>
          expect(state.decide).toHaveBeenCalledWith(activity, "reject"),
        );
      } else {
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我确认按所示原因删除/ }),
        );
        fireEvent.click(confirm);
        await waitFor(() =>
          expect(state.decide).toHaveBeenCalledWith(
            activity,
            "confirm",
            undefined,
            undefined,
            undefined,
            undefined,
            true,
          ),
        );
      }
      await screen.findByText(
        decision === "reject" ? "已拒绝 · 未执行" : "已执行",
      );
      expect(state.invalidateClientActivities).toHaveBeenCalledOnce();
      expect(state.invalidateClientFollowups).not.toHaveBeenCalled();
      expect(
        screen.queryByRole("button", { name: "确认执行" }),
      ).not.toBeInTheDocument();
    },
  );
  it.each(["confirm", "reject", "lost"])(
    "shows next followup separately and requires actual-result consent (%s)",
    async (decision) => {
      const original = {
        client_id: proposal.id,
        client_name: "客户甲",
        client_status: "active",
        assigned_actor_id: proposal.generation_id,
        assigned_actor_name: "原负责人",
        assigned_actor_type: "person",
        assigned_actor_status: "active",
        scheduled_at: "2026-09-17T09:00:00Z",
        timezone: "UTC",
        channel: "电话",
        purpose: "原计划",
        priority: "normal",
        status: "planned",
      };
      const fields = {
        result: "已讨论实际需求",
        completed_at: "2026-09-18T08:00:00Z",
        next_step: "准备方案",
      };
      const followup: AiActionProposal = {
        ...proposal,
        action: {
          action: "client_followup.complete",
          client_followup_id: proposal.id,
          expected_version: 1,
          changes: fields,
        },
        preview: {
          label: "原计划",
          before: original,
          after: { ...original, ...fields, status: "completed" },
          next_followup: {
            ...original,
            purpose: "续排方案会议",
            assigned_actor_name: "下一负责人",
            scheduled_at: "2026-10-02T01:00:00Z",
            timezone: "Asia/Shanghai",
          },
        },
        route: `/clients/${proposal.id}`,
      };
      state.load.mockResolvedValue([followup]);
      state.decide.mockImplementation(async () => {
        const result = {
          ...followup,
          status: decision === "reject" ? "rejected" : "confirmed",
          can_confirm: false,
          result_id:
            decision === "reject" ? null : followup.action.client_followup_id,
          result_version: decision === "reject" ? null : 1,
        };
        state.load.mockResolvedValue([result]);
        if (decision === "lost")
          throw new ApiError("lost", { code: "NETWORK_ERROR" });
        return result;
      });
      mount();
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      expect(
        screen.getByRole("region", { name: "下一次回访计划" }),
      ).toHaveTextContent("续排方案会议");
      expect(screen.getByText("下一负责人")).toBeVisible();
      expect(screen.getByText("已讨论实际需求")).toBeVisible();
      expect(screen.getByText(/任一步失败则全部不生效/)).toBeVisible();
      if (decision === "reject") {
        fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
        await screen.findByText("已拒绝 · 未执行");
        expect(state.decide).toHaveBeenCalledWith(followup, "reject");
      } else {
        fireEvent.click(
          screen.getByRole("checkbox", { name: /我确认回访已实际完成/ }),
        );
        expect(confirm).toBeEnabled();
        fireEvent.click(confirm);
        await screen.findByText("已执行");
        expect(state.decide).toHaveBeenCalledWith(
          followup,
          "confirm",
          undefined,
          undefined,
          undefined,
          true,
        );
        expect(state.decide).toHaveBeenCalledTimes(1);
        expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      }
      await waitFor(() =>
        expect(state.invalidateClientFollowups).toHaveBeenCalledOnce(),
      );
    },
  );
  it("explains local-only followup plans, links the Client and refreshes facts after a conflict", async () => {
    const fields = {
      client_id: proposal.id,
      client_name: "客户甲",
      client_status: "active",
      assigned_actor_id: proposal.generation_id,
      assigned_actor_name: "本地负责人",
      assigned_actor_type: "person",
      assigned_actor_status: "active",
      purpose: "确认交付",
      scheduled_at: "2026-10-01T09:00:00Z",
      timezone: "Asia/Shanghai",
      channel: "电话",
      priority: "normal",
      status: "planned",
    };
    const followup: AiActionProposal = {
      ...proposal,
      action: {
        action: "client_followup.cancel",
        client_followup_id: proposal.id,
        expected_version: 1,
        changes: { reason: "无需继续" },
      },
      preview: {
        label: "确认交付",
        before: fields,
        after: { ...fields, status: "cancelled", reason: "无需继续" },
      },
      route: `/clients/${proposal.id}`,
    };
    state.load.mockResolvedValue([followup]);
    state.decide.mockRejectedValue(
      new ApiError("changed", {
        code: "AI_ACTION_PREVIEW_CHANGED",
        status: 409,
      }),
    );
    mount();
    expect(await screen.findByText("取消客户回访计划")).toBeVisible();
    expect(screen.getByText(/不发送消息、不联系客户/)).toBeVisible();
    expect(screen.getAllByText("客户甲")).toHaveLength(2);
    expect(screen.getAllByText("Asia/Shanghai")).toHaveLength(2);
    expect(screen.getByRole("link", { name: "查看客户回访" })).toHaveAttribute(
      "href",
      followup.route,
    );
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "客户或负责人资料已变化",
    );
    await waitFor(() =>
      expect(state.invalidateClientFollowups).toHaveBeenCalledOnce(),
    );
    expect(state.invalidate).not.toHaveBeenCalled();
  });
  it.each([true, false])(
    "requires human new-cycle consent for Focus start (bound: %s)",
    async (bound) => {
      const focus: AiActionProposal = {
        ...proposal,
        action: {
          action: "focus.start",
          changes: {
            task_id: bound ? proposal.id : null,
            planned_seconds: 300,
            ...(bound ? { expected_task_version: 1 } : {}),
          },
        },
        preview: {
          label: "新专注",
          before: { status: "idle" },
          after: {
            status: "active",
            task_id: bound ? proposal.id : null,
            task_title: bound ? "新专注" : null,
            task_version: bound ? 1 : null,
            planned_seconds: 300,
          },
        },
        route: "",
      };
      state.load.mockResolvedValue([focus]);
      const result = {
        ...focus,
        status: "confirmed" as const,
        can_confirm: false,
        result_id: proposal.id,
        result_version: 1,
        route: "/focus",
      };
      state.decide.mockImplementation(async () => {
        state.load.mockResolvedValue([result]);
        return result;
      });
      mount();
      const confirm = await screen.findByRole("button", { name: "确认执行" });
      expect(confirm).toBeDisabled();
      const checkbox = screen.getByRole("checkbox", {
        name: bound ? /我确认按所示任务开始新循环/ : /我确认不绑定任务/,
      });
      fireEvent.click(checkbox);
      expect(confirm).toBeEnabled();
      fireEvent.click(confirm);
      await waitFor(() =>
        expect(state.decide).toHaveBeenCalledWith(
          focus,
          "confirm",
          undefined,
          undefined,
          { start: true },
        ),
      );
      expect(await screen.findByText("已执行")).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "查看专注" })).toHaveAttribute(
        "href",
        "/focus",
      );
      expect(state.invalidateFocus).toHaveBeenCalled();
    },
  );
  it("requires separate gap consent and recovers the approval after response loss", async () => {
    const focus: AiActionProposal = {
      ...proposal,
      action: {
        action: "focus.recover",
        focus_session_id: proposal.id,
        expected_version: 2,
        changes: { recovery_action: "include_gap_resume" },
      },
      preview: {
        label: "中断的专注",
        before: { status: "recovery_pending" },
        after: {
          status: "active",
          task_id: null,
          task_title: null,
          planned_seconds: 300,
          recovery_action: "include_gap_resume",
          last_heartbeat_at: "2026-09-18T12:00:00Z",
          accumulated_seconds: 20,
        },
      },
      route: "/focus",
    };
    state.load.mockResolvedValue([focus]);
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([
        {
          ...focus,
          status: "confirmed",
          can_confirm: false,
          result_id: proposal.id,
          result_version: 3,
        },
      ]);
      throw new ApiError("lost", { code: "NETWORK_ERROR" });
    });
    mount();
    const confirm = await screen.findByRole("button", { name: "确认执行" });
    expect(confirm).toBeDisabled();
    expect(screen.getByText("计入中断间隔并继续")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /我确认中断期间持续工作/ }),
    );
    fireEvent.click(confirm);
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(
      focus,
      "confirm",
      undefined,
      undefined,
      { includeGap: true },
    );
    expect(state.invalidateFocus).toHaveBeenCalled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
  it("can reject a Focus start without granting start consent", async () => {
    const focus: AiActionProposal = {
      ...proposal,
      action: {
        action: "focus.start",
        changes: { task_id: null, planned_seconds: 300 },
      },
    };
    state.load.mockResolvedValue([focus]);
    state.decide.mockImplementation(async () => {
      const result = {
        ...focus,
        status: "rejected" as const,
        can_confirm: false,
      };
      state.load.mockResolvedValue([result]);
      return result;
    });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    expect(await screen.findByText("已拒绝 · 未执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(focus, "reject");
  });
  it("explains confirm-time Focus control and refreshes authoritative snapshots even on uncertain failure", async () => {
    const focus: AiActionProposal = {
      ...proposal,
      action: {
        action: "focus.pause",
        focus_session_id: proposal.id,
        expected_version: 1,
        changes: {},
      },
      preview: {
        label: "正在写作",
        before: { status: "active" },
        after: {
          status: "paused",
          task_id: null,
          task_title: null,
          planned_seconds: 300,
        },
      },
      route: "/focus",
    };
    state.load.mockResolvedValue([focus]);
    state.decide.mockRejectedValue(
      new ApiError("network", { code: "NETWORK_ERROR" }),
    );
    mount();
    expect(await screen.findByText(/计时结算到确认时刻/)).toBeVisible();
    expect(screen.getByRole("link", { name: "查看专注" })).toHaveAttribute(
      "href",
      "/focus",
    );
    expect(state.decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() => expect(state.invalidateFocus).toHaveBeenCalledOnce());
    expect(state.decide).toHaveBeenCalledWith(focus, "confirm");
    expect(await screen.findByRole("alert")).toHaveTextContent("正在回读状态");
    expect(state.invalidate).not.toHaveBeenCalled();
  });
  it("keeps force-resolve disabled until the human acknowledges the exception", async () => {
    const forced: AiActionProposal = {
      ...proposal,
      action: {
        action: "inbox.force_resolve",
        inbox_item_id: proposal.id,
        expected_version: 2,
        changes: { reason: "客户取消交付" },
      },
      preview: {
        label: "例外事项",
        before: { status: "tracking" },
        after: {
          status: "resolved",
          resolution_mode: "forced",
          required_remaining_count: 2,
          required_blocked_count: 1,
          required_waiting_review_count: 1,
          reason: "客户取消交付",
        },
      },
      route: `/inbox/${proposal.id}`,
    };
    state.load.mockResolvedValue([forced]);
    state.decide.mockRejectedValue(
      new ApiError("changed", {
        code: "AI_ACTION_PREVIEW_CHANGED",
        status: 409,
      }),
    );
    mount();
    const confirm = await screen.findByRole("button", { name: "确认执行" });
    expect(confirm).toBeDisabled();
    expect(screen.getByText("人工例外强制解决")).toBeVisible();
    expect(screen.getByText("待验收的必需任务数")).toBeVisible();
    const consent = screen.getByRole("checkbox", {
      name: /我确认按所示原因例外结清此事项/,
    });
    expect(consent).not.toBeChecked();
    fireEvent.click(consent);
    fireEvent.click(confirm);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        forced,
        "confirm",
        undefined,
        true,
      ),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("旧提议未执行");
    await waitFor(() => expect(state.invalidateInbox).toHaveBeenCalled());
    expect(state.invalidate).not.toHaveBeenCalled();
    expect(
      screen.getByRole("link", { name: "查看收件箱事项" }),
    ).toHaveAttribute("href", forced.route);
  });

  it("can reject force-resolve without exceptional consent", async () => {
    const forced: AiActionProposal = {
      ...proposal,
      action: {
        action: "inbox.force_resolve",
        inbox_item_id: proposal.id,
        expected_version: 2,
        changes: { reason: "例外" },
      },
      preview: {
        label: "例外事项",
        before: {},
        after: { required_remaining_count: 0 },
      },
    };
    state.load.mockResolvedValue([forced]);
    const rejected: AiActionProposal = {
      ...forced,
      status: "rejected",
      can_confirm: false,
    };
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([rejected]);
      return rejected;
    });
    mount();
    expect(
      await screen.findByRole("button", { name: "确认执行" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(forced, "reject"),
    );
    expect(await screen.findByText("已拒绝 · 未执行")).toBeVisible();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("shows the Reminder schedule, cancellation boundary, real route and terminal recovery", async () => {
    const fields = {
      title: "月末复盘",
      summary: "",
      priority: "P2",
      trigger_at: "2026-10-31T01:00:00Z",
      status: "scheduled",
      recurrence_type: "monthly",
      recurrence_interval: 1,
      recurrence_timezone: "Asia/Shanghai",
      recurrence_anchor_day: 31,
      occurrence_number: 1,
    };
    const reminder: AiActionProposal = {
      ...proposal,
      action: {
        action: "reminder.cancel",
        reminder_id: proposal.id,
        expected_version: 1,
        changes: { reason: "停止系列" },
      },
      preview: {
        label: "月末复盘",
        before: fields,
        after: { ...fields, status: "cancelled", reason: "停止系列" },
      },
      route: `/inbox?reminder=${proposal.id}`,
    };
    state.load.mockResolvedValue([reminder]);
    state.decide.mockRejectedValue(
      new ApiError("fired", { code: "REMINDER_NOT_SCHEDULED", status: 409 }),
    );
    mount();
    expect(await screen.findByText("取消本地提醒")).toBeVisible();
    expect(screen.getAllByText("Asia/Shanghai")).toHaveLength(2);
    expect(
      screen.getByText(/取消当前待触发记录会停止该系列后续提醒/),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: /查看本地提醒/ })).toHaveAttribute(
      "href",
      reminder.route,
    );
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "提醒已触发或已取消",
    );
    await waitFor(() => expect(state.invalidateReminders).toHaveBeenCalled());
    expect(state.invalidate).not.toHaveBeenCalled();
  });

  it.each([true, false])(
    "shows the split graph and refreshes facts (exact receipt: %s)",
    async (hasReceipt) => {
      const actor = {
        id: proposal.id,
        label: "小林",
        type: "person" as const,
        version: 1,
      };
      const fields = {
        title: "准备交付",
        description: "草稿说明",
        priority: "P1",
        kind: "work",
        planned_date: "2026-09-20",
        due_date: null,
        estimated_minutes: 50,
        project_id: null,
        completion_criteria: "交付验收证据",
        review_policy: "none",
        status: "todo",
      };
      const draft = {
        key: "parent",
        parent_key: "",
        is_required: true,
        assignee: actor,
        reviewer: null,
        project: null,
        tags: [],
        fields,
      };
      const split: AiActionProposal = {
        ...proposal,
        action: {
          action: "inbox.split",
          inbox_item_id: proposal.id,
          expected_version: 1,
          changes: {},
        },
        preview: {
          label: "跟进事项",
          before: { status: "open" },
          after: { status: "tracking", created_task_count: 2 },
          tasks: [
            draft,
            {
              ...draft,
              key: "child",
              parent_key: "parent",
              is_required: false,
              fields: { ...fields, title: "可选检查" },
            },
          ],
        },
        route: "/inbox/" + proposal.id,
      };
      state.load.mockResolvedValue([split]);
      state.decide.mockResolvedValue({
        ...split,
        created_task_ids: hasReceipt
          ? [proposal.id, proposal.generation_id]
          : undefined,
        status: "confirmed",
        can_confirm: false,
        result_id: proposal.id,
        result_version: 2,
      });
      state.load.mockResolvedValueOnce([split]).mockResolvedValue([
        {
          ...split,
          status: "confirmed",
          can_confirm: false,
          result_id: proposal.id,
          result_version: 2,
          created_task_ids: hasReceipt
            ? [proposal.id, proposal.generation_id]
            : undefined,
        },
      ]);
      mount();
      expect(
        await screen.findByRole("region", { name: "整批任务预览" }),
      ).toHaveTextContent("2 项将一次性创建");
      expect(screen.getByText("1. 准备交付")).toBeInTheDocument();
      expect(screen.getByText("2. 可选检查")).toBeInTheDocument();
      expect(screen.getByText("准备交付")).toBeInTheDocument();
      expect(screen.getAllByText("小林（本地责任记录）")).toHaveLength(2);
      expect(screen.getAllByText("交付验收证据")).toHaveLength(2);
      expect(state.decide).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "确认创建整批任务" }));
      await waitFor(() =>
        expect(state.decide).toHaveBeenCalledExactlyOnceWith(split, "confirm"),
      );
      await waitFor(() => expect(state.invalidate).toHaveBeenCalledOnce());
      expect(state.invalidateInbox).toHaveBeenCalledOnce();
      if (!hasReceipt) {
        expect(await screen.findByLabelText("拆分任务结果")).toHaveTextContent(
          "此历史审批未保存拆分任务的精确对应",
        );
        expect(screen.queryByRole("link", { name: "任务 1" })).toBeNull();
        return;
      }
      expect(await screen.findByLabelText("拆分任务结果")).toHaveTextContent(
        "已创建 2 个任务",
      );
      expect(screen.getByRole("link", { name: "任务 1" })).toHaveAttribute(
        "href",
        expect.stringContaining(`/tasks/${proposal.id}`),
      );
    },
  );
  it("shows relationship effects and explains related progress conflicts without executing a substitute", async () => {
    const relation: AiActionProposal = {
      ...proposal,
      action: {
        action: "inbox.set_required",
        inbox_item_id: proposal.action.task_id,
        expected_version: 2,
        changes: {
          task_id: proposal.id,
          expected_task_version: 1,
          is_required: false,
        },
      },
      preview: {
        label: "调整关联",
        before: { is_required: true, status: "tracking" },
        after: {
          task_title: "关联工作",
          task_id: proposal.id,
          is_required: false,
          status: "resolved",
          resolution_policy: "all_required_tasks_done",
          required_remaining_count: 0,
        },
      },
      route: "/inbox/" + proposal.action.task_id,
    };
    state.load.mockResolvedValue([relation]);
    state.decide.mockRejectedValue(
      new ApiError("changed", {
        code: "AI_ACTION_PREVIEW_CHANGED",
        status: 409,
      }),
    );
    mount();
    expect(await screen.findByText("关联工作")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看关联任务" })).toHaveAttribute(
      "href",
      "/tasks/" + proposal.id,
    );
    expect(screen.getByText("必需任务全部完成后自动解决")).toBeInTheDocument();
    expect(screen.getByText("是")).toBeInTheDocument();
    expect(
      screen.getByText(/自动策略可能据此解决收件箱事项/),
    ).toBeInTheDocument();
    expect(state.decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "关联任务的进度已变化",
    );
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(relation, "confirm");
    await waitFor(() => expect(state.invalidateInbox).toHaveBeenCalled());
  });
  const inboxProposal: AiActionProposal = {
    ...proposal,
    action: {
      action: "inbox.resolve",
      inbox_item_id: proposal.action.task_id,
      expected_version: 2,
      changes: { reason: "已核对" },
    },
    preview: {
      label: "核对事项",
      before: { status: "tracking" },
      after: { status: "resolved", reason: "已核对", snoozed_until: null },
    },
    route: "/inbox/" + proposal.action.task_id,
  };
  const readAllProposal: AiActionProposal = {
    ...proposal,
    action: {
      action: "inbox.read_all",
      changes: {
        through_created_at: "2026-09-18T11:59:59.123456789Z",
      },
    },
    preview: {
      label: "将快照内 3 条未读事项标为已读",
      before: {
        snapshot_at: "2026-09-18T11:59:59.123456789Z",
        snapshot_unread_count: 3,
      },
      after: {
        through_created_at: "2026-09-18T11:59:59.123456789Z",
        snapshot_unread_count: 0,
        marked_count: 3,
      },
      inbox_read_all: {
        through_created_at: "2026-09-18T11:59:59.123456789Z",
        candidate_count: 3,
        selection_fingerprint: "b".repeat(64),
      },
    },
    route: "/inbox",
  };
  it("shows the Inbox snapshot boundary, confirms the exact proposal and refreshes only read-all facts", async () => {
    const confirmed: AiActionProposal = {
      ...readAllProposal,
      status: "confirmed",
      can_confirm: false,
      result_id: readAllProposal.id,
      result_version: 1,
      decided_at: "2026-09-18T12:00:02Z",
      inbox_read_all_result: {
        through_created_at: "2026-09-18T11:59:59.123456789Z",
        marked_count: 3,
      },
    };
    state.load.mockResolvedValue([readAllProposal]);
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([confirmed]);
      return confirmed;
    });

    const client = mount(readAllProposal.generation_id);

    expect(
      await screen.findByText("将快照内 3 条未读事项标为已读"),
    ).toBeVisible();
    expect(screen.getByText("执行截止时间")).toBeVisible();
    expect(
      screen.getByText(/快照后新到、恢复显示或被修改的事项不会包含/),
    ).toBeVisible();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.getByRole("link", { name: "查看收件箱" })).toHaveAttribute(
      "href",
      `/inbox?return_session=${readAllProposal.generation_id}`,
    );

    fireEvent.click(screen.getByRole("button", { name: "确认标记 3 条" }));

    expect(await screen.findByText("已执行 · 已标记 3 条")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(
      readAllProposal,
      "confirm",
    );
    await waitFor(() =>
      expect(state.invalidateInboxReadAll).toHaveBeenCalledWith(client),
    );
    expect(state.invalidateInbox).not.toHaveBeenCalled();
    expect(state.invalidateProjects).not.toHaveBeenCalled();
    expect(state.invalidate).not.toHaveBeenCalled();
  });
  it("allows rejecting Inbox read-all without executing the snapshot", async () => {
    const rejected: AiActionProposal = {
      ...readAllProposal,
      status: "rejected",
      can_confirm: false,
      decided_at: "2026-09-18T12:00:02Z",
    };
    state.load.mockResolvedValue([readAllProposal]);
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([rejected]);
      return rejected;
    });

    mount();
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));

    expect(await screen.findByText("已拒绝 · 未执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledExactlyOnceWith(
      readAllProposal,
      "reject",
    );
  });
  it("confirms Inbox changes without claiming linked tasks completed and refreshes read models", async () => {
    state.load.mockResolvedValue([inboxProposal]);
    state.decide.mockImplementation(async () => {
      const result = {
        ...inboxProposal,
        status: "confirmed" as const,
        can_confirm: false,
        result_id: inboxProposal.action.inbox_item_id!,
        result_version: 3,
      };
      state.load.mockResolvedValue([result]);
      return result;
    });
    mount();
    expect(
      await screen.findByText(/不会完成关联任务或更改原始来源/),
    ).toBeInTheDocument();
    expect(screen.getByText("已解决")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "查看收件箱事项" }),
    ).toHaveAttribute("href", inboxProposal.route);
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledWith(inboxProposal, "confirm");
    await waitFor(() => expect(state.invalidateInbox).toHaveBeenCalled());
    expect(state.invalidateProjects).not.toHaveBeenCalled();
    expect(state.invalidate).not.toHaveBeenCalled();
  });
  it("explains required-task guards without falling back to force resolve", async () => {
    state.load.mockResolvedValue([inboxProposal]);
    state.decide.mockRejectedValue(
      new ApiError("guard", {
        code: "INBOX_REQUIRED_TASKS_INCOMPLETE",
        status: 409,
      }),
    );
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "仍有未完成的必需任务",
    );
    expect(state.decide).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("已执行")).not.toBeInTheDocument();
  });
  const projectProposal: AiActionProposal = {
    ...proposal,
    action: {
      action: "project.complete",
      project_id: proposal.action.task_id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "交付项目",
      before: { status: "in_progress" },
      after: { status: "completed", incomplete_task_count: 2 },
    },
    route: "/projects/" + proposal.action.task_id,
  };
  it("requires explicit incomplete-task consent and refreshes project facts", async () => {
    state.load.mockResolvedValue([projectProposal]);
    state.decide.mockImplementation(async () => {
      const confirmed = {
        ...projectProposal,
        status: "confirmed" as const,
        can_confirm: false,
        result_id: projectProposal.action.project_id!,
        result_version: 3,
      };
      state.load.mockResolvedValue([confirmed]);
      return confirmed;
    });
    mount();
    expect(
      await screen.findByText("完成项目不会自动完成、取消或删除其中的任务。"),
    ).toBeInTheDocument();
    const button = screen.getByRole("button", { name: "确认执行" });
    expect(button).toBeDisabled();
    fireEvent.click(
      screen.getByRole("checkbox", { name: /保留这 2 项未完成任务/ }),
    );
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(
        projectProposal,
        "confirm",
        true,
      ),
    );
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    await waitFor(() => expect(state.invalidateProjects).toHaveBeenCalled());
    expect(screen.getByRole("link", { name: "查看项目" })).toHaveAttribute(
      "href",
      projectProposal.route,
    );
  });
  it("allows refusing project completion without consenting to unfinished tasks", async () => {
    state.load.mockResolvedValue([projectProposal]);
    state.decide.mockImplementation(async () => {
      const rejected = {
        ...projectProposal,
        status: "rejected" as const,
        can_confirm: false,
      };
      state.load.mockResolvedValue([rejected]);
      return rejected;
    });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    expect(await screen.findByText("已拒绝 · 未执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledWith(projectProposal, "reject");
  });
  it("shows exact changes and waits for a human click before executing", async () => {
    const confirmed = {
      ...proposal,
      status: "confirmed" as const,
      can_confirm: false,
      result_id: proposal.action.task_id!,
      result_version: 3,
    };
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([confirmed]);
      return confirmed;
    });
    mount();
    expect(await screen.findByText("2026-09-20")).toBeInTheDocument();
    expect(screen.getByText("未设置")).toBeInTheDocument();
    expect(state.decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
    await waitFor(() =>
      expect(state.decide).toHaveBeenCalledWith(proposal, "confirm"),
    );
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "确认执行" })).toBeNull();
    expect(state.invalidate).toHaveBeenCalled();
    expect(screen.getByRole("link", { name: "查看任务" })).toHaveAttribute(
      "href",
      proposal.route,
    );
  });
  it("recovers a committed decision after response loss by reading it back", async () => {
    state.decide.mockImplementation(async () => {
      state.load.mockResolvedValue([
        {
          ...proposal,
          status: "confirmed",
          can_confirm: false,
          result_id: proposal.action.task_id,
          result_version: 3,
        },
      ]);
      throw new ApiError("lost", { code: "NETWORK_ERROR" });
    });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    expect(await screen.findByText("已执行")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(state.decide).toHaveBeenCalledTimes(1);
  });
  it("explains stale versions without automatically approving a refreshed task", async () => {
    state.decide.mockRejectedValue(
      new ApiError("conflict", { code: "VERSION_CONFLICT", status: 409 }),
    );
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "确认执行" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "任务已在其他地方修改",
    );
    expect(state.decide).toHaveBeenCalledTimes(1);
  });
  it("rejects a suggestion and never submits a confirm request", async () => {
    state.decide.mockImplementation(async () => {
      const rejected = {
        ...proposal,
        status: "rejected" as const,
        can_confirm: false,
      };
      state.load.mockResolvedValue([rejected]);
      return rejected;
    });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: "拒绝" }));
    expect(await screen.findByText("已拒绝 · 未执行")).toBeInTheDocument();
    expect(state.decide).toHaveBeenCalledWith(proposal, "reject");
  });
  it("keeps unfinished proposals non-executable and offers explicit reload on errors", async () => {
    state.load.mockResolvedValue([{ ...proposal, can_confirm: false }]);
    mount();
    expect(
      await screen.findByRole("button", { name: "确认执行" }),
    ).toBeDisabled();
    cleanup();
    state.load.mockRejectedValue(new Error("offline"));
    mount();
    expect(
      await screen.findByRole(
        "button",
        { name: "重新读取操作建议" },
        { timeout: 2500 },
      ),
    ).toBeInTheDocument();
  });
  it.each(["expired", "unavailable"])(
    "can dismiss an %s proposal without executing it",
    async (status) => {
      const stale = { ...proposal, status, can_confirm: false };
      state.load.mockResolvedValue([stale]);
      state.decide.mockImplementation(async () => {
        const rejected = {
          ...proposal,
          status: "rejected",
          can_confirm: false,
        };
        state.load.mockResolvedValue([rejected]);
        return rejected;
      });
      mount();
      expect(
        await screen.findByRole("button", { name: "确认执行" }),
      ).toBeDisabled();
      fireEvent.click(screen.getByRole("button", { name: "拒绝" }));
      expect(await screen.findByText("已拒绝 · 未执行")).toBeInTheDocument();
      expect(state.decide).toHaveBeenCalledWith(stale, "reject");
    },
  );
});
it("shows complete note content and requires separate delete consent, then refreshes after a lost response", async () => {
  const note: AiActionProposal = {
    ...proposal,
    action: {
      action: "project_note.delete",
      project_note_id: proposal.id,
      expected_version: 1,
      changes: { reason: "重复笔记" },
    },
    preview: {
      label: "笔记",
      before: { body: "完整原始正文" },
      after: {
        body: "完整原始正文",
        note_state: "deleted",
        reason: "重复笔记",
      },
    },
  };
  state.load.mockResolvedValue([note]);
  state.decide.mockRejectedValue(
    new ApiError("Response lost", { status: 500 }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={note.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  await screen.findByText("删除项目笔记");
  expect(screen.getAllByText("完整原始正文").length).toBeGreaterThan(0);
  const confirm = screen.getByRole("button", { name: "确认执行" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我确认软删除这条项目笔记/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(note, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateProjects).toHaveBeenCalled());
  await waitFor(() => expect(state.load.mock.calls.length).toBeGreaterThan(1));
});

it("renders a roadmap milestone proposal and refreshes roadmap facts after confirmation", async () => {
  const milestone: AiActionProposal = {
    ...proposal,
    action: {
      action: "roadmap_milestone.archive",
      roadmap_milestone_id: "00000000-0000-4000-8000-000000000090",
      expected_version: 3,
      changes: {},
    },
    preview: {
      label: "第三季度交付",
      before: {
        status: "active",
        project_ids: ["00000000-0000-4000-8000-000000000091"],
      },
      after: {
        status: "archived",
        project_ids: ["00000000-0000-4000-8000-000000000091"],
      },
    },
    route: "/roadmap?milestone=00000000-0000-4000-8000-000000000090",
  };
  state.load.mockResolvedValue([milestone]);
  state.decide.mockResolvedValue({
    ...milestone,
    status: "confirmed",
    can_confirm: false,
    result_id: milestone.action.roadmap_milestone_id,
    result_version: 4,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={milestone.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("归档路线图里程碑")).toBeInTheDocument();
  expect(screen.getByText("第三季度交付")).toBeInTheDocument();
  expect(screen.getByText("进行中")).toBeInTheDocument();
  expect(screen.getByText("已归档")).toBeInTheDocument();
  expect(screen.getByText(/不会永久删除里程碑/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(milestone, "confirm"),
  );
  await waitFor(() => expect(state.invalidateRoadmap).toHaveBeenCalled());
});

it("renders a content schedule proposal and refreshes content facts after confirmation", async () => {
  const content: AiActionProposal = {
    ...proposal,
    action: {
      action: "content_item.schedule",
      content_item_id: "00000000-0000-4000-8000-000000000092",
      expected_version: 3,
      changes: {
        scheduled_at: "2026-09-20T17:00:00Z",
        scheduled_timezone: "America/Tijuana",
      },
    },
    preview: {
      label: "发布稿",
      before: {
        scheduled_at: null,
        scheduled_timezone: null,
        status: "draft",
      },
      after: {
        scheduled_at: "2026-09-20T17:00:00Z",
        scheduled_timezone: "America/Tijuana",
        status: "scheduled",
      },
    },
    route: "/content-calendar?item=00000000-0000-4000-8000-000000000092",
  };
  state.load.mockResolvedValue([content]);
  state.decide.mockResolvedValue({
    ...content,
    status: "confirmed",
    can_confirm: false,
    result_id: content.action.content_item_id,
    result_version: 4,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={content.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("安排内容发布时间")).toBeInTheDocument();
  expect(screen.getByText("发布稿")).toBeInTheDocument();
  expect(screen.getByText("草稿")).toBeInTheDocument();
  expect(screen.getByText("待触发")).toBeInTheDocument();
  expect(screen.getByText(/不会访问外部链接/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(content, "confirm"),
  );
  await waitFor(() => expect(state.invalidateContent).toHaveBeenCalled());
});

it("renders an exact content preparation-task relation without implying Task execution", async () => {
  const contentId = "00000000-0000-4000-8000-000000000092";
  const taskId = "00000000-0000-4000-8000-000000000094";
  const relation: AiActionProposal = {
    ...proposal,
    action: {
      action: "content_item.link_task",
      content_item_id: contentId,
      expected_version: 3,
      changes: {
        task_id: taskId,
        expected_task_version: 2,
        is_required: true,
      },
    },
    preview: {
      label: "发布稿",
      before: {
        relation_state: "unlinked",
        is_required: false,
        content_item_version: 3,
      },
      after: {
        task_id: taskId,
        task_title: "准备发布素材",
        task_version: 2,
        task_status: "todo",
        relation_state: "linked",
        is_required: true,
        content_item_version: 4,
      },
    },
    route: `/content-calendar?item=${contentId}`,
  };
  state.load.mockResolvedValue([relation]);
  state.decide.mockResolvedValue({
    ...relation,
    status: "confirmed",
    can_confirm: false,
    result_id: contentId,
    result_version: 4,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={relation.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("关联内容准备任务")).toBeInTheDocument();
  expect(screen.getAllByText("准备发布素材").length).toBeGreaterThan(0);
  expect(
    screen.getByText(/不会创建、完成、删除、分派或改变任务状态/),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(relation, "confirm"),
  );
  await waitFor(() => expect(state.invalidateContent).toHaveBeenCalled());
});

it("renders a private client profile preview and refreshes related facts", async () => {
  const clientProposal: AiActionProposal = {
    ...proposal,
    action: {
      action: "client.update",
      client_id: "00000000-0000-4000-8000-000000000094",
      expected_version: 2,
      changes: { contact_name: "Ada", email: "ada@example.com" },
    },
    preview: {
      label: "示例客户",
      before: { contact_name: null, email: null },
      after: { contact_name: "Ada", email: "ada@example.com" },
    },
    route: "/clients/00000000-0000-4000-8000-000000000094",
  };
  state.load.mockResolvedValue([clientProposal]);
  state.decide.mockResolvedValue({
    ...clientProposal,
    status: "confirmed",
    can_confirm: false,
    result_id: clientProposal.action.client_id,
    result_version: 3,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={clientProposal.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("修改客户资料")).toBeInTheDocument();
  expect(screen.getByText("Ada")).toBeInTheDocument();
  expect(screen.getByText("ada@example.com")).toBeInTheDocument();
  expect(screen.getByText(/不会联系客户、发送消息/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(clientProposal, "confirm"),
  );
  await waitFor(() => expect(state.invalidateClients).toHaveBeenCalled());
});

it("renders a local Client contact unlink and refreshes relationship facts", async () => {
  const clientId = "00000000-0000-4000-8000-000000000094";
  const contact: AiActionProposal = {
    ...proposal,
    action: {
      action: "client_contact.unlink",
      client_id: clientId,
      client_actor_link_id: "00000000-0000-4000-8000-000000000096",
      expected_version: 4,
      changes: { reason: "联系人职责已变更" },
    },
    preview: {
      label: "示例客户",
      before: {
        client_id: clientId,
        client_name: "示例客户",
        client_status: "active",
        actor_id: "00000000-0000-4000-8000-000000000095",
        actor_name: "本地联系人",
        actor_type: "person",
        actor_status: "active",
        actor_version: 2,
        role: "contact",
        link_state: "active",
      },
      after: {
        client_id: clientId,
        client_name: "示例客户",
        client_status: "active",
        actor_id: "00000000-0000-4000-8000-000000000095",
        actor_name: "本地联系人",
        actor_type: "person",
        actor_status: "active",
        actor_version: 2,
        role: "contact",
        link_state: "unlinked",
        reason: "联系人职责已变更",
      },
    },
    route: `/clients/${clientId}`,
  };
  state.load.mockResolvedValue([contact]);
  state.decide.mockResolvedValue({
    ...contact,
    status: "confirmed",
    can_confirm: false,
    result_id: contact.action.client_actor_link_id,
    result_version: 5,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={contact.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("解除客户联系人")).toBeInTheDocument();
  expect(screen.getAllByText("本地联系人")).toHaveLength(2);
  expect(screen.getByText(/关系历史都会保留/)).toBeVisible();
  expect(screen.getByRole("link", { name: "查看客户联系人" })).toHaveAttribute(
    "href",
    `/clients/${clientId}`,
  );
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(contact, "confirm"),
  );
  await waitFor(() =>
    expect(state.invalidateClientContacts).toHaveBeenCalled(),
  );
  expect(state.invalidateClients).not.toHaveBeenCalled();
});

it("renders a local person confirmation and refreshes shared identity facts", async () => {
  const actorId = "00000000-0000-4000-8000-000000000097";
  const person: AiActionProposal = {
    ...proposal,
    action: {
      action: "person.update",
      actor_id: actorId,
      expected_version: 2,
      changes: { display_name: "Ada Lovelace", notes: "用户明确提供" },
    },
    preview: {
      label: "Ada",
      before: { display_name: "Ada", notes: "" },
      after: { display_name: "Ada Lovelace", notes: "用户明确提供" },
    },
    route: "",
  };
  state.load.mockResolvedValue([person]);
  state.decide.mockResolvedValue({
    ...person,
    status: "confirmed",
    can_confirm: false,
    result_id: actorId,
    result_version: 3,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={person.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("修改本地人员")).toBeInTheDocument();
  expect(screen.getByText(/不创建登录账号、不发送消息/)).toBeVisible();
  expect(
    screen.getByRole("button", { name: "打开人员与责任设置" }),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "确认执行" }));
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(person, "confirm"),
  );
  await waitFor(() => expect(state.invalidatePeople).toHaveBeenCalledOnce());
  expect(state.invalidateClientContacts).not.toHaveBeenCalled();
});

it("requires explicit permanent Task deletion consent and shows every local impact", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "task.delete",
      task_id: proposal.action.task_id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "删除这项任务",
      before: {},
      after: {
        task_deleted: true,
        child_tasks_moved_to_top_level: 2,
        submissions_deleted: 1,
        artifacts_deleted: 3,
        file_artifacts_deleted: 1,
        assignments_deleted: 1,
        agent_runs_deleted: 2,
        focus_sessions_detached: 4,
        inbox_relations_detached: 2,
        inbox_sources_coordinated: 3,
        tag_links_deleted: 2,
      },
    },
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.task_id,
    result_version: 2,
    route: "/tasks",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除任务")).toHaveLength(2);
  expect(screen.getByText(/直属子任务会移到顶层/)).toBeVisible();
  const agentRunImpact = screen.getByText("删除 Agent 执行历史");
  expect(agentRunImpact).toBeVisible();
  expect(agentRunImpact.nextElementSibling).toHaveTextContent("2");
  const confirm = screen.getByRole("button", { name: "确认永久删除" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对全部删除影响/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidate).toHaveBeenCalled());
});

it("requires explicit permanent Tag deletion consent and refreshes Tag and Task facts", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "tag.delete",
      tag_id: proposal.action.task_id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "旧标签",
      before: { name: "旧标签", color: "#445566", tag_version: 2 },
      after: { tag_deleted: true, detached_tasks: 4 },
    },
    route: "/tasks",
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.tag_id,
    result_version: 2,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("永久删除任务标签")).toBeVisible();
  expect(screen.getByText(/任务不会被删除或改变状态/)).toBeVisible();
  const impact = screen.getByText("解除关联的任务");
  expect(impact.nextElementSibling).toHaveTextContent("4");
  const confirm = screen.getByRole("button", { name: "确认永久删除标签" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对关联任务数量/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidate).toHaveBeenCalled());
  expect(state.invalidateFocus).toHaveBeenCalled();
});

it("requires explicit permanent Project deletion consent and shows every local impact", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "project.delete",
      project_id: proposal.action.task_id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "已归档项目",
      before: { status: "archived", project_version: 2 },
      after: {
        project_deleted: true,
        project_tasks_detached: 3,
        project_draft_invoices_detached: 1,
        project_financial_entries_detached: 2,
        project_notes_deleted: 4,
        project_attachments_deleted: 5,
        project_attachment_files_deleted: 2,
        project_inbox_sources_coordinated: 1,
      },
    },
    route: `/projects/${proposal.action.task_id}`,
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.project_id,
    result_version: 2,
    route: "/projects",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除项目")).toHaveLength(2);
  expect(screen.getByText(/项目笔记、附件记录/)).toBeVisible();
  const impact = screen.getByText("删除项目笔记");
  expect(impact.nextElementSibling).toHaveTextContent("4");
  const confirm = screen.getByRole("button", { name: "确认永久删除项目" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已核对全部删除和解除关联影响/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateProjects).toHaveBeenCalled());
});

it("requires explicit permanent Client deletion consent and shows every local impact", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "client.delete",
      client_id: proposal.action.task_id,
      expected_version: 6,
      changes: {},
    },
    preview: {
      label: "已停用客户",
      before: { status: "inactive", client_version: 6 },
      after: {
        client_deleted: true,
        client_projects_detached: 2,
        client_activities_deleted: 3,
        client_contact_history_deleted: 1,
        client_attachments_deleted: 2,
        client_attachment_files_deleted: 1,
      },
    },
    route: `/clients/${proposal.action.task_id}`,
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.client_id,
    result_version: 6,
    route: "/clients",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除客户")).toHaveLength(2);
  expect(screen.getByText(/客户活动、联系人关系历史/)).toBeVisible();
  const impact = screen.getByText("删除客户活动记录");
  expect(impact.nextElementSibling).toHaveTextContent("3");
  const confirm = screen.getByRole("button", { name: "确认永久删除客户" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /永久删除这个已停用客户/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateClients).toHaveBeenCalled());
});

it("requires explicit permanent Content Item deletion consent and shows relation impact", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "content_item.delete",
      content_item_id: proposal.action.task_id,
      expected_version: 4,
      changes: {},
    },
    preview: {
      label: "已归档内容",
      before: { status: "archived", content_item_version: 4 },
      after: {
        content_item_deleted: true,
        content_item_task_links_deleted: 2,
        content_item_inbox_sources_marked: 1,
      },
    },
    route: `/content-calendar?item=${proposal.action.task_id}`,
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.content_item_id,
    result_version: 4,
    route: "/content-calendar",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除内容条目")).toHaveLength(2);
  expect(screen.getByText(/关联项目与任务本身不会被删除/)).toBeVisible();
  const impact = screen.getByText("解除准备任务关系");
  expect(impact.nextElementSibling).toHaveTextContent("2");
  const confirm = screen.getByRole("button", { name: "确认永久删除内容" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /永久删除这个已归档内容条目/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateContent).toHaveBeenCalled());
});

it("requires independently verified publication before recording a Content Item as published", async () => {
  const publishing: AiActionProposal = {
    ...proposal,
    action: {
      action: "content_item.publish",
      content_item_id: proposal.action.task_id,
      expected_version: 2,
      changes: {
        published_at: "2026-09-18T20:00:00+08:00",
        external_link: "https://example.test/post",
      },
    },
    preview: {
      label: "发布说明",
      before: { status: "scheduled", published_at: null, external_link: null },
      after: {
        status: "published",
        published_at: "2026-09-18T12:00:00Z",
        external_link: "https://example.test/post",
      },
    },
    route: `/content-calendar?item=${proposal.action.task_id}`,
  };
  state.load.mockResolvedValue([publishing]);
  state.decide.mockResolvedValue({
    ...publishing,
    status: "confirmed",
    can_confirm: false,
    result_id: publishing.action.content_item_id,
    result_version: 3,
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={publishing.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("确认内容已发布")).toBeVisible();
  expect(screen.getByText(/应用不会发布、访问或验证外部链接/)).toBeVisible();
  expect(screen.getByText("2026-09-18T12:00:00Z")).toBeVisible();
  expect(screen.getByText("https://example.test/post")).toBeVisible();
  const confirm = screen.getByRole("button", { name: "确认记录已发布" });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /我已在外部平台独立核实/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(publishing, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateContent).toHaveBeenCalled());
});

it("shows the saved task view filters and requires separate deletion consent", async () => {
  const viewId = "00000000-0000-4000-8000-0000000000e1";
  const tagId = "00000000-0000-4000-8000-0000000000e2";
  const definition = {
    q: "",
    status: "active",
    priority: "P0",
    kind: "",
    project_id: "",
    client_id: "",
    tag_ids: [tagId],
    planned_date: "",
    planned_from: "2026-09-21",
    planned_to: "2026-09-27",
    due_from: "",
    due_to: "",
    sort: "-due_date",
  };
  const create: AiActionProposal = {
    ...proposal,
    id: "00000000-0000-4000-8000-0000000000e3",
    action: {
      action: "task_view.create",
      changes: {
        name: "本周 P0",
        definition: { ...definition, tag_ids: [...definition.tag_ids] },
      },
    },
    preview: {
      label: "本周 P0",
      before: {},
      after: { name: "本周 P0" },
      task_view: {
        name: "本周 P0",
        definition: { ...definition, tag_ids: [...definition.tag_ids] },
        next_version: 1,
        tag_names: ["Focus"],
      },
    },
    route: "/tasks",
  };
  const deletion: AiActionProposal = {
    ...proposal,
    id: "00000000-0000-4000-8000-0000000000e4",
    action: {
      action: "task_view.delete",
      task_saved_view_id: viewId,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "本周 P0",
      before: { name: "本周 P0" },
      after: {},
      task_view: {
        id: viewId,
        name: "本周 P0",
        definition: { ...definition, tag_ids: [...definition.tag_ids] },
        version: 2,
        deleted: true,
        tag_names: ["Focus"],
      },
    },
    route: "/tasks",
  };
  state.load.mockResolvedValue([create, deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: viewId,
    result_version: 2,
    route: "/tasks",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={create.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("保存任务视图")).toBeVisible();
  expect(screen.getByText("删除任务视图")).toBeVisible();
  expect(screen.getAllByText("优先级").length).toBeGreaterThan(0);
  expect(screen.getAllByText("计划范围").length).toBeGreaterThan(0);
  expect(screen.getAllByText(/2026-09-21 → 2026-09-27/).length).toBeGreaterThan(
    0,
  );
  expect(screen.getAllByText(/Focus/).length).toBeGreaterThan(0);
  const createButton = screen.getByRole("button", { name: "确认保存任务视图" });
  expect(createButton).toBeEnabled();
  const deleteButton = screen.getByRole("button", { name: "确认删除任务视图" });
  expect(deleteButton).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /同意删除这个任务保存视图/ }),
  );
  expect(deleteButton).toBeEnabled();
  fireEvent.click(deleteButton);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
});

it("shows the proposed knowledge text excerpt and confirms a source without extra consent", async () => {
  const content = "# 纪要\n\n讨论本地知识库只保存受管副本。\n";
  const bytes = new TextEncoder().encode(content).length;
  const sourceId = "00000000-0000-4000-8000-0000000000c1";
  const create: AiActionProposal = {
    ...proposal,
    id: "00000000-0000-4000-8000-0000000000c2",
    action: {
      action: "knowledge_source.create",
      changes: { name: "meeting-notes.md", title: "纪要", content },
    },
    preview: {
      label: "meeting-notes.md",
      before: {},
      after: {
        name: "meeting-notes.md",
        source_type: "markdown",
        status: "indexing",
        version: 1,
        size_bytes: bytes,
      },
      knowledge: {
        name: "meeting-notes.md",
        source_type: "markdown",
        status: "pending",
        version: 0,
        next_status: "indexing",
        next_version: 1,
        new_job_operation: "import",
        chunk_count: 0,
        document_count: 0,
        proposed_content_bytes: bytes,
        proposed_content_sha256: "c".repeat(64),
        proposed_excerpt: content,
        proposed_excerpt_truncated: false,
        content_included: false,
      },
    },
    route: "/knowledge",
  };
  state.load.mockResolvedValue([create]);
  state.decide.mockResolvedValue({
    ...create,
    status: "confirmed",
    can_confirm: false,
    result_id: sourceId,
    result_version: 1,
    route: "/knowledge",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={create.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("存入知识库")).toBeVisible();
  expect(screen.getByText(/讨论本地知识库只保存受管副本/)).toBeVisible();
  expect(screen.getByText(/不会扫描磁盘或读取本地文件/)).toBeVisible();
  const confirm = screen.getByRole("button", { name: "确认存入知识库" });
  expect(confirm).toBeEnabled();
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(create, "confirm", false),
  );
});

it("requires explicit knowledge source deletion consent and shows metadata-only scope", async () => {
  const sourceId = proposal.action.task_id!;
  const jobId = "00000000-0000-4000-8000-0000000000b2";
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "knowledge_source.delete",
      knowledge_source_id: sourceId,
      expected_version: 3,
      changes: { reason: "示例过期" },
    },
    preview: {
      label: "billing-guide.md",
      before: { status: "ready", version: 3, chunks: 4, documents: 1 },
      after: { status: "deleted", version: 4, chunks: 0, documents: 0 },
      knowledge: {
        source_id: sourceId,
        name: "billing-guide.md",
        source_type: "markdown",
        status: "ready",
        version: 3,
        next_status: "deleted",
        next_version: 4,
        chunk_count: 4,
        document_count: 1,
        content_included: false,
      },
    },
    route: "/knowledge",
  };
  const retry: AiActionProposal = {
    ...proposal,
    id: "00000000-0000-4000-8000-0000000000b3",
    action: {
      action: "knowledge_index_job.retry",
      knowledge_index_job_id: jobId,
      expected_version: 3,
      changes: {},
    },
    preview: {
      label: "billing-guide.md",
      before: {
        status: "ready",
        version: 3,
        chunks: 4,
        documents: 1,
        job_status: "failed",
        job_attempt: 1,
      },
      after: {
        status: "indexing",
        version: 4,
        chunks: 4,
        documents: 1,
        job_status: "queued",
      },
      knowledge: {
        source_id: sourceId,
        knowledge_index_job_id: jobId,
        name: "billing-guide.md",
        source_type: "markdown",
        status: "ready",
        version: 3,
        next_status: "indexing",
        next_version: 4,
        chunk_count: 4,
        document_count: 1,
        new_job_operation: "reindex",
        job: {
          id: jobId,
          operation: "reindex",
          status: "failed",
          attempt: 1,
          error_code: "KNOWLEDGE_INDEX_FAILED",
        },
        content_included: false,
      },
    },
    route: "/knowledge",
  };
  state.load.mockResolvedValue([deletion, retry]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: sourceId,
    result_version: 4,
    route: "/knowledge",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除知识库来源")).not.toHaveLength(0);
  expect(screen.getByText("重试知识库索引")).toBeVisible();
  expect(
    screen.getAllByText(/正文、提取文本和文件字节不会发送给模型/).length,
  ).toBeGreaterThan(0);
  const retryButton = screen.getByRole("button", { name: "确认执行" });
  expect(retryButton).toBeEnabled();
  const confirm = screen.getByRole("button", {
    name: "确认永久删除知识库来源",
  });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /永久删除该知识库来源/ }),
  );
  expect(confirm).toBeEnabled();
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
});

it("requires explicit roadmap milestone deletion consent and shows relation impact", async () => {
  const deletion: AiActionProposal = {
    ...proposal,
    action: {
      action: "roadmap_milestone.delete",
      roadmap_milestone_id: proposal.action.task_id,
      expected_version: 2,
      changes: {},
    },
    preview: {
      label: "已归档里程碑",
      before: { status: "archived", roadmap_milestone_version: 2 },
      after: {
        roadmap_milestone_deleted: true,
        roadmap_milestone_project_links_deleted: 2,
        roadmap_milestone_inbox_sources_marked: 1,
      },
    },
    route: `/roadmap?milestone=${proposal.action.task_id}`,
  };
  state.load.mockResolvedValue([deletion]);
  state.decide.mockResolvedValue({
    ...deletion,
    status: "confirmed",
    can_confirm: false,
    result_id: deletion.action.roadmap_milestone_id,
    result_version: 2,
    route: "/roadmap",
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <AiWorkspaceActions generationId={deletion.generation_id} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findAllByText("永久删除路线图里程碑")).toHaveLength(2);
  expect(screen.getByText(/关联项目与任务本身不会被删除/)).toBeVisible();
  const impact = screen.getByText("解除关联项目关系");
  expect(impact.nextElementSibling).toHaveTextContent("2");
  const confirm = screen.getByRole("button", {
    name: "确认永久删除里程碑",
  });
  expect(confirm).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /永久删除这个已归档路线图里程碑/ }),
  );
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(state.decide).toHaveBeenCalledWith(deletion, "confirm", true),
  );
  await waitFor(() => expect(state.invalidateRoadmap).toHaveBeenCalled());
});
