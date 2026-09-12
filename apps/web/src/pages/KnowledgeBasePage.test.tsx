import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cancelKnowledgeIndexJob,
  createKnowledgeSource,
  deleteKnowledgeSource,
  downloadKnowledgeSourcesCSV,
  getKnowledgeSources,
  reindexKnowledgeSource,
  retryKnowledgeIndexJob,
  searchKnowledge,
} from "../api/client";
import type { KnowledgeSource } from "../types/models";
import { KnowledgeBasePage } from "./KnowledgeBasePage";

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  createKnowledgeSource: vi.fn(),
  cancelKnowledgeIndexJob: vi.fn(),
  deleteKnowledgeSource: vi.fn(),
  downloadKnowledgeSourcesCSV: vi.fn(),
  getKnowledgeSources: vi.fn(),
  reindexKnowledgeSource: vi.fn(),
  retryKnowledgeIndexJob: vi.fn(),
  searchKnowledge: vi.fn(),
}));

const source: KnowledgeSource = {
  id: "source-1",
  name: "billing-guide.md",
  title: "Billing guide",
  sourceType: "markdown",
  importMode: "managed_copy",
  mimeType: "text/markdown",
  sizeBytes: 2048,
  contentSha256: "a".repeat(64),
  status: "ready",
  lastIndexedAt: "2026-09-08T12:00:00Z",
  deletedAt: null,
  deleteReason: null,
  version: 1,
  createdAt: "2026-09-08T12:00:00Z",
  updatedAt: "2026-09-08T12:00:00Z",
  documentId: "document-1",
  documentVersion: 1,
  chunkCount: 3,
  latestJob: null,
};

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <KnowledgeBasePage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.mocked(getKnowledgeSources).mockResolvedValue({
    items: [source],
    meta: { page: 1, pageSize: 100, total: 1 },
  });
  vi.mocked(searchKnowledge).mockResolvedValue({
    query: "发票",
    sourceIds: [source.id],
    items: [
      {
        chunkId: "chunk-1",
        documentId: "document-1",
        sourceId: source.id,
        sourceName: source.name,
        sourceType: "markdown",
        documentTitle: "Billing guide",
        documentVersion: 1,
        chunkIndex: 0,
        startChar: 0,
        endChar: 20,
        startLine: 2,
        endLine: 3,
        startPage: 1,
        endPage: 1,
        excerpt: "客户发票在付款后归档",
        highlights: [{ start: 2, end: 4 }],
        rank: -1,
      },
    ],
  });
  vi.mocked(createKnowledgeSource).mockResolvedValue({
    source,
    job: {
      id: "job-1",
      sourceId: source.id,
      operation: "import",
      status: "succeeded",
      stage: "complete",
      progress: 100,
      attempt: 1,
      retryOfJobId: null,
      errorCode: null,
      cancelRequested: false,
      startedAt: "2026-09-08T12:00:00Z",
      completedAt: "2026-09-08T12:00:00Z",
      createdAt: "2026-09-08T12:00:00Z",
    },
  });
  vi.mocked(deleteKnowledgeSource).mockResolvedValue();
  vi.mocked(downloadKnowledgeSourcesCSV).mockResolvedValue({
    blob: new Blob(["id,name\nsource-1,billing-guide.md\n"], {
      type: "text/csv",
    }),
    fileName: "knowledge-sources-20260908.csv",
  });
  vi.mocked(cancelKnowledgeIndexJob).mockResolvedValue({
    id: "job-active",
    sourceId: source.id,
    operation: "import",
    status: "cancelled",
    stage: "complete",
    progress: 40,
    attempt: 1,
    retryOfJobId: null,
    errorCode: null,
    cancelRequested: true,
    startedAt: "2026-09-08T12:00:00Z",
    completedAt: "2026-09-08T12:01:00Z",
    createdAt: "2026-09-08T12:00:00Z",
  });
  vi.mocked(reindexKnowledgeSource).mockResolvedValue({
    source: { ...source, version: 2, documentVersion: 2 },
    job: {
      id: "job-2",
      sourceId: source.id,
      operation: "reindex",
      status: "succeeded",
      stage: "complete",
      progress: 100,
      attempt: 1,
      retryOfJobId: null,
      errorCode: null,
      cancelRequested: false,
      startedAt: "2026-09-08T12:00:00Z",
      completedAt: "2026-09-08T12:00:00Z",
      createdAt: "2026-09-08T12:00:00Z",
    },
  });
  vi.mocked(retryKnowledgeIndexJob).mockResolvedValue({
    source: { ...source, status: "indexing", version: 2 },
    job: {
      id: "job-retry",
      sourceId: source.id,
      operation: "import",
      status: "queued",
      stage: "queued",
      progress: 0,
      attempt: 2,
      retryOfJobId: "job-failed",
      errorCode: null,
      cancelRequested: false,
      startedAt: null,
      completedAt: null,
      createdAt: "2026-09-08T12:02:00Z",
    },
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("KnowledgeBasePage", () => {
  it("filters deterministic local search and renders source positions with safe highlights", async () => {
    renderPage();

    expect(await screen.findByText("billing-guide.md")).toBeVisible();
    expect(screen.getByText("3")).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "筛选来源 billing-guide.md" }),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "搜索知识库" }), {
      target: { value: "发票" },
    });
    fireEvent.click(screen.getByRole("button", { name: "搜索" }));

    await waitFor(() =>
      expect(searchKnowledge).toHaveBeenCalledWith("发票", [source.id]),
    );
    expect(await screen.findByText("发票")).toBeVisible();
    expect(screen.getByText("第 2–3 行 · 文档 v1")).toBeVisible();
    expect(screen.getByText("发票").tagName).toBe("MARK");
    expect(
      screen.queryByText("当前不会把检索内容发送给 AI 或任何远程服务。"),
    ).not.toBeInTheDocument();
  });

  it("shows page locations and a type label for pdf sources", async () => {
    vi.mocked(getKnowledgeSources).mockResolvedValue({
      items: [
        {
          ...source,
          name: "manual.pdf",
          sourceType: "pdf",
          mimeType: "application/pdf",
        },
      ],
      meta: { page: 1, pageSize: 100, total: 1 },
    });
    vi.mocked(searchKnowledge).mockResolvedValue({
      query: "安装",
      sourceIds: [source.id],
      items: [
        {
          chunkId: "chunk-2",
          documentId: "document-1",
          sourceId: source.id,
          sourceName: "manual.pdf",
          sourceType: "pdf",
          documentTitle: "Manual",
          documentVersion: 1,
          chunkIndex: 0,
          startChar: 0,
          endChar: 20,
          startLine: 41,
          endLine: 43,
          startPage: 2,
          endPage: 3,
          excerpt: "安装步骤说明",
          highlights: [{ start: 0, end: 2 }],
          rank: -1,
        },
      ],
    });
    renderPage();

    expect(await screen.findByText("manual.pdf")).toBeVisible();
    expect(screen.getByText(/2\.0 KiB · PDF · 3 个分段/)).toBeVisible();
    fireEvent.change(screen.getByRole("textbox", { name: "搜索知识库" }), {
      target: { value: "安装" },
    });
    fireEvent.click(screen.getByRole("button", { name: "搜索" }));

    expect(
      await screen.findByText("第 2–3 页 · 第 41–43 行 · 文档 v1"),
    ).toBeVisible();
  });

  it("imports an explicitly selected file and confirms complete local deletion", async () => {
    renderPage();
    await screen.findByText("billing-guide.md");

    const file = new File(["# local"], "local.md", { type: "text/markdown" });
    fireEvent.change(screen.getByLabelText("选择知识库文件"), {
      target: { files: [file] },
    });
    await waitFor(() =>
      expect(createKnowledgeSource).toHaveBeenCalledWith(file),
    );
    expect(
      await screen.findByText(/已将 billing-guide.md 加入本地索引队列/),
    ).toBeVisible();

    fireEvent.click(
      screen.getByRole("button", { name: "删除来源 billing-guide.md" }),
    );
    expect(
      screen.getByRole("dialog", { name: "删除知识来源？" }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "删除来源" }));
    await waitFor(() =>
      expect(deleteKnowledgeSource).toHaveBeenCalledWith(
        source.id,
        source.version,
      ),
    );
    expect(await screen.findByText(/已删除 billing-guide.md/)).toBeVisible();
  });

  it("shows actor progress and lets the user cancel the active job", async () => {
    vi.mocked(getKnowledgeSources).mockResolvedValue({
      items: [
        {
          ...source,
          status: "indexing",
          latestJob: {
            id: "job-active",
            sourceId: source.id,
            operation: "import",
            status: "running",
            stage: "chunking",
            progress: 40,
            attempt: 1,
            retryOfJobId: null,
            errorCode: null,
            cancelRequested: false,
            startedAt: "2026-09-08T12:00:00Z",
            completedAt: null,
            createdAt: "2026-09-08T12:00:00Z",
          },
        },
      ],
      meta: { page: 1, pageSize: 100, total: 1 },
    });
    renderPage();

    expect(await screen.findByText("索引中 40%")).toBeVisible();
    expect(screen.getByText("生成分段 · attempt 1")).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "取消索引 billing-guide.md" }),
    );
    await waitFor(() =>
      expect(cancelKnowledgeIndexJob).toHaveBeenCalledWith("job-active"),
    );
  });

  it("retries a failed job with the current source version", async () => {
    vi.mocked(getKnowledgeSources).mockResolvedValue({
      items: [
        {
          ...source,
          status: "failed",
          latestJob: {
            id: "job-failed",
            sourceId: source.id,
            operation: "import",
            status: "failed",
            stage: "complete",
            progress: 40,
            attempt: 1,
            retryOfJobId: null,
            errorCode: "KNOWLEDGE_INDEX_INTERRUPTED",
            cancelRequested: false,
            startedAt: "2026-09-08T12:00:00Z",
            completedAt: "2026-09-08T12:01:00Z",
            createdAt: "2026-09-08T12:00:00Z",
          },
        },
      ],
      meta: { page: 1, pageSize: 100, total: 1 },
    });
    renderPage();

    await screen.findByText("索引失败");
    expect(
      screen.getByText("KNOWLEDGE_INDEX_INTERRUPTED · attempt 1"),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "重试索引 billing-guide.md" }),
    );
    await waitFor(() =>
      expect(retryKnowledgeIndexJob).toHaveBeenCalledWith(
        "job-failed",
        source.version,
      ),
    );
  });

  it("exports only the source and index-status manifest", async () => {
    const createObjectURL = vi.fn(() => "blob:knowledge-manifest");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    renderPage();
    await screen.findByText("billing-guide.md");

    fireEvent.click(screen.getByRole("button", { name: "导出清单" }));

    await waitFor(() => expect(downloadKnowledgeSourcesCSV).toHaveBeenCalled());
    expect(createObjectURL).toHaveBeenCalled();
    expect(click).toHaveBeenCalled();
    expect(
      await screen.findByText(/knowledge-sources-20260908.csv/),
    ).toBeVisible();
  });
});
