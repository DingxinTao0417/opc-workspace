import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import type {
  useAgentRunsQuery,
  useAiAgentInboxQuery,
  useAiWorkPlanInboxQuery,
  useRestoreDiagnosticsQuery,
} from "../api/hooks";
import type {
  AiAgentInboxAgentRun,
  AiAgentInboxContinuation,
} from "../api/aiAgentInbox";
import {
  continuationReasons,
  continuationStatuses,
} from "../api/aiPlanContinuation";
import { aiActionLabels } from "../api/aiWorkspaceActions";
import { planInboxStateLabels } from "../api/aiWorkPlan";
import { aiContinuationHref } from "../lib/aiContinuationLocation";
import { knowledgeIndexLocationHref } from "../lib/knowledgeIndexLocation";
import { agentRunLifecycle } from "../lib/agentRunLifecycle";
import { agentRunProgressStatus } from "../lib/agentRunProgress";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";
import {
  adapterIssueHandoff,
  agentRunIssueHandoff,
  agentRunInboxIssueHandoff,
  continuationInboxHandoff,
  automationFailureHandoff,
  generationFailureHandoff,
  knowledgeFailureHandoff,
  maintenanceFailureHandoff,
  providerIssueHandoff,
  reviewSubmissionHandoff,
  restoreDiagnosticsHandoff,
} from "../lib/aiIssueHandoff";
import { useAiChatStore } from "../store/aiChat";
import {
  outstandingFileOperationReminders,
  useFileOperationReminders,
  type FileOperationReminder,
} from "../store/fileOperationReminders";
import { useUiStore } from "../store/ui";
import type { AgentRunSummary } from "../types/models";
import { ErrorState, LoadingState } from "./feedback";

const fileOperationSourceLabels: Record<
  FileOperationReminder["source"],
  string
> = {
  ai_project_file: "智能体项目文件修改",
  agent_output_file: "Agent 产出写入项目文件",
  file_recovery: "文件恢复",
  file_replacement_restore: "替换撤回与恢复",
};

const fileOperationStatusLabels: Record<
  FileOperationReminder["status"],
  string
> = {
  in_flight: "进行中",
  interrupted: "应用在操作进行中退出 · 结果未知",
  uncertain: "结果未确认 · 不要重复提交",
  recovery_required: "需要核对恢复记录",
};

function runAttentionLabel(run: AgentRunSummary): string {
  const lifecycle = agentRunLifecycle(run);
  return lifecycle.phase === "running" && run.progress
    ? agentRunProgressStatus(run.progress)
    : lifecycle.label;
}

function agentInboxRunAttentionLabel(run: AiAgentInboxAgentRun): string {
  return agentRunLifecycle({
    status: run.run_status,
    outputDeliveryStatus: run.output_delivery_status,
  }).label;
}

function continuationInboxAttentionLabel(
  item: AiAgentInboxContinuation,
): string {
  return `${continuationStatuses[item.continuation_status]} · ${continuationReasons[item.continuation_reason]}`;
}

function maintenanceFailureLabel(component: string, operation: string): string {
  const labels: Record<string, string> = {
    "backup:create": "本地备份创建失败",
    "backup:verify": "本地备份校验失败",
    "backup:drill": "本地备份恢复演练失败",
    "backup:restore": "本地备份恢复安排失败",
    "database:startup": "本地数据库启动失败",
    "database:migration": "本地数据库迁移失败",
    "database:runtime": "本地数据库运行失败",
    "sidecar:startup": "本地服务启动失败",
    "storage:low_space": "本地存储空间不足",
  };
  return labels[`${component}:${operation}`] ?? "系统维护需要处理";
}

function AgentInboxGroup({
  label,
  count,
  children,
}: {
  label: string;
  count: number;
  children: ReactNode;
}) {
  if (count === 0) return null;
  return (
    <section aria-label={label} className="ai-agent-inbox-group">
      <div className="nav-label ai-agent-inbox-heading">
        <span>{label}</span>
        <span aria-label={`${count} 项`} className="ai-agent-inbox-count">
          {count}
        </span>
      </div>
      <div className="ai-plan-inbox">{children}</div>
    </section>
  );
}

interface AiAgentInboxPanelProps {
  planInbox: ReturnType<typeof useAiWorkPlanInboxQuery>;
  agentInbox: ReturnType<typeof useAiAgentInboxQuery>;
  runInbox: ReturnType<typeof useAgentRunsQuery>;
  restoreDiagnostics: ReturnType<typeof useRestoreDiagnosticsQuery>;
  isStreaming: boolean;
  onClose: () => void;
}

/**
 * Codex-style Agent Inbox. Every source keeps its own loading/error state, so
 * a failed read is never rendered as "nothing to do"; groups without items do
 * not occupy space and the whole inbox scrolls as one list.
 */
export function AiAgentInboxPanel({
  planInbox,
  agentInbox,
  runInbox,
  restoreDiagnostics,
  isStreaming,
  onClose,
}: AiAgentInboxPanelProps) {
  const navigate = useNavigate();
  const setActiveSessionId = useAiChatStore(
    (state) => state.setActiveSessionId,
  );
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const openAgentRunDrawer = useUiStore((state) => state.openAgentRunDrawer);
  const rightOverviewCollapsed = useUiStore(
    (state) => state.rightOverviewCollapsed,
  );
  const toggleRightOverviewCollapsed = useUiStore(
    (state) => state.toggleRightOverviewCollapsed,
  );
  const setRightPanelTab = useUiStore((state) => state.setRightPanelTab);
  const fileReminders = outstandingFileOperationReminders(
    useFileOperationReminders((state) => state.reminders),
  );
  const dismissFileReminder = useFileOperationReminders(
    (state) => state.dismiss,
  );

  const planReady = !planInbox.isPending && !planInbox.isError;
  const agentReady = !agentInbox.isPending && !agentInbox.isError;
  const runReady = !runInbox.isPending && !runInbox.isError;
  const restoreReady =
    !restoreDiagnostics.isPending && !restoreDiagnostics.isError;

  const planInboxItems = planReady
    ? (planInbox.data?.pages.flatMap((page) => page.data) ?? [])
    : [];
  const planInboxMeta = planInbox.data?.pages[0]?.meta;
  const runInboxItems = runReady ? (runInbox.data?.items ?? []) : [];
  const runInboxTotal = runReady ? (runInbox.data?.meta.total ?? 0) : 0;
  const agentInboxItems = agentReady
    ? (agentInbox.data?.pages.flatMap((page) => page.data) ?? [])
    : [];
  const agentInboxMeta = agentInbox.data?.pages[0]?.meta;
  const runInboxIDs = new Set(runInboxItems.map((run) => run.id));
  // Planned and unplanned runs share one inbox kind. Without the unplanned
  // run ledger we cannot prove which ones belong to a plan, so none are shown
  // under the plan label until that read succeeds.
  const plannedAgentRunInboxItems = runReady
    ? agentInboxItems.filter(
        (item): item is AiAgentInboxAgentRun =>
          item.kind === "agent_run" && !runInboxIDs.has(item.id),
      )
    : [];
  const continuationInboxItems = agentInboxItems.filter(
    (item): item is AiAgentInboxContinuation => item.kind === "continuation",
  );
  const approvalInboxItems = agentInboxItems.filter(
    (item) => item.kind === "approval",
  );
  const accessRequestInboxItems = agentInboxItems.filter(
    (item) => item.kind === "access_request",
  );
  const reviewInboxItems = agentInboxItems.filter(
    (item) => item.kind === "review",
  );
  const automationFailureInboxItems = agentInboxItems.filter(
    (item) => item.kind === "automation_failure",
  );
  const knowledgeFailureInboxItems = agentInboxItems.filter(
    (item) => item.kind === "knowledge_failure",
  );
  const generationFailureInboxItems = agentInboxItems.filter(
    (item) => item.kind === "generation_failure",
  );
  const providerIssueInboxItems = agentInboxItems.filter(
    (item) => item.kind === "provider_issue",
  );
  const adapterIssueInboxItems = agentInboxItems.filter(
    (item) => item.kind === "adapter_issue",
  );
  const maintenanceFailureInboxItems = agentInboxItems.filter(
    (item) => item.kind === "maintenance_failure",
  );
  const recoveryAttention =
    restoreReady &&
    restoreDiagnostics.data &&
    (restoreDiagnostics.data.restartRequired ||
      restoreDiagnostics.data.cleanupRequired ||
      restoreDiagnostics.data.attentionRequired)
      ? restoreDiagnostics.data
      : null;

  const sources = [
    { key: "plan", label: "智能体计划", query: planInbox },
    { key: "agent", label: "确认、验收与异常事项", query: agentInbox },
    { key: "runs", label: "独立执行", query: runInbox },
    { key: "restore", label: "恢复诊断", query: restoreDiagnostics },
  ] as const;
  const loadingSources = sources.filter((source) => source.query.isPending);
  const failedSources = sources.filter((source) => source.query.isError);
  const visibleTotal =
    approvalInboxItems.length +
    accessRequestInboxItems.length +
    fileReminders.length +
    reviewInboxItems.length +
    runInboxItems.length +
    plannedAgentRunInboxItems.length +
    planInboxItems.length +
    continuationInboxItems.length +
    generationFailureInboxItems.length +
    automationFailureInboxItems.length +
    knowledgeFailureInboxItems.length +
    providerIssueInboxItems.length +
    adapterIssueInboxItems.length +
    (recoveryAttention ? 1 : 0) +
    maintenanceFailureInboxItems.length;
  const allClear =
    loadingSources.length === 0 &&
    failedSources.length === 0 &&
    visibleTotal === 0 &&
    !planInbox.hasNextPage &&
    !agentInbox.hasNextPage;

  return (
    <div aria-label="续办队列" className="ai-agent-inbox" role="region">
      {loadingSources.length > 0 ? (
        <LoadingState
          label={`正在读取${loadingSources.map((source) => source.label).join("、")}…`}
        />
      ) : null}
      {failedSources.map((source) => (
        <ErrorState
          compact
          key={source.key}
          message={`无法读取${source.label}，这里的列表可能不完整`}
          onRetry={() => void source.query.refetch()}
          title="续办队列读取不完整"
        />
      ))}
      {allClear ? (
        <p className="ai-session-empty ai-agent-inbox-empty">
          没有需要你处理的事项。新的确认、验收、执行结果和异常会出现在这里。
        </p>
      ) : null}

      <AgentInboxGroup count={approvalInboxItems.length} label="等待确认">
        {approvalInboxItems.map((item) => (
          <button
            className="ai-plan-inbox-row"
            disabled={isStreaming}
            key={item.id}
            onClick={() => {
              setActiveSessionId(item.session_id);
              onClose();
              navigate(
                aiContinuationHref({
                  kind: "approval",
                  sessionId: item.session_id,
                  generationId: item.generation_id,
                  proposalId: item.id,
                }),
              );
            }}
            title={`${item.session_title} · 等待确认`}
            type="button"
          >
            <span className="ai-plan-inbox-title">
              {aiActionLabels[item.action as keyof typeof aiActionLabels] ??
                item.action}
            </span>
            <span className="ai-plan-inbox-meta">等待你的确认</span>
            <small>{item.session_title}</small>
          </button>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={accessRequestInboxItems.length}
        label="权限待核对"
      >
        {accessRequestInboxItems.map((item) => (
          <button
            className="ai-plan-inbox-row"
            disabled={isStreaming}
            key={item.id}
            onClick={() => {
              setActiveSessionId(item.session_id);
              onClose();
              navigate(`/ai?session=${item.session_id}`);
            }}
            title={`${item.session_title} · 权限待核对`}
            type="button"
          >
            <span className="ai-plan-inbox-title">工作台权限请求</span>
            <span className="ai-plan-inbox-meta">打开原会话核对</span>
            <small>{item.session_title}</small>
          </button>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup count={fileReminders.length} label="文件操作待核对">
        {fileReminders.map((reminder) => (
          <div className="ai-plan-inbox-entry" key={reminder.id}>
            <button
              className="ai-plan-inbox-row"
              data-state={reminder.status}
              onClick={() => {
                if (rightOverviewCollapsed) toggleRightOverviewCollapsed();
                setRightPanelTab("files", "activate");
                onClose();
              }}
              title={`${fileOperationSourceLabels[reminder.source]} · 打开文件恢复核对`}
              type="button"
            >
              <span className="ai-plan-inbox-title">
                {fileOperationSourceLabels[reminder.source]}
              </span>
              <span className="ai-plan-inbox-meta">
                {fileOperationStatusLabels[reminder.status]}
              </span>
              <small>
                本机界面记录，非执行回执 · 发起于{" "}
                {new Date(reminder.startedAt).toLocaleString("zh-CN", {
                  hour12: false,
                })}
              </small>
            </button>
            <button
              className="button button-quiet ai-issue-handoff-button"
              onClick={() => dismissFileReminder(reminder.id)}
              type="button"
            >
              已核对恢复记录，移除提醒
            </button>
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup count={reviewInboxItems.length} label="待验收">
        {reviewInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                navigate(
                  `/tasks/${item.task_id}/submissions/${item.submission_id}`,
                );
                onClose();
              }}
              title={`${item.task_title} · 第 ${item.sequence} 次提交`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.task_title}</span>
              <span className="ai-plan-inbox-meta">
                等待人工验收 · 第 {item.sequence} 次提交
              </span>
              <small>
                {item.submission_origin === "child_rollup"
                  ? "子任务汇总"
                  : "人工或 Agent 提交"}
              </small>
            </button>
            <AiIssueHandoffButton
              content={reviewSubmissionHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup count={runInboxItems.length} label="独立执行">
        {runInboxItems.map((run) => (
          <div className="ai-plan-inbox-entry" key={run.id}>
            <button
              className="ai-plan-inbox-row"
              data-state={run.outputDeliveryStatus}
              onClick={() => openAgentRunDrawer(run.taskId, run.id)}
              title={`${run.taskTitle} · ${runAttentionLabel(run)}`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{run.taskTitle}</span>
              <span className="ai-plan-inbox-meta">
                {runAttentionLabel(run)} · 第 {run.attempt} 次
              </span>
              <small>{run.model}</small>
            </button>
            <AiIssueHandoffButton
              content={agentRunIssueHandoff(run)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
        {runInboxTotal > runInboxItems.length ? (
          <button
            className="ai-plan-inbox-more"
            onClick={() => {
              if (rightOverviewCollapsed) {
                toggleRightOverviewCollapsed();
              }
              setRightPanelTab("agents");
              onClose();
            }}
            type="button"
          >
            还有 {runInboxTotal - runInboxItems.length} 项，在执行页查看
          </button>
        ) : null}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={plannedAgentRunInboxItems.length}
        label="计划内执行"
      >
        {plannedAgentRunInboxItems.map((run) => (
          <div className="ai-plan-inbox-entry" key={run.id}>
            <button
              className="ai-plan-inbox-row"
              data-state={run.output_delivery_status}
              onClick={() => openAgentRunDrawer(run.task_id, run.id)}
              title={`${run.task_title} · ${agentInboxRunAttentionLabel(run)}`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{run.task_title}</span>
              <span className="ai-plan-inbox-meta">
                {agentInboxRunAttentionLabel(run)} · 第 {run.attempt} 次
              </span>
              <small>来自当前智能体计划</small>
            </button>
            <AiIssueHandoffButton
              content={agentRunInboxIssueHandoff(run)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup count={planInboxItems.length} label="智能体计划">
        {planInboxItems.map((item) => (
          <button
            className="ai-plan-inbox-row"
            disabled={isStreaming}
            key={item.session_id}
            onClick={() => {
              setActiveSessionId(item.session_id);
              onClose();
              navigate(
                aiContinuationHref({
                  kind: "plan",
                  sessionId: item.session_id,
                }),
              );
            }}
            title={`${item.session_title} · ${item.plan_title}`}
            type="button"
          >
            <span className="ai-plan-inbox-title">{item.plan_title}</span>
            <span className="ai-plan-inbox-meta">
              {planInboxStateLabels[item.state]} · 已满足{" "}
              {item.satisfied_step_total}/
              {item.step_total - (item.superseded_step_total ?? 0)}
              {(item.superseded_step_total ?? 0) > 0
                ? ` · 历史替代 ${item.superseded_step_total} 步`
                : null}
            </span>
            <small>{item.session_title}</small>
          </button>
        ))}
        {planInboxMeta?.window_limited ? (
          <p className="ai-session-empty">
            队列只显示最近 1000 个计划；较早计划请从会话搜索进入。
          </p>
        ) : null}
      </AgentInboxGroup>
      {planReady && planInbox.hasNextPage ? (
        <button
          className="ai-plan-inbox-more"
          disabled={planInbox.isFetchingNextPage}
          onClick={() => void planInbox.fetchNextPage()}
          type="button"
        >
          {planInbox.isFetchingNextPage ? "正在加载…" : "加载更多计划"}
        </button>
      ) : null}

      <AgentInboxGroup count={continuationInboxItems.length} label="后台续办">
        {continuationInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              data-state={item.continuation_status}
              disabled={isStreaming}
              onClick={() => {
                setActiveSessionId(item.session_id);
                onClose();
                navigate(
                  aiContinuationHref({
                    kind: "plan",
                    sessionId: item.session_id,
                  }),
                );
              }}
              title={`${item.session_title} · ${continuationInboxAttentionLabel(item)}`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.session_title}</span>
              <span className="ai-plan-inbox-meta">
                {continuationInboxAttentionLabel(item)}
              </span>
              <small>
                已启动 {item.continuation_turns_started}/
                {item.continuation_max_turns} 轮 · 到期{" "}
                {new Date(item.continuation_expires_at).toLocaleTimeString(
                  "zh-CN",
                  { hour: "2-digit", minute: "2-digit", hour12: false },
                )}
              </small>
            </button>
            <AiIssueHandoffButton
              content={continuationInboxHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={generationFailureInboxItems.length}
        label="对话异常"
      >
        {generationFailureInboxItems.map((item) => {
          const interrupted = item.error_code === "AI_GENERATION_INTERRUPTED";
          return (
            <div className="ai-plan-inbox-entry" key={item.id}>
              <button
                className="ai-plan-inbox-row"
                disabled={isStreaming}
                onClick={() => {
                  setActiveSessionId(item.session_id);
                  onClose();
                }}
                title={`${item.session_title} · ${interrupted ? "生成中断" : "生成失败"}`}
                type="button"
              >
                <span className="ai-plan-inbox-title">
                  {item.session_title}
                </span>
                <span className="ai-plan-inbox-meta">
                  {interrupted
                    ? "服务重启时中断 · 可手动继续"
                    : "生成失败 · 可继续对话"}
                </span>
                <small>{item.error_code}</small>
              </button>
              <AiIssueHandoffButton
                content={generationFailureHandoff(item)}
                disabled={isStreaming}
                onNavigate={() => {
                  setActiveSessionId(item.session_id);
                  onClose();
                }}
              />
            </div>
          );
        })}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={automationFailureInboxItems.length}
        label="自动化异常"
      >
        {automationFailureInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                navigate(`/settings/automation?run=${item.id}`);
                onClose();
              }}
              title={`${item.rule_name} · 自动化执行失败`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.rule_name}</span>
              <span className="ai-plan-inbox-meta">
                执行失败 · 第 {item.attempt} 次
              </span>
              <small>
                {item.retryable
                  ? item.retry_at
                    ? "已安排自动重试"
                    : "可在详情中人工重试"
                  : "不可重试，请查看失败原因"}
              </small>
            </button>
            <AiIssueHandoffButton
              content={automationFailureHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={knowledgeFailureInboxItems.length}
        label="知识库异常"
      >
        {knowledgeFailureInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                const href = knowledgeIndexLocationHref(
                  item.source_id,
                  item.id,
                );
                if (href) navigate(href);
                onClose();
              }}
              title={`${item.source_name} · 知识库索引失败`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.source_name}</span>
              <span className="ai-plan-inbox-meta">
                索引失败 · 第 {item.attempt} 次
              </span>
              <small>
                {item.operation === "import" ? "首次导入" : "重建索引"} ·
                可在知识库中人工重试
              </small>
            </button>
            <AiIssueHandoffButton
              content={knowledgeFailureHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={providerIssueInboxItems.length}
        label="供应商待处理"
      >
        {providerIssueInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                setSettingsOpen(true, "ai");
                onClose();
              }}
              title={`${item.provider_name} · 供应商未就绪`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.provider_name}</span>
              <span className="ai-plan-inbox-meta">
                {item.provider_status === "unconfigured"
                  ? "尚未完成配置"
                  : "连接检查未通过"}
              </span>
              <small>
                {item.provider_model}
                {item.error_code ? ` · ${item.error_code}` : ""}
              </small>
            </button>
            <AiIssueHandoffButton
              content={providerIssueHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={adapterIssueInboxItems.length}
        label="本地 Agent 待处理"
      >
        {adapterIssueInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                setSettingsOpen(true, "agent");
                onClose();
              }}
              title={`${item.adapter_name} · 本地 Agent 未就绪`}
              type="button"
            >
              <span className="ai-plan-inbox-title">{item.adapter_name}</span>
              <span className="ai-plan-inbox-meta">
                {item.health_status === "blocked"
                  ? "运行条件被阻断"
                  : "健康检查未通过"}
              </span>
              <small>
                {item.adapter_key}
                {item.error_code ? ` · ${item.error_code}` : ""}
              </small>
            </button>
            <AiIssueHandoffButton
              content={adapterIssueHandoff(item)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      <AgentInboxGroup count={recoveryAttention ? 1 : 0} label="数据恢复">
        {recoveryAttention ? (
          <div className="ai-plan-inbox-entry">
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                setSettingsOpen(true, "data");
                onClose();
              }}
              title="数据恢复需要处理"
              type="button"
            >
              <span className="ai-plan-inbox-title">本地数据恢复</span>
              <span className="ai-plan-inbox-meta">
                {recoveryAttention.attentionRequired
                  ? "恢复诊断需要人工核对"
                  : recoveryAttention.restartRequired
                    ? "恢复计划已挂起，等待重启"
                    : "恢复已应用，清理未完成"}
              </span>
              <small>
                {recoveryAttention.failedAttemptCount > 0
                  ? `${recoveryAttention.failedAttemptCount} 次失败记录`
                  : recoveryAttention.invalidEntryCount > 0
                    ? `${recoveryAttention.invalidEntryCount} 个无效条目`
                    : "打开数据设置查看恢复诊断"}
              </small>
            </button>
            <AiIssueHandoffButton
              content={restoreDiagnosticsHandoff(recoveryAttention)}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ) : null}
      </AgentInboxGroup>

      <AgentInboxGroup
        count={maintenanceFailureInboxItems.length}
        label="系统维护"
      >
        {maintenanceFailureInboxItems.map((item) => (
          <div className="ai-plan-inbox-entry" key={item.id}>
            <button
              className="ai-plan-inbox-row"
              onClick={() => {
                navigate(`/inbox/${item.id}`);
                onClose();
              }}
              title={`${maintenanceFailureLabel(item.component, item.operation)} · 打开原事项`}
              type="button"
            >
              <span className="ai-plan-inbox-title">
                {maintenanceFailureLabel(item.component, item.operation)}
              </span>
              <span className="ai-plan-inbox-meta">打开原系统维护事项</span>
              <small>{item.error_code}</small>
            </button>
            <AiIssueHandoffButton
              content={maintenanceFailureHandoff(
                item,
                maintenanceFailureLabel(item.component, item.operation),
              )}
              disabled={isStreaming}
              onNavigate={onClose}
            />
          </div>
        ))}
      </AgentInboxGroup>

      {agentReady && agentInbox.hasNextPage ? (
        <button
          className="ai-plan-inbox-more"
          disabled={agentInbox.isFetchingNextPage}
          onClick={() => void agentInbox.fetchNextPage()}
          type="button"
        >
          {agentInbox.isFetchingNextPage ? "正在加载…" : "加载更多续办事项"}
        </button>
      ) : null}
      {agentReady && agentInboxMeta?.window_limited ? (
        <p className="ai-session-empty">
          队列只显示最近 1000 项；更早事项请从对应会话或任务进入。
        </p>
      ) : null}
    </div>
  );
}
