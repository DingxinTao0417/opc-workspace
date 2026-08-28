import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Client, ClientInput } from "../types/models";
import {
  clientQueryKey,
  projectQueryKey,
  useCreateClient,
  useUpdateClient,
} from "./hooks";

const calls = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return { ...actual, createClient: calls.create, updateClient: calls.update };
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
});
