import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { AiSessionRail } from "./AiSessionRail";
import { useAiChatStore } from "../store/aiChat";
import { useUiStore } from "../store/ui";

const mocks = vi.hoisted(() => ({
  refetch: vi.fn(),
  create: vi.fn(),
  remove: vi.fn(),
  query: {
    data: [] as unknown[],
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  createState: {
    mutateAsync: vi.fn(),
    isPending: false,
    error: null as unknown,
  },
  deleteState: {
    mutateAsync: vi.fn(),
    error: null as unknown,
  },
}));

vi.mock("../api/hooks", () => ({
  useAiSessionsQuery: () => mocks.query,
  useCreateAiSession: () => mocks.createState,
  useDeleteAiSession: () => mocks.deleteState,
}));

const today = new Date().toISOString();
const sessionOne = {
  id: "session-1",
  title: "落地页任务",
  updated_at: today,
  version: 7,
};
const sessionTwo = {
  id: "session-2",
  title: "周报草稿",
  updated_at: today,
  version: 2,
};

function renderRail() {
  return render(
    <MemoryRouter>
      <AiSessionRail />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.query.data = [sessionOne, sessionTwo];
  mocks.query.isPending = false;
  mocks.query.isError = false;
  mocks.createState.mutateAsync = vi.fn(async () => ({ id: "session-9" }));
  mocks.createState.isPending = false;
  mocks.createState.error = null;
  mocks.deleteState.mutateAsync = vi.fn(async () => ({}));
  mocks.deleteState.error = null;
  useAiChatStore.setState({
    activeSessionId: "session-1",
    sessionFilter: "",
    streaming: null,
    retainedTurns: [],
  });
});

afterEach(() => {
  cleanup();
  useAiChatStore.setState({
    activeSessionId: "",
    sessionFilter: "",
    streaming: null,
    retainedTurns: [],
  });
});

describe("AiSessionRail", () => {
  it("lists sessions for the day and marks the active one", () => {
    renderRail();

    expect(screen.getByLabelText("会话列表")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^落地页任务/ }),
    ).toBeInTheDocument();
    expect(
      screen
        .getByRole("button", { name: /^落地页任务/ })
        .closest(".ai-session-row"),
    ).toHaveAttribute("data-active", "true");
    expect(screen.getByText("今天")).toBeInTheDocument();
  });

  it("filters sessions by the rail search box", () => {
    renderRail();

    fireEvent.change(screen.getByLabelText("搜索会话"), {
      target: { value: "周报" },
    });

    expect(
      screen.getByRole("button", { name: /^周报草稿/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^落地页任务/ })).toBeNull();
  });

  it("selects a session and starts a new one", async () => {
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /^周报草稿/ }));
    expect(useAiChatStore.getState().activeSessionId).toBe("session-2");

    fireEvent.click(screen.getByRole("button", { name: "新会话" }));
    await waitFor(() => {
      expect(useAiChatStore.getState().activeSessionId).toBe("session-9");
    });
  });

  it("deletes a session with its current version and drops local turns", async () => {
    useAiChatStore.setState({
      retainedTurns: [
        {
          sessionId: "session-1",
          generationId: "generation-1",
          userText: "问题",
          text: "临时回复",
          reasoning: "",
          status: "completed",
          createdAt: today,
        },
      ],
    });
    renderRail();

    fireEvent.click(
      screen.getByRole("button", { name: "删除会话 落地页任务" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "删除" }));

    await waitFor(() => {
      expect(mocks.deleteState.mutateAsync).toHaveBeenCalledWith({
        id: "session-1",
        expectedVersion: 7,
      });
      expect(useAiChatStore.getState().retainedTurns).toEqual([]);
      expect(useAiChatStore.getState().activeSessionId).toBe("");
    });
  });

  it("locks session controls while a reply is streaming", () => {
    useAiChatStore.setState({
      streaming: { sessionId: "session-1", text: "…", reasoning: "" },
    });
    renderRail();

    expect(screen.getByRole("button", { name: "新会话" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /^落地页任务/ })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "删除会话 落地页任务" }),
    ).toBeDisabled();
  });

  it("exposes the scheduled-task, project and settings rows", () => {
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: "隐藏会话侧边栏" }));
    expect(useUiStore.getState().agentRailCollapsed).toBe(true);
    useUiStore.setState({ agentRailCollapsed: false });

    fireEvent.click(screen.getByRole("button", { name: "定时任务" }));
    expect(useUiStore.getState().settingsOpen).toBe(true);
    expect(useUiStore.getState().settingsModule).toBe("automation");

    useUiStore.setState({ settingsOpen: false, settingsModule: "general" });
    fireEvent.click(screen.getByRole("button", { name: "项目" }));
    expect(useUiStore.getState().settingsOpen).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "AI 助手设置" }));
    expect(useUiStore.getState().settingsOpen).toBe(true);
    expect(useUiStore.getState().settingsModule).toBe("ai");
  });
});
