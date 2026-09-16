import {
  PanelLeftClose,
  CalendarClock,
  FolderKanban,
  Search,
  Settings2,
  SquarePen,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  useAiSessionsQuery,
  useCreateAiSession,
  useDeleteAiSession,
} from "../api/hooks";
import { useAiChatStore } from "../store/aiChat";
import { useUiStore } from "../store/ui";
import type { AiSession } from "../types/models";
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
  const createSession = useCreateAiSession();
  const deleteSession = useDeleteAiSession();
  const activeSessionId = useAiChatStore((state) => state.activeSessionId);
  const setActiveSessionId = useAiChatStore(
    (state) => state.setActiveSessionId,
  );
  const sessionFilter = useAiChatStore((state) => state.sessionFilter);
  const setSessionFilter = useAiChatStore((state) => state.setSessionFilter);
  const isStreaming = useAiChatStore((state) => state.streaming !== null);
  const [deletingSession, setDeletingSession] = useState<string | null>(null);
  const navigate = useNavigate();
  const setSettingsOpen = useUiStore((state) => state.setSettingsOpen);
  const toggleAgentRailCollapsed = useUiStore(
    (state) => state.toggleAgentRailCollapsed,
  );

  const sessionList = useMemo(() => {
    const all = sessions.data ?? [];
    const keyword = sessionFilter.trim().toLowerCase();
    const filtered = keyword
      ? all.filter((session) => session.title.toLowerCase().includes(keyword))
      : all;
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
  }, [sessions.data, sessionFilter]);

  return (
    <aside aria-label="会话列表" className="ai-session-rail" id="agent-sidebar">
      <div className="ai-rail-head">
        <span className="ai-rail-head-label">会话</span>
        <button
          aria-controls="agent-sidebar"
          aria-expanded="true"
          aria-label="隐藏会话侧边栏"
          className="icon-button ai-rail-collapse"
          onClick={toggleAgentRailCollapsed}
          title="隐藏会话侧边栏"
          type="button"
        >
          <PanelLeftClose aria-hidden="true" size={16} />
        </button>
      </div>
      <button
        className="ai-rail-primary"
        disabled={createSession.isPending || isStreaming}
        onClick={() => {
          void createSession
            .mutateAsync()
            .then((session) => setActiveSessionId(session.id))
            .catch(() => {
              // createSession.error renders the message below
            });
        }}
        type="button"
      >
        <SquarePen aria-hidden="true" size={16} />
        <span>新会话</span>
      </button>
      {createSession.error ? (
        <div className="ai-session-create-error" role="alert">
          无法新建会话，请重试
        </div>
      ) : null}
      <nav aria-label="智能体快捷入口" className="ai-rail-actions">
        <button
          onClick={() => setSettingsOpen(true, "automation")}
          type="button"
        >
          <CalendarClock aria-hidden="true" size={16} />
          <span>定时任务</span>
        </button>
        <button onClick={() => navigate("/projects")} type="button">
          <FolderKanban aria-hidden="true" size={16} />
          <span>项目</span>
        </button>
      </nav>
      <div className="ai-rail-search">
        <Search size={14} />
        <input
          aria-label="搜索会话"
          onChange={(event) => setSessionFilter(event.target.value)}
          placeholder="搜索会话"
          value={sessionFilter}
        />
      </div>
      <div className="ai-rail-section">对话</div>
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
          {sessionFilter
            ? "没有匹配的会话。"
            : "还没有会话，发送第一条消息开始。"}
        </p>
      ) : (
        sessionList.map((group) => (
          <div key={group.label}>
            <div className="ai-rail-group-label">{group.label}</div>
            {group.sessions.map((session) => (
              <div
                className="ai-session-row"
                data-active={session.id === activeSessionId}
                key={session.id}
              >
                <button
                  disabled={isStreaming}
                  onClick={() => setActiveSessionId(session.id)}
                  title={session.title}
                  type="button"
                >
                  <span className="ai-session-title">{session.title}</span>
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

      <button
        className="ai-rail-settings"
        onClick={() => setSettingsOpen(true, "ai")}
        type="button"
      >
        <Settings2 aria-hidden="true" size={16} />
        <span>AI 助手设置</span>
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
