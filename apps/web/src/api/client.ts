import type {
  BatchUpdateTasksInput,
  BatchUpdateTasksResult,
  DeleteTagResult,
  HealthResponse,
  NewTaskInput,
  DeleteProjectResult,
  Project,
  ProjectInput,
  ProjectListParams,
  ProjectListResult,
  ProjectStatus,
  ProjectTransitionAction,
  ReorderTasksInput,
  ReorderTasksResult,
  Tag,
  TagInput,
  TagListParams,
  TagListResult,
  Task,
  TaskKind,
  TaskListParams,
  TaskListResult,
  TaskPriority,
  TaskStatus,
  TodayStats,
  UpdateTagInput,
  UpdateTaskInput,
  UpdateProjectInput,
} from "../types/models";

const DEV_TOKEN =
  import.meta.env.VITE_OPC_SESSION_TOKEN ?? "opc-workspace-local-dev";

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

async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const connection = await getRuntimeConnection();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  if (connection.token)
    headers.set("Authorization", `Bearer ${connection.token}`);

  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 8_000);

  try {
    const response = await fetch(`${connection.baseUrl}${path}`, {
      ...init,
      headers,
      signal: init.signal ?? controller.signal,
    });
    const body = (await response.json().catch(() => undefined)) as unknown;

    if (!response.ok) {
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

    return body as T;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new ApiError("连接本地 Sidecar 超时", { code: "TIMEOUT" });
    }
    throw new ApiError("无法连接本地 Sidecar", { code: "NETWORK_ERROR" });
  } finally {
    window.clearTimeout(timeout);
  }
}

function invalidResponse(message: string): never {
  throw new ApiError(message, { code: "INVALID_RESPONSE" });
}

function asTaskStatus(value: unknown): TaskStatus {
  if (value === "todo" || value === "in_progress" || value === "done") {
    return value;
  }
  return invalidResponse("任务状态响应无效");
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
    tags: Array.isArray(rawTags) ? rawTags.map(normalizeTag) : [],
  };
}

export async function getHealth(): Promise<HealthResponse> {
  return apiRequest<HealthResponse>("/health");
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
      status: input.status,
      priority: input.priority,
      project_id: input.projectId ?? null,
      parent_task_id: input.parentTaskId ?? null,
      completion_criteria: input.completionCriteria ?? "",
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

export async function updateTaskStatus(
  id: string,
  status: TaskStatus,
  expectedVersion: number,
): Promise<Task> {
  const payload = await apiRequest<unknown>(
    `/api/v1/tasks/${encodeURIComponent(id)}/status`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ status }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeTask(body);
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
  await apiRequest<void>(`/api/v1/tasks/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: expectedVersionHeader(expectedVersion),
  });
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

export async function getTodayStats(date: string): Promise<TodayStats> {
  const payload = await apiRequest<unknown>(
    `/api/v1/stats/today?date=${encodeURIComponent(date)}`,
  );
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
      minutes: numeric(data.focus.minutes),
    },
  };
}
