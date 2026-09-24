import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  WorkspaceFilePreview,
  WorkspaceFiles,
  WorkspaceReview,
} from "./WorkspaceFiles";
import { useWorkspacePanels } from "../store/workspacePanels";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { workspaceApi } from "../api/workspace";
import { getAgentRun } from "../api/client";
import type { AgentRun } from "../types/models";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
vi.mock("../api/workspace", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/workspace")>()),
  workspaceApi: {
    list: vi.fn(),
    preview: vi.fn(),
    review: vi.fn(),
    fileSnapshot: vi.fn(),
    validateFileSnapshot: vi.fn(),
    releaseFileSnapshot: vi.fn().mockResolvedValue(undefined),
    fileWriteCapability: vi.fn(),
    applyFileEdit: vi.fn(),
    cancelFileOperation: vi.fn(),
  },
}));
vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getAgentRun: vi.fn(),
}));
const root = { id: "approved-root", name: "project", path: "D:/project" };
function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      {children}
    </QueryClientProvider>
  );
}
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useWorkspacePanels.setState({ tabs: [], activeId: "", root: null });
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  useAgentRunFileApply.setState({
    candidate: null,
    review: null,
    loading: false,
    error: null,
  });
});
describe("local workspace read panels", () => {
  it("prepares an exact file from its granted root only on click and reveals the chat", async () => {
    const stage = vi
      .spyOn(useAiProjectFiles.getState(), "stage")
      .mockResolvedValue(undefined);
    try {
      vi.mocked(workspaceApi.preview).mockResolvedValue({
        kind: "text",
        content: "preview",
        size: 7,
        truncated: false,
      });
      useWorkspacePanels.setState({ maximized: true });
      render(
        <MemoryRouter>
          <WorkspaceFilePreview
            panel={{
              id: "file",
              kind: "file",
              title: "a.txt",
              path: "src/a.txt",
              root,
            }}
          />
        </MemoryRouter>,
        { wrapper },
      );
      const button = await screen.findByRole("button", {
        name: /加入本条消息文件上下文/,
      });
      expect(stage).not.toHaveBeenCalled();
      fireEvent.click(button);
      expect(stage).toHaveBeenCalledWith(root, "src/a.txt");
      expect(useWorkspacePanels.getState().maximized).toBe(false);
    } finally {
      stage.mockRestore();
    }
  });
  it("uses the granted root and opens a file in its own tab", async () => {
    vi.mocked(workspaceApi.list).mockResolvedValue({
      entries: [
        { name: "readme.md", path: "readme.md", size: 12, directory: false },
      ],
      truncated: false,
    });
    render(
      <WorkspaceFiles
        panel={{ id: "files", kind: "files", title: "文件", root }}
      />,
      { wrapper },
    );
    fireEvent.click(await screen.findByRole("button", { name: /readme.md/ }));
    expect(workspaceApi.list).toHaveBeenCalledWith("approved-root", "");
    expect(useWorkspacePanels.getState().tabs[0]).toMatchObject({
      kind: "file",
      path: "readme.md",
      root,
    });
  });
  it("renders HTML as escaped source rather than executable markup", async () => {
    vi.mocked(workspaceApi.preview).mockResolvedValue({
      kind: "text",
      content: '<script>alert("test")</script>',
      size: 30,
      truncated: true,
    });
    const view = render(
      <MemoryRouter>
        <WorkspaceFilePreview
          panel={{
            id: "file",
            kind: "file",
            title: "file.html",
            path: "file.html",
            root,
          }}
        />
      </MemoryRouter>,
      { wrapper },
    );
    expect(
      await screen.findByText('<script>alert("test")</script>'),
    ).toBeInTheDocument();
    expect(view.container.querySelector("script")).toBeNull();
    expect(screen.getByText(/仅预览开头部分/)).toBeInTheDocument();
  });

  it("stages only the displayed text preview for a later explicit AI send", async () => {
    vi.mocked(workspaceApi.preview).mockResolvedValue({
      kind: "text",
      content: "const visibleToHuman = true;",
      size: 28,
      truncated: false,
    });
    render(
      <MemoryRouter>
        <WorkspaceFilePreview
          panel={{
            id: "file",
            kind: "file",
            title: "review.ts",
            path: "src/private/review.ts",
            root,
          }}
        />
      </MemoryRouter>,
      { wrapper },
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "交给智能体分析" }),
    );
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "本地文本预览",
      route: "/ai?workspace=files",
      scopes: [],
      attachment: "file_preview",
    });
    expect(pending?.prompt).toContain("const visibleToHuman = true;");
    expect(pending?.prompt).not.toContain(root.path);
    expect(pending?.prompt).not.toContain("src/private/review.ts");
    expect(workspaceApi.preview).toHaveBeenCalledWith(
      root.id,
      "src/private/review.ts",
    );
  });
  it("reviews a submitted Agent file against one selected target before native apply", async () => {
    const candidate = {
      runId: "018f0000-0000-7000-8000-000000000101",
      taskId: "018f0000-0000-7000-8000-000000000102",
      taskTitle: "生成报告",
      submissionId: "018f0000-0000-7000-8000-000000000103",
      artifactId: "018f0000-0000-7000-8000-000000000104",
      attempt: 1,
      index: 0,
      name: "report.md",
      mime: "text/markdown",
      content: "new report",
    };
    vi.mocked(getAgentRun).mockResolvedValue({
      id: candidate.runId,
      taskId: candidate.taskId,
      attempt: candidate.attempt,
      status: "succeeded",
      outputContract: {
        type: "file",
        name: candidate.name,
        mime: candidate.mime,
      },
      resultText: candidate.content,
      outputDeliveryStatus: "submitted",
      submissionId: candidate.submissionId,
      artifactId: candidate.artifactId,
    } as AgentRun);
    vi.mocked(workspaceApi.preview).mockResolvedValue({
      kind: "text",
      content: "old report",
      size: 10,
      truncated: false,
    });
    vi.mocked(workspaceApi.fileSnapshot).mockResolvedValue({
      id: "018f0000-0000-7000-8000-000000000109",
      path: "src/report.md",
      content: "old report",
      size: 10,
      sha256: "a".repeat(64),
      expiresInSeconds: 600,
    });
    vi.mocked(workspaceApi.fileWriteCapability).mockResolvedValue({
      available: true,
      reason: "已核验",
    });
    vi.mocked(workspaceApi.applyFileEdit).mockImplementation(
      async (_root, _snapshot, operationId) => ({
        id: operationId,
        status: "applied",
        backupPath: "recovery/record.json",
      }),
    );
    expect(useAgentRunFileApply.getState().select(candidate)).toBe(true);
    render(
      <MemoryRouter>
        <WorkspaceFilePreview
          panel={{
            id: "file",
            kind: "file",
            title: "report.md",
            path: "src/report.md",
            root,
          }}
        />
      </MemoryRouter>,
      { wrapper },
    );
    const prepare = await screen.findByRole("button", {
      name: "准备替换当前文件",
    });
    await waitFor(() => expect(prepare).toBeEnabled());
    fireEvent.click(prepare);
    await waitFor(() =>
      expect(useAgentRunFileApply.getState().loading).toBe(false),
    );
    expect(useAgentRunFileApply.getState().error).toBeNull();
    expect(useAgentRunFileApply.getState().review).not.toBeNull();
    expect(
      await screen.findByLabelText("Agent 产出完整文件差异"),
    ).toHaveTextContent("- old report");
    expect(screen.getByLabelText("Agent 产出完整文件差异")).toHaveTextContent(
      "+ new report",
    );
    fireEvent.click(
      await screen.findByRole("checkbox", {
        name: /同意用此 Agent 产出替换当前文件/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认写入此文件" }));
    expect(await screen.findByText(/写入及完整字节核验成功/)).toBeVisible();
    expect(workspaceApi.applyFileEdit).toHaveBeenCalledWith(
      root.id,
      "018f0000-0000-7000-8000-000000000109",
      expect.stringMatching(
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
      ),
      "new report",
    );
  });
  it("shows native failures and never substitutes fabricated Git changes", async () => {
    vi.mocked(workspaceApi.review).mockRejectedValue(new Error("Git 不可用"));
    render(
      <WorkspaceReview
        panel={{ id: "review", kind: "review", title: "review", root }}
      />,
      { wrapper },
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Git 不可用");
    expect(screen.queryByText("工作区干净")).toBeNull();
  });

  it("stages only the displayed Git review snapshot for a later explicit AI send", async () => {
    vi.mocked(workspaceApi.review).mockResolvedValue({
      branch: "feature/ai-assistant",
      status: " M apps/web/src/components/WorkspaceFiles.tsx",
      diff: "+const reviewed = true;",
    });
    render(
      <MemoryRouter>
        <WorkspaceReview
          panel={{ id: "review", kind: "review", title: "review", root }}
        />
      </MemoryRouter>,
      { wrapper },
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "交给智能体审查" }),
    );
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "Git 变更审查",
      route: "/ai?workspace=review",
      scopes: [],
      attachment: "git_review",
    });
    expect(pending?.prompt).toContain("feature/ai-assistant");
    expect(pending?.prompt).toContain("+const reviewed = true;");
    expect(pending?.prompt).not.toContain(root.path);
    expect(workspaceApi.review).toHaveBeenCalledWith(root.id, false);
  });
});
