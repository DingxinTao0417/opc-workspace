import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAgentRun,
  getTaskAgentRunFiles,
  resetRuntimeConnection,
} from "./client";

const ids = {
  task: "018f0000-0000-7000-8000-000000000601",
  file: "018f0000-0000-7000-8000-000000000602",
  run: "018f0000-0000-7000-8000-000000000603",
  assignment: "018f0000-0000-7000-8000-000000000604",
  actor: "018f0000-0000-7000-8000-000000000605",
  adapter: "018f0000-0000-7000-8000-000000000606",
  owner: "018f0000-0000-7000-8000-000000000607",
  provider: "018f0000-0000-7000-8000-000000000608",
};

const candidate = {
  source_kind: "task_artifact",
  id: ids.file,
  name: "brief.md",
  mime: "text/markdown",
  size_bytes: 128,
  sha256: "a".repeat(64),
  eligible: true,
  created_at: "2026-09-18T10:00:00Z",
};

const limits = {
  max_files: 4,
  max_file_bytes: 65_536,
  max_total_bytes: 131_072,
  max_result_bytes: 65_536,
};

const queuedRun = {
  id: ids.run,
  task_id: ids.task,
  assignment_id: ids.assignment,
  actor_id: ids.actor,
  adapter_id: ids.adapter,
  created_by_actor_id: ids.owner,
  parent_run_id: null,
  attempt: 1,
  status: "queued",
  provider_id: ids.provider,
  model: "test-model",
  result_text: null,
  result_bytes: null,
  error_code: null,
  output_delivery_status: "not_ready",
  output_delivery_error_code: null,
  submission_id: null,
  artifact_id: null,
  started_at: null,
  completed_at: null,
  created_at: "2026-09-18T10:00:00Z",
};

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  resetRuntimeConnection();
});

describe("Agent Run controlled files client", () => {
  it("serializes rework identity and its independent consent without file permission", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: { ...queuedRun, execution_contract_version: 4 } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const rework = {
      submissionId: ids.assignment,
      artifactIds: [ids.file],
      expectedTaskVersion: 7,
    };
    const confirmation = {
      version: 2,
      configVersion: 3,
      kind: "remote" as const,
    };
    await createAgentRun(ids.task, ids.provider, {
      rework,
      confirmReworkContext: true,
      reworkProviderConfirmation: confirmation,
    });
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      provider_id: ids.provider,
      rework: {
        submission_id: ids.assignment,
        artifact_ids: [ids.file],
        expected_task_version: 7,
      },
      confirm_rework_context: true,
      rework_provider_confirmation: {
        version: 2,
        config_version: 3,
        kind: "remote",
      },
    });
  });
  it.each([
    "missingConsent",
    "missingProvider",
    "duplicate",
    "autoAssign",
    "extra",
    "noRework",
  ])("rejects invalid rework before network: %s", async (kind) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const options: Record<string, unknown> = {
      rework: {
        submissionId: ids.assignment,
        artifactIds: [ids.file],
        expectedTaskVersion: 7,
      },
      confirmReworkContext: true,
      reworkProviderConfirmation: {
        version: 2,
        configVersion: 3,
        kind: "remote",
      },
    };
    if (kind === "missingConsent") delete options.confirmReworkContext;
    if (kind === "missingProvider") delete options.reworkProviderConfirmation;
    if (kind === "duplicate")
      (options.rework as Record<string, unknown>).artifactIds = [
        ids.file,
        ids.file,
      ];
    if (kind === "autoAssign")
      options.autoAssign = { actorId: ids.actor, expectedTaskVersion: 7 };
    if (kind === "extra")
      (options.rework as Record<string, unknown>).content = "forged";
    if (kind === "noRework") delete options.rework;
    await expect(
      createAgentRun(ids.task, ids.provider, options),
    ).rejects.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("keeps the legacy provider-only body for a text run without files", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: queuedRun }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createAgentRun(ids.task, ids.provider);

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      provider_id: ids.provider,
    });
  });

  it("sends an unassigned Task snapshot as one atomic create-run command", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: queuedRun }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createAgentRun(ids.task, ids.provider, {
      autoAssign: { actorId: ids.actor, expectedTaskVersion: 7 },
    });

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      provider_id: ids.provider,
      auto_assign: {
        actor_id: ids.actor,
        expected_task_version: 7,
      },
    });
  });

  it("rejects malformed automatic assignment identities before the request", async () => {
    const fetchMock = vi.fn<typeof fetch>();
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      createAgentRun(ids.task, ids.provider, {
        autoAssign: { actorId: "not-an-actor", expectedTaskVersion: 0 },
      }),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("parses metadata-only candidates and the exact server limits", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ data: [candidate], meta: { limits } })),
    );

    await expect(getTaskAgentRunFiles(ids.task)).resolves.toEqual({
      items: [
        {
          sourceKind: "task_artifact",
          id: ids.file,
          name: "brief.md",
          mime: "text/markdown",
          sizeBytes: 128,
          sha256: "a".repeat(64),
          eligible: true,
          errorCode: null,
          createdAt: "2026-09-18T10:00:00Z",
        },
      ],
      limits: {
        maxFiles: 4,
        maxFileBytes: 65_536,
        maxTotalBytes: 131_072,
        maxResultBytes: 65_536,
      },
    });
  });

  it("accepts a service-side limit reduction and exposes it to the UI", async () => {
    const tightened = {
      max_files: 2,
      max_file_bytes: 32_768,
      max_total_bytes: 65_536,
      max_result_bytes: 32_768,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({ data: [candidate], meta: { limits: tightened } }),
      ),
    );

    await expect(getTaskAgentRunFiles(ids.task)).resolves.toMatchObject({
      limits: {
        maxFiles: 2,
        maxFileBytes: 32_768,
        maxTotalBytes: 65_536,
        maxResultBytes: 32_768,
      },
    });
  });

  it("keeps unsupported or incomplete files as unavailable metadata without hiding eligible text", async () => {
    const unavailable = [
      {
        ...candidate,
        id: "018f0000-0000-7000-8000-000000000609",
        name: "diagram.png",
        mime: "image/png",
        eligible: false,
        error_code: "AGENT_RUN_FILE_INVALID",
      },
      {
        ...candidate,
        id: "018f0000-0000-7000-8000-000000000610",
        name: "missing.txt",
        mime: "",
        size_bytes: 0,
        sha256: "",
        eligible: false,
        error_code: "AGENT_RUN_FILE_UNAVAILABLE",
      },
    ];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse({ data: [candidate, ...unavailable], meta: { limits } }),
      ),
    );

    const result = await getTaskAgentRunFiles(ids.task);

    expect(result.items.map((item) => [item.name, item.eligible])).toEqual([
      ["brief.md", true],
      ["diagram.png", false],
      ["missing.txt", false],
    ]);
  });

  it.each([
    ["candidate body", { ...candidate, content: "secret" }, limits],
    ["invalid hash", { ...candidate, sha256: "bad" }, limits],
    [
      "limit drift",
      candidate,
      { ...limits, max_total_bytes: limits.max_total_bytes + 1 },
    ],
  ])(
    "rejects %s instead of retaining unsafe candidate data",
    async (_name, row, serverLimits) => {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          jsonResponse({ data: [row], meta: { limits: serverLimits } }),
        ),
      );

      await expect(getTaskAgentRunFiles(ids.task)).rejects.toMatchObject({
        code: "INVALID_RESPONSE",
      });
    },
  );

  it("sends selected identities, the frozen output preset and explicit consent", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: queuedRun }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createAgentRun(ids.task, ids.provider, {
      inputFiles: [{ sourceKind: "task_artifact", id: ids.file }],
      outputContract: {
        type: "file",
        name: "agent-output.md",
        mime: "text/markdown",
      },
      confirmFileAccess: true,
      fileAccessProviderConfirmation: {
        version: 7,
        configVersion: 11,
        kind: "remote",
      },
    });

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      provider_id: ids.provider,
      input_files: [{ source_kind: "task_artifact", id: ids.file }],
      confirm_file_access: true,
      file_access_provider_confirmation: {
        version: 7,
        config_version: 11,
        kind: "remote",
      },
      output_contract: {
        type: "file",
        name: "agent-output.md",
        mime: "text/markdown",
      },
    });
  });

  it("requires consent only when files are selected and never sends it otherwise", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: queuedRun }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      createAgentRun(ids.task, ids.provider, {
        inputFiles: [{ sourceKind: "task_artifact", id: ids.file }],
        outputContract: { type: "text" },
      }),
    ).rejects.toMatchObject({
      code: "AGENT_FILE_ACCESS_CONFIRMATION_REQUIRED",
    });
    expect(fetchMock).not.toHaveBeenCalled();

    await expect(
      createAgentRun(ids.task, ids.provider, {
        inputFiles: [{ sourceKind: "task_artifact", id: ids.file }],
        outputContract: { type: "text" },
        confirmFileAccess: true,
      }),
    ).rejects.toMatchObject({
      code: "AGENT_FILE_ACCESS_PROVIDER_CONFIRMATION_REQUIRED",
    });
    expect(fetchMock).not.toHaveBeenCalled();

    await createAgentRun(ids.task, ids.provider, {
      outputContract: {
        type: "file",
        name: "agent-output.txt",
        mime: "text/plain",
      },
    });
    const body = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body));
    expect(body).not.toHaveProperty("confirm_file_access");
    expect(body).not.toHaveProperty("input_files");
  });

  it("rejects malformed or spurious Provider confirmation snapshots", async () => {
    const fetchMock = vi.fn<typeof fetch>(async () =>
      jsonResponse({ data: queuedRun }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      createAgentRun(ids.task, ids.provider, {
        inputFiles: [{ sourceKind: "task_artifact", id: ids.file }],
        confirmFileAccess: true,
        fileAccessProviderConfirmation: {
          version: 7,
          configVersion: 11,
          kind: "local",
          unexpected: true,
        } as never,
      }),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });

    await expect(
      createAgentRun(ids.task, ids.provider, {
        fileAccessProviderConfirmation: {
          version: 7,
          configVersion: 11,
          kind: "local",
        },
      }),
    ).rejects.toMatchObject({ code: "INVALID_INPUT" });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
