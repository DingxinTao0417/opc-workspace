import { AlertTriangle, Clock3, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { matchPath, useLocation, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useDeleteTask,
  useTaskOptionsQuery,
  useTaskQuery,
  useUpdateTask,
} from "../api/hooks";
import { useUiStore } from "../store/ui";
import type {
  Task,
  TaskKind,
  TaskPriority,
  TaskReviewPolicy,
  TaskStatus,
} from "../types/models";
import { ErrorState, SkeletonRows } from "./feedback";
import { Modal } from "./Modal";
import { ProjectSelect } from "./ProjectSelect";
import { TaskAssignmentsSection } from "./TaskAssignmentsSection";
import { TaskEventsSection } from "./TaskEventsSection";
import { TaskFocusHistorySection } from "./TaskFocusHistorySection";
import { TaskLifecycleSection } from "./TaskLifecycleSection";
import { TaskOutputsSection } from "./TaskOutputsSection";
import { TaskTagPicker } from "./TaskTagPicker";

const priorities: { value: TaskPriority; label: string }[] = [
  { value: "P0", label: "紧急" },
  { value: "P1", label: "高" },
  { value: "P2", label: "中" },
  { value: "P3", label: "低" },
];

const taskKinds: { value: TaskKind; label: string }[] = [
  { value: "work", label: "工作" },
  { value: "review", label: "复核" },
  { value: "followup", label: "跟进" },
  { value: "reminder", label: "提醒" },
];

const statusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

function toLocalDateTime(value: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function toRfc3339(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function formatUpdatedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function mutationErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") return null;
    if (error.code === "TASK_HAS_ACTIVE_INBOX_RELATIONS") {
      return "该任务仍被收件箱条目关联。请先到收件箱解除活动关联，再删除任务。";
    }
    if (error.code === "TASK_HAS_ACTIVE_INBOX_SOURCES") {
      return "该任务或其产出仍是活动收件箱事项的来源。请先解决或忽略这些来源项，再删除任务。";
    }
    if (error.code === "TASK_CONTENT_ITEMS_EXIST") {
      return "该任务仍被内容日历条目引用。请先到内容日历解除任务关联，再删除任务。";
    }
    const suffix = error.requestId ? ` · 请求 ${error.requestId}` : "";
    return `${error.message}${suffix}`;
  }
  return "任务操作失败，请重试。";
}

export function TaskDetailModal() {
  const taskId = useUiStore((state) => state.taskDetailId);
  const setTaskId = useUiStore((state) => state.setTaskDetailId);
  const location = useLocation();
  const navigate = useNavigate();
  const routedTaskId = matchPath("/tasks/:taskId", location.pathname)?.params
    .taskId;
  const previousRoutedTaskId = useRef<string | null>(null);
  const query = useTaskQuery(taskId);
  const updateMutation = useUpdateTask();
  const deleteMutation = useDeleteTask();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<TaskKind>("work");
  const [priority, setPriority] = useState<TaskPriority>("P2");
  const [plannedDate, setPlannedDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [estimatedMinutes, setEstimatedMinutes] = useState("");
  const [projectId, setProjectId] = useState("");
  const [parentTaskId, setParentTaskId] = useState("");
  const [completionCriteria, setCompletionCriteria] = useState("");
  const [reviewPolicy, setReviewPolicy] = useState<TaskReviewPolicy>("none");
  const [tagIds, setTagIds] = useState<string[]>([]);
  const [draftVersion, setDraftVersion] = useState(1);
  const [versionConflict, setVersionConflict] = useState(false);
  const [conflictLatestTask, setConflictLatestTask] = useState<Task | null>(
    null,
  );
  const [conflictRefreshing, setConflictRefreshing] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [assignmentBusy, setAssignmentBusy] = useState(false);
  const [workflowBusy, setWorkflowBusy] = useState(false);
  const [outputBusy, setOutputBusy] = useState(false);
  const [submissionHistoryState, setSubmissionHistoryState] = useState<{
    loading: boolean;
    error: boolean;
    total: number | null;
  }>({ loading: true, error: false, total: null });
  const task = query.data;
  const tasksQuery = useTaskOptionsQuery(Boolean(taskId));
  const taskWriteBusy = updateMutation.isPending || deleteMutation.isPending;
  const busy = taskWriteBusy || assignmentBusy || workflowBusy || outputBusy;
  const skipNextHydrate = useRef(false);
  const hydratedTaskId = useRef<string | null>(null);

  useEffect(() => {
    if (routedTaskId) {
      previousRoutedTaskId.current = routedTaskId;
      setTaskId(routedTaskId);
      return;
    }
    if (previousRoutedTaskId.current) {
      previousRoutedTaskId.current = null;
      setTaskId(null);
    }
  }, [routedTaskId, setTaskId]);

  const hydrate = (value: Task) => {
    setTitle(value.title);
    setDescription(value.description);
    setKind(value.kind);
    setPriority(value.priority);
    setPlannedDate(value.plannedDate ?? "");
    setDueDate(toLocalDateTime(value.dueDate));
    setEstimatedMinutes(
      value.estimatedMinutes === null ? "" : String(value.estimatedMinutes),
    );
    setProjectId(value.projectId ?? "");
    setParentTaskId(value.parentTaskId ?? "");
    setCompletionCriteria(value.completionCriteria);
    setReviewPolicy(value.reviewPolicy);
    setTagIds(value.tags.map((tag) => tag.id));
    setDraftVersion(value.version);
    hydratedTaskId.current = value.id;
    setConfirmingDelete(false);
    setValidationError(null);
  };

  const taskDraftDirty = Boolean(
    task &&
    hydratedTaskId.current === task.id &&
    (title !== task.title ||
      description !== task.description ||
      kind !== task.kind ||
      priority !== task.priority ||
      plannedDate !== (task.plannedDate ?? "") ||
      dueDate !== toLocalDateTime(task.dueDate) ||
      estimatedMinutes !==
        (task.estimatedMinutes === null ? "" : String(task.estimatedMinutes)) ||
      projectId !== (task.projectId ?? "") ||
      parentTaskId !== (task.parentTaskId ?? "") ||
      completionCriteria !== task.completionCriteria ||
      reviewPolicy !== task.reviewPolicy ||
      tagIds.join("\u0000") !== task.tags.map((tag) => tag.id).join("\u0000")),
  );

  useEffect(() => {
    setVersionConflict(false);
    setConflictLatestTask(null);
    setConflictRefreshing(false);
    skipNextHydrate.current = false;
    hydratedTaskId.current = null;
    setAssignmentBusy(false);
    setWorkflowBusy(false);
    setOutputBusy(false);
    setSubmissionHistoryState({ loading: true, error: false, total: null });
  }, [taskId]);

  useEffect(() => {
    if (!task) return;
    if (versionConflict || skipNextHydrate.current) {
      skipNextHydrate.current = false;
      return;
    }
    if (taskDraftDirty) return;
    hydrate(task);
  }, [task, versionConflict, taskDraftDirty]);

  const errorMessage = useMemo(
    () =>
      validationError ??
      mutationErrorMessage(updateMutation.error) ??
      mutationErrorMessage(deleteMutation.error),
    [deleteMutation.error, updateMutation.error, validationError],
  );

  const close = () => {
    if (busy) return;
    updateMutation.reset();
    deleteMutation.reset();
    setConfirmingDelete(false);
    setVersionConflict(false);
    setConflictLatestTask(null);
    setConflictRefreshing(false);
    setValidationError(null);
    setTaskId(null);
    if (routedTaskId) navigate("/tasks", { replace: true });
  };

  const refreshTask = async (): Promise<Task | null> => {
    const result = await query.refetch();
    return result.isSuccess && result.data ? result.data : null;
  };

  const refreshFactConflict = async (expectedVersion: number) => {
    setConflictRefreshing(true);
    setConflictLatestTask(null);
    const latest = await refreshTask();
    setConflictLatestTask(
      latest && latest.version > expectedVersion ? latest : null,
    );
    setConflictRefreshing(false);
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (
      !taskId ||
      assignmentBusy ||
      workflowBusy ||
      outputBusy ||
      versionConflict
    )
      return;

    const cleanTitle = title.trim();
    if (cleanTitle.length < 2) {
      setValidationError("任务名称至少需要 2 个字符。");
      return;
    }
    const parsedMinutes =
      estimatedMinutes === "" ? null : Number(estimatedMinutes);
    if (
      parsedMinutes !== null &&
      (!Number.isInteger(parsedMinutes) || parsedMinutes < 0)
    ) {
      setValidationError("预计时长必须是大于或等于 0 的整数。");
      return;
    }
    const normalizedDueDate = toRfc3339(dueDate);
    if (dueDate && !normalizedDueDate) {
      setValidationError("截止时间格式无效，请重新选择。");
      return;
    }

    setValidationError(null);
    updateMutation.mutate(
      {
        id: taskId,
        input: {
          title: cleanTitle,
          description,
          kind,
          priority,
          projectId: projectId || null,
          parentTaskId: parentTaskId || null,
          completionCriteria,
          reviewPolicy,
          tagIds,
          plannedDate: plannedDate || null,
          dueDate: normalizedDueDate,
          estimatedMinutes: parsedMinutes,
          expectedVersion: draftVersion,
        },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            setVersionConflict(true);
            void refreshFactConflict(draftVersion);
          }
        },
        onSuccess: close,
      },
    );
  };

  const confirmDelete = () => {
    if (
      !taskId ||
      assignmentBusy ||
      workflowBusy ||
      outputBusy ||
      versionConflict
    )
      return;
    deleteMutation.mutate(
      { id: taskId, expectedVersion: draftVersion },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            setVersionConflict(true);
            setConfirmingDelete(false);
            void refreshFactConflict(draftVersion);
          }
        },
        onSuccess: close,
      },
    );
  };

  return (
    <Modal
      footer={
        task ? (
          confirmingDelete ? (
            <>
              <span className="task-delete-warning">
                <AlertTriangle size={14} />
                删除后无法恢复，确定继续？
              </span>
              <button
                className="button button-secondary"
                disabled={busy}
                onClick={() => setConfirmingDelete(false)}
                type="button"
              >
                返回
              </button>
              <button
                className="button button-danger"
                disabled={busy}
                onClick={confirmDelete}
                type="button"
              >
                {deleteMutation.isPending ? "正在删除…" : "确认删除"}
              </button>
            </>
          ) : (
            <>
              <button
                className="button button-quiet task-detail-delete"
                disabled={busy || versionConflict}
                onClick={() => setConfirmingDelete(true)}
                type="button"
              >
                <Trash2 size={14} />
                删除任务
              </button>
              <button
                className="button button-secondary"
                disabled={busy}
                onClick={close}
                type="button"
              >
                取消
              </button>
              <button
                className="button button-primary"
                disabled={title.trim().length < 2 || busy || versionConflict}
                form="task-detail-form"
                type="submit"
              >
                {updateMutation.isPending ? "正在保存…" : "保存修改"}
              </button>
            </>
          )
        ) : (
          <button
            className="button button-secondary"
            onClick={close}
            type="button"
          >
            关闭
          </button>
        )
      }
      onClose={close}
      open={Boolean(taskId)}
      title="任务详情"
      width="700px"
    >
      {query.isPending ? <SkeletonRows count={5} /> : null}

      {query.isError ? (
        <ErrorState
          compact
          message="无法读取任务详情，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}

      {task ? (
        <form id="task-detail-form" onSubmit={submit}>
          {versionConflict ? (
            <div className="task-conflict" role="alert">
              <AlertTriangle size={15} />
              <div>
                <strong>此任务已在其他窗口发生变化</strong>
                <span>
                  {conflictRefreshing
                    ? "正在读取最新版，你的草稿仍保留。"
                    : conflictLatestTask
                      ? `已读取最新版 v${conflictLatestTask.version}，你的草稿仍保留。请决定如何继续。`
                      : "尚未确认最新版，你的草稿仍保留；当前不会重试写入。"}
                </span>
              </div>
              {!conflictRefreshing && !conflictLatestTask ? (
                <button
                  className="button button-secondary"
                  onClick={() => void refreshFactConflict(draftVersion)}
                  type="button"
                >
                  重试读取
                </button>
              ) : null}
              <button
                className="button button-secondary"
                disabled={!conflictLatestTask || conflictRefreshing}
                onClick={() => {
                  if (!conflictLatestTask) return;
                  hydrate(conflictLatestTask);
                  setVersionConflict(false);
                  setConflictLatestTask(null);
                  updateMutation.reset();
                  deleteMutation.reset();
                }}
                type="button"
              >
                载入最新
              </button>
              <button
                className="button button-primary"
                disabled={!conflictLatestTask || conflictRefreshing}
                onClick={() => {
                  if (!conflictLatestTask) return;
                  skipNextHydrate.current = true;
                  setDraftVersion(conflictLatestTask.version);
                  setVersionConflict(false);
                  setConflictLatestTask(null);
                  updateMutation.reset();
                  deleteMutation.reset();
                }}
                type="button"
              >
                保留草稿重试
              </button>
            </div>
          ) : null}
          <div className="task-detail-summary">
            <span className={`task-status-pill task-status-${task.status}`}>
              {statusLabels[task.status]}
            </span>
            <span>
              <Clock3 size={12} />
              已记录 {task.actualMinutes} 分钟
            </span>
            <span>更新于 {formatUpdatedAt(task.updatedAt)}</span>
            {task.subtaskTotal > 0 ? (
              <span>
                子任务完成 {task.subtaskCompleted}/
                {Math.max(0, task.subtaskTotal - (task.subtaskCancelled ?? 0))}
                {(task.subtaskCancelled ?? 0) > 0
                  ? ` · 已取消 ${task.subtaskCancelled}`
                  : ""}
              </span>
            ) : null}
          </div>

          <TaskLifecycleSection
            disabled={
              taskWriteBusy ||
              assignmentBusy ||
              outputBusy ||
              confirmingDelete ||
              versionConflict
            }
            hasUnsavedFacts={taskDraftDirty}
            onBusyChange={setWorkflowBusy}
            onRefreshTask={refreshTask}
            onTaskUpdated={(updatedTask) => {
              setDraftVersion((current) =>
                Math.max(current, updatedTask.version),
              );
            }}
            task={task}
          />

          <TaskAssignmentsSection
            disabled={
              taskWriteBusy ||
              workflowBusy ||
              outputBusy ||
              confirmingDelete ||
              versionConflict
            }
            onBusyChange={setAssignmentBusy}
            onTaskUpdated={(updatedTask) => {
              setDraftVersion((current) =>
                Math.max(current, updatedTask.version),
              );
            }}
            task={task}
          />

          <TaskOutputsSection
            disabled={
              taskWriteBusy ||
              assignmentBusy ||
              workflowBusy ||
              confirmingDelete ||
              versionConflict
            }
            hasUnsavedFacts={taskDraftDirty}
            onBusyChange={setOutputBusy}
            onHistoryStateChange={setSubmissionHistoryState}
            onRefreshTask={refreshTask}
            onTaskUpdated={(updatedTask) => {
              setDraftVersion((current) =>
                Math.max(current, updatedTask.version),
              );
            }}
            task={task}
          />

          <label className="form-field">
            <span>任务名称</span>
            <input
              autoFocus
              maxLength={200}
              onChange={(event) => setTitle(event.target.value)}
              value={title}
            />
          </label>

          <label className="form-field">
            <span>完成标准</span>
            <textarea
              maxLength={10_000}
              onChange={(event) => setCompletionCriteria(event.target.value)}
              placeholder="写清楚完成后应达到的可验证结果…"
              rows={3}
              value={completionCriteria}
            />
          </label>

          <label className="form-field">
            <span>验收策略</span>
            <select
              aria-label="验收策略"
              disabled={
                busy ||
                task.status !== "todo" ||
                task.currentSubmissionId !== null ||
                submissionHistoryState.loading ||
                submissionHistoryState.error ||
                submissionHistoryState.total !== 0
              }
              onChange={(event) =>
                setReviewPolicy(event.target.value as TaskReviewPolicy)
              }
              value={reviewPolicy}
            >
              <option value="none">无需验收 · 可直接完成</option>
              <option value="manual">人工验收 · 提交产出后完成</option>
            </select>
            <small>
              {submissionHistoryState.loading
                ? "正在确认是否已有提交历史…"
                : submissionHistoryState.error
                  ? "提交历史暂不可用，验收策略已锁定。"
                  : task.status !== "todo" ||
                      task.currentSubmissionId !== null ||
                      submissionHistoryState.total !== 0
                    ? "只有待办且从未提交产出的任务可以修改验收策略。"
                    : reviewPolicy === "manual"
                      ? "需先设置负责人和所有者审核人，再提交产出。"
                      : "适合无需单独检查产出的快速任务。"}
            </small>
          </label>

          <label className="form-field">
            <span>描述</span>
            <textarea
              maxLength={10_000}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="补充任务背景、要求或完成条件…"
              rows={5}
              value={description}
            />
          </label>

          <div className="form-grid">
            <label className="form-field">
              <span>计划日期</span>
              <input
                onChange={(event) => setPlannedDate(event.target.value)}
                type="date"
                value={plannedDate}
              />
            </label>
            <label className="form-field">
              <span>截止时间</span>
              <input
                onChange={(event) => setDueDate(event.target.value)}
                type="datetime-local"
                value={dueDate}
              />
            </label>
          </div>

          <div className="form-grid">
            <div className="form-field">
              <span>项目</span>
              <ProjectSelect
                ariaLabel="项目"
                emptyLabel="未归项目"
                onChange={setProjectId}
                selectedName={
                  projectId === task.projectId ? task.projectName : undefined
                }
                value={projectId}
                variant="form"
              />
            </div>
            <label className="form-field">
              <span>父任务</span>
              <select
                disabled={tasksQuery.isPending || tasksQuery.isError}
                onChange={(event) => setParentTaskId(event.target.value)}
                value={parentTaskId}
              >
                <option value="">
                  {tasksQuery.isPending
                    ? "正在读取任务…"
                    : tasksQuery.isError
                      ? "任务暂不可用"
                      : "无父任务"}
                </option>
                {task.parentTaskId &&
                !(tasksQuery.data ?? []).some(
                  (candidate) => candidate.id === task.parentTaskId,
                ) ? (
                  <option value={task.parentTaskId}>
                    {task.parentTaskTitle ??
                      `父任务 ${task.parentTaskId.slice(0, 8)}…`}
                  </option>
                ) : null}
                {(tasksQuery.data ?? [])
                  .filter((candidate) => candidate.id !== task.id)
                  .map((candidate) => (
                    <option key={candidate.id} value={candidate.id}>
                      {candidate.title}
                    </option>
                  ))}
              </select>
            </label>
          </div>

          <div className="form-grid">
            <label className="form-field">
              <span>任务类型</span>
              <select
                onChange={(event) => setKind(event.target.value as TaskKind)}
                value={kind}
              >
                {taskKinds.map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="form-field">
              <span>预计时长</span>
              <div className="field-with-suffix">
                <input
                  aria-label="预计时长"
                  min="0"
                  onChange={(event) => setEstimatedMinutes(event.target.value)}
                  step="1"
                  type="number"
                  value={estimatedMinutes}
                />
                <span>分钟</span>
              </div>
            </label>
          </div>

          <div className="form-field">
            <span>标签</span>
            <TaskTagPicker
              disabled={busy}
              enabled={Boolean(taskId)}
              onChange={setTagIds}
              selectedIds={tagIds}
            />
          </div>

          <fieldset className="form-field form-field-last">
            <legend>优先级</legend>
            <div className="priority-segment">
              {priorities.map((item) => (
                <button
                  className={
                    priority === item.value
                      ? "priority-option priority-option-active"
                      : "priority-option"
                  }
                  key={item.value}
                  onClick={() => setPriority(item.value)}
                  type="button"
                >
                  <span
                    className={`priority-dot priority-${item.value.toLowerCase()}`}
                  />
                  {item.label}
                </button>
              ))}
            </div>
          </fieldset>

          <TaskFocusHistorySection taskId={task.id} />
          <TaskEventsSection taskId={task.id} />

          {errorMessage ? (
            <div className="form-error" role="alert">
              {errorMessage}
            </div>
          ) : null}
          <p className="form-note">
            普通编辑不会改变任务状态；状态变化只能使用上方受控命令。
          </p>
        </form>
      ) : null}
    </Modal>
  );
}
