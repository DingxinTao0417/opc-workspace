import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createClientActorLink,
  deleteClientActorLink,
  getClientActorLinks,
  normalizeClientActorLink,
  resetRuntimeConnection,
} from "./client";

function linkPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "link-1",
    client_id: "client-1",
    role: "contact",
    actor: {
      id: "actor-1",
      type: "person",
      display_name: "陶先生",
      status: "active",
      version: 2,
    },
    linked_by: { id: "owner-1", type: "owner", display_name: "Owner" },
    linked_at: "2026-08-28T08:00:00Z",
    unlinked_at: null,
    unlinked_by: null,
    unlink_reason: null,
    client_version: 4,
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

describe("client actor link API contract", () => {
  it("strictly normalizes active and historical relationship facts", () => {
    expect(normalizeClientActorLink(linkPayload())).toEqual({
      id: "link-1",
      clientId: "client-1",
      role: "contact",
      actor: {
        id: "actor-1",
        type: "person",
        displayName: "陶先生",
        status: "active",
        version: 2,
      },
      linkedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
      linkedAt: "2026-08-28T08:00:00Z",
      unlinkedAt: null,
      unlinkedBy: null,
      unlinkReason: null,
      clientVersion: 4,
    });
    expect(
      normalizeClientActorLink(
        linkPayload({
          unlinked_at: "2026-08-28T09:00:00Z",
          unlinked_by: {
            id: "owner-1",
            type: "owner",
            display_name: "Owner",
          },
          unlink_reason: "联系人已变更",
          client_version: 5,
        }),
      ).unlinkReason,
    ).toBe("联系人已变更");
    expect(() =>
      normalizeClientActorLink(linkPayload({ role: "owner" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeClientActorLink(
        linkPayload({ unlinked_at: "2026-08-28T09:00:00Z" }),
      ),
    ).toThrow(ApiError);
  });

  it("serializes the unlinked state and validates aggregate metadata", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [
          linkPayload({
            unlinked_at: "2026-08-28T09:00:00Z",
            unlinked_by: {
              id: "owner-1",
              type: "owner",
              display_name: "Owner",
            },
            unlink_reason: "联系人已变更",
          }),
        ],
        meta: { page: 2, page_size: 5, total: 6, client_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const result = await getClientActorLinks("client-1", {
      page: 2,
      pageSize: 5,
      state: "unlinked",
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
      state: "unlinked",
    });
    expect(result.meta.clientVersion).toBe(4);
  });

  it("keeps the legacy all-state query compatible", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [linkPayload()],
        meta: { page: 1, page_size: 20, total: 1, client_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getClientActorLinks("client-1", { includeUnlinked: true });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.searchParams.get("include_unlinked")).toBe("true");
    expect(url.searchParams.has("state")).toBe(false);
  });

  it.each([
    {
      name: "a mismatched page",
      input: { state: "active" as const, page: 2, pageSize: 1 },
      data: [linkPayload()],
      meta: { page: 1, page_size: 1, total: 1, client_version: 4 },
    },
    {
      name: "an underfilled page",
      input: { state: "unlinked" as const, page: 1, pageSize: 6 },
      data: [],
      meta: { page: 1, page_size: 6, total: 5, client_version: 4 },
    },
    {
      name: "duplicate relationship ids",
      input: { state: "all" as const, page: 1, pageSize: 2 },
      data: [linkPayload(), linkPayload()],
      meta: { page: 1, page_size: 2, total: 2, client_version: 4 },
    },
    {
      name: "an active row in unlinked history",
      input: { state: "unlinked" as const, page: 1, pageSize: 1 },
      data: [linkPayload()],
      meta: { page: 1, page_size: 1, total: 1, client_version: 4 },
    },
    {
      name: "multiple active contacts",
      input: { state: "active" as const, page: 1, pageSize: 2 },
      data: [linkPayload()],
      meta: { page: 1, page_size: 2, total: 2, client_version: 4 },
    },
    {
      name: "a historical row in the active view",
      input: { state: "active" as const, page: 1, pageSize: 1 },
      data: [
        linkPayload({
          unlinked_at: "2026-08-28T09:00:00Z",
          unlinked_by: {
            id: "owner-1",
            type: "owner",
            display_name: "Owner",
          },
          unlink_reason: "联系人已变更",
        }),
      ],
      meta: { page: 1, page_size: 1, total: 1, client_version: 4 },
    },
    {
      name: "a stale item client version",
      input: { state: "all" as const, page: 1, pageSize: 1 },
      data: [linkPayload({ client_version: 3 })],
      meta: { page: 1, page_size: 1, total: 1, client_version: 4 },
    },
  ])("rejects $name", async ({ input, data, meta }) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => jsonResponse({ data, meta })),
    );

    await expect(getClientActorLinks("client-1", input)).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
  });

  it("rejects conflicting filters before requesting the Sidecar", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      getClientActorLinks("client-1", {
        state: "active",
        includeUnlinked: true,
      } as never),
    ).rejects.toMatchObject({ code: "INVALID_FILTER" });
    await expect(
      getClientActorLinks("client-1", { state: "invalid" } as never),
    ).rejects.toMatchObject({ code: "INVALID_FILTER" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("forwards request cancellation to the active list request", async () => {
    const controller = new AbortController();
    let requestSignal: AbortSignal | undefined;
    let markStarted: (() => void) | undefined;
    const started = new Promise<void>((resolve) => {
      markStarted = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        requestSignal = init?.signal ?? undefined;
        markStarted?.();
        return new Promise<Response>((_resolve, reject) => {
          requestSignal?.addEventListener(
            "abort",
            () => reject(new DOMException("Aborted", "AbortError")),
            { once: true },
          );
        });
      }),
    );

    const request = getClientActorLinks(
      "client-1",
      { state: "active", page: 1, pageSize: 1 },
      controller.signal,
    );
    await started;
    expect(requestSignal?.aborted).toBe(false);

    controller.abort();
    expect(requestSignal?.aborted).toBe(true);
    await expect(request).rejects.toMatchObject({ code: "TIMEOUT" });
  });

  it("uses Client If-Match and idempotency for link and confirmed unlink", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) =>
        jsonResponse({
          data: linkPayload(
            init?.method === "DELETE"
              ? {
                  unlinked_at: "2026-08-28T09:00:00Z",
                  unlinked_by: {
                    id: "owner-1",
                    type: "owner",
                    display_name: "Owner",
                  },
                  unlink_reason: "联系人已变更",
                  client_version: 5,
                }
              : {},
          ),
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await createClientActorLink(
      "client-1",
      { actorId: "actor-1", expectedVersion: 3 },
      "link-key",
    );
    await createClientActorLink(
      "client-1",
      {
        createPerson: { displayName: "陶先生", notes: "客户联系人" },
        expectedVersion: 4,
      },
      "create-person-key",
    );
    await deleteClientActorLink(
      "link-1",
      { reason: "联系人已变更", expectedVersion: 4 },
      "unlink-key",
    );

    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      role: "contact",
      actor_id: "actor-1",
    });
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      role: "contact",
      create_person: { display_name: "陶先生", notes: "客户联系人" },
    });
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("If-Match"),
    ).toBe('"3"');
    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("link-key");
    const deleteUrl = new URL(
      String(fetchMock.mock.calls[2][0]),
      "http://local",
    );
    expect(deleteUrl.searchParams.get("confirm")).toBe("true");
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"4"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("Idempotency-Key"),
    ).toBe("unlink-key");
  });
});
