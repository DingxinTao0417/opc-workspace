import type {
  AiEvaluationSuiteKey,
  CreateAiEvaluationReviewInput,
} from "../../types/models";
import {
  cancelAiEvaluation,
  createAiEvaluation,
  createAiEvaluationReview,
  deleteAiEvaluation,
  getAiEvaluation,
  getAiEvaluationReviews,
  getAiEvaluations,
  getAiEvaluationSummary,
} from "../client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import {
  aiEvaluationQueryKey,
  aiEvaluationReviewsQueryKey,
  aiEvaluationSummaryQueryKey,
  aiEvaluationsQueryKey,
} from "./queryKeys";

export function useAiEvaluationsQuery(enabled = true) {
  return useQuery({
    queryKey: aiEvaluationsQueryKey,
    queryFn: () => getAiEvaluations({ page: 1, pageSize: 20 }),
    enabled,
    retry: 2,
    retryDelay: 500,
    refetchInterval: (query) =>
      query.state.data?.items.some(
        (run) => run.status === "queued" || run.status === "running",
      )
        ? 500
        : false,
    refetchIntervalInBackground: false,
  });
}

export function useAiEvaluationSummaryQuery(enabled = true) {
  return useQuery({
    queryKey: aiEvaluationSummaryQueryKey,
    queryFn: () => getAiEvaluationSummary({ limit: 12 }),
    enabled,
    retry: 2,
    retryDelay: 500,
    refetchInterval: (query) =>
      (query.state.data?.statusCounts.queued ?? 0) +
        (query.state.data?.statusCounts.running ?? 0) >
      0
        ? 500
        : false,
    refetchIntervalInBackground: false,
  });
}

export function useAiEvaluationReviewsQuery(enabled = true) {
  return useQuery({
    queryKey: aiEvaluationReviewsQueryKey,
    queryFn: () => getAiEvaluationReviews({ page: 1, pageSize: 20 }),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useAiEvaluationQuery(id: string, enabled = true) {
  return useQuery({
    queryKey: aiEvaluationQueryKey(id),
    queryFn: () => getAiEvaluation(id),
    enabled: enabled && id !== "",
    retry: 2,
    retryDelay: 500,
    refetchInterval: (query) =>
      query.state.data?.status === "queued" ||
      query.state.data?.status === "running"
        ? 500
        : false,
    refetchIntervalInBackground: false,
  });
}

export function useCreateAiEvaluation() {
  const queryClient = useQueryClient();
  const idempotency = useRef<{ identity: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      providerId,
      providerVersion,
      suiteKey,
    }: {
      providerId: string;
      providerVersion: number;
      suiteKey: AiEvaluationSuiteKey;
    }) => {
      const identity = `${providerId}\u0000${providerVersion}\u0000${suiteKey}`;
      if (idempotency.current?.identity !== identity) {
        idempotency.current = { identity, key: crypto.randomUUID() };
      }
      return createAiEvaluation(
        providerId,
        providerVersion,
        suiteKey,
        idempotency.current.key,
      );
    },
    onSuccess: async (run) => {
      idempotency.current = null;
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiEvaluationsQueryKey }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationSummaryQueryKey,
        }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationQueryKey(run.id),
        }),
      ]);
    },
    onError: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiEvaluationsQueryKey }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationSummaryQueryKey,
        }),
      ]);
    },
  });
}

export function useCreateAiEvaluationReview() {
  const queryClient = useQueryClient();
  const idempotency = useRef<{ identity: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateAiEvaluationReviewInput) => {
      const group = input.group;
      const identity = JSON.stringify([
        group.providerId,
        group.providerNameSnapshot,
        group.providerModelSnapshot,
        group.datasetVersion,
        group.suiteKey,
        group.providerVersionMin,
        group.providerVersionMax,
        group.lastCompletedAt,
        group.runCount,
        group.totalCases,
        group.passedCases,
        group.failedCases,
        group.readinessStatus,
        group.readinessReasons,
        input.decision,
        input.reason,
      ]);
      if (idempotency.current?.identity !== identity) {
        idempotency.current = { identity, key: crypto.randomUUID() };
      }
      return createAiEvaluationReview(input, idempotency.current.key);
    },
    onSuccess: async () => {
      idempotency.current = null;
      await queryClient.invalidateQueries({
        queryKey: aiEvaluationReviewsQueryKey,
      });
    },
    onError: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: aiEvaluationReviewsQueryKey,
        }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationSummaryQueryKey,
        }),
      ]);
    },
  });
}

export function useCancelAiEvaluation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id }: { id: string }) => cancelAiEvaluation(id),
    onSettled: async (_result, _error, variables) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiEvaluationsQueryKey }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationSummaryQueryKey,
        }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationQueryKey(variables.id),
        }),
      ]);
    },
  });
}

export function useDeleteAiEvaluation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id }: { id: string }) => deleteAiEvaluation(id),
    onSuccess: async (_result, variables) => {
      queryClient.removeQueries({
        queryKey: aiEvaluationQueryKey(variables.id),
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiEvaluationsQueryKey }),
        queryClient.invalidateQueries({
          queryKey: aiEvaluationSummaryQueryKey,
        }),
      ]);
    },
  });
}
