import type { PageMeta } from "./common";

export type KnowledgeSourceType = "text" | "markdown" | "pdf";

export type KnowledgeSourceStatus =
  "pending" | "indexing" | "ready" | "stale" | "missing" | "failed" | "deleted";

export interface KnowledgeSource {
  id: string;
  name: string;
  title: string;
  sourceType: KnowledgeSourceType;
  importMode: "managed_copy";
  mimeType: string;
  sizeBytes: number;
  contentSha256: string;
  status: KnowledgeSourceStatus;
  lastIndexedAt: string | null;
  deletedAt: string | null;
  deleteReason: string | null;
  version: number;
  createdAt: string;
  updatedAt: string;
  documentId: string | null;
  documentVersion: number | null;
  chunkCount: number;
  latestJob: KnowledgeIndexJob | null;
}

export interface KnowledgeSourceListResult {
  items: KnowledgeSource[];
  meta: PageMeta;
}

export interface KnowledgeSourcesCSVDownload {
  blob: Blob;
  fileName: string;
}

export type KnowledgeIndexJobStatus =
  "queued" | "running" | "succeeded" | "failed" | "cancelled";

export interface KnowledgeIndexJob {
  id: string;
  sourceId: string;
  operation: "import" | "reindex";
  status: KnowledgeIndexJobStatus;
  stage: "queued" | "extracting" | "chunking" | "indexing" | "complete";
  progress: number;
  attempt: number;
  retryOfJobId: string | null;
  errorCode: string | null;
  cancelRequested: boolean;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
}

export interface KnowledgeImportResult {
  source: KnowledgeSource;
  job: KnowledgeIndexJob;
}

export interface KnowledgeHighlight {
  start: number;
  end: number;
}

export interface KnowledgeSearchResult {
  chunkId: string;
  documentId: string;
  sourceId: string;
  sourceName: string;
  sourceType: KnowledgeSourceType;
  documentTitle: string;
  documentVersion: number;
  chunkIndex: number;
  startChar: number;
  endChar: number;
  startLine: number;
  endLine: number;
  startPage: number;
  endPage: number;
  excerpt: string;
  highlights: KnowledgeHighlight[];
  rank: number;
}

export interface KnowledgeSearchResponse {
  items: KnowledgeSearchResult[];
  query: string;
  sourceIds: string[];
}
