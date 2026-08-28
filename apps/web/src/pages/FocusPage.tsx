import {
  Ban,
  Clock3,
  Pause,
  Play,
  RotateCcw,
  Settings2,
  ShieldCheck,
  SkipForward,
  Square,
} from "lucide-react";
import { useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useActiveFocusSessionQuery,
  useCancelFocusSession,
  useCreateFocusSession,
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
