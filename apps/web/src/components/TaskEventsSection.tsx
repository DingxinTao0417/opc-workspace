import {
  AlertTriangle,
  Check,
  CirclePlay,
  Clock3,
  History,
  LoaderCircle,
  RotateCcw,
  ShieldAlert,
  UserRound,
  UserRoundCog,
  XCircle,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { ApiError } from "../api/client";
import { useTaskEventsQuery } from "../api/hooks";
import type { TaskStatus, TaskWorkflowEvent } from "../types/models";

interface TaskEventsSectionProps {
  taskId: string;
}

interface EventPresentation {
  icon: LucideIcon;
  label: string;
}

const eventPresentations: Record<string, EventPresentation> = {
  task_started: { icon: CirclePlay, label: "开始执行任务" },
  task_blocked: { icon: ShieldAlert, label: "将任务标记为阻塞" },
  task_unblocked: { icon: RotateCcw, label: "解除任务阻塞" },
  task_completed: { icon: Check, label: "完成任务" },
  task_cancelled: { icon: XCircle, label: "取消任务" },
  task_reopened: { icon: RotateCcw, label: "重新打开任务" },
  assignment_created: { icon: UserRound, label: "创建责任分派" },
  assignment_reassigned: { icon: UserRoundCog, label: "改派责任人" },
  assignment_ended: { icon: UserRoundCog, label: "结束责任分派" },
  migration_assignment_backfill: {
    icon: History,
    label: "迁移推定历史负责人",
  },
};

const statusLabels: Partial<Record<TaskStatus, string>> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

const internalReasonLabels: Record<string, string> = {
  "Task completed": "任务完成后自动结束",
  "Task cancelled": "任务取消后自动结束",
  schema_v7_migration_inferred_owner: "历史责任记录由迁移推定",
};

function formatEventTime(value: string): string {
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

function eventQueryErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "活动记录读取失败；任务详情仍可继续查看。";
}

function eventReason(event: TaskWorkflowEvent): string | null {
  if (event.action === "migration_assignment_backfill") {
    return "历史责任记录由迁移推定";
  }
  if (!event.reason) return null;
  return internalReasonLabels[event.reason] ?? event.reason;
}

function snapshotStatus(
  snapshot: Record<string, unknown> | null,
): TaskStatus | null {
  const status = snapshot?.status;
  return typeof status === "string" && status in statusLabels
    ? (status as TaskStatus)
    : null;
}

function EventRow({ event }: { event: TaskWorkflowEvent }) {
  const presentation = eventPresentations[event.action] ?? {
    icon: History,
    label: "任务记录已更新",
  };
  const Icon = presentation.icon;
  const previousStatus = snapshotStatus(event.previous);
  const currentStatus = snapshotStatus(event.current);
  const reason = eventReason(event);
  return (
    <li>
      <span className="task-event-icon">
        <Icon aria-hidden="true" size={13} />
      </span>
      <div>
        <div className="task-event-title">
          <strong>{presentation.label}</strong>
          {event.action === "migration_assignment_backfill" ? (
            <em>迁移推定</em>
          ) : null}
        </div>
        <p>
          <Clock3 size={11} />
          {formatEventTime(event.createdAt)}
          {event.actor ? ` · ${event.actor.displayName}` : " · 系统记录"}
        </p>
        {previousStatus && currentStatus && previousStatus !== currentStatus ? (
          <small>
            {statusLabels[previousStatus]} → {statusLabels[currentStatus]}
          </small>
        ) : null}
        {reason ? <small title={reason}>原因：{reason}</small> : null}
      </div>
    </li>
  );
}

export function TaskEventsSection({ taskId }: TaskEventsSectionProps) {
  const [expanded, setExpanded] = useState(false);
  const query = useTaskEventsQuery(taskId, { pageSize: 20 }, expanded);
  const events = useMemo(() => {
    const seen = new Set<string>();
    return (query.data?.pages ?? [])
      .flatMap((page) => page.items)
      .filter((event) => {
        if (seen.has(event.id)) return false;
        seen.add(event.id);
        return true;
      });
  }, [query.data?.pages]);
  const total = query.data?.pages[0]?.meta.total ?? 0;

  return (
    <section aria-labelledby="task-events-heading" className="task-events">
      <div className="task-events-heading">
        <div>
          <h3 id="task-events-heading">活动时间线</h3>
          <p>状态与责任变化按发生顺序保留，不作为任务状态的第二副本。</p>
        </div>
        <button
          aria-expanded={expanded}
          className="button button-quiet"
          onClick={() => setExpanded((value) => !value)}
          type="button"
        >
          <History size={13} />
          {expanded ? "收起" : total > 0 ? `查看 ${total} 条` : "查看记录"}
        </button>
      </div>

      {expanded ? (
        <div className="task-events-panel">
          {query.isPending ? (
            <div className="task-events-state">
              <LoaderCircle className="spin" size={14} /> 正在读取活动记录…
            </div>
          ) : null}
          {query.isError ? (
            <div className="task-events-error" role="alert">
              <AlertTriangle size={14} />
              <span>{eventQueryErrorMessage(query.error)}</span>
              <button
                className="form-inline-action"
                onClick={() => void query.refetch()}
                type="button"
              >
                重试
              </button>
            </div>
          ) : null}
          {!query.isPending && !query.isError && events.length === 0 ? (
            <div className="task-events-state">暂无活动记录</div>
          ) : null}
          {events.length > 0 ? (
            <ol className="task-event-timeline">
              {events.map((event) => (
                <EventRow event={event} key={event.id} />
              ))}
            </ol>
          ) : null}
          {query.hasNextPage ? (
            <button
              className="button button-secondary task-events-load-more"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
              type="button"
            >
              {query.isFetchingNextPage ? "正在读取…" : "加载更早"}
            </button>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
