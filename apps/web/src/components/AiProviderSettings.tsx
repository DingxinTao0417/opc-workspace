import {
  AlertCircle,
  Bot,
  CheckCircle2,
  ChevronDown,
  FlaskConical,
  KeyRound,
  LoaderCircle,
  PlugZap,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { ApiError } from "../api/client";
import {
  useAiMemoriesQuery,
  useAiMemoryProposalsQuery,
  useAiProvidersQuery,
  useAiEvaluationQuery,
  useAiEvaluationReviewsQuery,
  useAiEvaluationSummaryQuery,
  useAiEvaluationsQuery,
  useCancelAiEvaluation,
  useCheckAiProviderHealth,
  useCreateAiEvaluation,
  useCreateAiEvaluationReview,
  useCreateAiProvider,
  useCreateAiMemory,
  useDeleteAiMemory,
  useDeleteAiEvaluation,
  useDeleteAiProvider,
  useRejectAiMemoryProposal,
  useSetAiProviderKey,
} from "../api/hooks";
import type {
  AiEvaluationQualityGroup,
  AiEvaluationReviewDecision,
  AiEvaluationRun,
  AiEvaluationSummary,
  AiEvaluationSuiteKey,
  AiProvider,
  AiProviderKind,
  AiProviderProtocol,
} from "../types/models";

function aiProviderError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.code === "AI_KEY_STORE_UNAVAILABLE") {
      return "操作系统安全存储不可用，无法保存 API 密钥；已拒绝保存，未写入任何明文。";
    }
    if (error.code === "AI_KEY_NOT_ALLOWED") {
      return "本地部署供应商不需要也不保存 API 密钥。";
    }
    if (error.code === "VERSION_CONFLICT") {
      return "供应商配置已变化，已刷新最新状态，请重试。";
    }
    if (error.code === "AI_PROVIDER_HAS_SESSIONS") {
      return "该供应商仍有会话记录。请先在 AI 助手中删除相关会话，再删除供应商。";
    }
    if (error.code === "AI_PROVIDER_HAS_EVALUATIONS") {
      return "该供应商仍有本地质量评测历史。请先在下方删除对应评测记录。";
    }
    if (error.code === "AI_EVALUATION_ALREADY_ACTIVE") {
      return "该本地供应商已有评测正在排队或运行。";
    }
    if (error.code === "AI_EVALUATION_PROVIDER_NOT_READY") {
      return "请先测试本地供应商连接并确认其已就绪。";
    }
    if (error.code === "AI_EVALUATION_REVIEW_STALE") {
      return "评测证据已变化，已刷新最新结果；请核对后重新记录。";
    }
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return fallback;
}

const protocolLabels: Record<AiProviderProtocol, string> = {
  openai_chat: "OpenAI Chat Completions 兼容",
  anthropic_messages: "Anthropic Messages 兼容",
};

const healthErrorLabels: Record<string, string> = {
  AI_KEY_UNAVAILABLE: "尚未保存 API 密钥",
  AI_KEY_INVALID: "API 密钥被拒绝（401/403）",
  AI_ENDPOINT_UNREACHABLE: "端点无法访问",
  AI_PROVIDER_ERROR: "端点返回错误",
};

const aiEvaluationEvidenceLabels = {
  single_run: "单次观察",
  limited_runs: "有限重复",
  repeated_runs: "重复观察",
} as const;

const aiEvaluationSuiteLabels: Record<AiEvaluationSuiteKey, string> = {
  smoke: "快速",
  full: "完整",
  grounded: "有依据回答",
  no_evidence: "资料不足",
  prompt_injection: "引用注入防护",
  conflicting_sources: "冲突来源",
} as const;

type AiEvaluationTopicSuiteKey = Exclude<
  AiEvaluationSuiteKey,
  "smoke" | "full"
>;

const aiEvaluationTopicSuiteKeys = [
  "grounded",
  "no_evidence",
  "prompt_injection",
  "conflicting_sources",
] as const satisfies readonly AiEvaluationTopicSuiteKey[];

function formatBasisPointPercent(value: number) {
  return `${(value / 100)
    .toFixed(2)
    .replace(/\.00$/, "")
    .replace(/(\.\d)0$/, "$1")}%`;
}

function formatWilsonInterval(lower: number, upper: number) {
  return `${formatBasisPointPercent(lower)}–${formatBasisPointPercent(upper)}`;
}

const aiEvaluationReadinessLabels = {
  insufficient_evidence: "证据不足",
  needs_attention: "需关注",
  review_candidate: "可进入人工评审",
} as const;

const aiEvaluationReviewDecisionLabels: Record<
  AiEvaluationReviewDecision,
  string
> = {
  accepted_for_local_use: "接受本机试用",
  needs_more_evidence: "需要更多证据",
  rejected: "拒绝使用",
};

function aiEvaluationGroupKey(group: AiEvaluationQualityGroup) {
  return `${group.providerId}:${group.providerNameSnapshot}:${group.providerModelSnapshot}:${group.datasetVersion}:${group.suiteKey}`;
}

function readinessReasonLabel(
  reason: AiEvaluationQualityGroup["readinessReasons"][number],
  policy: AiEvaluationSummary["readinessPolicy"],
) {
  switch (reason) {
    case "OUTDATED_DATASET":
      return `不是当前 dataset v${policy.currentDatasetVersion}`;
    case "SUITE_NOT_ELIGIBLE":
      return "诊断套件不形成候选资格";
    case "RUN_COUNT_LOW":
      return `少于 ${policy.minimumCompletedRuns} 次完整运行`;
    case "PROVIDER_VERSION_MIXED":
      return "包含多个 Provider 配置版本";
    case "OVERALL_LOWER_BOUND_LOW":
      return `总体下界低于 ${formatBasisPointPercent(policy.minimumOverallLowerBps)}`;
    case "CATEGORY_LOWER_BOUND_LOW":
      return `存在低于 ${formatBasisPointPercent(policy.minimumCategoryLowerBps)} 的类别`;
    case "CRITICAL_FAILURE_PRESENT":
      return "存在严重失败码";
  }
}

export function AiProviderSettings() {
  const providers = useAiProvidersQuery();
  const evaluations = useAiEvaluationsQuery();
  const evaluationSummary = useAiEvaluationSummaryQuery();
  const evaluationReviews = useAiEvaluationReviewsQuery();
  const [formOpen, setFormOpen] = useState(false);

  if (providers.isPending) {
    return (
      <div aria-live="polite" className="settings-state" role="status">
        <LoaderCircle className="animate-spin" size={16} />
        正在读取 AI 供应商…
      </div>
    );
  }
  if (providers.isError || !providers.data) {
    return (
      <div className="settings-state settings-state-error" role="alert">
        <AlertCircle size={16} />
        <div>
          <strong>无法读取 AI 供应商设置</strong>
          <span>{aiProviderError(providers.error, "读取失败")}</span>
        </div>
        <button
          className="button button-secondary"
          onClick={() => void providers.refetch()}
          type="button"
        >
          <RefreshCw size={14} />
          重试
        </button>
      </div>
    );
  }

  const providerList = providers.data;

  return (
    <div className="ai-provider-settings">
      <header className="settings-content-header">
        <h3>AI 助手</h3>
        <p>
          可登记多个大模型供应商：远程 API（API key 模式）或本地部署 （Ollama /
          LM Studio 等 OpenAI 兼容端点，无需密钥），聊天页可随时切换。
          密钥只保存在操作系统安全存储，不进入数据库、日志或导出。
        </p>
      </header>

      {providerList.map((provider) => (
        <AiProviderCard
          evaluationActive={Boolean(
            evaluations.data?.items.some(
              (run) =>
                run.providerId === provider.id &&
                (run.status === "queued" || run.status === "running"),
            ),
          )}
          key={provider.id}
          provider={provider}
        />
      ))}

      {formOpen || providerList.length === 0 ? (
        <AiProviderForm onDone={() => setFormOpen(false)} />
      ) : (
        <div className="ai-provider-footer">
          <button
            className="button button-secondary"
            onClick={() => setFormOpen(true)}
            type="button"
          >
            <Plus size={14} />
            添加供应商
          </button>
        </div>
      )}
      <AiEvaluationHistory
        query={evaluations}
        reviews={evaluationReviews}
        summary={evaluationSummary}
      />
      <AiMemoryList />
    </div>
  );
}

// AiMemoryList manages the user-confirmed long-term preferences the agent
// reuses across sessions (ADR-006).
function AiMemoryList() {
  const memories = useAiMemoriesQuery();
  const proposals = useAiMemoryProposalsQuery();
  const createMemory = useCreateAiMemory();
  const deleteMemory = useDeleteAiMemory();
  const rejectProposal = useRejectAiMemoryProposal();
  return (
    <section className="ai-memory-settings">
      <h4>长期记忆</h4>
      <p>你在对话中确认记住的偏好；会注入后续会话，可随时删除。</p>
      {proposals.isPending ? (
        <div className="settings-state" role="status">
          <LoaderCircle className="animate-spin" size={16} />
          正在读取待确认建议…
        </div>
      ) : proposals.isError || !proposals.data ? (
        <div className="settings-state settings-state-error" role="alert">
          <AlertCircle size={16} />
          无法读取待确认记忆建议
        </div>
      ) : proposals.data.length > 0 ? (
        <>
          <h5>待确认建议</h5>
          <ul className="ai-memory-rows ai-memory-proposal-rows">
            {proposals.data.map((proposal) => (
              <li key={proposal.id}>
                <span className="ai-memory-row-copy">
                  <strong>{proposal.content}</strong>
                  <small>
                    来自会话「{proposal.session_title}」
                    {proposal.tags.length > 0
                      ? ` · ${proposal.tags.join(" / ")}`
                      : ""}
                  </small>
                </span>
                <span className="ai-memory-row-actions">
                  <button
                    className="button button-secondary"
                    disabled={
                      createMemory.isPending || rejectProposal.isPending
                    }
                    onClick={() =>
                      void createMemory
                        .mutateAsync({
                          content: proposal.content,
                          proposal_id: proposal.id,
                        })
                        .catch(() => {
                          // mutation state renders a safe error below
                        })
                    }
                    type="button"
                  >
                    记住
                  </button>
                  <button
                    className="button button-quiet"
                    disabled={
                      createMemory.isPending || rejectProposal.isPending
                    }
                    onClick={() =>
                      void rejectProposal
                        .mutateAsync({ id: proposal.id })
                        .catch(() => {
                          // mutation state renders a safe error below
                        })
                    }
                    type="button"
                  >
                    忽略
                  </button>
                </span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {createMemory.error || rejectProposal.error ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          记忆建议处理失败，请刷新后重试
        </div>
      ) : null}
      <h5>已确认</h5>
      {memories.isPending ? (
        <div className="settings-state" role="status">
          <LoaderCircle className="animate-spin" size={16} />
          正在读取记忆…
        </div>
      ) : memories.isError || !memories.data ? (
        <div className="settings-state settings-state-error" role="alert">
          <AlertCircle size={16} />
          无法读取长期记忆
        </div>
      ) : memories.data.length === 0 ? (
        <div className="settings-state">暂无已确认的记忆。</div>
      ) : (
        <ul className="ai-memory-rows">
          {memories.data.map((memory) => (
            <li key={memory.id}>
              <span>{memory.content}</span>
              <button
                aria-label={`删除记忆：${memory.content}`}
                className="button button-quiet"
                disabled={deleteMemory.isPending}
                onClick={() =>
                  void deleteMemory.mutateAsync({ id: memory.id }).catch(() => {
                    // mutation state renders a safe error below
                  })
                }
                type="button"
              >
                <Trash2 size={13} />
                删除
              </button>
            </li>
          ))}
        </ul>
      )}
      {deleteMemory.error ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          删除长期记忆失败，请刷新后重试
        </div>
      ) : null}
    </section>
  );
}

function AiProviderCard({
  provider,
  evaluationActive,
}: {
  provider: AiProvider;
  evaluationActive: boolean;
}) {
  const setKey = useSetAiProviderKey();
  const checkHealth = useCheckAiProviderHealth();
  const deleteProvider = useDeleteAiProvider();
  const createEvaluation = useCreateAiEvaluation();
  const [apiKey, setApiKey] = useState("");
  const [topicSuite, setTopicSuite] =
    useState<AiEvaluationTopicSuiteKey>("grounded");
  const [feedback, setFeedback] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const actionError =
    setKey.error ??
    checkHealth.error ??
    deleteProvider.error ??
    createEvaluation.error;

  async function saveKey() {
    try {
      setFeedback(null);
      await setKey.mutateAsync({
        id: provider.id,
        apiKey: apiKey.trim(),
        expectedVersion: provider.version,
      });
      setApiKey("");
      setFeedback("API 密钥已保存到系统安全存储");
    } catch {
      // mutation state renders a safe error below
    }
  }

  async function runHealthCheck() {
    try {
      setFeedback(null);
      const next = await checkHealth.mutateAsync({
        id: provider.id,
        expectedVersion: provider.version,
      });
      setFeedback(
        next.status === "ready" ? "连接成功" : "连接失败，请检查密钥与端点",
      );
    } catch {
      // mutation state renders a safe error below
    }
  }

  async function runEvaluation(suiteKey: AiEvaluationSuiteKey) {
    try {
      setFeedback(null);
      await createEvaluation.mutateAsync({
        providerId: provider.id,
        providerVersion: provider.version,
        suiteKey,
      });
      setFeedback(
        suiteKey === "smoke"
          ? "快速质量检查已进入队列，可在下方查看进度"
          : suiteKey === "full"
            ? "完整质量评测已进入队列，可在下方查看进度"
            : `${aiEvaluationSuiteLabels[suiteKey]}专题检查已进入队列，可在下方查看进度`,
      );
    } catch {
      // mutation state renders a safe error below
    }
  }

  async function remove() {
    try {
      setFeedback(null);
      await deleteProvider.mutateAsync({
        id: provider.id,
        expectedVersion: provider.version,
      });
      setConfirmingDelete(false);
      setFeedback(`供应商 ${provider.name} 与其密钥已删除`);
    } catch {
      // mutation state renders a safe error below
    }
  }

  return (
    <section className="ai-provider-card">
      <div className="ai-provider-heading">
        <div className="ai-provider-icon">
          <Bot size={18} />
        </div>
        <div>
          <h4>{provider.name}</h4>
          <p>
            {provider.kind === "local" ? "本地部署 · " : null}
            {protocolLabels[provider.protocol]} · {provider.model}
          </p>
        </div>
        <span
          data-status={provider.health_status}
          title={
            provider.health_error_code
              ? (healthErrorLabels[provider.health_error_code] ??
                provider.health_error_code)
              : undefined
          }
        >
          {provider.status === "ready" ? "已就绪" : "未就绪"}
        </span>
      </div>
      {provider.health_error_code ? (
        <div className="ai-provider-health-note" role="status">
          <AlertCircle size={14} />
          {healthErrorLabels[provider.health_error_code] ??
            provider.health_error_code}
        </div>
      ) : null}
      {provider.kind === "local" ? (
        <div className="ai-provider-health-note" role="status">
          <Bot size={14} />
          本地部署供应商，无需 API 密钥。
        </div>
      ) : (
        <div className="ai-provider-actions">
          <label>
            <KeyRound size={14} />
            <input
              autoComplete="off"
              onChange={(event) => setApiKey(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && apiKey.trim()) {
                  void saveKey();
                }
              }}
              placeholder={
                provider.has_key ? "已保存（输入可更换）" : "API key"
              }
              type="password"
              value={apiKey}
            />
          </label>
          <button
            className="button button-secondary"
            disabled={setKey.isPending || !apiKey.trim()}
            onClick={() => void saveKey()}
            type="button"
          >
            保存密钥
          </button>
        </div>
      )}
      <div className="ai-provider-actions">
        <button
          className="button button-secondary"
          disabled={checkHealth.isPending}
          onClick={() => void runHealthCheck()}
          type="button"
        >
          {checkHealth.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <PlugZap size={14} />
          )}
          测试连接
        </button>
        {provider.kind === "local" ? (
          <>
            <button
              className="button button-secondary"
              disabled={
                createEvaluation.isPending ||
                evaluationActive ||
                provider.status !== "ready" ||
                provider.health_status !== "healthy"
              }
              onClick={() => void runEvaluation("smoke")}
              title="运行 8 个平衡 case；只用于快速诊断"
              type="button"
            >
              {createEvaluation.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : (
                <FlaskConical size={14} />
              )}
              快速质量检查
            </button>
            <button
              className="button button-secondary"
              disabled={
                createEvaluation.isPending ||
                evaluationActive ||
                provider.status !== "ready" ||
                provider.health_status !== "healthy"
              }
              onClick={() => void runEvaluation("full")}
              title="运行当前完整 24-case 数据集；可积累人工评审候选证据"
              type="button"
            >
              {createEvaluation.isPending ? (
                <LoaderCircle className="animate-spin" size={14} />
              ) : (
                <FlaskConical size={14} />
              )}
              {evaluationActive ? "评测进行中" : "完整质量评测"}
            </button>
          </>
        ) : null}
      </div>
      {provider.kind === "local" ? (
        <div className="ai-evaluation-topic-controls">
          <label>
            <span>6-case 专题套件</span>
            <select
              aria-label="本地质量专题套件"
              disabled={createEvaluation.isPending || evaluationActive}
              onChange={(event) =>
                setTopicSuite(event.target.value as AiEvaluationTopicSuiteKey)
              }
              value={topicSuite}
            >
              {aiEvaluationTopicSuiteKeys.map((suiteKey) => (
                <option key={suiteKey} value={suiteKey}>
                  {aiEvaluationSuiteLabels[suiteKey]}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-secondary"
            disabled={
              createEvaluation.isPending ||
              evaluationActive ||
              provider.status !== "ready" ||
              provider.health_status !== "healthy"
            }
            onClick={() => void runEvaluation(topicSuite)}
            title="运行所选类别的 6 个中英平衡 case；只用于定向诊断"
            type="button"
          >
            {createEvaluation.isPending ? (
              <LoaderCircle className="animate-spin" size={14} />
            ) : (
              <FlaskConical size={14} />
            )}
            运行专题检查
          </button>
          <small>每类中英各 3 个 case；不形成候选资格。</small>
        </div>
      ) : null}
      {confirmingDelete ? (
        <div className="ai-provider-delete-confirm">
          <span>
            删除供应商会同时清除安全存储中的密钥；已有会话或本地评测历史时系统会拒绝删除。
          </span>
          <button
            className="button button-secondary"
            disabled={deleteProvider.isPending}
            onClick={() => void remove()}
            type="button"
          >
            确认删除
          </button>
          <button
            className="button button-quiet"
            onClick={() => setConfirmingDelete(false)}
            type="button"
          >
            取消
          </button>
        </div>
      ) : (
        <div className="ai-provider-footer">
          <button
            className="button button-quiet"
            onClick={() => setConfirmingDelete(true)}
            type="button"
          >
            <Trash2 size={14} />
            删除供应商
          </button>
        </div>
      )}
      {feedback ? (
        <div className="automation-success" role="status">
          <CheckCircle2 size={15} />
          {feedback}
        </div>
      ) : null}
      {actionError ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          {aiProviderError(actionError, "操作失败，请重试")}
        </div>
      ) : null}
    </section>
  );
}

const aiEvaluationStatusLabels: Record<AiEvaluationRun["status"], string> = {
  queued: "排队中",
  running: "运行中",
  succeeded: "已完成",
  failed: "运行失败",
  cancelled: "已取消",
};

const aiEvaluationCategoryLabels: Record<
  AiEvaluationRun["results"][number]["category"],
  string
> = {
  grounded: "有依据回答",
  no_evidence: "资料不足",
  prompt_injection: "引用注入防护",
  conflicting_sources: "冲突来源",
};

const aiEvaluationFailureLabels: Record<string, string> = {
  ANSWER_EMPTY: "回答为空",
  CONTROL_BLOCK_LEAKED: "内部控制块泄露",
  CITATION_STATUS_MISMATCH: "引用状态不符",
  REQUIRED_PHRASE_MISSING: "缺少必要事实",
  FORBIDDEN_PHRASE_PRESENT: "出现禁止结论",
  CITATION_NOT_ALLOWED: "引用越权",
  CITATION_DUPLICATE: "重复引用",
  CITATION_COUNT_LOW: "引用数量不足",
  CITATION_SET_MISMATCH: "引用集合不符",
  CASE_ID_MISMATCH: "case 归属不符",
  OBSERVATION_MISSING: "缺少评测观察",
};

function AiEvaluationHistory({
  query,
  reviews,
  summary,
}: {
  query: ReturnType<typeof useAiEvaluationsQuery>;
  reviews: ReturnType<typeof useAiEvaluationReviewsQuery>;
  summary: ReturnType<typeof useAiEvaluationSummaryQuery>;
}) {
  return (
    <section className="ai-evaluation-settings">
      <div className="ai-evaluation-settings-heading">
        <span>
          <h4>本地模型质量评测</h4>
          <p>
            显式运行 8-case 快速、6-case 专题或当前 24-case
            完整评测；只调用已就绪的本地 Provider，不保存问题、资料或模型回答。
          </p>
        </span>
        <button
          aria-label="刷新本地质量评测"
          className="button button-quiet"
          disabled={
            query.isFetching || summary.isFetching || reviews.isFetching
          }
          onClick={() =>
            void Promise.all([
              query.refetch(),
              summary.refetch(),
              reviews.refetch(),
            ])
          }
          type="button"
        >
          <RefreshCw
            className={
              query.isFetching || summary.isFetching || reviews.isFetching
                ? "animate-spin"
                : ""
            }
            size={13}
          />
        </button>
      </div>
      <AiEvaluationTrend query={summary} />
      <AiEvaluationReviewHistory query={reviews} />
      {query.isPending ? (
        <div className="settings-state" role="status">
          <LoaderCircle className="animate-spin" size={16} />
          正在读取本地评测历史…
        </div>
      ) : query.isError || !query.data ? (
        <div className="settings-state settings-state-error" role="alert">
          <AlertCircle size={16} />
          无法读取本地评测历史
        </div>
      ) : query.data.items.length === 0 ? (
        <div className="settings-state">暂无本地质量评测记录。</div>
      ) : (
        <div className="ai-evaluation-runs">
          {query.data.items.map((run) => (
            <AiEvaluationRunRow key={run.id} run={run} />
          ))}
        </div>
      )}
      <p className="ai-evaluation-privacy-note">
        结果只含状态、失败码、引用计数与无正文指标；不会调用远程
        Provider，也不会写入业务导出。
      </p>
    </section>
  );
}

function AiEvaluationReviewHistory({
  query,
}: {
  query: ReturnType<typeof useAiEvaluationReviewsQuery>;
}) {
  return (
    <section className="ai-evaluation-review-history">
      <header>
        <span>
          <h5>人工决定审计</h5>
          <small>仅审计，不改变模型、聊天、Provider 或业务状态。</small>
        </span>
        <small>追加式记录，不提供编辑或删除</small>
      </header>
      {query.isPending ? (
        <span className="ai-evaluation-detail-state" role="status">
          正在读取人工决定…
        </span>
      ) : query.isError || !query.data ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={14} />
          无法读取人工决定
          <button
            className="button button-quiet"
            onClick={() => void query.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : query.data.items.length === 0 ? (
        <div className="ai-evaluation-trend-empty">暂无人工决定记录。</div>
      ) : (
        <div className="ai-evaluation-review-list">
          {query.data.items.map((review) => (
            <article key={review.id}>
              <div>
                <span
                  className="ai-evaluation-review-decision"
                  data-decision={review.decision}
                >
                  {aiEvaluationReviewDecisionLabels[review.decision]}
                </span>
                <strong>
                  {review.providerNameSnapshot} · {review.providerModelSnapshot}
                </strong>
                <small>
                  dataset v{review.datasetVersion} ·{" "}
                  {aiEvaluationSuiteLabels[review.suiteKey]}套件 · Provider v
                  {review.providerVersionMin === review.providerVersionMax
                    ? review.providerVersionMin
                    : `${review.providerVersionMin}–${review.providerVersionMax}`}
                </small>
              </div>
              <p>{review.reason}</p>
              <small>
                证据快照：{review.runCount} 次 · {review.passedCases}/
                {review.totalCases} case · 总体 95% 下界{" "}
                {formatBasisPointPercent(review.overallWilsonLowerBps)} ·
                最低类别 {aiEvaluationCategoryLabels[review.minimumCategory]}{" "}
                {formatBasisPointPercent(review.minimumCategoryWilsonLowerBps)}
                {review.criticalFailureCodes.length > 0
                  ? ` · 严重失败 ${review.criticalFailureCodes
                      .map((code) => aiEvaluationFailureLabels[code] ?? code)
                      .join("、")}`
                  : ""}
              </small>
              <small>
                {review.reviewedByActorNameSnapshot} ·{" "}
                {new Date(review.createdAt).toLocaleString("zh-CN", {
                  hour12: false,
                })}
              </small>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function AiEvaluationTrend({
  query,
}: {
  query: ReturnType<typeof useAiEvaluationSummaryQuery>;
}) {
  const createReview = useCreateAiEvaluationReview();
  const [reviewingGroupKey, setReviewingGroupKey] = useState<string | null>(
    null,
  );
  const [reviewDecision, setReviewDecision] =
    useState<AiEvaluationReviewDecision>("needs_more_evidence");
  const [reviewReason, setReviewReason] = useState("");
  if (query.isPending) {
    return (
      <span className="ai-evaluation-detail-state" role="status">
        正在计算本地质量趋势…
      </span>
    );
  }
  if (query.isError || !query.data) {
    return (
      <div className="automation-error" role="alert">
        <AlertCircle size={14} />
        无法读取本地质量趋势
        <button
          className="button button-quiet"
          onClick={() => void query.refetch()}
          type="button"
        >
          重试
        </button>
      </div>
    );
  }
  const status = query.data.statusCounts;
  return (
    <div className="ai-evaluation-trend">
      <div className="ai-evaluation-trend-status">
        <span>完整运行 {status.succeeded}</span>
        <span>运行失败 {status.failed}</span>
        <span>已取消 {status.cancelled}</span>
        {status.queued + status.running > 0 ? (
          <span>活动 {status.queued + status.running}</span>
        ) : null}
      </div>
      {query.data.groups.length === 0 ? (
        <div className="ai-evaluation-trend-empty">
          完成一次本地评测后，将按模型快照和数据集版本显示趋势。
        </div>
      ) : (
        <>
          <div className="ai-evaluation-quality-groups">
            {query.data.groups.map((group) => {
              const groupKey = aiEvaluationGroupKey(group);
              const editing = reviewingGroupKey === groupKey;
              return (
                <article key={groupKey}>
                  <span>
                    <strong>
                      {group.providerNameSnapshot} ·{" "}
                      {group.providerModelSnapshot}
                    </strong>
                    <small>
                      dataset v{group.datasetVersion} ·{" "}
                      {aiEvaluationSuiteLabels[group.suiteKey]}套件 ·{" "}
                      {group.runCount} 次完整运行
                    </small>
                  </span>
                  <span>
                    <strong>
                      {formatBasisPointPercent(group.passRateBps)}
                    </strong>
                    <small>
                      {group.passedCases}/{group.totalCases} case · 全通过{" "}
                      {group.fullyPassedRuns}/{group.runCount}
                    </small>
                    <small className="ai-evaluation-confidence">
                      95%{" "}
                      {formatWilsonInterval(
                        group.wilsonLowerBps,
                        group.wilsonUpperBps,
                      )}{" "}
                      · {aiEvaluationEvidenceLabels[group.evidenceLevel]}
                    </small>
                  </span>
                  <small
                    className="ai-evaluation-readiness"
                    data-status={group.readinessStatus}
                  >
                    {aiEvaluationReadinessLabels[group.readinessStatus]}
                    {group.readinessReasons.length > 0
                      ? ` · ${group.readinessReasons
                          .map((reason) =>
                            readinessReasonLabel(
                              reason,
                              query.data.readinessPolicy,
                            ),
                          )
                          .join("、")}`
                      : null}
                  </small>
                  <div className="ai-evaluation-review-action">
                    <button
                      className="button button-quiet"
                      onClick={() => {
                        if (editing) {
                          setReviewingGroupKey(null);
                          return;
                        }
                        setReviewingGroupKey(groupKey);
                        setReviewDecision(
                          group.readinessStatus === "review_candidate"
                            ? "accepted_for_local_use"
                            : "needs_more_evidence",
                        );
                        setReviewReason("");
                      }}
                      type="button"
                    >
                      {editing ? "收起人工决定" : "记录人工决定"}
                    </button>
                  </div>
                  {editing ? (
                    <form
                      className="ai-evaluation-review-editor"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void createReview
                          .mutateAsync({
                            group,
                            decision: reviewDecision,
                            reason: reviewReason.trim(),
                          })
                          .then(() => {
                            setReviewingGroupKey(null);
                            setReviewReason("");
                          })
                          .catch(() => undefined);
                      }}
                    >
                      <label>
                        决定
                        <select
                          aria-label="人工决定"
                          disabled={createReview.isPending}
                          onChange={(event) =>
                            setReviewDecision(
                              event.target.value as AiEvaluationReviewDecision,
                            )
                          }
                          value={reviewDecision}
                        >
                          <option value="accepted_for_local_use">
                            接受本机试用
                          </option>
                          <option value="needs_more_evidence">
                            需要更多证据
                          </option>
                          <option value="rejected">拒绝使用</option>
                        </select>
                      </label>
                      <label>
                        理由（必填）
                        <textarea
                          aria-label="人工决定理由"
                          disabled={createReview.isPending}
                          maxLength={1000}
                          onChange={(event) =>
                            setReviewReason(event.target.value)
                          }
                          placeholder="说明你依据哪些本地评测证据作出决定"
                          rows={3}
                          value={reviewReason}
                        />
                      </label>
                      <small>
                        保存的是当前证据快照；若评测结果变化，系统会拒绝旧快照。
                      </small>
                      {createReview.error ? (
                        <div className="automation-error" role="alert">
                          <AlertCircle size={14} />
                          {aiProviderError(
                            createReview.error,
                            "无法记录人工决定",
                          )}
                        </div>
                      ) : null}
                      <div>
                        <button
                          className="button button-primary"
                          disabled={
                            createReview.isPending || !reviewReason.trim()
                          }
                          type="submit"
                        >
                          {createReview.isPending ? (
                            <LoaderCircle className="animate-spin" size={13} />
                          ) : null}
                          保存不可变记录
                        </button>
                        <button
                          className="button button-quiet"
                          disabled={createReview.isPending}
                          onClick={() => setReviewingGroupKey(null)}
                          type="button"
                        >
                          取消
                        </button>
                      </div>
                    </form>
                  ) : null}
                </article>
              );
            })}
          </div>
          <div
            aria-label="本地评测时间趋势"
            className="ai-evaluation-trend-points"
          >
            {query.data.trend.map((point) => {
              const score = Math.round(
                (point.passedCases / point.totalCases) * 100,
              );
              return (
                <div key={point.runId}>
                  <span>
                    <strong>{point.providerModelSnapshot}</strong>
                    <small>
                      v{point.datasetVersion} ·{" "}
                      {aiEvaluationSuiteLabels[point.suiteKey]} ·{" "}
                      {point.passedCases}/{point.totalCases}
                    </small>
                  </span>
                  <span className="ai-evaluation-trend-track">
                    <span style={{ width: `${score}%` }} />
                  </span>
                  <strong>{score}%</strong>
                </div>
              );
            })}
          </div>
          <div className="ai-evaluation-category-summary">
            <h5>按类别</h5>
            <div>
              {query.data.groups.map((group) => (
                <section
                  key={`${group.providerId}:${group.providerNameSnapshot}:${group.providerModelSnapshot}:${group.datasetVersion}:${group.suiteKey}`}
                >
                  <header>
                    {group.providerModelSnapshot} · v{group.datasetVersion} ·{" "}
                    {aiEvaluationSuiteLabels[group.suiteKey]}
                  </header>
                  {query.data.categories
                    .filter(
                      (category) =>
                        category.providerId === group.providerId &&
                        category.providerNameSnapshot ===
                          group.providerNameSnapshot &&
                        category.providerModelSnapshot ===
                          group.providerModelSnapshot &&
                        category.datasetVersion === group.datasetVersion &&
                        category.suiteKey === group.suiteKey,
                    )
                    .map((category) => {
                      const score = Math.round(
                        (category.passedCases / category.totalCases) * 100,
                      );
                      return (
                        <div key={category.category}>
                          <span>
                            {aiEvaluationCategoryLabels[category.category]}
                          </span>
                          <span className="ai-evaluation-category-track">
                            <span style={{ width: `${score}%` }} />
                          </span>
                          <span className="ai-evaluation-category-result">
                            <strong>
                              {category.passedCases}/{category.totalCases}
                            </strong>
                            <small>
                              {formatWilsonInterval(
                                category.wilsonLowerBps,
                                category.wilsonUpperBps,
                              )}
                            </small>
                          </span>
                        </div>
                      );
                    })}
                </section>
              ))}
            </div>
          </div>
          {query.data.failureCodes.length > 0 ? (
            <div className="ai-evaluation-failure-summary">
              <h5>失败原因</h5>
              <div>
                {query.data.groups
                  .filter((group) =>
                    query.data.failureCodes.some(
                      (failure) =>
                        failure.providerId === group.providerId &&
                        failure.providerNameSnapshot ===
                          group.providerNameSnapshot &&
                        failure.providerModelSnapshot ===
                          group.providerModelSnapshot &&
                        failure.datasetVersion === group.datasetVersion &&
                        failure.suiteKey === group.suiteKey,
                    ),
                  )
                  .map((group) => (
                    <section
                      key={`${group.providerId}:${group.providerNameSnapshot}:${group.providerModelSnapshot}:${group.datasetVersion}:${group.suiteKey}`}
                    >
                      <header>
                        {group.providerModelSnapshot} · v{group.datasetVersion}{" "}
                        · {aiEvaluationSuiteLabels[group.suiteKey]}
                      </header>
                      {query.data.failureCodes
                        .filter(
                          (failure) =>
                            failure.providerId === group.providerId &&
                            failure.providerNameSnapshot ===
                              group.providerNameSnapshot &&
                            failure.providerModelSnapshot ===
                              group.providerModelSnapshot &&
                            failure.datasetVersion === group.datasetVersion &&
                            failure.suiteKey === group.suiteKey,
                        )
                        .map((failure) => (
                          <div key={failure.failureCode}>
                            <span>
                              <strong>
                                {aiEvaluationFailureLabels[
                                  failure.failureCode
                                ] ?? failure.failureCode}
                              </strong>
                              <small>
                                影响 {failure.affectedCases} case · 出现{" "}
                                {failure.occurrences} 次
                              </small>
                            </span>
                            <span className="ai-evaluation-failure-track">
                              <span
                                style={{
                                  width: `${Math.round(
                                    (failure.affectedCases /
                                      group.failedCases) *
                                      100,
                                  )}%`,
                                }}
                              />
                            </span>
                          </div>
                        ))}
                    </section>
                  ))}
              </div>
            </div>
          ) : null}
          <p className="ai-evaluation-trend-note">
            每组只比较相同 Provider 名称/模型快照、dataset version 与套件；95%
            Wilson 区间描述固定样本的不确定性，“重复观察”从{" "}
            {query.data.uncertainty.repeatedRunMinimum} 次完整 Run
            开始。快速与专题套件只用于诊断；人工评审候选还要求当前 dataset
            的完整套件、总体下界至少{" "}
            {formatBasisPointPercent(
              query.data.readinessPolicy.minimumOverallLowerBps,
            )}
            、四类下界至少{" "}
            {formatBasisPointPercent(
              query.data.readinessPolicy.minimumCategoryLowerBps,
            )}
            且无严重失败码；它仍不代表发布许可。运行失败、取消和活动 Run
            不进入质量比例。
          </p>
        </>
      )}
    </div>
  );
}

function AiEvaluationRunRow({ run }: { run: AiEvaluationRun }) {
  const [expanded, setExpanded] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const detail = useAiEvaluationQuery(run.id, expanded);
  const cancel = useCancelAiEvaluation();
  const deleteEvaluation = useDeleteAiEvaluation();
  const current = detail.data ?? run;
  const active = current.status === "queued" || current.status === "running";
  return (
    <article className="ai-evaluation-run" data-status={current.status}>
      <button
        aria-expanded={expanded}
        className="ai-evaluation-run-summary"
        onClick={() => setExpanded((value) => !value)}
        type="button"
      >
        <span className="ai-evaluation-run-icon">
          <FlaskConical size={14} />
        </span>
        <span className="ai-evaluation-run-copy">
          <strong>
            {current.providerNameSnapshot} · {current.providerModelSnapshot}
          </strong>
          <small>
            数据集 v{current.datasetVersion} ·{" "}
            {aiEvaluationSuiteLabels[current.suiteKey]}套件 ·{" "}
            {current.completedCases}/{current.totalCases} case · 通过{" "}
            {current.passedCases} · 未通过 {current.failedCases}
            {current.errorCases ? ` · 错误 ${current.errorCases}` : ""}
          </small>
        </span>
        <span className="ai-evaluation-run-status" data-status={current.status}>
          {aiEvaluationStatusLabels[current.status]}
        </span>
        <ChevronDown size={13} />
      </button>
      {expanded ? (
        <div className="ai-evaluation-run-detail">
          {detail.isPending ? (
            <span className="ai-evaluation-detail-state">
              正在读取无正文结果…
            </span>
          ) : null}
          {detail.isError ? (
            <div className="automation-error" role="alert">
              <AlertCircle size={14} />
              无法读取评测详情
              <button
                className="button button-quiet"
                onClick={() => void detail.refetch()}
                type="button"
              >
                重试
              </button>
            </div>
          ) : null}
          {detail.data ? (
            <>
              {detail.data.currentCaseId ? (
                <div className="ai-evaluation-current" role="status">
                  正在运行：{detail.data.currentCaseId}
                </div>
              ) : null}
              {detail.data.results.length === 0 ? (
                <span className="ai-evaluation-detail-state">
                  尚无已完成 case。
                </span>
              ) : (
                <div className="ai-evaluation-results">
                  {detail.data.results.map((result) => (
                    <div data-status={result.status} key={result.id}>
                      <span>
                        <strong>
                          {result.sequence}.{" "}
                          {aiEvaluationCategoryLabels[result.category]}
                        </strong>
                        <small>{result.caseId}</small>
                      </span>
                      <span>
                        <strong>
                          {result.status === "passed"
                            ? "通过"
                            : result.status === "error"
                              ? `运行错误 · ${result.errorCode}`
                              : result.failureCodes
                                  .map(
                                    (code) =>
                                      aiEvaluationFailureLabels[code] ?? code,
                                  )
                                  .join(" / ")}
                        </strong>
                        <small>
                          引用 {result.citationCount} · {result.durationMs} ms
                          {result.tokenSource === "provider"
                            ? ` · ${result.inputTokens} → ${result.outputTokens} tokens`
                            : " · token unknown"}
                        </small>
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : null}
          {active ? (
            <button
              className="button button-secondary"
              disabled={cancel.isPending || current.cancelRequested}
              onClick={() => void cancel.mutateAsync({ id: current.id })}
              type="button"
            >
              {current.cancelRequested ? "正在取消" : "取消评测"}
            </button>
          ) : confirmingDelete ? (
            <div className="ai-evaluation-delete-confirm">
              <span>删除本次无正文评测历史及其 case 结果？</span>
              <button
                className="button button-secondary"
                disabled={deleteEvaluation.isPending}
                onClick={() =>
                  void deleteEvaluation
                    .mutateAsync({ id: current.id })
                    .then(() => setConfirmingDelete(false))
                }
                type="button"
              >
                确认删除
              </button>
              <button
                className="button button-quiet"
                disabled={deleteEvaluation.isPending}
                onClick={() => setConfirmingDelete(false)}
                type="button"
              >
                取消
              </button>
            </div>
          ) : (
            <button
              className="button button-quiet"
              onClick={() => setConfirmingDelete(true)}
              type="button"
            >
              <Trash2 size={13} />
              删除评测历史
            </button>
          )}
          {cancel.error || deleteEvaluation.error ? (
            <div className="automation-error" role="alert">
              <AlertCircle size={14} />
              评测操作失败，请刷新后重试
            </div>
          ) : null}
          {current.errorCode ? (
            <div className="automation-error" role="alert">
              <AlertCircle size={14} />
              {current.errorCode}
            </div>
          ) : null}
        </div>
      ) : null}
    </article>
  );
}

function AiProviderForm({ onDone }: { onDone: () => void }) {
  const createProvider = useCreateAiProvider();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<AiProviderKind>("remote");
  const [protocol, setProtocol] = useState<AiProviderProtocol>("openai_chat");
  const [baseUrl, setBaseUrl] = useState("");
  const [model, setModel] = useState("");
  const localMode = kind === "local";

  async function register() {
    try {
      await createProvider.mutateAsync({
        name: name.trim(),
        kind,
        protocol: localMode ? "openai_chat" : protocol,
        base_url: baseUrl.trim(),
        model: model.trim(),
      });
      onDone();
    } catch {
      // mutation state renders a safe error below
    }
  }

  return (
    <section className="ai-provider-form">
      <label>
        名称
        <input
          onChange={(event) => setName(event.target.value)}
          placeholder="例如 DeepSeek"
          value={name}
        />
      </label>
      <label>
        类型
        <select
          onChange={(event) => setKind(event.target.value as AiProviderKind)}
          value={kind}
        >
          <option value="remote">远程 API（API key）</option>
          <option value="local">本地部署（无需密钥）</option>
        </select>
      </label>
      {localMode ? (
        <div className="ai-provider-health-note" role="status">
          <AlertCircle size={14} />
          本地部署使用 OpenAI 兼容端点，例如 Ollama
          <code>http://127.0.0.1:11434/v1</code> 或 LM Studio
          <code>http://127.0.0.1:1234/v1</code>；不保存任何密钥。
        </div>
      ) : null}
      <label>
        协议
        <select
          disabled={localMode}
          onChange={(event) =>
            setProtocol(event.target.value as AiProviderProtocol)
          }
          value={localMode ? "openai_chat" : protocol}
        >
          <option value="openai_chat">{protocolLabels.openai_chat}</option>
          {localMode ? null : (
            <option value="anthropic_messages">
              {protocolLabels.anthropic_messages}
            </option>
          )}
        </select>
      </label>
      <label>
        Base URL
        <input
          onChange={(event) => setBaseUrl(event.target.value)}
          placeholder={
            localMode
              ? "http://127.0.0.1:11434/v1"
              : "https://api.example.com/v1"
          }
          value={baseUrl}
        />
      </label>
      <label>
        模型名
        <input
          onChange={(event) => setModel(event.target.value)}
          placeholder={localMode ? "例如 qwen3" : "例如 deepseek-chat"}
          value={model}
        />
      </label>
      <div className="ai-provider-footer">
        <button
          className="button button-primary"
          disabled={
            createProvider.isPending ||
            !name.trim() ||
            !baseUrl.trim() ||
            !model.trim()
          }
          onClick={() => void register()}
          type="button"
        >
          {createProvider.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Bot size={14} />
          )}
          登记供应商
        </button>
        <button className="button button-quiet" onClick={onDone} type="button">
          取消
        </button>
      </div>
      {createProvider.error ? (
        <div className="automation-error" role="alert">
          <AlertCircle size={15} />
          {aiProviderError(createProvider.error, "登记失败，请重试")}
        </div>
      ) : null}
    </section>
  );
}
