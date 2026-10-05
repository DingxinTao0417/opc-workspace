import type { PageMeta } from "./common";
import type { ProjectStatus } from "./projects";

export type RoadmapMilestoneStatus =
  "planned" | "active" | "achieved" | "archived";

export interface RoadmapMilestoneProject {
  id: string;
  name: string;
  status: ProjectStatus;
}

export interface RoadmapMilestoneTaskSummary {
  total: number;
  completed: number;
  inProgress: number;
  progressPercent: number;
}

export interface RoadmapMilestone {
  id: string;
  title: string;
  description: string | null;
  year: number;
  quarter: 1 | 2 | 3 | 4;
  targetDate: string;
  status: RoadmapMilestoneStatus;
  manualOrder: number;
  archivedFromStatus: Exclude<RoadmapMilestoneStatus, "archived"> | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  projects: RoadmapMilestoneProject[];
  taskSummary: RoadmapMilestoneTaskSummary;
}

export interface RoadmapMilestoneListParams {
  page?: number;
  pageSize?: number;
  year?: number;
  quarter?: 1 | 2 | 3 | 4;
  status?: RoadmapMilestoneStatus;
  projectId?: string;
  includeArchived?: boolean;
  sort?: "target_date";
}

export interface RoadmapMilestoneListResult {
  items: RoadmapMilestone[];
  meta: PageMeta;
}

export interface CreateRoadmapMilestoneInput {
  title: string;
  description: string | null;
  year: number;
  quarter: 1 | 2 | 3 | 4;
  targetDate: string;
  status?: Exclude<RoadmapMilestoneStatus, "archived">;
  projectIds: string[];
}

export interface UpdateRoadmapMilestoneInput {
  title?: string;
  description?: string | null;
  year?: number;
  quarter?: 1 | 2 | 3 | 4;
  targetDate?: string;
  status?: Exclude<RoadmapMilestoneStatus, "archived">;
  projectIds?: string[];
  expectedVersion: number;
}

export interface DeleteRoadmapMilestoneResult {
  deletedId: string;
}

export interface ReorderRoadmapMilestonesInput {
  items: Array<{
    id: string;
    expectedVersion: number;
  }>;
}
