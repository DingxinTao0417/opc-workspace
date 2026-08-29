import { afterEach, describe, expect, it, vi } from "vitest";
import {
  deleteRoadmapMilestone,
  getRoadmapMilestone,
  resetRuntimeConnection,
  updateRoadmapMilestone,
} from "./client";

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function milestonePayload() {
  return {
    id: "milestone-1",
    title: "路线图交互收口",
    description: null,
    year: 2026,
    quarter: 3,
    target_date: "2026-09-30",
    status: "achieved",
    manual_order: 1024,
    archived_from_status: null,
    version: 5,
    created_at: "2026-08-29T00:00:00Z",
    updated_at: "2026-08-29T01:00:00Z",
    projects: [],
    task_summary: {
      total: 0,
      completed: 0,
      in_progress: 0,
      progress_percent: 0,
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("roadmap API contract", () => {
  it("loads and normalizes one milestone detail", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: milestonePayload() }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(getRoadmapMilestone("milestone-1")).resolves.toMatchObject({
      id: "milestone-1",
      title: "路线图交互收口",
      status: "achieved",
      version: 5,
      taskSummary: { total: 0, progressPercent: 0 },
    });
    expect(String(fetchMock.mock.calls[0][0])).toContain(
      "/api/v1/roadmap/milestones/milestone-1",
    );
  });

  it("serializes editable facts with optimistic concurrency", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: milestonePayload() }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await updateRoadmapMilestone("milestone-1", {
      title: "路线图交互收口",
      description: null,
      year: 2026,
      quarter: 3,
      targetDate: "2026-09-30",
      status: "achieved",
      projectIds: ["project-1"],
      expectedVersion: 4,
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/v1/roadmap/milestones/milestone-1");
    expect(init?.method).toBe("PATCH");
    expect(new Headers(init?.headers).get("If-Match")).toBe('"4"');
    expect(JSON.parse(String(init?.body))).toEqual({
      title: "路线图交互收口",
      description: null,
      year: 2026,
      quarter: 3,
      target_date: "2026-09-30",
      status: "achieved",
      project_ids: ["project-1"],
    });
  });

  it("uses both server confirmation and optimistic concurrency for deletion", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: { deleted_id: "milestone-1" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(deleteRoadmapMilestone("milestone-1", 5)).resolves.toEqual({
      deletedId: "milestone-1",
    });

    const [rawUrl, init] = fetchMock.mock.calls[0];
    const url = new URL(String(rawUrl), "http://local");
    expect(url.searchParams.get("confirm")).toBe("true");
    expect(init?.method).toBe("DELETE");
    expect(new Headers(init?.headers).get("If-Match")).toBe('"5"');
  });
});
