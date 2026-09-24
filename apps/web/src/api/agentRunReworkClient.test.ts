import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAgentRun,
  getAgentRun,
  getAgentRuns,
  getTaskAgentRuns,
  resetRuntimeConnection,
  retryAgentRun,
} from "./client";
import type {
  CreateAgentRunOptions,
  RetryAgentRunReworkOptions,
} from "../types/models";

const id = {
  task: "018f0000-0000-7000-8000-000000000951",
  run: "018f0000-0000-7000-8000-000000000952",
  actor: "018f0000-0000-7000-8000-000000000953",
  assignment: "018f0000-0000-7000-8000-000000000954",
  adapter: "018f0000-0000-7000-8000-000000000955",
  provider: "018f0000-0000-7000-8000-000000000956",
  submission: "018f0000-0000-7000-8000-000000000957",
  artifact: "018f0000-0000-7000-8000-000000000958",
  file: "018f0000-0000-7000-8000-000000000959",
};
const confirmation = { version: 3, configVersion: 5, kind: "remote" as const };
const wireConfirmation = { version: 3, config_version: 5, kind: "remote" };
const rework = {
  submissionId: id.submission,
  artifactIds: [id.artifact],
  expectedTaskVersion: 7,
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
  model: "test-model",
  execution_contract_version: 4,
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
const context = {
  submission_id: id.submission,
  sequence: 1,
  review_reason: "完整退回意见",
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
function detail() {
  return {
    ...run,
    rework_context: structuredClone(context),
    input_files: [structuredClone(file)],
    rework_provider_confirmation: { ...wireConfirmation },
  };
}
function respond(data: unknown, meta?: unknown) {
  return new Response(JSON.stringify({ data, ...(meta ? { meta } : {}) }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
const listMeta = {
  page: 1,
  page_size: 20,
  total: 1,
  active_total: 1,
  pending_delivery_total: 0,
  succeeded_total: 0,
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("native Agent rework command transport", () => {
  it("keeps rework and controlled file confirmations separate when creating one Run", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => respond(run));
    vi.stubGlobal("fetch", fetchMock);
    await createAgentRun(id.task, id.provider, {
      rework,
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      inputFiles: [{ sourceKind: "task_artifact", id: id.file }],
      confirmFileAccess: true,
      fileAccessProviderConfirmation: confirmation,
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toMatchObject({
      provider_id: id.provider,
      rework: {
        submission_id: id.submission,
        artifact_ids: [id.artifact],
        expected_task_version: 7,
      },
      confirm_rework_context: true,
      rework_provider_confirmation: wireConfirmation,
      input_files: [{ source_kind: "task_artifact", id: id.file }],
      confirm_file_access: true,
      file_access_provider_confirmation: wireConfirmation,
    });
    expect(String(fetchMock.mock.calls[0][1]?.body)).not.toContain(
      "完整旧产出",
    );
  });

  it.each([
    "zeroProviderVersion",
    "badProviderKind",
    "extraProviderField",
    "nullRework",
    "fileConsentDoesNotGrantRework",
  ])("rejects invalid create %s without HTTP", async (kind) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const options: Record<string, unknown> = {
      rework,
      confirmReworkContext: true,
      reworkProviderConfirmation: { ...confirmation },
    };
    if (kind === "zeroProviderVersion")
      (options.reworkProviderConfirmation as Record<string, unknown>).version =
        0;
    if (kind === "badProviderKind")
      (options.reworkProviderConfirmation as Record<string, unknown>).kind =
        "unknown";
    if (kind === "extraProviderField")
      (options.reworkProviderConfirmation as Record<string, unknown>).endpoint =
        "private";
    if (kind === "nullRework") options.rework = null;
    if (kind === "fileConsentDoesNotGrantRework") {
      delete options.confirmReworkContext;
      options.confirmFileAccess = true;
      options.fileAccessProviderConfirmation = confirmation;
    }
    await expect(
      createAgentRun(id.task, id.provider, options as CreateAgentRunOptions),
    ).rejects.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("preserves legacy empty-body retry and sends only explicit v4 consent", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => respond(run));
    vi.stubGlobal("fetch", fetchMock);
    await retryAgentRun(id.run);
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined();
    await retryAgentRun(id.run, {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      confirm_rework_context: true,
      rework_provider_confirmation: wireConfirmation,
    });
    await retryAgentRun(id.run, {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      confirmFileAccess: true,
      fileAccessProviderConfirmation: confirmation,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      confirm_rework_context: true,
      rework_provider_confirmation: wireConfirmation,
      confirm_file_access: true,
      file_access_provider_confirmation: wireConfirmation,
    });
    expect(
      fetchMock.mock.calls.every(([url]) =>
        String(url).endsWith(`/api/v1/agent-runs/${id.run}/retry`),
      ),
    ).toBe(true);
  });

  it.each([
    {},
    { confirmReworkContext: false, reworkProviderConfirmation: confirmation },
    { confirmReworkContext: true },
    {
      confirmReworkContext: true,
      reworkProviderConfirmation: { ...confirmation, configVersion: 0 },
    },
    {
      confirmReworkContext: true,
      reworkProviderConfirmation: { ...confirmation, kind: "invalid" },
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
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      confirmFileAccess: false,
      fileAccessProviderConfirmation: confirmation,
    },
    {
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
      confirmFileAccess: true,
      fileAccessProviderConfirmation: {
        ...confirmation,
        version: Number.MAX_SAFE_INTEGER + 1,
      },
    },
  ])(
    "fails closed before retry HTTP for invalid confirmation %#",
    async (options) => {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      await expect(
        retryAgentRun(id.run, options as RetryAgentRunReworkOptions),
      ).rejects.toMatchObject({ code: "INVALID_INPUT" });
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );
});

describe("Agent rework detail and list privacy contracts", () => {
  it("loads full context and only frozen file metadata from exact detail", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () => respond(detail()));
    vi.stubGlobal("fetch", fetchMock);
    const result = await getAgentRun(id.run);
    expect(result).toMatchObject({
      id: id.run,
      executionContractVersion: 4,
      reworkContext: context,
      reworkProviderConfirmation: confirmation,
      reworkInputFiles: [file],
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`/api/v1/agent-runs/${id.run}`),
      expect.any(Object),
    );
    expect(result.reworkInputFiles?.[0]).not.toHaveProperty("content");
  });

  it("normalizes native omitempty for a reason-only v4 detail to no input files", async () => {
    const value: Record<string, unknown> = detail();
    delete value.input_files;
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond(value)),
    );
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      reworkContext: context,
      reworkProviderConfirmation: confirmation,
      reworkInputFiles: [],
    });
  });

  it.each([
    "missingContext",
    "missingProvider",
    "nullFileList",
    "badContext",
    "wrongRun",
    "wrongVersion",
    "fileBody",
    "filePath",
    "duplicateFiles",
    "oversizedFiles",
    "invalidProvider",
    "legacyPrivateContext",
  ])("rejects incomplete or contaminated detail %s", async (kind) => {
    const value: Record<string, unknown> = detail();
    if (kind === "missingContext") delete value.rework_context;
    if (kind === "missingProvider") delete value.rework_provider_confirmation;
    if (kind === "nullFileList") value.input_files = null;
    if (kind === "badContext")
      value.rework_context = { ...context, review_reason: "" };
    if (kind === "wrongRun") value.id = id.task;
    if (kind === "wrongVersion") value.execution_contract_version = 5;
    if (kind === "fileBody")
      value.input_files = [{ ...file, content: "do not expose input bytes" }];
    if (kind === "filePath")
      value.input_files = [{ ...file, relative_path: "private/store" }];
    if (kind === "duplicateFiles") value.input_files = [file, file];
    if (kind === "oversizedFiles")
      value.input_files = [{ ...file, size_bytes: 65_537 }];
    if (kind === "invalidProvider")
      value.rework_provider_confirmation = {
        ...wireConfirmation,
        config_version: 0,
      };
    if (kind === "legacyPrivateContext") value.execution_contract_version = 3;
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond(value)),
    );
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    "rework_context",
    "reworkContext",
    "input_files",
    "rework_provider_confirmation",
  ])(
    "rejects private %s on both Task and global metadata lists",
    async (field) => {
      const leaked = {
        ...run,
        [field]: field === "input_files" ? [file] : context,
      };
      const fetchMock = vi.fn<typeof fetch>(async () =>
        respond([leaked], listMeta),
      );
      vi.stubGlobal("fetch", fetchMock);
      await expect(getTaskAgentRuns(id.task)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      const { result_text: _ignored, ...summary } = leaked;
      fetchMock.mockImplementation(async () =>
        respond([{ ...summary, task_title: "Task" }], listMeta),
      );
      await expect(getAgentRuns()).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("keeps v4 list records metadata-only instead of inventing a rework preview", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond([run], listMeta)),
    );
    const rows = await getTaskAgentRuns(id.task);
    expect(rows[0]).toMatchObject({ id: id.run, executionContractVersion: 4 });
    expect(rows[0]).not.toHaveProperty("reworkContext");
    expect(rows[0]).not.toHaveProperty("reworkInputFiles");
  });
});
