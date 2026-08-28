import {
  AlertTriangle,
  BellOff,
  BellRing,
  Check,
  Clock3,
  Edit3,
  Eye,
  FileCheck2,
  History,
  Inbox,
  LoaderCircle,
  Link2,
  ListChecks,
  RotateCcw,
  Unlink2,
  XCircle,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useMemo } from "react";
import { ApiError } from "../api/client";
import { useInboxItemEventsQuery } from "../api/hooks";
import type { InboxWorkflowEvent } from "../types/models";

interface EventPresentation {
  icon: LucideIcon;
  label: string;
}

const eventPresentations: Record<string, EventPresentation> = {
  inbox_item_created: { icon: Inbox, label: "创建收件箱条目" },
  inbox_item_updated: { icon: Edit3, label: "更新条目资料" },
  inbox_item_read: { icon: Eye, label: "标记为已读" },
  inbox_item_snoozed: { icon: BellOff, label: "设置稍后处理" },
  inbox_item_unsnoozed: { icon: BellRing, label: "恢复到待处理" },
  inbox_item_resolved: { icon: Check, label: "标记为已解决" },
  inbox_item_dismissed: { icon: XCircle, label: "忽略并归档" },
  inbox_item_reopened: { icon: RotateCcw, label: "重新打开" },
  task_linked: { icon: Link2, label: "关联任务" },
  task_requirement_changed: { icon: ListChecks, label: "调整任务要求" },
  task_unlinked: { icon: Unlink2, label: "解除任务关联" },
  created: { icon: Inbox, label: "创建收件箱条目" },
  updated: { icon: Edit3, label: "更新条目资料" },
  read: { icon: Eye, label: "标记为已读" },
  snoozed: { icon: BellOff, label: "设置稍后处理" },
  unsnoozed: { icon: BellRing, label: "恢复到待处理" },
  resolved: { icon: Check, label: "标记为已解决" },
  dismissed: { icon: XCircle, label: "忽略并归档" },
  reopened: { icon: RotateCcw, label: "重新打开" },
  source_projected: { icon: FileCheck2, label: "从业务来源创建" },
  source_deleted: { icon: AlertTriangle, label: "业务来源已删除" },
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

function snapshotString(
  snapshot: Record<string, unknown> | null,
  ...keys: string[]
): string | null {
  for (const key of keys) {
    const value = snapshot?.[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  return null;
}

function eventDetail(event: InboxWorkflowEvent): string | null {
  const nestedRelation =
    event.current?.relation &&
    typeof event.current.relation === "object" &&
    !Array.isArray(event.current.relation)
      ? (event.current.relation as Record<string, unknown>)
      : null;
  const taskTitle = snapshotString(
    nestedRelation ?? event.current,
    "task_title_snapshot",
    "task_title",
    "title",
  );
  if (event.action === "task_linked") {
    const isRequired =
      nestedRelation?.is_required ??
      nestedRelation?.isRequired ??
      event.current?.is_required ??
      event.current?.isRequired;
    return taskTitle
      ? `${taskTitle}${typeof isRequired === "boolean" ? ` · ${isRequired ? "必需" : "可选"}` : ""}`
      : null;
  }
  if (event.action === "task_requirement_changed") {
    const isRequired =
      nestedRelation?.is_required ??
      nestedRelation?.isRequired ??
      event.current?.is_required ??
      event.current?.isRequired;
    if (typeof isRequired === "boolean") {
      return `${taskTitle ? `${taskTitle} · ` : ""}${isRequired ? "设为必需任务" : "改为可选任务"}`;
    }
  }
  const reason =
    event.reason ??
    snapshotString(
      event.current,
      "reason",
      "resolution_reason",
      "dismiss_reason",
    );
  if (reason) {
    return event.action === "task_unlinked" && taskTitle
      ? `${taskTitle} · 原因：${reason}`
      : `原因：${reason}`;
  }
  const snoozedUntil = snapshotString(
    event.current,
    "snoozed_until",
    "snoozedUntil",
  );
  if (!snoozedUntil) return null;
  const date = new Date(snoozedUntil);
  return Number.isNaN(date.getTime())
    ? `稍后至 ${snoozedUntil}`
    : `稍后至 ${formatEventTime(snoozedUntil)}`;
}

function queryErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "活动记录读取失败；条目详情仍可继续查看。";
}

function EventRow({ event }: { event: InboxWorkflowEvent }) {
  const presentation = eventPresentations[event.action] ?? {
    icon: History,
    label: "收件箱记录已更新",
  };
  const Icon = presentation.icon;
  const detail = eventDetail(event);
  return (
    <li>
      <span className="task-event-icon">
        <Icon aria-hidden="true" size={13} />
      </span>
      <div>
        <div className="task-event-title">
          <strong>{presentation.label}</strong>
        </div>
        <p>
          <Clock3 size={11} />
          {formatEventTime(event.createdAt)}
          {event.actor ? ` · ${event.actor.displayName}` : " · 系统记录"}
        </p>
        {detail ? <small title={detail}>{detail}</small> : null}
      </div>
    </li>
  );
}

export function InboxItemEventsSection({ itemId }: { itemId: string }) {
  const query = useInboxItemEventsQuery(itemId, { pageSize: 20 });
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

  return (
    <section
      aria-labelledby="inbox-events-heading"
      className="task-events inbox-events"
    >
      <div className="task-events-heading">
        <div>
          <h3 id="inbox-events-heading">活动时间线</h3>
          <p>操作按发生顺序追加保存，不替代条目的当前状态。</p>
        </div>
      </div>
      <div className="task-events-panel">
        {query.isPending ? (
          <div className="task-events-state">
            <LoaderCircle className="animate-spin" size={14} />
            正在读取活动记录…
          </div>
        ) : null}
        {query.isError ? (
          <div className="task-events-error" role="alert">
            <AlertTriangle size={14} />
            <span>{queryErrorMessage(query.error)}</span>
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
    </section>
  );
}
