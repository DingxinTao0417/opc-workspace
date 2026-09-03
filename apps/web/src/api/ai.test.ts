import { afterEach, describe, expect, it, vi } from "vitest";

import { streamAiChat } from "./ai";
import { ApiError } from "./client";

function sseResponse(chunks: string[], init?: ResponseInit): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
  return new Response(stream, init ?? { status: 200 });
}

const mockConnection = {
  baseUrl: "http://127.0.0.1:9876",
  token: "test-token",
};

vi.mock("./client", async (importOriginal) => {
  const original = await importOriginal<typeof import("./client")>();
  return {
    ...original,
    getRuntimeConnection: vi.fn(async () => mockConnection),
  };
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("streamAiChat", () => {
  it("parses meta, delta, replacement, and done events from the SSE stream", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse([
        'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
        'event: delta\ndata: {"generation_id":"gen-1","text":"你"}\n\nevent: delta\ndata: {"generation_id":"gen-1","text":"好"}\n\n',
        'event: replace\ndata: {"generation_id":"gen-1","text":"修订后的你好","reasoning":"复核后补全"}\n\n',
        'event: done\ndata: {"generation_id":"gen-1"}\n\n',
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      sessionId: "s-1",
      message: "你好",
      onEvent: (event) => events.push(event),
    });

    expect(events).toEqual([
      {
        type: "meta",
        meta: {
          protocol: "openai_chat",
          generation_id: "gen-1",
          session_id: "s-1",
          model: "gpt-test",
          provider_id: "p-1",
          sse_protocol: "opc-ai-sse-v1",
        },
      },
      { type: "delta", generationId: "gen-1", text: "你" },
      { type: "delta", generationId: "gen-1", text: "好" },
      {
        type: "replace",
        generationId: "gen-1",
        text: "修订后的你好",
        reasoning: "复核后补全",
      },
      { type: "done", generationId: "gen-1" },
    ]);
    const [, init] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit,
    ];
    const headers = init.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer test-token");
    expect(headers.Accept).toBe("text/event-stream");
  });

  it("surfaces the error event and keeps partial text delivered before it", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse([
        'event: delta\ndata: {"generation_id":"gen-1","text":"部分"}\n\n',
        'event: error\ndata: {"generation_id":"gen-1","error":"AI_STREAM_ERROR"}\n\n',
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "触发失败",
      onEvent: (event) => events.push(event),
    });
    expect(events).toEqual([
      { type: "delta", generationId: "gen-1", text: "部分" },
      { type: "error", generationId: "gen-1", error: "AI_STREAM_ERROR" },
    ]);
  });

  it("maps non-2xx responses to an ApiError with the server error code", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ code: "AI_PROVIDER_BUSY", message: "busy" }),
          { status: 409 },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      streamAiChat({ providerId: "p-1", message: "hi", onEvent: () => {} }),
    ).rejects.toMatchObject({
      code: "AI_PROVIDER_BUSY",
    } satisfies Partial<ApiError>);
  });

  it("returns silently when the signal was aborted before connecting", async () => {
    const controller = new AbortController();
    controller.abort();
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        // mirror fetch semantics: an already-aborted signal rejects
        if (init?.signal?.aborted) {
          throw new DOMException("Aborted", "AbortError");
        }
        return sseResponse([]);
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "hi",
        signal: controller.signal,
        onEvent: () => {},
      }),
    ).resolves.toBeUndefined();
  });
});
