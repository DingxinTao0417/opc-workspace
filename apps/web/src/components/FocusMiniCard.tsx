import { Pause, Play, RefreshCw } from "lucide-react";
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

export function FocusMiniCard() {
  const focusQuery = useActiveFocusSessionQuery();
  const pauseFocus = usePauseFocusSession();
  const resumeFocus = useResumeFocusSession();
  const focusMinutes = useSettingsStore(
    (state) => state.preview?.focus.focusMinutes ?? state.focusMinutes,
  );
  const clock = useFocusClock(focusQuery.data);
  const breakClock = useBreakClock();
  const cyclePhase = useFocusCycleStore((state) => state.phase);
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
  const running = session?.status === "active";
  const paused = session?.status === "paused";
  const recoveryPending = session?.status === "recovery_pending";
  const busy = pauseFocus.isPending || resumeFocus.isPending;

  const statusLabel = focusQuery.isPending
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
                : "待开始";
  const dotState = recoveryPending
    ? "alert"
    : running || inBreak
      ? "active"
      : paused
        ? "paused"
        : "idle";

  const toggle = () => {
    if (!session) return;
    const input = { id: session.id, expectedVersion: session.version };
    if (running) pauseFocus.mutate(input);
    if (paused) resumeFocus.mutate(input);
  };

  const actionTitle = focusQuery.isError
    ? "重试读取专注会话"
    : running
      ? "暂停计时"
      : paused
        ? "继续专注"
        : inBreak
          ? breakEndsAtMs === null
            ? "开始休息"
            : "暂停休息"
          : recoveryPending
            ? "处理上次会话"
            : cyclePhase === "ready"
              ? "开始下一块"
              : "选择任务并开始";

  return (
    <div className="focus-mini-card sidebar-copy">
      <span
        aria-hidden="true"
        className="focus-mini-dot"
        data-state={dotState}
      />
      <strong className="focus-mini-time">
        {formatFocusTime(remainingSeconds)}
      </strong>
      <span className="focus-mini-label" title={statusLabel}>
        {statusLabel}
      </span>
      {focusQuery.isError ? (
        <button
          aria-label={actionTitle}
          className="icon-button focus-mini-action"
          onClick={() => void focusQuery.refetch()}
          title={actionTitle}
          type="button"
        >
          <RefreshCw size={14} />
        </button>
      ) : running || paused ? (
        <button
          aria-label={actionTitle}
          className="icon-button focus-mini-action"
          disabled={busy}
          onClick={toggle}
          title={actionTitle}
          type="button"
        >
          {running ? <Pause size={14} /> : <Play size={14} />}
        </button>
      ) : inBreak ? (
        <button
          aria-label={actionTitle}
          className="icon-button focus-mini-action"
          onClick={() =>
            breakEndsAtMs === null ? resumeBreak() : pauseBreak()
          }
          title={actionTitle}
          type="button"
        >
          {breakEndsAtMs === null ? <Play size={14} /> : <Pause size={14} />}
        </button>
      ) : (
        <Link
          aria-label={actionTitle}
          className="icon-button focus-mini-action"
          title={actionTitle}
          to="/focus"
        >
          <Play size={14} />
        </Link>
      )}
    </div>
  );
}
