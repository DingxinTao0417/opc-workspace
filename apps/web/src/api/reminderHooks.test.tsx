import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Reminder } from "../types/models";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  reminderDetailQueryKey,
  useCancelReminder,
  useCreateReminder,
  useReminderQuery,
  useRemindersQuery,
} from "./hooks";

const calls = vi.hoisted(() => ({
  list: vi.fn(),
  detail: vi.fn(),
  create: vi.fn(),
  cancel: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getReminders: calls.list,
    getReminder: calls.detail,
    createReminder: calls.create,
    cancelReminder: calls.cancel,
  };
});

function reminder(
  status: Reminder["status"] = "scheduled",
  version = 1,
): Reminder {
  const terminal = status !== "scheduled";
  return {
    id: "018f0000-0000-7000-8000-000000001501",
    sourceEntityType: "manual",
    sourceEntityId: null,
    title: "复查本地备份",
    summary: "",
    priority: "P2",
    triggerAt: "2099-08-30T01:00:00Z",
    status,
    sourceEventKey: "reminder:018f0000-0000-7000-8000-000000001501:due",
    createdByActorId: "00000000-0000-5000-8000-000000000001",
    seriesId: "018f0000-0000-7000-8000-000000001501",
    recurrenceType: "none",
    recurrenceInterval: 1,
    recurrenceTimezone: "UTC",
    occurrenceNumber: 1,
    recurrenceAnchorDay: 1,
    firedAt: status === "fired" ? "2099-08-30T01:00:01Z" : null,
    inboxItemId:
      status === "fired" ? "018f0000-0000-7000-8000-000000001502" : null,
    cancelledByActorId:
      status === "cancelled" ? "00000000-0000-5000-8000-000000000001" : null,
    cancelledAt: status === "cancelled" ? "2026-08-28T11:00:00Z" : null,
    cancelReason: status === "cancelled" ? "计划取消" : null,
    version,
    createdAt: "2026-08-28T10:00:00Z",
    updatedAt: "2026-08-28T10:00:00Z",
    availableActions: terminal ? [] : ["edit", "cancel"],
  };
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("Reminder hooks", () => {
  it("polls the local scheduler result at the Inbox refresh interval", async () => {
    vi.useFakeTimers();
    calls.list.mockResolvedValue({
      items: [],
      meta: {
        page: 1,
        pageSize: 20,
        total: 0,
        serverNow: "2026-08-28T10:00:00Z",
      },
    });
    const queryClient = createQueryClient();
    renderHook(() => useRemindersQuery(), { wrapper: wrapperFor(queryClient) });
    await act(async () => void (await vi.advanceTimersByTimeAsync(0)));
    expect(calls.list).toHaveBeenCalledTimes(1);
    await act(
      async () =>
        void (await vi.advanceTimersByTimeAsync(
          INBOX_LIST_REFRESH_INTERVAL_MS,
        )),
    );
    expect(calls.list).toHaveBeenCalledTimes(2);
    expect(calls.list).toHaveBeenLastCalledWith({}, expect.any(AbortSignal));
  });

  it("forwards cancellation to list and detail queries while null detail stays disabled", async () => {
    const listInput = { status: "scheduled" as const, page: 2, pageSize: 20 };
    calls.list.mockResolvedValue({
      items: [],
      meta: {
        page: 2,
        pageSize: 20,
        total: 0,
        serverNow: "2026-08-28T10:00:00Z",
      },
    });
    calls.detail.mockResolvedValue(reminder());
    const queryClient = createQueryClient();
    const wrapper = wrapperFor(queryClient);

    const disabled = renderHook(() => useReminderQuery(null), { wrapper });
    expect(disabled.result.current.fetchStatus).toBe("idle");
    expect(calls.detail).not.toHaveBeenCalled();

    const list = renderHook(() => useRemindersQuery(listInput), { wrapper });
    const detail = renderHook(() => useReminderQuery(reminder().id), {
      wrapper,
    });
    await waitFor(() => expect(list.result.current.isSuccess).toBe(true));
    await waitFor(() => expect(detail.result.current.isSuccess).toBe(true));

    expect(calls.list).toHaveBeenCalledWith(listInput, expect.any(AbortSignal));
    expect(calls.detail).toHaveBeenCalledWith(
      reminder().id,
      expect.any(AbortSignal),
    );
    expect(
      queryClient.getQueryData(reminderDetailQueryKey(reminder().id)),
    ).toEqual(reminder());
  });

  it("reuses the create key after a lost response and caches the result", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(reminder());
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCreateReminder(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      title: "复查本地备份",
      summary: "",
      priority: "P2" as const,
      triggerAt: "2099-08-30T01:00:00Z",
      recurrenceType: "none" as const,
      recurrenceInterval: 1,
      recurrenceTimezone: "UTC",
    };
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(calls.create.mock.calls[1][1]).toBe(calls.create.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(reminderDetailQueryKey(reminder().id)),
    ).toEqual(reminder());
  });

  it("reuses the cancel key and stores the terminal version", async () => {
    const cancelled = reminder("cancelled", 2);
    calls.cancel
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(cancelled);
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCancelReminder(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = { id: reminder().id, reason: "计划取消", expectedVersion: 1 };
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(calls.cancel.mock.calls[1][1]).toBe(calls.cancel.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(reminderDetailQueryKey(reminder().id)),
    ).toEqual(cancelled);
  });
});
