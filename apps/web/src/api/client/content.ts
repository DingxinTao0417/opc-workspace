import type {
  ContentItem,
  ContentItemListParams,
  ContentItemListResult,
  ContentItemStatus,
  CreateContentItemInput,
  PublishContentItemInput,
  ScheduleContentItemInput,
  UpdateContentItemInput,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  invalidResponse,
  isRecord,
  nullableString,
  numeric,
  stringField,
} from "./core";
import { asTaskStatus } from "./tasks";

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
  if (input.scheduleState) params.set("schedule_state", input.scheduleState);
  if (input.platform?.trim()) params.set("platform", input.platform.trim());
  if (input.status) params.set("status", input.status);
  if (input.projectId) params.set("project_id", input.projectId);
  if (input.includeArchived) params.set("include_archived", "true");
  return normalizeContentItemListResult(
    await apiRequest<unknown>(`/api/v1/content-items?${params}`, { signal }),
    input,
  );
}

export async function getContentItem(
  id: string,
  signal?: AbortSignal,
): Promise<ContentItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}`,
    { signal },
  );
  return normalizeContentItem(
    isRecord(payload) && "data" in payload ? payload.data : payload,
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

export async function deleteContentItem(
  id: string,
  expectedVersion: number,
): Promise<{ deletedId: string }> {
  const payload = await apiRequest<unknown>(
    `/api/v1/content-items/${encodeURIComponent(id)}?confirm=true`,
    {
      method: "DELETE",
      headers: expectedVersionHeader(expectedVersion),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("内容项删除响应格式无效");
  const deletedId = stringField(body, "deleted_id");
  if (deletedId !== id) return invalidResponse("内容项删除响应格式无效");
  return { deletedId };
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
