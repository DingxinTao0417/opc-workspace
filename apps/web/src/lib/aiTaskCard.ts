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

// stripAiTaskBlock removes the suggestion block from the displayed reply.
export function stripAiTaskBlock(content: string): string {
  return content.replace(AI_TASK_BLOCK_PATTERN, "").trimEnd();
}
