import { CalendarClock, CircleCheck, CircleOff, Clock3 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useClientFollowupsQuery } from "../api/hooks";
import type {
  ClientFollowup,
  ClientFollowupPriority,
  ClientFollowupStatus,
} from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

const statusLabel: Record<ClientFollowupStatus, string> = {
  planned: "待回访",
  completed: "已完成",
  skipped: "已跳过",
  cancelled: "已取消",
};

const priorityLabel: Record<ClientFollowupPriority, string> = {
  low: "低优先级",
  normal: "普通优先级",
  high: "高优先级",
};

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function isOverdue(followup: ClientFollowup): boolean {
  return (
    followup.status === "planned" &&
    new Date(followup.scheduledAt).getTime() < Date.now()
  );
}

function detailCopy(followup: ClientFollowup): string | null {
  if (followup.status === "completed") {
    return followup.result
      ? `结果：${followup.result}`
      : "已完成，暂未记录结果。";
  }
  if (followup.status === "skipped") {
    return followup.skipReason
      ? `跳过原因：${followup.skipReason}`
      : "已跳过。";
  }
  if (followup.status === "cancelled") {
    return followup.cancelReason
      ? `取消原因：${followup.cancelReason}`
      : "已取消。";
  }
  return followup.notes;
}

export function ClientFollowupsSection({ clientId }: { clientId: string }) {
  const [page, setPage] = useState(1);
  const queryInput = useMemo(() => ({ page, pageSize: 6 }), [page]);
  const query = useClientFollowupsQuery(clientId, queryInput);
  const items = query.data?.items ?? [];
  const totalPages = Math.max(
    1,
    Math.ceil((query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 6)),
  );

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  return (
    <section className="project-detail-section client-followups-section">
      <div className="project-detail-heading">
        <div>
          <h2>客户回访</h2>
          <p>
            计划、到期提醒和历史均保存在本机；当前可查看时间线，创建和处理入口将在下一页面纵切提供。
          </p>
        </div>
        <span>{query.data?.meta.total ?? 0} 项</span>
      </div>

      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取客户回访计划。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          message="客户回访计划创建后，会在这里按计划时间显示；到期项会同步投影到本地收件箱。"
          title="暂无客户回访"
        />
      ) : null}
      {items.length > 0 ? (
        <div className="client-followup-list">
          {items.map((followup) => {
            const overdue = isOverdue(followup);
            const Icon =
              followup.status === "completed"
                ? CircleCheck
                : followup.status === "planned"
                  ? Clock3
                  : CircleOff;
            const copy = detailCopy(followup);
            return (
              <article
                className={overdue ? "is-overdue" : undefined}
                key={followup.id}
              >
                <span className="client-followup-icon" aria-hidden="true">
                  <Icon size={14} />
                </span>
                <div className="client-followup-copy">
                  <div>
                    <strong>{followup.purpose}</strong>
                    <span>
                      {overdue ? "已逾期" : statusLabel[followup.status]}
                    </span>
                    <span
                      className={`client-followup-priority ${followup.priority}`}
                    >
                      {priorityLabel[followup.priority]}
                    </span>
                  </div>
                  {copy ? <p>{copy}</p> : null}
                  <small>
                    <CalendarClock size={11} />{" "}
                    {formatTime(followup.scheduledAt)} · {followup.channel} ·
                    负责人 {followup.assignedActorName}
                  </small>
                  {followup.nextStep ? (
                    <small className="client-followup-next-step">
                      下一步：{followup.nextStep}
                    </small>
                  ) : null}
                </div>
              </article>
            );
          })}
        </div>
      ) : null}
      {totalPages > 1 ? (
        <nav aria-label="客户回访分页" className="pagination">
          <button
            className="button button-secondary"
            disabled={page <= 1 || query.isFetching}
            onClick={() => setPage((value) => Math.max(1, value - 1))}
            type="button"
          >
            上一页
          </button>
          <span>
            第 {page} / {totalPages} 页
          </span>
          <button
            className="button button-secondary"
            disabled={page >= totalPages || query.isFetching}
            onClick={() => setPage((value) => value + 1)}
            type="button"
          >
            下一页
          </button>
        </nav>
      ) : null}
    </section>
  );
}
