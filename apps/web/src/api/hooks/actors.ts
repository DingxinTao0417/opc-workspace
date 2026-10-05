import type {
  ActorListParams,
  CreatePersonActorInput,
  UpdateActorInput,
} from "../../types/models";
import {
  ApiError,
  createPersonActor,
  getActor,
  getActors,
  getAllActors,
  updateActor,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  invalidateActorOptions,
  invalidateActorReferences,
} from "./invalidation";
import { actorDetailQueryKey, actorQueryKey } from "./queryKeys";

export function useActorsQuery(input: ActorListParams = {}, enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "list", input],
    queryFn: ({ signal }) => getActors(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useAssignmentActorOptionsQuery(enabled = true) {
  return useQuery({
    queryKey: [...actorQueryKey, "assignment-options"],
    queryFn: ({ signal }) => getAllActors({ status: "active" }, signal),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useActorQuery(id: string | null) {
  return useQuery({
    queryKey: actorDetailQueryKey(id ?? "closed"),
    queryFn: () => getActor(id!),
    enabled: Boolean(id),
    retry: 1,
  });
}

export function useCreatePersonActor() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreatePersonActorInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createPersonActor(input, attempt.current.key);
    },
    onSuccess: async (actor) => {
      attempt.current = null;
      queryClient.setQueryData(actorDetailQueryKey(actor.id), actor);
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
  });
}

export function useUpdateActor() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateActorInput }) =>
      updateActor(id, input),
    onSuccess: async (actor) => {
      queryClient.setQueryData(actorDetailQueryKey(actor.id), actor);
      await invalidateActorReferences(queryClient);
      await queryClient.invalidateQueries({ queryKey: actorQueryKey });
    },
    onError: async (error, variables) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await Promise.all([
          invalidateActorReferences(queryClient),
          invalidateActorOptions(queryClient),
        ]);
        await queryClient.invalidateQueries({
          queryKey: actorDetailQueryKey(variables.id),
          exact: true,
        });
      }
    },
  });
}
