import {
  AlertTriangle,
  Ban,
  Check,
  CirclePlay,
  LoaderCircle,
  RotateCcw,
  ShieldAlert,
  Unlock,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import { useTaskLifecycleCommand } from "../api/hooks";
import type {
  Task,
  TaskLifecycleAction,
  TaskLifecycleCommandInput,
  TaskStatus,
} from "../types/models";

interface LifecycleEditor {
  action: TaskLifecycleAction;
  reason: string;
  expectedVersion: number;
}

interface LifecycleConflict {
  latestVersion: number | null;
  refreshing: boolean;
}

interface TaskLifecycleSectionProps {
  task: Task;
  disabled?: boolean;
  hasUnsavedFacts?: boolean;
  onBusyChange?: (busy: boolean) => void;
  onRefreshTask: () => Promise<Task | null>;
  onTaskUpdated?: (task: Task) => void;
}

const statusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

const actionLabels: Record<TaskLifecycleAction, string> = {
  start: "开始执行",
  block: "标记阻塞",
  unblock: "解除阻塞",
  complete: "完成任务",
  cancel: "取消任务",
  reopen: "重新打开",
};

const actionIcons = {
  start: CirclePlay,
  block: ShieldAlert,
  unblock: Unlock,
  complete: Check,
  cancel: Ban,
  reopen: RotateCcw,
} satisfies Record<TaskLifecycleAction, typeof CirclePlay>;

const errorMessages: Record<string, string> = {
  VERSION_REQUIRED: "任务版本缺失，请刷新详情后重试。",
  INVALID_VERSION: "任务版本无效，请刷新详情后重试。",
  TASK_TRANSITION_NOT_ALLOWED: "当前任务状态不允许执行此操作。",
  TASK_ASSIGNEE_REQUIRED: "开始任务前需要先设置负责人。",
  TASK_REVIEW_REQUIRED: "此任务必须通过受控验收流程，不能直接完成。",
  TASK_BLOCK_STATE_INVALID: "阻塞前状态缺失，无法安全解除阻塞。",
  LIFECYCLE_COMMAND_REQUIRED: "任务状态只能通过受控命令改变。",
  TASK_NOT_FOUND: "任务已不存在，请关闭详情。",
  IDEMPOTENCY_CONFLICT: "本次操作与已有重试记录不一致，请刷新后重试。",
};

function availableActions(task: Task): TaskLifecycleAction[] {
  switch (task.status) {
    case "todo":
      return task.reviewPolicy === "none"
        ? ["start", "block", "complete", "cancel"]
        : ["start", "block", "cancel"];
    case "in_progress":
      return task.reviewPolicy === "none"
        ? ["block", "complete", "cancel"]
        : ["block", "cancel"];
    case "waiting_review":
      return ["block", "cancel"];
    case "blocked":
      return ["unblock", "cancel"];
    case "done":
    case "cancelled":
      return ["reopen"];
  }
}

function lifecycleErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") return null;
    const message = errorMessages[error.code] ?? error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "任务状态操作失败，请重试。";
}

function actionDescription(action: TaskLifecycleAction, task: Task): string {
  switch (action) {
    case "start":
      return "任务将进入进行中。需要已有活动负责人。";
    case "block":
      return "记录阻塞原因；解除后会恢复到阻塞前状态。";
    case "unblock":
      return `任务将恢复为${statusLabels[task.blockedFromStatus ?? "todo"]}。`;
    case "complete":
      return "任务将完成，并结束当前负责人和审核人分派。";
    case "cancel":
      return "取消不等同于完成；当前责任分派会结束，历史记录会保留。";
    case "reopen":
      return "任务将回到待办；此前结束的责任分派不会恢复。";
  }
}

export function TaskLifecycleSection({
  task,
  disabled = false,
  hasUnsavedFacts = false,
  onBusyChange,
  onRefreshTask,
  onTaskUpdated,
}: TaskLifecycleSectionProps) {
  const mutation = useTaskLifecycleCommand();
  const [editor, setEditor] = useState<LifecycleEditor | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [conflict, setConflict] = useState<LifecycleConflict | null>(null);
  const actions = availableActions(task);
  const errorMessage = useMemo(
    () => validationError ?? lifecycleErrorMessage(mutation.error),
    [mutation.error, validationError],
  );

  useEffect(() => {
    onBusyChange?.(mutation.isPending);
    return () => onBusyChange?.(false);
  }, [mutation.isPending, onBusyChange]);

  useEffect(() => {
    setEditor(null);
    setConflict(null);
    setValidationError(null);
    mutation.reset();
  }, [task.id]);

  const beginAction = (action: TaskLifecycleAction) => {
    mutation.reset();
    setValidationError(null);
    setConflict(null);
    setEditor({ action, reason: "", expectedVersion: task.version });
  };

  const refreshAfterConflict = async (expectedVersion: number) => {
    setConflict({ latestVersion: null, refreshing: true });
    const latest = await onRefreshTask();
    setConflict({
      latestVersion:
        latest && latest.version > expectedVersion ? latest.version : null,
      refreshing: false,
    });
  };

  const submit = () => {
    if (!editor || disabled || hasUnsavedFacts || mutation.isPending) return;
    const reason = editor.reason.trim();
    if ((editor.action === "block" || editor.action === "cancel") && !reason) {
      setValidationError("请填写原因。");
      return;
    }
    setValidationError(null);
    const input: TaskLifecycleCommandInput =
      editor.action === "block" || editor.action === "cancel"
        ? {
            action: editor.action,
            reason,
            expectedVersion: editor.expectedVersion,
          }
        : {
            action: editor.action,
            expectedVersion: editor.expectedVersion,
          };
    mutation.mutate(
      { id: task.id, input },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            void refreshAfterConflict(editor.expectedVersion);
          }
        },
        onSuccess: (result) => {
          onTaskUpdated?.(result.task);
          setEditor(null);
          setConflict(null);
          setValidationError(null);
        },
      },
    );
  };

  return (
    <section
      aria-labelledby="task-lifecycle-heading"
      className="task-lifecycle"
    >
      <div className="task-lifecycle-heading">
        <div>
          <h3 id="task-lifecycle-heading">任务状态</h3>
          <p>状态只能通过受控命令改变，并记录到活动时间线。</p>
        </div>
        <div className="task-lifecycle-badges">
          <span className={`task-status-pill task-status-${task.status}`}>
            {statusLabels[task.status]}
          </span>
          <span className="task-review-policy">
            {task.reviewPolicy === "none" ? "无需验收" : "受控验收"}
          </span>
        </div>
      </div>

      {task.status === "blocked" && task.blockedReason ? (
        <div className="task-blocked-reason">
          <AlertTriangle size={13} />
          <span>阻塞原因：{task.blockedReason}</span>
        </div>
      ) : null}

      {hasUnsavedFacts ? (
        <div className="task-lifecycle-notice">
          当前有未保存的任务信息；请先保存，再执行状态操作。
        </div>
      ) : null}

      <div aria-label="可用任务状态操作" className="task-lifecycle-actions">
        {actions.map((action) => {
          const Icon = actionIcons[action];
          return (
            <button
              className={
                action === "cancel"
                  ? "button button-quiet task-lifecycle-cancel"
                  : action === "complete"
                    ? "button button-primary"
                    : "button button-secondary"
              }
              disabled={disabled || hasUnsavedFacts || mutation.isPending}
              key={action}
              onClick={() => beginAction(action)}
              type="button"
            >
              <Icon size={13} />
              {actionLabels[action]}
            </button>
          );
        })}
      </div>

      {editor ? (
        <div className="task-lifecycle-editor">
          <div className="task-lifecycle-editor-heading">
            <div>
              <strong>{actionLabels[editor.action]}</strong>
              <span>{actionDescription(editor.action, task)}</span>
            </div>
            <button
              aria-label="关闭状态操作"
              className="button button-quiet"
              disabled={mutation.isPending}
              onClick={() => {
                setEditor(null);
                setConflict(null);
                setValidationError(null);
                mutation.reset();
              }}
              type="button"
            >
              <X size={13} />
            </button>
          </div>

          {editor.action === "block" || editor.action === "cancel" ? (
            <label className="form-field task-lifecycle-reason">
              <span>{editor.action === "block" ? "阻塞原因" : "取消原因"}</span>
              <textarea
                aria-label={editor.action === "block" ? "阻塞原因" : "取消原因"}
                maxLength={1_000}
                onChange={(event) =>
                  setEditor((current) =>
                    current ? { ...current, reason: event.target.value } : null,
                  )
                }
                rows={3}
                value={editor.reason}
              />
              <small>{editor.reason.length}/1000</small>
            </label>
          ) : null}

          {conflict ? (
            <div className="task-conflict task-lifecycle-conflict" role="alert">
              {conflict.refreshing ? (
                <LoaderCircle className="spin" size={15} />
              ) : (
                <AlertTriangle size={15} />
              )}
              <div>
                <strong>任务已在其他窗口发生变化</strong>
                <span>
                  {conflict.refreshing
                    ? "正在读取最新任务；你的操作和原因仍保留。"
                    : conflict.latestVersion
                      ? `已读取最新版 v${conflict.latestVersion}；请确认后再次提交。`
                      : "尚未确认最新版；当前不会重放此操作。"}
                </span>
              </div>
              {!conflict.refreshing && !conflict.latestVersion ? (
                <button
                  className="button button-secondary"
                  onClick={() =>
                    void refreshAfterConflict(editor.expectedVersion)
                  }
                  type="button"
                >
                  重试读取
                </button>
              ) : null}
              <button
                className="button button-primary"
                disabled={!conflict.latestVersion || conflict.refreshing}
                onClick={() => {
                  if (!conflict.latestVersion) return;
                  setEditor((current) =>
                    current
                      ? {
                          ...current,
                          expectedVersion: conflict.latestVersion!,
                        }
                      : null,
                  );
                  setConflict(null);
                  mutation.reset();
                }}
                type="button"
              >
                保留操作
              </button>
            </div>
          ) : null}

          {errorMessage ? (
            <div className="task-lifecycle-error" role="alert">
              {errorMessage}
            </div>
          ) : null}

          <div className="task-lifecycle-editor-actions">
            <button
              className="button button-secondary"
              disabled={mutation.isPending}
              onClick={() => setEditor(null)}
              type="button"
            >
              返回
            </button>
            <button
              className={
                editor.action === "cancel"
                  ? "button button-danger"
                  : "button button-primary"
              }
              disabled={
                disabled ||
                hasUnsavedFacts ||
                mutation.isPending ||
                Boolean(conflict)
              }
              onClick={submit}
              type="button"
            >
              {mutation.isPending
                ? "正在提交…"
                : `确认${actionLabels[editor.action]}`}
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}
