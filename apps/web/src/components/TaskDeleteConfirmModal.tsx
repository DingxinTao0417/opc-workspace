import { AlertTriangle, Trash2 } from "lucide-react";
import { useEffect } from "react";
import { ApiError } from "../api/client";
import { useDeleteTask } from "../api/hooks";
import type { Task } from "../types/models";
import { Modal } from "./Modal";

function deleteErrorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    const message =
      error.code === "VERSION_CONFLICT"
        ? "任务已被更新。列表已刷新，请关闭后按最新内容重试。"
        : error.code === "TASK_HAS_ACTIVE_INBOX_RELATIONS"
          ? "该任务仍关联活动收件箱条目。请先在收件箱解除关联，再删除任务。"
          : error.code === "TASK_HAS_ACTIVE_INBOX_SOURCES"
            ? "该任务仍有产出作为活动收件箱事项的来源。请先解决或忽略这些跟进项，再删除任务。"
            : error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "删除任务失败，请重试。";
}

export function TaskDeleteConfirmModal({
  task,
  onClose,
}: {
  task: Task | null;
  onClose: () => void;
}) {
  const mutation = useDeleteTask();

  useEffect(() => {
    mutation.reset();
  }, [task?.id]);

  const close = () => {
    if (mutation.isPending) return;
    mutation.reset();
    onClose();
  };
  const confirm = () => {
    if (!task || mutation.isPending) return;
    mutation.mutate(
      { id: task.id, expectedVersion: task.version },
      { onSuccess: onClose },
    );
  };
  const error = deleteErrorText(mutation.error);

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
            className="button button-danger"
            disabled={!task || mutation.isPending}
            onClick={confirm}
            type="button"
          >
            <Trash2 size={14} />
            {mutation.isPending ? "正在删除…" : "确认删除"}
          </button>
        </>
      }
      onClose={close}
      open={Boolean(task)}
      title="删除任务"
      width="440px"
    >
      <div className="task-delete-confirm-copy">
        <AlertTriangle aria-hidden="true" size={18} />
        <div>
          <strong>确定删除“{task?.title}”吗？</strong>
          <p>
            删除后无法恢复；存在活动收件箱关系或产出来源跟进项时，系统会拒绝操作。
          </p>
        </div>
      </div>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </Modal>
  );
}
