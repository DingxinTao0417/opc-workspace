import {
  AlertCircle,
  ArrowUp,
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
  PanelRightOpen,
  PanelLeftOpen,
  Plus,
  Search,
  Sparkles,
  Square,
  X,
} from "lucide-react";
import { useAiProjectFiles } from "../store/aiProjectFiles";
import { AiProjectFileContext } from "../components/AiProjectFileContext";
import { AiProjectFileReview } from "../components/AiProjectFileReview";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import {
  getAiRunSteps,
  getAiUsageSummary,
  searchKnowledge,
  rejectAiMemoryProposal,
} from "../api/client";
import {
  getAiCompactionStatus,
  getAiMemoryDecision,
  rejectAiMessageMemory,
} from "../api/aiActions";
import {
  aiUsageSummaryQueryKey,
  useAiChatStream,
  useAiMessagesInfiniteQuery,
  useAiProvidersQuery,
  useCreateAiMemory,
  useAiSessionsQuery,
  useConfirmAiMessageTask,
  useCreateAiSession,
  usePreviewAiBusinessContext,
  useTaskQuery,
} from "../api/hooks";
import { ErrorState, LoadingState } from "../components/feedback";
import { ClientSelect } from "../components/ClientSelect";
import { Modal } from "../components/Modal";
import { ModeSwitch } from "../components/ModeSwitch";
import { AiCopyButton } from "../components/AiCopyButton";
import {
  AiWorkspaceAccess,
  AiWorkspaceGrantSummary,
} from "../components/AiWorkspaceAccess";
import {
  AiAccessRequestPrompt,
  accessRequestContinuationPrompt,
} from "../components/AiAccessRequestCard";
import { AiWorkspaceRecordNavigation } from "../components/AiWorkspaceRecordNavigation";
import {
  AiWorkspaceActions,
  useAiWorkspaceActions,
} from "../components/AiWorkspaceActions";
import { renderAiRichText } from "../components/AiRichText";
import { AiCitationEvidence } from "../components/AiCitationEvidence";
import { AiRunProgress } from "../components/AiRunProgress";
import { AiWorkPlan } from "../components/AiWorkPlan";
import {
  AiAutomaticOrigin,
  AiAutomaticReply,
  AiContinuationSendNotice,
  usePlanContinuation,
} from "../components/AiPlanContinuation";
import { isContinuationActive } from "../api/aiPlanContinuation";
import { AiApprovalContinuation } from "../components/AiApprovalContinuation";
import { AiActionReceiptAttachment } from "../components/AiActionReceiptAttachment";
import type { AiActionContinuation } from "../lib/aiActionContinuation";
import { aiToolLabel } from "../lib/aiToolLabels";
import {
  parseAiContinuationLocation,
  type AiContinuationLocation,
} from "../lib/aiContinuationLocation";
import {
  AiIssueHandoffCard,
  AiWorkbenchHandoffCard,
} from "../components/AiWorkbenchHandoff";
import { workbenchHandoffDetails } from "../store/aiWorkbenchHandoff";
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
import { useAiChatStore } from "../store/aiChat";
import type {
  AiBusinessContextPreview,
  AiBusinessContextProviderSnapshot,
  AiBusinessContextSource,
  AiBusinessContextType,
  AiCitation,
  AiCitationStatus,
  AiMessage,
  AiSession,
  AiWorkspaceGrant,
  AiWorkspaceScope,
  AiKnowledgeContextSource,
  KnowledgeSearchResult,
  TaskStatus,
} from "../types/models";

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

function draftFromSuggestion(suggestion: AiTaskSuggestion): DraftTaskForm {
  return {
    title: suggestion.title,
    description: suggestion.description ?? "",
    dueDate: suggestion.due ?? "",
    projectId: null,
  };
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

function isCanonicalWorkspaceId(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
    value,
  );
}

export function AiAssistantPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [continuation, setContinuation] = useState<
    (AiContinuationLocation & { request: number }) | null
  >(null);
  const continuationRequest = useRef(0);
  const providers = useAiProvidersQuery();
  const sessions = useAiSessionsQuery();
  const createSession = useCreateAiSession();
  const previewContext = usePreviewAiBusinessContext();
  const chat = useAiChatStream();
  const setSettingsOpen = useUiStore((store) => store.setSettingsOpen);
  const agentRailCollapsed = useUiStore((store) => store.agentRailCollapsed);
  const toggleAgentRailCollapsed = useUiStore(
    (store) => store.toggleAgentRailCollapsed,
  );
  const rightOverviewCollapsed = useUiStore(
    (store) => store.rightOverviewCollapsed,
  );
  const toggleRightOverviewCollapsed = useUiStore(
    (store) => store.toggleRightOverviewCollapsed,
  );

  const activeSessionId = useAiChatStore((state) => state.activeSessionId);
  const liveAccessRequest = useAiChatStore(
    (state) => state.accessRequests[activeSessionId],
  );
  const dismissAccessRequest = useAiChatStore(
    (state) => state.dismissAccessRequest,
  );
  const automaticContinuation = usePlanContinuation(activeSessionId);
  const [automaticSendBlocked, setAutomaticSendBlocked] = useState(false);
  useEffect(
    () => setAutomaticSendBlocked(false),
    [activeSessionId, automaticContinuation.data?.status],
  );
  const setActiveSessionId = useAiChatStore(
    (state) => state.setActiveSessionId,
  );
  const [selectedProviderId, setSelectedProviderId] = useState("");
  const input = useAiChatStore((state) => state.input);
  const setInput = useAiChatStore((state) => state.setInput);
  const retainedTurns = useAiChatStore((state) => state.retainedTurns);
  const [pendingCard, setPendingCard] = useState<PendingTaskCard | null>(null);
  const [draft, setDraft] = useState<DraftTaskForm | null>(null);
  const [taskError, setTaskError] = useState<string | null>(null);
  const [contextPanelOpen, setContextPanelOpen] = useState(false);
  const [workspaceApproval, setWorkspaceApproval] = useState<{
    providerId: string;
    sessionId: string;
    grant: AiWorkspaceGrant;
  } | null>(null);
  const [workspaceAccessRequest, setWorkspaceAccessRequest] = useState(0);
  const [actionReceiptGenerationId, setActionReceiptGenerationId] =
    useState<string>();
  const [actionRecheckProposalId, setActionRecheckProposalId] =
    useState<string>();
  const receiptPreparedAt = useRef(0);
  const receiptAttachmentId = useRef<string>();
  const acceptedCommand = useAiChatStore((state) => state.acceptedCommand);
  const [recommendedWorkspaceScopes, setRecommendedWorkspaceScopes] = useState<
    AiWorkspaceScope[]
  >(["work", "actions"]);
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
  const [fileSendError, setFileSendError] = useState<string | null>(null);
  const filePreparing = useRef(false);
  const fileSendIdentity = useRef("");
  fileSendIdentity.current = JSON.stringify([
    activeSessionId,
    activeProvider?.id,
    activeProvider?.version,
    automaticContinuation.data?.status,
    workspaceApproval,
    confirmedContext,
    contextTaskId,
    contextProjectId,
    contextClientId,
    selectedKnowledgeChunks,
    actionReceiptGenerationId,
    actionRecheckProposalId,
  ]);

  useEffect(() => {
    const requestedSettings = searchParams.get("settings");
    if (
      requestedSettings !== "ai" &&
      requestedSettings !== "data" &&
      requestedSettings !== "actors" &&
      requestedSettings !== "agent" &&
      requestedSettings !== "diagnostics"
    )
      return;
    setSettingsOpen(true, requestedSettings);
    const next = new URLSearchParams(searchParams);
    next.delete("settings");
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams, setSettingsOpen]);

  useEffect(() => {
    const requestedSession = searchParams.get("session");
    if (!requestedSession) return;
    if (isCanonicalWorkspaceId(requestedSession)) {
      setActiveSessionId(requestedSession);
      const target = parseAiContinuationLocation(searchParams);
      setContinuation(
        target ? { ...target, request: ++continuationRequest.current } : null,
      );
    }
    const next = new URLSearchParams(searchParams);
    next.delete("session");
    next.delete("generation");
    next.delete("proposal");
    next.delete("plan");
    setSearchParams(next, { replace: true });
  }, [searchParams, setActiveSessionId, setSearchParams]);

  useEffect(() => {
    if (
      !searchParams.has("session") &&
      continuation &&
      continuation.sessionId !== activeSessionId
    )
      setContinuation(null);
  }, [activeSessionId, continuation, searchParams]);

  const workspaceGrant =
    workspaceApproval &&
    activeProvider &&
    workspaceApproval.providerId === activeProvider.id &&
    workspaceApproval.sessionId === activeSessionId &&
    workspaceApproval.grant.provider_version === activeProvider.version
      ? workspaceApproval.grant
      : undefined;
  useEffect(() => {
    setWorkspaceApproval(null);
    receiptAttachmentId.current = undefined;
    setActionReceiptGenerationId(undefined);
    setActionRecheckProposalId(undefined);
  }, [activeSessionId, activeProvider?.id, activeProvider?.version]);
  useEffect(() => {
    if (
      actionReceiptGenerationId &&
      acceptedCommand &&
      acceptedCommand.sequence > receiptPreparedAt.current &&
      acceptedCommand.owner === "main" &&
      acceptedCommand.sessionId === activeSessionId &&
      !!receiptAttachmentId.current &&
      acceptedCommand.actionReceiptAttachmentId ===
        receiptAttachmentId.current &&
      acceptedCommand.actionReceiptGenerationId === actionReceiptGenerationId &&
      acceptedCommand.actionRecheckProposalId === actionRecheckProposalId
    ) {
      receiptAttachmentId.current = undefined;
      setActionReceiptGenerationId(undefined);
      setActionRecheckProposalId(undefined);
      setWorkspaceApproval(null);
    }
  }, [
    acceptedCommand,
    actionReceiptGenerationId,
    actionRecheckProposalId,
    activeSessionId,
  ]);

  const continuationSource = JSON.stringify([
    activeSessionId,
    activeProvider?.id,
    activeProvider?.version,
    chat.isStreaming,
  ]);
  const continuationSourceRef = useRef({ key: continuationSource, epoch: 0 });
  if (continuationSourceRef.current.key !== continuationSource)
    continuationSourceRef.current = {
      key: continuationSource,
      epoch: continuationSourceRef.current.epoch + 1,
    };
  const continuationEpoch = continuationSourceRef.current.epoch;

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
  const confirmTask = useConfirmAiMessageTask(activeSessionId);
  const compaction = useQuery({
    queryKey: ["ai", "compaction", activeSessionId],
    queryFn: () => getAiCompactionStatus(activeSessionId),
    enabled: activeSessionId !== "",
    refetchInterval: 15_000,
    retry: 1,
  });

  useEffect(() => {
    if (chat.streamOwner?.startsWith("side:")) return;
    // Returning from a workspace result must keep the explicitly selected
    // conversation, even if another session is still generating/recovering.
    if (useAiChatStore.getState().activeSessionId) return;
    if (chat.streaming?.sessionId) setActiveSessionId(chat.streaming.sessionId);
  }, [chat.streaming?.sessionId, chat.streamOwner]);

  useEffect(() => {
    if (activeSessionId)
      useAiChatStore.setState({ lastSessionId: activeSessionId });
  }, [activeSessionId]);

  // The session list lives in the shell rail, so this pane adopts the last
  // used session and clears per-session task drafts whenever it changes.
  useEffect(() => {
    if (activeSessionId || searchParams.has("session")) return;
    if (useAiChatStore.getState().streamOwner?.startsWith("side:")) return;
    const lastSessionId = useAiChatStore.getState().lastSessionId;
    if (lastSessionId) setActiveSessionId(lastSessionId);
  }, [activeSessionId, setActiveSessionId, searchParams]);

  useEffect(() => {
    setPendingCard(null);
    setDraft(null);
  }, [activeSessionId]);

  useEffect(() => {
    if (useAiChatStore.getState().streamOwner?.startsWith("side:")) return;
    if (useAiChatStore.getState().activeSessionId) return;
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

  useEffect(() => {
    if (
      continuation?.kind === "plan" &&
      continuation.sessionId === activeSessionId
    ) {
      scrollRef.current?.scrollTo?.({ top: 0 });
      autoScrolledSessionRef.current = activeSessionId;
    }
  }, [continuation, activeSessionId]);

  const streamedText = chat.streaming?.text ?? "";
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
  const visibleRetainedTurns = retainedTurns.filter(
    (turn) =>
      turn.sessionId === activeSessionId &&
      turn.generationId !== chat.streaming?.generationId &&
      !loadedMessages.some(
        (message) =>
          message.role === "assistant" &&
          message.generation_id === turn.generationId,
      ),
  );
  const hasMessages =
    loadedMessages.length > 0 || visibleRetainedTurns.length > 0;

  useEffect(() => {
    if (
      pendingCard &&
      loadedMessages.some(
        (message) => message.id === pendingCard.messageId && message.task_id,
      )
    ) {
      setPendingCard(null);
      setDraft(null);
      setTaskError(null);
    }
  }, [loadedMessages, pendingCard]);

  function openTaskCard(message: AiMessage) {
    if (message.role !== "assistant") return;
    const suggestion = parseAiTaskSuggestion(message.content);
    if (!suggestion) return;
    setPendingCard({ messageId: message.id, suggestion });
    setDraft(draftFromSuggestion(suggestion));
    setTaskError(null);
  }

  async function confirmCreateTask() {
    if (!pendingCard || !draft || !draft.title.trim() || confirmTask.isPending)
      return;
    setTaskError(null);
    try {
      await confirmTask.mutateAsync({
        messageId: pendingCard.messageId,
        input: {
          title: draft.title.trim(),
          description: draft.description.trim() || undefined,
          priority: "P2",
          projectId: draft.projectId ?? undefined,
          dueDate: draft.dueDate ? draft.dueDate : null,
        },
      });
      setPendingCard(null);
      setDraft(null);
    } catch (error) {
      setTaskError(
        error instanceof Error && error.message
          ? `任务确认未完成：${error.message}。可重试同一建议，系统会读取已有结果，不会重复创建。`
          : "任务确认未完成，可重试同一建议，系统不会重复创建。",
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

  function prepareContinuation(
    prompt: string,
    scopes: AiWorkspaceScope[],
    generationId?: string,
    recheckProposalId?: string,
  ) {
    if (!activeProvider || chat.isStreaming) return;
    if (
      generationId &&
      (!activeSessionId || !isCanonicalWorkspaceId(generationId))
    )
      return;
    setWorkspaceApproval(null);
    clearSelectedContext();
    setContextPanelOpen(false);
    receiptAttachmentId.current = generationId
      ? crypto.randomUUID()
      : undefined;
    setActionReceiptGenerationId(generationId);
    setActionRecheckProposalId(recheckProposalId);
    receiptPreparedAt.current =
      useAiChatStore.getState().acceptedCommand?.sequence ?? 0;
    setInput((current) =>
      current.endsWith(prompt)
        ? current
        : current.trim()
          ? `${current}\n\n${prompt}`
          : prompt,
    );
    setRecommendedWorkspaceScopes(scopes);
    setWorkspaceAccessRequest((request) => request + 1);
    setContinuation(null);
  }
  const prepareContinuationRef = useRef(prepareContinuation);
  prepareContinuationRef.current = prepareContinuation;
  const continuePlan = useMemo(
    () =>
      !activeProvider || chat.isStreaming
        ? undefined
        : (prompt: string, scopes: AiWorkspaceScope[]) => {
            if (continuationSourceRef.current.epoch !== continuationEpoch)
              return;
            prepareContinuationRef.current(prompt, scopes);
          },
    [continuationEpoch, !!activeProvider, chat.isStreaming],
  );
  const continueActions = useMemo(
    () =>
      !activeProvider || chat.isStreaming
        ? undefined
        : (request: AiActionContinuation) => {
            if (
              continuationSourceRef.current.epoch !== continuationEpoch ||
              (request.sourceSessionId &&
                request.sourceSessionId !== activeSessionId) ||
              (request.recheckProposalId &&
                !isCanonicalWorkspaceId(request.recheckProposalId))
            )
              return;
            prepareContinuationRef.current(
              request.prompt,
              request.scopes,
              request.generationId,
              request.recheckProposalId,
            );
          },
    [continuationEpoch, activeSessionId, !!activeProvider, chat.isStreaming],
  );

  async function sendMessage() {
    if (isContinuationActive(automaticContinuation.data)) {
      setAutomaticSendBlocked(true);
      return;
    }
    const message = input.trim();
    const draftRevision = useAiChatStore.getState().inputRevision;
    if (
      !activeProvider ||
      !message ||
      chat.isStreaming ||
      (actionRecheckProposalId && !workspaceGrant) ||
      (contextSelectionCount > 0 && !confirmedContextIsCurrent)
    )
      return;
    const attachmentIdForRequest = receiptAttachmentId.current;
    if (filePreparing.current) return;
    const fileIdentity = fileSendIdentity.current;
    const fileSelection = useAiProjectFiles.getState().selection;
    const fileApproval = useAiProjectFiles.getState().approval;
    let projectFiles: import("../api/ai").AiProjectFileContext | undefined;
    filePreparing.current = true;
    setFileSendError(null);
    try {
      if (
        fileSelection ||
        useAiProjectFiles.getState().loading ||
        useAiProjectFiles.getState().error
      ) {
        projectFiles = await useAiProjectFiles
          .getState()
          .prepare(activeSessionId, activeProvider);
      }
      if (
        fileIdentity !== fileSendIdentity.current ||
        useAiChatStore.getState().inputRevision !== draftRevision
      ) {
        throw new Error("对话、模型或消息已变化，请重新核对后发送");
      }
      if (
        projectFiles &&
        (useAiProjectFiles.getState().selection !== fileSelection ||
          useAiProjectFiles.getState().approval !== fileApproval)
      ) {
        throw new Error("文件选择或授权已变化，请重新确认后发送");
      }
      if (projectFiles && useAiChatStore.getState().streaming) {
        throw new Error("已有生成正在运行，请等待结束后再发送文件");
      }
      // Consume on the attempt, including uncertain acceptance. Retain only
      // the local baseline for this one ephemeral review, never a new grant.
      if (projectFiles)
        useAiProjectFiles.getState().consumeForReview(activeProvider);
    } catch (error) {
      setFileSendError(error instanceof Error ? error.message : String(error));
      return;
    } finally {
      filePreparing.current = false;
    }
    const outcome = await chat.send({
      providerId: activeProvider.id,
      sessionId: activeSessionId || undefined,
      message,
      ...(projectFiles ? { projectFiles } : {}),
      workspace: workspaceGrant,
      ...(actionReceiptGenerationId ? { actionReceiptGenerationId } : {}),
      ...(actionReceiptGenerationId && attachmentIdForRequest
        ? { actionReceiptAttachmentId: attachmentIdForRequest }
        : {}),
      ...(actionRecheckProposalId ? { actionRecheckProposalId } : {}),
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
    const sameAttachment =
      receiptAttachmentId.current === attachmentIdForRequest;
    if (
      sameAttachment &&
      (outcome.accepted ||
        outcome.errorCode === "AI_WORKSPACE_PROVIDER_CHANGED")
    ) {
      setWorkspaceApproval(null);
    }
    if (outcome.accepted && sameAttachment) {
      receiptAttachmentId.current = undefined;
      setActionReceiptGenerationId(undefined);
      setActionRecheckProposalId(undefined);
    }
    // Preserve a rejected/uncertain draft. Accepted messages are in the server
    // history and must not be silently resent. Never overwrite newer typing.
    if (
      outcome.accepted &&
      useAiChatStore.getState().inputRevision === draftRevision
    )
      setInput((current) => (current.trim() === message ? "" : current));
    if (
      outcome.sessionId &&
      outcome.sessionId !== activeSessionId &&
      useAiChatStore.getState().activeSessionId === activeSessionId
    ) {
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
    if (outcome.accepted && !outcome.error) {
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
        <section className="ai-chat-main">
          <header className="ai-chat-header">
            <div className="ai-mobile-session-controls">
              <select
                aria-label="选择会话"
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
                aria-label="新建会话"
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
              <ModeSwitch />
            </div>
            <div className="ai-chat-heading">
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
            {agentRailCollapsed ? (
              <button
                aria-controls="agent-sidebar"
                aria-expanded={false}
                aria-label="显示会话侧边栏"
                className="icon-button ai-sidebar-toggle ai-left-sidebar-toggle"
                onClick={toggleAgentRailCollapsed}
                title="显示会话侧边栏"
                type="button"
              >
                <PanelLeftOpen aria-hidden="true" size={16} />
              </button>
            ) : null}
            {rightOverviewCollapsed ? (
              <button
                aria-controls="right-overview"
                aria-expanded={false}
                aria-label="打开右侧工作栏"
                className="icon-button ai-sidebar-toggle"
                onClick={toggleRightOverviewCollapsed}
                title="打开右侧工作栏"
                type="button"
              >
                <PanelRightOpen aria-hidden="true" size={16} />
              </button>
            ) : null}
          </header>

          <div className="ai-chat-messages" ref={scrollRef}>
            <div className="ai-chat-messages-inner">
              <AiWorkPlan
                key={activeSessionId}
                sessionId={activeSessionId ?? ""}
                focusRequest={
                  continuation?.kind === "plan" &&
                  continuation.sessionId === activeSessionId
                    ? continuation.request
                    : 0
                }
                onContinue={continuePlan}
                onContinueActions={continueActions}
                live={
                  chat.isStreaming &&
                  chat.streaming?.sessionId === activeSessionId
                }
              />
              <AiContinuationSendNotice lease={automaticContinuation.data} />
              {automaticSendBlocked ? (
                <p role="alert">
                  该会话正在自动续办，请先停止自动续办后再发送。草稿和单次权限仍保留，不会自动发送。
                </p>
              ) : null}
              <AiWorkspaceRecordNavigation sessionId={activeSessionId} />
              {continuation?.kind === "approval" &&
              continuation.sessionId === activeSessionId ? (
                <AiApprovalContinuation
                  key={continuation.request}
                  target={continuation}
                  onContinue={continueActions}
                  onClose={() => setContinuation(null)}
                />
              ) : null}
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
                      attachedTaskTitle={message.task_title_snapshot}
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
                      origin={message.origin}
                      key={message.id}
                      messageId={message.id}
                      sessionId={message.session_id}
                      onOpenTaskCard={() => openTaskCard(message)}
                      onContinueActions={
                        message.session_id === activeSessionId
                          ? continueActions
                          : undefined
                      }
                      reasoning={message.reasoning}
                      role={message.role}
                      status={message.status}
                    />
                  ))}
                </>
              )}
              <AiAutomaticReply
                sessionId={activeSessionId}
                knownGenerationIds={loadedMessages
                  .filter((m) => m.role === "assistant")
                  .map((m) => m.generation_id ?? "")}
              />
              {visibleRetainedTurns.length > 0 ? (
                <p role="status">
                  以下回复仅在当前窗口内存中保留，刷新后可能丢失（最多 20
                  回合、8 MiB）。
                  仅供查看，创建任务或保存永久记忆需要在持久会话或相应页面另行确认。
                </p>
              ) : null}
              {visibleRetainedTurns.map((turn) => (
                <div key={turn.generationId}>
                  {turn.userText ? (
                    <div className="ai-msg-user">
                      <div className="ai-bubble-user">{turn.userText}</div>
                    </div>
                  ) : null}
                  <AiMessageBlock
                    readOnly
                    role="assistant"
                    messageId=""
                    sessionId={turn.sessionId}
                    content={turn.text}
                    reasoning={turn.reasoning}
                    createdAt={turn.createdAt}
                    status={turn.status}
                    attachedTaskId={null}
                    attachedTaskTitle={null}
                    contextProvider={null}
                    contextSources={[]}
                    contextKnowledge={[]}
                    citationStatus={
                      turn.citationEvidence?.status ??
                      (turn.status === "completed" ||
                      turn.status === "incomplete"
                        ? null
                        : "not_requested")
                    }
                    citations={turn.citationEvidence?.items ?? []}
                    generationId={null}
                    onOpenTaskCard={() => {}}
                  />
                  <AiRunProgress steps={turn.progress} />
                </div>
              ))}
              {chat.interrupted?.sessionId === activeSessionId &&
              !visibleRetainedTurns.some(
                (turn) => turn.generationId === chat.interrupted?.generationId,
              ) &&
              !loadedMessages.some(
                (message) =>
                  message.generation_id === chat.interrupted?.generationId,
              ) ? (
                <div className="ai-msg-text" data-status="failed">
                  <AiRunProgress steps={chat.interrupted.progress} />
                  {chat.interrupted.reasoning ? (
                    <AiThinkingProcess
                      reasoning={chat.interrupted.reasoning}
                      live={false}
                    />
                  ) : null}
                  {renderAiRichText(
                    displayAiReply(chat.interrupted.text, true),
                    chat.interrupted.sessionId,
                  )}
                  <p>（连接已中断，内容不完整；可重新读取会话历史）</p>
                </div>
              ) : null}
              {compaction.data?.status === "running" ? (
                <span role="status">正在整理早期对话…</span>
              ) : null}
              {compaction.data?.status === "pending" ? (
                <span role="status">历史整理待继续，下次对话后会再次尝试</span>
              ) : null}
              {compaction.data?.partial_message ? (
                <span role="status">较长消息正在分段整理，尚未全部压缩</span>
              ) : null}
              {compaction.data?.status === "failed" ? (
                <span role="status">
                  早期对话整理失败，当前使用最近对话；后续会再次尝试
                </span>
              ) : null}
              {compaction.data?.status === "cancelled" ? (
                <span role="status">早期对话整理已停止，已完成的进度保留</span>
              ) : null}
              {chat.isStreaming &&
              (!chat.streaming?.sessionId ||
                chat.streaming.sessionId === activeSessionId) ? (
                <>
                  {chat.sentMessage !== null &&
                  !(
                    loadedMessages.at(-1)?.role === "user" &&
                    loadedMessages.at(-1)?.content === chat.sentMessage
                  ) ? (
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
                    <div className="ai-msg-body">
                      <AiRunProgress
                        steps={chat.streaming?.progress}
                        live={!chat.streamError}
                      />
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
                              chat.streaming?.sessionId,
                            )
                          : (chat.streaming?.reasoning ?? "").length > 0 ||
                              chat.streaming?.progress?.length
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
                  disabled={confirmTask.isPending || !draft.title.trim()}
                  onClick={() => void confirmCreateTask()}
                  type="button"
                >
                  {confirmTask.isPending ? (
                    <LoaderCircle className="animate-spin" size={14} />
                  ) : (
                    <CheckCircle2 size={14} />
                  )}
                  确认创建
                </button>
                <button
                  className="button button-quiet"
                  disabled={confirmTask.isPending}
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
              <AiAccessRequestPrompt
                sessionId={activeSessionId}
                messages={loadedMessages}
                liveRequest={liveAccessRequest}
                continuation={automaticContinuation.data}
                streamingGenerationId={
                  chat.streaming?.sessionId === activeSessionId
                    ? chat.streaming.generationId
                    : undefined
                }
                disabled={!activeProvider || chat.isStreaming}
                onPrepare={(scopes) => {
                  if (!activeProvider || chat.isStreaming) return;
                  prepareContinuation(accessRequestContinuationPrompt, scopes);
                }}
                onDismissLive={(generationId) =>
                  dismissAccessRequest(activeSessionId, generationId)
                }
              />
              <AiProjectFileReview
                sessionId={activeSessionId}
                provider={activeProvider}
              />
              <AiProjectFileContext
                sessionId={activeSessionId}
                provider={activeProvider}
                disabled={chat.isStreaming}
              />
              {fileSendError && (
                <p className="ws-error" role="alert">
                  {fileSendError}
                </p>
              )}
              <AiWorkbenchHandoffCard
                disabled={!activeProvider || chat.isStreaming}
                onPrepare={(handoff) => {
                  const detail = workbenchHandoffDetails(
                    handoff.type,
                    handoff.id,
                  );
                  if (!detail || !activeProvider || chat.isStreaming) return;
                  // A new workbench intent never carries earlier grants or
                  // unrelated context into the next message.
                  setWorkspaceApproval(null);
                  setActionReceiptGenerationId(undefined);
                  setActionRecheckProposalId(undefined);
                  clearSelectedContext();
                  setContextPanelOpen(false);
                  setInput((current) =>
                    current.endsWith(detail.prompt)
                      ? current
                      : current.trim()
                        ? `${current}\n\n${detail.prompt}`
                        : detail.prompt,
                  );
                  setRecommendedWorkspaceScopes(detail.scopes);
                  setWorkspaceAccessRequest((request) => request + 1);
                }}
              />
              <AiIssueHandoffCard
                disabled={!activeProvider || chat.isStreaming}
                onPrepare={(handoff) => {
                  if (!activeProvider || chat.isStreaming) return;
                  // A new queue intent never carries earlier grants or
                  // unrelated context into the next message.
                  setWorkspaceApproval(null);
                  setActionReceiptGenerationId(undefined);
                  setActionRecheckProposalId(undefined);
                  clearSelectedContext();
                  setContextPanelOpen(false);
                  setInput((current) =>
                    current.endsWith(handoff.prompt)
                      ? current
                      : current.trim()
                        ? `${current}\n\n${handoff.prompt}`
                        : handoff.prompt,
                  );
                  if (handoff.scopes.length > 0) {
                    setRecommendedWorkspaceScopes(handoff.scopes);
                    setWorkspaceAccessRequest((request) => request + 1);
                  }
                }}
              />
              {actionReceiptGenerationId ? (
                <AiActionReceiptAttachment
                  disabled={chat.isStreaming}
                  recheckProposalId={actionRecheckProposalId}
                  onRemove={() => {
                    receiptAttachmentId.current = undefined;
                    setActionReceiptGenerationId(undefined);
                    setActionRecheckProposalId(undefined);
                  }}
                />
              ) : null}
              <div className="ai-composer-box">
                <textarea
                  aria-label="消息输入框"
                  className="ai-composer-textarea"
                  disabled={!activeProvider || chat.isStreaming}
                  onChange={(event) => setInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (
                      event.key === "Enter" &&
                      !event.shiftKey &&
                      !event.nativeEvent.isComposing &&
                      event.nativeEvent.keyCode !== 229
                    ) {
                      event.preventDefault();
                      void sendMessage();
                    }
                  }}
                  placeholder={
                    activeProvider ? "向 AI 助手提问…" : "配置 AI 供应商后可用"
                  }
                  rows={3}
                  title="Enter 发送，Shift+Enter 换行"
                  value={input}
                />
                <div className="ai-composer-tool-row">
                  <div className="ai-composer-tools-left">
                    {activeProvider ? (
                      <AiWorkspaceAccess
                        key={`${activeProvider.id}:${activeProvider.version}:${activeSessionId}`}
                        provider={activeProvider}
                        openRequest={workspaceAccessRequest}
                        recommendedScopes={recommendedWorkspaceScopes}
                        persist={activeSession?.persist ?? true}
                        value={workspaceGrant}
                        disabled={chat.isStreaming}
                        onChange={(grant) =>
                          setWorkspaceApproval(
                            grant
                              ? {
                                  providerId: activeProvider.id,
                                  sessionId: activeSessionId,
                                  grant,
                                }
                              : null,
                          )
                        }
                      />
                    ) : null}
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
                        <Plus size={16} />
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
                        (!!actionRecheckProposalId && !workspaceGrant) ||
                        (contextSelectionCount > 0 &&
                          !confirmedContextIsCurrent)
                      }
                      onClick={() => void sendMessage()}
                      type="button"
                    >
                      <ArrowUp size={19} />
                    </button>
                  )}
                </div>
                <AiWorkspaceGrantSummary value={workspaceGrant} />
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
  review_policy: "验收方式",
  review_policy_change_allowed: "当前允许切换验收方式",
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

function displayAIContextValue(value: unknown, field?: string): string {
  if (value === null || value === undefined || value === "") return "未设置";
  if (field === "review_policy") {
    if (value === "manual") return "人工验收";
    if (value === "none") return "无需人工验收";
  }
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
                  <dd>{displayAIContextValue(value, field)}</dd>
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

const aiRunStepKindLabels: Record<string, string> = {
  generation: "本次生成",
  model_turn: "模型轮次",
  tool_call: "工具调用",
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
                        {step.toolName
                          ? ` · ${aiToolLabel(step.toolName) ?? step.toolName}`
                          : ""}
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
  origin,
  role,
  messageId,
  sessionId,
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
  attachedTaskTitle,
  onOpenTaskCard,
  onContinueActions,
  readOnly = false,
}: {
  origin?: AiMessage["origin"];
  role: AiMessage["role"];
  messageId: string;
  sessionId: string;
  content: string;
  contextProvider: AiMessage["context_provider"];
  contextSources: AiBusinessContextSource[];
  contextKnowledge: AiKnowledgeContextSource[];
  citationStatus: AiCitationStatus | null;
  citations: AiCitation[];
  generationId: string | null;
  reasoning: string | null;
  createdAt: string;
  status: AiMessage["status"] | "incomplete";
  attachedTaskId: string | null;
  attachedTaskTitle: string | null;
  onOpenTaskCard: () => void;
  onContinueActions?: (request: AiActionContinuation) => void;
  readOnly?: boolean;
}) {
  const navigate = useNavigate();
  const actions = useAiWorkspaceActions(
    role === "assistant" && !readOnly ? generationId : null,
  );
  const hasActions = !!actions.data?.length;
  const allowLegacyTask =
    !generationId || readOnly || (actions.isSuccess && !hasActions);
  if (role === "user") {
    return (
      <div className="ai-msg-user">
        <div className="ai-user-message-stack">
          <AiAutomaticOrigin origin={origin} />
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
  const display = attachedTaskId
    ? `已创建任务「${attachedTaskTitle ?? suggestion?.title ?? "新任务"}」，可以从下方打开查看。`
    : suggestion && hasActions
      ? "工作台操作建议及实际执行状态见下方。"
      : suggestion
        ? status === "completed"
          ? readOnly
            ? `已整理任务建议「${suggestion.title}」，尚未创建。此回复仅供查看，请另行确认后创建。`
            : `已整理任务建议「${suggestion.title}」，尚未创建。请确认下面的信息后创建。`
          : "回复尚未完成，任务未创建。"
        : parseAiMemorySuggestion(content)
          ? readOnly
            ? "我整理了一条记忆建议，尚未保存。此回复仅供查看，请另行确认后添加永久记忆。"
            : "我整理了一条记忆建议，保存状态见下方。"
          : displayAiReply(content);
  return (
    <div className="ai-msg">
      <div className="ai-msg-body">
        <AiAutomaticOrigin origin={origin} />
        {reasoning ? (
          <AiThinkingProcess reasoning={reasoning} live={false} />
        ) : null}
        <div className="ai-msg-text" data-status={status}>
          {status !== "completed" && !attachedTaskId ? (
            <p>以下仅为未完成的模型草稿，不代表任务已创建或永久记忆已保存。</p>
          ) : null}
          {status === "cancelled" && display === ""
            ? "（已停止生成）"
            : renderAiRichText(display, sessionId)}
          {status === "cancelled" && display !== ""
            ? "（已停止生成，内容不完整）"
            : null}
          {status === "failed" ? "（生成失败，内容可能不完整）" : null}
          {status === "incomplete"
            ? "（生成已结束，但完整回复未保存；这里只保留收到的片段）"
            : null}
        </div>
        <AiCitationEvidence
          citations={citations}
          status={citationStatus}
          sessionId={sessionId}
          incomplete={status === "incomplete"}
        />
        {generationId ? <AiRunTimeline generationId={generationId} /> : null}
        {generationId && !readOnly ? (
          <AiWorkspaceActions
            generationId={generationId}
            sessionId={sessionId}
            onContinue={onContinueActions}
          />
        ) : null}
        {attachedTaskId ? (
          <AiTaskCreatedCard
            onOpen={() => navigate(`/tasks/${attachedTaskId}`)}
            taskId={attachedTaskId}
            fallbackTitle={content}
          />
        ) : allowLegacyTask &&
          suggestion &&
          status === "completed" &&
          !readOnly ? (
          <button
            className="ai-action-chip"
            onClick={onOpenTaskCard}
            type="button"
          >
            <Sparkles size={13} />
            建议任务：{suggestion.title}（点击确认创建）
          </button>
        ) : null}
        {status === "completed" && !readOnly ? (
          <AiMemorySuggestionCard
            content={content}
            messageId={messageId}
            sessionId={sessionId}
          />
        ) : null}
        {status === "completed" && display ? (
          <AiCopyButton text={display} />
        ) : null}
      </div>
    </div>
  );
}

function displayAiReply(content: string, streaming = false): string {
  const display = stripAiTaskBlock(content).trim();
  if (
    !streaming &&
    ((/\[opc:task\]/i.test(content) && !parseAiTaskSuggestion(content)) ||
      (/\[opc:memory\]/i.test(content) && !parseAiMemorySuggestion(content)))
  ) {
    return `${display}${display ? "\n\n" : ""}这条建议格式不完整，未执行任何操作，请重新生成。`;
  }
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
  return /\[\/?opc(?::|$)/i.test(content) ? display : content;
}

// AiMemorySuggestionCard shows the user-confirmation gate for a remembered
// preference: model output is an untrusted suggestion and nothing is stored
// until the user confirms (ADR-006).
function AiMemorySuggestionCard({
  content,
  messageId,
  sessionId,
}: {
  content: string;
  messageId: string;
  sessionId: string;
}) {
  const createMemory = useCreateAiMemory();
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(false);
  const memory = parseAiMemorySuggestion(content);
  const decision = useQuery({
    queryKey: [
      "ai",
      "memory-proposals",
      "decision",
      memory?.proposalId ?? messageId,
    ],
    queryFn: () => getAiMemoryDecision(memory?.proposalId, messageId),
    enabled: !!memory,
    retry: 1,
  });
  const rejectMemory = useMutation({
    mutationFn: () =>
      memory?.proposalId
        ? rejectAiMemoryProposal(memory.proposalId)
        : rejectAiMessageMemory(messageId),
    onSettled: async () => {
      await queryClient.invalidateQueries({
        queryKey: ["ai", "memory-proposals"],
      });
    },
  });
  if (!memory) return null;
  if (
    decision.data &&
    (decision.data.session_id !== sessionId ||
      decision.data.content !== memory.content)
  ) {
    return (
      <div className="ai-memory-error" role="alert">
        记忆建议与当前会话不一致，未执行任何操作。
      </div>
    );
  }
  if (decision.data?.status === "rejected")
    return (
      <div className="ai-memory-card" role="status">
        已忽略这条记忆建议
      </div>
    );
  if (saved || decision.data?.status === "confirmed") {
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
        disabled={
          createMemory.isPending ||
          rejectMemory.isPending ||
          !decision.data ||
          decision.data.status !== "pending"
        }
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
        disabled={
          createMemory.isPending ||
          rejectMemory.isPending ||
          !decision.data ||
          decision.data.status !== "pending"
        }
        onClick={() => void rejectMemory.mutateAsync().catch(() => {})}
        type="button"
      >
        忽略
      </button>
      {decision.isPending ? <span role="status">正在读取建议状态…</span> : null}
      {decision.isError ? (
        <button type="button" onClick={() => void decision.refetch()}>
          状态读取失败，重试
        </button>
      ) : null}
      {createMemory.error || rejectMemory.error ? (
        <span className="ai-memory-error" role="alert">
          操作未确认，请重试
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
