import type {
  ClientInput,
  ClientListParams,
  UpdateClientInput,
} from "../../types/models";
import {
  ApiError,
  createClient,
  deleteClient,
  getAllActors,
  getClient,
  getClients,
  updateClient,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateClientEditFacts,
  invalidateClientQueries,
  invalidateUnifiedSearch,
} from "./invalidation";
import {
  actorQueryKey,
  clientDetailQueryKey,
  clientQueryKey,
  projectQueryKey,
} from "./queryKeys";

export function useClientActorOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "client-contact-options"],
    queryFn: ({ signal }) =>
      getAllActors({ type: "person", status: "active" }, signal),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientsQuery(input: ClientListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...clientQueryKey, "list", input],
    queryFn: ({ signal }) => getClients(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientOptionsQuery(search = "", page = 1, enabled = true) {
  const normalizedSearch = search.trim();
  return useQuery({
    queryKey: [...clientQueryKey, "options", normalizedSearch, page],
    queryFn: ({ signal }) =>
      getClients(
        {
          page,
          pageSize: 20,
          q: normalizedSearch || undefined,
          sort: "name",
        },
        signal,
      ),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useClientQuery(id: string | null) {
  return useQuery({
    queryKey: clientDetailQueryKey(id ?? "missing"),
    queryFn: ({ signal }) => getClient(id!, signal),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useCreateClient() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: ClientInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createClient(input, attempt.current.key);
    },
    onSuccess: async (client) => {
      attempt.current = null;
      queryClient.setQueryData(clientDetailQueryKey(client.id), client);
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await invalidateUnifiedSearch(queryClient);
    },
  });
}

export function useUpdateClient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateClientInput }) =>
      updateClient(id, input),
    onSuccess: async (client) => {
      queryClient.setQueryData(clientDetailQueryKey(client.id), client);
      await invalidateClientEditFacts(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateClientEditFacts(queryClient, variables.id);
      }
    },
  });
}

export function useDeleteClient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      expectedVersion,
    }: {
      id: string;
      expectedVersion: number;
    }) => deleteClient(id, expectedVersion),
    onSuccess: async (_, variables) => {
      queryClient.removeQueries({
        queryKey: clientDetailQueryKey(variables.id),
      });
      await queryClient.invalidateQueries({ queryKey: clientQueryKey });
      await queryClient.invalidateQueries({ queryKey: projectQueryKey });
      await invalidateUnifiedSearch(queryClient);
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await Promise.all([
          ...invalidateClientQueries(queryClient, variables.id),
          queryClient.invalidateQueries({ queryKey: projectQueryKey }),
          invalidateUnifiedSearch(queryClient),
        ]);
      }
    },
  });
}
