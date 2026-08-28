import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useLayoutEffect, useState, type ReactNode } from "react";
import { ApiError } from "../api/client";
import { settingsQueryKey } from "../api/hooks";
import { bootstrapAppSettings } from "../settings/bootstrap";
import { clearLegacySettings, useSettingsStore } from "../store/settings";
import { ErrorState, LoadingState } from "./feedback";

function settingsBootstrapError(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.message}${error.requestId ? ` · 请求 ${error.requestId}` : ""}`;
  }
  return "无法读取本地设置，请确认 Sidecar 已启动后重试。";
}

export function SettingsBootstrap({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const replaceCommitted = useSettingsStore((state) => state.replaceCommitted);
  const [appliedAt, setAppliedAt] = useState(0);
  const query = useQuery({
    queryKey: [...settingsQueryKey, "bootstrap"],
    queryFn: bootstrapAppSettings,
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });

  useLayoutEffect(() => {
    if (!query.data || query.dataUpdatedAt === appliedAt) return;
    replaceCommitted(query.data.committed);
    queryClient.setQueryData(settingsQueryKey, query.data.settings);
    if (query.data.legacyExists) clearLegacySettings();
    setAppliedAt(query.dataUpdatedAt);
  }, [
    appliedAt,
    query.data,
    query.dataUpdatedAt,
    queryClient,
    replaceCommitted,
  ]);

  if (query.isPending || (query.data && appliedAt !== query.dataUpdatedAt)) {
    return (
      <main className="settings-bootstrap-shell">
        <LoadingState label="正在读取本地设置…" />
      </main>
    );
  }
  if (query.isError) {
    return (
      <main className="settings-bootstrap-shell">
        <ErrorState
          message={settingsBootstrapError(query.error)}
          onRetry={() => void query.refetch()}
          title="本地设置加载失败"
        />
      </main>
    );
  }
  return children;
}
