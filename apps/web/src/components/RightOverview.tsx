import { ArrowUpRight, Pause, Play, RefreshCw, Zap } from "lucide-react";
import { Link } from "react-router-dom";
import {
  useActiveFocusSessionQuery,
  usePauseFocusSession,
  useResumeFocusSession,
} from "../api/hooks";
import {
  formatFocusTime,
  useBreakClock,
  useFocusClock,
  useFocusCycleStore,
} from "../store/focus";
import { useSettingsStore } from "../store/settings";

export function RightOverview() {
  const focusQuery = useActiveFocusSessionQuery();
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
        <div className="overview-footnote">暂无客户动态</div>
      </section>
    </aside>
  );
}
