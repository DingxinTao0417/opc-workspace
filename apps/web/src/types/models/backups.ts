export type BackupVerificationStatus = "verified" | "unverified" | "invalid";

export type BackupKind = "manual" | "scheduled" | "unknown";

export interface BackupSummary {
  id: string;
  createdAt: string;
  verifiedAt: string | null;
  verificationStatus: BackupVerificationStatus;
  note: string;
  appVersion: string;
  apiVersion: string;
  schemaVersion: number;
  artifactCount: number;
  artifactBytes: number;
  databaseBytes: number;
  totalBytes: number;
  error: string | null;
  kind: BackupKind;
}

export interface CreateBackupInput {
  note: string;
}

export interface BackupArchiveDownload {
  blob: Blob;
  fileName: string;
  backupId: string;
  formatVersion: 1;
}

export type ScheduledBackupStatus = "idle" | "succeeded" | "failed";

export interface ScheduledBackupPolicy {
  enabled: boolean;
  localTime: string;
  timezone: string;
  retentionCount: number;
  lastAttemptedDate: string | null;
  lastAttemptAt: string | null;
  lastSuccessAt: string | null;
  lastBackupId: string | null;
  lastStatus: ScheduledBackupStatus;
  lastErrorCode: string | null;
  version: number;
  updatedAt: string;
  nextRunAt: string | null;
}

export interface UpdateScheduledBackupPolicyInput {
  enabled: boolean;
  localTime: string;
  timezone: string;
  retentionCount: number;
  expectedVersion: number;
}

export interface BackupRestoreDrillResult {
  backupId: string;
  drilledAt: string;
  sourceSchemaVersion: number;
  resultSchemaVersion: number;
  artifactCount: number;
  temporaryDataCleaned: boolean;
}

export interface ScheduledBackupRestoreResult {
  backupId: string;
  rollbackBackupId: string;
  requestedAt: string;
  restartRequired: true;
}

export interface BusinessDataExportDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export interface BusinessPackageDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export interface BusinessPackageImportPreview {
  formatVersion: 1;
  schemaVersion: number;
  targetSchemaVersion: number;
  exportedAt: string;
  tableCounts: Record<string, number>;
  totalRows: number;
  targetRows: number;
  keyConflicts: number;
  conflictTables: BusinessImportTableConflict[];
  upgradeSteps: BusinessImportUpgradeStep[];
  semanticConflicts: number;
  semanticConflictRules: BusinessImportSemanticConflict[];
  resolutions: BusinessImportResolution[];
  planDigest: string | null;
  fileCount: number;
  fileBytes: number;
  fileConflicts: number;
  skippedFiles: number;
  canApply: boolean;
  applyMode: BusinessImportApplyMode | null;
  blocker: BusinessImportBlocker | null;
  /** Set exactly when the blocker is `apply_rehearsal_failed`. */
  rehearsalFailure: BusinessImportRehearsalFailure | null;
}

export type BusinessImportBlocker =
  | "target_key_conflicts"
  | "target_semantic_conflicts"
  | "target_file_conflicts"
  | "source_schema_older"
  | "source_schema_newer"
  | "apply_rehearsal_failed";

/**
 * Why a rehearsed import would be rejected: the stable token of the database
 * rule, never row values or the database's own message.
 */
export interface BusinessImportRehearsalFailure {
  code: string;
}

/** One change an older package goes through before it is imported. */
export interface BusinessImportUpgradeStep {
  schemaVersion: number;
  table: string;
  change: string;
  column: string;
}

/** Incoming rows that would repeat a value a UNIQUE index protects. */
export interface BusinessImportSemanticConflict {
  table: string;
  index: string;
  conflicts: number;
}

export type BusinessImportResolutionRule = "keep_target" | "skip_identical";

/** Incoming rows that the import will not write. */
export interface BusinessImportResolution {
  table: string;
  rule: BusinessImportResolutionRule;
  rows: number;
}

export type BusinessImportApplyMode = "replace_empty" | "append";

export interface BusinessImportTableConflict {
  table: string;
  incomingRows: number;
  targetRows: number;
  keyConflicts: number;
}

export interface BusinessPackageImportResult {
  importedRows: number;
  importedFiles: number;
  backupId: string;
  applyMode: BusinessImportApplyMode;
}

export interface RestoreDiagnostics {
  status:
    | "idle"
    | "restart_required"
    | "restored"
    | "cleanup_required"
    | "attention_required";
  restartRequired: boolean;
  appliedThisStartup: boolean;
  cleanupRequired: boolean;
  attentionRequired: boolean;
  backupId: string | null;
  rollbackBackupId: string | null;
  requestedAt: string | null;
  residualAppliedCount: number;
  failedAttemptCount: number;
  invalidEntryCount: number;
}

export interface BusinessImportPreview {
  formatVersion: 1;
  schemaVersion: number;
  targetSchemaVersion: number;
  exportedAt: string;
  tableCounts: Record<string, number>;
  totalRows: number;
  targetRows: number;
  keyConflicts: number;
  conflictTables: BusinessImportTableConflict[];
  upgradeSteps: BusinessImportUpgradeStep[];
  semanticConflicts: number;
  semanticConflictRules: BusinessImportSemanticConflict[];
  resolutions: BusinessImportResolution[];
  planDigest: string | null;
  canApply: boolean;
  applyMode: BusinessImportApplyMode | null;
  blocker: BusinessImportBlocker | null;
  /** Set exactly when the blocker is `apply_rehearsal_failed`. */
  rehearsalFailure: BusinessImportRehearsalFailure | null;
}

export interface BusinessImportResult {
  importedRows: number;
  backupId: string;
  applyMode: BusinessImportApplyMode;
}

export interface DiagnosticPackageDownload {
  blob: Blob;
  fileName: string;
  formatVersion: 1;
}

export type StorageCapacityStatus = "healthy" | "low" | "unavailable";

export interface StorageCapacityLocation {
  kind: "database" | "artifacts" | "backups";
  status: StorageCapacityStatus;
  availableBytes: number | null;
  totalBytes: number | null;
  sharedVolume: boolean;
}

export interface StorageCapacityResult {
  checkedAt: string;
  thresholdGiB: number;
  locations: StorageCapacityLocation[];
}

export type StorageCapacityScope =
  | "database"
  | "artifacts"
  | "backups"
  | "database+artifacts"
  | "database+backups"
  | "artifacts+backups"
  | "database+artifacts+backups";

export interface StorageCapacityHistoryPoint {
  scope: StorageCapacityScope;
  checkedAt: string;
  availableBytes: number;
  totalBytes: number;
  thresholdBytes: number;
  status: "healthy" | "low";
}

export interface StorageCapacityHistoryResult {
  from: string;
  to: string;
  points: StorageCapacityHistoryPoint[];
}
