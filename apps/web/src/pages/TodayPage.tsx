import {
  AlertTriangle,
  CalendarDays,
  CheckCircle2,
  Hourglass,
  List,
  Plus,
  Target,
} from "lucide-react";
import { useTodayStatsQuery, useTasksQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { Task } from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { TaskList } from "../components/TaskList";

function localDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function formatToday(date: Date): string {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "long",
    day: "numeric",
    weekday: "short",
  }).format(date);
}

function taskSplit(tasks: Task[]) {
  const active = tasks.filter(
    (task) => task.status !== "done" && task.status !== "cancelled",
  );
  return {
    today: active.slice(0, 3),
    later: active.slice(3, 6),
  };
}

export function TodayPage() {
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const tasksQuery = useTasksQuery();
  const now = new Date();
  const dateKey = localDateKey(now);
  const statsQuery = useTodayStatsQuery(dateKey);
  const live = tasksQuery.isSuccess;
  const displayTasks = tasksQuery.data ?? [];
  const groups = taskSplit(displayTasks);
  const realStats = statsQuery.data;
  const estimated = realStats
    ? Math.round((realStats.tasks.estimatedMinutes / 60) * 10) / 10
    : 0;
  const stats = [
    { icon: Hourglass, value: `${estimated}h`, label: "预计时长" },
    {
      icon: CheckCircle2,
      value: realStats?.tasks.remaining ?? 0,
      label: "项待完成",
    },
    {
      icon: AlertTriangle,
      value: realStats?.tasks.overdue ?? 0,
      label: "项已逾期",
      danger: true,
    },
  ];

  return (
    <div className="page today-page">
      <header className="today-header">
        <div className="today-heading-group">
          <h1 className="page-title">今日</h1>
          <span className="date-pill">
            <CalendarDays size={13} />
            {formatToday(now)}
          </span>
        </div>
        <div className="page-actions">
          <button
            aria-label="列表视图"
            className="icon-button icon-button-active"
            type="button"
          >
            <List size={16} />
          </button>
          <button
            className="button button-primary"
            onClick={() => setNewTaskOpen(true)}
            type="button"
          >
            <Plus size={15} /> 新建
          </button>
        </div>
      </header>

      <section className="stats-row" aria-label="今日统计">
        {stats.map(({ icon: Icon, value, label, danger }) => (
          <div
            className={`stat-card${danger ? " stat-card-danger" : ""}`}
            key={label}
          >
            <span className="stat-icon">
              <Icon size={15} />
            </span>
            <strong>{value}</strong>
            <span>{label}</span>
          </div>
        ))}
        <div className="goal-card">
          <Target size={15} />
          <span>月收入目标</span>
          <strong>—</strong>
          <div className="goal-track">
            <span style={{ width: "0%" }} />
          </div>
        </div>
      </section>

      {tasksQuery.isError ? (
        <ErrorState
          compact
          message="无法读取任务；请确认本地服务已启动后重试。"
          onRetry={() => void tasksQuery.refetch()}
        />
      ) : null}

      {statsQuery.isError ? (
        <ErrorState
          compact
          message="无法读取今日统计；当前不显示估算数据。"
          onRetry={() => void statsQuery.refetch()}
        />
      ) : null}

      <section className="task-section">
        <div className="section-heading compact-heading">
          <div>
            <span className="section-kicker">接下来</span>
            <h2>今天</h2>
          </div>
          <span className="section-count">{groups.today.length}</span>
        </div>
        {tasksQuery.isPending ? (
          <SkeletonRows count={3} />
        ) : tasksQuery.isSuccess && displayTasks.length === 0 ? (
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
            title="今天还没有任务"
          />
        ) : (
          <TaskList compact live={live} tasks={groups.today} />
        )}
      </section>

      {tasksQuery.isPending || displayTasks.length > 0 ? (
        <section className="task-section">
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker">稍后</span>
              <h2>本周</h2>
            </div>
            <span className="section-count">{groups.later.length}</span>
          </div>
          {tasksQuery.isPending ? (
            <SkeletonRows count={2} />
          ) : (
            <TaskList compact live={live} tasks={groups.later} />
          )}
        </section>
      ) : null}
    </div>
  );
}
