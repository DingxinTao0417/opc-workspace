import { ChevronRight, Plus, Search } from "lucide-react";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useClientsQuery } from "../api/hooks";
import { ClientFormModal } from "../components/ClientFormModal";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import type { ClientStatus } from "../types/models";

const statusLabels: Record<ClientStatus, string> = {
  active: "活跃",
  lead: "潜在客户",
  inactive: "已停用",
};

function statusClass(status: ClientStatus): string {
  if (status === "active") return "status-green";
  if (status === "lead") return "status-purple";
  return "status-neutral";
}

function formatLatestActivity(value: string | null): string {
  if (!value) return "暂无本地活动";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(date);
}

export function ClientsPage() {
  const location = useLocation();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<ClientStatus | "">("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const query = useClientsQuery({
    page,
    pageSize: 20,
    q: search,
    status: status || undefined,
    sort: "-updated_at",
  });
  const clients = query.data?.items ?? [];
  const totalPages = Math.max(
    1,
    Math.ceil(
      (query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 20),
    ),
  );
  const hasFilters = Boolean(search.trim() || status);
  const deletionNotice =
    typeof location.state === "object" &&
    location.state !== null &&
    "clientDeletion" in location.state &&
    typeof location.state.clientDeletion === "number"
      ? `客户已永久删除，已解除 ${location.state.clientDeletion} 个项目关联。`
      : null;

  return (
    <div className="page clients-page">
      <PageHeader
        actions={
          <button
            className="button button-primary"
            onClick={() => setCreating(true)}
            type="button"
          >
            <Plus size={15} />
            新建客户
          </button>
        }
        meta={
          <span className="page-count">
            {query.isPending
              ? "读取中"
              : query.isSuccess
                ? `${query.data.meta.total} 个`
                : "数据不可用"}
          </span>
        }
        title="客户"
      />

      {deletionNotice ? (
        <div className="client-notice" role="status">
          {deletionNotice}
        </div>
      ) : null}

      <div className="toolbar clients-toolbar">
        <label className="toolbar-search">
          <Search size={15} />
          <input
            aria-label="搜索客户"
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="搜索名称、联系人、邮箱或电话…"
            value={search}
          />
        </label>
        <label className="toolbar-select">
          <span className="sr-only">客户状态</span>
          <select
            aria-label="客户状态"
            onChange={(event) => {
              setStatus(event.target.value as ClientStatus | "");
              setPage(1);
            }}
            value={status}
          >
            <option value="">全部状态</option>
            <option value="active">活跃</option>
            <option value="lead">潜在客户</option>
            <option value="inactive">已停用</option>
          </select>
        </label>
      </div>

      {query.isError ? (
        <ErrorState
          message="无法读取客户数据，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? <SkeletonRows count={7} /> : null}

      {query.isSuccess && clients.length === 0 ? (
        <EmptyState
          action={
            hasFilters ? (
              <button
                className="button button-secondary"
                onClick={() => {
                  setSearch("");
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
                onClick={() => setCreating(true)}
                type="button"
              >
                <Plus size={15} />
                新建第一个客户
              </button>
            )
          }
          message={
            hasFilters
              ? "调整关键词或状态筛选后再试。"
              : "添加客户后，可以在这里管理资料并查看真实关联项目。"
          }
          title={hasFilters ? "没有匹配的客户" : "暂无客户"}
        />
      ) : null}

      {clients.length > 0 ? (
        <>
          <div className="table-panel client-table-panel">
            <table className="data-table client-table">
              <thead>
                <tr>
                  <th scope="col">客户</th>
                  <th scope="col">联系人</th>
                  <th scope="col">项目数</th>
                  <th scope="col">累计收入</th>
                  <th scope="col">状态</th>
                  <th scope="col">最近动态</th>
                  <th aria-label="操作" scope="col" />
                </tr>
              </thead>
              <tbody>
                {clients.map((client) => (
                  <tr key={client.id}>
                    <td>
                      <Link
                        className="client-cell client-row-link"
                        to={`/clients/${client.id}`}
                      >
                        <span aria-hidden="true" className="client-avatar">
                          {client.name.trim().charAt(0).toUpperCase() || "客"}
                        </span>
                        <strong>{client.name}</strong>
                      </Link>
                    </td>
                    <td>{client.contactName ?? "—"}</td>
                    <td className="client-count-cell">{client.projectCount}</td>
                    <td className="muted-cell">待客户聚合</td>
                    <td>
                      <span
                        className={`status-badge client-status ${statusClass(client.status)}`}
                      >
                        <span
                          aria-hidden="true"
                          className="client-status-dot"
                        />
                        {statusLabels[client.status]}
                      </span>
                    </td>
                    <td className="muted-cell">
                      {formatLatestActivity(client.latestActivityAt)}
                    </td>
                    <td className="client-table-action">
                      <Link
                        aria-label={`查看 ${client.name} 详情`}
                        className="icon-button"
                        to={`/clients/${client.id}`}
                      >
                        <ChevronRight size={15} />
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 ? (
            <nav aria-label="客户分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={page <= 1 || query.isFetching}
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
                disabled={page >= totalPages || query.isFetching}
                onClick={() => setPage((value) => value + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </>
      ) : null}

      <ClientFormModal onClose={() => setCreating(false)} open={creating} />
    </div>
  );
}
