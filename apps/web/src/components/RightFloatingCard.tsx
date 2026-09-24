import { useQuery } from "@tanstack/react-query";
import {
  BellRing,
  BookOpenText,
  Bot,
  CheckSquare2,
  ChevronRight,
  Cpu,
  DatabaseBackup,
  Sparkles,
  TimerReset,
} from "lucide-react";
import { getKnowledgeSources } from "../api/client";
import {
  useAgentRunsQuery,
  useAiEvaluationsQuery,
  useAiProvidersQuery,
  useActiveFocusSessionQuery,
  useBackupsQuery,
  useHealthQuery,
} from "../api/hooks";
import { useUiStore } from "../store/ui";

function relativeLabel(iso: string, nowMs: number) {
  const diff = Math.max(0, nowMs - new Date(iso).getTime());
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  return `${Math.floor(hours / 24)} 天前`;
}

export function RightFloatingCard({ inline = false }: { inline?: boolean }) {
  const healthQuery = useHealthQuery();
  const backupsQuery = useBackupsQuery();
  const agentRunsQuery = useAgentRunsQuery({ pageSize: 50 });
  const focusQuery = useActiveFocusSessionQuery();
  const evaluationsQuery = useAiEvaluationsQuery();
  const providersQuery = useAiProvidersQuery();
  const knowledgeQuery = useQuery({
    queryKey: ["knowledge", "sources"],
    queryFn: () => getKnowledgeSources({ pageSize: 100 }),
    retry: 1,
    staleTime: 30_000,
  });
  const setRightPanelTab = useUiStore((state) => state.setRightPanelTab);
  const rightOverviewCollapsed = useUiStore(
    (state) => state.rightOverviewCollapsed,
  );
  const toggleRightOverviewCollapsed = useUiStore(
    (state) => state.toggleRightOverviewCollapsed,
  );

  const health = healthQuery.data;
  const latestBackup = backupsQuery.data?.[0];
  const agentRunMeta = agentRunsQuery.data?.meta;
  const runningAgents = agentRunMeta?.activeTotal ?? 0;
  const pendingDeliveryAgents = agentRunMeta?.pendingDeliveryTotal ?? 0;
  const doneAgents = agentRunMeta?.succeededTotal ?? 0;
  const focusSession = focusQuery.data?.session;
  const runningEvaluations = (evaluationsQuery.data?.items ?? []).filter(
    (evaluation) =>
      evaluation.status === "running" || evaluation.status === "queued",
  ).length;
  const indexingSources = (knowledgeQuery.data?.items ?? []).filter(
    (source) => source.status === "indexing",
  ).length;
  const backgroundParts = [
    focusSession ? "专注" : null,
    runningEvaluations > 0 ? `评测 ${runningEvaluations}` : null,
    indexingSources > 0 ? `索引 ${indexingSources}` : null,
  ].filter(Boolean);

  const openAgents = () => {
    if (rightOverviewCollapsed) toggleRightOverviewCollapsed();
    setRightPanelTab("agents");
  };

  return (
    <aside
      aria-label="工作区状态"
      className={inline ? "ws-environment" : "right-floating-card"}
    >
      <section className="ov-group">
        <div className="ov-group-title">环境信息</div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <Cpu size={16} />
          </span>
          <span className="ov-label">版本</span>
          <span className="ov-value">
            {health
              ? `v${health.app.version} · ${health.schema.version}`
              : healthQuery.isError
                ? "读取失败"
                : "读取中"}
          </span>
        </div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <DatabaseBackup size={16} />
          </span>
          <span className="ov-label">最近备份</span>
          <span className="ov-value">
            {backupsQuery.isError
              ? "读取失败"
              : latestBackup
                ? relativeLabel(latestBackup.createdAt, Date.now())
                : "暂无"}
          </span>
        </div>
      </section>

      <section className="ov-group">
        <div className="ov-group-title">Agent 执行</div>
        <button className="ov-row" onClick={openAgents} type="button">
          <span className="ov-row-icon" aria-hidden="true">
            <Bot size={16} />
          </span>
          <span className="ov-label">Agent Runs</span>
          <span className="ov-value">
            {agentRunsQuery.isError
              ? "读取失败"
              : `${runningAgents} 运行${
                  pendingDeliveryAgents > 0
                    ? ` · ${pendingDeliveryAgents} 待登记`
                    : ""
                } · ${doneAgents} 完成`}
          </span>
          <span className="ov-chevron" aria-hidden="true">
            <ChevronRight size={14} />
          </span>
        </button>
      </section>

      <section className="ov-group">
        <div className="ov-group-title">后台进程</div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <TimerReset size={16} />
          </span>
          <span className="ov-label">运行中作业</span>
          <span className="ov-value">
            {backgroundParts.length > 0 ? backgroundParts.join(" · ") : "无"}
          </span>
        </div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <BellRing size={16} />
          </span>
          <span className="ov-label">专注会话</span>
          <span className="ov-value">
            {focusQuery.isError
              ? "读取失败"
              : focusSession
                ? focusSession.status
                : "空闲"}
          </span>
        </div>
      </section>

      <section className="ov-group">
        <div className="ov-group-title">来源</div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <BookOpenText size={16} />
          </span>
          <span className="ov-label">知识库</span>
          <span className="ov-value">
            {knowledgeQuery.isError
              ? "读取失败"
              : `${knowledgeQuery.data?.meta.total ?? 0} 来源`}
          </span>
        </div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <Sparkles size={16} />
          </span>
          <span className="ov-label">AI Provider</span>
          <span className="ov-value">
            {providersQuery.isError
              ? "读取失败"
              : `${providersQuery.data?.length ?? 0} 已配置`}
          </span>
        </div>
        <div className="ov-row">
          <span className="ov-row-icon" aria-hidden="true">
            <CheckSquare2 size={16} />
          </span>
          <span className="ov-label">评测</span>
          <span className="ov-value">
            {evaluationsQuery.isError
              ? "读取失败"
              : `${evaluationsQuery.data?.meta.total ?? 0} 次`}
          </span>
        </div>
      </section>
    </aside>
  );
}
