import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Actor, ClientFollowup } from "../types/models";
import { ClientFollowupsSection } from "./ClientFollowupsSection";

const actor: Actor = {
  id: "owner-1",
  type: "owner",
  displayName: "工作区 Owner",
  status: "active",
  isBuiltin: true,
  notes: "",
  metadata: {},
  version: 1,
  createdAt: "2026-08-28T08:00:00Z",
  updatedAt: "2026-08-28T08:00:00Z",
};

const followup: ClientFollowup = {
  id: "followup-1",
  clientId: "client-1",
  clientName: "星河工作室",
  assignedActorId: actor.id,
  assignedActorName: actor.displayName,
  assignedActorType: actor.type,
  scheduledAt: "2026-09-01T08:00:00Z",
  timezone: "Asia/Shanghai",
  channel: "微信",
  purpose: "确认交付反馈",
  notes: "先确认是否验收",
  status: "planned",
  priority: "normal",
  completedAt: null,
  result: null,
  nextStep: null,
  skippedAt: null,
  skipReason: null,
  cancelledAt: null,
  cancelReason: null,
  rescheduledFromId: null,
  version: 2,
  createdAt: "2026-08-28T08:00:00Z",
  updatedAt: "2026-08-28T08:00:00Z",
  clientVersion: 3,
  nextFollowup: null,
};

const state = vi.hoisted(() => ({
  query: vi.fn(),
  create: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  update: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  complete: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  skip: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  cancel: { error: null, isPending: false, mutate: vi.fn(), reset: vi.fn() },
  reschedule: {
    error: null,
    isPending: false,
    mutate: vi.fn(),
    reset: vi.fn(),
  },
}));

vi.mock("../api/hooks", () => ({
  useClientFollowupsQuery: state.query,
  useClientFollowupActorOptionsQuery: () => ({
    data: [actor],
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }),
  useCreateClientFollowup: () => state.create,
  useUpdateClientFollowup: () => state.update,
  useCompleteClientFollowup: () => state.complete,
  useSkipClientFollowup: () => state.skip,
  useCancelClientFollowup: () => state.cancel,
  useRescheduleClientFollowup: () => state.reschedule,
}));

describe("ClientFollowupsSection", () => {
  beforeEach(() => {
    state.query.mockReturnValue({
      data: { items: [followup], meta: { page: 1, pageSize: 6, total: 1 } },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    for (const mutation of [
      state.create,
      state.update,
      state.complete,
      state.skip,
      state.cancel,
      state.reschedule,
    ]) {
      mutation.mutate.mockClear();
    }
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("creates a local plan with the selected active owner", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "安排回访" }));
    fireEvent.change(screen.getByLabelText("渠道"), {
      target: { value: "电话" },
    });
    fireEvent.change(screen.getByLabelText("目的"), {
      target: { value: "确认范围" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存回访计划" }));

    expect(state.create.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        clientId: "client-1",
        assignedActorId: actor.id,
        channel: "电话",
        purpose: "确认范围",
        priority: "normal",
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("filters the client timeline through the existing paginated status query", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.change(screen.getByLabelText("回访状态筛选"), {
      target: { value: "completed" },
    });

    expect(state.query).toHaveBeenLastCalledWith("client-1", {
      page: 1,
      pageSize: 6,
      status: "completed",
    });
    expect(screen.getByText("已完成 1 条")).toBeTruthy();
  });

  it("requires a result before issuing a versioned completion command", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "完成" }));
    fireEvent.click(screen.getByRole("button", { name: "记录回访结果" }));
    expect(screen.getByText("回访结果需填写 1–4,000 个字符。")).toBeTruthy();
    expect(state.complete.mutate).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("回访结果"), {
      target: { value: "客户确认验收。" },
    });
    fireEvent.click(screen.getByRole("button", { name: "记录回访结果" }));
    expect(state.complete.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        id: followup.id,
        input: expect.objectContaining({
          result: "客户确认验收。",
          expectedVersion: followup.version,
        }),
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });

  it("can schedule the next local followup in the completion command", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "完成" }));
    fireEvent.change(screen.getByLabelText("回访结果"), {
      target: { value: "客户希望下月继续沟通。" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "同时安排下一次本地回访" }),
    );
    fireEvent.change(screen.getByLabelText("渠道"), {
      target: { value: "电话" },
    });
    fireEvent.change(screen.getByLabelText("目的"), {
      target: { value: "确认下一阶段需求" },
    });
    fireEvent.click(screen.getByRole("button", { name: "记录回访结果" }));

    expect(state.complete.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        id: followup.id,
        input: expect.objectContaining({
          nextFollowup: expect.objectContaining({
            assignedActorId: actor.id,
            channel: "电话",
            purpose: "确认下一阶段需求",
          }),
        }),
      }),
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
  });
});
