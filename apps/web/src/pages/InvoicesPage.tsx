import {
  CircleDollarSign,
  Eye,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Send,
  Trash2,
} from "lucide-react";
import { useDeferredValue, useState } from "react";
import { ApiError } from "../api/client";
import {
  useDeleteInvoice,
  useInvoicesQuery,
  useTransitionInvoice,
} from "../api/hooks";
import { InvoiceFormModal } from "../components/InvoiceFormModal";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import type {
  Invoice,
  InvoiceStatus,
  InvoiceTransitionAction,
} from "../types/models";

const statusLabels: Record<InvoiceStatus, string> = {
  draft: "草稿",
  sent: "已发送",
  viewed: "已查看",
  paid: "已付款",
  overdue: "已逾期",
};

type InvoiceCommand = {
  invoice: Invoice;
  action: InvoiceTransitionAction | "delete";
};

function localDateValue(date = new Date()): string {
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 10);
}

function formatAmount(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency,
    currencyDisplay: "narrowSymbol",
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

function statusClass(status: InvoiceStatus): string {
  if (status === "paid") return "status-green";
  if (status === "overdue") return "status-red";
  if (status === "sent" || status === "viewed") return "status-purple";
  return "status-neutral";
}

function apiErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "该发票已在其他窗口更新，列表正在刷新，请重新确认后再试。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return fallback;
}

function commandTitle(command: InvoiceCommand | null): string {
  if (!command) return "确认发票操作";
  if (command.action === "delete") return "删除发票草稿";
  if (command.action === "mark_sent") return "确认已发送";
  if (command.action === "mark_viewed") return "确认客户已查看";
  return "登记付款";
}

export function InvoicesPage() {
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search.trim());
  const [status, setStatus] = useState<InvoiceStatus | "">("");
  const [currency, setCurrency] = useState("");
  const [page, setPage] = useState(1);
  const [formInvoice, setFormInvoice] = useState<Invoice | null | undefined>(
    undefined,
  );
  const [actionMenu, setActionMenu] = useState<string | null>(null);
  const [command, setCommand] = useState<InvoiceCommand | null>(null);
  const [paidDate, setPaidDate] = useState(localDateValue());
  const invoicesQuery = useInvoicesQuery({
    page,
    pageSize: 20,
    q: deferredSearch || undefined,
    status: status || undefined,
    currency: currency || undefined,
    sort: "-issue_date,-created_at",
  });
  const sentCountQuery = useInvoicesQuery({
    page: 1,
    pageSize: 1,
    status: "sent",
  });
  const viewedCountQuery = useInvoicesQuery({
    page: 1,
    pageSize: 1,
    status: "viewed",
  });
  const paidCountQuery = useInvoicesQuery({
    page: 1,
    pageSize: 1,
    status: "paid",
  });
  const overdueCountQuery = useInvoicesQuery({
    page: 1,
    pageSize: 1,
    status: "overdue",
  });
  const transitionMutation = useTransitionInvoice();
  const deleteMutation = useDeleteInvoice();
  const invoices = invoicesQuery.data?.items ?? [];
  const total = invoicesQuery.data?.meta.total ?? 0;
  const totalPages = Math.max(
    1,
    Math.ceil(total / (invoicesQuery.data?.meta.pageSize ?? 20)),
  );
  const hasFilters = Boolean(search.trim() || status || currency);
  const sentCount = sentCountQuery.data?.meta.total;
  const viewedCount = viewedCountQuery.data?.meta.total;
  const receivableCount =
    sentCount === undefined || viewedCount === undefined
      ? null
      : sentCount + viewedCount;
  const receivableCountError =
    sentCountQuery.isError || viewedCountQuery.isError;
  const commandBusy = transitionMutation.isPending || deleteMutation.isPending;
  const commandError =
    command?.action === "delete"
      ? deleteMutation.error
      : transitionMutation.error;

  const openCommand = (invoice: Invoice, action: InvoiceCommand["action"]) => {
    transitionMutation.reset();
    deleteMutation.reset();
    setPaidDate(localDateValue());
    setCommand({ invoice, action });
    setActionMenu(null);
  };

  const closeCommand = () => {
    if (!commandBusy) setCommand(null);
  };

  const confirmCommand = () => {
    if (!command) return;
    if (command.action === "delete") {
      deleteMutation.mutate(
        {
          id: command.invoice.id,
          expectedVersion: command.invoice.version,
        },
        { onSuccess: () => setCommand(null) },
      );
      return;
    }
    transitionMutation.mutate(
      {
        id: command.invoice.id,
        input: {
          action: command.action,
          expectedVersion: command.invoice.version,
          ...(command.action === "mark_paid" ? { paidDate } : {}),
        },
      },
      { onSuccess: () => setCommand(null) },
    );
  };

  const clearFilters = () => {
    setSearch("");
    setStatus("");
    setCurrency("");
    setPage(1);
  };

  return (
    <div className="page invoices-page">
      <PageHeader
        actions={
          <button
            className="button button-primary"
            onClick={() => setFormInvoice(null)}
            type="button"
          >
            <Plus size={15} />
            新建发票
          </button>
        }
        meta={
          <span className="page-count">
            {invoicesQuery.isPending ? "读取中" : `${total} 张`}
          </span>
        }
        title="发票"
      />

      <div className="toolbar invoices-toolbar">
        <label className="toolbar-search">
          <Search size={14} />
          <span className="sr-only">搜索发票</span>
          <input
            aria-label="搜索发票"
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="搜索发票编号、客户或项目"
            value={search}
          />
        </label>
        <label className="toolbar-select">
          <span className="sr-only">发票状态</span>
          <select
            aria-label="发票状态"
            onChange={(event) => {
              setStatus(event.target.value as InvoiceStatus | "");
              setPage(1);
            }}
            value={status}
          >
            <option value="">全部状态</option>
            <option value="draft">草稿</option>
            <option value="sent">已发送</option>
            <option value="viewed">已查看</option>
            <option value="overdue">已逾期</option>
            <option value="paid">已付款</option>
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">发票币种</span>
          <select
            aria-label="发票币种"
            onChange={(event) => {
              setCurrency(event.target.value);
              setPage(1);
            }}
            value={currency}
          >
            <option value="">全部币种</option>
            <option value="CNY">CNY</option>
            <option value="USD">USD</option>
            <option value="EUR">EUR</option>
            <option value="HKD">HKD</option>
          </select>
        </label>
      </div>

      <section aria-label="发票状态概览" className="invoice-stat-grid">
        <article className="kpi-card invoice-stat-card">
          <span>待收发票（未逾期）</span>
          <strong>
            {receivableCount === null ? "—" : `${receivableCount} 张`}
          </strong>
          <small>
            {receivableCountError
              ? "全量状态计数暂不可用"
              : sentCount === undefined || viewedCount === undefined
                ? "正在读取全量状态计数"
                : `已发送 ${sentCount} 张 · 已查看 ${viewedCount} 张`}
          </small>
        </article>
        <article className="kpi-card invoice-stat-card">
          <span>已付款发票</span>
          <strong>
            {paidCountQuery.data ? `${paidCountQuery.data.meta.total} 张` : "—"}
          </strong>
          <small>
            {paidCountQuery.isError
              ? "全量状态计数暂不可用"
              : "全量已付款状态计数"}
          </small>
        </article>
        <article className="kpi-card invoice-stat-card">
          <span>逾期发票</span>
          <strong className="invoice-stat-overdue">
            {overdueCountQuery.data
              ? `${overdueCountQuery.data.meta.total} 张`
              : "—"}
          </strong>
          <small>
            {overdueCountQuery.isError
              ? "全量状态计数暂不可用"
              : "全量逾期状态计数"}
          </small>
        </article>
      </section>

      {invoicesQuery.isError ? (
        <ErrorState
          message="无法读取发票，请确认本地服务已连接。"
          onRetry={() => void invoicesQuery.refetch()}
        />
      ) : null}
      {invoicesQuery.isPending ? <SkeletonRows count={7} /> : null}

      {invoicesQuery.isSuccess && invoices.length === 0 ? (
        <EmptyState
          action={
            hasFilters ? (
              <button
                className="button button-secondary"
                onClick={clearFilters}
                type="button"
              >
                清除筛选
              </button>
            ) : (
              <button
                className="button button-primary"
                onClick={() => setFormInvoice(null)}
                type="button"
              >
                <Plus size={15} />
                新建第一张发票
              </button>
            )
          }
          message={
            hasFilters
              ? "调整搜索、状态或币种筛选后再试。"
              : "创建草稿后，可以在这里跟踪发送、查看和付款状态。"
          }
          title={hasFilters ? "没有匹配的发票" : "暂无发票"}
        />
      ) : null}

      {invoices.length > 0 ? (
        <>
          <div className="table-panel invoices-table-panel">
            <table className="data-table invoices-table">
              <thead>
                <tr>
                  <th scope="col">发票编号</th>
                  <th scope="col">客户 / 项目</th>
                  <th scope="col">金额</th>
                  <th scope="col">状态</th>
                  <th scope="col">开票 / 到期</th>
                  <th aria-label="操作" scope="col" />
                </tr>
              </thead>
              <tbody>
                {invoices.map((invoice) => (
                  <tr key={invoice.id}>
                    <td>
                      <strong className="invoice-number">
                        {invoice.invoiceNumber}
                      </strong>
                    </td>
                    <td>
                      <div className="invoice-client-cell">
                        <span>{invoice.clientName}</span>
                        <small>{invoice.projectName ?? "未关联项目"}</small>
                      </div>
                    </td>
                    <td className="invoice-amount-cell">
                      {formatAmount(invoice.amountMinor, invoice.currency)}
                    </td>
                    <td>
                      <span
                        className={`status-badge ${statusClass(invoice.status)}`}
                      >
                        {statusLabels[invoice.status]}
                      </span>
                    </td>
                    <td>
                      <div className="invoice-date-cell">
                        <span>{invoice.issueDate}</span>
                        <small
                          className={
                            invoice.status === "overdue"
                              ? "invoice-date-overdue"
                              : undefined
                          }
                        >
                          到期 {invoice.dueDate}
                        </small>
                      </div>
                    </td>
                    <td className="invoice-table-action">
                      {invoice.status !== "paid" ? (
                        <div className="invoice-row-actions">
                          <button
                            aria-label={`打开 ${invoice.invoiceNumber} 操作`}
                            className="icon-button"
                            onClick={() =>
                              setActionMenu((value) =>
                                value === invoice.id ? null : invoice.id,
                              )
                            }
                            type="button"
                          >
                            <MoreHorizontal size={15} />
                          </button>
                          {actionMenu === invoice.id ? (
                            <div className="invoice-action-menu">
                              {invoice.status === "draft" ? (
                                <>
                                  <button
                                    onClick={() => {
                                      setFormInvoice(invoice);
                                      setActionMenu(null);
                                    }}
                                    type="button"
                                  >
                                    <Pencil size={13} /> 编辑
                                  </button>
                                  <button
                                    onClick={() =>
                                      openCommand(invoice, "mark_sent")
                                    }
                                    type="button"
                                  >
                                    <Send size={13} /> 确认已发送
                                  </button>
                                  <button
                                    className="danger-text"
                                    onClick={() =>
                                      openCommand(invoice, "delete")
                                    }
                                    type="button"
                                  >
                                    <Trash2 size={13} /> 删除
                                  </button>
                                </>
                              ) : null}
                              {invoice.status === "sent" ? (
                                <button
                                  onClick={() =>
                                    openCommand(invoice, "mark_viewed")
                                  }
                                  type="button"
                                >
                                  <Eye size={13} /> 确认已查看
                                </button>
                              ) : null}
                              {invoice.status === "viewed" ||
                              invoice.status === "overdue" ? (
                                <button
                                  onClick={() =>
                                    openCommand(invoice, "mark_paid")
                                  }
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
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 ? (
            <nav aria-label="发票分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={page <= 1 || invoicesQuery.isFetching}
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
                disabled={page >= totalPages || invoicesQuery.isFetching}
                onClick={() => setPage((value) => value + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </>
      ) : null}

      <InvoiceFormModal
        invoice={formInvoice ?? undefined}
        onClose={() => setFormInvoice(undefined)}
        open={formInvoice !== undefined}
      />

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
                command?.action === "delete"
                  ? "button button-danger"
                  : "button button-primary"
              }
              disabled={
                commandBusy ||
                (command?.action === "mark_paid" &&
                  (!paidDate || paidDate < command.invoice.issueDate))
              }
              onClick={confirmCommand}
              type="button"
            >
              {commandBusy
                ? "正在处理…"
                : command?.action === "delete"
                  ? "确认删除"
                  : command?.action === "mark_paid"
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
        {command?.action === "delete" ? (
          <p className="modal-copy">
            将永久删除草稿 {command.invoice.invoiceNumber}。此操作无法恢复。
          </p>
        ) : null}
        {command?.action === "mark_sent" ? (
          <p className="modal-copy">
            确认后只记录“已发送”状态；应用不会替你发送邮件或生成 PDF。
          </p>
        ) : null}
        {command?.action === "mark_viewed" ? (
          <p className="modal-copy">请在已从客户处确认查看后再更新状态。</p>
        ) : null}
        {command?.action === "mark_paid" ? (
          <>
            <p className="modal-copy">
              确认付款会生成关联收入记录，发票将变为只读。
            </p>
            <label className="form-field form-field-last">
              <span>付款日期</span>
              <input
                aria-label="付款日期"
                autoFocus
                min={command.invoice.issueDate}
                onChange={(event) => setPaidDate(event.target.value)}
                type="date"
                value={paidDate}
              />
            </label>
          </>
        ) : null}
        {commandError ? (
          <div className="form-error" role="alert">
            {apiErrorMessage(commandError, "发票操作失败，请重试。")}
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
