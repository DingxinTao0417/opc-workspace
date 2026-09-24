type InvokeCommand = (
  command: string,
  args?: Record<string, unknown>,
) => Promise<unknown>;

export type StartupStage =
  | "waiting_for_sidecar"
  | "acquiring_workspace_lock"
  | "checking_pending_restore"
  | "verifying_restore_package"
  | "applying_restore"
  | "verifying_restored_workspace"
  | "finalizing_restore"
  | "opening_database"
  | "creating_migration_rollback"
  | "applying_database_migration"
  | "initializing_workspace"
  | "starting_local_api";

export type NativeShortcutRegistration = "registered" | "unavailable";

export interface NativeShortcutDiagnostics {
  commandPalette: NativeShortcutRegistration;
  newTask: NativeShortcutRegistration;
}

export type DesktopCapabilityAvailability =
  "available" | "unavailable" | "not_implemented";

export interface DesktopCapabilities {
  tray: DesktopCapabilityAvailability;
  nativeNotifications: DesktopCapabilityAvailability;
  autostart: DesktopCapabilityAvailability;
  nativeFileDialogs: DesktopCapabilityAvailability;
  offlineUpdates: DesktopCapabilityAvailability;
}

export interface RuntimeDiagnostics {
  environment: "browser" | "desktop";
  phase: "external" | "starting" | "restarting" | "ready" | "error";
  generation: number | null;
  startupStage: StartupStage | null;
  nativeShortcuts: NativeShortcutDiagnostics | null;
  desktopCapabilities: DesktopCapabilities | null;
  appVersion: string | null;
  apiVersion: string | null;
  schemaVersion: string | null;
}

export function isDesktopRuntime(): boolean {
  if (typeof window === "undefined") return false;
  const runtimeWindow = window as Window & {
    __TAURI__?: unknown;
    __TAURI_INTERNALS__?: unknown;
  };
  return Boolean(runtimeWindow.__TAURI__ || runtimeWindow.__TAURI_INTERNALS__);
}

export async function requestApplicationRestart(
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("restart_application");
  return true;
}

export type StartupRestoreChoice = {
  id: string;
  createdAt: string | null;
  verificationStatus: "verified" | "unverified" | "invalid";
  kind: string;
  schemaVersion: number;
  note?: string | null;
};

export type ScheduledRestoreResult = {
  backupId: string;
  rollbackBackupId: string;
  requestedAt: string;
  restartRequired: boolean;
};

const canonicalId =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

function parseStartupRestoreChoice(value: unknown): StartupRestoreChoice {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("INVALID_RESPONSE");
  }
  const record = value as Record<string, unknown>;
  const id = String(record.id ?? "");
  const status = String(record.verificationStatus ?? "");
  const kind = String(record.kind ?? "");
  const schemaVersion = Number(record.schemaVersion);
  if (
    !canonicalId.test(id) ||
    !["verified", "unverified", "invalid"].includes(status) ||
    !kind ||
    kind.length > 40 ||
    !Number.isInteger(schemaVersion) ||
    schemaVersion < 0 ||
    (record.createdAt != null &&
      (typeof record.createdAt !== "string" ||
        !Number.isFinite(Date.parse(record.createdAt)))) ||
    (record.note != null &&
      (typeof record.note !== "string" || record.note.length > 120))
  ) {
    throw new Error("INVALID_RESPONSE");
  }
  return {
    id,
    createdAt: (record.createdAt as string | null) ?? null,
    verificationStatus: status as StartupRestoreChoice["verificationStatus"],
    kind,
    schemaVersion,
    note: (record.note as string | null | undefined) ?? null,
  };
}

export async function listStartupRestoreChoices(
  invokeCommand?: InvokeCommand,
): Promise<StartupRestoreChoice[]> {
  if (!invokeCommand && !isDesktopRuntime()) return [];
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  const raw = await invoke("list_startup_restore_choices");
  if (!Array.isArray(raw) || raw.length > 20) {
    throw new Error("INVALID_RESPONSE");
  }
  return raw.map(parseStartupRestoreChoice);
}

export async function scheduleStartupRestore(
  backupId: string,
  invokeCommand?: InvokeCommand,
): Promise<ScheduledRestoreResult> {
  if (!canonicalId.test(backupId)) {
    throw new Error("INVALID_RESPONSE");
  }
  if (!invokeCommand && !isDesktopRuntime()) {
    throw new Error("UNAVAILABLE");
  }
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  const raw = (await invoke("schedule_startup_restore", {
    args: { backupId, confirm: true },
  })) as Record<string, unknown> | null;
  if (!raw || typeof raw !== "object") {
    throw new Error("INVALID_RESPONSE");
  }
  const backup = String(raw.backupId ?? "");
  const rollback = String(raw.rollbackBackupId ?? "");
  const requestedAt = String(raw.requestedAt ?? "");
  if (
    !canonicalId.test(backup) ||
    !canonicalId.test(rollback) ||
    backup === rollback ||
    raw.restartRequired !== true ||
    !Number.isFinite(Date.parse(requestedAt))
  ) {
    throw new Error("INVALID_RESPONSE");
  }
  return {
    backupId: backup,
    rollbackBackupId: rollback,
    requestedAt,
    restartRequired: true,
  };
}

export async function openDesktopLogDirectory(
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("open_log_directory");
  return true;
}

export async function setCloseToTrayEnabled(
  enabled: boolean,
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("set_close_to_tray_enabled", { enabled });
  return true;
}

export async function openExternalBrowserWindow(
  url: string,
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("open_external_browser", { url });
  return true;
}

export async function closeExternalBrowserWindow(
  invokeCommand?: InvokeCommand,
): Promise<boolean> {
  if (!invokeCommand && !isDesktopRuntime()) return false;
  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  await invoke("close_external_browser");
  return true;
}

function optionalVersion(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function runtimeGeneration(value: unknown): number | null {
  if (value === null) return null;
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) {
    return value;
  }
  throw new Error("Invalid desktop runtime generation");
}

function startupStage(value: unknown): StartupStage | null {
  if (value === null || value === undefined) return null;
  const stages: ReadonlySet<string> = new Set([
    "waiting_for_sidecar",
    "acquiring_workspace_lock",
    "checking_pending_restore",
    "verifying_restore_package",
    "applying_restore",
    "verifying_restored_workspace",
    "finalizing_restore",
    "opening_database",
    "creating_migration_rollback",
    "applying_database_migration",
    "initializing_workspace",
    "starting_local_api",
  ]);
  if (typeof value === "string" && stages.has(value))
    return value as StartupStage;
  throw new Error("Invalid desktop startup stage");
}

function nativeShortcutRegistration(
  value: unknown,
): NativeShortcutRegistration {
  if (value === "registered" || value === "unavailable") return value;
  throw new Error("Invalid desktop shortcut registration");
}

function nativeShortcutDiagnostics(value: unknown): NativeShortcutDiagnostics {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Invalid desktop shortcut status");
  }
  const record = value as Record<string, unknown>;
  return {
    commandPalette: nativeShortcutRegistration(record.commandPalette),
    newTask: nativeShortcutRegistration(record.newTask),
  };
}

function desktopCapabilityAvailability(
  value: unknown,
): DesktopCapabilityAvailability {
  if (
    value === "available" ||
    value === "unavailable" ||
    value === "not_implemented"
  ) {
    return value;
  }
  throw new Error("Invalid desktop capability availability");
}

function parseDesktopCapabilities(value: unknown): DesktopCapabilities {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Invalid desktop capabilities");
  }
  const record = value as Record<string, unknown>;
  return {
    tray: desktopCapabilityAvailability(record.tray),
    nativeNotifications: desktopCapabilityAvailability(
      record.nativeNotifications,
    ),
    autostart: desktopCapabilityAvailability(record.autostart),
    nativeFileDialogs: desktopCapabilityAvailability(record.nativeFileDialogs),
    offlineUpdates: desktopCapabilityAvailability(record.offlineUpdates),
  };
}

export async function getRuntimeDiagnostics(
  invokeCommand?: InvokeCommand,
): Promise<RuntimeDiagnostics> {
  if (!invokeCommand && !isDesktopRuntime()) {
    return {
      environment: "browser",
      phase: "external",
      generation: null,
      startupStage: null,
      nativeShortcuts: null,
      desktopCapabilities: null,
      appVersion: null,
      apiVersion: null,
      schemaVersion: null,
    };
  }

  const invoke =
    invokeCommand ?? (await import("@tauri-apps/api/core")).invoke<unknown>;
  const value = await invoke("sidecar_status");
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Invalid desktop runtime status");
  }
  const record = value as Record<string, unknown>;
  let nativeShortcuts: NativeShortcutDiagnostics | null = null;
  let desktopCapabilities: DesktopCapabilities | null = null;
  try {
    nativeShortcuts = nativeShortcutDiagnostics(
      await invoke("desktop_shortcut_status"),
    );
  } catch {
    // Native shortcut registration must never prevent a recovery-safe desktop status read.
  }
  try {
    desktopCapabilities = parseDesktopCapabilities(
      await invoke("desktop_capabilities"),
    );
  } catch {
    // Capability discovery must never prevent a recovery-safe desktop status read.
  }
  const phase = record.phase;
  if (
    phase !== "starting" &&
    phase !== "restarting" &&
    phase !== "ready" &&
    phase !== "error"
  ) {
    throw new Error("Invalid desktop runtime phase");
  }

  return {
    environment: "desktop",
    phase,
    generation: runtimeGeneration(record.generation),
    startupStage: startupStage(record.startupStage),
    nativeShortcuts,
    desktopCapabilities,
    appVersion: optionalVersion(record.appVersion),
    apiVersion: optionalVersion(record.apiVersion),
    schemaVersion: optionalVersion(record.schemaVersion),
  };
}
