import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectNote } from "../types/models";
import { ProjectNotesSection } from "./ProjectNotesSection";

const note: ProjectNote = {
  id: "note-1",
  projectId: "project-1",
  title: "交付范围",
  body: "确认第一阶段交付范围",
  occurredAt: "2026-08-28T08:00:00Z",
  createdBy: { id: "owner-1", type: "owner", displayName: "我" },
  version: 2,
  deletedAt: null,
  deletedByActorId: null,
  deleteReason: null,
  createdAt: "2026-08-28T08:01:00Z",
  updatedAt: "2026-08-28T08:02:00Z",
  projectVersion: 4,
};

const state = vi.hoisted(() => ({
  queryInput: {} as Record<string, unknown>,
  create: { mutate: vi.fn(), reset: vi.fn() },
  update: { mutate: vi.fn(), reset: vi.fn() },
  remove: { mutate: vi.fn(), reset: vi.fn() },
  refetch: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useProjectNotesQuery: (
    _projectId: string,
    input: Record<string, unknown>,
  ) => {
    state.queryInput = input;
    return {
      data: {
        items: [note],
        meta: { page: 1, pageSize: 6, total: 1, projectVersion: 4 },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: state.refetch,
    };
  },
  useCreateProjectNote: () => ({
    ...state.create,
    error: null,
    isPending: false,
  }),
  useUpdateProjectNote: () => ({
    ...state.update,
    error: null,
    isPending: false,
  }),
  useDeleteProjectNote: () => ({
    ...state.remove,
    error: null,
    isPending: false,
  }),
}));

describe("ProjectNotesSection", () => {
  beforeEach(() => {
    state.create.mutate.mockReset();
    state.update.mutate.mockReset();
    state.remove.mutate.mockReset();
    state.refetch.mockReset();
  });

  afterEach(cleanup);

  it("creates, edits, deletes, and switches deleted-history queries", () => {
    render(<ProjectNotesSection archived={false} projectId="project-1" />);

    expect(screen.getByText("确认第一阶段交付范围")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "添加笔记" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "新的决策" },
    });
    fireEvent.change(screen.getByLabelText("正文"), {
      target: { value: "记录已经确认的事实" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存笔记" }));
    expect(state.create.mutate).toHaveBeenCalledWith(
      {
        projectId: "project-1",
        input: expect.objectContaining({
          title: "新的决策",
          body: "记录已经确认的事实",
        }),
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "编辑笔记 交付范围" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "更新后的范围" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(state.update.mutate).toHaveBeenCalledWith(
      {
        id: "note-1",
        input: expect.objectContaining({
          title: "更新后的范围",
          expectedVersion: 2,
        }),
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "删除笔记 交付范围" }));
    fireEvent.click(screen.getByRole("button", { name: "确认删除笔记" }));
    expect(screen.getByText("删除原因需填写 1–1,000 个字符。")).toBeVisible();
    fireEvent.change(screen.getByLabelText("删除原因"), {
      target: { value: "重复记录" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认删除笔记" }));
    expect(state.remove.mutate).toHaveBeenCalledWith(
      {
        id: "note-1",
        input: { reason: "重复记录", expectedVersion: 2 },
      },
      expect.objectContaining({ onError: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("checkbox", { name: "显示已删除记录" }));
    expect(state.queryInput).toEqual(
      expect.objectContaining({ includeDeleted: true, page: 1 }),
    );
  });

  it("keeps archived projects readable but disables note writes", () => {
    render(<ProjectNotesSection archived projectId="project-1" />);

    expect(screen.getByText(/归档项目只读/)).toBeVisible();
    expect(screen.getByRole("button", { name: "添加笔记" })).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "编辑笔记 交付范围" }),
    ).not.toBeInTheDocument();
  });
});
