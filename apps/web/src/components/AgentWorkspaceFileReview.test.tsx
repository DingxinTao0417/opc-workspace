import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { workspaceApi } from "../api/workspace";
import { useAiProjectFileReview } from "../store/aiProjectFileReview";
import { useUiStore } from "../store/ui";
import { useWorkspacePanels } from "../store/workspacePanels";
import { AgentWorkspace } from "./AgentWorkspace";

const releaseSnapshot = vi
  .spyOn(workspaceApi, "releaseFileSnapshot")
  .mockResolvedValue();

afterEach(() => {
  cleanup();
  useAiProjectFileReview.getState().clear();
  useWorkspacePanels.setState({ tabs: [], activeId: "", splitId: null });
  useUiStore.setState({
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
    rightPanelRequest: 0,
    workspacePanelsRequest: null,
    rightOverviewCollapsed: false,
  });
  releaseSnapshot.mockClear();
});

function showPreview() {
  const expiresAt = Date.now() + 600_000;
  const snapshot = {
    id: "snapshot",
    path: "src/example.ts",
    content: "const value = '<old>'\n",
    sha256: "a".repeat(64),
    size: 22,
    expiresInSeconds: 600,
  };
  const selection = {
    id: 1,
    rootId: "root",
    rootName: "workspace",
    expiresAt,
    files: [{ snapshot, expiresAt }],
    totalBytes: snapshot.size,
  };
  useAiProjectFileReview.setState({
    review: {
      selection,
      targetFile: selection.files[0],
      approval: {
        selectionId: 1,
        sessionId: "session",
        providerId: "provider",
        providerVersion: 1,
      },
      providerLabel: "Local model",
      requestId: "request",
      generationId: "generation",
      proposal: {
        id: "proposal",
        path: snapshot.path,
        baseSHA256: snapshot.sha256,
        content: "const value = '<script>new</script>'\n",
      },
      status: "ready",
      error: null,
    },
    error: null,
  });
  useWorkspacePanels.setState({
    tabs: [
      {
        id: "ai-file-edit:proposal",
        kind: "ai-file-edit",
        title: "AI 文件差异",
        proposalId: "proposal",
      },
    ],
    activeId: "ai-file-edit:proposal",
    splitId: null,
  });
}

it("shows only the exact in-memory proposal in a read-only workspace tab", () => {
  showPreview();
  render(
    <MemoryRouter>
      <AgentWorkspace />
    </MemoryRouter>,
  );

  expect(screen.getByRole("tab", { name: "AI 文件差异" })).toBeVisible();
  expect(screen.getByText("src/example.ts")).toBeVisible();
  const diff = screen.getByLabelText("右侧完整文件差异");
  expect(diff.textContent).toContain("'<old>'\\n");
  expect(diff.textContent).toContain("'<script>new</script>'\\n");
  expect(diff.querySelector("script")).toBeNull();
  expect(screen.getByText("待人工复核")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: /确认写入|复核当前文件/ }),
  ).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "新建工作区标签" }));
  expect(screen.queryByRole("menuitem", { name: "AI 文件差异" })).toBeNull();
});

it("does not restore a closed or replaced proposal from panel metadata", async () => {
  showPreview();
  render(
    <MemoryRouter>
      <AgentWorkspace />
    </MemoryRouter>,
  );
  useAiProjectFileReview.setState((state) => ({
    review: {
      ...state.review!,
      proposal: { ...state.review!.proposal!, id: "replacement" },
    },
  }));
  await waitFor(() =>
    expect(screen.getByText(/该提议已关闭、过期或被替换/)).toBeVisible(),
  );
  expect(screen.queryByLabelText("右侧完整文件差异")).toBeNull();

  useAiProjectFileReview.getState().clear();

  await waitFor(() =>
    expect(screen.getByText(/该提议已关闭、过期或被替换/)).toBeVisible(),
  );
  expect(screen.queryByLabelText("右侧完整文件差异")).toBeNull();
});
