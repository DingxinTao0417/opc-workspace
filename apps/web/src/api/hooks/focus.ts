import { useFocusCycleStore } from "../../store/focus";
import { useSettingsStore } from "../../store/settings";
import type {
  CreateFocusSessionInput,
  FocusReportParams,
  FocusSessionCommandInput,
  FocusSessionListParams,
  FocusSessionSnapshot,
  RecoverFocusSessionInput,
} from "../../types/models";
import {
  ApiError,
  cancelFocusSession,
  createFocusSession,
  getActiveFocusSession,
  getFocusReport,
  getFocusSessions,
  getTodayStats,
  pauseFocusSession,
  recoverFocusSession,
  resumeFocusSession,
  stopFocusSession,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateFocusDependents } from "./invalidation";
import {
  focusReportQueryKey,
  focusSessionHistoryQueryKey,
  focusSessionQueryKey,
} from "./queryKeys";

const openFocusStatuses = new Set(["active", "paused", "recovery_pending"]);

function activeFocusSnapshot(
  snapshot: FocusSessionSnapshot,
): FocusSessionSnapshot {
  if (snapshot.session && !openFocusStatuses.has(snapshot.session.status)) {
    return { ...snapshot, session: null };
  }
  return snapshot;
}

function cacheFocusSnapshot(
  queryClient: QueryClient,
  snapshot: FocusSessionSnapshot,
  before: FocusSessionSnapshot | undefined,
) {
  const current =
    queryClient.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey);
  const incoming = snapshot.session;
  if (current?.session && incoming) {
    // Commands describe one Session, not the current global slot. A late end
    // receipt must never erase B; a late pause must never rewind A's version.
    if (
      current.session.id !== incoming.id ||
      current.session.version > incoming.version
    )
      return false;
    if (
      current.session.version === incoming.version &&
      Date.parse(current.serverNow) > Date.parse(snapshot.serverNow)
    )
      return false;
  } else if (
    current &&
    (current !== before ||
      Date.parse(current.serverNow) > Date.parse(snapshot.serverNow))
  ) {
    // A fresh null is authoritative too (the old create may be an idempotent
    // replay). Do not resurrect it; refetch the active endpoint instead.
    return false;
  }
  queryClient.setQueryData(focusSessionQueryKey, activeFocusSnapshot(snapshot));
  return true;
}

async function captureFocusMutation(queryClient: QueryClient) {
  const cycleRevision = useFocusCycleStore.getState().revision;
  const {
    focusMinutes,
    breakMinutes,
    cycles,
    autoStartBreak,
    autoStartFocus,
    soundEnabled,
  } = useSettingsStore.getState();
  await queryClient.cancelQueries({ queryKey: focusSessionQueryKey });
  return {
    before:
      queryClient.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey),
    cycleRevision,
    settings: {
      focusMinutes,
      breakMinutes,
      cycles,
      autoStartBreak,
      autoStartFocus,
      soundEnabled,
    },
  };
}

function focusErrorNeedsRefresh(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 ||
      error.status === 409 ||
      error.code === "NETWORK_ERROR" ||
      error.code === "TIMEOUT")
  );
}

function focusCommandCanRetry(failureCount: number, error: unknown): boolean {
  return (
    failureCount < 2 &&
    error instanceof ApiError &&
    (error.code === "NETWORK_ERROR" || error.code === "TIMEOUT")
  );
}

export function useActiveFocusSessionQuery() {
  return useQuery({
    queryKey: focusSessionQueryKey,
    queryFn: ({ signal }) => getActiveFocusSession(signal),
    refetchInterval: (query) =>
      query.state.data?.session?.status === "active" ? 15_000 : false,
    refetchOnWindowFocus: true,
    retry: 2,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

export function useFocusSessionHistoryQuery(
  input: FocusSessionListParams,
  enabled = true,
) {
  return useQuery({
    queryKey: [...focusSessionHistoryQueryKey, input],
    queryFn: () => getFocusSessions(input),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useFocusReportQuery(input: FocusReportParams, enabled = true) {
  return useQuery({
    queryKey: [...focusReportQueryKey, input],
    queryFn: () => getFocusReport(input),
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useCreateFocusSession() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    onMutate: async (input: CreateFocusSessionInput) => {
      const mayReplay = attempt.current?.fingerprint === JSON.stringify(input);
      return { ...(await captureFocusMutation(queryClient)), mayReplay };
    },
    mutationFn: (input: CreateFocusSessionInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createFocusSession(input, attempt.current.key);
    },
    onSuccess: async (snapshot, _input, context) => {
      attempt.current = null;
      await queryClient.cancelQueries({ queryKey: focusSessionQueryKey });
      // Retrying creation can return a historical idempotent snapshot, even
      // after that Session ended within the same server-clock second. Read
      // the live slot before publishing/rebinding it; timestamps cannot prove it.
      const currentSnapshot = context?.mayReplay
        ? await getActiveFocusSession().catch(() => undefined)
        : snapshot;
      if (!currentSnapshot) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
        return;
      }
      const accepted = cacheFocusSnapshot(
        queryClient,
        currentSnapshot,
        context?.before,
      );
      const cycle = useFocusCycleStore.getState();
      const session = currentSnapshot.session;
      if (
        accepted &&
        session?.status === "active" &&
        session.id === snapshot.session?.id &&
        cycle.revision === context?.cycleRevision
      ) {
        cycle.beginWork(
          session.taskId,
          context.settings.cycles,
          session.taskTitle,
          session.id,
        );
      }
      await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

function useSimpleFocusCommand(
  command: (
    id: string,
    expectedVersion: number,
  ) => Promise<FocusSessionSnapshot>,
) {
  const queryClient = useQueryClient();
  return useMutation({
    onMutate: () => captureFocusMutation(queryClient),
    mutationFn: ({ id, expectedVersion }: FocusSessionCommandInput) =>
      command(id, expectedVersion),
    onSuccess: async (snapshot, _input, context) => {
      await queryClient.cancelQueries({ queryKey: focusSessionQueryKey });
      cacheFocusSnapshot(queryClient, snapshot, context?.before);
      await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

export function usePauseFocusSession() {
  return useSimpleFocusCommand(pauseFocusSession);
}

export function useResumeFocusSession() {
  return useSimpleFocusCommand(resumeFocusSession);
}

export function useRecoverFocusSession() {
  const queryClient = useQueryClient();
  return useMutation({
    onMutate: () => captureFocusMutation(queryClient),
    mutationFn: ({ id, action, expectedVersion }: RecoverFocusSessionInput) =>
      recoverFocusSession(id, action, expectedVersion),
    onSuccess: async (snapshot, _input, context) => {
      await queryClient.cancelQueries({ queryKey: focusSessionQueryKey });
      cacheFocusSnapshot(queryClient, snapshot, context?.before);
      if (snapshot.session?.status === "interrupted") {
        useFocusCycleStore.getState().resetCycle(snapshot.session.id);
        await invalidateFocusDependents(queryClient);
      } else {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
  });
}

function useIdempotentFocusEndCommand(
  command: (
    id: string,
    expectedVersion: number,
    idempotencyKey: string,
  ) => Promise<FocusSessionSnapshot>,
  completeCycle = false,
) {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    onMutate: () => captureFocusMutation(queryClient),
    mutationFn: ({ id, expectedVersion }: FocusSessionCommandInput) => {
      const fingerprint = `${id}:${expectedVersion}`;
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return command(id, expectedVersion, attempt.current.key);
    },
    onSuccess: async (snapshot, _input, context) => {
      attempt.current = null;
      await queryClient.cancelQueries({ queryKey: focusSessionQueryKey });
      const current =
        queryClient.getQueryData<FocusSessionSnapshot>(focusSessionQueryKey);
      const accepted = cacheFocusSnapshot(
        queryClient,
        snapshot,
        context?.before,
      );
      const session = snapshot.session;
      if (session && (accepted || !current?.session)) {
        const cycle = useFocusCycleStore.getState();
        if (completeCycle && context && session.status === "completed") {
          cycle.completeWork(
            session.taskId,
            context.settings,
            session.taskTitle,
            Date.now(),
            session.id,
          );
        } else {
          cycle.resetCycle(session.id);
        }
      }
      await invalidateFocusDependents(queryClient);
    },
    onError: async (error) => {
      if (focusErrorNeedsRefresh(error)) {
        await queryClient.invalidateQueries({ queryKey: focusSessionQueryKey });
      }
    },
    retry: focusCommandCanRetry,
    retryDelay: 500,
  });
}

export function useStopFocusSession(completeCycle = false) {
  return useIdempotentFocusEndCommand(stopFocusSession, completeCycle);
}

export function useCancelFocusSession() {
  return useIdempotentFocusEndCommand(cancelFocusSession);
}

export function useTodayStatsQuery(date: string) {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  return useQuery({
    queryKey: ["stats", "today", date, timezone],
    queryFn: () => getTodayStats(date, timezone),
    refetchInterval: 60_000,
    retry: 2,
    retryDelay: 500,
    staleTime: 10_000,
  });
}
