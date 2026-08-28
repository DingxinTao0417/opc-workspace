import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ClientAttachment } from "../types/models";
import { ClientAttachmentsSection } from "./ClientAttachmentsSection";

const attachment: ClientAttachment = {
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
};

const state = vi.hoisted(() => ({
  items: [] as ClientAttachment[],
  queryInput: null as unknown,
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  remove: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  download: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useClientAttachmentsQuery: (_id: string, input: unknown) => {
    state.queryInput = input;
    return {
      data: {
        items: state.items,
        meta: {
          page: 1,
          pageSize: 10,
          total: state.items.length,
          clientVersion: 4,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
  },
  useCreateClientAttachment: () => state.create,
  useDeleteClientAttachment: () => state.remove,
  useDownloadClientAttachment: () => state.download,
}));

describe("ClientAttachmentsSection", () => {
  beforeEach(() => {
    state.items = [];
    state.create.mutate.mockClear();
    state.remove.mutate.mockClear();
    state.download.mutate.mockClear();
  });

  afterEach(cleanup);

  it("previews a selected local file before uploading with the client version", () => {
    const view = render(
      <ClientAttachmentsSection clientId="client-1" clientVersion={3} />,
    );
    const input = view.container.querySelector('input[type="file"]');
    const file = new File(["file body"], "source.pdf", {
      type: "application/pdf",
    });
    fireEvent.change(input!, { target: { files: [file] } });
    expect(screen.getByDisplayValue("source.pdf")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("附件名称"), {
      target: { value: "  客户合同.pdf  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认上传" }));
    expect(state.create.mutate).toHaveBeenCalledWith(
      {
        clientId: "client-1",
        input: {
          file,
          name: "客户合同.pdf",
          expectedVersion: 4,
        },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("requires a reason before confirmed deletion", () => {
    state.items = [attachment];
    render(<ClientAttachmentsSection clientId="client-1" clientVersion={4} />);
    fireEvent.click(screen.getByRole("button", { name: "删除 合同.pdf" }));
    const confirm = screen.getByRole("button", { name: "确认删除" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText("填写删除原因"), {
      target: { value: "已替换" },
    });
    fireEvent.click(confirm);
    expect(state.remove.mutate).toHaveBeenCalledWith(
      {
        id: "attachment-1",
        clientId: "client-1",
        input: { reason: "已替换", expectedVersion: 4 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("switches to auditable deleted history", () => {
    render(<ClientAttachmentsSection clientId="client-1" clientVersion={4} />);
    fireEvent.click(screen.getByRole("button", { name: "删除历史" }));
    expect(state.queryInput).toEqual(
      expect.objectContaining({ includeDeleted: true, page: 1 }),
    );
  });
});
