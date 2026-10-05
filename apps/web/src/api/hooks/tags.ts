import type {
  TagInput,
  TagListParams,
  UpdateTagInput,
} from "../../types/models";
import {
  ApiError,
  createTag,
  deleteTag,
  getAllTags,
  getTags,
  updateTag,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateFocusReadModels } from "./invalidation";
import { tagQueryKey, taskQueryKey } from "./queryKeys";

export function useTagsQuery(input: TagListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...tagQueryKey, "list", input],
    queryFn: () => getTags(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useTagOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...tagQueryKey, "options"],
    queryFn: () => getAllTags({ sort: "name" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTag() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: TagInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTag(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
    },
  });
}

export function useUpdateTag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateTagInput }) =>
      updateTag(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await invalidateFocusReadModels(queryClient, { report: true });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
        await invalidateFocusReadModels(queryClient, { report: true });
      }
    },
  });
}

export function useDeleteTag() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteTag(id, expectedVersion),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: tagQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await invalidateFocusReadModels(queryClient, { report: true });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
        await invalidateFocusReadModels(queryClient, { report: true });
      }
    },
  });
}
