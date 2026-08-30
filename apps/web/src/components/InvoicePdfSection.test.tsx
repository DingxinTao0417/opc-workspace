import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Invoice, InvoicePdfMetadata } from "../types/models";
import { InvoicePdfSection } from "./InvoicePdfSection";

const hooks = vi.hoisted(() => ({
  query: vi.fn(),
  generate: {
    isPending: false,
    mutateAsync: vi.fn(),
  },
  download: {
    isPending: false,
    mutateAsync: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useInvoicePdfQuery: hooks.query,
  useGenerateInvoicePdf: () => hooks.generate,
  useDownloadInvoicePdf: () => hooks.download,
}));

const invoice: Invoice = {
  id: "invoice-1",
  invoiceNumber: "INV-2026-001",
  clientId: "client-1",
  clientName: "星河工作室",
  projectId: null,
  projectName: null,
  amountMinor: 12800,
  currency: "CNY",
  status: "draft",
  issueDate: "2026-08-29",
  dueDate: "2026-09-29",
  paidDate: null,
  notes: "",
  financialEntryId: null,
  version: 3,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T00:00:00Z",
};

const metadata: InvoicePdfMetadata = {
  invoiceId: "invoice-1",
  fileName: "INV-2026-001.pdf",
  mimeType: "application/pdf",
  sizeBytes: 4096,
  sha256: "a".repeat(64),
  generatedFromVersion: 3,
  generatedAt: "2026-08-29T12:00:00Z",
  integrityStatus: "verified",
  integrityCheckedAt: "2026-08-29T12:00:01Z",
};

const refetch = vi.fn();

function queryResult(
  overrides: Partial<{
    data: InvoicePdfMetadata | null;
    error: unknown;
    isError: boolean;
    isFetching: boolean;
    isPending: boolean;
  }> = {},
) {
  return {
    data: null,
    error: null,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch,
    ...overrides,
  };
}

describe("InvoicePdfSection", () => {
  beforeEach(() => {
    hooks.query.mockReturnValue(queryResult());
    hooks.generate.isPending = false;
    hooks.generate.mutateAsync.mockResolvedValue(metadata);
    hooks.download.isPending = false;
    hooks.download.mutateAsync.mockResolvedValue({
      blob: new Blob(["%PDF-1.7"]),
      fileName: metadata.fileName,
    });
    refetch.mockResolvedValue({ data: null });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("allows a draft invoice to generate a local preview without changing status", async () => {
    render(<InvoicePdfSection invoice={invoice} />);

    expect(hooks.query).toHaveBeenCalledWith("invoice-1");
    expect(screen.getByText("尚未生成本地 PDF")).toBeTruthy();
    expect(screen.getByText(/不会发送或改变发票状态/)).toBeTruthy();
    expect(screen.getByText(/不代表税务电子发票/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "生成 PDF" }));
    expect(hooks.generate.mutateAsync).toHaveBeenCalledWith({
      id: "invoice-1",
      expectedVersion: 3,
    });
    expect(
      await screen.findByText(/已在本机生成；发票状态未改变/),
    ).toBeTruthy();
  });

  it("keeps an older verified PDF downloadable and recommends regeneration", async () => {
    hooks.query.mockReturnValue(
      queryResult({ data: { ...metadata, generatedFromVersion: 2 } }),
    );
    const createObjectURL = vi.fn(() => "blob:invoice-pdf");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);

    render(<InvoicePdfSection invoice={invoice} />);

    expect(screen.getByText(/当前 PDF 基于旧版本/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "下载 PDF" }));

    await waitFor(() => expect(click).toHaveBeenCalledTimes(1));
    expect(hooks.download.mutateAsync).toHaveBeenCalledWith({
      id: "invoice-1",
      name: "INV-2026-001.pdf",
    });
    expect(createObjectURL).toHaveBeenCalled();
  });

  it.each([
    ["missing", "本地 PDF 文件已缺失"],
    ["mismatch", "本地 PDF 完整性校验失败"],
  ] as const)(
    "blocks download when integrity is %s and keeps regeneration available",
    (integrityStatus, message) => {
      hooks.query.mockReturnValue(
        queryResult({ data: { ...metadata, integrityStatus } }),
      );

      render(<InvoicePdfSection invoice={invoice} />);

      expect(screen.getByText(new RegExp(message))).toBeTruthy();
      expect(screen.getByRole("button", { name: "下载 PDF" })).toBeDisabled();
      expect(
        screen.getByRole("button", { name: "重新生成 PDF" }),
      ).toBeEnabled();
    },
  );

  it("shows loading and retryable metadata errors before enabling commands", () => {
    hooks.query.mockReturnValue(
      queryResult({ data: undefined as never, isPending: true }),
    );
    const view = render(<InvoicePdfSection invoice={invoice} />);

    expect(screen.getByText("正在检查本地 PDF…")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "生成 PDF" })).toBeNull();

    hooks.query.mockReturnValue(
      queryResult({
        data: undefined as never,
        error: new ApiError("storage unavailable", {
          code: "INVOICE_PDF_STORAGE_UNAVAILABLE",
          status: 503,
        }),
        isError: true,
      }),
    );
    view.rerender(<InvoicePdfSection invoice={invoice} />);
    expect(screen.getByText("PDF 状态不可用")).toBeTruthy();
    expect(screen.getByText(/本地 PDF 存储暂不可用/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("refreshes the invoice after a generation version conflict", async () => {
    hooks.generate.mutateAsync.mockRejectedValueOnce(
      new ApiError("conflict", { code: "VERSION_CONFLICT", status: 409 }),
    );
    const onInvoiceConflict = vi.fn(async () => undefined);
    render(
      <InvoicePdfSection
        invoice={invoice}
        onInvoiceConflict={onInvoiceConflict}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "生成 PDF" }));

    await waitFor(() => expect(onInvoiceConflict).toHaveBeenCalledTimes(1));
    expect(screen.getByText(/发票版本已变化/)).toBeTruthy();
  });

  it("serializes generation and download operations", () => {
    hooks.query.mockReturnValue(queryResult({ data: metadata }));
    hooks.generate.isPending = true;
    render(<InvoicePdfSection invoice={invoice} />);

    expect(screen.getByRole("button", { name: "正在生成…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "下载 PDF" })).toBeDisabled();
  });
});
