import type {
  SearchListParams,
  SearchListResult,
  SearchResourceType,
  SearchResult,
} from "../../types/models";
import {
  apiRequest,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  positiveInteger,
} from "./core";

const searchResourceTypes = new Set<SearchResourceType>([
  "task",
  "project",
  "client",
  "inbox_item",
  "invoice",
  "roadmap_milestone",
  "content_item",
]);

const searchRoutes: Record<SearchResourceType, (id: string) => string> = {
  task: (id) => `/tasks/${id}`,
  project: (id) => `/projects/${id}`,
  client: (id) => `/clients/${id}`,
  inbox_item: (id) => `/inbox/${id}`,
  invoice: (id) => `/invoices/${id}`,
  roadmap_milestone: (id) => `/roadmap?milestone=${id}`,
  content_item: (id) => `/content-calendar?item=${id}`,
};

const searchMatchedFields: Record<SearchResourceType, Set<string>> = {
  task: new Set(["title", "description"]),
  project: new Set(["name", "description"]),
  client: new Set(["name", "contact_name", "email", "phone"]),
  inbox_item: new Set(["title", "summary"]),
  invoice: new Set(["invoice_number", "client_name"]),
  roadmap_milestone: new Set(["title", "description", "project_names"]),
  content_item: new Set(["title", "notes", "platform"]),
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
  invoice: new Set(["draft", "sent", "viewed", "paid", "overdue"]),
  roadmap_milestone: new Set(["planned", "active", "achieved"]),
  content_item: new Set([
    "draft",
    "in_review",
    "scheduled",
    "published",
    "cancelled",
  ]),
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
      entry.route !== searchRoutes[resourceType](entry.resource_id) ||
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
  signal?: AbortSignal,
): Promise<SearchListResult> {
  const params = new URLSearchParams({
    q: input.q.trim(),
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
  });
  if (input.types?.length) params.set("types", input.types.join(","));
  const payload = await apiRequest<unknown>(`/api/v1/search?${params}`, {
    signal,
  });
  return normalizeSearchListResult(payload);
}
