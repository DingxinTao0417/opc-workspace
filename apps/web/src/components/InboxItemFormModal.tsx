import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useCreateInboxItem } from "../api/hooks";
import type { InboxItem, InboxItemPriority } from "../types/models";
import { Modal } from "./Modal";

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

function toIsoDate(value: string): string | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

function createErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "收件箱条目创建失败，请重试。";
}

export function InboxItemFormModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated?: (item: InboxItem) => void;
}) {
  const mutation = useCreateInboxItem();
  const initialized = useRef(false);
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [priority, setPriority] = useState<InboxItemPriority>("P2");
  const [dueAt, setDueAt] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      initialized.current = false;
      return;
    }
    if (initialized.current) return;
    initialized.current = true;
    setTitle("");
    setSummary("");
    setPriority("P2");
    setDueAt("");
    setValidationError(null);
    mutation.reset();
  }, [open]);

  const errorMessage = useMemo(
    () => validationError ?? createErrorMessage(mutation.error),
    [mutation.error, validationError],
  );

  const close = () => {
    if (!mutation.isPending) onClose();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
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
    setValidationError(null);
    mutation.mutate(
      {
        title: cleanTitle,
        summary: cleanSummary,
        priority,
        dueAt: dueAtIso,
      },
      {
        onSuccess: (item) => {
          onCreated?.(item);
          onClose();
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
            disabled={mutation.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={!title.trim() || mutation.isPending}
            form="inbox-create-form"
            type="submit"
          >
            {mutation.isPending ? "正在创建…" : "加入收件箱"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title="新建手工条目"
      width="600px"
    >
      <form id="inbox-create-form" onSubmit={submit}>
        <label className="form-field">
          <span>标题</span>
          <input
            autoFocus
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="需要处理什么？"
            value={title}
          />
        </label>

        <label className="form-field">
          <span>说明</span>
          <textarea
            maxLength={10_000}
            onChange={(event) => setSummary(event.target.value)}
            placeholder="补充背景、目标或处理边界（可选）"
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

        {errorMessage ? (
          <div className="form-error" role="alert">
            {errorMessage}
          </div>
        ) : null}
        <p className="form-note">
          该条目只保存在本机；当前不会创建任务、发送消息或触发自动化。
        </p>
      </form>
    </Modal>
  );
}
