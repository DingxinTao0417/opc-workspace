import {
  AlertCircle,
  Archive,
  CalendarClock,
  DatabaseBackup,
  Download,
  FileJson,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  Trash2,
  Undo2,
  Upload,
} from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError } from "../api/client";
import { requestApplicationRestart } from "../api/desktop";
import {
  useBackupsQuery,
  useApplyBusinessDataImport,
  useApplyBusinessPackageImport,
  useCreateBackup,
  useDeleteBackup,
  useDownloadBackupArchive,
  useDrillBackupRestore,
  useExportBusinessData,
  useExportBusinessPackage,
  usePreviewBusinessDataImport,
  usePreviewBusinessPackageImport,
  useRestoreDiagnosticsQuery,
  useScheduledBackupPolicyQuery,
  useScheduleBackupRestore,
  useUpdateScheduledBackupPolicy,
  useVerifyBackup,
} from "../api/hooks";
import type {
  BackupSummary,
  BusinessImportPreview,
  BusinessImportResolutionRule,
  BusinessImportResult,
  BusinessPackageImportPreview,
  ScheduledBackupPolicy,
} from "../types/models";
import { dataBackupSettingsHandoff } from "../lib/aiIssueHandoff";
import { useUiStore } from "../store/ui";
import { AiIssueHandoffButton } from "./AiWorkbenchHandoff";

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
  }
  return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GiB`;
}

function formatLocalTime(value: string): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "时间未知";
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(parsed);
}

type ImportPreview = BusinessImportPreview | BusinessPackageImportPreview;

const importTableLabels: Record<string, string> = {
  actors: "人员",
  agent_adapters: "Agent 适配器",
  app_settings: "设置",
  automation_rules: "自动化规则",
  automation_runs: "自动化运行记录",
  clients: "客户",
  client_activities: "客户动态",
  client_actor_links: "客户联系人",
  client_attachments: "客户附件",
  client_followups: "客户回访",
  projects: "项目",
  project_attachments: "项目附件",
  project_notes: "项目笔记",
  tasks: "任务",
  tags: "标签",
  task_tags: "任务标签",
  task_artifacts: "任务产出",
  task_assignments: "任务分派",
  task_saved_views: "保存视图",
  task_submissions: "提交记录",
  focus_sessions: "专注记录",
  focus_session_intervals: "专注时段",
  inbox_items: "收件箱",
  inbox_item_tasks: "收件箱转任务",
  invoices: "发票",
  invoice_pdf_assets: "发票 PDF",
  financial_entries: "收支记录",
  reminders: "提醒",
  roadmap_milestones: "路线图",
  roadmap_milestone_projects: "路线图项目",
  content_items: "内容日历",
  content_item_tasks: "内容日历任务",
  workflow_events: "历史事件",
  workspace_avatars: "工作区头像",
};

const importUpgradeChangeLabels: Record<string, string> = {
  add_column: "补入新列",
};

const importResolutionLabels: Record<BusinessImportResolutionRule, string> = {
  keep_target: "保留目标现有内容",
  skip_identical: "与目标完全相同，跳过",
};

/** Unique values a person can recognise; any other rule shows its index name. */
const importUniqueValueLabels: Record<string, string> = {
  "tags:sqlite_autoindex_tags_2": "名称",
  "invoices:sqlite_autoindex_invoices_2": "发票号",
  "task_saved_views:idx_task_saved_views_name": "名称",
};

const historicalReminder = (what: string, since: string) =>
  `源数据里有“${what}”提醒的历史记录，而${since}。这类历史提醒目前还不能导入。`;

/**
 * Why the database would reject the rows, by the token its rule aborts with.
 * An Inbox reminder is only accepted while its source is still in that state.
 */
const importRehearsalReasons: Record<string, string> = {
  TASK_DUE_INBOX_SOURCE_NOT_CURRENT: historicalReminder(
    "任务到期",
    "对应任务之后已完成、取消或改期",
  ),
  INVALID_TASK_DUE_INBOX_SOURCE: historicalReminder(
    "任务到期",
    "对应任务之后已被删除，或记录本身不符合规则",
  ),
  TASK_BLOCKED_INBOX_SOURCE_NOT_CURRENT: historicalReminder(
    "任务阻塞",
    "对应任务之后已解除阻塞",
  ),
  INVALID_TASK_BLOCKED_INBOX_SOURCE: historicalReminder(
    "任务阻塞",
    "对应任务之后已被删除，或记录本身不符合规则",
  ),
  CONTENT_ITEM_INBOX_SOURCE_NOT_FOUND: historicalReminder(
    "内容日历",
    "对应内容之后已改变状态或版本",
  ),
  INVALID_CONTENT_ITEM_INBOX_SOURCE: historicalReminder(
    "内容日历",
    "对应内容之后已被删除，或记录本身不符合规则",
  ),
  ROADMAP_MILESTONE_INBOX_SOURCE_NOT_FOUND: historicalReminder(
    "路线图里程碑",
    "对应里程碑之后已改变状态或版本",
  ),
  INVALID_ROADMAP_MILESTONE_INBOX_SOURCE: historicalReminder(
    "路线图里程碑",
    "对应里程碑之后已被删除，或记录本身不符合规则",
  ),
  FOREIGN_KEY_CONSTRAINT: "源数据里有记录引用了并不存在的对象。",
  UNIQUE_CONSTRAINT: "源数据里有记录会与已有内容的唯一值重复。",
};

/** The package comes from an older schema and is converted before it is applied. */
function importNeedsUpgrade(preview: ImportPreview): boolean {
  return preview.schemaVersion < preview.targetSchemaVersion;
}

function ImportUpgradeDetails({ preview }: { preview: ImportPreview }) {
  if (
    !importNeedsUpgrade(preview) ||
    preview.blocker === "source_schema_older"
  ) {
    return null;
  }
  return (
    <div className="settings-import-conflicts">
      <small>
        源数据来自 schema v{preview.schemaVersion}，会先在内存中转换为当前 v
        {preview.targetSchemaVersion} 结构再校验，源文件不会被修改
        {preview.upgradeSteps.length === 0
          ? "；可导出数据的结构在这两个版本之间没有变化。"
          : "。"}
      </small>
      {preview.upgradeSteps.length !== 0 ? (
        <ul aria-label="升级转换步骤">
          {preview.upgradeSteps.map((step) => (
            <li
              key={`${step.schemaVersion}:${step.table}:${step.change}:${step.column}`}
            >
              <span>{importTableLabels[step.table] ?? step.table}</span>
              <span>
                v{step.schemaVersion} ·{" "}
                {importUpgradeChangeLabels[step.change] ?? step.change}{" "}
                {step.column}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ImportResolutionDetails({ preview }: { preview: ImportPreview }) {
  // A blocked import writes nothing at all, so there is nothing to single out.
  if (!preview.canApply || preview.resolutions.length === 0) return null;
  const skippedFiles = "skippedFiles" in preview ? preview.skippedFiles : 0;
  return (
    <div className="settings-import-conflicts">
      <small>
        下列源数据不会写入，目标工作区的对应内容保持不变
        {skippedFiles > 0
          ? `；其中 ${skippedFiles} 个受控文件已在目标中，不会重复写入`
          : ""}
        。
      </small>
      <ul aria-label="不写入的源数据">
        {preview.resolutions.map((resolution) => (
          <li key={`${resolution.table}:${resolution.rule}`}>
            <span>
              {importTableLabels[resolution.table] ?? resolution.table}
            </span>
            <span>
              {importResolutionLabels[resolution.rule]} · {resolution.rows} 行
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ImportBlockerDetails({ preview }: { preview: ImportPreview }) {
  if (preview.blocker === "source_schema_older") {
    return (
      <small className="settings-import-blocker">
        源数据为 schema v{preview.schemaVersion}，当前工作区为 v
        {preview.targetSchemaVersion}
        。这个旧版本不在可升级导入的范围内；请先用较新版本的应用打开原工作区并重新导出。数据没有改变。
      </small>
    );
  }
  if (preview.blocker === "source_schema_newer") {
    return (
      <small className="settings-import-blocker">
        源数据为较新的 schema v{preview.schemaVersion}，当前应用只支持 v
        {preview.targetSchemaVersion}。请升级应用后重试，数据没有改变。
      </small>
    );
  }
  if (preview.blocker === "target_file_conflicts") {
    const fileConflicts =
      "fileConflicts" in preview ? preview.fileConflicts : 0;
    return (
      <small className="settings-import-blocker">
        受控存储中已有 {fileConflicts}
        个同名对象；为避免覆盖，本次导入已在写入前阻断。
      </small>
    );
  }
  if (preview.blocker === "apply_rehearsal_failed") {
    const code = preview.rehearsalFailure?.code ?? "";
    return (
      <small className="settings-import-blocker">
        按当前工作区的数据规则，这个包在写入时会被拒绝，所以不能导入。已在创建备份前阻断，数据没有改变。
        {importRehearsalReasons[code] ?? "数据库拒绝了其中的记录。"}
        规则代码 <code>{code}</code>
      </small>
    );
  }
  if (preview.blocker === "target_semantic_conflicts") {
    return (
      <div className="settings-import-conflicts">
        <small className="settings-import-blocker">
          源数据中有 {preview.semanticConflicts}{" "}
          行会与当前工作区已有内容的唯一值重复（例如同名标签、相同发票号）。导入不会覆盖、合并或自动改名，已在创建备份前阻断，数据没有改变。
        </small>
        <ul aria-label="唯一值冲突清单">
          {preview.semanticConflictRules.map((rule) => (
            <li key={`${rule.table}:${rule.index}`}>
              <span>{importTableLabels[rule.table] ?? rule.table}</span>
              <span>
                {importUniqueValueLabels[`${rule.table}:${rule.index}`] ??
                  rule.index}{" "}
                · 重复 {rule.conflicts}
              </span>
            </li>
          ))}
        </ul>
      </div>
    );
  }
  if (preview.blocker !== "target_key_conflicts") return null;
  return (
    <div className="settings-import-conflicts">
      <small className="settings-import-blocker">
        当前工作区已有 {preview.targetRows} 行业务事实；检测到{" "}
        {preview.keyConflicts}{" "}
        条主键重叠且内容不同。冲突事实不会被覆盖、跳过或自动改名。
      </small>
      <ul aria-label="导入冲突清单">
        {preview.conflictTables.map((table) => (
          <li key={table.table}>
            <span>{importTableLabels[table.table] ?? table.table}</span>
            <span>
              源 {table.incomingRows} · 目标 {table.targetRows} · 重叠{" "}
              {table.keyConflicts}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ImportAppendDetails({ preview }: { preview: ImportPreview }) {
  if (!preview.canApply || preview.applyMode !== "append") return null;
  return (
    <div className="settings-import-conflicts">
      <small>
        当前工作区已有 {preview.targetRows}
        行业务事实，预检未发现主键重叠或唯一值重复。本次只追加源数据，并保留目标名称、头像和设置等既有事实；只有未修改的默认自动化会接收源配置。应用前仍会创建完整回滚备份。
      </small>
      <ul aria-label="目标工作区数据清单">
        {preview.conflictTables.map((table) => (
          <li key={table.table}>
            <span>{importTableLabels[table.table] ?? table.table}</span>
            <span>
              源 {table.incomingRows} · 目标 {table.targetRows} · 重叠 0
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function backupErrorText(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof ApiError) {
    const withRequest = (message: string) =>
      error.requestId ? `${message} · 请求 ${error.requestId}` : message;
    if (error.code === "BACKUP_SPACE_INSUFFICIENT") {
      return "备份位置可用空间不足，请清理备份位置或旧备份后重试。";
    }
    if (error.code === "BACKUP_CAPACITY_UNAVAILABLE") {
      return "暂时无法确认备份容量，请刷新容量状态并确认本地存储可用后重试。";
    }
    if (error.code === "BACKUP_INVALID") {
      return withRequest("备份完整性校验失败，请勿下载或用它恢复数据。");
    }
    if (error.code === "BACKUP_NOT_FOUND") {
      return withRequest("备份不存在或已被删除，请刷新备份列表。");
    }
    if (error.code === "BACKUP_EXPORT_SPACE_INSUFFICIENT") {
      return withRequest(
        "导出完整备份所需的临时空间不足，请清理本地磁盘后重试。",
      );
    }
    if (error.code === "BACKUP_EXPORT_CAPACITY_UNAVAILABLE") {
      return withRequest(
        "暂时无法确认完整备份导出容量，请检查本地存储后重试。",
      );
    }
    if (error.code === "BACKUP_EXPORT_FAILED") {
      return withRequest("无法生成完整备份 ZIP，请重新校验备份后重试。");
    }
    if (error.code === "IDEMPOTENCY_CONFLICT") {
      return "这次创建请求与上一次内容不一致，请重新操作。";
    }
    if (error.code === "BACKUP_NOT_RESTORABLE") {
      return "备份无法在隔离临时数据根完成恢复演练，请勿用它替换当前数据。";
    }
    if (error.code === "RESTORE_CONFIRMATION_REQUIRED") {
      return "必须明确确认后才能安排恢复。";
    }
    if (error.code === "RESTORE_ROLLBACK_BACKUP_FAILED") {
      return "无法为当前数据创建自动回滚点，恢复没有被安排。";
    }
    if (error.code === "RESTORE_ROLLBACK_SPACE_INSUFFICIENT") {
      return "备份位置空间不足，无法同时创建当前数据回滚点并暂存恢复包；恢复没有被安排。";
    }
    if (error.code === "RESTORE_ROLLBACK_CAPACITY_UNAVAILABLE") {
      return "暂时无法确认恢复所需容量，请刷新容量状态并确认本地存储可用后重试。";
    }
    if (error.code === "RESTORE_SCHEDULE_FAILED") {
      return "无法安全挂起恢复，当前数据没有改变。";
    }
    if (error.code === "RESTORE_WORKSPACE_MISMATCH") {
      return "这个备份不属于当前工作区，不能覆盖当前数据。";
    }
    if (error.code === "RESTORE_ALREADY_PENDING") {
      return "已有另一个备份等待恢复，请先关闭并重新打开应用。";
    }
    if (error.code === "RESTORE_RESTART_REQUIRED") {
      return withRequest("恢复已经挂起，请关闭并重新打开应用后继续。");
    }
    if (error.code === "BACKUP_DELETE_UNSAFE") {
      return "备份目录包含不安全的文件系统项，已拒绝删除。";
    }
    if (error.code === "BACKUP_DELETE_FAILED") {
      return "备份删除未完整结束，请对同一备份重试。";
    }
    if (error.code === "DATA_EXPORT_FAILED") {
      return "无法生成一致的业务数据导出，请重试。";
    }
    if (error.code === "DATA_PACKAGE_EXPORT_UNAVAILABLE") {
      return "当前数据目录未配置含文件业务导出。";
    }
    if (error.code === "DATA_PACKAGE_EXPORT_FAILED") {
      return "无法生成完整的含文件业务包；请检查受控文件后重试。";
    }
    if (error.code === "INVALID_IMPORT_JSON") {
      return "所选文件不是有效的 opc-workspace 业务 JSON。";
    }
    if (error.code === "IMPORT_TOO_LARGE") {
      return "导入文件不能超过 16 MiB。";
    }
    if (error.code === "IMPORT_VERSION_UNSUPPORTED") {
      return "导入文件的格式、API 或 schema 版本与当前应用不兼容。";
    }
    if (error.code === "IMPORT_FILES_UNSUPPORTED") {
      return "这个快照包含受控文件；当前 JSON 导入不能恢复文件正文。";
    }
    if (error.code === "IMPORT_PACKAGE_UNAVAILABLE") {
      return "当前数据目录未配置含文件业务包导入。";
    }
    if (error.code === "IMPORT_PACKAGE_TOO_LARGE") {
      return "含文件业务包不能超过 2 GiB。";
    }
    if (error.code === "INVALID_IMPORT_PACKAGE") {
      return "所选文件不是有效的 opc-workspace 业务 ZIP。";
    }
    if (
      error.code === "IMPORT_PACKAGE_MANIFEST_INVALID" ||
      error.code === "IMPORT_PACKAGE_FILE_INVALID"
    ) {
      return "含文件业务包的清单、业务数据或文件完整性校验失败。";
    }
    if (error.code === "IMPORT_PACKAGE_APPLY_FAILED") {
      return "含文件业务包未能原子应用；业务数据和受控文件均未导入。";
    }
    if (error.code === "IMPORT_ACTIVE_FOCUS_UNSUPPORTED") {
      return "源工作区仍有活动专注，请先结束或取消后重新导出。";
    }
    if (
      error.code === "IMPORT_MANIFEST_INVALID" ||
      error.code === "IMPORT_SCHEMA_MISMATCH" ||
      error.code === "IMPORT_ROW_INVALID"
    ) {
      return "导入文件结构不完整或已被修改，已拒绝应用。";
    }
    if (error.code === "IMPORT_TARGET_CONFLICT") {
      return "预检后目标数据发生变化或仍有主键冲突；没有覆盖或追加任何业务事实。";
    }
    if (error.code === "IMPORT_TARGET_SEMANTIC_CONFLICT") {
      return "预检后目标数据发生变化或仍有唯一值重复；没有覆盖或追加任何业务事实。";
    }
    if (error.code === "IMPORT_REHEARSAL_FAILED") {
      return "按当前数据规则，这个包在写入时会被拒绝；没有创建备份或写入数据，请重新选择文件查看预检说明。";
    }
    if (error.code === "IMPORT_PLAN_CHANGED") {
      return "预检之后目标数据或导入方案已变化；没有创建备份或写入数据，请重新选择文件并检查最新预检结果。";
    }
    if (error.code === "IMPORT_TARGET_FILE_CONFLICT") {
      return "受控存储中已有同名对象；没有覆盖文件或追加业务事实。";
    }
    if (error.code === "IMPORT_CONFIRMATION_REQUIRED") {
      return "导入模式已变化，请重新选择文件、检查最新预检结果后确认。";
    }
    if (error.code === "IMPORT_BACKUP_FAILED") {
      return "无法创建导入前回滚备份，现有数据没有改变。";
    }
    if (error.code === "IMPORT_BACKUP_SPACE_INSUFFICIENT") {
      return "备份位置空间不足，无法创建导入前回滚备份；现有数据没有改变。";
    }
    if (error.code === "IMPORT_BACKUP_CAPACITY_UNAVAILABLE") {
      return "暂时无法确认导入前回滚备份容量；现有数据没有改变，请检查本地存储后重试。";
    }
    if (error.code === "IMPORT_APPLY_FAILED") {
      return "导入数据未通过完整性校验，整批内容已回滚。";
    }
    return error.requestId
      ? `${error.message} · 请求 ${error.requestId}`
      : error.message;
  }
  return "本地备份操作失败，请重试。";
}

function BackupCard({
  backup,
  busy,
  drillingId,
  exportingId,
  verifyingId,
  onDrill,
  onExport,
  onDelete,
  onRestore,
  onVerify,
}: {
  backup: BackupSummary;
  busy: boolean;
  drillingId: string | null;
  exportingId: string | null;
  verifyingId: string | null;
  onDrill: (id: string) => void;
  onExport: (id: string) => void;
  onDelete: (backup: BackupSummary) => void;
  onRestore: (backup: BackupSummary) => void;
  onVerify: (id: string) => void;
}) {
  const invalid = backup.verificationStatus === "invalid";
  const verifying = verifyingId === backup.id;
  const drilling = drillingId === backup.id;
  const exporting = exportingId === backup.id;
  return (
    <article
      className="settings-backup-card"
      data-status={backup.verificationStatus}
    >
      <div className="settings-backup-card-icon">
        {invalid ? <AlertCircle size={17} /> : <Archive size={17} />}
      </div>
      <div className="settings-backup-card-body">
        <div className="settings-backup-card-title">
          <strong>
            {backup.note ||
              (backup.kind === "scheduled" ? "计划备份" : "手动备份")}
          </strong>
          <span>{formatLocalTime(backup.createdAt)}</span>
        </div>
        {invalid ? (
          <p className="settings-backup-error">
            {backup.error || "备份清单无效"}
          </p>
        ) : (
          <p>
            schema v{backup.schemaVersion} · {backup.artifactCount} 个文件 ·{" "}
            {formatBytes(backup.totalBytes)}
          </p>
        )}
        <small title={backup.id}>ID {backup.id.slice(0, 8)}</small>
      </div>
      <div className="settings-backup-card-actions">
        <button
          aria-label={`下载完整备份 ${backup.id.slice(0, 8)}`}
          className="button button-secondary"
          disabled={busy || invalid}
          onClick={() => onExport(backup.id)}
          type="button"
        >
          {exporting ? (
            <LoaderCircle className="animate-spin" size={13} />
          ) : (
            <Download size={13} />
          )}
          {exporting ? "正在导出…" : "下载完整备份"}
        </button>
        <button
          aria-label={`重新校验备份 ${backup.id.slice(0, 8)}`}
          className="button button-secondary settings-backup-verify"
          disabled={busy}
          onClick={() => onVerify(backup.id)}
          type="button"
        >
          {verifying ? (
            <LoaderCircle className="animate-spin" size={13} />
          ) : (
            <ShieldCheck size={13} />
          )}
          {verifying ? "校验中…" : "重新校验"}
        </button>
        <button
          aria-label={`恢复演练备份 ${backup.id.slice(0, 8)}`}
          className="button button-secondary settings-backup-drill"
          disabled={busy || invalid}
          onClick={() => onDrill(backup.id)}
          type="button"
        >
          {drilling ? (
            <LoaderCircle className="animate-spin" size={13} />
          ) : (
            <RotateCcw size={13} />
          )}
          {drilling ? "演练中…" : "恢复演练"}
        </button>
        <button
          aria-label={`恢复备份 ${backup.id.slice(0, 8)}`}
          className="button button-danger settings-backup-restore"
          disabled={busy || invalid}
          onClick={() => onRestore(backup)}
          type="button"
        >
          <Undo2 size={13} />
          恢复此备份
        </button>
        <button
          aria-label={`删除备份 ${backup.id.slice(0, 8)}`}
          className="button button-quiet settings-backup-delete"
          disabled={busy}
          onClick={() => onDelete(backup)}
          type="button"
        >
          <Trash2 size={13} />
          删除备份
        </button>
      </div>
    </article>
  );
}

interface BackupSettingsProps {
  storageSettings?: ReactNode;
}

export function BackupSettings({ storageSettings }: BackupSettingsProps = {}) {
  const backupsQuery = useBackupsQuery();
  const scheduledPolicyQuery = useScheduledBackupPolicyQuery();
  const updateScheduledPolicyMutation = useUpdateScheduledBackupPolicy();
  const restoreDiagnosticsQuery = useRestoreDiagnosticsQuery();
  const createMutation = useCreateBackup();
  const verifyMutation = useVerifyBackup();
  const drillMutation = useDrillBackupRestore();
  const restoreMutation = useScheduleBackupRestore();
  const deleteMutation = useDeleteBackup();
  const backupArchiveMutation = useDownloadBackupArchive();
  const exportMutation = useExportBusinessData();
  const packageExportMutation = useExportBusinessPackage();
  // Applying an import remounts the app once its settings are re-read, which
  // would lose a message kept here and send the dialog back to its first
  // section. The outcome therefore lives in the UI store, and the dialog is
  // told to stay on this section, before the refresh starts.
  const importNotice = useUiStore((state) => state.dataImportNotice);
  const setImportNotice = useUiStore((state) => state.setDataImportNotice);
  const reportImport = (message: string) => {
    setImportNotice(message);
    const { settingsOpen, setSettingsOpen } = useUiStore.getState();
    if (settingsOpen) setSettingsOpen(true, "data");
  };
  const importedRowsText = (result: BusinessImportResult) =>
    `${result.applyMode === "append" ? "已完成追加导入，处理" : "已导入"} ${result.importedRows} 行源业务数据`;
  const importPreviewMutation = usePreviewBusinessDataImport();
  const importApplyMutation = useApplyBusinessDataImport({
    onApplied: (result) =>
      reportImport(
        `${importedRowsText(result)}；导入前回滚备份为 ${result.backupId.slice(0, 8)}。`,
      ),
  });
  const packageImportPreviewMutation = usePreviewBusinessPackageImport();
  const packageImportApplyMutation = useApplyBusinessPackageImport({
    onApplied: (result) =>
      reportImport(
        `${importedRowsText(result)}和 ${result.importedFiles} 个受控文件；导入前回滚备份为 ${result.backupId.slice(0, 8)}。`,
      ),
  });
  const [importFile, setImportFile] = useState<File | null>(null);
  const [packageImportFile, setPackageImportFile] = useState<File | null>(null);
  const [note, setNote] = useState("");
  const [verifyingId, setVerifyingId] = useState<string | null>(null);
  const [drillingId, setDrillingId] = useState<string | null>(null);
  const [exportingId, setExportingId] = useState<string | null>(null);
  const [restoreCandidate, setRestoreCandidate] =
    useState<BackupSummary | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<BackupSummary | null>(
    null,
  );
  const [scheduledRestore, setScheduledRestore] = useState<{
    backupId: string;
    rollbackBackupId: string;
  } | null>(null);
  const [localSuccess, setLocalSuccess] = useState<string | null>(null);
  const success = localSuccess ?? importNotice;
  const successRef = useRef<HTMLParagraphElement | null>(null);
  useEffect(() => {
    // The remount leaves the dialog at the top of this section, above the
    // message that says what the import did.
    if (importNotice) {
      successRef.current?.scrollIntoView?.({ block: "nearest" });
    }
  }, [importNotice]);
  // Every action replaces whatever the previous one reported.
  const setSuccess = (message: string | null) => {
    setLocalSuccess(message);
    setImportNotice(null);
  };
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [restartError, setRestartError] = useState<string | null>(null);
  const [restarting, setRestarting] = useState(false);
  const [policyDraft, setPolicyDraft] = useState<ScheduledBackupPolicy | null>(
    null,
  );
  const [policyDirty, setPolicyDirty] = useState(false);
  useEffect(() => {
    if (scheduledPolicyQuery.data && !policyDirty) {
      setPolicyDraft(scheduledPolicyQuery.data);
    }
  }, [policyDirty, scheduledPolicyQuery.data]);
  const pending =
    createMutation.isPending ||
    verifyMutation.isPending ||
    drillMutation.isPending ||
    restoreMutation.isPending ||
    deleteMutation.isPending ||
    backupArchiveMutation.isPending ||
    exportMutation.isPending ||
    packageExportMutation.isPending ||
    importPreviewMutation.isPending ||
    importApplyMutation.isPending ||
    packageImportPreviewMutation.isPending ||
    packageImportApplyMutation.isPending ||
    updateScheduledPolicyMutation.isPending;
  const restoreDiagnostics = restoreDiagnosticsQuery.data;
  const diagnosticRestore =
    restoreDiagnostics?.restartRequired &&
    restoreDiagnostics.backupId &&
    restoreDiagnostics.rollbackBackupId
      ? {
          backupId: restoreDiagnostics.backupId,
          rollbackBackupId: restoreDiagnostics.rollbackBackupId,
        }
      : null;
  const restoreReady = scheduledRestore ?? diagnosticRestore;
  const locked = pending || restoreReady !== null;
  const handoffContent = dataBackupSettingsHandoff({
    backupCount: backupsQuery.data?.length ?? 0,
    policyEnabled: policyDraft?.enabled ?? null,
    policyStatus: policyDraft?.lastStatus ?? "unknown",
    restoreStatus: restoreDiagnostics?.status ?? "unknown",
  });
  const mutationError =
    createMutation.error ??
    verifyMutation.error ??
    drillMutation.error ??
    restoreMutation.error ??
    deleteMutation.error ??
    backupArchiveMutation.error ??
    exportMutation.error ??
    packageExportMutation.error ??
    importPreviewMutation.error ??
    importApplyMutation.error ??
    packageImportPreviewMutation.error ??
    packageImportApplyMutation.error ??
    downloadError;

  const changePolicyDraft = (
    update: Partial<
      Pick<
        ScheduledBackupPolicy,
        "enabled" | "localTime" | "timezone" | "retentionCount"
      >
    >,
  ) => {
    setPolicyDraft((current) =>
      current ? { ...current, ...update } : current,
    );
    setPolicyDirty(true);
    setSuccess(null);
    updateScheduledPolicyMutation.reset();
  };

  const cancelPolicyDraft = () => {
    setPolicyDraft(scheduledPolicyQuery.data ?? null);
    setPolicyDirty(false);
    updateScheduledPolicyMutation.reset();
  };

  const savePolicyDraft = () => {
    if (!policyDraft || !policyDirty) return;
    updateScheduledPolicyMutation.mutate(
      {
        enabled: policyDraft.enabled,
        localTime: policyDraft.localTime,
        timezone: policyDraft.timezone.trim(),
        retentionCount: policyDraft.retentionCount,
        expectedVersion: policyDraft.version,
      },
      {
        onSuccess: (saved) => {
          setPolicyDraft(saved);
          setPolicyDirty(false);
          setSuccess(
            saved.enabled
              ? `计划备份已启用：每天 ${saved.localTime}（${saved.timezone}），保留最近 ${saved.retentionCount} 份自动备份。`
              : "计划备份已关闭；已有备份不会被删除。",
          );
        },
      },
    );
  };

  const resetExportFeedback = () => {
    setDownloadError(null);
    backupArchiveMutation.reset();
    exportMutation.reset();
    packageExportMutation.reset();
  };

  const create = () => {
    setSuccess(null);
    resetExportFeedback();
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    createMutation.mutate(
      { note: note.trim() },
      {
        onSuccess: (backup) => {
          setNote("");
          setSuccess(
            `备份已创建并校验：${backup.artifactCount} 个受控文件，${formatBytes(backup.totalBytes)}。`,
          );
        },
      },
    );
  };

  const verify = (id: string) => {
    setSuccess(null);
    resetExportFeedback();
    setVerifyingId(id);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    verifyMutation.mutate(id, {
      onSuccess: (backup) =>
        setSuccess(`备份 ${backup.id.slice(0, 8)} 完整性校验通过。`),
      onSettled: () => setVerifyingId(null),
    });
  };

  const drill = (id: string) => {
    setSuccess(null);
    resetExportFeedback();
    setDrillingId(id);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    drillMutation.mutate(id, {
      onSuccess: (result) =>
        setSuccess(
          `备份 ${result.backupId.slice(0, 8)} 已在隔离临时数据根完成恢复演练：schema v${result.sourceSchemaVersion} → v${result.resultSchemaVersion}，${result.artifactCount} 个受控文件。`,
        ),
      onSettled: () => setDrillingId(null),
    });
  };

  const scheduleRestore = () => {
    if (!restoreCandidate) return;
    setSuccess(null);
    resetExportFeedback();
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    restoreMutation.mutate(restoreCandidate.id, {
      onSuccess: (result) => {
        setScheduledRestore({
          backupId: result.backupId,
          rollbackBackupId: result.rollbackBackupId,
        });
        setRestoreCandidate(null);
      },
    });
  };

  const chooseRestore = (backup: BackupSummary) => {
    setDeleteCandidate(null);
    setRestoreCandidate(backup);
    setSuccess(null);
    resetExportFeedback();
    deleteMutation.reset();
  };

  const chooseDelete = (backup: BackupSummary) => {
    setRestoreCandidate(null);
    setDeleteCandidate(backup);
    setSuccess(null);
    resetExportFeedback();
    restoreMutation.reset();
    deleteMutation.reset();
  };

  const deleteSelectedBackup = () => {
    if (!deleteCandidate) return;
    const id = deleteCandidate.id;
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    deleteMutation.mutate(id, {
      onSuccess: () => {
        setDeleteCandidate(null);
        setSuccess(`备份 ${id.slice(0, 8)} 已永久删除。`);
      },
    });
  };

  const exportBackupArchive = (id: string) => {
    setSuccess(null);
    resetExportFeedback();
    setExportingId(id);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    backupArchiveMutation.mutate(id, {
      onSuccess: (result) => {
        if (
          typeof URL === "undefined" ||
          typeof URL.createObjectURL !== "function"
        ) {
          setDownloadError(
            "当前运行环境不支持保存完整备份，请在桌面应用中重试。",
          );
          return;
        }
        const url = URL.createObjectURL(result.blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = result.fileName;
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        window.setTimeout(() => URL.revokeObjectURL(url), 0);
        setSuccess(`敏感的完整本机备份已保存：${result.fileName}`);
      },
      onSettled: () => setExportingId(null),
    });
  };

  const exportBusinessData = () => {
    setSuccess(null);
    resetExportFeedback();
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    exportMutation.mutate(undefined, {
      onSuccess: (result) => {
        if (typeof URL.createObjectURL !== "function") {
          setDownloadError(
            "当前运行环境不支持保存导出文件，请在桌面应用中重试。",
          );
          return;
        }
        const url = URL.createObjectURL(result.blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = result.fileName;
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        window.setTimeout(() => URL.revokeObjectURL(url), 0);
        setSuccess(`业务数据已导出：${result.fileName}`);
      },
    });
  };

  const exportBusinessPackage = () => {
    setSuccess(null);
    resetExportFeedback();
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    deleteMutation.reset();
    packageExportMutation.mutate(undefined, {
      onSuccess: (result) => {
        if (typeof URL.createObjectURL !== "function") {
          setDownloadError(
            "当前运行环境不支持保存导出文件，请在桌面应用中重试。",
          );
          return;
        }
        const url = URL.createObjectURL(result.blob);
        const anchor = document.createElement("a");
        anchor.href = url;
        anchor.download = result.fileName;
        anchor.rel = "noopener";
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
        window.setTimeout(() => URL.revokeObjectURL(url), 0);
        setSuccess(`含文件业务包已导出：${result.fileName}`);
      },
    });
  };

  const chooseImportFile = (file: File | null) => {
    setSuccess(null);
    setDownloadError(null);
    importApplyMutation.reset();
    importPreviewMutation.reset();
    packageImportApplyMutation.reset();
    packageImportPreviewMutation.reset();
    setPackageImportFile(null);
    setImportFile(file);
    if (!file) return;
    if (file.size > 16 * 1024 * 1024) {
      setImportFile(null);
      setDownloadError("导入文件不能超过 16 MiB。");
      return;
    }
    importPreviewMutation.mutate(file);
  };

  const choosePackageImportFile = (file: File | null) => {
    setSuccess(null);
    setDownloadError(null);
    importApplyMutation.reset();
    importPreviewMutation.reset();
    packageImportApplyMutation.reset();
    packageImportPreviewMutation.reset();
    setImportFile(null);
    setPackageImportFile(file);
    if (!file) return;
    if (file.size > 2 * 1024 * 1024 * 1024) {
      setPackageImportFile(null);
      setDownloadError("含文件业务包不能超过 2 GiB。");
      return;
    }
    packageImportPreviewMutation.mutate(file);
  };

  const applyBusinessImport = () => {
    if (!importFile || !importPreviewMutation.data?.canApply) return;
    setSuccess(null);
    importApplyMutation.reset();
    importApplyMutation.mutate(
      {
        file: importFile,
        applyMode: importPreviewMutation.data.applyMode!,
        upgrade: importNeedsUpgrade(importPreviewMutation.data),
        planDigest: importPreviewMutation.data.planDigest,
      },
      {
        onSuccess: () => {
          setImportFile(null);
          importPreviewMutation.reset();
        },
      },
    );
  };

  const applyBusinessPackageImport = () => {
    if (!packageImportFile || !packageImportPreviewMutation.data?.canApply)
      return;
    setSuccess(null);
    packageImportApplyMutation.reset();
    packageImportApplyMutation.mutate(
      {
        file: packageImportFile,
        applyMode: packageImportPreviewMutation.data.applyMode!,
        upgrade: importNeedsUpgrade(packageImportPreviewMutation.data),
        planDigest: packageImportPreviewMutation.data.planDigest,
      },
      {
        onSuccess: () => {
          setPackageImportFile(null);
          packageImportPreviewMutation.reset();
        },
      },
    );
  };

  const restartApplication = async () => {
    setRestartError(null);
    setRestarting(true);
    try {
      const requested = await requestApplicationRestart();
      if (!requested) {
        setRestartError(
          "浏览器开发模式不会接管 Sidecar，请手动停止并重新启动本地服务。",
        );
        setRestarting(false);
      }
    } catch (error) {
      setRestartError(
        error instanceof Error
          ? error.message
          : "桌面应用无法安全重启，请手动关闭后重新打开。",
      );
      setRestarting(false);
    }
  };

  return (
    <>
      <header className="settings-content-header">
        <h3>数据与备份</h3>
        <p>创建 SQLite 与受控文件处于同一写入边界的本地备份。</p>
        <AiIssueHandoffButton content={handoffContent} disabled={locked} />
      </header>

      {storageSettings}

      <div className="settings-group settings-backup-schedule">
        <div className="settings-backup-intro">
          <CalendarClock size={18} />
          <div>
            <strong>计划备份与自动保留</strong>
            <span>
              每个本地日期最多尝试一次；错过时间后会在下一次应用启动或后台扫描时补偿执行。
            </span>
          </div>
        </div>
        {scheduledPolicyQuery.isError ? (
          <div className="settings-state settings-state-error" role="alert">
            <AlertCircle size={16} />
            <div>
              <strong>无法读取计划备份策略</strong>
              <span>{backupErrorText(scheduledPolicyQuery.error)}</span>
            </div>
            <button
              className="button button-secondary"
              onClick={() => void scheduledPolicyQuery.refetch()}
              type="button"
            >
              重试
            </button>
          </div>
        ) : scheduledPolicyQuery.isPending || !policyDraft ? (
          <div aria-live="polite" className="settings-state" role="status">
            <LoaderCircle className="animate-spin" size={16} />
            正在读取计划备份策略…
          </div>
        ) : (
          <>
            <div className="settings-toggle-row">
              <div>
                <div className="settings-label settings-toggle-label">
                  启用每日计划
                </div>
                <p>点击立即预览；只有保存后后台任务才会按新策略运行。</p>
              </div>
              <button
                aria-checked={policyDraft.enabled}
                aria-label="启用每日计划备份"
                className="settings-toggle"
                data-checked={policyDraft.enabled}
                disabled={locked}
                onClick={() =>
                  changePolicyDraft({ enabled: !policyDraft.enabled })
                }
                role="switch"
                type="button"
              >
                <span />
              </button>
            </div>
            <div className="settings-backup-policy-grid">
              <label>
                <span className="settings-label">每日时间</span>
                <input
                  aria-label="每日备份时间"
                  className="settings-text-input"
                  disabled={locked}
                  onInput={(event) =>
                    changePolicyDraft({ localTime: event.currentTarget.value })
                  }
                  type="time"
                  value={policyDraft.localTime}
                />
              </label>
              <label>
                <span className="settings-label">IANA 时区</span>
                <input
                  aria-label="计划备份时区"
                  className="settings-text-input"
                  disabled={locked}
                  maxLength={100}
                  onChange={(event) =>
                    changePolicyDraft({ timezone: event.target.value })
                  }
                  placeholder="Asia/Shanghai"
                  value={policyDraft.timezone}
                />
              </label>
              <label>
                <span className="settings-label">自动备份保留份数</span>
                <input
                  aria-label="自动备份保留份数"
                  className="settings-text-input"
                  disabled={locked}
                  max={365}
                  min={1}
                  onChange={(event) =>
                    changePolicyDraft({
                      retentionCount: Number(event.target.value),
                    })
                  }
                  type="number"
                  value={policyDraft.retentionCount}
                />
              </label>
            </div>
            <p className="settings-backup-policy-preview" role="status">
              {policyDraft.enabled
                ? "预览：每天 " +
                  (policyDraft.localTime || "--:--") +
                  "（" +
                  (policyDraft.timezone || "未设置时区") +
                  "）创建完整校验备份，仅自动清理超过 " +
                  (policyDraft.retentionCount || 0) +
                  " 份的计划备份；手工及回滚备份不受影响。"
                : "预览：关闭计划执行，不创建或自动清理任何备份。"}
            </p>
            {policyDraft.lastStatus === "succeeded" &&
            policyDraft.lastSuccessAt ? (
              <small>
                最近成功：{formatLocalTime(policyDraft.lastSuccessAt)} · 备份{" "}
                {policyDraft.lastBackupId?.slice(0, 8) ?? "未知"}
              </small>
            ) : policyDraft.lastStatus === "failed" ? (
              <small className="settings-backup-error">
                最近一次计划执行失败：
                {policyDraft.lastErrorCode ?? "未知错误"}。现有备份未被覆盖。
              </small>
            ) : (
              <small>尚无计划备份执行记录。</small>
            )}
            {updateScheduledPolicyMutation.error ? (
              <p className="form-error settings-backup-feedback" role="alert">
                {backupErrorText(updateScheduledPolicyMutation.error)}
              </p>
            ) : null}
            <div className="settings-backup-policy-actions">
              <button
                className="button button-secondary"
                disabled={
                  !policyDirty || updateScheduledPolicyMutation.isPending
                }
                onClick={cancelPolicyDraft}
                type="button"
              >
                取消
              </button>
              <button
                className="button button-primary"
                disabled={
                  !policyDirty ||
                  updateScheduledPolicyMutation.isPending ||
                  !policyDraft.localTime ||
                  !policyDraft.timezone.trim() ||
                  policyDraft.retentionCount < 1 ||
                  policyDraft.retentionCount > 365
                }
                onClick={savePolicyDraft}
                type="button"
              >
                {updateScheduledPolicyMutation.isPending ? (
                  <LoaderCircle className="animate-spin" size={13} />
                ) : null}
                {updateScheduledPolicyMutation.isPending
                  ? "正在保存…"
                  : "保存计划"}
              </button>
            </div>
          </>
        )}
      </div>

      <div className="settings-group settings-backup-export">
        <div className="settings-backup-intro">
          {restoreDiagnostics?.attentionRequired ||
          restoreDiagnostics?.cleanupRequired ? (
            <AlertCircle size={18} />
          ) : restoreDiagnosticsQuery.isPending ? (
            <LoaderCircle className="animate-spin" size={18} />
          ) : (
            <ShieldCheck size={18} />
          )}
          <div>
            <strong>启动恢复诊断</strong>
            {restoreDiagnosticsQuery.isPending ? (
              <span>正在读取安全恢复状态…</span>
            ) : restoreDiagnosticsQuery.isError ? (
              <span>恢复诊断暂时不可用；不会据此推断恢复成功。</span>
            ) : restoreDiagnostics?.status === "attention_required" ? (
              <span>
                发现 {restoreDiagnostics.failedAttemptCount} 次失败记录和{" "}
                {restoreDiagnostics.invalidEntryCount}{" "}
                个无效记录；当前数据未被自动清理，请结合运行诊断检查。
              </span>
            ) : restoreDiagnostics?.status === "cleanup_required" ? (
              <span>
                数据恢复已经完成，但有{" "}
                {restoreDiagnostics.residualAppliedCount || 1}{" "}
                项启动清理未完全结束；恢复不会重复执行。
              </span>
            ) : restoreDiagnostics?.status === "restored" ? (
              <span>
                本次启动已完成备份{" "}
                {restoreDiagnostics.backupId?.slice(0, 8) ?? "未知"}{" "}
                的恢复，并通过最终验证。
              </span>
            ) : restoreDiagnostics?.status === "restart_required" ? (
              <span>恢复计划已安全挂起，重启前业务写入保持冻结。</span>
            ) : (
              <span>未发现待应用恢复、失败隔离或残留清理记录。</span>
            )}
          </div>
        </div>
        <button
          className="button button-secondary settings-backup-export-button"
          disabled={restoreDiagnosticsQuery.isFetching}
          onClick={() => void restoreDiagnosticsQuery.refetch()}
          type="button"
        >
          {restoreDiagnosticsQuery.isFetching ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <RefreshCw size={14} />
          )}
          {restoreDiagnosticsQuery.isFetching ? "检查中…" : "重新检查"}
        </button>
      </div>

      <div className="settings-group settings-backup-create">
        <div className="settings-backup-intro">
          <DatabaseBackup size={18} />
          <div>
            <strong>创建已校验备份</strong>
            <span>
              创建期间会短暂暂停业务请求，并校验数据库、身份标记和每个受控文件。
            </span>
          </div>
        </div>
        <label>
          <span className="settings-label">备份说明（可选）</span>
          <input
            aria-label="备份说明"
            className="settings-text-input"
            disabled={locked}
            maxLength={200}
            onChange={(event) => {
              setNote(event.target.value);
              setSuccess(null);
              createMutation.reset();
              resetExportFeedback();
            }}
            placeholder="例如：开始本周工作前"
            value={note}
          />
        </label>
        <button
          className="button button-primary settings-backup-create-button"
          disabled={locked}
          onClick={create}
          type="button"
        >
          {createMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <DatabaseBackup size={14} />
          )}
          {createMutation.isPending ? "创建并校验中…" : "立即备份"}
        </button>
      </div>

      <div className="settings-group settings-backup-export">
        <div className="settings-backup-intro">
          <FileJson size={18} />
          <div>
            <strong>导出业务数据 JSON</strong>
            <span>
              下载版本化业务快照，包含业务表和文件产出元数据，不包含文件正文、会话令牌、机器绝对路径或运行维护表。
            </span>
          </div>
        </div>
        <button
          className="button button-secondary settings-backup-export-button"
          disabled={locked}
          onClick={exportBusinessData}
          type="button"
        >
          {exportMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Download size={14} />
          )}
          {exportMutation.isPending ? "正在生成…" : "下载 JSON"}
        </button>
      </div>

      <div className="settings-group settings-backup-export">
        <div className="settings-backup-intro">
          <Archive size={18} />
          <div>
            <strong>导出含文件业务包</strong>
            <span>
              下载 ZIP，包含版本化业务
              JSON、清单和全部活动受控文件；生成时逐个校验大小与 SHA-256。
            </span>
          </div>
        </div>
        <button
          className="button button-secondary settings-backup-export-button"
          disabled={locked}
          onClick={exportBusinessPackage}
          type="button"
        >
          {packageExportMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Download size={14} />
          )}
          {packageExportMutation.isPending ? "正在打包…" : "下载含文件 ZIP"}
        </button>
      </div>

      <div className="settings-group settings-backup-export">
        <div className="settings-backup-intro">
          <Upload size={18} />
          <div>
            <strong>导入业务数据 JSON</strong>
            <span>
              先预检官方导出文件；受支持的旧版本包会列出升级转换步骤并单独确认，非空工作区会列出目标表、主键重叠和唯一值重复；零冲突时可保留当前事实并安全追加，完全相同的行会跳过。
            </span>
          </div>
        </div>
        <label className="button button-secondary settings-backup-export-button">
          {importPreviewMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Upload size={14} />
          )}
          {importPreviewMutation.isPending ? "正在预检…" : "选择 JSON"}
          <input
            accept="application/json,.json"
            aria-label="选择业务数据 JSON"
            disabled={locked}
            hidden
            onChange={(event) => {
              const file = event.target.files?.[0] ?? null;
              event.target.value = "";
              chooseImportFile(file);
            }}
            type="file"
          />
        </label>
        {importFile && importPreviewMutation.data ? (
          <div className="settings-backup-confirm">
            <FileJson size={18} />
            <div>
              <strong>{importFile.name}</strong>
              <p>
                schema v{importPreviewMutation.data.schemaVersion} · 共{" "}
                {importPreviewMutation.data.totalRows} 行业务数据
              </p>
              <ImportUpgradeDetails preview={importPreviewMutation.data} />
              {!importPreviewMutation.data.canApply ? (
                <ImportBlockerDetails preview={importPreviewMutation.data} />
              ) : importPreviewMutation.data.applyMode === "append" ? (
                <ImportAppendDetails preview={importPreviewMutation.data} />
              ) : (
                <small>
                  应用前会自动创建并校验回滚备份；任一行失败则整批回滚。
                </small>
              )}
              <ImportResolutionDetails preview={importPreviewMutation.data} />
            </div>
            <div className="settings-backup-confirm-actions">
              <button
                className="button button-secondary"
                disabled={importApplyMutation.isPending}
                onClick={() => chooseImportFile(null)}
                type="button"
              >
                取消
              </button>
              <button
                className="button button-danger"
                disabled={
                  !importPreviewMutation.data.canApply ||
                  importApplyMutation.isPending
                }
                onClick={applyBusinessImport}
                type="button"
              >
                {importApplyMutation.isPending ? (
                  <LoaderCircle className="animate-spin" size={13} />
                ) : (
                  <Upload size={13} />
                )}
                {importApplyMutation.isPending
                  ? "正在导入…"
                  : importNeedsUpgrade(importPreviewMutation.data) &&
                      importPreviewMutation.data.canApply
                    ? "确认升级并导入"
                    : "确认导入"}
              </button>
            </div>
          </div>
        ) : null}
      </div>

      <div className="settings-group settings-backup-export">
        <div className="settings-backup-intro">
          <Archive size={18} />
          <div>
            <strong>导入含文件业务包</strong>
            <span>
              选择官方 ZIP 后先校验清单、业务数据和全部文件；预检会分类 schema
              方向、主键、唯一值与文件冲突；零冲突时可安全追加，已存在且完全相同的行和文件会跳过。
            </span>
          </div>
        </div>
        <label className="button button-secondary settings-backup-export-button">
          {packageImportPreviewMutation.isPending ? (
            <LoaderCircle className="animate-spin" size={14} />
          ) : (
            <Upload size={14} />
          )}
          {packageImportPreviewMutation.isPending ? "正在校验…" : "选择 ZIP"}
          <input
            accept="application/zip,.zip"
            aria-label="选择含文件业务 ZIP"
            disabled={locked}
            hidden
            onChange={(event) => {
              const file = event.target.files?.[0] ?? null;
              event.target.value = "";
              choosePackageImportFile(file);
            }}
            type="file"
          />
        </label>
        {packageImportFile && packageImportPreviewMutation.data ? (
          <div className="settings-backup-confirm">
            <Archive size={18} />
            <div>
              <strong>{packageImportFile.name}</strong>
              <p>
                schema v{packageImportPreviewMutation.data.schemaVersion} · 共{" "}
                {packageImportPreviewMutation.data.totalRows} 行 ·{" "}
                {packageImportPreviewMutation.data.fileCount} 个受控文件 ·{" "}
                {formatBytes(packageImportPreviewMutation.data.fileBytes)}
              </p>
              <ImportUpgradeDetails
                preview={packageImportPreviewMutation.data}
              />
              {!packageImportPreviewMutation.data.canApply ? (
                <ImportBlockerDetails
                  preview={packageImportPreviewMutation.data}
                />
              ) : packageImportPreviewMutation.data.applyMode === "append" ? (
                <ImportAppendDetails
                  preview={packageImportPreviewMutation.data}
                />
              ) : (
                <small>
                  应用前会自动创建并校验回滚备份；数据库或任一文件失败都会整批补偿。
                </small>
              )}
              <ImportResolutionDetails
                preview={packageImportPreviewMutation.data}
              />
            </div>
            <div className="settings-backup-confirm-actions">
              <button
                className="button button-secondary"
                disabled={packageImportApplyMutation.isPending}
                onClick={() => choosePackageImportFile(null)}
                type="button"
              >
                取消
              </button>
              <button
                className="button button-danger"
                disabled={
                  !packageImportPreviewMutation.data.canApply ||
                  packageImportApplyMutation.isPending
                }
                onClick={applyBusinessPackageImport}
                type="button"
              >
                {packageImportApplyMutation.isPending ? (
                  <LoaderCircle className="animate-spin" size={13} />
                ) : (
                  <Upload size={13} />
                )}
                {packageImportApplyMutation.isPending
                  ? "正在导入文件…"
                  : importNeedsUpgrade(packageImportPreviewMutation.data) &&
                      packageImportPreviewMutation.data.canApply
                    ? "确认升级并含文件导入"
                    : "确认含文件导入"}
              </button>
            </div>
          </div>
        ) : null}
      </div>

      {success ? (
        <p className="settings-backup-success" ref={successRef} role="status">
          <ShieldCheck size={14} />
          {success}
        </p>
      ) : null}
      {mutationError ? (
        <p className="form-error settings-backup-feedback" role="alert">
          {backupErrorText(mutationError)}
        </p>
      ) : null}

      <div className="settings-backup-list-header">
        <div>
          <strong>本地备份</strong>
          <span>
            列表展示上次成功校验记录；下载的完整备份包含
            SQLite、工作区身份、历史和受控文件，属于敏感本机数据，不适合作为普通业务
            ZIP 分享。
          </span>
        </div>
        <button
          aria-label="刷新备份列表"
          className="button button-quiet"
          disabled={backupsQuery.isFetching || locked}
          onClick={() => void backupsQuery.refetch()}
          type="button"
        >
          <RefreshCw
            className={backupsQuery.isFetching ? "animate-spin" : undefined}
            size={14}
          />
        </button>
      </div>

      {backupsQuery.isPending ? (
        <div aria-live="polite" className="settings-state" role="status">
          <LoaderCircle className="animate-spin" size={16} />
          正在读取本地备份…
        </div>
      ) : backupsQuery.isError ? (
        <div className="settings-state settings-state-error" role="alert">
          <AlertCircle size={16} />
          <div>
            <strong>无法读取备份列表</strong>
            <span>{backupErrorText(backupsQuery.error)}</span>
          </div>
          <button
            className="button button-secondary"
            onClick={() => void backupsQuery.refetch()}
            type="button"
          >
            重试
          </button>
        </div>
      ) : backupsQuery.data.length === 0 ? (
        <div className="settings-backup-empty">
          <Archive size={18} />
          <strong>还没有本地备份</strong>
          <span>创建后会保存在应用数据目录的 backups 文件夹中。</span>
        </div>
      ) : (
        <div className="settings-backup-list">
          {backupsQuery.data.map((backup) => (
            <BackupCard
              backup={backup}
              busy={locked}
              drillingId={drillingId}
              exportingId={exportingId}
              key={backup.id}
              onDrill={drill}
              onExport={exportBackupArchive}
              onDelete={chooseDelete}
              onRestore={chooseRestore}
              onVerify={verify}
              verifyingId={verifyingId}
            />
          ))}
        </div>
      )}

      {restoreCandidate ? (
        <section aria-label="确认恢复备份" className="settings-backup-confirm">
          <AlertCircle size={18} />
          <div>
            <strong>确认恢复到“{restoreCandidate.note || "手动备份"}”？</strong>
            <p>
              Sidecar
              会先再次演练并为当前状态创建自动回滚备份，然后挂起所有业务操作。关闭并重新打开应用后，才会在数据库打开前替换当前数据。
            </p>
            <small>
              目标：{formatLocalTime(restoreCandidate.createdAt)} · ID{" "}
              {restoreCandidate.id.slice(0, 8)}
            </small>
          </div>
          <div className="settings-backup-confirm-actions">
            <button
              className="button button-secondary"
              disabled={restoreMutation.isPending}
              onClick={() => setRestoreCandidate(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={restoreMutation.isPending}
              onClick={scheduleRestore}
              type="button"
            >
              {restoreMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={13} />
              ) : (
                <Undo2 size={13} />
              )}
              {restoreMutation.isPending
                ? "创建回滚点并安排中…"
                : "确认并安排恢复"}
            </button>
          </div>
        </section>
      ) : null}

      {deleteCandidate ? (
        <section aria-label="确认删除备份" className="settings-backup-confirm">
          <AlertCircle size={18} />
          <div>
            <strong>永久删除“{deleteCandidate.note || "手动备份"}”？</strong>
            <p>
              删除后无法再从这个备份恢复，但不会改变当前数据库或受控文件。损坏的备份包也可以通过此流程安全移除。
            </p>
            <small>
              目标：{formatLocalTime(deleteCandidate.createdAt)} · ID{" "}
              {deleteCandidate.id.slice(0, 8)}
            </small>
          </div>
          <div className="settings-backup-confirm-actions">
            <button
              className="button button-secondary"
              disabled={deleteMutation.isPending}
              onClick={() => setDeleteCandidate(null)}
              type="button"
            >
              取消
            </button>
            <button
              className="button button-danger"
              disabled={deleteMutation.isPending}
              onClick={deleteSelectedBackup}
              type="button"
            >
              {deleteMutation.isPending ? (
                <LoaderCircle className="animate-spin" size={13} />
              ) : (
                <Trash2 size={13} />
              )}
              {deleteMutation.isPending ? "正在永久删除…" : "确认永久删除"}
            </button>
          </div>
        </section>
      ) : null}

      {restoreReady ? (
        <section
          aria-live="assertive"
          className="settings-backup-restore-ready"
          role="status"
        >
          <ShieldCheck size={18} />
          <div>
            <strong>恢复已安全挂起，需要重启应用</strong>
            <p>
              重启前业务写入已停止。启动时将应用备份{" "}
              {restoreReady.backupId.slice(0, 8)}；当前状态的自动回滚点为{" "}
              {restoreReady.rollbackBackupId.slice(0, 8)}。
            </p>
          </div>
          <button
            className="button button-primary settings-backup-restart-button"
            disabled={restarting}
            onClick={() => void restartApplication()}
            type="button"
          >
            {restarting ? (
              <LoaderCircle className="animate-spin" size={13} />
            ) : (
              <RefreshCw size={13} />
            )}
            {restarting ? "正在安全重启…" : "立即安全重启"}
          </button>
          {restartError ? (
            <p
              className="form-error settings-backup-restart-error"
              role="alert"
            >
              {restartError}
            </p>
          ) : null}
        </section>
      ) : null}

      <p className="settings-inline-note">
        当前已开放每日计划备份与只清理自动包的保留策略、版本化业务
        JSON、含活动受控文件的 ZIP
        导入导出，以及备份的创建、查看、完整性校验、隔离演练、重启前安全恢复安排和确认删除；两类导入都支持同
        schema 零主键冲突追加，并在应用前创建回滚备份。
      </p>
    </>
  );
}
