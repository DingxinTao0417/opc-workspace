import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { projectFileWriteGate } from "./projectFileWriteGate";
import { AiProjectFileReview } from "../components/AiProjectFileReview";
import { WorkspaceFileRecovery } from "../components/WorkspaceFileRecovery";
import { useAiProjectFileReview } from "../store/aiProjectFileReview";
import { workspaceApi } from "../api/workspace";
import type { AiProvider } from "../types/models";

const unavailableReason =
  "当前桌面构建未提供已绑定的单文件安全辅助程序；仍可只读审查差异";
// Keep the actual production gates. Native IPC reports an unbound build.
vi.mock("../api/workspace", async (load) => ({
  ...(await load<typeof import("../api/workspace")>()),
  workspaceApi: {
    validateFileSnapshot: vi.fn(async () => {}),
    releaseFileSnapshot: vi.fn(async () => {}),
    fileWriteCapability: vi.fn(async () => ({
      available: false,
      reason: "当前桌面构建未提供已绑定的单文件安全辅助程序；仍可只读审查差异",
    })),
    applyFileEdit: vi.fn(),
    cancelFileOperation: vi.fn(async () => {}),
    applyFileRecovery: vi.fn(),
    fileRecoveryList: vi.fn(),
    fileRecoveryPreview: vi.fn(),
  },
}));
afterEach(() => {
  cleanup();
  useAiProjectFileReview.getState().clear();
  vi.clearAllMocks();
});

it("keeps the AI review read-only when the installed helper is unavailable", async () => {
  expect(projectFileWriteGate.enabled).toBe(false);
  const provider = { id: "provider", version: 1 } as AiProvider;
  const snapshot = {
    id: "snapshot",
    path: "a.txt",
    content: "original",
    size: 8,
    sha256: "a".repeat(64),
    expiresInSeconds: 600,
  };
  const selectedFile = { snapshot, expiresAt: Date.now() + 600000 };
  useAiProjectFileReview.setState({
    error: null,
    review: {
      selection: {
        id: 1,
        rootId: "root",
        rootName: "project",
        expiresAt: Date.now() + 600000,
        files: [selectedFile],
        totalBytes: snapshot.size,
      },
      targetFile: selectedFile,
      approval: {
        selectionId: 1,
        sessionId: "session",
        providerId: provider.id,
        providerVersion: 1,
      },
      providerLabel: "provider",
      requestId: "request",
      generationId: "generation",
      status: "ready",
      error: null,
      proposal: {
        id: "proposal",
        path: "a.txt",
        baseSHA256: "a".repeat(64),
        content: "candidate",
      },
    },
  });
  render(<AiProjectFileReview sessionId="session" provider={provider} />);
  expect(screen.getByLabelText("完整文件差异")).toHaveTextContent("candidate");
  expect(await screen.findByText(unavailableReason)).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: "确认写入此文件" })).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "复核当前文件（不写入）" }),
  );
  await screen.findByText(/复核通过/);
  expect(useAiProjectFileReview.getState().review?.status).toBe("checked");
  expect(workspaceApi.applyFileEdit).not.toHaveBeenCalled();
});

it("permits explicit recovery inspection but offers no production restore control", async () => {
  const id = "018f0000-0000-7000-8000-000000000201";
  const snapshotId = "018f0000-0000-7000-8000-000000000202";
  vi.mocked(workspaceApi.fileRecoveryList).mockResolvedValue({
    records: [{ id, path: "a.txt", createdAt: 1, restores: null }],
    damaged: 0,
    directory: "local recovery",
  });
  vi.mocked(workspaceApi.fileRecoveryPreview).mockResolvedValue({
    id,
    snapshotId,
    path: "a.txt",
    currentBase64: btoa("candidate"),
    originalBase64: btoa("original"),
    expiresInSeconds: 600,
  });
  render(
    <WorkspaceFileRecovery
      root={{ id: "root", name: "project", path: "D:/project" }}
    />,
  );
  const button = await screen.findByRole("button", { name: /核对恢复 a.txt/ });
  expect(workspaceApi.fileRecoveryPreview).not.toHaveBeenCalled();
  fireEvent.click(button);
  expect(await screen.findByLabelText("完整恢复差异")).toHaveTextContent(
    "original",
  );
  expect(screen.getByText(projectFileWriteGate.reason)).toBeInTheDocument();
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect(screen.queryByRole("button", { name: "确认恢复此文件" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "取消恢复预览" }));
  expect(workspaceApi.releaseFileSnapshot).toHaveBeenCalledWith(
    "root",
    snapshotId,
  );
  expect(workspaceApi.applyFileRecovery).not.toHaveBeenCalled();
});
