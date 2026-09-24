import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, type ReactNode } from "react";
import { ApiError, getProjectNote } from "../api/client";
import { projectNoteQueryKey } from "../api/hooks";
import { isWorkspaceIdentity } from "../lib/focusReportLocation";
import type { ProjectNote } from "../types/models";
import { ReturnToAiChat } from "./ClientRecordLocation";
import { ErrorState, LoadingState } from "./feedback";

export function ProjectNoteLocation({
  projectId,
  noteId,
  children,
  onClose,
  returnSession,
}: {
  projectId: string;
  noteId: string;
  children: (note: ProjectNote) => ReactNode;
  onClose?: () => void;
  returnSession?: string | null;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const located = useRef(false);
  const query = useQuery({
    queryKey: [...projectNoteQueryKey(projectId), "detail", noteId],
    queryFn: async ({ signal }) => {
      const note = await getProjectNote(noteId, signal);
      if (note.id !== noteId || note.projectId !== projectId) {
        throw new ApiError("笔记不属于当前项目。", {
          code: "PROJECT_NOTE_IDENTITY_MISMATCH",
        });
      }
      return note;
    },
    enabled: isWorkspaceIdentity(projectId) && isWorkspaceIdentity(noteId),
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnMount: "always",
  });

  useEffect(() => {
    located.current = false;
  }, [noteId, projectId]);

  useEffect(() => {
    if (query.isFetching || located.current) return;
    located.current = true;
    panel.current?.scrollIntoView?.({ block: "center" });
    panel.current?.focus({ preventScroll: true });
  }, [query.isFetching]);

  const mismatch =
    query.error instanceof ApiError &&
    query.error.code === "PROJECT_NOTE_IDENTITY_MISMATCH";
  return (
    <div
      aria-label="定位的项目笔记"
      className="project-note-location"
      ref={panel}
      tabIndex={-1}
    >
      <div className="client-record-location-heading">
        <div>
          <strong>当前定位的项目笔记</strong>
          <p>
            按项目与笔记 ID
            读取最新事实，不受下方列表筛选或分页影响；查看不会执行操作。
          </p>
        </div>
        <ReturnToAiChat sessionId={returnSession} />
        {onClose ? (
          <button
            className="button button-quiet"
            onClick={onClose}
            type="button"
          >
            关闭定位
          </button>
        ) : null}
      </div>
      {query.isFetching || query.isPending ? (
        <LoadingState label="正在读取项目笔记…" />
      ) : query.isError ? (
        <ErrorState
          compact
          title="项目笔记不可用"
          message={
            mismatch
              ? "链接中的笔记不属于当前项目，未展示其内容。"
              : "笔记可能已不存在，或本地服务暂不可用；不会用列表中的其他笔记替代。"
          }
          onRetry={() => void query.refetch()}
        />
      ) : query.data ? (
        children(query.data)
      ) : null}
    </div>
  );
}
