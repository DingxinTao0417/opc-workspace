import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cancelKnowledgeIndexJob,
  createKnowledgeSource,
  deleteKnowledgeSource,
  downloadKnowledgeSourcesCSV,
  getKnowledgeSources,
  reindexKnowledgeSource,
  resetRuntimeConnection,
  retryKnowledgeIndexJob,
  searchKnowledge,
} from "./client";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function sourceFixture(version = 1) {
  return {
    id: "source-1",
    name: "guide.md",
    title: "Guide",
    source_type: "markdown",
    import_mode: "managed_copy",
    mime_type: "text/markdown",
    size_bytes: 128,
    content_sha256: "a".repeat(64),
    status: "ready",
    last_indexed_at: "2026-09-08T12:00:00Z",
    deleted_at: null,
    delete_reason: null,
    version,
    created_at: "2026-09-08T12:00:00Z",
    updated_at: "2026-09-08T12:00:00Z",
    document_id: "document-1",
    document_version: version,
    chunk_count: 2,
    latest_job: jobFixture(),
  };
}

function jobFixture(operation: "import" | "reindex" = "import") {
  return {
    id: "job-1",
    source_id: "source-1",
    operation,
    status: "succeeded",
    stage: "complete",
    progress: 100,
    attempt: 1,
    retry_of_job_id: null,
    error_code: null,
    cancel_requested: false,
    started_at: "2026-09-08T12:00:00Z",
    completed_at: "2026-09-08T12:00:00Z",
    created_at: "2026-09-08T12:00:00Z",
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("knowledge-base requests", () => {
  it("uploads a managed local copy and parses its index evidence", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        { data: { source: sourceFixture(), job: jobFixture() } },
        201,
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const file = new File(["# Local guide"], "guide.md", {
      type: "text/markdown",
    });

    const result = await createKnowledgeSource(file, "Local guide");

    expect(result.source).toMatchObject({
      id: "source-1",
      sourceType: "markdown",
      importMode: "managed_copy",
      documentVersion: 1,
      chunkCount: 2,
    });
    expect(result.job).toMatchObject({
      sourceId: "source-1",
      status: "succeeded",
      progress: 100,
    });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit,
    ];
    expect(url).toContain("/api/v1/knowledge/sources");
    expect(init.method).toBe("POST");
    expect(new Headers(init.headers).get("Idempotency-Key")).toMatch(
      /^[0-9a-f-]{36}$/,
    );
    expect(init.body).toBeInstanceOf(FormData);
    expect((init.body as FormData).get("file")).toBeInstanceOf(File);
    expect((init.body as FormData).get("title")).toBe("Local guide");
  });

  it("lists sources and sends version-bound lifecycle requests", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          data: [sourceFixture()],
          meta: { page: 1, page_size: 20, total: 1 },
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          data: {
            source: sourceFixture(2),
            job: jobFixture("reindex"),
          },
        }),
      )
      .mockResolvedValueOnce(jsonResponse({ data: { status: "deleted" } }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getKnowledgeSources()).resolves.toMatchObject({
      items: [{ name: "guide.md", status: "ready" }],
      meta: { total: 1 },
    });
    await expect(reindexKnowledgeSource("source-1", 1)).resolves.toMatchObject({
      source: { version: 2 },
    });
    await deleteKnowledgeSource("source-1", 2);

    const [, reindexInit] = fetchMock.mock.calls[1] as unknown as [
      string,
      RequestInit,
    ];
    expect(new Headers(reindexInit.headers).get("If-Match")).toBe('"1"');
    const [deleteURL, deleteInit] = fetchMock.mock.calls[2] as unknown as [
      string,
      RequestInit,
    ];
    expect(deleteURL).toContain("confirm=true");
    expect(new Headers(deleteInit.headers).get("If-Match")).toBe('"2"');
  });

  it("sends source filters and preserves rune-based highlight ranges", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({
        data: [
          {
            chunk_id: "chunk-1",
            document_id: "document-1",
            source_id: "source-1",
            source_name: "guide.md",
            source_type: "markdown",
            document_title: "Guide",
            document_version: 2,
            chunk_index: 0,
            start_char: 0,
            end_char: 12,
            start_line: 1,
            end_line: 2,
            start_page: 1,
            end_page: 1,
            excerpt: "客户发票已归档",
            highlights: [{ start: 2, end: 4 }],
            rank: -0.75,
          },
        ],
        meta: {
          query: "发票",
          result_count: 1,
          source_ids: ["source-1"],
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await searchKnowledge("发票", ["source-1"]);

    expect(result).toMatchObject({
      query: "发票",
      sourceIds: ["source-1"],
      items: [
        {
          excerpt: "客户发票已归档",
          highlights: [{ start: 2, end: 4 }],
          startLine: 1,
          startPage: 1,
          documentVersion: 2,
        },
      ],
    });
    const [, init] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit,
    ];
    expect(JSON.parse(String(init.body))).toEqual({
      query: "发票",
      source_ids: ["source-1"],
      limit: 20,
    });
  });

  it("imports PDF sources and still rejects unsupported extensions", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        {
          data: {
            source: {
              ...sourceFixture(),
              name: "manual.pdf",
              source_type: "pdf",
              mime_type: "application/pdf",
            },
            job: { ...jobFixture(), source_id: "source-1" },
          },
        },
        201,
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const file = new File(["%PDF-1.7 test"], "manual.pdf", {
      type: "application/pdf",
    });

    const result = await createKnowledgeSource(file);

    expect(result.source).toMatchObject({
      sourceType: "pdf",
      mimeType: "application/pdf",
    });
    const unsupported = new File(["docx"], "notes.docx");
    await expect(createKnowledgeSource(unsupported)).rejects.toMatchObject({
      code: "KNOWLEDGE_FORMAT_UNSUPPORTED",
    });
  });

  it("parses pdf page locations and rejects responses without them", async () => {
    const searchRow = {
      chunk_id: "chunk-1",
      document_id: "document-1",
      source_id: "source-1",
      source_name: "manual.pdf",
      source_type: "pdf",
      document_title: "Manual",
      document_version: 1,
      chunk_index: 0,
      start_char: 0,
      end_char: 12,
      start_line: 41,
      end_line: 43,
      start_page: 2,
      end_page: 3,
      excerpt: "安装步骤说明",
      highlights: [{ start: 0, end: 2 }],
      rank: -1.5,
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          data: [searchRow],
          meta: { query: "安装", result_count: 1, source_ids: [] },
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          data: [{ ...searchRow, start_page: undefined }],
          meta: { query: "安装", result_count: 1, source_ids: [] },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const result = await searchKnowledge("安装");
    expect(result.items[0]).toMatchObject({
      sourceType: "pdf",
      startPage: 2,
      endPage: 3,
    });

    await expect(searchKnowledge("安装")).rejects.toThrow();
  });

  it("cancels an active job and retries it against the latest source version", async () => {
    const cancelledJob = {
      ...jobFixture(),
      status: "cancelled",
      cancel_requested: true,
    };
    const retriedJob = {
      ...jobFixture("import"),
      id: "job-2",
      status: "queued",
      stage: "queued",
      progress: 0,
      attempt: 2,
      retry_of_job_id: "job-1",
      started_at: null,
      completed_at: null,
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: cancelledJob }))
      .mockResolvedValueOnce(
        jsonResponse({
          data: {
            source: {
              ...sourceFixture(3),
              status: "indexing",
              latest_job: retriedJob,
            },
            job: retriedJob,
          },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(cancelKnowledgeIndexJob("job-1")).resolves.toMatchObject({
      status: "cancelled",
      cancelRequested: true,
    });
    await expect(retryKnowledgeIndexJob("job-1", 2)).resolves.toMatchObject({
      source: { version: 3, status: "indexing" },
      job: { id: "job-2", attempt: 2, retryOfJobId: "job-1" },
    });
    const [cancelURL] = fetchMock.mock.calls[0] as unknown as [string];
    const [retryURL, retryInit] = fetchMock.mock.calls[1] as unknown as [
      string,
      RequestInit,
    ];
    expect(cancelURL).toContain("/job-1/cancel");
    expect(retryURL).toContain("/job-1/retry");
    expect(new Headers(retryInit.headers).get("If-Match")).toBe('"2"');
  });

  it("downloads the confirmed metadata-only source manifest", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response("id,name\nsource-1,guide.md\n", {
          status: 200,
          headers: {
            "Content-Type": "text/csv; charset=utf-8",
            "Content-Disposition":
              'attachment; filename="knowledge-sources-20260908.csv"',
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await downloadKnowledgeSourcesCSV();

    expect(result.fileName).toBe("knowledge-sources-20260908.csv");
    expect(result.blob.type).toBe("text/csv;charset=utf-8");
    const [url] = fetchMock.mock.calls[0] as unknown as [string];
    expect(url).toContain("/api/v1/knowledge/sources/export.csv?confirm=true");
  });
});
