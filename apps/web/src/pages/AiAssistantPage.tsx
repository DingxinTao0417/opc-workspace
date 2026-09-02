import {
  AlertCircle,
  Brain,
  CalendarDays,
  CheckCircle2,
  FileText,
  Lightbulb,
  ListChecks,
  LoaderCircle,
  Plus,
  Search,
  Send,
  Sparkles,
  Square,
  Trash2,
} from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  useAiChatStream,
  useAiMessagesInfiniteQuery,
  useAiProvidersQuery,
  useAiSessionsQuery,
  useAttachTaskToAiMessage,
  useCreateAiSession,
  useCreateTask,
  useDeleteAiSession,
  useTaskQuery,
} from "../api/hooks";
import { ErrorState, LoadingState } from "../components/feedback";
import { Modal } from "../components/Modal";
import { ProjectSelect } from "../components/ProjectSelect";
import {
  parseAiTaskSuggestion,
  stripAiTaskBlock,
  type AiTaskSuggestion,
} from "../lib/aiTaskCard";
import { useUiStore } from "../store/ui";
import type { AiMessage, AiSession, TaskStatus } from "../types/models";

interface PendingTaskCard {
  messageId: string;
  suggestion: AiTaskSuggestion;
}

interface DraftTaskForm {
  title: string;
  description: string;
  dueDate: string;
  projectId: string | null;
}

interface SessionGroup {
  label: string;
  sessions: AiSession[];
}

// 极简只读 markdown 渲染：段落、有序/无序列表、**加粗**、`code`。
// 只生成 React 节点（无 dangerouslySetInnerHTML），模型输出天然被转义。
function inlineMarkdown(text: string, keyPrefix: string): ReactNode[] {
  return text
    .split(/(\*\*[^*]+\*\*|`[^`]+`)/g)
    .filter((part) => part !== "")
    .map((part, index) => {
      const key = `${keyPrefix}-${index}`;
      if (part.startsWith("**") && part.endsWith("**") && part.length > 4) {
        return <strong key={key}>{part.slice(2, -2)}</strong>;
      }
      if (part.startsWith("`") && part.endsWith("`") && part.length > 2) {
        return <code key={key}>{part.slice(1, -1)}</code>;
      }
      return <span key={key}>{part}</span>;
    });
}

function renderAiRichText(content: string): ReactNode[] {
  const lines = content.split(/\r?\n/);
  const blocks: ReactNode[] = [];
  let paragraph: string[] = [];
  let list: { ordered: boolean; items: string[] } | null = null;
  let blockKey = 0;

  const flushParagraph = () => {
    if (paragraph.length > 0) {
      const key = `p-${blockKey++}`;
      blocks.push(<p key={key}>{inlineMarkdown(paragraph.join(" "), key)}</p>);
      paragraph = [];
    }
  };
  const flushList = () => {
    if (list) {
      const key = `l-${blockKey++}`;
      const items = list.items.map((item, index) => (
        <li key={`${key}-${index}`}>
          {inlineMarkdown(item, `${key}-${index}`)}
        </li>
      ));
      blocks.push(
        list.ordered ? <ol key={key}>{items}</ol> : <ul key={key}>{items}</ul>,
      );
      list = null;
    }
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
    const ordered = /^(\d+)[.、)]\s+/.exec(line);
    const unordered = /^[-*•]\s+/.exec(line);
    if (!line.trim()) {
      flushParagraph();
      flushList();
      continue;
    }
    if (ordered) {
      flushParagraph();
      if (!list || !list.ordered) {
        flushList();
        list = { ordered: true, items: [] };
      }
      list.items.push(line.slice(ordered[0].length));
      continue;
    }
    if (unordered) {
      flushParagraph();
      if (list && list.ordered) {
        flushList();
      }
      if (!list) {
        list = { ordered: false, items: [] };
      }
      list.items.push(line.slice(unordered[0].length));
      continue;
    }
    flushList();
    paragraph.push(line.trim());
  }
  flushParagraph();
  flushList();
  return blocks;
}

function draftFromSuggestion(suggestion: AiTaskSuggestion): DraftTaskForm {
  return {
    title: suggestion.title,
    description: suggestion.description ?? "",
    dueDate: suggestion.due ?? "",
    projectId: null,
  };
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

function messageTimeLabel(createdAt: string): string {
  const date = new Date(createdAt);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function greeting(): string {
  const hour = new Date().getHours();
  if (hour < 6) return "夜深了";
  if (hour < 12) return "早上好";
  if (hour < 18) return "下午好";
  return "晚上好";
}

const SUGGESTIONS: Array<{
  icon: typeof ListChecks;
  title: string;
  sub: string;
  prompt: string;
}> = [
  {
    icon: ListChecks,
    title: "梳理今日任务",
    sub: "把今天的待办按优先级排个处理顺序",
    prompt: "帮我梳理今天的任务，按优先级给出处理顺序建议。",
  },
  {
    icon: FileText,
    title: "起草周报",
    sub: "把本周要做的事整理成周报框架",
    prompt: "帮我起草一份本周周报的框架，按项目分类汇总进展。",
  },
  {
    icon: CalendarDays,
    title: "拆解计划",
    sub: "把一个模糊目标拆成可执行任务",
    prompt: "我有一个目标想推进，帮我把它拆解成可执行的任务清单。",
  },
  {
    icon: Lightbulb,
    title: "头脑风暴",
    sub: "换个角度为想法找突破口",
    prompt: "和我头脑风暴一下，帮我为一个想法找到三个不同的切入角度。",
  },
];

const statusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "已阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

export function AiAssistantPage() {
  const navigate = useNavigate();
  const providers = useAiProvidersQuery();
  const sessions = useAiSessionsQuery();
  const createSession = useCreateAiSession();
  const deleteSession = useDeleteAiSession();
  const createTask = useCreateTask();
  const chat = useAiChatStream();
  const setSettingsOpen = useUiStore((store) => store.setSettingsOpen);

  const [activeSessionId, setActiveSessionId] = useState("");
  const [selectedProviderId, setSelectedProviderId] = useState("");
  const [input, setInput] = useState("");
  const [sessionFilter, setSessionFilter] = useState("");
  const [pendingCard, setPendingCard] = useState<PendingTaskCard | null>(null);
  const [draft, setDraft] = useState<DraftTaskForm | null>(null);
  const [taskError, setTaskError] = useState<string | null>(null);
  const [deletingSession, setDeletingSession] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const readyProviders =
    providers.data?.filter(
      (provider) => provider.status === "ready" && provider.has_key,
    ) ?? [];
  const activeProvider =
    readyProviders.find((provider) => provider.id === selectedProviderId) ??
    null;

  useEffect(() => {
    if (
      readyProviders.length > 0 &&
      !readyProviders.some((provider) => provider.id === selectedProviderId)
    ) {
      setSelectedProviderId(readyProviders[0].id);
    }
  }, [readyProviders, selectedProviderId]);

  const messages = useAiMessagesInfiniteQuery(
    activeSessionId,
    activeSessionId !== "",
  );
  const attachTask = useAttachTaskToAiMessage(activeSessionId);

  useEffect(() => {
    if (!activeSessionId && sessions.data && sessions.data.length > 0) {
      setActiveSessionId(sessions.data[0].id);
    }
  }, [activeSessionId, sessions.data]);

  useEffect(() => {
    const node = scrollRef.current;
    if (!node || typeof node.scrollTo !== "function") return;
    node.scrollTo({ top: node.scrollHeight });
  }, [messages.data?.pages, chat.streaming?.text]);

  const streamedText = chat.streaming?.text ?? "";
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

  const activeSession = sessions.data?.find(
    (session) => session.id === activeSessionId,
  );
  const hasMessages =
    !!messages.data && messages.data.pages.some((page) => page.data.length > 0);

  function openTaskCard(message: AiMessage) {
    if (message.role !== "assistant") return;
    const suggestion = parseAiTaskSuggestion(message.content);
    if (!suggestion) return;
    setPendingCard({ messageId: message.id, suggestion });
    setDraft(draftFromSuggestion(suggestion));
    setTaskError(null);
  }

  async function confirmCreateTask() {
    if (!pendingCard || !draft || !draft.title.trim()) return;
    setTaskError(null);
    try {
      const task = await createTask.mutateAsync({
        title: draft.title.trim(),
        description: draft.description.trim() || undefined,
        priority: "P2",
        projectId: draft.projectId ?? undefined,
        dueDate: draft.dueDate ? draft.dueDate : null,
      });
      await attachTask.mutateAsync({
        messageId: pendingCard.messageId,
        taskId: task.id,
      });
      setPendingCard(null);
      setDraft(null);
    } catch (error) {
      setTaskError(
        error instanceof Error && error.message
          ? `任务创建失败：${error.message}`
          : "任务创建失败，请重试",
      );
    }
  }

  async function sendMessage() {
    const message = input.trim();
    if (!activeProvider || !message || chat.isStreaming) return;
    setInput("");
    const outcome = await chat.send({
      providerId: activeProvider.id,
      sessionId: activeSessionId || undefined,
      message,
    });
    if (outcome.sessionId && outcome.sessionId !== activeSessionId) {
      setActiveSessionId(outcome.sessionId);
    }
  }

  if (providers.isPending) {
    return (
      <div className="page ai-page">
        <LoadingState label="正在读取 AI 助手配置…" />
      </div>
    );
  }
  if (providers.isError || !providers.data) {
    return (
      <div className="page ai-page">
        <ErrorState
          onRetry={() => void providers.refetch()}
          title="AI 助手不可用"
          message="无法读取 AI 供应商配置，本地核心功能不受影响。可点击重试。"
        />
      </div>
    );
  }

  const providerError =
    providers.data.length === 0
      ? "尚未配置 AI 供应商。请到 设置 → AI 助手 登记 API key 供应商并保存密钥。"
      : readyProviders.length === 0
        ? "AI 供应商未就绪。请到 设置 → AI 助手 保存 API 密钥并完成连接测试。"
        : null;

  return (
    <div className="page ai-page">
      {providerError ? (
        <div className="ai-provider-banner" role="status">
          <AlertCircle size={15} />
          <span>{providerError}</span>
          <button
            className="button button-secondary"
            onClick={() => setSettingsOpen(true, "ai")}
            type="button"
          >
            打开设置
          </button>
        </div>
      ) : null}

      <div className="ai-layout">
        <aside className="ai-session-rail" aria-label="会话列表">
          <button
            className="ai-new-chat-btn"
            disabled={createSession.isPending}
            onClick={() => {
              setPendingCard(null);
              setDraft(null);
              void createSession.mutateAsync().then((session) => {
                setActiveSessionId(session.id);
              });
            }}
            type="button"
          >
            <Plus size={15} />
            新会话
          </button>
          <div className="ai-rail-search">
            <Search size={14} />
            <input
              aria-label="搜索会话"
              onChange={(event) => setSessionFilter(event.target.value)}
              placeholder="搜索会话"
              value={sessionFilter}
            />
          </div>
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
                      onClick={() => {
                        setActiveSessionId(session.id);
                        setPendingCard(null);
                        setDraft(null);
                      }}
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
        </aside>

        <section className="ai-chat-main">
          <header className="ai-chat-header">
            <div className="min-w-0">
              <div className="ai-chat-header-title">
                {activeSession?.title ?? "新会话"}
              </div>
              <div className="ai-chat-header-sub">
                {activeProvider
                  ? `${activeProvider.name} · ${activeProvider.model}`
                  : "未配置供应商"}
              </div>
            </div>
          </header>

          <div className="ai-chat-messages" ref={scrollRef}>
            <div className="ai-chat-messages-inner">
              {activeSessionId && messages.isPending ? (
                <LoadingState label="正在读取消息…" />
              ) : activeSessionId && messages.isError ? (
                <ErrorState
                  compact
                  message="无法读取会话消息"
                  onRetry={() => void messages.refetch()}
                />
              ) : !activeSessionId || !hasMessages ? (
                <div className="ai-hero">
                  <div className="ai-hero-avatar">
                    <Sparkles size={26} />
                  </div>
                  <div className="ai-hero-title">{greeting()}</div>
                  <div className="ai-hero-sub">
                    我是你的本地 AI 助手，可以帮你分析、起草和规划。
                    回答只读生成，创建任务前会先给你确认卡片。
                  </div>
                  <div className="ai-suggest-grid">
                    {SUGGESTIONS.map((suggestion) => (
                      <button
                        className="ai-suggest-card"
                        key={suggestion.title}
                        onClick={() => setInput(suggestion.prompt)}
                        type="button"
                      >
                        <suggestion.icon size={15} />
                        <div className="ai-suggest-title">
                          {suggestion.title}
                        </div>
                        <div className="ai-suggest-sub">{suggestion.sub}</div>
                      </button>
                    ))}
                  </div>
                </div>
              ) : (
                messages.data.pages
                  .flatMap((page) => page.data)
                  .map((message) => (
                    <AiMessageBlock
                      attachedTaskId={message.task_id}
                      content={message.content}
                      createdAt={message.created_at}
                      key={message.id}
                      onOpenTaskCard={() => openTaskCard(message)}
                      reasoning={message.reasoning}
                      role={message.role}
                      status={message.status}
                    />
                  ))
              )}
              {chat.isStreaming ? (
                <>
                  {chat.sentMessage !== null ? (
                    <div className="ai-msg-user">
                      <span className="ai-msg-time" />
                      <div className="ai-bubble-user">{chat.sentMessage}</div>
                    </div>
                  ) : null}
                  <div className="ai-msg">
                    <div className="ai-avatar">
                      <Sparkles size={15} />
                    </div>
                    <div className="ai-msg-body">
                      <div className="ai-name-row">
                        <span className="ai-name">AI 助手</span>
                      </div>
                      {(chat.streaming?.reasoning ?? "").length > 0 ? (
                        <AiThinkingProcess
                          live={streamedText === ""}
                          reasoning={chat.streaming?.reasoning ?? ""}
                        />
                      ) : null}
                      <div className="ai-msg-text" data-streaming>
                        {streamedText
                          ? renderAiRichText(streamedText)
                          : (chat.streaming?.reasoning ?? "").length > 0
                            ? ""
                            : "正在思考…"}
                      </div>
                    </div>
                  </div>
                </>
              ) : null}
            </div>
          </div>

          {chat.streamError ? (
            <div className="ai-error-banner" role="alert">
              <AlertCircle size={14} />
              <span>{chat.streamError}</span>
            </div>
          ) : null}

          {pendingCard && draft ? (
            <div className="ai-task-card" role="group">
              <header>
                <Sparkles size={14} />
                <span>建议创建任务</span>
              </header>
              <label>
                标题（必填）
                <input
                  onChange={(event) =>
                    setDraft({ ...draft, title: event.target.value })
                  }
                  value={draft.title}
                />
              </label>
              <label>
                描述
                <textarea
                  onChange={(event) =>
                    setDraft({ ...draft, description: event.target.value })
                  }
                  rows={2}
                  value={draft.description}
                />
              </label>
              <div className="ai-task-card-row">
                <label>
                  截止日期
                  <input
                    onChange={(event) =>
                      setDraft({ ...draft, dueDate: event.target.value })
                    }
                    placeholder="YYYY-MM-DD"
                    type="date"
                    value={draft.dueDate}
                  />
                </label>
                <label>
                  项目
                  <ProjectSelect
                    ariaLabel="选择任务所属项目"
                    emptyLabel="不关联项目"
                    onChange={(projectId) => setDraft({ ...draft, projectId })}
                    value={draft.projectId ?? ""}
                    variant="form"
                  />
                </label>
              </div>
              {taskError ? (
                <div className="ai-task-card-error" role="alert">
                  <AlertCircle size={14} />
                  {taskError}
                </div>
              ) : null}
              <footer>
                <button
                  className="button button-primary"
                  disabled={
                    createTask.isPending ||
                    attachTask.isPending ||
                    !draft.title.trim()
                  }
                  onClick={() => void confirmCreateTask()}
                  type="button"
                >
                  {createTask.isPending || attachTask.isPending ? (
                    <LoaderCircle className="animate-spin" size={14} />
                  ) : (
                    <CheckCircle2 size={14} />
                  )}
                  确认创建
                </button>
                <button
                  className="button button-quiet"
                  onClick={() => {
                    setPendingCard(null);
                    setDraft(null);
                  }}
                  type="button"
                >
                  取消
                </button>
              </footer>
            </div>
          ) : null}

          <div className="ai-composer">
            <div className="ai-composer-inner">
              <div className="ai-composer-box">
                <textarea
                  className="ai-composer-textarea"
                  disabled={!activeProvider || chat.isStreaming}
                  onChange={(event) => setInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.shiftKey) {
                      event.preventDefault();
                      void sendMessage();
                    }
                  }}
                  placeholder={
                    activeProvider
                      ? "向 AI 助手提问… Enter 发送，Shift+Enter 换行"
                      : "配置 AI 供应商后可用"
                  }
                  rows={3}
                  value={input}
                />
                <div className="ai-composer-tool-row">
                  <div className="ai-composer-tools-left">
                    {activeProvider ? (
                      <select
                        aria-label="选择 AI 供应商"
                        className="ai-model-select"
                        onChange={(event) =>
                          setSelectedProviderId(event.target.value)
                        }
                        value={activeProvider.id}
                      >
                        {readyProviders.map((provider) => (
                          <option key={provider.id} value={provider.id}>
                            {provider.name} · {provider.model}
                          </option>
                        ))}
                      </select>
                    ) : null}
                  </div>
                  {chat.isStreaming ? (
                    <button
                      aria-label="停止生成"
                      className="ai-send-btn is-stop"
                      onClick={chat.stop}
                      type="button"
                    >
                      <Square size={14} />
                    </button>
                  ) : (
                    <button
                      aria-label="发送"
                      className="ai-send-btn"
                      disabled={!activeProvider || !input.trim()}
                      onClick={() => void sendMessage()}
                      type="button"
                    >
                      <Send size={15} />
                    </button>
                  )}
                </div>
              </div>
              <div className="ai-composer-hint">
                AI 生成内容仅供参考 · 回答只读，创建任务需你确认 ·
                对话数据仅保存在本机
              </div>
            </div>
          </div>
        </section>
      </div>

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
                    expectedVersion: 1,
                  })
                  .then(() => {
                    if (sessionId === activeSessionId) {
                      setActiveSessionId("");
                    }
                    setDeletingSession(null);
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
      </Modal>
    </div>
  );
}

function AiThinkingProcess({
  reasoning,
  live,
}: {
  reasoning: string;
  live: boolean;
}) {
  const [expanded, setExpanded] = useState(live);
  return (
    <div className="ai-thinking">
      <button
        className="ai-thinking-toggle"
        onClick={() => setExpanded((previous) => !previous)}
        type="button"
      >
        <Brain size={14} />
        <span>{live ? "思考中…" : "思考过程"}</span>
        {live ? (
          <LoaderCircle className="animate-spin" size={12} />
        ) : (
          <span className="ai-thinking-chevron">{expanded ? "▾" : "▸"}</span>
        )}
      </button>
      {expanded ? <div className="ai-think-card">{reasoning}</div> : null}
    </div>
  );
}

function AiMessageBlock({
  role,
  content,
  reasoning,
  createdAt,
  status,
  attachedTaskId,
  onOpenTaskCard,
}: {
  role: AiMessage["role"];
  content: string;
  reasoning: string | null;
  createdAt: string;
  status: AiMessage["status"];
  attachedTaskId: string | null;
  onOpenTaskCard: () => void;
}) {
  const navigate = useNavigate();
  if (role === "user") {
    return (
      <div className="ai-msg-user">
        <span className="ai-msg-time">{messageTimeLabel(createdAt)}</span>
        <div className="ai-bubble-user">{content}</div>
      </div>
    );
  }
  const suggestion = parseAiTaskSuggestion(content);
  const display = stripAiTaskBlock(content);
  return (
    <div className="ai-msg">
      <div className="ai-avatar">
        <Sparkles size={15} />
      </div>
      <div className="ai-msg-body">
        <div className="ai-name-row">
          <span className="ai-name">AI 助手</span>
          <span className="ai-msg-time">{messageTimeLabel(createdAt)}</span>
        </div>
        {reasoning ? (
          <AiThinkingProcess reasoning={reasoning} live={false} />
        ) : null}
        <div className="ai-msg-text" data-status={status}>
          {status === "cancelled" && display === ""
            ? "（已停止生成）"
            : renderAiRichText(display)}
          {status === "cancelled" && display !== ""
            ? "（已停止生成，内容不完整）"
            : null}
        </div>
        {attachedTaskId ? (
          <AiTaskCreatedCard
            onOpen={() => navigate(`/tasks/${attachedTaskId}`)}
            taskId={attachedTaskId}
            fallbackTitle={content}
          />
        ) : suggestion ? (
          <button
            className="ai-action-chip"
            onClick={onOpenTaskCard}
            type="button"
          >
            <Sparkles size={13} />
            建议任务：{suggestion.title}（点击确认创建）
          </button>
        ) : null}
      </div>
    </div>
  );
}

function AiTaskCreatedCard({
  taskId,
  fallbackTitle,
  onOpen,
}: {
  taskId: string;
  fallbackTitle: string;
  onOpen: () => void;
}) {
  const task = useTaskQuery(taskId);
  const suggestion = parseAiTaskSuggestion(fallbackTitle);
  const title = task.data?.title ?? suggestion?.title ?? taskId;
  const due = task.data?.dueDate ?? suggestion?.due ?? null;
  const status: TaskStatus = task.data?.status ?? "todo";
  return (
    <button
      className="ai-created-task-card"
      onClick={onOpen}
      title="打开任务详情"
      type="button"
    >
      <div className="ai-created-task-head">
        <span className="ai-created-tag">
          <CheckCircle2 size={11} />
          Created
        </span>
        <span className="ai-created-chev">›</span>
      </div>
      <div className="ai-created-title">{title}</div>
      <div className="ai-created-meta">
        <span>{statusLabels[status]}</span>
        {due ? (
          <span>
            <CalendarDays size={12} />
            {due} 截止
          </span>
        ) : null}
      </div>
    </button>
  );
}
