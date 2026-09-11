import { ApiError, getRuntimeConnection } from "./client";
import type {
  AiBusinessContextSelection,
  AiChatStreamEvent,
} from "../types/models";

export interface StreamAiChatInput {
  providerId: string;
  sessionId?: string;
  message: string;
  context?: AiBusinessContextSelection;
  requestId?: string;
  signal?: AbortSignal;
  onEvent: (event: AiChatStreamEvent) => void;
}

export class AiChatAlreadyAccepted extends ApiError {
  constructor(
    readonly generationId: string,
    readonly sessionId: string,
  ) {
    super("消息已被接收，正在恢复原来的生成", {
      code: "AI_CHAT_ALREADY_ACCEPTED",
      status: 409,
    });
  }
}

// streamAiChat consumes the Sidecar's opc-ai-sse-v1 stream and forwards
// parsed events. Aborting the signal disconnects the request, which the
// Sidecar treats as a cancel and keeps the generated partial content.
export async function streamAiChat(input: StreamAiChatInput): Promise<void> {
  const connection = await getRuntimeConnection();
  const controller = new AbortController();
  const upstreamSignal = input.signal;
  const abortFromUpstream = () => controller.abort();
  if (upstreamSignal?.aborted) controller.abort();
  else
    upstreamSignal?.addEventListener("abort", abortFromUpstream, {
      once: true,
    });

  let response: Response;
  try {
    response = await fetch(`${connection.baseUrl}/api/v1/ai/chat`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        "X-Request-ID": crypto.randomUUID(),
        "Idempotency-Key": input.requestId ?? crypto.randomUUID(),
        ...(connection.token
          ? { Authorization: `Bearer ${connection.token}` }
          : {}),
      },
      body: JSON.stringify({
        provider_id: input.providerId,
        session_id: input.sessionId ?? "",
        message: input.message,
        ...(input.context ? { context: input.context } : {}),
      }),
      signal: controller.signal,
    });
  } catch (error) {
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
    if (controller.signal.aborted) return;
    throw new ApiError("无法连接本地 Sidecar", { code: "NETWORK_ERROR" });
  }

  try {
    if (!response.ok || !response.body) {
      let code = "HTTP_ERROR";
      let message = `请求失败（${response.status}）`;
      try {
        const body = (await response.json()) as {
          code?: string;
          message?: string;
          error?: { code?: string; message?: string };
          generation_id?: string;
          session_id?: string;
        };
        code =
          body.error?.code ??
          (typeof body.code === "string" ? body.code : code);
        message =
          body.error?.message ??
          (typeof body.message === "string" ? body.message : message);
        if (
          code === "AI_CHAT_ALREADY_ACCEPTED" &&
          typeof body.generation_id === "string" &&
          typeof body.session_id === "string"
        ) {
          throw new AiChatAlreadyAccepted(body.generation_id, body.session_id);
        }
      } catch (error) {
        if (error instanceof AiChatAlreadyAccepted) throw error;
        // non-JSON error body keeps the generic message
      }
      throw new ApiError(message, { code, status: response.status });
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let terminal = false;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let boundary = /\r?\n\r?\n/.exec(buffer);
      let separator = boundary?.index ?? -1;
      while (separator >= 0) {
        const block = buffer.slice(0, separator);
        buffer = buffer.slice(separator + (boundary?.[0].length ?? 2));
        const event = parseAiStreamBlock(block);
        if (event) {
          input.onEvent(event);
          terminal =
            event.type === "done" ||
            event.type === "cancelled" ||
            event.type === "error";
          if (terminal) {
            await reader.cancel().catch(() => {});
            return;
          }
        }
        boundary = /\r?\n\r?\n/.exec(buffer);
        separator = boundary?.index ?? -1;
      }
    }
    if (!terminal)
      throw new ApiError("AI 回答流提前中断，已收到的内容可能不完整", {
        code: "AI_STREAM_INCOMPLETE",
      });
  } catch (error) {
    if (controller.signal.aborted) return;
    if (error instanceof ApiError) throw error;
    throw new ApiError("AI 回答流中断", { code: "AI_STREAM_ERROR" });
  } finally {
    upstreamSignal?.removeEventListener("abort", abortFromUpstream);
  }
}

function parseAiStreamBlock(block: string): AiChatStreamEvent | null {
  let event = "message";
  const dataLines: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trim());
    }
  }
  if (dataLines.length === 0) return null;
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(dataLines.join("\n")) as Record<string, unknown>;
  } catch {
    return null;
  }
  if (!payload || typeof payload !== "object" || Array.isArray(payload))
    return null;
  const generationId =
    typeof payload.generation_id === "string" ? payload.generation_id : "";
  if (!generationId) return null;
  switch (event) {
    case "meta":
      return {
        type: "meta",
        meta: {
          protocol: text(payload.protocol),
          generation_id: generationId,
          session_id: text(payload.session_id),
          model: text(payload.model),
          provider_id: text(payload.provider_id),
          sse_protocol: text(payload.sse_protocol),
        },
      };
    case "delta":
      return {
        type: "delta",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
      };
    case "reasoning":
      return {
        type: "reasoning",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
      };
    case "replace":
      return {
        type: "replace",
        generationId,
        text: typeof payload.text === "string" ? payload.text : "",
        reasoning:
          typeof payload.reasoning === "string" ? payload.reasoning : "",
      };
    case "done":
      return { type: "done", generationId };
    case "cancelled":
      return {
        type: "cancelled",
        generationId,
        partialText:
          typeof payload.partial_text === "string" ? payload.partial_text : "",
      };
    case "error":
      return {
        type: "error",
        generationId,
        error:
          typeof payload.error === "string" ? payload.error : "AI_STREAM_ERROR",
        detail: typeof payload.detail === "string" ? payload.detail : undefined,
        ...(typeof payload.partial_text === "string"
          ? { partialText: payload.partial_text }
          : {}),
      };
    default:
      return null;
  }
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
