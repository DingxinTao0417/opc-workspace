import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import * as localCalendar from "../lib/localCalendar";
import type { ContentItem } from "../types/models";
import { ContentCalendarPage } from "./ContentCalendarPage";

const hooks = vi.hoisted(() => ({
  items: vi.fn(),
  detail: vi.fn(),
  projects: vi.fn(),
  tasks: vi.fn(),
  create: vi.fn(),
  createError: null as Error | null,
  createPending: false,
  createReset: vi.fn(),
  update: vi.fn(),
  schedule: vi.fn(),
  scheduleReset: vi.fn(),
  publish: vi.fn(),
  link: vi.fn(),
  unlink: vi.fn(),
  remove: vi.fn(),
  removeReset: vi.fn(),
  removeError: null as Error | null,
  removePending: false,
}));
vi.mock("../api/hooks", () => ({
  useContentItemsInfiniteQuery: hooks.items,
  useContentItemQuery: hooks.detail,
  useProjectsQuery: hooks.projects,
  useTasksQuery: hooks.tasks,
  useCreateContentItem: () => ({
    isPending: hooks.createPending,
    isError: Boolean(hooks.createError),
    error: hooks.createError,
    mutate: hooks.create,
    reset: hooks.createReset,
  }),
  useUpdateContentItem: () => ({
    isPending: false,
    error: null,
    mutate: hooks.update,
  }),
  useScheduleContentItem: () => ({
    isPending: false,
    isError: false,
    error: null,
    reset: hooks.scheduleReset,
    mutate: hooks.schedule,
  }),
  usePublishContentItem: () => ({
    isPending: false,
    error: null,
    mutate: hooks.publish,
  }),
  useLinkContentItemTask: () => ({
    isPending: false,
    error: null,
    mutate: hooks.link,
  }),
  useUnlinkContentItemTask: () => ({
    isPending: false,
    error: null,
    mutate: hooks.unlink,
  }),
  useDeleteContentItem: () => ({
    isPending: hooks.removePending,
    error: hooks.removeError,
    mutate: hooks.remove,
    reset: hooks.removeReset,
  }),
}));

const item: ContentItem = {
  id: "content-1",
  title: "发布产品更新",
  platform: "微信公众号",
  status: "scheduled",
  scheduledAt: "2026-09-04T01:00:00Z",
  scheduledTimezone: "Asia/Shanghai",
  publishedAt: null,
  projectId: null,
  notes: null,
  externalLink: null,
  manualOrder: 1024,
  archivedFromStatus: null,
  version: 1,
  createdAt: "2026-08-29T00:00:00Z",
  updatedAt: "2026-08-29T00:00:00Z",
  tasks: [],
  requiredTaskTotal: 2,
  requiredTaskDone: 1,
};

const unscheduledItem: ContentItem = {
  ...item,
  id: "content-unscheduled",
  title: "等待安排发布时间",
  status: "draft",
  scheduledAt: null,
  scheduledTimezone: null,
};

const archivedUnscheduledItem: ContentItem = {
  ...unscheduledItem,
  id: "content-archived-unscheduled",
  title: "已归档未排期内容",
  status: "archived",
  archivedFromStatus: "draft",
};

const archivedScheduledItem: ContentItem = {
  ...item,
  id: "content-archived-scheduled",
  title: "去年已归档内容",
  status: "archived",
  scheduledAt: "2024-01-12T01:00:00Z",
  archivedFromStatus: "scheduled",
};

function contentItemsResult(
  items: ContentItem[],
  options: {
    hasData?: boolean;
    hasNextPage?: boolean;
    total?: number;
    isError?: boolean;
    isFetchNextPageError?: boolean;
    isFetching?: boolean;
    isFetchingNextPage?: boolean;
    isPending?: boolean;
    isPlaceholderData?: boolean;
    isRefetchError?: boolean;
    fetchNextPage?: ReturnType<typeof vi.fn>;
    refetch?: ReturnType<typeof vi.fn>;
  } = {},
) {
  const isPending = options.isPending ?? false;
  const hasData = options.hasData ?? !isPending;
  return {
    data: hasData
      ? {
          pages: [
            {
              items,
              meta: {
                page: 1,
                pageSize: 100,
                total: options.total ?? items.length,
              },
            },
          ],
        }
      : undefined,
    isError: options.isError ?? false,
    isFetchNextPageError: options.isFetchNextPageError ?? false,
    isFetching: options.isFetching ?? false,
    isFetchingNextPage: options.isFetchingNextPage ?? false,
    isPlaceholderData: options.isPlaceholderData ?? false,
    isRefetchError: options.isRefetchError ?? false,
    hasNextPage: options.hasNextPage ?? false,
    isPending,
    isSuccess: !isPending && !options.isError,
    fetchNextPage: options.fetchNextPage ?? vi.fn(),
    refetch: options.refetch ?? vi.fn(),
  };
}

function LocationProbe() {
  const location = useLocation();
  return (
    <output data-testid="location-probe">{`${location.pathname}${location.search}`}</output>
  );
}

describe("ContentCalendarPage", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });
  beforeEach(() => {
    hooks.create.mockReset();
    hooks.createError = null;
    hooks.createPending = false;
    hooks.createReset.mockReset();
    hooks.createReset.mockImplementation(() => {
      hooks.createError = null;
    });
    hooks.update.mockReset();
    hooks.schedule.mockReset();
    hooks.scheduleReset.mockReset();
    hooks.publish.mockReset();
    hooks.link.mockReset();
    hooks.unlink.mockReset();
    hooks.remove.mockReset();
    hooks.removeReset.mockReset();
    hooks.removeError = null;
    hooks.removePending = false;
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id ? item : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    hooks.items.mockReturnValue({
      data: {
        pages: [{ items: [item], meta: { page: 1, pageSize: 100, total: 1 } }],
      },
      isError: false,
      isFetchNextPageError: false,
      isFetchingNextPage: false,
      hasNextPage: false,
      isPending: false,
      isSuccess: true,
      fetchNextPage: vi.fn(),
      refetch: vi.fn(),
    });
    hooks.projects.mockReturnValue({ data: { items: [] }, isPending: false });
    hooks.tasks.mockReturnValue({
      data: [{ id: "task-2", title: "准备配图", status: "todo" }],
      isPending: false,
    });
  });
  it("renders content scheduling facts without external publishing controls", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("发布产品更新")).toBeTruthy();
    expect(screen.getByText("微信公众号")).toBeTruthy();
    expect(screen.getByText(/1\/2 项准备任务/)).toBeTruthy();
    expect(screen.queryByText("发布到平台")).toBeNull();
  });

  it("keeps the existing month range, status filter, and rescheduling affordances", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("button", { name: "月历", pressed: true }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "上个月" })).toBeTruthy();
    const statusFilter = screen.getByRole("combobox", { name: "状态" });
    expect(statusFilter).toBeTruthy();
    expect(
      within(statusFilter).getByRole("option", { name: "已归档（当前月）" }),
    ).toBeTruthy();
    expect(screen.getByText(/键盘改期/)).toBeTruthy();
    const input = hooks.items.mock.calls.at(-1)?.[0];
    expect(input).toEqual(
      expect.objectContaining({
        pageSize: 100,
        scheduledFrom: expect.any(String),
        scheduledTo: expect.any(String),
        includeArchived: false,
      }),
    );
    expect(input).not.toHaveProperty("scheduleState");
  });

  it("discovers unscheduled active content and preserves unrelated URL state", () => {
    hooks.items.mockImplementation((input) =>
      contentItemsResult(
        input.scheduleState === "unscheduled" ? [unscheduledItem] : [item],
      ),
    );
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?source=inbox&campaign=launch"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "无排期" }));

    const input = hooks.items.mock.calls.at(-1)?.[0];
    expect(input).toEqual({
      pageSize: 100,
      scheduleState: "unscheduled",
      includeArchived: false,
    });
    expect(input).not.toHaveProperty("scheduledFrom");
    expect(input).not.toHaveProperty("scheduledTo");
    expect(input).not.toHaveProperty("status");
    expect(screen.getByText("等待安排发布时间")).toBeTruthy();
    expect(screen.getByText("1 条未排期内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "上个月" })).toBeNull();
    expect(screen.queryByText(/键盘改期/)).toBeNull();
    expect(screen.getByRole("button", { name: "新建无排期内容" })).toBeTruthy();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?source=inbox&campaign=launch&view=unscheduled",
    );

    fireEvent.click(
      screen.getByRole("button", { name: "查看内容 等待安排发布时间" }),
    );
    expect(hooks.detail).toHaveBeenLastCalledWith("content-unscheduled");
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "source=inbox&campaign=launch&view=unscheduled&item=content-unscheduled",
    );
  });

  it("creates an unscheduled draft without schedule fields and opens its addressed detail", () => {
    const created = {
      ...unscheduledItem,
      id: "content-created-unscheduled",
      title: "稍后安排的内容",
    };
    hooks.items.mockReturnValue(contentItemsResult([]));
    hooks.create.mockImplementation((_input, options) =>
      options.onSuccess(created),
    );
    render(
      <MemoryRouter
        initialEntries={[
          "/content-calendar?view=unscheduled&source=inbox&campaign=launch",
        ]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新建无排期内容" }));
    expect(screen.getByRole("dialog", { name: "新建无排期内容" })).toBeTruthy();
    expect(screen.queryByLabelText("计划日期")).toBeNull();
    expect(
      screen.getByText(
        "将创建为无排期草稿；稍后可在内容详情中设置计划发布时间。",
      ),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "  稍后安排的内容  " },
    });
    fireEvent.change(screen.getByLabelText("发布平台"), {
      target: { value: "  Newsletter  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建无排期内容" }));

    expect(hooks.create).toHaveBeenCalledWith(
      {
        title: "稍后安排的内容",
        platform: "Newsletter",
        status: "draft",
        projectId: null,
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(hooks.detail).toHaveBeenLastCalledWith(created.id);
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      `/content-calendar?view=unscheduled&source=inbox&campaign=launch&item=${created.id}`,
    );
    expect(screen.queryByRole("dialog", { name: "新建无排期内容" })).toBeNull();
  });

  it("keeps the creation mode chosen at open time when the background view changes", () => {
    hooks.items.mockReturnValue(contentItemsResult([]));
    render(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新建无排期内容" }));
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "锁定模式的内容" },
    });
    fireEvent.change(screen.getByLabelText("发布平台"), {
      target: { value: "博客" },
    });
    fireEvent.click(screen.getByRole("button", { name: "月历" }));

    expect(screen.getByRole("dialog", { name: "新建无排期内容" })).toBeTruthy();
    expect(screen.queryByLabelText("计划日期")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "创建无排期内容" }));
    expect(hooks.create).toHaveBeenCalledWith(
      {
        title: "锁定模式的内容",
        platform: "博客",
        status: "draft",
        projectId: null,
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("clears a cancelled draft and validation error before another creation mode opens", () => {
    hooks.items.mockReturnValue(contentItemsResult([]));
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新建内容" }));
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "不再保留的标题" },
    });
    fireEvent.change(screen.getByLabelText("发布平台"), {
      target: { value: "博客" },
    });
    fireEvent.submit(document.getElementById("content-item-form")!);
    expect(screen.getByText("请填写内容标题、平台和计划日期。")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(hooks.createReset).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole("button", { name: "无排期" }));
    fireEvent.click(screen.getByRole("button", { name: "新建无排期内容" }));
    expect(screen.getByLabelText("内容标题")).toHaveValue("");
    expect(screen.getByLabelText("发布平台")).toHaveValue("");
    expect(screen.queryByText("请填写内容标题、平台和计划日期。")).toBeNull();
  });

  it("keeps month creation scheduled and opens the created detail", () => {
    const created = {
      ...item,
      id: "content-created-scheduled",
      title: "九月发布计划",
    };
    hooks.create.mockImplementation((_input, options) =>
      options.onSuccess(created),
    );
    render(
      <MemoryRouter initialEntries={["/content-calendar?source=toolbar"]}>
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "新建内容" }));
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "九月发布计划" },
    });
    fireEvent.change(screen.getByLabelText("发布平台"), {
      target: { value: "微信公众号" },
    });
    fireEvent.change(screen.getByLabelText("计划日期"), {
      target: { value: "2026-09-05" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建内容" }));

    expect(hooks.create).toHaveBeenCalledWith(
      {
        title: "九月发布计划",
        platform: "微信公众号",
        status: "scheduled",
        projectId: null,
        scheduledAt: new Date("2026-09-05T09:00:00").toISOString(),
        scheduledTimezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(hooks.detail).toHaveBeenLastCalledWith(created.id);
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      `/content-calendar?source=toolbar&item=${created.id}`,
    );
  });

  it("retains an unscheduled draft after failure and blocks duplicate pending submits", () => {
    hooks.items.mockReturnValue(contentItemsResult([]));
    const view = render(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "新建无排期内容" }));
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "保留的草稿" },
    });
    fireEvent.change(screen.getByLabelText("发布平台"), {
      target: { value: "博客" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建无排期内容" }));
    expect(hooks.create).toHaveBeenCalledTimes(1);

    hooks.createError = new Error("本地服务暂不可用");
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("本地服务暂不可用")).toBeTruthy();
    expect(screen.getByLabelText("内容标题")).toHaveValue("保留的草稿");
    expect(screen.getByLabelText("发布平台")).toHaveValue("博客");

    hooks.createError = null;
    hooks.createPending = true;
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    const form = document.getElementById("content-item-form");
    expect(form).not.toBeNull();
    fireEvent.submit(form!);
    expect(hooks.create).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("内容标题")).toBeDisabled();
    expect(screen.getByLabelText("发布平台")).toBeDisabled();
    expect(screen.getByRole("button", { name: "正在创建…" })).toBeDisabled();
  });

  it("discovers archived content across months and with or without a schedule", () => {
    hooks.items.mockImplementation((input) =>
      contentItemsResult(
        input.status === "archived"
          ? [archivedScheduledItem, archivedUnscheduledItem]
          : [item],
      ),
    );
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?view=archived&source=inbox"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    expect(hooks.items).toHaveBeenLastCalledWith({
      pageSize: 100,
      status: "archived",
      includeArchived: true,
    });
    expect(screen.getByText("去年已归档内容")).toBeTruthy();
    expect(screen.getByText("已归档未排期内容")).toBeTruthy();
    expect(screen.getByText("未排期")).toBeTruthy();
    expect(screen.getByText("2 条已归档内容")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "上个月" })).toBeNull();
    expect(screen.queryByRole("button", { name: /新建/ })).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "查看内容 去年已归档内容" }),
    );
    expect(hooks.detail).toHaveBeenLastCalledWith("content-archived-scheduled");
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "view=archived&source=inbox&item=content-archived-scheduled",
    );
  });

  it("changes only the view URL state while preserving an addressed item and source", () => {
    render(
      <MemoryRouter
        initialEntries={[
          "/content-calendar?item=content-1&source=inbox&campaign=launch",
        ]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "已归档" }));
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?item=content-1&source=inbox&campaign=launch&view=archived",
    );

    fireEvent.click(screen.getByRole("button", { name: "月历" }));
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?item=content-1&source=inbox&campaign=launch",
    );
  });

  it("keeps loaded unscheduled rows visible when a later page fails", () => {
    const fetchNextPage = vi.fn();
    hooks.items.mockReturnValue(
      contentItemsResult([unscheduledItem], {
        total: 140,
        isError: true,
        isFetchNextPageError: true,
        fetchNextPage,
      }),
    );
    render(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("无排期内容未完整加载")).toBeTruthy();
    expect(screen.queryByText(/无法读取无排期内容/)).toBeNull();
    expect(
      screen.getByText("已读取 1 / 140 条，后续页面暂时不可用。"),
    ).toBeTruthy();
    expect(screen.getByText("等待安排发布时间")).toBeTruthy();
    expect(screen.getByText("已读取 1 / 140 条未排期内容")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("loads discovery history only when the user asks for another page", () => {
    const fetchNextPage = vi.fn();
    hooks.items.mockReturnValue(
      contentItemsResult([archivedScheduledItem, archivedUnscheduledItem], {
        total: 250,
        hasNextPage: true,
        fetchNextPage,
      }),
    );
    render(
      <MemoryRouter initialEntries={["/content-calendar?view=archived"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(fetchNextPage).not.toHaveBeenCalled();
    expect(screen.getByText("已读取 2 / 250 条已归档内容")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "加载更多" }));
    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("keeps stale discovery facts visible and exposes a failed refresh", () => {
    const refetch = vi.fn();
    hooks.items.mockReturnValue(
      contentItemsResult([archivedScheduledItem], {
        isError: true,
        isRefetchError: true,
        refetch,
      }),
    );
    render(
      <MemoryRouter initialEntries={["/content-calendar?view=archived"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("去年已归档内容")).toBeTruthy();
    expect(screen.getByText("已归档内容未能刷新")).toBeTruthy();
    expect(
      screen.getByText("正在显示上次读取的已归档内容，最新刷新失败。"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it.each([
    [
      "unscheduled",
      "暂无无排期内容",
      "未设置计划发布时间且尚未归档的内容会显示在这里。",
    ],
    [
      "archived",
      "暂无已归档内容",
      "归档内容会跨月份集中显示，包括未排期的归档内容。",
    ],
  ])("shows an accurate %s empty state", (view, title, message) => {
    hooks.items.mockReturnValue(contentItemsResult([]));
    render(
      <MemoryRouter initialEntries={[`/content-calendar?view=${view}`]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText(title)).toBeTruthy();
    expect(screen.getByText(message)).toBeTruthy();
    if (view === "unscheduled") {
      expect(
        screen.getByRole("button", { name: "新建第一条无排期内容" }),
      ).toBeTruthy();
    } else {
      expect(screen.queryByRole("button", { name: /新建第一条/ })).toBeNull();
    }
  });

  it("announces the active discovery view while it is loading", () => {
    hooks.items.mockReturnValue(contentItemsResult([], { isPending: true }));
    render(
      <MemoryRouter initialEntries={["/content-calendar?view=archived"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("正在读取已归档内容…")).toHaveAttribute(
      "role",
      "status",
    );
    expect(screen.getByText("读取中")).toBeTruthy();
  });

  it("does not relabel placeholder rows and shows the new-view failure", () => {
    let phase: "placeholder" | "error" = "placeholder";
    hooks.items.mockImplementation(() =>
      phase === "placeholder"
        ? contentItemsResult([item], {
            isFetching: true,
            isPlaceholderData: true,
          })
        : contentItemsResult([], { hasData: false, isError: true }),
    );
    const view = render(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("正在读取无排期内容…")).toHaveAttribute(
      "role",
      "status",
    );
    expect(screen.queryByText("发布产品更新")).toBeNull();

    phase = "error";
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?view=unscheduled"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("无法读取无排期内容，请确认本地服务已连接。"),
    ).toBeTruthy();
    expect(screen.queryByText("发布产品更新")).toBeNull();
    expect(screen.getByText("数据不可用")).toBeTruthy();
  });

  it("normalizes an unknown view without dropping unrelated URL state", () => {
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?view=typo&source=inbox"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("button", { name: "月历", pressed: true }),
    ).toBeTruthy();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?source=inbox",
    );
  });

  it("follows the new local month at midnight and refreshes the visible range", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 11, 31, 23, 59, 59));
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("2026 年 12 月")).toBeTruthy();
    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scheduledFrom: new Date(2026, 10, 29).toISOString(),
        scheduledTo: new Date(2027, 0, 10).toISOString(),
      }),
    );

    act(() => vi.advanceTimersByTime(1_002));

    expect(screen.getByText("2027 年 1 月")).toBeTruthy();
    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scheduledFrom: new Date(2026, 11, 27).toISOString(),
        scheduledTo: new Date(2027, 1, 7).toISOString(),
      }),
    );
  });

  it("keeps a manually selected month when local midnight passes", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 11, 31, 23, 59, 59));
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "上个月" }));
    expect(screen.getByText("2026 年 11 月")).toBeTruthy();

    act(() => vi.advanceTimersByTime(1_002));

    expect(screen.getByText("2026 年 11 月")).toBeTruthy();
    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({
        scheduledFrom: new Date(2026, 10, 1).toISOString(),
        scheduledTo: new Date(2026, 11, 13).toISOString(),
      }),
    );
  });

  it("rebuilds the same semantic month when only the browser time zone changes", () => {
    const calendarSnapshot = vi
      .spyOn(localCalendar, "useLocalCalendar")
      .mockReturnValue({
        dateKey: "2026-08-30",
        timeZone: "America/Tijuana",
      });
    const dateFromKey = vi.spyOn(localCalendar, "localDateFromKey");
    const view = render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("2026 年 8 月")).toBeTruthy();
    dateFromKey.mockClear();

    calendarSnapshot.mockReturnValue({
      dateKey: "2026-08-30",
      timeZone: "Asia/Shanghai",
    });
    view.rerender(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByText("2026 年 8 月")).toBeTruthy();
    expect(dateFromKey).toHaveBeenCalledWith("2026-08-01");
  });

  it("opens the local edit workflow and exposes explicit schedule and publish actions", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "编辑 发布产品更新" }));
    expect(
      screen.getByRole("heading", { name: "内容详情与排期" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "保存排期" }));
    expect(hooks.schedule).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "content-1",
        input: expect.objectContaining({ expectedVersion: 1 }),
      }),
      expect.anything(),
    );
    fireEvent.click(screen.getByRole("button", { name: "确认已发布" }));
    expect(hooks.publish).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "content-1",
        input: expect.objectContaining({ expectedVersion: 1 }),
      }),
      expect.anything(),
    );
    fireEvent.change(screen.getByRole("combobox", { name: "选择准备任务" }), {
      target: { value: "task-2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "关联" }));
    expect(hooks.link).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "content-1",
        taskId: "task-2",
        isRequired: true,
        expectedVersion: 1,
      }),
      expect.anything(),
    );
  });

  it("opens a URL-addressed item and removes only that query on close", () => {
    hooks.items.mockReturnValue({
      data: {
        pages: [{ items: [], meta: { page: 1, pageSize: 100, total: 0 } }],
      },
      isError: false,
      isFetchNextPageError: false,
      isFetchingNextPage: false,
      hasNextPage: false,
      isPending: false,
      isSuccess: true,
      fetchNextPage: vi.fn(),
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?item=content-1&source=inbox"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    expect(hooks.detail).toHaveBeenCalledWith("content-1");
    expect(
      screen.getByRole("heading", { name: "内容详情与排期" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByText("关闭").closest("button")!);
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?source=inbox",
    );
  });

  it("offers permanent deletion only for an archived item", () => {
    const archivedItem = {
      ...item,
      status: "archived" as const,
      archivedFromStatus: "scheduled" as const,
    };
    const view = render(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("button", { name: "永久删除" })).toBeNull();

    hooks.detail.mockImplementation((id: string | null) => ({
      data: id ? archivedItem : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(screen.getByRole("button", { name: "永久删除" })).toBeTruthy();
  });

  it("cancels permanent deletion without closing the archived detail", () => {
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id
        ? {
            ...item,
            status: "archived" as const,
            archivedFromStatus: "scheduled" as const,
          }
        : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?item=content-1&source=inbox"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "永久删除" }));
    expect(screen.getByRole("heading", { name: "永久删除内容" })).toBeTruthy();
    expect(screen.getByText(/此操作不可恢复/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    expect(screen.queryByRole("heading", { name: "永久删除内容" })).toBeNull();
    expect(
      screen.getByRole("heading", { name: "内容详情与排期" }),
    ).toBeTruthy();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?item=content-1&source=inbox",
    );
    expect(hooks.remove).not.toHaveBeenCalled();
  });

  it("permanently deletes an archived item and preserves unrelated query parameters", () => {
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id
        ? {
            ...item,
            status: "archived" as const,
            archivedFromStatus: "scheduled" as const,
            version: 3,
          }
        : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    hooks.remove.mockImplementation((_input, options) => options.onSuccess());
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?item=content-1&source=inbox"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "永久删除" }));
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));

    expect(hooks.remove).toHaveBeenCalledWith(
      { id: "content-1", expectedVersion: 3 },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      "/content-calendar?source=inbox",
    );
  });

  it.each([
    ["VERSION_CONFLICT", "内容已在另一个窗口变化，请关闭确认框并刷新后重试。"],
    [
      "CONTENT_ITEM_NOT_ARCHIVED",
      "仅已归档内容可永久删除，请刷新确认当前状态。",
    ],
    [
      "CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES",
      "该内容仍有待处理的收件箱来源，请先到收件箱解决或忽略对应事项，再重试永久删除。",
    ],
    ["CONTENT_ITEM_NOT_FOUND", "该内容已不存在，请关闭详情并刷新内容日历。"],
  ])("maps %s and keeps deletion retryable", (code, message) => {
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id
        ? {
            ...item,
            status: "archived" as const,
            archivedFromStatus: "scheduled" as const,
          }
        : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    const view = render(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    fireEvent.click(screen.getByRole("button", { name: "永久删除" }));
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));
    hooks.removeError = new ApiError("private deletion detail", {
      code,
      requestId: "request-delete-1",
    });
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      `${message} · 请求 ID：request-delete-1`,
    );
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));

    expect(hooks.remove).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("heading", { name: "永久删除内容" })).toBeTruthy();
  });

  it("blocks duplicate permanent deletion while the request is pending", () => {
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id
        ? {
            ...item,
            status: "archived" as const,
            archivedFromStatus: "scheduled" as const,
          }
        : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    }));
    const view = render(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "永久删除" }));
    fireEvent.click(screen.getByRole("button", { name: "确认永久删除" }));

    hooks.removePending = true;
    view.rerender(
      <MemoryRouter initialEntries={["/content-calendar?item=content-1"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    const pending = screen.getByRole("button", {
      name: "正在永久删除…",
    });
    const confirmation = screen.getByRole("dialog", {
      name: "永久删除内容",
    });
    expect(pending).toBeDisabled();
    expect(
      within(confirmation).getByRole("button", { name: "取消" }),
    ).toBeDisabled();
    expect(
      within(confirmation).queryByRole("button", { name: "关闭" }),
    ).toBeNull();
    expect(
      Array.from(document.querySelectorAll(".modal-backdrop")).at(-1)?.tagName,
    ).toBe("DIV");
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("heading", { name: "永久删除内容" })).toBeTruthy();
    fireEvent.click(pending);
    expect(hooks.remove).toHaveBeenCalledTimes(1);
  });

  it("keeps a URL-addressed detail failure retryable", () => {
    const refetch = vi.fn();
    hooks.detail.mockReturnValue({
      data: undefined,
      isError: true,
      isPending: false,
      refetch,
    });

    render(
      <MemoryRouter initialEntries={["/content-calendar?item=missing-item"]}>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    expect(
      screen.getByText("无法读取内容详情，请确认本地服务已连接。"),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("moves an editable item to a visible adjacent-month day with its current version", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.drop(
      screen.getByRole("gridcell", { name: "2026-09-05，0 条内容" }),
      { dataTransfer: { getData: () => "content-1" } },
    );
    expect(hooks.schedule).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "content-1",
        input: expect.objectContaining({
          scheduledAt: "2026-09-05T01:00:00.000Z",
          scheduledTimezone: "Asia/Shanghai",
          expectedVersion: 1,
        }),
      }),
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("moves an editable item one day with the documented keyboard shortcut", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    const card = screen.getByRole("button", { name: "编辑 发布产品更新" });
    expect(card).toHaveAttribute(
      "aria-keyshortcuts",
      "Alt+ArrowLeft Alt+ArrowRight",
    );

    fireEvent.keyDown(card, { altKey: true, key: "ArrowRight" });

    expect(hooks.schedule).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "content-1",
        input: expect.objectContaining({
          scheduledAt: "2026-09-05T01:00:00.000Z",
          scheduledTimezone: "Asia/Shanghai",
          expectedVersion: 1,
        }),
      }),
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("previews a dropped date immediately and rolls back after a save failure", () => {
    const refetch = vi.fn();
    hooks.items.mockReturnValue({
      data: {
        pages: [{ items: [item], meta: { page: 1, pageSize: 100, total: 1 } }],
      },
      isError: false,
      isFetchNextPageError: false,
      isFetchingNextPage: false,
      hasNextPage: false,
      isPending: false,
      isSuccess: true,
      fetchNextPage: vi.fn(),
      refetch,
    });
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    const source = screen.getByRole("gridcell", {
      name: "2026-09-04，1 条内容",
    });
    const target = screen.getByRole("gridcell", {
      name: "2026-09-05，0 条内容",
    });

    fireEvent.drop(target, {
      dataTransfer: { getData: () => "content-1" },
    });

    expect(source).toHaveAccessibleName("2026-09-04，0 条内容");
    expect(target).toHaveAccessibleName("2026-09-05，1 条内容");
    const [, options] = hooks.schedule.mock.calls[0] as [
      unknown,
      { onError: (error: Error) => void },
    ];
    act(() => options.onError(new Error("版本冲突")));
    expect(source).toHaveAccessibleName("2026-09-04，1 条内容");
    expect(target).toHaveAccessibleName("2026-09-05，0 条内容");
    expect(screen.getByText("排期未保存，已恢复原日期。版本冲突")).toBeTruthy();
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("does not duplicate the hook-owned list refresh after a move conflict", () => {
    const refetch = vi.fn();
    hooks.items.mockReturnValue(
      contentItemsResult([item], {
        refetch,
      }),
    );
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );

    fireEvent.drop(
      screen.getByRole("gridcell", { name: "2026-09-05，0 条内容" }),
      { dataTransfer: { getData: () => "content-1" } },
    );
    const [, options] = hooks.schedule.mock.calls[0] as [
      unknown,
      { onError: (error: Error) => void },
    ];
    act(() =>
      options.onError(
        new ApiError("Content item changed", {
          code: "VERSION_CONFLICT",
          status: 409,
        }),
      ),
    );

    expect(
      screen.getByText("排期未保存，已恢复原日期。Content item changed"),
    ).toBeTruthy();
    expect(refetch).not.toHaveBeenCalled();
  });

  it("automatically advances through all visible-range pages", () => {
    const fetchNextPage = vi.fn();
    hooks.items.mockReturnValue({
      data: {
        pages: [
          {
            items: [item],
            meta: { page: 1, pageSize: 100, total: 101 },
          },
        ],
      },
      isError: false,
      isFetchNextPageError: false,
      isFetchingNextPage: false,
      hasNextPage: true,
      isPending: false,
      isSuccess: true,
      fetchNextPage,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("renders items collected from every loaded page", () => {
    const secondItem = {
      ...item,
      id: "content-2",
      title: "跨页内容",
      scheduledAt: "2026-09-05T01:00:00Z",
    };
    hooks.items.mockReturnValue({
      data: {
        pages: [
          {
            items: [item],
            meta: { page: 1, pageSize: 1, total: 2 },
          },
          {
            items: [secondItem],
            meta: { page: 2, pageSize: 1, total: 2 },
          },
        ],
      },
      isError: false,
      isFetchNextPageError: false,
      isFetchingNextPage: false,
      hasNextPage: false,
      isPending: false,
      isSuccess: true,
      fetchNextPage: vi.fn(),
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.getByText("发布产品更新")).toBeTruthy();
    expect(screen.getByText("跨页内容")).toBeTruthy();
    expect(screen.getByText("2 条")).toBeTruthy();
  });
});
