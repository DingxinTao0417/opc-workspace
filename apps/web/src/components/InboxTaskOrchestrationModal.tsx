import { AlertTriangle, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  useAssignmentActorOptionsQuery,
  useProjectOptionsQuery,
  useSplitInboxItem,
} from "../api/hooks";
import type {
  InboxItem,
  InboxResolutionPolicy,
  InboxSplitTaskInput,
  TaskKind,
  TaskPriority,
  TaskReviewPolicy,
} from "../types/models";
import { Modal } from "./Modal";

type DraftTask = Omit<InboxSplitTaskInput, "key"> & { localId: number };

const priorities: Array<{ value: TaskPriority; label: string }> = [
  { value: "P0", label: "紧急" },
  { value: "P1", label: "高" },
  { value: "P2", label: "中" },
  { value: "P3", label: "低" },
];

const kinds: Array<{ value: TaskKind; label: string }> = [
  { value: "work", label: "工作" },
  { value: "review", label: "复核" },
  { value: "followup", label: "跟进" },
  { value: "reminder", label: "提醒" },
];

function newDraft(localId: number, assigneeActorId = ""): DraftTask {
  return {
    localId,
    parentKey: null,
    title: "",
    description: "",
    kind: "work",
    priority: "P2",
    projectId: null,
    completionCriteria: "",
    tagIds: [],
    dueDate: null,
    plannedDate: null,
    estimatedMinutes: null,
    reviewPolicy: "none",
    isRequired: true,
    assigneeActorId,
  };
}

function taskKey(localId: number): string {
  return `task-${localId}`;
}

function orchestrationError(error: unknown): string {
  if (error instanceof ApiError) {
    const request = error.requestId ? ` · 请求 ${error.requestId}` : "";
    return `${error.message}${request}`;
  }
  return "任务拆分失败，请重试。";
}

export function InboxTaskOrchestrationModal({
  item,
  expectedVersion,
  open,
  onClose,
  onCreated,
}: {
  item: InboxItem;
  expectedVersion: number;
  open: boolean;
  onClose: () => void;
  onCreated?: () => Promise<unknown> | unknown;
}) {
  const actorsQuery = useAssignmentActorOptionsQuery(open);
  const projectsQuery = useProjectOptionsQuery(open);
  const mutation = useSplitInboxItem();
  const [policy, setPolicy] = useState<InboxResolutionPolicy>(
    "all_required_tasks_done",
  );
  const [tasks, setTasks] = useState<DraftTask[]>([newDraft(1)]);
  const [nextId, setNextId] = useState(2);
  const [validationError, setValidationError] = useState<string | null>(null);

  const actors = useMemo(
    () =>
      (actorsQuery.data ?? []).filter(
        (actor) =>
          actor.status === "active" &&
          (actor.type === "owner" || actor.type === "person"),
      ),
    [actorsQuery.data],
  );

  useEffect(() => {
    if (!open) return;
    setPolicy("all_required_tasks_done");
    setTasks([newDraft(1)]);
    setNextId(2);
    setValidationError(null);
    mutation.reset();
  }, [item.id, open]);

  useEffect(() => {
    const owner = actors.find((actor) => actor.type === "owner");
    if (!owner) return;
    setTasks((current) =>
      current.some((task) => !task.assigneeActorId)
        ? current.map((task) =>
            task.assigneeActorId
              ? task
              : { ...task, assigneeActorId: owner.id },
          )
        : current,
    );
  }, [actors]);

  const updateTask = (localId: number, patch: Partial<DraftTask>) => {
    setTasks((current) =>
      current.map((task) =>
        task.localId === localId ? { ...task, ...patch } : task,
      ),
    );
  };

  const addTask = () => {
    if (tasks.length >= 20) return;
    const owner = actors.find((actor) => actor.type === "owner");
    setTasks((current) => [
      ...current,
      newDraft(nextId, owner?.id ?? actors[0]?.id ?? ""),
    ]);
    setNextId((value) => value + 1);
  };

  const removeTask = (localId: number) => {
    if (tasks.length === 1) return;
    const removedKey = taskKey(localId);
    setTasks((current) =>
      current
        .filter((task) => task.localId !== localId)
        .map((task) =>
          task.parentKey === removedKey ? { ...task, parentKey: null } : task,
        ),
    );
  };

  const close = () => {
    if (mutation.isPending) return;
    onClose();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const invalidIndex = tasks.findIndex(
      (task) =>
        Array.from(task.title.trim()).length < 2 ||
        Array.from(task.title.trim()).length > 200,
    );
    if (invalidIndex >= 0) {
      setValidationError(
        `第 ${invalidIndex + 1} 个任务名称需要 2–200 个字符。`,
      );
      return;
    }
    if (
      policy === "all_required_tasks_done" &&
      !tasks.some((task) => task.isRequired)
    ) {
      setValidationError("自动解决策略至少需要一项必需任务。");
      return;
    }
    if (tasks.some((task) => !task.assigneeActorId)) {
      setValidationError("每个任务都必须选择负责人。");
      return;
    }
    setValidationError(null);
    mutation.mutate(
      {
        inboxItemId: item.id,
        expectedVersion,
        resolutionPolicy: policy,
        tasks: tasks.map((task) => ({
          key: taskKey(task.localId),
          parentKey: task.parentKey,
          title: task.title.trim(),
          description: task.description.trim(),
          kind: task.kind,
          priority: task.priority,
          projectId: task.projectId,
          completionCriteria: task.completionCriteria.trim(),
          tagIds: task.tagIds,
          dueDate: task.dueDate,
          plannedDate: task.plannedDate,
          estimatedMinutes: task.estimatedMinutes,
          reviewPolicy: task.reviewPolicy,
          isRequired: task.isRequired,
          assigneeActorId: task.assigneeActorId,
        })),
      },
      {
        onSuccess: async () => {
          await onCreated?.();
          onClose();
        },
      },
    );
  };

  const errorMessage =
    validationError ??
    (mutation.error ? orchestrationError(mutation.error) : null);

  return (
    <Modal
      dismissible={!mutation.isPending}
      footer={
        <>
          <span className="inbox-split-footer-note">
            {tasks.length} / 20 个任务 · 单次原子保存
          </span>
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
            disabled={mutation.isPending || actorsQuery.isError}
            form="inbox-task-split-form"
            type="submit"
          >
            {mutation.isPending ? "正在创建…" : "创建并开始跟踪"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title="拆分并分派任务"
      width="860px"
    >
      <form id="inbox-task-split-form" onSubmit={submit}>
        <div className="inbox-split-intro">
          <div>
            <strong>{item.title}</strong>
            <p>任务、父子关系、负责人和收件箱关系会在同一事务中保存。</p>
          </div>
          <label className="form-field inbox-split-policy">
            <span>解决策略</span>
            <select
              disabled={mutation.isPending}
              onChange={(event) =>
                setPolicy(event.target.value as InboxResolutionPolicy)
              }
              value={policy}
            >
              <option value="all_required_tasks_done">
                必需任务全部完成后自动解决
              </option>
              <option value="manual">仅手动解决</option>
            </select>
          </label>
        </div>

        {actorsQuery.isError ? (
          <div className="form-error" role="alert">
            无法读取负责人，请重新读取后再拆分。
            <button
              className="form-inline-action"
              onClick={() => void actorsQuery.refetch()}
              type="button"
            >
              重新读取
            </button>
          </div>
        ) : null}

        <div className="inbox-split-task-list">
          {tasks.map((task, index) => {
            const earlierTasks = tasks.slice(0, index);
            return (
              <section className="inbox-split-task-card" key={task.localId}>
                <header>
                  <span className="inbox-split-task-index">{index + 1}</span>
                  <strong>{task.title.trim() || "新任务"}</strong>
                  <button
                    aria-label={`删除第 ${index + 1} 个任务`}
                    className="icon-button"
                    disabled={tasks.length === 1 || mutation.isPending}
                    onClick={() => removeTask(task.localId)}
                    type="button"
                  >
                    <Trash2 size={14} />
                  </button>
                </header>
                <div className="inbox-split-task-grid">
                  <label className="form-field inbox-split-title-field">
                    <span>任务名称</span>
                    <input
                      autoFocus={index === 0}
                      disabled={mutation.isPending}
                      maxLength={200}
                      onChange={(event) =>
                        updateTask(task.localId, { title: event.target.value })
                      }
                      placeholder="写清楚可执行结果…"
                      value={task.title}
                    />
                  </label>
                  <label className="form-field">
                    <span>负责人</span>
                    <select
                      disabled={mutation.isPending || actorsQuery.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          assigneeActorId: event.target.value,
                        })
                      }
                      value={task.assigneeActorId}
                    >
                      <option value="">
                        {actorsQuery.isPending ? "正在读取…" : "选择负责人"}
                      </option>
                      {actors.map((actor) => (
                        <option key={actor.id} value={actor.id}>
                          {actor.displayName}
                          {actor.type === "owner" ? "（所有者）" : ""}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="form-field">
                    <span>父任务</span>
                    <select
                      disabled={mutation.isPending || earlierTasks.length === 0}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          parentKey: event.target.value || null,
                        })
                      }
                      value={task.parentKey ?? ""}
                    >
                      <option value="">无父任务</option>
                      {earlierTasks.map((candidate, candidateIndex) => (
                        <option
                          key={candidate.localId}
                          value={taskKey(candidate.localId)}
                        >
                          {candidateIndex + 1}.{" "}
                          {candidate.title || "未命名任务"}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="form-field">
                    <span>项目</span>
                    <select
                      disabled={mutation.isPending || projectsQuery.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          projectId: event.target.value || null,
                        })
                      }
                      value={task.projectId ?? ""}
                    >
                      <option value="">未归项目</option>
                      {(projectsQuery.data ?? []).map((project) => (
                        <option key={project.id} value={project.id}>
                          {project.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="form-field">
                    <span>类型</span>
                    <select
                      disabled={mutation.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          kind: event.target.value as TaskKind,
                        })
                      }
                      value={task.kind}
                    >
                      {kinds.map((kind) => (
                        <option key={kind.value} value={kind.value}>
                          {kind.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="form-field">
                    <span>优先级</span>
                    <select
                      disabled={mutation.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          priority: event.target.value as TaskPriority,
                        })
                      }
                      value={task.priority}
                    >
                      {priorities.map((priority) => (
                        <option key={priority.value} value={priority.value}>
                          {priority.value} · {priority.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="form-field">
                    <span>验收</span>
                    <select
                      disabled={mutation.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          reviewPolicy: event.target.value as TaskReviewPolicy,
                        })
                      }
                      value={task.reviewPolicy}
                    >
                      <option value="none">无需验收</option>
                      <option value="manual">所有者人工验收</option>
                    </select>
                  </label>
                  <label className="inbox-task-required-toggle inbox-split-required">
                    <input
                      checked={task.isRequired}
                      disabled={mutation.isPending}
                      onChange={(event) =>
                        updateTask(task.localId, {
                          isRequired: event.target.checked,
                        })
                      }
                      type="checkbox"
                    />
                    <span>
                      <strong>必需任务</strong>
                      <small>计入自动解决进度</small>
                    </span>
                  </label>
                </div>
                <label className="form-field form-field-last">
                  <span>描述</span>
                  <textarea
                    disabled={mutation.isPending}
                    maxLength={10_000}
                    onChange={(event) =>
                      updateTask(task.localId, {
                        description: event.target.value,
                      })
                    }
                    placeholder="补充上下文或完成标准…"
                    rows={2}
                    value={task.description}
                  />
                </label>
              </section>
            );
          })}
        </div>

        <button
          className="button button-secondary inbox-split-add"
          disabled={tasks.length >= 20 || mutation.isPending}
          onClick={addTask}
          type="button"
        >
          <Plus size={14} />
          添加任务
        </button>

        {errorMessage ? (
          <div className="form-error" role="alert">
            <AlertTriangle size={14} />
            {errorMessage}
          </div>
        ) : null}
      </form>
    </Modal>
  );
}
