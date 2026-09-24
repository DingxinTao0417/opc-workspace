import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import { getAiAgentInbox, parseAiAgentInbox } from "./aiAgentInbox";

const approval = {
  kind: "approval",
  id: "018f0000-0000-7000-8000-000000000101",
  created_at: "2026-09-19T12:01:00Z",
  action: "task.create",
  session_id: "018f0000-0000-7000-8000-000000000102",
  session_title: "整理新任务",
  generation_id: "018f0000-0000-7000-8000-000000000103",
} as const;

const review = {
  kind: "review",
  id: "018f0000-0000-7000-8000-000000000201",
  created_at: "2026-09-19T12:00:00Z",
  task_id: "018f0000-0000-7000-8000-000000000202",
  task_title: "验收 Agent 交付",
  submission_id: "018f0000-0000-7000-8000-000000000201",
  sequence: 3,
  submission_origin: "manual",
} as const;

const automationFailure = {
  kind: "automation_failure",
  id: "018f0000-0000-7000-8000-000000000301",
  created_at: "2026-09-19T11:59:00Z",
  rule_id: "018f0000-0000-7000-8000-000000000302",
  rule_name: "每日查看今日任务",
  attempt: 2,
  retryable: true,
  retry_at: "2026-09-19T12:10:00Z",
  error_code: "ACTION_WRITE_FAILED",
} as const;

const knowledgeFailure = {
  kind: "knowledge_failure",
  id: "018f0000-0000-7000-8000-000000000401",
  created_at: "2026-09-19T11:58:00Z",
  source_id: "018f0000-0000-7000-8000-000000000402",
  source_name: "billing-guide.md",
  operation: "reindex",
  attempt: 2,
  error_code: "KNOWLEDGE_INDEX_INTERRUPTED",
} as const;

const generationFailure = {
  kind: "generation_failure",
  id: "018f0000-0000-7000-8000-000000000501",
  created_at: "2026-09-19T11:57:00Z",
  session_id: "018f0000-0000-7000-8000-000000000502",
  session_title: "恢复失败的对话",
  generation_id: "018f0000-0000-7000-8000-000000000501",
  error_code: "AI_PROVIDER_ERROR",
} as const;

const providerIssue = {
  kind: "provider_issue",
  id: "018f0000-0000-7000-8000-000000000601",
  created_at: "2026-09-19T11:56:00Z",
  provider_id: "018f0000-0000-7000-8000-000000000601",
  provider_name: "本地推理服务",
  provider_model: "qwen-local",
  provider_status: "unavailable",
  health_status: "unhealthy",
  error_code: "AI_ENDPOINT_UNREACHABLE",
} as const;

const adapterIssue = {
  kind: "adapter_issue",
  id: "018f0000-0000-7000-8000-000000000651",
  created_at: "2026-09-19T11:55:30Z",
  adapter_id: "018f0000-0000-7000-8000-000000000651",
  adapter_name: "本地文本诊断执行器",
  adapter_key: "builtin-local-text-v1",
  adapter_status: "disabled",
  health_status: "blocked",
  isolation_status: "unverified",
  execution_ready: false,
  error_code: "PLATFORM_ISOLATION_UNVERIFIED",
} as const;

const maintenanceFailure = {
  kind: "maintenance_failure",
  id: "018f0000-0000-7000-8000-000000000701",
  created_at: "2026-09-19T11:55:00Z",
  component: "storage",
  operation: "low_space",
  error_code: "storage_low_space",
} as const;

const agentRun = {
  kind: "agent_run",
  id: "018f0000-0000-7000-8000-000000000901",
  created_at: "2026-09-19T11:54:00Z",
  task_id: "018f0000-0000-7000-8000-000000000902",
  task_title: "恢复 Agent 交付",
  attempt: 2,
  run_status: "failed",
  output_delivery_status: "not_ready",
} as const;

const continuation = {
  kind: "continuation",
  id: "018f0000-0000-7000-8000-000000000951",
  created_at: "2026-09-19T11:53:00Z",
  session_id: "018f0000-0000-7000-8000-000000000952",
  session_title: "继续发布计划",
  continuation_status: "waiting",
  continuation_reason: "pending_approval",
  continuation_max_turns: 4,
  continuation_turns_started: 1,
  continuation_expires_at: "2026-09-19T13:00:00Z",
} as const;

const payload = {
  data: [
    approval,
    review,
    automationFailure,
    knowledgeFailure,
    generationFailure,
    providerIssue,
    maintenanceFailure,
  ],
  meta: {
    kind_filter: "all",
    total: 7,
    approval_total: 1,
    access_request_total: 0,
    review_total: 1,
    automation_failure_total: 1,
    knowledge_failure_total: 1,
    generation_failure_total: 1,
    provider_issue_total: 1,
    adapter_issue_total: 0,
    maintenance_failure_total: 1,
    continuation_total: 0,
    has_more: false,
    next_offset: null,
    window_limited: false,
    as_of: "2026-09-19T12:02:00Z",
  },
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Agent Inbox", () => {
  it("accepts a metadata-only access request without treating it as a grant", () => {
    const request = {
      kind: "access_request",
      id: "018f0000-0000-7000-8000-000000000801",
      created_at: "2026-09-19T12:03:00Z",
      session_id: "018f0000-0000-7000-8000-000000000802",
      session_title: "核对工作台权限",
      generation_id: "018f0000-0000-7000-8000-000000000801",
    };
    const page = {
      data: [request],
      meta: {
        ...payload.meta,
        kind_filter: "access_request",
        total: 1,
        access_request_total: 1,
      },
    };
    expect(parseAiAgentInbox(page, "access_request")).toEqual(page);
    for (const invalid of [
      { ...request, scopes: ["work"] },
      { ...request, grant: true },
      { ...request, generation_id: approval.generation_id },
    ]) {
      expect(() =>
        parseAiAgentInbox({ ...page, data: [invalid] }, "access_request"),
      ).toThrow(ApiError);
    }
    expect(() =>
      parseAiAgentInbox(
        { ...page, meta: { ...page.meta, access_request_total: 0 } },
        "access_request",
      ),
    ).toThrow(ApiError);
  });

  it("keeps the inbox readable during an old-sidecar rolling restart only when legacy totals prove zero requests", () => {
    const { access_request_total: _oldField, ...legacyMeta } = payload.meta;
    expect(parseAiAgentInbox({ ...payload, meta: legacyMeta }, "all")).toEqual(
      payload,
    );
    expect(() =>
      parseAiAgentInbox(
        { ...payload, meta: { ...legacyMeta, total: payload.meta.total + 1 } },
        "all",
      ),
    ).toThrow(ApiError);
    expect(() =>
      parseAiAgentInbox(
        {
          ...payload,
          data: [{ kind: "access_request" }, ...payload.data],
          meta: legacyMeta,
        },
        "all",
      ),
    ).toThrow(ApiError);
  });
  it("accepts only the metadata-only discriminated response", () => {
    expect(parseAiAgentInbox(payload, "all")).toEqual(payload);
    for (const invalid of [
      {
        ...payload,
        data: [
          { ...approval, changes: { title: "secret" } },
          review,
          automationFailure,
          knowledgeFailure,
          generationFailure,
          providerIssue,
          maintenanceFailure,
        ],
      },
      {
        ...payload,
        data: [
          { ...approval, task_id: review.task_id },
          review,
          automationFailure,
          knowledgeFailure,
          generationFailure,
          providerIssue,
          maintenanceFailure,
        ],
      },
      {
        ...payload,
        data: [
          review,
          approval,
          automationFailure,
          knowledgeFailure,
          generationFailure,
          providerIssue,
          maintenanceFailure,
        ],
      },
      { ...payload, meta: { ...payload.meta, total: 8 } },
      { ...payload, meta: { ...payload.meta, next_offset: 1 } },
    ]) {
      expect(() => parseAiAgentInbox(invalid, "all")).toThrow(ApiError);
    }
  });

  it("sends strict kind and pagination parameters", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          data: [approval],
          meta: {
            ...payload.meta,
            kind_filter: "approval",
            total: 1,
            review_total: 1,
            automation_failure_total: 1,
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    await getAiAgentInbox("approval", 20, 4);
    const url = new URL(
      String(fetchMock.mock.calls[0]?.[0]),
      "http://localhost",
    );
    expect(url.pathname).toBe("/api/v1/ai/inbox");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      kind: "approval",
      limit: "20",
      offset: "4",
    });
  });

  it("accepts automation failures only as bounded metadata", () => {
    const automationPayload = {
      data: [automationFailure],
      meta: {
        ...payload.meta,
        kind_filter: "automation_failure",
        total: 1,
      },
    };
    expect(parseAiAgentInbox(automationPayload, "automation_failure")).toEqual(
      automationPayload,
    );
    expect(() =>
      parseAiAgentInbox(
        {
          ...automationPayload,
          data: [{ ...automationFailure, action_snapshot: { secret: true } }],
        },
        "automation_failure",
      ),
    ).toThrow(ApiError);
  });

  it("accepts only the current knowledge failure identity and bounded metadata", () => {
    const knowledgePayload = {
      data: [knowledgeFailure],
      meta: {
        ...payload.meta,
        kind_filter: "knowledge_failure",
        total: 1,
      },
    };
    expect(parseAiAgentInbox(knowledgePayload, "knowledge_failure")).toEqual(
      knowledgePayload,
    );
    expect(() =>
      parseAiAgentInbox(
        {
          ...knowledgePayload,
          data: [{ ...knowledgeFailure, source_body: "secret" }],
        },
        "knowledge_failure",
      ),
    ).toThrow(ApiError);
  });

  it("accepts failed or startup-interrupted current generations without response content", () => {
    const generationPayload = {
      data: [generationFailure],
      meta: {
        ...payload.meta,
        kind_filter: "generation_failure",
        total: 1,
      },
    };
    expect(parseAiAgentInbox(generationPayload, "generation_failure")).toEqual(
      generationPayload,
    );
    const interruptedPayload = {
      ...generationPayload,
      data: [
        {
          ...generationFailure,
          error_code: "AI_GENERATION_INTERRUPTED",
        },
      ],
    };
    expect(parseAiAgentInbox(interruptedPayload, "generation_failure")).toEqual(
      interruptedPayload,
    );
    expect(() =>
      parseAiAgentInbox(
        {
          ...generationPayload,
          data: [{ ...generationFailure, content: "private partial answer" }],
        },
        "generation_failure",
      ),
    ).toThrow(ApiError);
  });

  it("accepts provider recovery metadata without endpoint or credential fields", () => {
    const providerPayload = {
      data: [providerIssue],
      meta: {
        ...payload.meta,
        kind_filter: "provider_issue",
        total: 1,
      },
    };
    expect(parseAiAgentInbox(providerPayload, "provider_issue")).toEqual(
      providerPayload,
    );
    expect(() =>
      parseAiAgentInbox(
        {
          ...providerPayload,
          data: [
            {
              ...providerIssue,
              base_url: "http://127.0.0.1:11434/private",
            },
          ],
        },
        "provider_issue",
      ),
    ).toThrow(ApiError);
  });

  it("accepts only code-owned system maintenance identities", () => {
    const maintenancePayload = {
      data: [maintenanceFailure],
      meta: {
        ...payload.meta,
        kind_filter: "maintenance_failure",
        total: 1,
      },
    };
    expect(
      parseAiAgentInbox(maintenancePayload, "maintenance_failure"),
    ).toEqual(maintenancePayload);
    for (const invalid of [
      { ...maintenanceFailure, message: "private local path" },
      { ...maintenanceFailure, error_code: "database_runtime_failed" },
      { ...maintenanceFailure, operation: "unknown" },
    ]) {
      expect(() =>
        parseAiAgentInbox(
          { ...maintenancePayload, data: [invalid] },
          "maintenance_failure",
        ),
      ).toThrow(ApiError);
    }
  });

  it("accepts metadata-only Agent Run attention items and rejects content fields", () => {
    const agentRunPayload = {
      data: [agentRun],
      meta: {
        ...payload.meta,
        kind_filter: "agent_run",
        total: 1,
        agent_run_total: 1,
      },
    };
    expect(parseAiAgentInbox(agentRunPayload, "agent_run")).toEqual(
      agentRunPayload,
    );
    for (const invalid of [
      { ...agentRun, result_text: "private output" },
      { ...agentRun, run_status: "unknown" },
      { ...agentRun, output_delivery_status: "unknown" },
      { ...agentRun, attempt: 0 },
    ]) {
      expect(() =>
        parseAiAgentInbox({ ...agentRunPayload, data: [invalid] }, "agent_run"),
      ).toThrow(ApiError);
    }
  });

  it("accepts active plan continuations without inheriting authorization details", () => {
    const continuationPayload = {
      data: [continuation],
      meta: {
        ...payload.meta,
        kind_filter: "continuation",
        total: 1,
        continuation_total: 1,
      },
    };
    expect(parseAiAgentInbox(continuationPayload, "continuation")).toEqual(
      continuationPayload,
    );
    for (const invalid of [
      { ...continuation, continuation_status: "completed" },
      { ...continuation, continuation_reason: "private_reason" },
      { ...continuation, continuation_turns_started: 5 },
      { ...continuation, workspace: { scopes: ["actions"] } },
      { ...continuation, provider: { id: "private" } },
    ]) {
      expect(() =>
        parseAiAgentInbox(
          { ...continuationPayload, data: [invalid] },
          "continuation",
        ),
      ).toThrow(ApiError);
    }
  });

  it("accepts enabled adapter issues and rejects ready or privileged fields", () => {
    const adapterPayload = {
      data: [adapterIssue],
      meta: {
        ...payload.meta,
        kind_filter: "adapter_issue",
        total: 1,
        adapter_issue_total: 1,
      },
    };
    expect(parseAiAgentInbox(adapterPayload, "adapter_issue")).toEqual(
      adapterPayload,
    );
    for (const invalid of [
      { ...adapterIssue, executable_ref: "PRIVATE_PATH" },
      { ...adapterIssue, health_status: "healthy" },
      { ...adapterIssue, health_status: "unknown" },
      { ...adapterIssue, adapter_status: "enabled", health_status: "healthy" },
      { ...adapterIssue, execution_ready: "false" },
    ]) {
      expect(() =>
        parseAiAgentInbox(
          { ...adapterPayload, data: [invalid] },
          "adapter_issue",
        ),
      ).toThrow(ApiError);
    }
  });

  it("treats a missing adapter_issue_total as zero only when no adapter items are present", () => {
    const { adapter_issue_total: _omit, ...legacyMeta } = payload.meta;
    expect(parseAiAgentInbox({ ...payload, meta: legacyMeta }, "all")).toEqual({
      ...payload,
      meta: legacyMeta,
    });
    expect(() =>
      parseAiAgentInbox(
        {
          data: [adapterIssue],
          meta: {
            ...legacyMeta,
            kind_filter: "all",
            total: (legacyMeta.total as number) + 1,
            provider_issue_total: legacyMeta.provider_issue_total,
          },
        },
        "all",
      ),
    ).toThrow(ApiError);
  });
});
