import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Invoice } from "../types/models";
import { InvoiceFormModal } from "./InvoiceFormModal";

const mutations = vi.hoisted(() => ({
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useCreateInvoice: () => mutations.create,
  useUpdateInvoice: () => mutations.update,
}));

vi.mock("./ClientSelect", () => ({
  ClientSelect: ({
    value,
    onChange,
    ariaLabel,
  }: {
    value: string;
    onChange: (value: string) => void;
    ariaLabel: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">选择客户</option>
      <option value="client-1">星河工作室</option>
      <option value="client-2">远山设计</option>
    </select>
  ),
}));

vi.mock("./ProjectSelect", () => ({
  ProjectSelect: ({
    value,
    onChange,
    ariaLabel,
    clientId,
  }: {
    value: string;
    onChange: (value: string) => void;
    ariaLabel: string;
    clientId?: string;
  }) => (
    <select
      aria-label={ariaLabel}
      data-client-id={clientId}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">不关联项目</option>
      <option value="project-1">客户门户</option>
    </select>
  ),
}));

const invoice: Invoice = {
  id: "invoice-1",
  invoiceNumber: "INV-202608-001",
  clientId: "client-1",
  clientName: "星河工作室",
  projectId: "project-1",
  projectName: "客户门户",
  amountMinor: 128045,
  currency: "CNY",
  status: "draft",
  issueDate: "2026-08-29",
  dueDate: "2026-09-28",
  paidDate: null,
  notes: "首付款",
  financialEntryId: null,
  version: 4,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T01:00:00Z",
};

describe("InvoiceFormModal", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it("creates a draft with exact minor units and client-scoped project", () => {
    render(<InvoiceFormModal onClose={vi.fn()} open />);

    fireEvent.change(screen.getByLabelText("发票客户"), {
      target: { value: "client-1" },
    });
    expect(screen.getByLabelText("发票项目")).toHaveAttribute(
      "data-client-id",
      "client-1",
    );
    fireEvent.change(screen.getByLabelText("发票项目"), {
      target: { value: "project-1" },
    });
    fireEvent.change(screen.getByLabelText("发票金额"), {
      target: { value: "1280.45" },
    });
    fireEvent.submit(document.getElementById("invoice-form")!);

    expect(mutations.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        clientId: "client-1",
        projectId: "project-1",
        amountMinor: 128045,
        currency: "CNY",
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("clears a project when the customer changes", () => {
    render(<InvoiceFormModal invoice={invoice} onClose={vi.fn()} open />);
    expect(screen.getByLabelText("发票项目")).toHaveValue("project-1");

    fireEvent.change(screen.getByLabelText("发票客户"), {
      target: { value: "client-2" },
    });

    expect(screen.getByLabelText("发票项目")).toHaveValue("");
    expect(screen.getByLabelText("发票项目")).toHaveAttribute(
      "data-client-id",
      "client-2",
    );
  });

  it("updates a draft with the observed version", () => {
    render(<InvoiceFormModal invoice={invoice} onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("发票备注"), {
      target: { value: "调整付款条款" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(mutations.update.mutate).toHaveBeenCalledWith(
      {
        id: invoice.id,
        input: expect.objectContaining({
          amountMinor: 128045,
          notes: "调整付款条款",
          expectedVersion: 4,
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("blocks a due date before the issue date", () => {
    render(<InvoiceFormModal onClose={vi.fn()} open />);
    fireEvent.change(screen.getByLabelText("发票客户"), {
      target: { value: "client-1" },
    });
    fireEvent.change(screen.getByLabelText("发票金额"), {
      target: { value: "100" },
    });
    fireEvent.change(screen.getByLabelText("开票日期"), {
      target: { value: "2026-09-10" },
    });
    fireEvent.change(screen.getByLabelText("到期日期"), {
      target: { value: "2026-09-09" },
    });
    fireEvent.submit(document.getElementById("invoice-form")!);

    expect(screen.getByRole("alert")).toHaveTextContent(
      "到期日期不能早于开票日期",
    );
    expect(mutations.create.mutate).not.toHaveBeenCalled();
  });
});
