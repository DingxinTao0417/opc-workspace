import type { AppSettingUpdate, SearchListParams } from "../../types/models";
import {
  ApiError,
  commitAppSettingsWithAvatar,
  getAppSettings,
  getHealth,
  getSearchResults,
  updateAppSettings,
} from "../client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { searchQueryKey, settingsQueryKey } from "./queryKeys";

export function useSearchQuery(input: SearchListParams, enabled = true) {
  return useQuery({
    queryKey: [...searchQueryKey, input],
    queryFn: ({ signal }) => getSearchResults(input, signal),
    enabled,
    retry: 1,
    retryDelay: 500,
    staleTime: 5_000,
  });
}

export function useAppSettingsQuery(enabled = true) {
  return useQuery({
    queryKey: settingsQueryKey,
    queryFn: getAppSettings,
    enabled,
    retry: 2,
    retryDelay: 500,
    staleTime: 30_000,
  });
}

export function useUpdateAppSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (updates: AppSettingUpdate[]) => updateAppSettings(updates),
    onSuccess: (settings) => {
      queryClient.setQueryData(settingsQueryKey, settings);
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        error.code === "SETTINGS_VERSION_CONFLICT"
      ) {
        await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
      }
    },
  });
}

export function useCommitAppSettingsWithAvatar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      operation: "replace" | "remove";
      updates: AppSettingUpdate[];
      file?: File;
    }) =>
      commitAppSettingsWithAvatar(input.operation, input.updates, input.file),
    onSuccess: (settings) => {
      queryClient.setQueryData(settingsQueryKey, settings);
    },
    onError: async (error) => {
      if (
        error instanceof ApiError &&
        error.code === "SETTINGS_VERSION_CONFLICT"
      ) {
        await queryClient.invalidateQueries({ queryKey: settingsQueryKey });
      }
    },
  });
}

export function useHealthQuery(enabled = true) {
  return useQuery({
    queryKey: ["health"],
    queryFn: getHealth,
    enabled,
    retry: false,
    refetchInterval: 15_000,
  });
}
