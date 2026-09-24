import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  aiWorkPlanActiveSteps,
  aiWorkPlanRecordLinkLabel,
  aiWorkPlanProgress,
  aiWorkPlanContinuationScopes,
  aiWorkPlanContinuationPrompt,
  closeAiWorkPlan,
  getAiWorkPlan,
  getAiWorkPlanContinuation,
  aiAgentRunSubmissionStatusLabels,
  planContinuationBlockerLabels,
  planContinuationReasons,
  planStateLabels,
  type AiWorkPlanContinuationBlocker,
} from "../api/aiWorkPlan";
import { aiActionLabels } from "../api/aiWorkspaceActions";
import { aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type { AiWorkspaceScope } from "../types/models";
import { useAiChatStore } from "../store/aiChat";
import { AiWorkspaceActions } from "./AiWorkspaceActions";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import { ApiError } from "../api/client";
import { AiPlanContinuation } from "./AiPlanContinuation";

export function AiWorkPlan({
  sessionId,
  live = false,
  onContinue,
  onContinueActions,
  focusRequest = 0,
}: {
  sessionId: string;
  live?: boolean;
  onContinue?: (prompt: string, scopes: AiWorkspaceScope[]) => void;
  onContinueActions?: (request: AiActionContinuation) => void;
  focusRequest?: number;
}) {
  const [version, setVersion] = useState<number | undefined>();
  const [evidence, setEvidence] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [checking, setChecking] = useState(false);
  const [blockingGenerations, setBlockingGenerations] = useState<string[]>([]);
  const [continuationBlockers, setContinuationBlockers] = useState<
    AiWorkPlanContinuationBlocker[]
  >([]);
  const [continuationMessage, setContinuationMessage] = useState<string | null>(
    null,
  );
  const [closeOpen, setCloseOpen] = useState(false);
  const [closeAcknowledged, setCloseAcknowledged] = useState(false);
  const [closing, setClosing] = useState(false);
  const [closeMessage, setCloseMessage] = useState<string | null>(null);
  const checkRequest = useRef<AbortController | null>(null);
  const queryClient = useQueryClient();
  useEffect(() => {
    if (focusRequest > 0) {
      setVersion(undefined);
      setEvidence(null);
      setExpanded(true);
    }
  }, [focusRequest]);
  const planUpdateVersion = useAiChatStore(
    (state) => state.workPlanVersions[sessionId] ?? 0,
  );
  const continuationOwner = useMemo(
    () => ({}),
    [sessionId, version, live, onContinue, planUpdateVersion, focusRequest],
  );
  const continuationOwnerRef = useRef(continuationOwner);
  continuationOwnerRef.current = continuationOwner;
  useEffect(() => {
    setChecking(false);
    setBlockingGenerations([]);
    setContinuationBlockers([]);
    setContinuationMessage(null);
    setCloseOpen(false);
    setCloseAcknowledged(false);
    setCloseMessage(null);
    return () => {
      checkRequest.current?.abort();
      checkRequest.current = null;
    };
  }, [continuationOwner]);
  useEffect(() => {
    setVersion(undefined);
    setEvidence(null);
  }, [sessionId]);
  const query = useQuery({
    queryKey: [
      "ai",
      "work-plan",
      sessionId,
      version ?? "latest",
      version === undefined ? planUpdateVersion : 0,
    ],
    queryFn: ({ signal }) => getAiWorkPlan(sessionId, signal, version),
    enabled: isWorkspaceIdentity(sessionId),
    retry: false,
    refetchInterval: (q) =>
      live ||
      (q.state.data ? aiWorkPlanActiveSteps(q.state.data) : []).some(
        (s) =>
          [
            "pending",
            "child_pending_approval",
            "queued",
            "running",
            "output_pending",
          ].includes(s.state) && s.kind === "action",
      )
        ? 3000
        : false,
  });
  const latestPlanVersion = useRef(query.data?.version);
  latestPlanVersion.current = query.data?.version;
  async function prepareCheckedContinuation() {
    const observed = query.data;
    if (
      !observed ||
      !onContinue ||
      live ||
      version !== undefined ||
      checkRequest.current ||
      query.isFetching
    )
      return;
    const controller = new AbortController();
    checkRequest.current = controller;
    const owner = continuationOwner;
    const isCurrent = () =>
      !controller.signal.aborted &&
      checkRequest.current === controller &&
      continuationOwnerRef.current === owner;
    setChecking(true);
    setBlockingGenerations([]);
    setContinuationBlockers([]);
    setContinuationMessage(null);
    try {
      const checked = await getAiWorkPlanContinuation(
        sessionId,
        observed.version,
        controller.signal,
      );
      if (!isCurrent()) return;
      if (latestPlanVersion.current !== observed.version) {
        setContinuationMessage("计划已更新，请核对最新计划后再次继续。");
        return;
      }
      queryClient.setQueryData(
        ["ai", "work-plan", sessionId, "latest", planUpdateVersion],
        checked.plan,
      );
      setContinuationMessage(planContinuationReasons[checked.reason]);
      setContinuationBlockers(checked.blockers ?? []);
      setBlockingGenerations(
        Array.from(
          new Set([
            ...(checked.blockers ?? []).flatMap(
              (blocker) => blocker.generation_ids,
            ),
            ...(checked.blocking_generation_id
              ? [checked.blocking_generation_id]
              : []),
          ]),
        ),
      );
      if (checked.ready)
        onContinue(
          aiWorkPlanContinuationPrompt,
          aiWorkPlanContinuationScopes(checked.plan),
        );
    } catch (error) {
      if (!isCurrent()) return;
      if (
        error instanceof ApiError &&
        error.code === "AI_PLAN_CONTINUATION_CHANGED"
      ) {
        setContinuationMessage("计划已更新，请核对最新计划后再次继续。");
        void query.refetch();
      } else {
        setContinuationMessage(
          error instanceof ApiError &&
            error.code === "AI_PLAN_CONTINUATION_UNAVAILABLE"
            ? "计划证据不可核验，暂不能准备续办。请刷新回执后重试。"
            : "接续检查失败，未准备续办。请重新核对，不能依据旧缓存继续。",
        );
      }
    } finally {
      if (isCurrent()) {
        checkRequest.current = null;
        setChecking(false);
      }
    }
  }
  async function closePlan() {
    const observed = query.data;
    if (!observed || closing || live || version !== undefined) return;
    setClosing(true);
    setCloseMessage(null);
    try {
      const closed = await closeAiWorkPlan(sessionId, observed.version);
      queryClient.setQueryData(
        ["ai", "work-plan", sessionId, "latest", planUpdateVersion],
        closed,
      );
      setCloseOpen(false);
      setCloseAcknowledged(false);
      setContinuationMessage(null);
      setContinuationBlockers([]);
      setBlockingGenerations([]);
      // A closed plan stops claiming its Runs; both queues must re-read.
      void queryClient.invalidateQueries({ queryKey: ["ai", "work-plans"] });
      void queryClient.invalidateQueries({ queryKey: ["ai", "agent-inbox"] });
      void queryClient.invalidateQueries({ queryKey: ["agent-runs"] });
    } catch (error) {
      const code = error instanceof ApiError ? error.code : undefined;
      if (code === "AI_PLAN_CLOSE_CHANGED") void query.refetch();
      setCloseMessage(
        code === "AI_PLAN_CLOSE_BLOCKED"
          ? "计划仍有待确认、执行中、待登记、生成中或后台续办的事项，暂不能关闭。请刷新回执并先处理这些事项。"
          : code === "AI_PLAN_CLOSE_CHANGED"
            ? "计划已更新，请核对最新计划后再决定是否关闭。"
            : code === "AI_PLAN_CLOSE_UNAVAILABLE"
              ? "计划证据不可核验，暂不能关闭。请刷新回执后重试。"
              : "关闭计划失败，计划保持原状态。请刷新后重试。",
      );
    } finally {
      setClosing(false);
    }
  }
  // The terminal transition must refresh even when the last revision arrived
  // just before the final progress event or no plan existed on the first read.
  useEffect(() => {
    if (isWorkspaceIdentity(sessionId)) void query.refetch();
  }, [sessionId, live]);
  if (!isWorkspaceIdentity(sessionId)) return null;
  if (query.isError)
    return (
      <p className="ai-plan-error" role="status">
        计划暂不可读取，不能据此判断进度。
        <button type="button" onClick={() => void query.refetch()}>
          重试读取计划
        </button>
      </p>
    );
  const plan = query.data;
  if (!plan)
    return focusRequest > 0 ? (
      <p role="status">
        {query.isPending ? "正在定位执行计划…" : "该会话暂无已保存的执行计划。"}
      </p>
    ) : null;
  const continuationBlockedStates = new Set([
    "pending",
    "child_pending_approval",
    "queued",
    "running",
    "output_pending",
  ]);
  const activeSteps = aiWorkPlanActiveSteps(plan);
  const progress = aiWorkPlanProgress(plan);
  const stepTitles = new Map(plan.steps.map((step) => [step.id, step.title]));
  const retryContinuations = new Set(
    plan.steps
      .filter(
        (step) =>
          step.replaces &&
          step.evidence?.action === "agent_run.retry" &&
          step.evidence.retry_of_run_id,
      )
      .map((step) => step.id),
  );
  const restartContinuations = new Set(
    plan.steps
      .filter(
        (step) =>
          step.replaces &&
          step.evidence?.action === "agent_run.start" &&
          step.evidence.restart_of_run_id,
      )
      .map((step) => step.id),
  );
  const closed = plan.closed_at !== undefined;
  const canContinue =
    version === undefined &&
    !closed &&
    plan.generation_status !== "streaming" &&
    activeSteps.some((step) => !step.ready || !step.satisfied) &&
    !activeSteps.some(
      (step) =>
        step.kind === "action" && continuationBlockedStates.has(step.state),
    );
  // Mirrors the server gate; the server still re-checks every open obligation.
  const canClose =
    canContinue &&
    !live &&
    !activeSteps.some(
      (step) =>
        step.kind === "action" &&
        (step.state === "retained" || step.state === "streaming"),
    );
  return (
    <details
      className="ai-work-plan"
      data-closed={closed ? "true" : undefined}
      open={expanded}
      onToggle={(event) => setExpanded(event.currentTarget.open)}
    >
      <summary>
        执行计划 · {plan.title} <small>版本 {plan.version}</small>
        {closed ? <small> · 已由你关闭</small> : null}
      </summary>
      {closed ? (
        <p role="status">
          你已于
          <time dateTime={plan.closed_at}>
            {new Date(plan.closed_at!).toLocaleString("zh-CN", {
              hour12: false,
            })}
          </time>
          关闭此计划。它不再出现在续办队列，也不能继续或自动续办；关闭没有撤销任何操作，下面的回执仍按真实状态显示。
        </p>
      ) : null}
      <p>
        分析进度由模型报告；操作进度来自真实回执。计划不自动批准业务操作，也不代表任务已验收。
      </p>
      <p>
        已满足 {progress.satisfied_step_total}/{progress.effective_step_total}
        {progress.superseded_step_total > 0
          ? ` · 历史替代 ${progress.superseded_step_total} 步（保留原回执）`
          : null}
      </p>
      <div className="ai-plan-toolbar">
        <button
          type="button"
          disabled={plan.version <= 1 || query.isFetching}
          onClick={() => {
            setEvidence(null);
            setVersion(plan.version - 1);
          }}
        >
          上一版
        </button>
        {version !== undefined && (
          <button
            type="button"
            onClick={() => {
              setEvidence(null);
              setVersion(undefined);
            }}
          >
            返回最新计划
          </button>
        )}
        <button
          type="button"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          刷新回执
        </button>
        {canContinue && onContinue ? (
          <button
            type="button"
            disabled={checking || query.isFetching || live}
            onClick={() => void prepareCheckedContinuation()}
          >
            {checking ? "正在核对接续条件…" : "继续此计划"}
          </button>
        ) : null}
        {canClose && !closeOpen ? (
          <button
            type="button"
            disabled={checking || closing || query.isFetching}
            onClick={() => {
              setCloseOpen(true);
              setCloseAcknowledged(false);
              setCloseMessage(null);
            }}
          >
            关闭计划
          </button>
        ) : null}
      </div>
      {closeOpen && !closed ? (
        <div
          aria-label="确认关闭计划"
          className="ai-plan-close-confirm"
          role="group"
        >
          <p>
            关闭后，此计划不再出现在续办队列，也不能再“继续此计划”或开启后台续办。关闭不会拒绝待确认操作、取消执行、撤销结果或修改任务，已有回执全部保留；之后如有新的工作，可以在对话中让智能体重新制定计划。
          </p>
          <label>
            <input
              checked={closeAcknowledged}
              disabled={closing}
              onChange={(event) => setCloseAcknowledged(event.target.checked)}
              type="checkbox"
            />
            我已了解关闭只停止跟踪此计划，不会撤销任何已发生的操作
          </label>
          <div className="ai-plan-toolbar">
            <button
              type="button"
              disabled={!closeAcknowledged || closing}
              onClick={() => void closePlan()}
            >
              {closing ? "正在关闭…" : "确认关闭计划"}
            </button>
            <button
              type="button"
              disabled={closing}
              onClick={() => {
                setCloseOpen(false);
                setCloseAcknowledged(false);
              }}
            >
              暂不关闭
            </button>
          </div>
        </div>
      ) : null}
      {closeMessage ? (
        <p className="ai-plan-error" role="alert">
          {closeMessage}
        </p>
      ) : null}
      {continuationMessage ? <p role="status">{continuationMessage}</p> : null}
      {continuationBlockers.length > 0 ? (
        <p className="ai-plan-blocker-summary" role="status">
          阻塞摘要：
          {continuationBlockers
            .map(
              (blocker) =>
                `${planContinuationBlockerLabels[blocker.reason]} ${blocker.total}`,
            )
            .join(" · ")}
        </p>
      ) : null}
      {blockingGenerations.map((generationId, index) => (
        <button
          key={generationId}
          type="button"
          onClick={() =>
            setEvidence(evidence === generationId ? null : generationId)
          }
        >
          {blockingGenerations.length === 1
            ? "查看阻塞这一轮的操作建议"
            : `查看阻塞回执组 ${index + 1}`}
        </button>
      ))}
      <ol>
        {plan.steps.map((step) => (
          <li key={step.id}>
            <span className="ai-plan-title">{step.title}</span>
            <span className="ai-plan-state">
              {step.kind === "analysis" && step.state === "pending"
                ? "尚待分析"
                : planStateLabels[step.state]}
            </span>
            {step.replaces ? (
              <small>
                {restartContinuations.has(step.id)
                  ? "按当前事实重新执行接续"
                  : retryContinuations.has(step.id)
                    ? "重试接续"
                    : "替代"}
                “{stepTitles.get(step.replaces)}”
              </small>
            ) : null}
            {step.superseded_by ? (
              <small>
                已由“{stepTitles.get(step.superseded_by)}”
                {restartContinuations.has(step.superseded_by)
                  ? "按当前事实重新执行接续"
                  : retryContinuations.has(step.superseded_by)
                    ? "重试接续"
                    : "替代"}{" "}
                · 保留原状态与回执
              </small>
            ) : !step.ready ? (
              <small>前置步骤尚未满足（计划提示）</small>
            ) : null}
            {step.evidence && (
              <div className="ai-plan-links">
                <small>
                  关联操作：
                  {
                    aiActionLabels[
                      step.evidence.action as keyof typeof aiActionLabels
                    ]
                  }
                </small>
                {step.evidence.submission_status ? (
                  <small>
                    验收状态：
                    {
                      aiAgentRunSubmissionStatusLabels[
                        step.evidence.submission_status
                      ]
                    }
                  </small>
                ) : null}
                <button
                  type="button"
                  onClick={() =>
                    setEvidence(
                      evidence === step.evidence!.generation_id
                        ? null
                        : step.evidence!.generation_id,
                    )
                  }
                >
                  查看该轮操作建议
                </button>
                {step.evidence.route && (
                  <Link to={aiWorkspaceHref(step.evidence.route, sessionId)}>
                    {aiWorkPlanRecordLinkLabel(step)}
                  </Link>
                )}
              </div>
            )}
          </li>
        ))}
      </ol>
      {evidence && (
        <AiWorkspaceActions
          generationId={evidence}
          sessionId={sessionId}
          onContinue={onContinueActions}
        />
      )}
      <small>
        {closed
          ? "历史版本保留当时计划；回执按本次读取的真实状态显示。已关闭的计划不会再被继续。"
          : "历史版本保留当时计划；回执按本次读取的真实状态显示。可手动发送新消息并重新授权，或为最新计划另行授权有界自动续办。"}
      </small>
      {version === undefined && !closed ? (
        <AiPlanContinuation
          sessionId={sessionId}
          planVersion={plan.version}
          disabled={live || query.isFetching}
        />
      ) : null}
      {canContinue && onContinue ? (
        <small className="ai-plan-continuation-note">
          点击后先只读核对计划和相关整轮回执；通过后才填入续办提示并打开单次权限确认，不会自动发送或执行操作。
        </small>
      ) : null}
    </details>
  );
}
