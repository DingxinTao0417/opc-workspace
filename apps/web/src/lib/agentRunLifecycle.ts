import type {
  AgentRunStartGate,
  AgentRunStartGateCloseReason,
} from "../types/models";

/**
 * One vocabulary for every Task Agent Run surface. The server stores two
 * independent columns (`status`, `output_delivery_status`) whose legal pairs
 * are fixed by schema 075 triggers; the Submission review state is a separate
 * Task fact that only some reads carry. Anything outside the legal pairs is
 * reported as `unknown` rather than guessed as success or failure.
 */
export type AgentRunLifecyclePhase =
  | "pending"
  | "running"
  | "recovery_required"
  | "succeeded"
  | "submitted"
  | "confirmed"
  | "failed"
  | "cancelled"
  | "unknown";

export type AgentRunSubmissionReviewStatus =
  "pending_review" | "accepted" | "changes_requested" | "withdrawn";

export interface AgentRunLifecycleInput {
  status: string | undefined;
  outputDeliveryStatus: string | undefined;
  /** `undefined` means the read does not carry Submission identity. */
  submissionId?: string | null;
  submissionStatus?: AgentRunSubmissionReviewStatus | null;
  /** ADR-030 start gate, already validated against the Run by the parser. */
  startGate?: Pick<AgentRunStartGate, "status" | "closeReason" | "require">;
}

const startGateClosedDetails: Record<AgentRunStartGateCloseReason, string> = {
  predecessor_failed: "前置执行失败或中断，这次执行没有启动。",
  predecessor_cancelled: "前置执行已取消，这次执行没有启动。",
  predecessor_retained:
    "前置执行的结果只被保留、没有登记为产出，这次执行没有启动。",
  predecessor_not_accepted:
    "前置产出被要求返工或已撤回，未被验收接受，这次执行没有启动。",
  predecessor_unavailable: "前置执行记录无法确认，这次执行没有启动。",
  expired: "在确认的时限内前置条件没有满足，这次执行没有启动。",
  run_not_queued: "这次执行已不在等待队列中。",
};

export interface AgentRunLifecycle {
  phase: AgentRunLifecyclePhase;
  /** Short badge text. */
  label: string;
  /** One natural-language sentence that never claims more than the facts. */
  detail: string;
  /** The Run needs a human decision or recovery. */
  attention: boolean;
  /** The executor will not change this Run any more. */
  terminal: boolean;
}

function lifecycle(
  phase: AgentRunLifecyclePhase,
  label: string,
  detail: string,
  attention: boolean,
  terminal: boolean,
): AgentRunLifecycle {
  return { phase, label, detail, attention, terminal };
}

const unknownLifecycle = lifecycle(
  "unknown",
  "状态无法确认",
  "服务端返回的执行状态组合无法识别；请刷新后以执行详情为准，不要据此重试。",
  true,
  false,
);

function submittedLifecycle(
  submissionStatus: AgentRunSubmissionReviewStatus | null | undefined,
): AgentRunLifecycle {
  switch (submissionStatus) {
    case "accepted":
      return lifecycle(
        "confirmed",
        "产出已验收",
        "产出已提交为任务提交，并已由人工验收接受。",
        false,
        true,
      );
    case "changes_requested":
      return lifecycle(
        "submitted",
        "已提交 · 已要求返工",
        "产出已提交，但人工验收要求返工；原提交与执行记录保留。",
        true,
        true,
      );
    case "withdrawn":
      return lifecycle(
        "submitted",
        "已提交 · 已撤回",
        "这次产出提交已撤回，不再是当前待验收产出。",
        false,
        true,
      );
    case "pending_review":
      return lifecycle(
        "submitted",
        "已提交 · 待人工验收",
        "产出已提交为任务提交，等待人工验收；执行结束不等于任务完成。",
        true,
        true,
      );
    default:
      return lifecycle(
        "submitted",
        "已提交为任务产出",
        "产出已提交为任务提交；验收结果以任务提交记录为准，执行结束不等于任务完成。",
        false,
        true,
      );
  }
}

export function agentRunLifecycle(
  input: AgentRunLifecycleInput,
): AgentRunLifecycle {
  const { status, outputDeliveryStatus: delivery, startGate } = input;
  if (
    status === "queued" &&
    delivery === "not_ready" &&
    startGate?.status === "waiting"
  ) {
    return lifecycle(
      "pending",
      "等待前置执行",
      startGate.require === "accepted"
        ? "已确认；等前置执行的产出被人工验收接受后才会进入执行队列，期间可以取消。"
        : "已确认；等前置执行的产出登记为任务提交后才会进入执行队列，期间可以取消。",
      false,
      false,
    );
  }
  if (
    status === "cancelled" &&
    delivery === "not_ready" &&
    startGate?.status === "closed" &&
    startGate.closeReason &&
    startGate.closeReason !== "run_not_queued"
  ) {
    return lifecycle(
      "cancelled",
      startGate.closeReason === "expired"
        ? "未启动 · 等待超时"
        : "未启动 · 前置条件未满足",
      startGateClosedDetails[startGate.closeReason],
      true,
      true,
    );
  }
  if (status === "queued" && delivery === "not_ready") {
    return lifecycle(
      "pending",
      "排队中",
      "执行已受理并在本地队列等待，尚未调用模型。",
      false,
      false,
    );
  }
  if (status === "running" && delivery === "not_ready") {
    return lifecycle(
      "running",
      "执行中",
      "正在执行；结束并完成登记前不会成为任务产出。",
      false,
      false,
    );
  }
  if (status === "running" && delivery === "pending") {
    return lifecycle(
      "recovery_required",
      "结果待登记",
      "模型已结束，结果在受保护暂存区等待登记；请恢复登记，不要重跑模型。",
      true,
      false,
    );
  }
  if (status === "succeeded" && delivery === "retained") {
    return lifecycle(
      "succeeded",
      "结果已保留 · 未提交",
      "执行已结束，结果只保留在执行记录中，没有成为任务产出。",
      true,
      true,
    );
  }
  if (status === "succeeded" && delivery === "submitted") {
    // Without the server-issued Submission identity there is no proof that
    // an output was actually registered.
    if (input.submissionId === null || input.submissionId === "") {
      return unknownLifecycle;
    }
    return submittedLifecycle(input.submissionStatus);
  }
  if (delivery !== "not_ready") return unknownLifecycle;
  if (status === "failed") {
    return lifecycle(
      "failed",
      "执行失败",
      "执行失败，没有登记任务产出。",
      true,
      true,
    );
  }
  if (status === "interrupted") {
    return lifecycle(
      "failed",
      "执行中断",
      "执行被中断，没有登记任务产出；中断前的模型调用是否产生费用无法确认。",
      true,
      true,
    );
  }
  if (status === "cancelled") {
    return lifecycle(
      "cancelled",
      "已取消",
      "执行已取消，没有登记任务产出。",
      false,
      true,
    );
  }
  return unknownLifecycle;
}
