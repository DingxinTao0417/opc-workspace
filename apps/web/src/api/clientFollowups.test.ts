import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  cancelClientFollowup,
  completeClientFollowup,
  createClientFollowup,
  getClientFollowups,
  normalizeClientFollowup,
  resetRuntimeConnection,
  rescheduleClientFollowup,
  skipClientFollowup,
  updateClientFollowup,
} from "./client";

function followupPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "followup-1",
    client_id: "client-1",
    client_name: "星河工作室",
    assigned_actor_id: "owner-1",
    assigned_actor_name: "陶先生",
    assigned_actor_type: "owner",
    scheduled_at: "2026-08-30T08:00:00Z",
    timezone: "Asia/Shanghai",
    channel: "微信",
    purpose: "确认交付反馈",
    notes: "询问下一阶段需求",
    status: "planned",
    priority: "high",
    completed_at: null,
    result: null,
    next_step: null,
    skipped_at: null,
    skip_reason: null,
    cancelled_at: null,
    cancel_reason: null,
    rescheduled_from_id: null,
    version: 2,
    created_at: "2026-08-28T08:00:00Z",
    updated_at: "2026-08-28T08:01:00Z",
    client_version: 4,
    ...overrides,
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("client followup API contract", () => {
  it("strictly normalizes scheduled followup facts", () => {
    expect(normalizeClientFollowup(followupPayload())).toEqual({
      id: "followup-1",
      clientId: "client-1",
      clientName: "星河工作室",
      assignedActorId: "owner-1",
      assignedActorName: "陶先生",
      assignedActorType: "owner",
      scheduledAt: "2026-08-30T08:00:00Z",
      timezone: "Asia/Shanghai",
      channel: "微信",
      purpose: "确认交付反馈",
      notes: "询问下一阶段需求",
      status: "planned",
      priority: "high",
      completedAt: null,
      result: null,
      nextStep: null,
      skippedAt: null,
      skipReason: null,
      cancelledAt: null,
      cancelReason: null,
      rescheduledFromId: null,
      version: 2,
      createdAt: "2026-08-28T08:00:00Z",
      updatedAt: "2026-08-28T08:01:00Z",
      clientVersion: 4,
    });
    expect(() =>
      normalizeClientFollowup(followupPayload({ status: "overdue" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeClientFollowup(
        followupPayload({ assigned_actor_type: "agent" }),
      ),
    ).toThrow(ApiError);
  });

  it("serializes client-scoped pagination and supported filters", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [followupPayload()],
        meta: { page: 2, page_size: 5, total: 6 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getClientFollowups("client/1", {
      page: 2,
      pageSize: 5,
      status: "planned",
      assignedActorId: " owner-1 ",
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/clients/client%2F1/followups");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
      status: "planned",
      assigned_actor_id: "owner-1",
    });
    expect(result.meta).toEqual({ page: 2, pageSize: 5, total: 6 });
  });

  it("uses idempotency and versioned commands for the complete local workflow", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(_input);
        if (url.endsWith("/reschedule")) {
          return jsonResponse({
            data: {
              previous: followupPayload({
                status: "cancelled",
                cancelled_at: "2026-08-29T08:00:00Z",
                cancel_reason: "改期",
                version: 3,
              }),
              next: followupPayload({ id: "followup-2", version: 1 }),
            },
          });
        }
        return jsonResponse({
          data: followupPayload({
            status: url.endsWith("/complete")
              ? "completed"
              : url.includes("/skip")
                ? "skipped"
                : url.includes("?confirm=true")
                  ? "cancelled"
                  : "planned",
            completed_at: url.endsWith("/complete")
              ? "2026-08-29T08:00:00Z"
              : null,
            result: url.endsWith("/complete") ? "已确认" : null,
            skipped_at: url.includes("/skip") ? "2026-08-29T08:00:00Z" : null,
            skip_reason: url.includes("/skip") ? "无需继续" : null,
            cancelled_at: url.includes("?confirm=true")
              ? "2026-08-29T08:00:00Z"
              : null,
            cancel_reason: url.includes("?confirm=true") ? "取消" : null,
            version: init?.method === "POST" ? 3 : 2,
          }),
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    const plan = {
      assignedActorId: "owner-1",
      scheduledAt: "2026-08-30T08:00:00Z",
      timezone: "Asia/Shanghai",
      channel: "微信",
      purpose: "确认交付反馈",
      notes: null,
      priority: "normal" as const,
    };
    await createClientFollowup(
      { clientId: "client-1", ...plan },
      "followup-key",
    );
    await updateClientFollowup("followup-1", {
      ...plan,
      expectedVersion: 2,
    });
    await completeClientFollowup("followup-1", {
      result: "已确认",
      nextStep: null,
      completedAt: "2026-08-29T08:00:00Z",
      expectedVersion: 2,
    });
    await skipClientFollowup("followup-1", {
      reason: "无需继续",
      expectedVersion: 2,
    });
    await cancelClientFollowup("followup-1", {
      reason: "取消",
      expectedVersion: 2,
    });
    const rescheduled = await rescheduleClientFollowup("followup-1", {
      ...plan,
      reason: "改期",
      expectedVersion: 2,
    });

    expect(rescheduled.next.id).toBe("followup-2");
    const createInit = fetchMock.mock.calls[0][1];
    const updateInit = fetchMock.mock.calls[1][1];
    const completeInit = fetchMock.mock.calls[2][1];
    const skipInit = fetchMock.mock.calls[3][1];
    const cancelUrl = new URL(
      String(fetchMock.mock.calls[4][0]),
      "http://local",
    );
    const cancelInit = fetchMock.mock.calls[4][1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "followup-key",
    );
    expect(new Headers(updateInit?.headers).get("If-Match")).toBe('"2"');
    expect(new Headers(completeInit?.headers).get("If-Match")).toBe('"2"');
    expect(new Headers(skipInit?.headers).get("If-Match")).toBe('"2"');
    expect(cancelUrl.searchParams.get("confirm")).toBe("true");
    expect(new Headers(cancelInit?.headers).get("If-Match")).toBe('"2"');
    expect(JSON.parse(String(cancelInit?.body))).toEqual({ reason: "取消" });
  });
});
