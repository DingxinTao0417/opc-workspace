import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
vi.mock("./AgentRunRestartModal", () => ({
  AgentRunRestartModal: ({
    taskId,
    runId,
    onClose,
  }: {
    taskId: string;
    runId: string;
    onClose: () => void;
  }) => (
    <section aria-label="新执行预览入口">
      {taskId} / {runId}
      <button type="button" onClick={onClose}>
        关闭新执行预览
      </button>
    </section>
  ),
}));
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { useControlledFilesQuery, useTaskArtifactQuery } from "../api/hooks";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type {
  AgentRun,
  AgentRunSummary,
  ControlledFile,
  TaskArtifact,
} from "../types/models";
import type { AiWorkPlan } from "../api/aiWorkPlan";
import type {
  AiAgentChildGenerationCancelTarget,
  AiAgentFamily,
} from "../api/aiAgentFamily";
import { useAiChatStore } from "../store/aiChat";
import { useAgentRunFileApply } from "../store/agentRunFileApply";
import { useUiStore } from "../store/ui";
import { AgentsTab, ManagedFilesTab } from "./WorkspaceResources";

const mocks = vi.hoisted(() => ({
  cancelChildGeneration: vi.fn(),
  cancelChildGenerations: vi.fn(),
  cancelRun: vi.fn(),
  exactError: false,
  exactPending: false,
  exactQuery: vi.fn(),
  exactRefetch: vi.fn(),
  exactRun: undefined as AgentRun | undefined,
  allTotal: 1,
  activeTotal: 0,
  pendingDeliveryTotal: 1,
  succeededTotal: 0,
  pendingRuns: [] as unknown[],
  delegatedRuns: null as unknown[] | null,
  delegatedQueryInputs: [] as unknown[],
  agentFamilyQueryInputs: [] as unknown[],
  agentFamily: null as AiAgentFamily | null,
  runQueryInputs: [] as unknown[],
  runs: [] as unknown[],
  retryDelivery: vi.fn(),
  retryRun: vi.fn(),
  invalidateAgentRunDeliveryFacts: vi.fn(),
  getWorkPlan: vi.fn(),
  agentInbox: undefined as unknown,
  workPlanInbox: undefined as unknown,
}));

const pendingRun: AgentRunSummary = {
  id: "018f0000-0000-7000-8000-000000000701",
  taskId: "018f0000-0000-7000-8000-000000000702",
  taskTitle: "任务甲",
  assignmentId: "018f0000-0000-7000-8000-000000000703",
  actorId: "018f0000-0000-7000-8000-000000000704",
  adapterId: "018f0000-0000-7000-8000-000000000705",
  createdByActorId: "018f0000-0000-7000-8000-000000000706",
  parentRunId: null,
  attempt: 1,
  status: "running",
  providerId: "018f0000-0000-7000-8000-000000000707",
  model: "test-model",
  resultBytes: null,
  errorCode: null,
  outputDeliveryStatus: "pending",
  outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
  submissionId: null,
  artifactId: null,
  startedAt: "2026-09-18T10:00:01Z",
  completedAt: null,
  createdAt: "2026-09-18T10:00:00Z",
};

const artifactFile: ControlledFile = {
  id: "018f0000-0000-7000-8000-000000000721",
  scope: "artifact",
  name: "验收说明.md",
  mimeType: "text/markdown",
  sizeBytes: 128,
  sha256: "a".repeat(64),
  ownerLabel: "任务甲",
  contentRoute:
    "/api/v1/task-artifacts/018f0000-0000-7000-8000-000000000721/content",
  updatedAt: "2026-09-18T10:00:00Z",
};

const artifactDetail: TaskArtifact = {
  id: artifactFile.id,
  taskId: pendingRun.taskId,
  submissionId: "018f0000-0000-7000-8000-000000000722",
  submissionStatus: "pending_review",
  position: 1,
  storageKind: "text",
  name: artifactFile.name,
  mimeType: "text/markdown",
  sizeBytes: 128,
  sha256: artifactFile.sha256,
  requiresFollowup: false,
  producedByActorId: pendingRun.actorId,
  producedByActor: {
    id: pendingRun.actorId,
    displayName: "Agent",
    type: "agent",
    status: "active",
    isBuiltin: false,
    version: 1,
  },
  recordedByActorId: pendingRun.createdByActorId,
  recordedByActor: {
    id: pendingRun.createdByActorId,
    displayName: "Owner",
    type: "owner",
    status: "active",
    isBuiltin: true,
    version: 1,
  },
  integrityStatus: "verified",
  integrityCheckedAt: null,
  deletedAt: null,
  deletedByActorId: null,
  deletedByActor: null,
  deleteReason: null,
  createdAt: "2026-09-18T10:00:00Z",
  contentText: "请核对验收说明。",
  referenceUrl: null,
  structuredJson: null,
};

function detailFromSummary(
  summary: AgentRunSummary,
  resultText: string | null,
): AgentRun {
  const { taskTitle: _taskTitle, ...metadata } = summary;
  return { ...metadata, resultText };
}

vi.mock("../api/client", () => ({
  ApiError: class ApiError extends Error {},
  cancelAgentRun: mocks.cancelRun,
  retryAgentRun: mocks.retryRun,
}));
vi.mock("./AgentReworkRetryModal", () => ({
  AgentReworkRetryModal: ({
    runId,
    taskId,
    onClose,
  }: {
    runId: string;
    taskId: string;
    onClose: () => void;
  }) => (
    <section aria-label="精确返工重试预览">
      {runId} / {taskId}
      <button onClick={onClose}>关闭返工预览</button>
    </section>
  ),
}));

vi.mock("../api/aiWorkPlan", async () => ({
  ...(await vi.importActual("../api/aiWorkPlan")),
  getAiWorkPlan: mocks.getWorkPlan,
  planInboxStateLabels: {
    awaiting_continuation: "待继续",
  },
}));

vi.mock("../api/aiAgentFamily", async () => ({
  ...(await vi.importActual("../api/aiAgentFamily")),
  cancelAiAgentChildGeneration: mocks.cancelChildGeneration,
  cancelAiAgentChildGenerations: mocks.cancelChildGenerations,
}));

vi.mock("../api/hooks", () => ({
  aiAgentFamilyQueryKey: (sessionId: string) => [
    "ai",
    "sessions",
    sessionId,
    "agent-family",
  ],
  useAiAgentFamilyQuery: (sessionId: string, enabled = true) => {
    mocks.agentFamilyQueryInputs.push({ sessionId, enabled });
    return {
      data: enabled
        ? (mocks.agentFamily ?? {
            sessionId,
            parent: null,
            children: [],
            asOf: "2026-09-22T10:02:00Z",
          })
        : undefined,
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    };
  },
  useAgentRunQuery: (runId: string | null, taskId: string | null) => {
    mocks.exactQuery(runId, taskId);
    return {
      data: mocks.exactRun,
      isPending: mocks.exactPending,
      isError: mocks.exactError,
      refetch: mocks.exactRefetch,
    };
  },
  useAgentRunsQuery: (
    input: {
      page?: number;
      pageSize?: number;
      outputDeliveryStatus?: string;
    },
    enabled = true,
  ) => {
    mocks.runQueryInputs.push(input);
    const pendingOnly = input.outputDeliveryStatus === "pending";
    return {
      data: enabled
        ? {
            items: pendingOnly ? mocks.pendingRuns : mocks.runs,
            meta: {
              page: input.page ?? 1,
              pageSize: input.pageSize ?? 20,
              total: pendingOnly ? mocks.pendingRuns.length : mocks.allTotal,
              activeTotal: mocks.activeTotal,
              pendingDeliveryTotal: mocks.pendingDeliveryTotal,
              succeededTotal: mocks.succeededTotal,
            },
          }
        : undefined,
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    };
  },
  useAiSessionDelegatedRunsQuery: (
    sessionId: string,
    input: { page?: number; pageSize?: number; outputDeliveryStatus?: string },
    enabled = true,
  ) => {
    mocks.delegatedQueryInputs.push({ sessionId, input, enabled });
    const items =
      mocks.delegatedRuns ??
      mocks.runs.map((run) => ({
        runId: (run as AgentRunSummary).id,
        run,
        delegation: {
          sessionId,
          generationId: currentPlan.generation_id,
          proposalId: currentPlan.steps[1].proposal_id,
          action: "agent_run.start",
          createdAt: currentPlan.created_at,
          decidedAt: currentPlan.as_of,
          plan: {
            version: currentPlan.version,
            stepId: currentPlan.steps[1].id,
            stepTitle: currentPlan.steps[1].title,
          },
        },
      }));
    return {
      data: enabled
        ? {
            items,
            meta: {
              page: input.page ?? 1,
              pageSize: input.pageSize ?? 20,
              total: items.length,
              activeTotal: mocks.activeTotal,
              pendingDeliveryTotal: mocks.pendingDeliveryTotal,
              unavailableTotal: items.filter(
                (item) => !(item as { run: unknown }).run,
              ).length,
              asOf: currentPlan.as_of,
            },
          }
        : undefined,
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    };
  },
  useAiAgentInboxQuery: () => mocks.agentInbox,
  useAiWorkPlanInboxQuery: () => mocks.workPlanInbox,
  invalidateAgentRunDeliveryFacts: mocks.invalidateAgentRunDeliveryFacts,
  useRetryAgentRunOutputDelivery: () => ({
    mutate: mocks.retryDelivery,
    isPending: false,
    isError: false,
  }),
  useControlledFilesQuery: vi.fn(),
  useTaskArtifactQuery: vi.fn(),
}));

beforeEach(() => {
  useAiChatStore.setState({ activeSessionId: "", streaming: null });
  useUiStore.setState({
    aiInboxOpen: false,
    agentRailCollapsed: false,
    rightOverviewCollapsed: false,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
  });
  useAgentRunFileApply.setState({
    candidate: null,
    review: null,
    loading: false,
    error: null,
  });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
  mocks.runs.splice(0, mocks.runs.length, pendingRun);
  mocks.pendingRuns.splice(0, mocks.pendingRuns.length, pendingRun);
  mocks.runQueryInputs.splice(0, mocks.runQueryInputs.length);
  mocks.delegatedQueryInputs.splice(0, mocks.delegatedQueryInputs.length);
  mocks.agentFamilyQueryInputs.splice(0, mocks.agentFamilyQueryInputs.length);
  mocks.delegatedRuns = null;
  mocks.agentFamily = null;
  mocks.cancelChildGeneration.mockResolvedValue({ cancelRequested: true });
  mocks.cancelChildGenerations.mockImplementation(
    async (
      parentSessionId: string,
      targets: AiAgentChildGenerationCancelTarget[],
    ) => ({
      parentSessionId,
      items: targets.map((target) => ({ ...target, cancelRequested: true })),
    }),
  );
  mocks.allTotal = 1;
  mocks.activeTotal = 0;
  mocks.pendingDeliveryTotal = 1;
  mocks.succeededTotal = 0;
  mocks.exactRun = detailFromSummary(pendingRun, null);
  mocks.exactPending = false;
  mocks.exactError = false;
  mocks.getWorkPlan.mockResolvedValue(null);
  mocks.workPlanInbox = {
    data: {
      pages: [
        {
          data: [],
          meta: {
            attention_total: 0,
            running_total: 0,
            needs_approval_total: 0,
            needs_recovery_total: 0,
          },
        },
      ],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  mocks.agentInbox = {
    data: {
      pages: [
        {
          data: [],
          meta: { approval_total: 0, review_total: 0 },
        },
      ],
    },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  };
  vi.mocked(useControlledFilesQuery).mockReturnValue({
    data: { items: [artifactFile], meta: { page: 1, pageSize: 50, total: 1 } },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  } as never);
  vi.mocked(useTaskArtifactQuery).mockReturnValue({
    data: artifactDetail,
    isPending: false,
    isError: false,
    refetch: vi.fn(),
  } as never);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  useAiChatStore.setState({ activeSessionId: "", streaming: null });
  useUiStore.setState({
    aiInboxOpen: false,
    agentRailCollapsed: false,
    rightOverviewCollapsed: false,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
  });
  useAgentRunFileApply.setState({
    candidate: null,
    review: null,
    loading: false,
    error: null,
  });
  useAiWorkbenchHandoff.setState({
    pending: null,
    pendingIssue: null,
    revision: 0,
  });
});

const currentPlan: AiWorkPlan = {
  session_id: "018f0000-0000-7000-8000-000000000751",
  version: 3,
  generation_id: "018f0000-0000-7000-8000-000000000752",
  generation_status: "completed",
  title: "整理发布素材",
  created_at: "2026-09-20T10:00:00Z",
  as_of: "2026-09-20T10:02:00Z",
  steps: [
    {
      id: "draft",
      title: "整理初稿",
      kind: "analysis",
      depends_on: [],
      report: "reported_done",
      state: "reported_done",
      ready: true,
      satisfied: true,
    },
    {
      id: "run",
      title: "生成交付文件",
      kind: "action",
      depends_on: ["draft"],
      proposal_id: "018f0000-0000-7000-8000-000000000753",
      state: "running",
      ready: true,
      satisfied: false,
      evidence: {
        proposal_id: "018f0000-0000-7000-8000-000000000753",
        generation_id: "018f0000-0000-7000-8000-000000000752",
        action: "agent_run.start",
        status: "confirmed",
      },
    },
  ],
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

function attentionQuery(
  data: unknown[],
  meta: Record<string, number | boolean>,
) {
  return {
    data: { pages: [{ data, meta }] },
    isPending: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: false,
  };
}

function renderAgentsTab() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/today"]}>
        <AgentsTab />
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function renderManagedFilesTab() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <ManagedFilesTab />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("AgentsTab", () => {
  it("reviews and confirms one exact batch of selected active child generations", async () => {
    const firstChild = "018f0000-0000-7000-8000-000000000760";
    const firstGeneration = "018f0000-0000-7000-8000-000000000761";
    const secondChild = "018f0000-0000-7000-8000-000000000762";
    const secondGeneration = "018f0000-0000-7000-8000-000000000763";
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.agentFamily = {
      sessionId: currentPlan.session_id,
      parent: null,
      children: [
        {
          sessionId: firstChild,
          proposalId: "018f0000-0000-7000-8000-000000000764",
          taskName: "fact_check",
          status: "streaming",
          errorCode: null,
          available: true,
          pendingApprovals: 0,
          activeGenerationId: firstGeneration,
          createdAt: "2026-09-22T10:00:00Z",
          decidedAt: "2026-09-22T10:01:00Z",
        },
        {
          sessionId: secondChild,
          proposalId: "018f0000-0000-7000-8000-000000000765",
          taskName: "review_later",
          status: "queued",
          errorCode: null,
          available: true,
          pendingApprovals: 0,
          activeGenerationId: secondGeneration,
          createdAt: "2026-09-22T10:00:00Z",
          decidedAt: "2026-09-22T10:01:00Z",
        },
      ],
      asOf: "2026-09-22T10:02:00Z",
    };
    renderAgentsTab();

    fireEvent.click(
      screen.getByRole("checkbox", { name: "选择停止 fact_check" }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "选择停止 review_later" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "停止所选 (2)" }));

    const review = screen.getByRole("region", { name: "批量停止确认" });
    expect(within(review).getByText("fact_check")).toBeInTheDocument();
    expect(within(review).getByText("review_later")).toBeInTheDocument();
    expect(
      within(review).getByText(/不能撤回已发出的远端请求/),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "确认停止 2 个" }));

    await waitFor(() =>
      expect(mocks.cancelChildGenerations).toHaveBeenCalledWith(
        currentPlan.session_id,
        [
          { childSessionId: firstChild, generationId: firstGeneration },
          { childSessionId: secondChild, generationId: secondGeneration },
        ],
      ),
    );
    expect(screen.getAllByText("已请求停止 · 等待状态更新")).toHaveLength(2);
  });

  it("links an available child and keeps an unavailable child non-navigable", () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.agentFamily = {
      sessionId: currentPlan.session_id,
      parent: null,
      children: [
        {
          sessionId: "018f0000-0000-7000-8000-000000000760",
          proposalId: "018f0000-0000-7000-8000-000000000761",
          taskName: "fact_check",
          status: "streaming",
          errorCode: null,
          available: true,
          pendingApprovals: 2,
          activeGenerationId: null,
          createdAt: "2026-09-22T10:00:00Z",
          decidedAt: "2026-09-22T10:01:00Z",
        },
        {
          sessionId: "018f0000-0000-7000-8000-000000000762",
          proposalId: "018f0000-0000-7000-8000-000000000763",
          taskName: "review_later",
          status: "unavailable",
          errorCode: null,
          available: false,
          pendingApprovals: 0,
          activeGenerationId: null,
          createdAt: "2026-09-22T10:00:00Z",
          decidedAt: "2026-09-22T10:01:00Z",
        },
      ],
      asOf: "2026-09-22T10:02:00Z",
    };
    renderAgentsTab();

    expect(screen.getByRole("link", { name: /fact_check/ })).toHaveAttribute(
      "href",
      "/ai?session=018f0000-0000-7000-8000-000000000760",
    );
    expect(screen.getByText("review_later").closest("a")).toBeNull();
    expect(screen.getByText("会话不可用")).toBeInTheDocument();
    expect(screen.getByText("待人工核对 2 项")).toBeInTheDocument();
    expect(mocks.agentFamilyQueryInputs).toContainEqual({
      sessionId: currentPlan.session_id,
      enabled: true,
    });
  });

  it("links a delegated child back to its exact parent conversation", () => {
    const childSessionId = "018f0000-0000-7000-8000-000000000760";
    useAiChatStore.setState({ activeSessionId: childSessionId });
    mocks.agentFamily = {
      sessionId: childSessionId,
      parent: {
        sessionId: currentPlan.session_id,
        proposalId: "018f0000-0000-7000-8000-000000000761",
        taskName: "fact_check",
        status: "completed",
        errorCode: null,
        available: true,
        pendingApprovals: 0,
        activeGenerationId: null,
        createdAt: "2026-09-22T10:00:00Z",
        decidedAt: "2026-09-22T10:01:00Z",
      },
      children: [],
      asOf: "2026-09-22T10:02:00Z",
    };
    renderAgentsTab();

    expect(screen.getByRole("link", { name: /返回父会话/ })).toHaveAttribute(
      "href",
      `/ai?session=${currentPlan.session_id}`,
    );
  });

  it("defaults an active conversation to its exact delegated runs and preserves the global ledger", () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    renderAgentsTab();

    expect(screen.getByRole("button", { name: "当前对话" })).toHaveClass(
      "ov-scope-active",
    );
    expect(screen.getByText("计划步骤：生成交付文件")).toBeInTheDocument();
    expect(mocks.delegatedQueryInputs).toContainEqual(
      expect.objectContaining({
        sessionId: currentPlan.session_id,
        enabled: true,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "全部记录" }));
    expect(screen.getByRole("button", { name: "全部记录" })).toHaveClass(
      "ov-scope-active",
    );
    expect(mocks.runQueryInputs).toContainEqual(
      expect.objectContaining({ page: 1, pageSize: 20 }),
    );
  });

  it("keeps a missing delegated Run as a link to its exact historical approval", () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.delegatedRuns = [
      {
        runId: pendingRun.id,
        run: null,
        delegation: {
          sessionId: currentPlan.session_id,
          generationId: currentPlan.generation_id,
          proposalId: currentPlan.steps[1].proposal_id,
          action: "agent_run.start",
          createdAt: currentPlan.created_at,
          decidedAt: currentPlan.as_of,
          plan: null,
        },
      },
    ];
    renderAgentsTab();

    expect(screen.getByText("执行记录已不可用")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /查看历史确认回执/ }),
    ).toHaveAttribute(
      "href",
      `/ai?session=${currentPlan.session_id}&generation=${currentPlan.generation_id}&proposal=${currentPlan.steps[1].proposal_id}`,
    );
    expect(mocks.exactQuery).not.toHaveBeenCalled();
  });

  it("opens current-facts restart from exact retained resources without calling frozen retry", () => {
    const retained = {
      ...pendingRun,
      status: "succeeded" as const,
      outputDeliveryStatus: "retained" as const,
      outputDeliveryErrorCode: "AGENT_RUN_IDENTITY_CHANGED",
    };
    mocks.runs.splice(0, mocks.runs.length, retained);
    mocks.exactRun = detailFromSummary(retained, null);
    renderAgentsTab();
    fireEvent.click(screen.getByText("任务甲"));
    fireEvent.click(screen.getByRole("button", { name: "按当前事实重新执行" }));
    expect(
      screen.getByRole("region", { name: "新执行预览入口" }),
    ).toHaveTextContent(`${retained.taskId} / ${retained.id}`);
    expect(mocks.retryRun).not.toHaveBeenCalled();
    expect(mocks.retryDelivery).not.toHaveBeenCalled();
  });
  it.each([
    ["AGENT_MODEL_TRUNCATED", "模型输出未完整结束，未登记产出。"],
    ["AGENT_MODEL_FILTERED", "模型拒绝或过滤了输出，未登记产出。"],
    [
      "AGENT_MODEL_RESPONSE_INVALID",
      "模型响应不符合完整文本交付协议，未登记产出。",
    ],
  ])(
    "displays only the fixed explanation for %s in exact Run detail",
    (errorCode, message) => {
      const failed = {
        ...pendingRun,
        status: "failed" as const,
        outputDeliveryStatus: "not_ready" as const,
        outputDeliveryErrorCode: null,
        errorCode,
      };
      mocks.runs.splice(0, mocks.runs.length, failed);
      mocks.exactRun = detailFromSummary(failed, null);
      renderAgentsTab();
      fireEvent.click(screen.getByText("任务甲"));
      expect(screen.getByText(message)).toBeVisible();
      expect(mocks.retryRun).not.toHaveBeenCalled();
      expect(mocks.retryDelivery).not.toHaveBeenCalled();
    },
  );
  it("mirrors effective completion and keeps replaced refusals out of the denominator", async () => {
    const plan = structuredClone(currentPlan);
    const old = plan.steps[1];
    Object.assign(old, {
      state: "rejected",
      satisfied: false,
      superseded_by: "replacement",
    });
    old.evidence!.status = "rejected";
    plan.steps.push({
      id: "replacement",
      title: "替代步骤",
      kind: "action",
      depends_on: ["draft"],
      replaces: "run",
      proposal_id: "018f0000-0000-7000-8000-000000000754",
      state: "recorded",
      ready: true,
      satisfied: true,
      evidence: {
        proposal_id: "018f0000-0000-7000-8000-000000000754",
        generation_id: plan.generation_id,
        action: "task.create",
        status: "confirmed",
        result_id: pendingRun.taskId,
      },
    });
    useAiChatStore.setState({ activeSessionId: plan.session_id });
    mocks.getWorkPlan.mockResolvedValue(plan);
    renderAgentsTab();
    expect(await screen.findByText("计划已完成")).toBeInTheDocument();
    expect(screen.getByText("已满足 2/2")).toBeInTheDocument();
    expect(screen.getByText("历史替代 1 步")).toBeInTheDocument();
    expect(screen.queryByText("执行中 1")).toBeNull();
  });

  it("does not count an out-of-order outcome and mirrors streaming analysis as running", async () => {
    const plan = structuredClone(currentPlan);
    plan.generation_status = "streaming";
    Object.assign(plan.steps[0], {
      report: "in_progress",
      state: "in_progress",
      satisfied: false,
    });
    Object.assign(plan.steps[1], {
      state: "submitted",
      satisfied: true,
      ready: false,
    });
    useAiChatStore.setState({ activeSessionId: plan.session_id });
    mocks.getWorkPlan.mockResolvedValue(plan);
    renderAgentsTab();
    expect(await screen.findByText("正在执行")).toBeInTheDocument();
    expect(screen.getByText("已满足 0/2")).toBeInTheDocument();
    expect(screen.queryByText("计划已完成")).toBeNull();
    expect(screen.queryByText("历史替代")).toBeNull();
  });
  it("preserves first-load errors and retries", () => {
    const errorQuery = {
      data: undefined,
      isError: true,
      isPending: false,
      refetch: vi.fn(),
    };
    mocks.workPlanInbox = errorQuery;
    mocks.agentInbox = { ...errorQuery, refetch: vi.fn() };

    renderAgentsTab();

    expect(screen.getByRole("alert")).toHaveTextContent("部分续办事项读取失败");
    expect(screen.getByText("数量暂不可用")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(errorQuery.refetch).toHaveBeenCalledOnce();
    expect(
      (mocks.agentInbox as typeof errorQuery).refetch,
    ).toHaveBeenCalledOnce();
  });

  it("does not show an attention section when both sources successfully return empty", () => {
    renderAgentsTab();
    expect(
      screen.queryByRole("region", { name: "跨会话续办队列" }),
    ).not.toBeInTheDocument();
  });

  it.each(["plan", "agent"] as const)(
    "hides stale %s attention after an error and retains successful items with an incomplete count",
    (failedSource) => {
      const planQuery = attentionQuery(
        [
          {
            session_id: "session-plan",
            session_title: "计划会话",
            plan_title: "计划待办",
            state: "awaiting_continuation",
          },
        ],
        { attention_total: 1 },
      );
      const agentQuery = attentionQuery(
        [
          {
            id: "proposal-approval",
            kind: "approval",
            action: "task.complete",
            session_id: "session-approval",
            session_title: "审批会话",
          },
        ],
        { approval_total: 1, review_total: 0 },
      );
      planQuery.isError = failedSource === "plan";
      agentQuery.isError = failedSource === "agent";
      mocks.workPlanInbox = planQuery;
      mocks.agentInbox = agentQuery;

      renderAgentsTab();

      expect(screen.getByText("1 项 · 数量不完整")).toBeInTheDocument();
      expect(screen.getByRole("alert")).toBeInTheDocument();
      expect(
        screen.queryByText(failedSource === "plan" ? "计划待办" : "完成任务"),
      ).not.toBeInTheDocument();
      expect(
        screen.getByText(failedSource === "plan" ? "完成任务" : "计划待办"),
      ).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "重试" }));
      expect(planQuery.refetch).toHaveBeenCalledTimes(
        failedSource === "plan" ? 1 : 0,
      );
      expect(agentQuery.refetch).toHaveBeenCalledTimes(
        failedSource === "agent" ? 1 : 0,
      );
    },
  );

  it("opens the full queue and expands the rail when attention exists beyond the loaded page", () => {
    const query = attentionQuery(
      [{ id: "failure-1", kind: "automation_failure" }],
      { approval_total: 1, review_total: 0 },
    );
    query.hasNextPage = true;
    mocks.agentInbox = query;
    useUiStore.setState({ agentRailCollapsed: true });

    renderAgentsTab();
    fireEvent.click(
      screen.getByRole("button", { name: "在智能体侧栏查看全部续办事项" }),
    );

    expect(screen.getByTestId("location")).toHaveTextContent("/ai");
    expect(useUiStore.getState()).toMatchObject({
      aiInboxOpen: true,
      agentRailCollapsed: false,
    });
    expect(useAiChatStore.getState().activeSessionId).toBe("");
  });

  it("prevents plan and approval session switches while an answer is streaming", () => {
    mocks.workPlanInbox = attentionQuery(
      [
        {
          session_id: "session-plan",
          session_title: "计划会话",
          plan_title: "计划待办",
          state: "awaiting_continuation",
        },
      ],
      { attention_total: 1 },
    );
    mocks.agentInbox = attentionQuery(
      [
        {
          id: "proposal-approval",
          kind: "approval",
          action: "task.complete",
          session_id: "session-approval",
          session_title: "审批会话",
        },
      ],
      { approval_total: 1, review_total: 0 },
    );
    useAiChatStore.setState({
      activeSessionId: "session-running",
      streaming: {
        sessionId: "session-running",
        text: "生成中",
        reasoning: "",
      },
    });

    renderAgentsTab();

    const planButton = screen.getByRole("button", { name: /计划待办/ });
    const approvalButton = screen.getByRole("button", { name: /完成任务/ });
    expect(planButton).toBeDisabled();
    expect(approvalButton).toBeDisabled();
    fireEvent.click(planButton);
    fireEvent.click(approvalButton);
    expect(useAiChatStore.getState().activeSessionId).toBe("session-running");
    expect(screen.getByTestId("location")).toHaveTextContent("/today");
  });

  it("mirrors active plan steps and returns to the exact original action without executing", async () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.getWorkPlan.mockResolvedValue(currentPlan);

    renderAgentsTab();

    const card = await screen.findByRole("region", {
      name: "当前主对话计划",
    });
    expect(await screen.findByText("整理发布素材")).toBeInTheDocument();
    expect(within(card).getByText("正在执行")).toBeInTheDocument();
    expect(screen.getByText("已满足 1/2")).toBeInTheDocument();
    expect(screen.getByText("执行中 1")).toBeInTheDocument();
    const steps = within(card).getByRole("list", { name: "计划步骤" });
    expect(within(steps).getAllByRole("listitem")).toHaveLength(2);
    expect(within(steps).getByText("整理初稿")).toBeInTheDocument();
    expect(within(steps).getByText("分析完成（模型自报）")).toBeInTheDocument();
    expect(within(steps).getByText("生成交付文件")).toBeInTheDocument();
    expect(within(steps).getByText("执行中")).toBeInTheDocument();
    expect(
      within(card).getByRole("link", { name: "查看原对话建议" }),
    ).toHaveAttribute(
      "href",
      `/ai?session=${currentPlan.session_id}&generation=${currentPlan.generation_id}&proposal=${currentPlan.steps[1].proposal_id}`,
    );
    expect(mocks.getWorkPlan.mock.calls[0]?.[0]).toBe(currentPlan.session_id);
    expect(
      screen.getByRole("link", { name: "在主对话查看并继续" }),
    ).toHaveAttribute("href", `/ai?session=${currentPlan.session_id}&plan=1`);
    expect(
      screen.getByText(
        /分析状态由模型报告.*继续、授权、确认和恢复仍在原对话中人工完成/,
      ),
    ).toBeInTheDocument();
    expect(mocks.retryDelivery).not.toHaveBeenCalled();
    expect(mocks.cancelRun).not.toHaveBeenCalled();
  });

  it("only links verified workbench routes and hides superseded steps", async () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.getWorkPlan.mockResolvedValue({
      ...currentPlan,
      steps: [
        { ...currentPlan.steps[0], superseded_by: "run" },
        {
          ...currentPlan.steps[1],
          evidence: {
            ...currentPlan.steps[1].evidence!,
            route: `/tasks/${pendingRun.taskId}`,
          },
        },
      ],
    });

    renderAgentsTab();

    const steps = await screen.findByRole("list", { name: "计划步骤" });
    expect(within(steps).queryByText("整理初稿")).not.toBeInTheDocument();
    expect(
      within(steps).getByRole("link", { name: "查看工作台记录" }),
    ).toHaveAttribute(
      "href",
      `/tasks/${pendingRun.taskId}?return_session=${currentPlan.session_id}`,
    );
  });

  it("does not turn an untrusted plan route into a navigation link", async () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.getWorkPlan.mockResolvedValue({
      ...currentPlan,
      steps: [
        currentPlan.steps[0],
        {
          ...currentPlan.steps[1],
          evidence: {
            ...currentPlan.steps[1].evidence!,
            route: "https://untrusted.example/collect",
          },
        },
      ],
    });

    renderAgentsTab();

    const steps = await screen.findByRole("list", { name: "计划步骤" });
    expect(
      within(steps).queryByRole("link", { name: "查看工作台记录" }),
    ).not.toBeInTheDocument();
    expect(
      within(steps).getByRole("link", { name: "查看原对话建议" }),
    ).toBeInTheDocument();
  });

  it("keeps the current plan visible while inspecting an Agent Run", async () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.getWorkPlan.mockResolvedValue(currentPlan);

    renderAgentsTab();

    expect(await screen.findByText("生成交付文件")).toBeInTheDocument();
    fireEvent.click(screen.getByText("任务甲"));
    expect(
      screen.getByRole("button", { name: "返回列表" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "计划步骤" })).toHaveTextContent(
      "生成交付文件",
    );
  });

  it("shows a retryable error rather than hiding a failed plan read", async () => {
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    mocks.getWorkPlan.mockRejectedValueOnce(new Error("unavailable"));
    mocks.getWorkPlan.mockResolvedValueOnce(currentPlan);

    renderAgentsTab();

    const card = await screen.findByRole("region", {
      name: "当前主对话计划",
    });
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "计划读取失败，当前状态无法核对。",
    );
    expect(within(card).queryByText("整理初稿")).not.toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "重试读取计划" }));
    expect(await screen.findByText("整理初稿")).toBeInTheDocument();
  });

  it("surfaces another saved session's pending plan and returns to that source chat", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000751";
    mocks.workPlanInbox = {
      data: {
        pages: [
          {
            data: [
              {
                session_id: sessionId,
                session_title: "另一段智能体对话",
                plan_title: "整理客户回访",
                state: "awaiting_continuation",
              },
            ],
            meta: {
              attention_total: 1,
              running_total: 0,
              needs_approval_total: 0,
              needs_recovery_total: 0,
            },
          },
        ],
      },
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    };
    mocks.agentInbox = {
      data: {
        pages: [
          {
            data: [
              {
                id: "018f0000-0000-7000-8000-000000000752",
                kind: "approval",
                action: "task.complete",
                session_id: sessionId,
                generation_id: "018f0000-0000-7000-8000-000000000755",
                session_title: "另一段智能体对话",
              },
            ],
            meta: { approval_total: 1, review_total: 0 },
          },
        ],
      },
      isPending: false,
      isError: false,
      refetch: vi.fn(),
    };
    renderAgentsTab();

    expect(screen.getByText("跨会话续办")).toBeInTheDocument();
    expect(screen.getByText("整理客户回访")).toBeInTheDocument();
    expect(screen.getByText("完成任务")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /整理客户回访/ }));

    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}&plan=1`,
    );
    fireEvent.click(screen.getByRole("button", { name: /完成任务/ }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      `/ai?session=${sessionId}&generation=018f0000-0000-7000-8000-000000000755&proposal=018f0000-0000-7000-8000-000000000752`,
    );
  });

  it("shows pending delivery as recovery work and exposes the exact retry", () => {
    renderAgentsTab();

    expect(screen.getByText("结果待登记")).toBeInTheDocument();
    fireEvent.click(screen.getByText("任务甲"));

    expect(
      screen.getByText(/结果已进入受保护的恢复暂存区/),
    ).toBeInTheDocument();
    expect(mocks.exactQuery).toHaveBeenCalledWith(
      pendingRun.id,
      pendingRun.taskId,
    );
    fireEvent.click(screen.getByRole("button", { name: "重试登记产出" }));
    expect(mocks.retryDelivery).toHaveBeenCalledWith({
      runId: pendingRun.id,
      taskId: pendingRun.taskId,
    });
  });

  it("hands off the exact pending delivery from the resources detail without mutating it", () => {
    renderAgentsTab();

    fireEvent.click(screen.getByText("任务甲"));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "Agent 执行需要处理",
      route: `/tasks/${pendingRun.taskId}?agent_run=${pendingRun.id}`,
      scopes: ["work", "outputs", "actions", "agent_execution"],
    });
    expect(pending?.prompt).toContain("任务「任务甲」");
    expect(pending?.prompt).toContain("模型已结束、结果待登记");
    expect(pending?.prompt).toContain("workspace_agent_runs");
    expect(pending?.prompt).toContain("workspace_outputs");
    expect(mocks.retryDelivery).not.toHaveBeenCalled();
    expect(mocks.cancelRun).not.toHaveBeenCalled();
  });

  it("allows cancelling an actively running Run but not a delivery recovery", async () => {
    const activeRun = {
      ...pendingRun,
      id: "018f0000-0000-7000-8000-000000000708",
      taskTitle: "运行中的任务",
      outputDeliveryStatus: "not_ready" as const,
      outputDeliveryErrorCode: null,
    };
    mocks.runs.splice(0, mocks.runs.length, activeRun);
    mocks.exactRun = detailFromSummary(activeRun, null);
    mocks.cancelRun.mockResolvedValue(mocks.exactRun);
    renderAgentsTab();

    fireEvent.click(screen.getByText("运行中的任务"));
    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    await waitFor(() =>
      expect(mocks.cancelRun).toHaveBeenCalledWith(activeRun.id),
    );
    expect(mocks.invalidateAgentRunDeliveryFacts).toHaveBeenCalledWith(
      expect.anything(),
      activeRun.taskId,
    );
    expect(
      screen.queryByRole("button", { name: "重试登记产出" }),
    ).not.toBeInTheDocument();
  });

  it("refreshes continuation projections when a retry creates a queued Run", async () => {
    const failedRun = {
      ...pendingRun,
      status: "failed" as const,
      outputDeliveryStatus: "not_ready" as const,
      outputDeliveryErrorCode: null,
      errorCode: "AGENT_RUN_FAILED",
      completedAt: "2026-09-18T10:01:00Z",
    };
    const retriedRun = detailFromSummary(
      {
        ...failedRun,
        id: "018f0000-0000-7000-8000-000000000709",
        parentRunId: failedRun.id,
        attempt: 2,
        status: "queued",
        errorCode: null,
        completedAt: null,
      },
      null,
    );
    mocks.runs.splice(0, mocks.runs.length, failedRun);
    mocks.exactRun = detailFromSummary(failedRun, null);
    mocks.retryRun.mockResolvedValue(retriedRun);
    renderAgentsTab();

    fireEvent.click(screen.getByText("任务甲"));
    fireEvent.click(screen.getByRole("button", { name: "重试" }));

    await waitFor(() =>
      expect(mocks.retryRun).toHaveBeenCalledWith(failedRun.id),
    );
    expect(mocks.invalidateAgentRunDeliveryFacts).toHaveBeenCalledWith(
      expect.anything(),
      failedRun.taskId,
    );
  });

  it.each([4, 5, 6])(
    "routes a failed v%s Run to explicit confirmation without a naked retry",
    (version) => {
      const failed = {
        ...pendingRun,
        status: "failed" as const,
        outputDeliveryStatus: "not_ready" as const,
      };
      mocks.runs.splice(0, mocks.runs.length, failed);
      mocks.exactRun = {
        ...detailFromSummary(failed, null),
        executionContractVersion: version,
      };
      renderAgentsTab();
      fireEvent.click(screen.getByText("任务甲"));
      expect(
        screen.queryByLabelText("精确返工重试预览"),
      ).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "重试" }));
      expect(screen.getByLabelText("精确返工重试预览")).toHaveTextContent(
        failed.id,
      );
      expect(screen.getByLabelText("精确返工重试预览")).toHaveTextContent(
        failed.taskId,
      );
      expect(mocks.retryRun).not.toHaveBeenCalled();
    },
  );
  it.each([
    [4, "succeeded"],
    [4, "pending"],
    [5, "succeeded"],
    [5, "pending"],
  ] as const)(
    "does not offer model retry for a v%s %s result",
    (version, kind) => {
      const summary = {
        ...pendingRun,
        status:
          kind === "succeeded" ? ("succeeded" as const) : ("failed" as const),
        outputDeliveryStatus:
          kind === "pending" ? ("pending" as const) : ("submitted" as const),
      };
      mocks.runs.splice(0, mocks.runs.length, summary);
      mocks.exactRun = {
        ...detailFromSummary(summary, null),
        executionContractVersion: version,
      };
      renderAgentsTab();
      fireEvent.click(screen.getByText("任务甲"));
      expect(
        screen.queryByRole("button", { name: "重试" }),
      ).not.toBeInTheDocument();
      expect(mocks.retryRun).not.toHaveBeenCalled();
    },
  );
  it("loads the exact Run before displaying a terminal result", () => {
    const resultText = "仅来自详情接口的正文";
    const submittedRun: AgentRunSummary = {
      ...pendingRun,
      status: "succeeded",
      resultBytes: new TextEncoder().encode(resultText).byteLength,
      outputDeliveryStatus: "submitted",
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000709",
      artifactId: "018f0000-0000-7000-8000-000000000710",
      completedAt: "2026-09-18T10:02:00Z",
    };
    mocks.runs.splice(0, mocks.runs.length, submittedRun);
    mocks.exactRun = detailFromSummary(submittedRun, resultText);
    useAiChatStore.setState({ activeSessionId: currentPlan.session_id });
    renderAgentsTab();

    fireEvent.click(screen.getByText("任务甲"));

    expect(mocks.exactQuery).toHaveBeenCalledWith(
      submittedRun.id,
      submittedRun.taskId,
    );
    expect(screen.getByText(resultText)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "查看待验收提交" }),
    ).toHaveAttribute(
      "href",
      `/tasks/${submittedRun.taskId}/submissions/${submittedRun.submissionId}?return_session=${currentPlan.session_id}`,
    );
  });

  it("stages an exact submitted file result in the right-side file review", () => {
    const resultText = "# Agent 交付\n";
    const submittedRun: AgentRunSummary = {
      ...pendingRun,
      status: "succeeded",
      resultBytes: new TextEncoder().encode(resultText).byteLength,
      outputDeliveryStatus: "submitted",
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000709",
      artifactId: "018f0000-0000-7000-8000-000000000710",
      completedAt: "2026-09-18T10:02:00Z",
    };
    mocks.runs.splice(0, mocks.runs.length, submittedRun);
    mocks.exactRun = {
      ...detailFromSummary(submittedRun, resultText),
      outputContract: {
        type: "file",
        name: "交付说明.md",
        mime: "text/markdown",
      },
    };
    useUiStore.setState({ rightOverviewCollapsed: true });
    renderAgentsTab();

    fireEvent.click(screen.getByText("任务甲"));
    fireEvent.click(
      screen.getByRole("button", { name: "用于项目文件：交付说明.md" }),
    );

    expect(useAgentRunFileApply.getState().candidate).toMatchObject({
      runId: submittedRun.id,
      taskId: submittedRun.taskId,
      taskTitle: submittedRun.taskTitle,
      submissionId: submittedRun.submissionId,
      artifactId: submittedRun.artifactId,
      attempt: submittedRun.attempt,
      index: 0,
      name: "交付说明.md",
      mime: "text/markdown",
      content: resultText,
    });
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      rightPanelTab: "files",
      rightPanelRequestMode: "activate",
    });
    expect(mocks.retryDelivery).not.toHaveBeenCalled();
    expect(mocks.cancelRun).not.toHaveBeenCalled();
  });

  it("keeps result actions gated while the exact Run is loading", () => {
    mocks.exactRun = undefined;
    mocks.exactPending = true;
    renderAgentsTab();

    fireEvent.click(screen.getByText("任务甲"));

    expect(screen.getByText("正在读取执行详情…")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "重试登记产出" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  });

  it("finds an older pending delivery even when newer terminal history fills the first page", () => {
    mocks.runs.splice(0, mocks.runs.length, {
      ...pendingRun,
      id: "018f0000-0000-7000-8000-000000000711",
      taskTitle: "较新的已完成任务",
      status: "succeeded",
      outputDeliveryStatus: "submitted",
      outputDeliveryErrorCode: null,
      submissionId: "018f0000-0000-7000-8000-000000000712",
      artifactId: "018f0000-0000-7000-8000-000000000713",
      completedAt: "2026-09-18T10:03:00Z",
    });
    mocks.allTotal = 51;
    mocks.pendingDeliveryTotal = 1;
    mocks.succeededTotal = 51;
    renderAgentsTab();

    fireEvent.click(screen.getByRole("button", { name: /有产出等待恢复登记/ }));

    expect(screen.getByText("任务甲")).toBeInTheDocument();
    expect(mocks.runQueryInputs).toContainEqual(
      expect.objectContaining({
        page: 1,
        pageSize: 20,
        outputDeliveryStatus: "pending",
      }),
    );
  });

  it("pages through the complete Agent Run ledger", () => {
    mocks.allTotal = 41;
    renderAgentsTab();

    expect(screen.getByText("第 1 / 3 页")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    expect(mocks.runQueryInputs).toContainEqual(
      expect.objectContaining({ page: 2, pageSize: 20 }),
    );
  });

  it("hands off a controlled Task artifact from the resources file detail", () => {
    renderManagedFilesTab();

    fireEvent.click(screen.getByText("验收说明.md"));
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    expect(useTaskArtifactQuery).toHaveBeenCalledWith(artifactFile.id);
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "任务产出",
      route: `/tasks/${artifactDetail.taskId}/submissions/${artifactDetail.submissionId}`,
      scopes: ["work", "outputs"],
    });
    expect(pending?.prompt).toContain(`artifact_id=${artifactDetail.id}`);
    expect(pending?.prompt).toContain("指定提交批次里的");
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("workspace_task_submissions");
    expect(pending?.prompt).toContain("不要验收、删除或修改产出");
    expect(mocks.retryDelivery).not.toHaveBeenCalled();
    expect(mocks.cancelRun).not.toHaveBeenCalled();
  });
});
