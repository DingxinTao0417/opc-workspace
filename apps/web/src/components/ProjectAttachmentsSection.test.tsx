import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectAttachment } from "../types/models";
import { ProjectAttachmentsSection } from "./ProjectAttachmentsSection";

const attachment: ProjectAttachment = {
  id: "attachment-1",
  projectId: "project-1",
  name: "需求.pdf",
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
  projectVersion: 4,
};

const state = vi.hoisted(() => ({
  items: [] as ProjectAttachment[],
  queryInput: null as unknown,
  total: null as number | null,
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  remove: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  download: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
}));

vi.mock("../api/hooks", () => ({
  useProjectAttachmentsQuery: (
    _id: string,
    input: { page?: number; pageSize?: number },
  ) => {
    state.queryInput = input;
    return {
      data: {
        items: state.items,
        meta: {
          page: input.page ?? 1,
          pageSize: 10,
          total: state.total ?? state.items.length,
          projectVersion: 4,
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
  },
  useCreateProjectAttachment: () => state.create,
  useDeleteProjectAttachment: () => state.remove,
  useDownloadProjectAttachment: () => state.download,
}));

describe("ProjectAttachmentsSection", () => {
  beforeEach(() => {
    state.items = [];
    state.total = null;
    state.create.mutate.mockClear();
    state.remove.mutate.mockClear();
    state.download.mutate.mockClear();
  });

  afterEach(cleanup);

  it("previews a file and uploads it with the latest aggregate version", () => {
    const view = render(
      <ProjectAttachmentsSection
        archived={false}
        projectId="project-1"
        projectVersion={3}
      />,
    );
    const input = view.container.querySelector('input[type="file"]');
    const file = new File(["file body"], "source.pdf", {
      type: "application/pdf",
    });
    fireEvent.change(input!, { target: { files: [file] } });
    expect(screen.getByDisplayValue("source.pdf")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("附件名称"), {
      target: { value: "  项目需求.pdf  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认上传" }));
    expect(state.create.mutate).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        input: { file, name: "项目需求.pdf", expectedVersion: 4 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("requires a reason before confirmed deletion", () => {
    state.items = [attachment];
    render(
      <ProjectAttachmentsSection
        archived={false}
        projectId="project-1"
        projectVersion={4}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "删除 需求.pdf" }));
    const confirm = screen.getByRole("button", { name: "确认删除" });
    expect(confirm).toBeDisabled();
    fireEvent.change(screen.getByPlaceholderText("填写删除原因"), {
      target: { value: "已替换" },
    });
    fireEvent.click(confirm);
    expect(state.remove.mutate).toHaveBeenCalledWith(
      {
        id: "attachment-1",
        projectId: "project-1",
        input: { reason: "已替换", expectedVersion: 4 },
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("keeps archived projects readable while disabling writes", () => {
    state.items = [attachment];
    render(
      <ProjectAttachmentsSection
        archived
        projectId="project-1"
        projectVersion={4}
      />,
    );
    expect(screen.getByRole("button", { name: "添加附件" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "删除 需求.pdf" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "下载 需求.pdf" })).toBeEnabled();
  });

  it("switches to auditable deleted history", () => {
    render(
      <ProjectAttachmentsSection
        archived={false}
        projectId="project-1"
        projectVersion={4}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "删除历史" }));
    expect(state.queryInput).toEqual(
      expect.objectContaining({ includeDeleted: true, page: 1 }),
    );
  });

  it("settles on the last valid project attachment page", async () => {
    state.items = [attachment];
    state.total = 11;
    const view = render(
      <ProjectAttachmentsSection
        archived={false}
        projectId="project-1"
        projectVersion={4}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(state.queryInput).toEqual(expect.objectContaining({ page: 2 }));

    state.total = 0;
    view.rerender(
      <ProjectAttachmentsSection
        archived={false}
        projectId="project-1"
        projectVersion={4}
      />,
    );
    await waitFor(() =>
      expect(state.queryInput).toEqual(expect.objectContaining({ page: 1 })),
    );
  });
});
