import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type {
  AiProvider,
  AiWorkspaceGrant,
  AiWorkspaceScope,
} from "../types/models";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useWorkspacePanels } from "../store/workspacePanels";
import { WorkspaceChat } from "./WorkspaceChat";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import { getAiPlanContinuation } from "../api/aiPlanContinuation";
import {
  continuationFixture,
  continuationSessionId,
} from "../test/aiContinuationFixture";
vi.mock("../api/aiPlanContinuation", async () => ({
  ...(await vi.importActual("../api/aiPlanContinuation")),
  getAiPlanContinuation: vi.fn(async () => null),
}));

const mocks = vi.hoisted(() => ({
  capturePlanOnly: false,
  planProps: [] as Array<{
    sessionId: string;
    onContinue?: (prompt: string, scopes: AiWorkspaceScope[]) => void;
  }>,
  recheckProposalId: undefined as string | undefined,
  create: vi.fn(),
  providers: [] as AiProvider[],
  sessions: [] as Array<{ id: string; persist: boolean }>,
  pages: [] as Array<{ data: Array<Record<string, unknown>> }>,
  accessProps: [] as Array<{
    provider: AiProvider;
    value?: AiWorkspaceGrant;
    disabled: boolean;
    persist?: boolean;
    openRequest?: number;
    recommendedScopes?: string[];
  }>,
  actionProps: [] as Array<{ generationId: string; sessionId?: string }>,
}));

vi.mock("./AiWorkPlan", async (importOriginal) => {
  const original = await importOriginal<typeof import("./AiWorkPlan")>();
  return {
    ...original,
    AiWorkPlan: (props: Parameters<typeof original.AiWorkPlan>[0]) => {
      mocks.planProps.push(props);
      return mocks.capturePlanOnly ? null : <original.AiWorkPlan {...props} />;
    },
  };
});

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  createAiSession: mocks.create,
}));

vi.mock("../api/hooks", () => ({
  useAiProvidersQuery: () => ({ data: mocks.providers }),
  useAiSessionsQuery: () => ({ data: mocks.sessions }),
  useAiMessagesInfiniteQuery: () => ({
    data: { pages: mocks.pages },
    isError: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
  }),
}));

vi.mock("./AiWorkspaceAccess", async (importOriginal) => ({
  AiWorkspaceGrantSummary: (
    await importOriginal<typeof import("./AiWorkspaceAccess")>()
  ).AiWorkspaceGrantSummary,
  AiWorkspaceAccess: (props: {
    provider: AiProvider;
    value?: AiWorkspaceGrant;
    disabled: boolean;
    persist?: boolean;
    openRequest?: number;
    recommendedScopes?: string[];
    onChange: (value: AiWorkspaceGrant | undefined) => void;
  }) => {
    mocks.accessProps.push(props);
    return (
      <div>
        <button
          aria-label="模拟授权工作台"
          disabled={props.disabled}
          onClick={() =>
            props.onChange({
              provider_version: props.provider.version,
              scopes: ["work", "actions"],
            })
          }
          type="button"
        >
          授权
        </button>
        <span data-testid="side-workspace-grant">
          {props.value
            ? `${props.value.provider_version}:${props.value.scopes.join(",")}`
            : "none"}
        </span>
      </div>
    );
  },
}));

vi.mock("./AiWorkspaceActions", () => ({
  AiWorkspaceActions: (props: {
    generationId: string;
    sessionId?: string;
    onContinue?: (request: AiActionContinuation) => void;
  }) => {
    mocks.actionProps.push(props);
    return (
      <div data-testid={`side-actions-${props.generationId}`}>
        {props.sessionId ?? "missing-session"}
        {props.onContinue ? (
          <button
            onClick={() =>
              props.onContinue!({
                generationId: props.generationId,
                prompt: "根据真实操作回执继续原需求。",
                scopes: ["work", "actions"],
                ...(mocks.recheckProposalId
                  ? {
                      recheckProposalId: mocks.recheckProposalId,
                      sourceSessionId: props.sessionId,
                    }
                  : {}),
              })
            }
          >
            模拟接续操作
          </button>
        ) : null}
      </div>
    );
  },
}));

const originalSend = useAiChatStore.getState().send;

it("blocks a manual side send during a lease and preserves side draft and main session", async () => {
  mocks.capturePlanOnly = true;
  vi.mocked(getAiPlanContinuation).mockResolvedValue(continuationFixture());
  const send = vi.fn();
  useAiChatStore.setState({ send, activeSessionId: "main-session" });
  openChat({
    chatSessionId: continuationSessionId,
    draft: "侧边尚未发送的草稿",
  });
  renderChat();
  await screen.findByText(/发送新消息前，请先停止自动续办/);
  await waitFor(() =>
    expect(getAiPlanContinuation).toHaveBeenCalledWith(
      continuationSessionId,
      expect.any(AbortSignal),
    ),
  );
  await act(async () => {});
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await screen.findByText(/该会话正在自动续办，请先停止自动续办后再发送/);
  expect(send).not.toHaveBeenCalled();
  expect(useWorkspacePanels.getState().tabs[0].draft).toBe(
    "侧边尚未发送的草稿",
  );
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
});

function provider(id = "provider", version = 7): AiProvider {
  return {
    id,
    name: id,
    kind: "local",
    protocol: "openai_chat",
    base_url: "http://127.0.0.1:11434/v1",
    model: "test",
    status: "ready",
    health_status: "healthy",
    health_error_code: null,
    has_key: false,
    last_health_at: null,
    version,
    created_at: "2026-09-18T00:00:00Z",
    updated_at: "2026-09-18T00:00:00Z",
  };
}

function session(id: string) {
  return { id, persist: true };
}

function ChatView() {
  const panel = useWorkspacePanels((state) => state.tabs[0]);
  return panel ? <WorkspaceChat panel={panel} /> : null;
}

function renderChat() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <ChatView />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

function openChat(
  patch: Partial<{
    chatSessionId: string;
    draft: string;
    providerId: string;
  }> = {},
) {
  useWorkspacePanels
    .getState()
    .open({ kind: "chat", title: "chat", ...patch }, "side");
}

beforeEach(() => {
  vi.mocked(getAiPlanContinuation).mockResolvedValue(null);
  vi.clearAllMocks();
  mocks.providers = [provider()];
  mocks.sessions = [];
  mocks.pages = [];
  mocks.accessProps = [];
  mocks.actionProps = [];
  mocks.recheckProposalId = undefined;
  mocks.capturePlanOnly = false;
  mocks.planProps = [];
  useWorkspacePanels.setState({ tabs: [], activeId: "", splitId: null });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  useAiChatStore.setState({
    send: originalSend,
    streaming: null,
    interrupted: null,
    streamError: null,
    streamOwner: "main",
    activeSessionId: "",
    retainedTurns: [],
    acceptedCommand: null,
    accessRequests: {},
  });
});

afterEach(() => {
  cleanup();
  useAiChatStore.setState({ send: originalSend });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
});

it.each(["session", "provider", "provider-version", "owner", "streaming"])(
  "ignores a late side plan continuation after %s changes away and back",
  (source) => {
    mocks.capturePlanOnly = true;
    mocks.providers = [provider(), provider("other")];
    const send = vi.fn();
    useAiChatStore.setState({ send });
    openChat({
      chatSessionId: "side-session",
      draft: "原草稿",
      providerId: "provider",
    });
    renderChat();
    const oldContinue = mocks.planProps.at(-1)?.onContinue;
    expect(oldContinue).toBeTypeOf("function");
    act(() => {
      if (source === "streaming")
        useAiChatStore.setState({
          streaming: {
            sessionId: "side-session",
            generationId: "generation",
            text: "",
            reasoning: "",
          },
        });
      if (source === "provider-version")
        mocks.providers = [provider("provider", 8), provider("other")];
      const current = useWorkspacePanels.getState().tabs[0];
      useWorkspacePanels.setState({
        tabs: [
          {
            ...current,
            ...(source === "session" ? { chatSessionId: "other-session" } : {}),
            ...(source === "provider" ? { providerId: "other" } : {}),
            ...(source === "owner" ? { id: "another-side" } : {}),
            draft: "切换期间的草稿",
          },
        ],
      });
    });
    act(() => {
      useAiChatStore.setState({ streaming: null });
      mocks.providers = [provider(), provider("other")];
      const current = useWorkspacePanels.getState().tabs[0];
      useWorkspacePanels.setState({
        tabs: [
          {
            ...current,
            id: "side",
            chatSessionId: "side-session",
            providerId: "provider",
            draft: "返回后的新草稿",
          },
        ],
      });
    });
    fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
    const accessRequest = mocks.accessProps.at(-1)?.openRequest;
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "7:work,actions",
    );
    act(() => oldContinue!("已过期的计划接续", ["work", "actions"]));
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue("返回后的新草稿");
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "7:work,actions",
    );
    expect(mocks.accessProps.at(-1)?.openRequest).toBe(accessRequest);
    expect(send).not.toHaveBeenCalled();
  },
);

it("keeps a side plan callback stable and appends to the latest draft with fresh consent", () => {
  mocks.capturePlanOnly = true;
  const send = vi.fn();
  useAiChatStore.setState({ send });
  openChat({ chatSessionId: "side-session", draft: "原草稿" });
  renderChat();
  const continuePlan = mocks.planProps.at(-1)?.onContinue;
  expect(continuePlan).toBeTypeOf("function");
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
    target: { value: "核验期间的新补充" },
  });
  expect(mocks.planProps.at(-1)?.onContinue).toBe(continuePlan);
  const beforeAccess = mocks.accessProps.at(-1)?.openRequest ?? 0;
  act(() => continuePlan!("继续已核验计划", ["work", "actions"]));
  expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(
    "核验期间的新补充\n\n继续已核验计划",
  );
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent("none");
  expect(mocks.accessProps.at(-1)?.openRequest).toBe(beforeAccess + 1);
  expect(send).not.toHaveBeenCalled();
});

it("invalidates a checked side plan while a send is being accepted, even after creating clears", async () => {
  mocks.capturePlanOnly = true;
  let finishSend!: (value: {
    sessionId: string;
    accepted: boolean;
    error: null;
    errorCode: null;
    cancelled: boolean;
  }) => void;
  const pending = new Promise<Parameters<typeof finishSend>[0]>((resolve) => {
    finishSend = resolve;
  });
  const send = vi.fn(() => pending);
  useAiChatStore.setState({ send });
  openChat({ chatSessionId: "side-session", draft: "先发送的消息" });
  renderChat();
  const oldContinue = mocks.planProps.at(-1)?.onContinue;
  expect(oldContinue).toBeTypeOf("function");
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(mocks.planProps.at(-1)?.onContinue).toBeUndefined();
  await act(async () => {
    finishSend({
      sessionId: "side-session",
      accepted: false,
      error: null,
      errorCode: null,
      cancelled: false,
    });
  });
  expect(mocks.planProps.at(-1)?.onContinue).toBeTypeOf("function");
  fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
    target: { value: "受理后的新草稿" },
  });
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  const accessRequest = mocks.accessProps.at(-1)?.openRequest;
  act(() => oldContinue!("过期续办", ["work", "actions"]));
  expect(screen.getByLabelText("侧边聊天输入")).toHaveValue("受理后的新草稿");
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
    "7:work,actions",
  );
  expect(mocks.accessProps.at(-1)?.openRequest).toBe(accessRequest);
  expect(send).toHaveBeenCalledTimes(1);
});

it("keeps report navigation tied to a retained side conversation", () => {
  const sessionId = "018f0000-0000-7000-8000-000000005834";
  const route =
    "/focus?report=summary&date_from=2026-09-01&date_to=2026-09-18&timezone=UTC";
  useAiChatStore.setState({
    activeSessionId: "unrelated-main-session",
    retainedTurns: [
      {
        sessionId,
        generationId: "g",
        text: `[查看侧聊报告](${route})`,
        reasoning: "",
        userText: null,
        status: "completed",
        createdAt: "2026-09-18T00:00:00Z",
      },
    ],
  });
  openChat({ chatSessionId: sessionId });
  renderChat();
  expect(screen.getByRole("link", { name: "查看侧聊报告" })).toHaveAttribute(
    "href",
    `${route}&return_session=${sessionId}`,
  );
  expect(useAiChatStore.getState().activeSessionId).toBe(
    "unrelated-main-session",
  );
  expect(mocks.actionProps).toEqual([]);
});

it.each(["side:side", "main"])(
  "shows live and unconfirmed progress with owner %s in the matching side session",
  (owner) => {
    const progress = [
      {
        sequence: 2,
        kind: "model_turn" as const,
        turn_index: 1,
        status: "running" as const,
        started_at: new Date().toISOString(),
        duration_ms: 0,
      },
    ];
    useAiChatStore.setState({
      streamOwner: owner,
      streaming: {
        sessionId: "side-session",
        generationId: "g",
        text: "",
        reasoning: "",
        progress,
      },
    });
    openChat({ chatSessionId: "side-session" });
    const view = renderChat();
    expect(screen.getByText("模型推理 · 第 1 轮 · 进行中")).toBeInTheDocument();
    expect(screen.queryByText("正在思考…")).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "停止侧边生成" }),
    ).toBeInTheDocument();
    view.unmount();
    useAiChatStore.setState({
      interrupted: useAiChatStore.getState().streaming,
      streaming: null,
    });
    renderChat();
    expect(
      screen.getByText("模型推理 · 第 1 轮 · 结果未确认"),
    ).toBeInTheDocument();
  },
);

it("sends an independent session without an implicit workspace grant", async () => {
  mocks.create.mockResolvedValue({ id: "side-session" });
  const send = vi.fn().mockResolvedValue({
    accepted: true,
    sessionId: "side-session",
    error: null,
    errorCode: null,
  });
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ draft: "Question" });
  renderChat();
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(send.mock.calls[0][0]).toEqual({
    sessionId: "side-session",
    providerId: "provider",
    message: "Question",
    owner: "side:side",
  });
  expect(send.mock.calls[0][0]).not.toHaveProperty("workspace");
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
  await waitFor(() =>
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(""),
  );
});

it("sends an exact one-message work and actions grant, then consumes it after acceptance", async () => {
  mocks.sessions = [session("side-session")];
  const send = vi.fn().mockResolvedValue({
    accepted: true,
    sessionId: "side-session",
    error: null,
    errorCode: null,
  });
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ chatSessionId: "side-session", draft: "Create it" });
  renderChat();

  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
    "7:work,actions",
  );
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));

  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(send.mock.calls[0][0]).toEqual({
    sessionId: "side-session",
    providerId: "provider",
    message: "Create it",
    owner: "side:side",
    workspace: { provider_version: 7, scopes: ["work", "actions"] },
  });
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    ),
  );
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");

  fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
    target: { value: "Next" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
  expect(send.mock.calls[1][0]).not.toHaveProperty("workspace");
});

it.each([false, true])(
  "continues actions in the source side chat with fresh consent and a one-message receipt source (recheck=%s)",
  async (recheck) => {
    const generationId = "018f0000-0000-7000-8000-000000000762";
    const recheckProposalId = "018f0000-0000-7000-8000-000000000764";
    mocks.recheckProposalId = recheck ? recheckProposalId : undefined;
    const send = vi.fn(async (_input: unknown) => ({
      sessionId: "side-session",
      accepted: true,
      error: null,
      errorCode: null,
      cancelled: false,
    }));
    useAiChatStore.setState({ activeSessionId: "main-session", send });
    mocks.pages = [
      {
        data: [
          {
            id: "message",
            session_id: "side-session",
            generation_id: generationId,
            role: "assistant",
            content: "已确认",
            status: "completed",
            citations: [],
            citation_status: null,
          },
        ],
      },
    ];
    openChat({ chatSessionId: "side-session", draft: "侧聊未发送草稿" });
    renderChat();
    fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(
      "侧聊未发送草稿\n\n根据真实操作回执继续原需求。",
    );
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    );
    expect(screen.queryByLabelText("本条工作台授权范围")).toBeNull();
    expect(mocks.accessProps.at(-1)?.openRequest).toBeGreaterThan(0);
    expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
    expect(send).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
    expect(screen.getByLabelText("本条工作台授权范围")).toHaveTextContent(
      "下一条消息已选 2 项：工作事项、操作建议",
    );
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: "side-session",
          actionReceiptGenerationId: generationId,
          ...(recheck ? { actionRecheckProposalId: recheckProposalId } : {}),
          workspace: { provider_version: 7, scopes: ["work", "actions"] },
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
    expect(screen.queryByLabelText("本条工作台授权范围")).toBeNull();
    fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
      target: { value: "下一条" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1][0]).not.toHaveProperty(
      "actionReceiptGenerationId",
    );
    expect(send.mock.calls[1][0]).not.toHaveProperty("actionRecheckProposalId");
    expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
  },
);

it.each([true, false])(
  "consumes a recovered side receipt only in its owner (newer typing: %s)",
  async (typedNewDraft) => {
    const generationId = "018f0000-0000-7000-8000-000000000765";
    const recheckProposalId = "018f0000-0000-7000-8000-000000000766";
    mocks.recheckProposalId = recheckProposalId;
    const send = vi.fn(async (_input: unknown) => ({
      sessionId: "side-session",
      accepted: false,
      error: "network",
      errorCode: "AI_STREAM_ERROR",
      cancelled: false,
    }));
    useAiChatStore.setState({ send });
    mocks.pages = [
      {
        data: [
          {
            id: "message",
            session_id: "side-session",
            generation_id: generationId,
            role: "assistant",
            content: "已确认",
            status: "completed",
            citations: [],
          },
        ],
      },
    ];
    openChat({ chatSessionId: "side-session" });
    renderChat();
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    act(() =>
      useAiChatStore.setState({
        acceptedCommand: {
          sequence: 1,
          requestId: "main-request",
          owner: "main",
          sessionId: "side-session",
          actionReceiptGenerationId: generationId,
          actionRecheckProposalId: recheckProposalId,
          actionReceiptAttachmentId: (
            send.mock.calls[0][0] as { actionReceiptAttachmentId: string }
          ).actionReceiptAttachmentId,
        },
      }),
    );
    expect(
      screen.getByRole("button", { name: "移除重新核验上下文" }),
    ).toBeVisible();
    if (typedNewDraft)
      fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
        target: { value: "侧聊新草稿" },
      });
    act(() =>
      useAiChatStore.setState({
        acceptedCommand: {
          sequence: 2,
          requestId: "side-request",
          owner: "side:side",
          sessionId: "side-session",
          actionReceiptGenerationId: generationId,
          actionRecheckProposalId: recheckProposalId,
          actionReceiptAttachmentId: (
            send.mock.calls[0][0] as { actionReceiptAttachmentId: string }
          ).actionReceiptAttachmentId,
        },
      }),
    );
    expect(
      screen.queryByRole("button", { name: "移除重新核验上下文" }),
    ).toBeNull();
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(
      typedNewDraft ? "侧聊新草稿" : "",
    );
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    );
    fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
      target: { value: "核对后的下一条消息" },
    });
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
    expect(send.mock.calls[1][0]).not.toHaveProperty(
      "actionReceiptGenerationId",
    );
    expect(send.mock.calls[1][0]).not.toHaveProperty("actionRecheckProposalId");
  },
);

it("keeps a newly prepared side recheck when an older matching source is recovered", async () => {
  const generationId = "018f0000-0000-7000-8000-000000000765";
  const recheckProposalId = "018f0000-0000-7000-8000-000000000766";
  mocks.recheckProposalId = recheckProposalId;
  const send = vi.fn(async (_input: unknown) => ({
    sessionId: "side-session",
    accepted: false,
    error: "network",
    errorCode: "AI_STREAM_ERROR",
    cancelled: false,
  }));
  useAiChatStore.setState({ send });
  mocks.pages = [
    {
      data: [
        {
          id: "message",
          session_id: "side-session",
          generation_id: generationId,
          role: "assistant",
          content: "已确认",
          status: "completed",
          citations: [],
        },
      ],
    },
  ];
  openChat({ chatSessionId: "side-session" });
  renderChat();
  fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  const previousAttachmentId = (
    send.mock.calls[0][0] as { actionReceiptAttachmentId: string }
  ).actionReceiptAttachmentId;
  expect(previousAttachmentId).toEqual(expect.any(String));
  fireEvent.click(screen.getByRole("button", { name: "移除重新核验上下文" }));
  fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
    target: { value: "新侧聊草稿" },
  });
  act(() =>
    useAiChatStore.setState({
      acceptedCommand: {
        sequence: 1,
        requestId: "old-side-request",
        owner: "side:side",
        sessionId: "side-session",
        actionReceiptGenerationId: generationId,
        actionRecheckProposalId: recheckProposalId,
        actionReceiptAttachmentId: previousAttachmentId,
      },
    }),
  );
  expect(
    screen.getByRole("button", { name: "移除重新核验上下文" }),
  ).toBeVisible();
  expect(screen.getByLabelText("侧边聊天输入")).toHaveValue("新侧聊草稿");
  expect(screen.getByTestId("side-workspace-grant")).not.toHaveTextContent(
    "none",
  );
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
  const next = send.mock.calls[1][0] as {
    actionReceiptAttachmentId: string;
    actionRecheckProposalId: string;
    workspace: unknown;
  };
  expect(next.actionReceiptAttachmentId).not.toBe(previousAttachmentId);
  expect(next.actionRecheckProposalId).toBe(recheckProposalId);
  expect(next.workspace).toBeDefined();
});

it.each(["before-acceptance", "after-acceptance"])(
  "preserves a newly edited identical side draft %s when the old send finishes",
  async (editAt) => {
    const generationId = "018f0000-0000-7000-8000-000000000768";
    const recheckProposalId = "018f0000-0000-7000-8000-000000000769";
    mocks.recheckProposalId = recheckProposalId;
    let finish!: (result: Awaited<ReturnType<typeof originalSend>>) => void;
    const send = vi.fn(
      (_input: Parameters<typeof originalSend>[0]) =>
        new Promise<Awaited<ReturnType<typeof originalSend>>>((resolve) => {
          finish = resolve;
        }),
    );
    useAiChatStore.setState({ send });
    mocks.pages = [
      {
        data: [
          {
            id: "message",
            session_id: "side-session",
            generation_id: generationId,
            role: "assistant",
            content: "已确认",
            status: "completed",
            citations: [],
          },
        ],
      },
    ];
    openChat({ chatSessionId: "side-session" });
    renderChat();
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    const sent = send.mock.calls[0][0];
    const editSameDraft = () => {
      fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
        target: { value: "临时改写" },
      });
      fireEvent.change(screen.getByLabelText("侧边聊天输入"), {
        target: { value: sent.message },
      });
    };
    if (editAt === "before-acceptance") editSameDraft();
    act(() =>
      useAiChatStore.setState({
        acceptedCommand: {
          sequence: 1,
          requestId: "deferred-side-request",
          owner: "side:side",
          sessionId: "side-session",
          actionReceiptGenerationId: generationId,
          actionRecheckProposalId: recheckProposalId,
          actionReceiptAttachmentId: sent.actionReceiptAttachmentId,
        },
      }),
    );
    expect(
      screen.queryByRole("button", { name: "移除重新核验上下文" }),
    ).toBeNull();
    if (editAt === "after-acceptance") {
      expect(screen.getByLabelText("侧边聊天输入")).toHaveValue("");
      editSameDraft();
    } else {
      expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(sent.message);
    }
    await act(async () => {
      finish({
        sessionId: "side-session",
        accepted: true,
        cancelled: false,
        error: null,
        errorCode: null,
      });
    });
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(sent.message);
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    );
  },
);

it.each([false, true])(
  "allows removing the receipt attachment before sending (recheck: %s)",
  async (recheck) => {
    mocks.recheckProposalId = recheck
      ? "018f0000-0000-7000-8000-000000000766"
      : undefined;
    const send = vi.fn(async (_input: unknown) => ({
      sessionId: "side-session",
      accepted: true,
      error: null,
      errorCode: null,
      cancelled: false,
    }));
    useAiChatStore.setState({ send });
    mocks.pages = [
      {
        data: [
          {
            id: "message",
            session_id: "side-session",
            generation_id: "018f0000-0000-7000-8000-000000000762",
            role: "assistant",
            content: "已确认",
            status: "completed",
            citations: [],
          },
        ],
      },
    ];
    openChat({ chatSessionId: "side-session" });
    renderChat();
    fireEvent.click(screen.getByRole("button", { name: "模拟接续操作" }));
    if (recheck)
      expect(
        screen.getByRole("button", { name: "发送侧边消息" }),
      ).toBeDisabled();
    fireEvent.click(
      screen.getByRole("button", {
        name: recheck ? "移除重新核验上下文" : "移除操作回执",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    expect(send.mock.calls[0][0]).not.toHaveProperty(
      "actionReceiptGenerationId",
    );
    expect(send.mock.calls[0][0]).not.toHaveProperty("actionRecheckProposalId");
    expect(send.mock.calls[0][0]).not.toHaveProperty("workspace");
  },
);

it("prepares a workbench record handoff inside the side chat without sending", async () => {
  const send = vi.fn();
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ chatSessionId: "side-session", draft: "先看这个" });
  expect(
    useAiWorkbenchHandoff
      .getState()
      .stage("task", "018f0000-0000-7000-8000-000000005001"),
  ).toBe(true);
  renderChat();
  const initialAccess = mocks.accessProps.at(-1);

  fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));

  await waitFor(() =>
    expect(
      (screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement).value,
    ).toContain("/tasks/018f0000-0000-7000-8000-000000005001"),
  );
  const preparedRecordDraft = (
    screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement
  ).value;
  expect(preparedRecordDraft).toContain("workspace_get");
  expect(preparedRecordDraft).toContain("先看这个\n\n");
  expect(mocks.accessProps.at(-1)?.recommendedScopes).toEqual([
    "work",
    "actions",
  ]);
  expect(mocks.accessProps.at(-1)?.openRequest ?? 0).toBeGreaterThan(
    initialAccess?.openRequest ?? 0,
  );
  expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
  expect(send).not.toHaveBeenCalled();
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
});

it("keeps a side-chat access request on its own session and requires a fresh review", () => {
  const send = vi.fn();
  useAiChatStore.setState({
    activeSessionId: "main-session",
    send,
    accessRequests: {
      "side-session": {
        generationId: "generation-1",
        scopes: ["clients", "work"],
      },
    },
  });
  openChat({ chatSessionId: "side-session", draft: "已有草稿" });
  renderChat();
  const initialAccess = mocks.accessProps.at(-1);
  fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
  expect(
    (screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement).value,
  ).toContain("已有草稿\n\n请继续处理我刚才的请求");
  const preparedDraft = (
    screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement
  ).value;
  fireEvent.click(screen.getByRole("button", { name: "核对权限并准备继续" }));
  expect(
    (screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement).value,
  ).toBe(preparedDraft);
  expect(mocks.accessProps.at(-1)?.recommendedScopes).toEqual([
    "clients",
    "work",
  ]);
  expect(mocks.accessProps.at(-1)?.openRequest ?? 0).toBeGreaterThan(
    initialAccess?.openRequest ?? 0,
  );
  expect(useAiChatStore.getState().accessRequests["side-session"]).toEqual({
    generationId: "generation-1",
    scopes: ["clients", "work"],
  });
  expect(screen.getByRole("region", { name: "工作台权限请求" })).toBeVisible();
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");
  expect(send).not.toHaveBeenCalled();
});

it("prepares a scoped queue issue in the side chat and clears older one-message grants", async () => {
  const send = vi.fn();
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ chatSessionId: "side-session", draft: "帮我继续" });
  expect(
    useAiWorkbenchHandoff.getState().stageIssue({
      label: "失败的 Agent Run",
      route: "/tasks/018f0000-0000-7000-8000-000000005001",
      prompt:
        "请用 workspace_agent_runs 检查 run=018f0000-0000-7000-8000-000000005101 的失败原因。",
      scopes: ["work", "outputs", "actions", "agent_execution"],
    }),
  ).toBe(true);
  renderChat();
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
    "7:work,actions",
  );
  const initialAccess = mocks.accessProps.at(-1);

  fireEvent.click(screen.getByRole("button", { name: "带入问题并选择权限" }));

  await waitFor(() =>
    expect(
      (screen.getByLabelText("侧边聊天输入") as HTMLTextAreaElement).value,
    ).toContain("workspace_agent_runs"),
  );
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    ),
  );
  expect(mocks.accessProps.at(-1)?.recommendedScopes).toEqual([
    "work",
    "outputs",
    "actions",
    "agent_execution",
  ]);
  expect(mocks.accessProps.at(-1)?.openRequest ?? 0).toBeGreaterThan(
    initialAccess?.openRequest ?? 0,
  );
  expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  expect(send).not.toHaveBeenCalled();
});

it("prepares an unscoped queue issue in the side chat without opening permissions", async () => {
  const send = vi.fn();
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ chatSessionId: "side-session" });
  expect(
    useAiWorkbenchHandoff.getState().stageIssue({
      label: "Provider 异常",
      route: "/settings?settings=ai",
      routeLabel: "打开 AI 设置",
      prompt: "请解释 Provider 自检失败的常见排查步骤，不要修改设置。",
      scopes: [],
    }),
  ).toBe(true);
  renderChat();
  const initialAccess = mocks.accessProps.at(-1);

  fireEvent.click(screen.getByRole("button", { name: "带入问题" }));

  await waitFor(() =>
    expect(screen.getByLabelText("侧边聊天输入")).toHaveValue(
      "请解释 Provider 自检失败的常见排查步骤，不要修改设置。",
    ),
  );
  expect(mocks.accessProps.at(-1)?.recommendedScopes).toEqual([
    "work",
    "actions",
  ]);
  expect(mocks.accessProps.at(-1)?.openRequest ?? 0).toBe(
    initialAccess?.openRequest ?? 0,
  );
  expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  expect(send).not.toHaveBeenCalled();
});

it("rebinds a pre-session grant to the created side session and keeps it for an unaccepted retry", async () => {
  mocks.create.mockResolvedValue({ id: "created-side-session" });
  const send = vi.fn().mockResolvedValue({
    accepted: false,
    sessionId: "created-side-session",
    error: "retry safely",
    errorCode: "AI_STREAM_ERROR",
  });
  useAiChatStore.setState({ activeSessionId: "main-session", send });
  openChat({ draft: "Create from a new side chat" });
  renderChat();

  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
    "7:work,actions",
  );
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));

  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(mocks.create).toHaveBeenCalledTimes(1);
  expect(send.mock.calls[0][0]).toEqual({
    sessionId: "created-side-session",
    providerId: "provider",
    message: "Create from a new side chat",
    owner: "side:side",
    workspace: { provider_version: 7, scopes: ["work", "actions"] },
  });
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "7:work,actions",
    ),
  );
  expect(useWorkspacePanels.getState().tabs[0]?.chatSessionId).toBe(
    "created-side-session",
  );
  expect(useAiChatStore.getState().activeSessionId).toBe("main-session");

  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(2));
  expect(mocks.create).toHaveBeenCalledTimes(1);
  expect(send.mock.calls[1][0]).toEqual(send.mock.calls[0][0]);
});

it.each([
  {
    code: "AI_STREAM_ERROR",
    expected: "7:work,actions",
    label: "keeps an unaccepted grant for a safe retry",
  },
  {
    code: "AI_WORKSPACE_PROVIDER_CHANGED",
    expected: "none",
    label: "clears a grant when the provider identity changed",
  },
])("$label", async ({ code, expected }) => {
  mocks.sessions = [session("side-session")];
  const send = vi.fn().mockResolvedValue({
    accepted: false,
    sessionId: "side-session",
    error: "rejected",
    errorCode: code,
  });
  useAiChatStore.setState({ send });
  openChat({ chatSessionId: "side-session", draft: "Try" });
  renderChat();
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      expected,
    ),
  );
});

it("does not reuse a grant after the provider identity, version, or session changes", async () => {
  mocks.providers = [provider("provider", 7), provider("other", 3)];
  mocks.sessions = [session("session-a"), session("session-b")];
  const send = vi.fn().mockResolvedValue({
    accepted: false,
    sessionId: "session-b",
    error: "rejected",
    errorCode: "AI_STREAM_ERROR",
  });
  useAiChatStore.setState({ send });
  openChat({
    chatSessionId: "session-a",
    providerId: "provider",
    draft: "First",
  });
  const view = renderChat();

  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
    "7:work,actions",
  );

  fireEvent.change(screen.getByLabelText("侧边聊天模型"), {
    target: { value: "other" },
  });
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    ),
  );

  fireEvent.change(screen.getByLabelText("侧边聊天模型"), {
    target: { value: "provider" },
  });
  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  mocks.providers = [provider("provider", 8), provider("other", 3)];
  view.rerender(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <ChatView />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    ),
  );

  fireEvent.click(screen.getByRole("button", { name: "模拟授权工作台" }));
  useWorkspacePanels.getState().update("side", {
    chatSessionId: "session-b",
    draft: "After identity changes",
  });
  await waitFor(() =>
    expect(screen.getByTestId("side-workspace-grant")).toHaveTextContent(
      "none",
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "发送侧边消息" }));
  await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
  expect(send.mock.calls[0][0]).not.toHaveProperty("workspace");
  expect(send.mock.calls[0][0]).toMatchObject({
    providerId: "provider",
    sessionId: "session-b",
  });
});

it("renders workspace actions only for persisted assistant generations and binds the message session", () => {
  const messageSession = "018f0000-0000-7000-8000-000000005834";
  mocks.pages = [
    {
      data: [
        {
          id: "user-message",
          role: "user",
          content: "Question",
          reasoning: null,
          session_id: messageSession,
          generation_id: "user-generation",
          citation_status: "not_requested",
          citations: [],
        },
        {
          id: "assistant-with-generation",
          role: "assistant",
          content: "Proposal ready",
          reasoning: null,
          session_id: messageSession,
          generation_id: "assistant-generation",
          citation_status: "not_requested",
          citations: [],
        },
        {
          id: "assistant-without-generation",
          role: "assistant",
          content: "Plain answer",
          reasoning: null,
          session_id: messageSession,
          generation_id: null,
          citation_status: "not_requested",
          citations: [],
        },
      ],
    },
  ];
  openChat({ chatSessionId: "different-panel-session" });
  renderChat();

  expect(
    screen.getByTestId("side-actions-assistant-generation"),
  ).toHaveTextContent(messageSession);
  expect(mocks.actionProps).toEqual([
    { generationId: "assistant-generation", sessionId: messageSession },
  ]);
  expect(screen.queryByTestId("side-actions-user-generation")).toBeNull();
});

it.each(["missing", "no_evidence", "invalid", undefined] as const)(
  "shows %s citation evidence for a retained side conversation",
  (status) => {
    useAiChatStore.setState({
      retainedTurns: [
        {
          sessionId: "side-session",
          generationId: "g",
          text: "回答",
          reasoning: "",
          status: "incomplete",
          userText: null,
          createdAt: "2026-09-18T00:00:00Z",
          citationEvidence: status ? { status, items: [] } : undefined,
        },
      ],
    });
    openChat({ chatSessionId: "side-session" });
    renderChat();
    const text =
      status === "missing"
        ? "没有提供结构化引用"
        : status === "no_evidence"
          ? "没有足够的已授权资料证据"
          : status === "invalid"
            ? "已被 Sidecar 拒绝"
            : "未能恢复引用信息";
    expect(screen.getByRole("status")).toHaveTextContent(text);
    expect(mocks.actionProps).toEqual([]);
  },
);
