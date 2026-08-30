import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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
  useInboxItemTasksQuery: () => ({
    data: {
      pages: [
        {
          active: [],
          history: [],
          meta: {
            page: 1,
            pageSize: 20,
            total: 0,
            inboxItemVersion: 2,
            progress: {
              activeTotal: 0,
              requiredTotal: 0,
              requiredDone: 0,
              requiredRemaining: 0,
              requiredBlocked: 0,
              requiredWaitingReview: 0,
              requiredCancelled: 0,
              percent: null,
              allRequiredDone: false,
            },
          },
        },
      ],
    },
    fetchNextPage: vi.fn(),
    hasNextPage: false,
    isError: false,
    isFetchingNextPage: false,
    isPending: false,
    refetch: vi.fn().mockResolvedValue({ data: undefined }),
  }),
  useTaskPageQuery: () => ({
    data: undefined,
    isError: false,
    isFetching: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useLinkInboxItemTask: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUpdateInboxItemTaskRequirement: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useUnlinkInboxItemTask: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useForceResolveInboxItem: () => ({
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  }),
  useMarkAllInboxItemsRead: () => hooks.markAll,
  useCreateInboxItem: () => hooks.create,
  useUpdateInboxItem: () => hooks.update,
  useInboxItemCommand: () => hooks.command,
}));

describe("InboxPage", () => {
  const renderInbox = (initialEntry = "/inbox") =>
    render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route element={<InboxPage />} path="/inbox" />
          <Route element={<InboxPage />} path="/inbox/:inboxItemId" />
        </Routes>
      </MemoryRouter>,
    );

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
    renderInbox();

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

  it("uses the Invoice icon and stage summary when an Invoice event has no summary", () => {
    const invoiceId = "018f0000-0000-7000-8000-000000000826";
    const invoiceItem: InboxItem = {
      ...item,
      kind: "event",
      title: "发票到期：INV-2026-0829",
      summary: "",
      sourceEntityType: "invoice_due",
      sourceEntityId: invoiceId,
      sourceEventKey: "invoice:" + invoiceId + ":due:2026-08-28",
      dueAt: "2026-08-28T09:00:00+08:00",
      payloadJson: {
        invoice_id: invoiceId,
        invoice_number: "INV-2026-0829",
        client_id: "018f0000-0000-7000-8000-000000000827",
        client_name: "星河设计事务所",
        project_id: null,
        project_name: null,
        amount_minor: 128045,
        currency: "CNY",
        due_date: "2026-08-28",
        due_state: "due",
        occurrence_date: "2026-08-28",
        invoice_version: 4,
        projected_at: "2026-08-28T08:00:00Z",
        lead_days: 3,
      },
    };
    hooks.items.mockReturnValue({
      data: {
        items: [invoiceItem],
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

    const { container } = renderInbox();

    expect(screen.getByText("发票到期")).toBeTruthy();
    expect(container.querySelector("svg.lucide-receipt-text")).toBeTruthy();
  });

  it("opens the exact inbox item from a refreshable detail route", async () => {
    renderInbox(`/inbox/${item.id}`);

    expect(
      await screen.findByRole("dialog", { name: "收件箱详情" }),
    ).toBeVisible();
    expect(hooks.detail).toHaveBeenCalledWith(item.id);
  });

  it("passes view, search, priority, and paging facts to the server query", () => {
    renderInbox();

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
    renderInbox();

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
    const view = renderInbox();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(hooks.items).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
    );

    total = 30;
    view.rerender(
      <MemoryRouter>
        <InboxPage />
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(hooks.items).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1 }),
      ),
    );
  });

  it("applies a risk deep link and exposes the matching filter", async () => {
    renderInbox("/inbox?risk=blocked");

    await waitFor(() =>
      expect(hooks.items).toHaveBeenLastCalledWith(
        expect.objectContaining({ view: "inbox", risk: "blocked" }),
      ),
    );
    expect(screen.getByLabelText("跟进状态")).toHaveValue("blocked");
  });

  it("lists a backup-create maintenance item with its safe title", () => {
    const maintenanceItem: InboxItem = {
      ...item,
      kind: "event",
      title: "本地备份需要处理",
      summary:
        "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:create",
      sourceEventKey:
        "system:backup:create:018f0000-0000-7000-8000-000000000818",
      payloadJson: {
        component: "backup",
        operation: "create",
        failure_code: "backup_create_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      },
    };
    hooks.items.mockReturnValue({
      data: {
        items: [maintenanceItem],
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

    renderInbox();

    expect(
      screen.getByRole("button", { name: "查看 本地备份需要处理" }),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
      ),
    ).toBeTruthy();
  });

  it("lists a backup-verify maintenance item with its safe title", () => {
    const maintenanceItem: InboxItem = {
      ...item,
      kind: "event",
      title: "本地备份校验需要处理",
      summary:
        "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      sourceEntityType: "system_maintenance",
      sourceEntityId: "backup:verify",
      sourceEventKey:
        "system:backup:verify:018f0000-0000-7000-8000-000000000819",
      payloadJson: {
        component: "backup",
        operation: "verify",
        failure_code: "backup_verify_failed",
        occurred_at: "2026-08-28T12:00:00.000000000Z",
        message:
          "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
      },
    };
    hooks.items.mockReturnValue({
      data: {
        items: [maintenanceItem],
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

    renderInbox();

    expect(
      screen.getByRole("button", { name: "查看 本地备份校验需要处理" }),
    ).toBeTruthy();
  });
});
