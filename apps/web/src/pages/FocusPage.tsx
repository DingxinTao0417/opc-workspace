import { Pause, Play, RotateCcw, Settings2, ShieldCheck } from "lucide-react";
import { PageHeader } from "../components/PageHeader";
import { formatFocusTime, useFocusStore } from "../store/focus";
import { useUiStore } from "../store/ui";

export function FocusPage() {
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const running = useFocusStore((state) => state.running);
  const phase = useFocusStore((state) => state.phase);
  const completed = useFocusStore((state) => state.completed);
  const completedCycles = useFocusStore((state) => state.completedCycles);
  const cycles = useFocusStore((state) => state.cycles);
  const durationMinutes = useFocusStore((state) => state.durationMinutes);
  const remainingSeconds = useFocusStore((state) => state.remainingSeconds);
  const toggleFocus = useFocusStore((state) => state.toggle);
  const resetFocus = useFocusStore((state) => state.reset);
  const totalSeconds = durationMinutes * 60;
  const progress = completed
    ? 1
    : Math.min(1, Math.max(0, 1 - remainingSeconds / totalSeconds));

  const startOrToggle = () => {
    if (completed) {
      resetFocus();
      useFocusStore.getState().start();
      return;
    }
    toggleFocus();
  };

  return (
    <div className="page focus-page">
      <PageHeader
        actions={
          <button
            className="button button-secondary"
            onClick={() => setSettingsOpen(true)}
            type="button"
          >
            <Settings2 size={15} />
            专注设置
          </button>
        }
        meta={
          <span className="page-count">
            {completed
              ? `${cycles} / ${cycles} 已完成`
              : `${Math.min(completedCycles + 1, cycles)} / ${cycles} 专注块`}
          </span>
        }
        title="专注"
      />
      <section className="focus-stage">
        <div
          className="focus-stage-ring"
          style={
            {
              "--focus-progress": `${progress * 360}deg`,
            } as React.CSSProperties
          }
        >
          <div>
            <strong>{formatFocusTime(remainingSeconds)}</strong>
            <span>
              {completed
                ? "本轮完成"
                : running
                  ? phase === "focus"
                    ? "正在专注"
                    : "休息中"
                  : phase === "focus"
                    ? "准备开始"
                    : "准备休息"}
            </span>
          </div>
        </div>
        <span className="focus-project">
          {phase === "focus" ? "专注阶段" : "休息阶段"}
        </span>
        <h2>
          {completed
            ? "本轮专注已完成"
            : phase === "focus"
              ? "选择一项任务后开始专注"
              : "休息一下，准备下一轮"}
        </h2>
        <p>计时会在页面切换后继续；任务工时持久化将在后续版本接入。</p>
        <div className="focus-controls">
          <button
            aria-label="重置计时"
            className="icon-button icon-button-large"
            onClick={resetFocus}
            type="button"
          >
            <RotateCcw size={18} />
          </button>
          <button
            className="button button-primary focus-primary"
            onClick={startOrToggle}
            type="button"
          >
            {running ? <Pause size={17} /> : <Play size={17} />}{" "}
            {completed
              ? "重新开始"
              : running
                ? "暂停"
                : phase === "focus"
                  ? "开始专注"
                  : "开始休息"}
          </button>
        </div>
      </section>
      <section className="focus-note">
        <ShieldCheck size={17} />
        <div>
          <strong>当前能力边界</strong>
          <p>系统勿扰和本应用通知暂停将在桌面通知能力接入后启用。</p>
        </div>
      </section>
    </div>
  );
}
