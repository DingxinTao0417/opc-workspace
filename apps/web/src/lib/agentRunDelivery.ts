import type {
  AgentRun,
  AgentRunOutputDeliveryStatus,
  AgentRunStatus,
} from "../types/models";

const workspaceIdentity =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export const agentRunOutputDeliveryStatuses = [
  "not_ready",
  "pending",
  "submitted",
  "retained",
] as const satisfies readonly AgentRunOutputDeliveryStatus[];

export function validAgentRunDeliveryState(
  status: AgentRunStatus,
  deliveryStatus: AgentRunOutputDeliveryStatus,
  deliveryErrorCode: string | null,
  submissionId: string | null,
  artifactId: string | null,
): boolean {
  if (deliveryStatus === "submitted") {
    return (
      status === "succeeded" &&
      deliveryErrorCode === null &&
      submissionId !== null &&
      workspaceIdentity.test(submissionId) &&
      artifactId !== null &&
      workspaceIdentity.test(artifactId)
    );
  }
  if (deliveryStatus === "retained") {
    return (
      status === "succeeded" &&
      typeof deliveryErrorCode === "string" &&
      deliveryErrorCode.length > 0 &&
      submissionId === null &&
      artifactId === null
    );
  }
  if (deliveryStatus === "pending") {
    return (
      status === "running" &&
      deliveryErrorCode === "AGENT_OUTPUT_DELIVERY_PENDING" &&
      submissionId === null &&
      artifactId === null
    );
  }
  return (
    status !== "succeeded" &&
    deliveryErrorCode === null &&
    submissionId === null &&
    artifactId === null
  );
}

export function agentRunNeedsPolling(
  run: Pick<AgentRun, "status" | "outputDeliveryStatus">,
): boolean {
  return (
    run.status === "queued" ||
    run.status === "running" ||
    run.outputDeliveryStatus === "pending"
  );
}

export function agentRunDeliveryReason(errorCode: string | null): string {
  switch (errorCode) {
    case "TASK_MANUAL_REVIEW_REQUIRED":
      return "任务当前的验收配置不允许自动登记这次产出；结果已安全保留在执行记录中。";
    case "TASK_SUBMISSION_NOT_ALLOWED":
      return "任务当前状态不允许登记新产出；结果已安全保留在执行记录中。";
    case "AGENT_SUBMISSION_FAILED":
      return "登记产出时遇到问题；结果已安全保留在执行记录中。";
    default:
      return "本次结果未登记为任务产出，已安全保留在执行记录中。";
  }
}
