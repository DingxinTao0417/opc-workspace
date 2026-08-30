import {
  ArrowDownRight,
  ArrowUpRight,
  Download,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { ApiError } from "../api/client";
import {
  useExportFinancialEntries,
  useFinancialEntriesQuery,
  useIncomeStatsQuery,
  useVoidFinancialEntry,
} from "../api/hooks";
import { FinancialEntryFormModal } from "../components/FinancialEntryFormModal";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import type {
  FinancialEntry,
  FinancialEntryStatus,
  FinancialEntryType,
} from "../types/models";

const currencySymbols: Record<string, string> = {
  CNY: "¥",
  USD: "$",
  EUR: "€",
  HKD: "HK$",
};

const statusLabels: Record<FinancialEntryStatus, string> = {
  pending: "待确认",
  confirmed: "已确认",
  voided: "已作废",
};

function currentMonthValue(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

function monthBounds(value: string) {
  const [year, month] = value.split("-").map(Number);
  const lastDay = new Date(year, month, 0).getDate();
  return {
    dateFrom: `${value}-01`,
    dateTo: `${value}-${String(lastDay).padStart(2, "0")}`,
  };
}

function formatAmount(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat("zh-CN", {
    style: "currency",
    currency,
    currencyDisplay: "narrowSymbol",
    minimumFractionDigits: 2,
  }).format(amountMinor / 100);
}

function statusClass(status: FinancialEntryStatus): string {
  if (status === "confirmed") return "status-green";
  if (status === "pending") return "status-orange";
  return "status-neutral";
}

function apiErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return fallback;
}

export function IncomePage() {
  const [month, setMonth] = useState(currentMonthValue());
  const [currency, setCurrency] = useState("CNY");
  const [type, setType] = useState<FinancialEntryType | "">("");
  const [status, setStatus] = useState<FinancialEntryStatus | "">("");
  const [page, setPage] = useState(1);
  const [form, setForm] = useState<{
    entry?: FinancialEntry;
    initialType?: FinancialEntryType;
  } | null>(null);
  const [voiding, setVoiding] = useState<FinancialEntry | null>(null);
  const [voidReason, setVoidReason] = useState("");
  const [actionMenu, setActionMenu] = useState<string | null>(null);
  const bounds = useMemo(() => monthBounds(month), [month]);
  const listInput = {
    page,
    pageSize: 20,
    currency,
    type: type || undefined,
    status: status || undefined,
    dateFrom: bounds.dateFrom,
    dateTo: bounds.dateTo,
    includeVoided: status === "voided",
    sort: "-occurred_on,-created_at",
  } as const;
  const entriesQuery = useFinancialEntriesQuery(listInput);
  const statsQuery = useIncomeStatsQuery({ currency, ...bounds });
  const exportMutation = useExportFinancialEntries();
  const voidMutation = useVoidFinancialEntry();
  const entries = entriesQuery.data?.items ?? [];
  const total = entriesQuery.data?.meta.total ?? 0;
  const totalPages = Math.max(
    1,
    Math.ceil(total / (entriesQuery.data?.meta.pageSize ?? 20)),
  );
  const hasFilters = Boolean(type || status);

  const exportCSV = () => {
    exportMutation.mutate(
      { ...listInput, page: undefined, pageSize: undefined },
      {
        onSuccess: ({ blob, fileName }) => {
          const url = URL.createObjectURL(blob);
          const anchor = document.createElement("a");
          anchor.href = url;
          anchor.download = fileName;
          anchor.click();
          URL.revokeObjectURL(url);
        },
      },
    );
  };

  const confirmVoid = () => {
    if (!voiding || !voidReason.trim()) return;
    voidMutation.mutate(
      {
        id: voiding.id,
        input: {
          reason: voidReason.trim(),
          expectedVersion: voiding.version,
        },
      },
      {
        onSuccess: () => {
          setVoiding(null);
          setVoidReason("");
        },
      },
    );
  };

  return (
    <div className="page income-page">
      <PageHeader
        actions={
          <>
            <button
              className="button button-secondary"
              disabled={exportMutation.isPending || total === 0}
              onClick={exportCSV}
              type="button"
            >
              <Download size={15} />
              {exportMutation.isPending ? "正在导出…" : "导出 CSV"}
            </button>
            <button
              className="button button-primary"
              onClick={() => setForm({ initialType: "income" })}
              type="button"
            >
              <Plus size={15} />
              新建记录
            </button>
          </>
        }
        meta={
          <span className="page-count">
            {entriesQuery.isPending ? "读取中" : `${total} 条`}
          </span>
        }
        title="收入与支出"
      />

      <div className="toolbar finance-toolbar">
        <label className="toolbar-select finance-month-filter">
          <span className="sr-only">月份</span>
          <input
            aria-label="月份"
            onChange={(event) => {
              setMonth(event.target.value);
              setPage(1);
            }}
            type="month"
            value={month}
          />
        </label>
        <label className="toolbar-select">
          <span className="sr-only">币种</span>
          <select
            aria-label="币种筛选"
            onChange={(event) => {
              setCurrency(event.target.value);
              setPage(1);
            }}
            value={currency}
          >
            {Object.keys(currencySymbols).map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">收支类型</span>
          <select
            aria-label="收支类型"
            onChange={(event) => {
              setType(event.target.value as FinancialEntryType | "");
              setPage(1);
            }}
            value={type}
          >
            <option value="">全部类型</option>
            <option value="income">收入</option>
            <option value="expense">支出</option>
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">记录状态</span>
          <select
            aria-label="记录状态"
            onChange={(event) => {
              setStatus(event.target.value as FinancialEntryStatus | "");
              setPage(1);
            }}
            value={status}
          >
            <option value="">有效记录</option>
            <option value="pending">待确认</option>
            <option value="confirmed">已确认</option>
            <option value="voided">已作废</option>
          </select>
        </label>
      </div>

      {statsQuery.isError ? (
        <ErrorState
          compact
          message="财务统计暂时无法读取，记录列表仍可继续使用。"
          onRetry={() => void statsQuery.refetch()}
          title="统计加载失败"
        />
      ) : null}

      <section aria-label="本月收支概览" className="finance-kpi-grid">
        <article className="kpi-card finance-kpi-card">
          <span>已确认收入</span>
          <strong className="finance-positive">
            {statsQuery.data
              ? formatAmount(statsQuery.data.confirmedIncomeMinor, currency)
              : "—"}
          </strong>
          <small>
            {statsQuery.data
              ? `${statsQuery.data.confirmedIncomeCount} 笔 · 均值 ${formatAmount(statsQuery.data.averageIncomeMinor, currency)}`
              : "正在计算本地数据"}
          </small>
        </article>
        <article className="kpi-card finance-kpi-card">
          <span>已确认支出</span>
          <strong className="finance-negative">
            {statsQuery.data
              ? formatAmount(statsQuery.data.confirmedExpenseMinor, currency)
              : "—"}
          </strong>
          <small>仅统计当前月份与币种</small>
        </article>
        <article className="kpi-card finance-kpi-card">
          <span>净现金流</span>
          <strong>
            {statsQuery.data
              ? formatAmount(statsQuery.data.netCashFlowMinor, currency)
              : "—"}
          </strong>
          <small>已确认收入减已确认支出</small>
        </article>
        <article className="kpi-card finance-kpi-card">
          <span>待确认金额</span>
          <strong>
            {statsQuery.data
              ? formatAmount(
                  statsQuery.data.pendingIncomeMinor +
                    statsQuery.data.pendingExpenseMinor,
                  currency,
                )
              : "—"}
          </strong>
          <small>
            {statsQuery.data
              ? `收入 ${formatAmount(statsQuery.data.pendingIncomeMinor, currency)} · 支出 ${formatAmount(statsQuery.data.pendingExpenseMinor, currency)}`
              : "正在计算本地数据"}
          </small>
        </article>
      </section>

      {exportMutation.isError ? (
        <div className="form-error finance-action-error" role="alert">
          {apiErrorMessage(exportMutation.error, "CSV 导出失败，请重试。")}
        </div>
      ) : null}

      {entriesQuery.isError ? (
        <ErrorState
          message="无法读取财务记录，请确认本地服务已连接。"
          onRetry={() => void entriesQuery.refetch()}
        />
      ) : null}
      {entriesQuery.isPending ? <SkeletonRows count={7} /> : null}

      {entriesQuery.isSuccess && entries.length === 0 ? (
        <EmptyState
          action={
            hasFilters ? (
              <button
                className="button button-secondary"
                onClick={() => {
                  setType("");
                  setStatus("");
                  setPage(1);
                }}
                type="button"
              >
                清除筛选
              </button>
            ) : (
              <button
                className="button button-primary"
                onClick={() => setForm({ initialType: "income" })}
                type="button"
              >
                <Plus size={15} />
                新建第一条记录
              </button>
            )
          }
          message={
            hasFilters
              ? "调整类型或状态筛选后再试。"
              : "收入和支出数据只保存在本机，可随业务数据一起备份。"
          }
          title={hasFilters ? "没有匹配的记录" : "本月暂无财务记录"}
        />
      ) : null}

      {entries.length > 0 ? (
        <>
          <div className="table-panel income-table-panel">
            <table className="data-table finance-table">
              <thead>
                <tr>
                  <th scope="col">日期</th>
                  <th scope="col">类型 / 分类</th>
                  <th scope="col">客户 / 项目</th>
                  <th scope="col">状态</th>
                  <th scope="col">金额</th>
                  <th aria-label="操作" scope="col" />
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr key={entry.id}>
                    <td className="finance-date-cell">{entry.occurredOn}</td>
                    <td>
                      <div className="finance-kind-cell">
                        <span
                          aria-hidden="true"
                          className={`finance-kind-icon finance-kind-${entry.type}`}
                        >
                          {entry.type === "income" ? (
                            <ArrowDownRight size={14} />
                          ) : (
                            <ArrowUpRight size={14} />
                          )}
                        </span>
                        <div>
                          <strong>
                            {entry.type === "income" ? "收入" : "支出"}
                          </strong>
                          <small>{entry.category}</small>
                        </div>
                      </div>
                    </td>
                    <td>
                      <div className="finance-link-cell">
                        <span>{entry.clientName ?? "未关联客户"}</span>
                        <small>{entry.projectName ?? "未关联项目"}</small>
                      </div>
                    </td>
                    <td>
                      <span
                        className={`status-badge ${statusClass(entry.status)}`}
                      >
                        {statusLabels[entry.status]}
                      </span>
                    </td>
                    <td
                      className={`finance-amount-cell finance-amount-${entry.type}`}
                    >
                      {entry.type === "expense" ? "−" : "+"}
                      {formatAmount(entry.amountMinor, entry.currency)}
                    </td>
                    <td className="finance-table-action">
                      {entry.invoiceId || entry.invoiceNumber ? (
                        <span
                          aria-label={`${entry.invoiceNumber ?? "关联发票"} 自动生成，只读`}
                          className="finance-readonly-mark"
                          title="由发票付款自动生成，不可单独编辑或作废"
                        >
                          发票同步
                        </span>
                      ) : entry.status !== "voided" ? (
                        <div className="finance-row-actions">
                          <button
                            aria-label={`打开 ${entry.category} 操作`}
                            className="icon-button"
                            onClick={() =>
                              setActionMenu((value) =>
                                value === entry.id ? null : entry.id,
                              )
                            }
                            type="button"
                          >
                            <MoreHorizontal size={15} />
                          </button>
                          {actionMenu === entry.id ? (
                            <div className="finance-action-menu">
                              <button
                                onClick={() => {
                                  setForm({ entry });
                                  setActionMenu(null);
                                }}
                                type="button"
                              >
                                <Pencil size={13} /> 编辑
                              </button>
                              <button
                                className="danger-text"
                                onClick={() => {
                                  setVoiding(entry);
                                  setVoidReason("");
                                  setActionMenu(null);
                                }}
                                type="button"
                              >
                                <Trash2 size={13} /> 作废
                              </button>
                            </div>
                          ) : null}
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 ? (
            <nav aria-label="财务记录分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={page <= 1 || entriesQuery.isFetching}
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
                disabled={page >= totalPages || entriesQuery.isFetching}
                onClick={() => setPage((value) => value + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </>
      ) : null}

      <FinancialEntryFormModal
        entry={form?.entry}
        initialType={form?.initialType}
        onClose={() => setForm(null)}
        open={form !== null}
      />

      <Modal
        footer={
          <>
            <button
              className="button button-secondary"
              disabled={voidMutation.isPending}
              onClick={() => setVoiding(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={voidMutation.isPending || !voidReason.trim()}
              onClick={confirmVoid}
              type="button"
            >
              {voidMutation.isPending ? "正在作废…" : "确认作废"}
            </button>
          </>
        }
        onClose={() => {
          if (!voidMutation.isPending) setVoiding(null);
        }}
        open={Boolean(voiding)}
        title="作废财务记录"
        width="480px"
      >
        <p className="modal-copy">
          作废后记录会保留在审计历史中，且无法恢复。请输入原因确认操作。
        </p>
        <label className="form-field form-field-last">
          <span>作废原因</span>
          <textarea
            autoFocus
            maxLength={500}
            onChange={(event) => setVoidReason(event.target.value)}
            placeholder="例如：重复录入"
            rows={3}
            value={voidReason}
          />
        </label>
        {voidMutation.isError ? (
          <div className="form-error" role="alert">
            {apiErrorMessage(voidMutation.error, "记录作废失败，请重试。")}
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
