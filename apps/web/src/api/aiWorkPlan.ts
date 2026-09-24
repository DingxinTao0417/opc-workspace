import { ApiError, apiRequest } from "./client";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { isAgentRunRoute, isAiWorkspaceRoute } from "../lib/aiWorkspaceLinks";
import { isTaskSubmissionRoute } from "../lib/taskSubmissionLocation";
import { aiActionLabels } from "./aiWorkspaceActions";
import type { AiWorkspaceScope } from "../types/models";
import { aiActionContinuationScopes } from "../lib/aiActionContinuation";

export const planStateLabels: Record<string, string> = {
  pending: "等待确认 · 未执行",
  child_pending_approval: "子会话待人工核对",
  completed: "子任务已完成",
  not_proposed: "尚未提出操作",
  in_progress: "分析中（模型报告）",
  reported_done: "分析完成（模型自报）",
  awaiting_continuation: "待继续 · 当前生成已结束",
  recorded: "命令已登记",
  rejected: "已拒绝 · 未执行",
  expired: "建议已过期",
  unavailable: "生成未完成 · 不可执行",
  evidence_unavailable: "执行证据不可读取",
  queued: "执行排队中",
  streaming: "子任务执行中",
  running: "执行中",
  output_pending: "结果待登记 · 勿重跑模型",
  submitted: "产出已提交 · 非任务完成",
  retained: "结果已保留 · 未提交",
  failed: "执行失败",
  cancelled: "执行已取消",
  interrupted: "执行已中断",
  succeeded: "自动化执行成功",
  download_unverified: "已批准导出 · 下载未核实",
};

export type AiAgentRunSubmissionStatus =
  "pending_review" | "accepted" | "changes_requested" | "withdrawn";

export const aiAgentRunSubmissionStatusLabels: Record<
  AiAgentRunSubmissionStatus,
  string
> = {
  pending_review: "待人工验收",
  accepted: "已接受",
  changes_requested: "已要求返工",
  withdrawn: "已撤回",
};

export interface AiWorkPlanStep {
  id: string;
  title: string;
  kind: "analysis" | "action";
  depends_on: string[];
  report?: "pending" | "in_progress" | "reported_done";
  proposal_id?: string;
  replaces?: string;
  superseded_by?: string;
  state: string;
  ready: boolean;
  satisfied: boolean;
  evidence?: {
    proposal_id: string;
    generation_id: string;
    action: string;
    status: string;
    result_id?: string;
    retry_of_run_id?: string;
    restart_of_run_id?: string;
    route?: string;
    submission_status?: AiAgentRunSubmissionStatus;
  };
}
export interface AiWorkPlan {
  session_id: string;
  version: number;
  generation_id: string;
  generation_status: string;
  title: string;
  created_at: string;
  as_of: string;
  /** Present only when the user explicitly closed this exact revision. */
  closed_at?: string;
  steps: AiWorkPlanStep[];
}

export function aiWorkPlanActiveSteps(plan: Pick<AiWorkPlan, "steps">) {
  return plan.steps.filter((step) => step.superseded_by === undefined);
}

export function aiWorkPlanRecordLinkLabel(step: AiWorkPlanStep): string {
  const route = step.evidence?.route;
  if (!route || !step.evidence?.action.startsWith("agent_run."))
    return "查看工作台记录";
  if (isTaskSubmissionRoute(route)) return "查看待验收提交";
  if (isAgentRunRoute(route)) return "查看 Agent 执行";
  return "查看工作台记录";
}

/**
 * A plan continuation always needs the plan tool's base capability. Once a
 * real proposal receipt is present, its action name gives us enough
 * metadata-only information to recommend the additional read/proposal scopes
 * needed for the next turn. This never grants anything or infers file access;
 * the user still confirms the one-message grant in the access dialog.
 */
export function aiWorkPlanContinuationScopes(
  plan: Pick<AiWorkPlan, "steps">,
): AiWorkspaceScope[] {
  return aiActionContinuationScopes(
    aiWorkPlanActiveSteps(plan).map((step) => step.evidence?.action),
    { includePlan: true },
  );
}

export const planInboxStateLabels = {
  running: "正在运行",
  needs_approval: "等待你的确认",
  needs_recovery: "结果待恢复",
  awaiting_continuation: "待继续",
  completed: "计划已完成",
  closed: "已由你关闭",
} as const;

export const aiWorkPlanContinuationPrompt =
  "继续当前执行计划。请先读取最新计划和真实工作台状态，只推进当前可继续的一步；需要操作时创建新的确认建议，不要把计划、历史回复或人工确认当成已执行证据。接续检查只代表当时的只读快照，不是执行许可；不要重复已经登记的操作，不要重跑运行中或待登记的结果。";

export const planContinuationReasons = {
  ready: "已核对当前计划及相关整轮回执；仍需重新授权并手动发送。",
  generation_active: "该会话仍有生成进行中，请等待结束后重新核对。",
  plan_complete: "当前计划步骤已满足，无需再次准备续办；这不代表任务已验收。",
  pending_approval: "计划相关的整轮建议中仍有待确认操作，请先处理全部建议。",
  run_active: "计划相关的整轮建议中仍有 Agent 执行未结束，请稍后重新核对。",
  output_pending:
    "计划相关的整轮建议中仍有产出待登记，请先处理登记结果，不要重跑模型。",
  evidence_unavailable: "计划或相关整轮的执行证据不可核验，暂不能准备续办。",
  generation_unavailable: "计划相关生成的状态不可核验，暂不能准备续办。",
  plan_closed:
    "你已关闭此计划，不能再继续它；如有新的工作，请在对话中明确要求智能体制定新计划。",
} as const;

export const planContinuationBlockerLabels = {
  evidence_unavailable: "证据异常",
  generation_unavailable: "生成异常",
  pending_approval: "待确认",
  output_pending: "结果待登记",
  run_active: "Agent 执行中",
} as const;

export interface AiWorkPlanContinuationBlocker {
  reason: keyof typeof planContinuationBlockerLabels;
  total: number;
  generation_ids: string[];
}

export interface AiWorkPlanContinuation {
  plan: AiWorkPlan;
  ready: boolean;
  reason: keyof typeof planContinuationReasons;
  blocking_generation_id?: string;
  /** Omitted only when paired with an older local sidecar. */
  blockers?: AiWorkPlanContinuationBlocker[];
}

export function parseAiWorkPlanContinuation(
  value: unknown,
  sessionId: string,
  expectedVersion: number,
): AiWorkPlanContinuation {
  if (
    !integerBetween(expectedVersion, 1, 128) ||
    !object(value) ||
    Object.keys(value).some(
      (key) =>
        ![
          "plan",
          "ready",
          "reason",
          "blocking_generation_id",
          "blockers",
        ].includes(key),
    ) ||
    typeof value.ready !== "boolean" ||
    typeof value.reason !== "string" ||
    !Object.hasOwn(planContinuationReasons, value.reason) ||
    value.ready !== (value.reason === "ready")
  )
    return invalid();
  const plan = parseAiWorkPlan(value.plan, sessionId);
  if (!plan || plan.version !== expectedVersion) return invalid();
  const activeSteps = aiWorkPlanActiveSteps(plan);
  const planSources = new Set([
    plan.generation_id,
    ...activeSteps.map((step) => step.evidence?.generation_id).filter(identity),
  ]);
  let blockers: AiWorkPlanContinuationBlocker[] | undefined;
  if (value.blockers !== undefined) {
    if (!Array.isArray(value.blockers) || value.blockers.length > 5)
      return invalid();
    blockers = [];
    const reasons = Object.keys(planContinuationBlockerLabels) as Array<
      keyof typeof planContinuationBlockerLabels
    >;
    let lastPriority = -1;
    const seenReasons = new Set<string>();
    for (const raw of value.blockers) {
      if (
        !object(raw) ||
        Object.keys(raw).some(
          (key) => !["reason", "total", "generation_ids"].includes(key),
        ) ||
        typeof raw.reason !== "string" ||
        !Object.hasOwn(planContinuationBlockerLabels, raw.reason) ||
        seenReasons.has(raw.reason) ||
        !integerBetween(raw.total, 1, 1000) ||
        !Array.isArray(raw.generation_ids) ||
        raw.generation_ids.length > 13 ||
        raw.generation_ids.some(
          (id) => !identity(id) || !planSources.has(id),
        ) ||
        new Set(raw.generation_ids).size !== raw.generation_ids.length ||
        (raw.total as number) < raw.generation_ids.length
      )
        return invalid();
      const reason = raw.reason as keyof typeof planContinuationBlockerLabels;
      const priority = reasons.indexOf(reason);
      if (priority <= lastPriority) return invalid();
      lastPriority = priority;
      seenReasons.add(reason);
      blockers.push({
        reason,
        total: raw.total as number,
        generation_ids: raw.generation_ids as string[],
      });
    }
    const primaryIsBlocker = Object.hasOwn(
      planContinuationBlockerLabels,
      value.reason,
    );
    if (
      primaryIsBlocker !== blockers.length > 0 ||
      (primaryIsBlocker && blockers[0]?.reason !== value.reason)
    )
      return invalid();
  }
  const complete = activeSteps.every((step) => step.ready && step.satisfied);
  if (value.reason === "plan_complete" && !complete) return invalid();
  if ((value.reason === "plan_closed") !== (plan.closed_at !== undefined))
    return invalid();
  if (
    value.blocking_generation_id !== undefined &&
    (!identity(value.blocking_generation_id) ||
      ["ready", "plan_complete", "generation_active"].includes(value.reason) ||
      !planSources.has(value.blocking_generation_id) ||
      (blockers !== undefined &&
        blockers.length > 0 &&
        !blockers[0].generation_ids.includes(value.blocking_generation_id)))
  )
    return invalid();
  // The server additionally checks hidden siblings. Do not trust an apparent
  // ready response if its visible plan alone already contradicts readiness.
  if (
    value.ready &&
    (plan.generation_status === "streaming" ||
      complete ||
      activeSteps.some(
        (step) =>
          step.kind === "action" &&
          ([
            "pending",
            "queued",
            "running",
            "output_pending",
            "evidence_unavailable",
            "unavailable",
          ].includes(step.state) ||
            (step.proposal_id && !step.evidence)),
      ))
  )
    return invalid();
  return {
    plan,
    ready: value.ready,
    reason: value.reason as AiWorkPlanContinuation["reason"],
    ...(value.blocking_generation_id !== undefined
      ? { blocking_generation_id: value.blocking_generation_id as string }
      : {}),
    ...(blockers !== undefined ? { blockers } : {}),
  };
}

export type AiWorkPlanInboxState = keyof typeof planInboxStateLabels;
export type AiWorkPlanInboxFilter = AiWorkPlanInboxState | "attention" | "all";

export interface AiWorkPlanInboxItem {
  session_id: string;
  session_title: string;
  session_updated_at: string;
  version: number;
  generation_id: string;
  generation_status: "streaming" | "completed" | "failed" | "cancelled";
  plan_title: string;
  state: AiWorkPlanInboxState;
  step_total: number;
  satisfied_step_total: number;
  superseded_step_total?: number;
  pending_approval_total: number;
  active_step_total: number;
  recovery_step_total: number;
  created_at: string;
  as_of: string;
}

export interface AiWorkPlanInboxMeta {
  state_filter: AiWorkPlanInboxFilter;
  total: number;
  attention_total: number;
  running_total: number;
  needs_approval_total: number;
  needs_recovery_total: number;
  awaiting_continuation_total: number;
  completed_total: number;
  /** Omitted only when paired with an older local sidecar. */
  closed_total?: number;
  has_more: boolean;
  next_offset: number | null;
  window_limited: boolean;
  as_of: string;
}

export interface AiWorkPlanInboxPage {
  data: AiWorkPlanInboxItem[];
  meta: AiWorkPlanInboxMeta;
}
const identity = (v: unknown): v is string =>
  typeof v === "string" && isWorkspaceIdentity(v);
const object = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === "object" && !Array.isArray(v);
const bounded = (v: unknown, n: number): v is string =>
  typeof v === "string" && v.trim().length > 0 && [...v].length <= n;
function invalid(): never {
  throw new ApiError("计划响应无效，不能据此判断完成。", {
    code: "INVALID_RESPONSE",
  });
}

const integerBetween = (value: unknown, min: number, max: number) =>
  Number.isInteger(value) &&
  (value as number) >= min &&
  (value as number) <= max;

function expectedPlanInboxState(
  item: Pick<
    AiWorkPlanInboxItem,
    | "step_total"
    | "satisfied_step_total"
    | "superseded_step_total"
    | "pending_approval_total"
    | "active_step_total"
    | "recovery_step_total"
    | "generation_status"
  >,
): AiWorkPlanInboxState {
  if (
    item.satisfied_step_total + (item.superseded_step_total ?? 0) ===
    item.step_total
  )
    return "completed";
  if (item.recovery_step_total > 0) return "needs_recovery";
  if (item.pending_approval_total > 0) return "needs_approval";
  if (item.active_step_total > 0 || item.generation_status === "streaming")
    return "running";
  return "awaiting_continuation";
}

/** Current intent only; superseded receipts remain visible as history. */
export function aiWorkPlanProgress(
  plan: Pick<AiWorkPlan, "steps" | "generation_status">,
) {
  const steps = aiWorkPlanActiveSteps(plan);
  const counts = {
    step_total: plan.steps.length,
    superseded_step_total: plan.steps.length - steps.length,
    satisfied_step_total: steps.filter((step) => step.ready && step.satisfied)
      .length,
    pending_approval_total: steps.filter(
      (step) =>
        step.kind === "action" &&
        ["pending", "child_pending_approval"].includes(step.state),
    ).length,
    active_step_total: steps.filter(
      (step) =>
        step.kind === "action" && ["queued", "running"].includes(step.state),
    ).length,
    recovery_step_total: steps.filter(
      (step) =>
        step.kind === "action" &&
        ["output_pending", "retained"].includes(step.state),
    ).length,
  };
  return {
    ...counts,
    effective_step_total: steps.length,
    state: expectedPlanInboxState({
      ...counts,
      generation_status:
        plan.generation_status as AiWorkPlanInboxItem["generation_status"],
    }),
  };
}

export function parseAiWorkPlanInbox(
  value: unknown,
  expectedFilter: AiWorkPlanInboxFilter,
): AiWorkPlanInboxPage {
  if (!object(value) || !Array.isArray(value.data) || !object(value.meta))
    return invalid();
  const meta = value.meta;
  const closedTotal = meta.closed_total ?? 0;
  const counts = [
    meta.total,
    meta.attention_total,
    meta.running_total,
    meta.needs_approval_total,
    meta.needs_recovery_total,
    meta.awaiting_continuation_total,
    meta.completed_total,
    closedTotal,
  ];
  const filteredTotal =
    expectedFilter === "all"
      ? (meta.attention_total as number) +
        (meta.completed_total as number) +
        (closedTotal as number)
      : expectedFilter === "closed"
        ? (closedTotal as number)
        : expectedFilter === "attention"
          ? (meta.attention_total as number)
          : expectedFilter === "running"
            ? (meta.running_total as number)
            : expectedFilter === "needs_approval"
              ? (meta.needs_approval_total as number)
              : expectedFilter === "needs_recovery"
                ? (meta.needs_recovery_total as number)
                : expectedFilter === "awaiting_continuation"
                  ? (meta.awaiting_continuation_total as number)
                  : (meta.completed_total as number);
  if (
    meta.state_filter !== expectedFilter ||
    !counts.every((count) => integerBetween(count, 0, 1000)) ||
    meta.attention_total !==
      (meta.running_total as number) +
        (meta.needs_approval_total as number) +
        (meta.needs_recovery_total as number) +
        (meta.awaiting_continuation_total as number) ||
    (meta.attention_total as number) +
      (meta.completed_total as number) +
      (closedTotal as number) >
      1000 ||
    meta.total !== filteredTotal ||
    typeof meta.has_more !== "boolean" ||
    typeof meta.window_limited !== "boolean" ||
    (meta.next_offset !== null && !integerBetween(meta.next_offset, 1, 999)) ||
    meta.has_more !== (meta.next_offset !== null) ||
    typeof meta.as_of !== "string" ||
    !Number.isFinite(Date.parse(meta.as_of)) ||
    value.data.length > 50 ||
    value.data.length > (meta.total as number)
  )
    return invalid();
  const items: AiWorkPlanInboxItem[] = [];
  const sessions = new Set<string>();
  for (const raw of value.data) {
    if (
      !object(raw) ||
      !identity(raw.session_id) ||
      sessions.has(raw.session_id) ||
      !bounded(raw.session_title, 200) ||
      !bounded(raw.plan_title, 200) ||
      !identity(raw.generation_id) ||
      !integerBetween(raw.version, 1, 128) ||
      !["streaming", "completed", "failed", "cancelled"].includes(
        String(raw.generation_status),
      ) ||
      !Object.hasOwn(planInboxStateLabels, String(raw.state)) ||
      !integerBetween(raw.step_total, 1, 12) ||
      ("superseded_step_total" in raw &&
        !integerBetween(
          raw.superseded_step_total,
          0,
          (raw.step_total as number) - 1,
        )) ||
      !integerBetween(raw.satisfied_step_total, 0, raw.step_total as number) ||
      !integerBetween(
        raw.pending_approval_total,
        0,
        raw.step_total as number,
      ) ||
      !integerBetween(raw.active_step_total, 0, raw.step_total as number) ||
      !integerBetween(raw.recovery_step_total, 0, raw.step_total as number) ||
      typeof raw.session_updated_at !== "string" ||
      !Number.isFinite(Date.parse(raw.session_updated_at)) ||
      typeof raw.created_at !== "string" ||
      !Number.isFinite(Date.parse(raw.created_at)) ||
      typeof raw.as_of !== "string" ||
      !Number.isFinite(Date.parse(raw.as_of))
    )
      return invalid();
    const item = raw as unknown as AiWorkPlanInboxItem;
    const effectiveTotal = item.step_total - (item.superseded_step_total ?? 0);
    if (
      item.satisfied_step_total +
        item.pending_approval_total +
        item.active_step_total +
        item.recovery_step_total >
        effectiveTotal ||
      // Closure is a server-owned user fact; counts cannot reproduce it.
      (item.state !== "closed" &&
        item.state !== expectedPlanInboxState(item)) ||
      (expectedFilter === "attention" &&
        (item.state === "completed" || item.state === "closed")) ||
      (expectedFilter !== "all" &&
        expectedFilter !== "attention" &&
        item.state !== expectedFilter)
    )
      return invalid();
    sessions.add(item.session_id);
    items.push(item);
  }
  return { data: items, meta: meta as unknown as AiWorkPlanInboxMeta };
}

export function parseAiWorkPlan(
  value: unknown,
  sessionId: string,
): AiWorkPlan | null {
  if (value === null) return null;
  if (
    !object(value) ||
    value.session_id !== sessionId ||
    !isWorkspaceIdentity(sessionId) ||
    !identity(value.generation_id) ||
    !Number.isInteger(value.version) ||
    (value.version as number) < 1 ||
    (value.version as number) > 128 ||
    !["streaming", "completed", "failed", "cancelled"].includes(
      String(value.generation_status),
    ) ||
    !bounded(value.title, 200) ||
    !Array.isArray(value.steps) ||
    value.steps.length < 1 ||
    value.steps.length > 12 ||
    typeof value.created_at !== "string" ||
    !Number.isFinite(Date.parse(value.created_at)) ||
    typeof value.as_of !== "string" ||
    !Number.isFinite(Date.parse(value.as_of)) ||
    ("closed_at" in value &&
      (typeof value.closed_at !== "string" ||
        !Number.isFinite(Date.parse(value.closed_at))))
  )
    return invalid();
  const satisfied = new Map<string, boolean>();
  const steps = new Map<string, Record<string, unknown>>();
  const replacements = new Map<string, string>();
  const requiredRetryRuns = new Map<
    string,
    { runId: string; restartOnly: boolean }
  >();
  const proposals = new Set<string>();
  let active = 0;
  for (const step of value.steps) {
    if (
      !object(step) ||
      typeof step.id !== "string" ||
      !/^[a-z][a-z0-9_-]{0,39}$/.test(step.id) ||
      satisfied.has(step.id) ||
      !bounded(step.title, 300) ||
      !Array.isArray(step.depends_on) ||
      !step.depends_on.every(
        (dep) => typeof dep === "string" && satisfied.has(dep),
      ) ||
      new Set(step.depends_on).size !== step.depends_on.length ||
      typeof step.ready !== "boolean" ||
      typeof step.satisfied !== "boolean" ||
      typeof step.state !== "string" ||
      !Object.hasOwn(planStateLabels, step.state) ||
      step.ready !== step.depends_on.every((dep) => satisfied.get(dep))
    )
      return invalid();
    if (
      step.superseded_by === undefined &&
      step.depends_on.some((dep) => steps.get(dep)?.superseded_by !== undefined)
    )
      return invalid();
    if (step.kind === "analysis") {
      if (
        !["pending", "in_progress", "reported_done"].includes(
          String(step.report),
        ) ||
        "proposal_id" in step ||
        "evidence" in step ||
        "replaces" in step ||
        "superseded_by" in step ||
        step.satisfied !== (step.report === "reported_done") ||
        step.state !==
          (step.report === "in_progress" &&
          value.generation_status !== "streaming"
            ? "awaiting_continuation"
            : step.report)
      )
        return invalid();
      if (step.report === "in_progress") active++;
    } else if (step.kind === "action") {
      if ("report" in step) return invalid();
      if (
        "superseded_by" in step &&
        (typeof step.superseded_by !== "string" ||
          !/^[a-z][a-z0-9_-]{0,39}$/.test(step.superseded_by))
      )
        return invalid();
      if (step.proposal_id === undefined) {
        if (
          step.state !== "not_proposed" ||
          step.satisfied ||
          "evidence" in step
        )
          return invalid();
      } else {
        if (!identity(step.proposal_id) || proposals.has(step.proposal_id))
          return invalid();
        proposals.add(step.proposal_id);
        const e = step.evidence;
        if (e === undefined) {
          if (step.state !== "evidence_unavailable" || step.satisfied)
            return invalid();
        } else {
          if (
            !object(e) ||
            e.proposal_id !== step.proposal_id ||
            !identity(e.generation_id) ||
            !bounded(e.action, 80) ||
            !Object.hasOwn(aiActionLabels, e.action) ||
            ![
              "pending",
              "confirmed",
              "rejected",
              "expired",
              "unavailable",
            ].includes(String(e.status)) ||
            (e.result_id !== undefined && !identity(e.result_id)) ||
            ("retry_of_run_id" in e &&
              (e.action !== "agent_run.retry" ||
                !identity(e.retry_of_run_id) ||
                e.retry_of_run_id === e.result_id)) ||
            ("restart_of_run_id" in e &&
              (e.action !== "agent_run.start" ||
                "retry_of_run_id" in e ||
                !identity(e.restart_of_run_id) ||
                e.restart_of_run_id === e.result_id)) ||
            (e.route !== undefined &&
              (typeof e.route !== "string" || !isAiWorkspaceRoute(e.route))) ||
            (e.submission_status !== undefined &&
              (![
                "pending_review",
                "accepted",
                "changes_requested",
                "withdrawn",
              ].includes(String(e.submission_status)) ||
                !e.action.startsWith("agent_run.") ||
                step.state !== "submitted"))
          )
            return invalid();
          if (e.status !== "confirmed") {
            if (
              step.state !== e.status ||
              step.satisfied ||
              e.result_id !== undefined
            )
              return invalid();
          } else {
            if (!identity(e.result_id)) return invalid();
            const agent = e.action.startsWith("agent_run.");
            const automation = e.action === "automation.retry";
            const delegation =
              e.action === "agent_delegate.spawn" ||
              e.action === "agent_delegate.followup";
            const states = agent
              ? [
                  "queued",
                  "running",
                  "output_pending",
                  "submitted",
                  "retained",
                  "failed",
                  "cancelled",
                  "interrupted",
                  "evidence_unavailable",
                ]
              : automation
                ? ["succeeded", "failed", "evidence_unavailable"]
                : delegation
                  ? [
                      "queued",
                      "streaming",
                      "child_pending_approval",
                      "completed",
                      "failed",
                      "cancelled",
                      "evidence_unavailable",
                    ]
                  : e.action === "finance.export_csv"
                    ? ["download_unverified"]
                    : ["recorded"];
            const done =
              step.state === "recorded" ||
              (delegation && step.state === "completed") ||
              step.state === "submitted" ||
              step.state === "succeeded" ||
              (step.state === "cancelled" &&
                ["agent_run.cancel", "agent_run.cancel_many"].includes(
                  e.action,
                ));
            if (!states.includes(step.state) || step.satisfied !== done)
              return invalid();
          }
        }
      }
      if ("replaces" in step) {
        if (typeof step.replaces !== "string") return invalid();
        const previous = steps.get(step.replaces);
        if (
          !previous ||
          previous.kind !== "action" ||
          replacements.has(step.replaces) ||
          !object(previous.evidence) ||
          previous.satisfied !== false
        )
          return invalid();
        const source = previous.evidence;
        let requiredRun: string | undefined;
        let restartOnly = false;
        if (
          source.status === "confirmed" &&
          ["agent_run.start", "agent_run.retry"].includes(
            String(source.action),
          ) &&
          ["failed", "cancelled", "interrupted", "retained"].includes(
            String(previous.state),
          ) &&
          identity(source.result_id)
        ) {
          requiredRun = source.result_id;
          restartOnly = previous.state === "retained";
        } else if (
          ["rejected", "expired"].includes(String(previous.state)) &&
          source.status === previous.state
        ) {
          // A rejected retry does not erase the obligation to retry the
          // original failed Run. Only ordinary unexecuted intents may be
          // replaced by unrelated actions.
          const inherited = requiredRetryRuns.get(step.replaces);
          requiredRun =
            inherited?.runId ??
            (source.action === "agent_run.start" &&
            identity(source.restart_of_run_id)
              ? source.restart_of_run_id
              : undefined);
          // An unconfirmed restart does not prove the source was retained.
          // Keep its exact ID; only an observed retained ancestor forbids retry.
          restartOnly = inherited?.restartOnly ?? false;
        } else return invalid();
        if (requiredRun !== undefined) {
          if (
            !object(step.evidence) ||
            !(
              (step.evidence.action === "agent_run.retry" &&
                !restartOnly &&
                step.evidence.retry_of_run_id === requiredRun) ||
              (step.evidence.action === "agent_run.start" &&
                step.evidence.restart_of_run_id === requiredRun)
            )
          )
            return invalid();
          // This validates the metadata relationship, not the frozen Run
          // identity or delivery state, which remain server-owned checks.
          requiredRetryRuns.set(step.id, { runId: requiredRun, restartOnly });
        }
        replacements.set(step.replaces, step.id);
      }
    } else return invalid();
    satisfied.set(step.id, step.ready && step.satisfied);
    steps.set(step.id, step);
  }
  for (const [id, step] of steps) {
    if (
      replacements.has(id)
        ? step.superseded_by !== replacements.get(id)
        : "superseded_by" in step
    )
      return invalid();
  }
  if (active > 1) return invalid();
  return value as unknown as AiWorkPlan;
}

export async function getAiWorkPlan(
  sessionId: string,
  signal?: AbortSignal,
  version?: number,
) {
  if (
    !isWorkspaceIdentity(sessionId) ||
    (version !== undefined &&
      (!Number.isInteger(version) || version < 1 || version > 128))
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/sessions/${sessionId}/plan${version ? `?version=${version}` : ""}`,
    { signal },
  );
  const plan = parseAiWorkPlan(response.data, sessionId);
  if (version && (!plan || plan.version !== version)) return invalid();
  return plan;
}

export async function getAiWorkPlanContinuation(
  sessionId: string,
  expectedVersion: number,
  signal?: AbortSignal,
): Promise<AiWorkPlanContinuation> {
  if (
    !isWorkspaceIdentity(sessionId) ||
    !integerBetween(expectedVersion, 1, 128)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/sessions/${sessionId}/plan/continuation?expected_version=${expectedVersion}`,
    { signal },
  );
  return parseAiWorkPlanContinuation(response.data, sessionId, expectedVersion);
}

/**
 * Closes one exact plan revision after the user confirms it. The server
 * refuses while approvals, executions, recovery, a generation or a background
 * continuation are still open; closing never undoes or cancels anything.
 */
export async function closeAiWorkPlan(
  sessionId: string,
  expectedVersion: number,
): Promise<AiWorkPlan> {
  if (
    !isWorkspaceIdentity(sessionId) ||
    !integerBetween(expectedVersion, 1, 128)
  )
    return invalid();
  const response = await apiRequest<{ data: unknown }>(
    `/api/v1/ai/sessions/${sessionId}/plan/close`,
    {
      method: "POST",
      body: JSON.stringify({
        expected_version: expectedVersion,
        confirm_close: true,
      }),
    },
  );
  const plan = parseAiWorkPlan(response.data, sessionId);
  if (!plan || plan.version !== expectedVersion || !plan.closed_at)
    return invalid();
  return plan;
}

export async function getAiWorkPlanInbox(
  state: AiWorkPlanInboxFilter = "attention",
  limit = 20,
  offset = 0,
  signal?: AbortSignal,
): Promise<AiWorkPlanInboxPage> {
  if (
    !["all", "attention", ...Object.keys(planInboxStateLabels)].includes(
      state,
    ) ||
    !integerBetween(limit, 1, 50) ||
    !integerBetween(offset, 0, 999)
  )
    return invalid();
  const params = new URLSearchParams({
    state,
    limit: String(limit),
    offset: String(offset),
  });
  const response = await apiRequest<unknown>(
    `/api/v1/ai/work-plans?${params}`,
    { signal },
  );
  return parseAiWorkPlanInbox(response, state);
}
