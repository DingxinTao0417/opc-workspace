import type {
  ContentItem,
  ContentItemListParams,
  CreateContentItemInput,
  PublishContentItemInput,
  ScheduleContentItemInput,
  UpdateContentItemInput,
} from "../../types/models";
import {
  ApiError,
  createContentItem,
  deleteContentItem,
  getContentItem,
  getContentItems,
  linkContentItemTask,
  publishContentItem,
  scheduleContentItem,
  unlinkContentItemTask,
  updateContentItem,
} from "../client";
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { invalidateUnifiedSearch } from "./invalidation";
import {
  contentItemDetailQueryKey,
  contentItemListQueryKey,
  contentItemQueryKey,
  inboxQueryKey,
} from "./queryKeys";

export function useContentItemsInfiniteQuery(
  input: Omit<ContentItemListParams, "page"> = {},
) {
  const query = { ...input, pageSize: input.pageSize ?? 100 };
  return useInfiniteQuery({
    queryKey: [...contentItemListQueryKey, query],
    queryFn: ({ pageParam, signal }) =>
      getContentItems({ ...query, page: pageParam }, signal),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useContentItemQuery(id: string | null) {
  return useQuery({
    queryKey: contentItemDetailQueryKey(id ?? "idle"),
    queryFn: ({ signal }) => getContentItem(id!, signal),
    enabled: Boolean(id),
    retry: 2,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

export function useCreateContentItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateContentItemInput) => createContentItem(input),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: contentItemQueryKey }),
        invalidateUnifiedSearch(queryClient),
      ]);
    },
  });
}

function useContentItemMutation<T>(
  mutationFn: (input: T) => Promise<ContentItem>,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: contentItemQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
        invalidateUnifiedSearch(queryClient),
      ]);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: contentItemQueryKey }),
          queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
          invalidateUnifiedSearch(queryClient),
        ]);
      }
    },
  });
}

export function useUpdateContentItem() {
  return useContentItemMutation(
    ({ id, input }: { id: string; input: UpdateContentItemInput }) =>
      updateContentItem(id, input),
  );
}

export function useScheduleContentItem() {
  return useContentItemMutation(
    ({ id, input }: { id: string; input: ScheduleContentItemInput }) =>
      scheduleContentItem(id, input),
  );
}

export function usePublishContentItem() {
  return useContentItemMutation(
    ({ id, input }: { id: string; input: PublishContentItemInput }) =>
      publishContentItem(id, input),
  );
}

export function useDeleteContentItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteContentItem(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: contentItemDetailQueryKey(variables.id),
      });
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: contentItemListQueryKey,
        }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
        invalidateUnifiedSearch(queryClient),
      ]);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: contentItemListQueryKey }),
          queryClient.invalidateQueries({
            queryKey: contentItemDetailQueryKey(variables.id),
          }),
          queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
          invalidateUnifiedSearch(queryClient),
        ]);
      } else if (
        error instanceof ApiError &&
        error.code === "CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES"
      ) {
        await queryClient.invalidateQueries({ queryKey: inboxQueryKey });
      }
    },
  });
}

export function useLinkContentItemTask() {
  return useContentItemMutation(
    ({
      id,
      taskId,
      isRequired,
      expectedVersion,
    }: {
      id: string;
      taskId: string;
      isRequired: boolean;
      expectedVersion: number;
    }) => linkContentItemTask(id, taskId, isRequired, expectedVersion),
  );
}

export function useUnlinkContentItemTask() {
  return useContentItemMutation(
    ({
      id,
      taskId,
      expectedVersion,
    }: {
      id: string;
      taskId: string;
      expectedVersion: number;
    }) => unlinkContentItemTask(id, taskId, expectedVersion),
  );
}
