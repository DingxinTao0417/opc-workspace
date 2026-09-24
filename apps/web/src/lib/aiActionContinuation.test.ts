import { describe, expect, it } from "vitest";
import type {
  AiActionProposal,
  AiAgentRunResult,
} from "../api/aiWorkspaceActions";
import {
  aiActionContinuationScopes,
  assessAiActionContinuation,
  assessAiActionRecheck,
  findAiActionRecheckTarget,
} from "./aiActionContinuation";

const generationId = "018f0000-0000-7000-8000-000000000801";
const runId = "018f0000-0000-7000-8000-000000000802";
const proposal: AiActionProposal = {
  id: "018f0000-0000-7000-8000-000000000803",
  generation_id: generationId,
  fingerprint: "a".repeat(64),
  action: { action: "task.update", changes: { description: "private body" } },
  preview: {
    label: "private title",
    before: {},
    after: { description: "private preview" },
  },
  status: "confirmed",
  can_confirm: false,
  result_id: runId,
  result_version: 1,
  route: `/tasks/${runId}`,
  created_at: "2026-09-20T00:00:00Z",
  decided_at: "2026-09-20T00:01:00Z",
};
const run: AiAgentRunResult = {
  id: runId,
  task_id: "018f0000-0000-7000-8000-000000000804",
  status: "succeeded",
  attempt: 1,
  provider_id: "018f0000-0000-7000-8000-000000000805",
  model: "private model",
  output_delivery_status: "retained",
  output_delivery_error_code: null,
  submission_id: null,
  artifact_id: null,
};
function agent(result?: AiAgentRunResult): AiActionProposal {
  return {
    ...proposal,
    action: { action: "agent_run.start", changes: {} },
    agent_run_result: result,
  };
}

describe("action continuation readiness", () => {
  it.each(["confirmed", "rejected", "expired"] as const)(
    "prepares an explicit request for %s without carrying proposal content",
    (status) => {
      const { request } = assessAiActionContinuation(generationId, [
        { ...proposal, status },
      ]);
      expect(request?.generationId).toBe(generationId);
      expect(request?.scopes).toEqual(["work", "actions"]);
      expect(request?.prompt).toContain(generationId);
      expect(request?.prompt).toContain("拒绝或过期不等于执行成功");
      expect(request?.prompt).toContain("不要重新提出或执行已被拒绝的操作");
      expect(request?.prompt).toContain("不要重复执行");
      expect(JSON.stringify(request)).not.toContain("private");
      expect(JSON.stringify(request)).not.toContain(proposal.id);
    },
  );
  it.each(["pending", "unavailable"] as const)(
    "blocks the whole group for a %s sibling",
    (status) => {
      expect(
        assessAiActionContinuation(generationId, [
          proposal,
          { ...proposal, id: runId, status },
        ]).request,
      ).toBeNull();
    },
  );
  it("rejects incomplete, duplicated and mismatched generations", () => {
    expect(assessAiActionContinuation(generationId, []).request).toBeNull();
    expect(
      assessAiActionContinuation("invalid", [proposal]).request,
    ).toBeNull();
    expect(
      assessAiActionContinuation(generationId, [proposal, proposal]).request,
    ).toBeNull();
    expect(
      assessAiActionContinuation(generationId, [
        { ...proposal, generation_id: runId },
      ]).request,
    ).toBeNull();
  });
  it.each(["queued", "running"] as const)(
    "waits for a %s Agent Run",
    (status) => {
      expect(
        assessAiActionContinuation(generationId, [
          agent({ ...run, status, output_delivery_status: "not_ready" }),
        ]).request,
      ).toBeNull();
    },
  );
  it("blocks pending delivery and missing or mismatched execution evidence", () => {
    expect(
      assessAiActionContinuation(generationId, [
        agent({ ...run, status: "running", output_delivery_status: "pending" }),
      ]).reason,
    ).toContain("待登记");
    expect(
      assessAiActionContinuation(generationId, [agent()]).request,
    ).toBeNull();
    expect(
      assessAiActionContinuation(generationId, [
        agent({ ...run, id: generationId }),
      ]).request,
    ).toBeNull();
  });
  it.each(["submitted", "retained"] as const)(
    "allows checking a terminal %s result without claiming acceptance",
    (output_delivery_status) => {
      const { request } = assessAiActionContinuation(generationId, [
        agent({ ...run, output_delivery_status }),
      ]);
      expect(request?.scopes).toEqual([
        "work",
        "outputs",
        "actions",
        "agent_execution",
      ]);
      expect(request?.prompt).toContain("产出提交不等于任务完成或验收通过");
      expect(JSON.stringify(request)).not.toContain("private");
    },
  );
  it.each(["failed", "cancelled", "interrupted"] as const)(
    "allows explicitly reconsidering %s instead of rerunning automatically",
    (status) => {
      expect(
        assessAiActionContinuation(generationId, [
          agent({ ...run, status, output_delivery_status: "not_ready" }),
        ]).request,
      ).not.toBeNull();
    },
  );
  it("does not require a Run for a rejected execution request", () => {
    expect(
      assessAiActionContinuation(generationId, [
        { ...agent(), status: "rejected" },
      ]).request,
    ).not.toBeNull();
  });
});

it("recommends action scopes without inferring file or knowledge-content access", () => {
  expect(
    aiActionContinuationScopes([
      "agent_run.start",
      "task_submission.accept",
      "client_contact.link",
      "knowledge_source.reindex",
      "financial_entry.create",
      "invoice.update",
      "finance.export_csv",
      undefined,
    ]),
  ).toEqual([
    "work",
    "clients",
    "outputs",
    "actions",
    "agent_execution",
    "knowledge_actions",
    "finance",
    "finance_actions",
    "invoice_actions",
    "finance_exports",
  ]);
});

describe("conflicted action recheck identity", () => {
  const source = {
    sourceSessionId: "018f0000-0000-7000-8000-000000000806",
    generationId,
    proposalId: proposal.id,
    fingerprint: proposal.fingerprint,
  };
  const rejected: AiActionProposal = { ...proposal, status: "rejected" };

  it("prepares only an exact withdrawn source and never copies body/preview", () => {
    const { request } = assessAiActionRecheck(source, [rejected]);
    expect(request).toMatchObject({
      generationId,
      sourceSessionId: source.sourceSessionId,
      recheckProposalId: source.proposalId,
      scopes: ["work", "actions"],
    });
    expect(request?.prompt).toContain(source.proposalId);
    expect(request?.prompt).toContain("其他被拒绝的操作不要重新提出或执行");
    expect(request?.prompt).toContain("历史意图");
    expect(request?.prompt).toContain("需要用户另行选择文件权限");
    expect(JSON.stringify(request)).not.toContain("private");
    expect(JSON.stringify(request)).not.toContain(source.fingerprint);
  });

  it.each(["pending", "confirmed", "expired", "unavailable"] as const)(
    "does not treat %s as a durable rejection",
    (status) => {
      expect(
        assessAiActionRecheck(source, [{ ...proposal, status }]).request,
      ).toBeNull();
    },
  );

  it("requires canonical source identity, exact fingerprint, and a complete group", () => {
    for (const changed of [
      { ...source, sourceSessionId: "invalid" },
      { ...source, generationId: runId },
      { ...source, proposalId: runId },
      { ...source, fingerprint: "b".repeat(64) },
      { ...source, fingerprint: "invalid" },
    ])
      expect(findAiActionRecheckTarget(changed, [rejected])).toBeNull();
    expect(findAiActionRecheckTarget(source, [rejected, rejected])).toBeNull();
    expect(
      findAiActionRecheckTarget(source, [
        { ...rejected, generation_id: runId },
      ]),
    ).toBeNull();
  });

  it.each(["pending", "unavailable"] as const)(
    "blocks a hidden %s sibling",
    (status) => {
      expect(
        assessAiActionRecheck(source, [
          rejected,
          { ...proposal, id: runId, status },
        ]).request,
      ).toBeNull();
    },
  );

  it("keeps active/pending/missing Agent evidence blocked", () => {
    for (const result of [
      undefined,
      { ...run, status: "queued" as const },
      { ...run, status: "running" as const },
      { ...run, output_delivery_status: "pending" as const },
    ]) {
      expect(
        assessAiActionRecheck(source, [
          rejected,
          { ...agent(result), id: "018f0000-0000-7000-8000-000000000807" },
        ]).request,
      ).toBeNull();
    }
  });

  it("adds explicit project client dependency but no file/content grant", () => {
    expect(
      assessAiActionRecheck(source, [
        {
          ...rejected,
          action: { action: "project.update", changes: { client_id: null } },
        },
      ]).request?.scopes,
    ).toEqual(["work", "clients", "actions"]);
    expect(
      assessAiActionRecheck(source, [
        {
          ...rejected,
          action: {
            action: "agent_run.start",
            changes: { input_file_candidate_ids: ["private-file-id"] },
          },
        },
      ]).request?.scopes,
    ).toEqual(["work", "outputs", "actions", "agent_execution"]);
    expect(
      assessAiActionRecheck(source, [
        {
          ...rejected,
          action: {
            action: "knowledge_source.create",
            changes: { content: "private source text" },
          },
        },
      ]).request?.scopes,
    ).toEqual(["knowledge_actions"]);
  });
});

it.each([
  ["finance.export_csv", ["finance", "finance_exports"]],
  ["financial_entry.create", ["finance", "finance_actions"]],
  ["invoice.update", ["finance", "invoice_actions"]],
  ["knowledge_source.reindex", ["knowledge_actions"]],
  ["knowledge_index_job.cancel", ["knowledge_actions"]],
] as const)(
  "does not add work/actions to independent %s continuation",
  (action, expected) => {
    expect(aiActionContinuationScopes([action])).toEqual(expected);
    expect(aiActionContinuationScopes([action], { includePlan: true })).toEqual(
      ["work", "actions", ...expected],
    );
  },
);
