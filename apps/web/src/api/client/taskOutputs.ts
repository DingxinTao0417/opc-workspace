import type {
  DeleteTaskArtifactInput,
  DeleteTaskArtifactResult,
  ReviewTaskSubmissionInput,
  ReviewTaskSubmissionResult,
  SubmitTaskOutputInput,
  SubmitTaskOutputResult,
  TaskAggregateListMeta,
  TaskArtifact,
  TaskArtifactDownload,
  TaskArtifactListParams,
  TaskArtifactListResult,
  TaskArtifactStorageKind,
  TaskSubmission,
  TaskSubmissionListParams,
  TaskSubmissionListResult,
} from "../../types/models";
import { normalizeActorSummary, nullableActorSummary } from "./actors";
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
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  requiredNullableString,
  stringField,
  versionedCommandHeaders,
} from "./core";
import { normalizeTask, normalizeTaskWorkflowEvent } from "./tasks";

const MAX_JSON_REQUEST_BYTES = 1_024 * 1_024;

function asArtifactStorageKind(value: unknown): TaskArtifactStorageKind {
  if (
    value === "text" ||
    value === "link" ||
    value === "structured" ||
    value === "file"
  ) {
    return value;
  }
  return invalidResponse("任务产出类型响应无效");
}

function asArtifactIntegrityStatus(
  value: unknown,
): TaskArtifact["integrityStatus"] {
  if (
    value === "unverified" ||
    value === "verified" ||
    value === "missing" ||
    value === "mismatch"
  ) {
    return value;
  }
  return invalidResponse("任务产出完整性状态响应无效");
}

function asSubmissionStatus(value: unknown): TaskSubmission["status"] {
  if (
    value === "pending_review" ||
    value === "accepted" ||
    value === "changes_requested" ||
    value === "withdrawn"
  ) {
    return value;
  }
  return invalidResponse("任务提交状态响应无效");
}

function asTaskSubmissionOrigin(value: unknown): TaskSubmission["origin"] {
  // Durable v29 idempotency snapshots do not carry origin; those records were
  // all created by the manual/migration path and are safe to normalize.
  if (value === undefined || value === "manual") return "manual";
  if (value === "child_rollup") return value;
  return invalidResponse("任务提交来源响应无效");
}

export function normalizeTaskArtifactSummary(
  value: unknown,
): TaskArtifactListResult["items"][number] {
  if (!isRecord(value)) return invalidResponse("任务产出摘要响应格式无效");
  const id = stringField(value, "id");
  const taskId = stringField(value, "task_id", "taskId");
  const submissionId = stringField(value, "submission_id", "submissionId");
  const submissionStatus = asSubmissionStatus(
    fieldValue(value, "submission_status", "submissionStatus"),
  );
  const name = stringField(value, "name");
  const producedByActorId = stringField(
    value,
    "produced_by_actor_id",
    "producedByActorId",
  );
  const recordedByActorId = stringField(
    value,
    "recorded_by_actor_id",
    "recordedByActorId",
  );
  const createdAt = stringField(value, "created_at", "createdAt");
  const rawRequiresFollowup = fieldValue(
    value,
    "requires_followup",
    "requiresFollowup",
  );
  if (
    !id ||
    !taskId ||
    !submissionId ||
    !name ||
    !producedByActorId ||
    !recordedByActorId ||
    !createdAt ||
    typeof rawRequiresFollowup !== "boolean"
  ) {
    return invalidResponse("任务产出摘要响应格式无效");
  }
  const producedByActor = normalizeActorSummary(
    fieldValue(value, "produced_by_actor", "producedByActor"),
  );
  const recordedByActor = normalizeActorSummary(
    fieldValue(value, "recorded_by_actor", "recordedByActor"),
  );
  const deletedByActor = nullableActorSummary(
    fieldValue(value, "deleted_by_actor", "deletedByActor"),
    "任务产出删除人",
  );
  const deletedByActorId = requiredNullableString(
    fieldValue(value, "deleted_by_actor_id", "deletedByActorId"),
    "任务产出删除人 ID",
  );
  if (
    producedByActor.id !== producedByActorId ||
    recordedByActor.id !== recordedByActorId ||
    (deletedByActor?.id ?? null) !== deletedByActorId
  ) {
    return invalidResponse("任务产出责任主体响应不一致");
  }
  const mimeType = requiredNullableString(
    fieldValue(value, "mime_type", "mimeType"),
    "任务产出 MIME 类型",
  );
  const sha256 = requiredNullableString(value.sha256, "任务产出校验值");
  if (sha256 !== null && !/^[0-9a-f]{64}$/.test(sha256)) {
    return invalidResponse("任务产出校验值响应无效");
  }
  const rawSizeBytes = fieldValue(value, "size_bytes", "sizeBytes");
  if (rawSizeBytes === undefined) {
    return invalidResponse("任务产出大小响应无效");
  }
  const sizeBytes =
    rawSizeBytes === null
      ? null
      : nonNegativeInteger(rawSizeBytes, "任务产出大小");
  const storageKind = asArtifactStorageKind(
    fieldValue(value, "storage_kind", "storageKind"),
  );
  if (
    (storageKind === "file" &&
      (mimeType === null ||
        sizeBytes === null ||
        sizeBytes < 1 ||
        sha256 === null)) ||
    (storageKind !== "file" &&
      (mimeType !== null || sizeBytes !== null || sha256 !== null))
  ) {
    return invalidResponse("任务产出存储元数据与类型不一致");
  }
  const integrityStatus = asArtifactIntegrityStatus(
    fieldValue(value, "integrity_status", "integrityStatus"),
  );
  const integrityCheckedAt = requiredNullableString(
    fieldValue(value, "integrity_checked_at", "integrityCheckedAt"),
    "任务产出完整性检查时间",
  );
  if (
    (integrityStatus === "unverified" && integrityCheckedAt !== null) ||
    (integrityStatus !== "unverified" && integrityCheckedAt === null)
  ) {
    return invalidResponse("任务产出完整性状态响应不一致");
  }
  const deletedAt = requiredNullableString(
    fieldValue(value, "deleted_at", "deletedAt"),
    "任务产出删除时间",
  );
  const deleteReason = requiredNullableString(
    fieldValue(value, "delete_reason", "deleteReason"),
    "任务产出删除原因",
  );
  if (
    (deletedAt === null &&
      (deletedByActorId !== null || deleteReason !== null)) ||
    (deletedAt !== null && (deletedByActorId === null || deleteReason === null))
  ) {
    return invalidResponse("任务产出删除状态响应不一致");
  }
  return {
    id,
    taskId,
    submissionId,
    submissionStatus,
    position: positiveInteger(value.position, "任务产出位置"),
    storageKind,
    name,
    mimeType,
    sizeBytes,
    sha256,
    requiresFollowup: rawRequiresFollowup,
    producedByActorId,
    producedByActor,
    recordedByActorId,
    recordedByActor,
    integrityStatus,
    integrityCheckedAt,
    deletedAt,
    deletedByActorId,
    deletedByActor,
    deleteReason,
    createdAt,
  };
}

export function normalizeTaskArtifact(value: unknown): TaskArtifact {
  if (!isRecord(value)) return invalidResponse("任务产出详情响应格式无效");
  const summary = normalizeTaskArtifactSummary(value);
  const contentText = requiredNullableString(
    fieldValue(value, "content_text", "contentText"),
    "任务文本产出",
  );
  const referenceUrl = requiredNullableString(
    fieldValue(value, "reference_url", "referenceUrl"),
    "任务链接产出",
  );
  const rawStructured = fieldValue(value, "structured_json", "structuredJson");
  const structuredJson =
    rawStructured === null
      ? null
      : isRecord(rawStructured)
        ? rawStructured
        : invalidResponse("任务结构化产出响应无效");
  const activePayloads = [contentText, referenceUrl, structuredJson].filter(
    (item) => item !== null,
  ).length;
  if (
    (summary.deletedAt !== null && activePayloads !== 0) ||
    (summary.deletedAt === null &&
      ((summary.storageKind === "text" &&
        (contentText === null || activePayloads !== 1)) ||
        (summary.storageKind === "link" &&
          (referenceUrl === null || activePayloads !== 1)) ||
        (summary.storageKind === "structured" &&
          (structuredJson === null || activePayloads !== 1)) ||
        (summary.storageKind === "file" && activePayloads !== 0)))
  ) {
    return invalidResponse("任务产出详情与类型不一致");
  }
  return { ...summary, contentText, referenceUrl, structuredJson };
}

export function normalizeTaskSubmission(value: unknown): TaskSubmission {
  if (!isRecord(value)) return invalidResponse("任务提交响应格式无效");
  const id = stringField(value, "id");
  const taskId = stringField(value, "task_id", "taskId");
  const submittedByActorId = stringField(
    value,
    "submitted_by_actor_id",
    "submittedByActorId",
  );
  const submittedAt = stringField(value, "submitted_at", "submittedAt");
  if (
    !id ||
    !taskId ||
    !submittedByActorId ||
    !submittedAt ||
    typeof value.summary !== "string" ||
    typeof fieldValue(value, "is_inferred", "isInferred") !== "boolean" ||
    !Array.isArray(value.artifacts)
  ) {
    return invalidResponse("任务提交响应格式无效");
  }
  const submittedByActor = normalizeActorSummary(
    fieldValue(value, "submitted_by_actor", "submittedByActor"),
  );
  const reviewedByActorId = requiredNullableString(
    fieldValue(value, "reviewed_by_actor_id", "reviewedByActorId"),
    "任务审核人 ID",
  );
  const reviewedByActor = nullableActorSummary(
    fieldValue(value, "reviewed_by_actor", "reviewedByActor"),
    "任务审核人",
  );
  const withdrawnByActorId = requiredNullableString(
    fieldValue(value, "withdrawn_by_actor_id", "withdrawnByActorId"),
    "任务撤回人 ID",
  );
  const withdrawnByActor = nullableActorSummary(
    fieldValue(value, "withdrawn_by_actor", "withdrawnByActor"),
    "任务撤回人",
  );
  if (
    submittedByActor.id !== submittedByActorId ||
    (reviewedByActor?.id ?? null) !== reviewedByActorId ||
    (withdrawnByActor?.id ?? null) !== withdrawnByActorId
  ) {
    return invalidResponse("任务提交责任主体响应不一致");
  }
  const status = asSubmissionStatus(value.status);
  const origin = asTaskSubmissionOrigin(value.origin);
  const artifacts = value.artifacts.map(normalizeTaskArtifactSummary);
  if (
    artifacts.some(
      (artifact) =>
        artifact.taskId !== taskId ||
        artifact.submissionId !== id ||
        artifact.submissionStatus !== status,
    )
  ) {
    return invalidResponse("任务提交产出响应不一致");
  }
  if (
    origin === "child_rollup" &&
    (submittedByActor.type !== "system" ||
      !submittedByActor.isBuiltin ||
      fieldValue(value, "is_inferred", "isInferred") !== false ||
      artifacts.length !== 0)
  ) {
    return invalidResponse("父任务汇总提交来源不一致");
  }
  return {
    id,
    taskId,
    sequence: positiveInteger(value.sequence, "任务提交序号"),
    status,
    origin,
    summary: value.summary,
    submittedByActorId,
    submittedByActor,
    submittedAt,
    reviewedByActorId,
    reviewedByActor,
    reviewedAt: requiredNullableString(
      fieldValue(value, "reviewed_at", "reviewedAt"),
      "任务审核时间",
    ),
    reviewReason: requiredNullableString(
      fieldValue(value, "review_reason", "reviewReason"),
      "任务审核原因",
    ),
    withdrawnByActorId,
    withdrawnByActor,
    withdrawnAt: requiredNullableString(
      fieldValue(value, "withdrawn_at", "withdrawnAt"),
      "任务撤回时间",
    ),
    isInferred: fieldValue(value, "is_inferred", "isInferred") as boolean,
    artifacts,
  };
}

function normalizeTaskAggregateMeta(
  value: unknown,
  itemCount: number,
  label: string,
): TaskAggregateListMeta {
  if (!isRecord(value)) return invalidResponse(`${label}分页响应格式无效`);
  const meta = {
    page: positiveInteger(value.page, `${label}页码`),
    pageSize: positiveInteger(
      fieldValue(value, "page_size", "pageSize"),
      `${label}分页大小`,
    ),
    total: nonNegativeInteger(value.total, `${label}总数`),
    taskVersion: positiveInteger(
      fieldValue(value, "task_version", "taskVersion"),
      `${label}任务版本`,
    ),
  };
  if (itemCount > meta.pageSize || itemCount > meta.total) {
    return invalidResponse(`${label}分页响应不一致`);
  }
  return meta;
}

export function normalizeTaskSubmissionListResult(
  value: unknown,
): TaskSubmissionListResult {
  if (!isRecord(value) || !Array.isArray(value.data)) {
    return invalidResponse("任务提交列表响应格式无效");
  }
  const items = value.data.map(normalizeTaskSubmission);
  return {
    items,
    meta: normalizeTaskAggregateMeta(value.meta, items.length, "任务提交"),
  };
}

export function normalizeTaskArtifactListResult(
  value: unknown,
): TaskArtifactListResult {
  if (!isRecord(value) || !Array.isArray(value.data)) {
    return invalidResponse("任务产出列表响应格式无效");
  }
  const items = value.data.map(normalizeTaskArtifactSummary);
  return {
    items,
    meta: normalizeTaskAggregateMeta(value.meta, items.length, "任务产出"),
  };
}

export async function getTaskSubmissions(
  taskId: string,
  input: TaskSubmissionListParams = {},
): Promise<TaskSubmissionListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/submissions?${params}`,
  );
  const result = normalizeTaskSubmissionListResult(payload);
  if (
    result.items.some((submission) => submission.taskId !== taskId) ||
    result.items.some(
      (submission, index, items) =>
        index > 0 && items[index - 1].sequence <= submission.sequence,
    )
  ) {
    return invalidResponse("任务提交列表响应与请求不一致");
  }
  return result;
}

export async function getTaskArtifacts(
  taskId: string,
  input: TaskArtifactListParams = {},
): Promise<TaskArtifactListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.submissionId) params.set("submission_id", input.submissionId);
  if (input.includeDeleted !== undefined) {
    params.set("include_deleted", String(input.includeDeleted));
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/artifacts?${params}`,
  );
  const result = normalizeTaskArtifactListResult(payload);
  if (
    result.items.some((artifact) => artifact.taskId !== taskId) ||
    (input.submissionId &&
      result.items.some(
        (artifact) => artifact.submissionId !== input.submissionId,
      )) ||
    (!input.includeDeleted &&
      result.items.some((artifact) => artifact.deletedAt !== null))
  ) {
    return invalidResponse("任务产出列表响应与请求不一致");
  }
  return result;
}

export async function getTaskArtifact(
  id: string,
  signal?: AbortSignal,
): Promise<TaskArtifact> {
  const payload = await apiRequest<unknown>(
    `/api/v1/artifacts/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const artifact = normalizeTaskArtifact(body);
  if (artifact.id !== id) return invalidResponse("任务产出详情与请求不一致");
  return artifact;
}

export async function downloadTaskArtifact(
  id: string,
  fallbackName: string,
  signal?: AbortSignal,
): Promise<TaskArtifactDownload> {
  return apiFetch(
    `/api/v1/artifacts/${encodeURIComponent(id)}/content`,
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
    { signal },
    "application/octet-stream",
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
}

function serializeNewArtifact(
  artifact: SubmitTaskOutputInput["artifacts"][number],
  fileField?: string,
): Record<string, unknown> {
  const common = {
    client_ref: artifact.clientRef,
    storage_kind: artifact.storageKind,
    name: artifact.name,
    requires_followup: artifact.requiresFollowup,
  };
  switch (artifact.storageKind) {
    case "text":
      return { ...common, content_text: artifact.contentText };
    case "link":
      return { ...common, reference_url: artifact.referenceUrl };
    case "structured":
      return { ...common, structured_json: artifact.structuredJson };
    case "file":
      if (!fileField)
        throw new ApiError("文件产出缺少上传字段", { code: "INVALID_FILE" });
      return { ...common, file_field: fileField };
  }
}

function normalizeSubmitOutputResult(
  value: unknown,
  taskId: string,
): SubmitTaskOutputResult {
  if (
    !isRecord(value) ||
    !isRecord(value.data) ||
    !Array.isArray(value.data.artifacts)
  ) {
    return invalidResponse("提交任务产出响应格式无效");
  }
  const task = normalizeTask(value.data.task);
  const submission = normalizeTaskSubmission(value.data.submission);
  const artifacts = value.data.artifacts.map(normalizeTaskArtifactSummary);
  const event = normalizeTaskWorkflowEvent(value.data.event);
  if (
    task.id !== taskId ||
    submission.taskId !== taskId ||
    submission.id !== task.currentSubmissionId ||
    event.action !== "task_output_submitted" ||
    event.submissionId !== submission.id ||
    event.artifactId !== null ||
    artifacts.some(
      (artifact) =>
        artifact.taskId !== taskId ||
        artifact.submissionId !== submission.id ||
        artifact.submissionStatus !== submission.status,
    ) ||
    artifacts.length !== submission.artifacts.length
  ) {
    return invalidResponse("提交任务产出响应不一致");
  }
  return { task, submission, artifacts, event };
}

export async function submitTaskOutput(
  taskId: string,
  input: SubmitTaskOutputInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<SubmitTaskOutputResult> {
  const files = input.artifacts.filter(
    (
      artifact,
    ): artifact is Extract<
      SubmitTaskOutputInput["artifacts"][number],
      { storageKind: "file" }
    > => artifact.storageKind === "file",
  );
  const headers = versionedCommandHeaders(
    input.expectedVersion,
    idempotencyKey,
  );
  let body: BodyInit;
  if (files.length === 0) {
    const jsonBody = JSON.stringify({
      summary: input.summary,
      artifacts: input.artifacts.map((artifact) =>
        serializeNewArtifact(artifact),
      ),
    });
    if (
      new TextEncoder().encode(jsonBody).byteLength > MAX_JSON_REQUEST_BYTES
    ) {
      throw new ApiError("JSON 请求不能超过 1 MiB", {
        code: "REQUEST_TOO_LARGE",
        status: 413,
      });
    }
    body = jsonBody;
  } else {
    const form = new FormData();
    const fileFields = new Map<string, string>();
    files.forEach((artifact, index) => {
      const field = `artifact_file_${index + 1}`;
      fileFields.set(artifact.clientRef, field);
    });
    const manifest = JSON.stringify({
      summary: input.summary,
      artifacts: input.artifacts.map((artifact) =>
        serializeNewArtifact(artifact, fileFields.get(artifact.clientRef)),
      ),
    });
    const manifestBytes = new TextEncoder().encode(manifest).byteLength;
    if (manifestBytes > MAX_JSON_REQUEST_BYTES) {
      throw new ApiError("文件提交清单不能超过 1 MiB", {
        code: "REQUEST_TOO_LARGE",
        status: 413,
      });
    }
    for (const artifact of files) {
      if (artifact.file.size < 1) {
        throw new ApiError("文件产出不能为空", {
          code: "VALIDATION_ERROR",
          status: 422,
        });
      }
      if (artifact.file.size > MAX_ARTIFACT_FILE_BYTES) {
        throw new ApiError("单个文件产出不能超过 50 MiB", {
          code: "ARTIFACT_FILE_TOO_LARGE",
          status: 413,
        });
      }
    }
    const encoder = new TextEncoder();
    const encodedRequestEstimate = files.reduce((total, artifact) => {
      const field = fileFields.get(artifact.clientRef) ?? "";
      const headerTextBytes =
        encoder.encode(field).byteLength +
        // Quoting or non-ASCII transport can expand a browser-generated
        // Content-Disposition filename, so budget three times its UTF-8 size.
        encoder.encode(artifact.file.name).byteLength * 3;
      return (
        total +
        artifact.file.size +
        MULTIPART_FILE_PART_RESERVE_BYTES +
        headerTextBytes
      );
    }, manifestBytes + MULTIPART_ENVELOPE_RESERVE_BYTES);
    if (encodedRequestEstimate > MAX_MULTIPART_REQUEST_BYTES) {
      throw new ApiError("编码后的任务产出请求不能超过 100 MiB", {
        code: "REQUEST_TOO_LARGE",
        status: 413,
      });
    }
    form.append("manifest", manifest);
    files.forEach((artifact) => {
      const field = fileFields.get(artifact.clientRef);
      if (field !== undefined) {
        form.append(field, artifact.file, artifact.file.name);
      }
    });
    body = form;
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/submit-output`,
    { method: "POST", headers, body },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  return normalizeSubmitOutputResult(payload, taskId);
}

function normalizeReviewTaskSubmissionResult(
  value: unknown,
  taskId: string,
  decision: ReviewTaskSubmissionInput["decision"],
): ReviewTaskSubmissionResult {
  if (!isRecord(value) || !isRecord(value.data)) {
    return invalidResponse("任务验收响应格式无效");
  }
  const task = normalizeTask(value.data.task);
  const submission = normalizeTaskSubmission(value.data.submission);
  const event = normalizeTaskWorkflowEvent(value.data.event);
  const expectedStatus =
    decision === "accept" ? "accepted" : "changes_requested";
  const expectedTaskStatus = decision === "accept" ? "done" : "in_progress";
  const expectedEvent =
    decision === "accept" ? "task_review_accepted" : "task_changes_requested";
  if (
    task.id !== taskId ||
    task.status !== expectedTaskStatus ||
    task.currentSubmissionId !== submission.id ||
    submission.taskId !== taskId ||
    submission.status !== expectedStatus ||
    event.action !== expectedEvent ||
    event.submissionId !== submission.id ||
    event.artifactId !== null
  ) {
    return invalidResponse("任务验收响应不一致");
  }
  return { task, submission, event };
}

export async function reviewTaskSubmission(
  taskId: string,
  input: ReviewTaskSubmissionInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ReviewTaskSubmissionResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/review`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({
        decision: input.decision,
        ...(input.decision === "request_changes"
          ? { reason: input.reason }
          : {}),
      }),
    },
  );
  return normalizeReviewTaskSubmissionResult(payload, taskId, input.decision);
}

export async function deleteTaskArtifact(
  artifactId: string,
  taskId: string,
  input: DeleteTaskArtifactInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<DeleteTaskArtifactResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/artifacts/${encodeURIComponent(artifactId)}?confirm=true`,
    {
      method: "DELETE",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("删除任务产出响应格式无效");
  }
  const task = normalizeTask(payload.data.task);
  const artifact = normalizeTaskArtifactSummary(payload.data.artifact);
  const event = normalizeTaskWorkflowEvent(payload.data.event);
  if (
    task.id !== taskId ||
    artifact.id !== artifactId ||
    artifact.taskId !== taskId ||
    artifact.deletedAt === null ||
    event.action !== "task_artifact_deleted" ||
    event.submissionId !== artifact.submissionId ||
    event.artifactId !== artifact.id
  ) {
    return invalidResponse("删除任务产出响应不一致");
  }
  return { task, artifact, event };
}
