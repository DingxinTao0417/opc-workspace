import type {
  CreateTaskSavedViewInput,
  UpdateTaskSavedViewInput,
} from "../../types/models";
import {
  ApiError,
  createTaskSavedView,
  deleteTaskSavedView,
  getTaskSavedViews,
  updateTaskSavedView,
} from "../client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { taskSavedViewQueryKey } from "./queryKeys";

export function useTaskSavedViewsQuery() {
  return useQuery({
    queryKey: taskSavedViewQueryKey,
    queryFn: getTaskSavedViews,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTaskSavedViewInput) => createTaskSavedView(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
  });
}

export function useUpdateTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateTaskSavedViewInput;
    }) => updateTaskSavedView(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({
          queryKey: taskSavedViewQueryKey,
        });
      }
    },
  });
}

export function useDeleteTaskSavedView() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteTaskSavedView(id, expectedVersion),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskSavedViewQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({
          queryKey: taskSavedViewQueryKey,
        });
      }
    },
  });
}
