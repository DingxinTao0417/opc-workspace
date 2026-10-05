import type {
  AssignmentRole,
  CreateTaskAssignmentInput,
  EndTaskAssignmentInput,
  ReassignTaskAssignmentInput,
  ReassignTaskAssignmentResult,
  TaskAssignment,
  TaskAssignmentListParams,
  TaskAssignmentListResult,
  TaskAssignmentMutationResult,
} from "../../types/models";
import { normalizeActorSummary } from "./actors";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
  stringField,
  versionedCommandHeaders,
} from "./core";
import { normalizeTask } from "./tasks";

function asAssignmentRole(value: unknown): AssignmentRole {
  if (value === "assignee" || value === "reviewer") return value;
  return invalidResponse("任务分派角色响应无效");
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
