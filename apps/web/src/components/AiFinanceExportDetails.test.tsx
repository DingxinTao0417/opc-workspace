import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { AiActionProposal } from "../api/aiWorkspaceActions";
import { AiFinanceExportDetails } from "./AiFinanceExportDetails";

const mocks = vi.hoisted(() => ({ download: vi.fn() }));
vi.mock("../api/client", async () => ({
  ...(await vi.importActual("../api/client")),
  downloadApprovedFinancialCSV: mocks.download,
}));
const proposal: AiActionProposal = {
  id: "00000000-0000-4000-8000-000000000001",
  generation_id: "00000000-0000-4000-8000-000000000002",
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
  can_confirm: false,
  result_id: "00000000-0000-4000-8000-000000000001",
  result_version: 1,
  route: "",
  created_at: "2026-09-18T12:00:00Z",
  decided_at: "2026-09-18T12:01:00Z",
  fingerprint: "a".repeat(64),
  status: "confirmed",
  preview: {
    label: "九月 CSV",
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
      row_count: 0,
      size_bytes: 130,
      sha256: "b".repeat(64),
    },
  },
};
beforeEach(() => mocks.download.mockReset());
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function urlMocks() {
  const create = vi.fn(() => "blob:csv");
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = create;
      static revokeObjectURL = vi.fn();
    },
  );
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});
  return { create, click };
}
it("downloads only after a click with exact identity, no automatic download or saved-file claim", async () => {
  const { create, click } = urlMocks();
  mocks.download.mockResolvedValue({
    blob: new Blob(["csv"]),
    fileName: "approved.csv",
  });
  render(<AiFinanceExportDetails proposal={proposal} />);
  expect(mocks.download).not.toHaveBeenCalled();
  expect(screen.getByText(/零条时仅导出表头/)).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "下载已批准的 CSV" }));
  expect(await screen.findByRole("status")).toHaveTextContent(
    "请在下载列表确认保存结果",
  );
  expect(mocks.download).toHaveBeenCalledWith(
    {
      id: proposal.id,
      fingerprint: proposal.fingerprint,
      sha256: proposal.preview.after.sha256,
      sizeBytes: 130,
    },
    expect.any(AbortSignal),
  );
  expect(create).toHaveBeenCalledOnce();
  expect(click).toHaveBeenCalledOnce();
});
it("keeps changed data failures visible and never downloads a fallback", async () => {
  const { create } = urlMocks();
  mocks.download.mockRejectedValue(
    new ApiError("changed", { code: "AI_EXPORT_CHANGED" }),
  );
  render(<AiFinanceExportDetails proposal={proposal} />);
  fireEvent.click(screen.getByRole("button", { name: "下载已批准的 CSV" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("重新提议并核对");
  expect(create).not.toHaveBeenCalled();
  expect(mocks.download).toHaveBeenCalledOnce();
  expect(
    screen.getByRole("button", { name: "下载已批准的 CSV" }),
  ).toBeEnabled();
});
it("cancels on leaving the card and ignores a late blob", async () => {
  const { create } = urlMocks();
  let resolve!: (value: unknown) => void;
  mocks.download.mockReturnValue(new Promise((done) => (resolve = done)));
  const view = render(<AiFinanceExportDetails proposal={proposal} />);
  fireEvent.click(screen.getByRole("button", { name: "下载已批准的 CSV" }));
  const signal = mocks.download.mock.calls[0][1] as AbortSignal;
  view.unmount();
  expect(signal.aborted).toBe(true);
  resolve({ blob: new Blob(["csv"]), fileName: "approved.csv" });
  await waitFor(() => expect(create).not.toHaveBeenCalled());
});
