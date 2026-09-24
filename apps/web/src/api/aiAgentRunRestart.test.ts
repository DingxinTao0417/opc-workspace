import { beforeEach, describe, expect, it, vi } from "vitest";
import { decideAiAgentRun, parseAiActionProposal } from "./aiWorkspaceActions";
import { parseAiWorkPlan, aiWorkPlanProgress } from "./aiWorkPlan";

import { restartProposal } from "../test/agentRunRestartFixture";
const id = (n: number) =>
  `018f0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const time = "2026-09-21T12:00:00Z";
const mocks = vi.hoisted(() => ({ request: vi.fn() }));
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: mocks.request,
}));
beforeEach(() => mocks.request.mockReset());
function confirmedRestart() {
  const value = restartProposal();
  return {
    ...value,
    status: "confirmed",
    can_confirm: false,
    result_id: id(22),
    result_version: 1,
    decided_at: time,
    route: `/tasks/${id(3)}?agent_run=${id(22)}`,
    agent_run_result: {
      id: id(22),
      task_id: id(3),
      status: "queued",
      attempt: 1,
      provider_id: id(4),
      model: "current",
      output_delivery_status: "not_ready",
      output_delivery_error_code: null,
      submission_id: null,
      artifact_id: null,
      restart_of_run_id: id(5),
    },
  };
}

describe("current-facts Agent restart", () => {
  it.each(["failed", "cancelled", "interrupted"])(
    "accepts current-facts replacement of an unsuccessful %s run",
    (status) => {
      const value = restartProposal();
      Object.assign(value.preview.agent_run_start.restart, {
        status,
        output_delivery_status: "not_ready",
      });
      expect(
        parseAiActionProposal(value).action.changes.restart_of_run_id,
      ).toBe(id(5));
    },
  );
  it.each([null, "", "bad", id(22)])(
    "rejects missing/mismatched source %s",
    (source) => {
      const value = restartProposal();
      Object.assign(value.action.changes, { restart_of_run_id: source });
      expect(() => parseAiActionProposal(value)).toThrow();
    },
  );
  it.each([
    "run_id",
    "task_id",
    "actor_id",
    "provider_id",
    "completed_at",
    "model",
    "task_version",
    "attempt",
  ])("rejects invalid source field %s", (key) => {
    const value = restartProposal();
    Object.assign(value.preview.agent_run_start.restart, { [key]: null });
    expect(() => parseAiActionProposal(value)).toThrow();
  });
  it.each(["private_input", "provider_endpoint"])(
    "rejects extra source field %s",
    (key) => {
      const value = restartProposal();
      Object.assign(value.preview.agent_run_start.restart, {
        [key]: "PRIVATE",
      });
      expect(() => parseAiActionProposal(value)).toThrow();
    },
  );
  it("accepts only a distinct new receipt with exact restart source", () => {
    expect(
      parseAiActionProposal(confirmedRestart()).agent_run_result
        ?.restart_of_run_id,
    ).toBe(id(5));
    for (const source of [null, undefined, id(22), id(3)]) {
      const value = confirmedRestart();
      Object.assign(value.agent_run_result, { restart_of_run_id: source });
      expect(() => parseAiActionProposal(value)).toThrow();
    }
    const same = confirmedRestart();
    same.result_id = id(5);
    same.agent_run_result.id = id(5);
    expect(() => parseAiActionProposal(same)).toThrow();
  });
  it("serializes restart consent only for an explicit confirmation", async () => {
    const value = parseAiActionProposal(restartProposal());
    mocks.request.mockResolvedValue({ data: confirmedRestart() });
    await decideAiAgentRun(value, "confirm", true, undefined, undefined, true);
    expect(JSON.parse(mocks.request.mock.calls[0][1].body)).toEqual({
      fingerprint: value.fingerprint,
      decision: "confirm",
      confirm_agent_execution: true,
      confirm_agent_restart: true,
    });
    mocks.request.mockResolvedValue({
      data: {
        ...restartProposal(),
        status: "rejected",
        can_confirm: false,
        decided_at: time,
      },
    });
    await decideAiAgentRun(value, "reject", true, true, true, true);
    expect(JSON.parse(mocks.request.mock.calls[1][1].body)).toEqual({
      fingerprint: value.fingerprint,
      decision: "reject",
    });
  });
  it("accepts a new start with a retained source and a newly allocated attempt", () => {
    const p = parseAiActionProposal(restartProposal());
    expect(p.preview.agent_run_start?.attempt).toBe(1);
  });
  it.each(["queued", "running", "submitted", "pending"])(
    "rejects nonterminal or delivered source %s",
    (state) => {
      const value = restartProposal();
      value.preview.agent_run_start.restart.output_delivery_status = state;
      expect(() => parseAiActionProposal(value)).toThrow();
    },
  );
  it("accepts retained→new start as a plan replacement without rewriting the original", () => {
    const plan = parseAiWorkPlan(
      {
        session_id: id(20),
        generation_id: id(2),
        generation_status: "completed",
        version: 2,
        title: "执行计划",
        created_at: time,
        as_of: time,
        steps: [
          {
            id: "old",
            title: "原执行",
            kind: "action",
            depends_on: [],
            proposal_id: id(21),
            state: "retained",
            ready: true,
            satisfied: false,
            superseded_by: "new",
            evidence: {
              proposal_id: id(21),
              generation_id: id(2),
              action: "agent_run.start",
              status: "confirmed",
              result_id: id(5),
            },
          },
          {
            id: "new",
            title: "当前事实新执行",
            kind: "action",
            depends_on: [],
            proposal_id: id(1),
            state: "submitted",
            ready: true,
            satisfied: true,
            replaces: "old",
            evidence: {
              proposal_id: id(1),
              generation_id: id(2),
              action: "agent_run.start",
              status: "confirmed",
              result_id: id(22),
              restart_of_run_id: id(5),
            },
          },
        ],
      },
      id(20),
    )!;
    expect(plan.steps[0].satisfied).toBe(false);
    expect(aiWorkPlanProgress(plan).state).toBe("completed");
  });
  it.each(["rejected", "expired"])(
    "keeps a first bound restart source through %s even without the old step in the plan",
    (status) => {
      const first = {
        id: "first",
        title: "新执行",
        kind: "action",
        depends_on: [],
        proposal_id: id(1),
        state: status,
        ready: true,
        satisfied: false,
        superseded_by: "next",
        evidence: {
          proposal_id: id(1),
          generation_id: id(2),
          action: "agent_run.start",
          status,
          restart_of_run_id: id(5),
        },
      };
      const next = {
        ...first,
        id: "next",
        proposal_id: id(21),
        state: "pending",
        replaces: "first",
        evidence: { ...first.evidence, proposal_id: id(21), status: "pending" },
      } as Record<string, unknown>;
      delete next.superseded_by;
      const plan = {
        session_id: id(20),
        generation_id: id(2),
        generation_status: "completed",
        version: 2,
        title: "计划",
        created_at: time,
        as_of: time,
        steps: [first, next],
      };
      expect(aiWorkPlanProgress(parseAiWorkPlan(plan, id(20))!).state).toBe(
        "needs_approval",
      );
      const exactRetry = structuredClone(plan);
      const retryEvidence = {
        ...(next.evidence as Record<string, unknown>),
        action: "agent_run.retry",
        retry_of_run_id: id(5),
      };
      delete (retryEvidence as Record<string, unknown>).restart_of_run_id;
      Object.assign(exactRetry.steps[1], { evidence: retryEvidence });
      expect(
        aiWorkPlanProgress(parseAiWorkPlan(exactRetry, id(20))!).state,
      ).toBe("needs_approval");
      (
        exactRetry.steps[1].evidence as Record<string, unknown>
      ).retry_of_run_id = id(22);
      expect(() => parseAiWorkPlan(exactRetry, id(20))).toThrow();
      for (const evidence of [
        {
          ...(next.evidence as object),
          action: "task.create",
          restart_of_run_id: undefined,
        },
        {
          ...(next.evidence as object),
          action: "agent_run.start",
          restart_of_run_id: id(22),
        },
      ]) {
        const invalid = structuredClone(plan);
        Object.assign(invalid.steps[1], { evidence });
        expect(() => parseAiWorkPlan(invalid, id(20))).toThrow();
      }
    },
  );
});
