import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  decideAiAgentRunCancel,
  parseAiActionProposal,
} from "./aiWorkspaceActions";
import { apiRequest } from "./client";

vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: vi.fn(),
}));
const task = "018f0000-0000-7000-8000-000000000301";
const run = "018f0000-0000-7000-8000-000000000302";
const provider = "018f0000-0000-7000-8000-000000000303";
const task2 = "018f0000-0000-7000-8000-000000000306";
const run2 = "018f0000-0000-7000-8000-000000000307";
const pending = {
  id: "018f0000-0000-7000-8000-000000000304",
  generation_id: "018f0000-0000-7000-8000-000000000305",
  fingerprint: "a".repeat(64),
  action: {
    action: "agent_run.cancel",
    task_id: task,
    agent_run_id: run,
    expected_version: 3,
    changes: {},
  },
  preview: {
    label: "报告",
    before: {},
    after: {},
    agent_run_cancel: {
      run_id: run,
      task_id: task,
      task_version: 3,
      status: "running",
      attempt: 2,
      provider_id: provider,
      model: "test",
    },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: `/tasks/${task}?agent_run=${run}`,
  created_at: "2026-09-19T00:00:00Z",
  decided_at: null,
};
const confirmed = {
  ...pending,
  status: "confirmed",
  can_confirm: false,
  result_id: run,
  result_version: 1,
  decided_at: "2026-09-19T00:01:00Z",
  agent_run_result: {
    id: run,
    task_id: task,
    status: "running",
    attempt: 2,
    provider_id: provider,
    model: "test",
    output_delivery_status: "not_ready",
    output_delivery_error_code: null,
    submission_id: null,
    artifact_id: null,
  },
};
const batchPending = {
  id: "018f0000-0000-7000-8000-000000000308",
  generation_id: pending.generation_id,
  fingerprint: "b".repeat(64),
  action: {
    action: "agent_run.cancel_many",
    changes: {
      items: [
        { agent_run_id: run, task_id: task, expected_task_version: 3 },
        { agent_run_id: run2, task_id: task2, expected_task_version: 5 },
      ],
    },
  },
  preview: {
    label: "停止 2 个 Agent 执行",
    before: { active_runs: 2 },
    after: { cancel_requested_runs: 2 },
    agent_run_cancel_many: {
      count: 2,
      items: [
        {
          run_id: run,
          task_id: task,
          task_title: "报告",
          task_version: 3,
          status: "running",
          attempt: 2,
          provider_id: provider,
          model: "test",
        },
        {
          run_id: run2,
          task_id: task2,
          task_title: "复核",
          task_version: 5,
          status: "queued",
          attempt: 1,
          provider_id: provider,
          model: "test",
        },
      ],
    },
  },
  status: "pending",
  can_confirm: true,
  result_id: null,
  result_version: null,
  route: "/ai?workspace=agents",
  created_at: pending.created_at,
  decided_at: null,
};
const batchConfirmed = {
  ...batchPending,
  status: "confirmed",
  can_confirm: false,
  result_id: batchPending.id,
  result_version: 1,
  decided_at: confirmed.decided_at,
  agent_run_cancel_many_result: {
    count: 2,
    items: [
      { run_id: run, task_id: task, status: "running" },
      { run_id: run2, task_id: task2, status: "cancelled" },
    ],
  },
};
beforeEach(() => vi.mocked(apiRequest).mockReset());
describe("Agent cancellation contract", () => {
  it("accepts pending, stopping, stopped and deleted receipts", () => {
    expect(
      parseAiActionProposal(pending).preview.agent_run_cancel?.run_id,
    ).toBe(run);
    expect(parseAiActionProposal(confirmed).agent_run_result?.status).toBe(
      "running",
    );
    expect(
      parseAiActionProposal({
        ...confirmed,
        agent_run_result: {
          ...confirmed.agent_run_result,
          status: "cancelled",
        },
      }).agent_run_result?.status,
    ).toBe("cancelled");
    const { agent_run_result: _, ...deleted } = confirmed;
    expect(parseAiActionProposal({ ...deleted, route: "" }).route).toBe("");
  });
  it("rejects mismatched identities, output payloads and false success", () => {
    for (const value of [
      { ...pending, action: { ...pending.action, agent_run_id: provider } },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_run_cancel: {
            ...pending.preview.agent_run_cancel,
            task_version: 4,
          },
        },
      },
      { ...pending, route: "/tasks/another" },
      { ...pending, action: { ...pending.action, changes: { consent: true } } },
      {
        ...confirmed,
        agent_run_result: {
          ...confirmed.agent_run_result,
          status: "succeeded",
        },
      },
      {
        ...confirmed,
        agent_run_result: {
          ...confirmed.agent_run_result,
          result_text: "secret",
        },
      },
      {
        ...confirmed,
        agent_run_result: { ...confirmed.agent_run_result, task_id: provider },
      },
    ])
      expect(() => parseAiActionProposal(value)).toThrow();
  });
  it("sends separate cancellation consent only for confirm and validates replay identity", async () => {
    vi.mocked(apiRequest).mockResolvedValue({ data: confirmed });
    await decideAiAgentRunCancel(
      parseAiActionProposal(pending),
      "confirm",
      true,
    );
    expect(
      JSON.parse(vi.mocked(apiRequest).mock.calls[0][1]?.body as string),
    ).toEqual({
      fingerprint: pending.fingerprint,
      decision: "confirm",
      confirm_agent_cancel: true,
    });
    vi.mocked(apiRequest).mockResolvedValue({
      data: {
        ...pending,
        status: "rejected",
        can_confirm: false,
        decided_at: confirmed.decided_at,
      },
    });
    await decideAiAgentRunCancel(
      parseAiActionProposal(pending),
      "reject",
      true,
    );
    expect(
      JSON.parse(vi.mocked(apiRequest).mock.calls[1][1]?.body as string),
    ).toEqual({ fingerprint: pending.fingerprint, decision: "reject" });
    vi.mocked(apiRequest).mockResolvedValue({
      data: { ...confirmed, id: provider },
    });
    await expect(
      decideAiAgentRunCancel(parseAiActionProposal(pending), "confirm", true),
    ).rejects.toThrow();
  });
  it("validates and confirms one all-or-nothing cancellation set", async () => {
    expect(
      parseAiActionProposal(batchPending).preview.agent_run_cancel_many?.count,
    ).toBe(2);
    expect(
      parseAiActionProposal(batchConfirmed).agent_run_cancel_many_result?.items,
    ).toHaveLength(2);
    for (const value of [
      {
        ...batchPending,
        action: {
          ...batchPending.action,
          changes: {
            items: [
              batchPending.action.changes.items[0],
              batchPending.action.changes.items[0],
            ],
          },
        },
      },
      {
        ...batchPending,
        preview: {
          ...batchPending.preview,
          agent_run_cancel_many: {
            ...batchPending.preview.agent_run_cancel_many,
            count: 1,
          },
        },
      },
      {
        ...batchConfirmed,
        agent_run_cancel_many_result: {
          ...batchConfirmed.agent_run_cancel_many_result,
          items: [
            batchConfirmed.agent_run_cancel_many_result.items[1],
            batchConfirmed.agent_run_cancel_many_result.items[0],
          ],
        },
      },
      { ...batchConfirmed, result_id: run },
    ])
      expect(() => parseAiActionProposal(value)).toThrow();

    vi.mocked(apiRequest).mockResolvedValue({ data: batchConfirmed });
    await decideAiAgentRunCancel(
      parseAiActionProposal(batchPending),
      "confirm",
      true,
    );
    expect(
      JSON.parse(vi.mocked(apiRequest).mock.calls[0][1]?.body as string),
    ).toEqual({
      fingerprint: batchPending.fingerprint,
      decision: "confirm",
      confirm_agent_cancel: true,
    });
  });
});
