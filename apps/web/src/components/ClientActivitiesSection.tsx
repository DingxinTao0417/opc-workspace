import {
  CalendarClock,
  FileText,
  LoaderCircle,
  MessageSquareText,
  Pencil,
  Plus,
  Trash2,
  UsersRound,
} from "lucide-react";
import { useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useClientActivitiesQuery,
  useCreateClientActivity,
  useDeleteClientActivity,
  useUpdateClientActivity,
} from "../api/hooks";
import { useSettledPage } from "../lib/useSettledPage";
import type { ClientActivity, ClientActivityKind } from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

type EditableActivityKind = Exclude<ClientActivityKind, "system_reference">;

interface ActivityDraft {
  kind: EditableActivityKind;
  title: string;
  body: string;
  occurredAt: string;
}

function toLocalDateTimeInput(value: string | Date): string {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (number: number) => String(number).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function emptyDraft(): ActivityDraft {
  return {
    kind: "note",
    title: "",
    body: "",
    occurredAt: toLocalDateTimeInput(new Date()),
  };
}

function draftFromActivity(activity: ClientActivity): ActivityDraft {
  return {
    kind: activity.kind === "meeting" ? "meeting" : "note",
    title: activity.title,
    body: activity.body ?? "",
    occurredAt: toLocalDateTimeInput(activity.occurredAt),
  };
}

function formatActivityTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function activityError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "活动记录已在其他窗口变化，已刷新时间线；当前草稿仍保留，请核对后重试。";
    }
    if (error.code === "CLIENT_ACTIVITY_DELETED") {
      return "该活动记录已经删除，已刷新时间线。";
    }
    if (error.code === "CLIENT_ACTIVITY_READ_ONLY") {
      return "系统引用只用于展示来源事实，不能手动编辑或删除。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "客户活动操作失败，请重试。";
}

export function ClientActivitiesSection({ clientId }: { clientId: string }) {
  const [page, setPage] = useState(1);
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const queryInput = useMemo(
    () => ({ page, pageSize: 6, includeDeleted }),
    [includeDeleted, page],
  );
  const query = useClientActivitiesQuery(clientId, queryInput);
  const createMutation = useCreateClientActivity();
  const updateMutation = useUpdateClientActivity();
  const deleteMutation = useDeleteClientActivity();
  const [editing, setEditing] = useState<ClientActivity | "new" | null>(null);
  const [draft, setDraft] = useState<ActivityDraft>(emptyDraft);
  const [localError, setLocalError] = useState<string | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<ClientActivity | null>(
    null,
  );
  const [deleteReason, setDeleteReason] = useState("");
  const items = query.data?.items ?? [];
  const totalPages = Math.max(
    1,
    Math.ceil((query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 6)),
  );
  const pending =
    createMutation.isPending ||
    updateMutation.isPending ||
    deleteMutation.isPending;
  const mutationError =
    activityError(createMutation.error) ??
    activityError(updateMutation.error) ??
    activityError(deleteMutation.error);

  useSettledPage({
    page,
    meta: query.data?.meta,
    isFetching: query.isFetching,
    isPlaceholderData: query.isPlaceholderData,
    isSuccess: query.isSuccess,
    setPage,
  });

  const resetFeedback = () => {
    setLocalError(null);
    createMutation.reset();
    updateMutation.reset();
    deleteMutation.reset();
  };

  const openNew = () => {
    resetFeedback();
    setDeleteCandidate(null);
    setDraft(emptyDraft());
    setEditing("new");
  };

  const openEdit = (activity: ClientActivity) => {
    resetFeedback();
    setDeleteCandidate(null);
    setDraft(draftFromActivity(activity));
    setEditing(activity);
  };

  const closeEditor = () => {
    if (pending) return;
    setEditing(null);
    setLocalError(null);
  };

  const submit = () => {
    const title = draft.title.trim();
    const body = draft.body.trim();
    if (!title || title.length > 200) {
      setLocalError("标题需填写 1–200 个字符。");
      return;
    }
    if (!body || body.length > 10_000) {
      setLocalError("正文需填写 1–10,000 个字符。");
      return;
    }
    const occurredAt = new Date(draft.occurredAt);
    if (!draft.occurredAt || Number.isNaN(occurredAt.getTime())) {
      setLocalError("请选择有效的发生时间。");
      return;
    }
    if (occurredAt.getTime() > Date.now() + 5 * 60_000) {
      setLocalError("客户活动不能记录为未来时间。");
      return;
    }
    resetFeedback();
    if (editing === "new") {
      createMutation.mutate(
        {
          clientId,
          input: {
            kind: draft.kind,
            title,
            body,
            occurredAt: occurredAt.toISOString(),
          },
        },
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
      {
        id: editing.id,
        input: {
          kind: draft.kind,
          title,
          body,
          occurredAt: occurredAt.toISOString(),
          expectedVersion: editing.version,
        },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void query.refetch();
          }
        },
        onSuccess: () => setEditing(null),
      },
    );
  };

  const confirmDelete = () => {
    if (!deleteCandidate) return;
    const reason = deleteReason.trim();
    if (!reason || reason.length > 1_000) {
      setLocalError("删除原因需填写 1–1,000 个字符。");
      return;
    }
    resetFeedback();
    deleteMutation.mutate(
      {
        id: deleteCandidate.id,
        input: { reason, expectedVersion: deleteCandidate.version },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void query.refetch();
          }
        },
        onSuccess: () => {
          setDeleteCandidate(null);
          setDeleteReason("");
        },
      },
    );
  };

  return (
    <section className="project-detail-section client-activities-section">
      <div className="project-detail-heading">
        <div>
          <h2>本地活动</h2>
          <p>
            汇总手工记录的笔记与会议，以及系统记录的项目状态事实；这些内容不代表客户回访或其他外部通信。
          </p>
        </div>
        <button
          className="button button-primary"
          disabled={pending}
          onClick={openNew}
          type="button"
        >
          <Plus size={14} />
          记录活动
        </button>
      </div>

      <label className="client-activity-history-toggle">
        <input
          checked={includeDeleted}
          onChange={(event) => {
            setIncludeDeleted(event.target.checked);
            setPage(1);
          }}
          type="checkbox"
        />
        显示已删除记录
      </label>

      {editing ? (
        <div className="client-activity-editor">
          <div className="client-activity-editor-grid">
            <label>
              <span>类型</span>
              <select
                disabled={pending}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    kind: event.target.value as EditableActivityKind,
                  }))
                }
                value={draft.kind}
              >
                <option value="note">沟通笔记</option>
                <option value="meeting">会议记录</option>
              </select>
            </label>
            <label>
              <span>发生时间</span>
              <input
                disabled={pending}
                max={toLocalDateTimeInput(new Date())}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    occurredAt: event.target.value,
                  }))
                }
                type="datetime-local"
                value={draft.occurredAt}
              />
            </label>
          </div>
          <label>
            <span>标题</span>
            <input
              autoFocus
              disabled={pending}
              maxLength={200}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  title: event.target.value,
                }))
              }
              placeholder="例如：确认第二阶段交付范围"
              value={draft.title}
            />
          </label>
          <label>
            <span>正文</span>
            <textarea
              disabled={pending}
              maxLength={10_000}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  body: event.target.value,
                }))
              }
              placeholder="记录事实、结论和下一步，不自动发送给客户。"
              rows={5}
              value={draft.body}
            />
          </label>
          <div className="client-activity-editor-actions">
            <button
              className="button button-secondary"
              disabled={pending}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={pending}
              onClick={submit}
              type="button"
            >
              {pending ? (
                <LoaderCircle className="animate-spin" size={13} />
              ) : null}
              {editing === "new" ? "保存活动" : "保存修改"}
            </button>
          </div>
        </div>
      ) : null}

      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取客户活动，客户资料仍可继续查看。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          action={
            !includeDeleted ? (
              <button
                className="button button-secondary"
                onClick={openNew}
                type="button"
              >
                <Plus size={14} /> 记录第一条活动
              </button>
            ) : undefined
          }
          message={
            includeDeleted
              ? "当前客户没有任何活动历史。"
              : "手工记录活动或关联项目状态发生变化后，时间线会显示对应的本地事实。"
          }
          title="暂无本地活动"
        />
      ) : null}

      {items.length > 0 ? (
        <div className="client-activity-list">
          {items.map((activity) => {
            const deleted = activity.deletedAt !== null;
            const system = activity.kind === "system_reference";
            const projectWorkflowEvent =
              system && activity.sourceType === "project_workflow_event";
            const Icon =
              activity.kind === "meeting"
                ? UsersRound
                : system
                  ? CalendarClock
                  : MessageSquareText;
            return (
              <article
                className={deleted ? "is-deleted" : undefined}
                key={activity.id}
              >
                <span className="client-activity-icon" aria-hidden="true">
                  <Icon size={14} />
                </span>
                <div className="client-activity-copy">
                  <div>
                    <strong>{activity.title}</strong>
                    <span>
                      {activity.kind === "meeting"
                        ? "会议记录"
                        : projectWorkflowEvent
                          ? "项目生命周期"
                          : system
                            ? "系统引用"
                            : "沟通笔记"}
                    </span>
                  </div>
                  {deleted ? (
                    <p>
                      已于 {formatActivityTime(activity.deletedAt!)} 删除
                      {activity.deleteReason
                        ? `：${activity.deleteReason}`
                        : ""}
                    </p>
                  ) : system ? (
                    projectWorkflowEvent ? (
                      <p>来源：项目状态变更 · 系统只读</p>
                    ) : (
                      <p>
                        引用 {activity.sourceType} · {activity.sourceId}
                      </p>
                    )
                  ) : (
                    <p>{activity.body}</p>
                  )}
                  <small>
                    {formatActivityTime(activity.occurredAt)} · 由{" "}
                    {activity.createdBy.displayName} 记录
                  </small>
                </div>
                {!deleted && !system ? (
                  <div className="client-activity-actions">
                    <button
                      aria-label={`编辑活动 ${activity.title}`}
                      className="icon-button"
                      disabled={pending}
                      onClick={() => openEdit(activity)}
                      type="button"
                    >
                      <Pencil size={13} />
                    </button>
                    <button
                      aria-label={`删除活动 ${activity.title}`}
                      className="icon-button icon-button-danger"
                      disabled={pending}
                      onClick={() => {
                        resetFeedback();
                        setEditing(null);
                        setDeleteCandidate(activity);
                        setDeleteReason("");
                      }}
                      type="button"
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                ) : (
                  <FileText className="client-activity-readonly" size={13} />
                )}
              </article>
            );
          })}
        </div>
      ) : null}

      {totalPages > 1 ? (
        <nav aria-label="客户活动分页" className="pagination">
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

      {deleteCandidate ? (
        <div className="client-activity-delete" role="alert">
          <div>
            <strong>删除“{deleteCandidate.title}”？</strong>
            <p>
              活动会从默认时间线隐藏，但保留删除时间、记录人和原因用于本地追溯。
            </p>
          </div>
          <label>
            <span>删除原因</span>
            <textarea
              autoFocus
              disabled={pending}
              maxLength={1_000}
              onChange={(event) => setDeleteReason(event.target.value)}
              rows={2}
              value={deleteReason}
            />
          </label>
          <div className="client-activity-editor-actions">
            <button
              className="button button-secondary"
              disabled={pending}
              onClick={() => setDeleteCandidate(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={pending}
              onClick={confirmDelete}
              type="button"
            >
              <Trash2 size={13} />
              {deleteMutation.isPending ? "正在删除…" : "确认删除活动"}
            </button>
          </div>
        </div>
      ) : null}

      {localError || mutationError ? (
        <div className="form-error" role="alert">
          {localError ?? mutationError}
        </div>
      ) : null}
    </section>
  );
}
