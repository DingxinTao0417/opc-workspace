import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createTask,
  deleteTask,
  getHealth,
  getTask,
  getTasks,
  getTodayStats,
  resetRuntimeConnection,
  updateTask,
  updateTaskStatus,
} from "./client";
import type {
  NewTaskInput,
  TaskStatus,
  UpdateTaskInput,
} from "../types/models";

export const taskQueryKey = ["tasks"] as const;

export const taskDetailQueryKey = (id: string) => ["tasks", id] as const;

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

export function useTaskQuery(id: string | null) {
  return useQuery({
    queryKey: taskDetailQueryKey(id ?? "closed"),
    queryFn: () => getTask(id!),
    enabled: Boolean(id),
    retry: 1,
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

export function useUpdateTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateTaskInput }) =>
      updateTask(id, input),
    onSuccess: async (task) => {
      queryClient.setQueryData(taskDetailQueryKey(task.id), task);
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

export function useDeleteTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteTask(id),
    onSuccess: async (_, id) => {
      queryClient.removeQueries({ queryKey: taskDetailQueryKey(id) });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

export function resetApiAndRefetch(refetch: () => Promise<unknown>): void {
  resetRuntimeConnection();
  void refetch();
}
