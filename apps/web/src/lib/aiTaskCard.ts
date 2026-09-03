export interface AiTaskSuggestion {
  title: string;
  description?: string;
  due?: string;
}

const AI_TASK_BLOCK_PATTERN = /\[opc:task\]([\s\S]*?)\[\/opc:task\]/;

// parseAiTaskSuggestion extracts the first well-formed task suggestion block
// from an assistant reply. The block is model output and always treated as an
// untrusted preview: malformed JSON or a missing/oversized title yields null
// and the reply stays plain text.
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
}

const AI_MEMORY_BLOCK_PATTERN = /\[opc:memory\]([\s\S]*?)\[\/opc:memory\]/;

// The harness consumes the model's own [opc:selfcheck] verdict and never
// persists it, but streamed deltas reach the UI before that stripping, so the
// display layer drops it too (ADR-006).
const AI_SELF_CHECK_BLOCK_PATTERN =
  /\[opc:selfcheck\][\s\S]*?(\[\/opc:selfcheck\]|$)/;

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
  return { content: rawContent };
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
    .trimEnd();
}
