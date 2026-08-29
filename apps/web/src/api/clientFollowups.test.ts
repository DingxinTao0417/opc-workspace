import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getClientFollowups,
  normalizeClientFollowup,
  resetRuntimeConnection,
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
});
