import type {
  AutomationConfig,
  AutomationRunListParams,
} from "../../types/models";
import {
  ApiError,
  disableAutomationRule,
  enableAutomationRule,
  getAutomationRules,
  getAutomationRun,
  getAutomationRuns,
  previewAutomationRule,
  retryAutomationRun,
  updateAutomationRule,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  invalidateAutomationFacts,
  invalidateInboxFacts,
  invalidateTaskFacts,
} from "./invalidation";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  automationRulesQueryKey,
  automationRunDetailQueryKey,
  automationRunsQueryKey,
} from "./queryKeys";

export function useAutomationRulesQuery(enabled = true, fresh = false) {
  return useQuery({
    queryKey: automationRulesQueryKey,
    queryFn: ({ signal }) => getAutomationRules(signal),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: fresh ? 0 : 10_000,
    ...(fresh ? { refetchOnMount: "always" as const } : {}),
  });
}

export function useAutomationRunsQuery(
  input: AutomationRunListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...automationRunsQueryKey, input],
    queryFn: ({ signal }) => getAutomationRuns(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 1,
    staleTime: 5_000,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

export function useAutomationRunQuery(id: string | null, fresh = false) {
  return useQuery({
    queryKey: automationRunDetailQueryKey(id ?? "idle"),
    queryFn: ({ signal }) => getAutomationRun(id!, signal),
    enabled: Boolean(id),
    retry: 1,
    staleTime: fresh ? 0 : 5_000,
    ...(fresh ? { refetchOnMount: "always" as const, gcTime: 0 } : {}),
  });
}

export function usePreviewAutomationRule() {
  return useMutation({
    mutationFn: ({ id, config }: { id: string; config: AutomationConfig }) =>
      previewAutomationRule(id, config),
  });
}

export function useUpdateAutomationRule() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      config,
      expectedVersion,
    }: {
      id: string;
      config: AutomationConfig;
      expectedVersion: number;
    }) => updateAutomationRule(id, config, expectedVersion),
    onSuccess: async () => invalidateAutomationFacts(queryClient),
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        await queryClient.invalidateQueries({
          queryKey: automationRulesQueryKey,
        });
      }
    },
  });
}

export function useSetAutomationRuleEnabled() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      enabled,
      expectedVersion,
    }: {
      id: string;
      enabled: boolean;
      expectedVersion: number;
    }) =>
      enabled
        ? enableAutomationRule(id, expectedVersion)
        : disableAutomationRule(id, expectedVersion),
    onSuccess: async () => invalidateAutomationFacts(queryClient),
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        await queryClient.invalidateQueries({
          queryKey: automationRulesQueryKey,
        });
      }
    },
  });
}

export function useRetryAutomationRun() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: retryAutomationRun,
    onSuccess: async (run) => {
      await Promise.all([
        invalidateAutomationFacts(queryClient),
        ...(run.resultType === "task"
          ? [invalidateTaskFacts(queryClient)]
          : run.resultType === "inbox_item"
            ? [invalidateInboxFacts(queryClient)]
            : []),
      ]);
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey: automationRunsQueryKey });
    },
  });
}
