import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createInboxItem,
  executeInboxItemCommand,
  getInboxItemEvents,
  getInboxItems,
  markAllInboxItemsRead,
  normalizeInboxItem,
  resetRuntimeConnection,
  updateInboxItem,
} from "./client";

function inboxPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000000801",
    kind: "manual",
    title: "确认本周交付范围",
    summary: "整理需要人工确认的边界",
    source_entity_type: "manual",
    source_entity_id: null,
    source_event_key: null,
    source_deleted_at: null,
    priority: "P1",
    status: "open",
    resolution_policy: "manual",
    due_at: "2026-08-29T12:00:00Z",
    read_at: null,
    triaged_at: "2026-08-28T10:00:00Z",
    snoozed_until: null,
    resolved_by_actor_id: null,
    resolved_at: null,
    resolution_reason: null,
    resolution_mode: null,
    dismissed_by_actor_id: null,
    dismissed_at: null,
    dismiss_reason: null,
    payload_json: {},
    version: 2,
    created_at: "2026-08-28T10:00:00Z",
    updated_at: "2026-08-28T10:00:00Z",
    available_actions: ["edit", "read", "snooze", "resolve", "dismiss"],
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

describe("inbox API contract", () => {
  it("strictly normalizes manual item facts and executable actions", () => {
    expect(normalizeInboxItem(inboxPayload())).toMatchObject({
      id: "018f0000-0000-7000-8000-000000000801",
      kind: "manual",
      title: "确认本周交付范围",
      sourceEntityType: "manual",
      priority: "P1",
      status: "open",
      resolutionPolicy: "manual",
      version: 2,
      availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
    });
    expect(() =>
      normalizeInboxItem(inboxPayload({ kind: "notification" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem(inboxPayload({ priority: "urgent" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeInboxItem(inboxPayload({ payload_json: [] })),
    ).toThrow(ApiError);
  });

  it("serializes view, filters, paging, and snapshot metadata", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({
          data: [inboxPayload()],
          meta: {
            page: 2,
            page_size: 20,
            total: 21,
            unread_total: 4,
            snapshot_at: "2026-08-28T10:05:00.123456789Z",
            server_now: "2026-08-28T10:05:01.123456789Z",
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getInboxItems({
      view: "snoozed",
      q: " 交付 ",
      priority: "P1",
      page: 2,
      pageSize: 20,
    });

    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      view: "snoozed",
      page: "2",
      page_size: "20",
      q: "交付",
      priority: "P1",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 20,
      total: 21,
      unreadTotal: 4,
      snapshotAt: "2026-08-28T10:05:00.123456789Z",
      serverNow: "2026-08-28T10:05:01.123456789Z",
    });
  });

  it("keeps server-owned manual facts out of create and uses stable-write headers", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({ data: inboxPayload() }, 201),
    );
    vi.stubGlobal("fetch", fetchMock);
    const id = "018f0000-0000-7000-8000-000000000801";

    await createInboxItem(
      {
        title: "确认本周交付范围",
        summary: "整理需要人工确认的边界",
        priority: "P1",
        dueAt: null,
      },
      "inbox-create-1",
    );
    await updateInboxItem(id, {
      title: "确认最终交付范围",
      expectedVersion: 2,
    });
    await executeInboxItemCommand(
      {
        id,
        action: "snooze",
        snoozedUntil: "2026-08-29T12:00:00Z",
        expectedVersion: 3,
      },
      "inbox-snooze-1",
    );

    const createInit = fetchMock.mock.calls[0][1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "inbox-create-1",
    );
    expect(JSON.parse(String(createInit?.body))).toEqual({
      title: "确认本周交付范围",
      summary: "整理需要人工确认的边界",
      priority: "P1",
      due_at: null,
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"2"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"3"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("Idempotency-Key"),
    ).toBe("inbox-snooze-1");
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      snoozed_until: "2026-08-29T12:00:00Z",
    });
  });

  it("uses the exact list snapshot for read-all and normalizes the actor timeline", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, _init?: RequestInit) => {
        if (String(input).endsWith("/events?page=1&page_size=20")) {
          return jsonResponse({
            data: [
              {
                id: "018f0000-0000-7000-8000-000000000901",
                action: "created",
                actor_id: "00000000-0000-0000-0000-000000000001",
                actor: {
                  id: "00000000-0000-0000-0000-000000000001",
                  type: "owner",
                  display_name: "陶定鑫",
                  status: "active",
                  is_builtin: true,
                  version: 1,
                },
                request_id: "request-1",
                previous: null,
                current: { status: "open", reason: "手工创建" },
                reason: "手工创建",
                created_at: "2026-08-28T10:00:00Z",
              },
            ],
            meta: {
              page: 1,
              page_size: 20,
              total: 1,
              inbox_item_version: 2,
            },
          });
        }
        return jsonResponse({
          data: {
            through_created_at: "2026-08-28T10:05:00.123456789Z",
            marked_count: 3,
          },
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      markAllInboxItemsRead(
        "2026-08-28T10:05:00.123456789Z",
        "inbox-read-all-1",
      ),
    ).resolves.toEqual({
      throughCreatedAt: "2026-08-28T10:05:00.123456789Z",
      markedCount: 3,
    });
    const events = await getInboxItemEvents(
      "018f0000-0000-7000-8000-000000000801",
      { page: 1, pageSize: 20 },
    );

    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      through_created_at: "2026-08-28T10:05:00.123456789Z",
    });
    expect(events.items[0].actor?.displayName).toBe("陶定鑫");
    expect(events.items[0].reason).toBe("手工创建");
    expect(events.meta.inboxItemVersion).toBe(2);
  });
});
