import {
  AlarmClock,
  AlertCircle,
  ArrowUpRight,
  Clock3,
  Inbox,
  ListTodo,
  LoaderCircle,
  RotateCcw,
  Zap,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ApiError } from "../api/client";
import { useAutomationRunQuery, useRetryAutomationRun } from "../api/hooks";
import { automationRunHandoff } from "../lib/aiIssueHandoff";
import { agentRunFailureRuleId } from "../lib/agentRunFailureSource";
import type {
  AutomationRun,
  AutomationRunAttemptSummary,
} from "../types/models";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import { ErrorState, LoadingState } from "./feedback";
import { Modal } from "./Modal";

function formatDateTime(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "medium",
    hour12: false,
  }).format(date);
}

function statusLabel(status: AutomationRun["status"]): string {
  if (status === "succeeded") return "成功";
  if (status === "failed") return "失败";
  if (status === "skipped") return "已折叠";
  return "已取消";
}

function detailError(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.message}${error.requestId ? ` · 请求 ID：${error.requestId}` : ""}`;
  }
  return "无法读取自动化运行详情，请确认本地服务已就绪后重试。";
}

function snapshot(value: Record<string, unknown>): string {
  return JSON.stringify(value, null, 2);
}

function ResultFact({
  resultId,
  resultSummary,
  resultType,
  onOpenInboxItem,
  onOpenReminder,
  onOpenTask,
}: {
  resultId: string | null;
  resultSummary: string;
  resultType: string | null;
  onOpenInboxItem: (inboxItemId: string) => void;
  onOpenReminder: (reminderId: string) => void;
  onOpenTask: (taskId: string) => void;
}) {
  if (!resultType && !resultId && !resultSummary)
    return <span>未产生结果</span>;
  return (
    <div className="automation-run-result">
      <div>
        <strong>
          {resultType === "task"
            ? "任务"
            : resultType === "inbox_item"
              ? "收件箱事项"
              : resultType === "reminder"
                ? "提醒"
                : resultType || "结果"}
        </strong>
        <span>{resultSummary || resultId || "已记录"}</span>
        {resultId ? <code>{resultId}</code> : null}
      </div>
      {resultId && resultType === "task" ? (
        <button
          className="button button-secondary"
          onClick={() => onOpenTask(resultId)}
          type="button"
        >
          <ListTodo size={14} />
          打开任务
        </button>
      ) : resultId && resultType === "inbox_item" ? (
        <button
          className="button button-secondary"
          onClick={() => onOpenInboxItem(resultId)}
          type="button"
        >
          <Inbox size={14} />
          打开收件箱事项
        </button>
      ) : resultId && resultType === "reminder" ? (
        <button
          className="button button-secondary"
          onClick={() => onOpenReminder(resultId)}
          type="button"
        >
          <AlarmClock size={14} />
          打开提醒
        </button>
      ) : null}
    </div>
  );
}

function AttemptButton({
  attempt,
  currentId,
  disabled,
  onSelect,
}: {
  attempt: AutomationRunAttemptSummary;
  currentId: string;
  disabled: boolean;
  onSelect: (runId: string) => void;
}) {
  return (
    <button
      aria-current={attempt.id === currentId ? "true" : undefined}
      className="automation-retry-attempt"
      data-status={attempt.status}
      disabled={disabled}
      onClick={() => onSelect(attempt.id)}
      type="button"
    >
      <span>第 {attempt.attempt} 次</span>
      <strong>{statusLabel(attempt.status)}</strong>
      <small>
        {attempt.errorCode || attempt.resultSummary || "无执行摘要"}
      </small>
      <small>
        {formatDateTime(attempt.startedAt)} → {formatDateTime(attempt.endedAt)}
      </small>
      {attempt.id !== currentId ? <ArrowUpRight size={13} /> : null}
    </button>
  );
}

export function AutomationRunDetailModal({
  onClose,
  onOpenInboxItem,
  onOpenReminder,
  onOpenTask,
  runId,
  inline = false,
  onSelectRun,
}: {
  onClose: () => void;
  onOpenInboxItem: (inboxItemId: string) => void;
  onOpenReminder: (reminderId: string) => void;
  onOpenTask: (taskId: string) => void;
  runId: string | null;
  inline?: boolean;
  onSelectRun?: (id: string) => void;
}) {
  const [activeRunId, setActiveRunId] = useState<string | null>(runId);
  const selectedRunId = inline ? runId : activeRunId;
  const detailQuery = useAutomationRunQuery(selectedRunId, inline);
  const retryRun = useRetryAutomationRun();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    setActiveRunId(runId);
    if (!retryRun.isPending) retryRun.reset();
    // The mutation object is stable for the mounted query client. Reset only
    // when opening a different audit trail.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runId]);

  const detail =
    inline && (detailQuery.isFetching || detailQuery.isError)
      ? undefined
      : detailQuery.data;

  function selectAttempt(nextRunId: string) {
    if (retryRun.isPending || nextRunId === selectedRunId) return;
    retryRun.reset();
    if (onSelectRun) onSelectRun(nextRunId);
    else setActiveRunId(nextRunId);
  }

  async function retry() {
    if (!detail) return;
    try {
      const next = await retryRun.mutateAsync(detail.id);
      // A manual retry may finish after the user returns to chat. Keep the
      // actual mutation/cache outcome, but never navigate them back here.
      if (!mounted.current) return;
      if (onSelectRun) onSelectRun(next.id);
      else setActiveRunId(next.id);
    } catch {
      // The safe mutation error remains visible in the modal.
    }
  }

  const footer = (
    <div className="automation-run-detail-footer">
      <AiIssueHandoffButton
        content={
          detail
            ? automationRunHandoff(
                detail.id,
                detail.ruleName,
                detail.attempt,
                statusLabel(detail.status),
                detail.errorCode,
                detail.retryable,
              )
            : null
        }
        disabled={retryRun.isPending}
      />
      {detail?.status === "failed" && detail.retryable ? (
        <button
          className="button button-primary"
          disabled={retryRun.isPending}
          onClick={() => void retry()}
          type="button"
        >
          {retryRun.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <RotateCcw size={14} />
          )}
          {retryRun.isPending ? "正在重试…" : "重试本次运行"}
        </button>
      ) : null}
      <button
        className="button button-secondary"
        disabled={retryRun.isPending}
        onClick={onClose}
        type="button"
      >
        关闭
      </button>
    </div>
  );
  const content = (
    <>
      {(detailQuery.isPending || (inline && detailQuery.isFetching)) &&
      !detail ? (
        <LoadingState label="正在读取不可变运行记录…" />
      ) : !detail ? (
        <div role="alert">
          <ErrorState
            message={detailError(detailQuery.error)}
            onRetry={() => void detailQuery.refetch()}
            title="无法读取运行详情"
          />
        </div>
      ) : (
        <div className="automation-run-detail">
          {detailQuery.isError ? (
            <div className="automation-error" role="alert">
              <AlertCircle size={14} />
              <span>
                运行详情刷新失败，当前显示上次成功读取的不可变记录。
                {detailError(detailQuery.error)}
              </span>
              <button
                className="button button-secondary"
                onClick={() => void detailQuery.refetch()}
                type="button"
              >
                重试刷新
              </button>
            </div>
          ) : null}
          <section className="automation-run-facts">
            <div>
              <span>规则</span>
              <strong>{detail.ruleName}</strong>
              <small>
                {detail.presetKey} · 版本 {detail.ruleVersion}
              </small>
            </div>
            <div>
              <span>状态</span>
              <strong data-status={detail.status}>
                {statusLabel(detail.status)}
              </strong>
              <small>第 {detail.attempt} 次尝试</small>
            </div>
            <div>
              <span>开始</span>
              <strong>{formatDateTime(detail.startedAt)}</strong>
              <small>结束：{formatDateTime(detail.endedAt)}</small>
            </div>
          </section>

          <section className="automation-run-detail-section">
            <h4>触发来源</h4>
            {detail.source.kind === "schedule" ? (
              <div className="automation-source-fact">
                <Clock3 size={15} />
                <div>
                  <strong>计划触发</strong>
                  <span>
                    计划时间：{formatDateTime(detail.source.scheduledFor)}
                  </span>
                </div>
              </div>
            ) : (
              <div className="automation-source-fact">
                <Zap aria-hidden="true" size={15} />
                <div>
                  <strong>
                    {detail.source.available ? "业务事件" : "业务事件已不可用"}
                  </strong>
                  <span>
                    {detail.source.aggregateType || "未知聚合"} ·{" "}
                    {detail.source.action || "未知动作"}
                  </span>
                  {detail.source.aggregateId ? (
                    <code>聚合 ID：{detail.source.aggregateId}</code>
                  ) : null}
                  <code>{detail.source.eventId}</code>
                  {detail.source.occurredAt ? (
                    <small>
                      发生时间：{formatDateTime(detail.source.occurredAt)}
                    </small>
                  ) : null}
                </div>
              </div>
            )}
          </section>

          <section className="automation-run-detail-section">
            <h4>不可变执行快照</h4>
            <div className="automation-snapshot-grid">
              <div>
                <strong>配置快照</strong>
                <pre>{snapshot(detail.configSnapshot)}</pre>
              </div>
              <div>
                <strong>动作快照</strong>
                <pre>{snapshot(detail.actionSnapshot)}</pre>
              </div>
            </div>
          </section>

          <section className="automation-run-detail-section">
            <h4>错误与重试</h4>
            {detail.ruleId === agentRunFailureRuleId ? (
              <p>
                这里只重试失败诊断通知，不重跑
                Agent、不调用模型、不恢复产出登记。
              </p>
            ) : null}
            <dl className="automation-run-audit-list">
              <div>
                <dt>错误码</dt>
                <dd>{detail.errorCode || "无"}</dd>
              </div>
              <div>
                <dt>可重试</dt>
                <dd>{detail.retryable ? "是" : "否"}</dd>
              </div>
              <div>
                <dt>建议重试时间</dt>
                <dd>{formatDateTime(detail.retryAt)}</dd>
              </div>
              <div>
                <dt>重试来源</dt>
                <dd>{detail.retryOfRunId || "首次尝试"}</dd>
              </div>
            </dl>
            {retryRun.error ? (
              <p className="automation-error" role="alert">
                <AlertCircle size={14} />
                {detailError(retryRun.error)}
              </p>
            ) : null}
          </section>

          <section className="automation-run-detail-section">
            <h4>执行结果</h4>
            <ResultFact
              onOpenInboxItem={onOpenInboxItem}
              onOpenReminder={onOpenReminder}
              onOpenTask={onOpenTask}
              resultId={detail.resultId}
              resultSummary={detail.resultSummary}
              resultType={detail.resultType}
            />
          </section>

          <section className="automation-run-detail-section">
            <h4>完整尝试链</h4>
            <div className="automation-retry-chain">
              {detail.retryChain.map((attempt) => (
                <AttemptButton
                  attempt={attempt}
                  currentId={detail.id}
                  disabled={retryRun.isPending}
                  key={attempt.id}
                  onSelect={selectAttempt}
                />
              ))}
            </div>
          </section>
        </div>
      )}
    </>
  );
  return inline ? (
    <section aria-label="自动化运行详情">
      {content}
      {footer}
    </section>
  ) : (
    <Modal
      dismissible={!retryRun.isPending}
      footer={footer}
      onClose={onClose}
      open={Boolean(runId)}
      title="自动化运行详情"
      width="720px"
    >
      {content}
    </Modal>
  );
}
