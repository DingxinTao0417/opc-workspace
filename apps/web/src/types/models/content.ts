import type { PageMeta } from "./common";
import type { TaskStatus } from "./tasks";

export type ContentItemStatus =
  "draft" | "in_review" | "scheduled" | "published" | "cancelled" | "archived";

export interface ContentItemTask {
  id: string;
  title: string;
  status: TaskStatus;
  isRequired: boolean;
}

export interface ContentItem {
  id: string;
  title: string;
  platform: string;
  status: ContentItemStatus;
  scheduledAt: string | null;
  scheduledTimezone: string | null;
  publishedAt: string | null;
  projectId: string | null;
  notes: string | null;
  externalLink: string | null;
  manualOrder: number;
  archivedFromStatus: Exclude<ContentItemStatus, "archived"> | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  tasks: ContentItemTask[];
  requiredTaskTotal: number;
  requiredTaskDone: number;
}

export interface ContentItemListParams {
  page?: number;
  pageSize?: number;
  scheduledFrom?: string;
  scheduledTo?: string;
  scheduleState?: "scheduled" | "unscheduled";
  platform?: string;
  status?: ContentItemStatus;
  projectId?: string;
  includeArchived?: boolean;
}

export interface ContentItemListResult {
  items: ContentItem[];
  meta: PageMeta;
}

export interface CreateContentItemInput {
  title: string;
  platform: string;
  status?: Exclude<ContentItemStatus, "published" | "archived">;
  scheduledAt?: string | null;
  scheduledTimezone?: string | null;
  projectId?: string | null;
  notes?: string | null;
  externalLink?: string | null;
}

export interface UpdateContentItemInput {
  title?: string;
  platform?: string;
  status?: Exclude<ContentItemStatus, "published">;
  projectId?: string | null;
  notes?: string | null;
  externalLink?: string | null;
  expectedVersion: number;
}

export interface ScheduleContentItemInput {
  scheduledAt: string | null;
  scheduledTimezone: string | null;
  expectedVersion: number;
}

export interface PublishContentItemInput {
  publishedAt?: string;
  externalLink?: string | null;
  expectedVersion: number;
}
