import {
  AlertCircle,
  Archive,
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
import { useState, type ReactNode } from "react";
import { ApiError } from "../api/client";
import { requestApplicationRestart } from "../api/desktop";
import {
  useBackupsQuery,
  useApplyBusinessDataImport,
  useApplyBusinessPackageImport,
  useCreateBackup,
  useDeleteBackup,
  useDrillBackupRestore,
  useExportBusinessData,
  useExportBusinessPackage,
  usePreviewBusinessDataImport,
  usePreviewBusinessPackageImport,
  useRestoreDiagnosticsQuery,
  useScheduleBackupRestore,
  useVerifyBackup,
} from "../api/hooks";
import type {
  BackupSummary,
  BusinessImportPreview,
  BusinessPackageImportPreview,
} from "../types/models";

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
  app_settings: "设置",
  clients: "客户",
  client_activities: "客户动态",
  client_attachments: "客户附件",
  client_followups: "客户回访",
  projects: "项目",
  project_attachments: "项目附件",
  project_notes: "项目笔记",
  tasks: "任务",
  task_artifacts: "任务产出",
  focus_sessions: "专注记录",
  inbox_items: "收件箱",
  reminders: "提醒",
  roadmap_milestones: "路线图",
  content_items: "内容日历",
};

function ImportBlockerDetails({ preview }: { preview: ImportPreview }) {
  if (preview.blocker === "source_schema_older") {
    return (
      <small className="settings-import-blocker">
        源数据为 schema v{preview.schemaVersion}，当前工作区为 v
        {preview.targetSchemaVersion}
        。已识别为旧版本包，但尚未执行升级映射，数据没有改变。
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
  if (preview.blocker !== "target_not_empty") return null;
  return (
    <div className="settings-import-conflicts">
      <small className="settings-import-blocker">
        当前工作区已有 {preview.targetRows} 行业务事实；检测到{" "}
        {preview.keyConflicts}{" "}
        条主键重叠。当前仅提供只读冲突清单，不会覆盖或合并数据。
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

function backupErrorText(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof ApiError) {
    if (error.code === "BACKUP_SPACE_INSUFFICIENT") {
      return "备份位置可用空间不足，请清理备份位置或旧备份后重试。";
    }
    if (error.code === "BACKUP_CAPACITY_UNAVAILABLE") {
      return "暂时无法确认备份容量，请刷新容量状态并确认本地存储可用后重试。";
    }
    if (error.code === "BACKUP_INVALID") {
      return "备份完整性校验失败，请勿用它恢复数据。";
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
      return "恢复已经挂起，请关闭并重新打开应用后继续。";
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
    if (error.code === "IMPORT_TARGET_NOT_EMPTY") {
      return "当前工作区已有业务数据，安全导入不会覆盖它。";
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
  verifyingId,
  onDrill,
  onDelete,
  onRestore,
  onVerify,
}: {
  backup: BackupSummary;
  busy: boolean;
  drillingId: string | null;
  verifyingId: string | null;
  onDrill: (id: string) => void;
  onDelete: (backup: BackupSummary) => void;
  onRestore: (backup: BackupSummary) => void;
  onVerify: (id: string) => void;
}) {
  const invalid = backup.verificationStatus === "invalid";
  const verifying = verifyingId === backup.id;
  const drilling = drillingId === backup.id;
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
          <strong>{backup.note || "手动备份"}</strong>
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
  const restoreDiagnosticsQuery = useRestoreDiagnosticsQuery();
  const createMutation = useCreateBackup();
  const verifyMutation = useVerifyBackup();
  const drillMutation = useDrillBackupRestore();
  const restoreMutation = useScheduleBackupRestore();
  const deleteMutation = useDeleteBackup();
  const exportMutation = useExportBusinessData();
  const packageExportMutation = useExportBusinessPackage();
  const importPreviewMutation = usePreviewBusinessDataImport();
  const importApplyMutation = useApplyBusinessDataImport();
  const packageImportPreviewMutation = usePreviewBusinessPackageImport();
  const packageImportApplyMutation = useApplyBusinessPackageImport();
  const [importFile, setImportFile] = useState<File | null>(null);
  const [packageImportFile, setPackageImportFile] = useState<File | null>(null);
  const [note, setNote] = useState("");
  const [verifyingId, setVerifyingId] = useState<string | null>(null);
  const [drillingId, setDrillingId] = useState<string | null>(null);
  const [restoreCandidate, setRestoreCandidate] =
    useState<BackupSummary | null>(null);
  const [deleteCandidate, setDeleteCandidate] = useState<BackupSummary | null>(
    null,
  );
  const [scheduledRestore, setScheduledRestore] = useState<{
    backupId: string;
    rollbackBackupId: string;
  } | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [restartError, setRestartError] = useState<string | null>(null);
  const [restarting, setRestarting] = useState(false);
  const pending =
    createMutation.isPending ||
    verifyMutation.isPending ||
    drillMutation.isPending ||
    restoreMutation.isPending ||
    deleteMutation.isPending ||
    exportMutation.isPending ||
    packageExportMutation.isPending ||
    importPreviewMutation.isPending ||
    importApplyMutation.isPending ||
    packageImportPreviewMutation.isPending ||
    packageImportApplyMutation.isPending;
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
  const mutationError =
    createMutation.error ??
    verifyMutation.error ??
    drillMutation.error ??
    restoreMutation.error ??
    deleteMutation.error ??
    exportMutation.error ??
    packageExportMutation.error ??
    importPreviewMutation.error ??
    importApplyMutation.error ??
    packageImportPreviewMutation.error ??
    packageImportApplyMutation.error ??
    downloadError;

  const resetExportFeedback = () => {
    setDownloadError(null);
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
    importApplyMutation.mutate(importFile, {
      onSuccess: (result) => {
        setImportFile(null);
        importPreviewMutation.reset();
        setSuccess(
          `已原子导入 ${result.importedRows} 行业务数据；导入前回滚备份为 ${result.backupId.slice(0, 8)}。`,
        );
      },
    });
  };

  const applyBusinessPackageImport = () => {
    if (!packageImportFile || !packageImportPreviewMutation.data?.canApply)
      return;
    setSuccess(null);
    packageImportApplyMutation.reset();
    packageImportApplyMutation.mutate(packageImportFile, {
      onSuccess: (result) => {
        setPackageImportFile(null);
        packageImportPreviewMutation.reset();
        setSuccess(
          `已原子导入 ${result.importedRows} 行业务数据和 ${result.importedFiles} 个受控文件；导入前回滚备份为 ${result.backupId.slice(0, 8)}。`,
        );
      },
    });
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
      </header>

      {storageSettings}

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
              先预检官方导出文件；跨 schema
              会标明升级方向，非空工作区会列出目标表和主键重叠，但当前只对同
              schema 空工作区开放应用。
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
              {!importPreviewMutation.data.canApply ? (
                <ImportBlockerDetails preview={importPreviewMutation.data} />
              ) : (
                <small>
                  应用前会自动创建并校验回滚备份；任一行失败则整批回滚。
                </small>
              )}
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
                {importApplyMutation.isPending ? "正在导入…" : "确认导入"}
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
              方向与非空目标冲突，当前只对同 schema 空工作区开放应用。
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
              {!packageImportPreviewMutation.data.canApply ? (
                <ImportBlockerDetails
                  preview={packageImportPreviewMutation.data}
                />
              ) : (
                <small>
                  应用前会自动创建并校验回滚备份；数据库或任一文件失败都会整批补偿。
                </small>
              )}
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
                  : "确认含文件导入"}
              </button>
            </div>
          </div>
        ) : null}
      </div>

      {success ? (
        <p className="settings-backup-success" role="status">
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
          <span>列表展示上次成功校验记录，可随时重新进行完整校验。</span>
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
              key={backup.id}
              onDrill={drill}
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
        当前已开放版本化业务 JSON、含活动受控文件的 ZIP
        导入导出，以及备份的创建、查看、完整性校验、隔离演练、重启前安全恢复安排和确认删除；两类导入都只允许空工作区并先创建回滚备份。
      </p>
    </>
  );
}
