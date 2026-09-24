import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useFinancialEntryQuery } from "../api/hooks";
import {
  isWorkspaceIdentity,
  focusReportReturnSession,
} from "../lib/focusReportLocation";
import { ReturnToAiChat } from "../components/ClientRecordLocation";
import { ErrorState, LoadingState } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { FinancialEntryFormModal } from "../components/FinancialEntryFormModal";
import { formatInvoiceAmount } from "../components/invoicePresentation";
import { AiIssueHandoffButton } from "../components/AiWorkbenchHandoff";
import { financialEntryHandoff } from "../lib/aiIssueHandoff";

export function FinancialEntryDetailPage() {
  const { entryId } = useParams();
  // A new identity gets a new edit state; a draft never follows route changes.
  return <FinancialEntryDetail key={entryId} id={entryId} />;
}

function FinancialEntryDetail({ id }: { id?: string }) {
  const [params] = useSearchParams();
  const valid =
    isWorkspaceIdentity(id) &&
    [...params.keys()].every((key) => key === "return_session") &&
    params.getAll("return_session").length <= 1;
  const query = useFinancialEntryQuery(valid ? id : null, true);
  const [editing, setEditing] = useState(false);
  const returnSession = focusReportReturnSession(params);
  const entry = query.isFetching || query.isError ? undefined : query.data;
  return (
    <div className="page">
      <PageHeader
        title="收支记录详情"
        actions={
          <>
            <ReturnToAiChat sessionId={returnSession} />
            {entry ? (
              <AiIssueHandoffButton
                content={financialEntryHandoff(entry.id)}
                disabled={editing}
              />
            ) : null}
            <Link
              className="button button-secondary"
              to={
                returnSession
                  ? `/income?return_session=${returnSession}`
                  : "/income"
              }
            >
              全部收支
            </Link>
          </>
        }
      />
      {!valid ? (
        <p role="alert">财务记录链接无效。</p>
      ) : query.isPending || query.isFetching ? (
        <LoadingState label="正在读取当前财务记录…" />
      ) : !entry ? (
        <ErrorState
          title="财务记录不可用"
          message="记录可能已不可用或读取失败，不会显示旧记录代替。"
          onRetry={() => void query.refetch()}
        />
      ) : (
        <>
          <section
            className="invoice-detail-panel"
            aria-labelledby="financial-entry-title"
          >
            <div className="invoice-detail-panel-heading">
              <div>
                <h2 id="financial-entry-title">
                  {entry.type === "income" ? "收入" : "支出"} ·{" "}
                  {formatInvoiceAmount(entry.amountMinor, entry.currency)}
                </h2>
                <p>
                  {entry.occurredOn} · {entry.currency} ·{" "}
                  {entry.status === "confirmed"
                    ? "已确认"
                    : entry.status === "pending"
                      ? "待确认"
                      : "已作废"}{" "}
                  · 版本 {entry.version}
                </p>
              </div>
            </div>
            <div style={{ padding: "12px 14px", overflowWrap: "anywhere" }}>
              <p className="invoice-detail-code">记录 ID：{entry.id}</p>
              <p>分类：{entry.category}</p>
              {entry.clientId && (
                <p>
                  客户：
                  <Link to={`/clients/${entry.clientId}`}>
                    {entry.clientName ?? entry.clientId}
                  </Link>
                </p>
              )}
              {entry.projectId && (
                <p>
                  项目：
                  <Link to={`/projects/${entry.projectId}`}>
                    {entry.projectName ?? entry.projectId}
                  </Link>
                </p>
              )}
              {entry.invoiceId && (
                <p>
                  发票回款：
                  <Link
                    to={`/invoices/${entry.invoiceId}${returnSession ? `?return_session=${returnSession}` : ""}`}
                  >
                    {entry.invoiceNumber ?? entry.invoiceId}
                  </Link>
                  （由发票付款事务维护，不可单独编辑）
                </p>
              )}
              <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                备注：{entry.notes || "无"}
              </p>
              {entry.status === "voided" && (
                <p>作废原因：{entry.voidReason || "无"}</p>
              )}
              <p>
                此处是本地人工详情；查看不会确认付款，也不会把备注发送给模型。
              </p>
              {entry.status !== "voided" && !entry.invoiceId && (
                <button
                  type="button"
                  className="button button-secondary"
                  onClick={() => setEditing(true)}
                >
                  编辑记录
                </button>
              )}
            </div>
          </section>
          <FinancialEntryFormModal
            open={editing}
            entry={entry}
            onClose={() => {
              setEditing(false);
              void query.refetch();
            }}
          />
        </>
      )}
    </div>
  );
}
