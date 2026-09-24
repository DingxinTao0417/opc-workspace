import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAiPlanContinuation,
  getActiveAiPlanContinuations,
  getRecentAiPlanContinuations,
  getAiPlanContinuation,
  isContinuationActive,
  parseAiPlanContinuation,
  stopAiPlanContinuation,
  toggleContinuationScope,
  type AiPlanContinuation,
  type CreateAiPlanContinuation,
} from "./aiPlanContinuation";
import { getAiGeneration } from "./aiActions";
import { resetRuntimeConnection } from "./client";

const id = {
  lease: "018f0000-0000-7000-8000-000000007001",
  session: "018f0000-0000-7000-8000-000000007002",
  provider: "018f0000-0000-7000-8000-000000007003",
  generation: "018f0000-0000-7000-8000-000000007004",
  source: "018f0000-0000-7000-8000-000000007005",
  other: "018f0000-0000-7000-8000-000000007006",
};
function lease(): AiPlanContinuation {
  return {
    id: id.lease,
    session_id: id.session,
    version: 1,
    status: "waiting",
    reason: "ready",
    initial_plan_version: 3,
    current_plan_version: 3,
    provider: {
      id: id.provider,
      name: "Selected Provider",
      kind: "remote",
      protocol: "anthropic_messages",
      model: "selected-model",
      version: 2,
      config_version: 4,
    },
    workspace: { provider_version: 2, scopes: ["work", "actions"] },
    max_turns: 8,
    turns_started: 0,
    current_generation_id: null,
    last_generation_id: null,
    created_at: "2026-09-21T10:00:00.123456789Z",
    updated_at: "2026-09-21T10:00:00.123456789Z",
    expires_at: "2026-09-21T10:30:00.123456789Z",
  };
}
function input(): CreateAiPlanContinuation {
  return {
    expected_session_version: 7,
    expected_plan_version: 3,
    provider_id: id.provider,
    expected_provider_version: 2,
    expected_provider_config_version: 4,
    workspace: { provider_version: 2, scopes: ["work", "actions"] },
    max_turns: 8,
    ttl_minutes: 30,
    confirm_automatic_continuation: true,
  };
}
function mutate(value: unknown, path: string, replacement: unknown) {
  const keys = path.split(".");
  let row = value as Record<string, unknown>;
  for (const key of keys.slice(0, -1))
    row = row[key] as Record<string, unknown>;
  row[keys.at(-1)!] = replacement;
}
function stub(data: unknown, status = 200) {
  const fetchMock = vi.fn<typeof fetch>(
    async () =>
      new Response(JSON.stringify({ data }), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("bounded continuation lease API", () => {
  it("reads the exact session and preserves an absent lease as null", async () => {
    const fetchMock = stub(lease());
    await expect(getAiPlanContinuation(id.session)).resolves.toEqual(lease());
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/ai/sessions/${id.session}/continuation`,
    );
    expect(fetchMock.mock.calls[0][1]?.method ?? "GET").toBe("GET");
    stub(null);
    await expect(getAiPlanContinuation(id.session)).resolves.toBeNull();
  });

  it("serializes only explicit authorizations and versions for POST", async () => {
    const fetchMock = stub(lease(), 201);
    const request = input();
    await expect(
      createAiPlanContinuation(id.session, request),
    ).resolves.toEqual(lease());
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/ai/sessions/${id.session}/continuation`,
    );
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual(
      request,
    );
  });

  it("requires a separate restart choice and verifies that the server retained it", async () => {
    const request = {
      ...input(),
      resume_after_restart: true,
      confirm_restart_continuation: true as const,
    };
    const fetchMock = stub({ ...lease(), resume_after_restart: true }, 201);
    await expect(
      createAiPlanContinuation(id.session, request),
    ).resolves.toMatchObject({
      resume_after_restart: true,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual(
      request,
    );
    stub(lease(), 201);
    await expect(
      createAiPlanContinuation(id.session, request),
    ).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    { resume_after_restart: true },
    { confirm_restart_continuation: true },
    { resume_after_restart: 1, confirm_restart_continuation: true },
    { resume_after_restart: false, confirm_restart_continuation: true },
  ])("refuses an incomplete restart authorization %j", async (extra) => {
    const fetchMock = stub(lease(), 201);
    await expect(
      createAiPlanContinuation(id.session, {
        ...input(),
        ...extra,
      } as CreateAiPlanContinuation),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("stops only the exact lease without sending a business command", async () => {
    const stopped = { ...lease(), status: "stopped", reason: "user_stopped" };
    const fetchMock = stub(stopped);
    await expect(stopAiPlanContinuation(id.lease)).resolves.toEqual(stopped);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/ai/continuations/${id.lease}/stop`,
    );
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined();
  });

  it("keeps an owned generation visible while a stopped worker is exiting", () => {
    const value = {
      ...lease(),
      version: 3,
      status: "stopped",
      reason: "user_stopped",
      turns_started: 1,
      current_generation_id: id.generation,
      last_generation_id: id.generation,
    };
    expect(parseAiPlanContinuation(value)).toEqual(value);
    expect(isContinuationActive(parseAiPlanContinuation(value))).toBe(false);
  });

  it("reads only active leases and does not auto-create any", async () => {
    const fetchMock = stub([lease()]);
    await expect(getActiveAiPlanContinuations()).resolves.toEqual([lease()]);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/ai/continuations?status=active",
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1]?.method ?? "GET").toBe("GET");
  });

  it("reads bounded terminal results without granting or restarting work", async () => {
    const ended = {
      ...lease(),
      session_title: "项目收尾",
      status: "completed",
      reason: "plan_complete",
    };
    const fetchMock = stub([ended]);
    await expect(getRecentAiPlanContinuations()).resolves.toEqual([ended]);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/ai/continuations?status=recent",
    );
    expect(fetchMock.mock.calls[0][1]?.method ?? "GET").toBe("GET");
    stub([lease()]);
    await expect(getRecentAiPlanContinuations()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    stub([{ ...ended, session_title: "" }]);
    await expect(getRecentAiPlanContinuations()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    ["session_id", id.other],
    ["provider.id", id.other],
    ["provider.version", 3],
    ["provider.config_version", 5],
    ["initial_plan_version", 2],
    ["max_turns", 7],
    ["expires_at", "2026-09-21T11:30:00.123456789Z"],
    ["workspace.scopes", ["work", "actions", "outputs"]],
  ])("rejects a create receipt mismatching %s", async (path, value) => {
    const response = lease();
    mutate(response, String(path), value);
    stub(response, 201);
    await expect(
      createAiPlanContinuation(id.session, input()),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("does not adopt another session's GET response", async () => {
    stub({ ...lease(), session_id: id.other });
    await expect(getAiPlanContinuation(id.session)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    {
      name: "wrong identity",
      value: {
        ...lease(),
        id: id.other,
        status: "stopped",
        reason: "user_stopped",
      },
    },
    { name: "still active", value: lease() },
  ])("does not claim stop succeeded for $name", async ({ value }) => {
    stub(value);
    await expect(stopAiPlanContinuation(id.lease)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    {
      name: "duplicate session",
      values: [lease(), { ...lease(), id: id.other }],
    },
    {
      name: "terminal lease",
      values: [{ ...lease(), status: "stopped", reason: "user_stopped" }],
    },
    { name: "over capacity", values: Array.from({ length: 9 }, () => lease()) },
    {
      name: "duplicate lease ID",
      values: [lease(), { ...lease(), session_id: id.other }],
    },
  ])("rejects malformed active list: $name", async ({ values }) => {
    stub(values);
    await expect(getActiveAiPlanContinuations()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    ["max_turns", 0],
    ["max_turns", 9],
    ["max_turns", 1.5],
    ["turns_started", -1],
    ["turns_started", 9],
    ["turns_started", "0"],
    ["version", null],
    ["version", Number.MAX_SAFE_INTEGER + 1],
    ["initial_plan_version", 0],
    ["initial_plan_version", 129],
    ["current_plan_version", 2],
    ["current_plan_version", 129],
    ["id", "not-an-id"],
    ["session_id", id.session.toUpperCase()],
    ["status", "__proto__"],
    ["reason", "future_reason"],
    ["provider", null],
    ["provider.base_url", "https://private.invalid"],
    ["provider.config_version", 0],
    ["provider.kind", "builtin"],
    ["workspace.provider_version", 99],
    ["workspace.scopes", ["work"]],
    ["workspace.scopes", ["work", "actions", "actions"]],
    ["workspace.scopes", ["work", "actions", "workspace_ui"]],
    ["workspace.scopes", ["work", "actions", "agent_files"]],
    ["workspace.knowledge_sources", []],
    ["workspace.body", "private"],
    ["resume_after_restart", "yes"],
    ["last_generation_id", id.generation],
    ["current_generation_id", id.generation],
    ["created_at", "2026-02-30T10:00:00Z"],
    ["updated_at", "2026-09-21T10:00Z"],
    ["expires_at", "2026-09-21T09:00:00Z"],
    ["expires_at", "2026-09-21T13:00:00Z"],
    ["origin", { kind: "manual" }],
  ])("rejects malformed closed lease field %s=%j", (path, value) => {
    const response = lease();
    mutate(response, String(path), value);
    expect(() => parseAiPlanContinuation(response)).toThrow();
  });

  it.each(Object.keys(lease()))("requires the response field %s", (key) => {
    const value = lease() as unknown as Record<string, unknown>;
    delete value[key];
    expect(() => parseAiPlanContinuation(value)).toThrow();
  });

  it("rejects a running lease without an actual owned generation", () => {
    expect(() =>
      parseAiPlanContinuation({
        ...lease(),
        status: "running",
        reason: "generating",
      }),
    ).toThrow();
  });

  it.each([
    ["confirm_automatic_continuation", false],
    ["max_turns", 9],
    ["ttl_minutes", 0],
    ["ttl_minutes", 121],
    ["expected_plan_version", 129],
    ["expected_session_version", 0],
    ["provider_id", "bad"],
    ["confirm_all_actions", true],
  ])("does not send invalid create option %s", async (path, value) => {
    const fetchMock = stub(lease());
    const request = input();
    mutate(request, String(path), value);
    await expect(
      createAiPlanContinuation(id.session, request),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each(["source", "version", "widening"])(
    "does not adopt changed knowledge consent: %s",
    async (kind) => {
      const request = input();
      request.workspace = {
        provider_version: 2,
        scopes: ["work", "actions", "knowledge"],
        knowledge_sources: [
          { source_id: id.source, expected_source_version: 2 },
        ],
      };
      const response = lease();
      response.workspace = structuredClone(request.workspace);
      if (kind === "source")
        response.workspace.knowledge_sources![0].source_id = id.other;
      if (kind === "version")
        response.workspace.knowledge_sources![0].expected_source_version = 3;
      if (kind === "widening")
        response.workspace.knowledge_sources!.push({
          source_id: id.other,
          expected_source_version: 1,
        });
      stub(response, 201);
      await expect(
        createAiPlanContinuation(id.session, request),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    },
  );

  it("accepts the same explicit knowledge consent in canonical source order", async () => {
    const request = input();
    request.workspace = {
      provider_version: 2,
      scopes: ["knowledge", "actions", "work"],
      knowledge_sources: [
        { source_id: id.other, expected_source_version: 1 },
        { source_id: id.source, expected_source_version: 2 },
      ],
    };
    const response = lease();
    response.workspace = structuredClone(request.workspace);
    response.workspace.scopes.reverse();
    response.workspace.knowledge_sources!.reverse();
    stub(response, 201);
    await expect(
      createAiPlanContinuation(id.session, request),
    ).resolves.toEqual(response);
  });

  it("removes dependent file capabilities instead of inheriting them", () => {
    const scopes = toggleContinuationScope(
      ["work", "actions"],
      "agent_project_files",
      true,
    );
    expect(scopes).toEqual(
      expect.arrayContaining([
        "outputs",
        "agent_execution",
        "agent_files",
        "agent_project_files",
      ]),
    );
    expect(toggleContinuationScope(scopes, "outputs", false)).toEqual([
      "work",
      "actions",
    ]);
  });
});

describe("generation readback retains durable continuation origin", () => {
  const origin = {
    kind: "plan_continuation",
    continuation_id: id.lease,
    turn_index: 2,
    max_turns: 8,
  };
  const generation = {
    id: id.generation,
    session_id: id.session,
    provider_id: id.provider,
    status: "completed",
    content: "result",
    persist: true,
  };
  it("returns validated origin without treating completion as business acceptance", async () => {
    stub({ ...generation, origin });
    await expect(getAiGeneration(id.generation)).resolves.toMatchObject({
      status: "completed",
      origin,
    });
  });
  it.each([
    null,
    {},
    { ...origin, turn_index: 0 },
    { ...origin, turn_index: 9 },
    { ...origin, max_turns: 9 },
    { ...origin, approval: true },
  ])(
    "never downgrades malformed present origin to manual: %j",
    async (invalid) => {
      stub({ ...generation, origin: invalid });
      await expect(getAiGeneration(id.generation)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );
});
