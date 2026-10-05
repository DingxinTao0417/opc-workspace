import {
  checkAgentAdapter,
  disableAgentAdapter,
  enableAgentAdapter,
  getAgentAdapters,
  registerAgentAdapter,
} from "../client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { actorQueryKey, agentAdaptersQueryKey } from "./queryKeys";

export function useAgentAdaptersQuery(enabled = true) {
  return useQuery({
    queryKey: agentAdaptersQueryKey,
    queryFn: getAgentAdapters,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useRegisterAgentAdapter() {
  const queryClient = useQueryClient();
  const idempotencyKey = useRef<string | null>(null);
  return useMutation({
    mutationFn: () => {
      idempotencyKey.current ??= crypto.randomUUID();
      return registerAgentAdapter(
        "builtin-local-text-v1",
        idempotencyKey.current,
      );
    },
    onSuccess: async () => {
      idempotencyKey.current = null;
      await queryClient.invalidateQueries({ queryKey: agentAdaptersQueryKey });
    },
  });
}

export function useCheckAgentAdapter() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => checkAgentAdapter(id, expectedVersion),
    onSuccess: async (adapter) => {
      await queryClient.invalidateQueries({ queryKey: agentAdaptersQueryKey });
      return adapter;
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey: agentAdaptersQueryKey });
    },
  });
}

export function useSetAgentAdapterEnabled() {
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
        ? enableAgentAdapter(id, expectedVersion)
        : disableAgentAdapter(id, expectedVersion),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: agentAdaptersQueryKey }),
        queryClient.invalidateQueries({ queryKey: actorQueryKey }),
      ]);
    },
  });
}
