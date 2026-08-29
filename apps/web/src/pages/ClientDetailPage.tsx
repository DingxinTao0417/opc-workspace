import {
  AlertTriangle,
  ArrowLeft,
  Edit3,
  FolderKanban,
  Mail,
  Phone,
  Trash2,
  UserRound,
} from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/client";
import {
  useClientQuery,
  useDeleteClient,
  useProjectsQuery,
  useUpdateClient,
} from "../api/hooks";
import { ClientFormModal } from "../components/ClientFormModal";
import { ClientActivitiesSection } from "../components/ClientActivitiesSection";
import { ClientAttachmentsSection } from "../components/ClientAttachmentsSection";
import { ClientActorLinksSection } from "../components/ClientActorLinksSection";
import { ClientFollowupsSection } from "../components/ClientFollowupsSection";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import type { ClientStatus, ProjectStatus } from "../types/models";

const clientStatusLabels: Record<ClientStatus, string> = {
  active: "活跃",
  lead: "潜在客户",
  inactive: "已停用",
};

const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: "规划中",
  in_progress: "进行中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
};

function clientStatusClass(status: ClientStatus): string {
  if (status === "active") return "status-green";
  if (status === "lead") return "status-purple";
  return "status-neutral";
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function operationErrorMessage(error: unknown): string | null {
  if (!error) return null;
  if (error instanceof ApiError) {
    if (error.code === "VERSION_CONFLICT") {
      return "客户资料已在其他窗口变化，已刷新最新版本，请重新确认操作。";
    }
    if (
      error.code === "CLIENT_HAS_INVOICES" ||
      error.code === "CLIENT_DELETE_RESTRICTED"
    ) {
      return "该客户仍被发票引用，当前不能永久删除。可先停用客户。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "客户操作失败，请重试。";
}

export function ClientDetailPage() {
  const clientId = useParams().clientId ?? "";
  const navigate = useNavigate();
  const clientQuery = useClientQuery(clientId || null);
  const [projectPage, setProjectPage] = useState(1);
  const projectsQuery = useProjectsQuery(
    {
      page: projectPage,
      pageSize: 8,
      clientId,
      includeArchived: true,
      sort: "-updated_at",
    },
    Boolean(clientId),
  );
  const updateMutation = useUpdateClient();
  const deleteMutation = useDeleteClient();
  const [editing, setEditing] = useState(false);
  const [confirmingStatus, setConfirmingStatus] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const client = clientQuery.data;
  const projects = projectsQuery.data?.items ?? [];
  const projectPages = Math.max(
    1,
    Math.ceil(
      (projectsQuery.data?.meta.total ?? 0) /
        (projectsQuery.data?.meta.pageSize ?? 8),
    ),
  );
  const busy = updateMutation.isPending || deleteMutation.isPending;
  const operationError =
    operationErrorMessage(updateMutation.error) ??
    operationErrorMessage(deleteMutation.error);

  const refreshAfterConflict = async () => {
    const result = await clientQuery.refetch();
    return result.data;
  };

  const changeStatus = () => {
    if (!client) return;
    const status: ClientStatus =
      client.status === "inactive" ? "active" : "inactive";
    updateMutation.mutate(
      {
        id: client.id,
        input: { status, expectedVersion: client.version },
      },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            setConfirmingStatus(false);
            void clientQuery.refetch();
          }
        },
        onSuccess: () => setConfirmingStatus(false),
      },
    );
  };

  const permanentlyDelete = () => {
    if (!client) return;
    deleteMutation.mutate(
      { id: client.id, expectedVersion: client.version },
      {
        onError: (error) => {
          if (error instanceof ApiError && error.code === "VERSION_CONFLICT") {
            setConfirmingDelete(false);
            void clientQuery.refetch();
          }
        },
        onSuccess: (result) => {
          navigate("/clients", {
            replace: true,
            state: { clientDeletion: result.detachedProjects },
          });
        },
      },
    );
  };

  if (clientQuery.isPending) {
    return (
      <div className="page">
        <SkeletonRows count={8} />
      </div>
    );
  }

  if (clientQuery.isError || !client) {
    return (
      <div className="page">
        <Link className="project-back-link" to="/clients">
          <ArrowLeft size={14} />
          返回客户
        </Link>
        <ErrorState
          message="无法读取该客户，客户可能已删除或本地服务暂不可用。"
          onRetry={() => void clientQuery.refetch()}
          title="客户详情不可用"
        />
      </div>
    );
  }

  return (
    <div className="page client-detail-page">
      <PageHeader
        actions={
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={() => setEditing(true)}
            type="button"
          >
            <Edit3 size={14} />
            编辑资料
          </button>
        }
        eyebrow={
          <Link className="project-back-link" to="/clients">
            <ArrowLeft size={13} />
            全部客户
          </Link>
        }
        meta={
          <span
            className={`status-badge client-status ${clientStatusClass(client.status)}`}
          >
            <span aria-hidden="true" className="client-status-dot" />
            {clientStatusLabels[client.status]}
          </span>
        }
        title={client.name}
      />

      <section className="client-detail-card">
        <div className="client-detail-identity">
          <span
            aria-hidden="true"
            className="client-avatar client-avatar-large"
          >
            {client.name.trim().charAt(0).toUpperCase() || "客"}
          </span>
          <div>
            <h2>基本资料</h2>
            <p>{client.notes ?? "尚未填写客户备注。"}</p>
          </div>
        </div>
        <dl className="client-contact-grid">
          <div>
            <dt>
              <UserRound size={13} /> 联系人
            </dt>
            <dd>{client.contactName ?? "未填写"}</dd>
          </div>
          <div>
            <dt>
              <Mail size={13} /> 邮箱
            </dt>
            <dd>{client.email ?? "未填写"}</dd>
          </div>
          <div>
            <dt>
              <Phone size={13} /> 电话
            </dt>
            <dd>{client.phone ?? "未填写"}</dd>
          </div>
          <div>
            <dt>本地记录</dt>
            <dd>
              创建 {formatDateTime(client.createdAt)} · 更新{" "}
              {formatDateTime(client.updatedAt)}
            </dd>
          </div>
        </dl>
      </section>

      <section className="project-detail-section">
        <div className="project-detail-heading">
          <div>
            <h2>关联项目</h2>
            <p>项目关系来自真实 Project 数据，客户记录不重复保存项目清单。</p>
          </div>
          <span>
            {projectsQuery.data?.meta.total ?? client.projectCount} 项
          </span>
        </div>

        {projectsQuery.isPending ? <SkeletonRows count={4} /> : null}
        {projectsQuery.isError ? (
          <ErrorState
            compact
            message="无法读取关联项目。"
            onRetry={() => void projectsQuery.refetch()}
          />
        ) : null}
        {projectsQuery.isSuccess && projects.length === 0 ? (
          <EmptyState
            message="在新建或编辑项目时选择该客户，即会显示在这里。"
            title="暂无关联项目"
          />
        ) : null}
        {projects.length > 0 ? (
          <div className="client-project-list">
            {projects.map((project) => (
              <Link key={project.id} to={`/projects/${project.id}`}>
                <span className="client-project-icon" aria-hidden="true">
                  <FolderKanban size={14} />
                </span>
                <span>
                  <strong>{project.name}</strong>
                  <small>
                    {project.taskSummary.completed}/{project.taskSummary.total}{" "}
                    项任务
                  </small>
                </span>
                <span className="status-badge">
                  {projectStatusLabels[project.status]}
                </span>
              </Link>
            ))}
          </div>
        ) : null}
        {projectPages > 1 ? (
          <nav aria-label="客户关联项目分页" className="pagination">
            <button
              className="button button-secondary"
              disabled={projectPage <= 1 || projectsQuery.isFetching}
              onClick={() => setProjectPage((value) => Math.max(1, value - 1))}
              type="button"
            >
              上一页
            </button>
            <span>
              第 {projectPage} / {projectPages} 页
            </span>
            <button
              className="button button-secondary"
              disabled={projectPage >= projectPages || projectsQuery.isFetching}
              onClick={() => setProjectPage((value) => value + 1)}
              type="button"
            >
              下一页
            </button>
          </nav>
        ) : null}
      </section>

      <ClientActivitiesSection clientId={client.id} />

      <ClientFollowupsSection
        clientId={client.id}
        clientStatus={client.status}
      />

      <ClientAttachmentsSection
        clientId={client.id}
        clientVersion={client.version}
      />

      <ClientActorLinksSection
        clientId={client.id}
        clientVersion={client.version}
        contactName={client.contactName}
      />

      <section className="client-future-grid" aria-label="后续客户能力">
        <article>
          <strong>收入与发票</strong>
          <p>v0.4 交付财务事实后可用；当前不展示金额或发票模拟数据。</p>
          <span>后续版本</span>
        </article>
      </section>

      <section className="project-detail-section client-status-section">
        <div className="project-detail-heading">
          <div>
            <h2>客户状态</h2>
            <p>停用只改变客户状态，不会解除或删除关联项目。</p>
          </div>
        </div>
        {confirmingStatus ? (
          <div className="project-confirmation" role="alert">
            <AlertTriangle size={16} />
            <div>
              <strong>
                {client.status === "inactive"
                  ? "恢复为活跃客户？"
                  : "停用该客户？"}
              </strong>
              <p>项目与历史资料会继续保留。</p>
            </div>
            <button
              autoFocus
              className="button button-secondary"
              disabled={busy}
              onClick={() => setConfirmingStatus(false)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-primary"
              disabled={busy}
              onClick={changeStatus}
              type="button"
            >
              {updateMutation.isPending
                ? "正在更新…"
                : client.status === "inactive"
                  ? "确认恢复"
                  : "确认停用"}
            </button>
          </div>
        ) : (
          <button
            className="button button-secondary"
            disabled={busy}
            onClick={() => setConfirmingStatus(true)}
            type="button"
          >
            {client.status === "inactive" ? "恢复为活跃" : "停用客户"}
          </button>
        )}
      </section>

      <section className="project-detail-section project-danger-zone">
        <div>
          <h2>永久删除</h2>
          <p>
            {client.status === "inactive"
              ? `将解除 ${client.projectCount} 个项目的客户关联，但不会删除项目；若仍被发票引用，服务端会拒绝删除。`
              : "永久删除前必须先停用客户；停用不会解除或删除关联项目。"}
          </p>
        </div>
        {confirmingDelete && client.status === "inactive" ? (
          <div className="project-action-row">
            <button
              autoFocus
              className="button button-secondary"
              disabled={busy}
              onClick={() => setConfirmingDelete(false)}
              type="button"
            >
              返回
            </button>
            <button
              className="button button-danger"
              disabled={busy}
              onClick={permanentlyDelete}
              type="button"
            >
              <Trash2 size={14} />
              {deleteMutation.isPending ? "正在删除…" : "确认永久删除"}
            </button>
          </div>
        ) : (
          <button
            className="button button-danger"
            disabled={busy || client.status !== "inactive"}
            onClick={() => setConfirmingDelete(true)}
            title={client.status === "inactive" ? undefined : "请先停用客户"}
            type="button"
          >
            <Trash2 size={14} />
            {client.status === "inactive" ? "永久删除客户" : "先停用客户"}
          </button>
        )}
      </section>

      {operationError ? (
        <div className="form-error" role="alert">
          {operationError}
        </div>
      ) : null}

      <ClientFormModal
        client={client}
        onClose={() => setEditing(false)}
        onVersionConflict={refreshAfterConflict}
        open={editing}
      />
    </div>
  );
}
