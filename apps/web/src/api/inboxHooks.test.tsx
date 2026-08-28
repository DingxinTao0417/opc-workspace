import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../types/models";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  inboxDetailQueryKey,
  useCreateInboxItem,
  useInboxItemCommand,
  useInboxItemsQuery,
} from "./hooks";

const calls = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  command: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getInboxItems: calls.list,
    createInboxItem: calls.create,
    executeInboxItemCommand: calls.command,
  };
});

function inboxItem(version = 1): InboxItem {
  return {
    id: "018f0000-0000-7000-8000-000000000801",
    kind: "manual",
    title: "确认本周交付范围",
    summary: "",
    sourceEntityType: "manual",
    sourceEntityId: null,
    sourceEventKey: null,
    sourceDeletedAt: null,
    priority: "P2",
    status: "open",
    resolutionPolicy: "manual",
    dueAt: null,
    readAt: version > 1 ? "2026-08-28T10:01:00Z" : null,
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
    version,
    createdAt: "2026-08-28T10:00:00Z",
    updatedAt: "2026-08-28T10:00:00Z",
    availableActions:
      version > 1
        ? ["edit", "snooze", "resolve", "dismiss"]
        : ["edit", "read", "snooze", "resolve", "dismiss"],
  };
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
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

describe("inbox hooks", () => {
  it("refreshes the list so snoozed items return when the server clock reaches them", async () => {
    vi.useFakeTimers();
    calls.list.mockResolvedValue({
      items: [],
      meta: {
        page: 1,
        pageSize: 30,
        total: 0,
        unreadTotal: 0,
        snapshotAt: "2026-08-28T10:00:00.000000000Z",
        serverNow: "2026-08-28T10:00:00.000000000Z",
      },
    });
    const queryClient = createQueryClient();

    renderHook(() => useInboxItemsQuery(), {
      wrapper: wrapperFor(queryClient),
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(calls.list).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(INBOX_LIST_REFRESH_INTERVAL_MS);
    });
    expect(calls.list).toHaveBeenCalledTimes(2);
  });

  it("reuses one create idempotency key when the user retries the same draft", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(inboxItem());
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useCreateInboxItem(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      title: "确认本周交付范围",
      summary: "",
      priority: "P2" as const,
      dueAt: null,
    };

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.create.mock.calls[0][1]).toBeTruthy();
    expect(calls.create.mock.calls[1][1]).toBe(calls.create.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(inboxDetailQueryKey(inboxItem().id)),
    ).toEqual(inboxItem());
  });

  it("reuses the command key after a lost response and caches the returned version", async () => {
    calls.command
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(inboxItem(2));
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useInboxItemCommand(), {
      wrapper: wrapperFor(queryClient),
    });
    const input = {
      action: "read" as const,
      id: inboxItem().id,
      expectedVersion: 1,
    };

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.command.mock.calls[1][1]).toBe(calls.command.mock.calls[0][1]);
    expect(
      queryClient.getQueryData(inboxDetailQueryKey(inboxItem().id)),
    ).toEqual(inboxItem(2));
  });
});
