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
    });
    expect(() =>
      normalizeProjectArtifactItem(
        itemPayload({ task: { id: "other", title: "错误", status: "done" } }),
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
});
