import { ExternalLink, FileCheck2, TriangleAlert } from "lucide-react";
import { Link } from "react-router-dom";
import type { InboxItem } from "../types/models";

interface TaskArtifactSourceSnapshot {
  artifactId: string;
  artifactName: string;
  storageKind: string;
  taskId: string;
  taskTitle: string;
  submissionId: string;
  submissionSequence: number;
  projectId: string | null;
  projectName: string | null;
}

interface TaskBlockedSourceSnapshot {
  taskId: string;
  taskTitle: string;
  blockedReason: string;
  blockedAt: string;
  blockedFromStatus: "todo" | "in_progress" | "waiting_review";
  blockVersion: number;
  projectId: string | null;
  projectName: string | null;
}

function stringValue(
  value: Record<string, unknown>,
  key: string,
): string | null {
  const candidate = value[key];
  return typeof candidate === "string" && candidate.trim() ? candidate : null;
}

function taskArtifactSnapshot(
  item: InboxItem,
): TaskArtifactSourceSnapshot | null {
  if (item.sourceEntityType !== "task_artifact") return null;
  const payload = item.payloadJson;
  const artifactId = stringValue(payload, "artifact_id");
  const artifactName = stringValue(payload, "artifact_name");
  const storageKind = stringValue(payload, "storage_kind");
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const submissionId = stringValue(payload, "submission_id");
  const submissionSequence = payload.submission_sequence;
  if (
    !artifactId ||
    !artifactName ||
    !storageKind ||
    !taskId ||
    !taskTitle ||
    !submissionId ||
    !Number.isInteger(submissionSequence) ||
    (submissionSequence as number) < 1
  ) {
    return null;
  }
  return {
    artifactId,
    artifactName,
    storageKind,
    taskId,
    taskTitle,
    submissionId,
    submissionSequence: submissionSequence as number,
    projectId: stringValue(payload, "project_id"),
    projectName: stringValue(payload, "project_name"),
  };
}

function taskBlockedSnapshot(
  item: InboxItem,
): TaskBlockedSourceSnapshot | null {
  if (item.sourceEntityType !== "task") return null;
  const payload = item.payloadJson;
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const blockedReason = stringValue(payload, "blocked_reason");
  const blockedAt = stringValue(payload, "blocked_at");
  const blockedFromStatus = payload.blocked_from_status;
  const blockVersion = payload.block_version;
  if (
    !taskId ||
    !taskTitle ||
    !blockedReason ||
    !blockedAt ||
    (blockedFromStatus !== "todo" &&
      blockedFromStatus !== "in_progress" &&
      blockedFromStatus !== "waiting_review") ||
    !Number.isInteger(blockVersion) ||
    (blockVersion as number) < 2
  ) {
    return null;
  }
  return {
    taskId,
    taskTitle,
    blockedReason,
    blockedAt,
    blockedFromStatus,
    blockVersion: blockVersion as number,
    projectId: stringValue(payload, "project_id"),
    projectName: stringValue(payload, "project_name"),
  };
}

const storageKindLabels: Record<string, string> = {
  text: "文本",
  link: "链接",
  structured: "结构化数据",
  file: "文件",
};

export function InboxSourceContext({ item }: { item: InboxItem }) {
  const blockedSource = taskBlockedSnapshot(item);
  if (blockedSource) {
    const statusLabels = {
      todo: "待办",
      in_progress: "进行中",
      waiting_review: "待验收",
    } as const;
    const blockedDate = new Date(blockedSource.blockedAt);
    const blockedTime = Number.isNaN(blockedDate.getTime())
      ? blockedSource.blockedAt
      : blockedDate.toLocaleString("zh-CN", { hour12: false });
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <TriangleAlert size={15} />
          </span>
          <div>
            <strong>任务阻塞</strong>
            <small>{blockedTime}</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源任务已删除；以下快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>来源任务</dt>
            <dd>{blockedSource.taskTitle}</dd>
          </div>
          <div>
            <dt>阻塞原因</dt>
            <dd>{blockedSource.blockedReason}</dd>
          </div>
          <div>
            <dt>阻塞前状态</dt>
            <dd>{statusLabels[blockedSource.blockedFromStatus]}</dd>
          </div>
          {blockedSource.projectName ? (
            <div>
              <dt>所属项目</dt>
              <dd>{blockedSource.projectName}</dd>
            </div>
          ) : null}
        </dl>
        {item.sourceDeletedAt ? null : (
          <Link
            className="button button-secondary"
            to={`/tasks/${blockedSource.taskId}`}
          >
            查看来源任务
            <ExternalLink aria-hidden="true" size={13} />
          </Link>
        )}
      </section>
    );
  }

  const source = taskArtifactSnapshot(item);
  if (!source) return null;

  return (
    <section aria-label="来源上下文" className="inbox-source-context">
      <div className="inbox-source-context-heading">
        <span aria-hidden="true">
          <FileCheck2 size={15} />
        </span>
        <div>
          <strong>任务产出</strong>
          <small>第 {source.submissionSequence} 批提交</small>
        </div>
      </div>
      {item.sourceDeletedAt ? (
        <p className="inbox-source-missing" role="status">
          <TriangleAlert aria-hidden="true" size={14} />
          来源产出已删除；以下快照继续保留用于解释这项工作。
        </p>
      ) : null}
      <dl>
        <div>
          <dt>产出</dt>
          <dd>{source.artifactName}</dd>
        </div>
        <div>
          <dt>类型</dt>
          <dd>{storageKindLabels[source.storageKind] ?? source.storageKind}</dd>
        </div>
        <div>
          <dt>来源任务</dt>
          <dd>{source.taskTitle}</dd>
        </div>
        {source.projectName ? (
          <div>
            <dt>所属项目</dt>
            <dd>{source.projectName}</dd>
          </div>
        ) : null}
      </dl>
      {item.sourceDeletedAt ? null : (
        <Link
          className="button button-secondary"
          to={`/tasks/${source.taskId}`}
        >
          查看来源任务
          <ExternalLink aria-hidden="true" size={13} />
        </Link>
      )}
    </section>
  );
}
