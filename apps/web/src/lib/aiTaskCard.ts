export interface AiTaskSuggestion {
  title: string;
  description?: string;
  due?: string;
}

// Some compatible/local models repeat the opening marker instead of emitting
// the slash-prefixed closing marker. Treat that common shape as a recoverable
// formatting error so the user still gets the confirmation gate.
const AI_TASK_BLOCK_PATTERN =
  /\[opc:task\]\s*([\s\S]*?)\s*(?:\[\/opc:task\]|\[opc:task\])/i;

// parseAiTaskSuggestion extracts the first well-formed task suggestion block
// from an assistant reply. The block is model output and always treated as an
// untrusted preview: malformed JSON or a missing/oversized title yields null
// so the display layer can suppress the protocol text and report a natural-
// language failure without creating anything.
export function parseAiTaskSuggestion(
  content: string,
): AiTaskSuggestion | null {
  const match = AI_TASK_BLOCK_PATTERN.exec(content);
  if (!match) return null;
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(match[1]) as Record<string, unknown>;
  } catch {
    return null;
  }
  const rawTitle = typeof parsed.title === "string" ? parsed.title.trim() : "";
  if (!rawTitle || rawTitle.length > 200) return null;
  const description =
    typeof parsed.description === "string" && parsed.description.trim()
      ? parsed.description.trim()
      : undefined;
  const due =
    typeof parsed.due === "string" && /^\d{4}-\d{2}-\d{2}$/.test(parsed.due)
      ? parsed.due
      : undefined;
  return { title: rawTitle, description, due };
}

export interface AiMemorySuggestion {
  content: string;
  proposalId?: string;
}

const AI_MEMORY_BLOCK_PATTERN =
  /\[opc:memory\]\s*([\s\S]*?)\s*(?:\[\/opc:memory\]|\[opc:memory\])/i;

// The harness consumes the model's own [opc:selfcheck] verdict and never
// persists it, but streamed deltas reach the UI before that stripping, so the
// display layer drops it too (ADR-006).
const AI_SELF_CHECK_BLOCK_PATTERN =
  /\[opc:selfcheck\][\s\S]*?(\[\/opc:selfcheck\]|$)/;
const AI_CITATION_BLOCK_PATTERN =
  /\[opc:citations\][\s\S]*?(?:\[\/opc:citations\]|\[opc:citations\])/gi;

// parseAiMemorySuggestion extracts the first well-formed memory suggestion
// block from an assistant reply. Like task blocks it is untrusted model
// output: malformed JSON or missing/oversized content yields null.
export function parseAiMemorySuggestion(
  content: string,
): AiMemorySuggestion | null {
  const match = AI_MEMORY_BLOCK_PATTERN.exec(content);
  if (!match) return null;
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(match[1]) as Record<string, unknown>;
  } catch {
    return null;
  }
  const rawContent =
    typeof parsed.content === "string" ? parsed.content.trim() : "";
  if (!rawContent || rawContent.length > 500) return null;
  let proposalId: string | undefined;
  if (parsed.proposal_id !== undefined) {
    if (
      typeof parsed.proposal_id !== "string" ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
        parsed.proposal_id.trim(),
      )
    ) {
      return null;
    }
    proposalId = parsed.proposal_id.trim();
  }
  return { content: rawContent, ...(proposalId ? { proposalId } : {}) };
}

// stripAiSelfCheckBlock removes the internal self-check verdict from the
// displayed text (used on streamed deltas; persisted content is already
// clean).
export function stripAiSelfCheckBlock(content: string): string {
  return content.replace(AI_SELF_CHECK_BLOCK_PATTERN, "").trimEnd();
}

// stripAiTaskBlock removes both suggestion blocks (task + memory) from the
// displayed reply.
export function stripAiTaskBlock(content: string): string {
  return content
    .replace(AI_TASK_BLOCK_PATTERN, "")
    .replace(AI_MEMORY_BLOCK_PATTERN, "")
    .replace(AI_CITATION_BLOCK_PATTERN, "")
    .replace(/\[opc:(?:task|memory|citations)\][\s\S]*$/i, "")
    .trimEnd();
}
