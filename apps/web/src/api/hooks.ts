import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import {
  ApiError,
  batchUpdateTasks,
  createTag,
  createTask,
  createProject,
  deleteProject,
  deleteTag,
  deleteTask,
  getAllProjects,
  getAllTags,
  getAllTasks,
  getHealth,
  getTags,
  getTask,
  getTaskPage,
  getTasks,
  getTodayStats,
  getProject,
  getProjects,
  resetRuntimeConnection,
  reorderTasks,
  updateTag,
  updateTask,
  updateTaskStatus,
  transitionProject,
  updateProject,
} from "./client";
import type {
  BatchUpdateTasksInput,
  NewTaskInput,
  ProjectInput,
  ProjectListParams,
  ProjectTransitionAction,
  ReorderTasksInput,
  TagInput,
  TagListParams,
  Task,
  TaskListParams,
  TaskStatus,
  UpdateTagInput,
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

export const taskPageQueryKey = (input: TaskListParams) =>
  [...taskQueryKey, "page", input] as const;

export function useTaskPageQuery(input: TaskListParams = {}, enabled = true) {
  return useQuery({
    queryKey: taskPageQueryKey(input),
    queryFn: () => getTaskPage(input),
    enabled,
    placeholderData: keepPreviousData,
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

export function useTaskOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...taskQueryKey, "options"],
    queryFn: () => getAllTasks({ sort: "title" }),
    enabled,
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

function taskFromCachedValue(value: unknown, id: string): Task | undefined {
  if (Array.isArray(value)) {
    return value.find(
      (item): item is Task =>
        typeof item === "object" &&
        item !== null &&
        "id" in item &&
        item.id === id,
    );
  }
  if (typeof value !== "object" || value === null) return undefined;
  if ("id" in value && value.id === id && "version" in value) {
    return value as Task;
  }
  if ("items" in value && Array.isArray(value.items)) {
    return taskFromCachedValue(value.items, id);
  }
  return undefined;
}

function resolveTaskVersion(
  queryClient: QueryClient,
  id: string,
  explicitVersion?: number,
): number {
  if (
    typeof explicitVersion === "number" &&
    Number.isInteger(explicitVersion) &&
    explicitVersion >= 1
  ) {
    return explicitVersion;
  }
  const detail = queryClient.getQueryData<Task>(taskDetailQueryKey(id));
  if (detail && detail.version >= 1) return detail.version;
  for (const [, cached] of queryClient.getQueriesData({
    queryKey: taskQueryKey,
  })) {
    const task = taskFromCachedValue(cached, id);
    if (task && task.version >= 1) return task.version;
  }
  throw new ApiError("任务版本缺失，请刷新后重试", {
    code: "EXPECTED_VERSION_REQUIRED",
  });
}

export function useUpdateTaskStatus() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      status,
      expectedVersion,
    }: {
      id: string;
      status: TaskStatus;
      expectedVersion?: number;
    }) =>
      updateTaskStatus(
        id,
        status,
        resolveTaskVersion(queryClient, id, expectedVersion),
      ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      }
    },
  });
}

export function useUpdateTask() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateTaskInput }) =>
      updateTask(id, {
        ...input,
        expectedVersion: resolveTaskVersion(
          queryClient,
          id,
          input.expectedVersion,
        ),
      }),
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
    mutationFn: (
      variables: string | { id: string; expectedVersion?: number },
    ) => {
      const id = typeof variables === "string" ? variables : variables.id;
      const explicitVersion =
        typeof variables === "string" ? undefined : variables.expectedVersion;
      return deleteTask(
        id,
        resolveTaskVersion(queryClient, id, explicitVersion),
      );
    },
    onSuccess: async (_, variables) => {
      const id = typeof variables === "string" ? variables : variables.id;
      queryClient.removeQueries({ queryKey: taskDetailQueryKey(id) });
      await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
    },
  });
}

async function invalidateTaskFacts(queryClient: QueryClient): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: taskQueryKey });
  await queryClient.invalidateQueries({ queryKey: projectQueryKey });
  await queryClient.invalidateQueries({ queryKey: ["stats", "today"] });
}

function isTaskFactsStale(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "VERSION_CONFLICT" ||
      error.code === "TASK_BATCH_SET_CHANGED" ||
      error.code === "TASK_REORDER_SET_CHANGED")
  );
}

export function useBatchUpdateTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BatchUpdateTasksInput) => batchUpdateTasks(input),
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useReorderTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: ReorderTasksInput) => reorderTasks(input),
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useMoveTaskWithinPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      taskId,
      plannedDate,
      direction,
    }: {
      taskId: string;
      plannedDate: string | null;
      direction: "up" | "down";
    }) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法手动排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      const currentIndex = scoped.findIndex((task) => task.id === taskId);
      if (currentIndex < 0) {
        throw new ApiError("任务已不在当前计划日期，请刷新后重试", {
          code: "TASK_REORDER_SET_CHANGED",
        });
      }
      const step = direction === "up" ? -1 : 1;
      let targetIndex = currentIndex + step;
      while (
        targetIndex >= 0 &&
        targetIndex < scoped.length &&
        scoped[targetIndex].status !== scoped[currentIndex].status
      ) {
        targetIndex += step;
      }
      if (targetIndex < 0 || targetIndex >= scoped.length) {
        return {
          plannedDate,
          mode: "manual" as const,
          changed: 0,
          tasks: scoped,
        };
      }
      const ordered = [...scoped];
      [ordered[currentIndex], ordered[targetIndex]] = [
        ordered[targetIndex],
        ordered[currentIndex],
      ];
      return reorderTasks({
        plannedDate,
        mode: "manual",
        items: ordered.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export function useResetTaskOrder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (plannedDate: string | null) => {
      const tasks = await getAllTasks({
        plannedDate: plannedDate ?? undefined,
        sort: "manual_order",
      });
      const scoped = plannedDate
        ? tasks
        : tasks.filter((task) => task.plannedDate === null);
      if (scoped.length > 1_000) {
        throw new ApiError("同一计划日期超过 1000 项，暂时无法重置排序", {
          code: "TASK_REORDER_LIMIT",
        });
      }
      return reorderTasks({
        plannedDate,
        mode: "default",
        items: scoped.map((task) => ({
          id: task.id,
          expectedVersion: task.version,
        })),
      });
    },
    onSuccess: async () => invalidateTaskFacts(queryClient),
    onError: async (error) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskFacts(queryClient);
      }
    },
  });
}

export const tagQueryKey = ["tags"] as const;

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
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
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
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: tagQueryKey });
        await queryClient.invalidateQueries({ queryKey: taskQueryKey });
      }
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
