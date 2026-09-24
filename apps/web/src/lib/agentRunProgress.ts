import type { AgentRunProgress } from "../types/models";

const phaseLabels: Record<AgentRunProgress["phase"], string> = {
  preparing: "正在准备执行上下文",
  calling_model: "正在调用模型",
  registering_result: "正在校验并登记完整结果",
};

export function agentRunProgressLabel(progress: AgentRunProgress): string {
  return phaseLabels[progress.phase];
}

export function agentRunElapsedLabel(elapsedMs: number): string {
  const totalSeconds = Math.floor(elapsedMs / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes} 分 ${seconds} 秒` : `${seconds} 秒`;
}

export function agentRunProgressStatus(progress: AgentRunProgress): string {
  return `${agentRunProgressLabel(progress)} · 已运行 ${agentRunElapsedLabel(progress.elapsedMs)}`;
}
