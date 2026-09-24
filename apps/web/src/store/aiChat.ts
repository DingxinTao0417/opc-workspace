import { create } from "zustand";
import { AiChatAlreadyAccepted, streamAiChat } from "../api/ai";
import { ApiError } from "../api/client";
import { appendAiRunProgress } from "../api/aiProgress";
import { useUiStore } from "./ui";
import { useAiProjectFileReview } from "./aiProjectFileReview";
import {
  cancelAiGeneration,
  getActiveAiGenerations,
  getAiGeneration,
  getAiGenerationByRequest,
  type AiGeneration,
} from "../api/aiActions";
import type {
  AiBusinessContextSelection,
  AiCitationResult,
  AiRunProgress,
  AiWorkspaceScope,
} from "../types/models";

export interface AiChatStreamState {
  sessionId: string;
  generationId?: string;
  providerId?: string;
  text: string;
  reasoning: string;
  citationEvidence?: AiCitationResult;
  progress?: AiRunProgress[];
}

export interface AiRetainedTurn extends AiChatStreamState {
  generationId: string;
  userText: string | null;
  status: "completed" | "cancelled" | "failed" | "incomplete";
  createdAt: string;
}

export interface AiChatStreamOutcome {
  sessionId: string;
  cancelled: boolean;
  accepted: boolean;
  error: string | null;
  errorCode: string | null;
}

export interface AiAcceptedCommand {
  sequence: number;
  requestId: string;
  owner: string;
  sessionId: string;
  actionReceiptGenerationId?: string;
  actionRecheckProposalId?: string;
  actionReceiptAttachmentId?: string;
}

export interface AiAccessRequest {
  generationId: string;
  scopes: AiWorkspaceScope[];
}

interface ChatInput {
  owner?: string;
  providerId: string;
  sessionId?: string;
  message: string;
  context?: AiBusinessContextSelection;
  workspace?: import("../types/models").AiWorkspaceGrant;
  projectFiles?: import("../api/ai").AiProjectFileContext;
  actionReceiptGenerationId?: string;
  actionRecheckProposalId?: string;
  // Local attachment instance, never serialized by the API or persisted.
  actionReceiptAttachmentId?: string;
}

interface ChatStore {
  streamOwner: string;
  // Runtime-only acceptance identity, including acceptance discovered later by
  // recovery. It must never contain the prompt, grant or context body.
  acceptedCommand: AiAcceptedCommand | null;
  streaming: AiChatStreamState | null;
  interrupted: AiChatStreamState | null;
  retainedTurns: AiRetainedTurn[];
  streamError: string | null;
  sentMessage: string | null;
  activeGenerations: AiGeneration[];
  // Per-session metadata only. It invalidates the local work-plan query after
  // a server-confirmed revision; no plan body or workspace content is stored.
  workPlanVersions: Record<string, number>;
  // Live UI recommendation only. It is not an authorization and is never
  // persisted or inherited by a later generation.
  accessRequests: Record<string, AiAccessRequest>;
  lastSessionId: string;
  activeSessionId: string;
  input: string;
  inputRevision: number;
  setActiveSessionId: (sessionId: string) => void;
  setInput: (value: string | ((current: string) => string)) => void;
  send: (input: ChatInput) => Promise<AiChatStreamOutcome>;
  stop: () => Promise<void>;
  recover: () => Promise<void>;
  forgetSession: (sessionId: string) => void;
  dismissAccessRequest: (sessionId: string, generationId: string) => void;
}

const requestStorageKey = "opc-ai-pending-request-v1";
let controller: AbortController | null = null;
let uncertainRequest: {
  fingerprint: string;
  requestId: string;
  message: string;
  draftRevision: number;
  input: ChatInput;
} | null = null;
let refresh: (sessionId: string) => Promise<void> = async () => {};
let recovering = false;
// A pending status lookup never owns a newer command or stream terminal.
let commandRevision = 0;
let recoveryRevision = 0;
const retainedTurnLimit = 20;
const retainedByteLimit = 8 * 1024 * 1024;

function acceptedCommandMetadata(
  previous: AiAcceptedCommand | null,
  requestId: string,
  input: ChatInput,
  sessionId: string,
): AiAcceptedCommand {
  if (previous?.requestId === requestId) return previous;
  return {
    sequence: (previous?.sequence ?? 0) + 1,
    requestId,
    owner: input.owner ?? "main",
    sessionId,
    ...(input.actionReceiptGenerationId
      ? { actionReceiptGenerationId: input.actionReceiptGenerationId }
      : {}),
    ...(input.actionRecheckProposalId
      ? { actionRecheckProposalId: input.actionRecheckProposalId }
      : {}),
    ...(input.actionReceiptAttachmentId
      ? { actionReceiptAttachmentId: input.actionReceiptAttachmentId }
      : {}),
  };
}

function retainTurn(turns: AiRetainedTurn[], turn: AiRetainedTurn) {
  const previous = turns.find(
    (item) => item.generationId === turn.generationId,
  );
  const next = [
    ...turns.filter((item) => item.generationId !== turn.generationId),
    {
      ...turn,
      userText: turn.userText ?? previous?.userText ?? null,
      createdAt: previous?.createdAt ?? turn.createdAt,
    },
  ];
  const encoder = new TextEncoder();
  let bytes = 0;
  let keepFrom = next.length;
  for (let index = next.length - 1; index >= 0; index--) {
    const item = next[index];
    bytes +=
      encoder.encode(item.text).byteLength +
      encoder.encode(item.reasoning).byteLength +
      encoder.encode(item.userText ?? "").byteLength +
      encoder.encode(JSON.stringify(item.citationEvidence ?? null)).byteLength +
      encoder.encode(JSON.stringify(item.progress ?? null)).byteLength;
    if (bytes > retainedByteLimit || next.length - index > retainedTurnLimit)
      break;
    keepFrom = index;
  }
  return next.slice(keepFrom);
}

// Only identifiers survive a reload. Prompt, response, reasoning and drafts
// remain in application memory; non-persistent bodies never reach web storage.
function savedRequest(): { requestId: string; sessionId: string } | null {
  try {
    const value: unknown = JSON.parse(
      sessionStorage.getItem(requestStorageKey) ?? "null",
    );
    if (
      value &&
      typeof value === "object" &&
      "requestId" in value &&
      typeof value.requestId === "string"
    ) {
      return {
        requestId: value.requestId,
        sessionId:
          "sessionId" in value && typeof value.sessionId === "string"
            ? value.sessionId
            : "",
      };
    }
  } catch {
    /* unavailable browser storage must not prevent chat */
  }
  return null;
}

function saveRequest(requestId: string, sessionId: string) {
  try {
    sessionStorage.setItem(
      requestStorageKey,
      JSON.stringify({ requestId, sessionId }),
    );
  } catch {
    /* optional recovery metadata */
  }
}

function clearRequest() {
  try {
    sessionStorage.removeItem(requestStorageKey);
  } catch {
    /* optional recovery metadata */
  }
}

function isActive(generation: AiGeneration) {
  return generation.status === "queued" || generation.status === "streaming";
}

function visibleGeneration(generation: AiGeneration): AiChatStreamState {
  return {
    sessionId: generation.session_id,
    generationId: generation.id,
    providerId: generation.provider_id,
    text: generation.content,
    reasoning: generation.reasoning,
    citationEvidence: generation.citationEvidence,
    progress: generation.progress,
  };
}

export const aiStreamErrorHints: Record<string, string> = {
  AI_PROJECT_FILES_PROVIDER_CHANGED: "模型配置已变化，请重新核对文件外发授权。",
  AI_PROJECT_FILES_CONSENT_INVALID:
    "文件授权与当前对话不一致，请重新选择并确认。",
  AI_PROJECT_FILES_EXPIRED: "文件授权已过期，请重新读取并确认。",
  AI_PROJECT_FILES_INVALID: "文件路径或精确内容校验失败，请重新读取。",
  AI_PROJECT_FILES_TOO_LARGE:
    "完整文件超过 32 KiB 外发上限，请选择较小的完整文件。",
  AI_PROJECT_FILES_HEADLESS:
    "后台续办不能使用项目文件，请在桌面端重新确认并发送。",
  AI_CONTINUATION_ACTIVE:
    "该会话正在自动续办。请先点击“停止自动续办”，确认停止后再手动发送；草稿和单次授权未被自动重放。",
  AI_KEY_INVALID: "API 密钥被上游拒绝，请到 设置 → AI 助手 重新保存密钥",
  AI_ENDPOINT_INVALID: "端点路径不存在，请检查供应商的 Base URL",
  AI_PROVIDER_ERROR: "上游服务返回错误，请稍后重试或更换模型",
  AI_STREAM_ERROR: "AI 回答流中断，内容可能不完整",
  AI_STREAM_INCOMPLETE: "AI 回答流提前结束，已收到的内容不完整",
  AI_RESPONSE_TRUNCATED: "模型输出达到长度上限，回答已截断",
  AI_RESPONSE_FILTERED: "模型未能完成这条回答，已收到的内容可能不完整",
  AI_RESPONSE_BUDGET_EXHAUSTED: "本次生成达到累计响应上限，已保留部分内容",
  AI_TURN_BUDGET_EXHAUSTED: "本次生成达到模型轮数上限，尚未完成",
  AI_TOOL_BUDGET_EXHAUSTED: "本次生成达到工具调用上限，尚未完成",
  AI_ENDPOINT_UNREACHABLE: "无法连接上游端点，请检查网络或代理",
  AI_GENERATION_TIMEOUT: "生成超时，请重试或更换更快的模型",
  AI_PROMPT_TOO_LARGE: "当前消息和上下文超过提示词上限，请缩短内容后重试",
  AI_KEY_NOT_ALLOWED: "本地部署供应商不需要 API 密钥，请检查供应商类型配置",
  AI_CONTEXT_CHANGED: "所选工作区上下文已变化，请重新预览后再发送",
  AI_CONTEXT_PROVIDER_CHANGED: "AI 供应商配置已变化，请重新预览上下文",
  AI_WORKSPACE_PROVIDER_CHANGED: "AI 供应商已变化，请重新确认本次工作台权限",
  AI_ACTION_RECEIPT_SOURCE_INVALID:
    "操作回执来源无效，请移除后从原操作建议重新继续",
  AI_ACTION_RECEIPT_SOURCE_UNAVAILABLE:
    "原操作回执暂不可用，请从原会话刷新状态后重新继续",
  AI_ACTION_RECHECK_SOURCE_INVALID:
    "重新核验来源无效，请移除附件后从原建议重新准备",
  AI_ACTION_RECHECK_SOURCE_UNAVAILABLE:
    "原建议未撤回或已不可用，请从原会话重新核对；没有发送原操作参数",
  AI_ACTION_RECHECK_SCOPE_REQUIRED:
    "重新核验原参数需要本条消息重新授权对应范围；含文件的执行还需单独选择文件权限",
  AI_WORKSPACE_GRANT_INVALID:
    "工作台授权范围无效，请重新选择；执行产出和操作建议需要同时授权工作事项",
  AI_ACTION_PERSIST_REQUIRED:
    "当前会话不保存记录，无法保留操作审批。请开启会话保存，或取消操作建议权限后发送",
  AI_CONTEXT_SOURCE_NOT_FOUND: "所选工作区上下文已不存在，请重新选择",
  AI_CONTEXT_TOO_LARGE: "所选工作区上下文超过 16 KiB，请减少选择",
};

export function configureAiChatRefresh(callback: typeof refresh) {
  refresh = callback;
}

export const useAiChatStore = create<ChatStore>((set, get) => ({
  streamOwner: "main",
  acceptedCommand: null,
  streaming: null,
  interrupted: null,
  retainedTurns: [],
  streamError: null,
  sentMessage: null,
  activeGenerations: [],
  workPlanVersions: {},
  accessRequests: {},
  lastSessionId: "",
  activeSessionId: "",
  input: "",
  inputRevision: 0,
  setActiveSessionId: (activeSessionId) => set({ activeSessionId }),
  setInput: (value) =>
    set((state) => ({
      input: typeof value === "function" ? value(state.input) : value,
      inputRevision: state.inputRevision + 1,
    })),
  forgetSession: (sessionId) => {
    ++recoveryRevision;
    if (savedRequest()?.sessionId === sessionId) clearRequest();
    if (uncertainRequest?.input.sessionId === sessionId)
      uncertainRequest = null;
    set((state) => {
      const workPlanVersions = Object.fromEntries(
        Object.entries(state.workPlanVersions).filter(
          ([id]) => id !== sessionId,
        ),
      );
      const accessRequests = Object.fromEntries(
        Object.entries(state.accessRequests).filter(([id]) => id !== sessionId),
      );
      return {
        retainedTurns: state.retainedTurns.filter(
          (turn) => turn.sessionId !== sessionId,
        ),
        interrupted:
          state.interrupted?.sessionId === sessionId ? null : state.interrupted,
        workPlanVersions,
        accessRequests,
        lastSessionId:
          state.lastSessionId === sessionId ? "" : state.lastSessionId,
        activeSessionId:
          state.activeSessionId === sessionId ? "" : state.activeSessionId,
      };
    });
  },
  dismissAccessRequest: (sessionId, generationId) =>
    set((state) => {
      if (state.accessRequests[sessionId]?.generationId !== generationId)
        return state;
      const accessRequests = { ...state.accessRequests };
      delete accessRequests[sessionId];
      return { accessRequests };
    }),
  send: async (requestedInput) => {
    // A lost response may have allocated the first session while its meta
    // frame never reached this window. Retry the original command body, not
    // the newly selected session ID discovered by a background refresh.
    const previousInput = uncertainRequest?.input;
    const input =
      previousInput &&
      !previousInput.sessionId &&
      (previousInput.owner ?? "main") === (requestedInput.owner ?? "main") &&
      previousInput.providerId === requestedInput.providerId &&
      previousInput.message === requestedInput.message &&
      previousInput.actionReceiptGenerationId ===
        requestedInput.actionReceiptGenerationId &&
      previousInput.actionRecheckProposalId ===
        requestedInput.actionRecheckProposalId &&
      previousInput.actionReceiptAttachmentId ===
        requestedInput.actionReceiptAttachmentId &&
      JSON.stringify(previousInput.context) ===
        JSON.stringify(requestedInput.context) &&
      JSON.stringify(previousInput.workspace) ===
        JSON.stringify(requestedInput.workspace) &&
      JSON.stringify(previousInput.projectFiles) ===
        JSON.stringify(requestedInput.projectFiles)
        ? previousInput
        : requestedInput;
    if (get().streaming || controller)
      return {
        sessionId: input.sessionId ?? "",
        cancelled: false,
        accepted: false,
        error: "已有生成正在运行，请先停止",
        errorCode: "AI_PROVIDER_BUSY",
      };
    const fingerprint = JSON.stringify(input);
    const currentRevision = ++commandRevision;
    ++recoveryRevision;
    const requestId =
      uncertainRequest?.fingerprint === fingerprint
        ? uncertainRequest.requestId
        : crypto.randomUUID();
    uncertainRequest = {
      fingerprint,
      requestId,
      message: input.message,
      draftRevision: get().inputRevision,
      input,
    };
    saveRequest(requestId, input.sessionId ?? "");
    const currentController = new AbortController();
    useAiProjectFileReview
      .getState()
      .bind(
        requestId,
        (input.owner ?? "main") === "main" ? input.projectFiles : undefined,
      );
    controller = currentController;
    let sessionId = input.sessionId ?? "";
    let generationId = "";
    let accepted = false;
    let cancelled = false;
    let failure: string | null = null;
    let failureCode: string | null = null;
    let recoveringAccepted = false;
    let terminalStatus: AiRetainedTurn["status"] | null = null;
    set({
      streamOwner: input.owner ?? "main",
      streaming: {
        sessionId,
        providerId: input.providerId,
        text: "",
        reasoning: "",
      },
      sentMessage: input.message,
      streamError: null,
      interrupted: null,
    });
    try {
      await streamAiChat({
        ...input,
        requestId,
        signal: currentController.signal,
        onEvent: (event) => {
          if (event.type !== "meta" && event.generationId !== generationId)
            throw new ApiError("生成流身份不一致，未接收该回复", {
              code: "AI_STREAM_IDENTITY_MISMATCH",
            });
          if (event.type === "meta") {
            useAiProjectFileReview.getState().meta(requestId, event.meta);
            if (
              accepted &&
              (event.meta.generation_id !== generationId ||
                (event.meta.session_id && event.meta.session_id !== sessionId))
            )
              throw new ApiError("生成流身份发生变化，未接收该回复", {
                code: "AI_STREAM_IDENTITY_MISMATCH",
              });
            sessionId = event.meta.session_id || sessionId;
            generationId = event.meta.generation_id;
            accepted = true;
            saveRequest(requestId, sessionId);
            set((state) => ({
              acceptedCommand: acceptedCommandMetadata(
                state.acceptedCommand,
                requestId,
                input,
                sessionId,
              ),
              lastSessionId: sessionId,
              accessRequests: Object.fromEntries(
                Object.entries(state.accessRequests).filter(
                  ([id]) => id !== sessionId,
                ),
              ),
              streaming: {
                sessionId,
                generationId,
                providerId: input.providerId,
                text: "",
                reasoning: "",
              },
            }));
          } else if (event.type === "delta" || event.type === "reasoning") {
            set((state) => ({
              streaming: state.streaming
                ? {
                    ...state.streaming,
                    [event.type === "delta" ? "text" : "reasoning"]:
                      state.streaming[
                        event.type === "delta" ? "text" : "reasoning"
                      ] + event.text,
                  }
                : null,
            }));
          } else if (event.type === "progress") {
            set((state) => ({
              streaming: state.streaming
                ? {
                    ...state.streaming,
                    progress: appendAiRunProgress(
                      state.streaming.progress,
                      event.step,
                    ),
                  }
                : null,
            }));
          } else if (event.type === "project_file_proposal") {
            useAiProjectFileReview
              .getState()
              .receive(requestId, event.generationId, event.proposal);
          } else if (event.type === "workspace_panel") {
            // The server emits this only after the model has called the
            // separately consented, closed-enum workspace_open_panel tool.
            // It is UI navigation only: no panel content is added to the
            // model context and no command, URL or path is forwarded.
            useUiStore.setState({ rightOverviewCollapsed: false });
            // This comes from the UI-only Agent bridge. It should focus an
            // existing matching workspace tab instead of accumulating copies;
            // user-clicked workspace links retain the default "open" mode.
            useUiStore.getState().setRightPanelTab(event.panel, "activate");
          } else if (event.type === "workspace_panels") {
            // A paired fixed-panel request only arranges local workspace tabs.
            // It carries no panel content and does not start terminal commands.
            useUiStore.setState({ rightOverviewCollapsed: false });
            useUiStore
              .getState()
              .requestWorkspacePanels(event.panels, event.splitRatio);
          } else if (event.type === "workspace_browser_navigation") {
            // The server only emits an explicitly granted, validated HTTP(S)
            // target. It is still a proposal: EmbeddedBrowser renders a local
            // confirmation before opening a native or external browser tab.
            useUiStore.setState({ rightOverviewCollapsed: false });
            useUiStore.getState().setRightPanelTab("browser", "activate");
            useUiStore.getState().requestBrowserNavigation(event.url);
          } else if (event.type === "workspace_browser_action") {
            useUiStore.setState({ rightOverviewCollapsed: false });
            useUiStore.getState().setRightPanelTab("browser", "activate");
            useUiStore.getState().requestBrowserAction(event.action);
          } else if (event.type === "workspace_access_request") {
            // A request is not a grant: it only offers a new local permission
            // review. The current registry remains unchanged.
            set((state) => ({
              accessRequests: {
                ...state.accessRequests,
                [sessionId]: {
                  generationId: event.generationId,
                  scopes: event.scopes,
                },
              },
            }));
          } else if (event.type === "workspace_plan_updated") {
            // This metadata-only receipt is emitted only after the plan
            // revision commits. The plan cards use it to re-read the same
            // local session; it neither exposes the plan nor executes work.
            set((state) => ({
              workPlanVersions: {
                ...state.workPlanVersions,
                [sessionId]: Math.max(
                  state.workPlanVersions[sessionId] ?? 0,
                  event.version,
                ),
              },
            }));
          } else if (event.type === "workspace_record_navigation") {
            // This is only a local, user-confirmed navigation request for a
            // server-authorized record identity. It does not put record
            // content into the UI store or auto-leave the current chat.
            useUiStore
              .getState()
              .requestWorkspaceRecordNavigation(
                event.recordType,
                event.recordId,
                event.recordType === "agent_run" ||
                  event.recordType === "task_submission" ||
                  event.recordType === "task_artifact"
                  ? event.taskId
                  : undefined,
                event.recordType === "project_note" ||
                  event.recordType === "client_activity" ||
                  event.recordType === "client_followup"
                  ? event.parentId
                  : undefined,
                event.recordType === "task_artifact"
                  ? event.submissionId
                  : undefined,
              );
          } else if (event.type === "replace") {
            set({
              streaming: {
                ...get().streaming,
                sessionId,
                generationId,
                providerId: input.providerId,
                text: event.text,
                reasoning: event.reasoning,
              },
            });
          } else if (event.type === "cancelled") {
            terminalStatus = "cancelled";
            cancelled = true;
            set((state) => ({
              accessRequests: Object.fromEntries(
                Object.entries(state.accessRequests).filter(
                  ([id, request]) =>
                    id !== sessionId || request.generationId !== generationId,
                ),
              ),
              streaming: state.streaming
                ? {
                    ...state.streaming,
                    text: event.partialText || state.streaming.text,
                  }
                : null,
            }));
          } else if (event.type === "error") {
            terminalStatus = "failed";
            set((state) => ({
              accessRequests: Object.fromEntries(
                Object.entries(state.accessRequests).filter(
                  ([id, request]) =>
                    id !== sessionId || request.generationId !== generationId,
                ),
              ),
            }));
            if (event.partialText !== undefined) {
              set((state) => ({
                streaming: state.streaming
                  ? { ...state.streaming, text: event.partialText! }
                  : null,
              }));
            }
            failureCode = event.error;
            failure =
              aiStreamErrorHints[event.error] ??
              `AI 生成失败（${event.error}）`;
          } else if (event.type === "done") {
            if (
              get().streaming?.progress?.some(
                (step) => step.status === "running",
              )
            )
              throw new ApiError("生成结束但执行步骤尚未确认，正在核对状态", {
                code: "INVALID_RESPONSE",
              });
            terminalStatus = "completed";
            set((state) => ({
              streaming: state.streaming
                ? {
                    ...state.streaming,
                    citationEvidence: event.citationEvidence,
                  }
                : null,
            }));
          }
        },
      });
      cancelled ||= currentController.signal.aborted;
      // A local abort before meta/terminal is still an uncertain acceptance.
      // Keep its identifier so recovery can find (and stop) the accepted run.
      if (terminalStatus) {
        clearRequest();
        uncertainRequest = null;
      }
    } catch (error) {
      if (error instanceof AiChatAlreadyAccepted) {
        accepted = true;
        generationId = error.generationId;
        sessionId = error.sessionId;
        recoveringAccepted = true;
        set((state) => ({
          acceptedCommand: acceptedCommandMetadata(
            state.acceptedCommand,
            requestId,
            input,
            sessionId,
          ),
          streaming: {
            sessionId,
            generationId,
            providerId: input.providerId,
            text: "",
            reasoning: "",
          },
          lastSessionId: sessionId,
          sentMessage: null,
        }));
      } else {
        failureCode =
          error instanceof ApiError ? error.code : "AI_STREAM_ERROR";
        failure =
          aiStreamErrorHints[failureCode] ??
          (error instanceof Error ? error.message : "AI 回答流中断");
        // A definitive pre-acceptance rejection can be corrected and resent.
        // Network failures retain the same command key for an uncertain retry.
        if (
          !accepted &&
          error instanceof ApiError &&
          error.status &&
          error.status < 500
        ) {
          clearRequest();
          uncertainRequest = null;
        }
      }
    } finally {
      useAiProjectFileReview
        .getState()
        .finish(
          requestId,
          terminalStatus === "completed" &&
            !failure &&
            !cancelled &&
            !recoveringAccepted,
        );
      controller = null;
      ++recoveryRevision;
      if (!recoveringAccepted)
        set((state) => ({
          streaming: null,
          sentMessage: null,
          streamError: failure,
          retainedTurns:
            accepted && generationId && terminalStatus && state.streaming
              ? retainTurn(state.retainedTurns, {
                  ...state.streaming,
                  generationId,
                  userText: input.message,
                  status: terminalStatus,
                  createdAt: new Date().toISOString(),
                })
              : state.retainedTurns,
          interrupted:
            (failure || cancelled) &&
            (state.streaming?.text ||
              state.streaming?.reasoning ||
              state.streaming?.progress?.length)
              ? state.streaming
              : null,
        }));
      if (sessionId) await refresh(sessionId).catch(() => {});
    }
    if (
      currentRevision === commandRevision &&
      (recoveringAccepted || (failure && accepted) || cancelled)
    )
      await get().recover();
    return {
      sessionId,
      cancelled,
      accepted,
      error: failure,
      errorCode: failureCode,
    };
  },
  stop: async () => {
    ++recoveryRevision;
    const stoppedRevision = commandRevision;
    const current = get().streaming;
    controller?.abort();
    if (current?.generationId) {
      try {
        await cancelAiGeneration(current.generationId);
      } catch {
        if (stoppedRevision === commandRevision)
          set({
            streamError: "停止请求未确认，正在重新读取生成状态；可再次停止",
          });
      }
    }
    if (!controller && stoppedRevision === commandRevision)
      await get().recover();
  },
  recover: async () => {
    if (recovering) return;
    recovering = true;
    const revision = recoveryRevision;
    const ownsRecovery = () => revision === recoveryRevision && !controller;
    try {
      const active = await getActiveAiGenerations();
      if (revision !== recoveryRevision) return;
      set({ activeGenerations: active });
      if (controller) return;
      let current: AiGeneration | undefined;
      const pending = savedRequest();
      const previous = get().streaming;
      if (previous?.generationId)
        current =
          active.find((item) => item.id === previous.generationId) ??
          (await getAiGeneration(previous.generationId));
      else if (pending) {
        try {
          current =
            active.find(
              (item) => item.client_request_id === pending.requestId,
            ) ?? (await getAiGenerationByRequest(pending.requestId));
        } catch (error) {
          if (!ownsRecovery()) return;
          if (!(error instanceof ApiError) || error.status !== 404) throw error;
          clearRequest();
        }
      }
      if (!ownsRecovery()) return;
      // Background lease generations have their own polling/stop lifecycle.
      // Never let one claim a manual draft or its acceptance/recovery metadata.
      if (current?.origin) return;
      current ??= active.find((item) => !item.origin);
      if (!current) return;
      set({ lastSessionId: current.session_id });
      if (current.accessRequest) {
        set((state) => ({
          accessRequests: {
            ...state.accessRequests,
            [current.session_id]: {
              generationId: current.id,
              scopes: current.accessRequest!.scopes,
            },
          },
        }));
      }
      if (
        uncertainRequest &&
        current.client_request_id === uncertainRequest.requestId
      ) {
        const acceptedRequest = uncertainRequest;
        const acceptedSessionId = current.session_id;
        set((state) => ({
          acceptedCommand: acceptedCommandMetadata(
            state.acceptedCommand,
            acceptedRequest.requestId,
            acceptedRequest.input,
            acceptedSessionId,
          ),
          input:
            (acceptedRequest.input.owner ?? "main") === "main" &&
            state.inputRevision === acceptedRequest.draftRevision &&
            state.input.trim() === acceptedRequest.message
              ? ""
              : state.input,
        }));
      }
      if (isActive(current)) {
        set({
          streaming: visibleGeneration(current),
          sentMessage: null,
          streamError: null,
        });
      } else {
        const existing = get().retainedTurns.find(
          (turn) => turn.generationId === current.id,
        );
        // Non-persistent terminal records deliberately have no body. A cached
        // polling snapshot is only a partial answer, never a completed reply.
        const missingPrivateBody =
          !current.persist &&
          !existing &&
          !current.content &&
          current.status === "completed";
        const partial =
          previous?.generationId === current.id
            ? previous
            : get().interrupted?.generationId === current.id
              ? get().interrupted
              : null;
        const retained: AiRetainedTurn = existing
          ? {
              ...existing,
              citationEvidence:
                current.citationEvidence ?? existing.citationEvidence,
              progress: current.progress ?? existing.progress,
            }
          : {
              ...visibleGeneration(current),
              progress: current.progress ?? partial?.progress,
              generationId: current.id,
              text:
                current.content ||
                (!current.persist ? (partial?.text ?? "") : ""),
              reasoning:
                current.reasoning ||
                (!current.persist ? (partial?.reasoning ?? "") : ""),
              userText:
                uncertainRequest?.requestId === current.client_request_id
                  ? uncertainRequest.message
                  : null,
              status: missingPrivateBody
                ? "incomplete"
                : current.status === "completed"
                  ? "completed"
                  : current.status === "cancelled"
                    ? "cancelled"
                    : "failed",
              createdAt: new Date().toISOString(),
            };
        clearRequest();
        uncertainRequest = null;
        set({
          streaming: null,
          sentMessage: null,
          streamError: missingPrivateBody
            ? "非持久会话已结束，完整回复未保存；这里只能保留本窗口收到的片段"
            : current.status === "completed"
              ? null
              : current.status === "cancelled"
                ? "生成已停止，内容可能不完整"
                : (aiStreamErrorHints[current.error_code ?? ""] ??
                  "生成失败，请查看已保留的回复"),
          retainedTurns: retainTurn(get().retainedTurns, retained),
          interrupted:
            current.status !== "completed" && current.content
              ? visibleGeneration(current)
              : get().interrupted,
        });
        await refresh(current.session_id).catch(() => {});
      }
    } catch (error) {
      if (!ownsRecovery()) return;
      if (!controller && error instanceof ApiError && error.status === 404) {
        clearRequest();
        set((state) => ({
          streaming: null,
          sentMessage: null,
          interrupted:
            state.streaming?.text ||
            state.streaming?.reasoning ||
            state.streaming?.progress?.length
              ? state.streaming
              : state.interrupted,
          streamError:
            "原生成已不存在，本地服务可能已重启或数据已恢复，请检查会话历史",
        }));
      } else if (get().streaming || savedRequest()) {
        set({
          streamError:
            "暂时无法读取生成状态，正在等待本地服务恢复；没有重新发送消息",
        });
      }
    } finally {
      recovering = false;
    }
  },
}));
