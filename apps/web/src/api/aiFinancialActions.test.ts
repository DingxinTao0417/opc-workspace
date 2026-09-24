import { describe, expect, it, vi } from "vitest";
import {
  parseAiActionProposal,
  decideAiWorkspaceAction,
} from "./aiWorkspaceActions";
import { financialMinorDisplay } from "./aiFinancialActions";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const entryId = "00000000-0000-4000-8000-000000000003";
const record = {
  type: "expense",
  amount_minor: 1250,
  currency: "CNY",
  occurred_on: "2026-09-18",
  status: "confirmed",
  category: "办公",
  client_id: null,
  project_id: null,
  client_name: null,
  project_name: null,
  notes: "仅人工可见",
};
const base = {
  id: "00000000-0000-4000-8000-000000000001",
  generation_id: "00000000-0000-4000-8000-000000000002",
  fingerprint: "a".repeat(64),
  action: {
    action: "financial_entry.update",
    financial_entry_id: entryId,
    expected_version: 1,
    changes: { amount_minor: 2500 },
  },
  preview: {
    label: "办公支出",
    before: record,
    after: { ...record, amount_minor: 2500 },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "https://untrusted.example/",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: null,
};
describe("financial proposal contract", () => {
  it("validates all three operations and derives the real ledger route", () => {
    expect(parseAiActionProposal(base).route).toBe(`/income/${entryId}`);
    const { client_name: _client, project_name: _project, ...fields } = record;
    const create = {
      ...base,
      action: { action: "financial_entry.create", changes: fields },
      preview: { ...base.preview, before: {}, after: record },
    };
    expect(parseAiActionProposal(create).route).toBe("");
    expect(
      parseAiActionProposal({
        ...create,
        status: "confirmed",
        can_confirm: false,
        result_id: entryId,
        result_version: 1,
      }).route,
    ).toBe(`/income/${entryId}`);
    expect(
      parseAiActionProposal({
        ...base,
        action: {
          ...base.action,
          action: "financial_entry.void",
          changes: { reason: "重复" },
        },
        preview: {
          ...base.preview,
          after: { ...record, status: "voided", reason: "重复" },
        },
      }).action.action,
    ).toBe("financial_entry.void");
  });
  it.each([
    [
      "fraction",
      (p: any) => {
        p.action.changes.amount_minor = 1.25;
        p.preview.after.amount_minor = 1.25;
      },
    ],
    [
      "overflow",
      (p: any) => {
        p.preview.after.amount_minor = 9000000000000001;
      },
    ],
    [
      "changed amount",
      (p: any) => {
        p.preview.after.amount_minor = 1;
      },
    ],
    [
      "changed currency",
      (p: any) => {
        p.preview.after.currency = "USD";
      },
    ],
    [
      "bad date",
      (p: any) => {
        p.preview.after.occurred_on = "2026-02-30";
      },
    ],
    [
      "missing note",
      (p: any) => {
        delete p.preview.before.notes;
      },
    ],
    [
      "null note",
      (p: any) => {
        p.preview.after.notes = null;
      },
    ],
    [
      "unknown field",
      (p: any) => {
        p.action.changes.invoice_id = entryId;
      },
    ],
    [
      "missing name",
      (p: any) => {
        p.preview.after.project_id = entryId;
      },
    ],
    [
      "wrong target",
      (p: any) => {
        p.action.task_id = entryId;
      },
    ],
    [
      "unknown invoice target",
      (p: any) => {
        p.action.invoice_id = entryId;
      },
    ],
    [
      "model supplied consent",
      (p: any) => {
        p.action.confirm_financial_effects = true;
      },
    ],
    [
      "void via update",
      (p: any) => {
        p.action.changes.status = "voided";
        p.preview.after.status = "voided";
      },
    ],
  ])("rejects %s", (_name, mutate) => {
    const p = structuredClone(base);
    mutate(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("requires a separate financial consent field and never sends it on reject", async () => {
    const p = parseAiActionProposal(base);
    mocks.request.mockResolvedValue({
      data: {
        ...base,
        status: "confirmed",
        can_confirm: false,
        result_id: entryId,
        result_version: 2,
      },
    });
    await decideAiWorkspaceAction(
      p,
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: p.fingerprint,
      decision: "confirm",
      confirm_financial_effects: true,
    });
    mocks.request.mockResolvedValue({
      data: { ...base, status: "rejected", can_confirm: false },
    });
    await decideAiWorkspaceAction(
      p,
      "reject",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: p.fingerprint,
      decision: "reject",
    });
  });
  it("shows exact minor-unit values without floating-point rounding", () => {
    expect(financialMinorDisplay(1250, "CNY")).toBe(
      "CNY 12.50（1250 最小单位）",
    );
    expect(financialMinorDisplay(8999999999999999, "USD")).toBe(
      "USD 89999999999999.99（8999999999999999 最小单位）",
    );
  });
});
