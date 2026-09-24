import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import type { Reminder } from "../types/models";
import { ReminderManagerModal } from "./ReminderManagerModal";
import { MemoryRouter } from "react-router-dom";
import { useAiChatStore } from "../store/aiChat";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  cancel: vi.fn(),
  createReset: vi.fn(),
  updateReset: vi.fn(),
  cancelReset: vi.fn(),
  listRefetch: vi.fn(),
  detailRefetch: vi.fn(),
  listHook: vi.fn(),
  detailHook: vi.fn(),
  listItems: [] as Reminder[],
  listMeta: {
    page: 1,
    pageSize: 20,
    total: 0,
    serverNow: "2026-08-28T10:00:00Z",
  },
  listError: null as unknown,
  listPending: false,
  listFetching: false,
  listPlaceholder: false,
  details: {} as Record<string, Reminder | undefined>,
  detailError: null as unknown,
  detailPending: false,
  detailFetching: false,
  createPending: false,
  updatePending: false,
  cancelPending: false,
  createError: null as unknown,
  updateError: null as unknown,
  cancelError: null as unknown,
}));

const scheduled: Reminder = {
  id: "018f0000-0000-7000-8000-000000001501",
  sourceEntityType: "manual",
  sourceEntityId: null,
  title: "复查本地备份",
  summary: "确认恢复点可用",
  priority: "P1",
  triggerAt: "2099-08-30T01:00:00Z",
  status: "scheduled",
  sourceEventKey: "reminder:018f0000-0000-7000-8000-000000001501:due",
  createdByActorId: "00000000-0000-5000-8000-000000000001",
  seriesId: "018f0000-0000-7000-8000-000000001501",
  recurrenceType: "none",
  recurrenceInterval: 1,
  recurrenceTimezone: "UTC",
  occurrenceNumber: 1,
  recurrenceAnchorDay: 1,
  firedAt: null,
  inboxItemId: null,
  cancelledByActorId: null,
  cancelledAt: null,
  cancelReason: null,
  version: 3,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-28T10:00:00Z",
  availableActions: ["edit", "cancel"],
};

function reminder(overrides: Partial<Reminder>): Reminder {
  return { ...scheduled, ...overrides };
}

function mutation(
  mutate: ReturnType<typeof vi.fn>,
  reset: ReturnType<typeof vi.fn>,
  isPending: boolean,
  error: unknown,
) {
  return { mutate, reset, isPending, error };
}

vi.mock("../api/hooks", () => ({
  useRemindersQuery: (input: { page?: number }, enabled: boolean) => {
    mocks.listHook(input, enabled);
    return {
      data: {
        items: mocks.listItems,
        meta: { ...mocks.listMeta, page: input.page ?? 1 },
      },
      isPending: mocks.listPending,
      isFetching: mocks.listFetching,
      isPlaceholderData: mocks.listPlaceholder,
      isSuccess: !mocks.listPending && !mocks.listError,
      isError: Boolean(mocks.listError),
      error: mocks.listError,
      refetch: mocks.listRefetch,
    };
  },
  useReminderQuery: (id: string | null) => {
    mocks.detailHook(id);
    return {
      data: id ? mocks.details[id] : undefined,
      isPending: Boolean(id) && mocks.detailPending,
      isFetching: Boolean(id) && mocks.detailFetching,
      isSuccess:
        Boolean(id) && !mocks.detailPending && !Boolean(mocks.detailError),
      isError: Boolean(mocks.detailError),
      error: id ? mocks.detailError : null,
      refetch: mocks.detailRefetch,
    };
  },
  useCreateReminder: () =>
    mutation(
      mocks.create,
      mocks.createReset,
      mocks.createPending,
      mocks.createError,
    ),
  useUpdateReminder: () =>
    mutation(
      mocks.update,
      mocks.updateReset,
      mocks.updatePending,
      mocks.updateError,
    ),
  useCancelReminder: () =>
    mutation(
      mocks.cancel,
      mocks.cancelReset,
      mocks.cancelPending,
      mocks.cancelError,
    ),
}));

const baseProps: ComponentProps<typeof ReminderManagerModal> = {
  open: true,
  status: "scheduled",
  reminderId: null,
  onStateChange: vi.fn(),
  onClose: vi.fn(),
};

function renderManager(
  overrides: Partial<ComponentProps<typeof ReminderManagerModal>> = {},
) {
  return render(<ReminderManagerModal {...baseProps} {...overrides} />, {
    wrapper: MemoryRouter,
  });
}

function mutationSuccess(mock: ReturnType<typeof vi.fn>, value: Reminder) {
  const options = mock.mock.calls.at(-1)?.[1] as
    { onSuccess?: (reminder: Reminder) => void } | undefined;
  options?.onSuccess?.(value);
}

describe("ReminderManagerModal", () => {
  it("does not leave unsaved reminder edits through the agent handoff", () => {
    mocks.details[scheduled.id] = scheduled;
    renderManager({ reminderId: scheduled.id });
    const handoff = screen.getByRole("button", { name: "交给智能体" });
    expect(handoff).toBeEnabled();
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "未保存标题" },
    });
    expect(handoff).toBeDisabled();
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: scheduled.title },
    });
    fireEvent.click(handoff);
    expect(useAiWorkbenchHandoff.getState().pending).toBeNull();
    const pending = useAiWorkbenchHandoff.getState().pendingIssue;
    expect(pending).toMatchObject({
      label: "本地提醒",
      route: `/inbox?reminder=${scheduled.id}`,
      scopes: ["work", "actions"],
    });
    expect(pending?.prompt).toContain("workspace_get");
    expect(pending?.prompt).toContain("type=reminder");
    expect(pending?.prompt).toContain(`id=${scheduled.id}`);
    expect(pending?.prompt).toContain("reminder.update");
    expect(pending?.prompt).toContain("不要创建系统通知");
    expect(mocks.update).not.toHaveBeenCalled();
  });

  beforeEach(() => {
    mocks.listItems = [scheduled];
    mocks.listMeta = {
      page: 1,
      pageSize: 20,
      total: 1,
      serverNow: "2026-08-28T10:00:00Z",
    };
    mocks.listError = null;
    mocks.listPending = false;
    mocks.listFetching = false;
    mocks.listPlaceholder = false;
    mocks.details = { [scheduled.id]: scheduled };
    mocks.detailError = null;
    mocks.detailPending = false;
    mocks.detailFetching = false;
    mocks.createPending = false;
    mocks.updatePending = false;
    mocks.cancelPending = false;
    mocks.createError = null;
    mocks.updateError = null;
    mocks.cancelError = null;
    mocks.detailRefetch.mockResolvedValue({ data: scheduled });
  });

  afterEach(() => {
    cleanup();
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    useAiChatStore.setState({ activeSessionId: "" });
    vi.clearAllMocks();
  });

  it("opens a controlled deep link even when the active list omits it", async () => {
    mocks.listItems = [];
    mocks.listMeta.total = 0;
    const onStateChange = vi.fn();

    renderManager({ reminderId: scheduled.id, onStateChange });

    expect(await screen.findByLabelText("标题")).toHaveValue("复查本地备份");
    expect(onStateChange).not.toHaveBeenCalled();
  });

  it.each([false, true])(
    "returns to the source chat from a reminder without issuing a command (read failure: %s)",
    async (missing) => {
      const sessionId = "018f0000-0000-7000-8000-000000000022";
      mocks.listItems = [];
      if (missing) {
        mocks.details = {};
        mocks.detailError = new Error("unavailable");
      }
      render(
        <MemoryRouter>
          <ReminderManagerModal
            {...baseProps}
            reminderId={scheduled.id}
            returnSession={sessionId}
          />
        </MemoryRouter>,
      );
      expect(mocks.detailHook).toHaveBeenCalledWith(scheduled.id);
      fireEvent.click(await screen.findByRole("link", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
      for (const mutation of [mocks.create, mocks.update, mocks.cancel])
        expect(mutation).not.toHaveBeenCalled();
    },
  );

  it("waits for an in-flight reminder save before allowing return navigation", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000022";
    mocks.updatePending = true;
    const view = render(
      <MemoryRouter>
        <ReminderManagerModal
          {...baseProps}
          reminderId={scheduled.id}
          returnSession={sessionId}
        />
      </MemoryRouter>,
    );
    const pendingReturn = screen.getByRole("button", { name: "返回原对话" });
    expect(pendingReturn).toBeDisabled();
    expect(screen.queryByRole("link", { name: "返回原对话" })).toBeNull();
    fireEvent.click(pendingReturn);
    expect(useAiChatStore.getState().activeSessionId).not.toBe(sessionId);
    mocks.updatePending = false;
    view.rerender(
      <MemoryRouter>
        <ReminderManagerModal
          {...baseProps}
          reminderId={scheduled.id}
          returnSession={sessionId}
        />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole("link", { name: "返回原对话" }));
    expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
  });

  it.each(["edit", "create", "cancel"] as const)(
    "keeps the source chat and reminder draft while return is blocked (%s)",
    (kind) => {
      const sessionId = "018f0000-0000-7000-8000-000000000022";
      renderManager({ reminderId: scheduled.id, returnSession: sessionId });
      if (kind === "create") {
        fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
      } else if (kind === "cancel") {
        fireEvent.click(screen.getByRole("button", { name: "取消提醒" }));
      } else {
        fireEvent.change(screen.getByLabelText("标题"), {
          target: { value: "尚未保存的标题" },
        });
      }
      expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
      expect(
        screen.getByText(/请先保存或放弃当前提醒草稿，或退出取消确认/),
      ).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "返回原对话" }));
      expect(useAiChatStore.getState().activeSessionId).not.toBe(sessionId);
      expect(mocks.create).not.toHaveBeenCalled();
      expect(mocks.update).not.toHaveBeenCalled();
      expect(mocks.cancel).not.toHaveBeenCalled();
      if (kind === "edit") {
        expect(screen.getByLabelText("标题")).toHaveValue("尚未保存的标题");
        fireEvent.change(screen.getByLabelText("标题"), {
          target: { value: scheduled.title },
        });
        expect(
          screen.getByRole("link", { name: "返回原对话" }),
        ).toBeInTheDocument();
      } else if (kind === "cancel") {
        fireEvent.click(screen.getByRole("button", { name: "返回" }));
        expect(
          screen.getByRole("link", { name: "返回原对话" }),
        ).toBeInTheDocument();
      }
    },
  );

  it.each(["footer", "header", "backdrop", "escape"] as const)(
    "requires explicit discard before closing a reminder draft (%s)",
    (target) => {
      const onClose = vi.fn();
      renderManager({ reminderId: scheduled.id, onClose });
      fireEvent.change(screen.getByLabelText("标题"), {
        target: { value: "关闭前保留的标题" },
      });
      const close = () => {
        if (target === "escape") fireEvent.keyDown(document, { key: "Escape" });
        else if (target === "backdrop")
          fireEvent.click(screen.getByRole("button", { name: "关闭弹窗" }));
        else
          fireEvent.click(
            screen.getAllByRole("button", { name: "关闭" })[
              target === "header" ? 0 : 1
            ]!,
          );
      };
      close();
      expect(onClose).not.toHaveBeenCalled();
      expect(
        screen.getByRole("dialog", { name: "放弃提醒草稿？" }),
      ).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
      expect(screen.getByLabelText("标题")).toHaveValue("关闭前保留的标题");
      close();
      fireEvent.click(screen.getByRole("button", { name: "放弃草稿并关闭" }));
      expect(onClose).toHaveBeenCalledTimes(1);
      expect(mocks.update).not.toHaveBeenCalled();
    },
  );

  it.each(["status", "record", "create"] as const)(
    "protects a cancellation draft before an explicit manager transition (%s)",
    (target) => {
      const other = reminder({
        id: "018f0000-0000-7000-8000-000000001502",
        title: "另一条提醒",
      });
      mocks.listItems = [scheduled, other];
      const onStateChange = vi.fn();
      renderManager({ reminderId: scheduled.id, onStateChange });
      fireEvent.click(screen.getByRole("button", { name: "取消提醒" }));
      fireEvent.change(screen.getByLabelText("取消原因"), {
        target: { value: "尚未提交的取消原因" },
      });
      const transition = () =>
        fireEvent.click(
          screen.getByRole("button", {
            name:
              target === "status"
                ? "已取消"
                : target === "record"
                  ? /另一条提醒/
                  : "新建提醒",
          }),
        );
      transition();
      expect(onStateChange).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
      expect(screen.getByLabelText("取消原因")).toHaveValue(
        "尚未提交的取消原因",
      );
      transition();
      fireEvent.click(screen.getByRole("button", { name: "放弃草稿并继续" }));
      expect(onStateChange).toHaveBeenCalledExactlyOnceWith({
        status: target === "status" ? "cancelled" : "scheduled",
        reminderId: target === "record" ? other.id : null,
      });
      expect(mocks.cancel).not.toHaveBeenCalled();
    },
  );

  it("keeps an initialized edit protected after its latest detail becomes unavailable", () => {
    const props = {
      ...baseProps,
      reminderId: scheduled.id,
      returnSession: "018f0000-0000-7000-8000-000000000022",
    };
    const view = renderManager(props);
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "读取失败前的草稿" },
    });
    mocks.details = {};
    mocks.detailError = new Error("unavailable");
    view.rerender(<ReminderManagerModal {...props} />);
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    fireEvent.click(screen.getAllByRole("button", { name: "关闭" })[1]!);
    expect(props.onClose).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
    mocks.details = { [scheduled.id]: scheduled };
    mocks.detailError = null;
    view.rerender(<ReminderManagerModal {...props} />);
    expect(screen.getByLabelText("标题")).toHaveValue("读取失败前的草稿");
  });

  it.each(["create", "update", "cancel"] as const)(
    "does not allow a pending mutation to be bypassed through a discard dialog (%s)",
    (kind) => {
      const onClose = vi.fn();
      const props = { ...baseProps, reminderId: scheduled.id, onClose };
      const view = renderManager(props);
      fireEvent.change(screen.getByLabelText("标题"), {
        target: { value: "在途草稿" },
      });
      fireEvent.click(screen.getAllByRole("button", { name: "关闭" })[1]!);
      mocks[`${kind}Pending`] = true;
      view.rerender(<ReminderManagerModal {...props} />);
      expect(
        screen.getByRole("button", { name: "放弃草稿并关闭" }),
      ).toBeDisabled();
      fireEvent.click(screen.getByRole("button", { name: "放弃草稿并关闭" }));
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onClose).not.toHaveBeenCalled();
      mocks[`${kind}Pending`] = false;
      view.rerender(<ReminderManagerModal {...props} />);
      fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
      expect(screen.getByLabelText("标题")).toHaveValue("在途草稿");
    },
  );

  it("accepts a successful save as the new draft baseline before cache refresh", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000022";
    renderManager({ reminderId: scheduled.id, returnSession: sessionId });
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "已保存标题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));
    act(() =>
      mutationSuccess(
        mocks.update,
        reminder({ title: "已保存标题", version: 4 }),
      ),
    );
    expect(
      screen.getByRole("link", { name: "返回原对话" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "交给智能体" })).toBeEnabled();
    fireEvent.click(screen.getAllByRole("button", { name: "关闭" })[1]!);
    expect(baseProps.onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("dialog", { name: "放弃提醒草稿？" })).toBeNull();
  });

  it("does not block successful cancellation with a navigation discard guard", () => {
    const sessionId = "018f0000-0000-7000-8000-000000000022";
    const onStateChange = vi.fn();
    renderManager({
      reminderId: scheduled.id,
      returnSession: sessionId,
      onStateChange,
    });
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "取消前的本地标题" },
    });
    fireEvent.click(screen.getByRole("button", { name: "取消提醒" }));
    fireEvent.change(screen.getByLabelText("取消原因"), {
      target: { value: "无需提醒" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认取消" }));
    act(() =>
      mutationSuccess(
        mocks.cancel,
        reminder({ status: "cancelled", version: 4, cancelReason: "无需提醒" }),
      ),
    );
    expect(onStateChange).toHaveBeenCalledExactlyOnceWith({
      status: "cancelled",
      reminderId: scheduled.id,
    });
    expect(
      screen.getByRole("link", { name: "返回原对话" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "放弃提醒草稿？" })).toBeNull();
  });

  it("submits edits for the controlled reminder and its authoritative version", async () => {
    renderManager({ reminderId: scheduled.id });

    const title = await screen.findByLabelText("标题");
    fireEvent.change(title, { target: { value: "复查本地恢复点" } });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update.mock.calls[0][0]).toMatchObject({
      id: scheduled.id,
      input: {
        title: "复查本地恢复点",
        summary: "确认恢复点可用",
        priority: "P1",
        recurrenceType: "none",
        recurrenceInterval: 1,
        recurrenceTimezone: "UTC",
        expectedVersion: 3,
      },
    });
    expect(mocks.update.mock.calls[0][0].input.triggerAt).toBe(
      new Date("2099-08-30T01:00:00Z").toISOString(),
    );
  });

  it("protects a local edit before opening the inbox item of a newly fired reminder", () => {
    const onOpenInboxItem = vi.fn();
    const props = { ...baseProps, reminderId: scheduled.id, onOpenInboxItem };
    const view = renderManager(props);
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "触发前的草稿" },
    });
    const fired = reminder({
      status: "fired",
      inboxItemId: "018f0000-0000-7000-8000-000000001599",
      firedAt: "2099-08-30T01:00:05Z",
    });
    mocks.details[scheduled.id] = fired;
    view.rerender(<ReminderManagerModal {...props} status="fired" />);
    fireEvent.click(screen.getByRole("button", { name: "打开收件箱条目" }));
    expect(onOpenInboxItem).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
    fireEvent.click(screen.getByRole("button", { name: "打开收件箱条目" }));
    fireEvent.click(screen.getByRole("button", { name: "放弃草稿并继续" }));
    expect(onOpenInboxItem).toHaveBeenCalledExactlyOnceWith(fired.inboxItemId);
    expect(mocks.update).not.toHaveBeenCalled();
  });

  it.each(["create", "cancel"] as const)(
    "does not replace a newer selected draft with an old mutation callback (%s)",
    (kind) => {
      const props = {
        ...baseProps,
        reminderId: kind === "create" ? null : scheduled.id,
        returnSession: "018f0000-0000-7000-8000-000000000022",
      };
      const view = renderManager(props);
      if (kind === "create") {
        fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
        fireEvent.change(screen.getByLabelText("标题"), {
          target: { value: "旧创建请求" },
        });
        fireEvent.change(screen.getByLabelText("提醒时间"), {
          target: { value: "2099-09-01T10:30" },
        });
        fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));
      } else {
        fireEvent.click(screen.getByRole("button", { name: "取消提醒" }));
        fireEvent.change(screen.getByLabelText("取消原因"), {
          target: { value: "旧取消请求" },
        });
        fireEvent.click(screen.getByRole("button", { name: "确认取消" }));
      }
      const other = reminder({
        id: "018f0000-0000-7000-8000-000000001502",
        title: "新选择的提醒",
      });
      mocks.details[other.id] = other;
      view.rerender(<ReminderManagerModal {...props} reminderId={other.id} />);
      fireEvent.change(screen.getByLabelText("标题"), {
        target: { value: "新选择记录的未保存草稿" },
      });
      act(() => mutationSuccess(mocks[kind], scheduled));
      expect(screen.getByLabelText("标题")).toHaveValue(
        "新选择记录的未保存草稿",
      );
      expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    },
  );

  it("switches status and clears the selected reminder in one controlled call", () => {
    const onStateChange = vi.fn();
    renderManager({ reminderId: scheduled.id, onStateChange });

    fireEvent.click(screen.getByRole("button", { name: "已取消" }));

    expect(onStateChange).toHaveBeenCalledWith({
      status: "cancelled",
      reminderId: null,
    });
    expect(onStateChange).toHaveBeenCalledTimes(1);
  });

  it("selects a list row without changing the controlled status", () => {
    const cancelled = reminder({
      id: "018f0000-0000-7000-8000-000000001502",
      title: "已取消的提醒",
      status: "cancelled",
      cancelledAt: "2026-08-29T10:00:00Z",
      cancelReason: "不再需要",
      availableActions: [],
    });
    mocks.listItems = [cancelled];
    const onStateChange = vi.fn();
    renderManager({ status: "cancelled", onStateChange });

    fireEvent.click(screen.getByRole("button", { name: /已取消的提醒/ }));

    expect(onStateChange).toHaveBeenCalledWith({
      status: "cancelled",
      reminderId: cancelled.id,
    });
  });

  it("follows reminderId prop changes from browser navigation", async () => {
    const second = reminder({
      id: "018f0000-0000-7000-8000-000000001503",
      title: "检查第二个恢复点",
      version: 1,
    });
    mocks.details[second.id] = second;
    const onStateChange = vi.fn();
    const view = renderManager({
      reminderId: scheduled.id,
      onStateChange,
    });
    expect(await screen.findByLabelText("标题")).toHaveValue("复查本地备份");

    view.rerender(
      <ReminderManagerModal
        {...baseProps}
        onStateChange={onStateChange}
        reminderId={second.id}
      />,
    );

    await waitFor(() =>
      expect(screen.getByLabelText("标题")).toHaveValue("检查第二个恢复点"),
    );
    expect(mocks.detailHook).toHaveBeenLastCalledWith(second.id);
  });

  it("reconciles a polled scheduled reminder to fired without losing its id", async () => {
    const fired = reminder({
      status: "fired",
      firedAt: "2026-08-30T01:00:05Z",
      inboxItemId: "018f0000-0000-7000-8000-000000001599",
      version: 4,
      availableActions: [],
    });
    const onStateChange = vi.fn();
    const view = renderManager({
      reminderId: scheduled.id,
      onStateChange,
    });
    await screen.findByLabelText("标题");

    mocks.details[scheduled.id] = fired;
    view.rerender(
      <ReminderManagerModal
        {...baseProps}
        onStateChange={onStateChange}
        reminderId={scheduled.id}
      />,
    );

    await waitFor(() =>
      expect(onStateChange).toHaveBeenCalledWith(
        { status: "fired", reminderId: scheduled.id },
        { replace: true },
      ),
    );
    expect(onStateChange).toHaveBeenCalledTimes(1);
    expect(screen.getAllByText("复查本地备份")).toHaveLength(2);
  });

  it("waits for a current read before reconciling a cached fired reminder from the scheduled fallback", async () => {
    const fired = reminder({
      status: "fired",
      firedAt: "2026-08-30T01:00:05Z",
      inboxItemId: "018f0000-0000-7000-8000-000000001599",
      version: 4,
      availableActions: [],
    });
    mocks.details[scheduled.id] = fired;
    mocks.detailFetching = true;
    const onStateChange = vi.fn();
    const view = renderManager({
      reminderId: scheduled.id,
      onStateChange,
    });

    expect(await screen.findAllByText("复查本地备份")).not.toHaveLength(0);
    expect(onStateChange).not.toHaveBeenCalled();

    mocks.detailFetching = false;
    view.rerender(
      <ReminderManagerModal
        {...baseProps}
        onStateChange={onStateChange}
        reminderId={scheduled.id}
      />,
    );

    await waitFor(() =>
      expect(onStateChange).toHaveBeenCalledWith(
        { status: "fired", reminderId: scheduled.id },
        { replace: true },
      ),
    );
  });

  it("does not reconcile the URL from stale cached detail after refresh failure", async () => {
    mocks.detailError = new ApiError("本地连接已中断", {
      code: "NETWORK_ERROR",
    });
    const onStateChange = vi.fn();

    renderManager({
      status: "fired",
      reminderId: scheduled.id,
      onStateChange,
    });

    expect(await screen.findByText("提醒详情刷新失败")).toBeInTheDocument();
    expect(screen.getByLabelText("标题")).toHaveValue("复查本地备份");
    expect(onStateChange).not.toHaveBeenCalled();
  });

  it.each([
    ["参数无效", new ApiError("提醒 ID 无效", { status: 400 })],
    ["记录不存在", new ApiError("提醒不存在", { status: 404 })],
    ["网络失败", new Error("offline")],
  ])("renders a retryable detail error for %s", async (_label, error) => {
    mocks.details = {};
    mocks.detailError = error;
    renderManager({ reminderId: scheduled.id });

    expect(await screen.findByText("无法读取提醒详情")).toBeInTheDocument();
    expect(screen.queryByText("选择一条提醒")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(mocks.detailRefetch).toHaveBeenCalledTimes(1);
  });

  it("keeps cached facts visible when a detail refresh fails", async () => {
    mocks.detailError = new ApiError("本地连接已中断", {
      code: "NETWORK_ERROR",
    });
    renderManager({ reminderId: scheduled.id });

    expect(await screen.findByText("提醒详情刷新失败")).toBeInTheDocument();
    expect(screen.getByLabelText("标题")).toHaveValue("复查本地备份");
    expect(
      screen.getByText(/当前仍显示上次成功读取的记录/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    expect(mocks.detailRefetch).toHaveBeenCalledTimes(1);
  });

  it("creates a future reminder and selects its controlled scheduled detail", async () => {
    const onStateChange = vi.fn();
    renderManager({
      onStateChange,
      returnSession: "018f0000-0000-7000-8000-000000000022",
    });
    fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "检查项目交付" },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2099-09-01T10:30" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(mocks.create.mock.calls[0][0]).toEqual({
      title: "检查项目交付",
      summary: "",
      priority: "P2",
      triggerAt: new Date("2099-09-01T10:30").toISOString(),
      recurrenceType: "none",
      recurrenceInterval: 1,
      recurrenceTimezone: "UTC",
    });
    const created = reminder({
      id: "018f0000-0000-7000-8000-000000001504",
      title: "检查项目交付",
      version: 1,
    });
    act(() => mutationSuccess(mocks.create, created));

    expect(onStateChange).toHaveBeenCalledWith({
      status: "scheduled",
      reminderId: created.id,
    });
    expect(
      screen.getByRole("link", { name: "返回原对话" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "放弃提醒草稿？" })).toBeNull();
  });

  it("creates recurrence rules without losing timezone semantics", async () => {
    renderManager();
    fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "每月月底核账" },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2099-01-31T10:30" },
    });
    fireEvent.change(screen.getByLabelText("重复规则"), {
      target: { value: "monthly" },
    });

    expect(screen.getByText(/短月自动落在月末/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(mocks.create.mock.calls[0][0]).toMatchObject({
      title: "每月月底核账",
      recurrenceType: "monthly",
      recurrenceInterval: 1,
      recurrenceTimezone: expect.any(String),
    });
    expect(mocks.create.mock.calls[0][0].recurrenceTimezone).not.toBe("Local");
  });

  it("creates a recurring reminder in the current IANA timezone", async () => {
    renderManager();
    fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "隔天检查交付" },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2099-09-01T10:30" },
    });
    fireEvent.change(screen.getByLabelText("重复规则"), {
      target: { value: "daily" },
    });
    fireEvent.change(screen.getByLabelText("重复间隔"), {
      target: { value: "2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(mocks.create.mock.calls[0][0]).toMatchObject({
      title: "隔天检查交付",
      recurrenceType: "daily",
      recurrenceInterval: 2,
      recurrenceTimezone: expect.any(String),
    });
    expect(mocks.create.mock.calls[0][0].recurrenceTimezone).not.toBe("Local");
  });

  it("creates a weekday reminder and explains the Monday-to-Friday rule", async () => {
    renderManager();
    fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "工作日晨会" },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2099-01-05T09:00" },
    });
    fireEvent.change(screen.getByLabelText("重复规则"), {
      target: { value: "weekdays" },
    });

    expect(screen.getByText(/周一至周五/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));
    expect(mocks.create.mock.calls[0][0]).toMatchObject({
      title: "工作日晨会",
      recurrenceType: "weekdays",
      recurrenceInterval: 1,
      recurrenceTimezone: expect.any(String),
    });
  });

  it("moves a successfully cancelled reminder to cancelled with the same id", async () => {
    const onStateChange = vi.fn();
    renderManager({ reminderId: scheduled.id, onStateChange });
    await screen.findByLabelText("标题");
    fireEvent.click(screen.getByRole("button", { name: "取消提醒" }));
    fireEvent.change(screen.getByLabelText("取消原因"), {
      target: { value: "计划已调整" },
    });
    fireEvent.click(screen.getByRole("button", { name: "确认取消" }));

    await waitFor(() => expect(mocks.cancel).toHaveBeenCalledTimes(1));
    expect(mocks.cancel.mock.calls[0][0]).toEqual({
      id: scheduled.id,
      reason: "计划已调整",
      expectedVersion: 3,
    });
    const cancelled = reminder({
      status: "cancelled",
      cancelledAt: "2026-08-30T00:30:00Z",
      cancelReason: "计划已调整",
      version: 4,
      availableActions: [],
    });
    mutationSuccess(mocks.cancel, cancelled);

    expect(onStateChange).toHaveBeenCalledWith({
      status: "cancelled",
      reminderId: scheduled.id,
    });
  });

  it("does not let a late create response overwrite newer controlled navigation", async () => {
    const onStateChange = vi.fn();
    const view = renderManager({ onStateChange });
    fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "稍后完成" },
    });
    fireEvent.change(screen.getByLabelText("提醒时间"), {
      target: { value: "2099-09-01T10:30" },
    });
    fireEvent.click(screen.getByRole("button", { name: "创建提醒" }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1));

    const otherId = "018f0000-0000-7000-8000-000000001505";
    view.rerender(
      <ReminderManagerModal
        {...baseProps}
        onStateChange={onStateChange}
        reminderId={otherId}
      />,
    );
    mutationSuccess(
      mocks.create,
      reminder({ id: "018f0000-0000-7000-8000-000000001506" }),
    );

    expect(onStateChange).not.toHaveBeenCalled();
  });

  it("retains conflict recovery and reloads the latest selected fact", async () => {
    const view = renderManager({ reminderId: scheduled.id });
    await screen.findByLabelText("标题");
    mocks.updateError = new ApiError("版本冲突", {
      code: "VERSION_CONFLICT",
      status: 409,
    });
    view.rerender(
      <ReminderManagerModal {...baseProps} reminderId={scheduled.id} />,
    );

    expect(screen.getByText(/其他窗口发生变化/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));

    await waitFor(() => expect(mocks.detailRefetch).toHaveBeenCalledTimes(1));
  });

  it("does not replace a draft with retained cache when an explicit reload fails", async () => {
    mocks.updateError = new ApiError("版本冲突", {
      code: "VERSION_CONFLICT",
      status: 409,
    });
    mocks.detailRefetch.mockResolvedValue({ data: scheduled, isError: true });
    renderManager({
      reminderId: scheduled.id,
      returnSession: "018f0000-0000-7000-8000-000000000022",
    });
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "失败回读不能覆盖的草稿" },
    });
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByLabelText("标题")).toHaveValue("失败回读不能覆盖的草稿");
    expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    expect(screen.getByText(/其他窗口发生变化/)).toBeInTheDocument();
  });

  it.each([
    "record",
    "record_aba",
    "create",
    "edit",
    "discard_prompt",
  ] as const)(
    "does not apply a delayed explicit reload after the editor source changes (%s)",
    async (change) => {
      let resolve!: (value: { data: Reminder }) => void;
      mocks.detailRefetch.mockReturnValue(
        new Promise<{ data: Reminder }>((done) => {
          resolve = done;
        }),
      );
      mocks.updateError = new ApiError("版本冲突", {
        code: "VERSION_CONFLICT",
        status: 409,
      });
      const props = {
        ...baseProps,
        reminderId: scheduled.id,
        returnSession: "018f0000-0000-7000-8000-000000000022",
      };
      const view = renderManager(props);
      if (change === "discard_prompt") {
        fireEvent.change(screen.getByLabelText("标题"), {
          target: { value: "新操作的本地草稿" },
        });
      }
      fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
      if (change === "record" || change === "record_aba") {
        const other = reminder({
          id: "018f0000-0000-7000-8000-000000001502",
          title: "另一条提醒",
        });
        mocks.details[other.id] = other;
        view.rerender(
          <ReminderManagerModal {...props} reminderId={other.id} />,
        );
        if (change === "record_aba")
          view.rerender(<ReminderManagerModal {...props} />);
      } else if (change === "create") {
        fireEvent.click(screen.getByRole("button", { name: "新建提醒" }));
      }
      if (change === "discard_prompt") {
        fireEvent.click(screen.getAllByRole("button", { name: "关闭" })[1]!);
      } else {
        fireEvent.change(screen.getByLabelText("标题"), {
          target: { value: "新操作的本地草稿" },
        });
      }
      await act(async () =>
        resolve({ data: reminder({ title: "旧请求返回的标题", version: 4 }) }),
      );
      if (change === "discard_prompt") {
        expect(props.onClose).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "保留草稿" }));
      }
      expect(screen.getByLabelText("标题")).toHaveValue("新操作的本地草稿");
      expect(screen.getByRole("button", { name: "返回原对话" })).toBeDisabled();
    },
  );

  it("rejects an explicit reload response for a different reminder", async () => {
    mocks.updateError = new ApiError("版本冲突", {
      code: "VERSION_CONFLICT",
      status: 409,
    });
    mocks.detailRefetch.mockResolvedValue({
      data: reminder({
        id: "018f0000-0000-7000-8000-000000001502",
        title: "不匹配的提醒",
      }),
    });
    renderManager({ reminderId: scheduled.id });
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByLabelText("标题")).toHaveValue(scheduled.title);
    expect(screen.getByText(/其他窗口发生变化/)).toBeInTheDocument();
  });

  it("does not apply an explicit reload after the manager is unmounted", async () => {
    let resolve!: (value: { data: Reminder }) => void;
    mocks.detailRefetch.mockReturnValue(
      new Promise<{ data: Reminder }>((done) => {
        resolve = done;
      }),
    );
    mocks.updateError = new ApiError("版本冲突", {
      code: "VERSION_CONFLICT",
      status: 409,
    });
    const view = renderManager({ reminderId: scheduled.id });
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
    view.unmount();
    const resets = mocks.updateReset.mock.calls.length;
    await act(async () =>
      resolve({ data: reminder({ title: "卸载后的返回", version: 4 }) }),
    );
    expect(mocks.updateReset).toHaveBeenCalledTimes(resets);
  });

  it("applies only the latest explicit reload and renews the saved baseline", async () => {
    const resolvers: Array<(value: { data: Reminder }) => void> = [];
    mocks.detailRefetch.mockImplementation(
      () =>
        new Promise<{ data: Reminder }>((resolve) => {
          resolvers.push(resolve);
        }),
    );
    mocks.updateError = new ApiError("版本冲突", {
      code: "VERSION_CONFLICT",
      status: 409,
    });
    renderManager({
      reminderId: scheduled.id,
      returnSession: "018f0000-0000-7000-8000-000000000022",
    });
    fireEvent.change(screen.getByLabelText("标题"), {
      target: { value: "明确选择重载的旧草稿" },
    });
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
    fireEvent.click(screen.getByRole("button", { name: "加载最新记录" }));
    await act(async () =>
      resolvers[0]!({
        data: reminder({ title: "已被取代的请求", version: 4 }),
      }),
    );
    expect(screen.getByLabelText("标题")).toHaveValue("明确选择重载的旧草稿");
    await act(async () =>
      resolvers[1]!({
        data: reminder({ title: "最新加载的标题", version: 5 }),
      }),
    );
    expect(screen.getByLabelText("标题")).toHaveValue("最新加载的标题");
    expect(
      screen.getByRole("link", { name: "返回原对话" }),
    ).toBeInTheDocument();
  });

  it("keeps server pagination when the manager becomes controlled", async () => {
    mocks.listMeta = { ...mocks.listMeta, total: 41 };
    renderManager();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));

    await waitFor(() =>
      expect(mocks.listHook).toHaveBeenLastCalledWith(
        expect.objectContaining({ status: "scheduled", page: 2, pageSize: 20 }),
        true,
      ),
    );
  });

  it("settles on the last valid reminder page after the list shrinks", async () => {
    mocks.listMeta = { ...mocks.listMeta, total: 21 };
    const view = renderManager();

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    await waitFor(() =>
      expect(mocks.listHook).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 2, pageSize: 20 }),
        true,
      ),
    );

    mocks.listMeta = { ...mocks.listMeta, total: 0 };
    view.rerender(<ReminderManagerModal {...baseProps} />);

    await waitFor(() =>
      expect(mocks.listHook).toHaveBeenLastCalledWith(
        expect.objectContaining({ page: 1, pageSize: 20 }),
        true,
      ),
    );
  });

  it("opens the generated inbox item from a fired detail", async () => {
    const fired = reminder({
      status: "fired",
      firedAt: "2026-08-30T01:00:05Z",
      inboxItemId: "018f0000-0000-7000-8000-000000001599",
      availableActions: [],
    });
    mocks.details[scheduled.id] = fired;
    const onOpenInboxItem = vi.fn();
    renderManager({
      status: "fired",
      reminderId: scheduled.id,
      onOpenInboxItem,
    });

    fireEvent.click(
      await screen.findByRole("button", { name: "打开收件箱条目" }),
    );
    expect(onOpenInboxItem).toHaveBeenCalledWith(fired.inboxItemId);
  });
});
