import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { AiSessionRail } from "./AiSessionRail";
import { useAiChatStore } from "../store/aiChat";
import { useUiStore } from "../store/ui";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import {
  resetFileOperationRemindersForTests,
  useFileOperationReminders,
} from "../store/fileOperationReminders";

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
  planInbox: {
    data: {
      pages: [
        {
          data: [] as unknown[],
          meta: {
            attention_total: 0,
            window_limited: false,
          },
        },
      ],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
  },
  runInbox: {
    data: {
      items: [] as unknown[],
      meta: {
        total: 0,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 0,
      },
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  },
  agentInbox: {
    data: {
      pages: [
        {
          data: [] as unknown[],
          meta: {
            total: 0,
            approval_total: 0,
            review_total: 0,
            automation_failure_total: 0,
            knowledge_failure_total: 0,
            generation_failure_total: 0,
            provider_issue_total: 0,
            maintenance_failure_total: 0,
            window_limited: false,
          },
        },
      ],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
    fetchNextPage: vi.fn(),
    isFetchingNextPage: false,
  },
  restoreDiagnostics: {
    data: {
      status: "idle",
      restartRequired: false,
      appliedThisStartup: false,
      cleanupRequired: false,
      attentionRequired: false,
      backupId: null as string | null,
      rollbackBackupId: null as string | null,
      requestedAt: null as string | null,
      residualAppliedCount: 0,
      failedAttemptCount: 0,
      invalidEntryCount: 0,
    },
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
  useAiWorkPlanInboxQuery: () => mocks.planInbox,
  useAiAgentInboxQuery: () => mocks.agentInbox,
  useAgentRunsQuery: () => mocks.runInbox,
  useRestoreDiagnosticsQuery: () => mocks.restoreDiagnostics,
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

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="location">
      {location.pathname}
      {location.search}
    </output>
  );
}

function renderRail() {
  return render(
    <MemoryRouter>
      <AiSessionRail />
      <LocationProbe />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.query.data = [sessionOne, sessionTwo];
  mocks.query.isPending = false;
  mocks.query.isError = false;
  mocks.planInbox.data.pages = [
    {
      data: [],
      meta: { attention_total: 0, window_limited: false },
    },
  ];
  mocks.planInbox.isPending = false;
  mocks.planInbox.isError = false;
  mocks.planInbox.hasNextPage = false;
  mocks.planInbox.isFetchingNextPage = false;
  mocks.planInbox.fetchNextPage = vi.fn();
  mocks.planInbox.refetch = vi.fn();
  mocks.runInbox.data = {
    items: [],
    meta: {
      total: 0,
      activeTotal: 0,
      pendingDeliveryTotal: 0,
      succeededTotal: 0,
    },
  };
  mocks.runInbox.isPending = false;
  mocks.runInbox.isError = false;
  mocks.runInbox.refetch = vi.fn();
  mocks.agentInbox.data.pages = [
    {
      data: [],
      meta: {
        total: 0,
        approval_total: 0,
        review_total: 0,
        automation_failure_total: 0,
        knowledge_failure_total: 0,
        generation_failure_total: 0,
        provider_issue_total: 0,
        maintenance_failure_total: 0,
        window_limited: false,
      },
    },
  ];
  mocks.agentInbox.isPending = false;
  mocks.agentInbox.isError = false;
  mocks.agentInbox.refetch = vi.fn();
  mocks.agentInbox.hasNextPage = false;
  mocks.agentInbox.fetchNextPage = vi.fn();
  mocks.agentInbox.isFetchingNextPage = false;
  mocks.restoreDiagnostics.data = {
    status: "idle",
    restartRequired: false,
    appliedThisStartup: false,
    cleanupRequired: false,
    attentionRequired: false,
    backupId: null,
    rollbackBackupId: null,
    requestedAt: null,
    residualAppliedCount: 0,
    failedAttemptCount: 0,
    invalidEntryCount: 0,
  };
  mocks.restoreDiagnostics.isPending = false;
  mocks.restoreDiagnostics.isError = false;
  mocks.restoreDiagnostics.refetch = vi.fn();
  mocks.createState.mutateAsync = vi.fn(async () => ({ id: "session-9" }));
  mocks.createState.isPending = false;
  mocks.createState.error = null;
  mocks.deleteState.mutateAsync = vi.fn(async () => ({}));
  mocks.deleteState.error = null;
  useAiChatStore.setState({
    activeSessionId: "session-1",
    streaming: null,
    retainedTurns: [],
  });
  useUiStore.setState({
    agentRunDrawer: null,
    aiInboxOpen: false,
    agentRailCollapsed: false,
    rightOverviewCollapsed: false,
  });
  useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
});

afterEach(() => {
  cleanup();
  resetFileOperationRemindersForTests();
  useUiStore.setState({ aiInboxOpen: false, agentRailCollapsed: false });
  useAiChatStore.setState({
    activeSessionId: "",
    streaming: null,
    retainedTurns: [],
  });
});

describe("AiSessionRail", () => {
  it("honors the workspace queue request when the rail mounts and preserves it across remounts", () => {
    useUiStore.setState({ agentRailCollapsed: true });
    useUiStore.getState().openAiInbox();
    const first = renderRail();

    expect(screen.getByRole("button", { name: "续办队列" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText(/没有需要你处理的事项/)).toBeInTheDocument();
    expect(useUiStore.getState().agentRailCollapsed).toBe(false);

    first.unmount();
    renderRail();
    expect(screen.getByRole("button", { name: "续办队列" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    fireEvent.click(screen.getByRole("button", { name: "续办队列" }));
    expect(useUiStore.getState().aiInboxOpen).toBe(false);
    expect(screen.queryByText(/没有需要你处理的事项/)).not.toBeInTheDocument();
  });

  it("only renders inbox groups that have items and counts them", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "approval",
            id: "018f0000-0000-7000-8000-000000000591",
            created_at: today,
            action: "task.create",
            session_id: "session-1",
            session_title: "落地页任务",
            generation_id: "018f0000-0000-7000-8000-000000000592",
          },
          {
            kind: "approval",
            id: "018f0000-0000-7000-8000-000000000593",
            created_at: today,
            action: "task.update",
            session_id: "session-1",
            session_title: "落地页任务",
            generation_id: "018f0000-0000-7000-8000-000000000592",
          },
        ],
        meta: {
          total: 2,
          approval_total: 2,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    useUiStore.getState().openAiInbox();
    renderRail();

    const inbox = screen.getByRole("region", { name: "续办队列" });
    const approvals = screen.getByRole("region", { name: "等待确认" });
    expect(inbox).toContainElement(approvals);
    expect(approvals).toHaveTextContent("2");
    expect(
      screen.queryByRole("region", { name: "待验收" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("智能体计划")).not.toBeInTheDocument();
    expect(screen.queryByText(/没有需要你处理的事项/)).not.toBeInTheDocument();
  });

  it("reports a failed inbox read once and never shows it as cleared", () => {
    mocks.agentInbox.isError = true;
    useUiStore.getState().openAiInbox();
    renderRail();

    const errors = screen.getAllByText(/无法读取确认、验收与异常事项/);
    expect(errors).toHaveLength(1);
    expect(screen.queryByText(/没有需要你处理的事项/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /重试/ }));
    expect(mocks.agentInbox.refetch).toHaveBeenCalledTimes(1);
  });

  it("rediscovers an unobserved file operation and lets a person clear it", () => {
    useFileOperationReminders.setState({
      reminders: [
        {
          id: "018f0000-0000-7000-8000-000000000597",
          source: "ai_project_file",
          status: "interrupted",
          startedAt: Date.parse("2026-09-23T10:00:00Z"),
        },
        {
          id: "018f0000-0000-7000-8000-000000000598",
          source: "file_recovery",
          status: "in_flight",
          startedAt: Date.parse("2026-09-23T10:05:00Z"),
        },
      ],
    });
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    const group = screen.getByRole("region", { name: "文件操作待核对" });
    expect(group).toHaveTextContent("应用在操作进行中退出 · 结果未知");
    expect(group).toHaveTextContent("本机界面记录，非执行回执");
    expect(group).not.toHaveTextContent("进行中 ·");
    fireEvent.click(screen.getByRole("button", { name: /智能体项目文件修改/ }));
    expect(useUiStore.getState().rightPanelTab).toBe("files");

    act(() => useUiStore.getState().openAiInbox());
    fireEvent.click(
      screen.getByRole("button", { name: "已核对恢复记录，移除提醒" }),
    );
    expect(
      screen.queryByRole("region", { name: "文件操作待核对" }),
    ).not.toBeInTheDocument();
    expect(useFileOperationReminders.getState().reminders).toHaveLength(1);
  });

  it("does not label runs as planned while the unplanned run ledger is unavailable", () => {
    mocks.runInbox.isError = true;
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "agent_run",
            id: "018f0000-0000-7000-8000-000000000595",
            created_at: today,
            task_id: "018f0000-0000-7000-8000-000000000596",
            task_title: "来源未确认的执行",
            attempt: 1,
            run_status: "failed",
            output_delivery_status: "not_ready",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    useUiStore.getState().openAiInbox();
    renderRail();

    expect(screen.getByText(/无法读取独立执行/)).toBeInTheDocument();
    expect(screen.queryByText("计划内执行")).not.toBeInTheDocument();
    expect(screen.queryByText("来源未确认的执行")).not.toBeInTheDocument();
    expect(screen.queryByText(/没有需要你处理的事项/)).not.toBeInTheDocument();
  });

  it("reuses the workbench sidebar visual classes on the agent rail", () => {
    mocks.planInbox.data.pages = [
      {
        data: [],
        meta: { attention_total: 2, window_limited: false },
      },
    ];
    renderRail();

    const rail = screen.getByLabelText("会话列表");
    expect(rail.querySelector(".brand-block")).not.toBeNull();
    expect(rail.querySelector(".brand-name")).not.toBeNull();
    expect(rail.querySelector(".local-pill")).toBeNull();
    expect(screen.queryByText("v0.1.1")).not.toBeInTheDocument();
    expect(screen.queryByText("Agent")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "工作台" }),
    ).not.toBeInTheDocument();

    expect(screen.getByRole("button", { name: "新会话" })).toHaveClass(
      "nav-item",
    );
    expect(screen.getByRole("button", { name: "定时任务" })).toHaveClass(
      "nav-item",
    );
    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveClass(
      "nav-item",
    );
    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "2",
    );
    expect(
      screen.getByRole("button", { name: /续办队列/ }).querySelector(".nav-badge"),
    ).toHaveTextContent("2");
    expect(screen.getByRole("button", { name: "AI 助手设置" })).toHaveClass(
      "nav-item",
      "sidebar-settings",
    );
    expect(screen.queryByLabelText("搜索会话")).toBeNull();
    expect(screen.getByText("对话")).toHaveClass("nav-label");
    expect(screen.getByText("今天")).toHaveClass("nav-label");
  });

  it("lists sessions for the day and marks the active one", () => {
    renderRail();

    expect(screen.getByText("对话")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /^落地页任务/ }),
    ).toBeInTheDocument();
    expect(
      screen
        .getByRole("button", { name: /^落地页任务/ })
        .closest(".ai-session-row"),
    ).toHaveAttribute("data-active", "true");
    expect(screen.getByText("今天")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^落地页任务/ })).toHaveAttribute(
      "aria-current",
      "page",
    );
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

  it("opens the Agent Inbox and resumes the selected plan conversation", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000751";
    mocks.planInbox.data.pages = [
      {
        data: [
          {
            session_id: sessionId,
            session_title: "周报草稿",
            plan_title: "核对并提交周报",
            state: "awaiting_continuation",
            satisfied_step_total: 2,
            step_total: 3,
          },
        ],
        meta: { attention_total: 1, window_limited: false },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("智能体计划")).toBeInTheDocument();
    expect(screen.queryByText("对话")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /核对并提交周报/ }));

    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}&plan=1`,
    );
    expect(useUiStore.getState().aiInboxOpen).toBe(false);
    expect(screen.getByText("对话")).toBeInTheDocument();
  });

  it("shows effective plan progress while preserving the historical replacement count", () => {
    mocks.planInbox.data.pages = [
      {
        data: [
          {
            session_id: "018f0000-0000-7000-8000-000000000751",
            session_title: "原对话",
            plan_title: "带历史替代的计划",
            state: "awaiting_continuation",
            satisfied_step_total: 1,
            step_total: 4,
            superseded_step_total: 2,
          },
        ],
        meta: { attention_total: 1, window_limited: false },
      },
    ];
    renderRail();
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(
      screen.getByText("待继续 · 已满足 1/2 · 历史替代 2 步"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
  });

  it("adds unplanned actionable Runs to the inbox without changing conversations", () => {
    mocks.planInbox.data.pages = [
      {
        data: [],
        meta: { attention_total: 2, window_limited: false },
      },
    ];
    mocks.runInbox.data = {
      items: [
        {
          id: "018f0000-0000-7000-8000-000000000501",
          taskId: "018f0000-0000-7000-8000-000000000502",
          taskTitle: "整理失败的交付",
          status: "failed",
          outputDeliveryStatus: "not_ready",
          attempt: 2,
          model: "safe-model",
        },
      ],
      meta: {
        total: 1,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 0,
      },
    };
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "3",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("独立执行")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /整理失败的交付/ }));

    expect(useUiStore.getState().agentRunDrawer).toEqual({
      taskId: "018f0000-0000-7000-8000-000000000502",
      runId: "018f0000-0000-7000-8000-000000000501",
    });
    expect(useAiChatStore.getState().activeSessionId).toBe("session-1");
  });

  it("hands a planned Inbox Run back to the agent with fresh authorization", () => {
    const runId = "018f0000-0000-7000-8000-000000000551";
    const plannedTaskId = "018f0000-0000-7000-8000-000000000552";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "agent_run",
            id: runId,
            created_at: today,
            task_id: plannedTaskId,
            task_title: "计划中的发布任务",
            attempt: 4,
            run_status: "running",
            output_delivery_status: "not_ready",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("计划内执行")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.label).toBe("计划内 Agent 执行");
    expect(pending?.route).toBe(`/tasks/${plannedTaskId}?agent_run=${runId}`);
    expect(pending?.scopes).toEqual([
      "work",
      "outputs",
      "actions",
      "agent_execution",
    ]);
    expect(pending?.prompt).toContain("新的 agent_execution 授权");
    expect(pending?.prompt).toContain("不要继承上一条消息的授权");
  });

  it("shows active background continuations and opens their owning session", () => {
    const continuationId = "018f0000-0000-7000-8000-000000000571";
    const sessionId = "018f0000-0000-7000-8000-000000000572";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "continuation",
            id: continuationId,
            created_at: today,
            session_id: sessionId,
            session_title: "继续发布计划",
            continuation_status: "waiting",
            continuation_reason: "pending_approval",
            continuation_max_turns: 4,
            continuation_turns_started: 1,
            continuation_expires_at: "2026-09-23T13:00:00Z",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          continuation_total: 1,
          window_limited: false,
        } as (typeof mocks.agentInbox.data.pages)[number]["meta"] & {
          continuation_total: number;
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("后台续办")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /继续发布计划/ }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}&plan=1`,
    );
  });

  it("merges plan-external approvals and exact review destinations into the inbox", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000751";
    mocks.planInbox.data.pages = [
      {
        data: [],
        meta: { attention_total: 1, window_limited: false },
      },
    ];
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "approval",
            id: "018f0000-0000-7000-8000-000000000601",
            created_at: today,
            action: "project.update",
            session_id: sessionId,
            session_title: "周报草稿",
            generation_id: "018f0000-0000-7000-8000-000000000602",
          },
          {
            kind: "review",
            id: "018f0000-0000-7000-8000-000000000603",
            created_at: today,
            task_id: "018f0000-0000-7000-8000-000000000604",
            task_title: "检查 Agent 交付",
            submission_id: "018f0000-0000-7000-8000-000000000603",
            sequence: 2,
            submission_origin: "manual",
          },
        ],
        meta: {
          total: 2,
          approval_total: 1,
          review_total: 1,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "3",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("等待确认")).toBeInTheDocument();
    expect(screen.getByText("待验收")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /修改项目/ }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}&generation=018f0000-0000-7000-8000-000000000602&proposal=018f0000-0000-7000-8000-000000000601`,
    );
    expect(useUiStore.getState().aiInboxOpen).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: /检查 Agent 交付/ }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/tasks/018f0000-0000-7000-8000-000000000604/submissions/018f0000-0000-7000-8000-000000000603",
    );
  });

  it("opens a pending permission request in its saved conversation without granting it", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000831";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "access_request",
            id: "018f0000-0000-7000-8000-000000000832",
            created_at: today,
            session_id: sessionId,
            session_title: "核对权限",
            generation_id: "018f0000-0000-7000-8000-000000000832",
          },
        ],
        meta: { ...mocks.agentInbox.data.pages[0].meta, total: 1 },
      },
    ];
    renderRail();
    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("权限待核对")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /工作台权限请求/ }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}`,
    );
    expect(useUiStore.getState().aiInboxOpen).toBe(false);
  });

  it("surfaces the latest failed automation attempt and opens its exact detail", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "automation_failure",
            id: "018f0000-0000-7000-8000-000000000701",
            created_at: today,
            rule_id: "018f0000-0000-7000-8000-000000000702",
            rule_name: "每日查看今日任务",
            attempt: 2,
            retryable: true,
            retry_at: null,
            error_code: "ACTION_WRITE_FAILED",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 1,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("自动化异常")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /每日查看今日任务/ }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/settings/automation?run=018f0000-0000-7000-8000-000000000701",
    );
  });

  it("hands a failed knowledge index to the agent with only the metadata scope", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "knowledge_failure",
            id: "018f0000-0000-7000-8000-000000000801",
            created_at: today,
            source_id: "018f0000-0000-7000-8000-000000000802",
            source_name: "billing-guide.md",
            operation: "reindex",
            attempt: 2,
            error_code: "KNOWLEDGE_INDEX_INTERRUPTED",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 1,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).not.toBeNull();
    expect(pending?.scopes).toEqual(["knowledge_actions"]);
    expect(pending?.label).toBe("知识库索引失败");
    expect(pending?.route).toBe(
      "/knowledge?source=018f0000-0000-7000-8000-000000000802&job=018f0000-0000-7000-8000-000000000801",
    );
    expect(pending?.prompt).toContain("billing-guide.md");
    expect(pending?.prompt).toContain("KNOWLEDGE_INDEX_INTERRUPTED");
    expect(pending?.prompt).toContain("待我确认");
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
  });

  it("hands an unfinished agent run to the agent with execution scope only on demand", () => {
    mocks.runInbox.data = {
      items: [
        {
          id: "018f0000-0000-7000-8000-000000000901",
          taskId: "018f0000-0000-7000-8000-000000000902",
          taskTitle: "整理季度复盘",
          status: "failed",
          attempt: 2,
          outputDeliveryStatus: "not_ready",
          model: "deepseek-flash",
        },
      ],
      meta: {
        total: 1,
        activeTotal: 0,
        pendingDeliveryTotal: 0,
        succeededTotal: 0,
      },
    };
    renderRail();
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.scopes).toEqual([
      "work",
      "outputs",
      "actions",
      "agent_execution",
    ]);
    expect(pending?.route).toBe(
      "/tasks/018f0000-0000-7000-8000-000000000902?agent_run=018f0000-0000-7000-8000-000000000901",
    );
    expect(pending?.prompt).toContain("整理季度复盘");
  });

  it("hands a pending review submission to the agent with output review scope", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "review",
            id: "018f0000-0000-7000-8000-000000001201",
            created_at: today,
            task_id: "018f0000-0000-7000-8000-000000001202",
            task_title: "核对落地页交付",
            submission_id: "018f0000-0000-7000-8000-000000001201",
            sequence: 3,
            submission_origin: "manual",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 1,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.label).toBe("任务提交待验收");
    expect(pending?.scopes).toEqual(["work", "outputs", "actions"]);
    expect(pending?.route).toBe(
      "/tasks/018f0000-0000-7000-8000-000000001202/submissions/018f0000-0000-7000-8000-000000001201",
    );
    expect(pending?.prompt).toContain("核对落地页交付");
    expect(pending?.prompt).toContain("workspace_task_submissions");
    expect(pending?.prompt).toContain("不要把未读取的文件正文当作已检查");
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
  });

  it("surfaces a current knowledge index failure and opens its exact source and job", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "knowledge_failure",
            id: "018f0000-0000-7000-8000-000000000801",
            created_at: today,
            source_id: "018f0000-0000-7000-8000-000000000802",
            source_name: "billing-guide.md",
            operation: "reindex",
            attempt: 2,
            error_code: "KNOWLEDGE_INDEX_INTERRUPTED",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 1,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("知识库异常")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /billing-guide\.md/ }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "/knowledge?source=018f0000-0000-7000-8000-000000000802&job=018f0000-0000-7000-8000-000000000801",
    );
  });

  it("surfaces the current failed generation and returns to its conversation", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "generation_failure",
            id: "018f0000-0000-7000-8000-000000000901",
            created_at: today,
            session_id: "session-2",
            session_title: "周报草稿",
            generation_id: "018f0000-0000-7000-8000-000000000901",
            error_code: "AI_PROVIDER_ERROR",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 1,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("对话异常")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /周报草稿/ }));
    expect(useAiChatStore.getState().activeSessionId).toBe("session-2");
    expect(screen.getByText("对话")).toBeInTheDocument();
  });

  it("distinguishes a startup-interrupted generation and requires manual continuation", () => {
    const generationId = "018f0000-0000-7000-8000-000000000921";
    const sessionId = "018f0000-0000-7000-8000-000000000922";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "generation_failure",
            id: generationId,
            created_at: today,
            session_id: sessionId,
            session_title: "中断的周报草稿",
            generation_id: generationId,
            error_code: "AI_GENERATION_INTERRUPTED",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 1,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("服务重启时中断 · 可手动继续")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "对话生成中断",
      route: `/ai?session=${sessionId}`,
      scopes: [],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      "没有自动恢复",
    );
  });

  it("hands a failed generation to the agent without workspace scopes", () => {
    const generationId = "018f0000-0000-7000-8000-000000000911";
    const sessionId = "018f0000-0000-7000-8000-000000000912";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "generation_failure",
            id: generationId,
            created_at: today,
            session_id: sessionId,
            session_title: "周报草稿",
            generation_id: generationId,
            error_code: "AI_PROVIDER_ERROR",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 1,
          provider_issue_total: 0,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "对话生成失败",
      route: `/ai?session=${sessionId}`,
      routeLabel: "打开失败会话",
      scopes: [],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      "不要自动重试生成",
    );
  });

  it("surfaces a provider issue and opens the AI provider settings", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "provider_issue",
            id: "018f0000-0000-7000-8000-000000001001",
            created_at: today,
            provider_id: "018f0000-0000-7000-8000-000000001001",
            provider_name: "本地推理服务",
            provider_model: "qwen-local",
            provider_status: "unavailable",
            health_status: "unhealthy",
            error_code: "AI_ENDPOINT_UNREACHABLE",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 1,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("供应商待处理")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /本地推理服务/ }));
    expect(useUiStore.getState().settingsOpen).toBe(true);
    expect(useUiStore.getState().settingsModule).toBe("ai");
    expect(screen.getByText("对话")).toBeInTheDocument();
  });

  it("hands a provider issue to the agent without workspace scopes", () => {
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "provider_issue",
            id: "018f0000-0000-7000-8000-000000001011",
            created_at: today,
            provider_id: "018f0000-0000-7000-8000-000000001011",
            provider_name: "本地推理服务",
            provider_model: "qwen-local",
            provider_status: "unavailable",
            health_status: "unhealthy",
            error_code: "AI_ENDPOINT_UNREACHABLE",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 1,
          maintenance_failure_total: 0,
          window_limited: false,
        },
      },
    ];
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "供应商配置待处理",
      route: "/ai?settings=ai",
      routeLabel: "打开 AI 助手设置",
      scopes: [],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      "不要要求查看或复述密钥原文",
    );
  });

  it("surfaces pending database recovery and opens data settings", () => {
    mocks.restoreDiagnostics.data = {
      status: "restart_required",
      restartRequired: true,
      appliedThisStartup: false,
      cleanupRequired: false,
      attentionRequired: false,
      backupId: "018f0000-0000-7000-8000-000000001101",
      rollbackBackupId: "018f0000-0000-7000-8000-000000001102",
      requestedAt: today,
      residualAppliedCount: 0,
      failedAttemptCount: 0,
      invalidEntryCount: 0,
    };
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("数据恢复")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /本地数据恢复/ }));
    expect(useUiStore.getState().settingsOpen).toBe(true);
    expect(useUiStore.getState().settingsModule).toBe("data");
    expect(screen.getByText("对话")).toBeInTheDocument();
  });

  it("hands restore diagnostics to the agent without workspace scopes", () => {
    mocks.restoreDiagnostics.data = {
      status: "attention_required",
      restartRequired: false,
      appliedThisStartup: false,
      cleanupRequired: false,
      attentionRequired: true,
      backupId: null,
      rollbackBackupId: null,
      requestedAt: today,
      residualAppliedCount: 1,
      failedAttemptCount: 2,
      invalidEntryCount: 3,
    };
    renderRail();

    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toMatchObject({
      label: "数据恢复诊断",
      route: "/ai?settings=data",
      routeLabel: "打开数据设置",
      scopes: [],
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue?.prompt).toContain(
      "不要自动重启服务",
    );
  });

  it("surfaces a system maintenance incident and opens its original Inbox item", () => {
    const incidentId = "018f0000-0000-7000-8000-000000001201";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "maintenance_failure",
            id: incidentId,
            created_at: today,
            component: "storage",
            operation: "low_space",
            error_code: "STORAGE_LOW_SPACE",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 1,
          window_limited: false,
        },
      },
    ];
    renderRail();

    expect(screen.getByRole("button", { name: /续办队列/ })).toHaveTextContent(
      "1",
    );
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    expect(screen.getByText("系统维护")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /本地存储空间不足/ }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/inbox/${incidentId}`,
    );
  });

  it("hands a system maintenance incident to the agent as a bounded Inbox action", () => {
    const incidentId = "018f0000-0000-7000-8000-000000001301";
    mocks.agentInbox.data.pages = [
      {
        data: [
          {
            kind: "maintenance_failure",
            id: incidentId,
            created_at: today,
            component: "backup",
            operation: "verify",
            error_code: "backup_verify_failed",
          },
        ],
        meta: {
          total: 1,
          approval_total: 0,
          review_total: 0,
          automation_failure_total: 0,
          knowledge_failure_total: 0,
          generation_failure_total: 0,
          provider_issue_total: 0,
          maintenance_failure_total: 1,
          window_limited: false,
        },
      },
    ];
    renderRail();
    fireEvent.click(screen.getByRole("button", { name: /续办队列/ }));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending?.label).toBe("系统维护事项");
    expect(pending?.route).toBe(`/inbox/${incidentId}`);
    expect(pending?.scopes).toEqual(["work", "actions"]);
    expect(pending?.prompt).toContain("本地备份校验失败");
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("backup.verify");
    expect(pending?.prompt).toContain("inbox.resolve");
    expect(pending?.prompt).toContain("不要自动重试备份");
    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
  });
});
