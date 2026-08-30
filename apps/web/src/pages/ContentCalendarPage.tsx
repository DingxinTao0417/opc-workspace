import {
  AlertTriangle,
  Archive,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Edit3,
  Plus,
  Send,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useContentItemQuery,
  useCreateContentItem,
  useProjectsQuery,
  useContentItemsInfiniteQuery,
  useDeleteContentItem,
  useLinkContentItemTask,
  usePublishContentItem,
  useScheduleContentItem,
  useTasksQuery,
  useUnlinkContentItemTask,
  useUpdateContentItem,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { localDateFromKey, useLocalCalendar } from "../lib/localCalendar";
import {
  formatDateTimeLocalInTimeZone,
  localDateTimeToZonedISOString,
} from "../lib/zonedDateTime";
import type { ContentItem, ContentItemStatus } from "../types/models";

type ContentCalendarView = "month" | "unscheduled" | "archived";
type ContentCreationMode = "scheduled" | "unscheduled";

const statusLabel: Record<ContentItemStatus, string> = {
  draft: "草稿",
  in_review: "待审核",
  scheduled: "已排期",
  published: "已发布",
  cancelled: "已取消",
  archived: "已归档",
};
const statusClass: Record<ContentItemStatus, string> = {
  draft: "status-neutral",
  in_review: "status-purple",
  scheduled: "status-blue",
  published: "status-green",
  cancelled: "status-red",
  archived: "status-neutral",
};
const editableStatuses: Array<Exclude<ContentItemStatus, "published">> = [
  "draft",
  "in_review",
  "scheduled",
  "cancelled",
  "archived",
];

function dateKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function shiftDateKey(value: string, offset: number) {
  const [year, month, day] = value.split("-").map(Number);
  const shifted = new Date(Date.UTC(year, month - 1, day + offset));
  return `${shifted.getUTCFullYear()}-${String(shifted.getUTCMonth() + 1).padStart(2, "0")}-${String(shifted.getUTCDate()).padStart(2, "0")}`;
}
function shiftMonthKey(value: string, offset: number) {
  const [year, month] = value.split("-").map(Number);
  const shifted = new Date(Date.UTC(year, month - 1 + offset, 1));
  return `${shifted.getUTCFullYear()}-${String(shifted.getUTCMonth() + 1).padStart(2, "0")}`;
}
function calendarForMonth(month: Date) {
  const start = new Date(month.getFullYear(), month.getMonth(), 1);
  start.setDate(start.getDate() - start.getDay());
  const days = Array.from(
    { length: 42 },
    (_, index) =>
      new Date(start.getFullYear(), start.getMonth(), start.getDate() + index),
  );
  const end = new Date(days[41]);
  end.setDate(end.getDate() + 1);
  return { days, from: start.toISOString(), to: end.toISOString() };
}
function displaySchedule(value: string | null) {
  return value
    ? new Intl.DateTimeFormat("zh-CN", {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      }).format(new Date(value))
    : "未排期";
}
function localDateTime(value: string | null, timezone?: string | null) {
  return value
    ? formatDateTimeLocalInTimeZone(
        value,
        timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
      )
    : "";
}
function scheduledDateKey(value: string, timezone?: string | null) {
  return formatDateTimeLocalInTimeZone(
    value,
    timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  ).slice(0, 10);
}
function isSameInstant(left: string | null, right: string) {
  return (
    left !== null && new Date(left).getTime() === new Date(right).getTime()
  );
}
function mutationMessage(error: unknown) {
  return error instanceof Error && error.message
    ? error.message
    : "操作失败，请刷新后重试。";
}

function contentItemDeletionMessage(error: unknown) {
  let message = mutationMessage(error);
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      message = "内容已在另一个窗口变化，请关闭确认框并刷新后重试。";
    } else if (error.code === "CONTENT_ITEM_NOT_ARCHIVED") {
      message = "仅已归档内容可永久删除，请刷新确认当前状态。";
    } else if (error.code === "CONTENT_ITEM_HAS_ACTIVE_INBOX_SOURCES") {
      message =
        "该内容仍有待处理的收件箱来源，请先到收件箱解决或忽略对应事项，再重试永久删除。";
    } else if (error.code === "CONTENT_ITEM_NOT_FOUND") {
      message = "该内容已不存在，请关闭详情并刷新内容日历。";
    }
    if (error.requestId) message += ` · 请求 ID：${error.requestId}`;
  }
  return message;
}

function contentViewFromSearchParams(
  searchParams: URLSearchParams,
): ContentCalendarView {
  const value = searchParams.get("view");
  return value === "unscheduled" || value === "archived" ? value : "month";
}

function contentViewLabel(view: ContentCalendarView) {
  if (view === "unscheduled") return "无排期内容";
  if (view === "archived") return "已归档内容";
  return "内容日历";
}

function ContentDiscoveryList({
  items,
  label,
  onOpen,
}: {
  items: ContentItem[];
  label: string;
  onOpen: (id: string) => void;
}) {
  return (
    <section aria-label={label} className="content-calendar-list">
      {items.map((item) => (
        <article className="content-calendar-card" key={item.id}>
          <button
            aria-label={`查看内容 ${item.title}`}
            className="content-calendar-card-open"
            onClick={() => onOpen(item.id)}
            type="button"
          >
            <span className="content-calendar-card-main">
              {item.status === "archived" ? (
                <Archive aria-hidden="true" size={17} />
              ) : (
                <Clock3 aria-hidden="true" size={17} />
              )}
              <span>
                <strong className="content-calendar-card-title">
                  {item.title}
                </strong>
                <span className="content-calendar-card-platform">
                  {item.platform}
                </span>
              </span>
            </span>
            <span className="content-calendar-card-meta">
              <span className={`status-badge ${statusClass[item.status]}`}>
                {statusLabel[item.status]}
              </span>
              <span>
                <CalendarDays aria-hidden="true" size={13} />
                {displaySchedule(item.scheduledAt)}
              </span>
              <span>
                {item.requiredTaskDone}/{item.requiredTaskTotal} 项准备任务
              </span>
            </span>
          </button>
        </article>
      ))}
    </section>
  );
}

function CreateContentItemModal({
  open,
  onClose,
  onCreated,
  month,
  mode,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (item: ContentItem) => void;
  month: Date;
  mode: ContentCreationMode;
}) {
  const create = useCreateContentItem();
  const projects = useProjectsQuery(
    { page: 1, pageSize: 100, sort: "name" },
    open,
  );
  const [title, setTitle] = useState("");
  const [platform, setPlatform] = useState("");
  const [projectId, setProjectId] = useState("");
  const [date, setDate] = useState("");
  const [error, setError] = useState<string | null>(null);
  const resetDraft = () => {
    setTitle("");
    setPlatform("");
    setProjectId("");
    setDate("");
    setError(null);
    create.reset();
  };
  const close = () => {
    if (create.isPending) return;
    resetDraft();
    onClose();
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (create.isPending) return;
    if (!title.trim() || !platform.trim() || (mode === "scheduled" && !date)) {
      setError(
        mode === "scheduled"
          ? "请填写内容标题、平台和计划日期。"
          : "请填写内容标题和平台。",
      );
      return;
    }
    setError(null);
    const scheduledAt =
      mode === "scheduled"
        ? new Date(`${date}T09:00:00`).toISOString()
        : undefined;
    create.mutate(
      {
        title: title.trim(),
        platform: platform.trim(),
        status: mode === "scheduled" ? "scheduled" : "draft",
        projectId: projectId || null,
        ...(mode === "scheduled"
          ? {
              scheduledAt,
              scheduledTimezone:
                Intl.DateTimeFormat().resolvedOptions().timeZone,
            }
          : {}),
      },
      {
        onSuccess: (item) => {
          resetDraft();
          onCreated(item);
        },
      },
    );
  };
  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={create.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={create.isPending}
            form="content-item-form"
            type="submit"
          >
            {create.isPending
              ? "正在创建…"
              : mode === "unscheduled"
                ? "创建无排期内容"
                : "创建内容"}
          </button>
        </>
      }
      dismissible={!create.isPending}
      onClose={close}
      open={open}
      title={
        mode === "scheduled"
          ? `新建 ${month.getFullYear()}年${month.getMonth() + 1}月内容`
          : "新建无排期内容"
      }
      width="560px"
    >
      <form id="content-item-form" onSubmit={submit}>
        <label className="form-field">
          <span>内容标题</span>
          <input
            autoFocus
            disabled={create.isPending}
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="例如：发布本周产品更新"
            value={title}
          />
        </label>
        <label className="form-field">
          <span>发布平台</span>
          <input
            disabled={create.isPending}
            maxLength={64}
            onChange={(event) => setPlatform(event.target.value)}
            placeholder="例如：微信公众号"
            value={platform}
          />
        </label>
        {mode === "scheduled" ? (
          <label className="form-field">
            <span>计划日期</span>
            <input
              disabled={create.isPending}
              onChange={(event) => setDate(event.target.value)}
              required
              type="date"
              value={date}
            />
          </label>
        ) : (
          <p className="form-note">
            将创建为无排期草稿；稍后可在内容详情中设置计划发布时间。
          </p>
        )}
        <label className="form-field form-field-last">
          <span>关联项目（可选）</span>
          <select
            disabled={create.isPending}
            onChange={(event) => setProjectId(event.target.value)}
            value={projectId}
          >
            <option value="">不关联项目</option>
            {projects.data?.items.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
          </select>
        </label>
        {error || create.isError ? (
          <p className="form-field-error">
            {error ?? mutationMessage(create.error)}
          </p>
        ) : null}
      </form>
    </Modal>
  );
}

function EditContentItemModal({
  item,
  onClose,
}: {
  item: ContentItem | null;
  onClose: () => void;
}) {
  const update = useUpdateContentItem();
  const schedule = useScheduleContentItem();
  const publish = usePublishContentItem();
  const remove = useDeleteContentItem();
  const linkTask = useLinkContentItemTask();
  const unlinkTask = useUnlinkContentItemTask();
  const projects = useProjectsQuery(
    { page: 1, pageSize: 100, sort: "name" },
    Boolean(item),
  );
  const tasks = useTasksQuery({
    projectId: item?.projectId ?? undefined,
    loadAll: true,
  });
  const [title, setTitle] = useState(item?.title ?? "");
  const [platform, setPlatform] = useState(item?.platform ?? "");
  const [projectId, setProjectId] = useState(item?.projectId ?? "");
  const [notes, setNotes] = useState(item?.notes ?? "");
  const [status, setStatus] = useState<Exclude<ContentItemStatus, "published">>(
    (item?.status === "published" ? "draft" : item?.status) ?? "draft",
  );
  const [scheduledAt, setScheduledAt] = useState(
    localDateTime(item?.scheduledAt ?? null, item?.scheduledTimezone),
  );
  const [externalLink, setExternalLink] = useState(item?.externalLink ?? "");
  const [taskId, setTaskId] = useState("");
  const [taskRequired, setTaskRequired] = useState(true);
  const [scheduleError, setScheduleError] = useState<string | null>(null);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  if (!item) return null;
  const busy =
    update.isPending ||
    schedule.isPending ||
    publish.isPending ||
    remove.isPending ||
    linkTask.isPending ||
    unlinkTask.isPending;
  const saveDetails = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    update.mutate(
      {
        id: item.id,
        input: {
          title: title.trim(),
          platform: platform.trim(),
          projectId: projectId || null,
          notes: notes.trim() || null,
          status,
          expectedVersion: item.version,
        },
      },
      { onSuccess: onClose },
    );
  };
  const saveSchedule = () => {
    const timezone =
      item.scheduledTimezone ||
      Intl.DateTimeFormat().resolvedOptions().timeZone ||
      "UTC";
    const converted = scheduledAt
      ? localDateTimeToZonedISOString(scheduledAt, timezone)
      : null;
    if (converted && converted.kind !== "valid") {
      setScheduleError(
        converted.kind === "nonexistent"
          ? "该时区在所选时间发生夏令时跳转，请选择其他时间。"
          : "计划时间无效。",
      );
      return;
    }
    setScheduleError(null);
    schedule.mutate(
      {
        id: item.id,
        input: {
          scheduledAt: converted?.iso ?? null,
          scheduledTimezone: converted ? timezone : null,
          expectedVersion: item.version,
        },
      },
      { onSuccess: onClose },
    );
  };
  const confirmPublish = () =>
    publish.mutate(
      {
        id: item.id,
        input: {
          externalLink: externalLink.trim() || null,
          expectedVersion: item.version,
        },
      },
      { onSuccess: onClose },
    );
  const addTask = () => {
    if (!taskId) return;
    linkTask.mutate(
      {
        id: item.id,
        taskId,
        isRequired: taskRequired,
        expectedVersion: item.version,
      },
      { onSuccess: onClose },
    );
  };
  const removeTask = (linkedTaskId: string) =>
    unlinkTask.mutate(
      { id: item.id, taskId: linkedTaskId, expectedVersion: item.version },
      { onSuccess: onClose },
    );
  const openDeleteConfirm = () => {
    remove.reset();
    setDeleteConfirmOpen(true);
  };
  const closeDeleteConfirm = () => {
    if (remove.isPending) return;
    remove.reset();
    setDeleteConfirmOpen(false);
  };
  const confirmDelete = () =>
    remove.mutate(
      { id: item.id, expectedVersion: item.version },
      {
        onSuccess: () => {
          setDeleteConfirmOpen(false);
          onClose();
        },
      },
    );
  const error =
    update.error ??
    schedule.error ??
    publish.error ??
    linkTask.error ??
    unlinkTask.error;
  const linkedTaskIDs = new Set(item.tasks.map((task) => task.id));
  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={onClose}
            type="button"
          >
            关闭
          </button>
          {item.status === "archived" ? (
            <button
              className="button button-danger"
              disabled={busy}
              onClick={openDeleteConfirm}
              type="button"
            >
              <Trash2 size={14} />
              永久删除
            </button>
          ) : null}
          {item.status !== "published" &&
          item.status !== "archived" &&
          item.status !== "cancelled" ? (
            <button
              className="button button-secondary"
              disabled={busy}
              onClick={confirmPublish}
              type="button"
            >
              <Send size={14} />
              确认已发布
            </button>
          ) : null}
          <button
            className="button button-primary"
            disabled={busy || item.status === "published"}
            form="content-item-edit-form"
            type="submit"
          >
            保存信息
          </button>
        </>
      }
      onClose={onClose}
      open
      title="内容详情与排期"
      width="640px"
    >
      <form id="content-item-edit-form" onSubmit={saveDetails}>
        <div className="content-calendar-edit-grid">
          <label className="form-field">
            <span>内容标题</span>
            <input
              disabled={item.status === "published"}
              maxLength={200}
              onChange={(event) => setTitle(event.target.value)}
              value={title}
            />
          </label>
          <label className="form-field">
            <span>平台</span>
            <input
              disabled={item.status === "published"}
              maxLength={64}
              onChange={(event) => setPlatform(event.target.value)}
              value={platform}
            />
          </label>
          <label className="form-field">
            <span>状态</span>
            <select
              disabled={item.status === "published"}
              onChange={(event) =>
                setStatus(
                  event.target.value as Exclude<ContentItemStatus, "published">,
                )
              }
              value={status}
            >
              {editableStatuses.map((value) => (
                <option key={value} value={value}>
                  {statusLabel[value]}
                </option>
              ))}
            </select>
          </label>
          <label className="form-field">
            <span>关联项目</span>
            <select
              disabled={item.status === "published"}
              onChange={(event) => setProjectId(event.target.value)}
              value={projectId}
            >
              <option value="">不关联项目</option>
              {projects.data?.items.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                </option>
              ))}
            </select>
          </label>
          <label className="form-field content-calendar-edit-wide">
            <span>备注</span>
            <textarea
              disabled={item.status === "published"}
              maxLength={4000}
              onChange={(event) => setNotes(event.target.value)}
              rows={3}
              value={notes}
            />
          </label>
        </div>
        <div className="content-calendar-schedule-row">
          <label className="form-field">
            <span>计划发布时间</span>
            <input
              disabled={
                item.status === "published" || item.status === "archived"
              }
              onChange={(event) => setScheduledAt(event.target.value)}
              type="datetime-local"
              value={scheduledAt}
            />
          </label>
          <button
            className="button button-secondary"
            disabled={
              busy || item.status === "published" || item.status === "archived"
            }
            onClick={saveSchedule}
            type="button"
          >
            <CalendarDays size={14} />
            保存排期
          </button>
        </div>
        {scheduleError ? (
          <p className="form-field-error">{scheduleError}</p>
        ) : null}
        <label className="form-field">
          <span>外部链接文本（可选，不会自动访问）</span>
          <input
            maxLength={2048}
            onChange={(event) => setExternalLink(event.target.value)}
            placeholder="https://…"
            value={externalLink}
          />
        </label>
        <fieldset className="content-calendar-tasks">
          <legend>准备任务</legend>
          {item.tasks.length === 0 ? (
            <p>尚未关联准备任务。</p>
          ) : (
            item.tasks.map((task) => (
              <div className="content-calendar-task" key={task.id}>
                <span>{task.title}</span>
                <small>
                  {task.isRequired ? "必需" : "可选"} · {task.status}
                </small>
                <button
                  className="button button-quiet"
                  disabled={busy}
                  onClick={() => removeTask(task.id)}
                  type="button"
                >
                  解除
                </button>
              </div>
            ))
          )}
          <div className="content-calendar-task-link">
            <select
              aria-label="选择准备任务"
              onChange={(event) => setTaskId(event.target.value)}
              value={taskId}
            >
              <option value="">选择任务…</option>
              {tasks.data
                ?.filter((task) => !linkedTaskIDs.has(task.id))
                .map((task) => (
                  <option key={task.id} value={task.id}>
                    {task.title}
                  </option>
                ))}
            </select>
            <label>
              <input
                checked={taskRequired}
                onChange={(event) => setTaskRequired(event.target.checked)}
                type="checkbox"
              />
              必需
            </label>
            <button
              className="button button-secondary"
              disabled={busy || !taskId}
              onClick={addTask}
              type="button"
            >
              关联
            </button>
          </div>
        </fieldset>
        {error ? (
          <p className="form-field-error">{mutationMessage(error)}</p>
        ) : null}
      </form>
      <Modal
        dismissible={!remove.isPending}
        footer={
          <>
            <button
              className="button button-secondary"
              disabled={remove.isPending}
              onClick={closeDeleteConfirm}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={remove.isPending}
              onClick={confirmDelete}
              type="button"
            >
              {remove.isPending ? "正在永久删除…" : "确认永久删除"}
            </button>
          </>
        }
        onClose={closeDeleteConfirm}
        open={deleteConfirmOpen}
        title="永久删除内容"
        width="500px"
      >
        <div className="roadmap-delete-confirm-copy">
          <AlertTriangle size={22} />
          <div>
            <strong>{item.title}</strong>
            <p>
              此操作不可恢复。关联的准备任务不会被删除；若仍有待处理的 Inbox
              来源，删除将被安全阻止。
            </p>
          </div>
        </div>
        {remove.error ? (
          <p className="form-field-error" role="alert">
            {contentItemDeletionMessage(remove.error)}
          </p>
        ) : null}
      </Modal>
    </Modal>
  );
}

function ContentItemDetailModal({
  contentItemId,
  onClose,
}: {
  contentItemId: string | null;
  onClose: () => void;
}) {
  const query = useContentItemQuery(contentItemId);
  if (query.data) {
    return (
      <EditContentItemModal
        item={query.data}
        key={`${query.data.id}:${query.data.version}`}
        onClose={onClose}
      />
    );
  }
  return (
    <Modal
      footer={
        <button
          className="button button-secondary"
          onClick={onClose}
          type="button"
        >
          关闭
        </button>
      }
      onClose={onClose}
      open={Boolean(contentItemId)}
      title="内容详情与排期"
      width="640px"
    >
      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          message="无法读取内容详情，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
    </Modal>
  );
}

export function ContentCalendarPage() {
  const { dateKey: todayKey, timeZone } = useLocalCalendar();
  const currentMonthKey = todayKey.slice(0, 7);
  const previousCurrentMonthKey = useRef(currentMonthKey);
  const [searchParams, setSearchParams] = useSearchParams();
  const [monthKey, setMonthKey] = useState(currentMonthKey);
  const month = useMemo(
    () => localDateFromKey(`${monthKey}-01`),
    [monthKey, timeZone],
  );
  const [creatingMode, setCreatingMode] = useState<ContentCreationMode | null>(
    null,
  );
  const detailId = searchParams.get("item")?.trim() || null;
  const view = contentViewFromSearchParams(searchParams);
  const [status, setStatus] = useState<ContentItemStatus | "">("");
  const [moveError, setMoveError] = useState<string | null>(null);
  const [schedulePreviews, setSchedulePreviews] = useState<
    Record<string, string>
  >({});
  const schedule = useScheduleContentItem();
  useEffect(() => {
    const rawView = searchParams.get("view");
    if (rawView === null || rawView === "unscheduled" || rawView === "archived")
      return;
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete("view");
        return next;
      },
      { replace: true },
    );
  }, [searchParams, setSearchParams]);
  const changeView = (nextView: ContentCalendarView) => {
    const rawView = searchParams.get("view");
    if (nextView === view && (nextView !== "month" || rawView === null)) return;
    setMoveError(null);
    setSchedulePreviews({});
    schedule.reset();
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (nextView === "month") next.delete("view");
      else next.set("view", nextView);
      return next;
    });
  };
  const openDetail = (id: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set("item", id);
      return next;
    });
  };
  const closeDetail = () => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete("item");
        return next;
      },
      { replace: true },
    );
  };
  const calendar = useMemo(() => calendarForMonth(month), [month]);
  const queryInput =
    view === "month"
      ? {
          pageSize: 100,
          scheduledFrom: calendar.from,
          scheduledTo: calendar.to,
          status: status || undefined,
          includeArchived: status === "archived",
        }
      : view === "unscheduled"
        ? {
            pageSize: 100,
            scheduleState: "unscheduled" as const,
            includeArchived: false,
          }
        : {
            pageSize: 100,
            status: "archived" as const,
            includeArchived: true,
          };
  const query = useContentItemsInfiniteQuery(queryInput);
  const usingPlaceholderData = Boolean(query.isPlaceholderData);
  const transitioning = usingPlaceholderData && query.isFetching;
  useEffect(() => {
    const previousMonthKey = previousCurrentMonthKey.current;
    if (currentMonthKey !== previousMonthKey) {
      if (monthKey === previousMonthKey) {
        setMonthKey(currentMonthKey);
      }
      previousCurrentMonthKey.current = currentMonthKey;
    }
  }, [currentMonthKey, monthKey]);
  useEffect(() => {
    if (
      view === "month" &&
      query.hasNextPage &&
      !usingPlaceholderData &&
      !query.isFetchingNextPage &&
      !query.isFetchNextPageError
    ) {
      void query.fetchNextPage();
    }
  }, [
    query.fetchNextPage,
    query.hasNextPage,
    query.isFetchNextPageError,
    query.isFetchingNextPage,
    usingPlaceholderData,
    view,
  ]);
  const loadedItems = useMemo(() => {
    const unique = new Map<string, ContentItem>();
    for (const page of query.data?.pages ?? []) {
      for (const item of page.items) {
        if (!unique.has(item.id)) unique.set(item.id, item);
      }
    }
    return [...unique.values()];
  }, [query.data?.pages]);
  const items = usingPlaceholderData ? [] : loadedItems;
  useEffect(() => {
    setSchedulePreviews((current) => {
      let changed = false;
      const next = { ...current };
      for (const [itemID, scheduledAt] of Object.entries(current)) {
        const serverItem = items.find((item) => item.id === itemID);
        if (!serverItem || isSameInstant(serverItem.scheduledAt, scheduledAt)) {
          delete next[itemID];
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [items]);
  const total = usingPlaceholderData
    ? 0
    : (query.data?.pages[0]?.meta.total ?? 0);
  const viewLabel = contentViewLabel(view);
  const loading = query.isPending || transitioning;
  const hasLoadedData = Boolean(query.data) && !usingPlaceholderData;
  const countSuffix =
    view === "month"
      ? ""
      : view === "unscheduled"
        ? "未排期内容"
        : "已归档内容";
  const countText = loading
    ? "读取中"
    : hasLoadedData
      ? query.hasNextPage ||
        query.isFetchingNextPage ||
        query.isFetchNextPageError
        ? `已读取 ${items.length} / ${total} 条${countSuffix}`
        : view === "month"
          ? `${total} 条`
          : view === "unscheduled"
            ? `${total} 条未排期内容`
            : `${total} 条已归档内容`
      : "数据不可用";
  const itemsByDate = useMemo(() => {
    const groups = new Map<string, ContentItem[]>();
    for (const item of items) {
      const scheduledAt = schedulePreviews[item.id] ?? item.scheduledAt;
      if (!scheduledAt) continue;
      const key = scheduledDateKey(scheduledAt, item.scheduledTimezone);
      groups.set(key, [
        ...(groups.get(key) ?? []),
        scheduledAt === item.scheduledAt ? item : { ...item, scheduledAt },
      ]);
    }
    return groups;
  }, [items, schedulePreviews]);
  const shift = (offset: number) =>
    setMonthKey((current) => shiftMonthKey(current, offset));
  const moveItem = (itemID: string, targetDate: string) => {
    const item = items.find((candidate) => candidate.id === itemID);
    if (
      schedule.isPending ||
      !item?.scheduledAt ||
      item.status === "published" ||
      item.status === "archived" ||
      item.status === "cancelled"
    )
      return;
    if (
      scheduledDateKey(item.scheduledAt, item.scheduledTimezone) === targetDate
    ) {
      setMoveError(null);
      return;
    }
    const firstVisibleDate = dateKey(calendar.days[0]);
    const lastVisibleDate = dateKey(calendar.days.at(-1) ?? calendar.days[0]);
    if (targetDate < firstVisibleDate || targetDate > lastVisibleDate) {
      setMoveError("已到当前月格边界，请切换月份或使用详情表单改期。");
      return;
    }
    const timezone =
      item.scheduledTimezone ||
      Intl.DateTimeFormat().resolvedOptions().timeZone ||
      "UTC";
    const wallTime = formatDateTimeLocalInTimeZone(item.scheduledAt, timezone);
    const converted = localDateTimeToZonedISOString(
      `${targetDate}${wallTime.slice(10)}`,
      timezone,
    );
    if (converted.kind !== "valid") {
      setMoveError(
        converted.kind === "nonexistent"
          ? "目标日期的原发布时间落在夏令时跳转空档，请使用详情表单选择时间。"
          : "无法解析目标排期时间。",
      );
      return;
    }
    setMoveError(null);
    schedule.reset();
    setSchedulePreviews((current) => ({
      ...current,
      [item.id]: converted.iso,
    }));
    schedule.mutate(
      {
        id: item.id,
        input: {
          scheduledAt: converted.iso,
          scheduledTimezone: timezone,
          expectedVersion: item.version,
        },
      },
      {
        onError: (error) => {
          setSchedulePreviews((current) => {
            const next = { ...current };
            delete next[item.id];
            return next;
          });
          setMoveError(`排期未保存，已恢复原日期。${mutationMessage(error)}`);
          void query.refetch();
        },
      },
    );
  };

  return (
    <div className="page">
      <PageHeader
        actions={
          view !== "archived" ? (
            <button
              className="button button-primary"
              disabled={schedule.isPending}
              onClick={() =>
                setCreatingMode(
                  view === "unscheduled" ? "unscheduled" : "scheduled",
                )
              }
              type="button"
            >
              <Plus size={15} />
              {view === "unscheduled" ? "新建无排期内容" : "新建内容"}
            </button>
          ) : undefined
        }
        meta={<span className="page-count">{countText}</span>}
        title="内容日历"
      />
      <div
        aria-label="内容日历视图"
        className="content-calendar-view-switcher"
        role="group"
      >
        <button
          aria-pressed={view === "month"}
          className="button button-secondary"
          disabled={schedule.isPending}
          onClick={() => changeView("month")}
          type="button"
        >
          <CalendarDays size={14} />
          月历
        </button>
        <button
          aria-pressed={view === "unscheduled"}
          className="button button-secondary"
          disabled={schedule.isPending}
          onClick={() => changeView("unscheduled")}
          type="button"
        >
          <Clock3 size={14} />
          无排期
        </button>
        <button
          aria-pressed={view === "archived"}
          className="button button-secondary"
          disabled={schedule.isPending}
          onClick={() => changeView("archived")}
          type="button"
        >
          <Archive size={14} />
          已归档
        </button>
      </div>
      {view === "month" ? (
        <div className="toolbar content-calendar-toolbar">
          <button
            aria-label="上个月"
            className="button button-secondary button-icon"
            disabled={schedule.isPending}
            onClick={() => shift(-1)}
            type="button"
          >
            <ChevronLeft size={16} />
          </button>
          <strong className="content-calendar-month">
            {month.getFullYear()} 年 {month.getMonth() + 1} 月
          </strong>
          <button
            aria-label="下个月"
            className="button button-secondary button-icon"
            disabled={schedule.isPending}
            onClick={() => shift(1)}
            type="button"
          >
            <ChevronRight size={16} />
          </button>
          <label className="toolbar-select">
            <span className="sr-only">状态</span>
            <select
              disabled={schedule.isPending}
              onChange={(event) =>
                setStatus(event.target.value as ContentItemStatus | "")
              }
              value={status}
            >
              <option value="">未归档</option>
              {Object.entries(statusLabel).map(([key, label]) => (
                <option key={key} value={key}>
                  {key === "archived" ? "已归档（当前月）" : label}
                </option>
              ))}
            </select>
          </label>
        </div>
      ) : null}
      {query.isError && !hasLoadedData ? (
        <ErrorState
          message={`无法读取${viewLabel}，请确认本地服务已连接。`}
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isFetchNextPageError ? (
        <ErrorState
          message={`已读取 ${items.length} / ${total} 条，后续页面暂时不可用。`}
          onRetry={() => void query.fetchNextPage()}
          title={`${viewLabel}未完整加载`}
        />
      ) : null}
      {query.isRefetchError && hasLoadedData && !query.isFetchNextPageError ? (
        <ErrorState
          message={`正在显示上次读取的${viewLabel}，最新刷新失败。`}
          onRetry={() => void query.refetch()}
          title={`${viewLabel}未能刷新`}
        />
      ) : null}
      {loading ? (
        <div>
          <span aria-live="polite" className="sr-only" role="status">
            正在读取{viewLabel}…
          </span>
          <SkeletonRows count={4} />
        </div>
      ) : null}
      {view === "month" && (moveError || schedule.isError) ? (
        <p className="form-field-error content-calendar-drag-error">
          {moveError ?? mutationMessage(schedule.error)}
        </p>
      ) : null}
      {view === "month" && schedule.isPending ? (
        <p className="content-calendar-drag-status" role="status">
          正在保存排期调整…
        </p>
      ) : null}
      {view === "month" && hasLoadedData && items.length > 0 ? (
        <p className="content-calendar-keyboard-note">
          键盘改期：聚焦可编辑卡片后按 Alt + ← / →
          移动一天；精确时间可在详情中调整。
        </p>
      ) : null}
      {view === "month" && hasLoadedData ? (
        <section
          aria-label="内容月历"
          className="content-calendar-grid"
          role="grid"
        >
          {["日", "一", "二", "三", "四", "五", "六"].map((weekday) => (
            <div
              className="content-calendar-weekday"
              key={weekday}
              role="columnheader"
            >
              周{weekday}
            </div>
          ))}
          {calendar.days.map((day) => {
            const key = dateKey(day);
            const dayItems = itemsByDate.get(key) ?? [];
            const outside = day.getMonth() !== month.getMonth();
            return (
              <div
                aria-label={`${key}，${dayItems.length} 条内容`}
                className={`content-calendar-day${outside ? " is-outside" : ""}`}
                key={key}
                onDragOver={(event) => event.preventDefault()}
                onDrop={(event) => {
                  event.preventDefault();
                  moveItem(
                    event.dataTransfer.getData("text/content-item-id"),
                    key,
                  );
                }}
                role="gridcell"
              >
                <time dateTime={key}>{day.getDate()}</time>
                <div className="content-calendar-day-items">
                  {dayItems.map((item) => (
                    <button
                      aria-label={`编辑 ${item.title}`}
                      className={`content-calendar-day-item ${statusClass[item.status]}`}
                      disabled={schedule.isPending}
                      draggable={
                        !schedule.isPending &&
                        item.status !== "published" &&
                        item.status !== "archived" &&
                        item.status !== "cancelled"
                      }
                      key={item.id}
                      onKeyDown={(event) => {
                        if (
                          !event.altKey ||
                          (event.key !== "ArrowLeft" &&
                            event.key !== "ArrowRight") ||
                          !item.scheduledAt
                        )
                          return;
                        event.preventDefault();
                        moveItem(
                          item.id,
                          shiftDateKey(
                            scheduledDateKey(
                              item.scheduledAt,
                              item.scheduledTimezone,
                            ),
                            event.key === "ArrowLeft" ? -1 : 1,
                          ),
                        );
                      }}
                      onClick={() => openDetail(item.id)}
                      onDragStart={(event) => {
                        event.dataTransfer.effectAllowed = "move";
                        event.dataTransfer.setData(
                          "text/content-item-id",
                          item.id,
                        );
                      }}
                      type="button"
                      {...(item.status !== "published" &&
                      item.status !== "archived" &&
                      item.status !== "cancelled"
                        ? {
                            "aria-keyshortcuts": "Alt+ArrowLeft Alt+ArrowRight",
                          }
                        : {})}
                    >
                      <strong>{item.title}</strong>
                      <span>
                        {displaySchedule(item.scheduledAt).split(" ").at(-1)}
                      </span>
                      <span>
                        <em>{item.platform}</em> · {item.requiredTaskDone}/
                        {item.requiredTaskTotal} 项准备任务
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            );
          })}
        </section>
      ) : null}
      {view !== "month" && hasLoadedData && items.length > 0 ? (
        <ContentDiscoveryList
          items={items}
          label={viewLabel}
          onOpen={openDetail}
        />
      ) : null}
      {view !== "month" &&
      hasLoadedData &&
      query.hasNextPage &&
      !query.isFetchNextPageError ? (
        <div className="content-calendar-load-more">
          <button
            className="button button-secondary"
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage()}
            type="button"
          >
            {query.isFetchingNextPage ? "正在加载更多…" : "加载更多"}
          </button>
        </div>
      ) : null}
      {hasLoadedData && items.length === 0 ? (
        <EmptyState
          action={
            view !== "archived" ? (
              <button
                className="button button-primary"
                onClick={() =>
                  setCreatingMode(
                    view === "unscheduled" ? "unscheduled" : "scheduled",
                  )
                }
                type="button"
              >
                <Plus size={15} />
                {view === "unscheduled"
                  ? "新建第一条无排期内容"
                  : "新建第一条内容"}
              </button>
            ) : undefined
          }
          message={
            view === "month"
              ? "内容排期保存在本机，不会连接或发布到外部平台。"
              : view === "unscheduled"
                ? "未设置计划发布时间且尚未归档的内容会显示在这里。"
                : "归档内容会跨月份集中显示，包括未排期的归档内容。"
          }
          title={
            view === "month"
              ? "当前月格暂无内容排期"
              : view === "unscheduled"
                ? "暂无无排期内容"
                : "暂无已归档内容"
          }
        />
      ) : null}
      <CreateContentItemModal
        month={month}
        mode={creatingMode ?? "scheduled"}
        onClose={() => setCreatingMode(null)}
        onCreated={(item) => {
          setCreatingMode(null);
          openDetail(item.id);
        }}
        open={creatingMode !== null}
      />
      <ContentItemDetailModal contentItemId={detailId} onClose={closeDetail} />
    </div>
  );
}
