import { BookOpenText, LoaderCircle, Pencil, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useCreateProjectNote,
  useDeleteProjectNote,
  useProjectNotesQuery,
  useUpdateProjectNote,
} from "../api/hooks";
import { useSettledPage } from "../lib/useSettledPage";
import type { ProjectNote } from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

interface ProjectNotesSectionProps {
  projectId: string;
  archived: boolean;
}

interface NoteDraft {
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

function emptyDraft(): NoteDraft {
  return { title: "", body: "", occurredAt: toLocalDateTimeInput(new Date()) };
}

function draftFromNote(note: ProjectNote): NoteDraft {
  return {
    title: note.title,
    body: note.body ?? "",
    occurredAt: toLocalDateTimeInput(note.occurredAt),
  };
}

function formatNoteTime(value: string): string {
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

function noteError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "项目笔记已在其他窗口变化，已刷新列表；当前草稿仍保留，请核对后重试。";
    }
    if (error.code === "PROJECT_NOTE_DELETED") {
      return "该项目笔记已经删除，已刷新列表。";
    }
    if (error.code === "PROJECT_ARCHIVED") {
      return "归档项目只读，请先恢复项目再修改笔记。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "项目笔记操作失败，请重试。";
}

export function ProjectNotesSection({
  projectId,
  archived,
}: ProjectNotesSectionProps) {
  const [page, setPage] = useState(1);
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const queryInput = useMemo(
    () => ({ page, pageSize: 6, includeDeleted }),
    [includeDeleted, page],
  );
  const query = useProjectNotesQuery(projectId, queryInput);
  const createMutation = useCreateProjectNote();
  const updateMutation = useUpdateProjectNote();
  const deleteMutation = useDeleteProjectNote();
  const [editing, setEditing] = useState<ProjectNote | "new" | null>(null);
  const [draft, setDraft] = useState<NoteDraft>(emptyDraft);
  const [deleteCandidate, setDeleteCandidate] = useState<ProjectNote | null>(
    null,
  );
  const [deleteReason, setDeleteReason] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
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
    noteError(createMutation.error) ??
    noteError(updateMutation.error) ??
    noteError(deleteMutation.error);

  useSettledPage({
    page,
    meta: query.data?.meta,
    isFetching: query.isFetching,
    isPlaceholderData: query.isPlaceholderData,
    isSuccess: query.isSuccess,
    setPage,
  });

  useEffect(() => {
    if (!archived) return;
    setEditing(null);
    setDeleteCandidate(null);
    setLocalError(null);
  }, [archived]);

  const resetFeedback = () => {
    setLocalError(null);
    createMutation.reset();
    updateMutation.reset();
    deleteMutation.reset();
  };

  const openNew = () => {
    if (archived) return;
    resetFeedback();
    setDeleteCandidate(null);
    setDraft(emptyDraft());
    setEditing("new");
  };

  const openEdit = (note: ProjectNote) => {
    if (archived) return;
    resetFeedback();
    setDeleteCandidate(null);
    setDraft(draftFromNote(note));
    setEditing(note);
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
      setLocalError("请选择有效的记录时间。");
      return;
    }
    if (occurredAt.getTime() > Date.now() + 5 * 60_000) {
      setLocalError("项目笔记不能记录为未来时间。");
      return;
    }
    resetFeedback();
    if (editing === "new") {
      createMutation.mutate(
        {
          projectId,
          input: { title, body, occurredAt: occurredAt.toISOString() },
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
        projectId,
        id: editing.id,
        input: {
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
        projectId,
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
    <section className="project-detail-section project-notes-section">
      <div className="project-detail-heading">
        <div>
          <h2>项目笔记</h2>
          <p>
            记录项目事实、决策与上下文；系统命令审计仍保存在下方活动时间线。
          </p>
        </div>
        <button
          className="button button-primary"
          disabled={pending || archived}
          onClick={openNew}
          title={archived ? "恢复项目后再添加笔记" : undefined}
          type="button"
        >
          <Plus size={14} />
          添加笔记
        </button>
      </div>

      {archived ? (
        <p className="client-activity-history-toggle">
          归档项目只读；可以查看现有及已删除笔记。
        </p>
      ) : null}
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
          <div className="client-activity-editor-grid project-note-editor-grid">
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
              <span>记录时间</span>
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
              placeholder="记录已确认的事实、决定、风险和下一步。"
              rows={5}
              value={draft.body}
            />
          </label>
          <div className="client-activity-editor-actions">
            <button
              className="button button-secondary"
              disabled={pending}
              onClick={() => setEditing(null)}
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
              {editing === "new" ? "保存笔记" : "保存修改"}
            </button>
          </div>
        </div>
      ) : null}

      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取项目笔记，项目资料与任务仍可继续查看。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          action={
            !archived && !includeDeleted ? (
              <button
                className="button button-secondary"
                onClick={openNew}
                type="button"
              >
                <Plus size={14} /> 添加第一条笔记
              </button>
            ) : undefined
          }
          message={
            includeDeleted
              ? "当前项目没有任何笔记历史。"
              : archived
                ? "该归档项目没有项目笔记。"
                : "添加笔记后，这里会展示真实的本地项目上下文。"
          }
          title="暂无项目笔记"
        />
      ) : null}

      {items.length > 0 ? (
        <div className="client-activity-list">
          {items.map((note) => {
            const deleted = note.deletedAt !== null;
            return (
              <article
                className={deleted ? "is-deleted" : undefined}
                key={note.id}
              >
                <span className="client-activity-icon" aria-hidden="true">
                  <BookOpenText size={14} />
                </span>
                <div className="client-activity-copy">
                  <div>
                    <strong>{note.title}</strong>
                    <span>{deleted ? "已删除" : `v${note.version}`}</span>
                  </div>
                  {deleted ? (
                    <p>
                      已于 {formatNoteTime(note.deletedAt!)} 删除
                      {note.deleteReason ? `：${note.deleteReason}` : ""}
                    </p>
                  ) : (
                    <p>{note.body}</p>
                  )}
                  <small>
                    {formatNoteTime(note.occurredAt)} · 由{" "}
                    {note.createdBy.displayName} 记录
                  </small>
                </div>
                {!deleted && !archived ? (
                  <div className="client-activity-actions">
                    <button
                      aria-label={`编辑笔记 ${note.title}`}
                      className="icon-button"
                      disabled={pending}
                      onClick={() => openEdit(note)}
                      type="button"
                    >
                      <Pencil size={13} />
                    </button>
                    <button
                      aria-label={`删除笔记 ${note.title}`}
                      className="icon-button icon-button-danger"
                      disabled={pending}
                      onClick={() => {
                        resetFeedback();
                        setEditing(null);
                        setDeleteCandidate(note);
                        setDeleteReason("");
                      }}
                      type="button"
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                ) : (
                  <BookOpenText
                    className="client-activity-readonly"
                    size={13}
                  />
                )}
              </article>
            );
          })}
        </div>
      ) : null}

      {totalPages > 1 ? (
        <nav aria-label="项目笔记分页" className="pagination">
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
            <p>笔记正文会隐藏，但删除时间、记录人和原因会保留用于追溯。</p>
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
              {deleteMutation.isPending ? "正在删除…" : "确认删除笔记"}
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
