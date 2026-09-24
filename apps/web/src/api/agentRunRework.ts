import { apiRequest, ApiError } from "./client";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import {
  validAgentRunReworkContext,
  validAgentRunReworkRequest,
} from "../lib/agentRunRework";
import type {
  AgentRunReworkContext,
  AgentRunReworkRequest,
} from "../types/models";

export async function getAgentRunReworkPreview(
  taskId: string,
  rework: AgentRunReworkRequest,
  signal?: AbortSignal,
): Promise<AgentRunReworkContext> {
  if (!isWorkspaceIdentity(taskId) || !validAgentRunReworkRequest(rework))
    throw new ApiError("返工来源身份无效。", { code: "INVALID_INPUT" });
  const response = await apiRequest<{
    data: { task_id: string; task_version: number; rework_context: unknown };
  }>(`/api/v1/tasks/${taskId}/agent-runs/rework-preview`, {
    method: "POST",
    signal,
    body: JSON.stringify({
      rework: {
        submission_id: rework.submissionId,
        artifact_ids: rework.artifactIds,
        expected_task_version: rework.expectedTaskVersion,
      },
    }),
  });
  const data = response?.data;
  const context = data?.rework_context;
  if (
    !data ||
    Object.keys(data).sort().join() !== "rework_context,task_id,task_version" ||
    data.task_id !== taskId ||
    data.task_version !== rework.expectedTaskVersion ||
    !validAgentRunReworkContext(context) ||
    context.submission_id !== rework.submissionId ||
    context.artifacts.length !== rework.artifactIds.length ||
    context.artifacts.some(
      (item, index) => item.id !== rework.artifactIds[index],
    )
  )
    throw new ApiError("返工预览与所选来源不一致，请重新读取。", {
      code: "INVALID_RESPONSE",
    });
  return context;
}
