import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WorkspaceFileRecovery, recoveryDiff } from "./WorkspaceFileRecovery";
import {
  workspaceApi,
  type WorkspaceFileRecoveryPreview,
} from "../api/workspace";
// Isolated prototype tests only; production restoration remains disabled.
vi.mock("../lib/projectFileWriteGate", () => ({
  projectFileWriteGate: { enabled: true, reason: "isolated prototype test" },
}));
vi.mock("../api/workspace", async (load) => ({
  ...(await load<typeof import("../api/workspace")>()),
  workspaceApi: {
    fileRecoveryList: vi.fn(),
    fileRecoveryPreview: vi.fn(),
    applyFileRecovery: vi.fn(),
    releaseFileSnapshot: vi.fn(async () => {}),
  },
}));
const root = { id: "root", name: "project", path: "D:/project" };
const record = {
  id: "018f0000-0000-7000-8000-000000000201",
  path: "a.txt",
  createdAt: 1,
  restores: null,
};
const preview: WorkspaceFileRecoveryPreview = {
  id: record.id,
  snapshotId: "018f0000-0000-7000-8000-000000000202",
  path: record.path,
  currentBase64: btoa("current\r\n"),
  originalBase64: btoa("original\r\n"),
  expiresInSeconds: 600,
};
beforeEach(() => {
  vi.mocked(workspaceApi.fileRecoveryList).mockResolvedValue({
    records: [record],
    damaged: 0,
    directory: "D:/local/recovery",
  });
  vi.mocked(workspaceApi.fileRecoveryPreview).mockResolvedValue(preview);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useRealTimers();
});
it("reads metadata first, then full diff only on click, and restores only after explicit confirmation", async () => {
  vi.mocked(workspaceApi.applyFileRecovery).mockImplementationOnce(
    async (_root, _record, _snapshot, id) => ({
      id,
      status: "applied",
      backupPath: "local recovery copy",
    }),
  );
  render(<WorkspaceFileRecovery root={root} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /核对恢复 a.txt/ }),
  );
  const diff = await screen.findByLabelText("完整恢复差异");
  expect(diff).toHaveTextContent("current\\r\\n");
  expect(diff).toHaveTextContent("original\\r\\n");
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "确认恢复此文件" })).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: /我已核对完整差异/ }));
  fireEvent.click(screen.getByRole("button", { name: "确认恢复此文件" }));
  await screen.findByText(/恢复写入及完整核验成功/);
  expect(workspaceApi.applyFileRecovery).toHaveBeenCalledTimes(1);
  expect(workspaceApi.applyFileRecovery).toHaveBeenCalledWith(
    root.id,
    record.id,
    preview.snapshotId,
    expect.any(String),
  );
  expect(screen.queryByLabelText("完整恢复差异")).toBeNull();
});
it("shows torn UTF-8 as exact hexadecimal bytes, never lossy replacement text", () => {
  const value = recoveryDiff({
    ...preview,
    currentBase64: btoa(String.fromCharCode(0xe6, 0x00, 0x61)),
  });
  expect(value.binary).toBe(true);
  expect(value.before).toContain("e6 00 61");
  expect(value.before).not.toContain("�");
});
it("releases late previews after leaving the panel and does not inherit confirmation", async () => {
  let resolve!: (value: WorkspaceFileRecoveryPreview) => void;
  vi.mocked(workspaceApi.fileRecoveryPreview).mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const view = render(<WorkspaceFileRecovery root={root} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /核对恢复 a.txt/ }),
  );
  view.unmount();
  await act(async () => resolve(preview));
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
    root.id,
    preview.snapshotId,
  );
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
});
it("keeps uncertain restore failed closed and requires a new preview", async () => {
  vi.mocked(workspaceApi.applyFileRecovery).mockRejectedValueOnce(
    new Error("lost IPC"),
  );
  render(<WorkspaceFileRecovery root={root} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /核对恢复 a.txt/ }),
  );
  fireEvent.click(
    await screen.findByRole("checkbox", { name: /我已核对完整差异/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认恢复此文件" }));
  await screen.findByText(/恢复被拒绝或结果未确定/);
  expect(workspaceApi.applyFileRecovery).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("button", { name: "确认恢复此文件" })).toBeNull();
});
it("does not permit an expired review to restore", async () => {
  render(<WorkspaceFileRecovery root={root} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /核对恢复 a.txt/ }),
  );
  await screen.findByLabelText("完整恢复差异");
  fireEvent.click(screen.getByRole("checkbox", { name: /我已核对完整差异/ }));
  vi.spyOn(Date, "now").mockReturnValue(Date.now() + 601000);
  fireEvent.click(screen.getByRole("button", { name: "确认恢复此文件" }));
  await waitFor(() =>
    expect(screen.queryByLabelText("完整恢复差异")).toBeNull(),
  );
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
  vi.restoreAllMocks();
});
