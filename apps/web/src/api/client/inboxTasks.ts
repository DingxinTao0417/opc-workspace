import type {
  InboxEventListParams,
  InboxEventListResult,
  InboxItemTaskListParams,
  InboxItemTaskListResult,
  InboxItemTaskMutationResult,
  InboxItemTaskRelation,
  InboxSplitTaskResult,
  InboxTaskProgress,
  InboxTaskSummary,
  InboxWorkflowEvent,
  LinkInboxItemTaskInput,
  SplitInboxItemInput,
  SplitInboxItemResult,
  UnlinkInboxItemTaskInput,
  UpdateInboxItemTaskRequirementInput,
} from "../../types/models";
import { normalizeActorSummary } from "./actors";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  positiveInteger,
  stringField,
  versionedCommandHeaders,
} from "./core";
import { normalizeInboxItem } from "./inbox";
import { normalizeTaskAssignment } from "./taskAssignments";
import {
  asTaskKind,
  asTaskPriority,
  asTaskStatus,
  normalizeTask,
} from "./tasks";

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

export async function getInboxItemTasks(
  id: string,
  input: InboxItemTaskListParams = {},
  signal?: AbortSignal,
): Promise<InboxItemTaskListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}/tasks?${params}`,
    { signal },
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

export async function getInboxItemEvents(
  id: string,
  input: InboxEventListParams = {},
  signal?: AbortSignal,
): Promise<InboxEventListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}/events?${params}`,
    { signal },
  );
  return normalizeInboxEventListResult(payload);
}
