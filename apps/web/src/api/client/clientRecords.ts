import type {
  CancelClientFollowupInput,
  ClientActivity,
  ClientActivityListParams,
  ClientActivityListResult,
  ClientActorLink,
  ClientActorLinkListParams,
  ClientActorLinkListResult,
  ClientAttachment,
  ClientAttachmentDownload,
  ClientAttachmentListParams,
  ClientAttachmentListResult,
  ClientFollowup,
  ClientFollowupListParams,
  ClientFollowupListResult,
  CompleteClientFollowupInput,
  CreateClientActivityInput,
  CreateClientActorLinkInput,
  CreateClientAttachmentInput,
  CreateClientFollowupInput,
  DeleteClientActivityInput,
  DeleteClientActorLinkInput,
  DeleteClientAttachmentInput,
  RecentClientActivity,
  RecentClientActivityListParams,
  RecentClientActivityListResult,
  RescheduleClientFollowupInput,
  RescheduleClientFollowupResult,
  SkipClientFollowupInput,
  UpdateClientActivityInput,
  UpdateClientFollowupInput,
} from "../../types/models";
import { asActorStatus, asActorType } from "./actors";
import { asClientStatus, clientOptionalString } from "./clients";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  apiFetch,
  apiRequest,
  downloadFileName,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
} from "./core";

export function normalizeClientActivity(value: unknown): ClientActivity {
  if (!isRecord(value)) return invalidResponse("客户活动响应格式无效");
  const id = stringField(value, "id");
  const clientId = stringField(value, "client_id", "clientId");
  const title = stringField(value, "title");
  const occurredAt = stringField(value, "occurred_at", "occurredAt");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawCreatedBy = fieldValue(value, "created_by", "createdBy");
  if (
    !id ||
    !clientId ||
    !title ||
    !occurredAt ||
    !createdAt ||
    !updatedAt ||
    !isRecord(rawCreatedBy)
  ) {
    return invalidResponse("客户活动响应格式无效");
  }
  const kind = value.kind;
  if (kind !== "note" && kind !== "meeting" && kind !== "system_reference") {
    return invalidResponse("客户活动类型响应无效");
  }
  const createdById = stringField(rawCreatedBy, "id");
  const createdByDisplayName = stringField(
    rawCreatedBy,
    "display_name",
    "displayName",
  );
  if (!createdById || !createdByDisplayName) {
    return invalidResponse("客户活动记录人响应无效");
  }
  const body = clientOptionalString(value.body, "客户活动正文");
  const sourceType = clientOptionalString(
    fieldValue(value, "source_type", "sourceType"),
    "客户活动来源类型",
  );
  const sourceId = clientOptionalString(
    fieldValue(value, "source_id", "sourceId"),
    "客户活动来源 ID",
  );
  const deletedAt = clientOptionalString(
    fieldValue(value, "deleted_at", "deletedAt"),
    "客户活动删除时间",
  );
  const deletedByActorId = clientOptionalString(
    fieldValue(value, "deleted_by_actor_id", "deletedByActorId"),
    "客户活动删除人",
  );
  const deleteReason = clientOptionalString(
    fieldValue(value, "delete_reason", "deleteReason"),
    "客户活动删除原因",
  );
  const sourceContractValid =
    kind === "system_reference"
      ? body === null && sourceType !== null && sourceId !== null
      : sourceType === null &&
        sourceId === null &&
        (deletedAt === null ? body !== null : body === null);
  if (
    !sourceContractValid ||
    (deletedAt === null) !==
      (deletedByActorId === null && deleteReason === null) ||
    (deletedAt !== null && body !== null)
  ) {
    return invalidResponse("客户活动状态响应不一致");
  }
  return {
    id,
    clientId,
    kind,
    title,
    body,
    occurredAt,
    createdBy: {
      id: createdById,
      type: asActorType(rawCreatedBy.type),
      displayName: createdByDisplayName,
    },
    sourceType,
    sourceId,
    version: positiveInteger(value.version, "客户活动版本"),
    deletedAt,
    deletedByActorId,
    deleteReason,
    createdAt,
    updatedAt,
    clientVersion: positiveInteger(
      fieldValue(value, "client_version", "clientVersion"),
      "客户聚合版本",
    ),
  };
}

export function normalizeRecentClientActivity(
  value: unknown,
): RecentClientActivity {
  if (!isRecord(value)) return invalidResponse("最近客户动态响应格式无效");
  const clientName = stringField(value, "client_name", "clientName");
  if (!clientName) return invalidResponse("最近客户动态缺少客户名称");
  return {
    ...normalizeClientActivity(value),
    clientName,
    clientStatus: asClientStatus(
      fieldValue(value, "client_status", "clientStatus"),
    ),
  };
}

export function normalizeClientFollowup(value: unknown): ClientFollowup {
  if (!isRecord(value)) return invalidResponse("客户回访响应格式无效");
  const id = stringField(value, "id");
  const clientId = stringField(value, "client_id", "clientId");
  const clientName = stringField(value, "client_name", "clientName");
  const assignedActorId = stringField(
    value,
    "assigned_actor_id",
    "assignedActorId",
  );
  const assignedActorName = stringField(
    value,
    "assigned_actor_name",
    "assignedActorName",
  );
  const scheduledAt = stringField(value, "scheduled_at", "scheduledAt");
  const timezone = stringField(value, "timezone");
  const channel = stringField(value, "channel");
  const purpose = stringField(value, "purpose");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  if (
    !id ||
    !clientId ||
    !clientName ||
    !assignedActorId ||
    !assignedActorName ||
    !scheduledAt ||
    !timezone ||
    !channel ||
    !purpose ||
    !createdAt ||
    !updatedAt
  ) {
    return invalidResponse("客户回访响应格式无效");
  }
  const status = value.status;
  if (
    status !== "planned" &&
    status !== "completed" &&
    status !== "skipped" &&
    status !== "cancelled"
  ) {
    return invalidResponse("客户回访状态响应无效");
  }
  const priority = value.priority;
  if (priority !== "low" && priority !== "normal" && priority !== "high") {
    return invalidResponse("客户回访优先级响应无效");
  }
  const assignedActorType = asActorType(
    fieldValue(value, "assigned_actor_type", "assignedActorType"),
  );
  if (assignedActorType !== "owner" && assignedActorType !== "person") {
    return invalidResponse("客户回访负责人响应无效");
  }
  const rawNextFollowup = fieldValue(value, "next_followup", "nextFollowup");
  const nextFollowup =
    rawNextFollowup === undefined || rawNextFollowup === null
      ? null
      : normalizeClientFollowup(rawNextFollowup);
  return {
    id,
    clientId,
    clientName,
    assignedActorId,
    assignedActorName,
    assignedActorType,
    scheduledAt,
    timezone,
    channel,
    purpose,
    notes: clientOptionalString(value.notes, "客户回访备注"),
    status,
    priority,
    completedAt: clientOptionalString(
      fieldValue(value, "completed_at", "completedAt"),
      "客户回访完成时间",
    ),
    result: clientOptionalString(value.result, "客户回访结果"),
    nextStep: clientOptionalString(
      fieldValue(value, "next_step", "nextStep"),
      "客户回访下一步",
    ),
    skippedAt: clientOptionalString(
      fieldValue(value, "skipped_at", "skippedAt"),
      "客户回访跳过时间",
    ),
    skipReason: clientOptionalString(
      fieldValue(value, "skip_reason", "skipReason"),
      "客户回访跳过原因",
    ),
    cancelledAt: clientOptionalString(
      fieldValue(value, "cancelled_at", "cancelledAt"),
      "客户回访取消时间",
    ),
    cancelReason: clientOptionalString(
      fieldValue(value, "cancel_reason", "cancelReason"),
      "客户回访取消原因",
    ),
    rescheduledFromId: clientOptionalString(
      fieldValue(value, "rescheduled_from_id", "rescheduledFromId"),
      "客户回访重排来源",
    ),
    version: positiveInteger(value.version, "客户回访版本"),
    createdAt,
    updatedAt,
    clientVersion: positiveInteger(
      fieldValue(value, "client_version", "clientVersion"),
      "客户回访对应客户版本",
    ),
    nextFollowup,
  };
}

export function normalizeClientAttachment(value: unknown): ClientAttachment {
  if (!isRecord(value)) return invalidResponse("客户附件响应格式无效");
  const id = stringField(value, "id");
  const clientId = stringField(value, "client_id", "clientId");
  const name = stringField(value, "name");
  const mimeType = stringField(value, "mime_type", "mimeType");
  const sha256 = stringField(value, "sha256");
  const integrityCheckedAt = stringField(
    value,
    "integrity_checked_at",
    "integrityCheckedAt",
  );
  const createdAt = stringField(value, "created_at", "createdAt");
  const recordedBy = fieldValue(value, "recorded_by", "recordedBy");
  if (
    !id ||
    !clientId ||
    !name ||
    !mimeType ||
    !sha256 ||
    !/^[0-9a-f]{64}$/.test(sha256) ||
    !integrityCheckedAt ||
    !createdAt ||
    !isRecord(recordedBy)
  ) {
    return invalidResponse("客户附件响应格式无效");
  }
  const recordedById = stringField(recordedBy, "id");
  const recordedByName = stringField(recordedBy, "display_name", "displayName");
  if (!recordedById || !recordedByName) {
    return invalidResponse("客户附件记录人响应格式无效");
  }
  const integrityStatus = fieldValue(
    value,
    "integrity_status",
    "integrityStatus",
  );
  if (
    integrityStatus !== "verified" &&
    integrityStatus !== "missing" &&
    integrityStatus !== "mismatch"
  ) {
    return invalidResponse("客户附件完整性状态无效");
  }
  const activityId = clientOptionalString(
    fieldValue(value, "activity_id", "activityId"),
    "客户附件关联活动",
  );
  const deletedAt = clientOptionalString(
    fieldValue(value, "deleted_at", "deletedAt"),
    "客户附件删除时间",
  );
  const deletedByActorId = clientOptionalString(
    fieldValue(value, "deleted_by_actor_id", "deletedByActorId"),
    "客户附件删除人",
  );
  const deleteReason = clientOptionalString(
    fieldValue(value, "delete_reason", "deleteReason"),
    "客户附件删除原因",
  );
  if (
    (deletedAt === null) !==
    (deletedByActorId === null && deleteReason === null)
  ) {
    return invalidResponse("客户附件删除状态不一致");
  }
  return {
    id,
    clientId,
    activityId,
    name,
    mimeType,
    sizeBytes: positiveInteger(
      fieldValue(value, "size_bytes", "sizeBytes"),
      "客户附件大小",
    ),
    sha256,
    recordedBy: {
      id: recordedById,
      type: asActorType(recordedBy.type),
      displayName: recordedByName,
    },
    integrityStatus,
    integrityCheckedAt,
    deletedAt,
    deletedByActorId,
    deleteReason,
    createdAt,
    clientVersion: positiveInteger(
      fieldValue(value, "client_version", "clientVersion"),
      "客户附件对应客户版本",
    ),
  };
}

export function normalizeClientActorLink(value: unknown): ClientActorLink {
  if (!isRecord(value)) return invalidResponse("客户责任关联响应格式无效");
  const id = stringField(value, "id");
  const clientId = stringField(value, "client_id", "clientId");
  const linkedAt = stringField(value, "linked_at", "linkedAt");
  const rawActor = fieldValue(value, "actor");
  const rawLinkedBy = fieldValue(value, "linked_by", "linkedBy");
  const rawUnlinkedBy = fieldValue(value, "unlinked_by", "unlinkedBy");
  if (
    !id ||
    !clientId ||
    !linkedAt ||
    value.role !== "contact" ||
    !isRecord(rawActor) ||
    !isRecord(rawLinkedBy) ||
    (rawUnlinkedBy !== null && !isRecord(rawUnlinkedBy))
  ) {
    return invalidResponse("客户责任关联响应格式无效");
  }
  const actorId = stringField(rawActor, "id");
  const actorDisplayName = stringField(rawActor, "display_name", "displayName");
  const linkedById = stringField(rawLinkedBy, "id");
  const linkedByDisplayName = stringField(
    rawLinkedBy,
    "display_name",
    "displayName",
  );
  if (!actorId || !actorDisplayName || !linkedById || !linkedByDisplayName) {
    return invalidResponse("客户责任关联人员响应格式无效");
  }
  const unlinkedAt = clientOptionalString(
    fieldValue(value, "unlinked_at", "unlinkedAt"),
    "客户责任解除时间",
  );
  const unlinkReason = clientOptionalString(
    fieldValue(value, "unlink_reason", "unlinkReason"),
    "客户责任解除原因",
  );
  let unlinkedBy: ClientActorLink["unlinkedBy"] = null;
  if (isRecord(rawUnlinkedBy)) {
    const unlinkedById = stringField(rawUnlinkedBy, "id");
    const unlinkedByDisplayName = stringField(
      rawUnlinkedBy,
      "display_name",
      "displayName",
    );
    if (!unlinkedById || !unlinkedByDisplayName) {
      return invalidResponse("客户责任解除人员响应格式无效");
    }
    unlinkedBy = {
      id: unlinkedById,
      type: asActorType(rawUnlinkedBy.type),
      displayName: unlinkedByDisplayName,
    };
  }
  if (
    (unlinkedAt === null) !==
    (unlinkedBy === null && unlinkReason === null)
  ) {
    return invalidResponse("客户责任关联历史状态不一致");
  }
  return {
    id,
    clientId,
    role: "contact",
    actor: {
      id: actorId,
      type: asActorType(rawActor.type),
      displayName: actorDisplayName,
      status: asActorStatus(rawActor.status),
      version: positiveInteger(rawActor.version, "客户关联人员版本"),
    },
    linkedBy: {
      id: linkedById,
      type: asActorType(rawLinkedBy.type),
      displayName: linkedByDisplayName,
    },
    linkedAt,
    unlinkedAt,
    unlinkedBy,
    unlinkReason,
    clientVersion: positiveInteger(
      fieldValue(value, "client_version", "clientVersion"),
      "客户责任关联对应客户版本",
    ),
  };
}

export async function getClientActivities(
  clientId: string,
  input: ClientActivityListParams = {},
): Promise<ClientActivityListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.kind) params.set("kind", input.kind);
  if (input.includeDeleted) params.set("include_deleted", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/activities?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户活动列表响应格式无效");
  }
  return {
    items: payload.data.map(normalizeClientActivity),
    meta: {
      page: positiveInteger(payload.meta.page, "客户活动页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户活动每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户活动总数"),
      clientVersion: positiveInteger(
        fieldValue(payload.meta, "client_version", "clientVersion"),
        "客户活动对应客户版本",
      ),
    },
  };
}

export async function getRecentClientActivities(
  input: RecentClientActivityListParams = {},
): Promise<RecentClientActivityListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 5),
  });
  if (input.kind) params.set("kind", input.kind);
  const payload = await apiRequest<unknown>(
    `/api/v1/client-activities?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("最近客户动态列表响应格式无效");
  }
  return {
    items: payload.data.map(normalizeRecentClientActivity),
    meta: {
      page: positiveInteger(payload.meta.page, "最近客户动态页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "最近客户动态每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "最近客户动态总数"),
    },
  };
}

export async function getClientFollowups(
  clientId: string,
  input: ClientFollowupListParams = {},
): Promise<ClientFollowupListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.status) params.set("status", input.status);
  if (input.dueState) params.set("due_state", input.dueState);
  if (input.assignedActorId?.trim()) {
    params.set("assigned_actor_id", input.assignedActorId.trim());
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/followups?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户回访列表响应格式无效");
  }
  const serverNow = stringField(payload.meta, "server_now", "serverNow");
  if (!serverNow) return invalidResponse("客户回访列表缺少服务端时间基准");
  return {
    items: payload.data.map(normalizeClientFollowup),
    meta: {
      page: positiveInteger(payload.meta.page, "客户回访页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户回访每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户回访总数"),
      serverNow,
    },
  };
}

export async function createClientFollowup(
  input: CreateClientFollowupInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientFollowup> {
  const payload = await apiRequest<unknown>("/api/v1/client-followups", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      client_id: input.clientId,
      assigned_actor_id: input.assignedActorId,
      scheduled_at: input.scheduledAt,
      timezone: input.timezone,
      channel: input.channel,
      purpose: input.purpose,
      notes: input.notes,
      priority: input.priority,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientFollowup(body);
}

export async function updateClientFollowup(
  id: string,
  input: UpdateClientFollowupInput,
): Promise<ClientFollowup> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-followups/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.assignedActorId === undefined
          ? {}
          : { assigned_actor_id: input.assignedActorId }),
        ...(input.scheduledAt === undefined
          ? {}
          : { scheduled_at: input.scheduledAt }),
        ...(input.timezone === undefined ? {} : { timezone: input.timezone }),
        ...(input.channel === undefined ? {} : { channel: input.channel }),
        ...(input.purpose === undefined ? {} : { purpose: input.purpose }),
        ...(input.notes === undefined ? {} : { notes: input.notes }),
        ...(input.priority === undefined ? {} : { priority: input.priority }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientFollowup(body);
}

export async function completeClientFollowup(
  id: string,
  input: CompleteClientFollowupInput,
): Promise<ClientFollowup> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-followups/${encodeURIComponent(id)}/complete`,
    {
      method: "POST",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        result: input.result,
        next_step: input.nextStep,
        completed_at: input.completedAt,
        next_followup: input.nextFollowup
          ? {
              assigned_actor_id: input.nextFollowup.assignedActorId,
              scheduled_at: input.nextFollowup.scheduledAt,
              timezone: input.nextFollowup.timezone,
              channel: input.nextFollowup.channel,
              purpose: input.nextFollowup.purpose,
              notes: input.nextFollowup.notes,
              priority: input.nextFollowup.priority,
            }
          : null,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientFollowup(body);
}

async function transitionClientFollowup(
  id: string,
  action: "skip" | "cancel",
  input: SkipClientFollowupInput | CancelClientFollowupInput,
): Promise<ClientFollowup> {
  const suffix = action === "cancel" ? "?confirm=true" : "";
  const payload = await apiRequest<unknown>(
    `/api/v1/client-followups/${encodeURIComponent(id)}/${action}${suffix}`,
    {
      method: action === "skip" ? "POST" : "DELETE",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientFollowup(body);
}

export function skipClientFollowup(
  id: string,
  input: SkipClientFollowupInput,
): Promise<ClientFollowup> {
  return transitionClientFollowup(id, "skip", input);
}

export function cancelClientFollowup(
  id: string,
  input: CancelClientFollowupInput,
): Promise<ClientFollowup> {
  return transitionClientFollowup(id, "cancel", input);
}

export async function rescheduleClientFollowup(
  id: string,
  input: RescheduleClientFollowupInput,
): Promise<RescheduleClientFollowupResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-followups/${encodeURIComponent(id)}/reschedule`,
    {
      method: "POST",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        assigned_actor_id: input.assignedActorId,
        scheduled_at: input.scheduledAt,
        timezone: input.timezone,
        channel: input.channel,
        purpose: input.purpose,
        notes: input.notes,
        priority: input.priority,
        reason: input.reason,
      }),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("客户回访重排响应格式无效");
  return {
    previous: normalizeClientFollowup(body.previous),
    next: normalizeClientFollowup(body.next),
  };
}

export async function getClientActivity(
  id: string,
  signal?: AbortSignal,
): Promise<ClientActivity> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-activities/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActivity(body);
}

export async function getClientFollowup(
  id: string,
  signal?: AbortSignal,
): Promise<ClientFollowup> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-followups/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientFollowup(body);
}

export async function createClientActivity(
  clientId: string,
  input: CreateClientActivityInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientActivity> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/activities`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({
        kind: input.kind,
        title: input.title,
        body: input.body,
        occurred_at: input.occurredAt,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActivity(body);
}

export async function updateClientActivity(
  id: string,
  input: UpdateClientActivityInput,
): Promise<ClientActivity> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-activities/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.kind === undefined ? {} : { kind: input.kind }),
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.body === undefined ? {} : { body: input.body }),
        ...(input.occurredAt === undefined
          ? {}
          : { occurred_at: input.occurredAt }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActivity(body);
}

export async function deleteClientActivity(
  id: string,
  input: DeleteClientActivityInput,
): Promise<ClientActivity> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-activities/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActivity(body);
}

export async function getClientAttachments(
  clientId: string,
  input: ClientAttachmentListParams = {},
): Promise<ClientAttachmentListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.activityId) params.set("activity_id", input.activityId);
  if (input.includeDeleted) params.set("include_deleted", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/attachments?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户附件列表响应格式无效");
  }
  const items = payload.data.map(normalizeClientAttachment);
  if (items.some((attachment) => attachment.clientId !== clientId)) {
    return invalidResponse("客户附件列表与请求不一致");
  }
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "客户附件页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户附件每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户附件总数"),
      clientVersion: positiveInteger(
        fieldValue(payload.meta, "client_version", "clientVersion"),
        "客户附件对应客户版本",
      ),
    },
  };
}

export async function getClientAttachment(
  id: string,
): Promise<ClientAttachment> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-attachments/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const attachment = normalizeClientAttachment(body);
  if (attachment.id !== id) {
    return invalidResponse("客户附件详情与请求不一致");
  }
  return attachment;
}

export async function createClientAttachment(
  clientId: string,
  input: CreateClientAttachmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientAttachment> {
  const form = new FormData();
  form.append(
    "metadata",
    JSON.stringify({
      name: input.name,
      activity_id: input.activityId ?? null,
    }),
  );
  form.append("file", input.file, input.file.name);
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/attachments`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: form,
    },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const attachment = normalizeClientAttachment(body);
  if (attachment.clientId !== clientId) {
    return invalidResponse("客户附件创建响应与请求不一致");
  }
  return attachment;
}

export async function deleteClientAttachment(
  id: string,
  input: DeleteClientAttachmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientAttachment> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-attachments/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientAttachment(body);
}

export async function downloadClientAttachment(
  id: string,
  fallbackName: string,
): Promise<ClientAttachmentDownload> {
  return apiFetch(
    `/api/v1/client-attachments/${encodeURIComponent(id)}/content`,
    async (response) => {
      const blob = await response.blob();
      const mimeType = response.headers.get("Content-Type") ?? blob.type;
      return {
        blob,
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          fallbackName,
        ),
        mimeType: mimeType || "application/octet-stream",
      };
    },
    {},
    "application/octet-stream",
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
}

export async function getClientActorLinks(
  clientId: string,
  input: ClientActorLinkListParams = {},
  signal?: AbortSignal,
): Promise<ClientActorLinkListResult> {
  if (input.state !== undefined && input.includeUnlinked !== undefined) {
    throw new ApiError("客户责任关联筛选条件不能同时使用", {
      code: "INVALID_FILTER",
    });
  }
  const state = input.state ?? (input.includeUnlinked ? "all" : "active");
  if (state !== "active" && state !== "unlinked" && state !== "all") {
    throw new ApiError("客户责任关联筛选条件无效", {
      code: "INVALID_FILTER",
    });
  }
  const requestedPage = input.page ?? 1;
  const requestedPageSize = input.pageSize ?? 20;
  const params = new URLSearchParams({
    page: String(requestedPage),
    page_size: String(requestedPageSize),
  });
  if (input.state) params.set("state", input.state);
  if (input.includeUnlinked) params.set("include_unlinked", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/actor-links?${params}`,
    { signal },
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户责任关联列表响应格式无效");
  }
  const items = payload.data.map(normalizeClientActorLink);
  const page = positiveInteger(payload.meta.page, "客户责任关联页码");
  const pageSize = positiveInteger(
    fieldValue(payload.meta, "page_size", "pageSize"),
    "客户责任关联每页数量",
  );
  const total = nonNegativeInteger(payload.meta.total, "客户责任关联总数");
  const clientVersion = positiveInteger(
    fieldValue(payload.meta, "client_version", "clientVersion"),
    "客户责任关联对应客户版本",
  );
  const expectedItemCount = Math.min(
    pageSize,
    Math.max(0, total - (page - 1) * pageSize),
  );
  if (
    page !== requestedPage ||
    pageSize !== requestedPageSize ||
    items.length !== expectedItemCount ||
    new Set(items.map((link) => link.id)).size !== items.length ||
    items.some(
      (link) =>
        link.clientId !== clientId || link.clientVersion !== clientVersion,
    ) ||
    (state === "active" &&
      (total > 1 || items.some((link) => link.unlinkedAt !== null))) ||
    (state === "unlinked" && items.some((link) => link.unlinkedAt === null))
  ) {
    return invalidResponse("客户责任关联列表与请求不一致");
  }
  return {
    items,
    meta: {
      page,
      pageSize,
      total,
      clientVersion,
    },
  };
}

export async function createClientActorLink(
  clientId: string,
  input: CreateClientActorLinkInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientActorLink> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/actor-links`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify({
        role: "contact",
        ...("actorId" in input
          ? { actor_id: input.actorId }
          : {
              create_person: {
                display_name: input.createPerson.displayName,
                notes: input.createPerson.notes,
              },
            }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const link = normalizeClientActorLink(body);
  if (link.clientId !== clientId) {
    return invalidResponse("客户责任关联创建响应与请求不一致");
  }
  return link;
}

export async function deleteClientActorLink(
  id: string,
  input: DeleteClientActorLinkInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ClientActorLink> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-actor-links/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActorLink(body);
}
