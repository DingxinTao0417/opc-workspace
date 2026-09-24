import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAgentRun,
  getAgentRun,
  getAgentRuns,
  getTaskAgentRuns,
  resetRuntimeConnection,
  retryAgentRun,
} from "./client";
import type { RetryAgentRunReworkOptions } from "../types/models";

const id = {
  task: "018f0000-0000-7000-8000-00000000e001",
  run: "018f0000-0000-7000-8000-00000000e002",
  actor: "018f0000-0000-7000-8000-00000000e003",
  assignment: "018f0000-0000-7000-8000-00000000e004",
  adapter: "018f0000-0000-7000-8000-00000000e005",
  provider: "018f0000-0000-7000-8000-00000000e006",
  submission: "018f0000-0000-7000-8000-00000000e007",
  artifact: "018f0000-0000-7000-8000-00000000e008",
  file: "018f0000-0000-7000-8000-00000000e009",
};
const confirmation = { version: 3, configVersion: 5, kind: "remote" as const };
const wireConfirmation = { version: 3, config_version: 5, kind: "remote" };
const rework = {
  submission_id: id.submission,
  sequence: 1,
  review_reason: "完整返工意见",
  reviewed_at: "2026-09-21T11:00:00.123456789Z",
  artifacts: [
    {
      id: id.artifact,
      storage_kind: "text",
      name: "previous.md",
      content: "完整旧产出",
      sha256: "a".repeat(64),
    },
  ],
};
const file = {
  source_kind: "task_artifact",
  id: id.file,
  name: "reference.md",
  mime: "text/markdown",
  size_bytes: 512,
  sha256: "b".repeat(64),
};
const run = {
  id: id.run,
  task_id: id.task,
  assignment_id: id.assignment,
  actor_id: id.actor,
  adapter_id: id.adapter,
  created_by_actor_id: id.actor,
  parent_run_id: null,
  attempt: 2,
  status: "queued",
  provider_id: id.provider,
  model: "claude-test",
  execution_contract_version: 5,
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
  created_at: "2026-09-21T12:00:00Z",
};
const meta = {
  page: 1,
  page_size: 20,
  total: 1,
  active_total: 1,
  pending_delivery_total: 0,
  succeeded_total: 0,
};
function detail(): Record<string, unknown> {
  return {
    ...structuredClone(run),
    model_protocol: "anthropic_messages",
    max_output_tokens: 8192,
    execution_provider_confirmation: { ...wireConfirmation },
  };
}
function respond(data: unknown, listMeta?: unknown) {
  return new Response(
    JSON.stringify({ data, ...(listMeta ? { meta: listMeta } : {}) }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}
function stub(data: unknown, listMeta?: unknown) {
  const fetchMock = vi.fn<typeof fetch>(async () => respond(data, listMeta));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("Anthropic v5 exact detail contract", () => {
  it("loads a plain v5 detail with exact frozen protocol, bounded tokens and Provider identity", async () => {
    const fetchMock = stub(detail());
    const result = await getAgentRun(id.run);
    expect(result).toMatchObject({
      id: id.run,
      executionContractVersion: 5,
      outputContract: { type: "text" },
      modelProtocol: "anthropic_messages",
      maxOutputTokens: 8192,
      executionProviderConfirmation: confirmation,
      executionInputFiles: [],
    });
    expect(result).not.toHaveProperty("reworkContext");
    expect(result).not.toHaveProperty("reworkProviderConfirmation");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/agent-runs/${id.run}`,
    );
  });

  it("keeps actual files and optional rework separate from protocol confirmation", async () => {
    stub({ ...detail(), input_files: [file], rework_context: rework });
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      executionProviderConfirmation: confirmation,
      executionInputFiles: [file],
      reworkContext: rework,
    });
  });

  it.each([
    ["inputFiles", [file]],
    ["reworkContext", rework],
    ["reworkProviderConfirmation", wireConfirmation],
  ])(
    "rejects v5 private alias %s instead of dropping it and skipping separate consent",
    async (field, value) => {
      stub({ ...detail(), [field as string]: value });
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it.each([
    "model_protocol",
    "max_output_tokens",
    "output_contract",
    "execution_provider_confirmation",
  ])("requires nonnull v5 detail field %s", async (field) => {
    for (const mode of ["missing", "null"]) {
      const value = detail();
      if (mode === "missing") delete value[field];
      else value[field] = null;
      stub(value);
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    }
  });

  it.each([
    ["model_protocol", ""],
    ["model_protocol", "openai_chat"],
    ["model_protocol", " anthropic_messages"],
    ["model_protocol", 5],
    ["max_output_tokens", 0],
    ["max_output_tokens", 8193],
    ["max_output_tokens", "8192"],
    ["max_output_tokens", 8192.1],
    ["execution_provider_confirmation", { ...wireConfirmation, kind: "local" }],
    ["execution_provider_confirmation", { ...wireConfirmation, version: 0 }],
    [
      "execution_provider_confirmation",
      { ...wireConfirmation, config_version: "5" },
    ],
    [
      "execution_provider_confirmation",
      { ...wireConfirmation, endpoint: "private" },
    ],
    [
      "execution_provider_confirmation",
      { version: 3, configVersion: 5, kind: "remote" },
    ],
    ["output_contract", { type: "unknown" }],
    ["output_contract", { type: "text", name: "hidden.txt" }],
    [
      "output_contract",
      { type: "file", name: "../hidden.md", mime: "text/markdown" },
    ],
    ["modelProtocol", "anthropic_messages"],
    ["maxOutputTokens", 8192],
    ["executionProviderConfirmation", wireConfirmation],
    ["rework_provider_confirmation", wireConfirmation],
    ["execution_contract_version", 6],
    ["execution_contract_version", "5"],
    ["execution_contract_version", null],
    ["id", id.task],
  ])("rejects malformed or contradictory v5 %s: %j", async (field, value) => {
    stub({ ...detail(), [field as string]: value });
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each(
    [
      null,
      {},
      [file, file],
      [{ ...file, content: "private input body" }],
      [{ ...file, relative_path: "private/objects/file" }],
      [{ ...file, size_bytes: 65537 }],
      [{ ...file, size_bytes: "512" }],
      [{ ...file, sha256: "B".repeat(64) }],
      [{ ...file, source_kind: "arbitrary_file" }],
      [{ ...file, name: "../../file.md" }],
      [
        { ...file, size_bytes: 65536 },
        { ...file, id: id.artifact, size_bytes: 65536 },
        { ...file, id: id.submission, size_bytes: 1 },
      ],
    ].map((files) => [files]),
  )("rejects contaminated or unbounded v5 input metadata %j", async (files) => {
    stub({ ...detail(), input_files: files });
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    null,
    { ...rework, review_reason: "" },
    { ...rework, sequence: 0 },
    { ...rework, artifact_ids: [id.artifact] },
    {
      ...rework,
      artifacts: [{ ...rework.artifacts[0], storage_kind: "file" }],
    },
  ])(
    "does not silently drop malformed optional v5 rework %j",
    async (context) => {
      stub({ ...detail(), rework_context: context });
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("forwards cancellation to exact detail lookup without another request", async () => {
    const fetchMock = vi.fn<typeof fetch>(
      (_url, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("cancelled", "AbortError")),
            { once: true },
          );
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();
    const pending = getAgentRun(id.run, controller.signal);
    const rejected = expect(pending).rejects.toMatchObject({ code: "TIMEOUT" });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    controller.abort();
    await rejected;
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(true);
  });

  it("continues to accept old v4 detail without new protocol fields", async () => {
    const value: Record<string, unknown> = {
      ...run,
      execution_contract_version: 4,
      rework_context: rework,
      rework_provider_confirmation: wireConfirmation,
      input_files: [file],
    };
    delete value.output_contract;
    stub(value);
    const result = await getAgentRun(id.run);
    expect(result).toMatchObject({
      executionContractVersion: 4,
      reworkContext: rework,
      reworkProviderConfirmation: confirmation,
      reworkInputFiles: [file],
    });
    expect(result).not.toHaveProperty("modelProtocol");
    expect(result).not.toHaveProperty("executionProviderConfirmation");
  });

  it.each([1, 2, 3, 4])(
    "does not interpret protocol detail as old contract v%s",
    async (version) => {
      stub({ ...detail(), execution_contract_version: version });
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );
});

describe("Anthropic v5 metadata transport and independent retry consent", () => {
  it("accepts metadata-only v5 create and lists without inventing an exact detail", async () => {
    const fetchMock = stub(run);
    const created = await createAgentRun(id.task, id.provider);
    expect(created).toMatchObject({ id: id.run, executionContractVersion: 5 });
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      provider_id: id.provider,
    });
    fetchMock.mockImplementation(async () => respond([run], meta));
    expect((await getTaskAgentRuns(id.task))[0]).toMatchObject({
      id: id.run,
      executionContractVersion: 5,
    });
    const { result_text: _ignored, ...summary } = run;
    fetchMock.mockImplementation(async () =>
      respond([{ ...summary, task_title: "Task" }], meta),
    );
    const listed = (await getAgentRuns()).items[0];
    for (const item of [created, listed]) {
      expect(item).not.toHaveProperty("modelProtocol");
      expect(item).not.toHaveProperty("executionProviderConfirmation");
      expect(item).not.toHaveProperty("executionInputFiles");
      expect(item).not.toHaveProperty("reworkContext");
    }
  });

  it.each([
    ["model_protocol", "anthropic_messages"],
    ["max_output_tokens", 8192],
    ["execution_provider_confirmation", wireConfirmation],
    ["input_files", [file]],
    ["rework_context", rework],
    ["inputFiles", [file]],
    ["reworkProviderConfirmation", wireConfirmation],
  ])("keeps private %s out of Task and global lists", async (field, value) => {
    const leaked = { ...run, [field as string]: value };
    const fetchMock = stub([leaked], meta);
    await expect(getTaskAgentRuns(id.task)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    const { result_text: _ignored, ...summary } = leaked;
    fetchMock.mockImplementation(async () =>
      respond([{ ...summary, task_title: "Task" }], meta),
    );
    await expect(getAgentRuns()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    ["model_protocol", "anthropic_messages"],
    ["max_output_tokens", 8192],
    ["execution_provider_confirmation", wireConfirmation],
    ["input_files", [file]],
    ["rework_context", rework],
  ])(
    "does not accept private %s in create or retry receipts",
    async (field, value) => {
      stub({ ...run, [field as string]: value });
      await expect(createAgentRun(id.task, id.provider)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      await expect(retryAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("sends no body for plain v5 retry and never injects file or rework approval", async () => {
    const fetchMock = stub({
      ...run,
      id: id.artifact,
      parent_run_id: id.run,
      attempt: 3,
    });
    await expect(retryAgentRun(id.run)).resolves.toMatchObject({
      id: id.artifact,
      parentRunId: id.run,
      executionContractVersion: 5,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      `/api/v1/agent-runs/${id.run}/retry`,
    );
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined();
  });

  it.each(["files", "rework", "both"])(
    "serializes only independent %s retry confirmation",
    async (kind) => {
      const options: RetryAgentRunReworkOptions = {};
      const expected: Record<string, unknown> = {};
      if (kind !== "rework") {
        options.confirmFileAccess = true;
        options.fileAccessProviderConfirmation = confirmation;
        expected.confirm_file_access = true;
        expected.file_access_provider_confirmation = wireConfirmation;
      }
      if (kind !== "files") {
        options.confirmReworkContext = true;
        options.reworkProviderConfirmation = confirmation;
        expected.confirm_rework_context = true;
        expected.rework_provider_confirmation = wireConfirmation;
      }
      const fetchMock = stub({
        ...run,
        id: id.artifact,
        parent_run_id: id.run,
        attempt: 3,
      });
      await retryAgentRun(id.run, options);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual(
        expected,
      );
    },
  );

  it.each([
    null,
    {},
    { confirmFileAccess: true },
    { fileAccessProviderConfirmation: confirmation },
    { confirmFileAccess: false, fileAccessProviderConfirmation: confirmation },
    {
      confirmFileAccess: true,
      fileAccessProviderConfirmation: { ...confirmation, version: 0 },
    },
    {
      confirmFileAccess: true,
      fileAccessProviderConfirmation: { ...confirmation, configVersion: "5" },
    },
    {
      confirmFileAccess: true,
      fileAccessProviderConfirmation: { ...confirmation, endpoint: "private" },
    },
    {
      confirmFileAccess: true,
      fileAccessProviderConfirmation: confirmation,
      confirmReworkContext: true,
    },
    {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      confirmFileAccess: true,
    },
    {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      fileAccessProviderConfirmation: confirmation,
    },
    {
      confirmFileAccess: true,
      fileAccessProviderConfirmation: confirmation,
      modelProtocol: "anthropic_messages",
    },
  ])(
    "rejects incomplete or injected retry confirmation %# before HTTP",
    async (options) => {
      const fetchMock = stub(run);
      await expect(
        retryAgentRun(id.run, options as RetryAgentRunReworkOptions),
      ).rejects.toMatchObject({ code: "INVALID_INPUT" });
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it("preserves v4 explicit rework retry serialization and metadata receipt", async () => {
    const fetchMock = stub({
      ...run,
      execution_contract_version: 4,
      id: id.artifact,
      parent_run_id: id.run,
      attempt: 3,
    });
    const result = await retryAgentRun(id.run, {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
    });
    expect(result.executionContractVersion).toBe(4);
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      confirm_rework_context: true,
      rework_provider_confirmation: wireConfirmation,
    });
  });
});
