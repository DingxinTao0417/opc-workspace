import type {
  CancelReminderInput,
  CreateReminderInput,
  Reminder,
  ReminderAction,
  ReminderListParams,
  ReminderListResult,
  ReminderRecurrenceType,
  ReminderStatus,
  UpdateReminderInput,
} from "../../types/models";
import {
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  positiveInteger,
  stringField,
} from "./core";
import { asTaskPriority } from "./tasks";

function asReminderStatus(value: unknown): ReminderStatus {
  if (value === "scheduled" || value === "fired" || value === "cancelled") {
    return value;
  }
  return invalidResponse("提醒状态响应无效");
}

function asReminderRecurrenceType(value: unknown): ReminderRecurrenceType {
  if (
    value === "none" ||
    value === "daily" ||
    value === "weekly" ||
    value === "weekdays" ||
    value === "monthly"
  ) {
    return value;
  }
  return invalidResponse("提醒重复规则响应无效");
}

const reminderActions = new Set<ReminderAction>(["edit", "cancel"]);

export function normalizeReminder(value: unknown): Reminder {
  if (!isRecord(value)) return invalidResponse("提醒响应格式无效");
  const id = stringField(value, "id");
  const title = stringField(value, "title");
  const summary = fieldValue(value, "summary");
  const triggerAt = stringField(value, "trigger_at", "triggerAt");
  const sourceEventKey = stringField(
    value,
    "source_event_key",
    "sourceEventKey",
  );
  const createdByActorId = stringField(
    value,
    "created_by_actor_id",
    "createdByActorId",
  );
  const seriesId = stringField(value, "series_id", "seriesId");
  const recurrenceType = asReminderRecurrenceType(
    fieldValue(value, "recurrence_type", "recurrenceType"),
  );
  const recurrenceInterval = fieldValue(
    value,
    "recurrence_interval",
    "recurrenceInterval",
  );
  const recurrenceTimezone = stringField(
    value,
    "recurrence_timezone",
    "recurrenceTimezone",
  );
  const occurrenceNumber = fieldValue(
    value,
    "occurrence_number",
    "occurrenceNumber",
  );
  const recurrenceAnchorDay = fieldValue(
    value,
    "recurrence_anchor_day",
    "recurrenceAnchorDay",
  );
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const rawActions = fieldValue(value, "available_actions", "availableActions");
  const status = asReminderStatus(value.status);
  const sourceEntityId = nullableString(
    fieldValue(value, "source_entity_id", "sourceEntityId"),
  );
  const sourceEntityType = fieldValue(
    value,
    "source_entity_type",
    "sourceEntityType",
  );
  const firedAt = nullableString(fieldValue(value, "fired_at", "firedAt"));
  const inboxItemId = nullableString(
    fieldValue(value, "inbox_item_id", "inboxItemId"),
  );
  const cancelledByActorId = nullableString(
    fieldValue(value, "cancelled_by_actor_id", "cancelledByActorId"),
  );
  const cancelledAt = nullableString(
    fieldValue(value, "cancelled_at", "cancelledAt"),
  );
  const cancelReason = nullableString(
    fieldValue(value, "cancel_reason", "cancelReason"),
  );
  if (
    !id ||
    !title ||
    typeof summary !== "string" ||
    !triggerAt ||
    !sourceEventKey ||
    !createdByActorId ||
    !seriesId ||
    !Number.isInteger(recurrenceInterval) ||
    Number(recurrenceInterval) < 1 ||
    Number(recurrenceInterval) > 365 ||
    !recurrenceTimezone ||
    !Number.isInteger(occurrenceNumber) ||
    Number(occurrenceNumber) < 1 ||
    !Number.isInteger(recurrenceAnchorDay) ||
    Number(recurrenceAnchorDay) < 1 ||
    Number(recurrenceAnchorDay) > 31 ||
    (recurrenceType === "none" &&
      (recurrenceInterval !== 1 ||
        recurrenceTimezone !== "UTC" ||
        recurrenceAnchorDay !== 1)) ||
    ((recurrenceType === "daily" ||
      recurrenceType === "weekly" ||
      recurrenceType === "weekdays") &&
      recurrenceAnchorDay !== 1) ||
    !createdAt ||
    !updatedAt ||
    (sourceEntityType !== "manual" && sourceEntityType !== "automation") ||
    (sourceEntityType === "manual" && sourceEntityId !== null) ||
    (sourceEntityType === "automation" && sourceEntityId === null) ||
    !Array.isArray(rawActions) ||
    (status === "scheduled" &&
      (firedAt !== null ||
        inboxItemId !== null ||
        cancelledByActorId !== null ||
        cancelledAt !== null ||
        cancelReason !== null)) ||
    (status === "fired" &&
      (!firedAt ||
        !inboxItemId ||
        cancelledByActorId !== null ||
        cancelledAt !== null ||
        cancelReason !== null)) ||
    (status === "cancelled" &&
      (firedAt !== null ||
        inboxItemId !== null ||
        !cancelledByActorId ||
        !cancelledAt ||
        !cancelReason))
  ) {
    return invalidResponse("提醒响应格式无效");
  }
  const actions = rawActions.filter(
    (action): action is ReminderAction =>
      typeof action === "string" &&
      reminderActions.has(action as ReminderAction),
  );
  if (
    actions.length !== rawActions.length ||
    (status === "scheduled" &&
      (actions.length !== 2 ||
        !actions.includes("edit") ||
        !actions.includes("cancel"))) ||
    (status !== "scheduled" && actions.length !== 0)
  ) {
    return invalidResponse("提醒可用操作响应不一致");
  }
  return {
    id,
    sourceEntityType,
    sourceEntityId,
    title,
    summary,
    priority: asTaskPriority(value.priority),
    triggerAt,
    status,
    sourceEventKey,
    createdByActorId,
    seriesId,
    recurrenceType,
    recurrenceInterval: Number(recurrenceInterval),
    recurrenceTimezone,
    occurrenceNumber: Number(occurrenceNumber),
    recurrenceAnchorDay: Number(recurrenceAnchorDay),
    firedAt,
    inboxItemId,
    cancelledByActorId,
    cancelledAt,
    cancelReason,
    version: positiveInteger(value.version, "提醒版本"),
    createdAt,
    updatedAt,
    availableActions: actions,
  };
}

export function normalizeReminderListResult(
  value: unknown,
): ReminderListResult {
  if (!isRecord(value) || !Array.isArray(value.data) || !isRecord(value.meta)) {
    return invalidResponse("提醒列表响应格式无效");
  }
  const items = value.data.map(normalizeReminder);
  const serverNow = stringField(value.meta, "server_now", "serverNow");
  if (!serverNow) return invalidResponse("提醒列表缺少服务端时间基准");
  const result: ReminderListResult = {
    items,
    meta: {
      page: positiveInteger(value.meta.page, "提醒页码"),
      pageSize: positiveInteger(
        fieldValue(value.meta, "page_size", "pageSize"),
        "提醒分页大小",
      ),
      total: nonNegativeInteger(value.meta.total, "提醒总数"),
      serverNow,
    },
  };
  if (
    result.items.length > result.meta.pageSize ||
    result.meta.total < result.items.length
  ) {
    return invalidResponse("提醒分页响应不一致");
  }
  return result;
}

export async function getReminders(
  input: ReminderListParams = {},
  signal?: AbortSignal,
): Promise<ReminderListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
  });
  if (input.status) params.set("status", input.status);
  if (input.q?.trim()) params.set("q", input.q.trim());
  if (input.sort) params.set("sort", input.sort);
  const payload = await apiRequest<unknown>(`/api/v1/reminders?${params}`, {
    signal,
  });
  return normalizeReminderListResult(payload);
}

export async function getReminder(
  id: string,
  signal?: AbortSignal,
): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(id)}`,
    { signal },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function createReminder(
  input: CreateReminderInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Reminder> {
  const payload = await apiRequest<unknown>("/api/v1/reminders", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      title: input.title,
      summary: input.summary,
      priority: input.priority,
      trigger_at: input.triggerAt,
      recurrence_type: input.recurrenceType,
      recurrence_interval: input.recurrenceInterval,
      recurrence_timezone: input.recurrenceTimezone,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function updateReminder(
  id: string,
  input: UpdateReminderInput,
): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: expectedVersionHeader(input.expectedVersion),
      body: JSON.stringify({
        ...(input.title === undefined ? {} : { title: input.title }),
        ...(input.summary === undefined ? {} : { summary: input.summary }),
        ...(input.priority === undefined ? {} : { priority: input.priority }),
        ...(input.triggerAt === undefined
          ? {}
          : { trigger_at: input.triggerAt }),
        ...(input.recurrenceType === undefined
          ? {}
          : { recurrence_type: input.recurrenceType }),
        ...(input.recurrenceInterval === undefined
          ? {}
          : { recurrence_interval: input.recurrenceInterval }),
        ...(input.recurrenceTimezone === undefined
          ? {}
          : { recurrence_timezone: input.recurrenceTimezone }),
      }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}

export async function cancelReminder(
  input: CancelReminderInput,
  idempotencyKey: string = crypto.randomUUID(),
): Promise<Reminder> {
  const payload = await apiRequest<unknown>(
    `/api/v1/reminders/${encodeURIComponent(input.id)}`,
    {
      method: "DELETE",
      headers: {
        ...expectedVersionHeader(input.expectedVersion),
        "Idempotency-Key": idempotencyKey,
      },
      body: JSON.stringify({ reason: input.reason }),
    },
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeReminder(body);
}
