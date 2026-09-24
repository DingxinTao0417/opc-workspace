import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type { TaskSavedViewDefinition } from "../types/models";
import { TaskSavedViewsControl } from "./TaskSavedViewsControl";

const savedViewId = "018f0000-0000-7000-8000-000000001950";

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
        id: savedViewId,
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
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
  });
  afterEach(cleanup);

  function renderControl(onApply = vi.fn()) {
    render(
      <MemoryRouter>
        <TaskSavedViewsControl definition={definition} onApply={onApply} />
      </MemoryRouter>,
    );
    return { onApply };
  }

  it("applies, creates, updates, and confirms deletion", () => {
    const onApply = vi.fn();
    renderControl(onApply);

    fireEvent.change(screen.getByLabelText("已保存视图"), {
      target: { value: savedViewId },
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
      id: savedViewId,
      input: { expectedVersion: 3, definition },
    });

    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "确认永久删除“客户验收”",
    );
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    expect(mocks.remove).toHaveBeenCalledWith(
      { id: savedViewId, expectedVersion: 3 },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("hands the selected saved view to the agent without applying or mutating it", () => {
    const { onApply } = renderControl();

    fireEvent.change(screen.getByLabelText("已保存视图"), {
      target: { value: savedViewId },
    });
    onApply.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "交给智能体" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "任务保存视图",
      route: "/tasks",
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_task_views");
    expect(pending?.prompt).toContain("view=view");
    expect(pending?.prompt).toContain(savedViewId);
    expect(pending?.prompt).toContain("task_view.*");
    expect(pending?.prompt).toContain("不会修改任何任务");
    expect(onApply).not.toHaveBeenCalled();
    expect(mocks.create).not.toHaveBeenCalled();
    expect(mocks.update).not.toHaveBeenCalled();
    expect(mocks.remove).not.toHaveBeenCalled();
  });
});
