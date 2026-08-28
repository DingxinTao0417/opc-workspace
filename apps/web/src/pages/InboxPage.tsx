import {
  Archive,
  BellRing,
  CheckCheck,
  ChevronRight,
  Inbox,
  Plus,
  Search,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import { useInboxItemsQuery, useMarkAllInboxItemsRead } from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { InboxItemDetailModal } from "../components/InboxItemDetailModal";
import { InboxItemFormModal } from "../components/InboxItemFormModal";
import { PageHeader } from "../components/PageHeader";
import { ReminderManagerModal } from "../components/ReminderManagerModal";
import type {
  InboxItem,
  InboxItemPriority,
  InboxItemView,
} from "../types/models";

const viewLabels: Record<InboxItemView, string> = {
  inbox: "待处理",
  snoozed: "稍后",
  archive: "已归档",
};

const viewDescriptions: Record<InboxItemView, string> = {
  inbox: "手工记录和已到期的本地提醒会显示在这里。",
  snoozed: "设置恢复时间后，条目会暂存在这里。",
  archive: "已解决和已忽略的条目会保留在这里。",
};

function sameLocalDay(left: string, right: string): boolean {
  const leftDate = new Date(left);
  const rightDate = new Date(right);
  return (
    !Number.isNaN(leftDate.getTime()) &&
    !Number.isNaN(rightDate.getTime()) &&
    leftDate.getFullYear() === rightDate.getFullYear() &&
    leftDate.getMonth() === rightDate.getMonth() &&
    leftDate.getDate() === rightDate.getDate()
  );
}

function relativeTime(value: string, serverNow: string): string {
  const date = new Date(value);
  const now = new Date(serverNow);
  if (Number.isNaN(date.getTime()) || Number.isNaN(now.getTime())) return value;
  const differenceSeconds = Math.round(
    (date.getTime() - now.getTime()) / 1_000,
  );
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  const absolute = Math.abs(differenceSeconds);
  if (absolute < 60) return "刚刚";
  if (absolute < 60 * 60) {
    return formatter.format(Math.round(differenceSeconds / 60), "minute");
  }
  if (absolute < 24 * 60 * 60) {
    return formatter.format(Math.round(differenceSeconds / 3_600), "hour");
  }
  if (absolute < 7 * 24 * 60 * 60) {
    return formatter.format(Math.round(differenceSeconds / 86_400), "day");
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
  }).format(date);
}

function formatDateTime(value: string): string {
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

function InboxRow({
  item,
  serverNow,
  view,
  onOpen,
}: {
  item: InboxItem;
  serverNow: string;
  view: InboxItemView;
  onOpen: () => void;
}) {
  const secondary =
    view === "snoozed" && item.snoozedUntil
      ? `恢复于 ${formatDateTime(item.snoozedUntil)}`
      : view === "archive"
        ? item.status === "resolved"
          ? `已解决${item.resolutionReason ? ` · ${item.resolutionReason}` : ""}`
          : `已忽略${item.dismissReason ? ` · ${item.dismissReason}` : ""}`
        : item.summary || (item.kind === "reminder" ? "本地提醒" : "手工记录");
  return (
    <button
      aria-label={`查看 ${item.title}`}
      className={`inbox-row${item.readAt ? "" : " unread"}`}
      onClick={onOpen}
      type="button"
    >
      <span
        aria-label={item.readAt ? "已读" : "未读"}
        className={`inbox-unread-dot${item.readAt ? " off" : ""}`}
        role="img"
      />
      <span aria-hidden="true" className="inbox-row-icon">
        {view === "snoozed" ? (
          <BellRing size={15} />
        ) : view === "archive" ? (
          <Archive size={15} />
        ) : item.kind === "reminder" ? (
          <BellRing size={15} />
        ) : (
          <Inbox size={15} />
        )}
      </span>
      <span className="inbox-row-copy">
        <strong>{item.title}</strong>
        <small>{secondary}</small>
      </span>
      <span
        className={`inbox-priority inbox-priority-${item.priority.toLowerCase()}`}
      >
        {item.priority}
      </span>
      <time dateTime={item.createdAt}>
        {relativeTime(item.createdAt, serverNow)}
      </time>
      <ChevronRight aria-hidden="true" size={14} />
    </button>
  );
}

function mutationErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "全部已读操作失败，请重试。";
}

export function InboxPage() {
  const [view, setView] = useState<InboxItemView>("inbox");
  const [search, setSearch] = useState("");
  const [priority, setPriority] = useState<InboxItemPriority | "">("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [managingReminders, setManagingReminders] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const query = useInboxItemsQuery({
    view,
    q: search,
    priority: priority || undefined,
    page,
    pageSize: 30,
  });
  const markAllMutation = useMarkAllInboxItemsRead();
  const transitioning = query.isPlaceholderData && query.isFetching;
  const items = transitioning ? [] : (query.data?.items ?? []);
  const totalPages = Math.max(
    1,
    Math.ceil(
      (query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 30),
    ),
  );
  const hasFilters = Boolean(search.trim() || priority);
  const groupedItems = useMemo(() => {
    const serverNow = query.data?.meta.serverNow ?? new Date().toISOString();
    return {
      today: items.filter((item) => sameLocalDay(item.createdAt, serverNow)),
      earlier: items.filter((item) => !sameLocalDay(item.createdAt, serverNow)),
    };
  }, [items, query.data?.meta.serverNow]);
  const markAllError = mutationErrorMessage(markAllMutation.error);

  useEffect(() => {
    if (transitioning || !query.data || page <= totalPages) return;
    setPage(totalPages);
  }, [page, query.data, totalPages, transitioning]);

  const switchView = (nextView: InboxItemView) => {
    setView(nextView);
    setPage(1);
    markAllMutation.reset();
  };

  return (
    <div className="page inbox-page">
      <PageHeader
        actions={
          <div className="inbox-header-actions">
            <button
              className="button button-secondary"
              onClick={() => setManagingReminders(true)}
              type="button"
            >
              <BellRing size={15} />
              本地提醒
            </button>
            <button
              className="button button-secondary"
              disabled={
                query.isPending ||
                transitioning ||
                markAllMutation.isPending ||
                !query.data?.meta.unreadTotal
              }
              onClick={() => {
                if (!query.data) return;
                markAllMutation.mutate({
                  throughCreatedAt: query.data.meta.snapshotAt,
                });
              }}
              type="button"
            >
              <CheckCheck size={15} />
              {markAllMutation.isPending ? "正在处理…" : "全部标为已读"}
            </button>
            <button
              className="button button-primary"
              onClick={() => setCreating(true)}
              type="button"
            >
              <Plus size={15} />
              新建条目
            </button>
          </div>
        }
        meta={
          <span className="page-count" aria-live="polite">
            {query.isPending || transitioning
              ? "读取中"
              : query.isSuccess
                ? `${query.data.meta.unreadTotal} 条未读`
                : "数据不可用"}
          </span>
        }
        title="收件箱"
      />

      <div aria-label="收件箱视图" className="inbox-view-tabs" role="tablist">
        {(Object.keys(viewLabels) as InboxItemView[]).map((key) => (
          <button
            aria-selected={view === key}
            className={view === key ? "active" : undefined}
            key={key}
            onClick={() => switchView(key)}
            role="tab"
            type="button"
          >
            {viewLabels[key]}
          </button>
        ))}
      </div>

      <div className="toolbar inbox-toolbar">
        <label className="toolbar-search">
          <Search size={15} />
          <input
            aria-label="搜索收件箱"
            maxLength={200}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="搜索标题或说明…"
            value={search}
          />
        </label>
        <label className="toolbar-select">
          <span className="sr-only">优先级</span>
          <select
            aria-label="优先级"
            onChange={(event) => {
              setPriority(event.target.value as InboxItemPriority | "");
              setPage(1);
            }}
            value={priority}
          >
            <option value="">全部优先级</option>
            <option value="P0">P0 · 紧急</option>
            <option value="P1">P1 · 高</option>
            <option value="P2">P2 · 普通</option>
            <option value="P3">P3 · 低</option>
          </select>
        </label>
      </div>

      {markAllError ? (
        <div className="form-error inbox-page-error" role="alert">
          {markAllError}
        </div>
      ) : null}
      {query.isError ? (
        <ErrorState
          message="无法读取收件箱，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending || transitioning ? <SkeletonRows count={7} /> : null}

      {query.isSuccess && !transitioning && items.length === 0 ? (
        <EmptyState
          action={
            hasFilters ? (
              <button
                className="button button-secondary"
                onClick={() => {
                  setSearch("");
                  setPriority("");
                  setPage(1);
                }}
                type="button"
              >
                清除筛选
              </button>
            ) : view === "inbox" ? (
              <button
                className="button button-primary"
                onClick={() => setCreating(true)}
                type="button"
              >
                <Plus size={15} />
                新建第一个条目
              </button>
            ) : null
          }
          message={
            hasFilters ? "调整关键词或优先级后再试。" : viewDescriptions[view]
          }
          title={hasFilters ? "没有匹配的条目" : `${viewLabels[view]}为空`}
        />
      ) : null}

      {items.length > 0 ? (
        <div className="inbox-groups">
          {groupedItems.today.length > 0 ? (
            <section aria-labelledby="inbox-today-heading">
              <h2 id="inbox-today-heading">今天</h2>
              <div className="inbox-list">
                {groupedItems.today.map((item) => (
                  <InboxRow
                    item={item}
                    key={item.id}
                    onOpen={() => setSelectedId(item.id)}
                    serverNow={query.data?.meta.serverNow ?? item.createdAt}
                    view={view}
                  />
                ))}
              </div>
            </section>
          ) : null}
          {groupedItems.earlier.length > 0 ? (
            <section aria-labelledby="inbox-earlier-heading">
              <h2 id="inbox-earlier-heading">更早</h2>
              <div className="inbox-list">
                {groupedItems.earlier.map((item) => (
                  <InboxRow
                    item={item}
                    key={item.id}
                    onOpen={() => setSelectedId(item.id)}
                    serverNow={query.data?.meta.serverNow ?? item.createdAt}
                    view={view}
                  />
                ))}
              </div>
            </section>
          ) : null}
        </div>
      ) : null}

      {totalPages > 1 ? (
        <nav aria-label="收件箱分页" className="pagination">
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

      <InboxItemFormModal
        onClose={() => setCreating(false)}
        onCreated={(item) => setSelectedId(item.id)}
        open={creating}
      />
      <InboxItemDetailModal
        itemId={selectedId}
        onClose={() => setSelectedId(null)}
      />
      {managingReminders ? (
        <ReminderManagerModal
          onClose={() => setManagingReminders(false)}
          onOpenInboxItem={(id) => {
            setManagingReminders(false);
            setSelectedId(id);
          }}
          open
        />
      ) : null}
    </div>
  );
}
