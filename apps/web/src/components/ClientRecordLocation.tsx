import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { ApiError } from "../api/client";
import { clientActivityQueryKey, clientFollowupQueryKey } from "../api/hooks";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type { ClientRecordKind } from "../lib/clientRecordLocation";
import { useAiChatStore } from "../store/aiChat";
import { ErrorState, LoadingState } from "./feedback";

export interface ClientRecordLocationProps {
  selectedId?: string;
  returnSession?: string | null;
  onClearSelection?: () => void;
}

export function ReturnToAiChat({
  sessionId,
  disabled = false,
  onNavigate,
}: {
  sessionId: string | null | undefined;
  disabled?: boolean;
  onNavigate?: () => void;
}) {
  if (!isWorkspaceIdentity(sessionId)) return null;
  if (disabled)
    return (
      <button className="button button-secondary" type="button" disabled>
        返回原对话
      </button>
    );
  return (
    <Link
      className="button button-secondary"
      to="/ai"
      onClick={(event) => {
        if (
          event.defaultPrevented ||
          event.button !== 0 ||
          event.ctrlKey ||
          event.metaKey ||
          event.shiftKey ||
          event.altKey
        )
          return;
        useAiChatStore.getState().setActiveSessionId(sessionId);
        onNavigate?.();
      }}
    >
      返回原对话
    </Link>
  );
}

// This is a direct, fresh read, not a scan of pages or a copy of the AI preview.
// The query key stays under the existing Client aggregate invalidation prefix.
export function ClientRecordLocation<
  T extends { id: string; clientId: string },
>({
  clientId,
  recordId,
  kind,
  load,
  children,
  onClose,
  returnSession,
  navigationDisabled = false,
}: {
  clientId: string;
  recordId: string;
  kind: ClientRecordKind;
  load: (id: string, signal?: AbortSignal) => Promise<T>;
  children: (record: T) => ReactNode;
  onClose?: () => void;
  returnSession?: string | null;
  navigationDisabled?: boolean;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const located = useRef(false);
  const query = useQuery({
    queryKey: [
      ...(kind === "activity"
        ? clientActivityQueryKey(clientId)
        : clientFollowupQueryKey(clientId)),
      "detail",
      recordId,
    ],
    queryFn: async ({ signal }) => {
      const record = await load(recordId, signal);
      if (record.id !== recordId || record.clientId !== clientId) {
        throw new ApiError("记录不属于当前客户。", {
          code: "CLIENT_RECORD_IDENTITY_MISMATCH",
        });
      }
      return record;
    },
    enabled: isWorkspaceIdentity(clientId) && isWorkspaceIdentity(recordId),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
  });
  useEffect(() => {
    if (query.isFetching || located.current) return;
    located.current = true;
    panel.current?.scrollIntoView?.({ block: "center" });
    panel.current?.focus({ preventScroll: true });
  }, [query.isFetching]);
  const name = kind === "activity" ? "客户活动" : "客户回访";
  const mismatch =
    query.error instanceof ApiError &&
    query.error.code === "CLIENT_RECORD_IDENTITY_MISMATCH";
  return (
    <div
      className="client-record-location"
      ref={panel}
      tabIndex={-1}
      aria-label={`定位的${name}`}
    >
      <div className="client-record-location-heading">
        <div>
          <strong>当前定位的{name}</strong>
          <p>
            按记录 ID
            读取最新事实，不受下方列表筛选或分页影响；查看不会执行操作。
          </p>
        </div>
        <ReturnToAiChat
          sessionId={returnSession}
          disabled={navigationDisabled}
        />
        {onClose ? (
          <button
            className="button button-quiet"
            type="button"
            disabled={navigationDisabled}
            onClick={onClose}
          >
            关闭定位
          </button>
        ) : null}
      </div>
      {query.isFetching || query.isPending ? (
        <LoadingState label={`正在读取${name}…`} />
      ) : query.isError ? (
        <ErrorState
          compact
          title={`${name}不可用`}
          message={
            mismatch
              ? "链接中的记录不属于当前客户，未展示其内容。"
              : "记录可能已不存在，或本地服务暂不可用；不会用列表中的其他记录替代。"
          }
          onRetry={() => void query.refetch()}
        />
      ) : query.data ? (
        children(query.data)
      ) : null}
    </div>
  );
}
