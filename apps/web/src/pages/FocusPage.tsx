import {
  Ban,
  BarChart3,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock3,
  History,
  Pause,
  Play,
  RotateCcw,
  Settings2,
  ShieldCheck,
  SkipForward,
  Square,
} from "lucide-react";
import { useMemo, useState } from "react";
import type { FocusReportHour, FocusReportParams } from "../types/models";
import { ApiError } from "../api/client";
import {
  useActiveFocusSessionQuery,
  useCancelFocusSession,
  useCreateFocusSession,
  useFocusReportQuery,
  useFocusSessionHistoryQuery,
  usePauseFocusSession,
  useResumeFocusSession,
  useStopFocusSession,
  useTaskOptionsQuery,
  useTodayStatsQuery,
} from "../api/hooks";
import { PageHeader } from "../components/PageHeader";
import { ErrorState, LoadingState } from "../components/feedback";
import {
  formatFocusTime,
  useBreakClock,
  useFocusClock,
  useFocusCycleStore,
} from "../store/focus";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";

function localDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function focusHourLabel(hour: number): string {
  const nextHour = (hour + 1) % 24;
  return `${String(hour).padStart(2, "0")}:00–${String(nextHour).padStart(2, "0")}:00`;
}

function bestFocusHour(hours: FocusReportHour[]): FocusReportHour | null {
  return hours.reduce<FocusReportHour | null>(
    (best, item) =>
      item.seconds > 0 && (!best || item.seconds > best.seconds) ? item : best,
    null,
  );
}

const focusWeekdayLabels = [
  "周一",
  "周二",
  "周三",
  "周四",
  "周五",
  "周六",
  "周日",
];

function recentDateRange(now = new Date()) {
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const start = new Date(end);
  start.setDate(start.getDate() - 6);
  return { dateFrom: localDateKey(start), dateTo: localDateKey(end) };
}

type FocusReportRangeKind = "seven_days" | "thirty_days" | "month" | "custom";

interface FocusReportRange extends FocusReportParams {
  label: string;
}

const maxFocusReportDays = 93;

function addLocalDays(date: Date, days: number): Date {
  const result = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  result.setDate(result.getDate() + days);
  return result;
}

function focusReportRange(
  kind: Exclude<FocusReportRangeKind, "custom">,
  now = new Date(),
): FocusReportRange {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const dateTo = localDateKey(today);
  if (kind === "month") {
    return {
      dateFrom: localDateKey(
        new Date(today.getFullYear(), today.getMonth(), 1),
      ),
      dateTo,
      timezone: "",
      label: "本月",
    };
  }
  const days = kind === "seven_days" ? 7 : 30;
  return {
    dateFrom: localDateKey(addLocalDays(today, 1 - days)),
    dateTo,
    timezone: "",
    label: `最近 ${days} 天`,
  };
}

function localDateDistance(dateFrom: string, dateTo: string): number | null {
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(dateFrom) ||
    !/^\d{4}-\d{2}-\d{2}$/.test(dateTo)
  ) {
    return null;
  }
  const from = new Date(`${dateFrom}T12:00:00`);
  const to = new Date(`${dateTo}T12:00:00`);
  if (
    Number.isNaN(from.getTime()) ||
    Number.isNaN(to.getTime()) ||
    localDateKey(from) !== dateFrom ||
    localDateKey(to) !== dateTo
  ) {
    return null;
  }
  return Math.round((to.getTime() - from.getTime()) / 86_400_000) + 1;
}

function focusReportDayLabel(date: string, days: number): string {
  const parsed = new Date(`${date}T12:00:00`);
  if (days > 14) {
    return new Intl.DateTimeFormat("zh-CN", {
      month: "numeric",
      day: "numeric",
    }).format(parsed);
  }
  return new Intl.DateTimeFormat("zh-CN", { weekday: "short" }).format(parsed);
}

function focusHistoryStatus(status: string): string {
  if (status === "completed") return "已完成";
  if (status === "cancelled") return "已取消";
  return "意外中断";
}

function focusHistoryTime(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

function focusError(...errors: unknown[]): string | null {
  const error = errors.find(Boolean);
  if (!error) return null;
  return error instanceof ApiError
    ? error.message
    : "专注操作失败，请刷新本地状态后重试。";
}

export function FocusPage() {
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const previewFocusMinutes = useSettingsStore(
    (state) => state.preview?.focus.focusMinutes ?? state.focusMinutes,
  );
  const committedFocusMinutes = useSettingsStore((state) => state.focusMinutes);
  const committedCycles = useSettingsStore((state) => state.cycles);
  const focusQuery = useActiveFocusSessionQuery();
  const taskOptions = useTaskOptionsQuery();
  const createFocus = useCreateFocusSession();
  const pauseFocus = usePauseFocusSession();
  const resumeFocus = useResumeFocusSession();
  const stopFocus = useStopFocusSession();
  const cancelFocus = useCancelFocusSession();
  const todayStats = useTodayStatsQuery(localDateKey(new Date()));
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const initialReportRange = useMemo(() => recentDateRange(), []);
  const [historyPage, setHistoryPage] = useState(1);
  const [reportRangeKind, setReportRangeKind] =
    useState<FocusReportRangeKind>("seven_days");
  const [customReportRange, setCustomReportRange] =
    useState(initialReportRange);
  const selectedReportRange = useMemo(() => {
    if (reportRangeKind === "custom") {
      return { ...customReportRange, timezone, label: "自定义范围" };
    }
    return { ...focusReportRange(reportRangeKind), timezone };
  }, [customReportRange, reportRangeKind, timezone]);
  const customRangeDays = localDateDistance(
    customReportRange.dateFrom,
    customReportRange.dateTo,
  );
  const customRangeError =
    reportRangeKind !== "custom"
      ? null
      : customRangeDays === null
        ? "请选择有效的起止日期。"
        : customRangeDays < 1
          ? "结束日期不能早于开始日期。"
          : customRangeDays > maxFocusReportDays
            ? `自定义范围最多 ${maxFocusReportDays} 天。`
            : null;
  const focusReport = useFocusReportQuery(
    selectedReportRange,
    !customRangeError,
  );
  const focusHistory = useFocusSessionHistoryQuery({
    page: historyPage,
    pageSize: 6,
    status: "terminal",
  });
  const cyclePhase = useFocusCycleStore((state) => state.phase);
  const cycleTaskId = useFocusCycleStore((state) => state.taskId);
  const completedCycles = useFocusCycleStore((state) => state.completedCycles);
  const targetCycles = useFocusCycleStore((state) => state.targetCycles);
  const breakEndsAtMs = useFocusCycleStore((state) => state.breakEndsAtMs);
  const beginWork = useFocusCycleStore((state) => state.beginWork);
  const pauseBreak = useFocusCycleStore((state) => state.pauseBreak);
  const resumeBreak = useFocusCycleStore((state) => state.resumeBreak);
  const finishBreak = useFocusCycleStore((state) => state.finishBreak);
  const resetCycle = useFocusCycleStore((state) => state.resetCycle);
  const breakClock = useBreakClock();
  const [selectedTaskId, setSelectedTaskId] = useState("");
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [confirmUnbound, setConfirmUnbound] = useState(false);
  const snapshot = focusQuery.data;
  const session = snapshot?.session;
  const clock = useFocusClock(snapshot);
  const availableTasks = useMemo(
    () =>
      (taskOptions.data ?? []).filter((task) => task.status !== "cancelled"),
    [taskOptions.data],
  );
  const cycleTask = availableTasks.find((task) => task.id === cycleTaskId);
  const inBreak = !session && cyclePhase === "break";
  const readyForNext = !session && cyclePhase === "ready";
  const sequenceComplete = !session && cyclePhase === "complete";
  const idlePlannedSeconds = previewFocusMinutes * 60;
  const remainingSeconds = session
    ? clock.remainingSeconds
    : inBreak
      ? breakClock.remainingSeconds
      : idlePlannedSeconds;
  const progress = session ? clock.progress : inBreak ? breakClock.progress : 0;
  const busy =
    createFocus.isPending ||
    pauseFocus.isPending ||
    resumeFocus.isPending ||
    stopFocus.isPending ||
    cancelFocus.isPending;
  const actionError = focusError(
    createFocus.error,
    pauseFocus.error,
    resumeFocus.error,
    stopFocus.error,
    cancelFocus.error,
  );

  const startWork = () => {
    const continuing = readyForNext;
    const taskId = continuing ? cycleTaskId : selectedTaskId || null;
    if (!continuing && taskId === null && !confirmUnbound) {
      setConfirmUnbound(true);
      return;
    }
    const cycles = continuing ? targetCycles : committedCycles;
    const taskTitle = taskId
      ? (availableTasks.find((task) => task.id === taskId)?.title ??
        (continuing ? cycleTask?.title : null))
      : null;
    setConfirmCancel(false);
    createFocus.mutate(
      {
        taskId,
        plannedSeconds: committedFocusMinutes * 60,
      },
      {
        onSuccess: () => {
          setConfirmUnbound(false);
          beginWork(taskId, cycles, taskTitle);
        },
      },
    );
  };

  const command = (action: "pause" | "resume" | "stop" | "cancel") => {
    if (!session) return;
    const input = { id: session.id, expectedVersion: session.version };
    if (action === "pause") pauseFocus.mutate(input);
    if (action === "resume") resumeFocus.mutate(input);
    if (action === "stop") {
      stopFocus.mutate(input, { onSuccess: resetCycle });
    }
    if (action === "cancel") {
      cancelFocus.mutate(input, {
        onSuccess: () => {
          setConfirmCancel(false);
          resetCycle();
        },
      });
    }
  };

  const statusLabel = session
    ? session.status === "active"
      ? "正在专注"
      : session.status === "paused"
        ? "已暂停"
        : "等待恢复确认"
    : inBreak
      ? breakEndsAtMs === null
        ? "休息已暂停"
        : "休息中"
      : readyForNext
        ? "准备下一块"
        : sequenceComplete
          ? "本轮完成"
          : "准备开始";

  return (
    <div className="page focus-page">
      <PageHeader
        actions={
          <button
            className="button button-secondary"
            onClick={() => setSettingsOpen(true, "focus")}
            type="button"
          >
            <Settings2 size={15} />
            专注设置
          </button>
        }
        meta={
          <span className="page-count">
            {cyclePhase === "idle"
              ? `${previewFocusMinutes} 分钟`
              : `${Math.min(completedCycles + (session ? 1 : 0), targetCycles)} / ${targetCycles} 专注块`}
          </span>
        }
        title="专注"
      />

      {focusQuery.isPending ? (
        <LoadingState label="正在恢复本地专注状态…" />
      ) : focusQuery.isError ? (
        <ErrorState
          message="无法读取专注会话；不会在本地猜测或继续计时。"
          onRetry={() => void focusQuery.refetch()}
        />
      ) : (
        <>
          <section className="focus-stage">
            <div
              className="focus-stage-ring"
              data-running={
                session?.status === "active" ||
                (inBreak && breakEndsAtMs !== null)
              }
              style={
                {
                  "--focus-progress": `${progress * 360}deg`,
                } as React.CSSProperties
              }
            >
              <div>
                <strong>{formatFocusTime(remainingSeconds)}</strong>
                <span>{statusLabel}</span>
              </div>
            </div>

            {session ? (
              <>
                <span className="focus-project">本地 Focus Session</span>
                <h2>{session.taskTitle ?? "未绑定任务的专注"}</h2>
                <p>
                  计划 {formatFocusTime(session.plannedSeconds)} · 已确认{" "}
                  {formatFocusTime(clock.elapsedSeconds)}
                </p>
                {session.status !== "recovery_pending" ? (
                  <div className="focus-controls">
                    {session.status === "active" ? (
                      <button
                        className="button button-primary focus-primary"
                        disabled={busy}
                        onClick={() => command("pause")}
                        type="button"
                      >
                        <Pause size={17} /> 暂停
                      </button>
                    ) : (
                      <button
                        className="button button-primary focus-primary"
                        disabled={busy}
                        onClick={() => command("resume")}
                        type="button"
                      >
                        <Play size={17} /> 继续
                      </button>
                    )}
                    <button
                      className="button button-secondary"
                      disabled={busy}
                      onClick={() => command("stop")}
                      type="button"
                    >
                      <Square size={15} /> 提前结束并记入工时
                    </button>
                    <button
                      className={
                        confirmCancel
                          ? "button button-danger"
                          : "button button-quiet"
                      }
                      disabled={busy}
                      onClick={() => {
                        if (confirmCancel) command("cancel");
                        else setConfirmCancel(true);
                      }}
                      type="button"
                    >
                      <Ban size={15} />
                      {confirmCancel ? "确认取消且不记工时" : "取消本次"}
                    </button>
                  </div>
                ) : (
                  <p className="focus-recovery-inline">
                    请先在恢复对话框中确认中断间隔的处理方式。
                  </p>
                )}
              </>
            ) : inBreak ? (
              <>
                <span className="focus-project">休息阶段 · 不计入工时</span>
                <h2>第 {completedCycles} 个专注块已完成</h2>
                <p>下一块仍关联：{cycleTask?.title ?? "未绑定任务的专注"}</p>
                <div className="focus-controls">
                  <button
                    className="button button-primary focus-primary"
                    onClick={() =>
                      breakEndsAtMs === null ? resumeBreak() : pauseBreak()
                    }
                    type="button"
                  >
                    {breakEndsAtMs === null ? (
                      <Play size={17} />
                    ) : (
                      <Pause size={17} />
                    )}
                    {breakEndsAtMs === null ? "开始休息" : "暂停休息"}
                  </button>
                  <button
                    className="button button-secondary"
                    onClick={finishBreak}
                    type="button"
                  >
                    <SkipForward size={15} /> 跳过休息
                  </button>
                </div>
              </>
            ) : readyForNext ? (
              <>
                <span className="focus-project">下一次专注</span>
                <h2>{cycleTask?.title ?? "未绑定任务的专注"}</h2>
                <p>
                  已完成 {completedCycles} / {targetCycles}{" "}
                  个专注块；下一块使用已保存的 {committedFocusMinutes}{" "}
                  分钟设置。
                </p>
                <div className="focus-controls">
                  <button
                    className="button button-primary focus-primary"
                    disabled={busy}
                    onClick={startWork}
                    type="button"
                  >
                    <Play size={17} /> 开始下一块
                  </button>
                  <button
                    className="button button-quiet"
                    onClick={resetCycle}
                    type="button"
                  >
                    结束本轮
                  </button>
                </div>
              </>
            ) : sequenceComplete ? (
              <>
                <span className="focus-project">本轮专注</span>
                <h2>{targetCycles} 个专注块已完成</h2>
                <p>所有工作阶段均已写入本地 Session，休息时长未计入工时。</p>
                <div className="focus-controls">
                  <button
                    className="button button-primary focus-primary"
                    onClick={resetCycle}
                    type="button"
                  >
                    <RotateCcw size={17} /> 开始新一轮
                  </button>
                </div>
              </>
            ) : (
              <>
                <span className="focus-project">下一次专注</span>
                <h2>选择任务后开始</h2>
                <div className="focus-start-form">
                  <label htmlFor="focus-task">关联任务</label>
                  <select
                    disabled={taskOptions.isPending || busy}
                    id="focus-task"
                    onChange={(event) => {
                      setSelectedTaskId(event.target.value);
                      setConfirmUnbound(false);
                    }}
                    value={selectedTaskId}
                  >
                    <option value="">不绑定任务</option>
                    {availableTasks.map((task) => (
                      <option key={task.id} value={task.id}>
                        {task.title}
                      </option>
                    ))}
                  </select>
                  <span>
                    当前预览 {previewFocusMinutes}{" "}
                    分钟；真正开始时只读取已保存的 {committedFocusMinutes}{" "}
                    分钟设置。
                  </span>
                </div>
                {taskOptions.isError ? (
                  <ErrorState
                    compact
                    message="任务列表读取失败；仍可确认后开始不绑定任务的专注。"
                    onRetry={() => void taskOptions.refetch()}
                  />
                ) : null}
                <div className="focus-controls">
                  <button
                    className="button button-primary focus-primary"
                    disabled={busy}
                    onClick={startWork}
                    type="button"
                  >
                    <Play size={17} />
                    {createFocus.isPending
                      ? "正在开始…"
                      : confirmUnbound && !selectedTaskId
                        ? "确认不绑定任务并开始"
                        : "开始专注"}
                  </button>
                </div>
              </>
            )}
            {actionError ? (
              <p className="form-error focus-action-error" role="alert">
                {actionError}
              </p>
            ) : null}
          </section>

          <section className="focus-summary-grid" aria-label="今日专注统计">
            <article>
              <Clock3 size={16} />
              <span>今日完成</span>
              <strong>{todayStats.data?.focus.sessions ?? "—"} 个专注块</strong>
            </article>
            <article>
              <ShieldCheck size={16} />
              <span>今日已确认</span>
              <strong>{todayStats.data?.focus.minutes ?? "—"} 分钟</strong>
            </article>
          </section>
          {todayStats.isError ? (
            <ErrorState
              compact
              message="今日专注统计暂时不可用，会话计时不受影响。"
              onRetry={() => void todayStats.refetch()}
            />
          ) : null}

          <section className="focus-insights" aria-label="专注回顾">
            <div className="focus-panel focus-report-panel">
              <div className="focus-panel-heading">
                <div>
                  <span className="eyebrow">{selectedReportRange.label}</span>
                  <h2>专注趋势</h2>
                </div>
                <BarChart3 size={18} />
              </div>
              <div className="focus-report-range" aria-label="专注回顾范围">
                {(
                  [
                    ["seven_days", "7 天"],
                    ["thirty_days", "30 天"],
                    ["month", "本月"],
                    ["custom", "自定义"],
                  ] as Array<[FocusReportRangeKind, string]>
                ).map(([kind, label]) => (
                  <button
                    aria-pressed={reportRangeKind === kind}
                    className={
                      reportRangeKind === kind
                        ? "focus-report-range-button is-active"
                        : "focus-report-range-button"
                    }
                    key={kind}
                    onClick={() => setReportRangeKind(kind)}
                    type="button"
                  >
                    {label}
                  </button>
                ))}
              </div>
              {reportRangeKind === "custom" ? (
                <div className="focus-report-custom-range">
                  <label>
                    开始日期
                    <input
                      aria-label="专注回顾开始日期"
                      max={customReportRange.dateTo}
                      onChange={(event) =>
                        setCustomReportRange((current) => ({
                          ...current,
                          dateFrom: event.target.value,
                        }))
                      }
                      type="date"
                      value={customReportRange.dateFrom}
                    />
                  </label>
                  <label>
                    结束日期
                    <input
                      aria-label="专注回顾结束日期"
                      min={customReportRange.dateFrom}
                      onChange={(event) =>
                        setCustomReportRange((current) => ({
                          ...current,
                          dateTo: event.target.value,
                        }))
                      }
                      type="date"
                      value={customReportRange.dateTo}
                    />
                  </label>
                  <span>
                    {customRangeDays && customRangeDays > 0
                      ? `${customRangeDays} 天`
                      : "等待有效范围"}
                  </span>
                </div>
              ) : null}
              {customRangeError ? (
                <p className="form-error focus-report-range-error" role="alert">
                  {customRangeError}
                </p>
              ) : focusReport.isPending ? (
                <LoadingState label="正在统计本地专注记录…" />
              ) : focusReport.isError ? (
                <ErrorState
                  compact
                  message="本地专注统计暂时不可用。"
                  onRetry={() => void focusReport.refetch()}
                />
              ) : focusReport.data ? (
                <>
                  <div className="focus-report-metrics">
                    <div>
                      <span>专注块</span>
                      <strong>{focusReport.data.totals.sessions}</strong>
                    </div>
                    <div>
                      <span>已确认</span>
                      <strong>{focusReport.data.totals.minutes} 分钟</strong>
                    </div>
                    <div>
                      <span>连续专注</span>
                      <strong>{focusReport.data.currentStreakDays} 天</strong>
                    </div>
                    <div>
                      <span>本期最长</span>
                      <strong>{focusReport.data.longestStreakDays} 天</strong>
                    </div>
                  </div>
                  {focusReport.data.totals.seconds === 0 ? (
                    <div className="focus-empty-inline">
                      <CalendarDays size={18} />
                      <span>
                        {selectedReportRange.label}还没有已完成的专注记录。
                      </span>
                    </div>
                  ) : (
                    <>
                      <div
                        className="focus-bars"
                        aria-label="每日专注分钟数"
                        style={
                          {
                            "--focus-days": focusReport.data.days.length,
                          } as React.CSSProperties
                        }
                      >
                        {focusReport.data.days.map((day) => {
                          const maxMinutes = Math.max(
                            ...focusReport.data.days.map(
                              (item) => item.minutes,
                            ),
                            1,
                          );
                          return (
                            <div className="focus-bar-column" key={day.date}>
                              <span>{day.minutes || ""}</span>
                              <div>
                                <i
                                  style={{
                                    height: `${Math.max((day.minutes / maxMinutes) * 100, day.minutes ? 8 : 2)}%`,
                                  }}
                                />
                              </div>
                              <small>
                                {focusReportDayLabel(
                                  day.date,
                                  focusReport.data.days.length,
                                )}
                              </small>
                            </div>
                          );
                        })}
                      </div>
                      <div
                        aria-label="项目专注时间分布"
                        className="focus-project-distribution"
                      >
                        <div className="focus-project-distribution-heading">
                          <strong>项目分布</strong>
                          <span>按任务当前归属统计</span>
                        </div>
                        {focusReport.data.projects.map((project) => (
                          <article key={project.projectId ?? "unassigned"}>
                            <div>
                              <strong>
                                {project.projectName ?? "未归项目"}
                              </strong>
                              <span>
                                {project.sessions} 个专注块 · {project.minutes}{" "}
                                分钟
                              </span>
                            </div>
                            <div className="focus-project-distribution-track">
                              <i
                                style={{
                                  width: `${Math.max(
                                    (project.seconds /
                                      focusReport.data.totals.seconds) *
                                      100,
                                    2,
                                  )}%`,
                                }}
                              />
                            </div>
                          </article>
                        ))}
                      </div>
                      <div
                        aria-label="每日时段专注分布"
                        className="focus-hour-distribution"
                      >
                        <div className="focus-hour-distribution-heading">
                          <strong>时段分布</strong>
                          <span>
                            {bestFocusHour(focusReport.data.hours)
                              ? `最佳 ${focusHourLabel(
                                  bestFocusHour(focusReport.data.hours)!.hour,
                                )}`
                              : "暂无有效时段"}
                          </span>
                        </div>
                        <div className="focus-hour-grid">
                          {focusReport.data.hours.map((hour) => {
                            const maxSeconds = Math.max(
                              ...focusReport.data.hours.map(
                                (item) => item.seconds,
                              ),
                              1,
                            );
                            return (
                              <div
                                aria-label={`${focusHourLabel(hour.hour)}，${hour.minutes} 分钟，${hour.sessions} 个专注块`}
                                key={hour.hour}
                                title={`${focusHourLabel(hour.hour)} · ${hour.minutes} 分钟 · ${hour.sessions} 个专注块`}
                              >
                                <i
                                  style={{
                                    opacity: hour.seconds
                                      ? 0.25 +
                                        (hour.seconds / maxSeconds) * 0.75
                                      : 0.08,
                                  }}
                                />
                                <small>
                                  {hour.hour % 6 === 0
                                    ? String(hour.hour).padStart(2, "0")
                                    : ""}
                                </small>
                              </div>
                            );
                          })}
                        </div>
                      </div>
                      <div
                        aria-label="周几与小时专注热力图"
                        className="focus-heatmap"
                      >
                        <div className="focus-heatmap-heading">
                          <strong>专注热力图</strong>
                          <span>按当地星期与小时汇总</span>
                        </div>
                        <div className="focus-heatmap-scroll">
                          <div className="focus-heatmap-grid">
                            <span aria-hidden="true" />
                            {focusReport.data.hours.map((hour) => (
                              <small key={hour.hour}>
                                {hour.hour % 3 === 0
                                  ? String(hour.hour).padStart(2, "0")
                                  : ""}
                              </small>
                            ))}
                            {focusWeekdayLabels.map((label, weekdayIndex) => (
                              <div className="focus-heatmap-row" key={label}>
                                <strong>{label}</strong>
                                {focusReport.data.heatmap
                                  .slice(
                                    weekdayIndex * 24,
                                    (weekdayIndex + 1) * 24,
                                  )
                                  .map((cell) => {
                                    const maxSeconds = Math.max(
                                      ...focusReport.data.heatmap.map(
                                        (item) => item.seconds,
                                      ),
                                      1,
                                    );
                                    return (
                                      <i
                                        aria-label={`${label} ${focusHourLabel(cell.hour)}，${cell.minutes} 分钟，${cell.sessions} 个专注块`}
                                        key={cell.hour}
                                        style={{
                                          opacity: cell.seconds
                                            ? 0.2 +
                                              (cell.seconds / maxSeconds) * 0.8
                                            : 0.06,
                                        }}
                                        title={`${label} ${focusHourLabel(cell.hour)} · ${cell.minutes} 分钟 · ${cell.sessions} 个专注块`}
                                      />
                                    );
                                  })}
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    </>
                  )}
                </>
              ) : null}
            </div>

            <div className="focus-panel focus-history-panel">
              <div className="focus-panel-heading">
                <div>
                  <span className="eyebrow">本地记录</span>
                  <h2>最近专注</h2>
                </div>
                <History size={18} />
              </div>
              {focusHistory.isPending ? (
                <LoadingState label="正在读取专注历史…" />
              ) : focusHistory.isError ? (
                <ErrorState
                  compact
                  message="专注历史暂时不可用。"
                  onRetry={() => void focusHistory.refetch()}
                />
              ) : focusHistory.data?.items.length ? (
                <>
                  <div className="focus-history-list">
                    {focusHistory.data.items.map((item) => (
                      <article key={item.id}>
                        <span
                          className="focus-history-status"
                          data-status={item.status}
                        >
                          {focusHistoryStatus(item.status)}
                        </span>
                        <div>
                          <strong>{item.taskTitle ?? "未绑定任务"}</strong>
                          <span>
                            {focusHistoryTime(item.endedAt ?? item.updatedAt)}
                          </span>
                        </div>
                        <b>{formatFocusTime(item.accumulatedSeconds)}</b>
                      </article>
                    ))}
                  </div>
                  {focusHistory.data.meta.total >
                  focusHistory.data.meta.pageSize ? (
                    <div className="focus-history-pagination">
                      <button
                        aria-label="上一页专注历史"
                        className="icon-button"
                        disabled={historyPage === 1}
                        onClick={() => setHistoryPage((page) => page - 1)}
                        type="button"
                      >
                        <ChevronLeft size={15} />
                      </button>
                      <span>
                        {historyPage} /{" "}
                        {Math.ceil(
                          focusHistory.data.meta.total /
                            focusHistory.data.meta.pageSize,
                        )}
                      </span>
                      <button
                        aria-label="下一页专注历史"
                        className="icon-button"
                        disabled={
                          historyPage >=
                          Math.ceil(
                            focusHistory.data.meta.total /
                              focusHistory.data.meta.pageSize,
                          )
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
                  <History size={18} />
                  <span>完成一次专注后，记录会出现在这里。</span>
                </div>
              )}
            </div>
          </section>
        </>
      )}

      <section className="focus-note">
        <ShieldCheck size={17} />
        <div>
          <strong>本地事实计时</strong>
          <p>
            页面切换、刷新和系统挂起后都按 Sidecar
            的绝对时间恢复；取消与休息均不累计任务工时。
          </p>
        </div>
      </section>
    </div>
  );
}
