import {
  CalendarClock,
  CheckCircle2,
  CircleCheck,
  CircleOff,
  Clock3,
  Pencil,
  Plus,
  RotateCcw,
  XCircle,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useCancelClientFollowup,
  useClientFollowupActorOptionsQuery,
  useClientFollowupsQuery,
  useCompleteClientFollowup,
  useCreateClientFollowup,
  useRescheduleClientFollowup,
  useSkipClientFollowup,
  useUpdateClientFollowup,
} from "../api/hooks";
import type {
  Actor,
  ClientFollowup,
  ClientFollowupPriority,
  ClientFollowupStatus,
  ClientStatus,
} from "../types/models";
import {
  formatDateTimeLocalInTimeZone,
  localDateTimeToZonedISOString,
} from "../lib/zonedDateTime";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

type FollowupAction = "complete" | "skip" | "cancel" | "reschedule";

interface PlanDraft {
  assignedActorId: string;
  scheduledAt: string;
  timezone: string;
  channel: string;
  purpose: string;
  notes: string;
  priority: ClientFollowupPriority;
}

const statusLabel: Record<ClientFollowupStatus, string> = {
  planned: "待回访",
  completed: "已完成",
  skipped: "已跳过",
  cancelled: "已取消",
};

const filterLabel: Record<ClientFollowupStatus | "overdue", string> = {
  ...statusLabel,
  overdue: "已逾期",
};

const priorityLabel: Record<ClientFollowupPriority, string> = {
  low: "低优先级",
  normal: "普通优先级",
  high: "高优先级",
};

function localDateTime(value: string | Date): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (number: number) => String(number).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function defaultTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

function emptyPlanDraft(actorId = ""): PlanDraft {
  return {
    assignedActorId: actorId,
    scheduledAt: localDateTime(new Date()),
    timezone: defaultTimezone(),
    channel: "",
    purpose: "",
    notes: "",
    priority: "normal",
  };
}

function planDraftFromFollowup(followup: ClientFollowup): PlanDraft {
  return {
    assignedActorId: followup.assignedActorId,
    scheduledAt: formatDateTimeLocalInTimeZone(
      followup.scheduledAt,
      followup.timezone,
    ),
    timezone: followup.timezone,
    channel: followup.channel,
    purpose: followup.purpose,
    notes: followup.notes ?? "",
    priority: followup.priority,
  };
}

function formatTime(value: string, timezone: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  try {
    return new Intl.DateTimeFormat("zh-CN", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    }).format(date);
  } catch {
    return value;
  }
}

function isOverdue(
  followup: ClientFollowup,
  serverNow: string | undefined,
): boolean {
  const scheduledAt = new Date(followup.scheduledAt).getTime();
  const currentTime = serverNow ? new Date(serverNow).getTime() : Number.NaN;
  return (
    followup.status === "planned" &&
    Number.isFinite(scheduledAt) &&
    Number.isFinite(currentTime) &&
    scheduledAt < currentTime
  );
}

function detailCopy(followup: ClientFollowup): string | null {
  if (followup.status === "completed") {
    return followup.result
      ? `结果：${followup.result}`
      : "已完成，暂未记录结果。";
  }
  if (followup.status === "skipped") {
    return followup.skipReason
      ? `跳过原因：${followup.skipReason}`
      : "已跳过。";
  }
  if (followup.status === "cancelled") {
    return followup.cancelReason
      ? `取消原因：${followup.cancelReason}`
      : "已取消。";
  }
  return followup.notes;
}

function followupError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "回访计划已在其他窗口变化，已刷新最新记录，请核对后重试。";
    }
    if (error.code === "CLIENT_FOLLOWUP_FINAL") {
      return "该回访已进入终态，不能再修改或重复处理。";
    }
    if (error.code === "CLIENT_FOLLOWUP_CLIENT_INACTIVE") {
      return "该客户已停用；请先恢复客户状态，再安排、编辑或重新安排回访。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "客户回访操作失败，请重试。";
}

function planError(draft: PlanDraft): string | null {
  if (!draft.assignedActorId) return "请选择负责人。";
  if (!draft.timezone.trim() || draft.timezone.trim().length > 100) {
    return "请填写有效的 IANA 时区。";
  }
  const scheduledAt = localDateTimeToZonedISOString(
    draft.scheduledAt,
    draft.timezone.trim(),
  );
  if (scheduledAt.kind === "invalid")
    return "请选择有效的计划时间和 IANA 时区。";
  if (scheduledAt.kind === "nonexistent") {
    return "该时间在所选时区的夏令时切换中不存在，请调整计划时间。";
  }
  if (!draft.channel.trim() || draft.channel.trim().length > 100) {
    return "渠道需填写 1–100 个字符。";
  }
  if (!draft.purpose.trim() || draft.purpose.trim().length > 500) {
    return "目的需填写 1–500 个字符。";
  }
  if (draft.notes.trim().length > 4_000) return "备注不能超过 4,000 个字符。";
  return null;
}

function planInput(draft: PlanDraft) {
  const scheduledAt = localDateTimeToZonedISOString(
    draft.scheduledAt,
    draft.timezone.trim(),
  );
  if (scheduledAt.kind !== "valid") {
    throw new Error("Client followup plan must be validated before submission");
  }
  return {
    assignedActorId: draft.assignedActorId,
    scheduledAt: scheduledAt.iso,
    timezone: draft.timezone.trim(),
    channel: draft.channel.trim(),
    purpose: draft.purpose.trim(),
    notes: draft.notes.trim() || null,
    priority: draft.priority,
  };
}

function PlanFields({
  actors,
  draft,
  disabled,
  onChange,
}: {
  actors: Actor[];
  draft: PlanDraft;
  disabled: boolean;
  onChange: (draft: PlanDraft) => void;
}) {
  return (
    <div className="client-followup-editor-grid">
      <label>
        <span>计划时间</span>
        <input
          disabled={disabled}
          onChange={(event) =>
            onChange({ ...draft, scheduledAt: event.target.value })
          }
          type="datetime-local"
          value={draft.scheduledAt}
        />
      </label>
      <label>
        <span>负责人</span>
        <select
          disabled={disabled || actors.length === 0}
          onChange={(event) =>
            onChange({ ...draft, assignedActorId: event.target.value })
          }
          value={draft.assignedActorId}
        >
          <option value="">选择负责人</option>
          {actors.map((actor) => (
            <option key={actor.id} value={actor.id}>
              {actor.displayName}（{actor.type === "owner" ? "owner" : "person"}
              ）
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>渠道</span>
        <input
          disabled={disabled}
          maxLength={100}
          onChange={(event) =>
            onChange({ ...draft, channel: event.target.value })
          }
          placeholder="例如：微信、电话、线下面谈"
          value={draft.channel}
        />
      </label>
      <label>
        <span>优先级</span>
        <select
          disabled={disabled}
          onChange={(event) =>
            onChange({
              ...draft,
              priority: event.target.value as ClientFollowupPriority,
            })
          }
          value={draft.priority}
        >
          <option value="high">高优先级</option>
          <option value="normal">普通优先级</option>
          <option value="low">低优先级</option>
        </select>
      </label>
      <label className="client-followup-field-wide">
        <span>目的</span>
        <input
          disabled={disabled}
          maxLength={500}
          onChange={(event) =>
            onChange({ ...draft, purpose: event.target.value })
          }
          value={draft.purpose}
        />
      </label>
      <label className="client-followup-field-wide">
        <span>备注（可选）</span>
        <textarea
          disabled={disabled}
          maxLength={4_000}
          onChange={(event) =>
            onChange({ ...draft, notes: event.target.value })
          }
          rows={3}
          value={draft.notes}
        />
      </label>
      <label className="client-followup-field-wide">
        <span>IANA 时区</span>
        <input
          disabled={disabled}
          maxLength={100}
          onChange={(event) =>
            onChange({ ...draft, timezone: event.target.value })
          }
          placeholder="例如：Asia/Shanghai"
          value={draft.timezone}
        />
      </label>
    </div>
  );
}

export function ClientFollowupsSection({
  clientId,
  clientStatus = "active",
}: {
  clientId: string;
  clientStatus?: ClientStatus;
}) {
  const canPlan = clientStatus !== "inactive";
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<
    ClientFollowupStatus | "overdue" | ""
  >("");
  const [assignedActorFilter, setAssignedActorFilter] = useState("");
  const queryInput = useMemo(
    () => ({
      page,
      pageSize: 6,
      ...(statusFilter === "overdue"
        ? { status: "planned" as const, dueState: "overdue" as const }
        : statusFilter
          ? { status: statusFilter }
          : {}),
      ...(assignedActorFilter ? { assignedActorId: assignedActorFilter } : {}),
    }),
    [assignedActorFilter, page, statusFilter],
  );
  const query = useClientFollowupsQuery(clientId, queryInput);
  const actorsQuery = useClientFollowupActorOptionsQuery(true);
  const createMutation = useCreateClientFollowup();
  const updateMutation = useUpdateClientFollowup();
  const completeMutation = useCompleteClientFollowup();
  const skipMutation = useSkipClientFollowup();
  const cancelMutation = useCancelClientFollowup();
  const rescheduleMutation = useRescheduleClientFollowup();
  const [editing, setEditing] = useState<ClientFollowup | "new" | null>(null);
  const [planDraft, setPlanDraft] = useState<PlanDraft>(emptyPlanDraft);
  const [action, setAction] = useState<{
    kind: FollowupAction;
    followup: ClientFollowup;
  } | null>(null);
  const [reason, setReason] = useState("");
  const [result, setResult] = useState("");
  const [nextStep, setNextStep] = useState("");
  const [completedAt, setCompletedAt] = useState(localDateTime(new Date()));
  const [scheduleNext, setScheduleNext] = useState(false);
  const [nextPlanDraft, setNextPlanDraft] = useState<PlanDraft>(emptyPlanDraft);
  const [localError, setLocalError] = useState<string | null>(null);
  const items = query.data?.items ?? [];
  const actors = actorsQuery.data ?? [];
  const totalPages = Math.max(
    1,
    Math.ceil((query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 6)),
  );
  const pending =
    createMutation.isPending ||
    updateMutation.isPending ||
    completeMutation.isPending ||
    skipMutation.isPending ||
    cancelMutation.isPending ||
    rescheduleMutation.isPending;
  const mutationError =
    followupError(createMutation.error) ??
    followupError(updateMutation.error) ??
    followupError(completeMutation.error) ??
    followupError(skipMutation.error) ??
    followupError(cancelMutation.error) ??
    followupError(rescheduleMutation.error);

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  useEffect(() => {
    if (editing === "new" && !planDraft.assignedActorId && actors[0]) {
      setPlanDraft((draft) => ({ ...draft, assignedActorId: actors[0].id }));
    }
  }, [actors, editing, planDraft.assignedActorId]);

  useEffect(() => {
    if (canPlan) return;
    setScheduleNext(false);
    if (editing) setEditing(null);
    if (action?.kind === "reschedule") setAction(null);
  }, [action?.kind, canPlan, editing]);

  const resetFeedback = () => {
    setLocalError(null);
    createMutation.reset();
    updateMutation.reset();
    completeMutation.reset();
    skipMutation.reset();
    cancelMutation.reset();
    rescheduleMutation.reset();
  };
  const refreshAfterConflict = () => void query.refetch();
  const openNew = () => {
    if (!canPlan) return;
    resetFeedback();
    setAction(null);
    setPlanDraft(emptyPlanDraft(actors[0]?.id ?? ""));
    setEditing("new");
  };
  const openEdit = (followup: ClientFollowup) => {
    if (!canPlan) return;
    resetFeedback();
    setAction(null);
    setPlanDraft(planDraftFromFollowup(followup));
    setEditing(followup);
  };
  const openAction = (kind: FollowupAction, followup: ClientFollowup) => {
    if (!canPlan && kind === "reschedule") return;
    resetFeedback();
    setEditing(null);
    setAction({ kind, followup });
    setReason("");
    setResult("");
    setNextStep("");
    setCompletedAt(localDateTime(new Date()));
    setScheduleNext(false);
    setNextPlanDraft(
      emptyPlanDraft(followup.assignedActorId || actors[0]?.id || ""),
    );
    if (kind === "reschedule") setPlanDraft(planDraftFromFollowup(followup));
  };
  const closeEditors = () => {
    if (pending) return;
    setEditing(null);
    setAction(null);
    setLocalError(null);
  };
  const submitPlan = () => {
    if (!canPlan) {
      return setLocalError("该客户已停用；请先恢复客户状态再安排或编辑回访。");
    }
    const error = planError(planDraft);
    if (error) return setLocalError(error);
    resetFeedback();
    const input = planInput(planDraft);
    if (editing === "new") {
      createMutation.mutate(
        { clientId, ...input },
        {
          onSuccess: () => {
            setEditing(null);
            setPage(1);
          },
        },
      );
      return;
    }
    if (!editing) return;
    updateMutation.mutate(
      { id: editing.id, input: { ...input, expectedVersion: editing.version } },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
            refreshAfterConflict();
        },
        onSuccess: () => setEditing(null),
      },
    );
  };
  const submitAction = () => {
    if (!action) return;
    const { followup, kind } = action;
    if (!canPlan && kind === "reschedule") {
      return setLocalError("该客户已停用；请先恢复客户状态再重新安排回访。");
    }
    if (kind === "complete") {
      const value = result.trim();
      const completion = new Date(completedAt);
      if (!value || value.length > 4_000)
        return setLocalError("回访结果需填写 1–4,000 个字符。");
      if (!completedAt || Number.isNaN(completion.getTime()))
        return setLocalError("请选择有效的完成时间。");
      if (nextStep.trim().length > 4_000)
        return setLocalError("下一步不能超过 4,000 个字符。");
      if (scheduleNext) {
        const nextPlanError = planError(nextPlanDraft);
        if (nextPlanError) return setLocalError(`下一次回访：${nextPlanError}`);
      }
      resetFeedback();
      completeMutation.mutate(
        {
          id: followup.id,
          input: {
            result: value,
            nextStep: nextStep.trim() || null,
            completedAt: completion.toISOString(),
            nextFollowup:
              canPlan && scheduleNext ? planInput(nextPlanDraft) : null,
            expectedVersion: followup.version,
          },
        },
        {
          onError: (error) => {
            if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
              refreshAfterConflict();
          },
          onSuccess: () => setAction(null),
        },
      );
      return;
    }
    const reasonValue = reason.trim();
    if (!reasonValue || reasonValue.length > 1_000)
      return setLocalError("请填写 1–1,000 个字符的原因。");
    if (kind === "reschedule") {
      const error = planError(planDraft);
      if (error) return setLocalError(error);
      resetFeedback();
      rescheduleMutation.mutate(
        {
          id: followup.id,
          input: {
            ...planInput(planDraft),
            reason: reasonValue,
            expectedVersion: followup.version,
          },
        },
        {
          onError: (error) => {
            if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
              refreshAfterConflict();
          },
          onSuccess: () => {
            setAction(null);
            setPage(1);
          },
        },
      );
      return;
    }
    const mutation = kind === "skip" ? skipMutation : cancelMutation;
    mutation.mutate(
      {
        id: followup.id,
        input: { reason: reasonValue, expectedVersion: followup.version },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
            refreshAfterConflict();
        },
        onSuccess: () => setAction(null),
      },
    );
  };
  const actionTitle: Record<FollowupAction, string> = {
    complete: "记录回访结果",
    skip: "跳过回访",
    cancel: "取消回访计划",
    reschedule: "重新安排回访",
  };

  return (
    <section className="project-detail-section client-followups-section">
      <div className="project-detail-heading">
        <div>
          <h2>客户回访</h2>
          <p>
            计划、提醒与历史均保存在本机，不会发送邮件、短信或其他外部消息。
          </p>
        </div>
        {canPlan ? (
          <button
            className="button button-primary"
            disabled={pending || actorsQuery.isPending || actors.length === 0}
            onClick={openNew}
            title={
              actorsQuery.isError
                ? "无法读取负责人，请先重试。"
                : actors.length === 0
                  ? "请先保留 active owner 或 person。"
                  : undefined
            }
            type="button"
          >
            <Plus size={14} /> 安排回访
          </button>
        ) : null}
      </div>
      {!canPlan ? (
        <p className="client-followup-inactive-note">
          客户已停用。既有计划仍可完成、跳过或取消；恢复客户状态后才能安排、编辑或重排。
        </p>
      ) : null}
      <div className="client-followup-filters">
        <label>
          <span>显示</span>
          <select
            aria-label="回访状态筛选"
            disabled={query.isPending}
            onChange={(event) => {
              setStatusFilter(
                event.target.value as ClientFollowupStatus | "overdue" | "",
              );
              setPage(1);
            }}
            value={statusFilter}
          >
            <option value="">全部状态</option>
            <option value="overdue">仅已逾期</option>
            {Object.entries(statusLabel).map(([status, label]) => (
              <option key={status} value={status}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>负责人</span>
          <select
            aria-label="回访负责人筛选"
            disabled={query.isPending || actorsQuery.isPending}
            onChange={(event) => {
              setAssignedActorFilter(event.target.value);
              setPage(1);
            }}
            value={assignedActorFilter}
          >
            <option value="">全部负责人</option>
            {actors.map((actor) => (
              <option key={actor.id} value={actor.id}>
                {actor.displayName}（
                {actor.type === "owner" ? "owner" : "person"}）
              </option>
            ))}
          </select>
        </label>
        {query.isSuccess ? (
          <small>
            {statusFilter ? filterLabel[statusFilter] : "全部"}{" "}
            {query.data?.meta.total ?? 0} 条
          </small>
        ) : null}
      </div>
      {actorsQuery.isError ? (
        <ErrorState
          compact
          message="无法读取可分派的负责人。"
          onRetry={() => void actorsQuery.refetch()}
        />
      ) : null}
      {editing ? (
        <div className="client-followup-editor">
          <div>
            <strong>
              {editing === "new" ? "安排本地回访" : "编辑回访计划"}
            </strong>
            <p>回访只记录线下计划和结果；负责人不会收到应用通知。</p>
          </div>
          <PlanFields
            actors={actors}
            disabled={pending}
            draft={planDraft}
            onChange={setPlanDraft}
          />
          <div className="client-activity-editor-actions">
            <button
              className="button button-secondary"
              disabled={pending}
              onClick={closeEditors}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={pending || actors.length === 0}
              onClick={submitPlan}
              type="button"
            >
              <CalendarClock size={14} />
              {createMutation.isPending || updateMutation.isPending
                ? "正在保存…"
                : editing === "new"
                  ? "保存回访计划"
                  : "保存修改"}
            </button>
          </div>
        </div>
      ) : null}
      {action ? (
        <div className="client-followup-editor client-followup-action-editor">
          <div>
            <strong>
              {actionTitle[action.kind]}：{action.followup.purpose}
            </strong>
            <p>操作会保留本地状态与审计历史，不能自动联系客户。</p>
          </div>
          {action.kind === "complete" ? (
            <div className="client-followup-editor-grid">
              <label className="client-followup-field-wide">
                <span>回访结果</span>
                <textarea
                  autoFocus
                  disabled={pending}
                  maxLength={4_000}
                  onChange={(event) => setResult(event.target.value)}
                  rows={3}
                  value={result}
                />
              </label>
              <label>
                <span>完成时间</span>
                <input
                  disabled={pending}
                  onChange={(event) => setCompletedAt(event.target.value)}
                  type="datetime-local"
                  value={completedAt}
                />
              </label>
              <label>
                <span>下一步（可选）</span>
                <input
                  disabled={pending}
                  maxLength={4_000}
                  onChange={(event) => setNextStep(event.target.value)}
                  value={nextStep}
                />
              </label>
              {canPlan ? (
                <div className="client-followup-field-wide client-followup-next-toggle">
                  <button
                    aria-pressed={scheduleNext}
                    className="button button-secondary"
                    disabled={pending}
                    onClick={() => setScheduleNext((current) => !current)}
                    type="button"
                  >
                    同时安排下一次本地回访
                  </button>
                  <small>
                    保存时将与本次完成记录在同一事务提交，不发送外部消息。
                  </small>
                </div>
              ) : null}
              {canPlan && scheduleNext ? (
                <div className="client-followup-field-wide client-followup-next-plan">
                  <strong>下一次回访计划</strong>
                  <PlanFields
                    actors={actors}
                    disabled={pending}
                    draft={nextPlanDraft}
                    onChange={setNextPlanDraft}
                  />
                </div>
              ) : null}
            </div>
          ) : null}
          {action.kind === "reschedule" ? (
            <PlanFields
              actors={actors}
              disabled={pending}
              draft={planDraft}
              onChange={setPlanDraft}
            />
          ) : null}
          {action.kind !== "complete" ? (
            <label className="client-followup-reason">
              <span>{action.kind === "reschedule" ? "重排原因" : "原因"}</span>
              <textarea
                autoFocus={action.kind !== "reschedule"}
                disabled={pending}
                maxLength={1_000}
                onChange={(event) => setReason(event.target.value)}
                rows={2}
                value={reason}
              />
            </label>
          ) : null}
          <div className="client-activity-editor-actions">
            <button
              className="button button-secondary"
              disabled={pending}
              onClick={closeEditors}
              type="button"
            >
              返回
            </button>
            <button
              className={
                action.kind === "cancel"
                  ? "button button-danger"
                  : "button button-primary"
              }
              disabled={
                pending || (action.kind === "reschedule" && actors.length === 0)
              }
              onClick={submitAction}
              type="button"
            >
              {action.kind === "complete" ? <CheckCircle2 size={14} /> : null}
              {action.kind === "reschedule" ? <RotateCcw size={14} /> : null}
              {action.kind === "cancel" ? <XCircle size={14} /> : null}
              {pending ? "正在处理…" : actionTitle[action.kind]}
            </button>
          </div>
        </div>
      ) : null}
      {localError || mutationError ? (
        <div className="form-error" role="alert">
          {localError ?? mutationError}
        </div>
      ) : null}
      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取客户回访计划。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          action={
            canPlan && actors.length > 0 ? (
              <button
                className="button button-primary"
                onClick={openNew}
                type="button"
              >
                安排第一条回访
              </button>
            ) : undefined
          }
          message="创建回访计划后会在这里按计划时间显示；到期项会投影到本地收件箱。"
          title="暂无客户回访"
        />
      ) : null}
      {items.length > 0 ? (
        <div className="client-followup-list">
          {items.map((followup) => {
            const overdue = isOverdue(followup, query.data?.meta.serverNow);
            const Icon =
              followup.status === "completed"
                ? CircleCheck
                : followup.status === "planned"
                  ? Clock3
                  : CircleOff;
            const copy = detailCopy(followup);
            return (
              <article
                className={overdue ? "is-overdue" : undefined}
                key={followup.id}
              >
                <span className="client-followup-icon" aria-hidden="true">
                  <Icon size={14} />
                </span>
                <div className="client-followup-copy">
                  <div>
                    <strong>{followup.purpose}</strong>
                    <span>
                      {overdue ? "已逾期" : statusLabel[followup.status]}
                    </span>
                    <span
                      className={`client-followup-priority ${followup.priority}`}
                    >
                      {priorityLabel[followup.priority]}
                    </span>
                  </div>
                  {copy ? <p>{copy}</p> : null}
                  <small>
                    <CalendarClock size={11} />{" "}
                    {formatTime(followup.scheduledAt, followup.timezone)} ·{" "}
                    {followup.timezone} · {followup.channel} · 负责人{" "}
                    {followup.assignedActorName}
                  </small>
                  {followup.nextStep ? (
                    <small className="client-followup-next-step">
                      下一步：{followup.nextStep}
                    </small>
                  ) : null}
                </div>
                {followup.status === "planned" ? (
                  <div className="client-followup-actions">
                    {canPlan ? (
                      <button
                        aria-label={`编辑回访 ${followup.purpose}`}
                        className="icon-button"
                        disabled={pending}
                        onClick={() => openEdit(followup)}
                        type="button"
                      >
                        <Pencil size={13} />
                      </button>
                    ) : null}
                    <button
                      className="button button-secondary"
                      disabled={pending}
                      onClick={() => openAction("complete", followup)}
                      type="button"
                    >
                      完成
                    </button>
                    {canPlan ? (
                      <button
                        className="button button-secondary"
                        disabled={pending}
                        onClick={() => openAction("reschedule", followup)}
                        type="button"
                      >
                        重排
                      </button>
                    ) : null}
                    <button
                      className="button button-quiet"
                      disabled={pending}
                      onClick={() => openAction("skip", followup)}
                      type="button"
                    >
                      跳过
                    </button>
                    <button
                      className="button button-quiet"
                      disabled={pending}
                      onClick={() => openAction("cancel", followup)}
                      type="button"
                    >
                      取消
                    </button>
                  </div>
                ) : null}
              </article>
            );
          })}
        </div>
      ) : null}
      {totalPages > 1 ? (
        <nav aria-label="客户回访分页" className="pagination">
          <button
            className="button button-secondary"
            disabled={page <= 1 || query.isFetching || pending}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
            type="button"
          >
            上一页
          </button>
          <span>
            第 {page} / {totalPages} 页
          </span>
          <button
            className="button button-secondary"
            disabled={page >= totalPages || query.isFetching || pending}
            onClick={() => setPage((value) => value + 1)}
            type="button"
          >
            下一页
          </button>
        </nav>
      ) : null}
    </section>
  );
}
