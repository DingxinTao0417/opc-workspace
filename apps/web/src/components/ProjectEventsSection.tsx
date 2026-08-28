import {
  AlertTriangle,
  Archive,
  Check,
  CirclePause,
  CirclePlay,
  Clock3,
  Edit3,
  History,
  LoaderCircle,
  RotateCcw,
  Trash2,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { ApiError } from "../api/client";
import { useProjectEventsQuery } from "../api/hooks";
import type { ProjectStatus, ProjectWorkflowEvent } from "../types/models";

interface ProjectEventsSectionProps {
  projectId: string;
}

interface EventPresentation {
  icon: LucideIcon;
  label: string;
}

const eventPresentations: Record<string, EventPresentation> = {
  project_created: { icon: CirclePlay, label: "创建项目" },
  project_updated: { icon: Edit3, label: "修改项目资料" },
  project_started: { icon: CirclePlay, label: "开始项目" },
  project_paused: { icon: CirclePause, label: "暂停项目" },
  project_resumed: { icon: CirclePlay, label: "继续项目" },
  project_completed: { icon: Check, label: "完成项目" },
  project_reopened: { icon: RotateCcw, label: "重新打开项目" },
  project_archived: { icon: Archive, label: "归档项目" },
  project_restored: { icon: RotateCcw, label: "恢复项目" },
  project_deleted: { icon: Trash2, label: "永久删除项目" },
};

const statusLabels: Record<ProjectStatus, string> = {
  planning: "规划中",
  in_progress: "进行中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
};

const fieldLabels: Record<string, string> = {
  name: "名称",
  description: "说明",
  client_id: "客户",
  start_date: "开始日期",
  due_date: "截止日期",
  amount_minor: "合同金额",
  color: "颜色",
};

function formatEventTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function queryErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "活动记录读取失败；项目其他内容不受影响。";
}

function snapshotStatus(
  snapshot: Record<string, unknown> | null,
): ProjectStatus | null {
  const status = snapshot?.status;
  return typeof status === "string" && status in statusLabels
    ? (status as ProjectStatus)
    : null;
}

function changedFields(event: ProjectWorkflowEvent): string | null {
  if (event.action !== "project_updated" || !event.previous || !event.current) {
    return null;
  }
  const changed = Object.keys(fieldLabels).filter(
    (field) => event.previous?.[field] !== event.current?.[field],
  );
  return changed.length > 0
    ? `变更：${changed.map((field) => fieldLabels[field]).join("、")}`
    : "保存项目资料";
}

function EventRow({ event }: { event: ProjectWorkflowEvent }) {
  const presentation = eventPresentations[event.action] ?? {
    icon: History,
    label: "项目记录已更新",
  };
  const Icon = presentation.icon;
  const previousStatus = snapshotStatus(event.previous);
  const currentStatus = snapshotStatus(event.current);
  const fields = changedFields(event);
  return (
    <li>
      <span className="project-event-icon">
        <Icon aria-hidden="true" size={14} />
      </span>
      <div>
        <strong>{presentation.label}</strong>
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
        {fields ? <small>{fields}</small> : null}
      </div>
    </li>
  );
}

export function ProjectEventsSection({ projectId }: ProjectEventsSectionProps) {
  const [expanded, setExpanded] = useState(true);
  const query = useProjectEventsQuery(projectId, { pageSize: 20 }, expanded);
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
    <section
      aria-labelledby="project-events-heading"
      className="project-detail-section project-events"
    >
      <div className="project-detail-heading project-events-heading">
        <div>
          <h2 id="project-events-heading">活动时间线</h2>
          <p>项目资料与生命周期变化追加保留，不复制当前项目状态。</p>
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
        <div className="project-events-panel">
          {query.isPending ? (
            <div className="project-events-state">
              <LoaderCircle className="spin" size={14} /> 正在读取活动记录…
            </div>
          ) : null}
          {query.isError ? (
            <div className="project-events-error" role="alert">
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
            <div className="project-events-state">暂无项目活动记录。</div>
          ) : null}
          {events.length > 0 ? (
            <ol className="project-event-timeline">
              {events.map((event) => (
                <EventRow event={event} key={event.id} />
              ))}
            </ol>
          ) : null}
          {query.hasNextPage ? (
            <button
              className="button button-secondary project-events-load-more"
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
