import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AiAssistantPage } from "./AiAssistantPage";

afterEach(() => {
  cleanup();
});

const mockState = vi.hoisted(() => {
  const mutation = () => ({
    mutateAsync: vi.fn(async (_input?: unknown) => ({})),
    isPending: false,
    error: null as unknown,
  });
  return {
    providers: [] as unknown[],
    sessions: [] as unknown[],
    messagesPages: [] as { data: unknown[]; meta: { has_more: boolean } }[],
    streaming: null as { sessionId: string; text: string } | null,
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
        error: string | null;
      }> => ({ sessionId: "", cancelled: false, error: null }),
    ),
    stop: vi.fn(),
    createSession: mutation(),
    deleteSession: mutation(),
    createTask: mutation(),
    attachTask: mutation(),
    createMemory: mutation(),
    navigate: vi.fn(),
  };
});

vi.mock("../api/hooks", () => ({
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
  useDeleteAiSession: () => mockState.deleteSession,
  useCreateTask: () => mockState.createTask,
  useAttachTaskToAiMessage: () => mockState.attachTask,
  useTaskQuery: (id: string | null) => ({
    data: id && mockState.taskDetail ? mockState.taskDetail : undefined,
    isPending: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("../store/ui", () => ({
  useUiStore: () => ({}),
}));

vi.mock("react-router-dom", async (importOriginal) => {
  const original = await importOriginal<typeof import("react-router-dom")>();
  return {
    ...original,
    useNavigate: () => mockState.navigate,
  };
});

vi.mock("../components/ProjectSelect", () => ({
  ProjectSelect: () => <div data-testid="project-select" />,
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
    created_at: "2026-09-01T12:00:00Z",
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter>
      <AiAssistantPage />
    </MemoryRouter>,
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
  it("guides to settings when no provider is registered", () => {
    mockState.providers = [];
    renderPage();
    expect(screen.getByText(/尚未配置 AI 供应商/)).toBeTruthy();
  });

  it("shows the suggestion chip for a task block and strips the raw block from display", () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    renderPage();
    expect(taskChip("建议任务：写周报")).toBeTruthy();
    expect(screen.getByText(/好的，建议如下/)).toBeTruthy();
    expect(screen.queryByText(/\[opc:task\]/)).toBeNull();
  });

  it("opens the confirm card, creates the task through the task API, and attaches the reference", async () => {
    mockState.providers = [readyProvider];
    mockState.sessions = [activeSession];
    mockState.messagesPages = [
      { data: [assistantMessage()], meta: { has_more: false } },
    ];
    mockState.createTask.mutateAsync = vi.fn(async () => ({
      id: "task-1",
      title: "写周报",
    }));
    mockState.attachTask.mutateAsync = vi.fn(async () => ({}));
    renderPage();

    fireEvent.click(taskChip("建议任务：写周报"));
    const titleInput = screen.getByDisplayValue("写周报") as HTMLInputElement;
    expect(titleInput).toBeTruthy();
    fireEvent.click(screen.getByText("确认创建"));

    await waitFor(() => {
      expect(mockState.createTask.mutateAsync).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "写周报",
          priority: "P2",
          dueDate: "2026-09-02",
        }),
      );
      expect(mockState.attachTask.mutateAsync).toHaveBeenCalledWith({
        messageId: "message-1",
        taskId: "task-1",
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
        error: string | null;
      }> => ({ sessionId: "session-1", cancelled: false, error: null }),
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
              '已了解你的偏好。[opc:memory]{"content":"回答保持简洁"}[/opc:memory]',
          }),
        ],
        next_before_created_at: null,
        next_before_id: null,
      },
    ];
    renderPage();

    expect(screen.getByText(/记住偏好：回答保持简洁/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "记住" }));

    await waitFor(() => {
      expect(mockState.createMemory.mutateAsync).toHaveBeenCalledWith({
        content: "回答保持简洁",
      });
    });
    await waitFor(() => {
      expect(screen.getByText(/已记住：回答保持简洁/)).toBeTruthy();
    });
    // The raw suggestion block never renders as reply text.
    expect(screen.queryByText(/opc:memory/)).toBeNull();
  });
});
