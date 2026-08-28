import { AlertTriangle, Clock3, History, Play } from "lucide-react";
import {
  useActiveFocusSessionQuery,
  useRecoverFocusSession,
} from "../api/hooks";
import { ApiError } from "../api/client";
import {
  formatFocusTime,
  useFocusClock,
  useFocusCycleStore,
} from "../store/focus";
import type { FocusRecoveryAction } from "../types/models";
import { Modal } from "./Modal";

const recoveryOptions: {
  action: FocusRecoveryAction;
  icon: typeof Play;
  title: string;
  description: string;
  danger?: boolean;
}[] = [
  {
    action: "exclude_gap_resume",
    icon: Play,
    title: "按最后心跳恢复",
    description: "只保留应用仍在线时确认的专注时间，然后从现在继续。",
  },
  {
    action: "include_gap_resume",
    icon: Clock3,
    title: "计入中断间隔并恢复",
    description: "把应用离线期间也计入本次专注，然后从现在继续。",
  },
  {
    action: "interrupt",
    icon: History,
    title: "结束为中断",
    description: "保留已确认时长并结束会话，不累计到任务工时。",
    danger: true,
  },
];

export function FocusRecoveryModal() {
  const focusQuery = useActiveFocusSessionQuery();
  const recover = useRecoverFocusSession();
  const resetCycle = useFocusCycleStore((state) => state.resetCycle);
  const clock = useFocusClock(focusQuery.data);
  const session = focusQuery.data?.session;
  const open = session?.status === "recovery_pending";
  const error =
    recover.error instanceof ApiError
      ? recover.error.message
      : recover.isError
        ? "无法处理上次专注会话，请重试。"
        : null;

  const run = (action: FocusRecoveryAction) => {
    if (!session || session.status !== "recovery_pending") return;
    recover.mutate(
      {
        id: session.id,
        action,
        expectedVersion: session.version,
      },
      { onSuccess: () => action === "interrupt" && resetCycle() },
    );
  };

  return (
    <Modal
      dismissible={false}
      footer={
        <span className="focus-recovery-footer-note">
          必须明确选择后才能继续本次专注。
        </span>
      }
      onClose={() => undefined}
      open={open}
      title="恢复上次专注"
      width="560px"
    >
      <div className="focus-recovery-summary">
        <span className="focus-recovery-warning">
          <AlertTriangle size={17} />
        </span>
        <div>
          <strong>{session?.taskTitle ?? "未绑定任务的专注"}</strong>
          <p>
            已确认 {formatFocusTime(clock.elapsedSeconds)}
            {clock.uncertainSeconds > 0
              ? `，另有约 ${formatFocusTime(clock.uncertainSeconds)} 的中断间隔待确认。`
              : "，应用上次退出前的最后区间需要确认。"}
          </p>
        </div>
      </div>

      <div className="focus-recovery-options">
        {recoveryOptions.map(
          ({ action, icon: Icon, title, description, danger }) => (
            <button
              className={`focus-recovery-option${danger ? " is-danger" : ""}`}
              disabled={recover.isPending}
              key={action}
              onClick={() => run(action)}
              type="button"
            >
              <span>
                <Icon size={16} />
              </span>
              <span>
                <strong>{title}</strong>
                <small>{description}</small>
              </span>
            </button>
          ),
        )}
      </div>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </Modal>
  );
}
