import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AiProviderSettings } from "./AiProviderSettings";
import { ApiError } from "../api/client";

afterEach(() => {
  cleanup();
  mockState.evaluations = [];
  mockState.evaluationsStatus = "success";
  mockState.evaluationDetail = null;
  mockState.evaluationReviews = [];
  mockState.evaluationReviewsStatus = "success";
  mockState.createEvaluation.mutateAsync.mockClear();
  mockState.createEvaluationReview.mutateAsync.mockClear();
  mockState.cancelEvaluation.mutateAsync.mockClear();
  mockState.deleteEvaluation.mutateAsync.mockClear();
});

beforeEach(() => {
  mockState.evaluationSummary = emptyEvaluationSummary();
  mockState.createEvaluationReview.error = null;
  mockState.createEvaluationReview.isPending = false;
});

const mockState = vi.hoisted(() => {
  const mutation = () => ({
    mutateAsync: vi.fn(async (_input?: unknown) => ({})),
    isPending: false,
    error: null as unknown,
  });
  return {
    providers: [] as unknown[],
    providersStatus: "success" as "pending" | "error" | "success",
    createProvider: mutation(),
    setKey: mutation(),
    checkHealth: mutation(),
    deleteProvider: mutation(),
    memories: [] as unknown[],
    memoriesStatus: "success" as "pending" | "error" | "success",
    proposals: [] as unknown[],
    proposalsStatus: "success" as "pending" | "error" | "success",
    createMemory: mutation(),
    deleteMemory: mutation(),
    rejectProposal: mutation(),
    evaluations: [] as unknown[],
    evaluationsStatus: "success" as "pending" | "error" | "success",
    evaluationDetail: null as null | Record<string, unknown>,
    evaluationSummary: null as null | Record<string, unknown>,
    evaluationReviews: [] as unknown[],
    evaluationReviewsStatus: "success" as "pending" | "error" | "success",
    createEvaluation: mutation(),
    createEvaluationReview: mutation(),
    cancelEvaluation: mutation(),
    deleteEvaluation: mutation(),
  };
});

vi.mock("../api/hooks", () => ({
  useAiProvidersQuery: () => ({
    data:
      mockState.providersStatus === "success" ? mockState.providers : undefined,
    isPending: mockState.providersStatus === "pending",
    isError: mockState.providersStatus === "error",
    error: mockState.providersStatus === "error" ? new Error("boom") : null,
    refetch: vi.fn(),
  }),
  useCreateAiProvider: () => mockState.createProvider,
  useSetAiProviderKey: () => mockState.setKey,
  useCheckAiProviderHealth: () => mockState.checkHealth,
  useDeleteAiProvider: () => mockState.deleteProvider,
  useAiMemoriesQuery: () => ({
    data:
      mockState.memoriesStatus === "success" ? mockState.memories : undefined,
    isPending: mockState.memoriesStatus === "pending",
    isError: mockState.memoriesStatus === "error",
    error: null,
    refetch: vi.fn(),
  }),
  useAiMemoryProposalsQuery: () => ({
    data:
      mockState.proposalsStatus === "success" ? mockState.proposals : undefined,
    isPending: mockState.proposalsStatus === "pending",
    isError: mockState.proposalsStatus === "error",
    error: null,
    refetch: vi.fn(),
  }),
  useCreateAiMemory: () => mockState.createMemory,
  useDeleteAiMemory: () => mockState.deleteMemory,
  useRejectAiMemoryProposal: () => mockState.rejectProposal,
  useAiEvaluationsQuery: () => ({
    data:
      mockState.evaluationsStatus === "success"
        ? {
            items: mockState.evaluations,
            meta: {
              page: 1,
              pageSize: 20,
              total: mockState.evaluations.length,
            },
          }
        : undefined,
    isPending: mockState.evaluationsStatus === "pending",
    isError: mockState.evaluationsStatus === "error",
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  }),
  useAiEvaluationSummaryQuery: () => ({
    data: mockState.evaluationSummary,
    isPending: false,
    isError: false,
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  }),
  useAiEvaluationReviewsQuery: () => ({
    data:
      mockState.evaluationReviewsStatus === "success"
        ? {
            items: mockState.evaluationReviews,
            meta: {
              page: 1,
              pageSize: 20,
              total: mockState.evaluationReviews.length,
            },
          }
        : undefined,
    isPending: mockState.evaluationReviewsStatus === "pending",
    isError: mockState.evaluationReviewsStatus === "error",
    isFetching: false,
    error: null,
    refetch: vi.fn(),
  }),
  useAiEvaluationQuery: (_id: string, enabled: boolean) => ({
    data: enabled ? mockState.evaluationDetail : undefined,
    isPending: enabled && mockState.evaluationDetail === null,
    isError: false,
    error: null,
    refetch: vi.fn(),
  }),
  useCreateAiEvaluation: () => mockState.createEvaluation,
  useCreateAiEvaluationReview: () => mockState.createEvaluationReview,
  useCancelAiEvaluation: () => mockState.cancelEvaluation,
  useDeleteAiEvaluation: () => mockState.deleteEvaluation,
}));

function readyProvider(overrides: Record<string, unknown> = {}) {
  return {
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
    version: 3,
    created_at: "2026-09-01T11:00:00Z",
    updated_at: "2026-09-01T12:00:00Z",
    ...overrides,
  };
}

function evaluationRun(overrides: Record<string, unknown> = {}) {
  return {
    id: "evaluation-1",
    providerId: "provider-1",
    providerNameSnapshot: "Ollama",
    providerModelSnapshot: "qwen3",
    providerProtocolSnapshot: "openai_chat",
    providerVersion: 3,
    datasetVersion: 1,
    suiteKey: "full",
    status: "succeeded",
    totalCases: 4,
    completedCases: 4,
    passedCases: 3,
    failedCases: 1,
    errorCases: 0,
    currentCaseId: null,
    cancelRequested: false,
    errorCode: null,
    startedAt: "2026-09-09T08:00:00Z",
    completedAt: "2026-09-09T08:00:05Z",
    createdAt: "2026-09-09T08:00:00Z",
    updatedAt: "2026-09-09T08:00:05Z",
    results: [],
    ...overrides,
  };
}

function emptyEvaluationSummary() {
  return {
    scope: { providerId: null },
    uncertainty: {
      method: "wilson_score",
      confidenceLevelBps: 9500,
      repeatedRunMinimum: 3,
    },
    readinessPolicy: {
      mode: "advisory",
      currentDatasetVersion: 2,
      requiredSuite: "full",
      minimumCompletedRuns: 3,
      minimumOverallLowerBps: 8000,
      minimumCategoryLowerBps: 6000,
      requiredCategories: [
        "grounded",
        "no_evidence",
        "prompt_injection",
        "conflicting_sources",
      ],
      criticalFailureCodes: [
        "CONTROL_BLOCK_LEAKED",
        "CITATION_NOT_ALLOWED",
        "FORBIDDEN_PHRASE_PRESENT",
      ],
    },
    statusCounts: {
      total: 0,
      queued: 0,
      running: 0,
      succeeded: 0,
      failed: 0,
      cancelled: 0,
    },
    groups: [],
    categories: [],
    failureCodes: [],
    trend: [],
    trendLimit: 12,
  };
}

function candidateEvaluationGroup() {
  return {
    providerId: "provider-1",
    providerNameSnapshot: "Ollama",
    providerModelSnapshot: "qwen3",
    datasetVersion: 3,
    suiteKey: "full",
    providerVersionMin: 3,
    providerVersionMax: 3,
    runCount: 3,
    fullyPassedRuns: 3,
    totalCases: 72,
    passedCases: 72,
    failedCases: 0,
    passRateBps: 10000,
    wilsonLowerBps: 9493,
    wilsonUpperBps: 10000,
    evidenceLevel: "repeated_runs",
    readinessStatus: "review_candidate",
    readinessReasons: [],
    lastCompletedAt: "2026-09-09T10:02:01Z",
  };
}

describe("AiProviderSettings", () => {
  it("shows the registration form when no provider exists and registers on submit", async () => {
    mockState.providers = [];
    render(<AiProviderSettings />);

    expect(screen.getByText("名称")).toBeTruthy();
    expect(screen.getByText("登记供应商")).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("例如 DeepSeek"), {
      target: { value: "DeepSeek" },
    });
    fireEvent.change(
      screen.getByPlaceholderText("https://api.example.com/v1"),
      { target: { value: "https://api.deepseek.com/v1" } },
    );
    fireEvent.change(screen.getByPlaceholderText("例如 deepseek-chat"), {
      target: { value: "deepseek-chat" },
    });
    fireEvent.click(screen.getByText("登记供应商"));

    await waitFor(() => {
      expect(mockState.createProvider.mutateAsync).toHaveBeenCalledWith({
        name: "DeepSeek",
        kind: "remote",
        protocol: "openai_chat",
        base_url: "https://api.deepseek.com/v1",
        model: "deepseek-chat",
      });
    });
  });

  it("registers a local provider without a key and locks the protocol", async () => {
    mockState.providers = [];
    render(<AiProviderSettings />);

    fireEvent.change(screen.getByPlaceholderText("例如 DeepSeek"), {
      target: { value: "Ollama" },
    });
    fireEvent.change(screen.getByDisplayValue("远程 API（API key）"), {
      target: { value: "local" },
    });
    expect(screen.getByText(/本地部署使用 OpenAI 兼容端点/)).toBeTruthy();
    expect(
      screen.getByPlaceholderText("http://127.0.0.1:11434/v1"),
    ).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("http://127.0.0.1:11434/v1"), {
      target: { value: "http://127.0.0.1:11434/v1" },
    });
    fireEvent.change(screen.getByPlaceholderText("例如 qwen3"), {
      target: { value: "qwen3" },
    });
    fireEvent.click(screen.getByText("登记供应商"));

    await waitFor(() => {
      expect(mockState.createProvider.mutateAsync).toHaveBeenCalledWith({
        name: "Ollama",
        kind: "local",
        protocol: "openai_chat",
        base_url: "http://127.0.0.1:11434/v1",
        model: "qwen3",
      });
    });
  });

  it("renders a local provider card without any key input", () => {
    mockState.providers = [
      readyProvider({
        kind: "local",
        has_key: false,
        name: "Ollama",
        model: "qwen3",
      }),
    ];
    render(<AiProviderSettings />);

    expect(screen.getByText("Ollama")).toBeTruthy();
    expect(
      screen.getByText("本地部署 · OpenAI Chat Completions 兼容 · qwen3"),
    ).toBeTruthy();
    expect(screen.getByText("本地部署供应商，无需 API 密钥。")).toBeTruthy();
    expect(screen.queryByPlaceholderText("已保存（输入可更换）")).toBeNull();
    expect(screen.getByText("测试连接")).toBeTruthy();
    expect(screen.getByText("快速质量检查")).toBeTruthy();
    expect(screen.getByText("完整质量评测")).toBeTruthy();
    expect(screen.getByLabelText("本地质量专题套件")).toHaveValue("grounded");
    expect(screen.getByText("运行专题检查")).toBeTruthy();
  });

  it("starts an explicit evaluation only for the ready local provider", async () => {
    mockState.providers = [
      readyProvider({
        kind: "local",
        has_key: false,
        name: "Ollama",
        model: "qwen3",
      }),
    ];
    mockState.createEvaluation.mutateAsync.mockResolvedValue(
      evaluationRun({
        status: "queued",
        completedCases: 0,
        passedCases: 0,
        failedCases: 0,
      }),
    );
    render(<AiProviderSettings />);

    fireEvent.click(screen.getByText("快速质量检查"));

    await waitFor(() => {
      expect(mockState.createEvaluation.mutateAsync).toHaveBeenCalledWith({
        providerId: "provider-1",
        providerVersion: 3,
        suiteKey: "smoke",
      });
    });
    expect(screen.getByText(/快速质量检查已进入队列/)).toBeTruthy();

    fireEvent.change(screen.getByLabelText("本地质量专题套件"), {
      target: { value: "prompt_injection" },
    });
    fireEvent.click(screen.getByText("运行专题检查"));

    await waitFor(() => {
      expect(mockState.createEvaluation.mutateAsync).toHaveBeenLastCalledWith({
        providerId: "provider-1",
        providerVersion: 3,
        suiteKey: "prompt_injection",
      });
    });
    expect(screen.getByText(/引用注入防护专题检查已进入队列/)).toBeTruthy();
  });

  it("shows content-free evaluation history and expands stable failure codes", () => {
    mockState.providers = [
      readyProvider({
        kind: "local",
        has_key: false,
        name: "Ollama",
        model: "qwen3",
      }),
    ];
    mockState.evaluations = [evaluationRun()];
    mockState.evaluationSummary = {
      scope: { providerId: null },
      uncertainty: {
        method: "wilson_score",
        confidenceLevelBps: 9500,
        repeatedRunMinimum: 3,
      },
      readinessPolicy: {
        mode: "advisory",
        currentDatasetVersion: 1,
        requiredSuite: "full",
        minimumCompletedRuns: 3,
        minimumOverallLowerBps: 8000,
        minimumCategoryLowerBps: 6000,
        requiredCategories: [
          "grounded",
          "no_evidence",
          "prompt_injection",
          "conflicting_sources",
        ],
        criticalFailureCodes: [
          "CONTROL_BLOCK_LEAKED",
          "CITATION_NOT_ALLOWED",
          "FORBIDDEN_PHRASE_PRESENT",
        ],
      },
      statusCounts: {
        total: 5,
        queued: 0,
        running: 0,
        succeeded: 3,
        failed: 1,
        cancelled: 1,
      },
      groups: [
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          providerVersionMin: 3,
          providerVersionMax: 3,
          runCount: 3,
          fullyPassedRuns: 1,
          totalCases: 12,
          passedCases: 9,
          failedCases: 3,
          passRateBps: 7500,
          wilsonLowerBps: 4677,
          wilsonUpperBps: 9111,
          evidenceLevel: "repeated_runs",
          readinessStatus: "needs_attention",
          readinessReasons: [
            "OVERALL_LOWER_BOUND_LOW",
            "CATEGORY_LOWER_BOUND_LOW",
            "CRITICAL_FAILURE_PRESENT",
          ],
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
      ],
      categories: [
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          category: "grounded",
          totalCases: 3,
          passedCases: 3,
          failedCases: 0,
          passRateBps: 10000,
          wilsonLowerBps: 4385,
          wilsonUpperBps: 10000,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          category: "no_evidence",
          totalCases: 3,
          passedCases: 3,
          failedCases: 0,
          passRateBps: 10000,
          wilsonLowerBps: 4385,
          wilsonUpperBps: 10000,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          category: "prompt_injection",
          totalCases: 3,
          passedCases: 2,
          failedCases: 1,
          passRateBps: 6667,
          wilsonLowerBps: 2077,
          wilsonUpperBps: 9385,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          category: "conflicting_sources",
          totalCases: 3,
          passedCases: 1,
          failedCases: 2,
          passRateBps: 3333,
          wilsonLowerBps: 615,
          wilsonUpperBps: 7923,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
      ],
      failureCodes: [
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          failureCode: "REQUIRED_PHRASE_MISSING",
          affectedCases: 2,
          occurrences: 3,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          failureCode: "CITATION_SET_MISMATCH",
          affectedCases: 1,
          occurrences: 1,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
        {
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          failureCode: "FORBIDDEN_PHRASE_PRESENT",
          affectedCases: 1,
          occurrences: 1,
          lastCompletedAt: "2026-09-09T08:00:05Z",
        },
      ],
      trend: [
        {
          runId: "evaluation-trend-1",
          providerId: "provider-1",
          providerNameSnapshot: "Ollama",
          providerModelSnapshot: "qwen3",
          datasetVersion: 1,
          suiteKey: "full",
          totalCases: 4,
          passedCases: 3,
          failedCases: 1,
          completedAt: "2026-09-09T08:00:05Z",
        },
      ],
      trendLimit: 12,
    };
    mockState.evaluationDetail = evaluationRun({
      results: [
        {
          id: "result-1",
          runId: "evaluation-1",
          sequence: 1,
          caseId: "zh_invoice_grounded",
          language: "zh-CN",
          category: "grounded",
          status: "passed",
          failureCodes: [],
          citationStatus: "validated",
          citationCount: 1,
          durationMs: 800,
          inputBytes: 512,
          outputBytes: 128,
          inputTokens: 64,
          outputTokens: 16,
          tokenSource: "provider",
          errorCode: null,
          createdAt: "2026-09-09T08:00:01Z",
        },
        {
          id: "result-2",
          runId: "evaluation-1",
          sequence: 2,
          caseId: "zh_no_evidence_refund",
          language: "zh-CN",
          category: "no_evidence",
          status: "failed",
          failureCodes: ["REQUIRED_PHRASE_MISSING"],
          citationStatus: "no_evidence",
          citationCount: 0,
          durationMs: 900,
          inputBytes: 500,
          outputBytes: 80,
          inputTokens: null,
          outputTokens: null,
          tokenSource: null,
          errorCode: null,
          createdAt: "2026-09-09T08:00:02Z",
        },
      ],
    });
    render(<AiProviderSettings />);

    fireEvent.click(screen.getByRole("button", { name: /Ollama · qwen3/ }));

    expect(screen.getAllByText(/有依据回答/)).toHaveLength(3);
    expect(screen.getAllByText(/资料不足/)).toHaveLength(3);
    expect(screen.getAllByText("缺少必要事实")).toHaveLength(2);
    expect(screen.getByText(/64 → 16 tokens/)).toBeTruthy();
    expect(screen.getByText(/token unknown/)).toBeTruthy();
    expect(screen.getAllByText("75%")).toHaveLength(2);
    expect(screen.getByText("完整运行 3")).toBeTruthy();
    expect(screen.getByText("运行失败 1")).toBeTruthy();
    expect(screen.getByText("已取消 1")).toBeTruthy();
    expect(screen.getByText("按类别")).toBeTruthy();
    expect(screen.getAllByText("3/3")).toHaveLength(2);
    expect(screen.getByText("2/3")).toBeTruthy();
    expect(screen.getByText("1/3")).toBeTruthy();
    expect(screen.getByText("失败原因")).toBeTruthy();
    expect(screen.getByText("影响 2 case · 出现 3 次")).toBeTruthy();
    expect(screen.getByText("95% 46.77%–91.11% · 重复观察")).toBeTruthy();
    expect(screen.getAllByText("43.85%–100%")).toHaveLength(2);
    expect(screen.getByText(/不代表发布许可/)).toBeTruthy();
    expect(
      screen.getByText(
        "需关注 · 总体下界低于 80%、存在低于 60% 的类别、存在严重失败码",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("private model answer")).toBeNull();
    fireEvent.click(screen.getByText("删除评测历史"));
    fireEvent.click(screen.getByText("确认删除"));
    expect(mockState.deleteEvaluation.mutateAsync).toHaveBeenCalledWith({
      id: "evaluation-1",
    });
  });

  it("records a reasoned human decision and renders immutable local audit history", async () => {
    mockState.providers = [
      readyProvider({
        kind: "local",
        has_key: false,
        name: "Ollama",
        model: "qwen3",
      }),
    ];
    const group = candidateEvaluationGroup();
    mockState.evaluationSummary = {
      ...emptyEvaluationSummary(),
      readinessPolicy: {
        ...emptyEvaluationSummary().readinessPolicy,
        currentDatasetVersion: 3,
      },
      statusCounts: {
        total: 3,
        queued: 0,
        running: 0,
        succeeded: 3,
        failed: 0,
        cancelled: 0,
      },
      groups: [group],
    };
    mockState.evaluationReviews = [
      {
        id: "review-1",
        providerIdSnapshot: "provider-1",
        providerNameSnapshot: "Ollama",
        providerModelSnapshot: "qwen3",
        datasetVersion: 3,
        suiteKey: "full",
        providerVersionMin: 3,
        providerVersionMax: 3,
        groupLastCompletedAt: "2026-09-09T10:02:01Z",
        runCount: 3,
        totalCases: 72,
        passedCases: 72,
        failedCases: 0,
        overallWilsonLowerBps: 9493,
        minimumCategory: "conflicting_sources",
        minimumCategoryWilsonLowerBps: 8242,
        readinessStatus: "review_candidate",
        readinessReasons: [],
        criticalFailureCodes: [],
        decision: "accepted_for_local_use",
        reason: "三次完整评测稳定通过，仅批准本机试用。",
        reviewedByActorId: "00000000-0000-5000-8000-000000000001",
        reviewedByActorNameSnapshot: "我",
        createdAt: "2026-09-09T10:05:00Z",
      },
    ];

    render(<AiProviderSettings />);

    expect(screen.getByText("人工决定审计")).toBeTruthy();
    expect(screen.getByText(/仅审计，不改变模型、聊天/)).toBeTruthy();
    expect(
      screen.getByText("三次完整评测稳定通过，仅批准本机试用。"),
    ).toBeTruthy();
    expect(screen.queryByText("删除人工决定")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "记录人工决定" }));
    expect(screen.getByLabelText("人工决定")).toHaveValue(
      "accepted_for_local_use",
    );
    fireEvent.change(screen.getByLabelText("人工决定理由"), {
      target: { value: "  当前证据允许在本机灰度使用。  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存不可变记录" }));

    await waitFor(() => {
      expect(mockState.createEvaluationReview.mutateAsync).toHaveBeenCalledWith(
        {
          group,
          decision: "accepted_for_local_use",
          reason: "当前证据允许在本机灰度使用。",
        },
      );
    });
  });

  it("explains a stale evidence rejection without implying state changes", () => {
    mockState.providers = [readyProvider()];
    const group = candidateEvaluationGroup();
    mockState.evaluationSummary = {
      ...emptyEvaluationSummary(),
      groups: [group],
    };
    mockState.createEvaluationReview.error = new ApiError("stale", {
      code: "AI_EVALUATION_REVIEW_STALE",
    });

    render(<AiProviderSettings />);
    fireEvent.click(screen.getByRole("button", { name: "记录人工决定" }));

    expect(screen.getByText(/评测证据已变化/)).toBeTruthy();
  });

  it("lists confirmed memories and deletes one", async () => {
    mockState.providers = [readyProvider()];
    mockState.memories = [
      {
        id: "memory-1",
        content: "回答保持简洁",
        created_at: "2026-09-03T12:00:00Z",
        updated_at: "2026-09-03T12:00:00Z",
      },
    ];
    render(<AiProviderSettings />);

    expect(screen.getByText("长期记忆")).toBeTruthy();
    expect(screen.getByText("回答保持简洁")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "删除记忆：回答保持简洁" }),
    );

    await waitFor(() => {
      expect(mockState.deleteMemory.mutateAsync).toHaveBeenCalledWith({
        id: "memory-1",
      });
    });
  });

  it("shows the empty memory state", () => {
    mockState.providers = [readyProvider()];
    mockState.memories = [];
    render(<AiProviderSettings />);
    expect(screen.getByText("暂无已确认的记忆。")).toBeTruthy();
  });

  it("lists pending memory proposals for explicit confirmation or rejection", async () => {
    mockState.providers = [readyProvider()];
    mockState.memories = [];
    mockState.proposals = [
      {
        id: "018f0000-0000-7000-8000-000000005771",
        session_id: "session-1",
        session_title: "发布计划",
        content: "优先完成离线版本",
        tags: ["decision"],
        created_at: "2026-09-08T12:00:00Z",
      },
      {
        id: "018f0000-0000-7000-8000-000000005772",
        session_id: "session-2",
        session_title: "客户沟通",
        content: "回复保持简洁",
        tags: [],
        created_at: "2026-09-08T11:00:00Z",
      },
    ];
    render(<AiProviderSettings />);

    expect(screen.getByText("待确认建议")).toBeTruthy();
    expect(screen.getByText(/来自会话「发布计划」 · decision/)).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: "记住" })[0]);
    fireEvent.click(screen.getAllByRole("button", { name: "忽略" })[1]);

    await waitFor(() => {
      expect(mockState.createMemory.mutateAsync).toHaveBeenCalledWith({
        content: "优先完成离线版本",
        proposal_id: "018f0000-0000-7000-8000-000000005771",
      });
      expect(mockState.rejectProposal.mutateAsync).toHaveBeenCalledWith({
        id: "018f0000-0000-7000-8000-000000005772",
      });
    });
    mockState.proposals = [];
  });

  it("maps the AI_KEY_NOT_ALLOWED error to a safe explanation", () => {
    mockState.providers = [
      readyProvider({ kind: "local", has_key: false, name: "Ollama" }),
    ];
    mockState.setKey.error = new ApiError("not allowed", {
      code: "AI_KEY_NOT_ALLOWED",
    });
    render(<AiProviderSettings />);

    expect(
      screen.getByText(/本地部署供应商不需要也不保存 API 密钥/),
    ).toBeTruthy();
  });

  it("explains why a provider with conversation history cannot be deleted", () => {
    mockState.providers = [readyProvider()];
    mockState.setKey.error = null;
    mockState.checkHealth.error = null;
    mockState.deleteProvider.error = new ApiError("used", {
      code: "AI_PROVIDER_HAS_SESSIONS",
    });
    render(<AiProviderSettings />);
    expect(screen.getByText(/请先在 AI 助手中删除相关会话/)).toBeTruthy();
    mockState.deleteProvider.error = null;
  });

  it("renders a ready provider with status and key saving", async () => {
    mockState.providers = [readyProvider()];
    render(<AiProviderSettings />);

    expect(screen.getByText("DeepSeek")).toBeTruthy();
    expect(screen.getByText("已就绪")).toBeTruthy();
    expect(screen.getByPlaceholderText("已保存（输入可更换）")).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText("已保存（输入可更换）"), {
      target: { value: "sk-new" },
    });
    fireEvent.click(screen.getByText("保存密钥"));

    await waitFor(() => {
      expect(mockState.setKey.mutateAsync).toHaveBeenCalledWith({
        id: "provider-1",
        apiKey: "sk-new",
        expectedVersion: 3,
      });
    });
  });

  it("maps the unavailable key store error to a safe explanation", () => {
    mockState.providers = [readyProvider()];
    mockState.setKey.mutateAsync = vi.fn(async () => {
      throw new Error("unused");
    });
    mockState.setKey.error = new ApiError("store unavailable", {
      code: "AI_KEY_STORE_UNAVAILABLE",
    });
    render(<AiProviderSettings />);

    expect(
      screen.getByText(/操作系统安全存储不可用，无法保存 API 密钥/),
    ).toBeTruthy();
  });

  it("shows the human-readable health failure reason", () => {
    mockState.providers = [
      readyProvider({
        status: "unavailable",
        health_status: "unhealthy",
        health_error_code: "AI_KEY_INVALID",
      }),
    ];
    render(<AiProviderSettings />);
    expect(screen.getByText("未就绪")).toBeTruthy();
    expect(screen.getByText("API 密钥被拒绝（401/403）")).toBeTruthy();
  });

  it("renders every registered provider card and opens the add form on demand", () => {
    mockState.providers = [
      readyProvider(),
      readyProvider({
        id: "provider-2",
        name: "Kimi",
        model: "kimi-k2",
        base_url: "https://api.moonshot.cn/v1",
      }),
    ];
    render(<AiProviderSettings />);

    const deepSeekHeadings = screen
      .getAllByText("DeepSeek")
      .filter((node) => node.tagName === "H4");
    expect(deepSeekHeadings.length).toBe(1);
    expect(screen.getByText("Kimi")).toBeTruthy();
    expect(screen.queryByText("名称")).toBeNull();

    fireEvent.click(screen.getByText("添加供应商"));
    expect(screen.getByText("名称")).toBeTruthy();
  });
});
