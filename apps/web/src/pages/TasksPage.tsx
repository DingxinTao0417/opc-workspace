import { Columns3, List, Plus, Search, SlidersHorizontal } from "lucide-react";
import { useMemo, useState } from "react";
import { useTasksQuery } from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { TaskList } from "../components/TaskList";
import { useUiStore } from "../store/ui";
import type { Task, TaskStatus } from "../types/models";

const groups: { status: TaskStatus; label: string }[] = [
  { status: "in_progress", label: "进行中" },
  { status: "todo", label: "待办" },
  { status: "done", label: "已完成" },
];

export function TasksPage() {
  const query = useTasksQuery();
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const [search, setSearch] = useState("");
  const live = query.isSuccess;
  const displayTasks = query.data ?? [];
  const filtered = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase();
    if (!needle) return displayTasks;
    return displayTasks.filter((task) =>
      `${task.title} ${task.description} ${task.projectName ?? ""} ${(task.tags ?? []).join(" ")}`
        .toLocaleLowerCase()
        .includes(needle),
    );
  }, [displayTasks, search]);

  return (
    <div className="page">
      <PageHeader
        actions={
          <button
            className="button button-primary"
            onClick={() => setNewTaskOpen(true)}
            type="button"
          >
            <Plus size={15} />
            新建任务
          </button>
        }
        meta={
          <span className="page-count">
            {query.isPending
              ? "读取中"
              : query.isSuccess
                ? `${query.data.length} 项`
                : "数据不可用"}
          </span>
        }
        title="任务"
      />

      <div className="toolbar">
        <label className="toolbar-search">
          <Search size={15} />
          <input
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索任务…"
            value={search}
          />
        </label>
        <button className="button button-secondary" type="button">
          <SlidersHorizontal size={14} />
          筛选
        </button>
        <div className="segmented" aria-label="视图">
          <button
            aria-pressed="true"
            className="segmented-active"
            type="button"
          >
            <List size={15} />
          </button>
          <button
            aria-label="看板视图将在后续版本提供"
            disabled
            title="后续版本"
            type="button"
          >
            <Columns3 size={15} />
          </button>
        </div>
      </div>

      {query.isError ? (
        <ErrorState
          message="无法连接任务 API；请确认本地服务已启动后重试。"
          onRetry={() => void query.refetch()}
        />
      ) : null}

      {query.isPending ? <SkeletonRows count={7} /> : null}

      {query.isSuccess && query.data.length === 0 ? (
        <EmptyState
          action={
            <button
              className="button button-primary"
              onClick={() => setNewTaskOpen(true)}
              type="button"
            >
              <Plus size={15} />
              新建第一项任务
            </button>
          }
          message="本地数据库中还没有任务。"
          title="任务列表是空的"
        />
      ) : null}

      {!query.isPending && filtered.length > 0 ? (
        <div className="task-groups">
          {groups.map((group) => {
            const tasks: Task[] = filtered.filter(
              (task) => task.status === group.status,
            );
            if (!tasks.length) return null;
            return (
              <section className="task-group" key={group.status}>
                <div className="task-group-heading">
                  <h2>{group.label}</h2>
                  <span>{tasks.length}</span>
                </div>
                <TaskList live={live} tasks={tasks} />
              </section>
            );
          })}
        </div>
      ) : null}

      {!query.isPending && displayTasks.length > 0 && filtered.length === 0 ? (
        <EmptyState
          message="换一个关键词，或清除当前搜索条件。"
          title="没有匹配的任务"
        />
      ) : null}
    </div>
  );
}
