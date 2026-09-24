import { afterEach, describe, expect, it, vi } from "vitest";
import { getAiSessionDelegatedRuns } from "./aiSessionAgentRuns";

const id = (suffix: number) =>
  `018f0000-0000-7000-8000-${String(suffix).padStart(12, "0")}`;

const summary = {
  id: id(1),
  task_id: id(2),
  task_title: "任务甲",
  assignment_id: id(3),
  actor_id: id(4),
  adapter_id: id(5),
  created_by_actor_id: id(6),
  parent_run_id: null,
  attempt: 1,
  status: "running",
  provider_id: id(7),
  model: "test-model",
  result_bytes: null,
  error_code: null,
  output_delivery_status: "not_ready",
  output_delivery_error_code: null,
  submission_id: null,
  artifact_id: null,
  started_at: "2026-09-22T10:00:01Z",
  completed_at: null,
  created_at: "2026-09-22T10:00:00Z",
};

function response(data: unknown[]) {
  return new Response(
    JSON.stringify({
      data,
      meta: {
        page: 1,
        page_size: 20,
        total: data.length,
        active_total: 1,
        pending_delivery_total: 0,
        unavailable_total: data.filter(
          (item) => (item as { run?: unknown }).run === null,
        ).length,
        as_of: "2026-09-22T10:02:00Z",
      },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AI session delegated Agent Runs", () => {
  it("parses a scoped receipt and exact latest-plan association", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response([
          {
            run_id: id(1),
            run: summary,
            delegation: {
              session_id: id(8),
              generation_id: id(9),
              proposal_id: id(10),
              action: "agent_run.start",
              created_at: "2026-09-22T10:00:00Z",
              decided_at: "2026-09-22T10:01:00Z",
              plan: {
                version: 3,
                step_id: "delegate",
                step_title: "生成交付文件",
              },
            },
          },
        ]),
      ),
    );

    const result = await getAiSessionDelegatedRuns(id(8), {
      page: 1,
      pageSize: 20,
      outputDeliveryStatus: "pending",
    });

    expect(result.items[0]).toMatchObject({
      runId: id(1),
      run: { id: id(1), taskTitle: "任务甲" },
      delegation: {
        sessionId: id(8),
        generationId: id(9),
        proposalId: id(10),
        action: "agent_run.start",
        plan: { version: 3, stepId: "delegate", stepTitle: "生成交付文件" },
      },
    });
    expect(result.items[0].run).not.toHaveProperty("resultText");
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining(
        `/api/v1/ai/sessions/${id(8)}/delegated-runs?page=1&page_size=20&output_delivery_status=pending`,
      ),
      expect.any(Object),
    );
  });

  it("preserves an unavailable historical receipt without inventing a Run", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response([
          {
            run_id: id(1),
            run: null,
            delegation: {
              session_id: id(8),
              generation_id: id(9),
              proposal_id: id(10),
              action: "agent_run.retry",
              created_at: "2026-09-22T10:00:00Z",
              decided_at: "2026-09-22T10:01:00Z",
              plan: null,
            },
          },
        ]),
      ),
    );

    await expect(getAiSessionDelegatedRuns(id(8))).resolves.toMatchObject({
      items: [{ runId: id(1), run: null }],
      meta: { unavailableTotal: 1 },
    });
  });

  it.each([
    ["mismatched Run", { run_id: id(11), run: summary }],
    ["private proposal body", { run_id: id(1), run: summary, action_json: {} }],
    [
      "private result body",
      { run_id: id(1), run: { ...summary, result_text: "secret" } },
    ],
  ])("rejects %s in a delegated summary", async (_label, change) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response([
          {
            ...change,
            delegation: {
              session_id: id(8),
              generation_id: id(9),
              proposal_id: id(10),
              action: "agent_run.start",
              created_at: "2026-09-22T10:00:00Z",
              decided_at: "2026-09-22T10:01:00Z",
              plan: null,
            },
          },
        ]),
      ),
    );

    await expect(getAiSessionDelegatedRuns(id(8))).rejects.toThrow();
  });

  it("rejects a receipt projected from another conversation", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response([
          {
            run_id: id(1),
            run: summary,
            delegation: {
              session_id: id(12),
              generation_id: id(9),
              proposal_id: id(10),
              action: "agent_run.start",
              created_at: "2026-09-22T10:00:00Z",
              decided_at: "2026-09-22T10:01:00Z",
              plan: null,
            },
          },
        ]),
      ),
    );

    await expect(getAiSessionDelegatedRuns(id(8))).rejects.toThrow(
      "会话委派身份格式无效",
    );
  });
});
