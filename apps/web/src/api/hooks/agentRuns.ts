import { agentRunNeedsPolling } from "../../lib/agentRunDelivery";
import type {
  AgentRun,
  AgentRunInputSlotStatus,
  AgentRunListParams,
  ConfirmAgentRunStartGateInputs,
  ControlledFileListParams,
} from "../../types/models";
import { getAiAgentFamily } from "../aiAgentFamily";
import { getAiSessionDelegatedRuns } from "../aiSessionAgentRuns";
import type { AiSessionDelegatedRunListParams } from "../aiSessionAgentRuns";
import {
  ApiError,
  confirmAgentRunStartGateInputs,
  getAgentRun,
  getAgentRunStartGateInputs,
  getAgentRuns,
  getControlledFiles,
  getTaskAgentRuns,
  retryAgentRunOutputDelivery,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import {
  agentRunTerminalSignature,
  invalidateAgentRunDeliveryFacts,
  invalidateAgentRunTerminalFactsOnce,
} from "./invalidation";
import {
  agentRunDetailQueryKey,
  agentRunListQueryKey,
  agentRunQueryRootKey,
  agentRunStartGateInputsQueryKey,
  aiAgentFamilyQueryKey,
  aiSessionDelegatedRunsQueryKey,
  controlledFileListQueryKey,
  taskAgentRunsQueryKey,
} from "./queryKeys";

export function agentRunPollInterval(run: AgentRun | undefined) {
  return run && agentRunNeedsPolling(run) ? 2_000 : false;
}

export function useTaskAgentRunsQuery(taskId: string | null, enabled = true) {
  const queryClient = useQueryClient();
  const observedTerminalStates = useRef<{
    taskId: string;
    signatures: Map<string, string>;
  } | null>(null);
  const query = useQuery({
    queryKey: taskAgentRunsQueryKey(taskId ?? "closed"),
    queryFn: () => getTaskAgentRuns(taskId!),
    enabled: enabled && Boolean(taskId),
    refetchInterval: (query) =>
      query.state.data?.some(agentRunNeedsPolling) ? 2_000 : false,
  });
  useEffect(() => {
    if (!enabled || !taskId || !query.data) return;
    const signatures = new Map<string, string>();
    for (const run of query.data) {
      const signature = agentRunTerminalSignature(run);
      if (signature) signatures.set(run.id, signature);
    }
    const previous = observedTerminalStates.current;
    observedTerminalStates.current = { taskId, signatures };
    const newlyTerminal = query.data.filter((run) => {
      const signature = signatures.get(run.id);
      return Boolean(
        signature &&
        (previous?.taskId !== taskId ||
          previous.signatures.get(run.id) !== signature),
      );
    });
    if (newlyTerminal.length > 0) {
      void invalidateAgentRunTerminalFactsOnce(queryClient, newlyTerminal);
    }
  }, [enabled, query.data, queryClient, taskId]);
  return query;
}

export function useAgentRunQuery(
  runId: string | null,
  expectedTaskId: string | null,
  enabled = true,
) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: agentRunDetailQueryKey(
      runId ?? "closed",
      expectedTaskId ?? "unbound",
    ),
    queryFn: async ({ signal }) => {
      const run = await getAgentRun(runId!, signal);
      if (run.taskId !== expectedTaskId) {
        throw new ApiError("执行记录不属于当前任务。", {
          code: "AGENT_RUN_TASK_MISMATCH",
        });
      }
      return run;
    },
    enabled: enabled && Boolean(runId) && Boolean(expectedTaskId),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
    refetchInterval: (query) => agentRunPollInterval(query.state.data),
  });
  useEffect(() => {
    const run = query.data;
    if (!run || agentRunNeedsPolling(run)) return;
    void invalidateAgentRunTerminalFactsOnce(queryClient, [run]);
  }, [query.data, queryClient]);
  return query;
}

export function useRetryAgentRunOutputDelivery() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { runId: string; taskId: string }) => {
      const run = await retryAgentRunOutputDelivery(input.runId);
      if (run.taskId !== input.taskId) {
        throw new ApiError("产出登记记录不属于当前任务。", {
          code: "AGENT_RUN_TASK_MISMATCH",
        });
      }
      return run;
    },
    onSuccess: async (run) => {
      queryClient.setQueryData(agentRunDetailQueryKey(run.id, run.taskId), run);
      queryClient.setQueryData<AgentRun[]>(
        taskAgentRunsQueryKey(run.taskId),
        (rows) => rows?.map((row) => (row.id === run.id ? run : row)) ?? [run],
      );
      await invalidateAgentRunDeliveryFacts(queryClient, run.taskId);
    },
    onError: async (_error, input) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: agentRunDetailQueryKey(input.runId, input.taskId),
        }),
        queryClient.invalidateQueries({
          queryKey: taskAgentRunsQueryKey(input.taskId),
        }),
      ]);
    },
  });
}

/**
 * What a Run's input slot holds. It is never served from cache: the files and
 * the Provider shown here are what the person is about to confirm.
 */
export function useAgentRunStartGateInputsQuery(
  runId: string | null,
  slotStatus: AgentRunInputSlotStatus | null,
  enabled = true,
) {
  return useQuery({
    queryKey: agentRunStartGateInputsQueryKey(
      runId ?? "closed",
      slotStatus ?? "unknown",
    ),
    queryFn: ({ signal }) => getAgentRunStartGateInputs(runId!, signal),
    enabled: enabled && Boolean(runId) && Boolean(slotStatus),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
  });
}

export function useConfirmAgentRunStartGateInputs() {
  const queryClient = useQueryClient();
  // Confirmed or refused, what was on screen is no longer current: the Run
  // has started, or the files, the Provider or the gate changed.
  const refresh = (taskId: string) =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: agentRunQueryRootKey }),
      queryClient.invalidateQueries({
        queryKey: taskAgentRunsQueryKey(taskId),
      }),
    ]);
  return useMutation({
    mutationFn: (input: ConfirmAgentRunStartGateInputs & { taskId: string }) =>
      confirmAgentRunStartGateInputs(input),
    onSuccess: (_result, input) => refresh(input.taskId),
    onError: (_error, input) => refresh(input.taskId),
  });
}

export function useAgentRunsQuery(
  input: AgentRunListParams = {},
  enabled = true,
) {
  const queryClient = useQueryClient();
  const observedRunStates = useRef<Map<string, string | null> | null>(null);
  const query = useQuery({
    queryKey: [...agentRunListQueryKey(input)],
    queryFn: ({ signal }) => getAgentRuns(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: (query) =>
      (query.state.data?.meta.activeTotal ?? 0) > 0 ||
      (query.state.data?.meta.pendingDeliveryTotal ?? 0) > 0
        ? 5_000
        : false,
  });
  useEffect(() => {
    if (!query.data) return;
    const signatures = new Map<string, string | null>();
    for (const run of query.data.items) {
      signatures.set(run.id, agentRunTerminalSignature(run));
    }
    const previous = observedRunStates.current;
    observedRunStates.current = signatures;
    if (!previous) return;
    const newlyTerminal = query.data.items.filter((run) => {
      const signature = signatures.get(run.id);
      return Boolean(
        signature && previous.has(run.id) && previous.get(run.id) === null,
      );
    });
    if (newlyTerminal.length > 0) {
      void invalidateAgentRunTerminalFactsOnce(queryClient, newlyTerminal);
    }
  }, [query.data, queryClient]);
  return query;
}

export function useAiAgentFamilyQuery(sessionId: string, enabled = true) {
  return useQuery({
    queryKey: [...aiAgentFamilyQueryKey(sessionId)],
    queryFn: ({ signal }) => getAiAgentFamily(sessionId, signal),
    enabled: enabled && Boolean(sessionId),
    retry: 2,
    retryDelay: 500,
    staleTime: 5_000,
    refetchInterval: (query) =>
      query.state.data?.children.some(
        (child) => child.status === "queued" || child.status === "streaming",
      )
        ? 5_000
        : query.state.data?.children.length
          ? 15_000
          : false,
  });
}

export function useAiSessionDelegatedRunsQuery(
  sessionId: string,
  input: AiSessionDelegatedRunListParams = {},
  enabled = true,
) {
  const queryClient = useQueryClient();
  const observedRunStates = useRef<{
    sessionId: string;
    signatures: Map<string, string | null>;
  } | null>(null);
  const query = useQuery({
    queryKey: [...aiSessionDelegatedRunsQueryKey(sessionId, input)],
    queryFn: ({ signal }) =>
      getAiSessionDelegatedRuns(sessionId, input, signal),
    enabled: enabled && Boolean(sessionId),
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: (current) =>
      (current.state.data?.meta.activeTotal ?? 0) > 0 ||
      (current.state.data?.meta.pendingDeliveryTotal ?? 0) > 0
        ? 5_000
        : false,
  });
  useEffect(() => {
    if (!enabled || !query.data) return;
    const runs = query.data.items.flatMap((item) =>
      item.run ? [item.run] : [],
    );
    const signatures = new Map<string, string | null>();
    for (const run of runs) {
      signatures.set(run.id, agentRunTerminalSignature(run));
    }
    const previous = observedRunStates.current;
    observedRunStates.current = { sessionId, signatures };
    if (!previous || previous.sessionId !== sessionId) return;
    const newlyTerminal = runs.filter((run) => {
      const signature = signatures.get(run.id);
      return Boolean(
        signature &&
        previous.signatures.has(run.id) &&
        previous.signatures.get(run.id) === null,
      );
    });
    if (newlyTerminal.length > 0) {
      void invalidateAgentRunTerminalFactsOnce(queryClient, newlyTerminal);
    }
  }, [enabled, query.data, queryClient, sessionId]);
  return query;
}

export function useControlledFilesQuery(input: ControlledFileListParams = {}) {
  return useQuery({
    queryKey: [...controlledFileListQueryKey(input)],
    queryFn: ({ signal }) => getControlledFiles(input, signal),
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}
