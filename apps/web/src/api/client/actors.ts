import type {
  Actor,
  ActorListParams,
  ActorListResult,
  ActorSummary,
  CreatePersonActorInput,
  UpdateActorInput,
} from "../../types/models";
import {
  ApiError,
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
} from "./core";

export function asActorType(value: unknown): Actor["type"] {
  if (
    value === "owner" ||
    value === "person" ||
    value === "system" ||
    value === "agent"
  ) {
    return value;
  }
  return invalidResponse("责任主体类型响应无效");
}

export function asActorStatus(value: unknown): Actor["status"] {
  if (value === "active" || value === "inactive") return value;
  return invalidResponse("责任主体状态响应无效");
}

export function normalizeActorSummary(value: unknown): ActorSummary {
  if (!isRecord(value)) return invalidResponse("责任主体摘要响应格式无效");
  const id = stringField(value, "id");
  const displayName = stringField(value, "display_name", "displayName");
  const isBuiltin = fieldValue(value, "is_builtin", "isBuiltin");
  if (!id || !displayName || typeof isBuiltin !== "boolean") {
    return invalidResponse("责任主体摘要响应格式无效");
  }
  return {
    id,
    type: asActorType(value.type),
    displayName,
    status: asActorStatus(value.status),
    isBuiltin,
    version: positiveInteger(value.version, "责任主体摘要版本"),
  };
}

export function normalizeActor(value: unknown): Actor {
  if (!isRecord(value)) return invalidResponse("责任主体响应格式无效");
  const id = stringField(value, "id");
  const displayName = stringField(value, "display_name", "displayName");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const isBuiltin = fieldValue(value, "is_builtin", "isBuiltin");
  const metadata = value.metadata;
  const agentAdapterId = fieldValue(
    value,
    "agent_adapter_id",
    "agentAdapterId",
  );
  if (
    !id ||
    !displayName ||
    !createdAt ||
    !updatedAt ||
    typeof isBuiltin !== "boolean" ||
    !isRecord(metadata)
  ) {
    return invalidResponse("责任主体响应格式无效");
  }
  if (typeof value.notes !== "string") {
    return invalidResponse("责任主体备注响应无效");
  }
  if (
    agentAdapterId != null &&
    (typeof agentAdapterId !== "string" || !agentAdapterId.trim())
  ) {
    return invalidResponse("责任主体适配器关联响应无效");
  }
  return {
    id,
    type: asActorType(value.type),
    displayName,
    status: asActorStatus(value.status),
    isBuiltin,
    notes: value.notes,
    metadata,
    agentAdapterId: agentAdapterId ?? null,
    version: positiveInteger(value.version, "责任主体版本"),
    createdAt,
    updatedAt,
  };
}

export function nullableActorSummary(
  value: unknown,
  field: string,
): ActorSummary | null {
  if (value === null) return null;
  if (!isRecord(value)) return invalidResponse(`${field}响应无效`);
  return normalizeActorSummary(value);
}

export async function getActors(
  input: ActorListParams = {},
  signal?: AbortSignal,
): Promise<ActorListResult> {
  const requestedPage = input.page ?? 1;
  const requestedPageSize = input.pageSize ?? 50;
  if (
    !Number.isSafeInteger(requestedPage) ||
    requestedPage < 1 ||
    !Number.isSafeInteger(requestedPageSize) ||
    requestedPageSize < 1 ||
    requestedPageSize > 100 ||
    !Number.isSafeInteger((requestedPage - 1) * requestedPageSize)
  ) {
    throw new ApiError("责任主体分页参数无效", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  const params = new URLSearchParams({
    page: String(requestedPage),
    page_size: String(requestedPageSize),
  });
  if (input.type) params.set("type", input.type);
  if (input.status) params.set("status", input.status);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/actors?${params}`, {
    signal,
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("责任主体列表响应格式无效");
  }
  const items = payload.data.map(normalizeActor);
  const page = positiveInteger(payload.meta.page, "责任主体页码");
  const pageSize = positiveInteger(payload.meta.page_size, "责任主体分页大小");
  const total = nonNegativeInteger(payload.meta.total, "责任主体总数");
  const expectedItemCount = Math.min(
    requestedPageSize,
    Math.max(0, total - (requestedPage - 1) * requestedPageSize),
  );
  if (
    page !== requestedPage ||
    pageSize !== requestedPageSize ||
    items.length !== expectedItemCount ||
    new Set(items.map((actor) => actor.id)).size !== items.length ||
    (input.type !== undefined &&
      items.some((actor) => actor.type !== input.type)) ||
    (input.status !== undefined &&
      items.some((actor) => actor.status !== input.status))
  ) {
    return invalidResponse("责任主体列表分页响应无效");
  }
  return {
    items,
    meta: { page, pageSize, total },
  };
}

export async function getAllActors(
  input: Omit<ActorListParams, "page" | "pageSize"> = {},
  signal?: AbortSignal,
): Promise<Actor[]> {
  const actors: Actor[] = [];
  const actorIds = new Set<string>();
  const pageSize = 100;
  let page = 1;
  let total: number | null = null;

  while (true) {
    const result = await getActors({ ...input, page, pageSize }, signal);
    if (total === null) total = result.meta.total;
    else if (result.meta.total !== total) {
      return invalidResponse("责任主体列表分页总数不一致");
    }
    for (const actor of result.items) {
      if (actorIds.has(actor.id)) {
        return invalidResponse("责任主体列表包含重复记录");
      }
      actorIds.add(actor.id);
      actors.push(actor);
    }
    if (actors.length === total) break;
    page += 1;
  }

  return actors;
}

export async function getActor(id: string): Promise<Actor> {
  const payload = await apiRequest<unknown>(
    `/api/v1/actors/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeActor(body);
}

export async function createPersonActor(
  input: CreatePersonActorInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Actor> {
  const payload = await apiRequest<unknown>("/api/v1/actors", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      type: "person",
      display_name: input.displayName,
      notes: input.notes ?? "",
      metadata: input.metadata ?? {},
      status: input.status ?? "active",
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeActor(body);
}

export async function updateActor(
  id: string,
  input: UpdateActorInput,
): Promise<Actor> {
  const payload = await apiRequest<unknown>(
    `/api/v1/actors/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.displayName === undefined
          ? {}
          : { display_name: input.displayName }),
        ...(input.notes === undefined ? {} : { notes: input.notes }),
        ...(input.metadata === undefined ? {} : { metadata: input.metadata }),
        ...(input.status === undefined ? {} : { status: input.status }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeActor(body);
}
