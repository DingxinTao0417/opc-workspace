import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Client, ClientStatus } from "../types/models";
import { ClientSelect } from "./ClientSelect";

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
  useClientOptionsQuery: (search: string, page: number, enabled: boolean) =>
    hooks.optionsHook(search, page, enabled),
  useClientQuery: (id: string | null) => hooks.selectedHook(id),
}));

function client(id: string, name: string, status: ClientStatus): Client {
  return {
    id,
    name,
    status,
    contactName: null,
    email: null,
    phone: null,
    notes: null,
    version: 1,
    projectCount: 0,
    latestActivityAt: null,
    createdAt: "2026-08-29T00:00:00Z",
    updatedAt: "2026-08-29T00:00:00Z",
  };
}

function list(
  items: Client[],
  { page = 1, pageSize = 20, total = items.length } = {},
) {
  return { items, meta: { page, pageSize, total } };
}

const firstPage = [
  client("client-active", "星河工作室", "active"),
  client("client-lead", "远山设计", "lead"),
  client("client-inactive", "旧客户", "inactive"),
];

function renderSelect(
  props: Partial<React.ComponentProps<typeof ClientSelect>> = {},
) {
  const onChange = props.onChange ?? vi.fn();
  const result = render(
    <ClientSelect
      ariaLabel="客户"
      emptyLabel="全部客户"
      onChange={onChange}
      value=""
      variant="form"
      {...props}
    />,
  );
  return { ...result, onChange };
}

describe("ClientSelect", () => {
  beforeEach(() => {
    hooks.options = {
      data: list(firstPage),
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

  it("debounces search for 250ms and resets the server page", () => {
    vi.useFakeTimers();
    renderSelect();
    const combobox = screen.getByRole("combobox", { name: "客户" });

    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 1, false);
    fireEvent.focus(combobox);
    fireEvent.change(combobox, { target: { value: "  星河  " } });

    expect(screen.getByText("正在等待输入…")).toBeTruthy();
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 1, true);
    act(() => vi.advanceTimersByTime(249));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 1, true);
    act(() => vi.advanceTimersByTime(1));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("星河", 1, true);
  });

  it("selects an inactive client with the keyboard without submitting its form", () => {
    const onSubmit = vi.fn((event: React.FormEvent) => event.preventDefault());
    const onChange = vi.fn();
    render(
      <form onSubmit={onSubmit}>
        <ClientSelect
          ariaLabel="客户"
          emptyLabel="暂不关联"
          onChange={onChange}
          value=""
          variant="form"
        />
      </form>,
    );
    const combobox = screen.getByRole("combobox", { name: "客户" });
    fireEvent.focus(combobox);

    expect(screen.getByRole("option", { name: "旧客户，已停用" })).toBeTruthy();
    fireEvent.keyDown(combobox, { key: "End" });
    fireEvent.keyDown(combobox, { key: "Home" });
    fireEvent.keyDown(combobox, { key: "ArrowDown" });
    fireEvent.keyDown(combobox, { key: "ArrowDown" });
    fireEvent.keyDown(combobox, { key: "Enter" });

    expect(onChange).toHaveBeenCalledWith("client-inactive");
    expect(onSubmit).not.toHaveBeenCalled();
    expect(combobox).toHaveAttribute("aria-expanded", "false");
  });

  it("keeps clear reachable by keyboard and emits an explicit empty value", () => {
    hooks.selected.data = firstPage[0];
    const { onChange } = renderSelect({
      value: "client-active",
      selectedName: "星河工作室",
    });

    expect(screen.getByRole("combobox", { name: "客户" })).toHaveValue(
      "星河工作室",
    );
    expect(screen.getByText("活跃")).toBeTruthy();
    const clear = screen.getByRole("button", { name: "清除客户" });
    expect(clear.tabIndex).toBe(0);
    fireEvent.click(clear);
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("opens when the selected status or chevron area is clicked", () => {
    hooks.selected.data = firstPage[0];
    const { container } = renderSelect({
      value: "client-active",
      selectedName: "星河工作室",
    });
    const combobox = screen.getByRole("combobox", { name: "客户" });

    fireEvent.click(screen.getByText("活跃"));
    expect(combobox).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(combobox, { key: "Escape" });
    const chevron = container.querySelector(".client-select__chevron");
    expect(chevron).not.toBeNull();
    fireEvent.click(chevron!);
    expect(combobox).toHaveAttribute("aria-expanded", "true");
  });

  it("retains a selected client outside the current page and deduplicates IDs", () => {
    const selected = client("client-selected", "跨页客户", "inactive");
    hooks.selected.data = selected;
    hooks.options.data = list([firstPage[0], firstPage[0], firstPage[1]], {
      page: 2,
      total: 42,
    });
    renderSelect({
      value: selected.id,
      selectedName: selected.name,
      variant: "toolbar",
    });

    expect(screen.getByText("已停用")).toBeTruthy();
    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(3);
    expect(
      screen.getByRole("option", { name: "跨页客户，已停用" }),
    ).toHaveAttribute("aria-selected", "true");
    expect(
      screen.getAllByRole("option", { name: "星河工作室，活跃" }),
    ).toHaveLength(1);
  });

  it("uses selectedName when detail resolution fails without clearing the value", () => {
    hooks.selected.isError = true;
    hooks.options.data = list([]);
    const { onChange } = renderSelect({
      value: "missing-client",
      selectedName: "仍保留的客户",
    });

    expect(screen.getByRole("combobox", { name: "客户" })).toHaveValue(
      "仍保留的客户",
    );
    expect(screen.getByText("当前选择")).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));
    expect(
      screen.getByRole("option", { name: "仍保留的客户，当前选择" }),
    ).toBeTruthy();
    expect(screen.getByText("当前选择 · 详情暂不可用")).toBeTruthy();
  });

  it("shows an error with retry while retaining the selected value", () => {
    hooks.selected.data = firstPage[0];
    hooks.options = {
      data: undefined,
      isError: true,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    renderSelect({ value: firstPage[0].id, selectedName: firstPage[0].name });
    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "客户读取失败，当前选择已保留。",
    );
    expect(
      screen.getByRole("option", { name: "星河工作室，活跃" }),
    ).toBeTruthy();
    const retry = screen.getByRole("button", { name: "重试" });
    expect(retry.tabIndex).toBe(-1);
    fireEvent.keyDown(screen.getByRole("combobox", { name: "客户" }), {
      key: "Enter",
    });
    expect(hooks.options.refetch).toHaveBeenCalledOnce();
    fireEvent.click(retry);
    expect(hooks.options.refetch).toHaveBeenCalledTimes(2);
  });

  it("shows a no-match state after debounce and a more-results hint", () => {
    vi.useFakeTimers();
    hooks.options.data = list([], { total: 0 });
    const { unmount } = renderSelect();
    const combobox = screen.getByRole("combobox", { name: "客户" });
    fireEvent.focus(combobox);
    fireEvent.change(combobox, { target: { value: "不存在" } });
    act(() => vi.advanceTimersByTime(250));
    expect(screen.getByText("没有匹配“不存在”的客户")).toBeTruthy();

    unmount();
    hooks.options.data = list(firstPage, { total: 41 });
    renderSelect();
    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));
    expect(screen.getByText("第 1 / 3 页")).toBeTruthy();
    expect(screen.getByText(/结果较多，可继续输入缩小范围/)).toBeTruthy();
  });

  it("paginates by button and keyboard while locking stale results", () => {
    const pageOne = {
      data: list(firstPage.slice(0, 2), { page: 1, pageSize: 2, total: 3 }),
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
    const combobox = screen.getByRole("combobox", { name: "客户" });
    fireEvent.focus(combobox);
    expect(screen.getByText("第 1 / 2 页")).toBeTruthy();

    const nextPage = screen.getByRole("button", { name: "下一页客户" });
    expect(nextPage.tabIndex).toBe(-1);
    fireEvent.click(nextPage);
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 2, true);
    expect(screen.getByText("正在读取第 2 页…")).toBeTruthy();
    expect(
      screen.getByRole("option", { name: "星河工作室，活跃" }),
    ).toBeDisabled();
    fireEvent.click(screen.getByRole("option", { name: "星河工作室，活跃" }));
    expect(onChange).not.toHaveBeenCalled();

    pageTwo = {
      data: list([firstPage[2]], { page: 2, pageSize: 2, total: 3 }),
      isError: false,
      isFetching: false,
      isPending: false,
      refetch: vi.fn(),
    };
    rerender(
      <ClientSelect
        ariaLabel="客户"
        emptyLabel="全部客户"
        onChange={onChange}
        value=""
        variant="form"
      />,
    );
    expect(screen.getByText("第 2 / 2 页")).toBeTruthy();
    expect(
      screen.getByRole("option", { name: "旧客户，已停用" }),
    ).toBeEnabled();

    fireEvent.keyDown(combobox, { key: "PageUp" });
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 1, true);
    fireEvent.keyDown(combobox, { key: "PageDown" });
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 2, true);
  });

  it("settles on the last valid page when client options shrink", async () => {
    let total = 3;
    hooks.optionsHook.mockImplementation(
      (_search: string, requestedPage: number) => ({
        data: list(requestedPage === 1 ? firstPage.slice(0, 2) : [], {
          page: requestedPage,
          pageSize: 2,
          total,
        }),
        isError: false,
        isFetching: false,
        isPending: false,
        isPlaceholderData: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    const view = renderSelect();
    fireEvent.focus(screen.getByRole("combobox", { name: "客户" }));
    fireEvent.click(screen.getByRole("button", { name: "下一页客户" }));
    expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 2, true);

    total = 0;
    view.rerender(
      <ClientSelect
        ariaLabel="客户"
        emptyLabel="全部客户"
        onChange={view.onChange}
        value=""
        variant="form"
      />,
    );

    await waitFor(() =>
      expect(hooks.optionsHook).toHaveBeenLastCalledWith("", 1, true),
    );
  });

  it("closes on Escape without bubbling to an outer modal handler", () => {
    const outerKeyDown = vi.fn();
    render(
      <div onKeyDown={outerKeyDown}>
        <ClientSelect
          ariaLabel="客户"
          emptyLabel="全部客户"
          onChange={vi.fn()}
          value=""
          variant="filter"
        />
      </div>,
    );
    const combobox = screen.getByRole("combobox", { name: "客户" });
    fireEvent.focus(combobox);
    expect(combobox).toHaveAttribute("aria-expanded", "true");
    outerKeyDown.mockClear();
    fireEvent.keyDown(combobox, { key: "Escape" });
    expect(combobox).toHaveAttribute("aria-expanded", "false");
    expect(outerKeyDown).not.toHaveBeenCalled();
  });
});
