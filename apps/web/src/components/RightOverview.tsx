import {
  ArrowUpRight,
  CalendarDays,
  FileText,
  Flag,
  Pause,
  Play,
  RefreshCw,
  Zap,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  useActiveFocusSessionQuery,
  usePauseFocusSession,
  useRecentClientActivitiesQuery,
  useRoadmapMilestonesQuery,
  useResumeFocusSession,
} from "../api/hooks";
import {
  formatFocusTime,
  useBreakClock,
  useFocusClock,
  useFocusCycleStore,
} from "../store/focus";
import { useSettingsStore } from "../store/settings";

function localDateKey(date = new Date()) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function milestoneDateLabel(targetDate: string) {
  if (targetDate < localDateKey()) return "已逾期";
  if (targetDate === localDateKey()) return "今天";
  const [, month, day] = targetDate.split("-");
  return `${Number(month)}月${Number(day)}日`;
}

export function RightOverview() {
  const focusQuery = useActiveFocusSessionQuery();
  const recentActivitiesQuery = useRecentClientActivitiesQuery(3);
  const plannedMilestonesQuery = useRoadmapMilestonesQuery({
    page: 1,
    pageSize: 3,
    sort: "target_date",
    status: "planned",
  });
  const activeMilestonesQuery = useRoadmapMilestonesQuery({
    page: 1,
    pageSize: 3,
    sort: "target_date",
    status: "active",
  });
  const pauseFocus = usePauseFocusSession();
  const resumeFocus = useResumeFocusSession();
  const focusMinutes = useSettingsStore(
    (state) => state.preview?.focus.focusMinutes ?? state.focusMinutes,
  );
  const clock = useFocusClock(focusQuery.data);
  const breakClock = useBreakClock();
  const cyclePhase = useFocusCycleStore((state) => state.phase);
  const cycleTaskTitle = useFocusCycleStore((state) => state.taskTitle);
  const breakDurationSeconds = useFocusCycleStore(
    (state) => state.breakDurationSeconds,
  );
  const breakEndsAtMs = useFocusCycleStore((state) => state.breakEndsAtMs);
  const pauseBreak = useFocusCycleStore((state) => state.pauseBreak);
  const resumeBreak = useFocusCycleStore((state) => state.resumeBreak);
  const session = focusQuery.data?.session;
  const inBreak = !session && cyclePhase === "break";
  const plannedSeconds =
    session?.plannedSeconds ??
    (inBreak ? breakDurationSeconds : focusMinutes * 60);
  const remainingSeconds = session
    ? clock.remainingSeconds
    : inBreak
      ? breakClock.remainingSeconds
      : plannedSeconds;
  const ringOffset =
    326.7 *
    (1 - (session ? clock.progress : inBreak ? breakClock.progress : 0));
  const running = session?.status === "active";
  const paused = session?.status === "paused";
  const recoveryPending = session?.status === "recovery_pending";
  const busy = pauseFocus.isPending || resumeFocus.isPending;
  const upcomingMilestones = [
    ...(plannedMilestonesQuery.data?.items ?? []),
    ...(activeMilestonesQuery.data?.items ?? []),
  ]
    .sort(
      (left, right) =>
        left.targetDate.localeCompare(right.targetDate) ||
        left.id.localeCompare(right.id),
    )
    .slice(0, 3);
  const milestonesPending =
    plannedMilestonesQuery.isPending || activeMilestonesQuery.isPending;
  const milestonesError =
    plannedMilestonesQuery.isError || activeMilestonesQuery.isError;

  const toggle = () => {
    if (!session) return;
    const input = { id: session.id, expectedVersion: session.version };
    if (running) pauseFocus.mutate(input);
    if (paused) resumeFocus.mutate(input);
  };

  return (
    <aside className="right-sidebar" aria-label="今日概览">
      <section className="overview-card focus-overview">
        <div className="card-heading">
          <span>
            <Zap size={15} /> 专注模式
          </span>
          <span
            className={
              running
                ? "status-badge status-green"
                : recoveryPending
                  ? "status-badge status-red"
                  : inBreak
                    ? "status-badge status-green"
                    : "status-badge"
            }
          >
            {focusQuery.isPending
              ? "同步中"
              : running
                ? "进行中"
                : paused
                  ? "已暂停"
                  : recoveryPending
                    ? "待恢复"
                    : inBreak
                      ? breakEndsAtMs === null
                        ? "休息暂停"
                        : "休息中"
                      : cyclePhase === "ready"
                        ? "待下一块"
                        : cyclePhase === "complete"
                          ? "本轮完成"
                          : "待开始"}
          </span>
        </div>
        <div
          className="focus-ring"
          data-running={running}
          style={{ "--ring-offset": ringOffset } as React.CSSProperties}
        >
          <svg aria-hidden="true" viewBox="0 0 120 120">
            <circle className="ring-track" cx="60" cy="60" r="52" />
            <circle className="ring-value" cx="60" cy="60" r="52" />
          </svg>
          <div className="ring-copy">
            <strong>{formatFocusTime(remainingSeconds)}</strong>
            <span>/ {formatFocusTime(plannedSeconds)}</span>
          </div>
        </div>
        <div className="overview-focus-task">
          {session?.taskTitle ??
            (session
              ? "未绑定任务的专注"
              : (cycleTaskTitle ??
                (inBreak ? "未绑定任务的休息" : "尚未选择专注任务")))}
        </div>

        {focusQuery.isError ? (
          <button
            className="button button-secondary button-full"
            onClick={() => void focusQuery.refetch()}
            type="button"
          >
            <RefreshCw size={14} /> 重试读取
          </button>
        ) : running || paused ? (
          <button
            className="button button-secondary button-full"
            disabled={busy}
            onClick={toggle}
            type="button"
          >
            {running ? <Pause size={14} /> : <Play size={14} />}
            {running ? "暂停计时" : "继续专注"}
          </button>
        ) : inBreak ? (
          <button
            className="button button-secondary button-full"
            onClick={() =>
              breakEndsAtMs === null ? resumeBreak() : pauseBreak()
            }
            type="button"
          >
            {breakEndsAtMs === null ? <Play size={14} /> : <Pause size={14} />}
            {breakEndsAtMs === null ? "开始休息" : "暂停休息"}
          </button>
        ) : (
          <Link className="button button-secondary button-full" to="/focus">
            <Play size={14} />
            {recoveryPending
              ? "处理上次会话"
              : cyclePhase === "ready"
                ? "开始下一块"
                : cyclePhase === "complete"
                  ? "查看本轮"
                  : "选择任务并开始"}
          </Link>
        )}
      </section>

      <section className="overview-card">
        <div className="card-heading">
          <span>临近项目节点</span>
          <Link className="text-link" to="/roadmap">
            查看全部
          </Link>
        </div>
        {milestonesPending ? (
          <div className="overview-footnote overview-activity-state">
            正在读取路线图节点…
          </div>
        ) : milestonesError ? (
          <button
            className="overview-activity-retry"
            onClick={() =>
              void Promise.all([
                plannedMilestonesQuery.refetch(),
                activeMilestonesQuery.refetch(),
              ])
            }
            type="button"
          >
            <RefreshCw size={12} /> 节点读取失败，重试
          </button>
        ) : upcomingMilestones.length === 0 ? (
          <div className="overview-footnote overview-activity-state">
            暂无未完成的路线图节点
          </div>
        ) : (
          <div className="milestone-overview-list">
            {upcomingMilestones.map((milestone) => {
              const projectLabel =
                milestone.projects.length === 0
                  ? "未关联项目"
                  : `${milestone.projects[0].name}${
                      milestone.projects.length > 1
                        ? ` +${milestone.projects.length - 1}`
                        : ""
                    }`;
              const taskLabel =
                milestone.taskSummary.total === 0
                  ? "暂无关联任务"
                  : `${milestone.taskSummary.completed}/${milestone.taskSummary.total} 任务`;
              return (
                <Link
                  aria-label={`查看路线图节点：${milestone.title}`}
                  className="milestone-overview-row"
                  key={milestone.id}
                  to="/roadmap"
                >
                  <span
                    className="activity-icon activity-blue"
                    aria-hidden="true"
                  >
                    <Flag size={13} />
                  </span>
                  <span className="milestone-overview-copy">
                    <strong>{milestone.title}</strong>
                    <span>
                      {projectLabel} · {taskLabel}
                    </span>
                  </span>
                  <time
                    className={
                      milestone.targetDate < localDateKey() ? "is-overdue" : ""
                    }
                    dateTime={milestone.targetDate}
                  >
                    {milestoneDateLabel(milestone.targetDate)}
                  </time>
                </Link>
              );
            })}
          </div>
        )}
      </section>

      <section className="overview-card">
        <div className="overview-label">本月收入</div>
        <div className="income-number-row">
          <strong>¥0</strong>
        </div>
        <div className="overview-footnote">暂无收入记录</div>
        <Link className="overview-link" to="/income">
          查看收入 <ArrowUpRight size={13} />
        </Link>
      </section>

      <section className="overview-card">
        <div className="card-heading">
          <span>客户动态</span>
          <Link className="text-link" to="/clients">
            查看全部
          </Link>
        </div>
        {recentActivitiesQuery.isPending ? (
          <div className="overview-footnote overview-activity-state">
            正在读取本地动态…
          </div>
        ) : recentActivitiesQuery.isError ? (
          <button
            className="overview-activity-retry"
            onClick={() => void recentActivitiesQuery.refetch()}
            type="button"
          >
            <RefreshCw size={12} /> 读取失败，重试
          </button>
        ) : recentActivitiesQuery.data.items.length === 0 ? (
          <div className="overview-footnote overview-activity-state">
            暂无客户动态
          </div>
        ) : (
          <div className="activity-list">
            {recentActivitiesQuery.data.items.map((activity) => (
              <Link
                aria-label={`查看客户 ${activity.clientName}：${activity.title}`}
                className="activity-row"
                key={activity.id}
                to={`/clients/${activity.clientId}`}
              >
                <span
                  className={`activity-icon ${
                    activity.kind === "meeting"
                      ? "activity-green"
                      : "activity-blue"
                  }`}
                  aria-hidden="true"
                >
                  {activity.kind === "meeting" ? (
                    <CalendarDays size={13} />
                  ) : (
                    <FileText size={13} />
                  )}
                </span>
                <span className="activity-copy">
                  <strong>{activity.clientName}</strong> · {activity.title}
                </span>
                <time dateTime={activity.occurredAt}>
                  {new Intl.DateTimeFormat("zh-CN", {
                    month: "numeric",
                    day: "numeric",
                  }).format(new Date(activity.occurredAt))}
                </time>
              </Link>
            ))}
          </div>
        )}
      </section>
    </aside>
  );
}
