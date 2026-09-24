import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AiProjectFileReview } from "./AiProjectFileReview";
import { useAiProjectFileReview } from "../store/aiProjectFileReview";
import { fileReviewDiff, visibleFileLine } from "./aiFileReviewDiff";
import { workspaceApi } from "../api/workspace";
import type { AiProvider } from "../types/models";
import { useWorkspacePanels } from "../store/workspacePanels";
import { useUiStore } from "../store/ui";
vi.mock("../lib/projectFileWriteGate", () => ({
  useProjectFileWriteCapability: () => ({
    available: true,
    reason: "isolated bound-helper test",
  }),
}));
vi.mock("../api/workspace", async (load) => ({
  ...(await load<typeof import("../api/workspace")>()),
  workspaceApi: {
    validateFileSnapshot: vi.fn(async () => {}),
    releaseFileSnapshot: vi.fn(async () => {}),
    applyFileEdit: vi.fn(),
    cancelFileOperation: vi.fn(async () => {}),
  },
}));
const provider = { id: "p", version: 1 } as AiProvider;
function setup() {
  const snapshot = {
    id: "snap",
    path: "a.txt",
    content: "\ufeffold\r\n",
    sha256: "a".repeat(64),
    size: 8,
    expiresInSeconds: 600,
  };
  const selectedFile = { snapshot, expiresAt: Date.now() + 600000 };
  useAiProjectFileReview.setState({
    review: {
      selection: {
        id: 1,
        rootId: "r",
        rootName: "project",
        expiresAt: Date.now() + 600000,
        files: [selectedFile],
        totalBytes: snapshot.size,
      },
      targetFile: selectedFile,
      approval: {
        selectionId: 1,
        sessionId: "s",
        providerId: "p",
        providerVersion: 1,
      },
      providerLabel: "frozen provider",
      requestId: "q",
      generationId: "g",
      status: "ready",
      error: null,
      proposal: {
        id: "e",
        path: "a.txt",
        baseSHA256: "a".repeat(64),
        content: "<script>data only</script>\n",
      },
    },
    error: null,
  });
}
afterEach(() => {
  cleanup();
  useAiProjectFileReview.getState().clear();
  useWorkspacePanels.setState({ tabs: [], activeId: "", splitId: null });
  useUiStore.setState({ rightOverviewCollapsed: false });
  vi.clearAllMocks();
});
it("opens the exact proposal as a read-only right-workspace tab on demand", () => {
  setup();
  useUiStore.setState({ rightOverviewCollapsed: true });
  render(<AiProjectFileReview sessionId="s" provider={provider} />);
  fireEvent.click(screen.getByRole("button", { name: "在右侧工作区预览差异" }));
  expect(useUiStore.getState().rightOverviewCollapsed).toBe(false);
  expect(useWorkspacePanels.getState()).toMatchObject({
    activeId: "ai-file-edit:e",
    tabs: [
      {
        id: "ai-file-edit:e",
        kind: "ai-file-edit",
        title: "AI 文件差异",
        proposalId: "e",
      },
    ],
  });
});
it("shows complete escaped bytes and requires a separate explicit checkbox before writing", async () => {
  setup();
  const view = render(
    <AiProjectFileReview sessionId="s" provider={provider} />,
  );
  const diff = screen.getByLabelText("完整文件差异");
  expect(diff.textContent).toContain("\\ufeffold\\r\\n");
  expect(diff.textContent).toContain("<script>data only</script>\\n");
  expect(view.container.querySelector("script")).toBeNull();
  expect(screen.getByRole("button", { name: "确认写入此文件" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "确认写入此文件" }));
  expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
  expect(workspaceApi.validateFileSnapshot).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "复核当前文件（不写入）" }),
  );
  await screen.findByText(/复核通过/);
  expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
  view.rerender(<AiProjectFileReview sessionId="other" provider={provider} />);
  await waitFor(() =>
    expect(useAiProjectFileReview.getState().review).toBeNull(),
  );
});

it("manual confirmation writes exactly once and displays the local backup receipt", async () => {
  setup();
  vi.mocked(workspaceApi.applyFileEdit).mockResolvedValueOnce({
    id: "e",
    status: "applied",
    backupPath: "D:/local/recovery/e.json",
  });
  render(<AiProjectFileReview sessionId="s" provider={provider} />);
  fireEvent.click(screen.getByRole("checkbox", { name: /我已核对完整差异/ }));
  fireEvent.click(screen.getByRole("button", { name: "确认写入此文件" }));
  await screen.findByText(/写入及完整字节核验成功/);
  expect(workspaceApi.applyFileEdit).toHaveBeenCalledWith(
    "r",
    "snap",
    "e",
    "<script>data only</script>\n",
  );
  expect(screen.queryByRole("button", { name: "确认写入此文件" })).toBeNull();
});
it("requests cancellation for the exact in-flight proposal without closing it", async () => {
  setup();
  let finish!: (value: {
    id: string;
    status: "cancelled";
    backupPath: string;
  }) => void;
  vi.mocked(workspaceApi.applyFileEdit).mockReturnValueOnce(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  render(<AiProjectFileReview sessionId="s" provider={provider} />);
  fireEvent.click(screen.getByRole("checkbox", { name: /我已核对完整差异/ }));
  fireEvent.click(screen.getByRole("button", { name: "确认写入此文件" }));
  fireEvent.click(await screen.findByRole("button", { name: "取消本次操作" }));
  expect(workspaceApi.cancelFileOperation).toHaveBeenCalledWith("r", "e");
  finish({ id: "e", status: "cancelled", backupPath: "" });
  expect(
    await screen.findByText(/原生确认或系统授权已取消/),
  ).toBeInTheDocument();
});
it("does not show a candidate until the generation is complete", () => {
  setup();
  useAiProjectFileReview.setState((state) => ({
    review: { ...state.review!, status: "receiving" },
  }));
  render(<AiProjectFileReview sessionId="s" provider={provider} />);
  expect(screen.queryByLabelText("完整文件差异")).toBeNull();
  expect(screen.queryByRole("button", { name: /复核当前/ })).toBeNull();
});
it.each([
  ["", "a"],
  ["a", ""],
  ["one\r\ntwo", "one\r\nthree"],
  ["x\nx\nx", "x\ny\nx"],
  ["\ufeffx\r\n", "x\n"],
  ["x\n", "x"],
  ["a\n".repeat(16000), "b\n".repeat(16000)],
])(
  "diff reconstructs both full byte sources without omitting context",
  (before, after) => {
    const parts = fileReviewDiff(before, after);
    expect(
      parts
        .filter((p) => p.kind !== "added")
        .flatMap((p) => p.lines)
        .join(""),
    ).toBe(before);
    expect(
      parts
        .filter((p) => p.kind !== "removed")
        .flatMap((p) => p.lines)
        .join(""),
    ).toBe(after);
    expect(visibleFileLine("\u202e\t\r\n")).toBe("\\u202e\\t\\r\\n");
  },
);
