import {
  AlertTriangle,
  Braces,
  Download,
  ExternalLink,
  FileText,
  Link2,
  LoaderCircle,
  Paperclip,
  Trash2,
} from "lucide-react";
import { useId } from "react";
import { ApiError } from "../api/client";
import { useTaskArtifactQuery } from "../api/hooks";
import type { AiIssueHandoffContent } from "../lib/aiIssueHandoff";
import type {
  TaskArtifactStorageKind,
  TaskArtifactSummary,
} from "../types/models";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";

const storageLabels: Record<TaskArtifactStorageKind, string> = {
  text: "文本",
  link: "链接",
  structured: "结构化",
  file: "文件",
};

const detailErrorLabels: Record<string, string> = {
  ARTIFACT_DELETED: "这项产出已经删除。",
  ARTIFACT_FILE_MISSING: "受控文件已缺失。",
  ARTIFACT_INTEGRITY_MISMATCH: "文件校验不一致。",
};

function detailErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const message = detailErrorLabels[error.code] ?? error.message;
    return error.requestId ? `${message} · 请求 ${error.requestId}` : message;
  }
  return "产出详情读取失败。";
}

function formatTime(value: string | null): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

function formatBytes(value: number | null): string | null {
  if (value === null) return null;
  if (value < 1_024) return `${value} B`;
  if (value < 1_024 * 1_024) return `${(value / 1_024).toFixed(1)} KB`;
  return `${(value / (1_024 * 1_024)).toFixed(1)} MB`;
}

export interface TaskArtifactCardProps {
  readOnly?: boolean;
  artifact: TaskArtifactSummary;
  disabled: boolean;
  downloadDisabled: boolean;
  expanded: boolean;
  downloading: boolean;
  handoffContent?: AiIssueHandoffContent | null;
  onDelete: (artifact: TaskArtifactSummary) => void;
  onDownload: (artifact: TaskArtifactSummary) => void;
  onToggle: (artifactId: string) => void;
}

export function TaskArtifactCard({
  readOnly = false,
  artifact,
  disabled,
  downloadDisabled,
  expanded,
  downloading,
  handoffContent = null,
  onDelete,
  onDownload,
  onToggle,
}: TaskArtifactCardProps) {
  const detailId = useId();
  const detailQuery = useTaskArtifactQuery(
    artifact.deletedAt ? null : artifact.id,
    expanded,
    readOnly,
  );
  const candidate =
    detailQuery.isFetching || detailQuery.isError
      ? undefined
      : detailQuery.data;
  const mismatch =
    candidate &&
    (candidate.taskId !== artifact.taskId ||
      candidate.submissionId !== artifact.submissionId);
  const detail = mismatch || candidate?.deletedAt ? undefined : candidate;
  const deleted = artifact.deletedAt !== null;
  const integrityLabel =
    artifact.integrityStatus === "missing"
      ? "文件缺失"
      : artifact.integrityStatus === "mismatch"
        ? "校验不一致"
        : artifact.integrityStatus === "verified"
          ? "已校验"
          : "未校验";
  const downloadable =
    artifact.storageKind === "file" &&
    !deleted &&
    artifact.integrityStatus !== "missing" &&
    artifact.integrityStatus !== "mismatch";
  const canDelete =
    !readOnly && !deleted && artifact.submissionStatus !== "pending_review";

  return (
    <article className={`task-artifact-card${deleted ? " is-deleted" : ""}`}>
      <div className="task-artifact-card-heading">
        <span className="task-artifact-kind">
          {artifact.storageKind === "file" ? (
            <Paperclip size={12} />
          ) : artifact.storageKind === "link" ? (
            <Link2 size={12} />
          ) : artifact.storageKind === "structured" ? (
            <Braces size={12} />
          ) : (
            <FileText size={12} />
          )}
          {storageLabels[artifact.storageKind]}
        </span>
        <div className="task-artifact-actions">
          <AiIssueHandoffButton
            content={deleted ? null : handoffContent}
            disabled={disabled}
          />
          {downloadable ? (
            <button
              aria-label={`下载产出“${artifact.name}”`}
              className="button button-quiet"
              disabled={downloadDisabled}
              onClick={() => onDownload(artifact)}
              type="button"
            >
              {downloading ? (
                <LoaderCircle className="spin" size={12} />
              ) : (
                <Download size={12} />
              )}
              下载
            </button>
          ) : null}
          <button
            aria-controls={deleted ? undefined : detailId}
            aria-expanded={expanded}
            aria-label={`${expanded ? "收起" : "查看"}产出“${artifact.name}”`}
            className="button button-quiet"
            disabled={deleted}
            onClick={() => onToggle(artifact.id)}
            type="button"
          >
            {expanded ? "收起" : "查看"}
          </button>
          {canDelete ? (
            <button
              aria-label={`删除产出“${artifact.name}”`}
              className="button button-quiet task-artifact-delete"
              disabled={disabled}
              onClick={() => onDelete(artifact)}
              type="button"
            >
              <Trash2 size={12} />
              删除
            </button>
          ) : null}
        </div>
      </div>
      <strong>{artifact.name}</strong>
      <p>
        负责人 {artifact.producedByActor.displayName} 产出 ·{" "}
        {artifact.recordedByActor.displayName} 代录 ·{" "}
        {formatTime(artifact.createdAt)}
      </p>
      <div className="task-artifact-meta">
        {artifact.storageKind === "file" ? <span>{integrityLabel}</span> : null}
        {formatBytes(artifact.sizeBytes) ? (
          <span>{formatBytes(artifact.sizeBytes)}</span>
        ) : null}
        {artifact.requiresFollowup ? <span>需要后续跟进</span> : null}
        {deleted ? <span className="is-danger">已删除</span> : null}
      </div>

      {deleted ? (
        <div className="task-artifact-deleted">
          {formatTime(artifact.deletedAt)} 由{" "}
          {artifact.deletedByActor?.displayName ?? "所有者"}
          删除 · 原因：{artifact.deleteReason}
        </div>
      ) : null}

      {expanded && !deleted ? (
        <div className="task-artifact-detail" id={detailId}>
          {detailQuery.isPending || detailQuery.isFetching ? (
            <span
              aria-live="polite"
              className="task-output-state"
              role="status"
            >
              <LoaderCircle className="spin" size={13} /> 正在读取产出详情…
            </span>
          ) : null}
          {mismatch ? (
            <p role="alert">产出与提交批次不一致，未展示其内容。</p>
          ) : candidate?.deletedAt ? (
            <p role="alert">产出已删除，请刷新批次。</p>
          ) : null}
          {detailQuery.isError ? (
            <div className="task-output-error" role="alert">
              <AlertTriangle size={13} />
              <span>{detailErrorMessage(detailQuery.error)}</span>
              <button
                className="form-inline-action"
                onClick={() => void detailQuery.refetch()}
                type="button"
              >
                重试
              </button>
            </div>
          ) : null}
          {detail?.storageKind === "text" ? (
            <pre>{detail.contentText}</pre>
          ) : null}
          {detail?.storageKind === "structured" ? (
            <pre>{JSON.stringify(detail.structuredJson, null, 2)}</pre>
          ) : null}
          {detail?.storageKind === "link" && detail.referenceUrl ? (
            <a
              href={detail.referenceUrl}
              rel="noreferrer noopener"
              target="_blank"
            >
              {detail.referenceUrl}
              <ExternalLink size={11} />
            </a>
          ) : null}
          {detail?.storageKind === "file" ? (
            <dl>
              <div>
                <dt>MIME</dt>
                <dd>{detail.mimeType ?? "未知"}</dd>
              </div>
              <div>
                <dt>SHA-256</dt>
                <dd>{detail.sha256 ?? "未提供"}</dd>
              </div>
            </dl>
          ) : null}
        </div>
      ) : null}
    </article>
  );
}
