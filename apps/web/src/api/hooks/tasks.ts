import type {
  BatchUpdateTasksInput,
  NewTaskInput,
  Task,
  TaskLifecycleCommandInput,
  TaskListParams,
  UpdateTaskInput,
} from "../../types/models";
import {
  ApiError,
  batchUpdateTasks,
  createTask,
  deleteTask,
  executeTaskLifecycleCommand,
  getAllTasks,
  getTask,
  getTaskPage,
  getTasks,
  updateTask,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { localDayKey, taskDueDayKey } from "../../lib/taskDue";
import {
  invalidateFocusReadModels,
  invalidateTaskAggregates,
  invalidateTaskFacts,
} from "./invalidation";
import {
  agentRunQueryRootKey,
  taskAgentRunsQueryKey,
  taskArtifactQueryKey,
  taskAssignmentQueryKey,
  taskDetailQueryKey,
  taskEventQueryKey,
  taskPageQueryKey,
  taskQueryKey,
  taskSubmissionQueryKey,
} from "./queryKeys";

export function useTasksQuery(
  options: { projectId?: string; loadAll?: boolean } = {},
) {
  return useQuery({
    queryKey: [...taskQueryKey, "list", options],
    queryFn: ({ signal }) =>
      options.loadAll
        ? getAllTasks({ projectId: options.projectId }, signal)
        : getTasks({ projectId: options.projectId }, signal),
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export interface SidebarWeekTasks {
  plannedFrom: string;
  plannedTo: string;
  taskCount: number;
  completedCount: number;
  completedPercent: number;
}

export function useSidebarWeekTasksQuery({
  plannedFrom,
  plannedTo,
}: {
  plannedFrom: string;
  plannedTo: string;
}) {
  return useQuery({
    queryKey: [...taskQueryKey, "sidebar-week", plannedFrom, plannedTo],
    queryFn: async ({ signal }): Promise<SidebarWeekTasks> => {
      const tasks = await getAllTasks({ plannedFrom, plannedTo }, signal);
      const includedTasks = tasks.filter((task) => task.status !== "cancelled");
      const completedCount = includedTasks.filter(
        (task) => task.status === "done",
      ).length;
      const taskCount = includedTasks.length;
      return {
        plannedFrom,
        plannedTo,
        taskCount,
        completedCount,
        completedPercent: taskCount
          ? Math.round((completedCount / taskCount) * 100)
          : 0,
      };
    },
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

function shiftDateKey(dateKey: string, days: number): string {
  const [year, month, day] = dateKey.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return date.toISOString().slice(0, 10);
}

function endOfWeekKey(dateKey: string): string {
  const [year, month, day] = dateKey.split("-").map(Number);
  const weekday = new Date(Date.UTC(year, month - 1, day)).getUTCDay();
  return shiftDateKey(dateKey, weekday === 0 ? 0 : 7 - weekday);
}

export interface TodayTaskGroups {
  overdue: Task[];
  today: Task[];
  thisWeek: Task[];
  unscheduled: Task[];
}

export function useTodayTaskGroupsQuery(dateKey: string) {
  return useQuery({
    queryKey: [...taskQueryKey, "today-groups", dateKey],
    queryFn: async ({ signal }): Promise<TodayTaskGroups> => {
      const tomorrow = shiftDateKey(dateKey, 1);
      const endOfWeek = endOfWeekKey(dateKey);
      const [overdue, today, thisWeek, unscheduled] = await Promise.all([
        getAllTasks(
          { status: "active", plannedTo: shiftDateKey(dateKey, -1) },
          signal,
        ),
        getAllTasks({ status: "active", plannedDate: dateKey }, signal),
        tomorrow <= endOfWeek
          ? getAllTasks(
              {
                status: "active",
                plannedFrom: tomorrow,
                plannedTo: endOfWeek,
              },
              signal,
            )
          : Promise.resolve([]),
        getAllTasks({ status: "active", plannedState: "unscheduled" }, signal),
      ]);
      const taskIds = new Set<string>();
      for (const task of [...overdue, ...today, ...thisWeek, ...unscheduled]) {
        if (taskIds.has(task.id)) {
          throw new ApiError("今日任务分组响应包含重复任务", {
            code: "INVALID_RESPONSE",
          });
        }
        taskIds.add(task.id);
      }
      return { overdue, today, thisWeek, unscheduled };
    },
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export interface TaskDueDayGroup {
  dayKey: string;
  overdue: boolean;
  tasks: Task[];
}

export type TaskDueAgendaParams = Omit<
  TaskListParams,
  "page" | "pageSize" | "sort" | "dueState"
>;

// Open-ended due range keeps undated tasks out of the agenda without a
// dedicated server filter; substr(due_date,1,10) comparisons drop NULL rows.
const AGENDA_DUE_FROM = "1970-01-01";
const AGENDA_DUE_TO = "9999-12-31";

export function useTaskDueAgendaQuery(
  params: TaskDueAgendaParams,
  enabled = true,
) {
  return useQuery({
    queryKey: [...taskQueryKey, "due-agenda", params],
    queryFn: async ({ signal }): Promise<TaskDueDayGroup[]> => {
      const tasks = await getAllTasks(
        {
          ...params,
          status: params.status || "active",
          dueFrom: params.dueFrom || AGENDA_DUE_FROM,
          dueTo: params.dueTo || AGENDA_DUE_TO,
          sort: "due_date",
        },
        signal,
      );
      const todayKey = localDayKey(Date.now());
      const groups = new Map<string, Task[]>();
      for (const task of tasks) {
        const dayKey = taskDueDayKey(task);
        if (!dayKey) continue;
        const bucket = groups.get(dayKey);
        if (bucket) bucket.push(task);
        else groups.set(dayKey, [task]);
      }
      return [...groups.entries()]
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([dayKey, dayTasks]) => ({
          dayKey,
          overdue: dayKey < todayKey,
          tasks: dayTasks,
        }));
    },
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useTaskPageQuery(input: TaskListParams = {}, enabled = true) {
  return useQuery({
    queryKey: taskPageQueryKey(input),
    queryFn: ({ signal }) => getTaskPage(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    refetchInterval: input.dueState ? 60_000 : false,
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

export function setTaskDetailIfNotOlder(
  queryClient: QueryClient,
  task: Task,
): void {
  queryClient.setQueryData<Task>(taskDetailQueryKey(task.id), (current) =>
    current && current.version > task.version ? current : task,
  );
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
      await invalidateTaskAggregates(queryClient);
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

export function useTaskLifecycleCommand() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: TaskLifecycleCommandInput;
    }) => {
      const fingerprint = JSON.stringify({ id, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return executeTaskLifecycleCommand(id, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAggregates(queryClient);
    },
    onError: async (error, variables) => {
      if (
        error instanceof ApiError &&
        (error.status === 404 || error.status === 409)
      ) {
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: variables.id,
        });
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
      setTaskDetailIfNotOlder(queryClient, task);
      await invalidateTaskAggregates(queryClient);
      await invalidateFocusReadModels(queryClient, {
        history: true,
        report: true,
      });
    },
    onError: async (error, variables) => {
      if (isTaskFactsStale(error)) {
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: variables.id,
        });
        await invalidateFocusReadModels(queryClient, {
          history: true,
          report: true,
        });
      }
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
      await Promise.all([
        queryClient.cancelQueries({ queryKey: taskAgentRunsQueryKey(id) }),
        queryClient.cancelQueries({
          queryKey: agentRunQueryRootKey,
          predicate: (query) =>
            query.queryKey[1] === "detail" && query.queryKey[3] === id,
        }),
      ]);
      queryClient.removeQueries({ queryKey: taskDetailQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskAssignmentQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskEventQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskSubmissionQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskArtifactQueryKey(id) });
      queryClient.removeQueries({ queryKey: taskAgentRunsQueryKey(id) });
      queryClient.removeQueries({
        queryKey: agentRunQueryRootKey,
        predicate: (query) =>
          query.queryKey[1] === "detail" && query.queryKey[3] === id,
      });
      await Promise.all([
        invalidateTaskAggregates(queryClient),
        queryClient.invalidateQueries({ queryKey: ["ai", "actions"] }),
        queryClient.invalidateQueries({
          queryKey: agentRunQueryRootKey,
          predicate: (query) => query.queryKey[1] === "list",
        }),
      ]);
      await invalidateFocusReadModels(queryClient, {
        history: true,
        report: true,
      });
    },
    onError: async (error, variables) => {
      if (isTaskFactsStale(error)) {
        const id = typeof variables === "string" ? variables : variables.id;
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: id,
        });
        await invalidateFocusReadModels(queryClient, {
          history: true,
          report: true,
        });
      }
    },
  });
}

export function isTaskFactsStale(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "VERSION_CONFLICT" ||
      error.code === "TASK_BATCH_SET_CHANGED" ||
      error.code === "TASK_REORDER_SET_CHANGED")
  );
}

function batchCanReconcileTaskHierarchy(
  action: BatchUpdateTasksInput["action"],
): boolean {
  return (
    action !== "set_priority" &&
    action !== "set_due_date" &&
    action !== "set_project" &&
    action !== "set_planned_date" &&
    action !== "add_tags" &&
    action !== "remove_tags"
  );
}

export function useBatchUpdateTasks() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: BatchUpdateTasksInput) => batchUpdateTasks(input),
    onSuccess: async (_, input) => {
      await (batchCanReconcileTaskHierarchy(input.action)
        ? invalidateTaskAggregates(queryClient)
        : invalidateTaskFacts(queryClient));
      if (input.action === "set_project") {
        await invalidateFocusReadModels(queryClient, {
          history: true,
          report: true,
        });
      } else if (
        input.action === "add_tags" ||
        input.action === "remove_tags"
      ) {
        await invalidateFocusReadModels(queryClient, { report: true });
      }
    },
    onError: async (error, input) => {
      if (isTaskFactsStale(error)) {
        await (batchCanReconcileTaskHierarchy(input.action)
          ? invalidateTaskAggregates(queryClient)
          : invalidateTaskFacts(queryClient));
        if (input.action === "set_project") {
          await invalidateFocusReadModels(queryClient, {
            history: true,
            report: true,
          });
        } else if (
          input.action === "add_tags" ||
          input.action === "remove_tags"
        ) {
          await invalidateFocusReadModels(queryClient, { report: true });
        }
      }
    },
  });
}
