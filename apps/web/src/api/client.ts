import type {
  Actor,
  ActorListParams,
  ActorListResult,
  ActorSummary,
  AssignmentRole,
  BatchUpdateTasksInput,
  BatchUpdateTasksResult,
  Client,
  ClientInput,
  ClientListParams,
  ClientListResult,
  ClientStatus,
  CreateFocusSessionInput,
  CreatePersonActorInput,
  CreateTaskAssignmentInput,
  DeleteTaskArtifactInput,
  DeleteTaskArtifactResult,
  DeleteTagResult,
  EndTaskAssignmentInput,
  FocusRecoveryAction,
  FocusSession,
  FocusSessionSnapshot,
  HealthResponse,
  NewTaskInput,
  DeleteProjectResult,
  DeleteClientResult,
  Project,
  ProjectInput,
  ProjectListParams,
  ProjectListResult,
  ProjectStatus,
  ProjectTransitionAction,
  ReorderTasksInput,
  ReorderTasksResult,
  ReassignTaskAssignmentInput,
  ReassignTaskAssignmentResult,
  Tag,
  TagInput,
  TagListParams,
  TagListResult,
  Task,
  TaskArtifact,
  TaskArtifactDownload,
  TaskArtifactListParams,
  TaskArtifactListResult,
  TaskArtifactStorageKind,
  TaskAggregateListMeta,
  TaskAssignment,
  TaskAssignmentListParams,
  TaskAssignmentListResult,
  TaskAssignmentMutationResult,
  TaskKind,
  TaskEventListParams,
  TaskEventListResult,
  TaskLifecycleAction,
  TaskLifecycleCommandInput,
  TaskLifecycleCommandResult,
  TaskListParams,
  TaskListResult,
  TaskPriority,
  TaskReviewPolicy,
  TaskSubmission,
  TaskSubmissionListParams,
  TaskSubmissionListResult,
  SubmitTaskOutputInput,
  SubmitTaskOutputResult,
  ReviewTaskSubmissionInput,
  ReviewTaskSubmissionResult,
  TaskStatus,
  TaskWorkflowEvent,
  TodayStats,
  UpdateActorInput,
  UpdateClientInput,
  UpdateTagInput,
  UpdateTaskInput,
  UpdateProjectInput,
} from "../types/models";

const DEV_TOKEN =
  import.meta.env.VITE_OPC_SESSION_TOKEN ?? "opc-workspace-local-dev";

const DEFAULT_REQUEST_TIMEOUT_MS = 8_000;
const ARTIFACT_TRANSFER_TIMEOUT_MS = 120_000;
const MAX_JSON_REQUEST_BYTES = 1_024 * 1_024;
const MAX_ARTIFACT_FILE_BYTES = 50 * 1_024 * 1_024;
const MAX_MULTIPART_REQUEST_BYTES = 100 * 1_024 * 1_024;
// Browsers choose the multipart boundary and encode per-part headers after the
// FormData has been built. Reserve enough space for that envelope so a request
// that passes client validation does not immediately exceed the Sidecar limit.
const MULTIPART_ENVELOPE_RESERVE_BYTES = 64 * 1_024;
const MULTIPART_FILE_PART_RESERVE_BYTES = 2 * 1_024;

interface RuntimeConnection {
  baseUrl: string;
  token: string;
  source: "browser" | "tauri";
}

type JsonRecord = Record<string, unknown>;

export class ApiError extends Error {
  readonly code: string;
  readonly status?: number;
  readonly requestId?: string;

  constructor(
    message: string,
    options: { code?: string; status?: number; requestId?: string } = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.code = options.code ?? "REQUEST_ERROR";
    this.status = options.status;
    this.requestId = options.requestId;
  }
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(
  record: JsonRecord,
  ...keys: string[]
): string | undefined {
  for (const key of keys) {
    if (typeof record[key] === "string" && record[key])
      return record[key] as string;
  }
  return undefined;
}

function browserConnection(): RuntimeConnection {
  return { baseUrl: "", token: DEV_TOKEN, source: "browser" };
}

function isTauriRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const runtimeWindow = window as Window & {
    __TAURI__?: unknown;
    __TAURI_INTERNALS__?: unknown;
  };
  return Boolean(runtimeWindow.__TAURI__ || runtimeWindow.__TAURI_INTERNALS__);
}

let runtimeConnectionPromise: Promise<RuntimeConnection> | undefined;

async function resolveRuntimeConnection(): Promise<RuntimeConnection> {
  if (!isTauriRuntime()) return browserConnection();

  const { invoke } = await import("@tauri-apps/api/core");
  const readyStates = new Set(["ready", "running", "healthy", "ok"]);
  const maxAttempts = 50;

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const rawStatus = await invoke<unknown>("sidecar_status");
    if (!isRecord(rawStatus)) {
      throw new ApiError("桌面服务尚未返回可用连接信息", {
        code: "SIDECAR_UNAVAILABLE",
      });
    }

    const state = stringField(
      rawStatus,
      "phase",
      "state",
      "status",
    )?.toLowerCase();

    if (state === "error") {
      throw new ApiError(
        stringField(rawStatus, "message") ?? "本地 Sidecar 启动失败",
        { code: "SIDECAR_START_ERROR" },
      );
    }

    if (state && readyStates.has(state)) {
      const baseUrl = stringField(
        rawStatus,
        "base_url",
        "baseUrl",
        "url",
        "api_base_url",
        "apiBaseUrl",
      );
      const token = stringField(
        rawStatus,
        "token",
        "session_token",
        "sessionToken",
      );
      if (!baseUrl || !token) {
        throw new ApiError("本地 Sidecar 连接信息不完整", {
          code: "SIDECAR_INVALID_STATUS",
        });
      }
      return {
        baseUrl: baseUrl.replace(/\/$/, ""),
        token,
        source: "tauri",
      };
    }

    if (state && state !== "starting") {
      throw new ApiError(`本地 Sidecar 当前状态：${state}`, {
        code: "SIDECAR_UNAVAILABLE",
      });
    }

    await new Promise((resolve) => window.setTimeout(resolve, 100));
  }

  throw new ApiError("等待本地 Sidecar 就绪超时", {
    code: "SIDECAR_TIMEOUT",
  });
}

export function getRuntimeConnection(): Promise<RuntimeConnection> {
  runtimeConnectionPromise ??= resolveRuntimeConnection().catch((error) => {
    runtimeConnectionPromise = undefined;
    throw error;
  });
  return runtimeConnectionPromise;
}

export function resetRuntimeConnection(): void {
  runtimeConnectionPromise = undefined;
}

async function readResponseJSON(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch (error) {
    if (error instanceof SyntaxError) return undefined;
    throw error;
  }
}

async function apiFetch<T>(
  path: string,
  consume: (response: Response) => Promise<T>,
  init: RequestInit = {},
  accept = "application/json",
  timeoutMs = DEFAULT_REQUEST_TIMEOUT_MS,
): Promise<T> {
  const connection = await getRuntimeConnection();
  const headers = new Headers(init.headers);
  headers.set("Accept", accept);
  const multipart =
    typeof FormData !== "undefined" && init.body instanceof FormData;
  if (init.body && !multipart && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  if (connection.token)
    headers.set("Authorization", `Bearer ${connection.token}`);

  const controller = new AbortController();
  const upstreamSignal = init.signal;
  const abortFromUpstream = () => controller.abort();
  if (upstreamSignal?.aborted) controller.abort();
  else
    upstreamSignal?.addEventListener("abort", abortFromUpstream, {
      once: true,
    });
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(`${connection.baseUrl}${path}`, {
      ...init,
      headers,
      signal: controller.signal,
    });
    if (!response.ok) {
      const body = await readResponseJSON(response);
      const errorBody = isRecord(body) ? body : {};
      throw new ApiError(
        stringField(errorBody, "message") ?? `请求失败（${response.status}）`,
        {
          code: stringField(errorBody, "code") ?? "HTTP_ERROR",
          status: response.status,
          requestId: stringField(errorBody, "request_id", "requestId"),
        },
      );
    }

    return await consume(response);
  } catch (error) {
    if (error instanceof ApiError) throw error;
    if (
      controller.signal.aborted ||
      (error instanceof DOMException && error.name === "AbortError")
    ) {
      throw new ApiError("连接本地 Sidecar 超时", { code: "TIMEOUT" });
    }
    throw new ApiError("无法连接本地 Sidecar", { code: "NETWORK_ERROR" });
  } finally {
    window.clearTimeout(timeout);
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
  }
}

async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
  timeoutMs = DEFAULT_REQUEST_TIMEOUT_MS,
): Promise<T> {
  return apiFetch(
    path,
    async (response) => (await readResponseJSON(response)) as T,
    init,
    "application/json",
    timeoutMs,
  );
}

function invalidResponse(message: string): never {
  throw new ApiError(message, { code: "INVALID_RESPONSE" });
}

function asTaskStatus(value: unknown): TaskStatus {
  if (
    value === "todo" ||
    value === "in_progress" ||
    value === "blocked" ||
    value === "waiting_review" ||
    value === "done" ||
    value === "cancelled"
  ) {
    return value;
  }
  return invalidResponse("任务状态响应无效");
}

function asTaskReviewPolicy(value: unknown): TaskReviewPolicy {
  if (value === "none" || value === "manual") return value;
  return invalidResponse("任务验收策略响应无效");
}

function asBlockedFromStatus(value: unknown): Task["blockedFromStatus"] {
  if (value === null) return null;
  if (
    value === "todo" ||
    value === "in_progress" ||
    value === "waiting_review"
  ) {
    return value;
  }
  return invalidResponse("任务阻塞前状态响应无效");
}

function asTaskPriority(value: unknown): TaskPriority {
  if (value === "P0" || value === "P1" || value === "P2" || value === "P3") {
    return value;
  }
  return invalidResponse("任务优先级响应无效");
}

function asTaskKind(value: unknown): TaskKind {
  if (value === undefined || value === null || value === "") return "work";
  if (
    value === "work" ||
    value === "review" ||
    value === "followup" ||
    value === "reminder"
  ) {
    return value;
  }
  return invalidResponse("任务类型响应无效");
}

function nullableString(value: unknown): string | null {
  return typeof value === "string" && value ? value : null;
}

function fieldValue(record: JsonRecord, ...keys: string[]): unknown {
  for (const key of keys) {
    if (key in record) return record[key];
  }
  return undefined;
}

function numeric(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function nonNegativeInteger(
  value: unknown,
  field: string,
  fallback?: number,
): number {
  if (value === undefined && fallback !== undefined) return fallback;
  if (typeof value === "number" && Number.isInteger(value) && value >= 0) {
    return value;
  }
  return invalidResponse(`${field} 响应无效`);
}

function positiveInteger(
  value: unknown,
  field: string,
  fallback?: number,
): number {
  if (value === undefined && fallback !== undefined) return fallback;
  if (typeof value === "number" && Number.isInteger(value) && value >= 1) {
    return value;
  }
  return invalidResponse(`${field} 响应无效`);
}

function nullableNonNegativeInteger(
  value: unknown,
  field: string,
): number | null {
  if (value === undefined || value === null) return null;
  return nonNegativeInteger(value, field);
}

function expectedVersionHeader(value: unknown): Record<string, string> {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1) {
    throw new ApiError("资源版本缺失，请刷新后重试", {
      code: "EXPECTED_VERSION_REQUIRED",
    });
  }
  return { "If-Match": `"${value}"` };
}

function asProjectStatus(value: unknown): ProjectStatus {
  if (
    value === "in_progress" ||
    value === "paused" ||
    value === "completed" ||
    value === "archived"
  ) {
    return value;
  }
  return "planning";
}

function asClientStatus(value: unknown): ClientStatus {
  if (value === "active" || value === "lead" || value === "inactive") {
    return value;
  }
  return invalidResponse("客户状态响应无效");
}

function clientOptionalString(value: unknown, field: string): string | null {
  if (value === null) return null;
  if (typeof value === "string") return value || null;
  return invalidResponse(`${field}响应无效`);
}

function asActorType(value: unknown): Actor["type"] {
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

function asActorStatus(value: unknown): Actor["status"] {
  if (value === "active" || value === "inactive") return value;
  return invalidResponse("责任主体状态响应无效");
}

function asAssignmentRole(value: unknown): AssignmentRole {
  if (value === "assignee" || value === "reviewer") return value;
  return invalidResponse("任务分派角色响应无效");
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

function nullableAssignmentString(
  value: unknown,
  field: string,
): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field} 响应无效`);
}

export function normalizeTaskAssignment(value: unknown): TaskAssignment {
  if (!isRecord(value)) return invalidResponse("任务分派响应格式无效");
  const id = stringField(value, "id");
  const taskId = stringField(value, "task_id", "taskId");
  const actorId = stringField(value, "actor_id", "actorId");
  const assignedByActorId = stringField(
    value,
    "assigned_by_actor_id",
    "assignedByActorId",
  );
  const assignedAt = stringField(value, "assigned_at", "assignedAt");
  const rawUnassignedAt = fieldValue(value, "unassigned_at", "unassignedAt");
  const rawReason = value.reason;
  const isActive = fieldValue(value, "is_active", "isActive");
  const inferred = value.inferred;
  if (
    !id ||
    !taskId ||
    !actorId ||
    !assignedByActorId ||
    !assignedAt ||
    typeof isActive !== "boolean" ||
    typeof inferred !== "boolean"
  ) {
    return invalidResponse("任务分派响应格式无效");
  }
  const unassignedAt = nullableAssignmentString(
    rawUnassignedAt,
    "任务分派结束时间",
  );
  const reason = nullableAssignmentString(rawReason, "任务分派原因");
  if (reason !== null && reason.length > 1_000) {
    return invalidResponse("任务分派原因响应无效");
  }
  if (isActive !== (unassignedAt === null)) {
    return invalidResponse("任务分派活动状态响应不一致");
  }
  const actor = normalizeActorSummary(value.actor);
  const assignedByActor = normalizeActorSummary(
    fieldValue(value, "assigned_by_actor", "assignedByActor"),
  );
  if (actor.id !== actorId || assignedByActor.id !== assignedByActorId) {
    return invalidResponse("任务分派责任主体响应不一致");
  }
  return {
    id,
    taskId,
    role: asAssignmentRole(value.role),
    actorId,
    actor,
    assignedByActorId,
    assignedByActor,
    assignedAt,
    unassignedAt,
    reason,
    isActive,
    inferred,
  };
}

export function normalizeTaskAssignmentListResult(
  value: unknown,
): TaskAssignmentListResult {
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.meta)) {
    return invalidResponse("任务分派列表响应格式无效");
  }
  const active = value.data.active;
  const history = value.data.history;
  if (!isRecord(active) || !Array.isArray(history)) {
    return invalidResponse("任务分派列表响应格式无效");
  }
  const normalizeActive = (
    raw: unknown,
    role: AssignmentRole,
  ): TaskAssignment | null => {
    if (raw === null) return null;
    const assignment = normalizeTaskAssignment(raw);
    if (!assignment.isActive || assignment.role !== role) {
      return invalidResponse("当前任务分派响应不一致");
    }
    return assignment;
  };
  const normalizedHistory = history.map(normalizeTaskAssignment);
  if (normalizedHistory.some((assignment) => assignment.isActive)) {
    return invalidResponse("任务分派历史响应不一致");
  }
  const result: TaskAssignmentListResult = {
    active: {
      assignee: normalizeActive(active.assignee, "assignee"),
      reviewer: normalizeActive(active.reviewer, "reviewer"),
    },
    history: normalizedHistory,
    meta: {
      page: positiveInteger(value.meta.page, "任务分派历史页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "任务分派历史分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "任务分派历史总数"),
      taskVersion: positiveInteger(
        fieldValue(value.meta, "task_version", "taskVersion"),
        "任务分派对应任务版本",
      ),
    },
  };
  if (result.meta.total < result.history.length) {
    return invalidResponse("任务分派历史总数响应无效");
  }
  return result;
}

export function normalizeActor(value: unknown): Actor {
  if (!isRecord(value)) return invalidResponse("责任主体响应格式无效");
  const id = stringField(value, "id");
  const displayName = stringField(value, "display_name", "displayName");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const isBuiltin = fieldValue(value, "is_builtin", "isBuiltin");
  const metadata = value.metadata;
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
  return {
    id,
    type: asActorType(value.type),
    displayName,
    status: asActorStatus(value.status),
    isBuiltin,
    notes: value.notes,
    metadata,
    version: positiveInteger(value.version, "责任主体版本"),
    createdAt,
    updatedAt,
  };
}

function asArchivedProjectStatus(
  value: unknown,
): Project["archivedFromStatus"] {
  return value === "planning" ||
    value === "in_progress" ||
    value === "paused" ||
    value === "completed"
    ? value
    : null;
}

const projectActions = new Set<ProjectTransitionAction>([
  "start",
  "pause",
  "resume",
  "complete",
  "reopen",
  "archive",
  "restore",
]);

export function normalizeProject(value: unknown): Project {
  if (!isRecord(value)) {
    throw new ApiError("项目响应格式无效", { code: "INVALID_RESPONSE" });
  }
  const rawSummary = isRecord(value.task_summary)
    ? value.task_summary
    : isRecord(value.taskSummary)
      ? value.taskSummary
      : {};
  const rawActions = value.available_actions ?? value.availableActions;

  return {
    id: String(value.id ?? ""),
    name: String(value.name ?? "未命名项目"),
    description: String(value.description ?? ""),
    clientId: nullableString(value.client_id ?? value.clientId),
    clientName: nullableString(value.client_name ?? value.clientName),
    status: asProjectStatus(value.status),
    startDate: nullableString(value.start_date ?? value.startDate),
    dueDate: nullableString(value.due_date ?? value.dueDate),
    amountMinor:
      value.amount_minor === null || value.amountMinor === null
        ? null
        : numeric(value.amount_minor ?? value.amountMinor),
    color: nullableString(value.color),
    version: numeric(value.version, 1),
    archivedFromStatus: asArchivedProjectStatus(
      value.archived_from_status ?? value.archivedFromStatus,
    ),
    createdAt: String(value.created_at ?? value.createdAt ?? ""),
    updatedAt: String(value.updated_at ?? value.updatedAt ?? ""),
    taskSummary: {
      total: numeric(rawSummary.total),
      completed: numeric(rawSummary.completed),
      inProgress: numeric(rawSummary.in_progress ?? rawSummary.inProgress),
      remaining: numeric(rawSummary.remaining),
      progressPercent: numeric(
        rawSummary.progress_percent ?? rawSummary.progressPercent,
      ),
      actualMinutes: numeric(
        rawSummary.actual_minutes ?? rawSummary.actualMinutes,
      ),
    },
    invoiceCount: numeric(value.invoice_count ?? value.invoiceCount),
    availableActions: Array.isArray(rawActions)
      ? rawActions.filter(
          (action): action is ProjectTransitionAction =>
            typeof action === "string" &&
            projectActions.has(action as ProjectTransitionAction),
        )
      : [],
  };
}

export function normalizeClient(value: unknown): Client {
  if (!isRecord(value)) return invalidResponse("客户响应格式无效");
  const id = stringField(value, "id");
  const name = stringField(value, "name");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  if (!id || !name || !createdAt || !updatedAt) {
    return invalidResponse("客户响应格式无效");
  }

  return {
    id,
    name,
    contactName: clientOptionalString(
      fieldValue(value, "contact_name", "contactName"),
      "客户联系人",
    ),
    email: clientOptionalString(value.email, "客户邮箱"),
    phone: clientOptionalString(value.phone, "客户电话"),
    notes: clientOptionalString(value.notes, "客户备注"),
    status: asClientStatus(value.status),
    version: positiveInteger(value.version, "客户版本"),
    projectCount: nonNegativeInteger(
      fieldValue(value, "project_count", "projectCount"),
      "客户项目数",
    ),
    createdAt,
    updatedAt,
  };
}

export function normalizeTag(value: unknown): Tag {
  if (!isRecord(value)) return invalidResponse("标签响应格式无效");
  const id = stringField(value, "id");
  const name = stringField(value, "name");
  const color = stringField(value, "color");
  const createdAt = stringField(value, "created_at", "createdAt");
  if (
    !id ||
    !name ||
    !color ||
    !createdAt ||
    !/^#[0-9A-Fa-f]{6}$/.test(color)
  ) {
    return invalidResponse("标签响应格式无效");
  }
  return {
    id,
    name,
    color: color.toUpperCase(),
    version: positiveInteger(value.version, "标签版本", 1),
    createdAt,
  };
}

export function normalizeTask(value: unknown): Task {
  if (!isRecord(value)) return invalidResponse("任务响应格式无效");

  const id = stringField(value, "id");
  const title = stringField(value, "title");
  if (!id || !title || typeof value.description !== "string") {
    return invalidResponse("任务响应格式无效");
  }

  const estimatedMinutes = fieldValue(
    value,
    "estimated_minutes",
    "estimatedMinutes",
  );
  const manualOrder = fieldValue(value, "manual_order", "manualOrder");
  const rawTags = value.tags;
  if (rawTags !== undefined && !Array.isArray(rawTags)) {
    return invalidResponse("任务标签响应格式无效");
  }

  return {
    id,
    title,
    description: value.description,
    kind: asTaskKind(value.kind),
    status: asTaskStatus(value.status),
    priority: asTaskPriority(value.priority),
    projectId: nullableString(fieldValue(value, "project_id", "projectId")),
    projectName:
      nullableString(fieldValue(value, "project_name", "projectName")) ??
      undefined,
    parentTaskId: nullableString(
      fieldValue(value, "parent_task_id", "parentTaskId"),
    ),
    parentTaskTitle:
      nullableString(
        fieldValue(value, "parent_task_title", "parentTaskTitle"),
      ) ?? undefined,
    completionCriteria: String(
      fieldValue(value, "completion_criteria", "completionCriteria") ?? "",
    ),
    reviewPolicy: asTaskReviewPolicy(
      fieldValue(value, "review_policy", "reviewPolicy"),
    ),
    blockedReason: nullableString(
      fieldValue(value, "blocked_reason", "blockedReason"),
    ),
    blockedAt: nullableString(fieldValue(value, "blocked_at", "blockedAt")),
    blockedFromStatus: asBlockedFromStatus(
      fieldValue(value, "blocked_from_status", "blockedFromStatus"),
    ),
    dueDate: nullableString(fieldValue(value, "due_date", "dueDate")),
    plannedDate: nullableString(
      fieldValue(value, "planned_date", "plannedDate"),
    ),
    estimatedMinutes: nullableNonNegativeInteger(
      estimatedMinutes,
      "任务预计时长",
    ),
    actualMinutes: nonNegativeInteger(
      fieldValue(value, "actual_minutes", "actualMinutes"),
      "任务实际时长",
      0,
    ),
    manualOrder: nullableNonNegativeInteger(manualOrder, "任务手动顺序"),
    version: positiveInteger(value.version, "任务版本", 1),
    subtaskTotal: nonNegativeInteger(
      fieldValue(value, "subtask_total", "subtaskTotal"),
      "子任务总数",
      0,
    ),
    subtaskCompleted: nonNegativeInteger(
      fieldValue(value, "subtask_completed", "subtaskCompleted"),
      "已完成子任务数",
      0,
    ),
    createdAt: String(fieldValue(value, "created_at", "createdAt") ?? ""),
    updatedAt: String(fieldValue(value, "updated_at", "updatedAt") ?? ""),
    completedAt: nullableString(
      fieldValue(value, "completed_at", "completedAt"),
    ),
    submittedAt: nullableString(
      fieldValue(value, "submitted_at", "submittedAt"),
    ),
    reviewedAt: nullableString(fieldValue(value, "reviewed_at", "reviewedAt")),
    currentSubmissionId: nullableString(
      fieldValue(value, "current_submission_id", "currentSubmissionId"),
    ),
    tags: Array.isArray(rawTags) ? rawTags.map(normalizeTag) : [],
  };
}

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

function nullableActorSummary(
  value: unknown,
  field: string,
): ActorSummary | null {
  if (value === null) return null;
  if (!isRecord(value)) return invalidResponse(`${field}响应无效`);
  return normalizeActorSummary(value);
}

function requiredNullableString(value: unknown, field: string): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field}响应无效`);
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
  return {
    id,
    taskId,
    sequence: positiveInteger(value.sequence, "任务提交序号"),
    status,
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

function nullableEventString(value: unknown, field: string): string | null {
  if (value === null) return null;
  if (typeof value === "string" && value.length > 0) return value;
  return invalidResponse(`${field} 响应无效`);
}

function nullableEventSnapshot(
  value: unknown,
  field: string,
): Record<string, unknown> | null {
  if (value === null) return null;
  if (isRecord(value)) return value;
  return invalidResponse(`${field} 响应无效`);
}

export function normalizeTaskWorkflowEvent(value: unknown): TaskWorkflowEvent {
  if (!isRecord(value)) return invalidResponse("任务事件响应格式无效");
  const id = stringField(value, "id");
  const action = stringField(value, "action");
  const createdAt = stringField(value, "created_at", "createdAt");
  if (!id || !action || action.length > 100 || !createdAt) {
    return invalidResponse("任务事件响应格式无效");
  }
  const rawActor = value.actor;
  if (rawActor !== null && !isRecord(rawActor)) {
    return invalidResponse("任务事件责任主体响应无效");
  }
  const rawCommandSeq = fieldValue(value, "command_seq", "commandSeq");
  let commandSeq: number | null = null;
  if (rawCommandSeq !== null) {
    if (
      typeof rawCommandSeq !== "number" ||
      !Number.isInteger(rawCommandSeq) ||
      rawCommandSeq < 1
    ) {
      return invalidResponse("任务事件命令序号响应无效");
    }
    commandSeq = rawCommandSeq;
  }
  const reason = nullableEventString(value.reason, "任务事件原因");
  if (reason !== null && reason.length > 1_000) {
    return invalidResponse("任务事件原因响应无效");
  }
  const requestId = nullableEventString(
    fieldValue(value, "request_id", "requestId"),
    "任务事件请求 ID",
  );
  if (requestId !== null && requestId.length > 128) {
    return invalidResponse("任务事件请求 ID 响应无效");
  }
  return {
    id,
    action,
    actor: rawActor === null ? null : normalizeActorSummary(rawActor),
    assignmentId: nullableEventString(
      fieldValue(value, "assignment_id", "assignmentId"),
      "任务事件分派 ID",
    ),
    submissionId: nullableEventString(
      fieldValue(value, "submission_id", "submissionId"),
      "任务事件提交 ID",
    ),
    artifactId: nullableEventString(
      fieldValue(value, "artifact_id", "artifactId"),
      "任务事件产出 ID",
    ),
    requestId,
    commandSeq,
    previous: nullableEventSnapshot(value.previous, "任务事件旧快照"),
    current: nullableEventSnapshot(value.current, "任务事件新快照"),
    reason,
    createdAt,
  };
}

export function normalizeTaskEventListResult(
  value: unknown,
): TaskEventListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("任务事件列表响应格式无效");
  }
  const items = value.data.map(normalizeTaskWorkflowEvent);
  const meta = value.meta;
  const result: TaskEventListResult = {
    items,
    meta: {
      page: positiveInteger(meta.page, "任务事件页码"),
      pageSize: positiveInteger(
        fieldValue(meta, "page_size", "pageSize"),
        "任务事件分页大小",
      ),
      total: nonNegativeInteger(meta.total, "任务事件总数"),
      taskVersion: positiveInteger(
        fieldValue(meta, "task_version", "taskVersion"),
        "任务事件任务版本",
      ),
    },
  };
  if (
    result.items.length > result.meta.pageSize ||
    result.meta.total < result.items.length
  ) {
    return invalidResponse("任务事件分页响应不一致");
  }
  return result;
}

function normalizeTaskAssignmentMutationResult(
  value: unknown,
): TaskAssignmentMutationResult {
  if (!isRecord(value) || !isRecord(value.data)) {
    return invalidResponse("任务分派命令响应格式无效");
  }
  const assignment = normalizeTaskAssignment(value.data.assignment);
  const task = normalizeTask(value.data.task);
  if (assignment.taskId !== task.id) {
    return invalidResponse("任务分派命令响应不一致");
  }
  return { assignment, task };
}

function normalizeReassignTaskAssignmentResult(
  value: unknown,
): ReassignTaskAssignmentResult {
  if (!isRecord(value) || !isRecord(value.data)) {
    return invalidResponse("任务改派命令响应格式无效");
  }
  const result = normalizeTaskAssignmentMutationResult(value);
  const previousAssignment = normalizeTaskAssignment(
    fieldValue(value.data, "previous_assignment", "previousAssignment"),
  );
  if (
    previousAssignment.taskId !== result.task.id ||
    previousAssignment.role !== result.assignment.role ||
    previousAssignment.isActive ||
    !result.assignment.isActive
  ) {
    return invalidResponse("任务改派命令响应不一致");
  }
  return { ...result, previousAssignment };
}

function asFocusSessionStatus(value: unknown): FocusSession["status"] {
  if (
    value === "planned" ||
    value === "active" ||
    value === "paused" ||
    value === "recovery_pending" ||
    value === "completed" ||
    value === "cancelled" ||
    value === "interrupted"
  ) {
    return value;
  }
  return invalidResponse("专注会话状态响应无效");
}

function asFocusSessionEndReason(value: unknown): FocusSession["endReason"] {
  if (value === undefined || value === null || value === "") return null;
  if (
    value === "user_stop" ||
    value === "completed" ||
    value === "cancelled" ||
    value === "crash_recovery"
  ) {
    return value;
  }
  return invalidResponse("专注会话结束原因响应无效");
}

export function normalizeFocusSession(value: unknown): FocusSession {
  if (!isRecord(value)) return invalidResponse("专注会话响应格式无效");
  const id = stringField(value, "id");
  const startedAt = stringField(value, "started_at", "startedAt");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  if (!id || !startedAt || !createdAt || !updatedAt) {
    return invalidResponse("专注会话响应格式无效");
  }

  return {
    id,
    taskId: nullableString(fieldValue(value, "task_id", "taskId")),
    taskTitle: nullableString(fieldValue(value, "task_title", "taskTitle")),
    status: asFocusSessionStatus(value.status),
    plannedSeconds: positiveInteger(
      fieldValue(value, "planned_seconds", "plannedSeconds"),
      "专注计划秒数",
    ),
    accumulatedSeconds: nonNegativeInteger(
      fieldValue(value, "accumulated_seconds", "accumulatedSeconds"),
      "专注累计秒数",
    ),
    startedAt,
    endedAt: nullableString(fieldValue(value, "ended_at", "endedAt")),
    lastResumedAt: nullableString(
      fieldValue(value, "last_resumed_at", "lastResumedAt"),
    ),
    lastHeartbeatAt: nullableString(
      fieldValue(value, "last_heartbeat_at", "lastHeartbeatAt"),
    ),
    endReason: asFocusSessionEndReason(
      fieldValue(value, "end_reason", "endReason"),
    ),
    version: positiveInteger(value.version, "专注会话版本"),
    createdAt,
    updatedAt,
  };
}

export function normalizeFocusSessionSnapshot(
  payload: unknown,
): FocusSessionSnapshot {
  const root = isRecord(payload) ? payload : null;
  const data = root && "data" in root ? root.data : payload;
  const envelope = isRecord(data) ? data : null;
  const wrapped = Boolean(
    envelope && ("session" in envelope || !("id" in envelope)),
  );
  const rawSession = wrapped ? envelope?.session : data;
  const rawServerNow =
    fieldValue(envelope ?? {}, "server_now", "serverNow") ??
    fieldValue(root ?? {}, "server_now", "serverNow");
  if (typeof rawServerNow !== "string" || rawServerNow.length === 0) {
    return invalidResponse("专注会话缺少服务端时间基准");
  }
  if (
    rawSession !== null &&
    rawSession !== undefined &&
    !isRecord(rawSession)
  ) {
    return invalidResponse("专注会话响应格式无效");
  }

  return {
    session:
      rawSession === null || rawSession === undefined
        ? null
        : normalizeFocusSession(rawSession),
    serverNow: rawServerNow,
    receivedAtMs: Date.now(),
  };
}

export async function getHealth(): Promise<HealthResponse> {
  return apiRequest<HealthResponse>("/health");
}

export async function getActors(
  input: ActorListParams = {},
): Promise<ActorListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.type) params.set("type", input.type);
  if (input.status) params.set("status", input.status);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/actors?${params}`);
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("责任主体列表响应格式无效");
  }
  const items = payload.data.map(normalizeActor);
  const meta = isRecord(payload.meta) ? payload.meta : {};
  return {
    items,
    meta: {
      page: positiveInteger(meta.page, "责任主体页码", input.page ?? 1),
      pageSize: positiveInteger(
        meta.page_size ?? meta.pageSize,
        "责任主体分页大小",
        input.pageSize ?? 50,
      ),
      total: nonNegativeInteger(meta.total, "责任主体总数", items.length),
    },
  };
}

export async function getAllActors(
  input: Omit<ActorListParams, "page" | "pageSize"> = {},
): Promise<Actor[]> {
  const actors = new Map<string, Actor>();
  const pageSize = 100;
  let page = 1;

  while (true) {
    const result = await getActors({ ...input, page, pageSize });
    for (const actor of result.items) actors.set(actor.id, actor);
    if (
      result.items.length === 0 ||
      result.meta.page * result.meta.pageSize >= result.meta.total
    ) {
      break;
    }
    page += 1;
  }

  return [...actors.values()];
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

export async function getTaskPage(
  options: TaskListParams = {},
): Promise<TaskListResult> {
  const params = new URLSearchParams({
    page: String(options.page ?? 1),
    page_size: String(options.pageSize ?? 50),
  });
  if (options.q?.trim()) params.set("q", options.q.trim());
  if (options.kind) params.set("kind", options.kind);
  if (options.status) params.set("status", options.status);
  if (options.priority) params.set("priority", options.priority);
  if (options.projectId) params.set("project_id", options.projectId);
  for (const tagId of new Set(options.tagIds?.map((id) => id.trim()))) {
    if (tagId) params.append("tag_id", tagId);
  }
  if (options.plannedDate) params.set("planned_date", options.plannedDate);
  if (options.plannedFrom) params.set("planned_from", options.plannedFrom);
  if (options.plannedTo) params.set("planned_to", options.plannedTo);
  if (options.parentTaskId) params.set("parent_task_id", options.parentTaskId);
  if (options.rootOnly !== undefined)
    params.set("root_only", String(options.rootOnly));
  if (options.sort?.trim()) params.set("sort", options.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/tasks?${params}`);
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("任务列表响应格式无效");
  }
  const items = payload.data.map(normalizeTask);
  const meta = isRecord(payload.meta) ? payload.meta : {};
  return {
    items,
    meta: {
      page: numeric(meta.page, options.page ?? 1),
      pageSize: numeric(
        meta.page_size ?? meta.pageSize,
        options.pageSize ?? 50,
      ),
      total: numeric(meta.total, items.length),
    },
  };
}

export async function getTasks(options: TaskListParams = {}): Promise<Task[]> {
  return (
    await getTaskPage({
      ...options,
      pageSize: options.pageSize ?? 100,
    })
  ).items;
}

export async function getAllTasks(
  options: Omit<TaskListParams, "page" | "pageSize"> = {},
): Promise<Task[]> {
  const allTasks: Task[] = [];
  const pageSize = 100;
  let page = 1;
  let total = Number.POSITIVE_INFINITY;

  while (allTasks.length < total) {
    const result = await getTaskPage({ ...options, page, pageSize });
    allTasks.push(...result.items);
    total = result.meta.total;
    if (result.items.length === 0) break;
    page += 1;
  }
  return allTasks;
}

export async function getTask(id: string): Promise<Task> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTask(body);
}

export async function getTaskEvents(
  taskId: string,
  input: TaskEventListParams = {},
): Promise<TaskEventListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/events?${params}`,
  );
  return normalizeTaskEventListResult(payload);
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

export async function getTaskArtifact(id: string): Promise<TaskArtifact> {
  const payload = await apiRequest<unknown>(
    `/api/v1/artifacts/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const artifact = normalizeTaskArtifact(body);
  if (artifact.id !== id) return invalidResponse("任务产出详情与请求不一致");
  return artifact;
}

function downloadFileName(
  disposition: string | null,
  fallback: string,
): string {
  let candidate = "";
  const encoded = disposition?.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  if (encoded) {
    try {
      candidate = decodeURIComponent(encoded.trim());
    } catch {
      candidate = "";
    }
  }
  if (!candidate) {
    candidate = disposition?.match(/filename="?([^";]+)"?/i)?.[1]?.trim() ?? "";
  }
  const safe = (candidate || fallback)
    .replace(/[\\/\u0000-\u001f\u007f]/g, "_")
    .trim();
  return safe || "artifact";
}

export async function downloadTaskArtifact(
  id: string,
  fallbackName: string,
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
    {},
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

export async function getTaskAssignments(
  taskId: string,
  input: TaskAssignmentListParams = {},
): Promise<TaskAssignmentListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.role) params.set("role", input.role);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/assignments?${params}`,
  );
  const result = normalizeTaskAssignmentListResult(payload);
  const assignments = [
    result.active.assignee,
    result.active.reviewer,
    ...result.history,
  ].filter((assignment): assignment is TaskAssignment => assignment !== null);
  if (
    assignments.some((assignment) => assignment.taskId !== taskId) ||
    (input.role &&
      result.history.some((assignment) => assignment.role !== input.role))
  ) {
    return invalidResponse("任务分派列表响应与请求不一致");
  }
  return result;
}

function versionedCommandHeaders(
  expectedVersion: number,
  idempotencyKey: string,
): Record<string, string> {
  return {
    ...expectedVersionHeader(expectedVersion),
    "Idempotency-Key": idempotencyKey,
  };
}

export async function createTaskAssignment(
  taskId: string,
  input: CreateTaskAssignmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<TaskAssignmentMutationResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/assignments`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ role: input.role, actor_id: input.actorId }),
    },
  );
  const result = normalizeTaskAssignmentMutationResult(payload);
  if (
    result.task.id !== taskId ||
    !result.assignment.isActive ||
    result.assignment.role !== input.role ||
    result.assignment.actorId !== input.actorId
  ) {
    return invalidResponse("首次任务分派响应与请求不一致");
  }
  return result;
}

export async function reassignTaskAssignment(
  taskId: string,
  input: ReassignTaskAssignmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<ReassignTaskAssignmentResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(taskId)}/reassign`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({
        role: input.role,
        actor_id: input.actorId,
        reason: input.reason,
      }),
    },
  );
  const result = normalizeReassignTaskAssignmentResult(payload);
  if (
    result.task.id !== taskId ||
    result.assignment.role !== input.role ||
    result.assignment.actorId !== input.actorId
  ) {
    return invalidResponse("任务改派响应与请求不一致");
  }
  return result;
}

export async function endTaskAssignment(
  assignmentId: string,
  input: EndTaskAssignmentInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<TaskAssignmentMutationResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/assignments/${encodeURIComponent(assignmentId)}/end`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const result = normalizeTaskAssignmentMutationResult(payload);
  if (result.assignment.id !== assignmentId || result.assignment.isActive) {
    return invalidResponse("结束任务分派响应与请求不一致");
  }
  return result;
}

export async function createTask(
  input: NewTaskInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Task> {
  const payload = await apiRequest<unknown>("/api/v1/tasks", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
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
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTask(body);
}

const taskLifecycleEventActions: Record<TaskLifecycleAction, string> = {
  start: "task_started",
  block: "task_blocked",
  unblock: "task_unblocked",
  complete: "task_completed",
  cancel: "task_cancelled",
  reopen: "task_reopened",
};

export async function executeTaskLifecycleCommand(
  id: string,
  input: TaskLifecycleCommandInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<TaskLifecycleCommandResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(id)}/${input.action}`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify(
        input.action === "block" || input.action === "cancel"
          ? { reason: input.reason }
          : {},
      ),
    },
  );
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("任务生命周期命令响应格式无效");
  }
  const task = normalizeTask(payload.data.task);
  const event = normalizeTaskWorkflowEvent(payload.data.event);
  if (
    task.id !== id ||
    event.action !== taskLifecycleEventActions[input.action] ||
    (typeof event.current?.version === "number" &&
      event.current.version !== task.version)
  ) {
    return invalidResponse("任务生命周期命令响应不一致");
  }
  return { task, event };
}

export async function updateTask(
  id: string,
  input: UpdateTaskInput,
): Promise<Task> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        title: input.title,
        description: input.description,
        ...(input.kind === undefined ? {} : { kind: input.kind }),
        priority: input.priority,
        ...(input.projectId === undefined
          ? {}
          : { project_id: input.projectId }),
        ...(input.parentTaskId === undefined
          ? {}
          : { parent_task_id: input.parentTaskId }),
        ...(input.completionCriteria === undefined
          ? {}
          : { completion_criteria: input.completionCriteria }),
        ...(input.reviewPolicy === undefined
          ? {}
          : { review_policy: input.reviewPolicy }),
        ...(input.tagIds === undefined ? {} : { tag_ids: input.tagIds }),
        due_date: input.dueDate,
        planned_date: input.plannedDate,
        estimated_minutes: input.estimatedMinutes,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTask(body);
}

export async function deleteTask(
  id: string,
  expectedVersion: number,
): Promise<void> {
  try {
    await apiRequest<void>(
      `/api/v1/tasks/${encodeURIComponent(id)}`,
      {
        method: "DELETE",
        headers: expectedVersionHeader(expectedVersion),
      },
      ARTIFACT_TRANSFER_TIMEOUT_MS,
    );
  } catch (error) {
    // A Task aggregate delete may move controlled files before returning. If a
    // successful response was lost, retrying a missing Task has reached the
    // same desired state and is safe to treat as success in the desktop UI.
    if (error instanceof ApiError && error.code === "TASK_NOT_FOUND") return;
    throw error;
  }
}

export async function batchUpdateTasks(
  input: BatchUpdateTasksInput,
): Promise<BatchUpdateTasksResult> {
  const body: Record<string, unknown> = {
    action: input.action,
    items: input.items.map((item) => ({
      id: item.id,
      expected_version: item.expectedVersion,
    })),
  };
  if (input.action === "set_project") body.project_id = input.projectId;
  if (input.action === "set_planned_date")
    body.planned_date = input.plannedDate;
  if (input.action === "add_tags" || input.action === "remove_tags") {
    body.tag_ids = input.tagIds;
  }
  const payload = await apiRequest<unknown>("/api/v1/tasks/batch", {
    method: "PATCH",
    body: JSON.stringify(body),
  });
  const data =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(data) || !Array.isArray(data.tasks)) {
    return invalidResponse("任务批量操作响应格式无效");
  }
  const action = data.action;
  if (
    action !== "set_project" &&
    action !== "set_planned_date" &&
    action !== "add_tags" &&
    action !== "remove_tags"
  ) {
    return invalidResponse("任务批量操作类型无效");
  }
  return {
    action,
    changed: nonNegativeInteger(data.changed, "任务批量变更数", 0),
    tasks: data.tasks.map(normalizeTask),
  };
}

export async function reorderTasks(
  input: ReorderTasksInput,
): Promise<ReorderTasksResult> {
  const payload = await apiRequest<unknown>("/api/v1/tasks/reorder", {
    method: "PUT",
    body: JSON.stringify({
      planned_date: input.plannedDate,
      mode: input.mode,
      items: input.items.map((item) => ({
        id: item.id,
        expected_version: item.expectedVersion,
      })),
    }),
  });
  const data =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(data) || !Array.isArray(data.tasks)) {
    return invalidResponse("任务排序响应格式无效");
  }
  if (data.mode !== "manual" && data.mode !== "default") {
    return invalidResponse("任务排序模式无效");
  }
  return {
    plannedDate: nullableString(
      fieldValue(data, "planned_date", "plannedDate"),
    ),
    mode: data.mode,
    changed: nonNegativeInteger(data.changed, "任务排序变更数", 0),
    tasks: data.tasks.map(normalizeTask),
  };
}

export async function getTags(
  input: TagListParams = {},
): Promise<TagListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 100),
  });
  if (input.query?.trim()) params.set("q", input.query.trim());
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/tags?${params}`);
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("标签列表响应格式无效");
  }
  const meta = isRecord(payload.meta) ? payload.meta : {};
  return {
    items: payload.data.map(normalizeTag),
    meta: {
      page: numeric(meta.page, input.page ?? 1),
      pageSize: numeric(meta.page_size ?? meta.pageSize, input.pageSize ?? 100),
      total: numeric(meta.total),
    },
  };
}

export async function getAllTags(
  input: Omit<TagListParams, "page" | "pageSize"> = {},
): Promise<Tag[]> {
  const tags: Tag[] = [];
  const pageSize = 100;
  let page = 1;
  let total = Number.POSITIVE_INFINITY;

  while (tags.length < total) {
    const result = await getTags({ ...input, page, pageSize });
    tags.push(...result.items);
    total = result.meta.total;
    if (result.items.length === 0) break;
    page += 1;
  }
  return tags;
}

export async function createTag(
  input: TagInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Tag> {
  const payload = await apiRequest<unknown>("/api/v1/tags", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ name: input.name, color: input.color }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTag(body);
}

export async function updateTag(
  id: string,
  input: UpdateTagInput,
): Promise<Tag> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tags/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.color === undefined ? {} : { color: input.color }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTag(body);
}

export async function deleteTag(
  id: string,
  expectedVersion: number,
): Promise<DeleteTagResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tags/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(expectedVersion),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("标签删除响应格式无效");
  return {
    deletedId: String(body.deleted_id ?? body.deletedId ?? id),
    detachedTasks: nonNegativeInteger(
      body.detached_tasks ?? body.detachedTasks,
      "标签关联任务数",
      0,
    ),
  };
}

export async function getProjects(
  input: ProjectListParams = {},
): Promise<ProjectListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.query?.trim()) params.set("q", input.query.trim());
  if (input.status) params.set("status", input.status);
  if (input.clientId) params.set("client_id", input.clientId);
  if (input.includeArchived) params.set("include_archived", "true");
  if (input.sort) params.set("sort", input.sort);
  const payload = await apiRequest<unknown>(`/api/v1/projects?${params}`);
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    throw new ApiError("项目列表响应格式无效", {
      code: "INVALID_RESPONSE",
    });
  }
  const meta = isRecord(payload.meta) ? payload.meta : {};
  return {
    items: payload.data.map(normalizeProject),
    meta: {
      page: numeric(meta.page, input.page ?? 1),
      pageSize: numeric(meta.page_size ?? meta.pageSize, input.pageSize ?? 50),
      total: numeric(meta.total),
    },
  };
}

export async function getAllProjects(
  input: Omit<ProjectListParams, "page" | "pageSize"> = {},
): Promise<Project[]> {
  const projects: Project[] = [];
  const pageSize = 100;
  let page = 1;
  let total = Number.POSITIVE_INFINITY;

  while (projects.length < total) {
    const result = await getProjects({ ...input, page, pageSize });
    projects.push(...result.items);
    total = result.meta.total;
    if (result.items.length === 0) break;
    page += 1;
  }
  return projects;
}

export async function getProject(id: string): Promise<Project> {
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProject(body);
}

export async function createProject(
  input: ProjectInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Project> {
  const payload = await apiRequest<unknown>("/api/v1/projects", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      name: input.name,
      description: input.description,
      client_id: input.clientId,
      start_date: input.startDate,
      due_date: input.dueDate,
      amount_minor: input.amountMinor,
      color: input.color,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProject(body);
}

export async function updateProject(
  id: string,
  input: UpdateProjectInput,
): Promise<Project> {
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: { "If-Match": `"${input.expectedVersion}"` },
      body: JSON.stringify({
        name: input.name,
        description: input.description,
        client_id: input.clientId,
        start_date: input.startDate,
        due_date: input.dueDate,
        amount_minor: input.amountMinor,
        color: input.color,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProject(body);
}

export async function transitionProject(
  id: string,
  action: ProjectTransitionAction,
  expectedVersion: number,
  confirmIncompleteTasks = false,
): Promise<Project> {
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(id)}/transitions`,
    {
      method: "POST",
      headers: { "If-Match": `"${expectedVersion}"` },
      body: JSON.stringify({
        action,
        confirm_incomplete_tasks: confirmIncompleteTasks,
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProject(body);
}

export async function deleteProject(
  id: string,
  expectedVersion: number,
): Promise<DeleteProjectResult> {
  let payload: unknown;
  try {
    payload = await apiRequest<unknown>(
      `/api/v1/projects/${encodeURIComponent(id)}?confirm=true`,
      {
        method: "DELETE",
        headers: { "If-Match": `"${expectedVersion}"` },
      },
    );
  } catch (error) {
    // DELETE is idempotent from the desktop user's perspective. If the first
    // response was lost (or another window already deleted the project), the
    // desired final state has still been reached.
    if (error instanceof ApiError && error.code === "PROJECT_NOT_FOUND") {
      return { deletedId: id, detachedTasks: 0, detachedInvoices: 0 };
    }
    throw error;
  }
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) {
    throw new ApiError("项目删除响应格式无效", {
      code: "INVALID_RESPONSE",
    });
  }
  return {
    deletedId: String(body.deleted_id ?? body.deletedId ?? id),
    detachedTasks: numeric(body.detached_tasks ?? body.detachedTasks),
    detachedInvoices: numeric(body.detached_invoices ?? body.detachedInvoices),
  };
}

export async function getClients(
  input: ClientListParams = {},
): Promise<ClientListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.status) params.set("status", input.status);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/clients?${params}`);
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户列表响应格式无效");
  }
  return {
    items: payload.data.map(normalizeClient),
    meta: {
      page: positiveInteger(payload.meta.page, "客户列表页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户列表每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户列表总数"),
    },
  };
}

export async function getAllClients(
  input: Omit<ClientListParams, "page" | "pageSize"> = {},
): Promise<Client[]> {
  const clients: Client[] = [];
  const pageSize = 100;
  let page = 1;
  let total = Number.POSITIVE_INFINITY;

  while (clients.length < total) {
    const result = await getClients({ ...input, page, pageSize });
    clients.push(...result.items);
    total = result.meta.total;
    if (result.items.length === 0) break;
    page += 1;
  }
  return clients;
}

export async function getClient(id: string): Promise<Client> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function createClient(
  input: ClientInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Client> {
  const payload = await apiRequest<unknown>("/api/v1/clients", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      name: input.name,
      contact_name: input.contactName,
      email: input.email,
      phone: input.phone,
      notes: input.notes,
      status: input.status,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function updateClient(
  id: string,
  input: UpdateClientInput,
): Promise<Client> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.contactName === undefined
          ? {}
          : { contact_name: input.contactName }),
        ...(input.email === undefined ? {} : { email: input.email }),
        ...(input.phone === undefined ? {} : { phone: input.phone }),
        ...(input.notes === undefined ? {} : { notes: input.notes }),
        ...(input.status === undefined ? {} : { status: input.status }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClient(body);
}

export async function deleteClient(
  id: string,
  expectedVersion: number,
): Promise<DeleteClientResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(expectedVersion),
    },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("客户删除响应格式无效");
  return {
    deletedId: stringField(body, "deleted_id", "deletedId") ?? id,
    detachedProjects: nonNegativeInteger(
      fieldValue(body, "detached_projects", "detachedProjects"),
      "客户解绑项目数",
    ),
  };
}

export async function getActiveFocusSession(): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>("/api/v1/focus-sessions/active");
  return normalizeFocusSessionSnapshot(payload);
}

export async function createFocusSession(
  input: CreateFocusSessionInput,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>("/api/v1/focus-sessions", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      task_id: input.taskId,
      planned_seconds: input.plannedSeconds,
    }),
  });
  return normalizeFocusSessionSnapshot(payload);
}

async function executeFocusSessionCommand(
  id: string,
  command: "pause" | "resume" | "stop" | "cancel",
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<FocusSessionSnapshot> {
  const headers = {
    ...expectedVersionHeader(expectedVersion),
    ...(idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {}),
  };
  const payload = await apiRequest<unknown>(
    `/api/v1/focus-sessions/${encodeURIComponent(id)}/${command}`,
    {
      method: "POST",
      headers,
      body: "{}",
    },
  );
  return normalizeFocusSessionSnapshot(payload);
}

export function pauseFocusSession(
  id: string,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(id, "pause", expectedVersion);
}

export function resumeFocusSession(
  id: string,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(id, "resume", expectedVersion);
}

export function stopFocusSession(
  id: string,
  expectedVersion: number,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(
    id,
    "stop",
    expectedVersion,
    idempotencyKey,
  );
}

export function cancelFocusSession(
  id: string,
  expectedVersion: number,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(
    id,
    "cancel",
    expectedVersion,
    idempotencyKey,
  );
}

export async function recoverFocusSession(
  id: string,
  action: FocusRecoveryAction,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>(
    `/api/v1/focus-sessions/${encodeURIComponent(id)}/recover`,
    {
      method: "POST",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ action }),
    },
  );
  return normalizeFocusSessionSnapshot(payload);
}

export async function getTodayStats(
  date: string,
  timezone?: string | number,
): Promise<TodayStats> {
  const params = new URLSearchParams({ date });
  if (typeof timezone === "string" && timezone.trim()) {
    params.set("timezone", timezone.trim());
  } else if (typeof timezone === "number") {
    params.set("timezone_offset_minutes", String(timezone));
  }
  const payload = await apiRequest<unknown>(`/api/v1/stats/today?${params}`);
  const data =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(data) || !isRecord(data.tasks) || !isRecord(data.focus)) {
    throw new ApiError("今日统计响应格式无效", { code: "INVALID_RESPONSE" });
  }

  return {
    date: String(data.date ?? date),
    tasks: {
      total: numeric(data.tasks.total),
      completed: numeric(data.tasks.completed),
      remaining: numeric(data.tasks.remaining),
      overdue: numeric(data.tasks.overdue),
      dueSoon: numeric(data.tasks.due_soon ?? data.tasks.dueSoon),
      estimatedMinutes: numeric(
        data.tasks.estimated_minutes ?? data.tasks.estimatedMinutes,
      ),
      actualMinutes: numeric(
        data.tasks.actual_minutes ?? data.tasks.actualMinutes,
      ),
    },
    focus: {
      sessions: numeric(data.focus.sessions),
      seconds: numeric(data.focus.seconds),
      minutes: numeric(data.focus.minutes),
    },
  };
}
