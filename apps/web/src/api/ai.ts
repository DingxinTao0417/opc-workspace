import { ApiError, getRuntimeConnection } from "./client";
import type { AiChatStreamEvent } from "../types/models";

export interface StreamAiChatInput {
  providerId: string;
  sessionId?: string;
  message: string;
  signal?: AbortSignal;
  onEvent: (event: AiChatStreamEvent) => void;
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
        ...(connection.token
          ? { Authorization: `Bearer ${connection.token}` }
          : {}),
      },
      body: JSON.stringify({
        provider_id: input.providerId,
        session_id: input.sessionId ?? "",
        message: input.message,
      }),
      signal: controller.signal,
    });
  } catch (error) {
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
        };
        code = typeof body.code === "string" ? body.code : code;
        message = typeof body.message === "string" ? body.message : message;
      } catch {
        // non-JSON error body keeps the generic message
      }
      throw new ApiError(message, { code, status: response.status });
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let separator = buffer.indexOf("\n\n");
      while (separator >= 0) {
        const block = buffer.slice(0, separator);
        buffer = buffer.slice(separator + 2);
        const event = parseAiStreamBlock(block);
        if (event) input.onEvent(event);
        separator = buffer.indexOf("\n\n");
      }
    }
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
  const generationId =
    typeof payload.generation_id === "string" ? payload.generation_id : "";
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
      };
    default:
      return null;
  }
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}
