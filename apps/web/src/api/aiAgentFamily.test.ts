import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cancelAiAgentChildGenerations,
  getAiAgentFamily,
} from "./aiAgentFamily";

const id = (suffix: number) =>
  `018f0000-0000-7000-8000-${String(suffix).padStart(12, "0")}`;

const child = {
  session_id: id(2),
  proposal_id: id(3),
  task_name: "fact_check",
  status: "completed",
  error_code: null,
  available: true,
  pending_approvals: 1,
  active_generation_id: null,
  created_at: "2026-09-22T10:00:00Z",
  decided_at: "2026-09-22T10:01:00Z",
};

function response(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AI agent family", () => {
  it("posts exact batch stop identities and validates the ordered acceptance receipt", async () => {
    const targets = [
      { childSessionId: id(2), generationId: id(3) },
      { childSessionId: id(4), generationId: id(5) },
    ];
    const fetchMock = vi.fn(async () =>
      response({
        parent_session_id: id(1),
        items: targets.map((target) => ({
          child_session_id: target.childSessionId,
          generation_id: target.generationId,
          cancel_requested: true,
        })),
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      cancelAiAgentChildGenerations(id(1), targets),
    ).resolves.toEqual({
      parentSessionId: id(1),
      items: targets.map((target) => ({ ...target, cancelRequested: true })),
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(
        `/api/v1/ai/sessions/${id(1)}/agent-children/cancel`,
      ),
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          items: targets.map((target) => ({
            child_session_id: target.childSessionId,
            generation_id: target.generationId,
          })),
        }),
      }),
    );
  });

  it("rejects invalid or duplicate batch targets before network I/O", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const target = { childSessionId: id(2), generationId: id(3) };
    for (const targets of [
      [],
      Array.from({ length: 5 }, (_, index) => ({
        childSessionId: id(index + 10),
        generationId: id(index + 20),
      })),
      [target, target],
      [target, { childSessionId: id(4), generationId: target.generationId }],
    ]) {
      await expect(
        cancelAiAgentChildGenerations(id(1), targets),
      ).rejects.toThrow();
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects a partial or reordered stop acceptance receipt", async () => {
    const targets = [
      { childSessionId: id(2), generationId: id(3) },
      { childSessionId: id(4), generationId: id(5) },
    ];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          parent_session_id: id(1),
          items: [
            {
              child_session_id: targets[1].childSessionId,
              generation_id: targets[1].generationId,
              cancel_requested: true,
            },
            {
              child_session_id: targets[0].childSessionId,
              generation_id: targets[0].generationId,
              cancel_requested: true,
            },
          ],
        }),
      ),
    );
    await expect(
      cancelAiAgentChildGenerations(id(1), targets),
    ).rejects.toThrow();
  });

  it("parses a metadata-only parent and child navigation projection", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          session_id: id(1),
          parent: null,
          children: [child],
          as_of: "2026-09-22T10:02:00Z",
        }),
      ),
    );

    await expect(getAiAgentFamily(id(1))).resolves.toMatchObject({
      sessionId: id(1),
      parent: null,
      children: [
        { sessionId: id(2), taskName: "fact_check", pendingApprovals: 1 },
      ],
    });
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining(`/api/v1/ai/sessions/${id(1)}/agent-family`),
      expect.any(Object),
    );
  });

  it("keeps an unavailable child as a non-navigable tombstone", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          session_id: id(1),
          parent: null,
          children: [
            {
              ...child,
              status: "unavailable",
              available: false,
              pending_approvals: 0,
            },
          ],
          as_of: "2026-09-22T10:02:00Z",
        }),
      ),
    );

    await expect(getAiAgentFamily(id(1))).resolves.toMatchObject({
      children: [{ sessionId: id(2), available: false }],
    });
  });

  it.each([
    ["private message", { ...child, message: "secret" }],
    ["private scopes", { ...child, scopes: ["finance"] }],
    ["contradictory availability", { ...child, available: false }],
    ["negative pending approvals", { ...child, pending_approvals: -1 }],
    ["private approval body", { ...child, pending_action: "secret" }],
    ["invalid child identity", { ...child, session_id: id(1) }],
  ])("rejects %s", async (_label, member) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          session_id: id(1),
          parent: null,
          children: [member],
          as_of: "2026-09-22T10:02:00Z",
        }),
      ),
    );

    await expect(getAiAgentFamily(id(1))).rejects.toThrow();
  });

  it("rejects duplicate children, cross-session parent and oversized families", async () => {
    const cases = [
      { parent: null, children: [child, { ...child, proposal_id: id(4) }] },
      { parent: child, children: [child] },
      {
        parent: null,
        children: Array.from({ length: 5 }, (_, i) => ({
          ...child,
          session_id: id(i + 10),
          proposal_id: id(i + 20),
        })),
      },
    ];
    for (const candidate of cases) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () =>
          response({
            session_id: id(1),
            ...candidate,
            as_of: "2026-09-22T10:02:00Z",
          }),
        ),
      );
      await expect(getAiAgentFamily(id(1))).rejects.toThrow();
    }
  });

  it("rejects an invalid requested session before network I/O", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(getAiAgentFamily("not-a-uuid")).rejects.toThrow();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
