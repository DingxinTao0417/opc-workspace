import type {
  DeleteTaskArtifactInput,
  ReviewTaskSubmissionInput,
  SubmitTaskOutputInput,
  TaskArtifactListParams,
  TaskEventListParams,
  TaskSubmissionListParams,
} from "../../types/models";
import {
  ApiError,
  deleteTaskArtifact,
  downloadTaskArtifact,
  getTaskArtifact,
  getTaskArtifacts,
  getTaskEvents,
  getTaskSubmissions,
  reviewTaskSubmission,
  submitTaskOutput,
} from "../client";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateAiPlanReviewProjections,
  invalidateTaskAggregates,
} from "./invalidation";
import {
  taskArtifactDetailQueryKey,
  taskArtifactQueryKey,
  taskEventQueryKey,
  taskSubmissionQueryKey,
} from "./queryKeys";
import { setTaskDetailIfNotOlder } from "./tasks";

export function useTaskEventsQuery(
  taskId: string | null,
  input: Omit<TaskEventListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 20 };
  return useInfiniteQuery({
    queryKey: [...taskEventQueryKey(taskId ?? "closed"), "timeline", query],
    queryFn: ({ pageParam }) =>
      getTaskEvents(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskSubmissionsQuery(
  taskId: string | null,
  input: Omit<TaskSubmissionListParams, "page"> = {},
  enabled = true,
) {
  const query = { pageSize: input.pageSize ?? 10 };
  return useInfiniteQuery({
    queryKey: [...taskSubmissionQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskSubmissions(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskArtifactsQuery(
  taskId: string | null,
  input: Omit<TaskArtifactListParams, "page"> = {},
  enabled = true,
) {
  const query = {
    pageSize: input.pageSize ?? 20,
    submissionId: input.submissionId,
    includeDeleted: input.includeDeleted ?? true,
  };
  return useInfiniteQuery({
    queryKey: [...taskArtifactQueryKey(taskId ?? "closed"), "history", query],
    queryFn: ({ pageParam }) =>
      getTaskArtifacts(taskId!, { ...query, page: pageParam }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.meta.page * lastPage.meta.pageSize < lastPage.meta.total
        ? lastPage.meta.page + 1
        : undefined,
    enabled: Boolean(taskId) && enabled,
    retry: 1,
    staleTime: 10_000,
  });
}

export function useTaskArtifactQuery(
  artifactId: string | null,
  enabled = true,
  fresh = false,
) {
  return useQuery({
    queryKey: taskArtifactDetailQueryKey(artifactId ?? "closed"),
    queryFn: ({ signal }) => getTaskArtifact(artifactId!, signal),
    enabled: Boolean(artifactId) && enabled,
    retry: 1,
    ...(fresh ? { staleTime: 0, refetchOnMount: "always" as const } : {}),
  });
}

function submitOutputFingerprint(
  taskId: string,
  input: SubmitTaskOutputInput,
  fileToken: (file: File) => string,
): string {
  return JSON.stringify({
    taskId,
    expectedVersion: input.expectedVersion,
    summary: input.summary,
    artifacts: input.artifacts.map((artifact) => {
      const common = {
        clientRef: artifact.clientRef,
        storageKind: artifact.storageKind,
        name: artifact.name,
        requiresFollowup: artifact.requiresFollowup,
      };
      switch (artifact.storageKind) {
        case "text":
          return { ...common, contentText: artifact.contentText };
        case "link":
          return { ...common, referenceUrl: artifact.referenceUrl };
        case "structured":
          return { ...common, structuredJson: artifact.structuredJson };
        case "file":
          return {
            ...common,
            file: {
              token: fileToken(artifact.file),
              name: artifact.file.name,
              size: artifact.file.size,
              type: artifact.file.type,
              lastModified: artifact.file.lastModified,
            },
          };
      }
    }),
  });
}

function outputErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 || error.status === 409 || error.status === 410)
  );
}

export function useSubmitTaskOutput() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  const fileTokens = useRef(new WeakMap<File, string>());
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: SubmitTaskOutputInput;
    }) => {
      const fingerprint = submitOutputFingerprint(taskId, input, (file) => {
        const existing = fileTokens.current.get(file);
        if (existing) return existing;
        const token = crypto.randomUUID();
        fileTokens.current.set(file, token);
        return token;
      });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return submitTaskOutput(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAggregates(queryClient);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: variables.taskId,
        });
      }
    },
  });
}

export function useReviewTaskSubmission() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      input,
    }: {
      taskId: string;
      input: ReviewTaskSubmissionInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return reviewTaskSubmission(taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      setTaskDetailIfNotOlder(queryClient, result.task);
      await Promise.all([
        invalidateTaskAggregates(queryClient),
        invalidateAiPlanReviewProjections(queryClient),
      ]);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: variables.taskId,
        });
      }
    },
  });
}

export function useDeleteTaskArtifact() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      taskId,
      artifactId,
      input,
    }: {
      taskId: string;
      artifactId: string;
      input: DeleteTaskArtifactInput;
    }) => {
      const fingerprint = JSON.stringify({ taskId, artifactId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteTaskArtifact(artifactId, taskId, input, attempt.current.key);
    },
    onSuccess: async (result) => {
      attempt.current = null;
      queryClient.removeQueries({
        queryKey: taskArtifactDetailQueryKey(result.artifact.id),
      });
      setTaskDetailIfNotOlder(queryClient, result.task);
      await invalidateTaskAggregates(queryClient);
    },
    onError: async (error, variables) => {
      if (outputErrorNeedsRefresh(error)) {
        await invalidateTaskAggregates(queryClient, {
          preserveActiveDetailId: variables.taskId,
        });
      }
    },
  });
}

export function useDownloadTaskArtifact() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { taskId: string; id: string; name: string }) =>
      downloadTaskArtifact(id, name),
    onSettled: async (_data, _error, variables) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: taskArtifactDetailQueryKey(variables.id),
        }),
        queryClient.invalidateQueries({
          queryKey: taskArtifactQueryKey(variables.taskId),
        }),
        queryClient.invalidateQueries({
          queryKey: taskSubmissionQueryKey(variables.taskId),
        }),
      ]);
    },
  });
}
