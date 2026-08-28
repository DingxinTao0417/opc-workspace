import {
  AlertCircle,
  Archive,
  DatabaseBackup,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
} from "lucide-react";
import { useState } from "react";
import { ApiError } from "../api/client";
import {
  useBackupsQuery,
  useCreateBackup,
  useDrillBackupRestore,
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
  onVerify,
}: {
  backup: BackupSummary;
  busy: boolean;
  drillingId: string | null;
  verifyingId: string | null;
  onDrill: (id: string) => void;
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
      </div>
    </article>
  );
}

export function BackupSettings() {
  const backupsQuery = useBackupsQuery();
  const createMutation = useCreateBackup();
  const verifyMutation = useVerifyBackup();
  const drillMutation = useDrillBackupRestore();
  const [note, setNote] = useState("");
  const [verifyingId, setVerifyingId] = useState<string | null>(null);
  const [drillingId, setDrillingId] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const pending =
    createMutation.isPending ||
    verifyMutation.isPending ||
    drillMutation.isPending;
  const mutationError =
    createMutation.error ?? verifyMutation.error ?? drillMutation.error;

  const create = () => {
    setSuccess(null);
    createMutation.reset();
    verifyMutation.reset();
    drillMutation.reset();
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
    drillMutation.mutate(id, {
      onSuccess: (result) =>
        setSuccess(
          `备份 ${result.backupId.slice(0, 8)} 已在隔离临时数据根完成恢复演练：schema v${result.sourceSchemaVersion} → v${result.resultSchemaVersion}，${result.artifactCount} 个受控文件。`,
        ),
      onSettled: () => setDrillingId(null),
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
            disabled={pending}
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
          disabled={pending}
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
          disabled={backupsQuery.isFetching || pending}
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
              busy={pending}
              drillingId={drillingId}
              key={backup.id}
              onDrill={drill}
              onVerify={verify}
              verifyingId={verifyingId}
            />
          ))}
        </div>
      )}

      <p className="settings-inline-note">
        当前已开放创建、查看、完整性校验和隔离恢复演练；实际替换当前数据、删除与
        JSON 导出会在后续独立安全流程完成后启用。
      </p>
    </>
  );
}
