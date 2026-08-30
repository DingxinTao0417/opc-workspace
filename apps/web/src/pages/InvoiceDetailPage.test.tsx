import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Invoice } from "../types/models";
import { InvoiceDetailPage } from "./InvoiceDetailPage";

const hooks = vi.hoisted(() => ({
  detail: vi.fn(),
  transition: {
    error: null as unknown,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  },
  remove: {
    error: null as unknown,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useInvoiceQuery: hooks.detail,
  useTransitionInvoice: () => hooks.transition,
  useDeleteInvoice: () => hooks.remove,
}));

vi.mock("../components/InvoiceFormModal", () => ({
  InvoiceFormModal: ({
    open,
    invoice,
    onVersionConflict,
  }: {
    open: boolean;
    invoice?: Invoice;
    onVersionConflict?: () => void;
  }) =>
    open ? (
      <div role="dialog">
        <span>编辑 {invoice?.invoiceNumber}</span>
        <button onClick={onVersionConflict} type="button">
          模拟编辑冲突
        </button>
      </div>
    ) : null,
}));

vi.mock("../components/InvoicePdfSection", () => ({
  InvoicePdfSection: ({ invoice }: { invoice: Invoice }) => (
    <div>PDF 区块 {invoice.invoiceNumber}</div>
  ),
}));

const paidInvoice: Invoice = {
  id: "invoice-1",
  invoiceNumber: "INV-202608-001",
  clientId: "client-1",
  clientName: "星河工作室",
  projectId: "project-1",
  projectName: "客户门户",
  amountMinor: 128045,
  currency: "CNY",
  status: "paid",
  issueDate: "2026-08-29",
  dueDate: "2026-09-28",
  paidDate: "2026-09-03",
  notes: "首付款",
  financialEntryId: "entry-1",
  version: 4,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-09-03T00:00:00Z",
};

const refetch = vi.fn();

function detailResult(
  overrides: Partial<{
    data: Invoice;
    error: unknown;
    isError: boolean;
    isPending: boolean;
    refetch: typeof refetch;
  }> = {},
) {
  return {
    data: paidInvoice,
    error: null,
    isError: false,
    isPending: false,
    refetch,
    ...overrides,
  };
}

function renderDetail(path = "/invoices/invoice-1") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<div>发票列表目标</div>} path="/invoices" />
        <Route element={<InvoiceDetailPage />} path="/invoices/:invoiceId" />
      </Routes>
    </MemoryRouter>,
  );
}

describe("InvoiceDetailPage", () => {
  beforeEach(() => {
    refetch.mockResolvedValue({ isSuccess: true, data: paidInvoice });
    hooks.transition.mutateAsync.mockResolvedValue(paidInvoice);
    hooks.remove.mutateAsync.mockResolvedValue(undefined);
    hooks.detail.mockReturnValue(detailResult());
    hooks.transition.error = null;
    hooks.transition.isPending = false;
    hooks.remove.error = null;
    hooks.remove.isPending = false;
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("reads only the routed invoice and renders every current field", () => {
    renderDetail();

    expect(hooks.detail).toHaveBeenCalledWith("invoice-1");
    expect(
      screen.getByRole("heading", { name: "INV-202608-001" }),
    ).toBeTruthy();
    expect(screen.getByText("¥1,280.45")).toBeTruthy();
    expect(screen.getByText("星河工作室")).toBeTruthy();
    expect(screen.getByRole("link", { name: "星河工作室" })).toHaveAttribute(
      "href",
      "/clients/client-1",
    );
    expect(screen.getByText("client-1")).toBeTruthy();
    expect(screen.getByText("客户门户")).toBeTruthy();
    expect(screen.getByRole("link", { name: "客户门户" })).toHaveAttribute(
      "href",
      "/projects/project-1",
    );
    expect(screen.getByText("project-1")).toBeTruthy();
    expect(screen.getByText("invoice-1")).toBeTruthy();
    expect(screen.getByText("2026-08-29")).toBeTruthy();
    expect(screen.getByText("2026-09-28")).toBeTruthy();
    expect(screen.getByText("2026-09-03")).toBeTruthy();
    expect(screen.getByText("entry-1")).toBeTruthy();
    expect(screen.getByText("首付款")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
    expect(screen.getAllByText("已付款").length).toBeGreaterThan(0);
    expect(screen.getByText("PDF 区块 INV-202608-001")).toBeTruthy();
  });

  it("renders explicit empty values for every nullable relation", () => {
    hooks.detail.mockReturnValue(
      detailResult({
        data: {
          ...paidInvoice,
          status: "draft",
          projectId: null,
          projectName: null,
          paidDate: null,
          financialEntryId: null,
          notes: "  ",
        },
      }),
    );

    renderDetail();

    expect(screen.getByText("未关联项目")).toBeTruthy();
    expect(screen.getByText("未设置项目关联")).toBeTruthy();
    expect(screen.getByText("尚未付款")).toBeTruthy();
    expect(screen.getByText("未关联收入记录")).toBeTruthy();
    expect(screen.getByText("未填写备注")).toBeTruthy();
  });

  it("renders a loading state without issuing a list query", () => {
    hooks.detail.mockReturnValue(
      detailResult({ data: undefined as never, isPending: true }),
    );

    renderDetail("/invoices/direct-refresh");

    expect(hooks.detail).toHaveBeenCalledWith("direct-refresh");
    expect(screen.getByLabelText("正在加载")).toBeTruthy();
  });

  it("distinguishes a missing invoice from a retryable service error", () => {
    hooks.detail.mockReturnValue(
      detailResult({
        data: undefined as never,
        error: new ApiError("missing", {
          code: "INVOICE_NOT_FOUND",
          status: 404,
        }),
        isError: true,
      }),
    );
    const view = renderDetail();

    expect(screen.getByText("发票不存在")).toBeTruthy();
    expect(screen.getByRole("link", { name: "返回发票列表" })).toHaveAttribute(
      "href",
      "/invoices",
    );

    hooks.detail.mockReturnValue(
      detailResult({
        data: undefined as never,
        error: new ApiError("offline", { status: 503 }),
        isError: true,
      }),
    );
    view.rerender(
      <MemoryRouter initialEntries={["/invoices/invoice-1"]}>
        <Routes>
          <Route element={<InvoiceDetailPage />} path="/invoices/:invoiceId" />
        </Routes>
      </MemoryRouter>,
    );

    expect(screen.getByText("发票详情不可用")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("uses the shared command with the observed version", async () => {
    const draft = { ...paidInvoice, status: "draft" as const, version: 7 };
    hooks.transition.mutateAsync.mockResolvedValueOnce({
      ...draft,
      status: "sent",
      version: 8,
    });
    hooks.detail.mockReturnValue(detailResult({ data: draft }));
    renderDetail();

    fireEvent.click(screen.getByRole("button", { name: "确认已发送" }));
    fireEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(hooks.transition.mutateAsync).toHaveBeenCalledWith({
      id: "invoice-1",
      input: { action: "mark_sent", expectedVersion: 7 },
    });
    expect(await screen.findByText("发票状态已更新为已发送。")).toBeTruthy();
  });

  it("refreshes the detail and prompts again after a version conflict", async () => {
    hooks.transition.mutateAsync.mockRejectedValueOnce(
      new ApiError("conflict", { code: "VERSION_CONFLICT", status: 409 }),
    );
    hooks.detail.mockReturnValue(
      detailResult({ data: { ...paidInvoice, status: "sent", version: 5 } }),
    );
    renderDetail();

    fireEvent.click(screen.getByRole("button", { name: "确认已查看" }));
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1));
    expect(
      await screen.findByText("发票已在其他窗口更新，详情已刷新，请重新操作。"),
    ).toBeTruthy();
  });

  it("does not misclassify another business 409 as a version conflict", async () => {
    hooks.transition.mutateAsync.mockRejectedValueOnce(
      new ApiError("invalid transition", {
        code: "INVALID_INVOICE_TRANSITION",
        status: 409,
      }),
    );
    hooks.detail.mockReturnValue(
      detailResult({ data: { ...paidInvoice, status: "sent", version: 5 } }),
    );
    renderDetail();

    fireEvent.click(screen.getByRole("button", { name: "确认已查看" }));
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    await waitFor(() =>
      expect(hooks.transition.mutateAsync).toHaveBeenCalled(),
    );
    expect(refetch).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog", { name: "确认客户已查看" })).toBeTruthy();
  });

  it("returns to the invoice list with replace after deleting a draft", async () => {
    hooks.detail.mockReturnValue(
      detailResult({ data: { ...paidInvoice, status: "draft" } }),
    );
    renderDetail();

    fireEvent.click(screen.getByRole("button", { name: "删除草稿" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    expect(await screen.findByText("发票列表目标")).toBeTruthy();
  });

  it("closes a stale edit and refreshes the latest detail", async () => {
    hooks.detail.mockReturnValue(
      detailResult({ data: { ...paidInvoice, status: "draft" } }),
    );
    renderDetail();

    fireEvent.click(screen.getByRole("button", { name: "编辑草稿" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "模拟编辑冲突" }));

    await waitFor(() => expect(refetch).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
