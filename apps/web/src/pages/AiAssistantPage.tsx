import {
  AlertCircle,
  BarChart3,
  Brain,
  CalendarDays,
  CheckCircle2,
  Database,
  Eye,
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
  X,
} from "lucide-react";
import { useMutation, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  getAiRunSteps,
  getAiUsageSummary,
  searchKnowledge,
} from "../api/client";
import {
  aiUsageSummaryQueryKey,
  useAiChatStream,
  useAiMessagesInfiniteQuery,
  useAiProvidersQuery,
  useCreateAiMemory,
  useAiSessionsQuery,
  useAttachTaskToAiMessage,
  useCreateAiSession,
  useCreateTask,
  useDeleteAiSession,
  usePreviewAiBusinessContext,
  useTaskQuery,
} from "../api/hooks";
import { ErrorState, LoadingState } from "../components/feedback";
import { ClientSelect } from "../components/ClientSelect";
import { Modal } from "../components/Modal";
import { ProjectSelect } from "../components/ProjectSelect";
import { TaskSelect } from "../components/TaskSelect";
import {
  parseAiMemorySuggestion,
  parseAiTaskSuggestion,
  stripAiSelfCheckBlock,
  stripAiTaskBlock,
  type AiTaskSuggestion,
} from "../lib/aiTaskCard";
import { useUiStore } from "../store/ui";
import type {
  AiBusinessContextPreview,
  AiBusinessContextProviderSnapshot,
  AiBusinessContextSource,
  AiBusinessContextType,
  AiCitation,
  AiCitationStatus,
  AiMessage,
  AiSession,
  AiKnowledgeContextSource,
  KnowledgeSearchResult,
  TaskStatus,
} from "../types/models";

interface PendingTaskCard {
  messageId: string;
  suggestion: AiTaskSuggestion;
  createdTaskId?: string;
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
  const previewContext = usePreviewAiBusinessContext();
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
  const [contextPanelOpen, setContextPanelOpen] = useState(false);
  const [contextPreviewOpen, setContextPreviewOpen] = useState(false);
  const [contextTaskId, setContextTaskId] = useState("");
  const [contextProjectId, setContextProjectId] = useState("");
  const [contextClientId, setContextClientId] = useState("");
  const [contextKnowledgeQuery, setContextKnowledgeQuery] = useState("");
  const [selectedKnowledgeChunks, setSelectedKnowledgeChunks] = useState<
    KnowledgeSearchResult[]
  >([]);
  const [confirmedContext, setConfirmedContext] =
    useState<AiBusinessContextPreview | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const autoScrolledSessionRef = useRef("");
  const knowledgeSearch = useMutation({
    mutationFn: (query: string) => searchKnowledge(query, [], 8),
  });

  const readyProviders =
    providers.data?.filter(
      (provider) =>
        provider.status === "ready" &&
        (provider.kind === "local" || provider.has_key),
    ) ?? [];
  const activeProvider =
    readyProviders.find((provider) => provider.id === selectedProviderId) ??
    null;

  const selectedContextSources = useMemo(
    () =>
      [
        contextTaskId ? { type: "task" as const, id: contextTaskId } : null,
        contextProjectId
          ? { type: "project" as const, id: contextProjectId }
          : null,
        contextClientId
          ? { type: "client" as const, id: contextClientId }
          : null,
      ].filter(
        (source): source is { type: AiBusinessContextType; id: string } =>
          source !== null,
      ),
    [contextClientId, contextProjectId, contextTaskId],
  );
  const contextSelectionCount =
    selectedContextSources.length + selectedKnowledgeChunks.length;

  const confirmedContextIsCurrent = Boolean(
    confirmedContext &&
    activeProvider &&
    confirmedContext.provider_id === activeProvider.id &&
    confirmedContext.provider_version === activeProvider.version &&
    confirmedContext.sources.length === selectedContextSources.length &&
    confirmedContext.sources.every((source) =>
      selectedContextSources.some(
        (selected) =>
          selected.type === source.type && selected.id === source.id,
      ),
    ) &&
    (confirmedContext.knowledge ?? []).length ===
      selectedKnowledgeChunks.length &&
    (confirmedContext.knowledge ?? []).every((source) =>
      selectedKnowledgeChunks.some(
        (selected) => selected.chunkId === source.chunk_id,
      ),
    ),
  );

  useEffect(() => {
    if (
      readyProviders.length > 0 &&
      !readyProviders.some((provider) => provider.id === selectedProviderId)
    ) {
      setSelectedProviderId(readyProviders[0].id);
    }
  }, [readyProviders, selectedProviderId]);

  useEffect(() => {
    setConfirmedContext(null);
  }, [
    contextClientId,
    contextProjectId,
    contextTaskId,
    selectedKnowledgeChunks,
    selectedProviderId,
  ]);

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
    if (
      !node ||
      !messages.data ||
      typeof node.scrollTo !== "function" ||
      (autoScrolledSessionRef.current === activeSessionId &&
        chat.streaming?.sessionId !== activeSessionId)
    ) {
      return;
    }
    node.scrollTo({ top: node.scrollHeight });
    autoScrolledSessionRef.current = activeSessionId;
  }, [
    activeSessionId,
    messages.data,
    chat.streaming?.sessionId,
    chat.streaming?.text,
  ]);

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
  const loadedMessages = useMemo(
    () =>
      messages.data
        ? [...messages.data.pages].reverse().flatMap((page) => page.data)
        : [],
    [messages.data],
  );
  const hasMessages = loadedMessages.length > 0;

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
    let taskId = pendingCard.createdTaskId ?? null;
    try {
      if (!taskId) {
        const task = await createTask.mutateAsync({
          title: draft.title.trim(),
          description: draft.description.trim() || undefined,
          priority: "P2",
          projectId: draft.projectId ?? undefined,
          dueDate: draft.dueDate ? draft.dueDate : null,
        });
        taskId = task.id;
        setPendingCard({ ...pendingCard, createdTaskId: taskId });
      }
      await attachTask.mutateAsync({
        messageId: pendingCard.messageId,
        taskId,
      });
      setPendingCard(null);
      setDraft(null);
    } catch (error) {
      setTaskError(
        taskId
          ? "任务已经创建，但回复卡片关联失败。再次点击只会重试关联，不会重复创建任务。"
          : error instanceof Error && error.message
            ? `任务创建失败：${error.message}`
            : "任务创建失败，请重试",
      );
    }
  }

  async function previewSelectedContext() {
    if (!activeProvider || contextSelectionCount === 0) return;
    try {
      await previewContext.mutateAsync({
        provider_id: activeProvider.id,
        sources: selectedContextSources,
        knowledge: selectedKnowledgeChunks.map((result) => ({
          source_id: result.sourceId,
          document_id: result.documentId,
          chunk_id: result.chunkId,
        })),
      });
      setContextPreviewOpen(true);
    } catch {
      // mutation state renders a safe error in the context panel
    }
  }

  function clearSelectedContext() {
    setContextTaskId("");
    setContextProjectId("");
    setContextClientId("");
    setContextKnowledgeQuery("");
    setSelectedKnowledgeChunks([]);
    knowledgeSearch.reset();
    setConfirmedContext(null);
    previewContext.reset();
  }

  function toggleKnowledgeResult(result: KnowledgeSearchResult) {
    setSelectedKnowledgeChunks((current) => {
      if (current.some((item) => item.chunkId === result.chunkId)) {
        return current.filter((item) => item.chunkId !== result.chunkId);
      }
      if (current.length >= 3) return current;
      return [...current, result];
    });
  }

  async function sendMessage() {
    const message = input.trim();
    if (
      !activeProvider ||
      !message ||
      chat.isStreaming ||
      (contextSelectionCount > 0 && !confirmedContextIsCurrent)
    )
      return;
    setInput("");
    const outcome = await chat.send({
      providerId: activeProvider.id,
      sessionId: activeSessionId || undefined,
      message,
      context:
        confirmedContext && confirmedContextIsCurrent
          ? {
              provider_version: confirmedContext.provider_version,
              sources: confirmedContext.sources.map((source) => ({
                type: source.type,
                id: source.id,
                expected_version: source.version,
              })),
              knowledge: (confirmedContext.knowledge ?? []).map((source) => ({
                source_id: source.source_id,
                document_id: source.document_id,
                chunk_id: source.chunk_id,
                expected_source_version: source.source_version,
                expected_document_version: source.document_version,
              })),
            }
          : undefined,
    });
    if (outcome.sessionId && outcome.sessionId !== activeSessionId) {
      setActiveSessionId(outcome.sessionId);
    }
    if (
      outcome.errorCode === "AI_CONTEXT_CHANGED" ||
      outcome.errorCode === "AI_CONTEXT_PROVIDER_CHANGED" ||
      outcome.errorCode === "AI_CONTEXT_SOURCE_NOT_FOUND" ||
      outcome.errorCode === "AI_KNOWLEDGE_CONTEXT_CHANGED" ||
      outcome.errorCode === "AI_KNOWLEDGE_CONTEXT_NOT_FOUND"
    ) {
      setConfirmedContext(null);
      setContextPanelOpen(true);
    }
    if (!outcome.error) {
      setContextTaskId("");
      setContextProjectId("");
      setContextClientId("");
      setContextKnowledgeQuery("");
      setSelectedKnowledgeChunks([]);
      knowledgeSearch.reset();
      setConfirmedContext(null);
      setContextPanelOpen(false);
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
      ? "尚未配置 AI 供应商。请到 设置 → AI 助手 登记远程 API 供应商（保存密钥）或本地部署供应商（如 Ollama）。"
      : readyProviders.length === 0
        ? "AI 供应商未就绪。请到 设置 → AI 助手 保存 API 密钥（远程）或通过连接测试（本地）。"
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
            disabled={createSession.isPending || chat.isStreaming}
            onClick={() => {
              setPendingCard(null);
              setDraft(null);
              void createSession
                .mutateAsync()
                .then((session) => {
                  setActiveSessionId(session.id);
                })
                .catch(() => {
                  // mutation state renders the safe error below
                });
            }}
            type="button"
          >
            <Plus size={15} />
            新会话
          </button>
          {createSession.error ? (
            <div className="ai-session-create-error" role="alert">
              无法新建会话，请重试
            </div>
          ) : null}
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
                      disabled={chat.isStreaming}
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
                      disabled={chat.isStreaming}
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
            <div className="ai-mobile-session-controls">
              <select
                aria-label="移动端选择会话"
                disabled={chat.isStreaming}
                onChange={(event) => {
                  setActiveSessionId(event.target.value);
                  setPendingCard(null);
                  setDraft(null);
                }}
                value={activeSessionId}
              >
                <option value="">新会话</option>
                {(sessions.data ?? []).map((session) => (
                  <option key={session.id} value={session.id}>
                    {session.title}
                  </option>
                ))}
              </select>
              <button
                aria-label="移动端新会话"
                disabled={createSession.isPending || chat.isStreaming}
                onClick={() => {
                  setPendingCard(null);
                  setDraft(null);
                  void createSession
                    .mutateAsync()
                    .then((session) => setActiveSessionId(session.id))
                    .catch(() => {
                      // mutation state renders the safe error in the rail on wider layouts
                    });
                }}
                type="button"
              >
                <Plus size={14} />
              </button>
            </div>
            <div className="min-w-0">
              <div className="ai-chat-header-title">
                {activeSession?.title ?? "新会话"}
              </div>
              <div className="ai-chat-header-sub">
                {activeProvider
                  ? `${activeProvider.name} · ${activeProvider.model}`
                  : "未配置供应商"}
              </div>
              {(activeSession?.compacted_message_count ?? 0) > 0 ? (
                <span className="ai-context-compressed-badge" role="status">
                  已压缩前 {activeSession?.compacted_message_count} 条消息
                </span>
              ) : null}
            </div>
            <AiUsageSummaryPanel sessionId={activeSessionId} />
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
                <>
                  {messages.hasNextPage ? (
                    <button
                      className="button button-quiet ai-load-earlier"
                      disabled={messages.isFetchingNextPage}
                      onClick={() => void messages.fetchNextPage()}
                      type="button"
                    >
                      {messages.isFetchingNextPage
                        ? "正在加载…"
                        : "加载更早消息"}
                    </button>
                  ) : null}
                  {loadedMessages.map((message) => (
                    <AiMessageBlock
                      attachedTaskId={message.task_id}
                      content={message.content}
                      contextProvider={message.context_provider}
                      contextKnowledge={message.context_knowledge ?? []}
                      contextSources={message.context_sources}
                      citationStatus={
                        message.citation_status ?? "not_requested"
                      }
                      citations={message.citations ?? []}
                      generationId={message.generation_id ?? null}
                      createdAt={message.created_at}
                      key={message.id}
                      messageId={message.id}
                      onOpenTaskCard={() => openTaskCard(message)}
                      reasoning={message.reasoning}
                      role={message.role}
                      status={message.status}
                    />
                  ))}
                </>
              )}
              {chat.isStreaming ? (
                <>
                  {chat.sentMessage !== null ? (
                    <div className="ai-msg-user">
                      <span className="ai-msg-time" />
                      <div className="ai-user-message-stack">
                        <div className="ai-bubble-user">{chat.sentMessage}</div>
                        {confirmedContextIsCurrent && confirmedContext ? (
                          <AiBusinessContextChips
                            provider={{
                              id: confirmedContext.provider_id,
                              name: confirmedContext.provider_name,
                              kind: confirmedContext.provider_kind,
                              version: confirmedContext.provider_version,
                            }}
                            knowledge={confirmedContext.knowledge ?? []}
                            sources={confirmedContext.sources}
                          />
                        ) : null}
                      </div>
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
                          ? renderAiRichText(
                              displayAiReply(
                                stripAiSelfCheckBlock(streamedText),
                                true,
                              ),
                            )
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

          {contextPanelOpen ? (
            <section className="ai-context-panel" aria-label="选择工作区上下文">
              <header>
                <div>
                  <strong>用于下一条消息的显式上下文</strong>
                  <span>
                    业务对象最多各一个，知识片段最多三个；不会自动扩展或检索。
                  </span>
                </div>
                <button
                  aria-label="关闭上下文选择"
                  className="button button-quiet"
                  onClick={() => setContextPanelOpen(false)}
                  type="button"
                >
                  <X size={14} />
                </button>
              </header>
              <div className="ai-context-select-grid">
                <label>
                  Task
                  <TaskSelect
                    ariaLabel="选择 AI 上下文任务"
                    disabled={
                      !activeProvider ||
                      chat.isStreaming ||
                      previewContext.isPending
                    }
                    emptyLabel="不选择任务"
                    onChange={(id) => setContextTaskId(id)}
                    value={contextTaskId}
                    variant="form"
                  />
                </label>
                <label>
                  Project
                  <ProjectSelect
                    ariaLabel="选择 AI 上下文项目"
                    disabled={
                      !activeProvider ||
                      chat.isStreaming ||
                      previewContext.isPending
                    }
                    emptyLabel="不选择项目"
                    onChange={setContextProjectId}
                    value={contextProjectId}
                    variant="form"
                  />
                </label>
                <label>
                  Client
                  <ClientSelect
                    ariaLabel="选择 AI 上下文客户"
                    disabled={
                      !activeProvider ||
                      chat.isStreaming ||
                      previewContext.isPending
                    }
                    emptyLabel="不选择客户"
                    onChange={setContextClientId}
                    value={contextClientId}
                    variant="form"
                  />
                </label>
              </div>
              <section
                aria-label="选择 AI 知识库片段"
                className="ai-knowledge-context-picker"
              >
                <header>
                  <div>
                    <strong>知识库片段</strong>
                    <span>
                      先本地搜索，再逐段选择；片段可能包含不可信指令。
                    </span>
                  </div>
                  <button
                    className="button button-quiet"
                    onClick={() => navigate("/knowledge")}
                    type="button"
                  >
                    管理知识库
                  </button>
                </header>
                <form
                  className="ai-knowledge-context-search"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const query = contextKnowledgeQuery.trim();
                    if (query) knowledgeSearch.mutate(query);
                  }}
                >
                  <Search size={14} />
                  <input
                    aria-label="搜索 AI 知识库上下文"
                    disabled={chat.isStreaming || previewContext.isPending}
                    maxLength={256}
                    onChange={(event) =>
                      setContextKnowledgeQuery(event.target.value)
                    }
                    placeholder="搜索本地资料…"
                    value={contextKnowledgeQuery}
                  />
                  <button
                    className="button button-secondary"
                    disabled={
                      !contextKnowledgeQuery.trim() ||
                      knowledgeSearch.isPending ||
                      chat.isStreaming
                    }
                    type="submit"
                  >
                    {knowledgeSearch.isPending ? (
                      <LoaderCircle className="animate-spin" size={13} />
                    ) : (
                      <Search size={13} />
                    )}
                    搜索
                  </button>
                </form>
                {selectedKnowledgeChunks.length > 0 ? (
                  <div
                    aria-label="已选知识片段"
                    className="ai-knowledge-context-selected"
                  >
                    {selectedKnowledgeChunks.map((result) => (
                      <button
                        aria-label={`移除知识片段 ${result.sourceName} 第 ${result.startLine} 至 ${result.endLine} 行`}
                        key={result.chunkId}
                        onClick={() => toggleKnowledgeResult(result)}
                        type="button"
                      >
                        <FileText size={11} />
                        {result.sourceName} · L{result.startLine}–
                        {result.endLine}
                        <X size={10} />
                      </button>
                    ))}
                  </div>
                ) : null}
                {knowledgeSearch.data ? (
                  <div className="ai-knowledge-context-results">
                    {knowledgeSearch.data.items.length === 0 ? (
                      <span className="ai-knowledge-context-empty">
                        没有匹配片段，请调整关键词或先导入资料。
                      </span>
                    ) : (
                      knowledgeSearch.data.items.map((result) => {
                        const selected = selectedKnowledgeChunks.some(
                          (item) => item.chunkId === result.chunkId,
                        );
                        return (
                          <button
                            aria-pressed={selected}
                            className={selected ? "is-selected" : ""}
                            disabled={
                              !selected && selectedKnowledgeChunks.length >= 3
                            }
                            key={result.chunkId}
                            onClick={() => toggleKnowledgeResult(result)}
                            type="button"
                          >
                            <span>
                              <strong>{result.sourceName}</strong>
                              <small>
                                第 {result.startLine}–{result.endLine} 行 · 文档
                                v{result.documentVersion}
                              </small>
                            </span>
                            <p>{result.excerpt}</p>
                          </button>
                        );
                      })
                    )}
                  </div>
                ) : null}
                {knowledgeSearch.error ? (
                  <div className="ai-context-error" role="alert">
                    <AlertCircle size={14} />
                    {knowledgeSearch.error instanceof Error
                      ? knowledgeSearch.error.message
                      : "无法搜索本地知识库"}
                  </div>
                ) : null}
              </section>
              <footer>
                <span>
                  {contextSelectionCount === 0
                    ? "尚未选择上下文"
                    : confirmedContextIsCurrent
                      ? `已确认 ${contextSelectionCount} 项，将随下一条消息发送`
                      : `已选择 ${contextSelectionCount} 项，发送前需要预览确认`}
                </span>
                <div>
                  <button
                    className="button button-quiet"
                    disabled={contextSelectionCount === 0}
                    onClick={clearSelectedContext}
                    type="button"
                  >
                    清空
                  </button>
                  <button
                    className="button button-secondary"
                    disabled={
                      !activeProvider ||
                      contextSelectionCount === 0 ||
                      previewContext.isPending ||
                      chat.isStreaming
                    }
                    onClick={() => void previewSelectedContext()}
                    type="button"
                  >
                    {previewContext.isPending ? (
                      <LoaderCircle className="animate-spin" size={13} />
                    ) : (
                      <Eye size={13} />
                    )}
                    预览发送内容
                  </button>
                </div>
              </footer>
              {previewContext.error ? (
                <div className="ai-context-error" role="alert">
                  <AlertCircle size={14} />
                  {previewContext.error instanceof Error
                    ? previewContext.error.message
                    : "无法预览所选上下文"}
                </div>
              ) : null}
            </section>
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
                    <button
                      aria-pressed={contextPanelOpen}
                      className="ai-context-toggle"
                      data-confirmed={confirmedContextIsCurrent}
                      disabled={!activeProvider || chat.isStreaming}
                      onClick={() => setContextPanelOpen((open) => !open)}
                      type="button"
                    >
                      {confirmedContextIsCurrent ? (
                        <CheckCircle2 size={13} />
                      ) : (
                        <Database size={13} />
                      )}
                      {confirmedContextIsCurrent ? "上下文已确认" : "上下文"}
                      {contextSelectionCount > 0 ? (
                        <span>{contextSelectionCount}</span>
                      ) : null}
                    </button>
                    {activeProvider ? (
                      <select
                        aria-label="选择 AI 供应商"
                        className="ai-model-select"
                        disabled={chat.isStreaming}
                        onChange={(event) =>
                          setSelectedProviderId(event.target.value)
                        }
                        value={activeProvider.id}
                      >
                        {readyProviders.map((provider) => (
                          <option key={provider.id} value={provider.id}>
                            {provider.kind === "local"
                              ? `${provider.name} · ${provider.model} · 本地`
                              : `${provider.name} · ${provider.model}`}
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
                      disabled={
                        !activeProvider ||
                        !input.trim() ||
                        (contextSelectionCount > 0 &&
                          !confirmedContextIsCurrent)
                      }
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
                {activeProvider?.kind === "local"
                  ? "模型请求仅发送到本机回环端点"
                  : activeProvider
                    ? "当前对话上下文会发送给所选远程供应商"
                    : "尚未选择供应商，不会发送对话"}
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
              onClick={() => setContextPreviewOpen(false)}
              type="button"
            >
              返回修改
            </button>
            <button
              className="button button-primary"
              disabled={!previewContext.data}
              onClick={() => {
                const preview = previewContext.data;
                if (!preview) return;
                setConfirmedContext(preview);
                setContextPreviewOpen(false);
                setContextPanelOpen(false);
              }}
              type="button"
            >
              确认用于下一条消息
            </button>
          </>
        }
        onClose={() => setContextPreviewOpen(false)}
        open={contextPreviewOpen && Boolean(previewContext.data)}
        title="发送前检查工作区上下文"
        width="720px"
      >
        {previewContext.data ? (
          <AiBusinessContextPreviewContent preview={previewContext.data} />
        ) : null}
      </Modal>

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
                    if (sessionId === activeSessionId) {
                      setActiveSessionId("");
                    }
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
    </div>
  );
}

const aiBusinessContextTypeLabels: Record<AiBusinessContextType, string> = {
  task: "Task",
  project: "Project",
  client: "Client",
};

const aiBusinessContextFieldLabels: Record<string, string> = {
  title: "标题",
  name: "名称",
  description: "描述",
  kind: "类型",
  status: "状态",
  priority: "优先级",
  completion_criteria: "完成条件",
  planned_date: "计划日期",
  due_date: "截止日期",
  start_date: "开始日期",
  project_id: "所属项目 ID",
  project_name: "所属项目",
  notes: "备注",
  task_total: "任务总数",
  task_done: "已完成任务",
  task_blocked: "阻塞任务",
  task_waiting_review: "待验收任务",
};

function displayAIContextValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "未设置";
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }
  if (typeof value === "boolean") return value ? "是" : "否";
  return JSON.stringify(value);
}

function AiBusinessContextChips({
  provider,
  sources,
  knowledge,
}: {
  provider?: AiBusinessContextProviderSnapshot | null;
  sources: AiBusinessContextSource[];
  knowledge: AiKnowledgeContextSource[];
}) {
  return (
    <div className="ai-context-chips" aria-label="随消息发送的工作区上下文">
      {provider ? (
        <span className="is-provider">
          <Database size={11} />
          {provider.name} · {provider.kind === "local" ? "本地" : "远程"}
        </span>
      ) : null}
      {sources.map((source) => (
        <span key={`${source.type}:${source.id}`}>
          <Database size={11} />
          {aiBusinessContextTypeLabels[source.type]} · {source.label}
        </span>
      ))}
      {knowledge.map((source) => (
        <span className="is-knowledge" key={source.chunk_id}>
          <FileText size={11} />
          {source.source_name} · L{source.start_line}–{source.end_line}
        </span>
      ))}
    </div>
  );
}

function AiBusinessContextPreviewContent({
  preview,
}: {
  preview: AiBusinessContextPreview;
}) {
  return (
    <div className="ai-context-preview">
      <div
        className="ai-context-disclosure"
        data-remote={preview.leaves_device}
        role="status"
      >
        <Database size={16} />
        <div>
          <strong>
            {preview.leaves_device
              ? `将发送给远程供应商 ${preview.provider_name}`
              : `只发送到本机供应商 ${preview.provider_name}`}
          </strong>
          <span>
            共 {preview.sources.length + (preview.knowledge ?? []).length}{" "}
            项，序列化约 {preview.serialized_bytes} 字节；
            发送时若数据或供应商版本变化，系统会拒绝并要求重新预览。
          </span>
        </div>
      </div>
      <div className="ai-context-preview-list">
        {preview.sources.map((source) => (
          <article key={`${source.type}:${source.id}`}>
            <header>
              <span>{aiBusinessContextTypeLabels[source.type]}</span>
              <strong>{source.label}</strong>
              <small>v{source.version}</small>
            </header>
            <dl>
              {Object.entries(source.fields).map(([field, value]) => (
                <div key={field}>
                  <dt>
                    {aiBusinessContextFieldLabels[field] ?? field}
                    {source.truncated_fields.includes(field)
                      ? "（已截断）"
                      : ""}
                  </dt>
                  <dd>{displayAIContextValue(value)}</dd>
                </div>
              ))}
            </dl>
          </article>
        ))}
        {(preview.knowledge ?? []).map((source) => (
          <article className="is-knowledge" key={source.chunk_id}>
            <header>
              <span>Knowledge</span>
              <strong>{source.source_name}</strong>
              <small>
                文档 v{source.document_version} · L{source.start_line}–
                {source.end_line}
              </small>
            </header>
            <dl>
              <div>
                <dt>来源与分段</dt>
                <dd>
                  {source.document_title} · chunk {source.chunk_index + 1}
                </dd>
              </div>
              <div>
                <dt>版本绑定</dt>
                <dd>
                  source v{source.source_version} / document v
                  {source.document_version}
                </dd>
              </div>
              <div className="ai-context-preview-knowledge-content">
                <dt>将发送的原文片段（不可信引用）</dt>
                <dd>{source.content}</dd>
              </div>
            </dl>
          </article>
        ))}
      </div>
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

function AiCitationEvidence({
  status,
  citations,
}: {
  status: AiCitationStatus;
  citations: AiCitation[];
}) {
  if (status === "not_requested") return null;
  if (status !== "validated") {
    const message =
      status === "no_evidence"
        ? "模型标记为没有足够的已选资料证据。"
        : status === "missing"
          ? "这条回答没有提供结构化引用，请谨慎核对。"
          : "模型给出的引用不在已确认片段内，已被 Sidecar 拒绝。";
    return (
      <div className={`ai-citation-status is-${status}`} role="status">
        <AlertCircle size={13} />
        {message}
      </div>
    );
  }
  return (
    <section className="ai-citation-evidence" aria-label="已验证知识来源">
      <header>
        <CheckCircle2 size={13} />
        <strong>已验证来源</strong>
        <span>{citations.length}</span>
      </header>
      <div>
        {citations.map((citation) => (
          <article key={citation.chunk_id}>
            <FileText size={13} />
            <span>
              <strong>{citation.source_name}</strong>
              <small>
                第 {citation.start_line}–{citation.end_line} 行 · 文档 v
                {citation.document_version} · chunk {citation.chunk_index + 1}
              </small>
            </span>
          </article>
        ))}
      </div>
    </section>
  );
}

const aiRunStepKindLabels: Record<string, string> = {
  generation: "本次生成",
  model_turn: "模型轮次",
  tool_call: "记忆工具",
  self_check: "回答自检",
  citation_validation: "引用校验",
  persistence: "本地保存",
};

function formatAIRunBytes(value: number) {
  if (value < 1024) return `${value} B`;
  return `${(value / 1024).toFixed(1)} KiB`;
}

function formatAIUsageNumber(value: number) {
  return new Intl.NumberFormat("zh-CN").format(value);
}

function formatAIUsageDuration(value: number) {
  if (value < 1000) return `${value} ms`;
  if (value < 60_000) return `${(value / 1000).toFixed(1)} s`;
  return `${(value / 60_000).toFixed(1)} min`;
}

function formatAIUsageTrendDay(day: string) {
  return day.slice(5).replace("-", "/");
}

function AiUsageSummaryPanel({ sessionId }: { sessionId: string }) {
  const [expanded, setExpanded] = useState(false);
  const [trendDays, setTrendDays] = useState<7 | 30>(7);
  const summary = useQuery({
    queryKey: aiUsageSummaryQueryKey(sessionId, trendDays),
    queryFn: () => getAiUsageSummary({ sessionId, trendDays }),
    enabled: expanded && sessionId !== "",
  });
  if (!sessionId) return null;

  const terminalGenerations = summary.data
    ? summary.data.totals.completedGenerations +
      summary.data.totals.failedGenerations +
      summary.data.totals.cancelledGenerations
    : 0;
  return (
    <div className="ai-usage-summary">
      <button
        aria-expanded={expanded}
        className="ai-usage-summary-toggle"
        onClick={() => setExpanded((current) => !current)}
        type="button"
      >
        <BarChart3 size={13} />
        <span>本地用量</span>
        {summary.data ? (
          <small>
            {summary.data.totals.providerUsageGenerations}/{terminalGenerations}{" "}
            有 token
          </small>
        ) : null}
      </button>
      {expanded ? (
        <section
          aria-label="当前会话本地用量"
          className="ai-usage-summary-panel"
        >
          {summary.isPending ? (
            <span className="ai-usage-summary-loading">
              正在汇总本地运行事实…
            </span>
          ) : null}
          {summary.isError ? (
            <ErrorState
              compact
              message="无法读取本地用量"
              onRetry={() => void summary.refetch()}
            />
          ) : null}
          {summary.data ? (
            <>
              <dl className="ai-usage-summary-grid">
                <div>
                  <dt>已结束生成</dt>
                  <dd>{terminalGenerations}</dd>
                </div>
                <div>
                  <dt>Provider usage</dt>
                  <dd>
                    {summary.data.totals.providerUsageGenerations}/
                    {terminalGenerations}
                  </dd>
                </div>
                <div>
                  <dt>原始 token</dt>
                  <dd>
                    {summary.data.totals.providerUsageGenerations > 0
                      ? `${formatAIUsageNumber(summary.data.totals.inputTokens)} in / ${formatAIUsageNumber(summary.data.totals.outputTokens)} out`
                      : "—"}
                  </dd>
                </div>
                <div>
                  <dt>累计耗时</dt>
                  <dd>
                    {formatAIUsageDuration(summary.data.totals.durationMs)}
                  </dd>
                </div>
              </dl>
              <section className="ai-usage-summary-trend">
                <header>
                  <span>
                    <strong>用量趋势</strong>
                    <small>UTC 日 · 仅终态运行</small>
                  </span>
                  <span className="ai-usage-summary-range" role="group">
                    {([7, 30] as const).map((days) => (
                      <button
                        aria-pressed={trendDays === days}
                        className={trendDays === days ? "is-selected" : ""}
                        disabled={summary.isFetching}
                        key={days}
                        onClick={() => setTrendDays(days)}
                        type="button"
                      >
                        {days} 天
                      </button>
                    ))}
                  </span>
                </header>
                {(() => {
                  const maximum = Math.max(
                    1,
                    ...summary.data.trend.map(
                      (point) => point.totalGenerations,
                    ),
                  );
                  return (
                    <div
                      className={`ai-usage-summary-trend-points ${
                        trendDays === 30 ? "is-extended" : ""
                      }`}
                    >
                      {summary.data.trend.map((point) => {
                        const terminal =
                          point.completedGenerations +
                          point.failedGenerations +
                          point.cancelledGenerations;
                        return (
                          <div key={point.day}>
                            <small>{formatAIUsageTrendDay(point.day)}</small>
                            <span className="ai-usage-summary-trend-track">
                              <span
                                style={{
                                  width: `${Math.round(
                                    (point.totalGenerations / maximum) * 100,
                                  )}%`,
                                }}
                              />
                            </span>
                            <strong>{point.totalGenerations}</strong>
                            <small>
                              {terminal === 0
                                ? "—"
                                : `${point.providerUsageGenerations}/${terminal} token`}
                            </small>
                          </div>
                        );
                      })}
                    </div>
                  );
                })()}
              </section>
              <div className="ai-usage-summary-by-provider">
                {summary.data.providers.length === 0 ? (
                  <span>当前会话还没有生成记录。</span>
                ) : (
                  summary.data.providers.map((provider) => (
                    <article key={provider.providerId}>
                      <span>
                        <strong>{provider.providerName}</strong>
                        <small>
                          {provider.model} ·{" "}
                          {provider.providerKind === "local" ? "本地" : "远程"}
                        </small>
                      </span>
                      <span>
                        <strong>
                          {provider.providerUsageGenerations > 0
                            ? `${formatAIUsageNumber(provider.inputTokens)} → ${formatAIUsageNumber(provider.outputTokens)}`
                            : "usage unknown"}
                        </strong>
                        <small>
                          {provider.providerUsageGenerations}/
                          {provider.completedGenerations +
                            provider.failedGenerations +
                            provider.cancelledGenerations}{" "}
                          有 token
                        </small>
                      </span>
                    </article>
                  ))
                )}
              </div>
              <p className="ai-usage-summary-note">
                仅汇总本机运行步骤；趋势按 UTC 日统计终态运行；
                {summary.data.totals.unknownUsageGenerations} 次未返回 usage，
                不估算 token，也不计算费用。
              </p>
            </>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}

function AiRunTimeline({ generationId }: { generationId: string }) {
  const [expanded, setExpanded] = useState(false);
  const steps = useQuery({
    queryKey: ["ai", "run-steps", generationId],
    queryFn: () => getAiRunSteps(generationId),
    enabled: expanded,
  });
  return (
    <section className="ai-run-timeline" aria-label="AI 运行详情">
      <button
        aria-expanded={expanded}
        className="ai-run-timeline-toggle"
        onClick={() => setExpanded((current) => !current)}
        type="button"
      >
        <ListChecks size={13} />
        <span>运行详情</span>
        {steps.data ? (
          <small>
            {steps.data.meta.total} 步 · {steps.data.meta.durationMs} ms
          </small>
        ) : null}
        <span aria-hidden="true">{expanded ? "▾" : "▸"}</span>
      </button>
      {expanded ? (
        <div className="ai-run-timeline-panel">
          {steps.isPending ? (
            <span className="ai-run-timeline-loading">
              正在读取本地运行事实…
            </span>
          ) : null}
          {steps.isError ? (
            <ErrorState
              compact
              message={
                steps.error instanceof Error
                  ? steps.error.message
                  : "无法读取本地运行详情"
              }
              onRetry={() => void steps.refetch()}
            />
          ) : null}
          {steps.data ? (
            <>
              <div className="ai-run-timeline-summary">
                <span>输入 {formatAIRunBytes(steps.data.meta.inputBytes)}</span>
                <span>
                  输出 {formatAIRunBytes(steps.data.meta.outputBytes)}
                </span>
                {steps.data.meta.tokenSource === "provider" ? (
                  <span>
                    Provider token：{steps.data.meta.inputTokens} in /{" "}
                    {steps.data.meta.outputTokens} out
                  </span>
                ) : (
                  <span>Provider 未返回 token usage</span>
                )}
                <span>仅字节与耗时，不保存正文</span>
              </div>
              <div className="ai-run-timeline-list">
                {steps.data.items.map((step) => (
                  <article data-status={step.status} key={step.id}>
                    <span className="ai-run-step-index">{step.sequence}</span>
                    <span className="ai-run-step-copy">
                      <strong>
                        {aiRunStepKindLabels[step.kind] ?? step.kind}
                        {step.turnIndex ? ` ${step.turnIndex}` : ""}
                        {step.toolName ? ` · ${step.toolName}` : ""}
                      </strong>
                      <small>
                        {step.status === "succeeded"
                          ? "完成"
                          : step.status === "cancelled"
                            ? "已取消"
                            : step.status === "failed"
                              ? `失败 · ${step.errorCode}`
                              : "运行中"}
                      </small>
                    </span>
                    <span className="ai-run-step-metrics">
                      {step.durationMs ?? 0} ms
                      {step.inputBytes || step.outputBytes
                        ? ` · ${formatAIRunBytes(step.inputBytes)} → ${formatAIRunBytes(step.outputBytes)}`
                        : ""}
                      {step.tokenSource === "provider"
                        ? ` · ${step.inputTokens} → ${step.outputTokens} tokens`
                        : ""}
                    </span>
                  </article>
                ))}
              </div>
            </>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

function AiMessageBlock({
  role,
  messageId,
  content,
  contextProvider,
  contextSources,
  contextKnowledge,
  citationStatus,
  citations,
  generationId,
  reasoning,
  createdAt,
  status,
  attachedTaskId,
  onOpenTaskCard,
}: {
  role: AiMessage["role"];
  messageId: string;
  content: string;
  contextProvider: AiMessage["context_provider"];
  contextSources: AiBusinessContextSource[];
  contextKnowledge: AiKnowledgeContextSource[];
  citationStatus: AiCitationStatus;
  citations: AiCitation[];
  generationId: string | null;
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
        <div className="ai-user-message-stack">
          <div className="ai-bubble-user">{content}</div>
          {contextSources.length > 0 || contextKnowledge.length > 0 ? (
            <AiBusinessContextChips
              knowledge={contextKnowledge}
              provider={contextProvider}
              sources={contextSources}
            />
          ) : null}
        </div>
      </div>
    );
  }
  const suggestion = parseAiTaskSuggestion(content);
  const display = displayAiReply(content);
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
        <AiCitationEvidence citations={citations} status={citationStatus} />
        {generationId ? <AiRunTimeline generationId={generationId} /> : null}
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
        <AiMemorySuggestionCard content={content} messageId={messageId} />
      </div>
    </div>
  );
}

function displayAiReply(content: string, streaming = false): string {
  const display = stripAiTaskBlock(content).trim();
  if (display) return display;
  if (parseAiTaskSuggestion(content)) {
    return "好的，我已经整理成任务建议，请确认下面的信息后创建。";
  }
  if (parseAiMemorySuggestion(content)) {
    return "好的，我整理了一条可记住的信息，请确认是否保存。";
  }
  if (/\[opc:(?:task|memory)\]/i.test(content)) {
    return streaming ? "" : "这条建议格式不完整，未执行任何操作，请重新生成。";
  }
  return content;
}

// AiMemorySuggestionCard shows the user-confirmation gate for a remembered
// preference: model output is an untrusted suggestion and nothing is stored
// until the user confirms (ADR-006).
function AiMemorySuggestionCard({
  content,
  messageId,
}: {
  content: string;
  messageId: string;
}) {
  const createMemory = useCreateAiMemory();
  const [dismissed, setDismissed] = useState(false);
  const [saved, setSaved] = useState(false);
  const memory = parseAiMemorySuggestion(content);
  if (!memory || dismissed) return null;
  if (saved) {
    return (
      <div className="ai-memory-card is-saved" role="status">
        <Brain size={14} />
        已记住：{memory.content}
      </div>
    );
  }
  return (
    <div className="ai-memory-card">
      <span className="ai-memory-text">记住偏好：{memory.content}</span>
      <button
        className="button button-secondary"
        disabled={createMemory.isPending}
        onClick={() => {
          void createMemory
            .mutateAsync({
              content: memory.content,
              source_message_id: messageId,
              ...(memory.proposalId ? { proposal_id: memory.proposalId } : {}),
            })
            .then(() => setSaved(true))
            .catch(() => {
              /* save failure surfaces via the mutation state below */
            });
        }}
        type="button"
      >
        记住
      </button>
      <button
        className="button button-quiet"
        onClick={() => setDismissed(true)}
        type="button"
      >
        忽略
      </button>
      {createMemory.error ? (
        <span className="ai-memory-error" role="alert">
          保存失败，请重试
        </span>
      ) : null}
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
