import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createClientAttachment,
  deleteClientAttachment,
  downloadClientAttachment,
  getClientAttachment,
  getClientAttachments,
  normalizeClientAttachment,
  resetRuntimeConnection,
} from "./client";

function attachmentPayload(overrides: Record<string, unknown> = {}) {
  return {
    id: "attachment-1",
    client_id: "client-1",
    activity_id: null,
    name: "合同.pdf",
    mime_type: "application/pdf",
    size_bytes: 12,
    sha256: "a".repeat(64),
    recorded_by: { id: "owner-1", type: "owner", display_name: "Owner" },
    integrity_status: "verified",
    integrity_checked_at: "2026-08-28T08:00:00Z",
    deleted_at: null,
    deleted_by_actor_id: null,
    delete_reason: null,
    created_at: "2026-08-28T08:00:00Z",
    client_version: 4,
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

describe("client attachment API contract", () => {
  it("strictly normalizes active and deleted attachment facts", () => {
    expect(normalizeClientAttachment(attachmentPayload())).toEqual({
      id: "attachment-1",
      clientId: "client-1",
      activityId: null,
      name: "合同.pdf",
      mimeType: "application/pdf",
      sizeBytes: 12,
      sha256: "a".repeat(64),
      recordedBy: { id: "owner-1", type: "owner", displayName: "Owner" },
      integrityStatus: "verified",
      integrityCheckedAt: "2026-08-28T08:00:00Z",
      deletedAt: null,
      deletedByActorId: null,
      deleteReason: null,
      createdAt: "2026-08-28T08:00:00Z",
      clientVersion: 4,
    });
    expect(
      normalizeClientAttachment(
        attachmentPayload({
          deleted_at: "2026-08-28T09:00:00Z",
          deleted_by_actor_id: "owner-1",
          delete_reason: "已替换",
        }),
      ).deleteReason,
    ).toBe("已替换");
    expect(() =>
      normalizeClientAttachment(attachmentPayload({ sha256: "bad" })),
    ).toThrow(ApiError);
    expect(() =>
      normalizeClientAttachment(
        attachmentPayload({ deleted_at: "2026-08-28T09:00:00Z" }),
      ),
    ).toThrow(ApiError);
  });

  it("serializes list filters and aggregate metadata", async () => {
    const fetchMock = vi.fn(async (_input: string | URL | Request) =>
      jsonResponse({
        data: [attachmentPayload()],
        meta: { page: 2, page_size: 5, total: 6, client_version: 4 },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const result = await getClientAttachments("client-1", {
      page: 2,
      pageSize: 5,
      activityId: "activity-1",
      includeDeleted: true,
    });
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://local");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      page: "2",
      page_size: "5",
      activity_id: "activity-1",
      include_deleted: "true",
    });
    expect(result.meta.clientVersion).toBe(4);
  });

  it("uses ordered multipart, If-Match, idempotency, download, and confirmed deletion", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.endsWith("/content")) {
          return new Response(new Blob(["file body"]), {
            status: 200,
            headers: {
              "Content-Type": "application/pdf",
              "Content-Disposition": 'attachment; filename="contract.pdf"',
            },
          });
        }
        return jsonResponse({
          data: attachmentPayload(
            init?.method === "DELETE"
              ? {
                  deleted_at: "2026-08-28T09:00:00Z",
                  deleted_by_actor_id: "owner-1",
                  delete_reason: "已替换",
                }
              : {},
          ),
        });
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    const file = new File(["file body"], "source.pdf", {
      type: "application/pdf",
    });
    await createClientAttachment(
      "client-1",
      { file, name: "合同.pdf", activityId: null, expectedVersion: 3 },
      "upload-key",
    );
    await getClientAttachment("attachment-1");
    const downloaded = await downloadClientAttachment(
      "attachment-1",
      "fallback.pdf",
    );
    await deleteClientAttachment(
      "attachment-1",
      { reason: "已替换", expectedVersion: 4 },
      "delete-key",
    );

    const createInit = fetchMock.mock.calls[0][1];
    expect(new Headers(createInit?.headers).get("If-Match")).toBe('"3"');
    expect(new Headers(createInit?.headers).get("Idempotency-Key")).toBe(
      "upload-key",
    );
    const form = createInit?.body as FormData;
    expect(Array.from(form.keys())).toEqual(["metadata", "file"]);
    expect(JSON.parse(String(form.get("metadata")))).toEqual({
      name: "合同.pdf",
      activity_id: null,
    });
    expect(downloaded.fileName).toBe("contract.pdf");
    expect(downloaded.blob.size).toBeGreaterThan(0);
    const deleteUrl = new URL(
      String(fetchMock.mock.calls[3][0]),
      "http://local",
    );
    const deleteInit = fetchMock.mock.calls[3][1];
    expect(deleteUrl.searchParams.get("confirm")).toBe("true");
    expect(new Headers(deleteInit?.headers).get("If-Match")).toBe('"4"');
    expect(new Headers(deleteInit?.headers).get("Idempotency-Key")).toBe(
      "delete-key",
    );
  });
});
