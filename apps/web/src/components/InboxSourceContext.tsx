import {
  CalendarClock,
  DatabaseBackup,
  ExternalLink,
  FileCheck2,
  TriangleAlert,
} from "lucide-react";
import { Link } from "react-router-dom";
import type { InboxItem } from "../types/models";
import { useUiStore } from "../store/ui";

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

interface TaskDueSourceSnapshot {
  taskId: string;
  taskTitle: string;
  dueAt: string;
  projectedAt: string;
  dueState: "due_soon" | "overdue";
  projectId: string | null;
  projectName: string | null;
}

interface SystemMaintenanceSourceSnapshot {
  component: "backup" | "database" | "sidecar";
  operation:
    "create" | "verify" | "drill" | "restore" | "startup" | "migration";
  failureCode:
    | "backup_create_failed"
    | "backup_verify_failed"
    | "backup_drill_failed"
    | "backup_restore_failed"
    | "database_startup_failed"
    | "database_migration_failed"
    | "sidecar_startup_failed";
  occurredAt: string;
  message: string;
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

function taskDueSnapshot(item: InboxItem): TaskDueSourceSnapshot | null {
  if (item.sourceEntityType !== "task_due") return null;
  const payload = item.payloadJson;
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const dueAt = stringValue(payload, "due_at");
  const projectedAt = stringValue(payload, "projected_at");
  const dueState = payload.due_state;
  if (
    !taskId ||
    !taskTitle ||
    !dueAt ||
    !projectedAt ||
    (dueState !== "due_soon" && dueState !== "overdue") ||
    payload.lead_minutes !== 1440
  ) {
    return null;
  }
  return {
    taskId,
    taskTitle,
    dueAt,
    projectedAt,
    dueState,
    projectId: stringValue(payload, "project_id"),
    projectName: stringValue(payload, "project_name"),
  };
}

function systemMaintenanceSnapshot(
  item: InboxItem,
): SystemMaintenanceSourceSnapshot | null {
  if (item.sourceEntityType !== "system_maintenance") return null;
  const payload = item.payloadJson;
  const component = payload.component;
  const operation = payload.operation;
  const failureCode = payload.failure_code;
  const occurredAt = stringValue(payload, "occurred_at");
  const message = stringValue(payload, "message");
  const definitions = {
    "backup:create": "backup_create_failed",
    "backup:verify": "backup_verify_failed",
    "backup:drill": "backup_drill_failed",
    "backup:restore": "backup_restore_failed",
    "database:startup": "database_startup_failed",
    "database:migration": "database_migration_failed",
    "sidecar:startup": "sidecar_startup_failed",
  } as const;
  const key = `${String(component)}:${String(operation)}`;
  const expectedFailureCode = definitions[key as keyof typeof definitions];
  if (!expectedFailureCode || failureCode !== expectedFailureCode) return null;
  if (!occurredAt || !message) return null;
  return {
    component: component as SystemMaintenanceSourceSnapshot["component"],
    operation: operation as SystemMaintenanceSourceSnapshot["operation"],
    failureCode: expectedFailureCode,
    occurredAt,
    message,
  };
}

function localTimestamp(value: string): string {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? value
    : parsed.toLocaleString("zh-CN", { hour12: false });
}

const storageKindLabels: Record<string, string> = {
  text: "文本",
  link: "链接",
  structured: "结构化数据",
  file: "文件",
};

export function InboxSourceContext({ item }: { item: InboxItem }) {
  const openDataSettings = useUiStore((state) => state.setSettingsOpen);
  const maintenanceSource = systemMaintenanceSnapshot(item);
  if (maintenanceSource) {
    const maintenanceLabels = {
      "backup:create": ["本地备份创建失败", "本地备份", "创建"],
      "backup:verify": ["本地备份校验失败", "本地备份", "校验"],
      "backup:drill": ["本地备份恢复演练失败", "本地备份", "恢复演练"],
      "backup:restore": ["本地备份恢复安排失败", "本地备份", "恢复安排"],
      "database:startup": ["本地数据库启动失败", "本地数据库", "启动"],
      "database:migration": ["本地数据库迁移失败", "本地数据库", "迁移"],
      "sidecar:startup": ["本地服务启动失败", "本地服务", "启动"],
    } as const;
    const [summaryLabel, componentLabel, operationLabel] =
      maintenanceLabels[
        `${maintenanceSource.component}:${maintenanceSource.operation}` as keyof typeof maintenanceLabels
      ];
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <DatabaseBackup size={15} />
          </span>
          <div>
            <strong>系统维护</strong>
            <small>{summaryLabel}</small>
          </div>
        </div>
        <dl>
          <div>
            <dt>组件</dt>
            <dd>{componentLabel}</dd>
          </div>
          <div>
            <dt>操作</dt>
            <dd>{operationLabel}</dd>
          </div>
          <div>
            <dt>发生时间</dt>
            <dd>{localTimestamp(maintenanceSource.occurredAt)}</dd>
          </div>
          <div>
            <dt>说明</dt>
            <dd>{maintenanceSource.message}</dd>
          </div>
        </dl>
        {maintenanceSource.component !== "sidecar" ? (
          <button
            className="button button-secondary"
            onClick={() => openDataSettings(true, "data")}
            type="button"
          >
            打开数据与备份
          </button>
        ) : null}
      </section>
    );
  }

  const dueSource = taskDueSnapshot(item);
  if (dueSource) {
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <CalendarClock size={15} />
          </span>
          <div>
            <strong>
              {dueSource.dueState === "overdue" ? "任务逾期" : "任务临期"}
            </strong>
            <small>提前 24 小时进入收件箱</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源任务已删除；以下截止时间快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>来源任务</dt>
            <dd>{dueSource.taskTitle}</dd>
          </div>
          <div>
            <dt>截止时间</dt>
            <dd>{localTimestamp(dueSource.dueAt)}</dd>
          </div>
          <div>
            <dt>进入收件箱</dt>
            <dd>{localTimestamp(dueSource.projectedAt)}</dd>
          </div>
          {dueSource.projectName ? (
            <div>
              <dt>所属项目</dt>
              <dd>{dueSource.projectName}</dd>
            </div>
          ) : null}
        </dl>
        {item.sourceDeletedAt ? null : (
          <Link
            className="button button-secondary"
            to={`/tasks/${dueSource.taskId}`}
          >
            查看来源任务
            <ExternalLink aria-hidden="true" size={13} />
          </Link>
        )}
      </section>
    );
  }

  const blockedSource = taskBlockedSnapshot(item);
  if (blockedSource) {
    const statusLabels = {
      todo: "待办",
      in_progress: "进行中",
      waiting_review: "待验收",
    } as const;
    const blockedTime = localTimestamp(blockedSource.blockedAt);
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
