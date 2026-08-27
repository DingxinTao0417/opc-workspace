import { ArrowUpRight, Pause, Play, Zap } from "lucide-react";
import { Link } from "react-router-dom";
import { useTasksQuery } from "../api/hooks";
import { formatFocusTime, useFocusStore } from "../store/focus";

export function RightOverview() {
  const tasksQuery = useTasksQuery();
  const focusRunning = useFocusStore((state) => state.running);
  const focusCompleted = useFocusStore((state) => state.completed);
  const focusPhase = useFocusStore((state) => state.phase);
  const focusDurationMinutes = useFocusStore((state) => state.durationMinutes);
  const focusRemainingSeconds = useFocusStore(
    (state) => state.remainingSeconds,
  );
  const toggleFocus = useFocusStore((state) => state.toggle);
  const resetFocus = useFocusStore((state) => state.reset);
  const focusTask = tasksQuery.data?.find(
    (task) => task.status === "in_progress",
  );
  const totalSeconds = focusDurationMinutes * 60;
  const focusProgress = focusCompleted
    ? 1
    : Math.min(1, Math.max(0, 1 - focusRemainingSeconds / totalSeconds));
  const ringOffset = 326.7 * (1 - focusProgress);

  const startOrToggle = () => {
    if (focusCompleted) {
      resetFocus();
      useFocusStore.getState().start();
      return;
    }
    toggleFocus();
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
              focusRunning || focusCompleted
                ? "status-badge status-green"
                : "status-badge"
            }
          >
            {focusCompleted
              ? "已完成"
              : focusRunning
                ? focusPhase === "focus"
                  ? "专注中"
                  : "休息中"
                : "待开始"}
          </span>
        </div>
        <div
          className="focus-ring"
          data-running={focusRunning}
          style={{ "--ring-offset": ringOffset } as React.CSSProperties}
        >
          <svg aria-hidden="true" viewBox="0 0 120 120">
            <circle className="ring-track" cx="60" cy="60" r="52" />
            <circle className="ring-value" cx="60" cy="60" r="52" />
          </svg>
          <div className="ring-copy">
            <strong>{formatFocusTime(focusRemainingSeconds)}</strong>
            <span>{focusPhase === "focus" ? "专注" : "休息"}</span>
          </div>
        </div>
        <div className="overview-focus-task">
          {focusTask?.title ?? "未选择专注任务"}
        </div>
        <button
          className="button button-secondary button-full"
          onClick={startOrToggle}
          type="button"
        >
          {focusRunning ? <Pause size={14} /> : <Play size={14} />}
          {focusCompleted
            ? "重新开始"
            : focusRunning
              ? "暂停计时"
              : focusPhase === "focus"
                ? "开始专注"
                : "开始休息"}
        </button>
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
