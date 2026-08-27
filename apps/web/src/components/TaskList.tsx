import { Check, Circle, CircleDotDashed, Clock3 } from "lucide-react";
import { useUpdateTaskStatus } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { Task, TaskStatus } from "../types/models";

function formatMinutes(minutes: number | null): string {
  if (!minutes) return "—";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
}

function statusIcon(status: TaskStatus) {
  if (status === "done") return <Check size={13} />;
  if (status === "in_progress") return <CircleDotDashed size={14} />;
  return <Circle size={14} />;
}

function nextStatus(status: TaskStatus): TaskStatus {
  return status === "done" ? "todo" : "done";
}

export function TaskList({
  tasks,
  live,
  compact = false,
}: {
  tasks: Task[];
  live: boolean;
  compact?: boolean;
}) {
  const updateStatus = useUpdateTaskStatus();
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);

  return (
    <div className={compact ? "task-list task-list-compact" : "task-list"}>
      {tasks.map((task) => (
        <article className={`task-row task-${task.status}`} key={task.id}>
          <button
            aria-label={
              task.status === "done"
                ? `恢复任务：${task.title}`
                : `完成任务：${task.title}`
            }
            className={`task-check task-check-${task.status}`}
            disabled={!live || updateStatus.isPending}
            onClick={() =>
              updateStatus.mutate({
                id: task.id,
                status: nextStatus(task.status),
              })
            }
            title={live ? "更新任务状态" : "本地服务不可用"}
            type="button"
          >
            {statusIcon(task.status)}
          </button>
          <button
            aria-label={`查看任务：${task.title}`}
            className="task-open"
            onClick={() => setTaskDetailId(task.id)}
            type="button"
          >
            <span className="task-copy min-w-0 flex-1">
              <span className="task-title">{task.title}</span>
              {compact ? null : (
                <span className="task-tags">
                  {(task.tags ?? []).slice(0, 3).map((tag) => (
                    <span className="tag" key={tag}>
                      {tag}
                    </span>
                  ))}
                </span>
              )}
            </span>
            <span className="task-meta">
              <span className="task-project">
                {task.projectName ?? "未归项目"}
              </span>
              <span
                className={`priority priority-${task.priority.toLowerCase()}`}
              >
                {task.priority}
              </span>
              <span className="duration">
                <Clock3 size={12} />
                {formatMinutes(task.estimatedMinutes)}
              </span>
            </span>
          </button>
        </article>
      ))}
    </div>
  );
}
