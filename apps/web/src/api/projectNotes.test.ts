import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createProjectNote,
  deleteProjectNote,
  getProjectNote,
  getProjectNotes,
  normalizeProjectNote,
  normalizeProjectNoteListResult,
  resetRuntimeConnection,
  updateProjectNote,
} from "./client";

function notePayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "note-1",
    project_id: "project-1",
    title: "交付范围",
    body: "确认第一阶段范围",
    occurred_at: "2026-08-28T08:00:00Z",
    created_by: {
      id: "owner-1",
      type: "owner",
      display_name: "我",
    },
    version: 2,
    deleted_at: null,
    deleted_by_actor_id: null,
    delete_reason: null,
    created_at: "2026-08-28T08:01:00Z",
    updated_at: "2026-08-28T08:02:00Z",
    project_version: 4,
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

describe("project note API contract", () => {
  it("strictly normalizes active and deleted project-note facts", () => {
    expect(normalizeProjectNote(notePayload())).toEqual({
      id: "note-1",
      projectId: "project-1",
      title: "交付范围",
      body: "确认第一阶段范围",
      occurredAt: "2026-08-28T08:00:00Z",
      createdBy: { id: "owner-1", type: "owner", displayName: "我" },
      version: 2,
      deletedAt: null,
      deletedByActorId: null,
      deleteReason: null,
      createdAt: "2026-08-28T08:01:00Z",
      updatedAt: "2026-08-28T08:02:00Z",
      projectVersion: 4,
    });
    expect(
      normalizeProjectNote(
        notePayload({
          body: null,
          deleted_at: "2026-08-28T09:00:00Z",
          deleted_by_actor_id: "owner-1",
          delete_reason: "重复",
        }),
      ).deletedAt,
    ).toBe("2026-08-28T09:00:00Z");
    expect(() => normalizeProjectNote(notePayload({ body: null }))).toThrow(
      ApiError,
    );
    expect(() =>
      normalizeProjectNote(notePayload({ occurred_at: "not-time" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectNote(notePayload({ deleted_at: "2026-08-28T09:00:00Z" })),
    ).toThrow(ApiError);
  });

  it("serializes list filters and rejects inconsistent aggregate versions", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL) =>
      jsonResponse({
        data: [notePayload({ project_id: "project/1" })],
        meta: { page: 2, page_size: 5, total: 6, project_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getProjectNotes("project/1", {
      page: 2,
      pageSize: 5,
      includeDeleted: true,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/projects/project%2F1/notes");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
      include_deleted: "true",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 5,
      total: 6,
      projectVersion: 4,
    });
    expect(() =>
      normalizeProjectNoteListResult({
        data: [notePayload({ project_version: 3 })],
        meta: { page: 1, page_size: 20, total: 1, project_version: 4 },
      }),
    ).toThrow(ApiError);
  });

  it("uses idempotency, If-Match, and confirmed soft deletion contracts", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, init?: RequestInit) =>
        jsonResponse({
          data: notePayload({
            version: init?.method === "POST" ? 1 : 3,
            ...(init?.method === "DELETE"
              ? {
                  body: null,
                  deleted_at: "2026-08-28T09:00:00Z",
                  deleted_by_actor_id: "owner-1",
                  delete_reason: "重复",
                }
              : {}),
          }),
        }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createProjectNote(
      "project-1",
      {
        title: "复盘",
        body: "确认结论",
        occurredAt: "2026-08-28T08:00:00Z",
      },
      "project-note-key",
    );
    await getProjectNote("note-1");
    await updateProjectNote("note-1", {
      body: "更新结论",
      expectedVersion: 2,
    });
    await deleteProjectNote("note-1", {
      reason: "重复",
      expectedVersion: 3,
    });

    const createInit = fetchMock.mock.calls[0][1];
    const updateInit = fetchMock.mock.calls[2][1];
    const deleteUrl = new URL(
      String(fetchMock.mock.calls[3][0]),
      "http://local",
    );
    const deleteInit = fetchMock.mock.calls[3][1];
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "project-note-key",
    );
    expect(new Headers(updateInit?.headers).get("If-Match")).toBe('"2"');
    expect(deleteUrl.searchParams.get("confirm")).toBe("true");
    expect(new Headers(deleteInit?.headers).get("If-Match")).toBe('"3"');
    expect(JSON.parse(String(deleteInit?.body))).toEqual({ reason: "重复" });
  });
});
