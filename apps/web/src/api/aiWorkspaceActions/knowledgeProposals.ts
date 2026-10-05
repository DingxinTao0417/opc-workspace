import {
  canonicalUuid,
  isPositiveInteger,
  nonemptyString,
  object,
  sha256,
} from "./primitives";

export function validKnowledgeProposal(
  value: Record<string, unknown>,
): boolean {
  const action = value.action;
  const preview = value.preview;
  if (!object(action) || !object(preview)) return false;
  const name = String(action.action);
  if (
    ![
      "knowledge_source.create",
      "knowledge_source.reindex",
      "knowledge_source.delete",
      "knowledge_index_job.retry",
      "knowledge_index_job.cancel",
    ].includes(name)
  )
    return false;
  const knowledge = preview.knowledge;
  if (!object(knowledge)) return false;
  const before = preview.before;
  const after = preview.after;
  if (!object(before) || !object(after)) return false;
  const changes = action.changes;
  if (!object(changes)) return false;
  if (name === "knowledge_source.create") {
    // Creation carries the text the user supplied in this conversation; the
    // card must prove the excerpt really belongs to that exact content.
    if (
      action.knowledge_source_id !== undefined ||
      action.knowledge_index_job_id !== undefined ||
      action.expected_version !== undefined ||
      Object.keys(changes).some(
        (key) => !["name", "title", "content"].includes(key),
      ) ||
      typeof changes.name !== "string" ||
      !/\.(?:txt|md|markdown)$/i.test(changes.name) ||
      typeof changes.content !== "string" ||
      changes.content.length === 0 ||
      new TextEncoder().encode(changes.content).length > 24000 ||
      (changes.title !== undefined && typeof changes.title !== "string")
    )
      return false;
    const content = changes.content;
    const excerpt = knowledge.proposed_excerpt;
    const truncated = knowledge.proposed_excerpt_truncated;
    if (
      knowledge.name !== changes.name ||
      !["text", "markdown"].includes(String(knowledge.source_type)) ||
      knowledge.status !== "pending" ||
      knowledge.version !== 0 ||
      knowledge.next_status !== "indexing" ||
      knowledge.next_version !== 1 ||
      knowledge.new_job_operation !== "import" ||
      knowledge.chunk_count !== 0 ||
      knowledge.document_count !== 0 ||
      knowledge.content_included !== false ||
      !isPositiveInteger(knowledge.proposed_content_bytes) ||
      knowledge.proposed_content_bytes !==
        new TextEncoder().encode(content).length ||
      typeof knowledge.proposed_content_sha256 !== "string" ||
      !sha256.test(knowledge.proposed_content_sha256) ||
      typeof excerpt !== "string" ||
      typeof truncated !== "boolean" ||
      (truncated ? !content.startsWith(excerpt) : excerpt !== content) ||
      Object.keys(before).length !== 0 ||
      after.name !== knowledge.name ||
      after.source_type !== knowledge.source_type ||
      after.status !== "indexing" ||
      after.version !== 1 ||
      after.size_bytes !== knowledge.proposed_content_bytes
    )
      return false;
    if (
      value.result_id !== null &&
      !canonicalUuid.test(String(value.result_id))
    )
      return false;
    if (value.result_version !== null && value.result_version !== 1)
      return false;
    if (
      value.status === "confirmed" &&
      (value.result_id === null || value.result_version !== 1)
    )
      return false;
    return true;
  }
  const isJob = name.startsWith("knowledge_index_job.");
  const isDelete = name === "knowledge_source.delete";
  const jobTarget = action.knowledge_index_job_id;
  const sourceTarget = action.knowledge_source_id;
  if (isJob) {
    if (
      typeof jobTarget !== "string" ||
      !canonicalUuid.test(jobTarget) ||
      sourceTarget !== undefined
    )
      return false;
  } else if (
    typeof sourceTarget !== "string" ||
    !canonicalUuid.test(sourceTarget) ||
    jobTarget !== undefined
  )
    return false;
  if (!isPositiveInteger(action.expected_version)) return false;
  if (
    typeof knowledge.source_id !== "string" ||
    !canonicalUuid.test(knowledge.source_id) ||
    (isJob
      ? knowledge.knowledge_index_job_id !== jobTarget
      : sourceTarget !== knowledge.source_id)
  )
    return false;
  if (
    !nonemptyString(knowledge.name) ||
    Array.from(String(knowledge.name)).length > 255 ||
    !["text", "markdown", "pdf"].includes(String(knowledge.source_type)) ||
    !["pending", "indexing", "ready", "stale", "missing", "failed"].includes(
      String(knowledge.status),
    ) ||
    knowledge.content_included !== false ||
    !isPositiveInteger(knowledge.version) ||
    knowledge.version !== action.expected_version
  )
    return false;
  for (const key of ["chunk_count", "document_count"]) {
    const count = knowledge[key];
    if (!Number.isSafeInteger(count) || (count as number) < 0) return false;
  }
  if (
    before.status !== knowledge.status ||
    before.version !== knowledge.version ||
    before.chunks !== knowledge.chunk_count ||
    before.documents !== knowledge.document_count
  )
    return false;
  if (
    isDelete
      ? after.chunks !== 0 || after.documents !== 0
      : after.chunks !== before.chunks || after.documents !== before.documents
  )
    return false;
  if (isDelete) {
    if (
      Object.keys(changes).some((key) => key !== "reason") ||
      (changes.reason !== undefined &&
        (typeof changes.reason !== "string" ||
          changes.reason.trim().length === 0 ||
          Array.from(changes.reason).length > 500))
    )
      return false;
    if (
      knowledge.next_status !== "deleted" ||
      after.status !== "deleted" ||
      after.chunks !== 0 ||
      after.documents !== 0 ||
      knowledge.next_version !== knowledge.version + 1 ||
      after.version !== knowledge.next_version
    )
      return false;
  } else if (Object.keys(changes).length !== 0) {
    return false;
  }
  if (name === "knowledge_source.reindex") {
    if (
      knowledge.next_status !== "indexing" ||
      knowledge.new_job_operation !== "reindex" ||
      after.status !== "indexing" ||
      after.version !== knowledge.version + 1
    )
      return false;
    if (
      knowledge.document_version !== undefined &&
      !isPositiveInteger(knowledge.document_version)
    )
      return false;
  }
  if (isJob) {
    const job = knowledge.job;
    if (
      !object(job) ||
      job.id !== jobTarget ||
      !["import", "reindex"].includes(String(job.operation)) ||
      !isPositiveInteger(job.attempt) ||
      before.job_attempt !== job.attempt
    )
      return false;
    if (name === "knowledge_index_job.retry") {
      if (
        !["failed", "cancelled"].includes(String(job.status)) ||
        knowledge.next_status !== "indexing" ||
        knowledge.new_job_operation !== job.operation ||
        after.job_status !== "queued" ||
        after.version !== knowledge.version + 1
      )
        return false;
      if (job.error_code !== undefined && !nonemptyString(job.error_code))
        return false;
    } else {
      if (
        !["queued", "running"].includes(String(job.status)) ||
        !["ready", "failed"].includes(String(knowledge.next_status)) ||
        after.job_status !== "cancelled" ||
        before.job_status !== job.status ||
        after.status !== knowledge.next_status
      )
        return false;
    }
  }
  if (value.result_id !== null) {
    if (
      typeof value.result_id !== "string" ||
      !canonicalUuid.test(value.result_id)
    )
      return false;
    // Deletion and cancellation keep the addressed record identity; reindex
    // and retry receipts identify the new index attempt instead.
    if (isDelete && value.result_id !== knowledge.source_id) return false;
    if (name === "knowledge_index_job.cancel" && value.result_id !== jobTarget)
      return false;
  }
  if (value.result_version !== null && !isPositiveInteger(value.result_version))
    return false;
  if (
    value.status === "confirmed" &&
    (value.result_id === null || value.result_version === null)
  )
    return false;
  return true;
}
