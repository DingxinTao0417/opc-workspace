import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getAgentProjectFiles,
  previewAgentProjectFiles,
  recheckAgentProjectFiles,
} from "./agentProjectFiles";
import {
  createAgentRun,
  getAgentRun,
  getAgentRuns,
  getTaskAgentRuns,
  resetRuntimeConnection,
  retryAgentRun,
} from "./client";
import { projectFile, projectWireFile } from "../test/agentProjectFileFixture";
import { agentFileReference } from "../lib/agentProjectFiles";
import type {
  AgentRunFileReference,
  CreateAgentRunOptions,
  RetryAgentRunReworkOptions,
} from "../types/models";

const id = {
  task: "018f0000-0000-7000-8000-000000006201",
  run: "018f0000-0000-7000-8000-000000006202",
  provider: "018f0000-0000-7000-8000-000000006203",
  actor: "018f0000-0000-7000-8000-000000006204",
  assignment: "018f0000-0000-7000-8000-000000006205",
  adapter: "018f0000-0000-7000-8000-000000006206",
  next: "018f0000-0000-7000-8000-000000006207",
};
const confirmation = { version: 3, configVersion: 7, kind: "remote" as const };
const wireConfirmation = { version: 3, config_version: 7, kind: "remote" };
const reference = () => agentFileReference(structuredClone(projectFile));
const candidate = () => ({
  ...structuredClone(projectWireFile),
  eligible: true,
  created_at: projectFile.createdAt,
});
const options = (): CreateAgentRunOptions => ({
  inputFiles: [reference()],
  confirmFileAccess: true,
  fileAccessProviderConfirmation: confirmation,
  confirmProjectTaskFiles: true,
});
const retryOptions = (): RetryAgentRunReworkOptions => ({
  confirmProjectTaskFiles: true,
  confirmFileAccess: true,
  fileAccessProviderConfirmation: confirmation,
});
const run = {
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
  execution_contract_version: 6,
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
const listMeta = {
  page: 1,
  page_size: 20,
  total: 1,
  active_total: 1,
  pending_delivery_total: 0,
  succeeded_total: 0,
};
function detail(
  protocol = "anthropic_messages",
  kind = "remote",
): Record<string, unknown> {
  return {
    ...structuredClone(run),
    model_protocol: protocol,
    max_output_tokens: protocol === "openai_chat" ? 0 : 8192,
    execution_provider_confirmation: { ...wireConfirmation, kind },
    input_files: [structuredClone(projectWireFile)],
  };
}
function page() {
  return {
    data: [candidate()],
    meta: {
      offset: 0,
      limit: 20,
      total: 1,
      next_offset: null as number | null,
    },
  };
}
function respond(payload: unknown) {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
function stub(payload: unknown) {
  const mock = vi.fn<typeof fetch>(async () => respond(payload));
  vi.stubGlobal("fetch", mock);
  return mock;
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("project Task files native discovery and preview", () => {
  it("serializes literal metadata filters and returns complete source identity", async () => {
    const fetchMock = stub(page());
    await expect(
      getAgentProjectFiles(id.task, 0, "调研%_& ?"),
    ).resolves.toEqual({
      items: [projectFile],
      offset: 0,
      total: 1,
      nextOffset: null,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://localhost");
    expect(url.pathname).toBe(
      `/api/v1/tasks/${id.task}/agent-runs/project-task-files`,
    );
    expect(url.searchParams.get("query")).toBe("调研%_& ?");
    expect(url.searchParams.get("offset")).toBe("0");
    expect(fetchMock.mock.calls[0][1]?.body).toBeUndefined();
  });

  it("accepts empty pages and fixed-size pages without treating cursors as selected input", async () => {
    const fetchMock = stub({
      data: [],
      meta: { offset: 40, limit: 20, total: 2, next_offset: null },
    });
    await expect(getAgentProjectFiles(id.task, 40)).resolves.toEqual({
      items: [],
      offset: 40,
      total: 2,
      nextOffset: null,
    });
    const rows = Array.from({ length: 20 }, (_, index) => ({
      ...candidate(),
      id: `018f0000-0000-7000-8000-${String(7000 + index).padStart(12, "0")}`,
    }));
    fetchMock.mockImplementation(async () =>
      respond({
        data: rows,
        meta: { offset: 0, limit: 20, total: 21, next_offset: 20 },
      }),
    );
    await expect(getAgentProjectFiles(id.task)).resolves.toMatchObject({
      total: 21,
      nextOffset: 20,
    });
  });

  it.each([
    ["offset", 1],
    ["limit", 19],
    ["total", -1],
    ["total", "1"],
    ["total", 2],
    ["next_offset", 20],
    ["extra", "private"],
  ])("rejects inconsistent page metadata %s=%s", async (key, value) => {
    const payload = page();
    Object.assign(payload.meta, { [key]: value });
    stub(payload);
    await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([
    ["task_id", id.task],
    ["task_id", "not-uuid"],
    ["project_id", null],
    ["task_version", 0],
    ["task_version", "12"],
    ["submission_sequence", 0],
    ["submission_id", ""],
    ["task_title", "x"],
    ["task_title", " x "],
    ["task_title", "x".repeat(201)],
    ["task_title", "\ud800bad"],
    ["task_title", "bad\0title"],
    ["private_body", "secret"],
  ])("rejects invalid source proof %s", async (key, value) => {
    const payload = page();
    Object.assign(payload.data[0].source_task!, { [key]: value });
    stub(payload);
    await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each(["source_task", "sha256", "name", "mime", "id", "created_at"])(
    "requires candidate %s",
    async (field) => {
      const payload: Record<string, any> = page();
      delete payload.data[0][field];
      stub(payload);
      await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it.each(["content", "relative_path", "endpoint", "sourceTask"])(
    "never silently drops unapproved candidate %s",
    async (field) => {
      const payload = page();
      Object.assign(payload.data[0], { [field]: "secret" });
      stub(payload);
      await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("rejects duplicate or old-kind candidates and accepts explicit unavailable metadata", async () => {
    const payload = page();
    payload.data.push(candidate());
    payload.meta.total = 2;
    const fetchMock = stub(payload);
    await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    fetchMock.mockImplementation(async () =>
      respond({
        ...page(),
        data: [{ ...candidate(), source_kind: "task_artifact" }],
      }),
    );
    await expect(getAgentProjectFiles(id.task)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    fetchMock.mockImplementation(async () =>
      respond({
        ...page(),
        data: [
          {
            ...candidate(),
            eligible: false,
            error_code: "AGENT_RUN_FILE_UNAVAILABLE",
          },
        ],
      }),
    );
    await expect(getAgentProjectFiles(id.task)).resolves.toMatchObject({
      items: [{ eligible: false, errorCode: "AGENT_RUN_FILE_UNAVAILABLE" }],
    });
  });

  it.each(["x".repeat(201), "bad\0query", "\ud800query"])(
    "rejects invalid query before HTTP",
    async (query) => {
      const fetchMock = stub(page());
      await expect(
        getAgentProjectFiles(id.task, 0, query),
      ).rejects.toMatchObject({ code: "INVALID_INPUT" });
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it("sends only exact selected source/hash refs and preserves preview order", async () => {
    const second = {
      ...structuredClone(projectWireFile),
      id: id.next,
      name: "second.md",
    };
    const selected = [reference(), { ...reference(), id: id.next }];
    const fetchMock = stub({
      data: [structuredClone(projectWireFile), second],
    });
    await expect(previewAgentProjectFiles(id.task, selected)).resolves.toEqual([
      projectWireFile,
      second,
    ]);
    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body).toEqual({
      input_files: selected.map((file) => ({
        source_kind: "project_task_artifact",
        id: file.id,
        source_task: file.sourceTask,
        sha256: file.sha256,
      })),
    });
    expect(body).not.toHaveProperty("confirm_project_task_files");
    expect(fetchMock.mock.calls[0][1]?.method).toBe("POST");
    fetchMock.mockImplementation(async () =>
      respond({ data: [second, projectWireFile] }),
    );
    await expect(
      previewAgentProjectFiles(id.task, selected),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("counts the metadata filter limit in Unicode characters rather than UTF-16 units", async () => {
    const fetchMock = stub(page());
    const query = "😀".repeat(200);
    await expect(
      getAgentProjectFiles(id.task, 0, query),
    ).resolves.toMatchObject({ total: 1 });
    expect(
      new URL(
        String(fetchMock.mock.calls[0][0]),
        "http://localhost",
      ).searchParams.get("query"),
    ).toBe(query);
  });

  it("rejects aggregate input bytes above the unchanged 128 KiB preview ceiling", async () => {
    const ids = [projectFile.id, id.next, id.actor];
    const files = ids.map((id) => ({ ...reference(), id }));
    stub({
      data: ids.map((id) => ({
        ...structuredClone(projectWireFile),
        id,
        size_bytes: 65536,
      })),
    });
    await expect(
      previewAgentProjectFiles(id.task, files),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("propagates cancellation of a pending read-only preview without posting execution", async () => {
    let notifyStarted!: () => void;
    const started = new Promise<void>((resolve) => {
      notifyStarted = resolve;
    });
    const fetchMock = vi.fn<typeof fetch>(
      (_url, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          );
          notifyStarted();
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();
    const promise = previewAgentProjectFiles(
      id.task,
      [reference()],
      controller.signal,
    );
    await started;
    controller.abort();
    await expect(promise).rejects.toMatchObject({ code: "TIMEOUT" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "project-task-files/preview",
    );
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(true);
  });

  it.each([
    ["id", id.next],
    ["sha256", "a".repeat(64)],
    ["size_bytes", 0],
    ["size_bytes", 65537],
    ["size_bytes", "120"],
    ["mime", "application/octet-stream"],
    ["name", "../private.md"],
    ["content", "secret"],
    ["relative_path", "objects/private"],
  ])("rejects changed or invalid preview %s", async (field, value) => {
    stub({ data: [{ ...structuredClone(projectWireFile), [field]: value }] });
    await expect(
      previewAgentProjectFiles(id.task, [reference()]),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    "project_id",
    "task_id",
    "task_title",
    "task_version",
    "submission_id",
    "submission_sequence",
  ])("rechecks frozen source field %s", async (field) => {
    const source: Record<string, unknown> = { ...projectFile.sourceTask };
    source[field] =
      field.includes("version") || field.includes("sequence")
        ? Number(source[field]) + 1
        : field === "task_title"
          ? "新的来源标题"
          : id.next;
    stub({
      data: [{ ...structuredClone(projectWireFile), source_task: source }],
    });
    await expect(
      previewAgentProjectFiles(id.task, [reference()]),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("recheck also binds displayed names and size while preview alone binds source/hash", async () => {
    stub({ data: [{ ...projectWireFile, name: "renamed.md" }] });
    await expect(
      recheckAgentProjectFiles(id.task, [projectFile]),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it.each([
    { files: [] },
    { files: [reference(), reference()] },
    { files: [{ sourceKind: "task_artifact", id: projectFile.id }] },
    { files: [{ ...reference(), sourceTask: undefined }] },
    {
      files: [
        {
          ...reference(),
          sourceTask: { ...projectFile.sourceTask, task_id: id.task },
        },
      ],
    },
  ])("rejects invalid preview selection before HTTP", async ({ files }) => {
    const fetchMock = stub({ data: [] });
    await expect(
      previewAgentProjectFiles(id.task, files as AgentRunFileReference[]),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("v6 exact detail, receipts and independent confirmations", () => {
  it.each([
    ["openai_chat", "local"],
    ["openai_chat", "remote"],
    ["anthropic_messages", "remote"],
  ])(
    "parses explicit %s/%s protocol with real cross-task metadata",
    async (protocol, kind) => {
      stub({ data: detail(protocol, kind) });
      await expect(getAgentRun(id.run)).resolves.toMatchObject({
        executionContractVersion: 6,
        modelProtocol: protocol,
        maxOutputTokens: protocol === "openai_chat" ? 0 : 8192,
        executionInputFiles: [projectWireFile],
        executionProviderConfirmation: { ...confirmation, kind },
      });
    },
  );

  it.each([
    "input_files",
    "model_protocol",
    "max_output_tokens",
    "execution_provider_confirmation",
    "output_contract",
  ])("requires v6 detail field %s", async (field) => {
    for (const mode of ["missing", "null"]) {
      const value = detail();
      if (mode === "missing") delete value[field];
      else value[field] = null;
      stub({ data: value });
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    }
  });

  it.each([
    { input_files: [] },
    { model_protocol: "openai_chat", max_output_tokens: 8192 },
    { max_output_tokens: 0 },
    { execution_provider_confirmation: { ...wireConfirmation, kind: "local" } },
    { inputFiles: [projectWireFile] },
    { modelProtocol: "anthropic_messages" },
    { rework_context: null },
    { input_files: [{ ...projectWireFile, source_task: null }] },
    {
      input_files: [
        {
          ...projectWireFile,
          source_task: { ...projectFile.sourceTask, task_id: id.task },
        },
      ],
    },
    { input_files: [{ ...projectWireFile, source_kind: "task_artifact" }] },
  ])("fails closed on invalid v6 detail %#", async (mutation) => {
    stub({ data: { ...detail(), ...mutation } });
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([1, 2, 3, 4, 5])(
    "does not reinterpret project source as old v%d execution",
    async (version) => {
      stub({ data: { ...detail(), execution_contract_version: version } });
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("does not combine cross-task proofs from different Projects in one v6 detail", async () => {
    stub({
      data: {
        ...detail(),
        input_files: [
          projectWireFile,
          {
            ...structuredClone(projectWireFile),
            id: id.next,
            source_task: { ...projectFile.sourceTask, project_id: id.provider },
          },
        ],
      },
    });
    await expect(getAgentRun(id.run)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it("allows explicitly mixed original and cross-task file references without expanding old source fields", async () => {
    const old = {
      id: id.next,
      source_kind: "task_artifact",
      name: "local.md",
      mime: "text/markdown",
      size_bytes: 20,
      sha256: "a".repeat(64),
    };
    stub({ data: { ...detail(), input_files: [projectWireFile, old] } });
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      executionInputFiles: [projectWireFile, old],
    });
  });

  it("keeps old v5 implicit no-input details and empty-body retry compatible", async () => {
    const value = detail();
    value.execution_contract_version = 5;
    delete value.input_files;
    const fetchMock = stub({ data: value });
    await expect(getAgentRun(id.run)).resolves.toMatchObject({
      executionContractVersion: 5,
      executionInputFiles: [],
    });
    fetchMock.mockImplementation(async () =>
      respond({ data: { ...run, execution_contract_version: 5 } }),
    );
    await retryAgentRun(id.run);
    expect(fetchMock.mock.calls[1][1]?.body).toBeUndefined();
  });

  it("creates v6 only from explicit source and separate file plus project confirmations", async () => {
    const fetchMock = stub({ data: run });
    const result = await createAgentRun(id.task, id.provider, options());
    expect(result.executionContractVersion).toBe(6);
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      provider_id: id.provider,
      input_files: [
        {
          source_kind: "project_task_artifact",
          id: projectFile.id,
          source_task: projectFile.sourceTask,
          sha256: projectFile.sha256,
        },
      ],
      output_contract: { type: "text" },
      confirm_file_access: true,
      confirm_project_task_files: true,
      file_access_provider_confirmation: wireConfirmation,
    });
    expect(result).not.toHaveProperty("executionInputFiles");
  });

  it.each([
    { execution_contract_version: 5 },
    { task_id: id.next },
    { provider_id: id.next },
  ])(
    "does not downgrade or misbind confirmed cross-task create receipt %#",
    async (mutation) => {
      stub({ data: { ...run, ...mutation } });
      await expect(
        createAgentRun(id.task, id.provider, options()),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    },
  );

  it("sends three independent retry confirmations without reselecting frozen files", async () => {
    const fetchMock = stub({
      data: { ...run, id: id.next, parent_run_id: id.run, attempt: 2 },
    });
    const result = await retryAgentRun(id.run, retryOptions());
    expect(result.executionContractVersion).toBe(6);
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      confirm_project_task_files: true,
      confirm_file_access: true,
      file_access_provider_confirmation: wireConfirmation,
    });
  });

  it.each([
    { execution_contract_version: 5 },
    { parent_run_id: null },
    { parent_run_id: id.provider },
    { id: id.run },
  ])("rejects wrong v6 retry receipt %#", async (mutation) => {
    stub({
      data: {
        ...run,
        id: id.next,
        parent_run_id: id.run,
        attempt: 2,
        ...mutation,
      },
    });
    await expect(retryAgentRun(id.run, retryOptions())).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it.each([undefined, false, null])(
    "requires independent cross-task create confirmation %s",
    async (confirmed) => {
      const fetchMock = stub({ data: run });
      const value = options();
      (value as Record<string, unknown>).confirmProjectTaskFiles = confirmed;
      await expect(
        createAgentRun(id.task, id.provider, value),
      ).rejects.toMatchObject({
        code: "AGENT_PROJECT_FILE_CONFIRMATION_REQUIRED",
      });
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it.each([
    { confirmProjectTaskFiles: true },
    { ...retryOptions(), confirmFileAccess: false },
    { ...retryOptions(), fileAccessProviderConfirmation: undefined },
    { ...retryOptions(), confirmProjectTaskFiles: false },
  ])("rejects incomplete retry permission before HTTP", async (value) => {
    const fetchMock = stub({ data: run });
    await expect(
      retryAgentRun(id.run, value as RetryAgentRunReworkOptions),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    "input_files",
    "inputFiles",
    "rework_context",
    "model_protocol",
    "execution_provider_confirmation",
  ])(
    "never exposes private %s through v6 lists or mutation receipts",
    async (field) => {
      const leaked = {
        ...run,
        [field]: field.includes("input") ? [projectWireFile] : "private",
      };
      const fetchMock = stub({ data: leaked });
      await expect(
        createAgentRun(id.task, id.provider, options()),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
      await expect(retryAgentRun(id.run, retryOptions())).rejects.toMatchObject(
        { code: "INVALID_RESPONSE" },
      );
      fetchMock.mockImplementation(async () => respond({ data: [leaked] }));
      await expect(getTaskAgentRuns(id.task)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      const { result_text: _ignored, ...summary } = leaked;
      fetchMock.mockImplementation(async () =>
        respond({
          data: [{ ...summary, task_title: "Target" }],
          meta: listMeta,
        }),
      );
      await expect(getAgentRuns()).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );
});
