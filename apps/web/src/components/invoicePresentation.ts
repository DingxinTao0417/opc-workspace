import { ApiError } from "../api/client";
import type { InvoiceStatus } from "../types/models";

export const invoiceStatusLabels: Record<InvoiceStatus, string> = {
  draft: "草稿",
  sent: "已发送",
  viewed: "已查看",
  paid: "已付款",
  overdue: "已逾期",
};

export function formatInvoiceAmount(
  amountMinor: number,
  currency: string,
): string {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency,
    currencyDisplay: "narrowSymbol",
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

export function invoiceStatusClass(status: InvoiceStatus): string {
  if (status === "paid") return "status-green";
  if (status === "overdue") return "status-red";
  if (status === "sent" || status === "viewed") return "status-purple";
  return "status-neutral";
}

export function isInvoiceVersionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.code === "VERSION_CONFLICT";
}

export function invoiceErrorMessage(
  error: unknown,
  fallback: string,
  conflictMessage = "该发票已在其他窗口更新，请刷新后重新操作。",
): string {
  if (error instanceof ApiError) {
    if (isInvoiceVersionConflict(error)) return conflictMessage;
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return fallback;
}
