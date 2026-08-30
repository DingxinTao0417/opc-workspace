import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FinancialEntry } from "../types/models";
import {
  FinancialEntryFormModal,
  parseFinancialAmountMinor,
} from "./FinancialEntryFormModal";

const mutations = vi.hoisted(() => ({
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useCreateFinancialEntry: () => mutations.create,
  useUpdateFinancialEntry: () => mutations.update,
}));

vi.mock("./ClientSelect", () => ({
  ClientSelect: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (value: string) => void;
  }) => (
    <select
      aria-label="客户"
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">不关联客户</option>
      <option value="client-1">星河工作室</option>
    </select>
  ),
}));

vi.mock("./ProjectSelect", () => ({
  ProjectSelect: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (value: string) => void;
  }) => (
    <select
      aria-label="项目"
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">不关联项目</option>
      <option value="project-1">客户门户</option>
    </select>
  ),
}));

const entry: FinancialEntry = {
  id: "entry-1",
  type: "income",
  amountMinor: 128045,
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
  version: 4,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T01:00:00Z",
};

describe("FinancialEntryFormModal", () => {
  beforeEach(() => {
    mutations.create.mutate.mockClear();
    mutations.update.mutate.mockClear();
  });

  afterEach(cleanup);

  it("parses two-decimal amounts without floating point rounding", () => {
    expect(parseFinancialAmountMinor("123.45")).toBe(12345);
    expect(parseFinancialAmountMinor("1.2")).toBe(120);
    expect(parseFinancialAmountMinor("0")).toBeNull();
    expect(parseFinancialAmountMinor("1.234")).toBeNull();
  });

  it("creates a linked expense with exact minor units", () => {
    render(
      <FinancialEntryFormModal initialType="expense" onClose={vi.fn()} open />,
    );

    fireEvent.change(screen.getByLabelText("金额"), {
      target: { value: "88.50" },
    });
    fireEvent.change(screen.getByLabelText("分类"), {
      target: { value: "软件订阅" },
    });
    fireEvent.change(screen.getByLabelText("客户"), {
      target: { value: "client-1" },
    });
    fireEvent.change(screen.getByLabelText("项目"), {
      target: { value: "project-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建记录" }));

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "expense",
        amountMinor: 8850,
        currency: "CNY",
        category: "软件订阅",
        clientId: "client-1",
        projectId: "project-1",
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("updates with the observed version", () => {
    render(<FinancialEntryFormModal entry={entry} onClose={vi.fn()} open />);

    expect(screen.getByLabelText("金额")).toHaveValue("1280.45");
    fireEvent.change(screen.getByLabelText("分类"), {
      target: { value: "项目尾款" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(mutations.update.mutate).toHaveBeenCalledWith(
      {
        id: entry.id,
        input: expect.objectContaining({
          amountMinor: 128045,
          category: "项目尾款",
          expectedVersion: 4,
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("blocks malformed amounts before calling the API", () => {
    render(<FinancialEntryFormModal onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("金额"), {
      target: { value: "12.345" },
    });
    fireEvent.change(screen.getByLabelText("分类"), {
      target: { value: "项目回款" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建记录" }));

    expect(screen.getByRole("alert")).toHaveTextContent("金额必须大于 0");
    expect(mutations.create.mutate).not.toHaveBeenCalled();
  });
});
