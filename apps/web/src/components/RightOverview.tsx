import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  BellRing,
  BookOpenText,
  Bot,
  CheckSquare2,
  ChevronRight,
  Cpu,
  DatabaseBackup,
  ExternalLink,
  FileText,
  Inbox,
  Sparkles,
  TimerReset,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import {
  cancelAgentRun,
  getKnowledgeSources,
  retryAgentRun,
} from "../api/client";
import {
  agentRunListQueryKey,
  useAgentRunsQuery,
  useAiProvidersQuery,
  useBackupsQuery,
  useControlledFilesQuery,
  useHealthQuery,
  useInboxStatsQuery,
  useRemindersQuery,
  useTaskArtifactQuery,
  useTodayStatsQuery,
} from "../api/hooks";
import type {
  AgentRunSummary,
  ControlledFile,
  ControlledFileScope,
} from "../types/models";
import { useLocalCalendar } from "../lib/localCalendar";
import { openExternalBrowserWindow } from "../api/desktop";
import { useUiStore, type RightPanelTab } from "../store/ui";

function relativeLabel(iso: string, nowMs: number) {
  const diff = Math.max(0, nowMs - new Date(iso).getTime());
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  return `${Math.floor(hours / 24)} 天前`;
}

function formatTrigger(iso: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(iso));
}

function isLoopbackUrl(raw: string) {
  try {
    const url = new URL(raw);
    if (url.protocol !== "http:" && url.protocol !== "https:") return false;
    return ["127.0.0.1", "localhost", "::1"].includes(url.hostname);
  } catch {
    return false;
  }
}

interface RowProps {
  icon: LucideIcon;
  label: string;
  value: string;
  to?: string;
  onClick?: () => void;
  title?: string;
}

function OverviewRow({
  icon: Icon,
  label,
  value,
  to,
  onClick,
  title,
}: RowProps) {
  const content = (
    <>
      <span className="ov-row-icon" aria-hidden="true">
        <Icon size={16} />
      </span>
      <span className="ov-label">{label}</span>
      <span className="ov-value">{value}</span>
      <span className="ov-chevron" aria-hidden="true">
        <ChevronRight size={14} />
      </span>
    </>
  );
  if (to) {
    return (
      <Link className="ov-row" title={title ?? label} to={to}>
        {content}
      </Link>
    );
  }
  return (
    <button
      className="ov-row"
      onClick={onClick}
      title={title ?? label}
      type="button"
    >
      {content}
    </button>
  );
}

function SummaryTab() {
  const { dateKey } = useLocalCalendar();
  const healthQuery = useHealthQuery();
  const backupsQuery = useBackupsQuery();
  const todayStatsQuery = useTodayStatsQuery(dateKey);
  const inboxStatsQuery = useInboxStatsQuery();
  const nextReminderQuery = useRemindersQuery({
    page: 1,
    pageSize: 1,
    sort: "trigger_at",
    status: "scheduled",
  });
  const providersQuery = useAiProvidersQuery();
  const knowledgeCountQuery = useQuery({
    queryKey: ["knowledge", "sources", "count"],
    queryFn: () => getKnowledgeSources({ pageSize: 1 }),
    retry: 1,
    staleTime: 30_000,
  });
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);

  const health = healthQuery.data;
  const latestBackup = backupsQuery.data?.[0];
  const tasks = todayStatsQuery.data?.tasks;
  const focus = todayStatsQuery.data?.focus;
  const inbox = inboxStatsQuery.data;
  const nextReminder = nextReminderQuery.data?.items[0];

  return (
    <>
      <section className="ov-group">
        <div className="ov-group-title">本地运行</div>
        <OverviewRow
          icon={Cpu}
          label="版本"
          value={
            health
              ? `v${health.app.version} · API ${health.api.version} · schema ${health.schema.version}`
              : healthQuery.isError
                ? "读取失败"
                : "读取中"
          }
          onClick={() => setSettingsOpen(true, "about")}
        />
        <OverviewRow
          icon={DatabaseBackup}
          label="最近备份"
          value={
            backupsQuery.isError
              ? "读取失败"
              : latestBackup
                ? relativeLabel(latestBackup.createdAt, Date.now())
                : "暂无备份"
          }
          onClick={() => setSettingsOpen(true, "data")}
        />
      </section>
      <section className="ov-group">
        <div className="ov-group-title">今日</div>
        <OverviewRow
          icon={CheckSquare2}
          label="任务"
          value={
            todayStatsQuery.isError
              ? "读取失败"
              : tasks
                ? `${tasks.completed}/${tasks.total} · 逾期${tasks.overdue}`
                : "读取中"
          }
          to="/today"
        />
        <OverviewRow
          icon={TimerReset}
          label="专注"
          value={
            todayStatsQuery.isError
              ? "读取失败"
              : focus
                ? `${focus.sessions} 段 · ${focus.minutes} 分`
                : "读取中"
          }
          to="/focus"
        />
      </section>
      <section className="ov-group">
        <div className="ov-group-title">收件箱</div>
        <OverviewRow
          icon={Inbox}
          label="待处理 / 未读"
          value={
            inboxStatsQuery.isError
              ? "读取失败"
              : inbox
                ? `${inbox.pending} / ${inbox.unread}`
                : "读取中"
          }
          to="/inbox"
        />
        <OverviewRow
          icon={BellRing}
          label="下一提醒"
          value={
            nextReminderQuery.isError
              ? "读取失败"
              : nextReminder
                ? formatTrigger(nextReminder.triggerAt)
                : "无排程"
          }
          onClick={() => setSettingsOpen(true, "automation")}
        />
      </section>
      <section className="ov-group">
        <div className="ov-group-title">来源</div>
        <OverviewRow
          icon={BookOpenText}
          label="知识库"
          value={
            knowledgeCountQuery.isError
              ? "读取失败"
              : `${knowledgeCountQuery.data?.meta.total ?? 0} 个来源`
          }
          to="/knowledge"
        />
        <OverviewRow
          icon={Sparkles}
          label="AI Provider"
          value={
            providersQuery.isError
              ? "读取失败"
              : `${providersQuery.data?.length ?? 0} 已配置`
          }
          to="/ai"
        />
      </section>
    </>
  );
}

function AgentRunDetail({
  run,
  onBack,
}: {
  run: AgentRunSummary;
  onBack: () => void;
}) {
  const queryClient = useQueryClient();
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const retry = useMutation({
    mutationFn: () => retryAgentRun(run.id),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["agent-runs"] }),
  });
  const cancel = useMutation({
    mutationFn: () => cancelAgentRun(run.id),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["agent-runs"] }),
  });
  const terminal =
    run.status === "succeeded" ||
    run.status === "failed" ||
    run.status === "cancelled" ||
    run.status === "interrupted";

  return (
    <div className="ov-detail">
      <button className="ov-back" onClick={onBack} type="button">
        <ArrowLeft size={14} /> 返回列表
      </button>
      <div className="ov-detail-title" title={run.taskTitle}>
        {run.taskTitle}
      </div>
      <div className="ov-detail-meta">
        状态 {run.status} · 尝试 {run.attempt} · 模型 {run.model}
      </div>
      <div className="ov-detail-meta">
        开始 {run.startedAt ?? "—"} · 结束 {run.completedAt ?? "—"}
      </div>
      {run.errorCode ? (
        <div className="ov-detail-error">错误：{run.errorCode}</div>
      ) : null}
      {run.resultText ? (
        <pre className="ov-detail-result">{run.resultText}</pre>
      ) : (
        <div className="ov-detail-meta">暂无产出文本</div>
      )}
      <div className="ov-detail-actions">
        <button
          className="button button-secondary"
          onClick={() => setTaskDetailId(run.taskId)}
          type="button"
        >
          打开任务
        </button>
        {terminal ? (
          <button
            className="button button-secondary"
            disabled={retry.isPending}
            onClick={() => retry.mutate()}
            type="button"
          >
            重试
          </button>
        ) : null}
        {run.status === "queued" ? (
          <button
            className="button button-secondary"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate()}
            type="button"
          >
            取消
          </button>
        ) : null}
      </div>
    </div>
  );
}

function AgentsTab() {
  const runsQuery = useAgentRunsQuery({ pageSize: 50 });
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const runs = runsQuery.data?.items ?? [];
  const selected = runs.find((run) => run.id === selectedId) ?? null;

  if (selected) {
    return <AgentRunDetail run={selected} onBack={() => setSelectedId(null)} />;
  }

  return (
    <section className="ov-group">
      <div className="ov-group-title">子智能体（Agent Runs）</div>
      {runsQuery.isPending ? (
        <div className="ov-empty">正在读取执行记录…</div>
      ) : runsQuery.isError ? (
        <div className="ov-empty">
          读取失败 ·{" "}
          <button
            className="form-inline-action"
            onClick={() => void runsQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : runs.length === 0 ? (
        <div className="ov-empty">暂无 Agent 执行记录</div>
      ) : (
        runs.map((run) => (
          <button
            className="ov-row"
            key={run.id}
            onClick={() => setSelectedId(run.id)}
            type="button"
          >
            <span className="ov-row-icon" aria-hidden="true">
              <Bot size={14} />
            </span>
            <span className="ov-label" title={run.taskTitle}>
              {run.taskTitle}
            </span>
            <span className="ov-value">{run.status}</span>
          </button>
        ))
      )}
    </section>
  );
}

const fileScopes: { id: ControlledFileScope; label: string }[] = [
  { id: "artifact", label: "任务产出" },
  { id: "client_attachment", label: "客户附件" },
  { id: "project_attachment", label: "项目附件" },
  { id: "knowledge_document", label: "知识库" },
];

function FilesTab() {
  const [scope, setScope] = useState<ControlledFileScope>("artifact");
  const [selected, setSelected] = useState<ControlledFile | null>(null);
  const filesQuery = useControlledFilesQuery({ pageSize: 50, scope });
  const artifactQuery = useTaskArtifactQuery(
    selected?.scope === "artifact" ? selected.id : null,
  );
  const files = filesQuery.data?.items ?? [];

  return (
    <>
      <div className="ov-scope-bar">
        {fileScopes.map((item) => (
          <button
            className={`ov-scope${scope === item.id ? " ov-scope-active" : ""}`}
            key={item.id}
            onClick={() => {
              setScope(item.id);
              setSelected(null);
            }}
            type="button"
          >
            {item.label}
          </button>
        ))}
      </div>
      <section className="ov-group">
        <div className="ov-group-title">文件（只读）</div>
        {filesQuery.isPending ? (
          <div className="ov-empty">正在读取文件…</div>
        ) : filesQuery.isError ? (
          <div className="ov-empty">
            读取失败 ·{" "}
            <button
              className="form-inline-action"
              onClick={() => void filesQuery.refetch()}
              type="button"
            >
              重试
            </button>
          </div>
        ) : files.length === 0 ? (
          <div className="ov-empty">该范围暂无文件</div>
        ) : (
          files.map((file) => (
            <button
              className="ov-row"
              key={file.id}
              onClick={() => setSelected(file)}
              type="button"
            >
              <span className="ov-row-icon" aria-hidden="true">
                <FileText size={14} />
              </span>
              <span className="ov-label" title={file.name}>
                {file.name}
              </span>
              <span className="ov-value">{file.ownerLabel}</span>
            </button>
          ))
        )}
      </section>
      {selected ? (
        <div className="ov-detail">
          <button
            className="ov-back"
            onClick={() => setSelected(null)}
            type="button"
          >
            <ArrowLeft size={14} /> 返回列表
          </button>
          <div className="ov-detail-title" title={selected.name}>
            {selected.name}
          </div>
          <div className="ov-detail-meta">
            范围 {selected.scope} · 归属 {selected.ownerLabel || "—"}
          </div>
          <div className="ov-detail-meta">
            类型 {selected.mimeType ?? "—"} · 大小 {selected.sizeBytes ?? "—"}{" "}
            字节
          </div>
          <div className="ov-detail-meta">SHA-256 {selected.sha256 ?? "—"}</div>
          {selected.scope === "artifact" ? (
            artifactQuery.isPending ? (
              <div className="ov-empty">正在读取产出…</div>
            ) : artifactQuery.data?.contentText ? (
              <pre className="ov-detail-result">
                {artifactQuery.data.contentText}
              </pre>
            ) : (
              <div className="ov-empty">该产出不含可内联预览的文本</div>
            )
          ) : (
            <div className="ov-empty">该类型暂不支持内联预览，仅显示元数据</div>
          )}
        </div>
      ) : null}
    </>
  );
}

function BrowserTab() {
  const [address, setAddress] = useState("http://127.0.0.1:5173");
  const [target, setTarget] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const submit = () => {
    const raw = address.trim();
    if (!raw) return;
    setNotice(null);
    if (isLoopbackUrl(raw)) {
      setTarget(raw);
      return;
    }
    setTarget(null);
    void openExternalBrowserWindow(raw).then((opened) => {
      if (opened) {
        setNotice("已在独立原生窗口打开外部页面");
      } else {
        window.open(raw, "_blank", "noopener");
        setNotice("当前为浏览器开发模式，已在新标签打开");
      }
    });
  };

  return (
    <section className="ov-group">
      <div className="ov-group-title">内置浏览器</div>
      <div className="ov-browser-bar">
        <input
          className="ov-browser-input"
          onChange={(event) => setAddress(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") submit();
          }}
          placeholder="http://127.0.0.1:5173"
          value={address}
        />
        <button
          aria-label="打开地址"
          className="icon-button"
          onClick={submit}
          title="打开地址"
          type="button"
        >
          <ExternalLink size={14} />
        </button>
      </div>
      {notice ? <div className="ov-empty">{notice}</div> : null}
      <div className="ov-empty">
        仅回环地址（127.0.0.1 /
        localhost）在面板内内嵌；外部页面在桌面端以独立原生窗口打开。
      </div>
      {target ? (
        <iframe
          className="ov-browser-frame"
          src={target}
          title="内置浏览器预览"
        />
      ) : null}
    </section>
  );
}

const tabs: { id: RightPanelTab; label: string }[] = [
  { id: "summary", label: "概要" },
  { id: "agents", label: "子智能体" },
  { id: "files", label: "文件" },
  { id: "browser", label: "浏览器" },
];

export function RightOverview() {
  const activeTab = useUiStore((state) => state.rightPanelTab);
  const setRightPanelTab = useUiStore((state) => state.setRightPanelTab);

  return (
    <aside aria-label="今日概览" className="right-sidebar" id="right-overview">
      <div className="ov-tabs" role="tablist" aria-label="概览页签">
        {tabs.map(({ id, label }) => (
          <button
            aria-selected={activeTab === id}
            className={`ov-tab${activeTab === id ? " ov-tab-active" : ""}`}
            key={id}
            onClick={() => setRightPanelTab(id)}
            role="tab"
            type="button"
          >
            {label}
          </button>
        ))}
      </div>
      <div className="ov-tab-panel">
        {activeTab === "summary" ? (
          <SummaryTab />
        ) : activeTab === "agents" ? (
          <AgentsTab />
        ) : activeTab === "files" ? (
          <FilesTab />
        ) : (
          <BrowserTab />
        )}
      </div>
    </aside>
  );
}
