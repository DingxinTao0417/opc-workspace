import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Project, ProjectStatus } from "../types/models";
import { Modal } from "./Modal";
import { ProjectSelect } from "./ProjectSelect";

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
  useProjectOptionsQuery: (
    search: string,
    page: number,
    enabled: boolean,
    includeArchived: boolean,
    clientId?: string,
  ) => hooks.optionsHook(search, page, enabled, includeArchived, clientId),
  useProjectQuery: (id: string | null) => hooks.selectedHook(id),
}));

function project(
  id: string,
  name: string,
  status: ProjectStatus,
  clientName: string | null = null,
): Project {
  return {
    id,
    name,
    status,
    clientId: clientName ? `client-${id}` : null,
    clientName,
    description: "",
    startDate: null,
    dueDate: null,
    amountMinor: null,
    color: null,
    version: 1,
    archivedFromStatus: status === "archived" ? "completed" : null,
    createdAt: "2026-08-29T00:00:00Z",
    updatedAt: "2026-08-29T00:00:00Z",
    taskSummary: {
      total: 0,
      completed: 0,
      inProgress: 0,
      remaining: 0,
      progressPercent: 0,
      actualMinutes: 0,
    },
    invoiceCount: 0,
    availableActions: [],
  };
}

function list(
  items: Project[],
  { page = 1, pageSize = 20, total = items.length } = {},
) {
  return { items, meta: { page, pageSize, total } };
}

const projects = [
  project("project-planning", "官网升级", "planning", "星河工作室"),
  project("project-progress", "官网升级", "in_progress", "远山设计"),
  project("project-paused", "官网升级", "paused"),
  project("project-completed", "交付归档", "completed", "星河工作室"),
  project("project-archived", "历史官网", "archived", "旧客户"),
];

function renderSelect(
  props: Partial<React.ComponentProps<typeof ProjectSelect>> = {},
) {
  const onChange = props.onChange ?? vi.fn();
  const result = render(
    <ProjectSelect
      ariaLabel="项目"
      emptyLabel="未归项目"
      onChange={onChange}
      value=""
      variant="form"
      {...props}
    />,
  );
  return { ...result, onChange };
}

describe("ProjectSelect", () => {
  beforeEach(() => {
    hooks.options = {
      data: list(projects.slice(0, 4)),
      isError: false,
      isFetching: false,
      isPending: false,
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

  it("debounces search for 250ms, resets page, and forwards archive scope", () => {
    vi.useFakeTimers();
    renderSelect({ includeArchived: true, variant: "filter" });
    const combobox = screen.getByRole("combobox", { name: "项目" });

    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      false,
      true,
      undefined,
    );
    fireEvent.focus(combobox);
    fireEvent.change(combobox, { target: { value: "  官网  " } });

    expect(screen.getByText("正在等待输入…")).toBeTruthy();
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      true,
      true,
      undefined,
    );
    act(() => vi.advanceTimersByTime(249));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      true,
      true,
      undefined,
    );
    act(() => vi.advanceTimersByTime(1));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "官网",
      1,
      true,
      true,
      undefined,
    );
  });

  it("forwards the selected client scope to project options", () => {
    renderSelect({ clientId: "client-1" });
    fireEvent.focus(screen.getByRole("combobox", { name: "项目" }));

    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      true,
      false,
      "client-1",
    );
  });

  it("shows every project status and disambiguates same names with client context", () => {
    hooks.options.data = list([...projects, { ...projects[0] }]);
    const { onChange } = renderSelect({ includeArchived: true });
    const combobox = screen.getByRole("combobox", { name: "项目" });
    fireEvent.focus(combobox);

    expect(screen.getAllByRole("option")).toHaveLength(5);

    expect(
      screen.getByRole("option", {
        name: "官网升级，规划中，客户：星河工作室",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "官网升级，进行中，客户：远山设计",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "官网升级，已暂停，未关联客户",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "交付归档，已完成，客户：星河工作室",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "历史官网，已归档，客户：旧客户",
      }),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("option", {
        name: "官网升级，进行中，客户：远山设计",
      }),
    );
    expect(onChange).toHaveBeenCalledWith("project-progress");
  });

  it("retains the current archived project while excluding other archived options by default", () => {
    const current = projects[4];
    const anotherArchived = project(
      "project-archived-other",
      "另一历史项目",
      "archived",
    );
    hooks.selected.data = current;
    hooks.options.data = list([projects[0], current, anotherArchived]);
    renderSelect({ value: current.id, selectedName: current.name });

    expect(screen.getByRole("combobox", { name: "项目" })).toHaveValue(
      "历史官网",
    );
    expect(screen.getByText("已归档")).toBeTruthy();
    fireEvent.focus(screen.getByRole("combobox", { name: "项目" }));
    expect(
      screen.getByRole("option", {
        name: "历史官网，已归档，客户：旧客户",
      }),
    ).toHaveAttribute("aria-selected", "true");
    expect(
      screen.queryByRole("option", {
        name: "另一历史项目，已归档，未关联客户",
      }),
    ).toBeNull();
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      true,
      false,
      undefined,
    );
  });

  it("uses the keyboard without submitting the surrounding form", () => {
    const onSubmit = vi.fn((event: React.FormEvent) => event.preventDefault());
    const onChange = vi.fn();
    render(
      <form onSubmit={onSubmit}>
        <ProjectSelect
          ariaLabel="项目"
          emptyLabel="未归项目"
          onChange={onChange}
          value=""
          variant="form"
        />
      </form>,
    );
    const combobox = screen.getByRole("combobox", { name: "项目" });
    fireEvent.focus(combobox);
    fireEvent.keyDown(combobox, {
      key: "Enter",
      keyCode: 229,
      isComposing: true,
    });
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.keyDown(combobox, { key: "ArrowUp" });
    expect(combobox.getAttribute("aria-activedescendant")).toContain(
      "project-completed",
    );
    fireEvent.keyDown(combobox, { key: "Tab" });
    expect(combobox).toHaveAttribute("aria-expanded", "false");
    expect(onChange).not.toHaveBeenCalled();

    fireEvent.focus(combobox);
    fireEvent.keyDown(combobox, { key: "End" });
    expect(combobox.getAttribute("aria-activedescendant")).toContain(
      "project-completed",
    );
    fireEvent.keyDown(combobox, { key: "Home" });
    expect(combobox.getAttribute("aria-activedescendant")).toContain(
      "project-planning",
    );
    fireEvent.keyDown(combobox, { key: "ArrowDown" });
    fireEvent.keyDown(combobox, { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith("project-progress");
    expect(onSubmit).not.toHaveBeenCalled();
    expect(combobox).toHaveAttribute("aria-expanded", "false");
  });

  it("keeps a failed detail selection visible and only clears explicitly", () => {
    hooks.selected.isError = true;
    hooks.options.data = list([]);
    const { onChange } = renderSelect({
      value: "missing-project",
      selectedName: "仍保留的项目",
    });

    expect(screen.getByRole("combobox", { name: "项目" })).toHaveValue(
      "仍保留的项目",
    );
    expect(screen.getByText("当前选择")).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.focus(screen.getByRole("combobox", { name: "项目" }));
    expect(
      screen.getByRole("option", { name: "仍保留的项目，当前选择" }),
    ).toBeTruthy();
    expect(screen.getByText("当前选择 · 详情暂不可用")).toBeTruthy();

    const clear = screen.getByRole("button", { name: "清除项目" });
    expect(clear.tabIndex).toBe(0);
    fireEvent.click(clear);
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("shows loading and error states and retries from Enter or mouse", () => {
    hooks.options = {
      data: undefined,
      isError: false,
      isFetching: true,
      isPending: true,
      refetch: vi.fn(),
    };
    const { rerender } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "项目" });
    fireEvent.focus(combobox);
    expect(screen.getByText("正在读取项目…")).toBeTruthy();

    hooks.options = {
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    rerender(
      <ProjectSelect
        ariaLabel="项目"
        emptyLabel="未归项目"
        onChange={vi.fn()}
        value=""
        variant="form"
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "项目读取失败，当前选择已保留。",
    );
    fireEvent.keyDown(combobox, { key: "Enter" });
    expect(hooks.options.refetch).toHaveBeenCalledOnce();
    const retry = screen.getByRole("button", { name: "重试" });
    expect(retry.tabIndex).toBe(-1);
    fireEvent.click(retry);
    expect(hooks.options.refetch).toHaveBeenCalledTimes(2);
  });

  it("shows no-match and more-results feedback", () => {
    vi.useFakeTimers();
    hooks.options.data = list([], { total: 0 });
    const { unmount } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "项目" });
    fireEvent.focus(combobox);
    fireEvent.change(combobox, { target: { value: "不存在" } });
    act(() => vi.advanceTimersByTime(250));
    expect(screen.getByText("没有匹配“不存在”的项目")).toBeTruthy();

    unmount();
    hooks.options.data = list(projects.slice(0, 2), { total: 41 });
    renderSelect();
    fireEvent.focus(screen.getByRole("combobox", { name: "项目" }));
    expect(screen.getByText("第 1 / 3 页")).toBeTruthy();
    expect(screen.getByText(/结果较多，可继续输入缩小范围/)).toBeTruthy();
  });

  it("paginates by button and keyboard while locking stale results", () => {
    const pageOne = {
      data: list(projects.slice(0, 2), { page: 1, pageSize: 2, total: 3 }),
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
    hooks.optionsHook.mockImplementation(
      (_search: string, requestedPage: number) =>
        requestedPage === 2 ? pageTwo : pageOne,
    );
    const { onChange, rerender } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "项目" });
    fireEvent.focus(combobox);
    expect(screen.getByText("第 1 / 2 页")).toBeTruthy();

    const nextPage = screen.getByRole("button", { name: "下一页项目" });
    expect(nextPage.tabIndex).toBe(-1);
    fireEvent.click(nextPage);
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      2,
      true,
      false,
      undefined,
    );
    expect(screen.getByText("正在读取第 2 页…")).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "官网升级，规划中，客户：星河工作室",
      }),
    ).toBeDisabled();
    fireEvent.click(
      screen.getByRole("option", {
        name: "官网升级，规划中，客户：星河工作室",
      }),
    );
    expect(onChange).not.toHaveBeenCalled();

    pageTwo = {
      data: list([projects[2]], { page: 2, pageSize: 2, total: 3 }),
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    rerender(
      <ProjectSelect
        ariaLabel="项目"
        emptyLabel="未归项目"
        onChange={onChange}
        value=""
        variant="form"
      />,
    );
    expect(screen.getByText("第 2 / 2 页")).toBeTruthy();
    expect(
      screen.getByRole("option", {
        name: "官网升级，已暂停，未关联客户",
      }),
    ).toBeEnabled();
    fireEvent.keyDown(combobox, { key: "PageUp" });
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      1,
      true,
      false,
      undefined,
    );
    fireEvent.keyDown(combobox, { key: "PageDown" });
    expect(hooks.optionsHook).toHaveBeenLastCalledWith(
      "",
      2,
      true,
      false,
      undefined,
    );
  });

  it("renders in the viewport portal and keeps IME or selector Escape inside a real Modal", () => {
    const onClose = vi.fn();
    render(
      <Modal onClose={onClose} open title="项目选择测试">
        <ProjectSelect
          ariaLabel="项目"
          emptyLabel="全部项目"
          onChange={vi.fn()}
          value=""
          variant="toolbar"
        />
      </Modal>,
    );
    const combobox = screen.getByRole("combobox", { name: "项目" });
    const root = combobox.closest(".client-select") as HTMLDivElement;
    vi.spyOn(root, "getBoundingClientRect").mockReturnValue({
      x: 250,
      y: 40,
      top: 40,
      right: 330,
      bottom: 74,
      left: 250,
      width: 80,
      height: 34,
      toJSON: () => ({}),
    });
    fireEvent.focus(combobox);

    const listbox = screen.getByRole("listbox", { name: "项目候选项" });
    const popover = listbox.parentElement as HTMLDivElement;
    expect(popover.parentElement).toBe(document.body);
    expect(popover).toHaveStyle({ left: "250px", width: "260px" });

    fireEvent.keyDown(combobox, {
      key: "Escape",
      keyCode: 229,
      isComposing: true,
    });
    expect(combobox).toHaveAttribute("aria-expanded", "true");
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(combobox, { key: "Escape" });
    expect(combobox).toHaveAttribute("aria-expanded", "false");
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(combobox, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
  });
});
