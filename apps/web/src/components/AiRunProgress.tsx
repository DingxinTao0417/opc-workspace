import { useEffect, useState } from "react";
import { aiToolLabel } from "../lib/aiToolLabels";
import type { AiRunProgress as RunProgress } from "../types/models";

function label(step: RunProgress) {
  const adjusted = step.trimmed_history_turns
    ? ` · 已调整 ${step.trimmed_history_turns} 轮较早对话`
    : "";
  const compacted = step.compacted_tool_results
    ? ` · 已收起 ${step.compacted_tool_results} 条较早只读结果`
    : "";
  if (step.kind === "tool_call")
    return aiToolLabel(step.tool_name) ?? "调用工具";
  if (step.kind === "model_turn") {
    const retried =
      step.retry_count && step.retry_count > 0
        ? ` · 已自动重试 ${step.retry_count} 次`
        : "";
    return `模型推理 · 第 ${step.turn_index} 轮${retried}${adjusted}${compacted}`;
  }
  if (step.kind === "self_check")
    return `检查与修订回答${adjusted}${compacted}`;
  if (step.kind === "citation_validation") return "核验引用来源";
  return "保存生成结果";
}
function status(step: RunProgress, live: boolean) {
  if (step.status === "running") return live ? "进行中" : "结果未确认";
  return { succeeded: "完成", failed: "未成功", cancelled: "已停止" }[
    step.status
  ];
}
function elapsed(step: RunProgress, now: number, live: boolean) {
  if (step.kind === "persistence") return "";
  if (step.status === "running" && !live) return "";
  const ms =
    step.status === "running"
      ? Math.max(0, now - Date.parse(step.started_at))
      : step.duration_ms;
  return `${Math.floor(ms / 1000)} 秒`;
}

/** Runtime facts only. Completion of a proposal tool never approves its action. */
export function AiRunProgress({
  steps,
  live = false,
}: {
  steps?: RunProgress[];
  live?: boolean;
}) {
  const [now, setNow] = useState(Date.now);
  const last = steps?.at(-1);
  const ticking = live && last?.status === "running";
  useEffect(() => {
    if (!ticking) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [ticking, last?.sequence]);
  if (!last) return null;
  const current =
    last.status === "running"
      ? `${label(last)} · ${status(last, live)}`
      : live
        ? "正在整理生成结果…"
        : "执行步骤";
  return (
    <details className="ai-run-progress">
      <summary>
        <span role="status" aria-live="polite">
          {current}
        </span>
        <span aria-hidden="true">{elapsed(last, now, live)}</span>
      </summary>
      <ol aria-label="执行步骤">
        {steps!.map((step) => (
          <li key={step.sequence} data-status={step.status}>
            <span>{label(step)}</span>
            <span>
              {status(step, live)} {elapsed(step, now, live)}
            </span>
          </li>
        ))}
      </ol>
      <small>
        仅显示步骤与耗时，不含工具参数或结果；操作建议仍须逐项确认。
      </small>
      {steps?.some((step) => step.trimmed_history_turns) ? (
        <small>
          为继续处理已调整较早对话窗口；当前消息与已授权上下文保留，较早约束可能不完整。本地聊天记录未删除。
        </small>
      ) : null}
      {steps?.some((step) => step.compacted_tool_results) ? (
        <small>
          为继续处理已收起较早的只读查询结果，并非摘要；如需引用其中事实或版本，智能体必须按当前权限重新查询。最新工具结果与操作回执保留，本地记录未删除。
        </small>
      ) : null}
    </details>
  );
}
