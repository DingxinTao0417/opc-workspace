import {
  AlertCircle,
  Archive,
  DatabaseBackup,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  Undo2,
} from "lucide-react";
import { useState } from "react";
import { ApiError } from "../api/client";
import {
  useBackupsQuery,
  useCreateBackup,
  useDrillBackupRestore,
  useScheduleBackupRestore,
  useVerifyBackup,
} from "../api/hooks";
import type { BackupSummary } from "../types/models";

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

function backupErrorText(error: unknown): string {
  if (error instanceof ApiError) {
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
  onRestore,
  onVerify,
}: {
  backup: BackupSummary;
  busy: boolean;
  drillingId: string | null;
  verifyingId: string | null;
  onDrill: (id: string) => void;
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
      </div>
    </article>
  );
}

export function BackupSettings() {
  const backupsQuery = useBackupsQuery();
  const createMutation = useCreateBackup();
  const verifyMutation = useVerifyBackup();
  const drillMutation = useDrillBackupRestore();
  const restoreMutation = useScheduleBackupRestore();
  const [note, setNote] = useState("");
  const [verifyingId, setVerifyingId] = useState<string | null>(null);
  const [drillingId, setDrillingId] = useState<string | null>(null);
  const [restoreCandidate, setRestoreCandidate] =
    useState<BackupSummary | null>(null);
  const [scheduledRestore, setScheduledRestore] = useState<{
    backupId: string;
    rollbackBackupId: string;
  } | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const pending =
    createMutation.isPending ||
    verifyMutation.isPending ||
    drillMutation.isPending ||
    restoreMutation.isPending;
  const locked = pending || scheduledRestore !== null;
  const mutationError =
    createMutation.error ??
    verifyMutation.error ??
    drillMutation.error ??
    restoreMutation.error;

  const create = () => {
    setSuccess(null);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
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
    setVerifyingId(id);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
    verifyMutation.mutate(id, {
      onSuccess: (backup) =>
        setSuccess(`备份 ${backup.id.slice(0, 8)} 完整性校验通过。`),
      onSettled: () => setVerifyingId(null),
    });
  };

  const drill = (id: string) => {
    setSuccess(null);
    setDrillingId(id);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
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
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
    restoreMutation.reset();
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

  return (
    <>
      <header className="settings-content-header">
        <h3>数据与备份</h3>
        <p>创建 SQLite 与受控文件处于同一写入边界的本地备份。</p>
      </header>

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
              onRestore={setRestoreCandidate}
              onVerify={verify}
              verifyingId={verifyingId}
            />
          ))}
        </div>
      )}

      {restoreCandidate ? (
        <section
          aria-label="确认恢复备份"
          className="settings-backup-restore-confirm"
        >
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
          <div className="settings-backup-restore-confirm-actions">
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

      {scheduledRestore ? (
        <section
          aria-live="assertive"
          className="settings-backup-restore-ready"
          role="status"
        >
          <ShieldCheck size={18} />
          <div>
            <strong>恢复已安全挂起，请关闭并重新打开应用</strong>
            <p>
              重启前业务写入已停止。启动时将应用备份{" "}
              {scheduledRestore.backupId.slice(0, 8)}；当前状态的自动回滚点为{" "}
              {scheduledRestore.rollbackBackupId.slice(0, 8)}。
            </p>
          </div>
        </section>
      ) : null}

      <p className="settings-inline-note">
        当前已开放创建、查看、完整性校验、隔离演练和重启前安全恢复安排；删除与
        JSON 导出会在后续独立流程完成后启用。
      </p>
    </>
  );
}
