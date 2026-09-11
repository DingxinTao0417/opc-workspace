import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAiChatStream, useConfirmAiMessageTask } from "./hooks";
import { resetRuntimeConnection } from "./client";
import { useAiChatStore } from "../store/aiChat";

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}
const input = {
  providerId: "provider-1",
  sessionId: "session-1",
  message: "请帮我写作业",
};
const generation = {
  id: "generation-1",
  session_id: "session-1",
  provider_id: "provider-1",
  status: "streaming",
  content: "已有片段",
  reasoning: "",
  persist: true,
  client_request_id: "request-1",
  error_code: null,
};
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
function sse(signal?: AbortSignal | null) {
  let channel!: ReadableStreamDefaultController<Uint8Array>;
  const stream = new ReadableStream<Uint8Array>({
    start(value) {
      channel = value;
    },
    cancel() {
      signal?.removeEventListener("abort", abort);
    },
  });
  const abort = () => channel.error(new DOMException("Aborted", "AbortError"));
  signal?.addEventListener("abort", abort, { once: true });
  return {
    response: new Response(stream),
    push: (type: string, payload: unknown) =>
      channel.enqueue(
        new TextEncoder().encode(
          `event: ${type}\ndata: ${JSON.stringify(payload)}\n\n`,
        ),
      ),
    end: () => channel.close(),
  };
}

afterEach(async () => {
  cleanup();
  useAiChatStore.setState({
    streaming: null,
    interrupted: null,
    retainedTurns: [],
    sentMessage: null,
    streamError: null,
    input: "",
    lastSessionId: "",
    activeGenerations: [],
  });
  sessionStorage.clear();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("real AI stream hook and HTTP lifecycle", () => {
  it.each(["completed", "missing"])(
    "does not let an older %s recovery overwrite a new send or its command identity",
    async (oldStatus) => {
      sessionStorage.setItem(
        "opc-ai-pending-request-v1",
        JSON.stringify({ requestId: "old-request", sessionId: "old-session" }),
      );
      let resolveOld!: (response: Response) => void;
      let channel: ReturnType<typeof sse> | undefined;
      const fetcher = vi.fn(async (url: unknown, init?: RequestInit) => {
        if (String(url).endsWith("/active-generations"))
          return json({ data: [] });
        if (String(url).endsWith("/by-request/old-request"))
          return new Promise<Response>((resolve) => {
            resolveOld = resolve;
          });
        channel = sse(init?.signal);
        return channel.response;
      });
      vi.stubGlobal("fetch", fetcher);
      const view = renderHook(useAiChatStream, { wrapper: wrapper() });
      let recovering!: Promise<void>;
      act(() => {
        recovering = view.result.current.recover();
      });
      await waitFor(() => expect(resolveOld).toBeDefined());
      let sending!: ReturnType<typeof view.result.current.send>;
      act(() => {
        sending = view.result.current.send(input);
      });
      await waitFor(() => expect(channel).toBeDefined());
      const newIdentity = sessionStorage.getItem("opc-ai-pending-request-v1");
      try {
        await act(async () => {
          resolveOld(
            oldStatus === "missing"
              ? json({ code: "AI_GENERATION_NOT_FOUND" }, 404)
              : json({
                  data: {
                    ...generation,
                    session_id: "old-session",
                    status: "completed",
                  },
                }),
          );
          await recovering;
        });
        expect(view.result.current.streaming?.sessionId).toBe(input.sessionId);
        expect(sessionStorage.getItem("opc-ai-pending-request-v1")).toBe(
          newIdentity,
        );
      } finally {
        await act(async () => {
          channel!.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel!.push("done", { generation_id: generation.id });
          channel!.end();
          await sending;
        });
      }
    },
  );

  it("retains completed replies in application memory across route remounts without storing bodies", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const channel = sse();
        channel.push("meta", {
          generation_id: generation.id,
          session_id: input.sessionId,
        });
        channel.push("reasoning", {
          generation_id: generation.id,
          text: "私密思考",
        });
        channel.push("delta", {
          generation_id: generation.id,
          text: "临时会话的完整回复",
        });
        channel.push("done", { generation_id: generation.id });
        channel.end();
        return channel.response;
      }),
    );
    const first = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () => {
      await first.result.current.send(input);
    });
    first.unmount();
    const second = renderHook(useAiChatStream, { wrapper: wrapper() });
    expect(second.result.current.retainedTurns).toEqual([
      expect.objectContaining({
        sessionId: input.sessionId,
        generationId: generation.id,
        userText: input.message,
        text: "临时会话的完整回复",
        reasoning: "私密思考",
        status: "completed",
      }),
    ]);
    expect(JSON.stringify(sessionStorage)).not.toMatch(
      /临时会话|私密思考|写作业/,
    );
  });

  it("does not resurrect an active snapshot taken before a newer send completed", async () => {
    let resolveActive!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/active-generations"))
          return new Promise<Response>((resolve) => {
            resolveActive = resolve;
          });
        const channel = sse();
        channel.push("meta", {
          generation_id: generation.id,
          session_id: input.sessionId,
        });
        channel.push("delta", { generation_id: generation.id, text: "新回复" });
        channel.push("done", { generation_id: generation.id });
        channel.end();
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let recovering!: Promise<void>;
    act(() => {
      recovering = view.result.current.recover();
    });
    await waitFor(() => expect(resolveActive).toBeDefined());
    await act(async () => {
      await view.result.current.send(input);
    });
    await act(async () => {
      resolveActive(json({ data: [{ ...generation, id: "old-generation" }] }));
      await recovering;
    });
    expect(view.result.current.streaming).toBeNull();
    expect(view.result.current.activeGenerations).toEqual([]);
    expect(view.result.current.retainedTurns.at(-1)?.text).toBe("新回复");
  });

  it.each(["turns", "bytes"])(
    "bounds the private in-memory transcript by %s",
    async (limit) => {
      let sequence = 0;
      const body = limit === "bytes" ? "文".repeat(300_000) : "短回复";
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => {
          const id = `bounded-${sequence++}`;
          const channel = sse();
          channel.push("meta", {
            generation_id: id,
            session_id: input.sessionId,
          });
          channel.push("delta", { generation_id: id, text: body });
          channel.push("done", { generation_id: id });
          channel.end();
          return channel.response;
        }),
      );
      const view = renderHook(useAiChatStream, { wrapper: wrapper() });
      const count = limit === "bytes" ? 10 : 21;
      await act(async () => {
        for (let index = 0; index < count; index++)
          await view.result.current.send(input);
      });
      const retained = view.result.current.retainedTurns;
      expect(retained.length).toBeLessThan(count);
      expect(retained.length).toBeLessThanOrEqual(20);
      expect(retained.at(-1)?.generationId).toBe(`bounded-${count - 1}`);
      const size = retained.reduce(
        (bytes, item) =>
          bytes +
          new TextEncoder().encode(
            item.text + item.reasoning + (item.userText ?? ""),
          ).byteLength,
        0,
      );
      expect(size).toBeLessThanOrEqual(8 * 1024 * 1024);
      expect(sessionStorage.getItem("opc-ai-pending-request-v1")).toBeNull();
    },
  );

  it("keeps a recovered non-persistent partial snapshot when terminal metadata has no body", async () => {
    let finished = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/active-generations"))
          return json({
            data: finished ? [] : [{ ...generation, persist: false }],
          });
        return json({
          data: {
            ...generation,
            persist: false,
            status: "completed",
            content: "",
          },
        });
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () => {
      await view.result.current.recover();
    });
    expect(view.result.current.streaming?.text).toBe("已有片段");
    finished = true;
    await act(async () => {
      await view.result.current.recover();
    });
    expect(view.result.current.retainedTurns).toEqual([
      expect.objectContaining({ text: "已有片段", status: "incomplete" }),
    ]);
    expect(view.result.current.streamError).toContain("非持久");
  });

  it("recognizes a server cancellation terminal even when the browser signal was not aborted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const channel = sse();
        channel.push("meta", {
          generation_id: generation.id,
          session_id: input.sessionId,
        });
        channel.push("cancelled", {
          generation_id: generation.id,
          partial_text: "已收到的部分",
        });
        channel.end();
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () =>
      expect(await view.result.current.send(input)).toMatchObject({
        cancelled: true,
        accepted: true,
        error: null,
      }),
    );
    expect(view.result.current.interrupted?.text).toBe("已收到的部分");
  });
  it("keeps an interrupted private SSE body when recovery can only read terminal metadata", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/ai/chat")) {
          const channel = sse();
          channel.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel.push("delta", {
            generation_id: generation.id,
            text: "断连前的临时片段",
          });
          channel.push("reasoning", {
            generation_id: generation.id,
            text: "仅在内存中的思考",
          });
          channel.end();
          return channel.response;
        }
        if (String(url).endsWith("/active-generations"))
          return json({ data: [] });
        return json({
          data: {
            ...generation,
            persist: false,
            status: "completed",
            content: "",
            reasoning: "",
          },
        });
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () => {
      expect(await view.result.current.send(input)).toMatchObject({
        accepted: true,
        errorCode: "AI_STREAM_INCOMPLETE",
      });
    });
    expect(view.result.current.retainedTurns).toEqual([
      expect.objectContaining({
        text: "断连前的临时片段",
        reasoning: "仅在内存中的思考",
        status: "incomplete",
      }),
    ]);
    expect(sessionStorage.getItem("opc-ai-pending-request-v1")).toBeNull();
  });

  it("keeps the draft when stopping before the server has confirmed acceptance", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        if (String(url).endsWith("/ai/chat")) {
          channel = sse(init?.signal);
          return channel.response;
        }
        if (String(url).endsWith("/active-generations"))
          return json({ data: [] });
        return json({ code: "AI_GENERATION_NOT_FOUND" }, 404);
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    useAiChatStore.getState().setInput(input.message);
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      await view.result.current.stop();
      expect(await sending).toMatchObject({
        cancelled: true,
        accepted: false,
        error: null,
      });
    });
    expect(useAiChatStore.getState().input).toBe(input.message);
    expect(view.result.current.isStreaming).toBe(false);
  });
  it("survives route unmounts, rejects overlapping sends and stops the original generation", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    let cancelled = false;
    const fetcher = vi.fn(async (url: unknown, init?: RequestInit) => {
      if (String(url).endsWith("/ai/chat")) {
        channel = sse(init?.signal);
        return channel.response;
      }
      if (String(url).endsWith("/cancel")) {
        cancelled = true;
        return json({ data: {} });
      }
      if (String(url).endsWith("/active-generations"))
        return json({ data: cancelled ? [] : [generation] });
      return json({
        data: { ...generation, status: cancelled ? "cancelled" : "streaming" },
      });
    });
    vi.stubGlobal("fetch", fetcher);
    const first = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof first.result.current.send>;
    act(() => {
      sending = first.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    act(() => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: input.sessionId,
        provider_id: input.providerId,
      });
      channel!.push("delta", {
        generation_id: generation.id,
        text: "已有片段",
      });
    });
    await waitFor(() =>
      expect(first.result.current.streaming?.text).toBe("已有片段"),
    );
    first.unmount();
    const second = renderHook(useAiChatStream, { wrapper: wrapper() });
    expect(second.result.current.streaming?.text).toBe("已有片段");
    await act(async () => {
      expect(
        await second.result.current.send({ ...input, message: "另一条" }),
      ).toMatchObject({ errorCode: "AI_PROVIDER_BUSY" });
      await second.result.current.stop();
      expect(await sending).toMatchObject({ cancelled: true, accepted: true });
    });
    expect(
      fetcher.mock.calls.filter(([url]) => String(url).endsWith("/ai/chat")),
    ).toHaveLength(1);
    expect(cancelled).toBe(true);
    expect(second.result.current.isStreaming).toBe(false);
  });

  it("retains incomplete content and does not report a terminal-less EOF as success", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/ai/chat")) {
          const channel = sse();
          channel.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel.push("delta", {
            generation_id: generation.id,
            text: "已有片段",
          });
          channel.end();
          return channel.response;
        }
        if (String(url).endsWith("/active-generations"))
          return json({ data: [] });
        return json({
          data: {
            ...generation,
            status: "failed",
            error_code: "AI_STREAM_INCOMPLETE",
          },
        });
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () =>
      expect(await view.result.current.send(input)).toMatchObject({
        accepted: true,
        errorCode: "AI_STREAM_INCOMPLETE",
        cancelled: false,
      }),
    );
    expect(view.result.current.interrupted?.text).toBe("已有片段");
    expect(view.result.current.streamError).toContain("不完整");
  });

  it.each([undefined, "session-1"])(
    "reuses the original command after a %s session request loses its response",
    async (initialSessionId) => {
      const keys: string[] = [];
      const bodies: string[] = [];
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url: unknown, init?: RequestInit) => {
          if (String(url).endsWith("/ai/chat")) {
            keys.push(new Headers(init?.headers).get("Idempotency-Key")!);
            bodies.push(String(init?.body));
            if (keys.length === 1)
              throw new TypeError("connection dropped after acceptance");
            return json(
              {
                code: "AI_CHAT_ALREADY_ACCEPTED",
                generation_id: generation.id,
                session_id: input.sessionId,
              },
              409,
            );
          }
          if (String(url).endsWith("/active-generations"))
            return json({ data: [] });
          return json({ data: { ...generation, status: "completed" } });
        }),
      );
      const first = renderHook(useAiChatStream, { wrapper: wrapper() });
      await act(async () =>
        expect(
          await first.result.current.send({
            ...input,
            sessionId: initialSessionId,
          }),
        ).toMatchObject({
          accepted: false,
          errorCode: "NETWORK_ERROR",
        }),
      );
      expect(sessionStorage.getItem("opc-ai-pending-request-v1")).not.toContain(
        input.message,
      );
      first.unmount();
      const second = renderHook(useAiChatStream, { wrapper: wrapper() });
      await act(async () =>
        expect(await second.result.current.send(input)).toMatchObject({
          accepted: true,
          error: null,
        }),
      );
      expect(keys).toHaveLength(2);
      expect(keys[0]).toBe(keys[1]);
      expect(bodies[0]).toBe(bodies[1]);
      expect(JSON.parse(bodies[1]).session_id).toBe(initialSessionId ?? "");
      expect(second.result.current.isStreaming).toBe(false);
    },
  );

  it("discovers a running generation after reload and retains control during a transient status failure", async () => {
    sessionStorage.setItem(
      "opc-ai-pending-request-v1",
      JSON.stringify({ requestId: "request-1", sessionId: "session-1" }),
    );
    let offline = false;
    let cancelled = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (offline) throw new TypeError("temporarily offline");
        if (String(url).endsWith("/cancel")) {
          cancelled = true;
          return json({ data: {} });
        }
        if (String(url).endsWith("/active-generations"))
          return json({ data: cancelled ? [] : [generation] });
        return json({
          data: {
            ...generation,
            status: cancelled ? "cancelled" : "streaming",
          },
        });
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () => view.result.current.recover());
    expect(view.result.current.streaming).toMatchObject({
      generationId: generation.id,
      text: "已有片段",
    });
    offline = true;
    await act(async () => view.result.current.recover());
    expect(view.result.current.isStreaming).toBe(true);
    offline = false;
    await act(async () => view.result.current.stop());
    expect(view.result.current.isStreaming).toBe(false);
    expect(view.result.current.streamError).toContain("已停止");
  });

  it("clears controls when the service restart reports the generation missing", async () => {
    useAiChatStore.setState({
      streaming: {
        sessionId: input.sessionId,
        generationId: generation.id,
        text: "partial",
        reasoning: "",
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) =>
        String(url).endsWith("/active-generations")
          ? json({ data: [] })
          : json({ code: "AI_GENERATION_NOT_FOUND" }, 404),
      ),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    await act(async () => view.result.current.recover());
    expect(view.result.current.isStreaming).toBe(false);
    expect(view.result.current.streamError).toContain("重启");
    expect(view.result.current.interrupted?.text).toBe("partial");
  });
});

describe("real atomic task confirmation hook", () => {
  it("keeps the message command identity across response loss, unmount and concurrent confirmations", async () => {
    let created = 0;
    let requests = 0;
    const identities = new Set<string>();
    const task = {
      id: "task-once",
      title: "写作业",
      description: "",
      kind: "work",
      status: "todo",
      priority: "P2",
      review_policy: "none",
      blocked_from_status: null,
      estimated_minutes: null,
      manual_order: null,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        expect(init?.method).toBe("POST");
        expect(String(url)).toMatch(
          /\/ai\/messages\/message-1\/task-confirmation$/,
        );
        if (!identities.has(String(url))) {
          identities.add(String(url));
          created++;
        }
        requests++;
        if (requests === 1) throw new TypeError("response lost after commit");
        return json({
          data: { task, message: { id: "message-1", task_id: task.id } },
        });
      }),
    );
    const command = {
      messageId: "message-1",
      input: { title: "写作业", priority: "P2" as const },
    };
    const first = renderHook(() => useConfirmAiMessageTask("session-1"), {
      wrapper: wrapper(),
    });
    await act(async () =>
      expect(first.result.current.mutateAsync(command)).rejects.toThrow(),
    );
    first.unmount();
    const second = renderHook(() => useConfirmAiMessageTask("session-1"), {
      wrapper: wrapper(),
    });
    await act(async () => {
      const results = await Promise.all([
        second.result.current.mutateAsync(command),
        second.result.current.mutateAsync(command),
      ]);
      expect(results.map((result) => result.id)).toEqual([
        "task-once",
        "task-once",
      ]);
    });
    expect(created).toBe(1);
    expect(requests).toBe(3);
  });
});
