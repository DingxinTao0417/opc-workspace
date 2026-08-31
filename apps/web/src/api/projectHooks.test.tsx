import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project } from "../types/models";
import { ApiError } from "./client";
import {
  automationQueryKey,
  clientQueryKey,
  inboxQueryKey,
  projectDetailQueryKey,
  projectQueryKey,
  roadmapMilestoneQueryKey,
  searchQueryKey,
  useProjectOptionsQuery,
  useTransitionProject,
} from "./hooks";

const transitionProjectMock = vi.hoisted(() => vi.fn());
const getProjectsMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getProjects: getProjectsMock,
    transitionProject: transitionProjectMock,
  };
});

const completedProject: Project = {
  id: "project-1",
  name: "网站重构",
  description: "",
  clientId: "client-1",
  clientName: "示例客户",
  startDate: null,
  dueDate: null,
  amountMinor: null,
  color: "#6E7BF2",
  status: "completed",
  version: 4,
  archivedFromStatus: null,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-29T00:00:00Z",
  taskSummary: {
    total: 1,
    completed: 1,
    inProgress: 0,
    remaining: 0,
    progressPercent: 100,
    actualMinutes: 30,
  },
  invoiceCount: 0,
  availableActions: ["reopen", "archive"],
};

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

describe("useTransitionProject", () => {
  it("refreshes project facts and the client detail/activity prefix on success", async () => {
    transitionProjectMock.mockResolvedValue(completedProject);
    const queryClient = createQueryClient();
    const searchKey = [
      ...searchQueryKey,
      { q: completedProject.name, types: ["project"] },
    ] as const;
    queryClient.setQueryData(searchKey, {
      items: [],
      meta: { page: 1, pageSize: 20, total: 0 },
    });
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTransitionProject(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: completedProject.id,
        action: "complete",
        expectedVersion: 3,
      }),
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(
      queryClient.getQueryData(projectDetailQueryKey("project-1")),
    ).toEqual(completedProject);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clientQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: automationQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: roadmapMilestoneQueryKey,
    });
    expect(queryClient.getQueryState(searchKey)?.isInvalidated).toBe(true);
  });

  it("refreshes project and client facts after a version conflict", async () => {
    transitionProjectMock.mockRejectedValue(
      new ApiError("Project has changed", {
        code: "VERSION_CONFLICT",
        status: 409,
      }),
    );
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useTransitionProject(), {
      wrapper: wrapperFor(queryClient),
    });

    act(() =>
      result.current.mutate({
        id: completedProject.id,
        action: "reopen",
        expectedVersion: completedProject.version,
      }),
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
      expect(invalidate).toHaveBeenCalledWith({ queryKey: clientQueryKey });
      expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: automationQueryKey,
      });
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: roadmapMilestoneQueryKey,
      });
      expect(invalidate).toHaveBeenCalledWith({ queryKey: searchQueryKey });
    });
  });
});

describe("useProjectOptionsQuery", () => {
  it("requests one normalized project page including the archive mode", async () => {
    getProjectsMock.mockResolvedValue({
      items: [completedProject],
      meta: { page: 2, pageSize: 20, total: 21 },
    });
    const { result } = renderHook(
      () => useProjectOptionsQuery("  网站  ", 2, true, true),
      { wrapper: wrapperFor(createQueryClient()) },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(getProjectsMock).toHaveBeenCalledOnce();
    expect(getProjectsMock).toHaveBeenCalledWith(
      {
        page: 2,
        pageSize: 20,
        query: "网站",
        sort: "name",
        includeArchived: true,
      },
      expect.any(AbortSignal),
    );
  });

  it("aborts an obsolete project page when the search key changes or unmounts", async () => {
    const signals: AbortSignal[] = [];
    getProjectsMock.mockImplementation(
      (_input: unknown, signal?: AbortSignal) => {
        if (signal) signals.push(signal);
        return new Promise(() => undefined);
      },
    );
    const queryClient = createQueryClient();
    const { rerender, unmount } = renderHook(
      ({ search }) => useProjectOptionsQuery(search, 1, true),
      {
        initialProps: { search: "网站" },
        wrapper: wrapperFor(queryClient),
      },
    );

    await waitFor(() => expect(signals).toHaveLength(1));
    expect(signals[0].aborted).toBe(false);

    rerender({ search: "小程序" });
    await waitFor(() => expect(signals).toHaveLength(2));
    expect(signals[0].aborted).toBe(true);
    expect(signals[1].aborted).toBe(false);

    unmount();
    await waitFor(() => expect(signals[1].aborted).toBe(true));
  });
});
