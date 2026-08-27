import { useMemo, useState, type FormEvent } from "react";
import { useCreateTask } from "../api/hooks";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { TaskPriority } from "../types/models";
import { Modal } from "./Modal";

const priorities: { value: TaskPriority; label: string }[] = [
  { value: "P0", label: "紧急" },
  { value: "P1", label: "高" },
  { value: "P2", label: "中" },
  { value: "P3", label: "低" },
];

export function NewTaskModal() {
  const open = useUiStore((state) => state.newTaskOpen);
  const setOpen = useUiStore((state) => state.setNewTaskOpen);
  const mutation = useCreateTask();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [plannedDate, setPlannedDate] = useState("");
  const [estimatedMinutes, setEstimatedMinutes] = useState("50");
  const [priority, setPriority] = useState<TaskPriority>("P2");

  const errorMessage = useMemo(() => {
    if (!mutation.error) return null;
    if (mutation.error instanceof ApiError) {
      const suffix = mutation.error.requestId
        ? ` · 请求 ${mutation.error.requestId}`
        : "";
      return `${mutation.error.message}${suffix}`;
    }
    return "创建任务失败";
  }, [mutation.error]);

  const close = () => {
    if (mutation.isPending) return;
    setOpen(false);
    mutation.reset();
  };

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanTitle = title.trim();
    if (!cleanTitle) return;
    mutation.mutate(
      {
        title: cleanTitle,
        description: description.trim(),
        status: "todo",
        priority,
        projectId: null,
        plannedDate: plannedDate || null,
        estimatedMinutes: Number(estimatedMinutes) || null,
      },
      {
        onSuccess: () => {
          setTitle("");
          setDescription("");
          setPlannedDate("");
          setEstimatedMinutes("50");
          setPriority("P2");
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
            disabled={!title.trim() || mutation.isPending}
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
            <select disabled value="">
              <option value="">未归项目 · 项目 API 接入后可选</option>
            </select>
          </label>
          <label className="form-field">
            <span>计划日期</span>
            <input
              onChange={(event) => setPlannedDate(event.target.value)}
              type="date"
              value={plannedDate}
            />
          </label>
        </div>
        <label className="form-field">
          <span>预计时长</span>
          <div className="field-with-suffix">
            <input
              min="1"
              onChange={(event) => setEstimatedMinutes(event.target.value)}
              type="number"
              value={estimatedMinutes}
            />
            <span>分钟</span>
          </div>
        </label>
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
        <label className="form-field form-field-last">
          <span>描述</span>
          <textarea
            onChange={(event) => setDescription(event.target.value)}
            placeholder="补充任务细节…"
            rows={4}
            value={description}
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
