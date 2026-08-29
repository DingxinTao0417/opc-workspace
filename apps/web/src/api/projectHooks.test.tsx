import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project } from "../types/models";
import { ApiError } from "./client";
import {
  clientQueryKey,
  projectDetailQueryKey,
  projectQueryKey,
  useTransitionProject,
} from "./hooks";

const transitionProjectMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return { ...actual, transitionProject: transitionProjectMock };
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
    });
  });
});
