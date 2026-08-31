import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUiStore } from "../store/ui";
import { NewTaskModal } from "./NewTaskModal";

const createTask = vi.hoisted(() => vi.fn());

vi.mock("./ProjectSelect", () => ({
  ProjectSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value="project-1">品牌官网改版</option>
    </select>
  ),
}));

vi.mock("./TaskSelect", () => ({
  TaskSelect: ({
    ariaLabel,
    emptyLabel,
    onChange,
    value,
  }: {
    ariaLabel: string;
    emptyLabel: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <select
      aria-label={ariaLabel}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value="">{emptyLabel}</option>
      <option value="task-parent">父任务候选</option>
    </select>
  ),
}));

vi.mock("../api/hooks", () => ({
  useCreateTask: () => ({
    error: null,
    isPending: false,
    mutate: createTask,
    reset: vi.fn(),
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
    fireEvent.change(screen.getByLabelText("项目"), {
      target: { value: "" },
    });
    expect(screen.getByLabelText("项目")).toHaveValue("");
    fireEvent.change(screen.getByLabelText("项目"), {
      target: { value: "project-1" },
    });
    fireEvent.change(screen.getByLabelText("父任务"), {
      target: { value: "task-parent" },
    });
    fireEvent.change(screen.getByLabelText("任务名称"), {
      target: { value: "确认交付范围" },
    });
    fireEvent.change(screen.getByLabelText("验收策略"), {
      target: { value: "manual" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建任务" }));

    expect(createTask).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "确认交付范围",
        parentTaskId: "task-parent",
        projectId: "project-1",
        reviewPolicy: "manual",
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(createTask.mock.calls[0][0]).not.toHaveProperty("status");
  });
});
