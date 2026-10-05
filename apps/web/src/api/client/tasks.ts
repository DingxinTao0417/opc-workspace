import type {
  BatchUpdateTasksInput,
  BatchUpdateTasksResult,
  NewTaskInput,
  ReorderTasksInput,
  ReorderTasksResult,
  Task,
  TaskEventListParams,
  TaskEventListResult,
  TaskKind,
  TaskLifecycleAction,
  TaskLifecycleCommandInput,
  TaskLifecycleCommandResult,
  TaskListParams,
  TaskListResult,
  TaskPriority,
  TaskReviewPolicy,
  TaskStatus,
  TaskWorkflowEvent,
  UpdateTaskInput,
} from "../../types/models";
import { normalizeActorSummary } from "./actors";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableEventSnapshot,
  nullableEventString,
  nullableNonNegativeInteger,
  nullableString,
  positiveInteger,
  stringField,
  versionedCommandHeaders,
} from "./core";
import { normalizeTag } from "./tags";

export function asTaskStatus(value: unknown): TaskStatus {
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

export function asTaskPriority(value: unknown): TaskPriority {
  if (value === "P0" || value === "P1" || value === "P2" || value === "P3") {
    return value;
  }
  return invalidResponse("任务优先级响应无效");
}

export function asTaskKind(value: unknown): TaskKind {
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

export async function getTaskPage(
  options: TaskListParams = {},
  signal?: AbortSignal,
): Promise<TaskListResult> {
  const requestedPage = options.page ?? 1;
  const requestedPageSize = options.pageSize ?? 50;
  if (
    !Number.isSafeInteger(requestedPage) ||
    requestedPage < 1 ||
    requestedPage > 1_000_000 ||
    !Number.isSafeInteger(requestedPageSize) ||
    requestedPageSize < 1 ||
    requestedPageSize > 100 ||
    !Number.isSafeInteger((requestedPage - 1) * requestedPageSize)
  ) {
    throw new ApiError("任务分页参数无效", {
      code: "VALIDATION_ERROR",
      status: 422,
    });
  }
  const requestedKind = (options.kind?.trim() || undefined) as
    TaskKind | undefined;
  const requestedStatus = (options.status?.trim() || undefined) as
    TaskStatus | "active" | undefined;
  const requestedPriority = (options.priority?.trim() || undefined) as
    TaskPriority | undefined;
  const requestedProjectId = options.projectId?.trim() || undefined;
  const requestedClientId = options.clientId?.trim() || undefined;
  const requestedPlannedDate = options.plannedDate?.trim() || undefined;
  const requestedPlannedFrom = options.plannedFrom?.trim() || undefined;
  const requestedPlannedTo = options.plannedTo?.trim() || undefined;
  const requestedPlannedState = options.plannedState?.trim() || undefined;
  const requestedDueFrom = options.dueFrom?.trim() || undefined;
  const requestedDueTo = options.dueTo?.trim() || undefined;
  const requestedDueState = options.dueState?.trim() || undefined;
  const requestedParentTaskId = options.parentTaskId?.trim() || undefined;
  const params = new URLSearchParams({
    page: String(requestedPage),
    page_size: String(requestedPageSize),
  });
  if (options.q?.trim()) params.set("q", options.q.trim());
  if (requestedKind) params.set("kind", requestedKind);
  if (requestedStatus) params.set("status", requestedStatus);
  if (requestedPriority) params.set("priority", requestedPriority);
  if (requestedProjectId) params.set("project_id", requestedProjectId);
  if (requestedClientId) params.set("client_id", requestedClientId);
  for (const tagId of new Set(options.tagIds?.map((id) => id.trim()))) {
    if (tagId) params.append("tag_id", tagId);
  }
  if (requestedPlannedDate) params.set("planned_date", requestedPlannedDate);
  if (requestedPlannedFrom) params.set("planned_from", requestedPlannedFrom);
  if (requestedPlannedTo) params.set("planned_to", requestedPlannedTo);
  if (requestedPlannedState) params.set("planned_state", requestedPlannedState);
  if (requestedDueFrom) params.set("due_from", requestedDueFrom);
  if (requestedDueTo) params.set("due_to", requestedDueTo);
  if (requestedDueState) params.set("due_state", requestedDueState);
  if (requestedParentTaskId)
    params.set("parent_task_id", requestedParentTaskId);
  if (options.rootOnly !== undefined)
    params.set("root_only", String(options.rootOnly));
  if (options.sort?.trim()) params.set("sort", options.sort.trim());
  const payload = await apiRequest<unknown>(`/api/v1/tasks?${params}`, {
    signal,
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("任务列表响应格式无效");
  }
  const items = payload.data.map(normalizeTask);
  const page = positiveInteger(payload.meta.page, "任务页码");
  const pageSize = positiveInteger(payload.meta.page_size, "任务分页大小");
  const total = nonNegativeInteger(payload.meta.total, "任务总数");
  if (!Number.isSafeInteger(total)) {
    return invalidResponse("任务总数响应无效");
  }
  const expectedItemCount = Math.min(
    requestedPageSize,
    Math.max(0, total - (requestedPage - 1) * requestedPageSize),
  );
  const requestedTagIds = new Set(
    options.tagIds?.map((id) => id.trim()).filter(Boolean),
  );
  const mismatchedFilter = items.some((task) => {
    const dueDate = task.dueDate?.slice(0, 10) ?? null;
    const tagIds = new Set(task.tags.map((tag) => tag.id));
    return (
      (requestedKind !== undefined && task.kind !== requestedKind) ||
      (requestedStatus === "active" &&
        (task.status === "done" || task.status === "cancelled")) ||
      (requestedStatus !== undefined &&
        requestedStatus !== "active" &&
        task.status !== requestedStatus) ||
      (requestedPriority !== undefined &&
        task.priority !== requestedPriority) ||
      (requestedProjectId !== undefined &&
        task.projectId !== requestedProjectId) ||
      (requestedParentTaskId !== undefined &&
        task.parentTaskId !== requestedParentTaskId) ||
      (options.rootOnly === true && task.parentTaskId !== null) ||
      (requestedPlannedDate !== undefined &&
        task.plannedDate !== requestedPlannedDate) ||
      (requestedPlannedFrom !== undefined &&
        (task.plannedDate === null ||
          task.plannedDate < requestedPlannedFrom)) ||
      (requestedPlannedTo !== undefined &&
        (task.plannedDate === null || task.plannedDate > requestedPlannedTo)) ||
      (requestedPlannedState === "scheduled" && task.plannedDate === null) ||
      (requestedPlannedState === "unscheduled" && task.plannedDate !== null) ||
      (requestedDueState !== undefined &&
        (dueDate === null ||
          task.status === "done" ||
          task.status === "cancelled")) ||
      [...requestedTagIds].some((tagId) => !tagIds.has(tagId))
    );
  });
  if (
    page !== requestedPage ||
    pageSize !== requestedPageSize ||
    items.length !== expectedItemCount ||
    new Set(items.map((task) => task.id)).size !== items.length ||
    mismatchedFilter
  ) {
    return invalidResponse("任务列表分页响应无效");
  }
  return {
    items,
    meta: { page, pageSize, total },
  };
}

export async function getTasks(
  options: TaskListParams = {},
  signal?: AbortSignal,
): Promise<Task[]> {
  return (
    await getTaskPage(
      {
        ...options,
        pageSize: options.pageSize ?? 100,
      },
      signal,
    )
  ).items;
}

export async function getAllTasks(
  options: Omit<TaskListParams, "page" | "pageSize"> = {},
  signal?: AbortSignal,
): Promise<Task[]> {
  const allTasks: Task[] = [];
  const taskIds = new Set<string>();
  const pageSize = 100;
  let page = 1;
  let total: number | null = null;

  while (true) {
    const result = await getTaskPage({ ...options, page, pageSize }, signal);
    if (total === null) total = result.meta.total;
    else if (result.meta.total !== total) {
      return invalidResponse("任务列表分页总数不一致");
    }
    for (const task of result.items) {
      if (taskIds.has(task.id)) {
        return invalidResponse("任务列表包含重复记录");
      }
      taskIds.add(task.id);
      allTasks.push(task);
    }
    if (allTasks.length === total) break;
    if (page === 1_000_000) {
      return invalidResponse("任务列表分页超出服务端上限");
    }
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
  if (input.action === "set_priority") body.priority = input.priority;
  if (input.action === "set_due_date") body.due_date = input.dueDate;
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
    action !== "set_priority" &&
    action !== "set_due_date" &&
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
