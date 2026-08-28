import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getSearchResults,
  normalizeSearchListResult,
  resetRuntimeConnection,
} from "./client";

const payload = {
  resource_type: "project",
  resource_id: "project-1",
  title: "Atlas 项目",
  subtitle: "Atlas 客户",
  matched_fields: ["name", "description"],
  route: "/projects/project-1",
  status: "in_progress",
  updated_at: "2026-08-28T10:00:00Z",
};

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

describe("unified search API contract", () => {
  it("strictly normalizes resource identity, matched fields, route and paging", () => {
    expect(
      normalizeSearchListResult({
        data: [payload],
        meta: { page: 1, page_size: 12, total: 1 },
      }),
    ).toEqual({
      items: [
        {
          resourceType: "project",
          resourceId: "project-1",
          title: "Atlas 项目",
          subtitle: "Atlas 客户",
          matchedFields: ["name", "description"],
          route: "/projects/project-1",
          status: "in_progress",
          updatedAt: "2026-08-28T10:00:00Z",
        },
      ],
      meta: { page: 1, pageSize: 12, total: 1 },
    });
  });

  it("rejects unknown types, extra fields and routes that do not locate the resource", () => {
    for (const invalid of [
      { ...payload, resource_type: "invoice" },
      { ...payload, route: "/projects/another" },
      { ...payload, unexpected: true },
      { ...payload, matched_fields: ["name", 1] },
      { ...payload, matched_fields: ["phone"] },
      { ...payload, status: "archived" },
      { ...payload, updated_at: "not-a-time" },
    ]) {
      expect(() =>
        normalizeSearchListResult({
          data: [invalid],
          meta: { page: 1, page_size: 12, total: 1 },
        }),
      ).toThrow(ApiError);
    }
  });

  it("serializes trimmed query, type filters and pagination", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [payload],
        meta: { page: 2, page_size: 5, total: 7 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getSearchResults({
      q: "  Atlas  ",
      types: ["project", "client"],
      page: 2,
      pageSize: 5,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/search");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      q: "Atlas",
      page: "2",
      page_size: "5",
      types: "project,client",
    });
  });
});
