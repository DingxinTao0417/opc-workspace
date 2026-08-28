import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getProjectEvents,
  normalizeProjectEventListResult,
  normalizeProjectWorkflowEvent,
  resetRuntimeConnection,
} from "./client";

function eventPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "event-1",
    action: "project_started",
    actor: {
      id: "owner-1",
      type: "owner",
      display_name: "我",
      status: "active",
      is_builtin: true,
      version: 1,
    },
    request_id: "request-1",
    previous: { status: "planning", version: 1 },
    current: { status: "in_progress", version: 2 },
    created_at: "2026-08-28T10:00:00Z",
    ...overrides,
  };
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("project events API contract", () => {
  it("strictly normalizes actor, snapshots, request and timestamp", () => {
    expect(normalizeProjectWorkflowEvent(eventPayload())).toEqual({
      id: "event-1",
      action: "project_started",
      actor: {
        id: "owner-1",
        type: "owner",
        displayName: "我",
        status: "active",
        isBuiltin: true,
        version: 1,
      },
      requestId: "request-1",
      previous: { status: "planning", version: 1 },
      current: { status: "in_progress", version: 2 },
      createdAt: "2026-08-28T10:00:00Z",
    });
    expect(() =>
      normalizeProjectWorkflowEvent(eventPayload({ created_at: "not-time" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectWorkflowEvent(eventPayload({ actor: "owner" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectWorkflowEvent(eventPayload({ previous: [] })),
    ).toThrow(ApiError);
  });

  it("serializes pagination and requires project-version metadata", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [eventPayload()],
        meta: { page: 2, page_size: 5, total: 6, project_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getProjectEvents("project/1", {
      page: 2,
      pageSize: 5,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/projects/project%2F1/events");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 5,
      total: 6,
      projectVersion: 4,
    });
    expect(() =>
      normalizeProjectEventListResult({
        data: [],
        meta: { page: 1, page_size: 20, total: 0 },
      }),
    ).toThrow(ApiError);
  });

  it("rejects pagination that cannot contain the returned items", () => {
    expect(() =>
      normalizeProjectEventListResult({
        data: [eventPayload(), eventPayload({ id: "event-2" })],
        meta: { page: 1, page_size: 1, total: 2, project_version: 2 },
      }),
    ).toThrow(ApiError);
  });
});
