import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import { NewTaskModal } from "./NewTaskModal";

const createTask = vi.hoisted(() => vi.fn());

vi.mock("../api/hooks", () => ({
  useCreateTask: () => ({
    error: null,
    isPending: false,
    mutate: createTask,
    reset: vi.fn(),
  }),
  useProjectOptionsQuery: () => ({
    data: [{ id: "project-1", name: "品牌官网改版" }],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useTaskOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useTagOptionsQuery: () => ({
    data: [],
    isError: false,
    isPending: false,
    isSuccess: true,
    refetch: vi.fn(),
  }),
  useCreateTag: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
  }),
}));

describe("NewTaskModal", () => {
  beforeEach(() => {
    createTask.mockClear();
    useUiStore.setState({
      newTaskOpen: true,
      newTaskProjectId: "project-1",
    });
  });

  afterEach(() => {
    cleanup();
    useUiStore.setState({ newTaskOpen: false, newTaskProjectId: null });
  });

  it("uses the project selected by the project detail page", () => {
    render(<NewTaskModal />);

    expect(screen.getByLabelText("项目")).toHaveValue("project-1");
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "确认交付范围" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    expect(createTask).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "确认交付范围",
        projectId: "project-1",
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });
});
