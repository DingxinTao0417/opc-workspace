import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getAiMessages, resetRuntimeConnection } from "./client";

function responseWithCitation(
  status = "validated",
  citations: unknown[] = [
    {
      chunk_id: "chunk-1",
      source_id: "source-1",
      source_name: "guide.md",
      source_type: "markdown",
      source_version: 2,
      document_id: "document-1",
      document_title: "Guide",
      document_version: 3,
      chunk_index: 0,
      start_char: 0,
      end_char: 20,
      start_line: 2,
      end_line: 4,
    },
  ],
) {
  return new Response(
    JSON.stringify({
      data: [
        {
          id: "message-1",
          session_id: "session-1",
          role: "assistant",
          status: "completed",
          content: "Grounded answer",
          reasoning: null,
          task_id: null,
          task_title_snapshot: null,
          context_provider: null,
          context_sources: [],
          context_knowledge: [],
          citation_status: status,
          citations,
          created_at: "2026-09-08T12:00:00Z",
        },
      ],
      meta: { has_more: false },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("AI citation response parsing", () => {
  it("accepts a validated server-built citation location", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => responseWithCitation()),
    );

    const result = await getAiMessages("session-1");

    expect(result.data[0]).toMatchObject({
      citation_status: "validated",
      citations: [
        {
          source_name: "guide.md",
          document_version: 3,
          start_line: 2,
          end_line: 4,
        },
      ],
    });
  });

  it("fails closed when citation status and items disagree", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => responseWithCitation("missing")),
    );

    await expect(getAiMessages("session-1")).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    } satisfies Partial<ApiError>);
  });
});
