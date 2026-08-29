import type {
  Actor,
  AppSettingItem,
  AppSettingKey,
  AppSettingsResult,
  AppSettingUpdate,
  AppearanceSettingValue,
  ActorListParams,
  ActorListResult,
  ActorSummary,
  AssignmentRole,
  AutomationActionType,
  AutomationConfig,
  AutomationPreview,
  AutomationRule,
  AutomationRuleStatus,
  AutomationRun,
  AutomationRunListParams,
  AutomationRunListResult,
  AutomationRunStatus,
  AutomationTriggerType,
  AgentAdapter,
  BackupSummary,
  BackupRestoreDrillResult,
  BackupVerificationStatus,
  BusinessDataExportDownload,
  BusinessPackageDownload,
  BusinessPackageImportPreview,
  BusinessPackageImportResult,
  RestoreDiagnostics,
  BusinessImportPreview,
  BusinessImportResult,
  DiagnosticPackageDownload,
  ScheduledBackupRestoreResult,
  SearchListParams,
  SearchListResult,
  SearchResourceType,
  SearchResult,
  BatchUpdateTasksInput,
  BatchUpdateTasksResult,
  Client,
  ClientActivity,
  ClientAttachment,
  ClientAttachmentDownload,
  ClientAttachmentListParams,
  ClientAttachmentListResult,
  ClientActorLink,
  ClientActorLinkListParams,
  ClientActorLinkListResult,
  ClientActivityListParams,
  ClientActivityListResult,
  ClientFollowup,
  CancelClientFollowupInput,
  CompleteClientFollowupInput,
  CreateClientFollowupInput,
  ClientFollowupListParams,
  ClientFollowupListResult,
  ClientInput,
  ClientListParams,
  ClientListResult,
  ClientStatus,
  RescheduleClientFollowupInput,
  RescheduleClientFollowupResult,
  SkipClientFollowupInput,
  UpdateClientFollowupInput,
  CreateClientActivityInput,
  CreateClientAttachmentInput,
  CreateClientActorLinkInput,
  CreateFocusSessionInput,
  CreateBackupInput,
  CreateReminderInput,
  CreatePersonActorInput,
  CreateProjectNoteInput,
  CreateProjectAttachmentInput,
  CreateTaskSavedViewInput,
  CreateTaskAssignmentInput,
  DeleteTaskArtifactInput,
  DeleteTaskArtifactResult,
  DeleteTagResult,
  DeleteTaskSavedViewResult,
  EndTaskAssignmentInput,
  FocusRecoveryAction,
  FocusReport,
  FocusReportParams,
  FocusSession,
  FocusSessionListParams,
  FocusSessionListResult,
  FocusSessionSnapshot,
  ForceResolveInboxItemInput,
  HealthResponse,
  FocusSettingValue,
  GeneralSettingValue,
  StorageSettingValue,
  StorageCapacityResult,
  StorageCapacityStatus,
  InboxEventListParams,
  InboxEventListResult,
  InboxItem,
  InboxItemAction,
  InboxItemCommandInput,
  InboxItemListParams,
  InboxItemListResult,
  InboxItemTaskListParams,
  InboxItemTaskListResult,
  InboxItemTaskMutationResult,
  InboxItemTaskRelation,
  InboxItemStatus,
  InboxTaskProgress,
  InboxTaskSummary,
  InboxSplitTaskResult,
  InboxStats,
  InboxResolutionMode,
  InboxWorkflowEvent,
  CreateInboxItemInput,
  LinkInboxItemTaskInput,
  MarkAllInboxReadResult,
  CancelReminderInput,
  NewTaskInput,
  DeleteProjectResult,
  DeleteProjectNoteInput,
  DeleteProjectAttachmentInput,
  DeleteClientResult,
  DeleteClientActivityInput,
  DeleteClientAttachmentInput,
  DeleteClientActorLinkInput,
  Project,
  RoadmapMilestone,
  RoadmapMilestoneListParams,
  RoadmapMilestoneListResult,
  RoadmapMilestoneStatus,
  CreateRoadmapMilestoneInput,
  UpdateRoadmapMilestoneInput,
  ContentItem,
  ContentItemListParams,
  ContentItemListResult,
  ContentItemStatus,
  CreateContentItemInput,
  UpdateContentItemInput,
  ScheduleContentItemInput,
  PublishContentItemInput,
  ProjectAttachment,
  ProjectAttachmentDownload,
  ProjectAttachmentListParams,
  ProjectAttachmentListResult,
  ProjectArtifactItem,
  ProjectArtifactListParams,
  ProjectArtifactListResult,
  ProjectNote,
  ProjectNoteListParams,
  ProjectNoteListResult,
  ProjectInput,
  ProjectListParams,
  ProjectListResult,
  ProjectEventListParams,
  ProjectEventListResult,
  ProjectWorkflowEvent,
  ProjectStatus,
  ProjectTransitionAction,
  Reminder,
  ReminderAction,
  ReminderRecurrenceType,
  ReminderListParams,
  ReminderListResult,
  ReminderStatus,
  ReorderTasksInput,
  ReorderTasksResult,
  ReassignTaskAssignmentInput,
  ReassignTaskAssignmentResult,
  SplitInboxItemInput,
  SplitInboxItemResult,
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
  TaskSavedView,
  TaskSavedViewDefinition,
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
  UpdateClientActivityInput,
  UpdateInboxItemInput,
  UpdateInboxItemTaskRequirementInput,
  UpdateTagInput,
  UpdateTaskSavedViewInput,
  UpdateTaskInput,
  UpdateProjectInput,
  UpdateProjectNoteInput,
  UpdateReminderInput,
  UnlinkInboxItemTaskInput,
  WorkspaceSettingValue,
} from "../types/models";

const DEV_TOKEN =
  import.meta.env.VITE_OPC_SESSION_TOKEN ?? "opc-workspace-local-dev";

const DEFAULT_REQUEST_TIMEOUT_MS = 8_000;
const ARTIFACT_TRANSFER_TIMEOUT_MS = 120_000;
const BACKUP_OPERATION_TIMEOUT_MS = 180_000;
const MAX_JSON_REQUEST_BYTES = 1_024 * 1_024;
const MAX_ARTIFACT_FILE_BYTES = 50 * 1_024 * 1_024;
const MAX_WORKSPACE_AVATAR_BYTES = 2 * 1_024 * 1_024;
const MAX_MULTIPART_REQUEST_BYTES = 100 * 1_024 * 1_024;
// Browsers choose the multipart boundary and encode per-part headers after the
// FormData has been built. Reserve enough space for that envelope so a request
// that passes client validation does not immediately exceed the Sidecar limit.
const MULTIPART_ENVELOPE_RESERVE_BYTES = 64 * 1_024;
const MULTIPART_FILE_PART_RESERVE_BYTES = 2 * 1_024;

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

function hasExactKeys(record: JsonRecord, keys: readonly string[]): boolean {
  const actual = Object.keys(record).sort();
  const expected = [...keys].sort();
  return (
    actual.length === expected.length &&
    actual.every((key, index) => key === expected[index])
  );
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

function numberField(
  record: JsonRecord,
  ...keys: string[]
): number | undefined {
  for (const key of keys) {
    if (typeof record[key] === "number" && Number.isFinite(record[key])) {
      return record[key];
    }
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
  const waitableStates = new Set(["starting", "restarting"]);
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

    if (state && !waitableStates.has(state)) {
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
  const requestId = crypto.randomUUID();
  headers.set("Accept", accept);
  headers.set("X-Request-ID", requestId);
  const multipart =
    typeof FormData !== "undefined" && init.body instanceof FormData;
  if (init.body && !multipart && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  if (connection.token)
    headers.set("Authorization", `Bearer ${connection.token}`);
  let correlatedRequestId: string = requestId;

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
    correlatedRequestId =
      response.headers.get("X-Request-ID")?.trim() || requestId;
    if (!response.ok) {
      const body = await readResponseJSON(response);
      const errorBody = isRecord(body) ? body : {};
      throw new ApiError(
        stringField(errorBody, "message") ?? `请求失败（${response.status}）`,
        {
          code: stringField(errorBody, "code") ?? "HTTP_ERROR",
          status: response.status,
          requestId:
            correlatedRequestId ||
            stringField(errorBody, "request_id", "requestId") ||
            requestId,
        },
      );
    }

    return await consume(response);
  } catch (error) {
    if (error instanceof ApiError) {
      if (error.requestId) throw error;
      throw new ApiError(error.message, {
        code: error.code,
        status: error.status,
        requestId: correlatedRequestId,
      });
    }
    if (
      controller.signal.aborted ||
      (error instanceof DOMException && error.name === "AbortError")
    ) {
      throw new ApiError("连接本地 Sidecar 超时", {
        code: "TIMEOUT",
        requestId,
      });
    }
    throw new ApiError("无法连接本地 Sidecar", {
      code: "NETWORK_ERROR",
      requestId,
    });
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

const roadmapMilestoneStatuses = new Set<RoadmapMilestoneStatus>([
  "planned",
  "active",
  "achieved",
  "archived",
]);

function asRoadmapMilestoneStatus(value: unknown): RoadmapMilestoneStatus {
  if (
    typeof value === "string" &&
    roadmapMilestoneStatuses.has(value as RoadmapMilestoneStatus)
  ) {
    return value as RoadmapMilestoneStatus;
  }
  return "planned";
}

export function normalizeRoadmapMilestone(value: unknown): RoadmapMilestone {
  if (!isRecord(value)) return invalidResponse("路线图里程碑响应格式无效");
  const rawProjects = Array.isArray(value.projects) ? value.projects : [];
  const rawSummary = isRecord(value.task_summary)
    ? value.task_summary
    : isRecord(value.taskSummary)
      ? value.taskSummary
      : {};
  const status = asRoadmapMilestoneStatus(value.status);
  const archivedFromRaw =
    value.archived_from_status ?? value.archivedFromStatus;
  const archivedFromStatus = asRoadmapMilestoneStatus(archivedFromRaw);
  return {
    id: String(value.id ?? ""),
    title: String(value.title ?? "未命名里程碑"),
    description: nullableString(value.description),
    year: numeric(value.year),
    quarter: Math.min(4, Math.max(1, numeric(value.quarter, 1))) as
      1 | 2 | 3 | 4,
    targetDate: String(value.target_date ?? value.targetDate ?? ""),
    status,
    manualOrder: numeric(value.manual_order ?? value.manualOrder),
    archivedFromStatus:
      archivedFromRaw === null || archivedFromStatus === "archived"
        ? null
        : archivedFromStatus,
    version: numeric(value.version, 1),
    createdAt: String(value.created_at ?? value.createdAt ?? ""),
    updatedAt: String(value.updated_at ?? value.updatedAt ?? ""),
    projects: rawProjects.filter(isRecord).map((project) => ({
      id: String(project.id ?? ""),
      name: String(project.name ?? "未命名项目"),
      status: asProjectStatus(project.status),
    })),
    taskSummary: {
      total: numeric(rawSummary.total),
      completed: numeric(rawSummary.completed),
      inProgress: numeric(rawSummary.in_progress ?? rawSummary.inProgress),
      progressPercent: numeric(
        rawSummary.progress_percent ?? rawSummary.progressPercent,
      ),
    },
  };
}

function normalizeRoadmapMilestoneListResult(
  value: unknown,
  input: RoadmapMilestoneListParams = {},
): RoadmapMilestoneListResult {
  if (!isRecord(value) || !Array.isArray(value.data)) {
    return invalidResponse("路线图里程碑列表响应格式无效");
  }
  const meta = isRecord(value.meta) ? value.meta : {};
  return {
    items: value.data.map(normalizeRoadmapMilestone),
    meta: {
      page: numeric(meta.page, input.page ?? 1),
      pageSize: numeric(meta.page_size ?? meta.pageSize, input.pageSize ?? 50),
      total: numeric(meta.total),
    },
  };
}

const contentItemStatuses = new Set<ContentItemStatus>([
  "draft",
  "in_review",
  "scheduled",
  "published",
  "cancelled",
  "archived",
]);

function asContentItemStatus(value: unknown): ContentItemStatus {
  return typeof value === "string" &&
    contentItemStatuses.has(value as ContentItemStatus)
    ? (value as ContentItemStatus)
    : "draft";
}

export function normalizeContentItem(value: unknown): ContentItem {
  if (!isRecord(value)) return invalidResponse("内容条目响应格式无效");
  const rawTasks = Array.isArray(value.tasks) ? value.tasks : [];
  const archived = value.archived_from_status ?? value.archivedFromStatus;
  const archivedStatus = asContentItemStatus(archived);
  return {
    id: String(value.id ?? ""),
    title: String(value.title ?? "未命名内容"),
    platform: String(value.platform ?? "未分类"),
    status: asContentItemStatus(value.status),
    scheduledAt: nullableString(value.scheduled_at ?? value.scheduledAt),
    scheduledTimezone: nullableString(
      value.scheduled_timezone ?? value.scheduledTimezone,
    ),
    publishedAt: nullableString(value.published_at ?? value.publishedAt),
    projectId: nullableString(value.project_id ?? value.projectId),
    notes: nullableString(value.notes),
    externalLink: nullableString(value.external_link ?? value.externalLink),
    manualOrder: numeric(value.manual_order ?? value.manualOrder),
    archivedFromStatus:
      archived === null || archivedStatus === "archived"
        ? null
        : archivedStatus,
    version: numeric(value.version, 1),
    createdAt: String(value.created_at ?? value.createdAt ?? ""),
    updatedAt: String(value.updated_at ?? value.updatedAt ?? ""),
    tasks: rawTasks.filter(isRecord).map((task) => ({
      id: String(task.id ?? ""),
      title: String(task.title ?? "未命名任务"),
      status: asTaskStatus(task.status),
      isRequired: Boolean(task.is_required ?? task.isRequired),
    })),
    requiredTaskTotal: numeric(
      value.required_task_total ?? value.requiredTaskTotal,
    ),
    requiredTaskDone: numeric(
      value.required_task_done ?? value.requiredTaskDone,
    ),
  };
}

function normalizeContentItemListResult(
  value: unknown,
  input: ContentItemListParams = {},
): ContentItemListResult {
  if (!isRecord(value) || !Array.isArray(value.data))
    return invalidResponse("内容日历列表响应格式无效");
  const meta = isRecord(value.meta) ? value.meta : {};
  return {
    items: value.data.map(normalizeContentItem),
    meta: {
      page: numeric(meta.page, input.page ?? 1),
      pageSize: numeric(meta.page_size ?? meta.pageSize, input.pageSize ?? 50),
      total: numeric(meta.total),
    },
  };
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
    latestActivityAt: clientOptionalString(
      fieldValue(value, "latest_activity_at", "latestActivityAt"),
      "客户最近活动时间",
    ),
    createdAt,
    updatedAt,
  };
}

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
    subtaskCancelled: nonNegativeInteger(
      fieldValue(value, "subtask_cancelled", "subtaskCancelled"),
      "已取消子任务数",
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

function asTaskSubmissionOrigin(value: unknown): TaskSubmission["origin"] {
  // Durable v29 idempotency snapshots do not carry origin; those records were
  // all created by the manual/migration path and are safe to normalize.
  if (value === undefined || value === "manual") return "manual";
  if (value === "child_rollup") return value;
  return invalidResponse("任务提交来源响应无效");
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

export function normalizeFocusSessionListResult(
  payload: unknown,
): FocusSessionListResult {
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("专注历史响应格式无效");
  }
  const items = payload.data.map(normalizeFocusSession);
  if (
    items.some(
      (item) =>
        (item.status !== "completed" &&
          item.status !== "cancelled" &&
          item.status !== "interrupted") ||
        item.endedAt === null,
    )
  ) {
    return invalidResponse("专注历史响应格式无效");
  }
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "专注历史页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "专注历史每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "专注历史总数"),
    },
  };
}

export function normalizeFocusReport(payload: unknown): FocusReport {
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (
    !isRecord(body) ||
    !isRecord(body.totals) ||
    !Array.isArray(body.days) ||
    !Array.isArray(body.projects) ||
    !Array.isArray(body.hours) ||
    !Array.isArray(body.heatmap) ||
    !Array.isArray(body.tags)
  ) {
    return invalidResponse("专注周期统计响应格式无效");
  }
  const dateFrom = stringField(body, "date_from", "dateFrom");
  const dateTo = stringField(body, "date_to", "dateTo");
  const timezone = stringField(body, "timezone");
  if (!dateFrom || !dateTo || !timezone) {
    return invalidResponse("专注周期统计响应格式无效");
  }
  const hours = body.hours.map((item) => {
    if (!isRecord(item)) return invalidResponse("专注小时统计响应格式无效");
    return {
      hour: nonNegativeInteger(item.hour, "专注小时"),
      sessions: nonNegativeInteger(item.sessions, "小时专注块数"),
      seconds: nonNegativeInteger(item.seconds, "小时专注秒数"),
      minutes: nonNegativeInteger(item.minutes, "小时专注分钟数"),
    };
  });
  if (hours.length !== 24 || hours.some((item, index) => item.hour !== index)) {
    return invalidResponse("专注小时统计响应格式无效");
  }
  const heatmap = body.heatmap.map((item) => {
    if (!isRecord(item)) return invalidResponse("专注热力图响应格式无效");
    return {
      weekday: positiveInteger(item.weekday, "专注热力图星期"),
      hour: nonNegativeInteger(item.hour, "专注热力图小时"),
      sessions: nonNegativeInteger(item.sessions, "热力图专注块数"),
      seconds: nonNegativeInteger(item.seconds, "热力图专注秒数"),
      minutes: nonNegativeInteger(item.minutes, "热力图专注分钟数"),
    };
  });
  if (
    heatmap.length !== 7 * 24 ||
    heatmap.some(
      (item, index) =>
        item.weekday !== Math.floor(index / 24) + 1 || item.hour !== index % 24,
    )
  ) {
    return invalidResponse("专注热力图响应格式无效");
  }
  return {
    dateFrom,
    dateTo,
    timezone,
    totals: {
      sessions: nonNegativeInteger(body.totals.sessions, "专注总块数"),
      seconds: nonNegativeInteger(body.totals.seconds, "专注总秒数"),
      minutes: nonNegativeInteger(body.totals.minutes, "专注总分钟数"),
    },
    days: body.days.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注每日统计响应格式无效");
      const date = stringField(item, "date");
      if (!date) return invalidResponse("专注每日统计响应格式无效");
      return {
        date,
        sessions: nonNegativeInteger(item.sessions, "每日专注块数"),
        seconds: nonNegativeInteger(item.seconds, "每日专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "每日专注分钟数"),
      };
    }),
    projects: body.projects.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注项目统计响应格式无效");
      const projectId = nullableString(
        fieldValue(item, "project_id", "projectId"),
      );
      const projectName = nullableString(
        fieldValue(item, "project_name", "projectName"),
      );
      if ((projectId === null) !== (projectName === null)) {
        return invalidResponse("专注项目统计响应格式无效");
      }
      return {
        projectId,
        projectName,
        sessions: nonNegativeInteger(item.sessions, "项目专注块数"),
        seconds: nonNegativeInteger(item.seconds, "项目专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "项目专注分钟数"),
      };
    }),
    hours,
    heatmap,
    tags: body.tags.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注标签统计响应格式无效");
      const tagId = nullableString(fieldValue(item, "tag_id", "tagId"));
      const tagName = nullableString(fieldValue(item, "tag_name", "tagName"));
      const tagColor = nullableString(
        fieldValue(item, "tag_color", "tagColor"),
      );
      if (
        (tagId === null) !== (tagName === null) ||
        (tagId === null) !== (tagColor === null) ||
        (tagColor !== null && !/^#[0-9A-F]{6}$/.test(tagColor))
      ) {
        return invalidResponse("专注标签统计响应格式无效");
      }
      return {
        tagId,
        tagName,
        tagColor,
        sessions: nonNegativeInteger(item.sessions, "标签专注块数"),
        seconds: nonNegativeInteger(item.seconds, "标签专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "标签专注分钟数"),
      };
    }),
    currentStreakDays: nonNegativeInteger(
      fieldValue(body, "current_streak_days", "currentStreakDays"),
      "当前连续专注天数",
    ),
    longestStreakDays: nonNegativeInteger(
      fieldValue(body, "longest_streak_days", "longestStreakDays"),
      "最长连续专注天数",
    ),
  };
}

function asInboxItemStatus(value: unknown): InboxItemStatus {
  if (
    value === "open" ||
    value === "tracking" ||
    value === "resolved" ||
    value === "dismissed"
  ) {
    return value;
  }
  return invalidResponse("收件箱状态响应无效");
}

function asInboxItemKind(value: unknown): "manual" | "reminder" | "event" {
  if (value === "manual" || value === "reminder" || value === "event") {
    return value;
  }
  return invalidResponse("收件箱类型响应无效");
}

function asInboxResolutionMode(value: unknown): InboxResolutionMode | null {
  if (value === undefined || value === null || value === "") return null;
  if (value === "manual" || value === "forced" || value === "automatic") {
    return value;
  }
  return invalidResponse("收件箱解决方式响应无效");
}

const inboxActions = new Set<InboxItemAction>([
  "read",
  "edit",
  "snooze",
  "unsnooze",
  "resolve",
  "force-resolve",
  "dismiss",
  "reopen",
]);

const systemMaintenanceDefinitions = {
  "backup:create": {
    component: "backup",
    operation: "create",
    failureCode: "backup_create_failed",
    message:
      "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
  },
  "backup:verify": {
    component: "backup",
    operation: "verify",
    failureCode: "backup_verify_failed",
    message:
      "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
  },
  "backup:drill": {
    component: "backup",
    operation: "drill",
    failureCode: "backup_drill_failed",
    message:
      "无法在隔离环境中完成本地备份恢复演练。现有工作区数据没有被修改。请检查备份状态后重试。",
  },
  "backup:restore": {
    component: "backup",
    operation: "restore",
    failureCode: "backup_restore_failed",
    message:
      "无法安全安排本地备份恢复。现有工作区数据没有被修改。请检查本地存储后重试。",
  },
  "database:startup": {
    component: "database",
    operation: "startup",
    failureCode: "database_startup_failed",
    message:
      "上次启动未能安全打开本地数据库。工作区没有进入就绪状态；请检查本地存储和应用日志。",
  },
  "database:migration": {
    component: "database",
    operation: "migration",
    failureCode: "database_migration_failed",
    message:
      "上次启动未能完成受保护的数据库迁移。已有数据未被新版本继续使用；请检查回滚备份和应用日志。",
  },
  "database:runtime": {
    component: "database",
    operation: "runtime",
    failureCode: "database_runtime_failed",
    message:
      "运行中的本地数据库操作失败。请检查可用磁盘空间和应用日志，并在继续重要写入前创建或校验备份。",
  },
  "storage:low_space": {
    component: "storage",
    operation: "low_space",
    failureCode: "storage_low_space",
    message:
      "本地数据或备份所在磁盘的可用空间已低于 1 GiB。请释放空间，并在继续重要写入前创建或校验备份。",
  },
  "sidecar:startup": {
    component: "sidecar",
    operation: "startup",
    failureCode: "sidecar_startup_failed",
    message: "上次本地服务启动未能进入就绪状态。请检查应用日志后重新启动。",
  },
} as const;

export function normalizeInboxItem(value: unknown): InboxItem {
  if (!isRecord(value)) return invalidResponse("收件箱条目响应格式无效");
  const id = stringField(value, "id");
  const title = stringField(value, "title");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawSummary = fieldValue(value, "summary");
  const rawPayload = fieldValue(value, "payload_json", "payloadJson");
  const rawActions = fieldValue(value, "available_actions", "availableActions");
  const kind = asInboxItemKind(value.kind);
  const sourceEntityType = fieldValue(
    value,
    "source_entity_type",
    "sourceEntityType",
  );
  const sourceEntityId = nullableString(
    fieldValue(value, "source_entity_id", "sourceEntityId"),
  );
  const sourceEventKey = nullableString(
    fieldValue(value, "source_event_key", "sourceEventKey"),
  );
  const validTaskArtifactEvent =
    sourceEntityType === "task_artifact" &&
    !!sourceEntityId &&
    sourceEventKey === `task-artifact:${sourceEntityId}:followup` &&
    isRecord(rawPayload) &&
    rawPayload.artifact_id === sourceEntityId &&
    typeof rawPayload.artifact_name === "string" &&
    typeof rawPayload.task_id === "string" &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.submission_id === "string" &&
    typeof rawPayload.submission_sequence === "number";
  const validTaskBlockedEvent =
    sourceEntityType === "task" &&
    !!sourceEntityId &&
    isRecord(rawPayload) &&
    rawPayload.task_id === sourceEntityId &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.blocked_reason === "string" &&
    typeof rawPayload.blocked_at === "string" &&
    (rawPayload.blocked_from_status === "todo" ||
      rawPayload.blocked_from_status === "in_progress" ||
      rawPayload.blocked_from_status === "waiting_review") &&
    Number.isInteger(rawPayload.block_version) &&
    (rawPayload.block_version as number) >= 2 &&
    sourceEventKey ===
      `task:${sourceEntityId}:blocked:${String(rawPayload.block_version)}`;
  const validTaskDueEvent =
    sourceEntityType === "task_due" &&
    !!sourceEntityId &&
    isRecord(rawPayload) &&
    rawPayload.task_id === sourceEntityId &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.due_at === "string" &&
    typeof rawPayload.projected_at === "string" &&
    (rawPayload.due_state === "due_soon" ||
      rawPayload.due_state === "overdue") &&
    rawPayload.lead_minutes === 1440 &&
    sourceEventKey === `task:${sourceEntityId}:due:${rawPayload.due_at}`;
  const dueAt = nullableString(fieldValue(value, "due_at", "dueAt"));
  const sourceDeletedAt = nullableString(
    fieldValue(value, "source_deleted_at", "sourceDeletedAt"),
  );
  const validClientFollowupEvent =
    sourceEntityType === "client_followup" &&
    !!sourceEntityId &&
    sourceDeletedAt === null &&
    !!dueAt &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.client_followup_id === sourceEntityId &&
    typeof rawPayload.client_id === "string" &&
    rawPayload.client_id.trim().length > 0 &&
    rawPayload.scheduled_at === dueAt &&
    typeof rawPayload.timezone === "string" &&
    rawPayload.timezone.trim().length > 0 &&
    typeof rawPayload.channel === "string" &&
    rawPayload.channel.trim().length > 0 &&
    typeof sourceEventKey === "string" &&
    new RegExp(`^followup:${sourceEntityId}:due:[1-9]\\d*$`).test(
      sourceEventKey,
    );
  const validContentItemEvent =
    sourceEntityType === "content_item" &&
    !!sourceEntityId &&
    !!dueAt &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.content_item_id === sourceEntityId &&
    (rawPayload.event_type === "review_due" ||
      rawPayload.event_type === "publish_due") &&
    Number.isInteger(rawPayload.content_version) &&
    (rawPayload.content_version as number) >= 1 &&
    rawPayload.scheduled_at === dueAt &&
    typeof rawPayload.scheduled_timezone === "string" &&
    rawPayload.scheduled_timezone.trim().length > 0 &&
    sourceEventKey ===
      `content:${sourceEntityId}:${String(rawPayload.event_type)}:${String(rawPayload.content_version)}`;
  const validProjectCompletionEvent =
    sourceEntityType === "project_completion" &&
    !!sourceEntityId &&
    dueAt === null &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.project_id === sourceEntityId &&
    typeof rawPayload.project_name === "string" &&
    rawPayload.project_name.trim().length > 0 &&
    typeof rawPayload.completed_at === "string" &&
    rawPayload.completed_at.length > 0 &&
    Number.isInteger(rawPayload.completion_version) &&
    (rawPayload.completion_version as number) >= 2 &&
    Number.isInteger(rawPayload.incomplete_task_count) &&
    (rawPayload.incomplete_task_count as number) >= 0 &&
    sourceEventKey ===
      `project:${sourceEntityId}:completed:${String(rawPayload.completion_version)}`;
  const maintenanceDefinition =
    typeof sourceEntityId === "string"
      ? systemMaintenanceDefinitions[
          sourceEntityId as keyof typeof systemMaintenanceDefinitions
        ]
      : undefined;
  const validSystemMaintenanceEvent =
    sourceEntityType === "system_maintenance" &&
    !!sourceEntityId &&
    !!maintenanceDefinition &&
    dueAt === null &&
    sourceDeletedAt === null &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.component === maintenanceDefinition.component &&
    rawPayload.operation === maintenanceDefinition.operation &&
    rawPayload.failure_code === maintenanceDefinition.failureCode &&
    typeof rawPayload.occurred_at === "string" &&
    rawPayload.occurred_at.length > 0 &&
    rawPayload.message === maintenanceDefinition.message &&
    typeof sourceEventKey === "string" &&
    sourceEventKey.startsWith(`system:${sourceEntityId}:`) &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      sourceEventKey.slice(`system:${sourceEntityId}:`.length),
    );
  if (
    !id ||
    !title ||
    !createdAt ||
    !updatedAt ||
    typeof rawSummary !== "string" ||
    (sourceEntityType !== "manual" &&
      sourceEntityType !== "reminder" &&
      sourceEntityType !== "task_artifact" &&
      sourceEntityType !== "task" &&
      sourceEntityType !== "task_due" &&
      sourceEntityType !== "client_followup" &&
      sourceEntityType !== "content_item" &&
      sourceEntityType !== "project_completion" &&
      sourceEntityType !== "system_maintenance") ||
    (kind === "manual" &&
      (sourceEntityType !== "manual" ||
        sourceEntityId !== null ||
        sourceEventKey !== null)) ||
    (kind === "reminder" &&
      (sourceEntityType !== "reminder" ||
        !sourceEntityId ||
        !sourceEventKey)) ||
    (kind === "event" &&
      !validTaskArtifactEvent &&
      !validTaskBlockedEvent &&
      !validTaskDueEvent &&
      !validClientFollowupEvent &&
      !validContentItemEvent &&
      !validProjectCompletionEvent &&
      !validSystemMaintenanceEvent) ||
    (fieldValue(value, "resolution_policy", "resolutionPolicy") !== "manual" &&
      fieldValue(value, "resolution_policy", "resolutionPolicy") !==
        "all_required_tasks_done") ||
    (rawPayload !== undefined && !isRecord(rawPayload)) ||
    !Array.isArray(rawActions)
  ) {
    return invalidResponse("收件箱条目响应格式无效");
  }

  return {
    id,
    kind,
    title,
    summary: rawSummary,
    sourceEntityType,
    sourceEntityId,
    sourceEventKey,
    sourceDeletedAt,
    priority: asTaskPriority(value.priority),
    status: asInboxItemStatus(value.status),
    resolutionPolicy: fieldValue(
      value,
      "resolution_policy",
      "resolutionPolicy",
    ) as InboxItem["resolutionPolicy"],
    dueAt,
    readAt: nullableString(fieldValue(value, "read_at", "readAt")),
    triagedAt: nullableString(fieldValue(value, "triaged_at", "triagedAt")),
    snoozedUntil: nullableString(
      fieldValue(value, "snoozed_until", "snoozedUntil"),
    ),
    resolvedByActorId: nullableString(
      fieldValue(value, "resolved_by_actor_id", "resolvedByActorId"),
    ),
    resolvedAt: nullableString(fieldValue(value, "resolved_at", "resolvedAt")),
    resolutionReason: nullableString(
      fieldValue(value, "resolution_reason", "resolutionReason"),
    ),
    resolutionMode: asInboxResolutionMode(
      fieldValue(value, "resolution_mode", "resolutionMode"),
    ),
    dismissedByActorId: nullableString(
      fieldValue(value, "dismissed_by_actor_id", "dismissedByActorId"),
    ),
    dismissedAt: nullableString(
      fieldValue(value, "dismissed_at", "dismissedAt"),
    ),
    dismissReason: nullableString(
      fieldValue(value, "dismiss_reason", "dismissReason"),
    ),
    payloadJson: isRecord(rawPayload) ? rawPayload : {},
    version: positiveInteger(value.version, "收件箱条目版本"),
    createdAt,
    updatedAt,
    availableActions: rawActions.filter(
      (action): action is InboxItemAction =>
        typeof action === "string" &&
        inboxActions.has(action as InboxItemAction),
    ),
  };
}

export function normalizeInboxItemListResult(
  value: unknown,
): InboxItemListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("收件箱列表响应格式无效");
  }
  const items = value.data.map(normalizeInboxItem);
  const meta = value.meta;
  const snapshotAt = stringField(meta, "snapshot_at", "snapshotAt");
  const serverNow = stringField(meta, "server_now", "serverNow");
  if (!snapshotAt || !serverNow) {
    return invalidResponse("收件箱列表缺少服务端时间基准");
  }
  const result: InboxItemListResult = {
    items,
    meta: {
      page: positiveInteger(meta.page, "收件箱页码"),
      pageSize: positiveInteger(
        fieldValue(meta, "page_size", "pageSize"),
        "收件箱分页大小",
      ),
      total: nonNegativeInteger(meta.total, "收件箱总数"),
      unreadTotal: nonNegativeInteger(
        fieldValue(meta, "unread_total", "unreadTotal"),
        "收件箱未读数",
      ),
      snapshotAt,
      serverNow,
    },
  };
  if (
    result.items.length > result.meta.pageSize ||
    result.meta.total < result.items.length
  ) {
    return invalidResponse("收件箱分页响应不一致");
  }
  return result;
}

function asReminderStatus(value: unknown): ReminderStatus {
  if (value === "scheduled" || value === "fired" || value === "cancelled") {
    return value;
  }
  return invalidResponse("提醒状态响应无效");
}

function asReminderRecurrenceType(value: unknown): ReminderRecurrenceType {
  if (value === "none" || value === "daily" || value === "weekly") {
    return value;
  }
  return invalidResponse("提醒重复规则响应无效");
}

const reminderActions = new Set<ReminderAction>(["edit", "cancel"]);

export function normalizeReminder(value: unknown): Reminder {
  if (!isRecord(value)) return invalidResponse("提醒响应格式无效");
  const id = stringField(value, "id");
  const title = stringField(value, "title");
  const summary = fieldValue(value, "summary");
  const triggerAt = stringField(value, "trigger_at", "triggerAt");
  const sourceEventKey = stringField(
    value,
    "source_event_key",
    "sourceEventKey",
  );
  const createdByActorId = stringField(
    value,
    "created_by_actor_id",
    "createdByActorId",
  );
  const seriesId = stringField(value, "series_id", "seriesId");
  const recurrenceType = asReminderRecurrenceType(
    fieldValue(value, "recurrence_type", "recurrenceType"),
  );
  const recurrenceInterval = fieldValue(
    value,
    "recurrence_interval",
    "recurrenceInterval",
  );
  const recurrenceTimezone = stringField(
    value,
    "recurrence_timezone",
    "recurrenceTimezone",
  );
  const occurrenceNumber = fieldValue(
    value,
    "occurrence_number",
    "occurrenceNumber",
  );
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawActions = fieldValue(value, "available_actions", "availableActions");
  const status = asReminderStatus(value.status);
  const sourceEntityId = nullableString(
    fieldValue(value, "source_entity_id", "sourceEntityId"),
  );
  const sourceEntityType = fieldValue(
    value,
    "source_entity_type",
    "sourceEntityType",
  );
  const firedAt = nullableString(fieldValue(value, "fired_at", "firedAt"));
  const inboxItemId = nullableString(
    fieldValue(value, "inbox_item_id", "inboxItemId"),
  );
  const cancelledByActorId = nullableString(
    fieldValue(value, "cancelled_by_actor_id", "cancelledByActorId"),
  );
  const cancelledAt = nullableString(
    fieldValue(value, "cancelled_at", "cancelledAt"),
  );
  const cancelReason = nullableString(
    fieldValue(value, "cancel_reason", "cancelReason"),
  );
  if (
    !id ||
    !title ||
    typeof summary !== "string" ||
    !triggerAt ||
    !sourceEventKey ||
    !createdByActorId ||
    !seriesId ||
    !Number.isInteger(recurrenceInterval) ||
    Number(recurrenceInterval) < 1 ||
    Number(recurrenceInterval) > 365 ||
    !recurrenceTimezone ||
    !Number.isInteger(occurrenceNumber) ||
    Number(occurrenceNumber) < 1 ||
    (recurrenceType === "none" &&
      (recurrenceInterval !== 1 || recurrenceTimezone !== "UTC")) ||
    !createdAt ||
    !updatedAt ||
    (sourceEntityType !== "manual" && sourceEntityType !== "automation") ||
    (sourceEntityType === "manual" && sourceEntityId !== null) ||
    (sourceEntityType === "automation" && sourceEntityId === null) ||
    !Array.isArray(rawActions) ||
    (status === "scheduled" &&
      (firedAt !== null ||
        inboxItemId !== null ||
        cancelledByActorId !== null ||
        cancelledAt !== null ||
        cancelReason !== null)) ||
    (status === "fired" &&
      (!firedAt ||
        !inboxItemId ||
        cancelledByActorId !== null ||
        cancelledAt !== null ||
        cancelReason !== null)) ||
    (status === "cancelled" &&
      (firedAt !== null ||
        inboxItemId !== null ||
        !cancelledByActorId ||
        !cancelledAt ||
        !cancelReason))
  ) {
    return invalidResponse("提醒响应格式无效");
  }
  const actions = rawActions.filter(
    (action): action is ReminderAction =>
      typeof action === "string" &&
      reminderActions.has(action as ReminderAction),
  );
  if (
    actions.length !== rawActions.length ||
    (status === "scheduled" &&
      (actions.length !== 2 ||
        !actions.includes("edit") ||
        !actions.includes("cancel"))) ||
    (status !== "scheduled" && actions.length !== 0)
  ) {
    return invalidResponse("提醒可用操作响应不一致");
  }
  return {
    id,
    sourceEntityType,
    sourceEntityId,
    title,
    summary,
    priority: asTaskPriority(value.priority),
    triggerAt,
    status,
    sourceEventKey,
    createdByActorId,
    seriesId,
    recurrenceType,
    recurrenceInterval: Number(recurrenceInterval),
    recurrenceTimezone,
    occurrenceNumber: Number(occurrenceNumber),
    firedAt,
    inboxItemId,
    cancelledByActorId,
    cancelledAt,
    cancelReason,
    version: positiveInteger(value.version, "提醒版本"),
    createdAt,
    updatedAt,
    availableActions: actions,
  };
}

export function normalizeReminderListResult(
  value: unknown,
): ReminderListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("提醒列表响应格式无效");
  }
  const items = value.data.map(normalizeReminder);
  const serverNow = stringField(value.meta, "server_now", "serverNow");
  if (!serverNow) return invalidResponse("提醒列表缺少服务端时间基准");
  const result: ReminderListResult = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "提醒页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "提醒分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "提醒总数"),
      serverNow,
    },
  };
  if (
    result.items.length > result.meta.pageSize ||
    result.meta.total < result.items.length
  ) {
    return invalidResponse("提醒分页响应不一致");
  }
  return result;
}

function asAutomationRuleStatus(value: unknown): AutomationRuleStatus {
  if (value === "enabled" || value === "disabled" || value === "unavailable") {
    return value;
  }
  return invalidResponse("自动化规则状态响应无效");
}

function asAutomationTriggerType(value: unknown): AutomationTriggerType {
  if (value === "event" || value === "schedule") return value;
  return invalidResponse("自动化触发类型响应无效");
}

function asAutomationActionType(value: unknown): AutomationActionType {
  if (value === "inbox_item" || value === "task" || value === "reminder") {
    return value;
  }
  return invalidResponse("自动化动作类型响应无效");
}

function asAutomationRunStatus(value: unknown): AutomationRunStatus {
  if (
    value === "succeeded" ||
    value === "failed" ||
    value === "skipped" ||
    value === "cancelled"
  ) {
    return value;
  }
  return invalidResponse("自动化运行状态响应无效");
}

function normalizeAutomationConfig(value: unknown): AutomationConfig {
  if (!isRecord(value)) return invalidResponse("自动化配置响应格式无效");
  const priority = fieldValue(value, "priority");
  const localTime = fieldValue(value, "local_time", "localTime");
  const timezone = fieldValue(value, "timezone");
  if (priority !== undefined) {
    if (localTime !== undefined || timezone !== undefined) {
      return invalidResponse("自动化配置响应不一致");
    }
    return { priority: asTaskPriority(priority) };
  }
  if (
    typeof localTime !== "string" ||
    !/^\d{2}:\d{2}$/.test(localTime) ||
    typeof timezone !== "string" ||
    !timezone
  ) {
    return invalidResponse("自动化计划配置响应无效");
  }
  return { localTime, timezone };
}

function stringArray(value: unknown, label: string): string[] {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || !item)
  ) {
    return invalidResponse(`${label}响应无效`);
  }
  return [...value];
}

export function normalizeAutomationRule(value: unknown): AutomationRule {
  if (!isRecord(value)) return invalidResponse("自动化规则响应格式无效");
  const id = stringField(value, "id");
  const presetKey = stringField(value, "preset_key", "presetKey");
  const name = stringField(value, "name");
  const description = stringField(value, "description");
  const triggerLabel = stringField(value, "trigger_label", "triggerLabel");
  const actionLabel = stringField(value, "action_label", "actionLabel");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const available = value.available;
  const unavailableReason = fieldValue(
    value,
    "unavailable_reason",
    "unavailableReason",
  );
  const nextRunAt = nullableString(
    fieldValue(value, "next_run_at", "nextRunAt"),
  );
  const status = asAutomationRuleStatus(value.status);
  const triggerType = asAutomationTriggerType(
    fieldValue(value, "trigger_type", "triggerType"),
  );
  if (
    !id ||
    !presetKey ||
    !name ||
    !description ||
    !triggerLabel ||
    !actionLabel ||
    !createdAt ||
    !updatedAt ||
    typeof available !== "boolean" ||
    (unavailableReason !== undefined &&
      typeof unavailableReason !== "string") ||
    (status === "unavailable") !== !available ||
    (status === "enabled" && !available) ||
    (triggerType === "event" && nextRunAt !== null)
  ) {
    return invalidResponse("自动化规则响应不一致");
  }
  return {
    id,
    presetKey,
    name,
    description,
    status,
    available,
    unavailableReason:
      typeof unavailableReason === "string" ? unavailableReason : "",
    triggerType,
    triggerLabel,
    actionType: asAutomationActionType(
      fieldValue(value, "action_type", "actionType"),
    ),
    actionLabel,
    config: normalizeAutomationConfig(value.config),
    nextRunAt,
    permissions: stringArray(value.permissions, "自动化权限"),
    version: positiveInteger(value.version, "自动化规则版本"),
    createdAt,
    updatedAt,
  };
}

export function normalizeAutomationPreview(value: unknown): AutomationPreview {
  if (!isRecord(value)) return invalidResponse("自动化预览响应格式无效");
  const triggerSummary = stringField(
    value,
    "trigger_summary",
    "triggerSummary",
  );
  const actionSummary = stringField(value, "action_summary", "actionSummary");
  const unavailableReason = fieldValue(
    value,
    "unavailable_reason",
    "unavailableReason",
  );
  if (
    typeof value.can_enable !== "boolean" ||
    !triggerSummary ||
    !actionSummary ||
    (unavailableReason !== undefined && typeof unavailableReason !== "string")
  ) {
    return invalidResponse("自动化预览响应不一致");
  }
  return {
    canEnable: value.can_enable,
    unavailableReason:
      typeof unavailableReason === "string" ? unavailableReason : "",
    triggerSummary,
    actionSummary,
    config: normalizeAutomationConfig(value.config),
    nextRunAt: nullableString(fieldValue(value, "next_run_at", "nextRunAt")),
    permissions: stringArray(value.permissions, "自动化权限"),
  };
}

export function normalizeAutomationRun(value: unknown): AutomationRun {
  if (!isRecord(value)) return invalidResponse("自动化运行响应格式无效");
  const requiredStrings = {
    id: stringField(value, "id"),
    ruleId: stringField(value, "rule_id", "ruleId"),
    presetKey: stringField(value, "preset_key", "presetKey"),
    ruleName: stringField(value, "rule_name", "ruleName"),
    resultSummary: stringField(value, "result_summary", "resultSummary"),
    startedAt: stringField(value, "started_at", "startedAt"),
    endedAt: stringField(value, "ended_at", "endedAt"),
  };
  if (Object.values(requiredStrings).some((item) => !item)) {
    return invalidResponse("自动化运行响应缺少必要字段");
  }
  const triggerType = asAutomationTriggerType(
    fieldValue(value, "trigger_type", "triggerType"),
  );
  const sourceEventId = nullableString(
    fieldValue(value, "source_event_id", "sourceEventId"),
  );
  const scheduledFor = nullableString(
    fieldValue(value, "scheduled_for", "scheduledFor"),
  );
  const retryable = value.retryable;
  const configSnapshot = fieldValue(value, "config_snapshot", "configSnapshot");
  const actionSnapshot = fieldValue(value, "action_snapshot", "actionSnapshot");
  const attempt = positiveInteger(value.attempt, "自动化运行次数");
  const causalDepth = nonNegativeInteger(
    fieldValue(value, "causal_depth", "causalDepth"),
    "自动化因果深度",
  );
  if (
    typeof retryable !== "boolean" ||
    !isRecord(configSnapshot) ||
    !isRecord(actionSnapshot) ||
    attempt > 3 ||
    causalDepth > 4 ||
    (triggerType === "event" && (!sourceEventId || scheduledFor !== null)) ||
    (triggerType === "schedule" && (sourceEventId !== null || !scheduledFor))
  ) {
    return invalidResponse("自动化运行响应不一致");
  }
  return {
    id: requiredStrings.id!,
    ruleId: requiredStrings.ruleId!,
    presetKey: requiredStrings.presetKey!,
    ruleName: requiredStrings.ruleName!,
    ruleVersion: positiveInteger(
      fieldValue(value, "rule_version", "ruleVersion"),
      "自动化规则版本",
    ),
    triggerType,
    sourceEventId,
    scheduledFor,
    status: asAutomationRunStatus(value.status),
    attempt,
    retryOfRunId: nullableString(
      fieldValue(value, "retry_of_run_id", "retryOfRunId"),
    ),
    retryable,
    retryAt: nullableString(fieldValue(value, "retry_at", "retryAt")),
    causedByRunId: nullableString(
      fieldValue(value, "caused_by_run_id", "causedByRunId"),
    ),
    causalDepth,
    configSnapshot: { ...configSnapshot },
    actionSnapshot: { ...actionSnapshot },
    errorCode: nullableString(fieldValue(value, "error_code", "errorCode")),
    resultType: nullableString(fieldValue(value, "result_type", "resultType")),
    resultId: nullableString(fieldValue(value, "result_id", "resultId")),
    resultSummary: requiredStrings.resultSummary!,
    startedAt: requiredStrings.startedAt!,
    endedAt: requiredStrings.endedAt!,
  };
}

export function normalizeAutomationRunListResult(
  value: unknown,
): AutomationRunListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("自动化运行列表响应格式无效");
  }
  const items = value.data.map(normalizeAutomationRun);
  const result = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "自动化运行页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "自动化运行分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "自动化运行总数"),
    },
  };
  if (items.length > result.meta.pageSize || result.meta.total < items.length) {
    return invalidResponse("自动化运行分页响应不一致");
  }
  return result;
}

function normalizeAgentAdapterStatus(value: unknown): AgentAdapter["status"] {
  if (value === "enabled" || value === "disabled") return value;
  return invalidResponse("本地 Agent 适配器状态响应无效");
}

function normalizeAgentAdapterHealthStatus(
  value: unknown,
): AgentAdapter["healthStatus"] {
  if (
    value === "unknown" ||
    value === "blocked" ||
    value === "healthy" ||
    value === "unhealthy"
  ) {
    return value;
  }
  return invalidResponse("本地 Agent 健康状态响应无效");
}

function normalizeAgentAdapterIsolationStatus(
  value: unknown,
): AgentAdapter["isolationStatus"] {
  if (
    value === "unverified" ||
    value === "verified" ||
    value === "unsupported"
  ) {
    return value;
  }
  return invalidResponse("本地 Agent 隔离状态响应无效");
}

export function normalizeAgentAdapter(value: unknown): AgentAdapter {
  if (!isRecord(value)) {
    return invalidResponse("本地 Agent 适配器响应格式无效");
  }
  const manifest = value.manifest;
  const readiness = value.readiness;
  if (
    !isRecord(manifest) ||
    !isRecord(readiness) ||
    "executable_ref" in value ||
    "executableRef" in value ||
    "path" in value ||
    manifest.execution_mode !== "short_lived_process" ||
    typeof readiness.can_enable !== "boolean" ||
    typeof value.execution_ready !== "boolean"
  ) {
    return invalidResponse("本地 Agent 适配器响应格式无效");
  }
  const id = stringField(value, "id");
  const adapterKey = stringField(value, "adapter_key", "adapterKey");
  const displayName = stringField(value, "display_name", "displayName");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const healthErrorCode = nullableString(
    fieldValue(value, "health_error_code", "healthErrorCode"),
  );
  const lastHealthAt = nullableString(
    fieldValue(value, "last_health_at", "lastHealthAt"),
  );
  const unavailableCode = fieldValue(
    readiness,
    "unavailable_code",
    "unavailableCode",
  );
  if (
    !id ||
    !adapterKey ||
    !displayName ||
    !createdAt ||
    !updatedAt ||
    value.kind !== "builtin" ||
    fieldValue(value, "protocol_version", "protocolVersion") !==
      "opc-agent-pipe-v1" ||
    (unavailableCode !== undefined && typeof unavailableCode !== "string") ||
    (lastHealthAt !== null && Number.isNaN(Date.parse(lastHealthAt))) ||
    (healthErrorCode !== null && lastHealthAt === null)
  ) {
    return invalidResponse("本地 Agent 适配器响应不一致");
  }
  const capabilities = stringArray(manifest.capabilities, "本地 Agent 能力");
  const requirements = stringArray(
    manifest.requirements,
    "本地 Agent 安全要求",
  );
  const requiredGates = stringArray(
    fieldValue(readiness, "required_gates", "requiredGates"),
    "本地 Agent 启用闸门",
  );
  const executionReady = value.execution_ready;
  const canEnable = readiness.can_enable;
  if (
    executionReady !== canEnable ||
    requirements.length !== requiredGates.length ||
    requirements.some((gate, index) => gate !== requiredGates[index]) ||
    (executionReady && unavailableCode) ||
    (!executionReady && !unavailableCode)
  ) {
    return invalidResponse("本地 Agent 就绪状态响应不一致");
  }
  return {
    id,
    adapterKey,
    kind: "builtin",
    displayName,
    protocolVersion: "opc-agent-pipe-v1",
    manifest: {
      executionMode: "short_lived_process",
      capabilities,
      requirements,
    },
    status: normalizeAgentAdapterStatus(value.status),
    healthStatus: normalizeAgentAdapterHealthStatus(
      fieldValue(value, "health_status", "healthStatus"),
    ),
    healthErrorCode,
    isolationStatus: normalizeAgentAdapterIsolationStatus(
      fieldValue(value, "isolation_status", "isolationStatus"),
    ),
    executionReady,
    lastHealthAt,
    readiness: {
      canEnable,
      unavailableCode:
        typeof unavailableCode === "string" ? unavailableCode : "",
      requiredGates,
    },
    version: positiveInteger(value.version, "本地 Agent 适配器版本"),
    createdAt,
    updatedAt,
  };
}

function asInboxTaskRelationType(value: unknown): "created" | "linked" {
  if (value === "created" || value === "linked") return value;
  return invalidResponse("收件箱任务关系类型响应无效");
}

function normalizeInboxTaskSummary(value: unknown): InboxTaskSummary {
  if (!isRecord(value)) return invalidResponse("关联任务摘要响应格式无效");
  const id = stringField(value, "id");
  const title = stringField(value, "title");
  if (!id || !title) return invalidResponse("关联任务摘要响应格式无效");
  const projectId = nullableString(
    fieldValue(value, "project_id", "projectId"),
  );
  const projectName = nullableString(
    fieldValue(value, "project_name", "projectName"),
  );
  if (
    fieldValue(value, "project_id", "projectId") === undefined ||
    fieldValue(value, "project_name", "projectName") === undefined ||
    (value.kind !== "work" &&
      value.kind !== "review" &&
      value.kind !== "followup" &&
      value.kind !== "reminder") ||
    (projectId === null) !== (projectName === null)
  ) {
    return invalidResponse("关联任务项目摘要响应不一致");
  }
  return {
    id,
    title,
    status: asTaskStatus(value.status),
    priority: asTaskPriority(value.priority),
    kind: asTaskKind(value.kind),
    projectId,
    projectName,
    version: positiveInteger(value.version, "关联任务版本"),
  };
}

export function normalizeInboxTaskProgress(value: unknown): InboxTaskProgress {
  if (!isRecord(value)) return invalidResponse("关联任务进度响应格式无效");
  const rawPercent = fieldValue(value, "percent");
  const percent =
    rawPercent === null
      ? null
      : typeof rawPercent === "number" &&
          Number.isFinite(rawPercent) &&
          rawPercent >= 0 &&
          rawPercent <= 100
        ? rawPercent
        : invalidResponse("关联任务进度百分比响应无效");
  const allRequiredDone = fieldValue(
    value,
    "all_required_done",
    "allRequiredDone",
  );
  if (typeof allRequiredDone !== "boolean") {
    return invalidResponse("关联任务完成状态响应无效");
  }
  const progress: InboxTaskProgress = {
    activeTotal: nonNegativeInteger(
      fieldValue(value, "active_total", "activeTotal"),
      "活动关联任务数",
    ),
    requiredTotal: nonNegativeInteger(
      fieldValue(value, "required_total", "requiredTotal"),
      "必需任务数",
    ),
    requiredDone: nonNegativeInteger(
      fieldValue(value, "required_done", "requiredDone"),
      "已完成必需任务数",
    ),
    requiredRemaining: nonNegativeInteger(
      fieldValue(value, "required_remaining", "requiredRemaining"),
      "未完成必需任务数",
    ),
    requiredBlocked: nonNegativeInteger(
      fieldValue(value, "required_blocked", "requiredBlocked"),
      "阻塞必需任务数",
    ),
    requiredWaitingReview: nonNegativeInteger(
      fieldValue(value, "required_waiting_review", "requiredWaitingReview"),
      "待验收必需任务数",
    ),
    requiredCancelled: nonNegativeInteger(
      fieldValue(value, "required_cancelled", "requiredCancelled"),
      "已取消必需任务数",
    ),
    percent,
    allRequiredDone,
  };
  if (
    progress.requiredDone + progress.requiredRemaining !==
      progress.requiredTotal ||
    progress.requiredTotal > progress.activeTotal ||
    progress.requiredBlocked > progress.requiredRemaining ||
    progress.requiredWaitingReview > progress.requiredRemaining ||
    progress.requiredCancelled > progress.requiredRemaining ||
    progress.requiredBlocked +
      progress.requiredWaitingReview +
      progress.requiredCancelled >
      progress.requiredRemaining ||
    (progress.requiredTotal === 0 && progress.percent !== null) ||
    (progress.requiredTotal > 0 && progress.percent === null) ||
    progress.allRequiredDone !==
      (progress.requiredTotal > 0 && progress.requiredRemaining === 0) ||
    (progress.percent !== null &&
      progress.percent !==
        Math.floor((progress.requiredDone * 100) / progress.requiredTotal))
  ) {
    return invalidResponse("关联任务进度响应不一致");
  }
  return progress;
}

export function normalizeInboxItemTaskRelation(
  value: unknown,
): InboxItemTaskRelation {
  if (!isRecord(value)) return invalidResponse("收件箱任务关系响应格式无效");
  const id = stringField(value, "id");
  const inboxItemId = stringField(value, "inbox_item_id", "inboxItemId");
  const taskRefId = stringField(value, "task_ref_id", "taskRefId");
  const taskTitleSnapshot = stringField(
    value,
    "task_title_snapshot",
    "taskTitleSnapshot",
  );
  const linkedByActorId = stringField(
    value,
    "linked_by_actor_id",
    "linkedByActorId",
  );
  const linkedAt = stringField(value, "linked_at", "linkedAt");
  const isRequired = fieldValue(value, "is_required", "isRequired");
  const isActive = fieldValue(value, "is_active", "isActive");
  const taskDeleted = fieldValue(value, "task_deleted", "taskDeleted");
  const rawTask = fieldValue(value, "task");
  const rawLinkedActor = fieldValue(value, "linked_by_actor", "linkedByActor");
  const rawUnlinkedActor = fieldValue(
    value,
    "unlinked_by_actor",
    "unlinkedByActor",
  );
  if (
    !id ||
    !inboxItemId ||
    !taskRefId ||
    !taskTitleSnapshot ||
    !linkedByActorId ||
    !linkedAt ||
    typeof isRequired !== "boolean" ||
    typeof isActive !== "boolean" ||
    typeof taskDeleted !== "boolean" ||
    fieldValue(value, "task_id", "taskId") === undefined ||
    fieldValue(value, "unlinked_by_actor_id", "unlinkedByActorId") ===
      undefined ||
    fieldValue(value, "unlinked_at", "unlinkedAt") === undefined ||
    fieldValue(value, "unlink_reason", "unlinkReason") === undefined ||
    !isRecord(rawLinkedActor) ||
    (rawTask !== null && !isRecord(rawTask)) ||
    (rawUnlinkedActor !== null && !isRecord(rawUnlinkedActor))
  ) {
    return invalidResponse("收件箱任务关系响应格式无效");
  }
  const taskId = nullableString(fieldValue(value, "task_id", "taskId"));
  const task = rawTask === null ? null : normalizeInboxTaskSummary(rawTask);
  const linkedByActor = normalizeActorSummary(rawLinkedActor);
  const unlinkedByActor =
    rawUnlinkedActor === null ? null : normalizeActorSummary(rawUnlinkedActor);
  const unlinkedByActorId = nullableString(
    fieldValue(value, "unlinked_by_actor_id", "unlinkedByActorId"),
  );
  const unlinkedAt = nullableString(
    fieldValue(value, "unlinked_at", "unlinkedAt"),
  );
  const unlinkReason = nullableString(
    fieldValue(value, "unlink_reason", "unlinkReason"),
  );
  if (
    linkedByActor.id !== linkedByActorId ||
    (task && (!taskId || task.id !== taskId || task.id !== taskRefId)) ||
    (taskDeleted && (taskId !== null || task !== null)) ||
    (!taskDeleted && (taskId === null || task === null)) ||
    (isActive &&
      (unlinkedByActorId !== null ||
        unlinkedByActor !== null ||
        unlinkedAt !== null ||
        unlinkReason !== null)) ||
    (!isActive &&
      (!unlinkedByActorId ||
        !unlinkedByActor ||
        !unlinkedAt ||
        !unlinkReason)) ||
    (unlinkedByActor && unlinkedByActor.id !== unlinkedByActorId)
  ) {
    return invalidResponse("收件箱任务关系响应不一致");
  }
  return {
    id,
    inboxItemId,
    taskRefId,
    taskId,
    taskTitleSnapshot,
    task,
    relationType: asInboxTaskRelationType(
      fieldValue(value, "relation_type", "relationType"),
    ),
    isRequired,
    position: positiveInteger(value.position, "关联任务顺序"),
    linkedByActorId,
    linkedByActor,
    linkedAt,
    unlinkedByActorId,
    unlinkedByActor,
    unlinkedAt,
    unlinkReason,
    isActive,
    taskDeleted,
  };
}

export function normalizeInboxItemTaskListResult(
  value: unknown,
): InboxItemTaskListResult {
  if (
    !isRecord(value) ||
    !isRecord(value.data) ||
    !Array.isArray(value.data.active) ||
    !Array.isArray(value.data.history) ||
    !isRecord(value.meta)
  ) {
    return invalidResponse("收件箱任务关系列表响应格式无效");
  }
  const active = value.data.active.map(normalizeInboxItemTaskRelation);
  const history = value.data.history.map(normalizeInboxItemTaskRelation);
  const result: InboxItemTaskListResult = {
    active,
    history,
    meta: {
      page: positiveInteger(value.meta.page, "关联任务历史页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "关联任务历史分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "关联任务历史总数"),
      inboxItemVersion: positiveInteger(
        fieldValue(value.meta, "inbox_item_version", "inboxItemVersion"),
        "收件箱条目版本",
      ),
      progress: normalizeInboxTaskProgress(value.meta.progress),
    },
  };
  const relationIDs = new Set(
    [...active, ...history].map((relation) => relation.id),
  );
  const activeTaskIDs = new Set(active.map((relation) => relation.taskRefId));
  if (
    active.some((relation) => !relation.isActive) ||
    history.some((relation) => relation.isActive) ||
    result.history.length > result.meta.pageSize ||
    result.meta.total < result.history.length ||
    result.meta.progress.activeTotal !== result.active.length ||
    relationIDs.size !== active.length + history.length ||
    activeTaskIDs.size !== active.length ||
    active.some(
      (relation, index) =>
        index > 0 && relation.position <= active[index - 1].position,
    )
  ) {
    return invalidResponse("收件箱任务关系列表响应不一致");
  }
  return result;
}

export function normalizeInboxItemTaskMutationResult(
  value: unknown,
): InboxItemTaskMutationResult {
  const body = isRecord(value) && isRecord(value.data) ? value.data : value;
  if (!isRecord(body)) return invalidResponse("收件箱任务关系操作响应格式无效");
  return {
    inboxItem: normalizeInboxItem(fieldValue(body, "inbox_item", "inboxItem")),
    relation: normalizeInboxItemTaskRelation(body.relation),
    progress: normalizeInboxTaskProgress(body.progress),
  };
}

export function normalizeSplitInboxItemResult(
  value: unknown,
): SplitInboxItemResult {
  const body = isRecord(value) && isRecord(value.data) ? value.data : value;
  if (
    !isRecord(body) ||
    !Array.isArray(body.created) ||
    body.created.length < 1 ||
    body.created.length > 20
  ) {
    return invalidResponse("收件箱任务拆分响应格式无效");
  }
  const inboxItem = normalizeInboxItem(
    fieldValue(body, "inbox_item", "inboxItem"),
  );
  const created: InboxSplitTaskResult[] = body.created.map((entry) => {
    if (
      !isRecord(entry) ||
      typeof entry.key !== "string" ||
      !entry.key ||
      !Array.isArray(entry.assignments)
    ) {
      return invalidResponse("收件箱拆分任务响应格式无效");
    }
    const task = normalizeTask(entry.task);
    const relation = normalizeInboxItemTaskRelation(entry.relation);
    const assignments = entry.assignments.map(normalizeTaskAssignment);
    if (
      relation.inboxItemId !== inboxItem.id ||
      relation.taskRefId !== task.id ||
      relation.relationType !== "created" ||
      !relation.isActive ||
      assignments.length < 1 ||
      assignments.some((assignment) => assignment.taskId !== task.id) ||
      !assignments.some((assignment) => assignment.role === "assignee") ||
      (task.reviewPolicy === "manual" &&
        !assignments.some((assignment) => assignment.role === "reviewer"))
    ) {
      return invalidResponse("收件箱拆分任务响应不一致");
    }
    return { key: entry.key, task, assignments, relation };
  });
  const keys = new Set(created.map((entry) => entry.key));
  const taskIds = new Set(created.map((entry) => entry.task.id));
  const progress = normalizeInboxTaskProgress(body.progress);
  if (
    keys.size !== created.length ||
    taskIds.size !== created.length ||
    progress.activeTotal < created.length
  ) {
    return invalidResponse("收件箱任务拆分响应不一致");
  }
  return { inboxItem, created, progress };
}

export function normalizeInboxWorkflowEvent(
  value: unknown,
): InboxWorkflowEvent {
  if (!isRecord(value)) return invalidResponse("收件箱事件响应格式无效");
  const id = stringField(value, "id");
  const action = stringField(value, "action");
  const createdAt = stringField(value, "created_at", "createdAt");
  const previous = fieldValue(value, "previous");
  const current = fieldValue(value, "current");
  const rawActor = fieldValue(value, "actor");
  const actorId = nullableString(fieldValue(value, "actor_id", "actorId"));
  if (
    !id ||
    !action ||
    !createdAt ||
    (previous !== null && previous !== undefined && !isRecord(previous)) ||
    (current !== null && current !== undefined && !isRecord(current)) ||
    (rawActor !== null && rawActor !== undefined && !isRecord(rawActor))
  ) {
    return invalidResponse("收件箱事件响应格式无效");
  }
  const actor = isRecord(rawActor) ? normalizeActorSummary(rawActor) : null;
  if ((actor && actor.id !== actorId) || (!actor && actorId)) {
    return invalidResponse("收件箱事件责任主体响应不一致");
  }
  return {
    id,
    action,
    actorId,
    actor,
    requestId: nullableString(fieldValue(value, "request_id", "requestId")),
    previous: isRecord(previous) ? previous : null,
    current: isRecord(current) ? current : null,
    reason: nullableString(value.reason),
    createdAt,
  };
}

export function normalizeInboxEventListResult(
  value: unknown,
): InboxEventListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("收件箱事件列表响应格式无效");
  }
  const items = value.data.map(normalizeInboxWorkflowEvent);
  return {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "收件箱事件页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "收件箱事件分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "收件箱事件总数"),
      inboxItemVersion: positiveInteger(
        fieldValue(value.meta, "inbox_item_version", "inboxItemVersion"),
        "收件箱事件条目版本",
      ),
    },
  };
}

export function normalizeHealthResponse(value: unknown): HealthResponse {
  if (
    !isRecord(value) ||
    !isRecord(value.app) ||
    !isRecord(value.api) ||
    !isRecord(value.schema)
  ) {
    return invalidResponse("健康检查响应格式无效");
  }
  const status = stringField(value, "status");
  const appName = stringField(value.app, "name");
  const appVersion = stringField(value.app, "version");
  const commit = stringField(value.app, "commit");
  const apiVersion = stringField(value.api, "version");
  if (
    !status ||
    !appName ||
    !appVersion ||
    !commit ||
    !apiVersion ||
    status.length > 32 ||
    appName.length > 128 ||
    appVersion.length > 128 ||
    commit.length > 128 ||
    apiVersion.length > 64
  ) {
    return invalidResponse("健康检查响应字段无效");
  }
  return {
    status,
    app: { name: appName, version: appVersion, commit },
    api: { version: apiVersion },
    schema: {
      version: positiveInteger(value.schema.version, "数据库 schema 版本"),
    },
  };
}

export async function getHealth(): Promise<HealthResponse> {
  return normalizeHealthResponse(await apiRequest<unknown>("/health"));
}

const appSettingKeys: AppSettingKey[] = [
  "workspace",
  "general",
  "appearance",
  "focus",
  "storage",
];
const controlledAvatarReference =
  /^avatars\/[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.(?:png|jpg|webp)$/;

function normalizeAppSettingValue(key: AppSettingKey, value: unknown) {
  if (!isRecord(value)) return invalidResponse(`设置 ${key} 响应格式无效`);
  switch (key) {
    case "workspace": {
      if (!hasExactKeys(value, ["display_name", "avatar_ref"])) {
        return invalidResponse("工作区设置响应字段无效");
      }
      const displayName = value.display_name;
      const avatarRef = value.avatar_ref;
      if (
        typeof displayName !== "string" ||
        Array.from(displayName).length < 1 ||
        Array.from(displayName).length > 32 ||
        (avatarRef !== null &&
          (typeof avatarRef !== "string" ||
            !controlledAvatarReference.test(avatarRef)))
      ) {
        return invalidResponse("工作区设置响应值无效");
      }
      return { displayName, avatarRef };
    }
    case "general": {
      if (
        !hasExactKeys(value, [
          "default_route",
          "show_right_overview",
          "reduce_motion",
        ]) ||
        (value.default_route !== "today" &&
          value.default_route !== "tasks" &&
          value.default_route !== "projects" &&
          value.default_route !== "clients" &&
          value.default_route !== "focus") ||
        typeof value.show_right_overview !== "boolean" ||
        typeof value.reduce_motion !== "boolean"
      ) {
        return invalidResponse("通用设置响应值无效");
      }
      return {
        defaultRoute: value.default_route,
        showRightOverview: value.show_right_overview,
        reduceMotion: value.reduce_motion,
      };
    }
    case "appearance":
      if (
        !hasExactKeys(value, ["theme"]) ||
        (value.theme !== "light" && value.theme !== "dark")
      ) {
        return invalidResponse("外观设置响应值无效");
      }
      return { theme: value.theme };
    case "focus": {
      if (
        !hasExactKeys(value, [
          "focus_minutes",
          "break_minutes",
          "cycles",
          "auto_start_break",
          "auto_start_focus",
          "sound_enabled",
        ]) ||
        typeof value.focus_minutes !== "number" ||
        !Number.isInteger(value.focus_minutes) ||
        value.focus_minutes < 5 ||
        value.focus_minutes > 120 ||
        value.focus_minutes % 5 !== 0 ||
        typeof value.break_minutes !== "number" ||
        !Number.isInteger(value.break_minutes) ||
        value.break_minutes < 5 ||
        value.break_minutes > 30 ||
        value.break_minutes % 5 !== 0 ||
        typeof value.cycles !== "number" ||
        !Number.isInteger(value.cycles) ||
        value.cycles < 1 ||
        value.cycles > 8 ||
        typeof value.auto_start_break !== "boolean" ||
        typeof value.auto_start_focus !== "boolean" ||
        typeof value.sound_enabled !== "boolean"
      ) {
        return invalidResponse("专注设置响应值无效");
      }
      return {
        focusMinutes: value.focus_minutes,
        breakMinutes: value.break_minutes,
        cycles: value.cycles,
        autoStartBreak: value.auto_start_break,
        autoStartFocus: value.auto_start_focus,
        soundEnabled: value.sound_enabled,
      };
    }
    case "storage": {
      if (
        !hasExactKeys(value, ["low_space_threshold_gib"]) ||
        typeof value.low_space_threshold_gib !== "number" ||
        !Number.isInteger(value.low_space_threshold_gib) ||
        value.low_space_threshold_gib < 1 ||
        value.low_space_threshold_gib > 100
      ) {
        return invalidResponse("存储设置响应值无效");
      }
      return { lowSpaceThresholdGiB: value.low_space_threshold_gib };
    }
  }
}

function normalizeAppSettingItem(
  value: unknown,
  expectedKey: AppSettingKey,
): AppSettingItem {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "key",
      "value",
      "schema_version",
      "version",
      "stored",
      "updated_by_actor_id",
      "updated_at",
    ]) ||
    value.key !== expectedKey ||
    value.schema_version !== 1 ||
    typeof value.stored !== "boolean"
  ) {
    return invalidResponse(`设置 ${expectedKey} 响应字段无效`);
  }
  const version = nonNegativeInteger(value.version, `设置 ${expectedKey} 版本`);
  const actorID = value.updated_by_actor_id;
  const updatedAt = value.updated_at;
  if (
    (value.stored &&
      (version < 1 ||
        typeof actorID !== "string" ||
        !actorID ||
        typeof updatedAt !== "string" ||
        !updatedAt ||
        Number.isNaN(Date.parse(updatedAt)))) ||
    (!value.stored && (version !== 0 || actorID !== null || updatedAt !== null))
  ) {
    return invalidResponse(`设置 ${expectedKey} 存储元数据无效`);
  }
  const base = {
    key: expectedKey,
    schemaVersion: 1 as const,
    version,
    stored: value.stored,
    updatedByActorId: value.stored ? (actorID as string) : null,
    updatedAt: value.stored ? (updatedAt as string) : null,
  };
  const normalizedValue = normalizeAppSettingValue(expectedKey, value.value);
  switch (expectedKey) {
    case "workspace":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as WorkspaceSettingValue,
      };
    case "general":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as GeneralSettingValue,
      };
    case "appearance":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as AppearanceSettingValue,
      };
    case "focus":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as FocusSettingValue,
      };
    case "storage":
      return {
        ...base,
        key: expectedKey,
        value: normalizedValue as StorageSettingValue,
      };
  }
}

export function normalizeAppSettingsResponse(
  value: unknown,
): AppSettingsResult {
  if (!isRecord(value) || !hasExactKeys(value, ["data"])) {
    return invalidResponse("设置响应格式无效");
  }
  const data = value.data;
  if (
    !isRecord(data) ||
    !hasExactKeys(data, ["schema_version", "items"]) ||
    data.schema_version !== 1 ||
    !Array.isArray(data.items) ||
    data.items.length !== appSettingKeys.length
  ) {
    return invalidResponse("设置响应格式无效");
  }
  const items = data.items;
  return {
    schemaVersion: 1,
    items: appSettingKeys.map((key, index) =>
      normalizeAppSettingItem(items[index], key),
    ),
  };
}

export function getAppSetting<K extends AppSettingKey>(
  settings: AppSettingsResult,
  key: K,
): Extract<AppSettingItem, { key: K }> {
  const item = settings.items.find(
    (candidate): candidate is Extract<AppSettingItem, { key: K }> =>
      candidate.key === key,
  );
  if (!item) return invalidResponse(`设置 ${key} 缺失`);
  return item;
}

export async function getAppSettings(): Promise<AppSettingsResult> {
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>("/api/v1/settings"),
  );
}

function serializeAppSettingUpdate(update: AppSettingUpdate) {
  const base = { key: update.key, expected_version: update.expectedVersion };
  switch (update.key) {
    case "workspace":
      return {
        ...base,
        value: {
          display_name: update.value.displayName,
          avatar_ref: update.value.avatarRef,
        },
      };
    case "general":
      return {
        ...base,
        value: {
          default_route: update.value.defaultRoute,
          show_right_overview: update.value.showRightOverview,
          reduce_motion: update.value.reduceMotion,
        },
      };
    case "appearance":
      return { ...base, value: { theme: update.value.theme } };
    case "focus":
      return {
        ...base,
        value: {
          focus_minutes: update.value.focusMinutes,
          break_minutes: update.value.breakMinutes,
          cycles: update.value.cycles,
          auto_start_break: update.value.autoStartBreak,
          auto_start_focus: update.value.autoStartFocus,
          sound_enabled: update.value.soundEnabled,
        },
      };
    case "storage":
      return {
        ...base,
        value: {
          low_space_threshold_gib: update.value.lowSpaceThresholdGiB,
        },
      };
  }
}

export async function updateAppSettings(
  updates: AppSettingUpdate[],
): Promise<AppSettingsResult> {
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>("/api/v1/settings", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        updates: updates.map(serializeAppSettingUpdate),
      }),
    }),
  );
}

export async function commitAppSettingsWithAvatar(
  operation: "replace" | "remove",
  updates: AppSettingUpdate[],
  file?: File,
): Promise<AppSettingsResult> {
  if (!updates.some((update) => update.key === "workspace")) {
    throw new ApiError("头像变更必须同时提交工作区设置", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  if (operation === "replace") {
    if (!file || file.size < 1) {
      throw new ApiError("请选择头像图片", {
        code: "VALIDATION_ERROR",
        status: 422,
      });
    }
    if (file.size > MAX_WORKSPACE_AVATAR_BYTES) {
      throw new ApiError("头像图片不能超过 2 MB", {
        code: "AVATAR_FILE_TOO_LARGE",
        status: 413,
      });
    }
    if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
      throw new ApiError("请选择 PNG、JPG 或 WebP 图片", {
        code: "VALIDATION_ERROR",
        status: 422,
      });
    }
  } else if (file) {
    throw new ApiError("移除头像时不能附带文件", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  const form = new FormData();
  form.append(
    "manifest",
    JSON.stringify({
      operation,
      updates: updates.map(serializeAppSettingUpdate),
    }),
  );
  if (file) form.append("file", file, file.name || "workspace-avatar");
  return normalizeAppSettingsResponse(
    await apiRequest<unknown>(
      "/api/v1/settings/avatar",
      { method: "POST", body: form },
      ARTIFACT_TRANSFER_TIMEOUT_MS,
    ),
  );
}

export async function getWorkspaceAvatarBlob(): Promise<Blob> {
  return apiFetch(
    "/api/v1/settings/avatar/content",
    async (response) => {
      const contentType = response.headers.get("Content-Type")?.split(";")[0];
      if (
        !contentType ||
        !["image/png", "image/jpeg", "image/webp"].includes(contentType)
      ) {
        return invalidResponse("头像响应格式无效");
      }
      const blob = await response.blob();
      if (blob.size < 1 || blob.size > MAX_WORKSPACE_AVATAR_BYTES) {
        return invalidResponse("头像响应大小无效");
      }
      return blob;
    },
    undefined,
    "image/png,image/jpeg,image/webp",
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
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

const searchResourceTypes = new Set<SearchResourceType>([
  "task",
  "project",
  "client",
  "inbox_item",
]);

const searchRoutePrefixes: Record<SearchResourceType, string> = {
  task: "/tasks/",
  project: "/projects/",
  client: "/clients/",
  inbox_item: "/inbox/",
};

const searchMatchedFields: Record<SearchResourceType, Set<string>> = {
  task: new Set(["title", "description"]),
  project: new Set(["name", "description"]),
  client: new Set(["name", "contact_name", "email", "phone"]),
  inbox_item: new Set(["title", "summary"]),
};

const searchStatuses: Record<SearchResourceType, Set<string>> = {
  task: new Set([
    "todo",
    "in_progress",
    "blocked",
    "waiting_review",
    "done",
    "cancelled",
  ]),
  project: new Set(["planning", "in_progress", "paused", "completed"]),
  client: new Set(["active", "lead", "inactive"]),
  inbox_item: new Set(["open", "tracking"]),
};

export function normalizeSearchListResult(value: unknown): SearchListResult {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["data", "meta"]) ||
    !Array.isArray(value.data) ||
    !isRecord(value.meta) ||
    !hasExactKeys(value.meta, ["page", "page_size", "total"])
  ) {
    return invalidResponse("统一搜索响应格式无效");
  }
  const items = value.data.map((entry): SearchResult => {
    if (
      !isRecord(entry) ||
      !hasExactKeys(entry, [
        "resource_type",
        "resource_id",
        "title",
        "subtitle",
        "matched_fields",
        "route",
        "status",
        "updated_at",
      ]) ||
      typeof entry.resource_type !== "string" ||
      !searchResourceTypes.has(entry.resource_type as SearchResourceType) ||
      typeof entry.resource_id !== "string" ||
      !entry.resource_id ||
      typeof entry.title !== "string" ||
      !entry.title ||
      typeof entry.subtitle !== "string" ||
      !Array.isArray(entry.matched_fields) ||
      !entry.matched_fields.every(
        (field): field is string =>
          typeof field === "string" && field.length > 0,
      ) ||
      typeof entry.route !== "string" ||
      typeof entry.status !== "string" ||
      !entry.status ||
      typeof entry.updated_at !== "string" ||
      !entry.updated_at
    ) {
      return invalidResponse("统一搜索结果字段无效");
    }
    const resourceType = entry.resource_type as SearchResourceType;
    if (
      entry.route !==
        `${searchRoutePrefixes[resourceType]}${entry.resource_id}` ||
      !searchStatuses[resourceType].has(entry.status) ||
      entry.matched_fields.length === 0 ||
      new Set(entry.matched_fields).size !== entry.matched_fields.length ||
      !entry.matched_fields.every((field) =>
        searchMatchedFields[resourceType].has(field),
      ) ||
      Number.isNaN(Date.parse(entry.updated_at))
    ) {
      return invalidResponse("统一搜索结果语义无效");
    }
    return {
      resourceType,
      resourceId: entry.resource_id,
      title: entry.title,
      subtitle: entry.subtitle,
      matchedFields: [...entry.matched_fields],
      route: entry.route,
      status: entry.status,
      updatedAt: entry.updated_at,
    };
  });
  const page = positiveInteger(value.meta.page, "统一搜索页码");
  const pageSize = positiveInteger(value.meta.page_size, "统一搜索每页数量");
  const total = nonNegativeInteger(value.meta.total, "统一搜索总数");
  if (items.length > pageSize || items.length > total) {
    return invalidResponse("统一搜索分页响应无效");
  }
  return {
    items,
    meta: { page, pageSize, total },
  };
}

export async function getSearchResults(
  input: SearchListParams,
): Promise<SearchListResult> {
  const params = new URLSearchParams({
    q: input.q.trim(),
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.types?.length) params.set("types", input.types.join(","));
  const payload = await apiRequest<unknown>(`/api/v1/search?${params}`);
  return normalizeSearchListResult(payload);
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
  if (options.clientId) params.set("client_id", options.clientId);
  for (const tagId of new Set(options.tagIds?.map((id) => id.trim()))) {
    if (tagId) params.append("tag_id", tagId);
  }
  if (options.plannedDate) params.set("planned_date", options.plannedDate);
  if (options.plannedFrom) params.set("planned_from", options.plannedFrom);
  if (options.plannedTo) params.set("planned_to", options.plannedTo);
  if (options.plannedState) params.set("planned_state", options.plannedState);
  if (options.dueFrom) params.set("due_from", options.dueFrom);
  if (options.dueTo) params.set("due_to", options.dueTo);
  if (options.dueState) params.set("due_state", options.dueState);
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
  const eventVersion = event.current?.version;
  if (
    task.id !== id ||
    event.action !== taskLifecycleEventActions[input.action] ||
    (typeof eventVersion === "number" &&
      (eventVersion <= input.expectedVersion || eventVersion > task.version))
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
  if (input.action === "block" || input.action === "cancel") {
    body.reason = input.reason;
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
    action !== "remove_tags" &&
    action !== "start" &&
    action !== "block" &&
    action !== "unblock" &&
    action !== "complete" &&
    action !== "cancel" &&
    action !== "reopen"
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

function normalizeTaskSavedViewDefinition(
  value: unknown,
): TaskSavedViewDefinition {
  if (!isRecord(value) || !Array.isArray(value.tag_ids ?? value.tagIds)) {
    return invalidResponse("任务保存视图定义格式无效");
  }
  const readString = (snake: string, camel?: string): string => {
    const field = camel
      ? fieldValue(value, snake, camel)
      : fieldValue(value, snake);
    if (typeof field !== "string") {
      return invalidResponse("任务保存视图定义字段无效");
    }
    return field;
  };
  const status = readString("status");
  const priority = readString("priority");
  const kind = readString("kind");
  if (
    ![
      "",
      "active",
      "todo",
      "in_progress",
      "blocked",
      "waiting_review",
      "done",
      "cancelled",
    ].includes(status) ||
    !["", "P0", "P1", "P2", "P3"].includes(priority) ||
    !["", "work", "review", "followup", "reminder"].includes(kind)
  ) {
    return invalidResponse("任务保存视图枚举值无效");
  }
  const rawTags = (value.tag_ids ?? value.tagIds) as unknown[];
  if (rawTags.some((tag) => typeof tag !== "string")) {
    return invalidResponse("任务保存视图标签格式无效");
  }
  return {
    q: readString("q"),
    status: status as TaskSavedViewDefinition["status"],
    priority: priority as TaskSavedViewDefinition["priority"],
    kind: kind as TaskSavedViewDefinition["kind"],
    projectId: readString("project_id", "projectId"),
    clientId: readString("client_id", "clientId"),
    tagIds: rawTags as string[],
    plannedDate: readString("planned_date", "plannedDate"),
    plannedFrom: readString("planned_from", "plannedFrom"),
    plannedTo: readString("planned_to", "plannedTo"),
    dueFrom: readString("due_from", "dueFrom"),
    dueTo: readString("due_to", "dueTo"),
    sort: readString("sort"),
  };
}

export function normalizeTaskSavedView(value: unknown): TaskSavedView {
  if (!isRecord(value)) {
    return invalidResponse("任务保存视图响应格式无效");
  }
  const id = stringField(value, "id");
  const name = stringField(value, "name");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const schemaVersion = positiveInteger(
    fieldValue(value, "schema_version", "schemaVersion"),
    "任务保存视图结构版本",
  );
  if (!id || !name || !createdAt || !updatedAt || schemaVersion !== 1) {
    return invalidResponse("任务保存视图响应格式无效");
  }
  return {
    id,
    name,
    definition: normalizeTaskSavedViewDefinition(value.definition),
    schemaVersion,
    version: positiveInteger(value.version, "任务保存视图版本"),
    createdAt,
    updatedAt,
  };
}

function taskSavedViewDefinitionBody(definition: TaskSavedViewDefinition) {
  return {
    q: definition.q,
    status: definition.status,
    priority: definition.priority,
    kind: definition.kind,
    project_id: definition.projectId,
    client_id: definition.clientId,
    tag_ids: definition.tagIds,
    planned_date: definition.plannedDate,
    planned_from: definition.plannedFrom,
    planned_to: definition.plannedTo,
    due_from: definition.dueFrom,
    due_to: definition.dueTo,
    sort: definition.sort,
  };
}

export async function getTaskSavedViews(): Promise<TaskSavedView[]> {
  const payload = await apiRequest<unknown>("/api/v1/task-saved-views");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("任务保存视图列表响应格式无效");
  }
  return payload.data.map(normalizeTaskSavedView);
}

export async function createTaskSavedView(
  input: CreateTaskSavedViewInput,
): Promise<TaskSavedView> {
  const payload = await apiRequest<unknown>("/api/v1/task-saved-views", {
    method: "POST",
    body: JSON.stringify({
      name: input.name,
      definition: taskSavedViewDefinitionBody(input.definition),
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTaskSavedView(body);
}

export async function updateTaskSavedView(
  id: string,
  input: UpdateTaskSavedViewInput,
): Promise<TaskSavedView> {
  const payload = await apiRequest<unknown>(
    `/api/v1/task-saved-views/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.definition === undefined
          ? {}
          : { definition: taskSavedViewDefinitionBody(input.definition) }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTaskSavedView(body);
}

export async function deleteTaskSavedView(
  id: string,
  expectedVersion: number,
): Promise<DeleteTaskSavedViewResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/task-saved-views/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE", headers: expectedVersionHeader(expectedVersion) },
  );
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) {
    return invalidResponse("任务保存视图删除响应格式无效");
  }
  return { deletedId: String(body.deleted_id ?? body.deletedId ?? id) };
}

export function normalizeBackupSummary(value: unknown): BackupSummary {
  if (!isRecord(value)) {
    return invalidResponse("本地备份响应格式无效");
  }
  const id = stringField(value, "id");
  const createdAt = stringField(value, "created_at", "createdAt") ?? "";
  const verifiedAt = fieldValue(value, "verified_at", "verifiedAt");
  const verificationStatus = fieldValue(
    value,
    "verification_status",
    "verificationStatus",
  );
  const note = fieldValue(value, "note");
  const error = fieldValue(value, "error");
  const appVersion = fieldValue(value, "app_version", "appVersion");
  const apiVersion = fieldValue(value, "api_version", "apiVersion");
  if (
    !id ||
    (createdAt === "" && verificationStatus !== "invalid") ||
    !["verified", "unverified", "invalid"].includes(
      String(verificationStatus),
    ) ||
    (verifiedAt !== undefined && typeof verifiedAt !== "string") ||
    (note !== undefined && typeof note !== "string") ||
    (error !== undefined && typeof error !== "string") ||
    (appVersion !== undefined && typeof appVersion !== "string") ||
    (apiVersion !== undefined && typeof apiVersion !== "string")
  ) {
    return invalidResponse("本地备份响应格式无效");
  }
  const invalid = verificationStatus === "invalid";
  const readCount = (snake: string, camel: string, label: string) =>
    nonNegativeInteger(
      fieldValue(value, snake, camel),
      label,
      invalid ? 0 : undefined,
    );
  return {
    id,
    createdAt,
    verifiedAt:
      typeof verifiedAt === "string" && verifiedAt ? verifiedAt : null,
    verificationStatus: verificationStatus as BackupVerificationStatus,
    note: typeof note === "string" ? note : "",
    appVersion: typeof appVersion === "string" ? appVersion : "",
    apiVersion: typeof apiVersion === "string" ? apiVersion : "",
    schemaVersion: readCount(
      "schema_version",
      "schemaVersion",
      "备份数据库版本",
    ),
    artifactCount: readCount("artifact_count", "artifactCount", "备份文件数量"),
    artifactBytes: readCount("artifact_bytes", "artifactBytes", "备份文件大小"),
    databaseBytes: readCount(
      "database_bytes",
      "databaseBytes",
      "备份数据库大小",
    ),
    totalBytes: readCount("total_bytes", "totalBytes", "备份总大小"),
    error: typeof error === "string" && error ? error : null,
  };
}

export async function getBackups(): Promise<BackupSummary[]> {
  const payload = await apiRequest<unknown>("/api/v1/backups");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("本地备份列表响应格式无效");
  }
  return payload.data.map(normalizeBackupSummary);
}

export async function createBackup(
  input: CreateBackupInput,
  idempotencyKey: string,
): Promise<BackupSummary> {
  const payload = await apiRequest<unknown>(
    "/api/v1/backups",
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({ note: input.note }),
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeBackupSummary(body);
}

export async function verifyBackup(id: string): Promise<BackupSummary> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/verify`,
    { method: "POST", body: "{}" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeBackupSummary(body);
}

export async function drillBackupRestore(
  id: string,
): Promise<BackupRestoreDrillResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/drill`,
    { method: "POST", body: "{}" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) {
    return invalidResponse("备份恢复演练响应格式无效");
  }
  const backupId = stringField(body, "backup_id", "backupId");
  const drilledAt = stringField(body, "drilled_at", "drilledAt");
  const sourceSchemaVersion = positiveInteger(
    fieldValue(body, "source_schema_version", "sourceSchemaVersion"),
    "恢复演练来源数据库版本",
  );
  const resultSchemaVersion = positiveInteger(
    fieldValue(body, "result_schema_version", "resultSchemaVersion"),
    "恢复演练结果数据库版本",
  );
  const artifactCount = nonNegativeInteger(
    fieldValue(body, "artifact_count", "artifactCount"),
    "恢复演练文件数量",
  );
  const temporaryDataCleaned = fieldValue(
    body,
    "temporary_data_cleaned",
    "temporaryDataCleaned",
  );
  if (
    !backupId ||
    !drilledAt ||
    Number.isNaN(Date.parse(drilledAt)) ||
    temporaryDataCleaned !== true
  ) {
    return invalidResponse("备份恢复演练响应格式无效");
  }
  return {
    backupId,
    drilledAt,
    sourceSchemaVersion,
    resultSchemaVersion,
    artifactCount,
    temporaryDataCleaned,
  };
}

export async function scheduleBackupRestore(
  id: string,
): Promise<ScheduledBackupRestoreResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/restore`,
    { method: "POST", body: JSON.stringify({ confirm: true }) },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) {
    return invalidResponse("备份恢复安排响应格式无效");
  }
  const backupId = stringField(body, "backup_id", "backupId");
  const rollbackBackupId = stringField(
    body,
    "rollback_backup_id",
    "rollbackBackupId",
  );
  const requestedAt = stringField(body, "requested_at", "requestedAt");
  const restartRequired = fieldValue(
    body,
    "restart_required",
    "restartRequired",
  );
  if (
    !backupId ||
    !rollbackBackupId ||
    backupId === rollbackBackupId ||
    !requestedAt ||
    Number.isNaN(Date.parse(requestedAt)) ||
    restartRequired !== true
  ) {
    return invalidResponse("备份恢复安排响应格式无效");
  }
  return { backupId, rollbackBackupId, requestedAt, restartRequired };
}

export async function deleteBackup(id: string): Promise<void> {
  await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function downloadBusinessDataExport(): Promise<BusinessDataExportDownload> {
  return apiFetch(
    "/api/v1/exports/business-data",
    async (response) => {
      if (
        response.headers.get("X-Export-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/json")
      ) {
        return invalidResponse("业务数据导出响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-business-export.json",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/json",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function downloadBusinessPackage(): Promise<BusinessPackageDownload> {
  return apiFetch(
    "/api/v1/exports/business-package",
    async (response) => {
      if (
        response.headers.get("X-Business-Package-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/zip")
      ) {
        return invalidResponse("含文件业务导出包响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-business-files.zip",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/zip",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

function normalizeBusinessPackageImportPreview(
  value: unknown,
): BusinessPackageImportPreview {
  const body = isRecord(value) && "data" in value ? value.data : value;
  if (!isRecord(body)) return invalidResponse("含文件业务包预检响应格式无效");
  const formatVersion = numberField(body, "format_version", "formatVersion");
  const schemaVersion = numberField(body, "schema_version", "schemaVersion");
  const exportedAt = stringField(body, "exported_at", "exportedAt");
  const totalRows = numberField(body, "total_rows", "totalRows");
  const fileCount = numberField(body, "file_count", "fileCount");
  const fileBytes = numberField(body, "file_bytes", "fileBytes");
  const canApply = fieldValue(body, "can_apply", "canApply");
  const blocker = fieldValue(body, "blocker");
  const rawCounts = fieldValue(body, "table_counts", "tableCounts");
  if (
    formatVersion !== 1 ||
    !schemaVersion ||
    !Number.isInteger(schemaVersion) ||
    !exportedAt ||
    Number.isNaN(Date.parse(exportedAt)) ||
    totalRows === undefined ||
    !Number.isInteger(totalRows) ||
    totalRows < 0 ||
    fileCount === undefined ||
    !Number.isInteger(fileCount) ||
    fileCount < 0 ||
    fileBytes === undefined ||
    !Number.isSafeInteger(fileBytes) ||
    fileBytes < 0 ||
    typeof canApply !== "boolean" ||
    !isRecord(rawCounts) ||
    (blocker !== undefined && blocker !== "target_not_empty") ||
    (canApply && blocker !== undefined) ||
    (!canApply && blocker !== "target_not_empty")
  ) {
    return invalidResponse("含文件业务包预检响应格式无效");
  }
  const tableCounts: Record<string, number> = {};
  for (const [key, count] of Object.entries(rawCounts)) {
    if (!key || !Number.isInteger(count) || (count as number) < 0) {
      return invalidResponse("含文件业务包预检响应格式无效");
    }
    tableCounts[key] = count as number;
  }
  if (
    Object.values(tableCounts).reduce((sum, count) => sum + count, 0) !==
    totalRows
  ) {
    return invalidResponse("含文件业务包预检响应格式无效");
  }
  return {
    formatVersion: 1,
    schemaVersion,
    exportedAt,
    tableCounts,
    totalRows,
    fileCount,
    fileBytes,
    canApply,
    blocker: blocker === "target_not_empty" ? blocker : null,
  };
}

export async function previewBusinessPackageImport(
  file: File,
): Promise<BusinessPackageImportPreview> {
  return apiRequest<unknown>(
    "/api/v1/imports/business-package/preview",
    { method: "POST", body: file },
    BACKUP_OPERATION_TIMEOUT_MS,
  ).then(normalizeBusinessPackageImportPreview);
}

export async function applyBusinessPackageImport(
  file: File,
): Promise<BusinessPackageImportResult> {
  const payload = await apiRequest<unknown>(
    "/api/v1/imports/business-package",
    {
      method: "POST",
      body: file,
      headers: {
        "X-Import-Confirmation":
          "replace-empty-workspace-with-controlled-files",
      },
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("含文件业务包导入响应格式无效");
  const importedRows = numberField(body, "imported_rows", "importedRows");
  const importedFiles = numberField(body, "imported_files", "importedFiles");
  const backupId = stringField(body, "backup_id", "backupId");
  if (
    importedRows === undefined ||
    !Number.isInteger(importedRows) ||
    importedRows < 0 ||
    importedFiles === undefined ||
    !Number.isInteger(importedFiles) ||
    importedFiles < 0 ||
    !backupId
  ) {
    return invalidResponse("含文件业务包导入响应格式无效");
  }
  return { importedRows, importedFiles, backupId };
}

export async function getRestoreDiagnostics(): Promise<RestoreDiagnostics> {
  const payload = await apiRequest<unknown>(
    "/api/v1/backups/restore-diagnostics",
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("恢复诊断响应格式无效");
  const status = stringField(body, "status");
  const restartRequired = fieldValue(
    body,
    "restart_required",
    "restartRequired",
  );
  const appliedThisStartup = fieldValue(
    body,
    "applied_this_startup",
    "appliedThisStartup",
  );
  const cleanupRequired = fieldValue(
    body,
    "cleanup_required",
    "cleanupRequired",
  );
  const attentionRequired = fieldValue(
    body,
    "attention_required",
    "attentionRequired",
  );
  const backupId = fieldValue(body, "backup_id", "backupId");
  const rollbackBackupId = fieldValue(
    body,
    "rollback_backup_id",
    "rollbackBackupId",
  );
  const requestedAt = fieldValue(body, "requested_at", "requestedAt");
  const residualAppliedCount = numberField(
    body,
    "residual_applied_count",
    "residualAppliedCount",
  );
  const failedAttemptCount = numberField(
    body,
    "failed_attempt_count",
    "failedAttemptCount",
  );
  const invalidEntryCount = numberField(
    body,
    "invalid_entry_count",
    "invalidEntryCount",
  );
  const statuses = new Set([
    "idle",
    "restart_required",
    "restored",
    "cleanup_required",
    "attention_required",
  ]);
  const canonicalId = (value: unknown) =>
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    );
  if (
    !status ||
    !statuses.has(status) ||
    typeof restartRequired !== "boolean" ||
    typeof appliedThisStartup !== "boolean" ||
    typeof cleanupRequired !== "boolean" ||
    typeof attentionRequired !== "boolean" ||
    (backupId !== null && !canonicalId(backupId)) ||
    (rollbackBackupId !== null && !canonicalId(rollbackBackupId)) ||
    (backupId === null) !== (rollbackBackupId === null) ||
    (requestedAt !== null &&
      (typeof requestedAt !== "string" ||
        Number.isNaN(Date.parse(requestedAt)))) ||
    residualAppliedCount === undefined ||
    !Number.isInteger(residualAppliedCount) ||
    residualAppliedCount < 0 ||
    failedAttemptCount === undefined ||
    !Number.isInteger(failedAttemptCount) ||
    failedAttemptCount < 0 ||
    invalidEntryCount === undefined ||
    !Number.isInteger(invalidEntryCount) ||
    invalidEntryCount < 0 ||
    (status === "restart_required" && !restartRequired) ||
    (status === "cleanup_required" && !cleanupRequired) ||
    (status === "attention_required" && !attentionRequired) ||
    (status === "restored" && !appliedThisStartup) ||
    (status === "idle" &&
      (restartRequired ||
        appliedThisStartup ||
        cleanupRequired ||
        attentionRequired))
  ) {
    return invalidResponse("恢复诊断响应格式无效");
  }
  return {
    status: status as RestoreDiagnostics["status"],
    restartRequired,
    appliedThisStartup,
    cleanupRequired,
    attentionRequired,
    backupId: typeof backupId === "string" && backupId ? backupId : null,
    rollbackBackupId:
      typeof rollbackBackupId === "string" && rollbackBackupId
        ? rollbackBackupId
        : null,
    requestedAt: typeof requestedAt === "string" ? requestedAt : null,
    residualAppliedCount,
    failedAttemptCount,
    invalidEntryCount,
  };
}

function normalizeBusinessImportPreview(value: unknown): BusinessImportPreview {
  const body = isRecord(value) && "data" in value ? value.data : value;
  if (!isRecord(body)) return invalidResponse("业务数据导入预检响应格式无效");
  const formatVersion = numberField(body, "format_version", "formatVersion");
  const schemaVersion = numberField(body, "schema_version", "schemaVersion");
  const exportedAt = stringField(body, "exported_at", "exportedAt");
  const totalRows = numberField(body, "total_rows", "totalRows");
  const canApply = fieldValue(body, "can_apply", "canApply");
  const blocker = fieldValue(body, "blocker");
  const rawCounts = fieldValue(body, "table_counts", "tableCounts");
  if (
    formatVersion !== 1 ||
    !schemaVersion ||
    !Number.isInteger(schemaVersion) ||
    !exportedAt ||
    Number.isNaN(Date.parse(exportedAt)) ||
    totalRows === undefined ||
    !Number.isInteger(totalRows) ||
    totalRows < 0 ||
    typeof canApply !== "boolean" ||
    !isRecord(rawCounts) ||
    (blocker !== undefined && blocker !== "target_not_empty") ||
    (canApply && blocker !== undefined) ||
    (!canApply && blocker !== "target_not_empty")
  ) {
    return invalidResponse("业务数据导入预检响应格式无效");
  }
  const tableCounts: Record<string, number> = {};
  for (const [key, count] of Object.entries(rawCounts)) {
    if (!key || !Number.isInteger(count) || (count as number) < 0) {
      return invalidResponse("业务数据导入预检响应格式无效");
    }
    tableCounts[key] = count as number;
  }
  if (
    Object.values(tableCounts).reduce((sum, count) => sum + count, 0) !==
    totalRows
  ) {
    return invalidResponse("业务数据导入预检响应格式无效");
  }
  return {
    formatVersion: 1,
    schemaVersion,
    exportedAt,
    tableCounts,
    totalRows,
    canApply,
    blocker: blocker === "target_not_empty" ? blocker : null,
  };
}

export async function previewBusinessDataImport(
  file: File,
): Promise<BusinessImportPreview> {
  return apiRequest<unknown>(
    "/api/v1/imports/business-data/preview",
    { method: "POST", body: file },
    BACKUP_OPERATION_TIMEOUT_MS,
  ).then(normalizeBusinessImportPreview);
}

export async function applyBusinessDataImport(
  file: File,
): Promise<BusinessImportResult> {
  const payload = await apiRequest<unknown>(
    "/api/v1/imports/business-data",
    {
      method: "POST",
      body: file,
      headers: { "X-Import-Confirmation": "replace-empty-workspace" },
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("业务数据导入响应格式无效");
  const importedRows = numberField(body, "imported_rows", "importedRows");
  const backupId = stringField(body, "backup_id", "backupId");
  if (
    importedRows === undefined ||
    !Number.isInteger(importedRows) ||
    importedRows < 0 ||
    !backupId
  ) {
    return invalidResponse("业务数据导入响应格式无效");
  }
  return { importedRows, backupId };
}

export async function downloadDiagnosticPackage(): Promise<DiagnosticPackageDownload> {
  return apiFetch(
    "/api/v1/diagnostics/package",
    async (response) => {
      if (
        response.headers.get("X-Diagnostic-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/zip")
      ) {
        return invalidResponse("诊断包响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-diagnostics.zip",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/zip",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function getStorageCapacity(): Promise<StorageCapacityResult> {
  const payload = await apiRequest<unknown>("/api/v1/diagnostics/storage");
  if (
    !isRecord(payload) ||
    !hasExactKeys(payload, ["data"]) ||
    !isRecord(payload.data)
  ) {
    return invalidResponse("存储容量响应格式无效");
  }
  const data = payload.data;
  if (
    !hasExactKeys(data, ["checked_at", "threshold_gib", "locations"]) ||
    typeof data.checked_at !== "string" ||
    Number.isNaN(Date.parse(data.checked_at)) ||
    !Array.isArray(data.locations) ||
    data.locations.length !== 3
  ) {
    return invalidResponse("存储容量响应格式无效");
  }
  const kinds = ["database", "artifacts", "backups"] as const;
  const locations = data.locations.map((location, index) => {
    if (
      !isRecord(location) ||
      !hasExactKeys(location, [
        "kind",
        "status",
        "available_bytes",
        "total_bytes",
        "shared_volume",
      ]) ||
      location.kind !== kinds[index] ||
      typeof location.shared_volume !== "boolean" ||
      (location.status !== "healthy" &&
        location.status !== "low" &&
        location.status !== "unavailable")
    ) {
      return invalidResponse("存储容量位置响应格式无效");
    }
    const unavailable = location.status === "unavailable";
    if (
      (unavailable &&
        (location.available_bytes !== null || location.total_bytes !== null)) ||
      (!unavailable &&
        (typeof location.available_bytes !== "number" ||
          typeof location.total_bytes !== "number" ||
          !Number.isSafeInteger(location.available_bytes) ||
          !Number.isSafeInteger(location.total_bytes) ||
          location.available_bytes < 0 ||
          location.total_bytes < 1 ||
          location.available_bytes > location.total_bytes))
    ) {
      return invalidResponse("存储容量位置响应值无效");
    }
    const status = location.status as StorageCapacityStatus;
    return {
      kind: kinds[index],
      status,
      availableBytes: unavailable ? null : (location.available_bytes as number),
      totalBytes: unavailable ? null : (location.total_bytes as number),
      sharedVolume: location.shared_volume,
    };
  });
  return {
    checkedAt: data.checked_at,
    thresholdGiB: positiveInteger(data.threshold_gib, "低空间阈值"),
    locations,
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
  signal?: AbortSignal,
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
  const payload = await apiRequest<unknown>(`/api/v1/projects?${params}`, {
    signal,
  });
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

export async function getRoadmapMilestones(
  input: RoadmapMilestoneListParams = {},
  signal?: AbortSignal,
): Promise<RoadmapMilestoneListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.year) params.set("year", String(input.year));
  if (input.quarter) params.set("quarter", String(input.quarter));
  if (input.status) params.set("status", input.status);
  if (input.projectId) params.set("project_id", input.projectId);
  if (input.includeArchived) params.set("include_archived", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones?${params}`,
    { signal },
  );
  return normalizeRoadmapMilestoneListResult(payload, input);
}

export async function getContentItems(
  input: ContentItemListParams = {},
  signal?: AbortSignal,
): Promise<ContentItemListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.scheduledFrom) params.set("scheduled_from", input.scheduledFrom);
  if (input.scheduledTo) params.set("scheduled_to", input.scheduledTo);
  if (input.platform?.trim()) params.set("platform", input.platform.trim());
  if (input.status) params.set("status", input.status);
  if (input.projectId) params.set("project_id", input.projectId);
  if (input.includeArchived) params.set("include_archived", "true");
  return normalizeContentItemListResult(
    await apiRequest<unknown>(`/api/v1/content-items?${params}`, { signal }),
    input,
  );
}

export async function createContentItem(
  input: CreateContentItemInput,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>("/api/v1/content-items", {
    method: "POST",
    body: JSON.stringify({
      title: input.title,
      platform: input.platform,
      status: input.status,
      scheduled_at: input.scheduledAt,
      scheduled_timezone: input.scheduledTimezone,
      project_id: input.projectId,
      notes: input.notes,
      external_link: input.externalLink,
    }),
  });
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function updateContentItem(
  id: string,
  input: UpdateContentItemInput,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.platform === undefined ? {} : { platform: input.platform }),
        ...(input.status === undefined ? {} : { status: input.status }),
        ...(input.projectId === undefined
          ? {}
          : { project_id: input.projectId }),
        ...(input.notes === undefined ? {} : { notes: input.notes }),
        ...(input.externalLink === undefined
          ? {}
          : { external_link: input.externalLink }),
      }),
    },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function scheduleContentItem(
  id: string,
  input: ScheduleContentItemInput,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}/schedule`,
    {
      method: "PUT",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        scheduled_at: input.scheduledAt,
        scheduled_timezone: input.scheduledTimezone,
      }),
    },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function publishContentItem(
  id: string,
  input: PublishContentItemInput,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}/publish-confirmation`,
    {
      method: "POST",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        published_at: input.publishedAt,
        external_link: input.externalLink,
      }),
    },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function linkContentItemTask(
  id: string,
  taskId: string,
  isRequired: boolean,
  expectedVersion: number,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}/tasks`,
    {
      method: "POST",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ task_id: taskId, is_required: isRequired }),
    },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function unlinkContentItemTask(
  id: string,
  taskId: string,
  expectedVersion: number,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}/tasks/${encodeURIComponent(taskId)}`,
    { method: "DELETE", headers: expectedVersionHeader(expectedVersion) },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function createRoadmapMilestone(
  input: CreateRoadmapMilestoneInput,
): Promise<RoadmapMilestone> {
  const payload = await apiRequest<unknown>("/api/v1/roadmap/milestones", {
    method: "POST",
    body: JSON.stringify({
      title: input.title,
      description: input.description,
      year: input.year,
      quarter: input.quarter,
      target_date: input.targetDate,
      status: input.status,
      project_ids: input.projectIds,
    }),
  });
  return normalizeRoadmapMilestone(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function archiveRoadmapMilestone(
  id: string,
  expectedVersion: number,
): Promise<RoadmapMilestone> {
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones/${encodeURIComponent(id)}/archive`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  return normalizeRoadmapMilestone(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function restoreRoadmapMilestone(
  id: string,
  expectedVersion: number,
): Promise<RoadmapMilestone> {
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones/${encodeURIComponent(id)}/restore`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  return normalizeRoadmapMilestone(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function getProject(
  id: string,
  signal?: AbortSignal,
): Promise<Project> {
  const payload = await apiRequest<unknown>(
    `/api/v1/projects/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeProject(body);
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

export async function getProjectNote(id: string): Promise<ProjectNote> {
  const payload = await apiRequest<unknown>(
    `/api/v1/project-notes/${encodeURIComponent(id)}`,
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
  signal?: AbortSignal,
): Promise<ClientListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.status) params.set("status", input.status);
  if (input.sort?.trim()) params.set("sort", input.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/clients?${params}`, {
    signal,
  });
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

export async function getClient(
  id: string,
  signal?: AbortSignal,
): Promise<Client> {
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(id)}`,
    { signal },
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

export async function getClientActivity(id: string): Promise<ClientActivity> {
  const payload = await apiRequest<unknown>(
    `/api/v1/client-activities/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeClientActivity(body);
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
): Promise<ClientActorLinkListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.includeUnlinked) params.set("include_unlinked", "true");
  const payload = await apiRequest<unknown>(
    `/api/v1/clients/${encodeURIComponent(clientId)}/actor-links?${params}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("客户责任关联列表响应格式无效");
  }
  const items = payload.data.map(normalizeClientActorLink);
  if (items.some((link) => link.clientId !== clientId)) {
    return invalidResponse("客户责任关联列表与请求不一致");
  }
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "客户责任关联页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "客户责任关联每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "客户责任关联总数"),
      clientVersion: positiveInteger(
        fieldValue(payload.meta, "client_version", "clientVersion"),
        "客户责任关联对应客户版本",
      ),
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

export async function getInboxItems(
  input: InboxItemListParams = {},
): Promise<InboxItemListResult> {
  const params = new URLSearchParams({
    view: input.view ?? "inbox",
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.priority) params.set("priority", input.priority);
  if (input.risk) params.set("risk", input.risk);
  if (input.sourceEntityType) {
    params.set("source_entity_type", input.sourceEntityType);
  }
  const payload = await apiRequest<unknown>(`/api/v1/inbox-items?${params}`);
  return normalizeInboxItemListResult(payload);
}

export async function getInboxItem(id: string): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function getReminders(
  input: ReminderListParams = {},
): Promise<ReminderListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.status) params.set("status", input.status);
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.sort) params.set("sort", input.sort);
  const payload = await apiRequest<unknown>(`/api/v1/reminders?${params}`);
  return normalizeReminderListResult(payload);
}

export async function getReminder(id: string): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function createReminder(
  input: CreateReminderInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Reminder> {
  const payload = await apiRequest<unknown>("/api/v1/reminders", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      title: input.title,
      summary: input.summary,
      priority: input.priority,
      trigger_at: input.triggerAt,
      recurrence_type: input.recurrenceType,
      recurrence_interval: input.recurrenceInterval,
      recurrence_timezone: input.recurrenceTimezone,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function updateReminder(
  id: string,
  input: UpdateReminderInput,
): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.summary === undefined ? {} : { summary: input.summary }),
        ...(input.priority === undefined ? {} : { priority: input.priority }),
        ...(input.triggerAt === undefined
          ? {}
          : { trigger_at: input.triggerAt }),
        ...(input.recurrenceType === undefined
          ? {}
          : { recurrence_type: input.recurrenceType }),
        ...(input.recurrenceInterval === undefined
          ? {}
          : { recurrence_interval: input.recurrenceInterval }),
        ...(input.recurrenceTimezone === undefined
          ? {}
          : { recurrence_timezone: input.recurrenceTimezone }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function cancelReminder(
  input: CancelReminderInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(input.id)}`,
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
  return normalizeReminder(body);
}

function automationConfigPayload(
  config: AutomationConfig,
): Record<string, string> {
  if (config.priority) return { priority: config.priority };
  return {
    local_time: config.localTime ?? "",
    timezone: config.timezone ?? "",
  };
}

export async function getAutomationRules(): Promise<AutomationRule[]> {
  const payload = await apiRequest<unknown>("/api/v1/automations/rules");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("自动化规则列表响应格式无效");
  }
  return payload.data.map(normalizeAutomationRule);
}

export async function getAutomationRule(id: string): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

export async function previewAutomationRule(
  id: string,
  config: AutomationConfig,
): Promise<AutomationPreview> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}/preview`,
    {
      method: "POST",
      body: JSON.stringify({ config: automationConfigPayload(config) }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationPreview(body);
}

export async function updateAutomationRule(
  id: string,
  config: AutomationConfig,
  expectedVersion: number,
): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ config: automationConfigPayload(config) }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

async function changeAutomationRuleEnabled(
  id: string,
  action: "enable" | "disable",
  expectedVersion: number,
): Promise<AutomationRule> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/rules/${encodeURIComponent(id)}/${action}`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRule(body);
}

export function enableAutomationRule(
  id: string,
  expectedVersion: number,
): Promise<AutomationRule> {
  return changeAutomationRuleEnabled(id, "enable", expectedVersion);
}

export function disableAutomationRule(
  id: string,
  expectedVersion: number,
): Promise<AutomationRule> {
  return changeAutomationRuleEnabled(id, "disable", expectedVersion);
}

export async function getAutomationRuns(
  input: AutomationRunListParams = {},
): Promise<AutomationRunListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.ruleId) params.set("rule_id", input.ruleId);
  if (input.status) params.set("status", input.status);
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs?${params}`,
  );
  return normalizeAutomationRunListResult(payload);
}

export async function getAutomationRun(id: string): Promise<AutomationRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRun(body);
}

export async function retryAutomationRun(id: string): Promise<AutomationRun> {
  const payload = await apiRequest<unknown>(
    `/api/v1/automations/runs/${encodeURIComponent(id)}/retry`,
    { method: "POST" },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAutomationRun(body);
}

export async function getAgentAdapters(): Promise<AgentAdapter[]> {
  const payload = await apiRequest<unknown>("/api/v1/agent-adapters");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("本地 Agent 适配器列表响应格式无效");
  }
  return payload.data.map(normalizeAgentAdapter);
}

export async function registerAgentAdapter(
  presetKey = "builtin-local-text-v1",
  idempotencyKey: string = crypto.randomUUID(),
): Promise<AgentAdapter> {
  const payload = await apiRequest<unknown>("/api/v1/agent-adapters", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ preset_key: presetKey }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeAgentAdapter(body);
}

async function changeAgentAdapter(
  id: string,
  action: "check" | "enable" | "disable",
  expectedVersion: number,
): Promise<AgentAdapter> {
  const payload = await apiRequest<unknown>(
    `/api/v1/agent-adapters/${encodeURIComponent(id)}/${action}`,
    { method: "POST", headers: expectedVersionHeader(expectedVersion) },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const adapter = normalizeAgentAdapter(body);
  if (adapter.id !== id) {
    return invalidResponse("本地 Agent 适配器操作响应与请求不一致");
  }
  return adapter;
}

export function checkAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "check", expectedVersion);
}

export function enableAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "enable", expectedVersion);
}

export function disableAgentAdapter(
  id: string,
  expectedVersion: number,
): Promise<AgentAdapter> {
  return changeAgentAdapter(id, "disable", expectedVersion);
}

export async function getInboxItemTasks(
  id: string,
  input: InboxItemTaskListParams = {},
): Promise<InboxItemTaskListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}/tasks?${params}`,
  );
  const result = normalizeInboxItemTaskListResult(payload);
  if (
    result.active.some((relation) => relation.inboxItemId !== id) ||
    result.history.some((relation) => relation.inboxItemId !== id)
  ) {
    return invalidResponse("收件箱任务关系列表与请求不一致");
  }
  return result;
}

function assertInboxTaskMutationMatches(
  result: InboxItemTaskMutationResult,
  inboxItemId: string,
  taskId: string,
): InboxItemTaskMutationResult {
  if (
    result.inboxItem.id !== inboxItemId ||
    result.relation.inboxItemId !== inboxItemId ||
    result.relation.taskRefId !== taskId
  ) {
    return invalidResponse("收件箱任务关系操作响应与请求不一致");
  }
  return result;
}

export async function linkInboxItemTask(
  input: LinkInboxItemTaskInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItemTaskMutationResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.inboxItemId)}/tasks/${encodeURIComponent(input.taskId)}`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ is_required: input.isRequired }),
    },
  );
  const result = assertInboxTaskMutationMatches(
    normalizeInboxItemTaskMutationResult(payload),
    input.inboxItemId,
    input.taskId,
  );
  if (
    !result.relation.isActive ||
    result.relation.isRequired !== input.isRequired
  ) {
    return invalidResponse("收件箱任务关联结果不一致");
  }
  return result;
}

export async function updateInboxItemTaskRequirement(
  input: UpdateInboxItemTaskRequirementInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItemTaskMutationResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.inboxItemId)}/tasks/${encodeURIComponent(input.taskId)}`,
    {
      method: "PATCH",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ is_required: input.isRequired }),
    },
  );
  const result = assertInboxTaskMutationMatches(
    normalizeInboxItemTaskMutationResult(payload),
    input.inboxItemId,
    input.taskId,
  );
  if (
    !result.relation.isActive ||
    result.relation.isRequired !== input.isRequired
  ) {
    return invalidResponse("收件箱任务必需状态响应不一致");
  }
  return result;
}

export async function unlinkInboxItemTask(
  input: UnlinkInboxItemTaskInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItemTaskMutationResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.inboxItemId)}/tasks/${encodeURIComponent(input.taskId)}`,
    {
      method: "DELETE",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const result = assertInboxTaskMutationMatches(
    normalizeInboxItemTaskMutationResult(payload),
    input.inboxItemId,
    input.taskId,
  );
  if (result.relation.isActive) {
    return invalidResponse("收件箱任务解除结果不一致");
  }
  return result;
}

export async function splitInboxItem(
  input: SplitInboxItemInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<SplitInboxItemResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.inboxItemId)}/split`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({
        resolution_policy: input.resolutionPolicy,
        tasks: input.tasks.map((task) => ({
          key: task.key,
          parent_key: task.parentKey,
          title: task.title,
          description: task.description,
          kind: task.kind,
          priority: task.priority,
          project_id: task.projectId,
          completion_criteria: task.completionCriteria,
          tag_ids: task.tagIds,
          due_date: task.dueDate,
          planned_date: task.plannedDate,
          estimated_minutes: task.estimatedMinutes,
          review_policy: task.reviewPolicy,
          is_required: task.isRequired,
          assignee_actor_id: task.assigneeActorId,
        })),
      }),
    },
  );
  const result = normalizeSplitInboxItemResult(payload);
  if (
    result.inboxItem.id !== input.inboxItemId ||
    result.created.length !== input.tasks.length ||
    result.created.some((entry, index) => entry.key !== input.tasks[index].key)
  ) {
    return invalidResponse("收件箱任务拆分结果与请求不一致");
  }
  return result;
}

export async function forceResolveInboxItem(
  input: ForceResolveInboxItemInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.id)}/force-resolve`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ confirm: input.confirm, reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const result = normalizeInboxItem(body);
  if (
    result.id !== input.id ||
    result.status !== "resolved" ||
    result.resolutionMode !== "forced"
  ) {
    return invalidResponse("收件箱强制解决响应不一致");
  }
  return result;
}

export async function createInboxItem(
  input: CreateInboxItemInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>("/api/v1/inbox-items", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      title: input.title,
      summary: input.summary,
      priority: input.priority,
      due_at: input.dueAt,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function updateInboxItem(
  id: string,
  input: UpdateInboxItemInput,
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.summary === undefined ? {} : { summary: input.summary }),
        ...(input.priority === undefined ? {} : { priority: input.priority }),
        ...(input.dueAt === undefined ? {} : { due_at: input.dueAt }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function executeInboxItemCommand(
  input: InboxItemCommandInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  let body: Record<string, unknown> = {};
  if (input.action === "snooze") {
    body = { snoozed_until: input.snoozedUntil };
  } else if (input.action === "resolve" || input.action === "dismiss") {
    body = { reason: input.reason };
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.id)}/${input.action}`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify(body),
    },
  );
  const result =
    isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(result);
}

export async function markAllInboxItemsRead(
  throughCreatedAt: string,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<MarkAllInboxReadResult> {
  const payload = await apiRequest<unknown>("/api/v1/inbox-items/read-all", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ through_created_at: throughCreatedAt }),
  });
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("收件箱全部已读响应格式无效");
  const cutoff = stringField(body, "through_created_at", "throughCreatedAt");
  if (!cutoff) return invalidResponse("收件箱全部已读响应缺少截止时间");
  return {
    throughCreatedAt: cutoff,
    markedCount: nonNegativeInteger(
      fieldValue(body, "marked_count", "markedCount"),
      "收件箱已读数量",
    ),
  };
}

export async function getInboxItemEvents(
  id: string,
  input: InboxEventListParams = {},
): Promise<InboxEventListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}/events?${params}`,
  );
  return normalizeInboxEventListResult(payload);
}

export async function getActiveFocusSession(): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>("/api/v1/focus-sessions/active");
  return normalizeFocusSessionSnapshot(payload);
}

export async function getFocusSessions(
  input: FocusSessionListParams = {},
): Promise<FocusSessionListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
    status: input.status ?? "terminal",
  });
  if (input.taskId) params.set("task_id", input.taskId);
  if (input.projectId) params.set("project_id", input.projectId);
  const payload = await apiRequest<unknown>(`/api/v1/focus-sessions?${params}`);
  return normalizeFocusSessionListResult(payload);
}

export async function getFocusReport(
  input: FocusReportParams,
): Promise<FocusReport> {
  const params = new URLSearchParams({
    date_from: input.dateFrom,
    date_to: input.dateTo,
    timezone: input.timezone,
  });
  if (input.projectId) params.set("project_id", input.projectId);
  const payload = await apiRequest<unknown>(`/api/v1/stats/focus?${params}`);
  return normalizeFocusReport(payload);
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

export async function getInboxStats(): Promise<InboxStats> {
  const payload = await apiRequest<unknown>("/api/v1/stats/inbox");
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("收件箱统计响应格式无效");
  const serverNow = fieldValue(body, "server_now", "serverNow");
  if (typeof serverNow !== "string" || !serverNow) {
    return invalidResponse("收件箱统计响应格式无效");
  }
  const counts = {
    pending: numeric(body.pending, -1),
    unread: numeric(body.unread, -1),
    tracking: numeric(body.tracking, -1),
    blocked: numeric(body.blocked, -1),
    waitingReview: numeric(
      fieldValue(body, "waiting_review", "waitingReview"),
      -1,
    ),
  };
  if (Object.values(counts).some((count) => count < 0)) {
    return invalidResponse("收件箱统计响应格式无效");
  }
  return { serverNow, ...counts };
}
