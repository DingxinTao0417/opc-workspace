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
  it("sends project file disclosure only on the explicit request, never the next one", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse(['event: done\ndata: {"generation_id":"g"}\n\n']),
    );
    vi.stubGlobal("fetch", fetchMock);
    const file = {
      provider_id: "p",
      provider_version: 1,
      session_id: "s",
      confirmed: true as const,
      expires_at: "2026-09-22T12:00:00Z",
      files: [{ path: "a.txt", content: "private", sha256: "a".repeat(64) }],
    };
    await streamAiChat({
      providerId: "p",
      sessionId: "s",
      message: "analyze",
      projectFiles: file,
      onEvent: () => {},
    });
    await streamAiChat({
      providerId: "p",
      sessionId: "s",
      message: "next",
      onEvent: () => {},
    });
    const bodies = fetchMock.mock.calls.map((call) =>
      JSON.parse((call as unknown as [string, RequestInit])[1].body as string),
    );
    expect(bodies[0].project_files).toEqual(file);
    expect(bodies[1]).not.toHaveProperty("project_files");
  });
  it("sends an explicit receipt source only for this message without granting tools", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse(['event: done\ndata: {"generation_id":"g"}\n\n']),
    );
    vi.stubGlobal("fetch", fetchMock);
    await streamAiChat({
      providerId: "p",
      sessionId: "s",
      message: "continue",
      actionReceiptGenerationId: "source-generation",
      actionRecheckProposalId: "rejected-proposal",
      onEvent: () => {},
    });
    const body = (index: number) =>
      JSON.parse(
        (fetchMock.mock.calls[index] as unknown as [string, RequestInit])[1]
          .body as string,
      );
    expect(body(0)).toMatchObject({
      action_receipt_generation_id: "source-generation",
      action_recheck_proposal_id: "rejected-proposal",
    });
    expect(body(0)).not.toHaveProperty("workspace");
    await streamAiChat({
      providerId: "p",
      sessionId: "s",
      message: "next",
      onEvent: () => {},
    });
    expect(body(1)).not.toHaveProperty("action_receipt_generation_id");
    expect(body(1)).not.toHaveProperty("action_recheck_proposal_id");
  });
  it("transmits only the explicitly supplied one-run workspace grant", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse(['event: done\ndata: {"generation_id":"g"}\n\n']),
    );
    vi.stubGlobal("fetch", fetchMock);
    const workspace = { provider_version: 4, scopes: ["work" as const] };
    await streamAiChat({
      providerId: "p",
      message: "query",
      workspace,
      onEvent: () => {},
    });
    expect(
      JSON.parse(
        (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1]
          .body as string,
      ).workspace,
    ).toEqual(workspace);
    await streamAiChat({ providerId: "p", message: "next", onEvent: () => {} });
    expect(
      JSON.parse(
        (fetchMock.mock.calls[1] as unknown as [string, RequestInit])[1]
          .body as string,
      ),
    ).not.toHaveProperty("workspace");
  });
  it("handles CRLF split across chunks and ignores null/unknown frames", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          "event: delta\r\ndata: null\r\n\r",
          '\nevent: extension\ndata: {}\n\nevent: delta\r\ndata: {"generation_id":"g","text":"回答"}\r\n\r\n',
          'event: done\r\ndata: {"generation_id":"g"}\r\n\r\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p",
      message: "hi",
      onEvent: (event) => events.push(event),
    });
    expect(events).toEqual([
      { type: "delta", generationId: "g", text: "回答" },
      { type: "done", generationId: "g" },
    ]);
  });

  it("does not accept a malformed or unterminated done frame", async () => {
    for (const frame of [
      "event: done\ndata: null\n\n",
      'event: done\ndata: {"generation_id":"g"}',
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => sseResponse([frame])),
      );
      await expect(
        streamAiChat({ providerId: "p", message: "hi", onEvent: () => {} }),
      ).rejects.toMatchObject({ code: "AI_STREAM_INCOMPLETE" });
    }
  });
  it("rejects EOF without a terminal event and retains delivered content", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: delta\ndata: {"generation_id":"gen-1","text":"partial"}\n\n',
        ]),
      ),
    );
    const onEvent = vi.fn();
    await expect(
      streamAiChat({ providerId: "p-1", message: "hi", onEvent }),
    ).rejects.toMatchObject({ code: "AI_STREAM_INCOMPLETE" });
    expect(onEvent).toHaveBeenCalledWith({
      type: "delta",
      generationId: "gen-1",
      text: "partial",
    });
  });
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
      context: {
        provider_version: 3,
        sources: [{ type: "task", id: "task-1", expected_version: 4 }],
        knowledge: [
          {
            source_id: "source-1",
            document_id: "document-1",
            chunk_id: "chunk-1",
            expected_source_version: 2,
            expected_document_version: 1,
          },
        ],
      },
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
    expect(JSON.parse(String(init.body))).toMatchObject({
      context: {
        provider_version: 3,
        sources: [{ type: "task", id: "task-1", expected_version: 4 }],
        knowledge: [
          {
            source_id: "source-1",
            document_id: "document-1",
            chunk_id: "chunk-1",
            expected_source_version: 2,
            expected_document_version: 1,
          },
        ],
      },
    });
  });

  it("parses a closed-enum workspace panel request without accepting a URL or path", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_panel\ndata: {"generation_id":"gen-1","panel":"review"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开变更审查",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_panel",
      generationId: "gen-1",
      panel: "review",
    });
  });

  it("parses a bounded two-panel split request", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_panels\ndata: {"generation_id":"gen-1","panels":["files","review"],"split_ratio":0.62}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "并排打开文件和变更审查",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_panels",
      generationId: "gen-1",
      panels: ["files", "review"],
      splitRatio: 0.62,
    });
  });

  it("continues to parse a legacy two-panel event without a ratio", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_panels\ndata: {"generation_id":"gen-1","panels":["files","review"]}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "并排打开文件和变更审查",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_panels",
      generationId: "gen-1",
      panels: ["files", "review"],
    });
  });

  it("parses a separately consented browser request without opening it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_browser_navigation\ndata: {"generation_id":"gen-1","url":"https://example.com/docs?view=agent"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开文档",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_browser_navigation",
      generationId: "gen-1",
      url: "https://example.com/docs?view=agent",
    });
  });

  it("parses a browser action request without performing it", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_browser_action\ndata: {"generation_id":"gen-1","action":"reload"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "刷新浏览器",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_browser_action",
      generationId: "gen-1",
      action: "reload",
    });
  });

  it.each([
    ["work", "actions"],
    ["work", "actions", "outputs", "clients"],
  ])("parses complete fresh-grant recommendations %j", async (...scopes) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_access_request\ndata: ${JSON.stringify({ generation_id: "gen-1", scopes })}\n\n`,
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "创建任务",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_access_request",
      generationId: "gen-1",
      scopes,
    });
  });

  it("parses a metadata-only committed work-plan update", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_plan_updated\ndata: {"generation_id":"gen-1","version":2,"step_count":3}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "更新计划",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_plan_updated",
      generationId: "gen-1",
      version: 2,
      stepCount: 3,
    });
  });

  it("parses a user-confirmed local record navigation without accepting a route", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"task","record_id":"018f0000-0000-7000-8000-000000000001"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开任务",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_record_navigation",
      generationId: "gen-1",
      recordType: "task",
      recordId: "018f0000-0000-7000-8000-000000000001",
    });
  });

  it("parses an Agent Run navigation only with its server-derived Task identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"agent_run","record_id":"018f0000-0000-7000-8000-000000000011","task_id":"018f0000-0000-7000-8000-000000000012"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开执行过程",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_record_navigation",
      generationId: "gen-1",
      recordType: "agent_run",
      recordId: "018f0000-0000-7000-8000-000000000011",
      taskId: "018f0000-0000-7000-8000-000000000012",
    });
  });

  it("parses a Task Submission navigation only with its server-derived Task identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"task_submission","record_id":"018f0000-0000-7000-8000-000000000021","task_id":"018f0000-0000-7000-8000-000000000022"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开提交批次",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_record_navigation",
      generationId: "gen-1",
      recordType: "task_submission",
      recordId: "018f0000-0000-7000-8000-000000000021",
      taskId: "018f0000-0000-7000-8000-000000000022",
    });
  });

  it("parses a Task Artifact navigation only with server-derived Task and Submission identities", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          'event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"task_artifact","record_id":"018f0000-0000-7000-8000-000000000031","task_id":"018f0000-0000-7000-8000-000000000032","submission_id":"018f0000-0000-7000-8000-000000000033"}\n\n',
          'event: done\ndata: {"generation_id":"gen-1"}\n\n',
        ]),
      ),
    );
    const events: unknown[] = [];
    await streamAiChat({
      providerId: "p-1",
      message: "打开具体产出",
      onEvent: (event) => events.push(event),
    });
    expect(events).toContainEqual({
      type: "workspace_record_navigation",
      generationId: "gen-1",
      recordType: "task_artifact",
      recordId: "018f0000-0000-7000-8000-000000000031",
      taskId: "018f0000-0000-7000-8000-000000000032",
      submissionId: "018f0000-0000-7000-8000-000000000033",
    });
  });

  it.each([
    ["project_note", "project"],
    ["client_activity", "client"],
    ["client_followup", "client"],
  ])(
    "parses %s navigation only with a server-derived %s identity",
    async (recordType) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          sseResponse([
            'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
            `event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"${recordType}","record_id":"018f0000-0000-7000-8000-000000000021","parent_id":"018f0000-0000-7000-8000-000000000022"}\n\n`,
            'event: done\ndata: {"generation_id":"gen-1"}\n\n',
          ]),
        ),
      );
      const events: unknown[] = [];
      await streamAiChat({
        providerId: "p-1",
        message: "打开记录",
        onEvent: (event) => events.push(event),
      });
      expect(events).toContainEqual({
        type: "workspace_record_navigation",
        generationId: "gen-1",
        recordType,
        recordId: "018f0000-0000-7000-8000-000000000021",
        parentId: "018f0000-0000-7000-8000-000000000022",
      });
    },
  );

  it.each(["financial_entry", "invoice", "task_saved_view"])(
    "parses a %s navigation without extra record data",
    async (recordType) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          sseResponse([
            'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
            `event: workspace_record_navigation\ndata: {"generation_id":"gen-1","record_type":"${recordType}","record_id":"018f0000-0000-7000-8000-000000000021"}\n\n`,
            'event: done\ndata: {"generation_id":"gen-1"}\n\n',
          ]),
        ),
      );
      const events: unknown[] = [];
      await streamAiChat({
        providerId: "p-1",
        message: "打开记录",
        onEvent: (event) => events.push(event),
      });
      expect(events).toContainEqual({
        type: "workspace_record_navigation",
        generationId: "gen-1",
        recordType,
        recordId: "018f0000-0000-7000-8000-000000000021",
      });
    },
  );

  it.each([
    '{"generation_id":"gen-1","panel":"shell"}',
    '{"generation_id":"gen-1","panel":"review","url":"https://example.invalid"}',
    '{"generation_id":"gen-1","panel":"review","path":"C:\\\\secret"}',
  ])("rejects an invalid workspace panel event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_panel\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "打开面板",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","panels":["files"]}',
    '{"generation_id":"gen-1","panels":["files","files"]}',
    '{"generation_id":"gen-1","panels":["browser","browser"]}',
    '{"generation_id":"gen-1","panels":["files","shell"]}',
    '{"generation_id":"gen-1","panels":["files","review"],"path":"C:\\\\secret"}',
    '{"generation_id":"gen-1","panels":["files","review"],"split_ratio":0.249}',
    '{"generation_id":"gen-1","panels":["files","review"],"split_ratio":0.751}',
    '{"generation_id":"gen-1","panels":["files","review"],"split_ratio":null}',
    '{"generation_id":"gen-1","panels":["files","review"],"split_ratio":"0.5"}',
  ])("rejects an invalid workspace split event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          "event: workspace_panels\ndata: " + payload + "\n\n",
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "并排打开面板",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","url":"ftp://example.com"}',
    '{"generation_id":"gen-1","url":"https://user:secret@example.com"}',
    '{"generation_id":"gen-1","url":"https://example.com","panel":"browser"}',
    '{"generation_id":"gen-1","url":"javascript:alert(1)"}',
  ])("rejects an invalid browser navigation event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_browser_navigation\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "打开网页",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","action":"click"}',
    '{"generation_id":"gen-1","action":"reload","url":"https://example.com"}',
    '{"generation_id":"gen-1","action":0}',
  ])("rejects an invalid browser action event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_browser_action\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({ providerId: "p-1", message: "刷新", onEvent: () => {} }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","version":0,"step_count":3}',
    '{"generation_id":"gen-1","version":1,"step_count":13}',
    '{"generation_id":"gen-1","version":1,"step_count":3,"title":"private"}',
    '{"generation_id":"gen-1","version":"1","step_count":3}',
  ])("rejects an invalid work-plan update event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_plan_updated\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "更新计划",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","scopes":[]}',
    '{"generation_id":"gen-1","scopes":["work","work"]}',
    '{"generation_id":"gen-1","scopes":["unknown"]}',
    '{"generation_id":"gen-1","scopes":["work"],"granted":true}',
  ])("rejects an invalid workspace access request %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_access_request\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "创建任务",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    '{"generation_id":"gen-1","record_type":"person","record_id":"018f0000-0000-7000-8000-000000000001"}',
    '{"generation_id":"gen-1","record_type":"task","record_id":"not-a-uuid"}',
    '{"generation_id":"gen-1","record_type":"task","record_id":"018f0000-0000-7000-8000-000000000001","route":"/tasks/anything"}',
    '{"generation_id":"gen-1","record_type":"task","record_id":"018f0000-0000-7000-8000-000000000001","task_id":"018f0000-0000-7000-8000-000000000002"}',
    '{"generation_id":"gen-1","record_type":"agent_run","record_id":"018f0000-0000-7000-8000-000000000001"}',
    '{"generation_id":"gen-1","record_type":"agent_run","record_id":"018f0000-0000-7000-8000-000000000001","task_id":"not-a-uuid"}',
    '{"generation_id":"gen-1","record_type":"task_artifact","record_id":"018f0000-0000-7000-8000-000000000001"}',
    '{"generation_id":"gen-1","record_type":"task_artifact","record_id":"018f0000-0000-7000-8000-000000000001","task_id":"018f0000-0000-7000-8000-000000000002","submission_id":"bad"}',
    '{"generation_id":"gen-1","record_type":"task_artifact","record_id":"018f0000-0000-7000-8000-000000000001","task_id":"018f0000-0000-7000-8000-000000000002","submission_id":"018f0000-0000-7000-8000-000000000003","route":"/tasks/other"}',
    '{"generation_id":"gen-1","record_type":"project_note","record_id":"018f0000-0000-7000-8000-000000000001"}',
    '{"generation_id":"gen-1","record_type":"client_followup","record_id":"018f0000-0000-7000-8000-000000000001","parent_id":"not-a-uuid"}',
    '{"generation_id":"gen-1","record_type":"client_activity","record_id":"018f0000-0000-7000-8000-000000000001","parent_id":"018f0000-0000-7000-8000-000000000002","route":"/clients/other"}',
    '{"generation_id":"gen-1","record_type":"invoice","record_id":"018f0000-0000-7000-8000-000000000001","parent_id":"018f0000-0000-7000-8000-000000000002"}',
    '{"generation_id":"gen-1","record_type":"task_saved_view","record_id":"018f0000-0000-7000-8000-000000000001","definition":{"status":"todo"}}',
  ])("rejects an invalid record navigation event %s", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        sseResponse([
          'event: meta\ndata: {"protocol":"openai_chat","generation_id":"gen-1","session_id":"s-1","model":"gpt-test","provider_id":"p-1","sse_protocol":"opc-ai-sse-v1"}\n\n',
          `event: workspace_record_navigation\ndata: ${payload}\n\n`,
        ]),
      ),
    );
    await expect(
      streamAiChat({
        providerId: "p-1",
        message: "打开记录",
        onEvent: () => {},
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
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
