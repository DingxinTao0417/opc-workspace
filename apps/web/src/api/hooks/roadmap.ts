import type {
  CreateRoadmapMilestoneInput,
  ReorderRoadmapMilestonesInput,
  RoadmapMilestoneListParams,
  UpdateRoadmapMilestoneInput,
} from "../../types/models";
import {
  ApiError,
  archiveRoadmapMilestone,
  createRoadmapMilestone,
  deleteRoadmapMilestone,
  getRoadmapMilestone,
  getRoadmapMilestones,
  reorderRoadmapMilestones,
  restoreRoadmapMilestone,
  updateRoadmapMilestone,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { invalidateRoadmapReadModels } from "./invalidation";
import {
  roadmapMilestoneDetailQueryKey,
  roadmapMilestoneQueryKey,
} from "./queryKeys";

export function useRoadmapMilestonesQuery(
  input: RoadmapMilestoneListParams = {},
) {
  return useQuery({
    queryKey: [...roadmapMilestoneQueryKey, "list", input],
    queryFn: ({ signal }) => getRoadmapMilestones(input, signal),
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useRoadmapMilestoneQuery(id: string | null) {
  return useQuery({
    queryKey: roadmapMilestoneDetailQueryKey(id ?? "idle"),
    queryFn: ({ signal }) => getRoadmapMilestone(id!, signal),
    enabled: Boolean(id),
    retry: 2,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

function isRoadmapFactsStale(error: unknown): boolean {
  return error instanceof ApiError && error.code === "VERSION_CONFLICT";
}

export function useCreateRoadmapMilestone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateRoadmapMilestoneInput) =>
      createRoadmapMilestone(input),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient, true);
    },
  });
}

export function useUpdateRoadmapMilestone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateRoadmapMilestoneInput;
    }) => updateRoadmapMilestone(id, input),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient, true);
    },
    onError: async (error) => {
      if (isRoadmapFactsStale(error)) {
        await invalidateRoadmapReadModels(queryClient, true);
      }
    },
  });
}

export function useReorderRoadmapMilestones() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ReorderRoadmapMilestonesInput) =>
      reorderRoadmapMilestones(input),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient);
    },
    onError: async () => {
      await invalidateRoadmapReadModels(queryClient);
    },
  });
}

export function useArchiveRoadmapMilestone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => archiveRoadmapMilestone(id, expectedVersion),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient, true);
    },
    onError: async (error) => {
      if (isRoadmapFactsStale(error)) {
        await invalidateRoadmapReadModels(queryClient, true);
      }
    },
  });
}

export function useRestoreRoadmapMilestone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => restoreRoadmapMilestone(id, expectedVersion),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient, true);
    },
    onError: async (error) => {
      if (isRoadmapFactsStale(error)) {
        await invalidateRoadmapReadModels(queryClient, true);
      }
    },
  });
}

export function useDeleteRoadmapMilestone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteRoadmapMilestone(id, expectedVersion),
    onSuccess: async () => {
      await invalidateRoadmapReadModels(queryClient, true);
    },
    onError: async (error) => {
      if (isRoadmapFactsStale(error)) {
        await invalidateRoadmapReadModels(queryClient, true);
      }
    },
  });
}
