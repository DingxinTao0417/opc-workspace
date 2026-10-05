import type {
  KnowledgeImportResult,
  KnowledgeIndexJob,
  KnowledgeSearchResponse,
  KnowledgeSearchResult,
  KnowledgeSource,
  KnowledgeSourceListResult,
  KnowledgeSourcesCSVDownload,
} from "../../types/models";
import {
  ARTIFACT_TRANSFER_TIMEOUT_MS,
  ApiError,
  apiFetch,
  apiRequest,
  downloadFileName,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  numeric,
  positiveInteger,
  stringField,
} from "./core";

function knowledgeSourceFromRecord(value: unknown): KnowledgeSource {
  if (!isRecord(value)) return invalidResponse("知识库来源响应格式无效");
  const sourceType = stringField(value, "source_type", "sourceType");
  const status = stringField(value, "status");
  if (
    (sourceType !== "text" &&
      sourceType !== "markdown" &&
      sourceType !== "pdf") ||
    ![
      "pending",
      "indexing",
      "ready",
      "stale",
      "missing",
      "failed",
      "deleted",
    ].includes(status ?? "") ||
    stringField(value, "import_mode", "importMode") !== "managed_copy"
  ) {
    return invalidResponse("知识库来源响应格式无效");
  }
  const rawDocumentVersion = fieldValue(
    value,
    "document_version",
    "documentVersion",
  );
  const rawLatestJob = fieldValue(value, "latest_job", "latestJob");
  return {
    id: stringField(value, "id") ?? "",
    name: stringField(value, "name") ?? "",
    title: stringField(value, "title") ?? stringField(value, "name") ?? "",
    sourceType,
    importMode: "managed_copy",
    mimeType: stringField(value, "mime_type", "mimeType") ?? "",
    sizeBytes: nonNegativeInteger(
      fieldValue(value, "size_bytes", "sizeBytes"),
      "知识库来源大小",
    ),
    contentSha256: stringField(value, "content_sha256", "contentSha256") ?? "",
    status: status as KnowledgeSource["status"],
    lastIndexedAt: nullableString(
      fieldValue(value, "last_indexed_at", "lastIndexedAt"),
    ),
    deletedAt: nullableString(fieldValue(value, "deleted_at", "deletedAt")),
    deleteReason: nullableString(
      fieldValue(value, "delete_reason", "deleteReason"),
    ),
    version: positiveInteger(value.version, "知识库来源版本"),
    createdAt: stringField(value, "created_at", "createdAt") ?? "",
    updatedAt: stringField(value, "updated_at", "updatedAt") ?? "",
    documentId: nullableString(fieldValue(value, "document_id", "documentId")),
    documentVersion:
      rawDocumentVersion === null || rawDocumentVersion === undefined
        ? null
        : positiveInteger(rawDocumentVersion, "知识库文档版本"),
    chunkCount: nonNegativeInteger(
      fieldValue(value, "chunk_count", "chunkCount"),
      "知识库分段数量",
    ),
    latestJob:
      rawLatestJob === null || rawLatestJob === undefined
        ? null
        : knowledgeIndexJobFromRecord(rawLatestJob),
  };
}

function knowledgeIndexJobFromRecord(value: unknown): KnowledgeIndexJob {
  if (!isRecord(value)) return invalidResponse("知识库索引任务响应格式无效");
  const operation = stringField(value, "operation");
  const status = stringField(value, "status");
  const stage = stringField(value, "stage");
  if (
    (operation !== "import" && operation !== "reindex") ||
    !["queued", "running", "succeeded", "failed", "cancelled"].includes(
      status ?? "",
    ) ||
    !["queued", "extracting", "chunking", "indexing", "complete"].includes(
      stage ?? "",
    )
  ) {
    return invalidResponse("知识库索引任务响应格式无效");
  }
  const progress = nonNegativeInteger(value.progress, "知识库索引进度");
  if (progress > 100) return invalidResponse("知识库索引进度响应无效");
  return {
    id: stringField(value, "id") ?? "",
    sourceId: stringField(value, "source_id", "sourceId") ?? "",
    operation,
    status: status as KnowledgeIndexJob["status"],
    stage: stage as KnowledgeIndexJob["stage"],
    progress,
    attempt: positiveInteger(value.attempt, "知识库索引尝试次数"),
    retryOfJobId: nullableString(
      fieldValue(value, "retry_of_job_id", "retryOfJobId"),
    ),
    errorCode: nullableString(fieldValue(value, "error_code", "errorCode")),
    cancelRequested:
      fieldValue(value, "cancel_requested", "cancelRequested") === true,
    startedAt: nullableString(fieldValue(value, "started_at", "startedAt")),
    completedAt: nullableString(
      fieldValue(value, "completed_at", "completedAt"),
    ),
    createdAt: stringField(value, "created_at", "createdAt") ?? "",
  };
}

function knowledgeImportResult(value: unknown): KnowledgeImportResult {
  const body = isRecord(value) && isRecord(value.data) ? value.data : value;
  if (!isRecord(body)) return invalidResponse("知识库导入响应格式无效");
  const source = knowledgeSourceFromRecord(body.source);
  const job = knowledgeIndexJobFromRecord(body.job);
  if (source.id !== job.sourceId) {
    return invalidResponse("知识库导入来源与索引任务不一致");
  }
  return { source, job };
}

function knowledgeSearchResultFromRecord(
  value: unknown,
): KnowledgeSearchResult {
  if (!isRecord(value) || !Array.isArray(value.highlights)) {
    return invalidResponse("知识库检索结果响应格式无效");
  }
  const sourceType = stringField(value, "source_type", "sourceType");
  if (
    sourceType !== "text" &&
    sourceType !== "markdown" &&
    sourceType !== "pdf"
  ) {
    return invalidResponse("知识库检索来源类型无效");
  }
  const excerpt = stringField(value, "excerpt") ?? "";
  const excerptLength = Array.from(excerpt).length;
  const highlights = value.highlights.map((raw) => {
    if (!isRecord(raw)) return invalidResponse("知识库高亮响应格式无效");
    const start = nonNegativeInteger(raw.start, "知识库高亮起点");
    const end = positiveInteger(raw.end, "知识库高亮终点");
    if (end <= start || end > excerptLength) {
      return invalidResponse("知识库高亮范围响应无效");
    }
    return { start, end };
  });
  return {
    chunkId: stringField(value, "chunk_id", "chunkId") ?? "",
    documentId: stringField(value, "document_id", "documentId") ?? "",
    sourceId: stringField(value, "source_id", "sourceId") ?? "",
    sourceName: stringField(value, "source_name", "sourceName") ?? "",
    sourceType,
    documentTitle: stringField(value, "document_title", "documentTitle") ?? "",
    documentVersion: positiveInteger(
      fieldValue(value, "document_version", "documentVersion"),
      "知识库文档版本",
    ),
    chunkIndex: nonNegativeInteger(
      fieldValue(value, "chunk_index", "chunkIndex"),
      "知识库分段序号",
    ),
    startChar: nonNegativeInteger(
      fieldValue(value, "start_char", "startChar"),
      "知识库位置起点",
    ),
    endChar: positiveInteger(
      fieldValue(value, "end_char", "endChar"),
      "知识库位置终点",
    ),
    startLine: positiveInteger(
      fieldValue(value, "start_line", "startLine"),
      "知识库起始行",
    ),
    endLine: positiveInteger(
      fieldValue(value, "end_line", "endLine"),
      "知识库结束行",
    ),
    startPage: positiveInteger(
      fieldValue(value, "start_page", "startPage"),
      "知识库起始页",
    ),
    endPage: positiveInteger(
      fieldValue(value, "end_page", "endPage"),
      "知识库结束页",
    ),
    excerpt,
    highlights,
    rank: numeric(value.rank),
  };
}

export async function getKnowledgeSources(
  input: {
    page?: number;
    pageSize?: number;
    search?: string;
    includeDeleted?: boolean;
  } = {},
): Promise<KnowledgeSourceListResult> {
  const params = new URLSearchParams();
  if (input.page) params.set("page", String(input.page));
  if (input.pageSize) params.set("page_size", String(input.pageSize));
  if (input.search?.trim()) params.set("search", input.search.trim());
  if (input.includeDeleted) params.set("include_deleted", "true");
  const suffix = params.size ? `?${params.toString()}` : "";
  const payload = await apiRequest<unknown>(
    `/api/v1/knowledge/sources${suffix}`,
  );
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("知识库来源列表响应格式无效");
  }
  return {
    items: payload.data.map(knowledgeSourceFromRecord),
    meta: {
      page: positiveInteger(payload.meta.page, "知识库来源页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "知识库来源每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "知识库来源总数"),
    },
  };
}

export async function downloadKnowledgeSourcesCSV(): Promise<KnowledgeSourcesCSVDownload> {
  return apiFetch(
    "/api/v1/knowledge/sources/export.csv?confirm=true",
    async (response) => {
      if (!response.headers.get("Content-Type")?.startsWith("text/csv")) {
        return invalidResponse("知识库来源清单响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "knowledge-sources.csv",
        ),
      };
    },
  );
}

export async function createKnowledgeSource(
  file: File,
  title?: string,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<KnowledgeImportResult> {
  if (file.size < 1) {
    throw new ApiError("知识库文件不能为空", {
      code: "KNOWLEDGE_EMPTY_SOURCE",
      status: 422,
    });
  }
  if (file.size > 16 * 1024 * 1024) {
    throw new ApiError("知识库文件不能超过 16 MiB", {
      code: "KNOWLEDGE_SOURCE_TOO_LARGE",
      status: 413,
    });
  }
  if (!/\.(txt|md|markdown|pdf)$/i.test(file.name)) {
    throw new ApiError("当前只支持 TXT、Markdown 和 PDF 文件", {
      code: "KNOWLEDGE_FORMAT_UNSUPPORTED",
      status: 415,
    });
  }
  const form = new FormData();
  form.append("file", file, file.name);
  if (title?.trim()) form.append("title", title.trim());
  const payload = await apiRequest<unknown>(
    "/api/v1/knowledge/sources",
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: form,
    },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  return knowledgeImportResult(payload);
}

export async function reindexKnowledgeSource(
  id: string,
  expectedVersion: number,
): Promise<KnowledgeImportResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/knowledge/sources/${encodeURIComponent(id)}/reindex`,
    {
      method: "POST",
      headers: expectedVersionHeader(expectedVersion),
    },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  return knowledgeImportResult(payload);
}

export async function deleteKnowledgeSource(
  id: string,
  expectedVersion: number,
): Promise<void> {
  await apiRequest<unknown>(
    `/api/v1/knowledge/sources/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE", headers: expectedVersionHeader(expectedVersion) },
  );
}

export async function searchKnowledge(
  query: string,
  sourceIds: string[] = [],
  limit = 20,
): Promise<KnowledgeSearchResponse> {
  const payload = await apiRequest<unknown>("/api/v1/knowledge/search", {
    method: "POST",
    body: JSON.stringify({ query, source_ids: sourceIds, limit }),
  });
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("知识库检索响应格式无效");
  }
  const rawSourceIds = fieldValue(payload.meta, "source_ids", "sourceIds");
  if (
    !Array.isArray(rawSourceIds) ||
    rawSourceIds.some((id) => typeof id !== "string")
  ) {
    return invalidResponse("知识库检索来源筛选响应格式无效");
  }
  return {
    items: payload.data.map(knowledgeSearchResultFromRecord),
    query: stringField(payload.meta, "query") ?? query,
    sourceIds: rawSourceIds as string[],
  };
}

export async function getKnowledgeIndexJob(
  id: string,
): Promise<KnowledgeIndexJob> {
  const payload = await apiRequest<unknown>(
    `/api/v1/knowledge/index-jobs/${encodeURIComponent(id)}`,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return knowledgeIndexJobFromRecord(body);
}

export async function cancelKnowledgeIndexJob(
  id: string,
): Promise<KnowledgeIndexJob> {
  const payload = await apiRequest<unknown>(
    `/api/v1/knowledge/index-jobs/${encodeURIComponent(id)}/cancel`,
    { method: "POST" },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return knowledgeIndexJobFromRecord(body);
}

export async function retryKnowledgeIndexJob(
  id: string,
  expectedSourceVersion: number,
): Promise<KnowledgeImportResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/knowledge/index-jobs/${encodeURIComponent(id)}/retry`,
    {
      method: "POST",
      headers: expectedVersionHeader(expectedSourceVersion),
    },
    ARTIFACT_TRANSFER_TIMEOUT_MS,
  );
  return knowledgeImportResult(payload);
}
