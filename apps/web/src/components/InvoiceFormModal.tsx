import { useEffect, useMemo, useState, type FormEvent } from "react";
import { ApiError } from "../api/client";
import { useCreateInvoice, useUpdateInvoice } from "../api/hooks";
import type { Invoice, InvoiceInput } from "../types/models";
import {
  amountMinorToInput,
  parseFinancialAmountMinor,
} from "./FinancialEntryFormModal";
import { ClientSelect } from "./ClientSelect";
import { Modal } from "./Modal";
import { ProjectSelect } from "./ProjectSelect";

const currencies = ["CNY", "USD", "EUR", "HKD"] as const;

function localDateValue(date = new Date()): string {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 10);
}

function defaultDueDate(): string {
  const due = new Date();
  due.setDate(due.getDate() + 30);
  return localDateValue(due);
}

function validDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return (
    !Number.isNaN(parsed.getTime()) &&
    parsed.toISOString().slice(0, 10) === value
  );
}

function invoiceErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "该发票已在其他窗口修改，请关闭后重新打开再试。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "发票保存失败，请重试。";
}

export function InvoiceFormModal({
  open,
  invoice,
  onClose,
}: {
  open: boolean;
  invoice?: Invoice;
  onClose: () => void;
}) {
  const createMutation = useCreateInvoice();
  const updateMutation = useUpdateInvoice();
  const [clientId, setClientId] = useState("");
  const [projectId, setProjectId] = useState("");
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("CNY");
  const [issueDate, setIssueDate] = useState(localDateValue());
  const [dueDate, setDueDate] = useState(defaultDueDate());
  const [notes, setNotes] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const busy = createMutation.isPending || updateMutation.isPending;

  useEffect(() => {
    if (!open) return;
    setClientId(invoice?.clientId ?? "");
    setProjectId(invoice?.projectId ?? "");
    setAmount(invoice ? amountMinorToInput(invoice.amountMinor) : "");
    setCurrency(invoice?.currency ?? "CNY");
    setIssueDate(invoice?.issueDate ?? localDateValue());
    setDueDate(invoice?.dueDate ?? defaultDueDate());
    setNotes(invoice?.notes ?? "");
    setValidationError(null);
    createMutation.reset();
    updateMutation.reset();
  }, [invoice, open]);

  const mutationError = useMemo(
    () =>
      invoiceErrorMessage(createMutation.error) ??
      invoiceErrorMessage(updateMutation.error),
    [createMutation.error, updateMutation.error],
  );

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!clientId) {
      setValidationError("请选择客户。 ");
      return;
    }
    const amountMinor = parseFinancialAmountMinor(amount);
    if (amountMinor === null) {
      setValidationError("金额必须大于 0，最多保留两位小数。 ");
      return;
    }
    if (!validDate(issueDate) || !validDate(dueDate)) {
      setValidationError("请选择有效的开票日期和到期日期。 ");
      return;
    }
    if (dueDate < issueDate) {
      setValidationError("到期日期不能早于开票日期。 ");
      return;
    }
    if (notes.length > 10_000) {
      setValidationError("备注不能超过 10000 个字符。 ");
      return;
    }
    setValidationError(null);
    const input: InvoiceInput = {
      clientId,
      projectId: projectId || null,
      amountMinor,
      currency,
      issueDate,
      dueDate,
      notes,
    };
    if (invoice) {
      updateMutation.mutate(
        {
          id: invoice.id,
          input: { ...input, expectedVersion: invoice.version },
        },
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
            disabled={busy || !clientId || !amount.trim()}
            form="invoice-form"
            type="submit"
          >
            {busy ? "正在保存…" : invoice ? "保存修改" : "创建草稿"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title={invoice ? `编辑 ${invoice.invoiceNumber}` : "新建发票"}
      width="640px"
    >
      <form id="invoice-form" onSubmit={submit}>
        <div className="form-grid">
          <div className="form-field">
            <span>客户 *</span>
            <ClientSelect
              ariaLabel="发票客户"
              emptyLabel="选择客户"
              onChange={(nextClientId) => {
                if (nextClientId !== clientId) setProjectId("");
                setClientId(nextClientId);
              }}
              selectedName={
                clientId === invoice?.clientId ? invoice.clientName : undefined
              }
              value={clientId}
              variant="form"
            />
          </div>
          <div className="form-field">
            <span>项目</span>
            <ProjectSelect
              ariaLabel="发票项目"
              clientId={clientId || undefined}
              disabled={!clientId}
              emptyLabel={clientId ? "不关联项目" : "请先选择客户"}
              onChange={setProjectId}
              selectedName={
                projectId === invoice?.projectId
                  ? invoice.projectName
                  : undefined
              }
              value={projectId}
              variant="form"
            />
          </div>
        </div>

        <div className="form-grid">
          <label className="form-field">
            <span>金额 *</span>
            <input
              aria-label="发票金额"
              autoFocus
              inputMode="decimal"
              onChange={(event) => setAmount(event.target.value)}
              placeholder="0.00"
              value={amount}
            />
          </label>
          <label className="form-field">
            <span>币种</span>
            <select
              aria-label="发票币种"
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
            <span>开票日期</span>
            <input
              aria-label="开票日期"
              onChange={(event) => setIssueDate(event.target.value)}
              type="date"
              value={issueDate}
            />
          </label>
          <label className="form-field">
            <span>到期日期</span>
            <input
              aria-label="到期日期"
              min={issueDate}
              onChange={(event) => setDueDate(event.target.value)}
              type="date"
              value={dueDate}
            />
          </label>
        </div>

        <label className="form-field form-field-last">
          <span>备注</span>
          <textarea
            aria-label="发票备注"
            maxLength={10_000}
            onChange={(event) => setNotes(event.target.value)}
            placeholder="可记录付款条款或交付说明…"
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
          发票编号将在保存时自动生成。新发票始终以草稿创建，发送与付款需要单独确认。
        </p>
      </form>
    </Modal>
  );
}
