import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Ban,
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleDotDashed,
  Clock3,
  Eye,
} from "lucide-react";
import { useState } from "react";
import { useTaskPageQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { Task, TaskStatus } from "../types/models";

function formatMinutes(minutes: number | null): string {
  if (minutes === null) return "—";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
}

function statusIcon(status: TaskStatus) {
  if (status === "done") return <Check size={13} />;
  if (status === "in_progress") return <CircleDotDashed size={14} />;
  if (status === "blocked") return <AlertTriangle size={13} />;
  if (status === "waiting_review") return <Eye size={13} />;
  if (status === "cancelled") return <Ban size={13} />;
  return <Circle size={14} />;
}

const statusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

interface TaskListProps {
  tasks: Task[];
  live: boolean;
  compact?: boolean;
  hierarchical?: boolean;
  level?: number;
  showParent?: boolean;
  selectedIds?: Set<string>;
  selectionLimitReached?: boolean;
  onSelectionChange?: (task: Task, selected: boolean) => void;
  allowReorder?: boolean;
  onMove?: (task: Task, direction: "up" | "down") => void;
  reorderPendingId?: string | null;
}

function TaskRow({
  task,
  live,
  compact,
  hierarchical,
  level,
  showParent,
  selectedIds,
  selectionLimitReached,
  onSelectionChange,
  allowReorder,
  onMove,
  reorderPendingId,
}: Required<
  Pick<
    TaskListProps,
    | "live"
    | "compact"
    | "hierarchical"
    | "level"
    | "showParent"
    | "allowReorder"
  >
> &
  Omit<
    TaskListProps,
    | "tasks"
    | "live"
    | "compact"
    | "hierarchical"
    | "level"
    | "showParent"
    | "allowReorder"
  > & {
    task: Task;
  }) {
  const [expanded, setExpanded] = useState(false);
  const [childPage, setChildPage] = useState(1);
  const childrenQuery = useTaskPageQuery(
    {
      page: childPage,
      pageSize: 100,
      parentTaskId: task.id,
      sort: "manual_order",
    },
    hierarchical && expanded && task.subtaskTotal > 0,
  );
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const canExpand = hierarchical && task.subtaskTotal > 0;
  const isSelected = selectedIds?.has(task.id) ?? false;
  const childrenLive =
    live &&
    childrenQuery.isSuccess &&
    !childrenQuery.isPlaceholderData &&
    !childrenQuery.isFetching;
  const childTotal = childrenQuery.data?.meta.total ?? 0;
  const childPages = Math.max(1, Math.ceil(childTotal / 100));

  return (
    <div className="task-tree-node">
      <article
        aria-level={hierarchical ? level : undefined}
        className={`task-row task-${task.status}${isSelected ? " task-row-selected" : ""}`}
        role={hierarchical ? "treeitem" : undefined}
        style={hierarchical ? { paddingLeft: 9 + (level - 1) * 18 } : undefined}
      >
        <div className="task-leading">
          {onSelectionChange ? (
            <input
              aria-label={`选择任务：${task.title}`}
              checked={isSelected}
              className="task-select"
              disabled={!live || (selectionLimitReached && !isSelected)}
              onChange={(event) =>
                onSelectionChange(task, event.target.checked)
              }
              type="checkbox"
            />
          ) : null}
          {hierarchical ? (
            canExpand ? (
              <button
                aria-expanded={expanded}
                aria-label={`${expanded ? "收起" : "展开"}子任务：${task.title}`}
                className="task-expand"
                onClick={() => setExpanded((value) => !value)}
                type="button"
              >
                {expanded ? (
                  <ChevronDown size={14} />
                ) : (
                  <ChevronRight size={14} />
                )}
              </button>
            ) : (
              <span aria-hidden="true" className="task-expand-spacer" />
            )
          ) : null}
          <button
            aria-label={`查看任务状态：${task.title}（${statusLabels[task.status]}）`}
            className={`task-check task-check-${task.status}`}
            onClick={() => setTaskDetailId(task.id)}
            title={`${statusLabels[task.status]} · 打开详情执行状态操作`}
            type="button"
          >
            {statusIcon(task.status)}
          </button>
        </div>
        <button
          aria-label={`查看任务：${task.title}`}
          className="task-open"
          onClick={() => setTaskDetailId(task.id)}
          type="button"
        >
          <span className="task-copy min-w-0 flex-1">
            {showParent && task.parentTaskTitle ? (
              <span className="task-parent-path">
                {task.parentTaskTitle} / 子任务
              </span>
            ) : null}
            <span className="task-title">{task.title}</span>
            {compact ? null : (
              <span className="task-tags">
                {task.tags.slice(0, 3).map((tag) => (
                  <span className="tag" key={tag.id}>
                    <span
                      aria-hidden="true"
                      className="task-tag-dot"
                      style={{ backgroundColor: tag.color }}
                    />
                    {tag.name}
                  </span>
                ))}
                {task.tags.length > 3 ? (
                  <span className="tag">+{task.tags.length - 3}</span>
                ) : null}
              </span>
            )}
          </span>
          <span className="task-meta">
            {task.subtaskTotal > 0 ? (
              <span className="task-subtask-progress">
                {task.subtaskCompleted}/{task.subtaskTotal}
              </span>
            ) : null}
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
        {allowReorder && onMove ? (
          <div className="task-order-actions">
            <button
              aria-label={`上移任务：${task.title}`}
              disabled={!live || reorderPendingId === task.id}
              onClick={() => onMove(task, "up")}
              title="上移"
              type="button"
            >
              <ArrowUp size={12} />
            </button>
            <button
              aria-label={`下移任务：${task.title}`}
              disabled={!live || reorderPendingId === task.id}
              onClick={() => onMove(task, "down")}
              title="下移"
              type="button"
            >
              <ArrowDown size={12} />
            </button>
          </div>
        ) : null}
      </article>
      {canExpand && expanded ? (
        <div className="task-tree-children" role="group">
          {childrenQuery.isPending ? (
            <p className="task-tree-note">正在读取子任务…</p>
          ) : null}
          {childrenQuery.isError ? (
            <p className="task-row-error" role="alert">
              子任务读取失败。
              <button
                className="form-inline-action"
                onClick={() => void childrenQuery.refetch()}
                type="button"
              >
                重试
              </button>
            </p>
          ) : null}
          {childrenQuery.data?.items.length ? (
            <TaskList
              compact
              hierarchical
              level={level + 1}
              live={childrenLive}
              onMove={onMove}
              onSelectionChange={onSelectionChange}
              reorderPendingId={reorderPendingId}
              selectedIds={selectedIds}
              selectionLimitReached={selectionLimitReached}
              tasks={childrenQuery.data.items}
            />
          ) : null}
          {childPage > 1 || childTotal > 100 ? (
            <nav
              aria-label={`子任务分页：${task.title}`}
              className="task-tree-pagination"
            >
              <button
                className="form-inline-action"
                disabled={childPage <= 1 || childrenQuery.isFetching}
                onClick={() => setChildPage((value) => Math.max(1, value - 1))}
                type="button"
              >
                上一页
              </button>
              <span>
                {childPage} / {childPages} · 共 {childTotal} 项
              </span>
              <button
                className="form-inline-action"
                disabled={childPage >= childPages || childrenQuery.isFetching}
                onClick={() => setChildPage((value) => value + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function TaskList({
  tasks,
  live,
  compact = false,
  hierarchical = false,
  level = 1,
  showParent = false,
  selectedIds,
  selectionLimitReached = false,
  onSelectionChange,
  allowReorder = false,
  onMove,
  reorderPendingId = null,
}: TaskListProps) {
  return (
    <div
      className={compact ? "task-list task-list-compact" : "task-list"}
      role={hierarchical && level === 1 ? "tree" : undefined}
    >
      {tasks.map((task) => (
        <TaskRow
          allowReorder={allowReorder}
          compact={compact}
          hierarchical={hierarchical}
          key={task.id}
          level={level}
          live={live}
          onMove={onMove}
          onSelectionChange={onSelectionChange}
          reorderPendingId={reorderPendingId}
          selectedIds={selectedIds}
          selectionLimitReached={selectionLimitReached}
          showParent={showParent}
          task={task}
        />
      ))}
    </div>
  );
}
