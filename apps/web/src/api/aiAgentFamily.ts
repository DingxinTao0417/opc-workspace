import {
  ApiError,
  apiRequest,
  validCanonicalUUID,
  validRFC3339Timestamp,
} from "./client";

export type AiAgentFamilyStatus =
  "queued" | "streaming" | "completed" | "failed" | "cancelled" | "unavailable";

export interface AiAgentFamilyMember {
  sessionId: string;
  proposalId: string;
  taskName: string;
  status: AiAgentFamilyStatus;
  errorCode: string | null;
  available: boolean;
  pendingApprovals: number;
  activeGenerationId: string | null;
  createdAt: string;
  decidedAt: string;
}

export interface AiAgentFamily {
  sessionId: string;
  parent: AiAgentFamilyMember | null;
  children: AiAgentFamilyMember[];
  asOf: string;
}

type Row = Record<string, unknown>;

function invalid(message: string): never {
  throw new ApiError(message, { code: "INVALID_RESPONSE" });
}

function row(value: unknown): Row {
  if (!value || typeof value !== "object" || Array.isArray(value))
    return invalid("子会话关系响应格式无效");
  return value as Row;
}

function exactKeys(value: Row, keys: string[]): boolean {
  const actual = Object.keys(value);
  return actual.length === keys.length && keys.every((key) => key in value);
}

function member(value: unknown): AiAgentFamilyMember {
  const item = row(value);
  if (
    !exactKeys(item, [
      "session_id",
      "proposal_id",
      "task_name",
      "status",
      "error_code",
      "available",
      "pending_approvals",
      "active_generation_id",
      "created_at",
      "decided_at",
    ]) ||
    !validCanonicalUUID(item.session_id) ||
    !validCanonicalUUID(item.proposal_id) ||
    typeof item.task_name !== "string" ||
    !/^[a-z][a-z0-9_]{0,39}$/.test(item.task_name) ||
    ![
      "queued",
      "streaming",
      "completed",
      "failed",
      "cancelled",
      "unavailable",
    ].includes(String(item.status)) ||
    typeof item.available !== "boolean" ||
    (item.available === false) !== (item.status === "unavailable") ||
    !Number.isSafeInteger(item.pending_approvals) ||
    (item.pending_approvals as number) < 0 ||
    (!item.available && item.pending_approvals !== 0) ||
    (item.active_generation_id !== null &&
      (!validCanonicalUUID(item.active_generation_id) ||
        (item.status !== "queued" && item.status !== "streaming"))) ||
    (item.error_code !== null &&
      (typeof item.error_code !== "string" ||
        !/^[A-Z][A-Z0-9_]{0,99}$/.test(item.error_code))) ||
    !validRFC3339Timestamp(item.created_at) ||
    !validRFC3339Timestamp(item.decided_at)
  )
    return invalid("子会话关系成员格式无效");
  return {
    sessionId: item.session_id as string,
    proposalId: item.proposal_id as string,
    taskName: item.task_name,
    status: item.status as AiAgentFamilyStatus,
    errorCode: item.error_code as string | null,
    available: item.available,
    pendingApprovals: item.pending_approvals as number,
    activeGenerationId: item.active_generation_id as string | null,
    createdAt: item.created_at as string,
    decidedAt: item.decided_at as string,
  };
}

export interface AiAgentChildGenerationCancelResult {
  parentSessionId: string;
  childSessionId: string;
  generationId: string;
  cancelRequested: true;
}

export async function cancelAiAgentChildGeneration(
  parentSessionId: string,
  childSessionId: string,
  generationId: string,
): Promise<AiAgentChildGenerationCancelResult> {
  if (
    !validCanonicalUUID(parentSessionId) ||
    !validCanonicalUUID(childSessionId) ||
    !validCanonicalUUID(generationId)
  )
    return invalid("子会话停止请求的身份无效");
  const payload = row(
    await apiRequest<unknown>(
      `/api/v1/ai/sessions/${encodeURIComponent(parentSessionId)}/agent-children/${encodeURIComponent(childSessionId)}/generations/${encodeURIComponent(generationId)}/cancel`,
      { method: "POST" },
    ),
  );
  if (!exactKeys(payload, ["data"]))
    return invalid("子会话停止请求响应格式无效");
  const data = row(payload.data);
  if (
    !exactKeys(data, [
      "parent_session_id",
      "child_session_id",
      "generation_id",
      "cancel_requested",
    ]) ||
    data.parent_session_id !== parentSessionId ||
    data.child_session_id !== childSessionId ||
    data.generation_id !== generationId ||
    data.cancel_requested !== true
  )
    return invalid("子会话停止请求身份不一致");
  return {
    parentSessionId,
    childSessionId,
    generationId,
    cancelRequested: true,
  };
}

export interface AiAgentChildGenerationCancelTarget {
  childSessionId: string;
  generationId: string;
}

export interface AiAgentChildGenerationCancelBatchResult {
  parentSessionId: string;
  items: Array<AiAgentChildGenerationCancelTarget & { cancelRequested: true }>;
}

export async function cancelAiAgentChildGenerations(
  parentSessionId: string,
  targets: AiAgentChildGenerationCancelTarget[],
): Promise<AiAgentChildGenerationCancelBatchResult> {
  if (
    !validCanonicalUUID(parentSessionId) ||
    !Array.isArray(targets) ||
    targets.length < 1 ||
    targets.length > 4
  )
    return invalid("批量停止请求范围无效");
  const childIds = new Set<string>();
  const generationIds = new Set<string>();
  for (const target of targets) {
    if (
      !target ||
      !validCanonicalUUID(target.childSessionId) ||
      !validCanonicalUUID(target.generationId) ||
      childIds.has(target.childSessionId) ||
      generationIds.has(target.generationId)
    )
      return invalid("批量停止请求身份无效或重复");
    childIds.add(target.childSessionId);
    generationIds.add(target.generationId);
  }
  const payload = row(
    await apiRequest<unknown>(
      `/api/v1/ai/sessions/${encodeURIComponent(parentSessionId)}/agent-children/cancel`,
      {
        method: "POST",
        body: JSON.stringify({
          items: targets.map(({ childSessionId, generationId }) => ({
            child_session_id: childSessionId,
            generation_id: generationId,
          })),
        }),
      },
    ),
  );
  if (!exactKeys(payload, ["data"])) return invalid("批量停止响应格式无效");
  const data = row(payload.data);
  if (
    !exactKeys(data, ["parent_session_id", "items"]) ||
    data.parent_session_id !== parentSessionId ||
    !Array.isArray(data.items) ||
    data.items.length !== targets.length
  )
    return invalid("批量停止响应身份无效");
  const items = data.items.map((value, index) => {
    const item = row(value);
    const target = targets[index];
    if (
      !exactKeys(item, [
        "child_session_id",
        "generation_id",
        "cancel_requested",
      ]) ||
      item.child_session_id !== target.childSessionId ||
      item.generation_id !== target.generationId ||
      item.cancel_requested !== true
    )
      return invalid("批量停止响应与所选执行不一致");
    return { ...target, cancelRequested: true as const };
  });
  return { parentSessionId, items };
}

export async function getAiAgentFamily(
  sessionId: string,
  signal?: AbortSignal,
): Promise<AiAgentFamily> {
  if (!validCanonicalUUID(sessionId)) return invalid("AI 会话身份无效");
  const payload = row(
    await apiRequest<unknown>(
      `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/agent-family`,
      { signal },
    ),
  );
  if (!exactKeys(payload, ["data"])) return invalid("子会话关系响应格式无效");
  const data = row(payload.data);
  if (
    !exactKeys(data, ["session_id", "parent", "children", "as_of"]) ||
    data.session_id !== sessionId ||
    !Array.isArray(data.children) ||
    data.children.length > 4 ||
    !validRFC3339Timestamp(data.as_of)
  )
    return invalid("子会话关系响应格式无效");
  const parent = data.parent === null ? null : member(data.parent);
  const children = data.children.map(member);
  if (
    (parent && parent.sessionId === sessionId) ||
    (parent && parent.activeGenerationId !== null) ||
    (parent && children.length > 0) ||
    children.some((child) => child.sessionId === sessionId) ||
    new Set(children.map((child) => child.sessionId)).size !==
      children.length ||
    new Set(children.map((child) => child.proposalId)).size !== children.length
  )
    return invalid("子会话关系身份不一致");
  return { sessionId, parent, children, asOf: data.as_of as string };
}
