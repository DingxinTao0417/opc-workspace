import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../types/models";
import { InboxPage } from "./InboxPage";

const item: InboxItem = {
  id: "018f0000-0000-7000-8000-000000000801",
  kind: "manual",
  title: "确认本周交付范围",
  summary: "整理需要人工确认的边界",
  sourceEntityType: "manual",
  sourceEntityId: null,
  sourceEventKey: null,
  sourceDeletedAt: null,
  priority: "P1",
  status: "open",
  resolutionPolicy: "manual",
  dueAt: null,
  readAt: null,
  triagedAt: "2026-08-28T10:00:00Z",
  snoozedUntil: null,
  resolvedByActorId: null,
  resolvedAt: null,
  resolutionReason: null,
  resolutionMode: null,
  dismissedByActorId: null,
  dismissedAt: null,
  dismissReason: null,
  payloadJson: {},
  version: 2,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
};

const hooks = vi.hoisted(() => ({
  items: vi.fn(),
  detail: vi.fn(),
  markAll: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  create: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  update: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  command: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useInboxItemsQuery: hooks.items,
  useInboxItemQuery: hooks.detail,
  useInboxItemEventsQuery: () => ({
    data: {
      pages: [
        {
          items: [],
          meta: {
            page: 1,
            pageSize: 20,
            total: 0,
            inboxItemVersion: 2,
          },
        },
      ],
    },
    fetchNextPage: vi.fn(),
    hasNextPage: false,
    isError: false,
    isFetchingNextPage: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useMarkAllInboxItemsRead: () => hooks.markAll,
  useCreateInboxItem: () => hooks.create,
  useUpdateInboxItem: () => hooks.update,
  useInboxItemCommand: () => hooks.command,
}));

describe("InboxPage", () => {
  beforeEach(() => {
    hooks.items.mockReturnValue({
      data: {
        items: [item],
        meta: {
          page: 1,
          pageSize: 30,
          total: 1,
          unreadTotal: 1,
          snapshotAt: "2026-08-28T10:05:00.123456789Z",
          serverNow: "2026-08-28T10:05:00.123456789Z",
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    hooks.detail.mockImplementation((id: string | null) => ({
      data: id ? item : undefined,
      isError: false,
      isPending: false,
      refetch: vi.fn().mockResolvedValue({ data: item }),
    }));
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("renders the prototype hierarchy with real unread facts and opens details", () => {
    render(<InboxPage />);

    expect(screen.getByText("1 条未读")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "今天" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "未读" })).toBeTruthy();
    expect(screen.queryByText("Tony 回复报价")).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "查看 确认本周交付范围" }),
    );
    expect(screen.getByRole("dialog", { name: "收件箱详情" })).toBeTruthy();
    expect(screen.getAllByText("整理需要人工确认的边界")).toHaveLength(2);
  });

  it("passes view, search, priority, and paging facts to the server query", () => {
    render(<InboxPage />);

    fireEvent.click(screen.getByRole("tab", { name: "稍后" }));
    fireEvent.change(screen.getByLabelText("搜索收件箱"), {
      target: { value: "交付" },
    });
    fireEvent.change(screen.getByLabelText("优先级"), {
      target: { value: "P1" },
    });

    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({
        view: "snoozed",
        q: "交付",
        priority: "P1",
        page: 1,
        pageSize: 30,
      }),
    );
  });

  it("uses the exact server snapshot when marking visible items read", () => {
    render(<InboxPage />);

    fireEvent.click(screen.getByRole("button", { name: "全部标为已读" }));
    expect(hooks.markAll.mutate).toHaveBeenCalledWith({
      throughCreatedAt: "2026-08-28T10:05:00.123456789Z",
    });
  });

  it("returns to the last valid page when mutations shrink the result set", async () => {
    let total = 31;
    hooks.items.mockImplementation(
      (input: { page?: number; pageSize?: number }) => ({
        data: {
          items: input.page === 2 && total === 30 ? [] : [item],
          meta: {
            page: input.page ?? 1,
            pageSize: input.pageSize ?? 30,
            total,
            unreadTotal: 1,
            snapshotAt: "2026-08-28T10:05:00.123456789Z",
            serverNow: "2026-08-28T10:05:00.123456789Z",
          },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    const view = render(<InboxPage />);

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
    );

    total = 30;
    view.rerender(<InboxPage />);

    await waitFor(() =>
      expect(hooks.items).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1 }),
      ),
    );
  });
});
