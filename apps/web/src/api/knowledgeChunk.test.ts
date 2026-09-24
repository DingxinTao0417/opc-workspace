import { afterEach, describe, expect, it, vi } from "vitest";
import { resetRuntimeConnection } from "./client";
import { getKnowledgeChunk } from "./knowledgeChunk";

const location = {
  source_id: "018f0000-0000-7000-8000-000000006101",
  document_id: "018f0000-0000-7000-8000-000000006102",
  chunk_id: "018f0000-0000-7000-8000-000000006103",
  source_version: 2,
  document_version: 1,
};
const chunk = {
  ...location,
  source_name: "guide.pdf",
  document_title: "Guide",
  source_type: "pdf",
  content: "证据🧭",
  chunk_index: 0,
  start_char: 5,
  end_char: 8,
  start_line: 2,
  end_line: 2,
  start_page: 3,
  end_page: 3,
};

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("exact knowledge chunk reads", () => {
  it("sends only identities/versions with cancellation and returns bounded Unicode evidence", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            data: { ...chunk, original_content: "must not expose" },
          }),
        ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();
    expect(await getKnowledgeChunk(location, controller.signal)).toEqual(chunk);
    const [url, options] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit,
    ];
    const parsed = new URL(url, "http://localhost");
    expect(parsed.pathname).toBe(
      `/api/v1/knowledge/chunks/${location.chunk_id}`,
    );
    expect(Object.fromEntries(parsed.searchParams)).toEqual({
      source_id: location.source_id,
      document_id: location.document_id,
      source_version: "2",
      document_version: "1",
    });
    expect(options.method ?? "GET").toBe("GET");
    expect(options.signal).toBeInstanceOf(AbortSignal);
  });

  it("cancels an in-flight read when the preview is closed", async () => {
    const controller = new AbortController();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_url: string, options: RequestInit) =>
          new Promise<Response>((_resolve, reject) => {
            options.signal!.addEventListener(
              "abort",
              () => reject(new DOMException("cancelled", "AbortError")),
              { once: true },
            );
            controller.abort();
            expect(options.signal!.aborted).toBe(true);
          }),
      ),
    );
    await expect(
      getKnowledgeChunk(location, controller.signal),
    ).rejects.toMatchObject({ code: "TIMEOUT" });
  });

  it.each([
    { source_id: "../private" },
    { document_id: "other-document" },
    { chunk_id: "https://example.com" },
    { source_version: 0 },
    { document_version: Number.MAX_SAFE_INTEGER + 1 },
  ])("rejects invalid locations before making requests: %j", async (change) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getKnowledgeChunk({ ...location, ...change }),
    ).rejects.toMatchObject({ code: "INVALID_KNOWLEDGE_LOCATION" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    { source_version: 3 },
    { document_version: 2 },
    { chunk_id: "another-chunk" },
    { source_id: "another-source" },
    { document_id: "another-document" },
    { source_type: "html" },
    { source_type: ["pdf"] },
    { content: "" },
    { content: "a".repeat(4097), start_char: 0, end_char: 4097 },
    { end_char: 10 },
    { end_line: 1 },
    { start_page: 0 },
    { end_page: 2 },
    { end_page: undefined },
  ])(
    "fails closed on a substituted or malformed response: %j",
    async (change) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(
          async () =>
            new Response(JSON.stringify({ data: { ...chunk, ...change } })),
        ),
      );
      await expect(getKnowledgeChunk(location)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );
});
