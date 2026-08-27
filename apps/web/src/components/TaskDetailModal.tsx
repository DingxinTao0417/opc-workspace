import { AlertTriangle, Clock3, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useDeleteTask, useTaskQuery, useUpdateTask } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { TaskPriority, TaskStatus } from "../types/models";
import { ErrorState, SkeletonRows } from "./feedback";
import { Modal } from "./Modal";

const priorities: { value: TaskPriority; label: string }[] = [
  { value: "P0", label: "紧急" },
  { value: "P1", label: "高" },
  { value: "P2", label: "中" },
  { value: "P3", label: "低" },
];

const statusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  done: "已完成",
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
    const suffix = error.requestId ? ` · 请求 ${error.requestId}` : "";
    return `${error.message}${suffix}`;
  }
  return "任务操作失败，请重试。";
}

export function TaskDetailModal() {
  const taskId = useUiStore((state) => state.taskDetailId);
  const setTaskId = useUiStore((state) => state.setTaskDetailId);
  const query = useTaskQuery(taskId);
  const updateMutation = useUpdateTask();
  const deleteMutation = useDeleteTask();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<TaskPriority>("P2");
  const [plannedDate, setPlannedDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [estimatedMinutes, setEstimatedMinutes] = useState("");
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);
  const task = query.data;
  const busy = updateMutation.isPending || deleteMutation.isPending;

  useEffect(() => {
    if (!task) return;
    setTitle(task.title);
    setDescription(task.description);
    setPriority(task.priority);
    setPlannedDate(task.plannedDate ?? "");
    setDueDate(toLocalDateTime(task.dueDate));
    setEstimatedMinutes(
      task.estimatedMinutes === null ? "" : String(task.estimatedMinutes),
    );
    setConfirmingDelete(false);
    setValidationError(null);
  }, [task]);

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
    setValidationError(null);
    setTaskId(null);
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!taskId) return;

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
          priority,
          plannedDate: plannedDate || null,
          dueDate: normalizedDueDate,
          estimatedMinutes: parsedMinutes,
        },
      },
      { onSuccess: close },
    );
  };

  const confirmDelete = () => {
    if (!taskId) return;
    deleteMutation.mutate(taskId, { onSuccess: close });
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
                disabled={busy}
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
                disabled={title.trim().length < 2 || busy}
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
      width="620px"
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
          <div className="task-detail-summary">
            <span className={`task-status-pill task-status-${task.status}`}>
              {statusLabels[task.status]}
            </span>
            <span>
              <Clock3 size={12} />
              已记录 {task.actualMinutes} 分钟
            </span>
            <span>更新于 {formatUpdatedAt(task.updatedAt)}</span>
          </div>

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
            <label className="form-field">
              <span>项目</span>
              <select disabled value="">
                <option value="">
                  {task.projectName ??
                    (task.projectId
                      ? `已关联项目 ${task.projectId.slice(0, 8)}… · 项目管理完成后可编辑`
                      : "未归项目 · 项目管理完成后可编辑")}
                </option>
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

          {errorMessage ? (
            <div className="form-error" role="alert">
              {errorMessage}
            </div>
          ) : null}
          <p className="form-note">
            状态变更使用任务列表中的完成按钮；普通编辑不会绕过任务状态流转。
          </p>
        </form>
      ) : null}
    </Modal>
  );
}
