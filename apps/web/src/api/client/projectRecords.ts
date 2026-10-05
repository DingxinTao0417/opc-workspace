import type {
  CreateProjectAttachmentInput,
  CreateProjectNoteInput,
  DeleteProjectAttachmentInput,
  DeleteProjectNoteInput,
  ProjectArtifactItem,
  ProjectArtifactListParams,
  ProjectArtifactListResult,
  ProjectAttachment,
  ProjectAttachmentDownload,
  ProjectAttachmentListParams,
  ProjectAttachmentListResult,
  ProjectEventListParams,
  ProjectEventListResult,
  ProjectNote,
  ProjectNoteListParams,
  ProjectNoteListResult,
  ProjectWorkflowEvent,
  UpdateProjectNoteInput,
} from "../../types/models";
import { asActorType, normalizeActorSummary } from "./actors";
import { clientOptionalString } from "./clients";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  MAX_ARTIFACT_FILE_BYTES,
  MAX_MULTIPART_REQUEST_BYTES,
  MULTIPART_ENVELOPE_RESERVE_BYTES,
  MULTIPART_FILE_PART_RESERVE_BYTES,
  apiFetch,
  apiRequest,
  downloadFileName,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableEventSnapshot,
  nullableEventString,
  positiveInteger,
  requiredNullableString,
  stringField,
} from "./core";
import { asInboxItemStatus } from "./inbox";
import { normalizeInboxTaskProgress } from "./inboxTasks";
import { normalizeTaskArtifactSummary } from "./taskOutputs";
import { asTaskStatus } from "./tasks";

function validateSingleAttachmentFile(file: File, metadata: string): void {
  if (file.size < 1) {
    throw new ApiError("附件文件不能为空", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  if (file.size > MAX_ARTIFACT_FILE_BYTES) {
    throw new ApiError("单个附件不能超过 50 MiB", {
      code: "ATTACHMENT_FILE_TOO_LARGE",
      status: 413,
    });
  }
  const encoder = new TextEncoder();
  const estimatedBytes =
    encoder.encode(metadata).byteLength +
    file.size +
    encoder.encode(file.name).byteLength * 3 +
    MULTIPART_FILE_PART_RESERVE_BYTES +
    MULTIPART_ENVELOPE_RESERVE_BYTES;
  if (estimatedBytes > MAX_MULTIPART_REQUEST_BYTES) {
    throw new ApiError("编码后的附件请求不能超过 100 MiB", {
      code: "REQUEST_TOO_LARGE",
      status: 413,
    });
  }
}

export function normalizeProjectWorkflowEvent(
  value: unknown,
): ProjectWorkflowEvent {
  if (!isRecord(value)) return invalidResponse("项目事件响应格式无效");
  const id = stringField(value, "id");
  const action = stringField(value, "action");
  const createdAt = stringField(value, "created_at", "createdAt");
  const rawActor = value.actor;
  const requestId = nullableEventString(
    fieldValue(value, "request_id", "requestId"),
    "项目事件请求 ID",
  );
  if (
    !id ||
    !action ||
    action.length > 100 ||
    !createdAt ||
    Number.isNaN(Date.parse(createdAt)) ||
    (rawActor !== null && !isRecord(rawActor)) ||
    (requestId !== null && requestId.length > 128)
  ) {
    return invalidResponse("项目事件响应格式无效");
  }
  return {
    id,
    action,
    actor: rawActor === null ? null : normalizeActorSummary(rawActor),
    requestId,
    previous: nullableEventSnapshot(value.previous, "项目事件旧快照"),
    current: nullableEventSnapshot(value.current, "项目事件新快照"),
    createdAt,
  };
}

export function normalizeProjectEventListResult(
  value: unknown,
): ProjectEventListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("项目事件列表响应格式无效");
  }
  const items = value.data.map(normalizeProjectWorkflowEvent);
  const result: ProjectEventListResult = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "项目事件页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "项目事件分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "项目事件总数"),
      projectVersion: positiveInteger(
        fieldValue(value.meta, "project_version", "projectVersion"),
        "项目事件项目版本",
      ),
    },
  };
  if (items.length > result.meta.pageSize || items.length > result.meta.total) {
    return invalidResponse("项目事件分页响应不一致");
  }
  return result;
}

export function normalizeProjectNote(value: unknown): ProjectNote {
  if (!isRecord(value)) return invalidResponse("项目笔记响应格式无效");
  const id = stringField(value, "id");
  const projectId = stringField(value, "project_id", "projectId");
  const title = stringField(value, "title");
  const occurredAt = stringField(value, "occurred_at", "occurredAt");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawCreatedBy = fieldValue(value, "created_by", "createdBy");
  if (
    !id ||
    !projectId ||
    !title ||
    !occurredAt ||
    !createdAt ||
    !updatedAt ||
    Number.isNaN(Date.parse(occurredAt)) ||
    Number.isNaN(Date.parse(createdAt)) ||
    Number.isNaN(Date.parse(updatedAt)) ||
    !isRecord(rawCreatedBy)
  ) {
    return invalidResponse("项目笔记响应格式无效");
  }
  const createdById = stringField(rawCreatedBy, "id");
  const createdByDisplayName = stringField(
    rawCreatedBy,
    "display_name",
    "displayName",
  );
  if (!createdById || !createdByDisplayName) {
    return invalidResponse("项目笔记记录人响应无效");
  }
  const body = clientOptionalString(value.body, "项目笔记正文");
  const deletedAt = clientOptionalString(
    fieldValue(value, "deleted_at", "deletedAt"),
    "项目笔记删除时间",
  );
  const deletedByActorId = clientOptionalString(
    fieldValue(value, "deleted_by_actor_id", "deletedByActorId"),
    "项目笔记删除人",
  );
  const deleteReason = clientOptionalString(
    fieldValue(value, "delete_reason", "deleteReason"),
    "项目笔记删除原因",
  );
  if (
    (deletedAt === null) !==
      (deletedByActorId === null && deleteReason === null) ||
    (deletedAt === null ? body === null : body !== null) ||
    (deletedAt !== null && Number.isNaN(Date.parse(deletedAt)))
  ) {
    return invalidResponse("项目笔记状态响应不一致");
  }
  return {
    id,
    projectId,
    title,
    body,
    occurredAt,
    createdBy: {
      id: createdById,
      type: asActorType(rawCreatedBy.type),
      displayName: createdByDisplayName,
    },
    version: positiveInteger(value.version, "项目笔记版本"),
    deletedAt,
    deletedByActorId,
    deleteReason,
    createdAt,
    updatedAt,
    projectVersion: positiveInteger(
      fieldValue(value, "project_version", "projectVersion"),
      "项目笔记对应项目版本",
    ),
  };
}

export function normalizeProjectNoteListResult(
  value: unknown,
): ProjectNoteListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("项目笔记列表响应格式无效");
  }
  const items = value.data.map(normalizeProjectNote);
  const result: ProjectNoteListResult = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "项目笔记页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "项目笔记每页数量",
      ),
      total: nonNegativeInteger(value.meta.total, "项目笔记总数"),
      projectVersion: positiveInteger(
        fieldValue(value.meta, "project_version", "projectVersion"),
        "项目笔记对应项目版本",
      ),
    },
  };
  if (
    items.length > result.meta.pageSize ||
    items.length > result.meta.total ||
    items.some((note) => note.projectVersion !== result.meta.projectVersion)
  ) {
    return invalidResponse("项目笔记分页响应不一致");
  }
  return result;
}

export function normalizeProjectAttachment(value: unknown): ProjectAttachment {
  if (!isRecord(value)) return invalidResponse("项目附件响应格式无效");
  const id = stringField(value, "id");
  const projectId = stringField(value, "project_id", "projectId");
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
    !projectId ||
    !name ||
    !mimeType ||
    !sha256 ||
    !/^[0-9a-f]{64}$/.test(sha256) ||
    !integrityCheckedAt ||
    !createdAt ||
    !isRecord(recordedBy)
  ) {
    return invalidResponse("项目附件响应格式无效");
  }
  const recordedById = stringField(recordedBy, "id");
  const recordedByName = stringField(recordedBy, "display_name", "displayName");
  if (!recordedById || !recordedByName) {
    return invalidResponse("项目附件记录人响应格式无效");
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
    return invalidResponse("项目附件完整性状态无效");
  }
  const deletedAt = clientOptionalString(
    fieldValue(value, "deleted_at", "deletedAt"),
    "项目附件删除时间",
  );
  const deletedByActorId = clientOptionalString(
    fieldValue(value, "deleted_by_actor_id", "deletedByActorId"),
    "项目附件删除人",
  );
  const deleteReason = clientOptionalString(
    fieldValue(value, "delete_reason", "deleteReason"),
    "项目附件删除原因",
  );
  if (
    (deletedAt === null) !==
    (deletedByActorId === null && deleteReason === null)
  ) {
    return invalidResponse("项目附件删除状态不一致");
  }
  return {
    id,
    projectId,
    name,
    mimeType,
    sizeBytes: positiveInteger(
      fieldValue(value, "size_bytes", "sizeBytes"),
      "项目附件大小",
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
    projectVersion: positiveInteger(
      fieldValue(value, "project_version", "projectVersion"),
      "项目附件对应项目版本",
    ),
  };
}

export function normalizeProjectArtifactItem(
  value: unknown,
): ProjectArtifactItem {
  if (!isRecord(value) || !isRecord(value.task)) {
    return invalidResponse("项目产出响应格式无效");
  }
  const artifact = normalizeTaskArtifactSummary(value.artifact);
  const taskId = stringField(value.task, "id");
  const title = stringField(value.task, "title");
  const status = asTaskStatus(value.task.status);
  const submissionSequence = positiveInteger(
    fieldValue(value, "submission_sequence", "submissionSequence"),
    "项目产出提交序号",
  );
  if (!taskId || !title || taskId !== artifact.taskId) {
    return invalidResponse("项目产出任务上下文不一致");
  }
  const rawFollowup = value.followup;
  let followup: ProjectArtifactItem["followup"] = null;
  if (rawFollowup !== null) {
    if (!isRecord(rawFollowup)) {
      return invalidResponse("项目产出跟进响应格式无效");
    }
    const inboxItemId = stringField(
      rawFollowup,
      "inbox_item_id",
      "inboxItemId",
    );
    const status = asInboxItemStatus(rawFollowup.status);
    const inboxItemVersion = positiveInteger(
      fieldValue(rawFollowup, "inbox_item_version", "inboxItemVersion"),
      "项目产出跟进版本",
    );
    const resolutionPolicy = fieldValue(
      rawFollowup,
      "resolution_policy",
      "resolutionPolicy",
    );
    const sourceDeletedAt = requiredNullableString(
      fieldValue(rawFollowup, "source_deleted_at", "sourceDeletedAt"),
      "项目产出跟进来源删除时间",
    );
    if (
      !inboxItemId ||
      (resolutionPolicy !== "manual" &&
        resolutionPolicy !== "all_required_tasks_done") ||
      (sourceDeletedAt !== null &&
        status !== "resolved" &&
        status !== "dismissed")
    ) {
      return invalidResponse("项目产出跟进响应不一致");
    }
    followup = {
      inboxItemId,
      inboxItemVersion,
      status,
      resolutionPolicy,
      sourceDeletedAt,
      progress: normalizeInboxTaskProgress(rawFollowup.progress),
    };
  }
  if (!artifact.requiresFollowup && followup !== null) {
    return invalidResponse("无需跟进的项目产出不能关联跟进事项");
  }
  return {
    artifact,
    task: { id: taskId, title, status },
    submissionSequence,
    followup,
  };
}

export async function getProjectArtifacts(
  projectId: string,
  input: ProjectArtifactListParams = {},
  signal?: AbortSignal,
): Promise<ProjectArtifactListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.includeDeleted !== undefined) {
    params.set("include_deleted", String(input.includeDeleted));
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/artifacts?${params}`,
    { signal },
  );
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("项目产出列表响应格式无效");
  }
  const items = payload.data.map(normalizeProjectArtifactItem);
  const meta = isRecord(payload.meta) ? payload.meta : {};
  const page = positiveInteger(meta.page, "项目产出页码");
  const pageSize = positiveInteger(
    fieldValue(meta, "page_size", "pageSize"),
    "项目产出每页数量",
  );
  const total = nonNegativeInteger(meta.total, "项目产出总数");
  if (
    items.some((item) => item.artifact.taskId !== item.task.id) ||
    (!input.includeDeleted &&
      items.some((item) => item.artifact.deletedAt !== null)) ||
    page !== (input.page ?? 1) ||
    pageSize !== (input.pageSize ?? 20) ||
    items.length > pageSize ||
    items.length > total ||
    items.length > Math.max(0, total - (page - 1) * pageSize)
  ) {
    return invalidResponse("项目产出列表响应与请求不一致");
  }
  return {
    items,
    meta: {
      page,
      pageSize,
      total,
      projectVersion: positiveInteger(
        fieldValue(meta, "project_version", "projectVersion"),
        "项目版本",
      ),
    },
  };
}

export async function getProjectAttachments(
  projectId: string,
  input: ProjectAttachmentListParams = {},
): Promise<ProjectAttachmentListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.includeDeleted) params.set("include_deleted", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/attachments?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("项目附件列表响应格式无效");
  }
  const items = payload.data.map(normalizeProjectAttachment);
  if (items.some((attachment) => attachment.projectId !== projectId)) {
    return invalidResponse("项目附件列表与请求不一致");
  }
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "项目附件页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "项目附件每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "项目附件总数"),
      projectVersion: positiveInteger(
        fieldValue(payload.meta, "project_version", "projectVersion"),
        "项目附件对应项目版本",
      ),
    },
  };
}

export async function getProjectAttachment(
  id: string,
): Promise<ProjectAttachment> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-attachments/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const attachment = normalizeProjectAttachment(body);
  if (attachment.id !== id) {
    return invalidResponse("项目附件详情与请求不一致");
  }
  return attachment;
}

export async function createProjectAttachment(
  projectId: string,
  input: CreateProjectAttachmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ProjectAttachment> {
  const metadata = JSON.stringify({ name: input.name });
  validateSingleAttachmentFile(input.file, metadata);
  const form = new FormData();
  form.append("metadata", metadata);
  form.append("file", input.file, input.file.name);
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/attachments`,
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
  const attachment = normalizeProjectAttachment(body);
  if (attachment.projectId !== projectId) {
    return invalidResponse("项目附件创建响应与请求不一致");
  }
  return attachment;
}

export async function deleteProjectAttachment(
  id: string,
  input: DeleteProjectAttachmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ProjectAttachment> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-attachments/${encodeURIComponent(id)}?confirm=true`,
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
  return normalizeProjectAttachment(body);
}

export async function downloadProjectAttachment(
  id: string,
  fallbackName: string,
): Promise<ProjectAttachmentDownload> {
  return apiFetch(
    `/api/v1/project-attachments/${encodeURIComponent(id)}/content`,
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

export async function getProjectEvents(
  projectId: string,
  input: ProjectEventListParams = {},
): Promise<ProjectEventListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/events?${params}`,
  );
  return normalizeProjectEventListResult(payload);
}

export async function getProjectNotes(
  projectId: string,
  input: ProjectNoteListParams = {},
): Promise<ProjectNoteListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.includeDeleted) params.set("include_deleted", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/notes?${params}`,
  );
  const result = normalizeProjectNoteListResult(payload);
  if (result.items.some((note) => note.projectId !== projectId)) {
    return invalidResponse("项目笔记列表与请求项目不一致");
  }
  return result;
}

export async function getProjectNote(
  id: string,
  signal?: AbortSignal,
): Promise<ProjectNote> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-notes/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProjectNote(body);
}

export async function createProjectNote(
  projectId: string,
  input: CreateProjectNoteInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ProjectNote> {
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(projectId)}/notes`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({
        title: input.title,
        body: input.body,
        occurred_at: input.occurredAt,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProjectNote(body);
}

export async function updateProjectNote(
  id: string,
  input: UpdateProjectNoteInput,
): Promise<ProjectNote> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-notes/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.body === undefined ? {} : { body: input.body }),
        ...(input.occurredAt === undefined
          ? {}
          : { occurred_at: input.occurredAt }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProjectNote(body);
}

export async function deleteProjectNote(
  id: string,
  input: DeleteProjectNoteInput,
): Promise<ProjectNote> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-notes/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProjectNote(body);
}
