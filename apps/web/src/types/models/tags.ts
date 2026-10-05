import type { PageMeta } from "./common";

export interface Tag {
  id: string;
  name: string;
  color: string;
  version: number;
  createdAt: string;
}

export interface TagListResult {
  items: Tag[];
  meta: PageMeta;
}

export interface TagListParams {
  page?: number;
  pageSize?: number;
  query?: string;
  sort?: string;
}

export interface TagInput {
  name: string;
  color: string;
}

export interface UpdateTagInput {
  name?: string;
  color?: string;
  expectedVersion: number;
}

export interface DeleteTagResult {
  deletedId: string;
  detachedTasks: number;
}
