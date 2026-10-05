import type {
  CreateInboxItemInput,
  ForceResolveInboxItemInput,
  InboxEventListParams,
  InboxItemCommandInput,
  InboxItemListParams,
  InboxItemTaskListParams,
  LinkInboxItemTaskInput,
  MarkAllInboxReadInput,
  SplitInboxItemInput,
  UnlinkInboxItemTaskInput,
  UpdateInboxItemInput,
  UpdateInboxItemTaskRequirementInput,
} from "../../types/models";
import {
  ApiError,
  createInboxItem,
  executeInboxItemCommand,
  forceResolveInboxItem,
  getInboxItem,
  getInboxItemEvents,
  getInboxItems,
  getInboxItemTasks,
  getInboxStats,
  linkInboxItemTask,
  markAllInboxItemsRead,
  splitInboxItem,
  unlinkInboxItemTask,
  updateInboxItem,
  updateInboxItemTaskRequirement,
} from "../client";
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateInboxFacts } from "./invalidation";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  inboxDetailQueryKey,
  inboxEventQueryKey,
  inboxQueryKey,
  inboxStatsQueryKey,
  inboxTaskRelationQueryKey,
  taskQueryKey,
} from "./queryKeys";

export function useInboxStatsQuery() {
  return useQuery({
    queryKey: inboxStatsQueryKey,
    queryFn: getInboxStats,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: 15_000,
  });
}

export function useInboxItemsQuery(
  input: InboxItemListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...inboxQueryKey, "list", input],
    queryFn: ({ signal }) => getInboxItems(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

export function useInboxItemQuery(id: string | null) {
  return useQuery({
    queryKey: inboxDetailQueryKey(id ?? "closed"),
    queryFn: ({ signal }) => getInboxItem(id!, signal),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useInboxItemEventsQuery(
  id: string | null,
  input: Omit<InboxEventListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...inboxEventQueryKey(id ?? "closed"), "timeline", query],
    queryFn: ({ pageParam, signal }) =>
      getInboxItemEvents(id!, { ...query, page: pageParam }, signal),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(id) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useInboxItemTasksQuery(
  id: string | null,
  input: Omit<InboxItemTaskListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...inboxTaskRelationQueryKey(id ?? "closed"), "history", query],
    queryFn: ({ pageParam, signal }) =>
      getInboxItemTasks(id!, { ...query, page: pageParam }, signal),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(id) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

function inboxFactsNeedRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "VERSION_CONFLICT")
  );
}

function inboxConflictOwnsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 409 || error.code === "VERSION_CONFLICT")
  );
}

export function useCreateInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateInboxItemInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createInboxItem(input, attempt.current.key);
    },
    onSuccess: async (item) => {
      attempt.current = null;
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id, item);
    },
  });
}

export function useUpdateInboxItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateInboxItemInput }) =>
      updateInboxItem(id, input),
    onSuccess: async (item) => {
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id, item);
    },
    onError: async (error, variables) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, variables.id, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? variables.id
            : undefined,
        });
      }
    },
  });
}

export function useInboxItemCommand() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: InboxItemCommandInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return executeInboxItemCommand(input, attempt.current.key);
    },
    onSuccess: async (item) => {
      attempt.current = null;
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id, item);
    },
    onError: async (error, variables) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, variables.id, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? variables.id
            : undefined,
        });
      }
    },
  });
}

export function useMarkAllInboxItemsRead() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: MarkAllInboxReadInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return markAllInboxItemsRead(input.throughCreatedAt, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await invalidateInboxFacts(queryClient);
    },
  });
}

function useStableInboxTaskMutationKey<TInput>() {
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return {
    keyFor(input: TInput): string {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return attempt.current.key;
    },
    reset(): void {
      attempt.current = null;
    },
  };
}

async function applyInboxTaskMutationResult(
  queryClient: QueryClient,
  result: Awaited<ReturnType<typeof linkInboxItemTask>>,
): Promise<void> {
  queryClient.setQueryData(
    inboxDetailQueryKey(result.inboxItem.id),
    result.inboxItem,
  );
  await invalidateInboxFacts(
    queryClient,
    result.inboxItem.id,
    result.inboxItem,
  );
}

export function useLinkInboxItemTask() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<LinkInboxItemTaskInput>();
  return useMutation({
    mutationFn: (input: LinkInboxItemTaskInput) =>
      linkInboxItemTask(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
          preserveActiveTaskRelationsItemId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
        });
      }
    },
  });
}

export function useUpdateInboxItemTaskRequirement() {
  const queryClient = useQueryClient();
  const attempt =
    useStableInboxTaskMutationKey<UpdateInboxItemTaskRequirementInput>();
  return useMutation({
    mutationFn: (input: UpdateInboxItemTaskRequirementInput) =>
      updateInboxItemTaskRequirement(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
          preserveActiveTaskRelationsItemId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
        });
      }
    },
  });
}

export function useUnlinkInboxItemTask() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<UnlinkInboxItemTaskInput>();
  return useMutation({
    mutationFn: (input: UnlinkInboxItemTaskInput) =>
      unlinkInboxItemTask(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      await applyInboxTaskMutationResult(queryClient, result);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
          preserveActiveTaskRelationsItemId: inboxConflictOwnsRefresh(error)
            ? input.inboxItemId
            : undefined,
        });
      }
    },
  });
}

export function useSplitInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<SplitInboxItemInput>();
  return useMutation({
    mutationFn: (input: SplitInboxItemInput) =>
      splitInboxItem(input, attempt.keyFor(input)),
    onSuccess: async (result) => {
      attempt.reset();
      queryClient.setQueryData(
        inboxDetailQueryKey(result.inboxItem.id),
        result.inboxItem,
      );
      await Promise.all([
        invalidateInboxFacts(
          queryClient,
          result.inboxItem.id,
          result.inboxItem,
          { projectsMayChange: true },
        ),
        queryClient.invalidateQueries({ queryKey: taskQueryKey }),
        queryClient.invalidateQueries({ queryKey: ["stats", "today"] }),
      ]);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.inboxItemId);
      }
    },
  });
}

export function useForceResolveInboxItem() {
  const queryClient = useQueryClient();
  const attempt = useStableInboxTaskMutationKey<ForceResolveInboxItemInput>();
  return useMutation({
    mutationFn: (input: ForceResolveInboxItemInput) =>
      forceResolveInboxItem(input, attempt.keyFor(input)),
    onSuccess: async (item) => {
      attempt.reset();
      queryClient.setQueryData(inboxDetailQueryKey(item.id), item);
      await invalidateInboxFacts(queryClient, item.id, item);
    },
    onError: async (error, input) => {
      if (inboxFactsNeedRefresh(error)) {
        await invalidateInboxFacts(queryClient, input.id, undefined, {
          preserveActiveDetailId: inboxConflictOwnsRefresh(error)
            ? input.id
            : undefined,
          preserveActiveTaskRelationsItemId: inboxConflictOwnsRefresh(error)
            ? input.id
            : undefined,
        });
      }
    },
  });
}
