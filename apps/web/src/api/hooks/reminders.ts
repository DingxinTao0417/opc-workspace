import type {
  CancelReminderInput,
  CreateReminderInput,
  ReminderListParams,
  UpdateReminderInput,
} from "../../types/models";
import {
  ApiError,
  cancelReminder,
  createReminder,
  getReminder,
  getReminders,
  updateReminder,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateReminderFacts } from "./invalidation";
import {
  INBOX_LIST_REFRESH_INTERVAL_MS,
  inboxQueryKey,
  reminderDetailQueryKey,
  reminderQueryKey,
} from "./queryKeys";

export function useRemindersQuery(
  input: ReminderListParams = {},
  enabled = true,
) {
  return useQuery({
    queryKey: [...reminderQueryKey, "list", input],
    queryFn: ({ signal }) => getReminders(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

export function useReminderQuery(id: string | null) {
  return useQuery({
    queryKey: reminderDetailQueryKey(id ?? "closed"),
    queryFn: ({ signal }) => getReminder(id!, signal),
    enabled: Boolean(id),
    retry: 1,
    refetchInterval: INBOX_LIST_REFRESH_INTERVAL_MS,
  });
}

function reminderFactsNeedRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "VERSION_CONFLICT")
  );
}

function reminderMayHaveProjectedInbox(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.code === "VERSION_CONFLICT" ||
      error.code === "REMINDER_NOT_SCHEDULED" ||
      error.code === "REMINDER_INBOX_MISMATCH")
  );
}

async function refreshReminderAfterError(
  queryClient: QueryClient,
  id: string,
  error: unknown,
): Promise<void> {
  if (!reminderFactsNeedRefresh(error)) return;
  await Promise.all([
    invalidateReminderFacts(queryClient, id),
    ...(reminderMayHaveProjectedInbox(error)
      ? [queryClient.invalidateQueries({ queryKey: inboxQueryKey })]
      : []),
  ]);
}

export function useCreateReminder() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateReminderInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createReminder(input, attempt.current.key);
    },
    onSuccess: async (reminder) => {
      attempt.current = null;
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
  });
}

export function useUpdateReminder() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateReminderInput }) =>
      updateReminder(id, input),
    onSuccess: async (reminder) => {
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
    onError: async (error, variables) => {
      await refreshReminderAfterError(queryClient, variables.id, error);
    },
  });
}

export function useCancelReminder() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CancelReminderInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return cancelReminder(input, attempt.current.key);
    },
    onSuccess: async (reminder) => {
      attempt.current = null;
      queryClient.setQueryData(reminderDetailQueryKey(reminder.id), reminder);
      await invalidateReminderFacts(queryClient, reminder.id);
    },
    onError: async (error, variables) => {
      await refreshReminderAfterError(queryClient, variables.id, error);
    },
  });
}
