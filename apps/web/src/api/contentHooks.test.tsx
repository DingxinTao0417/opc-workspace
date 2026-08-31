import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./client";
import {
  contentItemDetailQueryKey,
  contentItemListQueryKey,
  contentItemQueryKey,
  inboxQueryKey,
  searchQueryKey,
  useDeleteContentItem,
} from "./hooks";

const deleteContentItemMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    deleteContentItem: deleteContentItemMock,
  };
});

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

afterEach(() => vi.clearAllMocks());

describe("useDeleteContentItem", () => {
  it("removes the deleted detail and invalidates content lists and Inbox facts", async () => {
    deleteContentItemMock.mockResolvedValue({ deletedId: "content-1" });
    const queryClient = createQueryClient();
    queryClient.setQueryData(contentItemDetailQueryKey("content-1"), {
      id: "content-1",
    });
    const searchKey = [
      ...searchQueryKey,
      { q: "内容发布", types: ["content_item"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const remove = vi.spyOn(queryClient, "removeQueries");
    const { result } = renderHook(() => useDeleteContentItem(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate({ id: "content-1", expectedVersion: 7 }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(deleteContentItemMock).toHaveBeenCalledWith("content-1", 7);
    expect(remove).toHaveBeenCalledWith({
      queryKey: contentItemDetailQueryKey("content-1"),
    });
    expect(
      queryClient.getQueryData(contentItemDetailQueryKey("content-1")),
    ).toBeUndefined();
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: contentItemListQueryKey,
    });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: contentItemQueryKey,
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: contentItemDetailQueryKey("content-1"),
    });
  });

  it("refreshes the list and exact detail after a version conflict", async () => {
    deleteContentItemMock.mockRejectedValue(
      new ApiError("Content item changed", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const remove = vi.spyOn(queryClient, "removeQueries");
    const { result } = renderHook(() => useDeleteContentItem(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate({ id: "content-1", expectedVersion: 7 }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: contentItemListQueryKey,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: contentItemDetailQueryKey("content-1"),
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: searchQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(remove).not.toHaveBeenCalled();
  });

  it("refreshes Inbox facts when active source projections block deletion", async () => {
    deleteContentItemMock.mockRejectedValue(
      new ApiError("Active Inbox sources", {
        code: "CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const remove = vi.spyOn(queryClient, "removeQueries");
    const { result } = renderHook(() => useDeleteContentItem(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() => result.current.mutate({ id: "content-1", expectedVersion: 7 }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: contentItemListQueryKey,
    });
    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: contentItemDetailQueryKey("content-1"),
    });
    expect(remove).not.toHaveBeenCalled();
  });
});
