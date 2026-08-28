import {
  BellOff,
  BellRing,
  CalendarClock,
  Check,
  Edit3,
  Eye,
  Inbox,
  RotateCcw,
  Save,
  XCircle,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import {
  useInboxItemCommand,
  useInboxItemQuery,
  useUpdateInboxItem,
} from "../api/hooks";
import type {
  InboxItem,
  InboxItemAction,
  InboxItemPriority,
  InboxItemStatus,
} from "../types/models";
import { ErrorState, SkeletonRows } from "./feedback";
import { InboxItemEventsSection } from "./InboxItemEventsSection";
import { InboxItemTasksSection } from "./InboxItemTasksSection";
import { Modal } from "./Modal";

type ActionEditor = "snooze" | "resolve" | "dismiss" | null;

const statusLabels: Record<InboxItemStatus, string> = {
  open: "待处理",
  tracking: "跟进中",
  resolved: "已解决",
  dismissed: "已忽略",
};

function statusClass(status: InboxItemStatus): string {
  if (status === "resolved") return "status-green";
  if (status === "dismissed") return "status-neutral";
  if (status === "tracking") return "status-purple";
  return "status-orange";
}

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

function formatDateTime(value: string | null): string {
  if (!value) return "未设置";
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

function toLocalInput(value: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}

function toIsoDate(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function oneHourLaterLocal(): string {
  return toLocalInput(new Date(Date.now() + 60 * 60 * 1_000).toISOString());
}

function errorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.status === 409 || error.code === "VERSION_CONFLICT") {
      return "条目已在其他窗口变化。最新版本已重新读取，当前草稿仍保留。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "收件箱操作失败，请重试。";
}

function actionAvailable(item: InboxItem, action: InboxItemAction): boolean {
  return item.availableActions.includes(action);
}

export function InboxItemDetailModal({
  itemId,
  onClose,
}: {
  itemId: string | null;
  onClose: () => void;
}) {
  const query = useInboxItemQuery(itemId);
  const updateMutation = useUpdateInboxItem();
  const commandMutation = useInboxItemCommand();
  const initializedFor = useRef<string | null>(null);
  const pendingConflictRefresh = useRef<{
    itemId: string;
    successMessage: string;
  } | null>(null);
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [priority, setPriority] = useState<InboxItemPriority>("P2");
  const [dueAt, setDueAt] = useState("");
  const [observedVersion, setObservedVersion] = useState(1);
  const [actionEditor, setActionEditor] = useState<ActionEditor>(null);
  const [actionValue, setActionValue] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [conflictMessage, setConflictMessage] = useState<string | null>(null);
  const [relationBusy, setRelationBusy] = useState(false);
  const item = query.data;
  const busy =
    updateMutation.isPending || commandMutation.isPending || relationBusy;

  useEffect(() => {
    if (!itemId) {
      initializedFor.current = null;
      pendingConflictRefresh.current = null;
      return;
    }
    if (!item || initializedFor.current === item.id) return;
    initializedFor.current = item.id;
    pendingConflictRefresh.current = null;
    setEditing(false);
    setTitle(item.title);
    setSummary(item.summary);
    setPriority(item.priority);
    setDueAt(toLocalInput(item.dueAt));
    setObservedVersion(item.version);
    setActionEditor(null);
    setActionValue("");
    setValidationError(null);
    setConflictMessage(null);
    setRelationBusy(false);
    updateMutation.reset();
    commandMutation.reset();
  }, [item, itemId]);

  useEffect(() => {
    const pending = pendingConflictRefresh.current;
    if (!item || !pending || pending.itemId !== item.id) return;
    setObservedVersion(item.version);
    setConflictMessage(pending.successMessage);
    pendingConflictRefresh.current = null;
  }, [item?.id, item?.version]);

  const operationError = useMemo(
    () =>
      validationError ??
      conflictMessage ??
      errorMessage(updateMutation.error) ??
      errorMessage(commandMutation.error),
    [
      commandMutation.error,
      conflictMessage,
      updateMutation.error,
      validationError,
    ],
  );

  const close = () => {
    if (!busy) onClose();
  };

  const refreshAfterConflict = (message: string) => {
    const refreshFailedMessage =
      "检测到版本冲突，但未能读取服务器最新版本；草稿已保留，请重试。";
    if (item) {
      pendingConflictRefresh.current = {
        itemId: item.id,
        successMessage: message,
      };
    }
    void query.refetch().then(
      (result) => {
        if (!result.isError && result.data) {
          setObservedVersion(result.data.version);
          setConflictMessage(message);
          pendingConflictRefresh.current = null;
          return;
        }
        setConflictMessage(refreshFailedMessage);
      },
      () => setConflictMessage(refreshFailedMessage),
    );
  };

  const saveEdit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!item) return;
    const cleanTitle = title.trim();
    const cleanSummary = summary.trim();
    if (unicodeLength(cleanTitle) < 2 || unicodeLength(cleanTitle) > 200) {
      setValidationError("标题需要 2–200 个字符。");
      return;
    }
    if (unicodeLength(cleanSummary) > 10_000) {
      setValidationError("说明不能超过 10,000 个字符。");
      return;
    }
    const dueAtIso = toIsoDate(dueAt);
    if (dueAt && !dueAtIso) {
      setValidationError("截止时间格式无效。");
      return;
    }
    const nextDueAt =
      dueAt === toLocalInput(item.dueAt) ? item.dueAt : dueAtIso;
    setValidationError(null);
    setConflictMessage(null);
    updateMutation.mutate(
      {
        id: item.id,
        input: {
          title: cleanTitle,
          summary: cleanSummary,
          priority,
          dueAt: nextDueAt,
          expectedVersion: observedVersion,
        },
      },
      {
        onError: (error) => {
          if (
            error instanceof ApiError &&
            (error.status === 409 || error.code === "VERSION_CONFLICT")
          ) {
            refreshAfterConflict(
              "已加载服务器最新版本，编辑草稿未被覆盖；请检查后再次保存。",
            );
          }
        },
        onSuccess: (updated) => {
          setObservedVersion(updated.version);
          pendingConflictRefresh.current = null;
          setEditing(false);
          setConflictMessage(null);
        },
      },
    );
  };

  const runCommand = (
    input:
      | { action: "read" | "unsnooze" | "reopen" }
      | { action: "snooze"; snoozedUntil: string }
      | { action: "resolve" | "dismiss"; reason: string },
  ) => {
    if (!item) return;
    setValidationError(null);
    setConflictMessage(null);
    commandMutation.mutate(
      { ...input, id: item.id, expectedVersion: item.version },
      {
        onError: (error) => {
          if (
            error instanceof ApiError &&
            (error.status === 409 || error.code === "VERSION_CONFLICT")
          ) {
            refreshAfterConflict(
              "条目已变化，最新状态已重新读取；当前操作内容仍保留，请重新确认。",
            );
          }
        },
        onSuccess: (updated) => {
          setObservedVersion(updated.version);
          pendingConflictRefresh.current = null;
          setActionEditor(null);
          setActionValue("");
          setConflictMessage(null);
        },
      },
    );
  };

  const submitAction = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (actionEditor === "snooze") {
      const snoozedUntil = toIsoDate(actionValue);
      if (!snoozedUntil) {
        setValidationError("稍后时间格式无效。");
        return;
      }
      runCommand({ action: "snooze", snoozedUntil });
      return;
    }
    const reason = actionValue.trim();
    if (!actionEditor || unicodeLength(reason) < 1) {
      setValidationError("请填写原因。");
      return;
    }
    if (unicodeLength(reason) > 2_000) {
      setValidationError("原因不能超过 2,000 个字符。");
      return;
    }
    runCommand({ action: actionEditor, reason });
  };

  const startAction = (action: Exclude<ActionEditor, null>) => {
    pendingConflictRefresh.current = null;
    setEditing(false);
    setActionEditor(action);
    setActionValue(action === "snooze" ? oneHourLaterLocal() : "");
    setValidationError(null);
    setConflictMessage(null);
    updateMutation.reset();
    commandMutation.reset();
  };

  return (
    <Modal
      onClose={close}
      open={Boolean(itemId)}
      title="收件箱详情"
      width="760px"
    >
      {query.isPending ? <SkeletonRows count={7} /> : null}
      {query.isError ? (
        <ErrorState
          message="无法读取该条目，它可能已变化或本地服务暂不可用。"
          onRetry={() => void query.refetch()}
          title="收件箱详情不可用"
        />
      ) : null}

      {item ? (
        <div className="inbox-detail">
          <div className="inbox-detail-heading">
            <span aria-hidden="true" className="inbox-detail-icon">
              <Inbox size={17} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="inbox-detail-title-row">
                <h3>{item.title}</h3>
                <span className={`status-badge ${statusClass(item.status)}`}>
                  {statusLabels[item.status]}
                </span>
              </div>
              <p>手工录入 · 仅保存在本机</p>
            </div>
          </div>

          {editing ? (
            <form className="inbox-detail-editor" onSubmit={saveEdit}>
              <label className="form-field">
                <span>标题</span>
                <input
                  autoFocus
                  maxLength={200}
                  onChange={(event) => setTitle(event.target.value)}
                  value={title}
                />
              </label>
              <label className="form-field">
                <span>说明</span>
                <textarea
                  maxLength={10_000}
                  onChange={(event) => setSummary(event.target.value)}
                  rows={5}
                  value={summary}
                />
              </label>
              <div className="form-grid">
                <label className="form-field">
                  <span>优先级</span>
                  <select
                    onChange={(event) =>
                      setPriority(event.target.value as InboxItemPriority)
                    }
                    value={priority}
                  >
                    <option value="P0">P0 · 紧急</option>
                    <option value="P1">P1 · 高</option>
                    <option value="P2">P2 · 普通</option>
                    <option value="P3">P3 · 低</option>
                  </select>
                </label>
                <label className="form-field">
                  <span>截止时间</span>
                  <input
                    onChange={(event) => setDueAt(event.target.value)}
                    type="datetime-local"
                    value={dueAt}
                  />
                </label>
              </div>
              <div className="inbox-detail-editor-actions">
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => {
                    pendingConflictRefresh.current = null;
                    setEditing(false);
                    setValidationError(null);
                    setConflictMessage(null);
                  }}
                  type="button"
                >
                  取消
                </button>
                <button
                  className="button button-primary"
                  disabled={!title.trim() || busy}
                  type="submit"
                >
                  <Save size={14} />
                  {updateMutation.isPending ? "正在保存…" : "保存修改"}
                </button>
              </div>
            </form>
          ) : (
            <>
              <p className="inbox-detail-summary">
                {item.summary || "尚未填写说明。"}
              </p>
              <dl className="inbox-detail-meta">
                <div>
                  <dt>优先级</dt>
                  <dd>
                    <span
                      className={`inbox-priority inbox-priority-${item.priority.toLowerCase()}`}
                    >
                      {item.priority}
                    </span>
                  </dd>
                </div>
                <div>
                  <dt>截止时间</dt>
                  <dd>{formatDateTime(item.dueAt)}</dd>
                </div>
                <div>
                  <dt>已读状态</dt>
                  <dd>{item.readAt ? formatDateTime(item.readAt) : "未读"}</dd>
                </div>
                <div>
                  <dt>创建时间</dt>
                  <dd>{formatDateTime(item.createdAt)}</dd>
                </div>
                {item.snoozedUntil ? (
                  <div>
                    <dt>稍后至</dt>
                    <dd>{formatDateTime(item.snoozedUntil)}</dd>
                  </div>
                ) : null}
                {item.status === "resolved" ? (
                  <div>
                    <dt>解决原因</dt>
                    <dd>{item.resolutionReason ?? "未记录"}</dd>
                  </div>
                ) : null}
                {item.status === "dismissed" ? (
                  <div>
                    <dt>忽略原因</dt>
                    <dd>{item.dismissReason ?? "未记录"}</dd>
                  </div>
                ) : null}
              </dl>
            </>
          )}

          {!editing ? (
            <section aria-label="条目操作" className="inbox-detail-actions">
              {actionAvailable(item, "read") && !item.readAt ? (
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => runCommand({ action: "read" })}
                  type="button"
                >
                  <Eye size={14} />
                  标为已读
                </button>
              ) : null}
              {actionAvailable(item, "edit") ? (
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => {
                    setTitle(item.title);
                    setSummary(item.summary);
                    setPriority(item.priority);
                    setDueAt(toLocalInput(item.dueAt));
                    setObservedVersion(item.version);
                    setEditing(true);
                    setActionEditor(null);
                    setValidationError(null);
                    setConflictMessage(null);
                  }}
                  type="button"
                >
                  <Edit3 size={14} />
                  编辑
                </button>
              ) : null}
              {actionAvailable(item, "unsnooze") ? (
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => runCommand({ action: "unsnooze" })}
                  type="button"
                >
                  <BellRing size={14} />
                  恢复待处理
                </button>
              ) : null}
              {actionAvailable(item, "snooze") ? (
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => startAction("snooze")}
                  type="button"
                >
                  <BellOff size={14} />
                  稍后处理
                </button>
              ) : null}
              {actionAvailable(item, "resolve") ? (
                <button
                  className="button button-primary"
                  disabled={busy}
                  onClick={() => startAction("resolve")}
                  type="button"
                >
                  <Check size={14} />
                  标记解决
                </button>
              ) : null}
              {actionAvailable(item, "dismiss") ? (
                <button
                  className="button button-danger"
                  disabled={busy}
                  onClick={() => startAction("dismiss")}
                  type="button"
                >
                  <XCircle size={14} />
                  忽略
                </button>
              ) : null}
              {actionAvailable(item, "reopen") ? (
                <button
                  className="button button-primary"
                  disabled={busy}
                  onClick={() => runCommand({ action: "reopen" })}
                  type="button"
                >
                  <RotateCcw size={14} />
                  重新打开
                </button>
              ) : null}
            </section>
          ) : null}

          {actionEditor ? (
            <form className="inbox-action-editor" onSubmit={submitAction}>
              <div>
                <strong>
                  {actionEditor === "snooze"
                    ? "设置恢复时间"
                    : actionEditor === "resolve"
                      ? "确认解决"
                      : "确认忽略"}
                </strong>
                <p>
                  {actionEditor === "snooze"
                    ? "到期后条目会回到待处理视图，不会复制新条目。"
                    : "请留下可审计的原因，之后仍可重新打开。"}
                </p>
              </div>
              {actionEditor === "snooze" ? (
                <label className="form-field form-field-last">
                  <span>稍后至</span>
                  <span className="inbox-datetime-field">
                    <CalendarClock size={14} />
                    <input
                      autoFocus
                      onChange={(event) => setActionValue(event.target.value)}
                      type="datetime-local"
                      value={actionValue}
                    />
                  </span>
                </label>
              ) : (
                <label className="form-field form-field-last">
                  <span>
                    {actionEditor === "resolve" ? "解决原因" : "忽略原因"}
                  </span>
                  <textarea
                    autoFocus
                    maxLength={2_000}
                    onChange={(event) => setActionValue(event.target.value)}
                    placeholder="说明为什么可以结束处理…"
                    rows={3}
                    value={actionValue}
                  />
                </label>
              )}
              <div className="inbox-detail-editor-actions">
                <button
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => {
                    setActionEditor(null);
                    setActionValue("");
                    setValidationError(null);
                    setConflictMessage(null);
                  }}
                  type="button"
                >
                  取消
                </button>
                <button
                  className={
                    actionEditor === "dismiss"
                      ? "button button-danger"
                      : "button button-primary"
                  }
                  disabled={!actionValue.trim() || busy}
                  type="submit"
                >
                  {commandMutation.isPending ? "正在处理…" : "确认"}
                </button>
              </div>
            </form>
          ) : null}

          {operationError ? (
            <div className="form-error" role="alert">
              {operationError}
            </div>
          ) : null}

          <InboxItemTasksSection
            disabled={editing || actionEditor !== null || busy}
            item={item}
            onBusyChange={setRelationBusy}
            onRefreshItem={async () => {
              const result = await query.refetch();
              if (result.isError || !result.data) {
                throw result.error ?? new Error("无法刷新收件箱条目");
              }
              return result.data;
            }}
          />

          <InboxItemEventsSection itemId={item.id} />
        </div>
      ) : null}
    </Modal>
  );
}
