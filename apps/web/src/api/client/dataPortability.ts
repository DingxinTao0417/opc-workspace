import type {
  BusinessDataExportDownload,
  BusinessImportApplyMode,
  BusinessImportPreview,
  BusinessImportResolutionRule,
  BusinessImportResult,
  BusinessPackageDownload,
  BusinessPackageImportPreview,
  BusinessPackageImportResult,
  DiagnosticPackageDownload,
  StorageCapacityHistoryResult,
  StorageCapacityResult,
  StorageCapacityScope,
  StorageCapacityStatus,
} from "../../types/models";
import {
  BACKUP_OPERATION_TIMEOUT_MS,
  apiFetch,
  apiRequest,
  downloadFileName,
  fieldValue,
  hasExactKeys,
  invalidResponse,
  isRecord,
  numberField,
  positiveInteger,
  stringField,
} from "./core";
import type { JsonRecord } from "./core";

export async function downloadBusinessDataExport(): Promise<BusinessDataExportDownload> {
  return apiFetch(
    "/api/v1/exports/business-data",
    async (response) => {
      if (
        response.headers.get("X-Export-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/json")
      ) {
        return invalidResponse("业务数据导出响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-business-export.json",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/json",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function downloadBusinessPackage(): Promise<BusinessPackageDownload> {
  return apiFetch(
    "/api/v1/exports/business-package",
    async (response) => {
      if (
        response.headers.get("X-Business-Package-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/zip")
      ) {
        return invalidResponse("含文件业务导出包响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-business-files.zip",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/zip",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

const businessImportBlockers = new Set([
  "target_key_conflicts",
  "target_semantic_conflicts",
  "source_schema_older",
  "source_schema_newer",
  "apply_rehearsal_failed",
]);

const businessPackageImportBlockers = new Set([
  ...businessImportBlockers,
  "target_file_conflicts",
]);

const businessImportApplyModes = new Set(["replace_empty", "append"]);

const businessImportResolutionRules = new Set([
  "keep_target",
  "skip_identical",
]);

/**
 * The part of a preview that describes how the package will be applied: the
 * steps that bring an older package to the current structure, the rows that
 * would repeat a unique value, and the rows the import leaves out.
 */
function normalizeBusinessImportPlanMetadata(
  body: JsonRecord,
  message: string,
) {
  const rawSteps = fieldValue(body, "upgrade_steps", "upgradeSteps");
  const semanticConflicts = numberField(
    body,
    "semantic_conflicts",
    "semanticConflicts",
  );
  const rawRules = fieldValue(
    body,
    "semantic_conflict_rules",
    "semanticConflictRules",
  );
  const rawResolutions = fieldValue(body, "resolutions");
  const rawDigest = fieldValue(body, "plan_digest", "planDigest");
  const rawRehearsal = fieldValue(
    body,
    "rehearsal_failure",
    "rehearsalFailure",
  );
  // Only the token of the rule that rejected the rows, nothing else.
  if (
    rawRehearsal !== undefined &&
    (!isRecord(rawRehearsal) ||
      Object.keys(rawRehearsal).length !== 1 ||
      typeof rawRehearsal.code !== "string" ||
      !/^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$/.test(rawRehearsal.code) ||
      rawRehearsal.code.length > 80)
  ) {
    return invalidResponse(message);
  }
  if (
    !Array.isArray(rawSteps) ||
    semanticConflicts === undefined ||
    !Number.isInteger(semanticConflicts) ||
    semanticConflicts < 0 ||
    !Array.isArray(rawRules) ||
    !Array.isArray(rawResolutions) ||
    (rawDigest !== undefined &&
      (typeof rawDigest !== "string" || !/^[0-9a-f]{64}$/.test(rawDigest)))
  ) {
    return invalidResponse(message);
  }
  const upgradeSteps = rawSteps.map((value) => {
    if (!isRecord(value)) return invalidResponse(message);
    const schemaVersion = numberField(value, "schema_version", "schemaVersion");
    const table = stringField(value, "table");
    const change = stringField(value, "change");
    const column = stringField(value, "column");
    if (
      schemaVersion === undefined ||
      !Number.isInteger(schemaVersion) ||
      schemaVersion < 1 ||
      !table ||
      !change ||
      !column
    ) {
      return invalidResponse(message);
    }
    return { schemaVersion, table, change, column };
  });
  const semanticConflictRules = rawRules.map((value) => {
    if (!isRecord(value)) return invalidResponse(message);
    const table = stringField(value, "table");
    const index = stringField(value, "index");
    const conflicts = numberField(value, "conflicts");
    if (
      !table ||
      !index ||
      conflicts === undefined ||
      !Number.isInteger(conflicts) ||
      conflicts < 1
    ) {
      return invalidResponse(message);
    }
    return { table, index, conflicts };
  });
  const resolutions = rawResolutions.map((value) => {
    if (!isRecord(value)) return invalidResponse(message);
    const table = stringField(value, "table");
    const rule = stringField(value, "rule");
    const rows = numberField(value, "rows");
    if (
      !table ||
      !rule ||
      !businessImportResolutionRules.has(rule) ||
      rows === undefined ||
      !Number.isInteger(rows) ||
      rows < 1
    ) {
      return invalidResponse(message);
    }
    return { table, rule: rule as BusinessImportResolutionRule, rows };
  });
  if (
    semanticConflictRules.reduce((sum, rule) => sum + rule.conflicts, 0) !==
    semanticConflicts
  ) {
    return invalidResponse(message);
  }
  return {
    upgradeSteps,
    semanticConflicts,
    semanticConflictRules,
    resolutions,
    planDigest: typeof rawDigest === "string" ? rawDigest : null,
    rehearsalFailure: isRecord(rawRehearsal)
      ? { code: rawRehearsal.code as string }
      : null,
  };
}

/**
 * What every applicable or blocked preview has to satisfy, whatever its
 * format: the blocker has to agree with the counts, an applicable plan has no
 * conflict left, and a digest accompanies exactly the plans that leave rows
 * out.
 */
function businessImportPlanIsConsistent(input: {
  schemaVersion: number;
  targetSchemaVersion: number;
  canApply: boolean;
  blocker: unknown;
  keyConflicts: number;
  plan: ReturnType<typeof normalizeBusinessImportPlanMetadata>;
}): boolean {
  const { plan } = input;
  const crossSchema = input.schemaVersion !== input.targetSchemaVersion;
  if (!crossSchema && plan.upgradeSteps.length !== 0) return false;
  if (
    plan.upgradeSteps.some(
      (step) =>
        step.schemaVersion <= input.schemaVersion ||
        step.schemaVersion > input.targetSchemaVersion,
    )
  ) {
    return false;
  }
  // The rehearsal is the last check: it names a rule exactly when it is the
  // reason, and only for a package nothing else stood against.
  if (
    (plan.rehearsalFailure !== null) !==
    (input.blocker === "apply_rehearsal_failed")
  ) {
    return false;
  }
  if (input.blocker === "apply_rehearsal_failed") {
    return (
      input.schemaVersion <= input.targetSchemaVersion &&
      input.keyConflicts === 0 &&
      plan.semanticConflicts === 0 &&
      plan.planDigest === null
    );
  }
  if (input.canApply) {
    return (
      input.schemaVersion <= input.targetSchemaVersion &&
      input.keyConflicts === 0 &&
      plan.semanticConflicts === 0 &&
      (plan.planDigest !== null) === (plan.resolutions.length !== 0)
    );
  }
  if (plan.planDigest !== null) return false;
  if (input.blocker === "target_key_conflicts") {
    return input.schemaVersion <= input.targetSchemaVersion;
  }
  if (input.blocker === "target_semantic_conflicts") {
    return (
      input.schemaVersion <= input.targetSchemaVersion &&
      input.keyConflicts === 0 &&
      plan.semanticConflicts !== 0
    );
  }
  if (
    input.blocker === "source_schema_older" ||
    input.blocker === "source_schema_newer"
  ) {
    return (
      plan.upgradeSteps.length === 0 &&
      plan.semanticConflicts === 0 &&
      plan.resolutions.length === 0
    );
  }
  return plan.semanticConflicts === 0;
}

/** The phrase that names both the apply mode and a schema upgrade. */
function businessImportConfirmation(input: {
  applyMode: BusinessImportApplyMode;
  upgrade: boolean;
  withFiles: boolean;
}): string {
  const base =
    input.applyMode === "append"
      ? "append-to-existing-workspace"
      : "replace-empty-workspace";
  return `${input.upgrade ? "upgrade-and-" : ""}${base}${
    input.withFiles ? "-with-controlled-files" : ""
  }`;
}

function businessImportHeaders(input: {
  applyMode: BusinessImportApplyMode;
  upgrade: boolean;
  planDigest: string | null;
  withFiles: boolean;
}): Record<string, string> {
  return {
    "X-Import-Confirmation": businessImportConfirmation(input),
    ...(input.planDigest ? { "X-Import-Plan-Digest": input.planDigest } : {}),
  };
}

function normalizeBusinessImportConflictMetadata(
  body: JsonRecord,
  message: string,
) {
  const targetSchemaVersion = numberField(
    body,
    "target_schema_version",
    "targetSchemaVersion",
  );
  const targetRows = numberField(body, "target_rows", "targetRows");
  const keyConflicts = numberField(body, "key_conflicts", "keyConflicts");
  const rawTables = fieldValue(body, "conflict_tables", "conflictTables");
  if (
    targetSchemaVersion === undefined ||
    !Number.isInteger(targetSchemaVersion) ||
    targetSchemaVersion < 1 ||
    targetRows === undefined ||
    !Number.isInteger(targetRows) ||
    targetRows < 0 ||
    keyConflicts === undefined ||
    !Number.isInteger(keyConflicts) ||
    keyConflicts < 0 ||
    !Array.isArray(rawTables)
  ) {
    return invalidResponse(message);
  }
  const conflictTables = rawTables.map((value) => {
    if (!isRecord(value)) return invalidResponse(message);
    const table = stringField(value, "table");
    const incomingRows = numberField(value, "incoming_rows", "incomingRows");
    const tableTargetRows = numberField(value, "target_rows", "targetRows");
    const tableKeyConflicts = numberField(
      value,
      "key_conflicts",
      "keyConflicts",
    );
    if (
      !table ||
      incomingRows === undefined ||
      !Number.isInteger(incomingRows) ||
      incomingRows < 0 ||
      tableTargetRows === undefined ||
      !Number.isInteger(tableTargetRows) ||
      tableTargetRows < 1 ||
      tableKeyConflicts === undefined ||
      !Number.isInteger(tableKeyConflicts) ||
      tableKeyConflicts < 0 ||
      // A deleted Project that survives only as an Inbox source claims its
      // identifier without an incoming row, so only the target bounds this.
      tableKeyConflicts > tableTargetRows
    ) {
      return invalidResponse(message);
    }
    return {
      table,
      incomingRows,
      targetRows: tableTargetRows,
      keyConflicts: tableKeyConflicts,
    };
  });
  const uniqueTables = new Set(conflictTables.map((table) => table.table));
  if (
    uniqueTables.size !== conflictTables.length ||
    conflictTables.reduce((sum, table) => sum + table.targetRows, 0) !==
      targetRows ||
    conflictTables.reduce((sum, table) => sum + table.keyConflicts, 0) !==
      keyConflicts
  ) {
    return invalidResponse(message);
  }
  return { targetSchemaVersion, targetRows, keyConflicts, conflictTables };
}

function normalizeBusinessPackageImportPreview(
  value: unknown,
): BusinessPackageImportPreview {
  const body = isRecord(value) && "data" in value ? value.data : value;
  if (!isRecord(body)) return invalidResponse("含文件业务包预检响应格式无效");
  const formatVersion = numberField(body, "format_version", "formatVersion");
  const schemaVersion = numberField(body, "schema_version", "schemaVersion");
  const exportedAt = stringField(body, "exported_at", "exportedAt");
  const totalRows = numberField(body, "total_rows", "totalRows");
  const fileCount = numberField(body, "file_count", "fileCount");
  const fileBytes = numberField(body, "file_bytes", "fileBytes");
  const fileConflicts = numberField(body, "file_conflicts", "fileConflicts");
  const canApply = fieldValue(body, "can_apply", "canApply");
  const applyMode = fieldValue(body, "apply_mode", "applyMode");
  const blocker = fieldValue(body, "blocker");
  const rawCounts = fieldValue(body, "table_counts", "tableCounts");
  const conflict = normalizeBusinessImportConflictMetadata(
    body,
    "含文件业务包预检响应格式无效",
  );
  const plan = normalizeBusinessImportPlanMetadata(
    body,
    "含文件业务包预检响应格式无效",
  );
  const skippedFiles = numberField(body, "skipped_files", "skippedFiles");
  if (
    skippedFiles === undefined ||
    !Number.isInteger(skippedFiles) ||
    skippedFiles < 0 ||
    formatVersion !== 1 ||
    !schemaVersion ||
    !Number.isInteger(schemaVersion) ||
    !exportedAt ||
    Number.isNaN(Date.parse(exportedAt)) ||
    totalRows === undefined ||
    !Number.isInteger(totalRows) ||
    totalRows < 0 ||
    fileCount === undefined ||
    !Number.isInteger(fileCount) ||
    fileCount < 0 ||
    fileBytes === undefined ||
    !Number.isSafeInteger(fileBytes) ||
    fileBytes < 0 ||
    fileConflicts === undefined ||
    !Number.isInteger(fileConflicts) ||
    fileConflicts < 0 ||
    typeof canApply !== "boolean" ||
    !isRecord(rawCounts) ||
    (blocker !== undefined &&
      (typeof blocker !== "string" ||
        !businessPackageImportBlockers.has(blocker))) ||
    (canApply && blocker !== undefined) ||
    (canApply &&
      (typeof applyMode !== "string" ||
        !businessImportApplyModes.has(applyMode))) ||
    (!canApply && applyMode !== undefined) ||
    (!canApply &&
      (typeof blocker !== "string" ||
        !businessPackageImportBlockers.has(blocker)))
  ) {
    return invalidResponse("含文件业务包预检响应格式无效");
  }
  const tableCounts: Record<string, number> = {};
  for (const [key, count] of Object.entries(rawCounts)) {
    if (!key || !Number.isInteger(count) || (count as number) < 0) {
      return invalidResponse("含文件业务包预检响应格式无效");
    }
    tableCounts[key] = count as number;
  }
  if (
    Object.values(tableCounts).reduce((sum, count) => sum + count, 0) !==
      totalRows ||
    skippedFiles > fileCount ||
    !businessImportPlanIsConsistent({
      schemaVersion,
      targetSchemaVersion: conflict.targetSchemaVersion,
      canApply,
      blocker,
      keyConflicts: conflict.keyConflicts,
      plan,
    }) ||
    (canApply &&
      (fileConflicts !== 0 ||
        (applyMode === "replace_empty" && conflict.targetRows !== 0) ||
        (applyMode === "append" && conflict.targetRows === 0))) ||
    (blocker === "target_key_conflicts" &&
      (conflict.targetRows === 0 || conflict.keyConflicts === 0)) ||
    (blocker === "target_file_conflicts" &&
      (schemaVersion > conflict.targetSchemaVersion ||
        conflict.keyConflicts !== 0 ||
        fileConflicts === 0)) ||
    (blocker === "source_schema_older" &&
      (schemaVersion >= conflict.targetSchemaVersion ||
        conflict.targetRows !== 0 ||
        conflict.keyConflicts !== 0 ||
        conflict.conflictTables.length !== 0)) ||
    (blocker === "source_schema_newer" &&
      (schemaVersion <= conflict.targetSchemaVersion ||
        conflict.targetRows !== 0 ||
        conflict.keyConflicts !== 0 ||
        conflict.conflictTables.length !== 0))
  ) {
    return invalidResponse("含文件业务包预检响应格式无效");
  }
  return {
    formatVersion: 1,
    schemaVersion,
    targetSchemaVersion: conflict.targetSchemaVersion,
    exportedAt,
    tableCounts,
    totalRows,
    targetRows: conflict.targetRows,
    keyConflicts: conflict.keyConflicts,
    conflictTables: conflict.conflictTables,
    ...plan,
    fileCount,
    fileBytes,
    fileConflicts,
    skippedFiles,
    canApply,
    applyMode:
      typeof applyMode === "string"
        ? (applyMode as BusinessPackageImportPreview["applyMode"])
        : null,
    blocker:
      typeof blocker === "string"
        ? (blocker as BusinessPackageImportPreview["blocker"])
        : null,
  };
}

export async function previewBusinessPackageImport(
  file: File,
): Promise<BusinessPackageImportPreview> {
  return apiRequest<unknown>(
    "/api/v1/imports/business-package/preview",
    { method: "POST", body: file },
    BACKUP_OPERATION_TIMEOUT_MS,
  ).then(normalizeBusinessPackageImportPreview);
}

export async function applyBusinessPackageImport(input: {
  file: File;
  applyMode: BusinessImportApplyMode;
  /** The package comes from an older schema and is converted first. */
  upgrade: boolean;
  /** Names the plan the preview showed; required when it leaves rows out. */
  planDigest: string | null;
}): Promise<BusinessPackageImportResult> {
  const payload = await apiRequest<unknown>(
    "/api/v1/imports/business-package",
    {
      method: "POST",
      body: input.file,
      headers: businessImportHeaders({ ...input, withFiles: true }),
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("含文件业务包导入响应格式无效");
  const importedRows = numberField(body, "imported_rows", "importedRows");
  const importedFiles = numberField(body, "imported_files", "importedFiles");
  const backupId = stringField(body, "backup_id", "backupId");
  const applyMode = stringField(body, "apply_mode", "applyMode");
  if (
    importedRows === undefined ||
    !Number.isInteger(importedRows) ||
    importedRows < 0 ||
    importedFiles === undefined ||
    !Number.isInteger(importedFiles) ||
    importedFiles < 0 ||
    !backupId ||
    !applyMode ||
    !businessImportApplyModes.has(applyMode)
  ) {
    return invalidResponse("含文件业务包导入响应格式无效");
  }
  return {
    importedRows,
    importedFiles,
    backupId,
    applyMode: applyMode as BusinessImportApplyMode,
  };
}

function normalizeBusinessImportPreview(value: unknown): BusinessImportPreview {
  const body = isRecord(value) && "data" in value ? value.data : value;
  if (!isRecord(body)) return invalidResponse("业务数据导入预检响应格式无效");
  const formatVersion = numberField(body, "format_version", "formatVersion");
  const schemaVersion = numberField(body, "schema_version", "schemaVersion");
  const exportedAt = stringField(body, "exported_at", "exportedAt");
  const totalRows = numberField(body, "total_rows", "totalRows");
  const canApply = fieldValue(body, "can_apply", "canApply");
  const applyMode = fieldValue(body, "apply_mode", "applyMode");
  const blocker = fieldValue(body, "blocker");
  const rawCounts = fieldValue(body, "table_counts", "tableCounts");
  const conflict = normalizeBusinessImportConflictMetadata(
    body,
    "业务数据导入预检响应格式无效",
  );
  const plan = normalizeBusinessImportPlanMetadata(
    body,
    "业务数据导入预检响应格式无效",
  );
  if (
    formatVersion !== 1 ||
    !schemaVersion ||
    !Number.isInteger(schemaVersion) ||
    !exportedAt ||
    Number.isNaN(Date.parse(exportedAt)) ||
    totalRows === undefined ||
    !Number.isInteger(totalRows) ||
    totalRows < 0 ||
    typeof canApply !== "boolean" ||
    !isRecord(rawCounts) ||
    (blocker !== undefined &&
      (typeof blocker !== "string" || !businessImportBlockers.has(blocker))) ||
    (canApply && blocker !== undefined) ||
    (canApply &&
      (typeof applyMode !== "string" ||
        !businessImportApplyModes.has(applyMode))) ||
    (!canApply && applyMode !== undefined) ||
    (!canApply &&
      (typeof blocker !== "string" || !businessImportBlockers.has(blocker)))
  ) {
    return invalidResponse("业务数据导入预检响应格式无效");
  }
  const tableCounts: Record<string, number> = {};
  for (const [key, count] of Object.entries(rawCounts)) {
    if (!key || !Number.isInteger(count) || (count as number) < 0) {
      return invalidResponse("业务数据导入预检响应格式无效");
    }
    tableCounts[key] = count as number;
  }
  if (
    Object.values(tableCounts).reduce((sum, count) => sum + count, 0) !==
      totalRows ||
    !businessImportPlanIsConsistent({
      schemaVersion,
      targetSchemaVersion: conflict.targetSchemaVersion,
      canApply,
      blocker,
      keyConflicts: conflict.keyConflicts,
      plan,
    }) ||
    (canApply &&
      ((applyMode === "replace_empty" && conflict.targetRows !== 0) ||
        (applyMode === "append" && conflict.targetRows === 0))) ||
    (blocker === "target_key_conflicts" &&
      (conflict.targetRows === 0 || conflict.keyConflicts === 0)) ||
    (blocker === "source_schema_older" &&
      (schemaVersion >= conflict.targetSchemaVersion ||
        conflict.targetRows !== 0 ||
        conflict.keyConflicts !== 0 ||
        conflict.conflictTables.length !== 0)) ||
    (blocker === "source_schema_newer" &&
      (schemaVersion <= conflict.targetSchemaVersion ||
        conflict.targetRows !== 0 ||
        conflict.keyConflicts !== 0 ||
        conflict.conflictTables.length !== 0))
  ) {
    return invalidResponse("业务数据导入预检响应格式无效");
  }
  return {
    formatVersion: 1,
    schemaVersion,
    targetSchemaVersion: conflict.targetSchemaVersion,
    exportedAt,
    tableCounts,
    totalRows,
    targetRows: conflict.targetRows,
    keyConflicts: conflict.keyConflicts,
    conflictTables: conflict.conflictTables,
    ...plan,
    canApply,
    applyMode:
      typeof applyMode === "string"
        ? (applyMode as BusinessImportPreview["applyMode"])
        : null,
    blocker:
      typeof blocker === "string"
        ? (blocker as BusinessImportPreview["blocker"])
        : null,
  };
}

export async function previewBusinessDataImport(
  file: File,
): Promise<BusinessImportPreview> {
  return apiRequest<unknown>(
    "/api/v1/imports/business-data/preview",
    { method: "POST", body: file },
    BACKUP_OPERATION_TIMEOUT_MS,
  ).then(normalizeBusinessImportPreview);
}

export async function applyBusinessDataImport(input: {
  file: File;
  applyMode: BusinessImportApplyMode;
  /** The package comes from an older schema and is converted first. */
  upgrade: boolean;
  /** Names the plan the preview showed; required when it leaves rows out. */
  planDigest: string | null;
}): Promise<BusinessImportResult> {
  const payload = await apiRequest<unknown>(
    "/api/v1/imports/business-data",
    {
      method: "POST",
      body: input.file,
      headers: businessImportHeaders({ ...input, withFiles: false }),
    },
    BACKUP_OPERATION_TIMEOUT_MS,
  );
  const body = isRecord(payload) && "data" in payload ? payload.data : payload;
  if (!isRecord(body)) return invalidResponse("业务数据导入响应格式无效");
  const importedRows = numberField(body, "imported_rows", "importedRows");
  const backupId = stringField(body, "backup_id", "backupId");
  const applyMode = stringField(body, "apply_mode", "applyMode");
  if (
    importedRows === undefined ||
    !Number.isInteger(importedRows) ||
    importedRows < 0 ||
    !backupId ||
    !applyMode ||
    !businessImportApplyModes.has(applyMode)
  ) {
    return invalidResponse("业务数据导入响应格式无效");
  }
  return {
    importedRows,
    backupId,
    applyMode: applyMode as BusinessImportApplyMode,
  };
}

export async function downloadDiagnosticPackage(): Promise<DiagnosticPackageDownload> {
  return apiFetch(
    "/api/v1/diagnostics/package",
    async (response) => {
      if (
        response.headers.get("X-Diagnostic-Format-Version") !== "1" ||
        !response.headers.get("Content-Type")?.startsWith("application/zip")
      ) {
        return invalidResponse("诊断包响应格式无效");
      }
      return {
        blob: await response.blob(),
        fileName: downloadFileName(
          response.headers.get("Content-Disposition"),
          "opc-workspace-diagnostics.zip",
        ),
        formatVersion: 1,
      };
    },
    {},
    "application/zip",
    BACKUP_OPERATION_TIMEOUT_MS,
  );
}

export async function getStorageCapacity(): Promise<StorageCapacityResult> {
  const payload = await apiRequest<unknown>(
    "/api/v1/diagnostics/storage/check",
    { method: "POST" },
  );
  if (
    !isRecord(payload) ||
    !hasExactKeys(payload, ["data"]) ||
    !isRecord(payload.data)
  ) {
    return invalidResponse("存储容量响应格式无效");
  }
  const data = payload.data;
  if (
    !hasExactKeys(data, ["checked_at", "threshold_gib", "locations"]) ||
    typeof data.checked_at !== "string" ||
    Number.isNaN(Date.parse(data.checked_at)) ||
    !Array.isArray(data.locations) ||
    data.locations.length !== 3
  ) {
    return invalidResponse("存储容量响应格式无效");
  }
  const kinds = ["database", "artifacts", "backups"] as const;
  const locations = data.locations.map((location, index) => {
    if (
      !isRecord(location) ||
      !hasExactKeys(location, [
        "kind",
        "status",
        "available_bytes",
        "total_bytes",
        "shared_volume",
      ]) ||
      location.kind !== kinds[index] ||
      typeof location.shared_volume !== "boolean" ||
      (location.status !== "healthy" &&
        location.status !== "low" &&
        location.status !== "unavailable")
    ) {
      return invalidResponse("存储容量位置响应格式无效");
    }
    const unavailable = location.status === "unavailable";
    if (
      (unavailable &&
        (location.available_bytes !== null || location.total_bytes !== null)) ||
      (!unavailable &&
        (typeof location.available_bytes !== "number" ||
          typeof location.total_bytes !== "number" ||
          !Number.isSafeInteger(location.available_bytes) ||
          !Number.isSafeInteger(location.total_bytes) ||
          location.available_bytes < 0 ||
          location.total_bytes < 1 ||
          location.available_bytes > location.total_bytes))
    ) {
      return invalidResponse("存储容量位置响应值无效");
    }
    const status = location.status as StorageCapacityStatus;
    return {
      kind: kinds[index],
      status,
      availableBytes: unavailable ? null : (location.available_bytes as number),
      totalBytes: unavailable ? null : (location.total_bytes as number),
      sharedVolume: location.shared_volume,
    };
  });
  return {
    checkedAt: data.checked_at,
    thresholdGiB: positiveInteger(data.threshold_gib, "低空间阈值"),
    locations,
  };
}

const storageCapacityScopes = new Set<StorageCapacityScope>([
  "database",
  "artifacts",
  "backups",
  "database+artifacts",
  "database+backups",
  "artifacts+backups",
  "database+artifacts+backups",
]);

export async function getStorageCapacityHistory(
  days = 7,
): Promise<StorageCapacityHistoryResult> {
  if (!Number.isInteger(days) || days < 1 || days > 30) {
    throw new Error("容量趋势天数必须是 1–30 的整数");
  }
  const payload = await apiRequest<unknown>(
    `/api/v1/diagnostics/storage/history?days=${days}`,
  );
  if (
    !isRecord(payload) ||
    !hasExactKeys(payload, ["data"]) ||
    !isRecord(payload.data)
  ) {
    return invalidResponse("存储容量趋势响应格式无效");
  }
  const data = payload.data;
  if (
    !hasExactKeys(data, ["from", "to", "points"]) ||
    typeof data.from !== "string" ||
    typeof data.to !== "string" ||
    Number.isNaN(Date.parse(data.from)) ||
    Number.isNaN(Date.parse(data.to)) ||
    Date.parse(data.from) > Date.parse(data.to) ||
    !Array.isArray(data.points)
  ) {
    return invalidResponse("存储容量趋势响应格式无效");
  }
  const from = data.from;
  const to = data.to;
  let previousTime = -Infinity;
  const points = data.points.map((point) => {
    if (
      !isRecord(point) ||
      !hasExactKeys(point, [
        "scope",
        "checked_at",
        "available_bytes",
        "total_bytes",
        "threshold_bytes",
        "status",
      ]) ||
      typeof point.scope !== "string" ||
      !storageCapacityScopes.has(point.scope as StorageCapacityScope) ||
      typeof point.checked_at !== "string" ||
      Number.isNaN(Date.parse(point.checked_at)) ||
      (point.status !== "healthy" && point.status !== "low") ||
      !Number.isSafeInteger(point.available_bytes) ||
      !Number.isSafeInteger(point.total_bytes) ||
      !Number.isSafeInteger(point.threshold_bytes) ||
      (point.available_bytes as number) < 0 ||
      (point.total_bytes as number) < 1 ||
      (point.available_bytes as number) > (point.total_bytes as number) ||
      (point.threshold_bytes as number) < 1
    ) {
      return invalidResponse("存储容量趋势点响应无效");
    }
    const checkedAt = Date.parse(point.checked_at);
    if (
      checkedAt < previousTime ||
      checkedAt < Date.parse(from) ||
      checkedAt > Date.parse(to)
    ) {
      return invalidResponse("存储容量趋势顺序无效");
    }
    previousTime = checkedAt;
    return {
      scope: point.scope as StorageCapacityScope,
      checkedAt: point.checked_at,
      availableBytes: point.available_bytes as number,
      totalBytes: point.total_bytes as number,
      thresholdBytes: point.threshold_bytes as number,
      status: point.status as "healthy" | "low",
    };
  });
  return { from, to, points };
}
