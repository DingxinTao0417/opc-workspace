import { useEffect, useRef, useState } from "react";
import { isTaskOutputAction } from "../api/aiTaskOutputActions";
import { AiTaskOutputDetails } from "./AiTaskOutputDetails";
import { AiAutomationRetryDetails } from "./AiAutomationRetryDetails";
import { AiFinanceExportDetails } from "./AiFinanceExportDetails";
import { AiInvoicePdfResult } from "./AiInvoicePdfResult";
import { AiAgentRunStartDetails } from "./AiAgentRunStartDetails";
import { financialMinorDisplay } from "../api/aiFinancialActions";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import { agentRunHref, aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import { taskSubmissionHref } from "../lib/taskSubmissionLocation";
import { agentRunDeliveryReason } from "../lib/agentRunDelivery";
import {
  agentRunLifecycle,
  type AgentRunLifecycleInput,
  type AgentRunSubmissionReviewStatus,
} from "../lib/agentRunLifecycle";
import type { AgentRunStartGateCloseReason } from "../types/models";
import { agentReworkErrorMessage } from "../lib/agentRunRework";
import { agentRestartErrorMessage } from "../lib/agentRunRestart";
import {
  assessAiActionContinuation,
  assessAiActionRecheck,
  findAiActionRecheckTarget,
  type AiActionContinuation,
  type AiActionRecheckSource,
} from "../lib/aiActionContinuation";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import {
  aiActionFields,
  aiActionLabels,
  decideAiAgentRun,
  decideAiAgentDelegation,
  confirmAiAgentDelegationBatch,
  decideAiAgentFollowup,
  decideAiAgentRunCancel,
  decideAiAgentRunRecovery,
  needsAutomationEffectsConsent,
  decideAiWorkspaceAction,
  decideAiTaskOutput,
  decideAiTaskDelete,
  decideAiProjectDelete,
  decideAiClientDelete,
  decideAiContentItemDelete,
  decideAiContentPublished,
  decideAiRoadmapMilestoneDelete,
  decideAiTagDelete,
  decideAiKnowledgeAction,
  decideAiTaskViewAction,
  decideAiProjectNote,
  decideAiFinanceExport,
  getAiWorkspaceActions,
  type AiActionProposal,
} from "../api/aiWorkspaceActions";
import {
  invalidateTaskAggregates,
  invalidateTaskOutputActionFacts,
  invalidateProjectActionFacts,
  invalidateRoadmapActionFacts,
  invalidateContentItemActionFacts,
  invalidateProjectNoteActionFacts,
  invalidateInboxActionFacts,
  invalidateInboxReadAllFacts,
  invalidateReminderActionFacts,
  invalidateFocusActionFacts,
  invalidateClientFollowupActionFacts,
  invalidateClientActivityActionFacts,
  invalidateClientActionFacts,
  invalidateClientContactActionFacts,
  invalidatePersonActionFacts,
  invalidateAutomationActionFacts,
  invalidateFinancialActionFacts,
  invalidateInvoiceActionFacts,
  useAgentRunQuery,
  useRetryAgentRunOutputDelivery,
} from "../api/hooks";

const statuses = {
  pending: "等待确认 · 尚未执行",
  confirmed: "已执行",
  rejected: "已拒绝 · 未执行",
  expired: "已过期 · 未执行",
  unavailable: "生成未完成 · 不可执行",
};

function actionCardStatus(
  proposal: Pick<AiActionProposal, "status" | "result_id">,
) {
  // A confirmed proposal without the server-issued result identity is not
  // proof that the domain transaction completed. Keep the card honest while
  // the action list is re-read (or the user refreshes the conversation).
  if (proposal.status === "confirmed" && !proposal.result_id) {
    return "已确认 · 结果待回读";
  }
  return statuses[proposal.status];
}

function agentRunCardStatus(
  status: string | undefined,
  deliveryStatus: string | undefined,
  submissionId: string | null,
  submissionStatus: AgentRunSubmissionReviewStatus | undefined,
  startGate: AgentRunLifecycleInput["startGate"],
) {
  if (status === undefined) return "已启动 · 状态待刷新";
  const lifecycle = agentRunLifecycle({
    status,
    outputDeliveryStatus: deliveryStatus,
    submissionId,
    submissionStatus,
    startGate,
  });
  if (startGate?.status === "waiting" && lifecycle.phase === "pending")
    return `已确认 · ${lifecycle.label}`;
  switch (lifecycle.phase) {
    case "pending":
    case "running":
      return `已启动 · ${lifecycle.label}`;
    case "failed":
    case "cancelled":
      return `${lifecycle.label} · 未提交产出`;
    default:
      return lifecycle.label;
  }
}

function agentDelegationCardStatus(status: string | undefined) {
  if (status === "queued") return "已确认 · 等待 Provider 空闲";
  if (status === "streaming") return "子智能体执行中";
  if (status === "completed") return "子智能体已完成回复";
  if (status === "failed") return "子智能体启动或执行失败";
  if (status === "cancelled") return "子智能体执行已取消";
  if (status === "unavailable") return "子会话已不可用 · 无法查看结果";
  return "已确认 · 正在登记子会话";
}
const values: Record<string, string> = {
  pending_review: "待验收",
  accepted: "已接受",
  changes_requested: "要求返工",
  child_rollup: "子任务汇总",
  draft: "草稿",
  sent: "已发送（本地记录）",
  viewed: "已查看（本地记录）",
  paid: "已收款（本地记录）",
  overdue: "逾期",
  income: "收入",
  expense: "支出",
  pending: "待确认",
  confirmed: "已确认（本地记录）",
  voided: "已作废（排除统计）",
  note: "备注",
  meeting: "会议",
  visible: "显示中",
  deleted: "已删除（历史保留）",
  skipped: "已跳过",
  low: "低",
  normal: "普通",
  high: "高",
  idle: "空闲",
  recovery_pending: "待确认中断间隔",
  interrupted: "已中断（不计入工时）",
  active: "进行中",
  planned: "已规划",
  achieved: "已达成",
  in_review: "待审核",
  published: "已发布",
  scheduled: "待触发",
  fired: "已触发",
  open: "待处理",
  tracking: "跟进中",
  resolved: "已解决",
  dismissed: "已忽略",
  read: "已读",
  unread: "未读",
  linked: "已关联",
  unlinked: "未关联",
  planning: "规划中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
  todo: "待办",
  in_progress: "进行中",
  blocked: "已阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
  none: "无需验收",
  manual: "人工验收",
  work: "工作",
  review: "审核",
  followup: "跟进",
  reminder: "提醒",
};
function display(
  value: string | number | boolean | null | string[],
  field: string,
) {
  if (Array.isArray(value)) return value.length ? value.join("、") : "无";
  if (field === "role") return value === "reviewer" ? "验收人" : "负责人";
  if (field === "actor_type" && value !== null)
    return (
      (
        {
          owner: "本人",
          person: "本地人员",
          agent: "Agent（仅分派）",
        } as Record<string, string>
      )[String(value)] ?? String(value)
    );
  if (field === "pdf_generated") return value ? "确认后生成" : "否";
  if (field === "published_at" && value === "confirmation_time")
    return "由你确认时的时间（UTC）";
  if (field === "pdf_replaced")
    return value ? "是（旧文件将被替换）" : "首次生成";
  if (field === "invoice_deleted") return value ? "是（无撤销）" : "否";
  if (field === "pdf_removed")
    return value ? "是（仅此发票受控文件）" : "无已存 PDF";
  if (field === "rule_enabled") return value ? "启用" : "停用";
  if (field === "available") return value ? "可用" : "不可用";
  if (field === "parent_rollup_gates_ready") return value ? "已满足" : "未满足";
  if (field === "will_request_parent_review") return value ? "是" : "否";
  if (field === "trigger_type")
    return value === "schedule" ? "本地时间" : "本地事件";
  if (field === "action_type")
    return (
      (
        { inbox_item: "收件箱事项", task: "任务", reminder: "提醒" } as Record<
          string,
          string
        >
      )[String(value)] ?? String(value)
    );
  if (value === null || value === "") return "未设置";
  if (field === "client_status" || field === "assigned_actor_status")
    return (
      (
        { active: "启用", inactive: "停用", lead: "潜在客户" } as Record<
          string,
          string
        >
      )[String(value)] ?? String(value)
    );
  if (field === "assigned_actor_type")
    return value === "owner" ? "所有者" : "本地人员";
  if (field === "is_required") return value ? "是" : "否";
  if (field === "recovery_action")
    return (
      (
        {
          include_gap_resume: "计入中断间隔并继续",
          exclude_gap_resume: "排除中断间隔并继续",
          interrupt: "按最后心跳结束为中断",
        } as Record<string, string>
      )[String(value)] ?? String(value)
    );
  if (field === "resolution_mode" && value === "forced")
    return "人工例外强制解决";
  if (field === "recurrence_type")
    return (
      (
        {
          none: "一次性",
          daily: "按天",
          weekly: "按周",
          weekdays: "按工作日（周一至周五）",
          monthly: "按月",
        } as Record<string, string>
      )[String(value)] ?? String(value)
    );
  if (field === "resolution_policy")
    return value === "all_required_tasks_done"
      ? "必需任务全部完成后自动解决"
      : "手工解决";
  return [
    "type",
    "status",
    "task_status",
    "relation_state",
    "kind",
    "activity_state",
    "note_state",
    "project_status",
    "review_policy",
    "submission_status",
    "submission_origin",
    "read_state",
    "priority",
  ].includes(field)
    ? (values[String(value)] ?? String(value))
    : String(value);
}
function agentDelegationQueueHint(error: unknown): string | null {
  if (!(error instanceof ApiError)) return null;
  if (error.code === "AI_DELEGATION_QUEUE_FULL")
    return "智能体任务队列已满，本次确认未生效，也没有为此请求创建子会话。正在重新读取提案状态；若仍显示待确认，等运行中的子任务释放容量后可直接再次确认，无需重新生成建议。";
  if (error.code === "AI_DELEGATION_QUEUE_UNAVAILABLE")
    return "智能体调度器暂不可用，本次确认未生效。正在重新读取提案状态；若仍显示待确认，稍后可在此重试，无需重新生成建议。";
  return null;
}

function errorHint(error: unknown, target: string) {
  const delegationQueueHint = agentDelegationQueueHint(error);
  if (delegationQueueHint) return delegationQueueHint;
  if (error instanceof ApiError && error.code === "AGENT_RUN_QUEUE_FULL")
    return "Agent 执行队列已满，本次确认未生效，任务/审批状态没有改变。等待已有执行释放容量后，可直接重新确认，无需重新生成建议。";
  if (error instanceof ApiError && error.code?.startsWith("AGENT_")) {
    const reworkMessage =
      agentRestartErrorMessage(error) ?? agentReworkErrorMessage(error);
    if (reworkMessage) return reworkMessage;
  }
  if (error instanceof ApiError) {
    if (error.code === "TASK_REVIEW_POLICY_LOCKED")
      return "任务已不处于待办或已有提交历史，不能再切换验收方式。请查看任务当前状态，不会自动重试或强制修改。";
    if (error.code === "PROJECT_NOTE_DELETE_CONFIRMATION_REQUIRED")
      return "请核对完整笔记，并勾选软删除确认。";
    if (
      error.code === "PROJECT_NOTE_DELETED" ||
      error.code === "PROJECT_NOTE_NOT_FOUND"
    )
      return "项目笔记已删除或不存在，旧建议未执行，请重新查询。";
    if (error.code === "AI_TASK_OUTPUT_CONFIRMATION_REQUIRED")
      return "请先核实完整产出、责任归属和验收证据，单独勾选后再确认。";
    if (error.code === "AI_OUTPUT_PREVIEW_TOO_LARGE")
      return "完整证据超出确认卡容量，请在任务详情中处理，不会截断证据后执行。";
    if (
      [
        "TASK_REVIEW_NOT_ALLOWED",
        "TASK_SUBMISSION_INVALID",
        "TASK_SUBMISSION_ALREADY_PENDING",
      ].includes(error.code ?? "")
    )
      return "提交批次或验收状态已变化，旧建议未执行。请重新查询当前批次，再决定提交或验收。";
    if (error.code === "TASK_REVIEWER_REQUIRED")
      return "请先设置当前可用的所有者验收人，再重新提议；智能体不能代替验收人。";
    if (error.code === "TASK_MANUAL_REVIEW_REQUIRED")
      return "该任务没有启用人工验收，不能提交或审核产出；请在任务详情核对验收策略。";
    if (error.code === "TASK_SUBMISSION_NOT_ALLOWED")
      return "任务已不是待办或进行中，不能按旧建议提交产出。请重新读取任务状态。";
    if (error.code === "FINANCE_EXPORT_CONFIRMATION_REQUIRED")
      return "请核对导出范围、完整记录数和敏感字段，并单独勾选同意。";
    if (target === "财务导出" && error.code === "AI_ACTION_PREVIEW_CHANGED")
      return "导出数据或关联资料已变化，旧范围未批准。请重新提议并核对完整导出内容。";
    if (target === "财务导出" && error.code === "EXPORT_TOO_LARGE")
      return "匹配内容超出 10000 条或 16 MiB，请缩小范围后重新提议，不会导出截断文件。";
    if (error.code === "INVOICE_PDF_CONFIRMATION_REQUIRED")
      return "请核对完整发票，并单独勾选生成或替换本地 PDF 的确认。";
    if (error.code === "INVOICE_PDF_STORAGE_UNAVAILABLE")
      return "本地 PDF 存储暂不可用，未执行生成；检查存储后可重试同一卡。";
    if (error.code === "INVOICE_PDF_STORAGE_ERROR")
      return "PDF 文件操作失败，未完成审批；已尝试恢复原文件，请检查存储后重试。";
    if (error.code === "INVOICE_ACTION_CONFIRMATION_REQUIRED")
      return "请核实发票事实、实际收款及本地自动化影响，勾选后再确认。";
    if (error.code === "INVOICE_NOT_DRAFT")
      return "发票已不再是草稿，不能按旧建议修改，请重新读取。";
    if (
      error.code === "INVALID_INVOICE_TRANSITION" ||
      error.code === "INVOICE_NOT_OVERDUE"
    )
      return "当前发票状态或日期不允许此操作，请重新读取。";
    if (error.code === "INVOICE_NOT_FOUND")
      return "发票已不存在，旧建议未执行。";
    if (error.code === "FINANCIAL_ACTION_CONFIRMATION_REQUIRED")
      return "请核对金额、币种、日期和状态，并勾选财务变更确认。";
    if (error.code === "INVOICE_LINKED_FINANCIAL_ENTRY_IMMUTABLE")
      return "这条记录来自发票回款，不能独立修改或作废，请查看关联发票。";
    if (error.code === "FINANCIAL_ENTRY_VOIDED")
      return "这条收支记录已经作废，旧建议未执行。";
    if (error.code === "FINANCIAL_ENTRY_NOT_FOUND")
      return "该收支记录已不存在，请重新查询。";
    if (error.code === "AUTOMATION_RETRY_CONFIRMATION_REQUIRED")
      return "请核对原运行完整快照，并勾选确认本次重试。";
    if (error.code === "AUTOMATION_RETRY_ALREADY_EXISTS")
      return "这条失败记录已有后续尝试，未重复执行。请打开自动化设置查看最新运行。";
    if (error.code === "AUTOMATION_RUN_NOT_RETRYABLE")
      return "该运行不可重试或已达三次上限，未执行。请查看最新运行记录。";
    if (error.code === "AUTOMATION_RULE_DISABLED")
      return "定时规则已停用，未重试；若要恢复，请单独确认启用规则。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "自动化运行")
      return "当前规则或权限已变化，旧重试建议未执行。请重新读取并确认原快照。";
    if (error.code === "AUTOMATION_ENABLE_CONFIRMATION_REQUIRED")
      return "请核对规则的持续本地效果，并勾选确认。";
    if (error.code === "AUTOMATION_DEPENDENCY_UNAVAILABLE")
      return "该内置规则的依赖尚未接通，不能启用。请在自动化设置查看原因。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "自动化规则")
      return "规则的权限或预设定义已变化，旧建议未执行。请重新读取并生成建议。";
    if (error.code === "CLIENT_ACTIVITY_DELETE_CONFIRMATION_REQUIRED")
      return "请核对删除原因，并勾选确认删除这条客户活动。";
    if (error.code === "CLIENT_ACTIVITY_READ_ONLY")
      return "系统活动只能查看，不能修改或删除。";
    if (error.code === "CLIENT_ACTIVITY_DELETED")
      return "这条活动已被删除，未再次执行。请读取最新时间线。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "客户活动")
      return "客户资料已变化，旧提议未执行。请重新读取并生成建议。";
    if (error.code === "CLIENT_FOLLOWUP_COMPLETION_CONFIRMATION_REQUIRED")
      return "请核对并勾选确认真实回访结果和完成时间，不能由智能体代替你确认。";
    if (error.code === "CLIENT_FOLLOWUP_CLIENT_INACTIVE")
      return "客户已停用，未新建或修改计划。请先恢复客户，或在客户页处理已有回访。";
    if (error.code === "CLIENT_FOLLOWUP_ASSIGNEE_UNAVAILABLE")
      return "回访负责人不可用，未执行。请重新选择启用的所有者或本地人员。";
    if (error.code === "CLIENT_FOLLOWUP_FINAL")
      return "回访已经完成、跳过或取消，不能再修改。请重新读取最新记录。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "客户回访")
      return "客户或负责人资料已变化，旧提议未执行。请重新读取并生成建议。";
    if (error.code === "INVALID_FOCUS_SESSION_STATE")
      return "专注状态已变化，旧提议未执行。请重新读取并生成建议，或打开专注页处理。";
    if (error.code === "ACTIVE_FOCUS_SESSION_EXISTS")
      return "已有正在专注、暂停或待恢复的会话，未开始新专注。请先处理现有会话。";
    if (error.code === "FOCUS_START_CONFIRMATION_REQUIRED")
      return "开始新专注需要额外勾选确认；本地休息和旧轮次会被新循环替代。";
    if (error.code === "FOCUS_GAP_CONFIRMATION_REQUIRED")
      return "计入中断间隔需要额外勾选确认，不能由智能体代替你决定。";
    if (error.code === "CONFIRMATION_REQUIRED")
      return "强制解决需要额外人工勾选；它只结清收件箱事项，不会完成任务或通过验收。";
    if (error.code === "INBOX_FORCE_RESOLVE_NOT_REQUIRED")
      return "此事项采用手工解决策略，请重新读取并使用普通解决操作。";
    if (
      error.code === "AI_ACTION_PREVIEW_CHANGED" &&
      target === "收件箱全部已读"
    )
      return "快照内的未读事项已经发生变化，旧提议未执行。请重新读取收件箱快照并核对新的数量。";
    if (error.code === "INBOX_READ_ALL_EMPTY")
      return "该收件箱快照已经没有可标记的未读事项，无需执行。";
    if (error.code === "AI_INBOX_READ_ALL_TOO_LARGE")
      return "快照内未读事项超过智能体单次 1000 项上限，请在收件箱页面人工处理。";
    if (error.code === "REMINDER_NOT_SCHEDULED")
      return "提醒已触发或已取消，旧提议未执行。请打开本地提醒查看最新状态；重复提醒请读取下一条待触发记录。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "Agent 执行")
      return "任务、分派、Agent、Adapter 或 Provider 已变化，旧快照未执行。请重新读取资格并生成建议。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED")
      return target === "专注"
        ? "专注关联的任务资料已变化，旧提议未执行。请让智能体重新读取并生成建议。"
        : target === "内容日历条目"
          ? "内容条目或关联任务进度已变化，旧提议未执行。请让智能体重新读取并生成建议。"
          : target === "本地人员"
            ? "人员档案已变化，旧提议未执行。请重新读取人员版本并生成建议。"
            : "关联任务的进度已变化，或负责人、项目、标签已更新；旧提议未执行。请让智能体重新读取并生成建议。";
    if (error.code === "ACTOR_NOT_EDITABLE")
      return "只能修改本地人员身份；本人、系统身份和 Agent 均未执行。请打开人员与责任设置核对。";
    if (error.code === "ACTOR_HAS_ACTIVE_ASSIGNMENTS")
      return "该人员仍承担活动任务责任，不能停用。请先在任务中改派或结束责任。";
    if (error.code === "ACTOR_HAS_ACTIVE_CLIENT_LINKS")
      return "该人员仍是客户的有效联系人，不能停用。请先在客户页解除联系人关系。";
    if (error.code === "ACTOR_HAS_PLANNED_CLIENT_FOLLOWUPS")
      return "该人员仍负责计划中的客户回访，不能停用。请先改派或结束这些回访。";
    if (
      [
        "ACTOR_NOT_FOUND",
        "ASSIGNMENT_ACTOR_NOT_ACTIVE",
        "ASSIGNMENT_ACTOR_NOT_EXECUTABLE",
        "TAG_NOT_FOUND",
      ].includes(error.code ?? "")
    )
      return "负责人或标签已不可用，旧提议未执行。请重新读取可选项并生成建议。";
    if (
      [
        "ASSIGNMENT_NOT_ACTIVE",
        "ASSIGNMENT_ALREADY_ACTIVE",
        "ASSIGNMENT_UNCHANGED",
      ].includes(error.code ?? "")
    )
      return "责任分派已变化或与当前相同，旧提议未执行。请重新读取当前责任，核对后再生成建议。";
    if (error.code === "ASSIGNMENT_REVIEWER_MUST_BE_OWNER")
      return "验收人只能是启用的本人，旧提议未执行。请重新查询可选验收人。";
    if (error.code === "ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED")
      return "所选角色类型不能承担此责任，旧提议未执行。请重新查询可选人员或就绪 Agent。";
    if (error.code === "TASK_NOT_ASSIGNABLE")
      return "任务已结束，不能分派或改派。旧提议未执行，请重新读取任务状态。";
    if (
      [
        "INBOX_TASK_ALREADY_LINKED",
        "INBOX_TASK_RELATION_NOT_ACTIVE",
        "INBOX_TASK_RELATION_NOT_FOUND",
      ].includes(error.code ?? "")
    )
      return "任务关联已变化，旧提议未执行。请重新读取当前关系。";
    if (error.code === "INBOX_TASK_LIMIT_REACHED")
      return "此事项已有 100 个活动任务关联，请先在收件箱整理。";
    if (error.code === "INBOX_REQUIRED_TASKS_INCOMPLETE")
      return "仍有未完成的必需任务，不能直接解决此事项。请打开收件箱查看关联任务。";
    if (error.code === "INBOX_ITEM_TERMINAL")
      return "事项已归档，请先重新打开，再生成修改建议。";
    if (error.code === "INBOX_ITEM_NOT_ARCHIVED")
      return "事项已处于待处理状态，无需重新打开。请重新读取最新状态。";
    if (error.code === "VALIDATION_ERROR")
      return "当前数据不再满足操作条件（例如提醒或稍后时间已过）。旧提议未执行，请重新读取并生成建议。";
    if (error.code === "VERSION_CONFLICT")
      return `${target}已在其他地方修改，旧提议未执行。请让智能体重新读取并生成建议。`;
    if (error.code === "ROADMAP_MILESTONE_ARCHIVED")
      return "路线图里程碑已归档，请先恢复后再修改。";
    if (error.code === "ROADMAP_MILESTONE_STATE_INVALID")
      return "路线图里程碑状态已变化，旧提议未执行，请重新读取。";
    if (error.code === "CONTENT_ITEM_STATE_INVALID")
      return "内容条目的状态或排期已变化，旧提议未执行，请重新读取。已发布事实仍需你在原生页面或新的确认卡中独立核实。";
    if (error.code === "CONTENT_PUBLISHED_CONFIRMATION_REQUIRED")
      return "请先独立核实外部平台的实际发布情况、时间和链接，再单独勾选确认。";
    if (error.code === "KNOWLEDGE_SOURCE_DELETE_CONFIRMATION_REQUIRED")
      return "请核对来源名称、版本与删除范围，并单独勾选永久删除确认。";
    if (error.code === "KNOWLEDGE_JOB_NOT_RETRYABLE")
      return "只有失败或取消的索引任务可以重试，请重新读取最新元数据。";
    if (error.code === "KNOWLEDGE_JOB_TERMINAL")
      return "该索引任务已结束，不能取消。请重新读取最新元数据。";
    if (error.code === "KNOWLEDGE_INDEX_IN_PROGRESS")
      return "该来源正在重建索引，旧建议未执行。请等待当前任务结束或先取消它。";
    if (error.code === "KNOWLEDGE_SOURCE_UNAVAILABLE")
      return "该来源的受管副本不可用，无法重建索引。请在知识库页面重新导入。";
    if (error.code === "KNOWLEDGE_SOURCE_DELETED")
      return "该知识库来源已删除，旧建议未执行。请重新读取知识库列表。";
    if (error.code === "TASK_VIEW_DELETE_CONFIRMATION_REQUIRED")
      return "请核对视图名称与筛选条件，并单独勾选删除确认。";
    if (error.code === "TASK_SAVED_VIEW_LIMIT_REACHED")
      return "任务视图最多 20 个。请先在任务页删除不用的视图，再重新生成建议。";
    if (error.code === "AI_ACTION_PREVIEW_CHANGED" && target === "任务保存视图")
      return "该视图已被修改或删除，旧建议未执行。请重新读取任务视图列表。";
    if (error.code === "INCOMPLETE_TASKS_CONFIRMATION_REQUIRED")
      return "项目仍有未完成任务，需要额外确认；完成项目不会改变这些任务。";
    if (error.code === "PROJECT_CLIENT_CHANGE_BLOCKED_BY_INVOICES")
      return "该项目已有发票关联，不能修改客户。请打开项目详情处理。";
    if (error.code === "PROJECT_ARCHIVED")
      return "项目已归档，请先恢复项目，再重新生成修改建议。";
    if (error.code === "INVALID_PROJECT_TRANSITION")
      return "项目当前状态不允许此操作，请重新读取并生成建议。";
    if (error.code === "TASK_REVIEW_REQUIRED")
      return "此任务需要人工验收，不能直接完成。请打开任务提交产出并验收。";
    if (error.code === "TASK_ASSIGNEE_REQUIRED")
      return "请先确认分派负责人（也可在任务详情分派），再重新提出开始操作。";
    if (error.code === "AI_ACTION_NOT_CONFIRMABLE")
      return "提议已过期或生成未完成，未执行。请重新生成。";
    if (error.code === "AGENT_EXECUTION_CONFIRMATION_REQUIRED")
      return "请核对完整执行快照并勾选确认；授权工作台能力本身不会启动 Agent。";
    if (error.code === "AGENT_CANCEL_CONFIRMATION_REQUIRED")
      return "请核对具体执行记录并勾选停止确认；这不会取消任务。";
    if (error.code === "AGENT_RUN_CANCEL_ALREADY_REQUESTED")
      return "该执行已接受停止请求，请查看执行状态并等待收尾，无需重复提议。";
    if (error.code === "AGENT_RUN_NOT_ACTIVE")
      return "执行已结束，未再取消。请打开执行过程查看最终状态。";
    if (error.code === "AGENT_RUN_NOT_RETRYABLE")
      return "只能重试失败、取消或中断的执行。请查看实际状态；待登记结果应恢复登记，不要重跑模型。";
    if (error.code === "AGENT_OUTPUT_RECOVERY_CONFIRMATION_REQUIRED")
      return "请核对具体执行记录并勾选恢复登记确认；不会重新调用模型。";
    if (error.code === "AGENT_OUTPUT_DELIVERY_NOT_PENDING")
      return "这次执行已不再等待登记，请打开执行过程查看实际结果，未重复登记。";
    if (error.code === "AGENT_OUTPUT_DELIVERY_PENDING")
      return "模型结果已完成并等待登记，不能取消丢弃。请打开执行过程恢复登记。";
    if (error.code === "AGENT_RUN_IDENTITY_CHANGED")
      return "任务、分派、Agent、Adapter 或 Provider 已变化，旧快照未执行。请重新读取资格并生成建议。";
    if (error.code === "AGENT_RUN_ALREADY_ACTIVE")
      return "该任务已有排队中或运行中的 Agent Run，未重复启动。请打开任务查看执行过程。";
    if (
      [
        "AGENT_RUN_NOT_EXECUTABLE",
        "AGENT_RUN_TASK_NOT_ACTIONABLE",
        "AGENT_PROVIDER_INVALID",
      ].includes(error.code ?? "")
    )
      return "当前任务、Agent、Adapter 或 Provider 已不满足执行条件，未启动。请在任务或设置中检查后重新提议。";
    if (error.status === 404) return "提议或目标已不存在，请重新读取。";
  }
  return "未能确认执行结果。正在回读状态；可刷新或重试同一提议，不会重复执行。";
}

const taskViewStatusLabels: Record<string, string> = {
  active: "活动（未完成）",
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

// Rendered straight from the strict proposal payload; empty filters are
// omitted so the reviewer sees the real scope of the preset.
function taskViewFilterRows(
  view: NonNullable<AiActionProposal["preview"]["task_view"]>,
) {
  const definition = view.definition;
  const rows: { label: string; value: string }[] = [];
  if (definition.q) rows.push({ label: "关键词", value: definition.q });
  if (definition.status)
    rows.push({
      label: "状态",
      value: taskViewStatusLabels[definition.status] ?? definition.status,
    });
  if (definition.priority)
    rows.push({ label: "优先级", value: definition.priority });
  if (definition.kind) rows.push({ label: "类型", value: definition.kind });
  if (definition.project_id)
    rows.push({
      label: "项目",
      value: view.project_name ?? definition.project_id,
    });
  if (definition.client_id)
    rows.push({
      label: "客户",
      value: view.client_name ?? definition.client_id,
    });
  if (definition.tag_ids?.length)
    rows.push({
      label: "标签",
      value:
        view.tag_names?.length === definition.tag_ids.length
          ? view.tag_names.join("、")
          : definition.tag_ids.join("、"),
    });
  if (definition.planned_date)
    rows.push({ label: "计划日期", value: definition.planned_date });
  if (definition.planned_from || definition.planned_to)
    rows.push({
      label: "计划范围",
      value: `${definition.planned_from ?? "不限"} → ${definition.planned_to ?? "不限"}`,
    });
  if (definition.due_from || definition.due_to)
    rows.push({
      label: "截止范围",
      value: `${definition.due_from ?? "不限"} → ${definition.due_to ?? "不限"}`,
    });
  if (definition.sort) rows.push({ label: "排序", value: definition.sort });
  return rows;
}

function focusActionNote(proposal: AiActionProposal) {
  switch (proposal.action.action) {
    case "focus.start":
      return "确认后按所示任务与时长开始新专注，并开始新的本地循环；已有休息和旧轮次将被替代。存在未结束会话时不会覆盖。开始专注不改变任务状态。";
    case "focus.pause":
      return "确认时才暂停，计时结算到确认时刻，不回退到提议时刻。不结束专注、不改变任务状态，也不重置休息与轮次。";
    case "focus.resume":
      return "确认后从当前时刻继续原专注；暂停时间不计入工时。不改变任务状态或休息与轮次。";
    case "focus.stop":
      return "确认时结束专注，按实际有效时间累计到关联任务工时（上限为计划时长）；不完成任务，不自动开始休息或下一轮。";
    case "focus.cancel":
      return "确认后取消本次专注，保留会话和已记录区间，但不累计任务工时、不开始休息或下一轮，不改变任务状态。";
    case "focus.recover":
      return proposal.action.changes.recovery_action === "include_gap_resume"
        ? "将最后心跳到实际确认时刻的中断间隔计入本次专注（上限为计划时长），从确认时刻继续；结束时才累计任务工时。请只在确实持续工作时选择。此前已结算时长不包含尚未关闭的最后工作区间。"
        : proposal.action.changes.recovery_action === "exclude_gap_resume"
          ? "只保留到最后心跳的有效工作时间，排除中断间隔，从确认时刻继续原会话；结束时才累计任务工时，不重置轮次。此前已结算时长不包含尚未关闭的最后工作区间。"
          : "保留到最后心跳的有效时间并结束为中断，仅保留审计记录，不累计任务工时、不开始休息或下一轮。";
    default:
      return "";
  }
}

// Confirmed proposals should read like a user-facing receipt rather than a
// transport status. The server result_id/route remain the source of truth;
// this helper only chooses a stable Chinese verb for the already-confirmed
// domain command and never runs or infers an operation.
function actionReceiptVerb(action: AiActionProposal["action"]["action"]) {
  const operation = action.slice(action.lastIndexOf(".") + 1);
  const verbs: Record<string, string> = {
    create: "创建",
    update: "更新",
    delete: "删除",
    start: "开始",
    pause: "暂停",
    resume: "继续",
    stop: "结束",
    cancel: "取消",
    complete: "记录完成",
    reopen: "重新打开",
    archive: "归档",
    restore: "恢复",
    enable: "启用",
    disable: "停用",
    retry: "记录重试",
    reindex: "开始重建",
    review: "提交审核",
    submit_output: "提交产出",
    assign: "更新责任",
    reassign: "更新责任",
    unassign: "结束责任",
    link: "建立关联",
    unlink: "解除关联",
    set_required: "更新必需任务",
    move: "调整顺序",
    schedule: "安排排期",
    unschedule: "清除排期",
    mark_sent: "记录已发送",
    mark_viewed: "记录已查看",
    mark_paid: "记录已收款",
    mark_overdue: "记录逾期",
    void: "作废",
    snooze: "延后处理",
    unsnooze: "恢复待处理",
    resolve: "解决",
    force_resolve: "强制解决",
    dismiss: "忽略",
    read: "标记已读",
    read_all: "标记全部已读",
    publish: "登记已发布",
    recover: "处理恢复",
    set_task_required: "更新必需任务",
  };
  return verbs[operation] ?? "执行";
}

function hasDedicatedActionReceipt(
  proposal: AiActionProposal,
  flags: {
    isAgentRun: boolean;
    isAgentDelegation: boolean;
    isAgentFollowup: boolean;
    isExport: boolean;
    isSplit: boolean;
    isTaskBatchCreate: boolean;
    isTaskCreate: boolean;
    isTaskUpdate: boolean;
  },
) {
  return (
    flags.isAgentRun ||
    flags.isAgentDelegation ||
    flags.isAgentFollowup ||
    flags.isExport ||
    flags.isSplit ||
    flags.isTaskBatchCreate ||
    flags.isTaskCreate ||
    flags.isTaskUpdate ||
    Boolean(
      proposal.invoice_pdf_result ||
      proposal.task_batch_create_result ||
      proposal.created_task_ids,
    )
  );
}

export function useAiWorkspaceActions(generationId: string | null) {
  return useQuery({
    queryKey: ["ai", "actions", generationId],
    queryFn: () => getAiWorkspaceActions(generationId!),
    enabled: !!generationId,
    retry: 1,
  });
}

type AiWorkspaceActionsProps = {
  generationId: string;
  sessionId?: string;
  proposalId?: string;
  onContinue?: (request: AiActionContinuation) => void;
};

export function AiWorkspaceActions(props: AiWorkspaceActionsProps) {
  return (
    <AiWorkspaceActionGroup
      key={`${props.generationId}:${props.sessionId ?? ""}:${props.proposalId ?? ""}`}
      {...props}
    />
  );
}

function AiWorkspaceActionGroup({
  generationId,
  sessionId,
  proposalId,
  onContinue,
}: AiWorkspaceActionsProps) {
  const query = useAiWorkspaceActions(generationId);
  const [delegationBatchMode, setDelegationBatchMode] = useState(false);
  const [delegationBatchSelection, setDelegationBatchSelection] = useState<
    Record<string, string>
  >({});
  const [delegationBatchConsent, setDelegationBatchConsent] = useState(false);
  const [delegationBatchFeedback, setDelegationBatchFeedback] = useState<
    string | null
  >(null);
  const [delegationBatchNotice, setDelegationBatchNotice] = useState<
    string | null
  >(null);
  // Keep only identities across failed reads; stale previews are still hidden.
  const [conflicts, setConflicts] = useState<AiActionRecheckSource[]>([]);
  const [recovering, setRecovering] = useState(false);
  const recoveryLock = useRef(false);
  const proposals = proposalId
    ? (query.data ?? []).filter((proposal) => proposal.id === proposalId)
    : (query.data ?? []);
  const pendingDelegations = query.isSuccess
    ? proposals.filter(
        (proposal) =>
          proposal.action.action === "agent_delegate.spawn" &&
          proposal.status === "pending" &&
          proposal.preview.agent_delegation !== undefined,
      )
    : [];
  const selectedDelegations = pendingDelegations.filter(
    (proposal) =>
      delegationBatchSelection[proposal.id] === proposal.fingerprint,
  );
  const delegationBatchSelectionStale =
    Object.keys(delegationBatchSelection).length !== selectedDelegations.length;
  const delegationBatchMutation = useMutation({
    mutationFn: () =>
      confirmAiAgentDelegationBatch(
        generationId,
        selectedDelegations,
        delegationBatchConsent,
      ),
    onSuccess: async (results) => {
      setDelegationBatchMode(false);
      setDelegationBatchSelection({});
      setDelegationBatchConsent(false);
      setDelegationBatchFeedback(null);
      setDelegationBatchNotice(
        `已原子确认 ${results.length} 个子会话；各自的启动状态将分别更新。`,
      );
      await query.refetch();
    },
    onError: async (reason) => {
      const queueHint = agentDelegationQueueHint(reason);
      setDelegationBatchFeedback(
        queueHint
          ? `整批未确认：${queueHint}`
          : "批次结果暂不可确认，正在重读每条提案；请先核对权威状态，不要盲目重复确认。",
      );
      await query.refetch();
    },
  });
  if (query.isSuccess && !proposals.length && !conflicts.length && !proposalId)
    return null;
  return (
    <section className="ai-workspace-actions" aria-label="工作台操作建议">
      {query.isPending ? (
        <p className="ai-action-note" role="status">
          正在读取工作台操作建议…
        </p>
      ) : query.isError ? (
        <p role="alert">
          操作建议读取失败。
          <button
            type="button"
            className="button button-quiet"
            onClick={() => void query.refetch()}
          >
            重新读取操作建议
          </button>
        </p>
      ) : !proposals.length && proposalId ? (
        <p role="status">该操作建议已不存在，请关闭后刷新续办队列。</p>
      ) : null}
      {delegationBatchNotice ? (
        <p role="status" className="ai-action-note">
          {delegationBatchNotice}
        </p>
      ) : null}
      {pendingDelegations.length >= 2 && !delegationBatchMode ? (
        <button
          className="button button-secondary"
          type="button"
          disabled={recovering || query.isFetching}
          onClick={() => {
            setDelegationBatchMode(true);
            setDelegationBatchSelection({});
            setDelegationBatchConsent(false);
            setDelegationBatchFeedback(null);
            setDelegationBatchNotice(null);
          }}
        >
          统一审查子委派（{pendingDelegations.length}）
        </button>
      ) : null}
      {delegationBatchMode ? (
        <section
          className="ai-action-agent-batch-review"
          aria-label="批量确认子智能体"
        >
          <strong>选择要一起确认的子任务</strong>
          <p className="ai-action-note">
            每项仍有独立指令和权限；选择不会自动授权。请逐项查看下方完整预览，再一次确认所选
            2–4 项。只有整组预览均未变化时才会创建子会话。
          </p>
          {selectedDelegations.length ? (
            <ul className="ai-action-agent-batch-list">
              {selectedDelegations.map((proposal) => {
                const preview = proposal.preview.agent_delegation;
                if (!preview) return null;
                return (
                  <li key={`${proposal.id}:${proposal.fingerprint}`}>
                    <strong>{preview.task_name}</strong>
                    <span>
                      {preview.provider_name} · {preview.model} · 权限：
                      {preview.scopes.join("、")} ·
                      {preview.leaves_device
                        ? "将发送到远程 Provider"
                        : "留在本机"}
                    </span>
                    <span>子会话：{preview.child_session_id}</span>
                    {preview.knowledge_sources?.length ? (
                      <span>
                        知识来源：
                        {preview.knowledge_sources
                          .map(
                            (source) =>
                              `${source.source_id} · v${source.expected_source_version}`,
                          )
                          .join("；")}
                      </span>
                    ) : null}
                    <code>{preview.message}</code>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="ai-action-note">尚未选择子任务；没有项目会被预选。</p>
          )}
          {delegationBatchSelectionStale ? (
            <p role="alert">
              所选提案状态或指纹已变化；请退出后刷新并重新选择。
            </p>
          ) : null}
          <label className="ai-action-consent">
            <input
              type="checkbox"
              checked={delegationBatchConsent}
              disabled={delegationBatchMutation.isPending}
              onChange={(event) =>
                setDelegationBatchConsent(event.target.checked)
              }
            />
            我已逐项核对每个所选子任务的完整指令、Provider/模型、独立权限、知识来源及数据是否离开设备；同意原子确认这组委派。启动状态仍逐项跟踪，启动失败不会自动重试。
          </label>
          {delegationBatchFeedback ? (
            <p role="alert">{delegationBatchFeedback}</p>
          ) : null}
          <div className="ai-action-agent-batch-controls">
            <button
              className="button button-primary"
              type="button"
              disabled={
                selectedDelegations.length < 2 ||
                selectedDelegations.length > 4 ||
                delegationBatchSelectionStale ||
                selectedDelegations.some((proposal) => !proposal.can_confirm) ||
                !delegationBatchConsent ||
                delegationBatchMutation.isPending ||
                query.isFetching
              }
              onClick={() => delegationBatchMutation.mutate()}
            >
              {delegationBatchMutation.isPending
                ? "正在原子确认…"
                : `确认委派 ${selectedDelegations.length} 个子智能体`}
            </button>
            <button
              className="button button-secondary"
              type="button"
              disabled={delegationBatchMutation.isPending}
              onClick={() => {
                setDelegationBatchMode(false);
                setDelegationBatchSelection({});
                setDelegationBatchConsent(false);
                setDelegationBatchFeedback(null);
              }}
            >
              退出批量审查
            </button>
          </div>
        </section>
      ) : null}
      {query.isSuccess &&
        proposals.map((proposal) => (
          <ActionCard
            key={`${proposal.id}:${proposal.fingerprint}`}
            proposal={proposal}
            sessionId={sessionId}
            disabled={recovering}
            delegationBatchMode={delegationBatchMode}
            delegationBatchSelected={
              delegationBatchSelection[proposal.id] === proposal.fingerprint
            }
            delegationBatchPending={delegationBatchMutation.isPending}
            onDelegationBatchSelectionChange={(selected) => {
              if (
                selected &&
                delegationBatchSelection[proposal.id] !==
                  proposal.fingerprint &&
                Object.keys(delegationBatchSelection).length >= 4
              ) {
                setDelegationBatchFeedback("每批最多选择四个子任务。");
                return;
              }
              setDelegationBatchSelection((current) => {
                if (!selected) {
                  const next = { ...current };
                  delete next[proposal.id];
                  return next;
                }
                if (!current[proposal.id] && Object.keys(current).length >= 4)
                  return current;
                return { ...current, [proposal.id]: proposal.fingerprint };
              });
              setDelegationBatchConsent(false);
              setDelegationBatchFeedback(null);
            }}
            onConfirmationConflict={() => {
              if (!sessionId || !isWorkspaceIdentity(sessionId)) return;
              const source: AiActionRecheckSource = {
                sourceSessionId: sessionId,
                generationId,
                proposalId: proposal.id,
                fingerprint: proposal.fingerprint,
              };
              setConflicts((current) =>
                current.some(
                  (item) =>
                    item.proposalId === source.proposalId &&
                    item.fingerprint === source.fingerprint,
                )
                  ? current
                  : [...current, source],
              );
            }}
          />
        ))}
      {onContinue
        ? conflicts.map((source) => (
            <ActionConflictRecheck
              key={`${source.proposalId}:${source.fingerprint}`}
              source={source}
              query={query}
              onContinue={onContinue}
              recoveryLock={recoveryLock}
              recovering={recovering}
              onRecovering={setRecovering}
            />
          ))
        : null}
      {onContinue && query.isSuccess && proposals.length > 0 ? (
        <ActionContinuationFooter
          key={`${generationId}:${sessionId ?? ""}:${proposalId ?? ""}`}
          generationId={generationId}
          proposalId={proposalId}
          query={query}
          onContinue={onContinue}
        />
      ) : null}
    </section>
  );
}

function ActionConflictRecheck({
  source,
  query,
  onContinue,
  recoveryLock,
  recovering,
  onRecovering,
}: {
  source: AiActionRecheckSource;
  query: ReturnType<typeof useAiWorkspaceActions>;
  onContinue: (request: AiActionContinuation) => void;
  recoveryLock: { current: boolean };
  recovering: boolean;
  onRecovering: (value: boolean) => void;
}) {
  const client = useQueryClient();
  const [consentEpoch, setConsentEpoch] = useState<number | null>(null);
  const [checking, setChecking] = useState(false);
  const [withdrawn, setWithdrawn] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const mounted = useRef(true);
  const callback = useRef({ value: onContinue, epoch: 0 });
  if (callback.current.value !== onContinue)
    callback.current = { value: onContinue, epoch: callback.current.epoch + 1 };
  const consent = consentEpoch === callback.current.epoch;
  useEffect(() => {
    setFeedback(null);
  }, [onContinue]);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const currentTarget = query.isSuccess
    ? findAiActionRecheckTarget(source, query.data)
    : null;
  const rejected = withdrawn || currentTarget?.status === "rejected";

  async function recheck() {
    if (!consent || recoveryLock.current) return;
    recoveryLock.current = true;
    onRecovering(true);
    setChecking(true);
    setFeedback(null);
    const initialCallback = callback.current;
    const stillCurrent = () => {
      if (!mounted.current) return false;
      if (callback.current.epoch !== initialCallback.epoch) {
        setFeedback("输入来源已变化，请重新勾选后核对。");
        return false;
      }
      return true;
    };
    try {
      let fresh = await query.refetch({ throwOnError: true });
      if (!stillCurrent()) return;
      const target =
        fresh.isSuccess && fresh.data
          ? findAiActionRecheckTarget(source, fresh.data)
          : null;
      if (!target) {
        setFeedback("原建议身份已变化或无法读取，请重新定位原审批。");
        return;
      }
      if (!["pending", "expired", "rejected"].includes(target.status)) {
        setFeedback(
          target.status === "confirmed"
            ? "原建议已经执行，不能视为已撤回或重新提议，请核对真实回执。"
            : "原建议当前不可处理，请重新读取生成状态。",
        );
        return;
      }
      if (target.status !== "rejected") {
        // Reject exactly the immutable fingerprint the user selected. No
        // execution/file/financial consent is carried into this decision.
        try {
          await decideAiWorkspaceAction(target, "reject");
        } catch {
          // A lost response is not proof of failure (or of withdrawal). Always
          // read the complete durable group before deciding what happened.
        }
        await Promise.all([
          client.invalidateQueries({
            queryKey: ["ai", "actions", source.generationId],
            refetchType: "none",
          }),
          client.invalidateQueries({ queryKey: ["ai", "work-plans"] }),
          client.invalidateQueries({ queryKey: ["ai", "agent-inbox"] }),
          client.invalidateQueries({
            queryKey: ["ai", "work-plan", source.sourceSessionId],
          }),
        ]);
        fresh = await query.refetch({ throwOnError: true });
        if (!stillCurrent()) return;
      }
      if (!fresh.isSuccess || !fresh.data) {
        setFeedback("无法核对最新操作状态，请重新读取后再继续。");
        return;
      }
      const refreshedTarget = findAiActionRecheckTarget(source, fresh.data);
      if (refreshedTarget?.status === "rejected") setWithdrawn(true);
      const readiness = assessAiActionRecheck(source, fresh.data);
      if (!readiness.request) {
        setFeedback(readiness.reason);
        return;
      }
      if (stillCurrent()) initialCallback.value(readiness.request);
    } catch {
      if (stillCurrent())
        setFeedback(
          "无法核对最新操作状态；不会据此重提或执行，请重新读取后再继续。",
        );
    } finally {
      recoveryLock.current = false;
      onRecovering(false);
      if (mounted.current) setChecking(false);
    }
  }

  return (
    <section
      className="ai-action-continuation"
      aria-label={`重新核验建议 ${source.proposalId}`}
    >
      <p className="ai-action-note">
        确认时发现版本或预览冲突。此入口只撤回这条旧建议并准备重新核验，不会批准新操作。
      </p>
      <label className="ai-action-consent">
        <input
          type="checkbox"
          checked={consent}
          disabled={recovering}
          onChange={(event) =>
            setConsentEpoch(
              event.target.checked ? callback.current.epoch : null,
            )
          }
        />
        重新发送原操作参数（可能含备注、财务或知识文本），不含完整审批预览，仅手动发送才交给最终所选模型
      </label>
      {feedback ? <p role="status">{feedback}</p> : null}
      <button
        className="button button-secondary"
        type="button"
        disabled={!consent || recovering || query.isFetching}
        onClick={() => void recheck()}
      >
        {checking
          ? "正在撤回并核对…"
          : rejected
            ? "核对已撤回建议并继续"
            : "撤回旧建议并重新核验"}
      </button>
      <p className="ai-action-note">
        只准备所选来源附件；草稿不复制原参数。仍需重新授权并手动发送。
      </p>
    </section>
  );
}

function ActionContinuationFooter({
  generationId,
  proposalId,
  query,
  onContinue,
}: {
  generationId: string;
  proposalId?: string;
  query: ReturnType<typeof useAiWorkspaceActions>;
  onContinue: (request: AiActionContinuation) => void;
}) {
  const [checking, setChecking] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const mounted = useRef(true);
  const pending = useRef(false);
  const continueRef = useRef(onContinue);
  continueRef.current = onContinue;
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const readiness = assessAiActionContinuation(generationId, query.data ?? []);

  async function refresh(prepare: boolean) {
    if (pending.current) return;
    pending.current = true;
    setChecking(true);
    setFeedback(null);
    try {
      const fresh = await query.refetch({ throwOnError: true });
      if (!mounted.current) return;
      if (!fresh.isSuccess || !fresh.data) {
        setFeedback("无法核对最新操作状态，请重新读取后再继续。");
        return;
      }
      if (
        proposalId &&
        !fresh.data.some((proposal) => proposal.id === proposalId)
      ) {
        setFeedback("原操作建议已不存在，请刷新续办队列。");
        return;
      }
      const next = assessAiActionContinuation(generationId, fresh.data);
      if (!next.request) {
        setFeedback(next.reason);
        return;
      }
      if (prepare) continueRef.current(next.request);
    } catch {
      if (mounted.current)
        setFeedback("无法核对最新操作状态，请重新读取后再继续。");
    } finally {
      pending.current = false;
      if (mounted.current) setChecking(false);
    }
  }

  return (
    <div className="ai-action-continuation">
      {feedback ? <p role="status">{feedback}</p> : null}
      {readiness.request ? (
        <>
          <button
            className="button button-secondary"
            type="button"
            disabled={checking || query.isFetching}
            onClick={() => void refresh(true)}
          >
            {checking ? "正在核对本轮状态…" : "继续处理这一轮"}
          </button>
          <p className="ai-action-note">
            核对后准备下一条消息，仍需选择权限并发送。
          </p>
        </>
      ) : (
        <>
          {!feedback ? (
            <p className="ai-action-note">{readiness.reason}</p>
          ) : null}
          <button
            className="button button-quiet"
            type="button"
            disabled={checking || query.isFetching}
            onClick={() => void refresh(false)}
          >
            {checking ? "正在核对本轮状态…" : "刷新本轮状态"}
          </button>
        </>
      )}
    </div>
  );
}

function ActionCard({
  proposal,
  sessionId,
  disabled = false,
  delegationBatchMode = false,
  delegationBatchSelected = false,
  delegationBatchPending = false,
  onDelegationBatchSelectionChange,
  onConfirmationConflict,
}: {
  proposal: AiActionProposal;
  sessionId?: string;
  disabled?: boolean;
  delegationBatchMode?: boolean;
  delegationBatchSelected?: boolean;
  delegationBatchPending?: boolean;
  onDelegationBatchSelectionChange?: (selected: boolean) => void;
  onConfirmationConflict?: () => void;
}) {
  const client = useQueryClient();
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const isAutomation = proposal.action.action.startsWith("automation.");
  const isKnowledge =
    proposal.action.action.startsWith("knowledge_source.") ||
    proposal.action.action.startsWith("knowledge_index_job.");
  const deletingKnowledgeSource =
    proposal.action.action === "knowledge_source.delete";
  const creatingKnowledgeSource =
    proposal.action.action === "knowledge_source.create";
  const [confirmKnowledgeDelete, setConfirmKnowledgeDelete] = useState(false);
  const isTaskView = proposal.action.action.startsWith("task_view.");
  const deletingTaskView = proposal.action.action === "task_view.delete";
  const [confirmTaskViewDelete, setConfirmTaskViewDelete] = useState(false);
  const isTaskBatch = proposal.action.action === "task.batch_update";
  const isTaskBatchCreate = proposal.action.action === "task.batch_create";
  const isTaskCreate = proposal.action.action === "task.create";
  const isTaskUpdate = proposal.action.action === "task.update";
  const isTaskOrder = proposal.action.action === "task.move";
  const isAgentDelegation = proposal.action.action === "agent_delegate.spawn";
  const isAgentFollowup = proposal.action.action === "agent_delegate.followup";
  const isAgentRunRetry = proposal.action.action === "agent_run.retry";
  const isAgentRunStart =
    proposal.action.action === "agent_run.start" || isAgentRunRetry;
  const isAgentRunCancelMany =
    proposal.action.action === "agent_run.cancel_many";
  const isAgentRunCancel =
    proposal.action.action === "agent_run.cancel" || isAgentRunCancelMany;
  const isAgentRunRecovery =
    proposal.action.action === "agent_run.recover_output";
  const isAgentRun = isAgentRunStart || isAgentRunCancel || isAgentRunRecovery;
  const [confirmAgentDelegation, setConfirmAgentDelegation] = useState(false);
  useEffect(() => {
    setConfirmAgentDelegation(false);
  }, [proposal.id, proposal.fingerprint]);
  const recordedAgentRun =
    isAgentRun && !isAgentRunCancelMany ? proposal.agent_run_result : undefined;
  const agentRunRecordDeleted =
    isAgentRun &&
    !isAgentRunCancelMany &&
    proposal.status === "confirmed" &&
    Boolean(proposal.result_id) &&
    !recordedAgentRun;
  const agentRunTaskId =
    isAgentRun && !isAgentRunCancelMany
      ? (proposal.preview.agent_run_start?.task.id ??
        proposal.preview.agent_run_cancel?.task_id ??
        proposal.preview.agent_run_recovery?.task_id ??
        null)
      : null;
  const agentRunId =
    isAgentRun && !isAgentRunCancelMany && proposal.status === "confirmed"
      ? (recordedAgentRun?.id ?? null)
      : null;
  const agentRunQuery = useAgentRunQuery(
    agentRunId,
    agentRunTaskId,
    Boolean(agentRunId && agentRunTaskId),
  );
  const retryAgentRunDelivery = useRetryAgentRunOutputDelivery();
  const agentRunStatus = agentRunQuery.data?.status ?? recordedAgentRun?.status;
  const agentRunDeliveryStatus =
    agentRunQuery.data?.outputDeliveryStatus ??
    recordedAgentRun?.output_delivery_status;
  const agentRunDeliveryErrorCode =
    agentRunQuery.data?.outputDeliveryErrorCode ??
    recordedAgentRun?.output_delivery_error_code ??
    null;
  const agentRunSubmissionId =
    agentRunQuery.data?.submissionId ?? recordedAgentRun?.submission_id ?? null;
  const agentRunRoute =
    agentRunTaskId &&
    agentRunDeliveryStatus === "submitted" &&
    agentRunSubmissionId
      ? taskSubmissionHref(agentRunTaskId, agentRunSubmissionId)
      : agentRunTaskId && agentRunId
        ? agentRunHref(agentRunTaskId, agentRunId)
        : proposal.route;
  const actionRoute = isAgentRun ? agentRunRoute : proposal.route;
  const isFinancial = proposal.action.action.startsWith("financial_entry.");
  const isExport = proposal.action.action === "finance.export_csv";
  const isTaskOutput = isTaskOutputAction(proposal.action.action);
  const [confirmTaskOutput, setConfirmTaskOutput] = useState(false);
  const [confirmAgentExecution, setConfirmAgentExecution] = useState(false);
  const [confirmAgentCancel, setConfirmAgentCancel] = useState(false);
  const [confirmAgentRecovery, setConfirmAgentRecovery] = useState(false);
  const [confirmAgentFiles, setConfirmAgentFiles] = useState(false);
  const [confirmAgentProjectFiles, setConfirmAgentProjectFiles] =
    useState(false);
  const agentRunHasProjectFiles =
    proposal.preview.agent_run_start?.input_files?.some(
      (file) => file.source_kind === "project_task_artifact",
    ) ?? false;
  const [confirmAgentRework, setConfirmAgentRework] = useState(false);
  const [confirmAgentRestart, setConfirmAgentRestart] = useState(false);
  const agentRunHasRestart =
    isAgentRunStart && !!proposal.preview.agent_run_start?.restart;
  const agentRunHasRework =
    isAgentRunStart && !!proposal.preview.agent_run_start?.rework_context;
  const agentRunConfirmationIdentity = JSON.stringify(
    proposal.preview.agent_run_start ?? null,
  );
  useEffect(() => {
    setConfirmAgentRework(false);
    setConfirmAgentRestart(false);
    setConfirmAgentExecution(false);
    setConfirmAgentFiles(false);
    setConfirmAgentProjectFiles(false);
  }, [proposal.id, proposal.fingerprint, agentRunConfirmationIdentity]);
  const agentRunHasInputFiles =
    isAgentRun &&
    (proposal.preview.agent_run_start?.input_files?.length ?? 0) > 0;
  const [confirmExport, setConfirmExport] = useState(false);
  const isInvoice = proposal.action.action.startsWith("invoice.");
  const deletingInvoice = proposal.action.action === "invoice.delete";
  const generatingInvoicePdf =
    proposal.action.action === "invoice.generate_pdf";
  const [confirmInvoicePdf, setConfirmInvoicePdf] = useState(false);
  const [confirmInvoiceDelete, setConfirmInvoiceDelete] = useState(false);
  const [confirmInvoice, setConfirmInvoice] = useState(false);
  const [confirmFinancial, setConfirmFinancial] = useState(false);
  const retryingAutomation = proposal.action.action === "automation.retry";
  const [confirmAutomationRetry, setConfirmAutomationRetry] = useState(false);
  const needsAutomationConsent = needsAutomationEffectsConsent(proposal);
  const [confirmAutomation, setConfirmAutomation] = useState(false);
  const isNote = proposal.action.action.startsWith("project_note.");
  const deletingNote = proposal.action.action === "project_note.delete";
  const [confirmNoteDelete, setConfirmNoteDelete] = useState(false);
  const isProject = proposal.action.action.startsWith("project.");
  const deletingProject = proposal.action.action === "project.delete";
  const [confirmProjectDelete, setConfirmProjectDelete] = useState(false);
  const deletingTask = proposal.action.action === "task.delete";
  const [confirmTaskDelete, setConfirmTaskDelete] = useState(false);
  const isTag = proposal.action.action.startsWith("tag.");
  const deletingTag = proposal.action.action === "tag.delete";
  const [confirmTagDelete, setConfirmTagDelete] = useState(false);
  const isRoadmap = proposal.action.action.startsWith("roadmap_milestone.");
  const isRoadmapOrder = proposal.action.action === "roadmap_milestone.move";
  const deletingRoadmap = proposal.action.action === "roadmap_milestone.delete";
  const [confirmRoadmapDelete, setConfirmRoadmapDelete] = useState(false);
  const isContentItem = proposal.action.action.startsWith("content_item.");
  const deletingContentItem = proposal.action.action === "content_item.delete";
  const publishingContentItem =
    proposal.action.action === "content_item.publish";
  const [confirmContentPublished, setConfirmContentPublished] = useState(false);
  const contentItemTaskAction = [
    "content_item.link_task",
    "content_item.set_task_required",
    "content_item.unlink_task",
  ].includes(proposal.action.action);
  const [confirmContentItemDelete, setConfirmContentItemDelete] =
    useState(false);
  const isInbox = proposal.action.action.startsWith("inbox.");
  const isReminder = proposal.action.action.startsWith("reminder.");
  const isFocus = proposal.action.action.startsWith("focus.");
  const isClient = proposal.action.action.startsWith("client.");
  const deletingClient = proposal.action.action === "client.delete";
  const [confirmClientDelete, setConfirmClientDelete] = useState(false);
  const isClientContact = proposal.action.action.startsWith("client_contact.");
  const isPerson = proposal.action.action.startsWith("person.");
  const isFollowup = proposal.action.action.startsWith("client_followup.");
  const isActivity = proposal.action.action.startsWith("client_activity.");
  const deletingActivity = proposal.action.action === "client_activity.delete";
  const isSplit = proposal.action.action === "inbox.split";
  const readingAllInbox = proposal.action.action === "inbox.read_all";
  const inboxTaskAction = [
    "inbox.link_task",
    "inbox.set_required",
    "inbox.unlink_task",
  ].includes(proposal.action.action);
  const target =
    isAgentDelegation || isAgentFollowup
      ? "子智能体"
      : isNote
        ? "项目笔记"
        : isTaskView
          ? "任务保存视图"
          : isTaskBatchCreate
            ? "项目"
            : isTaskBatch
              ? "批量任务"
              : isKnowledge
                ? "知识库来源"
                : isAgentRun
                  ? "Agent 执行"
                  : isExport
                    ? "财务导出"
                    : isInvoice
                      ? "发票"
                      : isFinancial
                        ? "收支记录"
                        : retryingAutomation
                          ? "自动化运行"
                          : isAutomation
                            ? "自动化规则"
                            : isClientContact
                              ? "客户联系人"
                              : isPerson
                                ? "本地人员"
                                : isClient
                                  ? "客户"
                                  : isActivity
                                    ? "客户活动"
                                    : isFollowup
                                      ? "客户回访"
                                      : isFocus
                                        ? "专注"
                                        : isReminder
                                          ? "本地提醒"
                                          : readingAllInbox
                                            ? "收件箱全部已读"
                                            : isInbox
                                              ? "收件箱事项"
                                              : isRoadmap
                                                ? "路线图里程碑"
                                                : isContentItem
                                                  ? "内容日历条目"
                                                  : isTag
                                                    ? "任务标签"
                                                    : isProject
                                                      ? "项目"
                                                      : "任务";
  const showGenericActionReceipt =
    proposal.status === "confirmed" &&
    Boolean(proposal.result_id) &&
    !hasDedicatedActionReceipt(proposal, {
      isAgentRun,
      isAgentDelegation,
      isAgentFollowup,
      isExport,
      isSplit,
      isTaskBatchCreate,
      isTaskCreate,
      isTaskUpdate,
    });
  const [confirmIncomplete, setConfirmIncomplete] = useState(false);
  const [confirmActivityDelete, setConfirmActivityDelete] = useState(false);
  const [confirmForceResolve, setConfirmForceResolve] = useState(false);
  const [confirmFocusStart, setConfirmFocusStart] = useState(false);
  const [confirmFocusGap, setConfirmFocusGap] = useState(false);
  const [confirmFollowupCompleted, setConfirmFollowupCompleted] =
    useState(false);
  const completingFollowup =
    proposal.action.action === "client_followup.complete";
  const startingFocus = proposal.action.action === "focus.start";
  const includingFocusGap =
    proposal.action.action === "focus.recover" &&
    proposal.action.changes.recovery_action === "include_gap_resume";
  const forceResolving = proposal.action.action === "inbox.force_resolve";
  const completingProject = proposal.action.action === "project.complete";
  const needsIncompleteConsent =
    completingProject &&
    Number(proposal.preview.after.incomplete_task_count) > 0;
  const [error, setError] = useState<string | null>(null);
  const undecided = ["pending", "expired", "unavailable"].includes(
    proposal.status,
  );
  const mutation = useMutation({
    mutationFn: (decision: "confirm" | "reject") =>
      isAgentDelegation
        ? decideAiAgentDelegation(proposal, decision, confirmAgentDelegation)
        : isAgentFollowup
          ? decideAiAgentFollowup(proposal, decision, confirmAgentDelegation)
          : isAgentRunRecovery
            ? decideAiAgentRunRecovery(proposal, decision, confirmAgentRecovery)
            : isAgentRunCancel
              ? decideAiAgentRunCancel(proposal, decision, confirmAgentCancel)
              : isAgentRunStart
                ? agentRunHasProjectFiles
                  ? decideAiAgentRun(
                      proposal,
                      decision,
                      confirmAgentExecution,
                      confirmAgentFiles,
                      agentRunHasRework ? confirmAgentRework : undefined,
                      agentRunHasRestart ? confirmAgentRestart : undefined,
                      confirmAgentProjectFiles,
                    )
                  : agentRunHasRestart
                    ? decideAiAgentRun(
                        proposal,
                        decision,
                        confirmAgentExecution,
                        agentRunHasInputFiles ? confirmAgentFiles : undefined,
                        agentRunHasRework ? confirmAgentRework : undefined,
                        confirmAgentRestart,
                      )
                    : agentRunHasRework
                      ? decideAiAgentRun(
                          proposal,
                          decision,
                          confirmAgentExecution,
                          agentRunHasInputFiles ? confirmAgentFiles : undefined,
                          confirmAgentRework,
                        )
                      : agentRunHasInputFiles
                        ? decideAiAgentRun(
                            proposal,
                            decision,
                            confirmAgentExecution,
                            confirmAgentFiles,
                          )
                        : decideAiAgentRun(
                            proposal,
                            decision,
                            confirmAgentExecution,
                          )
                : deletingTask
                  ? decideAiTaskDelete(proposal, decision, confirmTaskDelete)
                  : isKnowledge
                    ? decideAiKnowledgeAction(
                        proposal,
                        decision,
                        confirmKnowledgeDelete,
                      )
                    : isTaskView
                      ? decideAiTaskViewAction(
                          proposal,
                          decision,
                          confirmTaskViewDelete,
                        )
                      : deletingProject
                        ? decideAiProjectDelete(
                            proposal,
                            decision,
                            confirmProjectDelete,
                          )
                        : deletingTag
                          ? decideAiTagDelete(
                              proposal,
                              decision,
                              confirmTagDelete,
                            )
                          : deletingRoadmap
                            ? decideAiRoadmapMilestoneDelete(
                                proposal,
                                decision,
                                confirmRoadmapDelete,
                              )
                            : deletingClient
                              ? decideAiClientDelete(
                                  proposal,
                                  decision,
                                  confirmClientDelete,
                                )
                              : publishingContentItem
                                ? decideAiContentPublished(
                                    proposal,
                                    decision,
                                    confirmContentPublished,
                                  )
                                : deletingContentItem
                                  ? decideAiContentItemDelete(
                                      proposal,
                                      decision,
                                      confirmContentItemDelete,
                                    )
                                  : isNote
                                    ? decideAiProjectNote(
                                        proposal,
                                        decision,
                                        confirmNoteDelete,
                                      )
                                    : isTaskOutput
                                      ? decideAiTaskOutput(
                                          proposal,
                                          decision,
                                          confirmTaskOutput,
                                        )
                                      : isExport
                                        ? decideAiFinanceExport(
                                            proposal,
                                            decision,
                                            confirmExport,
                                          )
                                        : isInvoice && decision === "confirm"
                                          ? decideAiWorkspaceAction(
                                              proposal,
                                              decision,
                                              undefined,
                                              undefined,
                                              undefined,
                                              undefined,
                                              undefined,
                                              undefined,
                                              undefined,
                                              undefined,
                                              confirmInvoice,
                                              ...(generatingInvoicePdf
                                                ? [undefined, confirmInvoicePdf]
                                                : deletingInvoice
                                                  ? [confirmInvoiceDelete]
                                                  : []),
                                            )
                                          : isFinancial &&
                                              decision === "confirm"
                                            ? decideAiWorkspaceAction(
                                                proposal,
                                                decision,
                                                undefined,
                                                undefined,
                                                undefined,
                                                undefined,
                                                undefined,
                                                undefined,
                                                undefined,
                                                confirmFinancial,
                                              )
                                            : retryingAutomation &&
                                                decision === "confirm"
                                              ? decideAiWorkspaceAction(
                                                  proposal,
                                                  decision,
                                                  undefined,
                                                  undefined,
                                                  undefined,
                                                  undefined,
                                                  undefined,
                                                  undefined,
                                                  confirmAutomationRetry,
                                                )
                                              : needsAutomationConsent &&
                                                  decision === "confirm"
                                                ? decideAiWorkspaceAction(
                                                    proposal,
                                                    decision,
                                                    undefined,
                                                    undefined,
                                                    undefined,
                                                    undefined,
                                                    undefined,
                                                    confirmAutomation,
                                                  )
                                                : deletingActivity &&
                                                    decision === "confirm"
                                                  ? decideAiWorkspaceAction(
                                                      proposal,
                                                      decision,
                                                      undefined,
                                                      undefined,
                                                      undefined,
                                                      undefined,
                                                      confirmActivityDelete,
                                                    )
                                                  : completingProject &&
                                                      decision === "confirm"
                                                    ? decideAiWorkspaceAction(
                                                        proposal,
                                                        decision,
                                                        confirmIncomplete,
                                                      )
                                                    : forceResolving &&
                                                        decision === "confirm"
                                                      ? decideAiWorkspaceAction(
                                                          proposal,
                                                          decision,
                                                          undefined,
                                                          confirmForceResolve,
                                                        )
                                                      : (startingFocus ||
                                                            includingFocusGap) &&
                                                          decision === "confirm"
                                                        ? decideAiWorkspaceAction(
                                                            proposal,
                                                            decision,
                                                            undefined,
                                                            undefined,
                                                            startingFocus
                                                              ? {
                                                                  start:
                                                                    confirmFocusStart,
                                                                }
                                                              : {
                                                                  includeGap:
                                                                    confirmFocusGap,
                                                                },
                                                          )
                                                        : completingFollowup &&
                                                            decision ===
                                                              "confirm"
                                                          ? decideAiWorkspaceAction(
                                                              proposal,
                                                              decision,
                                                              undefined,
                                                              undefined,
                                                              undefined,
                                                              confirmFollowupCompleted,
                                                            )
                                                          : decideAiWorkspaceAction(
                                                              proposal,
                                                              decision,
                                                            ),
    onMutate: () => setError(null),
    onSuccess: (result) =>
      client.setQueryData<AiActionProposal[]>(
        ["ai", "actions", proposal.generation_id],
        (rows) => rows?.map((row) => (row.id === result.id ? result : row)),
      ),
    onError: (reason, decision) => {
      setError(errorHint(reason, target));
      if (agentRunHasProjectFiles) {
        setConfirmAgentProjectFiles(false);
        setConfirmAgentFiles(false);
        setConfirmAgentExecution(false);
        setConfirmAgentRework(false);
        setConfirmAgentRestart(false);
      }
      if (
        decision === "confirm" &&
        reason instanceof ApiError &&
        ["VERSION_CONFLICT", "AI_ACTION_PREVIEW_CHANGED"].includes(
          reason.code ?? "",
        )
      )
        onConfirmationConflict?.();
    },
    onSettled: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ["ai", "actions", proposal.generation_id],
        }),
        client.invalidateQueries({ queryKey: ["ai", "work-plans"] }),
        client.invalidateQueries({ queryKey: ["ai", "agent-inbox"] }),
        // A decision changes the server-projected receipt of this exact
        // conversation plan. Refresh it now rather than leaving the plan card
        // on its short polling cadence; no proposal body enters the cache.
        sessionId
          ? client.invalidateQueries({
              queryKey: ["ai", "work-plan", sessionId],
            })
          : Promise.resolve(),
        isAgentRun
          ? Promise.all([
              isAgentRunRecovery
                ? invalidateTaskOutputActionFacts(client)
                : invalidateTaskAggregates(client),
              client.invalidateQueries({ queryKey: ["task-agent-runs"] }),
              client.invalidateQueries({ queryKey: ["agent-runs"] }),
            ])
          : isNote
            ? invalidateProjectNoteActionFacts(client)
            : isKnowledge
              ? client.invalidateQueries({ queryKey: ["knowledge"] })
              : isTaskView
                ? Promise.all([
                    client.invalidateQueries({
                      queryKey: ["task-saved-views"],
                    }),
                    client.invalidateQueries({ queryKey: ["tasks"] }),
                  ])
                : isTaskBatch || isTaskBatchCreate || isTaskOrder
                  ? invalidateTaskAggregates(client)
                  : isExport
                    ? Promise.resolve()
                    : isInvoice
                      ? invalidateInvoiceActionFacts(
                          client,
                          proposal.action.action === "invoice.mark_overdue",
                        )
                      : isFinancial
                        ? invalidateFinancialActionFacts(client)
                        : isAutomation
                          ? invalidateAutomationActionFacts(
                              client,
                              retryingAutomation,
                            )
                          : isClientContact
                            ? invalidateClientContactActionFacts(client)
                            : isPerson
                              ? invalidatePersonActionFacts(client)
                              : isClient
                                ? invalidateClientActionFacts(client)
                                : isActivity
                                  ? invalidateClientActivityActionFacts(client)
                                  : isFollowup
                                    ? invalidateClientFollowupActionFacts(
                                        client,
                                      )
                                    : isFocus
                                      ? invalidateFocusActionFacts(client)
                                      : isReminder
                                        ? invalidateReminderActionFacts(client)
                                        : readingAllInbox
                                          ? invalidateInboxReadAllFacts(client)
                                          : isInbox
                                            ? invalidateInboxActionFacts(client)
                                            : isRoadmap
                                              ? invalidateRoadmapActionFacts(
                                                  client,
                                                )
                                              : isContentItem
                                                ? invalidateContentItemActionFacts(
                                                    client,
                                                  )
                                                : isProject
                                                  ? invalidateProjectActionFacts(
                                                      client,
                                                    )
                                                  : isTag
                                                    ? Promise.all([
                                                        client.invalidateQueries(
                                                          {
                                                            queryKey: ["tags"],
                                                          },
                                                        ),
                                                        invalidateTaskAggregates(
                                                          client,
                                                        ),
                                                        invalidateFocusActionFacts(
                                                          client,
                                                        ),
                                                      ])
                                                    : isTaskOutput
                                                      ? invalidateTaskOutputActionFacts(
                                                          client,
                                                        )
                                                      : invalidateTaskAggregates(
                                                          client,
                                                        ),
        ...(isSplit ? [invalidateTaskAggregates(client)] : []),
      ]);
    },
  });
  return (
    <article className="ai-workspace-action-card">
      <header>
        <strong>
          {agentRunHasRestart
            ? "按当前事实重新执行 Agent"
            : aiActionLabels[proposal.action.action]}
        </strong>
        <span role="status">
          {(isAgentDelegation || isAgentFollowup) &&
          proposal.status === "confirmed"
            ? agentDelegationCardStatus(
                isAgentFollowup
                  ? proposal.agent_followup_result?.status
                  : proposal.agent_delegation_result?.status,
              )
            : isAgentRun && proposal.status === "confirmed"
              ? isAgentRunCancelMany
                ? "批量停止请求已接受"
                : agentRunRecordDeleted
                  ? "已确认 · 执行记录已删除"
                  : isAgentRunCancel
                    ? agentRunStatus === "cancelled"
                      ? "执行已停止"
                      : "停止请求已接受 · 等待执行器收尾"
                    : agentRunCardStatus(
                        agentRunStatus,
                        agentRunDeliveryStatus,
                        agentRunSubmissionId,
                        agentRunSubmissionId &&
                          recordedAgentRun?.submission_id ===
                            agentRunSubmissionId
                          ? recordedAgentRun.submission_status
                          : undefined,
                        agentRunQuery.data
                          ? agentRunQuery.data.startGate
                          : recordedAgentRun?.start_gate
                            ? {
                                status: recordedAgentRun.start_gate.status,
                                require: recordedAgentRun.start_gate.require,
                                closeReason: recordedAgentRun.start_gate
                                  .close_reason as AgentRunStartGateCloseReason,
                              }
                            : undefined,
                      )
              : readingAllInbox && proposal.status === "confirmed"
                ? `已执行 · 已标记 ${proposal.inbox_read_all_result?.marked_count ?? proposal.preview.after.marked_count} 条`
                : isExport && proposal.status === "confirmed"
                  ? "已批准 · 下载需手动操作"
                  : retryingAutomation && proposal.status === "confirmed"
                    ? proposal.automation_run_result?.status === "succeeded"
                      ? "重试成功"
                      : "重试失败 · 已保留记录"
                    : actionCardStatus(proposal)}
        </span>
      </header>
      {isAgentDelegation && delegationBatchMode && undecided ? (
        <label className="ai-action-agent-batch-select">
          <input
            type="checkbox"
            checked={delegationBatchSelected}
            disabled={
              proposal.status !== "pending" ||
              !proposal.can_confirm ||
              delegationBatchPending ||
              disabled
            }
            onChange={(event) =>
              onDelegationBatchSelectionChange?.(event.target.checked)
            }
          />
          选择此子任务：{proposal.preview.agent_delegation?.task_name ?? "委派"}
        </label>
      ) : null}
      <p>{proposal.preview.label}</p>
      {isAgentDelegation && proposal.preview.agent_delegation ? (
        <section
          aria-label="子智能体委派预览"
          className="ai-action-agent-details"
        >
          <p>
            独立会话：{proposal.preview.agent_delegation.task_name} · 第{" "}
            {proposal.preview.agent_delegation.sibling_index}/
            {proposal.preview.agent_delegation.max_children} 个子任务
          </p>
          <p>
            Provider：{proposal.preview.agent_delegation.provider_name} ·{" "}
            {proposal.preview.agent_delegation.model}
          </p>
          <p>权限：{proposal.preview.agent_delegation.scopes.join("、")}</p>
          {proposal.preview.agent_delegation.knowledge_sources?.length ? (
            <div className="ai-action-long-text">
              <strong>委派的知识来源</strong>
              <ul>
                {proposal.preview.agent_delegation.knowledge_sources.map(
                  (source) => (
                    <li key={source.source_id}>
                      {source.source_id} · 版本 {source.expected_source_version}
                    </li>
                  ),
                )}
              </ul>
            </div>
          ) : null}
          <div className="ai-action-long-text">
            <strong>将发送给子智能体的完整指令</strong>
            <pre>{proposal.preview.agent_delegation.message}</pre>
          </div>
          <p className="ai-action-note">
            {proposal.preview.agent_delegation.leaves_device
              ? "该指令和已授权工作台内容会发送到远程 Provider，发送后无法撤回。"
              : "使用本地 Provider；指令和已授权内容留在本机。"}
            子会话不能继续创建子智能体；其中的写操作仍需你逐项确认。后续可另行提出需人工确认的续交办，但当前不提供自动合并、等待/中断树或同
            Provider 并行保证。
          </p>
          {proposal.status === "confirmed" &&
          proposal.agent_delegation_result ? (
            <p>
              执行状态：{proposal.agent_delegation_result.status}
              {proposal.agent_delegation_result.error_code
                ? ` · ${proposal.agent_delegation_result.error_code}`
                : ""}
            </p>
          ) : null}
          {proposal.status === "confirmed" &&
          proposal.agent_delegation_result?.pending_approvals ? (
            <p className="ai-action-note">
              子会话还有 {proposal.agent_delegation_result.pending_approvals}{" "}
              项待人工核对；请打开子会话逐项处理，父计划不会因此自动通过。
            </p>
          ) : null}
        </section>
      ) : null}
      {isAgentFollowup && proposal.preview.agent_followup ? (
        <section
          aria-label="子智能体续交办预览"
          className="ai-action-agent-details"
        >
          <p>
            现有子会话：{proposal.preview.agent_followup.task_name} · ID{" "}
            {proposal.preview.agent_followup.target.child_session_id}
          </p>
          <p>
            Provider：{proposal.preview.agent_followup.provider_name} ·{" "}
            {proposal.preview.agent_followup.model}
          </p>
          <p>本次权限：{proposal.preview.agent_followup.scopes.join("、")}</p>
          <p>
            接续基线：子会话版本{" "}
            {proposal.preview.agent_followup.target.child_version} · 上一轮生成{" "}
            {proposal.preview.agent_followup.target.previous_generation_id}
          </p>
          {proposal.preview.agent_followup.knowledge_sources?.length ? (
            <div className="ai-action-long-text">
              <strong>本次知识来源</strong>
              <ul>
                {proposal.preview.agent_followup.knowledge_sources.map(
                  (source) => (
                    <li key={source.source_id}>
                      {source.source_id} · 版本 {source.expected_source_version}
                    </li>
                  ),
                )}
              </ul>
            </div>
          ) : null}
          <div className="ai-action-long-text">
            <strong>将发送给现有子智能体的完整新指令</strong>
            <pre>{proposal.preview.agent_followup.message}</pre>
          </div>
          <p className="ai-action-note">
            {proposal.preview.agent_followup.leaves_device
              ? "本次指令及授权内容会发送到远程 Provider，发送后无法撤回。"
              : "使用本地 Provider；本次指令及授权内容留在本机。"}
            子会话保留此前对话；确认只启动一次新生成，不自动批准其中的操作，也不自动合并回复或完成父任务。
            如子会话已有新消息、待审批操作或 Provider
            变化，服务端会要求重新提议。
          </p>
          {proposal.status === "confirmed" && proposal.agent_followup_result ? (
            <p>
              执行状态：{proposal.agent_followup_result.status}
              {proposal.agent_followup_result.error_code
                ? ` · ${proposal.agent_followup_result.error_code}`
                : ""}
            </p>
          ) : null}
          {proposal.status === "confirmed" &&
          proposal.agent_followup_result?.pending_approvals ? (
            <p className="ai-action-note">
              子会话还有 {proposal.agent_followup_result.pending_approvals}{" "}
              项待人工核对； 请打开子会话逐项处理，父计划不会因此自动通过。
            </p>
          ) : null}
        </section>
      ) : null}
      {isAgentRunRetry && proposal.preview.agent_run_retry ? (
        <div className="ai-action-agent-details">
          <p>
            原执行：{proposal.preview.agent_run_retry.run_id} · 第{" "}
            {proposal.preview.agent_run_retry.attempt} 次
          </p>
          <p>
            按原任务、模型及文件快照重新执行，不覆盖原记录。确认后会再次调用
            Provider，可能再次产生费用；如包含文件，需重新确认发送。
          </p>
        </div>
      ) : null}
      {isAgentRunStart ? <AiAgentRunStartDetails proposal={proposal} /> : null}
      {isAgentRunRecovery && proposal.preview.agent_run_recovery ? (
        <div className="ai-action-agent-details">
          <p>执行 ID：{proposal.preview.agent_run_recovery.run_id}</p>
          <p>
            第 {proposal.preview.agent_run_recovery.attempt} 次执行 · 已保存
            {proposal.preview.agent_run_recovery.output_kind === "file"
              ? "文件"
              : "文本"}
            结果 · {proposal.preview.agent_run_recovery.output_bytes} 字节
          </p>
          <p>
            仅恢复已有结果的登记，不重新调用模型、不重新发送输入文件。符合原执行身份与验收规则时创建提交批次；条件已变化则保留结果并说明原因，不自动完成或验收任务。
          </p>
        </div>
      ) : null}
      {isAgentRunCancel && proposal.preview.agent_run_cancel ? (
        <section aria-label="停止执行预览">
          <p>
            第 {proposal.preview.agent_run_cancel.attempt} 次执行 ·{" "}
            {proposal.preview.agent_run_cancel.model}
          </p>
          <p>执行 ID：{proposal.preview.agent_run_cancel.run_id}</p>
          <p>
            提议时状态：
            {proposal.preview.agent_run_cancel.status === "queued"
              ? "排队中"
              : "运行中"}
          </p>
          <p className="ai-action-note">
            仅停止这一次执行，不取消任务、不删除已有产出。运行中需等待执行器收尾；不能撤回已发出的远程请求或保证停止服务端计费。已完成并待登记的结果不能取消。
          </p>
        </section>
      ) : null}
      {isAgentRunCancelMany && proposal.preview.agent_run_cancel_many ? (
        <section aria-label="批量停止执行预览">
          <p>
            将一次停止 {proposal.preview.agent_run_cancel_many.count} 个 Agent
            执行；确认时任一状态或任务版本变化都会整批拒绝。
          </p>
          <ul className="ai-action-agent-batch-list">
            {proposal.preview.agent_run_cancel_many.items.map((item) => (
              <li key={item.run_id}>
                <strong>{item.task_title}</strong>
                <span>
                  第 {item.attempt} 次 · {item.model} ·
                  {item.status === "queued" ? "排队中" : "运行中"}
                </span>
                <code>{item.run_id}</code>
              </li>
            ))}
          </ul>
          <p className="ai-action-note">
            只请求停止这些执行，不取消任务、不删除产出。已发出的远程请求或服务端计费不保证立刻停止。
          </p>
        </section>
      ) : null}
      {isAgentRun && proposal.status === "confirmed" ? (
        <section aria-label="Agent 产出登记状态">
          {agentRunRecordDeleted ? (
            <p className="ai-action-note">
              任务或执行记录已删除；本次确认历史仍会保留，但无法再读取运行状态或打开原记录。
            </p>
          ) : agentRunDeliveryStatus === "pending" ? (
            <>
              <p className="ai-action-note">
                执行结果已经保留，服务端仍在收口任务产出。这里只会读取状态；再次登记必须由你手动触发。
              </p>
              <button
                className="button button-secondary"
                disabled={
                  retryAgentRunDelivery.isPending ||
                  !agentRunId ||
                  !agentRunTaskId
                }
                onClick={() => {
                  if (!agentRunId || !agentRunTaskId) return;
                  retryAgentRunDelivery.mutate({
                    runId: agentRunId,
                    taskId: agentRunTaskId,
                  });
                }}
                type="button"
              >
                {retryAgentRunDelivery.isPending
                  ? "正在重试登记…"
                  : "重试登记产出"}
              </button>
            </>
          ) : agentRunDeliveryStatus === "submitted" ? (
            <p className="ai-action-note">
              已登记为待验收提交；任务仍需负责人查看证据并人工验收。
            </p>
          ) : agentRunDeliveryStatus === "retained" ? (
            <p className="ai-action-note">
              {agentRunDeliveryReason(agentRunDeliveryErrorCode)}
            </p>
          ) : null}
          {agentRunQuery.isError ? (
            <p role="alert">
              无法刷新精确执行状态，当前仅显示最近一次服务端回执。
              <button
                className="button button-quiet"
                onClick={() => void agentRunQuery.refetch()}
                type="button"
              >
                重新读取执行状态
              </button>
            </p>
          ) : null}
          {retryAgentRunDelivery.isError &&
          agentRunDeliveryStatus === "pending" ? (
            <p role="alert">
              {retryAgentRunDelivery.error instanceof ApiError &&
              retryAgentRunDelivery.error.code ===
                "AGENT_OUTPUT_DELIVERY_PENDING"
                ? "登记仍在恢复中，已保留结果并继续刷新状态；如有需要可稍后再次手动重试。"
                : retryAgentRunDelivery.error instanceof ApiError &&
                    retryAgentRunDelivery.error.code ===
                      "AGENT_OUTPUT_DELIVERY_NOT_PENDING"
                  ? "登记状态已经变化，未重复处理；正在读取最新状态。"
                  : "重试登记失败，结果仍保留在执行记录中。"}
            </p>
          ) : null}
        </section>
      ) : null}
      {isTaskOutput ? <AiTaskOutputDetails proposal={proposal} /> : null}
      {isExport ? (
        <AiFinanceExportDetails key={proposal.id} proposal={proposal} />
      ) : null}
      {deletingInvoice || generatingInvoicePdf ? (
        <section
          aria-label={
            generatingInvoicePdf ? "待生成发票及原 PDF" : "待删除发票及 PDF"
          }
          className="ai-split-preview"
        >
          <p className="ai-action-note">
            {generatingInvoicePdf ? (
              "按下列完整发票信息（含备注、关联名称）在本地生成 PDF。如有旧 PDF 将替换，应用内无法撤销；不改变发票状态、不发送给客户，文件内容不交给模型。下载需在生成后手动点击。资料或旧 PDF 变化后必须重新提议。"
            ) : (
              <>
                永久删除这张草稿及其已存
                PDF，应用内无法撤销，不删除客户或项目；审批预览与审计历史仍保留。
                PDF
                信息来自已存资产记录，不代表当前文件完整性；其他位置的副本和备份不会删除。
                文件被重新生成、发票或关联资料变化后，必须重新提议。
              </>
            )}
          </p>
          <dl>
            {Object.entries(proposal.preview.before).map(([field, value]) => (
              <div key={field}>
                <dt>{aiActionFields[field]}</dt>
                <dd>
                  {field === "amount_minor"
                    ? financialMinorDisplay(
                        Number(value),
                        String(proposal.preview.before.currency),
                      )
                    : display(value, field)}
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ) : null}
      {proposal.invoice_pdf_result && proposal.status === "confirmed" ? (
        <AiInvoicePdfResult
          key={proposal.invoice_pdf_result.asset_id}
          result={proposal.invoice_pdf_result}
        />
      ) : null}
      {isInvoice ? (
        <p className="ai-action-note">
          只记录本地发票事实，不发送发票、不执行银行付款；完整备注及关联名称只在人工卡展示。
          {proposal.action.action === "invoice.mark_paid"
            ? "请先核实已收到所示币种的全额回款和实际日期；确认后创建唯一已确认收入、更新统计并解决关联到期事项，不自动完成任务。"
            : null}
          {proposal.action.action === "invoice.mark_overdue"
            ? "确认时按真实到期日校验；逾期事件可能触发当前已启用的本地自动化任务，停用规则不会撤销已捕获投递。"
            : null}
          {proposal.action.action === "invoice.create"
            ? "确认前不分配编号或创建发票，确认时生成唯一编号。"
            : null}
        </p>
      ) : null}
      {isFinancial ? (
        <p className="ai-action-note">
          只修改本地账本及其统计，不执行银行付款或退款，也不证明资金到账。请核对全部前后值与关联；备注只在本地确认卡展示。
          {proposal.action.action === "financial_entry.void"
            ? "作废保留历史并排除统计，当前不能撤销。"
            : null}
        </p>
      ) : null}
      {retryingAutomation ? (
        <AiAutomationRetryDetails proposal={proposal} />
      ) : null}
      {isAutomation && !retryingAutomation ? (
        <p className="ai-action-note">
          这是持续生效的本地规则，不是仅执行一次的聊天命令。启用后会按所列条件自动创建本地工作事项，不对外发送消息。
          保存不立即运行；变更后的定时计划按确认时刻计算，相同配置保留原计划。
          停用阻止新触发，但不会撤销已捕获事件的投递与重试，也不删除历史和既有对象。配置变更不修改已捕获事件的快照。
        </p>
      ) : null}
      {isNote ? (
        <p className="ai-action-note">
          只记录或修订本地项目笔记，记录人为你；不会写入附件或改变项目生命周期。请核对完整正文和实际时间。删除只隐藏正文，删除历史和审批预览保留，目前不能撤销。
        </p>
      ) : null}
      {isClient ? (
        <p className="ai-action-note">
          只登记或修改本地客户主档。联系人、邮箱和电话仅在这张人工确认卡中展示；备注如来自本次授权的客户快照，已发送给当前所选
          Provider。不会联系客户、发送消息、处理附件或解绑项目。永久删除未开放，请到客户页人工处理。
        </p>
      ) : null}
      {isClientContact ? (
        <p className="ai-action-note">
          只关联已有的本地人员身份，不创建人员、不读取人员备注，也不会联系或发送消息。
          {proposal.action.action === "client_contact.unlink"
            ? "解除只结束当前关系，客户、人员和关系历史都会保留。"
            : "每个客户同时只能有一个有效联系人；确认时会重新核对客户版本和人员状态。"}
        </p>
      ) : null}
      {isPerson ? (
        <p className="ai-action-note">
          只创建或修改本地人员责任身份，不创建登录账号、不发送消息，也不连接外部通讯录。备注只能来自你本次明确提供的内容，并且只在这张人工确认卡展示；人员读取不会把已有备注发送给
          Provider。停用时会重新核对活动责任、客户联系人和计划回访，存在占用时不会自动解绑或改派。
        </p>
      ) : null}
      {isActivity ? (
        <p className="ai-action-note">
          只记录或修订你提供的本地备注/会议，不发送消息、不安排回访，也不代表智能体完成了沟通。确认后的记录归属于你。
          {deletingActivity
            ? "删除后默认时间线隐藏该记录，保留删除历史和附件；当前不支持撤销删除。"
            : "请核对实际发生时间和完整正文。"}
        </p>
      ) : null}
      {isFollowup ? (
        <p className="ai-action-note">
          请核对客户、负责人、UTC
          时间和计划时区。不发送消息、不联系客户，不自动生成客户活动。过去的计划时间会立即逾期；修改或结束原计划会归档旧的到期收件箱提醒，历史仍保留。
          {completingFollowup
            ? "完成操作只记录你已实际完成的回访，须核对结果和实际完成时间；不会代你完成沟通。"
            : null}
          {proposal.action.action === "client_followup.reschedule"
            ? "重新安排会取消并保留旧计划，同时创建下方新计划及关联历史；不会覆盖旧计划。"
            : null}
        </p>
      ) : null}
      {isFocus ? (
        <p className="ai-action-note">{focusActionNote(proposal)}</p>
      ) : null}
      {isReminder ? (
        <p className="ai-action-note">
          请核对 UTC
          触发时间与重复时区。只创建本地收件箱提醒；应用关闭期间不会唤醒，重启后补扫，不发送系统或远程通知。
          {proposal.action.action === "reminder.cancel"
            ? "取消当前待触发记录会停止该系列后续提醒，已触发历史及收件箱事项保留。"
            : "重复规则作用于当前待触发记录及后续次数；按月缺少锚点日期时取月末，工作日不排除法定节假日。"}
        </p>
      ) : null}
      {["task.assign", "task.reassign", "task.unassign"].includes(
        proposal.action.action,
      ) ? (
        <p className="ai-action-note">
          仅修改本地责任记录，保留分派历史；不会发送消息、启用适配器或启动
          Agent。确认前任务状态仅供参考：分派变化会按既有子任务规则重新核对父任务，可能发起或撤回待验收、重开父任务，但不会自动通过验收。
        </p>
      ) : null}
      {["task.create", "task.update"].includes(proposal.action.action) &&
      Object.hasOwn(proposal.action.changes, "tag_ids") ? (
        <p className="ai-action-note">
          标签按上方所示完整替换；“无”会移除该任务的全部标签。确认时会重新核对标签是否仍存在以及任务版本是否变化。
        </p>
      ) : null}
      {deletingTask ? (
        <p className="ai-action-note">
          永久删除任务且应用内无法恢复。直属子任务会移到顶层；所示提交批次、任务产出、责任和
          Agent
          执行历史会一并删除，本地文件产出会移入受控垃圾区后清理；专注与收件箱历史只解除任务关联。
          活动执行、待恢复产出、开放专注、活动收件箱关系或来源、内容日历关联会阻止删除。其他位置的副本和备份不受影响。
        </p>
      ) : null}
      {deletingProject ? (
        <p className="ai-action-note">
          永久删除已归档项目且应用内无法恢复。任务、草稿发票和允许解除的收支记录会保留，但会移除项目关联；项目笔记、附件记录和所示本地附件文件会删除。路线图或内容日历关联、非草稿发票、受保护收支记录及活动
          Agent
          文件引用会阻止删除。确认时会重新核对全部影响；其他位置的副本和备份不受影响。
        </p>
      ) : null}
      {deletingTag ? (
        <p className="ai-action-note">
          永久删除标签且应用内无法恢复。标签本身会删除，并从上方所示数量的任务解除关联；任务不会被删除或改变状态。确认时会重新核对标签版本和关联任务数量，发生变化必须重新提议。
        </p>
      ) : null}
      {["task.create", "task.update"].includes(proposal.action.action) &&
      Object.hasOwn(proposal.action.changes, "parent_task_id") ? (
        <p className="ai-action-note">
          父任务关系按上方所示设置；“无”表示移到顶层。确认时会重新核对任务版本、父任务是否仍存在以及是否形成循环，并按现有规则重新协调原父任务和新父任务的验收状态。
        </p>
      ) : null}
      {proposal.action.action === "task.update" &&
      Object.hasOwn(proposal.action.changes, "review_policy") ? (
        <div className="ai-action-note">
          <p>
            切换验收方式仅在任务为待办且没有任何提交历史时允许，确认时会重新核对。不会自动验收通过、完成任务或启动
            Agent 执行。
          </p>
          <p>
            {proposal.preview.before.review_policy ===
            proposal.preview.after.review_policy
              ? "验收方式保持不变，本次不会因策略切换生成新的子任务汇总提交。"
              : proposal.preview.after.review_policy === "none"
                ? "关闭人工验收后可按任务规则直接完成，不再要求提交产出验收。"
                : "人工验收要求提交产出并由验收人审核；普通任务还需设置负责人和所有者验收人。"}
          </p>
          <p>
            {proposal.preview.after.will_request_parent_review
              ? "当前非取消直属子任务均已完成，且负责人及所有者验收人条件已满足。将自动生成子任务汇总提交，并进入待验收；仍须人工审核。"
              : "按当前冻结的子任务与责任条件，本次设置不会自动提请父任务验收。"}
          </p>
        </div>
      ) : null}
      {!isAgentRun ? (
        <dl>
          {Object.entries(isExport ? {} : proposal.preview.after).map(
            ([field, value]) => (
              <div key={field}>
                <dt>{aiActionFields[field]}</dt>
                <dd>
                  {Object.hasOwn(proposal.preview.before, field) ? (
                    <>
                      <span className="ai-action-before">
                        {(isFinancial || isInvoice) && field === "amount_minor"
                          ? financialMinorDisplay(
                              Number(proposal.preview.before[field]),
                              String(proposal.preview.before.currency),
                            )
                          : display(proposal.preview.before[field], field)}
                      </span>
                      <span aria-label="改为"> → </span>
                    </>
                  ) : null}
                  {(isFinancial || isInvoice) && field === "amount_minor"
                    ? financialMinorDisplay(
                        Number(value),
                        String(proposal.preview.after.currency),
                      )
                    : display(value, field)}
                </dd>
              </div>
            ),
          )}
        </dl>
      ) : null}
      {proposal.status === "confirmed" &&
      proposal.result_id &&
      (isTaskCreate || isTaskUpdate) ? (
        <p aria-label="任务执行结果" className="ai-action-note">
          {isTaskCreate ? "已创建任务" : "已更新任务"}「
          {proposal.preview.label || "未命名任务"}」
          {proposal.route ? (
            <>
              ，
              <Link to={aiWorkspaceHref(proposal.route, sessionId)}>
                打开任务
              </Link>
            </>
          ) : (
            "。"
          )}
        </p>
      ) : null}
      {showGenericActionReceipt ? (
        <p aria-label="操作执行结果" className="ai-action-note">
          已{actionReceiptVerb(proposal.action.action)}
          {target}
          {proposal.preview.label && proposal.preview.label !== target
            ? `「${proposal.preview.label}」`
            : ""}
          {proposal.route ? (
            <>
              ，
              <Link to={aiWorkspaceHref(proposal.route, sessionId)}>
                打开{target}
              </Link>
            </>
          ) : (
            "。"
          )}
        </p>
      ) : null}
      {isTaskView && proposal.preview.task_view ? (
        <section aria-label="任务保存视图" className="ai-split-preview">
          <h4>{proposal.preview.task_view.name}</h4>
          {taskViewFilterRows(proposal.preview.task_view).length ? (
            <dl>
              {taskViewFilterRows(proposal.preview.task_view).map((row) => (
                <div key={row.label}>
                  <dt>{row.label}</dt>
                  <dd>{row.value}</dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="ai-action-note">
              该视图不设筛选条件，等同于全部任务。
            </p>
          )}
          <p className="ai-action-note">
            {deletingTaskView
              ? "确认后只删除这个本地筛选预设，不改动任何任务；当前筛选栏若正在使用它会回到默认条件。"
              : "视图只是任务页的本地筛选预设，不会修改任务，也不会自动应用；确认后你可在任务页自行选用。"}
          </p>
        </section>
      ) : null}
      {isTaskBatch && proposal.preview.task_batch ? (
        <section aria-label="批量任务变更" className="ai-split-preview">
          <p className="ai-action-note">
            以下 {proposal.preview.task_batch.count}{" "}
            个任务会在同一次确认中提交。确认时重新核对每个任务版本及相关项目、标签或责任人；任一项不满足条件则整批不生效。
            {[
              "set_assignee",
              "clear_assignee",
              "set_reviewer",
              "clear_reviewer",
            ].includes(proposal.preview.task_batch.action)
              ? "责任变更不会自动启动智能体，也不会联系人员。"
              : null}
          </p>
          {proposal.preview.task_batch.reason ? (
            <dl>
              <div>
                <dt>原因</dt>
                <dd>{proposal.preview.task_batch.reason}</dd>
              </div>
            </dl>
          ) : null}
          {proposal.preview.task_batch.items.map((item, index) => (
            <article key={item.task_id} className="ai-split-draft">
              <h4>
                {index + 1}. {item.title}
              </h4>
              <dl>
                <div>
                  <dt>任务版本</dt>
                  <dd>{item.version}</dd>
                </div>
                <div>
                  <dt>
                    {item.field === "project"
                      ? "所属项目"
                      : item.field === "priority"
                        ? "优先级"
                        : item.field === "due_date"
                          ? "截止时间"
                          : item.field === "planned_date"
                            ? "计划日期"
                            : item.field === "tags"
                              ? "标签"
                              : item.field === "assignee"
                                ? "负责人"
                                : item.field === "reviewer"
                                  ? "审核人"
                                  : "状态"}
                  </dt>
                  <dd>
                    <span className="ai-action-before">{item.before}</span>
                    <span aria-label="改为"> → </span>
                    {item.after}
                  </dd>
                </div>
                {item.before_actor || item.after_actor ? (
                  <div>
                    <dt>责任人身份</dt>
                    <dd>
                      {item.before_actor
                        ? `${item.before_actor.actor_type} · ${item.before_actor.actor_id} · v${item.before_actor.actor_version}`
                        : "未分派"}
                      <span aria-label="改为"> → </span>
                      {item.after_actor
                        ? `${item.after_actor.actor_type} · ${item.after_actor.actor_id} · v${item.after_actor.actor_version}`
                        : "未分派"}
                    </dd>
                  </div>
                ) : null}
              </dl>
            </article>
          ))}
        </section>
      ) : null}
      {isTaskBatchCreate && proposal.preview.task_batch_create ? (
        <section aria-label="批量创建项目任务" className="ai-split-preview">
          <p className="ai-action-note">
            在「{proposal.preview.task_batch_create.project_name}」中创建{" "}
            {proposal.preview.task_batch_create.count}{" "}
            个任务。确认时重新核对项目版本、责任人和完整草案；任一项失败则整批不创建。仅执行下方列明的初始分派，不会自动启动智能体或联系人员。
          </p>
          {proposal.preview.task_batch_create.items.map((item, index) => (
            <article key={item.key} className="ai-split-draft">
              <h4>
                {index + 1}. {item.title}
              </h4>
              <dl>
                {item.parent_key ? (
                  <div>
                    <dt>父任务</dt>
                    <dd>
                      {proposal.preview.task_batch_create?.items.find(
                        (candidate) => candidate.key === item.parent_key,
                      )?.title ?? item.parent_key}
                    </dd>
                  </div>
                ) : null}
                <div>
                  <dt>类型 / 优先级</dt>
                  <dd>
                    {values[item.kind]} / {item.priority}
                  </dd>
                </div>
                {item.description ? (
                  <div>
                    <dt>描述</dt>
                    <dd>{item.description}</dd>
                  </div>
                ) : null}
                {item.completion_criteria ? (
                  <div>
                    <dt>完成标准</dt>
                    <dd>{item.completion_criteria}</dd>
                  </div>
                ) : null}
                {item.planned_date ? (
                  <div>
                    <dt>计划日期</dt>
                    <dd>{item.planned_date}</dd>
                  </div>
                ) : null}
                {item.due_date ? (
                  <div>
                    <dt>截止时间</dt>
                    <dd>{item.due_date}</dd>
                  </div>
                ) : null}
                {item.estimated_minutes !== null ? (
                  <div>
                    <dt>预计用时</dt>
                    <dd>{item.estimated_minutes} 分钟</dd>
                  </div>
                ) : null}
                {item.tag_names.length ? (
                  <div>
                    <dt>标签</dt>
                    <dd>{item.tag_names.join("、")}</dd>
                  </div>
                ) : null}
                {item.assignments?.map((assignment) => (
                  <div key={assignment.role}>
                    <dt>
                      {assignment.role === "assignee"
                        ? "初始负责人"
                        : "初始审核人"}
                    </dt>
                    <dd>{assignment.actor_name}</dd>
                  </div>
                ))}
                <div>
                  <dt>验收方式</dt>
                  <dd>
                    {item.review_policy === "manual"
                      ? "人工验收"
                      : "无需人工验收"}
                  </dd>
                </div>
              </dl>
            </article>
          ))}
          {proposal.task_batch_create_result ? (
            <div aria-label="已创建任务" className="ai-action-note">
              已创建 {proposal.task_batch_create_result.count} 个任务
              {proposal.preview.task_batch_create.items.some(
                (item) => item.assignments?.length,
              ) ? (
                <>
                  ，其中{" "}
                  {
                    proposal.preview.task_batch_create.items.filter((item) =>
                      item.assignments?.some(
                        (assignment) => assignment.role === "assignee",
                      ),
                    ).length
                  }{" "}
                  个已设置初始负责人
                </>
              ) : null}
              ：{" "}
              {proposal.task_batch_create_result.items.map((item, index) => (
                <span key={item.id}>
                  {index > 0 ? "、" : ""}
                  <Link to={aiWorkspaceHref(item.route, sessionId)}>
                    {item.title}
                  </Link>
                </span>
              ))}
            </div>
          ) : null}
        </section>
      ) : null}
      {isTaskOrder && proposal.preview.task_order ? (
        <section aria-label="任务顺序调整" className="ai-split-preview">
          <h4>{proposal.preview.task_order.title}</h4>
          <dl>
            <div>
              <dt>计划日期组</dt>
              <dd>{proposal.preview.task_order.planned_date ?? "未排期"}</dd>
            </div>
            <div>
              <dt>调整位置</dt>
              <dd>
                活动任务第 {proposal.preview.task_order.before_position} 位
                <span aria-label="改为"> → </span>第{" "}
                {proposal.preview.task_order.after_position} 位，放在「
                {proposal.preview.task_order.anchor_title}」
                {proposal.preview.task_order.placement === "before"
                  ? "之前"
                  : "之后"}
              </dd>
            </div>
            <div>
              <dt>完整组校验</dt>
              <dd>
                {proposal.preview.task_order.group_count} 个任务，其中
                {proposal.preview.task_order.active_count} 个活动任务
              </dd>
            </div>
          </dl>
          <p className="ai-action-note">
            确认时重新核对整组任务与版本；变化后不应用旧建议。已完成和已取消任务保持原有位置，只调整活动任务顺序，不改日期或状态。保存会重新编号整组手动顺序，其他任务的版本也可能更新。
          </p>
        </section>
      ) : null}
      {isRoadmapOrder && proposal.preview.roadmap_order ? (
        <section aria-label="路线图顺序调整" className="ai-split-preview">
          <h4>{proposal.preview.roadmap_order.title}</h4>
          <dl>
            <div>
              <dt>季度</dt>
              <dd>
                {proposal.preview.roadmap_order.year} 年 Q
                {proposal.preview.roadmap_order.quarter}
              </dd>
            </div>
            <div>
              <dt>调整位置</dt>
              <dd>
                第 {proposal.preview.roadmap_order.before_position} 位
                <span aria-label="改为"> → </span>第{" "}
                {proposal.preview.roadmap_order.after_position} 位，放在「
                {proposal.preview.roadmap_order.anchor_title}」
                {proposal.preview.roadmap_order.placement === "before"
                  ? "之前"
                  : "之后"}
              </dd>
            </div>
            <div>
              <dt>完整季度</dt>
              <dd>
                {proposal.preview.roadmap_order.group_count} 个未归档里程碑
              </dd>
            </div>
          </dl>
          <p className="ai-action-note">
            确认时重新核对整个季度；变化后不应用旧建议。保存会重新编号本季度的手动顺序，其他里程碑版本也可能更新；不会改日期、状态或关联项目。
          </p>
        </section>
      ) : null}
      {deletingTaskView && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmTaskViewDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmTaskViewDelete(event.target.checked)}
          />
          我已核对视图名称与筛选条件，同意删除这个任务保存视图
        </label>
      ) : null}
      {isKnowledge && proposal.preview.knowledge ? (
        <section aria-label="知识库来源状态" className="ai-split-preview">
          <h4>{proposal.preview.knowledge.name}</h4>
          <dl>
            <div>
              <dt>来源状态</dt>
              <dd>{proposal.preview.knowledge.status}</dd>
            </div>
            <div>
              <dt>来源版本</dt>
              <dd>{proposal.preview.knowledge.version}</dd>
            </div>
            <div>
              <dt>已生成分段</dt>
              <dd>{proposal.preview.knowledge.chunk_count}</dd>
            </div>
            {proposal.preview.knowledge.job ? (
              <>
                <div>
                  <dt>索引任务</dt>
                  <dd>
                    {proposal.preview.knowledge.job.operation} ·{" "}
                    {proposal.preview.knowledge.job.status} · 第{" "}
                    {proposal.preview.knowledge.job.attempt} 次
                  </dd>
                </div>
                {proposal.preview.knowledge.job.error_code ? (
                  <div>
                    <dt>错误码</dt>
                    <dd>{proposal.preview.knowledge.job.error_code}</dd>
                  </div>
                ) : null}
              </>
            ) : null}
          </dl>
          <p className="ai-action-note">
            {creatingKnowledgeSource
              ? "下面是要写入知识库的正文开头；确认后会保存为你自己的受管副本并建立本地索引。库内其它来源的正文、提取文本和文件字节不会进入本次对话。"
              : "这里只展示来源与索引元数据，库内正文、提取文本和文件字节不会发送给模型。"}
            {deletingKnowledgeSource
              ? "永久删除会移除该来源、已生成索引与受管副本，无应用内撤销。"
              : creatingKnowledgeSource
                ? "不会扫描磁盘或读取本地文件。"
                : proposal.action.action === "knowledge_index_job.cancel"
                  ? "取消索引不会删除已有资料，只停止本次重建。"
                  : "重建索引会按受管副本重新生成分段，已保存的历史索引任务保留。"}
          </p>
          {creatingKnowledgeSource ? (
            <>
              <dl>
                <div>
                  <dt>内容字节</dt>
                  <dd>{proposal.preview.knowledge.proposed_content_bytes}</dd>
                </div>
                <div>
                  <dt>内容 SHA-256</dt>
                  <dd>{proposal.preview.knowledge.proposed_content_sha256}</dd>
                </div>
              </dl>
              {proposal.preview.knowledge.proposed_excerpt ? (
                <pre className="ai-knowledge-excerpt">
                  {proposal.preview.knowledge.proposed_excerpt}
                  {proposal.preview.knowledge.proposed_excerpt_truncated
                    ? "\n…（仅显示开头，完整正文以你上一条消息为准）"
                    : null}
                </pre>
              ) : null}
            </>
          ) : null}
        </section>
      ) : null}
      {deletingKnowledgeSource && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmKnowledgeDelete}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmKnowledgeDelete(event.target.checked)
            }
          />
          我已核对来源名称、版本、已生成分段数量与删除范围，同意永久删除该知识库来源、索引和受管副本
        </label>
      ) : null}
      {isTaskOutput && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmTaskOutput}
            disabled={mutation.isPending}
            onChange={(e) => setConfirmTaskOutput(e.target.checked)}
          />
          {proposal.action.action === "task.submit_output"
            ? "我已核实完整内容、产出归属和跟进事项，同意以人工提交记入待验收"
            : "我已人工检查本批次完整证据（含文件），确认所示验收决定及其任务影响"}
        </label>
      ) : null}
      {isAgentRunCancel && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentCancel}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentCancel(event.target.checked)}
          />
          {isAgentRunCancelMany
            ? `我已逐项核对这 ${proposal.preview.agent_run_cancel_many?.count ?? 0} 个执行与任务，同意整批发出停止请求；这不等于取消任务`
            : "我已核对执行 ID 与任务，同意停止这一次 Agent 执行；这不等于取消任务"}
        </label>
      ) : null}
      {(isAgentDelegation || isAgentFollowup) &&
      undecided &&
      !(isAgentDelegation && delegationBatchMode) ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentDelegation}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmAgentDelegation(event.target.checked)
            }
          />
          {isAgentFollowup
            ? "我已核对现有子会话、完整新指令、Provider、模型和本次权限，同意在该子会话启动一次新生成；这不代表任务完成或操作已执行"
            : "我已核对完整子任务指令、Provider、模型和权限子集，同意创建独立会话并启动一次后台生成；这不代表任务完成或操作已执行"}
        </label>
      ) : null}
      {isAgentRunStart && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentExecution}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentExecution(event.target.checked)}
          />
          {proposal.preview.agent_run_start?.start_gate
            ? "我已核对任务完整快照、Agent、Adapter、Provider、运行限制与上方启动条件，同意在条件满足后由系统启动这一次执行；我会人工检查结果，且执行成功不代表任务完成或验收通过"
            : "我已核对任务完整快照、Agent、Adapter、Provider 与运行限制，同意现在启动一次执行；我会人工检查结果，且执行成功不代表任务完成或验收通过"}
        </label>
      ) : null}
      {agentRunHasRestart && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentRestart}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentRestart(event.target.checked)}
          />
          我确认关联此原运行，按上方当前任务、分派、模型和明确选择的资料创建新执行；这不是原样重试，不覆盖旧结果，也不自动验收
        </label>
      ) : null}
      {isAgentRunRecovery && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentRecovery}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentRecovery(event.target.checked)}
          />
          我确认恢复这一次执行的已有产出登记，结果仍需人工检查和验收
        </label>
      ) : null}
      {agentRunHasRework && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentRework}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentRework(event.target.checked)}
          />
          我已核对完整返工意见与所选旧产出，同意发送给
          {proposal.preview.agent_run_start?.provider.kind === "remote"
            ? "远程 Provider（内容将离开本机，发送后无法撤回）"
            : "本地 Provider（内容留在本机）"}
          ；这不代替执行或输入文件确认
        </label>
      ) : null}
      {agentRunHasProjectFiles && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentProjectFiles}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmAgentProjectFiles(event.target.checked)
            }
          />
          我另行确认所列跨任务文件来自同项目已完成任务的当前已验收批次，同意交给本次
          Agent 使用；这不代替文件正文发送许可，也不代表当前任务已验收
        </label>
      ) : null}
      {isAgentRun && agentRunHasInputFiles && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAgentFiles}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAgentFiles(event.target.checked)}
          />
          我已核对上方每个受控输入文件的名称、来源、大小与
          SHA-256，同意执行器读取并发送正文给
          {proposal.preview.agent_run_start?.provider.kind === "remote"
            ? `远程 Provider「${proposal.preview.agent_run_start.provider.name}」（文件将离开本机）`
            : `本地 Provider「${proposal.preview.agent_run_start?.provider.name}」（文件留在本机）`}
        </label>
      ) : null}
      {proposal.preview.next_followup ? (
        <section aria-label="下一次回访计划" className="ai-split-preview">
          <h4>
            {completingFollowup ? "完成后安排下一次回访" : "重新安排的新计划"}
          </h4>
          <p className="ai-action-note">
            与原计划的处理一起提交，任一步失败则全部不生效。确认后结果 ID
            对应这条新计划；原计划保留。
          </p>
          <dl>
            {Object.entries(proposal.preview.next_followup).map(
              ([field, value]) => (
                <div key={field}>
                  <dt>{aiActionFields[field]}</dt>
                  <dd>{display(value, field)}</dd>
                </div>
              ),
            )}
          </dl>
        </section>
      ) : null}
      {isSplit && proposal.status === "confirmed" ? (
        <div className="ai-action-note" aria-label="拆分任务结果">
          {proposal.created_task_ids ? (
            <>
              已创建 {proposal.created_task_ids.length}{" "}
              个任务（历史结果，打开后读取当前状态）：
              {proposal.created_task_ids.map((id, index) => (
                <span key={id}>
                  {index > 0 ? "、" : ""}
                  <Link to={aiWorkspaceHref(`/tasks/${id}`, sessionId)}>
                    任务 {index + 1}
                  </Link>
                </span>
              ))}
            </>
          ) : (
            "此历史审批未保存拆分任务的精确对应，请在收件箱查看当前关联任务；当前列表不代表当时的创建结果。"
          )}
        </div>
      ) : null}
      {proposal.preview.tasks ? (
        <section aria-label="整批任务预览" className="ai-split-preview">
          <p className="ai-action-note">
            以下 {proposal.preview.tasks.length}{" "}
            项将一次性创建并分派。任一项失败则整批回滚，不会自动执行
            Agent，也不会向人员发送消息。
          </p>
          {proposal.preview.tasks.map((draft, index) => (
            <article key={draft.key} className="ai-split-draft">
              <h4>
                {index + 1}. {String(draft.fields.title)}
              </h4>
              <dl>
                <div>
                  <dt>父任务</dt>
                  <dd>
                    {draft.parent_key
                      ? String(
                          proposal.preview.tasks?.find(
                            (item) => item.key === draft.parent_key,
                          )?.fields.title,
                        )
                      : "无（根任务）"}
                  </dd>
                </div>
                <div>
                  <dt>必需任务</dt>
                  <dd>{draft.is_required ? "是" : "否"}</dd>
                </div>
                <div>
                  <dt>负责人</dt>
                  <dd>
                    {draft.assignee.label}（
                    {draft.assignee.type === "person"
                      ? "本地责任记录"
                      : draft.assignee.type === "agent"
                        ? "智能体，仅分派不启动"
                        : "本人"}
                    ）
                  </dd>
                </div>
                <div>
                  <dt>验收人</dt>
                  <dd>{draft.reviewer?.label ?? "无需验收"}</dd>
                </div>
                <div>
                  <dt>标签</dt>
                  <dd>
                    {draft.tags.map((tag) => tag.label).join("、") || "无"}
                  </dd>
                </div>
                {Object.entries(draft.fields)
                  .filter(([key]) => key !== "title")
                  .map(([field, value]) => (
                    <div key={field}>
                      <dt>{aiActionFields[field]}</dt>
                      <dd>
                        {field === "project_id"
                          ? (draft.project?.label ?? "未归项目")
                          : display(value, field)}
                      </dd>
                    </div>
                  ))}
              </dl>
            </article>
          ))}
        </section>
      ) : null}
      {proposal.action.expected_version ? (
        <small>
          {retryingAutomation
            ? `基于原运行捕获的规则版本 ${proposal.action.expected_version}；不是当前规则版本，也不是运行记录版本。`
            : `基于${isAgentRun ? "任务" : target}版本 ${proposal.action.expected_version}；目标变化后需重新提议。`}
        </small>
      ) : null}
      {completingProject ? (
        <p className="ai-action-note">
          完成项目不会自动完成、取消或删除其中的任务。
        </p>
      ) : null}
      {isInbox ? (
        <p className="ai-action-note">
          {readingAllInbox
            ? "只处理所示服务端快照内、当时可见且确认前未发生变化的未读事项。快照后新到、恢复显示或被修改的事项不会包含；此操作不会分诊、解决或忽略事项，也不会完成关联任务、取消提醒或修改来源。"
            : isSplit
              ? "此操作新建任务并关联当前事项；不会完成已有任务。只有活动必需任务全部完成，自动策略才会解决事项；人工验收由本人处理。"
              : inboxTaskAction
                ? "此操作只调整任务关联，不会完成、删除或分派任务。自动策略可能据此解决收件箱事项；解除后历史仍保留。"
                : "此操作只改变收件箱事项，不会完成关联任务或更改原始来源。"}
          {proposal.action.action === "inbox.snooze"
            ? "稍后只调整可见性，不会新建提醒。"
            : null}
        </p>
      ) : null}
      {proposal.action.action === "project.archive" ? (
        <p className="ai-action-note">
          归档后从默认项目列表隐藏，不能再向该项目添加任务；已有任务保留。
        </p>
      ) : null}
      {isRoadmap ? (
        <p className="ai-action-note">
          {deletingRoadmap
            ? "永久删除会解除所示项目关系，并把已终结的来源事项保留为来源已删除的审计快照；活动来源事项会阻止执行。关联项目与任务本身不会被删除，外部副本和备份不受影响。"
            : isRoadmapOrder
              ? "此操作只调整本地路线图的季度手动顺序，不修改关联项目、任务或收件箱事实。"
              : "此操作只修改本地路线图及其项目关联；相关临期或达成收件箱投影会按工作台现有规则同步。不会修改关联项目或任务，也不会永久删除里程碑。"}
        </p>
      ) : null}
      {isContentItem ? (
        <p className="ai-action-note">
          {deletingContentItem
            ? "永久删除会解除所示准备任务关系，并把已终结的来源事项保留为来源已删除的审计快照；活动来源事项会阻止执行。关联项目与任务本身不会被删除，外部副本和备份不受影响。"
            : publishingContentItem
              ? "这只记录你已在外部平台完成发布的本地事实，并终结旧的内容到期事项；应用不会发布、访问或验证外部链接。请独立核实实际发布内容与时间，不要仅凭智能体的说法确认。"
              : contentItemTaskAction
                ? "此操作只新增、调整或解除内容条目与现有任务的准备关系；不会创建、完成、删除、分派或改变任务状态。确认时会重新核对内容条目版本、任务版本和当前关系。"
                : "此操作只修改本地内容计划及其既有收件箱投影；不会访问外部链接、向平台发布、手工重排，或修改关联项目与任务状态。发布完成仍须由你在内容日历中明确确认。"}
        </p>
      ) : null}
      {needsIncompleteConsent && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmIncomplete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmIncomplete(event.target.checked)}
          />
          我确认保留这 {proposal.preview.after.incomplete_task_count}{" "}
          项未完成任务，并完成项目
        </label>
      ) : null}
      {deletingNote && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmNoteDelete}
            disabled={mutation.isPending}
            onChange={(e) => setConfirmNoteDelete(e.target.checked)}
          />
          我确认软删除这条项目笔记，了解历史与审批预览保留且无法撤销
        </label>
      ) : null}
      {deletingActivity && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmActivityDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmActivityDelete(event.target.checked)}
          />
          我确认按所示原因删除这条客户活动，保留历史和附件，且当前无法撤销
        </label>
      ) : null}
      {deletingTask && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmTaskDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmTaskDelete(event.target.checked)}
          />
          我已核对全部删除影响，确认永久删除这个任务及所示本地历史和文件，了解应用内无法恢复
        </label>
      ) : null}
      {deletingProject && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmProjectDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmProjectDelete(event.target.checked)}
          />
          我已核对全部删除和解除关联影响，确认永久删除这个已归档项目及所示本地笔记、附件记录和文件，了解应用内无法恢复
        </label>
      ) : null}
      {deletingClient ? (
        <p className="ai-action-note">
          永久删除会解除所示项目的客户关联，并删除本地客户活动、联系人关系历史、附件记录和附件文件。发票、收支记录或回访仍存在时会拒绝执行；外部副本和备份不受影响。
        </p>
      ) : null}
      {deletingClient && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmClientDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmClientDelete(event.target.checked)}
          />
          我已核对全部删除和解除关联影响，确认永久删除这个已停用客户及所示本地历史和文件，了解应用内无法恢复
        </label>
      ) : null}
      {deletingContentItem && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmContentItemDelete}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmContentItemDelete(event.target.checked)
            }
          />
          我已核对全部删除和关系影响，确认永久删除这个已归档内容条目，了解应用内无法恢复
        </label>
      ) : null}
      {publishingContentItem && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmContentPublished}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmContentPublished(event.target.checked)
            }
          />
          我已在外部平台独立核实该内容确实发布，并核对上方实际时间和链接；同意把它记录为本地已发布事实
        </label>
      ) : null}
      {deletingRoadmap && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmRoadmapDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmRoadmapDelete(event.target.checked)}
          />
          我已核对全部删除和关系影响，确认永久删除这个已归档路线图里程碑，了解应用内无法恢复
        </label>
      ) : null}
      {deletingTag && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmTagDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmTagDelete(event.target.checked)}
          />
          我已核对关联任务数量，确认永久删除这个标签并解除所示关联，了解应用内无法恢复
        </label>
      ) : null}
      {isExport && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmExport}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmExport(event.target.checked)}
          />
          我已核对全部导出范围和列，同意导出含备注及关联名称的本地
          CSV；确认后仍需手动下载
        </label>
      ) : null}
      {isFinancial && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmFinancial}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmFinancial(event.target.checked)}
          />
          我已核对收支类型、金额、币种、日期、状态和关联，确认按所示变更更新本地账本及统计
        </label>
      ) : null}
      {isInvoice && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmInvoice}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmInvoice(event.target.checked)}
          />
          {generatingInvoicePdf
            ? "我已核对将写入 PDF 的发票全部字段、备注及关联名称"
            : deletingInvoice
              ? "我已核对待删除草稿的编号、全部字段及已存 PDF 信息"
              : proposal.action.action === "invoice.mark_paid"
                ? "我确认已实际收到所示全额回款，收款日期属实，并同意入账及解决到期事项"
                : "我已核实发票全部前后值及实际状态，确认更新本地记录；逾期操作可能触发现有自动化"}
        </label>
      ) : null}
      {deletingInvoice && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmInvoiceDelete}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmInvoiceDelete(event.target.checked)}
          />
          我确认永久删除这张草稿及其已存 PDF，无法撤销；审批和审计历史仍保留
        </label>
      ) : null}
      {generatingInvoicePdf && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmInvoicePdf}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmInvoicePdf(event.target.checked)}
          />
          我同意生成本地 PDF，并替换已有
          PDF（如有）；旧文件无应用内撤销，不自动发送或下载
        </label>
      ) : null}
      {forceResolving && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmForceResolve}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmForceResolve(event.target.checked)}
          />
          我确认按所示原因例外结清此事项；保留{" "}
          {proposal.preview.after.required_remaining_count}{" "}
          项未完成必需任务，不改变任务状态或验收结果
        </label>
      ) : null}
      {startingFocus && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmFocusStart}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmFocusStart(event.target.checked)}
          />
          {proposal.preview.after.task_id === null
            ? "我确认不绑定任务，并以新循环替代当前本地休息和轮次"
            : "我确认按所示任务开始新循环，替代当前本地休息和轮次"}
        </label>
      ) : null}
      {completingFollowup && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmFollowupCompleted}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmFollowupCompleted(event.target.checked)
            }
          />
          我确认回访已实际完成，所示结果与完成时间属实
        </label>
      ) : null}
      {includingFocusGap && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmFocusGap}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmFocusGap(event.target.checked)}
          />
          我确认中断期间持续工作，将最后心跳到确认时刻的间隔计入本次专注
        </label>
      ) : null}
      {error && undecided ? <p role="alert">{error}</p> : null}
      {retryingAutomation && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAutomationRetry}
            disabled={mutation.isPending}
            onChange={(event) =>
              setConfirmAutomationRetry(event.target.checked)
            }
          />
          我确认按原快照重试该本地动作，保留既有最多三次尝试与自动退避规则
        </label>
      ) : null}
      {needsAutomationConsent && undecided ? (
        <label className="ai-action-consent">
          <input
            type="checkbox"
            checked={confirmAutomation}
            disabled={mutation.isPending}
            onChange={(event) => setConfirmAutomation(event.target.checked)}
          />
          我同意该规则持续自动创建所列本地工作事项，直到停用（已捕获事件仍会投递）
        </label>
      ) : null}
      <footer>
        {undecided ? (
          <>
            <button
              className="button button-primary"
              type="button"
              disabled={
                proposal.status !== "pending" ||
                !proposal.can_confirm ||
                (needsIncompleteConsent && !confirmIncomplete) ||
                (forceResolving && !confirmForceResolve) ||
                (startingFocus && !confirmFocusStart) ||
                (includingFocusGap && !confirmFocusGap) ||
                (completingFollowup && !confirmFollowupCompleted) ||
                (deletingNote && !confirmNoteDelete) ||
                (deletingActivity && !confirmActivityDelete) ||
                (deletingTask && !confirmTaskDelete) ||
                (deletingProject && !confirmProjectDelete) ||
                (deletingClient && !confirmClientDelete) ||
                (deletingContentItem && !confirmContentItemDelete) ||
                (publishingContentItem && !confirmContentPublished) ||
                (deletingRoadmap && !confirmRoadmapDelete) ||
                (deletingTag && !confirmTagDelete) ||
                (deletingKnowledgeSource && !confirmKnowledgeDelete) ||
                (deletingTaskView && !confirmTaskViewDelete) ||
                (isExport && !confirmExport) ||
                (isFinancial && !confirmFinancial) ||
                (isTaskOutput && !confirmTaskOutput) ||
                ((isAgentDelegation || isAgentFollowup) &&
                  !confirmAgentDelegation) ||
                (isAgentRunStart && !confirmAgentExecution) ||
                (isAgentRunCancel && !confirmAgentCancel) ||
                (isAgentRunRecovery && !confirmAgentRecovery) ||
                (agentRunHasInputFiles && !confirmAgentFiles) ||
                (agentRunHasProjectFiles && !confirmAgentProjectFiles) ||
                (agentRunHasRework && !confirmAgentRework) ||
                (agentRunHasRestart && !confirmAgentRestart) ||
                (isInvoice && !confirmInvoice) ||
                (deletingInvoice && !confirmInvoiceDelete) ||
                (generatingInvoicePdf && !confirmInvoicePdf) ||
                (needsAutomationConsent && !confirmAutomation) ||
                (retryingAutomation && !confirmAutomationRetry) ||
                (delegationBatchMode && isAgentDelegation) ||
                mutation.isPending ||
                disabled
              }
              onClick={() => mutation.mutate("confirm")}
            >
              {mutation.isPending
                ? "正在处理…"
                : isExport
                  ? "确认导出范围"
                  : isAgentDelegation
                    ? "确认委派子智能体"
                    : isAgentFollowup
                      ? "确认续交办子智能体"
                      : isAgentRunRecovery
                        ? "确认恢复登记"
                        : isAgentRunCancel
                          ? isAgentRunCancelMany
                            ? `确认停止 ${proposal.preview.agent_run_cancel_many?.count ?? 0} 个执行`
                            : "确认停止执行"
                          : isAgentRunStart
                            ? agentRunHasRestart
                              ? "确认按当前事实重新执行"
                              : isAgentRunRetry
                                ? "确认重试 Agent"
                                : "确认启动 Agent"
                            : readingAllInbox
                              ? `确认标记 ${proposal.preview.after.marked_count} 条`
                              : isSplit
                                ? "确认创建整批任务"
                                : deletingTask
                                  ? "确认永久删除"
                                  : deletingProject
                                    ? "确认永久删除项目"
                                    : deletingClient
                                      ? "确认永久删除客户"
                                      : deletingContentItem
                                        ? "确认永久删除内容"
                                        : publishingContentItem
                                          ? "确认记录已发布"
                                          : deletingRoadmap
                                            ? "确认永久删除里程碑"
                                            : deletingTag
                                              ? "确认永久删除标签"
                                              : deletingKnowledgeSource
                                                ? "确认永久删除知识库来源"
                                                : creatingKnowledgeSource
                                                  ? "确认存入知识库"
                                                  : deletingTaskView
                                                    ? "确认删除任务视图"
                                                    : proposal.action.action ===
                                                        "task_view.create"
                                                      ? "确认保存任务视图"
                                                      : proposal.action
                                                            .action ===
                                                          "task_view.update"
                                                        ? "确认修改任务视图"
                                                        : "确认执行"}
            </button>
            <button
              className="button button-secondary"
              type="button"
              disabled={
                mutation.isPending ||
                disabled ||
                (delegationBatchMode && isAgentDelegation)
              }
              onClick={() => mutation.mutate("reject")}
            >
              拒绝
            </button>
          </>
        ) : null}
        {actionRoute ? (
          <Link to={aiWorkspaceHref(actionRoute, sessionId)}>
            查看
            {isNote
              ? actionRoute.includes("?note=")
                ? "项目笔记"
                : "所属项目"
              : isAgentRun
                ? proposal.status === "confirmed" ||
                  isAgentRunCancel ||
                  isAgentRunRetry ||
                  isAgentRunRecovery
                  ? agentRunDeliveryStatus === "submitted"
                    ? "提交批次"
                    : "执行过程"
                  : "任务"
                : isAgentDelegation || isAgentFollowup
                  ? "子会话"
                  : readingAllInbox
                    ? "收件箱"
                    : deletingInvoice && proposal.status === "confirmed"
                      ? "发票列表"
                      : isTaskOutput && actionRoute.includes("/submissions/")
                        ? "提交批次"
                        : target}
          </Link>
        ) : null}
        {isAutomation && !proposal.route ? (
          <button
            className="button button-secondary"
            type="button"
            onClick={() => setSettingsOpen(true, "automation")}
          >
            打开自动化设置
          </button>
        ) : null}
        {isPerson ? (
          <button
            className="button button-secondary"
            type="button"
            onClick={() => setSettingsOpen(true, "actors")}
          >
            打开人员与责任设置
          </button>
        ) : null}
        {inboxTaskAction &&
        typeof proposal.preview.after.task_id === "string" ? (
          <Link to={`/tasks/${proposal.preview.after.task_id}`}>
            查看关联任务
          </Link>
        ) : null}
      </footer>
    </article>
  );
}
