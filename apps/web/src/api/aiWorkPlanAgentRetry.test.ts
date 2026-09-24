import { describe, expect, it } from "vitest";
import {
  aiWorkPlanProgress,
  parseAiWorkPlan,
  parseAiWorkPlanContinuation,
} from "./aiWorkPlan";

const session = "018f0000-0000-7000-8000-000000000901";
const generation = "018f0000-0000-7000-8000-000000000902";
const failedRun = "018f0000-0000-7000-8000-000000000903";
const childRun = "018f0000-0000-7000-8000-000000000904";
const originalProposal = "018f0000-0000-7000-8000-000000000905";
const retryProposal = "018f0000-0000-7000-8000-000000000906";

function fixture() {
  return {
    session_id: session,
    generation_id: generation,
    generation_status: "completed",
    version: 2,
    title: "提交 Agent 产出",
    created_at: "2026-09-21T12:00:00Z",
    as_of: "2026-09-21T12:01:00Z",
    steps: [
      {
        id: "original",
        title: "首次执行",
        kind: "action",
        depends_on: [],
        proposal_id: originalProposal,
        state: "failed",
        ready: true,
        satisfied: false,
        superseded_by: "retry",
        evidence: {
          proposal_id: originalProposal,
          generation_id: generation,
          action: "agent_run.start",
          status: "confirmed",
          result_id: failedRun,
        } as Record<string, unknown>,
      },
      {
        id: "retry",
        title: "重试执行",
        kind: "action",
        depends_on: [],
        proposal_id: retryProposal,
        state: "pending",
        ready: true,
        satisfied: false,
        replaces: "original",
        evidence: {
          proposal_id: retryProposal,
          generation_id: generation,
          action: "agent_run.retry",
          status: "pending",
          retry_of_run_id: failedRun,
        } as Record<string, unknown>,
      },
    ],
  };
}

describe("plan Agent Run retry lineage", () => {
  it.each(["failed", "cancelled", "interrupted"])(
    "keeps the %s original receipt and requires a new approval",
    (state) => {
      const value = fixture();
      value.steps[0].state = state;
      const plan = parseAiWorkPlan(value, session)!;
      expect(plan.steps[0]).toMatchObject({ state, satisfied: false });
      expect(aiWorkPlanProgress(plan)).toMatchObject({
        effective_step_total: 1,
        superseded_step_total: 1,
        satisfied_step_total: 0,
        pending_approval_total: 1,
        state: "needs_approval",
      });
      expect(() =>
        parseAiWorkPlanContinuation(
          { plan: value, ready: true, reason: "ready" },
          session,
          2,
        ),
      ).toThrow();
    },
  );

  it.each([
    ["queued", "running"],
    ["running", "running"],
    ["output_pending", "needs_recovery"],
    ["retained", "needs_recovery"],
    ["failed", "awaiting_continuation"],
    ["submitted", "completed"],
  ])(
    "only submitted retry completes the effective step: %s",
    (state, inbox) => {
      const value = fixture();
      value.steps[1].state = state;
      value.steps[1].satisfied = state === "submitted";
      Object.assign(value.steps[1].evidence, {
        status: "confirmed",
        result_id: childRun,
      });
      const plan = parseAiWorkPlan(value, session)!;
      expect(aiWorkPlanProgress(plan).state).toBe(inbox);
      expect(plan.steps[0].satisfied).toBe(false);
      expect(plan.steps[0].state).toBe("failed");
    },
  );

  it.each(["rejected", "expired", "unavailable"])(
    "keeps a %s retry readable but incomplete",
    (state) => {
      const value = fixture();
      value.steps[1].state = state;
      value.steps[1].evidence.status = state;
      const plan = parseAiWorkPlan(value, session)!;
      expect(aiWorkPlanProgress(plan).satisfied_step_total).toBe(0);
    },
  );

  it.each([undefined, null, "", "wrong-id", failedRun.toUpperCase(), childRun])(
    "rejects missing, malformed or mismatched retry identity %s",
    (identity) => {
      const value = fixture();
      value.steps[1].evidence.retry_of_run_id = identity;
      expect(() => parseAiWorkPlan(value, session)).toThrow();
    },
  );

  it.each([
    "queued",
    "running",
    "output_pending",
    "retained",
    "evidence_unavailable",
  ])("cannot retire a source with %s evidence", (state) => {
    const value = fixture();
    value.steps[0].state = state;
    expect(() => parseAiWorkPlan(value, session)).toThrow();
  });

  it("rejects unrelated actions and a retry pointing at its own result", () => {
    const value = fixture();
    value.steps[1].evidence.action = "task.create";
    expect(() => parseAiWorkPlan(value, session)).toThrow();
    value.steps[1].evidence.action = "agent_run.retry";
    value.steps[1].evidence.status = "confirmed";
    value.steps[1].evidence.result_id = failedRun;
    value.steps[1].state = "failed";
    expect(() => parseAiWorkPlan(value, session)).toThrow();
  });

  it("carries the required failed Run through rejected retries instead of laundering it", () => {
    const value = fixture();
    const rejected = value.steps[1];
    rejected.state = "rejected";
    rejected.evidence.status = "rejected";
    rejected.superseded_by = "retry_again";
    value.steps.push({
      ...rejected,
      id: "retry_again",
      replaces: "retry",
      superseded_by: undefined,
      proposal_id: childRun,
      state: "pending",
      evidence: {
        ...rejected.evidence,
        proposal_id: childRun,
        status: "pending",
      },
    });
    delete value.steps[2].superseded_by;
    expect(parseAiWorkPlan(value, session)).not.toBeNull();
    value.steps[2].evidence.action = "task.create";
    delete value.steps[2].evidence.retry_of_run_id;
    expect(() => parseAiWorkPlan(value, session)).toThrow();
  });

  it("anchors the next retry to a newly failed child, not its grandparent", () => {
    const value = fixture();
    const firstRetry = value.steps[1];
    firstRetry.state = "failed";
    firstRetry.superseded_by = "retry_again";
    Object.assign(firstRetry.evidence, {
      status: "confirmed",
      result_id: childRun,
    });
    value.steps.push({
      ...firstRetry,
      id: "retry_again",
      replaces: "retry",
      superseded_by: undefined,
      proposal_id: childRun,
      state: "pending",
      evidence: {
        ...firstRetry.evidence,
        proposal_id: childRun,
        status: "pending",
        retry_of_run_id: childRun,
      },
    });
    delete value.steps[2].superseded_by;
    delete value.steps[2].evidence.result_id;
    expect(parseAiWorkPlan(value, session)).not.toBeNull();
    value.steps[2].evidence.retry_of_run_id = failedRun;
    expect(() => parseAiWorkPlan(value, session)).toThrow();
  });

  it("keeps legacy standalone retry responses compatible but rejects malformed metadata", () => {
    const value = fixture();
    value.steps = [value.steps[1]];
    delete value.steps[0].replaces;
    delete value.steps[0].evidence.retry_of_run_id;
    expect(parseAiWorkPlan(value, session)).not.toBeNull();
    value.steps[0].evidence.retry_of_run_id = null;
    expect(() => parseAiWorkPlan(value, session)).toThrow();
  });
});
