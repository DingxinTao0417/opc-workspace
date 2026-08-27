import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createTask,
  getHealth,
  getTasks,
  getTodayStats,
  resetRuntimeConnection,
  updateTaskStatus,
} from "./client";
import type { NewTaskInput, TaskStatus } from "../types/models";

export const taskQueryKey = ["tasks"] as const;

export function useHealthQuery() {
  return useQuery({
    queryKey: ["health"],
    queryFn: getHealth,
    retry: false,
    refetchInterval: 15_000,
  });
}

export function useTasksQuery() {
  return useQuery({
    queryKey: taskQueryKey,
    queryFn: getTasks,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useTodayStatsQuery(date: string) {
  return useQuery({
    queryKey: ["stats", "today", date],
    queryFn: () => getTodayStats(date),
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: NewTaskInput) => createTask(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

export function useUpdateTaskStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: TaskStatus }) =>
      updateTaskStatus(id, status),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

export function resetApiAndRefetch(refetch: () => Promise<unknown>): void {
  resetRuntimeConnection();
  void refetch();
}
