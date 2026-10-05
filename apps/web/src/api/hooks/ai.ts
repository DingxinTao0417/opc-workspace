import { configureAiChatRefresh, useAiChatStore } from "../../store/aiChat";
import type { AiBusinessContextType, NewTaskInput } from "../../types/models";
import { confirmAiMessageTask } from "../aiActions";
import { getAiAgentInbox } from "../aiAgentInbox";
import { getAiWorkPlanInbox } from "../aiWorkPlan";
import {
  attachTaskToAiMessage,
  checkAiProviderHealth,
  createAiMemory,
  createAiProvider,
  createAiSession,
  deleteAiMemory,
  deleteAiProvider,
  deleteAiSession,
  getAiMemories,
  getAiMemoryProposals,
  getAiMessages,
  getAiProviders,
  getAiSessions,
  previewAiBusinessContext,
  rejectAiMemoryProposal,
  setAiProviderKey,
  updateAiProvider,
} from "../client";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateTaskAggregates } from "./invalidation";
import {
  aiAgentInboxQueryKey,
  aiMemoriesQueryKey,
  aiMemoryProposalsQueryKey,
  aiMessagesQueryKey,
  aiProvidersQueryKey,
  aiSessionsQueryKey,
  aiUsageSummarySessionQueryKey,
  aiWorkPlanInboxQueryKey,
} from "./queryKeys";

export function useAiMemoriesQuery(enabled = true) {
  return useQuery({
    queryKey: aiMemoriesQueryKey,
    queryFn: getAiMemories,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useAiMemoryProposalsQuery(enabled = true) {
  return useQuery({
    queryKey: aiMemoryProposalsQueryKey,
    queryFn: getAiMemoryProposals,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateAiMemory() {
  const queryClient = useQueryClient();
  const idempotencyKey = useRef<string | null>(null);
  return useMutation({
    mutationFn: (input: {
      content: string;
      source_message_id?: string;
      proposal_id?: string;
    }) => {
      idempotencyKey.current ??= crypto.randomUUID();
      return createAiMemory(input, idempotencyKey.current);
    },
    onSuccess: async () => {
      idempotencyKey.current = null;
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiMemoriesQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiMemoryProposalsQueryKey }),
      ]);
    },
    onError: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiMemoriesQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiMemoryProposalsQueryKey }),
      ]);
    },
  });
}

export function useRejectAiMemoryProposal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id }: { id: string }) => rejectAiMemoryProposal(id),
    onSettled: async () => {
      await queryClient.invalidateQueries({
        queryKey: aiMemoryProposalsQueryKey,
      });
    },
  });
}

export function usePreviewAiBusinessContext() {
  return useMutation({
    mutationFn: (input: {
      provider_id: string;
      sources: Array<{ type: AiBusinessContextType; id: string }>;
      knowledge?: Array<{
        source_id: string;
        document_id: string;
        chunk_id: string;
      }>;
    }) => previewAiBusinessContext(input),
  });
}

export function useDeleteAiMemory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id }: { id: string }) => deleteAiMemory(id),
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: aiMemoriesQueryKey });
    },
  });
}

export function useAiProvidersQuery(enabled = true) {
  return useQuery({
    queryKey: aiProvidersQueryKey,
    queryFn: getAiProviders,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateAiProvider() {
  const queryClient = useQueryClient();
  const idempotencyKey = useRef<string | null>(null);
  return useMutation({
    mutationFn: (input: {
      name: string;
      kind: "remote" | "local";
      protocol: "openai_chat" | "anthropic_messages";
      base_url: string;
      model: string;
    }) => {
      idempotencyKey.current ??= crypto.randomUUID();
      return createAiProvider(input, idempotencyKey.current);
    },
    onSuccess: async () => {
      idempotencyKey.current = null;
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
    onError: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
  });
}

export function useUpdateAiProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
      input,
    }: {
      id: string;
      expectedVersion: number;
      input: {
        name?: string;
        protocol?: "openai_chat" | "anthropic_messages";
        base_url?: string;
        model?: string;
      };
    }) => updateAiProvider(id, input, expectedVersion),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
  });
}

export function useDeleteAiProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteAiProvider(id, expectedVersion),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
  });
}

export function useCheckAiProviderHealth() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => checkAiProviderHealth(id, expectedVersion),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
  });
}

export function useSetAiProviderKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      apiKey,
      expectedVersion,
    }: {
      id: string;
      apiKey: string;
      expectedVersion: number;
    }) => setAiProviderKey(id, apiKey, expectedVersion),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: aiProvidersQueryKey }),
        queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      ]);
    },
  });
}

export function useAiSessionsQuery(enabled = true) {
  return useQuery({
    queryKey: aiSessionsQueryKey,
    queryFn: getAiSessions,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });
}

export function useAiWorkPlanInboxQuery(enabled = true) {
  return useInfiniteQuery({
    queryKey: aiWorkPlanInboxQueryKey,
    queryFn: ({ pageParam, signal }) =>
      getAiWorkPlanInbox("attention", 50, pageParam, signal),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => lastPage.meta.next_offset ?? undefined,
    enabled,
    retry: 1,
    staleTime: 5_000,
    refetchInterval: (query) => {
      const meta = query.state.data?.pages[0]?.meta;
      return meta &&
        (meta.running_total > 0 ||
          meta.needs_approval_total > 0 ||
          meta.needs_recovery_total > 0)
        ? 5_000
        : 15_000;
    },
    refetchIntervalInBackground: false,
  });
}

export function useAiAgentInboxQuery(enabled = true) {
  return useInfiniteQuery({
    queryKey: aiAgentInboxQueryKey,
    queryFn: ({ pageParam, signal }) =>
      getAiAgentInbox("all", 50, pageParam, signal),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => lastPage.meta.next_offset ?? undefined,
    enabled,
    retry: 1,
    staleTime: 5_000,
    refetchInterval: (query) =>
      (query.state.data?.pages[0]?.meta.total ?? 0) > 0 ? 5_000 : 15_000,
    refetchIntervalInBackground: false,
  });
}

export function useCreateAiSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => createAiSession(),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: aiSessionsQueryKey });
    },
  });
}

export function useDeleteAiSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteAiSession(id, expectedVersion),
    onSuccess: async (_result, variables) => {
      await queryClient.invalidateQueries({ queryKey: aiSessionsQueryKey });
      await queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey });
      await queryClient.invalidateQueries({
        queryKey: aiWorkPlanInboxQueryKey,
      });
      await queryClient.removeQueries({
        queryKey: aiMessagesQueryKey(variables.id),
      });
    },
    onError: async () => {
      await queryClient.invalidateQueries({ queryKey: aiSessionsQueryKey });
    },
  });
}

export function useAiMessagesInfiniteQuery(sessionId: string, enabled = true) {
  return useInfiniteQuery({
    queryKey: [...aiMessagesQueryKey(sessionId), "infinite"],
    queryFn: ({ pageParam }) => getAiMessages(sessionId, pageParam),
    initialPageParam: undefined as
      { oldestCreatedAt?: string; oldestId?: string } | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.meta.has_more &&
      lastPage.meta.oldest_created_at &&
      lastPage.meta.oldest_id
        ? {
            oldestCreatedAt: lastPage.meta.oldest_created_at,
            oldestId: lastPage.meta.oldest_id,
          }
        : undefined,
    enabled: enabled && sessionId !== "",
    retry: 2,
    retryDelay: 500,
  });
}

export function useAttachTaskToAiMessage(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      messageId,
      taskId,
    }: {
      messageId: string;
      taskId: string;
    }) => attachTaskToAiMessage(messageId, taskId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: aiMessagesQueryKey(sessionId),
      });
    },
  });
}

// The stream belongs to the application. Route unmounts only unsubscribe the
// view; the controller and generated content remain available to the monitor.
export function useAiChatStream() {
  const queryClient = useQueryClient();
  const state = useAiChatStore();
  configureAiChatRefresh(async (sessionId) => {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: aiMessagesQueryKey(sessionId),
      }),
      queryClient.invalidateQueries({ queryKey: aiSessionsQueryKey }),
      queryClient.invalidateQueries({ queryKey: aiAgentInboxQueryKey }),
      queryClient.invalidateQueries({ queryKey: aiWorkPlanInboxQueryKey }),
      queryClient.invalidateQueries({
        queryKey: ["ai", "work-plan", sessionId],
      }),
      queryClient.invalidateQueries({
        queryKey: aiUsageSummarySessionQueryKey(sessionId),
      }),
    ]);
  });
  return { ...state, isStreaming: state.streaming !== null };
}

export function useConfirmAiMessageTask(sessionId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      messageId,
      input,
    }: {
      messageId: string;
      input: NewTaskInput;
    }) => confirmAiMessageTask(messageId, input),
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: aiMessagesQueryKey(sessionId),
        }),
        invalidateTaskAggregates(queryClient),
      ]);
    },
  });
}
