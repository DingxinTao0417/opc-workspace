import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Bot, FileText } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  aiWorkPlanActiveSteps,
  aiWorkPlanRecordLinkLabel,
  aiWorkPlanProgress,
  getAiWorkPlan,
  planInboxStateLabels,
  planStateLabels,
  type AiWorkPlan,
} from "../api/aiWorkPlan";
import { aiActionLabels } from "../api/aiWorkspaceActions";
import {
  cancelAiAgentChildGeneration,
  cancelAiAgentChildGenerations,
  type AiAgentChildGenerationCancelBatchResult,
  type AiAgentChildGenerationCancelTarget,
} from "../api/aiAgentFamily";
import { cancelAgentRun, retryAgentRun } from "../api/client";
import {
  aiAgentFamilyQueryKey,
  invalidateAgentRunDeliveryFacts,
  useAiAgentInboxQuery,
  useAiAgentFamilyQuery,
  useAiSessionDelegatedRunsQuery,
  useAiWorkPlanInboxQuery,
  useAgentRunQuery,
  useAgentRunsQuery,
  useControlledFilesQuery,
  useRetryAgentRunOutputDelivery,
  useTaskArtifactQuery,
} from "../api/hooks";
import type { AiSessionDelegatedRunFacts } from "../api/aiSessionAgentRuns";
import type { AiAgentFamilyStatus } from "../api/aiAgentFamily";
import {
  agentRunIssueHandoff,
  taskArtifactHandoff,
} from "../lib/aiIssueHandoff";
import {
  aiContinuationHref,
  type AiContinuationLocation,
} from "../lib/aiContinuationLocation";
import { aiWorkspaceHref, isAiWorkspaceRoute } from "../lib/aiWorkspaceLinks";
import type {
  AgentRun,
  AgentRunSummary,
  ControlledFile,
  ControlledFileScope,
} from "../types/models";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import { agentRunDeliveryReason } from "../lib/agentRunDelivery";
import { taskSubmissionHref } from "../lib/taskSubmissionLocation";
import {
  agentRunFileApplyCandidates,
  useAgentRunFileApply,
} from "../store/agentRunFileApply";
import { useAiChatStore } from "../store/aiChat";
import { useUiStore } from "../store/ui";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import { AgentReworkRetryModal } from "./AgentReworkRetryModal";
import { AgentRunRestartModal } from "./AgentRunRestartModal";
import { canRestartAgentRun } from "../lib/agentRunRestart";
import { agentRunFailureReason } from "../lib/agentRunFailureSource";
import {
  agentRunProgressLabel,
  agentRunProgressStatus,
} from "../lib/agentRunProgress";
import { agentRunLifecycle } from "../lib/agentRunLifecycle";

function agentRunRowLabel(run: AgentRunSummary): string {
  const lifecycle = agentRunLifecycle(run);
  return lifecycle.phase === "running" && run.progress
    ? agentRunProgressLabel(run.progress)
    : lifecycle.label;
}

type PlanAttention = {
  label: string;
  className: string;
};

function currentPlanAttention(plan: AiWorkPlan): PlanAttention {
  switch (aiWorkPlanProgress(plan).state) {
    case "needs_recovery":
      return { label: "结果待恢复", className: "is-warning" };
    case "needs_approval":
      return { label: "等待确认", className: "is-warning" };
    case "running":
      return { label: "正在执行", className: "is-running" };
    case "completed":
      return { label: "计划已完成", className: "is-complete" };
    default:
      return { label: "待继续", className: "is-idle" };
  }
}

/**
 * The right-side Agent Run ledger and the conversation plan deliberately stay
 * separate facts. This compact card only mirrors the active main session's
 * already-authorized plan steps and sends the user back to the original
 * conversation or verified workbench record. It never turns a workbench tab into a
 * chat input, scope grant, approval, or Run control surface.
 */
function CurrentConversationPlan() {
  const sessionId = useAiChatStore((state) => state.activeSessionId);
  const planUpdateVersion = useAiChatStore(
    (state) => state.workPlanVersions[sessionId] ?? 0,
  );
  const query = useQuery({
    queryKey: ["ai", "work-plan", sessionId, "latest", planUpdateVersion],
    queryFn: ({ signal }) => getAiWorkPlan(sessionId, signal),
    enabled: isWorkspaceIdentity(sessionId),
    retry: false,
    refetchInterval: (current) => {
      const plan = current.state.data;
      return plan?.generation_status === "streaming" ||
        (plan ? aiWorkPlanActiveSteps(plan) : []).some(
          (step) =>
            step.kind === "action" &&
            ["pending", "queued", "running", "output_pending"].includes(
              step.state,
            ),
        )
        ? 3_000
        : false;
    },
  });
  if (!isWorkspaceIdentity(sessionId)) return null;
  if (query.isPending || query.isError)
    return (
      <section className="ov-plan" aria-label="当前主对话计划">
        <div className="ov-plan-heading">当前主对话计划</div>
        {query.isPending ? (
          <small className="ov-plan-note" role="status">
            正在读取当前计划…
          </small>
        ) : (
          <>
            <small className="ov-plan-note" role="alert">
              计划读取失败，当前状态无法核对。
            </small>
            <button
              className="form-inline-action ov-plan-link"
              onClick={() => void query.refetch()}
              type="button"
            >
              重试读取计划
            </button>
          </>
        )}
      </section>
    );
  const plan = query.data;
  if (!plan) return null;
  const attention = currentPlanAttention(plan);
  const progress = aiWorkPlanProgress(plan);
  const active = progress.active_step_total;
  const recoverable = progress.recovery_step_total;
  const steps = aiWorkPlanActiveSteps(plan);

  return (
    <section className="ov-plan" aria-label="当前主对话计划">
      <div className="ov-plan-heading">
        <span>当前主对话计划</span>
        <small>版本 {plan.version}</small>
      </div>
      <div className="ov-plan-title" title={plan.title}>
        {plan.title}
      </div>
      <div className="ov-plan-meta">
        <span className={`ov-plan-state ${attention.className}`}>
          {attention.label}
        </span>
        <span>
          已满足 {progress.satisfied_step_total}/{progress.effective_step_total}
        </span>
        {progress.superseded_step_total > 0 ? (
          <span>历史替代 {progress.superseded_step_total} 步</span>
        ) : null}
        {active > 0 ? <span>执行中 {active}</span> : null}
        {recoverable > 0 ? <span>待恢复 {recoverable}</span> : null}
      </div>
      <ol className="ov-plan-steps" aria-label="计划步骤">
        {steps.map((step) => {
          const proposalHref = step.evidence
            ? aiContinuationHref({
                kind: "approval",
                sessionId,
                generationId: step.evidence.generation_id,
                proposalId: step.evidence.proposal_id,
              })
            : "";
          const route = step.evidence?.route;
          const recordHref =
            route && isAiWorkspaceRoute(route)
              ? aiWorkspaceHref(route, sessionId)
              : "";
          return (
            <li className="ov-plan-step" key={step.id}>
              <span className="ov-plan-step-title" title={step.title}>
                {step.title}
              </span>
              <small className="ov-plan-step-state">
                {step.kind === "analysis" && step.state === "pending"
                  ? "尚待分析"
                  : (planStateLabels[step.state] ?? "状态待核对")}
                {!step.ready ? " · 前置步骤未满足" : ""}
              </small>
              {proposalHref || recordHref ? (
                <span className="ov-plan-step-links">
                  {proposalHref ? (
                    <Link to={proposalHref}>查看原对话建议</Link>
                  ) : null}
                  {recordHref ? (
                    <Link to={recordHref}>
                      {aiWorkPlanRecordLinkLabel(step)}
                    </Link>
                  ) : null}
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>
      <Link
        className="ov-plan-link"
        to={aiContinuationHref({ kind: "plan", sessionId })}
      >
        在主对话查看并继续
      </Link>
      <small className="ov-plan-note">
        分析状态由模型报告，操作状态以真实回执为准；继续、授权、确认和恢复仍在原对话中人工完成。
      </small>
    </section>
  );
}
function AgentRunDetail({
  summary,
  onBack,
}: {
  summary: AgentRunSummary;
  onBack: () => void;
}) {
  const runQuery = useAgentRunQuery(summary.id, summary.taskId);

  return (
    <div className="ov-detail">
      <button className="ov-back" onClick={onBack} type="button">
        <ArrowLeft size={14} /> 返回列表
      </button>
      <div className="ov-detail-title" title={summary.taskTitle}>
        {summary.taskTitle}
      </div>
      {runQuery.isPending ? (
        <div className="ov-empty">正在读取执行详情…</div>
      ) : runQuery.isError ? (
        <div className="ov-empty" role="alert">
          执行详情读取失败 ·{" "}
          <button
            className="form-inline-action"
            onClick={() => void runQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : runQuery.data ? (
        <LoadedAgentRunDetail
          run={runQuery.data}
          taskTitle={summary.taskTitle}
        />
      ) : (
        <div className="ov-empty">执行详情不可用</div>
      )}
    </div>
  );
}

function LoadedAgentRunDetail({
  run,
  taskTitle,
}: {
  run: AgentRun;
  taskTitle: string;
}) {
  const queryClient = useQueryClient();
  const activeSessionId = useAiChatStore((state) => state.activeSessionId);
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const retryDelivery = useRetryAgentRunOutputDelivery();
  const deliveryPending = run.outputDeliveryStatus === "pending";
  const fileApplyCandidates = agentRunFileApplyCandidates(run, taskTitle);
  const [reworkRetryOpen, setReworkRetryOpen] = useState(false);
  const [restartOpen, setRestartOpen] = useState(false);
  const retry = useMutation({
    mutationFn: () => retryAgentRun(run.id),
    onSuccess: (nextRun) =>
      void invalidateAgentRunDeliveryFacts(queryClient, nextRun.taskId),
  });
  const cancel = useMutation({
    mutationFn: () => cancelAgentRun(run.id),
    onSuccess: () =>
      void invalidateAgentRunDeliveryFacts(queryClient, run.taskId),
  });
  const terminal =
    run.status === "succeeded" ||
    run.status === "failed" ||
    run.status === "cancelled" ||
    run.status === "interrupted";
  const retryable =
    terminal &&
    (![4, 5, 6].includes(run.executionContractVersion ?? 0) ||
      (run.status !== "succeeded" && run.outputDeliveryStatus === "not_ready"));

  return (
    <>
      {restartOpen ? (
        <AgentRunRestartModal
          key={run.id}
          taskId={run.taskId}
          runId={run.id}
          onClose={() => setRestartOpen(false)}
          onSuccess={(next) => {
            setRestartOpen(false);
            void invalidateAgentRunDeliveryFacts(queryClient, next.taskId);
          }}
        />
      ) : null}
      {reworkRetryOpen ? (
        <AgentReworkRetryModal
          key={run.id}
          taskId={run.taskId}
          runId={run.id}
          onClose={() => setReworkRetryOpen(false)}
          onSuccess={(nextRun) => {
            setReworkRetryOpen(false);
            void invalidateAgentRunDeliveryFacts(queryClient, nextRun.taskId);
          }}
        />
      ) : null}
      <div className="ov-detail-meta">
        状态 {agentRunLifecycle(run).label} · 尝试 {run.attempt} · 模型{" "}
        {run.model}
      </div>
      <p className="ov-detail-meta">{agentRunLifecycle(run).detail}</p>
      {run.status === "running" && run.progress ? (
        <div className="ov-detail-meta" role="status" aria-live="polite">
          {agentRunProgressStatus(run.progress)}
        </div>
      ) : null}
      <div className="ov-detail-meta">
        开始 {run.startedAt ?? "—"} · 结束 {run.completedAt ?? "—"}
      </div>
      {run.restartOfRunId ? (
        <p className="ov-detail-meta">
          按当前事实重新执行 · 关联原运行 <code>{run.restartOfRunId}</code>
          ；不覆盖旧结果。
        </p>
      ) : null}
      {run.errorCode ? (
        <div className="ov-detail-error">
          错误：{run.errorCode}
          {agentRunFailureReason(run.errorCode) ? (
            <p>{agentRunFailureReason(run.errorCode)}</p>
          ) : null}
        </div>
      ) : null}
      {run.outputDeliveryStatus === "submitted" && run.submissionId ? (
        <div className="ov-detail-meta">
          已创建待验收提交；执行成功没有代替人工验收。{" "}
          <Link
            to={aiWorkspaceHref(
              taskSubmissionHref(run.taskId, run.submissionId),
              activeSessionId,
            )}
          >
            查看待验收提交
          </Link>
        </div>
      ) : run.outputDeliveryStatus === "retained" ? (
        <div className="ov-detail-meta">
          {agentRunDeliveryReason(run.outputDeliveryErrorCode)}
        </div>
      ) : null}
      {run.resultText ? (
        <pre className="ov-detail-result">{run.resultText}</pre>
      ) : deliveryPending ? (
        <div className="ov-detail-meta">
          结果已进入受保护的恢复暂存区；完成登记前不会显示为任务产出。
        </div>
      ) : (
        <div className="ov-detail-meta">暂无产出文本</div>
      )}
      <div className="ov-detail-actions">
        <AiIssueHandoffButton
          content={agentRunIssueHandoff({
            ...run,
            taskTitle,
          })}
        />
        <button
          className="button button-secondary"
          onClick={() => setTaskDetailId(run.taskId)}
          type="button"
        >
          打开任务
        </button>
        {fileApplyCandidates.map((candidate) => (
          <button
            className="button button-secondary"
            key={`${candidate.artifactId}:${candidate.index}`}
            onClick={() => {
              if (!useAgentRunFileApply.getState().select(candidate)) return;
              useUiStore.setState({ rightOverviewCollapsed: false });
              useUiStore.getState().setRightPanelTab("files", "activate");
            }}
            type="button"
          >
            用于项目文件：{candidate.name}
          </button>
        ))}
        {retryable ? (
          <button
            className="button button-secondary"
            disabled={retry.isPending}
            onClick={() =>
              [4, 5, 6].includes(run.executionContractVersion ?? 0)
                ? setReworkRetryOpen(true)
                : retry.mutate()
            }
            type="button"
          >
            重试
          </button>
        ) : null}
        {canRestartAgentRun(run) ? (
          <button
            type="button"
            className="button button-secondary"
            disabled={retry.isPending || cancel.isPending}
            onClick={() => setRestartOpen(true)}
          >
            按当前事实重新执行
          </button>
        ) : null}
        {run.status === "queued" ||
        (run.status === "running" && !deliveryPending) ? (
          <button
            className="button button-secondary"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate()}
            type="button"
          >
            取消
          </button>
        ) : null}
        {deliveryPending ? (
          <button
            className="button button-secondary"
            disabled={retryDelivery.isPending}
            onClick={() =>
              retryDelivery.mutate({ runId: run.id, taskId: run.taskId })
            }
            type="button"
          >
            {retryDelivery.isPending ? "正在恢复…" : "重试登记产出"}
          </button>
        ) : null}
      </div>
      {retry.isError || cancel.isError || retryDelivery.isError ? (
        <p role="alert" className="ws-error">
          操作失败，请刷新执行记录后重试。
        </p>
      ) : null}
    </>
  );
}

/**
 * The workspace keeps execution controls and chat plans separate. This is a
 * metadata-only bridge to attention that belongs to another saved session or
 * its native review surface; it never makes the workspace panel a second
 * proposal/approval UI or gives the model any workspace capability.
 */
function WorkspaceAttentionInbox() {
  const navigate = useNavigate();
  const openAiInbox = useUiStore((state) => state.openAiInbox);
  const isStreaming = useAiChatStore((state) => state.streaming !== null);
  const setActiveSessionId = useAiChatStore(
    (state) => state.setActiveSessionId,
  );
  const planInbox = useAiWorkPlanInboxQuery();
  const agentInbox = useAiAgentInboxQuery();
  const plans = useMemo(
    () =>
      planInbox.isError
        ? []
        : (planInbox.data?.pages.flatMap((page) => page.data) ?? []),
    [planInbox.data, planInbox.isError],
  );
  const agentItems = useMemo(
    () =>
      agentInbox.isError
        ? []
        : (agentInbox.data?.pages.flatMap((page) => page.data) ?? []),
    [agentInbox.data, agentInbox.isError],
  );
  const planMeta = planInbox.isError
    ? undefined
    : planInbox.data?.pages[0]?.meta;
  const agentMeta = agentInbox.isError
    ? undefined
    : agentInbox.data?.pages[0]?.meta;
  const pending = planInbox.isPending || agentInbox.isPending;
  const failed = planInbox.isError || agentInbox.isError;
  const approvalItems = agentItems.filter((item) => item.kind === "approval");
  const reviewItems = agentItems.filter((item) => item.kind === "review");
  const visibleCount =
    Math.min(plans.length, 3) +
    Math.min(approvalItems.length, 3) +
    Math.min(reviewItems.length, 3);
  const total =
    (planMeta?.attention_total ?? 0) +
    (agentMeta?.approval_total ?? 0) +
    (agentMeta?.review_total ?? 0);
  const openSession = (target: AiContinuationLocation) => {
    const href = aiContinuationHref(target);
    if (!href) return;
    setActiveSessionId(target.sessionId);
    navigate(href);
  };

  if (!total && !pending && !failed) return null;
  return (
    <section className="ov-plan" aria-label="跨会话续办队列">
      <div className="ov-plan-heading">
        <span>跨会话续办</span>
        <small>
          {total > 0 ? `${total > 99 ? "99+" : total} 项` : "数量暂不可用"}
          {failed && total > 0 ? " · 数量不完整" : null}
          {pending ? " · 正在读取" : null}
        </small>
      </div>
      <small className="ov-plan-note">
        这里只镜像已保存的计划、待确认建议和待验收提交；点击后仍回到原会话或原记录完成授权、确认与验收。
      </small>
      {pending ? <div className="ov-empty">正在读取续办事项…</div> : null}
      {plans.slice(0, 3).map((plan) => (
        <button
          className="ov-row"
          disabled={isStreaming}
          key={`plan:${plan.session_id}`}
          onClick={() =>
            openSession({ kind: "plan", sessionId: plan.session_id })
          }
          title={`${plan.plan_title} · ${plan.session_title}`}
          type="button"
        >
          <span className="ov-row-icon" aria-hidden="true">
            <Bot size={14} />
          </span>
          <span className="ov-label">{plan.plan_title}</span>
          <span className="ov-value">{planInboxStateLabels[plan.state]}</span>
        </button>
      ))}
      {approvalItems.slice(0, 3).map((item) => (
        <button
          className="ov-row"
          disabled={isStreaming}
          key={`approval:${item.id}`}
          onClick={() =>
            openSession({
              kind: "approval",
              sessionId: item.session_id,
              generationId: item.generation_id,
              proposalId: item.id,
            })
          }
          title={`${item.session_title} · 等待确认`}
          type="button"
        >
          <span className="ov-row-icon" aria-hidden="true">
            <Bot size={14} />
          </span>
          <span className="ov-label">
            {aiActionLabels[item.action as keyof typeof aiActionLabels] ??
              item.action}
          </span>
          <span className="ov-value">等待确认</span>
        </button>
      ))}
      {reviewItems.slice(0, 3).map((item) => (
        <button
          className="ov-row"
          key={`review:${item.id}`}
          onClick={() =>
            navigate(`/tasks/${item.task_id}/submissions/${item.submission_id}`)
          }
          title={`${item.task_title} · 第 ${item.sequence} 次提交`}
          type="button"
        >
          <span className="ov-row-icon" aria-hidden="true">
            <FileText size={14} />
          </span>
          <span className="ov-label">{item.task_title}</span>
          <span className="ov-value">待验收</span>
        </button>
      ))}
      {total > visibleCount ||
      (!planInbox.isError && planInbox.hasNextPage) ||
      (!agentInbox.isError && agentInbox.hasNextPage) ||
      planMeta?.window_limited ||
      agentMeta?.window_limited ? (
        <button
          className="form-inline-action ov-plan-link"
          onClick={() => {
            openAiInbox();
            navigate("/ai");
          }}
          type="button"
        >
          在智能体侧栏查看全部续办事项
        </button>
      ) : null}
      {failed ? (
        <div className="ov-empty" role="alert">
          部分续办事项读取失败 ·{" "}
          <button
            className="form-inline-action"
            onClick={() => {
              if (planInbox.isError) void planInbox.refetch();
              if (agentInbox.isError) void agentInbox.refetch();
            }}
            type="button"
          >
            重试
          </button>
        </div>
      ) : null}
    </section>
  );
}

const agentFamilyStatusLabels: Record<AiAgentFamilyStatus, string> = {
  queued: "等待启动",
  streaming: "执行中",
  completed: "已完成",
  failed: "执行失败",
  cancelled: "已取消",
  unavailable: "会话不可用",
};

function CurrentConversationAgentFamily({ sessionId }: { sessionId: string }) {
  const enabled = isWorkspaceIdentity(sessionId);
  const queryClient = useQueryClient();
  const query = useAiAgentFamilyQuery(sessionId, enabled);
  const [pendingCancel, setPendingCancel] = useState<{
    childSessionId: string;
    generationId: string;
    requested: boolean;
  } | null>(null);
  const [selectedBatchStops, setSelectedBatchStops] = useState<
    AiAgentChildGenerationCancelTarget[]
  >([]);
  const [batchReviewTargets, setBatchReviewTargets] = useState<
    AiAgentChildGenerationCancelTarget[]
  >([]);
  const [confirmingBatchStop, setConfirmingBatchStop] = useState(false);
  const [batchStopAccepted, setBatchStopAccepted] = useState<
    AiAgentChildGenerationCancelBatchResult["items"]
  >([]);
  const cancelChildGeneration = useMutation({
    mutationFn: ({
      childSessionId,
      generationId,
    }: {
      childSessionId: string;
      generationId: string;
    }) => cancelAiAgentChildGeneration(sessionId, childSessionId, generationId),
    onSuccess: async (result) => {
      setPendingCancel({
        childSessionId: result.childSessionId,
        generationId: result.generationId,
        requested: true,
      });
      await queryClient.invalidateQueries({
        queryKey: aiAgentFamilyQueryKey(sessionId),
      });
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: aiAgentFamilyQueryKey(sessionId),
      });
    },
  });
  const cancelChildGenerations = useMutation({
    mutationFn: (targets: AiAgentChildGenerationCancelTarget[]) =>
      cancelAiAgentChildGenerations(sessionId, targets),
    onSuccess: async (result) => {
      setBatchStopAccepted(result.items);
      setSelectedBatchStops([]);
      setBatchReviewTargets([]);
      setConfirmingBatchStop(false);
      await queryClient.invalidateQueries({
        queryKey: aiAgentFamilyQueryKey(sessionId),
      });
    },
    onError: async () => {
      await queryClient.invalidateQueries({
        queryKey: aiAgentFamilyQueryKey(sessionId),
      });
    },
  });
  useEffect(() => {
    if (!query.data) return;
    const stillCurrent = (target: AiAgentChildGenerationCancelTarget) => {
      const child = query.data.children.find(
        (item) => item.sessionId === target.childSessionId,
      );
      return child?.activeGenerationId === target.generationId;
    };
    setSelectedBatchStops((current) => {
      const next = current.filter(stillCurrent);
      return next.length === current.length ? current : next;
    });
    setBatchStopAccepted((current) => {
      const next = current.filter((target) => {
        const child = query.data!.children.find(
          (item) => item.sessionId === target.childSessionId,
        );
        return (
          child?.activeGenerationId === target.generationId ||
          (child?.activeGenerationId === null &&
            (child.status === "queued" || child.status === "streaming"))
        );
      });
      return next.length === current.length ? current : next;
    });
    if (!pendingCancel) return;
    const child = query.data.children.find(
      (item) => item.sessionId === pendingCancel.childSessionId,
    );
    const stillActive =
      child?.activeGenerationId === pendingCancel.generationId ||
      (pendingCancel.requested &&
        child?.activeGenerationId === null &&
        (child.status === "queued" || child.status === "streaming"));
    if (!stillActive) setPendingCancel(null);
  }, [pendingCancel, query.data]);
  if (!enabled) return null;
  if (query.isPending) {
    return <div className="ov-empty">正在读取子会话…</div>;
  }
  if (query.isError) {
    return (
      <div className="ov-empty">
        子会话读取失败 ·{" "}
        <button
          className="form-inline-action"
          onClick={() => void query.refetch()}
          type="button"
        >
          重试
        </button>
      </div>
    );
  }
  const family = query.data;
  if (!family || (!family.parent && family.children.length === 0)) return null;
  const batchReviewIsCurrent =
    batchReviewTargets.length > 0 &&
    batchReviewTargets.every((target) =>
      family.children.some(
        (child) =>
          child.sessionId === target.childSessionId &&
          child.activeGenerationId === target.generationId,
      ),
    );
  const cancellationInProgress =
    cancelChildGeneration.isPending ||
    pendingCancel !== null ||
    cancelChildGenerations.isPending;
  return (
    <section className="ov-agent-family" aria-label="当前对话子会话">
      <div className="ov-group-title">子会话</div>
      {!family.parent &&
      (selectedBatchStops.length > 0 || confirmingBatchStop) ? (
        <div className="ov-agent-family-batch">
          {confirmingBatchStop ? (
            <section aria-label="批量停止确认">
              <span>
                将请求停止 {batchReviewTargets.length} 个当前子会话生成：
              </span>
              <ul>
                {batchReviewTargets.map((target) => {
                  const child = family.children.find(
                    (item) => item.sessionId === target.childSessionId,
                  );
                  return (
                    <li key={target.childSessionId}>
                      {child?.taskName ?? "已变化的子会话"}
                    </li>
                  );
                })}
              </ul>
              <small>
                这只发送本地停止请求；不能撤回已发出的远端请求、计费或已经发生的操作。
              </small>
              {!batchReviewIsCurrent ? (
                <small role="alert">
                  子会话状态已变化。取消本次核对并重新选择后再试。
                </small>
              ) : null}
              <button
                className="form-inline-action"
                disabled={
                  !batchReviewIsCurrent ||
                  cancelChildGenerations.isPending ||
                  cancellationInProgress
                }
                onClick={() =>
                  cancelChildGenerations.mutate(batchReviewTargets)
                }
                type="button"
              >
                {cancelChildGenerations.isPending
                  ? "正在请求…"
                  : `确认停止 ${batchReviewTargets.length} 个`}
              </button>
              <button
                className="form-inline-action"
                disabled={cancelChildGenerations.isPending}
                onClick={() => {
                  setConfirmingBatchStop(false);
                  setBatchReviewTargets([]);
                }}
                type="button"
              >
                取消
              </button>
            </section>
          ) : (
            <button
              className="form-inline-action"
              disabled={cancellationInProgress}
              onClick={() => {
                setBatchReviewTargets(selectedBatchStops);
                setConfirmingBatchStop(true);
              }}
              type="button"
            >
              停止所选 ({selectedBatchStops.length})
            </button>
          )}
          {cancelChildGenerations.isError ? (
            <small role="alert">
              批量停止请求失败；状态已刷新，请核对后重试。
            </small>
          ) : null}
        </div>
      ) : null}
      {family.parent ? (
        <Link
          className="ov-row ov-agent-run-row"
          to={`/ai?session=${family.parent.sessionId}`}
        >
          <span className="ov-row-icon" aria-hidden="true">
            <Bot size={14} />
          </span>
          <span className="ov-agent-run-copy">
            <span className="ov-label">返回父会话</span>
            <small>当前子任务：{family.parent.taskName}</small>
          </span>
          <span className="ov-value">打开</span>
        </Link>
      ) : null}
      {family.children.map((child) => {
        const isConfirming =
          pendingCancel?.childSessionId === child.sessionId &&
          pendingCancel.generationId === child.activeGenerationId &&
          !pendingCancel.requested;
        const isCancelRequested =
          (pendingCancel?.childSessionId === child.sessionId &&
            pendingCancel.requested &&
            (pendingCancel.generationId === child.activeGenerationId ||
              (child.activeGenerationId === null &&
                (child.status === "queued" ||
                  child.status === "streaming")))) ||
          batchStopAccepted.some(
            (target) =>
              target.childSessionId === child.sessionId &&
              (target.generationId === child.activeGenerationId ||
                (child.activeGenerationId === null &&
                  (child.status === "queued" || child.status === "streaming"))),
          );
        const isCancelPending =
          cancelChildGeneration.isPending &&
          cancelChildGeneration.variables?.childSessionId === child.sessionId &&
          cancelChildGeneration.variables?.generationId ===
            child.activeGenerationId;
        return (
          <div className="ov-agent-family-entry" key={child.proposalId}>
            {child.available ? (
              <Link
                className="ov-row ov-agent-run-row"
                to={`/ai?session=${child.sessionId}`}
              >
                <span className="ov-row-icon" aria-hidden="true">
                  <Bot size={14} />
                </span>
                <span className="ov-agent-run-copy">
                  <span className="ov-label">{child.taskName}</span>
                  {child.errorCode ? <small>{child.errorCode}</small> : null}
                  {child.pendingApprovals > 0 ? (
                    <small>待人工核对 {child.pendingApprovals} 项</small>
                  ) : null}
                </span>
                <span className="ov-value">
                  {agentFamilyStatusLabels[child.status]}
                </span>
              </Link>
            ) : (
              <div className="ov-row ov-agent-run-row">
                <span className="ov-row-icon" aria-hidden="true">
                  <Bot size={14} />
                </span>
                <span className="ov-label">{child.taskName}</span>
                <span className="ov-value">会话不可用</span>
              </div>
            )}
            {!family.parent && child.activeGenerationId ? (
              <label className="ov-agent-family-selection">
                <input
                  aria-label={`选择停止 ${child.taskName}`}
                  checked={selectedBatchStops.some(
                    (target) =>
                      target.childSessionId === child.sessionId &&
                      target.generationId === child.activeGenerationId,
                  )}
                  disabled={
                    cancellationInProgress ||
                    confirmingBatchStop ||
                    isCancelRequested
                  }
                  onChange={(event) => {
                    const target = {
                      childSessionId: child.sessionId,
                      generationId: child.activeGenerationId as string,
                    };
                    setSelectedBatchStops((current) =>
                      event.target.checked
                        ? [...current, target]
                        : current.filter(
                            (item) => item.childSessionId !== child.sessionId,
                          ),
                    );
                  }}
                  type="checkbox"
                />
                选择停止本轮
              </label>
            ) : null}
            {!family.parent &&
            (child.activeGenerationId || isCancelRequested) ? (
              <div className="ov-agent-family-controls">
                {isCancelRequested ? (
                  <span role="status">已请求停止 · 等待状态更新</span>
                ) : isConfirming ? (
                  <>
                    <span>确认停止本轮生成？</span>
                    <span aria-live="polite">
                      仅请求中止；已发送的远端请求或已完成操作不会撤回。
                    </span>
                    <button
                      className="form-inline-action"
                      disabled={isCancelPending}
                      onClick={() =>
                        cancelChildGeneration.mutate({
                          childSessionId: child.sessionId,
                          generationId: child.activeGenerationId as string,
                        })
                      }
                      type="button"
                    >
                      {isCancelPending ? "正在请求…" : "确认停止"}
                    </button>
                    <button
                      className="form-inline-action"
                      disabled={isCancelPending}
                      onClick={() => setPendingCancel(null)}
                      type="button"
                    >
                      取消
                    </button>
                  </>
                ) : (
                  <button
                    className="form-inline-action"
                    disabled={
                      selectedBatchStops.length > 0 ||
                      confirmingBatchStop ||
                      cancelChildGenerations.isPending
                    }
                    onClick={() =>
                      setPendingCancel({
                        childSessionId: child.sessionId,
                        generationId: child.activeGenerationId!,
                        requested: false,
                      })
                    }
                    type="button"
                  >
                    停止
                  </button>
                )}
                {cancelChildGeneration.isError &&
                cancelChildGeneration.variables?.childSessionId ===
                  child.sessionId ? (
                  <small role="alert">停止请求失败；请检查状态后重试。</small>
                ) : null}
              </div>
            ) : null}
          </div>
        );
      })}
    </section>
  );
}

export function AgentsTab() {
  const activeSessionId = useAiChatStore((state) => state.activeSessionId);
  const hasActiveConversation = isWorkspaceIdentity(activeSessionId);
  const [filter, setFilter] = useState<"conversation" | "all" | "pending">(
    () => (hasActiveConversation ? "conversation" : "all"),
  );
  const [page, setPage] = useState(1);
  const pageSize = 20;
  const listInput = {
    page,
    pageSize,
    ...(filter === "pending"
      ? { outputDeliveryStatus: "pending" as const }
      : {}),
  };
  const globalRunsQuery = useAgentRunsQuery(
    listInput,
    filter !== "conversation",
  );
  const conversationRunsQuery = useAiSessionDelegatedRunsQuery(
    activeSessionId,
    listInput,
    filter === "conversation" && hasActiveConversation,
  );
  const [selected, setSelected] = useState<{
    summary: AgentRunSummary;
    delegation: AiSessionDelegatedRunFacts | null;
  } | null>(null);
  useEffect(() => {
    setFilter(hasActiveConversation ? "conversation" : "all");
    setPage(1);
    setSelected(null);
  }, [activeSessionId, hasActiveConversation]);
  const runsQuery =
    filter === "conversation" ? conversationRunsQuery : globalRunsQuery;
  const delegatedRuns = conversationRunsQuery.data?.items ?? [];
  const runs =
    filter === "conversation"
      ? delegatedRuns.flatMap((item) => (item.run ? [item.run] : []))
      : (globalRunsQuery.data?.items ?? []);
  const meta = runsQuery.data?.meta;
  const pageCount = meta ? Math.max(1, Math.ceil(meta.total / pageSize)) : 1;

  if (selected) {
    const sourceHref = selected.delegation
      ? aiContinuationHref({
          kind: "approval",
          sessionId: selected.delegation.sessionId,
          generationId: selected.delegation.generationId,
          proposalId: selected.delegation.proposalId,
        })
      : "";
    return (
      <>
        <CurrentConversationPlan />
        <CurrentConversationAgentFamily sessionId={activeSessionId} />
        {selected.delegation ? (
          <section
            className="ov-detail ov-agent-delegation"
            aria-label="当前对话委派来源"
          >
            <div className="ov-detail-meta">
              {selected.delegation.plan
                ? `计划步骤：${selected.delegation.plan.stepTitle}`
                : "当前对话委派 · 未绑定当前计划"}
            </div>
            {sourceHref ? <Link to={sourceHref}>查看原确认回执</Link> : null}
          </section>
        ) : null}
        <AgentRunDetail
          summary={selected.summary}
          onBack={() => setSelected(null)}
        />
      </>
    );
  }

  return (
    <section className="ov-group">
      <div className="ov-group-title">Agent 执行</div>
      <CurrentConversationPlan />
      <CurrentConversationAgentFamily sessionId={activeSessionId} />
      <WorkspaceAttentionInbox />
      <div className="ov-scope-bar" aria-label="Agent 执行筛选">
        {hasActiveConversation ? (
          <button
            className={`ov-scope${filter === "conversation" ? " ov-scope-active" : ""}`}
            onClick={() => {
              setFilter("conversation");
              setPage(1);
            }}
            type="button"
          >
            当前对话
          </button>
        ) : null}
        <button
          className={`ov-scope${filter === "all" ? " ov-scope-active" : ""}`}
          onClick={() => {
            setFilter("all");
            setPage(1);
          }}
          type="button"
        >
          全部记录
        </button>
        <button
          className={`ov-scope${filter === "pending" ? " ov-scope-active" : ""}`}
          onClick={() => {
            setFilter("pending");
            setPage(1);
          }}
          type="button"
        >
          待登记 {meta?.pendingDeliveryTotal ?? 0}
        </button>
      </div>
      {filter !== "pending" && (meta?.pendingDeliveryTotal ?? 0) > 0 ? (
        <button
          className="ov-row"
          onClick={() => {
            setFilter("pending");
            setPage(1);
          }}
          type="button"
        >
          <span className="ov-row-icon" aria-hidden="true">
            <Bot size={14} />
          </span>
          <span className="ov-label">有产出等待恢复登记</span>
          <span className="ov-value">{meta?.pendingDeliveryTotal} 个</span>
        </button>
      ) : null}
      {runsQuery.isPending ? (
        <div className="ov-empty">正在读取执行记录…</div>
      ) : runsQuery.isError ? (
        <div className="ov-empty">
          读取失败 ·{" "}
          <button
            className="form-inline-action"
            onClick={() => void runsQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : filter === "conversation" && delegatedRuns.length === 0 ? (
        <div className="ov-empty">当前对话尚未委派 Agent 执行</div>
      ) : filter !== "conversation" && runs.length === 0 ? (
        <div className="ov-empty">暂无 Agent 执行记录</div>
      ) : (
        <>
          {filter === "conversation"
            ? delegatedRuns.map((item) => {
                const sourceHref = aiContinuationHref({
                  kind: "approval",
                  sessionId: item.delegation.sessionId,
                  generationId: item.delegation.generationId,
                  proposalId: item.delegation.proposalId,
                });
                const context = item.delegation.plan
                  ? `计划步骤：${item.delegation.plan.stepTitle}`
                  : "当前对话委派 · 未绑定当前计划";
                if (!item.run) {
                  return (
                    <Link
                      className="ov-row ov-agent-run-row"
                      key={item.runId}
                      to={sourceHref}
                    >
                      <span className="ov-row-icon" aria-hidden="true">
                        <Bot size={14} />
                      </span>
                      <span className="ov-agent-run-copy">
                        <span className="ov-label">执行记录已不可用</span>
                        <small>{context} · 查看历史确认回执</small>
                      </span>
                      <span className="ov-value">历史回执</span>
                    </Link>
                  );
                }
                return (
                  <button
                    className="ov-row ov-agent-run-row"
                    key={item.runId}
                    onClick={() =>
                      setSelected({
                        summary: item.run!,
                        delegation: item.delegation,
                      })
                    }
                    type="button"
                  >
                    <span className="ov-row-icon" aria-hidden="true">
                      <Bot size={14} />
                    </span>
                    <span className="ov-agent-run-copy">
                      <span className="ov-label" title={item.run.taskTitle}>
                        {item.run.taskTitle}
                      </span>
                      <small>{context}</small>
                    </span>
                    <span className="ov-value">
                      {agentRunRowLabel(item.run)}
                    </span>
                  </button>
                );
              })
            : runs.map((run) => (
                <button
                  className="ov-row"
                  key={run.id}
                  onClick={() =>
                    setSelected({ summary: run, delegation: null })
                  }
                  type="button"
                >
                  <span className="ov-row-icon" aria-hidden="true">
                    <Bot size={14} />
                  </span>
                  <span className="ov-label" title={run.taskTitle}>
                    {run.taskTitle}
                  </span>
                  <span className="ov-value">{agentRunRowLabel(run)}</span>
                </button>
              ))}
          {pageCount > 1 ? (
            <div className="ov-detail-actions" aria-label="Agent 执行分页">
              <button
                className="button button-secondary"
                disabled={page <= 1}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
                type="button"
              >
                上一页
              </button>
              <span className="ov-detail-meta">
                第 {page} / {pageCount} 页
              </span>
              <button
                className="button button-secondary"
                disabled={page >= pageCount}
                onClick={() =>
                  setPage((current) => Math.min(pageCount, current + 1))
                }
                type="button"
              >
                下一页
              </button>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

const fileScopes: { id: ControlledFileScope; label: string }[] = [
  { id: "artifact", label: "任务产出" },
  { id: "client_attachment", label: "客户附件" },
  { id: "project_attachment", label: "项目附件" },
  { id: "knowledge_document", label: "知识库" },
];

export function ManagedFilesTab() {
  const [scope, setScope] = useState<ControlledFileScope>("artifact");
  const [selected, setSelected] = useState<ControlledFile | null>(null);
  const filesQuery = useControlledFilesQuery({ pageSize: 50, scope });
  const artifactQuery = useTaskArtifactQuery(
    selected?.scope === "artifact" ? selected.id : null,
  );
  const files = filesQuery.data?.items ?? [];

  return (
    <>
      <div className="ov-scope-bar">
        {fileScopes.map((item) => (
          <button
            className={`ov-scope${scope === item.id ? " ov-scope-active" : ""}`}
            key={item.id}
            onClick={() => {
              setScope(item.id);
              setSelected(null);
            }}
            type="button"
          >
            {item.label}
          </button>
        ))}
      </div>
      <section className="ov-group">
        <div className="ov-group-title">文件（只读）</div>
        {filesQuery.isPending ? (
          <div className="ov-empty">正在读取文件…</div>
        ) : filesQuery.isError ? (
          <div className="ov-empty">
            读取失败 ·{" "}
            <button
              className="form-inline-action"
              onClick={() => void filesQuery.refetch()}
              type="button"
            >
              重试
            </button>
          </div>
        ) : files.length === 0 ? (
          <div className="ov-empty">该范围暂无文件</div>
        ) : (
          files.map((file) => (
            <button
              className="ov-row"
              key={file.id}
              onClick={() => setSelected(file)}
              type="button"
            >
              <span className="ov-row-icon" aria-hidden="true">
                <FileText size={14} />
              </span>
              <span className="ov-label" title={file.name}>
                {file.name}
              </span>
              <span className="ov-value">{file.ownerLabel}</span>
            </button>
          ))
        )}
      </section>
      {selected ? (
        <div className="ov-detail">
          <button
            className="ov-back"
            onClick={() => setSelected(null)}
            type="button"
          >
            <ArrowLeft size={14} /> 返回列表
          </button>
          <div className="ov-detail-title" title={selected.name}>
            {selected.name}
          </div>
          <div className="ov-detail-meta">
            范围 {selected.scope} · 归属 {selected.ownerLabel || "—"}
          </div>
          <div className="ov-detail-meta">
            类型 {selected.mimeType ?? "—"} · 大小 {selected.sizeBytes ?? "—"}{" "}
            字节
          </div>
          <div className="ov-detail-meta">SHA-256 {selected.sha256 ?? "—"}</div>
          {selected.scope === "artifact" ? (
            artifactQuery.isPending ? (
              <div className="ov-empty">正在读取产出…</div>
            ) : artifactQuery.data ? (
              <>
                <div className="ov-detail-actions">
                  <AiIssueHandoffButton
                    content={
                      artifactQuery.data.deletedAt
                        ? null
                        : taskArtifactHandoff(
                            artifactQuery.data.id,
                            artifactQuery.data.taskId,
                            artifactQuery.data.submissionId,
                            artifactQuery.data.name,
                            selected.ownerLabel || "任务",
                            null,
                            artifactQuery.data.storageKind,
                            artifactQuery.data.submissionStatus,
                          )
                    }
                  />
                </div>
                {artifactQuery.data.contentText ? (
                  <pre className="ov-detail-result">
                    {artifactQuery.data.contentText}
                  </pre>
                ) : (
                  <div className="ov-empty">该产出不含可内联预览的文本</div>
                )}
              </>
            ) : (
              <div className="ov-empty">该产出不含可内联预览的文本</div>
            )
          ) : (
            <div className="ov-empty">该类型暂不支持内联预览，仅显示元数据</div>
          )}
        </div>
      ) : null}
    </>
  );
}
