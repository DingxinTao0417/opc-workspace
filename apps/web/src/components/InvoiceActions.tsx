import {
  CircleDollarSign,
  Eye,
  MoreHorizontal,
  Pencil,
  Send,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useDeleteInvoice, useTransitionInvoice } from "../api/hooks";
import type { Invoice, InvoiceTransitionAction } from "../types/models";
import { Modal } from "./Modal";
import {
  formatInvoiceAmount,
  invoiceErrorMessage,
  isInvoiceVersionConflict,
} from "./invoicePresentation";

type InvoiceUserCommand =
  Exclude<InvoiceTransitionAction, "mark_overdue"> | "delete";

function localDateValue(date = new Date()): string {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 10);
}

function commandTitle(command: InvoiceUserCommand | null): string {
  if (command === "delete") return "删除发票草稿";
  if (command === "mark_sent") return "确认已发送";
  if (command === "mark_viewed") return "确认客户已查看";
  if (command === "mark_paid") return "登记付款";
  return "确认发票操作";
}

export function InvoiceActions({
  invoice,
  variant,
  menuOpen = false,
  onMenuOpenChange,
  onEdit,
  onDeleted,
  onTransitioned,
  onConflict,
}: {
  invoice: Invoice;
  variant: "menu" | "detail";
  menuOpen?: boolean;
  onMenuOpenChange?: (open: boolean) => void;
  onEdit?: (invoice: Invoice) => void;
  onDeleted?: () => void;
  onTransitioned?: (invoice: Invoice) => void;
  onConflict?: () => void | Promise<void>;
}) {
  const transitionMutation = useTransitionInvoice();
  const deleteMutation = useDeleteInvoice();
  const [command, setCommand] = useState<InvoiceUserCommand | null>(null);
  const [paidDate, setPaidDate] = useState(localDateValue());
  const commandBusy = transitionMutation.isPending || deleteMutation.isPending;
  const commandError =
    command === "delete" ? deleteMutation.error : transitionMutation.error;

  useEffect(() => {
    if (transitionMutation.isPending || deleteMutation.isPending) return;
    setCommand(null);
    onMenuOpenChange?.(false);
    transitionMutation.reset();
    deleteMutation.reset();
  }, [invoice.id, invoice.version]);

  const openCommand = (action: InvoiceUserCommand) => {
    transitionMutation.reset();
    deleteMutation.reset();
    setPaidDate(localDateValue());
    setCommand(action);
    onMenuOpenChange?.(false);
  };

  const edit = () => {
    onMenuOpenChange?.(false);
    onEdit?.(invoice);
  };

  const closeCommand = () => {
    if (!commandBusy) setCommand(null);
  };

  const handleConflict = (error: unknown) => {
    if (!isInvoiceVersionConflict(error) || !onConflict) return;
    setCommand(null);
    void onConflict();
  };

  const confirmCommand = async () => {
    if (!command) return;
    try {
      if (command === "delete") {
        await deleteMutation.mutateAsync({
          id: invoice.id,
          expectedVersion: invoice.version,
        });
        setCommand(null);
        onDeleted?.();
        return;
      }
      const updatedInvoice = await transitionMutation.mutateAsync({
        id: invoice.id,
        input: {
          action: command,
          expectedVersion: invoice.version,
          ...(command === "mark_paid" ? { paidDate } : {}),
        },
      });
      setCommand(null);
      onTransitioned?.(updatedInvoice);
    } catch (error) {
      handleConflict(error);
    }
  };

  const actionButtons = (
    <>
      {invoice.status === "draft" ? (
        <>
          <button
            className="button button-secondary"
            onClick={edit}
            type="button"
          >
            <Pencil size={13} /> 编辑草稿
          </button>
          <button
            className="button button-primary"
            onClick={() => openCommand("mark_sent")}
            type="button"
          >
            <Send size={13} /> 确认已发送
          </button>
          <button
            className="button button-danger"
            onClick={() => openCommand("delete")}
            type="button"
          >
            <Trash2 size={13} /> 删除草稿
          </button>
        </>
      ) : null}
      {invoice.status === "sent" ? (
        <button
          className="button button-primary"
          onClick={() => openCommand("mark_viewed")}
          type="button"
        >
          <Eye size={13} /> 确认已查看
        </button>
      ) : null}
      {invoice.status === "viewed" || invoice.status === "overdue" ? (
        <button
          className="button button-primary"
          onClick={() => openCommand("mark_paid")}
          type="button"
        >
          <CircleDollarSign size={13} /> 登记付款
        </button>
      ) : null}
      {invoice.status === "paid" ? (
        <span className="invoice-readonly-mark">已付款发票只读</span>
      ) : null}
    </>
  );

  return (
    <>
      {variant === "menu" ? (
        invoice.status !== "paid" ? (
          <div className="invoice-row-actions">
            <button
              aria-label={`打开 ${invoice.invoiceNumber} 操作`}
              className="icon-button"
              onClick={() => onMenuOpenChange?.(!menuOpen)}
              type="button"
            >
              <MoreHorizontal size={15} />
            </button>
            {menuOpen ? (
              <div className="invoice-action-menu">
                {invoice.status === "draft" ? (
                  <>
                    <button onClick={edit} type="button">
                      <Pencil size={13} /> 编辑
                    </button>
                    <button
                      onClick={() => openCommand("mark_sent")}
                      type="button"
                    >
                      <Send size={13} /> 确认已发送
                    </button>
                    <button
                      className="danger-text"
                      onClick={() => openCommand("delete")}
                      type="button"
                    >
                      <Trash2 size={13} /> 删除
                    </button>
                  </>
                ) : null}
                {invoice.status === "sent" ? (
                  <button
                    onClick={() => openCommand("mark_viewed")}
                    type="button"
                  >
                    <Eye size={13} /> 确认已查看
                  </button>
                ) : null}
                {invoice.status === "viewed" || invoice.status === "overdue" ? (
                  <button
                    onClick={() => openCommand("mark_paid")}
                    type="button"
                  >
                    <CircleDollarSign size={13} /> 登记付款
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : (
          <span className="invoice-readonly-mark">只读</span>
        )
      ) : (
        <div aria-label="发票操作" className="invoice-detail-actions">
          {actionButtons}
        </div>
      )}

      <Modal
        footer={
          <>
            <button
              className="button button-secondary"
              disabled={commandBusy}
              onClick={closeCommand}
              type="button"
            >
              取消
            </button>
            <button
              className={
                command === "delete"
                  ? "button button-danger"
                  : "button button-primary"
              }
              disabled={
                commandBusy ||
                (command === "mark_paid" &&
                  (!paidDate || paidDate < invoice.issueDate))
              }
              onClick={confirmCommand}
              type="button"
            >
              {commandBusy
                ? "正在处理…"
                : command === "delete"
                  ? "确认删除"
                  : command === "mark_paid"
                    ? "确认付款"
                    : "确认"}
            </button>
          </>
        }
        onClose={closeCommand}
        open={Boolean(command)}
        title={commandTitle(command)}
        width="480px"
      >
        {command === "delete" ? (
          <p className="modal-copy">
            将永久删除草稿 {invoice.invoiceNumber}。此操作无法恢复。
          </p>
        ) : null}
        {command === "mark_sent" ? (
          <p className="modal-copy">
            确认后只记录“已发送”状态；应用不会替你发送邮件或生成 PDF。
          </p>
        ) : null}
        {command === "mark_viewed" ? (
          <p className="modal-copy">请在已从客户处确认查看后再更新状态。</p>
        ) : null}
        {command === "mark_paid" ? (
          <>
            <p className="modal-copy">
              请核对以下本地付款事实。确认后发票将变为只读。
            </p>
            <dl aria-label="付款确认摘要" className="invoice-payment-summary">
              <div>
                <dt>发票</dt>
                <dd className="invoice-detail-code">{invoice.invoiceNumber}</dd>
              </div>
              <div>
                <dt>客户</dt>
                <dd>{invoice.clientName}</dd>
              </div>
              <div>
                <dt>付款金额</dt>
                <dd>
                  {formatInvoiceAmount(invoice.amountMinor, invoice.currency)} ·{" "}
                  {invoice.currency}
                </dd>
              </div>
              <div>
                <dt>入账结果</dt>
                <dd>将创建一条已确认收入记录</dd>
              </div>
            </dl>
            <label className="form-field form-field-last">
              <span>付款日期</span>
              <input
                aria-label="付款日期"
                autoFocus
                disabled={commandBusy}
                max={localDateValue()}
                min={invoice.issueDate}
                onChange={(event) => setPaidDate(event.target.value)}
                type="date"
                value={paidDate}
              />
            </label>
          </>
        ) : null}
        {commandError ? (
          <div className="form-error" role="alert">
            {invoiceErrorMessage(
              commandError,
              "发票操作失败，请重试。",
              "该发票已在其他窗口更新，请刷新后重新操作。",
            )}
          </div>
        ) : null}
      </Modal>
    </>
  );
}
