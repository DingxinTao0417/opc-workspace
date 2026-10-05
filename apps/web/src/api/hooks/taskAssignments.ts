import type {
  CreateTaskAssignmentInput,
  EndTaskAssignmentInput,
  ReassignTaskAssignmentInput,
  TaskAssignmentListParams,
} from "../../types/models";
import {
  ApiError,
  createTaskAssignment,
  endTaskAssignment,
  getTaskAssignments,
  reassignTaskAssignment,
} from "../client";
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateTaskAssignments } from "./invalidation";
import { taskAssignmentQueryKey } from "./queryKeys";
import { setTaskDetailIfNotOlder } from "./tasks";

export function useTaskAssignmentsQuery(
  taskId: string | null,
  input: Omit<TaskAssignmentListParams, "page"> = {},
  enabled = true,
) {
  const query = {
    pageSize: input.pageSize ?? 20,
    role: input.role,
    sort: input.sort?.trim() || "-assigned_at",
  };
  return useInfiniteQuery({
    queryKey: [...taskAssignmentQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskAssignments(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    placeholderData: keepPreviousData,
    retry: 1,
    staleTime: 10_000,
  });
}

function assignmentErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "ASSIGNMENT_ACTOR_NOT_ACTIVE")
  );
}

async function refreshTaskAssignmentsAfterError(
  queryClient: QueryClient,
  error: unknown,
  taskId: string,
): Promise<void> {
  if (!assignmentErrorNeedsRefresh(error)) return;
  await invalidateTaskAssignments(
    queryClient,
    error instanceof ApiError && error.code === "VERSION_CONFLICT"
      ? taskId
      : undefined,
  );
}

export function useCreateTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: CreateTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createTaskAssignment(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient);
    },
    onError: (error, variables) =>
      refreshTaskAssignmentsAfterError(queryClient, error, variables.taskId),
  });
}

export function useReassignTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: ReassignTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return reassignTaskAssignment(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient);
    },
    onError: (error, variables) =>
      refreshTaskAssignmentsAfterError(queryClient, error, variables.taskId),
  });
}

export function useEndTaskAssignment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId: _taskId,
      assignmentId,
      input,
    }: {
      taskId: string;
      assignmentId: string;
      input: EndTaskAssignmentInput;
    }) => {
      const fingerprint = JSON.stringify({ assignmentId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return endTaskAssignment(assignmentId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAssignments(queryClient);
    },
    onError: (error, variables) =>
      refreshTaskAssignmentsAfterError(queryClient, error, variables.taskId),
  });
}
