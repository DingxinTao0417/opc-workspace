import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useCreateClient, useUpdateClient } from "../api/hooks";
import type { Client, ClientStatus } from "../types/models";
import { Modal } from "./Modal";

const statusLabels: Record<ClientStatus, string> = {
  active: "活跃",
  lead: "潜在客户",
  inactive: "已停用",
};

function optionalValue(value: string): string | null {
  const clean = value.trim();
  return clean || null;
}

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

function apiErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "客户资料已变化，草稿已保留。请确认最新版本后再次保存。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "客户保存失败，请重试。";
}

export function ClientFormModal({
  open,
  client,
  onClose,
  onVersionConflict,
}: {
  open: boolean;
  client?: Client;
  onClose: () => void;
  onVersionConflict?: () => Promise<Client | undefined>;
}) {
  const createMutation = useCreateClient();
  const updateMutation = useUpdateClient();
  const initializedFor = useRef<string | null>(null);
  const [name, setName] = useState("");
  const [contactName, setContactName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [notes, setNotes] = useState("");
  const [status, setStatus] = useState<ClientStatus>("active");
  const [observedVersion, setObservedVersion] = useState(1);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [conflictMessage, setConflictMessage] = useState<string | null>(null);
  const busy = createMutation.isPending || updateMutation.isPending;

  useEffect(() => {
    if (!open) {
      initializedFor.current = null;
      return;
    }
    const key = client?.id ?? "new";
    if (initializedFor.current === key) return;
    initializedFor.current = key;
    setName(client?.name ?? "");
    setContactName(client?.contactName ?? "");
    setEmail(client?.email ?? "");
    setPhone(client?.phone ?? "");
    setNotes(client?.notes ?? "");
    setStatus(client?.status ?? "active");
    setObservedVersion(client?.version ?? 1);
    setValidationError(null);
    setConflictMessage(null);
    createMutation.reset();
    updateMutation.reset();
  }, [client, open]);

  const errorMessage = useMemo(
    () =>
      validationError ??
      conflictMessage ??
      apiErrorMessage(createMutation.error) ??
      apiErrorMessage(updateMutation.error),
    [
      conflictMessage,
      createMutation.error,
      updateMutation.error,
      validationError,
    ],
  );

  const close = () => {
    if (!busy) onClose();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanName = name.trim();
    const cleanEmail = email.trim();
    if (unicodeLength(cleanName) < 1 || unicodeLength(cleanName) > 200) {
      setValidationError("客户名称需要 1–200 个字符。");
      return;
    }
    if (unicodeLength(contactName.trim()) > 200) {
      setValidationError("联系人不能超过 200 个字符。");
      return;
    }
    if (
      cleanEmail &&
      (unicodeLength(cleanEmail) > 320 || !/^[^\s@]+@[^\s@]+$/.test(cleanEmail))
    ) {
      setValidationError("请输入有效的邮箱地址。");
      return;
    }
    if (unicodeLength(phone.trim()) > 50) {
      setValidationError("电话不能超过 50 个字符。");
      return;
    }
    if (unicodeLength(notes.trim()) > 10_000) {
      setValidationError("备注不能超过 10,000 个字符。");
      return;
    }

    setValidationError(null);
    setConflictMessage(null);
    const input = {
      name: cleanName,
      contactName: optionalValue(contactName),
      email: optionalValue(email),
      phone: optionalValue(phone),
      notes: optionalValue(notes),
      status,
    };
    if (!client) {
      createMutation.mutate(input, { onSuccess: onClose });
      return;
    }
    updateMutation.mutate(
      {
        id: client.id,
        input: { ...input, expectedVersion: observedVersion },
      },
      {
        onError: (error) => {
          if (
            !(error instanceof ApiError) ||
            error.code !== "VERSION_CONFLICT"
          ) {
            return;
          }
          void (async () => {
            const latest = await onVersionConflict?.();
            if (latest) setObservedVersion(latest.version);
            setConflictMessage(
              latest
                ? `已加载版本 ${latest.version}，当前草稿未被覆盖；请检查后再次保存。`
                : "客户资料已变化，当前草稿未被覆盖；请刷新详情后再次保存。",
            );
          })();
        },
        onSuccess: onClose,
      },
    );
  };

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={!name.trim() || busy}
            form="client-form"
            type="submit"
          >
            {busy ? "正在保存…" : client ? "保存修改" : "创建客户"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title={client ? "编辑客户" : "新建客户"}
      width="600px"
    >
      <form id="client-form" onSubmit={submit}>
        <label className="form-field">
          <span>客户名称</span>
          <input
            autoFocus
            onChange={(event) => setName(event.target.value)}
            placeholder="公司或个人名称"
            value={name}
          />
        </label>

        <div className="form-grid">
          <label className="form-field">
            <span>联系人</span>
            <input
              onChange={(event) => setContactName(event.target.value)}
              placeholder="对接人姓名（可选）"
              value={contactName}
            />
          </label>
          <label className="form-field">
            <span>状态</span>
            <select
              onChange={(event) =>
                setStatus(event.target.value as ClientStatus)
              }
              value={status}
            >
              {Object.entries(statusLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="form-grid">
          <label className="form-field">
            <span>邮箱</span>
            <input
              onChange={(event) => setEmail(event.target.value)}
              placeholder="name@example.com（可选）"
              type="email"
              value={email}
            />
          </label>
          <label className="form-field">
            <span>电话</span>
            <input
              onChange={(event) => setPhone(event.target.value)}
              placeholder="手机号或座机（可选）"
              type="tel"
              value={phone}
            />
          </label>
        </div>

        <label className="form-field form-field-last">
          <span>备注</span>
          <textarea
            onChange={(event) => setNotes(event.target.value)}
            placeholder="记录合作背景、偏好或边界…"
            rows={5}
            value={notes}
          />
        </label>

        {errorMessage ? (
          <div className="form-error" role="alert">
            {errorMessage}
          </div>
        ) : null}
        <p className="form-note">
          客户联系人只保存在本机，不会自动成为任务负责人，也不会收到消息。
        </p>
      </form>
    </Modal>
  );
}
