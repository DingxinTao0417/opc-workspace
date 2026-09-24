import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { WorkspaceFileRecovery } from "./WorkspaceFileRecovery";
import { WorkspaceFileReplacementReview } from "./WorkspaceFileReplacementReview";
import {
  workspaceApi,
  type WorkspaceFileReplacementPreview,
  type WorkspaceFileReplacementReview as ReviewTicket,
} from "../api/workspace";
vi.mock("../lib/projectFileWriteGate", () => ({
  projectFileWriteGate: { enabled: true, reason: "isolated test" },
  useProjectFileWriteCapability: () => ({
    available: true,
    reason: "isolated bound-helper test",
  }),
}));
vi.mock("../api/workspace", async (load) => ({
  ...(await load<typeof import("../api/workspace")>()),
  workspaceApi: {
    fileRecoveryList: vi.fn(),
    fileRecoveryPreview: vi.fn(),
    fileReplacementPreview: vi.fn(),
    reviewFileReplacement: vi.fn(),
    releaseReplacementReview: vi.fn(async () => {}),
    applyFileReplacement: vi.fn(),
    cancelFileOperation: vi.fn(async () => {}),
    applyFileRecovery: vi.fn(),
    applyFileEdit: vi.fn(),
    releaseFileSnapshot: vi.fn(async () => {}),
  },
}));
const root = { id: "root", name: "project", path: "D:/project" };
const record = {
  id: "018f0000-0000-7000-8000-000000000301",
  path: "a.txt",
  version: 2 as const,
  createdAt: null,
  restores: null,
};
const view: WorkspaceFileReplacementPreview = {
  id: record.id,
  path: record.path,
  observation: "target_missing",
  target: "missing",
  parked: "original",
  staged: "candidate",
  currentBase64: null,
  originalBase64: btoa("original\r\n"),
  candidateBase64: btoa("<script>candidate</script>\n"),
};
beforeEach(() => {
  vi.mocked(workspaceApi.fileRecoveryList).mockResolvedValue({
    records: [record],
    damaged: 0,
    directory: "local/v1",
    replacementDirectory: "local/v2",
  });
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValue(view);
  vi.mocked(workspaceApi.reviewFileReplacement).mockResolvedValue({
    ...view,
    reviewId: "018f0000-0000-7000-8000-000000000302",
    mode: "missing_target",
    expiresInSeconds: 600,
  });
  vi.mocked(workspaceApi.applyFileReplacement).mockImplementation(
    async (_root, _review, operation) => ({
      id: operation,
      status: "applied",
      backupPath: "local/v2/record.json",
    }),
  );
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useRealTimers();
});
async function open() {
  const result = render(<WorkspaceFileRecovery root={root} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /核对恢复 a.txt/ }),
  );
  return result;
}
it("shows missing-to-original recovery separately from historical edits", async () => {
  await open();
  const direction = await screen.findByLabelText("待审查的恢复方向");
  expect(direction).toHaveTextContent("缺失恢复：缺失路径 → 原对象");
  expect(direction).toHaveTextContent("候选对象仍会保留，不安装候选内容");
  const diff = screen.getByLabelText("缺失路径 → 记录原文（新增目标）");
  expect(diff).toHaveTextContent("+ original");
  expect(diff).not.toHaveTextContent("candidate");
  expect(
    screen.getByText("历史修改对照（不是本次恢复方向）"),
  ).toBeInTheDocument();
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
});
it("distinguishes restoring an empty file from leaving the target absent", async () => {
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValueOnce({
    ...view,
    originalBase64: "",
  });
  await open();
  expect(
    await screen.findByText(
      "原对象是空文件；恢复后路径会存在，大小为 0 字节。",
    ),
  ).toBeInTheDocument();
});
it("shows undo from the installed candidate back to the original, preserving both", async () => {
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValueOnce({
    ...view,
    observation: "candidate_present",
    target: "candidate",
    staged: "missing",
    currentBase64: view.candidateBase64,
  });
  await open();
  expect(
    await screen.findByText("原样撤销：当前候选 → 原对象"),
  ).toBeInTheDocument();
  const diff = screen.getByLabelText("当前目标主文 → 记录原文");
  expect(diff).toHaveTextContent("- <script>candidate</script>");
  expect(diff).toHaveTextContent("+ original");
  expect(screen.getByLabelText("待审查的恢复方向")).toHaveTextContent(
    "候选先移回空闲暂存位置",
  );
});
it("does not reuse an in-flight review when switching project selections", async () => {
  let resolve!: (value: ReviewTicket) => void;
  vi.mocked(workspaceApi.reviewFileReplacement).mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const result = render(
    <WorkspaceFileReplacementReview
      root={root}
      record={record}
      onClose={() => {}}
    />,
  );
  fireEvent.click(
    await screen.findByRole("button", { name: "复核恢复条件（只读）" }),
  );
  result.rerender(
    <WorkspaceFileReplacementReview
      root={{ ...root, id: "new-selection" }}
      record={record}
      onClose={() => {}}
    />,
  );
  await screen.findByLabelText("记录原文 → 候选全文");
  await act(async () =>
    resolve({
      ...view,
      reviewId: "018f0000-0000-7000-8000-000000000302",
      mode: "missing_target",
      expiresInSeconds: 600,
    }),
  );
  expect(screen.queryByText(/已冻结本次恢复审查/)).toBeNull();
  expect(workspaceApi.releaseReplacementReview).toHaveBeenCalledWith(
    root.id,
    "018f0000-0000-7000-8000-000000000302",
  );
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
});
it("creates a local review only on a separate click and releases it on close", async () => {
  await open();
  await screen.findByLabelText("记录原文 → 候选全文");
  expect(workspaceApi.reviewFileReplacement).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "复核恢复条件（只读）" }));
  await screen.findByText(/已冻结本次恢复审查/);
  expect(workspaceApi.reviewFileReplacement).toHaveBeenCalledWith(
    root.id,
    record.id,
    "missing_target",
  );
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
  expect(screen.getByRole("checkbox")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "关闭替换记录预览" }));
  expect(workspaceApi.releaseReplacementReview).toHaveBeenCalledWith(
    root.id,
    "018f0000-0000-7000-8000-000000000302",
  );
  expect(workspaceApi.releaseFileSnapshot).not.toHaveBeenCalled();
});
it("applies one frozen v2 review only after checkbox confirmation", async () => {
  await open();
  fireEvent.click(
    await screen.findByRole("button", { name: "复核恢复条件（只读）" }),
  );
  const checkbox = await screen.findByRole("checkbox", {
    name: /同意进入原生确认/,
  });
  const apply = screen.getByRole("button", { name: "确认恢复此文件" });
  expect(apply).toBeDisabled();
  fireEvent.click(checkbox);
  fireEvent.click(apply);
  expect(await screen.findByText(/原对象已恢复到缺失路径/)).toBeInTheDocument();
  expect(workspaceApi.applyFileReplacement).toHaveBeenCalledTimes(1);
  const [rootId, reviewId, operationId] = vi.mocked(
    workspaceApi.applyFileReplacement,
  ).mock.calls[0];
  expect(rootId).toBe(root.id);
  expect(reviewId).toBe("018f0000-0000-7000-8000-000000000302");
  expect(operationId).toMatch(
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
  );
});
it("cancels the exact in-flight v2 operation and requires a fresh review", async () => {
  let finish!: (value: {
    id: string;
    status: "cancelled";
    backupPath: string;
  }) => void;
  vi.mocked(workspaceApi.applyFileReplacement).mockImplementationOnce(
    async (_root, _review, operation) =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await open();
  fireEvent.click(
    await screen.findByRole("button", { name: "复核恢复条件（只读）" }),
  );
  fireEvent.click(
    await screen.findByRole("checkbox", { name: /同意进入原生确认/ }),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认恢复此文件" }));
  const cancel = await screen.findByRole("button", {
    name: "取消本次操作",
  });
  fireEvent.click(cancel);
  const operationId = vi.mocked(workspaceApi.applyFileReplacement).mock
    .calls[0][2];
  await waitFor(() =>
    expect(workspaceApi.cancelFileOperation).toHaveBeenCalledWith(
      root.id,
      operationId,
    ),
  );
  finish({ id: operationId, status: "cancelled", backupPath: "" });
  expect(await screen.findByText(/旧审查已消费/)).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).toBeNull();
});
it("releases a late review after leaving without displaying it", async () => {
  let resolve!: (value: ReviewTicket) => void;
  vi.mocked(workspaceApi.reviewFileReplacement).mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  await open();
  fireEvent.click(
    await screen.findByRole("button", { name: "复核恢复条件（只读）" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "关闭替换记录预览" }));
  await act(async () =>
    resolve({
      ...view,
      reviewId: "late",
      mode: "missing_target",
      expiresInSeconds: 600,
    }),
  );
  expect(workspaceApi.releaseReplacementReview).toHaveBeenCalledWith(
    root.id,
    "late",
  );
  expect(screen.queryByText(/已冻结本次恢复审查/)).toBeNull();
});
it("rejects changed bytes or mode from a review even with a matching record id", async () => {
  vi.mocked(workspaceApi.reviewFileReplacement).mockResolvedValueOnce({
    ...view,
    originalBase64: btoa("changed source"),
    reviewId: "018f0000-0000-7000-8000-000000000302",
    mode: "missing_target",
    expiresInSeconds: 600,
  });
  await open();
  fireEvent.click(
    await screen.findByRole("button", { name: "复核恢复条件（只读）" }),
  );
  await screen.findByText(/审查结果与当前完整差异不一致/);
  expect(workspaceApi.releaseReplacementReview).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(/已冻结本次恢复审查/)).toBeNull();
});
it("expires a held condition review and does not keep it through refresh", async () => {
  await open();
  const button = await screen.findByRole("button", {
    name: "复核恢复条件（只读）",
  });
  vi.useFakeTimers();
  fireEvent.click(button);
  await act(async () => {
    await Promise.resolve();
  });
  expect(screen.getByText(/已冻结本次恢复审查/)).toBeInTheDocument();
  await act(async () => {
    vi.advanceTimersByTime(600000);
  });
  expect(
    screen.getByText("恢复条件审查已过期，请重新复核"),
  ).toBeInTheDocument();
  expect(workspaceApi.releaseReplacementReview).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(/已冻结本次恢复审查/)).toBeNull();
});
it("loads v2 only on explicit selection and shows missing separately from empty with no restore action", async () => {
  render(<WorkspaceFileRecovery root={root} />);
  const button = await screen.findByRole("button", { name: /核对恢复 a.txt/ });
  expect(workspaceApi.fileReplacementPreview).not.toHaveBeenCalled();
  fireEvent.click(button);
  const diff = await screen.findByLabelText("记录原文 → 候选全文");
  expect(diff).toHaveTextContent("original\\r\\n");
  expect(diff).toHaveTextContent("<script>candidate</script>");
  expect(diff.querySelector("script")).toBeNull();
  expect(
    screen.getByText(/目标路径：路径不存在（不是空文件）/),
  ).toBeInTheDocument();
  expect(screen.queryByLabelText("当前目标主文 → 记录原文")).toBeNull();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: "确认恢复此文件" })).toBeNull();
  expect(workspaceApi.fileRecoveryPreview).not.toHaveBeenCalled();
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
  expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
});
it("shows an installed empty candidate as present, not a missing path or success receipt", async () => {
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValue({
    ...view,
    observation: "candidate_present",
    target: "candidate",
    staged: "missing",
    currentBase64: "",
    candidateBase64: "",
  });
  await open();
  await screen.findByText("候选对象位于目标位置，原对象仍被保留");
  expect(screen.getByLabelText("当前目标主文 → 记录原文")).toHaveTextContent(
    "original",
  );
  expect(screen.queryByText("恢复写入及完整核验成功")).toBeNull();
});
it("shows a foreign target as a conflict without inventing current-file content", async () => {
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValue({
    ...view,
    observation: "conflict",
    target: "foreign",
  });
  await open();
  await screen.findByText("存在冲突或无法完整核对");
  expect(screen.getByText(/被其他文件占用，未读取其正文/)).toBeInTheDocument();
  expect(screen.queryByLabelText("当前目标主文 → 记录原文")).toBeNull();
});
it.each([
  { path: "different.txt" },
  { target: "missing", currentBase64: "" },
  { observation: "candidate_present" },
  { originalBase64: "%%%bad-base64" },
])("rejects mismatched or misleading native previews: %j", async (bad) => {
  vi.mocked(workspaceApi.fileReplacementPreview).mockResolvedValue({
    ...view,
    ...bad,
  } as WorkspaceFileReplacementPreview);
  await open();
  await screen.findByRole("alert");
  expect(screen.queryByLabelText("记录原文 → 候选全文")).toBeNull();
});
it("ignores late inspection after closing, without creating a snapshot or retry", async () => {
  let resolve!: (value: WorkspaceFileReplacementPreview) => void;
  vi.mocked(workspaceApi.fileReplacementPreview).mockReturnValueOnce(
    new Promise((r) => {
      resolve = r;
    }),
  );
  await open();
  fireEvent.click(
    await screen.findByRole("button", { name: "关闭替换记录预览" }),
  );
  await act(async () => resolve(view));
  expect(screen.queryByLabelText("记录原文 → 候选全文")).toBeNull();
  expect(workspaceApi.fileReplacementPreview).toHaveBeenCalledTimes(1);
  expect(workspaceApi.releaseFileSnapshot).not.toHaveBeenCalled();
});
it("clears previous observations on a failed refresh and expires local content", async () => {
  await open();
  await screen.findByLabelText("记录原文 → 候选全文");
  vi.mocked(workspaceApi.fileReplacementPreview).mockRejectedValueOnce(
    new Error("目录已替换"),
  );
  fireEvent.click(screen.getByRole("button", { name: "重新核对替换状态" }));
  await screen.findByText("目录已替换");
  expect(screen.queryByLabelText("记录原文 → 候选全文")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "重新核对替换状态" }));
  await screen.findByLabelText("记录原文 → 候选全文");
  // Reopen while fake time is active so the next observation owns a fake timer.
  fireEvent.click(screen.getByRole("button", { name: "关闭替换记录预览" }));
  vi.useFakeTimers();
  fireEvent.click(screen.getByRole("button", { name: /核对恢复 a.txt/ }));
  await act(async () => {
    await Promise.resolve();
  });
  await act(async () => {
    vi.advanceTimersByTime(600000);
  });
  expect(screen.queryByLabelText("记录原文 → 候选全文")).toBeNull();
  expect(screen.getByText("只读观察已过期，请重新核对")).toBeInTheDocument();
});
