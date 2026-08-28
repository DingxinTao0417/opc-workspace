import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  Clock3,
  History,
  LoaderCircle,
  UserRound,
  UserRoundPlus,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useAssignmentActorOptionsQuery,
  useCreateTaskAssignment,
  useEndTaskAssignment,
  useReassignTaskAssignment,
  useTaskAssignmentsQuery,
} from "../api/hooks";
import type {
  Actor,
  ActorSummary,
  AssignmentRole,
  Task,
  TaskAssignment,
} from "../types/models";

type AssignmentAction = "assign" | "reassign" | "end";

interface AssignmentEditorState {
  action: AssignmentAction;
  role: AssignmentRole;
  assignmentId?: string;
  currentActorId?: string;
  actorId: string;
  reason: string;
  expectedVersion: number;
}

interface TaskAssignmentsSectionProps {
  task: Task;
  disabled?: boolean;
  onBusyChange?: (busy: boolean) => void;
  onTaskUpdated?: (task: Task) => void;
}

const roleLabels: Record<AssignmentRole, string> = {
  assignee: "负责人",
  reviewer: "审核人",
};

const actorTypeLabels: Record<ActorSummary["type"], string> = {
  owner: "所有者",
  person: "本地人员",
  system: "系统",
  agent: "Agent",
};

const assignmentReasonLabels: Readonly<Record<string, string>> = {
  "Task completed": "任务完成后自动结束",
};

const errorMessages: Record<string, string> = {
  VERSION_REQUIRED: "任务版本缺失，请刷新详情后重试。",
  EXPECTED_VERSION_REQUIRED: "任务版本缺失，请刷新详情后重试。",
  ASSIGNMENT_ACTOR_NOT_ACTIVE: "所选责任人已停用，请重新选择。",
  ASSIGNMENT_ALREADY_ACTIVE: "此角色已有当前分派，已重新读取最新状态。",
  ASSIGNMENT_NOT_ACTIVE: "这条分派已经结束，已重新读取最新状态。",
  ASSIGNMENT_UNCHANGED: "新责任人与当前责任人相同，请重新选择。",
  TASK_NOT_ASSIGNABLE: "已完成的任务不能首次分派或改派。",
  TASK_NOT_FOUND: "任务已不存在，请关闭详情。",
  ASSIGNMENT_NOT_FOUND: "分派记录已不存在，已重新读取最新状态。",
  ACTOR_NOT_FOUND: "所选责任人已不存在，请重新选择。",
  ASSIGNMENT_ACTOR_TYPE_NOT_ALLOWED: "该责任主体不能用于此角色。",
  ASSIGNMENT_REVIEWER_MUST_BE_OWNER: "审核人只能选择所有者。",
  IDEMPOTENCY_CONFLICT: "本次操作与已有重试记录不一致，请刷新后重试。",
  IDEMPOTENCY_REPLAY_UNAVAILABLE:
    "无法安全重放本次操作，请刷新后确认最新状态。",
};

function actorInitials(name: string): string {
  return Array.from(name.trim()).slice(0, 2).join("") || "?";
}

function formatAssignmentTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatAssignmentReason(reason: string): string {
  return assignmentReasonLabels[reason] ?? reason;
}

function assignmentErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") return null;
    const message = errorMessages[error.code] ?? error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "责任分派操作失败，请重试。";
}

function ActorAvatar({ actor }: { actor: ActorSummary | Actor }) {
  return (
    <span className="task-assignment-avatar" data-type={actor.type}>
      {actorInitials(actor.displayName)}
    </span>
  );
}

interface AssignmentCardProps {
  assignment: TaskAssignment | null;
  disabled: boolean;
  role: AssignmentRole;
  taskDone: boolean;
  onEdit: (
    action: AssignmentAction,
    role: AssignmentRole,
    assignment?: TaskAssignment,
  ) => void;
}

function AssignmentCard({
  assignment,
  disabled,
  role,
  taskDone,
  onEdit,
}: AssignmentCardProps) {
  const label = roleLabels[role];
  if (!assignment) {
    return (
      <article className="task-assignment-card task-assignment-card-empty">
        <div className="task-assignment-card-heading">
          <span>{label}</span>
        </div>
        <UserRoundPlus aria-hidden="true" size={19} />
        <strong>{role === "assignee" ? "未分派负责人" : "未设置审核人"}</strong>
        <span>
          {taskDone
            ? "已完成任务不能新增分派"
            : role === "assignee"
              ? "选择所有者或本地人员"
              : "v0.1 仅支持所有者审核"}
        </span>
        <button
          className="button button-secondary"
          disabled={disabled || taskDone}
          onClick={() => onEdit("assign", role)}
          type="button"
        >
          {role === "assignee" ? "分派负责人" : "设置审核人"}
        </button>
      </article>
    );
  }

  return (
    <article className="task-assignment-card">
      <div className="task-assignment-card-heading">
        <span>{label}</span>
        <div className="task-assignment-card-actions">
          <button
            className="button button-quiet"
            disabled={disabled || taskDone}
            onClick={() => onEdit("reassign", role, assignment)}
            title={taskDone ? "已完成任务不能改派" : undefined}
            type="button"
          >
            改派
          </button>
          <button
            className="button button-quiet"
            disabled={disabled}
            onClick={() => onEdit("end", role, assignment)}
            type="button"
          >
            结束
          </button>
        </div>
      </div>
      <div className="task-assignment-person">
        <ActorAvatar actor={assignment.actor} />
        <div>
          <div className="task-assignment-person-title">
            <strong>{assignment.actor.displayName}</strong>
            <span>{actorTypeLabels[assignment.actor.type]}</span>
            {assignment.inferred ? <em>迁移推定</em> : null}
          </div>
          <p>
            {assignment.inferred
              ? "由历史迁移推定"
              : `由 ${assignment.assignedByActor.displayName} 分派`}
            {` · ${formatAssignmentTime(assignment.assignedAt)} 起`}
          </p>
        </div>
      </div>
      {assignment.actor.type === "person" ? (
        <p className="task-assignment-local-note">
          仅在本机记录，不会通知对方或授予访问权限。
        </p>
      ) : null}
    </article>
  );
}

export function TaskAssignmentsSection({
  task,
  disabled = false,
  onBusyChange,
  onTaskUpdated,
}: TaskAssignmentsSectionProps) {
  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyRole, setHistoryRole] = useState<AssignmentRole | "all">("all");
  const [editor, setEditor] = useState<AssignmentEditorState | null>(null);
  const [search, setSearch] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [versionConflict, setVersionConflict] = useState(false);
  const [conflictReadyVersion, setConflictReadyVersion] = useState<
    number | null
  >(null);

  const assignmentsQuery = useTaskAssignmentsQuery(task.id, {
    pageSize: 20,
    role: historyRole === "all" ? undefined : historyRole,
    sort: "-assigned_at",
  });
  const candidatesQuery = useAssignmentActorOptionsQuery(
    Boolean(editor && editor.action !== "end"),
  );
  const createMutation = useCreateTaskAssignment();
  const reassignMutation = useReassignTaskAssignment();
  const endMutation = useEndTaskAssignment();
  const commandBusy =
    createMutation.isPending ||
    reassignMutation.isPending ||
    endMutation.isPending;

  useEffect(() => {
    onBusyChange?.(commandBusy);
  }, [commandBusy, onBusyChange]);

  useEffect(
    () => () => {
      onBusyChange?.(false);
    },
    [onBusyChange],
  );

  useEffect(() => {
    setEditor(null);
    setSearch("");
    setValidationError(null);
    setVersionConflict(false);
    setConflictReadyVersion(null);
  }, [task.id]);

  const pages = assignmentsQuery.data?.pages ?? [];
  const firstPage = pages[0];
  const active = firstPage?.active;
  const latestTaskVersion = Math.max(
    task.version,
    firstPage?.meta.taskVersion ?? task.version,
  );
  const history = useMemo(() => {
    const byId = new Map<string, TaskAssignment>();
    for (const page of pages) {
      for (const assignment of page.history) {
        byId.set(assignment.id, assignment);
      }
    }
    return [...byId.values()];
  }, [pages]);

  const candidates = useMemo(() => {
    if (!editor) return [];
    const query = search.trim().toLocaleLowerCase("zh-CN");
    return (candidatesQuery.data ?? [])
      .filter(
        (actor) =>
          actor.status === "active" &&
          (editor.role === "reviewer"
            ? actor.type === "owner"
            : actor.type === "owner" || actor.type === "person"),
      )
      .filter(
        (actor) =>
          actor.id !== editor.currentActorId &&
          (!query ||
            actor.displayName.toLocaleLowerCase("zh-CN").includes(query)),
      );
  }, [candidatesQuery.data, editor, search]);

  useEffect(() => {
    if (
      editor?.role === "reviewer" &&
      editor.action !== "end" &&
      !editor.actorId
    ) {
      const owner = (candidatesQuery.data ?? []).find(
        (actor) => actor.status === "active" && actor.type === "owner",
      );
      if (owner && owner.id !== editor.currentActorId) {
        setEditor((current) =>
          current ? { ...current, actorId: owner.id } : current,
        );
      }
    }
  }, [candidatesQuery.data, editor]);

  const commandError =
    createMutation.error ?? reassignMutation.error ?? endMutation.error;
  const errorMessage = validationError ?? assignmentErrorMessage(commandError);

  const resetMutations = () => {
    createMutation.reset();
    reassignMutation.reset();
    endMutation.reset();
  };

  const closeEditor = () => {
    if (commandBusy) return;
    setEditor(null);
    setSearch("");
    setValidationError(null);
    setVersionConflict(false);
    setConflictReadyVersion(null);
    resetMutations();
  };

  const openEditor = (
    action: AssignmentAction,
    role: AssignmentRole,
    assignment?: TaskAssignment,
  ) => {
    if (disabled || commandBusy) return;
    setEditor({
      action,
      role,
      assignmentId: assignment?.id,
      currentActorId: assignment?.actorId,
      actorId: "",
      reason: "",
      expectedVersion: latestTaskVersion,
    });
    setSearch("");
    setValidationError(null);
    setVersionConflict(false);
    setConflictReadyVersion(null);
    resetMutations();
  };

  const handleSuccess = (result: { task: Task }) => {
    onTaskUpdated?.(result.task);
    setEditor(null);
    setSearch("");
    setValidationError(null);
    setVersionConflict(false);
    setConflictReadyVersion(null);
  };

  const handleError = (error: unknown) => {
    if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
      setVersionConflict(true);
      setConflictReadyVersion(null);
      const attemptedVersion = editor?.expectedVersion ?? latestTaskVersion;
      void assignmentsQuery.refetch().then((result) => {
        const refreshedVersion = result.data?.pages[0]?.meta.taskVersion;
        if (
          !result.isError &&
          typeof refreshedVersion === "number" &&
          refreshedVersion > attemptedVersion
        ) {
          setConflictReadyVersion(refreshedVersion);
        }
      });
    }
  };

  const submitEditor = () => {
    if (!editor || disabled || commandBusy || versionConflict) return;
    if (task.status === "done" && editor.action !== "end") {
      setValidationError("已完成的任务不能首次分派或改派。");
      return;
    }
    const expectedVersion = editor.expectedVersion;
    if (editor.action === "assign" || editor.action === "reassign") {
      if (!editor.actorId) {
        setValidationError(`请选择${roleLabels[editor.role]}。`);
        return;
      }
      if (editor.actorId === editor.currentActorId) {
        setValidationError("新责任人与当前责任人相同，请重新选择。");
        return;
      }
    }
    const reason = editor.reason.trim();
    if (editor.action !== "assign" && !reason) {
      setValidationError("请填写原因。");
      return;
    }
    setValidationError(null);

    if (editor.action === "assign") {
      createMutation.mutate(
        {
          taskId: task.id,
          input: {
            role: editor.role,
            actorId: editor.actorId,
            expectedVersion,
          },
        },
        { onError: handleError, onSuccess: handleSuccess },
      );
      return;
    }
    if (editor.action === "reassign") {
      reassignMutation.mutate(
        {
          taskId: task.id,
          input: {
            role: editor.role,
            actorId: editor.actorId,
            reason,
            expectedVersion,
          },
        },
        { onError: handleError, onSuccess: handleSuccess },
      );
      return;
    }
    if (!editor.assignmentId) {
      setValidationError("分派记录缺失，请刷新后重试。");
      return;
    }
    endMutation.mutate(
      {
        taskId: task.id,
        assignmentId: editor.assignmentId,
        input: { reason, expectedVersion },
      },
      { onError: handleError, onSuccess: handleSuccess },
    );
  };

  const selectedActor = (candidatesQuery.data ?? []).find(
    (actor) => actor.id === editor?.actorId,
  );

  return (
    <section
      className="task-assignments"
      aria-labelledby="task-assignments-title"
    >
      <div className="task-assignments-heading">
        <div>
          <h3 id="task-assignments-title">责任分派</h3>
          <p>本地责任记录与审核角色</p>
        </div>
        {firstPage ? (
          <button
            aria-expanded={historyOpen}
            className="button button-quiet task-assignment-history-toggle"
            onClick={() => setHistoryOpen((open) => !open)}
            type="button"
          >
            <History size={13} />
            历史 {firstPage.meta.total}
            {historyOpen ? <ChevronUp size={13} /> : <ChevronDown size={13} />}
          </button>
        ) : null}
      </div>

      {assignmentsQuery.isPending && !firstPage ? (
        <div className="task-assignment-grid" aria-label="正在读取责任分派">
          <div className="task-assignment-card task-assignment-card-loading" />
          <div className="task-assignment-card task-assignment-card-loading" />
        </div>
      ) : null}

      {assignmentsQuery.isError && !firstPage ? (
        <div className="task-assignment-query-error" role="alert">
          <AlertTriangle size={15} />
          <div>
            <strong>无法读取责任分派</strong>
            <span>{assignmentErrorMessage(assignmentsQuery.error)}</span>
          </div>
          <button
            className="button button-secondary"
            onClick={() => void assignmentsQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : null}

      {firstPage && active ? (
        <>
          {assignmentsQuery.isError ? (
            <div className="task-assignment-inline-error" role="alert">
              最新责任记录读取失败，当前显示上次快照。
              <button
                className="form-inline-action"
                onClick={() => void assignmentsQuery.refetch()}
                type="button"
              >
                重新读取
              </button>
            </div>
          ) : null}
          <div className="task-assignment-grid">
            <AssignmentCard
              assignment={active.assignee}
              disabled={disabled || commandBusy}
              onEdit={openEditor}
              role="assignee"
              taskDone={task.status === "done"}
            />
            <AssignmentCard
              assignment={active.reviewer}
              disabled={disabled || commandBusy}
              onEdit={openEditor}
              role="reviewer"
              taskDone={task.status === "done"}
            />
          </div>
        </>
      ) : null}

      {editor ? (
        <div className="task-assignment-editor" aria-label="责任分派编辑器">
          <div className="task-assignment-editor-heading">
            <div>
              <strong>
                {editor.action === "assign"
                  ? `设置${roleLabels[editor.role]}`
                  : editor.action === "reassign"
                    ? `改派${roleLabels[editor.role]}`
                    : `结束${roleLabels[editor.role]}记录`}
              </strong>
              <span>
                {editor.action === "assign"
                  ? "首次分派会建立一条新的活动记录。"
                  : editor.action === "reassign"
                    ? "旧记录会结束，新记录会在同一事务中生效。"
                    : "结束不会删除历史，也不会改变任务状态。"}
              </span>
            </div>
            <button
              className="button button-quiet"
              disabled={commandBusy}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
          </div>

          {versionConflict ? (
            <div
              className="task-conflict task-assignment-conflict"
              role="alert"
            >
              <AlertTriangle size={15} />
              <div>
                <strong>责任分派已在其他窗口变化</strong>
                <span>
                  {conflictReadyVersion
                    ? `已读取最新版 v${conflictReadyVersion}，你的选择和原因仍保留。`
                    : assignmentsQuery.isFetching
                      ? "正在读取最新责任分派，你的选择和原因仍保留。"
                      : "尚未确认最新版；请重新读取后再决定是否保留选择。"}
                </span>
              </div>
              <button
                className="button button-secondary"
                onClick={() => {
                  closeEditor();
                  void assignmentsQuery.refetch();
                }}
                type="button"
              >
                载入最新分派
              </button>
              <button
                className="button button-primary"
                disabled={
                  assignmentsQuery.isFetching || conflictReadyVersion === null
                }
                onClick={() => {
                  setEditor((current) =>
                    current
                      ? {
                          ...current,
                          expectedVersion: conflictReadyVersion!,
                        }
                      : current,
                  );
                  setVersionConflict(false);
                  setConflictReadyVersion(null);
                  resetMutations();
                }}
                type="button"
              >
                保留选择
              </button>
            </div>
          ) : null}

          {editor.action !== "end" ? (
            <div className="task-assignment-picker">
              <label className="form-field">
                <span>选择{roleLabels[editor.role]}</span>
                <input
                  aria-label={`搜索${roleLabels[editor.role]}`}
                  disabled={
                    candidatesQuery.isPending || candidatesQuery.isError
                  }
                  onChange={(event) => setSearch(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") event.preventDefault();
                  }}
                  placeholder={
                    editor.role === "reviewer"
                      ? "审核人仅支持所有者"
                      : "搜索所有者或本地人员…"
                  }
                  value={search}
                />
              </label>
              {candidatesQuery.isPending ? (
                <div className="task-assignment-picker-state">
                  <LoaderCircle className="is-spinning" size={14} />
                  正在读取候选人…
                </div>
              ) : null}
              {candidatesQuery.isError ? (
                <div className="task-assignment-picker-error" role="alert">
                  候选人读取失败；当前责任记录仍可查看。
                  <button
                    className="form-inline-action"
                    onClick={() => void candidatesQuery.refetch()}
                    type="button"
                  >
                    重试
                  </button>
                </div>
              ) : null}
              {candidatesQuery.isSuccess ? (
                <div
                  aria-label={`${roleLabels[editor.role]}候选人`}
                  className="task-assignment-options"
                  role="listbox"
                >
                  {candidates.length > 0 ? (
                    candidates.map((actor) => (
                      <button
                        aria-selected={editor.actorId === actor.id}
                        className="task-assignment-option"
                        data-selected={editor.actorId === actor.id}
                        key={actor.id}
                        onClick={() => {
                          setEditor((current) =>
                            current
                              ? { ...current, actorId: actor.id }
                              : current,
                          );
                          setValidationError(null);
                        }}
                        role="option"
                        type="button"
                      >
                        <ActorAvatar actor={actor} />
                        <span>
                          <strong>{actor.displayName}</strong>
                          <small>{actorTypeLabels[actor.type]}</small>
                        </span>
                      </button>
                    ))
                  ) : (
                    <span className="task-assignment-options-empty">
                      没有匹配的可用责任人
                    </span>
                  )}
                </div>
              ) : null}
              {selectedActor?.type === "person" ? (
                <div className="task-assignment-person-notice">
                  <UserRound size={14} />
                  <span>
                    仅在本机记录 {selectedActor.displayName}{" "}
                    为负责人；不会通知对方，也不会授予访问权限。
                  </span>
                </div>
              ) : null}
            </div>
          ) : null}

          {editor.action !== "assign" ? (
            <label className="form-field task-assignment-reason">
              <span>原因</span>
              <textarea
                aria-label={`${editor.action === "end" ? "结束" : "改派"}原因`}
                maxLength={1_000}
                onChange={(event) => {
                  setEditor((current) =>
                    current
                      ? { ...current, reason: event.target.value }
                      : current,
                  );
                  setValidationError(null);
                }}
                placeholder={
                  editor.action === "end"
                    ? "说明为什么结束这条责任记录…"
                    : "说明为什么改派…"
                }
                rows={3}
                value={editor.reason}
              />
              <small>{editor.reason.length}/1000</small>
            </label>
          ) : null}

          {errorMessage ? (
            <div className="form-error" role="alert">
              {errorMessage}
            </div>
          ) : null}

          <div className="task-assignment-editor-actions">
            <button
              className="button button-secondary"
              disabled={commandBusy}
              onClick={closeEditor}
              type="button"
            >
              取消
            </button>
            <button
              className={
                editor.action === "end"
                  ? "button button-danger"
                  : "button button-primary"
              }
              disabled={
                disabled ||
                commandBusy ||
                versionConflict ||
                candidatesQuery.isError ||
                (editor.action !== "end" && !editor.actorId)
              }
              onClick={submitEditor}
              type="button"
            >
              {commandBusy
                ? "正在保存…"
                : editor.action === "assign"
                  ? "确认分派"
                  : editor.action === "reassign"
                    ? "确认改派"
                    : "确认结束"}
            </button>
          </div>
        </div>
      ) : null}

      {historyOpen && firstPage ? (
        <div className="task-assignment-history">
          <div className="task-assignment-history-heading">
            <strong>分派历史</strong>
            <div
              aria-label="历史角色筛选"
              className="task-assignment-history-filter"
            >
              {(["all", "assignee", "reviewer"] as const).map((role) => (
                <button
                  aria-pressed={historyRole === role}
                  data-active={historyRole === role}
                  key={role}
                  onClick={() => setHistoryRole(role)}
                  type="button"
                >
                  {role === "all" ? "全部" : roleLabels[role]}
                </button>
              ))}
            </div>
          </div>
          {history.length > 0 ? (
            <ol className="task-assignment-timeline">
              {history.map((assignment) => (
                <li key={assignment.id}>
                  <ActorAvatar actor={assignment.actor} />
                  <div>
                    <div className="task-assignment-history-title">
                      <strong>{assignment.actor.displayName}</strong>
                      <span>{roleLabels[assignment.role]}</span>
                      {assignment.inferred ? <em>迁移推定</em> : null}
                    </div>
                    <p>
                      <Clock3 size={11} />
                      {formatAssignmentTime(assignment.assignedAt)} —{" "}
                      {formatAssignmentTime(assignment.unassignedAt!)}
                      {!assignment.inferred
                        ? ` · 由 ${assignment.assignedByActor.displayName} 分派`
                        : ""}
                    </p>
                    {assignment.inferred ? (
                      <small>这条责任记录由历史迁移推定。</small>
                    ) : assignment.reason ? (
                      <small title={formatAssignmentReason(assignment.reason)}>
                        原因：{formatAssignmentReason(assignment.reason)}
                      </small>
                    ) : null}
                  </div>
                </li>
              ))}
            </ol>
          ) : (
            <div className="task-assignment-history-empty">
              暂无{historyRole === "all" ? "" : roleLabels[historyRole]}历史记录
            </div>
          )}
          {assignmentsQuery.hasNextPage ? (
            <button
              className="button button-secondary task-assignment-load-more"
              disabled={assignmentsQuery.isFetchingNextPage}
              onClick={() => void assignmentsQuery.fetchNextPage()}
              type="button"
            >
              {assignmentsQuery.isFetchingNextPage ? "正在读取…" : "加载更早"}
            </button>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
