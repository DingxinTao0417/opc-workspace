import { beforeEach, describe, expect, it, vi } from "vitest";
import { decideAiAgentRun, parseAiActionProposal } from "./aiWorkspaceActions";

const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));

const id = {
  proposal: "018f0000-0000-7000-8000-000000000201",
  generation: "018f0000-0000-7000-8000-000000000202",
  task: "018f0000-0000-7000-8000-000000000203",
  assignment: "018f0000-0000-7000-8000-000000000204",
  actor: "018f0000-0000-7000-8000-000000000205",
  adapter: "018f0000-0000-7000-8000-000000000206",
  provider: "018f0000-0000-7000-8000-000000000207",
  run: "018f0000-0000-7000-8000-000000000208",
  submission: "018f0000-0000-7000-8000-000000000209",
  artifact: "018f0000-0000-7000-8000-000000000210",
  candidate: "018f0000-0000-7000-8000-000000000211",
  inputFile: "018f0000-0000-7000-8000-000000000212",
};

const pending = {
  id: id.proposal,
  generation_id: id.generation,
  fingerprint: "a".repeat(64),
  action: {
    action: "agent_run.start",
    task_id: id.task,
    expected_version: 9,
    changes: {
      provider_id: id.provider,
      expected_provider_version: 4,
      expected_provider_config_version: 6,
    },
  },
  preview: {
    label: "准备季度报告",
    before: {},
    after: {},
    agent_run_start: {
      task: {
        id: id.task,
        title: "准备季度报告",
        description: "汇总本季度数据",
        completion_criteria: "数字与原始凭证一致",
        status: "in_progress",
        kind: "work",
        review_policy: "manual",
        priority: "P1",
        project_id: null,
        parent_task_id: null,
        due_date: "2026-09-30T18:00:00Z",
        planned_date: "2026-09-19",
        estimated_minutes: 120,
        actual_minutes: 30,
        manual_order: null,
        version: 9,
        created_at: "2026-09-18T08:00:00Z",
        updated_at: "2026-09-18T09:00:00Z",
      },
      assignment: {
        id: id.assignment,
        role: "assignee",
        assigned_at: "2026-09-18T08:30:00Z",
        actor_id: id.actor,
      },
      agent: {
        id: id.actor,
        display_name: "报告 Agent",
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
        name: "远程模型",
        kind: "remote",
        protocol: "openai_chat",
        model: "test-model",
        status: "ready",
        health_status: "healthy",
        version: 4,
        config_version: 6,
        leaves_device: true,
      },
      attempt: 2,
      execution_contract_version: 1,
      runtime_limits: { timeout_seconds: 600, max_result_bytes: 65_536 },
      success_does_not_complete_task: true,
      result_requires_manual_review: true,
    },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: `/tasks/${id.task}`,
  created_at: "2026-09-18T10:00:00Z",
  decided_at: null,
};

const pendingWithFiles = {
  ...pending,
  action: {
    ...pending.action,
    changes: {
      ...pending.action.changes,
      input_file_candidate_ids: [id.candidate],
      output_kind: "file",
    },
  },
  preview: {
    ...pending.preview,
    agent_run_start: {
      ...pending.preview.agent_run_start,
      execution_contract_version: 2,
      input_files: [
        {
          source_kind: "task_artifact",
          id: id.inputFile,
          name: "quarter-source.md",
          mime: "text/markdown",
          size_bytes: 26,
          sha256: "b".repeat(64),
        },
      ],
      input_file_total_bytes: 26,
      input_files_leave_device: true,
      output_contract: {
        type: "file",
        name: "agent-run-attempt-2.md",
        mime: "text/markdown",
      },
      file_limits: {
        max_files: 4,
        max_file_bytes: 65_536,
        max_total_bytes: 131_072,
        max_result_bytes: 65_536,
      },
    },
  },
};

const pendingRework = {
  ...pending,
  action: {
    ...pending.action,
    changes: {
      ...pending.action.changes,
      rework_submission_id: id.submission,
      rework_artifact_ids: [id.artifact],
    },
  },
  preview: {
    ...pending.preview,
    agent_run_start: {
      ...pendingWithFiles.preview.agent_run_start,
      execution_contract_version: 4,
      input_files: undefined,
      input_file_total_bytes: undefined,
      input_files_leave_device: false,
      output_contract: { type: "text" },
      rework_context: {
        submission_id: id.submission,
        sequence: 1,
        review_reason: "补齐缺少的原始证据",
        reviewed_at: "2026-09-18T09:00:00Z",
        artifacts: [
          {
            id: id.artifact,
            storage_kind: "text",
            name: "原报告",
            content: "原文",
            sha256: "c".repeat(64),
          },
        ],
      },
    },
  },
};
delete (pendingRework.preview.agent_run_start as Record<string, unknown>)
  .input_files;
delete (pendingRework.preview.agent_run_start as Record<string, unknown>)
  .input_file_total_bytes;

describe("rework execution contract", () => {
  beforeEach(() => mocks.request.mockReset());
  it("accepts independently confirmed text rework without file access", async () => {
    const proposal = parseAiActionProposal(pendingRework);
    mocks.request.mockResolvedValue({ data: pendingRework });
    await decideAiAgentRun(proposal, "confirm", true, undefined, true);
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toMatchObject({
      confirm_agent_execution: true,
      confirm_agent_rework: true,
    });
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).not.toHaveProperty(
      "confirm_agent_files",
    );
  });
  it.each(["identity", "duplicate", "file", "missing", "unmatched", "extra"])(
    "rejects malformed rework: %s",
    (kind) => {
      const value = structuredClone(pendingRework);
      const context = value.preview.agent_run_start.rework_context;
      if (kind === "identity") context.submission_id = id.run;
      if (kind === "duplicate") context.artifacts.push(context.artifacts[0]);
      if (kind === "file") context.artifacts[0].storage_kind = "file";
      if (kind === "missing")
        delete (value.preview.agent_run_start as Record<string, unknown>)
          .rework_context;
      if (kind === "unmatched") value.action.changes.rework_artifact_ids = [];
      if (kind === "extra") Object.assign(context, { path: "C:/private" });
      expect(() => parseAiActionProposal(value)).toThrow();
    },
  );
});

describe("Agent execution action contract", () => {
  beforeEach(() => mocks.request.mockReset());

  it.each([pending, pendingWithFiles, pendingRework])(
    "accepts frozen retries without new file candidate IDs",
    (source) => {
      const retry = {
        ...source,
        action: {
          action: "agent_run.retry",
          task_id: id.task,
          agent_run_id: id.run,
          expected_version: 9,
          changes: {},
        },
        preview: {
          ...source.preview,
          agent_run_retry: { run_id: id.run, status: "failed", attempt: 1 },
        },
        route: `/tasks/${id.task}?agent_run=${id.run}`,
      };
      expect(parseAiActionProposal(retry).action.action).toBe(
        "agent_run.retry",
      );
      for (const status of ["queued", "running", "succeeded"]) {
        expect(() =>
          parseAiActionProposal({
            ...retry,
            preview: {
              ...retry.preview,
              agent_run_retry: { ...retry.preview.agent_run_retry, status },
            },
          }),
        ).toThrow();
      }
      expect(() =>
        parseAiActionProposal({
          ...retry,
          action: { ...retry.action, changes: { provider_id: id.provider } },
        }),
      ).toThrow();
      expect(() =>
        parseAiActionProposal({
          ...retry,
          preview: {
            ...retry.preview,
            agent_run_retry: {
              ...retry.preview.agent_run_retry,
              run_id: id.task,
            },
          },
        }),
      ).toThrow();
      expect(() =>
        parseAiActionProposal({
          ...retry,
          preview: {
            ...retry.preview,
            agent_run_retry: { ...retry.preview.agent_run_retry, attempt: 2 },
          },
        }),
      ).toThrow();
      expect(() =>
        parseAiActionProposal({ ...retry, route: `/tasks/${id.task}` }),
      ).toThrow();
    },
  );

  it("sends fresh execution and file consent on retry and rejects changed response action", async () => {
    const retry = parseAiActionProposal({
      ...pendingWithFiles,
      action: {
        action: "agent_run.retry",
        task_id: id.task,
        agent_run_id: id.run,
        expected_version: 9,
        changes: {},
      },
      preview: {
        ...pendingWithFiles.preview,
        agent_run_retry: { run_id: id.run, status: "interrupted", attempt: 1 },
      },
      route: `/tasks/${id.task}?agent_run=${id.run}`,
    });
    mocks.request.mockResolvedValue({ data: retry });
    await decideAiAgentRun(retry, "confirm", true, true);
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: retry.fingerprint,
      decision: "confirm",
      confirm_agent_execution: true,
      confirm_agent_files: true,
    });
    mocks.request.mockResolvedValue({ data: pendingWithFiles });
    await expect(
      decideAiAgentRun(retry, "confirm", true, true),
    ).rejects.toThrow();
  });

  it("parses the complete frozen preview and the real confirmed run receipt", () => {
    expect(
      parseAiActionProposal(pending).preview.agent_run_start?.attempt,
    ).toBe(2);
    const confirmed = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: id.run,
        task_id: id.task,
        status: "running",
        attempt: 2,
        provider_id: id.provider,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    expect(parseAiActionProposal(confirmed).agent_run_result).toEqual(
      confirmed.agent_run_result,
    );
  });

  it("binds a start gate preview to its changes and the confirmed receipt", () => {
    const predecessor = "018f0000-0000-7000-8000-0000000002a1";
    const gate = {
      predecessor_run_id: predecessor,
      predecessor_task_id: "018f0000-0000-7000-8000-0000000002a2",
      predecessor_task_title: "撰写初稿",
      predecessor_attempt: 1,
      require: "submitted",
      within_hours: 4,
    };
    const gated = {
      ...pending,
      action: {
        ...pending.action,
        changes: {
          ...pending.action.changes,
          start_after_run_id: predecessor,
          start_after: "submitted",
          start_within_hours: 4,
        },
      },
      preview: {
        ...pending.preview,
        agent_run_start: {
          ...pending.preview.agent_run_start,
          start_gate: gate,
        },
      },
    };
    expect(
      parseAiActionProposal(gated).preview.agent_run_start?.start_gate,
    ).toEqual(gate);
    for (const broken of [
      { ...gate, predecessor_run_id: "018f0000-0000-7000-8000-0000000002a9" },
      { ...gate, within_hours: 5 },
      { ...gate, require: "accepted" },
      { ...gate, predecessor_task_id: id.task },
      { ...gate, predecessor_status: "running" },
    ]) {
      expect(() =>
        parseAiActionProposal({
          ...gated,
          preview: {
            ...gated.preview,
            agent_run_start: {
              ...gated.preview.agent_run_start,
              start_gate: broken,
            },
          },
        }),
      ).toThrow();
    }
    expect(() =>
      parseAiActionProposal({ ...gated, preview: pending.preview }),
    ).toThrow();
    const receipt = {
      id: id.run,
      task_id: id.task,
      status: "queued",
      attempt: 2,
      provider_id: id.provider,
      model: "test-model",
      output_delivery_status: "not_ready",
      output_delivery_error_code: null,
      submission_id: null,
      artifact_id: null,
      start_gate: {
        predecessor_run_id: predecessor,
        require: "submitted",
        expires_at: "2026-09-18T14:01:00Z",
        status: "waiting",
      },
    };
    const confirmed = {
      ...gated,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: receipt,
    };
    expect(
      parseAiActionProposal(confirmed).agent_run_result?.start_gate,
    ).toEqual(receipt.start_gate);
    const { start_gate: _gate, ...ungated } = receipt;
    expect(() =>
      parseAiActionProposal({ ...confirmed, agent_run_result: ungated }),
    ).toThrow();
    expect(() =>
      parseAiActionProposal({
        ...confirmed,
        agent_run_result: { ...receipt, status: "running" },
      }),
    ).toThrow();
  });

  it("accepts only safe v1 no-op file fields on already-persisted cards", () => {
    for (const noOp of [
      { input_file_candidate_ids: [] },
      { output_kind: "text" },
      { input_file_candidate_ids: [], output_kind: "text" },
    ]) {
      const value = structuredClone(pending);
      value.action.changes = { ...value.action.changes, ...noOp };
      expect(
        parseAiActionProposal(value).preview.agent_run_start
          ?.execution_contract_version,
      ).toBe(1);
    }
    for (const unsafe of [
      { input_file_candidate_ids: [id.candidate] },
      { output_kind: "file" },
      { input_file_ids: [] },
    ]) {
      const value = structuredClone(pending);
      value.action.changes = { ...value.action.changes, ...unsafe };
      expect(() => parseAiActionProposal(value)).toThrow("操作确认信息不完整");
    }
  });

  it("accepts a legacy v1 provider without leaves_device but keeps the boundary strict", () => {
    const legacy = structuredClone(pending);
    delete (legacy.preview.agent_run_start.provider as Record<string, unknown>)
      .leaves_device;
    expect(
      parseAiActionProposal(legacy).preview.agent_run_start?.provider
        .leaves_device,
    ).toBeUndefined();

    const mismatched = structuredClone(pending);
    mismatched.preview.agent_run_start.provider.leaves_device = false;
    expect(() => parseAiActionProposal(mismatched)).toThrow(
      "操作确认信息不完整",
    );

    const extra = structuredClone(legacy);
    Object.assign(extra.preview.agent_run_start.provider, {
      device_boundary_note: "unexpected",
    });
    expect(() => parseAiActionProposal(extra)).toThrow("操作确认信息不完整");
  });

  it("strictly parses a metadata-only v2 file boundary without exposing real file facts in model changes", () => {
    const parsed = parseAiActionProposal(pendingWithFiles);
    expect(parsed.preview.agent_run_start).toMatchObject({
      execution_contract_version: 2,
      input_file_total_bytes: 26,
      input_files_leave_device: true,
      output_contract: {
        type: "file",
        name: "agent-run-attempt-2.md",
        mime: "text/markdown",
      },
    });
    expect(parsed.preview.agent_run_start?.input_files).toEqual(
      pendingWithFiles.preview.agent_run_start.input_files,
    );
    expect(parsed.action.changes).toEqual({
      provider_id: id.provider,
      expected_provider_version: 4,
      expected_provider_config_version: 6,
      input_file_candidate_ids: [id.candidate],
      output_kind: "file",
    });
    expect(JSON.stringify(parsed.action.changes)).not.toContain(id.inputFile);
    expect(JSON.stringify(parsed.action.changes)).not.toContain(
      "quarter-source.md",
    );

    const implicitText = structuredClone(pendingWithFiles);
    delete (implicitText.action.changes as Record<string, unknown>).output_kind;
    Object.assign(implicitText.preview.agent_run_start, {
      output_contract: { type: "text" },
    });
    expect(
      parseAiActionProposal(implicitText).preview.agent_run_start
        ?.output_contract,
    ).toEqual({ type: "text" });
  });
  it.each([3, 4])(
    "keeps real input-file consent for a version %s file contract",
    async (version) => {
      const value = structuredClone(
        version === 4 ? pendingRework : pendingWithFiles,
      );
      Object.assign(value.action.changes, {
        input_file_candidate_ids: [id.candidate],
        output_kind: version === 3 ? "files" : "file",
      });
      Object.assign(value.preview.agent_run_start, {
        execution_contract_version: version,
        input_files: pendingWithFiles.preview.agent_run_start.input_files,
        input_file_total_bytes: 26,
        input_files_leave_device: true,
        output_contract:
          version === 3
            ? {
                type: "files",
                files: [
                  { name: "summary.md", mime: "text/markdown" },
                  { name: "data.json", mime: "application/json" },
                ],
              }
            : pendingWithFiles.preview.agent_run_start.output_contract,
      });
      const parsed = parseAiActionProposal(value);
      mocks.request.mockResolvedValue({ data: value });
      await decideAiAgentRun(
        parsed,
        "confirm",
        true,
        true,
        version === 4 ? true : undefined,
      );
      expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toMatchObject({
        confirm_agent_execution: true,
        confirm_agent_files: true,
      });
      expect(
        JSON.parse(mocks.request.mock.calls[0][1].body).confirm_agent_rework,
      ).toBe(version === 4 ? true : undefined);
    },
  );

  it.each([
    [
      "file body",
      () => {
        const value = structuredClone(pendingWithFiles);
        Object.assign(value.preview.agent_run_start.input_files[0], {
          content: "secret",
        });
        return value;
      },
    ],
    [
      "file path",
      () => {
        const value = structuredClone(pendingWithFiles);
        Object.assign(value.preview.agent_run_start.input_files[0], {
          path: "C:/secret.md",
        });
        return value;
      },
    ],
    [
      "real file identity in model changes",
      () => {
        const value = structuredClone(pendingWithFiles);
        value.action.changes.input_file_candidate_ids = [id.inputFile];
        return value;
      },
    ],
    [
      "mismatched total",
      () => {
        const value = structuredClone(pendingWithFiles);
        value.preview.agent_run_start.input_file_total_bytes = 25;
        return value;
      },
    ],
    [
      "mismatched device boundary",
      () => {
        const value = structuredClone(pendingWithFiles);
        value.preview.agent_run_start.input_files_leave_device = false;
        return value;
      },
    ],
    [
      "unknown frozen output field",
      () => {
        const value = structuredClone(pendingWithFiles);
        Object.assign(value.preview.agent_run_start.output_contract, {
          path: "C:/result.md",
        });
        return value;
      },
    ],
    [
      "changed file limit",
      () => {
        const value = structuredClone(pendingWithFiles);
        value.preview.agent_run_start.file_limits.max_files = 5;
        return value;
      },
    ],
  ])("rejects v2 preview with %s", (_name, makeValue) => {
    expect(() => parseAiActionProposal(makeValue())).toThrow(
      "操作确认信息不完整",
    );
  });

  it("keeps a confirmed decision as a non-navigable tombstone after its Task and Run are deleted", () => {
    const tombstone = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: "",
      decided_at: "2026-09-18T10:01:00Z",
    };

    const parsed = parseAiActionProposal(tombstone);
    expect(parsed.result_id).toBe(id.run);
    expect(parsed.agent_run_result).toBeUndefined();
    expect(parsed.route).toBe("");
    expect(() =>
      parseAiActionProposal({
        ...tombstone,
        route: `/tasks/${id.task}?agent_run=${id.run}`,
      }),
    ).toThrow("操作确认信息不完整");
    expect(() =>
      parseAiActionProposal({ ...tombstone, agent_run_result: null }),
    ).toThrow("操作确认信息不完整");
  });

  it("derives the exact submission route only from a submitted delivery receipt", () => {
    const confirmed = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}/submissions/${id.submission}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: id.run,
        task_id: id.task,
        status: "succeeded",
        attempt: 2,
        provider_id: id.provider,
        model: "test-model",
        output_delivery_status: "submitted",
        output_delivery_error_code: null,
        submission_id: id.submission,
        submission_status: "pending_review",
        artifact_id: id.artifact,
      },
    };
    expect(parseAiActionProposal(confirmed).route).toBe(
      `/tasks/${id.task}/submissions/${id.submission}`,
    );
    expect(
      parseAiActionProposal(confirmed).agent_run_result?.submission_status,
    ).toBe("pending_review");
    expect(() =>
      parseAiActionProposal({
        ...confirmed,
        agent_run_result: {
          ...confirmed.agent_run_result,
          submission_status: "unreviewed",
        },
      }),
    ).toThrow("操作确认信息不完整");
  });

  it.each([
    [
      "succeeded without a delivery result",
      {
        status: "succeeded",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    ],
    [
      "submitted without both exact identities",
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
        submission_id: null,
        artifact_id: null,
      },
    ],
    [
      "pending after the run became terminal",
      {
        status: "succeeded",
        output_delivery_status: "pending",
        output_delivery_error_code: "AGENT_OUTPUT_DELIVERY_PENDING",
        submission_id: null,
        artifact_id: null,
      },
    ],
  ])("rejects %s", (_name, delivery) => {
    const confirmed = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: id.run,
        task_id: id.task,
        attempt: 2,
        provider_id: id.provider,
        model: "test-model",
        ...delivery,
      },
    };
    expect(() => parseAiActionProposal(confirmed)).toThrow(
      "操作确认信息不完整",
    );
  });

  it.each([
    [
      "missing frozen task fact",
      () => {
        const value = structuredClone(pending);
        delete (value.preview.agent_run_start.task as Record<string, unknown>)
          .completion_criteria;
        return value;
      },
    ],
    [
      "wrong provider config version",
      () => {
        const value = structuredClone(pending);
        value.preview.agent_run_start.provider.config_version = 7;
        return value;
      },
    ],
    [
      "looser runtime",
      () => {
        const value = structuredClone(pending);
        value.preview.agent_run_start.runtime_limits.timeout_seconds = 601;
        return value;
      },
    ],
    ["unexpected outer field", () => ({ ...pending, result_text: "secret" })],
    [
      "uppercase route identity",
      () => ({
        ...pending,
        route: `/tasks/${id.task.toUpperCase()}`,
      }),
    ],
  ])("rejects %s", (_name, makeValue) => {
    expect(() => parseAiActionProposal(makeValue())).toThrow(
      "操作确认信息不完整",
    );
  });

  it("sends the separate human execution confirmation only for confirm", async () => {
    const confirmed = {
      ...pending,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: id.run,
        task_id: id.task,
        status: "queued",
        attempt: 2,
        provider_id: id.provider,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    await decideAiAgentRun(parseAiActionProposal(pending), "confirm", true);
    expect(mocks.request).toHaveBeenCalledWith(
      `/api/v1/ai/actions/${id.proposal}/decision`,
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          fingerprint: pending.fingerprint,
          decision: "confirm",
          confirm_agent_execution: true,
        }),
      }),
    );

    mocks.request.mockResolvedValueOnce({
      data: { ...pending, status: "rejected", can_confirm: false },
    });
    await decideAiAgentRun(parseAiActionProposal(pending), "reject", true);
    expect(JSON.parse(mocks.request.mock.calls[1][1].body)).toEqual({
      fingerprint: pending.fingerprint,
      decision: "reject",
    });
  });

  it("sends file-body consent only when the frozen v2 preview has inputs", async () => {
    const confirmed = {
      ...pendingWithFiles,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: {
        id: id.run,
        task_id: id.task,
        status: "queued",
        attempt: 2,
        provider_id: id.provider,
        model: "test-model",
        output_delivery_status: "not_ready",
        output_delivery_error_code: null,
        submission_id: null,
        artifact_id: null,
      },
    };
    mocks.request.mockResolvedValueOnce({ data: confirmed });
    await decideAiAgentRun(
      parseAiActionProposal(pendingWithFiles),
      "confirm",
      true,
      true,
    );
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: pendingWithFiles.fingerprint,
      decision: "confirm",
      confirm_agent_execution: true,
      confirm_agent_files: true,
    });

    const fileOutputOnly = structuredClone(pendingWithFiles);
    delete (fileOutputOnly.action.changes as Record<string, unknown>)
      .input_file_candidate_ids;
    delete (fileOutputOnly.preview.agent_run_start as Record<string, unknown>)
      .input_files;
    delete (fileOutputOnly.preview.agent_run_start as Record<string, unknown>)
      .input_file_total_bytes;
    fileOutputOnly.preview.agent_run_start.input_files_leave_device = false;
    const confirmedOutputOnly = {
      ...fileOutputOnly,
      status: "confirmed",
      can_confirm: false,
      result_id: id.run,
      result_version: 1,
      route: `/tasks/${id.task}?agent_run=${id.run}`,
      decided_at: "2026-09-18T10:01:00Z",
      agent_run_result: confirmed.agent_run_result,
    };
    mocks.request.mockResolvedValueOnce({ data: confirmedOutputOnly });
    await decideAiAgentRun(
      parseAiActionProposal(fileOutputOnly),
      "confirm",
      true,
      true,
    );
    expect(JSON.parse(mocks.request.mock.calls[1][1].body)).toEqual({
      fingerprint: pendingWithFiles.fingerprint,
      decision: "confirm",
      confirm_agent_execution: true,
    });

    mocks.request.mockResolvedValueOnce({
      data: {
        ...pendingWithFiles,
        status: "rejected",
        can_confirm: false,
      },
    });
    await decideAiAgentRun(
      parseAiActionProposal(pendingWithFiles),
      "reject",
      true,
      true,
    );
    expect(JSON.parse(mocks.request.mock.calls[2][1].body)).toEqual({
      fingerprint: pendingWithFiles.fingerprint,
      decision: "reject",
    });
  });
});
