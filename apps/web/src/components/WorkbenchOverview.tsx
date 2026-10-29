import {
  ArrowUpRight,
  CalendarDays,
  FileText,
  Flag,
  RefreshCw,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  useIncomeStatsQuery,
  useRecentClientActivitiesQuery,
  useRoadmapMilestonesQuery,
} from "../api/hooks";
import {
  localDateFromKey,
  localDateKey,
  useLocalCalendar,
} from "../lib/localCalendar";

function currentMonthBounds(dateKey: string) {
  const date = localDateFromKey(dateKey);
  const firstDay = new Date(date.getFullYear(), date.getMonth(), 1);
  const lastDay = new Date(date.getFullYear(), date.getMonth() + 1, 0);
  return { dateFrom: localDateKey(firstDay), dateTo: localDateKey(lastDay) };
}

function formatCny(amountMinor: number) {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency: "CNY",
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

function milestoneDateLabel(targetDate: string, todayKey: string) {
  if (targetDate < todayKey) return "已逾期";
  if (targetDate === todayKey) return "今天";
  const [, month, day] = targetDate.split("-");
  return `${Number(month)}月${Number(day)}日`;
}

export function WorkbenchOverview() {
  const { dateKey: todayKey } = useLocalCalendar();
  const incomeStatsQuery = useIncomeStatsQuery({
    currency: "CNY",
    ...currentMonthBounds(todayKey),
  });
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

  return (
    <aside className="right-sidebar workbench-overview" aria-label="工作台概览">
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
                  to={`/roadmap?milestone=${encodeURIComponent(milestone.id)}`}
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
                      milestone.targetDate < todayKey ? "is-overdue" : ""
                    }
                    dateTime={milestone.targetDate}
                  >
                    {milestoneDateLabel(milestone.targetDate, todayKey)}
                  </time>
                </Link>
              );
            })}
          </div>
        )}
      </section>

      <section className="overview-card">
        <div className="overview-label">本月收入</div>
        {incomeStatsQuery.isPending ? (
          <div className="overview-footnote">正在读取本月收入…</div>
        ) : incomeStatsQuery.isError ? (
          <button
            className="overview-activity-retry"
            onClick={() => void incomeStatsQuery.refetch()}
            type="button"
          >
            <RefreshCw size={12} /> 收入读取失败，重试
          </button>
        ) : (
          <>
            <div className="income-number-row">
              <strong>
                {formatCny(incomeStatsQuery.data.confirmedIncomeMinor)}
              </strong>
            </div>
            <div className="overview-footnote">
              {incomeStatsQuery.data.entryCount === 0
                ? "暂无收入记录"
                : `${incomeStatsQuery.data.confirmedIncomeCount} 笔已确认`}
            </div>
          </>
        )}
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
                to={`/clients/${activity.clientId}?activity=${activity.id}`}
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
