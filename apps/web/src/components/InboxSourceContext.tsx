import {
  BellRing,
  CalendarClock,
  CheckCircle2,
  DatabaseBackup,
  ExternalLink,
  FileCheck2,
  ReceiptText,
  TriangleAlert,
  Zap,
} from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import type { InboxItem } from "../types/models";
import { aiWorkspaceHref } from "../lib/aiWorkspaceLinks";
import {
  agentRunFailureReason,
  parseAgentRunFailurePayload,
} from "../lib/agentRunFailureSource";
import { automationLocationHref } from "../lib/automationLocation";
import { taskSubmissionHref } from "../lib/taskSubmissionLocation";
import { useUiStore } from "../store/ui";
import { formatInvoiceAmount } from "./invoicePresentation";

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

interface InvoiceDueSourceSnapshot {
  invoiceId: string;
  invoiceNumber: string;
  clientId: string;
  clientName: string;
  projectId: string | null;
  projectName: string | null;
  amountMinor: number;
  currency: string;
  dueDate: string;
  dueState: "due_soon" | "due" | "overdue";
  occurrenceDate: string;
  invoiceVersion: number;
  projectedAt: string;
}

interface ClientFollowupSourceSnapshot {
  clientFollowupId: string;
  clientId: string;
  scheduledAt: string;
  timezone: string;
  channel: string;
}

interface ContentItemSourceSnapshot {
  contentItemId: string;
  eventType: "review_due" | "publish_due";
  contentVersion: number;
  scheduledAt: string;
  scheduledTimezone: string;
}

interface RoadmapMilestoneSourceSnapshot {
  roadmapMilestoneId: string;
  eventType: "due" | "achieved";
  milestoneVersion: number;
  targetDate: string;
  year: number;
  quarter: number;
}

interface ProjectCompletionSourceSnapshot {
  projectId: string;
  projectName: string;
  completedAt: string;
  completionVersion: number;
  incompleteTaskCount: number;
}

interface AutomationSourceSnapshot {
  automationRuleId: string;
  automationRunId: string;
  presetKey: "project-completed-inbox";
  projectId: string;
  projectName: string;
}

interface SystemMaintenanceSourceSnapshot {
  component: "backup" | "database" | "sidecar" | "storage";
  operation:
    | "create"
    | "verify"
    | "drill"
    | "restore"
    | "startup"
    | "migration"
    | "runtime"
    | "low_space";
  failureCode:
    | "backup_create_failed"
    | "backup_verify_failed"
    | "backup_drill_failed"
    | "backup_restore_failed"
    | "database_startup_failed"
    | "database_migration_failed"
    | "database_runtime_failed"
    | "storage_low_space"
    | "sidecar_startup_failed";
  occurredAt: string;
  message: string;
}

interface ReminderSourceSnapshot {
  reminderId: string;
  triggerAt: string;
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
  if (item.kind !== "event" || item.sourceEntityType !== "task_artifact")
    return null;
  const payload = item.payloadJson;
  const artifactId = stringValue(payload, "artifact_id");
  const artifactName = stringValue(payload, "artifact_name");
  const storageKind = stringValue(payload, "storage_kind");
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const submissionId = stringValue(payload, "submission_id");
  const submissionSequence = payload.submission_sequence;
  if (
    !canonicalUUID(artifactId) ||
    artifactId !== item.sourceEntityId ||
    item.sourceEventKey !== `task-artifact:${artifactId}:followup` ||
    !artifactName ||
    !storageKind ||
    !["text", "link", "structured", "file"].includes(storageKind) ||
    !canonicalUUID(taskId) ||
    !taskTitle ||
    !canonicalUUID(submissionId) ||
    !Number.isSafeInteger(submissionSequence) ||
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
  if (item.kind !== "event" || item.sourceEntityType !== "task") return null;
  const payload = item.payloadJson;
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const blockedReason = stringValue(payload, "blocked_reason");
  const blockedAt = stringValue(payload, "blocked_at");
  const blockedFromStatus = payload.blocked_from_status;
  const blockVersion = payload.block_version;
  if (
    !canonicalUUID(taskId) ||
    taskId !== item.sourceEntityId ||
    item.sourceEventKey !== `task:${taskId}:blocked:${String(blockVersion)}` ||
    !taskTitle ||
    !blockedReason ||
    !blockedAt ||
    (blockedFromStatus !== "todo" &&
      blockedFromStatus !== "in_progress" &&
      blockedFromStatus !== "waiting_review") ||
    !Number.isSafeInteger(blockVersion) ||
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
  if (item.kind !== "event" || item.sourceEntityType !== "task_due")
    return null;
  const payload = item.payloadJson;
  const taskId = stringValue(payload, "task_id");
  const taskTitle = stringValue(payload, "task_title");
  const dueAt = stringValue(payload, "due_at");
  const projectedAt = stringValue(payload, "projected_at");
  const dueState = payload.due_state;
  if (
    !canonicalUUID(taskId) ||
    taskId !== item.sourceEntityId ||
    item.sourceEventKey !== `task:${taskId}:due:${dueAt}` ||
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

function invoiceDueSnapshot(item: InboxItem): InvoiceDueSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "invoice_due")
    return null;
  const payload = item.payloadJson;
  const invoiceId = stringValue(payload, "invoice_id");
  const invoiceNumber = stringValue(payload, "invoice_number");
  const clientId = stringValue(payload, "client_id");
  const clientName = stringValue(payload, "client_name");
  const dueDate = stringValue(payload, "due_date");
  const occurrenceDate = stringValue(payload, "occurrence_date");
  const projectedAt = stringValue(payload, "projected_at");
  const amountMinor = payload.amount_minor;
  const currency = stringValue(payload, "currency");
  const dueState = payload.due_state;
  const invoiceVersion = payload.invoice_version;
  const projectId = payload.project_id;
  const projectName = payload.project_name;
  const validProject =
    (projectId === null && projectName === null) ||
    (typeof projectId === "string" &&
      projectId.trim().length > 0 &&
      typeof projectName === "string" &&
      projectName.trim().length > 0);
  if (
    !canonicalUUID(invoiceId) ||
    invoiceId !== item.sourceEntityId ||
    item.sourceEventKey !==
      `invoice:${invoiceId}:${String(dueState)}:${dueState === "overdue" ? occurrenceDate : dueDate}` ||
    !invoiceNumber ||
    !clientId ||
    !clientName ||
    !dueDate ||
    !occurrenceDate ||
    !projectedAt ||
    !Number.isSafeInteger(amountMinor) ||
    (amountMinor as number) <= 0 ||
    !currency ||
    !/^[A-Z]{3}$/.test(currency) ||
    (dueState !== "due_soon" && dueState !== "due" && dueState !== "overdue") ||
    !Number.isSafeInteger(invoiceVersion) ||
    (invoiceVersion as number) < 1 ||
    payload.lead_days !== 3 ||
    !validProject
  ) {
    return null;
  }
  return {
    invoiceId,
    invoiceNumber,
    clientId,
    clientName,
    projectId: projectId as string | null,
    projectName: projectName as string | null,
    amountMinor: amountMinor as number,
    currency,
    dueDate,
    dueState,
    occurrenceDate,
    invoiceVersion: invoiceVersion as number,
    projectedAt,
  };
}

function clientFollowupSnapshot(
  item: InboxItem,
): ClientFollowupSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "client_followup")
    return null;
  const payload = item.payloadJson;
  const clientFollowupId = stringValue(payload, "client_followup_id");
  const clientId = stringValue(payload, "client_id");
  const scheduledAt = stringValue(payload, "scheduled_at");
  const timezone = stringValue(payload, "timezone");
  const channel = stringValue(payload, "channel");
  if (
    !canonicalUUID(clientFollowupId) ||
    !canonicalUUID(clientId) ||
    !new RegExp(`^followup:${clientFollowupId}:due:[1-9]\\d*$`).test(
      item.sourceEventKey ?? "",
    ) ||
    !scheduledAt ||
    !timezone ||
    !channel ||
    clientFollowupId !== item.sourceEntityId ||
    scheduledAt !== item.dueAt
  ) {
    return null;
  }
  return { clientFollowupId, clientId, scheduledAt, timezone, channel };
}

function projectCompletionSnapshot(
  item: InboxItem,
): ProjectCompletionSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "project_completion")
    return null;
  const payload = item.payloadJson;
  const projectId = stringValue(payload, "project_id");
  const projectName = stringValue(payload, "project_name");
  const completedAt = stringValue(payload, "completed_at");
  const completionVersion = payload.completion_version;
  const incompleteTaskCount = payload.incomplete_task_count;
  if (
    !canonicalUUID(projectId) ||
    projectId !== item.sourceEntityId ||
    item.sourceEventKey !==
      `project:${projectId}:completed:${String(completionVersion)}` ||
    !projectName ||
    !completedAt ||
    !Number.isSafeInteger(completionVersion) ||
    (completionVersion as number) < 2 ||
    !Number.isInteger(incompleteTaskCount) ||
    (incompleteTaskCount as number) < 0
  ) {
    return null;
  }
  return {
    projectId,
    projectName,
    completedAt,
    completionVersion: completionVersion as number,
    incompleteTaskCount: incompleteTaskCount as number,
  };
}

function canonicalUUID(value: string | null): value is string {
  return Boolean(
    value &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    ),
  );
}

function validRFC3339Timestamp(value: string | null): value is string {
  if (!value) return false;
  const match = value.match(
    /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2})$/,
  );
  if (!match) return false;
  const parsedDate = new Date(`${match[1]}T00:00:00.000Z`);
  if (
    Number.isNaN(parsedDate.getTime()) ||
    parsedDate.toISOString().slice(0, 10) !== match[1]
  ) {
    return false;
  }
  const hour = Number(match[2]);
  const minute = Number(match[3]);
  const second = Number(match[4]);
  if (hour > 23 || minute > 59 || second > 59) return false;
  if (match[5] !== "Z") {
    const [offsetHour, offsetMinute] = match[5].slice(1).split(":").map(Number);
    if (offsetHour > 23 || offsetMinute > 59) return false;
  }
  return !Number.isNaN(new Date(value).getTime());
}

function reminderSnapshot(item: InboxItem): ReminderSourceSnapshot | null {
  if (
    item.kind !== "reminder" ||
    item.sourceEntityType !== "reminder" ||
    item.sourceDeletedAt !== null ||
    !canonicalUUID(item.sourceEntityId)
  ) {
    return null;
  }
  const payload = item.payloadJson;
  const reminderId = stringValue(payload, "reminder_id");
  const triggerAt = stringValue(payload, "trigger_at");
  if (
    Object.keys(payload).length !== 2 ||
    reminderId !== item.sourceEntityId ||
    !validRFC3339Timestamp(triggerAt) ||
    item.dueAt !== triggerAt ||
    item.sourceEventKey !== `reminder:${item.sourceEntityId}:due`
  ) {
    return null;
  }
  return { reminderId, triggerAt };
}

function automationSnapshot(item: InboxItem): AutomationSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "automation")
    return null;
  const payload = item.payloadJson;
  const automationRuleId = stringValue(payload, "automation_rule_id");
  const automationRunId = stringValue(payload, "automation_run_id");
  const presetKey = stringValue(payload, "preset_key");
  const projectId = stringValue(payload, "project_id");
  const projectName = stringValue(payload, "project_name");
  const sourceEventKey = item.sourceEventKey;
  const eventKeyPrefix = automationRuleId
    ? `automation:event:${automationRuleId}:`
    : null;
  if (
    Object.keys(payload).length !== 5 ||
    !canonicalUUID(automationRuleId) ||
    !canonicalUUID(automationRunId) ||
    automationRunId !== item.sourceEntityId ||
    presetKey !== "project-completed-inbox" ||
    !canonicalUUID(projectId) ||
    !projectName ||
    !eventKeyPrefix ||
    !sourceEventKey?.startsWith(eventKeyPrefix) ||
    !canonicalUUID(sourceEventKey.slice(eventKeyPrefix.length))
  ) {
    return null;
  }
  return {
    automationRuleId,
    automationRunId,
    presetKey,
    projectId,
    projectName,
  };
}

function contentItemSnapshot(
  item: InboxItem,
): ContentItemSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "content_item")
    return null;
  const payload = item.payloadJson;
  const contentItemId = stringValue(payload, "content_item_id");
  const eventType = payload.event_type;
  const contentVersion = payload.content_version;
  const scheduledAt = stringValue(payload, "scheduled_at");
  const scheduledTimezone = stringValue(payload, "scheduled_timezone");
  if (
    !canonicalUUID(contentItemId) ||
    item.sourceEventKey !==
      `content:${contentItemId}:${String(eventType)}:${String(contentVersion)}` ||
    (eventType !== "review_due" && eventType !== "publish_due") ||
    !Number.isSafeInteger(contentVersion) ||
    (contentVersion as number) < 1 ||
    !scheduledAt ||
    !scheduledTimezone ||
    contentItemId !== item.sourceEntityId ||
    scheduledAt !== item.dueAt
  ) {
    return null;
  }
  return {
    contentItemId,
    eventType,
    contentVersion: contentVersion as number,
    scheduledAt,
    scheduledTimezone,
  };
}

function roadmapMilestoneSnapshot(
  item: InboxItem,
): RoadmapMilestoneSourceSnapshot | null {
  if (item.kind !== "event" || item.sourceEntityType !== "roadmap_milestone")
    return null;
  const payload = item.payloadJson;
  const roadmapMilestoneId = stringValue(payload, "roadmap_milestone_id");
  const eventType = payload.event_type;
  const milestoneVersion = payload.milestone_version;
  const targetDate = stringValue(payload, "target_date");
  const year = payload.year;
  const quarter = payload.quarter;
  if (
    !canonicalUUID(roadmapMilestoneId) ||
    roadmapMilestoneId !== item.sourceEntityId ||
    item.sourceEventKey !==
      `roadmap:${roadmapMilestoneId}:${String(eventType)}:${String(milestoneVersion)}` ||
    (eventType !== "due" && eventType !== "achieved") ||
    !Number.isSafeInteger(milestoneVersion) ||
    (milestoneVersion as number) < 1 ||
    !targetDate ||
    !Number.isInteger(year) ||
    !Number.isInteger(quarter) ||
    (quarter as number) < 1 ||
    (quarter as number) > 4
  ) {
    return null;
  }
  return {
    roadmapMilestoneId,
    eventType,
    milestoneVersion: milestoneVersion as number,
    targetDate,
    year: year as number,
    quarter: quarter as number,
  };
}

function systemMaintenanceSnapshot(
  item: InboxItem,
): SystemMaintenanceSourceSnapshot | null {
  if (
    item.kind !== "event" ||
    item.sourceEntityType !== "system_maintenance" ||
    item.sourceDeletedAt !== null
  )
    return null;
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
    "database:runtime": "database_runtime_failed",
    "storage:low_space": "storage_low_space",
    "sidecar:startup": "sidecar_startup_failed",
  } as const;
  const key = `${String(component)}:${String(operation)}`;
  const eventPrefix = `system:${key}:`;
  if (
    Object.keys(payload).length !== 5 ||
    item.sourceEntityId !== key ||
    !item.sourceEventKey?.startsWith(eventPrefix) ||
    !canonicalUUID(item.sourceEventKey.slice(eventPrefix.length))
  )
    return null;
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

function SourceLink({
  href,
  label,
  disabled,
}: {
  href: string;
  label: string;
  disabled: boolean;
}) {
  // A disabled link must not retain an href that can bypass the pending/draft
  // guard via keyboard or a new tab. This is navigation, not a source proof.
  return disabled ? (
    <button
      className="button button-secondary"
      disabled
      type="button"
      title="请先完成当前操作或处理未保存的更改，再查看来源。"
    >
      {label}
      <ExternalLink aria-hidden="true" size={13} />
    </button>
  ) : (
    <Link className="button button-secondary" to={href}>
      {label}
      <ExternalLink aria-hidden="true" size={13} />
    </Link>
  );
}

function ReminderSourceDetails({
  source,
  returnSession,
  navigationDisabled,
}: {
  source: ReminderSourceSnapshot;
  returnSession?: string | null;
  navigationDisabled: boolean;
}) {
  const location = useLocation();
  const currentSearch = new URLSearchParams(location.search);
  const reminderSearch = new URLSearchParams();
  // Carry only native Inbox filters, never raw return/session or extra targets.
  for (const [key, allowed] of [
    ["risk", ["tracking", "blocked", "waiting_review"]],
    ["view", ["inbox", "snoozed", "archive"]],
  ] as const) {
    const value = currentSearch.get(key);
    if (
      currentSearch.getAll(key).length === 1 &&
      value &&
      allowed.some((item) => item === value)
    ) {
      reminderSearch.set(key, value);
    }
  }
  reminderSearch.set("reminders", "fired");
  reminderSearch.set("reminder", source.reminderId);
  const returnParams = new URLSearchParams(
    aiWorkspaceHref(
      `/inbox?reminder=${source.reminderId}`,
      returnSession ?? undefined,
    ).split("?")[1],
  );
  const session = returnParams.get("return_session");
  if (session) reminderSearch.set("return_session", session);
  return (
    <section aria-label="来源上下文" className="inbox-source-context">
      <div className="inbox-source-context-heading">
        <span aria-hidden="true">
          <BellRing size={15} />
        </span>
        <div>
          <strong>本地提醒</strong>
          <small>已触发并投递到收件箱</small>
        </div>
      </div>
      <dl>
        <div>
          <dt>触发时间</dt>
          <dd>{localTimestamp(source.triggerAt)}</dd>
        </div>
      </dl>
      <SourceLink
        href={`/inbox?${reminderSearch.toString()}`}
        label="查看来源提醒"
        disabled={navigationDisabled}
      />
    </section>
  );
}

export function InboxSourceContext({
  item,
  returnSession,
  navigationDisabled = false,
}: {
  item: InboxItem;
  returnSession?: string | null;
  navigationDisabled?: boolean;
}) {
  const openDataSettings = useUiStore((state) => state.setSettingsOpen);
  const sourceLink = (route: string, label: string) =>
    item.sourceDeletedAt ? null : (
      <SourceLink
        href={aiWorkspaceHref(route, returnSession ?? undefined)}
        label={label}
        disabled={navigationDisabled}
      />
    );

  if (item.sourceEntityType === "agent_run_failed") {
    const source = parseAgentRunFailurePayload(item.payloadJson);
    if (
      !source ||
      item.kind !== "event" ||
      item.sourceEntityId !== source.agent_run_id ||
      item.sourceEventKey !== `agent-run:${source.agent_run_id}:failed`
    )
      return null;
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <TriangleAlert size={15} />
          </span>
          <div>
            <strong>Agent 执行失败诊断</strong>
            <small>仅本地诊断通知</small>
          </div>
        </div>
        <p>
          以下是失败时的安全元数据，不是当前执行状态证明。查看、解决或忽略通知不会重跑
          Agent、调用模型或恢复产出登记。
        </p>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            来源执行已不可用；导入的业务数据不包含执行记录，不能从通知重新创建执行。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>失败执行 ID</dt>
            <dd>
              <code>{source.agent_run_id}</code>
            </dd>
          </div>
          <div>
            <dt>来源任务 ID</dt>
            <dd>
              <code>{source.task_id}</code>
            </dd>
          </div>
          <div>
            <dt>执行次数</dt>
            <dd>第 {source.attempt} 次</dd>
          </div>
          <div>
            <dt>安全错误码</dt>
            <dd>
              <code>{source.error_code}</code>
              {agentRunFailureReason(source.error_code) ? (
                <p>{agentRunFailureReason(source.error_code)}</p>
              ) : null}
            </dd>
          </div>
          <div>
            <dt>失败时间</dt>
            <dd>{localTimestamp(source.failed_at)}</dd>
          </div>
        </dl>
        {sourceLink(
          `/tasks/${source.task_id}?agent_run=${source.agent_run_id}`,
          "查看失败执行",
        )}
        {sourceLink(
          automationLocationHref("run", source.automation_run_id),
          "查看通知运行",
        )}
      </section>
    );
  }

  const reminderSource = reminderSnapshot(item);
  if (reminderSource) {
    return (
      <ReminderSourceDetails
        source={reminderSource}
        returnSession={returnSession}
        navigationDisabled={navigationDisabled}
      />
    );
  }

  const invoiceDueSource = invoiceDueSnapshot(item);
  if (invoiceDueSource) {
    const stage = {
      due_soon: { title: "发票临期", label: "临期（提前 3 天）" },
      due: { title: "发票到期", label: "到期" },
      overdue: { title: "发票逾期", label: "逾期" },
    }[invoiceDueSource.dueState];
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <ReceiptText size={15} />
          </span>
          <div>
            <strong>{stage.title}</strong>
            <small>本地发票到期提醒</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源发票已删除；以下到期快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>阶段</dt>
            <dd>{stage.label}</dd>
          </div>
          <div>
            <dt>发票编号</dt>
            <dd>{invoiceDueSource.invoiceNumber}</dd>
          </div>
          <div>
            <dt>客户</dt>
            <dd>{invoiceDueSource.clientName}</dd>
          </div>
          <div>
            <dt>金额</dt>
            <dd>
              {formatInvoiceAmount(
                invoiceDueSource.amountMinor,
                invoiceDueSource.currency,
              )}{" "}
              · {invoiceDueSource.currency}
            </dd>
          </div>
          <div>
            <dt>到期日期</dt>
            <dd>{invoiceDueSource.dueDate}</dd>
          </div>
          <div>
            <dt>发生日期</dt>
            <dd>{invoiceDueSource.occurrenceDate}</dd>
          </div>
          {invoiceDueSource.projectName ? (
            <div>
              <dt>所属项目</dt>
              <dd>{invoiceDueSource.projectName}</dd>
            </div>
          ) : null}
        </dl>
        {sourceLink(`/invoices/${invoiceDueSource.invoiceId}`, "查看来源发票")}
      </section>
    );
  }

  const roadmapSource = roadmapMilestoneSnapshot(item);
  if (roadmapSource) {
    const achieved = roadmapSource.eventType === "achieved";
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            {achieved ? (
              <CheckCircle2 size={15} />
            ) : (
              <CalendarClock size={15} />
            )}
          </span>
          <div>
            <strong>
              {achieved ? "路线图里程碑已达成" : "路线图里程碑到期"}
            </strong>
            <small>本地季度规划</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源里程碑已删除；以下计划快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>目标日期</dt>
            <dd>{roadmapSource.targetDate}</dd>
          </div>
          <div>
            <dt>所属季度</dt>
            <dd>
              {roadmapSource.year} Q{roadmapSource.quarter}
            </dd>
          </div>
          <div>
            <dt>计划版本</dt>
            <dd>v{roadmapSource.milestoneVersion}</dd>
          </div>
        </dl>
        {sourceLink(
          `/roadmap?milestone=${roadmapSource.roadmapMilestoneId}`,
          "查看路线图",
        )}
      </section>
    );
  }

  const contentSource = contentItemSnapshot(item);
  if (contentSource) {
    const isReview = contentSource.eventType === "review_due";
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <CalendarClock size={15} />
          </span>
          <div>
            <strong>{isReview ? "内容待审核" : "内容待发布"}</strong>
            <small>本地内容排期</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源内容已删除；以下排期快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>计划时间</dt>
            <dd>{localTimestamp(contentSource.scheduledAt)}</dd>
          </div>
          <div>
            <dt>计划时区</dt>
            <dd>{contentSource.scheduledTimezone}</dd>
          </div>
          <div>
            <dt>内容版本</dt>
            <dd>v{contentSource.contentVersion}</dd>
          </div>
        </dl>
        {sourceLink(
          `/content-calendar?item=${contentSource.contentItemId}`,
          "查看内容日历",
        )}
      </section>
    );
  }

  const automationSource = automationSnapshot(item);
  if (automationSource) {
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <Zap size={15} />
          </span>
          <div>
            <strong>项目完成自动化</strong>
            <small>本地规则生成的核对事项</small>
          </div>
        </div>
        <dl>
          <div>
            <dt>来源项目</dt>
            <dd>{automationSource.projectName}</dd>
          </div>
          <div>
            <dt>触发规则</dt>
            <dd>项目完成后核对开票</dd>
          </div>
          <div>
            <dt>处理边界</dt>
            <dd>仅创建本地核对事项，不会生成或发送发票</dd>
          </div>
        </dl>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            来源自动化运行已删除；以上历史快照继续保留。
          </p>
        ) : null}
        {sourceLink(
          automationLocationHref("run", automationSource.automationRunId),
          "查看自动化运行",
        )}
        {sourceLink(`/projects/${automationSource.projectId}`, "查看来源项目")}
      </section>
    );
  }

  const maintenanceSource = systemMaintenanceSnapshot(item);
  if (maintenanceSource) {
    const maintenanceLabels = {
      "backup:create": ["本地备份创建失败", "本地备份", "创建"],
      "backup:verify": ["本地备份校验失败", "本地备份", "校验"],
      "backup:drill": ["本地备份恢复演练失败", "本地备份", "恢复演练"],
      "backup:restore": ["本地备份恢复安排失败", "本地备份", "恢复安排"],
      "database:startup": ["本地数据库启动失败", "本地数据库", "启动"],
      "database:migration": ["本地数据库迁移失败", "本地数据库", "迁移"],
      "database:runtime": ["本地数据库运行失败", "本地数据库", "运行"],
      "storage:low_space": ["本地存储空间不足", "本地存储", "容量检查"],
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
            disabled={navigationDisabled}
            onClick={() => openDataSettings(true, "data")}
            type="button"
          >
            打开数据与备份
          </button>
        ) : null}
      </section>
    );
  }

  const projectSource = projectCompletionSnapshot(item);
  if (projectSource) {
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <CheckCircle2 size={15} />
          </span>
          <div>
            <strong>项目完成</strong>
            <small>{localTimestamp(projectSource.completedAt)}</small>
          </div>
        </div>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            <TriangleAlert aria-hidden="true" size={14} />
            来源项目已删除；以下完成快照继续保留用于解释这项工作。
          </p>
        ) : null}
        <dl>
          <div>
            <dt>来源项目</dt>
            <dd>{projectSource.projectName}</dd>
          </div>
          <div>
            <dt>完成时间</dt>
            <dd>{localTimestamp(projectSource.completedAt)}</dd>
          </div>
          <div>
            <dt>完成时未结任务</dt>
            <dd>{projectSource.incompleteTaskCount} 项</dd>
          </div>
        </dl>
        {sourceLink(`/projects/${projectSource.projectId}`, "查看来源项目")}
      </section>
    );
  }

  const followupSource = clientFollowupSnapshot(item);
  if (followupSource) {
    return (
      <section aria-label="来源上下文" className="inbox-source-context">
        <div className="inbox-source-context-heading">
          <span aria-hidden="true">
            <CalendarClock size={15} />
          </span>
          <div>
            <strong>客户回访到期</strong>
            <small>本地计划提醒</small>
          </div>
        </div>
        <dl>
          <div>
            <dt>计划时间</dt>
            <dd>{localTimestamp(followupSource.scheduledAt)}</dd>
          </div>
          <div>
            <dt>时区</dt>
            <dd>{followupSource.timezone}</dd>
          </div>
          <div>
            <dt>渠道</dt>
            <dd>{followupSource.channel}</dd>
          </div>
        </dl>
        {item.sourceDeletedAt ? (
          <p className="inbox-source-missing" role="status">
            来源回访已删除；以上计划快照继续保留。
          </p>
        ) : null}
        {sourceLink(
          `/clients/${followupSource.clientId}?followup=${followupSource.clientFollowupId}`,
          "查看客户回访",
        )}
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
        {sourceLink(`/tasks/${dueSource.taskId}`, "查看来源任务")}
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
        {sourceLink(`/tasks/${blockedSource.taskId}`, "查看来源任务")}
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
      <p className="muted">
        按历史快照定位提交批次，当前记录以详情读取结果为准。
      </p>
      {sourceLink(
        taskSubmissionHref(source.taskId, source.submissionId),
        "查看来源提交",
      )}
    </section>
  );
}
