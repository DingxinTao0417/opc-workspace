import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Invoice, InvoiceStatus } from "../types/models";
import { InvoicesPage } from "./InvoicesPage";

const hooks = vi.hoisted(() => ({
  query: vi.fn(),
  transition: {
    error: null,
    isPending: false,
    mutateAsync: vi.fn(),
    reset: vi.fn(),
  },
  remove: {
    error: null,
    isPending: false,
    mutateAsync: vi.fn(),
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

function invoice(status: InvoiceStatus, id: string = status): Invoice {
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

function list(items: Invoice[], total = items.length, page = 1) {
  return {
    data: { items, meta: { page, pageSize: 20, total } },
    isError: false,
    isFetching: false,
    isPending: false,
    isPlaceholderData: false,
    isSuccess: true,
    refetch: vi.fn(),
  };
}

function renderInvoices() {
  return render(
    <MemoryRouter initialEntries={["/invoices"]}>
      <Routes>
        <Route element={<InvoicesPage />} path="/invoices" />
        <Route element={<div>发票详情路由</div>} path="/invoices/:invoiceId" />
      </Routes>
    </MemoryRouter>,
  );
}

describe("InvoicesPage", () => {
  beforeEach(() => {
    hooks.transition.mutateAsync.mockResolvedValue(invoice("sent", "draft"));
    hooks.remove.mutateAsync.mockResolvedValue(undefined);
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
    renderInvoices();

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
    renderInvoices();

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

  it("clamps an out-of-range page after a settled response shrinks", async () => {
    let total = 41;
    hooks.query.mockImplementation(
      (input: { page?: number; pageSize?: number }) => {
        if (input.pageSize === 1) return list([], 0);
        const queryPage = input.page ?? 1;
        return list([invoice("sent", `page-${queryPage}`)], total, queryPage);
      },
    );
    const view = renderInvoices();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("第 2 / 3 页")).toBeTruthy();

    hooks.query.mockClear();
    total = 1;
    view.rerender(
      <MemoryRouter initialEntries={["/invoices"]}>
        <Routes>
          <Route element={<InvoicesPage />} path="/invoices" />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.query).toHaveBeenCalledWith(
        expect.objectContaining({ page: 1, pageSize: 20 }),
      ),
    );
  });

  it("keeps the requested page while a shrunken response is placeholder data", async () => {
    let total = 41;
    let placeholder = false;
    hooks.query.mockImplementation(
      (input: { page?: number; pageSize?: number }) => {
        if (input.pageSize === 1) return list([], 0);
        const queryPage = input.page ?? 1;
        return {
          ...list([invoice("sent", `page-${queryPage}`)], total, queryPage),
          isPlaceholderData: placeholder,
        };
      },
    );
    const view = renderInvoices();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    hooks.query.mockClear();
    total = 1;
    placeholder = true;
    view.rerender(
      <MemoryRouter initialEntries={["/invoices"]}>
        <Routes>
          <Route element={<InvoicesPage />} path="/invoices" />
        </Routes>
      </MemoryRouter>,
    );

    const placeholderMainCalls = hooks.query.mock.calls
      .map(([input]) => input as { page?: number; pageSize?: number })
      .filter((input) => input.pageSize === 20);
    expect(placeholderMainCalls.length).toBeGreaterThan(0);
    expect(placeholderMainCalls.every((input) => input.page === 2)).toBe(true);

    hooks.query.mockClear();
    placeholder = false;
    view.rerender(
      <MemoryRouter initialEntries={["/invoices"]}>
        <Routes>
          <Route element={<InvoicesPage />} path="/invoices" />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.query).toHaveBeenCalledWith(
        expect.objectContaining({ page: 1, pageSize: 20 }),
      ),
    );
  });

  it("confirms sending with the observed version", async () => {
    renderInvoices();
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-DRAFT 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认已发送" }));
    expect(screen.getByText(/不会替你发送邮件或生成 PDF/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(hooks.transition.mutateAsync).toHaveBeenCalledWith({
      id: "draft",
      input: { action: "mark_sent", expectedVersion: 3 },
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "确认已发送" })).toBeNull(),
    );
  });

  it("requires and submits a paid date for viewed invoices", async () => {
    hooks.transition.mutateAsync.mockResolvedValueOnce(
      invoice("paid", "viewed"),
    );
    renderInvoices();
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-VIEWED 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "登记付款" }));
    const dialog = screen.getByRole("dialog", { name: "登记付款" });
    const amountSummary = within(dialog).getByText("付款金额").parentElement;
    expect(within(dialog).getByText("INV-VIEWED")).toBeTruthy();
    expect(within(dialog).getByText("星河工作室")).toBeTruthy();
    expect(amountSummary).toHaveTextContent("¥1,280.45 · CNY");
    expect(within(dialog).getByText("将创建一条已确认收入记录")).toBeTruthy();
    const paymentDate = screen.getByLabelText("付款日期");
    const now = new Date();
    const localToday = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
    expect(paymentDate).toHaveAttribute("max", localToday);
    fireEvent.change(screen.getByLabelText("付款日期"), {
      target: { value: "2026-09-03" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认付款" }));

    expect(hooks.transition.mutateAsync).toHaveBeenCalledWith({
      id: "viewed",
      input: {
        action: "mark_paid",
        expectedVersion: 3,
        paidDate: "2026-09-03",
      },
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "登记付款" })).toBeNull(),
    );
  });

  it("deletes only after explicit draft confirmation", async () => {
    renderInvoices();
    fireEvent.click(
      screen.getByRole("button", { name: "打开 INV-DRAFT 操作" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

    expect(hooks.remove.mutateAsync).toHaveBeenCalledWith({
      id: "draft",
      expectedVersion: 3,
    });
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "删除发票草稿" })).toBeNull(),
    );
  });

  it("renders the filtered empty state without demo totals", () => {
    hooks.query.mockReturnValue(list([]));
    renderInvoices();
    fireEvent.change(screen.getByLabelText("发票状态"), {
      target: { value: "overdue" },
    });

    expect(screen.getByText("没有匹配的发票")).toBeTruthy();
    expect(screen.getByRole("button", { name: "清除筛选" })).toBeTruthy();
  });

  it("links an invoice number to its refreshable detail route", () => {
    renderInvoices();

    fireEvent.click(screen.getByRole("link", { name: "查看发票 INV-DRAFT" }));

    expect(screen.getByText("发票详情路由")).toBeTruthy();
  });
});
