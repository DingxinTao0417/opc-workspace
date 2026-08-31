import {
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
  return render(<ReminderManagerModal {...baseProps} {...overrides} />);
}

function mutationSuccess(mock: ReturnType<typeof vi.fn>, value: Reminder) {
  const options = mock.mock.calls.at(-1)?.[1] as
    { onSuccess?: (reminder: Reminder) => void } | undefined;
  options?.onSuccess?.(value);
}

describe("ReminderManagerModal", () => {
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
    renderManager({ onStateChange });
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
    mutationSuccess(mocks.create, created);

    expect(onStateChange).toHaveBeenCalledWith({
      status: "scheduled",
      reminderId: created.id,
    });
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
