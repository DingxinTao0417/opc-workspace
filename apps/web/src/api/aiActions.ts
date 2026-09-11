import { apiRequest, ApiError, normalizeTask } from "./client";
import type { NewTaskInput } from "../types/models";

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

export interface AiGeneration {
  id: string;
  session_id: string;
  provider_id: string;
  status: "queued" | "streaming" | "completed" | "cancelled" | "failed";
  content: string;
  reasoning: string;
  error_code: string | null;
  persist: boolean;
  client_request_id: string;
}

function generation(value: unknown): AiGeneration {
  if (
    !record(value) ||
    typeof value.id !== "string" ||
    typeof value.session_id !== "string" ||
    typeof value.provider_id !== "string" ||
    !["queued", "streaming", "completed", "cancelled", "failed"].includes(
      String(value.status),
    )
  ) {
    throw new ApiError("生成状态响应无效", { code: "INVALID_RESPONSE" });
  }
  return {
    id: value.id,
    session_id: value.session_id,
    provider_id: value.provider_id,
    status: value.status as AiGeneration["status"],
    content: typeof value.content === "string" ? value.content : "",
    reasoning: typeof value.reasoning === "string" ? value.reasoning : "",
    error_code: typeof value.error_code === "string" ? value.error_code : null,
    persist: value.persist !== false,
    client_request_id:
      typeof value.client_request_id === "string"
        ? value.client_request_id
        : "",
  };
}

export async function getActiveAiGenerations(): Promise<AiGeneration[]> {
  const payload = await apiRequest<unknown>("/api/v1/ai/active-generations");
  if (!record(payload) || !Array.isArray(payload.data))
    throw new ApiError("生成列表响应无效", { code: "INVALID_RESPONSE" });
  return payload.data.map(generation);
}

export async function getAiGeneration(id: string): Promise<AiGeneration> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/generations/${encodeURIComponent(id)}`,
  );
  return generation(record(payload) ? payload.data : null);
}

export async function getAiGenerationByRequest(
  id: string,
): Promise<AiGeneration> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/generations/by-request/${encodeURIComponent(id)}`,
  );
  return generation(record(payload) ? payload.data : null);
}

export async function cancelAiGeneration(id: string): Promise<void> {
  await apiRequest(`/api/v1/ai/generations/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
  });
}

export async function confirmAiMessageTask(
  messageId: string,
  input: NewTaskInput,
) {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/messages/${encodeURIComponent(messageId)}/task-confirmation`,
    {
      method: "POST",
      body: JSON.stringify({
        title: input.title,
        description: input.description ?? "",
        kind: input.kind ?? "work",
        priority: input.priority,
        project_id: input.projectId ?? null,
        parent_task_id: input.parentTaskId ?? null,
        completion_criteria: input.completionCriteria ?? "",
        review_policy: input.reviewPolicy ?? "none",
        tag_ids: input.tagIds ?? [],
        due_date: input.dueDate ?? null,
        planned_date: input.plannedDate ?? null,
        estimated_minutes: input.estimatedMinutes ?? null,
        manual_order: input.manualOrder ?? null,
      }),
    },
  );
  if (
    !record(payload) ||
    !record(payload.data) ||
    !record(payload.data.message)
  )
    throw new ApiError("任务确认响应无效，请重试读取结果", {
      code: "INVALID_RESPONSE",
    });
  const task = normalizeTask(payload.data.task);
  if (
    payload.data.message.id !== messageId ||
    payload.data.message.task_id !== task.id
  )
    throw new ApiError("任务确认关联不一致", { code: "INVALID_RESPONSE" });
  return task;
}

export interface AiMemoryDecision {
  id: string;
  session_id: string;
  content: string;
  status: "pending" | "confirmed" | "rejected";
  memory_id: string | null;
}

export async function getAiMemoryDecision(
  proposalId: string | undefined,
  messageId: string,
): Promise<AiMemoryDecision> {
  const payload = await apiRequest<unknown>(
    proposalId
      ? `/api/v1/ai/memory-proposals/${encodeURIComponent(proposalId)}`
      : `/api/v1/ai/messages/${encodeURIComponent(messageId)}/memory-decision`,
  );
  if (
    !record(payload) ||
    !record(payload.data) ||
    typeof payload.data.id !== "string" ||
    typeof payload.data.content !== "string" ||
    !["pending", "confirmed", "rejected"].includes(String(payload.data.status))
  )
    throw new ApiError("记忆建议状态无效", { code: "INVALID_RESPONSE" });
  return {
    id: payload.data.id,
    session_id:
      typeof payload.data.session_id === "string"
        ? payload.data.session_id
        : "",
    content: payload.data.content,
    status: payload.data.status as AiMemoryDecision["status"],
    memory_id:
      typeof payload.data.memory_id === "string"
        ? payload.data.memory_id
        : null,
  };
}

export async function rejectAiMessageMemory(messageId: string): Promise<void> {
  await apiRequest(
    `/api/v1/ai/messages/${encodeURIComponent(messageId)}/memory-decision`,
    { method: "DELETE" },
  );
}

export interface AiCompactionStatus {
  session_id: string;
  status: "idle" | "pending" | "running" | "succeeded" | "failed" | "cancelled";
  error_code: string | null;
  partial_message: boolean;
  compacted_message_count: number;
}

export async function getAiCompactionStatus(
  sessionId: string,
): Promise<AiCompactionStatus> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/sessions/${encodeURIComponent(sessionId)}/compaction`,
  );
  if (
    !record(payload) ||
    !record(payload.data) ||
    payload.data.session_id !== sessionId ||
    ![
      "idle",
      "pending",
      "running",
      "succeeded",
      "failed",
      "cancelled",
    ].includes(String(payload.data.status)) ||
    typeof payload.data.partial_message !== "boolean" ||
    typeof payload.data.compacted_message_count !== "number" ||
    !Number.isSafeInteger(payload.data.compacted_message_count) ||
    payload.data.compacted_message_count < 0
  )
    throw new ApiError("会话整理状态无效", { code: "INVALID_RESPONSE" });
  return {
    session_id: sessionId,
    status: payload.data.status as AiCompactionStatus["status"],
    error_code:
      typeof payload.data.error_code === "string"
        ? payload.data.error_code
        : null,
    partial_message: payload.data.partial_message,
    compacted_message_count: payload.data.compacted_message_count,
  };
}
