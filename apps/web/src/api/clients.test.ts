import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createClient,
  deleteClient,
  getClients,
  getProjects,
  normalizeClient,
  resetRuntimeConnection,
  updateClient,
} from "./client";

function clientPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "client-1",
    name: "星河工作室",
    contact_name: "陶先生",
    email: "hello@example.com",
    phone: "13800000000",
    notes: "品牌设计客户",
    status: "active",
    version: 3,
    project_count: 2,
    latest_activity_at: null,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: "2026-08-27T00:00:00Z",
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

describe("client API contract", () => {
  it("strictly normalizes client facts", () => {
    expect(normalizeClient(clientPayload())).toEqual({
      id: "client-1",
      name: "星河工作室",
      contactName: "陶先生",
      email: "hello@example.com",
      phone: "13800000000",
      notes: "品牌设计客户",
      status: "active",
      version: 3,
      projectCount: 2,
      latestActivityAt: null,
      createdAt: "2026-08-20T00:00:00Z",
      updatedAt: "2026-08-27T00:00:00Z",
    });
    expect(() => normalizeClient(clientPayload({ status: "unknown" }))).toThrow(
      ApiError,
    );
    expect(() => normalizeClient(clientPayload({ version: 0 }))).toThrow(
      ApiError,
    );
    expect(() => normalizeClient(clientPayload({ project_count: -1 }))).toThrow(
      ApiError,
    );
    expect(() =>
      normalizeClient(clientPayload({ contact_name: undefined })),
    ).toThrow(ApiError);
  });

  it("serializes list paging, search, status, and sort", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [clientPayload()],
        meta: { page: 2, page_size: 20, total: 21 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getClients({
      page: 2,
      pageSize: 20,
      q: "  星河  ",
      status: "active",
      sort: "-updated_at",
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "20",
      q: "星河",
      status: "active",
      sort: "-updated_at",
    });
    expect(result.meta).toEqual({ page: 2, pageSize: 20, total: 21 });
  });

  it("requests archived projects when a client detail needs the full relation", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [],
        meta: { page: 1, page_size: 8, total: 0 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getProjects({
      page: 1,
      pageSize: 8,
      clientId: "client-1",
      includeArchived: true,
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.searchParams.get("client_id")).toBe("client-1");
    expect(url.searchParams.get("include_archived")).toBe("true");
  });

  it("uses stable-write headers and explicit confirmed deletion", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = new URL(String(input), "http://local");
        if (init?.method === "DELETE") {
          return jsonResponse({
            data: { deleted_id: "client-1", detached_projects: 2 },
          });
        }
        return jsonResponse({
          data: clientPayload({ version: init?.method === "PATCH" ? 4 : 1 }),
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await createClient(
      {
        name: "星河工作室",
        contactName: null,
        email: null,
        phone: null,
        notes: null,
        status: "lead",
      },
      "client-create-key",
    );
    await updateClient("client-1", {
      status: "inactive",
      expectedVersion: 3,
    });
    await expect(deleteClient("client-1", 4)).resolves.toEqual({
      deletedId: "client-1",
      detachedProjects: 2,
    });

    const createInit = fetchMock.mock.calls[0][1];
    const updateInit = fetchMock.mock.calls[1][1];
    const [deleteUrl, deleteInit] = fetchMock.mock.calls[2];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "client-create-key",
    );
    expect(JSON.parse(String(createInit?.body))).toMatchObject({
      contact_name: null,
      status: "lead",
    });
    expect(new Headers(updateInit?.headers).get("If-Match")).toBe('"3"');
    expect(JSON.parse(String(updateInit?.body))).toEqual({
      status: "inactive",
    });
    expect(
      new URL(String(deleteUrl), "http://local").searchParams.get("confirm"),
    ).toBe("true");
    expect(new Headers(deleteInit?.headers).get("If-Match")).toBe('"4"');
  });
});
