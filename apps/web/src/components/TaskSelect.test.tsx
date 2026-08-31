import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Task, TaskStatus } from "../types/models";
import { TaskSelect } from "./TaskSelect";

const hooks = vi.hoisted(() => ({
  options: {
    data: undefined,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: vi.fn(),
  } as any,
  selected: {
    data: undefined,
    isError: false,
    isPending: false,
  } as any,
  optionsHook: vi.fn(),
  selectedHook: vi.fn(),
}));

vi.mock("../api/hooks", () => ({
  useTaskPageQuery: (input: unknown, enabled: boolean) =>
    hooks.optionsHook(input, enabled),
  useTaskQuery: (id: string | null) => hooks.selectedHook(id),
}));

function task(
  id: string,
  title: string,
  status: TaskStatus = "todo",
  projectName?: string,
): Task {
  return {
    id,
    title,
    description: "",
    kind: "work",
    status,
    priority: "P2",
    projectId: projectName ? `project-${id}` : null,
    projectName,
    parentTaskId: null,
    completionCriteria: "",
    reviewPolicy: "none",
    blockedReason: null,
    blockedAt: null,
    blockedFromStatus: null,
    dueDate: null,
    plannedDate: null,
    estimatedMinutes: null,
    actualMinutes: 0,
    manualOrder: null,
    version: 1,
    subtaskTotal: 0,
    subtaskCompleted: 0,
    createdAt: "2026-08-30T00:00:00Z",
    updatedAt: "2026-08-30T00:00:00Z",
    completedAt: null,
    submittedAt: null,
    reviewedAt: null,
    currentSubmissionId: null,
    tags: [],
  };
}

function list(
  items: Task[],
  { page = 1, pageSize = 20, total = items.length } = {},
) {
  return { items, meta: { page, pageSize, total } };
}

const tasks = [
  task("task-todo", "整理简报", "todo", "官网升级"),
  task("task-progress", "确认交付", "in_progress"),
  task("task-blocked", "等待素材", "blocked"),
  task("task-done", "归档结果", "done"),
  task("task-cancelled", "旧任务", "cancelled"),
];

function renderSelect(
  props: Partial<React.ComponentProps<typeof TaskSelect>> = {},
) {
  const onChange = props.onChange ?? vi.fn();
  const result = render(
    <TaskSelect
      ariaLabel="父任务"
      emptyLabel="无父任务"
      onChange={onChange}
      value=""
      variant="form"
      {...props}
    />,
  );
  return { ...result, onChange };
}

describe("TaskSelect", () => {
  beforeEach(() => {
    hooks.options = {
      data: list(tasks),
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    };
    hooks.selected = {
      data: undefined,
      isError: false,
      isPending: false,
    };
    hooks.optionsHook.mockReset();
    hooks.optionsHook.mockImplementation(() => hooks.options);
    hooks.selectedHook.mockReset();
    hooks.selectedHook.mockImplementation(() => hooks.selected);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("opens a bounded page and debounces trimmed server search", () => {
    vi.useFakeTimers();
    renderSelect();

    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20, q: undefined, sort: "title" },
      false,
    );
    const combobox = screen.getByRole("combobox", { name: "父任务" });
    fireEvent.focus(combobox);
    fireEvent.change(combobox, { target: { value: "  交付  " } });

    expect(screen.getByText("正在等待输入…")).toBeTruthy();
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20, q: undefined, sort: "title" },
      true,
    );
    act(() => vi.advanceTimersByTime(249));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20, q: undefined, sort: "title" },
      true,
    );
    act(() => vi.advanceTimersByTime(1));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20, q: "交付", sort: "title" },
      true,
    );
  });

  it("keeps the selected task while excluding self and cancelled candidates", () => {
    hooks.selected.data = tasks[4];
    const { onChange } = renderSelect({
      value: tasks[4].id,
      selectedTitle: tasks[4].title,
      excludeIds: [tasks[2].id],
      excludeStatuses: ["cancelled"],
    });

    expect(screen.getByRole("combobox", { name: "父任务" })).toHaveValue(
      "旧任务",
    );
    expect(screen.getByText("已取消")).toBeTruthy();
    fireEvent.focus(screen.getByRole("combobox", { name: "父任务" }));

    expect(
      screen.getByRole("option", {
        name: "旧任务，已取消，未关联项目",
      }),
    ).toHaveAttribute("aria-selected", "true");
    expect(
      screen.queryByRole("option", {
        name: "等待素材，阻塞，未关联项目",
      }),
    ).toBeNull();
    fireEvent.click(
      screen.getByRole("option", {
        name: "整理简报，待办，项目：官网升级",
      }),
    );
    expect(onChange).toHaveBeenCalledWith("task-todo", tasks[0]);
  });

  it("preserves a failed cross-page selection until the user clears it", () => {
    hooks.selected.isError = true;
    hooks.options.data = list([]);
    const { onChange } = renderSelect({
      value: "missing-task",
      selectedTitle: "仍保留的父任务",
    });

    expect(screen.getByRole("combobox", { name: "父任务" })).toHaveValue(
      "仍保留的父任务",
    );
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.focus(screen.getByRole("combobox", { name: "父任务" }));
    expect(
      screen.getByRole("option", { name: "仍保留的父任务，当前选择" }),
    ).toBeTruthy();
    expect(screen.getByText("当前选择 · 详情暂不可用")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "清除父任务" }));
    expect(onChange).toHaveBeenCalledWith("", null);
  });

  it("paginates while preventing a stale page from being selected", () => {
    const pageOne = {
      data: list(tasks.slice(0, 2), { page: 1, pageSize: 20, total: 21 }),
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    let pageTwo = {
      data: pageOne.data,
      isError: false,
      isFetching: true,
      isPending: false,
      refetch: vi.fn(),
    };
    hooks.optionsHook.mockImplementation((input: { page: number }) =>
      input.page === 2 ? pageTwo : pageOne,
    );
    const { onChange, rerender } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "父任务" });
    fireEvent.focus(combobox);

    fireEvent.click(screen.getByRole("button", { name: "下一页任务" }));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 2, pageSize: 20, q: undefined, sort: "title" },
      true,
    );
    expect(screen.getByText("正在读取第 2 页…")).toBeTruthy();
    const staleOption = screen.getByRole("option", {
      name: "整理简报，待办，项目：官网升级",
    });
    expect(staleOption).toBeDisabled();
    fireEvent.click(staleOption);
    expect(onChange).not.toHaveBeenCalled();

    pageTwo = {
      data: list([tasks[2]], { page: 2, pageSize: 20, total: 21 }),
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    rerender(
      <TaskSelect
        ariaLabel="父任务"
        emptyLabel="无父任务"
        onChange={onChange}
        value=""
        variant="form"
      />,
    );
    expect(screen.getByText("第 2 / 2 页")).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "等待素材，阻塞，未关联项目",
      }),
    ).toBeEnabled();
    fireEvent.keyDown(combobox, { key: "PageUp" });
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20, q: undefined, sort: "title" },
      true,
    );
  });

  it("settles on the last valid page when task options shrink", async () => {
    let total = 21;
    hooks.optionsHook.mockImplementation((input: { page: number }) => ({
      data: list(input.page === 1 ? tasks.slice(0, 2) : [], {
        page: input.page,
        pageSize: 20,
        total,
      }),
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    }));
    const view = renderSelect();
    fireEvent.focus(screen.getByRole("combobox", { name: "父任务" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页任务" }));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      { page: 2, pageSize: 20, q: undefined, sort: "title" },
      true,
    );

    total = 0;
    view.rerender(
      <TaskSelect
        ariaLabel="父任务"
        emptyLabel="无父任务"
        onChange={view.onChange}
        value=""
        variant="form"
      />,
    );

    await waitFor(() =>
      expect(hooks.optionsHook).toHaveBeenLastCalledWith(
        { page: 1, pageSize: 20, q: undefined, sort: "title" },
        true,
      ),
    );
  });

  it("shows loading, retryable error and no-match feedback", () => {
    hooks.options = {
      data: undefined,
      isError: false,
      isFetching: true,
      isPending: true,
      refetch: vi.fn(),
    };
    const { rerender, unmount } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "父任务" });
    fireEvent.focus(combobox);
    expect(screen.getByText("正在读取任务…")).toBeTruthy();

    hooks.options = {
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    rerender(
      <TaskSelect
        ariaLabel="父任务"
        emptyLabel="无父任务"
        onChange={vi.fn()}
        value=""
        variant="form"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "任务读取失败，当前选择已保留。",
    );
    fireEvent.keyDown(combobox, { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(hooks.options.refetch).toHaveBeenCalledTimes(2);

    unmount();
    vi.useFakeTimers();
    hooks.options.data = list([]);
    hooks.options.isError = false;
    renderSelect();
    const emptyCombobox = screen.getByRole("combobox", { name: "父任务" });
    fireEvent.focus(emptyCombobox);
    fireEvent.change(emptyCombobox, { target: { value: "不存在" } });
    act(() => vi.advanceTimersByTime(250));
    expect(screen.getByText("没有匹配“不存在”的任务")).toBeTruthy();
  });

  it("supports keyboard selection without submitting its surrounding form", () => {
    const onSubmit = vi.fn((event: React.FormEvent) => event.preventDefault());
    const onChange = vi.fn();
    render(
      <form onSubmit={onSubmit}>
        <TaskSelect
          ariaLabel="关联任务"
          emptyLabel="不绑定任务"
          onChange={onChange}
          value=""
          variant="form"
        />
      </form>,
    );
    const combobox = screen.getByRole("combobox", { name: "关联任务" });
    fireEvent.focus(combobox);
    fireEvent.keyDown(combobox, { key: "ArrowDown" });
    fireEvent.keyDown(combobox, { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith("task-progress", tasks[1]);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(combobox).toHaveAttribute("aria-expanded", "false");
  });
});
