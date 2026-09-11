import { create } from "zustand";
import { AiChatAlreadyAccepted, streamAiChat } from "../api/ai";
import { ApiError } from "../api/client";
import {
  cancelAiGeneration,
  getActiveAiGenerations,
  getAiGeneration,
  getAiGenerationByRequest,
  type AiGeneration,
} from "../api/aiActions";
import type { AiBusinessContextSelection } from "../types/models";

export interface AiChatStreamState {
  sessionId: string;
  generationId?: string;
  providerId?: string;
  text: string;
  reasoning: string;
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

interface ChatInput {
  providerId: string;
  sessionId?: string;
  message: string;
  context?: AiBusinessContextSelection;
}

interface ChatStore {
  streaming: AiChatStreamState | null;
  interrupted: AiChatStreamState | null;
  retainedTurns: AiRetainedTurn[];
  streamError: string | null;
  sentMessage: string | null;
  activeGenerations: AiGeneration[];
  lastSessionId: string;
  input: string;
  inputRevision: number;
  setInput: (value: string | ((current: string) => string)) => void;
  send: (input: ChatInput) => Promise<AiChatStreamOutcome>;
  stop: () => Promise<void>;
  recover: () => Promise<void>;
  forgetSession: (sessionId: string) => void;
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
      encoder.encode(item.userText ?? "").byteLength;
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
  };
}

export const aiStreamErrorHints: Record<string, string> = {
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
  AI_CONTEXT_SOURCE_NOT_FOUND: "所选工作区上下文已不存在，请重新选择",
  AI_CONTEXT_TOO_LARGE: "所选工作区上下文超过 16 KiB，请减少选择",
};

export function configureAiChatRefresh(callback: typeof refresh) {
  refresh = callback;
}

export const useAiChatStore = create<ChatStore>((set, get) => ({
  streaming: null,
  interrupted: null,
  retainedTurns: [],
  streamError: null,
  sentMessage: null,
  activeGenerations: [],
  lastSessionId: "",
  input: "",
  inputRevision: 0,
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
    set((state) => ({
      retainedTurns: state.retainedTurns.filter(
        (turn) => turn.sessionId !== sessionId,
      ),
      interrupted:
        state.interrupted?.sessionId === sessionId ? null : state.interrupted,
      lastSessionId:
        state.lastSessionId === sessionId ? "" : state.lastSessionId,
    }));
  },
  send: async (requestedInput) => {
    // A lost response may have allocated the first session while its meta
    // frame never reached this window. Retry the original command body, not
    // the newly selected session ID discovered by a background refresh.
    const previousInput = uncertainRequest?.input;
    const input =
      previousInput &&
      !previousInput.sessionId &&
      previousInput.providerId === requestedInput.providerId &&
      previousInput.message === requestedInput.message &&
      JSON.stringify(previousInput.context) ===
        JSON.stringify(requestedInput.context)
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
          if (event.type === "meta") {
            sessionId = event.meta.session_id || sessionId;
            generationId = event.meta.generation_id;
            accepted = true;
            saveRequest(requestId, sessionId);
            set({
              lastSessionId: sessionId,
              streaming: {
                sessionId,
                generationId,
                providerId: input.providerId,
                text: "",
                reasoning: "",
              },
            });
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
          } else if (event.type === "replace") {
            set({
              streaming: {
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
              streaming: state.streaming
                ? {
                    ...state.streaming,
                    text: event.partialText || state.streaming.text,
                  }
                : null,
            }));
          } else if (event.type === "error") {
            terminalStatus = "failed";
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
            terminalStatus = "completed";
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
        set({
          streaming: {
            sessionId,
            generationId,
            providerId: input.providerId,
            text: "",
            reasoning: "",
          },
          lastSessionId: sessionId,
          sentMessage: null,
        });
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
            (failure || cancelled) && state.streaming?.text
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
      current ??= active[0];
      if (!current) return;
      set({ lastSessionId: current.session_id });
      if (
        uncertainRequest &&
        current.client_request_id === uncertainRequest.requestId
      ) {
        const acceptedDraft = uncertainRequest.message;
        const acceptedRevision = uncertainRequest.draftRevision;
        set((state) => ({
          input:
            state.inputRevision === acceptedRevision &&
            state.input.trim() === acceptedDraft
              ? ""
              : state.input,
        }));
      }
      if (isActive(current)) {
        set({ streaming: visibleGeneration(current), sentMessage: null });
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
        const retained: AiRetainedTurn = existing ?? {
          ...visibleGeneration(current),
          generationId: current.id,
          text:
            current.content || (!current.persist ? (partial?.text ?? "") : ""),
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
            state.streaming?.text || state.streaming?.reasoning
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
