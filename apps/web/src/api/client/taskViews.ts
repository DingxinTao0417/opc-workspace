import type {
  CreateTaskSavedViewInput,
  DeleteTaskSavedViewResult,
  TaskSavedView,
  TaskSavedViewDefinition,
  UpdateTaskSavedViewInput,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  positiveInteger,
  stringField,
} from "./core";

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
