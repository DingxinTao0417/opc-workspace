import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Clock3,
  History,
  LoaderCircle,
  Timer,
} from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError } from "../api/client";
import { useFocusSessionHistoryQuery } from "../api/hooks";
import { formatFocusTime } from "../store/focus";
import type { FocusSessionStatus } from "../types/models";

interface TaskFocusHistorySectionProps {
  taskId: string;
}

const pageSize = 5;

function statusLabel(status: FocusSessionStatus): string {
  if (status === "completed") return "已计入工时";
  if (status === "cancelled") return "已取消";
  return "意外中断";
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function queryError(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "专注记录读取失败；任务其他内容不受影响。";
}

export function TaskFocusHistorySection({
  taskId,
}: TaskFocusHistorySectionProps) {
  const [expanded, setExpanded] = useState(false);
  const [page, setPage] = useState(1);
  const query = useFocusSessionHistoryQuery(
    { page, pageSize, status: "terminal", taskId },
    expanded,
  );
  const total = query.data?.meta.total ?? 0;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  useEffect(() => setPage(1), [taskId]);

  return (
    <section
      aria-labelledby="task-focus-history-heading"
      className="task-focus-history"
    >
      <div className="task-focus-history-heading">
        <div>
          <h3 id="task-focus-history-heading">专注记录</h3>
          <p>完成记录会计入任务工时；取消或中断只保留审计事实。</p>
        </div>
        <button
          aria-expanded={expanded}
          className="button button-quiet"
          onClick={() => {
            setExpanded((value) => !value);
            setPage(1);
          }}
          type="button"
        >
          <History size={13} />
          {expanded ? "收起" : "查看记录"}
        </button>
      </div>

      {expanded ? (
        <div className="task-focus-history-panel">
          {query.isPending ? (
            <div className="task-focus-history-state">
              <LoaderCircle className="spin" size={14} /> 正在读取专注记录…
            </div>
          ) : query.isError ? (
            <div className="task-focus-history-error" role="alert">
              <AlertTriangle size={14} />
              <span>{queryError(query.error)}</span>
              <button
                className="form-inline-action"
                onClick={() => void query.refetch()}
                type="button"
              >
                重试
              </button>
            </div>
          ) : query.data?.items.length ? (
            <>
              <ol className="task-focus-history-list">
                {query.data.items.map((session) => (
                  <li key={session.id}>
                    <span className="task-focus-history-icon">
                      <Timer aria-hidden="true" size={13} />
                    </span>
                    <div>
                      <strong>{statusLabel(session.status)}</strong>
                      <small>
                        <Clock3 size={11} />
                        {formatTime(session.endedAt ?? session.updatedAt)}
                      </small>
                    </div>
                    <b>{formatFocusTime(session.accumulatedSeconds)}</b>
                  </li>
                ))}
              </ol>
              {total > pageSize ? (
                <div className="task-focus-history-pagination">
                  <button
                    aria-label="上一页任务专注记录"
                    className="icon-button"
                    disabled={page === 1 || query.isFetching}
                    onClick={() => setPage((value) => value - 1)}
                    type="button"
                  >
                    <ChevronLeft size={14} />
                  </button>
                  <span>
                    {page} / {pageCount} · 共 {total} 条
                  </span>
                  <button
                    aria-label="下一页任务专注记录"
                    className="icon-button"
                    disabled={page >= pageCount || query.isFetching}
                    onClick={() => setPage((value) => value + 1)}
                    type="button"
                  >
                    <ChevronRight size={14} />
                  </button>
                </div>
              ) : null}
            </>
          ) : (
            <div className="task-focus-history-state">
              该任务还没有终态专注记录。
            </div>
          )}
        </div>
      ) : null}
    </section>
  );
}
