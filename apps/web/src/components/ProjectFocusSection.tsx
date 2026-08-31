import {
  BarChart3,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  History,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { CSSProperties } from "react";
import { ApiError } from "../api/client";
import { useFocusReportQuery, useFocusSessionHistoryQuery } from "../api/hooks";
import {
  localDateFromKey,
  localDateKey,
  useLocalCalendar,
} from "../lib/localCalendar";
import { useSettledPage } from "../lib/useSettledPage";
import { formatFocusTime } from "../store/focus";
import type { FocusSessionStatus } from "../types/models";
import { ErrorState, LoadingState } from "./feedback";

interface ProjectFocusSectionProps {
  projectId: string;
}

type ProjectFocusRangeKind = "seven_days" | "thirty_days" | "month";

const historyPageSize = 6;

function addLocalDays(date: Date, days: number): Date {
  const result = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  result.setDate(result.getDate() + days);
  return result;
}

function projectFocusRange(kind: ProjectFocusRangeKind, now = new Date()) {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const dateTo = localDateKey(today);
  if (kind === "month") {
    return {
      dateFrom: localDateKey(
        new Date(today.getFullYear(), today.getMonth(), 1),
      ),
      dateTo,
      label: "本月",
    };
  }
  const days = kind === "seven_days" ? 7 : 30;
  return {
    dateFrom: localDateKey(addLocalDays(today, 1 - days)),
    dateTo,
    label: `最近 ${days} 天`,
  };
}

function reportDayLabel(date: string, dayCount: number): string {
  const parsed = new Date(`${date}T12:00:00`);
  if (Number.isNaN(parsed.getTime())) return date;
  if (dayCount > 14) {
    return new Intl.DateTimeFormat("zh-CN", {
      month: "numeric",
      day: "numeric",
    }).format(parsed);
  }
  return new Intl.DateTimeFormat("zh-CN", { weekday: "short" }).format(parsed);
}

function historyStatusLabel(status: FocusSessionStatus): string {
  if (status === "completed") return "已完成";
  if (status === "cancelled") return "已取消";
  return "意外中断";
}

function historyEndTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function totalDurationLabel(seconds: number): string {
  const safeSeconds = Math.max(0, Math.floor(seconds));
  const hours = Math.floor(safeSeconds / 3600);
  const minutes = Math.floor((safeSeconds % 3600) / 60);
  const remainingSeconds = safeSeconds % 60;
  if (hours > 0) {
    return `${hours} 小时${minutes > 0 ? ` ${minutes} 分钟` : ""}`;
  }
  if (minutes > 0) {
    return `${minutes} 分钟${remainingSeconds > 0 ? ` ${remainingSeconds} 秒` : ""}`;
  }
  return `${remainingSeconds} 秒`;
}

function queryError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return fallback;
}

export function ProjectFocusSection({ projectId }: ProjectFocusSectionProps) {
  const { dateKey: todayKey, timeZone: timezone } = useLocalCalendar();
  const [rangeKind, setRangeKind] =
    useState<ProjectFocusRangeKind>("seven_days");
  const [historyPage, setHistoryPage] = useState(1);
  const selectedRange = useMemo(
    () => projectFocusRange(rangeKind, localDateFromKey(todayKey)),
    [rangeKind, todayKey],
  );
  const report = useFocusReportQuery({
    dateFrom: selectedRange.dateFrom,
    dateTo: selectedRange.dateTo,
    timezone,
    projectId,
  });
  const history = useFocusSessionHistoryQuery({
    page: historyPage,
    pageSize: historyPageSize,
    status: "terminal",
    projectId,
  });
  const historyTotal = history.data?.meta.total ?? 0;
  const historyPageCount = Math.max(
    1,
    Math.ceil(historyTotal / (history.data?.meta.pageSize ?? historyPageSize)),
  );
  const maxDailySeconds = Math.max(
    ...(report.data?.days.map((day) => day.seconds) ?? []),
    1,
  );

  useEffect(() => setHistoryPage(1), [projectId]);

  useSettledPage({
    page: historyPage,
    meta: history.data?.meta,
    isFetching: history.isFetching,
    isPlaceholderData: history.isPlaceholderData,
    isSuccess: history.isSuccess,
    setPage: setHistoryPage,
  });

  return (
    <section
      aria-labelledby="project-focus-heading"
      className="project-detail-section project-focus-section"
    >
      <div className="project-detail-heading">
        <div>
          <h2 id="project-focus-heading">项目专注</h2>
          <p>
            按 Session 绑定 Task 的“当前项目归属”统计；Task
            后续移入或移出项目时，历史会随当前归属重新分类。
          </p>
        </div>
      </div>

      <div className="project-focus-grid">
        <div
          aria-busy={report.isFetching}
          aria-labelledby="project-focus-trend-heading"
          className="focus-panel project-focus-panel"
        >
          <div className="focus-panel-heading">
            <div>
              <span className="eyebrow">{selectedRange.label}</span>
              <h2 id="project-focus-trend-heading">专注趋势</h2>
            </div>
            <BarChart3 aria-hidden="true" size={18} />
          </div>
          <div className="focus-report-range" aria-label="项目专注统计范围">
            {(
              [
                ["seven_days", "7 天"],
                ["thirty_days", "30 天"],
                ["month", "本月"],
              ] as Array<[ProjectFocusRangeKind, string]>
            ).map(([kind, label]) => (
              <button
                aria-pressed={rangeKind === kind}
                className={
                  rangeKind === kind
                    ? "focus-report-range-button is-active"
                    : "focus-report-range-button"
                }
                key={kind}
                onClick={() => setRangeKind(kind)}
                type="button"
              >
                {label}
              </button>
            ))}
          </div>

          {report.isPending ? (
            <LoadingState label="正在统计项目专注记录…" />
          ) : report.isError ? (
            <ErrorState
              compact
              message={queryError(
                report.error,
                "项目专注统计暂时不可用；项目其他内容不受影响。",
              )}
              onRetry={() => void report.refetch()}
            />
          ) : report.data ? (
            <>
              <div className="focus-report-metrics">
                <div>
                  <span>总专注时长</span>
                  <strong>
                    {totalDurationLabel(report.data.totals.seconds)}
                  </strong>
                </div>
                <div>
                  <span>已完成 Session</span>
                  <strong>{report.data.totals.sessions}</strong>
                </div>
                <div>
                  <span>当前连续天数</span>
                  <strong>{report.data.currentStreakDays} 天</strong>
                </div>
                <div>
                  <span>最长连续天数</span>
                  <strong>{report.data.longestStreakDays} 天</strong>
                </div>
              </div>
              {report.data.totals.seconds === 0 ? (
                <div className="focus-empty-inline">
                  <CalendarDays aria-hidden="true" size={18} />
                  <span>{selectedRange.label}还没有已完成的专注记录。</span>
                </div>
              ) : (
                <div
                  aria-label="项目每日专注趋势"
                  className="focus-bars"
                  style={
                    {
                      "--focus-days": report.data.days.length,
                    } as CSSProperties
                  }
                >
                  {report.data.days.map((day) => (
                    <div
                      aria-label={`${day.date}，${day.minutes} 分钟，${day.sessions} 个已完成 Session`}
                      className="focus-bar-column"
                      key={day.date}
                    >
                      <span>
                        {day.seconds
                          ? day.minutes > 0
                            ? day.minutes
                            : "<1"
                          : ""}
                      </span>
                      <div>
                        <i
                          style={{
                            height: `${Math.max(
                              (day.seconds / maxDailySeconds) * 100,
                              day.seconds ? 8 : 2,
                            )}%`,
                          }}
                        />
                      </div>
                      <small>
                        {reportDayLabel(day.date, report.data.days.length)}
                      </small>
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : null}
        </div>

        <div
          aria-busy={history.isFetching}
          aria-labelledby="project-focus-history-heading"
          className="focus-panel project-focus-panel"
        >
          <div className="focus-panel-heading">
            <div>
              <span className="eyebrow">终态记录</span>
              <h2 id="project-focus-history-heading">Session 历史</h2>
            </div>
            <History aria-hidden="true" size={18} />
          </div>
          {history.isPending ? (
            <LoadingState label="正在读取项目 Session 历史…" />
          ) : history.isError ? (
            <ErrorState
              compact
              message={queryError(
                history.error,
                "项目 Session 历史暂时不可用；项目其他内容不受影响。",
              )}
              onRetry={() => void history.refetch()}
            />
          ) : history.data?.items.length ? (
            <>
              <div className="focus-history-list">
                {history.data.items.map((session) => (
                  <article key={session.id}>
                    <span
                      className="focus-history-status"
                      data-status={session.status}
                    >
                      {historyStatusLabel(session.status)}
                    </span>
                    <div>
                      <strong>{session.taskTitle ?? "任务不可用"}</strong>
                      <span>
                        结束于{" "}
                        {historyEndTime(session.endedAt ?? session.updatedAt)}
                      </span>
                    </div>
                    <b title="Session 实际累计时长">
                      记录 {formatFocusTime(session.accumulatedSeconds)}
                    </b>
                  </article>
                ))}
              </div>
              {historyTotal >
              (history.data.meta.pageSize ?? historyPageSize) ? (
                <div className="focus-history-pagination">
                  <button
                    aria-label="上一页项目 Session 历史"
                    className="icon-button"
                    disabled={historyPage <= 1 || history.isFetching}
                    onClick={() =>
                      setHistoryPage((page) => Math.max(1, page - 1))
                    }
                    type="button"
                  >
                    <ChevronLeft size={15} />
                  </button>
                  <span>
                    {historyPage} / {historyPageCount} · 共 {historyTotal} 条
                  </span>
                  <button
                    aria-label="下一页项目 Session 历史"
                    className="icon-button"
                    disabled={
                      historyPage >= historyPageCount || history.isFetching
                    }
                    onClick={() => setHistoryPage((page) => page + 1)}
                    type="button"
                  >
                    <ChevronRight size={15} />
                  </button>
                </div>
              ) : null}
            </>
          ) : (
            <div className="focus-empty-inline">
              <History aria-hidden="true" size={18} />
              <span>当前项目归属下还没有终态 Session。</span>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
