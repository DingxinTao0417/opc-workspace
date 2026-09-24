import { ApiError, apiRequest } from "./client";
import {
  isKnowledgeLocation,
  knowledgeLocationKeys,
  type KnowledgeLocation,
} from "../lib/knowledgeLocation";
import type { AiKnowledgeContextSource } from "../types/models";

export type KnowledgeChunk = AiKnowledgeContextSource & {
  start_page: number;
  end_page: number;
};

export async function getKnowledgeChunk(
  location: KnowledgeLocation,
  signal?: AbortSignal,
): Promise<KnowledgeChunk> {
  if (!isKnowledgeLocation(location))
    throw new ApiError("知识引用位置无效", {
      code: "INVALID_KNOWLEDGE_LOCATION",
    });
  const params = new URLSearchParams();
  for (const key of knowledgeLocationKeys) {
    if (key !== "chunk_id") params.set(key, String(location[key]));
  }
  const response = await apiRequest<{ data?: unknown }>(
    `/api/v1/knowledge/chunks/${location.chunk_id}?${params}`,
    { signal },
  );
  const row = response?.data;
  const invalid = () =>
    new ApiError("知识片段响应与引用位置不一致，请重新加载", {
      code: "INVALID_RESPONSE",
    });
  if (!row || typeof row !== "object" || Array.isArray(row)) throw invalid();
  const data = row as Record<string, unknown>;
  if (knowledgeLocationKeys.some((key) => data[key] !== location[key]))
    throw invalid();
  if (
    typeof data.source_type !== "string" ||
    !["text", "markdown", "pdf"].includes(data.source_type)
  )
    throw invalid();
  for (const key of ["source_name", "document_title", "content"] as const) {
    if (typeof data[key] !== "string" || !data[key].trim()) throw invalid();
  }
  for (const key of [
    "chunk_index",
    "start_char",
    "end_char",
    "start_line",
    "end_line",
    "start_page",
    "end_page",
  ] as const) {
    if (
      typeof data[key] !== "number" ||
      !Number.isSafeInteger(data[key]) ||
      data[key] < (key === "chunk_index" || key === "start_char" ? 0 : 1)
    )
      throw invalid();
  }
  const chunk = data as unknown as KnowledgeChunk;
  const length = Array.from(chunk.content).length;
  if (
    length > 4096 ||
    chunk.end_char - chunk.start_char !== length ||
    chunk.end_line < chunk.start_line ||
    chunk.end_page < chunk.start_page
  )
    throw invalid();
  // Select only public chunk fields: no path, original bytes or file URLs.
  return {
    ...location,
    source_name: chunk.source_name,
    document_title: chunk.document_title,
    source_type: chunk.source_type,
    chunk_index: chunk.chunk_index,
    start_char: chunk.start_char,
    end_char: chunk.end_char,
    start_line: chunk.start_line,
    end_line: chunk.end_line,
    start_page: chunk.start_page,
    end_page: chunk.end_page,
    content: chunk.content,
  };
}
