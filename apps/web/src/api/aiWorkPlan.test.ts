import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiRequest } from "./client";
import {
  aiWorkPlanActiveSteps,
  aiWorkPlanContinuationScopes,
  aiWorkPlanProgress,
  closeAiWorkPlan,
  getAiWorkPlan,
  getAiWorkPlanContinuation,
  parseAiWorkPlanContinuation,
  getAiWorkPlanInbox,
  parseAiWorkPlan,
  parseAiWorkPlanInbox,
  type AiWorkPlan,
} from "./aiWorkPlan";
vi.mock("./client", async () => ({
  ...(await vi.importActual("./client")),
  apiRequest: vi.fn(),
}));
const session = "018f0000-0000-7000-8000-000000000901";
const generation = "018f0000-0000-7000-8000-000000000902";
const proposal = "018f0000-0000-7000-8000-000000000903";
const result = "018f0000-0000-7000-8000-000000000904";
function fixture(): AiWorkPlan {
  return {
    session_id: session,
    generation_id: generation,
    generation_status: "completed",
    version: 1,
    title: "Review and record",
    created_at: "2026-09-19T12:00:00Z",
    as_of: "2026-09-19T12:01:00Z",
    steps: [
      {
        id: "check",
        title: "Check",
        kind: "analysis",
        depends_on: [],
        report: "reported_done",
        state: "reported_done",
        ready: true,
        satisfied: true,
      },
      {
        id: "record",
        title: "Record",
        kind: "action",
        depends_on: ["check"],
        proposal_id: proposal,
        state: "recorded",
        ready: true,
        satisfied: true,
        evidence: {
          proposal_id: proposal,
          generation_id: generation,
          action: "task.create",
          status: "confirmed",
          result_id: result,
          route: `/tasks/${result}`,
        },
      },
    ],
  };
}
function inboxFixture() {
  return {
    data: [
      {
        session_id: session,
        session_title: "落地页会话",
        session_updated_at: "2026-09-19T12:02:00Z",
        version: 2,
        generation_id: generation,
        generation_status: "completed",
        plan_title: "完成落地页任务",
        state: "awaiting_continuation",
        step_total: 3,
        satisfied_step_total: 2,
        pending_approval_total: 0,
        active_step_total: 0,
        recovery_step_total: 0,
        created_at: "2026-09-19T12:00:00Z",
        as_of: "2026-09-19T12:03:00Z",
      },
    ],
    meta: {
      state_filter: "attention",
      total: 1,
      attention_total: 1,
      running_total: 0,
      needs_approval_total: 0,
      needs_recovery_total: 0,
      awaiting_continuation_total: 1,
      completed_total: 0,
      has_more: false,
      next_offset: null,
      window_limited: false,
      as_of: "2026-09-19T12:03:00Z",
    },
  };
}
function replacementFixture(): AiWorkPlan {
  const original = fixture() as AiWorkPlan;
  const old = original.steps[1];
  old.state = "rejected";
  old.satisfied = false;
  old.superseded_by = "replacement";
  old.evidence!.status = "rejected";
  delete old.evidence!.result_id;
  original.steps.push({
    ...fixture().steps[1],
    id: "replacement",
    title: "Replacement",
    kind: "action",
    replaces: old.id,
    proposal_id: "018f0000-0000-7000-8000-000000000905",
    evidence: {
      ...fixture().steps[1].evidence!,
      proposal_id: "018f0000-0000-7000-8000-000000000905",
    },
  });
  return original;
}
beforeEach(() => vi.resetAllMocks());
function continuationFixture() {
  const plan = fixture();
  plan.steps.push({
    id: "verify",
    title: "Verify",
    kind: "analysis",
    depends_on: ["record"],
    report: "pending",
    state: "pending",
    ready: true,
    satisfied: false,
  });
  return { plan, ready: true, reason: "ready" };
}

describe("atomic plan continuation check", () => {
  it("accepts only a blocking generation belonging to the current active plan sources", () => {
    const data = {
      ...continuationFixture(),
      ready: false,
      reason: "pending_approval",
      blocking_generation_id: generation,
    };
    expect(
      parseAiWorkPlanContinuation(data, session, 1).blocking_generation_id,
    ).toBe(generation);
    for (const patch of [
      { blocking_generation_id: result },
      { blocking_generation_id: null },
      { ready: true, reason: "ready" },
      { reason: "generation_active" },
    ])
      expect(() =>
        parseAiWorkPlanContinuation({ ...data, ...patch }, session, 1),
      ).toThrow();
  });
  it("validates a complete ordered blocker summary without accepting foreign groups", () => {
    const data = {
      ...continuationFixture(),
      ready: false,
      reason: "pending_approval",
      blocking_generation_id: generation,
      blockers: [
        {
          reason: "pending_approval",
          total: 2,
          generation_ids: [generation],
        },
        {
          reason: "run_active",
          total: 1,
          generation_ids: [generation],
        },
      ],
    };
    expect(parseAiWorkPlanContinuation(data, session, 1).blockers).toEqual(
      data.blockers,
    );
    for (const blockers of [
      [...data.blockers].reverse(),
      [data.blockers[0], data.blockers[0]],
      [{ ...data.blockers[0], total: 0 }],
      [{ ...data.blockers[0], generation_ids: [result] }],
      [{ ...data.blockers[0], total: 0, generation_ids: [generation] }],
      [{ ...data.blockers[0], reason: "output_pending" }],
    ])
      expect(() =>
        parseAiWorkPlanContinuation({ ...data, blockers }, session, 1),
      ).toThrow();
    expect(() =>
      parseAiWorkPlanContinuation(
        { ...continuationFixture(), blockers: data.blockers },
        session,
        1,
      ),
    ).toThrow();
  });
  it("uses the exact displayed version and an abortable read-only request", async () => {
    const data = continuationFixture();
    vi.mocked(apiRequest).mockResolvedValue({ data });
    const signal = new AbortController().signal;
    expect(await getAiWorkPlanContinuation(session, 1, signal)).toEqual(data);
    expect(apiRequest).toHaveBeenCalledExactlyOnceWith(
      `/api/v1/ai/sessions/${session}/plan/continuation?expected_version=1`,
      { signal },
    );
  });
  it.each([0, -1, 1.5, 129, Number.NaN, Number.MAX_SAFE_INTEGER + 1])(
    "rejects invalid version %s before any request",
    async (version) => {
      await expect(
        getAiWorkPlanContinuation(session, version),
      ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
      expect(apiRequest).not.toHaveBeenCalled();
    },
  );
  it("rejects invalid session before any request", async () => {
    await expect(
      getAiWorkPlanContinuation("foreign/path", 1),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
    expect(apiRequest).not.toHaveBeenCalled();
  });
  it.each([
    "generation_active",
    "pending_approval",
    "run_active",
    "output_pending",
    "evidence_unavailable",
    "generation_unavailable",
  ])("accepts the code-owned blocked reason %s", (reason) => {
    const data = { ...continuationFixture(), ready: false, reason };
    expect(parseAiWorkPlanContinuation(data, session, 1)).toEqual(data);
  });
  it.each([
    { ready: "true" },
    { ready: false },
    { reason: "unknown" },
    { reason: "run_active" },
    { ready: false, reason: null },
    { plan: null },
    { preview: "private" },
  ])("rejects inconsistent or extra response fields %j", (patch) => {
    expect(() =>
      parseAiWorkPlanContinuation(
        { ...continuationFixture(), ...patch },
        session,
        1,
      ),
    ).toThrow();
  });
  it("rejects foreign session, different version and an already satisfied plan", () => {
    const value = continuationFixture();
    expect(() => parseAiWorkPlanContinuation(value, result, 1)).toThrow();
    expect(() => parseAiWorkPlanContinuation(value, session, 2)).toThrow();
    expect(() =>
      parseAiWorkPlanContinuation({ ...value, plan: fixture() }, session, 1),
    ).toThrow();
    expect(() =>
      parseAiWorkPlanContinuation(
        { ...value, ready: false, reason: "plan_complete" },
        session,
        1,
      ),
    ).toThrow();
    expect(
      parseAiWorkPlanContinuation(
        { plan: fixture(), ready: false, reason: "plan_complete" },
        session,
        1,
      ).reason,
    ).toBe("plan_complete");
  });
  it("rejects ready when visible generation or action evidence blocks continuation", () => {
    const value = continuationFixture();
    value.plan.generation_status = "streaming";
    expect(() => parseAiWorkPlanContinuation(value, session, 1)).toThrow();
    value.plan.generation_status = "completed";
    Object.assign(value.plan.steps[1], { state: "pending", satisfied: false });
    value.plan.steps[1].evidence!.status = "pending";
    delete value.plan.steps[1].evidence!.result_id;
    value.plan.steps[2].ready = false;
    expect(() => parseAiWorkPlanContinuation(value, session, 1)).toThrow();
  });
});

describe("durable work plans", () => {
  it("accepts only known Task review states on submitted Agent Run evidence", () => {
    const plan = fixture();
    const step = plan.steps[1];
    step.state = "submitted";
    step.satisfied = true;
    step.evidence = {
      ...step.evidence!,
      action: "agent_run.start",
      result_id: result,
      route: `/tasks/${result}/submissions/${proposal}`,
      submission_status: "pending_review",
    };
    expect(parseAiWorkPlan(plan, session)).toBe(plan);

    const unknown = structuredClone(plan);
    (unknown.steps[1].evidence as Record<string, unknown>).submission_status =
      "unreviewed";
    expect(() => parseAiWorkPlan(unknown, session)).toThrow("计划响应无效");

    const unrelated = structuredClone(plan);
    unrelated.steps[1].evidence!.action = "task.create";
    expect(() => parseAiWorkPlan(unrelated, session)).toThrow("计划响应无效");
  });

  it.each(["agent_delegate.spawn", "agent_delegate.followup"])(
    "keeps a completed %s child with unresolved approvals blocked until review",
    (action) => {
      const plan = fixture();
      const step = plan.steps[1];
      step.evidence!.action = action;
      step.evidence!.route = `/ai?session=${result}`;
      for (const state of [
        "queued",
        "streaming",
        "child_pending_approval",
        "completed",
        "failed",
        "cancelled",
      ]) {
        step.state = state;
        step.satisfied = state === "completed";
        expect(parseAiWorkPlan(plan, session)).toBe(plan);
        if (state === "child_pending_approval") {
          expect(aiWorkPlanProgress(plan).pending_approval_total).toBe(1);
        }
      }
      step.state = "completed";
      step.satisfied = false;
      expect(() => parseAiWorkPlan(plan, session)).toThrow("计划响应无效");
      step.state = "pending";
      expect(() => parseAiWorkPlan(plan, session)).toThrow("计划响应无效");
    },
  );

  it("treats a verified batch interruption as complete only after every Run stops", () => {
    const plan = fixture();
    const step = plan.steps[1];
    step.state = "cancelled";
    step.satisfied = true;
    step.evidence!.action = "agent_run.cancel_many";
    step.evidence!.route = "/ai?workspace=agents";
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    step.state = "running";
    step.satisfied = false;
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    step.satisfied = true;
    expect(() => parseAiWorkPlan(plan, session)).toThrow("计划响应无效");
  });

  it("keeps rejected receipts truthful while counting only effective replacement steps", () => {
    const plan = replacementFixture();
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    expect(plan.steps[1]).toMatchObject({
      state: "rejected",
      satisfied: false,
      superseded_by: "replacement",
    });
    expect(aiWorkPlanActiveSteps(plan).map((step) => step.id)).toEqual([
      "check",
      "replacement",
    ]);
    expect(aiWorkPlanProgress(plan)).toMatchObject({
      state: "completed",
      step_total: 3,
      effective_step_total: 2,
      satisfied_step_total: 2,
      superseded_step_total: 1,
    });
    plan.steps[1].evidence!.action = "financial_entry.create";
    expect(aiWorkPlanContinuationScopes(plan)).toEqual(["work", "actions"]);
  });

  it("accepts an explicit chain without turning replaced refusals into success", () => {
    const plan = replacementFixture();
    const second = plan.steps[2];
    second.state = "expired";
    second.satisfied = false;
    second.superseded_by = "final";
    second.evidence!.status = "expired";
    delete second.evidence!.result_id;
    plan.steps.push({
      ...replacementFixture().steps[2],
      id: "final",
      replaces: "replacement",
      proposal_id: "018f0000-0000-7000-8000-000000000906",
      evidence: {
        ...replacementFixture().steps[2].evidence!,
        proposal_id: "018f0000-0000-7000-8000-000000000906",
      },
    });
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    expect(aiWorkPlanProgress(plan)).toMatchObject({
      state: "completed",
      effective_step_total: 2,
      superseded_step_total: 2,
    });
    expect(plan.steps.slice(1, 3).map((step) => step.satisfied)).toEqual([
      false,
      false,
    ]);
  });

  it("allows an unbound replacement but requires downstream dependencies to be explicitly rewired", () => {
    const plan = replacementFixture();
    const replacement = plan.steps[2];
    replacement.state = "not_proposed";
    replacement.satisfied = false;
    delete replacement.proposal_id;
    delete replacement.evidence;
    plan.steps.push({
      id: "review",
      title: "Review replacement",
      kind: "analysis",
      depends_on: ["replacement"],
      report: "pending",
      state: "pending",
      ready: false,
      satisfied: false,
    });
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    expect(aiWorkPlanProgress(plan)).toMatchObject({
      state: "awaiting_continuation",
      satisfied_step_total: 1,
      effective_step_total: 3,
    });
    plan.steps[3].depends_on = ["record"];
    expect(() => parseAiWorkPlan(plan, session)).toThrow("计划响应无效");
  });

  it.each<[string, (plan: AiWorkPlan) => void]>([
    [
      "missing reverse link",
      (p) => {
        delete p.steps[1].superseded_by;
      },
    ],
    [
      "extra reverse link",
      (p) => {
        p.steps[2].superseded_by = "ghost";
      },
    ],
    [
      "wrong reverse link",
      (p) => {
        p.steps[1].superseded_by = "check";
      },
    ],
    [
      "null reverse link",
      (p) => {
        p.steps[1].superseded_by = null as never;
      },
    ],
    [
      "empty reverse link",
      (p) => {
        p.steps[1].superseded_by = "";
      },
    ],
    [
      "undefined reverse link",
      (p) => {
        p.steps[1].superseded_by = undefined;
      },
    ],
    [
      "null replacement",
      (p) => {
        p.steps[2].replaces = null as never;
      },
    ],
    [
      "empty replacement",
      (p) => {
        p.steps[2].replaces = "";
      },
    ],
    [
      "undefined replacement",
      (p) => {
        p.steps[2].replaces = undefined;
      },
    ],
    [
      "unknown replacement",
      (p) => {
        p.steps[2].replaces = "ghost";
      },
    ],
    [
      "self replacement",
      (p) => {
        p.steps[2].replaces = "replacement";
      },
    ],
    [
      "forward replacement",
      (p) => {
        p.steps[1].replaces = "replacement";
      },
    ],
    [
      "analysis replacement",
      (p) => {
        p.steps[0].replaces = "record";
      },
    ],
    [
      "analysis reverse link",
      (p) => {
        p.steps[0].superseded_by = "replacement";
      },
    ],
    [
      "replacing analysis",
      (p) => {
        p.steps[2].replaces = "check";
      },
    ],
    [
      "missing old evidence",
      (p) => {
        delete p.steps[1].evidence;
        p.steps[1].state = "evidence_unavailable";
      },
    ],
    [
      "pending old receipt",
      (p) => {
        p.steps[1].state = "pending";
        p.steps[1].evidence!.status = "pending";
      },
    ],
    [
      "unavailable old receipt",
      (p) => {
        p.steps[1].state = "unavailable";
        p.steps[1].evidence!.status = "unavailable";
      },
    ],
    [
      "successful old receipt",
      (p) => {
        p.steps[1] = {
          ...fixture().steps[1],
          kind: "action",
          superseded_by: "replacement",
        };
      },
    ],
    [
      "dependent on replaced step",
      (p) => {
        p.steps[2].depends_on = ["record"];
        p.steps[2].ready = false;
      },
    ],
    [
      "duplicate replacement",
      (p) => {
        p.steps.push({
          ...p.steps[2],
          id: "duplicate",
          proposal_id: undefined,
          evidence: undefined,
          state: "not_proposed",
          satisfied: false,
        });
      },
    ],
  ])("rejects %s", (_label, mutate) => {
    const plan = replacementFixture();
    mutate(plan);
    expect(() => parseAiWorkPlan(plan, session)).toThrow("计划响应无效");
  });

  it("does not count out-of-order outcomes as effective progress and mirrors live analysis", () => {
    const plan = fixture() as AiWorkPlan;
    plan.steps[0].report = "in_progress";
    plan.steps[0].state = "in_progress";
    plan.steps[0].satisfied = false;
    plan.steps[1].ready = false;
    plan.generation_status = "streaming";
    expect(parseAiWorkPlan(plan, session)).toBe(plan);
    expect(aiWorkPlanProgress(plan)).toMatchObject({
      state: "running",
      satisfied_step_total: 0,
    });
  });
  it("reads exact session/revision with an abort signal and does not mutate anything", async () => {
    vi.mocked(apiRequest).mockResolvedValue({ data: fixture() });
    const signal = new AbortController().signal;
    expect((await getAiWorkPlan(session, signal, 1))?.version).toBe(1);
    expect(apiRequest).toHaveBeenCalledWith(
      `/api/v1/ai/sessions/${session}/plan?version=1`,
      { signal },
    );
    expect(parseAiWorkPlan(null, session)).toBeNull();
    await expect(getAiWorkPlan(session, signal, 2)).rejects.toThrow(
      "计划响应无效",
    );
  });
  it("rejects wrong sessions, cycles, forged completion and unsafe routes", () => {
    for (const mutate of [
      (p: ReturnType<typeof fixture>) => {
        p.session_id = generation;
      },
      (p: ReturnType<typeof fixture>) => {
        p.version = 129;
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[0].depends_on = ["record"];
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[0].state = "completed";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].state = "submitted";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].evidence!.status = "pending";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].evidence!.route = "https://evil.example/";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].evidence!.proposal_id = generation;
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].evidence!.action = "execute_shell";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].report = "reported_done";
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].ready = false;
      },
      (p: ReturnType<typeof fixture>) => {
        p.steps[1].satisfied = false;
      },
    ]) {
      const p = fixture();
      mutate(p);
      expect(() => parseAiWorkPlan(p, session)).toThrow("计划响应无效");
    }
  });
  it("distinguishes Run delivery from approval and terminal generation from live analysis", () => {
    for (const state of [
      "queued",
      "running",
      "output_pending",
      "retained",
      "submitted",
      "failed",
      "evidence_unavailable",
    ]) {
      const p = fixture();
      p.steps[1].evidence!.action = "agent_run.start";
      p.steps[1].state = state;
      p.steps[1].satisfied = state === "submitted";
      expect(parseAiWorkPlan(p, session)?.steps[1].state).toBe(state);
      p.steps[1].satisfied = !p.steps[1].satisfied;
      expect(() => parseAiWorkPlan(p, session)).toThrow();
    }
    const p = fixture();
    p.steps[0].report = "in_progress";
    p.steps[0].state = "awaiting_continuation";
    p.steps[0].satisfied = false;
    p.steps[1].ready = false;
    expect(parseAiWorkPlan(p, session)?.steps[0].state).toBe(
      "awaiting_continuation",
    );
  });
});

describe("Agent Inbox work plans", () => {
  it("retains legacy totals and accepts completion with explicitly counted historical replacements", () => {
    const page = inboxFixture();
    Object.assign(page.data[0], {
      superseded_step_total: 1,
      state: "completed",
    });
    Object.assign(page.meta, {
      state_filter: "all",
      attention_total: 0,
      awaiting_continuation_total: 0,
      completed_total: 1,
    });
    expect(parseAiWorkPlanInbox(page, "all").data[0]).toMatchObject({
      step_total: 3,
      satisfied_step_total: 2,
      superseded_step_total: 1,
    });
    expect(
      parseAiWorkPlanInbox(inboxFixture(), "attention").data[0]
        .superseded_step_total,
    ).toBeUndefined();
  });

  it("keeps a replacement needing approval in the attention queue", () => {
    const page = inboxFixture();
    Object.assign(page.data[0], {
      superseded_step_total: 1,
      satisfied_step_total: 1,
      pending_approval_total: 1,
      state: "needs_approval",
    });
    Object.assign(page.meta, {
      needs_approval_total: 1,
      awaiting_continuation_total: 0,
    });
    expect(parseAiWorkPlanInbox(page, "attention").data[0]).toMatchObject({
      state: "needs_approval",
      step_total: 3,
      superseded_step_total: 1,
      satisfied_step_total: 1,
    });
  });

  it.each([null, "1", -1, 0.5, 3, 4, undefined])(
    "rejects invalid or zero-effective replacement count %s",
    (superseded) => {
      const page = inboxFixture();
      Object.assign(page.data[0], { superseded_step_total: superseded });
      expect(() => parseAiWorkPlanInbox(page, "attention")).toThrow();
    },
  );

  it("rejects counter totals that exceed effective steps even when each counter fits separately", () => {
    const page = inboxFixture();
    Object.assign(page.data[0], {
      superseded_step_total: 1,
      satisfied_step_total: 0,
      pending_approval_total: 1,
      active_step_total: 1,
      recovery_step_total: 1,
      state: "needs_recovery",
    });
    Object.assign(page.meta, {
      needs_recovery_total: 1,
      awaiting_continuation_total: 0,
    });
    expect(() => parseAiWorkPlanInbox(page, "attention")).toThrow();
  });
  it("reads a bounded metadata-only attention page", async () => {
    vi.mocked(apiRequest).mockResolvedValue(inboxFixture());
    const signal = new AbortController().signal;
    const page = await getAiWorkPlanInbox("attention", 20, 0, signal);

    expect(page.data[0]).toMatchObject({
      session_id: session,
      state: "awaiting_continuation",
      satisfied_step_total: 2,
    });
    expect(apiRequest).toHaveBeenCalledWith(
      "/api/v1/ai/work-plans?state=attention&limit=20&offset=0",
      { signal },
    );
  });

  it("rejects forged state, counters, filters and duplicate sessions", () => {
    for (const mutate of [
      (page: ReturnType<typeof inboxFixture>) => {
        page.data[0].state = "completed";
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.data[0].satisfied_step_total = 3;
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.meta.attention_total = 2;
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.meta.total = 0;
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.meta.state_filter = "all";
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.data.push({ ...page.data[0] });
        page.meta.total = 2;
      },
      (page: ReturnType<typeof inboxFixture>) => {
        page.meta.has_more = true;
      },
    ]) {
      const page = inboxFixture();
      mutate(page);
      expect(() => parseAiWorkPlanInbox(page, "attention")).toThrow(
        "计划响应无效",
      );
    }
  });
});

describe("user-closed plans", () => {
  const closedAt = "2026-09-19T12:05:00Z";
  function closedInbox() {
    const page = inboxFixture();
    page.data[0].state = "closed";
    Object.assign(page.meta, {
      state_filter: "closed",
      total: 1,
      attention_total: 0,
      awaiting_continuation_total: 0,
      closed_total: 1,
    });
    return page;
  }

  it("accepts a closure timestamp only in a canonical date shape", () => {
    expect(
      parseAiWorkPlan({ ...fixture(), closed_at: closedAt }, session)
        ?.closed_at,
    ).toBe(closedAt);
    for (const closed_at of ["", "yesterday", null, 1]) {
      expect(() =>
        parseAiWorkPlan({ ...fixture(), closed_at }, session),
      ).toThrow("计划响应无效");
    }
  });

  it("binds plan_closed to a closed plan and nothing else", () => {
    const { plan } = continuationFixture();
    const closed = { ...plan, closed_at: closedAt };
    expect(
      parseAiWorkPlanContinuation(
        { plan: closed, ready: false, reason: "plan_closed", blockers: [] },
        session,
        1,
      ).reason,
    ).toBe("plan_closed");
    expect(() =>
      parseAiWorkPlanContinuation(
        { plan, ready: false, reason: "plan_closed", blockers: [] },
        session,
        1,
      ),
    ).toThrow("计划响应无效");
    expect(() =>
      parseAiWorkPlanContinuation(
        { plan: closed, ready: true, reason: "ready", blockers: [] },
        session,
        1,
      ),
    ).toThrow("计划响应无效");
  });

  it("keeps closed plans out of attention while listing them on request", () => {
    expect(parseAiWorkPlanInbox(closedInbox(), "closed").data[0].state).toBe(
      "closed",
    );
    const leaked = closedInbox();
    leaked.meta.state_filter = "attention";
    leaked.meta.total = 0;
    expect(() => parseAiWorkPlanInbox(leaked, "attention")).toThrow(
      "计划响应无效",
    );
    const all = closedInbox();
    all.meta.state_filter = "all";
    expect(parseAiWorkPlanInbox(all, "all").meta.closed_total).toBe(1);
    const legacy = inboxFixture();
    expect(parseAiWorkPlanInbox(legacy, "attention").data).toHaveLength(1);
  });

  it("posts an explicit close and requires the closed revision back", async () => {
    vi.mocked(apiRequest).mockResolvedValue({
      data: { ...fixture(), closed_at: closedAt },
    });
    await expect(closeAiWorkPlan(session, 1)).resolves.toMatchObject({
      version: 1,
      closed_at: closedAt,
    });
    expect(apiRequest).toHaveBeenCalledWith(
      `/api/v1/ai/sessions/${session}/plan/close`,
      {
        method: "POST",
        body: JSON.stringify({ expected_version: 1, confirm_close: true }),
      },
    );
    vi.mocked(apiRequest).mockResolvedValue({ data: fixture() });
    await expect(closeAiWorkPlan(session, 1)).rejects.toThrow("计划响应无效");
    await expect(closeAiWorkPlan(session, 0)).rejects.toThrow("计划响应无效");
  });
});
