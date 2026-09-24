import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AiInvoicePdfResult } from "./AiInvoicePdfResult";
import { ApiError } from "../api/client";
import type { AiInvoicePdfResult as PdfResult } from "../api/aiInvoiceActions";

const mocks = vi.hoisted(() => ({ download: vi.fn() }));
vi.mock("../api/client", async () => ({
  ...(await vi.importActual("../api/client")),
  downloadInvoicePdf: mocks.download,
}));
const result: PdfResult = {
  asset_id: "00000000-0000-4000-8000-000000000001",
  invoice_id: "00000000-0000-4000-8000-000000000002",
  file_name: "invoice.pdf",
  mime_type: "application/pdf",
  size_bytes: 1234,
  sha256: "a".repeat(64),
  generated_from_version: 3,
  generated_at: "2026-09-18T12:00:00Z",
  integrity_status: "verified",
  integrity_checked_at: "2026-09-18T12:00:00Z",
};
beforeEach(() => {
  mocks.download.mockReset();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function urlMocks() {
  const create = vi.fn(() => "blob:test");
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = create;
      static revokeObjectURL = vi.fn();
    },
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});
  return { create, click };
}
describe("historical invoice PDF result", () => {
  it("only downloads on a human click and reports handoff rather than saved-file success", async () => {
    const { create, click } = urlMocks();
    mocks.download.mockResolvedValue({
      blob: new Blob(["%PDF"]),
      fileName: "invoice.pdf",
    });
    render(<AiInvoicePdfResult result={result} />);
    expect(mocks.download).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "下载本次 PDF" }));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "请在下载列表确认保存结果",
    );
    expect(mocks.download).toHaveBeenCalledWith(
      result.invoice_id,
      result.file_name,
      {
        assetId: result.asset_id,
        sha256: result.sha256,
        sizeBytes: result.size_bytes,
      },
      expect.any(AbortSignal),
    );
    expect(create).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
  });
  it("explains replacement without silently regenerating and allows an explicit download retry", async () => {
    const { create } = urlMocks();
    mocks.download.mockRejectedValue(
      new ApiError("changed", { code: "INVOICE_PDF_CHANGED" }),
    );
    render(<AiInvoicePdfResult result={result} />);
    fireEvent.click(screen.getByRole("button", { name: "下载本次 PDF" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "不能从旧卡下载另一份文件",
    );
    expect(create).not.toHaveBeenCalled();
    expect(mocks.download).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "下载本次 PDF" })).toBeEnabled();
  });
  it("aborts on unmount and ignores late success without starting a download", async () => {
    const { create } = urlMocks();
    let resolve!: (v: unknown) => void;
    mocks.download.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const view = render(<AiInvoicePdfResult result={result} />);
    fireEvent.click(screen.getByRole("button", { name: "下载本次 PDF" }));
    const signal = mocks.download.mock.calls[0][3] as AbortSignal;
    view.unmount();
    expect(signal.aborted).toBe(true);
    resolve({ blob: new Blob(["%PDF"]), fileName: "invoice.pdf" });
    await waitFor(() => expect(create).not.toHaveBeenCalled());
  });
});
