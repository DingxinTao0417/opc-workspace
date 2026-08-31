import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  inboxQueryKey,
  roadmapMilestoneQueryKey,
  searchQueryKey,
  useUpdateRoadmapMilestone,
} from "./hooks";

const updateRoadmapMilestoneMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    updateRoadmapMilestone: updateRoadmapMilestoneMock,
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

describe("roadmap milestone mutations", () => {
  it("invalidates milestone lists and unified search after an update", async () => {
    updateRoadmapMilestoneMock.mockResolvedValue({ id: "milestone-1" });
    const queryClient = createQueryClient();
    const milestoneKey = [
      ...roadmapMilestoneQueryKey,
      "list",
      { year: 2026 },
    ] as const;
    const searchKey = [
      ...searchQueryKey,
      { q: "路线图交互收口", types: ["roadmap_milestone"] },
    ] as const;
    queryClient.setQueryData(milestoneKey, { items: [] });
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const { result } = renderHook(() => useUpdateRoadmapMilestone(), {
      wrapper: wrapperFor(queryClient),
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    act(() =>
      result.current.mutate({
        id: "milestone-1",
        input: { title: "路线图交互收口", expectedVersion: 4 },
      }),
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(queryClient.getQueryState(milestoneKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
  });
});
