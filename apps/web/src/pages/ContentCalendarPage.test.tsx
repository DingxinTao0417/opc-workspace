import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as localCalendar from "../lib/localCalendar";
import type { ContentItem } from "../types/models";
import { ContentCalendarPage } from "./ContentCalendarPage";

const hooks = vi.hoisted(() => ({
  items: vi.fn(),
  detail: vi.fn(),
  projects: vi.fn(),
  tasks: vi.fn(),
  update: vi.fn(),
  schedule: vi.fn(),
  scheduleReset: vi.fn(),
  publish: vi.fn(),
  link: vi.fn(),
  unlink: vi.fn(),
}));
vi.mock("../api/hooks", () => ({
  useContentItemsInfiniteQuery: hooks.items,
  useContentItemQuery: hooks.detail,
  useProjectsQuery: hooks.projects,
  useTasksQuery: hooks.tasks,
  useCreateContentItem: () => ({
    isPending: false,
    isError: false,
    mutate: vi.fn(),
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
    hooks.update.mockReset();
    hooks.schedule.mockReset();
    hooks.scheduleReset.mockReset();
    hooks.publish.mockReset();
    hooks.link.mockReset();
    hooks.unlink.mockReset();
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
