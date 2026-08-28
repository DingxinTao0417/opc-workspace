import { useEffect, useMemo, useState, type FormEvent } from "react";
import {
  useCreateTask,
  useProjectOptionsQuery,
  useTaskOptionsQuery,
} from "../api/hooks";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { TaskKind, TaskPriority } from "../types/models";
import { Modal } from "./Modal";
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

function toRfc3339(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export function NewTaskModal() {
  const open = useUiStore((state) => state.newTaskOpen);
  const preselectedProjectId = useUiStore((state) => state.newTaskProjectId);
  const setOpen = useUiStore((state) => state.setNewTaskOpen);
  const mutation = useCreateTask();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<TaskKind>("work");
  const [plannedDate, setPlannedDate] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [estimatedMinutes, setEstimatedMinutes] = useState("50");
  const [priority, setPriority] = useState<TaskPriority>("P2");
  const [projectId, setProjectId] = useState("");
  const [parentTaskId, setParentTaskId] = useState("");
  const [completionCriteria, setCompletionCriteria] = useState("");
  const [tagIds, setTagIds] = useState<string[]>([]);
  const [validationError, setValidationError] = useState<string | null>(null);
  const projectsQuery = useProjectOptionsQuery(open);
  const tasksQuery = useTaskOptionsQuery(open);

  useEffect(() => {
    if (open) setProjectId(preselectedProjectId ?? "");
  }, [open, preselectedProjectId]);

  const errorMessage = useMemo(() => {
    if (validationError) return validationError;
    if (!mutation.error) return null;
    if (mutation.error instanceof ApiError) {
      const suffix = mutation.error.requestId
        ? ` · 请求 ${mutation.error.requestId}`
        : "";
      return `${mutation.error.message}${suffix}`;
    }
    return "创建任务失败";
  }, [mutation.error, validationError]);

  const close = () => {
    if (mutation.isPending) return;
    setOpen(false);
    mutation.reset();
    setValidationError(null);
  };

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanTitle = title.trim();
    if (cleanTitle.length < 2 || cleanTitle.length > 200) {
      setValidationError("任务名称需要 2–200 个字符。");
      return;
    }
    const parsedMinutes =
      estimatedMinutes.trim() === "" ? null : Number(estimatedMinutes);
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
    mutation.mutate(
      {
        title: cleanTitle,
        description: description.trim(),
        kind,
        status: "todo",
        priority,
        projectId: projectId || null,
        parentTaskId: parentTaskId || null,
        completionCriteria: completionCriteria.trim(),
        tagIds,
        dueDate: normalizedDueDate,
        plannedDate: plannedDate || null,
        estimatedMinutes: parsedMinutes,
      },
      {
        onSuccess: () => {
          setTitle("");
          setDescription("");
          setKind("work");
          setPlannedDate("");
          setDueDate("");
          setEstimatedMinutes("50");
          setPriority("P2");
          setProjectId("");
          setParentTaskId("");
          setCompletionCriteria("");
          setTagIds([]);
          setValidationError(null);
          setOpen(false);
        },
      },
    );
  };

  return (
    <Modal
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
            disabled={title.trim().length < 2 || mutation.isPending}
            form="new-task-form"
            type="submit"
          >
            {mutation.isPending ? "正在保存…" : "创建任务"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title="新建任务"
    >
      <form id="new-task-form" onSubmit={onSubmit}>
        <label className="form-field">
          <span>任务名称</span>
          <input
            autoFocus
            onChange={(event) => setTitle(event.target.value)}
            placeholder="输入任务名称…"
            value={title}
          />
        </label>
        <div className="form-grid">
          <label className="form-field">
            <span>项目</span>
            <select
              disabled={projectsQuery.isPending || projectsQuery.isError}
              onChange={(event) => setProjectId(event.target.value)}
              value={projectId}
            >
              <option value="">
                {projectsQuery.isPending
                  ? "正在读取项目…"
                  : projectsQuery.isError
                    ? "项目暂不可用"
                    : "未归项目"}
              </option>
              {(projectsQuery.data ?? []).map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
            {projectsQuery.isError ? (
              <span className="form-field-error" role="alert">
                项目列表读取失败。
                <button
                  className="form-inline-action"
                  onClick={() => void projectsQuery.refetch()}
                  type="button"
                >
                  重新读取
                </button>
              </span>
            ) : null}
          </label>
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
              {(tasksQuery.data ?? []).map((task) => (
                <option key={task.id} value={task.id}>
                  {task.title}
                </option>
              ))}
            </select>
          </label>
        </div>

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
        <fieldset className="form-field">
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
        <div className="form-field">
          <span>标签</span>
          <TaskTagPicker
            disabled={mutation.isPending}
            enabled={open}
            onChange={setTagIds}
            selectedIds={tagIds}
          />
        </div>
        <label className="form-field form-field-last">
          <span>描述</span>
          <textarea
            onChange={(event) => setDescription(event.target.value)}
            placeholder="补充任务细节…"
            rows={4}
            value={description}
          />
        </label>
        <label className="form-field form-field-last">
          <span>完成标准</span>
          <textarea
            maxLength={10_000}
            onChange={(event) => setCompletionCriteria(event.target.value)}
            placeholder="写清楚完成后应达到的可验证结果…"
            rows={3}
            value={completionCriteria}
          />
        </label>
        {errorMessage ? (
          <div className="form-error" role="alert">
            {errorMessage}
          </div>
        ) : null}
        <p className="form-note">
          任务只会在本地 Sidecar 确认保存后出现在列表中。
        </p>
      </form>
    </Modal>
  );
}
