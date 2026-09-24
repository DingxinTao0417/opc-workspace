import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAiChatStream, useConfirmAiMessageTask } from "./hooks";
import { resetRuntimeConnection } from "./client";
import { useAiChatStore } from "../store/aiChat";
import { useUiStore } from "../store/ui";

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
const citation = {
  chunk_id: "018f0000-0000-7000-8000-000000000001",
  source_id: "018f0000-0000-7000-8000-000000000002",
  document_id: "018f0000-0000-7000-8000-000000000003",
  source_type: "pdf",
  source_name: "私密资料.pdf",
  document_title: "原始文档",
  source_version: 2,
  document_version: 3,
  chunk_index: 1,
  start_char: 10,
  end_char: 30,
  start_line: 2,
  end_line: 4,
  start_page: 3,
  end_page: 3,
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
    acceptedCommand: null,
    streaming: null,
    interrupted: null,
    retainedTurns: [],
    sentMessage: null,
    streamError: null,
    input: "",
    lastSessionId: "",
    activeGenerations: [],
    workPlanVersions: {},
    accessRequests: {},
  });
  useUiStore.setState({
    rightOverviewCollapsed: false,
    rightPanelTab: "summary",
    rightPanelRequestMode: "open",
    rightPanelRequest: 0,
    workspacePanelsRequest: null,
    workspacePanelsRequestId: 0,
    browserNavigationRequest: null,
    browserActionRequest: null,
    workspaceRecordNavigationRequest: null,
  });
  sessionStorage.clear();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("real AI stream hook and HTTP lifecycle", () => {
  it("keeps a missing-scope request local without granting or resending", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_access_request", {
        generation_id: generation.id,
        scopes: ["work", "actions"],
      });
    });
    expect(
      useAiChatStore.getState().accessRequests[generation.session_id],
    ).toEqual({
      generationId: generation.id,
      scopes: ["work", "actions"],
    });
    expect(useAiChatStore.getState().acceptedCommand?.sessionId).toBe(
      generation.session_id,
    );
    expect(useUiStore.getState().rightPanelTab).toBe("summary");
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
    expect(
      useAiChatStore.getState().accessRequests[generation.session_id],
    ).toBeDefined();
    act(() =>
      useAiChatStore
        .getState()
        .dismissAccessRequest(generation.session_id, generation.id),
    );
    expect(
      useAiChatStore.getState().accessRequests[generation.session_id],
    ).toBeUndefined();
  });

  it("opens only the server-authorized fixed workspace panel for the active generation", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    useUiStore.setState({ rightOverviewCollapsed: true });
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_panel", {
        generation_id: generation.id,
        panel: "agents",
      });
    });
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      rightPanelTab: "agents",
      rightPanelRequestMode: "activate",
      rightPanelRequest: 1,
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("requests a server-authorized two-panel split without exposing panel content", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    useUiStore.setState({ rightOverviewCollapsed: true });
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_panels", {
        generation_id: generation.id,
        panels: ["files", "review"],
        split_ratio: 0.62,
      });
    });
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      workspacePanelsRequest: {
        id: 1,
        panels: ["files", "review"],
        splitRatio: 0.62,
      },
      workspacePanelsRequestId: 1,
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("keeps a streamed browser action as a local confirmation request", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_browser_action", {
        generation_id: generation.id,
        action: "reload",
      });
    });
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      rightPanelTab: "browser",
      rightPanelRequestMode: "activate",
      browserActionRequest: { action: "reload" },
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("queues a server-authorized browser URL for local confirmation only", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_browser_navigation", {
        generation_id: generation.id,
        url: "https://example.com/docs",
      });
    });
    expect(useUiStore.getState()).toMatchObject({
      rightOverviewCollapsed: false,
      rightPanelTab: "browser",
      rightPanelRequestMode: "activate",
      browserNavigationRequest: { url: "https://example.com/docs" },
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("refreshes only metadata for the current plan after a committed plan update", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_plan_updated", {
        generation_id: generation.id,
        version: 2,
        step_count: 3,
      });
    });
    expect(useAiChatStore.getState().workPlanVersions).toEqual({
      [generation.session_id]: 2,
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("queues a server-authorized record identity for local confirmation only", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_record_navigation", {
        generation_id: generation.id,
        record_type: "task",
        record_id: "018f0000-0000-7000-8000-000000000001",
      });
    });
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest,
    ).toMatchObject({
      recordType: "task",
      recordId: "018f0000-0000-7000-8000-000000000001",
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("queues an Agent Run only with the Sidecar-derived Task identity", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_record_navigation", {
        generation_id: generation.id,
        record_type: "agent_run",
        record_id: "018f0000-0000-7000-8000-000000000011",
        task_id: "018f0000-0000-7000-8000-000000000012",
      });
    });
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest,
    ).toMatchObject({
      recordType: "agent_run",
      recordId: "018f0000-0000-7000-8000-000000000011",
      taskId: "018f0000-0000-7000-8000-000000000012",
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("queues a Task Submission only with the Sidecar-derived Task identity", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_record_navigation", {
        generation_id: generation.id,
        record_type: "task_submission",
        record_id: "018f0000-0000-7000-8000-000000000021",
        task_id: "018f0000-0000-7000-8000-000000000022",
      });
    });
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest,
    ).toMatchObject({
      recordType: "task_submission",
      recordId: "018f0000-0000-7000-8000-000000000021",
      taskId: "018f0000-0000-7000-8000-000000000022",
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("queues a Task Artifact only with Sidecar-derived Task and Submission identities", async () => {
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("workspace_record_navigation", {
        generation_id: generation.id,
        record_type: "task_artifact",
        record_id: "018f0000-0000-7000-8000-000000000031",
        task_id: "018f0000-0000-7000-8000-000000000032",
        submission_id: "018f0000-0000-7000-8000-000000000033",
      });
    });
    expect(
      useUiStore.getState().workspaceRecordNavigationRequest,
    ).toMatchObject({
      recordType: "task_artifact",
      recordId: "018f0000-0000-7000-8000-000000000031",
      taskId: "018f0000-0000-7000-8000-000000000032",
      submissionId: "018f0000-0000-7000-8000-000000000033",
    });
    await act(async () => {
      channel!.push("done", { generation_id: generation.id });
      channel!.end();
      await sending;
    });
  });

  it("keeps tool progress across replacement, route remount and terminal recovery without storing it", async () => {
    const running = {
      sequence: 2,
      kind: "model_turn",
      status: "running",
      turn_index: 1,
      started_at: "2026-09-18T10:00:00Z",
      duration_ms: 0,
    };
    const completed = {
      ...running,
      status: "succeeded",
      completed_at: "2026-09-18T10:00:01Z",
      duration_ms: 1000,
    };
    let channel: ReturnType<typeof sse> | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        channel = sse(init?.signal);
        return channel.response;
      }),
    );
    const view = renderHook(useAiChatStream, { wrapper: wrapper() });
    let sending!: ReturnType<typeof view.result.current.send>;
    act(() => {
      sending = view.result.current.send(input);
    });
    await waitFor(() => expect(channel).toBeDefined());
    await act(async () => {
      channel!.push("meta", {
        generation_id: generation.id,
        session_id: generation.session_id,
      });
      channel!.push("progress", {
        generation_id: generation.id,
        step: running,
      });
    });
    expect(useAiChatStore.getState().streaming?.progress).toEqual([running]);
    expect(JSON.stringify(sessionStorage)).not.toContain("model_turn");
    view.unmount();
    const remount = renderHook(useAiChatStream, { wrapper: wrapper() });
    expect(remount.result.current.streaming?.progress).toEqual([running]);
    await act(async () => {
      channel!.push("progress", {
        generation_id: generation.id,
        step: completed,
      });
      channel!.push("replace", {
        generation_id: generation.id,
        text: "最终回答",
        reasoning: "",
      });
      channel!.push("done", { generation_id: generation.id });
      await sending;
    });
    expect(useAiChatStore.getState().retainedTurns[0]).toMatchObject({
      text: "最终回答",
      progress: [completed],
    });
    sessionStorage.setItem(
      "opc-ai-pending-request-v1",
      JSON.stringify({
        requestId: "recover-progress",
        sessionId: generation.session_id,
      }),
    );
    useAiChatStore.setState({ retainedTurns: [] });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url) =>
        String(url).endsWith("/active-generations")
          ? json({ data: [] })
          : json({
              data: {
                ...generation,
                status: "completed",
                progress: [completed],
              },
            }),
      ),
    );
    await act(async () => {
      await useAiChatStore.getState().recover();
    });
    expect(useAiChatStore.getState().retainedTurns[0].progress).toEqual([
      completed,
    ]);
  });

  it.each(["eof", "premature-done"])(
    "retains progress-only %s without marking its running tool completed",
    async (ending) => {
      const progress = {
        sequence: 2,
        kind: "model_turn",
        status: "running",
        turn_index: 1,
        started_at: "2026-09-18T10:00:00Z",
        duration_ms: 0,
      };
      let channel: ReturnType<typeof sse> | undefined;
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url, init?: RequestInit) => {
          if (!String(url).endsWith("/chat")) throw new Error("offline");
          channel = sse(init?.signal);
          return channel.response;
        }),
      );
      let sending!: Promise<import("../store/aiChat").AiChatStreamOutcome>;
      act(() => {
        sending = useAiChatStore.getState().send(input);
      });
      await waitFor(() => expect(channel).toBeDefined());
      await act(async () => {
        channel!.push("meta", {
          generation_id: generation.id,
          session_id: generation.session_id,
        });
        channel!.push("progress", {
          generation_id: generation.id,
          step: progress,
        });
        if (ending === "premature-done")
          channel!.push("done", { generation_id: generation.id });
        channel!.end();
        await sending;
      });
      expect(useAiChatStore.getState().interrupted?.progress).toEqual([
        progress,
      ]);
      expect(useAiChatStore.getState().retainedTurns).toEqual([]);
      const recovered = { ...generation, content: "", progress: [progress] };
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url) =>
          String(url).endsWith("/active-generations")
            ? json({ data: [recovered] })
            : json({ data: recovered }),
        ),
      );
      await act(async () => {
        await useAiChatStore.getState().recover();
      });
      expect(useAiChatStore.getState().streaming?.progress).toEqual([progress]);
      expect(useAiChatStore.getState().streamError).toBeNull();
    },
  );
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
        channel.push("done", {
          generation_id: generation.id,
          citation_status: "validated",
          citations: [citation],
        });
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
        citationEvidence: { status: "validated", items: [citation] },
      }),
    ]);
    expect(JSON.stringify(sessionStorage)).not.toMatch(
      /临时会话|私密思考|写作业|私密资料|原始文档|chunk_id/,
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
            citation_status: "missing",
            citations: [],
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
      expect.objectContaining({
        text: "已有片段",
        status: "incomplete",
        citationEvidence: { status: "missing", items: [] },
      }),
    ]);
    expect(view.result.current.streamError).toContain("非持久");
  });

  it.each(["wrong-generation", "invalid-evidence"])(
    "does not mark %s terminal metadata as a completed reply",
    async (fault) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url: unknown) => {
          if (String(url).endsWith("/active-generations"))
            return json({ data: [] });
          if (!String(url).endsWith("/ai/chat"))
            return json({
              data: {
                ...generation,
                status: "completed",
                persist: false,
                content: "",
              },
            });
          const channel = sse();
          channel.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel.push("delta", {
            generation_id: generation.id,
            text: "仅收片段",
          });
          channel.push(
            "done",
            fault === "wrong-generation"
              ? { generation_id: "foreign" }
              : {
                  generation_id: generation.id,
                  citation_status: "validated",
                  citations: [],
                },
          );
          channel.end();
          return channel.response;
        }),
      );
      const view = renderHook(useAiChatStream, { wrapper: wrapper() });
      await act(async () => {
        expect(await view.result.current.send(input)).toMatchObject({
          errorCode:
            fault === "wrong-generation"
              ? "AI_STREAM_IDENTITY_MISMATCH"
              : "INVALID_RESPONSE",
        });
      });
      expect(view.result.current.retainedTurns).toEqual([
        expect.objectContaining({
          text: "仅收片段",
          status: "incomplete",
          citationEvidence: undefined,
        }),
      ]);
    },
  );

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

describe("accepted command identity and owner isolation", () => {
  it.each([
    { initialSessionId: undefined, sameAttachment: true },
    { initialSessionId: undefined, sameAttachment: false },
    { initialSessionId: "session-1", sameAttachment: true },
    { initialSessionId: "session-1", sameAttachment: false },
  ])(
    "isolates unknown receipt retries by attachment instance (session=$initialSessionId, same=$sameAttachment)",
    async ({ initialSessionId, sameAttachment }) => {
      const firstAttachment = "runtime-attachment-before-reselection";
      const nextAttachment = sameAttachment
        ? firstAttachment
        : "runtime-attachment-after-reselection";
      const keys: string[] = [];
      const bodies: string[] = [];
      vi.stubGlobal(
        "fetch",
        vi.fn(async (_url, init?: RequestInit) => {
          keys.push(new Headers(init?.headers).get("Idempotency-Key")!);
          bodies.push(String(init?.body));
          if (keys.length === 1) throw new TypeError("lost receipt response");
          const channel = sse(init?.signal);
          channel.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel.push("done", { generation_id: generation.id });
          channel.end();
          return channel.response;
        }),
      );
      const selected = {
        ...input,
        actionReceiptGenerationId: "same-source-generation",
        actionRecheckProposalId: "same-source-proposal",
      };
      await act(async () => {
        expect(
          await useAiChatStore.getState().send({
            ...selected,
            sessionId: initialSessionId,
            actionReceiptAttachmentId: firstAttachment,
          }),
        ).toMatchObject({ accepted: false, errorCode: "NETWORK_ERROR" });
        expect(JSON.stringify(sessionStorage)).not.toContain(firstAttachment);
        await useAiChatStore.getState().send({
          ...selected,
          actionReceiptAttachmentId: nextAttachment,
        });
      });
      expect(keys).toHaveLength(2);
      expect(keys[0] === keys[1]).toBe(sameAttachment);
      expect(JSON.parse(bodies[1]).session_id).toBe(
        sameAttachment ? (initialSessionId ?? "") : input.sessionId,
      );
      expect(useAiChatStore.getState().acceptedCommand).toMatchObject({
        requestId: keys[1],
        actionReceiptGenerationId: selected.actionReceiptGenerationId,
        actionRecheckProposalId: selected.actionRecheckProposalId,
        actionReceiptAttachmentId: nextAttachment,
      });
      expect(bodies.join("\n")).not.toMatch(
        /actionReceiptAttachmentId|action_receipt_attachment_id|runtime-attachment-/,
      );
      expect(JSON.stringify(sessionStorage)).not.toMatch(
        /actionReceiptAttachmentId|runtime-attachment-/,
      );
    },
  );

  it("publishes metadata-only acceptance once per request without persisting it", async () => {
    const keys: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url, init?: RequestInit) => {
        keys.push(new Headers(init?.headers).get("Idempotency-Key")!);
        const channel = sse(init?.signal);
        const meta = {
          generation_id: `generation-${keys.length}`,
          session_id: input.sessionId,
        };
        channel.push("meta", meta);
        channel.push("meta", meta);
        channel.push("done", { generation_id: meta.generation_id });
        channel.end();
        return channel.response;
      }),
    );
    await act(async () => {
      await useAiChatStore.getState().send({
        ...input,
        actionReceiptGenerationId: "receipt-source-1",
        actionRecheckProposalId: "recheck-source-1",
        actionReceiptAttachmentId: "attachment-source-1",
      });
    });
    expect(useAiChatStore.getState().acceptedCommand).toEqual({
      sequence: 1,
      requestId: keys[0],
      owner: "main",
      sessionId: input.sessionId,
      actionReceiptGenerationId: "receipt-source-1",
      actionRecheckProposalId: "recheck-source-1",
      actionReceiptAttachmentId: "attachment-source-1",
    });
    await act(async () => {
      await useAiChatStore.getState().send({ ...input, message: "第二个请求" });
    });
    expect(useAiChatStore.getState().acceptedCommand).toEqual({
      sequence: 2,
      requestId: keys[1],
      owner: "main",
      sessionId: input.sessionId,
    });
    expect(keys[0]).not.toBe(keys[1]);
    expect(JSON.stringify(sessionStorage)).not.toMatch(
      /acceptedCommand|receipt-source-1|recheck-source-1|attachment-source-1|写作业|第二个请求/,
    );
  });

  it("publishes AlreadyAccepted identity and does not increment it on matching recovery", async () => {
    let requestId = "";
    let completed = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown, init?: RequestInit) => {
        if (String(url).endsWith("/ai/chat")) {
          requestId = new Headers(init?.headers).get("Idempotency-Key")!;
          return json(
            {
              code: "AI_CHAT_ALREADY_ACCEPTED",
              generation_id: generation.id,
              session_id: input.sessionId,
            },
            409,
          );
        }
        const current = {
          ...generation,
          client_request_id: requestId,
          status: completed ? "completed" : "streaming",
        };
        return String(url).endsWith("/active-generations")
          ? json({ data: completed ? [] : [current] })
          : json({ data: current });
      }),
    );
    await act(async () => {
      expect(
        await useAiChatStore.getState().send({
          ...input,
          owner: "side:accepted",
          actionReceiptGenerationId: "receipt-already-accepted",
          actionRecheckProposalId: "recheck-already-accepted",
          actionReceiptAttachmentId: "attachment-already-accepted",
        }),
      ).toMatchObject({ accepted: true });
    });
    const accepted = useAiChatStore.getState().acceptedCommand;
    expect(accepted).toEqual({
      sequence: 1,
      requestId,
      owner: "side:accepted",
      sessionId: input.sessionId,
      actionReceiptGenerationId: "receipt-already-accepted",
      actionRecheckProposalId: "recheck-already-accepted",
      actionReceiptAttachmentId: "attachment-already-accepted",
    });
    expect(
      JSON.parse(sessionStorage.getItem("opc-ai-pending-request-v1")!),
    ).toEqual({ requestId, sessionId: input.sessionId });
    await act(async () => {
      completed = true;
      await useAiChatStore.getState().recover();
    });
    expect(useAiChatStore.getState().acceptedCommand).toBe(accepted);
  });

  it.each([
    { owner: undefined, changedDraft: false, clearsDraft: true },
    { owner: "main", changedDraft: false, clearsDraft: true },
    { owner: "side:receipt", changedDraft: false, clearsDraft: false },
    { owner: "main", changedDraft: true, clearsDraft: false },
  ])(
    "recovers uncertain acceptance for $owner without clearing another or newer draft ($changedDraft)",
    async ({ owner, changedDraft, clearsDraft }) => {
      let requestId = "";
      let completed = false;
      vi.stubGlobal(
        "fetch",
        vi.fn(async (url: unknown, init?: RequestInit) => {
          if (String(url).endsWith("/ai/chat")) {
            requestId = new Headers(init?.headers).get("Idempotency-Key")!;
            throw new TypeError("accepted but response lost before meta");
          }
          const current = {
            ...generation,
            client_request_id: requestId,
            status: completed ? "completed" : "streaming",
          };
          return String(url).endsWith("/active-generations")
            ? json({ data: completed ? [] : [current] })
            : json({ data: current });
        }),
      );
      useAiChatStore.getState().setInput(input.message);
      await act(async () => {
        expect(
          await useAiChatStore.getState().send({
            ...input,
            owner,
            actionReceiptGenerationId: "receipt-response-lost",
            actionReceiptAttachmentId: "attachment-response-lost",
          }),
        ).toMatchObject({ accepted: false });
      });
      expect(useAiChatStore.getState().acceptedCommand).toBeNull();
      if (changedDraft) {
        // Even equal text is a newer edit: draft revision must protect it.
        useAiChatStore.getState().setInput(input.message);
      }
      await act(async () => {
        await useAiChatStore.getState().recover();
      });
      const accepted = useAiChatStore.getState().acceptedCommand;
      expect(accepted).toEqual({
        sequence: 1,
        requestId,
        owner: owner ?? "main",
        sessionId: input.sessionId,
        actionReceiptGenerationId: "receipt-response-lost",
        actionReceiptAttachmentId: "attachment-response-lost",
      });
      expect(useAiChatStore.getState().input).toBe(
        clearsDraft ? "" : input.message,
      );
      expect(
        JSON.parse(sessionStorage.getItem("opc-ai-pending-request-v1")!),
      ).toEqual({ requestId, sessionId: input.sessionId });
      await act(async () => {
        completed = true;
        await useAiChatStore.getState().recover();
      });
      expect(useAiChatStore.getState().acceptedCommand).toBe(accepted);
    },
  );

  it.each([
    { firstOwner: undefined, nextOwner: "main", reuses: true },
    { firstOwner: "main", nextOwner: "side:new", reuses: false },
    { firstOwner: "side:old", nextOwner: "main", reuses: false },
    { firstOwner: "side:old", nextOwner: "side:new", reuses: false },
  ])(
    "isolates unknown first-session retries from $firstOwner to $nextOwner",
    async ({ firstOwner, nextOwner, reuses }) => {
      const keys: string[] = [];
      const bodies: { session_id: string }[] = [];
      vi.stubGlobal(
        "fetch",
        vi.fn(async (_url, init?: RequestInit) => {
          keys.push(new Headers(init?.headers).get("Idempotency-Key")!);
          bodies.push(JSON.parse(String(init?.body)));
          if (keys.length === 1) throw new TypeError("lost first response");
          const channel = sse(init?.signal);
          channel.push("meta", {
            generation_id: generation.id,
            session_id: input.sessionId,
          });
          channel.push("done", { generation_id: generation.id });
          channel.end();
          return channel.response;
        }),
      );
      await act(async () => {
        await useAiChatStore.getState().send({
          ...input,
          owner: firstOwner,
          sessionId: undefined,
        });
        await useAiChatStore.getState().send({ ...input, owner: nextOwner });
      });
      expect(keys[0] === keys[1]).toBe(reuses);
      expect(bodies[1].session_id).toBe(reuses ? "" : input.sessionId);
      expect(useAiChatStore.getState().acceptedCommand?.owner).toBe(nextOwner);
    },
  );

  it("does not publish receipt acceptance for an unrelated recovered generation", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: unknown) => {
        if (String(url).endsWith("/ai/chat"))
          throw new TypeError("unknown acceptance");
        if (String(url).endsWith("/active-generations"))
          return json({ data: [generation] });
        return json({ code: "AI_GENERATION_NOT_FOUND" }, 404);
      }),
    );
    useAiChatStore.getState().setInput(input.message);
    await act(async () => {
      await useAiChatStore.getState().send({
        ...input,
        actionReceiptGenerationId: "receipt-unmatched",
      });
      await useAiChatStore.getState().recover();
    });
    expect(useAiChatStore.getState().acceptedCommand).toBeNull();
    expect(useAiChatStore.getState().input).toBe(input.message);
    useAiChatStore.getState().forgetSession(input.sessionId);
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
