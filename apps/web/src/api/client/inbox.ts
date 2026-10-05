import { parseAgentRunFailurePayload } from "../../lib/agentRunFailureSource";
import type {
  CreateInboxItemInput,
  ForceResolveInboxItemInput,
  InboxItem,
  InboxItemAction,
  InboxItemCommandInput,
  InboxItemListParams,
  InboxItemListResult,
  InboxItemStatus,
  InboxResolutionMode,
  InboxStats,
  MarkAllInboxReadResult,
  UpdateInboxItemInput,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  hasExactKeys,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  numeric,
  positiveInteger,
  stringField,
  validCanonicalUUID,
  validInvoiceDate,
  validRFC3339Timestamp,
  versionedCommandHeaders,
} from "./core";
import { asTaskPriority } from "./tasks";

export function asInboxItemStatus(value: unknown): InboxItemStatus {
  if (
    value === "open" ||
    value === "tracking" ||
    value === "resolved" ||
    value === "dismissed"
  ) {
    return value;
  }
  return invalidResponse("收件箱状态响应无效");
}

function asInboxItemKind(value: unknown): "manual" | "reminder" | "event" {
  if (value === "manual" || value === "reminder" || value === "event") {
    return value;
  }
  return invalidResponse("收件箱类型响应无效");
}

function asInboxResolutionMode(value: unknown): InboxResolutionMode | null {
  if (value === undefined || value === null || value === "") return null;
  if (value === "manual" || value === "forced" || value === "automatic") {
    return value;
  }
  return invalidResponse("收件箱解决方式响应无效");
}

const inboxActions = new Set<InboxItemAction>([
  "read",
  "edit",
  "snooze",
  "unsnooze",
  "resolve",
  "force-resolve",
  "dismiss",
  "reopen",
]);

const systemMaintenanceDefinitions = {
  "backup:create": {
    component: "backup",
    operation: "create",
    failureCode: "backup_create_failed",
    message:
      "无法创建已验证的本地备份；现有数据没有被修改。请检查本地存储后重试。",
  },
  "backup:verify": {
    component: "backup",
    operation: "verify",
    failureCode: "backup_verify_failed",
    message:
      "无法完成已发布备份的完整性校验。现有工作区数据没有被修改。请稍后重试。",
  },
  "backup:drill": {
    component: "backup",
    operation: "drill",
    failureCode: "backup_drill_failed",
    message:
      "无法在隔离环境中完成本地备份恢复演练。现有工作区数据没有被修改。请检查备份状态后重试。",
  },
  "backup:restore": {
    component: "backup",
    operation: "restore",
    failureCode: "backup_restore_failed",
    message:
      "无法安全安排本地备份恢复。现有工作区数据没有被修改。请检查本地存储后重试。",
  },
  "database:startup": {
    component: "database",
    operation: "startup",
    failureCode: "database_startup_failed",
    message:
      "上次启动未能安全打开本地数据库。工作区没有进入就绪状态；请检查本地存储和应用日志。",
  },
  "database:migration": {
    component: "database",
    operation: "migration",
    failureCode: "database_migration_failed",
    message:
      "上次启动未能完成受保护的数据库迁移。已有数据未被新版本继续使用；请检查回滚备份和应用日志。",
  },
  "database:runtime": {
    component: "database",
    operation: "runtime",
    failureCode: "database_runtime_failed",
    message:
      "运行中的本地数据库操作失败。请检查可用磁盘空间和应用日志，并在继续重要写入前创建或校验备份。",
  },
  "storage:low_space": {
    component: "storage",
    operation: "low_space",
    failureCode: "storage_low_space",
    message:
      "本地数据或备份所在磁盘的可用空间已低于 1 GiB。请释放空间，并在继续重要写入前创建或校验备份。",
  },
  "sidecar:startup": {
    component: "sidecar",
    operation: "startup",
    failureCode: "sidecar_startup_failed",
    message: "上次本地服务启动未能进入就绪状态。请检查应用日志后重新启动。",
  },
} as const;

const invoiceDuePayloadKeys = [
  "invoice_id",
  "invoice_number",
  "client_id",
  "client_name",
  "project_id",
  "project_name",
  "amount_minor",
  "currency",
  "due_date",
  "due_state",
  "occurrence_date",
  "invoice_version",
  "projected_at",
  "lead_days",
] as const;

const automationInboxPayloadKeys = [
  "automation_rule_id",
  "automation_run_id",
  "preset_key",
  "project_id",
  "project_name",
] as const;

function validInvoiceDueStage(
  dueDate: string,
  occurrenceDate: string,
  dueState: unknown,
): boolean {
  const dayMilliseconds = 24 * 60 * 60 * 1_000;
  const dueDay = Date.parse(`${dueDate}T00:00:00.000Z`);
  const occurrenceDay = Date.parse(`${occurrenceDate}T00:00:00.000Z`);
  const daysBeforeDue = (dueDay - occurrenceDay) / dayMilliseconds;
  if (dueState === "due_soon") {
    return daysBeforeDue >= 1 && daysBeforeDue <= 3;
  }
  if (dueState === "due") return daysBeforeDue === 0;
  if (dueState === "overdue") return daysBeforeDue < 0;
  return false;
}

export function normalizeInboxItem(value: unknown): InboxItem {
  if (!isRecord(value)) return invalidResponse("收件箱条目响应格式无效");
  const id = stringField(value, "id");
  const title = stringField(value, "title");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawSummary = fieldValue(value, "summary");
  const rawPayload = fieldValue(value, "payload_json", "payloadJson");
  const rawActions = fieldValue(value, "available_actions", "availableActions");
  const kind = asInboxItemKind(value.kind);
  const sourceEntityType = fieldValue(
    value,
    "source_entity_type",
    "sourceEntityType",
  );
  const sourceEntityId = nullableString(
    fieldValue(value, "source_entity_id", "sourceEntityId"),
  );
  const sourceEventKey = nullableString(
    fieldValue(value, "source_event_key", "sourceEventKey"),
  );
  const validTaskArtifactEvent =
    sourceEntityType === "task_artifact" &&
    !!sourceEntityId &&
    sourceEventKey === `task-artifact:${sourceEntityId}:followup` &&
    isRecord(rawPayload) &&
    rawPayload.artifact_id === sourceEntityId &&
    typeof rawPayload.artifact_name === "string" &&
    typeof rawPayload.task_id === "string" &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.submission_id === "string" &&
    typeof rawPayload.submission_sequence === "number";
  const validTaskBlockedEvent =
    sourceEntityType === "task" &&
    !!sourceEntityId &&
    isRecord(rawPayload) &&
    rawPayload.task_id === sourceEntityId &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.blocked_reason === "string" &&
    typeof rawPayload.blocked_at === "string" &&
    (rawPayload.blocked_from_status === "todo" ||
      rawPayload.blocked_from_status === "in_progress" ||
      rawPayload.blocked_from_status === "waiting_review") &&
    Number.isInteger(rawPayload.block_version) &&
    (rawPayload.block_version as number) >= 2 &&
    sourceEventKey ===
      `task:${sourceEntityId}:blocked:${String(rawPayload.block_version)}`;
  const validTaskDueEvent =
    sourceEntityType === "task_due" &&
    !!sourceEntityId &&
    isRecord(rawPayload) &&
    rawPayload.task_id === sourceEntityId &&
    typeof rawPayload.task_title === "string" &&
    typeof rawPayload.due_at === "string" &&
    typeof rawPayload.projected_at === "string" &&
    (rawPayload.due_state === "due_soon" ||
      rawPayload.due_state === "overdue") &&
    rawPayload.lead_minutes === 1440 &&
    sourceEventKey === `task:${sourceEntityId}:due:${rawPayload.due_at}`;
  const rawDueAt = fieldValue(value, "due_at", "dueAt");
  const dueAt = nullableString(rawDueAt);
  const sourceDeletedAt = nullableString(
    fieldValue(value, "source_deleted_at", "sourceDeletedAt"),
  );
  const invoiceDueDate =
    isRecord(rawPayload) && typeof rawPayload.due_date === "string"
      ? rawPayload.due_date
      : "";
  const invoiceOccurrenceDate =
    isRecord(rawPayload) && typeof rawPayload.occurrence_date === "string"
      ? rawPayload.occurrence_date
      : "";
  const invoiceDueProjectId = isRecord(rawPayload)
    ? rawPayload.project_id
    : undefined;
  const invoiceDueProjectName = isRecord(rawPayload)
    ? rawPayload.project_name
    : undefined;
  const invoiceDueExpectedKey =
    isRecord(rawPayload) && typeof rawPayload.due_state === "string"
      ? rawPayload.due_state === "overdue"
        ? `invoice:${sourceEntityId}:overdue:${invoiceOccurrenceDate}`
        : `invoice:${sourceEntityId}:${rawPayload.due_state}:${invoiceDueDate}`
      : null;
  const validInvoiceDueEvent =
    sourceEntityType === "invoice_due" &&
    !!sourceEntityId &&
    !!dueAt &&
    validRFC3339Timestamp(dueAt) &&
    isRecord(rawPayload) &&
    hasExactKeys(rawPayload, invoiceDuePayloadKeys) &&
    rawPayload.invoice_id === sourceEntityId &&
    typeof rawPayload.invoice_number === "string" &&
    rawPayload.invoice_number.trim().length > 0 &&
    typeof rawPayload.client_id === "string" &&
    rawPayload.client_id.trim().length > 0 &&
    typeof rawPayload.client_name === "string" &&
    rawPayload.client_name.trim().length > 0 &&
    ((invoiceDueProjectId === null && invoiceDueProjectName === null) ||
      (typeof invoiceDueProjectId === "string" &&
        invoiceDueProjectId.trim().length > 0 &&
        typeof invoiceDueProjectName === "string" &&
        invoiceDueProjectName.trim().length > 0)) &&
    Number.isSafeInteger(rawPayload.amount_minor) &&
    (rawPayload.amount_minor as number) > 0 &&
    (rawPayload.amount_minor as number) <= 9_000_000_000_000_000 &&
    typeof rawPayload.currency === "string" &&
    /^[A-Z]{3}$/.test(rawPayload.currency) &&
    validInvoiceDate(invoiceDueDate) &&
    validInvoiceDate(invoiceOccurrenceDate) &&
    validInvoiceDueStage(
      invoiceDueDate,
      invoiceOccurrenceDate,
      rawPayload.due_state,
    ) &&
    Number.isSafeInteger(rawPayload.invoice_version) &&
    (rawPayload.invoice_version as number) >= 1 &&
    validRFC3339Timestamp(rawPayload.projected_at) &&
    rawPayload.lead_days === 3 &&
    sourceEventKey === invoiceDueExpectedKey;
  const validClientFollowupEvent =
    sourceEntityType === "client_followup" &&
    !!sourceEntityId &&
    sourceDeletedAt === null &&
    !!dueAt &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.client_followup_id === sourceEntityId &&
    typeof rawPayload.client_id === "string" &&
    rawPayload.client_id.trim().length > 0 &&
    rawPayload.scheduled_at === dueAt &&
    typeof rawPayload.timezone === "string" &&
    rawPayload.timezone.trim().length > 0 &&
    typeof rawPayload.channel === "string" &&
    rawPayload.channel.trim().length > 0 &&
    typeof sourceEventKey === "string" &&
    new RegExp(`^followup:${sourceEntityId}:due:[1-9]\\d*$`).test(
      sourceEventKey,
    );
  const validContentItemEvent =
    sourceEntityType === "content_item" &&
    !!sourceEntityId &&
    !!dueAt &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.content_item_id === sourceEntityId &&
    (rawPayload.event_type === "review_due" ||
      rawPayload.event_type === "publish_due") &&
    Number.isInteger(rawPayload.content_version) &&
    (rawPayload.content_version as number) >= 1 &&
    rawPayload.scheduled_at === dueAt &&
    typeof rawPayload.scheduled_timezone === "string" &&
    rawPayload.scheduled_timezone.trim().length > 0 &&
    sourceEventKey ===
      `content:${sourceEntityId}:${String(rawPayload.event_type)}:${String(rawPayload.content_version)}`;
  const roadmapTargetDate =
    isRecord(rawPayload) && typeof rawPayload.target_date === "string"
      ? new Date(`${rawPayload.target_date}T00:00:00Z`)
      : null;
  const validRoadmapMilestoneEvent =
    sourceEntityType === "roadmap_milestone" &&
    !!sourceEntityId &&
    !!dueAt &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 6 &&
    rawPayload.roadmap_milestone_id === sourceEntityId &&
    (rawPayload.event_type === "due" || rawPayload.event_type === "achieved") &&
    Number.isInteger(rawPayload.milestone_version) &&
    (rawPayload.milestone_version as number) >= 1 &&
    typeof rawPayload.target_date === "string" &&
    /^\d{4}-\d{2}-\d{2}$/.test(rawPayload.target_date) &&
    roadmapTargetDate !== null &&
    !Number.isNaN(roadmapTargetDate.getTime()) &&
    Number.isInteger(rawPayload.year) &&
    rawPayload.year === roadmapTargetDate.getUTCFullYear() &&
    Number.isInteger(rawPayload.quarter) &&
    rawPayload.quarter ===
      Math.floor(roadmapTargetDate.getUTCMonth() / 3) + 1 &&
    sourceEventKey ===
      `roadmap:${sourceEntityId}:${String(rawPayload.event_type)}:${String(rawPayload.milestone_version)}`;
  const validProjectCompletionEvent =
    sourceEntityType === "project_completion" &&
    !!sourceEntityId &&
    dueAt === null &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.project_id === sourceEntityId &&
    typeof rawPayload.project_name === "string" &&
    rawPayload.project_name.trim().length > 0 &&
    typeof rawPayload.completed_at === "string" &&
    rawPayload.completed_at.length > 0 &&
    Number.isInteger(rawPayload.completion_version) &&
    (rawPayload.completion_version as number) >= 2 &&
    Number.isInteger(rawPayload.incomplete_task_count) &&
    (rawPayload.incomplete_task_count as number) >= 0 &&
    sourceEventKey ===
      `project:${sourceEntityId}:completed:${String(rawPayload.completion_version)}`;
  const automationRuleId = isRecord(rawPayload)
    ? rawPayload.automation_rule_id
    : undefined;
  const automationEventKeyPrefix = validCanonicalUUID(automationRuleId)
    ? `automation:event:${automationRuleId}:`
    : null;
  const validAutomationEvent =
    sourceEntityType === "automation" &&
    validCanonicalUUID(sourceEntityId) &&
    (rawDueAt === null || validRFC3339Timestamp(rawDueAt)) &&
    sourceDeletedAt === null &&
    isRecord(rawPayload) &&
    hasExactKeys(rawPayload, automationInboxPayloadKeys) &&
    validCanonicalUUID(automationRuleId) &&
    rawPayload.automation_run_id === sourceEntityId &&
    rawPayload.preset_key === "project-completed-inbox" &&
    validCanonicalUUID(rawPayload.project_id) &&
    typeof rawPayload.project_name === "string" &&
    rawPayload.project_name.trim().length > 0 &&
    typeof sourceEventKey === "string" &&
    automationEventKeyPrefix !== null &&
    sourceEventKey.startsWith(automationEventKeyPrefix) &&
    validCanonicalUUID(sourceEventKey.slice(automationEventKeyPrefix.length));
  const agentRunFailure = parseAgentRunFailurePayload(rawPayload);
  const rawAgentSourceDeletedAt = fieldValue(
    value,
    "source_deleted_at",
    "sourceDeletedAt",
  );
  const validAgentRunFailureEvent =
    sourceEntityType === "agent_run_failed" &&
    agentRunFailure !== null &&
    sourceEntityId === agentRunFailure.agent_run_id &&
    sourceEventKey === `agent-run:${agentRunFailure.agent_run_id}:failed` &&
    (rawDueAt === null || validRFC3339Timestamp(rawDueAt)) &&
    (rawAgentSourceDeletedAt === null ||
      validRFC3339Timestamp(rawAgentSourceDeletedAt));
  const maintenanceDefinition =
    typeof sourceEntityId === "string"
      ? systemMaintenanceDefinitions[
          sourceEntityId as keyof typeof systemMaintenanceDefinitions
        ]
      : undefined;
  const validSystemMaintenanceEvent =
    sourceEntityType === "system_maintenance" &&
    !!sourceEntityId &&
    !!maintenanceDefinition &&
    dueAt === null &&
    sourceDeletedAt === null &&
    isRecord(rawPayload) &&
    Object.keys(rawPayload).length === 5 &&
    rawPayload.component === maintenanceDefinition.component &&
    rawPayload.operation === maintenanceDefinition.operation &&
    rawPayload.failure_code === maintenanceDefinition.failureCode &&
    typeof rawPayload.occurred_at === "string" &&
    rawPayload.occurred_at.length > 0 &&
    rawPayload.message === maintenanceDefinition.message &&
    typeof sourceEventKey === "string" &&
    sourceEventKey.startsWith(`system:${sourceEntityId}:`) &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      sourceEventKey.slice(`system:${sourceEntityId}:`.length),
    );
  if (
    !id ||
    !title ||
    !createdAt ||
    !updatedAt ||
    typeof rawSummary !== "string" ||
    (sourceEntityType !== "manual" &&
      sourceEntityType !== "reminder" &&
      sourceEntityType !== "task_artifact" &&
      sourceEntityType !== "task" &&
      sourceEntityType !== "task_due" &&
      sourceEntityType !== "invoice_due" &&
      sourceEntityType !== "client_followup" &&
      sourceEntityType !== "content_item" &&
      sourceEntityType !== "roadmap_milestone" &&
      sourceEntityType !== "project_completion" &&
      sourceEntityType !== "automation" &&
      sourceEntityType !== "agent_run_failed" &&
      sourceEntityType !== "system_maintenance") ||
    (kind === "manual" &&
      (sourceEntityType !== "manual" ||
        sourceEntityId !== null ||
        sourceEventKey !== null)) ||
    (kind === "reminder" &&
      (sourceEntityType !== "reminder" ||
        !sourceEntityId ||
        !sourceEventKey)) ||
    (kind === "event" &&
      !validTaskArtifactEvent &&
      !validTaskBlockedEvent &&
      !validTaskDueEvent &&
      !validInvoiceDueEvent &&
      !validClientFollowupEvent &&
      !validContentItemEvent &&
      !validRoadmapMilestoneEvent &&
      !validProjectCompletionEvent &&
      !validAutomationEvent &&
      !validAgentRunFailureEvent &&
      !validSystemMaintenanceEvent) ||
    (fieldValue(value, "resolution_policy", "resolutionPolicy") !== "manual" &&
      fieldValue(value, "resolution_policy", "resolutionPolicy") !==
        "all_required_tasks_done") ||
    (rawPayload !== undefined && !isRecord(rawPayload)) ||
    !Array.isArray(rawActions)
  ) {
    return invalidResponse("收件箱条目响应格式无效");
  }

  return {
    id,
    kind,
    title,
    summary: rawSummary,
    sourceEntityType,
    sourceEntityId,
    sourceEventKey,
    sourceDeletedAt,
    priority: asTaskPriority(value.priority),
    status: asInboxItemStatus(value.status),
    resolutionPolicy: fieldValue(
      value,
      "resolution_policy",
      "resolutionPolicy",
    ) as InboxItem["resolutionPolicy"],
    dueAt,
    readAt: nullableString(fieldValue(value, "read_at", "readAt")),
    triagedAt: nullableString(fieldValue(value, "triaged_at", "triagedAt")),
    snoozedUntil: nullableString(
      fieldValue(value, "snoozed_until", "snoozedUntil"),
    ),
    resolvedByActorId: nullableString(
      fieldValue(value, "resolved_by_actor_id", "resolvedByActorId"),
    ),
    resolvedAt: nullableString(fieldValue(value, "resolved_at", "resolvedAt")),
    resolutionReason: nullableString(
      fieldValue(value, "resolution_reason", "resolutionReason"),
    ),
    resolutionMode: asInboxResolutionMode(
      fieldValue(value, "resolution_mode", "resolutionMode"),
    ),
    dismissedByActorId: nullableString(
      fieldValue(value, "dismissed_by_actor_id", "dismissedByActorId"),
    ),
    dismissedAt: nullableString(
      fieldValue(value, "dismissed_at", "dismissedAt"),
    ),
    dismissReason: nullableString(
      fieldValue(value, "dismiss_reason", "dismissReason"),
    ),
    payloadJson: isRecord(rawPayload) ? rawPayload : {},
    version: positiveInteger(value.version, "收件箱条目版本"),
    createdAt,
    updatedAt,
    availableActions: rawActions.filter(
      (action): action is InboxItemAction =>
        typeof action === "string" &&
        inboxActions.has(action as InboxItemAction),
    ),
  };
}

export function normalizeInboxItemListResult(
  value: unknown,
): InboxItemListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("收件箱列表响应格式无效");
  }
  const items = value.data.map(normalizeInboxItem);
  const meta = value.meta;
  const snapshotAt = stringField(meta, "snapshot_at", "snapshotAt");
  const serverNow = stringField(meta, "server_now", "serverNow");
  if (!snapshotAt || !serverNow) {
    return invalidResponse("收件箱列表缺少服务端时间基准");
  }
  const result: InboxItemListResult = {
    items,
    meta: {
      page: positiveInteger(meta.page, "收件箱页码"),
      pageSize: positiveInteger(
        fieldValue(meta, "page_size", "pageSize"),
        "收件箱分页大小",
      ),
      total: nonNegativeInteger(meta.total, "收件箱总数"),
      unreadTotal: nonNegativeInteger(
        fieldValue(meta, "unread_total", "unreadTotal"),
        "收件箱未读数",
      ),
      snapshotAt,
      serverNow,
    },
  };
  if (
    result.items.length > result.meta.pageSize ||
    result.meta.total < result.items.length
  ) {
    return invalidResponse("收件箱分页响应不一致");
  }
  return result;
}

export async function getInboxItems(
  input: InboxItemListParams = {},
  signal?: AbortSignal,
): Promise<InboxItemListResult> {
  const params = new URLSearchParams({
    view: input.view ?? "inbox",
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.priority) params.set("priority", input.priority);
  if (input.risk) params.set("risk", input.risk);
  if (input.sourceEntityType) {
    params.set("source_entity_type", input.sourceEntityType);
  }
  const payload = await apiRequest<unknown>(`/api/v1/inbox-items?${params}`, {
    signal,
  });
  return normalizeInboxItemListResult(payload);
}

export async function getInboxItem(
  id: string,
  signal?: AbortSignal,
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function forceResolveInboxItem(
  input: ForceResolveInboxItemInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.id)}/force-resolve`,
    {
      method: "POST",
      headers: versionedCommandHeaders(input.expectedVersion, idempotencyKey),
      body: JSON.stringify({ confirm: input.confirm, reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  const result = normalizeInboxItem(body);
  if (
    result.id !== input.id ||
    result.status !== "resolved" ||
    result.resolutionMode !== "forced"
  ) {
    return invalidResponse("收件箱强制解决响应不一致");
  }
  return result;
}

export async function createInboxItem(
  input: CreateInboxItemInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>("/api/v1/inbox-items", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      title: input.title,
      summary: input.summary,
      priority: input.priority,
      due_at: input.dueAt,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function updateInboxItem(
  id: string,
  input: UpdateInboxItemInput,
): Promise<InboxItem> {
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.summary === undefined ? {} : { summary: input.summary }),
        ...(input.priority === undefined ? {} : { priority: input.priority }),
        ...(input.dueAt === undefined ? {} : { due_at: input.dueAt }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(body);
}

export async function executeInboxItemCommand(
  input: InboxItemCommandInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<InboxItem> {
  let body: Record<string, unknown> = {};
  if (input.action === "snooze") {
    body = { snoozed_until: input.snoozedUntil };
  } else if (input.action === "resolve" || input.action === "dismiss") {
    body = { reason: input.reason };
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/inbox-items/${encodeURIComponent(input.id)}/${input.action}`,
    {
      method: "POST",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify(body),
    },
  );
  const result =
    isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeInboxItem(result);
}

export async function markAllInboxItemsRead(
  throughCreatedAt: string,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<MarkAllInboxReadResult> {
  const payload = await apiRequest<unknown>("/api/v1/inbox-items/read-all", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({ through_created_at: throughCreatedAt }),
  });
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("收件箱全部已读响应格式无效");
  const cutoff = stringField(body, "through_created_at", "throughCreatedAt");
  if (!cutoff) return invalidResponse("收件箱全部已读响应缺少截止时间");
  return {
    throughCreatedAt: cutoff,
    markedCount: nonNegativeInteger(
      fieldValue(body, "marked_count", "markedCount"),
      "收件箱已读数量",
    ),
  };
}

export async function getInboxStats(): Promise<InboxStats> {
  const payload = await apiRequest<unknown>("/api/v1/stats/inbox");
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("收件箱统计响应格式无效");
  const serverNow = fieldValue(body, "server_now", "serverNow");
  if (typeof serverNow !== "string" || !serverNow) {
    return invalidResponse("收件箱统计响应格式无效");
  }
  const counts = {
    pending: numeric(body.pending, -1),
    unread: numeric(body.unread, -1),
    tracking: numeric(body.tracking, -1),
    blocked: numeric(body.blocked, -1),
    waitingReview: numeric(
      fieldValue(body, "waiting_review", "waitingReview"),
      -1,
    ),
  };
  if (Object.values(counts).some((count) => count < 0)) {
    return invalidResponse("收件箱统计响应格式无效");
  }
  return { serverNow, ...counts };
}
