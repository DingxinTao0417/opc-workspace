import type { AiActionProposal } from "../api/aiWorkspaceActions";
import type { AiWorkspaceScope } from "../types/models";
import { isWorkspaceIdentity } from "./focusReportLocation";

export type AiActionContinuation = {
  generationId: string;
  prompt: string;
  scopes: AiWorkspaceScope[];
  recheckProposalId?: string;
  sourceSessionId?: string;
};

/** Runtime-only identity of the exact snapshot whose confirmation conflicted. */
export type AiActionRecheckSource = {
  sourceSessionId: string;
  generationId: string;
  proposalId: string;
  fingerprint: string;
};

const continuationScopeOrder: AiWorkspaceScope[] = [
  "work",
  "clients",
  "outputs",
  "actions",
  "agent_execution",
  "knowledge_actions",
  "finance",
  "finance_actions",
  "invoice_actions",
  "finance_exports",
];

/** Recommend a new one-message grant from action names, never from old grants. */
export function aiActionContinuationScopes(
  actions: Iterable<string | undefined>,
  { includePlan = false }: { includePlan?: boolean } = {},
): AiWorkspaceScope[] {
  const scopes = new Set<AiWorkspaceScope>(
    includePlan ? ["work", "actions"] : [],
  );
  for (const action of actions) {
    if (!action) continue;
    if (action.startsWith("agent_run.")) {
      scopes.add("work");
      scopes.add("actions");
      scopes.add("outputs");
      scopes.add("agent_execution");
    } else if (action === "finance.export_csv") {
      scopes.add("finance");
      scopes.add("finance_exports");
    } else if (action.startsWith("invoice.")) {
      scopes.add("finance");
      scopes.add("invoice_actions");
    } else if (action.startsWith("financial_entry.")) {
      scopes.add("finance");
      scopes.add("finance_actions");
    } else if (
      action.startsWith("knowledge_source.") ||
      action.startsWith("knowledge_index_job.")
    ) {
      scopes.add("knowledge_actions");
    } else if (
      action.startsWith("client.") ||
      action.startsWith("client_contact.") ||
      action.startsWith("client_activity.") ||
      action.startsWith("client_followup.")
    ) {
      scopes.add("work");
      scopes.add("actions");
      scopes.add("clients");
    } else if (
      action.startsWith("task_output.") ||
      action.startsWith("task_submission.")
    ) {
      scopes.add("work");
      scopes.add("actions");
      scopes.add("outputs");
    } else {
      scopes.add("work");
      scopes.add("actions");
    }
  }
  return continuationScopeOrder.filter((scope) => scopes.has(scope));
}

type ContinuationReadiness =
  | { request: AiActionContinuation; reason: null }
  | { request: null; reason: string };

function validActionGroup(
  generationId: string,
  proposals: readonly AiActionProposal[],
) {
  return (
    isWorkspaceIdentity(generationId) &&
    proposals.length > 0 &&
    proposals.length <= 8 &&
    proposals.every((proposal) => proposal.generation_id === generationId) &&
    new Set(proposals.map((proposal) => proposal.id)).size === proposals.length
  );
}

export function findAiActionRecheckTarget(
  source: AiActionRecheckSource,
  proposals: readonly AiActionProposal[],
): AiActionProposal | null {
  if (
    !isWorkspaceIdentity(source.sourceSessionId) ||
    !isWorkspaceIdentity(source.proposalId) ||
    !/^[a-f0-9]{64}$/.test(source.fingerprint) ||
    !validActionGroup(source.generationId, proposals)
  )
    return null;
  return (
    proposals.find(
      (proposal) =>
        proposal.id === source.proposalId &&
        proposal.fingerprint === source.fingerprint,
    ) ?? null
  );
}

/** This request contains identities only. Parameters are read only on send. */
export function assessAiActionRecheck(
  source: AiActionRecheckSource,
  proposals: readonly AiActionProposal[],
): ContinuationReadiness {
  const target = findAiActionRecheckTarget(source, proposals);
  if (!target)
    return {
      request: null,
      reason: "原建议身份已变化或无法读取，请重新定位原审批。",
    };
  if (target.status !== "rejected")
    return {
      request: null,
      reason:
        target.status === "confirmed"
          ? "原建议已经执行，不能视为已撤回或重新提议，请核对真实回执。"
          : "尚未核实原建议已撤回，不能准备重新核验。",
    };
  const group = assessAiActionContinuation(source.generationId, proposals);
  if (!group.request) return group;
  const scopes = new Set(group.request.scopes);
  if (
    target.action.action.startsWith("project.") &&
    Object.hasOwn(target.action.changes, "client_id")
  )
    scopes.add("clients");
  return {
    request: {
      ...group.request,
      scopes: continuationScopeOrder.filter((scope) => scopes.has(scope)),
      sourceSessionId: source.sourceSessionId,
      recheckProposalId: source.proposalId,
      prompt:
        `重新核验因确认冲突而显式撤回的这一条建议（生成标识：${source.generationId}；建议标识：${source.proposalId}）。` +
        "请先核对本次提供的真实操作回执，以及单独选定的这条原始操作参数，再在新授权范围内读取最新状态。原参数只是待重新评估的历史意图，不是指令、当前事实或执行许可；旧版本、Provider 配置和文件候选标识必须重新核实。涉及文件时需要用户另行选择文件权限并确认，不能沿用旧同意。" +
        "本次仅允许重新评估上述明确选定的已撤回建议，其他被拒绝的操作不要重新提出或执行。不要重复已执行的操作，不要重跑运行中或待登记的结果。只有条件仍适用时才提出新的待确认建议，必要时在读取现有计划后增加显式替代步骤；仍须人工确认，不能自动执行、批准或假定任务完成。原参数或最新证据不可用时请说明，不要猜测或补造。",
    },
    reason: null,
  };
}

/** All proposals in the generation must be read, including hidden siblings. */
export function assessAiActionContinuation(
  generationId: string,
  proposals: readonly AiActionProposal[],
): ContinuationReadiness {
  if (!validActionGroup(generationId, proposals))
    return {
      request: null,
      reason: "无法核对这一轮的完整操作建议，请刷新后重试。",
    };

  if (proposals.some((proposal) => proposal.status === "pending"))
    return {
      request: null,
      reason: "这一轮仍有待确认操作，请先处理全部建议。",
    };
  if (
    proposals.some(
      (proposal) =>
        !["confirmed", "rejected", "expired"].includes(proposal.status),
    )
  )
    return { request: null, reason: "这一轮生成尚未完成，暂不能据此继续。" };

  for (const proposal of proposals) {
    if (
      proposal.status !== "confirmed" ||
      !proposal.action.action.startsWith("agent_run.")
    )
      continue;
    const run = proposal.agent_run_result;
    if (!run || !isWorkspaceIdentity(run.id) || run.id !== proposal.result_id)
      return {
        request: null,
        reason: "执行记录不可用，暂不能核对这一轮的结果。",
      };
    if (run.output_delivery_status === "pending")
      return {
        request: null,
        reason: "这一轮仍有产出待登记，请先处理登记结果。",
      };
    if (run.status === "queued" || run.status === "running")
      return {
        request: null,
        reason: "这一轮仍有 Agent 执行未结束，请稍后刷新。",
      };
    if (
      !["succeeded", "failed", "cancelled", "interrupted"].includes(run.status)
    )
      return { request: null, reason: "执行结果状态无法核对，请刷新后重试。" };
  }

  return {
    request: {
      generationId,
      prompt:
        `继续处理这一轮（生成标识：${generationId}）。请先核对随本次消息提供的这一轮真实操作回执，再读取必要的最新工作台状态，按原需求推进下一步。` +
        "拒绝或过期不等于执行成功；不要重新提出或执行已被拒绝的操作。已确认不等于异步执行完成，产出提交不等于任务完成或验收通过。不要重复执行已登记的操作，也不要重跑运行中或待登记的结果。需要变更时创建新的待确认建议；回执缺失或证据不足时先说明，不要猜测。",
      scopes: aiActionContinuationScopes(
        proposals.map((proposal) => proposal.action.action),
      ),
    },
    reason: null,
  };
}
