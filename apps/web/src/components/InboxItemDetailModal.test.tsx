import {
  act,
  cleanup,
  fireEvent,
  render as renderView,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import type { ReactElement } from "react";
import { ApiError } from "../api/client";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import type { InboxItem } from "../types/models";
import { InboxItemDetailModal } from "./InboxItemDetailModal";

function render(ui: ReactElement) {
  return renderView(ui, { wrapper: MemoryRouter });
}

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

const returnSession = "018f0000-0000-7000-8000-000000000899";
const followupSourceItem: InboxItem = {
  ...baseItem,
  kind: "event",
  sourceEntityType: "client_followup",
  sourceEntityId: "018f0000-0000-7000-8000-000000000809",
  sourceEventKey: "followup:018f0000-0000-7000-8000-000000000809:due:2",
  dueAt: "2026-08-30T10:00:00Z",
  payloadJson: {
    client_followup_id: "018f0000-0000-7000-8000-000000000809",
    client_id: "018f0000-0000-7000-8000-000000000808",
    scheduled_at: "2026-08-30T10:00:00Z",
    timezone: "Asia/Shanghai",
    channel: "phone",
  },
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
  link: {
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
  useLinkInboxItemTask: () => hooks.link,
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
  it("labels an Agent failure and keeps exact source navigation behind the draft and pending gates", () => {
    const runId = "018f0000-0000-7000-8000-000000000809";
    const taskId = "018f0000-0000-7000-8000-000000000808";
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        kind: "event",
        sourceEntityType: "agent_run_failed",
        sourceEntityId: runId,
        sourceEventKey: `agent-run:${runId}:failed`,
        payloadJson: {
          agent_run_id: runId,
          task_id: taskId,
          attempt: 2,
          error_code: "AGENT_MODEL_FAILED",
          failed_at: "2026-09-21T12:00:00.000000000Z",
          automation_rule_id: "00000000-0000-5000-8000-000000000105",
          automation_run_id: baseItem.id,
          source_event_id: returnSession,
        },
      },
      isError: false,
      isPending: false,
    });
    const view = render(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={vi.fn()}
        returnSession={returnSession}
      />,
    );
    expect(screen.getByText(/Agent 执行失败诊断.*仅保存在本机/)).toBeVisible();
    expect(screen.getByRole("link", { name: "查看失败执行" })).toHaveAttribute(
      "href",
      `/tasks/${taskId}?agent_run=${runId}&return_session=${returnSession}`,
    );
    fireEvent.click(screen.getByRole("button", { name: "标记解决" }));
    expect(screen.getByRole("button", { name: "查看失败执行" })).toBeDisabled();
    expect(
      screen.queryByRole("link", { name: "查看失败执行" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    hooks.command.isPending = true;
    view.rerender(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={vi.fn()}
        returnSession={returnSession}
      />,
    );
    expect(screen.getByRole("button", { name: "查看失败执行" })).toBeDisabled();
    expect(hooks.update.mutate).not.toHaveBeenCalled();
    expect(hooks.command.mutate).not.toHaveBeenCalled();
  });
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
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    vi.clearAllMocks();
    hooks.update.isPending = false;
    hooks.command.isPending = false;
    hooks.link.isPending = false;
  });

  it("hands off the exact inbox item only after editing or command drafts are closed", () => {
    const onClose = vi.fn();
    render(<InboxItemDetailModal itemId={baseItem.id} onClose={onClose} />);
    const handoff = screen.getByRole("button", { name: "交给智能体" });
    expect(handoff).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "尚未保存的收件箱草稿" },
    });
    expect(handoff).toBeDisabled();
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(screen.getByRole("button", { name: "标记解决" }));
    expect(handoff).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "收件箱事项",
      route: `/inbox/${baseItem.id}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=inbox_item");
    expect(pending?.prompt).toContain(`id=${baseItem.id}`);
    expect(pending?.prompt).toContain("inbox.*");
    expect(pending?.prompt).toContain("拆分任务不会自动执行 Agent");
    expect(pending?.prompt).toContain("不要直接改 Task 状态");
    expect(onClose).toHaveBeenCalledOnce();
    expect(hooks.update.mutate).not.toHaveBeenCalled();
    expect(hooks.command.mutate).not.toHaveBeenCalled();
  });

  it("preserves the originating conversation when opening the actual followup source", () => {
    const clientId = "018f0000-0000-7000-8000-000000000808";
    const followupId = "018f0000-0000-7000-8000-000000000809";
    const sessionId = "018f0000-0000-7000-8000-000000000899";
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        kind: "event",
        sourceEntityType: "client_followup",
        sourceEntityId: followupId,
        sourceEventKey: `followup:${followupId}:due:2`,
        dueAt: "2026-08-30T10:00:00Z",
        payloadJson: {
          client_followup_id: followupId,
          client_id: clientId,
          scheduled_at: "2026-08-30T10:00:00Z",
          timezone: "Asia/Shanghai",
          channel: "phone",
        },
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });
    render(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={vi.fn()}
        returnSession={sessionId}
      />,
    );
    expect(screen.getByRole("link", { name: "查看客户回访" })).toHaveAttribute(
      "href",
      `/clients/${clientId}?followup=${followupId}&return_session=${sessionId}`,
    );
    expect(hooks.update.mutate).not.toHaveBeenCalled();
    expect(hooks.command.mutate).not.toHaveBeenCalled();
    expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
  });

  it.each(["header", "escape", "backdrop"])(
    "protects an editing draft through the %s close path and return navigation",
    (entry) => {
      hooks.detail.mockReturnValue({ data: followupSourceItem });
      const onClose = vi.fn();
      render(
        <InboxItemDetailModal
          itemId={baseItem.id}
          onClose={onClose}
          returnSession={returnSession}
        />,
      );
      const modal = screen.getByRole("dialog", { name: "收件箱详情" });
      fireEvent.click(screen.getByRole("button", { name: "编辑" }));
      fireEvent.change(screen.getByLabelText("标题"), {
        target: { value: "必须保留的未保存标题" },
      });
      const back = screen.getByRole("button", { name: "返回原对话" });
      expect(back).toBeDisabled();
      fireEvent.click(back);
      expect(screen.queryByRole("link", { name: "查看客户回访" })).toBeNull();
      const requestClose = () => {
        if (entry === "escape") fireEvent.keyDown(document, { key: "Escape" });
        else if (entry === "backdrop")
          fireEvent.click(screen.getByRole("button", { name: "关闭弹窗" }));
        else
          fireEvent.click(within(modal).getByRole("button", { name: "关闭" }));
      };
      requestClose();
      expect(onClose).not.toHaveBeenCalled();
      const confirm = screen.getByRole("dialog", { name: "舍弃未保存更改？" });
      fireEvent.click(
        within(confirm).getByRole("button", { name: "继续编辑" }),
      );
      expect(screen.getByLabelText("标题")).toHaveValue("必须保留的未保存标题");
      expect(
        screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
      ).toBeNull();
      requestClose();
      fireEvent.click(screen.getByRole("button", { name: "舍弃更改并关闭" }));
      expect(onClose).toHaveBeenCalledOnce();
      expect(hooks.update.mutate).not.toHaveBeenCalled();
      expect(hooks.command.mutate).not.toHaveBeenCalled();
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
    },
  );

  it.each([
    ["标记解决", "解决原因", "尚未提交的解决说明"],
    ["忽略", "忽略原因", "尚未提交的忽略说明"],
    ["稍后处理", "稍后至", "2026-09-25T13:30"],
  ])(
    "protects the %s action draft from all local exits",
    (action, field, value) => {
      hooks.detail.mockReturnValue({ data: followupSourceItem });
      const onClose = vi.fn();
      render(
        <InboxItemDetailModal
          itemId={baseItem.id}
          onClose={onClose}
          returnSession={returnSession}
        />,
      );
      fireEvent.click(screen.getByRole("button", { name: action }));
      fireEvent.change(screen.getByLabelText(field), { target: { value } });
      expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
      expect(screen.queryByRole("link", { name: "查看客户回访" })).toBeNull();
      expect(
        screen.getByRole("button", { name: "查看客户回访" }),
      ).toBeDisabled();
      fireEvent.click(screen.getByRole("button", { name: "查看客户回访" }));
      expect(screen.getByLabelText(field)).toHaveValue(value);
      fireEvent.click(screen.getByRole("button", { name: "关闭" }));
      expect(onClose).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "继续编辑" }));
      expect(screen.getByLabelText(field)).toHaveValue(value);
      fireEvent.click(screen.getByRole("button", { name: "取消" }));
      expect(screen.getByRole("link", { name: "返回原对话" })).toHaveAttribute(
        "href",
        "/ai",
      );
      expect(screen.getByRole("link", { name: "查看客户回访" })).toBeVisible();
      fireEvent.click(screen.getByRole("button", { name: "关闭" }));
      expect(onClose).toHaveBeenCalledOnce();
      expect(hooks.command.mutate).not.toHaveBeenCalled();
    },
  );

  it.each(["update", "command", "link"] as const)(
    "blocks source navigation, return and dismissal during pending %s operations",
    (operation) => {
      hooks.detail.mockReturnValue({ data: followupSourceItem });
      const onClose = vi.fn();
      const ui = () => (
        <InboxItemDetailModal
          itemId={baseItem.id}
          onClose={onClose}
          returnSession={returnSession}
        />
      );
      const view = render(ui());
      hooks[operation].isPending = true;
      view.rerender(ui());
      expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
      expect(
        screen.getByRole("button", { name: "查看客户回访" }),
      ).toBeDisabled();
      expect(screen.queryByRole("link", { name: "查看客户回访" })).toBeNull();
      expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
      expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onClose).not.toHaveBeenCalled();
      expect(
        screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
      ).toBeNull();
      hooks[operation].isPending = false;
      view.rerender(ui());
      expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
      expect(screen.getByRole("link", { name: "查看客户回访" })).toBeVisible();
      fireEvent.click(screen.getByRole("button", { name: "关闭" }));
      expect(onClose).toHaveBeenCalledOnce();
      expect(hooks.update.mutate).not.toHaveBeenCalled();
      expect(hooks.command.mutate).not.toHaveBeenCalled();
      expect(hooks.link.mutate).not.toHaveBeenCalled();
    },
  );

  it.each(["update", "command", "link"] as const)(
    "freezes all edit fields and ignores repeated form submits while %s is pending",
    (operation) => {
      const ui = () => (
        <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />
      );
      const view = render(ui());
      fireEvent.click(screen.getByRole("button", { name: "编辑" }));
      fireEvent.change(screen.getByLabelText("标题"), {
        target: { value: "已提交的标题" },
      });
      fireEvent.change(screen.getByLabelText("说明"), {
        target: { value: "已提交的说明" },
      });
      fireEvent.change(screen.getByLabelText("优先级"), {
        target: { value: "P0" },
      });
      fireEvent.change(screen.getByLabelText("截止时间"), {
        target: { value: "2026-09-25T12:00" },
      });
      const form = screen.getByLabelText("标题").closest("form")!;
      fireEvent.submit(form);
      expect(hooks.update.mutate).toHaveBeenCalledOnce();
      hooks[operation].isPending = true;
      view.rerender(ui());
      for (const label of ["标题", "说明", "优先级", "截止时间"]) {
        expect(screen.getByLabelText(label)).toBeDisabled();
      }
      fireEvent.submit(form);
      expect(hooks.update.mutate).toHaveBeenCalledOnce();
      expect(hooks.command.mutate).not.toHaveBeenCalled();
      expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
      expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
      hooks[operation].isPending = false;
      view.rerender(ui());
      for (const label of ["标题", "说明", "优先级", "截止时间"]) {
        expect(screen.getByLabelText(label)).toBeEnabled();
      }
      expect(screen.getByLabelText("标题")).toHaveValue("已提交的标题");
      expect(screen.getByLabelText("说明")).toHaveValue("已提交的说明");
      expect(screen.getByLabelText("优先级")).toHaveValue("P0");
      expect(screen.getByLabelText("截止时间")).toHaveValue("2026-09-25T12:00");
    },
  );

  it.each([
    ["标记解决", "解决原因", "确认前的解决原因"],
    ["忽略", "忽略原因", "确认前的忽略原因"],
    ["稍后处理", "稍后至", "2026-09-25T12:00"],
  ])(
    "freezes the %s input until its command completes",
    (action, field, value) => {
      const ui = () => (
        <InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />
      );
      const view = render(ui());
      fireEvent.click(screen.getByRole("button", { name: action }));
      fireEvent.change(screen.getByLabelText(field), { target: { value } });
      const form = screen.getByLabelText(field).closest("form")!;
      fireEvent.submit(form);
      expect(hooks.command.mutate).toHaveBeenCalledOnce();
      hooks.command.isPending = true;
      view.rerender(ui());
      expect(screen.getByLabelText(field)).toBeDisabled();
      expect(screen.getByLabelText(field)).toHaveValue(value);
      fireEvent.submit(form);
      expect(hooks.command.mutate).toHaveBeenCalledOnce();
      expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
      expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
      hooks.command.isPending = false;
      view.rerender(ui());
      expect(screen.getByLabelText(field)).toBeEnabled();
      expect(screen.getByLabelText(field)).toHaveValue(value);
    },
  );

  it("restores navigation after a successful save without asking to discard the saved draft", () => {
    hooks.detail.mockReturnValue({ data: followupSourceItem });
    const onClose = vi.fn();
    render(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={onClose}
        returnSession={returnSession}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "已经保存的标题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    expect(hooks.update.mutate).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    act(() =>
      hooks.update.mutate.mock.lastCall![1].onSuccess({
        ...followupSourceItem,
        title: "已经保存的标题",
        version: 3,
      }),
    );
    expect(screen.getByRole("link", { name: "返回原对话" })).toBeVisible();
    expect(screen.getByRole("link", { name: "查看客户回访" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(onClose).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole("dialog", { name: "舍弃未保存更改？" }),
    ).toBeNull();
  });

  it("keeps a relation request that was already pending on mount protected", () => {
    hooks.detail.mockReturnValue({ data: followupSourceItem });
    hooks.link.isPending = true;
    const onClose = vi.fn();
    render(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={onClose}
        returnSession={returnSession}
      />,
    );
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "查看客户回访" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("preserves a conflicted draft and its leave protection after refreshing the item", async () => {
    const refetch = vi.fn().mockResolvedValue({
      data: { ...followupSourceItem, version: 3 },
    });
    hooks.detail.mockReturnValue({ data: followupSourceItem, refetch });
    const onClose = vi.fn();
    render(
      <InboxItemDetailModal
        itemId={baseItem.id}
        onClose={onClose}
        returnSession={returnSession}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "冲突后仍需保留的本地标题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    await act(async () => {
      hooks.update.mutate.mock.lastCall![1].onError(
        new ApiError("版本冲突", { code: "VERSION_CONFLICT", status: 409 }),
      );
    });
    expect(refetch).toHaveBeenCalledOnce();
    expect(screen.getByLabelText("标题")).toHaveValue(
      "冲突后仍需保留的本地标题",
    );
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(screen.getByLabelText("标题")).toHaveValue(
      "冲突后仍需保留的本地标题",
    );
    expect(hooks.update.mutate).toHaveBeenCalledOnce();
    expect(hooks.command.mutate).not.toHaveBeenCalled();
  });

  it("cannot confirm discard if an operation starts while the confirmation is open", () => {
    hooks.detail.mockReturnValue({ data: followupSourceItem });
    const onClose = vi.fn();
    const ui = () => (
      <InboxItemDetailModal itemId={baseItem.id} onClose={onClose} />
    );
    const view = render(ui());
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    fireEvent.change(screen.getByLabelText("说明"), {
      target: { value: "等待操作结束后再决定是否舍弃" },
    });
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    hooks.command.isPending = true;
    view.rerender(ui());
    const confirm = screen.getByRole("dialog", { name: "舍弃未保存更改？" });
    expect(
      within(confirm).getByRole("button", { name: "舍弃更改并关闭" }),
    ).toBeDisabled();
    fireEvent.click(
      within(confirm).getByRole("button", { name: "舍弃更改并关闭" }),
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(confirm).toBeVisible();
    expect(onClose).not.toHaveBeenCalled();
    hooks.command.isPending = false;
    view.rerender(ui());
    fireEvent.click(screen.getByRole("button", { name: "继续编辑" }));
    expect(screen.getByLabelText("说明")).toHaveValue(
      "等待操作结束后再决定是否舍弃",
    );
  });

  it("labels an invoice due source as a local invoice reminder", () => {
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        kind: "event",
        sourceEntityType: "invoice_due",
        sourceEntityId: "invoice-1",
        sourceEventKey: "invoice:invoice-1:due:2026-08-29",
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getByText("发票到期提醒 · 仅保存在本机")).toBeTruthy();
    expect(screen.queryByText("任务产出跟进 · 仅保存在本机")).toBeNull();
  });

  it("labels an Automation source and shows its immutable Project snapshot", () => {
    const projectId = "018f0000-0000-7000-8000-000000000201";
    hooks.detail.mockReturnValue({
      data: {
        ...baseItem,
        kind: "event",
        title: "核对并准备发票：官网升级",
        summary:
          "项目已完成。请人工核对是否需要开票，并准备后续资料；自动化不会生成或发送发票。",
        sourceEntityType: "automation",
        sourceEntityId: "018f0000-0000-7000-8000-000000000826",
        sourceEventKey:
          "automation:event:00000000-0000-5000-8000-000000000101:018f0000-0000-7000-8000-000000000825",
        dueAt: null,
        payloadJson: {
          automation_rule_id: "00000000-0000-5000-8000-000000000101",
          automation_run_id: "018f0000-0000-7000-8000-000000000826",
          preset_key: "project-completed-inbox",
          project_id: projectId,
          project_name: "官网升级",
        },
      },
      isError: false,
      isPending: false,
      refetch: vi.fn(),
    });

    render(<InboxItemDetailModal itemId={baseItem.id} onClose={vi.fn()} />);

    expect(screen.getByText("本地自动化事项 · 仅保存在本机")).toBeTruthy();
    expect(screen.getByText("项目完成自动化")).toBeTruthy();
    expect(screen.getByText("官网升级")).toBeTruthy();
    expect(
      screen.getByText("仅创建本地核对事项，不会生成或发送发票"),
    ).toBeTruthy();
    expect(screen.getByRole("link", { name: /查看来源项目/ })).toHaveAttribute(
      "href",
      `/projects/${projectId}`,
    );
    expect(screen.queryByText("任务产出跟进 · 仅保存在本机")).toBeNull();
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
    expect(screen.queryByRole("button", { name: "关闭" })).toBeNull();
    expect(screen.queryByRole("button", { name: "关闭弹窗" })).toBeNull();
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
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    expect(screen.getByLabelText("截止时间")).toBeDisabled();
    expect(screen.getByLabelText("优先级")).not.toBeDisabled();
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
