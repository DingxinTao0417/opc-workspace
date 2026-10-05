import type {
  BackupArchiveDownload,
  BackupRestoreDrillResult,
  BackupSummary,
  BackupVerificationStatus,
  CreateBackupInput,
  RestoreDiagnostics,
  ScheduledBackupPolicy,
  ScheduledBackupRestoreResult,
  UpdateScheduledBackupPolicyInput,
} from "../../types/models";
import {
  BACKUP_OPERATION_TIMEOUT_MS,
  apiFetch,
  apiRequest,
  downloadFileName,
  expectedVersionHeader,
  fieldValue,
  invalidResponse,
  isRecord,
  nonNegativeInteger,
  numberField,
  positiveInteger,
  stringField,
} from "./core";

export function normalizeBackupSummary(value: unknown): BackupSummary {
  if (!isRecord(value)) {
    return invalidResponse("本地备份响应格式无效");
  }
  const id = stringField(value, "id");
  const createdAt = stringField(value, "created_at", "createdAt") ?? "";
  const verifiedAt = fieldValue(value, "verified_at", "verifiedAt");
  const verificationStatus = fieldValue(
    value,
    "verification_status",
    "verificationStatus",
  );
  const note = fieldValue(value, "note");
  const error = fieldValue(value, "error");
  const appVersion = fieldValue(value, "app_version", "appVersion");
  const apiVersion = fieldValue(value, "api_version", "apiVersion");
  const kind = fieldValue(value, "kind");
  if (
    !id ||
    (createdAt === "" && verificationStatus !== "invalid") ||
    !["verified", "unverified", "invalid"].includes(
      String(verificationStatus),
    ) ||
    (verifiedAt !== undefined && typeof verifiedAt !== "string") ||
    (note !== undefined && typeof note !== "string") ||
    (error !== undefined && typeof error !== "string") ||
    (appVersion !== undefined && typeof appVersion !== "string") ||
    (apiVersion !== undefined && typeof apiVersion !== "string") ||
    !["manual", "scheduled", "unknown"].includes(String(kind))
  ) {
    return invalidResponse("本地备份响应格式无效");
  }
  const invalid = verificationStatus === "invalid";
  const readCount = (snake: string, camel: string, label: string) =>
    nonNegativeInteger(
      fieldValue(value, snake, camel),
      label,
      invalid ? 0 : undefined,
    );
  return {
    id,
    createdAt,
    verifiedAt:
      typeof verifiedAt === "string" && verifiedAt ? verifiedAt : null,
    verificationStatus: verificationStatus as BackupVerificationStatus,
    note: typeof note === "string" ? note : "",
    appVersion: typeof appVersion === "string" ? appVersion : "",
    apiVersion: typeof apiVersion === "string" ? apiVersion : "",
    schemaVersion: readCount(
      "schema_version",
      "schemaVersion",
      "备份数据库版本",
    ),
    artifactCount: readCount("artifact_count", "artifactCount", "备份文件数量"),
    artifactBytes: readCount("artifact_bytes", "artifactBytes", "备份文件大小"),
    databaseBytes: readCount(
      "database_bytes",
      "databaseBytes",
      "备份数据库大小",
    ),
    totalBytes: readCount("total_bytes", "totalBytes", "备份总大小"),
    error: typeof error === "string" && error ? error : null,
    kind: kind as BackupSummary["kind"],
  };
}

export function normalizeScheduledBackupPolicy(
  value: unknown,
): ScheduledBackupPolicy {
  if (!isRecord(value)) {
    return invalidResponse("计划备份策略响应格式无效");
  }
  const enabled = fieldValue(value, "enabled");
  const localTime = stringField(value, "local_time", "localTime");
  const timezone = stringField(value, "timezone");
  const retentionCount = positiveInteger(
    fieldValue(value, "retention_count", "retentionCount"),
    "计划备份保留数量",
  );
  const lastStatus = fieldValue(value, "last_status", "lastStatus");
  const version = positiveInteger(fieldValue(value, "version"), "计划备份版本");
  const updatedAt = stringField(value, "updated_at", "updatedAt");
  const nullableString = (snake: string, camel: string): string | null => {
    const raw = fieldValue(value, snake, camel);
    if (raw === null || raw === undefined || raw === "") return null;
    if (typeof raw !== "string")
      return invalidResponse("计划备份策略响应格式无效");
    return raw;
  };
  if (
    typeof enabled !== "boolean" ||
    !localTime ||
    !/^\d{2}:\d{2}$/.test(localTime) ||
    !timezone ||
    retentionCount > 365 ||
    !["idle", "succeeded", "failed"].includes(String(lastStatus)) ||
    !updatedAt
  ) {
    return invalidResponse("计划备份策略响应格式无效");
  }
  return {
    enabled,
    localTime,
    timezone,
    retentionCount,
    lastAttemptedDate: nullableString(
      "last_attempted_date",
      "lastAttemptedDate",
    ),
    lastAttemptAt: nullableString("last_attempt_at", "lastAttemptAt"),
    lastSuccessAt: nullableString("last_success_at", "lastSuccessAt"),
    lastBackupId: nullableString("last_backup_id", "lastBackupId"),
    lastStatus: lastStatus as ScheduledBackupPolicy["lastStatus"],
    lastErrorCode: nullableString("last_error_code", "lastErrorCode"),
    version,
    updatedAt,
    nextRunAt: nullableString("next_run_at", "nextRunAt"),
  };
}

export async function getScheduledBackupPolicy(): Promise<ScheduledBackupPolicy> {
  const payload = await apiRequest<unknown>("/api/v1/backups/policy");
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeScheduledBackupPolicy(body);
}

export async function updateScheduledBackupPolicy(
  input: UpdateScheduledBackupPolicyInput,
): Promise<ScheduledBackupPolicy> {
  const payload = await apiRequest<unknown>("/api/v1/backups/policy", {
    method: "PATCH",
    headers: expectedVersionHeader(input.expectedVersion),
    body: JSON.stringify({
      enabled: input.enabled,
      local_time: input.localTime,
      timezone: input.timezone,
      retention_count: input.retentionCount,
    }),
  });
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeScheduledBackupPolicy(body);
}

export async function getBackups(): Promise<BackupSummary[]> {
  const payload = await apiRequest<unknown>("/api/v1/backups");
  if (!isRecord(payload) || !Array.isArray(payload.data)) {
    return invalidResponse("本地备份列表响应格式无效");
  }
  return payload.data.map(normalizeBackupSummary);
}

export async function createBackup(
  input: CreateBackupInput,
  idempotencyKey: string,
): Promise<BackupSummary> {
  const payload = await apiRequest<unknown>(
    "/api/v1/backups",
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({ note: input.note }),
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeBackupSummary(body);
}

export async function verifyBackup(id: string): Promise<BackupSummary> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/verify`,
    { method: "POST", body: "{}" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  return normalizeBackupSummary(body);
}

export async function drillBackupRestore(
  id: string,
): Promise<BackupRestoreDrillResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/drill`,
    { method: "POST", body: "{}" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) {
    return invalidResponse("备份恢复演练响应格式无效");
  }
  const backupId = stringField(body, "backup_id", "backupId");
  const drilledAt = stringField(body, "drilled_at", "drilledAt");
  const sourceSchemaVersion = positiveInteger(
    fieldValue(body, "source_schema_version", "sourceSchemaVersion"),
    "恢复演练来源数据库版本",
  );
  const resultSchemaVersion = positiveInteger(
    fieldValue(body, "result_schema_version", "resultSchemaVersion"),
    "恢复演练结果数据库版本",
  );
  const artifactCount = nonNegativeInteger(
    fieldValue(body, "artifact_count", "artifactCount"),
    "恢复演练文件数量",
  );
  const temporaryDataCleaned = fieldValue(
    body,
    "temporary_data_cleaned",
    "temporaryDataCleaned",
  );
  if (
    !backupId ||
    !drilledAt ||
    Number.isNaN(Date.parse(drilledAt)) ||
    temporaryDataCleaned !== true
  ) {
    return invalidResponse("备份恢复演练响应格式无效");
  }
  return {
    backupId,
    drilledAt,
    sourceSchemaVersion,
    resultSchemaVersion,
    artifactCount,
    temporaryDataCleaned,
  };
}

export async function scheduleBackupRestore(
  id: string,
): Promise<ScheduledBackupRestoreResult> {
  const payload = await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}/restore`,
    { method: "POST", body: JSON.stringify({ confirm: true }) },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) {
    return invalidResponse("备份恢复安排响应格式无效");
  }
  const backupId = stringField(body, "backup_id", "backupId");
  const rollbackBackupId = stringField(
    body,
    "rollback_backup_id",
    "rollbackBackupId",
  );
  const requestedAt = stringField(body, "requested_at", "requestedAt");
  const restartRequired = fieldValue(
    body,
    "restart_required",
    "restartRequired",
  );
  if (
    !backupId ||
    !rollbackBackupId ||
    backupId === rollbackBackupId ||
    !requestedAt ||
    Number.isNaN(Date.parse(requestedAt)) ||
    restartRequired !== true
  ) {
    return invalidResponse("备份恢复安排响应格式无效");
  }
  return { backupId, rollbackBackupId, requestedAt, restartRequired };
}

export async function deleteBackup(id: string): Promise<void> {
  await apiRequest<unknown>(
    `/api/v1/backups/${encodeURIComponent(id)}?confirm=true`,
    { method: "DELETE" },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function downloadBackupArchive(
  id: string,
): Promise<BackupArchiveDownload> {
  return apiFetch(
    `/api/v1/backups/${encodeURIComponent(id)}/archive`,
    async (response) => {
      const contentType = response.headers.get("Content-Type");
      const mimeType = contentType?.split(";", 1)[0]?.trim().toLowerCase();
      if (
        mimeType !== "application/zip" ||
        response.headers.get("X-Backup-Package-Format-Version") !== "1" ||
        response.headers.get("X-Backup-ID") !== id
      ) {
        return invalidResponse("完整备份下载响应格式无效");
      }
      const blob = await response.blob();
      if (blob.size === 0) {
        return invalidResponse("完整备份下载响应格式无效");
      }
      return {
        blob,
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          `opc-workspace-backup-${id}.zip`,
        ),
        backupId: id,
        formatVersion: 1,
      };
    },
    {},
    "application/zip",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function getRestoreDiagnostics(): Promise<RestoreDiagnostics> {
  const payload = await apiRequest<unknown>(
    "/api/v1/backups/restore-diagnostics",
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("恢复诊断响应格式无效");
  const status = stringField(body, "status");
  const restartRequired = fieldValue(
    body,
    "restart_required",
    "restartRequired",
  );
  const appliedThisStartup = fieldValue(
    body,
    "applied_this_startup",
    "appliedThisStartup",
  );
  const cleanupRequired = fieldValue(
    body,
    "cleanup_required",
    "cleanupRequired",
  );
  const attentionRequired = fieldValue(
    body,
    "attention_required",
    "attentionRequired",
  );
  const backupId = fieldValue(body, "backup_id", "backupId");
  const rollbackBackupId = fieldValue(
    body,
    "rollback_backup_id",
    "rollbackBackupId",
  );
  const requestedAt = fieldValue(body, "requested_at", "requestedAt");
  const residualAppliedCount = numberField(
    body,
    "residual_applied_count",
    "residualAppliedCount",
  );
  const failedAttemptCount = numberField(
    body,
    "failed_attempt_count",
    "failedAttemptCount",
  );
  const invalidEntryCount = numberField(
    body,
    "invalid_entry_count",
    "invalidEntryCount",
  );
  const statuses = new Set([
    "idle",
    "restart_required",
    "restored",
    "cleanup_required",
    "attention_required",
  ]);
  const canonicalId = (value: unknown) =>
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    );
  if (
    !status ||
    !statuses.has(status) ||
    typeof restartRequired !== "boolean" ||
    typeof appliedThisStartup !== "boolean" ||
    typeof cleanupRequired !== "boolean" ||
    typeof attentionRequired !== "boolean" ||
    (backupId !== null && !canonicalId(backupId)) ||
    (rollbackBackupId !== null && !canonicalId(rollbackBackupId)) ||
    (backupId === null) !== (rollbackBackupId === null) ||
    (requestedAt !== null &&
      (typeof requestedAt !== "string" ||
        Number.isNaN(Date.parse(requestedAt)))) ||
    residualAppliedCount === undefined ||
    !Number.isInteger(residualAppliedCount) ||
    residualAppliedCount < 0 ||
    failedAttemptCount === undefined ||
    !Number.isInteger(failedAttemptCount) ||
    failedAttemptCount < 0 ||
    invalidEntryCount === undefined ||
    !Number.isInteger(invalidEntryCount) ||
    invalidEntryCount < 0 ||
    (status === "restart_required" && !restartRequired) ||
    (status === "cleanup_required" && !cleanupRequired) ||
    (status === "attention_required" && !attentionRequired) ||
    (status === "restored" && !appliedThisStartup) ||
    (status === "idle" &&
      (restartRequired ||
        appliedThisStartup ||
        cleanupRequired ||
        attentionRequired))
  ) {
    return invalidResponse("恢复诊断响应格式无效");
  }
  return {
    status: status as RestoreDiagnostics["status"],
    restartRequired,
    appliedThisStartup,
    cleanupRequired,
    attentionRequired,
    backupId: typeof backupId === "string" && backupId ? backupId : null,
    rollbackBackupId:
      typeof rollbackBackupId === "string" && rollbackBackupId
        ? rollbackBackupId
        : null,
    requestedAt: typeof requestedAt === "string" ? requestedAt : null,
    residualAppliedCount,
    failedAttemptCount,
    invalidEntryCount,
  };
}
