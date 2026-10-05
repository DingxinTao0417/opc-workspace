import type { PageMeta } from "./common";

export type SearchResourceType =
  | "task"
  | "project"
  | "client"
  | "inbox_item"
  | "invoice"
  | "roadmap_milestone"
  | "content_item";

export interface SearchResult {
  resourceType: SearchResourceType;
  resourceId: string;
  title: string;
  subtitle: string;
  matchedFields: string[];
  route: string;
  status: string;
  updatedAt: string;
}

export interface SearchListParams {
  q: string;
  types?: SearchResourceType[];
  page?: number;
  pageSize?: number;
}

export interface SearchListResult {
  items: SearchResult[];
  meta: PageMeta;
}
