import {
  AlertTriangle,
  CalendarDays,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  ClipboardCheck,
  Clock3,
  GitBranch,
  Hourglass,
  Inbox as InboxIcon,
  List,
  Plus,
  RotateCcw,
  Target,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import { Link } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useInboxStatsQuery,
  useInboxItemsQuery,
  useActiveFocusSessionQuery,
  useCreateFocusSession,
  useMoveTaskAcrossPlans,
  useMoveTaskWithinPlan,
  useResetTaskOrder,
  useTaskPageQuery,
  useTaskLifecycleCommand,
  useTodayTaskGroupsQuery,
  useTodayStatsQuery,
} from "../api/hooks";
import { useFocusCycleStore } from "../store/focus";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { TaskDeleteConfirmModal } from "../components/TaskDeleteConfirmModal";
import { TaskList } from "../components/TaskList";
import { TaskPlanModal } from "../components/TaskPlanModal";
import {
  localDateFromKey,
  localDateKey,
  useLocalCalendar,
} from "../lib/localCalendar";
import type { InboxItem, Task } from "../types/models";

function formatToday(date: Date): string {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "long",
    day: "numeric",
    weekday: "short",
  }).format(date);
}

function formatInboxDueAt(value: string | null): string {
  if (!value) return "时间待确认";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "时间待确认";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function clientFollowupClientId(item: InboxItem): string | null {
  if (item.sourceEntityType !== "client_followup") return null;
  const clientId = item.payloadJson.client_id;
  return typeof clientId === "string" && clientId.trim() ? clientId : null;
}

function dateFromKey(dateKey: string): Date {
  return localDateFromKey(dateKey);
}

function shiftLocalDateKey(dateKey: string, days: number): string {
  const date = dateFromKey(dateKey);
  date.setDate(date.getDate() + days);
  return localDateKey(date);
}

function isValidLocalDateKey(dateKey: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateKey)) return false;
  const date = dateFromKey(dateKey);
  return !Number.isNaN(date.getTime()) && localDateKey(date) === dateKey;
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

interface TodayTaskGroups {
  overdue: Task[];
  today: Task[];
  thisWeek: Task[];
  unscheduled: Task[];
}

const todayGroupKeys = [
  "overdue",
  "today",
  "thisWeek",
  "unscheduled",
] as const satisfies readonly (keyof TodayTaskGroups)[];

type DueRiskFilter = "overdue" | "due_soon";

const riskPageSize = 20;

function previewTaskDrop(
  groups: TodayTaskGroups,
  source: Task,
  target: Task,
): { groups: TodayTaskGroups; position: "before" | "after" } | null {
  const sourceGroup = todayGroupKeys.find((key) =>
    groups[key].some((task) => task.id === source.id),
  );
  const targetGroup = todayGroupKeys.find((key) =>
    groups[key].some((task) => task.id === target.id),
  );
  if (!sourceGroup || !targetGroup) return null;
  const sourceIndex = groups[sourceGroup].findIndex(
    (task) => task.id === source.id,
  );
  const targetIndex = groups[targetGroup].findIndex(
    (task) => task.id === target.id,
  );
  const position =
    sourceGroup === targetGroup && sourceIndex < targetIndex
      ? "after"
      : "before";
  const next: TodayTaskGroups = {
    overdue: groups.overdue.filter((task) => task.id !== source.id),
    today: groups.today.filter((task) => task.id !== source.id),
    thisWeek: groups.thisWeek.filter((task) => task.id !== source.id),
    unscheduled: groups.unscheduled.filter((task) => task.id !== source.id),
  };
  const destination = [...next[targetGroup]];
  const nextTargetIndex = destination.findIndex(
    (task) => task.id === target.id,
  );
  destination.splice(nextTargetIndex + (position === "after" ? 1 : 0), 0, {
    ...source,
    plannedDate: target.plannedDate,
  });
  next[targetGroup] = destination;
  return { groups: next, position };
}

function previewTaskDropToGroup(
  groups: TodayTaskGroups,
  source: Task,
  targetGroup: keyof TodayTaskGroups,
  targetPlannedDate: string | null,
): TodayTaskGroups {
  const next: TodayTaskGroups = {
    overdue: groups.overdue.filter((task) => task.id !== source.id),
    today: groups.today.filter((task) => task.id !== source.id),
    thisWeek: groups.thisWeek.filter((task) => task.id !== source.id),
    unscheduled: groups.unscheduled.filter((task) => task.id !== source.id),
  };
  next[targetGroup] = [
    ...next[targetGroup],
    { ...source, plannedDate: targetPlannedDate },
  ];
  return next;
}

export function TodayPage() {
  const setNewTaskOpen = useUiStore((state) => state.setNewTaskOpen);
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const { dateKey: todayKey } = useLocalCalendar();
  const [dateKey, setDateKey] = useState(todayKey);
  const previousTodayKey = useRef(todayKey);
  const [riskFilter, setRiskFilter] = useState<DueRiskFilter | null>(null);
  const [riskPage, setRiskPage] = useState(1);
  const [planningTask, setPlanningTask] = useState<Task | null>(null);
  const [deletingTask, setDeletingTask] = useState<Task | null>(null);
  const selectedDate = dateFromKey(dateKey);
  const isToday = dateKey === todayKey;
  const taskGroupsQuery = useTodayTaskGroupsQuery(dateKey);
  const statsQuery = useTodayStatsQuery(dateKey);
  const riskTasksQuery = useTaskPageQuery(
    {
      page: riskPage,
      pageSize: riskPageSize,
      status: "active",
      dueState: riskFilter ?? undefined,
      sort: "due_date",
    },
    riskFilter !== null,
  );
  const inboxStatsQuery = useInboxStatsQuery();
  const clientFollowupsQuery = useInboxItemsQuery(
    {
      view: "inbox",
      sourceEntityType: "client_followup",
      page: 1,
      pageSize: 5,
    },
    isToday,
  );
  const focusQuery = useActiveFocusSessionQuery();
  const lifecycleMutation = useTaskLifecycleCommand();
  const createFocusMutation = useCreateFocusSession();
  const moveMutation = useMoveTaskWithinPlan();
  const crossPlanMutation = useMoveTaskAcrossPlans();
  const resetOrderMutation = useResetTaskOrder();
  const [sharedDraggingTask, setSharedDraggingTask] = useState<Task | null>(
    null,
  );
  const [dragPreview, setDragPreview] = useState<TodayTaskGroups | null>(null);
  const focusMinutes = useSettingsStore((state) => state.focusMinutes);
  const focusCycles = useSettingsStore((state) => state.cycles);
  const focusPhase = useFocusCycleStore((state) => state.phase);
  const beginFocusWork = useFocusCycleStore((state) => state.beginWork);

  const riskTotal = riskTasksQuery.data?.meta.total ?? 0;
  const effectiveRiskPageSize = Math.max(
    1,
    riskTasksQuery.data?.meta.pageSize ?? riskPageSize,
  );
  const riskPageCount = Math.max(
    1,
    Math.ceil(riskTotal / effectiveRiskPageSize),
  );
  const riskPageOutOfRange = riskTotal > 0 && riskPage > riskPageCount;
  const riskListReady =
    riskFilter !== null &&
    riskTasksQuery.isSuccess &&
    !riskTasksQuery.isFetching &&
    !riskTasksQuery.isPlaceholderData;

  useEffect(() => {
    if (
      riskFilter !== null &&
      riskTasksQuery.isSuccess &&
      !riskTasksQuery.isPlaceholderData &&
      riskPage > riskPageCount
    ) {
      setRiskPage(riskPageCount);
    }
  }, [
    riskFilter,
    riskPage,
    riskPageCount,
    riskTasksQuery.isPlaceholderData,
    riskTasksQuery.isSuccess,
  ]);
  const live = taskGroupsQuery.isSuccess;
  const orderMutationPending =
    moveMutation.isPending ||
    crossPlanMutation.isPending ||
    resetOrderMutation.isPending;
  const dateNavigationLocked =
    orderMutationPending || sharedDraggingTask !== null;
  const riskSwitchDisabled = dateNavigationLocked;
  const changeDate = useCallback(
    (nextDateKey: string): boolean => {
      if (dateNavigationLocked || !isValidLocalDateKey(nextDateKey)) {
        return false;
      }
      if (nextDateKey === dateKey) return true;
      setRiskFilter(null);
      setRiskPage(1);
      setDragPreview(null);
      setSharedDraggingTask(null);
      moveMutation.reset();
      crossPlanMutation.reset();
      resetOrderMutation.reset();
      setDateKey(nextDateKey);
      return true;
    },
    [
      crossPlanMutation.reset,
      dateKey,
      dateNavigationLocked,
      moveMutation.reset,
      resetOrderMutation.reset,
    ],
  );

  useEffect(() => {
    const previousToday = previousTodayKey.current;
    if (todayKey === previousToday) return;
    if (dateKey !== previousToday) {
      previousTodayKey.current = todayKey;
      return;
    }
    if (dateNavigationLocked) return;
    if (changeDate(todayKey)) previousTodayKey.current = todayKey;
  }, [changeDate, dateKey, dateNavigationLocked, todayKey]);

  const reorderReady =
    live && !taskGroupsQuery.isFetching && !orderMutationPending;
  const reorderError =
    reorderErrorText(crossPlanMutation.error) ??
    reorderErrorText(moveMutation.error) ??
    reorderErrorText(resetOrderMutation.error);
  const crossPlanWarning = crossPlanMutation.data?.orderWarnings.length
    ? `任务已改期；${crossPlanMutation.data.orderWarnings.join("、")}，当前已刷新为服务端实际顺序。`
    : null;
  const quickActionError =
    quickActionErrorText(lifecycleMutation.error) ??
    quickActionErrorText(createFocusMutation.error);
  const quickActionPendingId = lifecycleMutation.isPending
    ? (lifecycleMutation.variables?.id ?? null)
    : createFocusMutation.isPending
      ? (createFocusMutation.variables?.taskId ?? null)
      : null;
  const quickActionsDisabled =
    lifecycleMutation.isPending ||
    createFocusMutation.isPending ||
    orderMutationPending ||
    (riskFilter !== null
      ? !riskListReady
      : taskGroupsQuery.isFetching || !reorderReady);
  const focusActionDisabled =
    !focusQuery.isSuccess ||
    focusQuery.isFetching ||
    Boolean(focusQuery.data?.session) ||
    focusPhase !== "idle";
  const serverGroups = taskGroupsQuery.data ?? {
    overdue: [],
    today: [],
    thisWeek: [],
    unscheduled: [],
  };
  const groups = dragPreview ?? serverGroups;
  const realStats = statsQuery.data;
  const estimated = realStats
    ? Math.round((realStats.tasks.estimatedMinutes / 60) * 10) / 10
    : 0;
  const statsPending = statsQuery.isPending;
  const statsUnavailable = statsQuery.isError && !realStats;
  const statValue = (value: string | number) =>
    statsPending ? "…" : statsUnavailable ? "—" : value;
  const stats = [
    {
      icon: Hourglass,
      value: statValue(`${estimated}h`),
      label: "预计时长",
    },
    {
      icon: CheckCircle2,
      value: statValue(realStats?.tasks.remaining ?? 0),
      label: "项待完成",
    },
    {
      icon: AlertTriangle,
      value: statValue(realStats?.tasks.overdue ?? 0),
      label: "项已逾期",
      danger: true,
      risk: "overdue" as const,
    },
    {
      icon: Clock3,
      value: statValue(realStats?.tasks.dueSoon ?? 0),
      label: "项临期",
      warning: true,
      risk: "due_soon" as const,
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
  const dueClientFollowups = (clientFollowupsQuery.data?.items ?? []).flatMap(
    (item) => {
      const clientId = clientFollowupClientId(item);
      return clientId ? [{ item, clientId }] : [];
    },
  );
  const clientFollowupTotal = clientFollowupsQuery.data?.meta.total ?? 0;
  const moveTask = (
    taskId: string,
    plannedDate: string | null,
    direction: "up" | "down",
  ) => {
    crossPlanMutation.reset();
    resetOrderMutation.reset();
    moveMutation.mutate({
      taskId,
      plannedDate,
      direction,
      scope: "active",
    });
  };
  const resetPlanOrder = (plannedDate: string | null) => {
    setDragPreview(null);
    crossPlanMutation.reset();
    moveMutation.reset();
    resetOrderMutation.mutate(plannedDate);
  };
  const dropTask = (source: Task, target: Task) => {
    const preview = previewTaskDrop(serverGroups, source, target);
    if (!preview) return;
    moveMutation.reset();
    resetOrderMutation.reset();
    crossPlanMutation.reset();
    setDragPreview(preview.groups);
    crossPlanMutation.mutate(
      {
        source,
        target,
        targetPlannedDate: target.plannedDate,
        position: preview.position,
      },
      { onSettled: () => setDragPreview(null) },
    );
  };
  const dropTaskToGroup = (
    source: Task,
    targetGroup: "today" | "unscheduled",
    targetPlannedDate: string | null,
  ) => {
    moveMutation.reset();
    resetOrderMutation.reset();
    crossPlanMutation.reset();
    setDragPreview(
      previewTaskDropToGroup(
        serverGroups,
        source,
        targetGroup,
        targetPlannedDate,
      ),
    );
    crossPlanMutation.mutate(
      {
        source,
        target: null,
        targetPlannedDate,
        position: "end",
      },
      { onSettled: () => setDragPreview(null) },
    );
  };
  const acceptGroupDrag = (event: DragEvent<HTMLElement>) => {
    if (!sharedDraggingTask || crossPlanMutation.isPending) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  };
  const runLifecycleAction = (task: Task, action: "start" | "complete") => {
    createFocusMutation.reset();
    crossPlanMutation.reset();
    lifecycleMutation.reset();
    lifecycleMutation.mutate({
      id: task.id,
      input: { action, expectedVersion: task.version },
    });
  };
  const startFocus = (task: Task) => {
    lifecycleMutation.reset();
    createFocusMutation.reset();
    crossPlanMutation.reset();
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
    focusActionDisabled:
      focusActionDisabled || (riskFilter !== null && !riskListReady),
    onDeleteTask: setDeletingTask,
    onEditTask: (task: Task) => setTaskDetailId(task.id),
    onCompleteTask: (task: Task) => runLifecycleAction(task, "complete"),
    onStartFocus: startFocus,
    onStartTask: (task: Task) => runLifecycleAction(task, "start"),
    quickActionPendingId,
    quickActionsDisabled,
  };
  const toggleRiskFilter = (next: DueRiskFilter) => {
    if (riskSwitchDisabled) return;
    setRiskPage(1);
    setDragPreview(null);
    setSharedDraggingTask(null);
    setRiskFilter((current) => (current === next ? null : next));
  };

  return (
    <div className="page today-page">
      <header className="today-header">
        <div className="today-heading-group">
          <h1 className="page-title">今日</h1>
          <div aria-label="日期切换" className="date-switcher">
            <button
              aria-label="前一天"
              disabled={dateNavigationLocked}
              onClick={() => changeDate(shiftLocalDateKey(dateKey, -1))}
              type="button"
            >
              <ChevronLeft size={13} />
            </button>
            <span
              className={`date-pill date-picker-pill${dateNavigationLocked ? " date-picker-pill-disabled" : ""}`}
            >
              <CalendarDays size={13} />
              <span>
                {isToday ? "今天 · " : null}
                {formatToday(selectedDate)}
              </span>
              <input
                aria-label="选择日期"
                disabled={dateNavigationLocked}
                onChange={(event) => changeDate(event.currentTarget.value)}
                title={
                  dateNavigationLocked
                    ? "任务顺序保存或拖拽期间不能切换日期"
                    : "选择日期"
                }
                type="date"
                value={dateKey}
              />
            </span>
            <button
              aria-label="后一天"
              disabled={dateNavigationLocked}
              onClick={() => changeDate(shiftLocalDateKey(dateKey, 1))}
              type="button"
            >
              <ChevronRight size={13} />
            </button>
            {!isToday ? (
              <button
                className="date-today-button"
                disabled={dateNavigationLocked}
                onClick={() => changeDate(todayKey)}
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
        {stats.map(({ icon: Icon, value, label, danger, warning, risk }) => {
          const className = `stat-card${danger ? " stat-card-danger" : ""}${warning ? " stat-card-warning" : ""}${risk ? " stat-card-button" : ""}${riskFilter === risk ? " stat-card-selected" : ""}`;
          const content = (
            <>
              <span className="stat-icon">
                <Icon size={15} />
              </span>
              <strong>{value}</strong>
              <span>{label}</span>
            </>
          );
          return risk ? (
            <button
              aria-pressed={riskFilter === risk}
              className={className}
              disabled={riskSwitchDisabled}
              key={label}
              onClick={() => toggleRiskFilter(risk)}
              type="button"
            >
              {content}
            </button>
          ) : (
            <div className={className} key={label}>
              {content}
            </div>
          );
        })}
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

      {isToday ? (
        <section aria-label="待办客户回访" className="today-followups-overview">
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker">关系维护</span>
              <h2>待办客户回访</h2>
            </div>
            <span className="section-count">
              {clientFollowupsQuery.isPending
                ? "…"
                : `${clientFollowupTotal} 项回访`}
            </span>
          </div>
          {clientFollowupsQuery.isPending ? (
            <SkeletonRows count={2} />
          ) : clientFollowupsQuery.isError ? (
            <ErrorState
              compact
              message="无法读取到期客户回访。"
              onRetry={() => void clientFollowupsQuery.refetch()}
            />
          ) : dueClientFollowups.length === 0 ? (
            <p className="today-followups-empty">目前没有到期的客户回访。</p>
          ) : (
            <div className="today-followups-list">
              {dueClientFollowups.map(({ item, clientId }) => (
                <Link
                  aria-label={`查看客户回访：${item.title}`}
                  className="today-followup-card"
                  key={item.id}
                  to={`/clients/${clientId}`}
                >
                  <span className="today-followup-copy">
                    <strong>{item.title}</strong>
                    <small>
                      {formatInboxDueAt(item.dueAt)} · {item.summary}
                    </small>
                  </span>
                  <ChevronRight aria-hidden="true" size={14} />
                </Link>
              ))}
              {clientFollowupTotal > dueClientFollowups.length ? (
                <Link className="today-followups-more" to="/inbox">
                  其余 {clientFollowupTotal - dueClientFollowups.length}{" "}
                  项在收件箱
                  <ChevronRight aria-hidden="true" size={13} />
                </Link>
              ) : null}
            </div>
          )}
        </section>
      ) : null}

      {riskFilter === null && taskGroupsQuery.isError ? (
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

      {crossPlanMutation.isPending ||
      moveMutation.isPending ||
      resetOrderMutation.isPending ||
      reorderError ||
      crossPlanWarning ? (
        <div className="task-order-banner today-order-status">
          {crossPlanMutation.isPending ||
          moveMutation.isPending ||
          resetOrderMutation.isPending ? (
            <span role="status">正在保存任务顺序…</span>
          ) : null}
          {reorderError ? (
            <span className="task-batch-error" role="alert">
              {reorderError}
            </span>
          ) : null}
          {crossPlanWarning ? (
            <span className="task-batch-warning" role="status">
              {crossPlanWarning}
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

      {riskFilter !== null ? (
        <section
          aria-labelledby="today-risk-heading"
          className="task-section today-risk-section"
        >
          <div className="section-heading compact-heading">
            <div>
              <span
                className={`section-kicker${riskFilter === "overdue" ? " danger-text" : ""}`}
              >
                截止风险
              </span>
              <h2 id="today-risk-heading">
                {riskFilter === "overdue" ? "逾期任务" : "临期任务"}
              </h2>
            </div>
            <div className="today-task-heading-actions">
              <span className="section-count">
                {riskTasksQuery.data ? `共 ${riskTotal} 项` : "数量待加载"}
              </span>
              <button
                className="button button-quiet today-clear-risk"
                disabled={riskSwitchDisabled}
                onClick={() => toggleRiskFilter(riskFilter)}
                type="button"
              >
                清除筛选
              </button>
            </div>
          </div>
          <p className="today-risk-description">
            {riskFilter === "overdue"
              ? "显示全部仍处于活动状态且已经逾期的任务，按截止时间排序。"
              : "显示全部仍处于活动状态且将在 24 小时内到期的任务，按截止时间排序。"}
          </p>
          {riskTasksQuery.isPending ||
          riskTasksQuery.isPlaceholderData ||
          riskPageOutOfRange ? (
            <SkeletonRows count={3} />
          ) : riskTasksQuery.isError ? (
            <ErrorState
              compact
              message={`无法读取${riskFilter === "overdue" ? "逾期" : "临期"}任务。`}
              onRetry={() => void riskTasksQuery.refetch()}
            />
          ) : riskTasksQuery.isSuccess && riskTotal === 0 ? (
            <EmptyState
              message={`当前没有活动的${riskFilter === "overdue" ? "逾期" : "临期"}任务。`}
              title={riskFilter === "overdue" ? "没有逾期任务" : "没有临期任务"}
            />
          ) : riskTasksQuery.isSuccess ? (
            <>
              {riskTasksQuery.isFetching ? (
                <p className="today-risk-refresh" role="status">
                  正在刷新截止风险…
                </p>
              ) : null}
              <TaskList
                {...taskQuickActionProps}
                compact
                live={riskListReady}
                onPlanTask={setPlanningTask}
                tasks={riskTasksQuery.data.items}
              />
              {riskPageCount > 1 ? (
                <nav
                  aria-label="截止风险任务分页"
                  className="pagination today-risk-pagination"
                >
                  <button
                    className="button button-secondary"
                    disabled={riskPage <= 1 || riskTasksQuery.isFetching}
                    onClick={() =>
                      setRiskPage((value) => Math.max(1, value - 1))
                    }
                    type="button"
                  >
                    上一页
                  </button>
                  <span>
                    {(riskPage - 1) * effectiveRiskPageSize + 1}–
                    {Math.min(
                      (riskPage - 1) * effectiveRiskPageSize +
                        riskTasksQuery.data.items.length,
                      riskTotal,
                    )}{" "}
                    / {riskTotal}
                  </span>
                  <button
                    className="button button-secondary"
                    disabled={
                      riskPage >= riskPageCount || riskTasksQuery.isFetching
                    }
                    onClick={() =>
                      setRiskPage((value) => Math.min(riskPageCount, value + 1))
                    }
                    type="button"
                  >
                    下一页
                  </button>
                </nav>
              ) : null}
            </>
          ) : null}
        </section>
      ) : null}

      {riskFilter === null && groups.overdue.length > 0 ? (
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
            allowDrag
            compact
            dragPending={crossPlanMutation.isPending}
            live={reorderReady}
            onDropTask={dropTask}
            onPlanTask={setPlanningTask}
            onSharedDraggingTaskChange={(task) => {
              if (task) crossPlanMutation.reset();
              setSharedDraggingTask(task);
            }}
            sharedDraggingTask={sharedDraggingTask}
            tasks={groups.overdue}
          />
        </section>
      ) : null}

      <section
        className={`task-section${sharedDraggingTask ? " task-section-drag-target" : ""}`}
        hidden={riskFilter !== null}
        onDragOver={acceptGroupDrag}
        onDrop={(event) => {
          event.preventDefault();
          if (sharedDraggingTask)
            dropTaskToGroup(sharedDraggingTask, "today", dateKey);
          setSharedDraggingTask(null);
        }}
      >
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
                onClick={() => resetPlanOrder(dateKey)}
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
        ) : sharedDraggingTask && groups.today.length === 0 ? (
          <div className="task-group-drop-target">
            拖到此处安排到{isToday ? "今天" : "所选日期"}
          </div>
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
            dragPending={crossPlanMutation.isPending}
            live={reorderReady}
            onMove={(task, direction) => moveTask(task.id, dateKey, direction)}
            onPlanTask={setPlanningTask}
            reorderPendingId={
              moveMutation.isPending
                ? (moveMutation.variables?.taskId ?? null)
                : null
            }
            onDropTask={dropTask}
            onSharedDraggingTaskChange={(task) => {
              if (task) crossPlanMutation.reset();
              setSharedDraggingTask(task);
            }}
            sharedDraggingTask={sharedDraggingTask}
            tasks={groups.today}
          />
        )}
      </section>

      {riskFilter === null &&
      (taskGroupsQuery.isPending || groups.thisWeek.length > 0) ? (
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
              allowDrag
              compact
              dragPending={crossPlanMutation.isPending}
              live={reorderReady}
              onDropTask={dropTask}
              onPlanTask={setPlanningTask}
              onSharedDraggingTaskChange={(task) => {
                if (task) crossPlanMutation.reset();
                setSharedDraggingTask(task);
              }}
              sharedDraggingTask={sharedDraggingTask}
              tasks={groups.thisWeek}
            />
          )}
        </section>
      ) : null}

      {riskFilter === null &&
      (groups.unscheduled.length > 0 || sharedDraggingTask) ? (
        <section
          className={`task-section${sharedDraggingTask ? " task-section-drag-target" : ""}`}
          onDragOver={acceptGroupDrag}
          onDrop={(event) => {
            event.preventDefault();
            if (sharedDraggingTask)
              dropTaskToGroup(sharedDraggingTask, "unscheduled", null);
            setSharedDraggingTask(null);
          }}
        >
          <div className="section-heading compact-heading">
            <div>
              <span className="section-kicker">待安排</span>
              <h2>未排期</h2>
            </div>
            <div className="today-task-heading-actions">
              <span className="section-count">{groups.unscheduled.length}</span>
              {groups.unscheduled.length > 0 ? (
                <button
                  className="button button-quiet today-reset-order"
                  disabled={!reorderReady}
                  onClick={() => resetPlanOrder(null)}
                  type="button"
                >
                  <RotateCcw size={12} />
                  恢复默认顺序
                </button>
              ) : null}
            </div>
          </div>
          {groups.unscheduled.length === 0 ? (
            <div className="task-group-drop-target">拖到此处设为未排期</div>
          ) : (
            <TaskList
              {...taskQuickActionProps}
              allowReorder
              allowDrag
              compact
              dragPending={crossPlanMutation.isPending}
              live={reorderReady}
              onMove={(task, direction) => moveTask(task.id, null, direction)}
              onPlanTask={setPlanningTask}
              reorderPendingId={
                moveMutation.isPending
                  ? (moveMutation.variables?.taskId ?? null)
                  : null
              }
              onDropTask={dropTask}
              onSharedDraggingTaskChange={(task) => {
                if (task) crossPlanMutation.reset();
                setSharedDraggingTask(task);
              }}
              sharedDraggingTask={sharedDraggingTask}
              tasks={groups.unscheduled}
            />
          )}
        </section>
      ) : null}
      <TaskPlanModal
        onClose={() => setPlanningTask(null)}
        open={Boolean(planningTask)}
        selectedDate={dateKey}
        task={planningTask}
      />
      <TaskDeleteConfirmModal
        onClose={() => setDeletingTask(null)}
        task={deletingTask}
      />
    </div>
  );
}
