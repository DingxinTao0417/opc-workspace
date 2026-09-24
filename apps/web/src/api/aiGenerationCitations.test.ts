import { afterEach, describe, expect, it, vi } from "vitest";
import { streamAiChat } from "./ai";
import { getAiGeneration } from "./aiActions";
import { resetRuntimeConnection } from "./client";

const citation = {
  chunk_id: "018f0000-0000-7000-8000-000000000001",
  source_id: "018f0000-0000-7000-8000-000000000002",
  document_id: "018f0000-0000-7000-8000-000000000003",
  source_type: "pdf",
  source_name: "私密资料.pdf",
  document_title: "原始文档",
  source_version: 2,
  document_version: 3,
  chunk_index: 1,
  start_char: 10,
  end_char: 30,
  start_line: 2,
  end_line: 4,
  start_page: 3,
  end_page: 3,
};

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

async function parse(via: string, extra: Record<string, unknown>) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const payload = {
        generation_id: "g",
        id: "g",
        session_id: "s",
        provider_id: "p",
        status: "completed",
        persist: false,
        content: "",
        reasoning: "",
        ...extra,
      };
      return new Response(
        via === "SSE"
          ? `event: done\ndata: ${JSON.stringify(payload)}\n\n`
          : JSON.stringify({ data: payload }),
      );
    }),
  );
  if (via === "GET") return (await getAiGeneration("g")).citationEvidence;
  const events: import("../types/models").AiChatStreamEvent[] = [];
  await streamAiChat({
    providerId: "p",
    message: "question",
    onEvent: (event) => events.push(event),
  });
  const done = events.find((event) => event.type === "done");
  expect(done).toBeDefined();
  return done?.citationEvidence;
}

describe.each(["SSE", "GET"])("%s citation metadata", (via) => {
  it("retains only server identity and PDF coordinates, never an injected content field", async () => {
    const result = await parse(via, {
      citation_status: "validated",
      citations: [{ ...citation, content: "raw secret" }],
    });
    expect(result).toEqual({ status: "validated", items: [citation] });
    expect(JSON.stringify(result)).not.toContain("raw secret");
  });
  it.each(["not_requested", "no_evidence", "missing", "invalid"])(
    "preserves %s without inventing sources",
    async (status) => {
      expect(
        await parse(via, { citation_status: status, citations: [] }),
      ).toEqual({ status, items: [] });
    },
  );
  it("treats absent metadata as unavailable, not not_requested", async () => {
    expect(await parse(via, {})).toBeUndefined();
  });
  it("rejects coordinates or versions outside exact integer range", async () => {
    await expect(
      parse(via, {
        citation_status: "validated",
        citations: [
          { ...citation, document_version: Number.MAX_SAFE_INTEGER + 1 },
        ],
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
  it.each([
    { citation_status: "validated" },
    { citations: [] },
    { citation_status: null, citations: [] },
    { citation_status: "validated", citations: [] },
    { citation_status: "missing", citations: [citation] },
    { citation_status: "validated", citations: [citation, citation] },
    {
      citation_status: "validated",
      citations: [{ ...citation, chunk_id: "forged" }],
    },
    {
      citation_status: "validated",
      citations: [{ ...citation, source_version: 0 }],
    },
    {
      citation_status: "validated",
      citations: [{ ...citation, start_page: 0 }],
    },
    {
      citation_status: "validated",
      citations: [{ ...citation, source_name: "大".repeat(6000) }],
    },
  ])("rejects malformed metadata %#", async (bad) => {
    await expect(parse(via, bad)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
});

it("does not attach citation results to an active generation", async () => {
  await expect(
    parse("GET", {
      status: "streaming",
      citation_status: "validated",
      citations: [citation],
    }),
  ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
});
