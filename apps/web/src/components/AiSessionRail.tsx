import {
  PanelLeftClose,
  CalendarClock,
  FolderKanban,
  ListTodo,
  Settings2,
  SquarePen,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  useAiAgentInboxQuery,
  useAiSessionsQuery,
  useAiWorkPlanInboxQuery,
  useAgentRunsQuery,
  useCreateAiSession,
  useDeleteAiSession,
  useRestoreDiagnosticsQuery,
} from "../api/hooks";
import { useAiChatStore } from "../store/aiChat";
import {
  outstandingFileOperationReminders,
  useFileOperationReminders,
} from "../store/fileOperationReminders";
import { useSettingsStore } from "../store/settings";
import { useUiStore } from "../store/ui";
import type { AiSession } from "../types/models";
import { AiAgentInboxPanel } from "./AiAgentInboxPanel";
import { ErrorState, LoadingState } from "./feedback";
import { Modal } from "./Modal";

interface SessionGroup {
  label: string;
  sessions: AiSession[];
}

function sessionBucket(updatedAt: string): "today" | "yesterday" | "earlier" {
  const date = new Date(updatedAt);
  if (Number.isNaN(date.getTime())) return "earlier";
  const now = new Date();
  const startOfToday = new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
  ).getTime();
  if (date.getTime() >= startOfToday) return "today";
  if (date.getTime() >= startOfToday - 86_400_000) return "yesterday";
  return "earlier";
}

function sessionTimeLabel(updatedAt: string): string {
  const date = new Date(updatedAt);
  if (Number.isNaN(date.getTime())) return "";
  const bucket = sessionBucket(updatedAt);
  if (bucket === "yesterday") return "昨天";
  if (bucket === "earlier") {
    return `${date.getMonth() + 1}/${date.getDate()}`;
  }
  return date.toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

/**
 * Conversation list for agent mode. It owns the query, creation and deletion
 * so the shell can host it, while the selected session lives in the shared AI
 * chat store because the conversation pane reads the same value.
 */
export function AiSessionRail() {
  const sessions = useAiSessionsQuery();
  const planInbox = useAiWorkPlanInboxQuery();
  const agentInbox = useAiAgentInboxQuery();
  const runInbox = useAgentRunsQuery({
    pageSize: 50,
    attentionOnly: true,
    unplannedOnly: true,
  });
  const restoreDiagnostics = useRestoreDiagnosticsQuery();
  const createSession = useCreateAiSession();
  const deleteSession = useDeleteAiSession();
  const activeSessionId = useAiChatStore((state) => state.activeSessionId);
  const setActiveSessionId = useAiChatStore(
    (state) => state.setActiveSessionId,
  );
  const isStreaming = useAiChatStore((state) => state.streaming !== null);
  const [deletingSession, setDeletingSession] = useState<string | null>(null);
  const showPlanInbox = useUiStore((state) => state.aiInboxOpen);
  const setShowPlanInbox = useUiStore((state) => state.setAiInboxOpen);
  const navigate = useNavigate();
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const toggleAgentRailCollapsed = useUiStore(
    (state) => state.toggleAgentRailCollapsed,
  );
  const displayName = useSettingsStore(
    (state) => state.preview?.profile.displayName ?? state.displayName,
  );
  const avatarDataUrl = useSettingsStore(
    (state) => state.preview?.profile.avatarDataUrl ?? state.avatarDataUrl,
  );

  const sessionList = useMemo(() => {
    const filtered = sessions.data ?? [];
    const buckets: Record<string, AiSession[]> = {
      today: [],
      yesterday: [],
      earlier: [],
    };
    for (const session of filtered) {
      buckets[sessionBucket(session.updated_at)].push(session);
    }
    const groups: SessionGroup[] = [];
    if (buckets.today.length > 0) {
      groups.push({ label: "今天", sessions: buckets.today });
    }
    if (buckets.yesterday.length > 0) {
      groups.push({ label: "昨天", sessions: buckets.yesterday });
    }
    if (buckets.earlier.length > 0) {
      groups.push({ label: "更早", sessions: buckets.earlier });
    }
    return groups;
  }, [sessions.data]);
  const planInboxMeta = planInbox.data?.pages[0]?.meta;
  const runInboxTotal = runInbox.data?.meta.total ?? 0;
  const agentInboxMeta = agentInbox.data?.pages[0]?.meta;
  const recoveryAttention = Boolean(
    restoreDiagnostics.data &&
    (restoreDiagnostics.data.restartRequired ||
      restoreDiagnostics.data.cleanupRequired ||
      restoreDiagnostics.data.attentionRequired),
  );
  const fileReminderTotal = useFileOperationReminders(
    (state) => outstandingFileOperationReminders(state.reminders).length,
  );
  const attentionTotal =
    (planInboxMeta?.attention_total ?? 0) +
    runInboxTotal +
    fileReminderTotal +
    Math.max(
      0,
      (agentInboxMeta?.total ?? 0) - (agentInboxMeta?.agent_run_total ?? 0),
    ) +
    (recoveryAttention ? 1 : 0);

  return (
    <aside aria-label="会话列表" className="ai-session-rail" id="agent-sidebar">
      <div className="brand-block">
        <div className="brand-mark">
          {avatarDataUrl ? (
            <img alt={`${displayName}的头像`} src={avatarDataUrl} />
          ) : (
            Array.from(displayName.trim())[0]?.toUpperCase() || "O"
          )}
        </div>
        <div className="sidebar-copy min-w-0">
          <div className="brand-name" title={displayName}>
            {displayName}
          </div>
        </div>
        <button
          aria-controls="agent-sidebar"
          aria-expanded="true"
          aria-label="隐藏会话侧边栏"
          className="icon-button sidebar-collapse-button"
          onClick={toggleAgentRailCollapsed}
          title="隐藏会话侧边栏"
          type="button"
        >
          <PanelLeftClose aria-hidden="true" size={17} />
        </button>
      </div>
      <button
        className="nav-item"
        disabled={createSession.isPending || isStreaming}
        onClick={() => {
          setShowPlanInbox(false);
          void createSession
            .mutateAsync()
            .then((session) => setActiveSessionId(session.id))
            .catch(() => {
              // createSession.error renders the message below
            });
        }}
        type="button"
      >
        <SquarePen aria-hidden="true" className="nav-icon" size={17} />
        <span className="nav-text">新会话</span>
      </button>
      {createSession.error ? (
        <div className="ai-session-create-error" role="alert">
          无法新建会话，请重试
        </div>
      ) : null}
      <nav aria-label="智能体快捷入口" className="ai-rail-actions">
        <button
          className="nav-item"
          onClick={() => setSettingsOpen(true, "automation")}
          type="button"
        >
          <CalendarClock aria-hidden="true" className="nav-icon" size={17} />
          <span className="nav-text">定时任务</span>
        </button>
        <button
          className="nav-item"
          onClick={() => navigate("/projects")}
          type="button"
        >
          <FolderKanban aria-hidden="true" className="nav-icon" size={17} />
          <span className="nav-text">项目</span>
        </button>
        <button
          aria-pressed={showPlanInbox}
          className={`nav-item${showPlanInbox ? " nav-item-active" : ""}`}
          onClick={() => setShowPlanInbox(!showPlanInbox)}
          type="button"
        >
          <ListTodo aria-hidden="true" className="nav-icon" size={17} />
          <span className="nav-text">续办队列</span>
          {attentionTotal > 0 ? (
            <span className="nav-badge">
              {attentionTotal > 99 ? "99+" : attentionTotal}
            </span>
          ) : null}
        </button>
      </nav>
      {showPlanInbox ? (
        <AiAgentInboxPanel
          agentInbox={agentInbox}
          isStreaming={isStreaming}
          onClose={() => setShowPlanInbox(false)}
          planInbox={planInbox}
          restoreDiagnostics={restoreDiagnostics}
          runInbox={runInbox}
        />
      ) : (
        <>
          <div className="nav-label">对话</div>
          <div className="ai-rail-sessions">
            {sessions.isPending ? (
              <LoadingState label="正在读取会话…" />
            ) : sessions.isError ? (
              <ErrorState
                compact
                message="无法读取会话列表"
                onRetry={() => void sessions.refetch()}
              />
            ) : sessionList.length === 0 ? (
              <p className="ai-session-empty">
                还没有会话，发送第一条消息开始。
              </p>
            ) : (
              sessionList.map((group) => (
                <div key={group.label}>
                  <div className="nav-label">{group.label}</div>
                  {group.sessions.map((session) => (
                    <div
                      className="ai-session-row"
                      data-active={session.id === activeSessionId}
                      key={session.id}
                    >
                      <button
                        aria-current={
                          session.id === activeSessionId ? "page" : undefined
                        }
                        disabled={isStreaming}
                        onClick={() => setActiveSessionId(session.id)}
                        title={session.title}
                        type="button"
                      >
                        <span className="ai-session-title">
                          {session.title}
                        </span>
                        <span className="ai-session-time">
                          {sessionTimeLabel(session.updated_at)}
                        </span>
                      </button>
                      <button
                        aria-label={`删除会话 ${session.title}`}
                        className="ai-session-delete"
                        disabled={isStreaming}
                        onClick={() => setDeletingSession(session.id)}
                        type="button"
                      >
                        <Trash2 size={13} />
                      </button>
                    </div>
                  ))}
                </div>
              ))
            )}
          </div>
        </>
      )}

      <button
        className="nav-item sidebar-settings"
        onClick={() => setSettingsOpen(true, "ai")}
        type="button"
      >
        <Settings2 aria-hidden="true" className="nav-icon" size={17} />
        <span className="nav-text">AI 助手设置</span>
      </button>

      <Modal
        footer={
          <>
            <button
              className="button button-secondary"
              onClick={() => setDeletingSession(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              onClick={() => {
                const sessionId = deletingSession;
                if (!sessionId) return;
                void deleteSession
                  .mutateAsync({
                    id: sessionId,
                    expectedVersion:
                      sessions.data?.find((session) => session.id === sessionId)
                        ?.version ?? 1,
                  })
                  .then(() => {
                    useAiChatStore.getState().forgetSession(sessionId);
                    setDeletingSession(null);
                  })
                  .catch(() => {
                    // Mutation state renders the conflict/failure below.
                  });
              }}
              type="button"
            >
              删除
            </button>
          </>
        }
        onClose={() => setDeletingSession(null)}
        open={deletingSession !== null}
        title="删除会话"
        width="420px"
      >
        <p>删除后该会话的全部本地消息不可恢复。确定删除？</p>
        {deleteSession.error ? (
          <p className="ai-task-card-error" role="alert">
            会话删除失败，已刷新最新状态，请重试。
          </p>
        ) : null}
      </Modal>
    </aside>
  );
}
