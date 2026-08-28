import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { TaskSavedViewDefinition } from "../types/models";
import { TaskSavedViewsControl } from "./TaskSavedViewsControl";

const definition: TaskSavedViewDefinition = {
  q: "交付",
  status: "waiting_review",
  priority: "P1",
  kind: "review",
  projectId: "project-1",
  clientId: "client-1",
  tagIds: ["tag-1"],
  plannedDate: "",
  plannedFrom: "2026-08-20",
  plannedTo: "2026-08-31",
  dueFrom: "2026-08-25",
  dueTo: "2026-09-05",
  sort: "-updated_at",
};

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  reset: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useTaskSavedViewsQuery: () => ({
    data: [
      {
        id: "view-1",
        name: "客户验收",
        definition: {
          q: "交付",
          status: "waiting_review",
          priority: "P1",
          kind: "review",
          projectId: "project-1",
          clientId: "client-1",
          tagIds: ["tag-1"],
          plannedDate: "",
          plannedFrom: "2026-08-20",
          plannedTo: "2026-08-31",
          dueFrom: "2026-08-25",
          dueTo: "2026-09-05",
          sort: "-updated_at",
        },
        schemaVersion: 1,
        version: 3,
        createdAt: "2026-08-27T08:00:00Z",
        updatedAt: "2026-08-28T08:00:00Z",
      },
    ],
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: mocks.create,
    reset: mocks.reset,
  }),
  useUpdateTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: mocks.update,
    reset: mocks.reset,
  }),
  useDeleteTaskSavedView: () => ({
    error: null,
    isPending: false,
    mutate: mocks.remove,
    reset: mocks.reset,
  }),
}));

describe("TaskSavedViewsControl", () => {
  beforeEach(() => {
    mocks.create.mockClear();
    mocks.update.mockClear();
    mocks.remove.mockClear();
    mocks.reset.mockClear();
  });

  it("applies, creates, updates, and confirms deletion", () => {
    const onApply = vi.fn();
    render(<TaskSavedViewsControl definition={definition} onApply={onApply} />);

    fireEvent.change(screen.getByLabelText("已保存视图"), {
      target: { value: "view-1" },
    });
    expect(onApply).toHaveBeenCalledWith(definition);

    fireEvent.change(screen.getByLabelText("新视图名称"), {
      target: { value: "本周交付" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存当前" }));
    expect(mocks.create).toHaveBeenCalledWith(
      { name: "本周交付", definition },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );

    fireEvent.click(screen.getByRole("button", { name: "更新所选" }));
    expect(mocks.update).toHaveBeenCalledWith({
      id: "view-1",
      input: { expectedVersion: 3, definition },
    });

    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "确认永久删除“客户验收”",
    );
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    expect(mocks.remove).toHaveBeenCalledWith(
      { id: "view-1", expectedVersion: 3 },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });
});
