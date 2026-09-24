import { describe, expect, it, vi } from "vitest";
import {
  parseAiActionProposal,
  decideAiWorkspaceAction,
} from "./aiWorkspaceActions";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
const id = "00000000-0000-4000-8000-000000000003";
const record = {
  invoice_number: "INV-2026-001",
  client_id: id,
  client_name: "客户",
  project_id: null,
  project_name: null,
  amount_minor: 1250,
  currency: "CNY",
  issue_date: "2026-09-01",
  due_date: "2026-09-20",
  paid_date: null,
  status: "viewed",
  notes: "仅人工可见",
};
const base = {
  id: "00000000-0000-4000-8000-000000000001",
  generation_id: "00000000-0000-4000-8000-000000000002",
  fingerprint: "a".repeat(64),
  action: {
    action: "invoice.mark_paid",
    invoice_id: id,
    expected_version: 3,
    changes: { paid_date: "2026-09-18" },
  },
  preview: {
    label: record.invoice_number,
    before: record,
    after: { ...record, paid_date: "2026-09-18", status: "paid" },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "https://untrusted.example/",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: null,
};
describe("invoice approval contract", () => {
  const deletion = {
    ...base,
    action: {
      action: "invoice.delete",
      invoice_id: id,
      expected_version: 3,
      changes: {},
    },
    preview: {
      label: record.invoice_number,
      before: {
        ...record,
        status: "draft",
        pdf_asset_id: id,
        pdf_file_name: "invoice.pdf",
        pdf_size_bytes: 1234,
        pdf_sha256: "b".repeat(64),
        pdf_generated_from_version: 2,
        pdf_generated_at: "2026-09-18T12:00:00Z",
      },
      after: { invoice_deleted: true, pdf_removed: true },
    },
  };
  const pdf = {
    ...deletion,
    action: { ...deletion.action, action: "invoice.generate_pdf" },
    preview: {
      ...deletion.preview,
      after: { pdf_generated: true, pdf_replaced: true },
    },
  };
  const pdfResult = {
    asset_id: "00000000-0000-4000-8000-000000000004",
    invoice_id: id,
    file_name: "invoice.pdf",
    mime_type: "application/pdf",
    size_bytes: 1234,
    sha256: "c".repeat(64),
    generated_from_version: 3,
    generated_at: "2026-09-18T12:00:00Z",
    integrity_status: "verified",
    integrity_checked_at: "2026-09-18T12:00:00Z",
  };
  const generated = {
    ...pdf,
    status: "confirmed",
    can_confirm: false,
    result_id: pdfResult.asset_id,
    result_version: 3,
    invoice_pdf_result: pdfResult,
  };
  it("binds PDF generation to complete invoice/PDF preview and a historical artifact result", () => {
    expect(parseAiActionProposal(pdf).route).toBe(`/invoices/${id}`);
    const result = parseAiActionProposal(generated);
    expect(result.route).toBe(`/invoices/${id}`);
    expect(result.invoice_pdf_result).toEqual(pdfResult);
  });
  it.each([
    [
      "absent result",
      (p: any) => {
        delete p.invoice_pdf_result;
      },
    ],
    [
      "wrong invoice",
      (p: any) => {
        p.invoice_pdf_result.invoice_id = pdfResult.asset_id;
      },
    ],
    [
      "wrong asset",
      (p: any) => {
        p.invoice_pdf_result.asset_id = id;
      },
    ],
    [
      "wrong version",
      (p: any) => {
        p.invoice_pdf_result.generated_from_version = 2;
      },
    ],
    [
      "wrong hash",
      (p: any) => {
        p.invoice_pdf_result.sha256 = "bad";
      },
    ],
    [
      "hidden path",
      (p: any) => {
        p.invoice_pdf_result.relative_path = "x";
      },
    ],
    [
      "filename traversal",
      (p: any) => {
        p.invoice_pdf_result.file_name = "../x.pdf";
      },
    ],
    [
      "unverified result",
      (p: any) => {
        p.invoice_pdf_result.integrity_status = "missing";
      },
    ],
    [
      "unbounded file",
      (p: any) => {
        p.invoice_pdf_result.size_bytes = 52428801;
      },
    ],
    [
      "incomplete preview",
      (p: any) => {
        delete p.preview.before.notes;
      },
    ],
    [
      "unshown replacement",
      (p: any) => {
        p.preview.after.pdf_replaced = false;
      },
    ],
    [
      "model consent",
      (p: any) => {
        p.action.confirm_invoice_pdf = true;
      },
    ],
    [
      "premature result",
      (p: any) => {
        p.status = "pending";
      },
    ],
  ])("rejects PDF %s", (_label, change) => {
    const value = structuredClone(generated);
    change(value);
    expect(() => parseAiActionProposal(value)).toThrow();
  });
  it("only sends PDF consent for human confirmation, never reject", async () => {
    mocks.request.mockResolvedValue({ data: generated });
    await decideAiWorkspaceAction(
      parseAiActionProposal(pdf),
      "confirm",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: pdf.fingerprint,
      decision: "confirm",
      confirm_invoice_effects: true,
      confirm_invoice_pdf: true,
    });
    mocks.request.mockResolvedValue({
      data: { ...pdf, status: "rejected", can_confirm: false },
    });
    await decideAiWorkspaceAction(
      parseAiActionProposal(pdf),
      "reject",
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      true,
      undefined,
      true,
    );
    expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
      fingerprint: pdf.fingerprint,
      decision: "reject",
    });
  });
  it("binds deletion to the complete draft/PDF and navigates away from a deleted detail", () => {
    expect(parseAiActionProposal(deletion).route).toBe(`/invoices/${id}`);
    expect(
      parseAiActionProposal({
        ...deletion,
        status: "confirmed",
        can_confirm: false,
        result_id: id,
        result_version: 3,
      }).route,
    ).toBe("/invoices");
    const noPdf = structuredClone(deletion);
    for (const key of Object.keys(noPdf.preview.before).filter((k) =>
      k.startsWith("pdf_"),
    ))
      (noPdf.preview.before as Record<string, unknown>)[key] = null;
    noPdf.preview.after.pdf_removed = false;
    expect(parseAiActionProposal(noPdf).action.action).toBe("invoice.delete");
  });
  it.each([
    [
      "sent invoice",
      (p: any) => {
        p.preview.before.status = "sent";
      },
    ],
    [
      "missing PDF identity",
      (p: any) => {
        delete p.preview.before.pdf_asset_id;
      },
    ],
    [
      "invented no-PDF claim",
      (p: any) => {
        p.preview.after.pdf_removed = false;
      },
    ],
    [
      "partial absent asset",
      (p: any) => {
        p.preview.before.pdf_asset_id = null;
      },
    ],
    [
      "hidden path",
      (p: any) => {
        p.preview.before.relative_path = "x.pdf";
      },
    ],
    [
      "missing notes",
      (p: any) => {
        delete p.preview.before.notes;
      },
    ],
    [
      "changed notes",
      (p: any) => {
        p.action.changes.notes = "new";
      },
    ],
    [
      "model deletion consent",
      (p: any) => {
        p.action.confirm_invoice_delete = true;
      },
    ],
    [
      "not deleted",
      (p: any) => {
        p.preview.after.invoice_deleted = false;
      },
    ],
    [
      "extra effects",
      (p: any) => {
        p.preview.after.status = "paid";
      },
    ],
    [
      "oversize PDF",
      (p: any) => {
        p.preview.before.pdf_size_bytes = 52428801;
      },
    ],
    [
      "future PDF version",
      (p: any) => {
        p.preview.before.pdf_generated_from_version = 4;
      },
    ],
    [
      "invalid hash",
      (p: any) => {
        p.preview.before.pdf_sha256 = "hash";
      },
    ],
  ])("rejects unsafe deletion: %s", (_label, mutate) => {
    const p = structuredClone(deletion);
    mutate(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("transmits both deletion consents only for this command and only on confirm", async () => {
    for (const input of [deletion, base])
      for (const decision of ["confirm", "reject"] as const) {
        const p = parseAiActionProposal(input);
        mocks.request.mockResolvedValue({
          data: {
            ...input,
            status: decision === "confirm" ? "confirmed" : "rejected",
            can_confirm: false,
            result_id: decision === "confirm" ? id : null,
            result_version: decision === "confirm" ? 3 : null,
          },
        });
        await decideAiWorkspaceAction(
          p,
          decision,
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          undefined,
          true,
          true,
        );
        expect(JSON.parse(mocks.request.mock.lastCall![1].body)).toEqual({
          fingerprint: p.fingerprint,
          decision,
          ...(decision === "confirm" ? { confirm_invoice_effects: true } : {}),
          ...(decision === "confirm" && input === deletion
            ? { confirm_invoice_delete: true }
            : {}),
        });
      }
  });
  it("accepts six commands, exact snapshots and derives invoice routes", () => {
    expect(parseAiActionProposal(base).route).toBe(`/invoices/${id}`);
    for (const [action, before, after, changes] of [
      ["update", "draft", "draft", { notes: "修订" }],
      ["mark_sent", "draft", "sent", {}],
      ["mark_viewed", "sent", "viewed", {}],
      ["mark_overdue", "sent", "overdue", {}],
    ] as const) {
      expect(
        parseAiActionProposal({
          ...base,
          action: { ...base.action, action: `invoice.${action}`, changes },
          preview: {
            label: record.invoice_number,
            before: { ...record, status: before },
            after: { ...record, ...changes, status: after },
          },
        }).action.action,
      ).toBe(`invoice.${action}`);
    }
    const {
      invoice_number: _number,
      client_name: _client,
      project_name: _project,
      status: _status,
      paid_date: _paid,
      ...changes
    } = record;
    const create = {
      ...base,
      action: { action: "invoice.create", changes },
      preview: {
        label: "新发票草稿",
        before: {},
        after: { ...record, invoice_number: null, status: "draft" },
      },
    };
    expect(parseAiActionProposal(create).route).toBe("");
    expect(
      parseAiActionProposal({
        ...create,
        status: "confirmed",
        can_confirm: false,
        result_id: id,
        result_version: 1,
      }).route,
    ).toBe(`/invoices/${id}`);
  });
  it.each([
    [
      "fractional amount",
      (p: any) => {
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
      "missing full notes",
      (p: any) => {
        delete p.preview.before.notes;
      },
    ],
    [
      "overlong notes",
      (p: any) => {
        p.preview.after.notes = "😀".repeat(10001);
      },
    ],
    [
      "renamed client",
      (p: any) => {
        p.preview.after.client_name = "另一客户";
      },
    ],
    [
      "changed amount",
      (p: any) => {
        p.preview.after.amount_minor = 2500;
      },
    ],
    [
      "wrong paid date",
      (p: any) => {
        p.preview.after.paid_date = "2026-09-19";
      },
    ],
    [
      "nonexistent date",
      (p: any) => {
        p.action.changes.paid_date = p.preview.after.paid_date = "2026-02-30";
      },
    ],
    [
      "paid before issue",
      (p: any) => {
        p.action.changes.paid_date = p.preview.after.paid_date = "2026-08-30";
      },
    ],
    [
      "skip sent/viewed",
      (p: any) => {
        p.preview.before.status = "draft";
      },
    ],
    [
      "manual number",
      (p: any) => {
        p.preview.after.invoice_number = "manual";
      },
    ],
    [
      "wrong target",
      (p: any) => {
        p.action.financial_entry_id = id;
      },
    ],
    [
      "missing version",
      (p: any) => {
        delete p.action.expected_version;
      },
    ],
    [
      "model consent",
      (p: any) => {
        p.action.confirm_invoice_effects = true;
      },
    ],
    [
      "transition edits",
      (p: any) => {
        p.action.changes.notes = "not allowed";
      },
    ],
    [
      "extra preview",
      (p: any) => {
        p.preview.after.secret = "hidden effect";
      },
    ],
  ])("rejects %s", (_label, mutate) => {
    const p = structuredClone(base);
    mutate(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it("sends only human invoice consent on confirm and strips it on reject", async () => {
    const p = parseAiActionProposal(base);
    for (const decision of ["confirm", "reject"] as const) {
      mocks.request.mockResolvedValue({
        data: {
          ...base,
          status: decision === "confirm" ? "confirmed" : "rejected",
          can_confirm: false,
          result_id: decision === "confirm" ? id : null,
          result_version: decision === "confirm" ? 4 : null,
        },
      });
      await decideAiWorkspaceAction(
        p,
        decision,
        undefined,
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
        decision,
        ...(decision === "confirm" ? { confirm_invoice_effects: true } : {}),
      });
    }
  });
});
