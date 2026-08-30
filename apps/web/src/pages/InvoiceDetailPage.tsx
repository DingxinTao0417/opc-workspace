import { ArrowLeft } from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import { useInvoiceQuery } from "../api/hooks";
import type { Invoice } from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { InvoiceActions } from "../components/InvoiceActions";
import { InvoiceFormModal } from "../components/InvoiceFormModal";
import { InvoicePdfSection } from "../components/InvoicePdfSection";
import {
  formatInvoiceAmount,
  invoiceStatusClass,
  invoiceStatusLabels,
} from "../components/invoicePresentation";
import { PageHeader } from "../components/PageHeader";

function formatTimestamp(value: string): string {
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

function invoiceNotFound(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 404 || error.code === "INVOICE_NOT_FOUND")
  );
}

export function InvoiceDetailPage() {
  const { invoiceId } = useParams<{ invoiceId: string }>();
  const navigate = useNavigate();
  const query = useInvoiceQuery(invoiceId ?? null);
  const [editing, setEditing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const invoice = query.data;
  const notFound = query.isError && invoiceNotFound(query.error);

  useEffect(() => {
    setEditing(false);
    setNotice(null);
  }, [invoiceId]);

  const refreshAfterConflict = async () => {
    setNotice("检测到版本冲突，正在读取最新发票…");
    const result = await query.refetch();
    setNotice(
      result.isSuccess && result.data
        ? "发票已在其他窗口更新，详情已刷新，请重新操作。"
        : "发票已发生变化，但最新详情读取失败，请重试。",
    );
  };

  const transitioned = (updatedInvoice: Invoice) => {
    setNotice(
      `发票状态已更新为${invoiceStatusLabels[updatedInvoice.status]}。`,
    );
  };

  const backLink = (
    <Link className="project-back-link" to="/invoices">
      <ArrowLeft size={13} />
      全部发票
    </Link>
  );

  if (query.isPending) {
    return (
      <div className="page invoice-detail-page">
        <PageHeader eyebrow={backLink} title="发票详情" />
        <SkeletonRows count={6} />
      </div>
    );
  }

  if (notFound) {
    return (
      <div className="page invoice-detail-page">
        <PageHeader eyebrow={backLink} title="发票详情" />
        <EmptyState
          action={
            <Link className="button button-secondary" to="/invoices">
              返回发票列表
            </Link>
          }
          message="这张发票可能已被删除，或当前链接不再有效。"
          title="发票不存在"
        />
      </div>
    );
  }

  if (!invoice) {
    return (
      <div className="page invoice-detail-page">
        <PageHeader eyebrow={backLink} title="发票详情" />
        <ErrorState
          message="无法读取这张发票，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
          title="发票详情不可用"
        />
      </div>
    );
  }

  return (
    <div className="page invoice-detail-page">
      <PageHeader
        actions={
          <InvoiceActions
            invoice={invoice}
            onConflict={refreshAfterConflict}
            onDeleted={() => navigate("/invoices", { replace: true })}
            onEdit={() => setEditing(true)}
            onTransitioned={transitioned}
            variant="detail"
          />
        }
        eyebrow={backLink}
        meta={
          <span
            className={`status-badge ${invoiceStatusClass(invoice.status)}`}
          >
            {invoiceStatusLabels[invoice.status]}
          </span>
        }
        title={invoice.invoiceNumber}
      />

      {notice ? (
        <div className="invoice-detail-notice" role="status">
          {notice}
        </div>
      ) : null}

      {query.isError ? (
        <ErrorState
          compact
          message="最新发票读取失败，当前仍显示上一次已知内容。"
          onRetry={() => void query.refetch()}
          title="发票刷新失败"
        />
      ) : null}

      <section className="invoice-detail-summary" aria-label="发票金额">
        <div>
          <span>发票金额</span>
          <strong>
            {formatInvoiceAmount(invoice.amountMinor, invoice.currency)}
          </strong>
          <small>{invoice.currency}</small>
        </div>
        <div>
          <span>客户</span>
          <strong>
            <Link
              className="invoice-detail-related-link"
              to={`/clients/${invoice.clientId}`}
            >
              {invoice.clientName}
            </Link>
          </strong>
          <small className="invoice-detail-code">{invoice.clientId}</small>
        </div>
        <div>
          <span>项目</span>
          <strong>
            {invoice.projectId ? (
              <Link
                className="invoice-detail-related-link"
                to={`/projects/${invoice.projectId}`}
              >
                {invoice.projectName ?? "查看关联项目"}
              </Link>
            ) : (
              "未关联项目"
            )}
          </strong>
          {invoice.projectId ? (
            <small className="invoice-detail-code">{invoice.projectId}</small>
          ) : (
            <small>未设置项目关联</small>
          )}
        </div>
      </section>

      <section
        className="invoice-detail-panel"
        aria-labelledby="invoice-facts-title"
      >
        <div className="invoice-detail-panel-heading">
          <div>
            <h2 id="invoice-facts-title">发票事实</h2>
            <p>所有金额、日期和关联均来自本地发票记录。</p>
          </div>
        </div>
        <dl className="invoice-detail-meta">
          <div>
            <dt>发票编号</dt>
            <dd className="invoice-detail-code">{invoice.invoiceNumber}</dd>
          </div>
          <div>
            <dt>发票 ID</dt>
            <dd className="invoice-detail-code">{invoice.id}</dd>
          </div>
          <div>
            <dt>币种</dt>
            <dd>{invoice.currency}</dd>
          </div>
          <div>
            <dt>状态</dt>
            <dd>{invoiceStatusLabels[invoice.status]}</dd>
          </div>
          <div>
            <dt>开票日期</dt>
            <dd>{invoice.issueDate}</dd>
          </div>
          <div>
            <dt>到期日期</dt>
            <dd>{invoice.dueDate}</dd>
          </div>
          <div>
            <dt>付款日期</dt>
            <dd>{invoice.paidDate ?? "尚未付款"}</dd>
          </div>
          <div>
            <dt>关联收入</dt>
            <dd
              className={
                invoice.financialEntryId ? "invoice-detail-code" : undefined
              }
            >
              {invoice.financialEntryId ?? "未关联收入记录"}
            </dd>
          </div>
          <div>
            <dt>版本</dt>
            <dd>{invoice.version}</dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>
              <time dateTime={invoice.createdAt}>
                {formatTimestamp(invoice.createdAt)}
              </time>
            </dd>
          </div>
          <div>
            <dt>更新时间</dt>
            <dd>
              <time dateTime={invoice.updatedAt}>
                {formatTimestamp(invoice.updatedAt)}
              </time>
            </dd>
          </div>
        </dl>
      </section>

      <InvoicePdfSection
        invoice={invoice}
        onInvoiceConflict={refreshAfterConflict}
      />

      <section className="invoice-detail-panel invoice-detail-notes">
        <div className="invoice-detail-panel-heading">
          <div>
            <h2>备注</h2>
            <p>发票草稿说明与人工记录。</p>
          </div>
        </div>
        <p>{invoice.notes.trim() || "未填写备注"}</p>
      </section>

      <InvoiceFormModal
        invoice={invoice}
        onClose={() => setEditing(false)}
        onVersionConflict={() => {
          setEditing(false);
          void refreshAfterConflict();
        }}
        open={editing}
      />
    </div>
  );
}
