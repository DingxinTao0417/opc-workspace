import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  decideAiAgentRunRecovery,
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
const submission = "018f0000-0000-7000-8000-000000000306";
const artifact = "018f0000-0000-7000-8000-000000000307";
const pending = {
  id: "018f0000-0000-7000-8000-000000000304",
  generation_id: "018f0000-0000-7000-8000-000000000305",
  fingerprint: "a".repeat(64),
  action: {
    action: "agent_run.recover_output",
    task_id: task,
    agent_run_id: run,
    expected_version: 3,
    changes: {},
  },
  preview: {
    label: "报告",
    before: {},
    after: {},
    agent_run_recovery: {
      run_id: run,
      task_id: task,
      task_version: 3,
      status: "running",
      attempt: 2,
      provider_id: provider,
      model: "test",
      output_kind: "file",
      output_bytes: 20,
      output_sha256: "b".repeat(64),
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
  route: `/tasks/${task}/submissions/${submission}`,
  agent_run_result: {
    id: run,
    task_id: task,
    status: "succeeded",
    attempt: 2,
    provider_id: provider,
    model: "test",
    output_delivery_status: "submitted",
    output_delivery_error_code: null,
    submission_id: submission,
    artifact_id: artifact,
  },
};
beforeEach(() => vi.mocked(apiRequest).mockReset());
describe("Agent output recovery contract", () => {
  it("accepts pending, submitted, retained and missing Run receipts", () => {
    expect(
      parseAiActionProposal(pending).preview.agent_run_recovery?.output_kind,
    ).toBe("file");
    expect(parseAiActionProposal(confirmed).route).toBe(confirmed.route);
    const retained = {
      ...confirmed,
      route: pending.route,
      agent_run_result: {
        ...confirmed.agent_run_result,
        output_delivery_status: "retained",
        output_delivery_error_code: "AGENT_RUN_IDENTITY_CHANGED",
        submission_id: null,
        artifact_id: null,
      },
    };
    expect(
      parseAiActionProposal(retained).agent_run_result?.output_delivery_status,
    ).toBe("retained");
    const { agent_run_result: _, ...deleted } = confirmed;
    expect(parseAiActionProposal({ ...deleted, route: "" }).route).toBe("");
  });
  it("rejects forged source identity, body, result state and route", () => {
    for (const value of [
      { ...pending, action: { ...pending.action, agent_run_id: provider } },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_run_recovery: {
            ...pending.preview.agent_run_recovery,
            output_sha256: "wrong",
          },
        },
      },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_run_recovery: {
            ...pending.preview.agent_run_recovery,
            output_bytes: 65_537,
          },
        },
      },
      {
        ...pending,
        preview: {
          ...pending.preview,
          agent_run_recovery: {
            ...pending.preview.agent_run_recovery,
            content: "private",
          },
        },
      },
      {
        ...confirmed,
        agent_run_result: { ...confirmed.agent_run_result, status: "running" },
      },
      {
        ...confirmed,
        agent_run_result: { ...confirmed.agent_run_result, artifact_id: null },
      },
      { ...confirmed, route: pending.route },
    ])
      expect(() => parseAiActionProposal(value)).toThrow();
  });
  it("sends only independent recovery consent and checks response identity", async () => {
    const proposal = parseAiActionProposal(pending);
    vi.mocked(apiRequest).mockResolvedValue({ data: confirmed });
    await decideAiAgentRunRecovery(proposal, "confirm", true);
    const options = vi.mocked(apiRequest).mock.calls[0][1];
    expect(JSON.parse(String(options?.body))).toEqual({
      fingerprint: proposal.fingerprint,
      decision: "confirm",
      confirm_agent_output_recovery: true,
    });
    vi.mocked(apiRequest).mockResolvedValue({
      data: { ...confirmed, id: provider },
    });
    await expect(
      decideAiAgentRunRecovery(proposal, "confirm", true),
    ).rejects.toThrow();
  });
});
