import type {
  CreateFocusSessionInput,
  FocusRecoveryAction,
  FocusReport,
  FocusReportParams,
  FocusSession,
  FocusSessionListParams,
  FocusSessionListResult,
  FocusSessionSnapshot,
  TodayStats,
} from "../../types/models";
import {
  ApiError,
  apiRequest,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  nullableString,
  numeric,
  positiveInteger,
  stringField,
} from "./core";

function asFocusSessionStatus(value: unknown): FocusSession["status"] {
  if (
    value === "planned" ||
    value === "active" ||
    value === "paused" ||
    value === "recovery_pending" ||
    value === "completed" ||
    value === "cancelled" ||
    value === "interrupted"
  ) {
    return value;
  }
  return invalidResponse("专注会话状态响应无效");
}

function asFocusSessionEndReason(value: unknown): FocusSession["endReason"] {
  if (value === undefined || value === null || value === "") return null;
  if (
    value === "user_stop" ||
    value === "completed" ||
    value === "cancelled" ||
    value === "crash_recovery"
  ) {
    return value;
  }
  return invalidResponse("专注会话结束原因响应无效");
}

export function normalizeFocusSession(value: unknown): FocusSession {
  if (!isRecord(value)) return invalidResponse("专注会话响应格式无效");
  const id = stringField(value, "id");
  const startedAt = stringField(value, "started_at", "startedAt");
  const createdAt = stringField(value, "created_at", "createdAt");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  if (!id || !startedAt || !createdAt || !updatedAt) {
    return invalidResponse("专注会话响应格式无效");
  }

  return {
    id,
    taskId: nullableString(fieldValue(value, "task_id", "taskId")),
    taskTitle: nullableString(fieldValue(value, "task_title", "taskTitle")),
    status: asFocusSessionStatus(value.status),
    plannedSeconds: positiveInteger(
      fieldValue(value, "planned_seconds", "plannedSeconds"),
      "专注计划秒数",
    ),
    accumulatedSeconds: nonNegativeInteger(
      fieldValue(value, "accumulated_seconds", "accumulatedSeconds"),
      "专注累计秒数",
    ),
    startedAt,
    endedAt: nullableString(fieldValue(value, "ended_at", "endedAt")),
    lastResumedAt: nullableString(
      fieldValue(value, "last_resumed_at", "lastResumedAt"),
    ),
    lastHeartbeatAt: nullableString(
      fieldValue(value, "last_heartbeat_at", "lastHeartbeatAt"),
    ),
    endReason: asFocusSessionEndReason(
      fieldValue(value, "end_reason", "endReason"),
    ),
    version: positiveInteger(value.version, "专注会话版本"),
    createdAt,
    updatedAt,
  };
}

export function normalizeFocusSessionSnapshot(
  payload: unknown,
): FocusSessionSnapshot {
  const root = isRecord(payload) ? payload : null;
  const data = root && "data" in root ? root.data : payload;
  const envelope = isRecord(data) ? data : null;
  const wrapped = Boolean(
    envelope && ("session" in envelope || !("id" in envelope)),
  );
  const rawSession = wrapped ? envelope?.session : data;
  const rawServerNow =
    fieldValue(envelope ?? {}, "server_now", "serverNow") ??
    fieldValue(root ?? {}, "server_now", "serverNow");
  if (typeof rawServerNow !== "string" || rawServerNow.length === 0) {
    return invalidResponse("专注会话缺少服务端时间基准");
  }
  if (
    rawSession !== null &&
    rawSession !== undefined &&
    !isRecord(rawSession)
  ) {
    return invalidResponse("专注会话响应格式无效");
  }

  return {
    session:
      rawSession === null || rawSession === undefined
        ? null
        : normalizeFocusSession(rawSession),
    serverNow: rawServerNow,
    receivedAtMs: Date.now(),
  };
}

export function normalizeFocusSessionListResult(
  payload: unknown,
): FocusSessionListResult {
  if (
    !isRecord(payload) ||
    !Array.isArray(payload.data) ||
    !isRecord(payload.meta)
  ) {
    return invalidResponse("专注历史响应格式无效");
  }
  const items = payload.data.map(normalizeFocusSession);
  if (
    items.some(
      (item) =>
        (item.status !== "completed" &&
          item.status !== "cancelled" &&
          item.status !== "interrupted") ||
        item.endedAt === null,
    )
  ) {
    return invalidResponse("专注历史响应格式无效");
  }
  return {
    items,
    meta: {
      page: positiveInteger(payload.meta.page, "专注历史页码"),
      pageSize: positiveInteger(
        fieldValue(payload.meta, "page_size", "pageSize"),
        "专注历史每页数量",
      ),
      total: nonNegativeInteger(payload.meta.total, "专注历史总数"),
    },
  };
}

export function normalizeFocusReport(payload: unknown): FocusReport {
  const body =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (
    !isRecord(body) ||
    !isRecord(body.totals) ||
    !Array.isArray(body.days) ||
    !Array.isArray(body.projects) ||
    !Array.isArray(body.hours) ||
    !Array.isArray(body.heatmap) ||
    !Array.isArray(body.tags)
  ) {
    return invalidResponse("专注周期统计响应格式无效");
  }
  const dateFrom = stringField(body, "date_from", "dateFrom");
  const dateTo = stringField(body, "date_to", "dateTo");
  const timezone = stringField(body, "timezone");
  if (!dateFrom || !dateTo || !timezone) {
    return invalidResponse("专注周期统计响应格式无效");
  }
  const hours = body.hours.map((item) => {
    if (!isRecord(item)) return invalidResponse("专注小时统计响应格式无效");
    return {
      hour: nonNegativeInteger(item.hour, "专注小时"),
      sessions: nonNegativeInteger(item.sessions, "小时专注块数"),
      seconds: nonNegativeInteger(item.seconds, "小时专注秒数"),
      minutes: nonNegativeInteger(item.minutes, "小时专注分钟数"),
    };
  });
  if (hours.length !== 24 || hours.some((item, index) => item.hour !== index)) {
    return invalidResponse("专注小时统计响应格式无效");
  }
  const heatmap = body.heatmap.map((item) => {
    if (!isRecord(item)) return invalidResponse("专注热力图响应格式无效");
    return {
      weekday: positiveInteger(item.weekday, "专注热力图星期"),
      hour: nonNegativeInteger(item.hour, "专注热力图小时"),
      sessions: nonNegativeInteger(item.sessions, "热力图专注块数"),
      seconds: nonNegativeInteger(item.seconds, "热力图专注秒数"),
      minutes: nonNegativeInteger(item.minutes, "热力图专注分钟数"),
    };
  });
  if (
    heatmap.length !== 7 * 24 ||
    heatmap.some(
      (item, index) =>
        item.weekday !== Math.floor(index / 24) + 1 || item.hour !== index % 24,
    )
  ) {
    return invalidResponse("专注热力图响应格式无效");
  }
  return {
    dateFrom,
    dateTo,
    timezone,
    totals: {
      sessions: nonNegativeInteger(body.totals.sessions, "专注总块数"),
      seconds: nonNegativeInteger(body.totals.seconds, "专注总秒数"),
      minutes: nonNegativeInteger(body.totals.minutes, "专注总分钟数"),
    },
    days: body.days.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注每日统计响应格式无效");
      const date = stringField(item, "date");
      if (!date) return invalidResponse("专注每日统计响应格式无效");
      return {
        date,
        sessions: nonNegativeInteger(item.sessions, "每日专注块数"),
        seconds: nonNegativeInteger(item.seconds, "每日专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "每日专注分钟数"),
      };
    }),
    projects: body.projects.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注项目统计响应格式无效");
      const projectId = nullableString(
        fieldValue(item, "project_id", "projectId"),
      );
      const projectName = nullableString(
        fieldValue(item, "project_name", "projectName"),
      );
      if ((projectId === null) !== (projectName === null)) {
        return invalidResponse("专注项目统计响应格式无效");
      }
      return {
        projectId,
        projectName,
        sessions: nonNegativeInteger(item.sessions, "项目专注块数"),
        seconds: nonNegativeInteger(item.seconds, "项目专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "项目专注分钟数"),
      };
    }),
    hours,
    heatmap,
    tags: body.tags.map((item) => {
      if (!isRecord(item)) return invalidResponse("专注标签统计响应格式无效");
      const tagId = nullableString(fieldValue(item, "tag_id", "tagId"));
      const tagName = nullableString(fieldValue(item, "tag_name", "tagName"));
      const tagColor = nullableString(
        fieldValue(item, "tag_color", "tagColor"),
      );
      if (
        (tagId === null) !== (tagName === null) ||
        (tagId === null) !== (tagColor === null) ||
        (tagColor !== null && !/^#[0-9A-F]{6}$/.test(tagColor))
      ) {
        return invalidResponse("专注标签统计响应格式无效");
      }
      return {
        tagId,
        tagName,
        tagColor,
        sessions: nonNegativeInteger(item.sessions, "标签专注块数"),
        seconds: nonNegativeInteger(item.seconds, "标签专注秒数"),
        minutes: nonNegativeInteger(item.minutes, "标签专注分钟数"),
      };
    }),
    currentStreakDays: nonNegativeInteger(
      fieldValue(body, "current_streak_days", "currentStreakDays"),
      "当前连续专注天数",
    ),
    longestStreakDays: nonNegativeInteger(
      fieldValue(body, "longest_streak_days", "longestStreakDays"),
      "最长连续专注天数",
    ),
  };
}

export async function getActiveFocusSession(
  signal?: AbortSignal,
): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>("/api/v1/focus-sessions/active", {
    signal,
  });
  return normalizeFocusSessionSnapshot(payload);
}

export async function getFocusSessions(
  input: FocusSessionListParams = {},
): Promise<FocusSessionListResult> {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 20),
    status: input.status ?? "terminal",
  });
  if (input.taskId) params.set("task_id", input.taskId);
  if (input.projectId) params.set("project_id", input.projectId);
  const payload = await apiRequest<unknown>(`/api/v1/focus-sessions?${params}`);
  return normalizeFocusSessionListResult(payload);
}

export async function getFocusReport(
  input: FocusReportParams,
): Promise<FocusReport> {
  const params = new URLSearchParams({
    date_from: input.dateFrom,
    date_to: input.dateTo,
    timezone: input.timezone,
  });
  if (input.projectId) params.set("project_id", input.projectId);
  const payload = await apiRequest<unknown>(`/api/v1/stats/focus?${params}`);
  return normalizeFocusReport(payload);
}

export async function createFocusSession(
  input: CreateFocusSessionInput,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>("/api/v1/focus-sessions", {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey },
    body: JSON.stringify({
      task_id: input.taskId,
      planned_seconds: input.plannedSeconds,
    }),
  });
  return normalizeFocusSessionSnapshot(payload);
}

async function executeFocusSessionCommand(
  id: string,
  command: "pause" | "resume" | "stop" | "cancel",
  expectedVersion: number,
  idempotencyKey?: string,
): Promise<FocusSessionSnapshot> {
  const headers = {
    ...expectedVersionHeader(expectedVersion),
    ...(idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {}),
  };
  const payload = await apiRequest<unknown>(
    `/api/v1/focus-sessions/${encodeURIComponent(id)}/${command}`,
    {
      method: "POST",
      headers,
      body: "{}",
    },
  );
  return normalizeFocusSessionSnapshot(payload);
}

export function pauseFocusSession(
  id: string,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(id, "pause", expectedVersion);
}

export function resumeFocusSession(
  id: string,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(id, "resume", expectedVersion);
}

export function stopFocusSession(
  id: string,
  expectedVersion: number,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(
    id,
    "stop",
    expectedVersion,
    idempotencyKey,
  );
}

export function cancelFocusSession(
  id: string,
  expectedVersion: number,
  idempotencyKey: string,
): Promise<FocusSessionSnapshot> {
  return executeFocusSessionCommand(
    id,
    "cancel",
    expectedVersion,
    idempotencyKey,
  );
}

export async function recoverFocusSession(
  id: string,
  action: FocusRecoveryAction,
  expectedVersion: number,
): Promise<FocusSessionSnapshot> {
  const payload = await apiRequest<unknown>(
    `/api/v1/focus-sessions/${encodeURIComponent(id)}/recover`,
    {
      method: "POST",
      headers: expectedVersionHeader(expectedVersion),
      body: JSON.stringify({ action }),
    },
  );
  return normalizeFocusSessionSnapshot(payload);
}

export async function getTodayStats(
  date: string,
  timezone?: string | number,
): Promise<TodayStats> {
  const params = new URLSearchParams({ date });
  if (typeof timezone === "string" && timezone.trim()) {
    params.set("timezone", timezone.trim());
  } else if (typeof timezone === "number") {
    params.set("timezone_offset_minutes", String(timezone));
  }
  const payload = await apiRequest<unknown>(`/api/v1/stats/today?${params}`);
  const data =
    isRecord(payload) && isRecord(payload.data) ? payload.data : payload;
  if (!isRecord(data) || !isRecord(data.tasks) || !isRecord(data.focus)) {
    throw new ApiError("今日统计响应格式无效", { code: "INVALID_RESPONSE" });
  }

  return {
    date: String(data.date ?? date),
    tasks: {
      total: numeric(data.tasks.total),
      completed: numeric(data.tasks.completed),
      remaining: numeric(data.tasks.remaining),
      overdue: numeric(data.tasks.overdue),
      dueSoon: numeric(data.tasks.due_soon ?? data.tasks.dueSoon),
      estimatedMinutes: numeric(
        data.tasks.estimated_minutes ?? data.tasks.estimatedMinutes,
      ),
      actualMinutes: numeric(
        data.tasks.actual_minutes ?? data.tasks.actualMinutes,
      ),
    },
    focus: {
      sessions: numeric(data.focus.sessions),
      seconds: numeric(data.focus.seconds),
      minutes: numeric(data.focus.minutes),
    },
  };
}
