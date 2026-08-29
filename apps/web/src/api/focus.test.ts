import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  cancelFocusSession,
  createFocusSession,
  getActiveFocusSession,
  getFocusReport,
  getFocusSessions,
  getTodayStats,
  normalizeFocusSessionSnapshot,
  pauseFocusSession,
  recoverFocusSession,
  resetRuntimeConnection,
  stopFocusSession,
} from "./client";

function focusSession(overrides: Record<string, unknown> = {}) {
  return {
    id: "018f0000-0000-7000-8000-000000000901",
    task_id: "018f0000-0000-7000-8000-000000000101",
    task_title: "整理交付清单",
    status: "active",
    planned_seconds: 3000,
    accumulated_seconds: 120,
    started_at: "2026-08-28T10:00:00Z",
    ended_at: null,
    last_resumed_at: "2026-08-28T10:02:00Z",
    last_heartbeat_at: "2026-08-28T10:03:00Z",
    end_reason: null,
    version: 3,
    created_at: "2026-08-28T10:00:00Z",
    updated_at: "2026-08-28T10:02:00Z",
    ...overrides,
  };
}

function snapshotBody(session: unknown = focusSession()) {
  return {
    data: {
      session,
      server_now: "2026-08-28T10:04:00Z",
      elapsed_seconds: 240,
      remaining_seconds: 2760,
    },
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("focus API contract", () => {
  it("normalizes a versioned server-time snapshot", () => {
    vi.spyOn(Date, "now").mockReturnValue(123_456);
    expect(normalizeFocusSessionSnapshot(snapshotBody())).toEqual({
      session: {
        id: "018f0000-0000-7000-8000-000000000901",
        taskId: "018f0000-0000-7000-8000-000000000101",
        taskTitle: "整理交付清单",
        status: "active",
        plannedSeconds: 3000,
        accumulatedSeconds: 120,
        startedAt: "2026-08-28T10:00:00Z",
        endedAt: null,
        lastResumedAt: "2026-08-28T10:02:00Z",
        lastHeartbeatAt: "2026-08-28T10:03:00Z",
        endReason: null,
        version: 3,
        createdAt: "2026-08-28T10:00:00Z",
        updatedAt: "2026-08-28T10:02:00Z",
      },
      serverNow: "2026-08-28T10:04:00Z",
      receivedAtMs: 123_456,
    });
    expect(
      normalizeFocusSessionSnapshot(snapshotBody(null)).session,
    ).toBeNull();
    expect(() =>
      normalizeFocusSessionSnapshot(
        snapshotBody(focusSession({ status: "invented" })),
      ),
    ).toThrow(ApiError);
  });

  it("reads the single active session", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse(snapshotBody()),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getActiveFocusSession();
    expect(result.session?.id).toBe("018f0000-0000-7000-8000-000000000901");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/focus-sessions/active");
  });

  it("creates a session with one idempotency key and server field names", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse(snapshotBody(), 201),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createFocusSession(
      {
        taskId: "018f0000-0000-7000-8000-000000000101",
        plannedSeconds: 3000,
      },
      "focus-create-1",
    );

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/focus-sessions");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("Idempotency-Key")).toBe(
      "focus-create-1",
    );
    expect(JSON.parse(String(init?.body))).toEqual({
      task_id: "018f0000-0000-7000-8000-000000000101",
      planned_seconds: 3000,
    });
  });

  it("sends If-Match and idempotency headers on state commands", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse(snapshotBody()),
    );
    vi.stubGlobal("fetch", fetchMock);
    const id = "018f0000-0000-7000-8000-000000000901";

    await pauseFocusSession(id, 3);
    await recoverFocusSession(id, "exclude_gap_resume", 4);
    await stopFocusSession(id, 5, "focus-stop-1");
    await cancelFocusSession(id, 6, "focus-cancel-1");

    const calls = fetchMock.mock.calls.map(([url, init]) => ({
      url,
      headers: new Headers(init?.headers),
      body: JSON.parse(String(init?.body)),
    }));
    expect(calls[0].headers.get("If-Match")).toBe('"3"');
    expect(calls[1].body).toEqual({ action: "exclude_gap_resume" });
    expect(calls[2].headers.get("Idempotency-Key")).toBe("focus-stop-1");
    expect(calls[3].headers.get("Idempotency-Key")).toBe("focus-cancel-1");
  });

  it("sends the browser IANA timezone to today stats", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({
          data: {
            date: "2026-08-28",
            tasks: {},
            focus: { sessions: 1, seconds: 1500, minutes: 25 },
          },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const stats = await getTodayStats("2026-08-28", "America/Los_Angeles");
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.searchParams.get("timezone")).toBe("America/Los_Angeles");
    expect(stats.focus).toEqual({ sessions: 1, seconds: 1500, minutes: 25 });
  });

  it("reads terminal history with stable pagination fields", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [
          focusSession({
            status: "completed",
            ended_at: "2026-08-28T10:25:00Z",
            last_resumed_at: null,
            end_reason: "completed",
          }),
        ],
        meta: { page: 2, page_size: 6, total: 8 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getFocusSessions({
      page: 2,
      pageSize: 6,
      status: "completed",
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/focus-sessions");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "6",
      status: "completed",
    });
    expect(result.meta).toEqual({ page: 2, pageSize: 6, total: 8 });
    expect(result.items[0]).toMatchObject({
      status: "completed",
      taskTitle: "整理交付清单",
    });
  });

  it("reads a timezone-explicit focus report", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: {
          date_from: "2026-08-22",
          date_to: "2026-08-28",
          timezone: "America/Los_Angeles",
          totals: { sessions: 2, seconds: 3000, minutes: 50 },
          days: [
            { date: "2026-08-28", sessions: 1, seconds: 1500, minutes: 25 },
          ],
          projects: [
            {
              project_id: "project-1",
              project_name: "客户门户",
              sessions: 2,
              seconds: 3000,
              minutes: 50,
            },
          ],
          hours: Array.from({ length: 24 }, (_, hour) => ({
            hour,
            sessions: hour === 9 ? 2 : 0,
            seconds: hour === 9 ? 3000 : 0,
            minutes: hour === 9 ? 50 : 0,
          })),
          heatmap: Array.from({ length: 7 * 24 }, (_, index) => ({
            weekday: Math.floor(index / 24) + 1,
            hour: index % 24,
            sessions: index === 8 ? 2 : 0,
            seconds: index === 8 ? 3000 : 0,
            minutes: index === 8 ? 50 : 0,
          })),
          tags: [
            {
              tag_id: "tag-1",
              tag_name: "深度工作",
              tag_color: "#6C5CE7",
              sessions: 2,
              seconds: 3000,
              minutes: 50,
            },
          ],
          current_streak_days: 1,
          longest_streak_days: 2,
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const report = await getFocusReport({
      dateFrom: "2026-08-22",
      dateTo: "2026-08-28",
      timezone: "America/Los_Angeles",
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.searchParams.get("timezone")).toBe("America/Los_Angeles");
    expect(report).toMatchObject({
      totals: { sessions: 2, seconds: 3000, minutes: 50 },
      currentStreakDays: 1,
      longestStreakDays: 2,
      projects: [
        expect.objectContaining({
          projectId: "project-1",
          projectName: "客户门户",
          minutes: 50,
        }),
      ],
      hours: expect.arrayContaining([
        expect.objectContaining({ hour: 9, minutes: 50, sessions: 2 }),
      ]),
      heatmap: expect.arrayContaining([
        expect.objectContaining({
          weekday: 1,
          hour: 8,
          minutes: 50,
          sessions: 2,
        }),
      ]),
      tags: [
        expect.objectContaining({
          tagId: "tag-1",
          tagName: "深度工作",
          tagColor: "#6C5CE7",
          minutes: 50,
        }),
      ],
    });
  });
});
