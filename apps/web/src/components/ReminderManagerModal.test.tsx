import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Reminder } from "../types/models";
import { ReminderManagerModal } from "./ReminderManagerModal";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  cancel: vi.fn(),
  listRefetch: vi.fn(),
  detailRefetch: vi.fn(),
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

function mutation(mutate: ReturnType<typeof vi.fn>) {
  return {
    mutate,
    reset: vi.fn(),
    isPending: false,
    error: null,
  };
}

vi.mock("../api/hooks", () => ({
  useRemindersQuery: () => ({
    data: {
      items: [scheduled],
      meta: {
        page: 1,
        pageSize: 20,
        total: 1,
        serverNow: "2026-08-28T10:00:00Z",
      },
    },
    isPending: false,
    isFetching: false,
    isPlaceholderData: false,
    isSuccess: true,
    isError: false,
    refetch: mocks.listRefetch,
  }),
  useReminderQuery: (id: string | null) => ({
    data: id ? scheduled : undefined,
    isPending: Boolean(id) && false,
    isFetching: false,
    error: null,
    refetch: mocks.detailRefetch,
  }),
  useCreateReminder: () => mutation(mocks.create),
  useUpdateReminder: () => mutation(mocks.update),
  useCancelReminder: () => mutation(mocks.cancel),
}));

describe("ReminderManagerModal", () => {
  beforeEach(() => {
    mocks.detailRefetch.mockResolvedValue({ data: scheduled });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens a scheduled reminder, preserves editable facts, and submits its version", async () => {
    render(
      <ReminderManagerModal onClose={vi.fn()} onOpenInboxItem={vi.fn()} open />,
    );

    fireEvent.click(screen.getByRole("button", { name: /复查本地备份/ }));
    const title = await screen.findByLabelText("标题");
    expect(title).toHaveValue("复查本地备份");
    fireEvent.change(title, { target: { value: "复查本地恢复点" } });
    fireEvent.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update.mock.calls[0][0]).toMatchObject({
      id: scheduled.id,
      input: {
        title: "复查本地恢复点",
        summary: "确认恢复点可用",
        priority: "P1",
        expectedVersion: 3,
      },
    });
    expect(mocks.update.mock.calls[0][0].input.triggerAt).toBe(
      new Date("2099-08-30T01:00:00Z").toISOString(),
    );
  });

  it("creates a future one-time reminder from the same manager", async () => {
    render(<ReminderManagerModal onClose={vi.fn()} open />);
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
    });
  });

  it("requires and submits an auditable cancellation reason", async () => {
    render(<ReminderManagerModal onClose={vi.fn()} open />);
    fireEvent.click(screen.getByRole("button", { name: /复查本地备份/ }));
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
  });
});
