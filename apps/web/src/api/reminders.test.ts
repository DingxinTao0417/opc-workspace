import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cancelReminder,
  createReminder,
  getReminders,
  normalizeReminder,
  normalizeReminderListResult,
  resetRuntimeConnection,
  updateReminder,
} from "./client";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function reminderPayload(overrides: Record<string, unknown> = {}) {
  const id = "018f0000-0000-7000-8000-000000001501";
  return {
    id,
    source_entity_type: "manual",
    source_entity_id: null,
    title: "复查本地备份",
    summary: "确认恢复点可用",
    priority: "P1",
    trigger_at: "2099-08-30T01:00:00Z",
    status: "scheduled",
    source_event_key: `reminder:${id}:due`,
    created_by_actor_id: "00000000-0000-5000-8000-000000000001",
    series_id: id,
    recurrence_type: "none",
    recurrence_interval: 1,
    recurrence_timezone: "UTC",
    occurrence_number: 1,
    recurrence_anchor_day: 1,
    fired_at: null,
    inbox_item_id: null,
    cancelled_by_actor_id: null,
    cancelled_at: null,
    cancel_reason: null,
    version: 1,
    created_at: "2026-08-28T10:00:00Z",
    updated_at: "2026-08-28T10:00:00Z",
    available_actions: ["edit", "cancel"],
    ...overrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("Reminder API contract", () => {
  it("strictly normalizes each lifecycle state", () => {
    expect(normalizeReminder(reminderPayload())).toMatchObject({
      title: "复查本地备份",
      status: "scheduled",
      priority: "P1",
      version: 1,
      recurrenceType: "none",
      recurrenceInterval: 1,
      availableActions: ["edit", "cancel"],
    });
    expect(
      normalizeReminder(
        reminderPayload({
          status: "fired",
          fired_at: "2099-08-30T01:00:01Z",
          inbox_item_id: "018f0000-0000-7000-8000-000000001502",
          available_actions: [],
        }),
      ),
    ).toMatchObject({ status: "fired", availableActions: [] });
    expect(
      normalizeReminder(
        reminderPayload({
          status: "cancelled",
          cancelled_by_actor_id: "00000000-0000-5000-8000-000000000001",
          cancelled_at: "2026-08-28T11:00:00Z",
          cancel_reason: "计划取消",
          available_actions: [],
        }),
      ),
    ).toMatchObject({ status: "cancelled", cancelReason: "计划取消" });
    expect(() =>
      normalizeReminder(reminderPayload({ status: "fired" })),
    ).toThrow();
    expect(() =>
      normalizeReminder(reminderPayload({ available_actions: ["delete"] })),
    ).toThrow();
    expect(
      normalizeReminder(
        reminderPayload({
          recurrence_type: "monthly",
          recurrence_timezone: "America/Los_Angeles",
          recurrence_anchor_day: 31,
        }),
      ),
    ).toMatchObject({ recurrenceType: "monthly", recurrenceAnchorDay: 31 });
    expect(() =>
      normalizeReminder(reminderPayload({ recurrence_anchor_day: 31 })),
    ).toThrow();
    expect(
      normalizeReminder(
        reminderPayload({
          recurrence_type: "weekdays",
          recurrence_timezone: "Asia/Shanghai",
        }),
      ),
    ).toMatchObject({ recurrenceType: "weekdays", recurrenceAnchorDay: 1 });
  });

  it("serializes filters and validates paging metadata", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({
          data: [reminderPayload()],
          meta: {
            page: 2,
            page_size: 20,
            total: 21,
            server_now: "2026-08-28T10:05:00Z",
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getReminders({
      status: "scheduled",
      q: " 备份 ",
      sort: "trigger_at",
      page: 2,
      pageSize: 20,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "20",
      status: "scheduled",
      q: "备份",
      sort: "trigger_at",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 20,
      total: 21,
      serverNow: "2026-08-28T10:05:00Z",
    });
    expect(() =>
      normalizeReminderListResult({
        data: [reminderPayload(), reminderPayload()],
        meta: { page: 1, page_size: 1, total: 2, server_now: "now" },
      }),
    ).toThrow();
  });

  it("uses idempotency and optimistic-lock headers for writes", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "DELETE") {
          return jsonResponse({
            data: reminderPayload({
              status: "cancelled",
              cancelled_by_actor_id: "00000000-0000-5000-8000-000000000001",
              cancelled_at: "2026-08-28T11:00:00Z",
              cancel_reason: "计划取消",
              version: 3,
              available_actions: [],
            }),
          });
        }
        return jsonResponse(
          {
            data: reminderPayload({
              version: init?.method === "PATCH" ? 2 : 1,
            }),
          },
          init?.method === "POST" ? 201 : 200,
        );
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    const id = String(reminderPayload().id);

    await createReminder(
      {
        title: "复查本地备份",
        summary: "确认恢复点可用",
        priority: "P1",
        triggerAt: "2099-08-30T01:00:00Z",
        recurrenceType: "daily",
        recurrenceInterval: 2,
        recurrenceTimezone: "Asia/Shanghai",
      },
      "reminder-create-1",
    );
    await updateReminder(id, {
      title: "复查本地恢复点",
      triggerAt: "2099-09-01T02:30:00Z",
      recurrenceType: "weekly",
      recurrenceInterval: 3,
      recurrenceTimezone: "America/Los_Angeles",
      expectedVersion: 1,
    });
    await cancelReminder(
      { id, reason: "计划取消", expectedVersion: 2 },
      "reminder-cancel-1",
    );

    expect(
      new Headers(fetchMock.mock.calls[0][1]?.headers).get("Idempotency-Key"),
    ).toBe("reminder-create-1");
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({
      title: "复查本地备份",
      summary: "确认恢复点可用",
      priority: "P1",
      trigger_at: "2099-08-30T01:00:00Z",
      recurrence_type: "daily",
      recurrence_interval: 2,
      recurrence_timezone: "Asia/Shanghai",
    });
    expect(
      new Headers(fetchMock.mock.calls[1][1]?.headers).get("If-Match"),
    ).toBe('"1"');
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({
      title: "复查本地恢复点",
      trigger_at: "2099-09-01T02:30:00Z",
      recurrence_type: "weekly",
      recurrence_interval: 3,
      recurrence_timezone: "America/Los_Angeles",
    });
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("If-Match"),
    ).toBe('"2"');
    expect(
      new Headers(fetchMock.mock.calls[2][1]?.headers).get("Idempotency-Key"),
    ).toBe("reminder-cancel-1");
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({
      reason: "计划取消",
    });
  });
});
