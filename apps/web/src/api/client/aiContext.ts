import type {
  AiBusinessContextPreview,
  AiBusinessContextProviderSnapshot,
  AiBusinessContextSource,
  AiBusinessContextType,
  AiCitation,
  AiCitationStatus,
  AiKnowledgeContextSource,
} from "../../types/models";
import {
  apiRequest,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  numeric,
  positiveInteger,
  stringField,
} from "./core";
import type { JsonRecord } from "./core";

export function aiBusinessContextSourceFromRecord(
  row: unknown,
): AiBusinessContextSource {
  if (
    !isRecord(row) ||
    !isRecord(row.fields) ||
    !Array.isArray(row.truncated_fields)
  ) {
    return invalidResponse("AI 业务上下文响应格式无效");
  }
  const type = stringField(row, "type");
  if (type !== "task" && type !== "project" && type !== "client") {
    return invalidResponse("AI 业务上下文类型无效");
  }
  const truncatedFields = row.truncated_fields.map((field) => {
    if (typeof field !== "string") {
      return invalidResponse("AI 业务上下文截断标记无效");
    }
    return field;
  });
  return {
    type,
    id: stringField(row, "id") ?? "",
    version: numeric(row.version, 0),
    label: stringField(row, "label") ?? "",
    fields: row.fields,
    truncated_fields: truncatedFields,
  };
}

export function aiBusinessContextProviderFromRecord(
  row: unknown,
): AiBusinessContextProviderSnapshot | null {
  if (row === null || row === undefined) return null;
  if (!isRecord(row)) {
    return invalidResponse("AI 业务上下文供应商响应格式无效");
  }
  const kind = stringField(row, "kind");
  if (kind !== "local" && kind !== "remote") {
    return invalidResponse("AI 业务上下文供应商类型无效");
  }
  return {
    id: stringField(row, "id") ?? "",
    name: stringField(row, "name") ?? "",
    kind,
    version: numeric(row.version, 0),
  };
}

function aiKnowledgePageRange(row: JsonRecord, sourceType: string) {
  const start = fieldValue(row, "start_page", "startPage");
  const end = fieldValue(row, "end_page", "endPage");
  if (
    sourceType !== "pdf" &&
    (start === undefined || start === 0) &&
    (end === undefined || end === 0)
  ) {
    return { start_page: 1, end_page: 1 };
  }
  const startPage = positiveInteger(start, "知识引用起始页");
  const endPage = positiveInteger(end, "知识引用结束页");
  if (
    !Number.isSafeInteger(startPage) ||
    !Number.isSafeInteger(endPage) ||
    endPage < startPage
  ) {
    return invalidResponse("知识引用页码范围无效");
  }
  return { start_page: startPage, end_page: endPage };
}

export function aiKnowledgeContextSourceFromRecord(
  row: unknown,
): AiKnowledgeContextSource {
  if (!isRecord(row)) {
    return invalidResponse("AI 知识库上下文响应格式无效");
  }
  const sourceType = stringField(row, "source_type", "sourceType");
  if (
    sourceType !== "text" &&
    sourceType !== "markdown" &&
    sourceType !== "pdf"
  ) {
    return invalidResponse("AI 知识库来源类型无效");
  }
  const startChar = nonNegativeInteger(
    fieldValue(row, "start_char", "startChar"),
    "AI 知识库字符起点",
  );
  const endChar = positiveInteger(
    fieldValue(row, "end_char", "endChar"),
    "AI 知识库字符终点",
  );
  const startLine = positiveInteger(
    fieldValue(row, "start_line", "startLine"),
    "AI 知识库起始行",
  );
  const endLine = positiveInteger(
    fieldValue(row, "end_line", "endLine"),
    "AI 知识库结束行",
  );
  if (endChar <= startChar || endLine < startLine) {
    return invalidResponse("AI 知识库位置响应无效");
  }
  return {
    source_id: stringField(row, "source_id", "sourceId") ?? "",
    source_name: stringField(row, "source_name", "sourceName") ?? "",
    source_version: positiveInteger(
      fieldValue(row, "source_version", "sourceVersion"),
      "AI 知识库来源版本",
    ),
    source_type: sourceType,
    document_id: stringField(row, "document_id", "documentId") ?? "",
    document_title: stringField(row, "document_title", "documentTitle") ?? "",
    document_version: positiveInteger(
      fieldValue(row, "document_version", "documentVersion"),
      "AI 知识库文档版本",
    ),
    chunk_id: stringField(row, "chunk_id", "chunkId") ?? "",
    chunk_index: nonNegativeInteger(
      fieldValue(row, "chunk_index", "chunkIndex"),
      "AI 知识库分段序号",
    ),
    start_char: startChar,
    end_char: endChar,
    start_line: startLine,
    end_line: endLine,
    ...aiKnowledgePageRange(row, sourceType),
    content: stringField(row, "content") ?? "",
  };
}

function aiCitationFromRecord(row: unknown): AiCitation {
  if (!isRecord(row)) return invalidResponse("AI 引用响应格式无效");
  const sourceType = stringField(row, "source_type", "sourceType");
  if (
    sourceType !== "text" &&
    sourceType !== "markdown" &&
    sourceType !== "pdf"
  ) {
    return invalidResponse("AI 引用来源类型无效");
  }
  const startChar = nonNegativeInteger(
    fieldValue(row, "start_char", "startChar"),
    "AI 引用字符起点",
  );
  const endChar = positiveInteger(
    fieldValue(row, "end_char", "endChar"),
    "AI 引用字符终点",
  );
  const startLine = positiveInteger(
    fieldValue(row, "start_line", "startLine"),
    "AI 引用起始行",
  );
  const endLine = positiveInteger(
    fieldValue(row, "end_line", "endLine"),
    "AI 引用结束行",
  );
  if (endChar <= startChar || endLine < startLine) {
    return invalidResponse("AI 引用位置响应无效");
  }
  return {
    chunk_id: stringField(row, "chunk_id", "chunkId") ?? "",
    source_id: stringField(row, "source_id", "sourceId") ?? "",
    source_name: stringField(row, "source_name", "sourceName") ?? "",
    source_type: sourceType,
    source_version: positiveInteger(
      fieldValue(row, "source_version", "sourceVersion"),
      "AI 引用来源版本",
    ),
    document_id: stringField(row, "document_id", "documentId") ?? "",
    document_title: stringField(row, "document_title", "documentTitle") ?? "",
    document_version: positiveInteger(
      fieldValue(row, "document_version", "documentVersion"),
      "AI 引用文档版本",
    ),
    chunk_index: nonNegativeInteger(
      fieldValue(row, "chunk_index", "chunkIndex"),
      "AI 引用分段序号",
    ),
    start_char: startChar,
    end_char: endChar,
    start_line: startLine,
    end_line: endLine,
    ...aiKnowledgePageRange(row, sourceType),
  };
}

export function aiCitationsFromMessage(row: JsonRecord): {
  status: AiCitationStatus;
  items: AiCitation[];
} {
  const rawStatus = fieldValue(row, "citation_status", "citationStatus");
  const status: AiCitationStatus =
    rawStatus === undefined
      ? "not_requested"
      : rawStatus === "not_requested" ||
          rawStatus === "validated" ||
          rawStatus === "no_evidence" ||
          rawStatus === "missing" ||
          rawStatus === "invalid"
        ? rawStatus
        : invalidResponse("AI 引用状态响应无效");
  const rawItems = fieldValue(row, "citations");
  const items =
    rawItems === undefined
      ? []
      : Array.isArray(rawItems)
        ? rawItems.map(aiCitationFromRecord)
        : invalidResponse("AI 引用列表响应格式无效");
  if ((status === "validated") !== items.length > 0 || items.length > 3) {
    return invalidResponse("AI 引用状态与列表不一致");
  }
  return { status, items };
}

// Generation/SSE metadata is optional for older services and expired temporary
// runs. Once present it must be complete and safe for precise source links.
export function aiGenerationCitations(row: JsonRecord) {
  if (row.citation_status === undefined && row.citations === undefined)
    return undefined;
  if (row.citation_status === undefined || !Array.isArray(row.citations))
    return invalidResponse("生成引用信息不完整");
  if (
    new TextEncoder().encode(JSON.stringify(row.citations)).byteLength >
    16 * 1024
  )
    return invalidResponse("生成引用信息超过大小限制");
  const result = aiCitationsFromMessage(row);
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  const seen = new Set<string>();
  for (const item of result.items) {
    if (
      !uuid.test(item.chunk_id) ||
      !uuid.test(item.source_id) ||
      !uuid.test(item.document_id) ||
      !item.source_name.trim() ||
      !item.document_title.trim() ||
      ![
        item.source_version,
        item.document_version,
        item.chunk_index,
        item.start_char,
        item.end_char,
        item.start_line,
        item.end_line,
        item.start_page,
        item.end_page,
      ].every(Number.isSafeInteger) ||
      seen.has(item.chunk_id)
    )
      return invalidResponse("生成引用身份无效");
    seen.add(item.chunk_id);
  }
  return result;
}

export async function previewAiBusinessContext(input: {
  provider_id: string;
  sources: Array<{ type: AiBusinessContextType; id: string }>;
  knowledge?: Array<{
    source_id: string;
    document_id: string;
    chunk_id: string;
  }>;
}): Promise<AiBusinessContextPreview> {
  const payload = await apiRequest<unknown>("/api/v1/ai/context/preview", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!isRecord(payload) || !isRecord(payload.data)) {
    return invalidResponse("AI 业务上下文预览响应格式无效");
  }
  const row = payload.data;
  const providerKind = stringField(row, "provider_kind");
  if (
    (providerKind !== "local" && providerKind !== "remote") ||
    !Array.isArray(row.sources) ||
    !(row.knowledge === undefined || Array.isArray(row.knowledge))
  ) {
    return invalidResponse("AI 业务上下文预览响应格式无效");
  }
  return {
    provider_id: stringField(row, "provider_id") ?? "",
    provider_name: stringField(row, "provider_name") ?? "",
    provider_kind: providerKind,
    provider_version: numeric(row.provider_version, 0),
    leaves_device: row.leaves_device === true,
    serialized_bytes: numeric(row.serialized_bytes, 0),
    sources: row.sources.map((source) =>
      aiBusinessContextSourceFromRecord(source),
    ),
    knowledge: Array.isArray(row.knowledge)
      ? row.knowledge.map(aiKnowledgeContextSourceFromRecord)
      : [],
  };
}
