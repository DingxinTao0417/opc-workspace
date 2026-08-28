import {
  AlarmClock,
  Bell,
  CalendarClock,
  CheckCircle2,
  ChevronRight,
  Inbox,
  Pencil,
  Plus,
  Search,
  XCircle,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  useCancelReminder,
  useCreateReminder,
  useReminderQuery,
  useRemindersQuery,
  useUpdateReminder,
} from "../api/hooks";
import type {
  InboxItemPriority,
  Reminder,
  ReminderStatus,
} from "../types/models";
import { EmptyState, SkeletonRows } from "./feedback";
import { Modal } from "./Modal";

const statusLabels: Record<ReminderStatus, string> = {
  scheduled: "待触发",
  fired: "已触发",
  cancelled: "已取消",
};

const statusDescriptions: Record<ReminderStatus, string> = {
  scheduled: "应用运行时每 15 秒扫描；关机期间错过的提醒会在下次启动补偿。",
  fired: "已触发提醒只保留审计事实，并关联生成的收件箱条目。",
  cancelled: "取消不会删除记录，原因和操作时间会保留在本机。",
};

type ReminderDraft = {
  title: string;
  summary: string;
  priority: InboxItemPriority;
  triggerAt: string;
};

const emptyDraft = (): ReminderDraft => ({
  title: "",
  summary: "",
  priority: "P2",
  triggerAt: "",
});

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

function toLocalDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function toISOString(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function draftFromReminder(reminder: Reminder): ReminderDraft {
  return {
    title: reminder.title,
    summary: reminder.summary,
    priority: reminder.priority,
    triggerAt: toLocalDateTime(reminder.triggerAt),
  };
}

function errorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "提醒已在其他窗口发生变化。当前草稿仍保留，请重新加载最新记录后确认。";
    }
    if (error.code === "REMINDER_NOT_SCHEDULED") {
      return "该提醒已经触发或取消，不能继续修改。请重新加载最新状态。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "提醒操作失败，请重试。";
}

function ReminderStatusIcon({ status }: { status: ReminderStatus }) {
  if (status === "fired") return <CheckCircle2 size={15} />;
  if (status === "cancelled") return <XCircle size={15} />;
  return <AlarmClock size={15} />;
}

export function ReminderManagerModal({
  open,
  onClose,
  onOpenInboxItem,
}: {
  open: boolean;
  onClose: () => void;
  onOpenInboxItem?: (id: string) => void;
}) {
  const [status, setStatus] = useState<ReminderStatus>("scheduled");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<ReminderDraft>(emptyDraft);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [cancelReason, setCancelReason] = useState("");
  const [confirmingCancel, setConfirmingCancel] = useState(false);
  const initializedEditor = useRef<string | null>(null);
  const query = useRemindersQuery(
    {
      status,
      q: search,
      page,
      pageSize: 20,
      sort: status === "scheduled" ? "trigger_at" : "-updated_at",
    },
    open,
  );
  const detailQuery = useReminderQuery(open ? selectedId : null);
  const createMutation = useCreateReminder();
  const updateMutation = useUpdateReminder();
  const cancelMutation = useCancelReminder();
  const transitioning = query.isPlaceholderData && query.isFetching;
  const items = transitioning ? [] : (query.data?.items ?? []);
  const totalPages = Math.max(
    1,
    Math.ceil(
      (query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 20),
    ),
  );
  const selected = detailQuery.data ?? null;
  const busy =
    createMutation.isPending ||
    updateMutation.isPending ||
    cancelMutation.isPending;

  useEffect(() => {
    if (!open) {
      setSelectedId(null);
      setCreating(false);
      initializedEditor.current = null;
      return;
    }
  }, [open]);

  useEffect(() => {
    if (!selected || creating || initializedEditor.current === selected.id) {
      return;
    }
    initializedEditor.current = selected.id;
    setDraft(draftFromReminder(selected));
    setValidationError(null);
    setCancelReason("");
    setConfirmingCancel(false);
    updateMutation.reset();
    cancelMutation.reset();
  }, [creating, selected]);

  useEffect(() => {
    if (transitioning || !query.data || page <= totalPages) return;
    setPage(totalPages);
  }, [page, query.data, totalPages, transitioning]);

  useEffect(() => {
    if (!selectedId || transitioning || query.isPending) return;
    if (items.some((item) => item.id === selectedId)) return;
    if (selected?.status === status) return;
    setSelectedId(null);
    initializedEditor.current = null;
  }, [
    items,
    query.isPending,
    selected?.status,
    selectedId,
    status,
    transitioning,
  ]);

  const operationError = useMemo(
    () =>
      validationError ??
      errorMessage(createMutation.error) ??
      errorMessage(updateMutation.error) ??
      errorMessage(cancelMutation.error) ??
      errorMessage(detailQuery.error),
    [
      cancelMutation.error,
      createMutation.error,
      detailQuery.error,
      updateMutation.error,
      validationError,
    ],
  );

  const resetMutations = () => {
    createMutation.reset();
    updateMutation.reset();
    cancelMutation.reset();
    setValidationError(null);
  };

  const switchStatus = (next: ReminderStatus) => {
    setStatus(next);
    setPage(1);
    setSelectedId(null);
    setCreating(false);
    initializedEditor.current = null;
    resetMutations();
  };

  const startCreating = () => {
    setCreating(true);
    setSelectedId(null);
    initializedEditor.current = null;
    setDraft(emptyDraft());
    setCancelReason("");
    setConfirmingCancel(false);
    resetMutations();
  };

  const selectReminder = (reminder: Reminder) => {
    setCreating(false);
    setSelectedId(reminder.id);
    initializedEditor.current = null;
    resetMutations();
  };

  const validateDraft = (): {
    value: ReminderDraft;
    triggerAt: string;
  } | null => {
    const cleanTitle = draft.title.trim();
    const cleanSummary = draft.summary.trim();
    if (unicodeLength(cleanTitle) < 2 || unicodeLength(cleanTitle) > 200) {
      setValidationError("标题需要 2–200 个字符。");
      return null;
    }
    if (unicodeLength(cleanSummary) > 10_000) {
      setValidationError("说明不能超过 10,000 个字符。");
      return null;
    }
    const triggerAt = toISOString(draft.triggerAt);
    if (!triggerAt || new Date(triggerAt).getTime() <= Date.now()) {
      setValidationError("提醒时间必须晚于当前时间。");
      return null;
    }
    setValidationError(null);
    return {
      value: { ...draft, title: cleanTitle, summary: cleanSummary },
      triggerAt,
    };
  };

  const save = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalized = validateDraft();
    if (!normalized) return;
    if (creating) {
      createMutation.mutate(
        {
          ...normalized.value,
          triggerAt: normalized.triggerAt,
        },
        {
          onSuccess: (reminder) => {
            setCreating(false);
            setStatus("scheduled");
            setPage(1);
            setSelectedId(reminder.id);
            initializedEditor.current = reminder.id;
            setDraft(draftFromReminder(reminder));
          },
        },
      );
      return;
    }
    if (!selected || selected.status !== "scheduled") return;
    updateMutation.mutate(
      {
        id: selected.id,
        input: {
          ...normalized.value,
          triggerAt: normalized.triggerAt,
          expectedVersion: selected.version,
        },
      },
      {
        onSuccess: (reminder) => {
          initializedEditor.current = reminder.id;
          setDraft(draftFromReminder(reminder));
        },
      },
    );
  };

  const reloadSelected = async () => {
    const result = await detailQuery.refetch();
    if (!result.data) return;
    initializedEditor.current = result.data.id;
    setDraft(draftFromReminder(result.data));
    setValidationError(null);
    updateMutation.reset();
    cancelMutation.reset();
  };

  const confirmCancel = () => {
    if (!selected || selected.status !== "scheduled") return;
    const reason = cancelReason.trim();
    if (unicodeLength(reason) < 1 || unicodeLength(reason) > 1000) {
      setValidationError("取消原因需要 1–1,000 个字符。");
      return;
    }
    setValidationError(null);
    cancelMutation.mutate(
      { id: selected.id, reason, expectedVersion: selected.version },
      {
        onSuccess: (reminder) => {
          setConfirmingCancel(false);
          setStatus("cancelled");
          setPage(1);
          initializedEditor.current = reminder.id;
        },
      },
    );
  };

  return (
    <Modal
      footer={
        <button
          className="button button-secondary"
          disabled={busy}
          onClick={onClose}
          type="button"
        >
          关闭
        </button>
      }
      onClose={onClose}
      open={open}
      title="本地提醒"
      width="940px"
    >
      <div className="reminder-manager">
        <aside aria-label="提醒分类" className="reminder-manager-nav">
          <div className="reminder-manager-nav-heading">
            <Bell size={15} />
            <span>提醒管理</span>
          </div>
          {(Object.keys(statusLabels) as ReminderStatus[]).map((key) => (
            <button
              aria-current={status === key ? "page" : undefined}
              className={status === key ? "active" : undefined}
              key={key}
              onClick={() => switchStatus(key)}
              type="button"
            >
              <ReminderStatusIcon status={key} />
              <span>{statusLabels[key]}</span>
            </button>
          ))}
          <p>一次性提醒仅保存在本机，不发送邮件、短信或远程通知。</p>
        </aside>

        <section className="reminder-manager-content">
          <header className="reminder-manager-heading">
            <div>
              <h3>{statusLabels[status]}</h3>
              <p>{statusDescriptions[status]}</p>
            </div>
            <button
              className="button button-primary"
              disabled={busy}
              onClick={startCreating}
              type="button"
            >
              <Plus size={15} />
              新建提醒
            </button>
          </header>

          <label className="reminder-manager-search">
            <Search size={14} />
            <input
              aria-label="搜索提醒"
              maxLength={200}
              onChange={(event) => {
                setSearch(event.target.value);
                setPage(1);
              }}
              placeholder="搜索标题或说明…"
              value={search}
            />
          </label>

          <div className="reminder-manager-workspace">
            <div className="reminder-manager-list-pane">
              {query.isError ? (
                <div className="reminder-manager-error" role="alert">
                  <span>无法读取提醒。</span>
                  <button
                    className="button button-secondary"
                    onClick={() => void query.refetch()}
                    type="button"
                  >
                    重试
                  </button>
                </div>
              ) : null}
              {query.isPending || transitioning ? (
                <SkeletonRows count={5} />
              ) : null}
              {query.isSuccess && !transitioning && items.length === 0 ? (
                <EmptyState
                  action={
                    status === "scheduled" && !search.trim() ? (
                      <button
                        className="button button-primary"
                        onClick={startCreating}
                        type="button"
                      >
                        <Plus size={15} />
                        新建第一个提醒
                      </button>
                    ) : undefined
                  }
                  message={
                    search.trim()
                      ? "换个关键词后再试。"
                      : statusDescriptions[status]
                  }
                  title={
                    search.trim()
                      ? "没有匹配的提醒"
                      : `${statusLabels[status]}为空`
                  }
                />
              ) : null}
              {items.length > 0 ? (
                <div className="reminder-manager-list">
                  {items.map((reminder) => (
                    <button
                      aria-pressed={selectedId === reminder.id}
                      className={
                        selectedId === reminder.id ? "active" : undefined
                      }
                      key={reminder.id}
                      onClick={() => selectReminder(reminder)}
                      type="button"
                    >
                      <span
                        className={`reminder-status-icon ${reminder.status}`}
                      >
                        <ReminderStatusIcon status={reminder.status} />
                      </span>
                      <span>
                        <strong>{reminder.title}</strong>
                        <small>
                          {reminder.status === "scheduled"
                            ? formatDateTime(reminder.triggerAt)
                            : reminder.status === "fired" && reminder.firedAt
                              ? `触发于 ${formatDateTime(reminder.firedAt)}`
                              : reminder.cancelledAt
                                ? `取消于 ${formatDateTime(reminder.cancelledAt)}`
                                : statusLabels[reminder.status]}
                        </small>
                      </span>
                      <span
                        className={`inbox-priority inbox-priority-${reminder.priority.toLowerCase()}`}
                      >
                        {reminder.priority}
                      </span>
                      <ChevronRight size={14} />
                    </button>
                  ))}
                </div>
              ) : null}
              {totalPages > 1 ? (
                <nav
                  aria-label="提醒分页"
                  className="reminder-manager-pagination"
                >
                  <button
                    className="button button-secondary"
                    disabled={page <= 1 || query.isFetching}
                    onClick={() => setPage((value) => Math.max(1, value - 1))}
                    type="button"
                  >
                    上一页
                  </button>
                  <span>
                    {page} / {totalPages}
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
            </div>

            <div className="reminder-manager-editor-pane">
              {creating || (selected && selected.status === "scheduled") ? (
                <form className="reminder-editor" onSubmit={save}>
                  <div className="reminder-editor-heading">
                    <div>
                      {creating ? <Plus size={16} /> : <Pencil size={16} />}
                      <strong>
                        {creating ? "新建一次性提醒" : "编辑与改期"}
                      </strong>
                    </div>
                    {!creating && selected ? (
                      <small>版本 {selected.version}</small>
                    ) : null}
                  </div>
                  <label className="form-field">
                    <span>标题</span>
                    <input
                      autoFocus={creating}
                      disabled={busy}
                      maxLength={200}
                      onChange={(event) =>
                        setDraft((value) => ({
                          ...value,
                          title: event.target.value,
                        }))
                      }
                      placeholder="需要在什么时候提醒？"
                      value={draft.title}
                    />
                  </label>
                  <label className="form-field">
                    <span>说明</span>
                    <textarea
                      disabled={busy}
                      maxLength={10_000}
                      onChange={(event) =>
                        setDraft((value) => ({
                          ...value,
                          summary: event.target.value,
                        }))
                      }
                      placeholder="补充提醒背景（可选）"
                      rows={4}
                      value={draft.summary}
                    />
                  </label>
                  <div className="form-grid">
                    <label className="form-field">
                      <span>优先级</span>
                      <select
                        disabled={busy}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            priority: event.target.value as InboxItemPriority,
                          }))
                        }
                        value={draft.priority}
                      >
                        <option value="P0">P0 · 紧急</option>
                        <option value="P1">P1 · 高</option>
                        <option value="P2">P2 · 普通</option>
                        <option value="P3">P3 · 低</option>
                      </select>
                    </label>
                    <label className="form-field">
                      <span>提醒时间</span>
                      <input
                        disabled={busy}
                        onChange={(event) =>
                          setDraft((value) => ({
                            ...value,
                            triggerAt: event.target.value,
                          }))
                        }
                        type="datetime-local"
                        value={draft.triggerAt}
                      />
                    </label>
                  </div>
                  {operationError ? (
                    <div
                      className="form-error reminder-editor-error"
                      role="alert"
                    >
                      <span>{operationError}</span>
                      {!creating &&
                      (updateMutation.error instanceof ApiError ||
                        cancelMutation.error instanceof ApiError) ? (
                        <button
                          className="button button-secondary"
                          disabled={detailQuery.isFetching}
                          onClick={() => void reloadSelected()}
                          type="button"
                        >
                          {detailQuery.isFetching
                            ? "正在加载…"
                            : "加载最新记录"}
                        </button>
                      ) : null}
                    </div>
                  ) : null}
                  <div className="reminder-editor-actions">
                    {!creating && selected ? (
                      <button
                        className="button button-danger"
                        disabled={busy}
                        onClick={() => {
                          setConfirmingCancel(true);
                          setValidationError(null);
                        }}
                        type="button"
                      >
                        取消提醒
                      </button>
                    ) : (
                      <span />
                    )}
                    <button
                      className="button button-primary"
                      disabled={busy || !draft.title.trim() || !draft.triggerAt}
                      type="submit"
                    >
                      {busy ? "正在保存…" : creating ? "创建提醒" : "保存修改"}
                    </button>
                  </div>
                  {confirmingCancel && selected ? (
                    <div className="reminder-cancel-editor">
                      <strong>保留取消原因</strong>
                      <p>取消会停止到期投影，但不会删除提醒事实。</p>
                      <label className="form-field">
                        <span>取消原因</span>
                        <textarea
                          autoFocus
                          disabled={busy}
                          maxLength={1000}
                          onChange={(event) =>
                            setCancelReason(event.target.value)
                          }
                          rows={3}
                          value={cancelReason}
                        />
                      </label>
                      <div>
                        <button
                          className="button button-secondary"
                          disabled={busy}
                          onClick={() => setConfirmingCancel(false)}
                          type="button"
                        >
                          返回
                        </button>
                        <button
                          className="button button-danger"
                          disabled={busy || !cancelReason.trim()}
                          onClick={confirmCancel}
                          type="button"
                        >
                          {cancelMutation.isPending ? "正在取消…" : "确认取消"}
                        </button>
                      </div>
                    </div>
                  ) : null}
                  <p className="form-note">
                    到期后会生成一条收件箱事项；重复扫描或重启不会重复生成。
                  </p>
                </form>
              ) : detailQuery.isPending && selectedId ? (
                <SkeletonRows count={4} />
              ) : selected ? (
                <div className="reminder-terminal-detail">
                  <span className={`reminder-status-icon ${selected.status}`}>
                    <ReminderStatusIcon status={selected.status} />
                  </span>
                  <div>
                    <small>{statusLabels[selected.status]}</small>
                    <h4>{selected.title}</h4>
                    <p>{selected.summary || "未填写说明。"}</p>
                  </div>
                  <dl>
                    <div>
                      <dt>原提醒时间</dt>
                      <dd>{formatDateTime(selected.triggerAt)}</dd>
                    </div>
                    <div>
                      <dt>优先级</dt>
                      <dd>{selected.priority}</dd>
                    </div>
                    {selected.firedAt ? (
                      <div>
                        <dt>触发时间</dt>
                        <dd>{formatDateTime(selected.firedAt)}</dd>
                      </div>
                    ) : null}
                    {selected.cancelledAt ? (
                      <div>
                        <dt>取消时间</dt>
                        <dd>{formatDateTime(selected.cancelledAt)}</dd>
                      </div>
                    ) : null}
                    {selected.cancelReason ? (
                      <div>
                        <dt>取消原因</dt>
                        <dd>{selected.cancelReason}</dd>
                      </div>
                    ) : null}
                  </dl>
                  {selected.inboxItemId ? (
                    <button
                      className="button button-primary"
                      onClick={() => onOpenInboxItem?.(selected.inboxItemId!)}
                      type="button"
                    >
                      <Inbox size={15} />
                      打开收件箱条目
                    </button>
                  ) : null}
                </div>
              ) : (
                <div className="reminder-manager-placeholder">
                  <CalendarClock size={28} />
                  <strong>选择一条提醒</strong>
                  <p>查看详情，或新建一条只保存在本机的一次性提醒。</p>
                </div>
              )}
            </div>
          </div>
        </section>
      </div>
    </Modal>
  );
}
