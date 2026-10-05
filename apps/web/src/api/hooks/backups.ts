import type {
  BusinessImportResult,
  BusinessPackageImportResult,
  CreateBackupInput,
  UpdateScheduledBackupPolicyInput,
} from "../../types/models";
import {
  ApiError,
  applyBusinessDataImport,
  applyBusinessPackageImport,
  createBackup,
  deleteBackup,
  downloadBackupArchive,
  downloadBusinessDataExport,
  downloadBusinessPackage,
  downloadDiagnosticPackage,
  drillBackupRestore,
  getBackups,
  getRestoreDiagnostics,
  getScheduledBackupPolicy,
  getStorageCapacity,
  getStorageCapacityHistory,
  previewBusinessDataImport,
  previewBusinessPackageImport,
  scheduleBackupRestore,
  updateScheduledBackupPolicy,
  verifyBackup,
} from "../client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { invalidateBackupIncident } from "./invalidation";
import {
  backupQueryKey,
  restoreDiagnosticsQueryKey,
  scheduledBackupPolicyQueryKey,
  storageCapacityHistoryQueryKey,
  storageCapacityQueryKey,
} from "./queryKeys";

export function useBackupsQuery(enabled = true) {
  return useQuery({
    queryKey: backupQueryKey,
    queryFn: getBackups,
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useScheduledBackupPolicyQuery(enabled = true) {
  return useQuery({
    queryKey: scheduledBackupPolicyQueryKey,
    queryFn: getScheduledBackupPolicy,
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useUpdateScheduledBackupPolicy() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateScheduledBackupPolicyInput) =>
      updateScheduledBackupPolicy(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
        await queryClient.invalidateQueries({
          queryKey: scheduledBackupPolicyQueryKey,
        });
      }
    },
  });
}

export function useRestoreDiagnosticsQuery(enabled = true) {
  return useQuery({
    queryKey: restoreDiagnosticsQueryKey,
    queryFn: getRestoreDiagnostics,
    enabled,
  });
}

export function useStorageCapacityQuery(enabled = true) {
  return useQuery({
    queryKey: storageCapacityQueryKey,
    queryFn: getStorageCapacity,
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 10_000,
  });
}

export function useStorageCapacityHistoryQuery(enabled = true, days = 7) {
  return useQuery({
    queryKey: [...storageCapacityHistoryQueryKey, days],
    queryFn: () => getStorageCapacityHistory(days),
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 60_000,
  });
}

export function useCreateBackup() {
  const queryClient = useQueryClient();
  const attempt = useRef<{ fingerprint: string; key: string } | null>(null);
  return useMutation({
    mutationFn: (input: CreateBackupInput) => {
      const fingerprint = JSON.stringify(input);
      if (!attempt.current || attempt.current.fingerprint !== fingerprint) {
        attempt.current = { fingerprint, key: crypto.randomUUID() };
      }
      return createBackup(input, attempt.current.key);
    },
    onSuccess: async () => {
      attempt.current = null;
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
    onError: async (error) => invalidateBackupIncident(queryClient, error),
  });
}

export function useVerifyBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => verifyBackup(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
    onError: async (error) => invalidateBackupIncident(queryClient, error),
  });
}

export function useDrillBackupRestore() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => drillBackupRestore(id),
    onError: async (error) => invalidateBackupIncident(queryClient, error),
  });
}

export function useScheduleBackupRestore() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => scheduleBackupRestore(id),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: backupQueryKey }),
        queryClient.invalidateQueries({ queryKey: restoreDiagnosticsQueryKey }),
      ]);
    },
    onError: async (error) => invalidateBackupIncident(queryClient, error),
  });
}

export function useDeleteBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteBackup(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: backupQueryKey });
    },
  });
}

export function useDownloadBackupArchive() {
  return useMutation({ mutationFn: (id: string) => downloadBackupArchive(id) });
}

export function useExportBusinessData() {
  return useMutation({ mutationFn: downloadBusinessDataExport });
}

export function useExportBusinessPackage() {
  return useMutation({ mutationFn: downloadBusinessPackage });
}

export function usePreviewBusinessDataImport() {
  return useMutation({ mutationFn: previewBusinessDataImport });
}

// An import replaces the settings too. Refreshing them goes through the
// settings bootstrap, which remounts the app, so a component that started the
// import is gone before the mutation settles and never sees its own result.
// onApplied runs before anything is refreshed.
export function useApplyBusinessDataImport(options?: {
  onApplied?: (result: BusinessImportResult) => void;
}) {
  const queryClient = useQueryClient();
  const onApplied = options?.onApplied;
  return useMutation({
    mutationFn: applyBusinessDataImport,
    onSuccess: async (result) => {
      onApplied?.(result);
      await queryClient.invalidateQueries();
    },
  });
}

export function usePreviewBusinessPackageImport() {
  return useMutation({ mutationFn: previewBusinessPackageImport });
}

export function useApplyBusinessPackageImport(options?: {
  onApplied?: (result: BusinessPackageImportResult) => void;
}) {
  const queryClient = useQueryClient();
  const onApplied = options?.onApplied;
  return useMutation({
    mutationFn: applyBusinessPackageImport,
    onSuccess: async (result) => {
      onApplied?.(result);
      await queryClient.invalidateQueries();
    },
  });
}

export function useDownloadDiagnosticPackage() {
  return useMutation({ mutationFn: downloadDiagnosticPackage });
}
