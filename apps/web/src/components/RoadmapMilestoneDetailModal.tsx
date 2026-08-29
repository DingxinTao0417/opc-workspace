import { CalendarDays, Edit3, FolderKanban } from "lucide-react";
import { Link } from "react-router-dom";
import { useRoadmapMilestoneQuery } from "../api/hooks";
import type { RoadmapMilestone, RoadmapMilestoneStatus } from "../types/models";
import { ErrorState, SkeletonRows } from "./feedback";
import { Modal } from "./Modal";

const statusLabels: Record<RoadmapMilestoneStatus, string> = {
  planned: "计划中",
  active: "进行中",
  achieved: "已达成",
  archived: "已归档",
};

function statusClass(status: RoadmapMilestoneStatus) {
  if (status === "active") return "status-purple";
  if (status === "achieved") return "status-green";
  if (status === "archived") return "status-neutral";
  return "status-blue";
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "未知";
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function RoadmapMilestoneDetailModal({
  milestoneId,
  onClose,
  onEdit,
}: {
  milestoneId: string | null;
  onClose: () => void;
  onEdit: (milestone: RoadmapMilestone) => void;
}) {
  const query = useRoadmapMilestoneQuery(milestoneId);
  const milestone = query.data;

  return (
    <Modal
      footer={
        <>
          <button
            className="button button-secondary"
            onClick={onClose}
            type="button"
          >
            关闭
          </button>
          {milestone && milestone.status !== "archived" ? (
            <button
              className="button button-primary"
              onClick={() => onEdit(milestone)}
              type="button"
            >
              <Edit3 size={14} />
              编辑里程碑
            </button>
          ) : null}
        </>
      }
      onClose={onClose}
      open={Boolean(milestoneId)}
      title="里程碑详情"
      width="620px"
    >
      {query.isPending ? <SkeletonRows count={4} /> : null}
      {query.isError ? (
        <ErrorState
          message="无法读取里程碑详情，请确认本地服务已连接。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {milestone ? (
        <article className="roadmap-detail">
          <header className="roadmap-detail-heading">
            <div>
              <span className={`status-badge ${statusClass(milestone.status)}`}>
                {statusLabels[milestone.status]}
              </span>
              <h2>{milestone.title}</h2>
            </div>
            <span className="roadmap-date">
              <CalendarDays size={14} />
              {milestone.targetDate}
            </span>
          </header>
          <p className="roadmap-detail-description">
            {milestone.description || "尚未填写里程碑说明。"}
          </p>
          <section
            aria-label="关联任务进度"
            className="roadmap-detail-progress"
          >
            <div className="roadmap-detail-progress-heading">
              <strong>关联任务进度</strong>
              <span>{milestone.taskSummary.progressPercent}%</span>
            </div>
            <div
              aria-label={`${milestone.title}详情任务完成进度`}
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
            <p>
              {milestone.taskSummary.completed} 已完成 ·{" "}
              {milestone.taskSummary.inProgress} 进行中 ·{" "}
              {milestone.taskSummary.total} 总计
            </p>
          </section>
          <dl className="roadmap-detail-facts">
            <div>
              <dt>规划期间</dt>
              <dd>
                {milestone.year} 年 Q{milestone.quarter}
              </dd>
            </div>
            <div>
              <dt>数据版本</dt>
              <dd>v{milestone.version}</dd>
            </div>
            <div>
              <dt>创建时间</dt>
              <dd>{formatDateTime(milestone.createdAt)}</dd>
            </div>
            <div>
              <dt>更新时间</dt>
              <dd>{formatDateTime(milestone.updatedAt)}</dd>
            </div>
          </dl>
          <section className="roadmap-detail-projects">
            <h3>关联项目 · {milestone.projects.length}</h3>
            {milestone.projects.length > 0 ? (
              <div className="roadmap-project-links">
                {milestone.projects.map((project) => (
                  <Link key={project.id} to={`/projects/${project.id}`}>
                    <FolderKanban size={13} />
                    {project.name}
                  </Link>
                ))}
              </div>
            ) : (
              <p>尚未关联项目。</p>
            )}
          </section>
          {milestone.status === "archived" ? (
            <p className="form-field-hint">
              已归档里程碑需要先恢复，才能继续编辑。
            </p>
          ) : null}
        </article>
      ) : null}
    </Modal>
  );
}
