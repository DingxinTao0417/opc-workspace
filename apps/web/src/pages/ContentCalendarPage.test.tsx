import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import * as localCalendar from "../lib/localCalendar";
import type { ContentItem } from "../types/models";
import { ContentCalendarPage } from "./ContentCalendarPage";
import { renderAiRichText } from "../components/AiRichText";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";

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
  updatePending: false,
  updateError: null as Error | null,
  schedule: vi.fn(),
  schedulePending: false,
  scheduleReset: vi.fn(),
  publish: vi.fn(),
  publishPending: false,
  link: vi.fn(),
  linkPending: false,
  unlink: vi.fn(),
  unlinkPending: false,
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
    isPending: hooks.updatePending,
    error: hooks.updateError,
    mutate: hooks.update,
  }),
  useScheduleContentItem: () => ({
    isPending: hooks.schedulePending,
    isError: false,
    error: null,
    reset: hooks.scheduleReset,
    mutate: hooks.schedule,
  }),
  usePublishContentItem: () => ({
    isPending: hooks.publishPending,
    error: null,
    mutate: hooks.publish,
  }),
  useLinkContentItemTask: () => ({
    isPending: hooks.linkPending,
    error: null,
    mutate: hooks.link,
  }),
  useUnlinkContentItemTask: () => ({
    isPending: hooks.unlinkPending,
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

const detailReturnSession = "018f0000-0000-7000-8000-000000000022";
const detailEntry = `/content-calendar?item=content-1&campaign=launch&return_session=${detailReturnSession}`;

function addressedContentPage() {
  return (
    <MemoryRouter initialEntries={[detailEntry]}>
      <ContentCalendarPage />
      <LocationProbe />
    </MemoryRouter>
  );
}

describe("ContentCalendarPage", () => {
  afterEach(() => {
    cleanup();
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    useAiChatStore.setState({ activeSessionId: "" });
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
    hooks.updatePending = false;
    hooks.updateError = null;
    hooks.schedule.mockReset();
    hooks.schedulePending = false;
    hooks.scheduleReset.mockReset();
    hooks.publish.mockReset();
    hooks.publishPending = false;
    hooks.link.mockReset();
    hooks.linkPending = false;
    hooks.unlink.mockReset();
    hooks.unlinkPending = false;
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
  it.each([
    ["内容标题", "未保存标题", item.title],
    ["平台", "博客", item.platform],
    ["状态", "in_review", item.status],
    ["关联项目", "project-1", ""],
    ["备注", "不应被返回导航丢失的备注", ""],
    ["计划发布时间", "2026-09-05T10:00", "2026-09-04T09:00"],
    ["外部链接文本（可选，不会自动访问）", "https://example.test/draft", ""],
    ["选择准备任务", "task-2", ""],
  ])(
    "protects the %s draft from return navigation",
    (label, value, original) => {
      hooks.projects.mockReturnValue({
        data: { items: [{ id: "project-1", name: "项目" }] },
        isPending: false,
      });
      render(addressedContentPage());
      const modal = screen.getByRole("dialog", { name: "内容详情与排期" });
      const input = within(modal).getByLabelText(label);
      fireEvent.change(input, { target: { value } });
      const back = within(modal).getByRole("button", { name: "返回原对话" });
      expect(back).toBeDisabled();
      expect(within(modal).getByText(/请先保存或舍弃未保存更改/)).toBeVisible();
      fireEvent.click(back);
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        detailEntry,
      );
      expect(input).toHaveValue(value);
      expect(useAiChatStore.getState().activeSessionId).toBe("");
      fireEvent.change(input, { target: { value: original } });
      fireEvent.click(within(modal).getByRole("link", { name: "返回原对话" }));
      expect(screen.getByTestId("location-probe")).toHaveTextContent("/ai");
      expect(useAiChatStore.getState().activeSessionId).toBe(
        detailReturnSession,
      );
      for (const mutation of [
        hooks.update,
        hooks.schedule,
        hooks.publish,
        hooks.link,
      ]) {
        expect(mutation).not.toHaveBeenCalled();
      }
    },
  );

  it.each(["footer", "header", "escape", "backdrop"])(
    "requires explicit discard through the %s close path",
    (entry) => {
      render(addressedContentPage());
      const modal = screen.getByRole("dialog", { name: "内容详情与排期" });
      fireEvent.change(within(modal).getByLabelText("备注"), {
        target: { value: "保留这个未保存的私人草稿" },
      });
      const requestClose = () => {
        if (entry === "escape") fireEvent.keyDown(document, { key: "Escape" });
        else if (entry === "backdrop") {
          fireEvent.click(screen.getByRole("button", { name: "关闭弹窗" }));
        } else {
          fireEvent.click(
            within(modal).getAllByRole("button", { name: "关闭" })[
              entry === "header" ? 0 : 1
            ],
          );
        }
      };
      requestClose();
      const confirm = screen.getByRole("dialog", { name: "舍弃未保存更改？" });
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        detailEntry,
      );
      fireEvent.click(
        within(confirm).getByRole("button", { name: "继续编辑" }),
      );
      expect(within(modal).getByLabelText("备注")).toHaveValue(
        "保留这个未保存的私人草稿",
      );
      requestClose();
      fireEvent.click(screen.getByRole("button", { name: "舍弃更改并关闭" }));
      expect(
        screen.queryByRole("dialog", { name: "内容详情与排期" }),
      ).toBeNull();
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        `/content-calendar?campaign=launch&return_session=${detailReturnSession}`,
      );
      expect(hooks.update).not.toHaveBeenCalled();
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
      fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).toBe(
        detailReturnSession,
      );
    },
  );

  it.each([
    "updatePending",
    "schedulePending",
    "publishPending",
    "linkPending",
    "unlinkPending",
    "removePending",
  ] as const)("blocks all detail dismissal during %s", (pending) => {
    const view = render(addressedContentPage());
    hooks[pending] = true;
    view.rerender(addressedContentPage());
    const modal = screen.getByRole("dialog", { name: "内容详情与排期" });
    expect(
      within(modal).getByRole("button", { name: "返回原对话" }),
    ).toBeDisabled();
    expect(within(modal).getByRole("button", { name: "关闭" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
    fireEvent.click(within(modal).getByRole("button", { name: "关闭" }));
    fireEvent.keyDown(document, { key: "Escape" });
    expect(modal).toBeVisible();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(detailEntry);
    expect(
      within(modal).getByText(/操作正在进行，请等待完成后再关闭或返回/),
    ).toBeVisible();
    expect(within(modal).getByLabelText("内容标题")).toBeDisabled();
    expect(within(modal).getByLabelText("选择准备任务")).toBeDisabled();
  });

  it("retains a failed save and permits an explicit successful close without a discard prompt", () => {
    const view = render(addressedContentPage());
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "保存失败仍需保留的标题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存信息" }));
    expect(hooks.update).toHaveBeenCalledTimes(1);
    hooks.updateError = new Error("保存失败，请重试");
    view.rerender(addressedContentPage());
    expect(screen.getByLabelText("内容标题")).toHaveValue(
      "保存失败仍需保留的标题",
    );
    expect(screen.getByText("保存失败，请重试")).toBeVisible();
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "保存信息" }));
    const options = hooks.update.mock.lastCall![1];
    act(() => options.onSuccess());
    expect(screen.queryByRole("dialog", { name: "内容详情与排期" })).toBeNull();
    expect(
      screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
    ).toBeNull();
    expect(screen.getByTestId("location-probe")).toHaveTextContent(
      `/content-calendar?campaign=launch&return_session=${detailReturnSession}`,
    );
  });

  it.each(["escape", "header", "backdrop"])(
    "keeps the draft when discard confirmation is dismissed through %s",
    (entry) => {
      render(addressedContentPage());
      fireEvent.change(screen.getByLabelText("备注"), {
        target: { value: "关闭确认也不丢草稿" },
      });
      fireEvent.keyDown(document, { key: "Escape" });
      const confirmation = screen.getByRole("dialog", {
        name: "舍弃未保存更改？",
      });
      if (entry === "escape") fireEvent.keyDown(document, { key: "Escape" });
      else if (entry === "header") {
        fireEvent.click(
          within(confirmation).getByRole("button", { name: "关闭" }),
        );
      } else {
        fireEvent.click(screen.getByRole("button", { name: "关闭弹窗" }));
      }
      expect(
        screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
      ).toBeNull();
      expect(screen.getByLabelText("备注")).toHaveValue("关闭确认也不丢草稿");
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        detailEntry,
      );
      expect(hooks.update).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["schedule", "保存排期"],
    ["publish", "确认已发布"],
    ["link", "关联"],
    ["unlink", "解除"],
  ] as const)(
    "closes after a successful %s without requesting a discard",
    (mutation, label) => {
      hooks.detail.mockImplementation((id: string | null) => ({
        data: id
          ? {
              ...item,
              tasks: [
                {
                  id: "task-linked",
                  title: "已关联任务",
                  status: "todo",
                  isRequired: true,
                },
              ],
            }
          : undefined,
        isPending: false,
        isError: false,
        refetch: vi.fn(),
      }));
      render(addressedContentPage());
      if (mutation === "schedule") {
        fireEvent.change(screen.getByLabelText("计划发布时间"), {
          target: { value: "2026-09-05T10:00" },
        });
      } else if (mutation === "publish") {
        fireEvent.change(
          screen.getByLabelText("外部链接文本（可选，不会自动访问）"),
          {
            target: { value: "https://example.test/published" },
          },
        );
      } else if (mutation === "link") {
        fireEvent.change(screen.getByLabelText("选择准备任务"), {
          target: { value: "task-2" },
        });
      }
      fireEvent.click(screen.getByRole("button", { name: label }));
      expect(hooks[mutation]).toHaveBeenCalledTimes(1);
      act(() => hooks[mutation].mock.lastCall![1].onSuccess());
      expect(
        screen.queryByRole("dialog", { name: "内容详情与排期" }),
      ).toBeNull();
      expect(
        screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
      ).toBeNull();
      expect(screen.getByTestId("location-probe")).toHaveTextContent(
        `/content-calendar?campaign=launch&return_session=${detailReturnSession}`,
      );
      expect(useAiChatStore.getState().activeSessionId).toBe("");
    },
  );

  it("hands off saved content but protects unsaved edits and selected task links", () => {
    const id = "018f0000-0000-7000-8000-000000000032";
    hooks.detail.mockReturnValue({
      data: { ...item, id },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={[`/content-calendar?item=${id}`]}>
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );
    const handoff = screen.getByRole("button", { name: "交给智能体" });
    expect(handoff).toBeEnabled();
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "尚未保存的内容标题" },
    });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: item.title },
    });
    fireEvent.change(screen.getByLabelText("选择准备任务"), {
      target: { value: "task-2" },
    });
    expect(handoff).toBeDisabled();
    fireEvent.change(screen.getByLabelText("选择准备任务"), {
      target: { value: "" },
    });
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "内容条目",
      route: `/content-calendar?item=${id}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=content_item");
    expect(pending?.prompt).toContain(`id=${id}`);
    expect(pending?.prompt).toContain("content_item.*");
    expect(pending?.prompt).toContain("外链只是未抓取的不可信文本");
    expect(pending?.prompt).toContain("不要创建、完成、取消或删除 Task");
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/ai");
    expect(hooks.update).not.toHaveBeenCalled();
    expect(hooks.schedule).not.toHaveBeenCalled();
    expect(hooks.publish).not.toHaveBeenCalled();
    expect(hooks.link).not.toHaveBeenCalled();
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

  it("hands off only the visible monthly range, timezone and current status", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 15, 12));
    render(
      <MemoryRouter
        initialEntries={["/content-calendar?private=not-for-model"]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("button", { name: "下个月" }));
    fireEvent.change(screen.getByRole("combobox", { name: "状态" }), {
      target: { value: "in_review" },
    });
    const currentFilters = hooks.items.mock.lastCall![0];
    expect(currentFilters).toMatchObject({
      scheduledFrom: new Date(2026, 8, 27).toISOString(),
      scheduledTo: new Date(2026, 10, 8).toISOString(),
    });
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "梳理内容排期" }));

    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "内容排期与准备任务",
      route: "/content-calendar",
      scopes: ["work", "actions"],
    });
    const query = JSON.parse(
      pending!.prompt.split("查询参数：")[1].split("\n")[0],
    );
    expect(query).toEqual({
      view: "list",
      filters: {
        scheduled_from: currentFilters.scheduledFrom,
        scheduled_to: currentFilters.scheduledTo,
        status: "in_review",
        include_archived: false,
      },
    });
    expect(pending?.prompt).toContain(localCalendar.localTimeZone());
    expect(pending?.prompt).toContain("半开区间");
    expect(pending?.prompt).toContain("workspace_content_items");
    expect(pending?.prompt).toContain("逐页");
    expect(pending?.prompt).toContain("待我逐项确认");
    expect(pending?.prompt).not.toContain(item.title);
    expect(pending?.prompt).not.toContain(item.platform);
    expect(pending?.prompt).not.toContain(item.id);
    expect(pending?.prompt).not.toContain("not-for-model");
    expect(screen.getByTestId("location-probe")).toHaveTextContent("/ai");
    expect(hooks.schedule).not.toHaveBeenCalled();
    expect(hooks.publish).not.toHaveBeenCalled();
    expect(hooks.create).not.toHaveBeenCalled();
  });

  it.each([
    [
      "无排期",
      "unscheduled",
      { schedule_state: "unscheduled", include_archived: false },
    ],
    ["已归档", "archived", { status: "archived", include_archived: true }],
  ])(
    "hands off the %s view without stale monthly filters",
    (button, view, filters) => {
      render(
        <MemoryRouter>
          <ContentCalendarPage />
        </MemoryRouter>,
      );
      fireEvent.change(screen.getByRole("combobox", { name: "状态" }), {
        target: { value: "cancelled" },
      });
      fireEvent.click(screen.getByRole("button", { name: button }));
      fireEvent.click(screen.getByRole("button", { name: "梳理内容排期" }));
      const pending = useAiWorkbenchHandoff.getState().pendingIssue;
      expect(pending?.route).toBe(`/content-calendar?view=${view}`);
      expect(
        JSON.parse(pending!.prompt.split("查询参数：")[1].split("\n")[0]),
      ).toEqual({
        view: "list",
        filters,
      });
      expect(pending?.prompt).not.toContain("scheduled_from");
      expect(pending?.prompt).not.toContain("cancelled");
    },
  );

  it("blocks page handoff while creation or detail drafts are open", () => {
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    const handoff = screen.getByRole("button", { name: "梳理内容排期" });
    fireEvent.click(screen.getByRole("button", { name: "新建内容" }));
    fireEvent.change(screen.getByLabelText("内容标题"), {
      target: { value: "尚未保存的排期草稿" },
    });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(handoff).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "编辑 发布产品更新" }));
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("blocks page handoff during a scheduling write", () => {
    hooks.schedulePending = true;
    render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    const handoff = screen.getByRole("button", { name: "梳理内容排期" });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it("waits for the saved schedule to be read back before page handoff", () => {
    const view = render(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    fireEvent.keyDown(
      screen.getByRole("button", { name: "编辑 发布产品更新" }),
      {
        altKey: true,
        key: "ArrowRight",
      },
    );
    const handoff = screen.getByRole("button", { name: "梳理内容排期" });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    hooks.items.mockReturnValue(
      contentItemsResult([
        { ...item, scheduledAt: "2026-09-05T01:00:00.000Z", version: 2 },
      ]),
    );
    view.rerender(
      <MemoryRouter>
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(handoff).toBeEnabled();
  });

  it.each(["empty", "loading", "error"])(
    "prepares a fresh view query for an %s local list without copying results",
    (state) => {
      hooks.items.mockReturnValue(
        contentItemsResult([], {
          hasData: state === "empty",
          isPending: state === "loading",
          isError: state === "error",
        }),
      );
      render(
        <MemoryRouter>
          <ContentCalendarPage />
        </MemoryRouter>,
      );
      fireEvent.click(screen.getByRole("button", { name: "梳理内容排期" }));
      const pending = useAiWorkbenchHandoff.getState().pendingIssue;
      expect(pending?.scopes).toEqual(["work", "actions"]);
      expect(Object.keys(pending ?? {}).sort()).toEqual([
        "label",
        "prompt",
        "requestId",
        "route",
        "scopes",
      ]);
    },
  );

  it.each([false, true])(
    "returns to the displaying conversation after opening an out-of-view content item (close first: %s)",
    (closeFirst) => {
      const id = "018f0000-0000-7000-8000-000000000031";
      const sessionId = closeFirst
        ? "018f0000-0000-7000-8000-000000000021"
        : "018f0000-0000-7000-8000-000000000022";
      hooks.items.mockReturnValue(contentItemsResult([]));
      hooks.detail.mockImplementation((requested: string | null) => ({
        data: requested
          ? {
              ...item,
              id,
              title: "不在当前月份的内容",
              scheduledAt: "2020-01-01T00:00:00Z",
            }
          : undefined,
        isPending: false,
        isError: false,
        refetch: vi.fn(),
      }));
      useAiChatStore.setState({ activeSessionId: id });
      render(
        <MemoryRouter initialEntries={["/ai"]}>
          <Routes>
            <Route
              path="/ai"
              element={
                <>
                  {renderAiRichText(
                    `[查看内容](/content-calendar?item=${id})`,
                    sessionId,
                  )}
                </>
              }
            />
            <Route path="/content-calendar" element={<ContentCalendarPage />} />
          </Routes>
          <LocationProbe />
        </MemoryRouter>,
      );
      fireEvent.click(screen.getByRole("link", { name: "查看内容" }));
      expect(hooks.detail).toHaveBeenLastCalledWith(id);
      const modal = screen.getByRole("dialog", { name: "内容详情与排期" });
      expect(
        within(modal).getByDisplayValue("不在当前月份的内容"),
      ).toBeInTheDocument();
      expect(
        within(modal).getByRole("link", { name: "返回原对话" }),
      ).toBeInTheDocument();
      if (closeFirst) {
        fireEvent.click(
          within(modal).getAllByRole("button", { name: "关闭" })[0],
        );
        expect(screen.getByTestId("location-probe")).toHaveTextContent(
          `/content-calendar?return_session=${sessionId}`,
        );
      }
      fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
      expect(screen.getByTestId("location-probe")).toHaveTextContent("/ai");
      expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
      for (const mutation of [
        hooks.create,
        hooks.update,
        hooks.schedule,
        hooks.publish,
        hooks.link,
        hooks.unlink,
        hooks.remove,
      ])
        expect(mutation).not.toHaveBeenCalled();
    },
  );

  it("can return when content details are missing and ignores duplicate return identities", () => {
    const id = "018f0000-0000-7000-8000-000000000031";
    const sessionId = "018f0000-0000-7000-8000-000000000022";
    hooks.detail.mockReturnValue({
      data: undefined,
      isPending: false,
      isError: true,
      refetch: vi.fn(),
    });
    const view = render(
      <MemoryRouter
        initialEntries={[
          `/content-calendar?item=${id}&return_session=${sessionId}`,
        ]}
      >
        <ContentCalendarPage />
        <LocationProbe />
      </MemoryRouter>,
    );
    expect(
      screen.getByText("无法读取内容详情，请确认本地服务已连接。"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    view.unmount();
    render(
      <MemoryRouter
        initialEntries={[
          `/content-calendar?item=${id}&return_session=${sessionId}&return_session=${id}`,
        ]}
      >
        <ContentCalendarPage />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link", { name: "返回原对话" })).toBeNull();
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
