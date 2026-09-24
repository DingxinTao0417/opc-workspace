import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import type { Actor, ClientFollowup } from "../types/models";
import { useAiWorkbenchHandoff } from "../store/aiWorkbenchHandoff";
import { useAiChatStore } from "../store/aiChat";
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
  detail: vi.fn(),
  responsePage: null as number | null,
  total: 1,
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

vi.mock("../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api/client")>()),
  getClientFollowup: state.detail,
}));

vi.mock("../api/hooks", () => ({
  clientActivityQueryKey: (id: string) => ["clients", id, "activities"],
  clientFollowupQueryKey: (id: string) => ["clients", id, "followups"],
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

const queryClients: QueryClient[] = [];

describe("ClientFollowupsSection", () => {
  beforeEach(() => {
    useAiWorkbenchHandoff.setState({ pending: null, pendingIssue: null });
    state.responsePage = null;
    state.total = 1;
    state.query.mockImplementation(
      (_clientId: string, input: { page?: number }) => ({
        data: {
          items: [followup],
          meta: {
            page: state.responsePage ?? input.page ?? 1,
            pageSize: 6,
            total: state.total,
            serverNow: "2026-08-29T12:00:00Z",
          },
        },
        isError: false,
        isFetching: false,
        isPending: false,
        isPlaceholderData: false,
        isSuccess: true,
        refetch: vi.fn(),
      }),
    );
    for (const mutation of [
      state.create,
      state.update,
      state.complete,
      state.skip,
      state.cancel,
      state.reschedule,
    ]) {
      mutation.isPending = false;
      mutation.mutate.mockClear();
    }
  });

  afterEach(() => {
    cleanup();
    queryClients.splice(0).forEach((client) => client.clear());
    useAiChatStore.setState({ activeSessionId: "" });
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

  it("settles on the last valid followup page after the timeline shrinks", async () => {
    state.total = 7;
    const view = render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(state.query).toHaveBeenLastCalledWith(
      "client-1",
      expect.objectContaining({ page: 2 }),
    );

    state.total = 0;
    view.rerender(<ClientFollowupsSection clientId="client-1" />);

    await waitFor(() =>
      expect(state.query).toHaveBeenLastCalledWith(
        "client-1",
        expect.objectContaining({ page: 1 }),
      ),
    );
  });

  it("filters the client timeline through the service-derived overdue query", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.change(screen.getByLabelText("回访状态筛选"), {
      target: { value: "overdue" },
    });

    expect(state.query).toHaveBeenLastCalledWith("client-1", {
      dueState: "overdue",
      page: 1,
      pageSize: 6,
      status: "planned",
    });
    expect(screen.getByText("已逾期 1 条")).toBeTruthy();
  });

  it("filters the client timeline by its active local assignee", () => {
    render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.change(screen.getByLabelText("回访负责人筛选"), {
      target: { value: actor.id },
    });

    expect(state.query).toHaveBeenLastCalledWith("client-1", {
      assignedActorId: actor.id,
      page: 1,
      pageSize: 6,
    });
  });

  it("uses the Sidecar clock rather than the browser clock for overdue labels", () => {
    state.query.mockReturnValue({
      data: {
        items: [followup],
        meta: {
          page: 1,
          pageSize: 6,
          total: 1,
          serverNow: "2026-10-01T00:00:00Z",
        },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isSuccess: true,
      refetch: vi.fn(),
    });

    render(<ClientFollowupsSection clientId="client-1" />);

    expect(screen.getByText("已逾期")).toBeTruthy();
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

  it("keeps an inactive client followup closable without exposing new plan actions", () => {
    render(
      <ClientFollowupsSection clientId="client-1" clientStatus="inactive" />,
    );

    expect(screen.getByText(/客户已停用/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "安排回访" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: `编辑回访 ${followup.purpose}` }),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "重排" })).toBeNull();
    expect(screen.getByRole("button", { name: "完成" })).toBeEnabled();

    fireEvent.click(screen.getByRole("button", { name: "完成" }));
    expect(
      screen.queryByRole("button", { name: "同时安排下一次本地回访" }),
    ).toBeNull();
  });

  it("closes stale plan editors when another window inactivates the client", () => {
    const view = render(<ClientFollowupsSection clientId="client-1" />);

    fireEvent.click(screen.getByRole("button", { name: "安排回访" }));
    expect(screen.getByText("安排本地回访")).toBeTruthy();

    view.rerender(
      <ClientFollowupsSection clientId="client-1" clientStatus="inactive" />,
    );

    expect(screen.queryByText("安排本地回访")).toBeNull();
    expect(screen.getByText(/客户已停用/)).toBeTruthy();
  });

  const clientId = "018f0000-0000-7000-8000-000000000901";
  const followupId = "018f0000-0000-7000-8000-000000000902";
  const sessionId = "018f0000-0000-7000-8000-000000000903";
  function HandoffPath() {
    return <output aria-label="当前地址">{useLocation().pathname}</output>;
  }
  function renderHandoff(selected = false) {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClients.push(queryClient);
    const close = vi.fn();
    state.detail.mockResolvedValue({ ...followup, id: followupId, clientId });
    state.query.mockReturnValue({
      data: {
        items: [{ ...followup, id: followupId, clientId }],
        meta: { page: 1, pageSize: 6, total: 1, serverNow: followup.createdAt },
      },
      isError: false,
      isFetching: false,
      isPending: false,
      isPlaceholderData: false,
      isSuccess: true,
      refetch: vi.fn(),
    });
    const element = () => (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[`/clients/${clientId}`]}>
          <HandoffPath />
          <ClientFollowupsSection
            clientId={clientId}
            selectedId={selected ? followupId : undefined}
            returnSession={selected ? sessionId : undefined}
            onClearSelection={close}
          />
        </MemoryRouter>
      </QueryClientProvider>
    );
    return { ...render(element()), element, close };
  }

  it.each([
    ["安排回访", "目的", "取消"],
    [`编辑回访 ${followup.purpose}`, "目的", "取消"],
    ["完成", "回访结果", "返回"],
    ["重排", "重排原因", "返回"],
    ["跳过", "原因", "返回"],
    ["取消", "原因", "返回"],
  ])(
    "preserves the %s draft before handing a followup to the agent",
    (open, field, close) => {
      renderHandoff();
      fireEvent.click(screen.getByRole("button", { name: open }));
      fireEvent.change(screen.getByLabelText(field), {
        target: { value: "尚未保存的私人草稿" },
      });
      const handoff = screen.getByRole("button", { name: "交给智能体" });
      expect(handoff).toBeDisabled();
      expect(screen.getByText(/请先保存或取消当前回访编辑/)).toBeVisible();
      fireEvent.click(handoff);
      expect(screen.getByLabelText(field)).toHaveValue("尚未保存的私人草稿");
      expect(screen.getByLabelText("当前地址")).toHaveTextContent(
        `/clients/${clientId}`,
      );
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
      for (const mutation of [
        state.create,
        state.update,
        state.complete,
        state.skip,
        state.cancel,
        state.reschedule,
      ]) {
        expect(mutation.mutate).not.toHaveBeenCalled();
      }
      fireEvent.click(
        within(
          screen
            .getByLabelText(field)
            .closest(".client-followup-editor") as HTMLElement,
        ).getByRole("button", { name: close }),
      );
      expect(handoff).toBeEnabled();
      fireEvent.click(handoff);
      expect(screen.getByLabelText("当前地址")).toHaveTextContent("/ai");
      const pending = useAiWorkbenchHandoff.getState().pendingIssue;
      expect(pending).toMatchObject({
        route: `/clients/${clientId}?followup=${followupId}`,
        scopes: ["work", "clients", "actions"],
      });
      expect(pending?.prompt).not.toContain("尚未保存的私人草稿");
    },
  );

  it.each([
    "create",
    "update",
    "complete",
    "skip",
    "cancel",
    "reschedule",
  ] as const)(
    "keeps handoff blocked while %s is pending and restores it after settling",
    (kind) => {
      state[kind].isPending = true;
      const view = renderHandoff();
      const handoff = screen.getByRole("button", { name: "交给智能体" });
      expect(handoff).toBeDisabled();
      expect(screen.getByText(/回访操作正在处理中/)).toBeVisible();
      fireEvent.click(handoff);
      expect(useAiWorkbenchHandoff.getState().pendingIssue).toBeNull();
      expect(screen.getByLabelText("当前地址")).toHaveTextContent(
        `/clients/${clientId}`,
      );
      state[kind].isPending = false;
      view.rerender(view.element());
      expect(handoff).toBeEnabled();
    },
  );

  it.each([
    [`编辑回访 ${followup.purpose}`, "目的", "取消"],
    ["完成", "回访结果", "返回"],
  ])(
    "keeps selected followup navigation blocked while the %s draft exists",
    async (open, field, dismiss) => {
      const view = renderHandoff(true);
      const panel = screen.getByLabelText("定位的客户回访");
      await within(panel).findByText(followup.purpose);
      expect(state.detail).toHaveBeenCalledWith(
        followupId,
        expect.any(AbortSignal),
      );
      useAiChatStore.setState({ activeSessionId: "different-session" });
      fireEvent.click(within(panel).getByRole("button", { name: open }));
      fireEvent.change(screen.getByLabelText(field), {
        target: { value: "保留这个未保存草稿" },
      });
      const returnButton = within(panel).getByRole("button", {
        name: "返回原对话",
      });
      const closeButton = within(panel).getByRole("button", {
        name: "关闭定位",
      });
      expect(returnButton).toBeDisabled();
      expect(closeButton).toBeDisabled();
      fireEvent.click(returnButton);
      fireEvent.click(closeButton);
      expect(view.close).not.toHaveBeenCalled();
      expect(screen.getByLabelText(field)).toHaveValue("保留这个未保存草稿");
      expect(useAiChatStore.getState().activeSessionId).toBe(
        "different-session",
      );
      expect(screen.getByLabelText("当前地址")).toHaveTextContent(
        `/clients/${clientId}`,
      );
      fireEvent.click(
        within(
          screen
            .getByLabelText(field)
            .closest(".client-followup-editor") as HTMLElement,
        ).getByRole("button", { name: dismiss }),
      );
      expect(closeButton).toBeEnabled();
      fireEvent.click(closeButton);
      expect(view.close).toHaveBeenCalledOnce();
      fireEvent.click(within(panel).getByRole("link", { name: "返回原对话" }));
      expect(screen.getByLabelText("当前地址")).toHaveTextContent("/ai");
      expect(useAiChatStore.getState().activeSessionId).toBe(sessionId);
    },
  );

  it.each([
    "create",
    "update",
    "complete",
    "skip",
    "cancel",
    "reschedule",
  ] as const)(
    "blocks selected followup return and close until %s settles",
    async (kind) => {
      state[kind].isPending = true;
      const view = renderHandoff(true);
      const panel = screen.getByLabelText("定位的客户回访");
      await within(panel).findByText(followup.purpose);
      expect(
        within(panel).getByRole("button", { name: "返回原对话" }),
      ).toBeDisabled();
      const closeButton = within(panel).getByRole("button", {
        name: "关闭定位",
      });
      expect(closeButton).toBeDisabled();
      fireEvent.click(closeButton);
      expect(view.close).not.toHaveBeenCalled();
      state[kind].isPending = false;
      view.rerender(view.element());
      expect(closeButton).toBeEnabled();
      expect(
        within(panel).getByRole("link", { name: "返回原对话" }),
      ).toHaveAttribute("href", "/ai");
    },
  );
});
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
