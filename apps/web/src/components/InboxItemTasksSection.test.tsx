import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import { useUiStore } from "../store/ui";
import type { InboxItem, InboxItemTaskRelation, Task } from "../types/models";
import { InboxItemTasksSection } from "./InboxItemTasksSection";

const item: InboxItem = {
  id: "inbox-1",
  kind: "manual",
  title: "确认发布范围",
  summary: "",
  sourceEntityType: "manual",
  sourceEntityId: null,
  sourceEventKey: null,
  sourceDeletedAt: null,
  priority: "P2",
  status: "open",
  resolutionPolicy: "manual",
  dueAt: null,
  readAt: null,
  triagedAt: null,
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

const candidate: Task = {
  id: "task-1",
  title: "整理发布清单",
  description: "",
  kind: "work",
  status: "cancelled",
  priority: "P1",
  projectId: null,
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
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  completedAt: null,
  submittedAt: null,
  reviewedAt: null,
  currentSubmissionId: null,
  tags: [],
};

const progress = {
  activeTotal: 0,
  requiredTotal: 0,
  requiredDone: 0,
  requiredRemaining: 0,
  requiredBlocked: 0,
  requiredWaitingReview: 0,
  requiredCancelled: 0,
  percent: null,
  allRequiredDone: false,
};

const actor = {
  id: "actor-owner",
  type: "owner" as const,
  displayName: "我",
  status: "active" as const,
  isBuiltin: true,
  version: 1,
};

function taskRelation(
  overrides: Partial<InboxItemTaskRelation> = {},
): InboxItemTaskRelation {
  return {
    id: "relation-1",
    inboxItemId: item.id,
    taskRefId: candidate.id,
    taskId: candidate.id,
    taskTitleSnapshot: candidate.title,
    task: {
      id: candidate.id,
      title: candidate.title,
      status: candidate.status,
      priority: candidate.priority,
      kind: candidate.kind,
      projectId: candidate.projectId,
      projectName: candidate.projectName ?? null,
      version: candidate.version,
    },
    relationType: "linked",
    isRequired: true,
    position: 1,
    linkedByActorId: actor.id,
    linkedByActor: actor,
    linkedAt: "2026-08-28T10:10:00Z",
    unlinkedByActorId: null,
    unlinkedByActor: null,
    unlinkedAt: null,
    unlinkReason: null,
    isActive: true,
    taskDeleted: false,
    ...overrides,
  };
}

const mocks = vi.hoisted(() => ({
  relations: vi.fn(),
  tasks: vi.fn(),
  link: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  requirement: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  unlink: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
  forceResolve: {
    error: null as unknown,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useInboxItemTasksQuery: mocks.relations,
  useTaskPageQuery: mocks.tasks,
  useLinkInboxItemTask: () => mocks.link,
  useUpdateInboxItemTaskRequirement: () => mocks.requirement,
  useUnlinkInboxItemTask: () => mocks.unlink,
  useForceResolveInboxItem: () => mocks.forceResolve,
}));

function relationPages(
  version = 2,
  active: InboxItemTaskRelation[] = [],
  history: InboxItemTaskRelation[] = [],
) {
  return {
    pages: [
      {
        active,
        history,
        meta: {
          page: 1,
          pageSize: 20,
          total: 0,
          inboxItemVersion: version,
          progress,
        },
      },
    ],
  };
}

describe("InboxItemTasksSection", () => {
  beforeEach(() => {
    useUiStore.setState({ taskDetailId: null });
    mocks.relations.mockReturnValue({
      data: relationPages(),
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn().mockResolvedValue({ data: relationPages(4) }),
    });
    mocks.tasks.mockReturnValue({
      data: {
        items: [candidate],
        meta: { page: 1, pageSize: 20, total: 1 },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn(),
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    mocks.link.mutate.mockReset();
    mocks.requirement.mutate.mockReset();
    mocks.unlink.mutate.mockReset();
    mocks.link.error = null;
    mocks.requirement.error = null;
    mocks.unlink.error = null;
    mocks.forceResolve.error = null;
    mocks.forceResolve.mutate.mockReset();
  });

  it("keeps cancelled tasks selectable and sends the Inbox version", () => {
    render(<InboxItemTasksSection item={item} />);

    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    expect(
      screen.getByText(
        "仅建立与已有任务的关系；如需一次创建多项工作，请使用“拆分并分派”。",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    expect(mocks.link.mutate).toHaveBeenCalledWith(
      {
        inboxItemId: "inbox-1",
        taskId: "task-1",
        isRequired: true,
        expectedVersion: 2,
      },
      expect.any(Object),
    );
  });

  it("opens an active linked Task in the shared Task detail", () => {
    mocks.relations.mockReturnValue({
      data: relationPages(2, [taskRelation()]),
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn(),
    });

    render(<InboxItemTasksSection item={item} />);
    fireEvent.click(
      screen.getByRole("button", { name: `打开任务“${candidate.title}”` }),
    );

    expect(useUiStore.getState().taskDetailId).toBe(candidate.id);
  });

  it("preserves the selected task after conflict and retries with the refreshed version", async () => {
    mocks.link.mutate
      .mockImplementationOnce((_input, options) => {
        options.onError(
          new ApiError("版本冲突", { status: 409, code: "VERSION_CONFLICT" }),
        );
      })
      .mockImplementationOnce(() => undefined);
    render(<InboxItemTasksSection item={item} />);

    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    const taskButton = screen.getByRole("button", { name: /整理发布清单/ });
    fireEvent.click(taskButton);
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "仍有效的选择和填写内容已保留",
      ),
    );
    expect(taskButton).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));
    expect(mocks.link.mutate.mock.calls[1][0]).toMatchObject({
      taskId: "task-1",
      expectedVersion: 4,
    });
  });

  it("keeps the draft but blocks replay when the conflict refresh fails", async () => {
    mocks.relations.mockReturnValue({
      data: relationPages(),
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      refetch: vi.fn().mockRejectedValue(new Error("offline")),
    });
    mocks.link.mutate.mockImplementation((_input, options) => {
      options.onError(
        new ApiError("版本冲突", { status: 409, code: "VERSION_CONFLICT" }),
      );
    });
    render(<InboxItemTasksSection item={item} />);

    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "未能读取服务器最新版本",
      ),
    );
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "重新读取" }),
    ).toBeInTheDocument();
  });

  it("blocks replay when the Inbox item refresh fails after a conflict", async () => {
    mocks.link.mutate.mockImplementation((_input, options) => {
      options.onError(
        new ApiError("版本冲突", { status: 409, code: "VERSION_CONFLICT" }),
      );
    });
    render(
      <InboxItemTasksSection
        item={item}
        onRefreshItem={() => Promise.reject(new Error("offline"))}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));
    fireEvent.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "未能读取服务器最新版本",
      ),
    );
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();
  });

  it("disables an already-open relation editor when its parent is disabled", () => {
    const { rerender } = render(<InboxItemTasksSection item={item} />);
    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));

    rerender(<InboxItemTasksSection disabled item={item} />);

    expect(screen.getByPlaceholderText("按任务标题搜索…")).toBeDisabled();
    expect(screen.getByRole("button", { name: /整理发布清单/ })).toBeDisabled();
    expect(
      screen.getByRole("checkbox", { name: /作为必需任务/ }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();
    fireEvent.submit(screen.getByRole("button", { name: "确认关联" }));
    expect(mocks.link.mutate).not.toHaveBeenCalled();
  });

  it("clears a hidden task selection after searching or paging", () => {
    mocks.tasks.mockReturnValue({
      data: {
        items: [candidate],
        meta: { page: 1, pageSize: 20, total: 21 },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn(),
    });
    render(<InboxItemTasksSection item={item} />);
    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    const taskButton = screen.getByRole("button", { name: /整理发布清单/ });

    fireEvent.click(taskButton);
    expect(screen.getByRole("button", { name: "确认关联" })).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "下一页任务" }));
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));
    expect(screen.getByRole("button", { name: "确认关联" })).toBeEnabled();
    fireEvent.change(screen.getByPlaceholderText("按任务标题搜索…"), {
      target: { value: "另一项任务" },
    });
    expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled();
    expect(screen.getByRole("button", { name: /整理发布清单/ })).toBeDisabled();
  });

  it("disables open requirement and unlink editors with their parent", () => {
    mocks.relations.mockReturnValue({
      data: relationPages(2, [taskRelation()]),
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn(),
    });
    const { rerender } = render(<InboxItemTasksSection item={item} />);

    fireEvent.click(screen.getByRole("button", { name: "改为可选" }));
    rerender(<InboxItemTasksSection disabled item={item} />);
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "确认修改" })).toBeDisabled();

    rerender(<InboxItemTasksSection item={item} />);
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(
      screen.getByRole("button", {
        name: "解除与任务“整理发布清单”的关联",
      }),
    );
    fireEvent.change(screen.getByPlaceholderText("说明为什么不再跟踪此任务…"), {
      target: { value: "不再需要" },
    });
    rerender(<InboxItemTasksSection disabled item={item} />);
    expect(
      screen.getByPlaceholderText("说明为什么不再跟踪此任务…"),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "确认解除" })).toBeDisabled();
    expect(mocks.requirement.mutate).not.toHaveBeenCalled();
    expect(mocks.unlink.mutate).not.toHaveBeenCalled();
  });

  it("invalidates the selection when the task becomes actively linked", async () => {
    let data = relationPages();
    mocks.relations.mockImplementation(() => ({
      data,
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn().mockResolvedValue({ data }),
    }));
    const { rerender } = render(<InboxItemTasksSection item={item} />);
    fireEvent.click(screen.getByRole("button", { name: "关联已有任务" }));
    fireEvent.click(screen.getByRole("button", { name: /整理发布清单/ }));
    expect(screen.getByRole("button", { name: "确认关联" })).toBeEnabled();

    data = relationPages(3, [taskRelation()]);
    rerender(<InboxItemTasksSection item={{ ...item, version: 3 }} />);

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "确认关联" })).toBeDisabled(),
    );
    expect(screen.queryByRole("button", { name: /^整理发布清单/ })).toBeNull();
  });

  it("marks deleted tasks in the unlinked history", () => {
    const deletedHistory = taskRelation({
      taskId: null,
      task: null,
      unlinkedByActorId: actor.id,
      unlinkedByActor: actor,
      unlinkedAt: "2026-08-28T10:20:00Z",
      unlinkReason: "不再需要",
      isActive: false,
      taskDeleted: true,
    });
    mocks.relations.mockReturnValue({
      data: relationPages(4, [], [deletedHistory]),
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isError: false,
      isFetchingNextPage: false,
      isPending: false,
      isPlaceholderData: false,
      refetch: vi.fn(),
    });

    render(<InboxItemTasksSection item={item} />);

    expect(screen.getByText(/原任务已删除 · 不再需要/)).toBeInTheDocument();
  });

  it("renders terminal Inbox items as read-only", () => {
    render(<InboxItemTasksSection item={{ ...item, status: "resolved" }} />);
    expect(
      screen.queryByRole("button", { name: "关联已有任务" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("该条目已结束，仅保留已有关系供查看。"),
    ).toBeInTheDocument();
  });

  it("requires an explicit reason before force resolving an automatic item", () => {
    const automaticItem: InboxItem = {
      ...item,
      status: "tracking",
      resolutionPolicy: "all_required_tasks_done",
      availableActions: ["edit", "read", "snooze", "force-resolve", "dismiss"],
    };
    mocks.relations.mockReturnValue({
      data: {
        pages: [
          {
            ...relationPages(2, [taskRelation()]).pages[0],
            meta: {
              ...relationPages(2).pages[0].meta,
              progress: {
                ...progress,
                activeTotal: 1,
                requiredTotal: 1,
                requiredRemaining: 1,
                requiredCancelled: 1,
                percent: 0,
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
      isPlaceholderData: false,
      refetch: vi.fn(),
    });

    render(<InboxItemTasksSection item={automaticItem} />);

    fireEvent.click(screen.getByRole("button", { name: "例外：强制解决" }));
    expect(screen.getByRole("button", { name: "确认强制解决" })).toBeDisabled();
    fireEvent.change(
      screen.getByPlaceholderText("说明为什么无需等待必需任务完成…"),
      { target: { value: "客户已在线下取消" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "确认强制解决" }));

    expect(mocks.forceResolve.mutate).toHaveBeenCalledWith(
      {
        id: item.id,
        expectedVersion: 2,
        reason: "客户已在线下取消",
        confirm: true,
      },
      expect.any(Object),
    );
  });
});
