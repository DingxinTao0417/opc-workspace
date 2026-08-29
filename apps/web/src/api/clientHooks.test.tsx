import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Client, ClientFollowup, ClientInput } from "../types/models";
import {
  clientQueryKey,
  inboxQueryKey,
  projectQueryKey,
  useClientOptionsQuery,
  useCompleteClientFollowup,
  useCreateClient,
  useUpdateClient,
} from "./hooks";

const calls = vi.hoisted(() => ({
  completeFollowup: vi.fn(),
  create: vi.fn(),
  list: vi.fn(),
  update: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    createClient: calls.create,
    completeClientFollowup: calls.completeFollowup,
    getClients: calls.list,
    updateClient: calls.update,
  };
});

const input: ClientInput = {
  name: "星河工作室",
  contactName: null,
  email: null,
  phone: null,
  notes: null,
  status: "active",
};

const client: Client = {
  id: "client-1",
  ...input,
  version: 2,
  projectCount: 1,
  latestActivityAt: null,
  createdAt: "2026-08-20T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
};

const clientFollowup: ClientFollowup = {
  id: "followup-1",
  clientId: client.id,
  clientName: client.name,
  assignedActorId: "00000000-0000-5000-8000-000000000001",
  assignedActorName: "应用所有者",
  assignedActorType: "owner",
  scheduledAt: "2026-08-29T09:00:00Z",
  timezone: "UTC",
  channel: "phone",
  purpose: "确认项目验收",
  notes: null,
  status: "completed",
  priority: "normal",
  completedAt: "2026-08-29T10:00:00Z",
  result: "已确认",
  nextStep: null,
  skippedAt: null,
  skipReason: null,
  cancelledAt: null,
  cancelReason: null,
  rescheduledFromId: null,
  version: 2,
  createdAt: "2026-08-28T10:00:00Z",
  updatedAt: "2026-08-29T10:00:00Z",
  clientVersion: 3,
};

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
}

afterEach(() => vi.clearAllMocks());

describe("client hooks", () => {
  it("requests one searchable client option page and forwards cancellation", async () => {
    calls.list.mockResolvedValue({
      items: [client],
      meta: { page: 1, pageSize: 20, total: 1 },
    });
    const { result } = renderHook(
      () => useClientOptionsQuery("  星河  ", 1, true),
      { wrapper: wrapperFor(createQueryClient()) },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(calls.list).toHaveBeenCalledOnce();
    expect(calls.list).toHaveBeenCalledWith(
      {
        page: 1,
        pageSize: 20,
        q: "星河",
        sort: "name",
      },
      expect.any(AbortSignal),
    );
  });

  it("aborts an obsolete option request when the query key changes or unmounts", async () => {
    const signals: AbortSignal[] = [];
    calls.list.mockImplementation((_input, signal?: AbortSignal) => {
      if (signal) signals.push(signal);
      return new Promise(() => undefined);
    });
    const queryClient = createQueryClient();
    const { rerender, unmount } = renderHook(
      ({ search }) => useClientOptionsQuery(search, 1, true),
      {
        initialProps: { search: "星河" },
        wrapper: wrapperFor(queryClient),
      },
    );

    await waitFor(() => expect(signals).toHaveLength(1));
    expect(signals[0].aborted).toBe(false);

    rerender({ search: "远山" });
    await waitFor(() => expect(signals).toHaveLength(2));
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);

    unmount();
    await waitFor(() => expect(signals[1].aborted).toBe(true));
  });

  it("reuses one idempotency key for an unchanged failed create retry", async () => {
    calls.create
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(client);
    const { result } = renderHook(() => useCreateClient(), {
      wrapper: wrapperFor(createQueryClient()),
    });

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(calls.create.mock.calls[0][1]).toBeTruthy();
    expect(calls.create.mock.calls[1][1]).toBe(calls.create.mock.calls[0][1]);
  });

  it("refreshes client and project aggregates after an update", async () => {
    calls.update.mockResolvedValue({ ...client, name: "星河设计", version: 3 });
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useUpdateClient(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: client.id,
        input: { name: "星河设计", expectedVersion: client.version },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: clientQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
  });

  it("refreshes Inbox projections after completing a client followup", async () => {
    calls.completeFollowup.mockResolvedValue(clientFollowup);
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useCompleteClientFollowup(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: clientFollowup.id,
        input: {
          result: "已确认",
          nextStep: null,
          completedAt: null,
          expectedVersion: 1,
        },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
  });
});
