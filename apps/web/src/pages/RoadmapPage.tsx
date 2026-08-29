import {
  AlertTriangle,
  Archive,
  CalendarDays,
  Edit3,
  FolderKanban,
  Map,
  Plus,
  RotateCcw,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  useArchiveRoadmapMilestone,
  useCreateRoadmapMilestone,
  useDeleteRoadmapMilestone,
  useProjectsQuery,
  useRestoreRoadmapMilestone,
  useRoadmapMilestonesQuery,
  useUpdateRoadmapMilestone,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { ProjectSelect } from "../components/ProjectSelect";
import type { RoadmapMilestone, RoadmapMilestoneStatus } from "../types/models";

const statusLabels: Record<RoadmapMilestoneStatus, string> = {
  planned: "计划中",
  active: "进行中",
  achieved: "已达成",
  archived: "已归档",
};

function currentPeriod() {
  const now = new Date();
  return {
    year: now.getFullYear(),
    quarter: (Math.floor(now.getMonth() / 3) + 1) as 1 | 2 | 3 | 4,
  };
}

function targetDateFor(year: number, quarter: number) {
  const month = quarter * 3;
  const day = new Date(Date.UTC(year, month, 0)).getUTCDate();
  return `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

function firstDateFor(year: number, quarter: number) {
  return `${year}-${String((quarter - 1) * 3 + 1).padStart(2, "0")}-01`;
}

function milestoneStatusClass(status: RoadmapMilestoneStatus) {
  if (status === "active") return "status-purple";
  if (status === "achieved") return "status-green";
  if (status === "archived") return "status-neutral";
  return "status-blue";
}

function RoadmapMilestoneCard({
  milestone,
  onDelete,
  onEdit,
}: {
  milestone: RoadmapMilestone;
  onDelete: (milestone: RoadmapMilestone) => void;
  onEdit: (milestone: RoadmapMilestone) => void;
}) {
  const archive = useArchiveRoadmapMilestone();
  const restore = useRestoreRoadmapMilestone();
  const busy = archive.isPending || restore.isPending;
  const error = archive.error ?? restore.error;

  return (
    <article className="roadmap-card">
      <div className="roadmap-card-heading">
        <div>
          <span
            className={`status-badge ${milestoneStatusClass(milestone.status)}`}
          >
            {statusLabels[milestone.status]}
          </span>
          <h2>{milestone.title}</h2>
        </div>
        <span className="roadmap-date">
          <CalendarDays size={13} />
          {milestone.targetDate}
        </span>
      </div>
      <p>{milestone.description || "尚未填写里程碑说明。"}</p>
      <div className="project-progress-row">
        <div
          aria-label={`${milestone.title}关联任务完成进度`}
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={milestone.taskSummary.progressPercent}
          className="project-progress"
          role="progressbar"
        >
          <span
            style={{ width: `${milestone.taskSummary.progressPercent}%` }}
          />
        </div>
        <strong>{milestone.taskSummary.progressPercent}%</strong>
      </div>
      <div className="roadmap-card-meta">
        <span>
          {milestone.taskSummary.completed}/{milestone.taskSummary.total}{" "}
          项关联任务
        </span>
        <span>{milestone.projects.length} 个关联项目</span>
      </div>
      {milestone.projects.length > 0 ? (
        <div className="roadmap-project-links">
          {milestone.projects.map((project) => (
            <Link key={project.id} to={`/projects/${project.id}`}>
              <FolderKanban size={13} />
              {project.name}
            </Link>
          ))}
        </div>
      ) : null}
      {error ? (
        <p className="form-field-error">操作未完成，请刷新后重试。</p>
      ) : null}
      <footer className="roadmap-card-actions">
        {milestone.status === "archived" ? (
          <>
            <button
              className="button button-secondary"
              disabled={busy}
              onClick={() =>
                restore.mutate({
                  id: milestone.id,
                  expectedVersion: milestone.version,
                })
              }
              type="button"
            >
              <RotateCcw size={14} />
              恢复
            </button>
            <button
              className="button button-danger"
              disabled={busy}
              onClick={() => onDelete(milestone)}
              type="button"
            >
              <Trash2 size={14} />
              永久删除
            </button>
          </>
        ) : (
          <>
            <button
              className="button button-secondary"
              disabled={busy}
              onClick={() => onEdit(milestone)}
              type="button"
            >
              <Edit3 size={14} />
              编辑
            </button>
            <button
              className="button button-quiet"
              disabled={busy}
              onClick={() =>
                archive.mutate({
                  id: milestone.id,
                  expectedVersion: milestone.version,
                })
              }
              type="button"
            >
              <Archive size={14} />
              归档
            </button>
          </>
        )}
      </footer>
    </article>
  );
}

function CreateRoadmapMilestoneModal({
  open,
  onClose,
  year,
  quarter,
}: {
  open: boolean;
  onClose: () => void;
  year: number;
  quarter: 1 | 2 | 3 | 4;
}) {
  const create = useCreateRoadmapMilestone();
  const projects = useProjectsQuery(
    { page: 1, pageSize: 100, sort: "name" },
    open,
  );
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [targetDate, setTargetDate] = useState(targetDateFor(year, quarter));
  const [projectIds, setProjectIds] = useState<string[]>([]);
  const [validationError, setValidationError] = useState<string | null>(null);

  const close = () => {
    if (!create.isPending) onClose();
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanTitle = title.trim();
    if (!cleanTitle) {
      setValidationError("请填写里程碑标题。");
      return;
    }
    const targetMonth = Number(targetDate.slice(5, 7));
    if (
      !targetDate.startsWith(`${year}-`) ||
      targetMonth < (quarter - 1) * 3 + 1 ||
      targetMonth > quarter * 3
    ) {
      setValidationError("目标日期需要在当前季度内。");
      return;
    }
    setValidationError(null);
    create.mutate(
      {
        title: cleanTitle,
        description: description.trim() || null,
        year,
        quarter,
        targetDate,
        projectIds,
      },
      {
        onSuccess: () => {
          setTitle("");
          setDescription("");
          setTargetDate(targetDateFor(year, quarter));
          setProjectIds([]);
          onClose();
        },
      },
    );
  };
  const error =
    validationError || (create.isError ? "创建失败，请检查输入后重试。" : null);
  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={create.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={create.isPending}
            form="roadmap-milestone-form"
            type="submit"
          >
            {create.isPending ? "正在创建…" : "创建里程碑"}
          </button>
        </>
      }
      onClose={close}
      open={open}
      title={`新建 ${year} 年 Q${quarter} 里程碑`}
      width="620px"
    >
      <form id="roadmap-milestone-form" onSubmit={submit}>
        <label className="form-field">
          <span>里程碑标题</span>
          <input
            autoFocus
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            placeholder="例如：完成本地规划工作流"
            value={title}
          />
        </label>
        <label className="form-field">
          <span>说明</span>
          <textarea
            maxLength={4000}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="目标、范围与验收边界…"
            rows={3}
            value={description}
          />
        </label>
        <label className="form-field">
          <span>目标日期</span>
          <input
            max={targetDateFor(year, quarter)}
            min={firstDateFor(year, quarter)}
            onChange={(event) => setTargetDate(event.target.value)}
            type="date"
            value={targetDate}
          />
        </label>
        <fieldset className="form-field form-field-last">
          <legend>关联项目</legend>
          {projects.isPending ? (
            <span className="form-field-hint">正在读取项目…</span>
          ) : null}
          {projects.data?.items.map((project) => (
            <label className="roadmap-project-option" key={project.id}>
              <input
                checked={projectIds.includes(project.id)}
                onChange={(event) =>
                  setProjectIds((items) =>
                    event.target.checked
                      ? [...items, project.id]
                      : items.filter((id) => id !== project.id),
                  )
                }
                type="checkbox"
              />
              <span>{project.name}</span>
              <small>{project.taskSummary.progressPercent}%</small>
            </label>
          ))}
          {projects.isSuccess && projects.data.items.length === 0 ? (
            <span className="form-field-hint">
              暂无可关联项目，可先创建里程碑。
            </span>
          ) : null}
        </fieldset>
        {error ? <p className="form-field-error">{error}</p> : null}
      </form>
    </Modal>
  );
}

function EditRoadmapMilestoneModal({
  milestone,
  onClose,
}: {
  milestone: RoadmapMilestone;
  onClose: () => void;
}) {
  const update = useUpdateRoadmapMilestone();
  const projects = useProjectsQuery({
    page: 1,
    pageSize: 100,
    sort: "name",
    includeArchived: true,
  });
  const [title, setTitle] = useState(milestone.title);
  const [description, setDescription] = useState(milestone.description ?? "");
  const [year, setYear] = useState(milestone.year);
  const [quarter, setQuarter] = useState(milestone.quarter);
  const [targetDate, setTargetDate] = useState(milestone.targetDate);
  const [status, setStatus] = useState<
    Exclude<RoadmapMilestoneStatus, "archived">
  >(milestone.status === "archived" ? "planned" : milestone.status);
  const [projectIds, setProjectIds] = useState(
    milestone.projects.map((project) => project.id),
  );
  const [validationError, setValidationError] = useState<string | null>(null);
  const close = () => {
    if (!update.isPending) onClose();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const cleanTitle = title.trim();
    if (!cleanTitle) {
      setValidationError("请填写里程碑标题。");
      return;
    }
    if (
      targetDate < firstDateFor(year, quarter) ||
      targetDate > targetDateFor(year, quarter)
    ) {
      setValidationError("目标日期需要在所选季度内。");
      return;
    }
    setValidationError(null);
    update.mutate(
      {
        id: milestone.id,
        input: {
          title: cleanTitle,
          description: description.trim() || null,
          year,
          quarter,
          targetDate,
          status,
          projectIds,
          expectedVersion: milestone.version,
        },
      },
      { onSuccess: onClose },
    );
  };
  const error =
    validationError ||
    (update.isError ? "保存失败，数据可能已变化，请刷新后重试。" : null);

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={update.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-primary"
            disabled={update.isPending}
            form="roadmap-milestone-edit-form"
            type="submit"
          >
            {update.isPending ? "正在保存…" : "保存更改"}
          </button>
        </>
      }
      onClose={close}
      open
      title="编辑里程碑"
      width="620px"
    >
      <form id="roadmap-milestone-edit-form" onSubmit={submit}>
        <label className="form-field">
          <span>里程碑标题</span>
          <input
            autoFocus
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            value={title}
          />
        </label>
        <label className="form-field">
          <span>说明</span>
          <textarea
            maxLength={4000}
            onChange={(event) => setDescription(event.target.value)}
            rows={3}
            value={description}
          />
        </label>
        <div className="roadmap-edit-period">
          <label className="form-field">
            <span>年份</span>
            <input
              max={9999}
              min={1}
              onChange={(event) => {
                const nextYear = Number(event.target.value);
                setYear(nextYear);
                if (nextYear > 0) {
                  setTargetDate(targetDateFor(nextYear, quarter));
                }
              }}
              type="number"
              value={year}
            />
          </label>
          <label className="form-field">
            <span>季度</span>
            <select
              onChange={(event) => {
                const nextQuarter = Number(event.target.value) as 1 | 2 | 3 | 4;
                setQuarter(nextQuarter);
                setTargetDate(targetDateFor(year, nextQuarter));
              }}
              value={quarter}
            >
              {[1, 2, 3, 4].map((item) => (
                <option key={item} value={item}>
                  Q{item}
                </option>
              ))}
            </select>
          </label>
          <label className="form-field">
            <span>状态</span>
            <select
              onChange={(event) =>
                setStatus(
                  event.target.value as Exclude<
                    RoadmapMilestoneStatus,
                    "archived"
                  >,
                )
              }
              value={status}
            >
              <option value="planned">计划中</option>
              <option value="active">进行中</option>
              <option value="achieved">已达成</option>
            </select>
          </label>
        </div>
        <label className="form-field">
          <span>目标日期</span>
          <input
            max={targetDateFor(year, quarter)}
            min={firstDateFor(year, quarter)}
            onChange={(event) => setTargetDate(event.target.value)}
            type="date"
            value={targetDate}
          />
        </label>
        <fieldset className="form-field form-field-last">
          <legend>关联项目</legend>
          {projects.isPending ? (
            <span className="form-field-hint">正在读取项目…</span>
          ) : null}
          {projects.data?.items.map((project) => (
            <label className="roadmap-project-option" key={project.id}>
              <input
                checked={projectIds.includes(project.id)}
                onChange={(event) =>
                  setProjectIds((items) =>
                    event.target.checked
                      ? [...items, project.id]
                      : items.filter((id) => id !== project.id),
                  )
                }
                type="checkbox"
              />
              <span>{project.name}</span>
              <small>{project.taskSummary.progressPercent}%</small>
            </label>
          ))}
        </fieldset>
        {error ? <p className="form-field-error">{error}</p> : null}
      </form>
    </Modal>
  );
}

function DeleteRoadmapMilestoneModal({
  milestone,
  onClose,
}: {
  milestone: RoadmapMilestone;
  onClose: () => void;
}) {
  const remove = useDeleteRoadmapMilestone();
  const close = () => {
    if (!remove.isPending) onClose();
  };
  const submit = () => {
    remove.mutate(
      { id: milestone.id, expectedVersion: milestone.version },
      { onSuccess: onClose },
    );
  };

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            disabled={remove.isPending}
            onClick={close}
            type="button"
          >
            取消
          </button>
          <button
            className="button button-danger"
            disabled={remove.isPending}
            onClick={submit}
            type="button"
          >
            {remove.isPending ? "正在删除…" : "确认永久删除"}
          </button>
        </>
      }
      onClose={close}
      open
      title="永久删除里程碑"
      width="500px"
    >
      <div className="roadmap-delete-confirm-copy">
        <AlertTriangle size={22} />
        <div>
          <strong>{milestone.title}</strong>
          <p>此操作不可撤销。关联项目和任务不会被删除。</p>
        </div>
      </div>
      {remove.isError ? (
        <p className="form-field-error">
          删除失败，数据可能已变化，请刷新后重试。
        </p>
      ) : null}
    </Modal>
  );
}

export function RoadmapPage() {
  const initial = useMemo(currentPeriod, []);
  const [year, setYear] = useState(initial.year);
  const [quarter, setQuarter] = useState(initial.quarter);
  const [status, setStatus] = useState<RoadmapMilestoneStatus | "">("");
  const [projectId, setProjectId] = useState("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<RoadmapMilestone | null>(null);
  const [deleting, setDeleting] = useState<RoadmapMilestone | null>(null);
  const query = useRoadmapMilestonesQuery({
    page,
    pageSize: 20,
    year,
    quarter,
    status: status || undefined,
    projectId: projectId || undefined,
    includeArchived: status === "archived",
  });
  const milestones = query.data?.items ?? [];
  const total = query.data?.meta.total ?? 0;
  const pageSize = query.data?.meta.pageSize ?? 20;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  useEffect(() => {
    if (!query.data || query.isFetching || page <= totalPages) return;
    setPage(totalPages);
  }, [page, query.data, query.isFetching, totalPages]);

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
            新建里程碑
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
        title="路线图"
      />
      <div className="toolbar roadmap-toolbar">
        <label className="toolbar-select">
          <span className="sr-only">年份</span>
          <select
            onChange={(event) => {
              setYear(Number(event.target.value));
              setPage(1);
            }}
            value={year}
          >
            {[year - 1, year, year + 1].map((item) => (
              <option key={item} value={item}>
                {item} 年
              </option>
            ))}
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">季度</span>
          <select
            onChange={(event) => {
              setQuarter(Number(event.target.value) as 1 | 2 | 3 | 4);
              setPage(1);
            }}
            value={quarter}
          >
            {[1, 2, 3, 4].map((item) => (
              <option key={item} value={item}>
                Q{item}
              </option>
            ))}
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">状态</span>
          <select
            onChange={(event) => {
              setStatus(event.target.value as RoadmapMilestoneStatus | "");
              setPage(1);
            }}
            value={status}
          >
            <option value="">未归档</option>
            <option value="planned">计划中</option>
            <option value="active">进行中</option>
            <option value="achieved">已达成</option>
            <option value="archived">已归档</option>
          </select>
        </label>
        <ProjectSelect
          ariaLabel="关联项目筛选"
          emptyLabel="全部项目"
          includeArchived
          onChange={(value) => {
            setProjectId(value);
            setPage(1);
          }}
          value={projectId}
          variant="toolbar"
        />
      </div>
      {query.isFetching && !query.isPending ? (
        <p className="roadmap-refresh" role="status">
          正在刷新路线图…
        </p>
      ) : null}
      {query.isError ? (
        <ErrorState
          message="无法读取路线图数据，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isSuccess && milestones.length === 0 ? (
        <EmptyState
          action={
            <button
              className="button button-primary"
              onClick={() => setCreating(true)}
              type="button"
            >
              <Plus size={15} />
              新建第一个里程碑
            </button>
          }
          message="里程碑会关联真实项目与任务，不会创建演示数据。"
          title={`${year} 年 Q${quarter} 暂无里程碑`}
        />
      ) : null}
      {milestones.length > 0 ? (
        <>
          <section
            className="roadmap-grid"
            aria-label={`${year} 年 Q${quarter} 路线图`}
          >
            <header className="roadmap-section-heading">
              <Map size={17} />
              <div>
                <strong>
                  {year} 年 · Q{quarter}
                </strong>
                <span>按目标日期和手动排序展示</span>
              </div>
            </header>
            {milestones.map((milestone) => (
              <RoadmapMilestoneCard
                key={milestone.id}
                milestone={milestone}
                onDelete={setDeleting}
                onEdit={setEditing}
              />
            ))}
          </section>
          {totalPages > 1 ? (
            <nav aria-label="路线图分页" className="pagination">
              <button
                className="button button-secondary"
                disabled={page <= 1 || query.isFetching}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
                type="button"
              >
                上一页
              </button>
              <span>
                {(page - 1) * pageSize + 1}–
                {Math.min((page - 1) * pageSize + milestones.length, total)} /{" "}
                {total}
              </span>
              <button
                className="button button-secondary"
                disabled={page >= totalPages || query.isFetching}
                onClick={() =>
                  setPage((value) => Math.min(totalPages, value + 1))
                }
                type="button"
              >
                下一页
              </button>
            </nav>
          ) : null}
        </>
      ) : null}
      <CreateRoadmapMilestoneModal
        onClose={() => setCreating(false)}
        open={creating}
        quarter={quarter}
        year={year}
      />
      {editing ? (
        <EditRoadmapMilestoneModal
          key={`${editing.id}:${editing.version}`}
          milestone={editing}
          onClose={() => setEditing(null)}
        />
      ) : null}
      {deleting ? (
        <DeleteRoadmapMilestoneModal
          key={`${deleting.id}:${deleting.version}`}
          milestone={deleting}
          onClose={() => setDeleting(null)}
        />
      ) : null}
    </div>
  );
}
