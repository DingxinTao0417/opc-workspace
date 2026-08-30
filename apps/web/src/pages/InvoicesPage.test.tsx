import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Invoice, InvoiceStatus } from "../types/models";
import { InvoicesPage } from "./InvoicesPage";

const hooks = vi.hoisted(() => ({
  query: vi.fn(),
  transition: {
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  remove: {
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useInvoicesQuery: hooks.query,
  useTransitionInvoice: () => hooks.transition,
  useDeleteInvoice: () => hooks.remove,
}));

vi.mock("../components/InvoiceFormModal", () => ({
  InvoiceFormModal: ({
    open,
    invoice,
  }: {
    open: boolean;
    invoice?: Invoice;
  }) =>
    open ? (
      <div role="dialog">
        {invoice ? `编辑 ${invoice.invoiceNumber}` : "新建发票"}
      </div>
    ) : null,
}));

function invoice(status: InvoiceStatus, id = status): Invoice {
  return {
    id,
    invoiceNumber: `INV-${id.toUpperCase()}`,
    clientId: "client-1",
    clientName: "星河工作室",
    projectId: "project-1",
    projectName: "客户门户",
    amountMinor: 128045,
    currency: "CNY",
    status,
    issueDate: "2026-08-29",
    dueDate: "2026-09-28",
    paidDate: status === "paid" ? "2026-09-02" : null,
    notes: "",
    financialEntryId: status === "paid" ? "entry-1" : null,
    version: 3,
    createdAt: "2026-08-29T00:00:00Z",
    updatedAt: "2026-08-29T01:00:00Z",
  };
}

function list(items: Invoice[], total = items.length) {
  return {
    data: { items, meta: { page: 1, pageSize: 20, total } },
    isError: false,
    isFetching: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  };
}

describe("InvoicesPage", () => {
  beforeEach(() => {
    hooks.query.mockImplementation(
      (input: { pageSize?: number; status?: InvoiceStatus }) => {
        if (input.pageSize === 1) {
          const totals: Record<InvoiceStatus, number> = {
            draft: 0,
            sent: 2,
            viewed: 3,
            paid: 4,
            overdue: 1,
          };
          return list([], input.status ? totals[input.status] : 0);
        }
        return list([
          invoice("draft"),
          invoice("sent"),
          invoice("viewed"),
          invoice("overdue"),
          invoice("paid"),
        ]);
      },
    );
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders the compact invoice table and real statuses", () => {
    render(<InvoicesPage />);

    expect(screen.getByRole("heading", { name: "发票" })).toBeTruthy();
    expect(screen.getByText("INV-DRAFT")).toBeTruthy();
    expect(screen.getAllByText("星河工作室")).toHaveLength(5);
    expect(screen.getAllByText("¥1,280.45")).toHaveLength(5);
    expect(screen.getAllByText("草稿").length).toBeGreaterThan(0);
    expect(screen.getAllByText("已付款").length).toBeGreaterThan(0);
    expect(screen.getByText("只读")).toBeTruthy();
    expect(screen.getAllByText("5 张").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("4 张")).toBeTruthy();
    expect(screen.getByText("1 张")).toBeTruthy();
  });

  it("passes search, status, and currency filters to the query", () => {
    render(<InvoicesPage />);

    fireEvent.change(screen.getByLabelText("搜索发票"), {
      target: { value: "INV-001" },
    });
    fireEvent.change(screen.getByLabelText("发票状态"), {
      target: { value: "overdue" },
    });
    fireEvent.change(screen.getByLabelText("发票币种"), {
      target: { value: "USD" },
    });

    expect(hooks.query).toHaveBeenCalledWith(
      expect.objectContaining({
        q: "INV-001",
        status: "overdue",
        currency: "USD",
        page: 1,
        pageSize: 20,
      }),
    );
  });

  it("confirms sending with the observed version", () => {
    render(<InvoicesPage />);
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-DRAFT 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认已发送" }));
    expect(screen.getByText(/不会替你发送邮件或生成 PDF/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(hooks.transition.mutate).toHaveBeenCalledWith(
      {
        id: "draft",
        input: { action: "mark_sent", expectedVersion: 3 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("requires and submits a paid date for viewed invoices", () => {
    render(<InvoicesPage />);
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-VIEWED 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "登记付款" }));
    fireEvent.change(screen.getByLabelText("付款日期"), {
      target: { value: "2026-09-03" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认付款" }));

    expect(hooks.transition.mutate).toHaveBeenCalledWith(
      {
        id: "viewed",
        input: {
          action: "mark_paid",
          expectedVersion: 3,
          paidDate: "2026-09-03",
        },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("deletes only after explicit draft confirmation", () => {
    render(<InvoicesPage />);
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-DRAFT 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(hooks.remove.mutate).toHaveBeenCalledWith(
      { id: "draft", expectedVersion: 3 },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("renders the filtered empty state without demo totals", () => {
    hooks.query.mockReturnValue(list([]));
    render(<InvoicesPage />);
    fireEvent.change(screen.getByLabelText("发票状态"), {
      target: { value: "overdue" },
    });

    expect(screen.getByText("没有匹配的发票")).toBeTruthy();
    expect(screen.getByRole("button", { name: "清除筛选" })).toBeTruthy();
  });
});
