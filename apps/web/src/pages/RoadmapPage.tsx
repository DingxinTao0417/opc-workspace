import {
  Archive,
  CalendarDays,
  FolderKanban,
  Map,
  Plus,
  RotateCcw,
} from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  useArchiveRoadmapMilestone,
  useCreateRoadmapMilestone,
  useProjectsQuery,
  useRestoreRoadmapMilestone,
  useRoadmapMilestonesQuery,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
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
  return `${year}-${String(quarter * 3).padStart(2, "0")}-28`;
}

function milestoneStatusClass(status: RoadmapMilestoneStatus) {
  if (status === "active") return "status-purple";
  if (status === "achieved") return "status-green";
  if (status === "archived") return "status-neutral";
  return "status-blue";
}

function RoadmapMilestoneCard({ milestone }: { milestone: RoadmapMilestone }) {
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
        ) : (
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
            min={`${year}-${String((quarter - 1) * 3 + 1).padStart(2, "0")}-01`}
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

export function RoadmapPage() {
  const initial = useMemo(currentPeriod, []);
  const [year, setYear] = useState(initial.year);
  const [quarter, setQuarter] = useState(initial.quarter);
  const [status, setStatus] = useState<RoadmapMilestoneStatus | "">("");
  const [creating, setCreating] = useState(false);
  const query = useRoadmapMilestonesQuery({
    year,
    quarter,
    status: status || undefined,
    includeArchived: status === "archived",
  });
  const milestones = query.data?.items ?? [];

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
            onChange={(event) => setYear(Number(event.target.value))}
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
            onChange={(event) =>
              setQuarter(Number(event.target.value) as 1 | 2 | 3 | 4)
            }
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
            onChange={(event) =>
              setStatus(event.target.value as RoadmapMilestoneStatus | "")
            }
            value={status}
          >
            <option value="">未归档</option>
            <option value="planned">计划中</option>
            <option value="active">进行中</option>
            <option value="achieved">已达成</option>
            <option value="archived">已归档</option>
          </select>
        </label>
      </div>
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
            <RoadmapMilestoneCard key={milestone.id} milestone={milestone} />
          ))}
        </section>
      ) : null}
      <CreateRoadmapMilestoneModal
        onClose={() => setCreating(false)}
        open={creating}
        quarter={quarter}
        year={year}
      />
    </div>
  );
}
