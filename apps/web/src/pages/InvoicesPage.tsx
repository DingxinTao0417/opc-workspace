import { Plus, Search } from "lucide-react";
import { useDeferredValue, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useInvoicesQuery } from "../api/hooks";
import { InvoiceActions } from "../components/InvoiceActions";
import { InvoiceFormModal } from "../components/InvoiceFormModal";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import {
  formatInvoiceAmount,
  invoiceStatusClass,
  invoiceStatusLabels,
} from "../components/invoicePresentation";
import { PageHeader } from "../components/PageHeader";
import type { Invoice, InvoiceStatus } from "../types/models";

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

  useEffect(() => {
    if (
      !invoicesQuery.isSuccess ||
      invoicesQuery.isFetching ||
      invoicesQuery.isPlaceholderData ||
      page <= totalPages
    ) {
      return;
    }
    setPage(totalPages);
  }, [
    invoicesQuery.isFetching,
    invoicesQuery.isPlaceholderData,
    invoicesQuery.isSuccess,
    page,
    totalPages,
  ]);

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
                      <Link
                        aria-label={`查看发票 ${invoice.invoiceNumber}`}
                        className="invoice-number invoice-number-link"
                        to={`/invoices/${invoice.id}`}
                      >
                        {invoice.invoiceNumber}
                      </Link>
                    </td>
                    <td>
                      <div className="invoice-client-cell">
                        <span>{invoice.clientName}</span>
                        <small>{invoice.projectName ?? "未关联项目"}</small>
                      </div>
                    </td>
                    <td className="invoice-amount-cell">
                      {formatInvoiceAmount(
                        invoice.amountMinor,
                        invoice.currency,
                      )}
                    </td>
                    <td>
                      <span
                        className={`status-badge ${invoiceStatusClass(invoice.status)}`}
                      >
                        {invoiceStatusLabels[invoice.status]}
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
                      <InvoiceActions
                        invoice={invoice}
                        menuOpen={actionMenu === invoice.id}
                        onEdit={setFormInvoice}
                        onMenuOpenChange={(open) =>
                          setActionMenu(open ? invoice.id : null)
                        }
                        variant="menu"
                      />
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
    </div>
  );
}
