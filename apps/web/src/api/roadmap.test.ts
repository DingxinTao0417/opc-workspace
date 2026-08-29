import { afterEach, describe, expect, it, vi } from "vitest";
import {
  deleteRoadmapMilestone,
  getRoadmapMilestone,
  getRoadmapMilestones,
  reorderRoadmapMilestones,
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
  it("serializes the target-date list order used by overview reads", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({
          data: [milestonePayload()],
          meta: { page: 1, page_size: 3, total: 1 },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      getRoadmapMilestones({
        page: 1,
        pageSize: 3,
        sort: "target_date",
        status: "active",
      }),
    ).resolves.toMatchObject({
      items: [{ id: "milestone-1" }],
      meta: { page: 1, pageSize: 3, total: 1 },
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.searchParams.get("sort")).toBe("target_date");
    expect(url.searchParams.get("status")).toBe("active");
    expect(url.searchParams.get("page_size")).toBe("3");
  });

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

  it("serializes and normalizes an atomic milestone reorder", async () => {
    const second = {
      ...milestonePayload(),
      id: "milestone-2",
      title: "第二个里程碑",
      manual_order: 2048,
      version: 3,
    };
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: [second, milestonePayload()] }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      reorderRoadmapMilestones({
        items: [
          { id: "milestone-2", expectedVersion: 3 },
          { id: "milestone-1", expectedVersion: 5 },
        ],
      }),
    ).resolves.toMatchObject([
      { id: "milestone-2", manualOrder: 2048, version: 3 },
      { id: "milestone-1", manualOrder: 1024, version: 5 },
    ]);

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/api/v1/roadmap/milestones/reorder");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(String(init?.body))).toEqual({
      items: [
        { id: "milestone-2", expected_version: 3 },
        { id: "milestone-1", expected_version: 5 },
      ],
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
