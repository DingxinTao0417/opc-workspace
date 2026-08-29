import {
  AlertTriangle,
  ArrowUpRight,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  History,
  Link2,
  ListTree,
  LoaderCircle,
  Search,
  ShieldAlert,
  Unlink2,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  useInboxItemTasksQuery,
  useForceResolveInboxItem,
  useLinkInboxItemTask,
  useTaskPageQuery,
  useUnlinkInboxItemTask,
  useUpdateInboxItemTaskRequirement,
} from "../api/hooks";
import type {
  InboxItem,
  InboxItemTaskRelation,
  InboxTaskProgress,
  TaskStatus,
} from "../types/models";
import { useUiStore } from "../store/ui";
import { InboxTaskOrchestrationModal } from "./InboxTaskOrchestrationModal";

const taskStatusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "已阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

type EditorState =
  | { type: "link" }
  | {
      type: "requirement";
      relation: InboxItemTaskRelation;
      isRequired: boolean;
    }
  | { type: "unlink"; relation: InboxItemTaskRelation }
  | null;

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "关联任务操作失败，请重试。";
}

function isConflict(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 409 || error.code === "VERSION_CONFLICT")
  );
}

function dedupeRelations(
  relations: InboxItemTaskRelation[],
): InboxItemTaskRelation[] {
  const seen = new Set<string>();
  return relations.filter((relation) => {
    if (seen.has(relation.id)) return false;
    seen.add(relation.id);
    return true;
  });
}

function TaskRelationRow({
  relation,
  readOnly,
  disabled,
  onRequirement,
  onUnlink,
  onOpen,
}: {
  relation: InboxItemTaskRelation;
  readOnly: boolean;
  disabled: boolean;
  onRequirement: () => void;
  onUnlink: () => void;
  onOpen: () => void;
}) {
  const task = relation.task;
  return (
    <li className="inbox-task-row">
      <span
        aria-hidden="true"
        className={`inbox-task-status-dot task-${task?.status ?? "deleted"}`}
      />
      <div className="inbox-task-row-copy">
        <div className="inbox-task-row-title">
          <strong>{task?.title ?? relation.taskTitleSnapshot}</strong>
          <span
            className={
              relation.isRequired
                ? "inbox-task-requirement required"
                : "inbox-task-requirement"
            }
          >
            {relation.isRequired ? "必需" : "可选"}
          </span>
        </div>
        <p>
          {relation.taskDeleted
            ? "任务已删除 · 保留历史快照"
            : `${taskStatusLabels[task!.status]} · ${task!.projectName ?? "无项目"} · ${task!.priority}`}
        </p>
      </div>
      {!relation.taskDeleted ? (
        <div className="inbox-task-row-actions">
          <button
            aria-label={`打开任务“${task?.title ?? relation.taskTitleSnapshot}”`}
            className="form-inline-action"
            disabled={disabled}
            onClick={onOpen}
            type="button"
          >
            <ArrowUpRight size={12} />
            打开任务
          </button>
          {!readOnly ? (
            <>
              <button
                className="form-inline-action"
                disabled={disabled}
                onClick={onRequirement}
                type="button"
              >
                {relation.isRequired ? "改为可选" : "设为必需"}
              </button>
              <button
                aria-label={`解除与任务“${task?.title ?? relation.taskTitleSnapshot}”的关联`}
                className="form-inline-action danger"
                disabled={disabled}
                onClick={onUnlink}
                type="button"
              >
                <Unlink2 size={12} />
                解除
              </button>
            </>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}

function ProgressSummary({ progress }: { progress: InboxTaskProgress }) {
  if (progress.requiredTotal === 0) {
    return (
      <p className="inbox-task-progress-empty">
        {progress.activeTotal > 0
          ? `${progress.activeTotal} 个关联任务，尚未设置必需任务`
          : "关联任务后可在这里跟踪处理进度"}
      </p>
    );
  }
  return (
    <div className="inbox-task-progress">
      <div className="inbox-task-progress-copy">
        <span>
          必需任务 {progress.requiredDone}/{progress.requiredTotal}
        </span>
        <strong>{progress.percent}%</strong>
      </div>
      <div
        aria-label={`必需任务完成 ${progress.percent}%`}
        aria-valuemax={100}
        aria-valuemin={0}
        aria-valuenow={progress.percent ?? undefined}
        className="inbox-task-progress-track"
        role="progressbar"
      >
        <span style={{ width: `${progress.percent ?? 0}%` }} />
      </div>
      {progress.allRequiredDone ? (
        <p className="inbox-task-progress-note success">
          <CheckCircle2 size={12} />
          所有必需任务均已完成
        </p>
      ) : progress.requiredBlocked > 0 ||
        progress.requiredWaitingReview > 0 ||
        progress.requiredCancelled > 0 ? (
        <p className="inbox-task-progress-note">
          {progress.requiredBlocked > 0
            ? `${progress.requiredBlocked} 个阻塞`
            : null}
          {progress.requiredBlocked > 0 &&
          (progress.requiredWaitingReview > 0 || progress.requiredCancelled > 0)
            ? " · "
            : null}
          {progress.requiredWaitingReview > 0
            ? `${progress.requiredWaitingReview} 个待验收`
            : null}
          {progress.requiredWaitingReview > 0 && progress.requiredCancelled > 0
            ? " · "
            : null}
          {progress.requiredCancelled > 0
            ? `${progress.requiredCancelled} 个已取消`
            : null}
        </p>
      ) : null}
    </div>
  );
}

export function InboxItemTasksSection({
  item,
  disabled = false,
  onBusyChange,
  onRefreshItem,
}: {
  item: InboxItem;
  disabled?: boolean;
  onBusyChange?: (busy: boolean) => void;
  onRefreshItem?: () => Promise<unknown>;
}) {
  const relationsQuery = useInboxItemTasksQuery(item.id, { pageSize: 20 });
  const linkMutation = useLinkInboxItemTask();
  const requirementMutation = useUpdateInboxItemTaskRequirement();
  const unlinkMutation = useUnlinkInboxItemTask();
  const forceResolveMutation = useForceResolveInboxItem();
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const [editor, setEditor] = useState<EditorState>(null);
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [candidatePage, setCandidatePage] = useState(1);
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [linkRequired, setLinkRequired] = useState(true);
  const [unlinkReason, setUnlinkReason] = useState("");
  const [observedVersion, setObservedVersion] = useState(item.version);
  const [localError, setLocalError] = useState<string | null>(null);
  const [conflictNotice, setConflictNotice] = useState<string | null>(null);
  const [conflictRefreshFailed, setConflictRefreshFailed] = useState(false);
  const [splitOpen, setSplitOpen] = useState(false);
  const [forceResolveOpen, setForceResolveOpen] = useState(false);
  const [forceResolveReason, setForceResolveReason] = useState("");
  const linkButtonRef = useRef<HTMLButtonElement>(null);
  const terminal = item.status === "resolved" || item.status === "dismissed";
  const busy =
    linkMutation.isPending ||
    requirementMutation.isPending ||
    unlinkMutation.isPending ||
    forceResolveMutation.isPending;
  const interactionDisabled = disabled || busy || terminal;

  useEffect(() => onBusyChange?.(busy), [busy, onBusyChange]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(search.trim());
      setCandidatePage(1);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [search]);

  const pages = relationsQuery.data?.pages ?? [];
  const active = useMemo(
    () => dedupeRelations(pages.flatMap((page) => page.active)),
    [pages],
  );
  const history = useMemo(
    () => dedupeRelations(pages.flatMap((page) => page.history)),
    [pages],
  );
  const latestMeta = pages[0]?.meta;
  const progress = latestMeta?.progress;
  const currentVersion = Math.max(
    item.version,
    latestMeta?.inboxItemVersion ?? item.version,
  );
  const activeTaskIds = useMemo(
    () => new Set(active.map((relation) => relation.taskRefId)),
    [active],
  );
  const candidateQuery = useTaskPageQuery(
    {
      page: candidatePage,
      pageSize: 20,
      q: debouncedSearch || undefined,
      sort: "title",
    },
    editor?.type === "link",
  );
  const candidates = useMemo(
    () =>
      (candidateQuery.data?.items ?? []).filter(
        (task) => !activeTaskIds.has(task.id),
      ),
    [activeTaskIds, candidateQuery.data?.items],
  );
  const candidateResultsCurrent =
    search.trim() === debouncedSearch && !candidateQuery.isPlaceholderData;
  const selectedTaskVisible =
    candidateResultsCurrent &&
    selectedTaskId !== null &&
    candidates.some((task) => task.id === selectedTaskId);

  useEffect(() => {
    if (
      !selectedTaskId ||
      candidateQuery.isPending ||
      candidateQuery.isPlaceholderData
    ) {
      return;
    }
    if (!candidates.some((task) => task.id === selectedTaskId)) {
      setSelectedTaskId(null);
    }
  }, [
    candidateQuery.isPending,
    candidateQuery.isPlaceholderData,
    candidates,
    selectedTaskId,
  ]);

  const resetMutationState = () => {
    linkMutation.reset();
    requirementMutation.reset();
    unlinkMutation.reset();
    setLocalError(null);
    setConflictNotice(null);
    setConflictRefreshFailed(false);
  };

  const closeEditor = () => {
    if (disabled || busy) return;
    setEditor(null);
    setSelectedTaskId(null);
    setUnlinkReason("");
    resetMutationState();
    window.setTimeout(() => linkButtonRef.current?.focus(), 0);
  };

  const finishEditor = () => {
    setEditor(null);
    setSelectedTaskId(null);
    setUnlinkReason("");
    setLocalError(null);
    setConflictNotice(null);
    setConflictRefreshFailed(false);
    window.setTimeout(() => linkButtonRef.current?.focus(), 0);
  };

  const refreshAfterConflict = async () => {
    const [relationsResult, itemResult] = await Promise.allSettled([
      relationsQuery.refetch(),
      onRefreshItem?.() ?? Promise.resolve(),
    ]);
    if (
      relationsResult.status === "fulfilled" &&
      !relationsResult.value.isError &&
      relationsResult.value.data?.pages[0] &&
      itemResult.status === "fulfilled"
    ) {
      setObservedVersion(
        relationsResult.value.data.pages[0].meta.inboxItemVersion,
      );
      setConflictRefreshFailed(false);
      setConflictNotice(
        "条目已在其他窗口变化。最新版本已读取，仍有效的选择和填写内容已保留；请检查后再次确认。",
      );
      return;
    }
    setConflictRefreshFailed(true);
    setConflictNotice(
      "检测到版本冲突，但未能读取服务器最新版本；当前选择和填写内容仍保留，请重新读取后再确认。",
    );
  };

  const handleMutationError = (error: unknown) => {
    if (isConflict(error)) {
      void refreshAfterConflict();
      return;
    }
    setLocalError(errorMessage(error));
  };

  const submitLink = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (interactionDisabled) return;
    if (!selectedTaskId || !selectedTaskVisible) {
      setLocalError("请选择要关联的任务。");
      return;
    }
    setLocalError(null);
    setConflictNotice(null);
    linkMutation.mutate(
      {
        inboxItemId: item.id,
        taskId: selectedTaskId,
        isRequired: linkRequired,
        expectedVersion: observedVersion,
      },
      {
        onError: handleMutationError,
        onSuccess: finishEditor,
      },
    );
  };

  const submitRequirement = () => {
    if (
      editor?.type !== "requirement" ||
      interactionDisabled ||
      !activeTaskIds.has(editor.relation.taskRefId)
    ) {
      return;
    }
    setLocalError(null);
    setConflictNotice(null);
    requirementMutation.mutate(
      {
        inboxItemId: item.id,
        taskId: editor.relation.taskRefId,
        isRequired: editor.isRequired,
        expectedVersion: observedVersion,
      },
      {
        onError: handleMutationError,
        onSuccess: finishEditor,
      },
    );
  };

  const submitUnlink = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (
      editor?.type !== "unlink" ||
      interactionDisabled ||
      !activeTaskIds.has(editor.relation.taskRefId)
    ) {
      return;
    }
    const reason = unlinkReason.trim();
    if (!reason) {
      setLocalError("请填写解除关联的原因。");
      return;
    }
    if (unicodeLength(reason) > 1_000) {
      setLocalError("解除原因不能超过 1,000 个字符。");
      return;
    }
    setLocalError(null);
    setConflictNotice(null);
    unlinkMutation.mutate(
      {
        inboxItemId: item.id,
        taskId: editor.relation.taskRefId,
        reason,
        expectedVersion: observedVersion,
      },
      {
        onError: handleMutationError,
        onSuccess: finishEditor,
      },
    );
  };

  const openLinkEditor = () => {
    if (interactionDisabled) return;
    resetMutationState();
    setEditor({ type: "link" });
    setObservedVersion(currentVersion);
    setSelectedTaskId(null);
    setSearch("");
    setDebouncedSearch("");
    setCandidatePage(1);
    setLinkRequired(true);
  };

  const operationError =
    localError ??
    conflictNotice ??
    (linkMutation.error ? errorMessage(linkMutation.error) : null) ??
    (requirementMutation.error
      ? errorMessage(requirementMutation.error)
      : null) ??
    (unlinkMutation.error ? errorMessage(unlinkMutation.error) : null) ??
    (forceResolveMutation.error
      ? errorMessage(forceResolveMutation.error)
      : null);

  const submitForceResolve = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (interactionDisabled) return;
    const reason = forceResolveReason.trim();
    if (!reason) {
      setLocalError("请填写强制解决原因。");
      return;
    }
    setLocalError(null);
    forceResolveMutation.mutate(
      {
        id: item.id,
        expectedVersion: currentVersion,
        reason,
        confirm: true,
      },
      {
        onError: handleMutationError,
        onSuccess: async () => {
          setForceResolveOpen(false);
          setForceResolveReason("");
          await onRefreshItem?.();
        },
      },
    );
  };

  return (
    <section
      aria-labelledby="inbox-tasks-heading"
      className="inbox-task-relations"
    >
      <div className="inbox-task-relations-heading">
        <div>
          <h3 id="inbox-tasks-heading">关联任务</h3>
          <p>任务状态由任务模块维护，这里只展示关系与派生进度。</p>
        </div>
        {!terminal && editor?.type !== "link" ? (
          <div className="inbox-task-heading-actions">
            <button
              className="button button-secondary inbox-task-link-button"
              disabled={disabled || busy}
              onClick={openLinkEditor}
              ref={linkButtonRef}
              type="button"
            >
              <Link2 size={13} />
              关联已有任务
            </button>
            <button
              className="button button-primary inbox-task-link-button"
              disabled={disabled || busy}
              onClick={() => setSplitOpen(true)}
              type="button"
            >
              <ListTree size={13} />
              拆分并分派
            </button>
          </div>
        ) : null}
      </div>

      {relationsQuery.isPending ? (
        <div className="inbox-task-state">
          <LoaderCircle className="animate-spin" size={14} />
          正在读取关联任务…
        </div>
      ) : null}
      {relationsQuery.isError ? (
        <div className="inbox-task-error" role="alert">
          <AlertTriangle size={14} />
          <span>{errorMessage(relationsQuery.error)}</span>
          <button
            className="form-inline-action"
            onClick={() => void relationsQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : null}

      {progress ? <ProgressSummary progress={progress} /> : null}

      {!relationsQuery.isPending && !relationsQuery.isError ? (
        active.length > 0 ? (
          <ul className="inbox-task-list">
            {active.map((relation) => (
              <TaskRelationRow
                disabled={disabled || busy || editor !== null}
                key={relation.id}
                onRequirement={() => {
                  resetMutationState();
                  setObservedVersion(currentVersion);
                  setEditor({
                    type: "requirement",
                    relation,
                    isRequired: !relation.isRequired,
                  });
                }}
                onOpen={() => {
                  if (relation.task) setTaskDetailId(relation.task.id);
                }}
                onUnlink={() => {
                  resetMutationState();
                  setObservedVersion(currentVersion);
                  setUnlinkReason("");
                  setEditor({ type: "unlink", relation });
                }}
                readOnly={terminal}
                relation={relation}
              />
            ))}
          </ul>
        ) : (
          <div className="inbox-task-empty">
            <Link2 aria-hidden="true" size={17} />
            <div>
              <strong>尚未关联任务</strong>
              <p>
                {terminal
                  ? "该条目已结束，仅保留已有关系供查看。"
                  : "关联已有任务后，可从收件箱跟踪执行进度。"}
              </p>
            </div>
          </div>
        )
      ) : null}

      {editor?.type === "link" ? (
        <form className="inbox-task-editor" onSubmit={submitLink}>
          <div className="inbox-task-editor-title">
            <strong>关联已有任务</strong>
            <p>
              仅建立与已有任务的关系；如需一次创建多项工作，请使用“拆分并分派”。
            </p>
          </div>
          <label className="form-field form-field-last">
            <span>搜索任务</span>
            <span className="inbox-task-search">
              <Search aria-hidden="true" size={14} />
              <input
                autoFocus
                disabled={interactionDisabled}
                onChange={(event) => {
                  setSelectedTaskId(null);
                  setSearch(event.target.value);
                }}
                placeholder="按任务标题搜索…"
                value={search}
              />
            </span>
          </label>
          <div className="inbox-task-candidates">
            {candidateQuery.isPending ? (
              <div className="inbox-task-state">
                <LoaderCircle className="animate-spin" size={14} />
                正在读取任务…
              </div>
            ) : null}
            {candidateQuery.isError ? (
              <div className="inbox-task-error" role="alert">
                <AlertTriangle size={14} />
                <span>任务列表读取失败。</span>
                <button
                  className="form-inline-action"
                  disabled={interactionDisabled}
                  onClick={() => void candidateQuery.refetch()}
                  type="button"
                >
                  重试
                </button>
              </div>
            ) : null}
            {!candidateQuery.isPending &&
            !candidateQuery.isError &&
            candidates.length === 0 ? (
              <div className="inbox-task-state">没有可关联的任务</div>
            ) : null}
            {candidates.map((task) => (
              <button
                aria-pressed={selectedTaskId === task.id}
                className="inbox-task-candidate"
                disabled={interactionDisabled || !candidateResultsCurrent}
                key={task.id}
                onClick={() => setSelectedTaskId(task.id)}
                type="button"
              >
                <span
                  aria-hidden="true"
                  className={`inbox-task-status-dot task-${task.status}`}
                />
                <span>
                  <strong>{task.title}</strong>
                  <small>
                    {taskStatusLabels[task.status]} ·{" "}
                    {task.projectName ?? "无项目"} · {task.priority}
                  </small>
                </span>
              </button>
            ))}
          </div>
          {candidateQuery.data && candidateQuery.data.meta.total > 20 ? (
            <div className="inbox-task-pagination">
              <button
                aria-label="上一页任务"
                className="form-inline-action"
                disabled={
                  interactionDisabled ||
                  candidatePage <= 1 ||
                  candidateQuery.isFetching
                }
                onClick={() => {
                  setSelectedTaskId(null);
                  setCandidatePage((page) => page - 1);
                }}
                type="button"
              >
                <ChevronLeft size={13} />
              </button>
              <span>
                {candidatePage} /{" "}
                {Math.max(
                  1,
                  Math.ceil(
                    candidateQuery.data.meta.total /
                      candidateQuery.data.meta.pageSize,
                  ),
                )}
              </span>
              <button
                aria-label="下一页任务"
                className="form-inline-action"
                disabled={
                  candidatePage * candidateQuery.data.meta.pageSize >=
                    candidateQuery.data.meta.total ||
                  candidateQuery.isFetching ||
                  interactionDisabled
                }
                onClick={() => {
                  setSelectedTaskId(null);
                  setCandidatePage((page) => page + 1);
                }}
                type="button"
              >
                <ChevronRight size={13} />
              </button>
            </div>
          ) : null}
          <label className="inbox-task-required-toggle">
            <input
              checked={linkRequired}
              disabled={interactionDisabled}
              onChange={(event) => setLinkRequired(event.target.checked)}
              type="checkbox"
            />
            <span>
              <strong>作为必需任务</strong>
              <small>必需任务会计入条目的处理进度。</small>
            </span>
          </label>
          <div className="inbox-detail-editor-actions">
            <button
              className="button button-secondary"
              disabled={disabled || busy}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={
                !selectedTaskVisible ||
                interactionDisabled ||
                conflictRefreshFailed
              }
              type="submit"
            >
              {linkMutation.isPending ? "正在关联…" : "确认关联"}
            </button>
          </div>
        </form>
      ) : null}

      {editor?.type === "requirement" ? (
        <div className="inbox-task-editor">
          <div className="inbox-task-editor-title">
            <strong>
              {editor.isRequired ? "设为必需任务" : "改为可选任务"}
            </strong>
            <p>
              “
              {editor.relation.task?.title ?? editor.relation.taskTitleSnapshot}
              ”
              {editor.isRequired
                ? "将计入条目处理进度。"
                : "将不再影响条目处理进度。"}
            </p>
          </div>
          <div className="inbox-detail-editor-actions">
            <button
              className="button button-secondary"
              disabled={disabled || busy}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={
                interactionDisabled ||
                conflictRefreshFailed ||
                !activeTaskIds.has(editor.relation.taskRefId)
              }
              onClick={submitRequirement}
              type="button"
            >
              {requirementMutation.isPending ? "正在保存…" : "确认修改"}
            </button>
          </div>
        </div>
      ) : null}

      {editor?.type === "unlink" ? (
        <form className="inbox-task-editor" onSubmit={submitUnlink}>
          <div className="inbox-task-editor-title">
            <strong>解除任务关联</strong>
            <p>关系会进入历史记录，任务本身不会被修改或删除。</p>
          </div>
          <label className="form-field form-field-last">
            <span>解除原因</span>
            <textarea
              autoFocus
              disabled={interactionDisabled}
              maxLength={1_000}
              onChange={(event) => setUnlinkReason(event.target.value)}
              placeholder="说明为什么不再跟踪此任务…"
              rows={3}
              value={unlinkReason}
            />
          </label>
          <div className="inbox-detail-editor-actions">
            <button
              className="button button-secondary"
              disabled={disabled || busy}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={
                !unlinkReason.trim() ||
                interactionDisabled ||
                conflictRefreshFailed ||
                !activeTaskIds.has(editor.relation.taskRefId)
              }
              type="submit"
            >
              {unlinkMutation.isPending ? "正在解除…" : "确认解除"}
            </button>
          </div>
        </form>
      ) : null}

      {operationError ? (
        <div className="inbox-task-operation-error" role="alert">
          <AlertTriangle size={14} />
          <span>{operationError}</span>
          {conflictRefreshFailed ? (
            <button
              className="form-inline-action"
              onClick={() => void refreshAfterConflict()}
              type="button"
            >
              重新读取
            </button>
          ) : null}
        </div>
      ) : null}

      {!terminal &&
      item.resolutionPolicy === "all_required_tasks_done" &&
      progress &&
      !progress.allRequiredDone ? (
        <div className="inbox-force-resolve-zone">
          {!forceResolveOpen ? (
            <button
              className="form-inline-action danger"
              disabled={disabled || busy}
              onClick={() => {
                setForceResolveReason("");
                setForceResolveOpen(true);
              }}
              type="button"
            >
              <ShieldAlert size={13} />
              例外：强制解决
            </button>
          ) : (
            <form className="inbox-task-editor" onSubmit={submitForceResolve}>
              <div className="inbox-task-editor-title">
                <strong>强制解决此收件箱项</strong>
                <p>未完成的必需任务会保留，操作原因将写入不可变事件记录。</p>
              </div>
              <label className="form-field form-field-last">
                <span>例外原因</span>
                <textarea
                  autoFocus
                  disabled={disabled || busy}
                  maxLength={2_000}
                  onChange={(event) =>
                    setForceResolveReason(event.target.value)
                  }
                  placeholder="说明为什么无需等待必需任务完成…"
                  rows={3}
                  value={forceResolveReason}
                />
              </label>
              <div className="inbox-detail-editor-actions">
                <button
                  className="button button-secondary"
                  disabled={disabled || busy}
                  onClick={() => setForceResolveOpen(false)}
                  type="button"
                >
                  取消
                </button>
                <button
                  className="button button-danger"
                  disabled={!forceResolveReason.trim() || disabled || busy}
                  type="submit"
                >
                  {forceResolveMutation.isPending
                    ? "正在解决…"
                    : "确认强制解决"}
                </button>
              </div>
            </form>
          )}
        </div>
      ) : null}

      {history.length > 0 || (latestMeta?.total ?? 0) > 0 ? (
        <details className="inbox-task-history">
          <summary>
            <History size={13} />
            已解除的关联（{latestMeta?.total ?? history.length}）
          </summary>
          {history.length > 0 ? (
            <ul>
              {history.map((relation) => (
                <li key={relation.id}>
                  <span>
                    {relation.task?.title ?? relation.taskTitleSnapshot}
                  </span>
                  <small>
                    {relation.taskDeleted ? "原任务已删除 · " : null}
                    {relation.unlinkReason} ·{" "}
                    {relation.unlinkedByActor?.displayName}
                  </small>
                  {relation.task ? (
                    <button
                      aria-label={`打开任务“${relation.task.title}”`}
                      className="form-inline-action"
                      onClick={() => setTaskDetailId(relation.task!.id)}
                      type="button"
                    >
                      <ArrowUpRight size={12} />
                      打开任务
                    </button>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : null}
          {relationsQuery.hasNextPage ? (
            <button
              className="form-inline-action inbox-task-history-more"
              disabled={relationsQuery.isFetchingNextPage}
              onClick={() => void relationsQuery.fetchNextPage()}
              type="button"
            >
              {relationsQuery.isFetchingNextPage ? "正在读取…" : "加载更早"}
            </button>
          ) : null}
        </details>
      ) : null}

      {splitOpen ? (
        <InboxTaskOrchestrationModal
          expectedVersion={currentVersion}
          item={item}
          onClose={() => setSplitOpen(false)}
          onCreated={onRefreshItem}
          open
        />
      ) : null}
    </section>
  );
}
