import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Project, ProjectInput } from "../types/models";
import { useCreateProject } from "./hooks";

const createProjectMock = vi.hoisted(() => vi.fn());

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return { ...actual, createProject: createProjectMock };
});

const input: ProjectInput = {
  name: "稳定重试项目",
  description: "验证响应丢失后的人工重试",
  clientId: null,
  startDate: null,
  dueDate: null,
  amountMinor: null,
  color: "#6E7BF2",
};

const project: Project = {
  id: "project-1",
  ...input,
  clientName: null,
  status: "planning",
  version: 1,
  archivedFromStatus: null,
  createdAt: "2026-08-27T00:00:00Z",
  updatedAt: "2026-08-27T00:00:00Z",
  taskSummary: {
    total: 0,
    completed: 0,
    inProgress: 0,
    remaining: 0,
    progressPercent: 0,
    actualMinutes: 0,
  },
  invoiceCount: 0,
  availableActions: ["start", "archive"],
};

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return function Wrapper({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useCreateProject", () => {
  afterEach(() => vi.clearAllMocks());

  it("reuses the same idempotency key when the same request is retried", async () => {
    createProjectMock
      .mockRejectedValueOnce(new Error("response lost"))
      .mockResolvedValueOnce(project);
    const { result } = renderHook(() => useCreateProject(), {
      wrapper: createWrapper(),
    });

    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isError).toBe(true));
    act(() => result.current.mutate(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const firstKey = createProjectMock.mock.calls[0][1];
    const retryKey = createProjectMock.mock.calls[1][1];
    expect(firstKey).toBeTruthy();
    expect(retryKey).toBe(firstKey);
  });
});
