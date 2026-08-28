import { CalendarDays, Plus, Search } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useProjectsQuery } from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { PageHeader } from "../components/PageHeader";
import { ProjectFormModal } from "../components/ProjectFormModal";
import type { Project, ProjectStatus } from "../types/models";

const statusLabels: Record<ProjectStatus, string> = {
  planning: "规划中",
  in_progress: "进行中",
  paused: "已暂停",
  completed: "已完成",
  archived: "已归档",
};

function statusClass(status: ProjectStatus): string {
  if (status === "in_progress") return "status-purple";
  if (status === "completed") return "status-green";
  if (status === "paused") return "status-red";
  return "status-neutral";
}

function formatDate(value: string | null): string {
  if (!value) return "未设置截止日期";
  const date = new Date(`${value}T00:00:00`);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
  }).format(date);
}

function ProjectCard({ project }: { project: Project }) {
  return (
    <Link className="project-card" to={`/projects/${project.id}`}>
      <div className="project-card-heading">
        <h2>{project.name}</h2>
        <span className={`status-badge ${statusClass(project.status)}`}>
          {statusLabels[project.status]}
        </span>
      </div>
      <p>{project.description || "尚未填写项目说明。"}</p>
      <div className="project-progress-row">
        <div
          aria-label={`${project.name}任务完成进度`}
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={project.taskSummary.progressPercent}
          className="project-progress"
          role="progressbar"
        >
          <span style={{ width: `${project.taskSummary.progressPercent}%` }} />
        </div>
        <strong>{project.taskSummary.progressPercent}%</strong>
      </div>
      <footer>
        <span className="project-task-stat">
          {project.taskSummary.completed}/{project.taskSummary.total} 项任务
        </span>
        {project.clientName ? (
          <span className="project-card-context">{project.clientName}</span>
        ) : project.dueDate ? (
          <span className="project-card-context">
            <CalendarDays size={12} />
            {formatDate(project.dueDate)}
          </span>
        ) : null}
      </footer>
    </Link>
  );
}

export function ProjectsPage() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<ProjectStatus | "">("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const query = useProjectsQuery({
    page,
    pageSize: 12,
    query: search,
    status: status || undefined,
    sort: "-updated_at",
  });
  const projects = query.data?.items ?? [];
  const totalPages = Math.max(
    1,
    Math.ceil(
      (query.data?.meta.total ?? 0) / (query.data?.meta.pageSize ?? 12),
    ),
  );
  const hasFilters = Boolean(search.trim() || status);

  return (
    <div className="page">
      <PageHeader
        actions={
          <button
            className="button button-primary"
            onClick={() => setCreating(true)}
            type="button"
          >
            <Plus size={15} />
            新建项目
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
        title="项目"
      />

      <div className="toolbar">
        <label className="toolbar-search">
          <Search size={15} />
          <input
            aria-label="搜索项目"
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder="搜索项目名称或描述…"
            value={search}
          />
        </label>
        <label className="toolbar-select">
          <span className="sr-only">项目状态</span>
          <select
            onChange={(event) => {
              setStatus(event.target.value as ProjectStatus | "");
              setPage(1);
            }}
            value={status}
          >
            <option value="">活跃项目</option>
            <option value="planning">规划中</option>
            <option value="in_progress">进行中</option>
            <option value="paused">已暂停</option>
            <option value="completed">已完成</option>
            <option value="archived">已归档</option>
          </select>
        </label>
      </div>

      {query.isError ? (
        <ErrorState
          message="无法读取项目数据，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? <SkeletonRows count={6} /> : null}

      {query.isSuccess && projects.length === 0 ? (
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
                新建第一个项目
              </button>
            )
          }
          message={
            hasFilters
              ? "调整关键词或状态筛选后再试。"
              : "创建项目后，可以在这里跟踪任务进度和交付状态。"
          }
          title={hasFilters ? "没有匹配的项目" : "暂无项目"}
        />
      ) : null}

      {projects.length > 0 ? (
        <>
          <div className="project-grid">
            {projects.map((project) => (
              <ProjectCard key={project.id} project={project} />
            ))}
          </div>
          {totalPages > 1 ? (
            <nav aria-label="项目分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={page <= 1}
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
                disabled={page >= totalPages}
                onClick={() => setPage((value) => value + 1)}
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </>
      ) : null}

      <ProjectFormModal onClose={() => setCreating(false)} open={creating} />
    </div>
  );
}
