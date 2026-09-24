import type { AiCitation } from "../types/models";

export type KnowledgeLocation = Pick<
  AiCitation,
  | "source_id"
  | "document_id"
  | "chunk_id"
  | "source_version"
  | "document_version"
>;

export const knowledgeLocationKeys = [
  "source_id",
  "document_id",
  "chunk_id",
  "source_version",
  "document_version",
] as const;

export function isKnowledgeIdentity(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)
  );
}

export function isKnowledgeLocation(value: KnowledgeLocation): boolean {
  return (
    [value.source_id, value.document_id, value.chunk_id].every(
      isKnowledgeIdentity,
    ) &&
    [value.source_version, value.document_version].every(
      (version) => Number.isSafeInteger(version) && version > 0,
    )
  );
}

export function parseKnowledgeLocation(
  params: URLSearchParams,
): KnowledgeLocation | null {
  if (knowledgeLocationKeys.some((key) => params.getAll(key).length !== 1))
    return null;
  for (const key of ["source_version", "document_version"] as const) {
    if (!/^[1-9][0-9]*$/.test(params.get(key) ?? "")) return null;
  }
  const value: KnowledgeLocation = {
    source_id: params.get("source_id")!,
    document_id: params.get("document_id")!,
    chunk_id: params.get("chunk_id")!,
    source_version: Number(params.get("source_version")),
    document_version: Number(params.get("document_version")),
  };
  return isKnowledgeLocation(value) ? value : null;
}

// Constructed only from server-validated citation metadata, never a model URL.
export function knowledgeLocationHref(
  value: KnowledgeLocation,
  sessionId?: string,
): string | null {
  if (!isKnowledgeLocation(value)) return null;
  const params = new URLSearchParams();
  for (const key of knowledgeLocationKeys) params.set(key, String(value[key]));
  if (isKnowledgeIdentity(sessionId)) params.set("return_session", sessionId);
  return `/knowledge?${params}`;
}
