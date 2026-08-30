import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useCreateFinancialEntry, useUpdateFinancialEntry } from "../api/hooks";
import type {
  FinancialEntry,
  FinancialEntryInput,
  FinancialEntryStatus,
  FinancialEntryType,
} from "../types/models";
import { ClientSelect } from "./ClientSelect";
import { Modal } from "./Modal";
import { ProjectSelect } from "./ProjectSelect";

const currencies = ["CNY", "USD", "EUR", "HKD"] as const;
const maximumAmountMinor = 9_000_000_000_000_000;

function localDateValue(date = new Date()): string {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 10);
}

export function amountMinorToInput(amountMinor: number): string {
  const whole = Math.floor(amountMinor / 100);
  const fraction = amountMinor % 100;
  return fraction === 0
    ? String(whole)
    : `${whole}.${String(fraction).padStart(2, "0")}`;
}

export function parseFinancialAmountMinor(value: string): number | null {
  const clean = value.trim();
  if (!/^\d+(?:\.\d{1,2})?$/.test(clean)) return null;
  const [whole, fraction = ""] = clean.split(".");
  const amount = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
  return Number.isSafeInteger(amount) &&
    amount > 0 &&
    amount <= maximumAmountMinor
    ? amount
    : null;
}

function errorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "该记录已在其他窗口修改，请关闭后重新打开再试。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "财务记录保存失败，请重试。";
}

export function FinancialEntryFormModal({
  open,
  entry,
  initialType = "income",
  onClose,
}: {
  open: boolean;
  entry?: FinancialEntry;
  initialType?: FinancialEntryType;
  onClose: () => void;
}) {
  const createMutation = useCreateFinancialEntry();
  const updateMutation = useUpdateFinancialEntry();
  const [type, setType] = useState<FinancialEntryType>(initialType);
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("CNY");
  const [occurredOn, setOccurredOn] = useState(localDateValue());
  const [status, setStatus] =
    useState<Exclude<FinancialEntryStatus, "voided">>("confirmed");
  const [category, setCategory] = useState("");
  const [clientId, setClientId] = useState("");
  const [projectId, setProjectId] = useState("");
  const [notes, setNotes] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const busy = createMutation.isPending || updateMutation.isPending;

  useEffect(() => {
    if (!open) return;
    setType(entry?.type ?? initialType);
    setAmount(entry ? amountMinorToInput(entry.amountMinor) : "");
    setCurrency(entry?.currency ?? "CNY");
    setOccurredOn(entry?.occurredOn ?? localDateValue());
    setStatus(entry?.status === "pending" ? "pending" : "confirmed");
    setCategory(entry?.category ?? "");
    setClientId(entry?.clientId ?? "");
    setProjectId(entry?.projectId ?? "");
    setNotes(entry?.notes ?? "");
    setValidationError(null);
    createMutation.reset();
    updateMutation.reset();
  }, [entry, initialType, open]);

  const mutationError = useMemo(
    () =>
      errorMessage(createMutation.error) ?? errorMessage(updateMutation.error),
    [createMutation.error, updateMutation.error],
  );

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const amountMinor = parseFinancialAmountMinor(amount);
    if (amountMinor === null) {
      setValidationError("金额必须大于 0，最多保留两位小数。 ");
      return;
    }
    const cleanCategory = category.trim();
    if (!cleanCategory || cleanCategory.length > 80) {
      setValidationError("分类需要 1–80 个字符。 ");
      return;
    }
    if (!/^\d{4}-\d{2}-\d{2}$/.test(occurredOn)) {
      setValidationError("请选择有效的发生日期。 ");
      return;
    }
    setValidationError(null);
    const input: FinancialEntryInput = {
      type,
      amountMinor,
      currency,
      occurredOn,
      status,
      category: cleanCategory,
      clientId: clientId || null,
      projectId: projectId || null,
      notes,
    };
    if (entry) {
      updateMutation.mutate(
        { id: entry.id, input: { ...input, expectedVersion: entry.version } },
        { onSuccess: onClose },
      );
      return;
    }
    createMutation.mutate(input, { onSuccess: onClose });
  };

  const close = () => {
    if (!busy) onClose();
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
            disabled={busy || !amount.trim() || !category.trim()}
            form="financial-entry-form"
            type="submit"
          >
            {busy ? "正在保存…" : entry ? "保存修改" : "创建记录"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title={entry ? "编辑财务记录" : "新建财务记录"}
      width="640px"
    >
      <form id="financial-entry-form" onSubmit={submit}>
        <div className="form-grid">
          <label className="form-field">
            <span>类型</span>
            <select
              aria-label="类型"
              autoFocus
              onChange={(event) =>
                setType(event.target.value as FinancialEntryType)
              }
              value={type}
            >
              <option value="income">收入</option>
              <option value="expense">支出</option>
            </select>
          </label>
          <label className="form-field">
            <span>状态</span>
            <select
              aria-label="状态"
              onChange={(event) =>
                setStatus(
                  event.target.value as Exclude<FinancialEntryStatus, "voided">,
                )
              }
              value={status}
            >
              <option value="confirmed">已确认</option>
              <option value="pending">待确认</option>
            </select>
          </label>
        </div>

        <div className="form-grid">
          <label className="form-field">
            <span>金额</span>
            <input
              aria-label="金额"
              inputMode="decimal"
              onChange={(event) => setAmount(event.target.value)}
              placeholder="0.00"
              value={amount}
            />
          </label>
          <label className="form-field">
            <span>币种</span>
            <select
              aria-label="币种"
              onChange={(event) => setCurrency(event.target.value)}
              value={currency}
            >
              {currencies.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="form-grid">
          <label className="form-field">
            <span>发生日期</span>
            <input
              aria-label="发生日期"
              onChange={(event) => setOccurredOn(event.target.value)}
              type="date"
              value={occurredOn}
            />
          </label>
          <label className="form-field">
            <span>分类</span>
            <input
              aria-label="分类"
              maxLength={80}
              onChange={(event) => setCategory(event.target.value)}
              placeholder={
                type === "income" ? "例如：项目回款" : "例如：软件订阅"
              }
              value={category}
            />
          </label>
        </div>

        <div className="form-grid">
          <div className="form-field">
            <span>客户</span>
            <ClientSelect
              ariaLabel="客户"
              emptyLabel="不关联客户"
              onChange={setClientId}
              selectedName={entry?.clientName}
              value={clientId}
              variant="form"
            />
          </div>
          <div className="form-field">
            <span>项目</span>
            <ProjectSelect
              ariaLabel="项目"
              emptyLabel="不关联项目"
              onChange={setProjectId}
              selectedName={entry?.projectName}
              value={projectId}
              variant="form"
            />
          </div>
        </div>

        <label className="form-field form-field-last">
          <span>备注</span>
          <textarea
            maxLength={10_000}
            onChange={(event) => setNotes(event.target.value)}
            placeholder="可记录付款渠道、凭证或补充说明…"
            rows={3}
            value={notes}
          />
        </label>

        {(validationError ?? mutationError) ? (
          <div className="form-error" role="alert">
            {validationError ?? mutationError}
          </div>
        ) : null}
        <p className="form-note">
          金额按两位小数精确保存；选择项目但未选择客户时，系统会自动继承项目客户。
        </p>
      </form>
    </Modal>
  );
}
