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

const extendedPayloads = [
  {
    resource_type: "invoice",
    resource_id: "invoice-1",
    title: "INV-2026-001",
    subtitle: "Atlas 客户",
    matched_fields: ["invoice_number", "client_name"],
    route: "/invoices/invoice-1",
    status: "sent",
    updated_at: "2026-08-28T11:00:00Z",
  },
  {
    resource_type: "roadmap_milestone",
    resource_id: "milestone-1",
    title: "第一版交付",
    subtitle: "Atlas 项目",
    matched_fields: ["title", "description", "project_names"],
    route: "/roadmap?milestone=milestone-1",
    status: "active",
    updated_at: "2026-08-28T12:00:00Z",
  },
  {
    resource_type: "content_item",
    resource_id: "content-1",
    title: "版本发布说明",
    subtitle: "微信公众号",
    matched_fields: ["title", "notes", "platform"],
    route: "/content-calendar?item=content-1",
    status: "scheduled",
    updated_at: "2026-08-28T13:00:00Z",
  },
] as const;

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

  it("strictly normalizes invoice, roadmap milestone and content item results", () => {
    expect(
      normalizeSearchListResult({
        data: extendedPayloads,
        meta: { page: 1, page_size: 12, total: 3 },
      }).items,
    ).toEqual([
      {
        resourceType: "invoice",
        resourceId: "invoice-1",
        title: "INV-2026-001",
        subtitle: "Atlas 客户",
        matchedFields: ["invoice_number", "client_name"],
        route: "/invoices/invoice-1",
        status: "sent",
        updatedAt: "2026-08-28T11:00:00Z",
      },
      {
        resourceType: "roadmap_milestone",
        resourceId: "milestone-1",
        title: "第一版交付",
        subtitle: "Atlas 项目",
        matchedFields: ["title", "description", "project_names"],
        route: "/roadmap?milestone=milestone-1",
        status: "active",
        updatedAt: "2026-08-28T12:00:00Z",
      },
      {
        resourceType: "content_item",
        resourceId: "content-1",
        title: "版本发布说明",
        subtitle: "微信公众号",
        matchedFields: ["title", "notes", "platform"],
        route: "/content-calendar?item=content-1",
        status: "scheduled",
        updatedAt: "2026-08-28T13:00:00Z",
      },
    ]);
  });

  it("rejects unknown types, extra fields and routes that do not locate the resource", () => {
    for (const invalid of [
      { ...payload, resource_type: "unknown" },
      { ...payload, route: "/projects/another" },
      { ...payload, unexpected: true },
      { ...payload, matched_fields: ["name", 1] },
      { ...payload, matched_fields: ["phone"] },
      { ...payload, status: "archived" },
      { ...payload, updated_at: "not-a-time" },
      { ...extendedPayloads[0], matched_fields: ["amount_minor"] },
      { ...extendedPayloads[1], route: "/roadmap/milestone-1" },
      { ...extendedPayloads[1], status: "archived" },
      { ...extendedPayloads[2], route: "/content-calendar/content-1" },
      { ...extendedPayloads[2], status: "archived" },
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
      types: ["project", "invoice", "content_item"],
      page: 2,
      pageSize: 5,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/search");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      q: "Atlas",
      page: "2",
      page_size: "5",
      types: "project,invoice,content_item",
    });
  });
});
