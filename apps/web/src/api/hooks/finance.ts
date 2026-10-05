import type {
  FinancialEntryInput,
  FinancialEntryListParams,
  IncomeStatsParams,
  UpdateFinancialEntryInput,
  VoidFinancialEntryInput,
} from "../../types/models";
import {
  ApiError,
  createFinancialEntry,
  downloadFinancialEntriesCSV,
  getFinancialEntries,
  getFinancialEntry,
  getIncomeStats,
  updateFinancialEntry,
  voidFinancialEntry,
} from "../client";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateFinancialReadModels } from "./invalidation";
import {
  financialEntryDetailQueryKey,
  financialEntryQueryKey,
  incomeStatsQueryKey,
} from "./queryKeys";

export function useFinancialEntriesQuery(
  input: FinancialEntryListParams = {},
  enabled = true,
  fresh = false,
) {
  return useQuery({
    queryKey: [...financialEntryQueryKey, "list", input],
    queryFn: ({ signal }) => getFinancialEntries(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: fresh ? 0 : 10_000,
    ...(fresh ? { refetchOnMount: "always" as const } : {}),
  });
}

export function useFinancialEntryQuery(id: string | null, fresh = false) {
  return useQuery({
    queryKey: financialEntryDetailQueryKey(id ?? "missing"),
    queryFn: ({ signal }) => getFinancialEntry(id!, signal),
    enabled: Boolean(id),
    retry: 1,
    ...(fresh
      ? { refetchOnMount: "always" as const, staleTime: 0, gcTime: 0 }
      : {}),
  });
}

export function useIncomeStatsQuery(
  input: IncomeStatsParams,
  enabled = true,
  fresh = false,
) {
  return useQuery({
    queryKey: [...incomeStatsQueryKey, input],
    queryFn: ({ signal }) => getIncomeStats(input, signal),
    enabled,
    placeholderData: keepPreviousData,
    retry: 2,
    retryDelay: 500,
    staleTime: fresh ? 0 : 10_000,
    ...(fresh ? { refetchOnMount: "always" as const } : {}),
  });
}

export function useCreateFinancialEntry() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: FinancialEntryInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createFinancialEntry(input, attempt.current.key);
    },
    onSuccess: async (entry) => {
      attempt.current = null;
      queryClient.setQueryData(financialEntryDetailQueryKey(entry.id), entry);
      await invalidateFinancialReadModels(queryClient);
    },
  });
}

export function useUpdateFinancialEntry() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateFinancialEntryInput;
    }) => updateFinancialEntry(id, input),
    onSuccess: async (entry) => {
      queryClient.setQueryData(financialEntryDetailQueryKey(entry.id), entry);
      await invalidateFinancialReadModels(queryClient);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateFinancialReadModels(queryClient);
      }
    },
  });
}

export function useVoidFinancialEntry() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: VoidFinancialEntryInput;
    }) => voidFinancialEntry(id, input),
    onSuccess: async (entry) => {
      queryClient.setQueryData(financialEntryDetailQueryKey(entry.id), entry);
      await invalidateFinancialReadModels(queryClient);
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await invalidateFinancialReadModels(queryClient);
      }
    },
  });
}

export function useExportFinancialEntries() {
  return useMutation({
    mutationFn: (input: FinancialEntryListParams) =>
      downloadFinancialEntriesCSV(input),
  });
}
