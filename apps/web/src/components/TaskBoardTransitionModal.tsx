import { AlertTriangle, ArrowRight, LoaderCircle } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { ApiError } from "../api/client";
import { useTaskLifecycleCommand } from "../api/hooks";
import type {
  Task,
  TaskLifecycleAction,
  TaskLifecycleCommandInput,
  TaskStatus,
} from "../types/models";
import { Modal } from "./Modal";

export interface TaskBoardTransition {
  task: Task;
  targetStatus: TaskStatus;
  action: TaskLifecycleAction;
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

export function resolveBoardTransition(
  task: Task,
  targetStatus: TaskStatus,
): TaskBoardTransition | null {
  let action: TaskLifecycleAction | null = null;
  if (task.status === "todo") {
    if (targetStatus === "in_progress") action = "start";
    else if (targetStatus === "blocked") action = "block";
    else if (targetStatus === "done" && task.reviewPolicy === "none")
      action = "complete";
    else if (targetStatus === "cancelled") action = "cancel";
  } else if (task.status === "in_progress") {
    if (targetStatus === "blocked") action = "block";
    else if (targetStatus === "done" && task.reviewPolicy === "none")
      action = "complete";
    else if (targetStatus === "cancelled") action = "cancel";
  } else if (task.status === "waiting_review") {
    if (targetStatus === "blocked") action = "block";
    else if (targetStatus === "cancelled") action = "cancel";
  } else if (task.status === "blocked") {
    if (targetStatus === task.blockedFromStatus) action = "unblock";
    else if (targetStatus === "cancelled") action = "cancel";
  } else if (
    (task.status === "done" || task.status === "cancelled") &&
    targetStatus === "todo"
  ) {
    action = "reopen";
  }
  return action ? { task, targetStatus, action } : null;
}

function transitionError(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    const message =
      error.code === "VERSION_CONFLICT"
        ? "任务已被其他操作更新，列表已刷新，请重新拖动。"
        : error.code === "TASK_ASSIGNEE_REQUIRED"
          ? "开始任务前需要先在详情中设置负责人。"
          : error.code === "TASK_REVIEW_REQUIRED"
            ? "该任务需要人工验收，不能直接拖到已完成。"
            : error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "任务状态变更失败，请重试。";
}

export function TaskBoardTransitionModal({
  transition,
  onClose,
  onConflict,
}: {
  transition: TaskBoardTransition | null;
  onClose: () => void;
  onConflict: () => void;
}) {
  const mutation = useTaskLifecycleCommand();
  const reasonId = useId();
  const [reason, setReason] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    setReason("");
    setValidationError(null);
    mutation.reset();
  }, [transition?.task.id, transition?.targetStatus]);

  const close = () => {
    if (mutation.isPending) return;
    mutation.reset();
    onClose();
  };
  const confirm = () => {
    if (!transition || mutation.isPending) return;
    const normalizedReason = reason.trim();
    if (
      (transition.action === "block" || transition.action === "cancel") &&
      !normalizedReason
    ) {
      setValidationError("请填写原因。该原因会写入任务活动记录。");
      return;
    }
    setValidationError(null);
    const input: TaskLifecycleCommandInput =
      transition.action === "block" || transition.action === "cancel"
        ? {
            action: transition.action,
            reason: normalizedReason,
            expectedVersion: transition.task.version,
          }
        : {
            action: transition.action,
            expectedVersion: transition.task.version,
          };
    mutation.mutate(
      { id: transition.task.id, input },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
            onConflict();
        },
        onSuccess: onClose,
      },
    );
  };
  const error = validationError ?? transitionError(mutation.error);
  const needsReason =
    transition?.action === "block" || transition?.action === "cancel";

  return (
    <Modal
      dismissible={!mutation.isPending}
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={mutation.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={!transition || mutation.isPending}
            onClick={confirm}
            type="button"
          >
            {mutation.isPending ? (
              <LoaderCircle className="spin" size={14} />
            ) : null}
            {mutation.isPending ? "正在变更…" : "确认变更"}
          </button>
        </>
      }
      onClose={close}
      open={Boolean(transition)}
      title="确认任务状态变更"
      width="460px"
    >
      <div className="task-board-transition-copy">
        <AlertTriangle aria-hidden="true" size={18} />
        <div>
          <strong>{transition?.task.title}</strong>
          <p>
            {transition ? statusLabels[transition.task.status] : ""}
            <ArrowRight aria-hidden="true" size={13} />
            {transition ? statusLabels[transition.targetStatus] : ""}
          </p>
          <small>
            将执行“{transition ? actionLabels[transition.action] : ""}”命令；
            状态仅在服务端校验成功后更新。
          </small>
        </div>
      </div>
      {needsReason ? (
        <div className="form-field task-board-transition-reason">
          <label htmlFor={reasonId}>原因</label>
          <textarea
            autoFocus
            id={reasonId}
            maxLength={1000}
            onChange={(event) => setReason(event.target.value)}
            placeholder={
              transition?.action === "block"
                ? "说明当前阻塞原因"
                : "说明取消原因"
            }
            rows={3}
            value={reason}
          />
          <small>{reason.length}/1000</small>
        </div>
      ) : null}
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </Modal>
  );
}
