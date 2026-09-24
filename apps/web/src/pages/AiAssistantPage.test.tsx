import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AiAssistantPage } from "./AiAssistantPage";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { workspaceApi } from "../api/workspace";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { getAiGeneration } from "../api/aiActions";
import { getAiWorkPlan, getAiWorkPlanContinuation } from "../api/aiWorkPlan";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import type { AiWorkspaceScope } from "../types/models";
import { getAiPlanContinuation } from "../api/aiPlanContinuation";
import {
  continuationFixture,
  continuationSessionId,
} from "../test/aiContinuationFixture";
vi.mock("../api/aiPlanContinuation", async () => ({
  ...(await vi.importActual("../api/aiPlanContinuation")),
  getAiPlanContinuation: vi.fn(async () => null),
}));

vi.mock("../components/AiWorkPlan", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("../components/AiWorkPlan")>();
  return {
    ...original,
    AiWorkPlan: (props: Parameters<typeof original.AiWorkPlan>[0]) => {
      mockState.planProps.push(props);
      return mockState.capturePlanOnly ? null : (
        <original.AiWorkPlan {...props} />
      );
    },
  };
});

vi.mock("../components/AiWorkspaceActions", () => ({
  AiWorkspaceActions: ({
    generationId,
    sessionId,
    onContinue,
  }: {
    generationId: string;
    sessionId?: string;
    onContinue?: (request: AiActionContinuation) => void;
  }) =>
    onContinue ? (
      <button
        onClick={() =>
          onContinue({
            generationId,
            prompt: "根据真实操作回执继续原需求。",
            scopes: ["work", "actions"],
            ...(mockState.recheckProposalId
              ? {
                  recheckProposalId: mockState.recheckProposalId,
                  sourceSessionId: sessionId,
                }
              : {}),
          })
        }
      >
        模拟接续操作
      </button>
    ) : null,
  useAiWorkspaceActions: () => ({ data: [], isSuccess: true }),
}));

vi.mock("../api/aiWorkPlan", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/aiWorkPlan")>()),
  getAiWorkPlan: vi.fn(async () => null),
  getAiWorkPlanContinuation: vi.fn(),
}));

afterEach(() => {
  vi.mocked(getAiPlanContinuation).mockResolvedValue(null);
  cleanup();
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  useAiChatStore.setState({
    input: "",
    lastSessionId: "",
    activeSessionId: "",
    streaming: null,
    interrupted: null,
    retainedTurns: [],
    acceptedCommand: null,
    accessRequests: {},
  });
  mockState.hasNextPage = false;
  mockState.streaming = null;
  mockState.isStreaming = false;
  mockState.sentMessage = null;
  mockState.recheckProposalId = undefined;
  mockState.capturePlanOnly = false;
  mockState.planProps = [];
  mockState.isFetchingNextPage = false;
  mockState.previewData = null;
  mockState.searchKnowledge.mockReset();
  mockState.searchKnowledge.mockResolvedValue({
    items: [],
    query: "",
    sourceIds: [],
  });
  mockState.getAiRunSteps.mockClear();
  mockState.getAiUsageSummary.mockClear();
  vi.mocked(getAiGeneration).mockReset();
  vi.mocked(getAiWorkPlan).mockReset();
  vi.mocked(getAiWorkPlan).mockResolvedValue(null);
  vi.mocked(getAiWorkPlanContinuation).mockReset();
});

const mockState = vi.hoisted(() => {
  const mutation = () => ({
    mutateAsync: vi.fn(async (_input?: unknown) => ({})),
    isPending: false,
    error: null as unknown,
  });
  return {
    capturePlanOnly: false,
    planProps: [] as Array<{
      sessionId: string;
      onContinue?: (prompt: string, scopes: AiWorkspaceScope[]) => void;
    }>,
    recheckProposalId: undefined as string | undefined,
    providers: [] as unknown[],
    sessions: [] as unknown[],
    messagesPages: [] as { data: unknown[]; meta: { has_more: boolean } }[],
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: vi.fn(async () => ({})),
    streaming: null as {
      sessionId: string;
      text: string;
      progress?: import("../types/models").AiRunProgress[];
    } | null,
    isStreaming: false,
    sentMessage: null as string | null,
    taskDetail: null as {
      title: string;
      status: string;
      dueDate: string | null;
    } | null,
    send: vi.fn(
      async (): Promise<{
        sessionId: string;
        cancelled: boolean;
        accepted: boolean;
        error: string | null;
        errorCode: string | null;
      }> => ({
        sessionId: "",
        cancelled: false,
        accepted: true,
        error: null,
        errorCode: null,
      }),
    ),
    stop: vi.fn(),
    createSession: mutation(),
    deleteSession: mutation(),
    setSettingsOpen: vi.fn(),
    toggleAgentRailCollapsed: vi.fn(),
    toggleRightOverviewCollapsed: vi.fn(),
    agentRailCollapsed: false,
    rightOverviewCollapsed: false,
    confirmTask: mutation(),
    createMemory: mutation(),
    previewData: null as null | Record<string, unknown>,
    previewContext: {
      mutateAsync: vi.fn(async () => mockState.previewData),
      isPending: false,
      error: null as unknown,
      reset: vi.fn(),
    },
    searchKnowledge: vi.fn(
      async (_query: string, _sourceIds: string[], _limit: number) => ({
        items: [] as Array<Record<string, unknown>>,
        query: "",
        sourceIds: [] as string[],
      }),
    ),
    getAiRunSteps: vi.fn(async (generationId: string) => ({
      items: [
        {
          id: "step-1",
          generationId,
          sequence: 1,
          kind: "generation" as const,
          status: "succeeded" as const,
          turnIndex: null,
          toolName: null,
          startedAt: "2026-09-08T12:00:00Z",
          completedAt: "2026-09-08T12:00:01Z",
          durationMs: 1000,
          inputBytes: 2048,
          outputBytes: 256,
          inputTokens: 100,
          outputTokens: 20,
          tokenSource: "provider" as const,
          errorCode: null,
        },
        {
          id: "step-2",
          generationId,
          sequence: 2,
          kind: "model_turn" as const,
          status: "succeeded" as const,
          turnIndex: 1,
          toolName: null,
          startedAt: "2026-09-08T12:00:00Z",
          completedAt: "2026-09-08T12:00:01Z",
          durationMs: 900,
          inputBytes: 2048,
          outputBytes: 256,
          inputTokens: 100,
          outputTokens: 20,
          tokenSource: "provider" as const,
          errorCode: null,
        },
        {
          id: "step-3",
          generationId,
          sequence: 3,
          kind: "tool_call" as const,
          status: "succeeded" as const,
          turnIndex: null,
          toolName: "workspace_search",
          startedAt: "2026-09-08T12:00:01Z",
          completedAt: "2026-09-08T12:00:01Z",
          durationMs: 2,
          inputBytes: 52,
          outputBytes: 14,
          inputTokens: null,
          outputTokens: null,
          tokenSource: "unknown" as const,
          errorCode: null,
        },
      ],
      meta: {
        generationId,
        status: "completed" as const,
        total: 3,
        inputBytes: 2048,
        outputBytes: 256,
        durationMs: 1000,
        inputTokens: 100,
        outputTokens: 20,
        tokenSource: "provider" as const,
      },
    })),
    getAiUsageSummary: vi.fn(
      async ({
        sessionId,
        trendDays = 7,
      }: {
        sessionId: string;
        trendDays?: number;
      }) => ({
        scope: { sessionId, providerId: null },
        totals: {
          totalGenerations: 4,
          completedGenerations: 2,
          failedGenerations: 1,
          cancelledGenerations: 0,
          activeGenerations: 1,
          providerUsageGenerations: 2,
          unknownUsageGenerations: 1,
          inputTokens: 450,
          outputTokens: 90,
          inputBytes: 8192,
          outputBytes: 2048,
          durationMs: 3500,
        },
        providers: [
          {
            providerId: "provider-1",
            providerName: "DeepSeek",
            providerKind: "remote" as const,
            providerProtocol: "openai_chat" as const,
            model: "deepseek-chat",
            totalGenerations: 4,
            completedGenerations: 2,
            failedGenerations: 1,
            cancelledGenerations: 0,
            activeGenerations: 1,
            providerUsageGenerations: 2,
            unknownUsageGenerations: 1,
            inputTokens: 450,
            outputTokens: 90,
            inputBytes: 8192,
            outputBytes: 2048,
            durationMs: 3500,
          },
        ],
        trendDays,
        trend: Array.from({ length: trendDays }, (_unused, index) => ({
          day: `2026-09-${String(index + 2).padStart(2, "0")}`,
          totalGenerations: index === trendDays - 1 ? 3 : 0,
          completedGenerations: index === trendDays - 1 ? 2 : 0,
          failedGenerations: index === trendDays - 1 ? 1 : 0,
          cancelledGenerations: 0,
          activeGenerations: 0,
          providerUsageGenerations: index === trendDays - 1 ? 2 : 0,
          unknownUsageGenerations: index === trendDays - 1 ? 1 : 0,
          inputTokens: index === trendDays - 1 ? 450 : 0,
          outputTokens: index === trendDays - 1 ? 90 : 0,
          inputBytes: index === trendDays - 1 ? 8192 : 0,
          outputBytes: index === trendDays - 1 ? 2048 : 0,
          durationMs: index === trendDays - 1 ? 3500 : 0,
        })),
      }),
    ),
    navigate: vi.fn(),
  };
});

vi.mock("../api/hooks", () => ({
  aiUsageSummaryQueryKey: (sessionId: string, trendDays = 7) => [
    "ai",
    "usage-summary",
    sessionId,
    trendDays,
  ],
  useAiProvidersQuery: () => ({
    data: mockState.providers,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useAiSessionsQuery: () => ({
    data: mockState.sessions,
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useAiMessagesInfiniteQuery: () => ({
    data: { pages: mockState.messagesPages, pageParams: [] },
    isPending: false,
    isError: false,
    error: null,
    refetch: vi.fn(),
    hasNextPage: mockState.hasNextPage,
    isFetchingNextPage: mockState.isFetchingNextPage,
    fetchNextPage: mockState.fetchNextPage,
  }),
  useAiChatStream: () => ({
    streaming: mockState.streaming,
    isStreaming: mockState.isStreaming,
    streamError: null,
    sentMessage: mockState.sentMessage,
    send: mockState.send,
    stop: mockState.stop,
  }),
  useCreateAiSession: () => mockState.createSession,
  useCreateAiMemory: () => mockState.createMemory,
  usePreviewAiBusinessContext: () => ({
    ...mockState.previewContext,
    data: mockState.previewData,
  }),
  useDeleteAiSession: () => mockState.deleteSession,
  useConfirmAiMessageTask: () => mockState.confirmTask,
  useTaskQuery: (id: string | null) => ({
    data: id && mockState.taskDetail ? mockState.taskDetail : undefined,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("../api/aiActions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/aiActions")>()),
  getAiGeneration: vi.fn(),
  getAiMemoryDecision: vi.fn(async () => ({
    id: "proposal-1",
    session_id: "session-1",
    content: "回答保持简洁",
    status: "pending",
    memory_id: null,
  })),
  getAiCompactionStatus: vi.fn(async () => ({
    session_id: "session-1",
    status: "idle",
    partial_message: false,
    compacted_message_count: 0,
    error_code: null,
  })),
}));

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  searchKnowledge: (query: string, sourceIds: string[], limit: number) =>
    mockState.searchKnowledge(query, sourceIds, limit),
  getAiRunSteps: (generationId: string) =>
    mockState.getAiRunSteps(generationId),
  getAiUsageSummary: (input: { sessionId: string; trendDays?: number }) =>
    mockState.getAiUsageSummary(input),
}));

vi.mock("../store/ui", () => ({
  useUiStore: (selector: (state: unknown) => unknown) =>
    selector({
      agentRailCollapsed: mockState.agentRailCollapsed,
      rightOverviewCollapsed: mockState.rightOverviewCollapsed,
      setSettingsOpen: mockState.setSettingsOpen,
      toggleAgentRailCollapsed: mockState.toggleAgentRailCollapsed,
      toggleRightOverviewCollapsed: mockState.toggleRightOverviewCollapsed,
      collapseAgentWorkspace: vi.fn(),
    }),
}));

vi.mock("react-router-dom", async (importOriginal) => {
  const original = await importOriginal<typeof import("react-router-dom")>();
  return {
    ...original,
    useNavigate: () => mockState.navigate,
  };
});

vi.mock("../components/ProjectSelect", () => ({
  ProjectSelect: (props: {
    ariaLabel: string;
    onChange: (id: string) => void;
  }) => (
    <button
      aria-label={props.ariaLabel}
      data-testid="project-select"
      onClick={() => props.onChange("project-context")}
      type="button"
    >
      选择项目
    </button>
  ),
}));

vi.mock("../components/TaskSelect", () => ({
  TaskSelect: (props: {
    ariaLabel: string;
    onChange: (id: string) => void;
  }) => (
    <button
      aria-label={props.ariaLabel}
      onClick={() => props.onChange("task-context")}
      type="button"
    >
      选择任务
    </button>
  ),
}));

vi.mock("../components/ClientSelect", () => ({
  ClientSelect: (props: {
    ariaLabel: string;
    onChange: (id: string) => void;
  }) => (
    <button
      aria-label={props.ariaLabel}
      onClick={() => props.onChange("client-context")}
      type="button"
    >
      选择客户
    </button>
  ),
}));

const readyProvider = {
  id: "provider-1",
  name: "DeepSeek",
  kind: "remote",
  protocol: "openai_chat",
  base_url: "https://api.deepseek.com/v1",
  model: "deepseek-chat",
  status: "ready",
  health_status: "healthy",
  health_error_code: null,
  has_key: true,
  last_health_at: "2026-09-01T12:00:00Z",
  version: 2,
  created_at: "2026-09-01T11:00:00Z",
  updated_at: "2026-09-01T12:00:00Z",
};

const activeSession = {
  id: "session-1",
  title: "新会话",
  persist: true,
  compacted_message_count: 0,
  version: 1,
  created_at: "2026-09-01T11:00:00Z",
  updated_at: "2026-09-01T12:00:00Z",
};

function assistantMessage(overrides: Record<string, unknown> = {}) {
  return {
    id: "message-1",
    session_id: "session-1",
    role: "assistant",
    status: "completed",
    content:
      '好的，建议如下\n[opc:task]{"title":"写周报","due":"2026-09-02"}[/opc:task]',
    task_id: null,
    task_title_snapshot: null,
    context_provider: null,
    context_sources: [],
    created_at: "2026-09-01T12:00:00Z",
    ...overrides,
  };
}

function renderPage(initialEntry = "/ai") {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <AiAssistantPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function taskChip(text: string): HTMLElement {
  const chips = screen
    .getAllByRole("button")
    .filter((button) => button.textContent?.includes(text));
  if (chips.length === 0) {
    throw new Error(`task chip not found: ${text}`);
  }
  return chips[0];
}

describe("AiAssistantPage", () => {
  it.each(["success", "changed", "draft-race"])(
    "checks exact files on the real send path: %s",
    async (mode) => {
      const sessionId = "018f0000-0000-7000-8000-000000000151";
      const providerId = "018f0000-0000-7000-8000-000000000152";
      mockState.providers = [{ ...readyProvider, id: providerId }];
      mockState.sessions = [{ ...activeSession, id: sessionId }];
      mockState.messagesPages = [{ data: [], meta: { has_more: false } }];
      mockState.send.mockClear();
      useAiChatStore.setState({ activeSessionId: sessionId });
      const snapshotSpy = vi
        .spyOn(workspaceApi, "fileSnapshot")
        .mockResolvedValue({
          id: "018f0000-0000-7000-8000-000000000153",
          path: "file.txt",
          content: "private-file",
          size: 12,
          sha256: "a".repeat(64),
          expiresInSeconds: 600,
        });
      const releaseSpy = vi
        .spyOn(workspaceApi, "releaseFileSnapshot")
        .mockResolvedValue(undefined);
      const validateSpy = vi
        .spyOn(workspaceApi, "validateFileSnapshot")
        .mockResolvedValue(undefined);
      try {
        renderPage();
        await act(async () => {
          await useAiProjectFiles
            .getState()
            .stage(
              { id: "root", name: "project", path: "D:/private" },
              "file.txt",
            );
        });
        fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
          target: { value: "分析这个文件" },
        });
        fireEvent.click(screen.getByRole("button", { name: "发送" }));
        await screen.findByText("请先确认本条消息的文件外发授权");
        expect(mockState.send).not.toHaveBeenCalled();
        fireEvent.click(
          screen.getByRole("button", {
            name: "仅授权下一条消息使用这些文件",
          }),
        );
        let complete!: () => void;
        if (mode === "changed")
          validateSpy.mockRejectedValueOnce(new Error("文件已变化"));
        if (mode === "draft-race")
          validateSpy.mockReturnValueOnce(
            new Promise<void>((resolve) => {
              complete = resolve;
            }),
          );
        fireEvent.click(screen.getByRole("button", { name: "发送" }));
        if (mode === "draft-race") {
          fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
            target: { value: "新的草稿" },
          });
          await act(async () => complete());
        }
        if (mode === "success") {
          await waitFor(() =>
            expect(mockState.send).toHaveBeenCalledWith(
              expect.objectContaining({
                message: "分析这个文件",
                projectFiles: expect.objectContaining({
                  provider_id: providerId,
                  session_id: sessionId,
                  files: [
                    {
                      path: "file.txt",
                      content: "private-file",
                      sha256: "a".repeat(64),
                    },
                  ],
                }),
              }),
            ),
          );
          expect(useAiProjectFiles.getState().selection).toBeNull();
          expect(releaseSpy).not.toHaveBeenCalled(); // Baseline transferred to the ephemeral review, disclosure consumed.
          fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
            target: { value: "下一条" },
          });
          fireEvent.click(screen.getByRole("button", { name: "发送" }));
          await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(2));
          expect(
            (
              mockState.send.mock.calls[1] as unknown as [
                Record<string, unknown>,
              ]
            )[0],
          ).not.toHaveProperty("projectFiles");
        } else {
          await screen.findByText(
            mode === "changed"
              ? "文件已变化"
              : "对话、模型或消息已变化，请重新核对后发送",
          );
          expect(mockState.send).not.toHaveBeenCalled();
        }
      } finally {
        cleanup();
        useAiProjectFiles.getState().clear();
        snapshotSpy.mockRestore();
        releaseSpy.mockRestore();
        validateSpy.mockRestore();
      }
    },
  );
  it.each([false, true])(
    "consumes an uncertain receipt source after background acceptance without clearing a newer draft (recheck: %s)",
    async (recheck) => {
      const generationId = "018f0000-0000-7000-8000-000000000764";
      const recheckProposalId = "018f0000-0000-7000-8000-000000000766";
      mockState.recheckProposalId = recheck ? recheckProposalId : undefined;
      const removalLabel = recheck ? "移除重新核验上下文" : "移除操作回执";
      mockState.providers = [readyProvider];
      mockState.sessions = [activeSession];
      mockState.messagesPages = [
        {
          data: [
            assistantMessage({
              generation_id: generationId,
              content: "已确认",
            }),
          ],
          meta: { has_more: false },
        },
      ];
      mockState.send.mockClear();
      mockState.send.mockResolvedValueOnce({
        sessionId: activeSession.id,
        cancelled: false,
        accepted: false,
        error: "network",
        errorCode: "AI_STREAM_ERROR",
      });
      renderPage();
      fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
      fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
      await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(1));
      const actionReceiptAttachmentId = (
        mockState.send.mock.calls as unknown as Array<[Record<string, unknown>]>
      )[0][0].actionReceiptAttachmentId as string;
      expect(screen.getByRole("button", { name: removalLabel })).toBeVisible();
      fireEvent.change(screen.getByLabelText("消息输入框"), {
        target: { value: "尚未发送的新草稿" },
      });
      if (recheck) {
        act(() =>
          useAiChatStore.setState({
            acceptedCommand: {
              sequence: 1,
              requestId: "different-recheck-request",
              owner: "main",
              sessionId: activeSession.id,
              actionReceiptGenerationId: generationId,
              actionReceiptAttachmentId,
              actionRecheckProposalId: "018f0000-0000-7000-8000-000000000767",
            },
          }),
        );
        expect(
          screen.getByRole("button", { name: removalLabel }),
        ).toBeVisible();
      }
      act(() =>
        useAiChatStore.setState({
          acceptedCommand: {
            sequence: 2,
            requestId: "accepted-request",
            owner: "main",
            sessionId: activeSession.id,
            actionReceiptGenerationId: generationId,
            actionReceiptAttachmentId,
            ...(recheck ? { actionRecheckProposalId: recheckProposalId } : {}),
          },
        }),
      );
      expect(screen.queryByRole("button", { name: removalLabel })).toBeNull();
      expect(screen.getByLabelText("消息输入框")).toHaveValue(
        "尚未发送的新草稿",
      );
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
      await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(2));
      expect(
        (
          mockState.send.mock.calls as unknown as Array<
            [Record<string, unknown>]
          >
        )[1][0],
      ).not.toHaveProperty("actionReceiptGenerationId");
      expect(
        (
          mockState.send.mock.calls as unknown as Array<
            [Record<string, unknown>]
          >
        )[1][0],
      ).not.toHaveProperty("actionRecheckProposalId");
      expect(
        (
          mockState.send.mock.calls as unknown as Array<
            [Record<string, unknown>]
          >
        )[1][0].workspace,
      ).toBeUndefined();
    },
  );
  it("keeps a newly prepared copy of the same recheck when an older send is recovered", async () => {
    const generationId = "018f0000-0000-7000-8000-000000000764";
    const recheckProposalId = "018f0000-0000-7000-8000-000000000766";
    mockState.recheckProposalId = recheckProposalId;
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({ generation_id: generationId, content: "已确认" }),
        ],
        meta: { has_more: false },
      },
    ];
    mockState.send.mockClear();
    mockState.send.mockResolvedValueOnce({
      sessionId: activeSession.id,
      cancelled: false,
      accepted: false,
      error: "network",
      errorCode: "AI_STREAM_ERROR",
    });
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(1));
    const previousAttachmentId = (
      mockState.send.mock.calls as unknown as Array<[Record<string, unknown>]>
    )[0][0].actionReceiptAttachmentId as string;
    expect(previousAttachmentId).toEqual(expect.any(String));
    fireEvent.click(screen.getByRole("button", { name: "移除重新核验上下文" }));
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.change(screen.getByLabelText("消息输入框"), {
      target: { value: "这是重新准备的草稿" },
    });
    act(() =>
      useAiChatStore.setState({
        acceptedCommand: {
          sequence: 1,
          requestId: "old-request",
          owner: "main",
          sessionId: activeSession.id,
          actionReceiptGenerationId: generationId,
          actionRecheckProposalId: recheckProposalId,
          actionReceiptAttachmentId: previousAttachmentId,
        },
      }),
    );
    expect(
      screen.getByRole("button", { name: "移除重新核验上下文" }),
    ).toBeVisible();
    expect(screen.getByLabelText("消息输入框")).toHaveValue(
      "这是重新准备的草稿",
    );
    expect(screen.getByRole("button", { name: "发送" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(2));
    const next = (
      mockState.send.mock.calls as unknown as Array<[Record<string, unknown>]>
    )[1][0];
    expect(next.actionReceiptAttachmentId).not.toBe(previousAttachmentId);
    expect(next.actionRecheckProposalId).toBe(recheckProposalId);
    expect(next.workspace).toBeDefined();
  });
  it("preserves an existing draft and clears prior permissions when continuing a plan", async () => {
    const sessionId = "018f0000-0000-7000-8000-000000000763";
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, id: sessionId }];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("尚未发送的想法");
    const plan: NonNullable<Awaited<ReturnType<typeof getAiWorkPlan>>> = {
      session_id: sessionId,
      generation_id: sessionId,
      generation_status: "completed",
      version: 1,
      title: "待继续计划",
      created_at: "2026-09-20T00:00:00Z",
      as_of: "2026-09-20T00:00:00Z",
      steps: [
        {
          id: "analysis",
          title: "分析下一步",
          kind: "analysis",
          depends_on: [],
          state: "pending",
          ready: true,
          satisfied: false,
        },
      ],
    };
    vi.mocked(getAiWorkPlan).mockResolvedValue(plan);
    vi.mocked(getAiWorkPlanContinuation).mockResolvedValue({
      plan,
      ready: true,
      reason: "ready",
    });
    renderPage(`/ai?session=${sessionId}&plan=1`);
    await screen.findByRole("button", { name: "继续此计划" });
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /^财务查询/ }));
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "继续此计划" }));
    await waitFor(() =>
      expect(useAiChatStore.getState().input).toMatch(
        /^尚未发送的想法\n\n继续当前执行计划/,
      ),
    );
    expect(getAiWorkPlanContinuation).toHaveBeenCalledWith(
      sessionId,
      1,
      expect.any(AbortSignal),
    );
    expect(
      screen.getByRole("checkbox", { name: /^财务查询/ }),
    ).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.queryByRole("button", { name: "移除操作回执" })).toBeNull();
    expect(mockState.send).not.toHaveBeenCalled();
  });
  it.each(["session", "provider", "provider-version", "streaming"])(
    "ignores a late plan continuation after %s changes away and back",
    (source) => {
      mockState.capturePlanOnly = true;
      mockState.providers = [readyProvider];
      mockState.sessions = [activeSession];
      mockState.messagesPages = [];
      mockState.send.mockClear();
      useAiChatStore.getState().setInput("原草稿");
      renderPage();
      const oldContinue = mockState.planProps.at(-1)?.onContinue;
      expect(oldContinue).toBeTypeOf("function");
      const originalSession = useAiChatStore.getState().activeSessionId;
      act(() => {
        if (source === "session")
          useAiChatStore.getState().setActiveSessionId("another-session");
        if (source === "provider")
          mockState.providers = [{ ...readyProvider, id: "another-provider" }];
        if (source === "provider-version")
          mockState.providers = [
            { ...readyProvider, version: readyProvider.version + 1 },
          ];
        if (source === "streaming") mockState.isStreaming = true;
        useAiChatStore.getState().setInput("切换中的草稿");
      });
      act(() => {
        mockState.providers = [readyProvider];
        mockState.isStreaming = false;
        useAiChatStore.getState().setActiveSessionId(originalSession);
        useAiChatStore.getState().setInput("返回后新草稿");
      });
      fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
      fireEvent.click(screen.getByRole("checkbox", { name: /^财务查询/ }));
      fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
      const grantButton = document.querySelector(
        'button[data-confirmed="true"]',
      );
      expect(grantButton).not.toBeNull();
      act(() => oldContinue!("已过期的计划接续", ["work", "actions"]));
      expect(screen.getByLabelText("消息输入框")).toHaveValue("返回后新草稿");
      expect(document.querySelector('button[data-confirmed="true"]')).toBe(
        grantButton,
      );
      expect(screen.queryByRole("checkbox", { name: /^工作事项/ })).toBeNull();
      expect(mockState.send).not.toHaveBeenCalled();
    },
  );
  it("keeps the plan callback stable while typing and applies it to the latest unsent draft", () => {
    mockState.capturePlanOnly = true;
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    renderPage();
    const continuePlan = mockState.planProps.at(-1)?.onContinue;
    expect(continuePlan).toBeTypeOf("function");
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /^财务查询/ }));
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.change(screen.getByLabelText("消息输入框"), {
      target: { value: "核验期间的新补充" },
    });
    expect(mockState.planProps.at(-1)?.onContinue).toBe(continuePlan);
    act(() => continuePlan!("继续已核验计划", ["work", "actions"]));
    expect(screen.getByLabelText("消息输入框")).toHaveValue(
      "核验期间的新补充\n\n继续已核验计划",
    );
    expect(document.querySelector('button[data-confirmed="true"]')).toBeNull();
    expect(
      screen.getByRole("checkbox", { name: /^财务查询/ }),
    ).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(mockState.send).not.toHaveBeenCalled();
  });
  it.each([false, true])(
    "prepares exact action receipts without losing drafts or inheriting old grants, and consumes the source once (recheck=%s)",
    async (recheck) => {
      const generationId = "018f0000-0000-7000-8000-000000000761";
      const recheckProposalId = "018f0000-0000-7000-8000-000000000763";
      mockState.recheckProposalId = recheck ? recheckProposalId : undefined;
      mockState.providers = [readyProvider];
      mockState.sessions = [activeSession];
      mockState.messagesPages = [
        {
          data: [
            assistantMessage({
              generation_id: generationId,
              content: "项目建议已确认",
            }),
          ],
          meta: { has_more: false },
        },
      ];
      mockState.send.mockClear();
      useAiChatStore.getState().setInput("我的未发送补充");
      renderPage();
      fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
      fireEvent.click(screen.getByRole("checkbox", { name: /^财务查询/ }));
      fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
      fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
      expect(screen.getByLabelText("消息输入框")).toHaveValue(
        "我的未发送补充\n\n根据真实操作回执继续原需求。",
      );
      expect(
        screen.getByRole("checkbox", { name: /^财务查询/ }),
      ).not.toBeChecked();
      expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
      expect(
        screen.getByText(recheck ? /本次重新核验旧建议/ : /本次将带入所选一轮/),
      ).toBeVisible();
      expect(mockState.send).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
      expect(mockState.send).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
      await waitFor(() =>
        expect(mockState.send).toHaveBeenCalledWith(
          expect.objectContaining({
            sessionId: activeSession.id,
            actionReceiptGenerationId: generationId,
            ...(recheck ? { actionRecheckProposalId: recheckProposalId } : {}),
            context: undefined,
            workspace: {
              provider_version: readyProvider.version,
              scopes: ["actions", "work"],
            },
          }),
        ),
      );
      await waitFor(() =>
        expect(
          screen.queryByRole("button", {
            name: recheck ? "移除重新核验上下文" : "移除操作回执",
          }),
        ).toBeNull(),
      );
      fireEvent.change(screen.getByLabelText("消息输入框"), {
        target: { value: "下一条" },
      });
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
      await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(2));
      expect(
        (mockState.send.mock.calls as unknown as Array<[unknown]>)[1][0],
      ).not.toHaveProperty("actionReceiptGenerationId");
      expect(
        (mockState.send.mock.calls as unknown as Array<[unknown]>)[1][0],
      ).not.toHaveProperty("actionRecheckProposalId");
    },
  );
  it("opens an old approval directly from its queue location without paging or sending", async () => {
    const sessionId = "018f0000-0000-7000-8000-000000000751";
    const generationId = "018f0000-0000-7000-8000-000000000752";
    const proposalId = "018f0000-0000-7000-8000-000000000753";
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, id: sessionId }];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({ session_id: sessionId, content: "最新回复" }),
        ],
        meta: { has_more: true },
      },
    ];
    mockState.hasNextPage = true;
    mockState.fetchNextPage.mockClear();
    mockState.send.mockClear();
    useAiChatStore.setState({
      lastSessionId: "018f0000-0000-7000-8000-000000000759",
    });
    vi.mocked(getAiGeneration).mockResolvedValue({
      id: generationId,
      session_id: sessionId,
      persist: true,
    } as never);
    renderPage(
      `/ai?session=${sessionId}&generation=${generationId}&proposal=${proposalId}`,
    );
    expect(
      await screen.findByRole("dialog", { name: "续办操作建议" }),
    ).toBeVisible();
    expect(await screen.findByText(/已定位到原操作建议/)).toBeVisible();
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(mockState.fetchNextPage).not.toHaveBeenCalled();
    expect(mockState.send).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("dialog", { name: "续办操作建议" })).toBeNull();
  });

  it("expands and scrolls to the plan requested by the queue", async () => {
    const sessionId = "018f0000-0000-7000-8000-000000000751";
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, id: sessionId }];
    mockState.messagesPages = [];
    const scrollTo = vi.fn();
    const originalScroll = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "scrollTo",
    );
    Object.defineProperty(HTMLElement.prototype, "scrollTo", {
      configurable: true,
      value: scrollTo,
    });
    vi.mocked(getAiWorkPlan).mockResolvedValue({
      session_id: sessionId,
      generation_id: sessionId,
      generation_status: "completed",
      version: 1,
      title: "旧会话待继续计划",
      created_at: "2026-09-20T00:00:00Z",
      as_of: "2026-09-20T00:00:00Z",
      steps: [],
    });
    try {
      renderPage(`/ai?session=${sessionId}&plan=1`);
      expect(
        (await screen.findByText(/执行计划 · 旧会话待继续计划/)).closest(
          "details",
        ),
      ).toHaveAttribute("open");
      expect(scrollTo).toHaveBeenCalledWith({ top: 0 });
      const messagePane = screen
        .getByText(/执行计划 · 旧会话待继续计划/)
        .closest(".ai-chat-messages")!;
      Object.defineProperty(messagePane, "scrollHeight", {
        configurable: true,
        value: 720,
      });
      scrollTo.mockClear();
      act(() => {
        mockState.isStreaming = true;
        mockState.streaming = { sessionId, text: "继续处理计划中的下一步" };
        useAiChatStore.getState().setInput("触发页面更新");
      });
      expect(scrollTo).toHaveBeenCalledWith({ top: 720 });
      expect(scrollTo).not.toHaveBeenCalledWith({ top: 0 });
    } finally {
      if (originalScroll)
        Object.defineProperty(
          HTMLElement.prototype,
          "scrollTo",
          originalScroll,
        );
      else Reflect.deleteProperty(HTMLElement.prototype, "scrollTo");
    }
  });
  it("prepares a queue issue with its own scope and never sends automatically", async () => {
    const sourceId = "22222222-2222-4222-8222-222222222223";
    const jobId = "22222222-2222-4222-8222-222222222224";
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("");
    useAiWorkbenchHandoff.getState().stageIssue({
      label: "知识库索引失败",
      route: `/knowledge?source=${sourceId}&job=${jobId}`,
      prompt: "请读取该来源与索引任务的真实状态，并给出下一步建议。",
      scopes: ["knowledge_actions"],
    });
    renderPage();
    expect(
      screen.getByRole("region", { name: "来自工作台的交接" }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "打开原生处理入口" }),
    ).toHaveAttribute("href", `/knowledge?source=${sourceId}&job=${jobId}`);
    expect(mockState.send).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));
    expect(screen.getByRole("checkbox", { name: /^知识库管理/ })).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^知识检索与引用/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^工作事项/ }),
    ).not.toBeChecked();
    expect(useAiChatStore.getState().input).toContain(
      "请读取该来源与索引任务的真实状态",
    );
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    expect(mockState.send).not.toHaveBeenCalled();
  });

  it("opens a new permission review for a model request without sending or inheriting access", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.setState({
      activeSessionId: activeSession.id,
      accessRequests: {
        [activeSession.id]: {
          generationId: "generation-1",
          scopes: ["work", "actions"],
        },
      },
    });
    renderPage();
    expect(
      screen.getByRole("region", { name: "工作台权限请求" }),
    ).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeChecked();
    expect(useAiChatStore.getState().input).toContain(
      "先按本条消息实际授予的范围重新读取",
    );
    const preparedDraft = useAiChatStore.getState().input;
    fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
    expect(useAiChatStore.getState().input).toBe(preparedDraft);
    expect(useAiChatStore.getState().accessRequests[activeSession.id]).toEqual({
      generationId: "generation-1",
      scopes: ["work", "actions"],
    });
    expect(
      screen.getByRole("region", { name: "工作台权限请求" }),
    ).toBeVisible();
    expect(mockState.send).not.toHaveBeenCalled();
  });

  it("prepares a non-workspace queue issue without opening workspace permissions", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("已有草稿");
    useAiWorkbenchHandoff.getState().stageIssue({
      label: "供应商配置待处理",
      route: "/ai?settings=ai",
      routeLabel: "打开 AI 助手设置",
      prompt: "请帮我排查这个供应商配置问题。",
      scopes: [],
    });
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "带入问题" }));

    expect(useAiChatStore.getState().input).toBe(
      "已有草稿\n\n请帮我排查这个供应商配置问题。",
    );
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    expect(mockState.send).not.toHaveBeenCalled();
  });

  it("prepares an exact workbench record without overwriting drafts, sending, or inheriting grants", async () => {
    const id = "22222222-2222-4222-8222-222222222222";
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("我原来的未发送草稿");
    useAiWorkbenchHandoff.getState().stage("client", id);
    renderPage();
    expect(
      screen.getByRole("region", { name: "来自工作台的事项" }),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "返回客户" })).toHaveAttribute(
      "href",
      `/clients/${id}`,
    );
    expect(screen.getByLabelText("消息输入框")).toHaveValue(
      "我原来的未发送草稿",
    );
    expect(mockState.send).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /^财务查询/ }));
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^客户资料/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^财务查询/ }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /^启动 \/ 重试/ }),
    ).not.toBeChecked();
    expect(mockState.send).not.toHaveBeenCalled();
    expect(useAiChatStore.getState().input).toMatch(/^我原来的未发送草稿\n\n/);
    expect(useAiChatStore.getState().input).toContain(`type=client、id=${id}`);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(mockState.send).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(mockState.send).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: activeSession.id,
          message: expect.stringContaining(`/clients/${id}`),
          workspace: {
            provider_version: readyProvider.version,
            scopes: ["actions", "work", "clients"],
          },
          context: undefined,
        }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "工作台权限" }),
      ).toBeInTheDocument(),
    );
  });

  it("keeps a handoff pending during generation and lets the user dismiss it without changing the draft", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = true;
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("保留草稿");
    useAiWorkbenchHandoff
      .getState()
      .stage("task", "22222222-2222-4222-8222-222222222222");
    renderPage();
    expect(
      screen.getByRole("button", { name: "带入问题并选择权限" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "暂不带入工作台事项" }));
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(useAiChatStore.getState().input).toBe("保留草稿");
    expect(mockState.send).not.toHaveBeenCalled();
  });

  it("retains the pending workbench record until a provider is ready", () => {
    mockState.providers = [];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    useAiChatStore.getState().setInput("保留草稿");
    const id = "22222222-2222-4222-8222-222222222222";
    useAiWorkbenchHandoff.getState().stage("task", id);
    renderPage();
    const prepare = screen.getByRole("button", { name: "带入问题并选择权限" });
    expect(prepare).toBeDisabled();
    fireEvent.click(prepare);
    expect(useAiWorkbenchHandoff.getState().pending?.id).toBe(id);
    expect(useAiChatStore.getState().input).toBe("保留草稿");
    expect(screen.queryByRole("dialog", { name: "工作台权限" })).toBeNull();
    expect(mockState.send).not.toHaveBeenCalled();
  });

  it("filters action permissions from workbench handoffs into an ephemeral conversation", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, persist: false }];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    const id = "22222222-2222-4222-8222-222222222222";
    useAiWorkbenchHandoff.getState().stage("task", id);
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    const actions = screen.getByRole("checkbox", { name: /^操作建议/ });
    expect(actions).not.toBeChecked();
    expect(actions).toBeDisabled();
    expect(mockState.send).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(mockState.send).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringContaining(`/tasks/${id}`),
          workspace: {
            provider_version: readyProvider.version,
            scopes: ["work"],
          },
        }),
      ),
    );
  });

  it("does not offer persistent action proposals in an ephemeral conversation", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, persist: false }];
    mockState.messagesPages = [];
    mockState.isStreaming = false;
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(screen.getByRole("checkbox", { name: /^操作建议/ })).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: /^工作事项/ }),
    ).not.toBeDisabled();
    expect(
      screen.getByText(/当前会话不保存记录，操作建议与多步计划暂不可用/),
    ).toBeInTheDocument();
  });
  it("grants workspace access explicitly for one message and clears it after acceptance", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = false;
    mockState.send.mockClear();
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "工作台权限" }));
    expect(
      screen.getByText(/查询结果会发送给以上远程供应商/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "仅授权下一条消息" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox", { name: /^执行产出/ }));
    expect(screen.getByRole("checkbox", { name: /^工作事项/ })).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "仅授权下一条消息" }));
    expect(screen.getByLabelText("本条工作台授权范围")).toHaveTextContent(
      "下一条消息已选 2 项：工作事项、执行产出",
    );
    fireEvent.change(screen.getByLabelText("消息输入框"), {
      target: { value: "查任务产出" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(mockState.send).toHaveBeenCalledWith(
        expect.objectContaining({
          workspace: {
            provider_version: readyProvider.version,
            scopes: ["work", "outputs"],
          },
        }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "工作台权限" }),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByLabelText("本条工作台授权范围")).toBeNull();
    fireEvent.change(screen.getByLabelText("消息输入框"), {
      target: { value: "下一条" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(mockState.send).toHaveBeenLastCalledWith(
        expect.objectContaining({ workspace: undefined }),
      ),
    );
  });

  it("renders only allowlisted workspace detail links from assistant markdown", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.isStreaming = false;
    const id = "018f0000-0000-7000-8000-000000005833";
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            content: `[真实任务](/tasks/${id}) [外部](https://example.com) [危险](javascript:alert)`,
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();
    expect(screen.getByRole("link", { name: "真实任务" })).toHaveAttribute(
      "href",
      `/tasks/${id}`,
    );
    expect(
      screen.queryByRole("link", { name: "外部" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "危险" }),
    ).not.toBeInTheDocument();
  });

  it("attaches the originating message session to report links, not a model-selected return target", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.isStreaming = false;
    const session = "018f0000-0000-7000-8000-000000005834";
    const route =
      "/focus?report=days&date_from=2026-11-01&date_to=2026-11-02&timezone=America%2FLos_Angeles";
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            session_id: session,
            content: `[查看报告](${route}) [伪造返回](${route}&return_session=${session})`,
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();
    expect(screen.getByRole("link", { name: "查看报告" })).toHaveAttribute(
      "href",
      `${route}&return_session=${session}`,
    );
    expect(
      screen.queryByRole("link", { name: "伪造返回" }),
    ).not.toBeInTheDocument();
  });

  it("does not replace a workspace return selection with a different running session", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = true;
    mockState.streaming = {
      sessionId: "another-running-session",
      text: "还在生成",
    };
    useAiChatStore.setState({ activeSessionId: activeSession.id });
    renderPage();
    expect(useAiChatStore.getState().activeSessionId).toBe(activeSession.id);
    expect(screen.queryByText("还在生成")).not.toBeInTheDocument();
    mockState.streaming = null;
    mockState.isStreaming = false;
  });

  it("renders non-persistent completed turns as read-only natural language with no confirmation commands", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [{ ...activeSession, persist: false }];
    mockState.messagesPages = [];
    mockState.isStreaming = false;
    useAiChatStore.setState({
      retainedTurns: [
        {
          sessionId: activeSession.id,
          generationId: "ephemeral-generation",
          userText: "创建一个任务并记住我的偏好",
          text: '已创建任务并记住了。[opc:task]{"title":"临时建议"}[/opc:task][opc:memory]{"content":"简洁回答"}[/opc:memory]',
          reasoning: "临时思考",
          status: "completed",
          createdAt: activeSession.created_at,
        },
      ],
    });
    renderPage();
    expect(screen.getByText("创建一个任务并记住我的偏好")).toBeTruthy();
    expect(
      screen.getByText(/已整理任务建议「临时建议」，尚未创建/),
    ).toBeTruthy();
    expect(screen.getByText(/仅在当前窗口内存中保留/)).toBeTruthy();
    expect(screen.queryByText(/已创建任务并记住了/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: /点击确认创建|记住这个/ }),
    ).toBeNull();
  });

  it.each(["completed", "incomplete"] as const)(
    "shows temporary citations without upgrading a %s reply or enabling writes",
    (status) => {
      mockState.providers = [readyProvider];
      mockState.sessions = [{ ...activeSession, persist: false }];
      mockState.messagesPages = [];
      mockState.isStreaming = false;
      useAiChatStore.setState({
        retainedTurns: [
          {
            sessionId: activeSession.id,
            generationId: "temporary-citation",
            userText: "核对资料",
            text: "临时回答",
            reasoning: "",
            status,
            createdAt: activeSession.created_at,
            citationEvidence: {
              status: "validated",
              items: [
                {
                  chunk_id: "018f0000-0000-7000-8000-000000000001",
                  source_id: "018f0000-0000-7000-8000-000000000002",
                  document_id: "018f0000-0000-7000-8000-000000000003",
                  source_type: "pdf",
                  source_name: "资料.pdf",
                  document_title: "文档",
                  source_version: 2,
                  document_version: 3,
                  chunk_index: 0,
                  start_char: 0,
                  end_char: 30,
                  start_line: 2,
                  end_line: 4,
                  start_page: 3,
                  end_page: 3,
                },
              ],
            },
          },
        ],
      });
      renderPage();
      expect(
        screen.getByRole("region", { name: "已验证知识来源" }),
      ).toHaveTextContent("资料.pdf");
      expect(screen.getByText(/第 3–3 页/)).toBeInTheDocument();
      expect(
        screen.getByRole("link", { name: "核验原片段" }).getAttribute("href"),
      ).toContain("document_version=3");
      if (status === "incomplete") {
        expect(screen.getByText(/当前仅有片段/)).toBeInTheDocument();
        expect(
          document.querySelector('.ai-msg-text[data-status="incomplete"]'),
        ).not.toBeNull();
      }
      expect(
        screen.queryByRole("button", { name: /点击确认创建|记住这个/ }),
      ).not.toBeInTheDocument();
    },
  );

  it("deduplicates a retained turn after its durable message arrives", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.isStreaming = false;
    const retained = {
      sessionId: activeSession.id,
      generationId: "durable-generation",
      userText: "问题",
      text: "唯一回复",
      reasoning: "",
      status: "completed" as const,
      createdAt: activeSession.created_at,
    };
    useAiChatStore.setState({ retainedTurns: [retained] });
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            content: retained.text,
            generation_id: retained.generationId,
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();
    expect(screen.getAllByText("唯一回复")).toHaveLength(1);
  });

  it("keeps a draft cancelled before acceptance", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = false;
    mockState.send.mockResolvedValueOnce({
      sessionId: "",
      cancelled: true,
      accepted: false,
      error: null,
      errorCode: null,
    });
    renderPage();
    const input = screen.getByPlaceholderText(/向 AI 助手提问/);
    fireEvent.change(input, { target: { value: "未接收的草稿" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
    });
    expect(input).toHaveValue("未接收的草稿");
  });

  it("does not send Enter while confirming an IME candidate", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = false;
    mockState.send.mockClear();
    renderPage();
    const input = screen.getByPlaceholderText(/向 AI 助手提问/);
    fireEvent.change(input, { target: { value: "中文输入" } });
    fireEvent.keyDown(input, { key: "Enter", isComposing: true, keyCode: 229 });
    expect(mockState.send).not.toHaveBeenCalled();
    fireEvent.keyDown(input, { key: "Enter", isComposing: false });
    await waitFor(() => expect(mockState.send).toHaveBeenCalledTimes(1));
  });

  it.each(["后来的草稿", "第一条"])(
    "does not overwrite newer typing %s when a previous accepted send settles",
    async (laterDraft) => {
      mockState.providers = [readyProvider];
      mockState.sessions = [activeSession];
      mockState.messagesPages = [];
      mockState.isStreaming = false;
      let resolve!: (value: {
        sessionId: string;
        cancelled: boolean;
        accepted: boolean;
        error: null;
        errorCode: null;
      }) => void;
      mockState.send.mockImplementationOnce(
        () =>
          new Promise((done) => {
            resolve = done;
          }),
      );
      renderPage();
      const input = screen.getByPlaceholderText(/向 AI 助手提问/);
      fireEvent.change(input, { target: { value: "第一条" } });
      fireEvent.click(screen.getByRole("button", { name: "发送" }));
      fireEvent.change(input, { target: { value: "" } });
      fireEvent.change(input, { target: { value: laterDraft } });
      resolve({
        sessionId: "session-1",
        cancelled: false,
        accepted: true,
        error: null,
        errorCode: null,
      });
      await waitFor(() => expect(input).toHaveValue(laterDraft));
    },
  );
  it("guides to settings when no provider is registered", () => {
    mockState.providers = [];
    renderPage();
    expect(screen.getByText(/尚未配置 AI 供应商/)).toBeTruthy();
  });

  it("opens AI settings from the provider issue deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?settings=ai");
    await waitFor(() =>
      expect(mockState.setSettingsOpen).toHaveBeenCalledWith(true, "ai"),
    );
  });

  it("opens data settings from the restore diagnostics deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?settings=data");
    await waitFor(() =>
      expect(mockState.setSettingsOpen).toHaveBeenCalledWith(true, "data"),
    );
  });

  it("opens actor settings from the local person handoff deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?settings=actors");
    await waitFor(() =>
      expect(mockState.setSettingsOpen).toHaveBeenCalledWith(true, "actors"),
    );
  });

  it("opens local Agent settings from the adapter handoff deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?settings=agent");
    await waitFor(() =>
      expect(mockState.setSettingsOpen).toHaveBeenCalledWith(true, "agent"),
    );
  });

  it("opens runtime diagnostics from the diagnostics handoff deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?settings=diagnostics");
    await waitFor(() =>
      expect(mockState.setSettingsOpen).toHaveBeenCalledWith(
        true,
        "diagnostics",
      ),
    );
  });

  it("opens a specific session from the failed generation deep link", async () => {
    const failedSession = "018f0000-0000-7000-8000-000000002221";
    mockState.providers = [readyProvider];
    mockState.sessions = [
      activeSession,
      {
        ...activeSession,
        id: failedSession,
        title: "失败会话",
      },
    ];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage(`/ai?session=${failedSession}`);
    await waitFor(() =>
      expect(useAiChatStore.getState().activeSessionId).toBe(failedSession),
    );
    expect(mockState.setSettingsOpen).not.toHaveBeenCalled();
  });

  it("ignores a non-canonical session deep link", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.setSettingsOpen.mockClear();
    renderPage("/ai?session=not-a-session");
    await waitFor(() =>
      expect(useAiChatStore.getState().activeSessionId).toBe(activeSession.id),
    );
    expect(mockState.setSettingsOpen).not.toHaveBeenCalled();
  });

  it("keeps session switching available in the compact AI header", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [
      activeSession,
      {
        ...activeSession,
        id: "session-2",
        title: "另一会话",
        updated_at: "2026-09-01T13:00:00Z",
      },
    ];
    renderPage();

    const selector = screen.getByLabelText("选择会话");
    expect(selector).toHaveValue("session-1");
    fireEvent.change(selector, { target: { value: "session-2" } });
    expect(selector).toHaveValue("session-2");
    expect(screen.getAllByText("另一会话").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "新建会话" })).toBeTruthy();
  });

  it("shows the suggestion chip for a task block and strips the raw block from display", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    renderPage();
    expect(taskChip("建议任务：写周报")).toBeTruthy();
    expect(screen.getByText(/已整理任务建议「写周报」，尚未创建/)).toBeTruthy();
    expect(screen.queryByText(/\[opc:task\]/)).toBeNull();
  });

  it("recovers a repeated task marker and supplies a natural-language reply", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            content: '[opc:task]{"title":"写作业"}[opc:task]',
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();

    expect(
      screen.getByText(
        "已整理任务建议「写作业」，尚未创建。请确认下面的信息后创建。",
      ),
    ).toBeTruthy();
    expect(taskChip("建议任务：写作业")).toBeTruthy();
    expect(screen.queryByText(/\[opc:task\]/)).toBeNull();
  });

  it("reports an invalid task block naturally without offering a broken card", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            content: "[opc:task]{not-json}[/opc:task]",
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();

    expect(
      screen.getByText("这条建议格式不完整，未执行任何操作，请重新生成。"),
    ).toBeTruthy();
    expect(screen.queryByText(/建议任务：/)).toBeNull();
    expect(screen.queryByText(/\[opc:task\]/)).toBeNull();
  });

  it("confirms an edited suggestion using the atomic message task command", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.confirmTask.mutateAsync = vi.fn(async () => ({
      id: "task-1",
      title: "写周报",
    }));
    renderPage();

    fireEvent.click(taskChip("建议任务：写周报"));
    const titleInput = screen.getByDisplayValue("写周报") as HTMLInputElement;
    expect(titleInput).toBeTruthy();
    fireEvent.click(screen.getByText("确认创建"));

    await waitFor(() => {
      expect(mockState.confirmTask.mutateAsync).toHaveBeenCalledWith({
        messageId: "message-1",
        input: expect.objectContaining({
          title: "写周报",
          priority: "P2",
          dueDate: "2026-09-02",
        }),
      });
    });
  });

  it("retries the same durable confirmation identity after closing and reopening a failed card", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.confirmTask.mutateAsync = vi
      .fn()
      .mockRejectedValueOnce(new Error("temporary"))
      .mockResolvedValueOnce({});
    renderPage();

    fireEvent.click(taskChip("建议任务：写周报"));
    fireEvent.click(screen.getByText("确认创建"));
    expect(await screen.findByText(/任务确认未完成/)).toBeTruthy();
    fireEvent.click(screen.getByText("取消"));
    fireEvent.click(taskChip("建议任务：写周报"));
    fireEvent.click(screen.getByText("确认创建"));

    await waitFor(() => {
      expect(mockState.confirmTask.mutateAsync).toHaveBeenCalledTimes(2);
      expect(mockState.confirmTask.mutateAsync).toHaveBeenLastCalledWith({
        messageId: "message-1",
        input: expect.objectContaining({ title: "写周报" }),
      });
    });
  });

  it("renders an attached task card and navigates to the task detail on click", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            content: "已为你创建任务。",
            task_id: "task-1",
            task_title_snapshot: "写周报",
          }),
        ],
        meta: { has_more: false },
      },
    ];
    mockState.taskDetail = {
      title: "写周报",
      status: "todo",
      dueDate: "2026-09-02",
    };
    const { unmount } = renderPage();

    const card = screen.getByText("Created").closest("button");
    expect(card).toBeTruthy();
    expect(screen.getByText("写周报")).toBeTruthy();
    expect(screen.getByText("2026-09-02 截止")).toBeTruthy();
    if (card) fireEvent.click(card);
    unmount();
    expect(mockState.navigate).toHaveBeenCalledWith("/tasks/task-1");
  });

  it("keeps the chat-header workspace control only for reopening a collapsed panel", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    const expanded = renderPage();

    expect(
      screen.queryByRole("button", { name: "关闭右侧工作栏" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "打开右侧工作栏" }),
    ).not.toBeInTheDocument();
    expanded.unmount();

    mockState.rightOverviewCollapsed = true;
    const collapsed = renderPage();
    fireEvent.click(screen.getByRole("button", { name: "打开右侧工作栏" }));
    expect(mockState.toggleRightOverviewCollapsed).toHaveBeenCalledOnce();
    collapsed.unmount();
    mockState.rightOverviewCollapsed = false;
  });

  it("offers a restore control once the conversation rail is hidden", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.agentRailCollapsed = true;
    renderPage();

    expect(screen.queryByRole("button", { name: "隐藏会话侧边栏" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "显示会话侧边栏" }));
    expect(mockState.toggleAgentRailCollapsed).toHaveBeenCalledOnce();
    mockState.agentRailCollapsed = false;
  });

  it("shows the stop button while streaming and the send button otherwise", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.isStreaming = true;
    mockState.streaming = { sessionId: "session-1", text: "正在生成…" };
    const { unmount } = renderPage();
    expect(screen.getByRole("button", { name: "停止生成" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "新建会话" })).toBeDisabled();
    expect(screen.getByLabelText("选择会话")).toBeDisabled();
    expect(screen.getByLabelText("选择 AI 供应商")).toBeDisabled();
    unmount();

    mockState.isStreaming = false;
    mockState.streaming = null;
    const second = renderPage();
    expect(screen.getByRole("button", { name: "发送" })).toBeTruthy();
    second.unmount();
  });

  it("shows the sent user message optimistically while streaming", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.isStreaming = true;
    mockState.streaming = { sessionId: "session-1", text: "" };
    mockState.sentMessage = "帮我总结今天";
    const { unmount } = renderPage();

    const userBubbles = screen
      .getAllByText("帮我总结今天")
      .filter((node) => node.closest(".ai-bubble-user"));
    expect(userBubbles.length).toBe(1);
    unmount();

    mockState.isStreaming = false;
    mockState.streaming = null;
    mockState.sentMessage = null;
  });

  it("shows the current workspace tool instead of a generic thinking placeholder", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.isStreaming = true;
    mockState.streaming = {
      sessionId: "session-1",
      text: "",
      progress: [
        {
          sequence: 2,
          kind: "tool_call",
          tool_name: "workspace_search",
          status: "running",
          started_at: new Date().toISOString(),
          duration_ms: 0,
        },
      ],
    };
    const view = renderPage();
    expect(screen.getByText("搜索工作台 · 进行中")).toBeInTheDocument();
    expect(screen.queryByText("正在思考…")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "停止生成" }),
    ).toBeInTheDocument();
    view.unmount();
    mockState.isStreaming = false;
    mockState.streaming = null;
  });

  it("offers a provider switcher when several providers are ready and sends with the selected one", async () => {
    mockState.providers = [
      readyProvider,
      {
        ...readyProvider,
        id: "provider-2",
        name: "Kimi",
        model: "kimi-k2",
        base_url: "https://api.moonshot.cn/v1",
      },
    ];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.send = vi.fn(
      async (): Promise<{
        sessionId: string;
        cancelled: boolean;
        accepted: boolean;
        error: string | null;
        errorCode: string | null;
      }> => ({
        sessionId: "session-1",
        cancelled: false,
        accepted: true,
        error: null,
        errorCode: null,
      }),
    );
    renderPage();

    const select = screen.getByLabelText("选择 AI 供应商") as HTMLSelectElement;
    expect(select.options.length).toBe(2);
    fireEvent.change(select, { target: { value: "provider-2" } });

    fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
      target: { value: "你好" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() => {
      expect(mockState.send).toHaveBeenCalledWith(
        expect.objectContaining({ providerId: "provider-2" }),
      );
    });
  });

  it("allows a ready local provider without an API key", () => {
    mockState.providers = [
      {
        ...readyProvider,
        id: "provider-local",
        name: "Ollama",
        kind: "local",
        has_key: false,
      },
    ];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    renderPage();

    expect(screen.getByLabelText("选择 AI 供应商")).toHaveTextContent(
      "Ollama · deepseek-chat · 本地",
    );
    expect(screen.getByPlaceholderText(/向 AI 助手提问/)).not.toBeDisabled();
    expect(
      screen.queryByText(/AI 生成内容仅供参考/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/模型请求仅发送到本机回环端点/),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText(/当前对话上下文会发送给所选远程供应商/),
    ).not.toBeInTheDocument();
  });

  it("shows older message pages before the newest page and loads more on demand", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({ id: "message-3", content: "消息 3" }),
          assistantMessage({ id: "message-4", content: "消息 4" }),
        ],
        meta: { has_more: true },
      },
      {
        data: [
          assistantMessage({ id: "message-1", content: "消息 1" }),
          assistantMessage({ id: "message-2", content: "消息 2" }),
        ],
        meta: { has_more: false },
      },
    ];
    mockState.hasNextPage = true;
    mockState.fetchNextPage = vi.fn(async () => ({}));
    renderPage();

    expect(
      screen.getAllByText(/^消息 \d$/).map((element) => element.textContent),
    ).toEqual(["消息 1", "消息 2", "消息 3", "消息 4"]);
    fireEvent.click(screen.getByRole("button", { name: "加载更早消息" }));
    expect(mockState.fetchNextPage).toHaveBeenCalledTimes(1);
    mockState.hasNextPage = false;
  });

  it("shows how many early messages are represented by the active snapshot", () => {
    mockState.sessions = [{ ...activeSession, compacted_message_count: 42 }];
    renderPage();
    expect(screen.getByText("已压缩前 42 条消息")).toBeTruthy();
  });

  it("shows local session usage with exact Provider coverage and unknown counts", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "本地用量" }));

    await waitFor(() => {
      expect(mockState.getAiUsageSummary).toHaveBeenCalledWith({
        sessionId: "session-1",
        trendDays: 7,
      });
    });
    await screen.findByText("450 in / 90 out");
    const panel = screen.getByRole("region", { name: "当前会话本地用量" });
    expect(within(panel).getByText("3", { selector: "dd" })).toBeTruthy();
    expect(within(panel).getByText("2/3", { selector: "dd" })).toBeTruthy();
    expect(within(panel).getByText("450 in / 90 out")).toBeTruthy();
    expect(within(panel).getByText("3.5 s")).toBeTruthy();
    expect(within(panel).getByText(/1 次未返回 usage/)).toBeTruthy();
    expect(within(panel).getByText(/不计算费用/)).toBeTruthy();
    expect(within(panel).getByText("用量趋势")).toBeTruthy();
    expect(within(panel).getByText("09/08")).toBeTruthy();
    fireEvent.click(within(panel).getByRole("button", { name: "30 天" }));
    await waitFor(() => {
      expect(mockState.getAiUsageSummary).toHaveBeenLastCalledWith({
        sessionId: "session-1",
        trendDays: 30,
      });
    });
  });

  it("requires an exact preview before sending selected workspace context", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    mockState.previewContext.mutateAsync.mockClear();
    mockState.previewData = {
      provider_id: "provider-1",
      provider_name: "DeepSeek",
      provider_kind: "remote",
      provider_version: 2,
      leaves_device: true,
      serialized_bytes: 640,
      sources: [
        {
          type: "task",
          id: "task-context",
          version: 4,
          label: "Prepare release",
          fields: {
            title: "Prepare release",
            status: "todo",
            review_policy: "manual",
            review_policy_change_allowed: true,
          },
          truncated_fields: [],
        },
        {
          type: "project",
          id: "project-context",
          version: 5,
          label: "Launch",
          fields: { name: "Launch", task_total: 3 },
          truncated_fields: [],
        },
        {
          type: "client",
          id: "client-context",
          version: 2,
          label: "Acme",
          fields: { name: "Acme", notes: "Local only" },
          truncated_fields: [],
        },
      ],
    };
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "上下文" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 AI 上下文任务" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 AI 上下文项目" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 AI 上下文客户" }));
    fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
      target: { value: "总结这些信息" },
    });
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "预览发送内容" }));
    await waitFor(() => {
      expect(mockState.previewContext.mutateAsync).toHaveBeenCalledWith({
        provider_id: "provider-1",
        sources: [
          { type: "task", id: "task-context" },
          { type: "project", id: "project-context" },
          { type: "client", id: "client-context" },
        ],
        knowledge: [],
      });
    });
    expect(screen.getByText("将发送给远程供应商 DeepSeek")).toBeTruthy();
    expect(screen.getByText(/序列化约 640 字节/)).toBeTruthy();
    expect(screen.getByText("验收方式")).toBeTruthy();
    expect(screen.getByText("人工验收")).toBeTruthy();
    expect(screen.getByText("当前允许切换验收方式")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "确认用于下一条消息" }));
    expect(screen.getByRole("button", { name: "发送" })).not.toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "发送" }));

    await waitFor(() => {
      expect(mockState.send).toHaveBeenCalledWith({
        providerId: "provider-1",
        sessionId: "session-1",
        message: "总结这些信息",
        context: {
          provider_version: 2,
          sources: [
            { type: "task", id: "task-context", expected_version: 4 },
            {
              type: "project",
              id: "project-context",
              expected_version: 5,
            },
            { type: "client", id: "client-context", expected_version: 2 },
          ],
          knowledge: [],
        },
      });
    });
  });

  it("shows the exact context sources stored with a historical user message", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          {
            id: "user-context-message",
            session_id: "session-1",
            role: "user",
            status: "completed",
            content: "总结任务",
            reasoning: null,
            task_id: null,
            task_title_snapshot: null,
            context_provider: {
              id: "provider-1",
              name: "DeepSeek",
              kind: "remote",
              version: 2,
            },
            context_sources: [
              {
                type: "task",
                id: "task-context",
                version: 4,
                label: "Prepare release",
                fields: { title: "Prepare release" },
                truncated_fields: [],
              },
            ],
            context_knowledge: [
              {
                source_id: "source-1",
                source_name: "guide.md",
                source_version: 2,
                source_type: "markdown",
                document_id: "document-1",
                document_title: "Guide",
                document_version: 1,
                chunk_id: "chunk-1",
                chunk_index: 0,
                start_char: 0,
                end_char: 10,
                start_line: 3,
                end_line: 4,
                content: "local evidence",
              },
            ],
            created_at: "2026-09-08T12:00:00Z",
          },
          assistantMessage({
            id: "assistant-citation-message",
            session_id: "018f0000-0000-7000-8000-000000006404",
            content: "根据资料，付款后归档。",
            citation_status: "validated",
            generation_id: "018f0000-0000-7000-8000-000000006401",
            citations: [
              {
                chunk_id: "018f0000-0000-7000-8000-000000006403",
                source_id: "018f0000-0000-7000-8000-000000006401",
                source_name: "cited-guide.md",
                source_type: "markdown",
                source_version: 2,
                document_id: "018f0000-0000-7000-8000-000000006402",
                document_title: "Cited guide",
                document_version: 1,
                chunk_index: 0,
                start_char: 0,
                end_char: 10,
                start_line: 6,
                end_line: 7,
              },
            ],
            created_at: "2026-09-08T12:01:00Z",
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();
    expect(screen.getByText("Task · Prepare release")).toBeTruthy();
    expect(screen.getByText("guide.md · L3–4")).toBeTruthy();
    expect(screen.getByText("DeepSeek · 远程")).toBeTruthy();
    expect(screen.getByRole("region", { name: "已验证知识来源" })).toBeTruthy();
    expect(screen.getByText("cited-guide.md")).toBeTruthy();
    expect(screen.getByText(/第 6–7 行 · 文档 v1/)).toBeTruthy();
    const citationLink = screen.getByRole("link", { name: "核验原片段" });
    const citationURL = new URL(
      citationLink.getAttribute("href")!,
      "http://localhost",
    );
    expect(citationURL.pathname).toBe("/knowledge");
    expect(Object.fromEntries(citationURL.searchParams)).toEqual({
      source_id: "018f0000-0000-7000-8000-000000006401",
      document_id: "018f0000-0000-7000-8000-000000006402",
      chunk_id: "018f0000-0000-7000-8000-000000006403",
      source_version: "2",
      document_version: "1",
      return_session: "018f0000-0000-7000-8000-000000006404",
    });
    fireEvent.click(screen.getByRole("button", { name: "运行详情" }));
    await waitFor(() =>
      expect(mockState.getAiRunSteps).toHaveBeenCalledWith(
        "018f0000-0000-7000-8000-000000006401",
      ),
    );
    expect(await screen.findByText("输入 2.0 KiB")).toBeTruthy();
    expect(screen.getByText("Provider token：100 in / 20 out")).toBeTruthy();
    expect(screen.getByText("仅字节与耗时，不保存正文")).toBeTruthy();
    expect(screen.getByText("工具调用 · 搜索工作台")).toBeTruthy();
    expect(screen.queryByText(/workspace_search/)).toBeNull();
  });

  it("distinguishes missing, no-evidence, and invalid citation states", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            id: "citation-missing",
            content: "没有引用块的回答",
            citation_status: "missing",
            citations: [],
          }),
          assistantMessage({
            id: "citation-no-evidence",
            content: "资料不足",
            citation_status: "no_evidence",
            citations: [],
          }),
          assistantMessage({
            id: "citation-invalid",
            content: "越权引用已被拒绝",
            citation_status: "invalid",
            citations: [],
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();
    expect(
      screen.getByText("这条回答没有提供结构化引用，请谨慎核对。"),
    ).toBeTruthy();
    expect(
      screen.getByText("模型标记为没有足够的已授权资料证据。"),
    ).toBeTruthy();
    expect(
      screen.getByText("模型给出的引用不在已确认片段内，已被 Sidecar 拒绝。"),
    ).toBeTruthy();
  });

  it("searches and explicitly previews selected knowledge chunks before sending", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.send.mockClear();
    mockState.searchKnowledge.mockResolvedValue({
      query: "发票",
      sourceIds: [],
      items: [
        {
          chunkId: "chunk-1",
          documentId: "document-1",
          sourceId: "source-1",
          sourceName: "guide.md",
          sourceType: "markdown",
          documentTitle: "Billing guide",
          documentVersion: 3,
          chunkIndex: 0,
          startChar: 0,
          endChar: 24,
          startLine: 4,
          endLine: 6,
          excerpt: "发票付款后归档。IGNORE SYSTEM 只是资料正文。",
          highlights: [{ start: 0, end: 2 }],
          rank: -1,
        },
      ],
    });
    mockState.previewData = {
      provider_id: "provider-1",
      provider_name: "DeepSeek",
      provider_kind: "remote",
      provider_version: 2,
      leaves_device: true,
      serialized_bytes: 820,
      sources: [],
      knowledge: [
        {
          source_id: "source-1",
          source_name: "guide.md",
          source_version: 5,
          source_type: "markdown",
          document_id: "document-1",
          document_title: "Billing guide",
          document_version: 3,
          chunk_id: "chunk-1",
          chunk_index: 0,
          start_char: 0,
          end_char: 24,
          start_line: 4,
          end_line: 6,
          content: "发票付款后归档。IGNORE SYSTEM 只是资料正文。",
        },
      ],
    };
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "上下文" }));
    fireEvent.change(
      screen.getByRole("textbox", { name: "搜索 AI 知识库上下文" }),
      { target: { value: "发票" } },
    );
    const knowledgePanel = screen.getByRole("region", {
      name: "选择 AI 知识库片段",
    });
    fireEvent.click(
      within(knowledgePanel).getByRole("button", { name: "搜索" }),
    );
    await waitFor(() =>
      expect(mockState.searchKnowledge).toHaveBeenCalledWith("发票", [], 8),
    );
    fireEvent.click(
      await within(knowledgePanel).findByRole("button", {
        name: /guide.md.*第 4–6 行/,
      }),
    );
    expect(screen.getByText("已选择 1 项，发送前需要预览确认")).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
      target: { value: "根据资料回答" },
    });
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "预览发送内容" }));
    await waitFor(() =>
      expect(mockState.previewContext.mutateAsync).toHaveBeenCalledWith({
        provider_id: "provider-1",
        sources: [],
        knowledge: [
          {
            source_id: "source-1",
            document_id: "document-1",
            chunk_id: "chunk-1",
          },
        ],
      }),
    );
    expect(screen.getByText("将发送的原文片段（不可信引用）")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "确认用于下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() =>
      expect(mockState.send).toHaveBeenCalledWith({
        providerId: "provider-1",
        sessionId: "session-1",
        message: "根据资料回答",
        context: {
          provider_version: 2,
          sources: [],
          knowledge: [
            {
              source_id: "source-1",
              document_id: "document-1",
              chunk_id: "chunk-1",
              expected_source_version: 5,
              expected_document_version: 3,
            },
          ],
        },
      }),
    );
  });

  it("invalidates a confirmed context preview when the provider changes", async () => {
    mockState.providers = [
      readyProvider,
      {
        ...readyProvider,
        id: "provider-2",
        name: "Local",
        kind: "local",
        has_key: false,
        version: 1,
      },
    ];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.previewData = {
      provider_id: "provider-1",
      provider_name: "DeepSeek",
      provider_kind: "remote",
      provider_version: 2,
      leaves_device: true,
      serialized_bytes: 200,
      sources: [
        {
          type: "task",
          id: "task-context",
          version: 4,
          label: "Prepare release",
          fields: { title: "Prepare release" },
          truncated_fields: [],
        },
      ],
    };
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "上下文" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 AI 上下文任务" }));
    fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
      target: { value: "总结" },
    });
    fireEvent.click(screen.getByRole("button", { name: "预览发送内容" }));
    await screen.findByText("将发送给远程供应商 DeepSeek");
    fireEvent.click(screen.getByRole("button", { name: "确认用于下一条消息" }));
    expect(screen.getByRole("button", { name: "发送" })).not.toBeDisabled();
    fireEvent.change(screen.getByLabelText("选择 AI 供应商"), {
      target: { value: "provider-2" },
    });
    expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
  });

  it("keeps selected sources but drops confirmation after a context version error", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [];
    mockState.previewData = {
      provider_id: "provider-1",
      provider_name: "DeepSeek",
      provider_kind: "remote",
      provider_version: 2,
      leaves_device: true,
      serialized_bytes: 200,
      sources: [
        {
          type: "task",
          id: "task-context",
          version: 4,
          label: "Prepare release",
          fields: { title: "Prepare release" },
          truncated_fields: [],
        },
      ],
    };
    mockState.send.mockResolvedValueOnce({
      sessionId: "session-1",
      cancelled: false,
      accepted: false,
      error: "context changed",
      errorCode: "AI_CONTEXT_CHANGED",
    });
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "上下文" }));
    fireEvent.click(screen.getByRole("button", { name: "选择 AI 上下文任务" }));
    fireEvent.change(screen.getByPlaceholderText(/向 AI 助手提问/), {
      target: { value: "总结" },
    });
    fireEvent.click(screen.getByRole("button", { name: "预览发送内容" }));
    await screen.findByText("将发送给远程供应商 DeepSeek");
    fireEvent.click(screen.getByRole("button", { name: "确认用于下一条消息" }));
    fireEvent.click(screen.getByRole("button", { name: "发送" }));
    await waitFor(() => {
      expect(screen.getByText("已选择 1 项，发送前需要预览确认")).toBeTruthy();
    });
    expect(screen.getByPlaceholderText(/向 AI 助手提问/)).toHaveValue("总结");
  });
});

describe("AiAssistantPage memory suggestion", () => {
  afterEach(() => {
    cleanup();
  });

  it("gates a memory suggestion behind explicit user confirmation", async () => {
    mockState.messagesPages = [
      {
        data: [
          assistantMessage({
            id: "message-memory",
            content:
              '已了解你的偏好。[opc:memory]{"content":"回答保持简洁","proposal_id":"018f0000-0000-7000-8000-000000005741"}[/opc:memory]',
          }),
        ],
        meta: { has_more: false },
      },
    ];
    renderPage();

    expect(screen.getByText(/记住偏好：回答保持简洁/)).toBeTruthy();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "记住" })).not.toBeDisabled(),
    );
    fireEvent.click(screen.getByRole("button", { name: "记住" }));

    await waitFor(() => {
      expect(mockState.createMemory.mutateAsync).toHaveBeenCalledWith({
        content: "回答保持简洁",
        source_message_id: "message-memory",
        proposal_id: "018f0000-0000-7000-8000-000000005741",
      });
    });
    await waitFor(() => {
      expect(screen.getByText(/已记住：回答保持简洁/)).toBeTruthy();
    });
    // The raw suggestion block never renders as reply text.
    expect(screen.queryByText(/opc:memory/)).toBeNull();
  });
});

it("keeps a main draft unsent while the saved session has an active continuation", async () => {
  mockState.providers = [readyProvider];
  mockState.sessions = [{ ...activeSession, id: continuationSessionId }];
  mockState.messagesPages = [];
  mockState.send.mockClear();
  vi.mocked(getAiPlanContinuation).mockResolvedValue(continuationFixture());
  useAiChatStore.setState({
    activeSessionId: continuationSessionId,
    input: "主会话新草稿",
  });
  renderPage(`/ai?session=${continuationSessionId}`);
  await screen.findByText(/发送新消息前，请先停止自动续办/);
  await waitFor(() =>
    expect(getAiPlanContinuation).toHaveBeenCalledWith(
      continuationSessionId,
      expect.any(AbortSignal),
    ),
  );
  fireEvent.change(screen.getByLabelText("消息输入框"), {
    target: { value: "主会话新草稿" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await screen.findByText(/该会话正在自动续办，请先停止自动续办后再发送/);
  expect(mockState.send).not.toHaveBeenCalled();
  expect(useAiChatStore.getState().input).toBe("主会话新草稿");
});
