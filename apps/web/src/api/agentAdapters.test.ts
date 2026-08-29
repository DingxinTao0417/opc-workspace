import { afterEach, describe, expect, it, vi } from "vitest";
import {
  checkAgentAdapter,
  getAgentAdapters,
  normalizeAgentAdapter,
  registerAgentAdapter,
  resetRuntimeConnection,
} from "./client";

function response(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function adapterPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-5000-8000-000000003401",
    adapter_key: "builtin-local-text-v1",
    kind: "builtin",
    display_name: "本地文本诊断执行器",
    protocol_version: "opc-agent-pipe-v1",
    manifest: {
      execution_mode: "short_lived_process",
      capabilities: [
        "read_task_snapshot",
        "write_text_artifact",
        "write_structured_artifact",
      ],
      requirements: [
        "process_isolation",
        "network_block",
        "process_tree_cleanup",
      ],
    },
    status: "disabled",
    health_status: "unknown",
    health_error_code: null,
    isolation_status: "unverified",
    execution_ready: false,
    last_health_at: null,
    readiness: {
      can_enable: false,
      unavailable_code: "PLATFORM_ISOLATION_UNVERIFIED",
      required_gates: [
        "process_isolation",
        "network_block",
        "process_tree_cleanup",
      ],
    },
    version: 1,
    created_at: "2026-08-29T12:00:00Z",
    updated_at: "2026-08-29T12:00:00Z",
    ...overrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("Agent Adapter API contract", () => {
  it("normalizes the list and sends controlled registration/check commands", async () => {
    const payload = adapterPayload();
    const fetchMock = vi.fn().mockResolvedValue(response({ data: [payload] }));
    vi.stubGlobal("fetch", fetchMock);

    const adapters = await getAgentAdapters();
    expect(adapters[0]).toMatchObject({
      adapterKey: "builtin-local-text-v1",
      executionReady: false,
      manifest: { executionMode: "short_lived_process" },
    });

    fetchMock.mockResolvedValueOnce(response({ data: payload }));
    await registerAgentAdapter("builtin-local-text-v1", "adapter-register-1");
    const registration = fetchMock.mock.calls[1];
    expect(JSON.parse(String(registration[1]?.body))).toEqual({
      preset_key: "builtin-local-text-v1",
    });
    expect(new Headers(registration[1]?.headers).get("Idempotency-Key")).toBe(
      "adapter-register-1",
    );

    fetchMock.mockResolvedValueOnce(response({ data: payload }));
    await checkAgentAdapter(String(payload.id), 1);
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"1"');
    expect(String(fetchMock.mock.calls[2][0])).toContain("/check");
  });

  it("rejects inconsistent readiness and any executable field", () => {
    expect(() =>
      normalizeAgentAdapter(
        adapterPayload({
          execution_ready: true,
          readiness: {
            can_enable: false,
            unavailable_code: "PLATFORM_ISOLATION_UNVERIFIED",
            required_gates: [
              "process_isolation",
              "network_block",
              "process_tree_cleanup",
            ],
          },
        }),
      ),
    ).toThrow(/就绪状态/);
    expect(() =>
      normalizeAgentAdapter(
        adapterPayload({ executable_ref: "builtin:local-text-v1" }),
      ),
    ).toThrow(/格式无效/);
  });
});
