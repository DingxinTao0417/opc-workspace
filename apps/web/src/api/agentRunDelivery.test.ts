import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getAgentRun,
  getAgentRuns,
  getTaskAgentRuns,
  retryAgentRunOutputDelivery,
} from "./client";

const id = {
  run: "018f0000-0000-7000-8000-000000000501",
  task: "018f0000-0000-7000-8000-000000000502",
  assignment: "018f0000-0000-7000-8000-000000000503",
  actor: "018f0000-0000-7000-8000-000000000504",
  adapter: "018f0000-0000-7000-8000-000000000505",
  owner: "018f0000-0000-7000-8000-000000000506",
  provider: "018f0000-0000-7000-8000-000000000507",
  submission: "018f0000-0000-7000-8000-000000000508",
  artifact: "018f0000-0000-7000-8000-000000000509",
  otherTask: "018f0000-0000-7000-8000-000000000510",
};

const pending = {
  id: id.run,
  task_id: id.task,
  assignment_id: id.assignment,
  actor_id: id.actor,
  adapter_id: id.adapter,
  created_by_actor_id: id.owner,
  parent_run_id: null,
  attempt: 1,
  status: "running",
  provider_id: id.provider,
  model: "test-model",
  result_text: null,
  result_bytes: null,
  error_code: null,
  output_delivery_status: "pending",
  output_delivery_error_code: "AGENT_OUTPUT_DELIVERY_PENDING",
  submission_id: null,
  artifact_id: null,
  started_at: "2026-09-18T10:00:01Z",
  completed_at: null,
  created_at: "2026-09-18T10:00:00Z",
};

function response(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function listResponse(data: unknown[]) {
  return new Response(
    JSON.stringify({
      data,
      meta: {
        page: 1,
        page_size: 20,
        total: data.length,
        active_total: 7,
        pending_delivery_total: 3,
        succeeded_total: 11,
      },
    }),
    {
      status: 200,
      headers: { "Content-Type": "application/json" },
    },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Agent Run output delivery parsing", () => {
  it("parses a lightweight global summary without materializing result text", async () => {
    const { result_text: _resultText, ...metadata } = pending;
    const summary = {
      ...metadata,
      task_title: "任务甲",
      status: "succeeded",
      result_bytes: 16,
      output_delivery_status: "submitted",
      output_delivery_error_code: null,
      submission_id: id.submission,
      artifact_id: id.artifact,
      completed_at: "2026-09-18T10:02:00Z",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => listResponse([summary])),
    );

    const result = await getAgentRuns({
      page: 2,
      pageSize: 7,
      outputDeliveryStatus: "pending",
      attentionOnly: true,
      unplannedOnly: true,
    });

    expect(result.items[0]).toMatchObject({
      id: id.run,
      taskTitle: "任务甲",
      status: "succeeded",
      resultBytes: 16,
      outputDeliveryStatus: "submitted",
      submissionId: id.submission,
      artifactId: id.artifact,
    });
    expect(result.items[0]).not.toHaveProperty("resultText");
    expect(result.meta).toMatchObject({
      activeTotal: 7,
      pendingDeliveryTotal: 3,
      succeededTotal: 11,
    });
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining(
        "page=2&page_size=7&output_delivery_status=pending&attention=1&plan_link=unplanned",
      ),
      expect.any(Object),
    );
  });

  it("rejects a global summary that leaks the result body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        listResponse([
          { ...pending, task_title: "任务甲", result_text: "leaked output" },
        ]),
      ),
    );

    await expect(getAgentRuns()).rejects.toThrow(
      "Agent 执行摘要不能包含产出正文",
    );
  });

  it("rejects exact records with a missing required execution identity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response({ ...pending, adapter_id: "" })),
    );

    await expect(getAgentRun(id.run)).rejects.toThrow(
      "Agent 执行身份响应格式无效",
    );
  });

  it("rejects a task-scoped list containing a Run from another task", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response([{ ...pending, task_id: id.otherTask }])),
    );

    await expect(getTaskAgentRuns(id.task)).rejects.toThrow(
      "Agent 执行列表包含其他任务的记录",
    );
  });

  it("parses the explicit pending delivery state without treating it as submitted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(pending)),
    );
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      id: id.run,
      status: "running",
      outputDeliveryStatus: "pending",
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_PENDING",
      resultText: null,
      resultBytes: null,
      submissionId: null,
      artifactId: null,
    });
  });

  it.each([
    [
      "succeeded without a delivery result",
      { status: "succeeded", output_delivery_status: "not_ready" },
    ],
    [
      "pending while queued",
      { status: "queued", output_delivery_status: "pending" },
    ],
    ["pending without a reason code", { output_delivery_error_code: null }],
    [
      "pending with an unexpected reason code",
      { output_delivery_error_code: "AGENT_OUTPUT_DELIVERY_FAILED" },
    ],
    [
      "submitted without both identities",
      {
        status: "succeeded",
        output_delivery_status: "submitted",
        output_delivery_error_code: null,
        submission_id: id.submission,
        artifact_id: null,
      },
    ],
    [
      "retained without a reason code",
      {
        status: "succeeded",
        output_delivery_status: "retained",
        output_delivery_error_code: null,
      },
    ],
  ])("rejects %s", async (_name, overrides) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response({ ...pending, ...overrides })),
    );
    await expect(getAgentRun(id.run)).rejects.toThrow(
      "Agent 执行与产出登记状态不一致",
    );
  });

  it.each([
    [
      "queued output before success",
      {
        status: "queued",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        result_text: "leaked output",
        result_bytes: 13,
        started_at: null,
      },
    ],
    [
      "running output before success",
      { result_text: "leaked output", result_bytes: 13 },
    ],
    ["running without a start time", { started_at: null }],
    [
      "failed without an error code",
      {
        status: "failed",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        error_code: null,
        completed_at: "2026-09-18T10:02:00Z",
      },
    ],
    [
      "cancelled with an error code",
      {
        status: "cancelled",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        error_code: "CANCELLED",
        completed_at: "2026-09-18T10:02:00Z",
      },
    ],
    [
      "interrupted without a completion time",
      {
        status: "interrupted",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
      },
    ],
    [
      "succeeded with an empty result",
      {
        status: "succeeded",
        completed_at: "2026-09-18T10:02:00Z",
        result_text: "",
        result_bytes: 0,
        output_delivery_status: "retained",
        output_delivery_error_code: "TASK_SUBMISSION_NOT_ALLOWED",
      },
    ],
    [
      "succeeded with a UTF-8 result larger than 64 KiB",
      {
        status: "succeeded",
        completed_at: "2026-09-18T10:02:00Z",
        result_text: "好".repeat(21_846),
        result_bytes: 65_538,
        output_delivery_status: "retained",
        output_delivery_error_code: "TASK_SUBMISSION_NOT_ALLOWED",
      },
    ],
  ])("rejects lifecycle mismatch: %s", async (_name, overrides) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response({ ...pending, ...overrides })),
    );
    await expect(getAgentRun(id.run)).rejects.toThrow(
      "Agent 执行生命周期状态不一致",
    );
  });

  it("posts an explicit retry and parses the exact submitted Run", async () => {
    const submitted = {
      ...pending,
      status: "succeeded",
      completed_at: "2026-09-18T10:02:00Z",
      result_text: "submitted output",
      result_bytes: 16,
      output_delivery_status: "submitted",
      output_delivery_error_code: null,
      submission_id: id.submission,
      artifact_id: id.artifact,
    };
    const fetchMock = vi.fn(async () => response(submitted));
    vi.stubGlobal("fetch", fetchMock);

    await expect(retryAgentRunOutputDelivery(id.run)).resolves.toMatchObject({
      id: id.run,
      outputDeliveryStatus: "submitted",
      submissionId: id.submission,
      artifactId: id.artifact,
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(
        `/api/v1/agent-runs/${id.run}/output-delivery/retry`,
      ),
      expect.objectContaining({ method: "POST" }),
    );
  });
});

describe("Agent Run start gates", () => {
  const gate = {
    predecessor_run_id: "018f0000-0000-7000-8000-000000000601",
    require: "accepted",
    expires_at: "2026-09-18T12:00:00Z",
    status: "waiting",
  };
  const queued = {
    ...pending,
    status: "queued",
    output_delivery_status: "not_ready",
    output_delivery_error_code: null,
  };

  it("parses a waiting gate on a queued Run", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response({ ...queued, start_gate: gate })),
    );
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      status: "queued",
      startGate: {
        predecessorRunId: gate.predecessor_run_id,
        require: "accepted",
        status: "waiting",
      },
    });
  });

  it.each([
    ["a waiting gate on a running Run", { ...pending, start_gate: gate }],
    [
      "a predecessor closure on a queued Run",
      {
        ...queued,
        start_gate: {
          ...gate,
          status: "closed",
          close_reason: "predecessor_failed",
        },
      },
    ],
    [
      "an unknown closure reason",
      {
        ...queued,
        status: "cancelled",
        completed_at: "2026-09-18T10:02:00Z",
        start_gate: { ...gate, status: "closed", close_reason: "shrug" },
      },
    ],
    [
      "an extra gate field",
      { ...queued, start_gate: { ...gate, task_title: "leak" } },
    ],
  ])("rejects %s", async (_name, body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(body)),
    );
    await expect(getAgentRun(id.run)).rejects.toThrow(/启动条件/);
  });
});

describe("Agent Runs created before frozen execution identity", () => {
  const legacyIdentity = {
    task_version: 0,
    assignment_assigned_at: "",
    actor_version: 0,
    adapter_version: 0,
    provider_version: 0,
    provider_config_version: 0,
    execution_contract_version: 0,
  };
  const legacyRetained = {
    ...pending,
    ...legacyIdentity,
    status: "succeeded",
    completed_at: "2026-09-18T10:02:00Z",
    result_text: "旧版执行的完整产出",
    result_bytes: new TextEncoder().encode("旧版执行的完整产出").byteLength,
    output_delivery_status: "retained",
    output_delivery_error_code: "AGENT_OUTPUT_DELIVERY_LEGACY",
  };
  const legacyFailed = {
    ...pending,
    ...legacyIdentity,
    id: "018f0000-0000-7000-8000-000000000511",
    attempt: 2,
    status: "failed",
    completed_at: "2026-09-18T10:03:00Z",
    error_code: "AGENT_EXECUTION_FAILED",
    output_delivery_status: "not_ready",
    output_delivery_error_code: null,
  };

  it("keeps legacy task history readable in the task Run list", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response([legacyRetained, legacyFailed])),
    );

    const runs = await getTaskAgentRuns(id.task);

    expect(runs).toHaveLength(2);
    expect(runs[0]).toMatchObject({
      id: id.run,
      status: "succeeded",
      outputDeliveryStatus: "retained",
      resultText: "旧版执行的完整产出",
      executionContractVersion: 0,
    });
    expect(runs[1]).toMatchObject({
      status: "failed",
      errorCode: "AGENT_EXECUTION_FAILED",
      executionContractVersion: 0,
    });
  });

  it("reads the exact legacy Run detail without private execution fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(legacyRetained)),
    );

    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      id: id.run,
      executionContractVersion: 0,
      outputDeliveryErrorCode: "AGENT_OUTPUT_DELIVERY_LEGACY",
    });
  });

  it.each([-1, 7, 1.5])(
    "still rejects an out-of-range contract version %s",
    async (version) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          response({ ...legacyRetained, execution_contract_version: version }),
        ),
      );
      await expect(getAgentRun(id.run)).rejects.toThrow(
        "Agent 执行契约版本无效",
      );
    },
  );

  it("does not let a legacy detail carry rework context", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          ...legacyRetained,
          rework_context: { submission_id: id.submission },
        }),
      ),
    );
    await expect(getAgentRun(id.run)).rejects.toThrow(
      "旧执行契约不能携带返工上下文",
    );
  });
});
