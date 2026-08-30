import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FinancialEntry } from "../types/models";
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

describe("IncomePage", () => {
  beforeEach(() => {
    hooks.entries.mockReturnValue({
      data: { items: [entry], meta: { page: 1, pageSize: 20, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    hooks.stats.mockReturnValue({
      data: {
        currency: "CNY",
        dateFrom: "2026-08-01",
        dateTo: "2026-08-31",
        confirmedIncomeMinor: 128000,
        confirmedExpenseMinor: 28000,
        pendingIncomeMinor: 5000,
        pendingExpenseMinor: 1000,
        netCashFlowMinor: 100000,
        confirmedIncomeCount: 1,
        averageIncomeMinor: 128000,
        entryCount: 3,
      },
      isError: false,
      refetch: vi.fn(),
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders real currency-scoped statistics and the ledger hierarchy", () => {
    render(<IncomePage />);

    expect(screen.getByRole("heading", { name: "收入与支出" })).toBeTruthy();
    expect(screen.getByText("¥1,280.00")).toBeTruthy();
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
});
