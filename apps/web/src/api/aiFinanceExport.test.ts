import { afterEach, describe, expect, it, vi } from "vitest";
import { financialCSVColumns } from "./aiFinanceExport";
import {
  decideAiFinanceExport,
  parseAiActionProposal,
} from "./aiWorkspaceActions";
import { downloadApprovedFinancialCSV, resetRuntimeConnection } from "./client";

function fixture() {
  return {
    id: "00000000-0000-4000-8000-000000000001",
    generation_id: "00000000-0000-4000-8000-000000000002",
    fingerprint: "a".repeat(64),
    action: {
      action: "finance.export_csv",
      changes: {
        export_filters: {
          currency: "CNY",
          date_from: "2026-09-01",
          date_to: "2026-09-30",
          entry_type: "all",
          status: "active",
        },
      },
    },
    preview: {
      label: "导出",
      before: {},
      after: {
        currency: "CNY",
        date_from: "2026-09-01",
        date_to: "2026-09-30",
        entry_type: "all",
        export_status: "active",
        category: "",
        client_id: null,
        project_id: null,
        row_count: 101,
        size_bytes: 1234,
        sha256: "b".repeat(64),
        csv_columns: financialCSVColumns,
        export_sort: "occurred_on DESC, created_at DESC, id ASC",
      },
    },
    status: "pending",
    can_confirm: true,
    result_id: null,
    result_version: null,
    route: "/tasks/forged",
    created_at: "2026-09-18T12:00:00Z",
    decided_at: null,
  };
}
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});
describe("finance export identity", () => {
  it("accepts full explicit scope and never treats approval as a business record link", () => {
    const p = parseAiActionProposal(fixture());
    expect(p.route).toBe("");
    expect(
      parseAiActionProposal({
        ...p,
        status: "confirmed",
        can_confirm: false,
        result_id: p.id,
        result_version: 1,
        decided_at: p.created_at,
      }).route,
    ).toBe("");
  });
  it.each([
    (p: any) => {
      delete p.action.changes.export_filters.currency;
    },
    (p: any) => {
      p.action.changes.export_filters.page = 1;
    },
    (p: any) => {
      p.action.changes.export_filters.date_from = "2026-02-30";
    },
    (p: any) => {
      p.action.changes.export_filters.date_to = "2028-01-01";
    },
    (p: any) => {
      p.action.expected_version = 1;
    },
    (p: any) => {
      p.action.task_id = p.id;
    },
    (p: any) => {
      p.preview.after.currency = "USD";
    },
    (p: any) => {
      p.preview.after.row_count = 10001;
    },
    (p: any) => {
      p.preview.after.size_bytes = 16 * 1024 * 1024 + 1;
    },
    (p: any) => {
      p.preview.after.sha256 = "bad";
    },
    (p: any) => {
      p.preview.after.csv_columns = "notes";
    },
    (p: any) => {
      p.preview.after.category = "other";
    },
    (p: any) => {
      p.preview.tasks = [];
    },
    (p: any) => {
      p.result_id = p.id;
    },
    (p: any) => {
      p.status = "confirmed";
      p.can_confirm = false;
    },
  ])("rejects malformed or mismatched export %s", (mutate) => {
    const p = fixture();
    mutate(p);
    expect(() => parseAiActionProposal(p)).toThrow();
  });
  it.each(["confirm", "reject"] as const)(
    "sends only human consent for %s",
    async (decision) => {
      const p = parseAiActionProposal(fixture());
      const confirmed = decision === "confirm";
      const response = {
        ...p,
        status: confirmed ? "confirmed" : "rejected",
        can_confirm: false,
        result_id: confirmed ? p.id : null,
        result_version: confirmed ? 1 : null,
        decided_at: p.created_at,
      };
      const fetch = vi.fn(
        async () =>
          new Response(JSON.stringify({ data: response }), {
            headers: { "Content-Type": "application/json" },
          }),
      );
      vi.stubGlobal("fetch", fetch);
      await decideAiFinanceExport(p, decision, true);
      const init = fetch.mock.calls[0] as unknown as [unknown, RequestInit];
      expect(JSON.parse(init[1].body as string)).toEqual({
        fingerprint: p.fingerprint,
        decision,
        ...(confirmed ? { confirm_finance_export: true } : {}),
      });
    },
  );
  it("downloads exact approval with hash/size checks, without new filters", async () => {
    const p = fixture(),
      text = "csv";
    const fetch = vi.fn(
      async () =>
        new Response(text, {
          headers: {
            "Content-Type": "text/csv",
            "X-Financial-CSV-SHA256": p.preview.after.sha256,
          },
        }),
    );
    vi.stubGlobal("fetch", fetch);
    const expected = {
      id: p.id,
      fingerprint: p.fingerprint,
      sha256: p.preview.after.sha256,
      sizeBytes: 3,
    };
    const file = await downloadApprovedFinancialCSV(expected);
    expect(file.fileName).toBe(`financial-entries-${p.id}.csv`);
    expect(String((fetch.mock.calls[0] as unknown as [unknown])[0])).toContain(
      `/ai/actions/${p.id}/export.csv?fingerprint=${p.fingerprint}`,
    );
    await expect(
      downloadApprovedFinancialCSV({ ...expected, sizeBytes: 4 }),
    ).rejects.toThrow();
    await expect(
      downloadApprovedFinancialCSV({ ...expected, sha256: "c".repeat(64) }),
    ).rejects.toThrow();
    fetch.mockClear();
    await expect(
      downloadApprovedFinancialCSV({ ...expected, id: "../other" }),
    ).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });
});
