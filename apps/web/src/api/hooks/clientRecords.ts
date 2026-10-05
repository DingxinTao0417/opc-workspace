import type {
  CancelClientFollowupInput,
  ClientActivityListParams,
  ClientActorLinkListParams,
  ClientAttachmentListParams,
  ClientFollowupListParams,
  CompleteClientFollowupInput,
  CreateClientActivityInput,
  CreateClientActorLinkInput,
  CreateClientAttachmentInput,
  CreateClientFollowupInput,
  DeleteClientActivityInput,
  DeleteClientActorLinkInput,
  DeleteClientAttachmentInput,
  RescheduleClientFollowupInput,
  SkipClientFollowupInput,
  UpdateClientActivityInput,
  UpdateClientFollowupInput,
} from "../../types/models";
import {
  ApiError,
  cancelClientFollowup,
  completeClientFollowup,
  createClientActivity,
  createClientActorLink,
  createClientAttachment,
  createClientFollowup,
  deleteClientActivity,
  deleteClientActorLink,
  deleteClientAttachment,
  downloadClientAttachment,
  getAllActors,
  getClientActivities,
  getClientActorLinks,
  getClientAttachments,
  getClientFollowups,
  getRecentClientActivities,
  rescheduleClientFollowup,
  skipClientFollowup,
  updateClientActivity,
  updateClientFollowup,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateClientActivityConflict,
  invalidateUnifiedSearch,
} from "./invalidation";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  actorQueryKey,
  clientActivityQueryKey,
  clientActorLinkQueryKey,
  clientAttachmentQueryKey,
  clientDetailQueryKey,
  clientFollowupQueryKey,
  clientQueryKey,
  inboxQueryKey,
  recentClientActivityQueryKey,
} from "./queryKeys";

export function useRecentClientActivitiesQuery(pageSize = 3) {
  return useQuery({
    queryKey: [...recentClientActivityQueryKey, { page: 1, pageSize }],
    queryFn: () => getRecentClientActivities({ page: 1, pageSize }),
    retry: 1,
  });
}

export function useClientActivitiesQuery(
  clientId: string | null,
  input: ClientActivityListParams = {},
) {
  return useQuery({
    queryKey: [...clientActivityQueryKey(clientId ?? "missing"), "list", input],
    queryFn: () => getClientActivities(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useClientFollowupsQuery(
  clientId: string | null,
  input: ClientFollowupListParams = {},
) {
  const includesPlannedFollowups = !input.status || input.status === "planned";
  return useQuery({
    queryKey: [...clientFollowupQueryKey(clientId ?? "missing"), "list", input],
    queryFn: () => getClientFollowups(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
    staleTime: 10_000,
    refetchInterval: includesPlannedFollowups
      ? INBOX_LIST_REFRESH_INTERVAL_MS
      : false,
  });
}

export function useClientFollowupActorOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "client-followup-options"],
    queryFn: async ({ signal }) => {
      const actors = await getAllActors({ status: "active" }, signal);
      return actors.filter(
        (actor) => actor.type === "owner" || actor.type === "person",
      );
    },
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

function useInvalidateClientFollowups() {
  const queryClient = useQueryClient();
  return async (clientId?: string) => {
    await Promise.all([
      ...(clientId
        ? [
            queryClient.invalidateQueries({
              queryKey: clientFollowupQueryKey(clientId),
            }),
            queryClient.invalidateQueries({
              queryKey: clientDetailQueryKey(clientId),
            }),
          ]
        : []),
      queryClient.invalidateQueries({ queryKey: clientQueryKey }),
      queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      invalidateUnifiedSearch(queryClient),
    ]);
  };
}

function isClientFollowupFactsStale(error: unknown): boolean {
  return error instanceof ApiError && error.code === "VERSION_CONFLICT";
}

export function useCreateClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateClientFollowupInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientFollowup(input, attempt.current.key);
    },
    onSuccess: async (followup) => {
      attempt.current = null;
      await invalidate(followup.clientId);
    },
  });
}

export function useUpdateClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateClientFollowupInput;
    }) => updateClientFollowup(id, input),
    onSuccess: async (followup) => invalidate(followup.clientId),
    onError: async (error) => {
      if (isClientFollowupFactsStale(error)) await invalidate();
    },
  });
}

export function useCompleteClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: CompleteClientFollowupInput;
    }) => completeClientFollowup(id, input),
    onSuccess: async (followup) => invalidate(followup.clientId),
    onError: async (error) => {
      if (isClientFollowupFactsStale(error)) await invalidate();
    },
  });
}

export function useSkipClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: SkipClientFollowupInput;
    }) => skipClientFollowup(id, input),
    onSuccess: async (followup) => invalidate(followup.clientId),
    onError: async (error) => {
      if (isClientFollowupFactsStale(error)) await invalidate();
    },
  });
}

export function useCancelClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: CancelClientFollowupInput;
    }) => cancelClientFollowup(id, input),
    onSuccess: async (followup) => invalidate(followup.clientId),
    onError: async (error) => {
      if (isClientFollowupFactsStale(error)) await invalidate();
    },
  });
}

export function useRescheduleClientFollowup() {
  const invalidate = useInvalidateClientFollowups();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: RescheduleClientFollowupInput;
    }) => rescheduleClientFollowup(id, input),
    onSuccess: async (result) => invalidate(result.next.clientId),
    onError: async (error) => {
      if (isClientFollowupFactsStale(error)) await invalidate();
    },
  });
}

export function useCreateClientActivity() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientActivityInput;
    }) => {
      const fingerprint = JSON.stringify({ clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientActivity(clientId, input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
  });
}

export function useUpdateClientActivity() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      clientId: _clientId,
      id,
      input,
    }: {
      clientId: string;
      id: string;
      input: UpdateClientActivityInput;
    }) => updateClientActivity(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateClientActivityConflict(queryClient, variables.clientId);
      }
    },
  });
}

export function useDeleteClientActivity() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      clientId: _clientId,
      id,
      input,
    }: {
      clientId: string;
      id: string;
      input: DeleteClientActivityInput;
    }) => deleteClientActivity(id, input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateClientActivityConflict(queryClient, variables.clientId);
      }
    },
  });
}

export function useClientAttachmentsQuery(
  clientId: string | null,
  input: ClientAttachmentListParams = {},
) {
  return useQuery({
    queryKey: [
      ...clientAttachmentQueryKey(clientId ?? "missing"),
      "list",
      input,
    ],
    queryFn: () => getClientAttachments(clientId!, input),
    enabled: Boolean(clientId),
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateClientAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({
        clientId,
        name: input.name,
        activityId: input.activityId,
        expectedVersion: input.expectedVersion,
        fileName: input.file.name,
        fileSize: input.file.size,
        fileModified: input.file.lastModified,
      });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientAttachment(clientId, input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDeleteClientAttachment() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      clientId,
      input,
    }: {
      id: string;
      clientId: string;
      input: DeleteClientAttachmentInput;
    }) => {
      const fingerprint = JSON.stringify({ id, clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteClientAttachment(id, input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDownloadClientAttachment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      name,
    }: {
      clientId: string;
      id: string;
      name: string;
    }) => downloadClientAttachment(id, name),
    onSettled: async (_data, _error, variables) => {
      await queryClient.invalidateQueries({
        queryKey: clientAttachmentQueryKey(variables.clientId),
      });
    },
  });
}

export function useClientActorLinksQuery(
  clientId: string | null,
  input: ClientActorLinkListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [
      ...clientActorLinkQueryKey(clientId ?? "missing"),
      "list",
      input,
    ],
    queryFn: ({ signal }) => getClientActorLinks(clientId!, input, signal),
    enabled: Boolean(clientId) && enabled,
    placeholderData: keepPreviousData,
    retry: 1,
  });
}

export function useCreateClientActorLink() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      clientId,
      input,
    }: {
      clientId: string;
      input: CreateClientActorLinkInput;
    }) => {
      const fingerprint = JSON.stringify({ clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClientActorLink(clientId, input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}

export function useDeleteClientActorLink() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: ({
      id,
      clientId,
      input,
    }: {
      id: string;
      clientId: string;
      input: DeleteClientActorLinkInput;
    }) => {
      const fingerprint = JSON.stringify({ id, clientId, input });
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return deleteClientActorLink(id, input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      }
    },
  });
}
