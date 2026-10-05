import { afterEach, describe, expect, it, vi } from "vitest";
import {
  agentRunStartGateFromRecord,
  confirmAgentRunStartGateInputs,
  getAgentRun,
  getAgentRunStartGateInputs,
  getTaskAgentRuns,
  resetRuntimeConnection,
} from "./client";

const id = {
  task: "018f0000-0000-7000-8000-000000007201",
  run: "018f0000-0000-7000-8000-000000007202",
  provider: "018f0000-0000-7000-8000-000000007203",
  actor: "018f0000-0000-7000-8000-000000007204",
  assignment: "018f0000-0000-7000-8000-000000007205",
  adapter: "018f0000-0000-7000-8000-000000007206",
  predecessor: "018f0000-0000-7000-8000-000000007207",
  sourceTask: "018f0000-0000-7000-8000-000000007208",
  project: "018f0000-0000-7000-8000-000000007209",
  submission: "018f0000-0000-7000-8000-000000007210",
  research: "018f0000-0000-7000-8000-000000007211",
  notes: "018f0000-0000-7000-8000-000000007212",
};
const expectedFiles = [
  { name: "research.md", mime: "text/markdown" },
  { name: "notes.md", mime: "text/markdown" },
];
const sourceTask = {
  project_id: id.project,
  task_id: id.sourceTask,
  task_title: "完成调研",
  task_version: 4,
  submission_id: id.submission,
  submission_sequence: 1,
};
const files = [
  {
    id: id.research,
    source_kind: "project_task_artifact",
    name: "research.md",
    mime: "text/markdown",
    size_bytes: 120,
    sha256: "a".repeat(64),
    source_task: sourceTask,
  },
  {
    id: id.notes,
    source_kind: "project_task_artifact",
    name: "notes.md",
    mime: "text/markdown",
    size_bytes: 80,
    sha256: "b".repeat(64),
    source_task: sourceTask,
  },
];
const gate = (status: string, slot: string, extra: object = {}) => ({
  predecessor_run_id: id.predecessor,
  require: "accepted",
  expires_at: "2026-10-04T14:00:00Z",
  status,
  ...extra,
  input_slot: {
    source: "predecessor_files",
    status: slot,
    expected_files: expectedFiles,
  },
});
const run = (extra: object = {}) => ({
  id: id.run,
  task_id: id.task,
  assignment_id: id.assignment,
  actor_id: id.actor,
  adapter_id: id.adapter,
  created_by_actor_id: id.actor,
  parent_run_id: null,
  attempt: 1,
  status: "queued",
  provider_id: id.provider,
  model: "selected-model",
  execution_contract_version: 7,
  output_contract: { type: "text" },
  result_text: null,
  result_bytes: null,
  error_code: null,
  output_delivery_status: "not_ready",
  output_delivery_error_code: null,
  submission_id: null,
  artifact_id: null,
  started_at: null,
  completed_at: null,
  created_at: "2026-10-04T12:00:00Z",
  start_gate: gate("waiting", "awaiting_predecessor"),
  ...extra,
});
const detail = (extra: object = {}) =>
  run({
    model_protocol: "openai_chat",
    max_output_tokens: 0,
    execution_provider_confirmation: {
      version: 3,
      config_version: 7,
      kind: "remote",
    },
    ...extra,
  });
const inputs = (extra: object = {}) => ({
  run_id: id.run,
  task_id: id.task,
  status: "awaiting_confirmation",
  expires_at: "2026-10-04T14:00:00Z",
  predecessor: {
    run_id: id.predecessor,
    task_id: id.sourceTask,
    task_title: "完成调研",
    attempt: 1,
  },
  expected_files: expectedFiles,
  provider: {
    id: id.provider,
    name: "远程模型",
    kind: "remote",
    model: "selected-model",
    version: 3,
    config_version: 7,
    leaves_device: true,
  },
  files,
  total_bytes: 200,
  limits: { max_files: 4, max_file_bytes: 65_536, max_total_bytes: 131_072 },
  fingerprint: "c".repeat(64),
  ...extra,
});

function stub(...payloads: unknown[]) {
  const mock = vi.fn<typeof fetch>();
  for (const payload of payloads) {
    mock.mockResolvedValueOnce(
      new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
  }
  vi.stubGlobal("fetch", mock);
  return mock;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("the input slot of a start gate", () => {
  it.each([
    ["waiting", "awaiting_predecessor", "queued", {}],
    ["waiting", "awaiting_confirmation", "queued", {}],
    ["released", "bound", "running", {}],
    [
      "closed",
      "closed",
      "cancelled",
      { close_reason: "predecessor_output_unavailable" },
    ],
  ])("follows a %s gate as %s", (status, slot, runStatus, extra) => {
    expect(
      agentRunStartGateFromRecord(
        gate(status, slot, extra),
        runStatus,
        "not_ready",
      )?.inputSlot,
    ).toEqual({
      source: "predecessor_files",
      status: slot,
      expectedFiles,
    });
  });

  it.each([
    [
      "a waiting gate that is already bound",
      gate("waiting", "bound"),
      "queued",
    ],
    [
      "a released gate that still waits",
      gate("released", "awaiting_confirmation"),
      "running",
    ],
    [
      "a closed gate with an open slot",
      gate("closed", "bound", { close_reason: "expired" }),
      "cancelled",
    ],
    [
      "a slot on a submitted condition",
      { ...gate("waiting", "awaiting_predecessor"), require: "submitted" },
      "queued",
    ],
    ["an unknown slot status", gate("waiting", "ready"), "queued"],
    [
      "an unknown source",
      {
        ...gate("waiting", "awaiting_predecessor"),
        input_slot: {
          source: "predecessor_text",
          status: "awaiting_predecessor",
          expected_files: expectedFiles,
        },
      },
      "queued",
    ],
    [
      "no announced file",
      {
        ...gate("waiting", "awaiting_predecessor"),
        input_slot: {
          source: "predecessor_files",
          status: "awaiting_predecessor",
          expected_files: [],
        },
      },
      "queued",
    ],
  ])("rejects %s", (_name, raw, runStatus) => {
    expect(() =>
      agentRunStartGateFromRecord(raw, runStatus, "not_ready"),
    ).toThrow();
  });

  it("reads the gate of a just-cancelled Run as the closure about to be recorded", () => {
    const { input_slot: _slot, ...plain } = gate(
      "waiting",
      "awaiting_predecessor",
    );
    expect(
      agentRunStartGateFromRecord(plain, "cancelled", "not_ready"),
    ).toEqual({
      predecessorRunId: id.predecessor,
      require: "accepted",
      expiresAt: "2026-10-04T14:00:00Z",
      status: "closed",
      closeReason: "run_not_queued",
    });
    expect(
      agentRunStartGateFromRecord(
        gate("waiting", "awaiting_confirmation"),
        "cancelled",
        "not_ready",
      ),
    ).toMatchObject({
      status: "closed",
      closeReason: "run_not_queued",
      inputSlot: { status: "closed", expectedFiles },
    });
  });

  it.each([
    ["running", "not_ready"],
    ["running", "pending"],
    ["succeeded", "submitted"],
    ["failed", "not_ready"],
    ["interrupted", "not_ready"],
  ])("still rejects a waiting gate on a %s + %s Run", (status, delivery) => {
    expect(() =>
      agentRunStartGateFromRecord(
        gate("waiting", "awaiting_predecessor"),
        status,
        delivery,
      ),
    ).toThrow();
  });

  it("rejects missing files as a reason on a gate that never had a slot", () => {
    const { input_slot: _slot, ...plain } = gate("closed", "closed", {
      close_reason: "predecessor_output_unavailable",
    });
    expect(() =>
      agentRunStartGateFromRecord(plain, "cancelled", "not_ready"),
    ).toThrow();
  });
});

describe("contract v7 Run reads", () => {
  it("lists a waiting v7 Run with its slot and no private detail", async () => {
    stub({ data: [run()] });
    const [listed] = await getTaskAgentRuns(id.task);
    expect(listed).toMatchObject({
      executionContractVersion: 7,
      startGate: { inputSlot: { status: "awaiting_predecessor" } },
    });
  });

  it("reads a waiting v7 Run without files and a bound one with them", async () => {
    stub(
      { data: detail() },
      {
        data: detail({
          status: "running",
          started_at: "2026-10-04T13:00:00Z",
          start_gate: gate("released", "bound"),
          input_files: files,
        }),
      },
    );
    const waiting = await getAgentRun(id.run);
    expect(waiting.executionInputFiles).toEqual([]);
    expect(waiting.modelProtocol).toBe("openai_chat");
    const bound = await getAgentRun(id.run);
    expect(bound.executionInputFiles).toEqual(files);
    expect(bound.startGate?.inputSlot?.status).toBe("bound");
  });

  it("reads a just-cancelled v7 Run whose gate is not closed yet", async () => {
    stub({
      data: detail({
        status: "cancelled",
        // A cancelled Run carries both times, as the server writes them.
        started_at: "2026-10-04T12:30:00Z",
        completed_at: "2026-10-04T12:30:00Z",
        start_gate: gate("waiting", "awaiting_confirmation"),
      }),
    });
    const run = await getAgentRun(id.run);
    expect(run.status).toBe("cancelled");
    expect(run.startGate).toMatchObject({
      status: "closed",
      closeReason: "run_not_queued",
      inputSlot: { status: "closed" },
    });
    expect(run.executionInputFiles).toEqual([]);
  });

  it.each([
    ["v7 without a slot", detail({ start_gate: undefined })],
    [
      "a slot on an older contract",
      detail({ execution_contract_version: 6, input_files: files }),
    ],
    ["files before anyone confirmed them", detail({ input_files: files })],
    [
      "a bound Run without its files",
      detail({
        status: "running",
        started_at: "2026-10-04T13:00:00Z",
        start_gate: gate("released", "bound"),
      }),
    ],
    [
      "a file that is not an accepted Task file",
      detail({
        status: "running",
        started_at: "2026-10-04T13:00:00Z",
        start_gate: gate("released", "bound"),
        input_files: [
          {
            id: id.research,
            source_kind: "task_artifact",
            name: "research.md",
            mime: "text/markdown",
            size_bytes: 120,
            sha256: "a".repeat(64),
          },
        ],
      }),
    ],
    [
      "a rework context",
      detail({
        rework_context: {
          submission_id: id.submission,
          sequence: 1,
          review_reason: "改",
          reviewed_at: "2026-10-04T12:30:00Z",
          artifacts: [],
        },
      }),
    ],
  ])("rejects %s", async (_name, value) => {
    stub({ data: JSON.parse(JSON.stringify(value)) });
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
});

describe("reading and confirming the predecessor's files", () => {
  it("reads exactly the files and the Provider to confirm", async () => {
    const fetchMock = stub({ data: inputs() });
    const result = await getAgentRunStartGateInputs(id.run);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/agent-runs/${id.run}/start-gate/inputs`,
    );
    expect(result).toMatchObject({
      status: "awaiting_confirmation",
      fingerprint: "c".repeat(64),
      totalBytes: 200,
      predecessor: { taskTitle: "完成调研", attempt: 1 },
      provider: { leavesDevice: true, version: 3, configVersion: 7 },
    });
    expect(result.files).toEqual(files);
  });

  it("reads a slot that still waits and one that is closed", async () => {
    stub(
      {
        data: inputs({
          status: "awaiting_predecessor",
          files: [],
          total_bytes: 0,
          fingerprint: undefined,
        }),
      },
      {
        data: inputs({
          status: "closed",
          close_reason: "expired",
          files: [],
          total_bytes: 0,
          fingerprint: undefined,
          predecessor: { run_id: id.predecessor },
        }),
      },
    );
    await expect(getAgentRunStartGateInputs(id.run)).resolves.toMatchObject({
      status: "awaiting_predecessor",
      files: [],
    });
    const closed = await getAgentRunStartGateInputs(id.run);
    expect(closed).toMatchObject({ status: "closed", closeReason: "expired" });
    expect(closed.fingerprint).toBeUndefined();
    expect(closed.predecessor).toEqual({ runId: id.predecessor });
  });

  it.each([
    ["another Run", inputs({ run_id: id.predecessor })],
    ["files without a fingerprint", inputs({ fingerprint: undefined })],
    [
      "a fingerprint with nothing to confirm",
      inputs({ status: "awaiting_predecessor", files: [], total_bytes: 0 }),
    ],
    [
      "fewer files than announced",
      inputs({ files: [files[0]], total_bytes: 120 }),
    ],
    [
      "a file under another name",
      inputs({ files: [{ ...files[0], name: "other.md" }, files[1]] }),
    ],
    ["a wrong total", inputs({ total_bytes: 201 })],
    [
      "a file of the Run's own Task",
      inputs({
        files: [
          { ...files[0], source_task: { ...sourceTask, task_id: id.task } },
          files[1],
        ],
      }),
    ],
    [
      "a path",
      inputs({
        files: [{ ...files[0], relative_path: "objects/x" }, files[1]],
      }),
    ],
    [
      "a remote Provider said to stay on the device",
      inputs({ provider: { ...inputs().provider, leaves_device: false } }),
    ],
    [
      "a closed slot without a reason",
      inputs({
        status: "closed",
        files: [],
        total_bytes: 0,
        fingerprint: undefined,
      }),
    ],
    [
      "larger limits",
      inputs({
        limits: {
          max_files: 8,
          max_file_bytes: 65_536,
          max_total_bytes: 131_072,
        },
      }),
    ],
  ])("rejects %s", async (_name, value) => {
    stub({ data: JSON.parse(JSON.stringify(value)) });
    await expect(getAgentRunStartGateInputs(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it("confirms both consents, the Provider shown and the fingerprint", async () => {
    const fetchMock = stub({ data: inputs({ status: "bound" }) });
    const result = await confirmAgentRunStartGateInputs({
      runId: id.run,
      fingerprint: "c".repeat(64),
      provider: { version: 3, configVersion: 7, kind: "remote" },
    });
    expect(result.status).toBe("bound");
    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain(
      `/api/v1/agent-runs/${id.run}/start-gate/inputs/confirm`,
    );
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({
      confirm_file_access: true,
      confirm_project_task_files: true,
      file_access_provider_confirmation: {
        version: 3,
        config_version: 7,
        kind: "remote",
      },
      fingerprint: "c".repeat(64),
    });
  });

  it("does not report success for anything but the files it confirmed", async () => {
    stub(
      { data: inputs() },
      { data: inputs({ status: "bound", fingerprint: "d".repeat(64) }) },
    );
    const input = {
      runId: id.run,
      fingerprint: "c".repeat(64),
      provider: { version: 3, configVersion: 7, kind: "remote" as const },
    };
    await expect(confirmAgentRunStartGateInputs(input)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    await expect(confirmAgentRunStartGateInputs(input)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });
});
