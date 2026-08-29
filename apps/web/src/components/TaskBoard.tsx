import { CalendarDays, FolderKanban } from "lucide-react";
import { useUiStore } from "../store/ui";
import type { Task, TaskStatus } from "../types/models";

const columns: { status: TaskStatus; label: string }[] = [
  { status: "todo", label: "待办" },
  { status: "in_progress", label: "进行中" },
  { status: "blocked", label: "阻塞" },
  { status: "waiting_review", label: "待验收" },
  { status: "done", label: "已完成" },
  { status: "cancelled", label: "已取消" },
];

interface TaskBoardProps {
  tasks: Task[];
  live: boolean;
  selectedIds: Set<string>;
  selectionLimitReached?: boolean;
  onSelectionChange: (task: Task, selected: boolean) => void;
}

function dateLabel(value: string): string {
  const [year, month, day] = value.split("-");
  return year && month && day ? `${month}/${day}` : value;
}

export function TaskBoard({
  tasks,
  live,
  selectedIds,
  selectionLimitReached = false,
  onSelectionChange,
}: TaskBoardProps) {
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);

  return (
    <div aria-label="任务看板" className="task-board">
      {columns.map((column) => {
        const columnTasks = tasks.filter(
          (task) => task.status === column.status,
        );
        return (
          <section
            aria-labelledby={`task-board-${column.status}`}
            className="task-board-column"
            key={column.status}
          >
            <header className="task-board-column-header">
              <span
                aria-hidden="true"
                className={`task-board-status task-board-status-${column.status}`}
              />
              <h2 id={`task-board-${column.status}`}>{column.label}</h2>
              <span title="当前页数量">{columnTasks.length}</span>
            </header>
            <div className="task-board-cards">
              {columnTasks.length ? (
                columnTasks.map((task) => {
                  const selected = selectedIds.has(task.id);
                  return (
                    <article className="task-board-card" key={task.id}>
                      <label className="task-board-select">
                        <input
                          aria-label={`选择任务：${task.title}`}
                          checked={selected}
                          disabled={
                            !live || (!selected && selectionLimitReached)
                          }
                          onChange={(event) =>
                            onSelectionChange(task, event.target.checked)
                          }
                          type="checkbox"
                        />
                      </label>
                      <button
                        aria-label={`查看任务：${task.title}`}
                        className="task-board-card-body"
                        onClick={() => setTaskDetailId(task.id)}
                        type="button"
                      >
                        <span className="task-board-card-title">
                          {task.title}
                        </span>
                        {task.projectName ? (
                          <span className="task-board-card-meta">
                            <FolderKanban size={12} />
                            {task.projectName}
                          </span>
                        ) : null}
                        <span className="task-board-card-footer">
                          <span
                            className={`task-priority task-priority-${task.priority.toLowerCase()}`}
                          >
                            {task.priority}
                          </span>
                          {task.dueDate ? (
                            <span className="task-board-card-meta">
                              <CalendarDays size={12} />
                              {dateLabel(task.dueDate)}
                            </span>
                          ) : null}
                          {task.tags.slice(0, 2).map((tag) => (
                            <span className="task-board-tag" key={tag.id}>
                              {tag.name}
                            </span>
                          ))}
                          {task.tags.length > 2 ? (
                            <span className="task-board-tag">
                              +{task.tags.length - 2}
                            </span>
                          ) : null}
                        </span>
                      </button>
                    </article>
                  );
                })
              ) : (
                <p className="task-board-empty">当前页暂无任务</p>
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
}
