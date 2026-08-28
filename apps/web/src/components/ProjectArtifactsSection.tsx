import { FileOutput, History, Paperclip } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useProjectArtifactsQuery } from "../api/hooks";
import { useUiStore } from "../store/ui";
import type { TaskArtifactStorageKind, TaskStatus } from "../types/models";
import { EmptyState, ErrorState, SkeletonRows } from "./feedback";

const storageLabels: Record<TaskArtifactStorageKind, string> = {
  text: "文本",
  link: "链接",
  structured: "结构化",
  file: "文件",
};

const taskStatusLabels: Record<TaskStatus, string> = {
  todo: "待办",
  in_progress: "进行中",
  blocked: "阻塞",
  waiting_review: "待验收",
  done: "已完成",
  cancelled: "已取消",
};

function formatTime(value: string): string {
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

export function ProjectArtifactsSection({ projectId }: { projectId: string }) {
  const setTaskDetailId = useUiStore((state) => state.setTaskDetailId);
  const [page, setPage] = useState(1);
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const input = useMemo(
    () => ({ page, pageSize: 6, includeDeleted }),
    [includeDeleted, page],
  );
  const query = useProjectArtifactsQuery(projectId, input);
  const items = query.data?.items ?? [];
  const total = query.data?.meta.total ?? 0;
  const totalPages = Math.max(
    1,
    Math.ceil(total / (query.data?.meta.pageSize ?? 6)),
  );

  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  return (
    <section className="project-detail-section project-artifacts-section">
      <div className="project-detail-heading">
        <div>
          <h2>项目产出</h2>
          <p>汇总项目任务的真实交付物；查看和验收仍在所属任务中完成。</p>
        </div>
        <div className="project-artifact-heading-actions">
          <label className="project-history-toggle">
            <input
              checked={includeDeleted}
              onChange={(event) => {
                setIncludeDeleted(event.target.checked);
                setPage(1);
              }}
              type="checkbox"
            />
            <History size={13} />
            删除历史
          </label>
          <span>{total} 项</span>
        </div>
      </div>

      {query.isPending ? <SkeletonRows count={3} /> : null}
      {query.isError ? (
        <ErrorState
          compact
          message="无法读取项目产出。"
          onRetry={() => void query.refetch()}
        />
      ) : null}
      {query.isSuccess && items.length === 0 ? (
        <EmptyState
          message={
            includeDeleted
              ? "项目任务还没有任何产出记录。"
              : "项目任务还没有可用产出；请在任务详情中提交交付物。"
          }
          title="项目产出为空"
        />
      ) : null}

      {items.length ? (
        <div className="project-artifact-list">
          {items.map(({ artifact, task, submissionSequence }) => (
            <article
              className={`project-artifact-row${artifact.deletedAt ? " is-deleted" : ""}`}
              key={artifact.id}
            >
              <span className="project-artifact-icon">
                {artifact.storageKind === "file" ? (
                  <Paperclip size={15} />
                ) : (
                  <FileOutput size={15} />
                )}
              </span>
              <div className="project-artifact-copy">
                <div>
                  <strong>{artifact.name}</strong>
                  <span>{storageLabels[artifact.storageKind]}</span>
                  {artifact.requiresFollowup ? <em>需要跟进</em> : null}
                  {artifact.deletedAt ? (
                    <em className="is-danger">已删除</em>
                  ) : null}
                </div>
                <p>
                  {task.title} · 第 {submissionSequence} 次提交 ·{" "}
                  {taskStatusLabels[task.status]} ·{" "}
                  {formatTime(artifact.createdAt)}
                </p>
                {artifact.deletedAt ? (
                  <small>删除原因：{artifact.deleteReason ?? "未记录"}</small>
                ) : null}
              </div>
              <button
                className="button button-secondary"
                onClick={() => setTaskDetailId(task.id)}
                type="button"
              >
                打开任务
              </button>
            </article>
          ))}
        </div>
      ) : null}

      {query.isSuccess && totalPages > 1 ? (
        <div className="pagination">
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
            onClick={() => setPage((value) => Math.min(totalPages, value + 1))}
            type="button"
          >
            下一页
          </button>
        </div>
      ) : null}
    </section>
  );
}
