import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  downloadInvoicePdf,
  generateInvoicePdf,
  getInvoicePdf,
  normalizeInvoicePdfMetadata,
  resetRuntimeConnection,
} from "./client";

const metadataPayload = {
  invoice_id: "invoice-1",
  file_name: "INV-2026-001.pdf",
  mime_type: "application/pdf",
  size_bytes: 4096,
  sha256: "a".repeat(64),
  generated_from_version: 3,
  generated_at: "2026-08-29T12:00:00Z",
  integrity_status: "verified",
  integrity_checked_at: "2026-08-29T12:00:01Z",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("invoice PDF requests", () => {
  it("strictly normalizes PDF metadata and integrity facts", () => {
    expect(normalizeInvoicePdfMetadata(metadataPayload)).toEqual({
      invoiceId: "invoice-1",
      fileName: "INV-2026-001.pdf",
      mimeType: "application/pdf",
      sizeBytes: 4096,
      sha256: "a".repeat(64),
      generatedFromVersion: 3,
      generatedAt: "2026-08-29T12:00:00Z",
      integrityStatus: "verified",
      integrityCheckedAt: "2026-08-29T12:00:01Z",
    });

    expect(() =>
      normalizeInvoicePdfMetadata({ ...metadataPayload, extra: true }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInvoicePdfMetadata({
        ...metadataPayload,
        integrity_status: "unknown",
      }),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInvoicePdfMetadata({ ...metadataPayload, sha256: "bad" }),
    ).toThrow(ApiError);
  });

  it("loads metadata and maps a missing local asset to an empty state", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({ data: { ...metadataPayload, invoice_id: "invoice/1" } }),
      )
      .mockResolvedValueOnce(
        jsonResponse(
          { code: "INVOICE_PDF_NOT_FOUND", message: "not found" },
          404,
        ),
      )
      .mockResolvedValueOnce(
        jsonResponse(
          { code: "INVOICE_NOT_FOUND", message: "invoice not found" },
          404,
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getInvoicePdf("invoice/1")).resolves.toMatchObject({
      invoiceId: "invoice/1",
      integrityStatus: "verified",
    });
    await expect(getInvoicePdf("invoice-1")).resolves.toBeNull();
    await expect(getInvoicePdf("missing-invoice")).rejects.toMatchObject({
      code: "INVOICE_NOT_FOUND",
      status: 404,
    });
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/invoices/invoice%2F1/pdf",
    );
  });

  it("generates with optimistic concurrency and idempotency headers", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: metadataPayload }, 201),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      generateInvoicePdf("invoice-1", 3, "pdf-attempt-1"),
    ).resolves.toMatchObject({ generatedFromVersion: 3 });

    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/invoices/invoice-1/generate-pdf",
    );
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers);
    expect(headers.get("If-Match")).toBe('"3"');
    expect(headers.get("Idempotency-Key")).toBe("pdf-attempt-1");
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined();
  });

  it("downloads only non-empty PDF responses with a safe filename", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response("%PDF-1.7\nlocal invoice", {
          headers: {
            "Content-Type": "application/pdf",
            "Content-Disposition": 'attachment; filename="invoice/unsafe.pdf"',
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await downloadInvoicePdf("invoice/1", "fallback.pdf");

    expect(result.blob.size).toBeGreaterThan(0);
    expect(result.fileName).toBe("invoice_unsafe.pdf");
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/invoices/invoice%2F1/pdf/download",
    );
    expect(new Headers(fetchMock.mock.calls[0][1]?.headers).get("Accept")).toBe(
      "application/pdf",
    );
  });

  it("rejects a download that is not a PDF", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response("not a pdf", {
            headers: { "Content-Type": "text/plain" },
          }),
      ),
    );

    await expect(
      downloadInvoicePdf("invoice-1", "fallback.pdf"),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
});
