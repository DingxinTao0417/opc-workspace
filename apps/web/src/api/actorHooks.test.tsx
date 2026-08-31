import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Actor } from "../types/models";
import { ApiError } from "./client";
import {
  actorDetailQueryKey,
  actorQueryKey,
  clientQueryKey,
  inboxQueryKey,
  projectQueryKey,
  taskArtifactQueryRootKey,
  taskAssignmentQueryRootKey,
  taskEventQueryRootKey,
  taskSubmissionQueryRootKey,
  useActorQuery,
  useActorsQuery,
  useAssignmentActorOptionsQuery,
  useClientActorOptionsQuery,
  useClientFollowupActorOptionsQuery,
  useUpdateActor,
} from "./hooks";

const api = vi.hoisted(() => ({
  getActors: vi.fn(),
  getAllActors: vi.fn(),
  getActor: vi.fn(),
  updateActor: vi.fn(),
}));

vi.mock("./client", async () => {
  const actual = await vi.importActual<typeof import("./client")>("./client");
  return {
    ...actual,
    getActors: api.getActors,
    getAllActors: api.getAllActors,
    getActor: api.getActor,
    updateActor: api.updateActor,
  };
});

const actor: Actor = {
  id: "actor-person-1",
  type: "person",
  displayName: "陈设计",
  status: "active",
  isBuiltin: false,
  notes: "负责视觉",
  metadata: {},
  version: 2,
  createdAt: "2026-08-30T00:00:00Z",
  updatedAt: "2026-08-30T00:00:00Z",
};

function createQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
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
  vi.clearAllMocks();
});

describe("actor queries", () => {
  it("aborts the obsolete actor page when paging changes or the query unmounts", async () => {
    const signals: AbortSignal[] = [];
    api.getActors.mockImplementation(
      (_input: unknown, signal?: AbortSignal) => {
        if (signal) signals.push(signal);
        return new Promise(() => undefined);
      },
    );
    const queryClient = createQueryClient();
    const hook = renderHook(
      ({ page }) => useActorsQuery({ page, pageSize: 20 }),
      {
        initialProps: { page: 1 },
        wrapper: wrapperFor(queryClient),
      },
    );

    await waitFor(() => expect(signals).toHaveLength(1));
    expect(api.getActors).toHaveBeenLastCalledWith(
      { page: 1, pageSize: 20 },
      signals[0],
    );
    expect(signals[0].aborted).toBe(false);

    hook.rerender({ page: 2 });
    await waitFor(() => expect(signals).toHaveLength(2));
    expect(signals[0].aborted).toBe(true);
    expect(api.getActors).toHaveBeenLastCalledWith(
      { page: 2, pageSize: 20 },
      signals[1],
    );
    expect(signals[1].aborted).toBe(false);

    hook.unmount();
    await waitFor(() => expect(signals[1].aborted).toBe(true));
    queryClient.clear();
  });

  it("forwards query cancellation to every complete actor option query", async () => {
    api.getAllActors.mockResolvedValue([]);
    const queryClient = createQueryClient();
    const wrapper = wrapperFor(queryClient);
    const assignment = renderHook(() => useAssignmentActorOptionsQuery(), {
      wrapper,
    });
    const clientContact = renderHook(() => useClientActorOptionsQuery(), {
      wrapper,
    });
    const clientFollowup = renderHook(
      () => useClientFollowupActorOptionsQuery(),
      { wrapper },
    );

    await waitFor(() => expect(api.getAllActors).toHaveBeenCalledTimes(3));
    const calls = api.getAllActors.mock.calls as Array<
      [Record<string, string>, AbortSignal]
    >;
    expect(
      calls.filter(
        ([input]) => input.status === "active" && input.type === undefined,
      ),
    ).toHaveLength(2);
    expect(
      calls.filter(
        ([input]) => input.status === "active" && input.type === "person",
      ),
    ).toHaveLength(1);
    expect(calls.every(([, signal]) => signal instanceof AbortSignal)).toBe(
      true,
    );
    expect(new Set(calls.map(([, signal]) => signal)).size).toBe(3);

    assignment.unmount();
    clientContact.unmount();
    clientFollowup.unmount();
    queryClient.clear();
  });

  it("refreshes only the addressed detail after a version conflict and all actor facts after success", async () => {
    const conflictLatest = {
      ...actor,
      displayName: "陈设计（已改名）",
      version: 3,
      updatedAt: "2026-08-30T00:01:00Z",
    };
    const updated = {
      ...conflictLatest,
      notes: "负责视觉与交付",
      version: 4,
      updatedAt: "2026-08-30T00:02:00Z",
    };
    const listInput = { page: 1, pageSize: 20, type: "person" as const };
    const initialList = {
      items: [actor],
      meta: { page: 1, pageSize: 20, total: 1 },
    };
    const updatedList = {
      items: [updated],
      meta: { page: 1, pageSize: 20, total: 1 },
    };
    api.getActors
      .mockResolvedValueOnce(initialList)
      .mockResolvedValueOnce(updatedList);
    api.getActor
      .mockResolvedValueOnce(actor)
      .mockResolvedValueOnce(conflictLatest)
      .mockResolvedValueOnce(updated);
    api.updateActor
      .mockRejectedValueOnce(
        new ApiError("版本冲突", {
          code: "VERSION_CONFLICT",
          status: 409,
        }),
      )
      .mockResolvedValueOnce(updated);
    const queryClient = createQueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const hook = renderHook(
      () => ({
        list: useActorsQuery(listInput),
        detail: useActorQuery(actor.id),
        update: useUpdateActor(),
      }),
      { wrapper: wrapperFor(queryClient) },
    );

    await waitFor(() => {
      expect(hook.result.current.list.isSuccess).toBe(true);
      expect(hook.result.current.detail.isSuccess).toBe(true);
    });
    const listKey = [...actorQueryKey, "list", listInput] as const;
    const listBeforeConflict = queryClient.getQueryData(listKey);

    let conflictError: unknown;
    await act(async () => {
      try {
        await hook.result.current.update.mutateAsync({
          id: actor.id,
          input: { displayName: "冲突改名", expectedVersion: actor.version },
        });
      } catch (error) {
        conflictError = error;
      }
    });
    expect(conflictError).toMatchObject({ code: "VERSION_CONFLICT" });

    await waitFor(() => expect(api.getActor).toHaveBeenCalledTimes(2));
    expect(api.getActors).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(listKey)).toBe(listBeforeConflict);
    expect(hook.result.current.detail.data).toEqual(conflictLatest);
    expect(invalidate).toHaveBeenLastCalledWith({
      queryKey: actorDetailQueryKey(actor.id),
      exact: true,
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: clientQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: projectQueryKey });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxQueryKey });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: taskAssignmentQueryRootKey,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: taskEventQueryRootKey,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: taskSubmissionQueryRootKey,
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: taskArtifactQueryRootKey,
    });

    await act(async () => {
      await hook.result.current.update.mutateAsync({
        id: actor.id,
        input: {
          notes: updated.notes,
          expectedVersion: conflictLatest.version,
        },
      });
    });

    await waitFor(() => expect(api.getActors).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(api.getActor).toHaveBeenCalledTimes(3));
    expect(hook.result.current.list.data).toEqual(updatedList);
    expect(hook.result.current.detail.data).toEqual(updated);
    expect(invalidate).toHaveBeenLastCalledWith({ queryKey: actorQueryKey });

    hook.unmount();
    queryClient.clear();
  });
});
