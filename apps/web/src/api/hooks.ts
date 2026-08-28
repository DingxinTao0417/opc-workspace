import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import {
  createTask,
  createProject,
  deleteProject,
  deleteTask,
  getAllProjects,
  getAllTasks,
  getHealth,
  getTask,
  getTasks,
  getTodayStats,
  getProject,
  getProjects,
  resetRuntimeConnection,
  updateTask,
  updateTaskStatus,
  transitionProject,
  updateProject,
} from "./client";
import type {
  NewTaskInput,
  ProjectInput,
  ProjectListParams,
  ProjectTransitionAction,
  TaskStatus,
  UpdateProjectInput,
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

export function useTasksQuery(
  options: { projectId?: string; loadAll?: boolean } = {},
) {
  return useQuery({
    queryKey: [...taskQueryKey, "list", options],
    queryFn: () =>
      options.loadAll
        ? getAllTasks({ projectId: options.projectId })
        : getTasks({ projectId: options.projectId }),
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
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: NewTaskInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTask(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
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
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
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
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
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
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

export const projectQueryKey = ["projects"] as const;
export const projectDetailQueryKey = (id: string) =>
  [...projectQueryKey, "detail", id] as const;

export function useProjectsQuery(
  input: ProjectListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...projectQueryKey, "list", input],
    queryFn: () => getProjects(input),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...projectQueryKey, "options"],
    queryFn: () => getAllProjects({ sort: "name" }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useProjectQuery(id: string | null) {
  return useQuery({
    queryKey: projectDetailQueryKey(id ?? "missing"),
    queryFn: () => getProject(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useCreateProject() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: ProjectInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createProject(input, attempt.current.key);
    },
    onSuccess: async (project) => {
      attempt.current = null;
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
  });
}

export function useUpdateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateProjectInput }) =>
      updateProject(id, input),
    onSuccess: async (project) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
    },
  });
}

export function useTransitionProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      action,
      expectedVersion,
      confirmIncompleteTasks,
    }: {
      id: string;
      action: ProjectTransitionAction;
      expectedVersion: number;
      confirmIncompleteTasks?: boolean;
    }) =>
      transitionProject(id, action, expectedVersion, confirmIncompleteTasks),
    onSuccess: async (project) => {
      queryClient.setQueryData(projectDetailQueryKey(project.id), project);
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
    },
  });
}

export function useDeleteProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteProject(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: projectDetailQueryKey(variables.id),
      });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
    },
  });
}

export function resetApiAndRefetch(refetch: () => Promise<unknown>): void {
  resetRuntimeConnection();
  void refetch();
}
