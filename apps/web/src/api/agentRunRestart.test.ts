import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getAgentRunRestartPreview,
  type AgentRunRestartOptions,
} from "./agentRunRestart";
import {
  createAgentRun,
  getAgentRun,
  getTaskAgentRuns,
  getAgentRuns,
  resetRuntimeConnection,
} from "./client";
import type { CreateAgentRunOptions } from "../types/models";

const id = {
  task: "018f0000-0000-7000-8000-00000000c001",
  source: "018f0000-0000-7000-8000-00000000c002",
  run: "018f0000-0000-7000-8000-00000000c003",
  actor: "018f0000-0000-7000-8000-00000000c004",
  assignment: "018f0000-0000-7000-8000-00000000c005",
  adapter: "018f0000-0000-7000-8000-00000000c006",
  provider: "018f0000-0000-7000-8000-00000000c007",
  oldProvider: "018f0000-0000-7000-8000-00000000c008",
  oldActor: "018f0000-0000-7000-8000-00000000c009",
  file: "018f0000-0000-7000-8000-00000000c010",
  submission: "018f0000-0000-7000-8000-00000000c011",
  artifact: "018f0000-0000-7000-8000-00000000c012",
};
const hash = "a".repeat(64);
const time = "2026-09-21T12:00:00.123456789Z";
const confirmation = { version: 4, configVersion: 6, kind: "remote" as const };
const restart = { runId: id.source, expectedTaskVersion: 9 };
const wireSource = () => ({
  run_id: id.source,
  task_id: id.task,
  status: "failed",
  output_delivery_status: "not_ready",
  attempt: 5,
  task_version: 3,
  actor_id: id.oldActor,
  provider_id: id.oldProvider,
  model: "old-model",
  completed_at: time,
});

function previewData(
  options: AgentRunRestartOptions = { restart },
): Record<string, any> {
  const output = options.outputContract ?? { type: "text" };
  const extended =
    !!options.rework || !!options.inputFiles?.length || output.type !== "text";
  const current: Record<string, any> = {
    task: {
      id: id.task,
      title: "当前任务",
      description: "完整的新业务要求",
      completion_criteria: "人工检查完整交付",
      status: "in_progress",
      kind: "work",
      review_policy: "manual",
      priority: "P1",
      project_id: null,
      parent_task_id: null,
      due_date: null,
      planned_date: null,
      estimated_minutes: null,
      actual_minutes: 0,
      manual_order: null,
      version: 9,
      created_at: time,
      updated_at: time,
    },
    assignment: {
      id: id.assignment,
      role: "assignee",
      assigned_at: time,
      actor_id: id.actor,
    },
    agent: {
      id: id.actor,
      display_name: "当前 Agent",
      type: "agent",
      status: "active",
      version: 3,
    },
    adapter: {
      id: id.adapter,
      display_name: "内置执行器",
      kind: "builtin",
      protocol_version: "opc-agent-pipe-v1",
      status: "enabled",
      health_status: "healthy",
      isolation_status: "verified",
      execution_ready: true,
      version: 5,
    },
    provider: {
      id: id.provider,
      name: "明确选中的模型",
      kind: "remote",
      protocol: "openai_chat",
      model: "current-model",
      status: "ready",
      health_status: "healthy",
      version: 4,
      config_version: 6,
      leaves_device: true,
    },
    attempt: 1,
    execution_contract_version: options.rework
      ? 4
      : output.type === "files"
        ? 3
        : extended
          ? 2
          : 1,
    runtime_limits: { timeout_seconds: 600, max_result_bytes: 65536 },
    success_does_not_complete_task: true,
    result_requires_manual_review: true,
    restart: wireSource(),
  };
  if (extended) {
    current.output_contract = output;
    current.input_files_leave_device = !!options.inputFiles?.length;
    current.file_limits = {
      max_files: 4,
      max_file_bytes: 65536,
      max_total_bytes: 131072,
      max_result_bytes: 65536,
    };
    if (options.inputFiles?.length) {
      current.input_files = options.inputFiles.map((file) => ({
        source_kind: file.sourceKind,
        id: file.id,
        name: "selected.md",
        mime: "text/markdown",
        size_bytes: 26,
        sha256: "b".repeat(64),
      }));
      current.input_file_total_bytes = 26 * options.inputFiles.length;
    }
    if (options.rework)
      current.rework_context = {
        submission_id: options.rework.submissionId,
        sequence: 2,
        review_reason: "完整退回意见",
        reviewed_at: time,
        artifacts: options.rework.artifactIds.map((artifact) => ({
          id: artifact,
          storage_kind: "text",
          name: "previous.md",
          content: "完整的明确选择旧稿",
          sha256: "c".repeat(64),
        })),
      };
  }
  return {
    task_id: id.task,
    task_version: 9,
    source_run: wireSource(),
    current_start: current,
    fingerprint: hash,
  };
}

function runData(): Record<string, any> {
  return {
    id: id.run,
    task_id: id.task,
    assignment_id: id.assignment,
    actor_id: id.actor,
    adapter_id: id.adapter,
    created_by_actor_id: id.actor,
    parent_run_id: null,
    restart_of_run_id: id.source,
    attempt: 1,
    status: "queued",
    provider_id: id.provider,
    model: "current-model",
    execution_contract_version: 1,
    result_text: null,
    result_bytes: null,
    error_code: null,
    output_delivery_status: "not_ready",
    output_delivery_error_code: null,
    submission_id: null,
    artifact_id: null,
    started_at: null,
    completed_at: null,
    created_at: time,
  };
}
function respond(data: unknown, meta?: unknown) {
  return new Response(JSON.stringify({ data, ...(meta ? { meta } : {}) }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("native current-facts restart contract", () => {
  it("reads exact current preview only, preserves source identity and passes cancellation without sending consent", async () => {
    const value = previewData();
    const fetchMock = vi.fn<typeof fetch>(async () => respond(value));
    vi.stubGlobal("fetch", fetchMock);
    const abort = new AbortController();
    const result = await getAgentRunRestartPreview(
      id.task,
      id.provider,
      { restart },
      abort.signal,
    );
    expect(result).toEqual(value);
    expect(result.current_start.attempt).toBeLessThan(
      result.source_run.attempt,
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`/tasks/${id.task}/agent-runs/restart-preview`),
      expect.objectContaining({ method: "POST", signal: abort.signal }),
    );
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      provider_id: id.provider,
      restart: { run_id: id.source, expected_task_version: 9 },
    });
    expect(String(fetchMock.mock.calls[0][1]?.body)).not.toMatch(
      /confirm|fingerprint|content|old-model|old_provider/,
    );
  });

  it.each([
    ["plain", { restart }],
    [
      "files plus default text",
      { restart, inputFiles: [{ sourceKind: "task_artifact", id: id.file }] },
    ],
    [
      "files plus explicit text",
      {
        restart,
        inputFiles: [{ sourceKind: "task_artifact", id: id.file }],
        outputContract: { type: "text" },
      },
    ],
    [
      "single output",
      {
        restart,
        outputContract: {
          type: "file",
          name: "agent-output.md",
          mime: "text/markdown",
        },
      },
    ],
    [
      "multiple outputs",
      {
        restart,
        outputContract: {
          type: "files",
          files: [
            { name: "agent-output.md", mime: "text/markdown" },
            { name: "agent-output.json", mime: "application/json" },
          ],
        },
      },
    ],
    [
      "rework reason-only",
      {
        restart,
        rework: {
          submissionId: id.submission,
          artifactIds: [],
          expectedTaskVersion: 9,
        },
      },
    ],
    [
      "rework selected text",
      {
        restart,
        rework: {
          submissionId: id.submission,
          artifactIds: [id.artifact],
          expectedTaskVersion: 9,
        },
      },
    ],
  ] as [string, AgentRunRestartOptions][])(
    "keeps preview/create selections byte-equivalent for %s, with independent confirmations",
    async (_name, options) => {
      const before = structuredClone(options);
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(respond(previewData(options)))
        .mockResolvedValueOnce(respond(runData()));
      vi.stubGlobal("fetch", fetchMock);
      const preview = await getAgentRunRestartPreview(
        id.task,
        id.provider,
        options,
      );
      const created = await createAgentRun(id.task, id.provider, {
        ...options,
        confirmRestart: true,
        restartPreviewHash: preview.fingerprint,
        ...(options.inputFiles?.length
          ? {
              confirmFileAccess: true,
              fileAccessProviderConfirmation: confirmation,
            }
          : {}),
        ...(options.rework
          ? {
              confirmReworkContext: true,
              reworkProviderConfirmation: confirmation,
            }
          : {}),
      });
      expect(created.restartOfRunId).toBe(id.source);
      expect(created.parentRunId).toBeNull();
      const previewBody = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
      const createBody = JSON.parse(String(fetchMock.mock.calls[1][1]?.body));
      expect(createBody.confirm_restart).toBe(true);
      expect(createBody.restart_preview_hash).toBe(hash);
      for (const key of [
        "confirm_restart",
        "restart_preview_hash",
        "confirm_file_access",
        "file_access_provider_confirmation",
        "confirm_rework_context",
        "rework_provider_confirmation",
      ])
        delete createBody[key];
      expect(createBody).toEqual(previewBody);
      expect(options).toEqual(before);
    },
  );

  it.each([
    ["null source", { restart: null }],
    ["zero version", { restart: { ...restart, expectedTaskVersion: 0 } }],
    [
      "injected source body",
      { restart: { ...restart, resultText: "do not inherit" } },
    ],
    ["consent on preview", { restart, confirmRestart: true }],
    ["automatic assignment", { restart, autoAssign: {} }],
    ["null files", { restart, inputFiles: null }],
    [
      "duplicate files",
      {
        restart,
        inputFiles: [
          { sourceKind: "task_artifact", id: id.file },
          { sourceKind: "task_artifact", id: id.file },
        ],
      },
    ],
    [
      "unmanaged source",
      { restart, inputFiles: [{ sourceKind: "path", id: id.file }] },
    ],
    ["null output", { restart, outputContract: null }],
    [
      "arbitrary file name",
      {
        restart,
        outputContract: {
          type: "file",
          name: "../../unsafe",
          mime: "text/plain",
        },
      },
    ],
    [
      "different rework version",
      {
        restart,
        rework: {
          submissionId: id.submission,
          artifactIds: [],
          expectedTaskVersion: 8,
        },
      },
    ],
  ])("rejects preview %s before HTTP", async (_name, options) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getAgentRunRestartPreview(
        id.task,
        id.provider,
        options as AgentRunRestartOptions,
      ),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    [
      "Task",
      (v: any) => {
        v.task_id = id.source;
      },
    ],
    [
      "version",
      (v: any) => {
        v.task_version++;
      },
    ],
    [
      "fingerprint",
      (v: any) => {
        v.fingerprint = "A".repeat(64);
      },
    ],
    [
      "unknown envelope",
      (v: any) => {
        v.secret = "hidden";
      },
    ],
    [
      "active source",
      (v: any) => {
        v.source_run.status = "running";
      },
    ],
    [
      "pending source",
      (v: any) => {
        v.source_run.output_delivery_status = "pending";
      },
    ],
    [
      "submitted source",
      (v: any) => {
        v.source_run.status = "succeeded";
        v.source_run.output_delivery_status = "submitted";
      },
    ],
    [
      "source body",
      (v: any) => {
        v.source_run.result_text = "must not be accepted";
      },
    ],
    [
      "unfrozen historical source",
      (v: any) => {
        v.source_run.task_version = 0;
      },
    ],
    [
      "source/current mismatch",
      (v: any) => {
        v.current_start.restart.model = "different source model";
      },
    ],
    [
      "current Provider",
      (v: any) => {
        v.current_start.provider.id = id.oldProvider;
      },
    ],
    [
      "current Task",
      (v: any) => {
        v.current_start.task.id = id.source;
      },
    ],
    [
      "unknown input",
      (v: any) => {
        v.current_start.input_snapshot_json = "private";
      },
    ],
    [
      "endpoint disclosure",
      (v: any) => {
        v.current_start.provider.base_url = "private";
      },
    ],
  ] as [string, (value: any) => void][])(
    "rejects mismatched/private response %s",
    async (_name, mutate) => {
      const value = previewData();
      mutate(value);
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>(async () => respond(value)),
      );
      await expect(
        getAgentRunRestartPreview(id.task, id.provider, { restart }),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    },
  );

  it("accepts safe retained source metadata without assuming success means delivered or inheriting its old input", async () => {
    const value = previewData();
    value.source_run.status = "succeeded";
    value.source_run.output_delivery_status = "retained";
    value.current_start.restart = Object.fromEntries(
      Object.entries(value.source_run).reverse(),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond(value)),
    );
    const result = await getAgentRunRestartPreview(id.task, id.provider, {
      restart,
    });
    expect(result.source_run.output_delivery_status).toBe("retained");
    expect(result.current_start.rework_context).toBeUndefined();
    expect(result.current_start.input_files).toBeUndefined();
  });

  it.each([
    "missing consent",
    "missing hash",
    "wrong hash",
    "stray consent",
    "auto assign",
    "file consent only",
    "rework consent only",
  ])("rejects creation %s before HTTP", async (kind) => {
    const options: CreateAgentRunOptions = {
      restart,
      confirmRestart: true,
      restartPreviewHash: hash,
    };
    if (kind === "missing consent") delete options.confirmRestart;
    if (kind === "missing hash") delete options.restartPreviewHash;
    if (kind === "wrong hash") options.restartPreviewHash = "not-a-proof";
    if (kind === "stray consent") delete options.restart;
    if (kind === "auto assign")
      options.autoAssign = { actorId: id.actor, expectedTaskVersion: 9 };
    if (kind === "file consent only") {
      delete options.confirmRestart;
      options.confirmFileAccess = true;
      options.fileAccessProviderConfirmation = confirmation;
      options.inputFiles = [{ sourceKind: "task_artifact", id: id.file }];
    }
    if (kind === "rework consent only") {
      delete options.confirmRestart;
      options.rework = {
        submissionId: id.submission,
        artifactIds: [],
        expectedTaskVersion: 9,
      };
      options.confirmReworkContext = true;
      options.reworkProviderConfirmation = confirmation;
    }
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      createAgentRun(id.task, id.provider, options),
    ).rejects.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    "missing source",
    "wrong source",
    "same Run",
    "retry parent",
    "wrong Task",
    "wrong Provider",
  ])("rejects unbound new execution receipt %s", async (kind) => {
    const value = runData();
    if (kind === "missing source") delete value.restart_of_run_id;
    if (kind === "wrong source") value.restart_of_run_id = id.artifact;
    if (kind === "same Run") value.id = id.source;
    if (kind === "retry parent") value.parent_run_id = id.source;
    if (kind === "wrong Task") value.task_id = id.artifact;
    if (kind === "wrong Provider") value.provider_id = id.oldProvider;
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => respond(value)),
    );
    await expect(
      createAgentRun(id.task, id.provider, {
        restart,
        confirmRestart: true,
        restartPreviewHash: hash,
      }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("preserves validated optional lineage across detail, Task list and global metadata; keeps legacy absent lineage compatible", async () => {
    const meta = {
      page: 1,
      page_size: 20,
      total: 1,
      active_total: 1,
      pending_delivery_total: 0,
      succeeded_total: 0,
    };
    const legacy = runData();
    delete legacy.restart_of_run_id;
    const global = runData();
    delete global.result_text;
    global.task_title = "当前任务";
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(respond(runData()))
      .mockResolvedValueOnce(respond([runData()]))
      .mockResolvedValueOnce(respond([global], meta))
      .mockResolvedValueOnce(respond(legacy));
    vi.stubGlobal("fetch", fetchMock);
    expect((await getAgentRun(id.run)).restartOfRunId).toBe(id.source);
    expect((await getTaskAgentRuns(id.task))[0].restartOfRunId).toBe(id.source);
    expect((await getAgentRuns()).items[0].restartOfRunId).toBe(id.source);
    expect((await getAgentRun(id.run)).restartOfRunId).toBeUndefined();
  });

  it.each([42, {}, [], false, ""])(
    "does not silently erase malformed present lineage %j",
    async (bad) => {
      const value = runData();
      value.restart_of_run_id = bad;
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>(async () => respond(value)),
      );
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it.each([id.source, id.artifact])(
    "rejects duplicate snake/camel restart identities even when one alias is %s",
    async (alias) => {
      const value = runData();
      value.restartOfRunId = alias;
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>(async () => respond(value)),
      );
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("rejects a preview that changes the explicitly chosen file or rework references", async () => {
    const options: AgentRunRestartOptions = {
      restart,
      inputFiles: [{ sourceKind: "task_artifact", id: id.file }],
      rework: {
        submissionId: id.submission,
        artifactIds: [id.artifact],
        expectedTaskVersion: 9,
      },
    };
    for (const kind of ["file", "rework"]) {
      const value = previewData(options);
      if (kind === "file") value.current_start.input_files[0].id = id.source;
      else value.current_start.rework_context.artifacts[0].id = id.source;
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>(async () => respond(value)),
      );
      await expect(
        getAgentRunRestartPreview(id.task, id.provider, options),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    }
  });

  it.each([
    ["numeric parent", { parent_run_id: 42 }],
    ["object parent", { parent_run_id: {} }],
    ["empty parent", { parent_run_id: "" }],
    ["hidden camel parent", { parent_run_id: null, parentRunId: id.source }],
  ])(
    "rejects a restart with %s instead of silently erasing the retry relation",
    async (_name, fields) => {
      const value = { ...runData(), ...(fields as object) };
      const fetchMock = vi.fn<typeof fetch>(async () => respond(value));
      vi.stubGlobal("fetch", fetchMock);
      await expect(getAgentRun(id.run)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
      await expect(
        createAgentRun(id.task, id.provider, {
          restart,
          confirmRestart: true,
          restartPreviewHash: hash,
        }),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    },
  );
});
