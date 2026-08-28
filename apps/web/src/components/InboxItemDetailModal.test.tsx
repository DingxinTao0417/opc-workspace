import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { InboxItem } from "../types/models";
import { InboxItemDetailModal } from "./InboxItemDetailModal";

const baseItem: InboxItem = {
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
  detail: vi.fn(),
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
  useInboxItemQuery: hooks.detail,
  useUpdateInboxItem: () => hooks.update,
  useInboxItemCommand: () => hooks.command,
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
}));

describe("InboxItemDetailModal", () => {
  beforeEach(() => {
    hooks.detail.mockReturnValue({
      data: baseItem,
      isError: false,
      isPending: false,
      refetch: vi.fn().mockResolvedValue({
        data: { ...baseItem, version: 3, title: "服务端新标题" },
      }),
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    hooks.update.isPending = false;
    hooks.command.isPending = false;
  });

  it("uses available actions when an expired snooze timestamp is retained", () => {
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        snoozedUntil: "2026-08-27T10:00:00Z",
        availableActions: ["edit", "read", "snooze", "resolve", "dismiss"],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getByRole("button", { name: "稍后处理" })).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "恢复待处理" }),
    ).not.toBeInTheDocument();
  });

  it("lets an unread archived item be marked read without reopening it", () => {
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        status: "resolved",
        resolvedByActorId: "00000000-0000-0000-0000-000000000001",
        resolvedAt: "2026-08-28T10:05:00Z",
        resolutionReason: "已经完成线下确认",
        resolutionMode: "manual",
        availableActions: ["read", "reopen"],
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "标为已读" }));

    expect(screen.getByRole("button", { name: "重新打开" })).toBeTruthy();
    expect(hooks.command.mutate).toHaveBeenCalledWith(
      {
        action: "read",
        id: baseItem.id,
        expectedVersion: 2,
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("treats an empty server action set as authoritative", () => {
    hooks.detail.mockReturnValue({
      data: { ...baseItem, availableActions: [] },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    for (const name of [
      "标为已读",
      "编辑",
      "稍后处理",
      "恢复待处理",
      "标记解决",
      "忽略",
      "重新打开",
    ]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
  });

  it("keeps the edit draft after a version conflict and loads the latest version", async () => {
    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "仍要保留的本地草稿" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    const options = hooks.update.mutate.mock.calls[0][1];
    options.onError(
      new ApiError("version conflict", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("编辑草稿未被覆盖"),
    );
    expect(screen.getByLabelText("标题")).toHaveValue("仍要保留的本地草稿");
    expect(hooks.update.mutate.mock.calls[0][0]).toEqual({
      id: baseItem.id,
      input: expect.objectContaining({ expectedVersion: 2 }),
    });
  });

  it("keeps the draft and adopts a later successful refresh after conflict recovery fails", async () => {
    const failedRefresh = vi
      .fn()
      .mockResolvedValue({ data: baseItem, isError: true });
    hooks.detail.mockReturnValue({
      data: baseItem,
      isError: false,
      isPending: false,
      refetch: failedRefresh,
    });
    const view = render(
      <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "冲突后仍保留的草稿" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    hooks.update.mutate.mock.calls[0][1].onError(
      new ApiError("version conflict", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "未能读取服务器最新版本",
      ),
    );

    const latestItem = { ...baseItem, title: "服务端更新后的标题", version: 6 };
    hooks.detail.mockReturnValue({
      data: latestItem,
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    view.rerender(
      <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />,
    );

    expect(screen.getByLabelText("标题")).toHaveValue("冲突后仍保留的草稿");
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(hooks.update.mutate.mock.calls[1][0]).toEqual({
      id: baseItem.id,
      input: expect.objectContaining({ expectedVersion: 6 }),
    });
  });

  it("does not silently adopt a background version while an unconflicted draft is open", () => {
    const view = render(
      <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "仍基于旧版本的编辑草稿" },
    });

    hooks.detail.mockReturnValue({
      data: { ...baseItem, title: "另一窗口的新标题", version: 6 },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    view.rerender(
      <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    expect(hooks.update.mutate.mock.calls[0][0]).toEqual({
      id: baseItem.id,
      input: expect.objectContaining({ expectedVersion: 2 }),
    });
  });

  it("requires an audit reason and sends a versioned resolve command", () => {
    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "标记解决" }));
    fireEvent.change(screen.getByLabelText("解决原因"), {
      target: { value: "已经线下确认" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(hooks.command.mutate).toHaveBeenCalledWith(
      {
        action: "resolve",
        id: baseItem.id,
        reason: "已经线下确认",
        expectedVersion: 2,
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSuccess: expect.any(Function),
      }),
    );
  });

  it("delegates snooze future-time validation to the server clock", () => {
    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "稍后处理" }));
    fireEvent.change(screen.getByLabelText("稍后至"), {
      target: { value: "2020-01-01T00:00" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认" }));

    expect(hooks.command.mutate).toHaveBeenCalledWith(
      {
        action: "snooze",
        id: baseItem.id,
        snoozedUntil: new Date("2020-01-01T00:00").toISOString(),
        expectedVersion: 2,
      },
      expect.any(Object),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("disables repeated actions while a command is pending", () => {
    hooks.command.isPending = true;
    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getByRole("button", { name: "标为已读" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "标记解决" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "关闭" })).toBeTruthy();
  });

  it("labels a backup-create maintenance item as system maintenance", () => {
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
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
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getAllByText("系统维护").length).toBeGreaterThan(0);
    expect(screen.getByText("本地备份创建失败")).toBeTruthy();
    expect(screen.getByRole("button", { name: "打开数据与备份" })).toBeTruthy();
  });

  it("labels a backup-verify maintenance item as system maintenance", () => {
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
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
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getAllByText("系统维护").length).toBeGreaterThan(0);
    expect(screen.getByText("本地备份校验失败")).toBeTruthy();
    expect(screen.getByRole("button", { name: "打开数据与备份" })).toBeTruthy();
  });
});
