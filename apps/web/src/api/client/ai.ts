import { parseAiAccessRequest } from "../../lib/aiAccessRequest";
import { parseAiGenerationOrigin } from "../../lib/aiGenerationOrigin";
import type {
  AiMemory,
  AiMemoryProposal,
  AiMessage,
  AiMessageListResult,
  AiProvider,
  AiSession,
  CreateAiProviderInput,
  UpdateAiProviderInput,
} from "../../types/models";
import {
  aiBusinessContextProviderFromRecord,
  aiBusinessContextSourceFromRecord,
  aiCitationsFromMessage,
  aiKnowledgeContextSourceFromRecord,
} from "./aiContext";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  numeric,
  stringField,
} from "./core";

export async function getAiProviders(): Promise<AiProvider[]> {
  const payload = await apiRequest<unknown>("/api/v1/ai/providers");
  return aiProviderList(payload);
}

function aiProviderList(body: unknown): AiProvider[] {
  if (!isRecord(body) || !Array.isArray(body.data)) {
    return invalidResponse("AI 供应商列表响应格式无效");
  }
  return body.data.map((row) => aiProviderFromRecord(row));
}

function aiProviderFromRecord(row: unknown): AiProvider {
  if (!isRecord(row)) return invalidResponse("AI 供应商响应格式无效");
  const protocol = stringField(row, "protocol");
  const status = stringField(row, "status");
  const healthStatus = stringField(row, "health_status");
  if (!protocol || !status || !healthStatus) {
    return invalidResponse("AI 供应商响应格式无效");
  }
  return {
    configVersion: numeric(
      fieldValue(row, "config_version", "configVersion"),
      1,
    ),
    id: stringField(row, "id") ?? "",
    name: stringField(row, "name") ?? "",
    kind: stringField(row, "kind") === "local" ? "local" : "remote",
    protocol: protocol as AiProvider["protocol"],
    base_url: stringField(row, "base_url") ?? "",
    model: stringField(row, "model") ?? "",
    status: status as AiProvider["status"],
    health_status: healthStatus as AiProvider["health_status"],
    health_error_code: stringField(row, "health_error_code") ?? null,
    has_key: row.has_key === true,
    last_health_at: stringField(row, "last_health_at") ?? null,
    version: numeric(row.version, 0),
    created_at: stringField(row, "created_at") ?? "",
    updated_at: stringField(row, "updated_at") ?? "",
  };
}

export async function createAiProvider(
  input: CreateAiProviderInput,
  idempotencyKey: string,
): Promise<AiProvider> {
  const payload = await apiRequest<unknown>("/api/v1/ai/providers", {
    method: "POST",
    body: JSON.stringify(input),
    headers: { "Idempotency-Key": idempotencyKey },
  });
  return aiProviderFromEnvelope(payload);
}

export async function getAiProvider(id: string): Promise<AiProvider> {
  const payload = await apiRequest<unknown>(`/api/v1/ai/providers/${id}`);
  return aiProviderFromEnvelope(payload);
}

export async function updateAiProvider(
  id: string,
  input: UpdateAiProviderInput,
  expectedVersion: number,
): Promise<AiProvider> {
  const payload = await apiRequest<unknown>(`/api/v1/ai/providers/${id}`, {
    method: "PATCH",
    body: JSON.stringify(input),
    headers: { "If-Match": `"${expectedVersion}"` },
  });
  return aiProviderFromEnvelope(payload);
}

export async function deleteAiProvider(
  id: string,
  expectedVersion: number,
): Promise<void> {
  await apiRequest<unknown>(`/api/v1/ai/providers/${id}`, {
    method: "DELETE",
    headers: { "If-Match": `"${expectedVersion}"` },
  });
}

export async function checkAiProviderHealth(
  id: string,
  expectedVersion: number,
): Promise<AiProvider> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/providers/${id}/health`,
    { method: "POST", headers: { "If-Match": `"${expectedVersion}"` } },
  );
  return aiProviderFromEnvelope(payload);
}

export async function setAiProviderKey(
  id: string,
  apiKey: string,
  expectedVersion: number,
): Promise<AiProvider> {
  const payload = await apiRequest<unknown>(`/api/v1/ai/providers/${id}/key`, {
    method: "POST",
    body: JSON.stringify({ api_key: apiKey }),
    headers: { "If-Match": `"${expectedVersion}"` },
  });
  return aiProviderFromEnvelope(payload);
}

function aiProviderFromEnvelope(payload: unknown): AiProvider {
  if (!isRecord(payload)) return invalidResponse("AI 供应商响应格式无效");
  return aiProviderFromRecord(payload.data);
}

function aiMemoryFromRecord(row: unknown): AiMemory {
  if (!isRecord(row)) return invalidResponse("AI 记忆响应格式无效");
  return {
    id: stringField(row, "id") ?? "",
    content: stringField(row, "content") ?? "",
    source_message_id: stringField(row, "source_message_id") ?? null,
    created_at: stringField(row, "created_at") ?? "",
    updated_at: stringField(row, "updated_at") ?? "",
  };
}

export async function getAiMemories(): Promise<AiMemory[]> {
  const payload = await apiRequest<unknown>("/api/v1/ai/memories");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("AI 记忆列表响应格式无效");
  }
  return payload.data.map((row) => aiMemoryFromRecord(row));
}

export async function createAiMemory(
  input: {
    content: string;
    source_message_id?: string;
    proposal_id?: string;
  },
  idempotencyKey: string,
): Promise<AiMemory> {
  const payload = await apiRequest<unknown>("/api/v1/ai/memories", {
    method: "POST",
    body: JSON.stringify(input),
    headers: { "Idempotency-Key": idempotencyKey },
  });
  if (!isRecord(payload)) return invalidResponse("AI 记忆响应格式无效");
  return aiMemoryFromRecord(payload.data);
}

export async function deleteAiMemory(id: string): Promise<void> {
  await apiRequest<unknown>(`/api/v1/ai/memories/${id}`, { method: "DELETE" });
}

function aiMemoryProposalFromRecord(row: unknown): AiMemoryProposal {
  if (!isRecord(row) || !Array.isArray(row.tags)) {
    return invalidResponse("AI 记忆建议响应格式无效");
  }
  const tags = row.tags.map((tag) => {
    if (typeof tag !== "string") {
      return invalidResponse("AI 记忆建议标签格式无效");
    }
    return tag;
  });
  return {
    id: stringField(row, "id") ?? "",
    session_id: stringField(row, "session_id") ?? "",
    session_title: stringField(row, "session_title") ?? "",
    content: stringField(row, "content") ?? "",
    tags,
    created_at: stringField(row, "created_at") ?? "",
  };
}

export async function getAiMemoryProposals(): Promise<AiMemoryProposal[]> {
  const payload = await apiRequest<unknown>("/api/v1/ai/memory-proposals");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("AI 记忆建议列表响应格式无效");
  }
  return payload.data.map((row) => aiMemoryProposalFromRecord(row));
}

export async function rejectAiMemoryProposal(id: string): Promise<void> {
  await apiRequest<unknown>(`/api/v1/ai/memory-proposals/${id}`, {
    method: "DELETE",
  });
}

export async function getAiSessions(): Promise<AiSession[]> {
  const payload = await apiRequest<unknown>("/api/v1/ai/sessions");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("AI 会话列表响应格式无效");
  }
  return payload.data.map((row) => {
    if (!isRecord(row)) return invalidResponse("AI 会话响应格式无效");
    return {
      id: stringField(row, "id") ?? "",
      title: stringField(row, "title") ?? "",
      persist: row.persist !== false,
      compacted_message_count: numeric(row.compacted_message_count, 0),
      version: numeric(row.version, 0),
      created_at: stringField(row, "created_at") ?? "",
      updated_at: stringField(row, "updated_at") ?? "",
    };
  });
}

export async function createAiSession(): Promise<AiSession> {
  const payload = await apiRequest<unknown>("/api/v1/ai/sessions", {
    method: "POST",
    body: JSON.stringify({}),
  });
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 会话响应格式无效");
  }
  const row = payload.data;
  return {
    id: stringField(row, "id") ?? "",
    title: stringField(row, "title") ?? "",
    persist: row.persist !== false,
    compacted_message_count: numeric(row.compacted_message_count, 0),
    version: numeric(row.version, 0),
    created_at: stringField(row, "created_at") ?? "",
    updated_at: stringField(row, "updated_at") ?? "",
  };
}

export async function deleteAiSession(
  id: string,
  expectedVersion: number,
): Promise<void> {
  await apiRequest<unknown>(`/api/v1/ai/sessions/${id}`, {
    method: "DELETE",
    headers: { "If-Match": `"${expectedVersion}"` },
  });
}

export async function getAiMessages(
  sessionId: string,
  cursor?: { oldestCreatedAt?: string; oldestId?: string },
): Promise<AiMessageListResult> {
  const params = new URLSearchParams();
  params.set("limit", "100");
  if (cursor?.oldestCreatedAt && cursor?.oldestId) {
    params.set("before_created_at", cursor.oldestCreatedAt);
    params.set("before_id", cursor.oldestId);
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/sessions/${sessionId}/messages?${params.toString()}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("AI 消息列表响应格式无效");
  }
  const data = payload.data.map((row) => {
    if (!isRecord(row)) return invalidResponse("AI 消息响应格式无效");
    const role = stringField(row, "role");
    const status = stringField(row, "status");
    const citation = aiCitationsFromMessage(row);
    return {
      origin: parseAiGenerationOrigin(row.origin),
      access_request: (() => {
        try {
          return parseAiAccessRequest(row.access_request);
        } catch {
          return invalidResponse("AI 工作台权限请求响应无效");
        }
      })(),
      id: stringField(row, "id") ?? "",
      session_id: stringField(row, "session_id") ?? "",
      role: (role === "assistant" ? "assistant" : "user") as AiMessage["role"],
      status: (status ?? "completed") as AiMessage["status"],
      content: stringField(row, "content") ?? "",
      reasoning: stringField(row, "reasoning") ?? null,
      task_id: stringField(row, "task_id") ?? null,
      task_title_snapshot: stringField(row, "task_title_snapshot") ?? null,
      generation_id: stringField(row, "generation_id", "generationId") ?? null,
      context_provider: aiBusinessContextProviderFromRecord(
        row.context_provider,
      ),
      context_sources: Array.isArray(row.context_sources)
        ? row.context_sources.map((source) =>
            aiBusinessContextSourceFromRecord(source),
          )
        : [],
      context_knowledge: Array.isArray(row.context_knowledge)
        ? row.context_knowledge.map(aiKnowledgeContextSourceFromRecord)
        : [],
      citation_status: citation.status,
      citations: citation.items,
      created_at: stringField(row, "created_at") ?? "",
    } satisfies AiMessage;
  });
  return {
    data,
    meta: {
      has_more: payload.meta.has_more === true,
      oldest_created_at: stringField(payload.meta, "oldest_created_at"),
      oldest_id: stringField(payload.meta, "oldest_id"),
    },
  };
}

export async function attachTaskToAiMessage(
  messageId: string,
  taskId: string,
): Promise<AiMessage> {
  const payload = await apiRequest<unknown>(
    `/api/v1/ai/messages/${messageId}/task`,
    { method: "POST", body: JSON.stringify({ task_id: taskId }) },
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 消息响应格式无效");
  }
  const row = payload.data;
  const citation = aiCitationsFromMessage(row);
  return {
    id: stringField(row, "id") ?? "",
    session_id: stringField(row, "session_id") ?? "",
    role: "assistant",
    status: "completed",
    content: stringField(row, "content") ?? "",
    reasoning: stringField(row, "reasoning") ?? null,
    task_id: stringField(row, "task_id") ?? null,
    task_title_snapshot: stringField(row, "task_title_snapshot") ?? null,
    generation_id: stringField(row, "generation_id", "generationId") ?? null,
    context_provider: aiBusinessContextProviderFromRecord(row.context_provider),
    context_sources: Array.isArray(row.context_sources)
      ? row.context_sources.map((source) =>
          aiBusinessContextSourceFromRecord(source),
        )
      : [],
    context_knowledge: Array.isArray(row.context_knowledge)
      ? row.context_knowledge.map(aiKnowledgeContextSourceFromRecord)
      : [],
    citation_status: citation.status,
    citations: citation.items,
    created_at: stringField(row, "created_at") ?? "",
  };
}
