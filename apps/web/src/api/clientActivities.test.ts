import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createClientActivity,
  deleteClientActivity,
  getClientActivities,
  getClientActivity,
  getRecentClientActivities,
  normalizeClientActivity,
  normalizeRecentClientActivity,
  resetRuntimeConnection,
  updateClientActivity,
} from "./client";

function activityPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "activity-1",
    client_id: "client-1",
    kind: "note",
    title: "项目沟通",
    body: "确认下一步",
    occurred_at: "2026-08-28T08:00:00Z",
    created_by: {
      id: "owner-1",
      type: "owner",
      display_name: "Owner",
    },
    source_type: null,
    source_id: null,
    version: 2,
    deleted_at: null,
    deleted_by_actor_id: null,
    delete_reason: null,
    created_at: "2026-08-28T08:01:00Z",
    updated_at: "2026-08-28T08:02:00Z",
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

describe("client activity API contract", () => {
  it("strictly normalizes active, deleted, and system-reference facts", () => {
    expect(normalizeClientActivity(activityPayload())).toEqual({
      id: "activity-1",
      clientId: "client-1",
      kind: "note",
      title: "项目沟通",
      body: "确认下一步",
      occurredAt: "2026-08-28T08:00:00Z",
      createdBy: { id: "owner-1", type: "owner", displayName: "Owner" },
      sourceType: null,
      sourceId: null,
      version: 2,
      deletedAt: null,
      deletedByActorId: null,
      deleteReason: null,
      createdAt: "2026-08-28T08:01:00Z",
      updatedAt: "2026-08-28T08:02:00Z",
      clientVersion: 4,
    });
    expect(
      normalizeClientActivity(
        activityPayload({
          kind: "system_reference",
          body: null,
          source_type: "workflow_event",
          source_id: "event-1",
        }),
      ).kind,
    ).toBe("system_reference");
    expect(
      normalizeClientActivity(
        activityPayload({
          body: null,
          deleted_at: "2026-08-28T09:00:00Z",
          deleted_by_actor_id: "owner-1",
          delete_reason: "重复",
        }),
      ).deletedAt,
    ).toBe("2026-08-28T09:00:00Z");
    expect(() =>
      normalizeClientActivity(activityPayload({ kind: "email_opened" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeClientActivity(activityPayload({ body: null })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeClientActivity(
        activityPayload({ deleted_at: "2026-08-28T09:00:00Z" }),
      ),
    ).toThrow(ApiError);
  });

  it("serializes list filters and strict pagination metadata", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [activityPayload()],
        meta: { page: 2, page_size: 5, total: 6, client_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getClientActivities("client/1", {
      page: 2,
      pageSize: 5,
      kind: "meeting",
      includeDeleted: true,
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/clients/client%2F1/activities");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
      kind: "meeting",
      include_deleted: "true",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 5,
      total: 6,
      clientVersion: 4,
    });
  });

  it("loads and strictly validates the cross-client recent activity read model", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [
          activityPayload({
            client_name: "示例客户",
            client_status: "lead",
          }),
        ],
        meta: { page: 1, page_size: 3, total: 1 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getRecentClientActivities({
      pageSize: 3,
      kind: "meeting",
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/client-activities");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "1",
      page_size: "3",
      kind: "meeting",
    });
    expect(result.items[0]).toMatchObject({
      clientName: "示例客户",
      clientStatus: "lead",
      clientId: "client-1",
    });
    expect(result.meta).toEqual({ page: 1, pageSize: 3, total: 1 });
    expect(() =>
      normalizeRecentClientActivity(
        activityPayload({ client_name: "示例客户", client_status: "paused" }),
      ),
    ).toThrow(ApiError);
  });

  it("uses idempotency, If-Match, and confirmed soft deletion contracts", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) =>
        jsonResponse({
          data: activityPayload({
            version: init?.method === "POST" ? 1 : 3,
            ...(init?.method === "DELETE"
              ? {
                  body: null,
                  deleted_at: "2026-08-28T09:00:00Z",
                  deleted_by_actor_id: "owner-1",
                  delete_reason: "重复",
                }
              : {}),
          }),
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createClientActivity(
      "client-1",
      {
        kind: "meeting",
        title: "复盘",
        body: "确认结论",
        occurredAt: "2026-08-28T08:00:00Z",
      },
      "activity-key",
    );
    await getClientActivity("activity-1");
    await updateClientActivity("activity-1", {
      body: "更新结论",
      expectedVersion: 2,
    });
    await deleteClientActivity("activity-1", {
      reason: "重复",
      expectedVersion: 3,
    });

    const createInit = fetchMock.mock.calls[0][1];
    const updateInit = fetchMock.mock.calls[2][1];
    const deleteUrl = new URL(
      String(fetchMock.mock.calls[3][0]),
      "http://local",
    );
    const deleteInit = fetchMock.mock.calls[3][1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "activity-key",
    );
    expect(new Headers(updateInit?.headers).get("If-Match")).toBe('"2"');
    expect(deleteUrl.searchParams.get("confirm")).toBe("true");
    expect(new Headers(deleteInit?.headers).get("If-Match")).toBe('"3"');
    expect(JSON.parse(String(deleteInit?.body))).toEqual({ reason: "重复" });
  });
});
