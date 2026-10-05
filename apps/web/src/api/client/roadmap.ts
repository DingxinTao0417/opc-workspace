import type {
  CreateRoadmapMilestoneInput,
  DeleteRoadmapMilestoneResult,
  ReorderRoadmapMilestonesInput,
  RoadmapMilestone,
  RoadmapMilestoneListParams,
  RoadmapMilestoneListResult,
  RoadmapMilestoneStatus,
  UpdateRoadmapMilestoneInput,
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
import { asProjectStatus } from "./projects";

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
  if (input.sort) params.set("sort", input.sort);
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones?${params}`,
    { signal },
  );
  return normalizeRoadmapMilestoneListResult(payload, input);
}

export async function getRoadmapMilestone(
  id: string,
  signal?: AbortSignal,
): Promise<RoadmapMilestone> {
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones/${encodeURIComponent(id)}`,
    { signal },
  );
  return normalizeRoadmapMilestone(
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

export async function updateRoadmapMilestone(
  id: string,
  input: UpdateRoadmapMilestoneInput,
): Promise<RoadmapMilestone> {
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        title: input.title,
        description: input.description,
        year: input.year,
        quarter: input.quarter,
        target_date: input.targetDate,
        status: input.status,
        project_ids: input.projectIds,
      }),
    },
  );
  return normalizeRoadmapMilestone(
    isRecord(payload) && "data" in payload ? payload.data : payload,
  );
}

export async function reorderRoadmapMilestones(
  input: ReorderRoadmapMilestonesInput,
): Promise<RoadmapMilestone[]> {
  const payload = await apiRequest<unknown>(
    "/api/v1/roadmap/milestones/reorder",
    {
      method: "PUT",
      body: JSON.stringify({
        items: input.items.map((item) => ({
          id: item.id,
          expected_version: item.expectedVersion,
        })),
      }),
    },
  );
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("路线图里程碑重排响应格式无效");
  }
  return payload.data.map(normalizeRoadmapMilestone);
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

export async function deleteRoadmapMilestone(
  id: string,
  expectedVersion: number,
): Promise<DeleteRoadmapMilestoneResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/roadmap/milestones/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE", headers: expectedVersionHeader(expectedVersion) },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("路线图里程碑删除响应格式无效");
  return {
    deletedId: stringField(body, "deleted_id", "deletedId") ?? id,
  };
}
