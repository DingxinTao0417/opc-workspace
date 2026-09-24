import { isKnowledgeIdentity } from "./knowledgeLocation";

export interface KnowledgeIndexLocation {
  source: string;
  job: string;
}

export const knowledgeIndexLocationKeys = ["source", "job"] as const;

export function parseKnowledgeIndexLocation(
  params: URLSearchParams,
): KnowledgeIndexLocation | null {
  if (
    [...params.keys()].some(
      (key) => !(knowledgeIndexLocationKeys as readonly string[]).includes(key),
    ) ||
    knowledgeIndexLocationKeys.some((key) => params.getAll(key).length !== 1)
  ) {
    return null;
  }
  const source = params.get("source");
  const job = params.get("job");
  return isKnowledgeIdentity(source) && isKnowledgeIdentity(job)
    ? { source, job }
    : null;
}

// Constructed from the strictly validated Agent Inbox response, never a model URL.
export function knowledgeIndexLocationHref(
  source: string,
  job: string,
): string | null {
  if (!isKnowledgeIdentity(source) || !isKnowledgeIdentity(job)) return null;
  const params = new URLSearchParams({ source, job });
  return `/knowledge?${params}`;
}
