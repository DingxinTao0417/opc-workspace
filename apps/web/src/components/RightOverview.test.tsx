import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { useWorkspacePanels } from "../store/workspacePanels";
import { RightOverview } from "./RightOverview";
import { useUiStore } from "../store/ui";

const mocks = vi.hoisted(() => ({
  knowledge: vi.fn(),
  terminalClose: vi.fn(),
}));
vi.mock("../api/workspace", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/workspace")>();
  return {
    ...original,
    workspaceApi: {
      ...original.workspaceApi,
      terminalClose: mocks.terminalClose,
    },
  };
});

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  cancelAgentRun: vi.fn(),
  getKnowledgeSources: mocks.knowledge,
  retryAgentRun: vi.fn(),
}));

vi.mock("../api/desktop", () => ({
  isDesktopRuntime: () => false,
}));

vi.mock("../api/hooks", () => ({
  useAiAgentFamilyQuery: () => ({
    data: undefined,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useAiSessionDelegatedRunsQuery: () => ({
    data: undefined,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useAiWorkPlanInboxQuery: () => ({
    data: { pages: [{ data: [], meta: { attention_total: 0 } }] },
    isError: false,
    isPending: false,
  }),
  useAiAgentInboxQuery: () => ({
    data: {
      pages: [{ data: [], meta: { approval_total: 0, review_total: 0 } }],
    },
    isError: false,
    isPending: false,
  }),
  useAgentRunsQuery: () => ({
    data: {
      items: [
        {
          id: "run-1",
          taskId: "task-1",
          taskTitle: "落地页任务",
          status: "succeeded",
          attempt: 1,
          model: "local-model",
          resultBytes: 12,
          errorCode: null,
          outputDeliveryStatus: "submitted",
          outputDeliveryErrorCode: null,
          submissionId: "submission-1",
          artifactId: "artifact-1",
          startedAt: "2026-09-13T01:00:00Z",
          completedAt: "2026-09-13T01:05:00Z",
          createdAt: "2026-09-13T01:00:00Z",
        },
      ],
      meta: {
        page: 1,
        pageSize: 50,
        total: 1,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 1,
      },
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useAgentRunQuery: () => ({
    data: {
      id: "run-1",
      taskId: "task-1",
      status: "succeeded",
      attempt: 1,
      model: "local-model",
      resultText: "交付正文",
      resultBytes: 12,
      errorCode: null,
      outputDeliveryStatus: "submitted",
      outputDeliveryErrorCode: null,
      submissionId: "submission-1",
      artifactId: "artifact-1",
      startedAt: "2026-09-13T01:00:00Z",
      completedAt: "2026-09-13T01:05:00Z",
      createdAt: "2026-09-13T01:00:00Z",
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useRetryAgentRunOutputDelivery: () => ({
    mutate: vi.fn(),
    isError: false,
    isPending: false,
  }),
  useAiProvidersQuery: () => ({ data: [{ id: "provider-1" }], isError: false }),
  useBackupsQuery: () => ({
    data: [{ id: "backup-1", createdAt: "2026-09-12T02:00:00Z" }],
    isError: false,
  }),
  useControlledFilesQuery: () => ({
    data: {
      items: [
        {
          id: "artifact-1",
          scope: "artifact",
          name: "报告.md",
          mimeType: "text/markdown",
          sizeBytes: 10,
          sha256: "ab",
          ownerLabel: "落地页任务",
          contentRoute: "/api/v1/artifacts/artifact-1",
          updatedAt: "2026-09-12T02:00:00Z",
        },
      ],
      meta: {
        page: 1,
        pageSize: 50,
        total: 1,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 1,
      },
    },
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useHealthQuery: () => ({
    data: {
      status: "ok",
      app: { name: "opc-workspace", version: "0.1.1", commit: "abc" },
      api: { version: "v1" },
      schema: { version: 71 },
    },
    isError: false,
  }),
  useInboxStatsQuery: () => ({
    data: { pending: 4, unread: 2 },
    isError: false,
  }),
  useRemindersQuery: () => ({
    data: { items: [{ id: "reminder-1", triggerAt: "2026-09-14T01:30:00Z" }] },
    isError: false,
  }),
  useTaskArtifactQuery: () => ({
    data: { contentText: "预览正文" },
    isError: false,
    isPending: false,
  }),
  useTodayStatsQuery: () => ({
    data: {
      tasks: { total: 5, completed: 2, overdue: 1 },
      focus: { sessions: 3, minutes: 45 },
    },
    isError: false,
  }),
}));

function renderOverview() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  mocks.knowledge.mockResolvedValue({
    items: [],
    meta: { page: 1, pageSize: 1, total: 7 },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <RightOverview />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  useUiStore.setState({
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
    rightPanelRequest: 0,
    workspacePanelsRequest: null,
    workspacePanelsRequestId: 0,
  });
  useWorkspacePanels.setState({
    tabs: [],
    activeId: "",
    splitId: null,
    splitRatio: 0.5,
    handledLayoutRequest: 0,
    root: null,
    maximized: false,
  });
});

function openTool(name: string) {
  fireEvent.click(screen.getByRole("button", { name: "新建工作区标签" }));
  fireEvent.click(screen.getByRole("menuitem", { name }));
}
describe("Agent right workspace", () => {
  it("starts with a tool launcher instead of business facts", () => {
    renderOverview();
    expect(screen.getByLabelText("智能体工作区")).toHaveAttribute(
      "id",
      "right-overview",
    );
    expect(
      screen.getByRole("tablist", { name: "工作区标签页" }),
    ).toBeInTheDocument();
    expect(screen.getByText("你的工作区")).toBeInTheDocument();
    expect(screen.queryByText("今日")).toBeNull();
    expect(screen.queryByText("收件箱")).toBeNull();
  });
  it("lists real agent runs and opens their deliverable", () => {
    renderOverview();
    openTool("Agent 执行");
    fireEvent.click(screen.getByRole("button", { name: /落地页任务/ }));
    expect(screen.getByText("交付正文")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /返回列表/ }));
    expect(screen.queryByText("交付正文")).toBeNull();
  });
  it("retains controlled-file previews as an explicit tool", () => {
    renderOverview();
    openTool("任务产出与附件");
    fireEvent.click(screen.getByRole("button", { name: /报告.md/ }));
    expect(screen.getByText("预览正文")).toBeInTheDocument();
  });
  it("explains browser limitations in web mode, without an iframe", () => {
    renderOverview();
    openTool("浏览器");
    expect(screen.getByText("在桌面版中浏览")).toBeInTheDocument();
    expect(document.querySelector("iframe")).toBeNull();
  });
  it("switches, splits and closes independent tabs", () => {
    renderOverview();
    openTool("文件");
    openTool("变更审查");
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "分栏显示" }));
    expect(screen.getAllByRole("tabpanel")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "关闭 文件" }));
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "关闭 变更审查" }));
    expect(screen.getByText("你的工作区")).toBeInTheDocument();
  });
  it("does not start a terminal merely by opening its tab", () => {
    renderOverview();
    openTool("终端");
    expect(
      screen.getByText(/以当前用户权限运行 PowerShell/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "请先选择工作目录" }),
    ).toBeDisabled();
  });
  it("opens an exact tool requested by the AI workspace link bridge", async () => {
    renderOverview();
    useUiStore.getState().setRightPanelTab("review");
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "变更审查" })).toBeInTheDocument(),
    );
    expect(screen.getByText("Git 变更审查")).toBeInTheDocument();
  });
  it("activates an existing tool instead of duplicating an AI-requested panel", async () => {
    renderOverview();
    useUiStore.getState().setRightPanelTab("review", "activate");
    await waitFor(() =>
      expect(screen.getAllByRole("tab", { name: "变更审查" })).toHaveLength(1),
    );
    useUiStore.getState().setRightPanelTab("review", "activate");
    await waitFor(() =>
      expect(screen.getAllByRole("tab", { name: "变更审查" })).toHaveLength(1),
    );
  });
  it("opens and arranges an AI-requested pair of fixed panels as a split", async () => {
    renderOverview();
    act(() =>
      useUiStore.getState().requestWorkspacePanels(["files", "review"], 0.62),
    );
    await waitFor(() => {
      const panels = useWorkspacePanels.getState();
      expect(panels.tabs.map((tab) => tab.kind)).toEqual(["files", "review"]);
      expect(panels.tabs.find((tab) => tab.id === panels.activeId)?.kind).toBe(
        "files",
      );
      expect(panels.tabs.find((tab) => tab.id === panels.splitId)?.kind).toBe(
        "review",
      );
      expect(panels.splitRatio).toBe(0.62);
    });
    expect(screen.getByRole("tab", { name: "文件" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "变更审查" })).toBeInTheDocument();
  });

  it("does not enqueue an AI layout request with an out-of-range ratio", () => {
    act(() =>
      useUiStore.getState().requestWorkspacePanels(["files", "review"], 0.9),
    );
    expect(useUiStore.getState().workspacePanelsRequest).toBeNull();
    expect(useUiStore.getState().workspacePanelsRequestId).toBe(0);
  });

  it("ignores an AI-requested split ratio outside the allowed range", () => {
    act(() =>
      useUiStore.getState().requestWorkspacePanels(["files", "review"], 0.9),
    );
    expect(useUiStore.getState().workspacePanelsRequest).toBeNull();
    expect(useUiStore.getState().workspacePanelsRequestId).toBe(0);
  });
  it("requires confirmation to stop a terminal and keeps the tab on native failure", async () => {
    const panels = useWorkspacePanels.getState();
    panels.open(
      { kind: "terminal", title: "运行中终端", terminalId: "pty-1" },
      "term",
    );
    panels.open({ kind: "review", title: "审查" }, "review");
    mocks.terminalClose.mockRejectedValueOnce(new Error("关闭失败"));
    renderOverview();
    fireEvent.click(screen.getByRole("button", { name: "关闭 运行中终端" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("关闭终端确认");
    expect(mocks.terminalClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "终止并关闭" }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("关闭失败"),
    );
    expect(screen.getByRole("tab", { name: "运行中终端" })).toBeInTheDocument();
    mocks.terminalClose.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole("button", { name: "终止并关闭" }));
    await waitFor(() =>
      expect(screen.queryByRole("tab", { name: "运行中终端" })).toBeNull(),
    );
    expect(mocks.terminalClose).toHaveBeenLastCalledWith("pty-1");
  });
});
