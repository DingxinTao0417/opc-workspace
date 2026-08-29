import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  getProjectArtifacts,
  normalizeProjectArtifactItem,
  resetRuntimeConnection,
} from "./client";

const actor = {
  id: "018f0000-0000-7000-8000-000000000001",
  type: "owner",
  display_name: "我",
  status: "active",
  is_builtin: true,
  version: 1,
};

function itemPayload(overrides: Record<string, unknown> = {}) {
  return {
    artifact: {
      id: "018f0000-0000-7000-8000-000000000011",
      task_id: "018f0000-0000-7000-8000-000000000021",
      submission_id: "018f0000-0000-7000-8000-000000000031",
      submission_status: "accepted",
      position: 1,
      storage_kind: "text",
      name: "交付说明",
      mime_type: null,
      size_bytes: null,
      sha256: null,
      requires_followup: true,
      produced_by_actor_id: actor.id,
      produced_by_actor: actor,
      recorded_by_actor_id: actor.id,
      recorded_by_actor: actor,
      integrity_status: "unverified",
      integrity_checked_at: null,
      deleted_at: null,
      deleted_by_actor_id: null,
      deleted_by_actor: null,
      delete_reason: null,
      created_at: "2026-08-28T08:00:00Z",
    },
    task: {
      id: "018f0000-0000-7000-8000-000000000021",
      title: "准备交付",
      status: "done",
    },
    submission_sequence: 2,
    followup: {
      inbox_item_id: "018f0000-0000-7000-8000-000000000041",
      inbox_item_version: 3,
      status: "tracking",
      resolution_policy: "all_required_tasks_done",
      source_deleted_at: null,
      progress: {
        active_total: 2,
        required_total: 2,
        required_done: 1,
        required_remaining: 1,
        required_blocked: 0,
        required_waiting_review: 1,
        required_cancelled: 0,
        percent: 50,
        all_required_done: false,
      },
    },
    ...overrides,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetRuntimeConnection();
});

describe("project Artifact API contract", () => {
  it("normalizes Task-owned project output context strictly", () => {
    expect(normalizeProjectArtifactItem(itemPayload())).toMatchObject({
      task: {
        id: "018f0000-0000-7000-8000-000000000021",
        title: "准备交付",
        status: "done",
      },
      submissionSequence: 2,
      artifact: { name: "交付说明", requiresFollowup: true },
      followup: {
        inboxItemId: "018f0000-0000-7000-8000-000000000041",
        inboxItemVersion: 3,
        status: "tracking",
        progress: {
          requiredDone: 1,
          requiredWaitingReview: 1,
          percent: 50,
        },
      },
    });
    expect(() =>
      normalizeProjectArtifactItem(
        itemPayload({ task: { id: "other", title: "错误", status: "done" } }),
      ),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectArtifactItem(itemPayload({ followup: undefined })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectArtifactItem(
        itemPayload({
          followup: {
            ...(itemPayload().followup as Record<string, unknown>),
            source_deleted_at: undefined,
          },
        }),
      ),
    ).toThrow(ApiError);
    expect(() =>
      normalizeProjectArtifactItem(
        itemPayload({
          followup: {
            inbox_item_id: "inbox-1",
            inbox_item_version: 2,
            status: "tracking",
            resolution_policy: "manual",
            source_deleted_at: "2026-08-28T09:00:00Z",
            progress: {
              active_total: 0,
              required_total: 0,
              required_done: 0,
              required_remaining: 0,
              required_blocked: 0,
              required_waiting_review: 0,
              required_cancelled: 0,
              percent: null,
              all_required_done: false,
            },
          },
        }),
      ),
    ).toThrow(ApiError);
  });

  it("serializes pagination and deleted-history filters", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL) =>
        new Response(
          JSON.stringify({
            data: [itemPayload()],
            meta: { page: 2, page_size: 6, total: 7, project_version: 9 },
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await getProjectArtifacts("project/1", {
      page: 2,
      pageSize: 6,
      includeDeleted: true,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(url.pathname).toBe("/api/v1/projects/project%2F1/artifacts");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "6",
      include_deleted: "true",
    });
    expect(result.meta).toEqual({
      page: 2,
      pageSize: 6,
      total: 7,
      projectVersion: 9,
    });
  });

  it("rejects pagination metadata that does not match the requested page", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [itemPayload()],
              meta: { page: 1, page_size: 6, total: 1, project_version: 9 },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );

    await expect(
      getProjectArtifacts("project-1", { page: 2, pageSize: 6 }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });

    const second = itemPayload();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              data: [
                itemPayload(),
                {
                  ...second,
                  artifact: {
                    ...(second.artifact as Record<string, unknown>),
                    id: "018f0000-0000-7000-8000-000000000012",
                  },
                },
              ],
              meta: { page: 2, page_size: 6, total: 7, project_version: 9 },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
    );
    await expect(
      getProjectArtifacts("project-1", { page: 2, pageSize: 6 }),
    ).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });
});
