import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FinancialEntry, IncomeStatsParams } from "../types/models";
import { IncomePage } from "./IncomePage";

const entry: FinancialEntry = {
  id: "entry-1",
  type: "income",
  amountMinor: 128000,
  currency: "CNY",
  occurredOn: "2026-08-29",
  status: "confirmed",
  category: "项目回款",
  clientId: "client-1",
  clientName: "星河工作室",
  projectId: "project-1",
  projectName: "客户门户",
  invoiceId: null,
  invoiceNumber: null,
  notes: "首付款",
  createdByActorId: "actor-1",
  voidedAt: null,
  voidReason: null,
  version: 2,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T01:00:00Z",
};

const invoiceLinkedEntry: FinancialEntry = {
  ...entry,
  id: "entry-invoice-1",
  category: "发票回款",
  invoiceId: "invoice-1",
  invoiceNumber: "INV-202608-001",
  notes: "发票付款自动生成",
};

const hooks = vi.hoisted(() => ({
  entries: vi.fn(),
  stats: vi.fn(),
  exportMutation: {
    error: null,
    isError: false,
    isPending: false,
    mutate: vi.fn(),
  },
  voidMutation: {
    error: null,
    isError: false,
    isPending: false,
    mutate: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useFinancialEntriesQuery: hooks.entries,
  useIncomeStatsQuery: hooks.stats,
  useExportFinancialEntries: () => hooks.exportMutation,
  useVoidFinancialEntry: () => hooks.voidMutation,
}));

vi.mock("../components/FinancialEntryFormModal", () => ({
  FinancialEntryFormModal: ({ open }: { open: boolean }) =>
    open ? <div role="dialog">财务记录表单</div> : null,
}));

function statsResult(input: IncomeStatsParams, confirmedIncomeMinor: number) {
  return {
    data: {
      ...input,
      confirmedIncomeMinor,
      confirmedExpenseMinor: 28000,
      pendingIncomeMinor: 5000,
      pendingExpenseMinor: 1000,
      netCashFlowMinor: confirmedIncomeMinor - 28000,
      confirmedIncomeCount: input.dateFrom.endsWith("-01-01") ? 8 : 1,
      averageIncomeMinor: 128000,
      entryCount: 3,
    },
    isError: false,
    isFetching: false,
    isPending: false,
    isPlaceholderData: false,
    refetch: vi.fn(),
  };
}

describe("IncomePage", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-29T12:00:00"));
    hooks.entries.mockReturnValue({
      data: { items: [entry], meta: { page: 1, pageSize: 20, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    hooks.stats.mockImplementation((input: IncomeStatsParams) =>
      statsResult(input, input.dateFrom === "2026-01-01" ? 888000 : 128000),
    );
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.useRealTimers();
  });

  it("renders real currency-scoped statistics and the ledger hierarchy", () => {
    render(<IncomePage />);

    expect(screen.getByRole("heading", { name: "收入与支出" })).toBeTruthy();
    expect(screen.getByText("¥1,280.00")).toBeTruthy();
    expect(screen.getByText("¥8,880.00")).toBeTruthy();
    expect(screen.getByText("截至 2026-08-29 · 8 笔")).toBeTruthy();
    expect(screen.getByText("¥280.00")).toBeTruthy();
    expect(screen.getByText("项目回款")).toBeTruthy();
    expect(screen.getByText("星河工作室")).toBeTruthy();
    expect(screen.getByText("客户门户")).toBeTruthy();
    expect(screen.getByText("+¥1,280.00")).toBeTruthy();
  });

  it("passes type, status, and currency filters to the local query", () => {
    render(<IncomePage />);

    fireEvent.change(screen.getByLabelText("币种筛选"), {
      target: { value: "USD" },
    });
    fireEvent.change(screen.getByLabelText("收支类型"), {
      target: { value: "expense" },
    });
    fireEvent.change(screen.getByLabelText("记录状态"), {
      target: { value: "voided" },
    });

    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({
        currency: "USD",
        type: "expense",
        status: "voided",
        includeVoided: true,
        page: 1,
      }),
    );
    expect(hooks.stats).toHaveBeenLastCalledWith(
      expect.objectContaining({ currency: "USD" }),
    );
    expect(hooks.stats).toHaveBeenCalledWith({
      currency: "USD",
      dateFrom: "2026-01-01",
      dateTo: "2026-08-29",
    });
  });

  it("clamps an out-of-range ledger page after a settled response shrinks", () => {
    let total = 41;
    hooks.entries.mockImplementation((input: { page?: number }) => {
      const queryPage = input.page ?? 1;
      return {
        data: {
          items: queryPage > Math.ceil(total / 20) ? [] : [entry],
          meta: { page: queryPage, pageSize: 20, total },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isPlaceholderData: false,
        isSuccess: true,
        refetch: vi.fn(),
      };
    });
    const view = render(<IncomePage />);

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
    );

    hooks.entries.mockClear();
    total = 1;
    view.rerender(<IncomePage />);

    expect(hooks.entries).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1 }),
    );
  });

  it("does not clamp the ledger page from placeholder data", () => {
    let total = 41;
    let placeholder = false;
    hooks.entries.mockImplementation((input: { page?: number }) => {
      const queryPage = input.page ?? 1;
      return {
        data: {
          items: queryPage > Math.ceil(total / 20) ? [] : [entry],
          meta: { page: queryPage, pageSize: 20, total },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isPlaceholderData: placeholder,
        isSuccess: true,
        refetch: vi.fn(),
      };
    });
    const view = render(<IncomePage />);
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    hooks.entries.mockClear();
    total = 1;
    placeholder = true;
    view.rerender(<IncomePage />);

    const placeholderPages = hooks.entries.mock.calls.map(
      ([input]) => (input as { page?: number }).page,
    );
    expect(placeholderPages.length).toBeGreaterThan(0);
    expect(placeholderPages.every((queryPage) => queryPage === 2)).toBe(true);

    hooks.entries.mockClear();
    placeholder = false;
    view.rerender(<IncomePage />);
    expect(hooks.entries).toHaveBeenCalledWith(
      expect.objectContaining({ page: 1 }),
    );
  });

  it("falls back to the current local month when the month control is cleared", () => {
    render(<IncomePage />);

    fireEvent.change(screen.getByLabelText("月份"), {
      target: { value: "" },
    });

    expect(screen.getByLabelText("月份")).toHaveValue("2026-08");
    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-08-01",
        dateTo: "2026-08-31",
      }),
    );
    expect(hooks.stats).toHaveBeenCalledWith({
      currency: "CNY",
      dateFrom: "2026-08-01",
      dateTo: "2026-08-31",
    });
    expect(
      hooks.stats.mock.calls.some((call: unknown[]) => {
        const input = call[0] as IncomeStatsParams;
        return input.dateFrom.includes("NaN") || input.dateTo.includes("NaN");
      }),
    ).toBe(false);
  });

  it("follows the new local month and year-to-date range at midnight", () => {
    vi.setSystemTime(new Date(2026, 11, 31, 23, 59, 59));
    render(<IncomePage />);

    expect(screen.getByLabelText("月份")).toHaveValue("2026-12");
    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-12-01",
        dateTo: "2026-12-31",
      }),
    );

    act(() => vi.advanceTimersByTime(1_002));

    expect(screen.getByLabelText("月份")).toHaveValue("2027-01");
    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2027-01-01",
        dateTo: "2027-01-31",
      }),
    );
    expect(hooks.stats).toHaveBeenCalledWith({
      currency: "CNY",
      dateFrom: "2027-01-01",
      dateTo: "2027-01-01",
    });
  });

  it("keeps a manually selected month while refreshing year-to-date bounds", () => {
    vi.setSystemTime(new Date(2026, 11, 31, 23, 59, 59));
    render(<IncomePage />);
    fireEvent.change(screen.getByLabelText("月份"), {
      target: { value: "2026-11" },
    });

    act(() => vi.advanceTimersByTime(1_002));

    expect(screen.getByLabelText("月份")).toHaveValue("2026-11");
    expect(hooks.entries).toHaveBeenLastCalledWith(
      expect.objectContaining({
        dateFrom: "2026-11-01",
        dateTo: "2026-11-30",
      }),
    );
    expect(hooks.stats).toHaveBeenCalledWith({
      currency: "CNY",
      dateFrom: "2027-01-01",
      dateTo: "2027-01-01",
    });
  });

  it("does not relabel placeholder statistics with a newly selected currency", () => {
    hooks.stats.mockImplementation((input: IncomeStatsParams) => {
      if (input.currency === "USD") {
        return {
          ...statsResult(
            { ...input, currency: "CNY" },
            input.dateFrom.endsWith("-01-01") ? 888000 : 128000,
          ),
          isFetching: true,
          isPlaceholderData: true,
        };
      }
      return statsResult(
        input,
        input.dateFrom.endsWith("-01-01") ? 888000 : 128000,
      );
    });
    render(<IncomePage />);

    fireEvent.change(screen.getByLabelText("币种筛选"), {
      target: { value: "USD" },
    });

    const monthlyCard = screen.getByText("本月已确认收入").closest("article");
    const yearlyCard = screen.getByText("本年度已确认收入").closest("article");
    expect(monthlyCard).not.toBeNull();
    expect(yearlyCard).not.toBeNull();
    expect(within(monthlyCard!).getByText("—")).toBeTruthy();
    expect(within(yearlyCard!).getByText("—")).toBeTruthy();
    expect(screen.queryByText("$1,280.00")).toBeNull();
    expect(screen.queryByText("$8,880.00")).toBeNull();
  });

  it("rejects a settled statistics response for another range", () => {
    hooks.stats.mockImplementation((input: IncomeStatsParams) => {
      if (input.dateFrom === "2026-08-01") {
        return statsResult(
          { ...input, dateFrom: "2026-07-01", dateTo: "2026-07-31" },
          999900,
        );
      }
      return statsResult(input, 888000);
    });

    render(<IncomePage />);

    expect(
      screen.getByText("统计响应与当前范围不一致，已停止显示可能过期的金额。"),
    ).toBeTruthy();
    const monthlyCard = screen.getByText("本月已确认收入").closest("article");
    expect(monthlyCard).not.toBeNull();
    expect(within(monthlyCard!).getByText("—")).toBeTruthy();
    expect(screen.queryByText("¥9,999.00")).toBeNull();
  });

  it("exports the visible range and requires an explicit void reason", () => {
    render(<IncomePage />);

    fireEvent.click(screen.getByRole("button", { name: "导出 CSV" }));
    expect(hooks.exportMutation.mutate).toHaveBeenCalledWith(
      expect.objectContaining({ currency: "CNY" }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "打开 项目回款 操作" }));
    fireEvent.click(screen.getByRole("button", { name: "作废" }));
    const confirmButton = screen.getByRole("button", { name: "确认作废" });
    expect(confirmButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText("作废原因"), {
      target: { value: "重复录入" },
    });
    fireEvent.click(confirmButton);

    expect(hooks.voidMutation.mutate).toHaveBeenCalledWith(
      {
        id: "entry-1",
        input: { reason: "重复录入", expectedVersion: 2 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("opens a real record form instead of a placeholder action", () => {
    render(<IncomePage />);
    fireEvent.click(screen.getByRole("button", { name: "新建记录" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("keeps invoice-generated income read-only in the row actions", () => {
    hooks.entries.mockReturnValue({
      data: {
        items: [invoiceLinkedEntry],
        meta: { page: 1, pageSize: 20, total: 1 },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });

    render(<IncomePage />);

    expect(screen.getByText("发票同步")).toHaveAttribute(
      "title",
      "由发票付款自动生成，不可单独编辑或作废",
    );
    expect(
      screen.queryByRole("button", { name: "打开 发票回款 操作" }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "编辑" })).toBeNull();
    expect(screen.queryByRole("button", { name: "作废" })).toBeNull();
  });
});
