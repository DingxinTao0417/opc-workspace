import {
  AlertTriangle,
  CalendarDays,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  ClipboardCheck,
  GitBranch,
  Hourglass,
  Inbox as InboxIcon,
  List,
  Plus,
  RotateCcw,
  Target,
} from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useInboxStatsQuery,
  useActiveFocusSessionQuery,
  useCreateFocusSession,
  useMoveTaskWithinPlan,
  useReorderActiveTasksWithinPlan,
  useResetTaskOrder,
  useTaskLifecycleCommand,
  useTodayTaskGroupsQuery,
  useTodayStatsQuery,
} from "../api/hooks";
import { useFocusCycleStore } from "../store/focus";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { TaskList } from "../components/TaskList";
import { TaskPlanModal } from "../components/TaskPlanModal";
import type { Task } from "../types/models";

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

function dateFromKey(dateKey: string): Date {
  const [year, month, day] = dateKey.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function shiftLocalDateKey(dateKey: string, days: number): string {
  const date = dateFromKey(dateKey);
  date.setDate(date.getDate() + days);
  return localDateKey(date);
}

function reorderErrorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) return error.message;
  return "无法保存任务顺序，请重试。";
}

const quickActionMessages: Record<string, string> = {
  TASK_ASSIGNEE_REQUIRED: "开始执行前需要先在任务详情中设置负责人。",
  TASK_REVIEW_REQUIRED: "此任务必须走受控验收流程，不能直接完成。",
  TASK_TRANSITION_NOT_ALLOWED: "任务状态已经变化，请按最新状态重试。",
  VERSION_CONFLICT: "任务已经被更新，最新状态正在刷新，请重试。",
  ACTIVE_FOCUS_SESSION_EXISTS: "已有专注会话，请先在右侧专注栏处理。",
};

function quickActionErrorText(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    const message = quickActionMessages[error.code] ?? error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "快捷操作失败，请刷新本地状态后重试。";
}

function applyOptimisticOrder(tasks: Task[], orderedIds?: string[]): Task[] {
  if (!orderedIds || orderedIds.length !== tasks.length) return tasks;
  const byId = new Map(tasks.map((task) => [task.id, task]));
  if (orderedIds.some((id) => !byId.has(id))) return tasks;
  return orderedIds.map((id) => byId.get(id)!);
}

function moveTaskId(
  tasks: Task[],
  sourceId: string,
  targetId: string,
): string[] {
  const ids = tasks.map((task) => task.id);
  const sourceIndex = ids.indexOf(sourceId);
  const targetIndex = ids.indexOf(targetId);
  if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex)
    return ids;
  const [source] = ids.splice(sourceIndex, 1);
  const targetAfterRemoval = ids.indexOf(targetId);
  ids.splice(
    targetAfterRemoval + (sourceIndex < targetIndex ? 1 : 0),
    0,
    source,
  );
  return ids;
}

export function TodayPage() {
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const today = new Date();
  const todayKey = localDateKey(today);
  const [dateKey, setDateKey] = useState(todayKey);
  const [planningTask, setPlanningTask] = useState<Task | null>(null);
  const selectedDate = dateFromKey(dateKey);
  const isToday = dateKey === todayKey;
  const taskGroupsQuery = useTodayTaskGroupsQuery(dateKey);
  const statsQuery = useTodayStatsQuery(dateKey);
  const inboxStatsQuery = useInboxStatsQuery();
  const focusQuery = useActiveFocusSessionQuery();
  const lifecycleMutation = useTaskLifecycleCommand();
  const createFocusMutation = useCreateFocusSession();
  const moveMutation = useMoveTaskWithinPlan();
  const dragMutation = useReorderActiveTasksWithinPlan();
  const resetOrderMutation = useResetTaskOrder();
  const [optimisticOrders, setOptimisticOrders] = useState<
    Record<string, string[]>
  >({});
  const focusMinutes = useSettingsStore((state) => state.focusMinutes);
  const focusCycles = useSettingsStore((state) => state.cycles);
  const focusPhase = useFocusCycleStore((state) => state.phase);
  const beginFocusWork = useFocusCycleStore((state) => state.beginWork);
  const live = taskGroupsQuery.isSuccess;
  const reorderReady =
    live &&
    !taskGroupsQuery.isFetching &&
    !moveMutation.isPending &&
    !dragMutation.isPending &&
    !resetOrderMutation.isPending;
  const reorderError =
    reorderErrorText(dragMutation.error) ??
    reorderErrorText(moveMutation.error) ??
    reorderErrorText(resetOrderMutation.error);
  const quickActionError =
    quickActionErrorText(lifecycleMutation.error) ??
    quickActionErrorText(createFocusMutation.error);
  const quickActionPendingId = lifecycleMutation.isPending
    ? (lifecycleMutation.variables?.id ?? null)
    : createFocusMutation.isPending
      ? (createFocusMutation.variables?.taskId ?? null)
      : null;
  const quickActionsDisabled =
    taskGroupsQuery.isFetching ||
    lifecycleMutation.isPending ||
    createFocusMutation.isPending ||
    !reorderReady;
  const focusActionDisabled =
    !focusQuery.isSuccess ||
    focusQuery.isFetching ||
    Boolean(focusQuery.data?.session) ||
    focusPhase !== "idle";
  const groups = taskGroupsQuery.data ?? {
    overdue: [],
    today: [],
    thisWeek: [],
    unscheduled: [],
  };
  const realStats = statsQuery.data;
  const todayOrderKey = `date:${dateKey}`;
  const unscheduledOrderKey = "unscheduled";
  const orderedTodayTasks = applyOptimisticOrder(
    groups.today,
    optimisticOrders[todayOrderKey],
  );
  const orderedUnscheduledTasks = applyOptimisticOrder(
    groups.unscheduled,
    optimisticOrders[unscheduledOrderKey],
  );
  const estimated = realStats
    ? Math.round((realStats.tasks.estimatedMinutes / 60) * 10) / 10
    : 0;
  const statsPending = statsQuery.isPending;
  const stats = [
    {
      icon: Hourglass,
      value: statsPending ? "…" : `${estimated}h`,
      label: "预计时长",
    },
    {
      icon: CheckCircle2,
      value: statsPending ? "…" : (realStats?.tasks.remaining ?? 0),
      label: "项待完成",
    },
    {
      icon: AlertTriangle,
      value: statsPending ? "…" : (realStats?.tasks.overdue ?? 0),
      label: "项已逾期",
      danger: true,
    },
  ];
  const inboxStats = [
    {
      icon: InboxIcon,
      label: "待处理",
      value: inboxStatsQuery.data?.pending ?? 0,
      to: "/inbox",
    },
    {
      icon: GitBranch,
      label: "跟进中",
      value: inboxStatsQuery.data?.tracking ?? 0,
      to: "/inbox?risk=tracking",
    },
    {
      icon: ClipboardCheck,
      label: "待验收",
      value: inboxStatsQuery.data?.waitingReview ?? 0,
      to: "/inbox?risk=waiting_review",
    },
    {
      icon: AlertTriangle,
      label: "有阻塞",
      value: inboxStatsQuery.data?.blocked ?? 0,
      to: "/inbox?risk=blocked",
      danger: true,
    },
  ];
  const moveTask = (
    taskId: string,
    plannedDate: string | null,
    direction: "up" | "down",
  ) => {
    dragMutation.reset();
    resetOrderMutation.reset();
    moveMutation.mutate({
      taskId,
      plannedDate,
      direction,
      scope: "active",
    });
  };
  const clearOptimisticOrder = (key: string) => {
    setOptimisticOrders((current) => {
      if (!(key in current)) return current;
      const next = { ...current };
      delete next[key];
      return next;
    });
  };
  const resetPlanOrder = (plannedDate: string | null, orderKey: string) => {
    clearOptimisticOrder(orderKey);
    dragMutation.reset();
    moveMutation.reset();
    resetOrderMutation.mutate(plannedDate);
  };
  const dropTask = (
    source: Task,
    target: Task,
    tasks: Task[],
    plannedDate: string | null,
    orderKey: string,
  ) => {
    const orderedTaskIds = moveTaskId(tasks, source.id, target.id);
    if (orderedTaskIds.every((id, index) => id === tasks[index]?.id)) return;
    moveMutation.reset();
    resetOrderMutation.reset();
    setOptimisticOrders((current) => ({
      ...current,
      [orderKey]: orderedTaskIds,
    }));
    dragMutation.mutate(
      { plannedDate, orderedTaskIds },
      { onSettled: () => clearOptimisticOrder(orderKey) },
    );
  };
  const runLifecycleAction = (task: Task, action: "start" | "complete") => {
    createFocusMutation.reset();
    lifecycleMutation.reset();
    lifecycleMutation.mutate({
      id: task.id,
      input: { action, expectedVersion: task.version },
    });
  };
  const startFocus = (task: Task) => {
    lifecycleMutation.reset();
    createFocusMutation.reset();
    if (focusActionDisabled) return;
    createFocusMutation.mutate(
      {
        taskId: task.id,
        plannedSeconds: focusMinutes * 60,
      },
      {
        onSuccess: () => beginFocusWork(task.id, focusCycles, task.title),
      },
    );
  };
  const taskQuickActionProps = {
    focusActionDisabled,
    onCompleteTask: (task: Task) => runLifecycleAction(task, "complete"),
    onStartFocus: startFocus,
    onStartTask: (task: Task) => runLifecycleAction(task, "start"),
    quickActionPendingId,
    quickActionsDisabled,
  };

  return (
    <div className="page today-page">
      <header className="today-header">
        <div className="today-heading-group">
          <h1 className="page-title">今日</h1>
          <div aria-label="日期切换" className="date-switcher">
            <button
              aria-label="前一天"
              onClick={() =>
                setDateKey((value) => shiftLocalDateKey(value, -1))
              }
              type="button"
            >
              <ChevronLeft size={13} />
            </button>
            <span className="date-pill">
              <CalendarDays size={13} />
              {isToday ? "今天 · " : null}
              {formatToday(selectedDate)}
            </span>
            <button
              aria-label="后一天"
              onClick={() => setDateKey((value) => shiftLocalDateKey(value, 1))}
              type="button"
            >
              <ChevronRight size={13} />
            </button>
            {!isToday ? (
              <button
                className="date-today-button"
                onClick={() => setDateKey(todayKey)}
                type="button"
              >
                回到今天
              </button>
            ) : null}
          </div>
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

      <section aria-label="收件箱概览" className="today-inbox-overview">
        <div className="section-heading compact-heading">
          <div>
            <span className="section-kicker">工作受理</span>
            <h2>收件箱</h2>
          </div>
          <span className="section-count">
            {inboxStatsQuery.data?.unread ?? 0} 未读
          </span>
        </div>
        {inboxStatsQuery.isPending ? (
          <SkeletonRows count={1} />
        ) : inboxStatsQuery.isError ? (
          <ErrorState
            compact
            message="无法读取收件箱概览。"
            onRetry={() => void inboxStatsQuery.refetch()}
          />
        ) : (
          <div className="today-inbox-grid">
            {inboxStats.map(({ icon: Icon, label, value, to, danger }) => (
              <Link
                className={`today-inbox-card${danger ? " danger" : ""}`}
                key={label}
                to={to}
              >
                <span>
                  <Icon size={15} /> {label}
                </span>
                <strong>{value}</strong>
                <ChevronRight aria-hidden="true" size={14} />
              </Link>
            ))}
          </div>
        )}
      </section>

      {taskGroupsQuery.isError ? (
        <ErrorState
          compact
          message="无法读取任务；请确认本地服务已启动后重试。"
          onRetry={() => void taskGroupsQuery.refetch()}
        />
      ) : null}

      {statsQuery.isError ? (
        <ErrorState
          compact
          message="无法读取今日统计；当前不显示估算数据。"
          onRetry={() => void statsQuery.refetch()}
        />
      ) : null}

      {dragMutation.isPending ||
      moveMutation.isPending ||
      resetOrderMutation.isPending ||
      reorderError ? (
        <div className="task-order-banner today-order-status">
          {dragMutation.isPending ||
          moveMutation.isPending ||
          resetOrderMutation.isPending ? (
            <span role="status">正在保存任务顺序…</span>
          ) : null}
          {reorderError ? (
            <span className="task-batch-error" role="alert">
              {reorderError}
            </span>
          ) : null}
        </div>
      ) : null}

      {lifecycleMutation.isPending ||
      createFocusMutation.isPending ||
      quickActionError ? (
        <div className="task-order-banner today-order-status">
          {lifecycleMutation.isPending ? (
            <span role="status">正在更新任务状态…</span>
          ) : null}
          {createFocusMutation.isPending ? (
            <span role="status">正在开始专注…</span>
          ) : null}
          {quickActionError ? (
            <span className="task-batch-error" role="alert">
              {quickActionError}
            </span>
          ) : null}
        </div>
      ) : null}

      {groups.overdue.length > 0 ? (
        <section className="task-section">
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker danger-text">需要处理</span>
              <h2>逾期计划</h2>
            </div>
            <span className="section-count">{groups.overdue.length}</span>
          </div>
          <TaskList
            {...taskQuickActionProps}
            compact
            live={live}
            onPlanTask={setPlanningTask}
            tasks={groups.overdue}
          />
        </section>
      ) : null}

      <section className="task-section">
        <div className="section-heading compact-heading">
          <div>
            <span className="section-kicker">
              {isToday ? "接下来" : "日期计划"}
            </span>
            <h2>{isToday ? "今天" : "所选日期"}</h2>
          </div>
          <div className="today-task-heading-actions">
            <span className="section-count">{groups.today.length}</span>
            {groups.today.length > 0 ? (
              <button
                className="button button-quiet today-reset-order"
                disabled={!reorderReady}
                onClick={() => resetPlanOrder(dateKey, todayOrderKey)}
                type="button"
              >
                <RotateCcw size={12} />
                恢复默认顺序
              </button>
            ) : null}
          </div>
        </div>
        {taskGroupsQuery.isPending ? (
          <SkeletonRows count={3} />
        ) : taskGroupsQuery.isSuccess && groups.today.length === 0 ? (
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
            message={`${isToday ? "今天" : "所选日期"}没有已安排的活动任务；可新建任务，或在任务页设置计划日期。`}
            title={isToday ? "今天已经安排妥当" : "所选日期暂无任务"}
          />
        ) : (
          <TaskList
            {...taskQuickActionProps}
            allowReorder
            allowDrag
            compact
            dragPending={dragMutation.isPending}
            live={reorderReady}
            onMove={(task, direction) => moveTask(task.id, dateKey, direction)}
            onPlanTask={setPlanningTask}
            reorderPendingId={
              moveMutation.isPending
                ? (moveMutation.variables?.taskId ?? null)
                : null
            }
            onDropTask={(source, target) =>
              dropTask(
                source,
                target,
                orderedTodayTasks,
                dateKey,
                todayOrderKey,
              )
            }
            tasks={orderedTodayTasks}
          />
        )}
      </section>

      {taskGroupsQuery.isPending || groups.thisWeek.length > 0 ? (
        <section className="task-section">
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker">稍后</span>
              <h2>本周</h2>
            </div>
            <span className="section-count">{groups.thisWeek.length}</span>
          </div>
          {taskGroupsQuery.isPending ? (
            <SkeletonRows count={2} />
          ) : (
            <TaskList
              {...taskQuickActionProps}
              compact
              live={live}
              onPlanTask={setPlanningTask}
              tasks={groups.thisWeek}
            />
          )}
        </section>
      ) : null}

      {groups.unscheduled.length > 0 ? (
        <section className="task-section">
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker">待安排</span>
              <h2>未排期</h2>
            </div>
            <div className="today-task-heading-actions">
              <span className="section-count">{groups.unscheduled.length}</span>
              <button
                className="button button-quiet today-reset-order"
                disabled={!reorderReady}
                onClick={() => resetPlanOrder(null, unscheduledOrderKey)}
                type="button"
              >
                <RotateCcw size={12} />
                恢复默认顺序
              </button>
            </div>
          </div>
          <TaskList
            {...taskQuickActionProps}
            allowReorder
            allowDrag
            compact
            dragPending={dragMutation.isPending}
            live={reorderReady}
            onMove={(task, direction) => moveTask(task.id, null, direction)}
            onPlanTask={setPlanningTask}
            reorderPendingId={
              moveMutation.isPending
                ? (moveMutation.variables?.taskId ?? null)
                : null
            }
            onDropTask={(source, target) =>
              dropTask(
                source,
                target,
                orderedUnscheduledTasks,
                null,
                unscheduledOrderKey,
              )
            }
            tasks={orderedUnscheduledTasks}
          />
        </section>
      ) : null}
      <TaskPlanModal
        onClose={() => setPlanningTask(null)}
        open={Boolean(planningTask)}
        selectedDate={dateKey}
        task={planningTask}
      />
    </div>
  );
}
