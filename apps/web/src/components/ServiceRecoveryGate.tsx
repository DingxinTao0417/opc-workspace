import {
  AlertTriangle,
  FolderOpen,
  LoaderCircle,
  RefreshCw,
  RotateCcw,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { resetRuntimeConnection } from "../api/client";
import {
  getRuntimeDiagnostics,
  isDesktopRuntime,
  listStartupRestoreChoices,
  openDesktopLogDirectory,
  requestApplicationRestart,
  scheduleStartupRestore,
  type RuntimeDiagnostics,
  type StartupRestoreChoice,
  type StartupStage,
} from "../api/desktop";

type RuntimeGateState =
  "checking" | "starting" | "restarting" | "ready" | "error";

interface ServiceRecoveryGateProps {
  children: ReactNode;
  desktop?: boolean;
  loadRuntime?: () => Promise<RuntimeDiagnostics>;
  openLogs?: () => Promise<boolean>;
  restart?: () => Promise<boolean>;
  resetConnection?: () => void;
  pollIntervalMs?: number;
  listBackups?: () => Promise<StartupRestoreChoice[]>;
  scheduleRestore?: (
    backupId: string,
  ) => Promise<{ backupId: string; restartRequired: boolean }>;
}

const readyPollIntervalMs = 3_000;

const startupStageMessages: Record<StartupStage, string> = {
  waiting_for_sidecar: "正在连接本地服务。",
  acquiring_workspace_lock: "正在确认本地工作区未被其他进程占用。",
  checking_pending_restore: "正在检查是否有待完成的本地恢复。",
  verifying_restore_package: "正在验证待恢复备份。",
  applying_restore: "正在恢复本地数据，请勿退出应用。",
  verifying_restored_workspace: "正在验证已恢复的本地数据。",
  finalizing_restore: "正在完成恢复清理。",
  opening_database: "正在打开本地数据库。",
  creating_migration_rollback: "正在为数据升级创建安全回滚点。",
  applying_database_migration: "正在更新本地数据结构。",
  initializing_workspace: "正在初始化本地工作区。",
  starting_local_api: "正在启动本地 API。",
};

export function ServiceRecoveryGate({
  children,
  desktop = isDesktopRuntime(),
  loadRuntime = getRuntimeDiagnostics,
  openLogs = openDesktopLogDirectory,
  restart = requestApplicationRestart,
  resetConnection = resetRuntimeConnection,
  pollIntervalMs = 500,
  listBackups = listStartupRestoreChoices,
  scheduleRestore = scheduleStartupRestore,
}: ServiceRecoveryGateProps) {
  const queryClient = useQueryClient();
  const [state, setState] = useState<RuntimeGateState>(
    desktop ? "checking" : "ready",
  );
  const [diagnostics, setDiagnostics] = useState<RuntimeDiagnostics | null>(
    null,
  );
  const [refreshVersion, setRefreshVersion] = useState(0);
  const [action, setAction] = useState<"logs" | "restart" | null>(null);
  const [actionError, setActionError] = useState(false);
  const [businessTreeVersion, setBusinessTreeVersion] = useState(0);
  const managedReady = useRef(false);
  const lastReadyGeneration = useRef<number | null | undefined>(undefined);
  const recoveryCleanup = useRef<Promise<void> | null>(null);
  const [showRestorePicker, setShowRestorePicker] = useState(false);
  const [restoreChoices, setRestoreChoices] = useState<
    StartupRestoreChoice[] | null
  >(null);
  const [selectedBackupId, setSelectedBackupId] = useState<string | null>(null);
  const [restoreConsent, setRestoreConsent] = useState(false);
  const [restoreAction, setRestoreAction] = useState<
    "load" | "schedule" | null
  >(null);
  const [restoreError, setRestoreError] = useState<string | null>(null);
  const [restoreScheduled, setRestoreScheduled] = useState(false);

  useEffect(() => {
    if (!desktop) {
      managedReady.current = false;
      lastReadyGeneration.current = undefined;
      recoveryCleanup.current = null;
      return;
    }
    let cancelled = false;
    let timer: number | undefined;

    const schedule = (delay: number) => {
      timer = window.setTimeout(() => void check(), delay);
    };

    const beginRecoveryEpoch = (): Promise<void> | null => {
      if (!managedReady.current) return null;
      if (recoveryCleanup.current) return recoveryCleanup.current;

      setState("restarting");
      resetConnection();
      recoveryCleanup.current = queryClient
        .cancelQueries()
        .catch(() => undefined)
        .then(() => queryClient.clear());
      return recoveryCleanup.current;
    };

    const finishRecoveryEpoch = async (): Promise<boolean> => {
      const cleanup = recoveryCleanup.current;
      if (cleanup) await cleanup;
      if (cancelled) return false;
      if (cleanup) setBusinessTreeVersion((version) => version + 1);
      recoveryCleanup.current = null;
      return true;
    };

    const check = async () => {
      try {
        const result = await loadRuntime();
        if (cancelled) return;
        setDiagnostics(result);
        if (result.environment === "browser" || result.phase === "external") {
          if (!(await finishRecoveryEpoch())) return;
          managedReady.current = false;
          lastReadyGeneration.current = undefined;
          setState("ready");
          return;
        }
        if (result.phase === "ready") {
          if (
            managedReady.current &&
            lastReadyGeneration.current !== result.generation
          ) {
            beginRecoveryEpoch();
          }
          if (!(await finishRecoveryEpoch())) return;
          managedReady.current = true;
          lastReadyGeneration.current = result.generation;
          setState("ready");
          schedule(readyPollIntervalMs);
          return;
        }
        if (result.phase === "starting" || result.phase === "restarting") {
          const recovering = managedReady.current;
          beginRecoveryEpoch();
          setState(recovering ? "restarting" : result.phase);
          schedule(pollIntervalMs);
          return;
        }
        beginRecoveryEpoch();
        setState("error");
        schedule(readyPollIntervalMs);
      } catch {
        if (cancelled) return;
        beginRecoveryEpoch();
        setState("error");
        schedule(readyPollIntervalMs);
      }
    };

    void check();
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [
    desktop,
    loadRuntime,
    pollIntervalMs,
    queryClient,
    refreshVersion,
    resetConnection,
  ]);

  if (!desktop || state === "ready") {
    return <Fragment key={businessTreeVersion}>{children}</Fragment>;
  }

  if (state === "checking" || state === "starting" || state === "restarting") {
    const recovering = state === "restarting";
    const startupMessage = diagnostics?.startupStage
      ? startupStageMessages[diagnostics.startupStage]
      : null;
    return (
      <main className="service-recovery-page" role="status">
        <div className="service-recovery-card service-recovery-starting">
          <span aria-hidden="true" className="service-recovery-icon">
            <LoaderCircle className="animate-spin" size={22} />
          </span>
          <p className="eyebrow">本地运行环境</p>
          <h1>{recovering ? "正在恢复本地服务" : "正在启动本地服务"}</h1>
          <p>
            {recovering
              ? "正在重新建立本地 API 连接，业务页面会在新服务就绪后重新载入。"
              : (startupMessage ??
                "正在完成数据库检查和本地 API 握手，业务页面会在服务就绪后显示。")}
          </p>
          <small>
            {recovering
              ? "恢复期间已暂停业务页面，并清除上一代服务的本地查询缓存。"
              : "启动期间不会使用演示数据，也不会连接远程服务。"}
          </small>
        </div>
      </main>
    );
  }

  const versionFacts = [
    diagnostics?.appVersion ? `应用 ${diagnostics.appVersion}` : null,
    diagnostics?.apiVersion ? `API ${diagnostics.apiVersion}` : null,
    diagnostics?.schemaVersion ? `Schema ${diagnostics.schemaVersion}` : null,
  ].filter((value): value is string => Boolean(value));

  const runAction = async (kind: "logs" | "restart") => {
    setAction(kind);
    setActionError(false);
    try {
      const completed = await (kind === "logs" ? openLogs() : restart());
      if (!completed) setActionError(true);
    } catch {
      setActionError(true);
    } finally {
      setAction(null);
    }
  };

  const openRestorePicker = async () => {
    setRestoreError(null);
    setShowRestorePicker(true);
    setRestoreAction("load");
    try {
      const choices = await listBackups();
      setRestoreChoices(choices);
    } catch {
      setRestoreChoices([]);
      setRestoreError("无法读取本地备份列表。");
    } finally {
      setRestoreAction(null);
    }
  };

  const confirmRestore = async () => {
    if (!selectedBackupId || !restoreConsent) return;
    setRestoreAction("schedule");
    setRestoreError(null);
    try {
      await scheduleRestore(selectedBackupId);
      setRestoreScheduled(true);
    } catch {
      setRestoreError("启动前恢复准备失败，当前数据未被替换。");
    } finally {
      setRestoreAction(null);
    }
  };

  return (
    <main className="service-recovery-page" role="alert">
      <div className="service-recovery-card">
        <span aria-hidden="true" className="service-recovery-icon">
          <AlertTriangle size={22} />
        </span>
        <p className="eyebrow">本地服务恢复</p>
        <h1>本地服务未能正常启动</h1>
        <p>
          业务页面已暂停显示，本地数据不会被自动替换。可以重新检查状态、打开脱敏日志，从已有备份安排恢复，或跳过后重启。
        </p>
        {versionFacts.length ? (
          <p className="service-recovery-facts">{versionFacts.join(" · ")}</p>
        ) : null}
        {actionError ? (
          <p className="service-recovery-action-error">
            操作未完成，请稍后重试。这里不会显示原始路径或底层错误。
          </p>
        ) : null}
        <div className="service-recovery-actions">
          <button
            autoFocus
            className="button button-primary"
            onClick={() => setRefreshVersion((version) => version + 1)}
            type="button"
          >
            <RefreshCw size={14} />
            重新检查状态
          </button>
          <button
            className="button button-secondary"
            disabled={action !== null}
            onClick={() => void runAction("logs")}
            type="button"
          >
            <FolderOpen size={14} />
            {action === "logs" ? "正在打开…" : "打开日志目录"}
          </button>
          <button
            className="button button-secondary"
            disabled={action !== null || restoreScheduled}
            onClick={() => void openRestorePicker()}
            type="button"
          >
            从备份恢复
          </button>
          <button
            className="button button-secondary"
            disabled={action !== null}
            onClick={() => void runAction("restart")}
            type="button"
          >
            <RotateCcw size={14} />
            {action === "restart" ? "正在重启…" : "跳过并重启"}
          </button>
        </div>
        {showRestorePicker ? (
          <div className="service-recovery-restore-picker">
            <p>
              选择一份已有本地备份后安排恢复；跳过则保持当前数据。不会展示本机路径。
            </p>
            {restoreScheduled ? (
              <p className="service-recovery-facts">
                已安排恢复。请重启应用以在打开业务数据前应用该备份。
              </p>
            ) : (
              <>
                {restoreAction === "load" ? (
                  <p>正在读取本地备份…</p>
                ) : restoreChoices && restoreChoices.length === 0 ? (
                  <p>没有可展示的本地备份。</p>
                ) : (
                  <ul className="service-recovery-restore-list">
                    {(restoreChoices ?? []).map((choice) => {
                      const selectable =
                        choice.verificationStatus !== "invalid";
                      return (
                        <li key={choice.id}>
                          <label>
                            <input
                              checked={selectedBackupId === choice.id}
                              disabled={!selectable || restoreScheduled}
                              name="startup-restore-backup"
                              onChange={() => {
                                setSelectedBackupId(choice.id);
                                setRestoreConsent(false);
                              }}
                              type="radio"
                              value={choice.id}
                            />
                            <span>
                              {choice.createdAt
                                ? new Date(choice.createdAt).toLocaleString()
                                : "时间未知"}
                              {" · "}
                              {choice.kind}
                              {" · "}
                              {choice.verificationStatus}
                              {choice.schemaVersion
                                ? ` · schema ${choice.schemaVersion}`
                                : ""}
                              {selectable ? "" : " · 不可恢复"}
                            </span>
                          </label>
                        </li>
                      );
                    })}
                  </ul>
                )}
                <label className="service-recovery-restore-consent">
                  <input
                    checked={restoreConsent}
                    disabled={
                      !selectedBackupId ||
                      restoreScheduled ||
                      restoreAction !== null
                    }
                    onChange={(event) =>
                      setRestoreConsent(event.target.checked)
                    }
                    type="checkbox"
                  />
                  <span>
                    我确认用所选备份替换当前本地数据，并理解需要重启后才会应用。
                  </span>
                </label>
                {restoreError ? (
                  <p className="service-recovery-action-error">
                    {restoreError}
                  </p>
                ) : null}
                <div className="service-recovery-actions">
                  <button
                    className="button button-primary"
                    disabled={
                      !selectedBackupId ||
                      !restoreConsent ||
                      restoreAction !== null ||
                      restoreScheduled
                    }
                    onClick={() => void confirmRestore()}
                    type="button"
                  >
                    {restoreAction === "schedule"
                      ? "正在准备…"
                      : "安排恢复并准备重启"}
                  </button>
                  <button
                    className="button button-secondary"
                    disabled={restoreAction === "schedule"}
                    onClick={() => {
                      setShowRestorePicker(false);
                      setSelectedBackupId(null);
                      setRestoreConsent(false);
                      setRestoreError(null);
                    }}
                    type="button"
                  >
                    跳过备份选择
                  </button>
                </div>
              </>
            )}
          </div>
        ) : null}
        <small>
          为避免泄露数据库路径、会话令牌或业务内容，恢复页不展示 Sidecar
          原始错误。
        </small>
      </div>
    </main>
  );
}
