import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Archive,
  CalendarDays,
  Edit3,
  Eye,
  FolderKanban,
  GripVertical,
  Map,
  Plus,
  RotateCcw,
  Save,
  Trash2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
  useArchiveRoadmapMilestone,
  useCreateRoadmapMilestone,
  useDeleteRoadmapMilestone,
  useProjectsQuery,
  useReorderRoadmapMilestones,
  useRestoreRoadmapMilestone,
  useRoadmapMilestonesQuery,
  useUpdateRoadmapMilestone,
} from "../api/hooks";
import { EmptyState, ErrorState, SkeletonRows } from "../components/feedback";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { ProjectSelect } from "../components/ProjectSelect";
import { RoadmapMilestoneDetailModal } from "../components/RoadmapMilestoneDetailModal";
import { useLocalCalendar } from "../lib/localCalendar";
import { useSettledPage } from "../lib/useSettledPage";
import type { RoadmapMilestone, RoadmapMilestoneStatus } from "../types/models";

const statusLabels: Record<RoadmapMilestoneStatus, string> = {
  planned: "计划中",
  active: "进行中",
  achieved: "已达成",
  archived: "已归档",
};

function currentPeriod(dateKey: string) {
  const year = Number(dateKey.slice(0, 4));
  const month = Number(dateKey.slice(5, 7));
  return {
    year,
    quarter: (Math.floor((month - 1) / 3) + 1) as 1 | 2 | 3 | 4,
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

function adjacentQuarter(
  year: number,
  quarter: 1 | 2 | 3 | 4,
  direction: -1 | 1,
) {
  if (quarter === 1 && direction === -1)
    return { year: year - 1, quarter: 4 as const };
  if (quarter === 4 && direction === 1)
    return { year: year + 1, quarter: 1 as const };
  return {
    year,
    quarter: (quarter + direction) as 1 | 2 | 3 | 4,
  };
}

function quarterCalendarMonths(year: number, quarter: number) {
  return [0, 1, 2].map((offset) => {
    const month = (quarter - 1) * 3 + offset + 1;
    const days = new Date(Date.UTC(year, month, 0)).getUTCDate();
    return {
      label: `${month} 月`,
      leadingDays: (new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 6) % 7,
      dates: Array.from(
        { length: days },
        (_, index) =>
          `${year}-${String(month).padStart(2, "0")}-${String(index + 1).padStart(2, "0")}`,
      ),
    };
  });
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
  onOpen,
  ordering = false,
  quarterMoving = false,
  dateMoving = false,
  dateDraft,
  orderIndex = 0,
  orderTotal = 0,
  dragging = false,
  onDragStart,
  onDragEnd,
  onDrop,
  onMove,
  onDateDraftChange,
  onDateSave,
  moveDisabled = false,
}: {
  milestone: RoadmapMilestone;
  onDelete: (milestone: RoadmapMilestone) => void;
  onEdit: (milestone: RoadmapMilestone) => void;
  onOpen: (milestone: RoadmapMilestone) => void;
  ordering?: boolean;
  quarterMoving?: boolean;
  dateMoving?: boolean;
  dateDraft?: string;
  orderIndex?: number;
  orderTotal?: number;
  dragging?: boolean;
  onDragStart?: () => void;
  onDragEnd?: () => void;
  onDrop?: () => void;
  onMove?: (direction: -1 | 1) => void;
  onDateDraftChange?: (value: string) => void;
  onDateSave?: () => void;
  moveDisabled?: boolean;
}) {
  const archive = useArchiveRoadmapMilestone();
  const restore = useRestoreRoadmapMilestone();
  const busy = archive.isPending || restore.isPending;
  const error = archive.error ?? restore.error;
  const movable = ordering || quarterMoving || dateMoving;
  const dragEnabled = movable && !moveDisabled;

  return (
    <article
      className={`roadmap-card${movable ? " is-ordering" : ""}${dragging ? " is-dragging" : ""}`}
      draggable={dragEnabled}
      onDragEnd={dragEnabled ? onDragEnd : undefined}
      onDragOver={
        dragEnabled
          ? (event) => {
              event.preventDefault();
            }
          : undefined
      }
      onDragStart={dragEnabled ? onDragStart : undefined}
      onDrop={dragEnabled ? onDrop : undefined}
    >
      <div className="roadmap-card-heading">
        <div>
          {movable ? (
            <span aria-hidden="true" className="roadmap-drag-handle">
              <GripVertical size={16} />
            </span>
          ) : null}
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
        {dateMoving ? (
          <div className="roadmap-date-controls">
            <label>
              <span className="sr-only">目标日期：{milestone.title}</span>
              <input
                aria-label={`目标日期：${milestone.title}`}
                disabled={moveDisabled}
                max={targetDateFor(milestone.year, milestone.quarter)}
                min={firstDateFor(milestone.year, milestone.quarter)}
                onChange={(event) => onDateDraftChange?.(event.target.value)}
                type="date"
                value={dateDraft ?? milestone.targetDate}
              />
            </label>
            <button
              aria-label={`应用目标日期：${milestone.title}`}
              className="button button-secondary"
              disabled={
                moveDisabled || !dateDraft || dateDraft === milestone.targetDate
              }
              onClick={onDateSave}
              type="button"
            >
              <CalendarDays size={14} />
              应用日期
            </button>
          </div>
        ) : quarterMoving ? (
          <div className="roadmap-reorder-controls">
            <span aria-live="polite">
              当前季度 {milestone.year} Q{milestone.quarter}
            </span>
            <button
              aria-label={`移到上一季度：${milestone.title}`}
              className="button button-secondary"
              disabled={moveDisabled}
              onClick={() => onMove?.(-1)}
              type="button"
            >
              <ArrowUp size={14} />
              上一季度
            </button>
            <button
              aria-label={`移到下一季度：${milestone.title}`}
              className="button button-secondary"
              disabled={moveDisabled}
              onClick={() => onMove?.(1)}
              type="button"
            >
              <ArrowDown size={14} />
              下一季度
            </button>
          </div>
        ) : ordering ? (
          <div className="roadmap-reorder-controls">
            <span aria-live="polite">
              当前位置 {orderIndex + 1} / {orderTotal}
            </span>
            <button
              aria-label={`上移：${milestone.title}`}
              className="button button-secondary"
              disabled={orderIndex === 0}
              onClick={() => onMove?.(-1)}
              type="button"
            >
              <ArrowUp size={14} />
              上移
            </button>
            <button
              aria-label={`下移：${milestone.title}`}
              className="button button-secondary"
              disabled={orderIndex === orderTotal - 1}
              onClick={() => onMove?.(1)}
              type="button"
            >
              <ArrowDown size={14} />
              下移
            </button>
          </div>
        ) : milestone.status === "archived" ? (
          <>
            <button
              className="button button-secondary"
              onClick={() => onOpen(milestone)}
              type="button"
            >
              <Eye size={14} />
              查看详情
            </button>
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
              onClick={() => onOpen(milestone)}
              type="button"
            >
              <Eye size={14} />
              查看详情
            </button>
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

  useEffect(() => {
    if (!open) setTargetDate(targetDateFor(year, quarter));
  }, [open, quarter, year]);

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
  const { dateKey } = useLocalCalendar();
  const current = useMemo(() => currentPeriod(dateKey), [dateKey]);
  const previousCurrent = useRef(current);
  const [searchParams, setSearchParams] = useSearchParams();
  const [year, setYear] = useState(current.year);
  const [quarter, setQuarter] = useState(current.quarter);
  const [status, setStatus] = useState<RoadmapMilestoneStatus | "">("");
  const [projectId, setProjectId] = useState("");
  const [page, setPage] = useState(1);
  const [viewMode, setViewMode] = useState<"quarter" | "year">("quarter");
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<RoadmapMilestone | null>(null);
  const [deleting, setDeleting] = useState<RoadmapMilestone | null>(null);
  const detailId = searchParams.get("milestone")?.trim() || null;
  const [reordering, setReordering] = useState(false);
  const [quarterMoving, setQuarterMoving] = useState(false);
  const [dateMoving, setDateMoving] = useState(false);
  const [dateDrafts, setDateDrafts] = useState<Record<string, string>>({});
  const [dateDraftInitialized, setDateDraftInitialized] = useState(false);
  const [reorderInitialized, setReorderInitialized] = useState(false);
  const [draftOrder, setDraftOrder] = useState<RoadmapMilestone[]>([]);
  const [draggedId, setDraggedId] = useState<string | null>(null);
  const reorder = useReorderRoadmapMilestones();
  const periodMove = useUpdateRoadmapMilestone();
  const dateMove = useUpdateRoadmapMilestone();
  const query = useRoadmapMilestonesQuery({
    page: reordering || dateMoving ? 1 : page,
    pageSize: reordering || dateMoving || viewMode === "year" ? 100 : 20,
    year,
    quarter: viewMode === "quarter" ? quarter : undefined,
    status: status || undefined,
    projectId: projectId || undefined,
    includeArchived: status === "archived",
  });
  const milestones = query.data?.items ?? [];
  const total = query.data?.meta.total ?? 0;
  const pageSize = query.data?.meta.pageSize ?? 20;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const reorderReady =
    reordering &&
    query.isSuccess &&
    !query.isFetching &&
    query.data.meta.page === 1 &&
    query.data.meta.pageSize === 100 &&
    query.data.items.length === query.data.meta.total;
  const dateMoveReady =
    dateMoving &&
    query.isSuccess &&
    !query.isFetching &&
    query.data.meta.page === 1 &&
    query.data.meta.pageSize === 100 &&
    query.data.items.length === query.data.meta.total;
  const visibleMilestones = reordering
    ? reorderInitialized
      ? draftOrder
      : []
    : milestones;
  const reorderDisabledReason = status
    ? "请先切换到未归档的全部状态。"
    : projectId
      ? "请先清除 Project 筛选。"
      : total > 100
        ? "当前季度超过 100 个里程碑，暂不支持完整批量排序。"
        : total < 2
          ? "至少需要两个里程碑才能调整顺序。"
          : query.isFetching || !query.isSuccess
            ? "路线图仍在读取，请稍候。"
            : null;
  const quarterMoveDisabledReason = status
    ? "请先切换到未归档的全部状态。"
    : projectId
      ? "请先清除 Project 筛选。"
      : total > 100
        ? "当前年度超过 100 个里程碑，暂不支持跨季度拖拽。"
        : total < 1
          ? "当前年度没有可移动的里程碑。"
          : query.isFetching || !query.isSuccess
            ? "路线图仍在读取，请稍候。"
            : null;
  const dateMoveDisabledReason = status
    ? "请先切换到未归档的全部状态。"
    : projectId
      ? "请先清除 Project 筛选。"
      : total > 100
        ? "当前季度超过 100 个里程碑，暂不支持完整日期调整。"
        : total < 1
          ? "当前季度没有可调整日期的里程碑。"
          : query.isFetching || !query.isSuccess
            ? "路线图仍在读取，请稍候。"
            : null;
  const openDetail = (id: string) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      next.set("milestone", id);
      return next;
    });
  };
  const closeDetail = () => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        next.delete("milestone");
        return next;
      },
      { replace: true },
    );
  };
  const calendarMonths = useMemo(
    () => quarterCalendarMonths(year, quarter),
    [quarter, year],
  );

  useEffect(() => {
    const previous = previousCurrent.current;
    previousCurrent.current = current;
    if (
      year === previous.year &&
      quarter === previous.quarter &&
      (year !== current.year || quarter !== current.quarter)
    ) {
      setYear(current.year);
      setQuarter(current.quarter);
      setPage(1);
    }
  }, [current, quarter, year]);

  useSettledPage({
    page,
    meta: query.data?.meta,
    isBlocked: reordering || dateMoving,
    isFetching: query.isFetching,
    isPlaceholderData: query.isPlaceholderData,
    isSuccess: query.isSuccess,
    setPage,
  });

  useEffect(() => {
    if (!reorderReady || reorderInitialized) return;
    setDraftOrder(query.data.items);
    setReorderInitialized(true);
  }, [query.data, reorderInitialized, reorderReady]);

  useEffect(() => {
    if (!dateMoveReady || dateMove.isPending || dateDraftInitialized) return;
    setDateDrafts(
      Object.fromEntries(
        query.data.items.map((milestone) => [
          milestone.id,
          milestone.targetDate,
        ]),
      ),
    );
    setDateDraftInitialized(true);
  }, [dateDraftInitialized, dateMove.isPending, dateMoveReady, query.data]);

  const cancelReorder = () => {
    if (reorder.isPending) return;
    setReordering(false);
    setReorderInitialized(false);
    setDraftOrder([]);
    setDraggedId(null);
    reorder.reset();
  };
  const moveDraftItem = (id: string, nextIndex: number) => {
    setDraftOrder((items) => {
      const currentIndex = items.findIndex((item) => item.id === id);
      if (
        currentIndex < 0 ||
        nextIndex < 0 ||
        nextIndex >= items.length ||
        currentIndex === nextIndex
      ) {
        return items;
      }
      const next = [...items];
      const [moved] = next.splice(currentIndex, 1);
      next.splice(nextIndex, 0, moved);
      return next;
    });
    reorder.reset();
  };
  const saveReorder = () => {
    if (!reorderReady || !reorderInitialized || draftOrder.length < 2) return;
    reorder.mutate(
      {
        items: draftOrder.map((milestone) => ({
          id: milestone.id,
          expectedVersion: milestone.version,
        })),
      },
      {
        onSuccess: () => {
          setReordering(false);
          setReorderInitialized(false);
          setDraftOrder([]);
          setDraggedId(null);
        },
        onError: () => {
          setReorderInitialized(false);
          setDraggedId(null);
        },
      },
    );
  };
  const moveToQuarter = (
    milestone: RoadmapMilestone,
    nextYear: number,
    nextQuarter: number,
  ) => {
    if (
      periodMove.isPending ||
      nextQuarter < 1 ||
      nextQuarter > 4 ||
      (nextYear === milestone.year && nextQuarter === milestone.quarter)
    )
      return;
    periodMove.reset();
    periodMove.mutate(
      {
        id: milestone.id,
        input: {
          year: nextYear,
          quarter: nextQuarter as 1 | 2 | 3 | 4,
          targetDate: targetDateFor(nextYear, nextQuarter),
          expectedVersion: milestone.version,
        },
      },
      { onError: () => void query.refetch() },
    );
  };
  const moveToDate = (milestone: RoadmapMilestone, targetDate: string) => {
    if (
      dateMove.isPending ||
      targetDate < firstDateFor(year, quarter) ||
      targetDate > targetDateFor(year, quarter) ||
      targetDate === milestone.targetDate
    )
      return;
    dateMove.reset();
    dateMove.mutate(
      {
        id: milestone.id,
        input: { targetDate, expectedVersion: milestone.version },
      },
      {
        onSuccess: () => {
          setDraggedId(null);
          setDateDraftInitialized(false);
        },
        onError: () => {
          setDraggedId(null);
          setDateDraftInitialized(false);
          void query.refetch();
        },
      },
    );
  };

  return (
    <div className="page">
      <PageHeader
        actions={
          <div className="roadmap-header-actions">
            {!reordering && !quarterMoving && !dateMoving ? (
              viewMode === "quarter" ? (
                <>
                  <button
                    className="button button-secondary"
                    disabled={Boolean(reorderDisabledReason)}
                    onClick={() => {
                      setPage(1);
                      setReordering(true);
                      setReorderInitialized(false);
                      setDraftOrder([]);
                      reorder.reset();
                    }}
                    title={reorderDisabledReason ?? "调整本季度里程碑顺序"}
                    type="button"
                  >
                    <GripVertical size={15} />
                    调整顺序
                  </button>
                  <button
                    className="button button-secondary"
                    disabled={Boolean(dateMoveDisabledReason)}
                    onClick={() => {
                      setPage(1);
                      setDateMoving(true);
                      setDateDrafts({});
                      setDateDraftInitialized(false);
                      setDraggedId(null);
                      dateMove.reset();
                    }}
                    title={dateMoveDisabledReason ?? "调整本季度精确目标日期"}
                    type="button"
                  >
                    <CalendarDays size={15} />
                    调整日期
                  </button>
                </>
              ) : (
                <button
                  className="button button-secondary"
                  disabled={Boolean(quarterMoveDisabledReason)}
                  onClick={() => {
                    setQuarterMoving(true);
                    setDraggedId(null);
                    periodMove.reset();
                  }}
                  title={quarterMoveDisabledReason ?? "调整里程碑所属季度"}
                  type="button"
                >
                  <GripVertical size={15} />
                  调整季度
                </button>
              )
            ) : null}
            <button
              className="button button-primary"
              disabled={reordering || quarterMoving || dateMoving}
              onClick={() => setCreating(true)}
              type="button"
            >
              <Plus size={15} />
              新建里程碑
            </button>
          </div>
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
          <span className="sr-only">展示范围</span>
          <select
            disabled={reordering || quarterMoving || dateMoving}
            onChange={(event) => {
              setViewMode(event.target.value as "quarter" | "year");
              setPage(1);
              setQuarterMoving(false);
              setDateMoving(false);
              setDraggedId(null);
            }}
            value={viewMode}
          >
            <option value="quarter">季度视图</option>
            <option value="year">年度视图</option>
          </select>
        </label>
        <label className="toolbar-select">
          <span className="sr-only">年份</span>
          <select
            disabled={reordering || quarterMoving || dateMoving}
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
            disabled={reordering || quarterMoving || dateMoving}
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
            disabled={reordering || quarterMoving || dateMoving}
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
          disabled={reordering || quarterMoving || dateMoving}
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
      {dateMoving ? (
        <>
          <section
            className="roadmap-reorder-bar"
            aria-label="目标日期调整工具"
          >
            <div>
              <strong>调整精确目标日期</strong>
              <span>
                把卡片拖到下方具体日期，或在卡片内输入日期后应用；每次移动都会检查当前版本并立即保存。
              </span>
              {!dateMoveReady ? (
                <span role="status">正在读取本季度完整里程碑列表…</span>
              ) : null}
              {dateMove.isError ? (
                <span className="form-field-error" role="alert">
                  日期调整失败，卡片仍保留原日期；数据可能已变化，请重试。
                </span>
              ) : null}
            </div>
            <div className="roadmap-reorder-actions">
              <button
                className="button button-secondary"
                disabled={dateMove.isPending}
                onClick={() => {
                  setDateMoving(false);
                  setDateDrafts({});
                  setDateDraftInitialized(false);
                  setDraggedId(null);
                  dateMove.reset();
                }}
                type="button"
              >
                <X size={14} />
                完成
              </button>
            </div>
          </section>
          {dateMoveReady ? (
            <section
              aria-label={`${year} 年 Q${quarter} 目标日期日历`}
              className="roadmap-date-calendar"
            >
              {calendarMonths.map((month) => (
                <section className="roadmap-date-month" key={month.label}>
                  <header>{month.label}</header>
                  <div className="roadmap-date-days">
                    {(["一", "二", "三", "四", "五", "六", "日"] as const).map(
                      (weekday) => (
                        <span className="roadmap-date-weekday" key={weekday}>
                          {weekday}
                        </span>
                      ),
                    )}
                    {Array.from({ length: month.leadingDays }, (_, index) => (
                      <span
                        aria-hidden="true"
                        className="roadmap-date-placeholder"
                        key={`blank-${index}`}
                      />
                    ))}
                    {month.dates.map((date) => (
                      <button
                        aria-label={`移动到 ${date}`}
                        className="roadmap-date-drop"
                        disabled={dateMove.isPending}
                        key={date}
                        onDragOver={(event) => event.preventDefault()}
                        onDrop={() => {
                          const moving = visibleMilestones.find(
                            (milestone) => milestone.id === draggedId,
                          );
                          if (moving) moveToDate(moving, date);
                          else setDraggedId(null);
                        }}
                        type="button"
                      >
                        {Number(date.slice(-2))}
                      </button>
                    ))}
                  </div>
                </section>
              ))}
            </section>
          ) : null}
        </>
      ) : null}
      {quarterMoving ? (
        <section className="roadmap-reorder-bar" aria-label="季度移动工具">
          <div>
            <strong>调整所属季度</strong>
            <span>
              把卡片拖到 Q1–Q4
              或年度边界，或使用上一季度/下一季度按钮；移动会把目标日期设为目标季度末。
            </span>
            {periodMove.isError ? (
              <span className="form-field-error" role="alert">
                移动失败，卡片仍保留在原季度；数据可能已变化，请重试。
              </span>
            ) : null}
          </div>
          <div className="roadmap-reorder-actions">
            <button
              className="button button-secondary"
              disabled={periodMove.isPending}
              onClick={() => {
                setQuarterMoving(false);
                setDraggedId(null);
                periodMove.reset();
              }}
              type="button"
            >
              <X size={14} />
              完成
            </button>
          </div>
        </section>
      ) : null}
      {reordering ? (
        <section className="roadmap-reorder-bar" aria-label="路线图排序工具">
          <div>
            <strong>调整本季度顺序</strong>
            <span>
              {reorderReady && reorderInitialized
                ? "拖动卡片，或用每张卡片的上移、下移按钮调整。保存时会检查所有数据版本。"
                : "正在读取本季度完整里程碑列表…"}
            </span>
            {reorder.isError ? (
              <span className="form-field-error" role="alert">
                保存失败，顺序已恢复到服务端最新状态；请检查后重试。
              </span>
            ) : null}
          </div>
          <div className="roadmap-reorder-actions">
            <button
              className="button button-secondary"
              disabled={reorder.isPending}
              onClick={cancelReorder}
              type="button"
            >
              <X size={14} />
              取消
            </button>
            <button
              className="button button-primary"
              disabled={
                !reorderReady || !reorderInitialized || reorder.isPending
              }
              onClick={saveReorder}
              type="button"
            >
              <Save size={14} />
              {reorder.isPending ? "正在保存…" : "保存顺序"}
            </button>
          </div>
        </section>
      ) : null}
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
      {query.isSuccess && !reordering && milestones.length === 0 ? (
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
          title={
            viewMode === "year"
              ? `${year} 年暂无里程碑`
              : `${year} 年 Q${quarter} 暂无里程碑`
          }
        />
      ) : null}
      {visibleMilestones.length > 0 ? (
        <>
          {viewMode === "year" ? (
            <section
              aria-label={`${year} 年路线图`}
              className="roadmap-year-panel"
            >
              {quarterMoving ? (
                <div
                  aria-label={`移动到 ${year - 1} 年 Q4`}
                  className="roadmap-year-boundary-drop"
                  onDragOver={(event) => event.preventDefault()}
                  onDrop={() => {
                    const moving = visibleMilestones.find(
                      (milestone) => milestone.id === draggedId,
                    );
                    if (moving) moveToQuarter(moving, year - 1, 4);
                    setDraggedId(null);
                  }}
                  role="region"
                >
                  <ArrowUp size={14} />
                  上一年 Q4 · {year - 1}
                </div>
              ) : null}
              {([1, 2, 3, 4] as const).map((itemQuarter) => {
                const quarterMilestones = visibleMilestones.filter(
                  (milestone) => milestone.quarter === itemQuarter,
                );
                return (
                  <section
                    className="roadmap-quarter-block"
                    key={itemQuarter}
                    onDragOver={
                      quarterMoving
                        ? (event) => event.preventDefault()
                        : undefined
                    }
                    onDrop={
                      quarterMoving
                        ? () => {
                            const moving = visibleMilestones.find(
                              (milestone) => milestone.id === draggedId,
                            );
                            if (moving)
                              moveToQuarter(moving, year, itemQuarter);
                            setDraggedId(null);
                          }
                        : undefined
                    }
                  >
                    <header className="roadmap-quarter-heading">
                      <strong>
                        Q{itemQuarter} · {year}
                      </strong>
                      <span>{quarterMilestones.length} 个里程碑</span>
                    </header>
                    <div className="roadmap-quarter-grid">
                      {quarterMilestones.length === 0 ? (
                        <p className="roadmap-quarter-empty">
                          {quarterMoving
                            ? `拖到这里，目标日期将设为 Q${itemQuarter} 季度末。`
                            : "本季度暂无里程碑。"}
                        </p>
                      ) : null}
                      {quarterMilestones.map((milestone) => (
                        <RoadmapMilestoneCard
                          dragging={draggedId === milestone.id}
                          key={milestone.id}
                          milestone={milestone}
                          moveDisabled={periodMove.isPending}
                          onDelete={setDeleting}
                          onDragEnd={() => setDraggedId(null)}
                          onDragStart={() => setDraggedId(milestone.id)}
                          onEdit={setEditing}
                          onMove={(direction) => {
                            const target = adjacentQuarter(
                              milestone.year,
                              milestone.quarter,
                              direction,
                            );
                            moveToQuarter(
                              milestone,
                              target.year,
                              target.quarter,
                            );
                          }}
                          onOpen={(value) => openDetail(value.id)}
                          quarterMoving={quarterMoving}
                        />
                      ))}
                    </div>
                  </section>
                );
              })}
              {quarterMoving ? (
                <div
                  aria-label={`移动到 ${year + 1} 年 Q1`}
                  className="roadmap-year-boundary-drop"
                  onDragOver={(event) => event.preventDefault()}
                  onDrop={() => {
                    const moving = visibleMilestones.find(
                      (milestone) => milestone.id === draggedId,
                    );
                    if (moving) moveToQuarter(moving, year + 1, 1);
                    setDraggedId(null);
                  }}
                  role="region"
                >
                  <ArrowDown size={14} />
                  下一年 Q1 · {year + 1}
                </div>
              ) : null}
            </section>
          ) : (
            <section
              className={`roadmap-grid${reordering || dateMoving ? " is-reordering" : ""}`}
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
              {visibleMilestones.map((milestone, index) => (
                <RoadmapMilestoneCard
                  dateDraft={dateDrafts[milestone.id]}
                  dateMoving={dateMoving}
                  dragging={draggedId === milestone.id}
                  key={milestone.id}
                  milestone={milestone}
                  onDelete={setDeleting}
                  onDragEnd={() => setDraggedId(null)}
                  onDragStart={() => setDraggedId(milestone.id)}
                  onDateDraftChange={(value) =>
                    setDateDrafts((drafts) => ({
                      ...drafts,
                      [milestone.id]: value,
                    }))
                  }
                  onDateSave={() =>
                    moveToDate(
                      milestone,
                      dateDrafts[milestone.id] ?? milestone.targetDate,
                    )
                  }
                  onDrop={() => {
                    if (!draggedId || draggedId === milestone.id) {
                      setDraggedId(null);
                      return;
                    }
                    moveDraftItem(draggedId, index);
                    setDraggedId(null);
                  }}
                  onEdit={setEditing}
                  onMove={(direction) =>
                    moveDraftItem(milestone.id, index + direction)
                  }
                  onOpen={(item) => openDetail(item.id)}
                  ordering={reordering}
                  orderIndex={index}
                  orderTotal={visibleMilestones.length}
                />
              ))}
            </section>
          )}
          {!reordering && !dateMoving && totalPages > 1 ? (
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
      <RoadmapMilestoneDetailModal
        milestoneId={detailId}
        onClose={closeDetail}
        onEdit={(milestone) => {
          closeDetail();
          setEditing(milestone);
        }}
      />
    </div>
  );
}
